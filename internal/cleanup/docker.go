package cleanup

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

const labelKey = "tengiz-app"

type dockerRuntime struct{}

// runDocker runs the docker CLI and returns trimmed combined output.
func runDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func splitLines(out string) []string {
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// removeAll removes items one at a time, logging (not failing on) individual errors.
func (r *dockerRuntime) removeAll(ctx context.Context, buildArgs func(item []string) []string, items []string) []string {
	var removed []string
	for _, item := range items {
		if _, err := runDocker(ctx, buildArgs([]string{item})...); err != nil {
			log.Printf("[cleanup] failed to remove %s: %v", item, err)
			continue
		}
		removed = append(removed, item)
	}
	return removed
}

// ---------- containers ----------

func buildExitedContainerListArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", fmt.Sprintf("label!=%s", labelKey),
		"--format", "{{.Names}}",
	}
}

func buildContainerRemoveArgs(names []string) []string {
	return append([]string{"rm"}, names...)
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) ([]string, error) {
	out, err := runDocker(ctx, buildExitedContainerListArgs()...)
	if err != nil {
		return nil, err
	}
	candidates := splitLines(out)
	if dryRun {
		return candidates, nil
	}
	return r.removeAll(ctx, buildContainerRemoveArgs, candidates), nil
}

// ---------- orchestration ----------

func (r *dockerRuntime) Prune(ctx context.Context, opts Options) (Report, error) {
	rep := Report{DryRun: opts.DryRun}

	if opts.All || opts.Containers {
		names, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("containers: %w", err)
		}
		rep.Containers = names
	}

	return rep, nil
}
