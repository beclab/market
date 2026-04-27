// Package payload contains the typed Go structs that back JSONB columns on
// the database models for which no business-domain struct exists yet (or
// for which keeping the on-disk schema decoupled from the API type is
// intentional).
//
// JSONB columns whose payload is already modelled in market/internal/v2/types
// reference those types directly from the model files via JSONB[types.XXX];
// they do NOT live here.
//
// The structs below are intentionally left as empty starting points. Add
// fields as the corresponding business logic lands; reads/writes are
// transparently handled by db/models.JSONB[T].
package payload

// ManifestMetadata backs user_applications.metadata (icon, title, categories,
// version, ... — whatever the OlaresManifest "metadata" block contains).
type ManifestMetadata struct{}

// ManifestSpec backs user_applications.spec (developer, website, description,
// license, locale, ... — everything from OlaresManifest "spec" except the
// resource limit fields, which live in Resources).
type ManifestSpec struct{}

// Resources backs user_applications.resources (requiredMemory, limitedMemory,
// requiredCpu, limitedCpu, requiredGpu, limitedGpu, requiredDisk, limitedDisk,
// supportArch).
type Resources struct{}

// Options backs user_applications.options (dependencies, conflicts, policies,
// allowedOutboundPorts, appScope, ...).
type Options struct{}

// Entrance backs a single element of user_applications.entrances and
// user_applications.shared_entrances. Manifest-side fields only (name, host,
// port, title, icon, openMethod, authLevel, invisible).
type Entrance struct{}

// Port backs a single element of user_applications.ports (network port
// exposure: name, host, port, protocol, exposePort, ...).
type Port struct{}

// TailscaleSpec backs user_applications.tailscale.
type TailscaleSpec struct{}

// Permission backs user_applications.permission (appData, appCache, userData,
// sysData, provider).
type Permission struct{}

// Middleware backs user_applications.middleware (redis, postgres, ... config).
type Middleware struct{}

// Env backs a single element of user_applications.envs.
type Env struct{}

// RuntimeEntrance backs user_application_states.entrances and
// user_application_states.shared_entrances. Same shape as the manifest-side
// Entrance plus the URL assigned by the cluster after install / upgrade.
type RuntimeEntrance struct{}

// StatusEntrances backs user_application_states.status_entrances. Stored as
// a JSON object (not an array) — the exact shape is defined by the
// upstream NATS message and is left empty here until the wire format is
// finalised.
type StatusEntrances struct{}
