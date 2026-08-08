package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= n {
		return nil
	}

	sort.Slice(lines, func(i, j int) bool {
		partsI := strings.SplitN(lines[i], "|", 2)
		partsJ := strings.SplitN(lines[j], "|", 2)
		if len(partsI) < 2 || len(partsJ) < 2 {
			return false
		}
		return partsI[1] < partsJ[1]
	})

	for i := 0; i < len(lines)-n; i++ {
		parts := strings.SplitN(lines[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") {
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
		}
	}
	return nil
}

type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCacheRemoved int
}

type PruneTarget struct {
	Resource string
	Filters  []string
}

func (t PruneTarget) Args() []string {
	return pruneArgs(t.Resource, t.Filters)
}

func PruneTargets(opts CleanupOptions) []PruneTarget {
	var targets []PruneTarget
	if opts.Containers {
		targets = append(targets, PruneTarget{Resource: "container", Filters: []string{"label=tengiz-deployment"}})
	}
	if opts.Images {
		targets = append(targets, PruneTarget{Resource: "image", Filters: []string{"dangling=true"}})
	}
	if opts.Volumes {
		targets = append(targets, PruneTarget{Resource: "volume"})
	}
	if opts.Networks {
		targets = append(targets, PruneTarget{Resource: "network"})
	}
	if opts.BuildCache {
		targets = append(targets, PruneTarget{Resource: "builder"})
	}
	return targets
}

func pruneArgs(resource string, filters []string) []string {
	args := []string{resource, "prune", "-f"}
	for _, f := range filters {
		args = append(args, "--filter", f)
	}
	return args
}

func countRemoved(output string) int {
	count := 0
	inSection := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Deleted ") && strings.HasSuffix(trimmed, ":") {
			inSection = true
			continue
		}
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			inSection = false
			continue
		}
		if inSection && !strings.HasPrefix(trimmed, "untagged") {
			count++
		}
	}
	return count
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	res := &CleanupResult{}
	if opts.Containers {
		n, err := r.prune(ctx, PruneTarget{Resource: "container", Filters: []string{"label=tengiz-deployment"}})
		if err != nil {
			return res, err
		}
		res.ContainersRemoved = n
	}
	if opts.Images {
		n, err := r.prune(ctx, PruneTarget{Resource: "image", Filters: []string{"dangling=true"}})
		if err != nil {
			return res, err
		}
		res.ImagesRemoved = n
	}
	if opts.Volumes {
		n, err := r.prune(ctx, PruneTarget{Resource: "volume"})
		if err != nil {
			return res, err
		}
		res.VolumesRemoved = n
	}
	if opts.Networks {
		n, err := r.prune(ctx, PruneTarget{Resource: "network"})
		if err != nil {
			return res, err
		}
		res.NetworksRemoved = n
	}
	if opts.BuildCache {
		n, err := r.prune(ctx, PruneTarget{Resource: "builder"})
		if err != nil {
			return res, err
		}
		res.BuildCacheRemoved = n
	}
	return res, nil
}

func (r *dockerRuntime) prune(ctx context.Context, t PruneTarget) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", t.Args()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker %s prune: %w\n%s", t.Resource, err, string(out))
	}
	return countRemoved(string(out)), nil
}
