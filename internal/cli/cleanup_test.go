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
		t.Error("cleanup command not registered on rootCmd")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	cmd := cleanupCmd
	flags := []string{"dry-run", "all", "containers", "images", "build-cache"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupCmdFlagDefaults(t *testing.T) {
	cmd := cleanupCmd
	if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
		t.Error("--dry-run should default to false")
	}
	if all, _ := cmd.Flags().GetBool("all"); all {
		t.Error("--all should default to false")
	}
	if containers, _ := cmd.Flags().GetBool("containers"); containers {
		t.Error("--containers should default to false")
	}
	if images, _ := cmd.Flags().GetBool("images"); images {
		t.Error("--images should default to false")
	}
	if buildCache, _ := cmd.Flags().GetBool("build-cache"); buildCache {
		t.Error("--build-cache should default to false")
	}
}
