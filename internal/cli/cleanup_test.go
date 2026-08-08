package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newCleanupTestCmd() *cobra.Command {
	c := &cobra.Command{}
	addCleanupFlags(c)
	return c
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupOptionsDefaultExcludesVolumes(t *testing.T) {
	c := newCleanupTestCmd()
	opts := cleanupOptions(c)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Errorf("default should enable containers/images/networks/build-cache, got %+v", opts)
	}
	if opts.Volumes {
		t.Error("default should NOT enable volumes")
	}
}

func TestCleanupOptionsAllIncludesVolumes(t *testing.T) {
	c := newCleanupTestCmd()
	c.Flags().Set("all", "true")
	opts := cleanupOptions(c)
	if !opts.Volumes {
		t.Error("--all should enable volumes")
	}
}

func TestCleanupOptionsVolumesOnly(t *testing.T) {
	c := newCleanupTestCmd()
	c.Flags().Set("volumes", "true")
	opts := cleanupOptions(c)
	if !opts.Volumes {
		t.Error("--volumes should enable volumes")
	}
	if opts.Containers || opts.Images || opts.Networks || opts.BuildCache {
		t.Errorf("only --volumes set, expected other categories off, got %+v", opts)
	}
}

func TestCleanupDryRunPrintsDockerCommands(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	out := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(out, "docker container prune -f --filter label!=tengiz-app") {
		t.Errorf("dry-run output missing container prune command, got:\n%s", out)
	}
	if strings.Contains(out, "volume prune") {
		t.Errorf("dry-run should not include volume prune by default, got:\n%s", out)
	}
}
