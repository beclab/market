# Helm Repository Service

这是一个完整的 Helm Chart Repository 服务实现，支持标准的 Helm 协议，并提供增强的管理功能。

## 📁 文件结构

```
market/pkg/v2/helm/
├── README.md           # 本文档
├── service.go          # 主服务和路由配置
├── types.go            # 数据类型定义
├── repository.go       # 标准 Helm Repository API
├── charts.go           # Chart 管理 API
└── system.go           # 系统管理 API
```

## 🚀 功能特性

### 标准 Helm Repository API
- ✅ `GET /index.yaml` - 获取仓库索引文件（兼容 Helm 客户端）
- ✅ `GET /charts/{filename}.tgz` - 下载 chart 包

### 增强管理 API (`/api/v1` 前缀)
- ✅ `GET /charts` - 列出所有 charts
- ✅ `POST /charts` - 上传 chart 包
- ✅ `GET /charts/{name}` - 获取 chart 信息
- ✅ `DELETE /charts/{name}/{version}` - 删除 chart 版本
- ✅ `GET /charts/{name}/versions` - 获取 chart 版本列表
- ✅ `GET /charts/search` - 搜索 charts
- ✅ `GET /charts/{name}/{version}/metadata` - 获取 chart 元数据

### 系统管理 API
- ✅ `GET /health` - 健康检查
- ✅ `GET /metrics` - 获取统计指标
- ✅ `GET /config` - 获取仓库配置
- ✅ `PUT /config` - 更新仓库配置
- ✅ `POST /index/rebuild` - 重建索引

## 📋 文件说明

### `service.go`
- **HelmRepository** 结构体定义
- **SetupHelmRoutes()** 路由配置
- **StartHelmRepositoryServer()** 独立服务器启动（端口 82）
- CORS 和日志中间件
- 自动启动功能（通过 `init()` 函数）

### `types.go`
包含所有数据类型定义：
- **Chart 管理类型**：`ChartSummary`, `ChartVersion`, `ChartMetadata` 等
- **系统管理类型**：`HealthStatus`, `RepositoryMetrics`, `RepositoryConfig` 等
- **响应类型**：各种 API 响应结构

### `repository.go`
标准 Helm Repository API 实现：
- **getRepositoryIndex()** - 生成和返回 index.yaml
- **downloadChart()** - 处理 chart 包下载

### `charts.go`
Chart 管理 API 实现：
- **listCharts()** - 列出 charts
- **uploadChart()** - 上传 chart
- **getChartInfo()** - 获取 chart 信息
- **deleteChartVersion()** - 删除版本
- **getChartVersions()** - 获取版本列表
- **searchCharts()** - 搜索功能
- **getChartMetadata()** - 获取元数据

### `system.go`
系统管理 API 实现：
- **healthCheck()** - 健康检查
- **getMetrics()** - 统计指标
- **getRepositoryConfig()** - 配置管理
- **updateRepositoryConfig()** - 配置更新
- **rebuildIndex()** - 索引重建

## 🔧 使用方式

### 自动启动
服务会在包导入时自动启动：
```go
import _ "market/pkg/v2/helm"
```

### 手动启动
```go
import "market/pkg/v2/helm"

func main() {
    if err := helm.StartHelmRepositoryServer(); err != nil {
        log.Fatal(err)
    }
}
```

### 作为 Helm Repository 使用
```bash
# 添加仓库
helm repo add myrepo http://localhost:82

# 更新仓库索引
helm repo update

# 搜索 charts
helm search repo myrepo

# 安装 chart
helm install myapp myrepo/mychart
```

### 管理 API 使用
```bash
# 健康检查
curl http://localhost:82/api/v1/health

# 列出所有 charts
curl http://localhost:82/api/v1/charts

# 搜索 charts
curl "http://localhost:82/api/v1/charts/search?q=nginx"

# 上传 chart
curl -X POST -F "chart=@mychart-1.0.0.tgz" http://localhost:82/api/v1/charts
```

## 📝 注释说明

每个 API 方法都包含详细的中英文注释：
- **Purpose/作用**：接口的目的和功能
- **Parameters/参数**：URL 参数、查询参数、请求体格式
- **Response/响应**：返回数据格式、HTTP 状态码
- **Implementation notes/实现要点**：实现时需要考虑的要点

## 🔄 实现状态

当前所有接口方法都是**空实现**（仅包含 TODO 注释），方便后续根据具体需求进行实现。每个方法都保留了完整的注释和实现指导。

## 🌟 特点

- ✅ **完全兼容 Helm 标准**：支持所有标准 Helm Repository 协议
- ✅ **模块化设计**：按功能拆分文件，便于维护
- ✅ **详细注释**：每个接口都有完整的中英文注释
- ✅ **独立运行**：可在 82 端口独立启动
- ✅ **中间件支持**：CORS 和日志记录
- ✅ **空实现**：所有方法都是空的，方便后续实现

## 用户上下文支持

所有 API 接口都支持基于用户上下文的数据隔离和权限控制：

### 必需的请求头
```http
X-Market-User: user123        # 用户ID
X-Market-Source: web-console  # 源系统标识符
```

### 数据隔离
- 每个用户只能看到和操作自己有权限的 charts
- 基于 `X-Market-User` 和 `X-Market-Source` 进行数据过滤
- 支持多租户环境

## 缓存管理器集成

服务与 AppInfo 缓存管理器集成，动态生成 Helm Repository 索引：

### 数据流程
1. 通过 `X-Market-User` 从 `CacheData` 中找到 `UserData`
2. 通过 `X-Market-Source` 从 `UserData` 中找到 `SourceData`
3. `SourceData` 中的 `AppInfoLatest` 包含所有可安装的应用
4. 使用 `AppInfoLatest` 动态生成 `getRepositoryIndex` 的返回结果

