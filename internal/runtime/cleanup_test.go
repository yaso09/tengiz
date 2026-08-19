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
	res, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ReclaimedBytes != 0 || res.Details != "" {
		t.Fatalf("expected empty result, got %+v", res)
	}
}

func TestDockerCleanupNoCategoriesDoesNothing(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ReclaimedBytes != 0 {
		t.Errorf("ReclaimedBytes = %d, want 0", res.ReclaimedBytes)
	}
	if res.Details != "" {
		t.Errorf("Details = %q, want empty", res.Details)
	}
}
