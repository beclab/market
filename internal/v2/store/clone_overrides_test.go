package store

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestApplyCloneOverrides_MetadataTitle pins the canonical metadata-title
// patch: source has {"title":"Origin","name":"foo"}, override is "Override",
// output keeps "name" intact and switches title to "Override".
func TestApplyCloneOverrides_MetadataTitle(t *testing.T) {
	srcMeta := []byte(`{"title":"Origin","name":"foo","version":"1.0.0"}`)
	out, _, err := applyCloneOverrides(srcMeta, nil, "Override", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v (raw=%s)", err, out)
	}
	if got["title"] != "Override" {
		t.Errorf("metadata.title: got %v want Override", got["title"])
	}
	if got["name"] != "foo" {
		t.Errorf("metadata.name leaked / changed: got %v want foo", got["name"])
	}
	if got["version"] != "1.0.0" {
		t.Errorf("metadata.version leaked / changed: got %v want 1.0.0", got["version"])
	}
}

// TestApplyCloneOverrides_EmptyTitleClears verifies user decision #2:
// an empty Title is interpreted as "clear" rather than "preserve". The
// metadata.title key remains, but its value becomes the empty string.
func TestApplyCloneOverrides_EmptyTitleClears(t *testing.T) {
	srcMeta := []byte(`{"title":"Origin","name":"foo"}`)
	out, _, err := applyCloneOverrides(srcMeta, nil, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v (raw=%s)", err, out)
	}
	v, ok := got["title"]
	if !ok {
		t.Fatalf("metadata.title key was removed; expected empty-string value")
	}
	if v != "" {
		t.Errorf("metadata.title: got %v want empty string", v)
	}
	if got["name"] != "foo" {
		t.Errorf("metadata.name lost on title clear: got %v want foo", got["name"])
	}
}

