package history

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/golang/glog"
)

// Example demonstrates how to use the HistoryModule
func Example() {
	// Create a new history module instance
	historyModule, err := NewHistoryModule()
	if err != nil {
		glog.Errorf("Failed to create history module: %v", err)
		return
	}
	defer historyModule.Close()

	// Example 1: Store a simple action record
	fmt.Println("=== Example 1: Store Action Record ===")
	actionRecord := &HistoryRecord{
		Type:    TypeActionInstall,
		Message: "Successfully installed application nginx",
		App:     "nginx",
		Time:    time.Now().Unix(),
	}

	if err := historyModule.StoreRecord(actionRecord); err != nil {
		glog.Errorf("Failed to store action record: %v", err)
	} else {
		fmt.Printf("Stored action record with ID: %d\n", actionRecord.ID)
	}

	// Example 2: Store a system record with extended data
	fmt.Println("\n=== Example 2: Store System Record with Extended Data ===")
	extendedData := map[string]interface{}{
		"version":     "1.2.3",
		"install_dir": "/opt/myapp",
		"config": map[string]string{
			"port":     "8080",
			"protocol": "https",
		},
	}
	extendedJSON, _ := json.Marshal(extendedData)

	systemRecord := &HistoryRecord{
		Type:     TypeSystemInstallSucceed,
		Message:  "System installation completed successfully",
		App:      "myapp",
		Extended: string(extendedJSON),
	}

	if err := historyModule.StoreRecord(systemRecord); err != nil {
		glog.Errorf("Failed to store system record: %v", err)
	} else {
		fmt.Printf("Stored system record with ID: %d\n", systemRecord.ID)
	}

	// Example 3: Query all records
	fmt.Println("\n=== Example 3: Query All Records ===")
	allRecords, err := historyModule.QueryRecords(&QueryCondition{
		Limit: 10,
	})
	if err != nil {
		glog.Errorf("Failed to query all records: %v", err)
	} else {
		fmt.Printf("Found %d records:\n", len(allRecords))
		for _, record := range allRecords {
			fmt.Printf("- ID: %d, Type: %s, App: %s, Message: %s, Time: %s\n",
				record.ID, record.Type, record.App, record.Message,
				time.Unix(record.Time, 0).Format("2006-01-02 15:04:05"))
		}
	}

	// Example 4: Query records by app
	fmt.Println("\n=== Example 4: Query Records by App ===")
	appRecords, err := historyModule.QueryRecords(&QueryCondition{
		App:   "nginx",
		Limit: 5,
	})
	if err != nil {
		glog.Errorf("Failed to query records by app: %v", err)
	} else {
		fmt.Printf("Found %d records for app 'nginx':\n", len(appRecords))
		for _, record := range appRecords {
			fmt.Printf("- Type: %s, Message: %s\n", record.Type, record.Message)
		}
	}

	// Example 5: Query records by type
	fmt.Println("\n=== Example 5: Query Records by Type ===")
	typeRecords, err := historyModule.QueryRecords(&QueryCondition{
		Type:  TypeActionInstall,
		Limit: 5,
	})
	if err != nil {
		glog.Errorf("Failed to query records by type: %v", err)
	} else {
		fmt.Printf("Found %d install action records:\n", len(typeRecords))
		for _, record := range typeRecords {
			fmt.Printf("- App: %s, Message: %s\n", record.App, record.Message)
		}
	}

	// Example 6: Query records by time range
	fmt.Println("\n=== Example 6: Query Records by Time Range ===")
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	timeRangeRecords, err := historyModule.QueryRecords(&QueryCondition{
		StartTime: oneHourAgo.Unix(),
		EndTime:   now.Unix(),
		Limit:     10,
	})
	if err != nil {
		glog.Errorf("Failed to query records by time range: %v", err)
	} else {
		fmt.Printf("Found %d records in the last hour:\n", len(timeRangeRecords))
		for _, record := range timeRangeRecords {
			fmt.Printf("- Type: %s, App: %s, Time: %s\n",
				record.Type, record.App,
				time.Unix(record.Time, 0).Format("2006-01-02 15:04:05"))
		}
	}

	// Example 7: Get record count
	fmt.Println("\n=== Example 7: Get Record Count ===")
	totalCount, err := historyModule.GetRecordCount(&QueryCondition{})
	if err != nil {
		glog.Errorf("Failed to get total record count: %v", err)
	} else {
		fmt.Printf("Total records in database: %d\n", totalCount)
	}

	appCount, err := historyModule.GetRecordCount(&QueryCondition{
		App: "nginx",
	})
	if err != nil {
		glog.Errorf("Failed to get app record count: %v", err)
	} else {
		fmt.Printf("Total records for app 'nginx': %d\n", appCount)
	}

	// Example 8: Health check
	fmt.Println("\n=== Example 8: Health Check ===")
	if err := historyModule.HealthCheck(); err != nil {
		fmt.Printf("Health check failed: %v\n", err)
	} else {
		fmt.Println("History module is healthy")
	}

	// Example 9: Store multiple records with different types
	fmt.Println("\n=== Example 9: Store Multiple Records ===")
	records := []*HistoryRecord{
		{
			Type:    TypeActionUninstall,
			Message: "Uninstalled application redis",
			App:     "redis",
		},
		{
			Type:     TypeSystemInstallFailed,
			Message:  "Failed to install system component: database connection error",
			App:      "database",
			Extended: `{"error_code": 500, "error_details": "Connection timeout"}`,
		},
		{
			Type:     "ACTION_UPGRADE",
			Message:  "Upgraded application to version 2.0.0",
			App:      "myapp",
			Extended: `{"from_version": "1.2.3", "to_version": "2.0.0"}`,
		},
	}

	for i, record := range records {
		if err := historyModule.StoreRecord(record); err != nil {
			glog.Errorf("Failed to store record %d: %v", i+1, err)
		} else {
			fmt.Printf("Stored record %d with ID: %d\n", i+1, record.ID)
		}
	}

	fmt.Println("\n=== Example Complete ===")
}

