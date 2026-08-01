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

func stoppedContainerArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--format", "{{.ID}}|{{.Labels}}",
	}
}

func hasLabel(labels, key string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] == key {
			return true
		}
	}
	return false
}

func parseForeignContainers(output string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		if hasLabel(parts[1], labelKey) {
			continue
		}
		ids = append(ids, parts[0])
	}
	return ids
}

func danglingImageArgs() []string {
	return []string{"images", "--filter", "dangling=true", "-q"}
}

func parseImageIDs(output string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line != "" {
			ids = append(ids, strings.TrimSpace(line))
		}
	}
	return ids
}

func buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	report := CleanupReport{DryRun: opts.DryRun}

	if opts.Containers {
		n, err := r.cleanupContainers(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.ContainersRemoved = n
	}

	if opts.Images {
		n, err := r.cleanupDanglingImages(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.ImagesRemoved = n
	}

	if opts.BuildCache {
		pruned, err := r.cleanupBuildCache(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.BuildCachePruned = pruned
	}

	if opts.Volumes {
		pruned, err := r.cleanupVolumes(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.VolumesPruned = pruned
	}

	return report, nil
}

func (r *dockerRuntime) cleanupContainers(ctx context.Context, dryRun bool) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", stoppedContainerArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	ids := parseForeignContainers(string(out))
	if dryRun {
		return len(ids), nil
	}
	removed := 0
	for _, id := range ids {
		if err := r.Remove(ctx, id); err != nil {
			log.Printf("[runtime] cleanup: failed to remove container %s: %v", id, err)
			continue
		}
		removed++
	}
	return removed, nil
}

func (r *dockerRuntime) cleanupDanglingImages(ctx context.Context, dryRun bool) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", danglingImageArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	ids := parseImageIDs(string(out))
	if dryRun {
		return len(ids), nil
	}
	removed := 0
	for _, id := range ids {
		if err := r.RemoveImage(ctx, id); err != nil {
			log.Printf("[runtime] cleanup: failed to remove image %s: %v", id, err)
			continue
		}
		removed++
	}
	return removed, nil
}

func (r *dockerRuntime) cleanupBuildCache(ctx context.Context, dryRun bool) (bool, error) {
	if dryRun {
		return false, nil
	}
	cmd := exec.CommandContext(ctx, "docker", buildCachePruneArgs()...)
	if _, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("docker builder prune: %w", err)
	}
	return true, nil
}

func (r *dockerRuntime) cleanupVolumes(ctx context.Context, dryRun bool) (bool, error) {
	if dryRun {
		return false, nil
	}
	cmd := exec.CommandContext(ctx, "docker", volumePruneArgs()...)
	if _, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("docker volume prune: %w", err)
	}
	return true, nil
}
