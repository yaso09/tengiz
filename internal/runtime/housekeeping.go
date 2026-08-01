package runtime

import (
	"context"
	"strings"
)

// PruneOptions selects which Docker resource categories are cleaned.
type PruneOptions struct {
	Containers bool
	Images     bool
	AllImages  bool
	Volumes    bool
	Networks   bool
	Cache      bool
}

// Any reports whether at least one category (or the AllImages modifier) is selected.
func (o PruneOptions) Any() bool {
	return o.Containers || o.Images || o.AllImages || o.Volumes || o.Networks || o.Cache
}

// PruneResult holds the raw `docker <object> prune` output per category.
type PruneResult struct {
	ContainerOutput string
	ImageOutput     string
	VolumeOutput    string
	NetworkOutput   string
	CacheOutput     string
}

// DryRunResult holds the count of items that would be removed per category.
type DryRunResult struct {
	Containers int
	Images     int
	Volumes    int
	Networks   int
	Cache      int
}

// Housekeeper manages Docker resource cleanup.
type Housekeeper interface {
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
	DryRun(ctx context.Context, opts PruneOptions) (DryRunResult, error)
	DiskUsage(ctx context.Context) (string, error)
}

// NewStubHousekeeper returns a no-op Housekeeper for tests.
func NewStubHousekeeper() Housekeeper {
	return &stubHousekeeper{}
}

type stubHousekeeper struct{}

func (h *stubHousekeeper) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}

func (h *stubHousekeeper) DryRun(ctx context.Context, opts PruneOptions) (DryRunResult, error) {
	return DryRunResult{}, nil
}

func (h *stubHousekeeper) DiskUsage(ctx context.Context) (string, error) {
	return "", nil
}

// tengizLabelFilter protects Tengiz-managed containers (labeled "tengiz-app")
// from being pruned. Docker's label!= filter matches resources WITHOUT the label.
const tengizLabelFilter = "label!=" + labelKey

func buildContainerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", tengizLabelFilter}
}

func buildImagePruneArgs(opts PruneOptions) []string {
	args := []string{"image", "prune", "-f"}
	if opts.AllImages {
		args = append(args, "-a")
	}
	return args
}

func buildVolumePruneArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", tengizLabelFilter}
}

func buildNetworkPruneArgs() []string {
	return []string{"network", "prune", "-f", "--filter", tengizLabelFilter}
}

func buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func buildContainerListArgs() []string {
	return []string{"ps", "-aq", "--filter", "status=exited", "--filter", "status=created", "--filter", tengizLabelFilter}
}

func buildImageListArgs(opts PruneOptions) []string {
	if opts.AllImages {
		return []string{"images", "-q"}
	}
	return []string{"images", "-q", "--filter", "dangling=true"}
}

func buildVolumeListArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "dangling=true", "--filter", tengizLabelFilter}
}

func buildNetworkListArgs() []string {
	return []string{"network", "ls", "-q", "--filter", "dangling=true", "--filter", tengizLabelFilter}
}

func buildCacheUsageArgs() []string {
	return []string{"builder", "du"}
}

func buildSystemDFArgs() []string {
	return []string{"system", "df"}
}

// countLines counts non-empty lines in command output.
func countLines(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

// pruneCommand pairs a resource category with the docker args that prune it.
type pruneCommand struct {
	kind string
	args []string
}

func pruneCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand
	if opts.Containers {
		cmds = append(cmds, pruneCommand{"containers", buildContainerPruneArgs()})
	}
	if opts.Images {
		cmds = append(cmds, pruneCommand{"images", buildImagePruneArgs(opts)})
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{"volumes", buildVolumePruneArgs()})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{"networks", buildNetworkPruneArgs()})
	}
	if opts.Cache {
		cmds = append(cmds, pruneCommand{"cache", buildCachePruneArgs()})
	}
	return cmds
}

func dryRunCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand
	if opts.Containers {
		cmds = append(cmds, pruneCommand{"containers", buildContainerListArgs()})
	}
	if opts.Images {
		cmds = append(cmds, pruneCommand{"images", buildImageListArgs(opts)})
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{"volumes", buildVolumeListArgs()})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{"networks", buildNetworkListArgs()})
	}
	if opts.Cache {
		cmds = append(cmds, pruneCommand{"cache", buildCacheUsageArgs()})
	}
	return cmds
}
