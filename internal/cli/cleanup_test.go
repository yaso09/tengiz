package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	for _, flag := range []string{"containers", "images", "networks", "volumes", "all", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func newCleanupTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cleanup"}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	return cmd
}

func TestCleanupOptionsFromFlagsDefault(t *testing.T) {
	cmd := newCleanupTestCmd()
	if err := cmd.ParseFlags([]string{}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Networks {
		t.Errorf("default opts = %+v, want containers+images+networks on", opts)
	}
	if opts.Volumes {
		t.Errorf("Volumes should default off, got %+v", opts)
	}
	if opts.DryRun {
		t.Errorf("DryRun should default off, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsAll(t *testing.T) {
	cmd := newCleanupTestCmd()
	if err := cmd.ParseFlags([]string{"--all", "--dry-run"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.Volumes {
		t.Errorf("--all should enable everything, got %+v", opts)
	}
	if !opts.DryRun {
		t.Errorf("DryRun should be set, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsVolumesOnly(t *testing.T) {
	cmd := newCleanupTestCmd()
	if err := cmd.ParseFlags([]string{"--volumes"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	opts := cleanupOptionsFromFlags(cmd)
	if opts.Containers || opts.Images || opts.Networks {
		t.Errorf("explicit --volumes should disable defaults, got %+v", opts)
	}
	if !opts.Volumes {
		t.Errorf("Volumes should be on, got %+v", opts)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1200, "1.20 kB"},
		{2_500_000, "2.50 MB"},
		{1_500_000_000, "1.50 GB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.in); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPrintCleanupReport(t *testing.T) {
	out := captureOutput(func() {
		printCleanupReport(&runtime.CleanupReport{
			ContainersRemoved: []string{"abc123def456"},
			ImagesRemoved:     2,
			NetworksRemoved:   1,
			BytesReclaimed:    2_500_000,
		})
	})
	for _, want := range []string{
		"containers removed: 1",
		"images removed: 2",
		"networks removed: 1",
		"space reclaimed: 2.50 MB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report output missing %q, got:\n%s", want, out)
		}
	}
}