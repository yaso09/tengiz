package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

var _ = (*dockerRuntime)(nil)

func (r *dockerRuntime) PruneSystem(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport

	if opts.All {
		opts.Containers = true
		opts.Images = true
		opts.Networks = true
		opts.BuildCache = true
		opts.Volumes = true
	}

	if opts.Containers {
		count, size, err := runContainerPrune(ctx)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		} else {
			report.ContainersPruned = count
			report.SpaceReclaimed = accumulateSpace(report.SpaceReclaimed, size)
		}
	}

	if opts.Images {
		count, size, err := runImagePrune(ctx, opts.Aggressive)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		} else {
			report.ImagesPruned = count
			report.SpaceReclaimed = accumulateSpace(report.SpaceReclaimed, size)
		}
	}

	if opts.Networks {
		count, size, err := runNetworkPrune(ctx)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		} else {
			report.NetworksPruned = count
			report.SpaceReclaimed = accumulateSpace(report.SpaceReclaimed, size)
		}
	}

	if opts.BuildCache {
		_, size, err := runBuildCachePrune(ctx)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		} else {
			report.BuildCacheFreed = size
			report.SpaceReclaimed = accumulateSpace(report.SpaceReclaimed, size)
		}
	}

	if opts.Volumes {
		count, size, err := runVolumePrune(ctx)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
		} else {
			report.VolumesPruned = count
			report.SpaceReclaimed = accumulateSpace(report.SpaceReclaimed, size)
		}
	}

	return report, nil
}

func runContainerPrune(ctx context.Context) (int, string, error) {
	args := []string{"container", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey)}
	return execPrune(ctx, args)
}

func runImagePrune(ctx context.Context, aggressive bool) (int, string, error) {
	if aggressive {
		return execPrune(ctx, []string{"image", "prune", "-a", "-f"})
	}
	return execPrune(ctx, []string{"image", "prune", "-a", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey)})
}

func runNetworkPrune(ctx context.Context) (int, string, error) {
	return execPrune(ctx, []string{"network", "prune", "-f"})
}

func runBuildCachePrune(ctx context.Context) (int, string, error) {
	return execPrune(ctx, []string{"builder", "prune", "-a", "-f"})
}

func runVolumePrune(ctx context.Context) (int, string, error) {
	return execPrune(ctx, []string{"volume", "prune", "-f"})
}

func execPrune(ctx context.Context, args []string) (int, string, error) {
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	count, size := parsePruneOutput(string(out))
	return count, size, nil
}

func parsePruneOutput(output string) (int, string) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	items := 0
	var totalSize string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			totalSize = strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
		} else if !strings.HasPrefix(trimmed, "Deleted:") && !strings.HasPrefix(trimmed, "Would ") && !strings.Contains(trimmed, " ") {
			items++
		}
	}
	return items, totalSize
}

func accumulateSpace(current, new string) string {
	if new == "" {
		return current
	}
	if current == "" {
		return new
	}
	return current + ", " + new
}
