package appservice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// Construction
// =====================================================================

func TestNewClient_WithBaseURL(t *testing.T) {
	c, err := NewClient(WithBaseURL("http://example.com:9000/"))
	require.NoError(t, err)

	hc := c.(*httpClient)
	// Trailing slash must be stripped so endpoint methods can append
	// "/path" without producing a double slash.
	assert.Equal(t, "http://example.com:9000", hc.baseURL)
	assert.Equal(t, defaultTimeout, hc.timeout)
	assert.NotNil(t, hc.http)
	assert.NotNil(t, hc.metrics)
}

func TestNewClient_NoBaseURLNoEnv(t *testing.T) {
	t.Setenv(envHost, "")
	t.Setenv(envPort, "")

	_, err := NewClient()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigMissing)
}

func TestNewClient_FromEnv(t *testing.T) {
	t.Setenv(envHost, "app-service.os-platform")
	t.Setenv(envPort, "8080")

	c, err := NewClient()
	require.NoError(t, err)
	assert.Equal(t, "http://app-service.os-platform:8080", c.(*httpClient).baseURL)
}

func TestNewClient_OptionsOverrideEnv(t *testing.T) {
	t.Setenv(envHost, "from-env")
	t.Setenv(envPort, "1")

	c, err := NewClient(WithBaseURL("http://from-option:9090"))
	require.NoError(t, err)
	assert.Equal(t, "http://from-option:9090", c.(*httpClient).baseURL)
}

// =====================================================================
// GetAllApps
// =====================================================================

func TestGetAllApps_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/app-service/v1/all/apps", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"metadata":{"name":"app-1","uid":"u1","namespace":"ns1"},
			 "spec":{"name":"appone","owner":"alice","entrances":[]},
			 "status":{"state":"running"}},
			{"metadata":{"name":"app-2","uid":"u2","namespace":"ns2"},
			 "spec":{"name":"apptwo","owner":"bob","entrances":[]},
			 "status":{"state":"installing"}}
		]`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	apps, err := c.GetAllApps(context.Background())
	require.NoError(t, err)
	require.Len(t, apps, 2)
	assert.Equal(t, "appone", apps[0].Spec.Name)
	assert.Equal(t, "alice", apps[0].Spec.Owner)
	assert.Equal(t, "running", apps[0].Status.State)
	assert.Equal(t, "installing", apps[1].Status.State)
}

func TestGetAllApps_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	_, err := c.GetAllApps(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAppServiceUnavailable)
}

func TestGetAllApps_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	_, err := c.GetAllApps(context.Background())
	assert.ErrorIs(t, err, ErrAppNotFound)
}

func TestGetAllApps_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `not json`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	_, err := c.GetAllApps(context.Background())
	assert.ErrorIs(t, err, ErrInvalidResponse)
}

// =====================================================================
// GetTerminusVersion
// =====================================================================

func TestGetTerminusVersion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/app-service/v1/terminus/version", r.URL.Path)
		_, _ = io.WriteString(w, `{"version":"1.13.0"}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	v, err := c.GetTerminusVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "1.13.0", v)
}

// =====================================================================
// GetUserInfo
// =====================================================================

