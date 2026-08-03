# Zero-Downtime Deploy Health Checks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Before switching traffic to a freshly-built container during a zero-downtime deploy, verify the new container passes an app-level HTTP health check; if it never becomes healthy, automatically roll back (remove the new container, free its port, keep the previous version serving traffic) and fail the deploy.

**Architecture:** The existing `healthcheck:` section in `.tengiz.yaml` (type `types.HealthCheckConfig`) becomes the deploy gate. `runtime.Manager.WaitForHealth` — currently dead code that is never called during deploy — is upgraded into a strict polling gate: wait for the container to be running → honor `start_period` grace → poll the health endpoint every `interval` seconds until it returns 2xx/3xx or the retry budget is exhausted. A small new `internal/deployhealth` package provides `Wait` (chooses TCP-only vs. health gate based on config, preserving backward compatibility) and `Abort` (rollback cleanup + records a `failed` deployment). Both zero-downtime deploy paths (`internal/cli/root.go` `deployCmd` and `internal/gitdeploy/deployer.go` `Pipeline.Deploy`) call `Wait` before registering the new proxy route and call `Abort` on failure — the old container is never touched, so traffic automatically stays on the previous version.

**Tech Stack:** Go 1.26, existing `runtime.Manager` interface, `config.Store`, `types.HealthCheckConfig`, `net/http` + `net/http/httptest` for tests. No new external dependencies.

## Global Constraints

- Backward compatible: apps **without** `healthcheck.enabled: true` keep the historical best-effort TCP-port readiness wait (warning on failure, deploy continues)
- The health gate applies **only** to zero-downtime redeploys (both `tengiz deploy` on an existing app and git-push deploys); the first-deploy path is unchanged
- Reuse existing `HealthCheckConfig` fields verbatim: `start_period` (grace seconds, default 0), `interval` (seconds between attempts, default 2), `retries` (number of attempts, default 30), `timeout` (per-request seconds, default 5), `endpoint` (default `/health`)
- On gate failure: remove the new versioned container via `RemoveBySuffix`, free the new port, record a deployment entry with status `failed`, send a `deploy:failure` notification, return an error to the caller
- Add `DeployFailed DeploymentStatus = "failed"` to `internal/types/types.go`
- Health checks run against the container's auto-detected host port (the `hc.Port` field is not used for gating)
- No new external dependencies
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/docker.go` | Upgrade `WaitForHealth` into a strict deploy gate; add `pollHealthURL`, `sleepWithContext`, and `hostPort` helpers; refactor `WaitForReady`/`GetContainerPort` to reuse `hostPort` |
| `internal/runtime/runtime_test.go` | Unit tests for `pollHealthURL` (no Docker needed) |
| `internal/types/types.go` | Add `DeployFailed` deployment status constant |
| `internal/deployhealth/deployhealth.go` | **New.** `Wait` (readiness+health gate selection) and `Abort` (rollback cleanup + failed-deployment record) |
| `internal/deployhealth/deployhealth_test.go` | **New.** Unit tests for `Wait` and `Abort` with a mock `runtime.Manager` |
| `internal/cli/root.go` | Wire `deployhealth.Wait`/`Abort` into the zero-downtime block of `deployCmd`; update init template comment |
| `internal/gitdeploy/deployer.go` | Wire `deployhealth.Wait`/`Abort` into `Pipeline.Deploy` zero-downtime path |
| `README.md` | Document the deploy health gate and automatic rollback behavior |

---

### Task 1: Upgrade `runtime.WaitForHealth` into a strict deploy gate

**Files:**
- Modify: `internal/runtime/docker.go` — replace `WaitForHealth` (lines 281-344), extract `hostPort`, add `pollHealthURL` + `sleepWithContext`, refactor `WaitForReady` (lines 571-616) and `GetContainerPort` (lines 544-569)
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: `types.HealthCheckConfig` (existing fields), `runtime.Manager` interface (unchanged — `WaitForHealth` already declared)
- Produces: `pollHealthURL(ctx context.Context, url string, timeout, interval time.Duration, retries int) error` — exported test target, unexported function; `sleepWithContext(ctx context.Context, d time.Duration) error`; `hostPort(ctx context.Context, containerName string) (int, error)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/runtime_test.go`:

```go
func TestPollHealthURLSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	err := pollHealthURL(context.Background(), ts.URL, 500*time.Millisecond, 10*time.Millisecond, 3)
	if err != nil {
		t.Fatalf("pollHealthURL() error = %v", err)
	}
}

