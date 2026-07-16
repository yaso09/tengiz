package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/yaso09/tengiz/internal/types"
)

func (r *dockerRuntime) RunOnce(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string) error {
	containerName := fmt.Sprintf("tengiz-%s-run-%d", sanitizeContainerName(cfg.Name), time.Now().Unix())

	args := []string{
		"run", "--rm",
		"--name", containerName,
		"--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
	}
	args = append(args, "-i")
	fi, _ := os.Stdin.Stat()
	if fi != nil && fi.Mode()&os.ModeCharDevice != 0 {
		args = append(args, "-t")
	}
	args = append(args, envArgs(cfg.Env)...)
	args = append(args, resourceArgs(cfg.Resources)...)
	args = append(args, volumeArgs(cfg.Volumes)...)
	args = append(args, imageTag)
	args = append(args, cmd...)

	dcmd := exec.CommandContext(ctx, "docker", args...)
	dcmd.Stdin = os.Stdin
	dcmd.Stdout = os.Stdout
	dcmd.Stderr = os.Stderr

	if err := dcmd.Run(); err != nil {
		return fmt.Errorf("run once: %w", err)
	}
	return nil
}
