package housekeeping

import (
	"context"
	"os/exec"
)

var RealDocker execFunc = func(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}
