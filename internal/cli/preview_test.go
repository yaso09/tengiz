package cli

import (
	"testing"
)

func TestPreviewCommandsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"preview"})
	if err != nil {
		t.Fatalf("preview command not found: %v", err)
	}
	if cmd == nil {
		t.Fatal("preview command is nil")
	}

	subCommands := []string{"list", "rm", "deploy"}
	for _, name := range subCommands {
		sub, _, err := cmd.Find([]string{name})
		if err != nil {
			t.Errorf("preview %s subcommand not found: %v", name, err)
		}
		if sub == nil {
			t.Errorf("preview %s subcommand is nil", name)
		}
	}
}
