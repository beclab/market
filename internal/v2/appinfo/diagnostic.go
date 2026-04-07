package appinfo

import (
	"strings"

	"github.com/golang/glog"
)

// DiagnosticInfo contains diagnostic information about cache and Redis state
type DiagnosticInfo struct {
	RedisKeys       []string                   `json:"redis_keys"`
	CacheUsers      map[string]*UserData       `json:"cache_users"`
	SourceAnalysis  map[string]*SourceAnalysis `json:"source_analysis"`
	Issues          []string                   `json:"issues"`
	Recommendations []string                   `json:"recommendations"`
}

// SourceAnalysis contains analysis of a specific source
type SourceAnalysis struct {
	SourceID                string   `json:"source_id"`
	HasAppInfoLatest        bool     `json:"has_app_info_latest"`
	HasAppInfoLatestPending bool     `json:"has_app_info_latest_pending"`
	HasAppStateLatest       bool     `json:"has_app_state_latest"`
	HasAppInfoHistory       bool     `json:"has_app_info_history"`
	AppInfoLatestCount      int      `json:"app_info_latest_count"`
	AppInfoPendingCount     int      `json:"app_info_pending_count"`
	AppStateLatestCount     int      `json:"app_state_latest_count"`
	AppInfoHistoryCount     int      `json:"app_info_history_count"`
	Issues                  []string `json:"issues"`
}

// DiagnoseCacheAndRedis performs diagnosis of cache and Redis state
func (cm *CacheManager) DiagnoseCacheAndRedis() error {
	glog.Infof("=== CACHE AND REDIS DIAGNOSTIC REPORT ===")

	// Get Redis keys
	redisKeys, err := cm.redisClient.client.Keys(cm.redisClient.ctx, "appinfo:*").Result()
	if err != nil {
		glog.Errorf("Failed to get Redis keys: %v", err)
		return err
	}

	glog.Infof("Redis Keys Found: %d", len(redisKeys))

	// Analyze cache state
	cm.mutex.RLock()
	userCount := len(cm.cache.Users)
	totalSources := 0
	issues := 0

	for userID, userData := range cm.cache.Users {
		glog.Infof("User: %s", userID)
		glog.Infof("  Hash: %s", userData.Hash)
		glog.Infof("  Sources: %d", len(userData.Sources))

		for sourceID, sourceData := range userData.Sources {
			totalSources++
			glog.Infof("    Source: %s", sourceID)
			glog.Infof("      AppInfoLatest: %d", len(sourceData.AppInfoLatest))
			glog.Infof("      AppInfoPending: %d", len(sourceData.AppInfoLatestPending))
			glog.Infof("      AppStateLatest: %d", len(sourceData.AppStateLatest))
			glog.Infof("      AppInfoHistory: %d", len(sourceData.AppInfoHistory))

			// Check for issues
			if strings.Contains(sourceID, ":") {
				glog.Warningf("      ISSUE: Source ID contains colons: %s", sourceID)
				issues++
			}

			if len(sourceData.AppInfoLatest) == 0 && len(sourceData.AppInfoLatestPending) > 0 {
				glog.Warningf("      ISSUE: Has pending data but no latest data")
				issues++
			}
		}
	}
	cm.mutex.RUnlock()

	glog.Infof("Total Users: %d", userCount)
	glog.Infof("Total Sources: %d", totalSources)
	glog.Infof("Issues Found: %d", issues)

	if issues > 0 {
		glog.Warningf("Recommendations:")
		glog.Warningf("  1. Run CleanupInvalidPendingData() to remove invalid entries")
		glog.Warningf("  2. Check hydration process for any failures")
		glog.Warningf("  3. Verify Redis connection and data integrity")
	}

	return nil
}

// ForceReloadFromRedis forces a complete reload of cache data from Redis
func (cm *CacheManager) ForceReloadFromRedis() error {
	glog.Infof("Force reloading cache data from Redis")

	cache, err := cm.redisClient.LoadCacheFromRedis()
	if err != nil {
		glog.Errorf("Failed to load cache from Redis: %v", err)
		return err
	}

	cm.mutex.Lock()
	cm.cache = cache
	cm.mutex.Unlock()

	glog.Infof("Successfully reloaded cache data from Redis")
	return nil
}
