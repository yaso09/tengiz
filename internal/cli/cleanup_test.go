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
		t.Errorf("expected Use=cleanup, got %q", cmd.Use)
	}
}

func TestCleanupFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	tests := []struct {
		name string
		flag string
	}{
		{"containers", "containers"},
		{"images", "images"},
		{"volumes", "volumes"},
		{"networks", "networks"},
		{"build-cache", "build-cache"},
		{"all", "all"},
		{"dry-run", "dry-run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := flags.Lookup(tt.flag)
			if f == nil {
				t.Fatalf("flag %q not found", tt.flag)
			}
			if f.Value.Type() != "bool" {
				t.Errorf("flag %q type = %s, want bool", tt.flag, f.Value.Type())
			}
		})
	}
}
