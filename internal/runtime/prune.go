package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type pruneCommand struct {
	label string
	args  []string
}

func buildPruneCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand
	if opts.Containers {
		cmds = append(cmds, pruneCommand{
			label: "containers",
			args:  []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		})
	}
	if opts.Images {
		cmds = append(cmds, pruneCommand{
			label: "images",
			args:  []string{"image", "prune", "-af", "--filter", "reference!=tengiz-apps/*"},
		})
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{
			label: "volumes",
			args:  []string{"volume", "prune", "-f"},
		})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{
			label: "networks",
			args:  []string{"network", "prune", "-f"},
		})
	}
	if opts.BuildCache {
		cmds = append(cmds, pruneCommand{
			label: "build cache",
			args:  []string{"builder", "prune", "-f"},
		})
	}
	return cmds
}

func PrunePlan(opts PruneOptions) []string {
	var plan []string
	for _, c := range buildPruneCommands(opts) {
		plan = append(plan, "docker "+strings.Join(c.args, " "))
	}
	return plan
}

func parseReclaimed(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var reclaimed []string
	for _, c := range buildPruneCommands(opts) {
		cmd := exec.CommandContext(ctx, "docker", c.args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return PruneResult{}, fmt.Errorf("docker %s prune: %w\n%s", c.label, err, string(out))
		}
		if sp := parseReclaimed(string(out)); sp != "" {
			reclaimed = append(reclaimed, sp)
		}
	}
	return PruneResult{Reclaimed: strings.Join(reclaimed, ", ")}, nil
}
