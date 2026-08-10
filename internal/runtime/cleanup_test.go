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
	summary, err := m.Prune(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(summary.ContainersRemoved) != 0 {
		t.Fatalf("expected 0 containers removed, got %d", len(summary.ContainersRemoved))
	}
}
