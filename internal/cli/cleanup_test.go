package cli

import "testing"

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatalf("cleanup command not found")
	}
}

func TestCleanupFlagsExist(t *testing.T) {
	for _, f := range []string{"dry-run", "containers", "images", "networks", "volumes", "build-cache", "status"} {
		if cleanupCmd.Flags().Lookup(f) == nil {
			t.Errorf("cleanupCmd missing --%s flag", f)
		}
	}
}

func TestCleanupDefaultsToAllCategories(t *testing.T) {
	opts := cleanupOptions(false, false, false, false, false, false)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.Volumes || !opts.BuildCache {
		t.Errorf("expected all categories enabled, got %+v", opts)
	}
}

func TestCleanupRespectsSelectedCategories(t *testing.T) {
	opts := cleanupOptions(false, true, false, false, false, false)
	if !opts.Containers {
		t.Error("containers should be enabled")
	}
	if opts.Images || opts.Networks || opts.Volumes || opts.BuildCache {
		t.Errorf("only containers should be enabled, got %+v", opts)
	}
}

func TestCleanupDryRunPassthrough(t *testing.T) {
	opts := cleanupOptions(true, false, false, false, false, false)
	if !opts.DryRun {
		t.Error("dry-run should be enabled")
	}
	if !opts.Containers {
		t.Error("default categories should still be enabled in dry-run")
	}
}
