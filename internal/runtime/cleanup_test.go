package runtime

import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0", report.ContainersRemoved)
	}
	if report.ImagesRemoved != 0 {
		t.Errorf("ImagesRemoved = %d, want 0", report.ImagesRemoved)
	}
	if report.NetworksRemoved != 0 {
		t.Errorf("NetworksRemoved = %d, want 0", report.NetworksRemoved)
	}
	if report.BuildCacheFreed != 0 {
		t.Errorf("BuildCacheFreed = %d, want 0", report.BuildCacheFreed)
	}
}

func TestStubPruneWithAppFilter(t *testing.T) {
	m := NewStub()
	opts := PruneOptions{AppName: "myapp", Containers: true}
	report, err := m.Prune(context.Background(), opts)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0", report.ContainersRemoved)
	}
}

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
