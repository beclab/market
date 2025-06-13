package api

import (
	"encoding/json"
	"log"
	"net/http"

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
