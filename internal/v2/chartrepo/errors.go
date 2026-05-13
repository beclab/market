// Package chartrepo provides a typed HTTP client for talking to the
// upstream chart-repo service. The package centralises every Market
// call site that today reads CHART_REPO_SERVICE_HOST and crafts a
// one-off http.Client; consolidating here gives us one place to set
// timeout, classify errors, attach metrics, and swap out the transport.
//
// Modelled directly on internal/v2/appservice (see that package's docs
// for the same architectural rationale). Key differences vs appservice:
//
//   - chart-repo is unauthenticated today, so there is no per-call
//     token / OpHeaders construct.
//   - chart-repo's responses are almost universally wrapped in the
//     {success, message, data} envelope, so the client surfaces
//     business failures (success=false) as a typed *BusinessError that
//     callers can errors.As to inspect message/data without parsing
//     strings.
//   - One endpoint (UploadChart) uses multipart/form-data; the rest are
//     application/json. We expose a single multipart helper alongside
//     the JSON path rather than generalising the body builder, because
//     the multipart caller is a single endpoint and a generalisation
//     would hide more than it abstracts.
//
// This file owns the error vocabulary. Callers do NOT see raw
// http.Client errors — every failure path returns a typed sentinel
// (use errors.Is) or a *BusinessError. This contract is the only
// sustainable way for a dozen call sites to make consistent retry /
// "user-vs-server" decisions without duplicating status-code parsing.
package chartrepo

import (
	"errors"
	"fmt"
)

// Sentinel errors. Compare with errors.Is; the wrapped values from
// the client preserve the underlying transport error via fmt.Errorf
// ("%w", err) when useful, so callers can still drill down for
// diagnostics if they want.
var (
	// ErrConfigMissing is returned by NewClient when the host cannot
	// be discovered (env var empty AND WithBaseURL not used).
	// Distinct from a network failure because the fix is configuration,
	// not retry.
	ErrConfigMissing = errors.New("chartrepo: CHART_REPO_SERVICE_HOST not configured")

	// ErrChartRepoUnavailable wraps any failure that is most likely
	// transient: connection refused, DNS failure, 5xx response, request
	// timeout from the underlying http.Client. Callers that want to
	// retry should gate on errors.Is(err, ErrChartRepoUnavailable).
	ErrChartRepoUnavailable = errors.New("chartrepo: service unavailable")

	// ErrNotFound is returned for HTTP 404 on any endpoint. Distinct
	// from "service down" so the API layer can map it to a proper 404
	// instead of a generic 500.
	ErrNotFound = errors.New("chartrepo: resource not found")

	// ErrUnauthorized is returned for HTTP 401/403. Most chart-repo
	// endpoints are unauthenticated today, but UploadChart /
	// DeleteLocalApp do forward an X-Authorization token, so we
	// surface this distinction in case upstream tightens auth later.
	ErrUnauthorized = errors.New("chartrepo: unauthorized")

	// ErrInvalidResponse is returned when the upstream replies with a
	// 2xx status but a body that cannot be decoded into the expected
	// shape. Almost always indicates an upstream protocol drift; the
	// fix is updating the client types, not retrying.
	ErrInvalidResponse = errors.New("chartrepo: invalid response body")
)

// BusinessError is the error returned for "successful HTTP, but the
// upstream envelope said success=false". Most chart-repo endpoints
// wrap their replies in {success, message, data}; on success=false the
// client surfaces *BusinessError so callers can errors.As it and
// inspect the original message before deciding what to surface to the
// end user.
//
// Endpoint is the verb / path identifier used in the log line, kept
// short and bounded (matches the metric `endpoint` label vocabulary)
// so error strings stay grep-able.
type BusinessError struct {
	Endpoint string // e.g. "add_market_source", "get_state_changes"
	Message  string // upstream "message" field, verbatim
}

func (e *BusinessError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message == "" {
		return fmt.Sprintf("chartrepo: %s reported success=false", e.Endpoint)
	}
	return fmt.Sprintf("chartrepo: %s reported success=false: %s", e.Endpoint, e.Message)
}

// classifyHTTPStatus maps a transport-level status code to one of the
// sentinel errors. Centralised so every endpoint method does the same
// classification — the moment one method diverges we lose the
// "callers can errors.Is uniformly" guarantee.
func classifyHTTPStatus(status int) error {
	switch {
	case status == 401 || status == 403:
		return ErrUnauthorized
	case status == 404:
		return ErrNotFound
	case status >= 500:
		return ErrChartRepoUnavailable
	default:
		return fmt.Errorf("chartrepo: unexpected HTTP status %d", status)
	}
}
