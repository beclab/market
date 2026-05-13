package types

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beclab/Olares/framework/oac"
	"github.com/beclab/api/manifest"
)

// UserAppManifest is the in-memory representation of the JSONB columns on
// user_applications that together describe an OlaresManifest rendered for
// a single user. Each field corresponds 1:1 to a JSONB column; nil means
// "the source raw_data_ex did not declare this block" and is persisted
// as SQL NULL.
//
// Storage shape: map keys mirror chart-repo's wire form (oac.AppConfiguration's
// json tags), so the JSONB columns faithfully reflect the typed payload
// chart-repo emits and direct PG readers / debug SQL see the same key
// shape that appears in OlaresManifest.yaml. Translation to the
// ApplicationInfoEntry API contract (e.g. requiredCpu → requiredCPU) is
// the sole responsibility of ComposeApplicationInfoEntry.
type UserAppManifest struct {
	// Metadata mirrors the OlaresManifest "metadata" block (name / icon /
	// title / description / categories / appid / version / rating /
	// target / type) populated from cfg.Metadata.
	Metadata        map[string]any
	Spec            map[string]any
	Resources       map[string]any
	Options         map[string]any
	Entrances       []map[string]any
	SharedEntrances []map[string]any
	Ports           []map[string]any
	Tailscale       map[string]any
	Permission      map[string]any
	Middleware      map[string]any
	Envs            []map[string]any
}

// resourceFieldKeys are the json tag names of the 8 resource cap fields on
// oac.AppSpec. They live as nested keys under cfg.Spec but are persisted
// in the dedicated user_applications.resources column, so buildSpec
// removes them from the spec catch-all.
var resourceFieldKeys = []string{
	"requiredMemory",
	"requiredDisk",
	"requiredCpu",
	"requiredGpu",
	"limitedMemory",
	"limitedDisk",
	"limitedCpu",
	"limitedGpu",
}

// BuildUserAppManifest splits a chart-repo raw_data_ex payload (typed
// *oac.AppConfiguration) into the JSONB-column shapes that
// user_applications expects. Each output column receives a verbatim map
// projection of the corresponding typed field, so the database mirrors
// chart-repo's wire shape one-to-one.
//
// Persistence policy: zero-valued resource caps (empty strings) are
// dropped from m.Resources to keep the column clean — equivalent to
// ApplicationInfoEntry's `omitempty` on the same fields. All other
// blocks are passed through verbatim from json.Marshal of the typed
// field, including empty containers (TailScale{} → empty map), so a
// direct PG reader can distinguish "chart-repo returned this block
// explicitly empty" from "chart-repo did not return this block at all"
// (= nil → SQL NULL).
//
// API consumers go through ComposeApplicationInfoEntry, which is where
// the wire-key → ApplicationInfoEntry-tag translations (resource caps
// casing, single-locale strings → multi-language map) happen.
func BuildUserAppManifest(cfg *oac.AppConfiguration) *UserAppManifest {
	if cfg == nil {
		return &UserAppManifest{}
	}

	m := &UserAppManifest{
		Metadata:        marshalToMap(cfg.Metadata),
		Resources:       buildResources(cfg.Spec),
		Spec:            buildSpec(cfg.Spec, cfg.Provider),
		Options:         marshalToMap(cfg.Options),
		Tailscale:       marshalToMap(cfg.TailScale),
		Permission:      marshalToMap(cfg.Permission),
		Entrances:       marshalSliceToMaps(cfg.Entrances),
		SharedEntrances: marshalSliceToMaps(cfg.SharedEntrances),
		Ports:           marshalSliceToMaps(cfg.Ports),
		Envs:            marshalSliceToMaps(cfg.Envs),
	}
	if cfg.Middleware != nil {
		m.Middleware = marshalToMap(*cfg.Middleware)
	}

	return m
}

