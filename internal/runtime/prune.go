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

func nonTengizStopped(all, tengiz, running []string) []string {
	inTengiz := make(map[string]bool, len(tengiz))
	for _, id := range tengiz {
		inTengiz[id] = true
	}
	inRunning := make(map[string]bool, len(running))
	for _, id := range running {
		inRunning[id] = true
	}
	var out []string
	for _, id := range all {
		if !inTengiz[id] && !inRunning[id] {
			out = append(out, id)
		}
	}
	return out
}

func (r *dockerRuntime) pruneDryRun(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	report := &PruneReport{DryRun: true}

	// Containers: stopped non-tengiz candidates.
	allOut, err := exec.CommandContext(ctx, "docker", "ps", "-aq").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps -aq: %w\n%s", err, string(allOut))
	}
	tengizOut, err := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "label=tengiz-app").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps (tengiz): %w\n%s", err, string(tengizOut))
	}
	runningOut, err := exec.CommandContext(ctx, "docker", "ps", "-q").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps -q: %w\n%s", err, string(runningOut))
	}

	report.Containers = len(nonTengizStopped(
		strings.Fields(string(allOut)),
		strings.Fields(string(tengizOut)),
		strings.Fields(string(runningOut)),
	))

	// Images: non-tengiz dangling (default) or all non-tengiz (--all).
	imgArgs := []string{"images", "-aq", "--filter", "dangling=true"}
	if opts.All {
		imgArgs = []string{"images", "-aq"}
	}
	imgOut, err := exec.CommandContext(ctx, "docker", imgArgs...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(imgOut))
	}
	tengizImgOut, err := exec.CommandContext(ctx, "docker", "images", "-aq", "--filter", "label=tengiz-app").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images (tengiz): %w\n%s", err, string(tengizImgOut))
	}
	tengizImgs := make(map[string]bool)
	for _, id := range strings.Fields(string(tengizImgOut)) {
		tengizImgs[id] = true
	}
	for _, id := range strings.Fields(string(imgOut)) {
		if !tengizImgs[id] {
			report.Images++
		}
	}

	return report, nil
}
