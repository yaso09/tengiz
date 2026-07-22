package gitdeploy

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/git"
	"github.com/yaso09/tengiz/internal/proxy"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type Pipeline struct {
	dataDir string
	env     string
	b       *builder.Builder
	rt      runtime.Manager
	store   *config.Store
}

func NewPipeline(dataDir string, rt runtime.Manager, store *config.Store) *Pipeline {
	return NewPipelineWithEnv(dataDir, "", rt, store)
}

func NewPipelineWithEnv(dataDir, env string, rt runtime.Manager, store *config.Store) *Pipeline {
	if env == "" {
		env = "production"
	}
	return &Pipeline{
		dataDir: dataDir,
		env:     env,
		b:       builder.New(dataDir),
		rt:      rt,
		store:   store,
	}
}

func extractAppName(repo string) string {
	name := strings.TrimSuffix(repo, ".git")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}

func (p *Pipeline) Deploy(ctx context.Context, repoURL, branch, provider string) error {
	appName := extractAppName(repoURL)

	log.Printf("[tengiz] git deploy: %s (%s/%s)", appName, provider, branch)

	cloneDir, err := os.MkdirTemp("", fmt.Sprintf("tengiz-%s-*", appName))
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(cloneDir)

	keyPath := ""
	if git.HasKey(p.dataDir) {
		keyPath = git.KeyPath(p.dataDir)
	}
	if err := git.Clone(ctx, repoURL, branch, cloneDir, keyPath); err != nil {
		return fmt.Errorf("clone: %w", err)
	}

	existingApp, lookupErr := p.store.GetApp(appName)

	detection, err := builder.Detect(cloneDir)
	if err != nil {
		return fmt.Errorf("detect: %w", err)
	}
	log.Printf("[tengiz] detected: %s (port %d)", detection.Framework, detection.InternalPort)

	cfg := &types.AppConfig{
		Name: appName,
		Port: detection.InternalPort,
		Serverless: types.ServerlessConfig{
			Enabled:     true,
			IdleTimeout: 5 * time.Minute,
		},
		Git: &types.GitConfig{
			Repo:     repoURL,
			Branch:   branch,
			Provider: provider,
		},
	}

	if lookupErr == nil {
		cfg.Env = existingApp.Config.Env
		cfg.Domains = existingApp.Domains
		cfg.HealthCheck = existingApp.Config.HealthCheck
		cfg.Serverless = existingApp.Config.Serverless
		cfg.Environment = existingApp.Config.Environment
		if existingApp.Config.Port != 0 {
			cfg.Port = existingApp.Config.Port
		}
	}

	if lookupErr == nil && existingApp.Config.Build.Builder == "nixpacks" {
		detection.Framework = builder.FrameworkNixpacks
	}
	if lookupErr == nil && existingApp.Config.Build.NixpacksConfig != nil {
		p.b.SetNixpacksConfig(existingApp.Config.Build.NixpacksConfig)
	}

	deploymentID := fmt.Sprintf("%d", time.Now().Unix())
	imageTag, buildLog, err := p.b.Build(ctx, cloneDir, appName, p.env, detection, deploymentID)
	if err != nil {
		fmt.Fprint(os.Stderr, buildLog)
		return fmt.Errorf("build: %w", err)
	}
	log.Printf("[tengiz] built image: %s", imageTag)

	if buildLog != "" {
		if saveErr := p.store.SaveBuildLog(appName, deploymentID, buildLog); saveErr != nil {
			log.Printf("[tengiz] warning: failed to save build log: %v", saveErr)
		}
		if pruneErr := p.store.PruneBuildLogs(appName, 5); pruneErr != nil {
			log.Printf("[tengiz] warning: failed to prune build logs: %v", pruneErr)
		}
	}

	if lookupErr != nil {
		port, err := p.store.AllocatePort(appName)
		if err != nil {
			return fmt.Errorf("port: %w", err)
		}

		if err := p.rt.Create(ctx, cfg, imageTag, port); err != nil {
			p.store.FreePort(port)
			return fmt.Errorf("create: %w", err)
		}
		log.Printf("[tengiz] running on port %d", port)

		p.store.AddDeployment(appName, types.DeploymentEntry{
			ID:        deploymentID,
			ImageTag:  imageTag,
			Port:      port,
			CreatedAt: time.Now(),
			Status:    string(types.DeployActive),
		})

		p.store.SaveApp(types.AppEntry{
			Name:        appName,
			ImageTag:    imageTag,
			Port:        port,
			Domains:     cfg.Domains,
			Config:      *cfg,
			GitRepo:     repoURL,
			GitBranch:   branch,
			GitProvider: provider,
		})

		if err := proxy.RegisterRouteWithProxy(appName, port); err != nil {
			log.Printf("[tengiz] proxy not available: %v", err)
		}

		if err := p.rt.KeepLastNImages(ctx, appName, 5); err != nil {
			log.Printf("[tengiz] warning: image cleanup: %v", err)
		}

		log.Printf("[tengiz] deployed: %s via git push", appName)
		return nil
	}

	// Zero-downtime deploy path: generate a fresh deployment ID for this new deploy
	deploymentID = fmt.Sprintf("%d", time.Now().Unix())
	newPort, err := p.store.AllocatePort(appName)
	if err != nil {
		return fmt.Errorf("port allocation: %w", err)
	}

	if err := p.rt.CreateVersioned(ctx, cfg, imageTag, newPort, deploymentID); err != nil {
		p.store.FreePort(newPort)
		return fmt.Errorf("create versioned: %w", err)
	}
	log.Printf("[tengiz] new container starting on port %d", newPort)

	containerName := runtime.ContainerName(appName, p.env)
	if err := p.rt.WaitForReady(ctx, fmt.Sprintf("%s-%s", containerName, deploymentID), cfg.Port); err != nil {
		log.Printf("[tengiz] warning: new container may not be ready: %v", err)
	}

	if err := proxy.RegisterRouteWithProxy(appName, newPort); err != nil {
		log.Printf("[tengiz] proxy not available: %v", err)
	}

	if existingApp.DeploymentSuffix != "" {
		p.rt.RemoveBySuffix(ctx, containerName, existingApp.DeploymentSuffix)
	} else {
		p.rt.Remove(ctx, containerName)
	}
	p.store.FreePort(existingApp.Port)

	p.store.AddDeployment(appName, types.DeploymentEntry{
		ID:        deploymentID,
		ImageTag:  imageTag,
		Port:      newPort,
		CreatedAt: time.Now(),
		Status:    string(types.DeployActive),
	})

	if existingApp.DeploymentSuffix != "" {
		p.store.AddDeployment(appName, types.DeploymentEntry{
			ID:        existingApp.DeploymentSuffix,
			ImageTag:  existingApp.ImageTag,
			Port:      existingApp.Port,
			CreatedAt: time.Now(),
			Status:    string(types.DeployPrevious),
		})
	}

	p.store.SaveApp(types.AppEntry{
		Name:             appName,
		ImageTag:         imageTag,
		Port:             newPort,
		Domains:          cfg.Domains,
		Config:           *cfg,
		DeploymentSuffix: deploymentID,
		GitRepo:          repoURL,
		GitBranch:        branch,
		GitProvider:      provider,
	})

	if err := p.rt.KeepLastNImages(ctx, appName, 5); err != nil {
		log.Printf("[tengiz] warning: image cleanup: %v", err)
	}

	log.Printf("[tengiz] deployed (zero-downtime) via git push: %s", appName)
	return nil
}