// EnrichEntranceURLs fills in `url` on each entrance / shared-entrance map
// of m, mirroring the URL scheme app-service applies at install time:
//
//   - 1 entrance:   "<appID>.<zone>"
//   - N entrances:  "<appID><i>.<zone>"          (i = 0-based index)
//   - shared (any): "<sharedPrefix><i>.<sharedZone>[:port]"
//                   sharedPrefix = first 8 hex chars of md5(appID + "shared")
//                   sharedZone   = zone with the first DNS label rewritten to "shared"
//                   :<port>     appended only when the entrance carries port > 0
//
// appID is the user_applications.app_id of the row being rendered (for
// clones this is md5(newAppName), not the original chart's app_id), so
// every install / clone produces its own URL space — the same convention
// app-service follows. Empty appID or zone makes the function a no-op:
// the caller is in a state where it cannot deterministically derive a
// URL and writing one anyway would mismatch what app-service later
// computes from authoritative inputs.
//
// Mutates m in place. The defaultThirdLevelDomainConfig override
// path that app-service consults is intentionally NOT replicated here:
// the override is per-(app, entrance) and lives in app-service's own
// settings; until we have a parallel surface for it on Market's side,
// rewriting URLs Market doesn't know about would silently shadow the
// app-service-computed value.
func EnrichEntranceURLs(m *UserAppManifest, appID, zone string) {
	if m == nil || appID == "" || zone == "" {
		return
	}

	switch len(m.Entrances) {
	case 0:
		// nothing to do
	case 1:
		m.Entrances[0]["url"] = fmt.Sprintf("%s.%s", appID, zone)
	default:
		for i, e := range m.Entrances {
			e["url"] = fmt.Sprintf("%s%d.%s", appID, i, zone)
		}
	}

	if len(m.SharedEntrances) > 0 {
		sharedZone := rewriteFirstLabel(zone, "shared")
		prefix := sharedEntrancePrefix(appID)
		for i, e := range m.SharedEntrances {
			port := mapToInt(e["port"])
			if port > 0 {
				e["url"] = fmt.Sprintf("%s%d.%s:%d", prefix, i, sharedZone, port)
			} else {
				e["url"] = fmt.Sprintf("%s%d.%s", prefix, i, sharedZone)
			}
		}
	}
}

// sharedEntrancePrefix mirrors app-service's AppName.SharedEntranceIdPrefix:
// first 8 hex characters of md5(appID + "shared"). The appID is taken
// verbatim because in our context AppName.GetAppID() resolves to
// user_applications.app_id (clone rows carry their own app_id), so
// callers pass that directly rather than re-running the AppName→AppID
// derivation.
func sharedEntrancePrefix(appID string) string {
	sum := md5.Sum([]byte(appID + "shared"))
	return hex.EncodeToString(sum[:])[:8]
}

// rewriteFirstLabel returns zone with its first dot-separated label
// replaced by replacement. "alice.olares.com" + "shared" -> "shared.olares.com".
// Empty input returns empty; a single-label input is replaced wholesale,
// matching strings.Split / strings.Join behaviour.
func rewriteFirstLabel(zone, replacement string) string {
	if zone == "" {
		return ""
	}
	tokens := strings.Split(zone, ".")
	tokens[0] = replacement
	return strings.Join(tokens, ".")
}

// mapToInt extracts an integer from a JSONB-decoded map value. The
// manifest's entrance maps come from a json.Marshal/Unmarshal round-trip
// (see marshalSliceToMaps), so numeric fields surface as float64
// regardless of their original Go type. We tolerate native ints for
// callers that may construct the map directly in tests.
func mapToInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case float32:
		return int(n)
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}

