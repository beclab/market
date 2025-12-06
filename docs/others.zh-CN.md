# 其他功能

## 诊断功能

Market 系统提供了用于运维的诊断和维护接口。这些接口允许管理员检查系统健康状态、管理缓存和执行清理操作。

**位置**: `internal/v2/appinfo/diagnostic.go`, `pkg/v2/api/app.go`

**可用接口**:
- `GET /api/v2/diagnostic` - 获取缓存和 Redis 诊断信息
- `POST /api/v2/reload` - 强制从 Redis 重新加载缓存数据
- `POST /api/v2/cleanup` - 清理无效的 pending 数据

## 应用生命周期管理

Market 系统提供了用于管理应用生命周期操作的接口，包括恢复、停止和打开应用。

**位置**: `pkg/v2/api/system.go`

**可用接口**:
- `POST /api/v2/apps/resume` - 恢复应用（支持 app 和 middleware 类型）
- `POST /api/v2/apps/stop` - 停止应用（支持 app 和 middleware 类型）
- `POST /api/v2/apps/open` - 打开应用（通过系统服务器发送 intent）

## 版本历史查询

Market 系统提供了用于查询应用版本历史记录的接口。

**位置**: `pkg/v2/api/system.go`

**可用接口**:
- `POST /api/v2/apps/version-history` - 查询应用的版本历史记录

## 本地应用管理

Market 系统提供了用于管理本地上传应用的接口，包括上传安装包和删除上传源。

**位置**: `pkg/v2/api/app.go`

**可用接口**:
- `POST /api/v2/apps/upload` - 上传应用安装包
- `DELETE /api/v2/local-apps/delete` - 删除上传源的应用（需要应用已卸载）

## 应用克隆

Market 系统支持克隆已安装的应用并自定义配置。克隆应用使用基于原始应用名称和请求哈希的命名规则，允许同一应用的不同配置实例共存。

**位置**: `pkg/v2/api/task.go`, `internal/v2/task/app_clone.go`

**可用接口**:
- `POST /api/v2/apps/{id}/clone` - 克隆已安装的应用

**关键特性**:
- 克隆应用命名：使用 `rawAppName + requestHash` 作为新应用名称（其中 `requestHash` 是整个克隆请求的 SHA256 哈希值的前 6 个字符，包括 source、app_name、title、envs 和 entrances）
- 自定义标题：支持为克隆应用设置自定义显示标题
- 自定义入口：支持为克隆应用配置自定义入口
- 前置条件：克隆操作要求原始应用必须已安装

## 设置管理扩展

Market 系统提供了用于管理用户市场设置的接口，允许用户配置其市场偏好。

**位置**: `pkg/v2/api/system.go`, `internal/v2/settings/`

**可用接口**:
- `GET /api/v2/settings/market-settings` - 获取用户市场设置
- `PUT /api/v2/settings/market-settings` - 更新用户市场设置

## 多市场源支持

Market 系统支持多个市场源，允许管理员配置和管理不同的应用源（包括远程源和本地源）。系统支持根据需要启用或禁用源。

**位置**: `pkg/v2/api/system.go`, `internal/v2/settings/manager.go`, `internal/v2/settings/types.go`

**可用接口**:
- `GET /api/v2/settings/market-source` - 获取市场源配置
- `POST /api/v2/settings/market-source` - 添加新的市场源
- `DELETE /api/v2/settings/market-source/{id}` - 根据 ID 删除市场源

**关键特性**:
- **源类型**: 支持两种类型的市场源：
  - `remote`: 远程市场源，由同步器定期同步
  - `local`: 本地市场源，不会被同步（通常用于本地上传的应用）
- **启用/禁用状态**: 每个市场源都有一个 `is_active` 标志，用于控制是否使用该源。只有活跃的源才会在系统检索可用市场源时被包含。非活跃的源在同步操作和应用查询时会被忽略。
- **源选择**: 同步器在同步周期中只处理远程类型的源。本地源在同步时会被跳过，但仍可用于应用安装和查询。

**内存和接口支持方式**:
- **内存存储**: 市场源配置在 `SettingsManager` 中存储为 `MarketSourcesConfig` 结构，包含 `MarketSource` 对象数组。缓存中每个用户的数据（`UserData`）使用 `Sources map[string]*SourceData` 结构，其中 key 为源 ID，允许多个市场源为每个用户共存。
- **接口返回结构**: 接口响应按市场源组织数据，使用 map 结构。例如，在市场数据接口中，响应包含 `user_data.sources[sourceID]`，其中每个源 ID 映射到其对应的应用数据（`AppInfoLatest`、`AppStateLatest` 等）。这允许客户端独立访问来自不同市场源的数据。

## 系统状态检查

系统状态接口聚合来自多个来源的系统信息，作为安装应用接口的前置检查接口。它收集用户信息、资源可用性、集群资源和系统版本信息，以确保系统已准备好进行应用安装。

**位置**: `pkg/v2/api/system.go`

**可用接口**:
- `GET /api/v2/settings/system-status` - 获取系统状态聚合（用户信息、资源、版本等）

**使用说明**: 在启动应用安装之前应调用此接口，以验证系统就绪状态和资源可用性。

## 同步/异步任务执行

所有任务操作（安装、卸载、升级、克隆、取消）都支持 `sync` 参数，用于控制操作是等待完成还是立即返回。

**位置**: `pkg/v2/api/task.go`

**执行模式**:
- **同步模式** (`sync: true`): API 等待任务完成并返回最终结果。响应包含任务执行结果和执行过程中发生的任何错误。
- **异步模式** (`sync: false` 或省略): API 立即返回任务 ID。调用者可以使用任务 ID 稍后查询任务状态和结果。

**支持的操作**:
- `POST /api/v2/apps/{id}/install` - 安装应用
- `POST /api/v2/apps/{id}/uninstall` - 卸载应用
- `POST /api/v2/apps/{id}/upgrade` - 升级应用
- `POST /api/v2/apps/{id}/clone` - 克隆应用
- `POST /api/v2/apps/{id}/cancel` - 取消安装

**实现方式**:
所有用户触发的任务执行都使用相同的异步逻辑，不区分同步或异步。区别仅在于 API 处理器的响应方式：
- **同步模式**: 调用 `AddTask` 启动异步任务后，API 处理器创建一个 channel 并在其上阻塞（`<-done`）以等待任务完成。注册一个回调函数，当异步任务完成时关闭 channel 并保存结果。处理器随后返回任务结果。
- **异步模式**: 调用 `AddTask` 启动异步任务后，API 处理器立即返回任务 ID，不等待。注册一个简单的回调函数仅用于日志记录。
