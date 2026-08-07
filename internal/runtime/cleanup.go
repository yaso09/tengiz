package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strconv"
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

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	result := PruneResult{DryRun: opts.DryRun}

	if opts.Images {
		n, err := r.pruneImages(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.ImagesRemoved = n
	}

	if opts.BuildCache {
		bytes, err := r.pruneBuildCache(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.BuildCacheReclaimed = bytes
	}

	if opts.Containers {
		n, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.ContainersRemoved = n
	}

	if opts.Volumes {
		n, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return result, err
		}
		result.VolumesRemoved = n
	}

	return result, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) (int, error) {
	if dryRun {
		out, err := exec.CommandContext(ctx, "docker", "ps", "-a",
			"--filter", "status=exited",
			"--filter", "label!=tengiz-app",
			"--format", "{{.ID}}").Output()
		if err != nil {
			return 0, fmt.Errorf("docker ps: %w", err)
		}
		return countNonEmptyLines(string(out)), nil
	}
	cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
		"--filter", "label!=tengiz-app")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return countPrunedItems(string(out)), nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, dryRun bool) (int, error) {
	if dryRun {
		out, err := exec.CommandContext(ctx, "docker", "images", "-q",
			"--filter", "dangling=true").Output()
		if err != nil {
			return 0, fmt.Errorf("docker images: %w", err)
		}
		return countNonEmptyLines(string(out)), nil
	}
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return countPrunedImages(string(out)), nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) (int, error) {
	if dryRun {
		out, err := exec.CommandContext(ctx, "docker", "volume", "ls", "-q",
			"-f", "dangling=true").Output()
		if err != nil {
			return 0, fmt.Errorf("docker volume ls: %w", err)
		}
		return countNonEmptyLines(string(out)), nil
	}
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return countPrunedItems(string(out)), nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) (int64, error) {
	if dryRun {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parseReclaimedBytes(string(out)), nil
}

func countPrunedItems(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "Total reclaimed") || strings.HasPrefix(t, "Deleted") {
			continue
		}
		count++
	}
	return count
}

func countNonEmptyLines(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func countPrunedImages(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "deleted:") {
			count++
		}
	}
	return count
}

func parseReclaimedBytes(out string) int64 {
	prefix := "Total reclaimed space: "
	idx := strings.Index(out, prefix)
	if idx < 0 {
		return 0
	}
	fields := strings.Fields(out[idx+len(prefix):])
	if len(fields) != 2 {
		return 0
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToUpper(fields[1])
	var mult float64
	switch unit {
	case "B":
		mult = 1
	case "KB", "KIB":
		mult = 1 << 10
	case "MB", "MIB":
		mult = 1 << 20
	case "GB", "GIB":
		mult = 1 << 30
	case "TB", "TIB":
		mult = 1 << 40
	default:
		return 0
	}
	return int64(val * mult)
}
