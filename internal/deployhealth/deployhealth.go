package deployhealth

import (
	"context"
	"fmt"
	"time"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

// Wait blocks until the freshly-created container is safe to receive
// traffic. When the app has healthcheck.enabled it runs an app-level HTTP
// health gate via runtime.Manager.WaitForHealth; otherwise it keeps the
// historical best-effort TCP readiness wait.
func Wait(ctx context.Context, rt runtime.Manager, cfg *types.AppConfig, versionedContainerName string, internalPort int) error {
	if cfg.HealthCheck != nil && cfg.HealthCheck.Enabled {
		return rt.WaitForHealth(ctx, versionedContainerName, cfg.HealthCheck)
	}
	return rt.WaitForReady(ctx, versionedContainerName, internalPort)
}

// Abort rolls back a deployment whose health gate failed: it removes the
// freshly-created container, frees its port, and records the deployment as
// failed. The previous container is left untouched and keeps serving
// traffic.
func Abort(ctx context.Context, rt runtime.Manager, store *config.Store, appName, containerName, deploymentID, imageTag string, newPort int) error {
	if err := rt.RemoveBySuffix(ctx, containerName, deploymentID); err != nil {
		return fmt.Errorf("remove failed container %s-%s: %w", containerName, deploymentID, err)
	}
	if err := store.FreePort(newPort); err != nil {
		return fmt.Errorf("free port %d: %w", newPort, err)
	}
	if err := store.AddDeployment(appName, types.DeploymentEntry{
		ID:        deploymentID,
		ImageTag:  imageTag,
		Port:      newPort,
		CreatedAt: time.Now(),
		Status:    string(types.DeployFailed),
	}); err != nil {
		return fmt.Errorf("record failed deployment: %w", err)
	}
	return nil
}