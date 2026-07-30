package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func (d *dockerRuntime) PruneContainers(ctx context.Context, env string, dryRun bool) ([]string, error) {
	args := []string{"container", "prune", "--filter", "label=tengiz-env=" + env, "--force"}
	if dryRun {
		args = append(args, "--filter", "until=0")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("prune containers: %w: %s", err, string(out))
	}
	return parsePruneOutput(string(out)), nil
}

func (d *dockerRuntime) PruneImages(ctx context.Context, env string, dryRun bool) ([]string, error) {
	args := []string{"image", "prune", "--filter", "label=tengiz-env=" + env, "--force"}
	if dryRun {
		args = append(args, "--filter", "until=0")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("prune images: %w: %s", err, string(out))
	}
	return parsePruneOutput(string(out)), nil
}

func (d *dockerRuntime) PruneVolumes(ctx context.Context, env string, dryRun bool) ([]string, error) {
	args := []string{"volume", "prune", "--force"}
	if dryRun {
		args = append(args, "--filter", "until=0")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("prune volumes: %w: %s", err, string(out))
	}
	return parsePruneOutput(string(out)), nil
}

func (d *dockerRuntime) PruneNetworks(ctx context.Context, env string, dryRun bool) ([]string, error) {
	args := []string{"network", "prune", "--force"}
	if dryRun {
		args = append(args, "--filter", "until=0")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("prune networks: %w: %s", err, string(out))
	}
	return parsePruneOutput(string(out)), nil
}

func (d *dockerRuntime) PruneBuildCache(ctx context.Context, dryRun bool) ([]string, error) {
	args := []string{"builder", "prune", "--force"}
	if dryRun {
		args = append(args, "--filter", "until=0")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("prune build cache: %w: %s", err, string(out))
	}
	return parsePruneOutput(string(out)), nil
}

func (d *dockerRuntime) PruneSystem(ctx context.Context, env string, dryRun bool, volumes bool) (PruneReport, error) {
	var report PruneReport
	report.DryRun = dryRun
	report.Env = env

	containers, err := d.PruneContainers(ctx, env, dryRun)
	if err != nil {
		return report, err
	}
	report.Containers = containers

	images, err := d.PruneImages(ctx, env, dryRun)
	if err != nil {
		return report, err
	}
	report.Images = images

	networks, err := d.PruneNetworks(ctx, env, dryRun)
	if err != nil {
		return report, err
	}
	report.Networks = networks

	_, err = d.PruneBuildCache(ctx, dryRun)
	if err != nil {
		return report, err
	}
	report.BuildCache = true

	if volumes {
		vols, err := d.PruneVolumes(ctx, env, dryRun)
		if err != nil {
			return report, err
		}
		report.Volumes = vols
	}

	return report, nil
}

func (d *dockerRuntime) DiskUsage(ctx context.Context) (DiskUsageReport, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{.Type}}\t{{.Size}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return DiskUsageReport{}, fmt.Errorf("disk usage: %w: %s", err, string(out))
	}

	var report DiskUsageReport
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		typ := parts[0]
		size, _ := parseSize(parts[1])
		switch typ {
		case "Containers":
			report.Containers = size
		case "Images":
			report.Images = size
		case "Volumes":
			report.Volumes = size
		case "Build Cache":
			report.BuildCache = size
		}
	}
	report.Total = report.Containers + report.Images + report.Volumes + report.BuildCache
	report.HumanTotal = humanBytes(report.Total)
	return report, nil
}

func parsePruneOutput(out string) []string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var result []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "Total reclaimed space:") {
			result = append(result, line)
		}
	}
	return result
}

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0B" {
		return 0, nil
	}
	var num float64
	var unit string
	n, _ := fmt.Sscanf(s, "%f%s", &num, &unit)
	if n < 1 {
		return 0, fmt.Errorf("cannot parse size: %s", s)
	}
	if n == 1 {
		return int64(num), nil
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
	}
	mult, ok := multipliers[unit]
	if !ok {
		return 0, fmt.Errorf("unknown unit: %s", unit)
	}
	return int64(num * float64(mult)), nil
}

func humanBytes(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
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
