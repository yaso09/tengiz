package builder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type DockerfileStrategy struct {
	dataDir string
}

func NewDockerfileStrategy(dataDir string) *DockerfileStrategy {
	return &DockerfileStrategy{dataDir: dataDir}
}

func (s *DockerfileStrategy) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
	if detection.Framework == FrameworkDocker {
		return s.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
	}
	if err := s.ensureDockerfile(dir, detection); err != nil {
		return "", "", fmt.Errorf("generate dockerfile: %w", err)
	}
	return s.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
}

func (s *DockerfileStrategy) ensureDockerfile(dir string, detection *Detection) error {
	dfPath := filepath.Join(dir, "Dockerfile")
	if _, err := os.Stat(dfPath); err == nil {
		return nil
	}
	content := generateDockerfile(detection)
	return os.WriteFile(dfPath, []byte(content), 0644)
}

func (s *DockerfileStrategy) buildWithDockerfile(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
	if env == "" {
		env = "production"
	}
	tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", tag, dir)

	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(os.Stdout, &logBuf)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	if err := cmd.Run(); err != nil {
		return "", logBuf.String(), fmt.Errorf("docker build: %w", err)
	}

	latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", appName, env)
	tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
	if out, err := tagCmd.CombinedOutput(); err != nil {
		return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
	}

	return tag, logBuf.String(), nil
}
