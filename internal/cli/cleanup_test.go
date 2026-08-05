package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil || cmd == nil || cmd.Name() != "cleanup" {
		t.Fatalf("cleanup command not registered: err=%v cmd=%v", err, cmd)
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"all", "volumes"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdRunEHelper(t *testing.T) {
	out := buildCleanupOutput(runtime.CleanupReport{
		ContainersRemoved: 2,
		ImagesRemoved:     3,
		NetworksRemoved:   1,
		SpaceReclaimed:    "1.2GB",
	})
	for _, want := range []string{"2 containers", "3 images", "1 networks", "1.2GB"} {
		if !strings.Contains(out, want) {
			t.Errorf("buildCleanupOutput() missing %q in: %q", want, out)
		}
	}
}
