package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatalf("cleanup command not found, got %v", cmd)
	}
}

func TestCleanupFlags(t *testing.T) {
	for _, f := range []string{"containers", "images", "volumes", "networks", "dry-run"} {
		if cleanupCmd.Flags().Lookup(f) == nil {
			t.Errorf("cleanup command missing --%s flag", f)
		}
	}
}

func TestDefaultCleanupOptions(t *testing.T) {
	all := defaultCleanupOptions(runtime.CleanupOptions{})
	if !all.Containers || !all.Images || !all.Volumes || !all.Networks {
		t.Errorf("default cleanup should enable all categories, got %+v", all)
	}

	partial := defaultCleanupOptions(runtime.CleanupOptions{Images: true})
	if !partial.Images || partial.Containers || partial.Volumes || partial.Networks {
		t.Errorf("explicit category should not enable others, got %+v", partial)
	}

	dry := defaultCleanupOptions(runtime.CleanupOptions{DryRun: true})
	if !dry.DryRun || !dry.Containers || !dry.Images || !dry.Volumes || !dry.Networks {
		t.Errorf("dry-run with no categories should keep DryRun and enable all, got %+v", dry)
	}
}
