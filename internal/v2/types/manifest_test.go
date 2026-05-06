package types

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBuildUserAppManifest_Splitting(t *testing.T) {
	rawData := map[string]any{
		"id":             "homebox",
		"appID":          "homebox",
		"name":           "homebox",
		"title":          map[string]any{"en-US": "HomeBox"},
		"description":    map[string]any{"en-US": "desc"},
		"icon":           "https://example/icon.png",
		"categories":     []any{"Lifestyle"},
		"locale":         []any{"en-US"},
		"version":        "1.0.3",
		"cfgType":        "app",
		"apiVersion":     "",
		"requiredMemory": "20Mi",
		"requiredCPU":    "50m",
		"limitedMemory":  "100Mi",
		"supportArch":    []any{"amd64"},
		"developer":      "SysAdmins Media",
		"website":        "https://example",
		"permission":     map[string]any{"appCache": true},
		"entrances":      []any{map[string]any{"name": "homebox"}},
	}

	m := BuildUserAppManifest(rawData)

	if m.Resources["requiredMemory"] != "20Mi" || m.Resources["limitedMemory"] != "100Mi" {
		t.Fatalf("resources missing required/limited memory: %+v", m.Resources)
	}
	if _, ok := m.Resources["supportArch"]; ok {
		t.Fatalf("supportArch must NOT be in resources, got %+v", m.Resources)
	}

	for _, k := range []string{"name", "title", "description", "icon", "categories", "locale"} {
		if _, ok := m.Spec[k]; !ok {
			t.Fatalf("spec.%s missing (should fall through via catch-all): %+v", k, m.Spec)
		}
	}
	if m.Spec["supportArch"] == nil {
		t.Fatalf("spec.supportArch missing (should fall through via catch-all): %+v", m.Spec)
	}
	if m.Spec["developer"] != "SysAdmins Media" {
		t.Fatalf("spec.developer missing: %+v", m.Spec)
	}
	if _, ok := m.Spec["id"]; ok {
		t.Fatalf("scalar key 'id' must not appear in spec")
	}
	if _, ok := m.Spec["version"]; ok {
		t.Fatalf("scalar key 'version' must not appear in spec (carried by manifest_version column)")
	}
	if _, ok := m.Spec["apiVersion"]; ok {
		t.Fatalf("scalar key 'apiVersion' must not appear in spec (carried by api_version column)")
	}
	if m.Permission["appCache"] != true {
		t.Fatalf("permission missing: %+v", m.Permission)
	}
	if len(m.Entrances) != 1 {
		t.Fatalf("entrances missing: %+v", m.Entrances)
	}
}

func TestBuildUserAppManifest_NilInput(t *testing.T) {
	m := BuildUserAppManifest(nil)
	if m == nil {
		t.Fatal("expected non-nil empty manifest")
	}
	if m.Spec != nil || m.Resources != nil {
		t.Fatalf("expected all-nil fields, got %+v", m)
	}
}

// TestBuildUserAppManifest_ForwardCompat asserts that a hypothetical new
// chart-repo field that none of the dedicated columns or scalars know
// about lands in spec via catch-all. This test exists to lock down the
// catch-all behaviour against accidental refactors.
func TestBuildUserAppManifest_ForwardCompat(t *testing.T) {
	rawData := map[string]any{
		"id":      "homebox",
		"version": "1.0.3",
		"cfgType": "app",
		// A future chart-repo addition we have never seen before.
		"hypotheticalNewField": map[string]any{"k": "v"},
	}
	m := BuildUserAppManifest(rawData)
	v, ok := m.Spec["hypotheticalNewField"]
	if !ok {
		t.Fatalf("future field must be captured in spec via catch-all: %+v", m.Spec)
	}
	if !reflect.DeepEqual(v, map[string]any{"k": "v"}) {
		t.Fatalf("future field value not preserved: %+v", v)
	}
}

// TestBuildUserAppManifest_KeepsZeroValues asserts that zero-valued
// raw_data keys (empty string, 0, false, JSON null, empty arrays / maps)
// are NOT dropped from spec. Direct PG readers rely on
// user_applications.spec mirroring raw_data, including zero values, so
// they can distinguish "chart-repo returned this key as zero" from
// "chart-repo did not return this key".
func TestBuildUserAppManifest_KeepsZeroValues(t *testing.T) {
	rawData := map[string]any{
		"id":            "homebox",
		"appID":         "homebox",
		"target":        "",
		"rating":        float64(0),
		"onlyAdmin":     false,
		"promoteVideo": "",
		"subCategory":  "",
		"namespace":    "",
		"featuredImage": "",
		"legal":        nil,
		"count":        nil,
		"screenshots":  nil,
		"tags":         nil,
		"emptyMap":     map[string]any{},
		"emptyList":    []any{},
	}

	m := BuildUserAppManifest(rawData)

	cases := []struct {
		key  string
		want any
	}{
		{"target", ""},
		{"rating", float64(0)},
		{"onlyAdmin", false},
		{"promoteVideo", ""},
		{"subCategory", ""},
		{"namespace", ""},
		{"featuredImage", ""},
		{"legal", nil},
		{"count", nil},
		{"screenshots", nil},
		{"tags", nil},
		{"emptyMap", map[string]any{}},
		{"emptyList", []any{}},
	}
	for _, c := range cases {
		v, ok := m.Spec[c.key]
		if !ok {
			t.Errorf("spec.%s missing; zero values must be preserved", c.key)
			continue
		}
		if !reflect.DeepEqual(v, c.want) {
			t.Errorf("spec.%s = %#v, want %#v", c.key, v, c.want)
		}
	}
}

