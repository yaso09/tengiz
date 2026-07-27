package housekeeping

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type mockStore struct {
	apps []types.AppEntry
}

func (m *mockStore) ListApps() ([]types.AppEntry, error) {
	return m.apps, nil
}

type mockRuntime struct {
	runtime.Manager
	pruneContainersCalled bool
	pruneImagesCalled     bool
	pruneVolumesCalled    bool
	pruneNetworksCalled   bool
	pruneBuildCacheCalled bool
	returnStats           runtime.PruneStats
	returnErr             error
}

func (m *mockRuntime) PruneContainers(ctx context.Context) (runtime.PruneStats, error) {
	m.pruneContainersCalled = true
	return m.returnStats, m.returnErr
}

func (m *mockRuntime) PruneImages(ctx context.Context, all bool) (runtime.PruneStats, error) {
	m.pruneImagesCalled = true
	return m.returnStats, m.returnErr
}

func (m *mockRuntime) PruneVolumes(ctx context.Context) (runtime.PruneStats, error) {
	m.pruneVolumesCalled = true
	return m.returnStats, m.returnErr
}

func (m *mockRuntime) PruneNetworks(ctx context.Context) (runtime.PruneStats, error) {
	m.pruneNetworksCalled = true
	return m.returnStats, m.returnErr
}

func (m *mockRuntime) PruneBuildCache(ctx context.Context) (runtime.PruneStats, error) {
	m.pruneBuildCacheCalled = true
	return m.returnStats, m.returnErr
}

func TestHousekeeperRunDefaults(t *testing.T) {
	rt := &mockRuntime{returnStats: runtime.PruneStats{ItemsRemoved: 0}}
	st := &mockStore{}

	h := New(rt, st)
	report, err := h.Run(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if !rt.pruneContainersCalled {
		t.Error("expected PruneContainers to be called")
	}
	if !rt.pruneImagesCalled {
		t.Error("expected PruneImages to be called")
	}
	if rt.pruneVolumesCalled {
		t.Error("PruneVolumes should NOT be called by default")
	}
	if rt.pruneNetworksCalled {
		t.Error("PruneNetworks should NOT be called by default")
	}
	if rt.pruneBuildCacheCalled {
		t.Error("PruneBuildCache should NOT be called by default")
	}
}

func TestHousekeeperRunAll(t *testing.T) {
	rt := &mockRuntime{returnStats: runtime.PruneStats{ItemsRemoved: 3}}
	st := &mockStore{}

	h := New(rt, st)
	report, err := h.Run(context.Background(), CleanupOptions{All: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !rt.pruneContainersCalled {
		t.Error("expected PruneContainers to be called with --all")
	}
	if !rt.pruneImagesCalled {
		t.Error("expected PruneImages to be called with --all")
	}
	if !rt.pruneVolumesCalled {
		t.Error("expected PruneVolumes to be called with --all")
	}
	if !rt.pruneNetworksCalled {
		t.Error("expected PruneNetworks to be called with --all")
	}
	if !rt.pruneBuildCacheCalled {
		t.Error("expected PruneBuildCache to be called with --all")
	}
	if report.ItemsRemoved() != 15 {
		t.Errorf("expected 15 total items removed, got %d", report.ItemsRemoved())
	}
}

func TestHousekeeperDryRun(t *testing.T) {
	rt := &mockRuntime{returnStats: runtime.PruneStats{ItemsRemoved: 0}}
	st := &mockStore{}

	h := New(rt, st)
	report, err := h.DryRun(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if rt.pruneContainersCalled {
		t.Error("PruneContainers should NOT be called during dry run")
	}
	if !report.DryRun {
		t.Error("expected DryRun flag in report")
	}
}

func TestHousekeeperPerApp(t *testing.T) {
	rt := &mockRuntime{returnStats: runtime.PruneStats{ItemsRemoved: 1}}
	st := &mockStore{
		apps: []types.AppEntry{
			{Name: "myapp", Port: 9001},
		},
	}

	h := New(rt, st)
	report, err := h.Run(context.Background(), CleanupOptions{AppName: "myapp"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !rt.pruneContainersCalled {
		t.Error("expected PruneContainers to be called")
	}
	_ = report
}
