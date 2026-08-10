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
	rep, err := m.Prune(context.Background(), PruneOptions{Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if rep.ImagesRemoved != 0 {
		t.Errorf("ImagesRemoved = %d, want 0", rep.ImagesRemoved)
	}
}
