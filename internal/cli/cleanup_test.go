package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd := findSubcommand(rootCmd, "cleanup")
	if cmd == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	cmd := findSubcommand(rootCmd, "cleanup")
	if cmd == nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd.Flags().Lookup("dry-run") == nil {
		t.Error("expected --dry-run flag")
	}
	if cmd.Flags().Lookup("all") == nil {
		t.Error("expected --all flag")
	}
}

func TestCleanupOptionsFromFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Set("dry-run", "true")
	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.DryRun {
		t.Error("expected DryRun true")
	}
	if opts.All {
		t.Error("expected All false")
	}
}

func TestFormatCleanupReport(t *testing.T) {
	report := runtime.CleanupReport{Containers: 3, Images: 5, Volumes: 2, Networks: 1}

	real := formatCleanupReport(report, false)
	if !strings.Contains(real, "removed") {
		t.Errorf("real report = %q, want 'removed'", real)
	}
	if !strings.Contains(real, "3 containers") || !strings.Contains(real, "5 images") {
		t.Errorf("real report = %q, want container/image counts", real)
	}

	dry := formatCleanupReport(report, true)
	if !strings.Contains(dry, "would remove") {
		t.Errorf("dry report = %q, want 'would remove'", dry)
	}
}

func TestRunCleanupCommandWithStub(t *testing.T) {
	out, err := runCleanupCommand(runtime.NewStub(), runtime.CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("runCleanupCommand() error = %v", err)
	}
	if !strings.Contains(out, "0 containers") {
		t.Errorf("output = %q, want zero counts", out)
	}
}
