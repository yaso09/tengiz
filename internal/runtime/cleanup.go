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

func (r *dockerRuntime) PruneSystem(ctx context.Context, dryRun bool) error {
	args := []string{"system", "prune", "-f",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneContainers(ctx context.Context, dryRun bool) error {
	args := []string{"container", "prune", "-f",
		"--filter", "label!=tengiz-app",
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, dryRun bool) error {
	args := []string{"image", "prune", "-f",
		"--filter", "label!=tengiz-app",
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context, dryRun bool) error {
	args := []string{"volume", "prune", "-f"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) DetectStaleContainers(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "label=tengiz-app",
		"--format", "{{.Names}}|{{.Labels}}|{{.State}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var stale []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		name := strings.TrimPrefix(parts[0], "/")
		state := parts[2]
		if state != "running" {
			stale = append(stale, name)
		}
	}
	return stale, nil
}

func (r *dockerRuntime) KeepLastNContainers(ctx context.Context, appName string, n int) error {
	format := `{{.ID}}|{{.CreatedAt}}`
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=tengiz-app=%s", appName),
		"--filter", "status=exited",
		"--filter", "status=created",
		"--format", format,
		"--no-trunc",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker ps: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= n {
		return nil
	}
	type container struct {
		id        string
		createdAt string
	}
	var containers []container
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		containers = append(containers, container{id: parts[0], createdAt: parts[1]})
	}
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].createdAt < containers[j].createdAt
	})
	for i := 0; i < len(containers)-n; i++ {
		rmCmd := exec.CommandContext(ctx, "docker", "rm", "-f", containers[i].id)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			log.Printf("[runtime] failed to remove stale container %s: %v\n%s", containers[i].id, err, string(out))
		}
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
