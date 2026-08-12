package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

type mockCleanupManager struct {
	opts cleanup.Options
	rep  cleanup.Report
}

func (m *mockCleanupManager) Prune(ctx context.Context, opts cleanup.Options) (cleanup.Report, error) {
	m.opts = opts
	return m.rep, nil
}

func newTestCleanupCmd(mgr cleanup.Manager) *cobra.Command {
	c := &cobra.Command{Use: "cleanup"}
	addCleanupFlags(c)
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return runCleanup(cmd, mgr)
	}
	return c
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"all", "containers", "images", "volumes", "networks", "builder-cache", "dry-run", "keep-last"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func TestRunCleanupDefaultsToContainersAndImages(t *testing.T) {
	dataDir = t.TempDir()
	m := &mockCleanupManager{rep: cleanup.Report{}}
	c := newTestCleanupCmd(m)
	out := captureOutput(func() {
		c.SetArgs(nil)
		if err := c.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !m.opts.Containers {
		t.Error("expected Containers=true by default")
	}
	if !m.opts.Images {
		t.Error("expected Images=true by default")
	}
	if m.opts.Volumes || m.opts.Networks || m.opts.BuildCache {
		t.Error("expected volumes/networks/cache to be false by default")
	}
	if !strings.Contains(out, "nothing to clean") {
		t.Errorf("expected 'nothing to clean' in output, got: %q", out)
	}
}

func TestRunCleanupCategoryFlags(t *testing.T) {
	m := &mockCleanupManager{rep: cleanup.Report{}}
	c := newTestCleanupCmd(m)
	c.SetArgs([]string{"--volumes", "--networks"})
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if m.opts.Containers || m.opts.Images {
		t.Error("expected containers/images to stay false when explicit flags are given")
	}
	if !m.opts.Volumes || !m.opts.Networks {
		t.Error("expected volumes/networks to be true")
	}
}

func TestRunCleanupAllAndDryRun(t *testing.T) {
	m := &mockCleanupManager{rep: cleanup.Report{DryRun: true}}
	c := newTestCleanupCmd(m)
	c.SetArgs([]string{"--all", "--dry-run"})
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !m.opts.All {
		t.Error("expected All=true")
	}
	if !m.opts.DryRun {
		t.Error("expected DryRun=true")
	}
}

func TestRunCleanupPrintsSummary(t *testing.T) {
	m := &mockCleanupManager{
		rep: cleanup.Report{
			Containers: []string{"old-helper"},
			Images:     []string{"tengiz-apps/demo:production-123"},
		},
	}
	c := newTestCleanupCmd(m)
	out := captureOutput(func() {
		c.SetArgs(nil)
		if err := c.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(out, "removed 2 items") {
		t.Errorf("summary line missing, got: %q", out)
	}
	if !strings.Contains(out, "containers: old-helper") {
		t.Errorf("container line missing, got: %q", out)
	}
	if !strings.Contains(out, "images: tengiz-apps/demo:production-123") {
		t.Errorf("image line missing, got: %q", out)
	}
}
