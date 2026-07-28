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

func (r *dockerRuntime) PruneContainers(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
		"--filter", "label!=tengiz-app",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f", "-a",
		"--filter", "label!=tengiz-app",
	)
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
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f", "-a")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) CleanupOrphanedContainers(ctx context.Context, activeApps []string) error {
	activeSet := make(map[string]bool, len(activeApps))
	for _, app := range activeApps {
		activeSet[app] = true
	}

	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s", labelKey),
		"--format", "{{.Names}}\t{{.Label \""+labelKey+"\"}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker ps for orphan check: %w\n%s", err, string(out))
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var removeNames []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		appName := strings.TrimSpace(parts[1])
		if !activeSet[appName] {
			removeNames = append(removeNames, strings.TrimSpace(parts[0]))
		}
	}

	for _, name := range removeNames {
		rmCmd := exec.CommandContext(ctx, "docker", "rm", "-f", name)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			log.Printf("[runtime] failed to remove orphaned container %s: %v\n%s", name, err, string(out))
		}
	}
	return nil
}

func (r *dockerRuntime) CleanupOrphanedImages(ctx context.Context, activeApps []string) error {
	activeSet := make(map[string]bool, len(activeApps))
	for _, app := range activeApps {
		activeSet[app] = true
	}

	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", "reference=tengiz-apps/*",
		"--format", "{{.Repository}}:{{.Tag}}\t{{.Repository}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images for orphan check: %w\n%s", err, string(out))
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var removeTags []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		tag := strings.TrimSpace(parts[0])
		repo := strings.TrimSpace(parts[1])
		appName := strings.TrimPrefix(repo, "tengiz-apps/")
		if !activeSet[appName] {
			removeTags = append(removeTags, tag)
		}
	}

	for _, tag := range removeTags {
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove orphaned image %s: %v", tag, err)
		}
	}
	return nil
}

func (r *dockerRuntime) CleanupAppImages(ctx context.Context, appName string) error {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w\n%s", err, string(out))
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := r.RemoveImage(ctx, line); err != nil {
			log.Printf("[runtime] failed to remove image %s: %v", line, err)
		}
	}
	return nil
}

func (r *dockerRuntime) CleanupAppResources(ctx context.Context, appName string) error {
	exec.CommandContext(ctx, "docker", "stop", "-t", "5", appName).Run()
	exec.CommandContext(ctx, "docker", "rm", "-f", appName).Run()

	if err := r.CleanupAppImages(ctx, appName); err != nil {
		return err
	}

	return nil
}
