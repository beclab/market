package types

import (
	"reflect"
	"testing"

	appv1 "github.com/beclab/api/api/app.bytetrade.io/v1alpha1"
	"github.com/beclab/api/manifest"
	"github.com/beclab/Olares/framework/oac"
)

// TestBuildUserAppManifest_Splitting verifies that an *oac.AppConfiguration
// payload lands in the right JSONB columns: metadata fields → m.Metadata,
// resource caps → m.Resources, the rest of AppSpec → m.Spec, top-level
// containers (entrances / permission / etc.) → their dedicated fields.
func TestBuildUserAppManifest_Splitting(t *testing.T) {
	cfg := &oac.AppConfiguration{
		ConfigVersion: "0.8.10",
		ConfigType:    "app",
		APIVersion:    "v2",
		Metadata: manifest.AppMetaData{
			Name:        "homebox",
			AppID:       "homebox",
			Title:       "HomeBox",
			Description: "desc",
			Icon:        "https://example/icon.png",
			Categories:  []string{"Lifestyle"},
			Version:     "1.0.3",
			Rating:      4.5,
			Target:      "",
			Type:        "app",
		},
		Spec: manifest.AppSpec{
			RequiredMemory: "20Mi",
			RequiredCPU:    "50m",
			LimitedMemory:  "100Mi",
			SupportArch:    []string{"amd64"},
			Developer:      "SysAdmins Media",
			Website:        "https://example",
			VersionName:    "1.0.3",
			Locale:         []string{"en-US"},
		},
		Permission: manifest.Permission{AppCache: true},
		Entrances: []appv1.Entrance{
			{Name: "homebox", Host: "homebox", Port: 80},
		},
	}

	m := BuildUserAppManifest(cfg)

	if m.Resources["requiredMemory"] != "20Mi" || m.Resources["limitedMemory"] != "100Mi" {
		t.Fatalf("resources missing required/limited memory: %+v", m.Resources)
	}
	if m.Resources["requiredCpu"] != "50m" {
		t.Fatalf("resources missing requiredCpu (oac casing): %+v", m.Resources)
	}
	// supportArch is in AppSpec but not a resource cap → falls into m.Spec
	if _, ok := m.Resources["supportArch"]; ok {
		t.Fatalf("supportArch must NOT be in resources, got %+v", m.Resources)
	}

	// metadata block lands in m.Metadata, not m.Spec
	for _, k := range []string{"name", "title", "description", "icon", "categories"} {
		if _, ok := m.Metadata[k]; !ok {
			t.Fatalf("metadata.%s missing: %+v", k, m.Metadata)
		}
	}

	// AppSpec catch-all → m.Spec
	if m.Spec["developer"] != "SysAdmins Media" {
		t.Fatalf("spec.developer missing: %+v", m.Spec)
	}
	if m.Spec["versionName"] != "1.0.3" {
		t.Fatalf("spec.versionName missing: %+v", m.Spec)
	}
	if m.Spec["website"] != "https://example" {
		t.Fatalf("spec.website missing: %+v", m.Spec)
	}
	// resource caps must NOT bleed into m.Spec — they were stripped from
	// the AppSpec marshal in buildSpec.
	for _, k := range resourceFieldKeys {
		if _, ok := m.Spec[k]; ok {
			t.Fatalf("resource key %q must not appear in spec: %+v", k, m.Spec)
		}
	}

	if v, ok := m.Permission["appCache"]; !ok || v != true {
		t.Fatalf("permission missing appCache: %+v", m.Permission)
	}
	if len(m.Entrances) != 1 || m.Entrances[0]["name"] != "homebox" {
		t.Fatalf("entrances missing or wrong shape: %+v", m.Entrances)
	}
}

// TestBuildUserAppManifest_NilInput verifies the nil guard returns an
// empty (but non-nil) manifest so callers can rely on the shape.
func TestBuildUserAppManifest_NilInput(t *testing.T) {
	m := BuildUserAppManifest(nil)
	if m == nil {
		t.Fatal("expected non-nil empty manifest")
	}
	if m.Spec != nil || m.Resources != nil || m.Metadata != nil {
		t.Fatalf("expected all-nil fields, got %+v", m)
	}
}

