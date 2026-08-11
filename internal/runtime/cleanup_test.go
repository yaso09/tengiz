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

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	result, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(result.Containers) != 0 || len(result.Images) != 0 || len(result.Volumes) != 0 || len(result.Networks) != 0 {
		t.Errorf("stub Cleanup should return empty result, got %+v", result)
	}
}
