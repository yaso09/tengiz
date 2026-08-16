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

// CleanupOptions controls which Docker resource categories are pruned.
//
// Containers, Images, and BuildCache are safe-by-default (running
// containers and tagged images are never touched). Volumes and Networks
// are opt-in because they can affect resources not managed by Tengiz.
type CleanupOptions struct {
	Containers bool // prune stopped Tengiz-managed containers (label tengiz-app)
	Images     bool // prune dangling images (untagged only)
	BuildCache bool // prune unused build cache
	Volumes    bool // prune unused volumes (opt-in)
	Networks   bool // prune unused networks (opt-in)
}

// CleanupResult reports what a Cleanup call removed. Counts are
// best-effort and derived from the docker prune output; the full docker
// output is available in Output.
type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCacheCleared bool
	Output            string
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
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
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

func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
