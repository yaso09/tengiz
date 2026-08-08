package runtime

import (
	"context"
)

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
}
