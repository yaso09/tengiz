package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/housekeeping"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cleanup, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil || cleanup == nil {
		t.Fatalf("expected cleanup command to be registered, err=%v", err)
	}
}

func TestMergeCleanupOptionsDefaultsAll(t *testing.T) {
	opts := mergeCleanupOptions(false, false, false, false, false)
	want := housekeeping.Options{Containers: true, Images: true, Volumes: true, Networks: true}
	if opts != want {
		t.Errorf("mergeCleanupOptions() = %+v, want %+v", opts, want)
	}
}

func TestMergeCleanupOptionsKeepsExplicit(t *testing.T) {
	opts := mergeCleanupOptions(true, false, false, true, true)
	if !opts.Containers || opts.Images || !opts.Networks || !opts.DryRun {
		t.Errorf("mergeCleanupOptions() = %+v", opts)
	}
	if opts.Volumes {
		t.Error("Volumes should stay false when not selected")
	}
}

func TestCleanupDryRunFlagPresent(t *testing.T) {
	cleanup, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup not found: %v", err)
	}
	if cleanup.Flags().Lookup("dry-run") == nil {
		t.Error("expected --dry-run flag on cleanup command")
	}
	if cleanup.Flags().Lookup("images") == nil {
		t.Error("expected --images flag on cleanup command")
	}
}
