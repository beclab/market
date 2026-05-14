package appservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"market/internal/v2/helper"
	"market/internal/v2/types"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"
)

// Client is the interface every consumer of app-service should depend
// on (instead of *httpClient or raw http.Client). The interface is
// intentionally narrow — exactly one method per upstream endpoint
// Market currently calls — so:
//
//   - the surface area is auditable (every method maps 1:1 to a real
//     network call);
//   - tests can mock the client by composing only the methods they
//     exercise (see mock.go);
//   - future endpoints land here as explicit additions, not as a
//     generic catch-all.
//
// All methods take a context.Context as their first argument; the
// context controls request cancellation and deadlines (which override
// the client's default timeout when shorter).
type Client interface {
	// Read endpoints — used by SCC, hydration, status checks. None of
	// them require a per-call token because the upstream endpoints
	// today are unauthenticated cluster-internal calls.
	GetAllApps(ctx context.Context) ([]*types.AppServiceResponse, error)
	GetMiddlewares(ctx context.Context) ([]*types.MiddlewareStatusResponseData, error)
	GetTerminusVersion(ctx context.Context) (string, error)

	// User-info uses the per-user token because app-service decodes
	// the user identity from it. Distinct from the cluster-wide
	// endpoints above.
	GetUserInfo(ctx context.Context, token string) (*UserInfo, error)

	// GetAppEntranceURLs is a convenience that filters GetAllApps()
	// down to one (user, app) and returns just the entrance->URL map.
	// Modeled as its own method (rather than forcing every caller to
	// re-walk the response) because today this is the second-most
	// common access pattern after GetAllApps and warrants its own
	// metric/error surface.
	GetAppEntranceURLs(ctx context.Context, appName, user string) (map[string]string, error)

	// Operation endpoints — each returns an OpResponse with the
	// upstream code and (on success) the OpID that ties this
	// operation to subsequent NATS state messages.
	//
	// On the wire, app-service may answer 200 OK but encode a
	// business failure in the JSON body's Code != 200. The client
	// surfaces that as *OpError (see errors.go) so callers can
	// errors.As and inspect the upstream code/message instead of
	// parsing strings.
	InstallApp(ctx context.Context, appName string, opts InstallOptions, hdr OpHeaders) (*OpResponse, error)
	CloneApp(ctx context.Context, urlAppName string, opts CloneOptions, hdr OpHeaders) (*OpResponse, error)
	UninstallApp(ctx context.Context, appName string, opts UninstallOptions, hdr OpHeaders) (*OpResponse, error)
	UpgradeApp(ctx context.Context, appName string, opts UpgradeOptions, hdr OpHeaders) (*OpResponse, error)
	CancelApp(ctx context.Context, appName string, hdr OpHeaders) (*OpResponse, error)
}

// httpClient is the production implementation of Client backed by
// net/http. It is unexported so callers MUST go through NewClient,
// which guarantees the field invariants (baseURL set, http set,
// metrics non-nil).
type httpClient struct {
	baseURL string        // e.g. "http://app-service.os-platform:80"
	http    *http.Client  // shared across all requests
	timeout time.Duration // default per-request timeout (used when ctx has no deadline)
	metrics Metrics       // never nil; defaults to noopMetrics
}

// NewClient builds a Client. The returned object is safe for
// concurrent use across goroutines.
//
// The base URL is resolved as follows, in order of precedence:
//  1. WithBaseURL(...) Option, if supplied;
//  2. APP_SERVICE_SERVICE_HOST + APP_SERVICE_SERVICE_PORT env vars;
//  3. error ErrConfigMissing.
//
// We resolve env vars at construction time (not per-request) on
// purpose: the K8s service env is set once at pod start; reading it
// per-request just adds syscalls. Tests that need a different host
// should use WithBaseURL directly.
func NewClient(opts ...Option) (Client, error) {
	c := &httpClient{
		timeout: defaultTimeout,
		metrics: noopMetrics{},
	}
	for _, opt := range opts {
		opt(c)
	}

	// Default baseURL from env if no Option set it.
	if c.baseURL == "" {
		host := strings.TrimSpace(os.Getenv(envHost))
		port := strings.TrimSpace(os.Getenv(envPort))
		if host == "" || port == "" {
			return nil, ErrConfigMissing
		}
		c.baseURL = fmt.Sprintf("http://%s:%s", host, port)
	}
	// Trim trailing slash so endpoint methods can append "/path"
	// without worrying about double slashes.
	c.baseURL = strings.TrimRight(c.baseURL, "/")

	if c.http == nil {
		c.http = &http.Client{Timeout: c.timeout}
	}

	return c, nil
}

