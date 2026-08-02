# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely prunes unused Docker resources (stopped foreign containers, dangling/unused images, unused networks, anonymous volumes) via label-based `docker system prune`, while never removing Tengiz-managed resources.

**Architecture:** A new `Prune(ctx, opts PruneOptions) (PruneResult, error)` method on the `runtime.Manager` interface, implemented in `internal/runtime/cleanup.go` by building docker CLI args through a pure `pruneArgs(opts)` helper (mirrors the existing `resourceArgs`/`buildLogArgs` pattern). The safety model is `docker system prune --force --filter "label!=tengiz-app"`: Docker only prunes resources that do **not** carry the `tengiz-app` label, and every Tengiz container/image is labeled `tengiz-app=<app>`, so idle/stopped apps and rollback images are preserved. `--dry-run` lists candidate resources via `docker ps -a`/`docker images`/`docker volume ls`/`docker network ls` without mutating anything. A new `internal/cli/cleanup.go` registers the cobra command and exposes a pure `runCleanup(rt, opts)` function so the CLI is unit-testable without a live Docker daemon.

**Tech Stack:** Go 1.26, cobra, `os/exec` (docker CLI). No new external dependencies.

## Global Constraints

- Prune command must ALWAYS include `--filter label!=tengiz-app` — resources with the `tengiz-app` label are Tengiz-managed and must never be removed
- Actual prune must ALWAYS include `--force` (non-interactive; the process has no TTY)
- `--dry-run` must never mutate Docker state (no `docker system prune` is invoked in dry-run mode)
- Default prune removes only **dangling** images; `--all`/`-a` extends to all unused images; `--volumes` adds anonymous-volume pruning (Tengiz uses host-path bind mounts, so no Tengiz data is at risk)
- Cleanup is environment-agnostic (applies to all `--env` environments); it does not read the `--env` flag
- Adding `Prune` to the `runtime.Manager` interface requires updating every implementation in the same commit: `dockerRuntime`, `stubManager` (`internal/runtime/runtime.go`), and test mocks `mockRTForDeploy` (`internal/cli/root_test.go`), `mockRuntime` (`internal/proxy/proxy_test.go`), `mockRuntime` (`internal/idle/idle_test.go`) — otherwise the repo will not compile
- No new external dependencies
- All existing tests must continue to pass
- Documentation must be updated: `README.md`, `AGENTS.md`, `docs/FUTURES_FEATURES.md`

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneResult` types; add `Prune` to `Manager` interface; stub implementation |
| `internal/runtime/cleanup.go` | `dockerRuntime.Prune` + `pruneArgs(opts)` helper + `dockerOutput` helper + dry-run listing |
| `internal/runtime/cleanup_test.go` | Tests for `pruneArgs` and stub `Prune` |
| `internal/cli/cleanup.go` | `cleanupCmd` cobra command + `runCleanup` + `formatCleanupResult` |
| `internal/cli/cleanup_test.go` | CLI tests using `mockRTForDeploy` (no Docker needed) |
| `internal/cli/root.go` | Register `cleanupCmd` + its flags in `init()` |
| `internal/cli/root_test.go` | Add `Prune` (and `pruned`/`pruneOpts`/`pruneErr` fields) to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `README.md` | Features bullet + `### tengiz cleanup` CLI Reference section |
| `AGENTS.md` | CLI command list + `runtime.Manager` architecture row |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as Implemented |

---

### Task 1: Extend the `runtime.Manager` interface with the Prune API

Add `PruneOptions`, `PruneResult`, the `Manager.Prune` method, and the stub implementation, then update all test mocks so the repo compiles and every existing test still passes.

