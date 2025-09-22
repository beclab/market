package syncerfn

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"market/internal/v2/settings"
	"market/internal/v2/types"
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
			log.Printf("PANIC in DetailFetchStep.Execute: %v", r)
			data.AddError(fmt.Errorf("panic in DetailFetchStep.Execute: %v", r))
		}
	}()

	log.Printf("Executing %s for %d apps in batches of %d", d.GetStepName(), len(data.AppIDs), d.BatchSize)

	if len(data.AppIDs) == 0 {
		log.Printf("No app IDs to fetch details for")
		return nil
	}

	// Process apps in batches
	totalBatches := (len(data.AppIDs) + d.BatchSize - 1) / d.BatchSize
	successCount := 0
	errorCount := 0
	overallStartTime := time.Now()

	log.Printf("Starting detail fetch with %d total batches", totalBatches)

	for i := 0; i < len(data.AppIDs); i += d.BatchSize {
		batchNumber := (i / d.BatchSize) + 1
		end := i + d.BatchSize
		if end > len(data.AppIDs) {
			end = len(data.AppIDs)
		}

		batch := data.AppIDs[i:end]
		batchStartTime := time.Now()
		progressPercent := float64(batchNumber) / float64(totalBatches) * 100

		log.Printf("Processing batch %d/%d (%.1f%% complete) with %d apps: %v",
			batchNumber, totalBatches, progressPercent, len(batch), batch)

		// Check context cancellation before processing each batch
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled during batch %d/%d processing", batchNumber, totalBatches)
			return fmt.Errorf("detail fetch cancelled: %w", ctx.Err())
		default:
		}

		// Add retry mechanism for batch processing
		maxRetries := 3
		var batchSuccessCount, batchErrorCount int

		for retry := 0; retry < maxRetries; retry++ {
			if retry > 0 {
				log.Printf("Retrying batch %d/%d (attempt %d/%d)", batchNumber, totalBatches, retry+1, maxRetries)
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
				log.Printf("Batch %d/%d failed, will retry: %d successful, %d errors",
					batchNumber, totalBatches, batchSuccessCount, batchErrorCount)
			}
		}

		successCount += batchSuccessCount
		errorCount += batchErrorCount

		batchDuration := time.Since(batchStartTime)
		// Log batch completion with timing
		log.Printf("Batch %d/%d completed in %v: %d successful, %d errors",
			batchNumber, totalBatches, batchDuration, batchSuccessCount, batchErrorCount)

		// Add a small delay between batches to avoid overwhelming the API
		if batchNumber < totalBatches {
			log.Printf("Waiting 200ms before processing next batch...")
			select {
			case <-ctx.Done():
				log.Printf("Context cancelled during batch delay")
				return fmt.Errorf("detail fetch cancelled during batch delay: %w", ctx.Err())
			case <-time.After(200 * time.Millisecond): // Increased delay to 200ms
				// Continue to next batch
			}
		}
	}

	overallDuration := time.Since(overallStartTime)
	log.Printf("Completed detail fetch in %v: %d successful, %d errors, total %d apps",
		overallDuration, successCount, errorCount, len(data.AppIDs))

	return nil
}

// CanSkip determines if this step can be skipped
func (d *DetailFetchStep) CanSkip(ctx context.Context, data *SyncContext) bool {
	// Skip if hashes match or no app IDs
	if data.HashMatches {
		log.Printf("Skipping %s - hashes match, no sync required", d.GetStepName())
		return true
	}

	if len(data.AppIDs) == 0 {
		log.Printf("Skipping %s - no app IDs to fetch details for", d.GetStepName())
		return true
	}

	return false
}

