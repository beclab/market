# Market 系统架构

[English Version](architecture-market-system.md) | [中文版本](architecture-market-system.zh-CN.md)

本文档描述了 Market 项目和 Dynamic Chart Repository 项目构成的完整 Market 程序的功能架构。

## 系统架构概览

Market 系统由两个核心项目组成：
- **Market 项目**：应用商店核心服务，负责应用信息管理、任务处理、API 服务
- **Dynamic Chart Repository 项目**：Helm Chart 动态仓库，负责 Chart 渲染、镜像分析、状态管理



## 两个项目之间的交互关系

### 1 应用渲染功能

```mermaid
graph LR
    M1[Market: TaskForApiStep<br/>应用渲染任务] -.->|POST /dcr/sync-app| A1[Chart Repo API<br/>渲染接口]
    A1 --> S1[Hydrator<br/>处理管道]
    A1 --> S2[CacheManager<br/>缓存管理]
```

### 2 数据同步功能

```mermaid
graph LR
    M2[Market: DataWatcherRepo<br/>数据同步] -.->|GET /state-changes| A2[Chart Repo API<br/>状态变更接口]
    M2 -.->|GET /repo/data| A3[Chart Repo API<br/>仓库数据接口]
    M2 -.->|POST /apps| A4[Chart Repo API<br/>应用信息接口]
    M2 -.->|GET /images| A5[Chart Repo API<br/>镜像信息接口]
    
    A2 --> S3[Status 模块<br/>状态管理]
    A2 --> S4[DataWatcher<br/>状态监控]
    A3 --> S2[CacheManager<br/>缓存管理]
    A4 --> S2
    A5 --> S2
```

### 3 配置管理功能

```mermaid
graph LR
    M3[Market: SettingsManager<br/>配置管理] -.->|GET/POST /settings/market-source| A6[Chart Repo API<br/>配置接口]
    A6 --> S5[SettingsManager<br/>配置管理]
    A6 --> S6[Redis<br/>持久化存储]
```

### 4 状态监控功能

```mermaid
graph LR
    M4[Market: RuntimeCollector<br/>运行时状态采集] -.->|GET /status /version| A7[Chart Repo API<br/>状态接口]
    A7 --> S3[Status 模块<br/>状态管理]
```


## 数据流

**颜色说明：**
- 🔵 **蓝色**：Market 项目执行的步骤（包括 Market 的 Redis 存储）
- 🔴 **粉色**：Chart Repo 项目执行的步骤（包括 Chart Repo 的 Redis 存储）
- 🟡 **黄色**：跨项目交互（API 调用）

