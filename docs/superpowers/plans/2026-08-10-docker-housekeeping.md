# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` so operators can reclaim disk space on a single-server Tengiz instance by removing stale stopped containers, dangling images, and the Docker build cache — without ever touching containers Tengiz still manages.

**Architecture:** The `runtime.Manager` interface gains five housekeeping operations that wrap `os/exec` calls to the Docker CLI (mirroring existing arg-builder + `dockerRuntime` patterns). A new `internal/cleanup` package orchestrates them: it lists stopped containers labeled `tengiz-app` + `tengiz-env=<env>`, diffs them against the set of container names currently registered in the env-scoped store (apps + preview deployments), and removes only the unregistered ("stale") ones, then prunes dangling images and build cache. A `--dry-run` flag previews without mutating; `--all` additionally prunes unused volumes and networks.

**Tech Stack:** Go 1.26, `os/exec` Docker CLI (no SDK), Cobra, existing `runtime.Manager`, `config.Store`, `runtime.ContainerName` helpers.

## Global Constraints

- No new external dependencies — all Docker interactions via `os/exec` `docker <...>` CLI
- Stopped-container discovery is env-scoped via the `tengiz-env` label; env defaults to `"production"`
- Containers registered in the env-scoped store (apps + previews) are ALWAYS protected — never removed by cleanup
- Container name conventions reused verbatim: `runtime.ContainerName(name, env)` and preview pattern `tengiz-<app>-pr-<n>`
- Default cleanup removes: stale stopped containers, dangling images, build cache. `--all` adds unused volumes + networks
- Volume prune always filtered with `--filter label!=tengiz-app` (defense in depth)
- `go test ./... -v -count=1` and `go vet ./...` must stay green; existing tests must not be weakened
- Per-category prune subcommands (#56) and scheduled/periodic cleanup are OUT OF SCOPE — this plan ships the manual `tengiz cleanup` op only
- Every task ends with an independent testable deliverable + commit

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add 5 methods to `Manager` interface + `stubManager` no-op impls |
| `internal/runtime/cleanup.go` | Pure arg-builders + `dockerRuntime` exec implementations for the 5 ops |
| `internal/runtime/cleanup_test.go` | Unit tests for the arg-builders + interface-satisfaction check |
| `internal/cleanup/cleanup.go` | NEW package: `ContainerOps` interface, `Cleaner`, `Result`, `New()` |
| `internal/cleanup/cleanup_test.go` | NEW package: fake runtime + `Cleaner` orchestration tests |
| `internal/cli/root.go` | `cleanupCmd` + registration + flags |
| `internal/cli/cleanup_test.go` | NEW: CLI registration + flag tests |
| `internal/proxy/proxy_test.go`, `internal/idle/idle_test.go`, `internal/cli/root_test.go` | Append 5 stub methods to existing `runtime.Manager` mocks so they keep compiling |
| `README.md`, `AGENTS.md`, `docs/FUTURES_FEATURES.md` | CLI reference + roadmap status updates |

One new package, three test files added, five production files touched.

---

### Task 1: Runtime prune/list arg-builders (test-first)

**Files:**
- Modify: `internal/runtime/cleanup.go` — add pure builder functions
- Test: `internal/runtime/cleanup_test.go` — add builder tests

**Interfaces:**
- Consumes: nothing
- Produces: `buildListStoppedArgs(env string) []string`, `buildImagePruneArgs() []string`, `buildBuildCachePruneArgs() []string`, `buildVolumePruneArgs() []string`, `buildNetworkPruneArgs() []string` — used by Task 2's `dockerRuntime` methods

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestBuildListStoppedArgs(t *testing.T) {
	got := buildListStoppedArgs("production")
	want := []string{
		"ps", "-aq",
		"--filter", "label=tengiz-app",
		"--filter", "label=tengiz-env=production",
		"--filter", "status=exited",
		"--format", "{{.Names}}",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildListStoppedArgsEmptyEnvDefaultsProduction(t *testing.T) {
	got := buildListStoppedArgs("")
	found := false
	for _, a := range got {
		if a == "label=tengiz-env=production" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected tengiz-env=production filter, got %v", got)
	}
}

func TestBuildImagePruneArgs(t *testing.T) {
	got := buildImagePruneArgs()
	want := []string{"image", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildBuildCachePruneArgs(t *testing.T) {
	got := buildBuildCachePruneArgs()
	want := []string{"builder", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildVolumePruneArgs(t *testing.T) {
	got := buildVolumePruneArgs()
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildNetworkPruneArgs(t *testing.T) {
	got := buildNetworkPruneArgs()
	want := []string{"network", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestBuildListStoppedArgs|TestBuildListStoppedArgsEmptyEnvDefaultsProduction|TestBuildImagePruneArgs|TestBuildBuildCachePruneArgs|TestBuildVolumePruneArgs|TestBuildNetworkPruneArgs" -v -count=1`
Expected: FAIL with `undefined: buildListStoppedArgs` (and the others)

- [ ] **Step 3: Write the minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
func buildListStoppedArgs(env string) []string {
	if env == "" {
		env = "production"
	}
	return []string{
		"ps", "-aq",
		"--filter", "label=tengiz-app",
		"--filter", fmt.Sprintf("label=tengiz-env=%s", env),
		"--filter", "status=exited",
		"--format", "{{.Names}}",
	}
}

func buildImagePruneArgs() []string {
	return []string{"image", "prune", "-f"}
}

func buildBuildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func buildVolumePruneArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func buildNetworkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestBuildListStoppedArgs|TestBuildListStoppedArgsEmptyEnvDefaultsProduction|TestBuildImagePruneArgs|TestBuildBuildCachePruneArgs|TestBuildVolumePruneArgs|TestBuildNetworkPruneArgs" -v -count=1`
Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add docker prune and stopped-container arg builders"
```

---

### Task 2: Extend `runtime.Manager` with housekeeping ops

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add 5 methods to `Manager` interface + `stubManager`
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime` exec implementations
- Modify: `internal/proxy/proxy_test.go`, `internal/idle/idle_test.go`, `internal/cli/root_test.go` — append stub methods to mocks so they keep satisfying `runtime.Manager`
- Test: `internal/runtime/cleanup_test.go` — interface-satisfaction compile test

**Interfaces:**
- Consumes: `buildListStoppedArgs`, `buildImagePruneArgs`, `buildBuildCachePruneArgs`, `buildVolumePruneArgs`, `buildNetworkPruneArgs` from Task 1
- Produces: `Manager.ListStoppedContainers(ctx, env) ([]string, error)`, `Manager.PruneDanglingImages(ctx) error`, `Manager.PruneBuildCache(ctx) error`, `Manager.PruneUnusedVolumes(ctx) error`, `Manager.PruneUnusedNetworks(ctx) error` — consumed by Task 3's `Cleaner`

- [ ] **Step 1: Write the failing compile-time check**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestDockerRuntimeSatisfiesCleanupOps(t *testing.T) {
	var iface interface {
		ListStoppedContainers(context.Context, string) ([]string, error)
		PruneDanglingImages(context.Context) error
		PruneBuildCache(context.Context) error
		PruneUnusedVolumes(context.Context) error
		PruneUnusedNetworks(context.Context) error
	} = &dockerRuntime{}
	if iface == nil {
		t.Fatal("dockerRuntime does not satisfy cleanup ops")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/runtime/... -run TestDockerRuntimeSatisfiesCleanupOps -v -count=1`
Expected: FAIL — `dockerRuntime does not implement ... (missing method ListStoppedContainers)`

- [ ] **Step 3: Add the 5 methods to the `Manager` interface**

In `internal/runtime/runtime.go`, after the `KeepLastNImages` line in the interface:

```go
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	ListStoppedContainers(ctx context.Context, env string) ([]string, error)
	PruneDanglingImages(ctx context.Context) error
	PruneBuildCache(ctx context.Context) error
	PruneUnusedVolumes(ctx context.Context) error
	PruneUnusedNetworks(ctx context.Context) error
```

- [ ] **Step 4: Add the stub implementations**

In `internal/runtime/runtime.go`, after the existing `keepLastNImages` stub method (`func (m *stubManager) KeepLastNImages(...)`), add:

```go
func (m *stubManager) ListStoppedContainers(ctx context.Context, env string) ([]string, error) {
	return nil, nil
}

func (m *stubManager) PruneDanglingImages(ctx context.Context) error {
	return nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context) error {
	return nil
}

func (m *stubManager) PruneUnusedVolumes(ctx context.Context) error {
	return nil
}

func (m *stubManager) PruneUnusedNetworks(ctx context.Context) error {
	return nil
}
```

- [ ] **Step 5: Add the `dockerRuntime` exec implementations**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) ListStoppedContainers(ctx context.Context, env string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildListStoppedArgs(env)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, strings.TrimPrefix(line, "/"))
		}
	}
	return names, nil
}

func (r *dockerRuntime) PruneDanglingImages(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "docker", buildImagePruneArgs()...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "docker", buildBuildCachePruneArgs()...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneUnusedVolumes(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "docker", buildVolumePruneArgs()...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneUnusedNetworks(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "docker", buildNetworkPruneArgs()...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return nil
}
```

- [ ] **Step 6: Update the mock runtimes so tests keep compiling**

Append these 5 methods to each of the three mock types. All three are `runtime.Manager` mocks whose package-level tests would otherwise fail to compile after the interface change:

`internal/proxy/proxy_test.go` → type `mockRuntime`:
```go
func (m *mockRuntime) ListStoppedContainers(ctx context.Context, env string) ([]string, error) { return nil, nil }
func (m *mockRuntime) PruneDanglingImages(ctx context.Context) error { return nil }
func (m *mockRuntime) PruneBuildCache(ctx context.Context) error { return nil }
func (m *mockRuntime) PruneUnusedVolumes(ctx context.Context) error { return nil }
func (m *mockRuntime) PruneUnusedNetworks(ctx context.Context) error { return nil }
```

`internal/idle/idle_test.go` → type `mockRuntime`:
```go
func (m *mockRuntime) ListStoppedContainers(ctx context.Context, env string) ([]string, error) { return nil, nil }
func (m *mockRuntime) PruneDanglingImages(ctx context.Context) error { return nil }
func (m *mockRuntime) PruneBuildCache(ctx context.Context) error { return nil }
func (m *mockRuntime) PruneUnusedVolumes(ctx context.Context) error { return nil }
func (m *mockRuntime) PruneUnusedNetworks(ctx context.Context) error { return nil }
```

`internal/cli/root_test.go` → type `mockRTForDeploy` (append after the existing `KeepLastNImages` method):
```go
func (m *mockRTForDeploy) ListStoppedContainers(ctx context.Context, env string) ([]string, error) { return nil, nil }
func (m *mockRTForDeploy) PruneDanglingImages(ctx context.Context) error { return nil }
func (m *mockRTForDeploy) PruneBuildCache(ctx context.Context) error { return nil }
func (m *mockRTForDeploy) PruneUnusedVolumes(ctx context.Context) error { return nil }
func (m *mockRTForDeploy) PruneUnusedNetworks(ctx context.Context) error { return nil }
```

- [ ] **Step 7: Run tests + build to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: ALL PASS (incl. new compile-time check)

Run: `go build ./...`
Expected: Build succeeds

Run: `go test ./internal/proxy/... ./internal/idle/... ./internal/cli/... -count=1`
Expected: PASS (proxy tests slow, ~2s each due to TCP dial timeout — expected)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: extend runtime.Manager with cleanup ops (stopped containers, prune images/cache/volumes/networks)"
```

---

### Task 3: `Cleaner` orchestrator in new `internal/cleanup` package

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Create: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `Manager` methods from Task 2 (via the narrower `ContainerOps` interface), `config.Store`, `runtime.ContainerName(name, env) string`
- Produces: `cleanup.New(rt ContainerOps, store *config.Store, env string) *Cleaner`, `(*Cleaner).Run(ctx context.Context, dryRun, all bool) (*Result, error)`, `cleanup.Result` — consumed by Task 4's CLI command

- [ ] **Step 1: Write the failing tests**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"sort"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)

type fakeRuntime struct {
	stopped     []string
	removed     []string
	prunedImage bool
	prunedCache bool
	prunedVols  bool
	prunedNets  bool
}

func (f *fakeRuntime) ListStoppedContainers(ctx context.Context, env string) ([]string, error) {
	return f.stopped, nil
}

func (f *fakeRuntime) Remove(ctx context.Context, name string) error {
	f.removed = append(f.removed, name)
	return nil
}

func (f *fakeRuntime) PruneDanglingImages(ctx context.Context) error {
	f.prunedImage = true
	return nil
}

func (f *fakeRuntime) PruneBuildCache(ctx context.Context) error {
	f.prunedCache = true
	return nil
}

func (f *fakeRuntime) PruneUnusedVolumes(ctx context.Context) error {
	f.prunedVols = true
	return nil
}

func (f *fakeRuntime) PruneUnusedNetworks(ctx context.Context) error {
	f.prunedNets = true
	return nil
}

func TestFakeRuntimeImplementsContainerOps(t *testing.T) {
	var _ ContainerOps = &fakeRuntime{}
}

func setupProductionStore(t *testing.T, dir string) *config.Store {
	store := config.NewStore(dir)
	if err := store.SaveApp(types.AppEntry{
		Name:             "myapp",
		Port:             9000,
		Environment:      "production",
		DeploymentSuffix: "1750000000",
		Config: types.AppConfig{
			Name:        "myapp",
			Environment: "production",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddPreview(types.PreviewEntry{
		AppName:  "myapp",
		PRNumber: 3,
	}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestRunRemovesOnlyStaleContainers(t *testing.T) {
	store := setupProductionStore(t, t.TempDir())
	rt := &fakeRuntime{
		stopped: []string{
			"tengiz-myapp",
			"tengiz-myapp-1750000000",
			"tengiz-myapp-pr-3",
			"tengiz-ghost",
			"tengiz-orphan-123",
		},
	}
	c := New(rt, store, "production")

	res, err := c.Run(context.Background(), false, false)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	sort.Strings(rt.removed)
	want := []string{"tengiz-ghost", "tengiz-orphan-123"}
	if len(rt.removed) != len(want) {
		t.Fatalf("removed = %v, want %v", rt.removed, want)
	}
	for i := range want {
		if rt.removed[i] != want[i] {
			t.Fatalf("removed[%d] = %q, want %q", i, rt.removed[i], want[i])
		}
	}
	if len(res.StaleContainers) != 2 {
		t.Errorf("StaleContainers = %v, want 2 entries", res.StaleContainers)
	}
	if !rt.prunedImage || !rt.prunedCache {
		t.Error("expected image + build cache prunes")
	}
	if rt.prunedVols || rt.prunedNets {
		t.Error("volumes/networks must not be pruned without --all")
	}
}

func TestRunDryRunMakesNoChanges(t *testing.T) {
	store := setupProductionStore(t, t.TempDir())
	rt := &fakeRuntime{
		stopped: []string{"tengiz-myapp", "tengiz-ghost"},
	}
	c := New(rt, store, "production")

	res, err := c.Run(context.Background(), true, false)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(rt.removed) != 0 {
		t.Errorf("dry-run removed containers: %v", rt.removed)
	}
	if rt.prunedImage || rt.prunedCache || rt.prunedVols || rt.prunedNets {
		t.Error("dry-run must not prune anything")
	}
	if !res.DryRun {
		t.Error("res.DryRun = false, want true")
	}
	if len(res.StaleContainers) != 1 || res.StaleContainers[0] != "tengiz-ghost" {
		t.Errorf("StaleContainers = %v, want [tengiz-ghost]", res.StaleContainers)
	}
}

func TestRunAllPrunesVolumesAndNetworks(t *testing.T) {
	store := setupProductionStore(t, t.TempDir())
	rt := &fakeRuntime{stopped: []string{}}
	c := New(rt, store, "production")

	res, err := c.Run(context.Background(), false, true)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !rt.prunedVols || !rt.prunedNets {
		t.Error("expected volume + network prunes with --all")
	}
	if !res.VolumesPruned || !res.NetworksPruned {
		t.Errorf("result flags wrong: %+v", res)
	}
}

func TestRunEmptyStoppedList(t *testing.T) {
	store := setupProductionStore(t, t.TempDir())
	rt := &fakeRuntime{stopped: []string{}}
	c := New(rt, store, "production")

	res, err := c.Run(context.Background(), false, false)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(res.StaleContainers) != 0 {
		t.Errorf("StaleContainers = %v, want empty", res.StaleContainers)
	}
	if len(rt.removed) != 0 {
		t.Errorf("removed = %v, want empty", rt.removed)
	}
}

func TestRunStagingUsesEnvContainerNames(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStoreWithEnv(dir, "staging")
	if err := store.SaveApp(types.AppEntry{
		Name:        "myapp",
		Port:        9001,
		Environment: "staging",
		Config: types.AppConfig{
			Name:        "myapp",
			Environment: "staging",
		},
	}); err != nil {
		t.Fatal(err)
	}

	rt := &fakeRuntime{
		stopped: []string{"tengiz-myapp-staging", "tengiz-myapp-staging-123", "tengiz-myapp", "tengiz-deadbeef"},
	}
	c := New(rt, store, "staging")

	if _, err := c.Run(context.Background(), false, false); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	sort.Strings(rt.removed)
	// The staging base container + its active version are protected. The env label
	// filter runs at the Docker layer (Task 2), but the Cleaner defensively treats
	// any non-registered container name as stale.
	want := []string{"tengiz-deadbeef", "tengiz-myapp"}
	if len(rt.removed) != len(want) {
		t.Fatalf("removed = %v, want %v", rt.removed, want)
	}
	for i := range want {
		if rt.removed[i] != want[i] {
			t.Fatalf("removed[%d] = %q, want %q", i, rt.removed[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: FAIL — package `github.com/yaso09/tengiz/internal/cleanup` is undefined / `undefined: New` / `undefined: ContainerOps`

- [ ] **Step 3: Write the minimal implementation**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

// ContainerOps is the subset of runtime.Manager the Cleaner needs.
// Keeping it narrow lets tests inject a tiny fake instead of a full
// runtime.Manager mock.
type ContainerOps interface {
	ListStoppedContainers(ctx context.Context, env string) ([]string, error)
	Remove(ctx context.Context, name string) error
	PruneDanglingImages(ctx context.Context) error
	PruneBuildCache(ctx context.Context) error
	PruneUnusedVolumes(ctx context.Context) error
	PruneUnusedNetworks(ctx context.Context) error
}

// Result describes what cleanup found or did.
type Result struct {
	DryRun           bool
	StaleContainers  []string
	ImagesPruned     bool
	BuildCachePruned bool
	VolumesPruned    bool
	NetworksPruned   bool
}

// Cleaner reclaims disk space from stale Tengiz Docker resources while never
// touching containers that are registered in the env-scoped store.
type Cleaner struct {
	rt    ContainerOps
	store *config.Store
	env   string
}

func New(rt ContainerOps, store *config.Store, env string) *Cleaner {
	if env == "" {
		env = "production"
	}
	return &Cleaner{rt: rt, store: store, env: env}
}

// protectedContainers returns the set of container names that are currently
// registered for the Cleaner's environment and must never be removed: each
// app's base container, its active versioned container (if any), and every
// active preview.
func (c *Cleaner) protectedContainers() (map[string]bool, error) {
	protected := make(map[string]bool)

	apps, err := c.store.ListApps()
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	for _, app := range apps {
		base := runtime.ContainerName(app.Name, app.Config.Environment)
		protected[base] = true
		if app.DeploymentSuffix != "" {
			protected[base+"-"+app.DeploymentSuffix] = true
		}
	}

	previews, err := c.store.ListAllPreviews()
	if err != nil {
		return nil, fmt.Errorf("list previews: %w", err)
	}
	for _, pv := range previews {
		protected[fmt.Sprintf("tengiz-%s-pr-%d", pv.AppName, pv.PRNumber)] = true
	}

	return protected, nil
}

// Run removes stale stopped containers and prunes unused Docker resources.
// With dryRun=true nothing is mutated — the report shows what would happen.
// With all=true unused volumes and networks are pruned too.
func (c *Cleaner) Run(ctx context.Context, dryRun, all bool) (*Result, error) {
	stopped, err := c.rt.ListStoppedContainers(ctx, c.env)
	if err != nil {
		return nil, err
	}

	protected, err := c.protectedContainers()
	if err != nil {
		return nil, err
	}

	result := &Result{DryRun: dryRun}
	for _, name := range stopped {
		if protected[name] {
			continue
		}
		result.StaleContainers = append(result.StaleContainers, name)
		if !dryRun {
			if err := c.rt.Remove(ctx, name); err != nil {
				log.Printf("[cleanup] failed to remove stale container %s: %v", name, err)
			}
		}
	}
	sort.Strings(result.StaleContainers)

	if dryRun {
		return result, nil
	}

	if err := c.rt.PruneDanglingImages(ctx); err != nil {
		log.Printf("[cleanup] image prune: %v", err)
	} else {
		result.ImagesPruned = true
	}

	if err := c.rt.PruneBuildCache(ctx); err != nil {
		log.Printf("[cleanup] build cache prune: %v", err)
	} else {
		result.BuildCachePruned = true
	}

	if all {
		if err := c.rt.PruneUnusedVolumes(ctx); err != nil {
			log.Printf("[cleanup] volume prune: %v", err)
		} else {
			result.VolumesPruned = true
		}
		if err := c.rt.PruneUnusedNetworks(ctx); err != nil {
			log.Printf("[cleanup] network prune: %v", err)
		} else {
			result.NetworksPruned = true
		}
	}

	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup.Cleaner to reclaim disk from stale containers and prunes"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — import `cleanup`, add `cleanupCmd`, register `rootCmd.AddCommand(cleanupCmd)` + flags in `init()`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.New(rt, store, env)`, `(*Cleaner).Run(ctx, dryRun, all)`, `cleanup.Result` from Task 3; `runtime.NewDocker()`, `config.NewStoreWithEnv` from existing code
- Produces: `tengiz cleanup [--dry-run|-n] [--all]` command, wired to the global `--env` flag (default `production`)

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import "testing"

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	if cleanupCmd.Flags().Lookup("dry-run") == nil {
		t.Error("cleanupCmd missing --dry-run flag")
	}
	if cleanupCmd.Flags().Lookup("all") == nil {
		t.Error("cleanupCmd missing --all flag")
	}
}

func TestCleanupFlagDefaults(t *testing.T) {
	dryRun, _ := cleanupCmd.Flags().GetBool("dry-run")
	if dryRun {
		t.Error("--dry-run defaults to true, want false")
	}
	all, _ := cleanupCmd.Flags().GetBool("all")
	if all {
		t.Error("--all defaults to true, want false")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupFlags|TestCleanupFlagDefaults" -v -count=1`
Expected: FAIL — `undefined: cleanupCmd` (or command not found)

- [ ] **Step 3: Add the import + command + registration**

In `internal/cli/root.go`:
1. Add to the import block (after the existing `config` import):
```go
	"github.com/yaso09/tengiz/internal/cleanup"
```
2. Add to `init()` (next to `rootCmd.AddCommand(rmCmd)`):
```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().BoolP("dry-run", "n", false, "show what would be cleaned without changing anything")
	cleanupCmd.Flags().Bool("all", false, "also prune unused volumes and networks")
```
3. Add the command definition (near `psCmd`, after the `rmCmd` block):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Reclaims disk space by removing stale stopped Tengiz containers,
dangling images, and the Docker build cache. Use --all to also prune unused
volumes and networks. Containers registered for the current environment
(apps and preview deployments) are always protected.

Preview with -n/--dry-run before making any changes.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)
		c := cleanup.New(rt, store, env)

		result, err := c.Run(cmd.Context(), dryRun, all)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Printf("[tengiz] cleanup report (%s):\n", env)
		fmt.Printf("  stale containers: %d\n", len(result.StaleContainers))
		for _, name := range result.StaleContainers {
			fmt.Printf("    - %s\n", name)
		}
		fmt.Printf("  dangling images pruned: %v\n", result.ImagesPruned)
		fmt.Printf("  build cache pruned: %v\n", result.BuildCachePruned)
		fmt.Printf("  unused volumes pruned: %v\n", result.VolumesPruned)
		fmt.Printf("  unused networks pruned: %v\n", result.NetworksPruned)
		if result.DryRun {
			fmt.Println("[tengiz] dry-run: no changes were made")
		}
		return nil
	},
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupFlags|TestCleanupFlagDefaults" -v -count=1`
Expected: PASS

- [ ] **Step 5: Build + vet**

Run: `go build ./...`
Expected: Build succeeds

Run: `go vet ./...`
Expected: No findings

- [ ] **Step 6: Manual smoke test (optional, needs Docker)**

Run: `docker ps -aq --filter label=tengiz-app --filter status=exited | head` then `go run . cleanup --dry-run`
Expected: dry-run report, no containers actually removed, exit 0

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command with --dry-run and --all flags"
```

---

### Task 5: Documentation

**Files:**
- Modify: `README.md` — add CLI reference section
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI list
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:**
- Consumes: CLI surface from Task 4
- Produces: discoverable docs consistent with the repo's command layout

- [ ] **Step 1: Add the CLI reference section to `README.md`**

Insert this section directly after the `### tengiz rm <app>` section (which ends at line 228 in the current file) and before `### tengiz rollback <app>`:

```markdown
### `tengiz cleanup [-n] [--all]`

Reclaim disk space used by stale Docker resources on a single-server instance.

| Flag | Description |
|------|-------------|
| `-n`, `--dry-run` | Show what would be cleaned without changing anything |
| `--all` | Also prune unused volumes and networks (default: stale containers, dangling images, build cache) |

Removes stopped Tengiz containers that are no longer registered in the store (interrupted deploys, orphaned previews), then prunes dangling images and the Docker build cache. Registered apps and preview deployments for the current `--env` are always protected. Under the hood it shells out to `docker <object> prune` with label filters (`tengiz-app`/`tengiz-env`), so the Docker CLI must be installed.
```

- [ ] **Step 2: Add `tengiz cleanup` to `AGENTS.md`**

Insert after the `tengiz ps             → list apps from Docker` line in the CLI block:

```markdown
tengiz cleanup        → prune stale stopped containers, dangling images, build cache (--all adds unused volumes/networks, -n/--dry-run previews)
```

- [ ] **Step 3: Mark the feature implemented in `docs/FUTURES_FEATURES.md`**

3a. In the P0 table (line 19), change the status glyph:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

3b. In the `## Docker Housekeeping (Otomatik Temizlik)` section (after its `- **Why add to Tengiz:**` line), add:

```markdown
- **Status:** ✅ Implemented (2026-08-10)
```

3c. In the `### ✅ Implemented Features (Not Pending)` table, append a row (after the **Webhook ile Otomatik Deploy** row):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-10) |
```

- [ ] **Step 4: Verify docs render consistency**

Run: `grep -n "Docker Housekeeping" docs/FUTURES_FEATURES.md`
Expected: the P0 row shows ✅, the feature section has a Status line, and the implemented table has a new row

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

### Task 6: Full verification + self-review

**Files:**
- No production changes — verification pass only

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -v -count=1`
Expected: All tests PASS (proxy TCP-dial tests are slow by design; idle tests are time-sensitive at 50ms granularity — both pre-existing)

- [ ] **Step 2: Run vet + build**

Run: `go vet ./...` then `go build -o /tmp/tengiz-cleanup-check .`
Expected: No findings, binary builds

- [ ] **Step 3: Self-review against the feature spec**

Check against `docs/FUTURES_FEATURES.md` #6:
- `tengiz cleanup` command ✅ (Task 4)
- Label-based pruning of containers/images/volumes/networks ✅ (Tasks 1–3: `tengiz-app`/`tengiz-env` label filters)
- Tengiz-managed containers protected ✅ (Task 3 protected set = registered apps + previews + active deployment suffix)
- Scale-to-zero stopped-but-registered containers survive ✅ (protected set covers base container names, which is where `idle`/cold-start state lives)
- Dry-run guardrail for production safety ✅ (Task 4 `--dry-run`)

- [ ] **Step 4: Placeholder scan**

Search the plan for `TBD`, `TODO`, `implement later`, `fill in details`, `Similar to Task`. None present — every step carries complete code.

- [ ] **Step 5: Type/signature consistency check**

- `ContainerOps.ListStoppedContainers(ctx context.Context, env string) ([]string, error)` — identical in runtime.Manager (Task 2) and cleanup.ContainerOps (Task 3)
- `PruneDanglingImages`, `PruneBuildCache`, `PruneUnusedVolumes`, `PruneUnusedNetworks` — `func(ctx context.Context) error` in both interface definitions
- `cleanup.New(rt ContainerOps, store *config.Store, env string) *Cleaner` and `(*Cleaner).Run(ctx, dryRun, all bool) (*Result, error)` — Task 4 consumes exactly these against Task 3 definitions
- `cleanup.Result` fields used by the CLI printer match Task 3's struct exactly
- `runtime.ContainerName(app.Name, app.Config.Environment)` reuse matches the multi-env naming convention documented in AGENTS.md

- [ ] **Step 6: Final commit (if any leftover diffs beyond prior commits)**

```bash
git status --short
git add -u
git commit -m "chore: finalize docker housekeeping feature" || true
```