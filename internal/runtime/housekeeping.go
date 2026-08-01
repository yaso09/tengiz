package runtime

import (
	"context"
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
