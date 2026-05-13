package uploadevent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"market/internal/v2/chartrepo"
	"market/internal/v2/watchers"

	"github.com/golang/glog"
)

// EventData mirrors the chart-repo state-change payload for an
// app_upload_completed event. Lifted to its own type so the handler
// signature does not depend on chartrepo wire types directly — the
// caller (DataWatcherRepo) does the chartrepo →  EventData translation
// and then any in-process integration test can stub EventData without
// pulling chartrepo types into its setup.
type EventData struct {
	UserID  string
	Source  string
	AppName string
}

// Input bundles the per-call inputs Handle needs. Splitting from the
// per-process dependencies (HandleDeps) lets a long-lived
// DataWatcherRepo construct HandleDeps once and only build a fresh
// Input per event.
type Input struct {
	Event   EventData
	Payload map[string]interface{}
}

// HandleDeps wires the side-effect dependencies the handler must
// reach into. All fields are required for a fully-functional event
// projection but each has a defined behaviour when absent so unit
// tests can opt into smaller surfaces:
//
//   - Sender    nil → no NATS push (everything else still happens).
//   - UserLookup nil → fall back to empty zone (no entrance URL rewrite,
//     same as user_applications.entrances on a zone-missing render).
type HandleDeps struct {
	Sender     DataSender
	UserLookup func(string) (watchers.UserDataInfo, bool)
}

// Result is what the handler returns to the caller. AffectedUsers is
// the canonical list of users whose data was touched in this call —
// today that is always exactly Input.Event.UserID, but the slice form
// future-proofs the interface in case we extend fan-out (e.g. push
// "version-changed" notifications to other users who already have a
// pre-existing row on the same source / app_id).
type Result struct {
	AffectedUsers []string
	AppID         string // the MD5-derived app_id, empty if event was unusable
}

// Handle is the single entry point for projecting an
// "app_upload_completed" event onto Postgres + the NATS push layer.
//
// Pipeline of side effects (in order, fail-fast — later steps are
// skipped on earlier failures so the handler never publishes a
// notification for a half-written event):
//
//  1. Decode payload (DecodeUploadPayload) — ErrPayloadUnusable is
//     logged + early-return without an error so a malformed event
//     does not abort the surrounding state-change batch.
//  2. Derive the canonical 8-char app_id (DeriveAppID).
//  3. Upsert the catalog-side `applications` row.
//  4. Look up the user's zone via the UserLookup dependency (best
//     effort — zone may not be ready yet at first upload).
//  5. Upsert the per-user `user_applications` row with render_status
//     = 'success' (failures internally call MarkRenderFailed so the
//     hydration loop can pick the row up on the next cycle).
//  6. Emit `new_app_ready` over NATS for the event user only.
//
// Returns AffectedUsers = [event user] when the upload was usable,
// empty slice otherwise. Any error returned is a real failure that
// should propagate to the caller's logging — ErrPayloadUnusable is
// converted into a structured warning + nil error here so the caller
// does not need to special-case it.
func Handle(ctx context.Context, in Input, deps HandleDeps) (Result, error) {
	res := Result{}

	userID := strings.TrimSpace(in.Event.UserID)
	sourceID := strings.TrimSpace(in.Event.Source)
	appNameHint := strings.TrimSpace(in.Event.AppName)
	if userID == "" || sourceID == "" {
		return res, fmt.Errorf("uploadevent.Handle: empty user (%q) or source (%q)", userID, sourceID)
	}

	payload, err := DecodeUploadPayload(in.Payload)
	if err != nil {
		if errors.Is(err, ErrPayloadUnusable) {
			glog.Warningf("uploadevent: dropping event (user=%s source=%s app=%s) — %v",
				userID, sourceID, appNameHint, err)
			return res, nil
		}
		return res, fmt.Errorf("decode upload payload (user=%s source=%s app=%s): %w",
			userID, sourceID, appNameHint, err)
	}

	appID := DeriveAppID(payload.RawDataEx)
	if appID == "" {
		// DecodeUploadPayload already enforces metadata.appid != "",
		// so this branch is theoretically unreachable. Keep it as a
		// belt-and-suspenders guard rather than a panic so an upstream
		// schema regression does not crash the handler.
		glog.Warningf("uploadevent: app_id derivation yielded empty (user=%s source=%s app=%s); dropping",
			userID, sourceID, appNameHint)
		return res, nil
	}
	res.AppID = appID

	// Catalog-side write first. If it fails, abort before we touch
	// the per-user row — applications.app_id is the LEFT JOIN target
	// for ListRenderCandidates, so a missing applications row would
	// make the user_applications row invisible to the hydration
	// candidate query and prevent the canonical retry path.
	upsertCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := upsertApplicationsRow(upsertCtx, sourceID, appID, payload); err != nil {
		return res, err
	}

	// Resolve user zone. Empty zone is acceptable —
	// EnrichEntranceURLs no-ops and the row is still usable for the
	// catalog-side reads. A future "zone-arrived" dirty signal should
	// flag rows for re-render once the annotation lands.
	zone := lookupUserZone(deps.UserLookup, userID)

	if err := upsertUserApplicationRow(upsertCtx, userID, zone, sourceID, appID, payload); err != nil {
		// Per-user write failed; MarkRenderFailed has been recorded
		// and the next pipeline cycle will retry. Skip the push so
		// the frontend doesn't see a "ready" notification for an app
		// it cannot actually load.
		return res, err
	}

	res.AffectedUsers = []string{userID}

	notifyNewAppReady(deps.Sender, userID, sourceID, appID, payload)

	glog.V(2).Infof("uploadevent: completed (user=%s source=%s app=%s name=%q version=%s)",
		userID, sourceID, appID, payload.RawDataEx.Metadata.Name, payload.RawDataEx.Metadata.Version)
	return res, nil
}

