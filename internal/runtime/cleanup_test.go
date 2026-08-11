package runtime

import (
	"context"
	"reflect"
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
	result, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(result.Containers) != 0 || len(result.Images) != 0 || len(result.Volumes) != 0 || len(result.Networks) != 0 {
		t.Errorf("stub Cleanup should return empty result, got %+v", result)
	}
}

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		category string
		expected []string
	}{
		{categoryContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{categoryImages, []string{"image", "prune", "-f"}},
		{categoryVolumes, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{categoryNetworks, []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}},
	}
	for _, tt := range tests {
		got := buildPruneArgs(tt.category)
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("buildPruneArgs(%q) = %v, want %v", tt.category, got, tt.expected)
		}
	}
}

func TestBuildDryRunArgs(t *testing.T) {
	tests := []struct {
		category string
		expected []string
	}{
		{categoryContainers, []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.ID}}"}},
		{categoryImages, []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}},
		{categoryVolumes, []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}},
		{categoryNetworks, []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}},
	}
	for _, tt := range tests {
		got := buildDryRunArgs(tt.category)
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("buildDryRunArgs(%q) = %v, want %v", tt.category, got, tt.expected)
		}
	}
}
