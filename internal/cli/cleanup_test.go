package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil {
		t.Fatal("cleanup command is nil")
	}
}

func TestCleanupFlags(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	if cmd == nil {
		t.Skip("cleanup command not registered")
	}
	flags := []string{"all", "containers", "images", "networks", "build-cache", "app", "dry-run"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("cleanup missing --%s flag", f)
		}
	}
}
