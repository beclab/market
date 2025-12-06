# Other Features

## Diagnostic Functions

The Market system provides diagnostic and maintenance interfaces for operational purposes. These interfaces allow administrators to inspect system health, manage cache, and perform cleanup operations.

**Location**: `internal/v2/appinfo/diagnostic.go`, `pkg/v2/api/app.go`

**Available Endpoints**:
- `GET /api/v2/diagnostic` - Retrieve cache and Redis diagnostic information
- `POST /api/v2/reload` - Force reload cache data from Redis
- `POST /api/v2/cleanup` - Clean up invalid pending data

## Application Lifecycle Management

The Market system provides interfaces for managing application lifecycle operations, including resuming, stopping, and opening applications.

**Location**: `pkg/v2/api/system.go`

**Available Endpoints**:
- `POST /api/v2/apps/resume` - Resume applications (supports both app and middleware types)
- `POST /api/v2/apps/stop` - Stop applications (supports both app and middleware types)
- `POST /api/v2/apps/open` - Open applications (sends intent through system server)

## Version History Query

The Market system provides an interface for querying application version history records.

**Location**: `pkg/v2/api/system.go`

**Available Endpoints**:
- `POST /api/v2/apps/version-history` - Query version history records for applications

## Local Application Management

The Market system provides interfaces for managing locally uploaded applications, including uploading installation packages and deleting uploaded sources.

**Location**: `pkg/v2/api/app.go`

**Available Endpoints**:
- `POST /api/v2/apps/upload` - Upload application installation package
- `DELETE /api/v2/local-apps/delete` - Delete uploaded source application (requires the application to be uninstalled first)

## Application Cloning

The Market system supports cloning installed applications with custom configurations. Cloned applications use a naming convention based on the original app name and request hash, allowing multiple instances of the same app with different configurations.

**Location**: `pkg/v2/api/task.go`, `internal/v2/task/app_clone.go`

**Available Endpoints**:
- `POST /api/v2/apps/{id}/clone` - Clone an installed application

**Key Features**:
- Cloned app naming: Uses `rawAppName + requestHash` as the new application name (where `requestHash` is the first 6 characters of the SHA256 hash calculated from the entire clone request, including source, app_name, title, envs, and entrances)
- Custom title: Supports a custom display title for the cloned application
- Custom entrances: Supports custom entrance configurations for the cloned application
- Prerequisite: The original application must be installed before cloning

## Settings Management

The Market system provides interfaces for managing user market settings, allowing users to configure their market preferences.

**Location**: `pkg/v2/api/system.go`, `internal/v2/settings/`

**Available Endpoints**:
- `GET /api/v2/settings/market-settings` - Retrieve user market settings
- `PUT /api/v2/settings/market-settings` - Update user market settings

## Multiple Market Sources Support

The Market system supports multiple market sources, allowing administrators to configure and manage different application sources (both remote and local). The system supports enabling or disabling sources as needed.

**Location**: `pkg/v2/api/system.go`, `internal/v2/settings/manager.go`, `internal/v2/settings/types.go`

**Available Endpoints**:
- `GET /api/v2/settings/market-source` - Retrieve market source configuration
- `POST /api/v2/settings/market-source` - Add a new market source
- `DELETE /api/v2/settings/market-source/{id}` - Delete a market source by ID

**Key Features**:
- **Source Types**: Supports two types of market sources:
  - `remote`: Remote market sources that are synced periodically by the Syncer
  - `local`: Local market sources that are not synced (typically for locally uploaded applications)
- **Enable/Disable Status**: Each market source has an `is_active` flag that controls whether it is used. Only active sources are included when the system retrieves available market sources. Inactive sources are ignored during sync operations and application queries.
- **Source Selection**: The Syncer only processes remote-type sources during sync cycles. Local sources are skipped during synchronization but can still be used for application installation and queries.

**Memory and API Support**:
- **In-Memory Storage**: Market source configurations are stored in `SettingsManager` as a `MarketSourcesConfig` structure containing an array of `MarketSource` objects. Each user's data in the cache (`UserData`) uses a `Sources map[string]*SourceData` structure, where the key is the source ID, allowing multiple market sources to coexist for each user.
- **API Response Structure**: API responses organize data by market source using a map structure. For example, in market data endpoints, the response contains `user_data.sources[sourceID]`, where each source ID maps to its corresponding application data (`AppInfoLatest`, `AppStateLatest`, etc.). This allows clients to access data from different market sources independently.

## System Status Check

The system status endpoint aggregates system information from multiple sources and serves as a prerequisite check before installing applications. It collects user information, resource availability, cluster resources, and system version information to ensure the system is ready for application installation.

**Location**: `pkg/v2/api/system.go`

**Available Endpoints**:
- `GET /api/v2/settings/system-status` - Get aggregated system status (user info, resources, version, etc.)

**Usage**: This endpoint should be called before initiating application installation to verify system readiness and resource availability.

## Synchronous/Asynchronous Task Execution

All task operations (install, uninstall, upgrade, clone, cancel) support a `sync` parameter that controls whether the operation waits for completion or returns immediately.

**Location**: `pkg/v2/api/task.go`

**Execution Modes**:
- **Synchronous mode** (`sync: true`): The API waits for the task to complete and returns the final result. The response includes the task execution result and any errors that occurred during execution.
- **Asynchronous mode** (`sync: false` or omitted): The API returns immediately with a task ID. The caller can use the task ID to query the task status and result later.

**Supported Operations**:
- `POST /api/v2/apps/{id}/install` - Install application
- `POST /api/v2/apps/{id}/uninstall` - Uninstall application
- `POST /api/v2/apps/{id}/upgrade` - Upgrade application
- `POST /api/v2/apps/{id}/clone` - Clone application
- `POST /api/v2/apps/{id}/cancel` - Cancel installation

**Implementation**:
All user-triggered tasks are executed asynchronously using the same logic, regardless of the `sync` parameter. The difference lies only in how the API handler responds:
- **Synchronous mode**: After calling `AddTask` to start the asynchronous task, the API handler creates a channel and blocks on it (`<-done`) to wait for the task completion. A callback function is registered that closes the channel and stores the result when the asynchronous task completes. The handler then returns the task result.
- **Asynchronous mode**: After calling `AddTask` to start the asynchronous task, the API handler immediately returns the task ID without waiting. A simple callback function is registered for logging purposes only.
