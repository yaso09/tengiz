# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by removing stale Tengiz-managed containers, old images, and unused volumes/networks/build-cache using label-aware, env-scoped Docker pruning.

**Architecture:** A new `internal/cleanup` package wraps `runtime.Manager` (for container/image removal) and `config.Store` (for tracking active/previous deployments) plus a thin injectable `commandRunner` for direct `docker` CLI calls (listing + volume/network/build-cache prune). Pure decision functions (`staleVersionedContainers`, `extractReclaimedSpace`) keep the safety logic unit-testable without a Docker daemon. The CLI wires a `cleanupCmd` into `internal/cli/root.go`.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` (`Remove`, `RemoveImage`, `KeepLastNImages`), `config.Store`, `os/exec` for the Docker CLI. No new external dependencies.

## Global Constraints

- Label-aware only: containers are matched via the `tengiz-app` label; never run global `docker system prune` (that removes non-Tengiz resources)
- Never remove running containers
- Never remove canonical containers (`tengiz-<app>`, `tengiz-<app>-<env>`) — a stopped canonical container is a scale-to-zero idle app that the proxy cold-starts on demand
- Never remove preview containers (`tengiz-<app>-pr-<n>`)
- Only remove versioned containers (label `tengiz-deployment`) whose deployment ID is NOT the app's active or previous deployment recorded in the store
- Preserve the newest `--keep` images per app (default 5, minimum 1)
- `--build-cache` defaults to `false` (build cache speeds up redeploys)
- Env-scoped: `--env` filters containers via the `tengiz-env` label and reads state from `apps-{env}.json` via `config.NewStoreWithEnv(dataDir, env)`
- Default env is `"production"` via the existing `getEnv(cmd)` helper
- All new code must pass `go build -o tengiz .`, `go vet ./...`, and `go test ./... -v -count=1`
- Output uses the existing `[tengiz]` prefix and the `dataDir` package global (CLI tests override `dataDir` with `t.TempDir()`)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/cleanup.go` | New package. `Options`/`Result` types, `Manager` with injectable `commandRunner`, and one prune method per resource class. Pure decision helpers live here too. |
| `internal/cleanup/cleanup_test.go` | Unit tests using the stub runtime, a fake `commandRunner`, and a temp store. No Docker daemon required. |
| `internal/cli/root.go` | Register `cleanupCmd`, its flags, and the report printing. |
| `internal/cli/cleanup_test.go` | CLI registration + flag-parsing tests (follows the `TestLogsCmdWithFlags` pattern). |
| `README.md` | Add `tengiz cleanup` to the CLI Reference. |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI list + `cleanup` package to the architecture table. |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as ✅ Implemented. |

---

### Task 1: Cleanup package core (types + Manager)

**Files:**
- Create: `internal/cleanup/cleanup.go` (core only: `Options`, `Result`, `commandRunner`, `Manager`, `NewManager`)
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager` (interface from `internal/runtime/runtime.go:31`), `config.Store` (`internal/config/store.go:16`)
- Produces: `cleanup.Options{Containers, Images, Volumes, Networks, BuildCache bool; Keep int; DryRun bool}`, `cleanup.Result{DryRun bool; ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved []string; BuildCacheReclaimed string}`, `cleanup.NewManager(rt runtime.Manager, store *config.Store, env string) *Manager`. Later tasks add private methods on `*Manager`.

- [ ] **Step 1: Write the failing test**

```go
// internal/cleanup/cleanup_test.go
package cleanup

