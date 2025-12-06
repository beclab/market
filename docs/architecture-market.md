[中文版 (Chinese)](./architecture-market.zh-CN.md) | [English](./architecture-market.md)

# Market Project Internal Architecture

```mermaid
graph TB
    subgraph "API Service Layer"
        MarketAPI[Market API Server<br/>RESTful API Service]
        V1API[v1 API<br/>Legacy Interface]
        V2API[v2 API<br/>New Interface]
    end

    subgraph "Core Business Modules"
        AppModule[App Module<br/>Application Information Processing Core<br/>• Application Data Management<br/>• Version Control<br/>• Status Tracking]
        TaskModule[Task Module<br/>Task Management<br/>• Install/Uninstall<br/>• Upgrade/Clone<br/>• Cancel Operations]
        PaymentModule[Payment Module<br/>Payment Management<br/>• Application Payment<br/>• In-App Purchase Configuration]
        RuntimeModule[Runtime Module<br/>Runtime Status<br/>• Application Status Monitoring<br/>• Task Status Tracking]
    end

    subgraph "Data Storage"
        RedisStore[(Redis<br/>Persistent Storage<br/>• Application Cache<br/>• Settings/Market Source Configuration)]
        MemoryCache[(In-Memory Cache<br/>Process Cache<br/>• AppInfoLatest<br/>• Status Snapshots)]
    end

    subgraph "Data Processing"
        Syncer[Syncer<br/>Data Synchronizer<br/>• Fetch Data from Remote API<br/>• Hash Comparison<br/>• Incremental Updates]
        HydratorMarket[Hydrator<br/>Market-side Processing<br/>• API Task Processing<br/>• Payment Task Processing]
        DataWatcherMarket[DataWatcher<br/>Market-side Monitoring<br/>• Application Status Monitoring<br/>• Repository Status Sync]
    end

    MarketAPI --> V1API
    MarketAPI --> V2API
    V1API -->|Read Cache| MemoryCache
    V2API -->|Read Cache| MemoryCache

    Syncer -->|Sync Data from Remote API| AppModule
    AppModule --> TaskModule
    TaskModule --> PaymentModule
    AppModule -->|Persist| RedisStore
    RuntimeModule -->|Collect Status| AppModule

    HydratorMarket --> AppModule
    DataWatcherMarket --> AppModule

    style MarketAPI fill:#e1f5ff
    style AppModule fill:#fff4e1
    style Syncer fill:#ffe1f5
    style RedisStore fill:#e1ffe1
    style MemoryCache fill:#e1f5ff
```

## Syncer Module Operation Logic

### Operation Flow Diagram

```mermaid
flowchart TD
    Start([Syncer Start]) --> Loop[Sync Loop syncLoop]
    Loop --> Wait{Wait for Sync Interval<br/>Default 5 minutes}
    Wait --> Cycle[Execute Sync Cycle<br/>executeSyncCycle]
    
    Cycle --> GetSources[Get Active Market Sources<br/>from SettingsManager]
    GetSources --> Filter[Filter Remote Sources<br/>Skip Local Sources]
    Filter --> HasRemote{Are there<br/>Remote Sources?}
    
    HasRemote -->|No| Skip[Skip This Sync]
    HasRemote -->|Yes| ForEach[Iterate Each Remote Source]
    
    ForEach --> SourceCycle[Execute Single Source Sync<br/>executeSyncCycleWithSource]
    SourceCycle --> CreateContext[Create SyncContext<br/>Set Version and Source Info]
    
    CreateContext --> StepLoop[Execute Sync Step Loop]
    StepLoop --> Step1[Step 1: Hash Comparison<br/>HashComparisonStep]
    Step1 --> CheckHash{Is Hash<br/>Matched?}
    CheckHash -->|Yes| SkipData[Skip Data Fetch]
    CheckHash -->|No| Step2[Step 2: Data Fetch<br/>DataFetchStep]
    
    Step2 --> Step3[Step 3: Detail Fetch<br/>DetailFetchStep]
    SkipData --> Step3
    
    Step3 --> HasData{Is there<br/>LatestData?}
    HasData -->|No| LogWarn[Log Warning]
    HasData -->|Yes| StoreData[Store Data to Cache<br/>AppInfoLatestPending]
    
    StoreData --> NotifyCache[Trigger via CacheManager<br/>Hydrator/DataWatcher]
    NotifyCache --> SourceSuccess[Single Source Sync Success]
    LogWarn --> SourceSuccess
    
    SourceSuccess --> NextSource{Are there<br/>Other Sources?}
    NextSource -->|Yes| ForEach
    NextSource -->|No| UpdateStats[Update Sync Statistics<br/>Success/Failure Count]
    
    UpdateStats --> HealthCheck[Health Check<br/>Consecutive Failure Count, etc.]
    Skip --> HealthCheck
    HealthCheck --> Loop
    
    style Start fill:#e1f5ff
    style Cycle fill:#fff4e1
    style Step1 fill:#ffe1f5
    style Step2 fill:#ffe1f5
    style Step3 fill:#ffe1f5
    style StoreData fill:#e1ffe1
    style HealthCheck fill:#ffe1f5
```

