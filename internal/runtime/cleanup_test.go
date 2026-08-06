package runtime

import (
	"context"
	"os"
	"path/filepath"
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
		name string
		got  []string
		want []string
	}{
		{"containers", pruneContainersArgs(), []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"images", pruneImagesArgs(), []string{"image", "prune", "-f"}},
		{"volumes", pruneVolumesArgs(), []string{"volume", "prune", "-f"}},
		{"networks", pruneNetworksArgs(), []string{"network", "prune", "-f"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.got) != len(tt.want) {
				t.Fatalf("len mismatch: got %v, want %v", tt.got, tt.want)
			}
			for i := range tt.want {
				if tt.got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, tt.got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDryListArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{"containers", containerDryListArgs(), []string{"ps", "-a", "--filter", "status=exited", "--format", "{{json .}}"}},
		{"images", imageDryListArgs(), []string{"images", "-a", "--filter", "dangling=true", "--format", "{{.ID}}"}},
		{"volumes", volumeDryListArgs(), []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}},
		{"networks", networkDryListArgs(), []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.ID}}"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.got) != len(tt.want) {
				t.Fatalf("len mismatch: got %v, want %v", tt.got, tt.want)
			}
			for i := range tt.want {
				if tt.got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, tt.got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name  string
		out   string
		wantN int
		wantB int64
	}{
		{"containers", "Deleted Containers:\n9b4a\n3f2c\n\nTotal reclaimed space: 5MB\n", 2, 5 << 20},
		{"images skips untagged", "Deleted Images:\nuntagged: sha256:aaa\ndeleted: sha256:ccc\n\nTotal reclaimed space: 2GB\n", 1, 2 << 30},
		{"volumes", "Deleted Volumes:\nvol1\nvol2\n\nTotal reclaimed space: 1.4kB\n", 2, 1433},
		{"networks", "Deleted Networks:\nnet1\n\nTotal reclaimed space: 0B\n", 1, 0},
		{"nothing deleted", "Total reclaimed space: 0B\n", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, b := parsePruneOutput(tt.out)
			if n != tt.wantN {
				t.Errorf("count = %d, want %d", n, tt.wantN)
			}
			if b != tt.wantB {
				t.Errorf("reclaimed = %d, want %d", b, tt.wantB)
			}
		})
	}
}

func TestParseHumanSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0B", 0},
		{"12B", 12},
		{"1.4kB", 1433},
		{"5MB", 5 << 20},
		{"2GB", 2 << 30},
		{"1TB", 1 << 40},
	}
	for _, tt := range tests {
		if got := parseHumanSize(tt.in); got != tt.want {
			t.Errorf("parseHumanSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestCountNonEmptyLines(t *testing.T) {
	if got := countNonEmptyLines("a\nb\n\n"); got != 2 {
		t.Errorf("countNonEmptyLines = %d, want 2", got)
	}
	if got := countNonEmptyLines(""); got != 0 {
		t.Errorf("countNonEmptyLines(empty) = %d, want 0", got)
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil {
		t.Fatal("Cleanup() returned nil result")
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.VolumesRemoved != 0 || res.NetworksRemoved != 0 || res.ReclaimedBytes != 0 {
		t.Errorf("expected zeroed result, got %+v", res)
	}
}

func TestStubSatisfiesManager(t *testing.T) {
	var m Manager = NewStub()
	if m == nil {
		t.Fatal("NewStub() does not implement Manager")
	}
}

const fakeDockerScript = `#!/bin/sh
case "$1" in
  ps)
    printf '%s\n' \
      '{"ID":"1111","Name":"orphan-app","Labels":"","State":"exited"}' \
      '{"ID":"2222","Name":"tengiz-myapp","Labels":"tengiz-app=myapp","State":"exited"}'
    ;;
  container)
    printf 'Deleted Containers:\n111\n\nTotal reclaimed space: 5MB\n'
    ;;
  images)
    printf 'sha256:aaa\nsha256:bbb\n'
    ;;
  image)
    printf 'Deleted Images:\nuntagged: sha256:aaa\ndeleted: sha256:ccc\n\nTotal reclaimed space: 2GB\n'
    ;;
  volume)
    if [ "$2" = "ls" ]; then
      printf 'vol1\n'
    else
      printf 'Deleted Volumes:\nvol1\nvol2\n\nTotal reclaimed space: 1.4kB\n'
    fi
    ;;
  network)
    if [ "$2" = "ls" ]; then
      printf 'net1\n'
    else
      printf 'Deleted Networks:\nnet1\n\nTotal reclaimed space: 0B\n'
    fi
    ;;
  *)
    exit 3
    ;;
esac
`

func setupFakeDocker(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	if err := os.WriteFile(script, []byte(fakeDockerScript), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestDockerRuntimeCleanupLive(t *testing.T) {
	setupFakeDocker(t)
	r := &dockerRuntime{}

	res, err := r.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
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
	if res.VolumesRemoved != 2 {
		t.Errorf("VolumesRemoved = %d, want 2", res.VolumesRemoved)
	}
	if res.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", res.NetworksRemoved)
	}
	wantBytes := int64(5<<20) + int64(2<<30) + 1433
	if res.ReclaimedBytes != wantBytes {
		t.Errorf("ReclaimedBytes = %d, want %d", res.ReclaimedBytes, wantBytes)
	}
}

func TestDockerRuntimeCleanupDryRun(t *testing.T) {
	setupFakeDocker(t)
	r := &dockerRuntime{}

	res, err := r.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("Cleanup(dry run) error = %v", err)
	}
	// container dry-list parses JSON labels: the tengiz-app labeled one is skipped
	if res.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1 (tengiz-labeled container excluded)", res.ContainersRemoved)
	}
	if res.ImagesRemoved != 2 {
		t.Errorf("ImagesRemoved = %d, want 2", res.ImagesRemoved)
	}
	if res.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", res.VolumesRemoved)
	}
	if res.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", res.NetworksRemoved)
	}
	if res.ReclaimedBytes != 0 {
		t.Errorf("ReclaimedBytes = %d, want 0 (dry run must not delete)", res.ReclaimedBytes)
	}
}

func TestDockerRuntimeCleanupSelective(t *testing.T) {
	setupFakeDocker(t)
	r := &dockerRuntime{}

	res, err := r.Cleanup(context.Background(), CleanupOptions{Volumes: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.VolumesRemoved != 2 {
		t.Errorf("VolumesRemoved = %d, want 2", res.VolumesRemoved)
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.NetworksRemoved != 0 {
		t.Errorf("unrequested categories were touched: %+v", res)
	}
}
