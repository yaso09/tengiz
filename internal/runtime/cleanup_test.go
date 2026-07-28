package runtime

import (
	"context"
	"testing"
)

func TestDockerRuntimePruneMethodsCompile(t *testing.T) {
	var _ Manager = &dockerRuntime{}
}

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	if err := m.PruneContainers(context.Background()); err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	if err := m.PruneImages(context.Background(), false); err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if err := m.PruneImages(context.Background(), true); err != nil {
		t.Fatalf("PruneImages(all) error = %v", err)
	}
}

func TestStubPruneVolumes(t *testing.T) {
	m := NewStub()
	if err := m.PruneVolumes(context.Background()); err != nil {
		t.Fatalf("PruneVolumes() error = %v", err)
	}
}

func TestStubPruneNetworks(t *testing.T) {
	m := NewStub()
	if err := m.PruneNetworks(context.Background()); err != nil {
		t.Fatalf("PruneNetworks() error = %v", err)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	if err := m.PruneBuildCache(context.Background()); err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
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
