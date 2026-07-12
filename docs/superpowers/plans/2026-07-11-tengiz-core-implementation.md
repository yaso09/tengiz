# Tengiz Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) for syntax tracking.

**Goal:** Build the Tengiz deployment engine core — a CLI tool that builds Docker images from source code, runs containers with scale-to-zero, and routes HTTP traffic via an on-demand reverse proxy.

**Architecture:** Go CLI using Docker SDK for container lifecycle. Reverse proxy with host-based routing and cold-start capability. Idle timer goroutines implement scale-to-zero. Config via `.tengiz.yaml` + CLI flags.

**Tech Stack:** Go 1.22+, Docker SDK (`github.com/docker/docker/client`), Cobra (`github.com/spf13/cobra`), Viper (`github.com/spf13/viper`)

---

## File Structure

```
tengiz/
├── cmd/
│   └── tengiz/
│       └── main.go              # CLI entry (cobra.Execute)
├── internal/
│   ├── builder/
│   │   ├── builder.go           # BuildImage, auto-generate Dockerfile
│   │   ├── detect.go            # Framework detection logic
│   │   └── builder_test.go
│   ├── proxy/
│   │   ├── proxy.go             # ReverseProxy + Router
│   │   └── proxy_test.go
│   ├── runtime/
│   │   ├── runtime.go           # Docker container lifecycle
│   │   └── runtime_test.go
│   ├── idle/
│   │   ├── idle.go              # Idle timer manager
│   │   └── idle_test.go
│   ├── config/
│   │   ├── config.go            # .tengiz.yaml parsing + merge
│   │   └── config_test.go
│   └── types/
│       └── types.go             # AppConfig, AppStatus, PortMap
├── go.mod
└── main.go                      # package main -> cmd.Execute()
```

---

### Task 1: Project scaffold, Go module, shared types

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `internal/types/types.go`

- [ ] **Step 1: Initialize Go module and main entry**

Run:
```powershell
go mod init github.com/yaso09/tengiz
```

- [ ] **Step 2: Write main.go**

`main.go`:
```go
package main

import "github.com/yaso09/tengiz/cmd/tengiz"

func main() {
	tengiz.Execute()
}
```

- [ ] **Step 3: Write shared types**

`internal/types/types.go`:
```go
package types

import "time"

type AppConfig struct {
	Name      string        `mapstructure:"app"`
	Port      int           `mapstructure:"port"`
	Build     BuildConfig   `mapstructure:"build"`
	Serverless ServerlessConfig `mapstructure:"serverless"`
	Domains   []string      `mapstructure:"domains"`
}

type BuildConfig struct {
	Command string `mapstructure:"command"`
	Output  string `mapstructure:"output"`
}

type ServerlessConfig struct {
	Enabled    bool          `mapstructure:"enabled"`
	IdleTimeout time.Duration `mapstructure:"idle_timeout"`
}

type AppState string

const (
	StateRunning AppState = "running"
	StateStopped AppState = "stopped"
	StateStarting AppState = "starting"
)

type AppStatus struct {
	Name      string    `json:"name"`
	State     AppState  `json:"state"`
	Port      int       `json:"port"`
	ImageHash string    `json:"image_hash"`
	CreatedAt time.Time `json:"created_at"`
	Domains   []string  `json:"domains"`
}

type PortEntry struct {
	AppName string `json:"app_name"`
	Port    int    `json:"port"`
}

type AppEntry struct {
	Name      string   `json:"name"`
	ImageTag  string   `json:"image_tag"`
	Port      int      `json:"port"`
	Domains   []string `json:"domains"`
	Config    AppConfig `json:"config"`
}
```

- [ ] **Step 4: Write cobra Execute scaffold**

`cmd/tengiz/main.go`:
```go
package tengiz

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tengiz",
	Short: "Tengiz - Serverless deployment platform",
	Long:  `Tengiz is a Vercel alternative. Deploy any app with scale-to-zero.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Build and verify**

Run:
```powershell
go mod tidy
go build ./...
```

Expected: binary builds without errors.

- [ ] **Step 6: Commit**

```powershell
git add -A
git commit -m "chore: scaffold tengiz Go module with shared types"
```

---

### Task 2: Config loader (.tengiz.yaml)

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write config package**

`internal/config/config.go`:
```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
	"github.com/yaso09/tengiz/internal/types"
)

const defaultIdleTimeout = 5 * time.Minute

func Load(path string) (*types.AppConfig, error) {
	v := viper.New()
	v.SetConfigFile(filepath.Join(path, ".tengiz.yaml"))
	v.SetConfigType("yaml")

	v.SetDefault("serverless.enabled", true)
	v.SetDefault("serverless.idle_timeout", defaultIdleTimeout)

	if _, err := os.Stat(v.ConfigFileUsed()); err == nil {
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config read: %w", err)
		}
	}

	var cfg types.AppConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config unmarshal: %w", err)
	}

	if cfg.Name == "" {
		return nil, fmt.Errorf("config: 'app' field is required")
	}

	return &cfg, nil
}

func FindProjectRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		if _, err := os.Stat(filepath.Join(dir, ".tengiz.yaml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs, nil
		}
		dir = parent
	}
}
```

