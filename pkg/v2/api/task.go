package api

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/gorilla/mux"

	"market/internal/v2/store"
	"market/internal/v2/task"
	"market/internal/v2/types"
	"market/internal/v2/utils"
)

// opType* are the user_application_states.op_type values written by API
// handlers at pending-row creation. The strings are persisted verbatim and
// match the operation kinds NATS msg.OpType uses, so a NATS event can
// overwrite the value cleanly without a translation table.
const (
	opTypeInstall   = "install"
	opTypeUninstall = "uninstall"
	opTypeUpgrade   = "upgrade"
	opTypeClone     = "clone"
	opTypeCancel    = "cancel"
)

// writePendingState writes the initial user_application_states pending row
// for an app operation, before TaskModule.AddTask is invoked. Failures are
// logged but never block task creation: the legacy cache + NATS path
// continues to drive client-visible behaviour during phase 1; the PG row
// is observability-only until later phases wire readers against it.
//
// sourceID may legitimately be empty for handlers (cancelInstall,
// uninstallApp) that look up app metadata across all of a user's sources
// and happen not to find the app — in that case the call is a no-op
// rather than a fabrication of source. Same for ErrUserApplicationStateNotFound,
// which fires when no user_applications row exists for the (user, source,
// app) tuple (clones, or apps removed from catalog mid-flight). Both are
// logged at V(3) so they are visible during phase-1 monitoring without
// polluting normal logs.
func (s *Server) writePendingState(ctx context.Context, userID, sourceID, appID, opType string) {
	if sourceID == "" || appID == "" {
		glog.V(3).Infof("writePendingState: skipping (user=%s source=%q app=%q op=%s) — missing source or app",
			userID, sourceID, appID, opType)
		return
	}
	if err := store.UpsertPendingState(ctx, store.PendingStateInput{
		UserID:   userID,
		SourceID: sourceID,
		AppID:    appID,
		OpType:   opType,
	}); err != nil {
		switch {
		case errors.Is(err, store.ErrUserApplicationStateNotFound):
			glog.V(3).Infof("writePendingState: no user_applications row for (user=%s source=%s app=%s op=%s); skipping pending state write",
				userID, sourceID, appID, opType)
		default:
			glog.Errorf("writePendingState: persist pending state failed (user=%s source=%s app=%s op=%s): %v",
				userID, sourceID, appID, opType, err)
		}
	}
}

const syncTaskHardTimeout = 30 * time.Minute

type syncTaskResult struct {
	result *task.AppOperationMessage
	err    error
}

// waitForSyncTask blocks until the task completes, the request context is
// cancelled (client disconnect), or the hard timeout fires. It returns the
// task result/error and whether the task actually completed. The returned
// callback is safe for late invocation — it will never panic even if called
// after this function has returned.
func waitForSyncTask(ctx context.Context) (callback task.TaskCallback, wait func() (*task.AppOperationMessage, error, bool)) {
	done := make(chan struct{})
	var res syncTaskResult
	var once sync.Once

	callback = func(result *task.AppOperationMessage, err error) {
		once.Do(func() {
			res.result = result
			res.err = err
			close(done)
		})
	}

	wait = func() (*task.AppOperationMessage, error, bool) {
		select {
		case <-done:
			return res.result, res.err, true
		case <-ctx.Done():
			// Prefer completed result if both channels are ready.
			select {
			case <-done:
				return res.result, res.err, true
			default:
			}
			return nil, nil, false
		case <-time.After(syncTaskHardTimeout):
			select {
			case <-done:
				return res.result, res.err, true
			default:
			}
			return nil, nil, false
		}
	}

	return callback, wait
}

// InstallAppRequest represents the request body for app installation
type InstallAppRequest struct {
	Source  string           `json:"source"`
	AppName string           `json:"app_name"`
	Version string           `json:"version"`
	Sync    bool             `json:"sync"` // Whether this is a synchronous request
	Envs    []task.AppEnvVar `json:"envs,omitempty"`
}

// CloneAppRequest represents the request body for app clone
type CloneAppRequest struct {
	Source    string             `json:"source"`
	AppName   string             `json:"app_name"`
	Title     string             `json:"title"` // Title for cloned app (used for display purposes)
	Sync      bool               `json:"sync"`  // Whether this is a synchronous request
	Envs      []task.AppEnvVar   `json:"envs,omitempty"`
	Entrances []task.AppEntrance `json:"entrances,omitempty"`
}

// CancelInstallRequest represents the request body for cancel installation
type CancelInstallRequest struct {
	Sync bool `json:"sync"` // Whether this is a synchronous request
}

// cloneAppIDFromName derives the user_applications.app_id for a clone
// row from its wire-level app_name (rawAppName + requestHash). MD5 is
// used because its hex digest is exactly 32 characters — the
// user_applications.app_id column is VARCHAR(32). The choice is for
// fixed-width fit, NOT for cryptographic strength: collision resistance
// is irrelevant here because the input (newAppName) is already unique
// per (rawAppName, requestHash) and the (user_id, source_id, app_id)
// UNIQUE constraint catches any pathological collision at write time.
func cloneAppIDFromName(newAppName string) string {
	sum := md5.Sum([]byte(newAppName))
	return hex.EncodeToString(sum[:])
}

