# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command and an optional periodic housekeeper that prune unused Docker containers, images, networks, volumes, and build cache — with label-based filtering so Tengiz-managed containers are never touched.

**Architecture:** Extend the `runtime.Manager` interface with a single `Prune(ctx, CleanupOptions) (CleanupReport, error)` method. The `dockerRuntime` implements it by shelling out to granular `docker container/image/network/volume/builder prune` commands with a `label!=tengiz-app` filter that protects all Tengiz-managed containers. A new `housekeeping` package wraps the scheduler (mirroring the existing `idle`/`health` packages) so cleanup can run on an interval. The `tengiz cleanup` CLI command performs a one-shot prune; a `--cleanup-interval` flag on `tengiz proxy` enables periodic cleaning.

**Tech Stack:** Go 1.26, Cobra CLI, `os/exec` (docker CLI), existing `runtime.Manager`, `config.Store`, no new external deps.

## Global Constraints

- Every docker prune command that touches Tengiz-managed resources MUST use the filter `label!=tengiz-app` (protects Tengiz app/versioned/preview containers and future labeled volumes).
- `runtime.Manager` gains exactly one new method: `Prune(ctx context.Context, opts CleanupOptions) (CleanupReport, error)`.
- All existing `Manager` implementations must be updated in the same task as the interface change (stub + 3 test mocks) or the build breaks.
- `CleanupOptions` fields: `All bool` (prune all unused images, not just dangling; also build cache), `IncludeVolumes bool`.
- `tengiz cleanup` always prunes containers, dangling images, and unused networks; `--all` additionally prunes all unused images + build cache; `--volumes` additionally prunes unused volumes.
- No new external dependencies. Default `tengiz proxy` behavior unchanged (cleanup disabled unless `--cleanup-interval` is set).
- All tests pass with `go test ./... -v -count=1`; `go vet ./...` clean.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupReport` types; add `Prune` to `Manager` interface + stub method |
| `internal/runtime/prune.go` (new) | Pure arg-builders `pruneContainerArgs`/`pruneImageArgs`/`pruneNetworkArgs`/`pruneVolumeArgs`/`pruneBuilderArgs`; `parsePruneOutput`; `dockerRuntime.Prune` exec implementation |
| `internal/runtime/prune_test.go` (new) | Table tests for arg-builders, output parser, stub `Prune`; compile-time interface satisfaction assertion for `dockerRuntime` |
| `internal/cli/root.go` | Register `cleanupCmd`; add `--all`/`--volumes` flags; add `--cleanup-interval` flag on `proxyCmd`; wire housekeeping into proxy |
| `internal/cli/cleanup_test.go` (new) | Registration, flag presence, RunE flag-parsing, proxy interval flag tests |
| `internal/housekeeping/housekeeping.go` (new) | `Manager` that runs `rt.Prune` on an interval; `Start()/Stop()/StopAll()` |
| `internal/housekeeping/housekeeping_test.go` (new) | Interval scheduling tests with a recording mock runtime |
| `internal/proxy/proxy_test.go` | Add `Prune` to its `mockRuntime` (required by interface) |
| `internal/idle/idle_test.go` | Add `Prune` to its `mockRuntime` (required by interface) |
| `README.md` | Document `tengiz cleanup` and proxy `--cleanup-interval` |
| `docs/FUTURES_FEATURES.md` | Mark #6 `Docker Housekeeping` as ✅ Implemented |

No state-file changes in `~/.tengiz/` — cleanup is a Docker daemon operation.

---

### Task 1: Add `Prune` to the runtime Manager interface

**Files:**
- Modify: `internal/runtime/runtime.go` — add types, interface method, stub
- Test: `internal/runtime/prune_test.go` — stub `Prune` test
- Modify: `internal/proxy/proxy_test.go:34` — add stub `Prune` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:33` — add stub `Prune` to `mockRuntime`
- Modify: `internal/cli/root_test.go:99` — add stub `Prune` to `mockRTForDeploy`

