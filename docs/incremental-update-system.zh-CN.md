# 前端数据同步系统

[English](./incremental-update-system.md)

## 概述

Market 系统提供了 hash、data 和 appinfo 接口，使前端应用可以实现高效的增量更新和定点更新策略。通过使用哈希比较来检测数据变化，结合两层数据获取方式（先获取基本信息，按需获取详细信息），前端可以实现高效的数据同步。

## 核心原理

系统使用哈希值来表示市场数据的当前状态。当哈希值发生变化时，表示底层数据已被修改。前端采用两层数据获取方式：
1. 通过 `data` 接口获取完整的应用列表和每个应用的基本信息
2. 通过 `appinfo` 接口按需获取特定应用的详细信息

这实现了增量更新（仅在哈希变化时）和定点更新（按需获取特定应用）的统一机制。

## API 接口

**位置**: `pkg/v2/api/app.go`

### 1. 哈希接口

- **接口**: `GET /api/v2/market/hash`
- **用途**: 获取用户市场数据的当前哈希值
- **响应**: 
  ```json
  {
    "hash": "abc123def456..."
  }
  ```
- **使用**: 前端应定期调用此接口以检查数据是否发生变化

### 2. 数据接口

- **接口**: `GET /api/v2/market/data`
- **用途**: 获取完整的应用列表和每个应用的基本信息
- **响应**: 包含过滤后的用户数据：
  - `AppInfoLatest`: 应用数组，每个应用仅包含基本信息（`AppSimpleInfo`），如：
    - `app_id`: 应用 ID
    - `app_name`: 应用名称
    - `app_icon`: 应用图标
    - `app_description`: 简要描述
    - `app_version`: 版本号
    - `app_title`: 显示标题
    - `categories`: 分类
    - `support_arch`: 支持的架构
  - `Others`: 主题、推荐、页面等
  - `Hash`: 数据的当前哈希值
- **重要说明**: 此接口**不返回**完整的应用信息。它只提供应用列表和每个应用的基本元数据。
- **使用**: 当哈希值变化或初始加载时，获取应用目录

### 3. 应用信息接口

- **接口**: `POST /api/v2/apps`
- **用途**: 获取特定应用的完整详细信息
- **请求体**:
  ```json
  {
    "apps": [
      {
        "appid": "app-id",
        "sourceDataName": "source-name"
      }
    ]
  }
  ```
- **响应**: 返回完整的 `AppInfoLatestData`，包括：
  - 完整的 `RawData`: 完整的应用元数据
  - `AppInfo`: 详细信息，包括应用入口、镜像分析、价格等
  - `Values`: 配置值
  - 包信息
  - 所有其他详细字段
- **使用**: 按需获取特定应用的详细信息


## 重要说明

### 状态数据是动态的

`GET /api/v2/market/state` 接口仅返回 `AppStateLatest` 数据（应用运行状态，如运行中、安装中、已停止等）。这些数据是**动态且不断变化的**，因此：

- **不要**对状态数据使用基于哈希的更新机制
- 状态数据应使用不同的策略单独获取
- 前端应组合：
  - 静态基本数据（来自 `/market/data`）
  - 动态状态数据（来自 `/market/state`）
  - 详细的应用信息（按需来自 `/api/v2/apps`）
  
  以构建完整的应用视图

### Hash 更新场景

哈希值在以下场景中会被重新计算并更新：

1. **应用处理完成**: 当应用从 `AppInfoLatestPending` 移动到 `AppInfoLatest` 后（由 `DataWatcher` 处理）
2. **初始加载**: 当用户数据的 hash 为空时（首次加载或初始化时）
3. **状态修正**: 当 `StatusCorrectionChecker` 应用状态修正后，会重新计算所有受影响用户的 hash
4. **状态数据更新**: 当状态缓存更新后，会重新计算所有用户的 hash（由 `DataWatcher` State 处理）
5. **镜像信息更新**: 当镜像信息更新后，会重新计算所有用户的 hash（由 `DataWatcher` Repo 处理）

**代码位置**: 
- `internal/v2/appinfo/datawatcher_app.go` - 应用处理和 hash 计算
- `internal/v2/appinfo/status_correction_check.go` - 状态修正后的 hash 更新
- `internal/v2/appinfo/datawatcher_state.go` - 状态更新后的 hash 重新计算
- `internal/v2/appinfo/datawatcher_repo.go` - 镜像信息更新后的 hash 重新计算

