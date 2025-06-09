# Settings API Documentation
# 设置API文档

## Overview | 概述

The Settings API provides endpoints to manage application market source configuration. The market source URL is stored both in memory and Redis for persistence.

设置API提供了管理应用市场源配置的接口。市场源URL既存储在内存中，也持久化到Redis中。

## Endpoints | 接口

### 1. Get Market Source Configuration | 获取市场源配置

**GET** `/api/v2/settings/market-source`

获取当前的应用市场源配置。

#### Response | 响应

```json
{
  "success": true,
  "message": "Market source configuration retrieved successfully",
  "data": {
    "url": "https://appstore-server-test.bttcdn.com",
    "updated_at": "2024-01-15T10:30:00Z"
  }
}
```

#### Error Responses | 错误响应

- `404 Not Found`: Market source configuration not found
- `500 Internal Server Error`: Settings manager not initialized

### 2. Set Market Source Configuration | 设置市场源配置

**PUT** `/api/v2/settings/market-source`

设置新的应用市场源配置。

#### Request Body | 请求体

```json
{
  "url": "https://new-market-source.example.com"
}
```

#### Response | 响应

```json
{
  "success": true,
  "message": "Market source configuration updated successfully",
  "data": {
    "url": "https://new-market-source.example.com",
    "updated_at": "2024-01-15T10:35:00Z"
  }
}
```

#### Error Responses | 错误响应

- `400 Bad Request`: Invalid request body or empty URL
- `500 Internal Server Error`: Failed to save configuration or settings manager not initialized

## Configuration | 配置

### Environment Variables | 环境变量

The following environment variables are used for Redis connection and default configuration:

以下环境变量用于Redis连接和默认配置：

- `REDIS_HOST`: Redis服务器地址 (默认: localhost)
- `REDIS_PORT`: Redis端口 (默认: 6379)
- `REDIS_PASSWORD`: Redis密码 (默认: 空)
- `REDIS_DB`: Redis数据库编号 (默认: 0)
- `SYNCER_REMOTE`: 默认市场源URL (如果Redis中没有配置时使用)

### Redis Storage | Redis存储

Market source configuration is stored in Redis using hash structure:

市场源配置使用哈希结构存储在Redis中：

- **Key**: `market:source:config`
- **Fields**:
  - `url`: Market source URL
  - `updated_at`: Unix timestamp of last update

## Testing | 测试

Use the provided test script to verify the API functionality:

使用提供的测试脚本验证API功能：

```bash
./test_settings.sh
```

## Implementation Details | 实现细节

### Architecture | 架构

1. **API Layer** (`pkg/v2/api/settings.go`): HTTP handlers for REST endpoints
2. **Business Logic** (`internal/v2/settings/manager.go`): Settings management logic
3. **Data Access** (`internal/v2/settings/redis.go`): Redis client implementation
4. **Types** (`internal/v2/settings/types.go`): Data structures and interfaces

### Data Flow | 数据流

1. **Initialization**: Load from Redis → Fallback to environment variable → Save to Redis and memory
2. **Get Operation**: Return from memory (thread-safe read)
3. **Set Operation**: Save to Redis → Update memory (thread-safe write)

### Thread Safety | 线程安全

The settings manager uses `sync.RWMutex` to ensure thread-safe access to the in-memory configuration.

设置管理器使用`sync.RWMutex`确保对内存配置的线程安全访问。 