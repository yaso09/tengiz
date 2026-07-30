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

func TestPruneReport_DryRun(t *testing.T) {
	r := &dockerRuntime{}
	_, err := r.PruneContainers(context.Background(), "production", true)
	if err != nil {
		t.Fatalf("PruneContainers dry run failed: %v", err)
	}
}

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty output",
			input:    "",
			expected: nil,
		},
		{
			name:     "only reclaimed line",
			input:    "Total reclaimed space: 1.2GB",
			expected: nil,
		},
		{
			name:     "container IDs",
			input:    "abc123\ndef456\nTotal reclaimed space: 500MB",
			expected: []string{"abc123", "def456"},
		},
		{
			name:     "deleted image tags",
			input:    "untagged: tengiz-apps/myapp:production-oldtag\nuntagged: tengiz-apps/myapp:staging-oldtag\nTotal reclaimed space: 1GB",
			expected: []string{
				"untagged: tengiz-apps/myapp:production-oldtag",
				"untagged: tengiz-apps/myapp:staging-oldtag",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parsePruneOutput(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d items, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, item := range result {
				if item != tt.expected[i] {
					t.Errorf("item %d: expected %q, got %q", i, tt.expected[i], item)
				}
			}
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"0B", 0},
		{"100", 100},
		{"1kB", 1000},
		{"1.5MB", 1500000},
		{"2GB", 2000000000},
		{"1KiB", 1024},
		{"1MiB", 1048576},
		{"1GiB", 1073741824},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseSize(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestParseSize_Errors(t *testing.T) {
	_, err := parseSize("invalid")
	if err == nil {
		t.Error("expected error for invalid string")
	}
	_, err = parseSize("unknown")
	if err == nil {
		t.Error("expected error for unknown unit")
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1500, "1.5 kB"},
		{1000000, "1.0 MB"},
		{2000000000, "2.0 GB"},
		{1500000000000, "1.5 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := humanBytes(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
