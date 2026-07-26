package cli

import (
	"testing"
)

func TestPreviewCmdRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "preview" {
			found = true
			break
		}
	}
	if !found {
		t.Error("preview command not registered on root")
	}
}

func TestPreviewSubCommands(t *testing.T) {
	if previewCmd == nil {
		t.Skip("previewCmd not defined")
	}
	subCommands := []string{"list", "rm", "deploy"}
	for _, name := range subCommands {
		found := false
		for _, sub := range previewCmd.Commands() {
			if sub.Use == name || len(sub.Use) > len(name) && sub.Use[:len(name)] == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("preview subcommand %q not found", name)
		}
	}
}
