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

func TestStubPruneSystem(t *testing.T) {
	m := NewStub()
	if err := m.PruneSystem(context.Background(), false); err != nil {
		t.Fatalf("PruneSystem() error = %v", err)
	}
}

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	if err := m.PruneContainers(context.Background(), false); err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	if err := m.PruneImages(context.Background(), false); err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	if err := m.PruneBuildCache(context.Background()); err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
}

func TestStubDetectStaleContainers(t *testing.T) {
	m := NewStub()
	containers, err := m.DetectStaleContainers(context.Background())
	if err != nil {
		t.Fatalf("DetectStaleContainers() error = %v", err)
	}
	if len(containers) != 0 {
		t.Errorf("DetectStaleContainers() = %v, want empty", containers)
	}
}

func TestStubKeepLastNContainers(t *testing.T) {
	m := NewStub()
	if err := m.KeepLastNContainers(context.Background(), "testapp", 5); err != nil {
		t.Fatalf("KeepLastNContainers() error = %v", err)
	}
}
