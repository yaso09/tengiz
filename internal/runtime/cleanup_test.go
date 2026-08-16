package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestStubRemoveImage(t *testing.T) {
	m := NewStub()
	if err := m.RemoveImage(context.Background(), "tengiz-apps/testapp:v1"); err != nil {
		t.Fatalf("RemoveImage() error = %v", err)
	}
}

func TestStubKeepLastNImages(t *testing.T) {
	m := NewStub()
	if err := m.KeepLastNImages(context.Background(), "testapp", 5); err != nil {
		t.Fatalf("KeepLastNImages() error = %v", err)
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	reports, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if reports != nil {
		t.Errorf("Cleanup() reports = %v, want nil", reports)
	}
}

func TestPruneCommandContainers(t *testing.T) {
	got := pruneCommand(CleanupContainers)
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneCommand(containers) = %v, want %v", got, want)
	}
}

func TestPruneCommandImages(t *testing.T) {
	got := pruneCommand(CleanupImages)
	want := []string{"image", "prune", "-f", "--filter", "dangling=true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneCommand(images) = %v, want %v", got, want)
	}
}

func TestPruneCommandVolumes(t *testing.T) {
	got := pruneCommand(CleanupVolumes)
	want := []string{"volume", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneCommand(volumes) = %v, want %v", got, want)
	}
}

func TestPruneCommandNetworks(t *testing.T) {
	got := pruneCommand(CleanupNetworks)
	want := []string{"network", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneCommand(networks) = %v, want %v", got, want)
	}
}

func TestPruneCommandBuildCache(t *testing.T) {
	got := pruneCommand(CleanupBuildCache)
	want := []string{"builder", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneCommand(build-cache) = %v, want %v", got, want)
	}
}

func TestDefaultCleanupCategories(t *testing.T) {
	want := []CleanupCategory{
		CleanupContainers,
		CleanupImages,
		CleanupNetworks,
		CleanupBuildCache,
	}
	got := defaultCleanupCategories()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("defaultCleanupCategories() = %v, want %v", got, want)
	}
}

func TestDockerCleanupDryRun(t *testing.T) {
	r := &dockerRuntime{}
	reports, err := r.Cleanup(context.Background(), CleanupOptions{
		Categories: []CleanupCategory{CleanupContainers, CleanupImages},
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("len(reports) = %d, want 2", len(reports))
	}
	if reports[0].Category != CleanupContainers ||
		reports[0].Command != "docker container prune -f --filter label!=tengiz-app" {
		t.Errorf("reports[0] = %+v", reports[0])
	}
	if reports[1].Category != CleanupImages ||
		reports[1].Command != "docker image prune -f --filter dangling=true" {
		t.Errorf("reports[1] = %+v", reports[1])
	}
}

func TestDockerCleanupDryRunDefaults(t *testing.T) {
	r := &dockerRuntime{}
	reports, err := r.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	want := []CleanupCategory{CleanupContainers, CleanupImages, CleanupNetworks, CleanupBuildCache}
	if len(reports) != len(want) {
		t.Fatalf("len(reports) = %d, want %d", len(reports), len(want))
	}
	for i, w := range want {
		if reports[i].Category != w {
			t.Errorf("reports[%d].Category = %s, want %s", i, reports[i].Category, w)
		}
		if reports[i].Command == "" {
			t.Errorf("reports[%d].Command is empty, want a docker command", i)
		}
	}
}