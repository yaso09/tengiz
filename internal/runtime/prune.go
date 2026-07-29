package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	report := &PruneReport{}

	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "system", "df")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		fmt.Print(string(out))
		return report, nil
	}

	if opts.All {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
	}

	if opts.Containers {
		out, err := r.pruneContainers(ctx)
		if err != nil {
			return report, fmt.Errorf("prune containers: %w", err)
		}
		report.ContainersReclaimed = parseDockerPruneOutput(out)
	}

	if opts.Images {
		out, err := r.pruneImages(ctx)
		if err != nil {
			return report, fmt.Errorf("prune images: %w", err)
		}
		report.ImagesReclaimed = parseDockerPruneOutput(out)
	}

	if opts.Volumes {
		out, err := r.pruneVolumes(ctx)
		if err != nil {
			return report, fmt.Errorf("prune volumes: %w", err)
		}
		report.VolumesReclaimed = parseDockerPruneOutput(out)
	}

	if opts.Networks {
		out, err := r.pruneNetworks(ctx)
		if err != nil {
			return report, fmt.Errorf("prune networks: %w", err)
		}
		_ = out
	}

	if opts.BuildCache {
		out, err := r.pruneBuildCache(ctx)
		if err != nil {
			return report, fmt.Errorf("prune build cache: %w", err)
		}
		report.BuildCacheReclaimed = parseDockerPruneOutput(out)
	}

	report.TotalReclaimed = report.ContainersReclaimed + report.ImagesReclaimed +
		report.VolumesReclaimed + report.BuildCacheReclaimed

	return report, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context) ([]byte, error) {
	args := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s", string(out))
	}
	return out, nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context) ([]byte, error) {
	args := []string{"image", "prune", "-a", "-f", "--filter", "label!=tengiz-app"}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s", string(out))
	}
	return out, nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context) ([]byte, error) {
	args := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s", string(out))
	}
	return out, nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context) ([]byte, error) {
	args := []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s", string(out))
	}
	return out, nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) ([]byte, error) {
	args := []string{"buildx", "prune", "-f"}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s", string(out))
	}
	return out, nil
}

var spaceRe = []byte("Total reclaimed space:")

func parseDockerPruneOutput(out []byte) int64 {
	lines := bytes.Split(out, []byte("\n"))
	for _, line := range lines {
		idx := bytes.Index(line, spaceRe)
		if idx < 0 {
			continue
		}
		rest := string(line[idx+len(spaceRe):])
		rest = strings.TrimSpace(rest)
		parts := strings.Fields(rest)
		if len(parts) < 2 {
			continue
		}
		val, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			continue
		}
		unit := parts[1]
		return convertToBytes(val, unit)
	}
	return 0
}

func convertToBytes(val float64, unit string) int64 {
	switch unit {
	case "B":
		return int64(val)
	case "kB":
		return int64(val * 1000)
	case "MB":
		return int64(val * 1000 * 1000)
	case "GB":
		return int64(val * 1000 * 1000 * 1000)
	case "TB":
		return int64(val * 1000 * 1000 * 1000 * 1000)
	case "KiB":
		return int64(val * 1024)
	case "MiB":
		return int64(val * 1024 * 1024)
	case "GiB":
		return int64(val * 1024 * 1024 * 1024)
	case "TiB":
		return int64(val * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(val)
	}
}
