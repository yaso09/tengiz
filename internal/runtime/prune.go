package runtime

import "context"

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
