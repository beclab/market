# Cache Architecture: Per-User Immutable Snapshot (RCU)

## Problem

A single global `RWMutex` (`cm.mutex`) protects the entire cache tree. This causes:
- Deadlocks (lock ordering conflicts between `cm.mutex` and `settingsManager.mu`)
- Data races (pointers escape the lock via `GetUserData`)
- Contention (all readers blocked during any write, even for different users)
- Long lock hold times (NATS publish, HTTP I/O, settings queries under write lock)

## Solution

Replace the global `RWMutex` with per-user `atomic.Pointer[UserData]` snapshots.

**Reads**: Lock-free via `atomic.Pointer.Load()`. Returns an immutable snapshot safe to use indefinitely.

**Writes**: Modify mutable internal state under `cm.mutex`, then publish an immutable snapshot via `publishUserSnapshot()`.

## Data Flow

```
Writers (Pipeline, NATS, HTTP)
    │
    ▼
cm.mutex (write lock) ─── cm.cache (mutable internal state)
    │
    ▼
publishUserSnapshot() ─── atomic.Pointer.Store(deepCopy)
    │
    ▼
Readers (HTTP API, Collector, SCC) ─── atomic.Pointer.Load() [NO LOCK]
```

## Migration Phases

### Phase 1 (this PR): Dual-path reads
- Add `userEntry` with `atomic.Pointer[UserData]`
- `GetUserData` / `GetAllUsersData` / `GetSourceData` read from atomic pointer (lock-free)
- Writers still use `cm.mutex`, then call `publishUserSnapshot` to update the atomic pointer
- Internal methods (`getUserData`, `getSourceData`) still access `cm.cache` directly under lock

### Phase 2 (future): Per-user write isolation
- Replace `cm.mutex` with per-user mutexes for write serialization
- Different users' writes become fully parallel

### Phase 3 (future): Remove global lock
- When all write paths use per-user locks, remove `cm.mutex` entirely
