package uploadevent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"market/internal/v2/chartrepo"
	"market/internal/v2/db/models"
	"market/internal/v2/store"
	"market/internal/v2/types"
)

// buildApplicationRow assembles the catalog-side `applications` row for
// an upload event. Field sources differ from the syncer-driven
// store.buildApplicationRow because the upload payload is the nested
// AppInfoLatest envelope (raw_data_ex / app_info / ...), not the flat
// chart-repo /repo/data appMap the syncer consumes.
//
// Field mapping (event-driven, this file only):
//
//   - source_id      ← event.Source (caller supplies)
//   - app_id         ← caller-supplied (the MD5-derived 8-char id)
//   - app_name       ← raw_data_ex.metadata.name (the OlaresManifest's
//                      authoritative app name); defaults to app_id when
//                      metadata.name is empty so the NOT NULL constraint
//                      is always satisfied.
//   - app_version    ← raw_data_ex.metadata.version, with the top-level
//                      payload "version" field as a fallback (chart-repo
//                      emits the same value at both spots — the dual
//                      lookup just prevents a parser hiccup from voiding
//                      the row).
//   - app_type       ← raw_data_ex.olaresManifest.type ("app" /
//                      "middleware" / ...), normalised through
//                      types.NormalizeAppType so the value matches the
//                      syncer-written rows. Falls back to metadata.type
//                      when ConfigType is empty (legacy manifests put
//                      "type" under metadata).
//   - app_entry      ← payload.app_info.app_entry, decoded into a typed
//                      *types.ApplicationInfoEntry. nil entry leaves the
//                      column NULL (matches syncer behaviour for apps
//                      whose chart-repo response omits app_entry).
//   - price          ← payload.app_info.price (chart-repo embeds the
//                      catalog-level price under app_info, alongside
//                      app_entry). nil leaves the column NULL.
//
// Returns nil when the resolved app_version is empty — caller treats
// this as a tolerable skip, mirroring buildApplicationRow's
// "missing version → silent drop" contract.
func buildApplicationRow(sourceID, appID string, payload *chartrepo.SyncAppPayload) *models.Application {
	if payload == nil || payload.RawDataEx == nil {
		return nil
	}
	meta := payload.RawDataEx.Metadata

	appVersion := strings.TrimSpace(meta.Version)
	if appVersion == "" {
		appVersion = strings.TrimSpace(payload.Version)
	}
	if appVersion == "" {
		return nil
	}

	appName := strings.TrimSpace(meta.Name)
	if appName == "" {
		appName = appID
	}

	cfgType := strings.TrimSpace(payload.RawDataEx.ConfigType)
	if cfgType == "" {
		cfgType = strings.TrimSpace(meta.Type)
	}

	row := &models.Application{
		SourceID:   sourceID,
		AppID:      appID,
		AppName:    appName,
		AppVersion: appVersion,
		AppType:    store.NormalizeAppType(cfgType),
	}

	if appInfoMap := payload.AppInfo; len(appInfoMap) > 0 {
		if entry := decodeAppEntry(appInfoMap["app_entry"]); entry != nil {
			j := models.NewJSONB(*entry)
			row.AppEntry = &j
		}
		if price := decodePrice(appInfoMap["price"]); price != nil {
			j := models.NewJSONB(*price)
			row.Price = &j
		}
	}

	return row
}

// upsertApplicationsRow writes the catalog-side row for the upload event.
// Returns the same error surface as store.UpsertApplicationRow so the
// orchestrator can decide whether to abort the event (genuine PG error)
// or continue (build returned nil → no row → no-op).
func upsertApplicationsRow(ctx context.Context, sourceID, appID string, payload *chartrepo.SyncAppPayload) error {
	row := buildApplicationRow(sourceID, appID, payload)
	if row == nil {
		return nil
	}
	if err := store.UpsertApplicationRow(ctx, row); err != nil {
		return fmt.Errorf("upsert applications (source=%s app=%s): %w", sourceID, appID, err)
	}
	return nil
}

// decodeAppEntry decodes the chart-repo-emitted app_info.app_entry
// payload into a *types.ApplicationInfoEntry. We accept three input
// shapes because chart-repo's wire form is intentionally loose:
//   - already-typed *types.ApplicationInfoEntry / types.ApplicationInfoEntry
//     (defensive — happens if a caller down the line strongly-types the
//     payload before forwarding);
//   - map[string]interface{} (the actual on-the-wire shape from /apps);
//   - nil / unrecognised type → nil (caller leaves column NULL).
//
// The map path goes through json.Marshal+Unmarshal so any json tags on
// ApplicationInfoEntry's fields apply, keeping the catalog row's
// app_entry shape identical to what NewApplicationInfoEntry would
// produce on the syncer path. This deliberately depends on chart-repo's
// app_info.app_entry being a json-compatible projection of the same
// types.ApplicationInfoEntry struct — a sane assumption today since
// chart-repo emits it from the same Go type.
func decodeAppEntry(raw interface{}) *types.ApplicationInfoEntry {
	switch v := raw.(type) {
	case nil:
		return nil
	case *types.ApplicationInfoEntry:
		return v
	case types.ApplicationInfoEntry:
		out := v
		return &out
	case map[string]interface{}:
		if len(v) == 0 {
			return nil
		}
		buf, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		out := &types.ApplicationInfoEntry{}
		if err := json.Unmarshal(buf, out); err != nil {
			return nil
		}
		return out
	default:
		return nil
	}
}

// decodePrice mirrors decodeAppEntry for the price block. The store
// package already exports a similar parser (parsePriceConfig), but it
// is package-private and only services the syncer path; duplicating
// the trivial logic here keeps the upload-event path self-contained
// and avoids exporting an internal helper just for one call site.
func decodePrice(raw interface{}) *types.PriceConfig {
	switch v := raw.(type) {
	case nil:
		return nil
	case *types.PriceConfig:
		return v
	case types.PriceConfig:
		out := v
		return &out
	case map[string]interface{}:
		if len(v) == 0 {
			return nil
		}
		buf, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		out := &types.PriceConfig{}
		if err := json.Unmarshal(buf, out); err != nil {
			return nil
		}
		return out
	default:
		return nil
	}
}