func TestPollHealthURLHTTP500Fails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	err := pollHealthURL(context.Background(), ts.URL, 500*time.Millisecond, 10*time.Millisecond, 1)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error = %v, want it to contain 'HTTP 500'", err)
	}
}

func TestPollHealthURLSuccessOnRetry(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	err := pollHealthURL(context.Background(), ts.URL, 500*time.Millisecond, 10*time.Millisecond, 3)
	if err != nil {
		t.Fatalf("pollHealthURL() error = %v", err)
	}
}

func TestPollHealthURLConnectionRefused(t *testing.T) {
	err := pollHealthURL(context.Background(), "http://127.0.0.1:1", 100*time.Millisecond, 5*time.Millisecond, 2)
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestPollHealthURLContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := pollHealthURL(ctx, "http://127.0.0.1:1", 100*time.Millisecond, 50*time.Millisecond, 5)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}
```

Update the imports of `internal/runtime/runtime_test.go` to:

```go
import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/types"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestPollHealthURL -v -count=1`

Expected: FAIL with `undefined: pollHealthURL`

- [ ] **Step 3: Write minimal implementation in `internal/runtime/docker.go`**

Add the two helper functions and replace `WaitForHealth`. First add `hostPort` (place it just above `WaitForHealth`):

```go
func (r *dockerRuntime) hostPort(ctx context.Context, containerName string) (int, error) {
	portCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{json .NetworkSettings.Ports}}", containerName)
	portOut, err := portCmd.CombinedOutput()
	if err != nil {
		return 0, err
	}
	var ports map[string][]map[string]string
	if err := json.Unmarshal(portOut, &ports); err != nil {
		return 0, nil
	}
	for _, bindings := range ports {
		for _, b := range bindings {
			if hp := b["HostPort"]; hp != "" {
				var hostPort int
				fmt.Sscanf(hp, "%d", &hostPort)
				if hostPort != 0 {
					return hostPort, nil
				}
			}
		}
	}
	return 0, nil
}
```

Replace the entire current `WaitForHealth` body (lines 281-344) with:

```go
func (r *dockerRuntime) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error {
	if hc == nil || !hc.Enabled {
		return nil
	}

	// Wait for the container to be running (it may take a moment to start)
	for {
		cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", name)
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

	// Grace period for slow-booting applications
	if hc.StartPeriod > 0 {
		if err := sleepWithContext(ctx, time.Duration(hc.StartPeriod)*time.Second); err != nil {
			return err
		}
	}

	hostPort, err := r.hostPort(ctx, name)
	if err != nil {
		return fmt.Errorf("inspect ports: %w", err)
	}
	if hostPort == 0 {
		return fmt.Errorf("no host port bound for %s", name)
	}

	timeout := hc.Timeout
	if timeout <= 0 {
		timeout = 5
	}
	interval := hc.Interval
	if interval <= 0 {
		interval = 2
	}
	retries := hc.Retries
	if retries <= 0 {
		retries = 30
	}

	endpoint := hc.Endpoint
	if endpoint == "" {
		endpoint = "/health"
	}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", hostPort, endpoint)
	return pollHealthURL(ctx, url, time.Duration(timeout)*time.Second, time.Duration(interval)*time.Second, retries)
}
```

Add `pollHealthURL` and `sleepWithContext` right after `WaitForHealth`:

```go
func pollHealthURL(ctx context.Context, url string, timeout, interval time.Duration, retries int) error {
	client := &http.Client{Timeout: timeout}
	var lastErr error
	for i := 0; i < retries; i++ {
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
		if i < retries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
			}
		}
	}
	return fmt.Errorf("health check failed after %d attempts: %w", retries, lastErr)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
```

Refactor `GetContainerPort` (lines 544-569) to reuse `hostPort` (behavior-preserving):