// TestBuildUserAppManifest_PassThroughEmptyContainers documents how the
// typed pipeline handles "empty" inputs. Unlike the legacy map-based
// path, *oac.AppConfiguration is a closed struct: value-type fields
// (Permission, Options, TailScale) cannot be told apart from "absent"
// at the typed layer — they always serialise to at least a default
// object — while pointer-type fields (Middleware) and slices with
// len==0 (SharedEntrances) ARE distinguishable from "present-empty"
// and stay nil so the JSONB column is written as SQL NULL.
func TestBuildUserAppManifest_PassThroughEmptyContainers(t *testing.T) {
	cfg := &oac.AppConfiguration{
		Metadata:        manifest.AppMetaData{Name: "homebox", Version: "1.0.4"},
		TailScale:       appv1.TailScale{},
		SharedEntrances: []appv1.Entrance{},
		// Middleware is *Middleware (pointer), Provider/Envs/etc. are
		// nil slices — all three cases below should produce nil columns.
	}

	m := BuildUserAppManifest(cfg)

	// TailScale fields are all omitempty, so empty struct → "{}" → empty map.
	if m.Tailscale == nil {
		t.Fatalf("tailscale must be preserved as (empty) map, got nil")
	}
	// Permission fields have no omitempty, so empty struct serialises
	// to a default-zero map; this is the unavoidable cost of typed
	// inputs — the column always gets written.
	if m.Permission == nil {
		t.Fatalf("permission expected non-nil for empty Permission{} (no omitempty), got nil")
	}

	// Pointer-nil Middleware → nil column (SQL NULL).
	if m.Middleware != nil {
		t.Fatalf("middleware must be nil when cfg.Middleware is a nil *Middleware, got %+v", m.Middleware)
	}
	// Empty slice → nil column (SQL NULL).
	if m.SharedEntrances != nil {
		t.Fatalf("sharedEntrances must be nil for empty input slice, got %+v", m.SharedEntrances)
	}
	if m.Envs != nil {
		t.Fatalf("envs must be nil when cfg omits it, got %+v", m.Envs)
	}
}

// TestBuildUserAppManifest_Resources_FromOAC pins the typed → m.Resources
// extraction. Note we use oac-casing keys (requiredCpu / requiredGpu /
// limitedCpu / limitedGpu) here; the ApplicationInfoEntry-casing
// translation (CPU / GPU all-caps) happens in
// ComposeApplicationInfoEntry, not in Build.
func TestBuildUserAppManifest_Resources_FromOAC(t *testing.T) {
	cfg := &oac.AppConfiguration{
		Spec: manifest.AppSpec{
			RequiredMemory: "20Mi",
			RequiredDisk:   "50Mi",
			RequiredCPU:    "50m",
			RequiredGPU:    "0",
			LimitedMemory:  "100Mi",
			LimitedDisk:    "200Mi",
			LimitedCPU:     "200m",
			LimitedGPU:     "1",
		},
	}
	m := BuildUserAppManifest(cfg)

	want := map[string]any{
		"requiredMemory": "20Mi",
		"requiredDisk":   "50Mi",
		"requiredCpu":    "50m",
		"requiredGpu":    "0",
		"limitedMemory":  "100Mi",
		"limitedDisk":    "200Mi",
		"limitedCpu":     "200m",
		"limitedGpu":     "1",
	}
	if !reflect.DeepEqual(m.Resources, want) {
		t.Fatalf("resources mismatch\n want: %#v\n got:  %#v", want, m.Resources)
	}
}

// TestBuildUserAppManifest_Resources_OmitsZero asserts that resource
// caps with empty-string values are dropped from m.Resources. oac.AppSpec
// carries these as bare string fields with no omitempty / pointer-nil
// marker, so an empty value here cannot be told apart from "not present"
// at the typed layer.
func TestBuildUserAppManifest_Resources_OmitsZero(t *testing.T) {
	cfg := &oac.AppConfiguration{
		Spec: manifest.AppSpec{
			RequiredMemory: "20Mi",
		},
	}
	m := BuildUserAppManifest(cfg)
	if _, ok := m.Resources["requiredCpu"]; ok {
		t.Errorf("empty RequiredCPU must not appear in m.Resources: %+v", m.Resources)
	}
	if _, ok := m.Resources["limitedGpu"]; ok {
		t.Errorf("empty LimitedGPU must not appear in m.Resources: %+v", m.Resources)
	}
	if m.Resources["requiredMemory"] != "20Mi" {
		t.Errorf("non-empty RequiredMemory lost: %+v", m.Resources)
	}
}

// TestComposeApplicationInfoEntry_InjectsScalars asserts the scalars
// pass-through: even when the manifest is empty, the returned entry
// carries the requested identity / header values. This is the property
// the hydration NATS push relies on so app_info never leaks empty
// id/appID/version/cfgType/apiVersion.
func TestComposeApplicationInfoEntry_InjectsScalars(t *testing.T) {
	m := &UserAppManifest{}
	scalars := EntryScalars{
		ID:         "homebox",
		AppID:      "homebox",
		Version:    "1.0.4",
		CfgType:    "app",
		ApiVersion: "v1",
	}
	entry, err := ComposeApplicationInfoEntry(m, scalars)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if entry.ID != "homebox" || entry.AppID != "homebox" {
		t.Errorf("id/appID not injected: %#v", entry)
	}
	if entry.Version != "1.0.4" {
		t.Errorf("version not injected: %q", entry.Version)
	}
	if entry.CfgType != "app" {
		t.Errorf("cfgType not injected: %q", entry.CfgType)
	}
	if entry.ApiVersion != "v1" {
		t.Errorf("apiVersion not injected: %q", entry.ApiVersion)
	}
}