// lookupUserZone is a thin wrapper around the injected UserLookup so
// the handler stays unaware of the underlying *sync.Map in
// internal/v2/watchers. Falls back to "" on missing user — callers
// must accept that as a documented "skip URL rewrite" signal.
func lookupUserZone(lookup func(string) (watchers.UserDataInfo, bool), userID string) string {
	if lookup == nil {
		return ""
	}
	info, ok := lookup(userID)
	if !ok {
		return ""
	}
	return info.Zone
}

// FetchAndHandle is a thin convenience wrapper for the chart-repo
// state-change hot path: it fetches the upload payload from chart-repo
// /apps and immediately calls Handle. Splitting the fetch into the
// caller (DataWatcherRepo) was tempting but every caller of this
// package today does both steps back-to-back, so co-locating them
// keeps the call site at DataWatcherRepo trivial.
//
// chartrepoBase is the host:port (or full URL) the underlying
// chartrepo.Client uses; the caller passes its own baseURL through.
// The fetched payload is converted to map[string]interface{} via the
// existing chartrepo.GetAppInfo signature (returns the apps[0] map),
// then handed to Handle which re-types it via DecodeUploadPayload —
// the round-trip is fine performance-wise because state-change
// volume is low.
func FetchAndHandle(
	ctx context.Context,
	chartrepoBase string,
	event EventData,
	deps HandleDeps,
) (Result, error) {
	res := Result{}

	c, err := chartrepo.NewClient(chartrepo.WithBaseURL(chartrepoBase))
	if err != nil {
		return res, fmt.Errorf("chartrepo client: %w", err)
	}
	apps, err := c.GetAppInfo(ctx, chartrepo.AppsRequest{
		UserID: event.UserID,
		Apps: []chartrepo.AppsRequestItem{{
			AppID:          event.AppName, // chart-repo /apps keys items by app_name on the wire
			SourceDataName: event.Source,
		}},
	})
	if err != nil {
		return res, fmt.Errorf("fetch upload payload: %w", err)
	}
	if len(apps) == 0 {
		glog.Warningf("uploadevent: chart-repo /apps returned no app for (user=%s source=%s app=%s); dropping",
			event.UserID, event.Source, event.AppName)
		return res, nil
	}
	return Handle(ctx, Input{Event: event, Payload: apps[0]}, deps)
}
