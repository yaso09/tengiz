package cli

import (
	"testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "cleanup" {
			found = true
			break
		}
	}
	if !found {
		t.Error("cleanup command not registered on root")
	}
}

func TestCleanupCmdHasVolumesFlag(t *testing.T) {
	if cleanupCmd == nil {
		t.Skip("cleanupCmd not defined")
	}
	if cleanupCmd.Flags().Lookup("volumes") == nil {
		t.Error("cleanup command missing --volumes flag")
	}
}

func TestCleanupCmdRejectsArgs(t *testing.T) {
	if cleanupCmd == nil {
		t.Skip("cleanupCmd not defined")
	}
	if err := cleanupCmd.Args(cleanupCmd, []string{"myapp"}); err == nil {
		t.Error("expected error when cleanup is called with arguments")
	}
}
