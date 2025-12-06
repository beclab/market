# Market - Application Store System

Market is an application store backend service developed in Go, providing core functionalities such as application management, task scheduling, and payment processing.

---

## System Architecture

### Market System Overall Architecture

The Market system consists of two core projects that work together to provide a complete application store service:

- **Market Project**: Core application store service, responsible for application information management, task processing, payment processing, and API services
- **Dynamic Chart Repository Project**: Helm Chart dynamic repository, responsible for Chart rendering, image analysis, and state management

The two projects interact through API interfaces, including:
- **Application Rendering**: Market synchronizes application data to Chart Repository for rendering processing
- **Data Synchronization**: Market retrieves state changes and application information from Chart Repository
- **Configuration Management**: Market shares market source configuration with Chart Repository
- **Status Monitoring**: Market retrieves runtime status information from Chart Repository

📖 [Market System Architecture](docs/architecture-market-system.md) | [中文版](docs/architecture-market-system.zh-CN.md)

### Market Project Internal Architecture

The Market project adopts a layered architecture design, mainly including the following layers:

- **API Service Layer**: Provides RESTful API services, supporting both v1 and v2 interface versions
- **Core Business Modules**: Including core functional modules such as application management, task management, payment management, and runtime status
- **Data Storage Layer**: Uses Redis for persistent storage, with in-memory cache providing high-performance data access
- **Data Processing Layer**: Includes data synchronizer (Syncer), data processor (Hydrator), and status monitor (DataWatcher)

The system achieves high cohesion and low coupling through modular design, with each module working together to provide complete application store service capabilities.

📖 [Market Project Internal Architecture](docs/architecture-market.md) | [中文版](docs/architecture-market.zh-CN.md)

---

## Feature Modules

### Core Features

#### 📦 Application Management

Provides complete application lifecycle management functionality, including application installation, uninstallation, upgrade, cloning, and other operations.

- Application installation, uninstallation, upgrade, cloning
- Application information query and synchronization
- Application configuration management
- Application status monitoring
- Application version history query
- Application lifecycle control (resume, stop, open)

📖 [Market Project Internal Architecture](docs/architecture-market.md) | [中文版](docs/architecture-market.zh-CN.md)  
📖 [API Reference Documentation](docs/api-reference.md) | [中文版](docs/api-reference.zh-CN.md)  
📖 [Other Features](docs/others.md) | [中文版](docs/others.zh-CN.md)

#### 📋 Application Information

Provides application information query and synchronization functionality, supporting batch queries and on-demand retrieval.

- Application information query (supports batch queries)
- Application information synchronization

📖 [Market Project Internal Architecture](docs/architecture-market.md) | [中文版](docs/architecture-market.zh-CN.md)  
📖 [API Reference Documentation](docs/api-reference.md) | [中文版](docs/api-reference.zh-CN.md)

#### 💳 Payment Management

Provides complete application payment process management, including payment state machine, payment polling, and frontend payment event handling.

- Payment state machine management
- Payment polling mechanism
- Frontend payment event handling
- Payment receipt management
- Developer verification and authorization

📖 [Payment Module Documentation](docs/payment-module.md) | [中文版](docs/payment-module.zh-CN.md)  
📖 [API Reference Documentation](docs/api-reference.md) | [中文版](docs/api-reference.zh-CN.md)

---

### Core Feature Architecture & Implementation

#### 🔄 Data Synchronization

Provides efficient frontend data synchronization mechanism, supporting incremental updates and on-demand updates.

- Hash-based data change detection
- Incremental data synchronization
- On-demand data retrieval
- Data cache management

📖 [Incremental Update System Documentation](docs/incremental-update-system.md) | [中文版](docs/incremental-update-system.zh-CN.md)  
📖 [API Reference Documentation](docs/api-reference.md) | [中文版](docs/api-reference.zh-CN.md)

#### ✅ Status Correction

Ensures consistency between cached application status and actual runtime status, automatically detecting and correcting status differences.

- Status consistency checking
- Automatic status correction
- Status difference detection
- Correction event recording

📖 [Status Correction Module Documentation](docs/status-correction-module.md) | [中文版](docs/status-correction-module.zh-CN.md)

#### ⚙️ Task Management

Provides asynchronous task scheduling and execution mechanism, supporting task status tracking, cancellation, and retry.

- Asynchronous task scheduling and execution
- Task status tracking
- Task cancellation and retry mechanism
- Task progress query

📖 [Market Project Internal Architecture](docs/architecture-market.md) | [中文版](docs/architecture-market.zh-CN.md)  
📖 [API Reference Documentation](docs/api-reference.md) | [中文版](docs/api-reference.zh-CN.md)

---

### Supporting Services

#### ⚙️ Settings Management

Provides system configuration management functionality, including market source configuration, Redis configuration, etc.

- Market source configuration management
- Redis configuration management
- System environment variable monitoring
- Configuration persistence

📖 [API Reference Documentation](docs/api-reference.md) | [中文版](docs/api-reference.zh-CN.md)  
📖 [Other Features](docs/others.md) | [中文版](docs/others.zh-CN.md)

---

### Developer Features

#### 📜 History

Provides operation history recording and system event tracking functionality, supporting history queries and filtering.

- Operation history recording
- System event tracking
- History query and filtering
- Automatic cleanup mechanism

📖 [History Module Documentation](docs/history-module.md) | [中文版](docs/history-module.zh-CN.md)  
📖 [API Reference Documentation](docs/api-reference.md) | [中文版](docs/api-reference.zh-CN.md)

#### 🔄 Runtime Status

Provides system runtime status collection and monitoring functionality, including application status, task status, and system component status.

- Application runtime status collection
- Task execution status monitoring
- System component status monitoring
- Real-time status query

📖 [Runtime Module Documentation](docs/runtime-module.md) | [中文版](docs/runtime-module.zh-CN.md)  
📖 [API Reference Documentation](docs/api-reference.md) | [中文版](docs/api-reference.zh-CN.md)

---

## API Documentation

Complete API interface reference documentation, including detailed descriptions of all endpoints, request parameters, and response formats.

📖 [API Reference Documentation](docs/api-reference.md) | [中文版](docs/api-reference.zh-CN.md)

---

## Documentation Index

### Architecture Documentation
- [Market System Architecture](docs/architecture-market-system.md) | [中文版](docs/architecture-market-system.zh-CN.md)
- [Market Project Internal Architecture](docs/architecture-market.md) | [中文版](docs/architecture-market.zh-CN.md)

### Feature Module Documentation
- [Payment Module](docs/payment-module.md) | [中文版](docs/payment-module.zh-CN.md)
- [History Module](docs/history-module.md) | [中文版](docs/history-module.zh-CN.md)
- [Runtime Module](docs/runtime-module.md) | [中文版](docs/runtime-module.zh-CN.md)
- [Status Correction Module](docs/status-correction-module.md) | [中文版](docs/status-correction-module.zh-CN.md)
- [Incremental Update System](docs/incremental-update-system.md) | [中文版](docs/incremental-update-system.zh-CN.md)

### API Documentation
- [API Reference Documentation](docs/api-reference.md) | [中文版](docs/api-reference.zh-CN.md)

### Other Documentation
- [Other Features](docs/others.md) | [中文版](docs/others.zh-CN.md)

