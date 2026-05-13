// Package payload contains typed Go structs that back JSONB columns on
// the database models for which no business-domain struct exists yet (or
// for which keeping the on-disk schema decoupled from the API type is
// intentional).
//
// JSONB columns whose payload is already modelled in market/internal/v2/types
// reference those types directly from the model files via JSONB[types.XXX];
// they do NOT live here.
//
// Note on user_applications: the eleven OlaresManifest blocks
// (metadata / spec / resources / options / entrances / shared_entrances /
// ports / tailscale / permission / middleware / envs) used to be backed by
// empty placeholder structs in this package. They are now stored as
// catch-all map[string]any payloads so chart-repo schema additions flow
// through without model changes — see internal/v2/types/manifest.go.
// Only user_application_states still keeps its placeholder types here.
package payload

// RuntimeEntrance backs user_application_states.entrances and
// user_application_states.shared_entrances. Same shape as the manifest-side
// Entrance plus the URL assigned by the cluster after install / upgrade.
type RuntimeEntrance struct{}

// StatusEntrance backs one element of user_application_states.status_entrances.
// The column itself is a JSON array of objects (one element per entrance)
// matching the upstream NATS msg.entranceStatuses shape; the exact field
// set is left empty here until the wire format is finalised.
type StatusEntrance struct{}
