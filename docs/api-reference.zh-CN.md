[English](./api-reference.md) | [中文版 (Chinese)](./api-reference.zh-CN.md)

# API 接口参考

本文档提供 Market 系统中所有 API 接口的完整参考。

## 基础 URL

所有 API 接口的前缀为 `/app-store/api/v2`。

## 响应格式

所有 API 响应遵循标准格式：

```json
{
  "success": boolean,
  "message": string,
  "data": object | array | null
}
```

## 错误码

常见 HTTP 状态码：
- `200 OK`: 请求成功
- `400 Bad Request`: 请求参数无效
- `401 Unauthorized`: 认证失败
- `404 Not Found`: 资源未找到
- `500 Internal Server Error`: 服务器错误
- `503 Service Unavailable`: 服务暂时不可用

## 认证

大多数接口需要用户认证。用户信息从请求头中提取：
- `X-Bfl-User`: 从请求头获取用户 ID（当前版本）
- `X-Authorization`: 基于 Token 的认证（旧版，已废弃）

---

## 市场数据接口

### GET /market/hash

获取当前用户的市场数据哈希值。

**响应:**
```json
{
  "success": true,
  "message": "Market hash retrieved successfully",
  "data": {
    "hash": string
  }
}
```

### GET /market/data

获取当前用户的完整市场数据（包含 AppInfoLatest 和 AppStateLatest）。

**响应:**
```json
{
  "success": true,
  "message": "Market data retrieved successfully",
  "data": {
    "user_data": {
      "sources": object,
      "hash": string
    },
    "user_id": string,
    "timestamp": number
  }
}
```

---

## 应用信息接口

**注意:** 应用信息接口（`POST /apps`）和市场数据接口（`GET /market/data`）在业务上是相同的，只是请求路径和格式不同。

### POST /apps

获取特定应用信息（支持批量查询）。

**请求体:**
```json
{
  "apps": [
    {
      "appid": string,
      "sourceDataName": string
    }
  ]
}
```

**响应:**
```json
{
  "success": true,
  "message": "Apps information retrieved successfully",
  "data": {
    "apps": array,
    "total_count": number,
    "not_found": array
  }
}
```

### POST /apps/upload

上传应用安装包。

**请求:** Multipart 表单数据
- `chart`: 文件（必需，.tgz 或 .tar.gz，最大 100MB）
- `source`: 字符串（可选，默认: "upload"）

**响应:**
```json
{
  "success": true,
  "message": "App package uploaded and processed successfully",
  "data": {
    "filename": string,
    "source": string,
    "user_id": string,
    "app_info": object
  }
}
```

### POST /apps/version-history

获取应用版本历史。

**请求体:**
```json
{
  "appName": string
}
```

**响应:**
```json
{
  "success": true,
  "message": "Version history retrieved successfully",
  "data": {
    "data": [
      {
        "appName": string,
        "version": string,
        "versionName": string,
        "mergedAt": string,
        "upgradeDescription": string
      }
    ]
  }
}
```

---

## 应用状态接口

### GET /market/state

获取当前用户的应用状态数据（仅 AppStateLatest）。

**响应:**
```json
{
  "success": true,
  "message": "Market state retrieved successfully",
  "data": {
    "user_data": {
      "sources": object,
      "hash": string
    },
    "user_id": string,
    "timestamp": number
  }
}
```

---

## 应用操作接口

### GET /settings/system-status

获取系统状态聚合（安装操作的前置接口）。

**响应:**
```json
{
  "success": true,
  "message": "System status aggregation complete",
  "data": {
    "user_info": object,
    "cur_user_resource": object,
    "cluster_resource": object,
    "terminus_version": object
  }
}
```

### POST /apps/{id}/install

安装应用。

**路径参数:**
- `id`: 应用 ID

**请求体:**
```json
{
  "source": string,
  "app_name": string,
  "version": string,
  "sync": boolean,
  "envs": [
    {
      "env_name": string,
      "env_value": string
    }
  ]
}
```

**响应（异步）:**
```json
{
  "success": true,
  "message": "App installation started successfully",
  "data": {
    "task_id": string
  }
}
```

**响应（同步）:**
```json
{
  "success": true,
  "message": "App installation completed successfully",
  "data": object
}
```

### POST /apps/{id}/clone

克隆应用（使用不同配置创建新实例）。

**路径参数:**
- `id`: 应用 ID

**请求体:**
```json
{
  "source": string,
  "app_name": string,
  "title": string,
  "sync": boolean,
  "envs": [
    {
      "env_name": string,
      "env_value": string
    }
  ],
  "entrances": [
    {
      "name": string,
      "port": number,
      "protocol": string
    }
  ]
}
```

**响应（异步）:**
```json
{
  "success": true,
  "message": "App clone installation started successfully",
  "data": {
    "task_id": string
  }
}
```

**响应（同步）:**
```json
{
  "success": true,
  "message": "App clone installation completed successfully",
  "data": object
}
```

### DELETE /apps/{id}/install

