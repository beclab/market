package uploadevent

import (
	"context"
	"fmt"
	"strings"

	"market/internal/v2/chartrepo"
	"market/internal/v2/store"
	"market/internal/v2/types"

	"github.com/golang/glog"
)

// upsertUserApplicationRow assembles and writes the per-user
// user_applications row for an upload event. It is the event-driven
// counterpart of TaskForApiStep.Execute's UpsertRenderSuccess call —
// the field assembly is identical because both are projecting the same
// chart-repo SyncAppPayload onto the same set of JSONB columns; only
// the trigger and surrounding flow differ.
//
// Inputs:
//   - userID:    the user the upload event is scoped to (only this
//                user gets a user_applications row; other users with
//                pre-existing rows on the same (source, app) will be
//                refreshed by the next pipeline cycle through
//                store.ListRenderCandidates' version-drift branch).
//   - userZone:  the user's bytetrade.io/zone annotation, used by
//                EnrichEntranceURLs. Empty zone is fine — URL rewrite
//                no-ops in that case.
//   - sourceID:  carried verbatim from the event.
//   - appID:     the MD5-derived 8-char id (DeriveAppID output). Used
//                for both AppID and AppRawID — uploads always produce
//                the original chart, never a clone.
//   - payload:   pre-decoded SyncAppPayload (see DecodeUploadPayload).
//
// On a successful UpsertRenderSuccess the function returns nil. On
// failure it both logs the error and writes a placeholder failure row
// via store.MarkRenderFailed so the next pipeline cycle's
// ListRenderCandidates picks up the (user, source, app) tuple via its
// "render_status='failed'" branch and retries through the canonical
// hydration path. Returning the error lets the caller decide whether
// to suppress the new_app_ready notification (it should — see handler).
func upsertUserApplicationRow(
	ctx context.Context,
	userID, userZone, sourceID, appID string,
	payload *chartrepo.SyncAppPayload,
) error {
	if payload == nil || payload.RawDataEx == nil {
		return fmt.Errorf("upsertUserApplicationRow: payload missing raw_data_ex")
	}

	meta := payload.RawDataEx.Metadata
	appName := strings.TrimSpace(meta.Name)
	if appName == "" {
		// Mirror buildApplicationRow: appID becomes the fallback so
		// the NOT NULL app_name constraint is always satisfied. This
		// will also keep the (source_id, app_name) lookups in
		// LookupAppLocator etc. from breaking when chart-repo emits a
		// nameless manifest (theoretical case — validation should
		// have rejected it earlier).
		appName = appID
	}

	manifest := types.BuildUserAppManifest(payload.RawDataEx)
	// Inject entrance / shared-entrance URLs the same way
	// TaskForApiStep does, using the upload event's user zone. Empty
	// zone is a known/handled state — EnrichEntranceURLs no-ops and a
	// later pipeline cycle (after the user CR's zone annotation lands)
	// will not re-render the row, so a separate "zone-arrived" dirty
	// signal is the right long-term fix; recorded in the codebase
	// analysis but out of scope for this handler.
	types.EnrichEntranceURLs(manifest, appID, userZone)

	in := store.UpsertRenderSuccessInput{
		UserID:          userID,
		SourceID:        sourceID,
		AppID:           appID,
		AppName:         appName,
		AppRawID:        appID,
		AppRawName:      appName,
		ManifestVersion: payload.RawDataEx.ConfigVersion,
		ManifestType:    payload.RawDataEx.ConfigType,
		APIVersion:      payload.RawDataEx.APIVersion,
		Manifest:        manifest,
		I18n:            payload.I18n,
		VersionHistory:  payload.VersionHistory,
		ImageAnalysis:   payload.ImageAnalysis,
		RawPackage:      payload.RawPackage,
		RenderedPackage: payload.RenderedPackage,
	}

	if err := store.UpsertRenderSuccess(ctx, in); err != nil {
		glog.Errorf("uploadevent: UpsertRenderSuccess failed (user=%s source=%s app=%s): %v",
			userID, sourceID, appID, err)
		// Best-effort failure record: ignore secondary error to avoid
		// shadowing the primary one. The next pipeline cycle picks
		// the row up via ListRenderCandidates and retries through the
		// hydration path either way.
		_ = store.MarkRenderFailed(ctx, store.MarkRenderFailedInput{
			UserID:      userID,
			SourceID:    sourceID,
			AppID:       appID,
			AppName:     appName,
			AppRawID:    appID,
			AppRawName:  appName,
			RenderError: err.Error(),
		})
		return fmt.Errorf("upsert user_applications (user=%s source=%s app=%s): %w",
			userID, sourceID, appID, err)
	}
	return nil
}
