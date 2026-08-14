package runtime

import (
	"context"
	"strings"
)

type PruneCategory string

const (
	PruneContainers PruneCategory = "containers"
	PruneImages     PruneCategory = "images"
	PruneVolumes    PruneCategory = "volumes"
	PruneNetworks   PruneCategory = "networks"
	PruneBuildCache PruneCategory = "build-cache"
)

var AllPruneCategories = []PruneCategory{
	PruneContainers, PruneImages, PruneVolumes, PruneNetworks, PruneBuildCache,
}

type PruneOptions struct {
	Categories []PruneCategory
	AllImages  bool
	DryRun     bool
}

type PruneResult struct {
	Category  PruneCategory
	Reclaimed string
	Err       error
}

type PruneReport struct {
	DryRun  bool
	Results []PruneResult
	DfRows  []SystemDfRow
}

type SystemDfRow struct {
	Type        string
	Total       string
	Active      string
	Size        string
	Reclaimable string
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}

func pruneContainerArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label=" + labelKey}
}

func pruneImageArgs(all bool) []string {
	if all {
		return []string{"image", "prune", "-a", "-f"}
	}
	return []string{"image", "prune", "-f"}
}

func pruneVolumeArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func pruneNetworkArgs() []string {
	return []string{"network", "prune", "-f"}
}

func pruneBuildCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func systemDfArgs() []string {
	return []string{"system", "df", "--format", "{{.Type}}\t{{.Total}}\t{{.Active}}\t{{.Size}}\t{{.Reclaimable}}"}
}

func pruneCommandArgs(cat PruneCategory, allImages bool) ([]string, bool) {
	switch cat {
	case PruneContainers:
		return pruneContainerArgs(), true
	case PruneImages:
		return pruneImageArgs(allImages), true
	case PruneVolumes:
		return pruneVolumeArgs(), true
	case PruneNetworks:
		return pruneNetworkArgs(), true
	case PruneBuildCache:
		return pruneBuildCacheArgs(), true
	default:
		return nil, false
	}
}

func parseReclaimed(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
		if strings.HasPrefix(line, "Total:") && strings.Contains(line, "Build Cache:") {
			parts := strings.SplitN(line, "Build Cache:", 2)
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func parseSystemDf(output string) []SystemDfRow {
	var rows []SystemDfRow
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) != 5 {
			continue
		}
		rows = append(rows, SystemDfRow{Type: parts[0], Total: parts[1], Active: parts[2], Size: parts[3], Reclaimable: parts[4]})
	}
	return rows
}