// ExampleBatchOperations demonstrates batch operations and pagination
func ExampleBatchOperations() {
	historyModule, err := NewHistoryModule()
	if err != nil {
		glog.Errorf("Failed to create history module: %v", err)
		return
	}
	defer historyModule.Close()

	fmt.Println("=== Batch Operations Example ===")

	// Create 50 test records
	fmt.Println("Creating 50 test records...")
	for i := 1; i <= 50; i++ {
		record := &HistoryRecord{
			Type:    TypeActionInstall,
			Message: fmt.Sprintf("Test record %d", i),
			App:     fmt.Sprintf("testapp%d", i%5+1), // 5 different apps
			Time:    time.Now().Add(-time.Duration(i) * time.Minute).Unix(),
		}

		if err := historyModule.StoreRecord(record); err != nil {
			glog.Errorf("Failed to store test record %d: %v", i, err)
		}
	}

	// Demonstrate pagination
	fmt.Println("\nDemonstrating pagination:")
	pageSize := 10
	page := 0

	for {
		records, err := historyModule.QueryRecords(&QueryCondition{
			Limit:  pageSize,
			Offset: page * pageSize,
		})
		if err != nil {
			glog.Errorf("Failed to query page %d: %v", page+1, err)
			break
		}

		if len(records) == 0 {
			break
		}

		fmt.Printf("Page %d: %d records\n", page+1, len(records))
		for _, record := range records {
			fmt.Printf("  - %s: %s (%s)\n",
				record.App, record.Message,
				time.Unix(record.Time, 0).Format("15:04:05"))
		}

		page++
		if page >= 3 { // Only show first 3 pages for demo
			fmt.Println("  ... (showing only first 3 pages)")
			break
		}
	}

	// Count records by app
	fmt.Println("\nRecords count by app:")
	for i := 1; i <= 5; i++ {
		appName := fmt.Sprintf("testapp%d", i)
		count, err := historyModule.GetRecordCount(&QueryCondition{
			App: appName,
		})
		if err != nil {
			glog.Errorf("Failed to get count for %s: %v", appName, err)
		} else {
			fmt.Printf("  %s: %d records\n", appName, count)
		}
	}
}
