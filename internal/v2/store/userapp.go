package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"market/internal/v2/db"
	"market/internal/v2/db/models"
	"market/internal/v2/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// renderErrorMaxLen mirrors the user_applications.render_error VARCHAR(200)
// width; MarkRenderFailed truncates anything longer so PG never rejects the
// row for the failure path.
const renderErrorMaxLen = 200

// ErrUserApplicationNotFound is returned by UpsertPurchaseInfo when no
// matching user_applications row exists. Purchase info is only ever written
// after a successful render has created the row, so this typically signals a
// caller bug rather than a data race.
var ErrUserApplicationNotFound = errors.New("user_applications row not found")

// RenderCandidate is a single (user, source, app) tuple the hydration loop
// should send to chart-repo. It is built from `applications` joined against
// `user_applications`; the *_existing fields describe the current
// user_applications state (zero values when no row exists yet).
type RenderCandidate struct {
	UserID     string
	SourceID   string
	AppID      string
	AppName    string
	AppVersion string
	AppType    string
	AppEntry   *types.ApplicationInfoEntry
	Price      *types.PriceConfig

	// Snapshot of the existing user_applications row, if any. ExistingUserAppID
	// is 0 when the row is missing (= never rendered for this user).
	//
	// ExistingAppVersion is the app's logical version (= metadata.version
	// in OlaresManifest.yaml) of the previously-rendered manifest, sourced
	// from user_applications.metadata->>'version'. NOT to be confused with
	// user_applications.manifest_version which stores the OlaresManifest
	// schema version (e.g. "0.8.10") and has nothing to do with upgrade
	// detection. Empty string when the row is missing or the metadata
	// column is NULL (legacy raw_data path); both cases trigger a (re-)
	// render via the candidate-selection query so legacy rows migrate
	// automatically on the next hydration cycle.
	ExistingUserAppID    int64
	ExistingRenderStatus string
	ExistingAppVersion   string
	ExistingFailures     int
}

// UpsertRenderSuccessInput captures everything UpsertRenderSuccess needs to
// flip a user_applications row to render_status='success'. Manifest carries
// the JSONB-column payloads (built via types.BuildUserAppManifest from
// chart-repo's raw_data); when nil the JSONB columns are written as SQL
// NULL — useful for first-time inserts before the manifest data is
// available.
type UpsertRenderSuccessInput struct {
	UserID   string
	SourceID string
	AppID    string

	AppName    string
	AppRawID   string
	AppRawName string

	ManifestVersion string
	ManifestType    string
	APIVersion      string

	Price *types.PriceConfig

	Manifest *types.UserAppManifest
}

// MarkRenderFailedInput captures the minimum fields required to upsert a
// failure row. AppName / AppRawID / AppRawName are only consumed on the
// INSERT side of the upsert (they satisfy NOT NULL constraints when the row
// doesn't exist yet) and are never overwritten on conflict.
type MarkRenderFailedInput struct {
	UserID   string
	SourceID string
	AppID    string

	AppName    string
	AppRawID   string
	AppRawName string

	RenderError string
}

