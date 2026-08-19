package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

var reclaimedSpaceRe = regexp.MustCompile(`Total reclaimed space:\s*(.+)$`)

func buildPruneArgs(cat types.CleanupCategory) []string {
	switch cat {
	case types.CleanupContainers:
		return []string{"container", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey)}
	case types.CleanupImages:
		return []string{"image", "prune", "-f", "-a", "--filter", fmt.Sprintf("label!=%s", types.ManagedImageLabel)}
	case types.CleanupVolumes:
		return []string{"volume", "prune", "-f"}
	case types.CleanupNetworks:
		return []string{"network", "prune", "-f"}
	case types.CleanupBuildCache:
		return []string{"builder", "prune", "-f"}
	default:
		return nil
	}
}

func buildDiskUsageArgs() []string {
	return []string{"system", "df"}
}

func cleanupCategories(opts types.PruneOptions) []types.CleanupCategory {
	var cats []types.CleanupCategory
	if opts.Containers {
		cats = append(cats, types.CleanupContainers)
	}
	if opts.Images {
		cats = append(cats, types.CleanupImages)
	}
	if opts.Volumes {
		cats = append(cats, types.CleanupVolumes)
	}
	if opts.Networks {
		cats = append(cats, types.CleanupNetworks)
	}
	if opts.BuildCache {
		cats = append(cats, types.CleanupBuildCache)
	}
	return cats
}

func extractReclaimedSpace(output string) (string, bool) {
	m := reclaimedSpaceRe.FindStringSubmatch(strings.TrimSpace(output))
	if len(m) < 2 {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

func summarizeReclaimed(spaces []string) string {
	seen := make(map[string]bool)
	var parts []string
	for _, s := range spaces {
		if s != "" && !seen[s] {
			seen[s] = true
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " + ")
}

func appendDetail(detail []string, out string) []string {
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			detail = append(detail, l)
		}
	}
	return detail
}

func (r *dockerRuntime) Prune(ctx context.Context, opts types.PruneOptions) (types.PruneResult, error) {
	cats := cleanupCategories(opts)
	if len(cats) == 0 {
		return types.PruneResult{}, fmt.Errorf("no cleanup categories selected")
	}

	var result types.PruneResult
	var reclaimed []string

	for _, cat := range cats {
		cmd := exec.CommandContext(ctx, "docker", buildPruneArgs(cat)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return result, fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out))
		}
		result.Categories = append(result.Categories, cat)
		result.Detail = appendDetail(result.Detail, string(out))
		if space, ok := extractReclaimedSpace(string(out)); ok {
			reclaimed = append(reclaimed, space)
		}
	}

	result.TotalReclaimed = summarizeReclaimed(reclaimed)
	return result, nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildDiskUsageArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
