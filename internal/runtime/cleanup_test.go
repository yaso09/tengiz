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

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		category PruneCategory
		want     []string
	}{
		{PruneContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{PruneImages, []string{"image", "prune", "-f"}},
		{PruneVolumes, []string{"volume", "prune", "-f"}},
		{PruneNetworks, []string{"network", "prune", "-f"}},
		{PruneBuildCache, []string{"builder", "prune", "-f"}},
		{PruneCategory("bogus"), nil},
	}
	for _, tc := range tests {
		got := pruneArgs(tc.category)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("pruneArgs(%q) = %v, want %v", tc.category, got, tc.want)
		}
	}
}

func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"Deleted Containers:\n[...]\n\nTotal reclaimed space: 123.4MB\n", "123.4MB"},
		{"Some deleted\nTotal reclaimed space: 0B\n", "0B"},
		{"No containers to delete.\n", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := parseReclaimed(tc.output)
		if got != tc.want {
			t.Errorf("parseReclaimed(%q) = %q, want %q", tc.output, got, tc.want)
		}
	}
}

func TestActiveCategoriesOrderAndFilter(t *testing.T) {
	tests := []struct {
		opts PruneOptions
		want []PruneCategory
	}{
		{PruneOptions{}, []PruneCategory{}},
		{PruneOptions{Containers: true}, []PruneCategory{PruneContainers}},
		{PruneOptions{Containers: true, Images: true}, []PruneCategory{PruneContainers, PruneImages}},
		{PruneOptions{Volumes: true, Networks: true, BuildCache: true}, []PruneCategory{PruneVolumes, PruneNetworks, PruneBuildCache}},
		{PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}, []PruneCategory{
			PruneContainers, PruneImages, PruneVolumes, PruneNetworks, PruneBuildCache,
		}},
	}
	for _, tc := range tests {
		got := activeCategories(tc.opts)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("activeCategories(%+v) = %v, want %v", tc.opts, got, tc.want)
		}
	}
}
