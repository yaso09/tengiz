package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}

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

const (
	categoryContainers = "containers"
	categoryImages     = "images"
	categoryVolumes    = "volumes"
	categoryNetworks   = "networks"
)

func buildPruneArgs(category string) []string {
	switch category {
	case categoryContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case categoryImages:
		return []string{"image", "prune", "-f"}
	case categoryVolumes:
		return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	case categoryNetworks:
		return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	}
	return nil
}

func buildDryRunArgs(category string) []string {
	switch category {
	case categoryContainers:
		return []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.ID}}"}
	case categoryImages:
		return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
	case categoryVolumes:
		return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
	case categoryNetworks:
		return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
	}
	return nil
}
