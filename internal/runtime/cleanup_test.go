package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		filters  []string
		expected []string
	}{
		{"container", "container", []string{"label=tengiz-deployment"}, []string{"container", "prune", "-f", "--filter", "label=tengiz-deployment"}},
		{"image", "image", []string{"dangling=true"}, []string{"image", "prune", "-f", "--filter", "dangling=true"}},
		{"volume-no-filters", "volume", nil, []string{"volume", "prune", "-f"}},
		{"network-no-filters", "network", nil, []string{"network", "prune", "-f"}},
		{"builder-no-filters", "builder", nil, []string{"builder", "prune", "-f"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneArgs(tt.resource, tt.filters)
			if len(got) != len(tt.expected) {
				t.Fatalf("pruneArgs() = %v, want %v", got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("pruneArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestCountRemoved(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
	}{
		{
			name: "containers",
			output: `Deleted Containers:
abcdef1234567890
Total reclaimed space: 5.3kB
`,
			want: 1,
		},
		{
			name: "images skips untagged lines",
			output: `Deleted Images:
untagged: tengiz-apps/myapp:old
deleted: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
Total reclaimed space: 1.2MB
`,
			want: 1,
		},
		{
			name: "volumes",
			output: `Deleted Volumes:
myapp-volume
Total reclaimed space: 12B
`,
			want: 1,
		},
		{
			name:   "no deletions",
			output: `Total reclaimed space: 0B
`,
			want: 0,
		},
		{
			name:   "empty",
			output: ``,
			want:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countRemoved(tt.output); got != tt.want {
				t.Errorf("countRemoved() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil || res.ContainersRemoved != 0 || res.ImagesRemoved != 0 {
		t.Errorf("stub Cleanup result = %+v, want zeroed result", res)
	}
}

const fakeDockerScript = `#!/usr/bin/env bash
case "$1" in
  container)
    echo "Deleted Containers:"
    echo "abcdef1234567890"
    echo "Total reclaimed space: 5.3kB"
    ;;
  image)
    echo "Deleted Images:"
    echo "untagged: tengiz-apps/myapp:old"
    echo "deleted: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    echo "Total reclaimed space: 1.2MB"
    ;;
  volume)
    echo "Deleted Volumes:"
    echo "myapp-volume"
    echo "Total reclaimed space: 12B"
    ;;
  network)
    echo "Deleted Networks:"
    echo "my-custom-net"
    echo "Total reclaimed space: 0B"
    ;;
  builder)
    echo "Total cache space: 4.2kB"
    ;;
  *)
    exit 1
    ;;
esac
exit 0
`

const failingDockerScript = `#!/usr/bin/env bash
echo "volume prune exploded" >&2
exit 1
`

func withFakeDocker(t *testing.T, script string) *dockerRuntime {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &dockerRuntime{}
}

func TestDockerCleanupAllTargets(t *testing.T) {
	rt := withFakeDocker(t, fakeDockerScript)

	res, err := rt.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	if res.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", res.ContainersRemoved)
	}
	if res.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1", res.ImagesRemoved)
	}
	if res.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", res.VolumesRemoved)
	}
	if res.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", res.NetworksRemoved)
	}
	if res.BuildCacheRemoved != 0 {
		t.Errorf("BuildCacheRemoved = %d, want 0", res.BuildCacheRemoved)
	}
}

func TestDockerCleanupNoTargetsRunsNothing(t *testing.T) {
	rt := withFakeDocker(t, fakeDockerScript)

	res, err := rt.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 ||
		res.VolumesRemoved != 0 || res.NetworksRemoved != 0 || res.BuildCacheRemoved != 0 {
		t.Errorf("unexpected removals with no targets: %+v", res)
	}
}

func TestDockerCleanupPruneError(t *testing.T) {
	rt := withFakeDocker(t, failingDockerScript)

	res, err := rt.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err == nil {
		t.Fatalf("expected error, got result %+v", res)
	}
	if !strings.Contains(err.Error(), "volume prune exploded") {
		t.Errorf("error = %q, want to include fake docker stderr", err)
	}
	if res.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0 (partial results returned on error)", res.ContainersRemoved)
	}
}
