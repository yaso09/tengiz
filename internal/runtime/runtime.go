package runtime

import (
	"context"
	"fmt"
	"io"

	"github.com/yaso09/tengiz/internal/types"
)

func ContainerName(name, env string) string {
	if env == "" || env == "production" {
		return fmt.Sprintf("tengiz-%s", name)
	}
	return fmt.Sprintf("tengiz-%s-%s", name, env)
}

type LogOptions struct {
	Follow bool
	Since  string
	Until  string
	Tail   int
	Grep   string
}

type RunOptions struct {
	Interactive bool
	ExtraEnv    map[string]string
}

type PruneReport struct {
	Containers int64 `json:"containers"`
	Images     int64 `json:"images"`
	Networks   int64 `json:"networks"`
	BuildCache int64 `json:"build_cache"`
	BytesFreed int64 `json:"bytes_freed"`
}

type Manager interface {
	Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	RemoveBySuffix(ctx context.Context, name string, suffix string) error
	IsActive(ctx context.Context, name string) (bool, error)
	GetContainerPort(ctx context.Context, name string, suffix string) (int, error)
	List(ctx context.Context) ([]types.AppStatus, error)
	Logs(ctx context.Context, name string, opts LogOptions) (io.ReadCloser, error)
	WaitForReady(ctx context.Context, name string, internalPort int) error
	WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	PruneSystem(ctx context.Context, force bool) (PruneReport, error)
	PruneBuildCache(ctx context.Context, force bool) (PruneReport, error)
	PruneContainers(ctx context.Context, appName string) error
	PruneImages(ctx context.Context, appName string, keep int) error
}

type stubManager struct{}

func NewStub() Manager {
	return &stubManager{}
}

func (m *stubManager) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	return nil
}

func (m *stubManager) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
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

func (m *stubManager) Logs(ctx context.Context, name string, opts LogOptions) (io.ReadCloser, error) {
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

func (m *stubManager) RemoveImage(ctx context.Context, imageTag string) error {
	return nil
}

func (m *stubManager) KeepLastNImages(ctx context.Context, appName string, n int) error {
	return nil
}

func (m *stubManager) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error {
	return nil
}

func (m *stubManager) PruneSystem(ctx context.Context, force bool) (PruneReport, error) {
	return PruneReport{}, nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context, force bool) (PruneReport, error) {
	return PruneReport{}, nil
}

func (m *stubManager) PruneContainers(ctx context.Context, appName string) error {
	return nil
}

func (m *stubManager) PruneImages(ctx context.Context, appName string, keep int) error {
	return nil
}