**Files:**
- Modify: `internal/runtime/runtime.go:18-49` (types + interface), `internal/runtime/runtime.go:113-119` (stub, near `KeepLastNImages`)
- Modify: `internal/cli/root_test.go:69-100` (mockRTForDeploy)
- Modify: `internal/proxy/proxy_test.go:15-35` (mockRuntime)
- Modify: `internal/idle/idle_test.go:14-34` (mockRuntime)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{All, Volumes, DryRun bool}`, `runtime.PruneResult{DryRun bool; Output string}`, `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`. Later tasks rely on these exact names and types.

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go` (after the existing `TestStubKeepLastNImages`):

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.DryRun {
		t.Error("Prune() result DryRun = true, want false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -count=1`
Expected: FAIL — `m.Prune undefined (type Manager has no field or method Prune)`. The `Manager` interface has no `Prune` method yet.

- [ ] **Step 3: Add the types and interface method**

In `internal/runtime/runtime.go`, add after the `RunOptions` struct (around line 29):

```go
type PruneOptions struct {
	All     bool // remove all unused images, not just dangling ones
	Volumes bool // also prune anonymous volumes
	DryRun  bool // list resources instead of removing them
}

type PruneResult struct {
	DryRun bool
	Output string
}
```

In the `Manager` interface, add `Prune` after `KeepLastNImages` (line 36):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

- [ ] **Step 4: Add the stub implementation**

In `internal/runtime/runtime.go`, add after the stub `KeepLastNImages` (line 119):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}
```

- [ ] **Step 5: Update the test mocks so the repo compiles**

The interface change breaks three test packages. Update each mock with a `Prune` method.

In `internal/cli/root_test.go`, extend the `mockRTForDeploy` struct (line 69) with three new fields:

```go
type mockRTForDeploy struct {
	created   atomic.Int32
	removed   atomic.Int32
	started   atomic.Int32
	stopped   atomic.Int32
	pruned    atomic.Int32
	pruneOpts runtime.PruneOptions
	pruneErr  error
}
```

Add the method after `KeepLastNImages` (line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	m.pruned.Add(1)
	m.pruneOpts = opts
	if m.pruneErr != nil {
		return runtime.PruneResult{}, m.pruneErr
	}
	return runtime.PruneResult{DryRun: opts.DryRun, Output: "Deleted Containers: abc123\n\nTotal reclaimed space: 1.2GB"}, nil
}
```

In `internal/proxy/proxy_test.go`, add after `KeepLastNImages` (line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

In `internal/idle/idle_test.go`, add after `KeepLastNImages` (line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 6: Run the full test suite to verify everything passes**

Run: `go test ./... -count=1`
Expected: PASS for `TestStubPrune` and all pre-existing tests (the interface-satisfaction tests `TestStubSatisfiesInterface` and `TestMockRTForDeployImplementsManager` still pass).

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add Prune API to Manager interface"
```

---

### Task 2: Implement `dockerRuntime.Prune` with `pruneArgs` and dry-run

Implement the actual Docker integration: a pure `pruneArgs(opts)` arg-builder (unit-testable, mirrors `resourceArgs`), the `dockerRuntime.Prune` method, a `dockerOutput` helper for raw docker output, and the dry-run listing.

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult`, `Manager.Prune` from Task 1
- Produces: `pruneArgs(opts PruneOptions) []string`, `(*dockerRuntime).Prune(ctx, opts) (PruneResult, error)`, `(*dockerRuntime).dockerOutput(ctx, args ...string) (string, error)` (unexported helpers; the CLI only uses the `Manager` interface)

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go` (match the element-by-element comparison style of `TestResourceArgs` in `runtime_test.go`):

```go
func TestPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		opts PruneOptions
		want []string
	}{
		{
			name: "default",
			opts: PruneOptions{},
			want: []string{"system", "prune", "--force", "--filter", "label!=tengiz-app"},
		},
		{
			name: "all",
			opts: PruneOptions{All: true},
			want: []string{"system", "prune", "--force", "--filter", "label!=tengiz-app", "--all"},
		},
		{
			name: "volumes",
			opts: PruneOptions{Volumes: true},
			want: []string{"system", "prune", "--force", "--filter", "label!=tengiz-app", "--volumes"},
		},
		{
			name: "all and volumes",
			opts: PruneOptions{All: true, Volumes: true},
			want: []string{"system", "prune", "--force", "--filter", "label!=tengiz-app", "--all", "--volumes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneArgs(tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("pruneArgs(%+v) = %v (len=%d), want %v (len=%d)", tt.opts, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("pruneArgs(%+v)[%d] = %q, want %q", tt.opts, i, got[i], tt.want[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestPruneArgs -count=1`
Expected: FAIL — `pruneArgs undefined (type PruneOptions has no field or method pruneArgs)` (compile error: `undefined: pruneArgs`).

- [ ] **Step 3: Implement `pruneArgs`**

Add at the top of `internal/runtime/cleanup.go` (imports `fmt`, `log`, `os/exec`, `sort`, `strings`, `context` are already present):

```go
func pruneArgs(opts PruneOptions) []string {
	args := []string{"system", "prune", "--force", "--filter", "label!=tengiz-app"}
	if opts.All {
		args = append(args, "--all")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	return args
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestPruneArgs -count=1`
Expected: PASS.

- [ ] **Step 5: Implement `Prune`, `dockerOutput`, and the dry-run listing**

Add to `internal/runtime/cleanup.go` after `pruneArgs`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	if opts.DryRun {
		return r.pruneDryRun(ctx, opts)
	}
	cmd := exec.CommandContext(ctx, "docker", pruneArgs(opts)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneResult{}, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return PruneResult{DryRun: false, Output: string(out)}, nil
}

func (r *dockerRuntime) dockerOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) pruneDryRun(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var b strings.Builder
	b.WriteString("Resources that would be removed (dry run):\n")
	written := false

	add := func(title, output string) {
		if strings.TrimSpace(output) == "" {
			return
		}
		if !written {
			b.WriteString("\n")
			written = true
		}
		b.WriteString(title)
		b.WriteString("\n")
		b.WriteString(output)
		b.WriteString("\n")
	}

	containers, err := r.dockerOutput(ctx, "ps", "-a",
		"--filter", "status=exited",
		"--filter", "label!=tengiz-app",
		"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}")
	if err != nil {
		return PruneResult{}, err
	}
	add("Stopped containers:", containers)

	images, err := r.dockerOutput(ctx, "images",
		"--filter", "dangling=true",
		"--format", "{{.ID}}\t{{.Repository}}:{{.Tag}}")
	if err != nil {
		return PruneResult{}, err
	}
	add("Dangling images:", images)

	if opts.All {
		unused, err := r.dockerOutput(ctx, "images",
			"--filter", "label!=tengiz-app",
			"--format", "{{.ID}}\t{{.Repository}}:{{.Tag}}")
		if err != nil {
			return PruneResult{}, err
		}
		add("Unused images (--all):", unused)
	}

	if opts.Volumes {
		vols, err := r.dockerOutput(ctx, "volume", "ls",
			"--filter", "dangling=true",
			"--format", "{{.Name}}")
		if err != nil {
			return PruneResult{}, err
		}
		add("Dangling anonymous volumes:", vols)
	}

	networks, err := r.dockerOutput(ctx, "network", "ls",
		"--filter", "label!=tengiz-app",
		"--format", "{{.ID}}\t{{.Name}}")
	if err != nil {
		return PruneResult{}, err
	}
	add("Networks not managed by Tengiz (unused ones would be removed):", networks)

	if !written {
		b.WriteString("Nothing to clean.\n")
	}
	return PruneResult{DryRun: true, Output: b.String()}, nil
}
```

Note: the `--format` templates deliberately omit the `table` keyword so that empty results produce empty output and their section is skipped. The `--filter "label!=tengiz-app"` clause guarantees the listing and the prune never include Tengiz-managed containers/images/networks.

- [ ] **Step 6: Run the runtime tests to verify everything passes**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS for `TestPruneArgs`, `TestStubPrune`, and all existing runtime tests.

- [ ] **Step 7: Manual smoke test (requires Docker, optional but recommended)**

```bash
go build -o tengiz .
docker run -d --name foreign-ctr alpine sleep 300   # foreign container (no tengiz-app label)
docker create --name orphan busybox                 # stopped foreign container
./tengiz cleanup --dry-run                          # lists "Stopped containers:" including orphan
./tengiz cleanup --all --volumes                    # prunes orphan + dangling images, keeps foreign-ctr while running
docker rm -f foreign-ctr
```

Expected: `--dry-run` lists candidates and prints `[tengiz] dry run: nothing was removed.`; the real run prints docker's `Deleted Containers:`, `Total reclaimed space:` output. A running Tengiz app's container must survive the cleanup.

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker system prune with label protection and dry-run"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

Create the cobra command, a pure `runCleanup(rt, opts)` function (testable without Docker), and register command + flags.

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go:38-75` (register in `init()`)

**Interfaces:**
- Consumes: `runtime.Manager.Prune`, `runtime.PruneOptions`, `runtime.PruneResult` from Tasks 1-2
- Produces: `runCleanup(rt runtime.Manager, opts runtime.PruneOptions) (string, error)`, `formatCleanupResult(res runtime.PruneResult) string`, and the registered `cleanupCmd`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go` (mirrors the existing `TestDomainCommandsRegistered` style; uses the `mockRTForDeploy` mock from Task 1, so no Docker is required):

```go
package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	for _, name := range []string{"dry-run", "all", "volumes"} {
		if flag := cleanupCmd.Flags().Lookup(name); flag == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestRunCleanupPrintsResult(t *testing.T) {
	m := &mockRTForDeploy{}
	out, err := runCleanup(m, runtime.PruneOptions{})
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if !strings.Contains(out, "[tengiz] cleanup complete") {
		t.Errorf("output missing success header, got: %s", out)
	}
	if !strings.Contains(out, "Total reclaimed space") {
		t.Errorf("output missing docker prune result, got: %s", out)
	}
	if m.pruned.Load() != 1 {
		t.Errorf("expected Prune called once, got %d", m.pruned.Load())
	}
}

func TestRunCleanupDryRun(t *testing.T) {
	m := &mockRTForDeploy{}
	out, err := runCleanup(m, runtime.PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if !strings.Contains(out, "[tengiz] dry run") {
		t.Errorf("output missing dry-run header, got: %s", out)
	}
	if !m.pruneOpts.DryRun {
		t.Error("expected Prune called with DryRun=true")
	}
}

func TestRunCleanupAllAndVolumes(t *testing.T) {
	m := &mockRTForDeploy{}
	if _, err := runCleanup(m, runtime.PruneOptions{All: true, Volumes: true}); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if !m.pruneOpts.All || !m.pruneOpts.Volumes {
		t.Errorf("expected Prune called with All=true Volumes=true, got %+v", m.pruneOpts)
	}
}

func TestRunCleanupPropagatesError(t *testing.T) {
	m := &mockRTForDeploy{pruneErr: errors.New("docker unavailable")}
	if _, err := runCleanup(m, runtime.PruneOptions{}); err == nil {
		t.Error("expected error from runCleanup")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanup -count=1`
Expected: FAIL — `undefined: cleanupCmd` and `undefined: runCleanup`.

- [ ] **Step 3: Implement the command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long:  "Runs label-based `docker system prune`. Tengiz-managed resources (labeled `tengiz-app`) are never removed, so stopped or idle Tengiz containers, their images, and rollback images are preserved. Applies across all environments.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")

		opts := runtime.PruneOptions{All: all, Volumes: volumes, DryRun: dryRun}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		out, err := runCleanup(rt, opts)
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

func runCleanup(rt runtime.Manager, opts runtime.PruneOptions) (string, error) {
	res, err := rt.Prune(context.Background(), opts)
	if err != nil {
		return "", err
	}
	return formatCleanupResult(res), nil
}

func formatCleanupResult(res runtime.PruneResult) string {
	if res.DryRun {
		return fmt.Sprintf("[tengiz] dry run: nothing was removed.\n%s", res.Output)
	}
	return fmt.Sprintf("[tengiz] cleanup complete:\n%s", res.Output)
}
```

- [ ] **Step 4: Register the command and flags**

In `internal/cli/root.go` `init()`, after `rootCmd.AddCommand(runCmd)` (line 67), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().BoolP("all", "a", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "also prune anonymous volumes")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -count=1`
Expected: PASS for `TestCleanupCommandRegistered`, `TestCleanupFlagsRegistered`, `TestRunCleanupPrintsResult`, `TestRunCleanupDryRun`, `TestRunCleanupAllAndVolumes`, `TestRunCleanupPropagatesError`, and all existing CLI tests.

- [ ] **Step 6: Full verification**

Run: `go build -o /dev/null . && go vet ./... && go test ./... -count=1`
Expected: build success, vet clean, all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Update documentation

Document the new command and mark the feature as implemented.

**Files:**
- Modify: `README.md` (Features list ~line 23, CLI Reference ~line 418)
- Modify: `AGENTS.md` (CLI command list, `runtime.Manager` architecture row)
- Modify: `docs/FUTURES_FEATURES.md` (line 19 P0 table row #6, detailed section lines 377-381)

**Interfaces:** None — documentation only.

- [ ] **Step 1: Add a Features bullet to README.md**

In `README.md`, after the "Self-contained" bullet (line 23), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused Docker containers, images, volumes, and networks while protecting all Tengiz-managed resources.
```

- [ ] **Step 2: Add the CLI Reference section to README.md**

Add this section at the end of the `## CLI Reference` section (immediately before `## Configuration`, ~line 418):

```markdown
### `tengiz cleanup [--dry-run] [--all] [--volumes]`

Remove unused Docker resources (stopped containers, dangling images, unused networks, anonymous volumes).

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `-a`, `--all` | Also remove all unused images (not just dangling ones) |
| `--volumes` | Also prune anonymous volumes |

Runs label-based `docker system prune`. Resources labeled `tengiz-app` (all Tengiz containers and images) are never removed, so stopped/idle apps and rollback images are preserved. Safe to run anytime; reclaims disk space from orphaned build artifacts and foreign containers.
```

- [ ] **Step 3: Update AGENTS.md**

In the `runtime.Manager` architecture table row (AGENTS.md), change:

```
Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup.
```

to:

```
Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages`, `Prune` for rollback, image cleanup, and docker housekeeping.
```

In the CLI list (AGENTS.md), add after the `tengiz rollback <app>` line:

```
tengiz cleanup [--dry-run] [--all] [--volumes] → label-based docker system prune (never removes tengiz-app resources)
```

- [ ] **Step 4: Mark the feature as implemented in FUTURES_FEATURES.md**

In the P0 table (line 19), change `| 6 | **Docker Housekeeping** ⬜ |` to `| 6 | **Docker Housekeeping** ✅ |`.

In the detailed "Docker Housekeeping" section (lines 377-381), add a status line after the `- **Detected:** 2026-07-14` line:

```markdown
- **Status:** ✅ Implemented (2026-08-02)
```

- [ ] **Step 5: Verify docs changes render correctly**

Run: `git diff --stat`
Expected: 3 documentation files modified. Spot-check the README section anchors with `grep -n "tengiz cleanup" README.md AGENTS.md docs/FUTURES_FEATURES.md`.

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** (from `docs/FUTURES_FEATURES.md` P0 #6 + detailed section):

| Requirement | Task |
|-------------|------|
| "Label-based `docker system prune`" | Task 2 — `pruneArgs` always includes `--filter label!=tengiz-app` |
| "`tengiz cleanup`" CLI command | Task 3 |
| "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" (protect Tengiz-managed containers) | Global Constraint + Task 2 — `label!=tengiz-app` excludes every container/image labeled `tengiz-app=<app>` |
| Clean unused volumes/networks/containers/images | Task 2 — `--volumes`, dry-run network listing, stopped-container and dangling-image pruning |
| Reclaims disk space ("Disk space is the #1 production issue") | Task 2 + Task 3 — real prune path with reclaimed-space output |
| "Periyodik temizleme" (periodic cleanup) | Explicitly out of scope for this plan (needs a scheduler — see FUTURES #57 Background Monitoring Scheduler). Manual `tengiz cleanup` is the v1. |

**2. Placeholder scan:** No "TBD"/"TODO"/"implement later"/"similar to Task N" patterns; every code step contains complete, compilable code and every command includes expected output.

**3. Type consistency:**
- `PruneOptions{All, Volumes, DryRun bool}` and `PruneResult{DryRun bool; Output string}` are defined once in Task 1 and used identically in Tasks 2 and 3.
- `Manager.Prune(ctx, opts) (PruneResult, error)` — stub (Task 1), docker implementation (Task 2), mock (Task 1), and CLI call site `rt.Prune(context.Background(), opts)` (Task 3) all match.
- Mock fields `pruned atomic.Int32`, `pruneOpts runtime.PruneOptions`, `pruneErr error` are declared in Task 1 and consumed by Task 3 tests (`m.pruned.Load()`, `m.pruneOpts`, `m.pruneErr`) with matching names.
- `runCleanup(rt runtime.Manager, opts runtime.PruneOptions) (string, error)` and `formatCleanupResult(res runtime.PruneResult) string` are produced and consumed only within Task 3.
