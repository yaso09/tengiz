package runtime

import (
	"context"
	"testing"
)

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	if err := m.PruneContainers(context.Background(), ""); err != nil {
		t.Errorf("PruneContainers() error = %v", err)
	}
	if err := m.PruneContainers(context.Background(), "myapp"); err != nil {
		t.Errorf("PruneContainers(myapp) error = %v", err)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	if err := m.PruneImages(context.Background(), "", 5); err != nil {
		t.Errorf("PruneImages() error = %v", err)
	}
	if err := m.PruneImages(context.Background(), "myapp", 3); err != nil {
		t.Errorf("PruneImages(myapp) error = %v", err)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	if err := m.PruneBuildCache(context.Background()); err != nil {
		t.Errorf("PruneBuildCache() error = %v", err)
	}
}

func TestStubPruneOrphanedImages(t *testing.T) {
	m := NewStub()
	if err := m.PruneOrphanedImages(context.Background()); err != nil {
		t.Errorf("PruneOrphanedImages() error = %v", err)
	}
}

func TestStubListOrphanedResources(t *testing.T) {
	m := NewStub()
	resources, err := m.ListOrphanedResources(context.Background())
	if err != nil {
		t.Errorf("ListOrphanedResources() error = %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("expected 0 resources, got %d: %v", len(resources), resources)
	}
}
