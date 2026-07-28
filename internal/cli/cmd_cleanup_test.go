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

func TestCleanupDryRun(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--force"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("cleanup --dry-run --force failed: %v", err)
	}
}

func TestCleanupRequiresConfirmation(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--all"})
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	flag := cmd.Flags().Lookup("force")
	if flag == nil {
		t.Error("cleanup command missing --force flag")
	}
}
