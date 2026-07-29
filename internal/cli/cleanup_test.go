package cli

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

type mockCleanupManager struct {
	runtime.Manager
	pruneContainersCalled bool
	pruneImagesCalled     bool
	pruneVolumesCalled    bool
	pruneNetworksCalled   bool
	pruneBuildCacheCalled bool
}

func (m *mockCleanupManager) PruneContainers(ctx context.Context) (uint64, error) {
	m.pruneContainersCalled = true
	return 0, nil
}

func (m *mockCleanupManager) PruneImages(ctx context.Context, all bool) (uint64, error) {
	m.pruneImagesCalled = true
	return 0, nil
}

func (m *mockCleanupManager) PruneVolumes(ctx context.Context) (uint64, error) {
	m.pruneVolumesCalled = true
	return 0, nil
}

func (m *mockCleanupManager) PruneNetworks(ctx context.Context) (uint64, error) {
	m.pruneNetworksCalled = true
	return 0, nil
}

func (m *mockCleanupManager) PruneBuildCache(ctx context.Context, all bool) (uint64, error) {
	m.pruneBuildCacheCalled = true
	return 0, nil
}

func (m *mockCleanupManager) DiskUsage(ctx context.Context) (*runtime.DiskUsageInfo, error) {
	return &runtime.DiskUsageInfo{}, nil
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0B"},
		{500, "500B"},
		{1500, "1.50kB"},
		{1048576, "1.05MB"},
		{1073741824, "1.07GB"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatBytes(tt.input)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
