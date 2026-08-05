package housekeeping

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type recordingRuntime struct {
	prunes atomic.Int32
}

func (r *recordingRuntime) Prune(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	r.prunes.Add(1)
	return runtime.CleanupReport{}, nil
}
func (r *recordingRuntime) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (r *recordingRuntime) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (r *recordingRuntime) Start(ctx context.Context, name string) error { return nil }
func (r *recordingRuntime) Stop(ctx context.Context, name string) error { return nil }
func (r *recordingRuntime) Remove(ctx context.Context, name string) error { return nil }
func (r *recordingRuntime) IsActive(ctx context.Context, name string) (bool, error) { return false, nil }
func (r *recordingRuntime) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (r *recordingRuntime) Logs(ctx context.Context, name string, opts runtime.LogOptions) (io.ReadCloser, error) { return nil, nil }
func (r *recordingRuntime) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error { return nil }
func (r *recordingRuntime) RemoveBySuffix(ctx context.Context, name string, suffix string) error { return nil }
func (r *recordingRuntime) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) { return 0, nil }
func (r *recordingRuntime) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }
func (r *recordingRuntime) Restart(ctx context.Context, name string) error { return nil }
func (r *recordingRuntime) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error { return nil }
func (r *recordingRuntime) RemoveImage(ctx context.Context, imageTag string) error { return nil }
func (r *recordingRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error { return nil }
func (r *recordingRuntime) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts runtime.RunOptions) error { return nil }

func TestStartPrunesOnInterval(t *testing.T) {
	r := &recordingRuntime{}
	s := New(r, 30*time.Millisecond, runtime.CleanupOptions{})
	s.Start()
	defer s.Stop()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if r.prunes.Load() >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected >=2 prunes, got %d", r.prunes.Load())
}

func TestStopHaltsPruning(t *testing.T) {
	r := &recordingRuntime{}
	s := New(r, 20*time.Millisecond, runtime.CleanupOptions{})
	s.Start()
	time.Sleep(60 * time.Millisecond)
	s.Stop()
	idle := r.prunes.Load()
	if idle == 0 {
		t.Fatal("expected at least one prune before stop")
	}
	time.Sleep(60 * time.Millisecond)
	if after := r.prunes.Load(); after != idle {
		t.Fatalf("prunes continued after Stop: got %d want %d", after, idle)
	}
}
