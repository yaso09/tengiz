package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type mockPruneRT struct {
	containers      []runtime.ContainerInfo
	images          []runtime.ImageInfo
	networks        []runtime.NetworkInfo
	volumes         []runtime.VolumeInfo
	removed         []string
	imagesRemoved   []string
	networksRemoved []string
	volumesRemoved  []string
	buildCacheCalls int
}

func (m *mockPruneRT) ListContainers(ctx context.Context) ([]runtime.ContainerInfo, error) {
	return m.containers, nil
}

func (m *mockPruneRT) ListImages(ctx context.Context) ([]runtime.ImageInfo, error) {
	return m.images, nil
}

func (m *mockPruneRT) ListVolumes(ctx context.Context) ([]runtime.VolumeInfo, error) {
	return m.volumes, nil
}

func (m *mockPruneRT) PruneBuildCache(ctx context.Context) error {
	m.buildCacheCalls++
	return nil
}

func (m *mockPruneRT) ListNetworks(ctx context.Context) ([]runtime.NetworkInfo, error) {
	return m.networks, nil
}

func (m *mockPruneRT) RemoveNetwork(ctx context.Context, name string) error {
	m.networksRemoved = append(m.networksRemoved, name)
	return nil
}

func (m *mockPruneRT) Remove(ctx context.Context, name string) error {
	m.removed = append(m.removed, name)
	return nil
}

func (m *mockPruneRT) RemoveImage(ctx context.Context, imageTag string) error {
	m.imagesRemoved = append(m.imagesRemoved, imageTag)
	return nil
}

func (m *mockPruneRT) RemoveVolume(ctx context.Context, name string) error {
	m.volumesRemoved = append(m.volumesRemoved, name)
	return nil
}