**Interfaces:**
- Consumes: nothing new from other tasks.
- Produces: `runtime.CleanupOptions{ All bool; IncludeVolumes bool }`, `runtime.CleanupReport{ ContainersRemoved, ImagesRemoved, NetworksRemoved, VolumesRemoved, BuildCacheRemoved int; SpaceReclaimed string }`, `runtime.Manager.Prune(ctx context.Context, opts CleanupOptions) (CleanupReport, error)`.

- [ ] **Step 1: Write the failing stub test**

```go
// internal/runtime/prune_test.go
package runtime

import (
	"context"
	"testing"
)

func TestStubPruneReturnsEmptyReport(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), CleanupOptions{
		All:            true,
		IncludeVolumes: true,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0", report.ContainersRemoved)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPruneReturnsEmptyReport -v -count=1`
Expected: FAIL to compile with `undefined: CleanupOptions` / `stubManager.Prune`.

- [ ] **Step 3: Add types, interface method, and stub**

Add to `internal/runtime/runtime.go` (place types near the top, after `RunOptions`):

```go
type CleanupOptions struct {
	All            bool // prune all unused images (not just dangling) + build cache
	IncludeVolumes bool
}

type CleanupReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	BuildCacheRemoved int
	SpaceReclaimed    string
}
```

In the `Manager` interface, after the `KeepLastNImages` line:

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts CleanupOptions) (CleanupReport, error)
```

Add the stub method after the stub `KeepLastNImages` (around `internal/runtime/runtime.go:119`):

```go
func (m *stubManager) Prune(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
}
```

- [ ] **Step 4: Add `Prune` to the three existing test mocks so compilation holds**

In `internal/proxy/proxy_test.go`, after the `KeepLastNImages` line:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) { return runtime.CleanupReport{}, nil }
```

In `internal/idle/idle_test.go`, after the `KeepLastNImages` line:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) { return runtime.CleanupReport{}, nil }
```

In `internal/cli/root_test.go`, after the `KeepLastNImages` line:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) { return runtime.CleanupReport{}, nil }
```

- [ ] **Step 5: Run tests to verify it passes**

Run: `go test ./internal/runtime/ ./internal/proxy/ ./internal/idle/ ./internal/cli/ -count=1`
Expected: PASS (stub returns empty report; existing suites still green).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Prune method to Manager interface"
```

---

### Task 2: Implement `dockerRuntime.Prune` with arg builders + output parsing

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go` — add arg-builder + parser tests
- Modify: `internal/runtime/runtime_test.go` — compile-time assertion that `dockerRuntime` satisfies `Manager`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport` from Task 1.
- Produces: unexported `pruneArgs(opts CleanupOptions) map[string][]string` (docker argv per category), `parsePruneOutput(out string) (int, string)` returning `(removed, reclaimedSpace)`, and `func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupReport, error)`.

- [ ] **Step 1: Write the failing tests for arg builders, parser, and interface satisfaction**

```go
// internal/runtime/prune_test.go (append to the file from Task 1)
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestPruneContainerArgs(t *testing.T) {
	got := pruneContainerArgs().Exec
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneContainerArgs() = %v, want %v", got, want)
	}
}

func TestPruneImageArgsDangling(t *testing.T) {
	got := pruneImageArgs(false).Exec
	want := []string{"image", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneImageArgs(false) = %v, want %v", got, want)
	}
}

func TestPruneImageArgsAll(t *testing.T) {
	got := pruneImageArgs(true).Exec
	want := []string{"image", "prune", "-a", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneImageArgs(true) = %v, want %v", got, want)
	}
}

func TestPruneVolumeArgs(t *testing.T) {
	got := pruneVolumeArgs().Exec
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneVolumeArgs() = %v, want %v", got, want)
	}
}

