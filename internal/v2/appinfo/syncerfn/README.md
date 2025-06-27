# Syncer Steps System

This package provides a step-based abstraction synchronizer system for synchronizing information from remote API interfaces.

## Core Concepts

### SyncStep Interface
All synchronization steps must implement the `SyncStep` interface:
```go
type SyncStep interface {
    GetStepName() string                                    // Get step name
    Execute(ctx context.Context, data *SyncContext) error   // Execute step logic
    CanSkip(ctx context.Context, data *SyncContext) bool    // Determine if step can be skipped
}
```

### SyncContext
`SyncContext` holds shared data throughout the entire synchronization process:
- `Client`: HTTP client (resty)
- `Cache`: Cache data
- `RemoteHash`: Remote data hash value
- `LocalHash`: Local data hash value  
- `LatestData`: Latest fetched data
- `DetailedApps`: Detailed application information
- `AppIDs`: Application ID list
- `Errors`: Error list

## Built-in Steps

### 1. HashComparisonStep
- **Function**: Fetch data hash from remote API and compare with in-memory data
- **File**: `hash_comparison_step.go`
- **Usage**: `syncerfn.NewHashComparisonStep(remoteHashURL)`

### 2. DataFetchStep
- **Function**: Fetch latest data from remote
- **File**: `data_fetch_step.go`
- **Usage**: `syncerfn.NewDataFetchStep(remoteDataURL)`
- **Skip Condition**: Skip when remote hash equals local hash

### 3. DetailFetchStep
- **Function**: Iterate through latest data and fetch detailed information by appid from remote
- **File**: `detail_fetch_step.go`  
- **Usage**: `syncerfn.NewDetailFetchStep(detailURLTemplate)`
- **Features**: Supports concurrent requests, default limit of 10 concurrent requests

## Usage Examples

### Basic Usage
```go
import "market/internal/v2/appinfo/syncerfn"

// Create sync context
cache := appinfo.NewCacheData()
syncContext := syncerfn.NewSyncContext(cache)

// Create steps
hashStep := syncerfn.NewHashComparisonStep("https://api.example.com/hash")
dataStep := syncerfn.NewDataFetchStep("https://api.example.com/data")  
detailStep := syncerfn.NewDetailFetchStep("https://api.example.com/apps/%s")

// Execute steps
ctx := context.Background()
if err := hashStep.Execute(ctx, syncContext); err != nil {
    log.Printf("Hash step failed: %v", err)
}
```

### Custom Steps
```go
type CustomValidationStep struct {
    rules []ValidationRule
}

func (c *CustomValidationStep) GetStepName() string {
    return "Custom Validation Step"
}

func (c *CustomValidationStep) Execute(ctx context.Context, data *syncerfn.SyncContext) error {
    // Custom validation logic
    for _, rule := range c.rules {
        if err := rule.Validate(data.LatestData); err != nil {
            return fmt.Errorf("validation failed: %w", err)
        }
    }
    return nil
}

func (c *CustomValidationStep) CanSkip(ctx context.Context, data *syncerfn.SyncContext) bool {
    return len(data.LatestData) == 0 // Skip validation when no data
}
```

## Error Handling

Steps can add non-fatal errors through `SyncContext.AddError()`:
```go
if err := someOperation(); err != nil {
    data.AddError(fmt.Errorf("operation failed: %w", err))
    // Continue execution, don't return error
}
```

Check for errors:
```go
if syncContext.HasErrors() {
    errors := syncContext.GetErrors()
    for _, err := range errors {
        log.Printf("Error: %v", err)
    }
}
```

## Configuration Options

### DetailFetchStep Concurrency Control
```go
// Default 10 concurrent requests
step := syncerfn.NewDetailFetchStep("https://api.example.com/apps/%s")

// Custom concurrency limit
step := syncerfn.NewDetailFetchStepWithConcurrency("https://api.example.com/apps/%s", 20)
```

## Extensibility

The system is designed with extensibility in mind:

1. **Adding Steps**: Simply implement the `SyncStep` interface
2. **Step Order**: Can adjust step execution order arbitrarily
3. **Conditional Skipping**: Implement intelligent skipping through `CanSkip()` method
4. **Error Handling**: Supports both fatal errors and non-fatal error handling
5. **Concurrency Control**: Can implement concurrent processing within steps

## Best Practices

1. **Step Atomicity**: Each step should be independent and repeatable
2. **Error Handling**: Distinguish between fatal errors (return error) and warnings (AddError)
3. **Skip Conditions**: Set reasonable skip conditions to improve efficiency
4. **Logging**: Add appropriate logging in steps
5. **Timeout Handling**: Use context for timeout control 