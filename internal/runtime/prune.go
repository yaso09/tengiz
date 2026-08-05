package runtime

import "context"

func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
}
