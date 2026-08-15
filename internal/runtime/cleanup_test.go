package runtime

import (
	"context"
	"testing"
)

func TestPruneCommandsDefault(t *testing.T) {
	cmds := pruneCommands(PruneOptions{})
	if len(cmds) != 3 {
		t.Fatalf("expected 3 default commands (containers, images, build cache), got %d: %v", len(cmds), cmds)
	}
	// Containers: protected by label!=tengiz-app, no --all
	if got := cmds[0]; len(got) != 5 || got[0] != "container" || got[1] != "prune" || got[2] != "-f" || got[3] != "--filter" || got[4] != "label!=tengiz-app" {
		t.Errorf("containers command = %v, want [container prune -f --filter label!=tengiz-app]", got)
	}
	// Images: no --all
	if got := cmds[1]; len(got) != 3 || got[0] != "image" || got[1] != "prune" || got[2] != "-f" {
		t.Errorf("images command = %v, want [image prune -f]", got)
	}
	// Build cache: no --all
	if got := cmds[2]; len(got) != 3 || got[0] != "builder" || got[1] != "prune" || got[2] != "-f" {
		t.Errorf("build cache command = %v, want [builder prune -f]", got)
	}
}

func TestPruneCommandsAll(t *testing.T) {
	cmds := pruneCommands(PruneOptions{All: true})
	if len(cmds) != 3 {
		t.Fatalf("expected 3 commands with --all, got %d: %v", len(cmds), cmds)
	}
	// Containers with --all: NO label filter (allows Tengiz containers)
	if got := cmds[0]; len(got) != 3 || got[0] != "container" || got[1] != "prune" || got[2] != "-f" {
		t.Errorf("containers command = %v, want [container prune -f]", got)
	}
	// Images with --all
	if got := cmds[1]; len(got) != 4 || got[0] != "image" || got[1] != "prune" || got[2] != "-f" || got[3] != "--all" {
		t.Errorf("images command = %v, want [image prune -f --all]", got)
	}
	// Build cache with --all
	if got := cmds[2]; len(got) != 4 || got[0] != "builder" || got[1] != "prune" || got[2] != "-f" || got[3] != "--all" {
		t.Errorf("build cache command = %v, want [builder prune -f --all]", got)
	}
}

func TestPruneCommandsVolumes(t *testing.T) {
	cmds := pruneCommands(PruneOptions{Volumes: true})
	// Default 3 + volumes = 4
	if len(cmds) != 4 {
		t.Fatalf("expected 4 commands with --volumes, got %d: %v", len(cmds), cmds)
	}
	if got := cmds[3]; len(got) != 3 || got[0] != "volume" || got[1] != "prune" || got[2] != "-f" {
		t.Errorf("volumes command = %v, want [volume prune -f]", got)
	}
}

func TestPruneCommandsExplicitCategory(t *testing.T) {
	// Explicit Images=true only -> exactly one command, no default expansion
	cmds := pruneCommands(PruneOptions{Images: true})
	if len(cmds) != 1 {
		t.Fatalf("expected exactly 1 command when Images=true, got %d: %v", len(cmds), cmds)
	}
	if got := cmds[0]; got[0] != "image" {
		t.Errorf("expected image prune, got %v", got)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	out, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	if out != "" {
		t.Errorf("stub Prune() output = %q, want empty", out)
	}
}

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
