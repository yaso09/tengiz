# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup [category]` command family that prunes unused Docker containers, images, volumes, networks, and build cache while protecting Tengiz-managed containers via labels, plus an optional per-app image retention policy.

**Architecture:** A new `runtime.Manager` method `Prune(ctx, opts PruneOptions) (PruneResult, error)` shells out to `docker container prune`, `docker image prune`, `docker volume prune`, `docker network prune`, and `docker builder prune` via `os/exec`, using `--filter label!=tengiz-app` to protect Tengiz containers. A pure helper `pruneArgs(kind, all)` builds the argument lists so they are unit-testable without a Docker daemon. The CLI wires a new `internal/cli/cleanup.go` command (registered in its own `init()`) that maps an optional category argument and `--yes`/`--all`/`--keep` flags onto `PruneOptions`, prompts for confirmation unless `--yes`, and runs the existing `KeepLastNImages` per app when `--keep N > 0`. A narrow `runtimeCleaner` interface plus an injectable `newDockerRuntime` factory make the CLI testable with a two-method mock instead of a full 19-method `Manager` mock.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` (`os/exec`-based Docker CLI), stdlib `testing`. No new external dependencies.

## Global Constraints

- Go 1.26, stdlib `testing` only — no testify, no assertion helpers
- No Docker SDK — all Docker operations via `os/exec` `docker` CLI (must be installed)
- All destructive Docker commands MUST protect Tengiz containers with `--filter label!=tengiz-app` (containers, networks, volumes)
- Docker command output captured via `CombinedOutput()`; errors wrapped as `fmt.Errorf("docker <verb>: %w\n%s", err, string(out))`
- CLI success output prefixed `[tengiz]`; uses `getEnv(cmd)` and `dataDir` package globals (existing patterns)
- Default `--keep 0` — image retention disabled unless explicitly requested
- No new files outside: `internal/runtime/cleanup.go`, `internal/runtime/runtime.go`, `internal/runtime/cleanup_test.go`, `internal/cli/cleanup.go`, `internal/cli/cleanup_test.go`, plus mocks in 3 existing test files
- Follow AGENTS.md rules: work on branch `feat/docker-housekeeping`, update/add tests, run `go test ./... -v -count=1` and `go vet ./...`, commit after each task

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Add `PruneOptions`, `PruneResult` (+ `String()`), `pruneArgs()`, `runDocker()`, `dockerRuntime.Prune()` |
| `internal/runtime/runtime.go` | Add `Prune` to `Manager` interface; add stub implementation to `stubManager` |
| `internal/runtime/cleanup_test.go` | Add `TestPruneArgs`, `TestStubPrune` |
| `internal/cli/root_test.go` | Add `Prune` method to `mockRTForDeploy` (compile fix) |
| `internal/idle/idle_test.go` | Add `Prune` method to `mockRuntime` (compile fix) |
| `internal/proxy/proxy_test.go` | Add `Prune` method to `mockRuntime` (compile fix) |
| `internal/cli/cleanup.go` | New `tengiz cleanup` command + `runtimeCleaner` interface + `newDockerRuntime` factory + `confirm()` |
| `internal/cli/cleanup_test.go` | CLI tests with a 2-method `mockCleaner` |
| `README.md` | Document `tengiz cleanup` section |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 implemented |
| `AGENTS.md` | Add `tengiz cleanup` line to CLI reference |

---

### Task 1: Runtime Prune API

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Modify: `internal/runtime/runtime.go:31-49` (interface) and `runtime.go:113-119` (stub)
- Modify: `internal/runtime/cleanup_test.go`
- Modify: `internal/cli/root_test.go:76-100` (mockRTForDeploy)
- Modify: `internal/idle/idle_test.go:14-34` (mockRuntime)
- Modify: `internal/proxy/proxy_test.go:15-35` (mockRuntime)

**Interfaces:**
- Consumes: nothing new (uses existing `exec.CommandContext`, `types` package)
- Produces:
  - `runtime.PruneOptions struct { Containers, Images, Volumes, Networks, BuildCache, AllImages bool }`
  - `runtime.PruneResult struct { Containers, Images, Volumes, Networks, BuildCache string }`
  - `func (r PruneResult) String() string` — non-empty fields joined by `"\n"`
  - `func pruneArgs(kind string, all bool) []string` — unexported, returns docker args (nil for unknown kind)
  - `func runDocker(ctx context.Context, args ...string) ([]byte, error)` — unexported
  - `func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`
  - `Manager.Prune(...)` + `stubManager.Prune(...)`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		all      bool
		expected []string
	}{
		{"containers", "containers", false, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"images dangling", "images", false, []string{"image", "prune", "-f"}},
		{"images all", "images", true, []string{"image", "prune", "-f", "-a"}},
		{"volumes", "volumes", false, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"networks", "networks", false, []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"cache", "cache", false, []string{"builder", "prune", "-f"}},
		{"unknown", "bogus", false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneArgs(tt.kind, tt.all)
			if len(got) != len(tt.expected) {
				t.Fatalf("pruneArgs(%q, %v) = %v, want %v", tt.kind, tt.all, got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("pruneArgs(%q, %v) = %v, want %v", tt.kind, tt.all, got, tt.expected)
				}
			}
		})
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	result, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if result.String() != "" {
		t.Fatalf("expected empty stub result, got %q", result.String())
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestPruneArgs|TestStubPrune' -count=1`

