## History Module

> Language: **English** | [简体中文](./history-module.zh-CN.md)

### Overview

The history module is responsible for storing, querying, and managing application history records. It uses a PostgreSQL database for data storage and provides automatic cleanup for old records.

### Features

1. **Store history records** - Store various application operations and system events  
2. **Conditional query** - Query history records by type, application, time range, and other conditions  
3. **Automatic cleanup** - Periodically clean up records older than 1 month  
4. **Pagination support** - Support paginated queries for large datasets  
5. **Health check** - Provide database connection health check functionality  

### Data Structures

#### HistoryRecord

```go
type HistoryRecord struct {
    ID       int64       `json:"id" db:"id"`             // Primary key
    Type     HistoryType `json:"type" db:"type"`         // Record type (enum)
    Message  string      `json:"message" db:"message"`   // Description message
    Time     int64       `json:"time" db:"time"`         // Unix timestamp
    App      string      `json:"app" db:"app"`           // Application name
    Account  string      `json:"account" db:"account"`   // Account name
    Extended string      `json:"extended" db:"extended"` // JSON string for additional data
}
```

#### HistoryType (Record Types)

Predefined record types include:

- `ACTION_INSTALL` - Application installation operation  
- `ACTION_UNINSTALL` - Application uninstallation operation  
- `ACTION_CANCEL` - Application operation cancelled  
- `ACTION_UPGRADE` - Application upgrade operation  
- `SYSTEM_INSTALL_SUCCEED` - System installation succeeded  
- `SYSTEM_INSTALL_FAILED` - System installation failed  
- `action` - Generic action type  
- `system` - Generic system type  

#### QueryCondition

```go
type QueryCondition struct {
    Type      HistoryType `json:"type,omitempty"`        // Filter by type
    App       string      `json:"app,omitempty"`         // Filter by app
    Account   string      `json:"account,omitempty"`     // Filter by account
    StartTime int64       `json:"start_time,omitempty"`  // Time range start
    EndTime   int64       `json:"end_time,omitempty"`    // Time range end
    Limit     int         `json:"limit,omitempty"`       // Max records to return
    Offset    int         `json:"offset,omitempty"`      // Pagination offset
}
```

### Database Table Structure

The module automatically creates the following database table:

```sql
CREATE TABLE IF NOT EXISTS history_records (
    id BIGSERIAL PRIMARY KEY,
    type VARCHAR(100) NOT NULL,
    message TEXT NOT NULL,
    time BIGINT NOT NULL,
    app VARCHAR(255) NOT NULL,
    account VARCHAR(255) NOT NULL DEFAULT '',
    extended TEXT DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for better query performance
CREATE INDEX IF NOT EXISTS idx_history_type ON history_records(type);
CREATE INDEX IF NOT EXISTS idx_history_app ON history_records(app);
CREATE INDEX IF NOT EXISTS idx_history_account ON history_records(account);
CREATE INDEX IF NOT EXISTS idx_history_time ON history_records(time);
CREATE INDEX IF NOT EXISTS idx_history_created_at ON history_records(created_at);
```

### Automatic Cleanup Feature

The module automatically starts a background cleanup routine that periodically deletes records older than 1 month:

- Runs cleanup every 24 hours by default  
- Deletes records where the `time` field is less than the current time minus 1 month  
- The cleanup interval can be configured via the `HISTORY_CLEANUP_INTERVAL_HOURS` environment variable  

### Extending Record Types

You can easily add new record types (the following is an example; these constants may not exist in the current code base):

```go
const (
    TypeActionRestart   HistoryType = "ACTION_RESTART"
    TypeSystemBackup    HistoryType = "SYSTEM_BACKUP"
    TypeSystemRestore   HistoryType = "SYSTEM_RESTORE"
)
```

### Notes

1. The `Extended` field should contain valid JSON; if it is not valid JSON, the module logs a warning but still stores the original string.  
2. In public environments (as determined by `utils.IsPublicEnvironment()`), the history module is not initialized to avoid unnecessary database connections in certain deployment scenarios.  

