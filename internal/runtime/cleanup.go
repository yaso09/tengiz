package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func (o PruneOptions) effective() PruneOptions {
	e := o
	if !e.Containers && !e.Images && !e.Networks && !e.BuildCache {
		e.Containers = true
		e.Images = true
		e.BuildCache = true
	}
	return e
}

func pruneCommands(opts PruneOptions) [][]string {
	opts = opts.effective()
	var cmds [][]string
	add := func(args ...string) {
		cmds = append(cmds, args)
	}
	if opts.Containers {
		a := []string{"container", "prune", "-f"}
		if !opts.All {
			a = append(a, "--filter", "label!=tengiz-app")
		}
		add(a...)
	}
	if opts.Networks {
		add("network", "prune", "-f")
	}
	if opts.Images {
		a := []string{"image", "prune", "-f"}
		if opts.All {
			a = append(a, "--all")
		}
		add(a...)
	}
	if opts.BuildCache {
		a := []string{"builder", "prune", "-f"}
		if opts.All {
			a = append(a, "--all")
		}
		add(a...)
	}
	if opts.Volumes {
		add("volume", "prune", "-f")
	}
	return cmds
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
