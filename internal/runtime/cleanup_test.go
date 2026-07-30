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

func TestStubPruneMethods(t *testing.T) {
	m := NewStub()
	ctx := context.Background()

	if err := m.PruneContainers(ctx, ""); err != nil {
		t.Errorf("PruneContainers() error = %v", err)
	}
	if err := m.PruneImages(ctx, "", 5); err != nil {
		t.Errorf("PruneImages() error = %v", err)
	}
	if err := m.PruneBuildCache(ctx); err != nil {
		t.Errorf("PruneBuildCache() error = %v", err)
	}
	if err := m.PruneOrphanedImages(ctx); err != nil {
		t.Errorf("PruneOrphanedImages() error = %v", err)
	}
	resources, err := m.ListOrphanedResources(ctx)
	if err != nil {
		t.Errorf("ListOrphanedResources() error = %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("expected 0 orphaned resources, got %d", len(resources))
	}
}
