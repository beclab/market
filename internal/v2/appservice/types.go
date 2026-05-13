package appservice

import (
	"encoding/json"

	"market/internal/v2/types"
)

// App is the canonical representation of one app entry returned by
// app-service. The shape mirrors the legacy utils.AppServiceResponse so
// that migrating call sites is purely mechanical (rename + import); the
// nested Spec/Status reuse types from internal/v2/types to avoid
// re-declaring deeply nested structures and to keep wire compat with
// the rest of the codebase that already understands them.
//
// Pointer fields (Spec / Status) reflect the upstream contract where
// either may legitimately be absent — a rendered-but-not-yet-running
// app has Spec without Status, an externally-deleted app may surface
// the inverse during a brief window. Callers must nil-check before
// dereferencing.
type App struct {
	Metadata AppMetadata               `json:"metadata"`
	Spec     *AppSpec                  `json:"spec"`
	Status   *types.AppStateLatestDataStatus `json:"status"`
}

// AppMetadata is the K8s-style metadata block app-service places on
// every resource. UID is the K8s object UID, useful for diagnostic
// correlation only — Market does not use it as a primary key.
type AppMetadata struct {
	Name      string `json:"name"`
	UID       string `json:"uid"`
	Namespace string `json:"namespace"`
}

// AppSpec extends the embedded SpecMetadata with entrance-related
// blocks. The Settings pointer is intentionally optional so app-service
// payloads without per-app settings (older versions, middleware) decode
// cleanly without spurious empty objects.
type AppSpec struct {
	types.AppStateLatestDataSpecMetadata
	EntranceStatuses []types.AppStateLatestDataEntrances `json:"entrances"`
	SharedEntrances  []types.AppStateLatestDataEntrances `json:"sharedEntrances,omitempty"`
	Settings         *types.AppStateLatestDataSettings   `json:"settings,omitempty"`
}

// Middleware is the response shape from /app-service/v1/middlewares/status.
// It is deliberately distinct from App because the upstream JSON differs
// (resourceStatus / resourceType / version live at the top level here,
// whereas App nests them under Status). Modeling them separately keeps
// each codec straightforward; collapsing to a shared struct would force
// every reader to remember which fields are valid for which kind.
type Middleware struct {
	UUID           string             `json:"uuid"`
	Namespace      string             `json:"namespace"`
	User           string             `json:"user"`
	ResourceStatus string             `json:"resourceStatus"`
	ResourceType   string             `json:"resourceType"`
	CreateTime     string             `json:"createTime"`
	UpdateTime     string             `json:"updateTime"`
	Metadata       MiddlewareMetadata `json:"metadata"`
	Version        string             `json:"version"`
	Title          string             `json:"title"`
}

// MiddlewareMetadata mirrors the K8s metadata object app-service nests
// inside each middleware row. We extract just Name today; if more
// fields become relevant, add them here rather than fishing into a
// map[string]interface{}.
type MiddlewareMetadata struct {
	Name string `json:"name"`
}

// UserInfo is the projection of /app-service/v1/user-info that Market
// actually consumes. The upstream payload carries more (Avatar etc.)
// but we deliberately do not surface fields nobody reads — adding a
// field here is the explicit signal that the codebase started caring
// about it.
type UserInfo struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Email    string `json:"email"`
}

// VersionInfo is the response from /app-service/v1/terminus/version.
// Plain envelope; the version string itself is what callers want.
type VersionInfo struct {
	Version string `json:"version"`
}

// ----------------------------------------------------------------------
// Operation request bodies
// ----------------------------------------------------------------------
//
// All operation endpoints (install / uninstall / upgrade / cancel /
// clone) take a different request shape; we model each as its own
// struct rather than a single "OpRequest" union because:
//
//   - the JSON wire format is different per endpoint (uninstall takes
//     {all, deleteData}; install takes a full InstallOptions; cancel
//     takes nothing in the body but uses a query string);
//   - Go does not handle "tagged union" cleanly without sum types, and
//     using a generic map[string]interface{} would defeat the purpose
//     of having a typed client.
//
// The headers required for each (Token / X-Bfl-User / X-Market-User /
// X-Market-Source) are factored into the shared OpHeaders so the
// caller assembles them once and the client method body does not need
// to know which subset each endpoint actually uses.

