package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/types"
)

func newCleanupTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "cleanup"}
	initCleanupFlags(c)
	return c
}

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, name := range []string{"all", "containers", "images", "volumes", "build-cache", "dry-run", "yes", "keep"} {
		if flag := cleanupCmd.Flags().Lookup(name); flag == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestResolveCleanupFlagsDefault(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.ParseFlags([]string{})
	opts, keep, err := resolveCleanupFlags(cmd)
	if err != nil {
		t.Fatalf("resolveCleanupFlags: %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.BuildCache {
		t.Errorf("default opts = %+v, want Containers/Images/BuildCache true", opts)
	}
	if opts.Volumes {
		t.Error("volumes should default to false")
	}
	if opts.DryRun {
		t.Error("dry-run should default to false")
	}
	if keep != 5 {
		t.Errorf("keep = %d, want 5", keep)
	}
}

func TestResolveCleanupFlagsAll(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.ParseFlags([]string{"--all"})
	opts, _, err := resolveCleanupFlags(cmd)
	if err != nil {
		t.Fatalf("resolveCleanupFlags: %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.BuildCache {
		t.Errorf("--all opts = %+v, want Containers/Images/BuildCache true", opts)
	}
	if opts.Volumes {
		t.Error("--all must not enable volumes")
	}
}

func TestResolveCleanupFlagsExplicitVolumesRequiresYes(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.ParseFlags([]string{"--volumes"})
	_, _, err := resolveCleanupFlags(cmd)
	if err == nil {
		t.Fatal("expected error for --volumes without --yes")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error should mention --yes, got: %v", err)
	}
}

func TestResolveCleanupFlagsVolumesWithYes(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.ParseFlags([]string{"--volumes", "--yes"})
	opts, _, err := resolveCleanupFlags(cmd)
	if err != nil {
		t.Fatalf("resolveCleanupFlags: %v", err)
	}
	if !opts.Volumes {
		t.Error("volumes should be enabled")
	}
}

func TestResolveCleanupFlagsVolumesDryRunNoConfirmNeeded(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.ParseFlags([]string{"--volumes", "--dry-run"})
	opts, _, err := resolveCleanupFlags(cmd)
	if err != nil {
		t.Fatalf("resolveCleanupFlags: %v", err)
	}
	if !opts.Volumes || !opts.DryRun {
		t.Errorf("opts = %+v, want Volumes and DryRun true", opts)
	}
}

func TestResolveCleanupFlagsKeep(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.ParseFlags([]string{"--images", "--keep", "10"})
	opts, keep, err := resolveCleanupFlags(cmd)
	if err != nil {
		t.Fatalf("resolveCleanupFlags: %v", err)
	}
	if !opts.Images {
		t.Error("images should be enabled")
	}
	if keep != 10 {
		t.Errorf("keep = %d, want 10", keep)
	}
}

func TestPrintCleanupReport(t *testing.T) {
	out := captureOutput(func() {
		printCleanupReport(types.CleanupReport{
			DryRun:            true,
			ContainersRemoved: 3,
			ImagesRemoved:     7,
		})
	})
	for _, want := range []string{"dry-run summary", "containers removed: 3", "images removed: 7"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}