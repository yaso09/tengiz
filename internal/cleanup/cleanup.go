package cleanup

import (
	"context"
	"fmt"
	"os/exec"
)

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

type Report struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
}

type Manager interface {
	Cleanup(ctx context.Context, opts Options) (*Report, error)
}

type dockerCleaner struct{}

func NewDocker() (Manager, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerCleaner{}, nil
}

func (c *dockerCleaner) Cleanup(ctx context.Context, opts Options) (*Report, error) {
	return nil, fmt.Errorf("cleanup not implemented")
}

type stubManager struct{}

func NewStub() Manager {
	return &stubManager{}
}

func (m *stubManager) Cleanup(ctx context.Context, opts Options) (*Report, error) {
	return &Report{}, nil
}
