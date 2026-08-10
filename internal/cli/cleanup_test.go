package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func newCleanupTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "cleanup"}
	c.Flags().Bool("containers", false, "")
	c.Flags().Bool("images", false, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("build-cache", false, "")
	c.Flags().Bool("all", false, "")
	c.Flags().Bool("dry-run", false, "")
	return c
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatalf("cleanup command not found: %v", cmd)
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupDefaultsToAll(t *testing.T) {
	opts := cleanupOptionsFromFlags(newCleanupTestCmd())
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories enabled by default, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("DryRun should default to false")
	}
}

func TestCleanupCategoryFlagsScope(t *testing.T) {
	c := newCleanupTestCmd()
	c.Flags().Set("containers", "true")
	c.Flags().Set("dry-run", "true")
	opts := cleanupOptionsFromFlags(c)
	if !opts.Containers || opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("expected containers-only, got %+v", opts)
	}
	if !opts.DryRun {
		t.Error("expected DryRun true")
	}
}

func TestCleanupAllFlag(t *testing.T) {
	c := newCleanupTestCmd()
	c.Flags().Set("images", "true")
	c.Flags().Set("all", "true")
	opts := cleanupOptionsFromFlags(c)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories with --all, got %+v", opts)
	}
}

func TestRunCleanupWithMock(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	opts := runtime.CleanupOptions{Containers: true, DryRun: true}
	err := runCleanup(ctx, &mockRTForDeploy{}, opts, &buf)
	if err != nil {
		t.Fatalf("runCleanup: %v", err)
	}
	if !strings.Contains(buf.String(), "prune candidates (dry-run) containers: 0") {
		t.Errorf("unexpected output: %q", buf.String())
	}
}

func TestWriteCleanupSummary(t *testing.T) {
	s := runtime.CleanupSummary{
		ContainersRemoved: []string{"old-helper"},
		ImagesRemoved:     []string{"sha256:abc"},
		VolumesRemoved:    []string{"junk-vol"},
		NetworksRemoved:   []string{"junk-net"},
		BuildCacheOutput:  "Total reclaimed space: 12MB",
	}
	var buf bytes.Buffer
	writeCleanupSummary(&buf, s, false)
	out := buf.String()
	for _, want := range []string{
		"[tengiz] removed containers: 1",
		"  - old-helper",
		"[tengiz] removed images: 1",
		"  - sha256:abc",
		"[tengiz] removed volumes: 1",
		"  - junk-vol",
		"[tengiz] removed networks: 1",
		"  - junk-net",
		"[tengiz] build cache: Total reclaimed space: 12MB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
