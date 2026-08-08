package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func containerListArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
		"--format", "{{.ID}}",
	}
}

func containerPruneArgs() []string {
	return []string{
		"container", "prune", "--force",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
}

func imagePruneArgs() []string {
	// Dangling-only: never -a, so tagged rollback images survive.
	return []string{"image", "prune", "--force"}
}

func imageListArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "--force"}
}

func volumeListArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "--force"}
}

func cachePruneArgs() []string {
	return []string{"builder", "prune", "--force"}
}

func systemPruneDryRunArgs() []string {
	return []string{"system", "prune", "--all", "--volumes", "--dry-run"}
}

func findReclaimed(output string) string {
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(strings.ToLower(line), "reclaimed") {
			return line
		}
	}
	return ""
}

func (r *dockerRuntime) dockerOut(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

func (r *dockerRuntime) countLines(ctx context.Context, args ...string) (int, error) {
	out, err := r.dockerOut(ctx, args...)
	if err != nil {
		return 0, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n, nil
}

func (r *dockerRuntime) pruneCounted(ctx context.Context, listArgs, pruneArgs []string) (int, string, error) {
	before, err := r.countLines(ctx, listArgs...)
	if err != nil {
		return 0, "", err
	}
	out, err := r.dockerOut(ctx, pruneArgs...)
	if err != nil {
		return 0, "", fmt.Errorf("docker %s: %w\n%s", strings.Join(pruneArgs, " "), err, string(out))
	}
	after, err := r.countLines(ctx, listArgs...)
	if err != nil {
		return 0, "", err
	}
	removed := before - after
	if removed < 0 {
		removed = 0
	}
	return removed, findReclaimed(string(out)), nil
}

func (r *dockerRuntime) countNetworks(ctx context.Context) (int, error) {
	out, err := r.dockerOut(ctx, "network", "ls", "--format", "{{.Name}}")
	if err != nil {
		return 0, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	n := 0
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name = strings.TrimSpace(name)
		switch name {
		case "", "bridge", "host", "none":
			continue
		}
		n++
	}
	return n, nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	var rep CleanupReport

	if opts.DryRun {
		out, err := r.dockerOut(ctx, systemPruneDryRunArgs()...)
		if err != nil {
			return rep, fmt.Errorf("docker system prune --dry-run: %w\n%s", err, string(out))
		}
		rep.DryRun = string(out)
		return rep, nil
	}

	var reclaimed []string

	if opts.Containers {
		n, sum, err := r.pruneCounted(ctx, containerListArgs(), containerPruneArgs())
		if err != nil {
			return rep, err
		}
		rep.ContainersRemoved = n
		if sum != "" {
			reclaimed = append(reclaimed, sum)
		}
	}
	if opts.Images {
		n, sum, err := r.pruneCounted(ctx, imageListArgs(), imagePruneArgs())
		if err != nil {
			return rep, err
		}
		rep.ImagesRemoved = n
		if sum != "" {
			reclaimed = append(reclaimed, sum)
		}
	}
	if opts.Volumes {
		n, sum, err := r.pruneCounted(ctx, volumeListArgs(), volumePruneArgs())
		if err != nil {
			return rep, err
		}
		rep.VolumesRemoved = n
		if sum != "" {
			reclaimed = append(reclaimed, sum)
		}
	}
	if opts.Networks {
		before, err := r.countNetworks(ctx)
		if err != nil {
			return rep, err
		}
		out, err := r.dockerOut(ctx, networkPruneArgs()...)
		if err != nil {
			return rep, fmt.Errorf("docker %s: %w\n%s", strings.Join(networkPruneArgs(), " "), err, string(out))
		}
		after, err := r.countNetworks(ctx)
		if err != nil {
			return rep, err
		}
		removed := before - after
		if removed < 0 {
			removed = 0
		}
		rep.NetworksRemoved = removed
		if sum := findReclaimed(string(out)); sum != "" {
			reclaimed = append(reclaimed, sum)
		}
	}
	if opts.Cache {
		out, err := r.dockerOut(ctx, cachePruneArgs()...)
		if err != nil {
			return rep, fmt.Errorf("docker %s: %w\n%s", strings.Join(cachePruneArgs(), " "), err, string(out))
		}
		if sum := findReclaimed(string(out)); sum != "" {
			reclaimed = append(reclaimed, sum)
		}
	}

	rep.Reclaimed = strings.Join(reclaimed, ", ")
	return rep, nil
}