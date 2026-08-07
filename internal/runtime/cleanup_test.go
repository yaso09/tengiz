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

func TestCleanupCommandsDefaultsNone(t *testing.T) {
	cmds := cleanupCommands(CleanupOptions{})
	if len(cmds) != 0 {
		t.Fatalf("expected no commands for empty options, got %d", len(cmds))
	}
}

func TestCleanupCommandsOrder(t *testing.T) {
	opts := CleanupOptions{Containers: true, Images: true, BuildCache: true, Volumes: true, Networks: true}
	cmds := cleanupCommands(opts)
	want := []string{
		"container", "image", "builder", "volume", "network",
	}
	if len(cmds) != len(want) {
		t.Fatalf("expected %d commands, got %d: %v", len(want), len(cmds), cmds)
	}
	for i, first := range want {
		if cmds[i][0] != first {
			t.Errorf("command %d = %q, want %q", i, cmds[i][0], first)
		}
	}
}

func TestCleanupCommandsProtectsTengizContainers(t *testing.T) {
	var containerCmd []string
	for _, cmds := range cleanupCommands(CleanupOptions{Containers: true}) {
		if cmds[0] == "container" {
			containerCmd = cmds
		}
	}
	if containerCmd == nil {
		t.Fatal("container prune command not generated")
	}
	found := false
	for _, a := range containerCmd {
		if a == "label!=tengiz-app" {
			found = true
		}
	}
	if !found {
		t.Fatal("container prune command missing label!=tengiz-app protection")
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	out := "Deleted Containers:\n...\nTotal reclaimed space: 247.4MB\n"
	got := parseReclaimed(out)
	if got != "Total reclaimed space: 247.4MB" {
		t.Fatalf("parseReclaimed() = %q, want %q", got, "Total reclaimed space: 247.4MB")
	}
}

func TestParseReclaimedEmpty(t *testing.T) {
	if got := parseReclaimed("nothing here\n"); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}
