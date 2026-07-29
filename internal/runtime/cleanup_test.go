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
	report, err := m.Prune(context.Background(), PruneOptions{
		Containers: true,
		Images:     true,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.SpaceReclaimedBytes != 0 {
		t.Errorf("PruneReport.SpaceReclaimedBytes = %d, want 0", report.SpaceReclaimedBytes)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	removed, err := m.PruneImages(context.Background(), "testapp", 5)
	if err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("len(removed) = %d, want 0", len(removed))
	}
}
