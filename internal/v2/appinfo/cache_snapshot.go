package appinfo

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"market/internal/v2/types"

	"github.com/golang/glog"
)

// userEntry holds an immutable snapshot of a user's data behind an atomic pointer.
// Readers load the snapshot without any lock; writers deep-copy, mutate, and swap.
type userEntry struct {
	snapshot atomic.Pointer[types.UserData]
}

// userSnapshots maps userID -> *userEntry. sync.Map is chosen because reads
// vastly outnumber writes and keys (user IDs) are stable.
var _ sync.Map // type assertion only – field lives on CacheManager

// deepCopyUserData produces a fully independent clone of a UserData tree.
// It uses JSON round-trip which is proven correct for all cache types
// (the same serialization path is used for Redis persistence).
func deepCopyUserData(ud *types.UserData) *types.UserData {
	if ud == nil {
		return nil
	}
	data, err := json.Marshal(ud)
	if err != nil {
		glog.Errorf("deepCopyUserData: marshal failed: %v", err)
		return nil
	}
	var cp types.UserData
	if err := json.Unmarshal(data, &cp); err != nil {
		glog.Errorf("deepCopyUserData: unmarshal failed: %v", err)
		return nil
	}
	return &cp
}

// publishUserSnapshotLocked deep-copies the mutable cache entry for userID and
// stores it in the atomic snapshot. MUST be called while holding cm.mutex (read
// or write) so that the source data is consistent during the copy.
func (cm *CacheManager) publishUserSnapshotLocked(userID string) {
	userData := cm.cache.Users[userID]
	snapshot := deepCopyUserData(userData)

	if v, ok := cm.userSnapshots.Load(userID); ok {
		v.(*userEntry).snapshot.Store(snapshot)
	} else if snapshot != nil {
		e := &userEntry{}
		e.snapshot.Store(snapshot)
		cm.userSnapshots.Store(userID, e)
	}
}

// publishAllSnapshotsLocked publishes snapshots for every user currently in the
// mutable cache. Called during Start / bulk operations.
func (cm *CacheManager) publishAllSnapshotsLocked() {
	for userID := range cm.cache.Users {
		cm.publishUserSnapshotLocked(userID)
	}
	// Remove snapshots for users that no longer exist in the mutable cache.
	cm.userSnapshots.Range(func(key, _ any) bool {
		if _, exists := cm.cache.Users[key.(string)]; !exists {
			cm.userSnapshots.Delete(key)
		}
		return true
	})
}

// removeUserSnapshot deletes the snapshot for a removed user.
func (cm *CacheManager) removeUserSnapshot(userID string) {
	cm.userSnapshots.Delete(userID)
}