// buildResources extracts the 8 typed resource cap fields from cfg.Spec
// into a flat map keyed by oac json tags (requiredCpu / requiredGpu —
// lowercase pu/pu, matching oac.AppSpec.RequiredCPU's json:"requiredCpu"
// tag and chart-repo's wire). The casing translation to
// ApplicationInfoEntry's requiredCPU / requiredGPU happens later in
// ComposeApplicationInfoEntry via resourceKeyAliases.
//
// Empty strings are skipped: oac.AppSpec carries these as bare string
// fields with no omitempty / pointer-nil distinction, so an empty value
// here cannot be told apart from "field not present" anyway.
func buildResources(spec manifest.AppSpec) map[string]any {
	out := map[string]any{}
	if spec.RequiredMemory != "" {
		out["requiredMemory"] = spec.RequiredMemory
	}
	if spec.RequiredDisk != "" {
		out["requiredDisk"] = spec.RequiredDisk
	}
	if spec.RequiredCPU != "" {
		out["requiredCpu"] = spec.RequiredCPU
	}
	if spec.RequiredGPU != "" {
		out["requiredGpu"] = spec.RequiredGPU
	}
	if spec.LimitedMemory != "" {
		out["limitedMemory"] = spec.LimitedMemory
	}
	if spec.LimitedDisk != "" {
		out["limitedDisk"] = spec.LimitedDisk
	}
	if spec.LimitedCPU != "" {
		out["limitedCpu"] = spec.LimitedCPU
	}
	if spec.LimitedGPU != "" {
		out["limitedGpu"] = spec.LimitedGPU
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildSpec marshals AppSpec into a map and strips the 8 resource keys
// (which already live in m.Resources). Top-level providers — which have
// no dedicated column — are folded into spec under "provider" so they
// flow through to the API via the catch-all path.
func buildSpec(spec manifest.AppSpec, providers []manifest.Provider) map[string]any {
	out := marshalToMap(spec)
	if out == nil {
		out = map[string]any{}
	}
	for _, k := range resourceFieldKeys {
		delete(out, k)
	}
	if len(providers) > 0 {
		if pv := marshalSliceToMaps(providers); len(pv) > 0 {
			out["provider"] = pv
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// marshalToMap converts any value to a map[string]any via json round-trip.
// Returns nil on marshal failure or when the value serialises to JSON
// null (e.g. zero-valued nullable struct fields).
func marshalToMap(v any) map[string]any {
	buf, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	if string(buf) == "null" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(buf, &out); err != nil {
		return nil
	}
	return out
}

// marshalSliceToMaps converts a typed slice to []map[string]any via json
// round-trip. Returns nil for nil/empty slices so that the JSONB column
// is written as SQL NULL when chart-repo did not return the array.
func marshalSliceToMaps[T any](vs []T) []map[string]any {
	if len(vs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(vs))
	for _, v := range vs {
		mm := marshalToMap(v)
		if mm == nil {
			mm = map[string]any{}
		}
		out = append(out, mm)
	}
	return out
}

// resourceKeyAliases maps oac.AppSpec's json-tag style ("requiredCpu",
// "requiredGpu", "limitedCpu", "limitedGpu") used inside m.Resources and
// stored verbatim in user_applications.resources, to the legacy
// ApplicationInfoEntry json-tag style ("requiredCPU", "requiredGPU",
// "limitedCPU", "limitedGPU") that the NATS / API contract still uses.
//
// Long-term the two sides should converge on a single casing (oac's,
// most likely, since it matches OlaresManifest.yaml); until then this
// is the single translation point and lives in Compose so storage can
// faithfully mirror chart-repo's wire shape.
var resourceKeyAliases = map[string]string{
	"requiredCpu": "requiredCPU",
	"requiredGpu": "requiredGPU",
	"limitedCpu":  "limitedCPU",
	"limitedGpu":  "limitedGPU",
}

// multiLangStringKeys are merged-map keys that ApplicationInfoEntry types
// as map[string]string but oac.AppMetaData / oac.AppSpec carry as plain
// strings (no localisation in OlaresManifest.yaml). Without wrapping,
// json.Unmarshal in Compose would fail with "cannot unmarshal string
// into Go struct field of type map[string]string" and notifyRenderSuccess
// would silently fall back to the catalog entry on every push.
//
// We wrap into a single "en-US" locale so the merged JSON deserialises
// cleanly. This is a structural pass-through, NOT a localisation claim:
// real i18n strings come from a separate path (chart-repo's i18n bundle
// applied earlier in the pipeline), and any locale info chart-repo
// already supplied via that path will overwrite this placeholder.
var multiLangStringKeys = []string{
	"title",
	"description",
	"fullDescription",
	"upgradeDescription",
}

// EntryScalars carries the identity / header scalars that
// user_applications stores in dedicated scalar columns (and that
// raw_data_ex's typed shape either omits or names differently than the
// API contract expects). Without re-injection,
// ComposeApplicationInfoEntry would emit an *ApplicationInfoEntry with
// empty ID / AppID / Version / CfgType / ApiVersion since:
//   - id and appID are not present on *oac.AppConfiguration at all
//     (the catalog row's app_id is the source of truth);
//   - version arrives only nested under metadata.version, not at the
//     top level the API contract expects;
//   - cfgType arrives as olaresManifest.type (dotted key), not cfgType.
//
// Callers re-inject EntryScalars from their own scalar sources:
//   - hydration NATS push: from HydrationTask (task.AppID, task.AppVersion,
//     task.AppType, task.AppEntry.ApiVersion)
//   - future API path that reads user_applications: from the
//     UserApplication row's scalar columns (app_id, manifest_version,
//     manifest_type, api_version)
type EntryScalars struct {
	ID         string
	AppID      string
	Version    string
	CfgType    string
	ApiVersion string

	// I18n is the pre-shaped multi-language bundle the API contract
	// expects on ApplicationInfoEntry (locale-major + nested by
	// manifest block). API callers should run NestI18nForEntry on the
	// flat field-major bundle stored in user_applications.i18n before
	// passing it here. Hydration callers leave it nil; the persisted
	// app_entry payload is unaffected.
	I18n map[string]any

	// VersionHistory mirrors the user_applications.version_history
	// JSONB column. API callers pass it through verbatim so the
	// composed ApplicationInfoEntry.VersionHistory carries the
	// chart-repo-emitted changelog. nil leaves the field unset.
	VersionHistory []*VersionInfo
}

// ComposeApplicationInfoEntry is the inverse of BuildUserAppManifest. It
// merges the JSONB column payloads into a single map, translates
// storage-shape keys into ApplicationInfoEntry's json-tag shape, decodes
// the result into *ApplicationInfoEntry, and re-injects the identity
// scalars from `scalars`. Fields not declared on ApplicationInfoEntry
// are silently dropped; the struct is the API contract, not the storage
// contract.
//
// Translation steps applied here (and ONLY here):
//  1. resourceKeyAliases: requiredCpu/Gpu, limitedCpu/Gpu (oac casing)
//     → requiredCPU/GPU, limitedCPU/GPU (entry casing).
//  2. multiLangStringKeys: title / description / fullDescription /
//     upgradeDescription (string in oac) → map{"en-US": value} so
//     ApplicationInfoEntry's map[string]string fields deserialise.
//
// Round-trip property: BuildUserAppManifest(cfg) →
// ComposeApplicationInfoEntry(manifest, scalarsFromCfg) preserves every
// ApplicationInfoEntry-known field for which the typed → API mapping is
// straightforward. Multi-language strings degrade to a single en-US
// locale (real localisation is a separate concern).
func ComposeApplicationInfoEntry(m *UserAppManifest, scalars EntryScalars) (*ApplicationInfoEntry, error) {
	if m == nil {
		return nil, nil
	}

	merged := make(map[string]any)
	// Metadata, Resources, Spec all flatten into the same target struct
	// (ApplicationInfoEntry); their keys are disjoint by design (see
	// resourceFieldKeys removal in buildSpec, and the OlaresManifest
	// metadata vs spec split), so merging them in any order produces the
	// same result. Listing Spec last keeps "spec wins on accidental
	// overlap" as a defensive last-resort tiebreaker.
	for _, src := range []map[string]any{m.Metadata, m.Resources, m.Spec} {
		for k, v := range src {
			merged[k] = v
		}
	}
	if m.Options != nil {
		merged["options"] = m.Options
	}
	if m.Tailscale != nil {
		merged["tailscale"] = m.Tailscale
	}
	if m.Permission != nil {
		merged["permission"] = m.Permission
	}
	if m.Middleware != nil {
		merged["middleware"] = m.Middleware
	}
	if m.Entrances != nil {
		merged["entrances"] = m.Entrances
	}
	if m.SharedEntrances != nil {
		merged["sharedEntrances"] = m.SharedEntrances
	}
	if m.Ports != nil {
		merged["ports"] = m.Ports
	}
	if m.Envs != nil {
		merged["envs"] = m.Envs
	}

	for from, to := range resourceKeyAliases {
		if v, ok := merged[from]; ok {
			merged[to] = v
			delete(merged, from)
		}
	}
	for _, k := range multiLangStringKeys {
		if s, ok := merged[k].(string); ok {
			merged[k] = map[string]string{"en-US": s}
		}
	}

	buf, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("marshal merged manifest: %w", err)
	}
	var entry ApplicationInfoEntry
	if err := json.Unmarshal(buf, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal merged manifest into ApplicationInfoEntry: %w", err)
	}
	entry.ID = scalars.ID
	entry.AppID = scalars.AppID
	entry.Version = scalars.Version
	entry.CfgType = scalars.CfgType
	entry.ApiVersion = scalars.ApiVersion
	if scalars.I18n != nil {
		entry.I18n = scalars.I18n
	}
	if len(scalars.VersionHistory) > 0 {
		entry.VersionHistory = scalars.VersionHistory
	}
	return &entry, nil
}

// i18nFieldToBlock pins which manifest block each localisable field
// belongs to. Drives NestI18nForEntry's locale-major + nested output
// shape for the API contract; chart-repo's i18n top-level field is
// stored field-major and this map is the inverse routing table.
//
// Add an entry here when chart-repo introduces a new localisable
// field. Fields not listed here are ignored by NestI18nForEntry,
// which keeps the output stable when storage shape and API contract
// drift apart.
var i18nFieldToBlock = map[string]string{
	"title":              "metadata",
	"description":        "metadata",
	"fullDescription":    "spec",
	"upgradeDescription": "spec",
}

// NestI18nForEntry transforms chart-repo's field-major i18n bundle
// (as stored in user_applications.i18n) into the locale-major +
// manifest-block-nested shape the /api/v2/apps wire contract expects
// for ApplicationInfoEntry.I18n:
//
//	field-major (storage):
//	  { "title":       {"en-US": "...", "zh-CN": "..."},
//	    "description": {"en-US": "...", "zh-CN": "..."},
//	    "fullDescription":    {"en-US": "...", "zh-CN": "..."},
//	    "upgradeDescription": {"en-US": "...", "zh-CN": "..."} }
//
//	locale-major + nested (wire):
//	  { "en-US": { "metadata": {"title": "...", "description": "..."},
//	               "spec":     {"fullDescription": "...", "upgradeDescription": "..."},
//	               "entrances": null },
//	    "zh-CN": {...} }
//
// Empty values are dropped; locales that end up with no fields after
// filtering are still present in the output (with all blocks empty
// objects) only when at least one of their fields was carried; locales
// that had zero non-empty values across every recognised field are
// dropped entirely. The "entrances" key is emitted with a nil value
// per locale to match the wire contract; per-entrance localisation
// is not yet supported.
//
// Returns nil when the input is empty or no recognised fields are
// present.
func NestI18nForEntry(i18n map[string]map[string]string) map[string]any {
	if len(i18n) == 0 {
		return nil
	}
	// locale -> block -> field -> value
	staged := map[string]map[string]map[string]string{}
	for field, localeMap := range i18n {
		block, ok := i18nFieldToBlock[field]
		if !ok {
			continue
		}
		for locale, value := range localeMap {
			if value == "" {
				continue
			}
			byBlock, ok := staged[locale]
			if !ok {
				byBlock = map[string]map[string]string{}
				staged[locale] = byBlock
			}
			byField, ok := byBlock[block]
			if !ok {
				byField = map[string]string{}
				byBlock[block] = byField
			}
			byField[field] = value
		}
	}
	if len(staged) == 0 {
		return nil
	}
	out := make(map[string]any, len(staged))
	for locale, blocks := range staged {
		slot := make(map[string]any, len(blocks)+1)
		for k, v := range blocks {
			slot[k] = v
		}
		slot["entrances"] = nil
		out[locale] = slot
	}
	return out
}
