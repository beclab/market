package syncerfn

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"

	"market/internal/v2/settings"
	"market/internal/v2/types"
)

// HashResponse represents the response structure from the remote hash API
// HashResponse 表示远程hash API的响应结构
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
	// 从SyncContext获取版本，如果不可用则使用默认值
	version := data.GetVersion()
	if version == "" {
		version = "1.12.0" // fallback version
		log.Printf("No version provided in context, using default: %s", version)
	}

	log.Printf("Using version: %s", version)

	// Get current market source from context
	// 从上下文获取当前市场源
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		return fmt.Errorf("no market source available in sync context")
	}

	// Build complete URL from market source base URL and endpoint path
	// 从市场源基础URL和端点路径构建完整URL
	hashURL := h.SettingsManager.BuildAPIURL(marketSource.BaseURL, h.HashEndpointPath)
	log.Printf("Using hash URL: %s", hashURL)

	// Fetch remote hash with version parameter
	// 携带version参数获取远程hash
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
	// 解析JSON响应
	var hashResponse HashResponse
	if err := json.Unmarshal(resp.Body(), &hashResponse); err != nil {
		return fmt.Errorf("failed to parse hash response: %w", err)
	}

	data.RemoteHash = hashResponse.Hash

	// Calculate local hash
	// 计算本地hash
	data.LocalHash = h.calculateLocalHash(data.Cache)

	// Compare hashes and set result
	// 比较hash并设置结果
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

// calculateLocalHash computes hash of local cache data
// calculateLocalHash 计算本地缓存数据的hash
func (h *HashComparisonStep) calculateLocalHash(cache *types.CacheData) string {
	if cache == nil {
		return "empty_cache"
	}

	// Create a simple hash based on cache content
	// 基于缓存内容创建简单的hash
	cache.Mutex.RLock()
	defer cache.Mutex.RUnlock()

	// If no users exist, return empty hash
	// 如果没有用户存在，返回空hash
	if len(cache.Users) == 0 {
		log.Printf("No users in cache, returning empty hash")
		return "empty_cache_no_users"
	}

	// Generate hash based on actual cache content instead of timestamp
	// 基于实际缓存内容而不是时间戳生成hash
	var contentParts []string

	// Add user count
	contentParts = append(contentParts, fmt.Sprintf("users:%d", len(cache.Users)))

	// Add source information for each user
	for userID, userData := range cache.Users {
		userData.Mutex.RLock()
		sourceCount := len(userData.Sources)
		contentParts = append(contentParts, fmt.Sprintf("user:%s:sources:%d", userID, sourceCount))

		// Add information about each source's data
		for sourceID, sourceData := range userData.Sources {
			sourceData.Mutex.RLock()

			// Check if we have any actual app data
			hasLatest := len(sourceData.AppInfoLatest) > 0
			hasPending := len(sourceData.AppInfoLatestPending) > 0
			hasHistory := len(sourceData.AppInfoHistory) > 0
			hasState := len(sourceData.AppStateLatest) > 0
			hasOther := len(sourceData.Other) > 0

			contentParts = append(contentParts, fmt.Sprintf("source:%s:latest:%t:pending:%t:history:%t:state:%t:other:%t",
				sourceID, hasLatest, hasPending, hasHistory, hasState, hasOther))

			// If we have pending data, include its version in hash
			if hasPending && len(sourceData.AppInfoLatestPending) > 0 {
				for _, pendingData := range sourceData.AppInfoLatestPending {
					// Use the version from the pending data structure itself or from RawData
					if pendingData.Version != "" {
						contentParts = append(contentParts, fmt.Sprintf("pending_version:%s", pendingData.Version))
					} else if pendingData.RawData != nil && pendingData.RawData.Version != "" {
						contentParts = append(contentParts, fmt.Sprintf("pending_version:%s", pendingData.RawData.Version))
					}
				}
			}

			sourceData.Mutex.RUnlock()
		}
		userData.Mutex.RUnlock()
	}

	// Join all parts and calculate MD5 hash
	content := fmt.Sprintf("%s", contentParts)

	// Calculate MD5 hash
	// 计算MD5 hash
	hasher := md5.New()
	hasher.Write([]byte(content))
	localHash := fmt.Sprintf("%x", hasher.Sum(nil))

	log.Printf("Calculated local hash: %s (based on content: %v)", localHash, contentParts)
	return localHash
}
