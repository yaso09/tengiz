package housekeeping

import (
	"context"
	"fmt"
)

func defaultOpts(opts Options) Options {
	if opts.Containers || opts.Images || opts.Volumes || opts.Networks {
		return opts
	}
	opts.Containers = true
	opts.Images = true
	opts.Volumes = true
	opts.Networks = true
	return opts
}

func (m *Manager) Run(ctx context.Context, opts Options) (*Result, error) {
	opts = defaultOpts(opts)
	res := &Result{}

	if opts.Containers {
		names, err := m.orphanContainers(ctx)
		if err != nil {
			return nil, fmt.Errorf("list containers: %w", err)
		}
		for _, name := range names {
			if !opts.DryRun {
				m.exec(ctx, "rm", "--force", name)
			}
			res.ContainersRemoved = append(res.ContainersRemoved, name)
		}
	}

	if opts.Images {
		ids, err := m.danglingImages(ctx)
		if err != nil {
			return nil, fmt.Errorf("list images: %w", err)
		}
		if len(ids) > 0 {
			if !opts.DryRun {
				m.exec(ctx, "image", "prune", "-f", "--filter", "dangling=true")
			}
			res.ImagesRemoved = ids
		}
	}

	if opts.Volumes {
		names, err := m.danglingVolumes(ctx)
		if err != nil {
			return nil, fmt.Errorf("list volumes: %w", err)
		}
		if len(names) > 0 {
			if !opts.DryRun {
				m.exec(ctx, "volume", "prune", "-f")
			}
			res.VolumesRemoved = names
		}
	}

	if opts.Networks {
		names, err := m.danglingNetworks(ctx)
		if err != nil {
			return nil, fmt.Errorf("list networks: %w", err)
		}
		if len(names) > 0 {
			if !opts.DryRun {
				m.exec(ctx, "network", "prune", "-f")
			}
			res.NetworksRemoved = names
		}
	}

	return res, nil
}
