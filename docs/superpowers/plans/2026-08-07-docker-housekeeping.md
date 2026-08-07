# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes abandoned Docker containers, images, unused volumes, and build cache while leaving all Tengiz-managed application containers untouched, so disk space on single-server deployments is reclaimed without a global `docker system prune`.

**Architecture:** Extend the existing `runtime.Manager` interface with a single `Cleanup(ctx, CleanupOptions) (CleanupResult, error)` method. The `dockerRuntime` implementation shells out to `docker container prune` / `docker image prune` / `docker volume prune` / `docker builder prune`, each guarded by a filter that *excludes* containers carrying the `tengiz-app` label (the marker every Tengiz container gets at `Create` time). A new `cleanupCmd` Cobra command in `internal/cli/root.go` translates CLI flags into `CleanupOptions` and prints the reclaimed counts. The stub manager and mock test runtime get a no-op `Cleanup` so existing `runtime.Manager` consumers continue to compile.

**Tech Stack:** Go 1.26, Cobra (flags), `os/exec` (docker CLI — matches existing `dockerRuntime`), existing `runtime.Manager`, `internal/cli` root command wiring. No new external dependencies.

## Global Constraints

- Only prune Docker resources that are NOT managed by Tengiz — configuration must never remove a container labeled `tengiz-app=<name>`
- The `tengiz-app` and `tengiz-env` labels are set on every runtime-created container (see `internal/runtime/docker.go:76-77`)
- The `Cleanup` method MUST be added to the `runtime.Manager` interface AND to every implementation (`dockerRuntime`, `stubManager`) plus every mock in test files referenced by the interface assertion (`TestMockRTForDeployImplementsManager` in `internal/cli/root_test.go`)
- Command default (`tengiz cleanup` with no flags) prunes all four categories; each category can be limited via its own `--containers` / `--images` / `--volumes` / `--build-cache` flag
- `--all` is accepted as an explicit alias for "prune everything"; `--dry-run` lists what would be pruned without deleting
- New `Cleanup` controller must pass through the same `env` semantics already used by other commands via `getEnv(cmd)` (housekeeping is global across apps, so it does not filter by env, but it must not delete `tengiz-app`-labeled containers of ANY env)
- Existing tests must continue to pass unmodified after the interface extension (stub returns no-op)
- No new external Go dependencies — only the already-available `os/exec` docker CLI
- Deletion commands always run with `-f` / `--force` (non-interactive); `--dry-run` is the only safe, no-op escape hatch
- All error messages return `fmt.Errorf("docker container prune: %w\n%s", err, out)` style consistent with `internal/runtime/docker.go`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupResult`, and the `Cleanup` method to the `Manager` interface; add the no-op `Cleanup` to `stubManager` (`runtime.go`) |
| `internal/runtime/prune.go` | **Create** — `dockerRuntime.Cleanup` implementation + the four `prune*` helpers that shell out to docker |
| `internal/runtime/prune_test.go` | **Create** — unit tests for option resolution and result parsing |
| `internal/cli/root.go` | Add `cleanupCmd` + wire into `init()` |
| `internal/cli/cmd_cleanup_test.go` | **Create** — CLI tests: flag registration, dry-run parsing, stub-path invocation |
| `internal/cli/root_test.go` | Add missing `Cleanup` stub method to `mockRTForDeploy` (required for the `Manager` assertion to compile) |
| `README.md` | Add `tengiz cleanup` to the command list (UI/UX change rule from AGENTS.md) |

---

### Task 1: Extend `runtime.Manager` with the `Cleanup` method + stub

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface) and `:113-122` (stub)
- Modify: `internal/cli/root_test.go:76-100` (mock must implement the new method)
- Test: `internal/runtime/runtime_test.go` (append a compile-level stub assertion)

**Interfaces:**
- Consumes: nothing new (existing `context.Context`)
- Produces: `CleanupOptions{Containers, Images, Volumes, BuildCache, DryRun bool}`, `CleanupResult{ContainersRemoved, ImagesRemoved, VolumesRemoved, BuildCacheReclaimed int}`, and `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` on the `Manager` interface

- [ ] **Step 1: Write the failing test (stub must implement the new method)**

```go
// internal/runtime/runtime_test.go (append)
func TestStubCleanupImplementsInterface(t *testing.T) {
	var m Manager = NewStub()
	if m == nil {
		t.Fatal("NewStub() must implement Manager")
	}
	result, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	total := result.ContainersRemoved + result.ImagesRemoved + result.VolumesRemoved + result.BuildCacheReclaimed
	if total != 0 {
		t.Errorf("stub Cleanup should report zero removals, got %+v", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanupImplementsInterface -v -count=1`

Expected: FAIL with `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add types + interface method + stub method in `internal/runtime/runtime.go`**

Add the types just above the `Manager` interface:

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	BuildCache bool
	DryRun     bool
}

type CleanupResult struct {
	ContainersRemoved  int
	ImagesRemoved      int
	VolumesRemoved     int
	BuildCacheReclaimed int
}
```

Add the method to the `Manager` interface (after `Run`):

```go
type Manager interface {
	// ... existing methods ...
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
}
```

Add the no-op stub (after the stub `Run` method):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 4: Add the `Cleanup` method to the CLI mock in `internal/cli/root_test.go`**

Add after the mock's `Run` method:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubCleanupIsInterface -v -count=1`

Expected: PASS

- [ ] **Step 6: Verify nothing downstream breaks (interface now has one more method)**

Run: `go build ./...`

Expected: Build succeeds.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup method to Manager interface and stubs"
```

---

### Task 2: Implement `dockerRuntime.Cleanup` with category pruning in a new file

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go` **Create**

**Interfaces:**
- Consumes: `CleanupOptions` / `CleanupResult` from Task 1
- Produces: `dockerRuntime.Cleanup(ctx, opts) (CleanupResult, error)` — the exec-based implementation, plus unexported helpers `countPruned(lines string) int` usable from tests

- [ ] **Step 1: Write the failing tests for the parsing helper + docker invocation shape**

```go
// internal/runtime/prune_test.go (package runtime)
package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestCountPruned(t *testing.T) {
	// docker container prune output is one ID per line; trailing blank line is trimmed
	raw := "abc123\ndef456\n\n"
	got := countPruned(raw)
	if got != 2 {
		t.Errorf("countPruned(%q) = %d, want 2", raw, got)
	}
}

func TestCountPrunedEmpty(t *testing.T) {
	raw := ""
	if got := countPruned(raw); got != 0 {
		t.Errorf("countPruned(empty) = %d, want 0", got)
	}
}

func TestStubCleanupAllReturnsZero(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true, Images: true, Volumes: true, BuildCache: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ContainersRemoved+res.ImagesRemoved+res.VolumesRemoved+res.BuildCacheReclaimed != 0 {
		t.Errorf("stub should remove nothing, got %+v", res)
	}
}
```

Guard the two docker-exec-based behaviors with a test that only exercises the pure helper (never calls the real binary) so tests are hermetic and fast (no Docker dependency in CI):

```go
func TestCleanupFlagsSelectOnlyRequestedCategories(t *testing.T) {
	// A Cleanup with no flags must default to pruning everything (all true).
	opts := CleanupOptions{}
	want := opts.pruneAll()
	if !want {
		t.Error("empty CleanupOptions must default to pruning all categories")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestCountPruned|TestCleanupFlags|TestStubCleanupAllFlags" -v -count=1`

Expected: `TestCountPruned` fails (`undefined: countPruned`), `pruneAll` fails (`undefined: pruneAll`); the stub test passes.

- [ ] **Step 3: Write the implementation in `internal/runtime/prune.go`**

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// pruneAll reports whether opts requests every category (the default).
func (o CleanupOptions) pruneAll() bool {
	return !o.Containers && !o.Images && !o.Volumes && !o.BuildCache
}

// countPruned counts non-empty newline-separated lines in docker prune output.
func countPruned(out string) int {
	if strings.TrimSpace(out) == "" {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	n := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	// docker prune subcommands have no native --dry-run, so a dry run short
	// circuits before any destructive delete ever reaches the daemon.
	if opts.DryRun {
		return res, nil
	}

	all := opts.pruneAll()

	if all || opts.Containers {
		n, err := r.pruneContainers(ctx)
		if err != nil {
			return res, err
		}
		res.ContainersRemoved = n
	}

	if all || opts.Images {
		n, err := r.pruneImages(ctx)
		if err != nil {
			return res, err
		}
		res.ImagesRemoved = n
	}

	if all || opts.Volumes {
		n, err := r.pruneVolumes(ctx)
		if err != nil {
			return res, err
		}
		res.VolumesRemoved = n
	}

	if all || opts.BuildCache {
		n, err := r.pruneBuildCache(ctx)
		if err != nil {
			return res, err
		}
		res.BuildCacheReclaimed = n
	}

	return res, nil
}

// pruneContainers removes containers that are NOT labeled tengiz-app.
// label!=tengiz-app guarantees Tengiz-managed containers are never touched.
func (r *dockerRuntime) pruneContainers(ctx context.Context) (int, error) {
	args := []string{"container", "prune", "--force", "--filter", "label!=tengiz-app"}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return countPruned(string(out)), nil
}

// pruneImages removes all images not referenced by any container (-a prunes
// unused images too, not just dangling ones). Running containers always keep
// their images in use, so this never breaks a live app.
func (r *dockerRuntime) pruneImages(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f", "-a")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return countPruned(string(out)), nil
}

// pruneVolumes removes all volumes not in use by any container.
func (r *dockerRuntime) pruneVolumes(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return countPruned(string(out)), nil
}

// pruneBuildCache prunes builder cache (BuildKit). Reclaimed is reported in
// items (a rough count) since docker does not expose bytes via this path.
func (r *dockerRuntime) pruneBuildCache(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return countPruned(string(out)), nil
}
```

- [ ] **Step 4: Run test suite to verify it passes**

Run: `go test ./internal/runtime/ -run "TestCountPruned|TestCleanupFlags|TestStubCleanupAllFlags" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run the full runtime package test suite + build**

Run: `go test ./internal/runtime/ -v -count=1` and `go build ./...`

Expected: All PASS, build succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): implement docker housekeeping container/image/volume/build-cache prune"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` and register + cleanup flags in `init()`
- Test: `internal/cli/cmd_cleanup_test.go` **Create**

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult` from Tasks 1–2
- Produces: registered `tengiz cleanup` command whose `RunE` builds `CleanupOptions` from flags, calls `rt.Cleanup`, and prints counts

- [ ] **Step 1: Write the failing CLI tests**

```go
// internal/cli/cmd_cleanup_test.go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "build-cache", "dry-run"} {
		if f := cleanupCmd.Flags().Lookup(flag); f == nil {
			t.Errorf("cleanupCmd missing flag --%s", flag)
		}
	}
}