```mermaid
flowchart TD
    subgraph "Market 项目数据流"
        SyncerStart[Syncer 定时同步<br/>从远程 API 获取数据]
        DataParse[应用数据解析]
        AdminFetch[Admin 配置获取]
        DataMerge[数据合并]
        CacheUpdate[(更新内存缓存)]
        RedisPersist[(写入 Redis)]
    end

    subgraph "Market 到 Chart Repo 数据流"
        SyncRequest[同步请求<br/>POST /dcr/sync-app]
        PendingQueue[待处理队列<br/>AppInfoLatestPending]
    end

    subgraph "Chart Repo 处理流程"
        HydratorStart[Hydrator 启动处理<br/>从待处理队列获取任务]
        
        subgraph "5步处理流程"
            S1[1. SourceChartStep<br/>源Chart处理<br/>• 验证源Chart包是否存在<br/>• 从远程或本地获取Chart<br/>• 解压并验证Chart结构]
            S2[2. RenderedChartStep<br/>Chart渲染<br/>• 使用Helm渲染Chart模板<br/>• 应用values.yaml参数<br/>• 生成渲染后的Chart包]
            S3[3. CustomParamsUpdateStep<br/>自定义参数更新<br/>• 更新Chart中的自定义参数<br/>• 处理用户自定义配置<br/>• 合并参数到values.yaml]
            S4[4. ImageAnalysisStep<br/>镜像分析<br/>• 扫描Chart中的Docker镜像<br/>• 提取镜像名称和标签<br/>• 分析镜像架构和大小<br/>• 生成镜像信息缓存]
            S5[5. DatabaseUpdateStep<br/>数据库更新<br/>• 更新内存缓存CacheManager<br/>• 持久化到Redis存储<br/>• 将状态从Pending转为Latest<br/>• 存储Chart文件到文件系统]
        end

        Success[成功<br/>AppInfoLatest<br/>应用数据可用]
        Failed[失败<br/>AppRenderFailed<br/>记录错误信息]
    end

    subgraph "状态同步流程"
        StateChange[Chart Repo 生成状态变更事件<br/>app_upload_completed / image_info_updated]
        StateHistory[(状态历史存储<br/>Redis)]
        MarketPoll[Market DataWatcherRepo<br/>轮询 /state-changes<br/>每2分钟]
        FetchDetail[Market 获取详细信息<br/>POST /apps 或 GET /images]
        MarketCacheUpdate[Market 更新本地缓存<br/>应用信息 / 镜像信息]
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
    S5 -->|成功| Success
    S5 -->|失败| Failed

    Success --> StateChange
    StateChange --> StateHistory
    StateHistory --> MarketPoll
    MarketPoll --> FetchDetail
    FetchDetail --> MarketCacheUpdate

    %% Market 项目步骤 - 蓝色系
    style SyncerStart fill:#e1f5ff
    style DataParse fill:#e1f5ff
    style AdminFetch fill:#e1f5ff
    style DataMerge fill:#e1f5ff
    style CacheUpdate fill:#e1f5ff
    style RedisPersist fill:#e1f5ff
    style MarketPoll fill:#e1f5ff
    style FetchDetail fill:#e1f5ff
    style MarketCacheUpdate fill:#e1f5ff

    %% 跨项目交互 - 黄色系
    style SyncRequest fill:#fff4e1
    style PendingQueue fill:#fff4e1

    %% Chart Repo 项目步骤 - 粉色系（包括 Chart Repo 的 Redis 存储和结果状态）
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

## 核心交互流程

### 应用同步流程

```mermaid
sequenceDiagram
    participant Market as Market 项目
    participant ChartRepo as Chart Repository
    participant Redis as Redis
    participant Storage as Chart Storage

    Market->>ChartRepo: POST /dcr/sync-app<br/>(应用数据)
    ChartRepo->>Redis: 写入 Pending 队列
    ChartRepo-->>Market: 202 Accepted

    Note over ChartRepo: 异步处理
    ChartRepo->>ChartRepo: Hydrator 处理管道
    ChartRepo->>Storage: 存储 Chart 文件
    ChartRepo->>Redis: 更新 Latest 状态
    
    Market->>ChartRepo: GET /state-changes<br/>(轮询状态)
    ChartRepo-->>Market: 返回状态变更
    Market->>Market: 更新本地缓存
```

### 应用安装流程

```mermaid
sequenceDiagram
    participant Frontend as Market Frontend
    participant MarketAPI as Market API
    participant TaskModule as Task Module
    participant AppService as App Service
    participant ChartRepo as Chart Repository

    Frontend->>MarketAPI: POST /apps/{id}/install
    MarketAPI->>MarketAPI: 验证用户和应用信息
    MarketAPI->>MarketAPI: 从缓存获取应用数据
    MarketAPI->>TaskModule: 创建安装任务
    MarketAPI-->>Frontend: 返回任务ID（异步）或等待完成（同步）
    
    Note over TaskModule: 异步执行
    TaskModule->>TaskModule: 检查并发控制
    TaskModule->>TaskModule: 获取 VC（支付凭证）
    TaskModule->>AppService: POST /apps/{name}/install<br/>包含 repoUrl（Chart Repo地址）
    AppService->>ChartRepo: GET /static-index.yaml<br/>获取应用列表索引
    ChartRepo-->>AppService: 返回索引文件
    AppService->>ChartRepo: GET /charts/{name}-{version}.tgz<br/>下载 Chart 包
    ChartRepo-->>AppService: 返回 Chart 包
    AppService->>AppService: 执行安装操作
    AppService-->>TaskModule: 返回操作结果
    TaskModule->>TaskModule: 更新任务状态
    TaskModule->>MarketAPI: 通知任务完成
    
    alt 安装成功
        MarketAPI-->>Frontend: 返回安装成功结果（同步模式）
    else 安装失败（缺少env配置）
        MarketAPI-->>Frontend: 返回失败结果<br/>包含需要的env配置信息
        Frontend->>Frontend: 显示env配置表单<br/>让用户填充字段
        Frontend->>MarketAPI: POST /apps/{id}/install<br/>重新调用，包含完整的env配置
        Note over MarketAPI,ChartRepo: 重复上述安装流程
        MarketAPI-->>Frontend: 返回安装结果
    end
