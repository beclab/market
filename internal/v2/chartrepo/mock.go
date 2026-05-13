package chartrepo

import "context"

// MockClient is a hand-written test double for Client. It is in the
// production package (rather than a separate testing/ subpackage) so
// any package that imports chartrepo for its types can also use the
// mock without taking on a transitive test dependency.
//
// Pattern: each method has a configurable Func field. If set, the
// mock invokes it and returns whatever it returns. If unset, the
// method returns the zero value with nil error. This is the lightest-
// weight pattern that:
//
//   - lets a test mock just the methods it cares about (one liner per
//     method);
//   - keeps the mock's source obvious to a reader (no reflection, no
//     hidden state);
//   - records call counts in fields the test can assert on.
//
// We intentionally do NOT use a generated mock or a heavy framework
// (gomock, mockery): the Client interface is small and stable, the
// hand-written shim is short, and tests reading it stay auditable.
//
// Concurrency: the call-count fields are NOT safe under concurrent
// access. Tests that exercise the mock from multiple goroutines must
// add their own synchronization (or use sync/atomic counters).
type MockClient struct {
	GetVersionFunc                      func(ctx context.Context) (*VersionInfo, error)
	GetMarketSourcesFunc                func(ctx context.Context) (*MarketSourcesConfig, error)
	AddMarketSourceFunc                 func(ctx context.Context, src *MarketSource) error
	DeleteMarketSourceFunc              func(ctx context.Context, sourceID string) error
	GetRepoDataFunc                     func(ctx context.Context) (*RepoData, error)
	GetStateChangesFunc                 func(ctx context.Context, afterID int64, limit int) (*StateChangesData, error)
	GetAppInfoFunc                      func(ctx context.Context, in AppsRequest) ([]map[string]any, error)
	GetImageInfoFunc                    func(ctx context.Context, imageName string) (map[string]any, error)
	GetAppVersionForDownloadHistoryFunc func(ctx context.Context, userID, appName string) (string, string, error)
	UploadChartFunc                     func(ctx context.Context, in UploadChartInput) (*UploadChartResponseData, error)
	DeleteLocalAppFunc                  func(ctx context.Context, in DeleteLocalAppRequest, token, userID string) error
	SyncAppFunc                         func(ctx context.Context, req SyncAppRequest) (*SyncAppData, error)
	GetStatusFunc                       func(ctx context.Context, userID, sourceID string, include []StatusInclude) (*Status, error)

	GetVersionCalls                      int
	GetMarketSourcesCalls                int
	AddMarketSourceCalls                 int
	DeleteMarketSourceCalls              int
	GetRepoDataCalls                     int
	GetStateChangesCalls                 int
	GetAppInfoCalls                      int
	GetImageInfoCalls                    int
	GetAppVersionForDownloadHistoryCalls int
	UploadChartCalls                     int
	DeleteLocalAppCalls                  int
	SyncAppCalls                         int
	GetStatusCalls                       int
}

// Compile-time guarantee that MockClient satisfies Client. Catches
// signature drift the moment Client gains a method we forgot to mock.
var _ Client = (*MockClient)(nil)

func (m *MockClient) GetVersion(ctx context.Context) (*VersionInfo, error) {
	m.GetVersionCalls++
	if m.GetVersionFunc != nil {
		return m.GetVersionFunc(ctx)
	}
	return nil, nil
}

func (m *MockClient) GetMarketSources(ctx context.Context) (*MarketSourcesConfig, error) {
	m.GetMarketSourcesCalls++
	if m.GetMarketSourcesFunc != nil {
		return m.GetMarketSourcesFunc(ctx)
	}
	return nil, nil
}

func (m *MockClient) AddMarketSource(ctx context.Context, src *MarketSource) error {
	m.AddMarketSourceCalls++
	if m.AddMarketSourceFunc != nil {
		return m.AddMarketSourceFunc(ctx, src)
	}
	return nil
}

func (m *MockClient) DeleteMarketSource(ctx context.Context, sourceID string) error {
	m.DeleteMarketSourceCalls++
	if m.DeleteMarketSourceFunc != nil {
		return m.DeleteMarketSourceFunc(ctx, sourceID)
	}
	return nil
}

func (m *MockClient) GetRepoData(ctx context.Context) (*RepoData, error) {
	m.GetRepoDataCalls++
	if m.GetRepoDataFunc != nil {
		return m.GetRepoDataFunc(ctx)
	}
	return nil, nil
}

func (m *MockClient) GetStateChanges(ctx context.Context, afterID int64, limit int) (*StateChangesData, error) {
	m.GetStateChangesCalls++
	if m.GetStateChangesFunc != nil {
		return m.GetStateChangesFunc(ctx, afterID, limit)
	}
	return nil, nil
}

func (m *MockClient) GetAppInfo(ctx context.Context, in AppsRequest) ([]map[string]any, error) {
	m.GetAppInfoCalls++
	if m.GetAppInfoFunc != nil {
		return m.GetAppInfoFunc(ctx, in)
	}
	return nil, nil
}

func (m *MockClient) GetImageInfo(ctx context.Context, imageName string) (map[string]any, error) {
	m.GetImageInfoCalls++
	if m.GetImageInfoFunc != nil {
		return m.GetImageInfoFunc(ctx, imageName)
	}
	return nil, nil
}

func (m *MockClient) GetAppVersionForDownloadHistory(ctx context.Context, userID, appName string) (string, string, error) {
	m.GetAppVersionForDownloadHistoryCalls++
	if m.GetAppVersionForDownloadHistoryFunc != nil {
		return m.GetAppVersionForDownloadHistoryFunc(ctx, userID, appName)
	}
	return "", "", nil
}

func (m *MockClient) UploadChart(ctx context.Context, in UploadChartInput) (*UploadChartResponseData, error) {
	m.UploadChartCalls++
	if m.UploadChartFunc != nil {
		return m.UploadChartFunc(ctx, in)
	}
	return nil, nil
}

func (m *MockClient) DeleteLocalApp(ctx context.Context, in DeleteLocalAppRequest, token, userID string) error {
	m.DeleteLocalAppCalls++
	if m.DeleteLocalAppFunc != nil {
		return m.DeleteLocalAppFunc(ctx, in, token, userID)
	}
	return nil
}

func (m *MockClient) SyncApp(ctx context.Context, req SyncAppRequest) (*SyncAppData, error) {
	m.SyncAppCalls++
	if m.SyncAppFunc != nil {
		return m.SyncAppFunc(ctx, req)
	}
	return nil, nil
}

func (m *MockClient) GetStatus(ctx context.Context, userID, sourceID string, include []StatusInclude) (*Status, error) {
	m.GetStatusCalls++
	if m.GetStatusFunc != nil {
		return m.GetStatusFunc(ctx, userID, sourceID, include)
	}
	return nil, nil
}
