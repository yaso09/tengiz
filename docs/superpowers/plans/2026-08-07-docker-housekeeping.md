# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (images, volumes, build cache) while label-filtering so Tengiz-managed containers are always preserved.

**Architecture:** Extend the existing `runtime.Manager` interface with a `SystemCleanup` and `PruneBuildCache` method implemented on `dockerRuntime` via `exec`-based `docker` CLI calls. Command construction lives behind pure, unit-testable helper functions (`runtime.SystemPruneArgs`, `runtime.BuildCachePruneArgs`) following the existing args-builder pattern (`buildLogArgs`, `buildRunArgs`). A new Cobra `cleanupCmd` wires flags to the runtime and renders a dry-run plan without touching the Docker CLI.

**Tech Stack:** Go 1.26, `spf13/cobra`, `os/exec` (Docker CLI). No new dependencies. Matches existing `internal/runtime` conventions.

## Global Constraints

- Go module: `github.com/yaso09/tengiz`, Go 1.26. Single binary entry `main.go`.
- No Docker SDK — the runtime calls the `docker` CLI via `os/exec`. Docker must be installed separately; `runtime.NewDocker()` returns an error if `docker` is not on PATH.
- Tengiz-managed containers are labeled `tengiz-app=<appname>` (constant `labelKey` in `internal/runtime/docker.go:76`). Any prune filter MUST preserve objects carrying this label.
- Do not restructure the existing `runtime.Manager` interface beyond adding the two new methods; keep method signatures stable across tasks.
- All new tests go in the same package as the code under test (white-box), matching `internal/runtime/runtime_test.go` conventions.
- After implementing, run `go build ./...`, `go vet ./...`, and `go test ./... -count=1`; all must pass.
- Update `README.md` and `AGENTS.md` when adding a CLI command (project rule). Mark the feature implemented in `docs/FUTURES_FEATURES.md`.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/runtime/prune.go` (create) | `PruneOptions` type; pure arg builders `SystemPruneArgs`, `BuildCachePruneArgs`; `dockerRuntime.SystemCleanup`, `dockerRuntime.PruneBuildCache`. |
| `internal/runtime/prune_test.go` (create) | Unit tests for the pure arg builders (no Docker CLI). |
| `internal/runtime/cleanup.go` (modify) | (None needed — new methods live in `prune.go`.) |
| `internal/runtime/runtime.go` (modify) | Add `SystemCleanup` + `PruneBuildCache` to the `Manager` interface and to `stubManager`. |
| `internal/runtime/cleanup_test.go` (modify) | Stub tests for the two new interface methods. |
| `internal/cli/root.go` (modify) | Register `cleanupCmd`, define flags, wire runtime calls and dry-run plan output. |
| `internal/cli/cmd_cleanup_test.go` (create) | CLI registration, flag, and dry-run output tests. |
| `internal/cli/root_test.go` (modify) | Add the two methods to `mockRTForDeploy` so the mock keeps satisfying `Manager`. |
| `internal/proxy/proxy_test.go`, `internal/idle/idle_test.go` (modify) | Add the two methods to their `mockRuntime` types (both implement `Manager`). |
| `README.md`, `AGENTS.md`, `docs/FUTURES_FEATURES.md` (modify) | Document the new command; mark feature implemented. |

---

### Task 1: Pure prune-arg builders

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: package constant `labelKey` already defined in `internal/runtime/docker.go:76` (`"tengiz-app"`).
- Produces: `type PruneOptions struct { All bool; Volumes bool; DryRun bool }`; `SystemPruneArgs(opts PruneOptions) []string`; `BuildCachePruneArgs() []string`. Task 2's docker methods and Task 3's CLI use these exact signatures.

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"reflect"
	"testing"
)

func TestSystemPruneArgsDefaults(t *testing.T) {
	got := SystemPruneArgs(PruneOptions{})
	want := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SystemPruneArgs() = %v, want %v", got, want)
	}
}

func TestSystemPruneArgsAllAndVolumes(t *testing.T) {
	got := SystemPruneArgs(PruneOptions{All: true, Volumes: true})
	want := []string{"system", "prune", "-f", "-a", "--volumes", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SystemPruneArgs() = %v, want %v", got, want)
	}
}

func TestSystemPruneArgsIgnoresDryRun(t *testing.T) {
	got := SystemPruneArgs(PruneOptions{DryRun: true})
	want := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SystemPruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildCachePruneArgs(t *testing.T) {
	got := BuildCachePruneArgs()
	want := []string{"builder", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildCachePruneArgs() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestSystemPruneArgs -v -count=1`
