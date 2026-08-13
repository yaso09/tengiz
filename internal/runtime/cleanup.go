package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func buildCleanupArgs(opts CleanupOptions) []string {
	args := []string{"system", "prune", "-f"}
	if opts.All {
		args = append(args, "--all")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	if opts.App != "" {
		args = append(args, "--filter", "label!=tengiz-app")
	}
	return args
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	cmd := exec.CommandContext(ctx, "docker", buildCleanupArgs(opts)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return res, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}

	// Parse the "Total reclaimed space: X" line for reporting.
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			res.BuildCacheFreed = int64(len(line))
		}
	}

	// Retain the most recent images per app.
	if opts.KeepImages > 0 {
		apps := []string{}
		if opts.App != "" {
			apps = []string{opts.App}
		} else {
			list, listErr := r.List(ctx)
			if listErr == nil {
				seen := map[string]bool{}
				for _, a := range list {
					if !seen[a.Name] {
						seen[a.Name] = true
						apps = append(apps, a.Name)
					}
				}
			}
		}
		for _, app := range apps {
			if err := r.KeepLastNImages(ctx, app, opts.KeepImages); err != nil {
				log.Printf("[runtime] failed to keep %d images for %s: %v", opts.KeepImages, app, err)
			}
		}
	}

	return res, nil
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
