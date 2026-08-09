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

type PruneCategory string

const (
	PruneContainers PruneCategory = "containers"
	PruneImages     PruneCategory = "images"
	PruneVolumes    PruneCategory = "volumes"
	PruneNetworks   PruneCategory = "networks"
	PruneBuildCache PruneCategory = "build-cache"
)

type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type PruneResult struct {
	Category  PruneCategory
	DryRun    bool
	Args      []string
	Reclaimed string
	Err       error
}

func activeCategories(opts PruneOptions) []PruneCategory {
	categories := []PruneCategory{}
	if opts.Containers {
		categories = append(categories, PruneContainers)
	}
	if opts.Images {
		categories = append(categories, PruneImages)
	}
	if opts.Volumes {
		categories = append(categories, PruneVolumes)
	}
	if opts.Networks {
		categories = append(categories, PruneNetworks)
	}
	if opts.BuildCache {
		categories = append(categories, PruneBuildCache)
	}
	return categories
}

func pruneArgs(category PruneCategory) []string {
	switch category {
	case PruneContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case PruneImages:
		return []string{"image", "prune", "-f"}
	case PruneVolumes:
		return []string{"volume", "prune", "-f"}
	case PruneNetworks:
		return []string{"network", "prune", "-f"}
	case PruneBuildCache:
		return []string{"builder", "prune", "-f"}
	default:
		return nil
	}
}

func parseReclaimed(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) ([]PruneResult, error) {
	var results []PruneResult
	for _, category := range activeCategories(opts) {
		args := pruneArgs(category)
		result := PruneResult{Category: category, DryRun: opts.DryRun, Args: args}
		if opts.DryRun {
			result.Reclaimed = "(dry run)"
			results = append(results, result)
			continue
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			result.Err = fmt.Errorf("docker %s: %w\n%s", category, err, string(out))
			results = append(results, result)
			continue
		}
		result.Reclaimed = parseReclaimed(string(out))
		results = append(results, result)
	}
	return results, nil
}
