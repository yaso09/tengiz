package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// CleanupOptions selects which Docker resource categories to clean.
type CleanupOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	KeepImages int  // most recent deployment images to keep per app (<=0 means 5)
	DryRun     bool // report what would be removed without removing anything
}

// CleanupResult lists what was (or, for DryRun, would be) removed.
type CleanupResult struct {
	Containers []string
	Images     []string
	Networks   []string
	Volumes    []string
	BuildCache bool
	DryRun     bool
}

// Empty reports whether nothing was (or would be) removed.
func (c CleanupResult) Empty() bool {
	return len(c.Containers) == 0 && len(c.Images) == 0 &&
		len(c.Networks) == 0 && len(c.Volumes) == 0 && !c.BuildCache
}

// Housekeeper is the host-level Docker maintenance capability. The docker
// runtime implements it; the CLI type-asserts a Manager to it for cleanup.
type Housekeeper interface {
	Prune(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
}

func pruneContainersArgs(dryRun bool) []string {
	if dryRun {
		return []string{"ps", "-a",
			"--filter", "status=exited",
			"--filter", "status=created",
			"--filter", "status=dead",
			"--filter", "label!=tengiz-app",
			"--format", "{{.Names}}",
		}
	}
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func pruneDanglingImagesArgs(dryRun bool) []string {
	if dryRun {
		return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
	}
	return []string{"image", "prune", "-f"}
}

func pruneNetworksArgs(dryRun bool) []string {
	if dryRun {
		return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
	}
	return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func pruneVolumesArgs(dryRun bool) []string {
	if dryRun {
		return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
	}
	return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
}

// tengizImageListArgs lists Tengiz app images with their creation time.
func tengizImageListArgs() []string {
	return []string{"images", "--filter", "reference=tengiz-apps/*", "--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}"}
}

// parsePruneItems extracts the removed-item lines from a docker prune command's
// stdout, skipping the "Deleted ..." headers and the "Total reclaimed space"
// summary line.
func parsePruneItems(out string) []string {
	var items []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Deleted ") || strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		items = append(items, line)
	}
	return items
}

// parseListLines extracts non-empty lines from a docker ls command's stdout.
func parseListLines(out string) []string {
	var items []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		items = append(items, line)
	}
	return items
}

// oldImageTags returns which tagged Tengiz app images to remove: for each app
// (image repository), all but the keepN most recent by creation time, skipping
// any tag ending in ":latest".
func oldImageTags(lines []string, keepN int) []string {
	if keepN <= 0 {
		keepN = 5
	}
	type imgEntry struct {
		tag     string
		created string
	}
	byRepo := make(map[string][]imgEntry)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		repoTag := parts[0]
		idx := strings.LastIndex(repoTag, ":")
		if idx < 0 || idx == len(repoTag)-1 {
			continue
		}
		repo, tag := repoTag[:idx], repoTag[idx+1:]
		byRepo[repo] = append(byRepo[repo], imgEntry{tag: tag, created: parts[1]})
	}
	var toRemove []string
	for repo, entries := range byRepo {
		if len(entries) <= keepN {
			continue
		}
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].created < entries[j].created
		})
		for i := 0; i < len(entries)-keepN; i++ {
			if entries[i].tag == "latest" {
				continue
			}
			toRemove = append(toRemove, repo+":"+entries[i].tag)
		}
	}
	sort.Strings(toRemove)
	return toRemove
}

// Prune removes the Docker resources selected by opts. In dry-run mode nothing
// is removed; the result reports what would be removed instead.
func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var result CleanupResult
	result.DryRun = opts.DryRun

	var err error
	if opts.Containers {
		result.Containers, err = r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return result, fmt.Errorf("containers: %w", err)
		}
	}
	if opts.Images {
		result.Images, err = r.pruneImages(ctx, opts.KeepImages, opts.DryRun)
		if err != nil {
			return result, fmt.Errorf("images: %w", err)
		}
	}
	if opts.Networks {
		result.Networks, err = r.pruneNetworks(ctx, opts.DryRun)
		if err != nil {
			return result, fmt.Errorf("networks: %w", err)
		}
	}
	if opts.Volumes {
		result.Volumes, err = r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return result, fmt.Errorf("volumes: %w", err)
		}
	}
	if opts.BuildCache {
		if !opts.DryRun {
			if err := r.pruneBuildCache(ctx); err != nil {
				return result, fmt.Errorf("build cache: %w", err)
			}
		}
		result.BuildCache = true
	}
	return result, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) ([]string, error) {
	args := pruneContainersArgs(dryRun)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	if dryRun {
		return parseListLines(string(out)), nil
	}
	return parsePruneItems(string(out)), nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, keepN int, dryRun bool) ([]string, error) {
	var removed []string

	args := pruneDanglingImagesArgs(dryRun)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	if dryRun {
		removed = append(removed, parseListLines(string(out))...)
	} else {
		removed = append(removed, parsePruneItems(string(out))...)
	}

	listOut, err := exec.CommandContext(ctx, "docker", tengizImageListArgs()...).CombinedOutput()
	if err != nil {
		return removed, fmt.Errorf("docker images: %w\n%s", err, string(listOut))
	}
	lines := strings.Split(strings.TrimSpace(string(listOut)), "\n")
	for _, tag := range oldImageTags(lines, keepN) {
		if dryRun {
			removed = append(removed, tag)
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			return removed, fmt.Errorf("remove old image %s: %w", tag, err)
		}
		removed = append(removed, tag)
	}
	return removed, nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) ([]string, error) {
	args := pruneNetworksArgs(dryRun)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	if dryRun {
		return parseListLines(string(out)), nil
	}
	return parsePruneItems(string(out)), nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) ([]string, error) {
	args := pruneVolumesArgs(dryRun)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	if dryRun {
		return parseListLines(string(out)), nil
	}
	return parsePruneItems(string(out)), nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-af")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}