func TestGetUserInfo_TokenHeaderForwarded(t *testing.T) {
	var seenToken atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenToken.Store(r.Header.Get("X-Authorization"))
		_, _ = io.WriteString(w, `{"username":"alice","role":"admin","email":"a@b"}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	ui, err := c.GetUserInfo(context.Background(), "tok-abc")
	require.NoError(t, err)
	assert.Equal(t, "alice", ui.Username)
	assert.Equal(t, "tok-abc", seenToken.Load())
}

func TestGetUserInfo_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	_, err := c.GetUserInfo(context.Background(), "tok")
	assert.ErrorIs(t, err, ErrUnauthorized)
}

// =====================================================================
// GetAppEntranceURLs
// =====================================================================

func TestGetAppEntranceURLs_Filters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[
			{"spec":{"name":"target","owner":"alice","entrances":[
				{"name":"web","url":"web.example.com"},
				{"name":"api","url":""},
				{"name":"admin","url":"admin.example.com"}
			]},"status":{"state":"running"}},
			{"spec":{"name":"other","owner":"alice","entrances":[
				{"name":"x","url":"should-not-appear"}
			]},"status":{"state":"running"}}
		]`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	urls, err := c.GetAppEntranceURLs(context.Background(), "target", "alice")
	require.NoError(t, err)
	assert.Len(t, urls, 2) // empty URL skipped
	assert.Equal(t, "web.example.com", urls["web"])
	assert.Equal(t, "admin.example.com", urls["admin"])
	assert.NotContains(t, urls, "x")
	assert.NotContains(t, urls, "api")
}

// =====================================================================
// Operation endpoints
// =====================================================================

func TestInstallApp_Success(t *testing.T) {
	var seen struct {
		path         string
		method       string
		auth         string
		bflUser      string
		marketUser   string
		marketSource string
		body         string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.path = r.URL.Path
		seen.method = r.Method
		seen.auth = r.Header.Get("X-Authorization")
		seen.bflUser = r.Header.Get("X-Bfl-User")
		seen.marketUser = r.Header.Get("X-Market-User")
		seen.marketSource = r.Header.Get("X-Market-Source")
		buf, _ := io.ReadAll(r.Body)
		seen.body = string(buf)
		_, _ = io.WriteString(w, `{"code":200,"data":{"opID":"op-xyz"}}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	resp, err := c.InstallApp(context.Background(), "myapp", InstallOptions{
		Source:       "market",
		User:         "alice",
		MarketSource: "market.olares",
		Envs:         []AppEnvVar{},
	}, OpHeaders{
		Token:        "tok",
		User:         "alice",
		MarketUser:   "alice",
		MarketSource: "market.olares",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Data)
	assert.Equal(t, "op-xyz", resp.Data.OpID)

	// Wire-format assertions: every header app-service expects must
	// be present, the body must be the install JSON.
	assert.Equal(t, "/app-service/v1/apps/myapp/install", seen.path)
	assert.Equal(t, http.MethodPost, seen.method)
	assert.Equal(t, "tok", seen.auth)
	assert.Equal(t, "alice", seen.bflUser)
	assert.Equal(t, "alice", seen.marketUser)
	assert.Equal(t, "market.olares", seen.marketSource)
	assert.Contains(t, seen.body, `"x_market_source":"market.olares"`)
}

func TestInstallApp_BusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// HTTP 200 but Code != 200 — the typical "validation failed"
		// shape app-service ships.
		_, _ = io.WriteString(w, `{"code":400,"message":"bad input","data":{"opID":"op-partial"}}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	resp, err := c.InstallApp(context.Background(), "myapp", InstallOptions{Envs: []AppEnvVar{}}, OpHeaders{Token: "t"})
	require.Error(t, err)

	var opErr *OpError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, "install", opErr.Op)
	assert.Equal(t, 400, opErr.Code)
	assert.Equal(t, "bad input", opErr.Message)
	assert.Equal(t, "op-partial", opErr.OpID)
	// Response is also returned so callers that want the raw body can
	// inspect it without re-parsing.
	require.NotNil(t, resp)
	assert.Equal(t, 400, resp.Code)
}

// TestInstallApp_BusinessError_StructuredData covers the 422 +
// appenv path where app-service returns HTTP 200 with a business
// error envelope that carries a STRUCTURED data field whose shape
// does NOT match OpResult (the typed success-shape decode target).
//
// Two invariants:
//
//  1. opResp.Data.OpID stays empty (typed decode finds no opID),
//     keeping the legacy "Data!=nil but OpID empty" treatment
//     intact for callers that gate on OpID presence.
//  2. opResp.DataRaw preserves the original bytes byte-for-byte so
//     the install handler can forward them under
//     `backend_response.data` to the frontend without lossy
//     interface{} round-tripping (notably the mixed-case "data" vs
//     "Data" keys upstream uses).
func TestInstallApp_BusinessError_StructuredData(t *testing.T) {
	const upstreamBody = `{"code":422,"data":{"type":"appenv","Data":{"missingValues":[{"envName":"USERNAME","type":"string","required":true},{"envName":"PASSWORD","type":"password","required":true}],"missingRefs":null,"invalidValues":null}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, upstreamBody)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	resp, err := c.InstallApp(context.Background(), "medusa",
		InstallOptions{Envs: []AppEnvVar{}}, OpHeaders{Token: "t"})
	require.Error(t, err)

	var opErr *OpError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, 422, opErr.Code)
	assert.Equal(t, "install", opErr.Op)
	assert.Equal(t, "", opErr.OpID)

	require.NotNil(t, resp)
	assert.Equal(t, 422, resp.Code)
	// Data is the typed view; OpID is empty because the upstream
	// payload doesn't include one. Existing callers that gate on
	// Data.OpID continue to short-circuit as "no opID".
	require.NotNil(t, resp.Data)
	assert.Equal(t, "", resp.Data.OpID)
	// DataRaw must carry the structured payload verbatim. We assert
	// on byte-level equality after a JSON round-trip on the input so
	// the test is robust to whitespace differences app-service might
	// introduce; what matters is the same logical JSON value lands.
	require.NotEmpty(t, resp.DataRaw)
	var got, want map[string]any
	require.NoError(t, json.Unmarshal(resp.DataRaw, &got))
	require.NoError(t, json.Unmarshal([]byte(`{"type":"appenv","Data":{"missingValues":[{"envName":"USERNAME","type":"string","required":true},{"envName":"PASSWORD","type":"password","required":true}],"missingRefs":null,"invalidValues":null}}`), &want))
	assert.Equal(t, want, got)
}

// TestInstallApp_Success_DataRawPopulated confirms DataRaw is also
// populated on the success path (forward-compat for upstream adding
// new fields alongside opID). Existing `resp.Data.OpID` access is
// preserved.
func TestInstallApp_Success_DataRawPopulated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"code":200,"data":{"opID":"op-ok","warnings":["x"]}}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	resp, err := c.InstallApp(context.Background(), "myapp",
		InstallOptions{Envs: []AppEnvVar{}}, OpHeaders{Token: "t"})
	require.NoError(t, err)
	require.NotNil(t, resp.Data)
	assert.Equal(t, "op-ok", resp.Data.OpID)
	require.NotEmpty(t, resp.DataRaw)
	// The new "warnings" field rides through DataRaw without an
	// OpResult schema change.
	assert.Contains(t, string(resp.DataRaw), `"warnings"`)
}

// TestOpResponse_NullData ensures `data: null` and missing-data
// envelopes do not produce spurious Data/DataRaw values.
func TestOpResponse_NullData(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"null", `{"code":200,"data":null}`},
		{"missing", `{"code":200}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r OpResponse
			require.NoError(t, json.Unmarshal([]byte(tc.body), &r))
			assert.Equal(t, 200, r.Code)
			assert.Nil(t, r.Data)
			assert.Empty(t, r.DataRaw)
		})
	}
}

