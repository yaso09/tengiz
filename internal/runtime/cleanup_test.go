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
	report, err := m.PruneSystem(context.Background(), false)
	if err != nil {
		t.Fatalf("PruneSystem() error = %v", err)
	}
	if report.Containers != 0 || report.Images != 0 || report.Networks != 0 || report.BuildCache != 0 {
		t.Errorf("PruneSystem() returned non-zero report on stub: %+v", report)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	report, err := m.PruneBuildCache(context.Background(), false)
	if err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
	if report.BuildCache != 0 {
		t.Errorf("PruneBuildCache() returned non-zero build cache on stub: %+v", report)
	}
}

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	if err := m.PruneContainers(context.Background(), "testapp"); err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	if err := m.PruneImages(context.Background(), "testapp", 5); err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
}
