package cli

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/yaso09/tengiz/internal/runtime"
)

func resetCleanupFlags() {
	cleanupCmd.Flags().VisitAll(func(f *pflag.Flag) {
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

func TestCleanupCommandHasFlags(t *testing.T) {
	for _, flag := range []string{"all", "containers", "images", "networks", "volumes", "build-cache", "dry-run"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestResolvePruneOptionsDefaults(t *testing.T) {
	resetCleanupFlags()
	cmd := cleanupCmd
	cmd.ParseFlags([]string{})
	opts, err := resolvePruneOptions(cmd)
	if err != nil {
		t.Fatalf("resolvePruneOptions() error = %v", err)
	}
	want := runtime.PruneOptions{Containers: true, Images: true, Networks: true}
	if !reflect.DeepEqual(opts, want) {
		t.Errorf("default opts = %+v, want %+v", opts, want)
	}
}

func TestResolvePruneOptionsAll(t *testing.T) {
	resetCleanupFlags()
	cmd := cleanupCmd
	cmd.ParseFlags([]string{"--all"})
	opts, err := resolvePruneOptions(cmd)
	if err != nil {
		t.Fatalf("resolvePruneOptions() error = %v", err)
	}
	want := runtime.PruneOptions{Containers: true, Images: true, Networks: true, Volumes: true, BuildCache: true}
	if !reflect.DeepEqual(opts, want) {
		t.Errorf("--all opts = %+v, want %+v", opts, want)
	}
}

func TestResolvePruneOptionsExplicitVolumesOnly(t *testing.T) {
	resetCleanupFlags()
	cmd := cleanupCmd
	cmd.ParseFlags([]string{"--volumes"})
	opts, err := resolvePruneOptions(cmd)
	if err != nil {
		t.Fatalf("resolvePruneOptions() error = %v", err)
	}
	want := runtime.PruneOptions{Volumes: true}
	if !reflect.DeepEqual(opts, want) {
		t.Errorf("--volumes opts = %+v, want %+v", opts, want)
	}
}

func TestResolvePruneOptionsDryRunKeepsDefaults(t *testing.T) {
	resetCleanupFlags()
	cmd := cleanupCmd
	cmd.ParseFlags([]string{"--dry-run"})
	opts, err := resolvePruneOptions(cmd)
	if err != nil {
		t.Fatalf("resolvePruneOptions() error = %v", err)
	}
	want := runtime.PruneOptions{Containers: true, Images: true, Networks: true}
	if !reflect.DeepEqual(opts, want) {
		t.Errorf("--dry-run opts = %+v, want %+v", opts, want)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		n    uint64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{2500, "2.5 kB"},
		{1024000, "1.0 MB"},
		{2348000000, "2.3 GB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.n); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestCleanupDryRunPrintsPlan(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	out := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("cleanup --dry-run failed: %v", err)
		}
	})
	for _, want := range []string{"would prune containers", "would prune images", "would prune networks"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q, got: %s", want, out)
		}
	}
}