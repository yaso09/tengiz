package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd.Use != "cleanup" {
		t.Fatalf("expected 'cleanup', got %q", cmd.Use)
	}
}

func TestCleanupSubcommands(t *testing.T) {
	subcommands := []string{"containers", "images", "volumes", "networks", "build-cache", "all"}
	for _, sub := range subcommands {
		t.Run(sub, func(t *testing.T) {
			cmd, _, err := rootCmd.Find([]string{"cleanup", sub})
			if err != nil {
				t.Fatalf("cleanup %s not found: %v", sub, err)
			}
			if cmd.Use != sub {
				t.Errorf("expected use %q, got %q", sub, cmd.Use)
			}
		})
	}
}

func TestCleanupAllVolumesFlag(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup", "all"})
	if err != nil {
		t.Fatalf("cleanup all not found: %v", err)
	}
	flag := cmd.Flags().Lookup("volumes")
	if flag == nil {
		t.Fatal("expected --volumes flag on cleanup all")
	}
	if flag.DefValue != "false" {
		t.Errorf("expected default false, got %q", flag.DefValue)
	}
}

func TestCleanupPersistentFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup not found: %v", err)
	}
	for _, name := range []string{"dry-run", "force"} {
		flag := cmd.PersistentFlags().Lookup(name)
		if flag == nil {
			t.Errorf("expected --%s persistent flag on cleanup", name)
		}
	}
}
