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
	report, err := m.PruneContainers(context.Background(), nil)
	if err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
	if report.ItemsRemoved != 0 {
		t.Errorf("ItemsRemoved = %d, want 0", report.ItemsRemoved)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	report, err := m.PruneImages(context.Background(), nil)
	if err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if report.ItemsRemoved != 0 {
		t.Errorf("ItemsRemoved = %d, want 0", report.ItemsRemoved)
	}
}
