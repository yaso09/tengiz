package cli

import (
	"strings"
	"testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"containers", "images", "build-cache", "volumes", "networks", "all", "force"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestConfirmHTonYes(t *testing.T) {
	if !confirm(strings.NewReader("y\n"), "") {
		t.Fatal("expected 'y' to confirm")
	}
	if !confirm(strings.NewReader("YES\n"), "") {
		t.Fatal("expected 'YES' to confirm")
	}
}

func TestConfirmRejectsNo(t *testing.T) {
	if confirm(strings.NewReader("n\n"), "") {
		t.Fatal("expected 'n' to reject")
	}
	if confirm(strings.NewReader("\n"), "") {
		t.Fatal("expected empty input to reject")
	}
}
