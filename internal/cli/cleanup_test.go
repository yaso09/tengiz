package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	if cleanupCmd == nil {
		t.Fatal("cleanupCmd is nil")
	}
	if cleanupCmd.Use != "cleanup" {
		t.Errorf("cleanupCmd.Use = %q, want 'cleanup'", cleanupCmd.Use)
	}
}

func TestCleanupFlags(t *testing.T) {
	expected := []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run", "app"}
	for _, name := range expected {
		t.Run(name, func(t *testing.T) {
			flag := cleanupCmd.Flags().Lookup(name)
			if flag == nil {
				t.Errorf("cleanupCmd missing --%s flag", name)
			}
		})
	}
}
