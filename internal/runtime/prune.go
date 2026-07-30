package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

func (r *dockerRuntime) PruneContainers(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
	args := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if cfg != nil && cfg.ContainerMaxAge != "" {
		args = append(args, "--filter", fmt.Sprintf("until=%s", cfg.ContainerMaxAge))
	}
	return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneImages(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
	args := []string{"image", "prune", "-f", "--filter", "label!=tengiz-app"}
	if cfg != nil && cfg.ImageMaxAge != "" {
		args = append(args, "--filter", fmt.Sprintf("until=%s", cfg.ImageMaxAge))
	}
	report, err := r.execPrune(ctx, args)
	if err != nil {
		return report, err
	}
	if cfg != nil && cfg.PruneDanglingOnly == false {
		argsAll := []string{"image", "prune", "-f", "-a", "--filter", "label!=tengiz-app"}
		if cfg.ImageMaxAge != "" {
			argsAll = append(argsAll, "--filter", fmt.Sprintf("until=%s", cfg.ImageMaxAge))
		}
		reportAll, errAll := r.execPrune(ctx, argsAll)
		if errAll == nil {
			report.ItemsRemoved += reportAll.ItemsRemoved
			report.ReclaimedBytes += reportAll.ReclaimedBytes
			report.Details = append(report.Details, reportAll.Details...)
		}
	}
	return report, nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
	args := []string{"volume", "prune", "-f"}
	if cfg != nil && cfg.VolumeMaxAge != "" {
		args = append(args, "--filter", fmt.Sprintf("until=%s", cfg.VolumeMaxAge))
	}
	return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
	args := []string{"network", "prune", "-f"}
	if cfg != nil && cfg.NetworkMaxAge != "" {
		args = append(args, "--filter", fmt.Sprintf("until=%s", cfg.NetworkMaxAge))
	}
	return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
	args := []string{"builder", "prune", "-f"}
	if cfg != nil && cfg.BuildCacheMaxAge != "" {
		args = append(args, "--filter", fmt.Sprintf("until=%s", cfg.BuildCacheMaxAge))
	}
	if cfg != nil && cfg.KeepBuildCacheBytes != "" {
		args = append(args, "--keep-storage", cfg.KeepBuildCacheBytes)
	}
	report, err := r.execPrune(ctx, args)
	if err != nil {
		return report, err
	}
	return report, nil
}

func (r *dockerRuntime) execPrune(ctx context.Context, args []string) (PruneReport, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	output := strings.TrimSpace(stdout.String())
	report := PruneReport{}

	if output != "" {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "Total reclaimed space:") {
				reclaimed := strings.TrimPrefix(line, "Total reclaimed space:")
				reclaimed = strings.TrimSpace(reclaimed)
				report.ReclaimedBytes = parseReclaimedBytes(reclaimed)
			} else {
				report.ItemsRemoved++
				report.Details = append(report.Details, line)
			}
		}
	}

	if err != nil {
		return report, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return report, nil
}

func parseReclaimedBytes(s string) uint64 {
	parts := strings.SplitN(s, " ", 2)
	if len(parts) < 2 {
		return 0
	}
	val, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToLower(strings.TrimSpace(parts[1]))
	switch unit {
	case "b":
		return uint64(val)
	case "kb", "kib":
		return uint64(val * 1024)
	case "mb", "mib":
		return uint64(val * 1024 * 1024)
	case "gb", "gib":
		return uint64(val * 1024 * 1024 * 1024)
	default:
		return uint64(val)
	}
}
