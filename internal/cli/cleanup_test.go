package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"dry-run", "all", "containers", "images", "volumes", "networks"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	var called bool
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		if !dryRun || !all || !containers {
			t.Errorf("flags not parsed: dry-run=%v all=%v containers=%v", dryRun, all, containers)
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all", "--containers"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}

func TestCleanupCmdNoCategoryError(t *testing.T) {
	for _, flag := range []string{"dry-run", "all", "containers", "images", "volumes", "networks"} {
		cleanupCmd.Flags().Set(flag, "false")
	}
	rootCmd.SetArgs([]string{"cleanup"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no cleanup category selected")
	}
	if !strings.Contains(err.Error(), "no cleanup category selected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFormatList(t *testing.T) {
	if got := formatList(nil); got != "none" {
		t.Errorf("formatList(nil) = %q, want \"none\"", got)
	}
	if got := formatList([]string{}); got != "none" {
		t.Errorf("formatList([]) = %q, want \"none\"", got)
	}
	if got := formatList([]string{"a", "b"}); got != "a, b" {
		t.Errorf("formatList([a b]) = %q, want \"a, b\"", got)
	}
}