// fetchAppsBatch fetches detailed information for a batch of apps
func (d *DetailFetchStep) fetchAppsBatch(ctx context.Context, appIDs []string, data *SyncContext) (int, int) {
	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in fetchAppsBatch: %v", r)
			data.AddError(fmt.Errorf("panic in fetchAppsBatch: %v", r))
		}
	}()

	// Get current market source from context
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		errMsg := fmt.Errorf("no market source available in sync context for detail fetch")
		data.AddError(errMsg)
		log.Printf("ERROR: %v", errMsg)
		return 0, len(appIDs)
	}

	// Build complete URL from market source base URL and endpoint path
	detailURL := d.SettingsManager.BuildAPIURL(marketSource.BaseURL, d.DetailEndpointPath)
	log.Printf("Fetching details for batch from: %s", detailURL)

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
	log.Printf("Sending batch request for %d apps...", len(appIDs))

	resp, err := data.Client.R().
		SetContext(requestCtx).
		SetBody(request).
		SetResult(&rawResponse).
		Post(detailURL)

	requestDuration := time.Since(requestStartTime)

	if err != nil {
		errMsg := fmt.Errorf("failed to fetch batch details for apps %v from %s: %w", appIDs, detailURL, err)
		data.AddError(errMsg)
		log.Printf("ERROR: %v (request took %v)", errMsg, requestDuration)
		return 0, len(appIDs)
	}

	// Log response status for debugging
	log.Printf("Batch request completed with status: %d for apps: %v (request took %v)",
		resp.StatusCode(), appIDs, requestDuration)

	// Handle different status codes
	switch resp.StatusCode() {
	case 200:
		// Success - process the apps and replace simplified data with detailed data
		log.Printf("Processing successful response for batch with %d apps", len(appIDs))
		data.mutex.Lock()
		log.Printf("Acquired mutex lock for batch processing")

		// Collect apps that need to be removed from cache to avoid nested locks
		appsToRemove := make([]struct {
			appID      string
			appInfoMap map[string]interface{}
		}, 0)

		// Extract apps from raw response
		if appsData, ok := rawResponse["apps"].(map[string]interface{}); ok {
			log.Printf("Found %d apps in response data", len(appsData))
			// Update the original LatestData with detailed information
			if data.LatestData != nil && data.LatestData.Data.Apps != nil {
				log.Printf("Processing %d apps in LatestData", len(appsData))

				for appID, appData := range appsData {
					log.Printf("Processing app %s in batch", appID)
					if appInfoMap, ok := appData.(map[string]interface{}); ok {
						// Check for Suspend or Remove labels before processing the app
						shouldSkip := false
						if appLabels, ok := appInfoMap["appLabels"].([]interface{}); ok {
							log.Printf("App %s has %d labels", appID, len(appLabels))
							for _, labelInterface := range appLabels {
								if label, ok := labelInterface.(string); ok {
									if strings.EqualFold(label, SuspendLabel) || strings.EqualFold(label, RemoveLabel) {
										shouldSkip = true
										log.Printf("Warning: Skipping app %s - contains label: %s", appID, label)

										// Collect app for removal instead of calling directly to avoid nested locks
										appsToRemove = append(appsToRemove, struct {
											appID      string
											appInfoMap map[string]interface{}
										}{appID: appID, appInfoMap: appInfoMap})
										break
									}
								}
							}
						} else {
							log.Printf("App %s has no labels or labels is not an array", appID)
						}

						// Skip processing if app should be removed
						if shouldSkip {
							log.Printf("Skipping app %s due to suspend/remove label", appID)
							// Remove from LatestData immediately
							delete(data.LatestData.Data.Apps, appID)
							log.Printf("Removed app %s from LatestData due to suspend/remove label", appID)
							continue
						}

						log.Printf("Processing app %s data replacement", appID)

						// Replace the simplified app data with detailed data in LatestData
						detailedAppData := map[string]interface{}{
							// Basic fields
							"id":        appInfoMap["id"],
							"name":      appInfoMap["name"],
							"cfgType":   appInfoMap["cfgType"],
							"chartName": appInfoMap["chartName"],
							"icon":      appInfoMap["icon"],
							// Skip multi-language fields to avoid type mismatch
							// "description": appInfoMap["description"],
							"appID": appInfoMap["appID"],
							// "title":       appInfoMap["title"],
							"version":     appInfoMap["version"],
							"categories":  appInfoMap["categories"],
							"versionName": appInfoMap["versionName"],

							// Extended fields - skip multi-language fields
							// "fullDescription":    appInfoMap["fullDescription"],
							// "upgradeDescription": appInfoMap["upgradeDescription"],
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

						data.LatestData.Data.Apps[appID] = detailedAppData

						// Also store in DetailedApps for backward compatibility
						data.DetailedApps[appID] = detailedAppData

						// log.Printf("DetailedAppData: %v", detailedAppData)

						// Log the main app information with more details
						log.Printf("Replaced app data with details - ID: %s, Name: %s, Version: %s",
							appInfoMap["id"], appInfoMap["name"], appInfoMap["version"])
						log.Printf("App %s data replacement completed", appID)
					} else {
						log.Printf("Warning: App %s data is not a map", appID)
					}
				}
				log.Printf("Finished processing all apps in LatestData")
			} else {
				log.Printf("WARNING: LatestData or LatestData.Data.Apps is nil")
			}
		} else {
			log.Printf("WARNING: No apps data found in response")
		}

		log.Printf("Releasing mutex lock for batch processing")
		data.mutex.Unlock()
		log.Printf("Mutex lock released successfully")

		// Now remove apps from cache after releasing the main lock to avoid nested locks
		for _, appToRemove := range appsToRemove {
			log.Printf("Calling removeAppFromCache for app %s", appToRemove.appID)
			d.removeAppFromCache(appToRemove.appID, appToRemove.appInfoMap, data)
			log.Printf("removeAppFromCache completed for app %s", appToRemove.appID)
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
			log.Printf("WARNING: Apps not found in batch: %v", rawResponse["not_found"])
		}

		log.Printf("Successfully fetched and replaced details for %d/%d apps in batch", successCount, len(appIDs))
		return successCount, errorCount

	case 202:
		// Accepted - data is being loaded
		message := ""
		if msg, ok := rawResponse["message"].(string); ok {
			message = msg
		}
		errMsg := fmt.Errorf("data is being loaded for version %s, batch: %v - %s", d.Version, appIDs, message)
		data.AddError(errMsg)
		log.Printf("WARNING: %v", errMsg)
		// Return all apps as errors for 202 status
		return 0, len(appIDs)

	case 400:
		// Bad Request
		errMsg := fmt.Errorf("bad request for batch %v to %s: status %d", appIDs, detailURL, resp.StatusCode())
		data.AddError(errMsg)
		log.Printf("ERROR: %v", errMsg)
		return 0, len(appIDs)

	case 429:
		// Rate limit exceeded - this is a retryable error
		errMsg := fmt.Errorf("rate limit exceeded for batch %v to %s: status %d", appIDs, detailURL, resp.StatusCode())
		data.AddError(errMsg)
		log.Printf("ERROR: %v", errMsg)
		// Return all apps as errors for rate limiting - will trigger retry
		return 0, len(appIDs)

	case 500, 502, 503, 504:
		// Server errors - these are retryable
		errMsg := fmt.Errorf("server error for batch %v to %s: status %d", appIDs, detailURL, resp.StatusCode())
		data.AddError(errMsg)
		log.Printf("ERROR: %v", errMsg)
		// Return all apps as errors for server errors - will trigger retry
		return 0, len(appIDs)

	default:
		// Other errors
		errMsg := fmt.Errorf("detail API for batch %v to %s returned status %d", appIDs, detailURL, resp.StatusCode())
		data.AddError(errMsg)
		log.Printf("ERROR: %v", errMsg)
		return 0, len(appIDs)
	}
}