```



### 应用卸载流程

```mermaid
sequenceDiagram
    participant Frontend as Market Frontend
    participant MarketAPI as Market API
    participant TaskModule as Task Module
    participant AppService as App Service

    Frontend->>MarketAPI: POST /apps/{id}/uninstall
    MarketAPI->>MarketAPI: 验证用户和应用信息
    MarketAPI->>TaskModule: 创建卸载任务
    MarketAPI-->>Frontend: 返回任务ID（异步）或等待完成（同步）
    
    Note over TaskModule: 异步执行
    TaskModule->>AppService: POST /apps/{name}/uninstall<br/>包含 all 参数
    AppService->>AppService: 执行卸载操作
    AppService-->>TaskModule: 返回操作结果
    TaskModule->>TaskModule: 更新任务状态
    TaskModule->>MarketAPI: 通知任务完成
```

### 应用克隆流程

```mermaid
sequenceDiagram
    participant Frontend as Market Frontend
    participant MarketAPI as Market API
    participant TaskModule as Task Module
    participant AppService as App Service
    participant ChartRepo as Chart Repository

    Frontend->>MarketAPI: POST /apps/{id}/clone<br/>指定新应用名称和配置
    MarketAPI->>MarketAPI: 验证用户和应用信息
    MarketAPI->>MarketAPI: 从缓存获取应用数据
    MarketAPI->>TaskModule: 创建克隆任务<br/>包含 rawAppName, envsHash 等
    MarketAPI-->>Frontend: 返回任务ID（异步）或等待完成（同步）
    
    Note over TaskModule: 异步执行
    TaskModule->>TaskModule: 检查并发控制
    TaskModule->>TaskModule: 获取 VC（支付凭证）
    TaskModule->>AppService: POST /apps/{name}/install<br/>包含 repoUrl, rawAppName, entrances 等
    AppService->>ChartRepo: GET /static-index.yaml<br/>获取应用列表索引
    ChartRepo-->>AppService: 返回索引文件
    AppService->>ChartRepo: GET /charts/{name}-{version}.tgz<br/>下载 Chart 包
    ChartRepo-->>AppService: 返回 Chart 包
    AppService->>AppService: 执行克隆安装操作
    AppService-->>TaskModule: 返回操作结果
    TaskModule->>TaskModule: 更新任务状态
    TaskModule->>MarketAPI: 通知任务完成
    
    alt 克隆成功
        MarketAPI-->>Frontend: 返回克隆成功结果（同步模式）
    else 克隆失败（App Service返回失败）
        AppService-->>TaskModule: 返回失败结果<br/>包含需要补充的配置信息<br/>（应用入口title、应用title等）
        TaskModule->>TaskModule: 更新任务状态为失败<br/>包含backend_response
        TaskModule->>MarketAPI: 通知任务完成（失败）
        MarketAPI-->>Frontend: 返回失败结果<br/>包含backend_response中的配置信息
        Frontend->>Frontend: 解析backend_response<br/>显示配置表单<br/>让用户填充应用入口title、应用title等字段
        Frontend->>MarketAPI: POST /apps/{id}/clone<br/>重新调用，包含完整的配置信息
        Note over MarketAPI,ChartRepo: 重复上述克隆流程
        MarketAPI-->>Frontend: 返回克隆结果
    end
```



