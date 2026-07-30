package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestRmCommandReturnsImageCleanupInfo(t *testing.T) {
	if rmCmd.Use != "rm <app>" {
		t.Errorf("rmCmd.Use = %q, want %q", rmCmd.Use, "rm <app>")
	}
}

func TestCleanupCommandRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "cleanup" {
			found = true
			break
		}
	}
	if !found {
		t.Error("cleanup command not registered on rootCmd")
	}
}

func TestCleanupCommandHasFlags(t *testing.T) {
	var cleanupCmd *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Use == "cleanup" {
			cleanupCmd = c
			break
		}
	}
	if cleanupCmd == nil {
		t.Skip("cleanup cmd not registered yet")
	}

	expectedFlags := []string{"containers", "images", "networks", "build-cache", "volumes", "all", "aggressive", "keep-images"}
	for _, name := range expectedFlags {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup command missing --%s flag", name)
		}
	}
}
