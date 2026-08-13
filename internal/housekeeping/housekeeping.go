package housekeeping

import (
	"context"
	"fmt"
	"log"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Options struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	Keep       int
}

type Summary struct {
	Containers int
	Dangling   int
	OldImages  int
	Volumes    int
	Networks   int
	BuildCache string
	DiskUsage  string
	DryRun     bool
}

type Cleaner struct {
	rt runtime.Cleaner
}

func New(rt runtime.Cleaner) *Cleaner {
	return &Cleaner{rt: rt}
}

func (c *Cleaner) Run(ctx context.Context, opts Options) (Summary, error) {
	keep := opts.Keep
	if keep <= 0 {
		keep = 5
	}
	if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.BuildCache {
		opts.Containers, opts.Images, opts.Volumes, opts.Networks, opts.BuildCache = true, true, true, true, true
	}

	var s Summary
	s.DryRun = opts.DryRun

	if opts.Containers {
		all, err := c.rt.ListAllContainers(ctx)
		if err != nil {
			return s, fmt.Errorf("list containers: %w", err)
		}
		targets := selectHelperContainers(all)
		if opts.DryRun {
			s.Containers = len(targets)
		} else if len(targets) > 0 {
			n, err := c.rt.RemoveContainers(ctx, targets)
			if err != nil {
				return s, fmt.Errorf("remove helper containers: %w", err)
			}
			s.Containers = n
		}
	}

	if opts.Images {
		imgs, err := c.rt.ListImages(ctx)
		if err != nil {
			return s, fmt.Errorf("list images: %w", err)
		}
		dangling := selectDanglingImages(imgs)
		old := selectOldAppImages(imgs, keep)
		if opts.DryRun {
			s.Dangling = len(dangling)
			s.OldImages = len(old)
		} else {
			if len(dangling) > 0 {
				n, err := c.rt.RemoveImages(ctx, dangling)
				if err != nil {
					return s, fmt.Errorf("remove dangling images: %w", err)
				}
				s.Dangling = n
			}
			if len(old) > 0 {
				n, err := c.rt.RemoveImages(ctx, old)
				if err != nil {
					return s, fmt.Errorf("remove old app images: %w", err)
				}
				s.OldImages = n
			}
		}
	}

	if opts.Volumes {
		names, err := c.rt.ListDanglingVolumes(ctx)
		if err != nil {
			return s, fmt.Errorf("list volumes: %w", err)
		}
		if opts.DryRun {
			s.Volumes = len(names)
		} else if len(names) > 0 {
			n, err := c.rt.RemoveVolumes(ctx, names)
			if err != nil {
				return s, fmt.Errorf("remove unused volumes: %w", err)
			}
			s.Volumes = n
		}
	}

	if opts.Networks {
		ids, err := c.rt.ListDanglingNetworks(ctx)
		if err != nil {
			return s, fmt.Errorf("list networks: %w", err)
		}
		if opts.DryRun {
			s.Networks = len(ids)
		} else if len(ids) > 0 {
			n, err := c.rt.RemoveNetworks(ctx, ids)
			if err != nil {
				return s, fmt.Errorf("remove unused networks: %w", err)
			}
			s.Networks = n
		}
	}

	if opts.BuildCache {
		if opts.DryRun {
			size, err := c.rt.BuildCacheSize(ctx)
			if err != nil {
				return s, fmt.Errorf("build cache size: %w", err)
			}
			s.BuildCache = size
		} else {
			reclaimed, err := c.rt.PruneBuildCache(ctx)
			if err != nil {
				return s, fmt.Errorf("prune build cache: %w", err)
			}
			s.BuildCache = reclaimed
		}
	}

	usage, err := c.rt.DiskUsage(ctx)
	if err != nil {
		log.Printf("[housekeeping] disk usage: %v", err)
	} else {
		s.DiskUsage = usage
	}
	return s, nil
}
