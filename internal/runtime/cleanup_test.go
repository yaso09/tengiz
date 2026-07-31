package runtime

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	result, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !result.DryRun {
		t.Errorf("CleanupResult.DryRun = false, want true")
	}
}

func TestContainerPruneCmd(t *testing.T) {
	if got, want := containerPruneCmd(true), []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}; !reflect.DeepEqual(got, want) {
		t.Errorf("containerPruneCmd(true) = %v, want %v", got, want)
	}
	if got, want := containerPruneCmd(false), []string{"container", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("containerPruneCmd(false) = %v, want %v", got, want)
	}
}

func TestImagePruneCmd(t *testing.T) {
	if got, want := imagePruneCmd(), []string{"image", "prune", "-a", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("imagePruneCmd() = %v, want %v", got, want)
	}
}

func TestVolumePruneCmd(t *testing.T) {
	if got, want := volumePruneCmd(), []string{"volume", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("volumePruneCmd() = %v, want %v", got, want)
	}
}

func TestNetworkPruneCmd(t *testing.T) {
	if got, want := networkPruneCmd(), []string{"network", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("networkPruneCmd() = %v, want %v", got, want)
	}
}

func TestBuildCachePruneCmd(t *testing.T) {
	if got, want := buildCachePruneCmd(), []string{"builder", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildCachePruneCmd() = %v, want %v", got, want)
	}
}

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name      string
		out       string
		wantCount int
		wantSpace string
	}{
		{
			name:      "containers",
			out:       "Deleted Containers:\nabc123\n\nTotal reclaimed space: 4.096kB\n",
			wantCount: 1,
			wantSpace: "4.096kB",
		},
		{
			name:      "images",
			out:       "Untagged: nginx:latest\nDeleted Images:\ndeleted: sha256:xyz\n\nTotal reclaimed space: 25.5MB\n",
			wantCount: 1,
			wantSpace: "25.5MB",
		},
		{
			name:      "networks has no reclaimed line",
			out:       "Deleted Networks:\nnet1\n",
			wantCount: 1,
			wantSpace: "",
		},
		{
			name:      "empty output",
			out:       "",
			wantCount: 0,
			wantSpace: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count, space := parsePruneOutput(tc.out)
			if count != tc.wantCount {
				t.Errorf("parsePruneOutput(%q) count = %d, want %d", tc.out, count, tc.wantCount)
			}
			if space != tc.wantSpace {
				t.Errorf("parsePruneOutput(%q) space = %q, want %q", tc.out, space, tc.wantSpace)
			}
		})
	}
}

func TestStoppedContainerNames(t *testing.T) {
	names := stoppedContainerNames("tengiz-myapp|Exited (0) 5 minutes ago\nfoo|Up 2 hours\nbar|Created\n")
	if !reflect.DeepEqual(names, []string{"tengiz-myapp", "bar"}) {
		t.Errorf("stoppedContainerNames() = %v, want [tengiz-myapp bar]", names)
	}
}

func TestUnusedImageRefs(t *testing.T) {
	images := "sha256:img1|nginx:latest\nsha256:img2|<none>\nsha256:img3|tengiz-apps/myapp:v1\nsha256:img4|alpine:3.19\n"
	containers := "nginx:latest\ntengiz-apps/myapp:v1\n"
	unused := unusedImageRefs(images, containers)
	if !reflect.DeepEqual(unused, []string{"sha256:img2", "alpine:3.19"}) {
		t.Errorf("unusedImageRefs() = %v, want [sha256:img2 alpine:3.19]", unused)
	}
}

func TestReclaimableBuildCacheSize(t *testing.T) {
	out := `{"ID":"a","Reclaimable":true,"Size":100}
{"ID":"b","Reclaimable":false,"Size":50}
{"ID":"c","Reclaimable":true,"Size":25}`
	if got := reclaimableBuildCacheSize(out); got != 125 {
		t.Errorf("reclaimableBuildCacheSize() = %d, want 125", got)
	}
}

func TestNonEmptyLines(t *testing.T) {
	if got, want := nonEmptyLines("  a \nb\n\n"), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("nonEmptyLines() = %v, want %v", got, want)
	}
}

