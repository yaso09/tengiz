package runtime

import "context"

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	return &PruneReport{DryRun: opts.DryRun}, nil
}