Expected: build error — `undefined: PruneOptions`, `undefined: SystemPruneArgs`, `undefined: BuildCachePruneArgs`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
)

type PruneOptions struct {
	All     bool
	Volumes bool
	DryRun  bool
}

func SystemPruneArgs(opts PruneOptions) []string {
	args := []string{"system", "prune", "-f"}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	return append(args, "--filter", fmt.Sprintf("label!=%s", labelKey))
}

func BuildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func (r *dockerRuntime) SystemCleanup(ctx context.Context, opts PruneOptions) error {
	if opts.DryRun {
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", SystemPruneArgs(opts)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", BuildCachePruneArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}
```

The `SystemCleanup`/`PruneBuildCache` methods are added here but not yet wired into the interface — they compile because `dockerRuntime` already exists. The interface additions come in Task 2.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestSystemPruneArgs|TestBuildCachePruneArgs' -v -count=1`
Expected: 4 PARS — the four `Test*` functions pass.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: add prune arg builders for docker housekeeping"
```

---

### Task 2: Extend the `Manager` interface

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface) and `internal/runtime/runtime.go:113-119` (stub)
- Modify: `internal/runtime/cleanup_test.go` (add stub tests)
- Modify: `internal/cli/root_test.go` (mock `mockRTForDeploy`, around line 99)
- Modify: `internal/proxy/proxy_test.go` (mock `mockRuntime`, around line 34)
- Modify: `internal/idle/idle_test.go` (mock `mockRuntime`, around line 33)

**Interfaces:**
- Consumes: `PruneOptions` from Task 1.
- Produces: `Manager` now requires `SystemCleanup(ctx context.Context, opts PruneOptions) error` and `PruneBuildCache(ctx context.Context) error`. All mocks and the stub must implement them.

- [ ] **Step 1: Write the failing test**

X Add stub tests to `internal/runtime/cleanup_test.go`:

```go
func TestStubSystemCleanup(t *testing.T) {
	m := NewStub()
	if err := m.SystemCleanup(context.Background(), PruneOptions{}); err != nil {
		t.Fatalf("SystemCleanup() error = %v", err)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	if err := m.PruneBuildCache(context.Background()); err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubSystemCleanup -v -count=1`
Expected: build error `does not implement Manager (missing method SystemCleanup)`.

- [ ] **Step 3: Write minimal implementation**

3a. Add both methods to the `Manager` interface in `internal/runtime/runtime.go`. Insert after the `KeepLastNImages` line (currently line 36):

```go
	SystemCleanup(ctx context.Context, opts PruneOptions) error
	PruneBuildCache(ctx context.Context) error
```

3b. Add both methods to `stubManager` in the same file (after the existing `KeepLastNImages` stub at line 117):

```go
func (m *stubManager) SystemCleanup(ctx context.Context, opts PruneOptions) error {
	return nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context) error {
	return nil
}
```

3c. `dockerRuntime` already implements both methods from Task 1 — no change needed there.

3d. Update the three mock types that also implement `Manager` so the package set compiles.

In `internal/cli/root_test.go` (add after the `KeepLastNImages` line 99):

```go
func (m *mockRTForDeploy) SystemCleanup(ctx context.Context, opts runtime.PruneOptions) error { return nil }
func (m *mockRTForDeploy) PruneBuildCache(ctx context.Context) error { return nil }
```

In `internal/proxy/proxy_test.go` (add after the `KeepLastNImages` line 34):

```go
func (m *mockRuntime) SystemCleanup(ctx context.Context, opts runtime.PruneOptions) error { return nil }
func (m *mockRuntime) PruneBuildCache(ctx context.Context) error { return nil }
```

In `internal/idle/idle_test.go` (add after the `KeepLastNImages` line 33):

```go
func (m *mockRuntime) SystemCleanup(ctx context.Context, opts runtime.PruneOptions) error { return nil }
func (m *mockRuntime) PruneBuildCache(ctx context.Context) error { return nil }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: build succeeds, vet clean, all tests pass (the stub tests from Step 1 now pass).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add SystemCleanup and PruneBuildCache to runtime Manager"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go:46` (register command), `internal/cli/root.go:88` (add flags in `init()`)
- Test: `internal/cli/cmd_cleanup_test.go` (create)

**Interfaces:**
- Consumes: `runtime.SystemPruneArgs`, `runtime.BuildCachePruneArgs`, `runtime.NewDocker`, `Manager.SystemCleanup`, `Manager.PruneBuildCache` from Tasks 1–2.
- Produces: registered `cleanup` command with flags `--all`, `--volumes`, `--build-cache`, `--dry-run`.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cmd_cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"all", "volumes", "build-cache", "dry-run"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing flag --%s", flag)
		}
	}
}

