# History Module

## Overview

The history module is responsible for storing, querying, and managing application history records. This module uses PostgreSQL database for data storage and provides automatic cleanup functionality for old records.

## Features

1. **Store History Records** - Store various application operations and system events
2. **Conditional Query** - Query history records by type, application, time range, and other conditions
3. **Automatic Cleanup** - Periodically clean up records older than 1 month
4. **Pagination Support** - Support paginated queries for large datasets
5. **Health Check** - Provide database connection health check functionality

## Data Structures

### HistoryRecord
```go
type HistoryRecord struct {
    ID       int64       `json:"id" db:"id"`           // Primary key
    Type     HistoryType `json:"type" db:"type"`       // Record type (enum)
    Message  string      `json:"message" db:"message"` // Description message
    Time     int64       `json:"time" db:"time"`       // Unix timestamp
    App      string      `json:"app" db:"app"`         // Application name
    Extended string      `json:"extended" db:"extended"` // JSON string for additional data
}
```

### HistoryType (Record Types)
Predefined record types include:
- `ACTION_INSTALL` - Application installation operation
- `ACTION_UNINSTALL` - Application uninstallation operation
- `SYSTEM_INSTALL_SUCCEED` - System installation success
- `SYSTEM_INSTALL_FAILED` - System installation failure
- `action` - Generic action type
- `system` - Generic system type

### QueryCondition
```go
type QueryCondition struct {
    Type      HistoryType `json:"type,omitempty"`       // Filter by type
    App       string      `json:"app,omitempty"`        // Filter by app
    StartTime int64       `json:"start_time,omitempty"` // Time range start
    EndTime   int64       `json:"end_time,omitempty"`   // Time range end
    Limit     int         `json:"limit,omitempty"`      // Max records to return
    Offset    int         `json:"offset,omitempty"`     // Pagination offset
}
```

## Environment Variables Configuration

```bash
# PostgreSQL database configuration
POSTGRES_HOST=localhost              # Database host (default: localhost)
POSTGRES_PORT=5432                  # Database port (default: 5432)
POSTGRES_DB=history                 # Database name (default: history)
POSTGRES_USER=postgres              # Database user (default: postgres)
POSTGRES_PASSWORD=password          # Database password (default: password)

# Cleanup configuration
HISTORY_CLEANUP_INTERVAL_HOURS=24   # Cleanup interval in hours (default: 24)
```

## Usage

### 1. Create History Module Instance

```go
package main

import (
    "market/internal/v2/history"
    "github.com/golang/glog"
)

func main() {
    // Create history module instance
    historyModule, err := history.NewHistoryModule()
    if err != nil {
        glog.Fatalf("Failed to create history module: %v", err)
    }
    defer historyModule.Close()
    
    // Your code here...
}
```

### 2. Store History Records

```go
// Store a simple action record
record := &history.HistoryRecord{
    Type:    history.TypeActionInstall,
    Message: "Successfully installed nginx application",
    App:     "nginx",
}

err := historyModule.StoreRecord(record)
if err != nil {
    glog.Errorf("Failed to store record: %v", err)
} else {
    glog.Infof("Stored record with ID: %d", record.ID)
}

// Store record with extended data
extendedData := map[string]interface{}{
    "version": "1.2.3",
    "config": map[string]string{
        "port": "8080",
        "protocol": "https",
    },
}
extendedJSON, _ := json.Marshal(extendedData)

recordWithExtended := &history.HistoryRecord{
    Type:     history.TypeSystemInstallSucceed,
    Message:  "System component installed successfully",
    App:      "myapp",
    Extended: string(extendedJSON),
}

err = historyModule.StoreRecord(recordWithExtended)
```

### 3. Query History Records

