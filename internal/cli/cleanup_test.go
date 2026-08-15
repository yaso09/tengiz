package cli

import (
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

func TestCleanupFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "all-images", "volumes"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupCmdForwardsOptions(t *testing.T) {
	var gotOpts runtime.CleanupOptions
	var gotDryRun bool
	original := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = original }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		gotDryRun, _ = cmd.Flags().GetBool("dry-run")
		allImages, _ := cmd.Flags().GetBool("all-images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		gotOpts = runtime.CleanupOptions{
			DryRun:         gotDryRun,
			PruneAllImages: allImages,
			PruneVolumes:   volumes,
		}
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all-images", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !gotDryRun || !gotOpts.PruneAllImages || !gotOpts.PruneVolumes {
		t.Fatalf("options = %+v, want DryRun=true PruneAllImages=true PruneVolumes=true", gotOpts)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		n    uint64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1200, "1.2kB"},
		{12310000, "12.3MB"},
		{1200000000, "1.2GB"},
	}
	for _, tc := range tests {
		if got := formatBytes(tc.n); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}