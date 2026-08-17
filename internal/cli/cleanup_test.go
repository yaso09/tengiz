package cli

import (
	"strings"
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"all", "volumes", "build-cache", "dry-run"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupDryRunPrintsCommands(t *testing.T) {
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all", "--volumes"})
		rootCmd.Execute()
	})
	for _, want := range []string{
		"docker container prune -f --filter label!=tengiz-app",
		"docker image prune -f -a --filter reference!=tengiz-apps/*",
		"docker network prune -f",
		"docker volume prune -f",
		"docker builder prune -f -a",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("dry-run output missing %q, got:\n%s", want, output)
		}
	}
}