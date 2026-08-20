package runtime

import (
	"context"
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
	result, err := m.Cleanup(context.Background(), CleanupOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result != (CleanupResult{}) {
		t.Errorf("expected zero CleanupResult, got %+v", result)
	}
}

func TestParsePruneOutput(t *testing.T) {
	output := `Deleted Containers:
abc123
def456

Deleted Networks:
tengiz-custom-net

Deleted Images:
untagged: tengiz-apps/myapp:production-123
deleted: sha256:abc123def456

Total reclaimed space: 1.234GB`

	got := parsePruneOutput(output)
	if got.ContainersDeleted != 2 {
		t.Errorf("ContainersDeleted = %d, want 2", got.ContainersDeleted)
	}
	if got.NetworksDeleted != 1 {
		t.Errorf("NetworksDeleted = %d, want 1", got.NetworksDeleted)
	}
	if got.ImagesDeleted != 2 {
		t.Errorf("ImagesDeleted = %d, want 2", got.ImagesDeleted)
	}
	if got.VolumesDeleted != 0 {
		t.Errorf("VolumesDeleted = %d, want 0", got.VolumesDeleted)
	}
	if got.BuildCacheDeleted != 0 {
		t.Errorf("BuildCacheDeleted = %d, want 0", got.BuildCacheDeleted)
	}
	if got.ReclaimedSpace != "1.234GB" {
		t.Errorf("ReclaimedSpace = %q, want %q", got.ReclaimedSpace, "1.234GB")
	}
}

func TestParsePruneOutputVolumesAndCache(t *testing.T) {
	output := `Deleted Volumes:
vol1

Deleted Build Cache Objects:
abc123`

	got := parsePruneOutput(output)
	if got.VolumesDeleted != 1 {
		t.Errorf("VolumesDeleted = %d, want 1", got.VolumesDeleted)
	}
	if got.BuildCacheDeleted != 1 {
		t.Errorf("BuildCacheDeleted = %d, want 1", got.BuildCacheDeleted)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	got := parsePruneOutput("")
	if got != (CleanupResult{}) {
		t.Errorf("expected zero CleanupResult, got %+v", got)
	}
}

func TestParsePruneOutputIgnoresWarningBlock(t *testing.T) {
	output := `WARNING! This will remove:
  - all stopped containers
  - all networks not used by at least one container
  - all dangling images
  - all dangling build cache

Deleted Containers:
abc123

Total reclaimed space: 5MB`

	got := parsePruneOutput(output)
	if got.ContainersDeleted != 1 {
		t.Errorf("ContainersDeleted = %d, want 1", got.ContainersDeleted)
	}
	if got.ReclaimedSpace != "5MB" {
		t.Errorf("ReclaimedSpace = %q, want %q", got.ReclaimedSpace, "5MB")
	}
}
