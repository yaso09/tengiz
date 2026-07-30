package runtime

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
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
	opts := types.CleanupOptions{All: true}
	report, err := m.Cleanup(context.Background(), opts)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 {
		t.Errorf("expected empty report from stub, got %+v", report)
	}
}

func TestCleanupOptionsDefaults(t *testing.T) {
	opts := types.CleanupOptions{}
	if opts.All {
		t.Error("All should default to false")
	}
	if opts.KeepImages != 0 {
		t.Errorf("KeepImages should default to 0, got %d", opts.KeepImages)
	}
}
