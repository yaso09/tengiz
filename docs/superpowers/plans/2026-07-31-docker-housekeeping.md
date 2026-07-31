# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that removes stale Tengiz-managed containers and optionally prunes dangling images and the Docker build cache, reclaiming disk space on single-server deployments.

**Architecture:** The runtime package gains three new `Manager` methods: `ListContainers` (lists every container labeled `tengiz-app` + `tengiz-env=<env>` with its labels), `PruneDanglingImages`, and `PruneBuildCache` (thin `docker` CLI wrappers returning docker's stdout). A pure helper `cli.staleContainers(containers, active)` decides which containers are safe to remove by comparing each container's `tengiz-deployment` label against the app's current `DeploymentSuffix` in the env-scoped config store. The `tengiz cleanup` cobra command orchestrates: compute stale containers, `docker rm -f` them, and optionally prune images/build cache. Only containers labeled `tengiz-app` are ever considered, so the active deployment and non-Tengiz containers are never touched.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` / `runtime.dockerRuntime` (exec-based), existing `config.Store`, existing `dockerPS`/`labelKey`/`envLabelKey` helpers. No new external dependencies.

## Global Constraints

- Only containers labeled `tengiz-app` are candidates for removal — never touch other containers
- `ListContainers` filters by both `label=tengiz-app` AND `label=tengiz-env=<env>` so envs stay isolated
- The active deployment of an app (container whose `tengiz-deployment` label equals the store's `DeploymentSuffix`, or the unversioned container when `DeploymentSuffix == ""`) is ALWAYS kept, even if stopped
- A versioned container (`tengiz-deployment` label set) is stale when its app is not in the store OR its deployment label differs from the app's current suffix
- An unversioned container (no `tengiz-deployment` label) is stale when its app is not in the store OR the app has a non-empty `DeploymentSuffix`
- `tengiz cleanup` with no flags removes stale containers only; `--images`, `--build-cache`, `--all`, `--dry-run` modify that
- All new `Manager` interface methods must also be added to `stubManager` and the three existing test mocks (`mockRTForDeploy`, idle `mockRuntime`, proxy `mockRuntime`) in the same task that adds the interface methods, or the repo will not compile
- No new external dependencies
- All commands run with `go test ./... -v -count=1` and `go vet ./...` green before each commit
- Feature branch: `git checkout -b feat/docker-housekeeping` before starting

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add 3 new methods to `Manager` interface + `stubManager` |
| `internal/runtime/housekeeping.go` | NEW: `ContainerInfo`, `parseLabels`, `parseContainerInfo`, `ListContainers`, `PruneDanglingImages`, `PruneBuildCache`, `pruneImageArgs`, `pruneBuildCacheArgs` |
| `internal/runtime/housekeeping_test.go` | NEW: tests for parsing + prune arg builders + stub methods |
| `internal/cli/cleanup.go` | NEW: `staleContainers` pure function, `runCleanup` orchestration, `cleanupCmd` cobra command |
| `internal/cli/cleanup_test.go` | NEW: `cleanupMockRuntime` (full `Manager` impl) + stale/cleanup/dry-run tests |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()`; add flags in `Execute()` |
| `internal/cli/root_test.go` | Add 3 new methods to `mockRTForDeploy` |
| `internal/idle/idle_test.go` | Add 3 new methods to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add 3 new methods to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 implemented |

---

### Task 1: Runtime container listing + interface extension

Add `ContainerInfo` and `ListContainers` to the runtime package, extend the `Manager` interface with all three new methods, and update `stubManager` plus the three existing test mocks so the whole repo still compiles.

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Modify: `internal/runtime/runtime.go:31-49` (interface) and `runtime.go:113-122` (stub)
- Modify: `internal/cli/root_test.go` (after line 99), `internal/idle/idle_test.go` (after line 16), `internal/proxy/proxy_test.go` (after line 34)
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `dockerPS` struct and `labelKey`/`envLabelKey` consts (already in `internal/runtime/docker.go`), `dockerRuntime` type
- Produces:
  - `type ContainerInfo struct { ID, Name, State, AppName, Environment, Deployment string }`
  - `Manager.ListContainers(ctx context.Context, env string) ([]ContainerInfo, error)`
  - `Manager.PruneDanglingImages(ctx context.Context) (string, error)`
  - `Manager.PruneBuildCache(ctx context.Context) (string, error)`
  - `func parseLabels(labelStr string) map[string]string`
  - `func parseContainerInfo(line string) ContainerInfo`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/housekeeping_test.go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestParseContainerInfo(t *testing.T) {
	line := `{"ID":"abc123","Name":"/tengiz-myapp-1712345678","State":"exited","Ports":"","Labels":"tengiz-app=myapp,tengiz-env=production,tengiz-deployment=1712345678"}`
	info := parseContainerInfo(line)
	want := ContainerInfo{
		ID:          "abc123",
		Name:        "tengiz-myapp-1712345678",
		State:       "exited",
		AppName:     "myapp",
		Environment: "production",
		Deployment:  "1712345678",
	}
	if !reflect.DeepEqual(info, want) {
		t.Errorf("parseContainerInfo() = %+v, want %+v", info, want)
	}
}

func TestParseContainerInfoEmptyLine(t *testing.T) {
	info := parseContainerInfo("")
	if info.Name != "" {
		t.Errorf("expected zero value for empty line, got %+v", info)
	}
}

func TestParseLabels(t *testing.T) {
	labels := parseLabels("tengiz-app=myapp,tengiz-env=staging,tengiz-deployment=v3")
	want := map[string]string{
		"tengiz-app":        "myapp",
		"tengiz-env":        "staging",
		"tengiz-deployment": "v3",
	}
	if !reflect.DeepEqual(labels, want) {
		t.Errorf("parseLabels() = %v, want %v", labels, want)
	}
}

func TestParseLabelsSingleKey(t *testing.T) {
	labels := parseLabels("tengiz-app=myapp")
	if labels["tengiz-app"] != "myapp" {
		t.Errorf("parseLabels() missing key, got %v", labels)
	}
}

func TestStubListContainers(t *testing.T) {
	m := NewStub()
	containers, err := m.ListContainers(context.Background(), "production")
	if err != nil {
		t.Fatalf("ListContainers() error = %v", err)
	}
	if containers != nil {
		t.Errorf("expected nil from stub, got %v", containers)
	}
}

func TestStubPruneDanglingImages(t *testing.T) {
	m := NewStub()
	out, err := m.PruneDanglingImages(context.Background())
	if err != nil {
		t.Fatalf("PruneDanglingImages() error = %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output from stub, got %q", out)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	out, err := m.PruneBuildCache(context.Background())
	if err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output from stub, got %q", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestParse|TestStubList|TestStubPrune" -v -count=1`

Expected: FAIL with `undefined: ContainerInfo`, `undefined: parseContainerInfo`, `undefined: parseLabels`, and interface errors (`stubManager does not implement Manager`).

- [ ] **Step 3: Create `internal/runtime/housekeeping.go`**

```go
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type ContainerInfo struct {
	ID          string
	Name        string
	State       string
	AppName     string
	Environment string
	Deployment  string
}

func parseLabels(labelStr string) map[string]string {
	labels := make(map[string]string)
	for _, part := range strings.Split(labelStr, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			labels[kv[0]] = kv[1]
		} else if len(kv) == 1 && kv[0] != "" {
			labels[kv[0]] = ""
		}
	}
	return labels
}

func parseContainerInfo(line string) ContainerInfo {
	var entry dockerPS
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return ContainerInfo{}
	}
	labels := parseLabels(entry.Labels)
	return ContainerInfo{
		ID:          entry.ID,
		Name:        strings.TrimPrefix(entry.Name, "/"),
		State:       entry.State,
		AppName:     labels[labelKey],
		Environment: labels[envLabelKey],
		Deployment:  labels["tengiz-deployment"],
	}
}

func (r *dockerRuntime) ListContainers(ctx context.Context, env string) ([]ContainerInfo, error) {
	if env == "" {
		env = "production"
	}
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s", labelKey),
		"--filter", fmt.Sprintf("label=%s=%s", envLabelKey, env),
		"--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var containers []ContainerInfo
	for _, line := range lines {
		if line == "" {
			continue
		}
		info := parseContainerInfo(line)
		if info.Name != "" {
			containers = append(containers, info)
		}
	}
	return containers, nil
}
```

- [ ] **Step 4: Extend the `Manager` interface and stub in `internal/runtime/runtime.go`**

Add these three lines to the `Manager` interface (after the `KeepLastNImages` line):

```go
	ListContainers(ctx context.Context, env string) ([]ContainerInfo, error)
	PruneDanglingImages(ctx context.Context) (string, error)
	PruneBuildCache(ctx context.Context) (string, error)
```

Add these three methods to `stubManager` (after the `KeepLastNImages` stub):

```go
func (m *stubManager) ListContainers(ctx context.Context, env string) ([]ContainerInfo, error) {
	return nil, nil
}

func (m *stubManager) PruneDanglingImages(ctx context.Context) (string, error) {
	return "", nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 5: Update the three existing test mocks so the repo compiles**

In `internal/cli/root_test.go` (after the `KeepLastNImages` method on `mockRTForDeploy`):

```go
func (m *mockRTForDeploy) ListContainers(ctx context.Context, env string) ([]runtime.ContainerInfo, error) { return nil, nil }
func (m *mockRTForDeploy) PruneDanglingImages(ctx context.Context) (string, error) { return "", nil }
func (m *mockRTForDeploy) PruneBuildCache(ctx context.Context) (string, error) { return "", nil }
```

In `internal/idle/idle_test.go` (after the `KeepLastNImages` method on `mockRuntime`):

```go
func (m *mockRuntime) ListContainers(ctx context.Context, env string) ([]runtime.ContainerInfo, error) { return nil, nil }
func (m *mockRuntime) PruneDanglingImages(ctx context.Context) (string, error) { return "", nil }
func (m *mockRuntime) PruneBuildCache(ctx context.Context) (string, error) { return "", nil }
```

In `internal/proxy/proxy_test.go` (after the `KeepLastNImages` method on `mockRuntime`):

```go
func (m *mockRuntime) ListContainers(ctx context.Context, env string) ([]runtime.ContainerInfo, error) { return nil, nil }
func (m *mockRuntime) PruneDanglingImages(ctx context.Context) (string, error) { return "", nil }
func (m *mockRuntime) PruneBuildCache(ctx context.Context) (string, error) { return "", nil }
```

- [ ] **Step 6: Run the tests and full suite**

Run: `go test ./... -v -count=1`
Expected: PASS — all runtime tests including the new `TestParse*`/`TestStub*` pass, and `go vet ./...` is clean.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go internal/runtime/runtime.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add ListContainers and prune methods to Manager"
```

---

### Task 2: Runtime dangling-image and build-cache pruning

Implement the two prune methods in the runtime package with testable arg builders.

**Files:**
- Modify: `internal/runtime/housekeeping.go` (append below `ListContainers`)
- Test: `internal/runtime/housekeeping_test.go` (append)

**Interfaces:**
- Consumes: `dockerRuntime`, `Manager.PruneDanglingImages(ctx) (string, error)`, `Manager.PruneBuildCache(ctx) (string, error)` (already declared in Task 1)
- Produces: `func pruneImageArgs() []string`, `func pruneBuildCacheArgs() []string` (used only by this task's implementation)

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/runtime/housekeeping_test.go
func TestPruneImageArgs(t *testing.T) {
	want := []string{"image", "prune", "-f", "--filter", "dangling=true"}
	got := pruneImageArgs()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneImageArgs() = %v, want %v", got, want)
	}
}

func TestPruneBuildCacheArgs(t *testing.T) {
	want := []string{"builder", "prune", "-f"}
	got := pruneBuildCacheArgs()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneBuildCacheArgs() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneImageArgs|TestPruneBuildCacheArgs" -v -count=1`

Expected: FAIL with `undefined: pruneImageArgs`, `undefined: pruneBuildCacheArgs`.

- [ ] **Step 3: Write the minimal implementation**

Append to `internal/runtime/housekeeping.go`:

```go
func pruneImageArgs() []string {
	return []string{"image", "prune", "-f", "--filter", "dangling=true"}
}

func pruneBuildCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func (r *dockerRuntime) PruneDanglingImages(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", pruneImageArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", pruneBuildCacheArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return string(out), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneImageArgs|TestPruneBuildCacheArgs|TestStubPrune" -v -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat(runtime): add dangling image and build cache pruning"
```

---

### Task 3: Stale-container detection logic

Add the pure `staleContainers` function to the CLI package.

**Files:**
- Create: `internal/cli/cleanup.go` (function `staleContainers` only in this task)
- Test: `internal/cli/cleanup_test.go` (stale tests only in this task)

**Interfaces:**
- Consumes: `runtime.ContainerInfo` (Task 1)
- Produces: `func staleContainers(containers []runtime.ContainerInfo, active map[string]string) []runtime.ContainerInfo` where `active` maps app name → current `DeploymentSuffix`; `cleanupOptions` struct and `runCleanup` come in Task 4

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestStaleContainers(t *testing.T) {
	active := map[string]string{"myapp": "v3"}
	tests := []struct {
		name      string
		container runtime.ContainerInfo
		wantStale bool
	}{
		{"active versioned", runtime.ContainerInfo{Name: "tengiz-myapp-v3", AppName: "myapp", Deployment: "v3"}, false},
		{"old versioned", runtime.ContainerInfo{Name: "tengiz-myapp-v1", AppName: "myapp", Deployment: "v1"}, true},
		{"unversioned with versioned active", runtime.ContainerInfo{Name: "tengiz-myapp", AppName: "myapp", Deployment: ""}, true},
		{"orphan unversioned", runtime.ContainerInfo{Name: "tengiz-orphan", AppName: "orphan", Deployment: ""}, true},
		{"orphan versioned", runtime.ContainerInfo{Name: "tengiz-orphan-v9", AppName: "orphan", Deployment: "v9"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stale := staleContainers([]runtime.ContainerInfo{tt.container}, active)
			if got := len(stale) == 1; got != tt.wantStale {
				t.Errorf("staleContainers() stale=%v, want %v (container %+v)", got, tt.wantStale, tt.container)
			}
		})
	}
}

func TestStaleContainersFirstDeployKept(t *testing.T) {
	active := map[string]string{"myapp": ""}
	c := runtime.ContainerInfo{Name: "tengiz-myapp", AppName: "myapp", Deployment: ""}
	if stale := staleContainers([]runtime.ContainerInfo{c}, active); len(stale) != 0 {
		t.Errorf("expected first-deploy unversioned container to be kept, got %v", stale)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestStaleContainers" -v -count=1`

Expected: FAIL with `undefined: staleContainers`.

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/cli/cleanup.go
package cli

import (
	"github.com/yaso09/tengiz/internal/runtime"
)

func staleContainers(containers []runtime.ContainerInfo, active map[string]string) []runtime.ContainerInfo {
	var stale []runtime.ContainerInfo
	for _, c := range containers {
		current, known := active[c.AppName]
		if c.Deployment != "" {
			if !known || current != c.Deployment {
				stale = append(stale, c)
			}
		} else {
			if !known || current != "" {
				stale = append(stale, c)
			}
		}
	}
	return stale
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestStaleContainers" -v -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add stale container detection logic"
```

---

### Task 4: `tengiz cleanup` command

Wire up the `runCleanup` orchestration and the cobra command, and test it with a mock runtime.

**Files:**
- Modify: `internal/cli/cleanup.go` (append `cleanupOptions`, `runCleanup`, `cleanupCmd`)
- Modify: `internal/cli/root.go` (`init()` at line 38 area: `rootCmd.AddCommand(cleanupCmd)`; `Execute()` flags block)
- Test: `internal/cli/cleanup_test.go` (append `cleanupMockRuntime`, `TestRunCleanup`, `TestRunCleanupDryRun`)

**Interfaces:**
- Consumes: `staleContainers` (Task 3), `Manager.ListContainers` / `Manager.PruneDanglingImages` / `Manager.PruneBuildCache` (Tasks 1-2), `config.Store`, `config.NewStoreWithEnv`
- Produces: `type cleanupOptions struct { Containers, Images, BuildCache, DryRun bool }`, `func runCleanup(ctx context.Context, rt runtime.Manager, store *config.Store, env string, opts cleanupOptions) ([]runtime.ContainerInfo, error)`, `cleanupCmd *cobra.Command`

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/cli/cleanup_test.go
import (
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type cleanupMockRuntime struct {
	containers   []runtime.ContainerInfo
	removed      []string
	prunedImages bool
	prunedCache  bool
}

func (m *cleanupMockRuntime) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *cleanupMockRuntime) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *cleanupMockRuntime) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error { return nil }
func (m *cleanupMockRuntime) Start(ctx context.Context, name string) error { return nil }
func (m *cleanupMockRuntime) Stop(ctx context.Context, name string) error { return nil }
func (m *cleanupMockRuntime) Restart(ctx context.Context, name string) error { return nil }
func (m *cleanupMockRuntime) Remove(ctx context.Context, name string) error { m.removed = append(m.removed, name); return nil }
func (m *cleanupMockRuntime) RemoveBySuffix(ctx context.Context, name string, suffix string) error { return nil }
func (m *cleanupMockRuntime) IsActive(ctx context.Context, name string) (bool, error) { return true, nil }
func (m *cleanupMockRuntime) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) { return 0, nil }
func (m *cleanupMockRuntime) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *cleanupMockRuntime) Logs(ctx context.Context, name string, opts runtime.LogOptions) (io.ReadCloser, error) { return nil, nil }
func (m *cleanupMockRuntime) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }
func (m *cleanupMockRuntime) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error { return nil }
func (m *cleanupMockRuntime) RemoveImage(ctx context.Context, imageTag string) error { return nil }
func (m *cleanupMockRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error { return nil }
func (m *cleanupMockRuntime) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts runtime.RunOptions) error { return nil }
func (m *cleanupMockRuntime) ListContainers(ctx context.Context, env string) ([]runtime.ContainerInfo, error) { return m.containers, nil }
func (m *cleanupMockRuntime) PruneDanglingImages(ctx context.Context) (string, error) { m.prunedImages = true; return "Total reclaimed space: 1.2GB\n", nil }
func (m *cleanupMockRuntime) PruneBuildCache(ctx context.Context) (string, error) { m.prunedCache = true; return "Total reclaimed space: 512MB\n", nil }

func TestRunCleanup(t *testing.T) {
	store := config.NewStore(t.TempDir())
	if err := store.SaveApp(types.AppEntry{Name: "myapp", DeploymentSuffix: "v3"}); err != nil {
		t.Fatal(err)
	}
	mock := &cleanupMockRuntime{
		containers: []runtime.ContainerInfo{
			{Name: "tengiz-myapp-v3", AppName: "myapp", Deployment: "v3"},
			{Name: "tengiz-myapp-v1", AppName: "myapp", Deployment: "v1"},
			{Name: "tengiz-orphan", AppName: "orphan", Deployment: ""},
			{Name: "tengiz-myapp", AppName: "myapp", Deployment: ""},
		},
	}
	stale, err := runCleanup(context.Background(), mock, store, "production", cleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if len(stale) != 3 {
		t.Fatalf("expected 3 stale containers, got %d: %v", len(stale), stale)
	}
	want := []string{"tengiz-myapp-v1", "tengiz-orphan", "tengiz-myapp"}
	if !reflect.DeepEqual(mock.removed, want) {
		t.Errorf("removed = %v, want %v", mock.removed, want)
	}
}

func TestRunCleanupDryRun(t *testing.T) {
	store := config.NewStore(t.TempDir())
	if err := store.SaveApp(types.AppEntry{Name: "myapp", DeploymentSuffix: "v3"}); err != nil {
		t.Fatal(err)
	}
	mock := &cleanupMockRuntime{
		containers: []runtime.ContainerInfo{
			{Name: "tengiz-myapp-v1", AppName: "myapp", Deployment: "v1"},
		},
	}
	stale, err := runCleanup(context.Background(), mock, store, "production", cleanupOptions{Containers: true, DryRun: true})
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale container, got %d", len(stale))
	}
	if len(mock.removed) != 0 {
		t.Errorf("dry-run must not remove containers, removed = %v", mock.removed)
	}
}
```

Note: the `import` block in this file must be merged with the existing one from Task 3 (`testing`, `runtime`) — combine into a single import block when appending.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestRunCleanup" -v -count=1`

Expected: FAIL with `undefined: runCleanup`, `undefined: cleanupOptions`.

- [ ] **Step 3: Append the orchestration function and command to `internal/cli/cleanup.go`**

```go
type cleanupOptions struct {
	Containers bool
	Images     bool
	BuildCache bool
	DryRun     bool
}

func runCleanup(ctx context.Context, rt runtime.Manager, store *config.Store, env string, opts cleanupOptions) ([]runtime.ContainerInfo, error) {
	apps, err := store.ListApps()
	if err != nil {
		return nil, err
	}
	active := make(map[string]string, len(apps))
	for _, app := range apps {
		active[app.Name] = app.DeploymentSuffix
	}

	containers, err := rt.ListContainers(ctx, env)
	if err != nil {
		return nil, err
	}
	stale := staleContainers(containers, active)

	if !opts.DryRun {
		for _, c := range stale {
			if err := rt.Remove(ctx, c.Name); err != nil {
				return stale, err
			}
		}
	}
	return stale, nil
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove stale Tengiz containers and prune Docker resources",
	Long: `Remove stale containers and prune Docker resources to reclaim disk space.

Only containers managed by Tengiz (labeled tengiz-app=*) are ever considered;
the active deployment of each app is always kept. Old versioned containers
from previous zero-downtime deploys, orphaned containers, and stopped
containers no longer associated with an active app are removed.

Flags:
  --containers   remove stale Tengiz containers (default)
  --images       prune dangling (untagged) Docker images
  --build-cache  prune the Docker build cache
  --all          remove containers, prune images and build cache
  --dry-run      show what would be removed without removing anything`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := cleanupOptions{}
		if all, _ := cmd.Flags().GetBool("all"); all {
			opts.Containers = true
			opts.Images = true
			opts.BuildCache = true
		} else {
			opts.Containers, _ = cmd.Flags().GetBool("containers")
			opts.Images, _ = cmd.Flags().GetBool("images")
			opts.BuildCache, _ = cmd.Flags().GetBool("build-cache")
		}
		opts.DryRun, _ = cmd.Flags().GetBool("dry-run")

		if opts.Containers {
			stale, err := runCleanup(cmd.Context(), rt, store, env, opts)
			if err != nil {
				return fmt.Errorf("container cleanup: %w", err)
			}
			if len(stale) == 0 {
				fmt.Println("[tengiz] no stale containers found")
			}
			for _, c := range stale {
				verb := "removed"
				if opts.DryRun {
					verb = "would remove"
				}
				fmt.Printf("[tengiz] %s stale container %s (app=%s)\n", verb, c.Name, c.AppName)
			}
		}

		if opts.Images {
			if opts.DryRun {
				fmt.Println("[tengiz] would prune dangling images")
			} else {
				out, err := rt.PruneDanglingImages(cmd.Context())
				if err != nil {
					return fmt.Errorf("image prune: %w", err)
				}
				if strings.TrimSpace(out) != "" {
					fmt.Print(out)
				}
			}
		}

		if opts.BuildCache {
			if opts.DryRun {
				fmt.Println("[tengiz] would prune build cache")
			} else {
				out, err := rt.PruneBuildCache(cmd.Context())
				if err != nil {
					return fmt.Errorf("build cache prune: %w", err)
				}
				if strings.TrimSpace(out) != "" {
					fmt.Print(out)
				}
			}
		}
		return nil
	},
}
```

Update the imports of `internal/cli/cleanup.go` to:

```go
import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)
```

- [ ] **Step 4: Register the command in `internal/cli/root.go`**

In `init()` (after `rootCmd.AddCommand(rollbackCmd)`):

```go
	rootCmd.AddCommand(cleanupCmd)
```

In `Execute()` (with the other command flags):

```go
	cleanupCmd.Flags().Bool("containers", true, "remove stale Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling (untagged) Docker images")
	cleanupCmd.Flags().Bool("build-cache", false, "prune the Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "remove containers, prune images and build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./... -v -count=1`

Expected: PASS — new `TestRunCleanup`/`TestRunCleanupDryRun` pass, all existing tests still pass.

- [ ] **Step 6: Run vet**

Run: `go vet ./...`

Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Documentation

Update README and the features doc.

**Files:**
- Modify: `README.md` (commands section, after the `tengiz rollback` section)
- Modify: `docs/FUTURES_FEATURES.md` (row 6 in P0 table + Implemented table)

**Interfaces:**
- Consumes: `tengiz cleanup` command from Task 4

- [ ] **Step 1: Document `tengiz cleanup` in `README.md`**

Find the `### \`tengiz rollback <app>\`` section in `README.md` and add the following section after it:

```markdown
### `tengiz cleanup`

Remove stale Tengiz containers and prune Docker resources to reclaim disk space.

Only containers managed by Tengiz (labeled `tengiz-app=*`) are ever considered — the active deployment of each app is always kept. Old versioned containers from previous zero-downtime deploys, orphaned containers, and stopped containers no longer associated with an active app are removed.

```bash
tengiz cleanup               # remove stale containers only
tengiz cleanup --images      # also prune dangling (untagged) images
tengiz cleanup --build-cache # also prune the Docker build cache
tengiz cleanup --all         # containers + images + build cache
tengiz cleanup --dry-run     # show what would be removed, remove nothing
```
```

- [ ] **Step 2: Update `docs/FUTURES_FEATURES.md`**

Change the P0 table row for feature #6 from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

And add a row to the `✅ Implemented Features (Not Pending)` table:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-07-31) |
```

- [ ] **Step 3: Verify the full suite**

Run: `go test ./... -v -count=1 && go vet ./...`

Expected: PASS and clean.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage** (from `docs/FUTURES_FEATURES.md` #6 — "Label-based `docker system prune`. `tengiz cleanup`."):
- `tengiz cleanup` command → Task 4
- Label-based pruning (only `tengiz-app`/`tengiz-env` labeled containers considered) → Task 1 (`ListContainers` filters), Task 3 (`staleContainers`)
- Disk-space reclamation via dangling-image + build-cache pruning → Task 2
- Active deployments always protected → Task 3 stale rules + tests
- No gaps.

**2. Placeholder scan:** All steps contain complete code and exact commands; no TBD/TODO/"similar to Task N" references.

**3. Type consistency:**
- `ContainerInfo` fields (`Name`, `AppName`, `Deployment`, `Environment`) defined in Task 1 and used consistently in Tasks 3-4.
- `Manager.ListContainers(ctx, env) ([]ContainerInfo, error)`, `PruneDanglingImages(ctx) (string, error)`, `PruneBuildCache(ctx) (string, error)` declared in Task 1, implemented in Tasks 1-2, consumed in Task 4 with matching signatures.
- `staleContainers(containers []runtime.ContainerInfo, active map[string]string) []runtime.ContainerInfo` defined in Task 3, consumed in Task 4 identically.
- `cleanupOptions{Containers, Images, BuildCache, DryRun}` defined in Task 4 and used only there.
- `runCleanup(ctx, rt, store, env, opts)` signature matches between Task 4 implementation and tests.
