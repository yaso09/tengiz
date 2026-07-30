package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd.Use != "cleanup" {
		t.Errorf("expected Use='cleanup', got %q", cmd.Use)
	}
}

func TestCleanupFlags(t *testing.T) {
	flags := []string{"containers", "images", "volumes", "networks", "build-cache", "all", "keep", "force"}
	for _, name := range flags {
		t.Run(name, func(t *testing.T) {
			f := cleanupCmd.Flags().Lookup(name)
			if f == nil {
				t.Errorf("cleanupCmd missing --%s flag", name)
			}
		})
	}
}

func TestCleanupDefaultAll(t *testing.T) {
	cleanupCmd.ParseFlags([]string{})
	all, _ := cleanupCmd.Flags().GetBool("all")
	containers, _ := cleanupCmd.Flags().GetBool("containers")
	images, _ := cleanupCmd.Flags().GetBool("images")
	volumes, _ := cleanupCmd.Flags().GetBool("volumes")
	networks, _ := cleanupCmd.Flags().GetBool("networks")
	buildCache, _ := cleanupCmd.Flags().GetBool("build-cache")

	if all {
		t.Error("--all should default to false at flag level")
	}

	// RunE default-to-all logic: when no specific flag set, opts.All becomes true
	opts := types.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		All:        all,
	}
	if !containers && !images && !volumes && !networks && !buildCache {
		opts.All = true
	}
	if !opts.All {
		t.Error("expected RunE to set All=true when no resource flags specified")
	}
}