func TestParsePruneOutput(t *testing.T) {
	out := `Total reclaimed space: 1.2GB

abc123
def456`
	removed, reclaimed := parsePruneOutput(out)
	if removed != 2 {
		t.Errorf("parsePruneOutput() removed = %d, want 2", removed)
	}
	if reclaimed != "1.2GB" {
		t.Errorf("parsePruneOutput() reclaimed = %q, want %q", reclaimed, "1.2GB")
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	removed, reclaimed := parsePruneOutput("Total reclaimed space: 0B")
	if removed != 0 {
		t.Errorf("parsePruneOutput() removed = %d, want 0", removed)
	}
	if reclaimed == "" {
		t.Error("parsePruneOutput() reclaimed should be non-empty")
	}
}

func TestDockerRuntimeImplementsManager(t *testing.T) {
	var _ Manager = &dockerRuntime{}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestPrune|TestParsePruneOutput|TestDockerRuntimeImplementsManager" -v -count=1`
Expected: FAIL to compile with `undefined: pruneContainerArgs` and `dockerRuntime does not implement Manager`.

- [ ] **Step 3: Implement**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type pruneCmd struct {
	Exec []string
}

func pruneContainerArgs() pruneCmd {
	return pruneCmd{Exec: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}}
}

func pruneImageArgs(all bool) pruneCmd {
	args := []string{"image", "prune"}
	if all {
		args = append(args, "-a")
	}
	args = append(args, "-f", "--filter", "label!=tengiz-app")
	return pruneCmd{Exec: args}
}

func pruneNetworkArgs() pruneCmd {
	return pruneCmd{Exec: []string{"network", "prune", "-f"}}
}

func pruneVolumeArgs() pruneCmd {
	return pruneCmd{Exec: []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}}
}

func pruneBuilderArgs() pruneCmd {
	return pruneCmd{Exec: []string{"builder", "prune", "-f"}}
}

// parsePruneOutput extracts the count of removed items (non-empty, non-header
// lines) and the "Total reclaimed space: X" value from a docker prune stdout.
func parsePruneOutput(out string) (int, string) {
	removed := 0
	reclaimed := ""
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "Total reclaimed space:") {
			reclaimed = strings.TrimSpace(strings.TrimPrefix(t, "Total reclaimed space:"))
			continue
		}
		removed++
	}
	return removed, reclaimed
}

func execPrune(ctx context.Context, cmd pruneCmd) (int, string, error) {
	c := exec.CommandContext(ctx, "docker", cmd.Exec...)
	out, err := c.CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("docker %s: %w\n%s", strings.Join(cmd.Exec, " "), err, string(out))
	}
	removed, reclaimed := parsePruneOutput(string(out))
	return removed, reclaimed, nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	var report CleanupReport

	removed, reclaimed, err := execPrune(ctx, pruneContainerArgs())
	if err != nil {
		return report, fmt.Errorf("container prune: %w", err)
	}
	report.ContainersRemoved = removed
	report.SpaceReclaimed = reclaimed

	removed, reclaimed, err = execPrune(ctx, pruneNetworkArgs())
	if err != nil {
		return report, fmt.Errorf("network prune: %w", err)
	}
	report.NetworksRemoved = removed
	if reclaimed != "" && report.SpaceReclaimed == "" {
		report.SpaceReclaimed = reclaimed
	}

	removed, reclaimed, err = execPrune(ctx, pruneImageArgs(opts.All))
	if err != nil {
		return report, fmt.Errorf("image prune: %w", err)
	}
	report.ImagesRemoved = removed
	if reclaimed != "" {
		report.SpaceReclaimed = reclaimed
	}

	if opts.All {
		removed, reclaimed, err := execPrune(ctx, pruneBuilderArgs())
		if err != nil {
			return report, fmt.Errorf("builder prune: %w", err)
		}
		report.BuildCacheRemoved = removed
		if reclaimed != "" {
			report.SpaceReclaimed = reclaimed
		}
	}

	if opts.IncludeVolumes {
		removed, reclaimed, err := execPrune(ctx, pruneVolumeArgs())
		if err != nil {
			return report, fmt.Errorf("volume prune: %w", err)
		}
		report.VolumesRemoved = removed
		if reclaimed != "" {
			report.SpaceReclaimed = reclaimed
		}
	}

	return report, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestPrune|TestParsePruneOutput|TestDockerRuntimeImplementsManager" -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): implement label-based docker prune"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — define `cleanupCmd`, register in `init()`, declare flags
