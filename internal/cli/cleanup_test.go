package cli

import (
	"strings"
	"testing"
)

func TestCleanupDryRun(t *testing.T) {
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "[dry-run]") {
		t.Errorf("dry-run output missing '[dry-run]' prefix, got: %s", output)
	}
	if !strings.Contains(output, "No resources were removed") {
		t.Errorf("dry-run should state no resources removed, got: %s", output)
	}
}

func TestCleanupDryRunContainers(t *testing.T) {
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--containers"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "containers") {
		t.Errorf("dry-run with --containers should mention containers, got: %s", output)
	}
}

func TestCleanupHelp(t *testing.T) {
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--help"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "Remove unused Docker resources") {
		t.Errorf("help missing description, got: %s", output)
	}
}
