package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func runDockerPrune(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker prune: %w\n%s", err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) getDockerDiskInfo(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{.Type}}|{{.Size}}|{{.Reclaimable}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

func parseReclaimed(output string) string {
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) == 3 && parts[0] == "Images" {
			return parts[2]
		}
	}
	return ""
}

func (r *dockerRuntime) PruneContainers(ctx context.Context) (*CleanupResult, error) {
	out, err := runDockerPrune(ctx, "container", "prune", "--filter", "label!=tengiz-app", "-f")
	if err != nil {
		return nil, err
	}
	result := &CleanupResult{}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Deleted Containers:") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &result.ContainersRemoved)
			}
		}
	}
	if result.ContainersRemoved == 0 && len(lines) > 1 {
		result.ContainersRemoved = len(lines) - 1
	}
	if len(lines) > 0 && strings.Contains(out, "Total reclaimed space:") {
		for _, line := range lines {
			if strings.Contains(line, "Total reclaimed space:") {
				result.ReclaimedSpace = strings.TrimPrefix(line, "Total reclaimed space:")
			}
		}
	}
	return result, nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context) (*CleanupResult, error) {
	out, err := runDockerPrune(ctx, "image", "prune", "-a", "-f")
	if err != nil {
		return nil, err
	}
	result := &CleanupResult{}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Deleted Images:") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &result.ImagesRemoved)
			}
		}
		if strings.Contains(line, "Total reclaimed space:") {
			result.ReclaimedSpace = strings.TrimPrefix(line, "Total reclaimed space: ")
		}
	}
	return result, nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context) (*CleanupResult, error) {
	out, err := runDockerPrune(ctx, "volume", "prune", "-f")
	if err != nil {
		return nil, err
	}
	result := &CleanupResult{}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Deleted Volumes:") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &result.VolumesRemoved)
			}
		}
		if strings.Contains(line, "Total reclaimed space:") {
			result.ReclaimedSpace = strings.TrimPrefix(line, "Total reclaimed space: ")
		}
	}
	return result, nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context) (*CleanupResult, error) {
	out, err := runDockerPrune(ctx, "network", "prune", "-f")
	if err != nil {
		return nil, err
	}
	result := &CleanupResult{}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Deleted Networks:") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &result.NetworksRemoved)
			}
		}
	}
	return result, nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) (*CleanupResult, error) {
	out, err := runDockerPrune(ctx, "builder", "prune", "-a", "-f")
	if err != nil {
		return nil, err
	}
	result := &CleanupResult{}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Total reclaimed space:") {
			result.BuildCacheFreed = strings.TrimPrefix(line, "Total reclaimed space: ")
			result.ReclaimedSpace = result.BuildCacheFreed
		}
	}
	return result, nil
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
