package syncerfn

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"market/internal/v2/settings"
	"market/internal/v2/types"

	"github.com/golang/glog"
)

// Label constants for filtering apps
const (
	RemoveLabel  = "remove"
	SuspendLabel = "suspend"
)

// DetailFetchStep implements the third step: fetch detailed info for each app
type DetailFetchStep struct {
	DetailEndpointPath string                    // Relative path like "/api/v1/applications/info"
	BatchSize          int                       // Number of apps to fetch in each batch
	Version            string                    // Version parameter for API requests
	SettingsManager    *settings.SettingsManager // Settings manager to build complete URLs
}

// NewDetailFetchStep creates a new detail fetch step
func NewDetailFetchStep(detailEndpointPath string, version string, settingsManager *settings.SettingsManager) *DetailFetchStep {
	return &DetailFetchStep{
		DetailEndpointPath: detailEndpointPath,
		BatchSize:          10, // Default batch size of 10
		Version:            version,
		SettingsManager:    settingsManager,
	}
}

// NewDetailFetchStepWithBatchSize creates a new detail fetch step with custom batch size
func NewDetailFetchStepWithBatchSize(detailEndpointPath string, version string, settingsManager *settings.SettingsManager, batchSize int) *DetailFetchStep {
	return &DetailFetchStep{
		DetailEndpointPath: detailEndpointPath,
		BatchSize:          batchSize,
		Version:            version,
		SettingsManager:    settingsManager,
	}
}

// GetStepName returns the name of this step
func (d *DetailFetchStep) GetStepName() string {
	return "Detail Fetch Step"
}

// Execute performs the detail fetching logic with batch processing
func (d *DetailFetchStep) Execute(ctx context.Context, data *SyncContext) error {
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		return fmt.Errorf("no market source available in sync context")
	}

	detailURL := d.SettingsManager.BuildAPIURL(marketSource.BaseURL, d.DetailEndpointPath)

	if strings.HasPrefix(detailURL, "file://") {
		return nil
	}

	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			glog.Errorf("PANIC in DetailFetchStep.Execute: %v", r)
			data.AddError(fmt.Errorf("panic in DetailFetchStep.Execute: %v", r))
		}
	}()

	glog.V(2).Infof("Executing %s for %d apps in batches of %d", d.GetStepName(), len(data.AppIDs), d.BatchSize)

	if len(data.AppIDs) == 0 {
		glog.V(3).Info("No app IDs to fetch details for")
		return nil
	}

	// Process apps in batches
	totalBatches := (len(data.AppIDs) + d.BatchSize - 1) / d.BatchSize
	successCount := 0
	errorCount := 0
	overallStartTime := time.Now()

	glog.V(3).Infof("Starting detail fetch with %d total batches", totalBatches)

	for i := 0; i < len(data.AppIDs); i += d.BatchSize {
		batchNumber := (i / d.BatchSize) + 1
		end := i + d.BatchSize
		if end > len(data.AppIDs) {
			end = len(data.AppIDs)
		}

		batch := data.AppIDs[i:end]
		batchStartTime := time.Now()
		progressPercent := float64(batchNumber) / float64(totalBatches) * 100

		glog.V(3).Infof("Processing batch %d/%d (%.1f%% complete) with %d apps: %v",
			batchNumber, totalBatches, progressPercent, len(batch), batch)

		// Check context cancellation before processing each batch
		select {
		case <-ctx.Done():
			glog.V(3).Infof("Context cancelled during batch %d/%d processing", batchNumber, totalBatches)
			return fmt.Errorf("detail fetch cancelled: %w", ctx.Err())
		default:
		}

		// Add retry mechanism for batch processing
		maxRetries := 3
		var batchSuccessCount, batchErrorCount int

		for retry := 0; retry < maxRetries; retry++ {
			if retry > 0 {
				glog.V(3).Infof("Retrying batch %d/%d (attempt %d/%d)", batchNumber, totalBatches, retry+1, maxRetries)
				// Add exponential backoff delay
				backoffDelay := time.Duration(retry) * 2 * time.Second
				select {
				case <-ctx.Done():
					return fmt.Errorf("context cancelled during retry delay: %w", ctx.Err())
				case <-time.After(backoffDelay):
					// Continue with retry
				}
			}

			batchSuccessCount, batchErrorCount = d.fetchAppsBatch(ctx, batch, data)

			// If successful or context cancelled, break retry loop
			if batchSuccessCount > 0 || ctx.Err() != nil {
				break
			}

			// Log retry attempt
			if retry < maxRetries-1 {
				glog.V(2).Infof("Batch %d/%d failed, will retry: %d successful, %d errors",
					batchNumber, totalBatches, batchSuccessCount, batchErrorCount)
			}
		}

		successCount += batchSuccessCount
		errorCount += batchErrorCount

		batchDuration := time.Since(batchStartTime)
		// Log batch completion with timing
		glog.V(2).Infof("Batch %d/%d completed in %v: %d successful, %d errors",
			batchNumber, totalBatches, batchDuration, batchSuccessCount, batchErrorCount)

		// Add a small delay between batches to avoid overwhelming the API
		if batchNumber < totalBatches {
			glog.V(3).Infof("Waiting 200ms before processing next batch...")
			select {
			case <-ctx.Done():
				glog.V(3).Infof("Context cancelled during batch delay")
				return fmt.Errorf("detail fetch cancelled during batch delay: %w", ctx.Err())
			case <-time.After(200 * time.Millisecond): // Increased delay to 200ms
				// Continue to next batch
			}
		}
	}

	overallDuration := time.Since(overallStartTime)
	glog.V(2).Infof("Completed detail fetch in %v: %d successful, %d errors, total %d apps",
		overallDuration, successCount, errorCount, len(data.AppIDs))

	// Final cleanup: Check all remaining apps in LatestData.Data.Apps for suspend/remove labels
	// This handles cases where apps might have suspend/remove labels in the initial data fetch
	// but were not returned in the detail API response
	if data.LatestData != nil && data.LatestData.Data.Apps != nil {
		d.cleanupSuspendedAppsFromLatestData(data)
	}

	return nil
}

