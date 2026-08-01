package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil {
		t.Fatal("cleanup command is nil")
	}
	expectedFlags := []string{"dry-run", "all", "containers", "images", "cache", "volumes"}
	for _, flag := range expectedFlags {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func TestFormatCleanupReport(t *testing.T) {
	report := runtime.CleanupReport{
		ContainersRemoved: 2,
		ImagesRemoved:     3,
		BuildCachePruned:  true,
		VolumesPruned:     true,
		DryRun:            false,
	}
	out := formatCleanupReport(report)
	for _, want := range []string{"cleanup complete", "containers removed:  2", "images removed:      3", "build cache pruned:  true", "volumes pruned:      true"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatCleanupReport() = %q, missing %q", out, want)
		}
	}
}

func TestFormatCleanupReportDryRun(t *testing.T) {
	report := runtime.CleanupReport{DryRun: true}
	out := formatCleanupReport(report)
	if !strings.Contains(out, "dry-run") {
		t.Errorf("formatCleanupReport() = %q, expected dry-run marker", out)
	}
}
