package cli

import (
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/yaso09/tengiz/internal/runtime"
)

func resetCleanupFlags() {
	cleanupCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		f.Value.Set(f.DefValue)
	})
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	for _, flag := range []string{"dry-run", "yes", "volumes", "all"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestRunCleanupDryRun(t *testing.T) {
	resetCleanupFlags()
	dataDir = t.TempDir()
	cmd := cleanupCmd
	if err := cmd.ParseFlags([]string{"--dry-run"}); err != nil {
		t.Fatal(err)
	}
	output := captureOutput(func() {
		if err := runCleanup(cmd, runtime.NewStub()); err != nil {
			t.Errorf("runCleanup: %v", err)
		}
	})
	if !strings.Contains(output, "would remove") {
		t.Errorf("dry-run output missing 'would remove', got: %s", output)
	}
	if !strings.Contains(output, "build cache: yes") {
		t.Errorf("dry-run output missing build cache line, got: %s", output)
	}
}

func TestRunCleanupYes(t *testing.T) {
	resetCleanupFlags()
	dataDir = t.TempDir()
	cmd := cleanupCmd
	if err := cmd.ParseFlags([]string{"--yes"}); err != nil {
		t.Fatal(err)
	}
	output := captureOutput(func() {
		if err := runCleanup(cmd, runtime.NewStub()); err != nil {
			t.Errorf("runCleanup: %v", err)
		}
	})
	if !strings.Contains(output, "cleanup complete") {
		t.Errorf("output missing 'cleanup complete', got: %s", output)
	}
	if !strings.Contains(output, "build cache pruned: yes") {
		t.Errorf("output missing build cache pruned line, got: %s", output)
	}
}

func TestConfirmCleanup(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"n\n", false},
		{"\n", false},
		{"no\n", false},
	}
	for _, tt := range tests {
		got, err := confirmCleanup(strings.NewReader(tt.input))
		if err != nil {
			t.Fatalf("confirmCleanup(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("confirmCleanup(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
