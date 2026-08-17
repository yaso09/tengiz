package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const protectLabelFilter = "label!=tengiz-app"

type pruneStep struct {
	label string
	args  []string
}

func prunePlan(opts PruneOptions) []pruneStep {
	steps := []pruneStep{
		{label: "containers", args: []string{"container", "prune", "-f", "--filter", protectLabelFilter}},
		{label: "images", args: []string{"image", "prune", "-f"}},
		{label: "networks", args: []string{"network", "prune", "-f"}},
		{label: "build-cache", args: []string{"builder", "prune", "-f"}},
	}
	if opts.Volumes {
		steps = append(steps, pruneStep{
			label: "volumes",
			args:  []string{"volume", "prune", "-f", "--filter", protectLabelFilter},
		})
	}
	return steps
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var result PruneResult
	for _, step := range prunePlan(opts) {
		cmd := exec.CommandContext(ctx, "docker", step.args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return result, fmt.Errorf("docker %s prune: %w\n%s", step.label, err, string(out))
		}
		line := strings.TrimSpace(string(out))
		switch step.label {
		case "containers":
			result.Containers = line
		case "images":
			result.Images = line
		case "networks":
			result.Networks = line
		case "volumes":
			result.Volumes = line
		case "build-cache":
			result.BuildCache = line
		}
	}
	return result, nil
}

func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
