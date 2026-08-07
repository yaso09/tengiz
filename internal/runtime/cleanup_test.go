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

func TestPruneOptionsHasAllCategories(t *testing.T) {
	opts := PruneOptions{
		Containers: true,
		Images:     true,
		AllImages:  true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
		DryRun:     true,
	}
	if !opts.Containers || !opts.Images || !opts.AllImages || !opts.Volumes || !opts.Networks || !opts.BuildCache || !opts.DryRun {
		t.Fatal("PruneOptions missing expected fields")
	}
}

func TestStubPruneAndDiskUsage(t *testing.T) {
	m := NewStub()
	ctx := context.Background()
	_, err := m.Prune(ctx, PruneOptions{Containers: true})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	du, err := m.DiskUsage(ctx)
	if err != nil {
		t.Fatalf("stub DiskUsage() error = %v", err)
	}
	if du.Images != "" || du.Reclaimable != "" {
		t.Fatalf("expected empty stub DiskUsage, got %+v", du)
	}
}

func TestStubImplementsManager(t *testing.T) {
	var m Manager = NewStub()
	if m == nil {
		t.Fatal("stubManager must implement Manager")
	}
}
