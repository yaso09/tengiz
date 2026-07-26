package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestSecretCommandsRegistered(t *testing.T) {
	secretCmd := findSubcommand(rootCmd, "secret")
	if secretCmd == nil {
		t.Fatal("secret command not registered on rootCmd")
	}

	expected := []string{"set", "get", "unset", "list"}
	for _, name := range expected {
		sub := findSubcommand(secretCmd, name)
		if sub == nil {
			t.Fatalf("secret %s subcommand not registered", name)
		}
	}
}

func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}
