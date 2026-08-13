package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	for _, name := range []string{"volumes", "keep", "dry-run"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup missing --%s flag", name)
		}
	}
}

func TestCleanupSummaryLines(t *testing.T) {
	report := &runtime.CleanupReport{
		ContainersRemoved: 2,
		ImagesRemoved:     5,
		NetworksRemoved:   1,
		VolumesRemoved:    0,
		ReclaimedSpace:    "1.55GB",
	}
	lines := cleanupSummaryLines(report)
	expected := []string{
		"[tengiz] containers removed: 2",
		"[tengiz] images removed: 5",
		"[tengiz] networks removed: 1",
		"[tengiz] volumes removed: 0",
		"[tengiz] reclaimed space: 1.55GB",
	}
	if len(lines) != len(expected) {
		t.Fatalf("cleanupSummaryLines() returned %d lines, want %d", len(lines), len(expected))
	}
	for i := range expected {
		if lines[i] != expected[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], expected[i])
		}
	}
}

func TestCleanupDryRun(t *testing.T) {
	tmpDir := t.TempDir()
	oldDataDir := dataDir
	dataDir = tmpDir
	defer func() { dataDir = oldDataDir }()

	store := config.NewStore(dataDir)
	if err := store.SaveApp(types.AppEntry{
		Name:   "testapp",
		Config: types.AppConfig{Name: "testapp"},
	}); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "testapp") {
		t.Errorf("dry-run output missing app name, got: %s", output)
	}
	if !strings.Contains(output, "dry-run") {
		t.Errorf("dry-run output missing marker, got: %s", output)
	}
}