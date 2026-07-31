package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCmdHasFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "all", "volumes"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupCmdRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c == cleanupCmd {
			found = true
		}
	}
	if !found {
		t.Error("cleanupCmd not registered on rootCmd")
	}
}

func TestCleanupDryRunOutput(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", true, "")
	out := captureOutput(func() { cleanupCmd.RunE(cmd, nil) })
	if !strings.Contains(out, "dry run") && !strings.Contains(out, "would") {
		t.Errorf("expected dry-run wording in output, got %q", out)
	}
}
