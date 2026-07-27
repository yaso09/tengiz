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

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{}

	if opts.All {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
	}

	if opts.Containers {
		out, err := r.pruneContainers(ctx)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		} else {
			report.ContainersRemoved, report.ContainersFreed = parsePruneOutput(out)
		}
	}

	if opts.Images {
		out, err := r.pruneImages(ctx)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		} else {
			report.ImagesRemoved, report.ImagesFreed = parsePruneOutput(out)
		}
	}

	if opts.Volumes {
		out, err := r.pruneVolumes(ctx)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		} else {
			report.VolumesRemoved = parsePruneCount(out)
		}
	}

	if opts.Networks {
		out, err := r.pruneNetworks(ctx)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		} else {
			report.NetworksRemoved = parsePruneCount(out)
		}
	}

	if opts.BuildCache {
		out, err := r.pruneBuildCache(ctx)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		} else {
			report.BuildCacheFreed = parseBuildCacheOutput(out)
		}
	}

	return report, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", "container", "prune",
		"--force",
		"--filter", "label!=tengiz-app",
	)
	return cmd.CombinedOutput()
}

func (r *dockerRuntime) pruneImages(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "prune",
		"--force", "--all",
		"--filter", "label!=tengiz-app",
	)
	return cmd.CombinedOutput()
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune",
		"--force",
	)
	return cmd.CombinedOutput()
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "prune",
		"--force",
		"--filter", "label!=tengiz-app",
	)
	return cmd.CombinedOutput()
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune",
		"--force", "--all",
	)
	return cmd.CombinedOutput()
}

func parsePruneOutput(out []byte) (int, string) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	count := 0
	space := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "Total reclaimed space:") {
			space = strings.TrimPrefix(line, "Total reclaimed space: ")
		} else if line != "" && !strings.HasPrefix(line, "WARNING") {
			count++
		}
	}
	return count, space
}

func parsePruneCount(out []byte) int {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	count := 0
	for _, line := range lines {
		if line != "" && !strings.HasPrefix(line, "WARNING") && !strings.HasPrefix(line, "Total reclaimed") {
			count++
		}
	}
	return count
}

func parseBuildCacheOutput(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimPrefix(line, "Total reclaimed space: ")
		}
	}
	return ""
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
