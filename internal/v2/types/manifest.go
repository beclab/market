package types

import (
	"encoding/json"
	"fmt"

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
	return &entry, nil
}
