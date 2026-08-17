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

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	result, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.Output != "" {
		t.Errorf("Cleanup() Output = %q, want empty", result.Output)
	}
}

func TestCleanupCommands(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want int
	}{
		{"all", CleanupOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}, 5},
		{"containers only", CleanupOptions{Containers: true}, 1},
		{"images only", CleanupOptions{Images: true}, 1},
		{"volumes only", CleanupOptions{Volumes: true}, 1},
		{"networks only", CleanupOptions{Networks: true}, 1},
		{"build cache only", CleanupOptions{BuildCache: true}, 1},
		{"none", CleanupOptions{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanupCommands(tt.opts)
			if len(got) != tt.want {
				t.Fatalf("cleanupCommands() = %d commands, want %d", len(got), tt.want)
			}
		})
	}
}

func TestCleanupCommandsProtectTengiz(t *testing.T) {
	cmds := cleanupCommands(CleanupOptions{Containers: true, Volumes: true, Networks: true})
	if len(cmds) != 3 {
		t.Fatalf("cleanupCommands() = %d commands, want 3", len(cmds))
	}
	for _, c := range cmds {
		found := false
		for _, arg := range c {
			if arg == "label!=tengiz-app" {
				found = true
			}
		}
		if !found {
			t.Errorf("command %v missing label!=tengiz-app protection", c)
		}
	}
}

func TestCleanupCommandsImagePruneDanglingOnly(t *testing.T) {
	cmds := cleanupCommands(CleanupOptions{Images: true})
	if len(cmds) != 1 {
		t.Fatalf("cleanupCommands() = %d commands, want 1", len(cmds))
	}
	cmd := cmds[0]
	if cmd[0] != "image" || cmd[1] != "prune" {
		t.Errorf("expected image prune command, got %v", cmd)
	}
	for _, arg := range cmd {
		if arg == "-a" || arg == "--all" {
			t.Errorf("image prune must NOT use -a (would delete tagged rollback images), got %v", cmd)
		}
	}
}
