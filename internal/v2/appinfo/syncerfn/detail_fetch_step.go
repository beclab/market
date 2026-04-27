package syncerfn

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"market/internal/v2/settings"
	"market/internal/v2/store"
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

// TODO(syncer): installed/uninstalling + delisted filtering is intentionally
// skipped in this step while cache is being removed from the pipeline.
// A later hydration/consistency stage should own that decision.

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

	detailURL := d.SettingsManager.BuildAPIURL(marketSource.SourceURL, d.DetailEndpointPath)

	if strings.HasPrefix(detailURL, "file://") {
		return nil
	}

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

	// TODO(syncer): cache-based suspended/remove cleanup will be removed with cache deprecation.

	return nil
}

// CanSkip determines if this step can be skipped
func (d *DetailFetchStep) CanSkip(ctx context.Context, data *SyncContext) bool {
	// Skip if hashes match or no app IDs
	// if data.HashMatches {
	// 	glog.V(3).Infof("Skipping %s - hashes match, no sync required", d.GetStepName())
	// 	return true
	// }

	// if len(data.AppIDs) == 0 {
	// 	glog.V(3).Infof("Skipping %s - no app IDs to fetch details for", d.GetStepName())
	// 	return true
	// }

	return false
}

// fetchAppsBatch fetches detailed information for a batch of apps
func (d *DetailFetchStep) fetchAppsBatch(ctx context.Context, appIDs []string, data *SyncContext) (int, int) {
	// Get current market source from context
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		errMsg := fmt.Errorf("no market source available in sync context for detail fetch")
		data.AddError(errMsg)
		glog.Errorf("ERROR: %v", errMsg)
		return 0, len(appIDs)
	}

	// Build complete URL from market source base URL and endpoint path
	detailURL := d.SettingsManager.BuildAPIURL(marketSource.SourceURL, d.DetailEndpointPath)
	glog.V(3).Infof("Fetching details for batch from: %s, Source: %s", detailURL, marketSource.SourceID)

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

	if resp.StatusCode() != 200 {
		errMsg := fmt.Errorf("detail API for batch %v to %s returned status %d", appIDs, detailURL, resp.StatusCode())
		data.AddError(errMsg)
		glog.Errorf("ERROR: %v", errMsg)
		return 0, len(appIDs)
	}

	// Sync added/version-changed apps to PG in one batch transaction.
	if err := d.syncBatchAppsToPG(ctx, appIDs, data, rawResponse); err != nil {
		errMsg := fmt.Errorf("sync batch apps to pg failed for source %s, batch %v: %w", marketSource.SourceID, appIDs, err)
		data.AddError(errMsg)
		glog.Errorf("ERROR: %v", errMsg)
		return 0, len(appIDs)
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
}

func (d *DetailFetchStep) syncBatchAppsToPG(ctx context.Context, appIDs []string, data *SyncContext, rawResponse map[string]interface{}) error {
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		return fmt.Errorf("no market source available in sync context for pg sync")
	}

	requestApps, ok := rawResponse["apps"].(map[string]interface{})
	if !ok || len(requestApps) == 0 {
		return nil
	}

	storedApps, err := store.QueryAppsByIDs(ctx, marketSource.SourceID, appIDs)
	if err != nil {
		return err
	}

	storedIDSet := make(map[string]struct{}, len(storedApps))
	storedVersionByID := make(map[string]string, len(storedApps))
	for _, app := range storedApps {
		if app == nil || app.AppID == "" {
			continue
		}
		storedIDSet[app.AppID] = struct{}{}
		storedVersionByID[app.AppID] = app.AppVersion
	}

	requestIDSet := make(map[string]struct{}, len(requestApps))
	requestVersionByID := make(map[string]string, len(requestApps))
	for requestAppID, requestAppData := range requestApps {
		appMap, ok := requestAppData.(map[string]interface{})
		if !ok || requestAppID == "" {
			continue
		}
		if hasSuspendOrRemoveLabels(appMap["appLabels"]) {
			continue
		}
		requestIDSet[requestAppID] = struct{}{}
		version, _ := appMap["version"].(string)
		requestVersionByID[requestAppID] = strings.TrimSpace(version)
	}

	storedIDs := make([]string, 0, len(storedIDSet))
	for id := range storedIDSet {
		storedIDs = append(storedIDs, id)
	}
	requestIDs := make([]string, 0, len(requestIDSet))
	for id := range requestIDSet {
		requestIDs = append(requestIDs, id)
	}
	sort.Strings(storedIDs)
	sort.Strings(requestIDs)
	diffAppIDs := diffAppIDs(storedIDs, requestIDs)

	if len(diffAppIDs.Kept) > 0 {
		changedAppIDs := make([]string, 0, len(diffAppIDs.Kept))
		for _, id := range diffAppIDs.Kept {
			oldVersion := strings.TrimSpace(storedVersionByID[id])
			newVersion := strings.TrimSpace(requestVersionByID[id])
			if newVersion == "" || oldVersion == newVersion {
				continue
			}
			changedAppIDs = append(changedAppIDs, id)
		}
		diffAppIDs.Kept = changedAppIDs
	}

	glog.Infof("SyncBatchApps, source: %s, add: %d, kept: %d",
		marketSource.SourceID, len(diffAppIDs.Added), len(diffAppIDs.Kept))

	return store.SyncBatchApps(ctx, store.BatchSyncInput{
		SourceID:      marketSource.SourceID,
		RequestApps:   requestApps,
		AddedAppIDs:   diffAppIDs.Added,
		ChangedAppIDs: diffAppIDs.Kept,
	})
}

func hasSuspendOrRemoveLabels(raw interface{}) bool {
	labels, ok := raw.([]interface{})
	if !ok || len(labels) == 0 {
		return false
	}
	for _, item := range labels {
		label, ok := item.(string)
		if !ok {
			continue
		}
		if strings.EqualFold(label, SuspendLabel) || strings.EqualFold(label, RemoveLabel) {
			return true
		}
	}
	return false
}

type appIDDiff struct {
	Added   []string
	Removed []string
	Kept    []string
}

func diffAppIDs(oldAppIDs, newAppIDs []string) appIDDiff {
	oldSet := make(map[string]struct{}, len(oldAppIDs))
	newSet := make(map[string]struct{}, len(newAppIDs))

	for _, id := range oldAppIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		oldSet[id] = struct{}{}
	}
	for _, id := range newAppIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		newSet[id] = struct{}{}
	}

	diff := appIDDiff{
		Added:   make([]string, 0),
		Removed: make([]string, 0),
		Kept:    make([]string, 0),
	}

	for id := range newSet {
		if _, exists := oldSet[id]; exists {
			diff.Kept = append(diff.Kept, id)
		} else {
			diff.Added = append(diff.Added, id)
		}
	}
	for id := range oldSet {
		if _, exists := newSet[id]; !exists {
			diff.Removed = append(diff.Removed, id)
		}
	}

	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	sort.Strings(diff.Kept)
	return diff
}
