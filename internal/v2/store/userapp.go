package store

import (
	"context"
	"encoding/json"
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
// CloneUserApplicationInput is the input bag for materialising a clone-side
// user_applications row alongside its pending user_application_states row
// in a single PG transaction.
//
// Identity:
//
//   - UserID / SourceID / SrcAppName locate the source (original) row.
//     The source row is identified by (user, source, app_name) plus the
//     intrinsic filter app_id = app_raw_id (= original chart row, not a
//     prior clone) and render_status = 'success'.
//   - NewAppID / NewAppName are the clone row's own identity columns. The
//     caller derives them deterministically from the request body (see
//     pkg/v2/api/task.go cloneAppIDFromName / calculateCloneRequestHash).
//
// Display overrides — see applyCloneOverrides for the field-by-field
// patch contract. The empty-string interpretation here is intentionally
// "clear" rather than "preserve": the clone request comes from a user
// form whose blanked field is meant to wipe the inherited title, not
// fall back to the original chart's text. M < N entries leave the
// trailing source entrances untouched; M > N entries are silently
// dropped.
//
//   - Title              -> user_applications.metadata.title
//   - EntranceTitles[i]  -> user_applications.entrances[i].title
//
// PendingStateVersion anchors user_application_states.installed_version /
// target_version at row creation. cloneApp passes the original app's
// currently installed version so a later uninstall can JOIN on it via
// LookupInstalledApp the same way install paths do.
type CloneUserApplicationInput struct {
	UserID     string
	SourceID   string
	SrcAppName string

	NewAppID   string
	NewAppName string

	Title          string
	EntranceTitles []string

	PendingStateVersion string
}

// CloneUserApplication materialises a clone-side user_applications row and
// the matching user_application_states pending row in one PG transaction.
//
// Mechanics (per the refactor agreed for the read-modify-write path):
//
//  1. SELECT the source row's columns into Go memory. JSONB columns are
//     read as raw bytes (driver returns []byte) so non-patched fields
//     round-trip without going through any Go-side parse / re-encode —
//     numeric precision, key order inside individual JSONB objects, and
//     unknown-key payloads all survive untouched.
//  2. Patch user_applications.metadata.title and
//     user_applications.entrances[i].title via applyCloneOverrides, which
//     decodes ONLY the keys it needs (map[string]json.RawMessage /
//     []json.RawMessage) so the surrounding bytes are still preserved.
//  3. INSERT the patched + identity-substituted row into user_applications.
//     ON CONFLICT (user_id, source_id, app_id) DO UPDATE SET updated_at =
//     NOW() — defensive against a concurrent identical clone slipping
//     past the upstream duplicate guard; the caller-side check normally
//     rejects duplicates before reaching this function.
//  4. Refresh the pending user_application_states row via
//     upsertPendingStateInTx (op_type = 'clone', Version =
//     PendingStateVersion) so the clone has a state row anchored to its
//     own user_applications.id before the executor's linkStateOpID call.
//
// Both writes commit together. On any failure the transaction rolls back,
// leaving Market with no half-materialised clone — important because the
// caller (AppClone executor) has already received an opID from app-service
// and the clone is mid-install on the cluster; surfacing the error lets
// the executor log loudly enough for operator-driven reconciliation.
//
// Returns ErrUserApplicationNotFound when the source row cannot be located
// (caller validation issue — cloneApp's HTTP handler resolves the source
// via GetAppInstallRow first, so a miss here is a programmer error or a
// race with a concurrent delete).
func CloneUserApplication(ctx context.Context, in CloneUserApplicationInput) error {
	in.UserID = strings.TrimSpace(in.UserID)
	in.SourceID = strings.TrimSpace(in.SourceID)
	in.SrcAppName = strings.TrimSpace(in.SrcAppName)
	in.NewAppID = strings.TrimSpace(in.NewAppID)
	in.NewAppName = strings.TrimSpace(in.NewAppName)
	if in.UserID == "" || in.SourceID == "" || in.SrcAppName == "" || in.NewAppID == "" || in.NewAppName == "" {
		return fmt.Errorf("CloneUserApplication: empty UserID/SourceID/SrcAppName/NewAppID/NewAppName")
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Step 1: read the source row's columns. JSONB columns come back
		// as []byte (lib/pq / pgx both surface jsonb as []byte). nil ->
		// SQL NULL in the source; we mirror that on the way out via
		// nullableJSON.
		const selectQuery = `
SELECT
    app_raw_id, app_raw_name,
    manifest_version, manifest_type, api_version,
    metadata, spec, resources, options, entrances, shared_entrances, ports,
    tailscale, permission, middleware, envs,
    price, purchase_info, i18n, version_history, image_analysis,
    raw_package, rendered_package
FROM user_applications
WHERE user_id       = ?
  AND source_id     = ?
  AND app_name      = ?
  AND app_id        = app_raw_id
  AND render_status = 'success'
LIMIT 1
`
		var src struct {
			AppRawID        string `gorm:"column:app_raw_id"`
			AppRawName      string `gorm:"column:app_raw_name"`
			ManifestVersion string `gorm:"column:manifest_version"`
			ManifestType    string `gorm:"column:manifest_type"`
			APIVersion      string `gorm:"column:api_version"`
			Metadata        []byte `gorm:"column:metadata"`
			Spec            []byte `gorm:"column:spec"`
			Resources       []byte `gorm:"column:resources"`
			Options         []byte `gorm:"column:options"`
			Entrances       []byte `gorm:"column:entrances"`
			SharedEntrances []byte `gorm:"column:shared_entrances"`
			Ports           []byte `gorm:"column:ports"`
			Tailscale       []byte `gorm:"column:tailscale"`
			Permission      []byte `gorm:"column:permission"`
			Middleware      []byte `gorm:"column:middleware"`
			Envs            []byte `gorm:"column:envs"`
			Price           []byte `gorm:"column:price"`
			PurchaseInfo    []byte `gorm:"column:purchase_info"`
			I18n            []byte `gorm:"column:i18n"`
			VersionHistory  []byte `gorm:"column:version_history"`
			ImageAnalysis   []byte `gorm:"column:image_analysis"`
			RawPackage      string `gorm:"column:raw_package"`
			RenderedPackage string `gorm:"column:rendered_package"`
		}
		res := tx.Raw(selectQuery, in.UserID, in.SourceID, in.SrcAppName).Scan(&src)
		if err := res.Error; err != nil {
			return fmt.Errorf("clone user_applications: select source (user=%s source=%s app=%s): %w",
				in.UserID, in.SourceID, in.SrcAppName, err)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: clone source not found (user=%s source=%s app=%s)", ErrUserApplicationNotFound,
				in.UserID, in.SourceID, in.SrcAppName)
		}

		// Step 2: patch metadata.title + entrances[*].title in Go, leaving
		// every other byte untouched.
		patchedMetadata, patchedEntrances, err := applyCloneOverrides(src.Metadata, src.Entrances, in.Title, in.EntranceTitles)
		if err != nil {
			return fmt.Errorf("clone user_applications: apply overrides (user=%s source=%s app=%s): %w",
				in.UserID, in.SourceID, in.SrcAppName, err)
		}

		// Step 3: write the clone row. INSERT...VALUES with the source's
		// bytes for non-patched JSONB columns and the patched bytes for
		// metadata / entrances. nullableJSON keeps SQL NULL inputs as NULL
		// rather than promoting them to '{}' or '[]'.
		const insertQuery = `
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
) VALUES (
    ?, ?,
    ?, ?, ?, ?,
    ?, ?, ?,
    ?, ?, ?, ?, ?, ?, ?,
    ?, ?, ?, ?,
    ?, ?, ?, ?, ?,
    ?, ?,
    'success', '', 0,
    FALSE, NOW(), NOW()
)
ON CONFLICT (user_id, source_id, app_id) DO UPDATE SET
    updated_at = NOW()
`
		if err := tx.Exec(insertQuery,
			in.UserID, in.SourceID,
			in.NewAppID, src.AppRawID, in.NewAppName, src.AppRawName,
			src.ManifestVersion, src.ManifestType, src.APIVersion,
			nullableJSON(patchedMetadata), nullableJSON(src.Spec), nullableJSON(src.Resources), nullableJSON(src.Options),
			nullableJSON(patchedEntrances), nullableJSON(src.SharedEntrances), nullableJSON(src.Ports),
			nullableJSON(src.Tailscale), nullableJSON(src.Permission), nullableJSON(src.Middleware), nullableJSON(src.Envs),
			nullableJSON(src.Price), nullableJSON(src.PurchaseInfo), nullableJSON(src.I18n), nullableJSON(src.VersionHistory), nullableJSON(src.ImageAnalysis),
			src.RawPackage, src.RenderedPackage,
		).Error; err != nil {
			return fmt.Errorf("clone user_applications: insert (user=%s source=%s src=%s -> new=%s/%s): %w",
				in.UserID, in.SourceID, in.SrcAppName, in.NewAppName, in.NewAppID, err)
		}

		// Step 4: refresh the pending state row in the same transaction.
		// upsertPendingStateInTx handles the INSERT-or-UPDATE on the
		// (user_id, source_id, app_id) tuple via the FK chain through
		// user_applications.id; the clone row was just inserted above,
		// so it must be visible to this lookup.
		if err := upsertPendingStateInTx(tx, PendingTaskState{
			UserID:   in.UserID,
			SourceID: in.SourceID,
			AppID:    in.NewAppID,
			OpType:   "clone",
			Version:  in.PendingStateVersion,
		}); err != nil {
			return fmt.Errorf("clone user_applications: upsert pending state (user=%s source=%s app=%s): %w",
				in.UserID, in.SourceID, in.NewAppID, err)
		}

		return nil
	})
}