- Test: `internal/cli/cleanup_test.go` (new)

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.Manager.Prune` from Tasks 1–2.
- Produces: `cleanupCmd` (root command `tengiz cleanup`), flags `--all`, `--volumes`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil || cmd == nil || cmd.Name() != "cleanup" {
		t.Fatalf("cleanup command not registered: err=%v cmd=%v", err, cmd)
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"all", "volumes"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdRunEHelper(t *testing.T) {
	out := buildCleanupOutput(runtime.CleanupReport{
		ContainersRemoved: 2,
		ImagesRemoved:     3,
		NetworksRemoved:   1,
		SpaceReclaimed:    "1.2GB",
	})
	for _, want := range []string{"2 containers", "3 images", "1 networks", "1.2GB"} {
		if !strings.Contains(out, want) {
			t.Errorf("buildCleanupOutput() missing %q in: %q", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanupCmd" -v -count=1`
Expected: FAIL to compile with `undefined: cleanupCmd`, `undefined: buildCleanupOutput`.

- [ ] **Step 3: Implement the command and a testable reporter**

Add to `internal/cli/root.go` (place after the `rmCmd` definition, before `logsCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, networks, volumes)",
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		report, err := rt.Prune(cmd.Context(), runtime.CleanupOptions{
			All:            all,
			IncludeVolumes: volumes,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		fmt.Print(buildCleanupOutput(report))
		return nil
	},
}

// buildCleanupOutput formats a CleanupReport for the cleanup command.
func buildCleanupOutput(r runtime.CleanupReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[tengiz] cleanup complete: %d containers, %d images, %d networks removed\n",
		r.ContainersRemoved, r.ImagesRemoved, r.NetworksRemoved)
	if r.BuildCacheRemoved > 0 {
		fmt.Fprintf(&b, "[tengiz]   build cache entries removed: %d\n", r.BuildCacheRemoved)
	}
	if r.VolumesRemoved > 0 {
		fmt.Fprintf(&b, "[tengiz]   volumes removed: %d\n", r.VolumesRemoved)
	}
	if r.SpaceReclaimed != "" {
		fmt.Fprintf(&b, "[tengiz]   space reclaimed: %s\n", r.SpaceReclaimed)
	}
	return b.String()
}
```

`strings` is already imported in `root.go`. Register in `init()` next to the other `rootCmd.AddCommand(...)` calls:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("all", false, "prune all unused images (not just dangling) and build cache")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
```

- [ ] **Step 4: Nothing extra needed**

`TestCleanupCmdRunEHelper` already exercises `buildCleanupOutput` with a concrete `runtime.CleanupReport`, so no Docker or fake runtime is required in this task. (The success-path of `cleanupCmd.RunE` is covered indirectly by the arg/exec tests in Task 2; it cannot run headlessly because it shells out to the real `docker` CLI via `runtime.NewDocker()`.)
	```go
import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -run "TestCleanupCmd" -v -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Periodic housekeeping scheduler package

**Files:**
- Create: `internal/housekeeping/housekeeping.go`
- Test: `internal/housekeeping/housekeeping_test.go` (new)

**Interfaces:**
- Consumes: `runtime.Manager` (its `Prune`), `runtime.CleanupOptions`, `runtime.CleanupReport` from Tasks 1–2.
- Produces: `housekeeping.New(rt runtime.Manager, interval time.Duration, opts runtime.CleanupOptions) *Manager`, methods `Start()`, `Stop()`, `StopAll()`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/housekeeping/housekeeping_test.go
package housekeeping

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
)

