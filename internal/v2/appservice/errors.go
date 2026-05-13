// Package appservice provides a typed HTTP client for talking to the
// upstream app-service. The package centralises every Market call site
// that today reads APP_SERVICE_SERVICE_HOST/PORT and crafts a one-off
// http.Client; consolidating here gives us one place to set timeout,
// classify errors, attach metrics, and swap out the transport.
//
// This file owns the error vocabulary the client surfaces. Callers do
// NOT see raw http.Client errors — every failure path returns a typed
// sentinel (use errors.Is) or an *OpError carrying the upstream code/
// message. This contract is the only sustainable way for a dozen call
// sites to make consistent decisions ("is this retryable? did the user
// hit auth? is the app simply missing?") without duplicating
// status-code parsing in every business module.
package appservice

import (
	"errors"
	"fmt"
)

// Sentinel errors. Compare with errors.Is; the wrapped values from the
// client preserve the underlying transport error via fmt.Errorf("%w",
// err) when useful, so callers can still drill down for diagnostics if
// they want.
var (
	// ErrConfigMissing is returned by NewClient when the host/port
	// cannot be discovered (env vars empty AND WithBaseURL not used).
	// Distinct from a network failure because the fix is configuration,
	// not retry.
	ErrConfigMissing = errors.New("appservice: HOST/PORT not configured")

	// ErrAppServiceUnavailable wraps any failure that is most likely
	// transient: connection refused, DNS failure, 5xx response, request
	// timeout from the underlying http.Client. Callers that want to
	// retry should gate on errors.Is(err, ErrAppServiceUnavailable).
	ErrAppServiceUnavailable = errors.New("appservice: service unavailable")

	// ErrAppNotFound is returned for HTTP 404 on an app/middleware
	// lookup or operation. Distinct from "service down" so the API
	// layer can map it to a proper 404 instead of a generic 500.
	ErrAppNotFound = errors.New("appservice: app not found")

	// ErrUnauthorized is returned for HTTP 401/403. The caller is
	// expected to refresh the token (or fail the request).
	ErrUnauthorized = errors.New("appservice: unauthorized")

	// ErrInvalidResponse is returned when the upstream replies with a
	// 2xx status but a body that cannot be decoded into the expected
	// shape. Almost always indicates an upstream protocol drift; the
	// fix is updating the client types, not retrying.
	ErrInvalidResponse = errors.New("appservice: invalid response body")
)

// OpError is the error returned for "successful HTTP, but the upstream
// said code != 200 in the JSON body". Operation endpoints (install /
// uninstall / upgrade / cancel) wrap their business failures this way
// and the caller can errors.As it to inspect the original Code/Message
// before deciding what to surface to the end user.
//
// OpID is included when present so that even failure paths can be
// correlated with a NATS event stream (some app-service failure
// responses still ship an OpID for the partial state).
type OpError struct {
	Op      string // "install" / "uninstall" / "upgrade" / "cancel"
	Code    int    // upstream business code (NOT HTTP status)
	Message string
	OpID    string
}

func (e *OpError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.OpID != "" {
		return fmt.Sprintf("appservice: %s op_id=%s code=%d: %s", e.Op, e.OpID, e.Code, e.Message)
	}
	return fmt.Sprintf("appservice: %s code=%d: %s", e.Op, e.Code, e.Message)
}

// classifyHTTPStatus maps a transport-level status code to one of the
// sentinel errors. Centralised so every endpoint method does the same
// classification — the moment one method diverges we lose the "callers
// can errors.Is uniformly" guarantee.
func classifyHTTPStatus(status int) error {
	switch {
	case status == 401 || status == 403:
		return ErrUnauthorized
	case status == 404:
		return ErrAppNotFound
	case status >= 500:
		return ErrAppServiceUnavailable
	default:
		return fmt.Errorf("appservice: unexpected HTTP status %d", status)
	}
}