// CanSkip determines if this step can be skipped
func (d *DetailFetchStep) CanSkip(ctx context.Context, data *SyncContext) bool {
	// Skip if hashes match or no app IDs
	if data.HashMatches {
		glog.V(3).Infof("Skipping %s - hashes match, no sync required", d.GetStepName())
		return true
	}

	if len(data.AppIDs) == 0 {
		glog.V(3).Infof("Skipping %s - no app IDs to fetch details for", d.GetStepName())
		return true
	}

	return false
}

// fetchAppsBatch fetches detailed information for a batch of apps
func (d *DetailFetchStep) fetchAppsBatch(ctx context.Context, appIDs []string, data *SyncContext) (int, int) {
	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			glog.Errorf("PANIC in fetchAppsBatch: %v", r)
			data.AddError(fmt.Errorf("panic in fetchAppsBatch: %v", r))
		}
	}()

	// Get current market source from context
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		errMsg := fmt.Errorf("no market source available in sync context for detail fetch")
		data.AddError(errMsg)
		glog.Errorf("ERROR: %v", errMsg)
		return 0, len(appIDs)
	}

	// Build complete URL from market source base URL and endpoint path
	detailURL := d.SettingsManager.BuildAPIURL(marketSource.BaseURL, d.DetailEndpointPath)
	glog.V(2).Infof("Fetching details for batch from: %s, Source: %s", detailURL, marketSource.ID)

	request := types.AppsInfoRequest{
		AppIds:  appIDs,
		Version: d.Version,
	}

	// Use map[string]interface{} to avoid type mismatch with multi-language fields
	var rawResponse map[string]interface{}

	// Add timeout context for this specific request
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	requestStartTime := time.Now()
	glog.V(3).Infof("Sending batch request for %d apps...", len(appIDs))

	resp, err := data.Client.R().
		SetContext(requestCtx).
		SetBody(request).
		SetResult(&rawResponse).
		Post(detailURL)

	requestDuration := time.Since(requestStartTime)

	if err != nil {
		errMsg := fmt.Errorf("failed to fetch batch details for apps %v from %s: %w", appIDs, detailURL, err)
		data.AddError(errMsg)
		glog.Errorf("ERROR: %v (request took %v)", errMsg, requestDuration)
		return 0, len(appIDs)
	}

	// Log response status for debugging
	glog.V(3).Infof("Batch request completed with status: %d for apps: %v (request took %v)",
		resp.StatusCode(), appIDs, requestDuration)

	// Handle different status codes
	switch resp.StatusCode() {
	case 200:
		// Success - process the apps and replace simplified data with detailed data
		glog.V(3).Infof("Processing successful response for batch with %d apps", len(appIDs))

		// CRITICAL: Get sourceID BEFORE acquiring mutex lock to avoid deadlock
		// GetMarketSource() uses RLock() internally, and calling it while holding Lock()
		// would cause permanent deadlock in the same goroutine
		// IMPORTANT: use MarketSource.ID as the key for Sources map (not Name)
		sourceID := ""
		if marketSource := data.GetMarketSource(); marketSource != nil {
			sourceID = marketSource.ID
		}

		data.mutex.Lock()
		glog.V(3).Infof("Acquired mutex lock for batch processing")

		// Collect apps that need to be removed from cache to avoid nested locks
		appsToRemove := make([]struct {
			appID      string
			appInfoMap map[string]interface{}
		}, 0)

		// Extract apps from raw response
		if appsData, ok := rawResponse["apps"].(map[string]interface{}); ok {
			glog.V(3).Infof("Found %d apps in response data", len(appsData))
			// Update the original LatestData with detailed information
			if data.LatestData != nil && data.LatestData.Data.Apps != nil {
				glog.V(3).Infof("Processing %d apps in LatestData", len(appsData))

				for appID, appData := range appsData {
					glog.V(3).Infof("Processing app %s in batch", appID)
					if appInfoMap, ok := appData.(map[string]interface{}); ok {
						// Check for Suspend or Remove labels before processing the app
						shouldSkip := false
						shouldRemoveFromCache := false
						appInstalled := false
						var appName string
						if name, ok := appInfoMap["name"].(string); ok {
							appName = name
						}

						if appLabels, ok := appInfoMap["appLabels"].([]interface{}); ok {
							for _, labelInterface := range appLabels {
								if label, ok := labelInterface.(string); ok {
									if strings.EqualFold(label, SuspendLabel) || strings.EqualFold(label, RemoveLabel) {
										shouldSkip = true

										shouldRemoveFromCache = true
										if d.isAppInstalled(appName, sourceID, data) {
											shouldRemoveFromCache = false
											appInstalled = true
											glog.V(3).Infof("App %s is suspended but still installed, keeping cache entry", appName)
										}

										if shouldRemoveFromCache {
											// Collect app for removal instead of calling directly to avoid nested locks
											appsToRemove = append(appsToRemove, struct {
												appID      string
												appInfoMap map[string]interface{}
											}{appID: appID, appInfoMap: appInfoMap})
										}
										break
									}
								}
							}
						}

						// Skip processing if app should be removed
						if shouldSkip {
							if shouldRemoveFromCache {
								// Remove from LatestData immediately when no installation is active
								delete(data.LatestData.Data.Apps, appID)

								// Also remove ALL versions of this app from LatestData.Data.Apps (by name)
								if appName != "" {
									for otherAppID, otherAppData := range data.LatestData.Data.Apps {
										if otherAppInfoMap, ok := otherAppData.(map[string]interface{}); ok {
											if otherAppName, ok := otherAppInfoMap["name"].(string); ok && otherAppName == appName {
												delete(data.LatestData.Data.Apps, otherAppID)
											}
										}
									}
								}
							} else if appInstalled {
								// For installed delisted apps, use chartrepo's complete data as base
								// and merge with original data only when chartrepo fields are empty/null
								// This ensures chartrepo's rendered data is preserved
								originalAppData, hasOriginal := data.LatestData.Data.Apps[appID]

								// Use mergeAppData to intelligently merge chartrepo data with original data
								// This will use chartrepo data when available, and fall back to original when chartrepo is empty/null
								mergedAppData := d.mergeAppData(originalAppData, appInfoMap, appID, hasOriginal)

								// Ensure appLabels contains suspend/remove label from chartrepo response
								if chartrepoLabels, ok := appInfoMap["appLabels"].([]interface{}); ok && len(chartrepoLabels) > 0 {
									mergedAppData["appLabels"] = chartrepoLabels
								} else if hasOriginal {
									// If chartrepo doesn't return labels, check if original has suspend/remove labels
									if originalMap, ok := originalAppData.(map[string]interface{}); ok {
										if originalLabels, ok := originalMap["appLabels"].([]interface{}); ok && len(originalLabels) > 0 {
											hasSuspendOrRemove := false
											for _, labelInterface := range originalLabels {
												if label, ok := labelInterface.(string); ok {
													if strings.EqualFold(label, SuspendLabel) || strings.EqualFold(label, RemoveLabel) {
														hasSuspendOrRemove = true
														break
													}
												}
											}
											if hasSuspendOrRemove {
												mergedAppData["appLabels"] = originalLabels
											}
										}
									}
								}

								// Update LatestData with merged data (chartrepo data as base, original as fallback)
								data.LatestData.Data.Apps[appID] = mergedAppData
								data.DetailedApps[appID] = mergedAppData
							}
							continue
						}

						glog.V(3).Infof("Processing app %s data replacement", appID)

						// Preserve appLabels from original data if detail API doesn't return it
						// This handles the case where DataFetchStep has suspend/remove labels
						// but detail API returns old version without labels
						originalAppData, hasOriginal := data.LatestData.Data.Apps[appID]
						var preservedAppLabels interface{}
						var preservedCategories interface{}
						var preservedSupportArch interface{}
						if hasOriginal {
							if originalMap, ok := originalAppData.(map[string]interface{}); ok {
								if originalLabels, ok := originalMap["appLabels"].([]interface{}); ok && len(originalLabels) > 0 {
									// Check if original has suspend/remove labels
									hasSuspendOrRemoveInOriginal := false
									for _, labelInterface := range originalLabels {
										if label, ok := labelInterface.(string); ok {
											if strings.EqualFold(label, SuspendLabel) || strings.EqualFold(label, RemoveLabel) {
												hasSuspendOrRemoveInOriginal = true
												break
											}
										}
									}
									// If original has suspend/remove labels but detail API doesn't return labels or returns empty labels
									if hasSuspendOrRemoveInOriginal {
										detailLabels, hasDetailLabels := appInfoMap["appLabels"].([]interface{})
										if !hasDetailLabels || len(detailLabels) == 0 {
											// Preserve original labels (including suspend/remove)
											preservedAppLabels = originalLabels
										}
									}
								}

								// Preserve categories from original data if detail API returns null or empty
								if originalCategories, ok := originalMap["categories"]; ok && originalCategories != nil {
									detailCategories := appInfoMap["categories"]
									shouldPreserveCategories := false
									if detailCategories == nil {
										shouldPreserveCategories = true
									} else if detailCategoriesSlice, ok := detailCategories.([]interface{}); ok && len(detailCategoriesSlice) == 0 {
										shouldPreserveCategories = true
									}
									if shouldPreserveCategories {
										preservedCategories = originalCategories
									}
								}

								// Preserve supportArch from original data if detail API returns null or empty
								if originalSupportArch, ok := originalMap["supportArch"]; ok && originalSupportArch != nil {
									detailSupportArch := appInfoMap["supportArch"]
									shouldPreserveSupportArch := false
									if detailSupportArch == nil {
										shouldPreserveSupportArch = true
									} else if detailSupportArchSlice, ok := detailSupportArch.([]interface{}); ok && len(detailSupportArchSlice) == 0 {
										shouldPreserveSupportArch = true
									}
									if shouldPreserveSupportArch {
										preservedSupportArch = originalSupportArch
									}
								}
							}
						}

						// Replace the simplified app data with detailed data in LatestData
						// Use smart merge to preserve original fields when detail API returns empty/null
						detailedAppData := d.mergeAppData(originalAppData, appInfoMap, appID, hasOriginal)

						// Use preserved labels if available (only if detail API didn't return labels)
						// IMPORTANT: If detail API returns appLabels (including suspend/remove), use them instead of preserved labels
						if preservedAppLabels != nil {
							// Only use preserved labels if detail API didn't return any labels
							detailLabels, hasDetailLabels := appInfoMap["appLabels"].([]interface{})
							if !hasDetailLabels || len(detailLabels) == 0 {
								detailedAppData["appLabels"] = preservedAppLabels
							}
						}

						// Use preserved categories if available
						if preservedCategories != nil {
							detailedAppData["categories"] = preservedCategories
						}

						// Use preserved supportArch if available
						if preservedSupportArch != nil {
							detailedAppData["supportArch"] = preservedSupportArch
						}

						data.LatestData.Data.Apps[appID] = detailedAppData

						// Also store in DetailedApps for backward compatibility
						data.DetailedApps[appID] = detailedAppData

						// glog.V(3).Infof("DetailedAppData: %v", detailedAppData)

						// Log the main app information with more details
						glog.V(3).Infof("Replaced app data with details - ID: %s, Name: %s, Version: %s",
							appInfoMap["id"], appInfoMap["name"], appInfoMap["version"])
						glog.V(3).Infof("App %s data replacement completed", appID)
					} else {
						glog.V(3).Infof("Warning: App %s data is not a map", appID)
					}
				}
				glog.V(3).Info("Finished processing all apps in LatestData")
			} else {
				glog.V(3).Info("WARNING: LatestData or LatestData.Data.Apps is nil")
			}
		} else {
			glog.V(3).Info("WARNING: No apps data found in response")
		}

		glog.V(3).Info("Releasing mutex lock for batch processing")
		data.mutex.Unlock()
		glog.V(3).Info("Mutex lock released successfully")

		// Now remove apps from cache after releasing the main lock to avoid nested locks
		for _, appToRemove := range appsToRemove {
			d.removeAppFromCache(appToRemove.appID, appToRemove.appInfoMap, data)
		}

		// Count successful and failed apps
		successCount := 0
		if appsData, ok := rawResponse["apps"].(map[string]interface{}); ok {
			successCount = len(appsData)
		}

		errorCount := 0
		if notFound, ok := rawResponse["not_found"].([]interface{}); ok {
			errorCount = len(notFound)
		}

		if errorCount > 0 {
			glog.V(3).Infof("WARNING: Apps not found in batch: %v", rawResponse["not_found"])
		}

		glog.V(3).Infof("Successfully fetched and replaced details for %d/%d apps in batch", successCount, len(appIDs))
		return successCount, errorCount

	case 202:
		// Accepted - data is being loaded
		message := ""
		if msg, ok := rawResponse["message"].(string); ok {
			message = msg
		}
		errMsg := fmt.Errorf("data is being loaded for version %s, batch: %v - %s", d.Version, appIDs, message)
		data.AddError(errMsg)
		glog.Errorf("WARNING: %v", errMsg)
		// Return all apps as errors for 202 status
		return 0, len(appIDs)

	case 400:
		// Bad Request
		errMsg := fmt.Errorf("bad request for batch %v to %s: status %d", appIDs, detailURL, resp.StatusCode())
		data.AddError(errMsg)
		glog.Errorf("ERROR: %v", errMsg)
		return 0, len(appIDs)

	case 429:
		// Rate limit exceeded - this is a retryable error
		errMsg := fmt.Errorf("rate limit exceeded for batch %v to %s: status %d", appIDs, detailURL, resp.StatusCode())
		data.AddError(errMsg)
		glog.Errorf("ERROR: %v", errMsg)
		// Return all apps as errors for rate limiting - will trigger retry
		return 0, len(appIDs)

	case 500, 502, 503, 504:
		// Server errors - these are retryable
		errMsg := fmt.Errorf("server error for batch %v to %s: status %d", appIDs, detailURL, resp.StatusCode())
		data.AddError(errMsg)
		glog.Errorf("ERROR: %v", errMsg)
		// Return all apps as errors for server errors - will trigger retry
		return 0, len(appIDs)

	default:
		// Other errors
		errMsg := fmt.Errorf("detail API for batch %v to %s returned status %d", appIDs, detailURL, resp.StatusCode())
		data.AddError(errMsg)
		glog.Errorf("ERROR: %v", errMsg)
		return 0, len(appIDs)
	}
}

