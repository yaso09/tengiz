package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"images", "volumes", "networks", "cache", "all", "dry-run", "keep"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestExpandCleanupFlags(t *testing.T) {
	tests := []struct {
		name            string
		images, volumes bool
		networks, cache bool
		all             bool
		wantImages      bool
		wantVolumes     bool
		wantNetworks    bool
		wantBuildCache  bool
	}{
		{name: "all defaults", wantImages: false, wantVolumes: false, wantNetworks: false, wantBuildCache: false},
		{name: "all flag enables everything", all: true, wantImages: true, wantVolumes: true, wantNetworks: true, wantBuildCache: true},
		{name: "images only", images: true, wantImages: true, wantVolumes: false, wantNetworks: false, wantBuildCache: false},
		{name: "cache only", cache: true, wantImages: false, wantVolumes: false, wantNetworks: false, wantBuildCache: true},
		{name: "all overrides but preserves truthy", images: true, all: true, wantImages: true, wantVolumes: true, wantNetworks: true, wantBuildCache: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandCleanupFlags(tt.images, tt.volumes, tt.networks, tt.cache, tt.all)
			if got.Images != tt.wantImages || got.Volumes != tt.wantVolumes ||
				got.Networks != tt.wantNetworks || got.BuildCache != tt.wantBuildCache {
				t.Errorf("expandCleanupFlags(%v, %v, %v, %v, %v) = %+v, want images=%v volumes=%v networks=%v cache=%v",
					tt.images, tt.volumes, tt.networks, tt.cache, tt.all,
					got, tt.wantImages, tt.wantVolumes, tt.wantNetworks, tt.wantBuildCache)
			}
		})
	}
}

func TestFormatCleanupReport(t *testing.T) {
	rep := runtime.PruneReport{
		ContainersRemoved: 2,
		ImagesRemoved:     3,
		VolumesRemoved:    1,
		NetworksRemoved:   1,
		Space:             "1.2MB",
	}
	out := formatCleanupReport("removed", rep)
	for _, want := range []string{"removed", "2 containers", "3 images", "1 volumes", "1 networks", "1.2MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatCleanupReport() = %q, missing %q", out, want)
		}
	}
}
