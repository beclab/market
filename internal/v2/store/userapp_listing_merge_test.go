package store

import (
	"reflect"
	"testing"

	"market/internal/v2/types"
)

// TestMergeEntranceStatuses_EqualLength pins the canonical case where
// upstream NATS reports a runtime status for every manifest-declared
// entrance: each output entry takes Name/Host/Port/Icon/Title/Url etc.
// from manifest and overlays State/StatusTime/Reason from status[i].
func TestMergeEntranceStatuses_EqualLength(t *testing.T) {
	manifest := []types.AppStateLatestDataEntrances{
		{Name: "ui", Host: "ui", Port: 80, Icon: "icon-ui", Title: "UI", Url: "ui.alice.olares.com"},
		{Name: "api", Host: "api", Port: 8080, Icon: "icon-api", Title: "API", Url: "api.alice.olares.com"},
	}
	status := []types.AppStateLatestDataEntrances{
		{State: "running", StatusTime: "2026-05-12T10:00:00Z", Reason: ""},
		{State: "starting", StatusTime: "2026-05-12T10:00:01Z", Reason: "warmup"},
	}

	got := mergeEntranceStatuses(manifest, status)
	want := []types.AppStateLatestDataEntrances{
		{Name: "ui", Host: "ui", Port: 80, Icon: "icon-ui", Title: "UI", Url: "ui.alice.olares.com",
			State: "running", StatusTime: "2026-05-12T10:00:00Z"},
		{Name: "api", Host: "api", Port: 8080, Icon: "icon-api", Title: "API", Url: "api.alice.olares.com",
			State: "starting", StatusTime: "2026-05-12T10:00:01Z", Reason: "warmup"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge equal-length mismatch\n got=%+v\nwant=%+v", got, want)
	}
}

// TestMergeEntranceStatuses_StatusShorter covers the case where the
// runtime has not yet reported on every manifest-declared entrance.
// Entries beyond status length must keep zero State/StatusTime/Reason
// while still surfacing their declarative fields so the frontend can
// render an "unknown state" placeholder.
func TestMergeEntranceStatuses_StatusShorter(t *testing.T) {
	manifest := []types.AppStateLatestDataEntrances{
		{Name: "ui", Title: "UI"},
		{Name: "api", Title: "API"},
		{Name: "metrics", Title: "Metrics"},
	}
	status := []types.AppStateLatestDataEntrances{
		{State: "running", StatusTime: "t1"},
	}

	got := mergeEntranceStatuses(manifest, status)
	if len(got) != 3 {
		t.Fatalf("expected 3 entries (manifest length), got %d", len(got))
	}
	if got[0].State != "running" || got[0].StatusTime != "t1" {
		t.Fatalf("got[0] should carry status[0]'s runtime fields, got %+v", got[0])
	}
	if got[1].State != "" || got[1].StatusTime != "" || got[1].Reason != "" {
		t.Fatalf("got[1] runtime fields should be zero, got %+v", got[1])
	}
	if got[1].Title != "API" {
		t.Fatalf("got[1].Title should be preserved from manifest, got %q", got[1].Title)
	}
	if got[2].State != "" || got[2].Title != "Metrics" {
		t.Fatalf("got[2] expected zero runtime + manifest Title, got %+v", got[2])
	}
}

// TestMergeEntranceStatuses_StatusLonger covers an upstream/manifest
// drift: the runtime reports MORE entrance statuses than the manifest
// declares (e.g. app-service spawned an entrance not yet known to the
// manifest). Excess status entries are dropped — manifest is the
// authoritative LIST and silently appending unknown entries would
// confuse the frontend renderer.
func TestMergeEntranceStatuses_StatusLonger(t *testing.T) {
	manifest := []types.AppStateLatestDataEntrances{
		{Name: "ui", Title: "UI"},
	}
	status := []types.AppStateLatestDataEntrances{
		{State: "running"},
		{State: "running"},
		{State: "starting"},
	}

	got := mergeEntranceStatuses(manifest, status)
	if len(got) != 1 {
		t.Fatalf("expected output length to match manifest (1), got %d", len(got))
	}
	if got[0].Title != "UI" || got[0].State != "running" {
		t.Fatalf("got[0] = %+v, want manifest fields + status[0].State", got[0])
	}
}

// TestMergeEntranceStatuses_ManifestEmpty is the legacy / partially
// rendered fallback: when ua.entrances is NULL we cannot anchor on
// declarative fields, so we surface the runtime status verbatim so
// the frontend at least sees state transitions. Non-runtime fields
// stay zero, matching the row's actual storage state.
func TestMergeEntranceStatuses_ManifestEmpty(t *testing.T) {
	status := []types.AppStateLatestDataEntrances{
		{State: "running"},
		{State: "starting"},
	}

	got := mergeEntranceStatuses(nil, status)
	if !reflect.DeepEqual(got, status) {
		t.Fatalf("nil manifest should pass status through verbatim\n got=%+v\nwant=%+v", got, status)
	}

	got2 := mergeEntranceStatuses([]types.AppStateLatestDataEntrances{}, status)
	if !reflect.DeepEqual(got2, status) {
		t.Fatalf("empty manifest should pass status through verbatim\n got=%+v\nwant=%+v", got2, status)
	}
}