// removeAppFromCache removes an app from cache for all users
func (d *DetailFetchStep) removeAppFromCache(appID string, appInfoMap map[string]interface{}, data *SyncContext) {
	appName, ok := appInfoMap["name"].(string)
	if !ok || appName == "" {
		glog.V(3).Infof("Warning: Cannot remove app from cache - app name is empty for app: %s", appID)
		return
	}

	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			glog.Errorf("PANIC in removeAppFromCache: %v, id: %s, name: %s", r, appID, appName)
		}
	}()

	glog.V(3).Infof("Starting to remove app %s %s from cache", appID, appName)

	// Get app name for matching - when an app is suspended, remove ALL versions of that app

	// Get source ID from market source
	source := data.GetMarketSource()
	if source == nil {
		glog.V(3).Infof("Warning: MarketSource is nil, cannot remove app %s %s from cache", appID, appName)
		return
	}
	// IMPORTANT: use MarketSource.ID as the key for Sources map (not Name)
	sourceID := source.ID
	glog.V(2).Infof("Removing all versions of app %s (name: %s) from cache for source: %s (sourceID=%s)", appID, appName, source.Name, sourceID)

	if data.CacheManager == nil {
		glog.V(3).Infof("Warning: CacheManager is nil, cannot remove app from cache")
		return
	}

	// Step 1: Use try read lock to find all data that needs to be removed
	glog.V(2).Infof("Step 1: Attempting to acquire read lock to find data for removal")
	if !data.CacheManager.TryRLock() {
		glog.Warningf("[TryRLock] Warning: Read lock not available for app removal, skipping: %s %s", appID, appName)
		return
	}

	// Collect all data that needs to be removed
	type RemovalData struct {
		userID               string
		sourceID             string
		newLatestList        []*types.AppInfoLatestData
		newPendingList       []*types.AppInfoLatestPendingData
		originalLatestCount  int
		originalPendingCount int
	}

	var removals []RemovalData

	glog.V(3).Infof("Processing %d users for app removal (read phase)", len(data.Cache.Users))

	for userID, userData := range data.Cache.Users {
		sourceData, sourceExists := userData.Sources[sourceID]
		if !sourceExists {
			continue
		}

		// Create new lists without the target app (all versions)
		var newLatestList []*types.AppInfoLatestData
		var newPendingList []*types.AppInfoLatestPendingData

		// Filter latest list - remove ALL versions of the app by name
		for _, latestApp := range sourceData.AppInfoLatest {
			if latestApp == nil || latestApp.RawData == nil {
				newLatestList = append(newLatestList, latestApp)
				continue
			}
			// Remove all versions of the app with matching name
			if latestApp.RawData.Name != appName {
				newLatestList = append(newLatestList, latestApp)
			} else {
				glog.V(3).Infof("Removing app version %s (name: %s) from AppInfoLatest", latestApp.RawData.Version, appName)
			}
		}

		// Filter pending list - remove ALL versions of the app by name
		for _, pendingApp := range sourceData.AppInfoLatestPending {
			if pendingApp == nil || pendingApp.RawData == nil {
				newPendingList = append(newPendingList, pendingApp)
				continue
			}
			// Remove all versions of the app with matching name
			if pendingApp.RawData.Name != appName {
				newPendingList = append(newPendingList, pendingApp)
			} else {
				glog.V(3).Infof("Removing pending app version %s (name: %s) from AppInfoLatestPending", pendingApp.RawData.Version, appName)
			}
		}

		// Only add to removals if there were actually items to remove
		if len(newLatestList) != len(sourceData.AppInfoLatest) || len(newPendingList) != len(sourceData.AppInfoLatestPending) {
			removals = append(removals, RemovalData{
				userID:               userID,
				sourceID:             sourceID,
				newLatestList:        newLatestList,
				newPendingList:       newPendingList,
				originalLatestCount:  len(sourceData.AppInfoLatest),
				originalPendingCount: len(sourceData.AppInfoLatestPending),
			})
		}
	}

	if len(removals) > 0 {
		glog.V(2).Infof("Step 1 completed: Found %d users with data to remove", len(removals))
	}

	// Release read lock before acquiring write lock (must release manually since we need to acquire write lock)
	data.CacheManager.RUnlock()

	// Step 2: Use try write lock to quickly update the data
	if len(removals) == 0 {
		glog.V(3).Infof("No data found to remove for app: %s", appID)
		return
	}

	glog.V(2).Info("Step 2: Attempting to acquire write lock to update data")
	if !data.CacheManager.TryLock() {
		glog.Warningf("[TryLock] Warning: Write lock not available for app removal, skipping: %s %s", appID, appName)
		return
	}
	defer data.CacheManager.Unlock()

	// Collect sync requests to trigger after releasing the lock
	type SyncReq struct {
		userID   string
		sourceID string
	}
	var syncReqs []SyncReq

	// Quickly update all the data by replacing array pointers
	for _, removal := range removals {
		userData := data.Cache.Users[removal.userID]
		sourceData := userData.Sources[removal.sourceID]

		// Replace array pointers (atomic operation)
		sourceData.AppInfoLatest = removal.newLatestList
		sourceData.AppInfoLatestPending = removal.newPendingList

		glog.V(3).Infof("Updated user: %s, source: %s, app: %s (latest: %d->%d, pending: %d->%d)",
			removal.userID, removal.sourceID, appName,
			removal.originalLatestCount, len(removal.newLatestList),
			removal.originalPendingCount, len(removal.newPendingList))

		// Collect sync request
		syncReqs = append(syncReqs, SyncReq{
			userID:   removal.userID,
			sourceID: removal.sourceID,
		})
	}

	glog.V(3).Infof("App removal from cache completed for app: %s %s", appID, appName)

	// Trigger sync to Redis for all affected users and sources after releasing the lock
	// Use reflection to access the private requestSync method
	// We do this in a goroutine to avoid blocking and to ensure the lock is released first
	go func() {
		// Wait a bit to ensure the lock is released
		time.Sleep(10 * time.Millisecond)

		cmValue := reflect.ValueOf(data.CacheManager)
		if cmValue.Kind() == reflect.Ptr {
			cmValue = cmValue.Elem()
		}

		requestSyncMethod := cmValue.MethodByName("requestSync")
		if !requestSyncMethod.IsValid() {
			glog.V(3).Infof("Warning: Cannot find requestSync method in CacheManager, sync to Redis will be handled by StoreCompleteDataToPending")
			return
		}

		// SyncSource = 1 (based on iota: SyncUser=0, SyncSource=1)
		const SyncSource = 1

		for _, syncReq := range syncReqs {
			// Create SyncRequest struct value
			// SyncRequest has: UserID string, SourceID string, Type SyncType (int)
			syncRequestValue := reflect.New(reflect.TypeOf(struct {
				UserID   string
				SourceID string
				Type     int
			}{})).Elem()
			syncRequestValue.Field(0).SetString(syncReq.userID)
			syncRequestValue.Field(1).SetString(syncReq.sourceID)
			syncRequestValue.Field(2).SetInt(SyncSource)

			// Call requestSync method
			requestSyncMethod.Call([]reflect.Value{syncRequestValue})
			glog.V(3).Infof("Triggered sync to Redis for user: %s, source: %s, app: %s", syncReq.userID, syncReq.sourceID, appName)
		}
	}()
}