### Operation Logic Description

- **Periodic Sync**: Syncer starts a complete sync cycle at configured intervals (default approximately every 5 minutes), reads currently enabled market sources from Settings, and only syncs remote-type sources.
- **Sequential Multi-Source Attempts**: For each remote market source, Syncer sequentially executes preconfigured sync steps. If a single source fails, it logs the error and continues to the next source. Sync is considered successful as long as at least one source succeeds.
- **Step-by-Step Execution**: Each sync cycle sequentially executes multiple Steps (hash comparison, list data fetch, detail data fetch, etc.). Each Step has timeout and skip conditions to avoid long-term blocking.
- **Unified Context and Cache Writing**: Syncer constructs a unified SyncContext for each sync, centrally managing remote version numbers, market source information, and latest data. When complete data is successfully obtained, it writes to cache (AppInfoLatestPending / Others, etc.) and triggers subsequent Hydrator and DataWatcher processing via `CacheManager`.
- **Monitoring and Health Checks**: Syncer internally records metrics such as last sync time, success/failure counts, current step, and last duration. These metrics can determine whether sync is running healthily (e.g., considered unhealthy when there are consecutive failures or no successful sync for a long time).

## Application Runtime Status Correctness Assurance Mechanism

- **Data Source**: The "source of truth" for application runtime status comes from runtime `app-service` (including `/app-service/v1/all/apps` and `/app-service/v1/middlewares/status`). These interfaces report actual status in real-time from the runtime environment.
- **Cache and Snapshots**: Market side maintains status caches such as `AppStateLatest` in memory and Redis, and builds user-level snapshots and hashes based on the cache to determine whether data needs to be synced and pushed.
- **Status Correction Component**: `StatusCorrectionChecker` periodically pulls the latest status from `app-service` and compares it with cached status, identifying the following types of issues:
  - **New Applications**: Records exist on remote but not in local cache → Automatically added to cache and history is recorded.
  - **Disappeared Applications**: Exist in cache but no longer exist on remote → Removed from cache, and related uninstall tasks are attempted to be marked as successful.
  - **Status Changes**: Same application has different status between remote and cache (including entry status changes) → Update cache based on remote and record history.
- **Task Status Correction**: After each status check, `StatusCorrectionChecker` traverses tasks currently in "running" status and aligns them with actual application status, for example:
  - Install/Clone tasks: If the application is already in `running`, mark the task as successful.
  - Uninstall/Cancel install tasks: If the application no longer exists on remote, mark the task as successful.
- **Hash Consistency Maintenance**: After each correction, recalculate user data snapshots and hashes for affected users, and persist the corrected status via `CacheManager.ForceSync` to ensure cache, hashes, and actual runtime status remain consistent.


