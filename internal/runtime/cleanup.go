package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
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

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport
	totalReclaimed := int64(0)

	commands := pruneDockerCommands(opts, opts.DryRun)

	for _, cmdParts := range commands {
		args := []string{cmdParts[0], "prune"}
		if !opts.DryRun {
			args = append(args, "-f")
		}
		args = append(args, "--filter", "label=tengiz-app")

		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("docker %s prune: %w\n%s", cmdParts[0], err, string(out))
		}

		output := string(out)

		switch cmdParts[0] {
		case "container":
			report.ContainersDeleted = countDeleted(output)
			reclaimed := parseReclaimed(output)
			totalReclaimed += reclaimed
		case "image":
			report.ImagesDeleted = countDeleted(output)
			reclaimed := parseReclaimed(output)
			totalReclaimed += reclaimed
		case "volume":
			report.VolumesDeleted = countDeleted(output)
			reclaimed := parseReclaimed(output)
			totalReclaimed += reclaimed
		case "network":
			report.NetworksDeleted = countDeleted(output)
		case "builder":
			report.BuildCacheCleaned = true
			reclaimed := parseReclaimed(output)
			totalReclaimed += reclaimed
		}

		if output != "" && !opts.DryRun {
			log.Printf("[runtime] docker %s prune result: %s", cmdParts[0], strings.TrimSpace(output))
		}
	}

	report.SpaceReclaimed = formatBytes(totalReclaimed)
	return report, nil
}

func pruneDockerCommands(opts PruneOptions, dryRun bool) [][]string {
	categories := [][]string{
		{"container"},
		{"image"},
		{"volume"},
		{"network"},
		{"builder"},
	}

	if opts.All {
		return categories
	}

	var selected [][]string
	if opts.Containers {
		selected = append(selected, categories[0])
	}
	if opts.Images {
		selected = append(selected, categories[1])
	}
	if opts.Volumes {
		selected = append(selected, categories[2])
	}
	if opts.Networks {
		selected = append(selected, categories[3])
	}
	if opts.BuildCache {
		selected = append(selected, categories[4])
	}

	if len(selected) == 0 {
		return categories
	}
	return selected
}

func countDeleted(output string) int {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Total") || strings.HasPrefix(line, "Deleted") {
			continue
		}
		if strings.Contains(line, "SpaceReclaimed") {
			continue
		}
		count++
	}
	return count
}

func parseReclaimed(output string) int64 {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return parseSize(strings.TrimSpace(parts[1]))
			}
		}
		if strings.HasPrefix(line, "{") && strings.Contains(line, "SpaceReclaimed") {
			var result struct {
				SpaceReclaimed int64 `json:"SpaceReclaimed"`
			}
			if err := json.Unmarshal([]byte(line), &result); err == nil {
				return result.SpaceReclaimed
			}
		}
	}
	return 0
}

func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	var value float64
	var unit string
	n, _ := fmt.Sscanf(s, "%f%s", &value, &unit)
	if n < 1 {
		return 0
	}
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "B":
		return int64(value)
	case "KB", "K":
		return int64(value * 1024)
	case "MB", "M":
		return int64(value * 1024 * 1024)
	case "GB", "G":
		return int64(value * 1024 * 1024 * 1024)
	case "TB", "T":
		return int64(value * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(value)
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.2fGB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.2fMB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.2fKB", float64(b)/1024)
	default:
		return fmt.Sprintf("%dB", b)
	}
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
