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
	result, err := m.PruneContainers(context.Background())
	if err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
	if result == nil {
		t.Fatal("PruneContainers() result is nil")
	}
	if result.ContainersRemoved != 0 {
		t.Errorf("PruneContainers().ContainersRemoved = %d, want 0", result.ContainersRemoved)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	result, err := m.PruneImages(context.Background())
	if err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if result == nil {
		t.Fatal("PruneImages() result is nil")
	}
}

func TestStubPruneVolumes(t *testing.T) {
	m := NewStub()
	result, err := m.PruneVolumes(context.Background())
	if err != nil {
		t.Fatalf("PruneVolumes() error = %v", err)
	}
	if result == nil {
		t.Fatal("PruneVolumes() result is nil")
	}
}

func TestStubPruneNetworks(t *testing.T) {
	m := NewStub()
	result, err := m.PruneNetworks(context.Background())
	if err != nil {
		t.Fatalf("PruneNetworks() error = %v", err)
	}
	if result == nil {
		t.Fatal("PruneNetworks() result is nil")
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	result, err := m.PruneBuildCache(context.Background())
	if err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
	if result == nil {
		t.Fatal("PruneBuildCache() result is nil")
	}
}