const fakeDockerScript = `#!/bin/sh
echo "$*" >> "$TENGIZ_FAKE_DOCKER_LOG"
case "$1 $2" in
"container prune")
	echo "Deleted Containers:"
	echo "abc123"
	echo ""
	echo "Total reclaimed space: 4.096kB"
	;;
"image prune")
	echo "Untagged: nginx:latest"
	echo "Deleted Images:"
	echo "deleted: sha256:xyz"
	echo ""
	echo "Total reclaimed space: 25.5MB"
	;;
"volume prune")
	echo "Deleted Volumes:"
	echo "vol1"
	echo ""
	echo "Total reclaimed space: 0B"
	;;
"network prune")
	echo "Deleted Networks:"
	echo "net1"
	;;
"builder prune")
	echo "Deleted build cache objects:"
	echo "obj1"
	echo ""
	echo "Total reclaimed space: 42.3MB"
	;;
"ps -a")
	case "$*" in
	*--no-trunc*)
		echo "nginx:latest"
		echo "tengiz-apps/myapp:v1"
		;;
	*"label!=tengiz-app"*)
		echo "worker123|Exited (1) 2 hours ago"
		;;
	*)
		echo "tengiz-myapp|Exited (0) 5 minutes ago"
		echo "worker123|Exited (1) 2 hours ago"
		;;
	esac
	;;
"images --no-trunc")
	echo "sha256:img1|nginx:latest"
	echo "sha256:img2|<none>"
	;;
"volume ls")
	echo "vol1"
	;;
"network ls")
	echo "net1"
	;;
"builder du")
	echo '{"ID":"obj1","Reclaimable":true,"Size":44323328}'
	;;
esac
`

func newFakeDocker(t *testing.T, script string) (Manager, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "docker")
	logPath := filepath.Join(dir, "docker.log")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("TENGIZ_FAKE_DOCKER_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	rt, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() with fake docker: %v", err)
	}
	return rt, logPath
}

func TestDockerRuntimeCleanupPrunesResources(t *testing.T) {
	rt, logPath := newFakeDocker(t, fakeDockerScript)
	result, err := rt.Cleanup(context.Background(), CleanupOptions{
		Containers:              true,
		Images:                  true,
		Volumes:                 true,
		Networks:                true,
		BuildCache:              true,
		ProtectTengizContainers: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.DryRun {
		t.Error("DryRun = true, want false")
	}
	if result.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", result.ContainersRemoved)
	}
	if result.ContainersSpace != "4.096kB" {
		t.Errorf("ContainersSpace = %q, want 4.096kB", result.ContainersSpace)
	}
	if result.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1", result.ImagesRemoved)
	}
	if result.ImagesSpace != "25.5MB" {
		t.Errorf("ImagesSpace = %q, want 25.5MB", result.ImagesSpace)
	}
	if result.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", result.VolumesRemoved)
	}
	if result.VolumesSpace != "0B" {
		t.Errorf("VolumesSpace = %q, want 0B", result.VolumesSpace)
	}
	if result.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", result.NetworksRemoved)
	}
	if result.BuildCacheSpace != "42.3MB" {
		t.Errorf("BuildCacheSpace = %q, want 42.3MB", result.BuildCacheSpace)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	logStr := string(logData)
	for _, want := range []string{
		"container prune -f --filter label!=tengiz-app",
		"image prune -a -f",
		"volume prune -f",
		"network prune -f",
		"builder prune -f",
	} {
		if !strings.Contains(logStr, want) {
			t.Errorf("docker log missing %q; got:\n%s", want, logStr)
		}
	}
}

func TestDockerRuntimeCleanupDryRun(t *testing.T) {
	rt, _ := newFakeDocker(t, fakeDockerScript)
	result, err := rt.Cleanup(context.Background(), CleanupOptions{
		Containers:              true,
		Images:                  true,
		Volumes:                 true,
		Networks:                true,
		BuildCache:              true,
		ProtectTengizContainers: true,
		DryRun:                  true,
	})
	if err != nil {
		t.Fatalf("Cleanup() dry-run error = %v", err)
	}
	if !result.DryRun {
		t.Error("DryRun = false, want true")
	}
	if result.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1 (tengiz-managed containers excluded)", result.ContainersRemoved)
	}
	if result.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1 (nginx:latest used, <none> dangling)", result.ImagesRemoved)
	}
	if result.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", result.VolumesRemoved)
	}
	if result.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", result.NetworksRemoved)
	}
	if result.BuildCacheBytes != 44323328 {
		t.Errorf("BuildCacheBytes = %d, want 44323328", result.BuildCacheBytes)
	}
	if result.ContainersSpace != "" || result.ImagesSpace != "" {
		t.Error("dry-run must not report reclaimed space (nothing was removed)")
	}
}
