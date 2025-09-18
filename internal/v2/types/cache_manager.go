package types

// CacheManagerInterface defines the interface for cache management operations
// This interface is used to avoid circular imports between packages
type CacheManagerInterface interface {
	// Lock acquires the cache manager's write lock
	Lock()

	// Unlock releases the cache manager's write lock
	Unlock()

	// RLock acquires the cache manager's read lock
	RLock()

	// RUnlock releases the cache manager's read lock
	RUnlock()

	// GetCache returns the underlying cache data
	GetCache() *CacheData
}
