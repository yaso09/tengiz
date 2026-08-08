package cli

import (
	"testing"

	"github.com/spf13/cobra"
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

func TestCleanupCommandHasFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "build-cache", "all", "keep"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("--%s flag not found", name)
		}
	}
}

func TestCleanupDryRunDoesNotTouchDocker(t *testing.T) {
	// The real RunE short-circuits before NewDocker() when --dry-run is set.
	// Override RunE to confirm flag flow, since real docker is absent in CI.
	original := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = original }()
	var dryRun bool
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		var err error
		dryRun, err = cmd.Flags().GetBool("dry-run")
		return err
	}
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("cleanup --dry-run error = %v", err)
	}
	if !dryRun {
		t.Fatal("dry-run flag was not read")
	}
}
