package gitdeploy

import (
	"context"
	"fmt"
	"log"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/preview"
)

func previewKey(appName string, prNumber int) string {
	return config.PreviewKey(appName, prNumber)
}

func previewContainerName(appName string, prNumber int) string {
	return fmt.Sprintf("tengiz-%s-pr-%d", appName, prNumber)
}

func previewSubdomain(appName string, prNumber int) string {
	return fmt.Sprintf("pr-%d.%s.tengiz.local", prNumber, appName)
}

func previewImageTag(appName string, prNumber int, deploymentID string) string {
	return fmt.Sprintf("tengiz-apps/%s:pr-%d-%s", appName, prNumber, deploymentID)
}

func (p *Pipeline) PreviewDeploy(ctx context.Context, repoURL string, prNumber int, branch string) error {
	appName := extractAppName(repoURL)
	if appName == "" {
		return fmt.Errorf("cannot extract app name from repo URL: %s", repoURL)
	}

	mgr := preview.NewManager(p.dataDir, p.store, p.rt)

	existing, err := p.store.GetPreview(appName, prNumber)
	if existing != nil && err == nil {
		_, updateErr := mgr.Update(ctx, appName, prNumber, branch)
		if updateErr != nil {
			log.Printf("[tengiz] preview update error: %v", updateErr)
		}
		return updateErr
	}

	_, createErr := mgr.Create(ctx, appName, prNumber, branch, repoURL)
	return createErr
}

func (p *Pipeline) PreviewCleanup(ctx context.Context, repoURL string, prNumber int) error {
	appName := extractAppName(repoURL)
	if appName == "" {
		return fmt.Errorf("cannot extract app name from repo URL: %s", repoURL)
	}

	if _, err := p.store.GetPreview(appName, prNumber); err != nil {
		return fmt.Errorf("preview not found: %w", err)
	}

	mgr := preview.NewManager(p.dataDir, p.store, p.rt)
	if err := mgr.Delete(ctx, appName, prNumber); err != nil {
		return fmt.Errorf("preview cleanup: %w", err)
	}

	log.Printf("[tengiz] preview: PR #%d cleaned up", prNumber)
	return nil
}
