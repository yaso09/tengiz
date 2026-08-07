package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// pruneAll reports whether opts requests every category (the default).
func (o CleanupOptions) pruneAll() bool {
	return !o.Containers && !o.Images && !o.Volumes && !o.BuildCache
}

// countPruned counts non-empty newline-separated lines in docker prune output.
func countPruned(out string) int {
	if strings.TrimSpace(out) == "" {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	n := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	// docker prune subcommands have no native --dry-run, so a dry run short
	// circuits before any destructive delete ever reaches the daemon.
	if opts.DryRun {
		return res, nil
	}

	all := opts.pruneAll()

	if all || opts.Containers {
		n, err := r.pruneContainers(ctx)
		if err != nil {
			return res, err
		}
		res.ContainersRemoved = n
	}

	if all || opts.Images {
		n, err := r.pruneImages(ctx)
		if err != nil {
			return res, err
		}
		res.ImagesRemoved = n
	}

	if all || opts.Volumes {
		n, err := r.pruneVolumes(ctx)
		if err != nil {
			return res, err
		}
		res.VolumesRemoved = n
	}

	if all || opts.BuildCache {
		n, err := r.pruneBuildCache(ctx)
		if err != nil {
			return res, err
		}
		res.BuildCacheReclaimed = n
	}

	return res, nil
}

// pruneContainers removes containers that are NOT labeled tengiz-app.
// label!=tengiz-app guarantees Tengiz-managed containers are never touched.
func (r *dockerRuntime) pruneContainers(ctx context.Context) (int, error) {
	args := []string{"container", "prune", "--force", "--filter", "label!=tengiz-app"}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return countPruned(string(out)), nil
}

// pruneImages removes all images not referenced by any container (-a prunes
// unused images too, not just dangling ones). Running containers always keep
// their images in use, so this never breaks a live app.
func (r *dockerRuntime) pruneImages(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f", "-a")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return countPruned(string(out)), nil
}

// pruneVolumes removes all volumes not in use by any container.
func (r *dockerRuntime) pruneVolumes(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return countPruned(string(out)), nil
}

// pruneBuildCache prunes builder cache (BuildKit). Reclaimed is reported in
// items (a rough count) since docker does not expose bytes via this path.
func (r *dockerRuntime) pruneBuildCache(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return countPruned(string(out)), nil
}