// =====================================================================
// Read endpoints
// =====================================================================

// GetAllApps fetches the full app inventory from app-service. Used by:
//   - SCC reconciliation (compare PG state vs reality);
//   - StartupReconciler (recover Running tasks);
//   - GetAppEntranceURLs (filter for one app);
//   - utils/setup (initial sync at boot).
//
// Returns []*App so callers can mutate slice elements without
// affecting the upstream cache (we always decode fresh per call).
func (c *httpClient) GetAllApps(ctx context.Context) ([]*types.AppServiceResponse, error) {
	const ep = "get_all_apps"
	body, status, err := c.doRequest(ctx, ep, http.MethodGet, "/app-service/v1/all/apps", nil, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, classifyHTTPStatus(status)
	}

	var apps []*types.AppServiceResponse
	if err := json.Unmarshal(body, &apps); err != nil {
		c.metrics.ObserveRequest(ep, status, 0, errKindDecode)
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return apps, nil
}

// GetMiddlewares fetches the middleware status list. Distinct
// endpoint, distinct response shape from GetAllApps (see Middleware
// in types.go for why they are not unified).
func (c *httpClient) GetMiddlewares(ctx context.Context) ([]*types.MiddlewareStatusResponseData, error) {
	const ep = "get_middlewares"
	body, status, err := c.doRequest(ctx, ep, http.MethodGet, "/app-service/v1/middlewares/status", nil, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, classifyHTTPStatus(status)
	}

	var middlewares []*types.MiddlewareStatusResponseData
	if err := json.Unmarshal(body, &middlewares); err != nil {
		c.metrics.ObserveRequest(ep, status, 0, errKindDecode)
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return middlewares, nil
}

// GetTerminusVersion returns the app-service-reported Terminus
// platform version string. Used by the syncer to attach a version
// query parameter to chart-repo calls; failure to retrieve it falls
// back to a hardcoded default at the call site (see utils.GetTerminusVersionValue).
func (c *httpClient) GetTerminusVersion(ctx context.Context) (string, error) {
	const ep = "get_terminus_version"
	body, status, err := c.doRequest(ctx, ep, http.MethodGet, "/app-service/v1/terminus/version", nil, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", classifyHTTPStatus(status)
	}

	var v VersionInfo
	if err := json.Unmarshal(body, &v); err != nil {
		c.metrics.ObserveRequest(ep, status, 0, errKindDecode)
		return "", fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return v.Version, nil
}

// GetUserInfo resolves the user identity associated with the supplied
// session token. Returns ErrUnauthorized for HTTP 401/403.
func (c *httpClient) GetUserInfo(ctx context.Context, token string) (*UserInfo, error) {
	const ep = "get_user_info"
	headers := map[string]string{
		"X-Authorization": token,
		"Accept":          "*/*",
	}
	body, status, err := c.doRequest(ctx, ep, http.MethodGet, "/app-service/v1/user-info", headers, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, classifyHTTPStatus(status)
	}

	var ui UserInfo
	if err := json.Unmarshal(body, &ui); err != nil {
		c.metrics.ObserveRequest(ep, status, 0, errKindDecode)
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return &ui, nil
}

// GetAppEntranceURLs returns name→URL for each entrance of the
// (user, appName) tuple. Empty map (not error) when the app is not
// found — upstream behaviour is "/all/apps" returns the whole list
// regardless, and absence in that list is the only "not found"
// signal we have at this granularity.
func (c *httpClient) GetAppEntranceURLs(ctx context.Context, appName, user string) (map[string]string, error) {
	apps, err := c.GetAllApps(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string)
	for _, app := range apps {
		if app == nil || app.Spec == nil {
			continue
		}
		if app.Spec.Name != appName || app.Spec.Owner != user {
			continue
		}
		for _, e := range app.Spec.EntranceStatuses {
			if e.Url != "" {
				out[e.Name] = e.Url
			}
		}
		break // an (owner, name) pair is unique in the upstream list
	}
	return out, nil
}

// =====================================================================
// Operation endpoints
// =====================================================================

// InstallApp posts an install request for `appName`. Returns the
// upstream OpResponse (containing the OpID) on success; *OpError when
// app-service returns 200 but Code != 200; ErrAppServiceUnavailable /
// ErrUnauthorized / ErrAppNotFound for transport-level failures.
//
// Note: app-service today GENERATES the OpID itself and returns it in
// OpResponse.Data. If the upstream contract ever evolves to accept a
// caller-supplied OpID (idempotency token), wire it through opts and
// the response code path stays identical.
func (c *httpClient) InstallApp(ctx context.Context, appName string, opts InstallOptions, hdr OpHeaders) (*OpResponse, error) {
	const ep = "install_app"
	path := "/app-service/v1/apps/" + url.PathEscape(appName) + "/install"
	return c.doOp(ctx, ep, "install", path, opts, hdr)
}

// CloneApp posts an install request to a clone-flavoured URL. The
// urlAppName parameter is the synthesized "rawAppName + requestHash"
// path segment app-service uses to disambiguate clones from the
// original app — see task/app_clone.go for why it has to be assembled
// outside the client.
func (c *httpClient) CloneApp(ctx context.Context, urlAppName string, opts CloneOptions, hdr OpHeaders) (*OpResponse, error) {
	const ep = "clone_app"
	path := "/app-service/v1/apps/" + url.PathEscape(urlAppName) + "/install"
	return c.doOp(ctx, ep, "clone", path, opts, hdr)
}

// UninstallApp posts to /apps/{name}/uninstall with the {all,
// deleteData} body app-service expects.
func (c *httpClient) UninstallApp(ctx context.Context, appName string, opts UninstallOptions, hdr OpHeaders) (*OpResponse, error) {
	const ep = "uninstall_app"
	path := "/app-service/v1/apps/" + url.PathEscape(appName) + "/uninstall"
	return c.doOp(ctx, ep, "uninstall", path, opts, hdr)
}

// UpgradeApp posts to /apps/{name}/upgrade with the version + envs +
// images upgrade payload.
func (c *httpClient) UpgradeApp(ctx context.Context, appName string, opts UpgradeOptions, hdr OpHeaders) (*OpResponse, error) {
	const ep = "upgrade_app"
	path := "/app-service/v1/apps/" + url.PathEscape(appName) + "/upgrade"
	return c.doOp(ctx, ep, "upgrade", path, opts, hdr)
}

// CancelApp posts to /apps/{name}/cancel with no body. The "type=
// operate" query parameter is required by app-service to distinguish
// operate-cancel from other cancel kinds; we hardcode it because
// Market only ever issues operate cancels today.
func (c *httpClient) CancelApp(ctx context.Context, appName string, hdr OpHeaders) (*OpResponse, error) {
	const ep = "cancel_app"
	path := "/app-service/v1/apps/" + url.PathEscape(appName) + "/cancel?type=operate"
	return c.doOp(ctx, ep, "cancel", path, nil, hdr)
}

// =====================================================================
// Internal helpers
// =====================================================================

// doOp is the shared body of every operation endpoint. opName is the
// human-readable verb used in OpError; payload may be nil (cancel) and
// is JSON-encoded otherwise.
func (c *httpClient) doOp(ctx context.Context, ep, opName, path string, payload interface{}, hdr OpHeaders) (*OpResponse, error) {

	glog.Infof("[appservice] request path: %s, op: %s, payload: %s, hdr: %s", path, opName, helper.ParseJson(payload), helper.ParseJson(hdr))

	var body io.Reader = nil
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("appservice: marshal %s payload: %w", opName, err)
		}
		body = bytes.NewReader(buf)
	}

	respBody, status, err := c.doRequest(ctx, ep, http.MethodPost, path, opHeadersToMap(hdr), body)
	if err != nil {
		return nil, err
	}

	// Non-200 path. Two sub-shapes possible:
	//   a) Plain-text body (sidecar 5xx, "Internal Server Error\n",
	//      reverse-proxy default pages). The envelope decode fails
	//      silently, op stays zero-valued, and we fall through to
	//      the bare HTTP classification — same as before.
	//   b) Structured business-error envelope. app-service uses this
	//      shape for HTTP 422 + body {code, data} on validation
	//      failures (e.g. install's appenv missingValues schema). We
	//      MUST return the OpResponse so opResp.DataRaw can carry the
	//      structured payload up to the install handler's
	//      backend_response forwarding path, AND surface the error as
	//      *OpError so callers' errors.As(err, &opErr) matches the
	//      same shape as the HTTP-200-business-error branch below.
	//      This makes the two non-success paths symmetric.
	if status != http.StatusOK {
		var op OpResponse
		_ = json.Unmarshal(respBody, &op)

		if op.Code != 0 || op.Message != "" || len(op.DataRaw) > 0 {
			c.metrics.ObserveRequest(ep, status, 0, errKindBusinessError)
			opID := ""
			if op.Data != nil {
				opID = op.Data.OpID
			}
			return &op, &OpError{
				Op:      opName,
				Code:    op.Code,
				Message: op.Message,
				OpID:    opID,
			}
		}
		return nil, classifyHTTPStatus(status)
	}

	// HTTP 200: decode the envelope strictly. A decode failure now
	// indicates real protocol drift — surface ErrInvalidResponse so
	// callers don't confuse it with a transient network blip.
	var op OpResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &op); err != nil {
			c.metrics.ObserveRequest(ep, status, 0, errKindDecode)
			return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
		}
	}

	// HTTP 200 but business code != 200 → typed OpError.
	if op.Code != 0 && op.Code != http.StatusOK {
		c.metrics.ObserveRequest(ep, status, 0, errKindBusinessError)
		opID := ""
		if op.Data != nil {
			opID = op.Data.OpID
		}
		return &op, &OpError{
			Op:      opName,
			Code:    op.Code,
			Message: op.Message,
			OpID:    opID,
		}
	}

	return &op, nil
}

