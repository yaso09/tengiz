package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "volumes"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	var dryRun, volumes bool
	original := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = original }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		dryRun, _ = cmd.Flags().GetBool("dry-run")
		volumes, _ = cmd.Flags().GetBool("volumes")
		return nil
	}
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !dryRun {
		t.Error("--dry-run flag was not parsed as true")
	}
	if !volumes {
		t.Error("--volumes flag was not parsed as true")
	}
}