// ListRenderCandidates returns up to `limit` (user, source, app) tuples that
// hydration should (re)render for the given user. It LEFT JOINs applications
// against user_applications to surface three classes of work:
//
//   - applications rows with no matching user_applications row (never rendered)
//   - user_applications rows in render_status='pending' or 'failed'
//   - user_applications rows where the previously-rendered app version
//     (metadata->>'version') drifted away from applications.app_version
//     (= source-side version bump)
//
// Note we compare metadata->>'version' (the app's logical version) against
// applications.app_version, NOT manifest_version: the manifest_version
// column stores the OlaresManifest schema version (e.g. "0.8.10") and has
// nothing to do with the app's logical version (e.g. "1.0.3"). Legacy
// raw_data rows where the metadata JSONB column is NULL also match this
// branch via NULL IS DISTINCT FROM, and migrate automatically on their
// next hydration cycle.
//
// User ↔ source visibility is intentionally not filtered here: every user
// sees every source's applications, matching the legacy cache-based fan-out.
// When user/source membership lands in PG, add an EXISTS clause against the
// future user_sources table.
func ListRenderCandidates(ctx context.Context, userID string, limit int) ([]RenderCandidate, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("userID cannot be empty")
	}
	if limit <= 0 {
		return nil, nil
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	type row struct {
		SourceID   string `gorm:"column:source_id"`
		AppID      string `gorm:"column:app_id"`
		AppName    string `gorm:"column:app_name"`
		AppVersion string `gorm:"column:app_version"`
		AppType    string `gorm:"column:app_type"`

		AppEntry *models.JSONB[types.ApplicationInfoEntry] `gorm:"column:app_entry"`
		Price    *models.JSONB[types.PriceConfig]          `gorm:"column:price"`

		ExistingUserAppID    int64  `gorm:"column:ua_id"`
		ExistingRenderStatus string `gorm:"column:ua_render_status"`
		ExistingAppVersion   string `gorm:"column:ua_existing_app_version"`
		ExistingFailures     int    `gorm:"column:ua_failures"`
	}

	const query = `
SELECT a.source_id,
       a.app_id,
       a.app_name,
       a.app_version,
       a.app_type,
       a.app_entry,
       a.price,
       COALESCE(ua.id, 0)                                  AS ua_id,
       COALESCE(ua.render_status, '')                      AS ua_render_status,
       COALESCE(ua.metadata->>'version', '')               AS ua_existing_app_version,
       COALESCE(ua.render_consecutive_failures, 0)         AS ua_failures
FROM applications a
LEFT JOIN user_applications ua
       ON ua.user_id   = ?
      AND ua.source_id = a.source_id
      AND ua.app_id    = a.app_id
WHERE ua.id IS NULL
   OR ua.render_status IN ('pending', 'failed')
   OR (ua.render_status = 'success' AND (ua.metadata->>'version') IS DISTINCT FROM a.app_version)
ORDER BY ua_failures, a.updated_at
LIMIT ?
`

	var rows []row
	if err := gdb.WithContext(ctx).Raw(query, userID, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list render candidates for user %s: %w", userID, err)
	}

	out := make([]RenderCandidate, 0, len(rows))
	for _, r := range rows {
		c := RenderCandidate{
			UserID:               userID,
			SourceID:             r.SourceID,
			AppID:                r.AppID,
			AppName:              r.AppName,
			AppVersion:           r.AppVersion,
			AppType:              r.AppType,
			ExistingUserAppID:    r.ExistingUserAppID,
			ExistingRenderStatus: r.ExistingRenderStatus,
			ExistingAppVersion:   r.ExistingAppVersion,
			ExistingFailures:     r.ExistingFailures,
		}
		if r.AppEntry != nil {
			entry := r.AppEntry.Data
			c.AppEntry = &entry
		}
		if r.Price != nil {
			price := r.Price.Data
			c.Price = &price
		}
		out = append(out, c)
	}
	return out, nil
}

// UpsertRenderSuccess inserts or updates the user_applications row for a
// successful render. On conflict it overwrites the columns this method
// owns (manifest scalars, price, render status fields, and all JSONB
// manifest columns); purchase_info is not touched here (payment step
// owns it).
//
// When in.Manifest is nil, the JSONB columns are written as SQL NULL —
// useful for callers that have only the scalar header info available.
func UpsertRenderSuccess(ctx context.Context, in UpsertRenderSuccessInput) error {
	if err := validateIdentity(in.UserID, in.SourceID, in.AppID); err != nil {
		return err
	}
	appName := strings.TrimSpace(in.AppName)
	if appName == "" {
		return fmt.Errorf("AppName cannot be empty")
	}
	appRawID := strings.TrimSpace(in.AppRawID)
	if appRawID == "" {
		appRawID = in.AppID
	}
	appRawName := strings.TrimSpace(in.AppRawName)
	if appRawName == "" {
		appRawName = appName
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	row := &models.UserApplication{
		UserID:                    in.UserID,
		SourceID:                  in.SourceID,
		AppID:                     in.AppID,
		AppName:                   appName,
		AppRawID:                  appRawID,
		AppRawName:                appRawName,
		ManifestVersion:           in.ManifestVersion,
		ManifestType:              in.ManifestType,
		APIVersion:                in.APIVersion,
		RenderStatus:              "success",
		RenderError:               "",
		RenderConsecutiveFailures: 0,
	}
	if in.Price != nil {
		priceJSON := models.NewJSONB(*in.Price)
		row.Price = &priceJSON
	}
	if in.Manifest != nil {
		assignManifestColumns(row, in.Manifest)
	}

	result := gdb.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "source_id"},
				{Name: "app_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"manifest_version",
				"manifest_type",
				"api_version",
				"price",
				"render_status",
				"render_error",
				"render_consecutive_failures",
				"metadata",
				"spec",
				"resources",
				"options",
				"entrances",
				"shared_entrances",
				"ports",
				"tailscale",
				"permission",
				"middleware",
				"envs",
			}),
		}).
		Create(row)
	if result.Error != nil {
		return fmt.Errorf("upsert user_applications success row (user=%s source=%s app=%s): %w",
			in.UserID, in.SourceID, in.AppID, result.Error)
	}
	return nil
}