取消应用安装。

**路径参数:**
- `id`: 应用名称

**请求体:**
```json
{
  "sync": boolean
}
```

**响应（异步）:**
```json
{
  "success": true,
  "message": "App installation cancellation started successfully",
  "data": {
    "task_id": string
  }
}
```

**响应（同步）:**
```json
{
  "success": true,
  "message": "App installation cancellation completed successfully",
  "data": object
}
```

### PUT /apps/{id}/upgrade

升级应用。

**路径参数:**
- `id`: 应用 ID

**请求体:**
```json
{
  "source": string,
  "app_name": string,
  "version": string,
  "sync": boolean,
  "envs": [
    {
      "env_name": string,
      "env_value": string
    }
  ]
}
```

**响应（异步）:**
```json
{
  "success": true,
  "message": "App upgrade started successfully",
  "data": {
    "task_id": string
  }
}
```

**响应（同步）:**
```json
{
  "success": true,
  "message": "App upgrade completed successfully",
  "data": object
}
```

### DELETE /apps/{id}

卸载应用。

**路径参数:**
- `id`: 应用名称

**请求体:**
```json
{
  "sync": boolean,
  "all": boolean
}
```

**响应（异步）:**
```json
{
  "success": true,
  "message": "App uninstallation started successfully",
  "data": {
    "task_id": string
  }
}
```

**响应（同步）:**
```json
{
  "success": true,
  "message": "App uninstallation completed successfully",
  "data": object
}
```

### POST /apps/open

打开应用。

**请求体:**
```json
{
  "id": string
}
```

**响应:**
```json
{
  "success": true,
  "message": "Application opened successfully",
  "data": null
}
```

### POST /apps/resume

恢复暂停的应用。

**请求体:**
```json
{
  "appName": string,
  "all": boolean
}
```

**响应:**
```json
{
  "success": true,
  "message": "Application resumed successfully",
  "data": {
    "appName": string,
    "type": string,
    "result": object
  }
}
```

### POST /apps/stop

停止运行中的应用。

**请求体:**
```json
{
  "appName": string,
  "all": boolean
}
```

**请求体参数:**
- `appName`: 应用名称（必需）
- `all`: 是否停止所有实例，默认为 false（可选）

**响应:**
```json
{
  "success": true,
  "message": "Application stopped successfully",
  "data": {
    "appName": string,
    "type": string,
    "result": object
  }
}
```

### DELETE /local-apps/delete

删除本地源应用。

**请求体:**
```json
{
  "app_name": string,
  "app_version": string
}
```

**响应:**
```json
{
  "success": true,
  "message": "App deleted successfully from upload source",
  "data": {
    "app_name": string,
    "app_version": string,
    "user_id": string,
    "source": string,
    "deleted_at": number
  }
}
```

---

## 支付接口

### GET /sources/{source}/apps/{id}/payment-status

检查应用支付状态（推荐路由，带源参数）。

**路径参数:**
- `source`: 市场源名称
- `id`: 应用 ID

**请求头:**
- `X-Forwarded-Host`: 必需，用于构建回调 URL

**响应:**
```json
{
  "success": true,
  "message": "App payment status retrieved successfully",
  "data": {
    "app_id": string,
    "source": string,
    "requires_purchase": boolean,
    "status": string,
    "message": string,
    "payment_error": string
  }
}
```

### GET /apps/{id}/payment-status

检查应用支付状态（旧版路由，搜索所有源）。

**路径参数:**
- `id`: 应用 ID

**请求头:**
- `X-Forwarded-Host`: 必需，用于构建回调 URL

**响应:**
```json
{
  "success": true,
  "message": "App payment status retrieved successfully",
  "data": {
    "app_id": string,
    "source": string,
    "requires_purchase": boolean,
    "status": string,
    "message": string,
    "payment_error": string
  }
}
```

### POST /sources/{source}/apps/{id}/purchase

购买应用。

**路径参数:**
- `source`: 市场源名称
- `id`: 应用 ID

**请求头:**
- `X-Forwarded-Host`: 必需，用于用户 DID 解析

**响应:**
```json
{
  "success": true,
  "message": "OK",
  "data": {
    "app_id": string,
    "source": string,
    "user": string
  }
}
```

### POST /payment/submit-signature

提交签名以处理支付。

**请求体:**
```json
{
  "jws": string,
  "application_verifiable_credential": {
    "productId": string,
    "txHash": string
  },
  "product_credential_manifest": object
}
```

**请求头:**
- `X-Bfl-User`: 必需
- `X-Forwarded-Host`: 必需

**响应:**
```json
{
  "success": true,
  "message": "Signature submission processed successfully",
  "data": {
    "jws": string,
    "sign_body": string,
    "user": string,
    "status": string
  }
}
```

### POST /payment/start-polling

前端支付完成后启动支付轮询。

**请求体:**
```json
{
  "source_id": string,
  "app_id": string,
  "tx_hash": string,
  "product_id": string
}
```

