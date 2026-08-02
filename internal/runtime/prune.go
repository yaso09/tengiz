package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

var sizeUnits = []struct {
	suffix string
	mult   int64
}{
	{"TB", 1_000_000_000_000},
	{"GB", 1_000_000_000},
	{"MB", 1_000_000},
	{"kB", 1_000},
	{"B", 1},
}

func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	for _, u := range sizeUnits {
		if strings.HasSuffix(s, u.suffix) {
			numStr := strings.TrimSuffix(s, u.suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0
			}
			return int64(num * float64(u.mult))
		}
	}
	return 0
}

func parseReclaimedSpace(output string) int64 {
	var total int64
	for _, line := range strings.Split(output, "\n") {
		const marker = "Total reclaimed space:"
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		val := strings.TrimSpace(line[idx+len(marker):])
		total += parseSize(val)
	}
	return total
}

func FormatBytes(n int64) string {
	if n < 1_000 {
		return strconv.FormatInt(n, 10) + "B"
	}
	for _, u := range sizeUnits {
		if n >= u.mult {
			return strconv.FormatFloat(float64(n)/float64(u.mult), 'f', 2, 64) + u.suffix
		}
	}
	return strconv.FormatInt(n, 10) + "B"
}

type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	All        bool
	DryRun     bool
}

type PruneReport struct {
	ContainersPruned bool
	ImagesPruned     bool
	VolumesPruned    bool
	NetworksPruned   bool
	BuildCachePruned bool
	ReclaimedBytes   int64
	DryRun           bool
	Summary          string
}

type pruneCommand struct {
	name string
	args []string
}

func buildPruneCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand

	containerArgs := []string{"container", "prune", "-f"}
	if !opts.All {
		containerArgs = append(containerArgs, "--filter", "label!=tengiz-app")
	}
	if opts.Containers {
		cmds = append(cmds, pruneCommand{name: "containers", args: containerArgs})
	}

	imageArgs := []string{"image", "prune", "-f"}
	if opts.All {
		imageArgs = []string{"image", "prune", "-a", "-f"}
	}
	if opts.Images {
		cmds = append(cmds, pruneCommand{name: "images", args: imageArgs})
	}

	if opts.Volumes {
		cmds = append(cmds, pruneCommand{name: "volumes", args: []string{"volume", "prune", "-f"}})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{name: "networks", args: []string{"network", "prune", "-f"}})
	}
	if opts.BuildCache {
		cmds = append(cmds, pruneCommand{name: "build-cache", args: []string{"builder", "prune", "-f"}})
	}
	return cmds
}

func runDockerPrune(ctx context.Context, args []string) (bool, int64, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, 0, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.Contains(string(out), "Deleted"), parseReclaimedSpace(string(out)), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "system", "df")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		return &PruneReport{DryRun: true, Summary: string(out)}, nil
	}

	report := &PruneReport{}
	for _, pc := range buildPruneCommands(opts) {
		removed, reclaimed, err := runDockerPrune(ctx, pc.args)
		if err != nil {
			return nil, fmt.Errorf("prune %s: %w", pc.name, err)
		}
		report.ReclaimedBytes += reclaimed
		switch pc.name {
		case "containers":
			report.ContainersPruned = removed
		case "images":
			report.ImagesPruned = removed
		case "volumes":
			report.VolumesPruned = removed
		case "networks":
			report.NetworksPruned = removed
		case "build-cache":
			report.BuildCachePruned = removed
		}
	}
	return report, nil
}
