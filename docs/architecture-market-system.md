# Market System Architecture

[中文版本](architecture-market-system.zh-CN.md) | [English Version](architecture-market-system.md)

This document describes the functional architecture of the complete Market application composed of the Market project and the Dynamic Chart Repository project.

## System Architecture Overview

The Market system consists of two core projects:
- **Market Project**: Core app store service responsible for application information management, task processing, and API services
- **Dynamic Chart Repository Project**: Helm Chart dynamic repository responsible for Chart rendering, image analysis, and status management



## Interaction Between the Two Projects

### 1 Application Rendering Functionality

```mermaid
graph LR
    M1[Market: TaskForApiStep<br/>Application Rendering Task] -.->|POST /dcr/sync-app| A1[Chart Repo API<br/>Rendering Interface]
    A1 --> S1[Hydrator<br/>Processing Pipeline]
    A1 --> S2[CacheManager<br/>Cache Management]
```

### 2 Data Synchronization Functionality

```mermaid
graph LR
    M2[Market: DataWatcherRepo<br/>Data Synchronization] -.->|GET /state-changes| A2[Chart Repo API<br/>Status Change Interface]
    M2 -.->|GET /repo/data| A3[Chart Repo API<br/>Repository Data Interface]
    M2 -.->|POST /apps| A4[Chart Repo API<br/>Application Information Interface]
    M2 -.->|GET /images| A5[Chart Repo API<br/>Image Information Interface]
    
    A2 --> S3[Status Module<br/>Status Management]
    A2 --> S4[DataWatcher<br/>Status Monitoring]
    A3 --> S2[CacheManager<br/>Cache Management]
    A4 --> S2
    A5 --> S2
```

### 3 Configuration Management Functionality

```mermaid
graph LR
    M3[Market: SettingsManager<br/>Configuration Management] -.->|GET/POST /settings/market-source| A6[Chart Repo API<br/>Configuration Interface]
    A6 --> S5[SettingsManager<br/>Configuration Management]
    A6 --> S6[Redis<br/>Persistent Storage]
```

### 4 Status Monitoring Functionality

```mermaid
graph LR
    M4[Market: RuntimeCollector<br/>Runtime Status Collection] -.->|GET /status /version| A7[Chart Repo API<br/>Status Interface]
    A7 --> S3[Status Module<br/>Status Management]
```


## Data Flow