- [ ] **Step 2: Write config test**

`internal/config/config_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadBasicConfig(t *testing.T) {
	dir := t.TempDir()
	yaml := `app: myapp\nport: 3000`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Name != "myapp" {
		t.Errorf("Name = %q, want %q", cfg.Name, "myapp")
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want %d", cfg.Port, 3000)
	}
	if !cfg.Serverless.Enabled {
		t.Errorf("Serverless.Enabled = false, want true")
	}
	if cfg.Serverless.IdleTimeout != 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want %v", cfg.Serverless.IdleTimeout, 5*time.Minute)
	}
}

func TestLoadMissingAppField(t *testing.T) {
	dir := t.TempDir()
	yaml := `port: 3000`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load() expected error for missing 'app' field")
	}
}

func TestLoadNoFile(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load() expected error when no .tengiz.yaml")
	}
}
```

- [ ] **Step 3: Run test**

Run:
```powershell
go test ./internal/config/ -v
```

Expected: tests pass

- [ ] **Step 4: Commit**

```powershell
git add -A
git commit -m "feat: add config loader for .tengiz.yaml"
```

---

### Task 3: Runtime — Docker container lifecycle interface + implementation

**Files:**
- Create: `internal/runtime/runtime.go`
- Create: `internal/runtime/runtime_test.go`

- [ ] **Step 1: Write runtime package**

`internal/runtime/runtime.go`:
```go
package runtime

import (
	"context"
	"io"

	"github.com/yaso09/tengiz/internal/types"
)

type Manager interface {
	Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	IsActive(ctx context.Context, name string) (bool, error)
	List(ctx context.Context) ([]types.AppStatus, error)
	Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error)
	WaitForReady(ctx context.Context, name string, internalPort int) error
}

type dockerManager struct {
	client dockerClient
}

type dockerClient interface {
	// will be implemented via Docker SDK
}

func New() Manager {
	return &dockerManager{}
}

func (m *dockerManager) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	// TODO: implement with docker SDK
	return nil
}

func (m *dockerManager) Start(ctx context.Context, name string) error {
	return nil
}

func (m *dockerManager) Stop(ctx context.Context, name string) error {
	return nil
}

func (m *dockerManager) Remove(ctx context.Context, name string) error {
	return nil
}

func (m *dockerManager) IsActive(ctx context.Context, name string) (bool, error) {
	return false, nil
}

func (m *dockerManager) List(ctx context.Context) ([]types.AppStatus, error) {
	return nil, nil
}

func (m *dockerManager) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) {
	return nil, nil
}

func (m *dockerManager) WaitForReady(ctx context.Context, name string, internalPort int) error {
	return nil
}
```

- [ ] **Step 2: Write runtime test with mock**

`internal/runtime/runtime_test.go`:
```go
package runtime

import (
	"testing"
)

func TestNew(t *testing.T) {
	m := New()
	if m == nil {
		t.Fatal("New() returned nil")
	}
}

func TestCreate(t *testing.T) {
	// TODO: integration test requires Docker daemon
	t.Skip("integration test requires Docker daemon")
}
```

- [ ] **Step 3: Run test**

Run:
```powershell
go test ./internal/runtime/ -v
```

Expected: tests pass (skipped integration test)

- [ ] **Step 4: Commit**

```powershell
git add -A
git commit -m "feat: add runtime interface with Docker lifecycle"
```

---

### Task 4: Runtime — Docker SDK implementation

**Files:**
- Modify: `internal/runtime/runtime.go`
- Create: `internal/runtime/docker.go`

- [ ] **Step 1: Write Docker SDK implementation**

