package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	for _, name := range []string{"dry-run", "all", "volumes"} {
		if flag := cleanupCmd.Flags().Lookup(name); flag == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestRunCleanupPrintsResult(t *testing.T) {
	m := &mockRTForDeploy{}
	out, err := runCleanup(m, runtime.PruneOptions{})
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if !strings.Contains(out, "[tengiz] cleanup complete") {
		t.Errorf("output missing success header, got: %s", out)
	}
	if !strings.Contains(out, "Total reclaimed space") {
		t.Errorf("output missing docker prune result, got: %s", out)
	}
	if m.pruned.Load() != 1 {
		t.Errorf("expected Prune called once, got %d", m.pruned.Load())
	}
}

func TestRunCleanupDryRun(t *testing.T) {
	m := &mockRTForDeploy{}
	out, err := runCleanup(m, runtime.PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if !strings.Contains(out, "[tengiz] dry run") {
		t.Errorf("output missing dry-run header, got: %s", out)
	}
	if !m.pruneOpts.DryRun {
		t.Error("expected Prune called with DryRun=true")
	}
}

func TestRunCleanupAllAndVolumes(t *testing.T) {
	m := &mockRTForDeploy{}
	if _, err := runCleanup(m, runtime.PruneOptions{All: true, Volumes: true}); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if !m.pruneOpts.All || !m.pruneOpts.Volumes {
		t.Errorf("expected Prune called with All=true Volumes=true, got %+v", m.pruneOpts)
	}
}

func TestRunCleanupPropagatesError(t *testing.T) {
	m := &mockRTForDeploy{pruneErr: errors.New("docker unavailable")}
	if _, err := runCleanup(m, runtime.PruneOptions{}); err == nil {
		t.Error("expected error from runCleanup")
	}
}