// doRequest is the single place every endpoint goes through to make
// an HTTP call. Centralising here means:
//
//   - Content-Type / Accept defaults are uniform;
//   - timeouts are applied identically;
//   - metrics observation happens exactly once per request;
//   - error classification (network vs timeout vs server) is consistent.
//
// Returns (body, status, err). body is the fully-read response body
// (caller may json.Unmarshal it); status is the HTTP status code.
// err is non-nil only for TRANSPORT failures (network, DNS, timeout,
// body-read error) — a non-2xx HTTP response returns the body+status
// with err == nil so the caller decides whether to error on it.
func (c *httpClient) doRequest(
	ctx context.Context,
	ep string,
	method string,
	path string,
	headers map[string]string,
	body io.Reader,
) (responseBody []byte, status int, err error) {
	start := time.Now()

	// Apply default timeout if the caller's context has no deadline of
	// its own. We only wrap when there is no deadline — otherwise the
	// caller's deadline (which they presumably set for a reason) wins.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, body)

	// if body == nil {
	// 	req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	// } else {

	// }

	if err != nil {
		c.metrics.ObserveRequest(ep, 0, time.Since(start), errKindNetwork)
		return nil, 0, fmt.Errorf("appservice: build request: %w", err)
	}

	// Default headers; per-call headers may override.
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/json")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// Distinguish timeouts from generic network failures so
		// metrics dashboards can spot a slow upstream vs an
		// unreachable one.
		kind := errKindNetwork
		if errors.Is(err, context.DeadlineExceeded) {
			kind = errKindTimeout
		}
		c.metrics.ObserveRequest(ep, 0, time.Since(start), kind)
		return nil, 0, fmt.Errorf("%w: %v", ErrAppServiceUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.metrics.ObserveRequest(ep, resp.StatusCode, time.Since(start), errKindNetwork)
		return nil, resp.StatusCode, fmt.Errorf("appservice: read response: %w", err)
	}

	// Record success/failure status before returning. The business-
	// code branch in doOp records errKindBusinessError on its own.
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		c.metrics.ObserveRequest(ep, resp.StatusCode, time.Since(start), errKindAuth)
	case resp.StatusCode == http.StatusNotFound:
		c.metrics.ObserveRequest(ep, resp.StatusCode, time.Since(start), errKindNotFound)
	case resp.StatusCode >= 500:
		c.metrics.ObserveRequest(ep, resp.StatusCode, time.Since(start), errKindServerError)
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		c.metrics.ObserveRequest(ep, resp.StatusCode, time.Since(start), errKindNone)
	default:
		c.metrics.ObserveRequest(ep, resp.StatusCode, time.Since(start), errKindServerError)
	}

	return respBody, resp.StatusCode, nil
}

// opHeadersToMap projects OpHeaders into the string map doRequest
// consumes. Empty fields are omitted so we don't ship blank header
// values that could confuse middleware between Market and app-service.
func opHeadersToMap(h OpHeaders) map[string]string {
	m := make(map[string]string, 4)
	if h.Token != "" {
		m["X-Authorization"] = h.Token
	}
	if h.User != "" {
		m["X-Bfl-User"] = h.User
	}
	if h.MarketUser != "" {
		m["X-Market-User"] = h.MarketUser
	}
	if h.MarketSource != "" {
		m["X-Market-Source"] = h.MarketSource
	}
	return m
}