`internal/runtime/docker.go`:
```go
package runtime

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/yaso09/tengiz/internal/types"
)

const labelKey = "tengiz-app"

type dockerRuntime struct {
	cli *client.Client
}

func NewDocker() (Manager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &dockerRuntime{cli: cli}, nil
}

func (r *dockerRuntime) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	internalPort := cfg.Port
	if internalPort == 0 {
		internalPort = 8080
	}

	containerName := fmt.Sprintf("tengiz-%s", cfg.Name)
	resp, err := r.cli.ContainerCreate(ctx, &container.Config{
		Image: imageTag,
		Labels: map[string]string{
			labelKey: cfg.Name,
		},
		ExposedPorts: nat.PortSet{
			nat.Port(fmt.Sprintf("%d/tcp", internalPort)): struct{}{},
		},
	}, &container.HostConfig{
		PortBindings: nat.PortMap{
			nat.Port(fmt.Sprintf("%d/tcp", internalPort)): []nat.PortBinding{
				{HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", port)},
			},
		},
		AutoRemove: false,
	}, nil, nil, containerName)
	if err != nil {
		return fmt.Errorf("container create: %w", err)
	}

	return r.cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
}

func (r *dockerRuntime) Start(ctx context.Context, name string) error {
	containerName := fmt.Sprintf("tengiz-%s", name)
	return r.cli.ContainerStart(ctx, containerName, container.StartOptions{})
}

func (r *dockerRuntime) Stop(ctx context.Context, name string) error {
	containerName := fmt.Sprintf("tengiz-%s", name)
	timeout := 5
	return r.cli.ContainerStop(ctx, containerName, container.StopOptions{Timeout: &timeout})
}

func (r *dockerRuntime) Remove(ctx context.Context, name string) error {
	containerName := fmt.Sprintf("tengiz-%s", name)
	return r.cli.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
}

func (r *dockerRuntime) IsActive(ctx context.Context, name string) (bool, error) {
	containerName := fmt.Sprintf("tengiz-%s", name)
	json, err := r.cli.ContainerInspect(ctx, containerName)
	if err != nil {
		return false, nil
	}
	return json.State.Running, nil
}

func (r *dockerRuntime) List(ctx context.Context) ([]types.AppStatus, error) {
	containers, err := r.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}

	var apps []types.AppStatus
	for _, c := range containers {
		appName, ok := c.Labels[labelKey]
		if !ok {
			continue
		}
		state := types.StateStopped
		hostPort := 0
		if c.State == "running" {
			state = types.StateRunning
		}
		for _, p := range c.Ports {
			if p.PublicPort > 0 {
				hostPort = int(p.PublicPort)
				break
			}
		}
		apps = append(apps, types.AppStatus{
			Name:  appName,
			State: state,
			Port:  hostPort,
		})
	}
	return apps, nil
}

func (r *dockerRuntime) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) {
	containerName := fmt.Sprintf("tengiz-%s", name)
	return r.cli.ContainerLogs(ctx, containerName, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: false,
	})
}

func (r *dockerRuntime) WaitForReady(ctx context.Context, name string, internalPort int) error {
	containerName := fmt.Sprintf("tengiz-%s", name)
	var inspect container.InspectResponse
	for {
		var err error
		inspect, err = r.cli.ContainerInspect(ctx, containerName)
		if err != nil {
			return err
		}
		if inspect.State.Running {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	hostPort := 0
	for _, bindings := range inspect.NetworkSettings.Ports {
		if len(bindings) > 0 {
			fmt.Sscanf(bindings[0].HostPort, "%d", &hostPort)
			break
		}
	}
	if hostPort > 0 {
		return waitForPort(ctx, "127.0.0.1", hostPort, 30*time.Second)
	}
	return nil
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


```

- [ ] **Step 2: Run build check**

Run:
```powershell
go mod tidy
go build ./...
```

Expected: builds without errors

- [ ] **Step 3: Commit**

```powershell
git add -A
git commit -m "feat: implement Docker SDK runtime lifecycle"
```

---

### Task 5: Builder — framework detection and Docker image build

**Files:**
- Create: `internal/builder/detect.go`
- Create: `internal/builder/builder.go`
- Create: `internal/builder/builder_test.go`

- [ ] **Step 1: Write framework detector**

