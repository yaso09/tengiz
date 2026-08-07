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

// CleanupOptions controls which Docker resources a Cleanup call prunes.
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	Force      bool
}

// CleanupResult reports what a Cleanup call pruned.
type CleanupResult struct {
	Categories []string
	Reclaimed  string
}

// cleanupCommands returns the docker prune argv vectors for each enabled
// category. Tengiz-managed containers are labeled tengiz-app, so the
// "label!=tengiz-app" filter guarantees a cleanup never deletes a managed app.
func cleanupCommands(opts CleanupOptions) [][]string {
	var cmds [][]string
	if opts.Containers {
		cmds = append(cmds, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.Images {
		cmds = append(cmds, []string{"image", "prune", "-af"})
	}
	if opts.BuildCache {
		cmds = append(cmds, []string{"builder", "prune", "-f"})
	}
	if opts.Volumes {
		cmds = append(cmds, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.Networks {
		cmds = append(cmds, []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	return cmds
}

// parseReclaimed extracts the human-readable "Total reclaimed space" line
// from a docker prune output, or "" if none is present.
func parseReclaimed(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(strings.ToLower(trimmed), "reclaimed") {
			return trimmed
		}
	}
	return ""
}
