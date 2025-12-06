## 1. Module Overview

The `runtime` module is responsible for **aggregating and exposing runtime information of the market system**, including:
- **Application Runtime Status / Lifecycle**: For each user and each source, the current state of applications (running, installing, upgrading, uninstalling, failed/stopped) and their corresponding health status.
- **Task Execution Status**: Current status and progress of background tasks such as installation, uninstallation, upgrade, cloning, etc. (more focused on the "operation process" itself).
- **System Component Status**: Health checks and metrics of sub-components such as the `appinfo` module, Syncer, Hydrator, task module, cache, etc.
- **Chart Repo Status**: Application processing status, image download/analysis status, and related task information collected from the Chart Repo service.

> Core goal: **Provide a unified Runtime view** that allows frontend and operations/monitoring systems to obtain the current runtime health status and real-time snapshots of key processes through simple APIs.
>
> Main code entry points:
>- `internal/v2/runtime/types.go` (data structure definitions)
>- `internal/v2/runtime/store.go` (in-memory state storage)
>- `internal/v2/runtime/collector.go` (state collection)
>- `internal/v2/runtime/service.go` (external service interface)


## 2. Operation Mode and Internal Flow

The `runtime` module adopts a three-layer architecture of "collection-storage-query": `StateCollector` is responsible for periodically collecting status from various business modules, `StateStore` is responsible for maintaining a unified runtime snapshot in memory, and `RuntimeStateService` is responsible for providing query interfaces externally. After the module starts, the collector refreshes the storage at fixed intervals (default 5 seconds), ensuring that the data returned by query interfaces always reflects the latest runtime status of the system.

### 2.1 Module Lifecycle

During module initialization, `StateStore` is first created as the in-memory storage center, then `StateCollector` is created and references to `TaskModule` and `AppInfoModule` are injected. After calling `RuntimeStateService.Start()`, the collector immediately performs a full collection, then starts a background goroutine that performs periodic collection every 5 seconds. During collection, the collector pulls the latest status from the task module, application information module, and Chart Repo service, converts it to a unified data model, and writes it to `StateStore`. Query interfaces access `StateStore` through `RuntimeStateService`, returning the current snapshot or filtered results based on conditions.

### 2.2 In-Memory State Storage `StateStore`

`StateStore` is the unified runtime storage within the process, maintaining the following in-memory mappings:

- **Application State Mapping** (`appStates`): Key is `userID:sourceID:appName`, value is `AppFlowState`, recording the runtime stage, health status, version, and other information of each application under the current user and source.
- **Task State Mapping** (`tasks`): Key is `taskID`, value is `TaskState`, recording the status, progress, timestamps, and other information of all in-progress and recently completed tasks.
- **Component State Mapping** (`components`): Key is the component name (such as `appinfo_module`, `syncer`, `hydrator`, etc.), value is `ComponentStatus`, recording the health status and runtime metrics of each sub-module.
- **Chart Repo Status** (`chartRepo`): Aggregated Chart Repo service status, including application processing progress, image download/analysis status, task queues, etc.

All state updates are completed through methods such as `UpdateAppState`, `UpdateTask`, `UpdateComponent`, `UpdateChartRepoStatus`. These methods use `TryLock` to acquire write locks, and will abandon the current update and log when lock contention is intense, avoiding blocking critical business logic. Read operations are completed through methods such as `GetAppState`, `GetTask`, `GetComponent`, which also use `TryRLock` to acquire read locks.

The `GetSnapshot()` method generates a complete `RuntimeSnapshot`: copies all current in-memory mappings, calculates aggregated statistical information `RuntimeSummary` (such as total number of applications, total number of tasks, number of tasks in each state, number of healthy/unhealthy applications, number of active components, etc.), and records the snapshot generation timestamp. The snapshot generation process briefly holds a read lock to ensure data consistency.

### 2.3 State Collector `StateCollector`

`StateCollector` is responsible for periodically pulling status from various business modules and writing it to `StateStore`. After the collector starts, it immediately performs a full collection, then triggers periodic collection every 5 seconds. The collection process executes in an independent goroutine, controlled by `context` for lifecycle management, supporting graceful shutdown.

The collector needs to hold references to `TaskModule` and `AppInfoModule`. These references are injected through `SetTaskModule` and `SetAppInfoModule` methods, and are protected by internal read-write locks during collection to avoid concurrent modifications.

Each collection executes the following steps:

1. **Collect Task States** (`collectTaskStates`):
   - Get pending and running task lists from `TaskModule` in memory.
   - Get recently completed task lists from the database (default maximum 100 entries).
   - Merge all tasks, convert to `TaskState` format (including task type, status, progress estimation, etc.), and write to `StateStore`.
   - For tasks that are no longer in `TaskModule`, remove them from `StateStore` to maintain synchronization.

