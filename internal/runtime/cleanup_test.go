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

func assertCleanupCommands(t *testing.T, got, want []CleanupCommand) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d commands, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if len(got[i].Args) != len(want[i].Args) {
			t.Fatalf("cmd[%d] args = %v, want %v", i, got[i].Args, want[i].Args)
		}
		for j := range want[i].Args {
			if got[i].Args[j] != want[i].Args[j] {
				t.Fatalf("cmd[%d].Args[%d] = %q, want %q", i, j, got[i].Args[j], want[i].Args[j])
			}
		}
	}
}

func TestBuildCleanupCommandsDefault(t *testing.T) {
	cmds := BuildCleanupCommands(CleanupOptions{})
	assertCleanupCommands(t, cmds, []CleanupCommand{
		{Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{Args: []string{"image", "prune", "-f"}},
		{Args: []string{"network", "prune", "-f"}},
	})
}

func TestBuildCleanupCommandsAll(t *testing.T) {
	cmds := BuildCleanupCommands(CleanupOptions{All: true})
	assertCleanupCommands(t, cmds, []CleanupCommand{
		{Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{Args: []string{"image", "prune", "-f", "-a", "--filter", "reference!=tengiz-apps/*"}},
		{Args: []string{"network", "prune", "-f"}},
		{Args: []string{"builder", "prune", "-f", "-a"}},
	})
}

func TestBuildCleanupCommandsVolumes(t *testing.T) {
	cmds := BuildCleanupCommands(CleanupOptions{Volumes: true})
	assertCleanupCommands(t, cmds, []CleanupCommand{
		{Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{Args: []string{"image", "prune", "-f"}},
		{Args: []string{"network", "prune", "-f"}},
		{Args: []string{"volume", "prune", "-f"}},
	})
}

func TestBuildCleanupCommandsBuildCache(t *testing.T) {
	cmds := BuildCleanupCommands(CleanupOptions{BuildCache: true})
	assertCleanupCommands(t, cmds, []CleanupCommand{
		{Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{Args: []string{"image", "prune", "-f"}},
		{Args: []string{"network", "prune", "-f"}},
		{Args: []string{"builder", "prune", "-f"}},
	})
}

func TestBuildCleanupCommandsAllVolumesBuildCache(t *testing.T) {
	cmds := BuildCleanupCommands(CleanupOptions{All: true, Volumes: true, BuildCache: true})
	assertCleanupCommands(t, cmds, []CleanupCommand{
		{Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{Args: []string{"image", "prune", "-f", "-a", "--filter", "reference!=tengiz-apps/*"}},
		{Args: []string{"network", "prune", "-f"}},
		{Args: []string{"volume", "prune", "-f"}},
		{Args: []string{"builder", "prune", "-f", "-a"}},
	})
}
