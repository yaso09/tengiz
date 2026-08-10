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

func TestPruneArgsContainersProtectsTengizLabeledContainers(t *testing.T) {
	args := pruneArgs("containers")
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("pruneArgs(containers) = %v, want %v", args, want)
	}
}

func TestPruneArgsAllCategories(t *testing.T) {
	tests := []struct {
		category string
		want     []string
	}{
		{"build-cache", []string{"builder", "prune", "-f"}},
		{"images", []string{"image", "prune", "-f"}},
		{"networks", []string{"network", "prune", "-f"}},
		{"volumes", []string{"volume", "prune", "-f"}},
		{"unknown", nil},
	}
	for _, tc := range tests {
		if got := pruneArgs(tc.category); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("pruneArgs(%q) = %v, want %v", tc.category, got, tc.want)
		}
	}
}

func TestSystemDfArgs(t *testing.T) {
	want := []string{"system", "df"}
	if got := systemDfArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("systemDfArgs() = %v, want %v", got, want)
	}
}

func TestCleanupCategoriesOrder(t *testing.T) {
	opts := CleanupOptions{Containers: true, Images: true, Networks: true, BuildCache: true, Volumes: true}
	want := []string{"containers", "build-cache", "images", "networks", "volumes"}
	if got := cleanupCategories(opts); !reflect.DeepEqual(got, want) {
		t.Errorf("cleanupCategories() = %v, want %v", got, want)
	}
}

func TestCleanupCategoriesPartial(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want []string
	}{
		{"empty", CleanupOptions{}, nil},
		{"volumes only", CleanupOptions{Volumes: true}, []string{"volumes"}},
		{"containers only", CleanupOptions{Containers: true}, []string{"containers"}},
	}
	for _, tc := range tests {
		if got := cleanupCategories(tc.opts); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: cleanupCategories() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStubCleanupReturnsEmptyReport(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report != "" {
		t.Errorf("Cleanup() report = %q, want empty", report)
	}
}
