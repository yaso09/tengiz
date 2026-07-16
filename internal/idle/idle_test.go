package idle

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type mockRuntime struct {
	stopped atomic.Bool
}

func (m *mockRuntime) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *mockRuntime) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *mockRuntime) Start(ctx context.Context, name string) error { return nil }
func (m *mockRuntime) Stop(ctx context.Context, name string) error { m.stopped.Store(true); return nil }
func (m *mockRuntime) Remove(ctx context.Context, name string) error { return nil }
func (m *mockRuntime) IsActive(ctx context.Context, name string) (bool, error) { return false, nil }
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

func TestResetExtendsTimer(t *testing.T) {
	mock := &mockRuntime{}
	mgr := New(mock, 50*time.Millisecond)

	mgr.Reset("testapp")
	time.Sleep(30 * time.Millisecond)
	mgr.Reset("testapp") // reset before expiry
	time.Sleep(30 * time.Millisecond)
	mgr.Reset("testapp") // reset again
	time.Sleep(30 * time.Millisecond)

	if mock.stopped.Load() {
		t.Error("app stopped too early, Reset() did not extend timer")
	}

	// wait for final timer to expire
	time.Sleep(60 * time.Millisecond)
	if !mock.stopped.Load() {
		t.Error("app was not stopped after idle timeout")
	}
}

func TestStopPreventsTimeout(t *testing.T) {
	mock := &mockRuntime{}
	mgr := New(mock, 50*time.Millisecond)

	mgr.Reset("testapp")
	mgr.Stop("testapp")
	time.Sleep(100 * time.Millisecond)

	if mock.stopped.Load() {
		t.Error("app was stopped despite Stop() being called")
	}
}