Expected: FAIL with `undefined: pruneArgs` / `undefined: Prune` (the test file does not compile).

- [ ] **Step 4: Implement `pruneArgs`, `runDocker`, types, and `Prune`**

Append to `internal/runtime/cleanup.go` (it already imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`):

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	AllImages  bool
}

type PruneResult struct {
	Containers string
	Images     string
	Volumes    string
	Networks   string
	BuildCache string
}

func (r PruneResult) String() string {
	var parts []string
	for _, s := range []string{r.Containers, r.Images, r.Volumes, r.Networks, r.BuildCache} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

func pruneArgs(kind string, all bool) []string {
	switch kind {
	case "containers":
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "images":
		args := []string{"image", "prune", "-f"}
		if all {
			args = append(args, "-a")
		}
		return args
	case "volumes":
		return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "networks":
		return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "cache":
		return []string{"builder", "prune", "-f"}
	default:
		return nil
	}
}

func runDocker(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var result PruneResult

	if opts.Containers {
		out, err := runDocker(ctx, pruneArgs("containers", false)...)
		if err != nil {
			return result, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
		}
		result.Containers = strings.TrimSpace(string(out))
	}
	if opts.Images {
		out, err := runDocker(ctx, pruneArgs("images", opts.AllImages)...)
		if err != nil {
			return result, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
		}
		result.Images = strings.TrimSpace(string(out))
	}
	if opts.Volumes {
		out, err := runDocker(ctx, pruneArgs("volumes", false)...)
		if err != nil {
			return result, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
		}
		result.Volumes = strings.TrimSpace(string(out))
	}
	if opts.Networks {
		out, err := runDocker(ctx, pruneArgs("networks", false)...)
		if err != nil {
			return result, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
		}
		result.Networks = strings.TrimSpace(string(out))
	}
	if opts.BuildCache {
		out, err := runDocker(ctx, pruneArgs("cache", false)...)
		if err != nil {
			return result, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
		}
		result.BuildCache = strings.TrimSpace(string(out))
	}

	return result, nil
}
```

- [ ] **Step 5: Add `Prune` to the `Manager` interface and the stub**

In `internal/runtime/runtime.go`, add to the interface after the `Run` line (line 48):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

Add to the stub at the end of the file (after `stubManager.Run`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}
```

- [ ] **Step 6: Update the three test-file mock `Manager` implementations**

Add this method to `mockRTForDeploy` in `internal/cli/root_test.go` (after its `Run` method, line 100):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

Add this method to `mockRuntime` in `internal/idle/idle_test.go` (after its `Run` method, line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

Add this method to `mockRuntime` in `internal/proxy/proxy_test.go` (after its `Run` method, line 35):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./... -count=1`

Expected: PASS (all packages compile; `TestPruneArgs` and `TestStubPrune` green).

- [ ] **Step 8: Run vet**

Run: `go vet ./...`

Expected: no output (exit 0).

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Prune API for docker resource cleanup"
```

---

### Task 2: CLI `tengiz cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult` (from Task 1), `runtime.Prune` and `runtime.KeepLastNImages(ctx, appName string, n int) error` (existing), `config.NewStoreWithEnv(dataDir, env)`, `store.ListApps()`, `getEnv(cmd)`, `dataDir`
- Produces:
  - `type runtimeCleaner interface { Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error); KeepLastNImages(ctx context.Context, appName string, n int) error }`
  - `var newDockerRuntime = func() (runtimeCleaner, error)` — injectable factory, default `runtime.NewDocker`
  - `cleanupCmd` (Cobra command: `tengiz cleanup [category]`)
  - `func cleanupOptions(category string, all bool) (runtime.PruneOptions, error)` — pure, maps category → options
  - `func runCleanup(cmd *cobra.Command, category string) error`
  - `func confirm(prompt string) bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type mockCleaner struct {
	pruned    atomic.Int32
	keepCalls atomic.Int32
	lastOpts  runtime.PruneOptions
	result    runtime.PruneResult
}

