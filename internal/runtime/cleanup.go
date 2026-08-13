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

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func imagePruneArgs() []string {
	return []string{"image", "prune", "-f", "--filter", "dangling=true"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func builderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

// parsePruneOutput extracts the item lines between a docker prune's
// "Deleted <X>:" header and the trailing "Total reclaimed space" line.
func parsePruneOutput(output string) []string {
	var ids []string
	started := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			if started {
				return ids
			}
		case strings.HasPrefix(line, "Deleted "):
			started = true
		case strings.HasPrefix(line, "Total reclaimed"):
			return ids
		case started:
			ids = append(ids, line)
		}
	}
	return ids
}

// countPruned counts item lines, skipping any with the given prefix
// (e.g. "untagged" so image output counts only actual deletions).
func countPruned(lines []string, skipPrefix string) int {
	n := 0
	for _, l := range lines {
		if skipPrefix != "" && strings.HasPrefix(l, skipPrefix) {
			continue
		}
		n++
	}
	return n
}

// parseReclaimedBytes parses "Total reclaimed space: <N><unit>" into bytes.
func parseReclaimedBytes(output string) int64 {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		return parseSize(rest)
	}
	return 0
}

func parseSize(s string) int64 {
	i := 0
	for i < len(s) && (s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	if i == 0 {
		return 0
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	unit := strings.TrimSpace(s[i:])
	switch unit {
	case "", "B", "b":
		return int64(num)
	case "kB", "KB", "K", "k":
		return int64(num * 1000)
	case "KiB", "KIB":
		return int64(num * 1024)
	case "MB", "mB", "M", "m":
		return int64(num * 1000 * 1000)
	case "MiB", "MIB":
		return int64(num * 1024 * 1024)
	case "GB", "G", "g":
		return int64(num * 1000 * 1000 * 1000)
	case "GiB", "GIB":
		return int64(num * 1024 * 1024 * 1024)
	case "TB", "T":
		return int64(num * 1000 * 1000 * 1000 * 1000)
	case "TiB", "TIB":
		return int64(num * 1024 * 1024 * 1024 * 1024)
	default:
		return 0
	}
}

type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type CleanupReport struct {
	Containers int
	Images     int
	Volumes    int
	Networks   int
	BuildCache int64
}

func runDockerPrune(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return string(out), nil
}

func pruneAndCount(ctx context.Context, args []string, skipPrefix string, firstErr *error) int {
	out, err := runDockerPrune(ctx, args)
	if err != nil {
		if *firstErr == nil {
			*firstErr = err
		}
		return 0
	}
	return countPruned(parsePruneOutput(out), skipPrefix)
}

func pruneAndReclaimed(ctx context.Context, args []string, firstErr *error) int64 {
	out, err := runDockerPrune(ctx, args)
	if err != nil {
		if *firstErr == nil {
			*firstErr = err
		}
		return 0
	}
	return parseReclaimedBytes(out)
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	var report CleanupReport
	var firstErr error

	if opts.Containers {
		report.Containers = pruneAndCount(ctx, containerPruneArgs(), "", &firstErr)
	}
	if opts.Images {
		report.Images = pruneAndCount(ctx, imagePruneArgs(), "untagged", &firstErr)
	}
	if opts.Volumes {
		report.Volumes = pruneAndCount(ctx, volumePruneArgs(), "", &firstErr)
	}
	if opts.Networks {
		report.Networks = pruneAndCount(ctx, networkPruneArgs(), "", &firstErr)
	}
	if opts.BuildCache {
		report.BuildCache = pruneAndReclaimed(ctx, builderPruneArgs(), &firstErr)
	}
	return report, firstErr
}
