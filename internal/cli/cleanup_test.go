package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRuns(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("env", "production", "")
	cleanupCmd.RunE(cmd, []string{})
}

func TestCleanupReportOutput(t *testing.T) {
	printCleanupReport(nil)

	printCleanupReport(&runtime.CleanupReport{
		ContainersRemoved: 5,
		ContainersFreed:   "1.2GB",
	})
}

func TestCleanupCommandFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("env", "production", "")
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	cmd.Flags().BoolP("all", "a", false, "")
	cmd.Flags().BoolP("force", "f", false, "")

	cmd.Flags().Set("all", "true")
	cmd.Flags().Set("force", "true")

	_ = cleanupCmd.RunE
}
