package chartrepo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"market/internal/v2/helper"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"
)

// Client is the interface every consumer of chart-repo should depend
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
	// Health
	GetVersion(ctx context.Context) (*VersionInfo, error)

	// Market sources — chart-repo's parallel store. Today writes go
	// here and to PG; reads should prefer PG (see settings/manager.go
	// GetMarketSource docstring).
	GetMarketSources(ctx context.Context) (*MarketSourcesConfig, error)
	AddMarketSource(ctx context.Context, src *MarketSource) error
	DeleteMarketSource(ctx context.Context, sourceID string) error

	// Data plane — used by datawatcher_repo and the cache correction
	// path in appinfomodule.
	GetRepoData(ctx context.Context) (*RepoData, error)
	GetStateChanges(ctx context.Context, afterID int64, limit int) (*StateChangesData, error)
	GetAppInfo(ctx context.Context, in AppsRequest) ([]map[string]interface{}, error)
	GetImageInfo(ctx context.Context, imageName string) (map[string]any, error)

	// Download history — single-row lookup used by the install
	// history reconciliation path.
	GetAppVersionForDownloadHistory(ctx context.Context, userID, appName string) (version, source string, _ error)

	// Local chart management — multipart upload + JSON delete.
	UploadChart(ctx context.Context, in UploadChartInput) (*UploadChartResponseData, error)
	DeleteLocalApp(ctx context.Context, in DeleteLocalAppRequest, token, userID string) error

	// Hydration — chart-repo's per-app render entry point.
	SyncApp(ctx context.Context, req SyncAppRequest) (*SyncAppData, error)

	// Runtime status — aggregate view collector uses.
	GetStatus(ctx context.Context, userID, sourceID string, include []StatusInclude) (*Status, error)
}

// httpClient is the production implementation of Client backed by
// net/http. It is unexported so callers MUST go through NewClient,
// which guarantees the field invariants (baseURL set, http set,
// metrics non-nil).
type httpClient struct {
	baseURL string        // e.g. "http://chart-repo.os-platform:8080"
	http    *http.Client  // shared across all requests
	timeout time.Duration // default per-request timeout (used when ctx has no deadline)
	metrics Metrics       // never nil; defaults to noopMetrics
}

// NewClient builds a Client. The returned object is safe for
// concurrent use across goroutines.
//
// The base URL is resolved as follows, in order of precedence:
//  1. WithBaseURL(...) Option, if supplied;
//  2. CHART_REPO_SERVICE_HOST env var (normalised — see normaliseBaseURL);
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

	if c.baseURL == "" {
		host := strings.TrimSpace(os.Getenv(envServiceHost))
		if host == "" {
			return nil, ErrConfigMissing
		}
		c.baseURL = host
	}
	c.baseURL = normaliseBaseURL(c.baseURL)

	if c.http == nil {
		c.http = &http.Client{Timeout: c.timeout}
	}

	return c, nil
}

// normaliseBaseURL coerces every form callers historically used into a
// canonical scheme://host[:port] form with no trailing slash. The
// legacy callsites variously fed in:
//
//   - bare host:       "chart-repo.os-platform"
//   - host:port:       "chart-repo.os-platform:8080"
//   - full URL:        "http://chart-repo:8080"
//   - misconfigured:   "http://localhost:8080" with code that then
//                      prepended "http://" again — yielding "http://
//                      http://localhost:8080/...". Centralising the
//                      normalisation eliminates that long-standing
//                      bug class once Stage 2 migrations land.
func normaliseBaseURL(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "http://" + s
	}
	return strings.TrimRight(s, "/")
}

// =====================================================================
// Endpoint implementations
// =====================================================================

