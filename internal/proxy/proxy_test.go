package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type mockRuntime struct {
	active bool
}

func (m *mockRuntime) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *mockRuntime) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *mockRuntime) Start(ctx context.Context, name string) error { m.active = true; return nil }
func (m *mockRuntime) Stop(ctx context.Context, name string) error { m.active = false; return nil }
func (m *mockRuntime) Remove(ctx context.Context, name string) error { return nil }
func (m *mockRuntime) IsActive(ctx context.Context, name string) (bool, error) { return m.active, nil }
func (m *mockRuntime) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *mockRuntime) Logs(ctx context.Context, name string, opts runtime.LogOptions) (io.ReadCloser, error) { return nil, nil }
func (m *mockRuntime) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error { return nil }
func (m *mockRuntime) RemoveBySuffix(ctx context.Context, name string, suffix string) error { return nil }
func (m *mockRuntime) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) { return 0, nil }
func (m *mockRuntime) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }
func (m *mockRuntime) Restart(ctx context.Context, name string) error { return nil }
func (m *mockRuntime) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error { return nil }
func (m *mockRuntime) RemoveImage(ctx context.Context, imageTag string) error { return nil }
func (m *mockRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error { return nil }
func (m *mockRuntime) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts runtime.RunOptions) error { return nil }

