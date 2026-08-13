package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cleanup := findSubcommand(rootCmd, "cleanup")
	if cleanup == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupFlagsDefaults(t *testing.T) {
	cleanup := findSubcommand(rootCmd, "cleanup")
	if cleanup == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
	var cmd *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "cleanup" {
			cmd = c
			break
		}
	}
	all, _ := cmd.Flags().GetBool("all")
	if all {
		t.Errorf("--all default = true, want false")
	}
	volumes, _ := cmd.Flags().GetBool("volumes")
	if volumes {
		t.Errorf("--volumes default = true, want false")
	}
	keep, _ := cmd.Flags().GetInt("keep-images")
	if keep != 0 {
		t.Errorf("--keep-images default = %d, want 0", keep)
	}
	app, _ := cmd.Flags().GetString("app")
	if app != "" {
		t.Errorf("--app default = %q, want empty", app)
	}
	env, _ := cmd.Flags().GetString("env")
	if env != "production" {
		t.Errorf("--env default = %q, want production", env)
	}
}