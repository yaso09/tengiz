package runtime

import (
	"context"
	"io"

	"github.com/yaso09/tengiz/internal/types"
)

type Manager interface {
	Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	RemoveBySuffix(ctx context.Context, name string, suffix string) error
	IsActive(ctx context.Context, name string) (bool, error)
	GetContainerPort(ctx context.Context, name string, suffix string) (int, error)
	List(ctx context.Context) ([]types.AppStatus, error)
	Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error)
	WaitForReady(ctx context.Context, name string, internalPort int) error
	WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error
}

type stubManager struct{}

func NewStub() Manager {
	return &stubManager{}
}

func (m *stubManager) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	return nil
}

func (m *stubManager) Start(ctx context.Context, name string) error {
	return nil
}

func (m *stubManager) Stop(ctx context.Context, name string) error {
	return nil
}

func (m *stubManager) Restart(ctx context.Context, name string) error {
	return nil
}

func (m *stubManager) Remove(ctx context.Context, name string) error {
	return nil
}

func (m *stubManager) IsActive(ctx context.Context, name string) (bool, error) {
	return false, nil
}

func (m *stubManager) List(ctx context.Context) ([]types.AppStatus, error) {
	return nil, nil
}

func (m *stubManager) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) {
	return nil, nil
}

func (m *stubManager) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error {
	return nil
}

func (m *stubManager) RemoveBySuffix(ctx context.Context, name string, suffix string) error {
	return nil
}

func (m *stubManager) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) {
	return 0, nil
}

func (m *stubManager) WaitForReady(ctx context.Context, name string, internalPort int) error {
	return nil
}

func (m *stubManager) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error {
	return nil
}
