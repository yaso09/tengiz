package runtime

import (
	"context"
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
	report, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0", report.ContainersRemoved)
	}
	if report.ImagesRemoved != 0 {
		t.Errorf("ImagesRemoved = %d, want 0", report.ImagesRemoved)
	}
	if report.BuildCachePruned {
		t.Error("BuildCachePruned = true, want false")
	}
	if report.VolumesPruned {
		t.Error("VolumesPruned = true, want false")
	}
	if report.DryRun {
		t.Error("DryRun = true, want false")
	}
}

func TestHasLabel(t *testing.T) {
	tests := []struct {
		name   string
		labels string
		key    string
		want   bool
	}{
		{"tengiz app label present", "tengiz-app=myapp,tengiz-env=production", "tengiz-app", true},
		{"tengiz env only", "tengiz-env=production", "tengiz-app", false},
		{"unrelated labels", "maintainer=dev,role=web", "tengiz-app", false},
		{"empty", "", "tengiz-app", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasLabel(tt.labels, tt.key); got != tt.want {
				t.Errorf("hasLabel(%q, %q) = %v, want %v", tt.labels, tt.key, got, tt.want)
			}
		})
	}
}

func TestParseForeignContainers(t *testing.T) {
	output := "abc123|tengiz-app=myapp,tengiz-env=production\n" +
		"def456|maintainer=dev\n" +
		"ghi789|tengiz-app=other\n" +
		"jkl012|\n"
	ids := parseForeignContainers(output)
	want := []string{"def456", "jkl012"}
	if len(ids) != len(want) {
		t.Fatalf("parseForeignContainers() = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestParseForeignContainersEmpty(t *testing.T) {
	ids := parseForeignContainers("")
	if len(ids) != 0 {
		t.Fatalf("expected no IDs, got %v", ids)
	}
}

func TestParseImageIDs(t *testing.T) {
	output := "sha256:aaa\n\nsha256:bbb\nsha256:ccc\n"
	ids := parseImageIDs(output)
	want := []string{"sha256:aaa", "sha256:bbb", "sha256:ccc"}
	if len(ids) != len(want) {
		t.Fatalf("parseImageIDs() = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestStoppedContainerArgs(t *testing.T) {
	args := stoppedContainerArgs()
	joined := strings.Join(args, " ")
	for _, want := range []string{"ps", "-a", "status=exited", "status=created", "{{.ID}}|{{.Labels}}"} {
		if !strings.Contains(joined, want) {
			t.Errorf("stoppedContainerArgs() = %v, missing %q", args, want)
		}
	}
}

func TestDanglingImageArgs(t *testing.T) {
	args := danglingImageArgs()
	joined := strings.Join(args, " ")
	for _, want := range []string{"images", "dangling=true", "-q"} {
		if !strings.Contains(joined, want) {
			t.Errorf("danglingImageArgs() = %v, missing %q", args, want)
		}
	}
}

func TestPruneArgs(t *testing.T) {
	if got := strings.Join(buildCachePruneArgs(), " "); !strings.Contains(got, "builder") || !strings.Contains(got, "prune") {
		t.Errorf("buildCachePruneArgs() = %v", buildCachePruneArgs())
	}
	if got := strings.Join(volumePruneArgs(), " "); !strings.Contains(got, "volume") || !strings.Contains(got, "prune") {
		t.Errorf("volumePruneArgs() = %v", volumePruneArgs())
	}
}

func TestDockerRuntimeImplementsCleanup(t *testing.T) {
	var _ Manager = (*dockerRuntime)(nil)
}
