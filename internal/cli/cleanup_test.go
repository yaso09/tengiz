package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "networks", "build-cache", "volumes", "dry-run", "interval"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupIntervalFlagValue(t *testing.T) {
	cleanupCmd.ParseFlags([]string{"--interval", "30"})
	v, _ := cleanupCmd.Flags().GetInt("interval")
	if v != 30 {
		t.Errorf("interval = %d, want 30", v)
	}
}

func TestCleanupOptionsDefaultSafeSet(t *testing.T) {
	// Explicitly false everywhere → no category selected → the safe default set applies.
	cleanupCmd.ParseFlags([]string{
		"--containers=false", "--images=false", "--networks=false",
		"--build-cache=false", "--volumes=false", "--dry-run=false",
	})
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Errorf("defaults must enable containers/images/networks/build-cache, got %+v", opts)
	}
	if opts.Volumes {
		t.Errorf("volumes must not be pruned by default, got %+v", opts)
	}
	if opts.DryRun {
		t.Errorf("dry-run must default to false, got %+v", opts)
	}
}

func TestCleanupOptionsVolumeIsExplicit(t *testing.T) {
	cleanupCmd.ParseFlags([]string{
		"--volumes", "--containers=false", "--images=false",
		"--networks=false", "--build-cache=false", "--dry-run=false",
	})
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.Volumes {
		t.Fatalf("expected volumes enabled, got %+v", opts)
	}
	if opts.Containers || opts.Images || opts.Networks || opts.BuildCache {
		t.Errorf("explicit flags must not enable the default set, got %+v", opts)
	}
}

func TestCleanupOptionsDryRunKeepsDefaults(t *testing.T) {
	cleanupCmd.ParseFlags([]string{
		"--dry-run", "--containers=false", "--images=false",
		"--networks=false", "--build-cache=false", "--volumes=false",
	})
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.DryRun {
		t.Fatalf("expected dry-run enabled, got %+v", opts)
	}
	if !opts.Containers {
		t.Errorf("dry-run must still carry the default categories, got %+v", opts)
	}
}
