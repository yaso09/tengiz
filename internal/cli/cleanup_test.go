package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRuns(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("env", "production", "")
	cleanupCmd.RunE(cmd, []string{})
}
