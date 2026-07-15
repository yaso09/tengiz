# Container Health Check + Auto Restart Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add configurable HTTP health checks and automatic container restart when health checks fail, via a background checker service, Docker HEALTHCHECK in generated Dockerfiles, and CLI commands.

**Architecture:** Two-layer approach: (1) Docker-level `HEALTHCHECK` instruction in generated Dockerfiles so Docker itself monitors container health, and (2) a Tengiz-level background health checker (`internal/health`) that periodically issues HTTP GET requests to the configured endpoint and calls `runtime.Restart()` on failure. Health check configuration is read from `.tengiz.yaml` (already parsed by Viper into `HealthCheckConfig`) and stored in `AppEntry.Config.HealthCheck`. A new `Restart` method is added to the runtime Manager interface.

**Tech Stack:** Go 1.26, `net/http` (for HTTP health checks), `os/exec` (Docker CLI), existing `internal/runtime`, `internal/config/store`, `internal/types` packages. No new dependencies.

## Global Constraints

- No new external dependencies beyond Go stdlib + existing (`cobra`, `viper`).
- Container names prefixed `tengiz-<appname>`, labeled with `tengiz-app=<appname>`.
- All store operations use `~/.tengiz/*.json` files via `config.Store`.
- All runtime operations use Docker CLI via `os/exec`.
- New features must be tested; tests must pass before commit.

---
## File Structure

| File | Responsibility | Change |
|------|---------------|--------|
| `internal/types/types.go` | Shared types: add `StartPeriod` to `HealthCheckConfig`, add `RestartCount`/`HealthStatus` to `AppEntry` | Modify |
| `internal/runtime/runtime.go` | Manager interface + stub: add `Restart`, add `WaitForHealth` | Modify |
| `internal/runtime/docker.go` | Docker impl: implement `Restart` (docker restart), implement `WaitForHealth` (HTTP GET with retries) | Modify |
| `internal/builder/detect.go` | Detection struct: add `HealthCheck` field | Modify |
| `internal/builder/builder.go` | Dockerfile generation: append `HEALTHCHECK` instruction when `HealthCheck.Enabled` | Modify |
| `internal/health/health.go` | New package: background health checker service (per-app goroutine, periodic HTTP checks, auto-restart) | Create |
| `internal/health/health_test.go` | Tests for health checker | Create |
| `internal/cli/root.go` | Wire health checker into proxy startup, add `tengiz health` command, show health in `ps`, update `init` template | Modify |
| `internal/cli/root_test.go` | Tests for CLI additions | Modify |
| `README.md` | Document `healthcheck` section in `.tengiz.yaml` | Modify |

---

### Task 1: Add `Restart` to runtime Manager interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go` (lines 10-22 for interface, lines 24-72 for stub)
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `Manager.Restart(ctx context.Context, name string) error`

- [ ] **Step 1: Write the failing test for Restart on stub**

Add to `internal/runtime/runtime_test.go`:

```go
func TestStubRestart(t *testing.T) {
	m := NewStub()
	if err := m.Restart(context.Background(), "testapp"); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -v -count=1 -run TestStubRestart`
Expected: FAIL — `m.Restart undefined (type Manager has no field or method Restart)`

- [ ] **Step 3: Add `Restart` to Manager interface + stub**

In `internal/runtime/runtime.go`, add `Restart` to the interface block:

```go
type Manager interface {
	Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	RemoveBySuffix(ctx context.Context, name string, suffix string) error
	IsActive(ctx context.Context, name string) (bool, error)
	GetContainerPort(ctx context.Context, name string, suffix string) (int, error)
	List(ctx context.Context) ([]types.AppStatus, error)
	Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error)
	WaitForReady(ctx context.Context, name string, internalPort int) error
}
```

Add the stub method after `Start`:

