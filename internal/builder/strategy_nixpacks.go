package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

type nixpacksPlan struct {
	Providers []string          `json:"providers"`
	Variables map[string]string `json:"variables,omitempty"`
	Phases    []nixpacksPhase   `json:"phases"`
	StartCmds []string          `json:"startCmds"`
}

type nixpacksPhase struct {
	Name string   `json:"name"`
	Cmds []string `json:"cmds"`
}

func (p *nixpacksPlan) parse(data []byte) error {
	return json.Unmarshal(data, p)
}

type NixpacksStrategy struct{}

func NewNixpacksStrategy() *NixpacksStrategy {
	return &NixpacksStrategy{}
}

func (s *NixpacksStrategy) checkCLI() error {
	_, err := exec.LookPath("nixpacks")
	return err
}

func (s *NixpacksStrategy) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
	if err := s.checkCLI(); err != nil {
		return "", "", fmt.Errorf("nixpacks CLI not found: %w\ninstall: curl -fsSL https://nixpacks.com/install.sh | bash", err)
	}

	if env == "" {
		env = "production"
	}
	tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(io.Discard, &logBuf)

	args := []string{"build", dir, "--name", tag}
	cmd := exec.CommandContext(ctx, "nixpacks", args...)
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