**Color Legend:**
- 🔵 **Blue**: Steps executed by the Market project (including Market's Redis storage)
- 🔴 **Pink**: Steps executed by the Chart Repo project (including Chart Repo's Redis storage)
- 🟡 **Yellow**: Cross-project interactions (API calls)

```mermaid
flowchart TD
    subgraph "Market Project Data Flow"
        SyncerStart[Syncer Scheduled Sync<br/>Fetch Data from Remote API]
        DataParse[Application Data Parsing]
        AdminFetch[Admin Configuration Fetch]
        DataMerge[Data Merging]
        CacheUpdate[(Update In-Memory Cache)]
        RedisPersist[(Write to Redis)]
    end

    subgraph "Market to Chart Repo Data Flow"
        SyncRequest[Synchronization Request<br/>POST /dcr/sync-app]
        PendingQueue[Pending Queue<br/>AppInfoLatestPending]
    end

    subgraph "Chart Repo Processing Flow"
        HydratorStart[Hydrator Start Processing<br/>Fetch Tasks from Pending Queue]
        
        subgraph "5-Step Processing Flow"
            S1[1. SourceChartStep<br/>Source Chart Processing<br/>• Verify if source Chart package exists<br/>• Fetch Chart from remote or local<br/>• Extract and verify Chart structure]
            S2[2. RenderedChartStep<br/>Chart Rendering<br/>• Render Chart template using Helm<br/>• Apply values.yaml parameters<br/>• Generate rendered Chart package]
            S3[3. CustomParamsUpdateStep<br/>Custom Parameters Update<br/>• Update custom parameters in Chart<br/>• Handle user custom configurations<br/>• Merge parameters into values.yaml]
            S4[4. ImageAnalysisStep<br/>Image Analysis<br/>• Scan Docker images in Chart<br/>• Extract image names and tags<br/>• Analyze image architecture and size<br/>• Generate image information cache]
            S5[5. DatabaseUpdateStep<br/>Database Update<br/>• Update in-memory cache CacheManager<br/>• Persist to Redis storage<br/>• Change status from Pending to Latest<br/>• Store Chart files to file system]
        end

        Success[Success<br/>AppInfoLatest<br/>Application Data Available]
        Failed[Failed<br/>AppRenderFailed<br/>Record Error Information]
    end

    subgraph "Status Synchronization Flow"
        StateChange[Chart Repo Generates Status Change Events<br/>app_upload_completed / image_info_updated]
        StateHistory[(Status History Storage<br/>Redis)]
        MarketPoll[Market DataWatcherRepo<br/>Poll /state-changes<br/>Every 2 minutes]
        FetchDetail[Market Fetch Detailed Information<br/>POST /apps or GET /images]
        MarketCacheUpdate[Market Update Local Cache<br/>Application Information / Image Information]
    end

    SyncerStart --> DataParse
    AdminFetch --> DataMerge
    DataParse --> DataMerge
    DataMerge --> CacheUpdate
    CacheUpdate --> RedisPersist

    DataMerge --> SyncRequest
    SyncRequest --> PendingQueue
    PendingQueue --> HydratorStart
    HydratorStart --> S1
    S1 --> S2
    S2 --> S3
    S3 --> S4
    S4 --> S5
    S5 -->|Success| Success
    S5 -->|Failed| Failed

    Success --> StateChange
    StateChange --> StateHistory
    StateHistory --> MarketPoll
    MarketPoll --> FetchDetail
    FetchDetail --> MarketCacheUpdate

    %% Market project steps - blue tones
    style SyncerStart fill:#e1f5ff
    style DataParse fill:#e1f5ff
    style AdminFetch fill:#e1f5ff
    style DataMerge fill:#e1f5ff
    style CacheUpdate fill:#e1f5ff
    style RedisPersist fill:#e1f5ff
    style MarketPoll fill:#e1f5ff
    style FetchDetail fill:#e1f5ff
    style MarketCacheUpdate fill:#e1f5ff

    %% Cross-project interactions - yellow tones
    style SyncRequest fill:#fff4e1
    style PendingQueue fill:#fff4e1

    %% Chart Repo project steps - pink tones (including Chart Repo's Redis storage and result status)
    style HydratorStart fill:#ffe1f5
    style S1 fill:#ffe1f5
    style S2 fill:#ffe1f5
    style S3 fill:#ffe1f5
    style S4 fill:#ffe1f5
    style S5 fill:#ffe1f5
    style StateChange fill:#ffe1f5
    style StateHistory fill:#ffe1f5
    style Success fill:#ffe1f5
    style Failed fill:#ffe1f5
```

## Core Interaction Flows

### Application Synchronization Flow

```mermaid
sequenceDiagram
    participant Market as Market Project
    participant ChartRepo as Chart Repository
    participant Redis as Redis
    participant Storage as Chart Storage

    Market->>ChartRepo: POST /dcr/sync-app<br/>(Application Data)
    ChartRepo->>Redis: Write to Pending Queue
    ChartRepo-->>Market: 202 Accepted

    Note over ChartRepo: Asynchronous Processing
    ChartRepo->>ChartRepo: Hydrator Processing Pipeline
    ChartRepo->>Storage: Store Chart Files
    ChartRepo->>Redis: Update Latest Status
    
    Market->>ChartRepo: GET /state-changes<br/>(Poll Status)
    ChartRepo-->>Market: Return Status Changes
    Market->>Market: Update Local Cache
```

### Application Installation Flow

```mermaid
sequenceDiagram
    participant Frontend as Market Frontend
    participant MarketAPI as Market API
    participant TaskModule as Task Module
    participant AppService as App Service
    participant ChartRepo as Chart Repository

    Frontend->>MarketAPI: POST /apps/{id}/install
    MarketAPI->>MarketAPI: Validate User and Application Information
    MarketAPI->>MarketAPI: Fetch Application Data from Cache
    MarketAPI->>TaskModule: Create Installation Task
    MarketAPI-->>Frontend: Return Task ID (Async) or Wait for Completion (Sync)
    
    Note over TaskModule: Asynchronous Execution
    TaskModule->>TaskModule: Check Concurrency Control
    TaskModule->>TaskModule: Get VC (Payment Credential)
    TaskModule->>AppService: POST /apps/{name}/install<br/>Include repoUrl (Chart Repo Address)
    AppService->>ChartRepo: GET /static-index.yaml<br/>Fetch Application List Index
    ChartRepo-->>AppService: Return Index File
    AppService->>ChartRepo: GET /charts/{name}-{version}.tgz<br/>Download Chart Package
    ChartRepo-->>AppService: Return Chart Package
    AppService->>AppService: Execute Installation Operation
    AppService-->>TaskModule: Return Operation Result
    TaskModule->>TaskModule: Update Task Status
    TaskModule->>MarketAPI: Notify Task Completion
    
    alt Installation Success
        MarketAPI-->>Frontend: Return Installation Success Result (Sync Mode)
    else Installation Failed (Missing env Configuration)
        MarketAPI-->>Frontend: Return Failure Result<br/>Include Required env Configuration Information
        Frontend->>Frontend: Display env Configuration Form<br/>Let User Fill Fields
        Frontend->>MarketAPI: POST /apps/{id}/install<br/>Retry Call with Complete env Configuration
        Note over MarketAPI,ChartRepo: Repeat Above Installation Flow
        MarketAPI-->>Frontend: Return Installation Result
    end
```



### Application Uninstallation Flow

```mermaid
sequenceDiagram
    participant Frontend as Market Frontend
    participant MarketAPI as Market API
    participant TaskModule as Task Module
    participant AppService as App Service

    Frontend->>MarketAPI: POST /apps/{id}/uninstall
    MarketAPI->>MarketAPI: Validate User and Application Information
    MarketAPI->>TaskModule: Create Uninstallation Task
    MarketAPI-->>Frontend: Return Task ID (Async) or Wait for Completion (Sync)
    
    Note over TaskModule: Asynchronous Execution
    TaskModule->>AppService: POST /apps/{name}/uninstall<br/>Include all Parameter
    AppService->>AppService: Execute Uninstallation Operation
    AppService-->>TaskModule: Return Operation Result
    TaskModule->>TaskModule: Update Task Status
    TaskModule->>MarketAPI: Notify Task Completion
```

### Application Cloning Flow

```mermaid
sequenceDiagram
    participant Frontend as Market Frontend
    participant MarketAPI as Market API
    participant TaskModule as Task Module
    participant AppService as App Service
    participant ChartRepo as Chart Repository

    Frontend->>MarketAPI: POST /apps/{id}/clone<br/>Specify New Application Name and Configuration
    MarketAPI->>MarketAPI: Validate User and Application Information
    MarketAPI->>MarketAPI: Fetch Application Data from Cache
    MarketAPI->>TaskModule: Create Cloning Task<br/>Include rawAppName, envsHash, etc.
    MarketAPI-->>Frontend: Return Task ID (Async) or Wait for Completion (Sync)
    
    Note over TaskModule: Asynchronous Execution
    TaskModule->>TaskModule: Check Concurrency Control
    TaskModule->>TaskModule: Get VC (Payment Credential)
    TaskModule->>AppService: POST /apps/{name}/install<br/>Include repoUrl, rawAppName, entrances, etc.
    AppService->>ChartRepo: GET /static-index.yaml<br/>Fetch Application List Index
    ChartRepo-->>AppService: Return Index File
    AppService->>ChartRepo: GET /charts/{name}-{version}.tgz<br/>Download Chart Package
    ChartRepo-->>AppService: Return Chart Package
    AppService->>AppService: Execute Cloning Installation Operation
    AppService-->>TaskModule: Return Operation Result
    TaskModule->>TaskModule: Update Task Status
    TaskModule->>MarketAPI: Notify Task Completion
    
    alt Cloning Success
        MarketAPI-->>Frontend: Return Cloning Success Result (Sync Mode)
    else Cloning Failed (App Service Returns Failure)
        AppService-->>TaskModule: Return Failure Result<br/>Include Required Configuration Information<br/>(Application Entrance Title, Application Title, etc.)
        TaskModule->>TaskModule: Update Task Status to Failed<br/>Include backend_response
        TaskModule->>MarketAPI: Notify Task Completion (Failed)
        MarketAPI-->>Frontend: Return Failure Result<br/>Include Configuration Information from backend_response
        Frontend->>Frontend: Parse backend_response<br/>Display Configuration Form<br/>Let User Fill Application Entrance Title, Application Title, etc.
        Frontend->>MarketAPI: POST /apps/{id}/clone<br/>Retry Call with Complete Configuration Information
        Note over MarketAPI,ChartRepo: Repeat Above Cloning Flow
        MarketAPI-->>Frontend: Return Cloning Result
    end
```



