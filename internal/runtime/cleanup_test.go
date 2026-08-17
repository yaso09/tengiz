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

func TestBuildPruneCommands(t *testing.T) {
	tests := []struct {
		name     string
		opts     PruneOptions
		expected [][]string
	}{
		{
			name: "no categories",
			opts: PruneOptions{},
			expected: nil,
		},
		{
			name: "containers only",
			opts: PruneOptions{Containers: true},
			expected: [][]string{
				{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
			},
		},
		{
			name: "images dangling",
			opts: PruneOptions{Images: true},
			expected: [][]string{
				{"image", "prune", "-f"},
			},
		},
		{
			name: "images all",
			opts: PruneOptions{Images: true, All: true},
			expected: [][]string{
				{"image", "prune", "-f", "-a"},
			},
		},
		{
			name: "networks only",
			opts: PruneOptions{Networks: true},
			expected: [][]string{
				{"network", "prune", "-f"},
			},
		},
		{
			name: "volumes only",
			opts: PruneOptions{Volumes: true},
			expected: [][]string{
				{"volume", "prune", "-f"},
			},
		},
		{
			name: "build cache only",
			opts: PruneOptions{BuildCache: true},
			expected: [][]string{
				{"builder", "prune", "-f"},
			},
		},
		{
			name: "all categories",
			opts: PruneOptions{Containers: true, Images: true, Networks: true, Volumes: true, BuildCache: true, All: true},
			expected: [][]string{
				{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
				{"image", "prune", "-f", "-a"},
				{"network", "prune", "-f"},
				{"volume", "prune", "-f"},
				{"builder", "prune", "-f"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneCommands(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildPruneCommands() = %v, want %v", got, tt.expected)
			}
			for i := range got {
				if len(got[i]) != len(tt.expected[i]) {
					t.Fatalf("command %d: got %v, want %v", i, got[i], tt.expected[i])
				}
				for j := range got[i] {
					if got[i][j] != tt.expected[i][j] {
						t.Fatalf("command %d arg %d: got %q, want %q", i, j, got[i][j], tt.expected[i][j])
					}
				}
			}
		})
	}
}
