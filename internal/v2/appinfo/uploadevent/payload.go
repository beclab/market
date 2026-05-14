package uploadevent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"market/internal/v2/chartrepo"
)

// ErrPayloadUnusable is returned by DecodeUploadPayload when the
// chart-repo /apps response does not carry the minimum data we need
// to write either the applications row or the user_applications row.
// It is a sentinel rather than a wrapped error because the handler
// turns this case into a structured warning log + early-return,
// distinct from genuine decode failures (malformed JSON, unexpected
// type) that propagate as ordinary errors.
var ErrPayloadUnusable = errors.New("upload payload missing raw_data_ex / metadata.appid")

// DecodeUploadPayload converts the chart-repo /apps map response into
// a typed *chartrepo.SyncAppPayload via a marshal / unmarshal round-trip.
// The two have identical wire shapes — chart-repo emits the same
// AppInfoLatest envelope from both /apps (event-handler caller) and
// /dcr/sync-app (hydration caller) — so the round-trip is safe and
// keeps the rest of this package working against a typed value rather
// than an untyped map.
//
// Returns:
//   - (*payload, nil)             on a fully-decoded, usable response;
//   - (nil, ErrPayloadUnusable)   when the response decodes but lacks
//     either raw_data_ex or metadata.appid — the caller should log
//     and drop the event because no usable app_id can be derived;
//   - (nil, error)                on a real marshal/unmarshal failure.
//
// Validation kept here:
//   - raw_data_ex is required (we need its typed metadata to compute
//     app_id and to feed BuildUserAppManifest);
//   - metadata.appid is required (it is the MD5 input for app_id);
//   - everything else is optional — empty i18n / image_analysis /
//     packages are valid pass-through (downstream writers handle nils).
func DecodeUploadPayload(in map[string]interface{}) (*chartrepo.SyncAppPayload, error) {
	if in == nil {
		return nil, ErrPayloadUnusable
	}
	buf, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("marshal upload payload: %w", err)
	}
	out := &chartrepo.SyncAppPayload{}
	if err := json.Unmarshal(buf, out); err != nil {
		return nil, fmt.Errorf("unmarshal upload payload: %w", err)
	}
	if out.RawDataEx == nil {
		return nil, ErrPayloadUnusable
	}
	if strings.TrimSpace(out.RawDataEx.Metadata.AppID) == "" {
		return nil, ErrPayloadUnusable
	}
	return out, nil
}