`internal/builder/detect.go`:
```go
package builder

import (
	"os"
	"path/filepath"
)

type Framework string

const (
	FrameworkNextJS  Framework = "nextjs"
	FrameworkVite    Framework = "vite"
	FrameworkGo      Framework = "go"
	FrameworkNode    Framework = "node"
	FrameworkPython  Framework = "python"
	FrameworkStatic  Framework = "static"
	FrameworkDocker  Framework = "docker"
)

type Detection struct {
	Framework   Framework
	BuildCmd    string
	OutputDir   string
	InternalPort int
}

func Detect(dir string) (*Detection, error) {
	if hasFile(dir, "Dockerfile") {
		return &Detection{Framework: FrameworkDocker, InternalPort: 8080}, nil
	}
	if hasFile(dir, "next.config.js") || hasFile(dir, "next.config.ts") {
		return &Detection{
			Framework:    FrameworkNextJS,
			BuildCmd:     "npm run build",
			OutputDir:    ".next",
			InternalPort: 3000,
		}, nil
	}
	if hasFile(dir, "vite.config.js") || hasFile(dir, "vite.config.ts") {
		cmd := "npm run build"
		output := "dist"
		return &Detection{
			Framework:    FrameworkVite,
			BuildCmd:     cmd,
			OutputDir:    output,
			InternalPort: 80,
		}, nil
	}
	if hasFile(dir, "go.mod") {
		return &Detection{
			Framework:    FrameworkGo,
			BuildCmd:     "go build -o app .",
			InternalPort: 8080,
		}, nil
	}
	if hasFile(dir, "package.json") {
		return &Detection{
			Framework:    FrameworkNode,
			BuildCmd:     "npm run build",
			InternalPort: 8080,
		}, nil
	}
	if hasFile(dir, "requirements.txt") || hasFile(dir, "Pipfile") || hasFile(dir, "pyproject.toml") {
		return &Detection{
			Framework:    FrameworkPython,
			BuildCmd:     "",
			InternalPort: 8000,
		}, nil
	}
	if hasFile(dir, "index.html") {
		return &Detection{
			Framework:    FrameworkStatic,
			BuildCmd:     "",
			OutputDir:    ".",
			InternalPort: 80,
		}, nil
	}
	return &Detection{Framework: FrameworkStatic, InternalPort: 80}, nil
}

func hasFile(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !info.IsDir()
}
```

- [ ] **Step 2: Write detector test**

`internal/builder/builder_test.go`:
```go
package builder

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectNextJS(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "next.config.js"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkNextJS {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNextJS)
	}
}

func TestDetectDocker(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM node"), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkDocker {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkDocker)
	}
}

func TestDetectGo(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkGo {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkGo)
	}
}

func TestDetectVite(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "vite.config.ts"), []byte(""), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkVite {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkVite)
	}
}

func TestDetectStatic(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkStatic {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkStatic)
	}
}
```

- [ ] **Step 3: Run detector test**

Run:
```powershell
go test ./internal/builder/ -v -run TestDetect
```

Expected: all tests pass

- [ ] **Step 4: Write Dockerfile auto-generator and builder**

`internal/builder/builder.go`:
```go
package builder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Builder struct {
	dataDir string
}

func New(dataDir string) *Builder {
	return &Builder{dataDir: dataDir}
}

func (b *Builder) Build(ctx context.Context, dir string, appName string, detection *Detection) (string, error) {
	if detection.Framework == FrameworkDocker {
		return b.buildWithDockerfile(ctx, dir, appName)
	}
	if err := b.ensureDockerfile(dir, detection); err != nil {
		return "", fmt.Errorf("generate dockerfile: %w", err)
	}
	return b.buildWithDockerfile(ctx, dir, appName)
}

func (b *Builder) ensureDockerfile(dir string, detection *Detection) error {
	dfPath := filepath.Join(dir, "Dockerfile")
	if _, err := os.Stat(dfPath); err == nil {
		return nil
	}
	content, err := generateDockerfile(detection)
	if err != nil {
		return err
	}
	return os.WriteFile(dfPath, []byte(content), 0644)
}

func (b *Builder) buildWithDockerfile(ctx context.Context, dir string, appName string) (string, error) {
	tag := fmt.Sprintf("tengiz-apps/%s:latest", appName)
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", tag, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker build: %w", err)
	}
	return tag, nil
}

func generateDockerfile(d *Detection) (string, error) {
	switch d.Framework {
	case FrameworkNextJS:
		return `FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:20-alpine
WORKDIR /app
COPY --from=builder /app/.next ./.next
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/package.json ./
EXPOSE 3000
CMD ["npm", "start"]`, nil

	case FrameworkVite:
		return `FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]`, nil

	case FrameworkGo:
		return `FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o app .

FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/app .
EXPOSE 8080
CMD ["./app"]`, nil

	case FrameworkNode:
		return `FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
EXPOSE 8080
CMD ["npm", "start"]`, nil

	case FrameworkPython:
		return `FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["python", "app.py"]`, nil

	case FrameworkStatic:
		return `FROM nginx:alpine
COPY . /usr/share/nginx/html
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]`, nil

	default:
		return "", fmt.Errorf("unknown framework: %s", d.Framework)
	}
}
```

- [ ] **Step 5: Run full test suite**

Run:
```powershell
go test ./internal/builder/ -v
```

Expected: all tests pass

- [ ] **Step 6: Commit**

```powershell
git add -A
git commit -m "feat: add builder with framework detection and Dockerfile generation"
```

---

### Task 6: Proxy — reverse proxy with on-demand container routing

**Files:**
- Create: `internal/proxy/proxy.go`
- Create: `internal/proxy/proxy_test.go`

- [ ] **Step 1: Write proxy**

