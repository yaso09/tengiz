package cleanup

import (
	"context"
	"io"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type fakeManager struct {
	containersCalls int
	imagesCalls     int
	volumesCalls    int
	networksCalls   int
	keepCalls       int
	keepApps        []string
	keepN           []int
	containersRet   int
	imagesRet       int
	volumesRet      int
	networksRet     int
	before          runtime.DockerDiskInfo
	after           runtime.DockerDiskInfo
	diskCalls       int
}

func (m *fakeManager) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *fakeManager) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *fakeManager) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error { return nil }
func (m *fakeManager) Start(ctx context.Context, name string) error { return nil }
func (m *fakeManager) Stop(ctx context.Context, name string) error { return nil }
func (m *fakeManager) Restart(ctx context.Context, name string) error { return nil }
func (m *fakeManager) Remove(ctx context.Context, name string) error { return nil }
func (m *fakeManager) RemoveBySuffix(ctx context.Context, name string, suffix string) error { return nil }
func (m *fakeManager) IsActive(ctx context.Context, name string) (bool, error) { return true, nil }
func (m *fakeManager) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) { return 0, nil }
func (m *fakeManager) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *fakeManager) Logs(ctx context.Context, name string, opts runtime.LogOptions) (io.ReadCloser, error) { return nil, nil }
func (m *fakeManager) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }
func (m *fakeManager) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error { return nil }
func (m *fakeManager) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts runtime.RunOptions) error { return nil }
func (m *fakeManager) RemoveImage(ctx context.Context, imageTag string) error { return nil }
func (m *fakeManager) KeepLastNImages(ctx context.Context, appName string, n int) error {
	m.keepCalls++
	m.keepApps = append(m.keepApps, appName)
	m.keepN = append(m.keepN, n)
	return nil
}
func (m *fakeManager) PruneContainers(ctx context.Context) (int, error) { m.containersCalls++; return m.containersRet, nil }
func (m *fakeManager) PruneImages(ctx context.Context) (int, error)     { m.imagesCalls++; return m.imagesRet, nil }
func (m *fakeManager) PruneVolumes(ctx context.Context) (int, error)    { m.volumesCalls++; return m.volumesRet, nil }
func (m *fakeManager) PruneNetworks(ctx context.Context) (int, error)   { m.networksCalls++; return m.networksRet, nil }
func (m *fakeManager) DockerDiskInfo(ctx context.Context) (runtime.DockerDiskInfo, error) {
	m.diskCalls++
	if m.diskCalls > 1 {
		return m.after, nil
	}
	return m.before, nil
}

func TestRunDefaultsToAllCategories(t *testing.T) {
	fake := &fakeManager{}
	c := New(fake, config.NewStore(t.TempDir()))
	c.Run(context.Background(), Options{})
	if fake.containersCalls != 1 || fake.imagesCalls != 1 || fake.volumesCalls != 1 || fake.networksCalls != 1 {
		t.Errorf("expected all prune categories called once, got containers=%d images=%d volumes=%d networks=%d",
			fake.containersCalls, fake.imagesCalls, fake.volumesCalls, fake.networksCalls)
	}
}

func TestRunSelectiveCategories(t *testing.T) {
	fake := &fakeManager{}
	c := New(fake, config.NewStore(t.TempDir()))
	c.Run(context.Background(), Options{Containers: true})
	if fake.containersCalls != 1 {
		t.Errorf("containersCalls = %d, want 1", fake.containersCalls)
	}
	if fake.imagesCalls != 0 || fake.volumesCalls != 0 || fake.networksCalls != 0 {
		t.Errorf("unexpected prune calls for unselected categories: images=%d volumes=%d networks=%d",
			fake.imagesCalls, fake.volumesCalls, fake.networksCalls)
	}
}

func TestRunAggregatesCounts(t *testing.T) {
	fake := &fakeManager{
		containersRet: 3,
		imagesRet:     4,
		volumesRet:    5,
		networksRet:   6,
	}
	c := New(fake, config.NewStore(t.TempDir()))
	res := c.Run(context.Background(), Options{})
	if res.ContainersRemoved != 3 || res.ImagesRemoved != 4 || res.VolumesRemoved != 5 || res.NetworksRemoved != 6 {
		t.Errorf("aggregated counts wrong: %+v", res)
	}
}

func TestRunAppliesRetentionAcrossApps(t *testing.T) {
	fake := &fakeManager{}
	store := config.NewStore(t.TempDir())
	if err := store.SaveApp(types.AppEntry{Name: "beta"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveApp(types.AppEntry{Name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	c := New(fake, store)
	res := c.Run(context.Background(), Options{Images: true, Retention: 3})
	if fake.keepCalls != 2 {
		t.Fatalf("keepCalls = %d, want 2", fake.keepCalls)
	}
	if len(res.RetentionApps) != 2 || res.RetentionApps[0] != "alpha" || res.RetentionApps[1] != "beta" {
		t.Errorf("RetentionApps = %v, want [alpha beta]", res.RetentionApps)
	}
	for _, n := range fake.keepN {
		if n != 3 {
			t.Errorf("KeepLastNImages retention = %d, want 3", n)
		}
	}
}

func TestRunRetentionDefaultsToFive(t *testing.T) {
	fake := &fakeManager{}
	store := config.NewStore(t.TempDir())
	if err := store.SaveApp(types.AppEntry{Name: "myapp"}); err != nil {
		t.Fatal(err)
	}
	c := New(fake, store)
	c.Run(context.Background(), Options{Containers: true})
	if fake.keepCalls != 1 {
		t.Fatalf("keepCalls = %d, want 1", fake.keepCalls)
	}
	if fake.keepN[0] != 5 {
		t.Errorf("KeepLastNImages n = %d, want default 5", fake.keepN[0])
	}
}

func TestRunDryRunPerformsNoMutations(t *testing.T) {
	fake := &fakeManager{before: runtime.DockerDiskInfo{TotalReclaimBytes: 100}}
	store := config.NewStore(t.TempDir())
	if err := store.SaveApp(types.AppEntry{Name: "myapp"}); err != nil {
		t.Fatal(err)
	}
	c := New(fake, store)
	res := c.Run(context.Background(), Options{DryRun: true})
	if fake.containersCalls != 0 || fake.imagesCalls != 0 || fake.volumesCalls != 0 || fake.networksCalls != 0 {
		t.Errorf("dry run performed prune mutations: containers=%d images=%d volumes=%d networks=%d",
			fake.containersCalls, fake.imagesCalls, fake.volumesCalls, fake.networksCalls)
	}
	if fake.keepCalls != 0 {
		t.Errorf("dry run performed retention mutation, keepCalls = %d", fake.keepCalls)
	}
	if res.DryRun != true {
		t.Error("res.DryRun = false, want true")
	}
	if res.ReclaimedBytes != 100 {
		t.Errorf("ReclaimedBytes = %d, want 100 (reclaimable total)", res.ReclaimedBytes)
	}
}

func TestRunReportsReclaimedDiff(t *testing.T) {
	fake := &fakeManager{
		before: runtime.DockerDiskInfo{TotalReclaimBytes: 1000},
		after:  runtime.DockerDiskInfo{TotalReclaimBytes: 400},
	}
	c := New(fake, config.NewStore(t.TempDir()))
	res := c.Run(context.Background(), Options{})
	if res.ReclaimedBytes != 600 {
		t.Errorf("ReclaimedBytes = %d, want 600", res.ReclaimedBytes)
	}
}