// TestComposeApplicationInfoEntry_MergesMetadata asserts that
// UserAppManifest.Metadata — the OlaresManifest "metadata" block from
// raw_data_ex — is folded into the merged map alongside Spec/Resources,
// so its name / icon / categories etc. land on the composed
// *ApplicationInfoEntry. (Title / description go through the
// multi-language wrap path; covered by TestComposeApplicationInfoEntry_WrapsMultiLangStrings.)
func TestComposeApplicationInfoEntry_MergesMetadata(t *testing.T) {
	m := &UserAppManifest{
		Metadata: map[string]any{
			"name":       "ollamav2",
			"icon":       "https://example/icon.png",
			"categories": []any{"Productivity"},
			"rating":     float64(0),
			"target":     "",
		},
		Spec: map[string]any{
			"developer":   "ollama",
			"versionName": "0.20.5",
			"website":     "https://ollama.com/",
		},
	}

	entry, err := ComposeApplicationInfoEntry(m, EntryScalars{
		ID:         "ollamav2",
		AppID:      "ollamav2",
		Version:    "1.0.22",
		CfgType:    "app",
		ApiVersion: "v2",
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	if entry.Name != "ollamav2" {
		t.Errorf("name from metadata lost: %q", entry.Name)
	}
	if entry.Icon != "https://example/icon.png" {
		t.Errorf("icon from metadata lost: %q", entry.Icon)
	}
	if len(entry.Categories) != 1 || entry.Categories[0] != "Productivity" {
		t.Errorf("categories from metadata lost: %+v", entry.Categories)
	}
	if entry.Developer != "ollama" {
		t.Errorf("developer from spec lost: %q", entry.Developer)
	}
	if entry.VersionName != "0.20.5" {
		t.Errorf("versionName from spec lost: %q", entry.VersionName)
	}
}

// TestComposeApplicationInfoEntry_TranslatesResourceKeys nails the
// oac-casing → ApplicationInfoEntry-casing translation for the 4
// CPU/GPU resource caps. If anyone changes resourceKeyAliases or the
// ApplicationInfoEntry json tags without updating the other side, this
// test fails immediately.
func TestComposeApplicationInfoEntry_TranslatesResourceKeys(t *testing.T) {
	m := &UserAppManifest{
		Resources: map[string]any{
			"requiredMemory": "20Mi",
			"requiredCpu":    "50m",
			"requiredGpu":    "0",
			"limitedMemory":  "100Mi",
			"limitedCpu":     "200m",
			"limitedGpu":     "1",
		},
	}
	entry, err := ComposeApplicationInfoEntry(m, EntryScalars{})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	if entry.RequiredMemory != "20Mi" {
		t.Errorf("requiredMemory lost: %q", entry.RequiredMemory)
	}
	if entry.RequiredCPU != "50m" {
		t.Errorf("requiredCpu→requiredCPU translation broken: %q", entry.RequiredCPU)
	}
	if entry.RequiredGPU != "0" {
		t.Errorf("requiredGpu→requiredGPU translation broken: %q", entry.RequiredGPU)
	}
	if entry.LimitedCPU != "200m" {
		t.Errorf("limitedCpu→limitedCPU translation broken: %q", entry.LimitedCPU)
	}
	if entry.LimitedGPU != "1" {
		t.Errorf("limitedGpu→limitedGPU translation broken: %q", entry.LimitedGPU)
	}
}

// TestComposeApplicationInfoEntry_WrapsMultiLangStrings asserts that
// the four AppMetaData / AppSpec single-string fields that
// ApplicationInfoEntry types as map[string]string are wrapped into a
// single "en-US" locale before unmarshaling, so Compose doesn't fail
// with a json type error and notifyRenderSuccess doesn't have to fall
// back to the catalog entry on every push.
func TestComposeApplicationInfoEntry_WrapsMultiLangStrings(t *testing.T) {
	m := &UserAppManifest{
		Metadata: map[string]any{
			"name":        "homebox",
			"title":       "HomeBox",
			"description": "desc",
		},
		Spec: map[string]any{
			"fullDescription":    "full",
			"upgradeDescription": "upgrade",
		},
	}
	entry, err := ComposeApplicationInfoEntry(m, EntryScalars{})
	if err != nil {
		t.Fatalf("compose returned error (multi-lang wrap broken?): %v", err)
	}
	if got := entry.Title["en-US"]; got != "HomeBox" {
		t.Errorf("title[en-US] = %q, want %q", got, "HomeBox")
	}
	if got := entry.Description["en-US"]; got != "desc" {
		t.Errorf("description[en-US] = %q, want %q", got, "desc")
	}
	if got := entry.FullDescription["en-US"]; got != "full" {
		t.Errorf("fullDescription[en-US] = %q, want %q", got, "full")
	}
	if got := entry.UpgradeDescription["en-US"]; got != "upgrade" {
		t.Errorf("upgradeDescription[en-US] = %q, want %q", got, "upgrade")
	}
}

// TestRoundTrip_OACToEntry is the end-to-end smoke test for the typed
// pipeline: build a populated *oac.AppConfiguration → BuildUserAppManifest
// → ComposeApplicationInfoEntry → assert the API contract carries the
// right fields. CPU/GPU translation, multi-language wrap, and scalar
// re-injection are all exercised together here.
func TestRoundTrip_OACToEntry(t *testing.T) {
	cfg := &oac.AppConfiguration{
		ConfigVersion: "0.8.10",
		ConfigType:    "app",
		APIVersion:    "v2",
		Metadata: manifest.AppMetaData{
			Name:        "homebox",
			AppID:       "homebox",
			Icon:        "https://example/icon.png",
			Title:       "HomeBox",
			Description: "desc",
			Categories:  []string{"Lifestyle"},
			Version:     "1.0.3",
		},
		Spec: manifest.AppSpec{
			Developer:      "SysAdmins Media",
			Website:        "https://example",
			VersionName:    "1.0.3",
			RequiredMemory: "20Mi",
			RequiredDisk:   "50Mi",
			RequiredCPU:    "50m",
			LimitedMemory:  "100Mi",
			LimitedCPU:     "200m",
			SupportArch:    []string{"amd64", "arm64"},
		},
		Permission: manifest.Permission{AppCache: true, AppData: true},
		Entrances: []appv1.Entrance{
			{Name: "homebox", Host: "homebox", Port: 80},
		},
	}

	m := BuildUserAppManifest(cfg)
	entry, err := ComposeApplicationInfoEntry(m, EntryScalars{
		ID:         "homebox",
		AppID:      "homebox",
		Version:    cfg.Metadata.Version,
		CfgType:    cfg.ConfigType,
		ApiVersion: cfg.APIVersion,
	})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	// Identity scalars (re-injected).
	if entry.ID != "homebox" || entry.AppID != "homebox" {
		t.Errorf("id/appID lost: %#v", entry)
	}
	if entry.Version != "1.0.3" || entry.CfgType != "app" || entry.ApiVersion != "v2" {
		t.Errorf("scalars lost: ver=%q cfg=%q api=%q", entry.Version, entry.CfgType, entry.ApiVersion)
	}

	// Metadata.
	if entry.Name != "homebox" || entry.Icon != "https://example/icon.png" {
		t.Errorf("metadata lost: %#v", entry)
	}
	if entry.Title["en-US"] != "HomeBox" || entry.Description["en-US"] != "desc" {
		t.Errorf("multi-lang wrap broken: title=%v desc=%v", entry.Title, entry.Description)
	}
	if len(entry.Categories) != 1 || entry.Categories[0] != "Lifestyle" {
		t.Errorf("categories lost: %+v", entry.Categories)
	}

	// Spec catch-all.
	if entry.Developer != "SysAdmins Media" || entry.Website != "https://example" {
		t.Errorf("spec catch-all lost: %#v", entry)
	}
	if entry.VersionName != "1.0.3" {
		t.Errorf("versionName lost: %q", entry.VersionName)
	}
	if len(entry.SupportArch) != 2 {
		t.Errorf("supportArch lost: %+v", entry.SupportArch)
	}

	// Resources (with case translation).
	if entry.RequiredMemory != "20Mi" || entry.RequiredDisk != "50Mi" {
		t.Errorf("memory/disk caps lost: %#v", entry)
	}
	if entry.RequiredCPU != "50m" {
		t.Errorf("requiredCPU lost in round-trip: %q", entry.RequiredCPU)
	}
	if entry.LimitedMemory != "100Mi" || entry.LimitedCPU != "200m" {
		t.Errorf("limited caps lost: %#v", entry)
	}

	// Pass-through containers.
	if v, ok := entry.Permission["appCache"]; !ok || v != true {
		t.Errorf("permission lost: %+v", entry.Permission)
	}
	if len(entry.Entrances) != 1 {
		t.Errorf("entrances lost: %+v", entry.Entrances)
	}
}
