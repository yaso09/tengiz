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

type PruneOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
}

type PruneReport struct {
	Containers     int    `json:"containers"`
	Images         int    `json:"images"`
	Networks       int    `json:"networks"`
	Volumes        int    `json:"volumes"`
	ReclaimedSpace string `json:"reclaimed_space"`
}

type pruneType string

const (
	pruneContainers pruneType = "containers"
	pruneImages     pruneType = "images"
	pruneNetworks   pruneType = "networks"
	pruneVolumes    pruneType = "volumes"
)

func buildPruneArgs(t pruneType) []string {
	switch t {
	case pruneContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case pruneImages:
		return []string{"image", "prune", "-f"}
	case pruneNetworks:
		return []string{"network", "prune", "-f"}
	case pruneVolumes:
		return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	default:
		return nil
	}
}

func parsePruneOutput(output string) (int, string) {
	count := 0
	reclaimed := ""
	seenDeletedHeading := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			reclaimed = strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
			continue
		}
		if strings.HasPrefix(trimmed, "Deleted ") {
			seenDeletedHeading = true
			continue
		}
		if seenDeletedHeading && trimmed != "" {
			count++
		}
	}
	return count, reclaimed
}

var sizeUnits = []struct {
	suffix  string
	divisor float64
}{
	{"TB", 1e12},
	{"GB", 1e9},
	{"MB", 1e6},
	{"kB", 1e3},
	{"B", 1},
}

func parseSize(s string) float64 {
	s = strings.TrimSpace(s)
	for _, u := range sizeUnits {
		if strings.HasSuffix(s, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0
			}
			return f * u.divisor
		}
	}
	return 0
}

func sumReclaimed(values []string) string {
	var total float64
	for _, v := range values {
		total += parseSize(v)
	}
	for _, u := range sizeUnits {
		if u.divisor > 1 && total >= u.divisor {
			return fmt.Sprintf("%.4g%s", total/u.divisor, u.suffix)
		}
	}
	return fmt.Sprintf("%.0fB", total)
}
