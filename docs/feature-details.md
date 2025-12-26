# Feature Details Documentation

[中文](./feature-details.zh-CN.md)

## Overview

This document records implementation details, bug fixes, and optimizations for various feature modules in the Market system.

## Pending State Message Delayed Processing Mechanism

### Problem Description

In the `datawatcher_state` module, when receiving status change push notifications from app-service, the system needs to find the matching source information through a set of logic. There exists a race condition issue:

If the first message of an install operation is in `pending` state, and this message is sent and received during the install API request, the found source might be from the previous installation instead of the current one.

**Root Cause Analysis**:
1. `pending` state is not in the failed/canceled state list, so it participates in cache lookup
2. Cache lookup logic sorts by `statusTime` in descending order and selects the newest record
3. When a new installation starts, the task might still be in the `pendingTasks` queue, not yet executed to `AppInstall`, so there's no `OpID` yet
4. If the previous installation's `pending` state has a `statusTime` newer than or equal to the current message's `CreateTime`, cache lookup will match to the previous source
5. Fallback mechanisms (via `OpID` or `appName+user` lookup) may also fail, as the new task might not have an `OpID` yet, or `GetLatestTaskByAppNameAndUser` might return the previous task

### Solution

Implemented a delayed processing mechanism. For `pending` state messages, first check if there are unfinished install tasks, and if so, delay processing:

**Implementation Location**:
- `internal/v2/appinfo/datawatcher_state.go`
- `internal/v2/task/taskmodule.go`

**Key Changes**:

1. **New Method in TaskModule**:
   - `HasPendingOrRunningInstallTask(appName, user string) (hasTask bool, lockAcquired bool)`
   - Uses non-blocking `TryRLock` to check for unfinished install/clone tasks
   - Returns two values: whether there are tasks, and whether the lock was successfully acquired

2. **Delayed Processing Mechanism in DataWatcherState**:
   - Added `DelayedMessage` struct to store delayed messages
   - Added `delayedMessages` queue and background processing goroutine
   - `processDelayedMessages()` checks the delayed queue every second
   - `processDelayedMessagesBatch()` processes expired delayed messages

3. **Special Handling in handleMessage**:
   - For `pending` state install operation messages, first check if there are unfinished install tasks
   - If unable to acquire lock, delay processing (to avoid blocking NATS message processing)
   - If there are unfinished tasks, delay message processing
   - Delayed messages retry every 2 seconds, up to 10 times (about 20 seconds total)

