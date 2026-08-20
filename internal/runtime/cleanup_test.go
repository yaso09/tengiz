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
	results, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if results != nil {
		t.Errorf("Cleanup() = %v, want nil", results)
	}
}

func TestCleanupConstants(t *testing.T) {
	want := []CleanupCategory{
		CleanupContainers,
		CleanupImages,
		CleanupVolumes,
		CleanupNetworks,
		CleanupBuildCache,
	}
	if len(AllCleanupCategories) != len(want) {
		t.Fatalf("AllCleanupCategories len = %d, want %d", len(AllCleanupCategories), len(want))
	}
	for i := range want {
		if AllCleanupCategories[i] != want[i] {
			t.Errorf("AllCleanupCategories[%d] = %q, want %q", i, AllCleanupCategories[i], want[i])
		}
	}
}

func TestCleanupCategoriesDefault(t *testing.T) {
	got := cleanupCategories(nil)
	if len(got) != len(AllCleanupCategories) {
		t.Fatalf("cleanupCategories(nil) len = %d, want %d", len(got), len(AllCleanupCategories))
	}
	for i := range got {
		if got[i] != AllCleanupCategories[i] {
			t.Errorf("cleanupCategories(nil)[%d] = %q, want %q", i, got[i], AllCleanupCategories[i])
		}
	}
}

func TestCleanupCategoriesExplicit(t *testing.T) {
	want := []CleanupCategory{CleanupContainers, CleanupImages}
	got := cleanupCategories(want)
	if len(got) != 2 || got[0] != CleanupContainers || got[1] != CleanupImages {
		t.Errorf("cleanupCategories() = %v, want %v", got, want)
	}
}

func TestCleanupCommandArgs(t *testing.T) {
	tests := []struct {
		name     string
		category CleanupCategory
		dryRun   bool
		expected []string
	}{
		{
			name:     "containers",
			category: CleanupContainers,
			expected: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "containers dry run",
			category: CleanupContainers,
			dryRun:   true,
			expected: []string{"container", "prune", "-f", "--dry-run", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "images",
			category: CleanupImages,
			expected: []string{"image", "prune", "-f"},
		},
		{
			name:     "volumes",
			category: CleanupVolumes,
			expected: []string{"volume", "prune", "-f"},
		},
		{
			name:     "networks",
			category: CleanupNetworks,
			expected: []string{"network", "prune", "-f"},
		},
		{
			name:     "build cache",
			category: CleanupBuildCache,
			expected: []string{"builder", "prune", "-f"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanupCommandArgs(tt.category, tt.dryRun)
			if len(got) != len(tt.expected) {
				t.Fatalf("cleanupCommandArgs() = %v, want %v", got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("arg[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected uint64
	}{
		{"empty", "", 0},
		{"no marker", "Deleted Containers:\n", 0},
		{"bytes", "Deleted Containers:\nabc123\n\nTotal reclaimed space: 512 B\n", 512},
		{"kb", "Total reclaimed space: 12.5 kB\n", 12800},
		{"mb", "Total reclaimed space: 1.5 MB\n", 1572864},
		{"gb", "Total reclaimed space: 2 GB\n", 2147483648},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseReclaimedSpace(tt.output)
			if got != tt.expected {
				t.Errorf("parseReclaimedSpace(%q) = %d, want %d", tt.output, got, tt.expected)
			}
		})
	}
}
