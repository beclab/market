## 1. 模块概览

`runtime` 模块负责**聚合和对外暴露市场系统的运行态信息**，包括：
- **应用运行状态 / 生命周期**: 每个用户、每个来源下应用当前是运行、安装中、升级中、卸载中还是失败/停止，以及对应健康度。
- **任务执行状态**: 安装、卸载、升级、克隆等后台任务的当前状态与进度（更偏向“操作过程”本身）。
- **系统组件状态**: `appinfo` 模块、同步器（Syncer）、Hydrator、任务模块、缓存等子组件的健康检查与指标。
- **Chart Repo 状态**: 从 Chart Repo 服务采集的应用处理状态、镜像下载/分析状态以及相关任务信息。

> 核心目标是：**提供一个统一的 Runtime 视图**，让前端和运维/监控系统可以通过简单的 API 获取当前市场系统的运行健康情况和关键进程的实时快照。
>
> 代码入口主要位于：
>- `internal/v2/runtime/types.go`（数据结构定义）
>- `internal/v2/runtime/store.go`（内存状态存储）
>- `internal/v2/runtime/collector.go`（状态采集）
>- `internal/v2/runtime/service.go`（对外服务接口）


## 2. 运行方式与内部流程

`runtime` 模块采用“采集-存储-查询”三层架构：`StateCollector` 负责从各业务模块定期采集状态，`StateStore` 负责在内存中维护统一的运行态快照，`RuntimeStateService` 负责对外提供查询接口。模块启动后，采集器以固定周期（默认 5 秒）刷新存储，确保查询接口返回的数据始终反映系统最新运行状态。

### 2.1 模块生命周期

模块初始化时，首先创建 `StateStore` 作为内存存储中心，然后创建 `StateCollector` 并注入对 `TaskModule` 和 `AppInfoModule` 的引用。调用 `RuntimeStateService.Start()` 后，采集器立即执行一次全量采集，随后启动后台协程，每 5 秒执行一次周期性采集。采集过程中，采集器从任务模块、应用信息模块和 Chart Repo 服务拉取最新状态，转换为统一的数据模型后写入 `StateStore`。查询接口通过 `RuntimeStateService` 访问 `StateStore`，返回当前快照或按条件过滤后的结果。

### 2.2 内存状态存储 `StateStore`

`StateStore` 是进程内的统一运行态存储，维护以下内存映射：

- **应用状态映射** (`appStates`): 键为 `userID:sourceID:appName`，值为 `AppFlowState`，记录每个应用在当前用户和来源下的运行阶段、健康状态、版本等信息。
- **任务状态映射** (`tasks`): 键为 `taskID`，值为 `TaskState`，记录所有进行中和最近完成的任务状态、进度、时间戳等。
- **组件状态映射** (`components`): 键为组件名称（如 `appinfo_module`、`syncer`、`hydrator` 等），值为 `ComponentStatus`，记录各子模块的健康状态和运行指标。
- **Chart Repo 状态** (`chartRepo`): 聚合的 Chart Repo 服务状态，包括应用处理进度、镜像下载/分析状态、任务队列等。

所有状态更新通过 `UpdateAppState`、`UpdateTask`、`UpdateComponent`、`UpdateChartRepoStatus` 等方法完成，这些方法使用 `TryLock` 获取写锁，在锁竞争激烈时会放弃本次更新并记录日志，避免阻塞关键业务逻辑。读取操作通过 `GetAppState`、`GetTask`、`GetComponent` 等方法完成，同样使用 `TryRLock` 获取读锁。

`GetSnapshot()` 方法生成完整的 `RuntimeSnapshot`：复制当前所有内存映射，计算聚合统计信息 `RuntimeSummary`（如总应用数、总任务数、各状态任务数量、健康/不健康应用数量、活跃组件数等），并记录快照生成时间戳。快照生成过程会短暂持有读锁，确保数据一致性。

### 2.3 状态采集器 `StateCollector`

`StateCollector` 负责从各业务模块定期拉取状态并写入 `StateStore`。采集器启动后，立即执行一次全量采集，随后每 5 秒触发一次周期性采集。采集过程在独立协程中执行，通过 `context` 控制生命周期，支持优雅停止。

采集器需要持有对 `TaskModule` 和 `AppInfoModule` 的引用，这些引用通过 `SetTaskModule` 和 `SetAppInfoModule` 方法注入，并在采集过程中通过内部读写锁保护，避免并发修改。

每次采集执行以下步骤：

1. **采集任务状态** (`collectTaskStates`):
   - 从 `TaskModule` 获取内存中的 pending 和 running 任务列表。
   - 从数据库获取最近完成的任务列表（默认最多 100 条）。
   - 合并所有任务，转换为 `TaskState` 格式（包括任务类型、状态、进度估算等），写入 `StateStore`。
   - 对于已不在 `TaskModule` 中的任务，从 `StateStore` 中移除，保持同步。

