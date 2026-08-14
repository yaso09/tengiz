package runtime

import (
	"context"
	"fmt"
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

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantCount int64
		wantFreed int64
	}{
		{
			name:      "nothing to delete",
			output:    "Total reclaimed space: 0B\n",
			wantCount: 0,
			wantFreed: 0,
		},
		{
			name: "containers",
			output: "Deleted Containers:\n" +
				"abc123\n" +
				"def456\n" +
				"\n" +
				"Total reclaimed space: 12.5MB\n",
			wantCount: 2,
			wantFreed: 12500000,
		},
		{
			name: "images",
			output: "Deleted Images:\n" +
				"deleted: sha256:abc\n" +
				"untagged: tengiz-apps/foo:production-123\n" +
				"deleted: sha256:def\n" +
				"Total reclaimed space: 523MB\n",
			wantCount: 3,
			wantFreed: 523000000,
		},
		{
			name: "empty deleted section",
			output: "Deleted Networks:\n" +
				"Total reclaimed space: 0B\n",
			wantCount: 0,
			wantFreed: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, freed := parsePruneOutput(tt.output)
			if count != tt.wantCount {
				t.Errorf("count = %d, want %d", count, tt.wantCount)
			}
			if freed != tt.wantFreed {
				t.Errorf("freed = %d, want %d", freed, tt.wantFreed)
			}
		})
	}
}

func TestParseBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0B", 0},
		{"100B", 100},
		{"1.5MB", 1500000},
		{"2GB", 2000000000},
		{"1GiB", 1073741824},
		{"512KiB", 524288},
		{"garbage", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := parseBytes(tt.in); got != tt.want {
				t.Errorf("parseBytes(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1048576, "1.0 MiB"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.in), func(t *testing.T) {
			if got := FormatBytes(tt.in); got != tt.want {
				t.Errorf("FormatBytes(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDockerCleanupDryRun(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Cleanup(context.Background(), CleanupOptions{
		DryRun:     true,
		Containers: true,
		Images:     true,
		BuildCache: true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup(dry-run) error = %v", err)
	}
	if res.ContainersPruned != 0 || res.ImagesPruned != 0 || res.BuildCacheFreed != 0 {
		t.Errorf("dry-run should not prune anything, got %+v", res)
	}
}
