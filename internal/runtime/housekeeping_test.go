package runtime

import (
	"context"
	"testing"
)

func TestStubManagerCleanup(t *testing.T) {
	m := NewStub()
	rep, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if rep.ContainersRemoved != 0 || rep.ImagesRemoved != 0 ||
		rep.VolumesRemoved != 0 || rep.NetworksRemoved != 0 {
		t.Errorf("stub Cleanup should report zero removals, got %+v", rep)
	}
	if rep.Reclaimed != "" || rep.DryRun != "" {
		t.Errorf("stub Cleanup should return empty strings, got %+v", rep)
	}
}

func TestContainerPruneArgs(t *testing.T) {
	want := []string{
		"container", "prune", "--force",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
	got := containerPruneArgs()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

func TestImagePruneArgs(t *testing.T) {
	got := imagePruneArgs()
	if got[0] != "image" && got[0] != "images" {
		t.Errorf("first arg = %q, want image prune", got[0])
	}
	if len(got) != 3 || got[1] != "prune" || got[2] != "--force" {
		t.Errorf("imagePruneArgs should be [image prune --force], got %v", got)
	}
}

func TestCachePruneArgs(t *testing.T) {
	got := cachePruneArgs()
	if len(got) != 3 || got[0] != "builder" || got[1] != "prune" || got[2] != "--force" {
		t.Errorf("cachePruneArgs = %v, want [builder prune --force]", got)
	}
}

func TestFindReclaimed(t *testing.T) {
	out := "Untagged: tengiz-apps/myapp:production-123\nDeleted: sha256:abc\nTotal reclaimed space: 12.5kB\n"
	if got := findReclaimed(out); got != "Total reclaimed space: 12.5kB" {
		t.Errorf("findReclaimed = %q, want the reclaimed line", got)
	}
	if got := findReclaimed("nothing here\n"); got != "" {
		t.Errorf("findReclaimed should return empty when absent, got %q", got)
	}
}
