package deployhealth

import (
	"context"
	"errors"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type gateMockRT struct {
	runtime.Manager
	healthErr    error
	readyErr     error
	removeErr    error
	readyCalls   int
	healthCalls  int
	removed      []string
	removeSuffix string
}

func newGateMockRT() *gateMockRT {
	return &gateMockRT{Manager: runtime.NewStub()}
}

func (m *gateMockRT) WaitForReady(ctx context.Context, name string, internalPort int) error {
	m.readyCalls++
	return m.readyErr
}

func (m *gateMockRT) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error {
	m.healthCalls++
	return m.healthErr
}

func (m *gateMockRT) RemoveBySuffix(ctx context.Context, name string, suffix string) error {
	m.removed = append(m.removed, name)
	m.removeSuffix = suffix
	return m.removeErr
}

func TestWaitUsesWaitForReadyWhenHealthDisabled(t *testing.T) {
	rt := newGateMockRT()
	cfg := &types.AppConfig{Name: "myapp"}
	err := Wait(context.Background(), rt, cfg, "tengiz-myapp-123", 8080)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if rt.readyCalls != 1 || rt.healthCalls != 0 {
		t.Errorf("Wait() readyCalls=%d healthCalls=%d, want 1/0", rt.readyCalls, rt.healthCalls)
	}
}

func TestWaitUsesWaitForHealthWhenEnabled(t *testing.T) {
	rt := newGateMockRT()
	cfg := &types.AppConfig{
		Name:        "myapp",
		HealthCheck: &types.HealthCheckConfig{Enabled: true, Endpoint: "/health"},
	}
	err := Wait(context.Background(), rt, cfg, "tengiz-myapp-123", 8080)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if rt.healthCalls != 1 || rt.readyCalls != 0 {
		t.Errorf("Wait() readyCalls=%d healthCalls=%d, want 0/1", rt.readyCalls, rt.healthCalls)
	}
}

func TestWaitHealthFailurePropagates(t *testing.T) {
	rt := newGateMockRT()
	rt.healthErr = errors.New("health check failed after 30 attempts: connection refused")
	cfg := &types.AppConfig{
		Name:        "myapp",
		HealthCheck: &types.HealthCheckConfig{Enabled: true},
	}
	err := Wait(context.Background(), rt, cfg, "tengiz-myapp-123", 8080)
	if err == nil {
		t.Fatal("expected WaitForHealth error to propagate")
	}
}

func TestAbortRemovesContainerAndFreesPort(t *testing.T) {
	store := config.NewStore(t.TempDir())
	port, err := store.AllocatePort("myapp")
	if err != nil {
		t.Fatal(err)
	}
	rt := newGateMockRT()
	err = Abort(context.Background(), rt, store, "myapp", "tengiz-myapp", "999", "tengiz-apps/myapp:v1", port)
	if err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
	if len(rt.removed) != 1 || rt.removed[0] != "tengiz-myapp" || rt.removeSuffix != "999" {
		t.Errorf("RemoveBySuffix called with %v suffix=%q, want [tengiz-myapp] suffix=999", rt.removed, rt.removeSuffix)
	}
	got, err := store.AllocatePort("other")
	if err != nil {
		t.Fatal(err)
	}
	if got != port {
		t.Errorf("port %d not freed, reallocated %d", port, got)
	}
	deps, err := store.GetDeployments("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0].Status != string(types.DeployFailed) {
		t.Errorf("deployments = %+v, want 1 entry with status %q", deps, types.DeployFailed)
	}
}

func TestAbortPropagatesRemoveError(t *testing.T) {
	store := config.NewStore(t.TempDir())
	port, _ := store.AllocatePort("myapp")
	rt := newGateMockRT()
	rt.removeErr = errors.New("docker rm failed")
	err := Abort(context.Background(), rt, store, "myapp", "tengiz-myapp", "999", "tengiz-apps/myapp:v1", port)
	if err == nil {
		t.Fatal("expected RemoveBySuffix error to propagate")
	}
}