// applyCloneOverrides patches the title field of the source's metadata
// JSONB object and the title field of each source entrance JSONB element
// covered by entranceTitles, returning the new JSONB bytes for both
// columns. Non-title fields round-trip via json.RawMessage so their
// original bytes are preserved (numeric precision, unknown keys, nested
// payloads).
//
// Per-field semantics:
//
//   - Title: written verbatim into metadata.title. Empty string clears
//     the inherited title (the key is set to "", not removed). When the
//     source has no metadata column (NULL) AND Title is empty, the
//     output metadata stays nil so the clone row also stores SQL NULL.
//     When the source has no metadata AND Title is non-empty, a fresh
//     {"title": Title} object is created.
//   - EntranceTitles: written verbatim into entrances[i].title for
//     i in [0, len(EntranceTitles)). Indices >= len(source entrances)
//     are silently dropped (request overshoot). Indices >=
//     len(EntranceTitles) leave the corresponding source entrance
//     untouched. Empty title at a covered index clears that entrance's
//     title.
//
// Returns the patched bytes (or the source bytes verbatim when no patch
// is applicable). Caller is responsible for translating nil -> SQL NULL
// at the DB boundary via nullableJSON.
func applyCloneOverrides(metadataIn, entrancesIn []byte, title string, entranceTitles []string) (metadataOut, entrancesOut []byte, err error) {
	// metadata
	switch {
	case len(metadataIn) == 0 && title == "":
		// No source metadata and no override -> preserve SQL NULL.
		metadataOut = nil
	case len(metadataIn) == 0:
		// Source metadata is NULL but the caller wants a title. Synthesize
		// a single-key object so the clone still has a title.
		out, err := json.Marshal(map[string]json.RawMessage{
			"title": jsonString(title),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("encode synthetic metadata: %w", err)
		}
		metadataOut = out
	default:
		var meta map[string]json.RawMessage
		if err := json.Unmarshal(metadataIn, &meta); err != nil {
			return nil, nil, fmt.Errorf("decode metadata: %w", err)
		}
		if meta == nil {
			meta = map[string]json.RawMessage{}
		}
		meta["title"] = jsonString(title)
		out, err := json.Marshal(meta)
		if err != nil {
			return nil, nil, fmt.Errorf("encode metadata: %w", err)
		}
		metadataOut = out
	}

	// entrances
	switch {
	case len(entrancesIn) == 0:
		// Source has no entrances column. Nothing to patch; mirror NULL.
		entrancesOut = nil
	case len(entranceTitles) == 0:
		// No overrides -> copy bytes verbatim.
		entrancesOut = entrancesIn
	default:
		var entries []json.RawMessage
		if err := json.Unmarshal(entrancesIn, &entries); err != nil {
			return nil, nil, fmt.Errorf("decode entrances: %w", err)
		}
		for i, t := range entranceTitles {
			if i >= len(entries) {
				// Request overshoot: no source entrance at this index.
				// Per the M > N rule the extras are silently dropped.
				break
			}
			var item map[string]json.RawMessage
			if len(entries[i]) > 0 {
				if err := json.Unmarshal(entries[i], &item); err != nil {
					return nil, nil, fmt.Errorf("decode entrances[%d]: %w", i, err)
				}
			}
			if item == nil {
				item = map[string]json.RawMessage{}
			}
			item["title"] = jsonString(t)
			newItem, err := json.Marshal(item)
			if err != nil {
				return nil, nil, fmt.Errorf("encode entrances[%d]: %w", i, err)
			}
			entries[i] = newItem
		}
		out, err := json.Marshal(entries)
		if err != nil {
			return nil, nil, fmt.Errorf("encode entrances: %w", err)
		}
		entrancesOut = out
	}

	return metadataOut, entrancesOut, nil
}

// jsonString returns the JSON encoding of s (always a quoted string).
// Wrapping json.Marshal here so callers reading applyCloneOverrides see
// "JSON-encode the title" intent at a glance rather than parsing the
// raw call.
func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
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
