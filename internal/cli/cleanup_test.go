package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func newTestCleanupCmd() *cobra.Command {
	c := &cobra.Command{Use: "cleanup"}
	registerCleanupFlags(c)
	return c
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	for _, flag := range []string{"dry-run", "containers", "images", "all-images", "volumes", "networks", "build-cache"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupOptionsFromFlagsDefaultAll(t *testing.T) {
	c := newTestCleanupCmd()
	c.ParseFlags([]string{})
	opts, err := cleanupOptionsFromFlags(c)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories when no flags given, got %+v", opts)
	}
	if opts.AllImages {
		t.Error("AllImages should be false by default")
	}
	if opts.DryRun {
		t.Error("DryRun should be false by default")
	}
}

func TestCleanupOptionsFromFlagsSingleCategory(t *testing.T) {
	c := newTestCleanupCmd()
	c.ParseFlags([]string{"--volumes", "--dry-run"})
	opts, err := cleanupOptionsFromFlags(c)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.Volumes {
		t.Error("Volumes should be true")
	}
	if !opts.DryRun {
		t.Error("DryRun should be true")
	}
	if opts.Containers || opts.Images || opts.Networks || opts.BuildCache {
		t.Errorf("expected only Volumes enabled, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsAllImagesRequiresImages(t *testing.T) {
	c := newTestCleanupCmd()
	c.ParseFlags([]string{"--all-images"})
	if _, err := cleanupOptionsFromFlags(c); err == nil {
		t.Error("expected error when --all-images used without --images")
	}
}

func TestCleanupOptionsFromFlagsImagesAndAllImages(t *testing.T) {
	c := newTestCleanupCmd()
	c.ParseFlags([]string{"--images", "--all-images"})
	opts, err := cleanupOptionsFromFlags(c)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.Images || !opts.AllImages {
		t.Errorf("expected Images and AllImages true, got %+v", opts)
	}
	if opts.Containers {
		t.Error("Containers should stay false")
	}
}
