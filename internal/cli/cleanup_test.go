package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func resetCleanupFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run"} {
		if err := cleanupCmd.Flags().Set(name, "false"); err != nil {
			t.Fatalf("reset --%s: %v", name, err)
		}
	}
}

func TestCleanupRootRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run", "every"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupTargetsDefaultsNeitherFlag(t *testing.T) {
	resetCleanupFlags(t)
	opts := cleanupTargets(cleanupCmd)
	if opts.Containers || opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("default targets should all be false, got %+v", opts)
	}
}

func TestCleanupTargetsAllFlag(t *testing.T) {
	resetCleanupFlags(t)
	if err := cleanupCmd.Flags().Set("all", "true"); err != nil {
		t.Fatal(err)
	}
	opts := cleanupTargets(cleanupCmd)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("--all should enable every target, got %+v", opts)
	}
}

func TestCleanupWithoutTargetsReturnsError(t *testing.T) {
	resetCleanupFlags(t)
	rootCmd.SetArgs([]string{"cleanup"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for cleanup with no target flags")
	}
}

func TestExecuteCleanupDryRun(t *testing.T) {
	var buf bytes.Buffer
	opts := runtime.CleanupOptions{Containers: true, Volumes: true}
	err := executeCleanup(t.Context(), &buf, runtime.NewStub(), opts, true)
	if err != nil {
		t.Fatalf("executeCleanup(dry-run) error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "docker container prune -f --filter label=tengiz-deployment") {
		t.Errorf("dry-run output missing container command:\n%s", out)
	}
	if !strings.Contains(out, "docker volume prune -f") {
		t.Errorf("dry-run output missing volume command:\n%s", out)
	}
	if strings.Contains(out, "removed:") {
		t.Errorf("dry-run should not report removals:\n%s", out)
	}
}

func TestExecuteCleanupReportsResult(t *testing.T) {
	var buf bytes.Buffer
	opts := runtime.CleanupOptions{Images: true}
	err := executeCleanup(context.Background(), &buf, runtime.NewStub(), opts, false)
	if err != nil {
		t.Fatalf("executeCleanup() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "images removed:") {
		t.Errorf("result output missing images count:\n%s", out)
	}
}