```go
func (r *dockerRuntime) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) {
	containerName := fmt.Sprintf("%s-%s", name, suffix)
	return r.hostPort(ctx, containerName)
}
```

Refactor the port auto-detect section of `WaitForReady` (lines 589-615) to reuse `hostPort` (behavior-preserving):

```go
	// Auto-detect host port from container inspect
	if hostPort, _ := r.hostPort(ctx, containerName); hostPort != 0 {
		return waitForPort(ctx, "127.0.0.1", hostPort, 30*time.Second)
	}
	return nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestPollHealthURL -v -count=1`

Expected: PASS for all 5 tests

Then run the full package to check nothing regressed:
Run: `go test ./internal/runtime/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat(runtime): upgrade WaitForHealth into strict deploy health gate"
```

---

### Task 2: Create `internal/deployhealth` package (`Wait` + `Abort`)

**Files:**
- Create: `internal/deployhealth/deployhealth.go`
- Create: `internal/deployhealth/deployhealth_test.go`
- Modify: `internal/types/types.go` — add `DeployFailed` status constant

**Interfaces:**
- Consumes: `runtime.Manager` (specifically `WaitForReady`, `WaitForHealth`, `RemoveBySuffix`), `config.Store` (`FreePort`, `AddDeployment`), `types.DeployFailed`
- Produces: `deployhealth.Wait(ctx context.Context, rt runtime.Manager, cfg *types.AppConfig, versionedContainerName string, internalPort int) error`; `deployhealth.Abort(ctx context.Context, rt runtime.Manager, store *config.Store, appName, containerName, deploymentID, imageTag string, newPort int) error`

- [ ] **Step 1: Write the failing tests**

Create `internal/deployhealth/deployhealth_test.go`:

```go
package deployhealth

import (
	"context"
	"errors"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type gateMockRT struct {
	runtime.Manager
	healthErr    error
	readyErr     error
	removeErr    error
	readyCalls   int
	healthCalls  int
	removed      []string
	removeSuffix string
}

func newGateMockRT() *gateMockRT {
	return &gateMockRT{Manager: runtime.NewStub()}
}

func (m *gateMockRT) WaitForReady(ctx context.Context, name string, internalPort int) error {
	m.readyCalls++
	return m.readyErr
}

func (m *gateMockRT) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error {
	m.healthCalls++
	return m.healthErr
}

func (m *gateMockRT) RemoveBySuffix(ctx context.Context, name string, suffix string) error {
	m.removed = append(m.removed, name)
	m.removeSuffix = suffix
	return m.removeErr
}

func TestWaitUsesWaitForReadyWhenHealthDisabled(t *testing.T) {
	rt := newGateMockRT()
	cfg := &types.AppConfig{Name: "myapp"}
	err := Wait(context.Background(), rt, cfg, "tengiz-myapp-123", 8080)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if rt.readyCalls != 1 || rt.healthCalls != 0 {
		t.Errorf("Wait() readyCalls=%d healthCalls=%d, want 1/0", rt.readyCalls, rt.healthCalls)
	}
}

func TestWaitUsesWaitForHealthWhenEnabled(t *testing.T) {
	rt := newGateMockRT()
	cfg := &types.AppConfig{
		Name:        "myapp",
		HealthCheck: &types.HealthCheckConfig{Enabled: true, Endpoint: "/health"},
	}
	err := Wait(context.Background(), rt, cfg, "tengiz-myapp-123", 8080)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if rt.healthCalls != 1 || rt.readyCalls != 0 {
		t.Errorf("Wait() readyCalls=%d healthCalls=%d, want 0/1", rt.readyCalls, rt.healthCalls)
	}
}

func TestWaitHealthFailurePropagates(t *testing.T) {
	rt := newGateMockRT()
	rt.healthErr = errors.New("health check failed after 30 attempts: connection refused")
	cfg := &types.AppConfig{
		Name:        "myapp",
		HealthCheck: &types.HealthCheckConfig{Enabled: true},
	}
	err := Wait(context.Background(), rt, cfg, "tengiz-myapp-123", 8080)
	if err == nil {
		t.Fatal("expected WaitForHealth error to propagate")
	}
}

func TestAbortRemovesContainerAndFreesPort(t *testing.T) {
	store := config.NewStore(t.TempDir())
	port, err := store.AllocatePort("myapp")
	if err != nil {
		t.Fatal(err)
	}
	rt := newGateMockRT()
	err = Abort(context.Background(), rt, store, "myapp", "tengiz-myapp", "999", "tengiz-apps/myapp:v1", port)
	if err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
	if len(rt.removed) != 1 || rt.removed[0] != "tengiz-myapp" || rt.removeSuffix != "999" {
		t.Errorf("RemoveBySuffix called with %v suffix=%q, want [tengiz-myapp] suffix=999", rt.removed, rt.removeSuffix)
	}
	got, err := store.AllocatePort("other")
	if err != nil {
		t.Fatal(err)
	}
	if got != port {
		t.Errorf("port %d not freed, reallocated %d", port, got)
	}
	deps, err := store.GetDeployments("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0].Status != string(types.DeployFailed) {
		t.Errorf("deployments = %+v, want 1 entry with status %q", deps, types.DeployFailed)
	}
}

func TestAbortPropagatesRemoveError(t *testing.T) {
	store := config.NewStore(t.TempDir())
	port, _ := store.AllocatePort("myapp")
	rt := newGateMockRT()
	rt.removeErr = errors.New("docker rm failed")
	err := Abort(context.Background(), rt, store, "myapp", "tengiz-myapp", "999", "tengiz-apps/myapp:v1", port)
	if err == nil {
		t.Fatal("expected RemoveBySuffix error to propagate")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/deployhealth/ -v -count=1`

