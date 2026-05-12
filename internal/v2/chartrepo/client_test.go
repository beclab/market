package chartrepo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
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
// Construction / NewClient
// =====================================================================

func TestNewClient_WithBaseURL_TrailingSlashStripped(t *testing.T) {
	c, err := NewClient(WithBaseURL("http://example.com:9000/"))
	require.NoError(t, err)
	assert.Equal(t, "http://example.com:9000", c.(*httpClient).baseURL)
}

func TestNewClient_NoEnv_ReturnsConfigMissing(t *testing.T) {
	t.Setenv(envServiceHost, "")
	_, err := NewClient()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigMissing)
}

func TestNewClient_FromEnv_BareHost_PrefixesHTTP(t *testing.T) {
	t.Setenv(envServiceHost, "chart-repo.os-platform:8080")
	c, err := NewClient()
	require.NoError(t, err)
	// The legacy "http://http://..." double-prefix bug used to bite
	// when env held a bare host:port and code prepended "http://"
	// blindly. NewClient now applies the prefix exactly once.
	assert.Equal(t, "http://chart-repo.os-platform:8080", c.(*httpClient).baseURL)
}

func TestNewClient_FromEnv_AlreadyHasScheme_NoDoublePrefix(t *testing.T) {
	t.Setenv(envServiceHost, "http://chart-repo.os-platform:8080/")
	c, err := NewClient()
	require.NoError(t, err)
	assert.Equal(t, "http://chart-repo.os-platform:8080", c.(*httpClient).baseURL)
}

func TestNewClient_OptionsOverrideEnv(t *testing.T) {
	t.Setenv(envServiceHost, "from-env")
	c, err := NewClient(WithBaseURL("http://from-option:9090"))
	require.NoError(t, err)
	assert.Equal(t, "http://from-option:9090", c.(*httpClient).baseURL)
}

// =====================================================================
// GetVersion
// =====================================================================

func TestGetVersion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, pathPrefix+"/version", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		_, _ = io.WriteString(w, `{"success":true,"data":{"version":"0.2.3","build_time":"now","git_commit":"abc","timestamp":17}}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	v, err := c.GetVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "0.2.3", v.Version)
	assert.Equal(t, int64(17), v.Timestamp)
}

func TestGetVersion_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	_, err := c.GetVersion(context.Background())
	assert.ErrorIs(t, err, ErrChartRepoUnavailable)
}

func TestGetVersion_BusinessFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"message":"degraded"}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	_, err := c.GetVersion(context.Background())
	require.Error(t, err)
	var be *BusinessError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, "get_version", be.Endpoint)
	assert.Equal(t, "degraded", be.Message)
}

func TestGetVersion_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `not json`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	_, err := c.GetVersion(context.Background())
	assert.ErrorIs(t, err, ErrInvalidResponse)
}

// =====================================================================
// Market sources (GET / POST / DELETE)
// =====================================================================

func TestGetMarketSources_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, pathPrefix+"/settings/market-source", r.URL.Path)
		_, _ = io.WriteString(w, `{"success":true,"data":{"sources":[{"id":"market.olares","name":"market.olares","type":"remote","base_url":"https://x","priority":100,"is_active":true,"updated_at":"2024-01-01T00:00:00Z","description":"d"}],"default_source":"market.olares","updated_at":"2024-01-01T00:00:00Z"}}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	cfg, err := c.GetMarketSources(context.Background())
	require.NoError(t, err)
	require.Len(t, cfg.Sources, 1)
	assert.Equal(t, "market.olares", cfg.Sources[0].ID)
	assert.Equal(t, "market.olares", cfg.DefaultSource)
}

func TestAddMarketSource_Success(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		receivedBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"success":true,"message":"ok"}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	src := &MarketSource{ID: "upload", Name: "upload", Type: "local", BaseURL: "file://", Priority: 50, IsActive: true}
	err := c.AddMarketSource(context.Background(), src)
	require.NoError(t, err)
	// The body must round-trip the wire field names.
	var got MarketSource
	require.NoError(t, json.Unmarshal(receivedBody, &got))
	assert.Equal(t, "upload", got.ID)
	assert.Equal(t, "local", got.Type)
}

func TestAddMarketSource_BusinessFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"message":"duplicate"}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	err := c.AddMarketSource(context.Background(), &MarketSource{ID: "x"})
	require.Error(t, err)
	var be *BusinessError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, "duplicate", be.Message)
}

func TestDeleteMarketSource_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, pathPrefix+"/settings/market-source/my%2Fsource", r.URL.EscapedPath())
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	require.NoError(t, c.DeleteMarketSource(context.Background(), "my/source"))
}

