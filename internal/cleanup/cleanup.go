package cleanup

import (
	"context"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Pruner interface {
	Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error)
}

type Options struct {
	AllImages bool
	Volumes   bool
	DryRun    bool
}

type Result struct {
	ReclaimedSpace string
	DryRun         bool
	Commands       [][]string
}

type Manager struct {
	pruner Pruner
}

func New(pruner Pruner) *Manager {
	return &Manager{pruner: pruner}
}

func (m *Manager) Prune(ctx context.Context, opts Options) (*Result, error) {
	res, err := m.pruner.Prune(ctx, runtime.PruneOptions{
		All:     opts.AllImages,
		Volumes: opts.Volumes,
		DryRun:  opts.DryRun,
	})
	if err != nil {
		return nil, err
	}
	return &Result{
		ReclaimedSpace: res.ReclaimedSpace,
		DryRun:         res.DryRun,
		Commands:       res.Commands,
	}, nil
}
