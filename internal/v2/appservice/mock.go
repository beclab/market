package appservice

import "context"

// MockClient is a hand-written test double for Client. It is in the
// production package (rather than a separate testing/ subpackage) so
// any package that imports appservice for its types can also use the
// mock without taking on a transitive test dependency.
//
// Pattern: each method has a configurable Func field. If set, the
// mock invokes it and returns whatever it returns. If unset, the
// method returns the zero value with nil error. This is the
// lightest-weight pattern that:
//
//   - lets a test mock just the methods it cares about (one liner per
//     method);
//   - keeps the mock's source obvious to a reader (no reflection, no
//     hidden state);
//   - records call counts in fields the test can assert on.
//
// We intentionally do NOT use a generated mock or a heavy framework
// (gomock, mockery): the Client interface is small and stable, the
// hand-written shim is ~100 lines, and tests reading it stay
// auditable. Revisit if Client grows past ~20 methods or if matchers
// get hairy.
//
// Concurrency: the call-count fields are NOT safe under concurrent
// access. Tests that exercise the mock from multiple goroutines must
// add their own synchronization (or use sync/atomic counters).
type MockClient struct {
	// Read endpoints
	GetAllAppsFunc         func(ctx context.Context) ([]*App, error)
	GetMiddlewaresFunc     func(ctx context.Context) ([]*Middleware, error)
	GetTerminusVersionFunc func(ctx context.Context) (string, error)
	GetUserInfoFunc        func(ctx context.Context, token string) (*UserInfo, error)
	GetAppEntranceURLsFunc func(ctx context.Context, appName, user string) (map[string]string, error)

	// Operation endpoints
	InstallAppFunc   func(ctx context.Context, appName string, opts InstallOptions, hdr OpHeaders) (*OpResponse, error)
	CloneAppFunc     func(ctx context.Context, urlAppName string, opts CloneOptions, hdr OpHeaders) (*OpResponse, error)
	UninstallAppFunc func(ctx context.Context, appName string, opts UninstallOptions, hdr OpHeaders) (*OpResponse, error)
	UpgradeAppFunc   func(ctx context.Context, appName string, opts UpgradeOptions, hdr OpHeaders) (*OpResponse, error)
	CancelAppFunc    func(ctx context.Context, appName string, hdr OpHeaders) (*OpResponse, error)

	// Per-method invocation counts. Tests assert on these to verify
	// "the code under test made exactly N calls to X". Pre-allocated
	// here (rather than created lazily) so a test that never calls a
	// method still sees count = 0 instead of a missing-key surprise.
	GetAllAppsCalls         int
	GetMiddlewaresCalls     int
	GetTerminusVersionCalls int
	GetUserInfoCalls        int
	GetAppEntranceURLsCalls int
	InstallAppCalls         int
	CloneAppCalls           int
	UninstallAppCalls       int
	UpgradeAppCalls         int
	CancelAppCalls          int
}

// Compile-time guarantee that MockClient satisfies Client. Catches
// signature drift the moment Client gains a method we forgot to mock.
var _ Client = (*MockClient)(nil)

// ----- Client interface implementation -----

func (m *MockClient) GetAllApps(ctx context.Context) ([]*App, error) {
	m.GetAllAppsCalls++
	if m.GetAllAppsFunc != nil {
		return m.GetAllAppsFunc(ctx)
	}
	return nil, nil
}

func (m *MockClient) GetMiddlewares(ctx context.Context) ([]*Middleware, error) {
	m.GetMiddlewaresCalls++
	if m.GetMiddlewaresFunc != nil {
		return m.GetMiddlewaresFunc(ctx)
	}
	return nil, nil
}

func (m *MockClient) GetTerminusVersion(ctx context.Context) (string, error) {
	m.GetTerminusVersionCalls++
	if m.GetTerminusVersionFunc != nil {
		return m.GetTerminusVersionFunc(ctx)
	}
	return "", nil
}

func (m *MockClient) GetUserInfo(ctx context.Context, token string) (*UserInfo, error) {
	m.GetUserInfoCalls++
	if m.GetUserInfoFunc != nil {
		return m.GetUserInfoFunc(ctx, token)
	}
	return nil, nil
}

func (m *MockClient) GetAppEntranceURLs(ctx context.Context, appName, user string) (map[string]string, error) {
	m.GetAppEntranceURLsCalls++
	if m.GetAppEntranceURLsFunc != nil {
		return m.GetAppEntranceURLsFunc(ctx, appName, user)
	}
	return nil, nil
}

func (m *MockClient) InstallApp(ctx context.Context, appName string, opts InstallOptions, hdr OpHeaders) (*OpResponse, error) {
	m.InstallAppCalls++
	if m.InstallAppFunc != nil {
		return m.InstallAppFunc(ctx, appName, opts, hdr)
	}
	return nil, nil
}

func (m *MockClient) CloneApp(ctx context.Context, urlAppName string, opts CloneOptions, hdr OpHeaders) (*OpResponse, error) {
	m.CloneAppCalls++
	if m.CloneAppFunc != nil {
		return m.CloneAppFunc(ctx, urlAppName, opts, hdr)
	}
	return nil, nil
}

func (m *MockClient) UninstallApp(ctx context.Context, appName string, opts UninstallOptions, hdr OpHeaders) (*OpResponse, error) {
	m.UninstallAppCalls++
	if m.UninstallAppFunc != nil {
		return m.UninstallAppFunc(ctx, appName, opts, hdr)
	}
	return nil, nil
}

func (m *MockClient) UpgradeApp(ctx context.Context, appName string, opts UpgradeOptions, hdr OpHeaders) (*OpResponse, error) {
	m.UpgradeAppCalls++
	if m.UpgradeAppFunc != nil {
		return m.UpgradeAppFunc(ctx, appName, opts, hdr)
	}
	return nil, nil
}

func (m *MockClient) CancelApp(ctx context.Context, appName string, hdr OpHeaders) (*OpResponse, error) {
	m.CancelAppCalls++
	if m.CancelAppFunc != nil {
		return m.CancelAppFunc(ctx, appName, hdr)
	}
	return nil, nil
}
