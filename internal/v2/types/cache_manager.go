package types

// CacheManagerInterface defines the interface for cache management operations
// This interface is used to avoid circular imports between packages
type CacheManagerInterface interface {
	// Lock acquires the cache manager's write lock
	Lock()

	// Unlock releases the cache manager's write lock
	Unlock()

	// TryLock attempts to acquire the cache manager's write lock without blocking
	// Returns true if lock acquired, false if would block
	TryLock() bool

	// RLock acquires the cache manager's read lock
	RLock()

	// RUnlock releases the cache manager's read lock
	RUnlock()

	// TryRLock attempts to acquire the cache manager's read lock without blocking
	// Returns true if lock acquired, false if would block
	TryRLock() bool

	// GetCache returns the underlying cache data
	GetCache() *CacheData
}