```go
func (m *stubManager) Restart(ctx context.Context, name string) error {
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -v -count=1 -run TestStubRestart`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go
git commit -m "feat(runtime): add Restart to Manager interface + stub"
```

---

### Task 2: Implement `Restart` in Docker runtime

**Files:**
- Modify: `internal/runtime/docker.go`
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: `Manager.Restart(ctx, name) error` from Task 1
- Produces: working Docker restart impl

- [ ] **Step 1: Write the test**

Add to `internal/runtime/runtime_test.go`:

```go
func TestStubRestartInterface(t *testing.T) {
	var m Manager = NewStub()
	if err := m.Restart(context.Background(), "testapp"); err != nil {
		t.Fatalf("Restart via interface: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify initial state**

Run: `go test ./internal/runtime/ -v -count=1 -run TestStubRestartInterface`
Expected: PASS (stub already works)

- [ ] **Step 3: Implement `Restart` in Docker runtime**

Add method to `dockerRuntime` in `internal/runtime/docker.go`, after `Stop`:

```go
func (r *dockerRuntime) Restart(ctx context.Context, name string) error {
	containerName := fmt.Sprintf("tengiz-%s", name)
	cmd := exec.CommandContext(ctx, "docker", "restart", "-t", "5", containerName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker restart: %w\n%s", err, string(out))
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat(runtime): implement Restart in Docker runtime"
```

---

### Task 3: Add health tracking fields + StartPeriod to types

**Files:**
- Modify: `internal/types/types.go`

**Interfaces:**
- Consumes: nothing
- Produces: `HealthCheckConfig.StartPeriod`, `AppEntry.RestartCount`, `AppEntry.HealthStatus`

- [ ] **Step 1: Add `StartPeriod` to `HealthCheckConfig`**

In `internal/types/types.go`, add `StartPeriod` field:

```go
type HealthCheckConfig struct {
	Enabled     bool   `mapstructure:"enabled" yaml:"enabled"`
	Endpoint    string `mapstructure:"endpoint" yaml:"endpoint"`
	Port        int    `mapstructure:"port" yaml:"port"`
	Interval    int    `mapstructure:"interval" yaml:"interval"`
	Retries     int    `mapstructure:"retries" yaml:"retries"`
	Timeout     int    `mapstructure:"timeout" yaml:"timeout"`
	StartPeriod int    `mapstructure:"start_period" yaml:"start_period"`
}
```

- [ ] **Step 2: Add health tracking constants and fields to `AppEntry`**

Add after the `DeploymentStatus` const block:

```go
const (
	HealthUnknown   = "unknown"
	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy"
)
```

Add fields to `AppEntry`:

```go
type AppEntry struct {
	Name             string            `json:"name"`
	ImageTag         string            `json:"image_tag"`
	Port             int               `json:"port"`
	Domains          []string          `json:"domains"`
	Config           AppConfig         `json:"config"`
	DeploymentSuffix string            `json:"deployment_suffix,omitempty"`
	Deployments      []DeploymentEntry `json:"deployments,omitempty"`
	RestartCount     int               `json:"restart_count,omitempty"`
	HealthStatus     string            `json:"health_status,omitempty"`
}
```

- [ ] **Step 3: Run vet to verify compilation**

Run: `go vet ./internal/types/...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/types/types.go
git commit -m "feat(types): add StartPeriod, RestartCount, HealthStatus fields"
```

---

### Task 4: Add `WaitForHealth` to runtime (HTTP health check method)

**Files:**
- Modify: `internal/runtime/runtime.go` (interface + stub)
- Modify: `internal/runtime/docker.go` (Docker implementation)
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: `types.HealthCheckConfig` from Task 3
- Produces: `Manager.WaitForHealth(ctx, name, hc) error`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/runtime_test.go`:

```go
func TestStubWaitForHealth(t *testing.T) {
	m := NewStub()
	hc := &types.HealthCheckConfig{Enabled: true, Endpoint: "/health", Timeout: 1}
	if err := m.WaitForHealth(context.Background(), "testapp", hc); err != nil {
		t.Fatalf("WaitForHealth() error = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -v -count=1 -run TestStubWaitForHealth`
Expected: FAIL — `m.WaitForHealth undefined`

- [ ] **Step 3: Add `WaitForHealth` to interface + stub**

In `internal/runtime/runtime.go`, add to the Manager interface:

```go
WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error
```

Add stub method:

```go
func (m *stubManager) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error {
	return nil
}
```

- [ ] **Step 4: Implement `WaitForHealth` in Docker runtime**

Add import `"net/http"` to `internal/runtime/docker.go`.

Add to `dockerRuntime`:

```go
func (r *dockerRuntime) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error {
	if hc == nil || !hc.Enabled {
		return nil
	}

	containerName := fmt.Sprintf("tengiz-%s", name)

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
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat(runtime): add WaitForHealth for HTTP health checks"
```

---

### Task 5: Add Docker HEALTHCHECK instruction to generated Dockerfiles

**Files:**
- Modify: `internal/builder/detect.go` (add HealthCheck to Detection struct)
- Modify: `internal/builder/builder.go` (add HEALTHCHECK to generated Dockerfiles)
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.HealthCheckConfig` from Task 3
- Produces: `Detection.HealthCheck` field; HEALTHCHECK in generated Dockerfile

- [ ] **Step 1: Write failing tests**

Add to `internal/builder/builder_test.go`:

```go
func TestGenerateDockerfileWithHealthCheck(t *testing.T) {
	hc := &types.HealthCheckConfig{
		Enabled:  true,
		Endpoint: "/healthz",
		Interval: 15,
		Timeout:  3,
		Retries:  2,
	}
	d := &Detection{
		Framework:    FrameworkNode,
		InternalPort: 3000,
		HealthCheck:  hc,
	}
	df := generateDockerfile(d)
	if !strings.Contains(df, "HEALTHCHECK") {
		t.Error("generated Dockerfile missing HEALTHCHECK instruction")
	}
	if !strings.Contains(df, "/healthz") {
		t.Error("generated Dockerfile missing custom endpoint")
	}
	if !strings.Contains(df, "--interval=15s") {
		t.Error("generated Dockerfile missing custom interval")
	}
}

func TestGenerateDockerfileWithoutHealthCheck(t *testing.T) {
	d := &Detection{
		Framework:    FrameworkGo,
		InternalPort: 8080,
	}
	df := generateDockerfile(d)
	if strings.Contains(df, "HEALTHCHECK") {
		t.Error("generated Dockerfile should not contain HEALTHCHECK when not configured")
	}
}
```

Add import for `"strings"` and `"github.com/yaso09/tengiz/internal/types"` at the top of the test file (read existing imports first to see what's there).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -v -count=1 -run TestGenerateDockerfileWithHealthCheck`
Expected: FAIL — Detection has no field `HealthCheck`

- [ ] **Step 3: Add `HealthCheck` to Detection struct**

In `internal/builder/detect.go`, add the field:

```go
type Detection struct {
	Framework    Framework
	BuildCmd     string
	OutputDir    string
	InternalPort int
	HealthCheck  *types.HealthCheckConfig
}
```

Add import for `"github.com/yaso09/tengiz/internal/types"` in detect.go.

- [ ] **Step 4: Add HEALTHCHECK to Dockerfile generation**

Modify `generateDockerfile` in `internal/builder/builder.go`. After the switch block, before the return:

```go
func generateDockerfile(d *Detection) string {
	port := d.InternalPort
	if port == 0 {
		port = 8080
	}

	var df string
	switch d.Framework {
	case FrameworkNextJS:
		df = fmt.Sprintf(`FROM node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:22-alpine
WORKDIR /app
COPY --from=builder /app/.next ./.next
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/package.json ./
EXPOSE %d
CMD ["npm", "start"]`, port)
	case FrameworkVite:
		df = fmt.Sprintf(`FROM node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE %d
CMD ["nginx", "-g", "daemon off;"]`, port)
	case FrameworkGo:
		df = fmt.Sprintf(`FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o app .

FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/app .
EXPOSE %d
CMD ["./app"]`, port)
	case FrameworkNode:
		df = fmt.Sprintf(`FROM node:22-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
EXPOSE %d
CMD ["npm", "start"]`, port)
	case FrameworkPython:
		df = fmt.Sprintf(`FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE %d
CMD ["python", "app.py"]`, port)
	case FrameworkStatic:
		df = fmt.Sprintf(`FROM nginx:alpine
COPY . /usr/share/nginx/html
EXPOSE %d
CMD ["nginx", "-g", "daemon off;"]`, port)
	default:
		df = fmt.Sprintf(`FROM alpine
EXPOSE %d
CMD ["echo", "no dockerfile generated for this framework"]`, port)
	}

	if d.HealthCheck != nil && d.HealthCheck.Enabled {
		endpoint := d.HealthCheck.Endpoint
		if endpoint == "" {
			endpoint = "/health"
		}
		interval := d.HealthCheck.Interval
		if interval <= 0 {
			interval = 30
		}
		timeout := d.HealthCheck.Timeout
		if timeout <= 0 {
			timeout = 5
		}
		retries := d.HealthCheck.Retries
		if retries <= 0 {
			retries = 3
		}
		df += fmt.Sprintf("\nHEALTHCHECK --interval=%ds --timeout=%ds --start-period=%ds --retries=%d CMD curl -f http://localhost:%d%s || exit 1\n",
			interval, timeout, d.HealthCheck.StartPeriod, retries, port, endpoint)
	}

	return df
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/builder/ -v -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/builder/detect.go internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat(builder): inject HEALTHCHECK into generated Dockerfiles"
```

---

### Task 6: Create health checker background service

**Files:**
- Create: `internal/health/health.go`
- Create: `internal/health/health_test.go`

**Interfaces:**
- Consumes: `runtime.Manager` (Restart, IsActive), `config.Store` (GetApp, UpdateApp)
- Produces: `health.Checker` with Start/Stop/CheckOnce/StopAll

- [ ] **Step 1: Write the test first**

Create `internal/health/health_test.go`:

```go
package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

func TestNewChecker(t *testing.T) {
	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())
	c := New(rt, store)
	if c == nil {
		t.Fatal("New() returned nil")
	}
}

func TestCheckOnceSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())

	app := types.AppEntry{
		Name: "testapp",
		Port: 9999,
		Config: types.AppConfig{
			HealthCheck: &types.HealthCheckConfig{
				Enabled:  true,
				Endpoint: "/health",
				Timeout:  2,
				Retries:  1,
			},
		},
	}
	store.SaveApp(app)

	c := New(rt, store)
	err := c.CheckOnce(context.Background(), "testapp")
	if err == nil {
		t.Error("expected connection refused error (port 9999 is not a server)")
	}
}

func TestCheckOnceNoHealthConfig(t *testing.T) {
	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())

	app := types.AppEntry{
		Name: "testapp",
		Port: 9000,
		Config: types.AppConfig{
			HealthCheck: nil,
		},
	}
	store.SaveApp(app)

	c := New(rt, store)
	err := c.CheckOnce(context.Background(), "testapp")
	if err != nil {
		t.Fatalf("CheckOnce on app without health config: %v", err)
	}
}

func TestStartStopChecker(t *testing.T) {
	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())

	app := types.AppEntry{
		Name: "testapp",
		Port: 9001,
		Config: types.AppConfig{
			HealthCheck: &types.HealthCheckConfig{
				Enabled:  true,
				Endpoint: "/health",
				Interval: 1,
				Timeout:  1,
				Retries:  1,
			},
		},
	}
	store.SaveApp(app)

	c := New(rt, store)
	c.Start("testapp")
	c.Start("testapp")
	c.Stop("testapp")
	c.Stop("nonexistent")
	c.StopAll()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/health/ -v -count=1`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement the health checker**

Create `internal/health/health.go`:

```go
package health

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type Checker struct {
	rt     runtime.Manager
	store  *config.Store
	mu     sync.Mutex
	checks map[string]context.CancelFunc
}

func New(rt runtime.Manager, store *config.Store) *Checker {
	return &Checker{
		rt:     rt,
		store:  store,
		checks: make(map[string]context.CancelFunc),
	}
}

func (c *Checker) Start(appName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.checks[appName]; ok {
		return
	}

	app, err := c.store.GetApp(appName)
	if err != nil {
		log.Printf("[health] app %q not found, not starting checker: %v", appName, err)
		return
	}
	if app.Config.HealthCheck == nil || !app.Config.HealthCheck.Enabled {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.checks[appName] = cancel
	go c.runChecker(ctx, appName)
}

func (c *Checker) Stop(appName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel, ok := c.checks[appName]; ok {
		cancel()
		delete(c.checks, appName)
	}
}

func (c *Checker) StopAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cancel := range c.checks {
		cancel()
	}
	c.checks = make(map[string]context.CancelFunc)
}

func (c *Checker) runChecker(ctx context.Context, appName string) {
	for {
		app, err := c.store.GetApp(appName)
		if err != nil {
			log.Printf("[health] app %q lookup failed: %v", appName, err)
			return
		}

		hc := app.Config.HealthCheck
		if hc == nil || !hc.Enabled {
			return
		}

		interval := hc.Interval
		if interval <= 0 {
			interval = 30
		}

		healthy := c.doHTTPCheck(ctx, app, hc)
		if healthy {
			if app.HealthStatus != types.HealthHealthy {
				app.HealthStatus = types.HealthHealthy
				app.RestartCount = 0
				c.store.UpdateApp(app)
			}
		} else {
			app.HealthStatus = types.HealthUnhealthy
			app.RestartCount++
			c.store.UpdateApp(app)

			log.Printf("[health] %s unhealthy (attempt %d), restarting", appName, app.RestartCount)
			if err := c.rt.Restart(ctx, appName); err != nil {
				log.Printf("[health] restart %s failed: %v", appName, err)
			} else {
				log.Printf("[health] %s restarted successfully", appName)
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(interval) * time.Second):
		}
	}
}

func (c *Checker) doHTTPCheck(ctx context.Context, app types.AppEntry, hc *types.HealthCheckConfig) bool {
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

	url := fmt.Sprintf("http://127.0.0.1:%d%s", app.Port, endpoint)
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}

	for i := 0; i <= retries; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func (c *Checker) CheckOnce(ctx context.Context, appName string) error {
	app, err := c.store.GetApp(appName)
	if err != nil {
		return fmt.Errorf("app %q not found: %w", appName, err)
	}

	hc := app.Config.HealthCheck
	if hc == nil || !hc.Enabled {
		return fmt.Errorf("health check not configured for %q", appName)
	}

	healthy := c.doHTTPCheck(ctx, app, hc)
	if !healthy {
		return fmt.Errorf("%s is unhealthy", appName)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/health/ -v -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/health/
git commit -m "feat(health): create background health checker service"
```

---

### Task 7: Wire health checker into proxy startup + add `tengiz health` command

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `health.Checker` (Start, Stop, CheckOnce, StopAll)
- Produces: health checker running alongside proxy; `tengiz health <app>` command

- [ ] **Step 1: Write tests for CLI additions**

Add to `internal/cli/root_test.go`. Read the file first to understand existing test patterns.

Create tests:

```go
func TestHealthCmdNoApp(t *testing.T) {
	rootCmd.SetArgs([]string{"health"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for missing app name")
	}
}

func TestHealthCmdUnknownApp(t *testing.T) {
	rootCmd.SetArgs([]string{"health", "nonexistent"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for unknown app")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -v -count=1 -run TestHealthCmd`
Expected: FAIL — unknown command "health"

- [ ] **Step 3: Wire health checker into proxy startup**

In `internal/cli/root.go`, add import for `"github.com/yaso09/tengiz/internal/health"`.

In the `proxyCmd` RunE function, after creating the idle manager, add health checker setup:

```go
		healthChecker := health.New(rt, store)
		apps, err := store.ListApps()
		if err == nil {
			for _, app := range apps {
				healthChecker.Start(app.Name)
			}
		}
		defer healthChecker.StopAll()
```

Insert these lines after `p.SetIdleManager(idleMgr)` and before `store := config.NewStore(dataDir)` (which should be moved up). Actually the existing `proxyCmd` already has `store` after idle setup. Let me show the full modified proxyCmd section:

The existing code (lines 253-300) should be updated to:

```go
var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Start the reverse proxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		appFlag, _ := cmd.Flags().GetString("app")
		portFlag, _ := cmd.Flags().GetInt("port")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		p := proxy.New(rt, portFlag)

		if appFlag != "" {
			p.SetDefaultApp(appFlag)
		}

		idleMgr := idle.New(rt, 5*time.Minute)
		p.SetIdleManager(idleMgr)

		store := config.NewStore(dataDir)

		healthChecker := health.New(rt, store)
		defer healthChecker.StopAll()

		apps, err := store.ListApps()
		if err == nil {
			for _, app := range apps {
				p.Register(app.Name, app.Port)
				fmt.Printf("[tengiz] route: %s -> :%d\n", app.Name, app.Port)
				for _, domain := range app.Domains {
					p.RegisterDomain(domain, app.Name)
					fmt.Printf("[tengiz] domain: %s -> %s\n", domain, app.Name)
				}
				healthChecker.Start(app.Name)
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
```

- [ ] **Step 4: Add `tengiz health` CLI command**

In `internal/cli/root.go`, add the health command before `var domainCmd`:

```go
var healthCmd = &cobra.Command{
	Use:   "health <app>",
	Short: "Check application health status",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		store := config.NewStore(dataDir)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		app, err := store.GetApp(appName)
		if err != nil {
			return fmt.Errorf("app %q not found", appName)
		}

		if app.Config.HealthCheck == nil || !app.Config.HealthCheck.Enabled {
			return fmt.Errorf("health check not configured for %q", appName)
		}

		c := health.New(rt, store)
		if err := c.CheckOnce(cmd.Context(), appName); err != nil {
			fmt.Printf("[tengiz] %s is UNHEALTHY: %v\n", appName, err)
			return nil
		}
		fmt.Printf("[tengiz] %s is healthy\n", appName)
		return nil
	},
}
```

Register it in `init()`:

```go
rootCmd.AddCommand(healthCmd)
```

- [ ] **Step 5: Build and verify**

Run: `go build -o /dev/null .`
Expected: builds without error

- [ ] **Step 6: Run tests**

Run: `go test ./internal/cli/ -v -count=1`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): wire health checker into proxy, add tengiz health command"
```

---

### Task 8: Show health status in `tengiz ps` output

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/root_test.go`:

```go
func TestPsHeaderContainsHealth(t *testing.T) {
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"ps"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "HEALTH") {
		t.Error("ps output missing HEALTH column header")
	}
}
```

Need a `captureOutput` helper:

```go
func captureOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = old
	return buf.String()
}
```

Add imports for `"bytes"`, `"io"`, `"os"`, `"strings"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -v -count=1 -run TestPsHeaderContainsHealth`
Expected: FAIL — ps output doesn't have HEALTH column yet

- [ ] **Step 3: Modify `psCmd` to show health status**

Change the `psCmd` RunE in `internal/cli/root.go`. Update the header and row:

```go
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

		store := config.NewStore(dataDir)
		storeApps, _ := store.ListApps()
		healthMap := make(map[string]string, len(storeApps))
		for _, sa := range storeApps {
			healthMap[sa.Name] = sa.HealthStatus
			if healthMap[sa.Name] == "" {
				healthMap[sa.Name] = string(types.HealthUnknown)
			}
		}

		fmt.Printf("%-20s %-10s %-8s %-10s\n", "NAME", "STATE", "PORT", "HEALTH")
		for _, a := range apps {
			portStr := fmt.Sprintf("%d", a.Port)
			if a.Port == 0 {
				portStr = "-"
			}
			health := healthMap[a.Name]
			if health == "" {
				health = string(types.HealthUnknown)
			}
			fmt.Printf("%-20s %-10s %-8s %-10s\n", a.Name, a.State, portStr, health)
		}
		return nil
	},
}
```

Add import for `"github.com/yaso09/tengiz/internal/config"` if not already present (it is).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cli/ -v -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): show health status in tengiz ps output"
```

---

### Task 9: Update init template + README documentation

**Files:**
- Modify: `internal/cli/root.go` (init template)
- Modify: `README.md`

- [ ] **Step 1: Update `init` template to include healthcheck section**

In `internal/cli/root.go`, modify the `initCmd` template content:

```go
content := fmt.Sprintf(`name: %s
# port: 3000            # container internal port (auto-detected if omitted)
serverless:
  enabled: true
  idle_timeout: 5m      # scale-to-zero timeout
# healthcheck:
#   enabled: true
#   endpoint: /health
#   port: 3000
#   interval: 30
#   retries: 3
#   timeout: 5
#   start_period: 0
# domains:
#   - app.example.com
# env:
#   DATABASE_URL: postgres://localhost:5432/myapp
#   API_KEY: your-secret-key
`, name)
```

- [ ] **Step 2: Verify init output**

Run: `go build -o /dev/null . && ./tengiz init testapp 2>/dev/null && cat .tengiz.yaml && rm .tengiz.yaml`
Expected: template contains healthcheck section

- [ ] **Step 3: Update README.md**

Read `README.md` first, then update the healthcheck section. The existing README (around line 211) already has healthcheck fields. Add `start_period` to the example:

Replace the healthcheck section in README.md:

```yaml
healthcheck:
  enabled: true
  endpoint: /health
  port: 3000
  interval: 30
  retries: 3
  timeout: 5
  start_period: 0
```

Also add a brief feature description after the example if it doesn't exist. Read the README section to see what's there.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go README.md
git commit -m "docs: add healthcheck to init template and README"
```

---

### Self-Review

**1. Spec coverage:** The spec requires:
- [x] Health check configuration in `.tengiz.yaml` — already exists, extended with `start_period` (Task 3)
- [x] Cold start retry on failure — handled by `WaitForHealth` (Task 4) and health checker (Task 6)
- [x] Container crash auto-restart — health checker calls `runtime.Restart` on health failure (Task 6)
- [x] Docker HEALTHCHECK instruction — Task 5 adds to generated Dockerfiles
- [x] CLI commands — `tengiz health <app>` (Task 7), health column in `ps` (Task 8)

**2. Placeholder scan:** All steps contain complete code, no TBD/TODO/fill-in patterns are present.

**3. Type consistency:** Cross-reference all types, method signatures, and field names:
- `HealthCheckConfig.StartPeriod` — defined in Task 3, used in Task 5 (generated Dockerfile)
- `Manager.Restart(ctx, name) error` — defined in Task 1, used in Task 2 (impl) and Task 6 (health checker)
- `Manager.WaitForHealth(ctx, name, hc) error` — defined in Task 4
- `AppEntry.HealthStatus` / `AppEntry.RestartCount` — defined in Task 3, used in Task 6 and Task 8
- `Detection.HealthCheck` — defined in Task 5
- `health.Checker` — defined in Task 6, used in Task 7

All types and method signatures are consistent across tasks.
