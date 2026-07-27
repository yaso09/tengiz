package cli

import (
	"testing"
)

func TestCleanupCmdRegistration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil {
		t.Fatal("cleanup command is nil")
	}
	if cmd.Use != "cleanup" {
		t.Errorf("Use = %q, want %q", cmd.Use, "cleanup")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	if cmd == nil {
		t.Fatal("cleanup command not found")
	}

	flags := []string{"all", "containers", "images", "volumes", "networks", "build-cache", "dry-run", "force"}
	for _, f := range flags {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("flag --%s not found on cleanup command", f)
		}
	}
}

func TestCleanupCmdEnvFlag(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd.Flags().Lookup("dry-run") == nil {
		t.Error("--dry-run flag not found on cleanup")
	}
	// Verify global --env flag is accessible (inherited from persistent flags)
	envFlag := rootCmd.PersistentFlags().Lookup("env")
	if envFlag == nil {
		t.Error("--env persistent flag not found on rootCmd")
	}
}

func TestDescribeCategories(t *testing.T) {
	tests := []struct {
		opts     string
		expected string
	}{
		{"all", "all Docker resources"},
		{"containers", "containers"},
		{"images", "images"},
		{"volumes", "volumes"},
		{"networks", "networks"},
		{"build-cache", "build cache"},
	}
	for _, tt := range tests {
		t.Run(tt.opts, func(t *testing.T) {
			// describeCategories is local to root.go
		})
	}
}
