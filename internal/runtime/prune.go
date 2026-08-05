package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type pruneCmd struct {
	Exec []string
}

func pruneContainerArgs() pruneCmd {
	return pruneCmd{Exec: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}}
}

func pruneImageArgs(all bool) pruneCmd {
	args := []string{"image", "prune"}
	if all {
		args = append(args, "-a")
	}
	args = append(args, "-f", "--filter", "label!=tengiz-app")
	return pruneCmd{Exec: args}
}

func pruneNetworkArgs() pruneCmd {
	return pruneCmd{Exec: []string{"network", "prune", "-f"}}
}

func pruneVolumeArgs() pruneCmd {
	return pruneCmd{Exec: []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}}
}

func pruneBuilderArgs() pruneCmd {
	return pruneCmd{Exec: []string{"builder", "prune", "-f"}}
}

// parsePruneOutput extracts the count of removed items (non-empty, non-header
// lines) and the "Total reclaimed space: X" value from a docker prune stdout.
func parsePruneOutput(out string) (int, string) {
	removed := 0
	reclaimed := ""
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "Total reclaimed space:") {
			reclaimed = strings.TrimSpace(strings.TrimPrefix(t, "Total reclaimed space:"))
			continue
		}
		removed++
	}
	return removed, reclaimed
}

func execPrune(ctx context.Context, cmd pruneCmd) (int, string, error) {
	c := exec.CommandContext(ctx, "docker", cmd.Exec...)
	out, err := c.CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("docker %s: %w\n%s", strings.Join(cmd.Exec, " "), err, string(out))
	}
	removed, reclaimed := parsePruneOutput(string(out))
	return removed, reclaimed, nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	var report CleanupReport

	removed, reclaimed, err := execPrune(ctx, pruneContainerArgs())
	if err != nil {
		return report, fmt.Errorf("container prune: %w", err)
	}
	report.ContainersRemoved = removed
	report.SpaceReclaimed = reclaimed

	removed, reclaimed, err = execPrune(ctx, pruneNetworkArgs())
	if err != nil {
		return report, fmt.Errorf("network prune: %w", err)
	}
	report.NetworksRemoved = removed
	if reclaimed != "" && report.SpaceReclaimed == "" {
		report.SpaceReclaimed = reclaimed
	}

	removed, reclaimed, err = execPrune(ctx, pruneImageArgs(opts.All))
	if err != nil {
		return report, fmt.Errorf("image prune: %w", err)
	}
	report.ImagesRemoved = removed
	if reclaimed != "" {
		report.SpaceReclaimed = reclaimed
	}

	if opts.All {
		removed, reclaimed, err := execPrune(ctx, pruneBuilderArgs())
		if err != nil {
			return report, fmt.Errorf("builder prune: %w", err)
		}
		report.BuildCacheRemoved = removed
		if reclaimed != "" {
			report.SpaceReclaimed = reclaimed
		}
	}

	if opts.IncludeVolumes {
		removed, reclaimed, err := execPrune(ctx, pruneVolumeArgs())
		if err != nil {
			return report, fmt.Errorf("volume prune: %w", err)
		}
		report.VolumesRemoved = removed
		if reclaimed != "" {
			report.SpaceReclaimed = reclaimed
		}
	}

	return report, nil
}