func TestInstallApp_TransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	_, err := c.InstallApp(context.Background(), "myapp", InstallOptions{Envs: []AppEnvVar{}}, OpHeaders{Token: "t"})
	assert.ErrorIs(t, err, ErrAppServiceUnavailable)
}

func TestUninstallApp_BodyShape(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		_, _ = io.WriteString(w, `{"code":200,"data":{"opID":"op-u1"}}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	resp, err := c.UninstallApp(context.Background(), "myapp",
		UninstallOptions{All: true, DeleteData: false},
		OpHeaders{Token: "t", User: "alice"})
	require.NoError(t, err)
	assert.Equal(t, "op-u1", resp.Data.OpID)

	// Body must be exactly {all,deleteData} — these are required by
	// app-service even when false.
	assert.Contains(t, body, `"all":true`)
	assert.Contains(t, body, `"deleteData":false`)
}

func TestCancelApp_NoBody(t *testing.T) {
	var (
		method   string
		query    string
		hadBody  bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		query = r.URL.RawQuery
		buf, _ := io.ReadAll(r.Body)
		hadBody = len(buf) > 0
		_, _ = io.WriteString(w, `{"code":200,"data":{"opID":"op-c1"}}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	resp, err := c.CancelApp(context.Background(), "myapp", OpHeaders{Token: "t", User: "alice"})
	require.NoError(t, err)
	assert.Equal(t, "op-c1", resp.Data.OpID)
	assert.Equal(t, http.MethodPost, method)
	assert.Equal(t, "type=operate", query)
	assert.False(t, hadBody, "cancel must not send a body")
}

// =====================================================================
// Helpers
// =====================================================================

func TestOpHeadersToMap_OmitsEmpty(t *testing.T) {
	m := opHeadersToMap(OpHeaders{Token: "t", User: "u"})
	assert.Equal(t, map[string]string{
		"X-Authorization": "t",
		"X-Bfl-User":      "u",
	}, m)

	m = opHeadersToMap(OpHeaders{})
	assert.Empty(t, m)
}

func TestClassifyHTTPStatus(t *testing.T) {
	assert.ErrorIs(t, classifyHTTPStatus(401), ErrUnauthorized)
	assert.ErrorIs(t, classifyHTTPStatus(403), ErrUnauthorized)
	assert.ErrorIs(t, classifyHTTPStatus(404), ErrAppNotFound)
	assert.ErrorIs(t, classifyHTTPStatus(500), ErrAppServiceUnavailable)
	assert.ErrorIs(t, classifyHTTPStatus(503), ErrAppServiceUnavailable)
	// Non-classified status codes get a generic error (not one of the
	// sentinels) so callers don't accidentally treat them as a known
	// retryable failure.
	err := classifyHTTPStatus(418)
	assert.NotErrorIs(t, err, ErrUnauthorized)
	assert.NotErrorIs(t, err, ErrAppNotFound)
	assert.NotErrorIs(t, err, ErrAppServiceUnavailable)
	assert.Contains(t, err.Error(), "418")
}

// =====================================================================
// Metrics observation
// =====================================================================

type recordingMetrics struct {
	last struct {
		endpoint string
		status   int
		errKind  string
	}
}

func (r *recordingMetrics) ObserveRequest(endpoint string, status int, _ time.Duration, errKind string) {
	r.last.endpoint = endpoint
	r.last.status = status
	r.last.errKind = errKind
}

func TestMetrics_RecordedOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"version":"1.0"}`)
	}))
	defer srv.Close()

	rec := &recordingMetrics{}
	c, _ := NewClient(WithBaseURL(srv.URL), WithMetrics(rec))
	_, err := c.GetTerminusVersion(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "get_terminus_version", rec.last.endpoint)
	assert.Equal(t, http.StatusOK, rec.last.status)
	assert.Equal(t, errKindNone, rec.last.errKind)
}

func TestMetrics_RecordedOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	rec := &recordingMetrics{}
	c, _ := NewClient(WithBaseURL(srv.URL), WithMetrics(rec))
	_, err := c.GetAllApps(context.Background())
	require.Error(t, err)
	assert.Equal(t, "get_all_apps", rec.last.endpoint)
	assert.Equal(t, http.StatusInternalServerError, rec.last.status)
	assert.Equal(t, errKindServerError, rec.last.errKind)
}

// =====================================================================
// Request defaults / context
// =====================================================================

func TestDoRequest_DefaultsContentTypeForBody(t *testing.T) {
	var ct string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct = r.Header.Get("Content-Type")
		_, _ = io.WriteString(w, `{"code":200,"data":{"opID":"x"}}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	_, err := c.UninstallApp(context.Background(), "a",
		UninstallOptions{}, OpHeaders{Token: "t"})
	require.NoError(t, err)
	assert.Equal(t, "application/json", ct)
}

func TestDoRequest_RespectsContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Sleep longer than the caller's context allows.
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.GetAllApps(ctx)
	require.Error(t, err)
	// Caller's deadline must propagate as a transport error wrapped
	// in ErrAppServiceUnavailable, not silently succeed.
	assert.True(t, errors.Is(err, ErrAppServiceUnavailable) || strings.Contains(err.Error(), "context"),
		"expected context deadline error, got %v", err)
}

// =====================================================================
// MockClient sanity
// =====================================================================

func TestMockClient_RecordsCallsAndReturnsConfiguredValue(t *testing.T) {
	mock := &MockClient{
		GetTerminusVersionFunc: func(ctx context.Context) (string, error) {
			return "9.9.9", nil
		},
	}

	v, err := mock.GetTerminusVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "9.9.9", v)
	assert.Equal(t, 1, mock.GetTerminusVersionCalls)

	// Methods without configured Func return zero value, not panic.
	apps, err := mock.GetAllApps(context.Background())
	require.NoError(t, err)
	assert.Nil(t, apps)
	assert.Equal(t, 1, mock.GetAllAppsCalls)
}
