package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func parsePruneOutput(output string) PruneReport {
	var r PruneReport
	section := ""
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "Deleted Containers:":
			section = "containers"
		case trimmed == "Deleted Images:":
			section = "images"
		case trimmed == "Deleted Networks:":
			section = "networks"
		case trimmed == "Deleted Volumes:":
			section = "volumes"
		case trimmed == "Deleted build cache objects:":
			section = "buildcache"
		case strings.HasPrefix(trimmed, "Total reclaimed space:"):
			r.ReclaimedSpace = strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
			section = ""
		case trimmed == "":
			section = ""
		default:
			switch section {
			case "containers":
				r.Containers++
			case "images":
				if strings.HasPrefix(trimmed, "deleted:") {
					r.Images++
				}
			case "networks":
				r.Networks++
			case "volumes":
				r.Volumes++
			case "buildcache":
				r.BuildCache++
			}
		}
	}
	return r
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	if opts.DryRun {
		return r.pruneDryRun(ctx, opts)
	}

	args := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}

	report := parsePruneOutput(string(out))
	report.DryRun = false
	return &report, nil
}

func (r *dockerRuntime) pruneDryRun(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	return &PruneReport{DryRun: true}, nil
}
