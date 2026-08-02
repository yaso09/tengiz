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

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{}, nil
}

func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error) {
	return "", nil
}

func pruneArgs(category string) []string {
	switch category {
	case "container":
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "image":
		return []string{"image", "prune", "-f"}
	case "network":
		return []string{"network", "prune", "-f"}
	case "volume":
		return []string{"volume", "prune", "-f"}
	case "builder":
		return []string{"builder", "prune", "-f"}
	}
	return nil
}

func listArgs(category string) []string {
	switch category {
	case "container":
		return []string{"container", "ls", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.Names}}"}
	case "image":
		return []string{"images", "-a", "--filter", "dangling=true", "--format", "{{.Repository}}:{{.Tag}}"}
	case "network":
		return []string{"network", "ls", "--format", "{{.Name}}"}
	case "volume":
		return []string{"volume", "ls", "--format", "{{.Name}}"}
	case "builder":
		return []string{"builder", "du", "--format", "{{.ID}}"}
	}
	return nil
}

func parseListOutput(output string) []string {
	var names []string
	for _, line := range strings.Split(output, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		names = append(names, t)
	}
	return names
}

func filterNetworks(names []string) []string {
	var filtered []string
	for _, n := range names {
		switch n {
		case "bridge", "host", "none":
			continue
		}
		filtered = append(filtered, n)
	}
	return filtered
}

func parseReclaimedSpace(line string) (int64, error) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return 0, fmt.Errorf("no ':' in %q", line)
	}
	rest := strings.TrimSpace(line[idx+1:])
	for _, unit := range []struct {
		suffix string
		mult   int64
	}{
		{"TB", 1000000000000},
		{"GB", 1000000000},
		{"MB", 1000000},
		{"kB", 1000},
		{"B", 1},
	} {
		if strings.HasSuffix(rest, unit.suffix) {
			numStr := strings.TrimSpace(strings.TrimSuffix(rest, unit.suffix))
			f, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("parse number %q: %w", numStr, err)
			}
			return int64(f * float64(unit.mult)), nil
		}
	}
	return 0, fmt.Errorf("unknown unit in %q", rest)
}

func extractReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		t := strings.TrimSpace(line)
		if strings.Contains(t, "Total reclaimed space:") {
			return t
		}
	}
	return ""
}

func humanBytes(b int64) string {
	if b < 1000 {
		return fmt.Sprintf("%dB", b)
	}
	units := []string{"B", "kB", "MB", "GB", "TB"}
	f := float64(b)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	return fmt.Sprintf("%.1f%s", f, units[i])
}

func sumReclaimed(lines []string) string {
	var total int64
	for _, l := range lines {
		if l == "" {
			continue
		}
		b, err := parseReclaimedSpace(l)
		if err == nil {
			total += b
		}
	}
	if total == 0 && len(lines) == 0 {
		return ""
	}
	return humanBytes(total)
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
