package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
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

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
}

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=" + labelKey}
}

func imagePruneArgs() []string {
	return []string{"image", "prune", "-a", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func builderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func cleanupPruneJobs(opts CleanupOptions) [][]string {
	var jobs [][]string
	if opts.All || opts.Containers {
		jobs = append(jobs, containerPruneArgs())
	}
	if opts.All || opts.Images {
		jobs = append(jobs, imagePruneArgs())
	}
	if opts.All || opts.Volumes {
		jobs = append(jobs, volumePruneArgs())
	}
	if opts.All || opts.Networks {
		jobs = append(jobs, networkPruneArgs())
	}
	if opts.All || opts.BuildCache {
		jobs = append(jobs, builderPruneArgs())
	}
	return jobs
}

type pruneResult struct {
	removed int
	freed   uint64
}

var totalSpaceRe = regexp.MustCompile(`(?m)^(?:Total reclaimed space|Total):\s*(.+)$`)

func parsePruneOutput(out []byte) pruneResult {
	var res pruneResult
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if m := totalSpaceRe.FindStringSubmatch(line); m != nil {
			res.freed = parseSize(m[1])
			continue
		}
		if isShortID(line) || strings.HasPrefix(line, "deleted:") || isCacheRow(line) {
			res.removed++
		}
	}
	return res
}

func isShortID(s string) bool {
	if len(s) < 12 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func isCacheRow(s string) bool {
	fields := strings.Fields(s)
	if len(fields) < 4 {
		return false
	}
	return isShortID(fields[0])
}

var sizeRe = regexp.MustCompile(`^\s*([0-9.]+)\s*(b|kb|mb|gb|tb|kib|mib|gib|tib)?\s*$`)

func parseSize(s string) uint64 {
	m := sizeRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return 0
	}
	num, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	var mult float64
	switch m[2] {
	case "", "b":
		mult = 1
	case "kb":
		mult = 1000
	case "mb":
		mult = 1e6
	case "gb":
		mult = 1e9
	case "tb":
		mult = 1e12
	case "kib":
		mult = 1024
	case "mib":
		mult = 1024 * 1024
	case "gib":
		mult = 1024 * 1024 * 1024
	case "tib":
		mult = 1024 * 1024 * 1024 * 1024
	default:
		mult = 1
	}
	return uint64(num * mult)
}

func formatBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.2fkB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
