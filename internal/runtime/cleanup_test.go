package runtime

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), types.CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
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

func TestParseSpace(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"0B", 0},
		{"", 0},
		{"100B", 100},
		{"1KB", 1000},
		{"1MB", 1000 * 1000},
		{"1GB", 1000 * 1000 * 1000},
		{"1.5MB", 1500000},
		{"2KiB", 2048},
		{"3MiB", 3 * 1024 * 1024},
	}
	for _, tt := range tests {
		got := parseSpace(tt.input)
		if got != tt.want {
			t.Errorf("parseSpace(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParsePruneOutput(t *testing.T) {
	output := "Deleted Images:\nuntagged: foo:latest\ndeleted: sha256:abc\ndeleted: sha256:def\n\nTotal reclaimed space: 1.5GB"
	n, freed := parsePruneOutput(output)
	if n != 3 {
		t.Errorf("count = %d, want 3", n)
	}
	if freed != 1500000000 {
		t.Errorf("space = %d, want 1500000000", freed)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	output := "Total reclaimed space: 0B"
	n, freed := parsePruneOutput(output)
	if n != 0 {
		t.Errorf("count = %d, want 0", n)
	}
	if freed != 0 {
		t.Errorf("space = %d, want 0", freed)
	}
}

func TestParseBuildCacheOutput(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"123.4MB", 123400000},
		{"Total: 5GB", 5000000000},
		{"0B", 0},
	}
	for _, tt := range tests {
		got := parseBuildCacheOutput(tt.input)
		if got != tt.want {
			t.Errorf("parseBuildCacheOutput(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestCountPrunedLines(t *testing.T) {
	output := "Deleted Containers:\ntengiz-helper\nbuild-cache-abc\n\nTotal reclaimed space: 100MB"
	count := countPrunedLines(output)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestCountPrunedLinesEmpty(t *testing.T) {
	output := "Total reclaimed space: 0B"
	count := countPrunedLines(output)
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}
