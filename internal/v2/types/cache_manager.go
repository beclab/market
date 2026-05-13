package types

// CacheManagerInterface defines the interface for cache management operations
// This interface is used to avoid circular imports between packages
type CacheManagerInterface interface {
	// Hierarchical read accessors
	GetAllUsersData() map[string]*UserData
	GetUserData(userID string) *UserData
	GetUserIDs() []string

	// Specific read queries
	HasSourceData(sourceID string) bool
	IsAppInstalled(sourceID, appName string) bool
	GetSourceOthersHash(sourceID string) string
	FindPendingDataForApp(userID, sourceID, appID string) *AppInfoLatestPendingData

	// Write operations
	UpdateSourceOthers(sourceID string, others *Others)
	RemoveAppFromAllSources(appName, sourceID string) int
}
