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

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		output    string
		wantCount int
		wantSpace string
	}{
		{
			name:      "containers with two entries",
			kind:      "container",
			output:    "Deleted Containers:\nabcd1234abcd\nefef1234efef\n\nTotal reclaimed space: 123.4MB\n",
			wantCount: 2,
			wantSpace: "123.4MB",
		},
		{
			name:      "images with untagged lines",
			kind:      "image",
			output:    "Deleted Images:\nuntagged: tengiz-apps/foo:latest\nuntagged: sha256:abc123\n\nTotal reclaimed space: 2.3GB\n",
			wantCount: 2,
			wantSpace: "2.3GB",
		},
		{
			name:      "networks no reclaimed line",
			kind:      "network",
			output:    "Deleted Networks:\nfoo_network\n",
			wantCount: 1,
			wantSpace: "",
		},
		{
			name:      "volumes",
			kind:      "volume",
			output:    "Deleted Volumes:\nvol1\n\nTotal reclaimed space: 4.5MB\n",
			wantCount: 1,
			wantSpace: "4.5MB",
		},
		{
			name:      "build cache",
			kind:      "builder",
			output:    "Deleted Build Cache Entry:\nsha256:abc123\n\nTotal reclaimed space: 5.4MB\n",
			wantCount: 1,
			wantSpace: "5.4MB",
		},
		{
			name:      "empty output",
			kind:      "container",
			output:    "",
			wantCount: 0,
			wantSpace: "",
		},
		{
			name:      "nothing to prune",
			kind:      "image",
			output:    "Total reclaimed space: 0B\n",
			wantCount: 0,
			wantSpace: "0B",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, space := parsePruneOutput(tt.kind, tt.output)
			if count != tt.wantCount {
				t.Errorf("parsePruneOutput(%q).count = %d, want %d", tt.kind, count, tt.wantCount)
			}
			if space != tt.wantSpace {
				t.Errorf("parsePruneOutput(%q).space = %q, want %q", tt.kind, space, tt.wantSpace)
			}
		})
	}
}

func TestDockerPruneNoCategories(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Containers != 0 || res.Images != 0 || res.Networks != 0 || res.Volumes != 0 || res.BuildCache != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
}
