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
var scalarKeys = []string{
	"id",
	"appID",
	"version",
	"cfgType",
	"apiVersion",
}

// BuildUserAppManifest splits a chart-repo raw_data payload into the
// JSONB-column shapes that user_applications expects. spec is a catch-all:
// every raw_data key not consumed by another column or scalar lands there,
// so chart-repo schema additions flow through without code changes.
//
// The caller is expected to have already applied any role-based filtering
// (e.g. stripping admin-only fields when writing for a regular user) on
// rawData before calling this function. BuildUserAppManifest itself is a
// pure transformation with no role awareness.
//
// Zero-valued raw_data fields (empty string, 0, empty slice/map, nil) are
// dropped from spec to avoid noise; they remain in resources / pass-through
// columns if they happen to be in those fixed key sets.
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
		len(passThroughObjectKeys)+len(passThroughArrayKeys)+len(scalarKeys))
	for _, list := range [][]string{resourcesKeys, passThroughObjectKeys, passThroughArrayKeys, scalarKeys} {
		for _, k := range list {
			consumed[k] = struct{}{}
		}
	}

	spec := make(map[string]any)
	for k, v := range rawData {
		if _, skip := consumed[k]; skip {
			continue
		}
		if isZeroValue(v) {
			continue
		}
		spec[k] = v
	}
	if len(spec) > 0 {
		m.Spec = spec
	}

	return m
}

// ComposeApplicationInfoEntry is the inverse of BuildUserAppManifest. It
// merges the JSONB column payloads into a single map and decodes the
// result into *ApplicationInfoEntry, the shape returned to the frontend.
// Fields not declared on ApplicationInfoEntry are silently dropped; the
// struct is the API contract, not the storage contract.
//
// Round-trip property: BuildUserAppManifest(rawData) → ComposeApplicationInfoEntry
// preserves every key that ApplicationInfoEntry knows about, regardless of
// which JSONB column it ended up in.
func ComposeApplicationInfoEntry(m *UserAppManifest) (*ApplicationInfoEntry, error) {
	if m == nil {
		return nil, nil
	}

	merged := make(map[string]any)
	for _, src := range []map[string]any{m.Resources, m.Spec} {
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
	return &entry, nil
}

// extractFixedFields returns a sub-map containing only the requested keys
// from rawData; missing or zero-valued entries are skipped to keep the
// resulting JSONB column compact.
func extractFixedFields(rawData map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		v, ok := rawData[k]
		if !ok || isZeroValue(v) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func passThroughObject(rawData map[string]any, key string) map[string]any {
	v, ok := rawData[key].(map[string]any)
	if !ok || len(v) == 0 {
		return nil
	}
	return v
}

func passThroughArray(rawData map[string]any, key string) []map[string]any {
	switch v := rawData[key].(type) {
	case []map[string]any:
		if len(v) == 0 {
			return nil
		}
		return v
	case []any:
		if len(v) == 0 {
			return nil
		}
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if mm, ok := item.(map[string]any); ok {
				out = append(out, mm)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

// isZeroValue reports whether v is the zero value of its dynamic type for
// the kinds of values json.Unmarshal can produce. Used by spec collection
// to drop noise such as empty strings, 0, false, empty arrays, empty maps,
// and nil pointers.
func isZeroValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case float64:
		return x == 0
	case int:
		return x == 0
	case int64:
		return x == 0
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	case []string:
		return len(x) == 0
	case []map[string]any:
		return len(x) == 0
	}
	return false
}