`internal/proxy/proxy.go`:
```go
package proxy

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type Proxy struct {
	mu       sync.RWMutex
	routes   map[string]*route
	runtime  runtime.Manager
	port     int
}

type route struct {
	target *url.URL
	proxy  *httputil.ReverseProxy
	app    string
}

func New(rt runtime.Manager, port int) *Proxy {
	return &Proxy{
		routes:  make(map[string]*route),
		runtime: rt,
		port:    port,
	}
}

func (p *Proxy) Register(app string, targetPort int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	targetURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", targetPort))
	p.routes[app] = &route{
		target: targetURL,
		proxy:  httputil.NewSingleHostReverseProxy(targetURL),
		app:    app,
	}
}

func (p *Proxy) Unregister(app string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.routes, app)
}

func (p *Proxy) extractApp(host string) string {
	host = strings.Split(host, ":")[0]
	parts := strings.SplitN(host, ".", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	app := p.extractApp(r.Host)
	if app == "" {
		http.Error(w, "missing app name in host", http.StatusBadRequest)
		return
	}

	p.mu.RLock()
	rt, ok := p.routes[app]
	p.mu.RUnlock()

	if !ok {
		http.Error(w, fmt.Sprintf("unknown app: %s", app), http.StatusNotFound)
		return
	}

	// Check if container is active, cold start if needed
	active, err := p.runtime.IsActive(r.Context(), app)
	if err != nil || !active {
		log.Printf("[proxy] cold start: %s", app)
		if err := p.runtime.Start(r.Context(), app); err != nil {
			http.Error(w, fmt.Sprintf("cold start failed: %s", err), http.Status502)
			return
		}
		// wait for readiness
		_ = p.runtime.WaitForReady(r.Context(), app, 0)
	}

	rt.proxy.ServeHTTP(w, r)
}

func (p *Proxy) Start(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", p.port)
	log.Printf("[proxy] listening on %s", addr)
	server := &http.Server{
		Addr:    addr,
		Handler: p,
	}
	go func() {
		<-ctx.Done()
		server.Close()
	}()
	return server.ListenAndServe()
}
```

- [ ] **Step 2: Write proxy test with mock runtime**

`internal/proxy/proxy_test.go`:
```go
package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockRuntime struct {
	active bool
}

func (m *mockRuntime) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *mockRuntime) Start(ctx context.Context, name string) error { m.active = true; return nil }
func (m *mockRuntime) Stop(ctx context.Context, name string) error { m.active = false; return nil }
func (m *mockRuntime) Remove(ctx context.Context, name string) error { return nil }
func (m *mockRuntime) IsActive(ctx context.Context, name string) (bool, error) { return m.active, nil }
func (m *mockRuntime) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *mockRuntime) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) { return nil, nil }
func (m *mockRuntime) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }

func TestExtractApp(t *testing.T) {
	p := New(nil, 8080)
	tests := []struct {
		host string
		want string
	}{
		{"myapp.tengiz.local", "myapp"},
		{"myapp.tengiz.local:8080", "myapp"},
		{"tengiz.local", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := p.extractApp(tt.host)
		if got != tt.want {
			t.Errorf("extractApp(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestRegisterAndServe(t *testing.T) {
	mock := &mockRuntime{active: true}
	p := New(mock, 8080)
	p.Register("testapp", 19999)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "testapp.tengiz.local"
	w := httptest.NewRecorder()

	p.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode == http.StatusBadGateway {
		t.Log("cold start attempted (expected with no backend)")
	}
}
```

- [ ] **Step 3: Run test**

Run:
```powershell
go test ./internal/proxy/ -v
```

Expected: tests pass

- [ ] **Step 4: Commit**

```powershell
git add -A
git commit -m "feat: add reverse proxy with on-demand routing"
```

---

### Task 7: Idle timer — scale-to-zero manager

**Files:**
- Create: `internal/idle/idle.go`
- Create: `internal/idle/idle_test.go`

- [ ] **Step 1: Write idle timer manager**

`internal/idle/idle.go`:
```go
package idle

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Manager struct {
	mu      sync.Mutex
	timers  map[string]*time.Timer
	runtime runtime.Manager
	timeout time.Duration
}

func New(rt runtime.Manager, timeout time.Duration) *Manager {
	return &Manager{
		timers:  make(map[string]*time.Timer),
		runtime: rt,
		timeout: timeout,
	}
}

func (m *Manager) Reset(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t, ok := m.timers[name]; ok {
		t.Stop()
		t.Reset(m.timeout)
		return
	}

	m.timers[name] = time.AfterFunc(m.timeout, func() {
		m.stopApp(name)
	})
}

func (m *Manager) Stop(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t, ok := m.timers[name]; ok {
		t.Stop()
		delete(m.timers, name)
	}
}

func (m *Manager) stopApp(name string) {
	m.mu.Lock()
	delete(m.timers, name)
	m.mu.Unlock()

	log.Printf("[idle] stopping %s (idle timeout)", name)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.runtime.Stop(ctx, name); err != nil {
		log.Printf("[idle] error stopping %s: %v", name, err)
	}
}
```