// calculateCloneRequestHash calculates SHA256 hash of the entire clone request (excluding Sync field) and returns first 6 characters
// The hash is based on: Source, AppName, Title, Envs, and Entrances
func calculateCloneRequestHash(request CloneAppRequest) string {
	// Create a struct for hashing that excludes Sync field
	type CloneRequestForHash struct {
		Source    string             `json:"source"`
		AppName   string             `json:"app_name"`
		Title     string             `json:"title"`
		Envs      []task.AppEnvVar   `json:"envs,omitempty"`
		Entrances []task.AppEntrance `json:"entrances,omitempty"`
	}

	hashData := CloneRequestForHash{
		Source:    request.Source,
		AppName:   request.AppName,
		Title:     request.Title,
		Envs:      request.Envs,
		Entrances: request.Entrances,
	}

	// Sort envs by name for consistent hashing
	if len(hashData.Envs) > 0 {
		sortedEnvs := make([]task.AppEnvVar, len(hashData.Envs))
		copy(sortedEnvs, hashData.Envs)
		sort.Slice(sortedEnvs, func(i, j int) bool {
			return sortedEnvs[i].EnvName < sortedEnvs[j].EnvName
		})
		hashData.Envs = sortedEnvs
	}

	// Sort entrances by name for consistent hashing
	if len(hashData.Entrances) > 0 {
		sortedEntrances := make([]task.AppEntrance, len(hashData.Entrances))
		copy(sortedEntrances, hashData.Entrances)
		sort.Slice(sortedEntrances, func(i, j int) bool {
			return sortedEntrances[i].Name < sortedEntrances[j].Name
		})
		hashData.Entrances = sortedEntrances
	}

	// Marshal to JSON for hashing
	requestJSON, err := json.Marshal(hashData)
	if err != nil {
		glog.Errorf("Failed to marshal clone request for hash calculation: %v", err)
		// Fallback: hash the error
		hash := sha256.Sum256([]byte(fmt.Sprintf("error:%v", err)))
		return hex.EncodeToString(hash[:])[:6]
	}

	// Calculate SHA256 hash
	hash := sha256.Sum256(requestJSON)
	hashStr := hex.EncodeToString(hash[:])

	// Return first 6 characters
	if len(hashStr) >= 6 {
		return hashStr[:6]
	}
	return hashStr
}

// extractInstallProductMetadata returns productID & developerName to
// help VC injection. Inputs are sourced from user_applications
// (price column + app_id column) so the helper does not depend on the
// applications.app_entry catalogue projection.
//
// fallbackAppID is the user_applications.app_id of the row being
// installed — used as the productID fallback when the price config
// does not carry an explicit ProductID.
func extractInstallProductMetadata(price *types.PriceConfig, fallbackAppID string) (string, string) {
	var productID string
	var developerName string

	if price != nil {
		developerName = strings.TrimSpace(price.Developer)

		if price.Paid != nil {
			if pid := strings.TrimSpace(price.Paid.ProductID); pid != "" {
				productID = pid
			} else if len(price.Paid.Price) > 0 && fallbackAppID != "" {
				productID = fallbackAppID
			}
		}

		if productID == "" {
			for _, product := range price.Products {
				if pid := strings.TrimSpace(product.ProductID); pid != "" {
					productID = pid
					break
				}
			}
		}
	}

	if productID == "" && fallbackAppID != "" {
		productID = fallbackAppID
	}

	return productID, developerName
}