Expected: FAIL with `undefined: Wait`, `undefined: Abort`, `undefined: DeployFailed`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/types/types.go`, inside the `DeploymentStatus` const block (lines 158-164):

```go
const (
	DeployActive   DeploymentStatus = "active"
	DeployPrevious DeploymentStatus = "previous"
	DeployRolled   DeploymentStatus = "rolled"
	DeployFailed   DeploymentStatus = "failed"
)
```

Create `internal/deployhealth/deployhealth.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/deployhealth/ -v -count=1`

Expected: PASS for all 5 tests

- [ ] **Step 5: Commit**

```bash
git add internal/deployhealth/ internal/types/types.go
git commit -m "feat: add deployhealth gate with automatic rollback on failed health check"
```

---

### Task 3: Wire the deploy gate into `tengiz deploy`

**Files:**
- Modify: `internal/cli/root.go` — add `deployhealth` import; replace the readiness block in `deployCmd` (lines 410-414)

**Interfaces:**
- Consumes: `deployhealth.Wait`, `deployhealth.Abort`, `notifyMgr.SendAsync`, `store.FreePort`/`AddDeployment` (via `Abort`)
- Produces: nothing new — behavior change in the zero-downtime path

- [ ] **Step 1: Write the failing test**

This wiring runs only when Docker is available (build + container create), which this repo's test harness cannot exercise — verification is therefore compile + vet + the existing test suite. Add an assertion that the package is wired by checking the compile step in Step 2. No new test file.

- [ ] **Step 2: Run test to verify it fails (compilation check)**

Run: `go build ./...`

Expected: PASS currently (no change yet)

- [ ] **Step 3: Write minimal implementation**

Add the import to `internal/cli/root.go` (alphabetical order, inside the existing import block lines 16-30):

```go
	"github.com/yaso09/tengiz/internal/deployhealth"
```

Replace lines 410-414:

```go
		// Wait for the new container to be ready
		containerName := runtime.ContainerName(cfg.Name, cfg.Environment)
		if err := rt.WaitForReady(context.Background(), fmt.Sprintf("%s-%s", containerName, deploymentID), cfg.Port); err != nil {
			log.Printf("[tengiz] warning: new container may not be ready: %v", err)
		}
