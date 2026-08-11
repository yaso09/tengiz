package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yaso09/tengiz/internal/types"
)

func envArgs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var args []string
	for _, k := range keys {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, env[k]))
	}
	return args
}

func volumeArgs(volumes []types.VolumeConfig) []string {
	if len(volumes) == 0 {
		return nil
	}
	keys := make([]int, len(volumes))
	for i := range volumes {
		keys[i] = i
	}
	sort.Slice(keys, func(i, j int) bool {
		return volumes[keys[i]].ContainerPath < volumes[keys[j]].ContainerPath
	})
	var args []string
	for _, i := range keys {
		v := volumes[i]
		mount := fmt.Sprintf("%s:%s", v.HostPath, v.ContainerPath)
		if v.ReadOnly {
			mount += ":ro"
		}
		args = append(args, "-v", mount)
	}
	return args
}

func resourceArgs(rc *types.ResourceConfig) []string {
	if rc == nil || (rc.CPU == "" && rc.Memory == "") {
		return nil
	}
	var args []string
	if rc.Memory != "" {
		args = append(args, "--memory", rc.Memory)
	}
	if rc.CPU != "" {
		args = append(args, "--cpus", rc.CPU)
	}
	return args
}

const labelKey = "tengiz-app"
const envLabelKey = "tengiz-env"

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
	cn := ContainerName(cfg.Name, cfg.Environment)

	args := []string{
		"run", "-d",
		"--name", cn,
		"--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
		"--label", fmt.Sprintf("%s=%s", envLabelKey, cfg.Environment),
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, internalPort),
		"--restart", "no",
	}
	args = append(args, envArgs(cfg.Env)...)
	args = append(args, resourceArgs(cfg.Resources)...)
	args = append(args, volumeArgs(cfg.Volumes)...)
	args = append(args, imageTag)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	internalPort := cfg.Port
	if internalPort == 0 {
		internalPort = 8080
	}
	cn := ContainerName(cfg.Name, cfg.Environment)

	args := []string{
		"run", "-d",
		"--name", cn,
		"--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
		"--label", fmt.Sprintf("%s=%s", envLabelKey, cfg.Environment),
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, internalPort),
		"--restart", "no",
	}
	args = append(args, envArgs(cfg.Env)...)
	args = append(args, resourceArgs(cfg.Resources)...)
	args = append(args, volumeArgs(cfg.Volumes)...)
	args = append(args, imageTag)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker create from image: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) Start(ctx context.Context, name string) error {
	containerName := name

	imageTag, ports, envs, vols := r.getContainerConfig(ctx, containerName)

	cmd := exec.CommandContext(ctx, "docker", "start", containerName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker start: %w\n%s", err, string(out))
	}

	// Verify container is actually running (may have exited immediately)
	active, _ := r.IsActive(ctx, containerName)
	if !active && imageTag != "" {
		log.Printf("[runtime] container %s exited after start, recreating", name)
		exec.CommandContext(ctx, "docker", "rm", "-f", containerName).Run()
		args := []string{"run", "-d",
			"--name", containerName,
			"--label", fmt.Sprintf("%s=%s", labelKey, name),
			"--restart", "no",
		}
		args = append(args, ports...)
		args = append(args, envs...)
		resourceArgsFromOld := r.getResourceArgs(ctx, containerName)
		args = append(args, vols...)
		args = append(args, resourceArgsFromOld...)
		args = append(args, imageTag)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("docker recreate: %w\n%s", err, string(out))
		}
	}

	return nil
}

func (r *dockerRuntime) getContainerConfig(ctx context.Context, containerName string) (string, []string, []string, []string) {
	// Get image
	cmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{.Config.Image}}", containerName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, nil, nil
	}
	imageTag := strings.TrimSpace(string(out))

	// Get port bindings
	portCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{json .HostConfig.PortBindings}}", containerName)
	portOut, err := portCmd.CombinedOutput()
	if err != nil {
		return imageTag, nil, nil, nil
	}

	var bindings map[string][]map[string]string
	if err := json.Unmarshal(portOut, &bindings); err != nil {
		return imageTag, nil, nil, nil
	}

	var ports []string
	for containerPort, hosts := range bindings {
		for _, h := range hosts {
			hostIP := h["HostIP"]
			hostPort := h["HostPort"]
			if hostIP == "" {
				hostIP = "127.0.0.1"
			}
			p := fmt.Sprintf("%s:%s:%s", hostIP, hostPort, containerPort)
			ports = append(ports, "-p", p)
		}
	}

	// Get env variables
	envCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{json .Config.Env}}", containerName)
	envOut, err := envCmd.CombinedOutput()
	var envs []string
	if err == nil {
		var envList []string
		if err := json.Unmarshal(envOut, &envList); err == nil {
			for _, e := range envList {
				envs = append(envs, "-e", e)
			}
		}
	}

	// Get volume binds
	volCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{json .HostConfig.Binds}}", containerName)
	volOut, err := volCmd.CombinedOutput()
	var vols []string
	if err == nil {
		var binds []string
		if err := json.Unmarshal(volOut, &binds); err == nil {
			for _, b := range binds {
				vols = append(vols, "-v", b)
			}
		}
	}

	return imageTag, ports, envs, vols
}