func TestDeleteMarketSource_EmptyID(t *testing.T) {
	c, _ := NewClient(WithBaseURL("http://x"))
	err := c.DeleteMarketSource(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty sourceID")
}

// =====================================================================
// GetRepoData (no envelope)
// =====================================================================

func TestGetRepoData_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, pathPrefix+"/repo/data", r.URL.Path)
		_, _ = io.WriteString(w, `{"user_data":{"sources":{"src1":{"app_info_latest":[{"app_simple_info":{"app_id":"a1"}}]}}}}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	rd, err := c.GetRepoData(context.Background())
	require.NoError(t, err)
	require.NotNil(t, rd.UserData)
	src := rd.UserData.Sources["src1"]
	require.NotNil(t, src)
	require.Len(t, src.AppInfoLatest, 1)
	assert.Equal(t, "a1", src.AppInfoLatest[0].AppSimpleInfo.AppID)
}

// =====================================================================
// GetStateChanges
// =====================================================================

func TestGetStateChanges_PropagatesQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "42", r.URL.Query().Get("after_id"))
		assert.Equal(t, "100", r.URL.Query().Get("limit"))
		_, _ = io.WriteString(w, `{"success":true,"data":{"after_id":42,"limit":100,"count":1,"total_available":1,"state_changes":[{"id":43,"type":"app_upload_completed","app_data":{"source":"s","app_name":"a","user_id":"u"},"timestamp":"2024-01-01T00:00:00Z"}]}}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	data, err := c.GetStateChanges(context.Background(), 42, 100)
	require.NoError(t, err)
	require.Len(t, data.StateChanges, 1)
	assert.Equal(t, "app_upload_completed", data.StateChanges[0].Type)
	require.NotNil(t, data.StateChanges[0].AppData)
	assert.Equal(t, "u", data.StateChanges[0].AppData.UserID)
}

// =====================================================================
// GetAppInfo (batch POST)
// =====================================================================

func TestGetAppInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		var req AppsRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "u1", req.UserID)
		require.Len(t, req.Apps, 1)
		assert.Equal(t, "myapp", req.Apps[0].AppID)
		assert.Equal(t, "src1", req.Apps[0].SourceDataName)
		_, _ = io.WriteString(w, `{"success":true,"data":{"apps":[{"id":"a1","name":"myapp"}]}}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	apps, err := c.GetAppInfo(context.Background(), AppsRequest{
		Apps:   []AppsRequestItem{{AppID: "myapp", SourceDataName: "src1"}},
		UserID: "u1",
	})
	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.Equal(t, "myapp", apps[0]["name"])
}

// =====================================================================
// GetImageInfo
// =====================================================================

func TestGetImageInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "registry.io/foo/bar:v1", r.URL.Query().Get("imageName"))
		_, _ = io.WriteString(w, `{"success":true,"data":{"image_info":{"tag":"v1","total_size":1024}}}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	info, err := c.GetImageInfo(context.Background(), "registry.io/foo/bar:v1")
	require.NoError(t, err)
	assert.Equal(t, "v1", info["tag"])
}

func TestGetImageInfo_EmptyName(t *testing.T) {
	c, _ := NewClient(WithBaseURL("http://x"))
	_, err := c.GetImageInfo(context.Background(), "")
	require.Error(t, err)
}

// =====================================================================
// GetAppVersionForDownloadHistory
// =====================================================================

