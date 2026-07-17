package builder

import "context"

type Strategy interface {
	Build(ctx context.Context, dir, appName, env, deploymentID string, detection *Detection) (imageTag string, buildLog string, err error)
}
