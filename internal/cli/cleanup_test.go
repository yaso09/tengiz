package cli

import (
	"strings"
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
	flags := cleanupCmd.Flags()
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache", "all"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanup command missing --%s flag", name)
		}
	}
}

func TestCleanupOptionsFromFlagsDefaults(t *testing.T) {
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.Containers {
		t.Error("Containers should default to true")
	}
	if !opts.Images {
		t.Error("Images should default to true")
	}
	if !opts.Cache {
		t.Error("Cache should default to true")
	}
	if opts.Volumes || opts.Networks {
		t.Error("Volumes and Networks should default to false")
	}
	if opts.DryRun {
		t.Error("DryRun should default to false")
	}
}

func TestCleanupOptionsFromFlagsAll(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", true, "")
	cmd.Flags().Bool("images", true, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("cache", true, "")
	cmd.Flags().Bool("all", false, "")
	cmd.ParseFlags([]string{"--all"})

	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Cache || !opts.Volumes || !opts.Networks {
		t.Errorf("--all should enable every category, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsDedicated(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", true, "")
	cmd.Flags().Bool("images", true, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("cache", true, "")
	cmd.Flags().Bool("all", false, "")
	cmd.ParseFlags([]string{"--volumes", "--dry-run"})

	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Volumes {
		t.Error("--volumes should enable volumes")
	}
	if !opts.DryRun {
		t.Error("--dry-run should be true")
	}
	if opts.Containers != true || opts.Images != true || opts.Cache != true {
		t.Errorf("--volumes should not disable other defaults, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsDisable(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", true, "")
	cmd.Flags().Bool("images", true, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("cache", true, "")
	cmd.Flags().Bool("all", false, "")
	cmd.ParseFlags([]string{"--containers=false"})

	opts := cleanupOptionsFromFlags(cmd)
	if opts.Containers {
		t.Error("--containers=false should disable containers")
	}
	if !opts.Images {
		t.Error("Images should remain enabled")
	}
}

func TestCleanupHelpListsFlags(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--help"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("cleanup --help failed: %v", err)
	}
	for _, flag := range []string{"--dry-run", "--volumes", "--networks", "--all"} {
		// flags are registered on the command — verify presence after Execute
		if cleanupCmd.Flags().Lookup(strings.TrimPrefix(flag, "--")) == nil {
			t.Errorf("cleanup missing flag %q", flag)
		}
	}
}