// 6. Install application (single)
func (s *Server) installApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appID := vars["id"]
	glog.V(2).Infof("POST /api/v2/apps/%s/install - Installing app", appID)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(2).Infof("Retrieved user ID for install request: %s", userID)

	// Step 2: Parse request body
	var request InstallAppRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		glog.Errorf("Failed to parse request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request format", nil)
		return
	}

	// Step 3: Validate required fields
	if request.Source == "" || request.AppName == "" || request.Version == "" {
		glog.V(4).Infof("Missing required fields in request")
		s.sendResponse(w, http.StatusBadRequest, false, "Missing required fields: source, app_name, and version are required", nil)
		return
	}

	// Step 4: Resolve the user's rendered manifest from PG. App
	// operations only consume per-user data (user_applications); the
	// catalogue (applications) is intentionally not joined.
	row, err := store.GetAppInstallRow(r.Context(), userID, request.Source, request.AppName, request.Version)
	if err != nil {
		glog.Errorf("installApp: failed to load user_applications row for app=%s source=%s user=%s: %v",
			request.AppName, request.Source, userID, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to load app data", nil)
		return
	}
	if row == nil {
		glog.V(4).Infof("App not found: %s version %s in source: %s", request.AppName, request.Version, request.Source)
		s.sendResponse(w, http.StatusNotFound, false, "App not found", nil)
		return
	}

	if row.Price == nil {
		glog.V(2).Infof("installApp: row.Price is nil for app=%s, source=%s", request.AppName, request.Source)
	} else {
		glog.V(2).Infof("installApp: row.Price detected for app=%s, source=%s", request.AppName, request.Source)
	}

	// Step 5: Verify chart package exists
	chartFilename := fmt.Sprintf("%s-%s.tgz", request.AppName, request.Version)
	chartPath := filepath.Join(row.RenderedPackage, chartFilename)

	// if _, err := os.Stat(chartPath); err != nil {
	// 	glog.Errorf("Chart package not found at path: %s", chartPath)
	// 	s.sendResponse(w, http.StatusNotFound, false, "Chart package not found", nil)
	// 	return
	// }

	// Step 6: Resolve cfgType from manifest_type, defaulting when empty
	cfgType := row.ManifestType
	if cfgType == "" {
		glog.V(2).Infof("Warning: manifest_type empty for app: %s, using default 'app'", request.AppName)
		cfgType = "app"
	} else {
		glog.V(2).Infof("Retrieved cfgType: %s for app: %s", cfgType, request.AppName)
	}

	images := []task.Image{}
	if row.ImageAnalysis != nil {
		for _, image := range row.ImageAnalysis.Data.Images {
			images = append(images, task.Image{
				Name: image.Name,
				Size: image.TotalSize,
			})
		}
	}

	// realAppID is the user_applications.app_id of the row being
	// installed — the manifest's id, equivalent to the legacy
	// AppInfo.AppEntry.ID for non-clone rows (which is all install
	// targets). Falling back to request.AppName mirrors the legacy
	// path when both manifest ids are blank.
	appId := row.AppID

	var price *types.PriceConfig
	if row.Price != nil {
		p := row.Price.Data
		price = &p
	}
	productID, developerName := extractInstallProductMetadata(price, appId)
	glog.V(2).Infof("installApp: extracted product metadata app=%s, source=%s, productID=%s, developer=%s", request.AppName, request.Source, productID, developerName)

	glog.V(2).Infof("installApp: resolved appId=%s for app=%s, source=%s, sync=%v", appId, request.AppName, request.Source, request.Sync)

	// Step 7: Create installation task
	taskMetadata := map[string]interface{}{
		"user_id":    userID,
		"source":     request.Source,
		"app_name":   request.AppName,
		"version":    request.Version,
		"chart_path": chartPath,
		"token":      utils.GetTokenFromRequest(restfulReq),
		"cfgType":    cfgType, // Add cfgType to metadata
		"images":     images,
		"envs":       request.Envs,
	}
	if productID != "" {
		taskMetadata["productID"] = productID
		glog.V(2).Infof("installApp: added productID=%s to metadata for app=%s, source=%s", productID, request.AppName, request.Source)
	}
	if developerName != "" {
		taskMetadata["developerName"] = developerName
		glog.V(2).Infof("installApp: added developerName=%s to metadata for app=%s, source=%s", developerName, request.AppName, request.Source)
	}

	taskMetadata["realAppID"] = appId

	// user_application_states pending row is established by
	// taskModule.AddTask → store.CreateTask in the same transaction
	// as task_records, so the state side-effect cannot diverge from
	// the task creation. realAppID + source live in taskMetadata for
	// CreateTask to locate the row.

	// Handle synchronous requests with proper blocking
	if request.Sync { // +
		callback, wait := waitForSyncTask(r.Context())

		t, err := s.taskModule.AddTask(task.InstallApp, request.AppName, userID, taskMetadata, callback)
		if err != nil {
			glog.Errorf("Failed to create installation task for app: %s, error: %v", request.AppName, err)
			s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create installation task", nil)
			return
		}

		glog.V(2).Infof("Created synchronous installation task: ID=%s for app: %s, version: %s", t.ID, request.AppName, request.Version)

		taskResult, taskError, completed := wait()
		if !completed {
			glog.Warningf("Synchronous installation wait ended before completion for app: %s, task: %s", request.AppName, t.ID)
			s.sendResponse(w, http.StatusAccepted, true, "Task is still running, query task status for result", map[string]interface{}{
				"task_id": t.ID,
			})
			return
		}

		// var resultData map[string]interface{}
		// if taskResult != nil {
		// 	resultData = map[string]interface{}{
		// 		"raw_result": taskResult,
		// 	}
		// }

		if taskError != nil {
			glog.Errorf("Synchronous installation failed for app: %s, error: %v", request.AppName, taskError)
			s.sendResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("Installation failed: %v", taskError), taskResult)
		} else {
			glog.V(2).Infof("Synchronous installation completed successfully for app: %s", request.AppName)
			s.sendResponse(w, http.StatusOK, true, "App installation completed successfully", taskResult)
		}
		return
	}

	// Handle asynchronous requests
	callback := func(result *task.AppOperationMessage, err error) {
		// For async requests, callback is just for logging
		if err != nil {
			glog.Errorf("Asynchronous installation failed for app: %s, error: %v", request.AppName, err)
		} else {
			glog.V(2).Infof("Asynchronous installation completed successfully for app: %s", request.AppName)
		}
	}

	task, err := s.taskModule.AddTask(task.InstallApp, request.AppName, userID, taskMetadata, callback)
	if err != nil {
		glog.Errorf("Failed to create installation task for app: %s, error: %v", request.AppName, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create installation task", nil)
		return
	}

	glog.V(2).Infof("Created asynchronous installation task: ID=%s for app: %s, version: %s", task.ID, request.AppName, request.Version)

	// Return immediately for asynchronous requests
	s.sendResponse(w, http.StatusOK, true, "App installation started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}

// 6.1. Clone application (single)
func (s *Server) cloneApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appID := vars["id"]
	glog.V(2).Infof("POST /api/v2/apps/%s/clone - Cloning app", appID)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(2).Infof("Retrieved user ID for clone request: %s", userID)

	// Step 2: Parse request body
	var request CloneAppRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		glog.Errorf("Failed to parse request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request format", nil)
		return
	}

	// Step 3: Validate required fields
	if request.Source == "" || request.AppName == "" {
		glog.V(3).Info("Missing required fields in request")
		s.sendResponse(w, http.StatusBadRequest, false, "Missing required fields: source and app_name are required", nil)
		return
	}

	// Step 4: Resolve the user's rendered manifest from PG. cloneApp
	// only acts on the original chart row (app_id == app_raw_id);
	// GetAppInstallRow with version="" returns the latest successful
	// render of that row. The catalogue (applications) is intentionally
	// not joined.
	row, err := store.GetAppInstallRow(r.Context(), userID, request.Source, request.AppName, "")
	if err != nil {
		glog.Errorf("cloneApp: failed to load user_applications row for app=%s source=%s user=%s: %v",
			request.AppName, request.Source, userID, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to load app data", nil)
		return
	}
	if row == nil {
		glog.V(3).Infof("App not found: %s in source: %s", request.AppName, request.Source)
		s.sendResponse(w, http.StatusNotFound, false, "App not found", nil)
		return
	}

	// Step 5: rawAppName is the original chart's manifest name. The
	// SQL filters app_id = app_raw_id so row.AppName == row.AppRawName
	// for the row returned here; either field is correct.
	rawAppName := row.AppRawName
	if rawAppName == "" {
		glog.V(3).Infof("Raw app name not found for app: %s", request.AppName)
		s.sendResponse(w, http.StatusBadRequest, false, "Raw app name not found", nil)
		return
	}

	// Step 6: Calculate hash suffix from entire clone request and
	// construct new app name + app_id. newAppID is md5(newAppName) so
	// the clone has its own (user, source, app_id) identity in
	// user_applications, independent of the original chart's app_id.
	requestHash := calculateCloneRequestHash(request)
	newAppName := rawAppName + requestHash
	newAppID := cloneAppIDFromName(newAppName)

	// Step 7: Read the original app's installed version from PG. Clone
	// requires the original to already be installed; an empty result
	// means no user_application_states row exists for the (user,
	// source, rawAppName) tuple, which is the legitimate "not
	// installed" 404.
	appVersion, err := store.GetInstalledAppVersion(r.Context(), userID, request.Source, rawAppName)
	if err != nil {
		glog.Errorf("cloneApp: failed to read installed version for app=%s source=%s user=%s: %v",
			rawAppName, request.Source, userID, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to read installed app version", nil)
		return
	}
	if appVersion == "" {
		glog.V(3).Infof("Original app not found in installed state: %s in source: %s for user: %s", rawAppName, request.Source, userID)
		s.sendResponse(w, http.StatusNotFound, false, fmt.Sprintf("Original app %s is not installed in source %s", rawAppName, request.Source), nil)
		return
	}

	glog.V(2).Infof("Cloning app: rawAppName=%s, requestHash=%s, title=%s, newAppName=%s, version=%s (from installed original app)",
		rawAppName, requestHash, request.Title, newAppName, appVersion)

	// Step 8: Build chart_path using the original chart's
	// rendered_package directory and the freshly resolved version.
	chartFilename := fmt.Sprintf("%s-%s.tgz", request.AppName, appVersion)
	chartPath := filepath.Join(row.RenderedPackage, chartFilename)

	// Step 9: cfgType comes from manifest_type; fall back to "app" the
	// same way installApp does when manifest_type is empty.
	cfgType := row.ManifestType
	if cfgType == "" {
		glog.V(3).Infof("Warning: manifest_type empty for app: %s, using default 'app'", request.AppName)
		cfgType = "app"
	} else {
		glog.V(2).Infof("Retrieved cfgType: %s for app: %s", cfgType, request.AppName)
	}

	images := []task.Image{}
	if row.ImageAnalysis != nil {
		for _, image := range row.ImageAnalysis.Data.Images {
			images = append(images, task.Image{
				Name: image.Name,
				Size: image.TotalSize,
			})
		}
	}

	// Step 10: Materialise the clone's user_applications row is
	// DEFERRED to the task executor (internal/v2/task/app_clone.go),
	// which runs it ONLY after app-service has accepted the clone
	// request and returned an opID. Writing the row here used to be
	// the source of "ghost clone rows": app-service can reject a
	// clone with HTTP 422 when the request is missing required envs
	// / entrances, the user fills in the form, retries with slightly
	// different parameters, and each retry produces a fresh
	// (newAppName, newAppID) — md5(rawAppName + sha256(request)[:6])
	// is deterministic per-request, so every distinct request body
	// hashes to a distinct app_id and bypasses the
	// (user_id, source_id, app_id) UNIQUE constraint.
	//
	// Tradeoff: AddTask's pendingStateFromMetadata still emits a
	// PendingTaskState targeting newAppID below, so CreateTask
	// attempts the user_application_states upsert in-transaction
	// with task_records; the upsert soft-skips on
	// ErrUserApplicationStateNotFound (no user_applications row yet)
	// and the task_records insert still commits. The clone's state
	// row is materialised in the same executor step as the
	// user_applications row, after the opID is known.

	// Step 11: Create clone installation task. realAppID is the
	// clone row's app_id (md5(newAppName)) — the executor uses it
	// to call CloneUserApplication + UpsertPendingState after
	// app-service success. Version is also propagated so
	// installed_version / target_version land the same way they do
	// for install.
	taskMetadata := map[string]interface{}{
		"user_id":     userID,
		"source":      request.Source,
		"version":     appVersion, // Use version from targetApp
		"chart_path":  chartPath,
		"token":       utils.GetTokenFromRequest(restfulReq),
		"cfgType":     cfgType,
		"images":      images,
		"envs":        request.Envs,
		"entrances":   request.Entrances,
		"rawAppName":  rawAppName,    // Pass rawAppName in metadata
		"requestHash": requestHash,   // Pass requestHash in metadata
		"title":       request.Title, // Pass title in metadata (for display purposes)
		"realAppID":   newAppID,      // Anchor the state row to the clone's user_applications.app_id
	}

	// Handle synchronous requests with proper blocking
	if request.Sync {
		callback, wait := waitForSyncTask(r.Context())

		t, err := s.taskModule.AddTask(task.CloneApp, newAppName, userID, taskMetadata, callback)
		if err != nil {
			glog.Errorf("Failed to create clone installation task for app: %s, error: %v", newAppName, err)
			s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create clone installation task", nil)
			return
		}

		glog.V(2).Infof("Created synchronous clone installation task: ID=%s for app: %s (rawAppName=%s, requestHash=%s, title=%s)", t.ID, newAppName, rawAppName, requestHash, request.Title)

		taskResult, taskError, completed := wait()
		if !completed {
			glog.Warningf("Synchronous clone installation wait ended before completion for app: %s, task: %s", newAppName, t.ID)
			s.sendResponse(w, http.StatusAccepted, true, "Task is still running, query task status for result", map[string]interface{}{
				"task_id": t.ID,
			})
			return
		}

		var resultData map[string]interface{}
		if taskResult != nil {
			resultData = map[string]interface{}{
				"raw_result": taskResult,
			}
		}

		if taskError != nil {
			glog.Errorf("Synchronous clone installation failed for app: %s, error: %v", newAppName, taskError)
			s.sendResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("Clone installation failed: %v", taskError), resultData)
		} else {
			glog.V(2).Infof("Synchronous clone installation completed successfully for app: %s", newAppName)
			s.sendResponse(w, http.StatusOK, true, "App clone installation completed successfully", resultData)
		}
		return
	}

	// Handle asynchronous requests
	callback := func(result *task.AppOperationMessage, err error) {
		// For async requests, callback is just for logging
		if err != nil {
			glog.Errorf("Asynchronous clone installation failed for app: %s, error: %v", newAppName, err)
		} else {
			glog.V(2).Infof("Asynchronous clone installation completed successfully for app: %s", newAppName)
		}
	}

	task, err := s.taskModule.AddTask(task.CloneApp, newAppName, userID, taskMetadata, callback)
	if err != nil {
		glog.Errorf("Failed to create clone installation task for app: %s, error: %v", newAppName, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create clone installation task", nil)
		return
	}

	glog.V(2).Infof("Created asynchronous clone installation task: ID=%s for app: %s (rawAppName=%s, requestHash=%s, title=%s)", task.ID, newAppName, rawAppName, requestHash, request.Title)

	// Return immediately for asynchronous requests
	s.sendResponse(w, http.StatusOK, true, "App clone installation started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}

// 7. Cancel installation (single)
func (s *Server) cancelInstall(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appName := vars["id"]
	glog.V(2).Infof("DELETE /api/v2/apps/%s/install - Canceling app installation", appName)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(2).Infof("Retrieved user ID for cancel request: %s", userID)

	// Step 2: Parse request body for sync parameter
	var request CancelInstallRequest
	var sync bool
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&request); err == nil {
			sync = request.Sync
		}
	}

	// Step 3: Resolve (source, app_id, manifest_type) from PG.
	// cancelInstall receives only app_name on the wire; LookupAppLocator
	// pivots to the user_applications row across all of this user's
	// sources. stateSourceID + stateAppID feed the user_application_states
	// write that taskModule.AddTask performs in the same transaction
	// as task_records — the state column itself is owned by the NATS
	// pipeline; AddTask only refreshes op_type so the frontend sees
	// "running (canceling)" immediately. When no locator row matches
	// (empty stateSourceID / stateAppID), AddTask soft-skips the state
	// write and the task is still created — matching the legacy
	// best-effort semantics that a cancel must not be rejected because
	// the catalogue view is unavailable.
	var cfgType, stateSourceID, stateAppID string
	loc, err := store.LookupAppLocator(r.Context(), userID, appName)
	if err != nil {
		glog.Errorf("cancelInstall: failed to look up user_applications row for app=%s user=%s: %v",
			appName, userID, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to load app data", nil)
		return
	}
	if loc != nil {
		stateSourceID = loc.SourceID
		stateAppID = loc.AppID
		cfgType = loc.ManifestType
		glog.V(3).Infof("Retrieved cfgType: %s for app: %s from source: %s (app_id=%s)", cfgType, appName, stateSourceID, stateAppID)
	}
	if cfgType == "" {
		glog.V(3).Infof("Warning: cfgType not resolved for app: %s, using default 'app'", appName)
		cfgType = "app"
	}

	// Step 4: Create cancel installation task
	taskMetadata := map[string]interface{}{
		"user_id":  userID,
		"app_name": appName,
		"token":    utils.GetTokenFromRequest(restfulReq),
		"cfgType":  cfgType, // Use retrieved cfgType
	}
	// Push the resolved (source, app_id) into task metadata so
	// AddTask's PG transaction can find the user_application_states
	// row by (user, source, app_id), and so the executor's
	// linkStateOpID can later patch op_id onto the same row when
	// app-service returns it.
	if stateSourceID != "" {
		taskMetadata["source"] = stateSourceID
	}
	if stateAppID != "" {
		taskMetadata["realAppID"] = stateAppID
	}

	// Handle synchronous requests with proper blocking
	if sync {
		callback, wait := waitForSyncTask(r.Context())

		t, err := s.taskModule.AddTask(task.CancelAppInstall, appName, userID, taskMetadata, callback)
		if err != nil {
			glog.Errorf("Failed to create cancel installation task for app: %s, error: %v", appName, err)
			s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create cancel installation task", nil)
			return
		}

		glog.V(2).Infof("Created synchronous cancel installation task: ID=%s for app: %s", t.ID, appName)

		taskResult, taskError, completed := wait()
		if !completed {
			glog.Warningf("Synchronous cancel installation wait ended before completion for app: %s, task: %s", appName, t.ID)
			s.sendResponse(w, http.StatusAccepted, true, "Task is still running, query task status for result", map[string]interface{}{
				"task_id": t.ID,
			})
			return
		}

		var resultData map[string]interface{}
		if taskResult != nil {
			resultData = map[string]interface{}{
				"raw_result": taskResult,
			}
		}

		if taskError != nil {
			glog.Errorf("Synchronous cancel installation failed for app: %s, error: %v", appName, taskError)
			s.sendResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("Cancel installation failed: %v", taskError), resultData)
		} else {
			glog.V(2).Infof("Synchronous cancel installation completed successfully for app: %s", appName)
			s.sendResponse(w, http.StatusOK, true, "App installation cancellation completed successfully", resultData)
		}
		return
	}

	// Handle asynchronous requests
	callback := func(result *task.AppOperationMessage, err error) {
		// For async requests, callback is just for logging
		if err != nil {
			glog.Errorf("Asynchronous cancel installation failed for app: %s, error: %v", appName, err)
		} else {
			glog.V(3).Infof("Asynchronous cancel installation completed successfully for app: %s", appName)
		}
	}

	task, err := s.taskModule.AddTask(task.CancelAppInstall, appName, userID, taskMetadata, callback)
	if err != nil {
		glog.Errorf("Failed to create cancel installation task for app: %s, error: %v", appName, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create cancel installation task", nil)
		return
	}

	glog.V(2).Infof("Created asynchronous cancel installation task: ID=%s for app: %s", task.ID, appName)

	// Return immediately for asynchronous requests
	s.sendResponse(w, http.StatusOK, true, "App installation cancellation started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}

// 8. Uninstall application (single)
func (s *Server) uninstallApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appName := vars["id"]
	glog.V(2).Infof("DELETE /api/v2/apps/%s - Uninstalling app", appName)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(2).Infof("Retrieved user ID for uninstall request: %s", userID)

	// Step 2: Parse request body for sync / all / deleteData / version
	var requestBody map[string]interface{}
	var sync bool
	var all bool
	var deleteData bool
	var version string
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err == nil {
			if syncVal, ok := requestBody["sync"].(bool); ok {
				sync = syncVal
			}
			if allVal, ok := requestBody["all"].(bool); ok {
				all = allVal
			}
			if deleteDataVal, ok := requestBody["deleteData"].(bool); ok {
				deleteData = deleteDataVal
			}
			if versionVal, ok := requestBody["version"].(string); ok {
				version = strings.TrimSpace(versionVal)
			}
		}
	}

	// Step 3: Validate required fields. version is the anchor that
	// LookupInstalledApp uses to JOIN user_application_states and
	// pivot to the exact (user_applications) row install populated;
	// missing it would force a fall back to a fuzzy lookup and
	// re-introduce the multi-source / clone collision bugs.
	if version == "" {
		glog.V(3).Infof("uninstallApp: missing required field 'version' for app=%s user=%s", appName, userID)
		s.sendResponse(w, http.StatusBadRequest, false, "Missing required field: version", nil)
		return
	}

	// Step 4: Resolve (source, app_id, manifest_type) from PG via the
	// installed-app helper. The JOIN against user_application_states
	// guarantees this app has actually been installed (state row present)
	// and the installed_version filter locks onto the exact row install
	// committed at task-creation time. A nil result means "this app is
	// not installed at the requested version" — surfaced as 404 rather
	// than dispatching an uninstall against an unknown row.
	loc, err := store.LookupInstalledApp(r.Context(), userID, appName, version)
	if err != nil {
		glog.Errorf("uninstallApp: failed to look up installed app=%s version=%s user=%s: %v",
			appName, version, userID, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to load app data", nil)
		return
	}
	if loc == nil {
		glog.V(3).Infof("uninstallApp: app not installed (or version mismatch): app=%s version=%s user=%s",
			appName, version, userID)
		s.sendResponse(w, http.StatusNotFound, false, fmt.Sprintf("App %s version %s is not installed", appName, version), nil)
		return
	}
	stateSourceID := loc.SourceID
	stateAppID := loc.AppID
	cfgType := loc.ManifestType
	if cfgType == "" {
		glog.V(3).Infof("Warning: cfgType not resolved for app: %s, using default 'app'", appName)
		cfgType = "app"
	} else {
		glog.V(3).Infof("Retrieved cfgType: %s for app: %s from source: %s (app_id=%s, version=%s)",
			cfgType, appName, stateSourceID, stateAppID, version)
	}

	// Step 5: Create uninstallation task. (source, realAppID) are
	// guaranteed non-empty here — LookupInstalledApp returned a row,
	// otherwise we 404'd above. AddTask's PG transaction uses them
	// to ON CONFLICT-update the same user_application_states row
	// install populated; the executor's linkStateOpID likewise pivots
	// on (source, realAppID) when the op_id arrives back from
	// app-service.
	taskMetadata := map[string]interface{}{
		"user_id":    userID,
		"app_name":   appName,
		"token":      utils.GetTokenFromRequest(restfulReq),
		"cfgType":    cfgType,
		"all":        all,
		"deleteData": deleteData,
		"source":     stateSourceID,
		"realAppID":  stateAppID,
	}

	// Handle synchronous requests with proper blocking
	if sync {
		callback, wait := waitForSyncTask(r.Context())

		t, err := s.taskModule.AddTask(task.UninstallApp, appName, userID, taskMetadata, callback)
		if err != nil {
			glog.Errorf("Failed to create uninstallation task for app: %s, error: %v", appName, err)
			s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create uninstallation task", nil)
			return
		}

		glog.V(2).Infof("Created synchronous uninstallation task: ID=%s for app: %s", t.ID, appName)

		taskResult, taskError, completed := wait()
		if !completed {
			glog.Warningf("Synchronous uninstallation wait ended before completion for app: %s, task: %s", appName, t.ID)
			s.sendResponse(w, http.StatusAccepted, true, "Task is still running, query task status for result", map[string]interface{}{
				"task_id": t.ID,
			})
			return
		}

		var resultData map[string]interface{}
		if taskResult != nil {
			resultData = map[string]interface{}{
				"raw_result": taskResult,
			}
		}

		if taskError != nil {
			glog.Errorf("Synchronous uninstallation failed for app: %s, error: %v", appName, taskError)
			s.sendResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("Uninstallation failed: %v", taskError), resultData)
		} else {
			glog.V(2).Infof("Synchronous uninstallation completed successfully for app: %s", appName)
			s.sendResponse(w, http.StatusOK, true, "App uninstallation completed successfully", resultData)
		}
		return
	}

	// Handle asynchronous requests
	callback := func(result *task.AppOperationMessage, err error) {
		// For async requests, callback is just for logging
		if err != nil {
			glog.Errorf("Asynchronous uninstallation failed for app: %s, error: %v", appName, err)
		} else {
			glog.V(2).Infof("Asynchronous uninstallation completed successfully for app: %s", appName)
		}
	}

	task, err := s.taskModule.AddTask(task.UninstallApp, appName, userID, taskMetadata, callback)
	if err != nil {
		glog.Errorf("Failed to create uninstallation task for app: %s, error: %v", appName, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create uninstallation task", nil)
		return
	}

	glog.V(2).Infof("Created asynchronous uninstallation task: ID=%s for app: %s", task.ID, appName)

	// Return immediately for asynchronous requests
	s.sendResponse(w, http.StatusOK, true, "App uninstallation started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}

// 9. Upgrade application (single)
func (s *Server) upgradeApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appID := vars["id"]
	glog.V(2).Infof("PUT /api/v2/apps/%s/upgrade - Upgrading app", appID)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(2).Infof("Retrieved user ID for upgrade request: %s", userID)

	// Step 2: Parse request body
	var request InstallAppRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		glog.Errorf("Failed to parse request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request format", nil)
		return
	}

	// Step 3: Validate required fields
	if request.Source == "" || request.AppName == "" || request.Version == "" {
		glog.V(3).Infof("Missing required fields in request")
		s.sendResponse(w, http.StatusBadRequest, false, "Missing required fields: source, app_name, and version are required", nil)
		return
	}

	// Step 4: Resolve the user's rendered manifest from PG.
	// GetAppUpgradeRow handles both direct upgrade (request.AppName is
	// the original chart's name) and clone upgrade (request.AppName is
	// a clone alias) in a single round-trip, returning:
	//   - row: the ORIGINAL chart row (where the chart artefacts live)
	//   - matchedAppName: app_name of the row the wire request resolved
	//     to (== row.AppName for direct, == clone alias for clone)
	//   - matchedAppID: app_id of that same matched row — for direct it
	//     equals row.AppID, for clones it is the clone row's own app_id
	//     (md5 of newAppName, written by cloneApp). This is the value
	//     that goes into task metadata as realAppID so the state-row
	//     UPSERT lands on the row the user clicked on, not on the
	//     original chart row.
	row, matchedAppName, matchedAppID, err := store.GetAppUpgradeRow(r.Context(), userID, request.Source, request.AppName, request.Version)
	if err != nil {
		glog.Errorf("upgradeApp: failed to load user_applications row for app=%s version=%s source=%s user=%s: %v",
			request.AppName, request.Version, request.Source, userID, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to load app data", nil)
		return
	}
	if row == nil {
		glog.V(3).Infof("App not found: %s version %s in source: %s", request.AppName, request.Version, request.Source)
		s.sendResponse(w, http.StatusNotFound, false, "App not found", nil)
		return
	}

	var rawAppName string
	if matchedAppName != "" && matchedAppName != row.AppName {
		rawAppName = row.AppName
		glog.V(3).Infof("Resolved clone upgrade: cloneAlias=%s -> rawAppName=%s, version=%s", matchedAppName, rawAppName, request.Version)
	}

	// Step 5: Build chart_path. row.AppName == row.AppRawName because
	// GetAppUpgradeRow's outer alias filters app_id = app_raw_id.
	chartFilename := fmt.Sprintf("%s-%s.tgz", row.AppName, request.Version)
	chartPath := filepath.Join(row.RenderedPackage, chartFilename)

	// Step 6: cfgType comes from manifest_type with the standard "app"
	// fallback when empty.
	cfgType := row.ManifestType
	if cfgType == "" {
		glog.V(3).Infof("Warning: manifest_type empty for app: %s, using default 'app'", request.AppName)
		cfgType = "app"
	} else {
		glog.V(3).Infof("Retrieved cfgType: %s for app: %s", cfgType, request.AppName)
	}

	images := []task.Image{}
	if row.ImageAnalysis != nil {
		for _, image := range row.ImageAnalysis.Data.Images {
			images = append(images, task.Image{
				Name: image.Name,
				Size: image.TotalSize,
			})
		}
	}

	// Step 7: Create upgrade task. realAppID is matchedAppID — the
	// app_id of the row the user actually clicked on (clone row's
	// own app_id for clone upgrades, original's app_id for direct
	// upgrades). Without this, pendingStateFromMetadata would fall
	// back to app_name and upsertPendingStateInTx's WHERE app_id=?
	// would not match either row, silently soft-skipping the state
	// write.
	taskMetadata := map[string]interface{}{
		"user_id":    userID,
		"source":     request.Source,
		"app_name":   request.AppName, // Use request.AppName (clone app name) for upgrade task
		"version":    request.Version,
		"chart_path": chartPath,
		"token":      utils.GetTokenFromRequest(restfulReq),
		"cfgType":    cfgType, // Add cfgType to metadata
		"images":     images,
		"envs":       request.Envs,
		"realAppID":  matchedAppID,
	}

	// If this is a clone app, add rawAppName to metadata
	if rawAppName != "" {
		taskMetadata["rawAppName"] = rawAppName
		glog.V(3).Infof("Adding rawAppName to upgrade task metadata: %s for clone app: %s", rawAppName, request.AppName)
	}

	// Refresh the pending row before AddTask. Upgrade rows must already
	// exist (the app is currently installed), so writePendingState should
	// always find a matching user_applications row — log on miss for
	// observability.
	s.writePendingState(r.Context(), userID, request.Source, request.AppName, opTypeUpgrade)

	// Handle synchronous requests with proper blocking
	if request.Sync {
		callback, wait := waitForSyncTask(r.Context())

		t, err := s.taskModule.AddTask(task.UpgradeApp, request.AppName, userID, taskMetadata, callback)
		if err != nil {
			glog.Errorf("Failed to create upgrade task for app: %s, error: %v", request.AppName, err)
			s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create upgrade task", nil)
			return
		}

		glog.V(2).Infof("Created synchronous upgrade task: ID=%s for app: %s version: %s", t.ID, request.AppName, request.Version)

		taskResult, taskError, completed := wait()
		if !completed {
			glog.Warningf("Synchronous upgrade wait ended before completion for app: %s, task: %s", request.AppName, t.ID)
			s.sendResponse(w, http.StatusAccepted, true, "Task is still running, query task status for result", map[string]interface{}{
				"task_id": t.ID,
			})
			return
		}

		var resultData map[string]interface{}
		if taskResult != nil {
			resultData = map[string]interface{}{
				"raw_result": taskResult,
			}
		}

		if taskError != nil {
			glog.Errorf("Synchronous upgrade failed for app: %s, error: %v", request.AppName, taskError)
			s.sendResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("Upgrade failed: %v", taskError), resultData)
		} else {
			glog.V(2).Infof("Synchronous upgrade completed successfully for app: %s", request.AppName)
			s.sendResponse(w, http.StatusOK, true, "App upgrade completed successfully", resultData)
		}
		return
	}

	// Handle asynchronous requests
	callback := func(result *task.AppOperationMessage, err error) {
		// For async requests, callback is just for logging
		if err != nil {
			glog.Errorf("Asynchronous upgrade failed for app: %s, error: %v", request.AppName, err)
		} else {
			glog.V(3).Infof("Asynchronous upgrade completed successfully for app: %s", request.AppName)
		}
	}

	task, err := s.taskModule.AddTask(task.UpgradeApp, request.AppName, userID, taskMetadata, callback)
	if err != nil {
		glog.Errorf("Failed to create upgrade task for app: %s, error: %v", request.AppName, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create upgrade task", nil)
		return
	}

	glog.V(2).Infof("Created asynchronous upgrade task: ID=%s for app: %s version: %s", task.ID, request.AppName, request.Version)

	// Return immediately for asynchronous requests
	s.sendResponse(w, http.StatusOK, true, "App upgrade started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}
