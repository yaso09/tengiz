# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes stopped non-Tengiz containers, dangling images, unused volumes, unused networks, and (optionally) the Docker build cache — using label-based filtering so every Tengiz-managed container is protected.

**Architecture:** A new `runtime.Manager` interface method `Cleanup(ctx, CleanupOptions) (CleanupReport, error)` is implemented by `dockerRuntime` via the `docker` CLI (`os/exec`, matching every other runtime call). Discovery uses listing commands with `label!=tengiz-app` / `dangling=true` filters; removal uses `docker rm`/`rmi`/`volume rm`/`network rm`/`builder prune`. Pure helper functions build the argument lists and parse `id|name` output so they are unit-testable without a Docker daemon. A new Cobra command `tengiz cleanup` wires the report to user output with `--dry-run` support.

**Tech Stack:** Go 1.26, `os/exec`, Cobra, the existing `runtime.Manager` interface + `NewStub()` test double. No new external dependencies.

## Global Constraints

- Command: `tengiz cleanup` — label-based filtering protects all Tengiz-managed containers
- Never remove a container carrying the `tengiz-app` label (app containers AND preview containers carry it; see `docker.go:98`, `preview/manager.go:98`)
- Never remove tagged `tengiz-apps/*` images (rollback depends on them) — only dangling (untagged) images are pruned
- Default prune targets when no category flag is given: containers, images, volumes, networks
- Build cache (`docker builder prune -f`) is opt-in via `--cache`
- `--dry-run` lists what would be removed without removing anything; build-cache dry-run is skipped (cannot be enumerated) and reported as such
- Docker CLI subcommands used verbatim: `docker ps -a`, `docker images`, `docker volume ls`, `docker network ls`, `docker rm -f`, `docker rmi`, `docker volume rm`, `docker network rm`, `docker builder prune -f`
- The `dockerRuntime` exec calls cannot run in CI (no daemon) — unit tests cover the pure arg-builders, the line parser, and the `NewStub()` behavior only, following the existing repo test convention
- Adding `Cleanup` to the `Manager` interface is a compile-breaking change: BOTH concrete implementers (`stubManager` in `runtime.go`, `mockRTForDeploy` in `internal/cli/root_test.go`) must be updated in the same task
- No new external dependencies; `go vet ./...` and `go build -o tengiz .` must stay green
- Repo rules: add/update tests with every change, then commit; update `README.md` and `docs/FUTURES_FEATURES.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/housekeeping.go` (create) | Cleanup types (`CleanupCategory`, `CleanupOptions`, `CleanupItem`, `CleanupReport`), pure arg-builders, `parseCleanupLines`, `runDocker` helper, `dockerRuntime.Cleanup` + per-category exec implementations |
| `internal/runtime/housekeeping_test.go` (create) | Unit tests for pure helpers and report counting |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager` implementation |
| `internal/cli/root.go` | Register `cleanupCmd` + flags |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy`; add command/flag tests |
| `README.md` | Document `tengiz cleanup` (feature bullet + CLI Reference section) |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as implemented |

---

### Task 1: Cleanup types + report helpers

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupCategory` (const `CleanupContainers`), `runtime.CleanupOptions{Containers, Images, Volumes, Networks, BuildCache, DryRun bool}`, `runtime.CleanupItem{Type CleanupCategory, ID, Name string}`, `runtime.CleanupReport{Items []CleanupItem, CacheCleaned bool}`, `CleanupReport.Total() int`, `CleanupReport.Count(t CleanupCategory) int`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/housekeeping_test.go`:

```go
package runtime

import "testing"

func TestCleanupReportCountAndTotal(t *testing.T) {
	report := CleanupReport{
		Items: []CleanupItem{
			{Type: CleanupContainers, ID: "abc123", Name: "old-build-cache"},
			{Type: CleanupContainers, ID: "def456", Name: "gitlab-runner"},
			{Type: CleanupImages, ID: "sha256:123", Name: "<none>:<none>"},
			{Type: CleanupVolumes, Name: "tmpdata"},
		},
	}
	if report.Total() != 4 {
		t.Errorf("Total() = %d, want 4", report.Total())
	}
	if got := report.Count(CleanupContainers); got != 2 {
		t.Errorf("Count(containers) = %d, want 2", got)
	}
	if got := report.Count(CleanupImages); got != 1 {
		t.Errorf("Count(images) = %d, want 1", got)
	}
	if got := report.Count(CleanupNetworks); got != 0 {
		t.Errorf("Count(networks) = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestCleanupReportCountAndTotal -v -count=1`