// OpHeaders carries the common per-operation HTTP headers that
// app-service expects on install / uninstall / upgrade / cancel /
// clone. Token is the user's session token (sent as X-Authorization);
// User is the local Olares user account (sent as X-Bfl-User); the two
// X-Market-* fields are read by app-service to attribute the operation
// back to the originating market source for observability.
//
// Token + User are mandatory for every operation today. The X-Market-*
// pair is optional in the wire protocol (older app-service versions
// ignored them) but Market always populates them when known so that
// downstream attribution does not silently break.
type OpHeaders struct {
	Token        string // → X-Authorization
	User         string // → X-Bfl-User
	MarketUser   string // → X-Market-User
	MarketSource string // → X-Market-Source
}

// InstallOptions is the JSON payload for /apps/{name}/install. Mirrors
// the legacy task.InstallOptions field-by-field; future migrations can
// drop the duplicate over there once every call site uses this client.
//
// The Envs field is REQUIRED by app-service even when empty (the
// upstream contract treats the absence of "envs" as malformed) — that
// is why it does not carry omitempty.
type InstallOptions struct {
	App          string      `json:"appName,omitempty"`
	Dev          bool        `json:"devMode,omitempty"`
	RepoUrl      string      `json:"repoUrl,omitempty"`
	CfgUrl       string      `json:"cfgUrl,omitempty"`
	Version      string      `json:"version,omitempty"`
	Source       string      `json:"source,omitempty"`
	User         string      `json:"x_market_user,omitempty"`
	MarketSource string      `json:"x_market_source,omitempty"`
	Images       []Image     `json:"images,omitempty"`
	Envs         []AppEnvVar `json:"envs"`
}

// CloneOptions extends InstallOptions with the clone-specific fields
// (RawAppName / RequestHash / Title / Entrances). It is a separate
// type because the URL path itself differs (clone hits the install
// endpoint with a synthesized "rawAppName+hash" path segment) and
// because future divergence between install and clone payloads should
// not bleed back into the install struct.
type CloneOptions struct {
	App          string        `json:"appName,omitempty"`
	Dev          bool          `json:"devMode,omitempty"`
	RepoUrl      string        `json:"repoUrl,omitempty"`
	CfgUrl       string        `json:"cfgUrl,omitempty"`
	Version      string        `json:"version,omitempty"`
	Source       string        `json:"source,omitempty"`
	User         string        `json:"x_market_user,omitempty"`
	MarketSource string        `json:"x_market_source,omitempty"`
	Images       []Image       `json:"images,omitempty"`
	Envs         []AppEnvVar   `json:"envs"`
	Entrances    []AppEntrance `json:"entrances,omitempty"`
	RawAppName   string        `json:"rawAppName,omitempty"`
	RequestHash  string        `json:"requestHash,omitempty"`
	Title        string        `json:"title,omitempty"`
}

// UpgradeOptions covers the upgrade request body. Source/MarketSource
// remain because app-service uses them to decide which chart-repo to
// pull from; older versions accepted upgrades without those fields, so
// callers populating only the strictly-required ones (Version) will
// still work against a recent app-service.
type UpgradeOptions struct {
	RepoUrl      string      `json:"repoUrl,omitempty"`
	Version      string      `json:"version,omitempty"`
	User         string      `json:"x_market_user,omitempty"`
	Source       string      `json:"source,omitempty"`
	MarketSource string      `json:"x_market_source,omitempty"`
	Images       []Image     `json:"images,omitempty"`
	Envs         []AppEnvVar `json:"envs"`
}

// UninstallOptions is the small body app-service expects on
// /apps/{name}/uninstall. Both flags default to false on the wire,
// matching the legacy behaviour where missing fields meant "do not
// cascade / do not delete data".
type UninstallOptions struct {
	All        bool `json:"all"`
	DeleteData bool `json:"deleteData"`
}

// AppEnvVar / AppEnvValueFrom are the env shapes app-service accepts
// on install / upgrade. They mirror the K8s-style EnvVar but the JSON
// schema is upstream-defined, so do NOT swap to corev1.EnvVar even
// though it looks similar — the wire fields differ.
type AppEnvVar struct {
	EnvName   string           `json:"envName" yaml:"envName" validate:"required"`
	Value     string           `json:"value,omitempty" yaml:"value,omitempty"`
	ValueFrom *AppEnvValueFrom `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`
}

type AppEnvValueFrom struct {
	EnvName string `json:"envName,omitempty" yaml:"envName,omitempty"`
}

