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

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	reclaimed, err := m.PruneContainers(context.Background())
	if err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
	if reclaimed != 0 {
		t.Errorf("PruneContainers() = %d, want 0", reclaimed)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	reclaimed, err := m.PruneImages(context.Background(), false)
	if err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if reclaimed != 0 {
		t.Errorf("PruneImages() = %d, want 0", reclaimed)
	}
}

func TestStubPruneVolumes(t *testing.T) {
	m := NewStub()
	reclaimed, err := m.PruneVolumes(context.Background())
	if err != nil {
		t.Fatalf("PruneVolumes() error = %v", err)
	}
	if reclaimed != 0 {
		t.Errorf("PruneVolumes() = %d, want 0", reclaimed)
	}
}

func TestStubPruneNetworks(t *testing.T) {
	m := NewStub()
	reclaimed, err := m.PruneNetworks(context.Background())
	if err != nil {
		t.Fatalf("PruneNetworks() error = %v", err)
	}
	if reclaimed != 0 {
		t.Errorf("PruneNetworks() = %d, want 0", reclaimed)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	reclaimed, err := m.PruneBuildCache(context.Background(), true)
	if err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
	if reclaimed != 0 {
		t.Errorf("PruneBuildCache() = %d, want 0", reclaimed)
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	info, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if info == nil {
		t.Fatal("DiskUsage() returned nil")
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{"0", 0},
		{"", 0},
		{"1.5GB", 1500000000},
		{"234MB", 234000000},
		{"1GiB", 1073741824},
		{"500KiB", 512000},
		{"100B", 100},
		{"2.5TB", 2500000000000},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseSize(tt.input)
			if got != tt.want {
				t.Errorf("parseSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParsePruneOutput(t *testing.T) {
	output := []byte("Deleted Containers:\nabc123\nTotal reclaimed space: 1.234GB\n")
	got := parsePruneOutput(output)
	if got != 1234000000 {
		t.Errorf("parsePruneOutput() = %d, want 1234000000", got)
	}

	outputNoMatch := []byte("Nothing to clean")
	got = parsePruneOutput(outputNoMatch)
	if got != 0 {
		t.Errorf("parsePruneOutput() expected 0, got %d", got)
	}
}
