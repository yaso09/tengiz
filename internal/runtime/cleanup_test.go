package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestDockerPruneOptionsToArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     PruneOptions
		category string
		expected []string
	}{
		{
			name:     "prune containers",
			opts:     PruneOptions{Containers: true},
			category: "container",
			expected: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "prune images",
			opts:     PruneOptions{Images: true},
			category: "image",
			expected: []string{"image", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "prune images with dangling filter",
			opts:     PruneOptions{Images: true, KeepImages: 3},
			category: "image",
			expected: []string{"image", "prune", "-f", "--filter", "label!=tengiz-app", "--filter", "until=24h"},
		},
		{
			name:     "prune volumes",
			opts:     PruneOptions{Volumes: true},
			category: "volume",
			expected: []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "prune networks",
			opts:     PruneOptions{Networks: true},
			category: "network",
			expected: []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "prune build cache",
			opts:     PruneOptions{BuildCache: true},
			category: "builder",
			expected: []string{"builder", "prune", "-f"},
		},
		{
			name:     "dry run does not add -f",
			opts:     PruneOptions{Containers: true, DryRun: true},
			category: "container",
			expected: []string{"container", "prune", "--filter", "label!=tengiz-app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := pruneArgsForCategory(tt.category, tt.opts)
			if !reflect.DeepEqual(args, tt.expected) {
				t.Errorf("pruneArgsForCategory(%q, %+v) = %v, want %v", tt.category, tt.opts, args, tt.expected)
			}
		})
	}
}

func TestStubPruneReturnsEmptyReport(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{All: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
}

func TestStubOrphanedDetection(t *testing.T) {
	m := NewStub()
	containers, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(containers) != 0 {
		t.Errorf("expected 0 containers, got %d", len(containers))
	}
}
