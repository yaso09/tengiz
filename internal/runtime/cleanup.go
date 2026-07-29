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

func (r *dockerRuntime) Cleanup(ctx context.Context, opts types.CleanupOptions) (*types.CleanupReport, error) {
	report := &types.CleanupReport{DryRun: opts.DryRun}

	if opts.All {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.BuildCache = true
	}

	if opts.Containers {
		n, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return report, fmt.Errorf("prune containers: %w", err)
		}
		report.ContainersRemoved = n
	}

	if opts.Images {
		n, freed, err := r.pruneImages(ctx, opts.DryRun)
		if err != nil {
			return report, fmt.Errorf("prune images: %w", err)
		}
		report.ImagesRemoved = n
		report.TotalSpaceFreed += freed
	}

	if opts.Volumes {
		n, freed, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return report, fmt.Errorf("prune volumes: %w", err)
		}
		report.VolumesRemoved = n
		report.TotalSpaceFreed += freed
	}

	if opts.BuildCache {
		freed, err := r.pruneBuildCache(ctx, opts.DryRun)
		if err != nil {
			return report, fmt.Errorf("prune build cache: %w", err)
		}
		report.BuildCacheFreed = freed
		report.TotalSpaceFreed += freed
	}

	return report, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) (int, error) {
	args := []string{"container", "prune", "-f"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, "--filter", "label!=tengiz-app", "--filter", "label!=tengiz-env")
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return countPrunedLines(string(out)), nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, dryRun bool) (int, int64, error) {
	args := []string{"image", "prune", "-f", "--all"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	n, freed := parsePruneOutput(string(out))
	return n, freed, nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) (int, int64, error) {
	args := []string{"volume", "prune", "-f"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	n, freed := parsePruneOutput(string(out))
	return n, freed, nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) (int64, error) {
	args := []string{"builder", "prune", "-f", "--all"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	freed := parseBuildCacheOutput(string(out))
	return freed, nil
}

func countPrunedLines(output string) int {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "Total reclaimed space: 0B" || strings.HasPrefix(line, "Deleted") || strings.HasPrefix(line, "Total reclaimed space:") {
			if strings.HasPrefix(line, "Total reclaimed space:") {
				continue
			}
			continue
		}
		count++
	}
	return count
}

func parsePruneOutput(output string) (int, int64) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	count := 0
	var space int64
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "deleted:") || strings.HasPrefix(line, "untagged:") {
			count++
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			space = parseSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return count, space
}

func parseBuildCacheOutput(output string) int64 {
	output = strings.TrimSpace(output)
	output = strings.TrimPrefix(output, "Total: ")
	return parseSpace(output)
}

func parseSpace(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0B" {
		return 0
	}

	var value float64
	var unit string
	n, _ := fmt.Sscanf(s, "%f%s", &value, &unit)
	if n < 2 {
		return 0
	}

	multipliers := map[string]int64{
		"B":  1,
		"kB": 1000,
		"MB": 1000 * 1000,
		"GB": 1000 * 1000 * 1000,
		"TB": 1000 * 1000 * 1000 * 1000,
		"KiB": 1024,
		"MiB": 1024 * 1024,
		"GiB": 1024 * 1024 * 1024,
		"TiB": 1024 * 1024 * 1024 * 1024,
		"KB": 1000,
		"K":  1000,
		"M":  1000 * 1000,
		"G":  1000 * 1000 * 1000,
		"T":  1000 * 1000 * 1000 * 1000,
	}

	if mult, ok := multipliers[unit]; ok {
		return int64(value * float64(mult))
	}
	return 0
}
