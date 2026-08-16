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

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		BuildCache: true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.VolumesRemoved != 0 || res.NetworksRemoved != 0 {
		t.Errorf("Cleanup() result = %+v, want all-zero counts", res)
	}
	if res.BuildCacheCleared {
		t.Error("BuildCacheCleared = true, want false")
	}
}

func TestPruneCategories(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want []string
	}{
		{"nothing enabled", CleanupOptions{}, nil},
		{"containers only", CleanupOptions{Containers: true}, []string{"containers"}},
		{"safe defaults", CleanupOptions{Containers: true, Images: true, BuildCache: true},
			[]string{"containers", "images", "build-cache"}},
		{"all enabled", CleanupOptions{Containers: true, Images: true, BuildCache: true, Volumes: true, Networks: true},
			[]string{"containers", "images", "build-cache", "volumes", "networks"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneCategories(tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("pruneCategories(%+v) = %v (len %d), want %v (len %d)", tt.opts, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("pruneCategories(%+v)[%d] = %q, want %q", tt.opts, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCleanupCommandArgs(t *testing.T) {
	tests := []struct {
		category string
		want     []string
	}{
		{"containers", []string{"container", "prune", "-f", "--filter", "label=tengiz-app"}},
		{"images", []string{"image", "prune", "-f"}},
		{"build-cache", []string{"builder", "prune", "-f"}},
		{"volumes", []string{"volume", "prune", "-f"}},
		{"networks", []string{"network", "prune", "-f"}},
	}
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			got := cleanupCommandArgs(tt.category)
			if len(got) != len(tt.want) {
				t.Fatalf("cleanupCommandArgs(%q) = %v, want %v", tt.category, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("cleanupCommandArgs(%q)[%d] = %q, want %q", tt.category, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCountPruneOutput(t *testing.T) {
	const containerOutput = `Deleted Containers:
f3b9c2e1a4d5
a1b2c3d4e5f6

Total reclaimed space: 1.234MB`
	if got := countPruneOutput(containerOutput); got != 2 {
		t.Errorf("countPruneOutput() = %d, want 2", got)
	}

	const emptyOutput = `Total reclaimed space: 0B`
	if got := countPruneOutput(emptyOutput); got != 0 {
		t.Errorf("countPruneOutput() = %d, want 0", got)
	}
}
