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

func TestStubPrune(t *testing.T) {
	m := NewStub()
	opts := PruneOptions{All: true}
	report, err := m.Prune(context.Background(), opts)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersDeleted != 0 {
		t.Errorf("ContainersDeleted = %d, want 0", report.ContainersDeleted)
	}
	if report.ImagesDeleted != 0 {
		t.Errorf("ImagesDeleted = %d, want 0", report.ImagesDeleted)
	}
	if report.SpaceReclaimed != "0B" {
		t.Errorf("SpaceReclaimed = %q, want %q", report.SpaceReclaimed, "0B")
	}
}

func TestStubPruneDryRun(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune(dry-run) error = %v", err)
	}
	if report.ContainersDeleted != 0 {
		t.Errorf("dry-run ContainersDeleted = %d, want 0", report.ContainersDeleted)
	}
}

func TestStubPrunePerCategory(t *testing.T) {
	m := NewStub()
	opts := PruneOptions{Containers: true, Images: true}
	report, err := m.Prune(context.Background(), opts)
	if err != nil {
		t.Fatalf("Prune(categories) error = %v", err)
	}
	if report.ContainersDeleted != 0 {
		t.Errorf("ContainersDeleted = %d, want 0", report.ContainersDeleted)
	}
}
