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

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{}, nil
}

func pruneContainersArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=" + labelKey}
}

func pruneImagesArgs() []string {
	return []string{"image", "prune", "-f"}
}

func pruneVolumesArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func pruneNetworksArgs() []string {
	return []string{"network", "prune", "-f"}
}

func containerDryListArgs() []string {
	return []string{"ps", "-a", "--filter", "status=exited", "--format", "{{json .}}"}
}

func imageDryListArgs() []string {
	return []string{"images", "-a", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func volumeDryListArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func networkDryListArgs() []string {
	return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func splitLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func countNonEmptyLines(s string) int {
	return len(splitLines(s))
}

// parsePruneOutput counts the items reported as deleted by a docker prune
// command and parses the "Total reclaimed space" footer into bytes.
func parsePruneOutput(out string) (int, int64) {
	count := 0
	var reclaimed int64
	inSection := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			reclaimed = parseSizeField(line)
			inSection = false
			continue
		}
		if strings.HasSuffix(line, ":") {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(line, "untagged:") {
				continue
			}
			count++
		}
	}
	return count, reclaimed
}

func parseSizeField(line string) int64 {
	return parseHumanSize(strings.TrimPrefix(line, "Total reclaimed space:"))
}

// parseHumanSize converts Docker's human-readable sizes (e.g. "12B",
// "1.4kB", "5MB", "2GB") into bytes. Unparseable input returns 0.
func parseHumanSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	num := s
	unit := ""
	for i := 0; i < len(s); i++ {
		if (s[i] < '0' || s[i] > '9') && s[i] != '.' && s[i] != '-' {
			num = s[:i]
			unit = s[i:]
			break
		}
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	var mult float64
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "b", "":
		mult = 1
	case "kb", "kib":
		mult = 1 << 10
	case "mb", "mib":
		mult = 1 << 20
	case "gb", "gib":
		mult = 1 << 30
	case "tb", "tib":
		mult = 1 << 40
	default:
		mult = 1
	}
	return int64(v * mult)
}
