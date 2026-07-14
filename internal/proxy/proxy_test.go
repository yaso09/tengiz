package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
