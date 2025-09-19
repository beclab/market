package syncerfn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"market/internal/v2/settings"
	"market/internal/v2/types"
)

// HashResponse represents the response structure from the remote hash API
type HashResponse struct {
	Hash        string `json:"hash"`
	LastUpdated string `json:"last_updated"`
	Version     string `json:"version"`
}

// HashComparisonStep implements the first step: compare remote and local hash
type HashComparisonStep struct {
	HashEndpointPath string                    // Relative path like "/api/v1/appstore/hash"
	SettingsManager  *settings.SettingsManager // Settings manager to build complete URLs
}

// NewHashComparisonStep creates a new hash comparison step
func NewHashComparisonStep(hashEndpointPath string, settingsManager *settings.SettingsManager) *HashComparisonStep {
	return &HashComparisonStep{
		HashEndpointPath: hashEndpointPath,
		SettingsManager:  settingsManager,
	}
}

// GetStepName returns the name of this step
func (h *HashComparisonStep) GetStepName() string {
	return "Hash Comparison Step"
}

// Execute performs the hash comparison logic
func (h *HashComparisonStep) Execute(ctx context.Context, data *SyncContext) error {
	log.Printf("Executing %s", h.GetStepName())

	// Get version from SyncContext, if not available use default
	version := data.GetVersion()
	if version == "" {
		version = "1.12.0" // fallback version
		log.Printf("No version provided in context, using default: %s", version)
	}

	log.Printf("Using version: %s", version)

	// Get current market source from context
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		return fmt.Errorf("no market source available in sync context")
	}

	// Build complete URL from market source base URL and endpoint path
	hashURL := h.SettingsManager.BuildAPIURL(marketSource.BaseURL, h.HashEndpointPath)
	log.Printf("Using hash URL: %s", hashURL)

	if strings.HasPrefix(hashURL, "file://") {
		return nil
	}

	// Fetch remote hash with version parameter
	resp, err := data.Client.R().
		SetContext(ctx).
		SetQueryParam("version", version).
		Get(hashURL)

	if err != nil {
		return fmt.Errorf("failed to fetch remote hash from %s: %w", hashURL, err)
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("remote hash API returned status %d from %s", resp.StatusCode(), hashURL)
	}

	// Parse JSON response
	var hashResponse HashResponse
	if err := json.Unmarshal(resp.Body(), &hashResponse); err != nil {
		return fmt.Errorf("failed to parse hash response: %w", err)
	}

	data.RemoteHash = hashResponse.Hash

	// Calculate local hash with proper locking
	if data.CacheManager != nil {
		data.CacheManager.RLock()
		data.LocalHash = h.calculateLocalHash(data.Cache, data.GetMarketSource())
		data.CacheManager.RUnlock()
	}

	// Compare hashes and set result
	data.HashMatches = data.RemoteHash == data.LocalHash

	log.Printf("Remote hash: %s, Local hash: %s, Match: %t",
		data.RemoteHash, data.LocalHash, data.HashMatches)

	if data.HashMatches {
		log.Printf("Hashes match - subsequent steps may be skipped")
	} else {
		log.Printf("Hashes differ - sync is required")
	}

	return nil
}

// CanSkip determines if this step can be skipped
func (h *HashComparisonStep) CanSkip(ctx context.Context, data *SyncContext) bool {
	return false // Always execute hash comparison
}

// calculateLocalHash computes hash from local SourceData Others.Hash for specific market source
func (h *HashComparisonStep) calculateLocalHash(cache *types.CacheData, marketSource *settings.MarketSource) string {
	if cache == nil {
		log.Printf("Cache is nil, returning empty_cache hash")
		return "empty_cache"
	}

	if marketSource == nil {
		log.Printf("MarketSource is nil, returning no_market_source hash")
		return "no_market_source"
	}

	// Use market source name as source ID to match syncer.go behavior
	sourceID := marketSource.ID

	// Note: This function is called from SyncContext with proper locking

	// If no users exist, return empty hash
	if len(cache.Users) == 0 {
		log.Printf("No users in cache, returning empty hash")
		return "empty_cache_no_users"
	}

	// Look for Others.Hash only in the current market source
	var sourceHash string
	var foundValidHash bool

	for userID, userData := range cache.Users {
		// Check if this user has data for the specific source
		if sourceData, exists := userData.Sources[sourceID]; exists {
			// Check if Others exists and has a Hash
			if sourceData.Others != nil && sourceData.Others.Hash != "" {
				sourceHash = sourceData.Others.Hash
				foundValidHash = true
				log.Printf("Found Others.Hash for user:%s source:%s hash:%s", userID, sourceID, sourceHash)
				break // Use the first valid hash found
			} else {
				log.Printf("No valid Others.Hash for user:%s source:%s (Others: %v)", userID, sourceID, sourceData.Others)
			}
		} else {
			log.Printf("No data found for user:%s source:%s", userID, sourceID)
		}
	}

	// If no valid Others.Hash found for the specific source, return appropriate hash
	if !foundValidHash {
		log.Printf("No valid Others.Hash found for source:%s, returning no_source_hash", sourceID)
		return "no_source_hash"
	}

	log.Printf("Using Others.Hash from source:%s as local hash: %s", sourceID, sourceHash)
	return sourceHash
}
