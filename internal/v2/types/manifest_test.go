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

	// All "metadata-like" fields should now fall into spec via catch-all.
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
	if _, ok := m.Spec["apiVersion"]; ok {
		t.Fatalf("zero-value/scalar key 'apiVersion' must not appear in spec")
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

	// Re-inject scalars (= user_applications scalar columns) on the
	// composed result so the comparison only checks manifest content.
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
