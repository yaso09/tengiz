package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CleanOptions selects which categories of unused Docker resources to clean.
type CleanOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	Cache      bool
	DryRun     bool
}

// CleanItem is a single resource that was (or would be) removed.
type CleanItem struct {
	Kind string
	ID   string
}

// CleanResult summarizes a cleanup run.
type CleanResult struct {
	Items  []CleanItem
	DryRun bool
}

func (r *CleanResult) add(kind, id string) {
	r.Items = append(r.Items, CleanItem{Kind: kind, ID: id})
}

// Clean removes unused Docker resources. Tengiz-managed containers (labeled
// tengiz-app) and volumes mounted by apps are never touched. In dry-run mode
// it only reports what would be removed.
func (r *dockerRuntime) Clean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	result := CleanResult{DryRun: opts.DryRun}

	if opts.Containers {
		ids, err := r.orphanStoppedContainers(ctx)
		if err != nil {
			return result, err
		}
		for _, id := range ids {
			result.add("container", id)
			if opts.DryRun {
				continue
			}
			if err := r.removeResource(ctx, "rm", "-f", id); err != nil {
				return result, err
			}
		}
	}

	if opts.Images {
		ids, err := r.danglingImages(ctx)
		if err != nil {
			return result, err
		}
		for _, id := range ids {
			result.add("image", id)
			if opts.DryRun {
				continue
			}
			if err := r.removeResource(ctx, "rmi", "-f", id); err != nil {
				return result, err
			}
		}
	}

	if opts.Volumes {
		ids, err := r.danglingVolumes(ctx)
		if err != nil {
			return result, err
		}
		for _, id := range ids {
			result.add("volume", id)
			if opts.DryRun {
				continue
			}
			if err := r.removeResource(ctx, "volume", "rm", id); err != nil {
				return result, err
			}
		}
	}

	if opts.Networks {
		ids, err := r.danglingNetworks(ctx)
		if err != nil {
			return result, err
		}
		for _, id := range ids {
			result.add("network", id)
			if opts.DryRun {
				continue
			}
			if err := r.removeResource(ctx, "network", "rm", id); err != nil {
				return result, err
			}
		}
	}

	if opts.Cache {
		result.add("cache", "build-cache")
		if !opts.DryRun {
			if err := r.removeResource(ctx, "builder", "prune", "-f"); err != nil {
				return result, err
			}
		}
	}

	return result, nil
}

func (r *dockerRuntime) orphanStoppedContainers(ctx context.Context) ([]string, error) {
	out, err := r.dockerOutput(ctx, "ps", "-a", "-q",
		"--filter", "status=exited",
		"--filter", "status=created")
	if err != nil {
		return nil, err
	}
	protected, err := r.dockerOutput(ctx, "ps", "-a", "-q",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--filter", "label=tengiz-app")
	if err != nil {
		return nil, err
	}
	return excludeIDs(parseIDList(out), parseIDList(protected)), nil
}

// excludeIDs returns ids with any id present in excluded removed.
func excludeIDs(ids, excluded []string) []string {
	set := make(map[string]struct{}, len(excluded))
	for _, id := range excluded {
		set[id] = struct{}{}
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := set[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}

func (r *dockerRuntime) danglingImages(ctx context.Context) ([]string, error) {
	out, err := r.dockerOutput(ctx, "images", "--filter", "dangling=true", "-q")
	if err != nil {
		return nil, err
	}
	return parseIDList(out), nil
}

func (r *dockerRuntime) danglingVolumes(ctx context.Context) ([]string, error) {
	out, err := r.dockerOutput(ctx, "volume", "ls", "--filter", "dangling=true", "-q")
	if err != nil {
		return nil, err
	}
	return parseIDList(out), nil
}

func (r *dockerRuntime) danglingNetworks(ctx context.Context) ([]string, error) {
	out, err := r.dockerOutput(ctx, "network", "ls", "--filter", "dangling=true", "-q")
	if err != nil {
		return nil, err
	}
	return parseIDList(out), nil
}

func (r *dockerRuntime) dockerOutput(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return out, nil
}

func (r *dockerRuntime) removeResource(ctx context.Context, args ...string) error {
	full := append([]string{"docker"}, args...)
	cmd := exec.CommandContext(ctx, full[0], full[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", strings.Join(full, " "), err, string(out))
	}
	return nil
}

func parseIDList(out []byte) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids
}