import (
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestNewManagerDefaultsEnv(t *testing.T) {
	m := NewManager(runtime.NewStub(), config.NewStoreWithEnv(t.TempDir(), ""), "")
	if m.env != "production" {
		t.Fatalf("env = %q, want %q", m.env, "production")
	}
}

func TestNewManagerPreservesEnv(t *testing.T) {
	m := NewManager(runtime.NewStub(), config.NewStoreWithEnv(t.TempDir(), "staging"), "staging")
	if m.env != "staging" {
		t.Fatalf("env = %q, want %q", m.env, "staging")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: FAIL with `undefined: NewManager` / `package cleanup is not in std` (package does not exist yet).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cleanup/cleanup.go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	Keep       int
	DryRun     bool
}

type Result struct {
	DryRun              bool
	ContainersRemoved   []string
	ImagesRemoved       []string
	VolumesRemoved      []string
	NetworksRemoved     []string
	BuildCacheReclaimed string
}

type commandRunner func(ctx context.Context, args ...string) (string, error)

type Manager struct {
	env   string
	rt    runtime.Manager
	store *config.Store
	run   commandRunner
}

func NewManager(rt runtime.Manager, store *config.Store, env string) *Manager {
	if env == "" {
		env = "production"
	}
	return &Manager{
		env:   env,
		rt:    rt,
		store: store,
		run: func(ctx context.Context, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, "docker", args...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			}
			return string(out), nil
		},
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: PASS (both tests green).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add cleanup package core types and Manager"
```

---

### Task 2: Stale container pruning

**Files:**
- Modify: `internal/cleanup/cleanup.go` — add `dockerPS`, `containerInfo`, `listContainers`, `staleVersionedContainers`, `trackedDeployments`, `pruneContainers`
- Test: `internal/cleanup/cleanup_test.go` — add `TestStaleVersionedContainers`, `TestPruneRemovesStaleContainers`, plus the `countingStub` and `newFakeRunner` helpers

**Interfaces:**
- Consumes: `Manager.run` (Task 1), `Manager.rt.Remove(ctx, name) error`, `Manager.store.ListApps()`, `Manager.store.GetPreviousDeployment(appName)` 
- Produces: `staleVersionedContainers(containers []containerInfo, tracked map[string][]string) []string` (pure), `pruneContainers(ctx, opts Options, result *Result) error` (private method)

Safety rule implemented here: a versioned container (has `tengiz-deployment` label) is stale if it is `exited` AND its deployment ID is not the active or previous deployment for its app. Canonical containers (no `tengiz-deployment` label) and running containers are never candidates.

- [ ] **Step 1: Write the failing tests**

```go
// internal/cleanup/cleanup_test.go (append)
import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type countingStub struct {
	runtime.Manager
	removedContainers []string
	removedImages     []string
	keepCalls         []string
}

func (c *countingStub) Remove(ctx context.Context, name string) error {
	c.removedContainers = append(c.removedContainers, name)
	return nil
}

func (c *countingStub) RemoveImage(ctx context.Context, tag string) error {
	c.removedImages = append(c.removedImages, tag)
	return nil
}

func (c *countingStub) KeepLastNImages(ctx context.Context, appName string, n int) error {
	c.keepCalls = append(c.keepCalls, appName)
	return nil
}

func newFakeRunner(containersJSON, danglingImages, volumes, networks string) commandRunner {
	return func(ctx context.Context, args ...string) (string, error) {
		switch args[0] {
		case "ps":
			return containersJSON, nil
		case "images":
			return danglingImages, nil
		case "volume":
			if len(args) > 1 && args[1] == "ls" {
				return volumes, nil
			}
			return "Total reclaimed space: 0 B", nil
		case "network":
			if len(args) > 1 && args[1] == "ls" {
				return networks, nil
			}
			return "Total reclaimed space: 0 B", nil
		case "builder":
			return "Total reclaimed space: 1.5 GB", nil
		default:
			return "", nil
		}
	}
}

func TestStaleVersionedContainers(t *testing.T) {
	containers := []containerInfo{
		{Name: "tengiz-app-prod-111", State: "exited", App: "app", Deployment: "111"},
		{Name: "tengiz-app-prod-222", State: "exited", App: "app", Deployment: "222"},
		{Name: "tengiz-app-prod-333", State: "exited", App: "app", Deployment: "333"},
		{Name: "tengiz-app", State: "exited", App: "app", Deployment: ""},
		{Name: "tengiz-app-prod-111", State: "running", App: "app", Deployment: "111"},
		{Name: "tengiz-app-pr-7", State: "exited", App: "app", Deployment: ""},
	}
	tracked := map[string][]string{"app": {"222", "333"}}

	stale := staleVersionedContainers(containers, tracked)
	// 111 exited + not tracked -> stale
	// 222, 333 tracked -> kept
	// canonical (no deployment) -> kept
	// 111 running -> kept
	// preview (no deployment) -> kept
	if len(stale) != 1 || stale[0] != "tengiz-app-prod-111" {
		t.Fatalf("stale = %v, want [tengiz-app-prod-111]", stale)
	}
}

func TestPruneRemovesStaleContainers(t *testing.T) {
	store := config.NewStoreWithEnv(t.TempDir(), "production")
	if err := store.SaveApp(types.AppEntry{
		Name:             "app",
		DeploymentSuffix: "222",
		Config:           types.AppConfig{Name: "app"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddDeployment("app", types.DeploymentEntry{ID: "222", Status: string(types.DeployActive)}); err != nil {
		t.Fatal(err)
	}

	stub := &countingStub{Manager: runtime.NewStub()}
	runner := newFakeRunner(
		`{"ID":"1","Name":"/tengiz-app-prod-111","State":"exited","Labels":"tengiz-app=app,tengiz-env=production,tengiz-deployment=111"}
{"ID":"2","Name":"/tengiz-app-prod-222","State":"exited","Labels":"tengiz-app=app,tengiz-env=production,tengiz-deployment=222"}`,
		"", "", "")
	m := &Manager{env: "production", rt: stub, store: store, run: runner}

	result := &Result{}
	if err := m.pruneContainers(context.Background(), Options{Containers: true}, result); err != nil {
		t.Fatal(err)
	}
	if len(stub.removedContainers) != 1 || stub.removedContainers[0] != "tengiz-app-prod-111" {
		t.Fatalf("removed = %v, want [tengiz-app-prod-111]", stub.removedContainers)
	}
	if len(result.ContainersRemoved) != 1 {
		t.Fatalf("report containers = %d, want 1", len(result.ContainersRemoved))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestStaleVersionedContainers|TestPruneRemovesStaleContainers" -v -count=1`
Expected: FAIL with `undefined: containerInfo`, `undefined: staleVersionedContainers`, `undefined: pruneContainers`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cleanup/cleanup.go (append)
import (
	"encoding/json"
	"sort"
)

type dockerPS struct {
	ID     string `json:"ID"`
	Name   string `json:"Name"`
	State  string `json:"State"`
	Labels string `json:"Labels"`
}

type containerInfo struct {
	ID         string
	Name       string
	State      string
	App        string
	Env        string
	Deployment string
}

func (m *Manager) listContainers(ctx context.Context) ([]containerInfo, error) {
	out, err := m.run(ctx, "ps", "-a",
		"--filter", "label=tengiz-app",
		"--filter", fmt.Sprintf("label=tengiz-env=%s", m.env),
		"--format", "{{json .}}")
	if err != nil {
		return nil, err
	}

	var containers []containerInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var entry dockerPS
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		info := containerInfo{
			ID:    entry.ID,
			Name:  strings.TrimPrefix(entry.Name, "/"),
			State: entry.State,
		}
		for _, part := range strings.Split(entry.Labels, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "tengiz-app":
				info.App = kv[1]
			case "tengiz-env":
				info.Env = kv[1]
			case "tengiz-deployment":
				info.Deployment = kv[1]
			}
		}
		containers = append(containers, info)
	}
	return containers, nil
}

func staleVersionedContainers(containers []containerInfo, tracked map[string][]string) []string {
	var stale []string
	for _, c := range containers {
		if c.State != "exited" || c.Deployment == "" || c.App == "" {
			continue
		}
		keep := false
		for _, id := range tracked[c.App] {
			if id == c.Deployment {
				keep = true
				break
			}
		}
		if !keep {
			stale = append(stale, c.Name)
		}
	}
	sort.Strings(stale)
	return stale
}

func (m *Manager) trackedDeployments() map[string][]string {
	tracked := make(map[string][]string)
	apps, err := m.store.ListApps()
	if err != nil {
		return tracked
	}
	for _, app := range apps {
		var ids []string
		if app.DeploymentSuffix != "" {
			ids = append(ids, app.DeploymentSuffix)
		}
		if prev, err := m.store.GetPreviousDeployment(app.Name); err == nil && prev != nil {
			ids = append(ids, prev.ID)
		}
		if len(ids) > 0 {
			tracked[app.Name] = ids
		}
	}
	return tracked
}

func (m *Manager) pruneContainers(ctx context.Context, opts Options, result *Result) error {
	containers, err := m.listContainers(ctx)
	if err != nil {
		return err
	}
	stale := staleVersionedContainers(containers, m.trackedDeployments())
	if opts.DryRun {
		result.ContainersRemoved = stale
		return nil
	}
	for _, name := range stale {
		if err := m.rt.Remove(ctx, name); err != nil {
			return err
		}
		result.ContainersRemoved = append(result.ContainersRemoved, name)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -run "TestStaleVersionedContainers|TestPruneRemovesStaleContainers" -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): prune stale stopped versioned containers"
```

---

### Task 3: Image pruning (dangling images + per-app retention)

**Files:**
- Modify: `internal/cleanup/cleanup.go` — add `listDanglingImages`, `pruneImages`
- Test: `internal/cleanup/cleanup_test.go` — add `TestPruneImages`

**Interfaces:**
- Consumes: `Manager.rt.KeepLastNImages(ctx, appName string, n int) error`, `Manager.rt.RemoveImage(ctx, tag string) error`, `Manager.store.ListApps()`
- Produces: `pruneImages(ctx, opts Options, result *Result) error` (private method)

Image removal covers two sources: (1) per-app retention via the existing `runtime.Manager.KeepLastNImages` (skipped in dry-run since it mutates state), and (2) dangling images listed via `docker images --filter dangling=true -q`, each removed with `RemoveImage`.

- [ ] **Step 1: Write the failing test**

```go
// internal/cleanup/cleanup_test.go (append)
func TestPruneImages(t *testing.T) {
	store := config.NewStoreWithEnv(t.TempDir(), "production")
	if err := store.SaveApp(types.AppEntry{Name: "app", Config: types.AppConfig{Name: "app"}}); err != nil {
		t.Fatal(err)
	}

	stub := &countingStub{Manager: runtime.NewStub()}
	runner := newFakeRunner("", "sha256:abc\nsha256:def\n", "", "")
	m := &Manager{env: "production", rt: stub, store: store, run: runner}

	result := &Result{}
	if err := m.pruneImages(context.Background(), Options{Images: true, Keep: 5}, result); err != nil {
		t.Fatal(err)
	}
	if len(stub.removedImages) != 2 {
		t.Fatalf("removed images = %d, want 2 (%v)", len(stub.removedImages), stub.removedImages)
	}
	if len(stub.keepCalls) != 1 || stub.keepCalls[0] != "app" {
		t.Fatalf("keep calls = %v, want [app]", stub.keepCalls)
	}
	if len(result.ImagesRemoved) != 2 {
		t.Fatalf("report images = %d, want 2", len(result.ImagesRemoved))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestPruneImages" -v -count=1`
Expected: FAIL with `undefined: pruneImages`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cleanup/cleanup.go (append)
func (m *Manager) pruneImages(ctx context.Context, opts Options, result *Result) error {
	if !opts.DryRun {
		apps, err := m.store.ListApps()
		if err == nil {
			for _, app := range apps {
				if err := m.rt.KeepLastNImages(ctx, app.Name, opts.Keep); err != nil {
					return err
				}
			}
		}
	}

	dangling, err := m.listDanglingImages(ctx)
	if err != nil {
		return err
	}
	if opts.DryRun {
		result.ImagesRemoved = dangling
		return nil
	}
	for _, id := range dangling {
		if err := m.rt.RemoveImage(ctx, id); err != nil {
			return err
		}
		result.ImagesRemoved = append(result.ImagesRemoved, id)
	}
	return nil
}

func (m *Manager) listDanglingImages(ctx context.Context) ([]string, error) {
	out, err := m.run(ctx, "images", "--filter", "dangling=true", "-q")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -run "TestPruneImages" -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): prune dangling images and retain newest N per app"
```

---

### Task 4: Volume and network pruning

**Files:**
- Modify: `internal/cleanup/cleanup.go` — add `listDanglingVolumes`, `pruneVolumes`, `listNetworks`, `pruneNetworks`
- Test: `internal/cleanup/cleanup_test.go` — add `TestPruneVolumesAndNetworks`

**Interfaces:**
- Consumes: `Manager.run` (Task 1)
- Produces: `pruneVolumes(ctx, opts Options, result *Result) error`, `pruneNetworks(ctx, opts Options, result *Result) error` (private methods)

Volume candidates come from `docker volume ls -q --filter dangling=true` (Docker's own "unused volume" filter — Tengiz uses host bind mounts, never named volumes, so pruning these is safe). Network candidates are all networks except the built-in `bridge`, `host`, `none`. In both cases the candidate list is recorded before the `prune -f` runs; dry-run records the list without running the prune.

- [ ] **Step 1: Write the failing test**

```go
// internal/cleanup/cleanup_test.go (append)
func TestPruneVolumesAndNetworks(t *testing.T) {
	store := config.NewStoreWithEnv(t.TempDir(), "production")
	stub := &countingStub{Manager: runtime.NewStub()}
	runner := newFakeRunner("", "", "myvol\n", "custom-net\n")
	m := &Manager{env: "production", rt: stub, store: store, run: runner}

	result := &Result{}
	if err := m.pruneVolumes(context.Background(), Options{Volumes: true}, result); err != nil {
		t.Fatal(err)
	}
	if err := m.pruneNetworks(context.Background(), Options{Networks: true}, result); err != nil {
		t.Fatal(err)
	}
	if len(result.VolumesRemoved) != 1 || result.VolumesRemoved[0] != "myvol" {
		t.Fatalf("volumes = %v, want [myvol]", result.VolumesRemoved)
	}
	if len(result.NetworksRemoved) != 1 || result.NetworksRemoved[0] != "custom-net" {
		t.Fatalf("networks = %v, want [custom-net]", result.NetworksRemoved)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestPruneVolumesAndNetworks" -v -count=1`
Expected: FAIL with `undefined: pruneVolumes`, `undefined: pruneNetworks`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cleanup/cleanup.go (append)
func (m *Manager) pruneVolumes(ctx context.Context, opts Options, result *Result) error {
	dangling, err := m.listDanglingVolumes(ctx)
	if err != nil {
		return err
	}
	if opts.DryRun {
		result.VolumesRemoved = dangling
		return nil
	}
	if _, err := m.run(ctx, "volume", "prune", "-f"); err != nil {
		return err
	}
	result.VolumesRemoved = dangling
	return nil
}

func (m *Manager) listDanglingVolumes(ctx context.Context) ([]string, error) {
	out, err := m.run(ctx, "volume", "ls", "-q", "--filter", "dangling=true")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (m *Manager) pruneNetworks(ctx context.Context, opts Options, result *Result) error {
	candidates, err := m.listNetworks(ctx)
	if err != nil {
		return err
	}
	if opts.DryRun {
		result.NetworksRemoved = candidates
		return nil
	}
	if _, err := m.run(ctx, "network", "prune", "-f"); err != nil {
		return err
	}
	result.NetworksRemoved = candidates
	return nil
}

func (m *Manager) listNetworks(ctx context.Context) ([]string, error) {
	out, err := m.run(ctx, "network", "ls", "-q")
	if err != nil {
		return nil, err
	}
	builtin := map[string]bool{"bridge": true, "host": true, "none": true}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" && !builtin[line] {
			names = append(names, line)
		}
	}
	sort.Strings(names)
	return names, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -run "TestPruneVolumesAndNetworks" -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): prune unused Docker volumes and networks"
```

---

### Task 5: Build cache pruning

**Files:**
- Modify: `internal/cleanup/cleanup.go` — add `extractReclaimedSpace`, `pruneBuildCache`
- Test: `internal/cleanup/cleanup_test.go` — add `TestExtractReclaimedSpace`, `TestPruneBuildCache`

**Interfaces:**
- Consumes: `Manager.run` (Task 1)
- Produces: `extractReclaimedSpace(out string) string` (pure), `pruneBuildCache(ctx, opts Options, result *Result) error` (private method)

`docker builder prune -f` prints `Total reclaimed space: X`; the pure helper extracts it. Dry-run performs no Docker call for build cache (we cannot enumerate cache entries without mutating).

- [ ] **Step 1: Write the failing tests**

```go
// internal/cleanup/cleanup_test.go (append)
func TestExtractReclaimedSpace(t *testing.T) {
	out := "Deleted build cache objects:\nb1234\n\nTotal reclaimed space: 1.5 GB"
	if got := extractReclaimedSpace(out); got != "1.5 GB" {
		t.Fatalf("extractReclaimedSpace = %q, want %q", got, "1.5 GB")
	}
	if got := extractReclaimedSpace("no reclaim line"); got != "" {
		t.Fatalf("extractReclaimedSpace = %q, want empty", got)
	}
}

func TestPruneBuildCache(t *testing.T) {
	store := config.NewStoreWithEnv(t.TempDir(), "production")
	stub := &countingStub{Manager: runtime.NewStub()}
	runner := newFakeRunner("", "", "", "")
	m := &Manager{env: "production", rt: stub, store: store, run: runner}

	result := &Result{}
	if err := m.pruneBuildCache(context.Background(), Options{BuildCache: true}, result); err != nil {
		t.Fatal(err)
	}
	if result.BuildCacheReclaimed != "1.5 GB" {
		t.Fatalf("reclaimed = %q, want %q", result.BuildCacheReclaimed, "1.5 GB")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestExtractReclaimedSpace|TestPruneBuildCache" -v -count=1`
Expected: FAIL with `undefined: extractReclaimedSpace`, `undefined: pruneBuildCache`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cleanup/cleanup.go (append)
func (m *Manager) pruneBuildCache(ctx context.Context, opts Options, result *Result) error {
	if opts.DryRun {
		return nil
	}
	out, err := m.run(ctx, "builder", "prune", "-f")
	if err != nil {
		return err
	}
	if reclaimed := extractReclaimedSpace(out); reclaimed != "" {
		result.BuildCacheReclaimed = reclaimed
	}
	return nil
}

func extractReclaimedSpace(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -run "TestExtractReclaimedSpace|TestPruneBuildCache" -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): prune Docker build cache and report reclaimed space"
```

---

### Task 6: `Prune` orchestration + dry-run coverage

**Files:**
- Modify: `internal/cleanup/cleanup.go` — add the `Prune` method
- Test: `internal/cleanup/cleanup_test.go` — add `TestPruneNothingDisabled`, `TestPruneDryRunDoesNotRemove`

**Interfaces:**
- Consumes: `pruneContainers`, `pruneImages`, `pruneVolumes`, `pruneNetworks`, `pruneBuildCache` (Tasks 2-5)
- Produces: `Prune(ctx context.Context, opts Options) (*Result, error)` — the public entry point the CLI calls

- [ ] **Step 1: Write the failing tests**

```go
// internal/cleanup/cleanup_test.go (append)
func TestPruneNothingDisabled(t *testing.T) {
	store := config.NewStoreWithEnv(t.TempDir(), "production")
	stub := &countingStub{Manager: runtime.NewStub()}
	var calls []string
	runner := func(ctx context.Context, args ...string) (string, error) {
		calls = append(calls, args[0])
		return "", nil
	}
	m := &Manager{env: "production", rt: stub, store: store, run: runner}

	result, err := m.Prune(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected a result")
	}
	if len(calls) != 0 {
		t.Fatalf("docker called when all options disabled: %v", calls)
	}
}

func TestPruneDryRunDoesNotRemove(t *testing.T) {
	store := config.NewStoreWithEnv(t.TempDir(), "production")
	if err := store.SaveApp(types.AppEntry{Name: "app", Config: types.AppConfig{Name: "app"}}); err != nil {
		t.Fatal(err)
	}

	stub := &countingStub{Manager: runtime.NewStub()}
	runner := newFakeRunner(
		`{"ID":"1","Name":"/tengiz-app-prod-111","State":"exited","Labels":"tengiz-app=app,tengiz-env=production,tengiz-deployment=111"}`,
		"sha256:abc\n", "myvol\n", "custom-net\n")
	m := &Manager{env: "production", rt: stub, store: store, run: runner}

	result, err := m.Prune(context.Background(), Options{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
		Keep:       5,
		DryRun:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun {
		t.Fatal("DryRun flag not set in result")
	}
	if len(stub.removedContainers) != 0 || len(stub.removedImages) != 0 {
		t.Fatalf("dry run removed something: containers=%v images=%v", stub.removedContainers, stub.removedImages)
	}
	if len(stub.keepCalls) != 0 {
		t.Fatalf("dry run called KeepLastNImages: %v", stub.keepCalls)
	}
	if len(result.ContainersRemoved) != 1 || len(result.ImagesRemoved) != 1 ||
		len(result.VolumesRemoved) != 1 || len(result.NetworksRemoved) != 1 {
		t.Fatalf("dry-run report incomplete: %+v", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestPruneNothingDisabled|TestPruneDryRunDoesNotRemove" -v -count=1`
Expected: FAIL with `undefined: Prune`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cleanup/cleanup.go (append)
func (m *Manager) Prune(ctx context.Context, opts Options) (*Result, error) {
	result := &Result{DryRun: opts.DryRun}
	if opts.Keep < 1 {
		opts.Keep = 5
	}
	if opts.Containers {
		if err := m.pruneContainers(ctx, opts, result); err != nil {
			return nil, err
		}
	}
	if opts.Images {
		if err := m.pruneImages(ctx, opts, result); err != nil {
			return nil, err
		}
	}
	if opts.Volumes {
		if err := m.pruneVolumes(ctx, opts, result); err != nil {
			return nil, err
		}
	}
	if opts.Networks {
		if err := m.pruneNetworks(ctx, opts, result); err != nil {
			return nil, err
		}
	}
	if opts.BuildCache {
		if err := m.pruneBuildCache(ctx, opts, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: PASS (all cleanup package tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add Prune orchestration with dry-run support"
```

---

### Task 7: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — import `internal/cleanup`, register `cleanupCmd`, add `cleanupCmd` var, define flags in `Execute()`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.NewManager(rt, store, env)`, `cleanup.Options`, `cleanup.Result` (Tasks 1-6), `runtime.NewDocker()`, `config.NewStoreWithEnv(dataDir, env)`, `getEnv(cmd)`
- Produces: the `cleanupCmd *cobra.Command` with flags `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--keep`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatalf("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks", "build-cache", "keep"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdParsesFlags(t *testing.T) {
	var parsed bool
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		keep, _ := cmd.Flags().GetInt("keep")
		if !dryRun {
			t.Error("dry-run = false, want true")
		}
		if !buildCache {
			t.Error("build-cache = false, want true")
		}
		if keep != 10 {
			t.Errorf("keep = %d, want 10", keep)
		}
		parsed = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--build-cache", "--keep", "10"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !parsed {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupCommandFlags|TestCleanupCmdParsesFlags" -v -count=1`
Expected: FAIL — `cleanup` not registered / `undefined: cleanupCmd`.

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/root.go`:

(a) Add the import (in the existing import block, after the `builder` import):

```go
	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/cleanup"
```

(b) Register the command in `init()` (add after `rootCmd.AddCommand(runCmd)`):

```go
	rootCmd.AddCommand(cleanupCmd)
```

(c) Add the command definition (place after the `runCmd` var, before `gitCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove stale containers, images, and volumes (Docker housekeeping)",
	Long: `Remove stale Docker resources to reclaim disk space.

Label-aware cleanup only touches resources created by Tengiz (containers
labeled tengiz-app). Stopped versioned containers from failed or orphaned
deployments are removed; the active and previous deployments are always
preserved. Canonical containers (including scale-to-zero stopped apps) and
preview containers are never removed.

Use --dry-run to preview what would be removed.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		keep, _ := cmd.Flags().GetInt("keep")
		if keep < 1 {
			keep = 1
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		store := config.NewStoreWithEnv(dataDir, env)
		mgr := cleanup.NewManager(rt, store, env)

		result, err := mgr.Prune(cmd.Context(), cleanup.Options{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
			Keep:       keep,
			DryRun:     dryRun,
		})
		if err != nil {
			return err
		}

		mode := "removed"
		if dryRun {
			mode = "would remove"
		}
		fmt.Printf("[tengiz] cleanup (%s):\n", env)
		fmt.Printf("  containers %s: %d\n", mode, len(result.ContainersRemoved))
		fmt.Printf("  images %s: %d\n", mode, len(result.ImagesRemoved))
		fmt.Printf("  volumes %s: %d\n", mode, len(result.VolumesRemoved))
		fmt.Printf("  networks %s: %d\n", mode, len(result.NetworksRemoved))
		if result.BuildCacheReclaimed != "" {
			fmt.Printf("  build cache reclaimed: %s\n", result.BuildCacheReclaimed)
		}
		if len(result.ContainersRemoved) == 0 && len(result.ImagesRemoved) == 0 &&
			len(result.VolumesRemoved) == 0 && len(result.NetworksRemoved) == 0 {
			fmt.Println("  nothing to clean")
		}
		return nil
	},
}
```

(d) Define flags in `Execute()` (after the `configSetCmd` flag line):

```go
	cleanupCmd.Flags().Bool("dry-run", false, "report what would be removed without removing anything")
	cleanupCmd.Flags().Bool("containers", true, "remove stale stopped deployment containers")
	cleanupCmd.Flags().Bool("images", true, "remove dangling images and keep the newest N per app")
	cleanupCmd.Flags().Bool("volumes", true, "prune unused Docker volumes")
	cleanupCmd.Flags().Bool("networks", true, "prune unused Docker networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Int("keep", 5, "number of recent images to keep per app")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go build -o /tmp/tengiz . && go test ./internal/cli/... -run "TestCleanupCommand" -v -count=1`
Expected: PASS. Build succeeds and all three CLI tests pass.

- [ ] **Step 5: Run the full suite**

Run: `go test ./... -v -count=1 && go vet ./...`
Expected: PASS for all packages, `go vet` clean.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 8: Documentation

**Files:**
- Modify: `README.md` — add `tengiz cleanup` to the CLI Reference
- Modify: `AGENTS.md` — add the CLI entry + architecture table row
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 Docker Housekeeping as implemented

- [ ] **Step 1: Add the README section**

Insert a `### tengiz cleanup` section right after the `### tengiz ps` section in `README.md`:

```markdown
### `tengiz cleanup`

Remove stale Docker resources (containers, images, volumes, networks) to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stale stopped deployment containers (default: true) |
| `--images` | Remove dangling images and keep the newest N per app (default: true) |
| `--volumes` | Prune unused Docker volumes (default: true) |
| `--networks` | Prune unused Docker networks (default: true) |
| `--build-cache` | Prune Docker build cache (default: false) |
| `--keep N` | Number of recent images to keep per app (default: 5) |
| `--dry-run` | Report what would be removed without removing anything |

Label-aware cleanup only touches resources managed by Tengiz (labeled `tengiz-app`). Running containers, scale-to-zero-stopped apps, preview deployments, and the active/previous deployment of each app are always preserved. Env-scoped: `--env staging` only cleans the `staging` environment.
```

- [ ] **Step 2: Update AGENTS.md**

In the CLI section (after the `tengiz ps` line), add:

```
tengiz cleanup [--dry-run] [--keep N] → label-aware Docker housekeeping (stale containers, dangling images, unused volumes/networks, build cache)
```

In the key architecture table, add a row after the `health` row:

```
| `cleanup` | Label-aware Docker housekeeping (`tengiz cleanup`). Removes stale stopped versioned containers, keeps newest N images per app, prunes dangling images and unused volumes/networks/build-cache. Pure decision functions testable without Docker. |
```

- [ ] **Step 3: Update FUTURES_FEATURES.md**

(a) In the P0 table, change feature #6 row from ⬜ to ✅:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

(b) In the `✅ Implemented Features (Not Pending)` table, add:

```
| — | **Docker Housekeeping** | Çok Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-02) |
```

(c) In the detailed feature section `## Docker Housekeeping (Otomatik Temizlik)`, add a Status line after `- **Detected:** 2026-07-14`:

```
- **Status:** ✅ Implemented (2026-08-02)
```

- [ ] **Step 4: Full verification**

Run: `go build -o /tmp/tengiz . && go vet ./... && go test ./... -v -count=1`
Expected: build OK, vet clean, all tests PASS.

- [ ] **Step 5: Manual smoke test (optional, requires Docker)**

```bash
./tengiz cleanup --dry-run
./tengiz cleanup
./tengiz cleanup --env staging --build-cache --keep 3
```
Expected: dry-run prints what would be removed without deleting anything; real run prints a cleanup report.

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage:** Feature #6 (Docker Housekeeping) requires label-based cleaning of unused volumes, networks, containers, and images plus a `tengiz cleanup` command. Task 2 covers containers, Task 3 covers images, Task 4 covers volumes+networks, Task 5 covers build cache, Task 7 adds the CLI, Task 8 updates docs and marks the feature implemented. The "label-based filtering protects Tengiz-managed containers" requirement maps to the `tengiz-app`/`tengiz-env` filters and the rule that only stale versioned containers are removed.

**2. Placeholder scan:** Every step contains concrete code and exact commands; no TBD/TODO or "handle errors later" steps. The only intentionally no-op path is `pruneBuildCache` under dry-run, which is implemented and covered by `TestPruneDryRunDoesNotRemove`.

**3. Type consistency:** `Options`, `Result`, `Manager`, `NewManager`, `Prune`, `staleVersionedContainers`, `extractReclaimedSpace`, `pruneContainers/pruneImages/pruneVolumes/pruneNetworks/pruneBuildCache`, `containerInfo`, `countingStub`, and `newFakeRunner` use identical names and signatures across every task that references them. `runtime.Manager` methods (`Remove`, `RemoveImage`, `KeepLastNImages`) match the interface in `internal/runtime/runtime.go:31-49`.
