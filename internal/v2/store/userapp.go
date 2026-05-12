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
// `market_sources` (for source_type) and `user_applications`; the *_existing
// fields describe the current user_applications state (zero values when no
// row exists yet).
//
// SourceType is the market source flavour — "local" | "remote" — sourced from
// market_sources.source_type via INNER JOIN. The INNER JOIN encodes the
// invariant that every applications.source_id has a matching market_sources
// row (market_sources is a superset of applications.source_id), so SourceType
// is guaranteed non-empty for any candidate this function returns. Hydration
// steps that branch on local vs remote rely on this guarantee.
type RenderCandidate struct {
	UserID     string
	SourceID   string
	SourceType string
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

	// UserZone is the per-instance domain suffix sourced from the User CR's
	// bytetrade.io/zone annotation (e.g. "alice.olares.com"). It is NOT
	// populated by ListRenderCandidates' SQL — the zone lives in k8s
	// annotations, not in PG — and is instead injected by the hydration
	// pipeline (see Pipeline.phaseHydrateApps) before the candidate is
	// dispatched. Downstream steps (notably types.EnrichEntranceURLs) use
	// it to derive entrance / shared-entrance URLs the same way app-service
	// does at install time. Empty when the user has no zone annotation
	// yet — callers must treat that as "skip URL rewriting" rather than
	// fabricate a fallback.
	UserZone string
}

// UpsertRenderSuccessInput captures everything UpsertRenderSuccess needs to
// flip a user_applications row to render_status='success'. Manifest carries
// the JSONB-column payloads (built via types.BuildUserAppManifest from
// chart-repo's raw_data); when nil the JSONB columns are written as SQL
// NULL — useful for first-time inserts before the manifest data is
// available.
//
// I18n / VersionHistory are sourced directly from chart-repo's sync-app
// response (top-level fields, not nested under raw_data_ex) and persist
// to dedicated JSONB columns. Empty / nil values write SQL NULL so a
// caller without those payloads does not clobber a previously stored
// non-empty bundle.
//
// ImageAnalysis carries chart-repo's per-app docker image analysis
// (types.ImageAnalysisResult) emitted alongside raw_data_ex. nil here
// writes SQL NULL — same "don't clobber on absence" rule as I18n /
// VersionHistory; the failure / placeholder paths supply nil and the
// previously-stored value (if any) survives.
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

	I18n           map[string]map[string]string
	VersionHistory []types.VersionInfo
	ImageAnalysis  *types.ImageAnalysisResult

	// RawPackage / RenderedPackage are chart-repo filesystem paths
	// returned by sync-app at the same level as raw_data_ex. Empty
	// strings are written verbatim — chart-repo always emits them on
	// the success path, so an empty value here typically signals a
	// caller bug rather than a missing upstream field. The OnConflict
	// path overwrites both columns in full so a re-render reflects
	// chart-repo's latest paths.
	RawPackage      string
	RenderedPackage string
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
// hydration should (re)render for the given user. It joins applications
// against market_sources (INNER, for source_type) and user_applications
// (LEFT, for the existing render state) to surface three classes of work:
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
// The INNER JOIN against market_sources serves a single purpose: enrich
// each candidate with source_type (local | remote) so downstream hydration
// steps can branch on it. It does NOT act as a filter — the relation
// applications.source_id ⊆ market_sources.source_id is a system-wide
// invariant (market_sources is the superset; applications can only carry
// source_ids that already exist there), so the join cannot remove any row
// that LEFT JOIN would have returned. We deliberately do NOT filter by
// market_sources.is_active / nsfw / priority here either; those would
// silently change the candidate set when a source is toggled, which is
// a separate product decision.
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
		SourceType string `gorm:"column:source_type"`
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
       ms.source_type                              AS source_type,
       a.app_id,
       a.app_name,
       a.app_version,
       a.app_type,
       a.app_entry,
       a.price,
       COALESCE(ua.id, 0)                          AS ua_id,
       COALESCE(ua.render_status, '')              AS ua_render_status,
       COALESCE(ua.metadata->>'version', '')       AS ua_existing_app_version,
       COALESCE(ua.render_consecutive_failures, 0) AS ua_failures