// removeAppFromCache removes an app from cache for all users
func (d *DetailFetchStep) removeAppFromCache(appID string, appInfoMap map[string]interface{}, data *SyncContext) {
	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in removeAppFromCache: %v", r)
		}
	}()

	log.Printf("Starting to remove app %s from cache", appID)

	// Get app name for matching
	appName, ok := appInfoMap["name"].(string)
	if !ok || appName == "" {
		log.Printf("Warning: Cannot remove app from cache - app name is empty for app: %s", appID)
		return
	}

	// Get source ID from market source
	sourceID := data.GetMarketSource().Name
	log.Printf("Removing app %s (name: %s) from cache for source: %s", appID, appName, sourceID)

	if data.CacheManager == nil {
		log.Printf("Warning: CacheManager is nil, cannot remove app from cache")
		return
	}

	// Step 1: Use read lock to find all data that needs to be removed
	log.Printf("Step 1: Acquiring read lock to find data for removal")
	data.CacheManager.RLock()

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

	log.Printf("Processing %d users for app removal (read phase)", len(data.Cache.Users))

	for userID, userData := range data.Cache.Users {
		sourceData, sourceExists := userData.Sources[sourceID]
		if !sourceExists {
			continue
		}

		// Create new lists without the target app
		var newLatestList []*types.AppInfoLatestData
		var newPendingList []*types.AppInfoLatestPendingData

		// Filter latest list
		for _, latestApp := range sourceData.AppInfoLatest {
			if latestApp.RawData == nil || latestApp.RawData.Name != appName {
				newLatestList = append(newLatestList, latestApp)
			}
		}

		// Filter pending list
		for _, pendingApp := range sourceData.AppInfoLatestPending {
			if pendingApp.RawData == nil || pendingApp.RawData.Name != appName {
				newPendingList = append(newPendingList, pendingApp)
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

	// Release read lock
	data.CacheManager.RUnlock()
	log.Printf("Step 1 completed: Found %d users with data to remove", len(removals))

	// Step 2: Use write lock to quickly update the data
	if len(removals) == 0 {
		log.Printf("No data found to remove for app: %s", appID)
		return
	}

	log.Printf("Step 2: Acquiring write lock to update data")
	data.CacheManager.Lock()
	defer data.CacheManager.Unlock()

	// Quickly update all the data by replacing array pointers
	for _, removal := range removals {
		userData := data.Cache.Users[removal.userID]
		sourceData := userData.Sources[removal.sourceID]

		// Replace array pointers (atomic operation)
		sourceData.AppInfoLatest = removal.newLatestList
		sourceData.AppInfoLatestPending = removal.newPendingList

		log.Printf("Updated user: %s, source: %s, app: %s (latest: %d->%d, pending: %d->%d)",
			removal.userID, removal.sourceID, appName,
			removal.originalLatestCount, len(removal.newLatestList),
			removal.originalPendingCount, len(removal.newPendingList))
	}

	log.Printf("App removal from cache completed for app: %s", appID)
}