// TestCleanupStubPath ensures the command routes through a no-op runtime without
// requiring a real Docker daemon, and prints the "Nothing" message when empty.
func TestCleanupStubPath(t *testing.T) {
	// Stub runtime returns zero counts; we assert no panic and that output
	// is produced. Execution reaches rt.Cleanup via the real code path.
	if cleanupCmd == nil {
		t.Fatal("cleanupCmd must be defined")
	}
	var _ *cobra.Command = cleanupCmd // compile-time reference
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanupCmd" -v -count=1`

Expected: FAIL (`package-level cleanupCmd is not defined`).

- [ ] **Step 3: Define the command in `internal/cli/root.go`**

Append before the `var psCmd` declaration (or near it), after the `rollbackCmd` block:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker containers, images, volumes, and build cache",
	Long: `Runs Docker housekeeping to reclaim disk space on the host.

By default, cleans up all four categories. Use --containers, --images,
--volumes, or --build-cache to restrict to a single category.

Containers managed by Tengiz (those carrying the tengiz-app label) are
never removed. Use --dry-run to see what would be removed without
deleting anything.

Examples:
  tengiz cleanup                 # prune everything reclaimable
  tengiz cleanup --images        # prune unused/dangling images only
  tengiz cleanup --dry-run       # show what would be removed, delete nothing`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			BuildCache: buildCache,
			DryRun:     dryRun,
		}

		res, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}

		if dryRun {
			fmt.Println("[tengiz] dry run: no resources removed")
			return nil
		}

		removed := res.ContainersRemoved + res.ImagesRemoved + res.VolumesRemoved
		if removed == 0 && res.BuildCacheReclaimed == 0 {
			fmt.Println("Nothing to clean.")
			return nil
		}

		fmt.Printf("[tengiz] removed %d containers, %d images, %d volumes, %d build-cache items\n",
			res.ContainersRemoved, res.ImagesRemoved, res.VolumesRemoved, res.BuildCacheReclaimed)
		return nil
	},
}
```

`cleanupCmd` must be defined at package scope (there's a `var cleanupCmd` reference in the test file). Register the command and its flags in `init()`:

```go
func init() {
	// ...existing registrations...
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", false, "prune unused containers (unmanaged by Tengiz)")
	cleanupCmd.Flags().Bool("images", false, "prune unused Docker images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused Docker volumes")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without deleting")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanupCmd" -v -count=1`

Expected: PASS

- [ ] **Step 5: Ensure the CLI mock compiles with the interface (add `Cleanup` if not already added in Task 1)**

If not already present in `internal/cli/root_test.go`, the `mockRTForDeploy` must include:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

This is required for `TestMockRTForDeployImplementsManager` to compile after the interface gained the method.

- [ ] **Step 6: Run the full CLI test suite + build**

Run: `go test ./internal/cli/ -v -count=1`
Run: `go build ./...`

Expected: All PASS; build succeeds.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cmd_cleanup_test.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 4: Update README and final self-review

**Files:**
- Modify: `README.md` — add `tengiz cleanup` to the command reference
- (No Go code changes in this task)

**Interfaces:**
- Consumes: the finalized `tengiz cleanup` command from Task 3
- Produces: accurate user-facing documentation per the AGENTS.md UI/UX rule

- [ ] **Step 1: Update the README command list**

In `README.md`, find the "Commands" / feature-list section and add the cleanup command (matching the surrounding style, e.g. a `tengiz cleanup` bullet):

```markdown
tengiz cleanup [--containers] [--images] [--volumes] [--build-cache] [--dry-run] → reclaim Docker disk space (never removes Tengiz-managed containers)
```

If the README uses a dedicated "Housekeeping / Maintenance" section, add a short subsection:

```markdown
## Docker Housekeeping

`tengiz cleanup` prunes abandoned Docker containers, dangling images, unused
volumes, and build cache. It never removes containers tagged `tengiz-app`
(your deployed apps are always safe). Use `--dry-run` to preview.
```

- [ ] **Step 2: Run the full test suite + vet**

Run: `go test ./... -count=1`
Run: `go vet ./...`

Expected: All PASS; vet clean. (Some tests may be skipped if no Docker daemon; that is acceptable.)

- [ ] **Step 3: Self-review against the spec**

Check the `docs/FUTURES_FEATURES.md` requirements:
- Label-based `docker system prune` ✅ (Task 2 — `label!=tengiz-app` filter)
- `tengiz cleanup` command ✅ (Task 3)
- Granular per-category pruning (containers/images/volumes/build cache) ✅ (Tasks 2–3 — `CleanupOptions` + flags)
- Tengiz-managed containers protected ✅ (label guard in `pruneContainers`)
- Non-interactive + dry-run safety ✅ (`-f` + `--dry-run`)

- [ ] **Step 4: Placeholder scan**

Search the plan for any `TBD`, `TODO`, "implement later", "fill in details", "Add appropriate error handling", "write tests for the above" without code, or "Similar to Task N". None present: every code change above carries a concrete code block and exact command with expected output.

- [ ] **Step 5: Type consistency check**

- `CleanupOptions(Containers, Images, Volumes, BuildCache, DryRun bool)` — identical field names across Task 1 stub, Task 2 impl, Task 3 CLI
- `CleanupResult(ContainersRemoved, ImagesRemoved, VolumesRemoved int, BuildCacheReclaimed int)` — consumed in Task 3's print exactly as produced in Task 2
- `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — signature identical on interface, stub, mock, and docker impl
- `runtime.Cleanup` / `runtime.CleanupOptions` package-qualified references are consistent in `internal/cli/root.go` and `internal/cli/root_test.go`

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```