FROM applications a
JOIN market_sources ms
       ON ms.source_id = a.source_id
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
			SourceType:           r.SourceType,
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
		RawPackage:                in.RawPackage,
		RenderedPackage:           in.RenderedPackage,
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
	if len(in.I18n) > 0 {
		j := models.NewJSONB(in.I18n)
		row.I18n = &j
	}
	if len(in.VersionHistory) > 0 {
		j := models.NewJSONB(in.VersionHistory)
		row.VersionHistory = &j
	}
	if in.ImageAnalysis != nil {
		j := models.NewJSONB(*in.ImageAnalysis)
		row.ImageAnalysis = &j
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
				"i18n",
				"version_history",
				"image_analysis",
				"raw_package",
				"rendered_package",
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
// CloneUserApplication materialises a clone-side user_applications row by
// copying the source (original) row in-place and substituting only the
// identity columns the clone needs to be addressable independently:
//
//   - app_id   <- newAppID   (caller passes md5(newAppName)[:32])
//   - app_name <- newAppName (caller passes rawAppName + requestHash)
//
// app_raw_id and app_raw_name carry over from the source row so the
// clone can still be traced back to the original chart in upgrade /
// SCC paths. Every other column (manifest blocks, JSONB payloads,
// rendered_package, image_analysis, price, ...) is copied verbatim
// because the clone shares the original's chart and rendered output;
// nothing about the clone install needs a fresh chart-repo render.
//
// render_status is forced to 'success' (and render_error / failures
// reset) so downstream API helpers that filter by render_status='success'
// surface the clone immediately. is_upgrade is reset to FALSE.
//
// The whole operation runs as a single INSERT...SELECT...ON CONFLICT
// DO UPDATE so:
//
//   - all per-column values come straight from the same source row
//     atomically (no read-then-write race); the source identity is
//     fixed by (user_id, source_id, srcAppName, app_id = app_raw_id);
//   - re-issuing the same clone request (same newAppName / newAppID
//     because requestHash is deterministic from the request body) is
//     idempotent: ON CONFLICT just bumps updated_at, preserving the
//     clone row's data even if the original has drifted since the
//     first clone attempt.
//
// Returns nil when the row was inserted or refreshed; ErrUserApplicationNotFound
// when the source row cannot be located (caller validation issue —
// cloneApp's GetAppInstallRow already proved the source exists, so a
// miss here is a programmer error / race with a concurrent delete).
func CloneUserApplication(ctx context.Context, userID, sourceID, srcAppName, newAppID, newAppName string) error {
	userID = strings.TrimSpace(userID)
	sourceID = strings.TrimSpace(sourceID)
	srcAppName = strings.TrimSpace(srcAppName)
	newAppID = strings.TrimSpace(newAppID)
	newAppName = strings.TrimSpace(newAppName)
	if userID == "" || sourceID == "" || srcAppName == "" || newAppID == "" || newAppName == "" {
		return fmt.Errorf("CloneUserApplication: empty userID/sourceID/srcAppName/newAppID/newAppName")
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	const query = `
INSERT INTO user_applications (
    user_id, source_id,
    app_id, app_raw_id, app_name, app_raw_name,
    manifest_version, manifest_type, api_version,
    metadata, spec, resources, options, entrances, shared_entrances, ports,
    tailscale, permission, middleware, envs,
    price, purchase_info, i18n, version_history, image_analysis,
    raw_package, rendered_package,
    render_status, render_error, render_consecutive_failures,
    is_upgrade, created_at, updated_at
)
SELECT
    src.user_id, src.source_id,
    ?, src.app_raw_id, ?, src.app_raw_name,
    src.manifest_version, src.manifest_type, src.api_version,
    src.metadata, src.spec, src.resources, src.options, src.entrances, src.shared_entrances, src.ports,
    src.tailscale, src.permission, src.middleware, src.envs,
    src.price, src.purchase_info, src.i18n, src.version_history, src.image_analysis,
    src.raw_package, src.rendered_package,
    'success', '', 0,
    FALSE, NOW(), NOW()
FROM user_applications src
WHERE src.user_id       = ?
  AND src.source_id     = ?
  AND src.app_name      = ?
  AND src.app_id        = src.app_raw_id
  AND src.render_status = 'success'
LIMIT 1
ON CONFLICT (user_id, source_id, app_id) DO UPDATE SET
    updated_at = NOW()
`

	res := gdb.WithContext(ctx).Exec(query,
		newAppID, newAppName,
		userID, sourceID, srcAppName,
	)
	if err := res.Error; err != nil {
		return fmt.Errorf("clone user_applications (user=%s source=%s src=%s -> new=%s/%s): %w",
			userID, sourceID, srcAppName, newAppName, newAppID, err)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: clone source not found (user=%s source=%s app=%s)", ErrUserApplicationNotFound,
			userID, sourceID, srcAppName)
	}
	return nil
}

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
