package runtime

import (
	"context"
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
