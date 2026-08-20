package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

type CleanupOptions struct {
	All     bool
	Volumes bool
}

type CleanupResult struct {
	ContainersDeleted int
	ImagesDeleted     int
	NetworksDeleted   int
	VolumesDeleted    int
	BuildCacheDeleted int
	ReclaimedSpace    string
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}

func parsePruneOutput(output string) CleanupResult {
	var result CleanupResult
	section := ""
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			result.ReclaimedSpace = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			section = ""
			continue
		}
		if strings.HasSuffix(line, ":") {
			section = pruneSection(line)
			continue
		}
		switch section {
		case "containers":
			result.ContainersDeleted++
		case "images":
			result.ImagesDeleted++
		case "networks":
			result.NetworksDeleted++
		case "volumes":
			result.VolumesDeleted++
		case "buildcache":
			result.BuildCacheDeleted++
		}
	}
	return result
}

func pruneSection(line string) string {
	switch {
	case strings.HasPrefix(line, "Deleted Containers"):
		return "containers"
	case strings.HasPrefix(line, "Deleted Images"):
		return "images"
	case strings.HasPrefix(line, "Deleted Networks"):
		return "networks"
	case strings.HasPrefix(line, "Deleted Volumes"):
		return "volumes"
	case strings.HasPrefix(line, "Deleted Build Cache"):
		return "buildcache"
	default:
		return ""
	}
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
