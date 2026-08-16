package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

// pruneCommand returns the docker subcommand args for a prune category.
// Category names: containers, images, volumes, networks, cache.
func pruneCommand(category string) []string {
	switch category {
	case "containers":
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "images":
		return []string{"image", "prune", "-f"}
	case "volumes":
		return []string{"volume", "prune", "-f"}
	case "networks":
		return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "cache":
		return []string{"builder", "prune", "-f"}
	}
	return nil
}

// extractReclaimedSpace parses "Total reclaimed space: X" (or the builder
// variant "Total: X") out of a docker prune command's combined output.
func extractReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
		if strings.HasPrefix(line, "Total:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total:"))
		}
	}
	return ""
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "system", "df")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return PruneResult{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		return PruneResult{Output: string(out), DryRun: true}, nil
	}

	categories := []struct {
		name string
		on   bool
	}{
		{"containers", opts.Containers},
		{"images", opts.Images},
		{"volumes", opts.Volumes},
		{"networks", opts.Networks},
		{"cache", opts.Cache},
	}

	var spaces []string
	for _, cat := range categories {
		if !cat.on {
			continue
		}
		args := pruneCommand(cat.name)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return PruneResult{}, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
		if s := extractReclaimedSpace(string(out)); s != "" {
			spaces = append(spaces, s)
		}
	}
	return PruneResult{ReclaimedSpace: strings.Join(spaces, ", ")}, nil
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