type recordingRuntime struct {
	prunes atomic.Int32
}

func (r *recordingRuntime) Prune(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	r.prunes.Add(1)
	return runtime.CleanupReport{}, nil
}

func TestStartPrunesOnInterval(t *testing.T) {
	r := &recordingRuntime{}
	s := New(r, 30*time.Millisecond, runtime.CleanupOptions{})
	s.Start()
	defer s.Stop()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if r.prunes.Load() >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected >=2 prunes, got %d", r.prunes.Load())
}

func TestStopHaltsPruning(t *testing.T) {
	r := &recordingRuntime{}
	s := New(r, 20*time.Millisecond, runtime.CleanupOptions{})
	s.Start()
	time.Sleep(60 * time.Millisecond)
	s.Stop()
	idle := r.prunes.Load()
	if idle == 0 {
		t.Fatal("expected at least one prune before stop")
	}
	time.Sleep(60 * time.Millisecond)
	if after := r.prunes.Load(); after != idle {
		t.Fatalf("prunes continued after Stop: got %d want %d", after, idle)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/housekeeping/ -v -count=1`
Expected: FAIL to compile with `undefined: housekeeping / New`.

- [ ] **Step 3: Implement**

Create `internal/housekeeping/housekeeping.go`:

```go
package housekeeping

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Manager struct {
	rt       runtime.Manager
	interval time.Duration
	opts     runtime.CleanupOptions
	mu       sync.Mutex
	cancel   context.CancelFunc
}

func New(rt runtime.Manager, interval time.Duration, opts runtime.CleanupOptions) *Manager {
	return &Manager{rt: rt, interval: interval, opts: opts}
}

func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.run(ctx)
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m *Manager) StopAll() {
	m.Stop()
}

func (m *Manager) run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report, err := m.rt.Prune(context.Background(), m.opts)
			if err != nil {
				log.Printf("[housekeeping] prune failed: %v", err)
				continue
			}
			log.Printf("[housekeeping] pruned %d containers, %d images, %d networks, %d volumes, %d build cache (%s reclaimed)",
				report.ContainersRemoved, report.ImagesRemoved, report.NetworksRemoved,
				report.VolumesRemoved, report.BuildCacheRemoved, report.SpaceReclaimed)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/housekeeping/ -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/housekeeping/
git commit -m "feat(housekeeping): add periodic cleanup scheduler"
```

---

### Task 5: Wire periodic cleaning into `tengiz proxy`

**Files:**
- Modify: `internal/cli/root.go` — add `--cleanup-interval` flag to `proxyCmd`; import housekeeping; start/stop scheduler in proxy RunE
- Test: `internal/cli/cleanup_test.go` — flag presence test

**Interfaces:**
- Consumes: `housekeeping.New`, `runtime.NewDocker`, `runtime.CleanupOptions` from Tasks 1–4.
- Produces: proxy flag `--cleanup-interval <duration>` (default `"0"` = disabled).

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cleanup_test.go (append)
func TestProxyCmdCleanupIntervalFlag(t *testing.T) {
	flag := proxyCmd.Flags().Lookup("cleanup-interval")
	if flag == nil {
		t.Fatal("proxy command missing --cleanup-interval flag")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestProxyCmdCleanupIntervalFlag -v -count=1`
Expected: FAIL with `proxy command missing --cleanup-interval flag`.

- [ ] **Step 3: Implement flag + wiring**

Add to `internal/cli/root.go` imports: `"github.com/yaso09/tengiz/internal/housekeeping"`.

In `init()` after the `runCmd` flag declarations, register the proxy flag:

```go
	proxyCmd.Flags().Duration("cleanup-interval", 0, "run docker housekeeping cleanup at this interval (e.g. 1h); 0 disables")
```

In the `proxyCmd.RunE` body (after `healthChecker := health.NewWithEnv(...)` / before `p.Start(ctx)`), add:

```go
	cleanupInterval, _ := cmd.Flags().GetDuration("cleanup-interval")
	var hk *housekeeping.Manager
	if cleanupInterval > 0 {
		hk = housekeeping.New(rt, cleanupInterval, runtime.CleanupOptions{})
		hk.Start()
		defer hk.Stop()
		fmt.Printf("[tengiz] housekeeping: pruning every %s\n", cleanupInterval)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestProxyCmdCleanupIntervalFlag -v -count=1`
Expected: PASS.

- [ ] **Step 5: Build to confirm compilation**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): wire periodic housekeeping into tengiz proxy"
```

---

### Task 6: Verifying the whole interface compiles and finalizing docs

**Files:**
- Modify: `internal/cli/cleanup_test.go` — confirm `TestCleanupCmdRunEHelper` uses a concrete `runtime.CleanupReport`
- Modify: `README.md` — document `tengiz cleanup` and proxy `--cleanup-interval`
- Modify: `docs/FUTURES_FEATURES.md` — mark #6 implemented
- Test: `internal/cli/cleanup_test.go` already holds tests

**Interfaces:**
- Consumes: everything from Tasks 1–5.

- [ ] **Step 1: Confirm `internal/cli/cleanup_test.go` compiles**

`TestCleanupCmdRunEHelper` (from Task 3) already passes a concrete `runtime.CleanupReport` to `buildCleanupOutput`. Run:

`go test ./internal/cli/ -run TestCleanupCmd -v -count=1`
Expected: PASS.

- [ ] **Step 2: Update `README.md`**

Add a `### tengiz cleanup` section after the `tengiz rm` section and a row for `--cleanup-interval` in the proxy section. Exact text to insert:

