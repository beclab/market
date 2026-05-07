package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"market/internal/v2/db"
	"market/internal/v2/db/models"
	"market/internal/v2/types"
)

// AppInfoSimpleRow is the projection ListAppInfoLatestForUser yields.
// Each row carries enough data to assemble the FilteredAppInfoLatestData
// payload returned by /market/data: the manifest metadata block (for
// AppSimpleInfo's app-facing fields) and the catalog-level app_entry
// (for SupportArch / AppLabels which the manifest metadata does not
// always carry).
type AppInfoSimpleRow struct {
	SourceID     string                                    `gorm:"column:source_id"`
	AppID        string                                    `gorm:"column:app_id"`
	AppName      string                                    `gorm:"column:app_name"`
	ManifestType string                                    `gorm:"column:manifest_type"`
	AppVersion   string                                    `gorm:"column:app_version"`
	UpdatedAt    time.Time                                 `gorm:"column:updated_at"`
	Metadata     *models.JSONB[map[string]any]             `gorm:"column:metadata"`
	AppEntry     *models.JSONB[types.ApplicationInfoEntry] `gorm:"column:app_entry"`
}

// AppStateLatestRow is the projection ListUserAppStateLatest yields.
// Mirrors the cache-side AppStateLatestData wire shape: scalar fields
// from user_application_states, identity fields from user_applications,
// and the three entrance JSONB blobs decoded into the typed slices the
// API contract expects.
type AppStateLatestRow struct {
	SourceID         string    `gorm:"column:source_id"`
	AppID            string    `gorm:"column:app_id"`
	AppName          string    `gorm:"column:app_name"`
	AppRawName       string    `gorm:"column:app_raw_name"`
	Metadata         *models.JSONB[map[string]any] `gorm:"column:metadata"`
	InstalledVersion string    `gorm:"column:installed_version"`
	IsSysApp         bool      `gorm:"column:is_sys_app"`
	State            string    `gorm:"column:state"`
	Reason           string    `gorm:"column:reason"`
	Message          string    `gorm:"column:message"`
	Progress         string    `gorm:"column:progress"`
	OpType           string    `gorm:"column:op_type"`
	EventCreateTime  time.Time `gorm:"column:event_create_time"`
	UASUpdatedAt     time.Time `gorm:"column:uas_updated_at"`

	// Entrance JSONB columns. The user_application_states schema stores
	// each as an array; using JSONB[[]types.AppStateLatestDataEntrances]
	// lets GORM decode straight into the wire-shape slice the API
	// returns. NULL columns leave the inner slice nil, which the
	// AppStateLatestData JSON encoder serialises as either omitempty or
	// `null` per the field tag.
	Entrances       *models.JSONB[[]types.AppStateLatestDataEntrances] `gorm:"column:entrances"`
	SharedEntrances *models.JSONB[[]types.AppStateLatestDataEntrances] `gorm:"column:shared_entrances"`
	StatusEntrances *models.JSONB[[]types.AppStateLatestDataEntrances] `gorm:"column:status_entrances"`
}

// ListAppInfoLatestForUser returns one AppInfoSimpleRow per
// successfully-rendered (user, source, app) tuple matching sourceIDs,
// JOINed against the catalogue so the caller can derive the
// AppSimpleInfo payload from a single round-trip.
//
// render_status is filtered to 'success' to mirror the cache-side
// behaviour: failed / pending renders are tracked but should not
// appear in /market/data's app_info_latest list.
//
// Empty userID or empty sourceIDs return an empty slice (no error)
// because the natural call site is the API handler, where these
// inputs are derived from the request and an empty source list
// genuinely means "nothing to fetch".
func ListAppInfoLatestForUser(ctx context.Context, userID string, sourceIDs []string) ([]*AppInfoSimpleRow, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || len(sourceIDs) == 0 {
		return []*AppInfoSimpleRow{}, nil
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	var rows []*AppInfoSimpleRow
	err := gdb.WithContext(ctx).
		Table("user_applications AS ua").
		Joins("JOIN applications AS a ON a.source_id = ua.source_id AND a.app_id = ua.app_id").
		Select(`ua.source_id,
		        ua.app_id,
		        ua.app_name,
		        ua.manifest_type,
		        COALESCE(ua.metadata->>'version', '') AS app_version,
		        ua.updated_at,
		        ua.metadata,
		        a.app_entry`).
		Where("ua.user_id = ?", userID).
		Where("ua.render_status = ?", "success").
		Where("ua.source_id IN ?", sourceIDs).
		Order("ua.source_id, ua.app_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list app_info_latest for user %s: %w", userID, err)
	}
	return rows, nil
}

// ListUserAppStateLatest returns one AppStateLatestRow per
// (user, source, app) tuple that has both a user_applications row
// and a user_application_states row, restricted to the supplied
// sourceIDs.
//
// All render statuses are included here (unlike ListAppInfoLatestForUser):
// the cache-side getMarketData also returns AppStateLatest for apps
// regardless of their render outcome, because runtime state is the
// authoritative signal once an install attempt has reached app-service.
//
// Empty userID or empty sourceIDs return an empty slice (no error),
// matching ListAppInfoLatestForUser's behaviour.
func ListUserAppStateLatest(ctx context.Context, userID string, sourceIDs []string) ([]*AppStateLatestRow, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || len(sourceIDs) == 0 {
		return []*AppStateLatestRow{}, nil
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	var rows []*AppStateLatestRow
	err := gdb.WithContext(ctx).
		Table("user_applications AS ua").
		Joins("JOIN user_application_states AS uas ON uas.user_application_id = ua.id").
		Select(`ua.source_id,
		        ua.app_id,
		        ua.app_name,
		        ua.app_raw_name,
		        ua.metadata,
		        uas.installed_version,
		        uas.is_sys_app,
		        uas.state,
		        uas.reason,
		        uas.message,
		        uas.progress,
		        uas.op_type,
		        uas.event_create_time,
		        uas.updated_at AS uas_updated_at,
		        uas.entrances,
		        uas.shared_entrances,
		        uas.status_entrances`).
		Where("ua.user_id = ?", userID).
		Where("ua.source_id IN ?", sourceIDs).
		Order("ua.source_id, ua.app_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list app_state_latest for user %s: %w", userID, err)
	}
	return rows, nil
}