2. **采集应用运行状态** (`collectAppFlowStates`):
   - 通过 `AppInfoModule` 的缓存管理器遍历所有用户和来源。
   - 根据 `AppStateLatest`、`AppInfoLatest`、`AppInfoLatestPending` 等缓存内容，推导每个应用的运行阶段（fetching、installing、running、upgrading、uninstalling、failed、stopped）和健康状态。
   - 检查是否存在针对该应用的安装/升级/卸载任务，如有则覆盖阶段标记。
   - 合成 `AppFlowState` 并写入 `StateStore`。

3. **采集组件状态** (`collectComponentStatuses`):
   - 从 `AppInfoModule` 获取模块状态、Syncer 指标、Hydrator 指标、缓存统计等信息。
   - 从 `TaskModule` 统计 pending/running 任务数量。
   - 构造多个 `ComponentStatus` 条目（`appinfo_module`、`syncer`、`hydrator`、`cache`、`task_module`），写入 `StateStore`。

4. **采集 Chart Repo 状态** (`collectChartRepoStatus`):
   - 从 `AppInfoModule` 缓存枚举所有用户和来源组合。
   - 读取环境变量 `CHART_REPO_SERVICE_HOST`（默认 `localhost:82`），构造 Chart Repo 状态查询 URL。
   - 通过 HTTP 调用 Chart Repo API，解析返回的 JSON 数据为 `ChartRepoStatus`。
   - 对多用户、多来源的结果进行聚合，写入 `StateStore`。

采集器设计为容错优先：当 HTTP 调用失败、JSON 解析失败或模块返回空数据时，仅记录日志，不会中断整个采集过程。单个数据源的异常不影响其他数据源的采集。

### 2.4 运行态服务 `RuntimeStateService`

`RuntimeStateService` 封装对 `StateStore` 和 `StateCollector` 的访问，为外部提供统一的查询接口。服务本身不直接访问业务模块，仅读取 `StateStore` 中的快照数据。

服务提供以下查询能力：

- **获取完整快照**: `GetSnapshot()` 直接返回 `StateStore` 当前生成的 `RuntimeSnapshot`，包含所有应用状态、任务状态、组件状态和 Chart Repo 状态，以及聚合统计信息。通常用于 API 层的 `/runtime/snapshot` 接口或内部调试。

- **基于条件过滤快照**: `GetSnapshotWithFilters(filters *SnapshotFilters)` 支持按用户 ID、来源 ID、应用名称、任务状态、任务类型等条件过滤，返回过滤后的快照，并重新计算 `Summary` 统计信息。`ParseFiltersFromQuery` 方法可从 HTTP Query 参数解析并构建过滤条件。

- **局部查询**: 
  - `GetAppState(userID, sourceID, appName)` 返回指定应用的 `AppFlowState`。
  - `GetTask(taskID)` 返回指定任务的 `TaskState`。
  - `GetTasksByApp(appName, userID)` 按应用和用户维度收集所有相关任务。
  - `GetTasksByUser(userID)` 按用户维度收集所有任务。
  - `GetAppsByUser(userID)` 按用户维度收集该用户所有应用的 `AppFlowState`。

服务的生命周期控制通过 `Start()` 和 `Stop()` 方法实现：`Start()` 启动内部的 `StateCollector`，`Stop()` 停止采集器。服务通常在模块初始化时创建，并通过依赖注入提供给 HTTP API 层使用。
## 3. 核心数据模型

`runtime` 模块通过以下数据模型对外暴露运行态信息：

- **`AppFlowState`**: 应用运行状态，记录每个应用在当前用户和来源下的运行阶段（fetching、installing、running、upgrading、uninstalling、failed、stopped）、健康状态、版本等信息。注意：`Stage` 字段当前仅在采集阶段计算，未在实际业务中使用，有效性待验证。

- **`TaskState`**: 任务执行状态，记录安装/卸载/升级/克隆等后台任务的状态、进度、时间戳、结果等信息。

- **`ComponentStatus`**: 组件健康状态，记录 `appinfo_module`、`syncer`、`hydrator`、`cache`、`task_module` 等子模块的运行状态和关键指标。

- **`ChartRepoStatus`**: Chart Repo 服务状态，聚合应用处理进度、镜像下载/分析状态、任务队列等信息。

- **`RuntimeSnapshot`**: 完整的运行态快照，包含上述所有状态映射以及聚合统计信息 `RuntimeSummary`（总应用数、总任务数、各状态任务数量、健康/不健康应用数量等）。

所有数据模型仅存在于内存中，不单独持久化。详细字段定义请参考 `internal/v2/runtime/types.go`。

## 4. API 接口

**位置**: `pkg/v2/api/runtime.go`

runtime 模块提供以下 HTTP 接口：

- `GET /api/v2/runtime/state` - 获取运行时状态快照
- `GET /api/v2/runtime/dashboard` - 获取运行时仪表板
- `GET /api/v2/runtime/dashboard-app` - 获取应用处理流程仪表板

