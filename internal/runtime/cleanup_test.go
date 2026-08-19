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

func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int64
	}{
		{"empty output", "", 0},
		{"no match", "Nothing to prune", 0},
		{"zero bytes", "Total reclaimed space: 0B", 0},
		{"lowercase k", "Total reclaimed space: 12.5kB", 12500},
		{"megabytes", "Total reclaimed space: 3.5MB", 3500000},
		{"gigabytes", "Total reclaimed space: 1.25GB", 1250000000},
		{"no space before unit", "Total reclaimed space: 500MB", 500000000},
		{"uppercase unit", "Total reclaimed space: 2KB", 2000},
		{"unit is case insensitive", "Total reclaimed space: 1.5gb", 1500000000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseReclaimed(tt.output)
			if got != tt.want {
				t.Errorf("parseReclaimed(%q) = %d, want %d", tt.output, got, tt.want)
			}
		})
	}
}
