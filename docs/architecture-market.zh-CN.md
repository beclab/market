[English](./architecture-market.md) | [中文版 (Chinese)](./architecture-market.zh-CN.md)

# Market 项目内部架构

```mermaid
graph TB
    subgraph "API 服务层"
        MarketAPI[Market API Server<br/>RESTful API 服务]
        V1API[v1 API<br/>旧版接口]
        V2API[v2 API<br/>新版接口]
    end

    subgraph "核心业务模块"
        AppModule[App Module<br/>应用信息处理核心<br/>• 应用数据管理<br/>• 版本控制<br/>• 状态跟踪]
        TaskModule[Task Module<br/>任务管理<br/>• 安装/卸载<br/>• 升级/克隆<br/>• 取消操作]
        PaymentModule[Payment Module<br/>支付管理<br/>• 应用付费<br/>• 内购配置]
        RuntimeModule[Runtime Module<br/>运行时状态<br/>• 应用状态监控<br/>• 任务状态跟踪]
    end

    subgraph "数据存储"
        RedisStore[(Redis<br/>持久化存储<br/>• 应用缓存<br/>• 设置/市场源配置)]
        MemoryCache[(In-Memory Cache<br/>进程内缓存<br/>• AppInfoLatest<br/>• 状态快照)]
    end

    subgraph "数据处理"
        Syncer[Syncer<br/>数据同步器<br/>• 从远程 API 获取数据<br/>• 哈希比较<br/>• 增量更新]
        HydratorMarket[Hydrator<br/>Market 端处理<br/>• API 任务处理<br/>• 支付任务处理]
        DataWatcherMarket[DataWatcher<br/>Market 端监控<br/>• 应用状态监控<br/>• 仓库状态同步]
    end

    MarketAPI --> V1API
    MarketAPI --> V2API
    V1API -->|读取缓存| MemoryCache
    V2API -->|读取缓存| MemoryCache

    Syncer -->|从远程 API 同步数据| AppModule
    AppModule --> TaskModule
    TaskModule --> PaymentModule
    AppModule -->|落盘| RedisStore
    RuntimeModule -->|状态收集| AppModule

    HydratorMarket --> AppModule
    DataWatcherMarket --> AppModule

    style MarketAPI fill:#e1f5ff
    style AppModule fill:#fff4e1
    style Syncer fill:#ffe1f5
    style RedisStore fill:#e1ffe1
    style MemoryCache fill:#e1f5ff
```

## Syncer 模块运行逻辑

### 运行流程图

```mermaid
flowchart TD
    Start([Syncer 启动]) --> Loop[同步循环 syncLoop]
    Loop --> Wait{等待同步间隔<br/>默认 5 分钟}
    Wait --> Cycle[执行同步周期<br/>executeSyncCycle]
    
    Cycle --> GetSources[获取活跃市场源<br/>从 SettingsManager]
    GetSources --> Filter[过滤远程类型源<br/>跳过本地源]
    Filter --> HasRemote{是否有<br/>远程源?}
    
    HasRemote -->|否| Skip[跳过本次同步]
    HasRemote -->|是| ForEach[遍历每个远程源]
    
    ForEach --> SourceCycle[执行单源同步<br/>executeSyncCycleWithSource]
    SourceCycle --> CreateContext[创建 SyncContext<br/>设置版本和源信息]
    
    CreateContext --> StepLoop[执行同步步骤循环]
    StepLoop --> Step1[Step 1: 哈希比较<br/>HashComparisonStep]
    Step1 --> CheckHash{哈希是否<br/>匹配?}
    CheckHash -->|是| SkipData[跳过数据拉取]
    CheckHash -->|否| Step2[Step 2: 数据拉取<br/>DataFetchStep]
    
    Step2 --> Step3[Step 3: 详情拉取<br/>DetailFetchStep]
    SkipData --> Step3
    
    Step3 --> HasData{是否有<br/>LatestData?}
    HasData -->|否| LogWarn[记录警告]
    HasData -->|是| StoreData[存储数据到缓存<br/>AppInfoLatestPending]
    
    StoreData --> NotifyCache[通过 CacheManager<br/>触发 Hydrator/DataWatcher]
    NotifyCache --> SourceSuccess[单源同步成功]
    LogWarn --> SourceSuccess
    
    SourceSuccess --> NextSource{还有<br/>其他源?}
    NextSource -->|是| ForEach
    NextSource -->|否| UpdateStats[更新同步统计<br/>成功/失败计数]
    
    UpdateStats --> HealthCheck[健康检查<br/>连续失败次数等]
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

### 运行逻辑说明

- **周期同步**：Syncer 会按配置的间隔（默认约每 5 分钟）启动一次完整同步循环，从 Settings 中读取当前启用的市场源，仅对远程类型的源执行同步。
- **多数据源顺序尝试**：对于每个远程市场源，Syncer 依次执行预配置的同步步骤，如果单个源失败会记录错误并继续尝试下一个源，只要至少有一个源成功，同步就被认为是成功的。
- **分步执行**：每个同步周期会依次执行多个 Step（哈希比较、列表数据拉取、详情数据拉取等），每个 Step 都有超时与可跳过判断，避免长时间阻塞。
- **统一上下文与缓存写入**：Syncer 为每次同步构造统一的 SyncContext，集中管理远程版本号、市场源信息和最新数据；当成功获取完整数据后，会将数据写入到缓存（AppInfoLatestPending / Others 等），并通过 `CacheManager` 触发后续 Hydrator、DataWatcher 的处理。
- **监控与健康检查**：Syncer 内部记录最近一次同步时间、成功/失败次数、当前步骤和最近一次时长等指标，通过这些指标可以判断同步是否健康运行（例如连续多次失败或长时间未成功同步时视为不健康）。

## 应用运行状态正确性保障机制

- **数据来源**：应用运行状态的“真相”来自运行时的 `app-service`（包括 `/app-service/v1/all/apps` 和 `/app-service/v1/middlewares/status`），这些接口由运行环境实时上报实际状态。
- **缓存与快照**：Market 侧在内存与 Redis 中维护 `AppStateLatest` 等状态缓存，并基于缓存构建用户级快照和哈希，用于判断数据是否需要同步与推送。
- **状态纠偏组件**：`StatusCorrectionChecker` 会定期从 `app-service` 拉取最新状态，与缓存中的状态进行对比，识别以下几类问题：
  - **应用新出现**：远端有记录、本地缓存不存在 → 自动补充到缓存中，并记录历史。
  - **应用消失**：缓存中存在、远端已不存在 → 从缓存中移除，并尝试将相关卸载任务标记为成功。
  - **状态变更**：同一应用在远端与缓存中的状态不同（包含入口状态变化）→ 以远端为准更新缓存并记录历史。
- **任务状态矫正**：在每次状态检查结束后，`StatusCorrectionChecker` 会遍历当前处于“运行中”的任务，将其与实际应用状态对齐，例如：
  - 安装/克隆任务：如果应用已经处于 `running`，则补记为任务成功。
  - 卸载/取消安装任务：如果应用在远端已经不存在，则补记为任务成功。
- **哈希一致性维护**：每次纠偏后，针对受影响的用户重新计算用户数据快照和哈希，并通过 `CacheManager.ForceSync` 将修正后的状态持久化，确保缓存、哈希和实际运行状态保持一致。

