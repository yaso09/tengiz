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

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult
	for _, category := range pruneCategories(opts) {
		args := cleanupCommandArgs(category)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return res, fmt.Errorf("docker %s prune: %w\n%s", category, err, string(out))
		}
		res.Output += string(out)
		removed := countPruneOutput(string(out))
		switch category {
		case "containers":
			res.ContainersRemoved = removed
		case "images":
			res.ImagesRemoved = removed
		case "build-cache":
			res.BuildCacheCleared = removed > 0
		case "volumes":
			res.VolumesRemoved = removed
		case "networks":
			res.NetworksRemoved = removed
		}
	}
	return res, nil
}

// pruneCategories returns the ordered list of enabled cleanup categories.
func pruneCategories(opts CleanupOptions) []string {
	var cats []string
	if opts.Containers {
		cats = append(cats, "containers")
	}
	if opts.Images {
		cats = append(cats, "images")
	}
	if opts.BuildCache {
		cats = append(cats, "build-cache")
	}
	if opts.Volumes {
		cats = append(cats, "volumes")
	}
	if opts.Networks {
		cats = append(cats, "networks")
	}
	return cats
}

// cleanupCommandArgs returns the docker argv for pruning a category.
//
// Containers are filtered by the tengiz-app label so only stopped
// Tengiz-managed containers are candidates. Images are pruned without
// -a, so only dangling (untagged) images are removed. Volumes and
// networks are removed only when unused by any container.
func cleanupCommandArgs(category string) []string {
	switch category {
	case "containers":
		return []string{"container", "prune", "-f", "--filter", fmt.Sprintf("label=%s", labelKey)}
	case "images":
		return []string{"image", "prune", "-f"}
	case "build-cache":
		return []string{"builder", "prune", "-f"}
	case "volumes":
		return []string{"volume", "prune", "-f"}
	case "networks":
		return []string{"network", "prune", "-f"}
	default:
		return nil
	}
}

// countPruneOutput counts the deleted items reported by a
// `docker <category> prune` invocation. The output looks like:
//
//	Deleted Containers:
//	f3b9c2e1a4d5
//	a1b2c3d4e5f6
//
//	Total reclaimed space: 1.234MB
//
// It skips empty lines, the section header (a line ending with ':'),
// and the "Total reclaimed space: ..." footer, then counts the rest.
func countPruneOutput(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "Total reclaimed space") {
			continue
		}
		if strings.HasSuffix(line, ":") {
			continue
		}
		count++
	}
	return count
}