// cleanupSuspendedAppsFromLatestData checks all apps in LatestData.Data.Apps for suspend/remove labels
// and removes them if they are not installed
func (d *DetailFetchStep) cleanupSuspendedAppsFromLatestData(data *SyncContext) {
	if data.LatestData == nil || data.LatestData.Data.Apps == nil {
		return
	}

	sourceID := ""
	if marketSource := data.GetMarketSource(); marketSource != nil {
		// IMPORTANT: use MarketSource.ID as the key for Sources map (not Name)
		sourceID = marketSource.ID
	}

	// Collect apps to remove
	appsToRemove := make([]struct {
		appID      string
		appInfoMap map[string]interface{}
	}, 0)

	// Check each app in LatestData.Data.Apps
	for appID, appDataInterface := range data.LatestData.Data.Apps {
		appInfoMap, ok := appDataInterface.(map[string]interface{})
		if !ok {
			continue
		}

		// Check for suspend/remove labels
		if appLabels, ok := appInfoMap["appLabels"].([]interface{}); ok {
			hasSuspendOrRemove := false
			var appName string
			if name, ok := appInfoMap["name"].(string); ok {
				appName = name
			}

			for _, labelInterface := range appLabels {
				if label, ok := labelInterface.(string); ok {
					if strings.EqualFold(label, SuspendLabel) || strings.EqualFold(label, RemoveLabel) {
						hasSuspendOrRemove = true
						break
					}
				}
			}

			if hasSuspendOrRemove {
				shouldRemoveFromCache := true
				if appName != "" && d.isAppInstalled(appName, sourceID, data) {
					shouldRemoveFromCache = false
				}

				if shouldRemoveFromCache {
					appsToRemove = append(appsToRemove, struct {
						appID      string
						appInfoMap map[string]interface{}
					}{appID: appID, appInfoMap: appInfoMap})
				}
			}
		}
	}

	// Remove apps from LatestData.Data.Apps
	if len(appsToRemove) > 0 {
		// Collect app names that need to be removed (to remove all versions)
		appNamesToRemove := make(map[string]bool)
		for _, appToRemove := range appsToRemove {
			if appName, ok := appToRemove.appInfoMap["name"].(string); ok && appName != "" {
				appNamesToRemove[appName] = true
			}
			// Remove the specific appID from LatestData.Data.Apps
			delete(data.LatestData.Data.Apps, appToRemove.appID)
		}

		// Remove ALL versions of suspended apps from LatestData.Data.Apps (by name)
		for appID, appDataInterface := range data.LatestData.Data.Apps {
			appInfoMap, ok := appDataInterface.(map[string]interface{})
			if !ok {
				continue
			}
			if appName, ok := appInfoMap["name"].(string); ok {
				if appNamesToRemove[appName] {
					delete(data.LatestData.Data.Apps, appID)
				}
			}
		}

		// Also remove from cache - remove all versions by name
		for appName := range appNamesToRemove {
			// Find the first appInfoMap for this app name to use for removeAppFromCache
			var appInfoMapForRemoval map[string]interface{}
			var appIDForRemoval string
			for _, appToRemove := range appsToRemove {
				if name, ok := appToRemove.appInfoMap["name"].(string); ok && name == appName {
					appInfoMapForRemoval = appToRemove.appInfoMap
					appIDForRemoval = appToRemove.appID
					break
				}
			}
			if appInfoMapForRemoval != nil {
				d.removeAppFromCache(appIDForRemoval, appInfoMapForRemoval, data)
			}
		}
	}
}