- [ ] **Step 2: Write idle timer test**

`internal/idle/idle_test.go`:
```go
package idle

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/types"
)

type mockRuntime struct {
	stopped atomic.Bool
}

func (m *mockRuntime) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *mockRuntime) Start(ctx context.Context, name string) error { return nil }
func (m *mockRuntime) Stop(ctx context.Context, name string) error { m.stopped.Store(true); return nil }
func (m *mockRuntime) Remove(ctx context.Context, name string) error { return nil }
func (m *mockRuntime) IsActive(ctx context.Context, name string) (bool, error) { return false, nil }
func (m *mockRuntime) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *mockRuntime) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) { return nil, nil }
func (m *mockRuntime) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }

func TestResetExtendsTimer(t *testing.T) {
	mock := &mockRuntime{}
	mgr := New(mock, 50*time.Millisecond)

	mgr.Reset("testapp")
	time.Sleep(30 * time.Millisecond)
	mgr.Reset("testapp") // reset before expiry
	time.Sleep(30 * time.Millisecond)
	mgr.Reset("testapp") // reset again
	time.Sleep(30 * time.Millisecond)

	if mock.stopped.Load() {
		t.Error("app stopped too early, Reset() did not extend timer")
	}

	// wait for final timer to expire
	time.Sleep(60 * time.Millisecond)
	if !mock.stopped.Load() {
		t.Error("app was not stopped after idle timeout")
	}
}

func TestStopPreventsTimeout(t *testing.T) {
	mock := &mockRuntime{}
	mgr := New(mock, 50*time.Millisecond)

	mgr.Reset("testapp")
	mgr.Stop("testapp")
	time.Sleep(100 * time.Millisecond)

	if mock.stopped.Load() {
		t.Error("app was stopped despite Stop() being called")
	}
}
```

- [ ] **Step 3: Run test**

Run:
```powershell
go test ./internal/idle/ -v -count=1
```

Expected: all pass

- [ ] **Step 4: Commit**

```powershell
git add -A
git commit -m "feat: add idle timer manager for scale-to-zero"
```

---

### Task 8: CLI — cobra commands

**Files:**
- Modify: `cmd/tengiz/main.go`
- Create: `internal/config/store.go`

- [ ] **Step 1: Write state store for persistence**

`internal/config/store.go`:
```go
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/yaso09/tengiz/internal/types"
)

type Store struct {
	mu       sync.Mutex
	dataDir  string
}

func NewStore(dataDir string) *Store {
	os.MkdirAll(dataDir, 0755)
	return &Store{dataDir: dataDir}
}

func (s *Store) SaveApp(app types.AppEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	apps[app.Name] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) RemoveApp(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	delete(apps, name)
	return s.writeJSON("apps.json", apps)
}

func (s *Store) ListApps() ([]types.AppEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	result := make([]types.AppEntry, 0, len(apps))
	for _, v := range apps {
		result = append(result, v)
	}
	return result, nil
}

func (s *Store) AllocatePort(appName string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ports := make(map[int]string)
	s.readJSON("ports.json", &ports)

	for p := 9000; p <= 9999; p++ {
		if _, used := ports[p]; !used {
			ports[p] = appName
			if err := s.writeJSON("ports.json", ports); err != nil {
				return 0, err
			}
			return p, nil
		}
	}
	return 0, nil
}

func (s *Store) FreePort(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ports := make(map[int]string)
	s.readJSON("ports.json", &ports)
	delete(ports, port)
	return s.writeJSON("ports.json", ports)
}

func (s *Store) readJSON(name string, v interface{}) {
	data, err := os.ReadFile(filepath.Join(s.dataDir, name))
	if err == nil {
		json.Unmarshal(data, v)
	}
}

func (s *Store) writeJSON(name string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dataDir, name), data, 0644)
}
```

- [ ] **Step 2: Write full CLI commands**

