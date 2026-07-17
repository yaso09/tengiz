package gitdeploy

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/git"
	"github.com/yaso09/tengiz/internal/proxy"
	"github.com/yaso09/tengiz/internal/types"
)

func previewContainerName(appName string, prNumber int) string {
	return fmt.Sprintf("tengiz-%s-pr-%d", appName, prNumber)
}

func previewSubdomain(appName string, prNumber int) string {
	return fmt.Sprintf("pr-%d.%s.tengiz.local", prNumber, appName)
}

func (p *Pipeline) PreviewDeploy(ctx context.Context, repoURL string, prNumber int, branch string) error {
	appName := extractAppName(repoURL)
	if appName == "" {
		return fmt.Errorf("cannot extract app name from repo URL: %s", repoURL)
	}

	pkey := fmt.Sprintf("%s-pr-%d", appName, prNumber)
	containerName := previewContainerName(appName, prNumber)

	tempDir, err := os.MkdirTemp("", "tengiz-preview-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	log.Printf("[tengiz] preview: cloning %s branch %s for PR #%d", repoURL, branch, prNumber)
	sshKeyPath := git.KeyPath(p.dataDir)
	if err := git.Clone(ctx, repoURL, branch, tempDir, sshKeyPath); err != nil {
		return fmt.Errorf("clone: %w", err)
	}

	detection, err := builder.Detect(tempDir)
	if err != nil {
		return fmt.Errorf("detect: %w", err)
	}
	log.Printf("[tengiz] preview: detected %s for PR #%d", detection.Framework, prNumber)

	cfg := &types.AppConfig{
		Name:    appName,
		Port:    detection.InternalPort,
		Environment: "preview",
		Serverless: types.ServerlessConfig{
			Enabled:     true,
			IdleTimeout: 30 * time.Minute,
		},
	}

	imageTag, buildLog, err := p.b.Build(ctx, tempDir, appName, fmt.Sprintf("pr-%d", prNumber), detection, fmt.Sprintf("%d", time.Now().Unix()))
	if err != nil {
		fmt.Fprint(os.Stderr, buildLog)
		return fmt.Errorf("build: %w", err)
	}

	if buildLog != "" {
		if saveErr := p.store.SaveBuildLog(pkey, fmt.Sprintf("%d", time.Now().Unix()), buildLog); saveErr != nil {
			log.Printf("[tengiz] warning: failed to save build log: %v", saveErr)
		}
	}

	port, err := p.store.AllocatePort(pkey)
	if err != nil {
		return fmt.Errorf("port: %w", err)
	}

	if err := p.rt.Create(ctx, cfg, imageTag, port); err != nil {
		p.store.FreePort(port)
		return fmt.Errorf("create: %w", err)
	}

	subdomain := previewSubdomain(appName, prNumber)
	newPreview := types.PreviewEntry{
		AppName:       appName,
		PullRequestID: prNumber,
		Branch:        branch,
		ImageTag:      imageTag,
		ContainerName: containerName,
		Port:          port,
		Subdomain:     subdomain,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Status:        types.PreviewActive,
	}

	if err := p.store.SavePreview(newPreview); err != nil {
		log.Printf("[tengiz] warning: failed to save preview: %v", err)
	}

	proxy.RegisterDomainWithProxy(subdomain, pkey)
	proxy.RegisterRouteWithProxy(pkey, port)

	log.Printf("[tengiz] preview: PR #%d deployed at http://%s:%d", prNumber, subdomain, port)
	return nil
}

func (p *Pipeline) PreviewCleanup(ctx context.Context, repoURL string, prNumber int) error {
	appName := extractAppName(repoURL)
	if appName == "" {
		return fmt.Errorf("cannot extract app name from repo URL: %s", repoURL)
	}

	pkey := fmt.Sprintf("%s-pr-%d", appName, prNumber)

	preview, err := p.store.GetPreview(appName, prNumber)
	if err != nil {
		return fmt.Errorf("preview not found: %w", err)
	}

	subdomain := previewSubdomain(appName, prNumber)
	proxy.UnregisterDomainWithProxy(subdomain)
	proxy.UnregisterRouteWithProxy(pkey)

	if err := p.rt.Remove(ctx, pkey); err != nil {
		log.Printf("[tengiz] warning: failed to remove container: %v", err)
	}

	p.store.FreePort(preview.Port)
	p.store.RemovePreview(appName, prNumber)

	if err := p.rt.KeepLastNImages(ctx, pkey, 0); err != nil {
		log.Printf("[tengiz] warning: image cleanup: %v", err)
	}

	log.Printf("[tengiz] preview: PR #%d cleaned up", prNumber)
	return nil
}
