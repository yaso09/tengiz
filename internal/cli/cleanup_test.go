package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlagsRegistered(t *testing.T) {
	for _, name := range []string{"containers", "images", "volumes", "networks", "dry-run", "retain-images"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	called := false
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		called = true
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		retain, _ := cmd.Flags().GetInt("retain-images")
		if containers != true {
			t.Errorf("containers = %v, want true", containers)
		}
		if images != false {
			t.Errorf("images = %v, want false", images)
		}
		if volumes != true {
			t.Errorf("volumes = %v, want true", volumes)
		}
		if networks != false {
			t.Errorf("networks = %v, want false", networks)
		}
		if dryRun != true {
			t.Errorf("dry-run = %v, want true", dryRun)
		}
		if retain != 2 {
			t.Errorf("retain-images = %d, want 2", retain)
		}
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--containers", "--volumes", "--dry-run", "--retain-images", "2"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}

func TestFormatCleanupResult(t *testing.T) {
	r := &cleanup.Result{
		ContainersRemoved: 2,
		ImagesRemoved:     3,
		VolumesRemoved:    1,
		NetworksRemoved:   0,
		RetentionApps:     []string{"alpha", "beta"},
		ReclaimedBytes:    1500,
	}
	out := formatCleanupResult(r)
	for _, want := range []string{
		"cleanup complete",
		"containers removed: 2",
		"images removed: 3",
		"volumes removed: 1",
		"networks removed: 0",
		"image retention applied to: alpha, beta",
		"1.5 KB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestFormatCleanupResultDryRun(t *testing.T) {
	r := &cleanup.Result{DryRun: true, ReclaimedBytes: 5000}
	out := formatCleanupResult(r)
	for _, want := range []string{"dry run", "would reclaim approximately 4.9 KB"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in       int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1500, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tc := range tests {
		if got := humanBytes(tc.in); got != tc.expected {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.expected)
		}
	}
}