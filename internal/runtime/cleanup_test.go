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

func TestBuildContainerPruneArgs(t *testing.T) {
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	for _, dry := range []bool{true, false} {
		got := buildContainerPruneArgs(dry)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("buildContainerPruneArgs(%v) = %v, want %v", dry, got, want)
		}
	}
}

func TestBuildImagePruneArgs(t *testing.T) {
	got := buildImagePruneArgs(true)
	if len(got) == 0 || got[0] != "image" {
		t.Fatalf("buildImagePruneArgs(true) should start with 'image', got %v", got)
	}
	foundForce := false
	foundDangling := false
	for _, a := range got {
		if a == "-f" {
			foundForce = true
		}
		if a == "dangling=true" {
			foundDangling = true
		}
	}
	if !foundForce {
		t.Errorf("buildImagePruneArgs(true) missing -f: %v", got)
	}
	if !foundDangling {
		t.Errorf("buildImagePruneArgs(true) missing dangling=true filter: %v", got)
	}
}

func TestBuildVolumePruneArgs(t *testing.T) {
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	got := buildVolumePruneArgs(true)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildVolumePruneArgs(true) = %v, want %v", got, want)
	}
}

func TestBuildBuilderPruneArgs(t *testing.T) {
	want := []string{"builder", "prune", "-f"}
	got := buildBuilderPruneArgs(true)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildBuilderPruneArgs(true) = %v, want %v", got, want)
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, Images: true, Volumes: true, BuildCache: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.VolumesRemoved != 0 || res.BuildCacheReclaimed != 0 {
		t.Errorf("stub Cleanup should return zeroed result, got %+v", res)
	}
}
