package builder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func (b *Builder) buildWithNixpacks(ctx context.Context, dir string, appName string, env string, deploymentID string, detection *Detection) (string, string, error) {
	if env == "" {
		env = "production"
	}

	if _, err := exec.LookPath("nixpacks"); err != nil {
		return "", "", fmt.Errorf("nixpacks not found in PATH: %w. Install from https://nixpacks.com/docs/install", err)
	}

	tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

	args := []string{"build", dir, "--name", tag}

	if detection.BuildCmd != "" {
		args = append(args, "--build-cmd", detection.BuildCmd)
	}

	cmd := exec.CommandContext(ctx, "nixpacks", args...)

	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(os.Stdout, &logBuf)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	if err := cmd.Run(); err != nil {
		return "", logBuf.String(), fmt.Errorf("nixpacks build: %w", err)
	}

	latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", appName, env)
	tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
	if out, err := tagCmd.CombinedOutput(); err != nil {
		return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
	}

	return tag, logBuf.String(), nil
}
