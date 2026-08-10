package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

const managedLabelPrefix = "tengiz-app="

func hasManagedLabel(labels string) bool {
	for _, part := range strings.Split(labels, ",") {
		if strings.HasPrefix(strings.TrimSpace(part), managedLabelPrefix) {
			return true
		}
	}
	return false
}

func selectUnmanagedStopped(entries []dockerPS) []string {
	var names []string
	for _, e := range entries {
		if e.State == "running" {
			continue
		}
		if hasManagedLabel(e.Labels) {
			continue
		}
		name := strings.TrimPrefix(e.Name, "/")
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func parseNameLines(out string) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names
}

func buildPSAllArgs() []string {
	return []string{"ps", "-a", "--format", "{{json .}}"}
}

func buildDanglingImagesArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func buildDanglingVolumesArgs() []string {
	return []string{"volume", "ls", "-f", "dangling=true", "--format", "{{.Name}}"}
}

func buildDanglingNetworksArgs() []string {
	return []string{"network", "ls", "-f", "dangling=true", "--format", "{{.Name}}"}
}

func buildBuilderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func (r *dockerRuntime) listStoppedUnmanagedContainers(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildPSAllArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps -a: %w\n%s", err, string(out))
	}
	var entries []dockerPS
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var entry dockerPS
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return selectUnmanagedStopped(entries), nil
}

func (r *dockerRuntime) listDanglingImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildDanglingImagesArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images dangling: %w\n%s", err, string(out))
	}
	return parseNameLines(string(out)), nil
}

func (r *dockerRuntime) listDanglingVolumes(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildDanglingVolumesArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	return parseNameLines(string(out)), nil
}

func (r *dockerRuntime) listDanglingNetworks(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildDanglingNetworksArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return parseNameLines(string(out)), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupSummary, error) {
	var summary CleanupSummary

	if opts.Containers {
		names, err := r.listStoppedUnmanagedContainers(ctx)
		if err != nil {
			return summary, err
		}
		for _, name := range names {
			if !opts.DryRun {
				if err := r.Remove(ctx, name); err != nil {
					return summary, err
				}
			}
			summary.ContainersRemoved = append(summary.ContainersRemoved, name)
		}
	}

	if opts.Images {
		ids, err := r.listDanglingImages(ctx)
		if err != nil {
			return summary, err
		}
		for _, id := range ids {
			if !opts.DryRun {
				if err := r.RemoveImage(ctx, id); err != nil {
					return summary, err
				}
			}
			summary.ImagesRemoved = append(summary.ImagesRemoved, id)
		}
	}

	if opts.Volumes {
		names, err := r.listDanglingVolumes(ctx)
		if err != nil {
			return summary, err
		}
		for _, name := range names {
			if !opts.DryRun {
				dcmd := exec.CommandContext(ctx, "docker", "volume", "rm", name)
				if out, err := dcmd.CombinedOutput(); err != nil {
					return summary, fmt.Errorf("docker volume rm %s: %w\n%s", name, err, string(out))
				}
			}
			summary.VolumesRemoved = append(summary.VolumesRemoved, name)
		}
	}

	if opts.Networks {
		names, err := r.listDanglingNetworks(ctx)
		if err != nil {
			return summary, err
		}
		for _, name := range names {
			if !opts.DryRun {
				dcmd := exec.CommandContext(ctx, "docker", "network", "rm", name)
				if out, err := dcmd.CombinedOutput(); err != nil {
					return summary, fmt.Errorf("docker network rm %s: %w\n%s", name, err, string(out))
				}
			}
			summary.NetworksRemoved = append(summary.NetworksRemoved, name)
		}
	}

	if opts.BuildCache && !opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", buildBuilderPruneArgs()...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return summary, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
		}
		summary.BuildCacheOutput = strings.TrimSpace(string(out))
	}

	return summary, nil
}
