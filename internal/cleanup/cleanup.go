package cleanup

import (
	"context"
	"fmt"
	"os/exec"
)

// Options controls which categories are pruned and how.
type Options struct {
	All        bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
	KeepLast   int
	Apps       []string
}

// Report lists what was removed (or, in dry-run mode, what would be removed).
type Report struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
	BuildCache bool
	DryRun     bool
}

// Total returns the number of individual items in the report.
func (r Report) Total() int {
	return len(r.Containers) + len(r.Images) + len(r.Volumes) + len(r.Networks)
}

// Manager prunes unused Docker resources.
type Manager interface {
	Prune(ctx context.Context, opts Options) (Report, error)
}

// NewDocker returns an exec-based Manager backed by the docker CLI.
func NewDocker() (Manager, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerRuntime{}, nil
}

// NewStub returns a Manager that does nothing, for tests.
func NewStub() Manager {
	return &stubManager{}
}

type stubManager struct{}

func (m *stubManager) Prune(ctx context.Context, opts Options) (Report, error) {
	return Report{DryRun: opts.DryRun}, nil
}
