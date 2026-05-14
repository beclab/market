// Package uploadevent owns the chart-repo "app_upload_completed" event
// projection into Postgres. It is deliberately separate from the
// hydration-driven render pipeline (internal/v2/appinfo/hydrationfn,
// internal/v2/appinfo/pipeline.go) for three reasons:
//
//  1. Payload shape: the upload event delivers chart-repo's full
//     AppInfoLatest payload (raw_data_ex, app_info, i18n, image_analysis,
//     packages) directly, while hydration receives a pending-data wrapper
//     and posts back to /dcr/sync-app to obtain the same payload. The
//     decoders look similar but the decision tree is not.
//
//  2. app_id derivation: the upload event is the canonical place where
//     the per-app, per-source, MD5-derived 8-character app_id is minted
//     (see DeriveAppID). The syncer path receives app_id keys directly
//     from chart-repo's /repo/data response and trusts them verbatim;
//     mixing the two derivations in the same code path would invite
//     drift between writers.
//
//  3. Failure / retry semantics: hydration owns render_consecutive_failures,
//     MarkRenderFailed bookkeeping, and the candidate-driven retry loop.
//     Upload events are fire-once and intentionally do NOT use that
//     retry machinery; on failure we let the next pipeline cycle's
//     ListRenderCandidates pick up the row organically.
//
// Reusable primitives (store.UpsertRenderSuccess, store.MarkRenderFailed,
// types.BuildUserAppManifest, types.EnrichEntranceURLs) ARE shared with
// hydration — they are PG / schema primitives, not pipeline-driven flow.
package uploadevent

import (
	"crypto/md5"
	"encoding/hex"
	"strings"

	"github.com/beclab/Olares/framework/oac"
)

// appIDPrefixLen is the number of hex characters retained from the
// md5 digest. 8 chars = 32 bits of entropy which is enough for the
// per-source uniqueness window (a single source rarely contains more
// than a few hundred apps) and matches the user_applications.app_id
// VARCHAR(32) column comfortably.
const appIDPrefixLen = 8

// DeriveAppID computes the canonical app_id for an uploaded app from
// its raw_data_ex.metadata.appid field. The derivation is
// md5(metadata.appid)[:8] (hex, lowercase) and is shared between
// applications.app_id, user_applications.app_id, and
// user_applications.app_raw_id so the four keys (catalog row + per-user
// row + raw-row identity) line up across the LEFT JOIN in
// store.ListRenderCandidates.
//
// Returns "" when cfg is nil or metadata.appid is empty / whitespace-only.
// Callers must treat that as a hard skip — without a usable appid the
// upload event cannot be projected onto either table.
func DeriveAppID(cfg *oac.AppConfiguration) string {
	if cfg == nil {
		return ""
	}
	rawAppID := strings.TrimSpace(cfg.Metadata.AppID)
	if rawAppID == "" {
		return ""
	}
	sum := md5.Sum([]byte(rawAppID))
	return hex.EncodeToString(sum[:])[:appIDPrefixLen]
}
