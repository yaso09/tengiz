package cli

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

func executeCommand(root *cobra.Command, args ...string) (string, error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	root.SetOut(nil)
	root.SetErr(nil)
	return buf.String(), err
}

func TestCleanupCommandUsage(t *testing.T) {
	output, err := executeCommand(rootCmd, "cleanup", "--help")
	if err != nil {
		t.Fatalf("cleanup --help error = %v", err)
	}
	if !contains(output, "Remove unused Docker resources") {
		t.Errorf("expected help text, got: %s", output)
	}
}

func TestCleanupCommandDryRun(t *testing.T) {
	output, err := executeCommand(rootCmd, "cleanup", "--dry-run")
	if err != nil {
		t.Fatalf("cleanup --dry-run error = %v", err)
	}
	if !contains(output, "dry-run") {
		t.Errorf("expected dry-run output, got: %s", output)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsInternal(s, substr)
}

func containsInternal(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
