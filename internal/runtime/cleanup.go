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

type pruneCommand struct {
	category string
	label    string
	args     []string
}

func buildPruneCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand
	if opts.Containers {
		cmds = append(cmds, pruneCommand{
			category: "container",
			label:    "containers",
			args:     []string{"prune", "-f", "--filter", "label!=tengiz-app"},
		})
	}
	if opts.Images {
		args := []string{"prune", "-f"}
		if opts.All {
			args = append(args, "-a")
		}
		cmds = append(cmds, pruneCommand{category: "image", label: "images", args: args})
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{
			category: "volume",
			label:    "volumes",
			args:     []string{"prune", "-f", "--filter", "label!=tengiz-app"},
		})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{
			category: "network",
			label:    "networks",
			args:     []string{"prune", "-f"},
		})
	}
	if opts.BuildCache {
		cmds = append(cmds, pruneCommand{
			category: "builder",
			label:    "build-cache",
			args:     []string{"prune", "-f"},
		})
	}
	return cmds
}

func buildPruneListCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand
	if opts.Containers {
		cmds = append(cmds, pruneCommand{
			category: "container",
			label:    "containers",
			args:     []string{"ls", "-a", "--filter", "status=exited"},
		})
	}
	if opts.Images {
		cmds = append(cmds, pruneCommand{
			category: "image",
			label:    "images",
			args:     []string{"ls", "--filter", "dangling=true"},
		})
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{
			category: "volume",
			label:    "volumes",
			args:     []string{"ls"},
		})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{
			category: "network",
			label:    "networks",
			args:     []string{"ls"},
		})
	}
	if opts.BuildCache {
		cmds = append(cmds, pruneCommand{
			category: "builder",
			label:    "build-cache",
			args:     []string{"du"},
		})
	}
	return cmds
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

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	var cmds []pruneCommand
	if opts.DryRun {
		cmds = buildPruneListCommands(opts)
	} else {
		cmds = buildPruneCommands(opts)
	}
	result := &PruneResult{DryRun: opts.DryRun, Outputs: make(map[string]string, len(cmds))}
	for _, c := range cmds {
		args := append([]string{c.category}, c.args...)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker %s: %w\n%s", c.label, err, string(out))
		}
		result.Outputs[c.label] = string(out)
	}
	return result, nil
}