**请求头:**
- `X-Forwarded-Host`: 必需

**响应:**
```json
{
  "success": true,
  "message": "Payment polling started successfully",
  "data": {
    "success": boolean,
    "message": string,
    "status": string
  }
}
```

### POST /payment/frontend-start

前端通知支付就绪。

**请求体:**
```json
{
  "source_id": string,
  "app_id": string,
  "product_id": string,
  "frontend_data": object
}
```

**请求头:**
- `X-Forwarded-Host`: 必需

**响应:**
```json
{
  "success": true,
  "message": "Frontend payment state updated",
  "data": {
    "app_id": string,
    "source": string,
    "user": string,
    "frontend_data": object
  }
}
```

### POST /payment/fetch-signature-callback

获取签名回调（来自 LarePass）。

**请求体:**
```json
{
  "signed": boolean,
  "jws": string,
  "application_verifiable_credential": {
    "productId": string,
    "txHash": string
  },
  "product_credential_manifest": object
}
```

**请求头:**
- `X-Bfl-User`: 必需

**响应:**
```json
{
  "success": true,
  "message": "Fetch signature callback processed",
  "data": {
    "status": string,
    "user": string,
    "jws_present": boolean
  }
}
```

---

## 设置接口

### GET /settings/market-source

获取市场源配置。

**响应:**
```json
{
  "success": true,
  "message": "Market source configuration retrieved successfully",
  "data": array
}
```

### POST /settings/market-source

添加市场源配置。

**请求体:**
```json
{
  "id": string,
  "name": string,
  "type": "local" | "remote",
  "base_url": string,
  "description": string
}
```

**响应:**
```json
{
  "success": true,
  "message": "Market source added successfully",
  "data": array
}
```

### DELETE /settings/market-source/{id}

删除市场源配置。

**路径参数:**
- `id`: 市场源 ID

**响应:**
```json
{
  "success": true,
  "message": "Market source deleted successfully",
  "data": array
}
```

### GET /settings/market-settings

获取当前用户的市场设置。

**响应:**
```json
{
  "success": true,
  "message": "Market settings retrieved successfully",
  "data": object
}
```

### PUT /settings/market-settings

更新当前用户的市场设置。

**请求体:**
```json
{
  // MarketSettings 对象
}
```

**响应:**
```json
{
  "success": true,
  "message": "Market settings updated successfully",
  "data": object
}
```

---

## 开发调试接口

### GET /market/debug-memory

获取市场调试内存信息（仅用于开发调试）。

**响应:**
```json
{
  "success": true,
  "message": "Market information retrieved successfully",
  "data": {
    "users_data": object,
    "cache_stats": object,
    "total_users": number,
    "total_sources": number
  }
}
```

### GET /diagnostic

获取缓存和 Redis 诊断信息（仅用于开发调试）。

**响应:**
```json
{
  "success": true,
  "message": "Diagnostic information retrieved successfully",
  "data": {
    "cache_stats": object,
    "users_data": object,
    "total_users": number,
    "total_sources": number,
    "is_running": boolean
  }
}
```

### POST /reload

强制从 Redis 重新加载缓存数据（仅用于开发调试）。

**响应:**
```json
{
  "success": true,
  "message": "Cache data reloaded successfully from Redis",
  "data": null
}
```

### POST /cleanup

清理无效的待处理数据（仅用于开发调试）。

**响应:**
```json
{
  "success": true,
  "message": "Cleanup completed successfully",
  "data": {
    "cleaned_count": number
  }
}
```

### GET /logs

按特定条件查询日志（仅用于开发调试）。

**查询参数:**
- `start`: 偏移量（默认: 0）
- `size`: 限制数量（默认: 100，最大: 1000）
- `type`: 按日志类型过滤
- `app`: 按应用名称过滤
- `start_time`: 按开始时间过滤（Unix 时间戳）
- `end_time`: 按结束时间过滤（Unix 时间戳）

**响应:**
```json
{
  "success": true,
  "message": "Logs retrieved successfully",
  "data": {
    "records": array,
    "total_count": number,
    "start": number,
    "size": number,
    "requested_size": number
  }
}
```

---

## 运行时状态接口

### GET /runtime/state

获取运行时状态（支持可选过滤器）。

**查询参数:**
- `user_id`: 按用户 ID 过滤
- `source_id`: 按源 ID 过滤
- `app_name`: 按应用名称过滤
- `task_status`: 按任务状态过滤
- `task_type`: 按任务类型过滤

**响应:**
```json
{
  "success": true,
  "message": "Runtime state retrieved successfully",
  "data": {
    "app_states": object,
    "tasks": array,
    "components": array,
    "timestamp": number
  }
}
```

### GET /runtime/dashboard

获取运行时仪表板（HTML 页面）。

**响应:** 包含运行时状态可视化的 HTML 页面

### GET /runtime/dashboard-app

获取应用处理流程的运行时仪表板（HTML 页面）。

**响应:** 包含应用处理流程可视化的 HTML 页面

