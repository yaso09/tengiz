package runtime

import (
	"context"
	"os/exec"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestRunOneOffDockerCommandShape(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	rt, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() failed: %v", err)
	}

	cfg := &types.AppConfig{
		Name: "testapp",
		Env:  map[string]string{"FOO": "bar"},
	}

	err = rt.RunOneOff(context.Background(), cfg, "alpine:latest", []string{"echo", "hello"}, RunOneOffOptions{Interactive: false})
	if err != nil {
		t.Fatalf("RunOneOff failed: %v", err)
	}
}

func TestRunOneOffPropagatesExitCode(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	rt, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() failed: %v", err)
	}

	cfg := &types.AppConfig{Name: "testapp"}

	err = rt.RunOneOff(context.Background(), cfg, "alpine:latest", []string{"sh", "-c", "exit 42"}, RunOneOffOptions{})
	if err == nil {
		t.Fatal("expected error for exit code 42")
	}
	if err.Error() != "command exited with code 42" {
		t.Fatalf("unexpected error: %v", err)
	}
}
