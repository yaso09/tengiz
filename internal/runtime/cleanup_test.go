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
	res, err := m.Prune(context.Background(), CleanupOptions{Targets: DefaultCleanupTargets()})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Containers != nil || res.Images != nil || res.Networks != nil || res.Volumes != nil || res.CacheBytes != 0 {
		t.Fatalf("expected empty CleanupResult, got %+v", res)
	}
}

func TestDefaultCleanupTargetsExcludesVolumes(t *testing.T) {
	targets := DefaultCleanupTargets()
	if len(targets) != 4 {
		t.Fatalf("expected 4 default targets, got %d", len(targets))
	}
	for _, tgt := range targets {
		if tgt == CleanupVolumes {
			t.Fatal("default targets must exclude volumes")
		}
	}
}

func TestAllCleanupTargetsIncludesAll(t *testing.T) {
	targets := AllCleanupTargets()
	if len(targets) != 5 {
		t.Fatalf("expected 5 targets, got %d", len(targets))
	}
}
