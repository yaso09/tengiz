package runtime

import "context"

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
