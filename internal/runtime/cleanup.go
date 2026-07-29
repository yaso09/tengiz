package runtime

import (
	"context"
	"fmt"
	"log"
	"math"
	"os/exec"
	"sort"
	"strings"
)

func pruneArgs(category string, opts PruneOptions) []string {
	args := []string{category, "prune", "-f"}

	switch category {
	case "builder":
		args = append(args, "-a")
		return args
	case "container":
		if opts.AppName != "" {
			args = append(args, "--filter", fmt.Sprintf("label=%s=%s", labelKey, opts.AppName))
		} else {
			args = append(args, "--filter", "label!=tengiz-app")
		}
	case "image":
		args = append(args, "--filter", "dangling=true")
		if opts.AppName != "" {
			args = append(args, "--filter", fmt.Sprintf("label=%s=%s", labelKey, opts.AppName))
		} else {
			args = append(args, "--filter", "label!=tengiz-app")
		}
	case "network":
		if opts.AppName != "" {
			args = append(args, "--filter", fmt.Sprintf("label=%s=%s", labelKey, opts.AppName))
		} else {
			args = append(args, "--filter", "label!=tengiz-app")
		}
	}

	if opts.Env != "" {
		args = append(args, "--filter", fmt.Sprintf("label=%s=%s", envLabelKey, opts.Env))
	}

	return args
}

func parsePruneCount(output string) int {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Total") || strings.HasPrefix(line, "Space") || strings.HasPrefix(line, "Deleted") {
			continue
		}
		count++
	}
	return count
}

func parsePruneSpace(output string) int64 {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Total reclaimed space") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return parseDockerSize(strings.TrimSpace(parts[1]))
			}
		}
	}
	return 0
}

func parseDockerSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var value float64
	var unit string
	n, _ := fmt.Sscanf(s, "%f%s", &value, &unit)
	if n < 1 {
		return 0
	}
	switch strings.TrimSpace(unit) {
	case "B", "":
		return int64(math.Round(value))
	case "kB":
		return int64(math.Round(value * 1024))
	case "MB":
		return int64(math.Round(value * 1024 * 1024))
	case "GB":
		return int64(math.Round(value * 1024 * 1024 * 1024))
	default:
		return int64(math.Round(value))
	}
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport
	report.DryRun = opts.DryRun

	if opts.All {
		opts.Containers = true
		opts.Images = true
		opts.Networks = true
		opts.BuildCache = true
	}

	if opts.Containers {
		args := pruneArgs("container", opts)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
		}
		report.ContainersRemoved = parsePruneCount(string(out))
		report.BuildCacheFreed += parsePruneSpace(string(out))
	}

	if opts.Images {
		args := pruneArgs("image", opts)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
		}
		report.ImagesRemoved = parsePruneCount(string(out))
		report.BuildCacheFreed += parsePruneSpace(string(out))
	}

	if opts.Networks {
		args := pruneArgs("network", opts)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
		}
		report.NetworksRemoved = parsePruneCount(string(out))
	}

	if opts.BuildCache {
		args := pruneArgs("builder", opts)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
		}
		report.BuildCacheFreed += parsePruneSpace(string(out))
	}

	return report, nil
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
