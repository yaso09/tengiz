package runtime

import (
	"context"
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

const pruneLabelFilter = "label!=tengiz-app"

var pruneCategorySingular = map[string]string{
	"containers": "container",
	"images":     "image",
	"volumes":    "volume",
	"networks":   "network",
	"builder":    "builder",
}

func buildPruneArgs(category string) []string {
	if category == "builder" {
		return []string{"builder", "prune", "-f"}
	}
	singular, ok := pruneCategorySingular[category]
	if !ok {
		return nil
	}
	return []string{singular, "prune", "-f", "--filter", pruneLabelFilter}
}

func buildPruneListArgs(category string) []string {
	switch category {
	case "containers":
		return []string{"ps", "-aq", "--format", "{{.ID}}|{{.State}}|{{.Labels}}"}
	case "images":
		return []string{"image", "ls", "-q", "--filter", "dangling=true"}
	case "volumes":
		return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
	case "networks":
		return []string{"network", "ls", "-q"}
	default:
		return nil
	}
}

func countPrunableContainers(output string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		state, labels := parts[1], parts[2]
		if state == "running" {
			continue
		}
		if strings.Contains(labels, "tengiz-app=") {
			continue
		}
		count++
	}
	return count
}

func countLines(output string) int {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return 0
	}
	n := 0
	for _, l := range strings.Split(trimmed, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	return &PruneResult{DryRun: opts.DryRun}, nil
}
