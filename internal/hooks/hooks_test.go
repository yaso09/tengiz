package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSuccess(t *testing.T) {
	dir := t.TempDir()
	err := Run(context.Background(), dir, []string{"touch hook1", "touch hook2"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, f := range []string{"hook1", "hook2"} {
		if _, statErr := os.Stat(filepath.Join(dir, f)); statErr != nil {
			t.Errorf("expected %s to exist: %v", f, statErr)
		}
	}
}

func TestRunEmptyCommands(t *testing.T) {
	if err := Run(context.Background(), t.TempDir(), nil); err != nil {
		t.Fatalf("Run() with nil commands error = %v", err)
	}
	if err := Run(context.Background(), t.TempDir(), []string{}); err != nil {
		t.Fatalf("Run() with empty commands error = %v", err)
	}
}

func TestRunFailureAborts(t *testing.T) {
	dir := t.TempDir()
	err := Run(context.Background(), dir, []string{
		"touch before",
		"exit 3",
		"touch after",
	})
	if err == nil {
		t.Fatal("Run() expected error for failing command")
	}
	if !strings.Contains(err.Error(), "exit 3") {
		t.Errorf("error = %v, want mention of the failing command", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "before")); statErr != nil {
		t.Errorf("first command should have run: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "after")); statErr == nil {
		t.Error("command after failure should NOT have run")
	}
}

func TestRunWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := Run(context.Background(), dir, []string{"pwd > pwd.out"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "pwd.out"))
	if err != nil {
		t.Fatalf("read pwd.out: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != dir {
		t.Errorf("pwd = %q, want %q", got, dir)
	}
}

func TestRunContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, t.TempDir(), []string{"sleep 10"}); err == nil {
		t.Fatal("Run() expected error for cancelled context")
	}
}