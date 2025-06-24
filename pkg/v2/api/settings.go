package api

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"market/internal/v2/settings"
)

// settingsManager holds the global settings manager instance
var settingsManager *settings.SettingsManager

// SetSettingsManager sets the global settings manager
func SetSettingsManager(sm *settings.SettingsManager) {
	settingsManager = sm
}

// MarketSourceRequest represents the request body for setting market source
type MarketSourceRequest struct {
	URL string `json:"url"`
}

// SystemStatusResponse
// Aggregated system status response
type SystemStatusResponse struct {
	UserInfo        interface{} `json:"user_info"`
	CurUserResource interface{} `json:"cur_user_resource"`
	ClusterResource interface{} `json:"cluster_resource"`
	TerminusVersion interface{} `json:"terminus_version"`
}

// getMarketSource handles GET /api/v2/settings/market-source
func (s *Server) getMarketSource(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/settings/market-source - Getting market source configuration")

	if settingsManager == nil {
		log.Println("Settings manager not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Settings manager not initialized", nil)
		return
	}

	config := settingsManager.GetMarketSource()
	if config == nil {
		log.Println("Market source configuration not found")
		s.sendResponse(w, http.StatusNotFound, false, "Market source configuration not found", nil)
		return
	}

	log.Printf("Market source configuration retrieved: %s", config.BaseURL)
	s.sendResponse(w, http.StatusOK, true, "Market source configuration retrieved successfully", config)
}

// setMarketSource handles PUT /api/v2/settings/market-source
func (s *Server) setMarketSource(w http.ResponseWriter, r *http.Request) {
	log.Println("PUT /api/v2/settings/market-source - Setting market source configuration")

	if settingsManager == nil {
		log.Println("Settings manager not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Settings manager not initialized", nil)
		return
	}

	var req MarketSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	if req.URL == "" {
		log.Println("Market source URL is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "Market source URL cannot be empty", nil)
		return
	}

	if err := settingsManager.SetMarketSource(req.URL); err != nil {
		log.Printf("Failed to set market source: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to set market source configuration", nil)
		return
	}

	// Get the updated configuration to return
	config := settingsManager.GetMarketSource()

	log.Printf("Market source configuration updated to: %s", req.URL)
	s.sendResponse(w, http.StatusOK, true, "Market source configuration updated successfully", config)
}

// getSystemStatus handles GET /api/v2/settings/system-status
// Aggregates system status from multiple sources
func (s *Server) getSystemStatus(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/settings/system-status - Aggregating system status")

	if settingsManager == nil {
		log.Println("Settings manager not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Settings manager not initialized", nil)
		return
	}

	appServiceHost := "127.0.0.1"
	appServicePort := "8080"
	if host := getenv("APP_SERVICE_SERVICE_HOST"); host != "" {
		appServiceHost = host
	}
	if port := getenv("APP_SERVICE_SERVICE_PORT"); port != "" {
		appServicePort = port
	}

	token := r.Header.Get("X-Authorization")

	userInfoURL := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/user-info"
	curUserResourceURL := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/user/resource"
	clusterResourceURL := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/cluster/resource"
	terminusVersionURL := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/terminus/version"

	userInfo, _ := doGetWithToken(userInfoURL, token)
	curUserResource, _ := doGetWithToken(curUserResourceURL, token)
	clusterResource, _ := doGetWithToken(clusterResourceURL, token)
	terminusVersion, _ := doGetWithToken(terminusVersionURL, token)

	resp := SystemStatusResponse{
		UserInfo:        userInfo,
		CurUserResource: curUserResource,
		ClusterResource: clusterResource,
		TerminusVersion: terminusVersion,
	}

	log.Println("System status aggregation complete")
	s.sendResponse(w, http.StatusOK, true, "System status aggregation complete", resp)
}

// Simple GET with token, returns parsed JSON or raw string
func doGetWithToken(url, token string) (interface{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("X-Authorization", token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func getenv(key string) string {
	return os.Getenv(key)
}
