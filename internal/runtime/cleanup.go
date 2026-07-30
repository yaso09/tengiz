package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func parsePruneOutput(output string) PruneReport {
	var report PruneReport
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Deleted Images:") || strings.HasPrefix(line, "Deleted:") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &report.Images)
			}
		} else if strings.HasPrefix(line, "Containers:") {
			fmt.Sscanf(line, "Containers: %d  Images: %d  Networks: %d  Build cache: %d",
				&report.Containers, &report.Images, &report.Networks, &report.BuildCache)
		} else if strings.HasPrefix(line, "Total reclaimed space:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				trimmed := strings.TrimSpace(parts[1])
				parsed := parseSize(trimmed)
				if parsed > report.BytesFreed {
					report.BytesFreed = parsed
				}
			}
		} else if strings.HasPrefix(line, "Space freed:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				parsed := parseSize(strings.TrimSpace(parts[1]))
				if parsed > report.BytesFreed {
					report.BytesFreed = parsed
				}
			}
		} else if strings.HasPrefix(line, "Space:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				sub := strings.Split(strings.TrimSpace(parts[1]), "/")
				parsed := parseSize(strings.TrimSpace(sub[0]))
				if parsed > report.BytesFreed {
					report.BytesFreed = parsed
				}
			}
		}
	}
	return report
}

func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var val float64
	var unit string
	n, _ := fmt.Sscanf(s, "%f%s", &val, &unit)
	if n < 1 {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "kb", "k":
		return int64(val * 1024)
	case "mb", "m":
		return int64(val * 1024 * 1024)
	case "gb", "g":
		return int64(val * 1024 * 1024 * 1024)
	case "tb", "t":
		return int64(val * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(val)
	}
}

func (r *dockerRuntime) PruneSystem(ctx context.Context, force bool) (PruneReport, error) {
	args := []string{"system", "prune",
		"--filter", fmt.Sprintf("label!=%s", labelKey),
		"--filter", fmt.Sprintf("label!=%s", envLabelKey),
	}
	if force {
		args = append(args, "-f")
	}
	args = append(args, "--volumes")

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}

	report := parsePruneOutput(string(out))
	return report, nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context, force bool) (PruneReport, error) {
	args := []string{"builder", "prune"}
	if force {
		args = append(args, "-f")
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}

	report := parsePruneOutput(string(out))
	return report, nil
}

func (r *dockerRuntime) PruneContainers(ctx context.Context, appName string) error {
	args := []string{"container", "prune",
		"--filter", fmt.Sprintf("label=%s=%s", labelKey, appName),
		"--filter", "status=exited",
		"-f",
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, appName string, keep int) error {
	return r.KeepLastNImages(ctx, appName, keep)
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
