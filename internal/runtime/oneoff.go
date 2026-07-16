package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/yaso09/tengiz/internal/types"
)

func (r *dockerRuntime) RunOneOff(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOneOffOptions) error {
	args := []string{
		"run", "--rm",
		"-i",
		"--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
		"--label", "tengiz-oneoff=true",
	}
	if opts.Interactive {
		args = append(args, "-t")
	}
	args = append(args, envArgs(cfg.Env)...)
	args = append(args, volumeArgs(cfg.Volumes)...)
	args = append(args, resourceArgs(cfg.Resources)...)
	args = append(args, imageTag)
	args = append(args, cmd...)

	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if err := c.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("command exited with code %d", exitErr.ExitCode())
		}
		return fmt.Errorf("docker run: %w", err)
	}
	return nil
}
