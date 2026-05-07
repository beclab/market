package types

import (
	"encoding/json"
	"fmt"
)

// UserAppManifest is the in-memory representation of the JSONB columns on
// user_applications that together describe an OlaresManifest rendered for
// a single user. Each field corresponds 1:1 to a JSONB column; nil means
// "the source raw_data did not declare this block" and is persisted as
// SQL NULL.
//
// Field-key invariant: every map key follows chart-repo's camelCase JSON
// tag convention (e.g. "requiredMemory", not "required_memory") so that
// ComposeApplicationInfoEntry can json.Unmarshal a merged view back into
// *ApplicationInfoEntry losslessly.
type UserAppManifest struct {
	// Metadata mirrors the OlaresManifest "metadata" block (name / icon /
	// title / description / categories / appid / version / rating /
	// target / type) when chart-repo's /dcr/sync-app returns the
	// structured raw_data_ex payload. While the legacy flat raw_data is
	// still in use this stays nil and the equivalent fields fall through
	// to Spec via the catch-all in BuildUserAppManifest.
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

// resourcesKeys lists the raw_data keys that go into user_applications.resources.
// All required* / limited* compute caps live here. Compatibility hints
// (supportArch, supportClient) intentionally fall through to spec via
// catch-all so that resources stays a pure resource-budget block.
var resourcesKeys = []string{
	"requiredMemory",
	"requiredDisk",
	"requiredCPU",
	"requiredGPU",
	"limitedMemory",
	"limitedDisk",
	"limitedCPU",
	"limitedGPU",
}

// passThroughObjectKeys are top-level raw_data keys whose value is a JSON
// object and is stored verbatim into a same-named JSONB column.
var passThroughObjectKeys = []string{
	"options",
	"tailscale",
	"permission",
	"middleware",
}

// passThroughArrayKeys are top-level raw_data keys whose value is a JSON
// array of objects and is stored verbatim into a same-named JSONB column.
// Note: "sharedEntrances" mirrors raw_data's camelCase key naming.
var passThroughArrayKeys = []string{
	"entrances",
	"sharedEntrances",
	"ports",
	"envs",
}

// scalarKeys are raw_data keys already carried by dedicated scalar columns
// on user_applications. They are excluded from spec to avoid duplication.
//
// Note: even though manifest_version / api_version are scalar columns, the
// app's logical version still needs to be visible inside spec (callers
// query user_applications.spec.version directly). Trimming this list is
// tracked separately; for now we keep the historical set so that
// manifest_version's legacy semantics (set from applications.app_version
// via task.AppVersion) stay untouched while we land the spec/metadata
// fixes.
var scalarKeys = []string{
	"id",
	"appID",
	"version",
	"cfgType",
	"apiVersion",
}

// excludedKeys are top-level raw_data keys that must NOT land in spec on
// the legacy flat raw_data path. The literal "metadata" key here is
// chart-repo's per-render processing metadata (hydration_task_id /
// rendered_chart_url / validation_status / ...) — NOT the OlaresManifest
// metadata block, which arrives nested under "metadata" only in the new
// raw_data_ex payload and lands directly into the dedicated metadata
// JSONB column via a separate code path.
//
// This complements the json:"-" tag on ApplicationInfoEntry.Metadata,
// which only suppresses the field on the struct → JSON path; raw_data
// arrives as a map[string]any so the tag does not apply, and without
// this list "metadata" would leak into spec via catch-all and pollute
// it with chart-repo internals.
var excludedKeys = []string{
	"metadata",
}

// BuildUserAppManifest splits a chart-repo raw_data payload into the
// JSONB-column shapes that user_applications expects. spec is a catch-all:
// every raw_data key not consumed by another column, not in scalarKeys,
// and not in excludedKeys lands there — so chart-repo schema additions
// flow through without code changes.
//
// The caller is expected to have already applied any role-based filtering
// (e.g. stripping admin-only fields when writing for a regular user) on
// rawData before calling this function. BuildUserAppManifest itself is a
// pure transformation with no role awareness.
//
// Persistence policy: every key chart-repo returns is preserved verbatim,
// including zero values (empty string, 0, false, nil, empty slice, empty
// map). This way user_applications.spec faithfully reflects the raw_data
// shape, which is what direct PG readers (debug tools, ops SQL) rely on.
// API consumers go through ComposeApplicationInfoEntry which round-trips
// these zero values losslessly through the ApplicationInfoEntry struct.
func BuildUserAppManifest(rawData map[string]any) *UserAppManifest {
	if rawData == nil {
		return &UserAppManifest{}
	}

	m := &UserAppManifest{
		Resources: extractFixedFields(rawData, resourcesKeys),
	}

	for _, k := range passThroughObjectKeys {
		v := passThroughObject(rawData, k)
		switch k {
		case "options":
			m.Options = v
		case "tailscale":
			m.Tailscale = v
		case "permission":
			m.Permission = v
		case "middleware":
			m.Middleware = v
		}
	}
	for _, k := range passThroughArrayKeys {
		v := passThroughArray(rawData, k)
		switch k {
		case "entrances":
			m.Entrances = v
		case "sharedEntrances":
			m.SharedEntrances = v
		case "ports":
			m.Ports = v
		case "envs":
			m.Envs = v
		}
	}

	consumed := make(map[string]struct{}, len(resourcesKeys)+
		len(passThroughObjectKeys)+len(passThroughArrayKeys)+
		len(scalarKeys)+len(excludedKeys))
	for _, list := range [][]string{
		resourcesKeys, passThroughObjectKeys, passThroughArrayKeys,
		scalarKeys, excludedKeys,
	} {
		for _, k := range list {
			consumed[k] = struct{}{}
		}
	}

	spec := make(map[string]any)
	for k, v := range rawData {
		if _, skip := consumed[k]; skip {
			continue
		}
		spec[k] = v
	}
	if len(spec) > 0 {
		m.Spec = spec
	}

	return m
}

// EntryScalars carries the identity / header scalars that
// BuildUserAppManifest strips out of raw_data because they have
// dedicated user_applications scalar columns (see scalarKeys). Without
// them, ComposeApplicationInfoEntry would emit an
// *ApplicationInfoEntry with empty ID / AppID / Version / CfgType /
// ApiVersion — visible on the NATS push and any future API caller —
// since the merged manifest map has no source for those keys.
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
// merges the JSONB column payloads into a single map, decodes the
// result into *ApplicationInfoEntry, and re-injects the identity scalars
// from `scalars` (because raw_data's id / appID / version / cfgType /
// apiVersion live in dedicated scalar columns rather than spec, see
// scalarKeys). Fields not declared on ApplicationInfoEntry are silently
// dropped; the struct is the API contract, not the storage contract.
//
// Round-trip property: BuildUserAppManifest(rawData) →
// ComposeApplicationInfoEntry(manifest, scalarsFromRawData) preserves
// every key ApplicationInfoEntry knows about, including the scalars,
// regardless of which JSONB column they ended up in.
func ComposeApplicationInfoEntry(m *UserAppManifest, scalars EntryScalars) (*ApplicationInfoEntry, error) {
	if m == nil {
		return nil, nil
	}

	merged := make(map[string]any)
	// Metadata, Resources, Spec all flatten into the same target struct
	// (ApplicationInfoEntry); their keys are disjoint by design (see the
	// resourcesKeys list and the OlaresManifest metadata vs spec split),
	// so merging them in any order produces the same result. Listing
	// Spec last keeps "spec wins on accidental overlap" as a defensive
	// last-resort tiebreaker.
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

// extractFixedFields returns a sub-map containing only the requested keys
// from rawData. Keys that are absent in rawData are not included; values
// that chart-repo returned (even zero values like "" or 0) are preserved
// verbatim so the resulting JSONB column mirrors raw_data exactly.
func extractFixedFields(rawData map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		v, ok := rawData[k]
		if !ok {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// passThroughObject returns the raw_data[key] map verbatim if it is a JSON
// object, otherwise nil. Empty objects are preserved (the column will
// contain '{}') so direct PG readers can tell "chart-repo returned this
// key as an empty object" apart from "chart-repo did not return this key".
func passThroughObject(rawData map[string]any, key string) map[string]any {
	v, ok := rawData[key].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

// passThroughArray returns the raw_data[key] array verbatim if it is a
// JSON array of objects, otherwise nil. Empty arrays are preserved (the
// column will contain '[]') for the same reason as passThroughObject.
func passThroughArray(rawData map[string]any, key string) []map[string]any {
	switch v := rawData[key].(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if mm, ok := item.(map[string]any); ok {
				out = append(out, mm)
			}
		}
		return out
	default:
		return nil
	}
}
