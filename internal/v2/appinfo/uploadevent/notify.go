package uploadevent

import (
	"strings"
	"time"

	"market/internal/v2/chartrepo"
	"market/internal/v2/types"

	"github.com/golang/glog"
)

// DataSender abstracts the NATS publisher dependency so the package
// can be exercised in tests with a stub. The single method we call
// matches *appinfo.DataSender.SendMarketSystemUpdate verbatim, so the
// real *DataSender satisfies this interface implicitly.
type DataSender interface {
	SendMarketSystemUpdate(update types.MarketSystemUpdate) error
}

// notifyNewAppReady emits the same `Point="new_app_ready"` market
// system update that Pipeline.notifyRenderSuccess emits at the end of
// hydration. Sharing the wire shape (NotifyType / Point / Extensions
// keys / ExtensionsObj.app_info structure) lets the frontend keep its
// single-event subscription path and treat upload-event-triggered
// pushes identically to hydration-triggered pushes.
//
// We intentionally do NOT call back into Pipeline.notifyRenderSuccess
// directly: that method is bound to *Pipeline state, takes a
// store.RenderCandidate + *hydrationfn.HydrationTask (neither of which
// the upload path produces), and would force this handler to fabricate
// hydration-shaped values. The 30-line near-duplication here is
// preferable to that coupling — see the package doc on uploadevent for
// the full rationale.
//
// Recipient policy:
//   - Only the user the upload event is scoped to (event.UserID) gets
//     the push. Other users who already observe the same (source,
//     app_id) row will get their notifications later through the
//     pipeline path when their own user_applications rows are
//     hydrated as version-drift candidates.
//
// Best-effort delivery:
//   - nil sender (test contexts, or DataSender construction failed at
//     startup) is silent.
//   - SendMarketSystemUpdate failures are logged but do NOT fail the
//     handler — the user_applications row is already persisted with
//     render_status='success', so a missed push is recoverable from
//     the next /market/data poll.
func notifyNewAppReady(
	sender DataSender,
	userID, sourceID, appID string,
	payload *chartrepo.SyncAppPayload,
) {
	if sender == nil || payload == nil || payload.RawDataEx == nil {
		return
	}
	meta := payload.RawDataEx.Metadata
	appName := strings.TrimSpace(meta.Name)
	if appName == "" {
		appName = appID
	}
	appVersion := strings.TrimSpace(meta.Version)
	if appVersion == "" {
		appVersion = strings.TrimSpace(payload.Version)
	}

	// Compose the wire `app_info` payload from the rendered manifest
	// using the same composer hydration uses. This keeps the JSON the
	// frontend receives shape-identical between the two paths and
	// avoids leaking chart-repo's wire-internal app_info nesting.
	manifest := types.BuildUserAppManifest(payload.RawDataEx)
	scalars := types.EntryScalars{
		ID:         appID,
		AppID:      appID,
		Version:    appVersion,
		CfgType:    payload.RawDataEx.ConfigType,
		ApiVersion: payload.RawDataEx.APIVersion,
	}
	var appInfo interface{}
	if entry, err := types.ComposeApplicationInfoEntry(manifest, scalars); err != nil {
		glog.Errorf("uploadevent: compose app_info from rendered manifest failed (user=%s source=%s app=%s): %v; falling back to chart-repo app_info",
			userID, sourceID, appID, err)
		appInfo = payload.AppInfo
	} else {
		appInfo = entry
	}

	update := types.MarketSystemUpdate{
		Timestamp:  time.Now().Unix(),
		User:       userID,
		NotifyType: "market_system_point",
		Point:      "new_app_ready",
		Extensions: map[string]string{
			"app_name":    appName,
			"app_version": appVersion,
			"source":      sourceID,
		},
		ExtensionsObj: map[string]interface{}{
			"app_info": appInfo,
		},
	}
	if err := sender.SendMarketSystemUpdate(update); err != nil {
		glog.Errorf("uploadevent: notify new_app_ready failed (user=%s source=%s app=%s): %v",
			userID, sourceID, appID, err)
	}
}
