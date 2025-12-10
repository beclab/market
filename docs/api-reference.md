[中文版 (Chinese)](./api-reference.zh-CN.md) | [English](./api-reference.md)

# API Reference

This document provides a complete reference for all API endpoints in the Market system.

## Base URL

All API endpoints are prefixed with `/app-store/api/v2`.

## Response Format

All API responses follow a standard format:

```json
{
  "success": boolean,
  "message": string,
  "data": object | array | null
}
```

## Error Codes

Common HTTP status codes:
- `200 OK`: Request successful
- `400 Bad Request`: Invalid request parameters
- `401 Unauthorized`: Authentication failed
- `404 Not Found`: Resource not found
- `500 Internal Server Error`: Server error
- `503 Service Unavailable`: Service temporarily unavailable

## Authentication

Most endpoints require user authentication. User information is extracted from request headers:
- `X-Bfl-User`: User ID from header (current version)
- `X-Authorization`: Token-based authentication (legacy, deprecated)

---

## Market Data APIs

### GET /market/hash

Get market data hash for the current user.

**Response:**
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

Get complete market data for the current user (includes AppInfoLatest and AppStateLatest).

**Response:**
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

## Application Information APIs

**Note:** Application information APIs (`POST /apps`) and market data APIs (`GET /market/data`) serve the same business purpose. They differ only in their request paths and formats.

### POST /apps

Get specific application information (supports multiple queries).

**Request Body:**
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

**Response:**
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

Upload application installation package.

**Request:** Multipart form data
- `chart`: File (required, .tgz or .tar.gz, max 100MB)
- `source`: String (optional, default: "upload")

**Response:**
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

Get application version history.

**Request Body:**
```json
{
  "appName": string
}
```

**Response:**
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

## Application State APIs

### GET /market/state

Get application state data for the current user (only AppStateLatest).

**Response:**
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

## Application Operation APIs

### GET /settings/system-status

Get system status aggregation (prerequisite for installation operations).

**Response:**
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

Install an application.

**Path Parameters:**
- `id`: Application ID

**Request Body:**
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

**Response (Async):**
```json
{
  "success": true,
  "message": "App installation started successfully",
  "data": {
    "task_id": string
  }
}
```

**Response (Sync):**
```json
{
  "success": true,
  "message": "App installation completed successfully",
  "data": object
}
```

### POST /apps/{id}/clone

Clone an application (create a new instance with different configuration).

**Path Parameters:**
- `id`: Application ID

**Request Body:**
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

**Response (Async):**
```json
{
  "success": true,
  "message": "App clone installation started successfully",
  "data": {
    "task_id": string
  }
}
```

**Response (Sync):**
```json
{
  "success": true,
  "message": "App clone installation completed successfully",
  "data": object
}
```

### DELETE /apps/{id}/install

Cancel application installation.

**Path Parameters:**
- `id`: Application name

**Request Body:**
```json
{
  "sync": boolean
}
```

**Response (Async):**
```json
{
  "success": true,
  "message": "App installation cancellation started successfully",
  "data": {
    "task_id": string
  }
}
```

**Response (Sync):**
```json
{
  "success": true,
  "message": "App installation cancellation completed successfully",
  "data": object
}
```

### PUT /apps/{id}/upgrade

Upgrade an application.

**Path Parameters:**
- `id`: Application ID

**Request Body:**
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

**Response (Async):**
```json
{
  "success": true,
  "message": "App upgrade started successfully",
  "data": {
    "task_id": string
  }
}
```

**Response (Sync):**
```json
{
  "success": true,
  "message": "App upgrade completed successfully",
  "data": object
}
```

### DELETE /apps/{id}

Uninstall an application.

**Path Parameters:**
- `id`: Application name

**Request Body:**
```json
{
  "sync": boolean,
  "all": boolean
}
```

**Response (Async):**
```json
{
  "success": true,
  "message": "App uninstallation started successfully",
  "data": {
    "task_id": string
  }
}
```

**Response (Sync):**
```json
{
  "success": true,
  "message": "App uninstallation completed successfully",
  "data": object
}
```

### POST /apps/open

Open an application.

**Request Body:**
```json
{
  "id": string
}
```

**Response:**
```json
{
  "success": true,
  "message": "Application opened successfully",
  "data": null
}
```

### POST /apps/resume

Resume a suspended application.

**Request Body:**
```json
{
  "appName": string
}
```

