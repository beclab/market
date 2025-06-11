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

// calculateLocalHash computes hash from local SourceData Others.Hash
// calculateLocalHash 从本地SourceData Others.Hash获取hash
func (h *HashComparisonStep) calculateLocalHash(cache *types.CacheData) string {
	if cache == nil {
		log.Printf("Cache is nil, returning empty_cache hash")
		return "empty_cache"
	}

	cache.Mutex.RLock()
	defer cache.Mutex.RUnlock()

	// If no users exist, return empty hash
	// 如果没有用户存在，返回空hash
	if len(cache.Users) == 0 {
		log.Printf("No users in cache, returning empty hash")
		return "empty_cache_no_users"
	}

	// Collect all Others.Hash values from all sources across all users
	// 收集所有用户所有源的Others.Hash值
	var othersHashes []string
	var foundValidHash bool

	for userID, userData := range cache.Users {
		userData.Mutex.RLock()

		for sourceID, sourceData := range userData.Sources {
			sourceData.Mutex.RLock()

			// Check if Others exists and has a Hash
			// 检查Others是否存在并包含Hash
			if sourceData.Others != nil && sourceData.Others.Hash != "" {
				othersHashes = append(othersHashes, sourceData.Others.Hash)
				foundValidHash = true
				log.Printf("Found Others.Hash for user:%s source:%s hash:%s", userID, sourceID, sourceData.Others.Hash)
			} else {
				log.Printf("No valid Others.Hash for user:%s source:%s (Others: %v)", userID, sourceID, sourceData.Others)
			}

			sourceData.Mutex.RUnlock()
		}

		userData.Mutex.RUnlock()
	}

	// If no valid Others.Hash found, return appropriate hash
	// 如果没有找到有效的Others.Hash，返回相应的hash
	if !foundValidHash {
		log.Printf("No valid Others.Hash found in any source, returning no_others_hash")
		return "no_others_hash"
	}

	// If only one hash found, return it directly
	// 如果只找到一个hash，直接返回
	if len(othersHashes) == 1 {
		localHash := othersHashes[0]
		log.Printf("Using single Others.Hash as local hash: %s", localHash)
		return localHash
	}

	// If multiple hashes found, create a combined hash
	// 如果找到多个hash，创建组合hash
	var combinedContent string
	for i, hash := range othersHashes {
		combinedContent += fmt.Sprintf("hash_%d:%s", i, hash)
	}

	// Calculate MD5 hash of combined hashes
	// 计算组合hash的MD5值
	hasher := md5.New()
	hasher.Write([]byte(combinedContent))
	localHash := fmt.Sprintf("%x", hasher.Sum(nil))

	log.Printf("Combined multiple Others.Hash values into local hash: %s (from hashes: %v)", localHash, othersHashes)
	return localHash
}
