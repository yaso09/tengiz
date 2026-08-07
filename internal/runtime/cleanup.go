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

const reclaimedPrefix = "Total reclaimed space: "

type pruneCommand struct {
	category string
	args     []string
}

func buildPruneCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand
	if opts.Containers {
		cmds = append(cmds, pruneCommand{
			category: "containers",
			// label filter keeps this safe: only stopped tengiz-managed containers
			args: []string{"container", "prune", "-f", "--filter", "label=" + labelKey},
		})
	}
	if opts.Images {
		args := []string{"image", "prune", "-f"}
		if opts.AllImages {
			args = append(args, "-a")
		}
		cmds = append(cmds, pruneCommand{category: "images", args: args})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{category: "networks", args: []string{"network", "prune", "-f"}})
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{category: "volumes", args: []string{"volume", "prune", "-f"}})
	}
	if opts.BuildCache {
		cmds = append(cmds, pruneCommand{category: "buildcache", args: []string{"builder", "prune", "-f", "-a"}})
	}
	return cmds
}

func buildDiskUsageArgs() []string {
	return []string{"system", "df", "--format", "{{.Type}}={{.Reclaimable}}"}
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, reclaimedPrefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, reclaimedPrefix))
		}
	}
	return ""
}

func parsePrunedCount(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, reclaimedPrefix) {
			continue
		}
		count++
	}
	return count
}

func parseHumanSize(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0B" {
		return 0, false
	}
	suffixes := []struct {
		suffix string
		mult   float64
	}{
		{"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"KB", 1e3},
	}
	for _, m := range suffixes {
		if strings.HasSuffix(s, m.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, m.suffix))
			v, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, false
			}
			return v * m.mult, true
		}
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v, true
	}
	return 0, false
}

func sumHumanSizes(sizes []string) string {
	var total float64
	for _, s := range sizes {
		if v, ok := parseHumanSize(s); ok {
			total += v
		}
	}
	if total == 0 {
		return ""
	}
	units := []struct {
		mult float64
		name string
	}{
		{1e9, "GB"}, {1e6, "MB"}, {1e3, "kB"}, {1, "B"},
	}
	for _, u := range units {
		if total >= u.mult {
			return fmt.Sprintf("%d%s", int(total/u.mult+0.5), u.name)
		}
	}
	return fmt.Sprintf("%dB", int(total+0.5))
}

func parseDiskUsage(output string) DiskUsage {
	var du DiskUsage
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		ty := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch ty {
		case "Images":
			du.Images = val
		case "Containers":
			du.Containers = val
		case "Local Volumes":
			du.Volumes = val
		case "Build Cache":
			du.BuildCache = val
		}
	}
	return du
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (DiskUsage, error) {
	return DiskUsage{}, nil
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
