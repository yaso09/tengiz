package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func newCleanupTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cleanup-test"}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("volumes", false, "")
	return cmd
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

func TestCleanupCommandHasCategoryFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "networks", "volumes"} {
		if f := cleanupCmd.Flags().Lookup(flag); f == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupOptionsDefaultAll(t *testing.T) {
	opts := cleanupOptions(newCleanupTestCmd())
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.Volumes {
		t.Errorf("cleanupOptions() default = %+v, want all true", opts)
	}
}

func TestCleanupOptionsContainersOnly(t *testing.T) {
	cmd := newCleanupTestCmd()
	if err := cmd.Flags().Set("containers", "true"); err != nil {
		t.Fatal(err)
	}
	opts := cleanupOptions(cmd)
	if !opts.Containers {
		t.Error("Containers should be true with --containers")
	}
	if opts.Images || opts.Networks || opts.Volumes {
		t.Errorf("only Containers should be true, got %+v", opts)
	}
}

func TestCleanupOptionsImagesOnly(t *testing.T) {
	cmd := newCleanupTestCmd()
	if err := cmd.Flags().Set("images", "true"); err != nil {
		t.Fatal(err)
	}
	opts := cleanupOptions(cmd)
	if !opts.Images {
		t.Error("Images should be true with --images")
	}
	if opts.Containers || opts.Networks || opts.Volumes {
		t.Errorf("only Images should be true, got %+v", opts)
	}
}

func TestFormatCleanupReport(t *testing.T) {
	report := &runtime.CleanupReport{
		ContainersRemoved: 2, ImagesRemoved: 3, DanglingImagesRemoved: 1,
		NetworksRemoved: 1, VolumesRemoved: 4,
	}
	got := formatCleanupReport(report)
	want := "[tengiz] cleanup complete: 2 containers, 3 images (1 dangling), 1 networks, 4 volumes removed\n"
	if got != want {
		t.Errorf("formatCleanupReport() = %q, want %q", got, want)
	}
}
