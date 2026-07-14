package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

type mockRuntime struct {
	active bool
}

func (m *mockRuntime) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *mockRuntime) Start(ctx context.Context, name string) error { m.active = true; return nil }
func (m *mockRuntime) Stop(ctx context.Context, name string) error { m.active = false; return nil }
func (m *mockRuntime) Remove(ctx context.Context, name string) error { return nil }
func (m *mockRuntime) IsActive(ctx context.Context, name string) (bool, error) { return m.active, nil }
func (m *mockRuntime) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *mockRuntime) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) { return nil, nil }
func (m *mockRuntime) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error { return nil }
func (m *mockRuntime) RemoveBySuffix(ctx context.Context, name string, suffix string) error { return nil }
func (m *mockRuntime) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) { return 0, nil }
func (m *mockRuntime) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }

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
