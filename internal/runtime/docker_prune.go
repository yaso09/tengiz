package runtime

import (
	"context"
	"fmt"
	"os/exec"
)

func (r *dockerRuntime) PruneContainers(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "container", "prune",
		"--filter", fmt.Sprintf("label!=%s", labelKey),
		"-f",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, all bool) error {
	args := []string{"image", "prune", "-f"}
	if all {
		args = append(args, "-a")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-a", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}
