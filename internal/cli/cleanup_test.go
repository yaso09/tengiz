package cli

import (
	"testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "cleanup" {
			found = true
			break
		}
	}
	if !found {
		t.Error("cleanup command not registered on rootCmd")
	}
}

func TestCleanupHasDryRunFlag(t *testing.T) {
	flag := cleanupCmd.Flags().Lookup("dry-run")
	if flag == nil {
		t.Error("cleanupCmd missing --dry-run flag")
	}
}

func TestCleanupHasAllFlag(t *testing.T) {
	flag := cleanupCmd.Flags().Lookup("all")
	if flag == nil {
		t.Error("cleanupCmd missing --all flag")
	}
}

func TestCleanupHasContainersFlag(t *testing.T) {
	flag := cleanupCmd.Flags().Lookup("containers")
	if flag == nil {
		t.Error("cleanupCmd missing --containers flag")
	}
}

func TestCleanupHasImagesFlag(t *testing.T) {
	flag := cleanupCmd.Flags().Lookup("images")
	if flag == nil {
		t.Error("cleanupCmd missing --images flag")
	}
}

func TestCleanupHasVolumesFlag(t *testing.T) {
	flag := cleanupCmd.Flags().Lookup("volumes")
	if flag == nil {
		t.Error("cleanupCmd missing --volumes flag")
	}
}

func TestCleanupHasNetworksFlag(t *testing.T) {
	flag := cleanupCmd.Flags().Lookup("networks")
	if flag == nil {
		t.Error("cleanupCmd missing --networks flag")
	}
}

func TestCleanupHasBuildCacheFlag(t *testing.T) {
	flag := cleanupCmd.Flags().Lookup("build-cache")
	if flag == nil {
		t.Error("cleanupCmd missing --build-cache flag")
	}
}