// mergeAppData merges detail API data with original data, preserving original values when detail API returns empty/null
func (d *DetailFetchStep) mergeAppData(originalAppData interface{}, appInfoMap map[string]interface{}, appID string, hasOriginal bool) map[string]interface{} {
	// Start with detail API data
	detailedAppData := map[string]interface{}{
		// Basic fields
		"id":          appInfoMap["id"],
		"name":        appInfoMap["name"],
		"cfgType":     appInfoMap["cfgType"],
		"chartName":   appInfoMap["chartName"],
		"icon":        appInfoMap["icon"],
		"appID":       appInfoMap["appID"],
		"version":     appInfoMap["version"],
		"categories":  appInfoMap["categories"],
		"versionName": appInfoMap["versionName"],

		// Extended fields
		"promoteImage":   appInfoMap["promoteImage"],
		"promoteVideo":   appInfoMap["promoteVideo"],
		"subCategory":    appInfoMap["subCategory"],
		"locale":         appInfoMap["locale"],
		"developer":      appInfoMap["developer"],
		"requiredMemory": appInfoMap["requiredMemory"],
		"requiredDisk":   appInfoMap["requiredDisk"],
		"supportClient":  appInfoMap["supportClient"],
		"supportArch":    appInfoMap["supportArch"],
		"requiredGPU":    appInfoMap["requiredGPU"],
		"requiredCPU":    appInfoMap["requiredCPU"],
		"rating":         appInfoMap["rating"],
		"target":         appInfoMap["target"],
		"permission":     appInfoMap["permission"],
		"entrances":      appInfoMap["entrances"],
		"middleware":     appInfoMap["middleware"],
		"options":        appInfoMap["options"],

		// Additional metadata fields
		"submitter":     appInfoMap["submitter"],
		"doc":           appInfoMap["doc"],
		"website":       appInfoMap["website"],
		"featuredImage": appInfoMap["featuredImage"],
		"sourceCode":    appInfoMap["sourceCode"],
		"license":       appInfoMap["license"],
		"legal":         appInfoMap["legal"],
		"i18n":          appInfoMap["i18n"],

		"modelSize": appInfoMap["modelSize"],
		"namespace": appInfoMap["namespace"],
		"onlyAdmin": appInfoMap["onlyAdmin"],

		"lastCommitHash": appInfoMap["lastCommitHash"],
		"createTime":     appInfoMap["createTime"],
		"updateTime":     appInfoMap["updateTime"],
		"appLabels":      appInfoMap["appLabels"],
		"count":          appInfoMap["count"],
		"variants":       appInfoMap["variants"],

		// Version history information
		"versionHistory": appInfoMap["versionHistory"],

		// Legacy fields for backward compatibility
		"screenshots": appInfoMap["screenshots"],
		"tags":        appInfoMap["tags"],
		"metadata":    appInfoMap["metadata"],
		"updated_at":  appInfoMap["updated_at"],
	}

	// If we have original data, merge it intelligently
	if hasOriginal {
		if originalMap, ok := originalAppData.(map[string]interface{}); ok {
			// List of fields to preserve if detail API returns empty/null
			// IMPORTANT: appLabels must be preserved if detail API returns empty/null, especially for delisted apps
			fieldsToPreserve := []string{
				"icon", "description", "title", "developer", "promoteImage", "promoteVideo",
				"subCategory", "locale", "requiredMemory", "requiredDisk", "supportClient",
				"requiredGPU", "requiredCPU", "rating", "target", "permission", "entrances",
				"middleware", "options", "submitter", "doc", "website", "featuredImage",
				"sourceCode", "license", "legal", "i18n", "modelSize", "namespace",
				"onlyAdmin", "lastCommitHash", "createTime", "updateTime", "count",
				"variants", "screenshots", "tags", "metadata", "updated_at", "versionHistory",
				"fullDescription", "upgradeDescription", "cfgType", "chartName", "versionName",
				"id", "name", "appID", "version", "appLabels", "categories", "supportArch",
			}

			for _, field := range fieldsToPreserve {
				detailValue := detailedAppData[field]
				originalValue, hasOriginalValue := originalMap[field]

				// Check if detail API returned empty/null value
				shouldPreserve := false
				if detailValue == nil {
					shouldPreserve = true
				} else if strValue, ok := detailValue.(string); ok && strValue == "" {
					shouldPreserve = true
				} else if sliceValue, ok := detailValue.([]interface{}); ok && len(sliceValue) == 0 {
					shouldPreserve = true
				} else if mapValue, ok := detailValue.(map[string]interface{}); ok && len(mapValue) == 0 {
					shouldPreserve = true
				}

				// Preserve original value if detail API returned empty/null and original has value
				if shouldPreserve && hasOriginalValue && originalValue != nil {
					detailedAppData[field] = originalValue
				}
			}
		}
	}

	return detailedAppData
}

