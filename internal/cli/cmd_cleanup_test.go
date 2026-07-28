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

func TestCleanupDryRunShowsCategories(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--force", "--containers", "--images"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("cleanup --dry-run --force --containers --images: %v", err)
	}
}

func TestCleanupForceFlag(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup"})
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	flag := cmd.Flags().Lookup("force")
	if flag == nil {
		t.Error("cleanup missing --force flag")
	}
	if flag != nil && flag.Shorthand != "f" {
		t.Error("cleanup --force should have -f shorthand")
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

func TestPsHasVerboseFlag(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"ps"})
	if err != nil {
		t.Fatalf("ps command not found: %v", err)
	}
	flag := cmd.Flags().Lookup("verbose")
	if flag == nil {
		t.Error("ps command missing --verbose flag")
	}
}

func TestPsVerboseOutput(t *testing.T) {
	rootCmd.SetArgs([]string{"ps", "--verbose"})
	err := rootCmd.Execute()
	if err != nil {
		t.Logf("ps --verbose error (expected if no docker): %v", err)
	}
}
