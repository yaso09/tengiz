package runtime

import (
	"context"
	"os/exec"
	"testing"
)

func TestDockerPruneDryRun(t *testing.T) {
	rt, err := NewDocker()
	if err != nil {
		t.Skip("docker binary not available")
	}
	ctx := context.Background()
	if out, err := exec.CommandContext(ctx, "docker", "ps").CombinedOutput(); err != nil {
		t.Skipf("docker daemon not reachable: %v\n%s", err, out)
	}
	summary, err := rt.Prune(ctx, PruneOptions{
		Containers: true,
		Images:     true,
		Networks:   true,
		Volumes:    true,
		BuildCache: true,
		All:        true,
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("Prune(dry-run) error = %v", err)
	}
	if !summary.DryRun {
		t.Error("summary.DryRun = false, want true")
	}
}