Expected: FAIL with "undefined: CleanupReport" (compile error)

- [ ] **Step 3: Write minimal implementation**

Create `internal/runtime/housekeeping.go`:

```go
package runtime

type CleanupCategory string

const (
	CleanupContainers CleanupCategory = "containers"
	CleanupImages     CleanupCategory = "images"
	CleanupVolumes    CleanupCategory = "volumes"
	CleanupNetworks   CleanupCategory = "networks"
	CleanupBuildCache CleanupCategory = "builder-cache"
)

type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type CleanupItem struct {
	Type CleanupCategory `json:"type"`
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name,omitempty"`
}

type CleanupReport struct {
	Items        []CleanupItem `json:"items"`
	CacheCleaned bool          `json:"cache_cleaned"`
}

func (r CleanupReport) Total() int {
	return len(r.Items)
}

func (r CleanupReport) Count(t CleanupCategory) int {
	n := 0
	for _, it := range r.Items {
		if it.Type == t {
			n++
		}
	}
	return n
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestCleanupReportCountAndTotal -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat(runtime): add cleanup report types"
```

---

### Task 2: Add `Cleanup` to `Manager` interface + container pruning

This is the compile-breaking step. The interface gains `Cleanup`, and BOTH implementers must add the method in this same task: `stubManager` (`internal/runtime/runtime.go`) and `mockRTForDeploy` (`internal/cli/root_test.go`). The `dockerRuntime` implementation lands with its full container-pruning branch.

**Files:**
- Modify: `internal/runtime/housekeeping.go` (add imports, `runDocker`, `containerCleanupListArgs`, `parseCleanupLines`, `cleanupContainers`, `Cleanup`)
- Modify: `internal/runtime/runtime.go:31-49` (interface) and `runtime.go:121` (stub)
- Modify: `internal/cli/root_test.go:100` (mockRTForDeploy)
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupReport`, `CleanupItem`, `CleanupCategory` from Task 1
- Produces: `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)`; helpers `containerCleanupListArgs() []string`, `parseCleanupLines(t CleanupCategory, out string) []CleanupItem`, `runDocker(ctx context.Context, args ...string) (string, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/housekeeping_test.go`:

```go
func TestContainerCleanupListArgs(t *testing.T) {
	got := containerCleanupListArgs()
	want := []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", "label!=tengiz-app",
		"--format", "{{.ID}}|{{.Names}}",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseCleanupLines(t *testing.T) {
	out := "abc123|old-build-cache\ndef456|gitlab-runner\n\n"
	items := parseCleanupLines(CleanupContainers, out)
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	if items[0].ID != "abc123" || items[0].Name != "old-build-cache" {
		t.Errorf("item[0] = %+v", items[0])
	}
	if items[1].ID != "def456" || items[1].Name != "gitlab-runner" {
		t.Errorf("item[1] = %+v", items[1])
	}
}

func TestParseCleanupLinesNameOnly(t *testing.T) {
	items := parseCleanupLines(CleanupVolumes, "tmpdata\n")
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	if items[0].Name != "tmpdata" || items[0].ID != "" {
		t.Errorf("item = %+v", items[0])
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.Total() != 0 {
		t.Errorf("Total() = %d, want 0", report.Total())
	}
	if report.CacheCleaned {
		t.Error("CacheCleaned = true, want false")
	}
}
```

Also append to `internal/cli/root_test.go` after the `Run` method (line 100):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return runtime.CleanupReport{}, nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestContainerCleanupListArgs|TestParseCleanupLines|TestParseCleanupLinesNameOnly|TestStubCleanup' -v -count=1`
Expected: FAIL with "undefined: containerCleanupListArgs" (and the stub test fails to compile because `stubManager` has no `Cleanup`)

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/housekeeping.go`:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func containerCleanupListArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", "label!=tengiz-app",
		"--format", "{{.ID}}|{{.Names}}",
	}
}

func parseCleanupLines(t CleanupCategory, out string) []CleanupItem {
	var items []CleanupItem
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		item := CleanupItem{Type: t}
		if parts := strings.SplitN(line, "|", 2); len(parts) == 2 {
			item.ID = parts[0]
			item.Name = parts[1]
		} else {
			item.Name = line
		}
		items = append(items, item)
	}
	return items
}

func (r *dockerRuntime) runDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) cleanupContainers(ctx context.Context, dryRun bool) ([]CleanupItem, error) {
	out, err := r.runDocker(ctx, containerCleanupListArgs()...)
	if err != nil {
		return nil, err
	}
	items := parseCleanupLines(CleanupContainers, out)
	if dryRun {
		return items, nil
	}
	for _, it := range items {
		id := it.ID
		if id == "" {
			id = it.Name
		}
		if _, err := r.runDocker(ctx, "rm", "-f", id); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	var report CleanupReport
	if opts.Containers {
		items, err := r.cleanupContainers(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.Items = append(report.Items, items...)
	}
	return report, nil
}
```

The `import` block must go at the top of `housekeeping.go` (above the type declarations from Task 1); the functions go below the types. Add `Cleanup` to the `Manager` interface in `internal/runtime/runtime.go`:

```go
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)
}
```

Add the stub implementation in `internal/runtime/runtime.go` after `stubManager.Run`:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ ./internal/cli/ -count=1`
Expected: PASS (both packages compile and all tests pass)

- [ ] **Step 5: Run vet and build**

Run: `go vet ./...`
Run: `go build -o tengiz .`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go internal/runtime/runtime.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface with container pruning"
```

---

### Task 3: Dangling image pruning

**Files:**
- Modify: `internal/runtime/housekeeping.go` (add `imageCleanupListArgs`, `cleanupImages`, extend `Cleanup`)
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `runDocker`, `parseCleanupLines` from Task 2
- Produces: `imageCleanupListArgs() []string`, `dockerRuntime.cleanupImages(ctx, dryRun bool) ([]CleanupItem, error)`; `Cleanup` now also handles `opts.Images`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/housekeeping_test.go`:

```go
func TestImageCleanupListArgs(t *testing.T) {
	got := imageCleanupListArgs()
	want := []string{
		"images",
		"--filter", "dangling=true",
		"--format", "{{.ID}}|{{.Repository}}:{{.Tag}}",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestImageCleanupListArgs -v -count=1`
Expected: FAIL with "undefined: imageCleanupListArgs"

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/housekeeping.go`:

```go
func imageCleanupListArgs() []string {
	return []string{
		"images",
		"--filter", "dangling=true",
		"--format", "{{.ID}}|{{.Repository}}:{{.Tag}}",
	}
}

func (r *dockerRuntime) cleanupImages(ctx context.Context, dryRun bool) ([]CleanupItem, error) {
	out, err := r.runDocker(ctx, imageCleanupListArgs()...)
	if err != nil {
		return nil, err
	}
	items := parseCleanupLines(CleanupImages, out)
	if dryRun || len(items) == 0 {
		return items, nil
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	args := append([]string{"rmi"}, ids...)
	if _, err := r.runDocker(ctx, args...); err != nil {
		return nil, err
	}
	return items, nil
}
```

Extend `dockerRuntime.Cleanup` in `internal/runtime/housekeeping.go` to add the images branch after the containers branch:

```go
	if opts.Containers {
		items, err := r.cleanupContainers(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.Items = append(report.Items, items...)
	}
	if opts.Images {
		items, err := r.cleanupImages(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.Items = append(report.Items, items...)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat(runtime): prune dangling images in cleanup"
```

---

### Task 4: Volume and network pruning

**Files:**
- Modify: `internal/runtime/housekeeping.go` (add `volumeCleanupListArgs`, `networkCleanupListArgs`, `cleanupVolumes`, `cleanupNetworks`, extend `Cleanup`)
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `runDocker`, `parseCleanupLines` from Task 2
- Produces: `volumeCleanupListArgs() []string`, `networkCleanupListArgs() []string`, `dockerRuntime.cleanupVolumes(ctx, dryRun bool) ([]CleanupItem, error)`, `dockerRuntime.cleanupNetworks(ctx, dryRun bool) ([]CleanupItem, error)`; `Cleanup` now also handles `opts.Volumes` and `opts.Networks`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/housekeeping_test.go`:

```go
func TestVolumeCleanupListArgs(t *testing.T) {
	got := volumeCleanupListArgs()
	want := []string{
		"volume", "ls",
		"--filter", "dangling=true",
		"--format", "{{.Name}}",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNetworkCleanupListArgs(t *testing.T) {
	got := networkCleanupListArgs()
	want := []string{
		"network", "ls",
		"--filter", "dangling=true",
		"--format", "{{.ID}}|{{.Name}}",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestVolumeCleanupListArgs|TestNetworkCleanupListArgs' -v -count=1`
Expected: FAIL with "undefined: volumeCleanupListArgs"

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/housekeeping.go`:

```go
func volumeCleanupListArgs() []string {
	return []string{
		"volume", "ls",
		"--filter", "dangling=true",
		"--format", "{{.Name}}",
	}
}

func networkCleanupListArgs() []string {
	return []string{
		"network", "ls",
		"--filter", "dangling=true",
		"--format", "{{.ID}}|{{.Name}}",
	}
}

func (r *dockerRuntime) cleanupVolumes(ctx context.Context, dryRun bool) ([]CleanupItem, error) {
	out, err := r.runDocker(ctx, volumeCleanupListArgs()...)
	if err != nil {
		return nil, err
	}
	items := parseCleanupLines(CleanupVolumes, out)
	if dryRun || len(items) == 0 {
		return items, nil
	}
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it.Name)
	}
	args := append([]string{"volume", "rm"}, names...)
	if _, err := r.runDocker(ctx, args...); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *dockerRuntime) cleanupNetworks(ctx context.Context, dryRun bool) ([]CleanupItem, error) {
	out, err := r.runDocker(ctx, networkCleanupListArgs()...)
	if err != nil {
		return nil, err
	}
	items := parseCleanupLines(CleanupNetworks, out)
	if dryRun || len(items) == 0 {
		return items, nil
	}
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it.Name)
	}
	args := append([]string{"network", "rm"}, names...)
	if _, err := r.runDocker(ctx, args...); err != nil {
		return nil, err
	}
	return items, nil
}
```

Extend `dockerRuntime.Cleanup` in `internal/runtime/housekeeping.go` to add the volumes and networks branches after the images branch:

```go
	if opts.Images {
		items, err := r.cleanupImages(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.Items = append(report.Items, items...)
	}
	if opts.Volumes {
		items, err := r.cleanupVolumes(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.Items = append(report.Items, items...)
	}
	if opts.Networks {
		items, err := r.cleanupNetworks(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.Items = append(report.Items, items...)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat(runtime): prune unused volumes and networks in cleanup"
```

---

### Task 5: Build cache pruning

**Files:**
- Modify: `internal/runtime/housekeeping.go` (add `buildCacheCleanupArgs`, `cleanupBuildCache`, extend `Cleanup`)
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `runDocker` from Task 2
- Produces: `buildCacheCleanupArgs() []string`, `dockerRuntime.cleanupBuildCache(ctx) error`; `Cleanup` now sets `report.CacheCleaned = true` when `opts.BuildCache` is set and not dry-run

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/housekeeping_test.go`:

```go
func TestBuildCacheCleanupArgs(t *testing.T) {
	got := buildCacheCleanupArgs()
	want := []string{"builder", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestBuildCacheCleanupArgs -v -count=1`
Expected: FAIL with "undefined: buildCacheCleanupArgs"

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/housekeeping.go`:

```go
func buildCacheCleanupArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func (r *dockerRuntime) cleanupBuildCache(ctx context.Context) error {
	if _, err := r.runDocker(ctx, buildCacheCleanupArgs()...); err != nil {
		return err
	}
	return nil
}
```

Extend `dockerRuntime.Cleanup` in `internal/runtime/housekeeping.go` to add the build-cache branch after the networks branch:

```go
	if opts.Networks {
		items, err := r.cleanupNetworks(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.Items = append(report.Items, items...)
	}
	if opts.BuildCache && !opts.DryRun {
		if err := r.cleanupBuildCache(ctx); err != nil {
			return report, err
		}
		report.CacheCleaned = true
	}
	return report, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -count=1`
Run: `go vet ./...`
Run: `go build -o tengiz .`
Expected: PASS, no vet errors, build succeeds

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat(runtime): prune docker build cache in cleanup"
```

---

### Task 6: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` (add `cleanupCmd`, register in `init()`, add flags in `init()`)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupReport` (from Tasks 1-5)
- Produces: `tengiz cleanup` command with flags `--containers`, `--images`, `--volumes`, `--networks`, `--cache`, `--dry-run`; `rootCmd.Find([]string{"cleanup"})` resolves

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--help"})
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("cleanup --help failed: %v", err)
	}
	helpText := buf.String()
	for _, flag := range []string{"--containers", "--images", "--volumes", "--networks", "--cache", "--dry-run"} {
		if !strings.Contains(helpText, flag) {
			t.Errorf("help text missing flag %q", flag)
		}
	}
}

func TestCleanupCmdFlagPassthrough(t *testing.T) {
	var called bool
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if !containers {
			t.Error("containers = false, want true")
		}
		if !images {
			t.Error("images = false, want true")
		}
		if volumes {
			t.Error("volumes = true, want false")
		}
		if !networks {
			t.Error("networks = false, want true")
		}
		if !cache {
			t.Error("cache = false, want true")
		}
		if !dryRun {
			t.Error("dry-run = false, want true")
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--containers", "--images", "--networks", "--cache", "--dry-run"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanupCommandRegistered|TestCleanupCmdFlagParsing|TestCleanupCmdFlagPassthrough' -v -count=1`
Expected: FAIL (command not found / compile error "undefined: cleanupCmd")

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/root.go`, add the command definition after `rollbackCmd` (after line 1016). In `init()`, add `rootCmd.AddCommand(cleanupCmd)` after `rootCmd.AddCommand(rollbackCmd)` (line 65), and add the flag definitions after the existing `webhookCmd` flags (line 88).

Command definition:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources",
	Long: `Remove Docker resources that are not managed by Tengiz.

Tengiz-managed containers (labeled tengiz-app) and tagged images (tengiz-apps/*)
are always protected.

Categories (none given = all four enabled):
  --containers   remove stopped containers without the tengiz-app label
  --images       remove dangling (untagged) images
  --volumes      remove unused volumes
  --networks     remove unused networks
  --cache        remove the Docker build cache

Use --dry-run to preview what would be removed without removing anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if !containers && !images && !volumes && !networks && !cache {
			containers, images, volumes, networks = true, true, true, true
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: cache,
			DryRun:     dryRun,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "removed"
		if dryRun {
			verb = "would remove"
		}

		reportCat := func(t runtime.CleanupCategory, label string) {
			if n := report.Count(t); n > 0 {
				fmt.Printf("[tengiz] %s %d %s\n", verb, n, label)
				for _, it := range report.Items {
					if it.Type == t {
						fmt.Printf("  %s %s\n", it.ID, it.Name)
					}
				}
			}
		}

		reportCat(runtime.CleanupContainers, "stopped non-Tengiz containers")
		reportCat(runtime.CleanupImages, "dangling images")
		reportCat(runtime.CleanupVolumes, "unused volumes")
		reportCat(runtime.CleanupNetworks, "unused networks")

		if cache {
			if dryRun {
				fmt.Println("[tengiz] build cache: skipped in dry-run (cannot enumerate)")
			} else if report.CacheCleaned {
				fmt.Println("[tengiz] removed Docker build cache")
			}
		}

		if report.Total() == 0 {
			fmt.Printf("[tengiz] nothing to %s\n", verb)
		}
		return nil
	},
}
```

In `init()` (inside the existing function body, after `rootCmd.AddCommand(rollbackCmd)`):

```go
	rootCmd.AddCommand(cleanupCmd)
```

In `init()` (inside the existing function body, after the `webhookCmd` flag block, before the closing brace):

```go
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling (untagged) images")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("cache", false, "remove the Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -count=1`
Expected: PASS

- [ ] **Step 5: Run full test suite, vet, and build**

Run: `go test ./... -count=1`
Run: `go vet ./...`
Run: `go build -o tengiz .`
Expected: all pass

- [ ] **Step 6: Manual smoke test (requires Docker)**

Run: `./tengiz cleanup --dry-run`
Expected: prints "nothing to remove" or lists would-be removals, without deleting anything
Run: `./tengiz cleanup`
Expected: prints per-category removal summaries

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 7: Documentation

**Files:**
- Modify: `README.md` (feature bullet at line 20 area; new CLI section between `tengiz rm` and `tengiz rollback`)
- Modify: `docs/FUTURES_FEATURES.md` (P0 row 6, Implemented table, feature section status)

**Interfaces:**
- Consumes: the `tengiz cleanup` command from Task 6

- [ ] **Step 1: Add a feature bullet to `README.md`**

After the "Deployment history" bullet (line 20), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes stopped non-Tengiz containers, dangling images, unused volumes/networks, and build cache, while never touching Tengiz-managed resources.
```

- [ ] **Step 2: Add the CLI reference section to `README.md`**

Insert a new section between the `tengiz rm` section and `### tengiz rollback` (line 230):

```markdown
### `tengiz cleanup`

Prune Docker resources that are not managed by Tengiz.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers without the `tengiz-app` label |
| `--images` | Remove dangling (untagged) images |
| `--volumes` | Remove unused volumes |
| `--networks` | Remove unused networks |
| `--cache` | Remove the Docker build cache (`docker builder prune`) |
| `--dry-run` | Preview what would be removed without removing anything |

If no category flags are given, all four resource categories are pruned. Tengiz-managed containers (labeled `tengiz-app`) and tagged images (`tengiz-apps/*`) are always protected — rollback images and scale-to-zero containers are never touched.

```
tengiz cleanup            # prune all four categories
tengiz cleanup --dry-run  # preview first
tengiz cleanup --cache    # only remove the build cache
```
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

1. In the P0 table, change row 6 status from `⬜` to `✅` (line 19):
   `| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based \`docker system prune\`. \`tengiz cleanup\`. |`

2. Add a row to the "✅ Implemented Features (Not Pending)" table:
   `| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-07) |`

3. In the "Docker Housekeeping (Otomatik Temizlik)" feature section, add a status line:
   `- **Status:** ✅ Implemented (2026-08-07)`

- [ ] **Step 4: Update `AGENTS.md`**

In the `runtime.Manager` row of the architecture table, append `Cleanup` to the interface description:

```markdown
| `runtime.Manager` | Interface for container lifecycle. `NewDocker()` = exec-based impl, `NewStub()` = test mock. Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup, `Cleanup` for housekeeping. `ContainerName(name, env)` helper. |
```

- [ ] **Step 5: Run full verification**

Run: `go test ./... -count=1`
Run: `go vet ./...`
Run: `go build -o tengiz .`
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage.** The spec (FUTURES_FEATURES.md #6) requires: `tengiz cleanup` command (Task 6), label-based filtering that protects Tengiz-managed containers (Task 2 `label!=tengiz-app`), and cleaning of unused containers/images/volumes/networks (Tasks 2-4), plus build cache as a bonus (Task 5). Periodic/job scheduling is out of scope — the spec's "why" centers on the manual `tengiz cleanup` command, and P0 table lists it as Düşük (Low) effort. No spec requirement is left without a task.

**2. Placeholder scan.** Every step contains concrete code, exact file paths, and exact commands with expected output. No "TBD", "handle edge cases", or "similar to Task N" references exist — each category implementation is written out in full even where it resembles a sibling.

**3. Type consistency.** `CleanupOptions` field names (`Containers/Images/Volumes/Networks/BuildCache/DryRun`) are identical across Tasks 1, 6. `CleanupReport.Count(t CleanupCategory)` and `.Total()` are used in the CLI (Task 6) exactly as defined in Task 1. `cleanupContainers`, `cleanupImages`, `cleanupVolumes`, `cleanupNetworks`, `cleanupBuildCache` are spelled identically in every task that references them. `parseCleanupLines` signature `(t CleanupCategory, out string) []CleanupItem` is consistent across Tasks 2-4. `mockRTForDeploy.Cleanup` signature matches the interface added in Task 2.
