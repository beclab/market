package task

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// downloadRecordLookup is the optional fallback used when the task lookup
// has only partial information. It is wired by main.go (or a test) via
// SetDownloadRecordLookup. Keeping this as an injection avoids dragging the
// chart-repo HTTP client (which currently lives in package utils) into the
// task package and creating an import cycle.
type downloadRecordLookup func(userID, appName string) (version, source string, err error)

var downloadRecordProvider downloadRecordLookup

// SetDownloadRecordLookup registers the chart-repo download fallback used by
// LookupAppInfoLastInstalled. Pass nil to disable the fallback.
func SetDownloadRecordLookup(fn downloadRecordLookup) { downloadRecordProvider = fn }

// LookupAppInfoLastInstalled fetches the version and source recorded for the
// most recent successful install / upgrade / clone of appName for userID. It
// reads task_records via the shared sqlx wrapper and falls back to the
// chart-repo download lookup (when registered) if the task row is missing
// data or absent.
//
// Returns ("", "", nil) when nothing is known about the app; returns an
// error only for unexpected failures (database errors other than "no rows").
func LookupAppInfoLastInstalled(userID, appName string) (string, string, error) {
	if db := GlobalSqlxDB(); db != nil {
		const query = `
        SELECT metadata, type
        FROM task_records
        WHERE app_name = $1
            AND user_account = $2
            AND status = $3
            AND type IN ($4, $5, $6)
        ORDER BY completed_at DESC NULLS LAST, created_at DESC
        LIMIT 1
        `

		var metadataStr string
		var taskType int
		err := db.QueryRow(query, appName, userID,
			int(Completed), int(InstallApp), int(UpgradeApp), int(CloneApp),
		).Scan(&metadataStr, &taskType)
		switch {
		case err == nil:
			if version, source, ok := extractFromMetadata(metadataStr); ok {
				if version != "" && source != "" {
					return version, source, nil
				}
				// We have at least one of the two — try the download
				// fallback to fill the missing one. CloneApp records
				// reference the original app via "rawAppName".
				downloadAppName := appName
				if taskType == int(CloneApp) {
					if raw := rawAppNameFromMetadata(metadataStr); raw != "" {
						downloadAppName = raw
					}
				}
				if downloadRecordProvider != nil {
					if dlVersion, dlSource, dlErr := downloadRecordProvider(userID, downloadAppName); dlErr == nil {
						if version == "" {
							version = dlVersion
						}
						if source == "" {
							source = dlSource
						}
					}
				}
				if version != "" || source != "" {
					return version, source, nil
				}
			}
		case err == sql.ErrNoRows:
			// No matching task; fall through to download fallback below.
		default:
			return "", "", fmt.Errorf("query task_records: %w", err)
		}
	}

	if downloadRecordProvider != nil {
		return downloadRecordProvider(userID, appName)
	}
	return "", "", nil
}

// extractFromMetadata pulls "version" and "source" string fields from the
// JSON-encoded task metadata column. Returns ok=false when the JSON cannot
// be parsed; in that case the caller should fall back to the download
// record.
func extractFromMetadata(raw string) (version, source string, ok bool) {
	if raw == "" {
		return "", "", true
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", "", false
	}
	if v, isStr := m["version"].(string); isStr {
		version = v
	}
	if s, isStr := m["source"].(string); isStr {
		source = s
	}
	return version, source, true
}

// rawAppNameFromMetadata returns the "rawAppName" field from the metadata
// JSON when present, used for CloneApp lookups that point at the original
// app name in download records.
func rawAppNameFromMetadata(raw string) string {
	if raw == "" {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	if v, ok := m["rawAppName"].(string); ok {
		return v
	}
	return ""
}
