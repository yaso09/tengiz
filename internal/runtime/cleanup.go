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

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}

type imageInfo struct {
	repoTag    string
	id         string
	containers int
}

func parseImageListLine(line string) (imageInfo, bool) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) != 3 {
		return imageInfo{}, false
	}
	containers, err := strconv.Atoi(parts[2])
	if err != nil {
		return imageInfo{}, false
	}
	return imageInfo{repoTag: parts[0], id: parts[1], containers: containers}, true
}

func isTengizRepo(repoTag string) bool {
	repo := repoTag
	if idx := strings.IndexByte(repoTag, ':'); idx >= 0 {
		repo = repoTag[:idx]
	}
	return strings.HasPrefix(repo, "tengiz-apps/")
}

func countDeletedIDs(out, section string) int {
	idx := strings.Index(out, section)
	if idx < 0 {
		return 0
	}
	count := 0
	rest := strings.TrimLeft(out[idx+len(section):], "\n\r \t")
	for _, line := range strings.Split(rest, "\n") {
		if strings.TrimSpace(line) == "" {
			break
		}
		count++
	}
	return count
}

func nonEmptyLineCount(out string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func parseReclaimedBytes(out string) (int64, bool) {
	const marker = "Total reclaimed space:"
	idx := strings.LastIndex(out, marker)
	if idx < 0 {
		return 0, false
	}
	rest := strings.TrimSpace(out[idx+len(marker):])
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if (c >= '0' && c <= '9') || c == '.' {
			continue
		}
		value, err := strconv.ParseFloat(rest[:i], 64)
		if err != nil {
			return 0, false
		}
		suffix := strings.ToLower(strings.TrimSpace(rest[i:]))
		var mult int64
		switch suffix {
		case "b":
			mult = 1
		case "kb":
			mult = 1000
		case "mb":
			mult = 1000 * 1000
		case "gb":
			mult = 1000 * 1000 * 1000
		case "tb":
			mult = 1000 * 1000 * 1000 * 1000
		default:
			return 0, false
		}
		return int64(value * float64(mult)), true
	}
	return 0, false
}

func formatBytes(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "kMGTPE"[exp])
}
