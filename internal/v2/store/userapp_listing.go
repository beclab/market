package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"market/internal/v2/db"
	"market/internal/v2/db/models"
	"market/internal/v2/helper"
	"market/internal/v2/types"
)

// AppInfoSimpleRow is the projection ListAppInfoLatestForUser yields.
// Each row carries enough data to assemble the FilteredAppInfoLatestData
// payload returned by /market/data: the manifest metadata block (for
// AppSimpleInfo's app-facing fields), the chart-repo i18n bundle (for
// localised title / description), and the catalog-level app_entry
// (for SupportArch / AppLabels which the manifest metadata does not
// always carry).
type AppInfoSimpleRow struct {
	SourceID     string                                      `gorm:"column:source_id"`
	AppID        string                                      `gorm:"column:app_id"`
	AppName      string                                      `gorm:"column:app_name"`
	ManifestType string                                      `gorm:"column:manifest_type"`
	AppVersion   string                                      `gorm:"column:app_version"`
	UpdatedAt    time.Time                                   `gorm:"column:updated_at"`
	Metadata     *models.JSONB[map[string]any]               `gorm:"column:metadata"`
	I18n         *models.JSONB[map[string]map[string]string] `gorm:"column:i18n"`
	AppEntry     *models.JSONB[types.ApplicationInfoEntry]   `gorm:"column:app_entry"`
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
		        ua.i18n,
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

// AppQueryKey identifies a single user_applications row in a
// per-(source, app) bulk lookup. The user_id is supplied separately
// because every row in a single ListAppDetailsForUser call shares
// the same user_id.
//
// AppName matches against user_applications.app_name (the manifest's
// metadata.name) rather than user_applications.app_id (the short
// hash) because every existing client surfaces the name as its
// addressable "app id" on the wire. (user_id, source_id, app_name)
// is NOT unique at the schema level — clones may share a name with
// the original app — so a name collision returns multiple rows; in
// that case the deterministic ORDER BY in the SQL picks one. Switch
// to AppID once a client sends the hashed id.
type AppQueryKey struct {
	SourceID string
	AppName  string
}

// AppDetailRow is the projection ListAppDetailsForUser yields. It
// carries every column needed to rebuild the API-contract
// *types.ApplicationInfoEntry: the eleven manifest JSONB blocks plus
// the standalone i18n / version_history columns plus the scalar
// identity fields. Price tags along because /api/v2/apps callers
// also surface paid-app info today (and price is per-(user, app)).
type AppDetailRow struct {
	SourceID        string                                      `gorm:"column:source_id"`
	AppID           string                                      `gorm:"column:app_id"`
	AppName         string                                      `gorm:"column:app_name"`
	AppRawID        string                                      `gorm:"column:app_raw_id"`
	AppRawName      string                                      `gorm:"column:app_raw_name"`
	ManifestVersion string                                      `gorm:"column:manifest_version"`
	ManifestType    string                                      `gorm:"column:manifest_type"`
	APIVersion      string                                      `gorm:"column:api_version"`
	UpdatedAt       time.Time                                   `gorm:"column:updated_at"`
	Metadata        *models.JSONB[map[string]any]               `gorm:"column:metadata"`
	Spec            *models.JSONB[map[string]any]               `gorm:"column:spec"`
	Resources       *models.JSONB[map[string]any]               `gorm:"column:resources"`
	Options         *models.JSONB[map[string]any]               `gorm:"column:options"`
	Tailscale       *models.JSONB[map[string]any]               `gorm:"column:tailscale"`
	Permission      *models.JSONB[map[string]any]               `gorm:"column:permission"`
	Middleware      *models.JSONB[map[string]any]               `gorm:"column:middleware"`
	Entrances       *models.JSONB[[]map[string]any]             `gorm:"column:entrances"`
	SharedEntrances *models.JSONB[[]map[string]any]             `gorm:"column:shared_entrances"`
	Ports           *models.JSONB[[]map[string]any]             `gorm:"column:ports"`
	Envs            *models.JSONB[[]map[string]any]             `gorm:"column:envs"`
	I18n            *models.JSONB[map[string]map[string]string] `gorm:"column:i18n"`
	VersionHistory  *models.JSONB[[]types.VersionInfo]          `gorm:"column:version_history"`
	Price           *models.JSONB[types.PriceConfig]            `gorm:"column:price"`
	AppEntry        *models.JSONB[types.ApplicationInfoEntry]   `gorm:"column:app_entry"`
}

// ListAppDetailsForUser fetches the full user_applications +
// applications JOIN row for each (source_id, app_id) tuple in keys,
// scoped to userID and render_status='success'. Used by /api/v2/apps
// to compose ApplicationInfoEntry from PG.
//
// The query uses PG's row-tuple IN expression to batch all keys into
// a single round-trip. Empty userID or empty keys return ([], nil).
func ListAppDetailsForUser(ctx context.Context, userID string, keys []AppQueryKey) ([]*AppDetailRow, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || len(keys) == 0 {
		return []*AppDetailRow{}, nil
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	tuples := make([]string, 0, len(keys))
	args := make([]interface{}, 0, 2+2*len(keys))
	args = append(args, userID, "success")
	for _, k := range keys {
		tuples = append(tuples, "(?, ?)")
		args = append(args, k.SourceID, k.AppName)
	}

	query := `
SELECT
    ua.source_id,
    ua.app_id,
    ua.app_name,
    ua.app_raw_id,
    ua.app_raw_name,
    ua.manifest_version,
    ua.manifest_type,
    ua.api_version,
    ua.updated_at,
    ua.metadata,
    ua.spec,
    ua.resources,
    ua.options,
    ua.tailscale,
    ua.permission,
    ua.middleware,
    ua.entrances,
    ua.shared_entrances,
    ua.ports,
    ua.envs,
    ua.i18n,
    ua.version_history,
    ua.price,
    a.app_entry
FROM user_applications ua
LEFT JOIN applications a
    ON a.source_id = ua.source_id AND a.app_id = ua.app_id
WHERE ua.user_id = ?
  AND ua.render_status = ?
  AND (ua.source_id, ua.app_name) IN (` + strings.Join(tuples, ",") + `)
ORDER BY ua.source_id, ua.app_name, ua.app_id
`

	var rows []*AppDetailRow
	if err := gdb.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list app details for user %s: %w", userID, err)
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

// AppInstallRow is the projection GetAppInstallRow yields. It carries
// the user_applications columns the install / upgrade / clone API
// handlers need to populate task metadata for the downstream app-service
// call: cfgType (manifest_type), the chart-repo rendered_package path
// used to derive chart_path, the image_analysis blob unpacked into
// task.Image[], and the per-(user, app) price config the VC injection
// step reads.
//
// All fields are sourced from user_applications. The applications table
// (catalogue-level app_entry) is intentionally NOT joined: app
// operations are scoped to the user's rendered manifest and any
// realAppID derivation is satisfied by user_applications.app_id (which
// is the manifest's id, identical to applications.app_entry.ID for the
// non-clone rows install / upgrade / clone act on — clones are filtered
// out by the SQL's app_id = app_raw_id predicate).
//
// Heavyweight manifest JSONB blocks (metadata / spec / resources / ...)
// are NOT projected here on purpose — the action handlers do not
// consume them and the bandwidth cost would be substantial;
// getAppsInfo (/api/v2/apps) keeps using AppDetailRow when the rich
// payload is required.
type AppInstallRow struct {
	SourceID        string                                   `gorm:"column:source_id"`
	AppID           string                                   `gorm:"column:app_id"`
	AppRawID        string                                   `gorm:"column:app_raw_id"`
	AppName         string                                   `gorm:"column:app_name"`
	AppRawName      string                                   `gorm:"column:app_raw_name"`
	ManifestType    string                                   `gorm:"column:manifest_type"`
	AppVersion      string                                   `gorm:"column:app_version"`
	RenderedPackage string                                   `gorm:"column:rendered_package"`
	Price           *models.JSONB[types.PriceConfig]         `gorm:"column:price"`
	ImageAnalysis   *models.JSONB[types.ImageAnalysisResult] `gorm:"column:image_analysis"`
}

// GetAppInstallRow fetches the single user_applications row that the
// install / upgrade / clone API handlers need to dispatch a backend
// call.
//
// version="" matches any version of the named app; this is the cloneApp
// path, where the version is later read from user_application_states
// (via GetInstalledAppVersion) rather than supplied by the caller. A
// non-empty version restricts the match exactly, mirroring installApp's
// "must match the version the user clicked" behaviour.
//
// The "ua.app_id = ua.app_raw_id" predicate intentionally excludes
// clone rows: install / upgrade / clone all act on the original chart
// even when the wire-level app_name is a clone alias, and the original
// row is the only one that carries the unmodified manifest_type and
// price config the action handlers consume.
//
// render_status is restricted to 'success' so failure / pending
// placeholder rows from MarkRenderFailed do not match. Returns
// (nil, nil) when no row matches; the caller is expected to surface a
// 404-style response in that case.
func GetAppInstallRow(ctx context.Context, userID, sourceID, appName, version string) (*AppInstallRow, error) {
	userID = strings.TrimSpace(userID)
	sourceID = strings.TrimSpace(sourceID)
	appName = strings.TrimSpace(appName)
	if userID == "" || sourceID == "" || appName == "" {
		return nil, fmt.Errorf("GetAppInstallRow: empty userID/sourceID/appName")
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	const query = `
SELECT ua.source_id,
       ua.app_id,
       ua.app_raw_id,
       ua.app_name,
       ua.app_raw_name,
       ua.manifest_type,
       COALESCE(ua.metadata->>'version', '') AS app_version,
       ua.rendered_package,
       ua.price,
       ua.image_analysis
FROM user_applications ua
WHERE ua.user_id        = ?
  AND ua.source_id      = ?
  AND ua.app_name       = ?
  AND ua.app_id         = ua.app_raw_id
  AND ua.render_status  = 'success'
  AND (? = '' OR ua.metadata->>'version' = ?)
ORDER BY ua.updated_at DESC
LIMIT 1
`

	var rows []*AppInstallRow
	if err := gdb.WithContext(ctx).Raw(query, userID, sourceID, appName, version, version).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("get app install row (user=%s source=%s app=%s version=%s): %w",
			userID, sourceID, appName, version, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// GetAppUpgradeRow resolves the user_applications row that the
// upgradeApp handler must dispatch a chart-upgrade against. Unlike
// GetAppInstallRow, the wire-level app_name supplied by the client
// may legitimately be either:
//
//   1. the original app's name (app_id == app_raw_id row), or
//   2. a clone alias (app_id != app_raw_id row, where app_raw_name
//      points at the underlying chart row).
//
// In both cases the chart that gets upgraded is the original row, so
// the SQL self-joins user_applications onto itself: the inner alias
// ("matched") locates the row whose app_name == appName in (user,
// source); the outer alias ("orig") follows app_raw_id back to the
// row with app_id = app_raw_id, which is the chart row the API
// handler actually consumes (manifest_type, rendered_package,
// image_analysis, etc.).
//
// Both sides require render_status='success' — the matched row must
// be a live install/clone and the original row must hold a usable
// rendered chart.
//
// version is matched against the original row's metadata->>'version'
// (NOT the matched row's), because the chart is the original's and
// its version is the canonical one. Empty version is rejected: every
// upgradeApp request carries a version on the wire and downstream
// chart_path construction needs it.
//
// Returns (row, matchedAppName, matchedAppID, nil). matchedAppName /
// matchedAppID are the user_applications.app_name / app_id of the
// inner alias — i.e. the original app's identity when the client
// passed the original, or the clone alias's identity when the client
// passed a clone. Callers:
//
//   - compare matchedAppName to row.AppName to decide whether the
//     request was a clone upgrade (matchedAppName != row.AppName) and
//     surface rawAppName in task metadata accordingly;
//   - put matchedAppID into task metadata as realAppID so the
//     downstream upsertPendingStateInTx anchors on the SAME
//     user_applications row the user clicked on (not the original
//     chart row, which lives at row.AppID). For non-clone upgrades
//     matchedAppID == row.AppID so callers can use it unconditionally.
//
// Returns (nil, "", "", nil) when no matching pair is found; the
// caller should respond 404.
func GetAppUpgradeRow(ctx context.Context, userID, sourceID, appName, version string) (*AppInstallRow, string, string, error) {
	userID = strings.TrimSpace(userID)
	sourceID = strings.TrimSpace(sourceID)
	appName = strings.TrimSpace(appName)
	version = strings.TrimSpace(version)
	if userID == "" || sourceID == "" || appName == "" || version == "" {
		return nil, "", "", fmt.Errorf("GetAppUpgradeRow: empty userID/sourceID/appName/version")
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, "", "", fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	const query = `
SELECT orig.source_id,
       orig.app_id,
       orig.app_raw_id,
       orig.app_name,
       orig.app_raw_name,
       orig.manifest_type,
       COALESCE(orig.metadata->>'version', '') AS app_version,
       orig.rendered_package,
       orig.price,
       orig.image_analysis,
       matched.app_name                        AS matched_app_name,
       matched.app_id                          AS matched_app_id
FROM user_applications matched
JOIN user_applications orig
        ON orig.user_id    = matched.user_id
       AND orig.source_id  = matched.source_id
       AND orig.app_raw_id = matched.app_raw_id
       AND orig.app_id     = orig.app_raw_id
WHERE matched.user_id        = ?
  AND matched.source_id      = ?
  AND matched.app_name       = ?
  AND matched.render_status  = 'success'
  AND orig.render_status     = 'success'
  AND orig.metadata->>'version' = ?
ORDER BY orig.updated_at DESC
LIMIT 1
`

	type row struct {
		SourceID        string                                   `gorm:"column:source_id"`
		AppID           string                                   `gorm:"column:app_id"`
		AppRawID        string                                   `gorm:"column:app_raw_id"`
		AppName         string                                   `gorm:"column:app_name"`
		AppRawName      string                                   `gorm:"column:app_raw_name"`
		ManifestType    string                                   `gorm:"column:manifest_type"`
		AppVersion      string                                   `gorm:"column:app_version"`
		RenderedPackage string                                   `gorm:"column:rendered_package"`
		Price           *models.JSONB[types.PriceConfig]         `gorm:"column:price"`
		ImageAnalysis   *models.JSONB[types.ImageAnalysisResult] `gorm:"column:image_analysis"`
		MatchedAppName  string                                   `gorm:"column:matched_app_name"`
		MatchedAppID    string                                   `gorm:"column:matched_app_id"`
	}

	var rows []*row
	if err := gdb.WithContext(ctx).Raw(query, userID, sourceID, appName, version).Scan(&rows).Error; err != nil {
		return nil, "", "", fmt.Errorf("get app upgrade row (user=%s source=%s app=%s version=%s): %w",
			userID, sourceID, appName, version, err)
	}
	if len(rows) == 0 {
		return nil, "", "", nil
	}
	r := rows[0]
	return &AppInstallRow{
		SourceID:        r.SourceID,
		AppID:           r.AppID,
		AppRawID:        r.AppRawID,
		AppName:         r.AppName,
		AppRawName:      r.AppRawName,
		ManifestType:    r.ManifestType,
		AppVersion:      r.AppVersion,
		RenderedPackage: r.RenderedPackage,
		Price:           r.Price,
		ImageAnalysis:   r.ImageAnalysis,
	}, r.MatchedAppName, r.MatchedAppID, nil
}

// AppLocatorRow is the projection LookupAppLocator yields. It carries
// the minimum fields cancelInstall / uninstallApp need to pivot from
// an app_name (which is what the wire contract gives them) to the
// (source_id, app_id) pair plus the manifest_type the executor uses
// as cfgType.
type AppLocatorRow struct {
	SourceID     string `gorm:"column:source_id"`
	AppID        string `gorm:"column:app_id"`
	AppRawID     string `gorm:"column:app_raw_id"`
	AppRawName   string `gorm:"column:app_raw_name"`
	ManifestType string `gorm:"column:manifest_type"`
}

// LookupAppLocator finds a single user_applications row by user_id and
// app_name across all sources. Used by cancelInstall and uninstallApp,
// which receive only the app_name on the wire and need to discover
// which source the row lives in plus the manifest_type that goes into
// the cancel / uninstall task metadata.
//
// The match is OR'd against ua.app_name and ua.app_raw_name so a
// clone-suffixed name resolves to the clone row directly, and a bare
// raw-name request still resolves through to the underlying row when
// only the original is present. This mirrors the cache-side fallback
// where datawatcher_state matches against either Status.Name or
// Status.RawAppName.
//
// When the same name shows up in multiple sources (e.g. an app
// installed from both a remote and a local source), the most recently
// rendered row wins via ORDER BY updated_at DESC — matching the
// cache-side "first hit wins" semantics where map iteration order
// was already non-deterministic.
//
// Returns (nil, nil) when no row matches; the caller surfaces the
// not-found case with whatever cfgType default is appropriate (usually
// "app").
func LookupAppLocator(ctx context.Context, userID, appName string) (*AppLocatorRow, error) {
	userID = strings.TrimSpace(userID)
	appName = strings.TrimSpace(appName)
	if userID == "" || appName == "" {
		return nil, fmt.Errorf("LookupAppLocator: empty userID/appName")
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	const query = `
SELECT ua.source_id,
       ua.app_id,
       ua.app_raw_id,
       ua.app_raw_name,
       ua.manifest_type
FROM user_applications ua
WHERE ua.user_id       = ?
  AND (ua.app_name     = ? OR ua.app_raw_name = ?)
  AND ua.render_status = 'success'
ORDER BY ua.updated_at DESC
LIMIT 1
`

	var rows []*AppLocatorRow
	if err := gdb.WithContext(ctx).Raw(query, userID, appName, appName).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("lookup app locator (user=%s app=%s): %w", userID, appName, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// LookupInstalledApp finds the user_applications row for an app that
// has actually been installed for this user, scoped by app_name AND
// installed_version. The JOIN against user_application_states serves
// two purposes:
//
//   - Existence proof: the state row's presence means an install task
//     has run (upsertPendingStateInTx writes the row at install task
//     creation) — no state row, no install. Returning nil here lets
//     uninstall surface a clean 404 instead of dispatching against
//     nothing.
//
//   - Version-anchored disambiguation: install writes the chart version
//     into installed_version at task creation. Filtering uninstall on
//     (app_name, installed_version) locks onto the SAME row install
//     populated, eliminating the multi-source / clone-vs-original
//     ambiguity that the older LookupAppLocator (no source filter, OR
//     across app_name/app_raw_name, no version) was prone to. The
//     downstream upsertPendingStateInTx then ON CONFLICTs cleanly on
//     the existing user_application_id rather than INSERTing a sibling
//     state row.
//
// app_name is matched exactly (NOT app_name OR app_raw_name): clones
// have their own app_name on the wire (rawAppName+hash) and write
// their own state row; uninstalling a clone passes the clone's wire
// name and naturally lands on its own row. Falling back to
// app_raw_name would re-introduce the original-vs-clone collision
// LookupAppLocator suffered from.
//
// render_status='success' filters out the failure / pending-render
// placeholder rows, matching every other action-side helper in this
// file.
//
// Returns (nil, nil) when no row matches; the caller surfaces 404.
// Empty userID / appName / version are caller bugs and return an
// error rather than a silent miss.
func LookupInstalledApp(ctx context.Context, userID, appName, version string) (*AppLocatorRow, error) {
	userID = strings.TrimSpace(userID)
	appName = strings.TrimSpace(appName)
	version = strings.TrimSpace(version)
	if userID == "" || appName == "" || version == "" {
		return nil, fmt.Errorf("LookupInstalledApp: empty userID/appName/version")
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	const query = `
SELECT ua.source_id,
       ua.app_id,
       ua.app_raw_id,
       ua.app_raw_name,
       ua.manifest_type
FROM user_applications ua
JOIN user_application_states uas
       ON uas.user_application_id = ua.id
WHERE ua.user_id            = ?
  AND ua.app_name           = ?
  AND uas.installed_version = ?
  AND ua.render_status      = 'success'
ORDER BY ua.updated_at DESC
LIMIT 1
`

	var rows []*AppLocatorRow
	if err := gdb.WithContext(ctx).Raw(query, userID, appName, version).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("lookup installed app (user=%s app=%s version=%s): %w",
			userID, appName, version, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// GetAppPaymentInfo composes a *types.AppInfo straight from PG for the
// six payment endpoints (getAppPaymentStatus / getAppPaymentStatusLegacy
// / purchaseApp / restorePurchase / startPaymentPolling /
// startFrontendPayment). The wire contract those handlers honour passes
// AppInfo to the paymentnew package, so doing the assembly here once
// removes per-handler boilerplate and keeps the JSONB unwrapping in a
// single place.
//
// AppEntry is sourced from applications (catalogue-side, identical
// across users); Price / PurchaseInfo / ImageAnalysis are sourced from
// the per-user user_applications row. nil JSONB columns leave the
// corresponding *types.AppInfo field nil — caller code must keep its
// existing nil guards (the cache-side build path also produced nil for
// these fields when chart-repo / payment had not populated them).
//
// sourceID="" widens the match across all sources, mirroring
// getAppPaymentStatusLegacy's behaviour where the route does not carry
// a source path parameter. appNameOrID is matched against both
// ua.app_name and ua.app_id so the helper handles the cache-side
// MatchAppID fallback chain (ID > AppID > Name) without forcing
// callers to disambiguate. Returns (nil, nil) when no row matches.
func GetAppPaymentInfo(ctx context.Context, userID, sourceID, appNameOrID string) (*types.AppInfo, error) {
	userID = strings.TrimSpace(userID)
	sourceID = strings.TrimSpace(sourceID)
	appNameOrID = strings.TrimSpace(appNameOrID)
	if userID == "" || appNameOrID == "" {
		return nil, fmt.Errorf("GetAppPaymentInfo: empty userID/appNameOrID")
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before user application store usage")
	}

	type row struct {
		Price         *models.JSONB[types.PriceConfig]          `gorm:"column:price"`
		PurchaseInfo  *models.JSONB[types.PurchaseInfo]         `gorm:"column:purchase_info"`
		ImageAnalysis *models.JSONB[types.ImageAnalysisResult]  `gorm:"column:image_analysis"`
		AppEntry      *models.JSONB[types.ApplicationInfoEntry] `gorm:"column:app_entry"`
	}

	const query = `
SELECT ua.price,
       ua.purchase_info,
       ua.image_analysis,
       a.app_entry
FROM user_applications ua
LEFT JOIN applications a
    ON a.source_id = ua.source_id AND a.app_id = ua.app_id
WHERE ua.user_id       = ?
  AND (? = '' OR ua.source_id = ?)
  AND (ua.app_name     = ? OR ua.app_id = ?)
  AND ua.render_status = 'success'
ORDER BY ua.updated_at DESC
LIMIT 1
`

	var rows []*row
	if err := gdb.WithContext(ctx).Raw(query, userID, sourceID, sourceID, appNameOrID, appNameOrID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("get app payment info (user=%s source=%s app=%s): %w",
			userID, sourceID, appNameOrID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	r := rows[0]
	info := &types.AppInfo{}
	if r.AppEntry != nil {
		entry := r.AppEntry.Data
		info.AppEntry = &entry
	}
	if r.Price != nil {
		price := r.Price.Data
		info.Price = &price
	}
	if r.PurchaseInfo != nil {
		pi := r.PurchaseInfo.Data
		info.PurchaseInfo = &pi
	}
	if r.ImageAnalysis != nil {
		ia := r.ImageAnalysis.Data
		info.ImageAnalysis = &ia
	}
	return info, nil
}

// ToWireData hydrates the AppStateLatestData wire shape from a
// user_application_states JOIN user_applications row. Three time
// fields (UpdateTime / StatusTime / LastTransitionTime) all collapse
// onto event_create_time because the schema only stores a single
// monotonic timestamp; if a future product need distinguishes them,
// add columns. Tailscale and Settings are not stored, so they are
// returned as empty / nil — the cache-side path also produced these
// values from the upstream NATS message verbatim.
//
// userID and sourceID are passed in because the Status block carries
// them as Owner / Source even though they are not stored on the
// user_application_states row itself.
//
// Two call sites consume this projection: the /market/state* API
// handlers (pkg/v2/api/app.go) and the StateNotifier push hook
// (internal/v2/appinfo/state.go). Keeping the converter next to the
// row definition avoids a parallel implementation drifting from the
// API-side wire contract.
func (r *AppStateLatestRow) ToWireData(userID, sourceID string) *types.AppStateLatestData {
	if r == nil {
		return nil
	}

	var icon, title string
	if r.Metadata != nil {
		if s, ok := r.Metadata.Data["icon"].(string); ok {
			icon = s
		}
		title = helper.PickStringFromAny(r.Metadata.Data["title"])
	}

	timeStr := ""
	if !r.EventCreateTime.IsZero() {
		timeStr = r.EventCreateTime.UTC().Format(time.RFC3339)
	}

	spec := &types.AppStateLatestDataSpec{
		AppStateLatestDataSpecMetadata: types.AppStateLatestDataSpecMetadata{
			Name:               r.AppName,
			RawAppName:         r.AppRawName,
			AppID:              r.AppID,
			IsSysApp:           r.IsSysApp,
			Owner:              userID,
			Icon:               icon,
			Source:             sourceID,
			Title:              title,
			State:              r.State,
			UpdateTime:         timeStr,
			StatusTime:         timeStr,
			LastTransitionTime: timeStr,
			Progress:           r.Progress,
			OpType:             r.OpType,
			Message:            r.Message,
			Reason:             r.Reason,
		},
		Tailscale: map[string]interface{}{},
	}
	if r.StatusEntrances != nil {
		spec.EntranceStatuses = r.StatusEntrances.Data
	}
	if r.SharedEntrances != nil {
		spec.SharedEntrances = r.SharedEntrances.Data
	}

	return &types.AppStateLatestData{
		Type:    types.AppStateLatest,
		Version: r.InstalledVersion,
		Status:  spec,
	}
}

// GetUserAppStateLatest reads a single user_application_states JOIN
// user_applications row identified by (user, source, app_name,
// app_raw_name). Mirrors ListUserAppStateLatest's projection so the
// row can flow into the same wire converter (ToWireData).
//
// AppName and AppRawName are matched via AND (not OR) for the same
// reason UpsertStateFromNATS uses AND — a user may have both an
// original app and one of its clones installed; the (name, rawName)
// pair is the only catalog-independent way to disambiguate. Both
// must equal the values carried by the originating NATS message.
//
// Returns (nil, nil) when no row matches; callers (the StateNotifier
// push hook) are expected to skip silently in that case — the parent
// user_applications row is missing or the state row was never
// written, both legitimate "no push" conditions.
func GetUserAppStateLatest(ctx context.Context, userID, sourceID, appName, appRawName string) (*AppStateLatestRow, error) {
	userID = strings.TrimSpace(userID)
	sourceID = strings.TrimSpace(sourceID)
	appName = strings.TrimSpace(appName)
	appRawName = strings.TrimSpace(appRawName)
	if userID == "" || sourceID == "" || appName == "" || appRawName == "" {
		return nil, fmt.Errorf("GetUserAppStateLatest: empty userID/sourceID/appName/appRawName")
	}

	gdb := db.Global()
	if gdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before user application state store usage")
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
		Where("ua.source_id = ?", sourceID).
		Where("ua.app_name = ?", appName).
		Where("ua.app_raw_name = ?", appRawName).
		Limit(1).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("get app_state_latest (user=%s source=%s app=%s rawApp=%s): %w",
			userID, sourceID, appName, appRawName, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}
