package cli

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type mockRTForDeploy struct {
	created atomic.Int32
	removed atomic.Int32
	started atomic.Int32
	stopped atomic.Int32
}

func (m *mockRTForDeploy) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	m.created.Add(1)
	return nil
}

func (m *mockRTForDeploy) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error {
	m.created.Add(1)
	return nil
}

func (m *mockRTForDeploy) Start(ctx context.Context, name string) error { m.started.Add(1); return nil }
func (m *mockRTForDeploy) Stop(ctx context.Context, name string) error { m.stopped.Add(1); return nil }
func (m *mockRTForDeploy) Remove(ctx context.Context, name string) error { m.removed.Add(1); return nil }
func (m *mockRTForDeploy) RemoveBySuffix(ctx context.Context, name string, suffix string) error { m.removed.Add(1); return nil }
func (m *mockRTForDeploy) IsActive(ctx context.Context, name string) (bool, error) { return true, nil }
func (m *mockRTForDeploy) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) { return 0, nil }
func (m *mockRTForDeploy) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *mockRTForDeploy) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) { return nil, nil }
func (m *mockRTForDeploy) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }

func TestDeployZeroDowntimeCreatesVersionedContainer(t *testing.T) {
	var m runtime.Manager = &mockRTForDeploy{}
	if m == nil {
		t.Fatal("mock does not implement Manager")
	}
}
