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

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func imagePruneArgs(all bool) []string {
	args := []string{"image", "prune", "-f"}
	if all {
		args = append(args, "-a")
	}
	return args
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func countPrunedItems(output, header string) int {
	count := 0
	in := false
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, header) {
			in = true
			continue
		}
		if in {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "Total reclaimed space:") {
				break
			}
			if trimmed != "" {
				count++
			}
		}
	}
	return count
}

func countDeletedLines(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "deleted:") {
			count++
		}
	}
	return count
}

var reclaimedSpaceRe = regexp.MustCompile(`(?i)total reclaimed space:\s*([0-9.]+)\s*([a-z]+)`)

func parseReclaimedBytes(output string) int64 {
	m := reclaimedSpaceRe.FindStringSubmatch(output)
	if m == nil {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(m[2]) {
	case "b":
		return int64(val)
	case "kb":
		return int64(val * 1024)
	case "mb":
		return int64(val * 1024 * 1024)
	case "gb":
		return int64(val * 1024 * 1024 * 1024)
	case "tb":
		return int64(val * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(val)
	}
}
