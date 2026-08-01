package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

// resetCleanupFlags restores all cleanup flag values to their defaults.
// Cobra/pflag persist flag values across Execute() calls on the global
// rootCmd, so tests that set different flags must reset state to stay
// order-independent.
func resetCleanupFlags() {
	for _, name := range []string{"all", "containers", "images", "all-images", "volumes", "networks", "cache", "dry-run", "force", "stats"} {
		cleanupCmd.Flags().Set(name, "false")
	}
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"all", "containers", "images", "all-images", "volumes", "networks", "cache", "dry-run", "force", "stats"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestDescribePruneTargets(t *testing.T) {
	tests := []struct {
		name string
		opts runtime.PruneOptions
		want string
	}{
		{
			name: "all categories",
			opts: runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, Cache: true},
			want: "stopped non-Tengiz containers, dangling images, unused volumes, unused networks, build cache",
		},
		{
			name: "all images",
			opts: runtime.PruneOptions{Images: true, AllImages: true},
			want: "unused images",
		},
		{
			name: "empty",
			opts: runtime.PruneOptions{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describePruneTargets(tt.opts); got != tt.want {
				t.Errorf("describePruneTargets() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanupForceSkipsPrompt(t *testing.T) {
	old := newHousekeeper
	defer func() { newHousekeeper = old }()
	newHousekeeper = func() (runtime.Housekeeper, error) {
		return runtime.NewStubHousekeeper(), nil
	}
	resetCleanupFlags()

	rootCmd.SetArgs([]string{"cleanup", "--force"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if strings.Contains(output, "Continue?") {
		t.Error("expected --force to skip the confirmation prompt")
	}
}

func TestCleanupDryRunOutput(t *testing.T) {
	old := newHousekeeper
	defer func() { newHousekeeper = old }()
	newHousekeeper = func() (runtime.Housekeeper, error) {
		return runtime.NewStubHousekeeper(), nil
	}
	resetCleanupFlags()

	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	for _, want := range []string{"containers:", "images:", "volumes:", "networks:", "cache:"} {
		if !strings.Contains(output, want) {
			t.Errorf("dry-run output missing %q, got: %s", want, output)
		}
	}
}

func TestCleanupSelectiveContainersOnly(t *testing.T) {
	old := newHousekeeper
	defer func() { newHousekeeper = old }()
	newHousekeeper = func() (runtime.Housekeeper, error) {
		return runtime.NewStubHousekeeper(), nil
	}
	resetCleanupFlags()

	rootCmd.SetArgs([]string{"cleanup", "--force", "--containers"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if strings.Contains(output, "dry run") {
		t.Error("--containers must not trigger the dry-run path")
	}
}