func (r *dockerRuntime) getResourceArgs(ctx context.Context, containerName string) []string {
	memCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{.HostConfig.Memory}}", containerName)
	memOut, err := memCmd.CombinedOutput()
	memStr := ""
	if err == nil {
		memStr = strings.TrimSpace(string(memOut))
	}

	cpuCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{.HostConfig.NanoCpus}}", containerName)
	cpuOut, err := cpuCmd.CombinedOutput()
	cpuStr := ""
	if err == nil {
		cpuStr = strings.TrimSpace(string(cpuOut))
	}

	if (memStr == "" || memStr == "0") && (cpuStr == "" || cpuStr == "0") {
		return nil
	}

	var args []string
	if memStr != "" && memStr != "0" {
		args = append(args, "--memory", memStr)
	}
	if cpuStr != "" && cpuStr != "0" {
		var nanocpus int64
		if _, err := fmt.Sscanf(cpuStr, "%d", &nanocpus); err == nil && nanocpus > 0 {
			cpus := float64(nanocpus) / 1e9
			args = append(args, "--cpus", fmt.Sprintf("%g", cpus))
		}
	}
	return args
}

func (r *dockerRuntime) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error {
	if hc == nil || !hc.Enabled {
		return nil
	}
	containerName := name
	portCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{json .NetworkSettings.Ports}}", containerName)
	portOut, err := portCmd.CombinedOutput()
	if err != nil {
		return nil
	}
	var ports map[string][]map[string]string
	if err := json.Unmarshal(portOut, &ports); err != nil {
		return nil
	}
	var hostPort int
	for _, bindings := range ports {
		for _, b := range bindings {
			if hp := b["HostPort"]; hp != "" {
				fmt.Sscanf(hp, "%d", &hostPort)
				break
			}
		}
		if hostPort != 0 {
			break
		}
	}
	if hostPort == 0 {
		return nil
	}
	endpoint := hc.Endpoint
	if endpoint == "" {
		endpoint = "/health"
	}
	timeout := hc.Timeout
	if timeout <= 0 {
		timeout = 5
	}
	retries := hc.Retries
	if retries <= 0 {
		retries = 3
	}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", hostPort, endpoint)
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	var lastErr error
	for i := 0; i <= retries; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return nil
			}
			lastErr = fmt.Errorf("health check returned HTTP %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("health check failed after %d retries: %w", retries, lastErr)
}

