package cleanup

import (
	"context"
	"log"
	"sort"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

const defaultRetention = 5

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	DryRun     bool
	Retention  int
}

type Result struct {
	DryRun            bool
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	RetentionApps     []string
	ReclaimedBytes    int64
}

type Cleaner struct {
	rt    runtime.Manager
	store *config.Store
}

func New(rt runtime.Manager, store *config.Store) *Cleaner {
	return &Cleaner{rt: rt, store: store}
}

func (c *Cleaner) Run(ctx context.Context, opts Options) *Result {
	if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
	}
	if opts.Retention <= 0 {
		opts.Retention = defaultRetention
	}

	res := &Result{DryRun: opts.DryRun}

	before, err := c.rt.DockerDiskInfo(ctx)
	if err != nil {
		log.Printf("[cleanup] warning: docker system df: %v", err)
	}

	if opts.DryRun {
		res.ReclaimedBytes = before.TotalReclaimBytes
		return res
	}

	if opts.Retention > 0 {
		apps, listErr := c.store.ListApps()
		if listErr == nil && len(apps) > 0 {
			sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
			for _, app := range apps {
				if keepErr := c.rt.KeepLastNImages(ctx, app.Name, opts.Retention); keepErr != nil {
					log.Printf("[cleanup] warning: retain images for %s: %v", app.Name, keepErr)
					continue
				}
				res.RetentionApps = append(res.RetentionApps, app.Name)
			}
		}
	}

	if opts.Containers {
		if n, opErr := c.rt.PruneContainers(ctx); opErr != nil {
			log.Printf("[cleanup] warning: container prune: %v", opErr)
		} else {
			res.ContainersRemoved = n
		}
	}
	if opts.Images {
		if n, opErr := c.rt.PruneImages(ctx); opErr != nil {
			log.Printf("[cleanup] warning: image prune: %v", opErr)
		} else {
			res.ImagesRemoved = n
		}
	}
	if opts.Volumes {
		if n, opErr := c.rt.PruneVolumes(ctx); opErr != nil {
			log.Printf("[cleanup] warning: volume prune: %v", opErr)
		} else {
			res.VolumesRemoved = n
		}
	}
	if opts.Networks {
		if n, opErr := c.rt.PruneNetworks(ctx); opErr != nil {
			log.Printf("[cleanup] warning: network prune: %v", opErr)
		} else {
			res.NetworksRemoved = n
		}
	}

	after, dfErr := c.rt.DockerDiskInfo(ctx)
	if dfErr == nil && after.TotalReclaimBytes < before.TotalReclaimBytes {
		res.ReclaimedBytes = before.TotalReclaimBytes - after.TotalReclaimBytes
	}
	return res
}
