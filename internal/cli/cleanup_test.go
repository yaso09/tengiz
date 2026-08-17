package cli

import (
	"strings"
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

func TestCleanupCommandFlags(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	for _, flag := range []string{"all", "volumes", "yes"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
	if cmd.Flags().Lookup("all").Shorthand != "a" {
		t.Errorf("--all should have shorthand -a")
	}
	if cmd.Flags().Lookup("yes").Shorthand != "y" {
		t.Errorf("--yes should have shorthand -y")
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()

	var gotAll, gotVolumes, gotYes bool
	var called bool
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		gotAll, _ = cmd.Flags().GetBool("all")
		gotVolumes, _ = cmd.Flags().GetBool("volumes")
		gotYes, _ = cmd.Flags().GetBool("yes")
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--all", "--volumes", "--yes"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
	if !gotAll || !gotVolumes || !gotYes {
		t.Errorf("flags not parsed: all=%v volumes=%v yes=%v", gotAll, gotVolumes, gotYes)
	}
}

func TestCleanupCmdNoArgs(t *testing.T) {
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()

	var args []string
	cleanupCmd.RunE = func(cmd *cobra.Command, a []string) error {
		args = a
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(args) != 0 {
		t.Errorf("cleanup should reject positional args, got %v", args)
	}
	rootCmd.SetArgs([]string{"cleanup", "extra"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected unknown-command error for positional arg, got %v", err)
	}
}