// TestBuildUserAppManifest_DropsMetadata asserts that chart-repo's own
// processing metadata block ("hydration_task_id", "rendered_chart_url",
// "validation_status", ...) never lands in user_applications.spec. The
// historical metadata column was dropped by migration 00008; without
// excludedKeys, this top-level "metadata" key would otherwise leak into
// spec via catch-all and re-create that data on the wrong column.
func TestBuildUserAppManifest_DropsMetadata(t *testing.T) {
	rawData := map[string]any{
		"id":      "homebox",
		"version": "1.0.4",
		"name":    "homebox",
		"metadata": map[string]any{
			"data_source":         "hydration_task",
			"hydration_status":    "completed",
			"hydration_task_id":   "user_market.olares_app_1234",
			"rendered_chart_url":  "https://example/chart.tgz",
			"validation_status":   "validated",
		},
	}

	m := BuildUserAppManifest(rawData)

	if _, ok := m.Spec["metadata"]; ok {
		t.Fatalf("spec must not contain chart-repo processing metadata: %+v", m.Spec)
	}
	// Sanity: the rest of the catch-all still works.
	if m.Spec["name"] != "homebox" {
		t.Fatalf("spec.name lost during metadata filtering: %+v", m.Spec)
	}
}

// TestBuildUserAppManifest_PassThroughEmptyContainers asserts that an
// explicitly empty pass-through key (e.g. "tailscale": {}) is preserved
// as an empty container rather than collapsed into nil. This complements
// TestBuildUserAppManifest_KeepsZeroValues: nil means "chart-repo did
// not return the key at all", while an empty object/array means
// "chart-repo returned it explicitly empty".
func TestBuildUserAppManifest_PassThroughEmptyContainers(t *testing.T) {
	rawData := map[string]any{
		"id":              "homebox",
		"version":         "1.0.4",
		"tailscale":       map[string]any{},
		"sharedEntrances": []any{},
	}

	m := BuildUserAppManifest(rawData)

	if m.Tailscale == nil {
		t.Fatalf("tailscale must be preserved as empty map, got nil")
	}
	if len(m.Tailscale) != 0 {
		t.Fatalf("tailscale must be an empty map, got %+v", m.Tailscale)
	}
	if m.SharedEntrances == nil {
		t.Fatalf("sharedEntrances must be preserved as empty slice, got nil")
	}
	if len(m.SharedEntrances) != 0 {
		t.Fatalf("sharedEntrances must be an empty slice, got %+v", m.SharedEntrances)
	}

	// Keys not present in rawData should still produce nil so the JSONB
	// column is written as SQL NULL.
	if m.Permission != nil {
		t.Fatalf("permission must be nil when raw_data omits the key, got %+v", m.Permission)
	}
}

// TestRoundTrip ensures BuildUserAppManifest -> ComposeApplicationInfoEntry
// preserves every non-scalar field declared on ApplicationInfoEntry.
//
// Scalar fields (id / appID / version / cfgType / apiVersion) are carried
// by dedicated user_applications columns and are NOT round-tripped through
// the manifest helpers — real callers re-inject them from their own scalar
// columns after Compose. The test mirrors this by re-injecting scalars on
// the composed result before comparing.
func TestRoundTrip(t *testing.T) {
	original := &ApplicationInfoEntry{
		ID:             "homebox",
		AppID:          "homebox",
		Name:           "homebox",
		ChartName:      "homebox",
		Icon:           "https://example/icon.png",
		Description:    map[string]string{"en-US": "desc"},
		Title:          map[string]string{"en-US": "HomeBox"},
		Version:        "1.0.3",
		CfgType:        "app",
		ApiVersion:     "",
		Categories:     []string{"Lifestyle"},
		Locale:         []string{"en-US"},
		Developer:      "SysAdmins Media",
		Website:        "https://example",
		RequiredMemory: "20Mi",
		RequiredDisk:   "50Mi",
		RequiredCPU:    "50m",
		LimitedMemory:  "100Mi",
		LimitedCPU:     "200m",
		SupportArch:    []string{"amd64", "arm64"},
		Permission:     map[string]any{"appCache": true, "appData": true},
		// Zero-valued fields exercise the "preserve everything" rule:
		// they survive the round-trip via spec rather than relying on
		// the struct's zero-value default.
		Target:        "",
		PromoteVideo:  "",
		SubCategory:   "",
		Namespace:     "",
		FeaturedImage: "",
		OnlyAdmin:     false,
	}

	buf, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var rawData map[string]any
	if err := json.Unmarshal(buf, &rawData); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	manifest := BuildUserAppManifest(rawData)
	composed, err := ComposeApplicationInfoEntry(manifest)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	composed.ID = original.ID
	composed.AppID = original.AppID
	composed.Version = original.Version
	composed.CfgType = original.CfgType
	composed.ApiVersion = original.ApiVersion

	wantJSON, _ := json.Marshal(original)
	gotJSON, _ := json.Marshal(composed)
	var want, got map[string]any
	_ = json.Unmarshal(wantJSON, &want)
	_ = json.Unmarshal(gotJSON, &got)

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("round-trip mismatch\n want: %s\n got:  %s", wantJSON, gotJSON)
	}
}
