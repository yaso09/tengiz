package runtime

import (
	"context"
	"fmt"
	"io"

	"github.com/yaso09/tengiz/internal/types"
)

// PruneReport summarizes what was cleaned.
type PruneReport struct {
	Containers []string `json:"containers,omitempty"`
	Images     []string `json:"images,omitempty"`
	Volumes    []string `json:"volumes,omitempty"`
	Networks   []string `json:"networks,omitempty"`
	BuildCache bool     `json:"build_cache,omitempty"`
	DryRun     bool     `json:"dry_run"`
	Env        string   `json:"env"`
}

// DiskUsageReport summarizes Docker disk usage.
type DiskUsageReport struct {
	Containers int64  `json:"containers_bytes"`
	Images     int64  `json:"images_bytes"`
	Volumes    int64  `json:"volumes_bytes"`
	BuildCache int64  `json:"build_cache_bytes"`
	Total      int64  `json:"total_bytes"`
	HumanTotal string `json:"human_total"`
}

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
	// PruneContainers removes stopped containers filtered by env label.
	PruneContainers(ctx context.Context, env string, dryRun bool) ([]string, error)
	// PruneImages removes unused images filtered by env label.
	PruneImages(ctx context.Context, env string, dryRun bool) ([]string, error)
	// PruneVolumes removes unused volumes.
	PruneVolumes(ctx context.Context, env string, dryRun bool) ([]string, error)
	// PruneNetworks removes unused networks.
	PruneNetworks(ctx context.Context, env string, dryRun bool) ([]string, error)
	// PruneBuildCache removes BuildKit build cache.
	PruneBuildCache(ctx context.Context, dryRun bool) ([]string, error)
	// PruneSystem runs docker system prune with label filters.
	PruneSystem(ctx context.Context, env string, dryRun bool, volumes bool) (PruneReport, error)
	// DiskUsage returns Docker disk usage summary.
	DiskUsage(ctx context.Context) (DiskUsageReport, error)
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

func (m *stubManager) PruneContainers(ctx context.Context, env string, dryRun bool) ([]string, error) {
	return nil, nil
}

func (m *stubManager) PruneImages(ctx context.Context, env string, dryRun bool) ([]string, error) {
	return nil, nil
}

func (m *stubManager) PruneVolumes(ctx context.Context, env string, dryRun bool) ([]string, error) {
	return nil, nil
}

func (m *stubManager) PruneNetworks(ctx context.Context, env string, dryRun bool) ([]string, error) {
	return nil, nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context, dryRun bool) ([]string, error) {
	return nil, nil
}

func (m *stubManager) PruneSystem(ctx context.Context, env string, dryRun bool, volumes bool) (PruneReport, error) {
	return PruneReport{DryRun: dryRun, Env: env}, nil
}

func (m *stubManager) DiskUsage(ctx context.Context) (DiskUsageReport, error) {
	return DiskUsageReport{}, nil
}
