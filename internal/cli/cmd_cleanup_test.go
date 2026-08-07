package cli

import (
	"testing"

	"github.com/spf13/cobra"
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
	for _, flag := range []string{"containers", "images", "volumes", "build-cache", "dry-run"} {
		if f := cleanupCmd.Flags().Lookup(flag); f == nil {
			t.Errorf("cleanupCmd missing flag --%s", flag)
		}
	}
}

func TestCleanupStubPath(t *testing.T) {
	if cleanupCmd == nil {
		t.Fatal("cleanupCmd must be defined")
	}
	var _ *cobra.Command = cleanupCmd
}