**Response:**
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

Stop a running application.

**Request Body:**
```json
{
  "appName": string,
  "all": boolean
}
```

**Request Body Parameters:**
- `appName`: Application name (required)
- `all`: Whether to stop all instances, defaults to false (optional)

**Response:**
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

Delete a local source application.

**Request Body:**
```json
{
  "app_name": string,
  "app_version": string
}
```

**Response:**
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

## Payment APIs

### GET /sources/{source}/apps/{id}/payment-status

Check application payment status (recommended route with source parameter).

**Path Parameters:**
- `source`: Market source name
- `id`: Application ID

**Headers:**
- `X-Forwarded-Host`: Required for callback URL construction

**Response:**
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

Check application payment status (legacy route, searches all sources).

**Path Parameters:**
- `id`: Application ID

**Headers:**
- `X-Forwarded-Host`: Required for callback URL construction

**Response:**
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

Purchase an application.

**Path Parameters:**
- `source`: Market source name
- `id`: Application ID

**Headers:**
- `X-Forwarded-Host`: Required for user DID resolution

**Response:**
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

Submit signature for payment processing.

**Request Body:**
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

**Headers:**
- `X-Bfl-User`: Required
- `X-Forwarded-Host`: Required

**Response:**
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

Start payment polling after frontend payment completion.

**Request Body:**
```json
{
  "source_id": string,
  "app_id": string,
  "tx_hash": string,
  "product_id": string
}
```

**Headers:**
- `X-Forwarded-Host`: Required

**Response:**
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

Frontend signals payment readiness.

**Request Body:**
```json
{
  "source_id": string,
  "app_id": string,
  "product_id": string,
  "frontend_data": object
}
```

**Headers:**
- `X-Forwarded-Host`: Required

**Response:**
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

Fetch signature callback (from LarePass).

**Request Body:**
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

**Headers:**
- `X-Bfl-User`: Required

**Response:**
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

## Settings APIs

### GET /settings/market-source

Get market source configuration.

**Response:**
```json
{
  "success": true,
  "message": "Market source configuration retrieved successfully",
  "data": array
}
```

### POST /settings/market-source

Add market source configuration.

**Request Body:**
```json
{
  "id": string,
  "name": string,
  "type": "local" | "remote",
  "base_url": string,
  "description": string
}
```

**Response:**
```json
{
  "success": true,
  "message": "Market source added successfully",
  "data": array
}
```

### DELETE /settings/market-source/{id}

Delete market source configuration.

**Path Parameters:**
- `id`: Market source ID

**Response:**
```json
{
  "success": true,
  "message": "Market source deleted successfully",
  "data": array
}
```

### GET /settings/market-settings

Get market settings for the current user.

**Response:**
```json
{
  "success": true,
  "message": "Market settings retrieved successfully",
  "data": object
}
```

### PUT /settings/market-settings

Update market settings for the current user.

**Request Body:**
```json
{
  // MarketSettings object
}
```

**Response:**
```json
{
  "success": true,
  "message": "Market settings updated successfully",
  "data": object
}
```

---

## Development & Debug APIs

### GET /market/debug-memory

Get market debug memory information (development use only).

**Response:**
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

Get cache and Redis diagnostic information (development use only).

**Response:**
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

Force reload cache data from Redis (development use only).

**Response:**
```json
{
  "success": true,
  "message": "Cache data reloaded successfully from Redis",
  "data": null
}
```

### POST /cleanup

Cleanup invalid pending data (development use only).

**Response:**
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

Query logs by specific conditions (development use only).

**Query Parameters:**
- `start`: Offset (default: 0)
- `size`: Limit (default: 100, max: 1000)
- `type`: Filter by log type
- `app`: Filter by application name
- `start_time`: Filter by start time (Unix timestamp)
- `end_time`: Filter by end time (Unix timestamp)

**Response:**
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

## Runtime State APIs

### GET /runtime/state

Get runtime state with optional filters.

**Query Parameters:**
- `user_id`: Filter by user ID
- `source_id`: Filter by source ID
- `app_name`: Filter by application name
- `task_status`: Filter by task status
- `task_type`: Filter by task type

**Response:**
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

Get runtime dashboard (HTML page).

**Response:** HTML page with runtime state visualization

### GET /runtime/dashboard-app

Get runtime dashboard for app processing flow (HTML page).

**Response:** HTML page with app processing flow visualization

