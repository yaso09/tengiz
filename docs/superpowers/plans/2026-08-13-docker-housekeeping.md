# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused networks, build cache, optionally volumes) using label-based filtering so Tengiz-managed containers are never removed, reclaiming disk on single-server deployments.

**Architecture:** A new `Prune(ctx, opts)` method on the `runtime.Manager` interface, implemented in `internal/runtime/cleanup.go`, lists candidates via `docker ps -aq` / `docker images -q` / `docker network ls -q` / `docker volume ls -q` with label + dangling filters, then removes them via `docker rm` / `docker rmi` / `docker network rm` / `docker volume rm` / `docker builder prune`. A `--dry-run` flag lists candidates without removing. The CLI command `tengiz cleanup` maps cobra flags to `runtime.CleanupOptions`, prompts for confirmation (unless `--force`/`--dry-run`), and prints a summary. Reuses existing `KeepLastNImages` for per-app image retention.

**Tech Stack:** Go 1.26, Cobra (existing CLI), existing `runtime.Manager`/`types` packages, Docker CLI via `os/exec` (no new external dependencies).

## Global Constraints

- No new external dependencies (cobra and stdlib only)
- Tengiz-managed containers (label `tengiz-app`) must NEVER be removed by a global cleanup — only by `--app <name>`
- Volumes are excluded by default (may hold persistent data); enabled only via `--volumes` or `--all`
- All cleanup operations must work with `--dry-run` without modifying Docker state
- `--env` flag is honored via `getEnv(cmd)` and affects nothing at the runtime layer (cleanup is host-wide), but the flag pattern is followed
- Keep last 5 images per app when `--app` is specified (matches deploy-time retention via `KeepLastNImages`)
- Every existing test must continue to pass; `mockRTForDeploy` and `stubManager` must be updated to satisfy the expanded `Manager` interface
- Commit after each task; run `go build ./...`, `go vet ./...`, `go test ./... -count=1` before each commit

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupTarget`, `CleanupOptions`, `CleanupResult` types; add `Prune` to `Manager` interface; stub implementation; `AllCleanupTargets()`/`DefaultCleanupTargets()` |
| `internal/runtime/cleanup.go` | Docker `Prune` implementation + per-category list/remove helpers + `parseIDList`, `containerListArgs`, `parseReclaimedSpace`, `FormatBytes` |
| `internal/runtime/cleanup_test.go` | Unit tests for pure helpers + stub `Prune` |
| `internal/runtime/runtime_test.go` | Tests for `AllCleanupTargets`/`DefaultCleanupTargets` (optional — folded into `cleanup_test.go`) |
| `internal/cli/cleanup.go` | New `tengiz cleanup` command + `cleanupOptionsFromFlags`, `confirmCleanup`, `printCleanupResult` |
| `internal/cli/cleanup_test.go` | CLI command registration, flag, option-mapping, confirmation tests |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` (compile fix for expanded interface) |
| `README.md` | Document `tengiz cleanup` in CLI Reference + Commands table |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

No new packages. New files: `internal/cli/cleanup.go`, `internal/cli/cleanup_test.go`. Everything else modifies existing files.

---

### Task 1: Cleanup types + Manager interface + stub + mock fix

**Files:**
- Modify: `internal/runtime/runtime.go` — add types, interface method, stub
- Test: `internal/runtime/cleanup_test.go` — add stub + target-list tests
- Modify: `internal/cli/root_test.go` — add `Prune` to `mockRTForDeploy`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type CleanupTarget string` with constants `CleanupContainers = "containers"`, `CleanupImages = "images"`, `CleanupNetworks = "networks"`, `CleanupVolumes = "volumes"`, `CleanupCache = "cache"`
  - `func AllCleanupTargets() []CleanupTarget`
  - `func DefaultCleanupTargets() []CleanupTarget`
  - `type CleanupOptions struct { Targets []CleanupTarget; AppName string; DryRun bool }`
  - `type CleanupResult struct { Containers []string; Images []string; Networks []string; Volumes []string; CacheBytes int64 }`
  - `Manager.Prune(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — added to interface

- [ ] **Step 1: Write the failing test**

The existing `internal/runtime/cleanup_test.go` already declares `package runtime` and imports `context` and `testing`. Append the following test functions to it (do not re-declare the package):

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), CleanupOptions{Targets: DefaultCleanupTargets()})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Containers != nil || res.Images != nil || res.Networks != nil || res.Volumes != nil || res.CacheBytes != 0 {
		t.Fatalf("expected empty CleanupResult, got %+v", res)
	}
}

