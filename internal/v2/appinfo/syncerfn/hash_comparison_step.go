package syncerfn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"market/internal/v2/settings"
	"market/internal/v2/store/marketsource"

	"github.com/golang/glog"
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
	glog.V(2).Infof("Executing %s", h.GetStepName())

	// Get version from SyncContext, if not available use default
	version := data.GetVersion()
	if version == "" {
		version = "1.12.3" // fallback version
		glog.V(2).Infof("No version provided in context, using default: %s", version)
	}

	glog.V(2).Infof("Using version: %s", version)

	// Get current market source from context
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		return fmt.Errorf("no market source available in sync context")
	}

	// Build complete URL from market source base URL and endpoint path
	hashURL := h.SettingsManager.BuildAPIURL(marketSource.BaseURL, h.HashEndpointPath)
	glog.V(2).Infof("Using hash, Source: %s, Version: %s, URL: %s", marketSource.ID, version, hashURL)

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

	// Calculate local hash from PostgreSQL (market_sources.data.hash).
	localHash, err := marketsource.GetOthersHash(ctx, marketSource.ID)
	if err != nil {
		return fmt.Errorf("failed to load local hash for source %s: %w", marketSource.ID, err)
	}
	data.LocalHash = localHash

	// Backward compatibility: when store is unavailable, fall back to cache hash.
	if data.LocalHash == "" && data.CacheManager != nil {
		data.LocalHash = data.CacheManager.GetSourceOthersHash(marketSource.ID)
	}
	if data.LocalHash == "" {
		if data.Cache == nil || len(data.Cache.Users) == 0 {
			data.LocalHash = "empty_cache_no_users"
		} else {
			data.LocalHash = "no_source_hash"
		}
	}

	// Compare hashes and set result
	data.HashMatches = data.RemoteHash == data.LocalHash

	glog.V(2).Infof("Remote hash: %s, Local hash: %s, Match: %t",
		data.RemoteHash, data.LocalHash, data.HashMatches)

	if data.HashMatches {
		glog.Infof("Hashes match - subsequent steps may be skipped")
	} else {
		glog.Info("Hashes differ - sync is required")
	}

	return nil
}

// CanSkip determines if this step can be skipped
func (h *HashComparisonStep) CanSkip(ctx context.Context, data *SyncContext) bool {
	return false // Always execute hash comparison
}
