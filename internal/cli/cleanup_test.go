package cli

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	if flags.Lookup("dry-run") == nil {
		t.Error("--dry-run flag missing")
	}
	if flags.Lookup("yes") == nil {
		t.Error("--yes flag missing")
	}
	if flags.Lookup("keep") == nil {
		t.Error("--keep flag missing")
	}
}

func TestCleanupCmdRequiresDryRunOrYes(t *testing.T) {
	dataDir = t.TempDir()
	rootCmd.SetArgs([]string{"cleanup"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when neither --dry-run nor -y given")
	}
	if !strings.Contains(err.Error(), "dry-run") {
		t.Errorf("error should mention --dry-run, got: %v", err)
	}
}

func TestFormatCleanupSummary(t *testing.T) {
	got := formatCleanupSummary(false,
		[]string{"tengiz-myapp-90"},
		[]string{"sha256:abc"},
		[]string{"tengiz-apps/myapp:production-100"},
		"Build cache pruned (12.5MB)")
	for _, want := range []string{
		"Removed 1 stale container(s):",
		"tengiz-myapp-90",
		"Removed 1 dangling image(s)",
		"Removed 1 old image(s):",
		"tengiz-apps/myapp:production-100",
		"Build cache pruned (12.5MB)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatCleanupSummary() missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatCleanupSummaryDryRun(t *testing.T) {
	got := formatCleanupSummary(true,
		[]string{"tengiz-myapp-90"},
		nil,
		nil,
		"Build cache would be pruned.")
	if !strings.Contains(got, "Would remove 1 stale container(s):") {
		t.Errorf("dry-run summary should use 'Would remove', got:\n%s", got)
	}
	if !strings.Contains(got, "Build cache would be pruned.") {
		t.Errorf("dry-run summary missing build cache note, got:\n%s", got)
	}
}

func TestCleanupDryRunSmoke(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	dataDir = t.TempDir()
	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Errorf("cleanup --dry-run: %v", err)
		}
	})
	if !strings.Contains(output, "stale container(s):") {
		t.Errorf("dry run output missing summary, got: %s", output)
	}
}