package cli

import (
	"testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
	if cleanupCmd == nil {
		t.Fatal("cleanupCmd is nil")
	}
	if cleanupCmd.Use != "cleanup" {
		t.Errorf("cleanupCmd.Use = %q, want %q", cleanupCmd.Use, "cleanup")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	flags := []string{"dry-run", "containers", "images", "volumes", "build-cache", "all", "keep"}
	for _, name := range flags {
		t.Run(name, func(t *testing.T) {
			f := cleanupCmd.Flags().Lookup(name)
			if f == nil {
				t.Errorf("cleanupCmd missing --%s flag", name)
			}
		})
	}
}

func TestCleanupCmdDryRunDefault(t *testing.T) {
	cleanupCmd.ParseFlags([]string{})
	dryRun, _ := cleanupCmd.Flags().GetBool("dry-run")
	if dryRun {
		t.Error("--dry-run should default to false")
	}
}

func TestCleanupCmdKeepDefault(t *testing.T) {
	cleanupCmd.ParseFlags([]string{})
	keep, _ := cleanupCmd.Flags().GetInt("keep")
	if keep != 5 {
		t.Errorf("--keep should default to 5, got %d", keep)
	}
}
