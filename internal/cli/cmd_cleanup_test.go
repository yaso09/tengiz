package cli

import (
	"os/exec"
	"testing"
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

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"dry-run", "volumes", "interval"} {
		if f := cleanupCmd.Flags().Lookup(flag); f == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdDryRunDefaultFalse(t *testing.T) {
	dryRun, _ := cleanupCmd.Flags().GetBool("dry-run")
	if dryRun {
		t.Error("--dry-run should default to false")
	}
}

func TestCleanupCmdNoArgs(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed; skipping")
	}
	rootCmd.SetArgs([]string{"cleanup"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal("cleanup with no args should not error, got:", err)
	}
}
