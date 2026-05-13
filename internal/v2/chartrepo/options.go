package chartrepo

import (
	"net/http"
	"time"
)

// Default values used when no Option overrides them. Centralised
// constants make it obvious in code review what every Client gets out
// of the box; changing a default ripples to all call sites that did
// not opt in to a custom value.
const (
	// defaultTimeout is a middle-of-the-road value covering the
	// dominant access pattern (sub-second JSON read/write endpoints).
	// Upload / long sync endpoints SHOULD override this via
	// WithTimeout per-Client, because their latency profile is
	// completely different from a status query.
	defaultTimeout = 30 * time.Second

	// envServiceHost is the env var Market has historically used to
	// discover chart-repo. The value is either a bare host ("chart-
	// repo.os-platform"), a host:port pair, or a full URL. NewClient
	// normalises all three forms into a baseURL ending in no
	// trailing slash and starting with http:// or https://.
	envServiceHost = "CHART_REPO_SERVICE_HOST"

	// pathPrefix is the common URL prefix shared by every endpoint.
	// Kept as a constant so a single relocation (e.g. /api/v3) is a
	// one-line change rather than 12 endpoint methods.
	pathPrefix = "/chart-repo/api/v2"
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
// instead of resolving CHART_REPO_SERVICE_HOST from env. Mainly
// for tests (httptest.Server.URL) and for environments where Market
// runs outside the K8s cluster chart-repo is in.
//
// The value may include or omit the scheme (http:// is added when
// missing), and may include or omit a trailing slash (it is stripped
// either way). It must NOT include the /chart-repo/api/v2 prefix —
// that is appended by every endpoint method internally.
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
