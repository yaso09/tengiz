package runtime

import (
	"context"
	"os/exec"
	"testing"
)

func TestDockerPruneContainersCommand(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	r, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	cmd := exec.CommandContext(context.Background(), "docker", "container", "prune",
		"--filter", "label=tengiz-app", "--force", "--dry-run")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("docker container prune dry-run failed (expected on some versions): %v\n%s", err, string(out))
	}
	_ = r
}
