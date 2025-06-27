package history

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDB sets up test database environment variables
func setupTestDB() {
	// Use test database configuration
	os.Setenv("POSTGRES_HOST", "localhost")
	os.Setenv("POSTGRES_PORT", "5432")
	os.Setenv("POSTGRES_DB", "history_test")
	os.Setenv("POSTGRES_USER", "postgres")
	os.Setenv("POSTGRES_PASSWORD", "password")
	os.Setenv("HISTORY_CLEANUP_INTERVAL_HOURS", "1") // 1 hour for testing
}

// cleanupTestDB cleans up test data
func cleanupTestDB(hm *HistoryModule) {
	if hm != nil && hm.db != nil {
		// Clean up test data
		hm.db.Exec("DELETE FROM history_records WHERE app LIKE 'test%'")
	}
}

func TestNewHistoryModule(t *testing.T) {
	setupTestDB()

	// Test creating new history module
	hm, err := NewHistoryModule()
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return
	}
	defer hm.Close()
	defer cleanupTestDB(hm)

	// Verify module is created properly
	assert.NotNil(t, hm)
	assert.NotNil(t, hm.db)

	// Test health check
	err = hm.HealthCheck()
	assert.NoError(t, err)
}

func TestStoreRecord(t *testing.T) {
	setupTestDB()

	hm, err := NewHistoryModule()
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return
	}
	defer hm.Close()
	defer cleanupTestDB(hm)

	// Test storing a simple record
	record := &HistoryRecord{
		Type:    TypeActionInstall,
		Message: "Test installation",
		App:     "testapp",
		Time:    time.Now().Unix(),
	}

	err = hm.StoreRecord(record)
	require.NoError(t, err)
	assert.Greater(t, record.ID, int64(0))

	// Test storing record with extended data
	extendedData := map[string]interface{}{
		"version": "1.0.0",
		"config":  map[string]string{"port": "8080"},
	}
	extendedJSON, _ := json.Marshal(extendedData)

	recordWithExtended := &HistoryRecord{
		Type:     TypeSystemInstallSucceed,
		Message:  "System component installed",
		App:      "testapp-extended",
		Extended: string(extendedJSON),
	}

	err = hm.StoreRecord(recordWithExtended)
	require.NoError(t, err)
	assert.Greater(t, recordWithExtended.ID, int64(0))

	// Test storing record with nil
	err = hm.StoreRecord(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be nil")
}

