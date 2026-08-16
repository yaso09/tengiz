package runtime

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCountDeleted(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
	}{
		{"empty output", "", 0},
		{"nothing deleted", "Total reclaimed space: 0B", 0},
		{
			"one container",
			"Deleted Containers:\nabc123abc123\n\nTotal reclaimed space: 0B",
			1,
		},
		{
			"two images with sha256 prefix",
			"Deleted Images:\nsha256:abc123abc123\nsha256:def456def456\n\nTotal reclaimed space: 1.2kB",
			2,
		},
		{
			"named volume",
			"Deleted Volumes:\nmyapp_data\n\nTotal reclaimed space: 0B",
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countDeleted(tt.output); got != tt.want {
				t.Errorf("countDeleted() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBuildPruneCommand(t *testing.T) {
	tests := []struct {
		category CleanupCategory
		want     []string
	}{
		{CleanupContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{CleanupImages, []string{"image", "prune", "-f"}},
		{CleanupNetworks, []string{"network", "prune", "-f"}},
		{CleanupVolumes, []string{"volume", "prune", "-f"}},
	}
	for _, tt := range tests {
		got := buildPruneCommand(tt.category)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("buildPruneCommand(%q) = %v, want %v", tt.category, got, tt.want)
		}
	}
}

func TestShouldPruneImage(t *testing.T) {
	tests := []struct {
		name           string
		reference      string
		imageID        string
		usedContainers string
		want           bool
	}{
		{"tengiz image protected", "tengiz-apps/myapp:v1", "img1", "", false},
		{"dangling image skipped", "<none>:<none>", "img2", "", false},
		{"image in use skipped", "busybox:latest", "img3", "c1\nc2\n", false},
		{"empty reference skipped", "", "img4", "", false},
		{"unused non-tengiz pruned", "busybox:latest", "img5", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldPruneImage(tt.reference, tt.imageID, tt.usedContainers); got != tt.want {
				t.Errorf("shouldPruneImage(%q, %q, %q) = %v, want %v",
					tt.reference, tt.imageID, tt.usedContainers, got, tt.want)
			}
		})
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
	}
}

func TestDockerCleanupRunsPruneCommands(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "calls.log")
	fakeDocker := filepath.Join(dir, "docker")

	script := `#!/bin/sh
echo "$@" >> "$LOG"
case "$1" in
  container)
    echo "Deleted Containers:"
    echo "abc123abc123"
    echo
    echo "Total reclaimed space: 0B"
    ;;
  network)
    echo "Deleted Networks:"
    echo "net1"
    echo
    echo "Total reclaimed space: 0B"
    ;;
  volume)
    echo "Deleted Volumes:"
    echo "vol1"
    echo
    echo "Total reclaimed space: 0B"
    ;;
  image)
    if [ "$2" = "ls" ]; then
      echo "tengiz-apps/myapp:v1|img1"
      echo "busybox:latest|img2"
      echo "<none>:<none>|img3"
    else
      echo "Deleted Images:"
      echo "img3"
      echo
      echo "Total reclaimed space: 0B"
    fi
    ;;
  ps)
    # busybox (img2) is unused: print nothing
    ;;
  rmi)
    ;;
esac
`
	script = strings.ReplaceAll(script, "$LOG", logFile)
	if err := os.WriteFile(fakeDocker, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}

	report, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true, Images: true, Networks: true, Volumes: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", report.ContainersRemoved)
	}
	if report.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", report.NetworksRemoved)
	}
	if report.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", report.VolumesRemoved)
	}
	if report.DanglingImagesRemoved != 1 {
		t.Errorf("DanglingImagesRemoved = %d, want 1", report.DanglingImagesRemoved)
	}
	if report.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1 (busybox:latest)", report.ImagesRemoved)
	}

	logContent, _ := os.ReadFile(logFile)
	if !strings.Contains(string(logContent), "label!=tengiz-app") {
		t.Errorf("container prune missing label filter, calls:\n%s", logContent)
	}
	if strings.Contains(string(logContent), "rmi -f tengiz-apps") {
		t.Errorf("tengiz-managed image was removed:\n%s", logContent)
	}
}
