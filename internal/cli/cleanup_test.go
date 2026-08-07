package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not registered")
	}
}

func TestCleanupDefaultResolver(t *testing.T) {
	c := &cobra.Command{}
	c.Flags().Bool("containers", false, "")
	c.Flags().Bool("images", false, "")
	c.Flags().Bool("all-images", false, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("build-cache", false, "")
	c.Flags().Bool("dry-run", false, "")
	opts := resolveCleanupOpts(c)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Fatalf("default opts must prune containers/images/networks/buildcache, got %+v", opts)
	}
	if opts.Volumes || opts.AllImages || opts.DryRun {
		t.Fatalf("defaults must not prune volumes/all-images/dry-run, got %+v", opts)
	}
}

func TestCleanupExplicitResolver(t *testing.T) {
	c := &cobra.Command{}
	c.Flags().Bool("containers", false, "")
	c.Flags().Bool("images", false, "")
	c.Flags().Bool("all-images", false, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("build-cache", false, "")
	c.Flags().Bool("dry-run", false, "")
	if err := c.Flags().Set("volumes", "true"); err != nil {
		t.Fatal(err)
	}
	if err := c.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	opts := resolveCleanupOpts(c)
	if !opts.Volumes || !opts.DryRun {
		t.Fatalf("expected volumes and dry-run set, got %+v", opts)
	}
	if opts.Containers || opts.Images || opts.Networks || opts.BuildCache {
		t.Fatalf("explicit flags must not default-on, got %+v", opts)
	}
}