func (m *mockCleaner) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	m.pruned.Add(1)
	m.lastOpts = opts
	return m.result, nil
}

func (m *mockCleaner) KeepLastNImages(ctx context.Context, appName string, n int) error {
	m.keepCalls.Add(1)
	return nil
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd.Name() != "cleanup" {
		t.Fatalf("expected cleanup command, got %q", cmd.Name())
	}
}

func TestCleanupCommandPrunesAllCategories(t *testing.T) {
	dataDir = t.TempDir()
	mock := &mockCleaner{result: runtime.PruneResult{Containers: "removed 2", Images: "removed 3"}}
	old := newDockerRuntime
	newDockerRuntime = func() (runtimeCleaner, error) { return mock, nil }
	defer func() { newDockerRuntime = old }()

	rootCmd.SetArgs([]string{"cleanup", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if mock.pruned.Load() != 1 {
		t.Fatalf("expected Prune called once, got %d", mock.pruned.Load())
	}
	opts := mock.lastOpts
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories, got %+v", opts)
	}
	if mock.keepCalls.Load() != 0 {
		t.Errorf("expected no KeepLastNImages calls with keep=0, got %d", mock.keepCalls.Load())
	}
}

func TestCleanupCommandCategoryImages(t *testing.T) {
	dataDir = t.TempDir()
	mock := &mockCleaner{}
	old := newDockerRuntime
	newDockerRuntime = func() (runtimeCleaner, error) { return mock, nil }
	defer func() { newDockerRuntime = old }()

	rootCmd.SetArgs([]string{"cleanup", "images", "--yes", "--all"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if mock.pruned.Load() != 1 {
		t.Fatalf("expected Prune called once, got %d", mock.pruned.Load())
	}
	opts := mock.lastOpts
	if !opts.Images || !opts.AllImages {
		t.Errorf("expected Images=true AllImages=true, got %+v", opts)
	}
	if opts.Containers || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("expected only images category, got %+v", opts)
	}
}

func TestCleanupCommandKeepRetention(t *testing.T) {
	dataDir = t.TempDir()
	store := config.NewStore(dataDir)
	if err := store.SaveApp(types.AppEntry{Name: "app1", Config: types.AppConfig{Name: "app1"}}); err != nil {
		t.Fatal(err)
	}
	mock := &mockCleaner{}
	old := newDockerRuntime
	newDockerRuntime = func() (runtimeCleaner, error) { return mock, nil }
	defer func() { newDockerRuntime = old }()

	rootCmd.SetArgs([]string{"cleanup", "--yes", "--keep", "3"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if mock.keepCalls.Load() != 1 {
		t.Fatalf("expected 1 KeepLastNImages call, got %d", mock.keepCalls.Load())
	}
}

func TestCleanupCommandUnknownCategory(t *testing.T) {
	dataDir = t.TempDir()
	rootCmd.SetArgs([]string{"cleanup", "bogus", "--yes"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for unknown category")
	}
}

func TestCleanupOptions(t *testing.T) {
	opts, err := cleanupOptions("containers", false)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Containers || opts.Images || opts.Volumes || opts.Networks || opts.BuildCache || opts.AllImages {
		t.Errorf("unexpected options for containers: %+v", opts)
	}

	opts, err = cleanupOptions("", true)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache || !opts.AllImages {
		t.Errorf("unexpected options for default all: %+v", opts)
	}

	if _, err := cleanupOptions("bogus", false); err == nil {
		t.Error("expected error for unknown category")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup' -count=1`

Expected: FAIL with `undefined: cleanupCmd` / `undefined: newDockerRuntime` (the test file does not compile).

- [ ] **Step 3: Implement the `tengiz cleanup` command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

type runtimeCleaner interface {
	Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error)
	KeepLastNImages(ctx context.Context, appName string, n int) error
}

var newDockerRuntime = func() (runtimeCleaner, error) {
	return runtime.NewDocker()
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup [category]",
	Short: "Prune unused Docker resources",
	Long: `Prunes unused Docker containers, images, volumes, networks and build cache.
Tengiz-managed containers are protected via the tengiz-app label.

Categories: containers, images, volumes, networks, cache.
With no category, all categories are pruned.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		category := ""
		if len(args) > 0 {
			category = args[0]
		}
		return runCleanup(cmd, category)
	},
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
	cleanupCmd.Flags().Bool("all", false, "remove all unused images (not just dangling)")
	cleanupCmd.Flags().Int("keep", 0, "keep the last N images per app (0 = skip image retention)")
}

func cleanupOptions(category string, all bool) (runtime.PruneOptions, error) {
	switch category {
	case "", "all":
		return runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true, AllImages: all}, nil
	case "containers":
		return runtime.PruneOptions{Containers: true}, nil
	case "images":
		return runtime.PruneOptions{Images: true, AllImages: all}, nil
	case "volumes":
		return runtime.PruneOptions{Volumes: true}, nil
	case "networks":
		return runtime.PruneOptions{Networks: true}, nil
	case "cache":
		return runtime.PruneOptions{BuildCache: true}, nil
	default:
		return runtime.PruneOptions{}, fmt.Errorf("unknown cleanup category %q (valid: containers, images, volumes, networks, cache)", category)
	}
}

func runCleanup(cmd *cobra.Command, category string) error {
	env := getEnv(cmd)
	yes, _ := cmd.Flags().GetBool("yes")
	all, _ := cmd.Flags().GetBool("all")
	keep, _ := cmd.Flags().GetInt("keep")

	opts, err := cleanupOptions(category, all)
	if err != nil {
		return err
	}

	if !yes && !confirm("This will remove unused Docker resources. Continue? [y/N]: ") {
		fmt.Println("[tengiz] aborted")
		return nil
	}

	rt, err := newDockerRuntime()
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}

	result, err := rt.Prune(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	if keep > 0 && opts.Images {
		store := config.NewStoreWithEnv(dataDir, env)
		apps, err := store.ListApps()
		if err != nil {
			return fmt.Errorf("list apps: %w", err)
		}
		for _, app := range apps {
			if err := rt.KeepLastNImages(cmd.Context(), app.Name, keep); err != nil {
				fmt.Printf("[tengiz] warning: image retention for %s: %v\n", app.Name, err)
			}
		}
	}

	if s := result.String(); s != "" {
		fmt.Println(s)
	}
	fmt.Println("[tengiz] cleanup complete")
	return nil
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes"
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup' -count=1`

Expected: PASS (all `TestCleanup*` tests green).

- [ ] **Step 5: Run the full test suite and vet**

Run: `go test ./... -count=1 && go vet ./...`

Expected: PASS, no vet output.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 3: Documentation and final verification

**Files:**
- Modify: `README.md` (add `tengiz cleanup` section after the `tengiz rollback` section, ~line 236)
- Modify: `docs/FUTURES_FEATURES.md` (P0 table row #6, implemented-features table, detailed section)
- Modify: `AGENTS.md` (add line to CLI reference, ~line 60)

**Interfaces:**
- Consumes: command surface from Task 2 (`tengiz cleanup [category]`, `--yes`, `--all`, `--keep`)
- Produces: updated docs only

- [ ] **Step 1: Add the `tengiz cleanup` section to README.md**

Insert after the `### tengiz rollback <app>` section (which ends before `### tengiz domain`):

```markdown
### `tengiz cleanup [category]`

Prune unused Docker resources to reclaim disk space. Tengiz-managed containers are protected via the `tengiz-app` label.

| Argument | Description |
|----------|-------------|
| `category` | Optional: `containers`, `images`, `volumes`, `networks`, `cache`. Defaults to all categories. |

| Flag | Description |
|------|-------------|
| `--yes`, `-y` | Skip the confirmation prompt |
| `--all` | Remove all unused images, not just dangling ones |
| `--keep N` | Keep the last N images per app after pruning |

Examples:

```
tengiz cleanup                 # prune containers, images, volumes, networks, build cache
tengiz cleanup images --all    # remove all unused images
tengiz cleanup --keep 5        # prune everything, keep last 5 images per app
tengiz cleanup --yes           # run without confirmation prompt
```
```

- [ ] **Step 2: Mark feature #6 as implemented in docs/FUTURES_FEATURES.md**

In the P0 table, change the row:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based prune with Tengiz-managed container protection. `tengiz cleanup`. |
```

Add a row to the "✅ Implemented Features (Not Pending)" table (after the Webhook row):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-20) |
```

In the detailed `## Docker Housekeeping (Otomatik Temizlik)` section, add a status line before `- **Detected:** 2026-07-14`:

```markdown
- **Status:** ✅ Implemented (2026-08-20)
```

- [ ] **Step 3: Add the command to AGENTS.md**

In the CLI section, after the `tengiz rollback <app>` line:

```markdown
tengiz cleanup [category]      → prune unused Docker resources (--yes, --all, --keep N)
```

- [ ] **Step 4: Build, test, vet**

Run:

```bash
go build ./...
go test ./... -v -count=1
go vet ./...
```

Expected: build succeeds, all tests PASS, vet clean.

- [ ] **Step 5: Manual smoke test (if Docker is available)**

Run: `tengiz cleanup --help`

Expected: shows usage, flags `--yes`, `--all`, `--keep`.

If `docker` is installed on the host, run `tengiz cleanup cache --yes` and expect output containing `Total reclaimed space:` followed by `[tengiz] cleanup complete`. If Docker is not installed, `tengiz cleanup cache --yes` should print a `docker:` error — acceptable and expected.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage:** The feature spec (FUTURES_FEATURES.md #6) requires: label-based `docker system prune` (implemented as per-category `docker container/network/volume prune` with `--filter label!=tengiz-app`, which achieves the same protection with finer control), and a `tengiz cleanup` command (Task 2). The detailed Coolify-derived description adds: cleanup of volumes/networks/containers/images (Task 1 covers all five categories including build cache) and "label-based filtering protects Tengiz-managed containers" (the `label!=tengiz-app` filters). No gaps.

**2. Placeholder scan:** All steps contain concrete code or exact commands with expected output. No "TBD"/"TODO"/"similar to Task N" placeholders. The only conditional step (Task 3 Step 5) is explicit about both possible outcomes.

**3. Type consistency:** `PruneOptions`/`PruneResult` field names are identical across Task 1 (definition), Task 2 (CLI tests `lastOpts` assertions, `cleanupOptions`), and Task 3 (README flags). `pruneArgs("cache", ...)` maps to `builder prune` everywhere. `runtimeCleaner` exposes exactly `Prune` + `KeepLastNImages` — both referenced by `runCleanup` and implemented by `mockCleaner`. `stubManager`, `mockRTForDeploy`, and both `mockRuntime` mocks all gain the same `Prune` signature. The category string `"cache"` is consistent between `cleanupOptions`, `pruneArgs`, and the usage text.

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-08-20-docker-housekeeping.md`. Two execution options:**

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**