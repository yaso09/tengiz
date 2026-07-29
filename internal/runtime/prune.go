package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var reclaimPattern = regexp.MustCompile(`Total reclaimed space:\s*([\d.]+\s*\w*)`)

func parsePruneOutput(out []byte) uint64 {
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		m := reclaimPattern.FindStringSubmatch(line)
		if len(m) >= 2 {
			return parseSize(m[1])
		}
	}
	return 0
}

func parseSize(s string) uint64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0
	}
	type unit struct {
		suffix string
		mult   uint64
	}
	units := []unit{
		{"TiB", 1024 * 1024 * 1024 * 1024},
		{"GiB", 1024 * 1024 * 1024},
		{"MiB", 1024 * 1024},
		{"KiB", 1024},
		{"TB", 1000 * 1000 * 1000 * 1000},
		{"GB", 1000 * 1000 * 1000},
		{"MB", 1000 * 1000},
		{"kB", 1000},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			numStr := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			val, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0
			}
			return uint64(val * float64(u.mult))
		}
	}
	return 0
}

func (r *dockerRuntime) protectLabelFilter() []string {
	return []string{
		"--filter", fmt.Sprintf("label!=%s", labelKey),
	}
}

func (r *dockerRuntime) PruneContainers(ctx context.Context) (uint64, error) {
	args := []string{"container", "prune", "-f"}
	args = append(args, r.protectLabelFilter()...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, all bool) (uint64, error) {
	args := []string{"image", "prune", "-f"}
	if all {
		args = append(args, "-a")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context) (uint64, error) {
	args := []string{"volume", "prune", "-f"}
	args = append(args, r.protectLabelFilter()...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context) (uint64, error) {
	args := []string{"network", "prune", "-f"}
	args = append(args, r.protectLabelFilter()...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context, all bool) (uint64, error) {
	args := []string{"builder", "prune", "-f"}
	if all {
		args = append(args, "-a")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (*DiskUsageInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{.Type}}\t{{.TotalCount}}\t{{.Size}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	info := &DiskUsageInfo{}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		typ := parts[0]
		count, _ := strconv.Atoi(parts[1])
		size := parts[2]
		switch typ {
		case "Containers":
			info.Containers = count
		case "Images":
			info.Images = count
		case "Volumes":
			info.Volumes = count
		case "Build Cache":
			info.BuildCache = count
		}
		if info.Size == "" || size > info.Size {
			info.Size = size
		}
	}
	return info, nil
}
