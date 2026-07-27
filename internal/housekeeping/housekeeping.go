package housekeeping

import (
	"context"
	"fmt"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type CleanupOptions struct {
	DryRun     bool
	All        bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	AppName    string
}

type categoryReport struct {
	Category string
	Stats    runtime.PruneStats
}

type CleanupReport struct {
	DryRun  bool
	Reports []categoryReport
}

func (r *CleanupReport) ItemsRemoved() int {
	total := 0
	for _, cr := range r.Reports {
		total += cr.Stats.ItemsRemoved
	}
	return total
}

type appLister interface {
	ListApps() ([]types.AppEntry, error)
}

type Housekeeper struct {
	rt    runtime.Manager
	store appLister
}

func New(rt runtime.Manager, store appLister) *Housekeeper {
	return &Housekeeper{rt: rt, store: store}
}

func (h *Housekeeper) Run(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	categories := h.resolveCategories(opts)
	var report CleanupReport

	for _, cat := range categories {
		if opts.DryRun {
			report.Reports = append(report.Reports, categoryReport{
				Category: cat,
				Stats:    runtime.PruneStats{},
			})
			continue
		}

		var stats runtime.PruneStats
		var err error

		switch cat {
		case "containers":
			stats, err = h.rt.PruneContainers(ctx)
		case "images":
			stats, err = h.rt.PruneImages(ctx, opts.All)
		case "volumes":
			stats, err = h.rt.PruneVolumes(ctx)
		case "networks":
			stats, err = h.rt.PruneNetworks(ctx)
		case "build-cache":
			stats, err = h.rt.PruneBuildCache(ctx)
		}

		if err != nil {
			return nil, fmt.Errorf("prune %s: %w", cat, err)
		}
		report.Reports = append(report.Reports, categoryReport{Category: cat, Stats: stats})
	}

	report.DryRun = opts.DryRun
	return &report, nil
}

func (h *Housekeeper) DryRun(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	opts.DryRun = true
	return h.Run(ctx, opts)
}

func (h *Housekeeper) resolveCategories(opts CleanupOptions) []string {
	if opts.All {
		return []string{"containers", "images", "volumes", "networks", "build-cache"}
	}

	var cats []string
	if opts.Containers {
		cats = append(cats, "containers")
	}
	if opts.Images {
		cats = append(cats, "images")
	}
	if opts.Volumes {
		cats = append(cats, "volumes")
	}
	if opts.Networks {
		cats = append(cats, "networks")
	}
	if opts.BuildCache {
		cats = append(cats, "build-cache")
	}

	if len(cats) == 0 {
		cats = []string{"containers", "images"}
	}
	return cats
}