// TestApplyCloneOverrides_MetadataNullPreservedOnEmptyTitle covers the
// "source metadata is SQL NULL and the override is also empty" path: the
// output must remain nil so the clone row preserves SQL NULL too.
func TestApplyCloneOverrides_MetadataNullPreservedOnEmptyTitle(t *testing.T) {
	out, _, err := applyCloneOverrides(nil, nil, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != nil {
		t.Errorf("metadata output: got %s, want nil to preserve SQL NULL", out)
	}
}

// TestApplyCloneOverrides_MetadataNullSynthesizedOnTitleSet pins the
// "source metadata is SQL NULL but caller supplied a title" path: a
// fresh {"title": "..."} object is synthesised.
func TestApplyCloneOverrides_MetadataNullSynthesizedOnTitleSet(t *testing.T) {
	out, _, err := applyCloneOverrides(nil, nil, "My Clone", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatalf("metadata output is nil; expected a {\"title\":...} synthetic object")
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v (raw=%s)", err, out)
	}
	if got["title"] != "My Clone" {
		t.Errorf("synthetic metadata.title: got %v want My Clone", got["title"])
	}
	if len(got) != 1 {
		t.Errorf("synthetic metadata should only carry title; got keys=%v", keys(got))
	}
}

// TestApplyCloneOverrides_EntrancesIndexAlignment exercises user decision
// #1 (index alignment) on a typical M == N case: each request title
// overrides the same-index source entrance's title; non-title fields
// (host, port, icon) round-trip byte-faithfully.
func TestApplyCloneOverrides_EntrancesIndexAlignment(t *testing.T) {
	srcEntrances := []byte(`[
		{"name":"ui","title":"UI Origin","host":"ui","port":80,"icon":"icon-ui"},
		{"name":"api","title":"API Origin","host":"api","port":8080,"icon":"icon-api"}
	]`)
	_, out, err := applyCloneOverrides(nil, srcEntrances, "", []string{"My UI", "My API"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON array: %v (raw=%s)", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("entrances length: got %d want 2", len(got))
	}
	if got[0]["title"] != "My UI" {
		t.Errorf("entrances[0].title: got %v want My UI", got[0]["title"])
	}
	if got[1]["title"] != "My API" {
		t.Errorf("entrances[1].title: got %v want My API", got[1]["title"])
	}
	// non-title fields round-trip
	if got[0]["host"] != "ui" || got[0]["icon"] != "icon-ui" {
		t.Errorf("entrances[0] non-title fields drifted: got %+v", got[0])
	}
	if got[1]["host"] != "api" || got[1]["icon"] != "icon-api" {
		t.Errorf("entrances[1] non-title fields drifted: got %+v", got[1])
	}
}

// TestApplyCloneOverrides_EmptyEntranceTitleClears verifies user decision
// #2 at the entrance level: an empty override title clears that
// entrance's title (without removing the key) and leaves other entrances
// at later indices untouched.
func TestApplyCloneOverrides_EmptyEntranceTitleClears(t *testing.T) {
	srcEntrances := []byte(`[
		{"name":"ui","title":"UI Origin","host":"ui"},
		{"name":"api","title":"API Origin","host":"api"}
	]`)
	_, out, err := applyCloneOverrides(nil, srcEntrances, "", []string{"", "Renamed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if v, ok := got[0]["title"]; !ok || v != "" {
		t.Errorf("entrances[0].title: got %v (ok=%v) want empty string with key present", v, ok)
	}
	if got[1]["title"] != "Renamed" {
		t.Errorf("entrances[1].title: got %v want Renamed", got[1]["title"])
	}
}

// TestApplyCloneOverrides_LessOverridesPreserveTail verifies user
// decision #3 for M < N: the source's trailing entrances keep their
// original titles when the override slice is shorter than the source.
func TestApplyCloneOverrides_LessOverridesPreserveTail(t *testing.T) {
	srcEntrances := []byte(`[
		{"name":"ui","title":"UI Origin"},
		{"name":"api","title":"API Origin"},
		{"name":"admin","title":"Admin Origin"}
	]`)
	_, out, err := applyCloneOverrides(nil, srcEntrances, "", []string{"Only UI"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("entrances length must stay 3 (M<N rule); got %d", len(got))
	}
	if got[0]["title"] != "Only UI" {
		t.Errorf("entrances[0].title: got %v want Only UI", got[0]["title"])
	}
	if got[1]["title"] != "API Origin" {
		t.Errorf("entrances[1].title: got %v want API Origin (preserved)", got[1]["title"])
	}
	if got[2]["title"] != "Admin Origin" {
		t.Errorf("entrances[2].title: got %v want Admin Origin (preserved)", got[2]["title"])
	}
}

// TestApplyCloneOverrides_MoreOverridesDroppedSilently verifies user
// decision #3 for M > N: extra request entries beyond the source's
// entrance count are silently dropped without error.
func TestApplyCloneOverrides_MoreOverridesDroppedSilently(t *testing.T) {
	srcEntrances := []byte(`[
		{"name":"ui","title":"UI Origin"}
	]`)
	_, out, err := applyCloneOverrides(nil, srcEntrances, "", []string{"Patched UI", "ignored 1", "ignored 2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entrances length must stay 1; got %d (M>N rule should drop extras)", len(got))
	}
	if got[0]["title"] != "Patched UI" {
		t.Errorf("entrances[0].title: got %v want Patched UI", got[0]["title"])
	}
}

// TestApplyCloneOverrides_EntrancesByteFidelity covers user decision #6:
// non-title bytes inside entrance objects round-trip without numeric
// precision loss. The "port":8080 integer must NOT become 8080.0 etc.
// after JSON decode + re-encode.
func TestApplyCloneOverrides_EntrancesByteFidelity(t *testing.T) {
	srcEntrances := []byte(`[{"name":"api","title":"API Origin","port":8080,"weight":1}]`)
	_, out, err := applyCloneOverrides(nil, srcEntrances, "", []string{"Patched"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// String search: any precision loss would manifest as "8080.0" or
	// "1e+09" style scientific notation. Direct byte match keeps the
	// assertion robust against future encoder whitespace changes.
	str := string(out)
	if !strings.Contains(str, `"port":8080`) {
		t.Errorf("port round-trip drift: got %s", str)
	}
	if !strings.Contains(str, `"weight":1`) {
		t.Errorf("weight round-trip drift: got %s", str)
	}
}

// TestApplyCloneOverrides_NilEntrancesNoOp covers the "source has no
// entrances column" case: an empty override slice produces a nil
// output (no work to do), and an explicit override slice also produces
// nil — there is nothing to patch.
func TestApplyCloneOverrides_NilEntrancesNoOp(t *testing.T) {
	_, out, err := applyCloneOverrides(nil, nil, "", []string{"would be dropped"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != nil {
		t.Errorf("entrances output: got %s, want nil for nil source", out)
	}
}

// TestApplyCloneOverrides_EntrancesVerbatimWithoutOverrides covers the
// "source has entrances but caller did not pass any override" case: the
// source bytes are returned verbatim (pointer equality is not required,
// byte equality is).
func TestApplyCloneOverrides_EntrancesVerbatimWithoutOverrides(t *testing.T) {
	src := []byte(`[{"name":"ui","title":"UI"}]`)
	_, out, err := applyCloneOverrides(nil, src, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(src) {
		t.Errorf("entrances drift on no-op path: got %s want %s", out, src)
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