func TestDefaultCleanupTargetsExcludesVolumes(t *testing.T) {
	targets := DefaultCleanupTargets()
	if len(targets) != 4 {
		t.Fatalf("expected 4 default targets, got %d", len(targets))
	}
	for _, tgt := range targets {
		if tgt == CleanupVolumes {
			t.Fatal("default targets must exclude volumes")
		}
	}
}

func TestAllCleanupTargetsIncludesAll(t *testing.T) {
	targets := AllCleanupTargets()
	if len(targets) != 5 {
		t.Fatalf("expected 5 targets, got %d", len(targets))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestStubPrune|TestDefaultCleanupTargetsExcludesVolumes|TestAllCleanupTargetsIncludesAll' -count=1`
Expected: FAIL — build error `undefined: CleanupOptions` / `undefined: DefaultCleanupTargets` / `CleanupTarget`.

- [ ] **Step 3: Add types + interface method + stub**

In `internal/runtime/runtime.go`, after the `RunOptions` type (around line 28), add:

```go
type CleanupTarget string

const (
	CleanupContainers CleanupTarget = "containers"
	CleanupImages     CleanupTarget = "images"
	CleanupNetworks   CleanupTarget = "networks"
	CleanupVolumes    CleanupTarget = "volumes"
	CleanupCache      CleanupTarget = "cache"
)

// AllCleanupTargets returns every prunable category.
func AllCleanupTargets() []CleanupTarget {
	return []CleanupTarget{
		CleanupContainers,
		CleanupImages,
		CleanupNetworks,
		CleanupVolumes,
		CleanupCache,
	}
}

// DefaultCleanupTargets returns the categories pruned when no flag is given.
// Volumes are intentionally excluded — they may hold persistent data.
func DefaultCleanupTargets() []CleanupTarget {
	return []CleanupTarget{
		CleanupContainers,
		CleanupImages,
		CleanupNetworks,
		CleanupCache,
	}
}

type CleanupOptions struct {
	Targets []CleanupTarget
	AppName string // if set, only prune resources labeled tengiz-app=<AppName>
	DryRun  bool
}

type CleanupResult struct {
	Containers []string // container IDs removed (or that would be removed)
	Images     []string // image IDs removed
	Networks   []string // network IDs removed
	Volumes    []string // volume names removed
	CacheBytes int64    // bytes reclaimed from build cache; -1 in dry-run when cache would be pruned
}
```

Add `Prune` to the `Manager` interface (after `KeepLastNImages`):

```go
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add the stub implementation (after `KeepLastNImages` on the stub):

```go
func (m *stubManager) Prune(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 4: Fix the CLI test mock so the package compiles**

In `internal/cli/root_test.go`, after the `KeepLastNImages` method of `mockRTForDeploy` (line 99), add:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/runtime/ ./internal/cli/ -count=1`
Expected: PASS (all new tests green, `TestMockRTForDeployImplementsManager` still passes).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Prune cleanup interface for docker housekeeping"
```

---

### Task 2: Container pruning — `parseIDList` + `containerListArgs` + exec wrappers

**Files:**
- Modify: `internal/runtime/cleanup.go` — add helpers + docker wrappers
- Test: `internal/runtime/cleanup_test.go` — table tests

**Interfaces:**
- Consumes: `labelKey` (const in `internal/runtime/docker.go:76`), `CleanupOptions`/`CleanupTarget` from Task 1
- Produces:
  - `func parseIDList(output string) []string`
  - `func containerListArgs(appName string) []string`
  - `func (r *dockerRuntime) listContainerIDs(ctx context.Context, appName string) ([]string, error)`
  - `func (r *dockerRuntime) removeContainers(ctx context.Context, ids []string) error`

- [ ] **Step 1: Write the failing test**

Append the following to `internal/runtime/cleanup_test.go` (package `runtime` already declared) and add `"reflect"` to its imports:

```go
func TestParseIDList(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{"empty", "", nil},
		{"single", "abc123\n", []string{"abc123"}},
		{"multiple with blanks", "abc\n def\n\nghi\n", []string{"abc", "def", "ghi"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIDList(tt.output)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseIDList(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestContainerListArgs(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		want    []string
	}{
		{
			name:    "no app excludes tengiz containers",
			appName: "",
			want:    []string{"ps", "-aq", "--filter", "status=exited", "--filter", "status=created", "--filter", "label!=tengiz-app"},
		},
		{
			name:    "specific app",
			appName: "myapp",
			want:    []string{"ps", "-aq", "--filter", "status=exited", "--filter", "status=created", "--filter", "label=tengiz-app=myapp"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containerListArgs(tt.appName)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("containerListArgs(%q) = %v, want %v", tt.appName, got, tt.want)
			}
		})
	}
}
```

Add `"reflect"` to the imports of `cleanup_test.go` (it is needed by `TestParseIDList` and, later, `TestDockerPruneEmptyTargets`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParseIDList|TestContainerListArgs' -count=1`
Expected: FAIL — `undefined: parseIDList` / `undefined: containerListArgs`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
func parseIDList(output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var ids []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids
}

// containerListArgs returns `docker ps -aq` filters for stopped/created
// containers. Without appName, Tengiz-managed containers (label tengiz-app)
// are excluded. With appName, only that app's containers are matched.
func containerListArgs(appName string) []string {
	args := []string{"ps", "-aq", "--filter", "status=exited", "--filter", "status=created"}
	if appName != "" {
		args = append(args, "--filter", fmt.Sprintf("label=%s=%s", labelKey, appName))
	} else {
		args = append(args, "--filter", fmt.Sprintf("label!=%s", labelKey))
	}
	return args
}

func (r *dockerRuntime) listContainerIDs(ctx context.Context, appName string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", containerListArgs(appName)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return parseIDList(string(out)), nil
}

func (r *dockerRuntime) removeContainers(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"rm", "-f"}, ids...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm: %w\n%s", err, string(out))
	}
	return nil
}
```

The imports in `cleanup.go` already include `context`, `fmt`, `os/exec`, `strings`. No import changes needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestParseIDList|TestContainerListArgs' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): container pruning helpers for cleanup"
```

---

### Task 3: Image / network / volume / build-cache pruning + space parsing

**Files:**
- Modify: `internal/runtime/cleanup.go` — list/remove wrappers + cache + `parseReclaimedSpace` + `FormatBytes`
- Test: `internal/runtime/cleanup_test.go` — table tests

**Interfaces:**
- Consumes: `parseIDList` from Task 2, `CleanupOptions` from Task 1
- Produces:
  - `func (r *dockerRuntime) listDanglingImages(ctx context.Context) ([]string, error)`
  - `func (r *dockerRuntime) removeImages(ctx context.Context, ids []string) error`
  - `func (r *dockerRuntime) listUnusedNetworks(ctx context.Context) ([]string, error)`
  - `func (r *dockerRuntime) removeNetworks(ctx context.Context, ids []string) error`
  - `func (r *dockerRuntime) listUnusedVolumes(ctx context.Context) ([]string, error)`
  - `func (r *dockerRuntime) removeVolumes(ctx context.Context, ids []string) error`
  - `func (r *dockerRuntime) pruneCache(ctx context.Context, dryRun bool) (int64, error)` — returns `-1` in dry-run
  - `func parseReclaimedSpace(output string) int64` (package-level, exported for test use)
  - `func FormatBytes(n int64) string` (exported — CLI uses it for display)

- [ ] **Step 1: Write the failing test**

Append the following to `internal/runtime/cleanup_test.go` (the `"reflect"` import added in Task 2 is already present):

```go
func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int64
	}{
		{"no marker", "clean", 0},
		{"zero", "Total reclaimed space: 0B", 0},
		{"kB adjacent", "Total reclaimed space: 1.5kB", 1500},
		{"MB spaced", "Total reclaimed space: 12.3 MB", 12300000},
		{"GB", "Total reclaimed space: 1.2GB", 1200000000},
		{"KiB", "Total reclaimed space: 2KiB", 2048},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseReclaimedSpace(tt.output); got != tt.want {
				t.Fatalf("parseReclaimedSpace(%q) = %d, want %d", tt.output, got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name string
		n    int64
		want string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 500, "500.00 B"},
		{"kB", 1500, "1.50 kB"},
		{"MB", 12300000, "12.30 MB"},
		{"GB", 1200000000, "1.20 GB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatBytes(tt.n); got != tt.want {
				t.Fatalf("FormatBytes(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

func TestDockerPruneEmptyTargets(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !reflect.DeepEqual(res, CleanupResult{}) {
		t.Fatalf("Prune() = %+v, want empty CleanupResult", res)
	}
}
```

Note: `TestDockerPruneEmptyTargets` exercises the `Prune` orchestration (implemented in Task 4) with zero targets — no docker commands run, so it works without Docker installed.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParseReclaimedSpace|TestFormatBytes|TestDockerPruneEmptyTargets' -count=1`
Expected: FAIL — `undefined: parseReclaimedSpace`, `undefined: FormatBytes`, `undefined: (*dockerRuntime).Prune`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) listDanglingImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return parseIDList(string(out)), nil
}

