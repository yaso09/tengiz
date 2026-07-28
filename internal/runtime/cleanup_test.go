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

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	if err := m.PruneContainers(context.Background()); err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	if err := m.PruneImages(context.Background()); err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
}

func TestStubPruneVolumes(t *testing.T) {
	m := NewStub()
	if err := m.PruneVolumes(context.Background()); err != nil {
		t.Fatalf("PruneVolumes() error = %v", err)
	}
}

func TestStubPruneNetworks(t *testing.T) {
	m := NewStub()
	if err := m.PruneNetworks(context.Background()); err != nil {
		t.Fatalf("PruneNetworks() error = %v", err)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	if err := m.PruneBuildCache(context.Background()); err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
}

func TestStubCleanupOrphanedContainers(t *testing.T) {
	m := NewStub()
	if err := m.CleanupOrphanedContainers(context.Background(), []string{"myapp"}); err != nil {
		t.Fatalf("CleanupOrphanedContainers() error = %v", err)
	}
}

func TestStubCleanupOrphanedImages(t *testing.T) {
	m := NewStub()
	if err := m.CleanupOrphanedImages(context.Background(), []string{"myapp"}); err != nil {
		t.Fatalf("CleanupOrphanedImages() error = %v", err)
	}
}

func TestStubCleanupAppImages(t *testing.T) {
	m := NewStub()
	if err := m.CleanupAppImages(context.Background(), "myapp"); err != nil {
		t.Fatalf("CleanupAppImages() error = %v", err)
	}
}

func TestStubCleanupAppResources(t *testing.T) {
	m := NewStub()
	if err := m.CleanupAppResources(context.Background(), "myapp"); err != nil {
		t.Fatalf("CleanupAppResources() error = %v", err)
	}
}
