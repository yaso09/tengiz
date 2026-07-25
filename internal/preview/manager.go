package preview

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/git"
	"github.com/yaso09/tengiz/internal/proxy"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type Manager struct {
	dataDir     string
	store       *config.Store
	rt          runtime.Manager
	builder     *builder.Builder
	nixpacksCfg *types.NixpacksConfig
}

func (m *Manager) SetNixpacksConfig(cfg *types.NixpacksConfig) {
	m.nixpacksCfg = cfg
	m.builder.SetNixpacksConfig(cfg)
}

func NewManager(dataDir string, store *config.Store, rt runtime.Manager) *Manager {
	return &Manager{
		dataDir: dataDir,
		store:   store,
		rt:      rt,
		builder: builder.New(dataDir),
	}
}

func (m *Manager) containerName(appName string, prNumber int) string {
	return fmt.Sprintf("tengiz-%s-pr-%d", appName, prNumber)
}

func (m *Manager) imageTag(appName string, prNumber int, deploymentID string) string {
	return fmt.Sprintf("tengiz-apps/%s:pr-%d-%s", appName, prNumber, deploymentID)
}

func (m *Manager) routeKey(appName string, prNumber int) string {
	return fmt.Sprintf("pr-%d.%s", prNumber, appName)
}

func (m *Manager) Create(ctx context.Context, appName string, prNumber int, branch, repoURL string) (*types.PreviewEntry, error) {
	cloneDir, err := os.MkdirTemp("", fmt.Sprintf("tengiz-%s-pr-%d-*", appName, prNumber))
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(cloneDir)

	keyPath := ""
	if git.HasKey(m.dataDir) {
		keyPath = git.KeyPath(m.dataDir)
	}
	if err := git.Clone(ctx, repoURL, branch, cloneDir, keyPath); err != nil {
		return nil, fmt.Errorf("clone: %w", err)
	}

	detection, err := builder.Detect(cloneDir)
	if err != nil {
		return nil, fmt.Errorf("detect: %w", err)
	}
	if m.nixpacksCfg != nil {
		detection.Framework = builder.FrameworkNixpacks
	}

	deploymentID := fmt.Sprintf("%d", time.Now().Unix())
	tag := m.imageTag(appName, prNumber, deploymentID)

	_, buildLog, err := m.builder.Build(ctx, cloneDir, appName, "", detection, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("build: %w", err)
	}
	_ = buildLog

	port, err := m.store.AllocatePort(m.routeKey(appName, prNumber))
	if err != nil {
		return nil, fmt.Errorf("port: %w", err)
	}

	cfg := &types.AppConfig{
		Name: appName,
		Port: detection.InternalPort,
		Serverless: types.ServerlessConfig{
			Enabled:     true,
			IdleTimeout: 5 * time.Minute,
		},
	}

	if err := m.rt.Create(ctx, cfg, tag, port); err != nil {
		m.store.FreePort(port)
		return nil, fmt.Errorf("create container: %w", err)
	}

	cName := m.containerName(appName, prNumber)

	preview := &types.PreviewEntry{
		AppName:       appName,
		PRNumber:      prNumber,
		Branch:        branch,
		RepoURL:       repoURL,
		ImageTag:      tag,
		Port:          port,
		ContainerName: cName,
		DeploymentID:  deploymentID,
		Status:        string(types.PreviewActive),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := m.store.AddPreview(*preview); err != nil {
		m.rt.Remove(ctx, cName)
		m.store.FreePort(port)
		return nil, fmt.Errorf("save preview: %w", err)
	}

	if err := proxy.RegisterRouteWithProxy(m.routeKey(appName, prNumber), port); err != nil {
		log.Printf("[tengiz] preview: proxy not available: %v", err)
	}

	return preview, nil
}

func (m *Manager) Update(ctx context.Context, appName string, prNumber int, branch string) (*types.PreviewEntry, error) {
	existing, err := m.store.GetPreview(appName, prNumber)
	if err != nil {
		return nil, fmt.Errorf("preview not found: %w", err)
	}

	cloneDir, err := os.MkdirTemp("", fmt.Sprintf("tengiz-%s-pr-%d-*", appName, prNumber))
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(cloneDir)

	keyPath := ""
	if git.HasKey(m.dataDir) {
		keyPath = git.KeyPath(m.dataDir)
	}
	if err := git.Clone(ctx, existing.RepoURL, branch, cloneDir, keyPath); err != nil {
		return nil, fmt.Errorf("clone: %w", err)
	}

	detection, err := builder.Detect(cloneDir)
	if err != nil {
		return nil, fmt.Errorf("detect: %w", err)
	}
	if m.nixpacksCfg != nil {
		detection.Framework = builder.FrameworkNixpacks
	}

	deploymentID := fmt.Sprintf("%d", time.Now().Unix())
	tag := m.imageTag(appName, prNumber, deploymentID)

	if _, _, err := m.builder.Build(ctx, cloneDir, appName, "", detection, deploymentID); err != nil {
		return nil, fmt.Errorf("build: %w", err)
	}

	cName := m.containerName(appName, prNumber)

	m.rt.Remove(ctx, cName)

	cfg := &types.AppConfig{
		Name: appName,
		Port: detection.InternalPort,
		Serverless: types.ServerlessConfig{
			Enabled:     true,
			IdleTimeout: 5 * time.Minute,
		},
	}
	if err := m.rt.Create(ctx, cfg, tag, existing.Port); err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	existing.ImageTag = tag
	existing.DeploymentID = deploymentID
	existing.Branch = branch
	existing.UpdatedAt = time.Now()

	if err := m.store.UpdatePreviewDeployment(appName, prNumber, tag, deploymentID); err != nil {
		return nil, fmt.Errorf("save preview: %w", err)
	}

	if err := proxy.RegisterRouteWithProxy(m.routeKey(appName, prNumber), existing.Port); err != nil {
		log.Printf("[tengiz] preview: proxy not available: %v", err)
	}

	m.rt.RemoveImage(ctx, tag)

	return existing, nil
}

func (m *Manager) Delete(ctx context.Context, appName string, prNumber int) error {
	existing, err := m.store.GetPreview(appName, prNumber)
	if err != nil {
		return fmt.Errorf("preview not found: %w", err)
	}

	cName := m.containerName(appName, prNumber)
	m.rt.Remove(ctx, cName)
	m.store.FreePort(existing.Port)
	m.store.DeletePreview(appName, prNumber)

	if err := proxy.UnregisterRouteWithProxy(m.routeKey(appName, prNumber)); err != nil {
		log.Printf("[tengiz] preview: proxy not available: %v", err)
	}

	if existing.ImageTag != "" {
		m.rt.RemoveImage(ctx, existing.ImageTag)
	}

	return nil
}

func (m *Manager) List(ctx context.Context, appName string) ([]types.PreviewEntry, error) {
	return m.store.ListPreviews(appName)
}
