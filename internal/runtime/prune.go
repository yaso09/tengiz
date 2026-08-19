package runtime

import (
	"fmt"
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
