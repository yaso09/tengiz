package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

func parsePruneOutput(output string) (int, string) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var count int
	space := "0B"

	headerDone := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Deleted") || strings.HasPrefix(trimmed, "TYPE") {
			headerDone = true
			continue
		}
		if strings.HasPrefix(trimmed, "Total") {
			if idx := strings.LastIndex(trimmed, ":"); idx >= 0 {
				space = strings.TrimSpace(trimmed[idx+1:])
			}
			continue
		}
		if headerDone {
			count++
		}
	}
	return count, space
}

func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error) {
	var report types.CleanupReport
	var spaces []string

	if opts.Containers || opts.All {
		cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f", "--filter", "label!=tengiz-app")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("container prune: %w\n%s", err, string(out))
		}
		count, space := parsePruneOutput(string(out))
		report.ContainersRemoved = count
		if space != "0B" {
			spaces = append(spaces, space)
		}
	}

	if opts.Images || opts.All {
		cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f", "--filter", "dangling=true")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("image prune: %w\n%s", err, string(out))
		}
		count, space := parsePruneOutput(string(out))
		report.ImagesRemoved = count
		if space != "0B" {
			spaces = append(spaces, space)
		}
	}

	if opts.Volumes || opts.All {
		cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("volume prune: %w\n%s", err, string(out))
		}
		count, space := parsePruneOutput(string(out))
		report.VolumesRemoved = count
		if space != "0B" {
			spaces = append(spaces, space)
		}
	}

	if opts.Networks || opts.All {
		cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("network prune: %w\n%s", err, string(out))
		}
		count, space := parsePruneOutput(string(out))
		report.NetworksRemoved = count
		if space != "0B" {
			spaces = append(spaces, space)
		}
	}

	if opts.BuildCache || opts.All {
		cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("builder prune: %w\n%s", err, string(out))
		}
		count, space := parsePruneOutput(string(out))
		report.BuildCacheCleaned = count > 0
		if space != "0B" {
			spaces = append(spaces, space)
		}
	}

	if len(spaces) > 0 {
		report.SpaceReclaimed = strings.Join(spaces, " + ")
	}
	return report, nil
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