`cmd/tengiz/main.go` (overwrite):
```go
package tengiz

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/idle"
	"github.com/yaso09/tengiz/internal/proxy"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

var dataDir string

func init() {
	home, _ := os.UserHomeDir()
	dataDir = filepath.Join(home, ".tengiz")
	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(rmCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.PersistentFlags().String("dir", ".", "project directory")
}

var rootCmd = &cobra.Command{
	Use:   "tengiz",
	Short: "Tengiz - Serverless deployment platform",
}

var deployCmd = &cobra.Command{
	Use:   "deploy [directory]",
	Short: "Build and deploy an application",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) > 0 {
			dir = args[0]
		}

		projectRoot, err := config.FindProjectRoot(dir)
		if err != nil {
			return fmt.Errorf("project root: %w", err)
		}

		cfg, err := config.Load(projectRoot)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}

		fmt.Printf("[tengiz] deploying %s from %s\n", cfg.Name, projectRoot)

		detection, err := builder.Detect(projectRoot)
		if err != nil {
			return fmt.Errorf("detect: %w", err)
		}
		fmt.Printf("[tengiz] detected: %s\n", detection.Framework)

		b := builder.New(dataDir)
		imageTag, err := b.Build(context.Background(), projectRoot, cfg.Name, detection)
		if err != nil {
			return fmt.Errorf("build: %w", err)
		}
		fmt.Printf("[tengiz] built image: %s\n", imageTag)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStore(dataDir)
		port, err := store.AllocatePort(cfg.Name)
		if err != nil {
			return fmt.Errorf("port: %w", err)
		}

		if err := rt.Create(context.Background(), cfg, imageTag, port); err != nil {
			return fmt.Errorf("create: %w", err)
		}
		fmt.Printf("[tengiz] running on port %d\n", port)

		store.SaveApp(types.AppEntry{
			Name:     cfg.Name,
			ImageTag: imageTag,
			Port:     port,
			Domains:  cfg.Domains,
			Config:   *cfg,
		})

		fmt.Printf("[tengiz] deployed: %s accessible at http://%s.tengiz.local:%d\n",
			cfg.Name, cfg.Name, port)
		return nil
	},
}

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Start the reverse proxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		p := proxy.New(rt, 8080)

		idleMgr := idle.New(rt, 5*time.Minute)
		p.SetIdleManager(idleMgr)

		store := config.NewStore(dataDir)
		apps, err := store.ListApps()
		if err == nil {
			for _, app := range apps {
				p.Register(app.Name, app.Port)
				fmt.Printf("[tengiz] route: %s -> :%d\n", app.Name, app.Port)
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		go func() {
			<-sig
			cancel()
		}()

		return p.Start(ctx)
	},
}

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List deployed applications",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		apps, err := rt.List(context.Background())
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}

		if len(apps) == 0 {
			fmt.Println("No applications deployed.")
			return nil
		}

		fmt.Printf("%-20s %-10s %-8s\n", "NAME", "STATE", "PORT")
		for _, a := range apps {
			fmt.Printf("%-20s %-10s %-8d\n", a.Name, a.State, a.Port)
		}
		return nil
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop <app>",
	Short: "Stop an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		return rt.Stop(context.Background(), args[0])
	},
}

var startCmd = &cobra.Command{
	Use:   "start <app>",
	Short: "Start a stopped application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		return rt.Start(context.Background(), args[0])
	},
}

var rmCmd = &cobra.Command{
	Use:   "rm <app>",
	Short: "Remove an application completely",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		store := config.NewStore(dataDir)
		if err := rt.Remove(context.Background(), args[0]); err != nil {
			return err
		}
		store.RemoveApp(args[0])
		fmt.Printf("[tengiz] removed: %s\n", args[0])
		return nil
	},
}

var logsCmd = &cobra.Command{
	Use:   "logs [-f] <app>",
	Short: "Show application logs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		follow, _ := cmd.Flags().GetBool("follow")
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		reader, err := rt.Logs(context.Background(), args[0], follow)
		if err != nil {
			return err
		}
		defer reader.Close()
		_, err = os.Stdout.ReadFrom(reader)
		return err
	},
}

func Execute() {
	logsCmd.Flags().BoolP("follow", "f", false, "follow log output")
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Build check**

Run:
```powershell
go mod tidy
go build ./...
```

Expected: builds without errors

- [ ] **Step 3: Commit**

```powershell
git add -A
git commit -m "feat: add all CLI commands (deploy, proxy, ps, stop, start, rm, logs)"
```

---

### Task 9: Wire idle timer into proxy + final integration

**Files:**
- Modify: `internal/proxy/proxy.go` — add idle manager integration
- Modify: `internal/proxy/proxy_test.go` — update test

- [ ] **Step 1: Update proxy to integrate idle timer**

`internal/proxy/proxy.go` — replace `ServeHTTP` and add `SetIdleManager`:
```go
package proxy

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/yaso09/tengiz/internal/idle"
	"github.com/yaso09/tengiz/internal/runtime"
)

type Proxy struct {
	mu          sync.RWMutex
	routes      map[string]*route
	runtime     runtime.Manager
	port        int
	idleManager *idle.Manager
}

type route struct {
	target *url.URL
	proxy  *httputil.ReverseProxy
	app    string
}

func New(rt runtime.Manager, port int) *Proxy {
	return &Proxy{
		routes:  make(map[string]*route),
		runtime: rt,
		port:    port,
	}
}