// assignManifestColumns copies a UserAppManifest's payloads into the
// corresponding JSONB pointer fields on the model row. nil payloads leave
// the field nil so GORM writes SQL NULL.
func assignManifestColumns(row *models.UserApplication, m *types.UserAppManifest) {
	if m.Metadata != nil {
		j := models.NewJSONB(m.Metadata)
		row.Metadata = &j
	}
	if m.Spec != nil {
		j := models.NewJSONB(m.Spec)
		row.Spec = &j
	}
	if m.Resources != nil {
		j := models.NewJSONB(m.Resources)
		row.Resources = &j
	}
	if m.Options != nil {
		j := models.NewJSONB(m.Options)
		row.Options = &j
	}
	if m.Tailscale != nil {
		j := models.NewJSONB(m.Tailscale)
		row.Tailscale = &j
	}
	if m.Permission != nil {
		j := models.NewJSONB(m.Permission)
		row.Permission = &j
	}
	if m.Middleware != nil {
		j := models.NewJSONB(m.Middleware)
		row.Middleware = &j
	}
	if m.Entrances != nil {
		j := models.NewJSONB(m.Entrances)
		row.Entrances = &j
	}
	if m.SharedEntrances != nil {
		j := models.NewJSONB(m.SharedEntrances)
		row.SharedEntrances = &j
	}
	if m.Ports != nil {
		j := models.NewJSONB(m.Ports)
		row.Ports = &j
	}
	if m.Envs != nil {
		j := models.NewJSONB(m.Envs)
		row.Envs = &j
	}
}

// MarkRenderFailed records a render failure for the given (user, source, app).
// It inserts a placeholder row when none exists; on conflict it bumps
// render_consecutive_failures, sets render_status='failed' and stores the
// (truncated) error message — without touching previously rendered manifest
// data so the UI can keep showing the last known good render.
func MarkRenderFailed(ctx context.Context, in MarkRenderFailedInput) error {
	if err := validateIdentity(in.UserID, in.SourceID, in.AppID); err != nil {
		return err
	}
	appName := strings.TrimSpace(in.AppName)
	if appName == "" {
		return fmt.Errorf("AppName cannot be empty")
	}
	appRawID := strings.TrimSpace(in.AppRawID)
	if appRawID == "" {
		appRawID = in.AppID
	}
	appRawName := strings.TrimSpace(in.AppRawName)
	if appRawName == "" {
		appRawName = appName
	}
	renderError := truncate(in.RenderError, renderErrorMaxLen)

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	row := &models.UserApplication{
		UserID:                    in.UserID,
		SourceID:                  in.SourceID,
		AppID:                     in.AppID,
		AppName:                   appName,
		AppRawID:                  appRawID,
		AppRawName:                appRawName,
		RenderStatus:              "failed",
		RenderError:               renderError,
		RenderConsecutiveFailures: 1,
	}

	result := gdb.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "source_id"},
				{Name: "app_id"},
			},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"render_status":               "failed",
				"render_error":                renderError,
				"render_consecutive_failures": gorm.Expr("user_applications.render_consecutive_failures + 1"),
			}),
		}).
		Create(row)
	if result.Error != nil {
		return fmt.Errorf("upsert user_applications failure row (user=%s source=%s app=%s): %w",
			in.UserID, in.SourceID, in.AppID, result.Error)
	}
	return nil
}

// UpsertPurchaseInfo writes the purchase_info JSONB column. It assumes the
// row already exists (created by an earlier UpsertRenderSuccess); if it
// doesn't, ErrUserApplicationNotFound is returned so the caller can decide
// whether to retry or surface a programming error. Pass pi=nil to clear the
// column.
func UpsertPurchaseInfo(ctx context.Context, userID, sourceID, appID string, pi *types.PurchaseInfo) error {
	if err := validateIdentity(userID, sourceID, appID); err != nil {
		return err
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	var payload interface{}
	if pi != nil {
		j := models.NewJSONB(*pi)
		payload = &j
	}

	result := gdb.WithContext(ctx).
		Model(&models.UserApplication{}).
		Where("user_id = ? AND source_id = ? AND app_id = ?", userID, sourceID, appID).
		Update("purchase_info", payload)
	if result.Error != nil {
		return fmt.Errorf("update user_applications.purchase_info (user=%s source=%s app=%s): %w",
			userID, sourceID, appID, result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrUserApplicationNotFound
	}
	return nil
}

// GetUserApplication fetches a single user_applications row by its natural
// key. Returns (nil, nil) when no row exists; returns the full model
// (including JSONB payloads) otherwise.
func GetUserApplication(ctx context.Context, userID, sourceID, appID string) (*models.UserApplication, error) {
	if err := validateIdentity(userID, sourceID, appID); err != nil {
		return nil, err
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	var row models.UserApplication
	err := gdb.WithContext(ctx).
		Where("user_id = ? AND source_id = ? AND app_id = ?", userID, sourceID, appID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get user_applications (user=%s source=%s app=%s): %w",
			userID, sourceID, appID, err)
	}
	return &row, nil
}

func validateIdentity(userID, sourceID, appID string) error {
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("userID cannot be empty")
	}
	if strings.TrimSpace(sourceID) == "" {
		return fmt.Errorf("sourceID cannot be empty")
	}
	if strings.TrimSpace(appID) == "" {
		return fmt.Errorf("appID cannot be empty")
	}
	return nil
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