```

with:

```go
		// Wait for the new container to be ready (TCP + app-level health when configured)
		containerName := runtime.ContainerName(cfg.Name, cfg.Environment)
		versionedName := fmt.Sprintf("%s-%s", containerName, deploymentID)
		if err := deployhealth.Wait(context.Background(), rt, cfg, versionedName, cfg.Port); err != nil {
			if cfg.HealthCheck != nil && cfg.HealthCheck.Enabled {
				if abortErr := deployhealth.Abort(context.Background(), rt, store, cfg.Name, containerName, deploymentID, imageTag, newPort); abortErr != nil {
					log.Printf("[tengiz] warning: rollback cleanup failed: %v", abortErr)
				}
				notifyMgr.SendAsync(context.Background(), types.NotificationEvent{
					Type:    types.EventDeployFailure,
					AppName: cfg.Name,
					Message: fmt.Sprintf("Health check failed for %s: %v (rolled back)", cfg.Name, err),
					Metadata: map[string]string{
						"environment": envFlag,
					},
				})
				return fmt.Errorf("deploy aborted: health check failed: %w", err)
			}
			log.Printf("[tengiz] warning: new container may not be ready: %v", err)
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go vet ./...`
Expected: PASS (compiles, no vet warnings)

Run: `go test ./internal/cli/... -count=1`
Expected: PASS (existing tests unchanged)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat(cli): gate zero-downtime deploy on app health with auto-rollback"
```

---

### Task 4: Wire the deploy gate into the git deploy pipeline

**Files:**
- Modify: `internal/gitdeploy/deployer.go` — add `deployhealth` import; replace the readiness block in `Deploy` (lines 269-272)

**Interfaces:**
- Consumes: `deployhealth.Wait`, `deployhealth.Abort`, `notifyMgr.SendAsync`
- Produces: nothing new — behavior change in the git-push zero-downtime path

- [ ] **Step 1: Write the failing test**

This path requires cloning a real repo and building with Docker; the existing `TestPipelineStartsDeploy` covers the failure branch and `TestPipelineDeployWithNixpacksDetectionOverride` is skipped for the same reason. Verification is compile + vet + the existing suite.

- [ ] **Step 2: Run test to verify it fails (compilation check)**

Run: `go build ./...`

Expected: PASS currently (no change yet)

- [ ] **Step 3: Write minimal implementation**

Add the import to `internal/gitdeploy/deployer.go` (inside the import block lines 4-19):

```go
	"github.com/yaso09/tengiz/internal/deployhealth"
```

Replace lines 269-272:

```go
	containerName := runtime.ContainerName(appName, p.env)
	if err := p.rt.WaitForReady(ctx, fmt.Sprintf("%s-%s", containerName, deploymentID), cfg.Port); err != nil {
		log.Printf("[tengiz] warning: new container may not be ready: %v", err)
	}
```

with:

```go
	containerName := runtime.ContainerName(appName, p.env)
	versionedName := fmt.Sprintf("%s-%s", containerName, deploymentID)
	if err := deployhealth.Wait(ctx, p.rt, cfg, versionedName, cfg.Port); err != nil {
		if cfg.HealthCheck != nil && cfg.HealthCheck.Enabled {
			if abortErr := deployhealth.Abort(ctx, p.rt, p.store, appName, containerName, deploymentID, imageTag, newPort); abortErr != nil {
				log.Printf("[tengiz] warning: rollback cleanup failed: %v", abortErr)
			}
			notifyMgr.SendAsync(ctx, types.NotificationEvent{
				Type:    types.EventDeployFailure,
				AppName: appName,
				Message: fmt.Sprintf("Health check failed for %s: %v (rolled back)", appName, err),
				Metadata: map[string]string{
					"environment": p.env,
				},
			})
			return fmt.Errorf("deploy aborted: health check failed: %w", err)
		}
		log.Printf("[tengiz] warning: new container may not be ready: %v", err)
	}
```

Note: `cfg.HealthCheck` is populated for redeploys from `existingApp.Config.HealthCheck` at line 98, so git-push redeploys pick up the stored healthcheck config.

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go vet ./...`
Expected: PASS

Run: `go test ./internal/gitdeploy/... ./internal/webhook/... -count=1`
Expected: PASS (existing tests unchanged)

- [ ] **Step 5: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "feat(gitdeploy): gate git-push zero-downtime deploy on app health with auto-rollback"
```

---

### Task 5: Update documentation

**Files:**
- Modify: `README.md` — document the deploy health gate after the config example (line ~451)
- Modify: `internal/cli/root.go` — add a comment to the init template healthcheck block (line ~138)

**Interfaces:**
- Consumes: nothing
- Produces: nothing — documentation only

- [ ] **Step 1: Write the failing test**

Documentation change — no test. Verification is that the README renders the new section and the template comment is present.

- [ ] **Step 2: Run test to verify it fails (no-op baseline)**

Run: `go build ./...`
Expected: PASS (no code change yet)

- [ ] **Step 3: Write minimal implementation**

In `README.md`, insert this section immediately after the closing ` ``` ` of the config example (after line 451, before the "Resource limits are passed to Docker..." paragraph):

```markdown
### Deploy Health Gate

When `healthcheck.enabled: true`, zero-downtime deploys verify the **new** container is healthy *before* traffic is switched to it. Tengiz waits for the container to be running, applies the `start_period` grace period, then polls `healthcheck.endpoint` (default `/health`) every `interval` seconds (default 2) for up to `retries` attempts (default 30), with a `timeout`-second per-request budget (default 5).

If the new container never becomes healthy, the deployment is **automatically rolled back**: the new container is removed, its port is freed, the deployment is recorded as `failed`, and the previous version keeps serving traffic. Apps without `healthcheck.enabled` keep the historical behavior (TCP port readiness only, best-effort).
```

In `internal/cli/root.go`, update the init template healthcheck block (lines 131-138) to:

```go
# healthcheck:
#   enabled: true
#   endpoint: /health
#   port: 3000
#   interval: 30
#   retries: 3
#   timeout: 5
#   start_period: 0      # grace before deploy health gate polls (zero-downtime deploys)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./...`
Expected: PASS

Run: `go test ./internal/cli/... -count=1`
Expected: PASS (template string change does not affect tests)

Verify the README section renders by reading the inserted lines back.

- [ ] **Step 5: Commit**

```bash
git add README.md internal/cli/root.go
git commit -m "docs: document zero-downtime deploy health gate and auto-rollback"
```

---

## Self-Review

**1. Spec coverage.** Feature #1 (P0) requires "app-level health validation before traffic is transferred + automatic rollback." The plan:
- App-level validation before traffic switch → Task 1 (`WaitForHealth` polling gate) + Task 2 (`deployhealth.Wait`) wired before `RegisterRouteWithProxy` in Tasks 3-4 ✓
- Automatic rollback → Task 2 (`deployhealth.Abort`) + call sites remove the new container, free the port, record `failed`, and return an error before the route is re-registered, so the old container keeps serving ✓
- Existing `healthcheck:` config reused as the gate → Global Constraints + Task 1 ✓
- No gaps identified.

**2. Placeholder scan.** No "TBD"/"TODO"/"add error handling" placeholders; every code step shows full, compilable code with exact file paths and expected test output. The two wiring tasks (3 and 4) cannot be executed in this repo's test harness (they require Docker), so their verification steps are compile + vet + regression tests — this is stated explicitly rather than hidden behind a placeholder.

**3. Type consistency.**
- `deployhealth.Wait(ctx, rt, cfg, versionedContainerName, internalPort) error` — used identically in Tasks 3 and 4 ✓
- `deployhealth.Abort(ctx, rt, store, appName, containerName, deploymentID, imageTag, newPort) error` — used identically in Tasks 3 and 4 ✓
- `types.DeployFailed` — defined in Task 2, consumed by `deployhealth.Abort` and asserted in `TestAbortRemovesContainerAndFreesPort` ✓
- `pollHealthURL(ctx, url, timeout, interval, retries)` signature matches Task 1's test calls (all pass `time.Duration` for timeout/interval and `int` for retries) ✓
- `runtime.Manager` interface is unchanged; only `WaitForHealth`'s implementation changes, so all existing mocks/stubs that implement the interface keep compiling ✓
- `WaitForHealth` still returns `nil` when `hc == nil || !hc.Enabled`, preserving stub/mock test expectations (`TestStubWaitForHealth`) ✓
