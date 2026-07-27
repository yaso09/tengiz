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
	stats, err := m.PruneContainers(context.Background())
	if err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
	if stats.ItemsRemoved != 0 {
		t.Fatalf("expected 0 items removed, got %d", stats.ItemsRemoved)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	stats, err := m.PruneImages(context.Background(), false)
	if err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if stats.ItemsRemoved != 0 {
		t.Fatalf("expected 0 items removed, got %d", stats.ItemsRemoved)
	}
	stats, err = m.PruneImages(context.Background(), true)
	if err != nil {
		t.Fatalf("PruneImages(all=true) error = %v", err)
	}
}

func TestStubPruneVolumes(t *testing.T) {
	m := NewStub()
	stats, err := m.PruneVolumes(context.Background())
	if err != nil {
		t.Fatalf("PruneVolumes() error = %v", err)
	}
	if stats.ItemsRemoved != 0 {
		t.Fatalf("expected 0 items removed, got %d", stats.ItemsRemoved)
	}
}

func TestStubPruneNetworks(t *testing.T) {
	m := NewStub()
	_, err := m.PruneNetworks(context.Background())
	if err != nil {
		t.Fatalf("PruneNetworks() error = %v", err)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	_, err := m.PruneBuildCache(context.Background())
	if err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
}