func (r *dockerRuntime) removeImages(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"rmi", "-f"}, ids...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) listUnusedNetworks(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return parseIDList(string(out)), nil
}

func (r *dockerRuntime) removeNetworks(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"network", "rm"}, ids...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) listUnusedVolumes(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	return parseIDList(string(out)), nil
}

func (r *dockerRuntime) removeVolumes(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"volume", "rm"}, ids...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume rm: %w\n%s", err, string(out))
	}
	return nil
}

var spaceUnits = map[string]int64{
	"B":   1,
	"kB":  1e3,
	"MB":  1e6,
	"GB":  1e9,
	"TB":  1e12,
	"KiB": 1 << 10,
	"MiB": 1 << 20,
	"GiB": 1 << 30,
	"TiB": 1 << 40,
}

// parseReclaimedSpace extracts the "Total reclaimed space:" value from a
// docker prune command's output and converts it to bytes.
func parseReclaimedSpace(output string) int64 {
	const marker = "Total reclaimed space:"
	idx := strings.Index(output, marker)
	if idx < 0 {
		return 0
	}
	rest := strings.TrimSpace(output[idx+len(marker):])
	if rest == "" {
		return 0
	}
	numEnd := 0
	for numEnd < len(rest) {
		c := rest[numEnd]
		if (c >= '0' && c <= '9') || c == '.' || c == ',' {
			numEnd++
			continue
		}
		break
	}
	if numEnd == 0 {
		return 0
	}
	val, err := strconv.ParseFloat(strings.Replace(rest[:numEnd], ",", ".", 1), 64)
	if err != nil {
		return 0
	}
	unit := strings.TrimSpace(rest[numEnd:])
	mult, ok := spaceUnits[unit]
	if !ok {
		return 0
	}
	return int64(val * float64(mult))
}

