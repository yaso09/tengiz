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

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     CleanupOptions
		expected []string
	}{
		{
			name:     "default",
			opts:     CleanupOptions{},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all",
			opts:     CleanupOptions{All: true},
			expected: []string{"system", "prune", "-f", "-a", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "volumes",
			opts:     CleanupOptions{Volumes: true},
			expected: []string{"system", "prune", "-f", "--volumes", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all and volumes",
			opts:     CleanupOptions{All: true, Volumes: true},
			expected: []string{"system", "prune", "-f", "-a", "--volumes", "--filter", "label!=tengiz-app"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneArgs(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("buildPruneArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParsePruneOutput(t *testing.T) {
	output := `Deleted Containers:
9bfb1cdc7c1a
abc123def456

Deleted Images:
untagged: foo:latest
deleted: sha256:aaaa1111
deleted: sha256:bbbb2222

Deleted Networks:
net1

Deleted Volumes:
vol1
vol2

Deleted Build Cache Objects:
cache1

Total reclaimed space: 1.2GB
`
	res := parsePruneOutput(output)
	if res.ContainersRemoved != 2 {
		t.Errorf("ContainersRemoved = %d, want 2", res.ContainersRemoved)
	}
	if res.ImagesRemoved != 3 {
		t.Errorf("ImagesRemoved = %d, want 3", res.ImagesRemoved)
	}
	if res.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", res.NetworksRemoved)
	}
	if res.VolumesRemoved != 2 {
		t.Errorf("VolumesRemoved = %d, want 2", res.VolumesRemoved)
	}
	if res.BuildCacheRemoved != 1 {
		t.Errorf("BuildCacheRemoved = %d, want 1", res.BuildCacheRemoved)
	}
	if res.SpaceReclaimed != "1.2GB" {
		t.Errorf("SpaceReclaimed = %q, want %q", res.SpaceReclaimed, "1.2GB")
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	res := parsePruneOutput("")
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.NetworksRemoved != 0 || res.VolumesRemoved != 0 || res.BuildCacheRemoved != 0 || res.SpaceReclaimed != "" {
		t.Errorf("expected zero-value result, got %+v", res)
	}
}

func TestCountLines(t *testing.T) {
	if n := countLines(""); n != 0 {
		t.Errorf("countLines(\"\") = %d, want 0", n)
	}
	if n := countLines("   \n"); n != 0 {
		t.Errorf("countLines(blank) = %d, want 0", n)
	}
	if n := countLines("a\nb\nc\n"); n != 3 {
		t.Errorf("countLines(a/b/c) = %d, want 3", n)
	}
}
