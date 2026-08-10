package runtime

import "context"

func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupSummary, error) {
	return CleanupSummary{}, nil
}
