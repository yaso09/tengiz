package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= n {
		return nil
	}

	sort.Slice(lines, func(i, j int) bool {
		partsI := strings.SplitN(lines[i], "|", 2)
		partsJ := strings.SplitN(lines[j], "|", 2)
		if len(partsI) < 2 || len(partsJ) < 2 {
			return false
		}
		return partsI[1] < partsJ[1]
	})

	for i := 0; i < len(lines)-n; i++ {
		parts := strings.SplitN(lines[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") {
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
		}
	}
	return nil
}

func parsePruneOutput(out []byte) PruneStats {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var deleted int
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Total reclaimed space:") || strings.HasPrefix(line, "Deleted") {
			continue
		}
		deleted++
	}
	return PruneStats{ItemsRemoved: deleted}
}

func (r *dockerRuntime) PruneContainers(ctx context.Context) (PruneStats, error) {
	cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneStats{}, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, all bool) (PruneStats, error) {
	args := []string{"image", "prune", "-f"}
	if all {
		args = append(args, "-a")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneStats{}, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context) (PruneStats, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneStats{}, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context) (PruneStats, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneStats{}, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) (PruneStats, error) {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneStats{}, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(out), nil
}
