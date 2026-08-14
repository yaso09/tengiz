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

type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	BuildCache bool
	Volumes    bool
	Networks   bool
	KeepImages int
}

type CleanupResult struct {
	ContainersPruned int64
	ImagesPruned     int64
	BuildCacheFreed  int64
	VolumesPruned    int64
	NetworksPruned   int64
}

func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var result CleanupResult
	for _, args := range buildCleanupCommands(opts) {
		desc := strings.Join(args, " ")
		if opts.DryRun {
			log.Printf("[tengiz] dry-run: docker %s", desc)
			continue
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return result, fmt.Errorf("docker %s: %w\n%s", desc, err, string(out))
		}
		count, freed := parsePruneOutput(string(out))
		switch args[0] {
		case "container":
			result.ContainersPruned += count
		case "image":
			result.ImagesPruned += count
		case "builder":
			result.BuildCacheFreed += freed
		case "volume":
			result.VolumesPruned += count
		case "network":
			result.NetworksPruned += count
		}
	}
	return result, nil
}

const cleanupLabelFilter = "label=" + labelKey

func buildCleanupCommands(opts CleanupOptions) [][]string {
	var cmds [][]string
	if opts.Containers {
		cmds = append(cmds, []string{"container", "prune", "--force", "--filter", cleanupLabelFilter})
	}
	if opts.Images {
		cmds = append(cmds, []string{"image", "prune", "--force"})
	}
	if opts.BuildCache {
		cmds = append(cmds, []string{"builder", "prune", "--force"})
	}
	if opts.Volumes {
		cmds = append(cmds, []string{"volume", "prune", "--force"})
	}
	if opts.Networks {
		cmds = append(cmds, []string{"network", "prune", "--force"})
	}
	return cmds
}

const reclaimedPrefix = "Total reclaimed space:"

func parsePruneOutput(output string) (int64, int64) {
	var count, freed int64
	inDeleted := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "total reclaimed space:") {
			freed = parseBytes(strings.TrimSpace(line[len(reclaimedPrefix):]))
			inDeleted = false
			continue
		}
		if strings.HasPrefix(lower, "deleted ") && strings.HasSuffix(line, ":") {
			inDeleted = true
			continue
		}
		if inDeleted {
			count++
		}
	}
	return count, freed
}

func parseBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	type unit struct {
		suffix string
		mult   float64
	}
	units := []unit{
		{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
		{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3},
		{"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0
			}
			return int64(f * u.mult)
		}
	}
	return 0
}

func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
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