func (r *dockerRuntime) Restart(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "restart", "-t", "5", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker restart: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) Stop(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "stop", "-t", "5", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker stop: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) Remove(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) IsActive(ctx context.Context, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", name)
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

func buildLogArgs(containerName string, opts LogOptions) []string {
	args := []string{"logs"}
	if opts.Follow {
		args = append(args, "-f")
	}
	if opts.Tail > 0 {
		args = append(args, "--tail", strconv.Itoa(opts.Tail))
	}
	if opts.Since != "" {
		args = append(args, "--since", opts.Since)
	}
	if opts.Until != "" {
		args = append(args, "--until", opts.Until)
	}
	args = append(args, containerName)
	return args
}

func buildRunArgs(cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) []string {
	args := []string{"run", "--rm"}
	if opts.Interactive {
		args = append(args, "-it")
	}
	args = append(args, "--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name))
	mergedEnv := make(map[string]string, len(cfg.Env)+len(opts.ExtraEnv))
	for k, v := range cfg.Env {
		mergedEnv[k] = v
	}
	for k, v := range opts.ExtraEnv {
		mergedEnv[k] = v
	}
	args = append(args, envArgs(mergedEnv)...)
	args = append(args, resourceArgs(cfg.Resources)...)
	args = append(args, volumeArgs(cfg.Volumes)...)
	args = append(args, imageTag)
	args = append(args, cmd...)
	return args
}

func (r *dockerRuntime) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error {
	args := buildRunArgs(cfg, imageTag, cmd, opts)
	dcmd := exec.CommandContext(ctx, "docker", args...)
	dcmd.Stdout = os.Stdout
	dcmd.Stderr = os.Stderr
	if opts.Interactive {
		dcmd.Stdin = os.Stdin
	}
	if err := dcmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("docker run: %w", err)
	}
	return nil
}

func (r *dockerRuntime) Logs(ctx context.Context, name string, opts LogOptions) (io.ReadCloser, error) {
	args := buildLogArgs(name, opts)
	cmd := exec.CommandContext(ctx, "docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if opts.Grep != "" {
		return newGrepReader(stdout, opts.Grep), nil
	}
	return stdout, nil
}

func (r *dockerRuntime) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error {
	internalPort := cfg.Port
	if internalPort == 0 {
		internalPort = 8080
	}
	cn := ContainerName(cfg.Name, cfg.Environment)
	containerName := fmt.Sprintf("%s-%s", cn, suffix)

	args := []string{
		"run", "-d",
		"--name", containerName,
		"--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
		"--label", fmt.Sprintf("%s=%s", envLabelKey, cfg.Environment),
		"--label", fmt.Sprintf("tengiz-deployment=%s", suffix),
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, internalPort),
		"--restart", "no",
	}
	args = append(args, envArgs(cfg.Env)...)
	args = append(args, resourceArgs(cfg.Resources)...)
	args = append(args, volumeArgs(cfg.Volumes)...)
	args = append(args, imageTag)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker versioned run: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) RemoveBySuffix(ctx context.Context, name string, suffix string) error {
	containerName := fmt.Sprintf("%s-%s", name, suffix)
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm %s: %w\n%s", containerName, err, string(out))
	}
	return nil
}

func (r *dockerRuntime) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) {
	containerName := fmt.Sprintf("%s-%s", name, suffix)
	portCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{json .NetworkSettings.Ports}}", containerName)
	portOut, err := portCmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("inspect %s: %w", containerName, err)
	}
	var ports map[string][]map[string]string
	if err := json.Unmarshal(portOut, &ports); err != nil {
		return 0, nil
	}
	var hostPort int
	for _, bindings := range ports {
		for _, b := range bindings {
			if hp := b["HostPort"]; hp != "" {
				fmt.Sscanf(hp, "%d", &hostPort)
				break
			}
		}
		if hostPort != 0 {
			break
		}
	}
	return hostPort, nil
}

func (r *dockerRuntime) WaitForReady(ctx context.Context, name string, internalPort int) error {
	containerName := name
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
	// Auto-detect host port from container inspect
	portCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{json .NetworkSettings.Ports}}", containerName)
	portOut, err := portCmd.CombinedOutput()
	if err != nil {
		return nil
	}
	var ports map[string][]map[string]string
	if err := json.Unmarshal(portOut, &ports); err != nil {
		return nil
	}
	var hostPort int
	for _, bindings := range ports {
		for _, b := range bindings {
			if hp := b["HostPort"]; hp != "" {
				fmt.Sscanf(hp, "%d", &hostPort)
				break
			}
		}
		if hostPort != 0 {
			break
		}
	}
	if hostPort == 0 {
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

type grepReader struct {
	reader  io.ReadCloser
	scanner *bufio.Scanner
	pattern string
	buf     []byte
}

func newGrepReader(r io.ReadCloser, pattern string) *grepReader {
	return &grepReader{
		reader:  r,
		scanner: bufio.NewScanner(r),
		pattern: pattern,
	}
}

func (g *grepReader) Read(p []byte) (int, error) {
	for g.scanner.Scan() {
		line := g.scanner.Bytes()
		if strings.Contains(string(line), g.pattern) {
			g.buf = append(g.buf, line...)
			g.buf = append(g.buf, '\n')
			n := copy(p, g.buf)
			g.buf = g.buf[n:]
			return n, nil
		}
	}
	if err := g.scanner.Err(); err != nil {
		return 0, err
	}
	return 0, io.EOF
}

func (g *grepReader) Close() error {
	return g.reader.Close()
}
