package cli

import (
	"testing"
)

func TestCleanupOptionsForFlags(t *testing.T) {
	all := cleanupOptionsForFlags(false, false, false, false, false)
	if !all.Containers || !all.Images || !all.Volumes || !all.Networks {
		t.Errorf("no flags set should clean all categories, got %+v", all)
	}

	imagesOnly := cleanupOptionsForFlags(false, true, false, false, false)
	if !imagesOnly.Images || imagesOnly.Containers || imagesOnly.Volumes || imagesOnly.Networks {
		t.Errorf("only --images should clean images, got %+v", imagesOnly)
	}

	dry := cleanupOptionsForFlags(false, false, false, false, true)
	if !dry.DryRun {
		t.Error("dry-run flag should pass through to options")
	}
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"containers", "images", "volumes", "networks", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}