### 初始化示例

```go
package main

import (
    "log"
    "market/internal/v2/appinfo"
    "market/pkg/v2/helm"
)

func main() {
    // 1. 创建 Redis 配置
    redisConfig := &appinfo.RedisConfig{
        Host:     "localhost",
        Port:     6379,
        Password: "",
        DB:       0,
        Timeout:  10 * time.Second,
    }

    // 2. 创建 Redis 客户端
    redisClient, err := appinfo.NewRedisClient(redisConfig)
    if err != nil {
        log.Fatalf("Failed to create Redis client: %v", err)
    }
    defer redisClient.Close()

    // 3. 创建用户配置
    userConfig := &appinfo.UserConfig{
        UserList:          []string{"user1", "user2", "admin"},
        AdminList:         []string{"admin"},
        MaxSourcesPerUser: 10,
    }

    // 4. 创建缓存管理器
    cacheManager := appinfo.NewCacheManager(redisClient, userConfig)

    // 5. 启动缓存管理器
    if err := cacheManager.Start(); err != nil {
        log.Fatalf("Failed to start cache manager: %v", err)
    }
    defer cacheManager.Stop()

    // 6. 启动 Helm Repository 服务器（带缓存管理器）
    log.Printf("Starting Helm Repository server with cache manager...")
    if err := helm.StartHelmRepositoryServerWithCacheManager(cacheManager); err != nil {
        log.Fatalf("Failed to start Helm Repository server: %v", err)
    }
}
```

### 独立启动（不带缓存管理器）

```go
package main

import (
    "log"
    "market/pkg/v2/helm"
)

func main() {
    // 启动基础 Helm Repository 服务器
    log.Printf("Starting basic Helm Repository server...")
    if err := helm.StartHelmRepositoryServer(); err != nil {
        log.Fatalf("Failed to start Helm Repository server: %v", err)
    }
}
```

## 使用示例

### 1. 获取仓库索引

```bash
curl -H "X-Market-User: user123" \
     -H "X-Market-Source: web-console" \
     http://localhost:82/index.yaml
```

响应示例：
```yaml
apiVersion: v1
generated: "2023-12-01T10:00:00Z"
entries:
  nginx:
    - name: nginx
      version: "1.2.3"
      appVersion: "1.21.0"
      description: "NGINX web server"
      home: "https://nginx.org"
      urls:
        - "/charts/nginx-1.2.3.tgz"
      created: "2023-12-01T09:00:00Z"
      digest: "sha256:1234567890abcdef..."
      maintainers:
        - name: "NGINX Team"
      keywords:
        - "web"
        - "server"
      icon: "https://example.com/nginx-icon.png"
```

### 2. 使用 Helm 客户端

```bash
# 添加仓库
helm repo add myrepo http://localhost:82 \
  --header "X-Market-User=user123" \
  --header "X-Market-Source=web-console"

# 更新仓库
helm repo update

# 搜索 charts
helm search repo myrepo/

# 安装 chart
helm install my-nginx myrepo/nginx
```

### 3. 管理 API 使用

```bash
# 列出所有 charts
curl -H "X-Market-User: user123" \
     -H "X-Market-Source: web-console" \
     http://localhost:82/api/v1/charts

# 搜索 charts
curl -H "X-Market-User: user123" \
     -H "X-Market-Source: web-console" \
     "http://localhost:82/api/v1/charts/search?q=nginx"

# 获取健康状态
curl -H "X-Market-User: user123" \
     -H "X-Market-Source: web-console" \
     http://localhost:82/api/v1/health
```

## 📝 注释说明

每个 API 方法都包含详细的中英文注释：
- **Purpose/作用**：接口的目的和功能
- **Parameters/参数**：URL 参数、查询参数、请求体格式
- **Response/响应**：返回数据格式、HTTP 状态码
- **Implementation notes/实现要点**：实现时需要考虑的要点

## 🔄 实现状态

当前所有接口方法都是**空实现**（仅包含 TODO 注释），方便后续根据具体需求进行实现。每个方法都保留了完整的注释和实现指导。

## 🌟 特点

- **完全兼容 Helm 标准协议**：支持所有标准 Helm 客户端
- **模块化设计**：代码按功能拆分，便于维护和扩展
- **用户级别数据隔离**：基于用户上下文的多租户支持
- **动态索引生成**：从缓存数据实时生成 Helm Repository 索引
- **详细的审计日志**：记录所有用户操作
- **CORS 支持**：支持浏览器直接访问
- **缓存优化**：支持 ETag 和 Last-Modified 缓存机制

## 端口配置

- **服务端口**：82
- **协议**：HTTP
- **路径前缀**：
  - 标准 Helm API：`/`
  - 管理 API：`/api/v1/`

## 错误处理

所有 API 都包含完整的错误处理和 HTTP 状态码：

- `200` - 成功
- `400` - 请求错误（缺少必需头部等）
- `401` - 未授权（无效用户上下文）
- `403` - 禁止访问（权限不足）
- `404` - 资源未找到
- `500` - 服务器内部错误
- `503` - 服务不可用（缓存管理器未初始化）

## 开发状态

- ✅ 用户上下文支持
- ✅ 缓存管理器集成
- ✅ 动态索引生成
- ✅ 标准 Helm Repository API 框架
- ✅ 增强管理 API 框架
- ✅ 系统管理 API 框架
- ⏳ Chart 下载功能（待实现）
- ⏳ Chart 上传功能（待实现）
- ⏳ 完整的 CRUD 操作（待实现）

所有方法目前都是空实现，但包含完整的接口定义、参数说明、响应格式和错误处理，便于后续开发。 