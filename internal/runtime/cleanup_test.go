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
	report, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
	}
	if report.DryRun {
		t.Errorf("DryRun = true, want false")
	}
	if len(report.ContainersRemoved) != 0 {
		t.Errorf("ContainersRemoved = %v, want empty", report.ContainersRemoved)
	}
}