```go
// Query all records (with pagination)
records, err := historyModule.QueryRecords(&history.QueryCondition{
    Limit: 10,
})

// Query records by application
appRecords, err := historyModule.QueryRecords(&history.QueryCondition{
    App:   "nginx",
    Limit: 5,
})

// Query records by type
typeRecords, err := historyModule.QueryRecords(&history.QueryCondition{
    Type:  history.TypeActionInstall,
    Limit: 10,
})

// Query records by time range
now := time.Now()
oneHourAgo := now.Add(-1 * time.Hour)

timeRangeRecords, err := historyModule.QueryRecords(&history.QueryCondition{
    StartTime: oneHourAgo.Unix(),
    EndTime:   now.Unix(),
    Limit:     20,
})

// Query with pagination
page1, err := historyModule.QueryRecords(&history.QueryCondition{
    Limit:  10,
    Offset: 0,
})

page2, err := historyModule.QueryRecords(&history.QueryCondition{
    Limit:  10,
    Offset: 10,
})
```

### 4. Get Record Count

```go
// Get total record count
totalCount, err := historyModule.GetRecordCount(&history.QueryCondition{})

// Get count by application
appCount, err := historyModule.GetRecordCount(&history.QueryCondition{
    App: "nginx",
})

// Get count by type
typeCount, err := historyModule.GetRecordCount(&history.QueryCondition{
    Type: history.TypeActionInstall,
})
```

### 5. Health Check

```go
// Check if the module is healthy
if err := historyModule.HealthCheck(); err != nil {
    glog.Errorf("History module health check failed: %v", err)
} else {
    glog.Infof("History module is healthy")
}
```

## Database Table Structure

The module automatically creates the following database table:

```sql
CREATE TABLE IF NOT EXISTS history_records (
    id BIGSERIAL PRIMARY KEY,
    type VARCHAR(100) NOT NULL,
    message TEXT NOT NULL,
    time BIGINT NOT NULL,
    app VARCHAR(255) NOT NULL,
    extended TEXT DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for better query performance
CREATE INDEX IF NOT EXISTS idx_history_type ON history_records(type);
CREATE INDEX IF NOT EXISTS idx_history_app ON history_records(app);
CREATE INDEX IF NOT EXISTS idx_history_time ON history_records(time);
CREATE INDEX IF NOT EXISTS idx_history_created_at ON history_records(created_at);
```

## Automatic Cleanup Feature

The module automatically starts a background cleanup routine that periodically deletes old records older than 1 month:

- Runs cleanup every 24 hours by default
- Deletes records where the `time` field is less than current time minus 1 month
- Cleanup interval can be configured via the `HISTORY_CLEANUP_INTERVAL_HOURS` environment variable

## Testing

Run unit tests:

```bash
# Run all tests
go test ./internal/v2/history/

# Run specific test
go test ./internal/v2/history/ -run TestStoreRecord

# Run benchmarks
go test ./internal/v2/history/ -bench=.
```

Note: Tests require PostgreSQL database to be available.

## Performance Considerations

1. **Database Indexes** - Indexes are created for commonly queried fields
2. **Paginated Queries** - Use LIMIT and OFFSET for pagination to avoid loading large amounts of data at once
3. **Automatic Cleanup** - Periodically clean old data to maintain database performance
4. **Connection Pool** - Use sqlx for database connection management

## Extending Record Types

You can easily add new record types:

```go
const (
    TypeActionUpgrade   HistoryType = "ACTION_UPGRADE"
    TypeActionRestart   HistoryType = "ACTION_RESTART"
    TypeSystemBackup    HistoryType = "SYSTEM_BACKUP"
    TypeSystemRestore   HistoryType = "SYSTEM_RESTORE"
)
```

## Error Handling

The module uses glog for logging, and all errors are logged and returned to the caller. It's recommended to handle errors appropriately when using the module:

```go
if err := historyModule.StoreRecord(record); err != nil {
    glog.Errorf("Failed to store history record: %v", err)
    // Handle error appropriately
    return err
}
```

## Notes

1. Ensure PostgreSQL database is properly configured and accessible
2. Regularly backup history record data
3. Monitor database disk usage
4. Adjust cleanup interval and retention time as needed
5. The Extended field should use valid JSON format 