# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command and `internal/housekeeping` package that reclaims disk space by pruning orphaned containers, dangling images, and unused volumes/networks — while protecting all Tengiz-managed resources via label-based filtering.

**Architecture:** A new `internal/housekeeping` package shells out to the `docker` CLI through an injectable `execFunc` (the real one uses `os/exec`; tests inject a fake recorder). It detects candidates with read-only docker queries (`docker ps -a`, `docker images -f dangling=true`, `docker volume ls -f dangling=true`, `docker network ls -f dangling=true`) and only destroys what is provably safe: exited/created containers **without** the `tengiz-app` label, untagged dangling images, and unnamed volumes/unused networks that Docker itself would prune. A new `tengiz cleanup` Cobra command exposes it with `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks` flags (all categories by default).

**Tech Stack:** Go 1.26 standard library only (`os/exec`, `encoding/json`, `strings`, `context`), Cobra (existing), the existing `docker` CLI (same exec-based approach as `internal/runtime/docker.go`). No new external dependencies.

## Global Constraints

- Module path: `github.com/yaso09/tengiz`; all new files go under `internal/housekeeping/`
- New package must import **only standard library** plus the module's own packages — no new deps
- All Docker access goes through `execFunc` (`func(ctx context.Context, args ...string) ([]byte, error)`); never call `exec.CommandContext` outside `housekeeping.exec.go`
- Protection rule (MUST NOT violate): never `docker rm`/`docker container prune` a container whose labels contain `tengiz-app=` — scale-to-zero keeps app containers stopped between requests, so "exited" is normal for managed apps
- Image cleanup is limited to `--filter dangling=true` (untagged) images only — never `docker image prune -a`, so tagged rollback images (`tengiz-apps/<app>:<deploymentID>`) are always kept
- Volume cleanup uses `docker volume prune -f` (unnamed volumes only); named/bind-mount persistent storage is never touched
- Network cleanup uses `docker network prune -f` (Docker skips `bridge`/`host`/`none` automatically)
- No code comments in new files (project rule); no behavior change to existing `runtime.Manager`
- Follow existing patterns from `internal/runtime/docker.go`: `exec.CommandContext(ctx, "docker", args...)`, `CombinedOutput()`, `fmt.Errorf("docker <verb>: %w\n%s", err, string(out))`
- Every commit must build: `go build ./...` and `go vet ./...` must pass, `go test ./... -count=1` must pass

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/housekeeping/housekeeping.go` | `Options`, `Result`, `Manager` struct, `NewManager`, `execFunc` type, label constant, `splitLines`, `defaultOpts` |
| `internal/housekeeping/exec.go` | `RealDocker` — the production `execFunc` wrapping `os/exec` |
| `internal/housekeeping/containers.go` | Orphan container detection + removal (`containerInfo`, `orphanContainers`) |
| `internal/housekeeping/images.go` | Dangling image detection + prune |
| `internal/housekeeping/networks.go` | Unused network detection + prune |
| `internal/housekeeping/volumes.go` | Unused volume detection + prune |
| `internal/housekeeping/run.go` | `Manager.Run` orchestration honoring `Options` + dry-run |
| `internal/housekeeping/containers_test.go` | Tests for orphan container logic (Task 1) |
| `internal/housekeeping/images_test.go` | Tests for dangling image logic (Task 2) |
| `internal/housekeeping/networks_test.go` + `volumes_test.go` | Tests (Task 3) |
| `internal/housekeeping/run_test.go` | Tests for orchestration + dry-run + default options (Task 4) |
| `internal/cli/cleanup.go` | `tengiz cleanup` Cobra command + `mergeCleanupOptions` helper |
| `internal/cli/cleanup_test.go` | CLI registration + options-merge tests (Task 5) |
| `README.md`, `AGENTS.md`, `docs/FUTURES_FEATURES.md` | Documentation updates (Task 6) |

A shared fake runner is defined once in `internal/housekeeping/containers_test.go` (same package) and reused by all later test files — it records every command invocation and returns canned output keyed by the joined command string.

---

### Task 1: Housekeeping types + orphan container cleanup

**Files:**
- Create: `internal/housekeeping/housekeeping.go`
- Create: `internal/housekeeping/exec.go`
- Create: `internal/housekeeping/containers.go`
- Create: `internal/housekeeping/containers_test.go`

**Interfaces:**
- Consumes: nothing new (standard library only)
- Produces: `housekeeping.execFunc` (type `func(ctx context.Context, args ...string) ([]byte, error)`), `housekeeping.RealDocker execFunc`, `housekeeping.NewManager(exec execFunc) *Manager`, `housekeeping.containerInfo` struct, `func (m *Manager) containers(ctx context.Context) ([]containerInfo, error)`, `func (m *Manager) orphanContainers(ctx context.Context) ([]string, error)`, `housekeeping.Options` / `housekeeping.Result` (fields: `ContainersRemoved []string`)

- [ ] **Step 1: Write the failing test**

```go
// internal/housekeeping/containers_test.go
package housekeeping

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func fakeRunner(records *[][]string, outputs map[string]string) execFunc {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		*records = append(*records, append([]string(nil), args...))
		key := strings.Join(args, " ")
		if out, ok := outputs[key]; ok {
			return []byte(out), nil
		}
		return []byte(""), nil
	}
}