2. **Collect Application Runtime States** (`collectAppFlowStates`):
   - Traverse all users and sources through `AppInfoModule`'s cache manager.
   - Based on cache contents such as `AppStateLatest`, `AppInfoLatest`, `AppInfoLatestPending`, derive the runtime stage (fetching, installing, running, upgrading, uninstalling, failed, stopped) and health status of each application.
   - Check if there are installation/upgrade/uninstallation tasks for this application, and if so, override the stage marker.
   - Synthesize `AppFlowState` and write to `StateStore`.

3. **Collect Component Statuses** (`collectComponentStatuses`):
   - Get module status, Syncer metrics, Hydrator metrics, cache statistics, and other information from `AppInfoModule`.
   - Count pending/running tasks from `TaskModule`.
   - Construct multiple `ComponentStatus` entries (`appinfo_module`, `syncer`, `hydrator`, `cache`, `task_module`), and write to `StateStore`.

4. **Collect Chart Repo Status** (`collectChartRepoStatus`):
   - Enumerate all user and source combinations from `AppInfoModule` cache.
   - Read environment variable `CHART_REPO_SERVICE_HOST` (default `localhost:82`), construct Chart Repo status query URL.
   - Call Chart Repo API via HTTP, parse the returned JSON data as `ChartRepoStatus`.
   - Aggregate results for multiple users and sources, and write to `StateStore`.

The collector is designed with fault tolerance as a priority: when HTTP calls fail, JSON parsing fails, or modules return empty data, it only logs and does not interrupt the entire collection process. Exceptions from a single data source do not affect collection from other data sources.

### 2.4 Runtime State Service `RuntimeStateService`

`RuntimeStateService` encapsulates access to `StateStore` and `StateCollector`, providing a unified query interface for external use. The service itself does not directly access business modules, only reading snapshot data from `StateStore`.

The service provides the following query capabilities:

- **Get Complete Snapshot**: `GetSnapshot()` directly returns the `RuntimeSnapshot` currently generated by `StateStore`, containing all application states, task states, component states, and Chart Repo status, as well as aggregated statistical information. Typically used for API layer's `/runtime/snapshot` interface or internal debugging.

- **Filter Snapshot Based on Conditions**: `GetSnapshotWithFilters(filters *SnapshotFilters)` supports filtering by user ID, source ID, application name, task status, task type, and other conditions, returning the filtered snapshot and recalculating `Summary` statistical information. The `ParseFiltersFromQuery` method can parse and construct filter conditions from HTTP Query parameters.

- **Partial Queries**: 
  - `GetAppState(userID, sourceID, appName)` returns the `AppFlowState` of the specified application.
  - `GetTask(taskID)` returns the `TaskState` of the specified task.
  - `GetTasksByApp(appName, userID)` collects all related tasks by application and user dimensions.
  - `GetTasksByUser(userID)` collects all tasks by user dimension.
  - `GetAppsByUser(userID)` collects all `AppFlowState` of the user's applications by user dimension.

Lifecycle control of the service is implemented through `Start()` and `Stop()` methods: `Start()` starts the internal `StateCollector`, `Stop()` stops the collector. The service is typically created during module initialization and provided to the HTTP API layer through dependency injection.

## 3. Core Data Models

The `runtime` module exposes runtime information through the following data models:

- **`AppFlowState`**: Application runtime status, recording the runtime stage (fetching, installing, running, upgrading, uninstalling, failed, stopped), health status, version, and other information of each application under the current user and source. Note: The `Stage` field is currently only calculated during the collection phase and is not used in actual business logic; its validity needs to be verified.

- **`TaskState`**: Task execution status, recording the status, progress, timestamps, results, and other information of background tasks such as installation, uninstallation, upgrade, cloning, etc.

- **`ComponentStatus`**: Component health status, recording the runtime status and key metrics of sub-modules such as `appinfo_module`, `syncer`, `hydrator`, `cache`, `task_module`, etc.

- **`ChartRepoStatus`**: Chart Repo service status, aggregating application processing progress, image download/analysis status, task queues, and other information.

- **`RuntimeSnapshot`**: Complete runtime snapshot, containing all the above state mappings as well as aggregated statistical information `RuntimeSummary` (total number of applications, total number of tasks, number of tasks in each state, number of healthy/unhealthy applications, etc.).

All data models exist only in memory and are not persisted separately. For detailed field definitions, please refer to `internal/v2/runtime/types.go`.

## 4. API Endpoints

**Location**: `pkg/v2/api/runtime.go`

The runtime module exposes the following HTTP endpoints:

- `GET /api/v2/runtime/state` - Get runtime state snapshot
- `GET /api/v2/runtime/dashboard` - Get runtime dashboard
- `GET /api/v2/runtime/dashboard-app` - Get application processing flow dashboard