func TestCleanupDryRunOutputSystemPrune(t *testing.T) {
	out := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
		if err := rootCmd.Execute(); err != nil {
			t.Errorf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(out, "docker system prune") {
		t.Errorf("dry-run output missing 'docker system prune', got: %s", out)
	}
	if !strings.Contains(out, "label!=tengiz-app") {
		t.Errorf("dry-run output missing label filter, got: %s", out)
	}
}

func TestCleanupDryRunWithBuildCache(t *testing.T) {
	out := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--build-cache"})
		if err := rootCmd.Execute(); err != nil {
			t.Errorf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(out, "docker builder prune") {
		t.Errorf("dry-run output missing 'docker builder prune', got: %s", out)
	}
}
```

Note: dry-run paths must NOT call `runtime.NewDocker()`, because Docker may be absent in the test environment.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`
Expected: FAIL. `TestCleanupCommandRegistered` fails with "cleanup command not found" (command not registered), and the other tests fail because `cleanupCmd` is undefined at compile time.

- [ ] **Step 3: Write minimal implementation**

3a. Add the command definition. Place it in `internal/cli/root.go` just above `var domainCmd = &cobra.Command{` (the block that starts around line 785). Insert:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources and images",
	Long: `Remove Docker resources left behind by builds and deploys.

Safe by default: the system prune runs with a label filter that preserves
every container managed by Tengiz (those labeled tengiz-app). Only stopped,
unused resources are removed.

Flags:
  --all         remove all unused images, not just dangling ones
  --volumes     remove unused volumes
  --build-cache remove the Docker build cache
  --dry-run     print the docker commands that would run, without running them`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts := runtime.PruneOptions{All: all, Volumes: volumes, DryRun: dryRun}

		plan := []string{"docker " + strings.Join(runtime.SystemPruneArgs(opts), " ")}
		if buildCache {
			plan = append(plan, "docker "+strings.Join(runtime.BuildCachePruneArgs(), " "))
		}

		if dryRun {
			fmt.Println("[tengiz] dry run — would execute:")
			for _, line := range plan {
				fmt.Printf("  %s\n", line)
			}
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if err := rt.SystemCleanup(cmd.Context(), opts); err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		if buildCache {
			if err := rt.PruneBuildCache(cmd.Context()); err != nil {
				return fmt.Errorf("build cache cleanup: %w", err)
			}
		}
		fmt.Println("[tengiz] done pruning unused Docker resources")
		return nil
	},
}
```

`internal/cli/root.go` already imports `runtime`, `strings`, and `fmt`, so no import changes are needed.

3b. Register the command in `init()` after `rootCmd.AddCommand(runCmd)` (around line 67). Insert:

```go
	rootCmd.AddCommand(cleanupCmd)
```

3c. Add the flags in `init()`. Append after the webhook flag definitions (around line 88):

```go
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("build-cache", false, "remove Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "print commands without running them")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`
And a full pass: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: the four new tests pass; full suite and vet pass.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/cmd_cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md` (add a `tengiz cleanup` section after the `tengiz rollback` block, line ~237)
- Modify: `AGENTS.md` (add a `tengiz cleanup` line to the CLI list)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 implemented)

**Interfaces:** None — documentation-only task.

- [ ] **Step 1: Write the documentation**

In `README.md`, after the `### tengiz rollback <app>` section (ends with the table at line 236, before `### tengiz domain` at line 238), insert:

```markdown
### `tengiz cleanup`

Remove unused Docker resources (dangling images, stopped containers, build cache, volumes) to reclaim disk space. The prune runs with a label filter that preserves every Tengiz-managed container, so running apps are never touched.

| Flag | Description |
|------|-------------|
| `--all` | Remove all unused images, not just dangling ones |
| `--volumes` | Remove unused volumes |
| `--build-cache` | Remove the Docker build cache |
| `--dry-run` | Print the docker commands that would run, without running them |

Example:

```bash
tengiz cleanup --dry-run          # preview what would be removed
tengiz cleanup --all --volumes    # full housekeeping
tengiz cleanup --build-cache      # just the build cache
```
```

In `AGENTS.md`, under the CLI command list (after the `tengiz rollback <app>` line), add:

```
tengiz cleanup [--all] [--volumes] [--build-cache] [--dry-run] → prune unused Docker resources (label-safe)
```

In `docs/FUTURES_FEATURES.md`, move feature **#6 Docker Housekeeping** behavior note: in the P0 table, change the row's marker from `⬜` to `✅` (edit row `| 6 | **Docker Housekeeping** ⬜ |` to `| 6 | **Docker Housekeeping** ✅ |`), and add a line to the "✅ Implemented Features" table:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-07) |
```

- [ ] **Step 2: Verify docs render / no build impact**

Run: `go build ./...` (should be unaffected) and visually confirm the README section renders.
Expected: build passes; markdown blocks balanced (each ``` opened has a closing ```).

- [ ] **Step 3: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage (FUTURES_FEATURES.md #6 — Docker Housekeeping, P0):**
- "Label-based `docker system prune`" → Task 1 (`labelKey` filter via `label!=tengiz-app`) and Task 3 wiring. ✅
- "`tengiz cleanup`" → Task 3 CLI command. ✅
- Disk-space management (the stated motivation) → `--all`, `--volumes`, and `--build-cache` flags. ✅
- Docs requirement (AGENTS.md rule "UI/UX go değişikliklerinde dokümantasyon güncelle") → Task 4. ✅
- Preserving running apps → label filter everywhere; `DryRun` short-circuits before any docker call. ✅

**2. Placeholder scan:** No "TBD/TODO", no "add error handling" without code, no "similar to Task N" references; every code step contains full, copyable code. Flag putted ones are declared in exactly their producing task.

**3. Type consistency:** `PruneOptions{All, Volumes, DryRun}`, `SystemPruneArgs(opts PruneOptions) []string`, `BuildCachePruneArgs() []string`, `SystemCleanup(ctx, opts) error`, `PruneBuildCache(ctx) error` are defined once in Task 1/provided and used identically in Tasks 2–3. The three mock types (`mockRTForDeploy`, two `mockRuntime`) plus `stubManager` are all updated in Task 2, so the interface extension compiles. `cleanupCmd` (package-level var) is referenced consistently in `root.go` and `cmd_cleanup_test.go`.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-08-07-docker-housekeeping.md`. Two execution options:**

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**