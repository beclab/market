package appservice

import "time"

// Metrics is the observation hook the client emits to. It is a small
// interface (one method per "category of event") so that integrators
// can wire it to Prometheus, OpenTelemetry, or anything else without
// the appservice package taking a hard dependency on a specific stack.
//
// The default implementation (noopMetrics) discards everything, which
// keeps the package zero-dependency for callers who do not care about
// metrics yet. Callers that DO care should pass WithMetrics(...) into
// NewClient with their own implementation.
//
// All methods MUST be safe to call concurrently (they are invoked from
// every request goroutine).
type Metrics interface {
	// ObserveRequest is called for every completed HTTP request,
	// successful or not. duration is the wall-clock time from "send
	// request" to "fully read body". status is the HTTP status code
	// (0 if the transport itself errored before getting a response).
	// errKind is one of "" (success), "network", "server_error",
	// "auth", "not_found", "business_error", "decode" — a small fixed
	// vocabulary so the observer can build labels with bounded
	// cardinality.
	ObserveRequest(endpoint string, status int, duration time.Duration, errKind string)
}

// noopMetrics is the zero-cost default. Storing this as the field
// value means endpoint methods can call s.metrics.ObserveRequest(...)
// without nil-checking, and the compiler will inline the empty body.
type noopMetrics struct{}

func (noopMetrics) ObserveRequest(_ string, _ int, _ time.Duration, _ string) {}

// errKind constants kept here so producers (the client) and consumers
// (any Metrics impl) agree on the exact label vocabulary. Adding a new
// kind requires updating both ends; that intentional friction prevents
// label-cardinality explosions in downstream metrics systems.
const (
	errKindNone          = "" // success
	errKindNetwork       = "network"
	errKindTimeout       = "timeout"
	errKindServerError   = "server_error"
	errKindAuth          = "auth"
	errKindNotFound      = "not_found"
	errKindBusinessError = "business_error"
	errKindDecode        = "decode"
)