// GetVersion fetches chart-repo's build metadata via /version. Used by
// the dependency-version check at Market boot.
func (c *httpClient) GetVersion(ctx context.Context) (*VersionInfo, error) {
	const ep = "get_version"
	var out VersionInfo
	if err := c.doJSONGet(ctx, ep, "/version", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMarketSources fetches the full market source configuration from
// chart-repo. Surfaces *BusinessError when success=false.
func (c *httpClient) GetMarketSources(ctx context.Context) (*MarketSourcesConfig, error) {
	const ep = "get_market_sources"
	var out MarketSourcesConfig
	if err := c.doJSONGet(ctx, ep, "/settings/market-source", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddMarketSource posts a market source row to chart-repo. The
// response is the standard {success, message} envelope with an
// empty data field on success; we discard the body once classified.
func (c *httpClient) AddMarketSource(ctx context.Context, src *MarketSource) error {
	const ep = "add_market_source"
	if src == nil {
		return fmt.Errorf("chartrepo: AddMarketSource called with nil source")
	}
	return c.doJSONPostNoData(ctx, ep, "/settings/market-source", src)
}

// DeleteMarketSource removes a market source by id. Path-segment
// escapes the id so sources whose name contains "/" or other URL-
// hostile characters do not break the request URL.
func (c *httpClient) DeleteMarketSource(ctx context.Context, sourceID string) error {
	const ep = "delete_market_source"
	if strings.TrimSpace(sourceID) == "" {
		return fmt.Errorf("chartrepo: DeleteMarketSource called with empty sourceID")
	}
	path := "/settings/market-source/" + url.PathEscape(sourceID)
	return c.doJSONNoBodyEnvelope(ctx, ep, http.MethodDelete, path)
}

// GetRepoData fetches the full repo data structure used by the cache-
// correction path. This endpoint is the odd one out: it returns the
// data structure directly at the top level, NOT wrapped in the
// {success, message, data} envelope. We therefore bypass the envelope
// decoder and unmarshal into the typed RepoData shape directly.
func (c *httpClient) GetRepoData(ctx context.Context) (*RepoData, error) {
	const ep = "get_repo_data"
	body, status, err := c.doRequest(ctx, ep, http.MethodGet, pathPrefix+"/repo/data", nil, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, classifyHTTPStatus(status)
	}
	var out RepoData
	if err := json.Unmarshal(body, &out); err != nil {
		c.metrics.ObserveRequest(ep, status, 0, errKindDecode)
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return &out, nil
}

// GetStateChanges fetches up to `limit` state-change rows whose id is
// strictly greater than afterID. limit is clamped to a non-negative
// integer; the upstream contract caps it server-side (currently 1000).
func (c *httpClient) GetStateChanges(ctx context.Context, afterID int64, limit int) (*StateChangesData, error) {
	const ep = "get_state_changes"
	if limit < 0 {
		limit = 0
	}
	path := fmt.Sprintf("/state-changes?after_id=%d&limit=%d", afterID, limit)
	var out StateChangesData
	if err := c.doJSONGet(ctx, ep, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAppInfo posts an Apps batch request and returns the flat
// []map[string]any payload chart-repo nests under
// envelope.data.apps. Returns an empty slice (not error) when the
// upstream replies with success=true but an empty apps array.
func (c *httpClient) GetAppInfo(ctx context.Context, in AppsRequest) ([]map[string]interface{}, error) {
	const ep = "get_app_info"
	var out AppsResponseData
	if err := c.doJSONPost(ctx, ep, "/apps", in, &out); err != nil {
		return nil, err
	}
	return out.Apps, nil
}

// GetImageInfo fetches chart-repo's analysis for a single image. The
// imageName is URL-query-escaped to handle the colons/slashes typical
// in OCI references (e.g. "registry.io/foo/bar:v1.2.3").
func (c *httpClient) GetImageInfo(ctx context.Context, imageName string) (map[string]interface{}, error) {
	const ep = "get_image_info"
	if strings.TrimSpace(imageName) == "" {
		return nil, fmt.Errorf("chartrepo: GetImageInfo called with empty imageName")
	}
	path := "/images?imageName=" + url.QueryEscape(imageName)
	var out ImagesResponseData
	if err := c.doJSONGet(ctx, ep, path, &out); err != nil {
		return nil, err
	}
	return out.ImageInfo, nil
}

// GetAppVersionForDownloadHistory resolves the (version, source) pair
// chart-repo associates with the given (userID, appName) download
// history row. Returns ErrInvalidResponse if either field is missing
// from the upstream payload (matches the legacy callsite behaviour).
func (c *httpClient) GetAppVersionForDownloadHistory(ctx context.Context, userID, appName string) (string, string, error) {
	const ep = "get_version_for_download_history"
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(appName) == "" {
		return "", "", fmt.Errorf("chartrepo: GetAppVersionForDownloadHistory called with empty userID/appName")
	}
	path := fmt.Sprintf("/app/version-for-download-history?user=%s&app_name=%s",
		url.QueryEscape(userID), url.QueryEscape(appName))
	var out AppDownloadHistoryData
	if err := c.doJSONGet(ctx, ep, path, &out); err != nil {
		return "", "", err
	}
	if out.Version == "" || out.Source == "" {
		c.metrics.ObserveRequest(ep, http.StatusOK, 0, errKindDecode)
		return "", "", fmt.Errorf("%w: version/source missing", ErrInvalidResponse)
	}
	return out.Version, out.Source, nil
}

// UploadChart uploads a chart package via multipart/form-data to
// /apps/upload. The body builder is local to this method (rather than
// piped through doRequest) because none of the other endpoints take
// multipart bodies; generalising the content-type branch would hide
// more than it abstracts. Token / UserID are mandatory — they are
// passed through three headers verbatim (mirroring the legacy
// callsite which sets all three from the same string).
func (c *httpClient) UploadChart(ctx context.Context, in UploadChartInput) (*UploadChartResponseData, error) {
	const ep = "upload_chart"
	if strings.TrimSpace(in.Filename) == "" {
		return nil, fmt.Errorf("chartrepo: UploadChart requires Filename")
	}
	if len(in.FileBytes) == 0 {
		return nil, fmt.Errorf("chartrepo: UploadChart requires FileBytes")
	}

	body, contentType, err := buildMultipartUpload(in)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{
		"Content-Type": contentType,
	}
	// X-Authorization / X-User-ID / X-Bfl-User mirror the legacy
	// callsite. We set them only when non-empty so tests / future
	// unauthenticated paths do not ship blank headers.
	if in.Token != "" {
		headers["X-Authorization"] = in.Token
	}
	if in.UserID != "" {
		headers["X-User-ID"] = in.UserID
		headers["X-Bfl-User"] = in.UserID
	}

	respBody, status, err := c.doRequest(ctx, ep, http.MethodPost, pathPrefix+"/apps/upload", headers, body)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, classifyHTTPStatus(status)
	}

	var env envelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		c.metrics.ObserveRequest(ep, status, 0, errKindDecode)
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if !env.Success {
		c.metrics.ObserveRequest(ep, status, 0, errKindBusinessError)
		return nil, &BusinessError{Endpoint: ep, Message: env.Message}
	}

	var out UploadChartResponseData
	if len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, &out); err != nil {
			c.metrics.ObserveRequest(ep, status, 0, errKindDecode)
			return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
		}
	}
	return &out, nil
}

// buildMultipartUpload constructs the multipart body for UploadChart.
// Kept as a standalone function so it can be unit-tested in isolation
// from the HTTP round-trip.
func buildMultipartUpload(in UploadChartInput) (io.Reader, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("chart", in.Filename)
	if err != nil {
		return nil, "", fmt.Errorf("chartrepo: create multipart file: %w", err)
	}
	if _, err := part.Write(in.FileBytes); err != nil {
		return nil, "", fmt.Errorf("chartrepo: write multipart file: %w", err)
	}
	if in.SourceID != "" {
		if err := writer.WriteField("source", in.SourceID); err != nil {
			return nil, "", fmt.Errorf("chartrepo: write multipart source field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("chartrepo: close multipart writer: %w", err)
	}
	return &buf, writer.FormDataContentType(), nil
}

// DeleteLocalApp asks chart-repo to remove a previously-uploaded
// local chart. The DELETE verb carries a JSON body (an unusual
// combination, but it matches the upstream contract). token / userID
// are propagated into headers when non-empty.
func (c *httpClient) DeleteLocalApp(ctx context.Context, in DeleteLocalAppRequest, token, userID string) error {
	const ep = "delete_local_app"
	bodyBytes, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("chartrepo: marshal delete_local_app body: %w", err)
	}
	headers := map[string]string{}
	if token != "" {
		headers["X-Authorization"] = token
	}
	if userID != "" {
		headers["X-User-ID"] = userID
		headers["X-Bfl-User"] = userID
	}

	respBody, status, err := c.doRequest(ctx, ep, http.MethodDelete, pathPrefix+"/local-apps/delete", headers, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return classifyHTTPStatus(status)
	}
	return c.decodeEnvelopeNoData(ep, status, respBody)
}

// SyncApp posts a render request for one (user, source, app) and
// returns the unwrapped SyncAppData payload. success=false is
// surfaced as *BusinessError — the hydration loop maps that to
// MarkRenderFailed without confusing it with a transport error.
func (c *httpClient) SyncApp(ctx context.Context, req SyncAppRequest) (*SyncAppData, error) {
	const ep = "sync_app"
	var out SyncAppData
	if err := c.doJSONPost(ctx, ep, "/dcr/sync-app", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetStatus fetches the runtime status for a (user, source) tuple.
// include filters which sub-blocks chart-repo should populate
// (apps / images / tasks) — passing an empty slice means "all".
func (c *httpClient) GetStatus(ctx context.Context, userID, sourceID string, include []StatusInclude) (*Status, error) {
	const ep = "get_status"
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sourceID) == "" {
		return nil, fmt.Errorf("chartrepo: GetStatus requires userID and sourceID")
	}

	q := url.Values{}
	q.Set("user", userID)
	q.Set("source", sourceID)
	if len(include) > 0 {
		parts := make([]string, 0, len(include))
		for _, s := range include {
			if v := strings.TrimSpace(string(s)); v != "" {
				parts = append(parts, v)
			}
		}
		if len(parts) > 0 {
			q.Set("include", strings.Join(parts, ","))
		}
	}
	path := "/status?" + q.Encode()

	var out Status
	if err := c.doJSONGet(ctx, ep, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// =====================================================================
// Internal helpers — envelope handling
// =====================================================================

// doJSONGet performs a GET, asserts HTTP 200, decodes the standard
// {success, message, data} envelope, and unmarshals data into out.
// success=false surfaces as *BusinessError.
func (c *httpClient) doJSONGet(ctx context.Context, ep, path string, out any) error {
	respBody, status, err := c.doRequest(ctx, ep, http.MethodGet, pathPrefix+path, nil, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return classifyHTTPStatus(status)
	}
	return c.decodeEnvelopeIntoData(ep, status, respBody, out)
}

// doJSONPost performs a POST with a JSON body, asserts HTTP 200,
// decodes the envelope, and unmarshals data into out.
func (c *httpClient) doJSONPost(ctx context.Context, ep, path string, body, out any) error {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("chartrepo: marshal %s body: %w", ep, err)
	}
	headers := map[string]string{"Content-Type": "application/json"}
	respBody, status, err := c.doRequest(ctx, ep, http.MethodPost, pathPrefix+path, headers, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return classifyHTTPStatus(status)
	}
	return c.decodeEnvelopeIntoData(ep, status, respBody, out)
}

// doJSONPostNoData performs a POST with a JSON body for endpoints
// whose envelope data field is uninteresting (or empty). Only
// success / message are validated.
func (c *httpClient) doJSONPostNoData(ctx context.Context, ep, path string, body any) error {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("chartrepo: marshal %s body: %w", ep, err)
	}
	headers := map[string]string{"Content-Type": "application/json"}
	respBody, status, err := c.doRequest(ctx, ep, http.MethodPost, pathPrefix+path, headers, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return classifyHTTPStatus(status)
	}
	return c.decodeEnvelopeNoData(ep, status, respBody)
}

// doJSONNoBodyEnvelope performs a request with no body and validates
// the {success, message} envelope. Used by DELETE endpoints whose
// response carries no data field.
func (c *httpClient) doJSONNoBodyEnvelope(ctx context.Context, ep, method, path string) error {
	respBody, status, err := c.doRequest(ctx, ep, method, pathPrefix+path, nil, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return classifyHTTPStatus(status)
	}
	return c.decodeEnvelopeNoData(ep, status, respBody)
}

// decodeEnvelopeIntoData parses {success, message, data} and, on
// success, unmarshals data into out. out may be nil if the caller
// only cares about success/message.
func (c *httpClient) decodeEnvelopeIntoData(ep string, status int, body []byte, out any) error {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		c.metrics.ObserveRequest(ep, status, 0, errKindDecode)
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if !env.Success {
		c.metrics.ObserveRequest(ep, status, 0, errKindBusinessError)
		return &BusinessError{Endpoint: ep, Message: env.Message}
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		c.metrics.ObserveRequest(ep, status, 0, errKindDecode)
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return nil
}

// decodeEnvelopeNoData is decodeEnvelopeIntoData minus the data
// unmarshal step. Used by endpoints whose response payload contains
// nothing the caller needs beyond the success flag.
func (c *httpClient) decodeEnvelopeNoData(ep string, status int, body []byte) error {
	var env envelope
	// Empty bodies are tolerated — chart-repo sometimes returns an
	// empty 200 OK on DELETE; treat as success.
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, &env); err != nil {
		c.metrics.ObserveRequest(ep, status, 0, errKindDecode)
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if !env.Success {
		c.metrics.ObserveRequest(ep, status, 0, errKindBusinessError)
		return &BusinessError{Endpoint: ep, Message: env.Message}
	}
	return nil
}

// =====================================================================
// Internal helpers — transport
// =====================================================================

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

	glog.Infof("[chartrepo] request path: %s, method: %s, headers: %s", path, method, helper.ParseJson(headers))

	start := time.Now()

	// Apply default timeout if the caller's context has no deadline of
	// its own. We only wrap when there is no deadline — otherwise the
	// caller's deadline (which they presumably set for a reason) wins.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		c.metrics.ObserveRequest(ep, 0, time.Since(start), errKindNetwork)
		return nil, 0, fmt.Errorf("chartrepo: build request: %w", err)
	}

	req.Header.Set("Accept", "*/*")
	// Caller-supplied Content-Type wins (multipart uses a generated
	// boundary value); we set a JSON default only when there is a
	// body and the caller did not specify one.
	if body != nil && headers["Content-Type"] == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		kind := errKindNetwork
		if errors.Is(err, context.DeadlineExceeded) {
			kind = errKindTimeout
		}
		c.metrics.ObserveRequest(ep, 0, time.Since(start), kind)
		return nil, 0, fmt.Errorf("%w: %v", ErrChartRepoUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.metrics.ObserveRequest(ep, resp.StatusCode, time.Since(start), errKindNetwork)
		return nil, resp.StatusCode, fmt.Errorf("chartrepo: read response: %w", err)
	}

	// Record success/failure status before returning. Business-code
	// failures (success=false) are recorded later by the envelope
	// decoder.
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
