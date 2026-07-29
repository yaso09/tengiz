package cli

import (
	"testing"
)

func TestCleanupAllDryRun(t *testing.T) {
	flag := cleanupCmd.Flags().Lookup("all")
	if flag == nil {
		t.Fatal("cleanupCmd missing --all flag")
	}
	all, _ := cleanupCmd.Flags().GetBool("all")
	if !all {
		t.Error("--all should default to true")
	}
}

func TestCleanupHasFlags(t *testing.T) {
	required := []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run"}
	for _, name := range required {
		flag := cleanupCmd.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupDryRunFlag(t *testing.T) {
	flag := cleanupCmd.Flags().Lookup("dry-run")
	if flag == nil {
		t.Fatal("cleanupCmd missing --dry-run flag")
	}
	dryRun, _ := cleanupCmd.Flags().GetBool("dry-run")
	if dryRun {
		t.Error("--dry-run should default to false")
	}
}