// Image is one container image that app-service should pre-pull as
// part of the install/upgrade flow. Size is in bytes (used by
// app-service for progress reporting, not by Market).
type Image struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// AppEntrance is the entrance descriptor used in clone install
// requests. Distinct from types.AppStateLatestDataEntrances because
// here we only set Name + Title — the rest of the entrance state
// (URL, port, status) is decided by app-service, not the caller.
type AppEntrance struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// ----------------------------------------------------------------------
// Operation responses
// ----------------------------------------------------------------------

// OpResponse is the JSON envelope app-service returns for every
// operation endpoint (install/uninstall/upgrade/cancel/clone). Code is
// the upstream business code (200 = success, anything else = failure
// even when HTTP was 200); Data carries the OpID that ties this
// operation to subsequent NATS state events.
//
// We intentionally surface Code as int (not a typed enum) because
// app-service does not document the full code space; preserving the
// raw value lets diagnostic logs print whatever app-service sent
// without lossy translation.
//
// DataRaw preserves the original bytes of the `data` field. Two
// scenarios make it necessary:
//
//  1. Business-error payloads. On HTTP 200 + Code != 200, app-service
//     may return a `data` block whose shape does NOT match OpResult,
//     e.g. install's 422 response carries
//     `data: {"type":"appenv","Data":{"missingValues":[...],...}}`.
//     The Data field above can't represent that — its strict OpResult
//     type silently drops the structured payload. DataRaw captures the
//     bytes verbatim so callers (today: app_install / app_upgrade /
//     ... in internal/v2/task) can forward them to the API layer's
//     `backend_response.data` without a lossy interface{} → map →
//     marshal round-trip that would also lose field ordering and
//     case sensitivity (the upstream literally uses both `data` and
//     `Data` as keys at adjacent nesting levels).
//
//  2. Forward compatibility on the success path. When app-service
//     starts emitting additional fields alongside `opID` (warnings,
//     hints, ...), DataRaw preserves them automatically without
//     requiring an OpResult schema change.
//
// DataRaw is populated by the custom UnmarshalJSON below; the
// json:"-" tag suppresses the field on the marshal side so a re-
// serialised OpResponse keeps the wire shape clean.
type OpResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message,omitempty"`
	Data    *OpResult       `json:"data,omitempty"`
	DataRaw json.RawMessage `json:"-"`
}

// UnmarshalJSON parses the upstream envelope and, in a single pass,
// (a) stores the raw bytes of the `data` field on DataRaw and (b) makes
// a best-effort decode of the same bytes into OpResult so the legacy
// `opResp.Data.OpID` access pattern keeps working unchanged.
//
// "Best effort" because OpResult only declares the `opID` field — Go's
// json.Unmarshal tolerates unknown keys without error, so a 422
// appenv-shaped payload still produces a non-nil Data (with OpID="")
// rather than a hard parse failure. Callers therefore continue to
// guard on `opResp.Data != nil && opResp.Data.OpID != ""` exactly as
// before, and consult DataRaw for the structured business-error body.
//
// Why a custom UnmarshalJSON rather than fixing doOp:
//
//   - Encapsulates the dual decoding inside the type. Every consumer
//     (live callers + tests + future endpoints) gets the behaviour
//     automatically, without each having to remember to capture raw
//     bytes side-by-side.
//   - doOp stays free of envelope-decoding details and keeps its
//     focus on HTTP transport classification.
func (r *OpResponse) UnmarshalJSON(b []byte) error {
	type alias struct {
		Code    int             `json:"code"`
		Message string          `json:"message,omitempty"`
		Data    json.RawMessage `json:"data,omitempty"`
	}
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	r.Code = a.Code
	r.Message = a.Message
	// `null` and empty are normalised to "no data" so callers can use
	// `len(opResp.DataRaw) > 0` as the sentinel without further string
	// comparisons against the literal "null".
	if len(a.Data) > 0 && string(a.Data) != "null" {
		r.DataRaw = a.Data
		var or OpResult
		// Unknown fields are tolerated; the decode succeeds with
		// OpID="" when the upstream `data` does not carry an opID
		// (typical for business-error payloads).
		if err := json.Unmarshal(a.Data, &or); err == nil {
			r.Data = &or
		}
	}
	return nil
}

// OpResult is the inner block of OpResponse that carries the OpID. We
// model it as a pointer because some OpResponse paths legitimately omit
// "data" entirely (e.g. validation rejection), and a nil here is the
// cleanest way to encode that absence without inventing a sentinel
// "empty OpID = absent" rule.
type OpResult struct {
	OpID string `json:"opID"`
}
