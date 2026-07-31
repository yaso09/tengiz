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

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !report.DryRun {
		t.Error("Prune() did not propagate DryRun to report")
	}
}

func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args = %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q (args: %v)", i, got[i], want[i], got)
		}
	}
}

func TestContainerPruneArgs(t *testing.T) {
	assertArgs(t, containerPruneArgs(false), []string{
		"container", "prune", "-f",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	})
	assertArgs(t, containerPruneArgs(true), []string{
		"container", "ls", "-a",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
		"--format", "{{.ID}}\t{{.Status}}",
	})
}

func TestDanglingImagePruneArgs(t *testing.T) {
	assertArgs(t, danglingImagePruneArgs(false), []string{"image", "prune", "-f"})
	assertArgs(t, danglingImagePruneArgs(true), []string{"image", "ls", "--filter", "dangling=true", "-q"})
}

func TestNetworkPruneArgs(t *testing.T) {
	assertArgs(t, networkPruneArgs(false), []string{"network", "prune", "-f"})
	assertArgs(t, networkPruneArgs(true), []string{"network", "ls", "-q"})
}

func TestVolumePruneArgs(t *testing.T) {
	assertArgs(t, volumePruneArgs(false), []string{"volume", "prune", "-f"})
	assertArgs(t, volumePruneArgs(true), []string{"volume", "ls", "-q"})
}

func TestBuildCachePruneArgs(t *testing.T) {
	assertArgs(t, buildCachePruneArgs(false), []string{"builder", "prune", "-f"})
	assertArgs(t, buildCachePruneArgs(true), []string{"system", "df", "--format", "{{.Type}}|{{.Size}}"})
}

func TestCountPruned(t *testing.T) {
	containerOut := "Deleted Containers:\n8e3f6c2b5a71\na1b2c3d4e5f6\n\nTotal reclaimed space: 123.4MB\n"
	if got := countPruned(containerOut); got != 2 {
		t.Errorf("countPruned(container) = %d, want 2", got)
	}
	imageOut := "Deleted Images:\nuntagged: busybox:latest\ndeleted: sha256:1234567890abcdef\n\nTotal reclaimed space: 456.7MB\n"
	if got := countPruned(imageOut); got != 2 {
		t.Errorf("countPruned(image) = %d, want 2", got)
	}
	emptyOut := "Deleted Networks:\n\nTotal reclaimed space: 0B\n"
	if got := countPruned(emptyOut); got != 0 {
		t.Errorf("countPruned(empty) = %d, want 0", got)
	}
}

func TestReclaimedSpace(t *testing.T) {
	out := "Deleted Containers:\nabc123\n\nTotal reclaimed space: 234.5MB\n"
	if got := reclaimedSpace(out); got != "234.5MB" {
		t.Errorf("reclaimedSpace() = %q, want 234.5MB", got)
	}
	if got := reclaimedSpace("Deleted Networks:\n\nTotal reclaimed space: 0B\n"); got != "0B" {
		t.Errorf("reclaimedSpace(0B) = %q, want 0B", got)
	}
	if got := reclaimedSpace(""); got != "" {
		t.Errorf("reclaimedSpace(empty) = %q, want empty", got)
	}
}

func TestCountLines(t *testing.T) {
	out := "abc123\ndef456\n\nxyz789\n"
	if got := countLines(out); got != 3 {
		t.Errorf("countLines() = %d, want 3", got)
	}
	if got := countLines(""); got != 0 {
		t.Errorf("countLines(empty) = %d, want 0", got)
	}
}

func TestCountPrunableContainers(t *testing.T) {
	out := "9f4c1a2b3c4d\tUp 3 hours\n7a1b2c3d4e5f\tExited (0) 2 hours ago\n6b5a4c3d2e1f\tCreated 5 minutes ago\n5a1b2c3d4e5f\tRestarting (1) 1 second ago\n"
	if got := countPrunableContainers(out); got != 2 {
		t.Errorf("countPrunableContainers() = %d, want 2", got)
	}
}

func TestFilterUnprotectedImages(t *testing.T) {
	out := "tengiz-apps/myapp:1700000000|sha256:aaaa\nbusybox:latest|sha256:bbbb\nubuntu:22.04|sha256:cccc\n<none>:<none>|sha256:dddd\n"
	got := filterUnprotectedImages(out)
	want := []string{"sha256:bbbb", "sha256:cccc"}
	if len(got) != len(want) {
		t.Fatalf("filterUnprotectedImages() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filterUnprotectedImages()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildCacheSize(t *testing.T) {
	out := "Images|2.4GB\nContainers|10MB\nLocal Volumes|0B\nBuild Cache|1.2GB\n"
	if got := buildCacheSize(out); got != "1.2GB" {
		t.Errorf("buildCacheSize() = %q, want 1.2GB", got)
	}
	if got := buildCacheSize("Images|2.4GB\n"); got != "" {
		t.Errorf("buildCacheSize(no build cache) = %q, want empty", got)
	}
}

func TestPruneStepsNames(t *testing.T) {
	steps := pruneSteps()
	want := []string{"containers", "images", "networks", "volumes", "build-cache"}
	if len(steps) != len(want) {
		t.Fatalf("pruneSteps() has %d steps, want %d", len(steps), len(want))
	}
	for i, name := range want {
		if steps[i].name != name {
			t.Errorf("pruneSteps()[%d].name = %q, want %q", i, steps[i].name, name)
		}
	}
}
