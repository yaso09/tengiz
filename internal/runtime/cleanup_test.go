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
	report, err := m.Cleanup(context.Background(), types.CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.DryRun {
		t.Error("DryRun should be false for default options")
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 {
		t.Errorf("stub Cleanup should return zeroed report, got %+v", report)
	}
}