// preserveFieldsForDelistedApp preserves fields from original data if detail API returns empty/null values
// This ensures installed delisted apps retain all their information
func (d *DetailFetchStep) preserveFieldsForDelistedApp(originalMap, detailMap map[string]interface{}, appID string) {
	// List of fields to preserve if detail API returns empty/null
	fieldsToPreserve := []string{
		"icon", "description", "title", "developer", "promoteImage", "promoteVideo",
		"subCategory", "locale", "requiredMemory", "requiredDisk", "supportClient",
		"requiredGPU", "requiredCPU", "rating", "target", "permission", "entrances",
		"middleware", "options", "submitter", "doc", "website", "featuredImage",
		"sourceCode", "license", "legal", "i18n", "modelSize", "namespace",
		"onlyAdmin", "lastCommitHash", "createTime", "updateTime", "count",
		"variants", "screenshots", "tags", "metadata", "updated_at", "versionHistory",
		"fullDescription", "upgradeDescription", "cfgType", "chartName", "versionName",
	}

	for _, field := range fieldsToPreserve {
		detailValue := detailMap[field]
		originalValue, hasOriginal := originalMap[field]

		// Check if detail API returned empty/null value
		shouldPreserve := false
		if detailValue == nil {
			shouldPreserve = true
		} else if strValue, ok := detailValue.(string); ok && strValue == "" {
			shouldPreserve = true
		} else if sliceValue, ok := detailValue.([]interface{}); ok && len(sliceValue) == 0 {
			shouldPreserve = true
		} else if mapValue, ok := detailValue.(map[string]interface{}); ok && len(mapValue) == 0 {
			shouldPreserve = true
		}

		// Preserve original value if detail API returned empty/null and original has value
		if shouldPreserve && hasOriginal && originalValue != nil {
			originalMap[field] = originalValue
		}
	}
}

// isAppInstalled determines whether the given app is currently installed for the active source.
func (d *DetailFetchStep) isAppInstalled(appName, sourceID string, data *SyncContext) bool {
	if appName == "" || sourceID == "" || data == nil || data.Cache == nil || data.CacheManager == nil {
		return false
	}

	// English comment: use try read lock to safely inspect installation states
	if !data.CacheManager.TryRLock() {
		glog.Warningf("[TryRLock] Warning: Read lock not available for isAppInstalled check, returning false, source: %s, name: %s", sourceID, appName)
		return false
	}
	defer data.CacheManager.RUnlock()

	for _, userData := range data.Cache.Users {
		if userData == nil {
			continue
		}
		sourceData, ok := userData.Sources[sourceID]
		if !ok || sourceData == nil {
			continue
		}
		for _, appState := range sourceData.AppStateLatest {
			if appState == nil {
				continue
			}
			if appState.Status.Name == appName && appState.Status.State != "uninstalled" {
				return true
			}
		}
	}

	return false
}
