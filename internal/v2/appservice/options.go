package appservice

import (
	"net/http"
	"time"
)

// Default values used when no Option overrides them. Centralised
// constants make it obvious in code review what every Client gets out
// of the box; changing a default ripples to all call sites that did
// not opt in to a custom value.
const (
	// defaultTimeout matches the legacy 30s used in setup.go and
	// systeminfo.go. Operation endpoints (install / uninstall) may
	// override this via WithTimeout because their latency profile is
	// different from a status query.
	defaultTimeout = 30 * time.Second

	// envHost / envPort are the env vars Market has historically used
	// to discover app-service. Kept as constants so a future relocation
	// (e.g. a config file) is a one-line change.
	envHost = "APP_SERVICE_SERVICE_HOST"
	envPort = "APP_SERVICE_SERVICE_PORT"
)

// Option is the functional-options knob for NewClient. We use this
// pattern instead of an exported Config struct because:
//
//   - it lets future fields land without breaking the NewClient
//     signature (additive only);
//   - it nudges callers toward "pass only what you actually need to
//     override" rather than "fill in a 12-field struct with mostly
//     zero values".
//
// Options run in order, so a later WithXxx overrides an earlier one.
// The base options resolved from env vars run first inside NewClient,
// so user-supplied options always win.
type Option func(*httpClient)

// WithBaseURL forces the client to talk to a specific URL prefix
// instead of resolving APP_SERVICE_SERVICE_HOST/PORT from env. Mainly
// for tests (httptest.Server.URL) and for environments where Market
// runs outside the K8s cluster app-service is in.
//
// The value should NOT include a trailing slash; the client appends
// each endpoint path with its own leading slash.
func WithBaseURL(url string) Option {
	return func(c *httpClient) {
		c.baseURL = url
	}
}

// WithTimeout replaces the default request timeout. Applies to the
// underlying http.Client as Client.Timeout, which covers connect +
// request + body read.
//
// A timeout of zero is treated as "no timeout" by Go's http.Client.
// We do not special-case it here (caller's responsibility); but
// passing 0 in production is a footgun — keep the default unless you
// have a strong reason.
func WithTimeout(d time.Duration) Option {
	return func(c *httpClient) {
		c.timeout = d
		// If a custom http.Client was injected via WithHTTPClient we do
		// NOT mutate it here — the caller owns its configuration. The
		// timeout still gets stamped on per-request contexts in the
		// endpoint methods.
	}
}

// WithHTTPClient lets the caller plug in their own *http.Client. Used
// by tests (to share a recorded transport) and potentially by future
// code that wants to layer in tracing, retry, or connection pool
// tuning at the transport level.
//
// The supplied client is used as-is — its Timeout, Transport, etc. are
// not modified. If the caller wants the client to honour
// WithTimeout/etc, they should configure those on the http.Client
// before passing it in.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *httpClient) {
		c.http = hc
	}
}

// WithMetrics installs a custom metrics sink. Defaults to a no-op sink
// inside NewClient when not supplied; that is intentional so the
// package has zero hard dependency on Prometheus / OpenTelemetry while
// still letting integrators wire in real metrics where they care.
//
// See metrics.go for the Metrics interface and the noop default.
func WithMetrics(m Metrics) Option {
	return func(c *httpClient) {
		c.metrics = m
	}
}