func TestExtractApp(t *testing.T) {
	p := New(nil, 8080)
	tests := []struct {
		host string
		want string
	}{
		{"myapp.tengiz.local", "myapp"},
		{"myapp.tengiz.local:8080", "myapp"},
		{"tengiz.local", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := p.extractApp(tt.host)
		if got != tt.want {
			t.Errorf("extractApp(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestRegisterAndServe(t *testing.T) {
	mock := &mockRuntime{active: true}
	p := New(mock, 8080)
	p.Register("testapp", 19999)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "testapp.tengiz.local"
	w := httptest.NewRecorder()

	p.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusOK {
		t.Errorf("unexpected status: %d", resp.StatusCode)
	}
}

func TestIdleResetOnRequest(t *testing.T) {
	mock := &mockRuntime{active: true}
	p := New(mock, 8080)
	p.Register("testapp", 19999)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "testapp.tengiz.local"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
}

// adminPortMu prevents parallel admin server port conflicts
var adminPortMu = make(chan struct{}, 1)

func init() {
	adminPortMu <- struct{}{}
}

func TestAdminRegisterEndpoint(t *testing.T) {
	<-adminPortMu

	ctx, cancel := context.WithCancel(context.Background())
	mock := &mockRuntime{active: true}
	p := New(mock, 8080)
	p.StartAdmin(ctx)

	defer func() {
		cancel()
		p.StopAdmin()
		adminPortMu <- struct{}{}
	}()

	// Wait for admin server to start
	var resp *http.Response
	var err error
	body := `{"app":"testapp","port":9001}`
	for i := 0; i < 20; i++ {
		time.Sleep(10 * time.Millisecond)
		resp, err = http.Post("http://127.0.0.1:9099/register", "application/json", strings.NewReader(body))
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	p.mu.RLock()
	_, ok := p.routes["testapp"]
	p.mu.RUnlock()
	if !ok {
		t.Error("route not registered after admin API call")
	}
}

func TestExtractAppWithCustomDomain(t *testing.T) {
	p := New(nil, 8080)
	p.RegisterDomain("app.example.com", "myapp")
	p.RegisterDomain("myapp.org", "myapp")

	tests := []struct {
		host string
		want string
	}{
		{"app.example.com", "myapp"},
		{"myapp.org", "myapp"},
		{"myapp.tengiz.local", "myapp"},
		{"tengiz.local", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := p.extractApp(tt.host)
		if got != tt.want {
			t.Errorf("extractApp(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestExtractAppEnvSubdomain(t *testing.T) {
	p := New(nil, 8080)
	p.Register("myapp-staging", 9001)
	app := p.extractApp("myapp-staging.tengiz.local:8080")
	if app != "myapp-staging" {
		t.Errorf("extractApp(%q) = %q, want %q", "myapp-staging.tengiz.local:8080", app, "myapp-staging")
	}
}

func TestExtractAppPreviewSubdomain(t *testing.T) {
	p := New(nil, 8080)
	p.Register("pr-42.myapp", 9100)

	app := p.extractApp("pr-42.myapp.tengiz.local:8080")
	if app != "pr-42.myapp" {
		t.Errorf("extractApp(%q) = %q, want %q", "pr-42.myapp.tengiz.local:8080", app, "pr-42.myapp")
	}
}

func TestExtractAppRegularSubdomain(t *testing.T) {
	p := New(nil, 8080)
	p.Register("myapp", 9001)

	app := p.extractApp("myapp.tengiz.local:8080")
	if app != "myapp" {
		t.Errorf("extractApp(%q) = %q, want %q", "myapp.tengiz.local:8080", app, "myapp")
	}
}

func TestRegisterDomainAndUnregisterDomain(t *testing.T) {
	p := New(nil, 8080)
	p.Register("testapp", 19999)

	p.RegisterDomain("example.com", "testapp")

	// Should route via domain
	p.mu.RLock()
	_, hasRoute := p.routes["testapp"]
	domainRoute, hasDomain := p.domains["example.com"]
	p.mu.RUnlock()

	if !hasRoute {
		t.Error("expected route to exist")
	}
	if !hasDomain {
		t.Error("expected domain to be registered")
	}
	if domainRoute != "testapp" {
		t.Errorf("domain route = %q, want testapp", domainRoute)
	}

	// extractApp should find it
	got := p.extractApp("example.com")
	if got != "testapp" {
		t.Errorf("extractApp(example.com) = %q, want testapp", got)
	}

	// Unregister domain
	p.UnregisterDomain("example.com")
	p.mu.RLock()
	_, hasDomainAfter := p.domains["example.com"]
	p.mu.RUnlock()

	if hasDomainAfter {
		t.Error("expected domain to be unregistered")
	}

	// extractApp should fall through
	got = p.extractApp("example.com")
	if got != "" {
		t.Errorf("extractApp after unregister = %q, want empty", got)
	}
}

func TestAdminAddDomainEndpoint(t *testing.T) {
	<-adminPortMu

	ctx, cancel := context.WithCancel(context.Background())
	mock := &mockRuntime{active: true}
	p := New(mock, 8080)
	p.Register("testapp", 9001)
	p.StartAdmin(ctx)

	defer func() {
		cancel()
		p.StopAdmin()
		adminPortMu <- struct{}{}
	}()

	var resp *http.Response
	var err error
	body := `{"domain":"example.com","app":"testapp"}`
	for i := 0; i < 20; i++ {
		time.Sleep(10 * time.Millisecond)
		resp, err = http.Post("http://127.0.0.1:9099/add-domain", "application/json", strings.NewReader(body))
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	p.mu.RLock()
	app, ok := p.domains["example.com"]
	p.mu.RUnlock()
	if !ok {
		t.Error("domain not registered after admin API call")
	}
	if app != "testapp" {
		t.Errorf("domain maps to %q, want testapp", app)
	}
}

func TestAdminRemoveDomainEndpoint(t *testing.T) {
	<-adminPortMu

	ctx, cancel := context.WithCancel(context.Background())
	mock := &mockRuntime{active: true}
	p := New(mock, 8080)
	p.Register("testapp", 9001)
	p.RegisterDomain("example.com", "testapp")
	p.StartAdmin(ctx)

	defer func() {
		cancel()
		p.StopAdmin()
		adminPortMu <- struct{}{}
	}()

	var resp *http.Response
	var err error
	for i := 0; i < 20; i++ {
		time.Sleep(10 * time.Millisecond)
		body := `{"domain":"example.com"}`
		req, _ := http.NewRequest("DELETE", "http://127.0.0.1:9099/remove-domain", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err = http.DefaultClient.Do(req)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	p.mu.RLock()
	_, ok := p.domains["example.com"]
	p.mu.RUnlock()
	if ok {
		t.Error("domain still registered after remove")
	}
}

func TestAdminUnregisterEndpoint(t *testing.T) {
	<-adminPortMu

	ctx, cancel := context.WithCancel(context.Background())
	mock := &mockRuntime{active: true}
	p := New(mock, 8080)
	p.StartAdmin(ctx)
	p.Register("testapp", 9001)

	defer func() {
		cancel()
		p.StopAdmin()
		adminPortMu <- struct{}{}
	}()

	var resp *http.Response
	var err error
	for i := 0; i < 20; i++ {
		time.Sleep(10 * time.Millisecond)
		body := `{"app":"testapp"}`
		req, _ := http.NewRequest("DELETE", "http://127.0.0.1:9099/unregister", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err = http.DefaultClient.Do(req)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	p.mu.RLock()
	_, ok := p.routes["testapp"]
	p.mu.RUnlock()
	if ok {
		t.Error("route still registered after unregister")
	}
}
