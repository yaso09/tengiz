package cli

import (
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
}

func TestCleanupFlags(t *testing.T) {
	for _, flag := range []string{"all", "volumes", "df"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupFlagParsing(t *testing.T) {
	called := false
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		df, _ := cmd.Flags().GetBool("df")
		if !all {
			t.Error("all = false, want true")
		}
		if !volumes {
			t.Error("volumes = false, want true")
		}
		if df {
			t.Error("df = true, want false")
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--all", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}

func TestCleanupDfFlagParsing(t *testing.T) {
	called := false
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		df, _ := cmd.Flags().GetBool("df")
		if !df {
			t.Error("df = false, want true")
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--df"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}