package housekeeping

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

type fakeCleaner struct {
	containers       []runtime.ContainerInfo
	images           []runtime.ImageInfo
	volumes          []string
	networks         []string
	buildCacheSize   string
	prunedBuildCache string
	diskUsage        string

	removedContainers []string
	removedImages     []string
	removedVolumes    []string
	removedNetworks   []string
	prunedBuild       bool
}

func (f *fakeCleaner) ListAllContainers(ctx context.Context) ([]runtime.ContainerInfo, error) {
	return f.containers, nil
}
func (f *fakeCleaner) ListImages(ctx context.Context) ([]runtime.ImageInfo, error) {
	return f.images, nil
}
func (f *fakeCleaner) ListDanglingVolumes(ctx context.Context) ([]string, error) {
	return f.volumes, nil
}
func (f *fakeCleaner) ListDanglingNetworks(ctx context.Context) ([]string, error) {
	return f.networks, nil
}
func (f *fakeCleaner) RemoveContainers(ctx context.Context, ids []string) (int, error) {
	f.removedContainers = append(f.removedContainers, ids...)
	return len(ids), nil
}
func (f *fakeCleaner) RemoveImages(ctx context.Context, tags []string) (int, error) {
	f.removedImages = append(f.removedImages, tags...)
	return len(tags), nil
}
func (f *fakeCleaner) RemoveVolumes(ctx context.Context, names []string) (int, error) {
	f.removedVolumes = append(f.removedVolumes, names...)
	return len(names), nil
}
func (f *fakeCleaner) RemoveNetworks(ctx context.Context, ids []string) (int, error) {
	f.removedNetworks = append(f.removedNetworks, ids...)
	return len(ids), nil
}
func (f *fakeCleaner) BuildCacheSize(ctx context.Context) (string, error) {
	return f.buildCacheSize, nil
}
func (f *fakeCleaner) PruneBuildCache(ctx context.Context) (string, error) {
	f.prunedBuild = true
	return f.prunedBuildCache, nil
}
func (f *fakeCleaner) DiskUsage(ctx context.Context) (string, error) {
	return f.diskUsage, nil
}

var _ runtime.Cleaner = (*fakeCleaner)(nil)

func TestRunAllCategories(t *testing.T) {
	f := &fakeCleaner{
		containers:       []runtime.ContainerInfo{{ID: "c1", State: "exited"}},
		images:           []runtime.ImageInfo{{Tag: "<none>:<none>", ID: "d1"}},
		volumes:          []string{"vol1"},
		networks:         []string{"net1"},
		prunedBuildCache: "1.2GB",
		diskUsage:        "df output",
	}
	c := New(f)
	s, err := c.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if s.Containers != 1 || s.Dangling != 1 || s.Volumes != 1 || s.Networks != 1 {
		t.Errorf("summary = %+v", s)
	}
	if s.BuildCache != "1.2GB" {
		t.Errorf("BuildCache = %q, want 1.2GB", s.BuildCache)
	}
	if !f.prunedBuild {
		t.Error("build cache was not pruned")
	}
	if len(f.removedContainers) != 1 || len(f.removedImages) != 1 ||
		len(f.removedVolumes) != 1 || len(f.removedNetworks) != 1 {
		t.Errorf("removed sets = containers:%v images:%v volumes:%v networks:%v",
			f.removedContainers, f.removedImages, f.removedVolumes, f.removedNetworks)
	}
}

func TestRunDryRunDoesNotRemove(t *testing.T) {
	f := &fakeCleaner{
		containers:      []runtime.ContainerInfo{{ID: "c1", State: "exited"}},
		images:          []runtime.ImageInfo{{Tag: "<none>:<none>", ID: "d1"}},
		volumes:         []string{"vol1"},
		networks:        []string{"net1"},
		buildCacheSize:  "2.4GB",
		diskUsage:       "df output",
	}
	c := New(f)
	s, err := c.Run(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if s.Containers != 1 || s.Dangling != 1 || s.Volumes != 1 || s.Networks != 1 {
		t.Errorf("dry summary = %+v", s)
	}
	if s.BuildCache != "2.4GB" {
		t.Errorf("dry BuildCache = %q, want reported size 2.4GB", s.BuildCache)
	}
	if f.prunedBuild {
		t.Error("build cache pruned during dry run")
	}
	if len(f.removedContainers)+len(f.removedImages)+len(f.removedVolumes)+len(f.removedNetworks) != 0 {
		t.Errorf("dry run removed something: %+v", f)
	}
}

func TestRunContainersOnly(t *testing.T) {
	f := &fakeCleaner{
		containers: []runtime.ContainerInfo{
			{ID: "c1", State: "exited"},
			{ID: "c2", State: "exited", Labels: map[string]string{"tengiz-app": "myapp"}},
		},
	}
	c := New(f)
	s, err := c.Run(context.Background(), Options{Containers: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if s.Containers != 1 || len(f.removedContainers) != 1 || f.removedContainers[0] != "c1" {
		t.Errorf("containers = summary:%d removed:%v", s.Containers, f.removedContainers)
	}
	if f.prunedBuild {
		t.Error("build cache pruned when only containers requested")
	}
}

func TestRunOldImagesRespectKeep(t *testing.T) {
	f := &fakeCleaner{
		images: []runtime.ImageInfo{
			{Tag: "tengiz-apps/myapp:v1", CreatedAt: "2026-07-01 10:00:00 +0000 UTC"},
			{Tag: "tengiz-apps/myapp:v2", CreatedAt: "2026-07-10 10:00:00 +0000 UTC"},
			{Tag: "tengiz-apps/myapp:v3", CreatedAt: "2026-07-15 10:00:00 +0000 UTC"},
		},
	}
	c := New(f)
	s, err := c.Run(context.Background(), Options{Images: true, Keep: 2})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if s.OldImages != 1 || s.Dangling != 0 {
		t.Errorf("summary = %+v", s)
	}
	if len(f.removedImages) != 1 || f.removedImages[0] != "tengiz-apps/myapp:v1" {
		t.Errorf("removedImages = %v, want [tengiz-apps/myapp:v1]", f.removedImages)
	}
}

func TestRunInUseImagesProtected(t *testing.T) {
	f := &fakeCleaner{
		images: []runtime.ImageInfo{
			{Tag: "tengiz-apps/myapp:v1", CreatedAt: "2026-07-01 10:00:00 +0000 UTC"},
			{Tag: "tengiz-apps/myapp:v2", CreatedAt: "2026-07-10 10:00:00 +0000 UTC", InUse: true},
		},
	}
	c := New(f)
	s, err := c.Run(context.Background(), Options{Images: true, Keep: 1})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	// eligible non-latest non-in-use: v1 only -> len 1 <= keep 1 -> nothing removed.
	if s.OldImages != 0 || len(f.removedImages) != 0 {
		t.Errorf("OldImages = %d, removed = %v, want 0/none", s.OldImages, f.removedImages)
	}
}
