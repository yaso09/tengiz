package cli

import (
	"testing"
)

func TestNotificationCommandsRegistered(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"notification"})
	if cmd == nil || cmd.Name() != "notification" {
		t.Fatal("notification command not registered on rootCmd")
	}

	subs := []string{"enable", "disable", "config", "set-channel", "show"}
	for _, name := range subs {
		sub, _, _ := cmd.Find([]string{name})
		if sub == nil || sub.Name() != name {
			t.Fatalf("notification %s subcommand not found", name)
		}
	}
}