// FormatBytes renders a byte count in a compact human-readable form.
func FormatBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "kB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	return fmt.Sprintf("%.2f %s", f, units[i])
}

func (r *dockerRuntime) pruneCache(ctx context.Context, dryRun bool) (int64, error) {
	if dryRun {
		// docker builder prune has no dry-run mode; report intent.
		return -1, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parseReclaimedSpace(string(out)), nil
}
```

Add `"strconv"` to the imports in `cleanup.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestParseReclaimedSpace|TestFormatBytes|TestDockerPruneEmptyTargets' -count=1`
Expected: PASS (Task 4's `Prune` still missing → `TestDockerPruneEmptyTargets` fails here; do the Task 4 implementation below in the same working session before committing if it blocks the build).

**Important:** `TestDockerPruneEmptyTargets` cannot pass until Task 4 adds `Prune`. If you prefer strictly green commits, implement Task 4's `Prune` method now (copy from Task 4 Step 2), then commit Tasks 3+4 together:

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): image/network/volume/cache pruning + space parsing"
```

---

### Task 4: `Prune` orchestration on `dockerRuntime`

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go` (already has `TestDockerPruneEmptyTargets` from Task 3)

**Interfaces:**
- Consumes: all list/remove helpers from Tasks 2-3, `CleanupOptions`/`CleanupResult` from Task 1, `KeepLastNImages` (existing)
- Produces: `func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — satisfies the `Manager` interface

- [ ] **Step 1: Write the failing test**

`TestDockerPruneEmptyTargets` (written in Task 3 Step 1) is the test for this task — it verifies the orchestration returns an empty result without invoking docker when no targets are given.

Run: `go test ./internal/runtime/ -run TestDockerPruneEmptyTargets -count=1`
Expected: FAIL — `undefined: (*dockerRuntime).Prune`.

- [ ] **Step 2: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	for _, target := range opts.Targets {
		switch target {
		case CleanupContainers:
			ids, err := r.listContainerIDs(ctx, opts.AppName)
			if err != nil {
				return res, err
			}
			res.Containers = ids
			if !opts.DryRun && len(ids) > 0 {
				if err := r.removeContainers(ctx, ids); err != nil {
					return res, err
				}
			}
		case CleanupImages:
			ids, err := r.listDanglingImages(ctx)
			if err != nil {
				return res, err
			}
			res.Images = ids
			if !opts.DryRun && len(ids) > 0 {
				if err := r.removeImages(ctx, ids); err != nil {
					return res, err
				}
			}
			if opts.AppName != "" {
				// Prune this app's old tagged images, keeping the newest 5.
				if err := r.KeepLastNImages(ctx, opts.AppName, 5); err != nil {
					log.Printf("[runtime] cleanup: failed to prune old images for %s: %v", opts.AppName, err)
				}
			}
		case CleanupNetworks:
			ids, err := r.listUnusedNetworks(ctx)
			if err != nil {
				return res, err
			}
			res.Networks = ids
			if !opts.DryRun && len(ids) > 0 {
				if err := r.removeNetworks(ctx, ids); err != nil {
					return res, err
				}
			}
		case CleanupVolumes:
			ids, err := r.listUnusedVolumes(ctx)
			if err != nil {
				return res, err
			}
			res.Volumes = ids
			if !opts.DryRun && len(ids) > 0 {
				if err := r.removeVolumes(ctx, ids); err != nil {
					return res, err
				}
			}
		case CleanupCache:
			bytes, err := r.pruneCache(ctx, opts.DryRun)
			if err != nil {
				return res, err
			}
			res.CacheBytes = bytes
		}
	}
	return res, nil
}
```

`log` is already imported in `cleanup.go`.

- [ ] **Step 3: Run tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/runtime/ -count=1`
Expected: PASS (all runtime tests including `TestDockerPruneEmptyTargets`).

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): orchestrate cleanup prune across categories"
```

---

### Task 5: CLI `tengiz cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupTarget`, `runtime.AllCleanupTargets()`, `runtime.DefaultCleanupTargets()`, `runtime.CleanupResult`, `runtime.FormatBytes` (all from Tasks 1 & 3), `runtime.NewDocker()` (existing), `getEnv(cmd)` (existing in `root.go`)
- Produces:
  - `var cleanupCmd = &cobra.Command{...}` registered on `rootCmd` via `init()`
  - `func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error)`
  - `func confirmCleanup(r io.Reader, prompt string) bool`
  - `func printCleanupResult(res runtime.CleanupResult, dryRun bool)`
  - Flags: `--force/-f`, `--dry-run`, `--app`, `--containers`, `--images`, `--networks`, `--volumes`, `--cache`, `--all`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func newCleanupTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "cleanup", RunE: cleanupCmd.RunE}
	c.Flags().BoolP("force", "f", false, "")
	c.Flags().Bool("dry-run", false, "")
	c.Flags().String("app", "", "")
	c.Flags().Bool("containers", false, "")
	c.Flags().Bool("images", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("cache", false, "")
	c.Flags().Bool("all", false, "")
	return c
}

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
	for _, flag := range []string{"force", "dry-run", "app", "containers", "images", "networks", "volumes", "cache", "all"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupOptionsFromFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want runtime.CleanupOptions
	}{
		{
			name: "defaults exclude volumes",
			args: []string{},
			want: runtime.CleanupOptions{Targets: runtime.DefaultCleanupTargets()},
		},
		{
			name: "dry run with app",
			args: []string{"--dry-run", "--app", "myapp"},
			want: runtime.CleanupOptions{Targets: runtime.DefaultCleanupTargets(), AppName: "myapp", DryRun: true},
		},
		{
			name: "volumes only",
			args: []string{"--volumes"},
			want: runtime.CleanupOptions{Targets: []runtime.CleanupTarget{runtime.CleanupVolumes}},
		},
		{
			name: "containers and cache",
			args: []string{"--containers", "--cache"},
			want: runtime.CleanupOptions{Targets: []runtime.CleanupTarget{runtime.CleanupContainers, runtime.CleanupCache}},
		},
		{
			name: "all",
			args: []string{"--all"},
			want: runtime.CleanupOptions{Targets: runtime.AllCleanupTargets()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCleanupTestCmd()
			if err := c.ParseFlags(tt.args); err != nil {
				t.Fatalf("ParseFlags(%v): %v", tt.args, err)
			}
			got, err := cleanupOptionsFromFlags(c)
			if err != nil {
				t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("cleanupOptionsFromFlags() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestConfirmCleanup(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"yes", "y\n", true},
		{"YES", "YES\n", true},
		{"no", "n\n", false},
		{"empty", "\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := confirmCleanup(strings.NewReader(tt.input), "continue?"); got != tt.want {
				t.Fatalf("confirmCleanup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCleanupCancelledOnNo(t *testing.T) {
	c := newCleanupTestCmd()
	c.SetIn(strings.NewReader("n\n"))
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestConfirmCleanup' -count=1`
Expected: FAIL — `undefined: cleanupCmd`, `undefined: cleanupOptionsFromFlags`, `undefined: confirmCleanup`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Removes unused Docker resources to reclaim disk space.

By default prunes stopped containers, dangling images, unused networks, and
build cache. Tengiz-managed containers (label tengiz-app) are never touched
unless --app is given. Volumes are excluded by default because they may hold
persistent data; use --volumes or --all to include them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		force, _ := cmd.Flags().GetBool("force")

		if !force && !opts.DryRun {
			if !confirmCleanup(cmd.InOrStdin(), "Remove unused Docker resources?") {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupResult(result, opts.DryRun)
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	flags := cmd.Flags()
	dryRun, _ := flags.GetBool("dry-run")
	appName, _ := flags.GetString("app")
	all, _ := flags.GetBool("all")

	var targets []runtime.CleanupTarget
	if all {
		targets = runtime.AllCleanupTargets()
	} else {
		cats := []struct {
			name   string
			target runtime.CleanupTarget
		}{
			{"containers", runtime.CleanupContainers},
			{"images", runtime.CleanupImages},
			{"networks", runtime.CleanupNetworks},
			{"volumes", runtime.CleanupVolumes},
			{"cache", runtime.CleanupCache},
		}
		for _, c := range cats {
			if v, _ := flags.GetBool(c.name); v {
				targets = append(targets, c.target)
			}
		}
		if len(targets) == 0 {
			targets = runtime.DefaultCleanupTargets()
		}
	}

	return runtime.CleanupOptions{Targets: targets, AppName: appName, DryRun: dryRun}, nil
}

func confirmCleanup(r io.Reader, prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	var answer string
	fmt.Fscanln(r, &answer)
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}

func printCleanupResult(res runtime.CleanupResult, dryRun bool) {
	mode := "removed"
	if dryRun {
		mode = "would be removed"
	}
	fmt.Printf("[tengiz] cleanup: %d containers %s\n", len(res.Containers), mode)
	fmt.Printf("[tengiz] cleanup: %d images %s\n", len(res.Images), mode)
	fmt.Printf("[tengiz] cleanup: %d networks %s\n", len(res.Networks), mode)
	fmt.Printf("[tengiz] cleanup: %d volumes %s\n", len(res.Volumes), mode)
	if res.CacheBytes < 0 {
		fmt.Println("[tengiz] cleanup: build cache would be pruned")
	} else {
		fmt.Printf("[tengiz] cleanup: build cache reclaimed %s\n", runtime.FormatBytes(res.CacheBytes))
	}
}

func init() {
	cleanupCmd.Flags().BoolP("force", "f", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be cleaned without removing anything")
	cleanupCmd.Flags().String("app", "", "only clean resources for this app")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (CAUTION: may delete persistent data)")
	cleanupCmd.Flags().Bool("cache", false, "prune build cache")
	cleanupCmd.Flags().Bool("all", false, "prune all categories including volumes")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/cli/ -count=1`
Expected: PASS (all CLI tests, new and existing).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 6: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the exact `tengiz cleanup` CLI surface produced in Task 5

- [ ] **Step 1: Add `tengiz cleanup` to README CLI Reference**

Insert after the `#### tengiz volume list <app>` section (after line ~302 in `README.md`, before `### tengiz preview`):

```markdown
### `tengiz cleanup`

Reclaim disk space by removing unused Docker resources. By default prunes stopped containers, dangling images, unused networks, and build cache. **Tengiz-managed containers are never removed** unless `--app` is given. Volumes are excluded by default because they may hold persistent data — use `--volumes` or `--all` to include them.

| Flag | Description |
|------|-------------|
| `-f`, `--force` | Skip the confirmation prompt |
| `--dry-run` | Show what would be cleaned without removing anything |
| `--app <name>` | Only clean resources for this app (prunes its stopped containers and keeps its 5 newest images) |
| `--containers` | Prune stopped containers |
| `--images` | Prune dangling images |
| `--networks` | Prune unused networks |
| `--volumes` | Prune unused volumes (CAUTION: may delete persistent data) |
| `--cache` | Prune build cache |
| `--all` | Prune all categories including volumes |

If no category flag is given, defaults to `--containers --images --networks --cache`. Without `--force` or `--dry-run`, prompts for confirmation before removing anything.

```bash
tengiz cleanup --dry-run   # see what would be removed
tengiz cleanup             # prune defaults (prompts first)
tengiz cleanup --all -f    # prune everything, no prompt
```
```

- [ ] **Step 2: Add `tengiz cleanup` to README Commands table**

Add a row to the Commands table near `README.md:575` (the `### Commands` table under the git-deploy section):

```markdown
| `tengiz cleanup [--all] [--dry-run] [--app <name>]` | Prune unused Docker resources to reclaim disk space |
```

- [ ] **Step 3: Mark feature #6 implemented in FUTURES_FEATURES.md**

1. In the P0 Priority Ranking table (`docs/FUTURES_FEATURES.md:19`), change the status cell:

   `| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel |` → `| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel |`

2. In the "✅ Implemented Features (Not Pending)" table, add:

   `| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-13) |`

3. In the `## Docker Housekeeping (Otomatik Temizlik)` detail section (line ~377), add a Status line after the "Why add to Tengiz" line:

   `- **Status:** ✅ Implemented (2026-08-13)`

- [ ] **Step 4: Verify full suite + commit**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: PASS.

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Manual Smoke Test (after all tasks)

Run against a real Docker daemon to confirm behavior end-to-end:

```bash
go build -o tengiz .
./tengiz cleanup --dry-run        # lists candidates, removes nothing
./tengiz cleanup -f               # prunes containers/images/networks/cache
./tengiz cleanup --all --dry-run  # also lists volumes
./tengiz cleanup --volumes -f     # prunes unused volumes too
./tengiz cleanup --app myapp -f   # only myapp's stopped containers + keeps 5 newest images
```

Verify that a running/stopped Tengiz app (`tengiz-app` label) is untouched by a global `tengiz cleanup --all -f` but IS removed by `tengiz cleanup --app <that-app> -f`.

## Self-Review

**Spec coverage:**
- Feature #6 "Docker Housekeeping": label-based cleanup that protects Tengiz containers → Task 2 (`label!=tengiz-app`), Task 4 (AppName guard), Task 5 (CLI). `tengiz cleanup` command → Task 5. Disk-space focus via dangling images/networks/containers/cache → Tasks 2-4.
- Feature #56 "Granular Docker Prune Operations" (per-category prune) → Task 5 category flags, Task 4 per-target orchestration.
- Test-first on every pure helper (parseIDList, containerListArgs, parseReclaimedSpace, FormatBytes, cleanupOptionsFromFlags, confirmCleanup), stub + empty-targets orchestration, CLI registration/flag tests.
- README/docs updated per repo rule.

**Placeholder scan:** No TBD/TODO/`similar to Task N`/`add error handling` — every step has complete code and exact commands with expected output.

**Type consistency:** `CleanupTarget`/`CleanupOptions`/`CleanupResult` names and fields match across Tasks 1-5. `Prune` signature (`ctx context.Context, opts CleanupOptions) (CleanupResult, error`) is identical on interface, stub, mock, and docker implementation. `FormatBytes` is exported in `runtime` (Task 3) and referenced as `runtime.FormatBytes` in CLI (Task 5) — consistent. `containerListArgs`/`parseIDList` used by both the container task and (via `parseIDList`) image/network/volume tasks.