func TestOrphanContainersFiltersManagedAndRunning(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{
		"ps -a --format {{json .}}": strings.Join([]string{
			`{"ID":"abc","Names":"/tengiz-myapp","State":"exited","Labels":"tengiz-app=myapp,tengiz-env=production"}`,
			`{"ID":"def","Names":"/stray","State":"exited","Labels":""}`,
			`{"ID":"ghi","Names":"/another","State":"running","Labels":""}`,
			`{"ID":"jkl","Names":"/created-one","State":"created","Labels":""}`,
		}, "\n"),
	})

	m := NewManager(runner)
	got, err := m.orphanContainers(context.Background())
	if err != nil {
		t.Fatalf("orphanContainers() error = %v", err)
	}
	want := []string{"def", "jkl"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("orphanContainers() = %v, want %v", got, want)
	}
}

func TestOrphanContainersPropagatesDockerError(t *testing.T) {
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		return nil, context.DeadlineExceeded
	}
	m := NewManager(runner)
	if _, err := m.orphanContainers(context.Background()); err == nil {
		t.Error("expected error when docker ps fails")
	}
}

func TestNewManagerNonNil(t *testing.T) {
	m := NewManager(RealDocker)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/housekeeping/ -run 'TestOrphan|TestNewManager' -v -count=1`
Expected: FAIL with `cannot find package` — `internal/housekeeping` does not exist yet.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/housekeeping/housekeeping.go
package housekeeping

import "context"

const labelApp = "tengiz-app"

type execFunc func(ctx context.Context, args ...string) ([]byte, error)

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	DryRun     bool
}

type Result struct {
	ContainersRemoved []string
	ImagesRemoved     []string
	VolumesRemoved    []string
	NetworksRemoved   []string
}

type Manager struct {
	exec execFunc
}

func NewManager(exec execFunc) *Manager {
	return &Manager{exec: exec}
}

func splitLines(data []byte) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
```

```go
// internal/housekeeping/exec.go
package housekeeping

import (
	"context"
	"os/exec"
)

var RealDocker execFunc = func(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}
```

```go
// internal/housekeeping/containers.go
package housekeeping

import (
	"context"
	"encoding/json"
	"strings"
)

type containerInfo struct {
	ID     string `json:"ID"`
	Name   string `json:"Name"`
	State  string `json:"State"`
	Labels string `json:"Labels"`
}

func (m *Manager) containers(ctx context.Context) ([]containerInfo, error) {
	data, err := m.exec(ctx, "ps", "-a", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var out []containerInfo
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ci containerInfo
		if json.Unmarshal([]byte(line), &ci) == nil {
			out = append(out, ci)
		}
	}
	return out, nil
}

func (m *Manager) orphanContainers(ctx context.Context) ([]string, error) {
	infos, err := m.containers(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ci := range infos {
		if ci.State != "exited" && ci.State != "created" {
			continue
		}
		if strings.Contains(ci.Labels, labelApp) {
			continue
		}
		names = append(names, ci.ID)
	}
	return names, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/housekeeping/ -run 'TestOrphan|TestNewManager' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
go build ./... && go vet ./...
git add internal/housekeeping/
git commit -m "feat: add housekeeping types and orphan container detection"
```

---

### Task 2: Dangling image cleanup

**Files:**
- Create: `internal/housekeeping/images.go`
- Create: `internal/housekeeping/images_test.go`

**Interfaces:**
- Consumes: `housekeeping.Result`, `housekeeping.Manager.exec`, `splitLines` (from Task 1)
- Produces: `func (m *Manager) danglingImages(ctx context.Context) ([]string, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/housekeeping/images_test.go
package housekeeping

import (
	"context"
	"reflect"
	"testing"
)

func TestDanglingImagesReturnsIDs(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{
		"images -q -f dangling=true": "sha256:111\nsha256:222\n",
	})
	m := NewManager(runner)
	got, err := m.danglingImages(context.Background())
	if err != nil {
		t.Fatalf("danglingImages() error = %v", err)
	}
	want := []string{"sha256:111", "sha256:222"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("danglingImages() = %v, want %v", got, want)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 docker call, got %d", len(records))
	}
}

func TestDanglingImagesEmpty(t *testing.T) {
	m := NewManager(func(ctx context.Context, args ...string) ([]byte, error) {
		return []byte(""), nil
	})
	got, err := m.danglingImages(context.Background())
	if err != nil {
		t.Fatalf("danglingImages() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no dangling images, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/housekeeping/ -run 'TestDanglingImages' -v -count=1`
Expected: FAIL with `undefined: (m *Manager).danglingImages` or `danglingImages not declared by type Manager`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/housekeeping/images.go
package housekeeping

import "context"

func (m *Manager) danglingImages(ctx context.Context) ([]string, error) {
	data, err := m.exec(ctx, "images", "-q", "-f", "dangling=true")
	if err != nil {
		return nil, err
	}
	return splitLines(data), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/housekeeping/ -run 'TestDanglingImages' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
go build ./... && go vet ./...
git add internal/housekeeping/images.go internal/housekeeping/images_test.go
git commit -m "feat: detect dangling docker images"
```

---

### Task 3: Unused volume and network cleanup

**Files:**
- Create: `internal/housekeeping/volumes.go`
- Create: `internal/housekeeping/networks.go`
- Create: `internal/housekeeping/volumes_test.go`
- Create: `internal/housekeeping/networks_test.go`

**Interfaces:**
- Consumes: `housekeeping.Result`, `housekeeping.Manager.exec`, `splitLines`
- Produces: `func (m *Manager) danglingVolumes(ctx context.Context) ([]string, error)`, `func (m *Manager) danglingNetworks(ctx context.Context) ([]string, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/housekeeping/volumes_test.go
package housekeeping

import (
	"context"
	"reflect"
	"testing"
)

func TestDanglingVolumesReturnsNames(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{
		"volume ls -q -f dangling=true": "vol_abc\nvol_def\n",
	})
	m := NewManager(runner)
	got, err := m.danglingVolumes(context.Background())
	if err != nil {
		t.Fatalf("danglingVolumes() error = %v", err)
	}
	if want := []string{"vol_abc", "vol_def"}; !reflect.DeepEqual(got, want) {
		t.Errorf("danglingVolumes() = %v, want %v", got, want)
	}
}
```

```go
// internal/housekeeping/networks_test.go
package housekeeping

import (
	"context"
	"reflect"
	"testing"
)

func TestDanglingNetworksReturnsIDs(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{
		"network ls -q -f dangling=true": "net_aaa\nnet_bbb\n",
	})
	m := NewManager(runner)
	got, err := m.danglingNetworks(context.Background())
	if err != nil {
		t.Fatalf("danglingNetworks() error = %v", err)
	}
	if want := []string{"net_aaa", "net_bbb"}; !reflect.DeepEqual(got, want) {
		t.Errorf("danglingNetworks() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/housekeeping/ -run 'TestDanglingVolumes|TestDanglingNetworks' -v -count=1`
Expected: FAIL with `undefined: (m *Manager).danglingVolumes` and `danglingNetworks`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/housekeeping/volumes.go
package housekeeping

import "context"

func (m *Manager) danglingVolumes(ctx context.Context) ([]string, error) {
	data, err := m.exec(ctx, "volume", "ls", "-q", "-f", "dangling=true")
	if err != nil {
		return nil, err
	}
	return splitLines(data), nil
}
```

```go
// internal/housekeeping/networks.go
package housekeeping

import "context"

func (m *Manager) danglingNetworks(ctx context.Context) ([]string, error) {
	data, err := m.exec(ctx, "network", "ls", "-q", "-f", "dangling=true")
	if err != nil {
		return nil, err
	}
	return splitLines(data), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/housekeeping/ -run 'TestDanglingVolumes|TestDanglingNetworks' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
go build ./... && go vet ./...
git add internal/housekeeping/volumes.go internal/housekeeping/networks.go internal/housekeeping/volumes_test.go internal/housekeeping/networks_test.go
git commit -m "feat: detect unused docker volumes and networks"
```

---

### Task 4: Orchestrated `Manager.Run` with default options and dry-run

**Files:**
- Create: `internal/housekeeping/run.go`
- Create: `internal/housekeeping/run_test.go`

**Interfaces:**
- Consumes: `orphanContainers`, `danglingImages`, `danglingVolumes`, `danglingNetworks` (Tasks 1-3), `Options`, `Result`
- Produces: `func (m *Manager) Run(ctx context.Context, opts Options) (*Result, error)`, `func defaultOpts(opts Options) Options`

Behavior contract:
- If no category flag is set in `opts`, all four categories default to `true`
- For each enabled category: query candidates; in dry-run mode append candidate names to the result **without** running any destructive command; otherwise run the destructive command (`docker rm --force <id>` per orphan container, `docker image prune -f --filter dangling=true`, `docker volume prune -f`, `docker network prune -f`) and append names to the result
- Query errors abort with a wrapped error; a failed individual `docker rm` is ignored (prune continues)

- [ ] **Step 1: Write the failing test**

```go
// internal/housekeeping/run_test.go
package housekeeping

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultOptsExpandsAll(t *testing.T) {
	got := defaultOpts(Options{})
	want := Options{Containers: true, Images: true, Volumes: true, Networks: true}
	if got != want {
		t.Errorf("defaultOpts() = %+v, want %+v", got, want)
	}
}

func TestDefaultOptsKeepsExplicit(t *testing.T) {
	got := defaultOpts(Options{Images: true})
	if !got.Images || got.Containers || got.Volumes || got.Networks {
		t.Errorf("defaultOpts() should keep only Images, got %+v", got)
	}
}

func TestRunPrunesEnabledCategories(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{
		"ps -a --format {{json .}}":          `{"ID":"abc","Names":"/stray","State":"exited","Labels":""}`,
		"images -q -f dangling=true":         "sha256:111\n",
		"volume ls -q -f dangling=true":      "vol_abc\n",
		"network ls -q -f dangling=true":     "net_aaa\n",
	})
	m := NewManager(runner)
	res, err := m.Run(context.Background(), Options{Containers: true, Images: true, Volumes: true, Networks: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(res.ContainersRemoved, []string{"abc"}) {
		t.Errorf("ContainersRemoved = %v", res.ContainersRemoved)
	}
	if !reflect.DeepEqual(res.ImagesRemoved, []string{"sha256:111"}) {
		t.Errorf("ImagesRemoved = %v", res.ImagesRemoved)
	}
	if !reflect.DeepEqual(res.VolumesRemoved, []string{"vol_abc"}) {
		t.Errorf("VolumesRemoved = %v", res.VolumesRemoved)
	}
	if !reflect.DeepEqual(res.NetworksRemoved, []string{"net_aaa"}) {
		t.Errorf("NetworksRemoved = %v", res.NetworksRemoved)
	}

	var hasRM, hasImagePrune, hasVolPrune, hasNetPrune bool
	for _, call := range records {
		if len(call) > 0 {
			switch call[0] {
			case "rm":
				hasRM = true
			case "image":
				hasImagePrune = true
			case "volume":
				hasVolPrune = true
			case "network":
				hasNetPrune = true
			}
		}
	}
	if !hasRM || !hasImagePrune || !hasVolPrune || !hasNetPrune {
		t.Errorf("expected all destructive commands; rm=%v image=%v volume=%v network=%v",
			hasRM, hasImagePrune, hasVolPrune, hasNetPrune)
	}
}

func TestRunDryRunSkipsDestructiveCommands(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{
		"ps -a --format {{json .}}":      `{"ID":"abc","Names":"/stray","State":"exited","Labels":""}`,
		"images -q -f dangling=true":     "sha256:111\n",
		"volume ls -q -f dangling=true":  "vol_abc\n",
		"network ls -q -f dangling=true": "net_aaa\n",
	})
	m := NewManager(runner)
	res, err := m.Run(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(res.ContainersRemoved) != 1 || len(res.ImagesRemoved) != 1 {
		t.Errorf("dry-run should still report candidates: %+v", res)
	}
	for _, call := range records {
		if len(call) > 0 && call[0] == "rm" {
			t.Errorf("dry-run must not run docker rm, got %v", call)
		}
		if len(call) > 1 && call[1] == "prune" {
			t.Errorf("dry-run must not run docker %s prune, got %v", call[0], call)
		}
	}
}

func TestRunReturnsErrorWhenDockerFails(t *testing.T) {
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		return nil, context.DeadlineExceeded
	}
	m := NewManager(runner)
	if _, err := m.Run(context.Background(), Options{Containers: true}); err == nil {
		t.Error("expected error when docker ps fails")
	}
}

func TestRunWithNoCandidatesRunsNoPrune(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{})
	m := NewManager(runner)
	res, err := m.Run(context.Background(), Options{Containers: true, Images: true, Volumes: true, Networks: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(res.ContainersRemoved)+len(res.ImagesRemoved)+len(res.VolumesRemoved)+len(res.NetworksRemoved) != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
	for _, call := range records {
		if strings.Contains(strings.Join(call, " "), "prune") {
			t.Errorf("no prune command expected, got %v", call)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/housekeeping/ -run 'TestDefaultOpts|TestRun' -v -count=1`
Expected: FAIL with `undefined: defaultOpts` and `undefined: (m *Manager).Run`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/housekeeping/run.go
package housekeeping

import (
	"context"
	"fmt"
)

func defaultOpts(opts Options) Options {
	if opts.Containers || opts.Images || opts.Volumes || opts.Networks {
		return opts
	}
	opts.Containers = true
	opts.Images = true
	opts.Volumes = true
	opts.Networks = true
	return opts
}

func (m *Manager) Run(ctx context.Context, opts Options) (*Result, error) {
	opts = defaultOpts(opts)
	res := &Result{}

	if opts.Containers {
		names, err := m.orphanContainers(ctx)
		if err != nil {
			return nil, fmt.Errorf("list containers: %w", err)
		}
		for _, name := range names {
			if !opts.DryRun {
				m.exec(ctx, "rm", "--force", name)
			}
			res.ContainersRemoved = append(res.ContainersRemoved, name)
		}
	}

	if opts.Images {
		ids, err := m.danglingImages(ctx)
		if err != nil {
			return nil, fmt.Errorf("list images: %w", err)
		}
		if len(ids) > 0 {
			if !opts.DryRun {
				m.exec(ctx, "image", "prune", "-f", "--filter", "dangling=true")
			}
			res.ImagesRemoved = ids
		}
	}

	if opts.Volumes {
		names, err := m.danglingVolumes(ctx)
		if err != nil {
			return nil, fmt.Errorf("list volumes: %w", err)
		}
		if len(names) > 0 {
			if !opts.DryRun {
				m.exec(ctx, "volume", "prune", "-f")
			}
			res.VolumesRemoved = names
		}
	}

	if opts.Networks {
		names, err := m.danglingNetworks(ctx)
		if err != nil {
			return nil, fmt.Errorf("list networks: %w", err)
		}
		if len(names) > 0 {
			if !opts.DryRun {
				m.exec(ctx, "network", "prune", "-f")
			}
			res.NetworksRemoved = names
		}
	}

	return res, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/housekeeping/ -count=1 -v`
Expected: PASS (all housekeeping tests)

- [ ] **Step 5: Commit**

```bash
go build ./... && go vet ./...
git add internal/housekeeping/run.go internal/housekeeping/run_test.go
git commit -m "feat: orchestrate docker cleanup with dry-run and defaults"
```

---

### Task 5: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:38` — add `rootCmd.AddCommand(cleanupCmd)` in `init()` (place near `psCmd`, after `rootCmd.AddCommand(psCmd)`)
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `housekeeping.NewManager`, `housekeeping.RealDocker`, `housekeeping.Options`, `housekeeping.Result`, `housekeeping.defaultOpts` (via `mergeCleanupOptions` behavior)
- Produces: `cleanupCmd *cobra.Command`, `func mergeCleanupOptions(containers, images, volumes, networks, dryRun bool) housekeeping.Options`

Command spec:
- `Use: "cleanup"`, `Short: "Remove unused Docker resources (images, volumes, networks, containers)"`
- `Long` explains label protection + dry-run
- Flags: `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks` (all default `false`)
- RunE: build options via `mergeCleanupOptions`, create `housekeeping.NewManager(housekeeping.RealDocker)`, call `Run`, then print a per-category summary with `fmt.Printf`
- Prints `[tengiz] cleanup complete` plus counts; empty categories are summarized as "0 removed"

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/housekeeping"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cleanup, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil || cleanup == nil {
		t.Fatalf("expected cleanup command to be registered, err=%v", err)
	}
}

func TestMergeCleanupOptionsDefaultsAll(t *testing.T) {
	opts := mergeCleanupOptions(false, false, false, false, false)
	want := housekeeping.Options{Containers: true, Images: true, Volumes: true, Networks: true}
	if opts != want {
		t.Errorf("mergeCleanupOptions() = %+v, want %+v", opts, want)
	}
}

func TestMergeCleanupOptionsKeepsExplicit(t *testing.T) {
	opts := mergeCleanupOptions(true, false, false, true, true)
	if !opts.Containers || opts.Images || !opts.Networks || !opts.DryRun {
		t.Errorf("mergeCleanupOptions() = %+v", opts)
	}
	if opts.Volumes {
		t.Error("Volumes should stay false when not selected")
	}
}

func TestCleanupDryRunFlagPresent(t *testing.T) {
	cleanup, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup not found: %v", err)
	}
	if cleanup.Flags().Lookup("dry-run") == nil {
		t.Error("expected --dry-run flag on cleanup command")
	}
	if cleanup.Flags().Lookup("images") == nil {
		t.Error("expected --images flag on cleanup command")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestMergeCleanup' -v -count=1`
Expected: FAIL with `cleanup not found` and `undefined: mergeCleanupOptions`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cli/cleanup.go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/housekeeping"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (images, volumes, networks, containers)",
	Long: `Prune orphaned containers, dangling images, and unused volumes and networks.

Tengiz-managed containers are protected by the "tengiz-app" label and are never
removed, even when stopped (scale-to-zero keeps containers stopped between requests).
Only untagged (dangling) images are removed, so rollback images are always kept.
Use --dry-run to preview what would be removed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts := mergeCleanupOptions(containers, images, volumes, networks, dryRun)

		mgr := housekeeping.NewManager(housekeeping.RealDocker)
		result, err := mgr.Run(cmd.Context(), opts)
		if err != nil {
			return err
		}

		if dryRun {
			fmt.Println("[tengiz] dry-run — nothing was removed:")
		} else {
			fmt.Println("[tengiz] cleanup complete:")
		}
		fmt.Printf("  containers removed: %d\n", len(result.ContainersRemoved))
		fmt.Printf("  images removed:     %d\n", len(result.ImagesRemoved))
		fmt.Printf("  volumes removed:    %d\n", len(result.VolumesRemoved))
		fmt.Printf("  networks removed:   %d\n", len(result.NetworksRemoved))
		return nil
	},
}

func mergeCleanupOptions(containers, images, volumes, networks, dryRun bool) housekeeping.Options {
	opts := housekeeping.Options{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		DryRun:     dryRun,
	}
	if !containers && !images && !volumes && !networks {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
	}
	return opts
}
```

Register the command by adding this line in `init()` in `internal/cli/root.go` right after `rootCmd.AddCommand(psCmd)`:

```go
rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestMergeCleanup' -v -count=1`
Expected: PASS

Then run the full suite to confirm nothing regressed:

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
go build ./... && go vet ./...
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 6: Documentation updates

**Files:**
- Modify: `README.md` — add a `### tengiz cleanup` section after the `### tengiz ps` section
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI command list
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 "Docker Housekeeping" as ✅ implemented (dated today)

**Interfaces:**
- Consumes: the command behavior from Task 5 (flags and output shape)
- Produces: updated user-facing docs describing the new command

- [ ] **Step 1: Add `tengiz cleanup` to README.md**

Insert after the `### tengiz ps` section (around line 146-151 in `README.md`):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space: orphaned containers, dangling images, unused volumes and networks.

Tengiz-managed containers are protected by the `tengiz-app` label and are never removed, even when stopped (scale-to-zero keeps containers stopped between requests). Only untagged (dangling) images are removed, so rollback images are always kept.

```
tengiz cleanup                     # prune all categories
tengiz cleanup --dry-run           # preview what would be removed
tengiz cleanup --images --volumes  # prune only images and volumes
```

Flags: `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`.
```

- [ ] **Step 2: Add the command to AGENTS.md**

In `AGENTS.md`, in the CLI code block under the existing `tengiz ps` line, add:

```
tengiz cleanup [--dry-run] [--containers] [--images] [--volumes] [--networks]  → prune unused Docker resources (label-protected)
```

- [ ] **Step 3: Mark feature #6 implemented in docs/FUTURES_FEATURES.md**

In the P0 table, change the `#6` row's checkmark from `⬜` to `✅` and append the date. The row becomes:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. (2026-08-05) |
```

- [ ] **Step 4: Verify the docs render correctly and commit**

```bash
go build ./...
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

- [ ] **Step 5: Final full verification**

```bash
go test ./... -count=1
go vet ./...
```

Expected: PASS — all tests across all packages pass with no regressions.

---

## Self-Review

**1. Spec coverage:** The spec (#6 Docker Housekeeping) requires: label-based filtering protecting Tengiz containers (Task 1 + Run), dangling image pruning (Task 2), volume/network pruning (Task 3), orchestration with dry-run (Task 4), and a `tengiz cleanup` CLI command (Task 5). The `CleanupHelperContainersJob` from the source rationale is covered by orphan-container removal (unlabeled exited/created containers) in Task 1. Periodic scheduled cleanup is intentionally out of scope — it is a separate P1 feature (#57 Background Monitoring Scheduler / #56 Granular Docker Prune) and would depend on the scheduler infra this plan deliberately avoids introducing.

**2. Placeholder scan:** No TBD/TODO, no "add error handling" prose — every code step includes full, compilable code; every test step includes concrete assertions; every expected output is stated.

**3. Type consistency:** `execFunc` (Task 1) is used unchanged in all later tasks; `NewManager(exec)` matches usage in Tasks 1-5; `Options`/`Result` fields (`ContainersRemoved`, `ImagesRemoved`, `VolumesRemoved`, `NetworksRemoved`) are defined once (Task 1) and referenced consistently in Task 4 output printing; `defaultOpts` signature matches the CLI task's behavior contract (defaults expand only when all four categories are false); `mergeCleanupOptions` returns `housekeeping.Options` matching the `Run` parameter type.