func TestQueryRecords(t *testing.T) {
	setupTestDB()

	hm, err := NewHistoryModule()
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return
	}
	defer hm.Close()
	defer cleanupTestDB(hm)

	// Create test records
	testRecords := []*HistoryRecord{
		{
			Type:    TypeActionInstall,
			Message: "Install nginx",
			App:     "testnginx",
			Time:    time.Now().Add(-2 * time.Hour).Unix(),
		},
		{
			Type:    TypeActionUninstall,
			Message: "Uninstall redis",
			App:     "testredis",
			Time:    time.Now().Add(-1 * time.Hour).Unix(),
		},
		{
			Type:    TypeSystemInstallSucceed,
			Message: "System component ready",
			App:     "testsystem",
			Time:    time.Now().Unix(),
		},
	}

	// Store test records
	for _, record := range testRecords {
		err := hm.StoreRecord(record)
		require.NoError(t, err)
	}

	// Test query all records
	t.Run("QueryAll", func(t *testing.T) {
		records, err := hm.QueryRecords(&QueryCondition{
			Limit: 10,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(records), 3)
	})

	// Test query by app
	t.Run("QueryByApp", func(t *testing.T) {
		records, err := hm.QueryRecords(&QueryCondition{
			App:   "testnginx",
			Limit: 5,
		})
		require.NoError(t, err)
		assert.Len(t, records, 1)
		assert.Equal(t, "testnginx", records[0].App)
		assert.Equal(t, TypeActionInstall, records[0].Type)
	})

	// Test query by type
	t.Run("QueryByType", func(t *testing.T) {
		records, err := hm.QueryRecords(&QueryCondition{
			Type:  TypeActionInstall,
			Limit: 5,
		})
		require.NoError(t, err)

		// Find our test record
		found := false
		for _, record := range records {
			if record.App == "testnginx" {
				found = true
				assert.Equal(t, TypeActionInstall, record.Type)
				break
			}
		}
		assert.True(t, found)
	})

	// Test query by time range
	t.Run("QueryByTimeRange", func(t *testing.T) {
		now := time.Now()
		oneHourAgo := now.Add(-30 * time.Minute)

		records, err := hm.QueryRecords(&QueryCondition{
			StartTime: oneHourAgo.Unix(),
			EndTime:   now.Unix(),
			Limit:     10,
		})
		require.NoError(t, err)

		// Should find the system record which was created recently
		found := false
		for _, record := range records {
			if record.App == "testsystem" {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	// Test query with pagination
	t.Run("QueryWithPagination", func(t *testing.T) {
		// First page
		page1, err := hm.QueryRecords(&QueryCondition{
			Limit:  2,
			Offset: 0,
		})
		require.NoError(t, err)
		assert.LessOrEqual(t, len(page1), 2)

		// Second page
		page2, err := hm.QueryRecords(&QueryCondition{
			Limit:  2,
			Offset: 2,
		})
		require.NoError(t, err)

		// Pages should be different (if we have enough records)
		if len(page1) > 0 && len(page2) > 0 {
			assert.NotEqual(t, page1[0].ID, page2[0].ID)
		}
	})
}

func TestGetRecordCount(t *testing.T) {
	setupTestDB()

	hm, err := NewHistoryModule()
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return
	}
	defer hm.Close()
	defer cleanupTestDB(hm)

	// Get initial count
	initialCount, err := hm.GetRecordCount(&QueryCondition{})
	require.NoError(t, err)

	// Store test records
	testRecords := []*HistoryRecord{
		{
			Type:    TypeActionInstall,
			Message: "Install app1",
			App:     "testapp1",
		},
		{
			Type:    TypeActionInstall,
			Message: "Install app2",
			App:     "testapp2",
		},
		{
			Type:    TypeActionUninstall,
			Message: "Uninstall app1",
			App:     "testapp1",
		},
	}

	for _, record := range testRecords {
		err := hm.StoreRecord(record)
		require.NoError(t, err)
	}

	// Test total count
	t.Run("TotalCount", func(t *testing.T) {
		count, err := hm.GetRecordCount(&QueryCondition{})
		require.NoError(t, err)
		assert.Equal(t, initialCount+3, count)
	})

	// Test count by app
	t.Run("CountByApp", func(t *testing.T) {
		count, err := hm.GetRecordCount(&QueryCondition{
			App: "testapp1",
		})
		require.NoError(t, err)
		assert.Equal(t, int64(2), count) // install + uninstall
	})

	// Test count by type
	t.Run("CountByType", func(t *testing.T) {
		count, err := hm.GetRecordCount(&QueryCondition{
			Type: TypeActionInstall,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(2)) // at least our 2 test records
	})
}

func TestHistoryTypes(t *testing.T) {
	// Test that all defined types are valid strings
	types := []HistoryType{
		TypeActionInstall,
		TypeActionUninstall,
		TypeSystemInstallSucceed,
		TypeSystemInstallFailed,
		TypeAction,
		TypeSystem,
	}

	for _, historyType := range types {
		assert.NotEmpty(t, string(historyType))
	}

	// Test type constants
	assert.Equal(t, "ACTION_INSTALL", string(TypeActionInstall))
	assert.Equal(t, "ACTION_UNINSTALL", string(TypeActionUninstall))
	assert.Equal(t, "SYSTEM_INSTALL_SUCCEED", string(TypeSystemInstallSucceed))
	assert.Equal(t, "SYSTEM_INSTALL_FAILED", string(TypeSystemInstallFailed))
	assert.Equal(t, "action", string(TypeAction))
	assert.Equal(t, "system", string(TypeSystem))
}

func TestRecordValidation(t *testing.T) {
	setupTestDB()

	hm, err := NewHistoryModule()
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return
	}
	defer hm.Close()
	defer cleanupTestDB(hm)

	// Test automatic timestamp setting
	t.Run("AutoTimestamp", func(t *testing.T) {
		record := &HistoryRecord{
			Type:    TypeActionInstall,
			Message: "Test auto timestamp",
			App:     "testapp-timestamp",
			// Time not set - should be set automatically
		}

		beforeStore := time.Now().Unix()
		err := hm.StoreRecord(record)
		afterStore := time.Now().Unix()

		require.NoError(t, err)
		assert.GreaterOrEqual(t, record.Time, beforeStore)
		assert.LessOrEqual(t, record.Time, afterStore)
	})

	// Test invalid JSON in extended field
	t.Run("InvalidJSON", func(t *testing.T) {
		record := &HistoryRecord{
			Type:     TypeSystemInstallSucceed,
			Message:  "Test invalid JSON",
			App:      "testapp-json",
			Extended: `{"invalid": json}`, // Invalid JSON
		}

		// Should still store the record (JSON validation is just a warning)
		err := hm.StoreRecord(record)
		assert.NoError(t, err)
	})
}

func TestCleanupFunctionality(t *testing.T) {
	setupTestDB()

	hm, err := NewHistoryModule()
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return
	}
	defer hm.Close()
	defer cleanupTestDB(hm)

	// Create old records (simulate 2 months old)
	twoMonthsAgo := time.Now().AddDate(0, -2, 0).Unix()
	oldRecord := &HistoryRecord{
		Type:    TypeActionInstall,
		Message: "Old record to be cleaned up",
		App:     "testapp-old",
		Time:    twoMonthsAgo,
	}

	// Create recent record
	recentRecord := &HistoryRecord{
		Type:    TypeActionInstall,
		Message: "Recent record to be kept",
		App:     "testapp-recent",
		Time:    time.Now().Unix(),
	}

	// Store both records
	err = hm.StoreRecord(oldRecord)
	require.NoError(t, err)

	err = hm.StoreRecord(recentRecord)
	require.NoError(t, err)

	// Run cleanup manually
	hm.cleanupOldRecords()

	// Verify old record is gone but recent record remains
	oldRecords, err := hm.QueryRecords(&QueryCondition{
		App: "testapp-old",
	})
	require.NoError(t, err)
	assert.Len(t, oldRecords, 0, "Old record should be cleaned up")

	recentRecords, err := hm.QueryRecords(&QueryCondition{
		App: "testapp-recent",
	})
	require.NoError(t, err)
	assert.Len(t, recentRecords, 1, "Recent record should be kept")
}

// BenchmarkStoreRecord benchmarks the store operation
func BenchmarkStoreRecord(b *testing.B) {
	setupTestDB()

	hm, err := NewHistoryModule()
	if err != nil {
		b.Skipf("Skipping benchmark - PostgreSQL not available: %v", err)
		return
	}
	defer hm.Close()
	defer cleanupTestDB(hm)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		record := &HistoryRecord{
			Type:    TypeActionInstall,
			Message: fmt.Sprintf("Benchmark record %d", i),
			App:     "benchmark-app",
		}

		err := hm.StoreRecord(record)
		if err != nil {
			b.Fatalf("Failed to store record: %v", err)
		}
	}
}

// BenchmarkQueryRecords benchmarks the query operation
func BenchmarkQueryRecords(b *testing.B) {
	setupTestDB()

	hm, err := NewHistoryModule()
	if err != nil {
		b.Skipf("Skipping benchmark - PostgreSQL not available: %v", err)
		return
	}
	defer hm.Close()
	defer cleanupTestDB(hm)

	// Create some test data
	for i := 0; i < 100; i++ {
		record := &HistoryRecord{
			Type:    TypeActionInstall,
			Message: fmt.Sprintf("Test record %d", i),
			App:     "benchmark-app",
		}
		hm.StoreRecord(record)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := hm.QueryRecords(&QueryCondition{
			App:   "benchmark-app",
			Limit: 10,
		})
		if err != nil {
			b.Fatalf("Failed to query records: %v", err)
		}
	}
}
