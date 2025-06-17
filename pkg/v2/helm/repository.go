package helm

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/yaml.v3"

	"market/internal/v2/appinfo"
	"market/internal/v2/types"
)

// Global cache manager instance
var globalCacheManager *appinfo.CacheManager

// SetCacheManager sets the global cache manager instance
func SetCacheManager(cm *appinfo.CacheManager) {
	globalCacheManager = cm
	log.Printf("Helm Repository: Cache manager set successfully")
}

// HelmIndexEntry represents a single chart entry in the Helm repository index
type HelmIndexEntry struct {
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version"`
	AppVersion  string            `yaml:"appVersion,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Home        string            `yaml:"home,omitempty"`
	Sources     []string          `yaml:"sources,omitempty"`
	URLs        []string          `yaml:"urls"`
	Created     time.Time         `yaml:"created"`
	Digest      string            `yaml:"digest,omitempty"`
	Maintainers []ChartMaintainer `yaml:"maintainers,omitempty"`
	Keywords    []string          `yaml:"keywords,omitempty"`
	Icon        string            `yaml:"icon,omitempty"`
	Deprecated  bool              `yaml:"deprecated,omitempty"`
}

// HelmRepositoryIndex represents the structure of a Helm repository index.yaml file
type HelmRepositoryIndex struct {
	APIVersion string                      `yaml:"apiVersion"`
	Generated  time.Time                   `yaml:"generated"`
	Entries    map[string][]HelmIndexEntry `yaml:"entries"`
}

// ==================== Standard Helm Repository API ====================

// getRepositoryIndex handles GET /index.yaml
//
// Function: Provides the Helm repository index file, core interface for Helm clients to discover and download charts
//
// Request Headers:
//   - User-Agent: Helm client identification (optional)
//   - Accept: application/x-yaml, */* (optional)
//   - X-Market-User: User ID for authentication (required)
//   - X-Market-Source: Source system identifier (required)
//
// Response Format: YAML
// Response Headers:
//   - Content-Type: application/x-yaml
//   - Cache-Control: public, max-age=300 (5 minutes cache)
//   - Last-Modified: <timestamp>
//   - ETag: <hash>
//
// Response Body Structure (YAML format):
// apiVersion: v1
// generated: "2023-12-01T10:00:00Z"
// entries:
//
//	chart-name:
//	  - name: chart-name
//	    version: "1.0.0"
//	    appVersion: "2.0.0"
//	    description: "Chart description"
//	    home: "https://example.com"
//	    sources:
//	      - "https://github.com/example/chart"
//	    urls:
//	      - "https://repo.example.com/charts/chart-name-1.0.0.tgz"
//	    created: "2023-12-01T09:00:00Z"
//	    digest: "sha256:1234567890abcdef..."
//	    maintainers:
//	      - name: "Maintainer Name"
//	        email: "maintainer@example.com"
//	    keywords:
//	      - "web"
//	      - "server"
//	    icon: "https://example.com/icon.png"
//	    deprecated: false
//
// HTTP Status Codes:
//   - 200: Success - index file returned
//   - 304: Not Modified - client cache is up to date
//   - 400: Bad Request - missing required headers
//   - 401: Unauthorized - invalid user context
//   - 500: Internal Server Error - failed to generate index
//   - 503: Service Unavailable - repository temporarily unavailable
func (hr *HelmRepository) getRepositoryIndex(w http.ResponseWriter, r *http.Request) {
	// Extract user context from headers
	userCtx := extractUserContext(r)

	// Validate user context
	if !validateUserContext(userCtx) {
		http.Error(w, "Missing required headers: X-Market-User and X-Market-Source", http.StatusBadRequest)
		return
	}

	// Log user action for audit
	logUserAction(userCtx, "GET", "index.yaml")

	// Check if cache manager is available
	if globalCacheManager == nil {
		log.Printf("Helm Repository: Cache manager not available")
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	// Get user data from cache
	userData := globalCacheManager.GetUserData(userCtx.UserID)
	if userData == nil {
		log.Printf("Helm Repository: No data found for user %s", userCtx.UserID)
		// Return empty index for users with no data
		emptyIndex := &HelmRepositoryIndex{
			APIVersion: "v1",
			Generated:  time.Now(),
			Entries:    make(map[string][]HelmIndexEntry),
		}
		hr.writeYAMLResponse(w, emptyIndex)
		return
	}

	// Get source data from user data
	sourceData := userData.Sources[userCtx.Source]
	if sourceData == nil {
		log.Printf("Helm Repository: No source data found for user %s, source %s", userCtx.UserID, userCtx.Source)
		// Return empty index for sources with no data
		emptyIndex := &HelmRepositoryIndex{
			APIVersion: "v1",
			Generated:  time.Now(),
			Entries:    make(map[string][]HelmIndexEntry),
		}
		hr.writeYAMLResponse(w, emptyIndex)
		return
	}

	// Generate Helm repository index from AppInfoLatest data
	index, err := hr.generateHelmIndex(sourceData.AppInfoLatest, userCtx)
	if err != nil {
		log.Printf("Helm Repository: Failed to generate index for user %s, source %s: %v", userCtx.UserID, userCtx.Source, err)
		http.Error(w, "Failed to generate repository index", http.StatusInternalServerError)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Cache-Control", "public, max-age=300") // 5 minutes cache
	w.Header().Set("Last-Modified", index.Generated.Format(http.TimeFormat))

	// Generate ETag based on user and source
	etag := fmt.Sprintf(`"%s-%s-%d"`, userCtx.UserID, userCtx.Source, index.Generated.Unix())
	w.Header().Set("ETag", etag)

	// Check if client has cached version
	if clientETag := r.Header.Get("If-None-Match"); clientETag == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Write YAML response
	hr.writeYAMLResponse(w, index)

	log.Printf("Helm Repository: Generated index with %d chart entries for user %s, source %s",
		len(index.Entries), userCtx.UserID, userCtx.Source)
}

// generateHelmIndex generates a Helm repository index from AppInfoLatest data
func (hr *HelmRepository) generateHelmIndex(appInfoLatest []*types.AppInfoLatestData, userCtx *UserContext) (*HelmRepositoryIndex, error) {
	index := &HelmRepositoryIndex{
		APIVersion: "v1",
		Generated:  time.Now(),
		Entries:    make(map[string][]HelmIndexEntry),
	}

	// Convert each AppInfoLatestData to Helm chart entries
	for _, appData := range appInfoLatest {
		if appData == nil || appData.RawData == nil {
			continue
		}

		// Extract app information
		app := appData.RawData

		// Skip apps without essential information
		if app.Name == "" || app.Version == "" {
			log.Printf("Helm Repository: Skipping app with missing name or version: ID=%s, Name=%s, Version=%s",
				app.ID, app.Name, app.Version)
			continue
		}

		// Create Helm chart entry
		chartEntry := HelmIndexEntry{
			Name:       app.Name,
			Version:    app.Version,
			AppVersion: app.Version, // Use same version for app version
			Created:    time.Unix(appData.Timestamp, 0),
			URLs:       []string{fmt.Sprintf("/charts/%s-%s.tgz", app.Name, app.Version)},
		}

		// Add description
		if len(app.Description) > 0 {
			// Use English description if available, otherwise use first available
			if desc, ok := app.Description["en-US"]; ok && desc != "" {
				chartEntry.Description = desc
			} else {
				for _, desc := range app.Description {
					if desc != "" {
						chartEntry.Description = desc
						break
					}
				}
			}
		}

		// Add icon
		if app.Icon != "" {
			chartEntry.Icon = app.Icon
		}

		// Add home URL if available
		if app.Website != "" {
			chartEntry.Home = app.Website
		}

		// Add source code URL
		if app.SourceCode != "" {
			chartEntry.Sources = []string{app.SourceCode}
		}

		// Add maintainers if available
		if app.Developer != "" {
			chartEntry.Maintainers = []ChartMaintainer{
				{
					Name: app.Developer,
				},
			}
		}

		// Add keywords from categories
		if len(app.Categories) > 0 {
			chartEntry.Keywords = app.Categories
		}

		// Generate digest (simple hash based on app data)
		digestData := fmt.Sprintf("%s-%s-%d", app.Name, app.Version, appData.Timestamp)
		chartEntry.Digest = fmt.Sprintf("sha256:%x", []byte(digestData))

		// Add to index entries
		chartName := app.Name
		if _, exists := index.Entries[chartName]; !exists {
			index.Entries[chartName] = make([]HelmIndexEntry, 0)
		}
		index.Entries[chartName] = append(index.Entries[chartName], chartEntry)
	}

	return index, nil
}

// writeYAMLResponse writes a YAML response to the HTTP response writer
func (hr *HelmRepository) writeYAMLResponse(w http.ResponseWriter, data interface{}) {
	yamlData, err := yaml.Marshal(data)
	if err != nil {
		log.Printf("Helm Repository: Failed to marshal YAML: %v", err)
		http.Error(w, "Failed to generate YAML response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	w.WriteHeader(http.StatusOK)
	w.Write(yamlData)
}

// downloadChart handles GET /charts/{filename}.tgz
//
// Function: Downloads specific chart package files, supports Helm client chart installation
//
// URL Parameters:
//   - filename: Chart package filename (format: chartname-version.tgz)
//
// Request Headers:
//   - Range: bytes=start-end (optional, for resumable downloads)
//   - If-None-Match: <etag> (optional, for cache validation)
//   - If-Modified-Since: <timestamp> (optional, for cache validation)
//   - X-Market-User: User ID for authentication (required)
//   - X-Market-Source: Source system identifier (required)
//
// Response Format: Binary (gzipped tar archive)
// Response Headers:
//   - Content-Type: application/gzip
//   - Content-Length: <file-size>
//   - Content-Disposition: attachment; filename="chartname-version.tgz"
//   - ETag: <file-hash>
//   - Last-Modified: <file-timestamp>
//   - Accept-Ranges: bytes (for resumable downloads)
//
// HTTP Status Codes:
//   - 200: Success - chart package returned
//   - 206: Partial Content - range request fulfilled
//   - 304: Not Modified - client cache is up to date
//   - 400: Bad Request - invalid filename format or missing headers
//   - 401: Unauthorized - invalid user context
//   - 403: Forbidden - user doesn't have access to this chart
//   - 404: Not Found - chart package not found
//   - 416: Range Not Satisfiable - invalid range request
//   - 500: Internal Server Error - failed to read chart file
func (hr *HelmRepository) downloadChart(w http.ResponseWriter, r *http.Request) {
	log.Printf("Helm Repository: Received download request for chart: %s", r.URL.Path)

	// Extract user context from headers
	userCtx := extractUserContext(r)
	log.Printf("Helm Repository: User context - UserID: %s, Source: %s", userCtx.UserID, userCtx.Source)

	// Validate user context
	if !validateUserContext(userCtx) {
		log.Printf("Helm Repository: Invalid user context")
		http.Error(w, "Missing required headers: X-Market-User and X-Market-Source", http.StatusBadRequest)
		return
	}

	// Extract filename from URL parameters
	vars := mux.Vars(r)
	filename := vars["filename"]
	log.Printf("Helm Repository: Extracted filename: %s", filename)

	// Log user action for audit
	logUserAction(userCtx, "DOWNLOAD", filename)

	// Check if cache manager is available
	if globalCacheManager == nil {
		log.Printf("Helm Repository: Cache manager not available")
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	// Get user data from cache
	userData := globalCacheManager.GetUserData(userCtx.UserID)
	if userData == nil {
		log.Printf("Helm Repository: No data found for user %s", userCtx.UserID)
		http.Error(w, "Chart not found", http.StatusNotFound)
		return
	}
	log.Printf("Helm Repository: Found user data for user %s", userCtx.UserID)

	// Get source data from user data
	sourceData := userData.Sources[userCtx.Source]
	if sourceData == nil {
		log.Printf("Helm Repository: No source data found for user %s, source %s", userCtx.UserID, userCtx.Source)
		http.Error(w, "Chart not found", http.StatusNotFound)
		return
	}
	log.Printf("Helm Repository: Found source data for user %s, source %s", userCtx.UserID, userCtx.Source)

	// Parse filename to extract chart name and version
	parts := strings.Split(strings.TrimSuffix(filename, ".tgz"), "-")
	if len(parts) < 2 {
		log.Printf("Helm Repository: Invalid chart filename format: %s", filename)
		http.Error(w, "Invalid chart filename format", http.StatusBadRequest)
		return
	}
	version := parts[len(parts)-1]
	chartName := strings.Join(parts[:len(parts)-1], "-")
	log.Printf("Helm Repository: Parsed chart name: %s, version: %s", chartName, version)

	// Find the chart in AppInfoLatest
	var targetApp *types.AppInfoLatestData
	for _, app := range sourceData.AppInfoLatest {
		if app != nil && app.RawData != nil {
			log.Printf("Helm Repository: Checking app - Name: %s, Version: %s", app.RawData.Name, app.RawData.Version)
			if app.RawData.Name == chartName && app.RawData.Version == version {
				targetApp = app
				break
			}
		}
	}

	if targetApp == nil {
		log.Printf("Helm Repository: Chart not found: %s", filename)
		http.Error(w, "Chart not found", http.StatusNotFound)
		return
	}
	log.Printf("Helm Repository: Found target app in AppInfoLatest")

	// Get chart package path from RenderedPackage
	chartDir := targetApp.RenderedPackage
	if chartDir == "" {
		log.Printf("Helm Repository: Chart package not found for %s", filename)
		http.Error(w, "Chart package not found", http.StatusNotFound)
		return
	}
	log.Printf("Helm Repository: Chart directory path: %s", chartDir)

	chartPath := filepath.Join(chartDir, filename)
	log.Printf("Helm Repository: Full chart package path: %s", chartPath)

	// Check if file exists
	fileInfo, err := os.Stat(chartPath)
	if err != nil {
		log.Printf("Helm Repository: Failed to access chart file %s: %v", chartPath, err)
		http.Error(w, "Failed to access chart file", http.StatusInternalServerError)
		return
	}

	if fileInfo.IsDir() {
		log.Printf("Helm Repository: Path is a directory, not a file: %s", chartPath)
		http.Error(w, "Invalid chart package", http.StatusInternalServerError)
		return
	}

	log.Printf("Helm Repository: Chart file exists, size: %d bytes", fileInfo.Size())

	// Handle range requests
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		// TODO: Implement range request handling
		log.Printf("Helm Repository: Range request not implemented yet")
	}

	f, err := os.Open(chartPath)
	if err != nil {
		log.Printf("Helm Repository: Failed to open chart file %s: %v", chartPath, err)
		http.Error(w, "Failed to open chart file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Last-Modified", fileInfo.ModTime().Format(http.TimeFormat))
	w.Header().Set("Accept-Ranges", "bytes")
	etag := fmt.Sprintf(`"%s-%d"`, filename, fileInfo.ModTime().Unix())
	w.Header().Set("ETag", etag)
	if clientETag := r.Header.Get("If-None-Match"); clientETag == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	log.Printf("Helm Repository: Starting to stream file content")
	_, err = io.Copy(w, f)
	if err != nil {
		log.Printf("Helm Repository: Error streaming file: %v", err)
		return
	}

	log.Printf("Helm Repository: Successfully served chart %s to user %s", filename, userCtx.UserID)
}
