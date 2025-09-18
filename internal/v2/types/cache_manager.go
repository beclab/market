package types

import "context"

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

	// TryLockWithContext attempts to acquire the write lock with context cancellation
	// Returns true if lock was acquired, false if context was cancelled or timeout occurred
	TryLockWithContext(ctx context.Context) bool

	// TryRLockWithContext attempts to acquire the read lock with context cancellation
	// Returns true if lock was acquired, false if context was cancelled or timeout occurred
	TryRLockWithContext(ctx context.Context) bool

	// GetCache returns the underlying cache data
	GetCache() *CacheData
}