func TestGetAppVersionForDownloadHistory_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "u1", r.URL.Query().Get("user"))
		assert.Equal(t, "myapp", r.URL.Query().Get("app_name"))
		_, _ = io.WriteString(w, `{"success":true,"data":{"version":"1.2.3","source":"market.olares"}}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	v, s, err := c.GetAppVersionForDownloadHistory(context.Background(), "u1", "myapp")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", v)
	assert.Equal(t, "market.olares", s)
}

func TestGetAppVersionForDownloadHistory_MissingField_InvalidResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":true,"data":{"version":"1.2.3"}}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	_, _, err := c.GetAppVersionForDownloadHistory(context.Background(), "u", "a")
	assert.ErrorIs(t, err, ErrInvalidResponse)
}

// =====================================================================
// UploadChart (multipart)
// =====================================================================

func TestUploadChart_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, pathPrefix+"/apps/upload", r.URL.Path)
		assert.Equal(t, "token-xyz", r.Header.Get("X-Authorization"))
		assert.Equal(t, "alice", r.Header.Get("X-User-ID"))
		assert.Equal(t, "alice", r.Header.Get("X-Bfl-User"))

		// Parse multipart body and validate parts.
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		assert.Equal(t, "multipart/form-data", mediaType)

		mr := multipart.NewReader(r.Body, params["boundary"])
		parts := map[string]string{}
		fileFound := false
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			b, _ := io.ReadAll(p)
			if p.FileName() != "" {
				fileFound = true
				assert.Equal(t, "chart.tgz", p.FileName())
				assert.Equal(t, "binarydata", string(b))
			} else {
				parts[p.FormName()] = string(b)
			}
		}
		assert.True(t, fileFound)
		assert.Equal(t, "upload", parts["source"])

		_, _ = io.WriteString(w, `{"success":true,"data":{"app_data":{"type":"latest","version":"1.0.0"}}}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	resp, err := c.UploadChart(context.Background(), UploadChartInput{
		UserID:    "alice",
		SourceID:  "upload",
		Filename:  "chart.tgz",
		FileBytes: []byte("binarydata"),
		Token:     "token-xyz",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.AppData)
}

func TestUploadChart_MissingFile(t *testing.T) {
	c, _ := NewClient(WithBaseURL("http://x"))
	_, err := c.UploadChart(context.Background(), UploadChartInput{Filename: "x.tgz"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FileBytes")
}

// =====================================================================
// DeleteLocalApp
// =====================================================================

func TestDeleteLocalApp_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, pathPrefix+"/local-apps/delete", r.URL.Path)
		assert.Equal(t, "t", r.Header.Get("X-Authorization"))
		var got DeleteLocalAppRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		assert.Equal(t, "myapp", got.AppName)
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	err := c.DeleteLocalApp(context.Background(), DeleteLocalAppRequest{
		AppName: "myapp", AppVersion: "1.0", SourceID: "upload",
	}, "t", "alice")
	require.NoError(t, err)
}

// =====================================================================
// SyncApp
// =====================================================================

func TestSyncApp_BusinessFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, pathPrefix+"/dcr/sync-app", r.URL.Path)
		_, _ = io.WriteString(w, `{"success":false,"message":"render rejected"}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	_, err := c.SyncApp(context.Background(), SyncAppRequest{SourceID: "s", UserName: "u"})
	require.Error(t, err)
	var be *BusinessError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, "render rejected", be.Message)
}

// =====================================================================
// GetStatus
// =====================================================================

func TestGetStatus_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "alice", q.Get("user"))
		assert.Equal(t, "market.olares", q.Get("source"))
		// include is a comma-separated string; ordering matches the slice ordering.
		assert.Equal(t, "apps,images,tasks", q.Get("include"))
		_, _ = io.WriteString(w, `{"success":true,"data":{"system":{"version":"0.2.0","uptime":42},"apps":[],"images":[],"tasks":null,"last_update":"2024-01-01T00:00:00Z"}}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	st, err := c.GetStatus(context.Background(), "alice", "market.olares",
		[]StatusInclude{StatusIncludeApps, StatusIncludeImages, StatusIncludeTasks})
	require.NoError(t, err)
	require.NotNil(t, st.System)
	assert.Equal(t, "0.2.0", st.System.Version)
}

func TestGetStatus_MissingIDs(t *testing.T) {
	c, _ := NewClient(WithBaseURL("http://x"))
	_, err := c.GetStatus(context.Background(), "", "src", nil)
	require.Error(t, err)
}

// =====================================================================
// Transport / metrics / 404
// =====================================================================

func TestEndpoint_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()
	c, _ := NewClient(WithBaseURL(srv.URL))
	_, err := c.GetVersion(context.Background())
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMetrics_Observed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"success":true,"data":{"version":"0.2.3","build_time":"","git_commit":"","timestamp":0}}`)
	}))
	defer srv.Close()

	sink := &countingMetrics{}
	c, _ := NewClient(WithBaseURL(srv.URL), WithMetrics(sink))
	_, err := c.GetVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&sink.total))
}

type countingMetrics struct {
	total int32
}

func (m *countingMetrics) ObserveRequest(_ string, _ int, _ time.Duration, _ string) {
	atomic.AddInt32(&m.total, 1)
}

// =====================================================================
// Context deadline propagation
// =====================================================================

func TestContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(w, `{"success":true,"data":{}}`)
	}))
	defer srv.Close()

	c, _ := NewClient(WithBaseURL(srv.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := c.GetVersion(ctx)
	require.Error(t, err)
	// We accept either ErrChartRepoUnavailable (transport) or a context
	// error string; the contract is "the request did not complete".
	if !errors.Is(err, ErrChartRepoUnavailable) {
		assert.Contains(t, strings.ToLower(err.Error()), "context")
	}
}