func (p *Proxy) SetIdleManager(m *idle.Manager) {
	p.idleManager = m
}

func (p *Proxy) Register(app string, targetPort int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	targetURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", targetPort))
	p.routes[app] = &route{
		target: targetURL,
		proxy:  httputil.NewSingleHostReverseProxy(targetURL),
		app:    app,
	}
}

func (p *Proxy) Unregister(app string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.routes, app)
}

func (p *Proxy) extractApp(host string) string {
	host = strings.Split(host, ":")[0]
	parts := strings.SplitN(host, ".", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	app := p.extractApp(r.Host)
	if app == "" {
		http.Error(w, "missing app name in host", http.StatusBadRequest)
		return
	}

	p.mu.RLock()
	rt, ok := p.routes[app]
	p.mu.RUnlock()

	if !ok {
		http.Error(w, fmt.Sprintf("unknown app: %s", app), http.StatusNotFound)
		return
	}

	active, err := p.runtime.IsActive(r.Context(), app)
	if err != nil || !active {
		log.Printf("[proxy] cold start: %s", app)
		if err := p.runtime.Start(r.Context(), app); err != nil {
			http.Error(w, fmt.Sprintf("cold start failed: %s", err), http.StatusBadGateway)
			return
		}
		if err := p.runtime.WaitForReady(r.Context(), app, 0); err != nil {
			log.Printf("[proxy] wait for ready: %v", err)
		}
	}

	if p.idleManager != nil {
		p.idleManager.Reset(app)
	}

	rt.proxy.ServeHTTP(w, r)
}

func (p *Proxy) Start(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", p.port)
	log.Printf("[proxy] listening on %s", addr)
	server := &http.Server{
		Addr:    addr,
		Handler: p,
	}
	go func() {
		<-ctx.Done()
		server.Close()
	}()
	return server.ListenAndServe()
}
```

- [ ] **Step 2: Update proxy test**

`internal/proxy/proxy_test.go`:
```go
package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yaso09/tengiz/internal/idle"
	"github.com/yaso09/tengiz/internal/types"
)

type mockRuntime struct {
	active bool
}

func (m *mockRuntime) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *mockRuntime) Start(ctx context.Context, name string) error { m.active = true; return nil }
func (m *mockRuntime) Stop(ctx context.Context, name string) error { m.active = false; return nil }
func (m *mockRuntime) Remove(ctx context.Context, name string) error { return nil }
func (m *mockRuntime) IsActive(ctx context.Context, name string) (bool, error) { return m.active, nil }
func (m *mockRuntime) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *mockRuntime) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) { return nil, nil }
func (m *mockRuntime) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }

func TestExtractApp(t *testing.T) {
	p := New(nil, 8080)
	tests := []struct {
		host string
		want string
	}{
		{"myapp.tengiz.local", "myapp"},
		{"myapp.tengiz.local:8080", "myapp"},
		{"tengiz.local", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := p.extractApp(tt.host)
		if got != tt.want {
			t.Errorf("extractApp(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestRegisterAndServe(t *testing.T) {
	mock := &mockRuntime{active: true}
	p := New(mock, 8080)
	p.Register("testapp", 19999)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "testapp.tengiz.local"
	w := httptest.NewRecorder()

	p.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode == http.StatusBadGateway {
		t.Log("cold start attempted (expected with no backend)")
	}
}

func TestIdleResetOnRequest(t *testing.T) {
	mock := &mockRuntime{active: true}
	p := New(mock, 8080)
	mgr := idle.New(mock, 0)
	p.SetIdleManager(mgr)
	p.Register("testapp", 19999)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "testapp.tengiz.local"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
}
```

- [ ] **Step 3: Install dependencies and build**

Run:
```powershell
go mod tidy
go build -o tengiz.exe .
```

Expected: builds without errors

- [ ] **Step 4: Run all tests**

Run:
```powershell
go test ./... -v -count=1
```

Expected: all passing

- [ ] **Step 5: Commit**

```powershell
git add -A
git commit -m "feat: wire idle timer into proxy, final integration"
```

---

## Spec Coverage Check

| Spec Requirement | Task |
|---|---|
| CLI-based deployment engine | Task 1, Task 8 |
| .tengiz.yaml config | Task 2 |
| Framework detection + Dockerfile generation | Task 5 |
| Docker image build | Task 5 |
| Container lifecycle (create, start, stop, remove) | Task 3, Task 4 |
| Reverse proxy with host-based routing | Task 6 |
| Scale-to-zero (idle timer) | Task 7 |
| Cold start on request | Task 6 (integrated) |
| State persistence (ports, apps) | Task 8 (store) |
| Log streaming | Task 8 (logs cmd) |
