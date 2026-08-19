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
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsExist(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, f := range []string{"containers", "images", "networks", "volumes", "build-cache", "all", "dry-run"} {
		if flags.Lookup(f) == nil {
			t.Errorf("cleanup missing --%s flag", f)
		}
	}
}

func newCleanupTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cleanup"}
	cmd.Flags().Bool("containers", true, "")
	cmd.Flags().Bool("images", true, "")
	cmd.Flags().Bool("networks", true, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("build-cache", true, "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	return cmd
}

func TestCleanupOptionsFromFlagsDefaults(t *testing.T) {
	opts := cleanupOptionsFromFlags(newCleanupTestCmd())
	want := runtime.PruneOptions{Containers: true, Images: true, Networks: true, BuildCache: true}
	if opts != want {
		t.Errorf("cleanupOptionsFromFlags() = %+v, want %+v", opts, want)
	}
}

func TestCleanupOptionsFromFlagsOverrides(t *testing.T) {
	cmd := newCleanupTestCmd()
	if err := cmd.ParseFlags([]string{
		"--all", "--volumes", "--dry-run",
		"--containers=false", "--images=false", "--networks=false", "--build-cache=false",
	}); err != nil {
		t.Fatal(err)
	}
	opts := cleanupOptionsFromFlags(cmd)
	want := runtime.PruneOptions{
		Containers: false,
		Images:     false,
		Networks:   false,
		Volumes:    true,
		BuildCache: false,
		All:        true,
		DryRun:     true,
	}
	if opts != want {
		t.Errorf("cleanupOptionsFromFlags() = %+v, want %+v", opts, want)
	}
}

func TestFormatPruneSummaryDryRun(t *testing.T) {
	s := runtime.PruneSummary{
		DryRun:         true,
		Containers:     []string{"web-test", "temp-build"},
		Images:         []string{"redis:7"},
		Networks:       []string{"mynet"},
		Volumes:        []string{"old-vol"},
		BuildCacheSize: 1200000000,
	}
	got := formatPruneSummary(s)
	for _, want := range []string{
		"[tengiz] cleanup dry-run - no changes made",
		"containers: 2 would be removed",
		"web-test, temp-build",
		"images:     1 would be removed",
		"redis:7",
		"networks:   1 would be removed",
		"volumes:    1 would be removed",
		"old-vol",
		"build cache: 1.20GB would be cleared",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatPruneSummary() missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatPruneSummaryReal(t *testing.T) {
	s := runtime.PruneSummary{
		Containers:     []string{"a", "b"},
		Images:         []string{"c"},
		ReclaimedBytes: 1500000,
	}
	got := formatPruneSummary(s)
	for _, want := range []string{
		"[tengiz] cleanup complete",
		"containers removed: 2",
		"images removed:     1",
		"networks removed:   0",
		"volumes removed:    0",
		"total reclaimed:    1.50MB",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatPruneSummary() missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{1500, "1.5kB"},
		{1500000, "1.50MB"},
		{1787000000, "1.79GB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}