func TestPruneDryRunRemovesNothing(t *testing.T) {
	m := &mockPruneRT{
		containers: []runtime.ContainerInfo{
			{Name: "foreign-stopped", State: "exited", Labels: map[string]string{}},
		},
		images: []runtime.ImageInfo{
			{ID: "sha256:aaa", Repository: "<none>", Tag: "<none>"},
		},
		volumes: []runtime.VolumeInfo{
			{Name: "freevol", InUse: false},
		},
	}
	c := New(m, config.NewStore(t.TempDir()))

	result, err := c.Prune(context.Background(), Options{DryRun: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune(dry-run): %v", err)
	}
	if len(result.ContainersRemoved) != 1 || result.ContainersRemoved[0] != "foreign-stopped" {
		t.Errorf("dry-run containers = %v, want [foreign-stopped]", result.ContainersRemoved)
	}
	if len(result.ImagesRemoved) != 1 || result.ImagesRemoved[0] != "sha256:aaa" {
		t.Errorf("dry-run images = %v, want [sha256:aaa]", result.ImagesRemoved)
	}
	if len(result.VolumesRemoved) != 1 || result.VolumesRemoved[0] != "freevol" {
		t.Errorf("dry-run volumes = %v, want [freevol]", result.VolumesRemoved)
	}
	if !result.BuildCache {
		t.Error("dry-run BuildCache = false, want true")
	}
	if len(m.removed) != 0 || len(m.imagesRemoved) != 0 || len(m.volumesRemoved) != 0 || m.buildCacheCalls != 0 {
		t.Errorf("dry-run mutated state: removed=%v images=%v volumes=%v cacheCalls=%d",
			m.removed, m.imagesRemoved, m.volumesRemoved, m.buildCacheCalls)
	}
}

func TestPruneRemovesCandidates(t *testing.T) {
	m := &mockPruneRT{
		containers: []runtime.ContainerInfo{
			{Name: "foreign-stopped", State: "exited", Labels: map[string]string{}},
			{Name: "tengiz-myapp", State: "exited", Labels: map[string]string{runtime.AppLabel: "myapp"}},
			{Name: "foreign-running", State: "running", Labels: map[string]string{}},
		},
		images: []runtime.ImageInfo{
			{ID: "sha256:aaa", Repository: "<none>", Tag: "<none>"},
			{ID: "sha256:bbb", Repository: "tengiz-apps/myapp", Tag: "production-v1"},
		},
		volumes: []runtime.VolumeInfo{
			{Name: "freevol", InUse: false},
			{Name: "usedvol", InUse: true},
		},
	}
	c := New(m, config.NewStore(t.TempDir()))

	result, err := c.Prune(context.Background(), Options{Volumes: true})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if len(m.removed) != 1 || m.removed[0] != "foreign-stopped" {
		t.Errorf("removed containers = %v, want [foreign-stopped]", m.removed)
	}
	if len(m.imagesRemoved) != 1 || m.imagesRemoved[0] != "sha256:aaa" {
		t.Errorf("removed images = %v, want [sha256:aaa]", m.imagesRemoved)
	}
	if len(m.volumesRemoved) != 1 || m.volumesRemoved[0] != "freevol" {
		t.Errorf("removed volumes = %v, want [freevol]", m.volumesRemoved)
	}
	if m.buildCacheCalls != 1 {
		t.Errorf("build cache calls = %d, want 1", m.buildCacheCalls)
	}
	if !result.BuildCache {
		t.Error("result.BuildCache = false, want true")
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

func TestPruneAllImagesProtectsDeploymentRefs(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)
	store.AddDeployment("myapp", types.DeploymentEntry{
		ID:       "1700000000",
		ImageTag: "tengiz-apps/myapp:production-v1",
		Status:   string(types.DeployActive),
	})

	m := &mockPruneRT{
		containers: []runtime.ContainerInfo{
			{Name: "tengiz-myapp", State: "running", Image: "sha256:bbb", Labels: map[string]string{runtime.AppLabel: "myapp"}},
		},
		images: []runtime.ImageInfo{
			{ID: "sha256:aaa", Repository: "<none>", Tag: "<none>"},
			{ID: "sha256:bbb", Repository: "tengiz-apps/myapp", Tag: "production-v1"},
			{ID: "sha256:ccc", Repository: "tengiz-apps/myapp", Tag: "production-v1"},
			{ID: "sha256:ddd", Repository: "tengiz-apps/oldapp", Tag: "production-v9"},
		},
	}
	c := New(m, store)

	result, err := c.Prune(context.Background(), Options{AllImages: true})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if len(result.ImagesRemoved) != 2 {
		t.Fatalf("images removed = %v, want 2 (dangling + unreferenced oldapp)", result.ImagesRemoved)
	}
	for _, img := range result.ImagesRemoved {
		if img == "sha256:bbb" || img == "tengiz-apps/myapp:production-v1" {
			t.Errorf("protected image removed: %s", img)
		}
	}
}

func TestPruneWithoutVolumesFlagKeepsVolumes(t *testing.T) {
	m := &mockPruneRT{
		volumes: []runtime.VolumeInfo{
			{Name: "freevol", InUse: false},
		},
	}
	c := New(m, config.NewStore(t.TempDir()))

	_, err := c.Prune(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(m.volumesRemoved) != 0 {
		t.Errorf("volumes removed without --volumes = %v, want none", m.volumesRemoved)
	}
}

func TestNetworkCandidates(t *testing.T) {
	networks := []runtime.NetworkInfo{
		{Name: "bridge", InUse: false},
		{Name: "host", InUse: false},
		{Name: "none", InUse: false},
		{Name: "mybridge", InUse: false},
		{Name: "usedbridge", InUse: true},
	}
	got := networkCandidates(networks)
	if len(got) != 1 || got[0].Name != "mybridge" {
		t.Fatalf("networkCandidates() = %+v, want only mybridge", got)
	}
}

func TestPruneRemovesUnusedNetworks(t *testing.T) {
	m := &mockPruneRT{
		networks: []runtime.NetworkInfo{
			{Name: "mybridge", InUse: false},
			{Name: "usedbridge", InUse: true},
		},
	}
	c := New(m, config.NewStore(t.TempDir()))

	result, err := c.Prune(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(m.networksRemoved) != 1 || m.networksRemoved[0] != "mybridge" {
		t.Errorf("networks removed = %v, want [mybridge]", m.networksRemoved)
	}
	if len(result.NetworksRemoved) != 1 || result.NetworksRemoved[0] != "mybridge" {
		t.Errorf("result.NetworksRemoved = %v, want [mybridge]", result.NetworksRemoved)
	}
}

func TestContainerCandidates(t *testing.T) {
	containers := []runtime.ContainerInfo{
		{Name: "foreign-stopped", State: "exited", Labels: map[string]string{"foo": "bar"}},
		{Name: "tengiz-myapp", State: "exited", Labels: map[string]string{runtime.AppLabel: "myapp"}},
		{Name: "tengiz-myapp-1700000000", State: "exited", Labels: map[string]string{runtime.AppLabel: "myapp", "tengiz-deployment": "1700000000"}},
		{Name: "foreign-running", State: "running", Labels: map[string]string{}},
		{Name: "tengiz-pr", State: "exited", Labels: map[string]string{runtime.AppLabel: "myapp"}},
	}
	got := containerCandidates(containers)
	if len(got) != 1 {
		t.Fatalf("containerCandidates() = %d candidates, want 1", len(got))
	}
	if got[0].Name != "foreign-stopped" {
		t.Errorf("candidate name = %q, want %q", got[0].Name, "foreign-stopped")
	}
}

func TestImageCandidatesDefaultOnlyDangling(t *testing.T) {
	images := []runtime.ImageInfo{
		{ID: "sha256:aaa", Repository: "<none>", Tag: "<none>"},
		{ID: "sha256:bbb", Repository: "tengiz-apps/myapp", Tag: "production-v1"},
		{ID: "sha256:ccc", Repository: "alpine", Tag: "latest"},
	}
	got := imageCandidates(images, map[string]bool{}, map[string]bool{}, false)
	if len(got) != 1 {
		t.Fatalf("default imageCandidates() = %d, want 1 (dangling only)", len(got))
	}
	if got[0].ID != "sha256:aaa" {
		t.Errorf("candidate ID = %q, want %q", got[0].ID, "sha256:aaa")
	}
}

func TestImageCandidatesAllSkipsProtected(t *testing.T) {
	images := []runtime.ImageInfo{
		{ID: "sha256:aaa", Repository: "<none>", Tag: "<none>"},
		{ID: "sha256:bbb", Repository: "tengiz-apps/myapp", Tag: "production-v1"},
		{ID: "sha256:ccc", Repository: "tengiz-apps/myapp", Tag: "production-v2"},
		{ID: "sha256:ddd", Repository: "alpine", Tag: "3.19"},
	}
	protectedIDs := map[string]bool{"sha256:ccc": true}
	protectedRefs := map[string]bool{"tengiz-apps/myapp:production-v1": true}
	got := imageCandidates(images, protectedIDs, protectedRefs, true)
	wantIDs := map[string]bool{"sha256:aaa": true, "sha256:ddd": true}
	if len(got) != len(wantIDs) {
		t.Fatalf("imageCandidates() = %d candidates, want %d", len(got), len(wantIDs))
	}
	for _, img := range got {
		if !wantIDs[img.ID] {
			t.Errorf("unexpected candidate %q", img.ID)
		}
	}
}

func TestVolumeCandidates(t *testing.T) {
	volumes := []runtime.VolumeInfo{
		{Name: "freevol", InUse: false},
		{Name: "usedvol", InUse: true},
	}
	got := volumeCandidates(volumes)
	if len(got) != 1 || got[0].Name != "freevol" {
		t.Fatalf("volumeCandidates() = %+v, want only freevol", got)
	}
}

func TestImageTargetsUsesIDForDangling(t *testing.T) {
	images := []runtime.ImageInfo{
		{ID: "sha256:aaa", Repository: "<none>", Tag: "<none>"},
		{ID: "sha256:bbb", Repository: "tengiz-apps/myapp", Tag: "production-v1"},
	}
	got := imageTargets(images)
	want := []string{"sha256:aaa", "tengiz-apps/myapp:production-v1"}
	if len(got) != len(want) {
		t.Fatalf("imageTargets() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("imageTargets()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReferencedImageRefsFromStore(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)

	store.SaveApp(types.AppEntry{Name: "myapp", ImageTag: "tengiz-apps/myapp:production-v1"})
	store.AddDeployment("myapp", types.DeploymentEntry{
		ID:       "1700000000",
		ImageTag: "tengiz-apps/myapp:production-v1",
		Status:   string(types.DeployActive),
	})
	store.AddDeployment("myapp", types.DeploymentEntry{
		ID:       "1700000001",
		ImageTag: "tengiz-apps/myapp:production-v2",
		Status:   string(types.DeployPrevious),
	})
	store.AddDeployment("otherapp", types.DeploymentEntry{
		ID:       "1700000002",
		ImageTag: "tengiz-apps/otherapp:production-v3",
		Status:   string(types.DeployActive),
	})

	refs, err := referencedImageRefs(dir)
	if err != nil {
		t.Fatalf("referencedImageRefs: %v", err)
	}
	for _, want := range []string{
		"tengiz-apps/myapp:production-v1",
		"tengiz-apps/myapp:production-v2",
		"tengiz-apps/otherapp:production-v3",
	} {
		if !refs[want] {
			t.Errorf("refs missing %q (got %v)", want, refs)
		}
	}
}

func TestReferencedImageRefsEmptyDir(t *testing.T) {
	refs, err := referencedImageRefs(t.TempDir())
	if err != nil {
		t.Fatalf("referencedImageRefs: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("refs = %v, want empty", refs)
	}
}

func TestReferencedImageRefsScansStagingEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deployments-staging.json"),
		[]byte(`{"myapp":[{"id":"1","image_tag":"tengiz-apps/myapp:staging-v1","port":9001,"created_at":"2026-08-11T00:00:00Z","status":"active"}]}`),
		0644); err != nil {
		t.Fatal(err)
	}
	refs, err := referencedImageRefs(dir)
	if err != nil {
		t.Fatalf("referencedImageRefs: %v", err)
	}
	if !refs["tengiz-apps/myapp:staging-v1"] {
		t.Errorf("refs missing staging image, got %v", refs)
	}
}
