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
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersPruned != 0 || res.ImagesPruned != 0 || res.BuildCacheFreed != 0 {
		t.Errorf("stub Cleanup() should return a zero result, got %+v", res)
	}
}

func TestBuildCleanupCommands(t *testing.T) {
	tests := []struct {
		name     string
		opts     CleanupOptions
		expected [][]string
	}{
		{
			name:     "nothing enabled",
			opts:     CleanupOptions{},
			expected: nil,
		},
		{
			name: "containers only",
			opts: CleanupOptions{Containers: true},
			expected: [][]string{
				{"container", "prune", "--force", "--filter", "label=tengiz-app"},
			},
		},
		{
			name: "all categories",
			opts: CleanupOptions{Containers: true, Images: true, BuildCache: true, Volumes: true, Networks: true},
			expected: [][]string{
				{"container", "prune", "--force", "--filter", "label=tengiz-app"},
				{"image", "prune", "--force"},
				{"builder", "prune", "--force"},
				{"volume", "prune", "--force"},
				{"network", "prune", "--force"},
			},
		},
		{
			name: "images and networks",
			opts: CleanupOptions{Images: true, Networks: true},
			expected: [][]string{
				{"image", "prune", "--force"},
				{"network", "prune", "--force"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCleanupCommands(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("len = %d, want %d: %v", len(got), len(tt.expected), got)
			}
			for i := range got {
				if len(got[i]) != len(tt.expected[i]) {
					t.Fatalf("cmd %d len = %d, want %d: %v", i, len(got[i]), len(tt.expected[i]), got[i])
				}
				for j := range got[i] {
					if got[i][j] != tt.expected[i][j] {
						t.Errorf("cmd %d arg %d = %q, want %q", i, j, got[i][j], tt.expected[i][j])
					}
				}
			}
		})
	}
}
