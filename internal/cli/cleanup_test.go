package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandHasFlags(t *testing.T) {
	expected := []string{"containers", "images", "volumes", "networks", "build-cache", "dry-run"}
	for _, name := range expected {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup command missing --%s flag", name)
		}
	}
}

func TestPruneOptionsDefaultsToAll(t *testing.T) {
	cmd := &cobra.Command{}
	addCleanupFlags(cmd)
	opts := pruneOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories default true, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("dry-run should default false")
	}
}

func TestPruneOptionsSelectedCategories(t *testing.T) {
	cmd := &cobra.Command{}
	addCleanupFlags(cmd)
	if err := cmd.Flags().Set("images", "true"); err != nil {
		t.Fatal(err)
	}
	opts := pruneOptionsFromFlags(cmd)
	if !opts.Images {
		t.Error("images should be enabled")
	}
	if opts.Containers || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("expected only images enabled, got %+v", opts)
	}
}

func TestPruneOptionsDryRun(t *testing.T) {
	cmd := &cobra.Command{}
	addCleanupFlags(cmd)
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	opts := pruneOptionsFromFlags(cmd)
	if !opts.DryRun {
		t.Error("dry-run should be true")
	}
}