package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
)

type nixpacksPlanOutput struct {
	Plan struct {
		StartCmd     string `json:"start_cmd"`
		InternalPort int    `json:"internal_port"`
	} `json:"plan"`
}

func nixpacksAvailable() bool {
	_, err := exec.LookPath("nixpacks")
	return err == nil
}

func DetectWithNixpacks(ctx context.Context, dir string) (*Detection, error) {
	if !nixpacksAvailable() {
		return nil, fmt.Errorf("nixpacks CLI not found in PATH")
	}

	cmd := exec.CommandContext(ctx, "nixpacks", "plan", dir, "--json")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("nixpacks plan: %w\n%s", err, out.String())
	}

	var plan nixpacksPlanOutput
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		return nil, fmt.Errorf("nixpacks plan parse: %w", err)
	}

	port := plan.Plan.InternalPort
	if port == 0 {
		port = 8080
	}

	return &Detection{
		Framework:    FrameworkNixpacks,
		InternalPort: port,
		Builder:      "nixpacks",
	}, nil
}

func NixpacksGenerateDockerfile(ctx context.Context, dir string) (string, error) {
	if !nixpacksAvailable() {
		return "", fmt.Errorf("nixpacks CLI not found in PATH")
	}

	dfPath := filepath.Join(dir, "Dockerfile.nixpacks")
	cmd := exec.CommandContext(ctx, "nixpacks", "build", dir,
		"--dockerfile-path", dfPath,
		"--no-cache",
	)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("nixpacks build: %w\n%s", err, buf.String())
	}

	return dfPath, nil
}
