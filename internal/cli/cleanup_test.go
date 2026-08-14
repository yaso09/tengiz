package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

func newCleanupTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	return cmd
}

func TestCleanupOptionsDefaultAll(t *testing.T) {
	opts := cleanupOptions(newCleanupTestCmd())
	want := cleanup.Options{Containers: true, Images: true, Volumes: true, Networks: true}
	if opts != want {
		t.Fatalf("cleanupOptions() = %+v, want %+v", opts, want)
	}
}

func TestCleanupOptionsSelectedCategories(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.Flags().Set("images", "true")
	cmd.Flags().Set("volumes", "true")
	opts := cleanupOptions(cmd)
	want := cleanup.Options{Containers: false, Images: true, Volumes: true, Networks: false}
	if opts != want {
		t.Fatalf("cleanupOptions() = %+v, want %+v", opts, want)
	}
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}
