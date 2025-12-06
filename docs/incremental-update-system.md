# Frontend Data Synchronization System

[中文版 (Chinese)](./incremental-update-system.zh-CN.md)

## Overview

The Market system provides hash, data, and appinfo APIs that enable frontend applications to implement efficient incremental and point-in-time update strategies. By using hash comparison to detect data changes, combined with a two-tier data fetching approach (basic information first, detailed information on demand), frontends can achieve efficient data synchronization.

## Core Principle

The system uses a hash value to represent the current state of market data. When the hash changes, it indicates that the underlying data has been modified. The frontend uses a two-tier approach:
1. Fetch the complete application list with basic information for each app via the `data` endpoint
2. Fetch detailed information for specific applications on demand via the `appinfo` endpoint

This achieves both incremental updates (only when hash changes) and point-in-time updates (specific apps as needed) as a unified mechanism.

## API Endpoints

**Location**: `pkg/v2/api/app.go`

### 1. Hash Endpoint

- **Endpoint**: `GET /api/v2/market/hash`
- **Purpose**: Get the current hash value of market data for a user
- **Response**: 
  ```json
  {
    "hash": "abc123def456..."
  }
  ```
- **Usage**: Frontend should call this endpoint periodically to check if data has changed

### 2. Data Endpoint

- **Endpoint**: `GET /api/v2/market/data`
- **Purpose**: Get the complete application list with basic information for each application
- **Response**: Contains filtered user data with:
  - `AppInfoLatest`: Array of applications, each containing only basic information (`AppSimpleInfo`) such as:
    - `app_id`: Application ID
    - `app_name`: Application name
    - `app_icon`: Application icon
    - `app_description`: Brief description
    - `app_version`: Version number
    - `app_title`: Display title
    - `categories`: Categories
    - `support_arch`: Supported architectures
  - `Others`: Topics, recommendations, pages, etc.
  - `Hash`: Current hash value of the data
- **Important**: This endpoint does **NOT** return complete application information. It only provides the application list and basic metadata for each app.
- **Usage**: Fetch when hash changes or for initial load to get the application catalog

### 3. App Info Endpoint

- **Endpoint**: `POST /api/v2/apps`
- **Purpose**: Get complete detailed information for specific applications
- **Request Body**:
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
- **Response**: Returns complete `AppInfoLatestData` including:
  - Full `RawData`: Complete application metadata
  - `AppInfo`: Detailed information including app entry, image analysis, pricing, etc.
  - `Values`: Configuration values
  - Package information
  - All other detailed fields
- **Usage**: Fetch detailed information for specific applications on demand


## Important Notes

### State Data is Dynamic

The `GET /api/v2/market/state` endpoint returns only `AppStateLatest` data (application runtime states such as running, installing, stopped, etc.). This data is **dynamic and constantly changing**, and therefore:

- **DO NOT** use the hash-based update mechanism for state data
- State data should be fetched separately using different strategies
- Frontend should combine:
  - Static basic data (from `/market/data`)
  - Dynamic state data (from `/market/state`)
  - Detailed app information (from `/api/v2/apps` on demand)
  
  to build the complete application view

### Hash Update Scenarios

The hash value is recalculated and updated in the following scenarios:

1. **Application Processing**: When applications are moved from `AppInfoLatestPending` to `AppInfoLatest` after processing completion (handled by `DataWatcher`)
2. **Initial Load**: When user data hash is empty during initial data loading or initialization
3. **Status Correction**: After status corrections are applied by `StatusCorrectionChecker`, the hash is recalculated for all affected users
4. **State Data Update**: After state cache is updated, all users' hashes are recalculated (handled by `DataWatcher` State)
5. **Image Info Update**: After image information is updated, all users' hashes are recalculated (handled by `DataWatcher` Repo)

**Location**: 
- `internal/v2/appinfo/datawatcher_app.go` - Application processing and hash calculation
- `internal/v2/appinfo/status_correction_check.go` - Status correction hash updates
- `internal/v2/appinfo/datawatcher_state.go` - State update hash recalculation
- `internal/v2/appinfo/datawatcher_repo.go` - Image info update hash recalculation
