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
	report, err := m.PruneSystem(context.Background(), PruneOptions{
		Containers: true,
		Images:     true,
		BuildCache: true,
	})
	if err != nil {
		t.Fatalf("PruneSystem on stub: %v", err)
	}
	if report == nil {
		t.Fatal("PruneSystem returned nil report")
	}
}

func TestPruneOptionsDefaults(t *testing.T) {
	opts := PruneOptions{}
	if opts.DryRun {
		t.Error("DryRun should default to false")
	}
	if opts.All {
		t.Error("All should default to false")
	}
}
