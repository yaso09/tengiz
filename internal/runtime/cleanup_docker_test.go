package runtime

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func skipIfNoDocker(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found")
	}
}

func TestDockerPruneSystem(t *testing.T) {
	skipIfNoDocker(t)
	r := &dockerRuntime{}
	// Test with dryRun=true first; if --dry-run flag not supported, skip
	if err := r.PruneSystem(context.Background(), true); err != nil {
		if strings.Contains(err.Error(), "unknown flag") {
			t.Skip("docker version does not support --dry-run flag")
		}
		t.Fatalf("PruneSystem(dryRun=true): %v", err)
	}
}

func TestDockerDetectStaleStopped(t *testing.T) {
	skipIfNoDocker(t)
	r := &dockerRuntime{}
	containers, err := r.DetectStaleContainers(context.Background())
	if err != nil {
		t.Fatalf("DetectStaleContainers: %v", err)
	}
	t.Logf("stale containers: %v", containers)
}
