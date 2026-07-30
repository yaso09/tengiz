package cli

import (
	"testing"
)

func TestCleanupUsesStoreConfig(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	env := getEnv(cmd)
	if env != "production" {
		t.Errorf("default env = %q, want %q", env, "production")
	}
}

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil {
		t.Fatal("cleanup command is nil")
	}
}

func TestCleanupAllFlag(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	cmd.ParseFlags([]string{"--all"})
	all, _ := cmd.Flags().GetBool("all")
	if !all {
		t.Error("--all flag should be true")
	}
}

func TestCleanupDryRunFlag(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	cmd.ParseFlags([]string{"--dry-run"})
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if !dryRun {
		t.Error("--dry-run flag should be true")
	}
}

func TestCleanupIndividualFlags(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	cmd.ParseFlags([]string{"--containers", "--images", "--volumes", "--networks", "--build-cache"})
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	if !containers {
		t.Error("--containers should be true")
	}
	if !images {
		t.Error("--images should be true")
	}
	if !volumes {
		t.Error("--volumes should be true")
	}
	if !networks {
		t.Error("--networks should be true")
	}
	if !buildCache {
		t.Error("--build-cache should be true")
	}
}