```markdown
### `tengiz cleanup`

Prune unused Docker resources (stopped non-Tengiz containers, networks, and images) to reclaim disk.

Tengiz-managed containers (labeled `tengiz-app`) are always protected.

| Flag | Description |
|------|-------------|
| `--all` | Also prune all unused images (not just dangling) and build cache |
| `--volumes` | Also prune unused volumes |

```

Match the existing table style in the file (use the same bar/formatting as the `tengiz proxy` flags table). Update the proxy example/flags list to include:

```
| `--cleanup-interval` | Run `docker housekeeping` every interval (e.g. `1h`); default `0` = disabled |
```

- [ ] **Step 3: Update `docs/FUTURES_FEATURES.md`**

- Change line 19 (`| 6 | **Docker Housekeeping**`) from `⬜` to `✅` and append date to the Status cell: `✅ Implemented (2026-08-05)`.
- The rationale cell can remain; update the trailing rationale to note the `tengiz cleanup` command is now implemented.

- [ ] **Step 4: Run the full suite and vet**

Run: `go test ./... -v -count=1 && go vet ./...`
Expected: all tests PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md internal/cli/cleanup_test.go
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage.** The feature spec (P0 #6) requires: label-based `docker system prune` (`Prune` uses `label!=tengiz-app` on every container/volume/image prune — Task 2), a `tengiz cleanup` command (Task 3), and periodic cleanup (`housekeeping` scheduler + proxy wiring — Tasks 4–5). Docs updated in Task 6. All three spec bullets are mapped to tasks.

**2. Placeholder scan.** Every step contains concrete code and exact commands. No `TBD`/`TODO`.

**3. Type consistency.** `CleanupOptions{All, IncludeVolumes}` and `CleanupReport{ContainersRemoved, ImagesRemoved, NetworksRemoved, VolumesRemoved, BuildCacheRemoved, SpaceReclaimed}` are defined once in Task 1 and reused identically everywhere (`prune.go`, `housekeeping`, CLI tests). The `Prune` signature `Prune(ctx, CleanupOptions) (CleanupReport, error)` is consistent across interface, stub, docker impl, and all three test mocks.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-05-docker-housekeeping.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

 Which approach?