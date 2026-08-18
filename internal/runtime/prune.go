package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func pruneContainerArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func pruneImageArgs() []string {
	return []string{"image", "prune", "-f", "--filter", "dangling=true", "--filter", "reference!=tengiz-apps/*"}
}

func pruneNetworkArgs() []string {
	return []string{"network", "prune", "-f"}
}

func pruneVolumeArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func pruneBuildCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func listContainerCandidatesArgs() []string {
	return []string{"ps", "-aq", "--filter", "status=exited", "--filter", "label!=tengiz-app"}
}

func listImageCandidatesArgs() []string {
	return []string{"images", "-q", "--filter", "dangling=true", "--filter", "reference!=tengiz-apps/*"}
}

func countOutputLines(out string) int {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	n := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func parsePruneOutput(out string) (int, string) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	count := 0
	reclaimed := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "Total reclaimed space:"):
			reclaimed = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		case strings.HasPrefix(line, "Build cache entries removed:"):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Build cache entries removed:"))); err == nil {
				count = n
			}
		case strings.HasPrefix(line, "deleted:") || strings.HasPrefix(line, "untagged:"):
			count++
		case strings.HasSuffix(line, ":"):
			// Header line such as "Deleted Containers:" — skip
		case !strings.Contains(line, " "):
			// Bare object ID (containers, networks, volumes)
			count++
		}
	}
	return count, reclaimed
}

func runPruneCommand(ctx context.Context, args []string) (int, string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("docker %s %s: %w\n%s", args[0], args[1], err, string(out))
	}
	count, reclaimed := parsePruneOutput(string(out))
	return count, reclaimed, nil
}

func runListCommand(ctx context.Context, args []string) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return countOutputLines(string(out)), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	res := &PruneResult{DryRun: opts.DryRun}

	if opts.Containers {
		if opts.DryRun {
			n, err := runListCommand(ctx, listContainerCandidatesArgs())
			if err != nil {
				return nil, err
			}
			res.ContainersRemoved = n
		} else {
			n, reclaimed, err := runPruneCommand(ctx, pruneContainerArgs())
			if err != nil {
				return nil, err
			}
			res.ContainersRemoved = n
			res.ReclaimedSpace = reclaimed
		}
	}

	if opts.Images {
		if opts.DryRun {
			n, err := runListCommand(ctx, listImageCandidatesArgs())
			if err != nil {
				return nil, err
			}
			res.ImagesRemoved = n
		} else {
			n, reclaimed, err := runPruneCommand(ctx, pruneImageArgs())
			if err != nil {
				return nil, err
			}
			res.ImagesRemoved = n
			res.ReclaimedSpace = reclaimed
		}
	}

	if opts.Networks && !opts.DryRun {
		n, reclaimed, err := runPruneCommand(ctx, pruneNetworkArgs())
		if err != nil {
			return nil, err
		}
		res.NetworksRemoved = n
		res.ReclaimedSpace = reclaimed
	}

	if opts.Volumes && !opts.DryRun {
		n, reclaimed, err := runPruneCommand(ctx, pruneVolumeArgs())
		if err != nil {
			return nil, err
		}
		res.VolumesRemoved = n
		res.ReclaimedSpace = reclaimed
	}

	if opts.BuildCache && !opts.DryRun {
		n, reclaimed, err := runPruneCommand(ctx, pruneBuildCacheArgs())
		if err != nil {
			return nil, err
		}
		res.BuildCacheRemoved = n
		res.ReclaimedSpace = reclaimed
	}

	return res, nil
}