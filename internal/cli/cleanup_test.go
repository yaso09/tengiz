package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "cleanup" {
			found = true
			if c.Args != nil && c.Args(rootCmd, []string{"extra"}) == nil {
				t.Error("cleanup should reject positional arguments")
			}
		}
	}
	if !found {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	f := cleanupCmd.Flags()
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "interval"} {
		if f.Lookup(name) == nil {
			t.Errorf("cleanup command missing --%s flag", name)
		}
	}
	if f.Lookup("dry-run").DefValue != "false" {
		t.Errorf("--dry-run default = %q, want false", f.Lookup("dry-run").DefValue)
	}
	if f.Lookup("interval").DefValue != "" {
		t.Errorf("--interval default = %q, want empty", f.Lookup("interval").DefValue)
	}
}

func TestCleanupInvalidInterval(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--interval", "bogus"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --interval value")
	}
}

func TestHumanizeSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{2048, "2.0 kB"},
		{5 << 20, "5.0 MB"},
		{3 << 30, "3.0 GB"},
	}
	for _, tc := range tests {
		if got := humanizeSize(tc.in); got != tc.want {
			t.Errorf("humanizeSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
