package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/yasir/tengiz/internal/types"
)

const labelKey = "tengiz-app"

type dockerRuntime struct{}

func NewDocker() (Manager, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerRuntime{}, nil
}

func (r *dockerRuntime) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	internalPort := cfg.Port
	if internalPort == 0 {
		internalPort = 8080
	}
	containerName := fmt.Sprintf("tengiz-%s", cfg.Name)

	args := []string{
		"run", "-d",
		"--name", containerName,
		"--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, internalPort),
		"--restart", "no",
		imageTag,
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) Start(ctx context.Context, name string) error {
	containerName := fmt.Sprintf("tengiz-%s", name)
	cmd := exec.CommandContext(ctx, "docker", "start", containerName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker start: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) Stop(ctx context.Context, name string) error {
	containerName := fmt.Sprintf("tengiz-%s", name)
	cmd := exec.CommandContext(ctx, "docker", "stop", "-t", "5", containerName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker stop: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) Remove(ctx context.Context, name string) error {
	containerName := fmt.Sprintf("tengiz-%s", name)
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) IsActive(ctx context.Context, name string) (bool, error) {
	containerName := fmt.Sprintf("tengiz-%s", name)
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", containerName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

type dockerPS struct {
	ID     string `json:"ID"`
	Name   string `json:"Name"`
	State  string `json:"State"`
	Ports  string `json:"Ports"`
	Labels string `json:"Labels"`
}

func (r *dockerRuntime) List(ctx context.Context) ([]types.AppStatus, error) {
	format := `{{json .}}`
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s", labelKey),
		"--format", format)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var apps []types.AppStatus
	for _, line := range lines {
		if line == "" {
			continue
		}
		var entry dockerPS
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		appName := ""
		for _, part := range strings.Split(entry.Labels, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 && kv[0] == labelKey {
				appName = kv[1]
				break
			}
		}
		if appName == "" {
			appName = strings.TrimPrefix(entry.Name, "/")
		}
		state := types.StateStopped
		if entry.State == "running" {
			state = types.StateRunning
		}
		apps = append(apps, types.AppStatus{
			Name:  appName,
			State: state,
		})
	}
	return apps, nil
}

func (r *dockerRuntime) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) {
	containerName := fmt.Sprintf("tengiz-%s", name)
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, containerName)
	cmd := exec.CommandContext(ctx, "docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return stdout, nil
}

func (r *dockerRuntime) WaitForReady(ctx context.Context, name string, internalPort int) error {
	containerName := fmt.Sprintf("tengiz-%s", name)
	// Wait for container to be running
	for {
		cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", containerName)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("inspect: %w", err)
		}
		if strings.TrimSpace(string(out)) == "true" {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	// Get the host port
	portCmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{(index (index .NetworkSettings.Ports \""+fmt.Sprintf("%d", internalPort)+"/tcp\") 0).HostPort}}", containerName)
	portOut, err := portCmd.CombinedOutput()
	if err != nil {
		return nil
	}
	hostPort := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(string(portOut)), "%d", &hostPort); err != nil {
		return nil
	}
	return waitForPort(ctx, "127.0.0.1", hostPort, 30*time.Second)
}

func waitForPort(ctx context.Context, host string, port int, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 1*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-deadline:
			return fmt.Errorf("timeout waiting for port %d", port)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func sanitizeContainerName(name string) string {
	s := strings.ToLower(name)
	var buf bytes.Buffer
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			buf.WriteRune(r)
		} else {
			buf.WriteRune('-')
		}
	}
	return strings.Trim(buf.String(), "-_")
}
