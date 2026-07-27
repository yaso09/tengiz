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
	opts := PruneOptions{All: true}
	report, err := m.Prune(context.Background(), opts)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersDeleted != 0 {
		t.Errorf("ContainersDeleted = %d, want 0", report.ContainersDeleted)
	}
	if report.ImagesDeleted != 0 {
		t.Errorf("ImagesDeleted = %d, want 0", report.ImagesDeleted)
	}
	if report.SpaceReclaimed != "0B" {
		t.Errorf("SpaceReclaimed = %q, want %q", report.SpaceReclaimed, "0B")
	}
}

func TestStubPruneDryRun(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune(dry-run) error = %v", err)
	}
	if report.ContainersDeleted != 0 {
		t.Errorf("dry-run ContainersDeleted = %d, want 0", report.ContainersDeleted)
	}
}

func TestStubPrunePerCategory(t *testing.T) {
	m := NewStub()
	opts := PruneOptions{Containers: true, Images: true}
	report, err := m.Prune(context.Background(), opts)
	if err != nil {
		t.Fatalf("Prune(categories) error = %v", err)
	}
	if report.ContainersDeleted != 0 {
		t.Errorf("ContainersDeleted = %d, want 0", report.ContainersDeleted)
	}
}

func TestPruneDockerCommandsAll(t *testing.T) {
	commands := pruneDockerCommands(PruneOptions{All: true}, false)
	expected := []string{
		"container",
		"image",
		"volume",
		"network",
		"builder",
	}
	if len(commands) != len(expected) {
		t.Fatalf("got %d commands, want %d: %v", len(commands), len(expected), commands)
	}
	for i, cmd := range commands {
		if cmd[0] != expected[i] {
			t.Errorf("command[%d] resource = %q, want %q", i, cmd[0], expected[i])
		}
	}
}

func TestPruneDockerCommandsPerCategory(t *testing.T) {
	commands := pruneDockerCommands(PruneOptions{Containers: true, Volumes: true}, true)
	if len(commands) != 2 {
		t.Fatalf("got %d commands, want 2: %v", len(commands), commands)
	}
	if commands[0][0] != "container" {
		t.Errorf("first command resource = %q, want %q", commands[0][0], "container")
	}
	if commands[1][0] != "volume" {
		t.Errorf("second command resource = %q, want %q", commands[1][0], "volume")
	}
}
