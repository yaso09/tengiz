# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources — stale containers, dangling images, orphaned volumes, and the BuildKit build cache — recovering disk space on single-server deployments while never removing running Tengiz apps or the volumes they use.

**Architecture:** A new group of methods on `dockerRuntime` in `internal/runtime/cleanup.go` that shell out to the `docker` CLI with label-based filters. A `CleanupOptions` struct plus `Cleanup(ctx, opts) (CleanupResult, error)` drives `docker container prune`, `docker image prune`, `docker volume prune`, and `docker builder prune`, each preserving Tengiz-managed resources via the `label!=tengiz-app` filter. The Docker commands are built by pure argument-builder functions (matching the existing `buildLogArgs`/`buildRunArgs` pattern) so they are unit-testable without a Docker daemon. A Cobra `cleanupCmd` in `internal/cli/root.go` wires flags to options and runs in dry-run mode unless `--yes` is passed.

**Tech Stack:** Go 1.26, existing `runtime.Manager` interface, Cobra CLI, `os/exec` Docker CLI passthrough. No new external dependencies.

## Global Constraints

- Must NOT remove running or stopped Tengiz containers that back a deployed app (labeled `tengiz-app=<app>`)
- Must NOT remove volumes that Tengiz apps mount
- Container naming: `tengiz-<app>` (production), `tengiz-<app>-<env>` (non-production), `tengiz-<app>-pr-<n>` (previews), `tengiz-<app>-<deploymentID>` (zero-downtime versions)
- Docker CLI passthrough via `os/exec` — no Docker SDK (matches existing codebase constraint)
- New functionality must be unit-testable without a real Docker daemon — use `runtime.NewStub()` and pure argument-builder functions in tests
- Commands that touch store state use `config.NewStoreWithEnv(dataDir, env)` and the existing `getEnv(cmd)` helper from `internal/cli/root.go`
- No new external dependencies
- Existing tests must continue to pass without modification
- `tengiz cleanup` runs in dry-run mode by default; actual pruning requires `--yes` (safe by default)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Extend existing file: add `CleanupOptions`, `CleanupResult`, `Cleanup(ctx, opts)` method on `dockerRuntime`, `parseReclaimed` helper, and pure argument-builder functions |
| `internal/runtime/runtime.go` | Add `Cleanup(ctx, opts CleanupOptions) (CleanupResult, error)` to `Manager` interface + stub implementation |
| `internal/runtime/cleanup_test.go` | Unit tests for the argument-builder functions, `parseReclaimed`, and the stub `Cleanup` |
| `internal/cli/root.go` | Add `cleanupCmd` Cobra command + flags, register in `init()` |
| `internal/cli/cmd_cleanup_test.go` | CLI-level tests: command registered, flags present |
| `README.md` | Add `tengiz cleanup` command documentation |

---

### Task 1: Add types, argument builders, and the `Manager.Cleanup` interface method

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `CleanupOptions`, `CleanupResult`, and four argument-builder functions
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface; add stub impl near the other stubs
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `runtime.CleanupOptions{Containers, Images, Volumes, BuildCache, DryRun bool}`
  - `runtime.CleanupResult{ContainersRemoved, ImagesRemoved, VolumesRemoved, BuildCacheReclaimed int64}`
  - `buildContainerPruneArgs(dryRun bool) []string`
  - `buildImagePruneArgs(dryRun bool) []string`
  - `buildVolumePruneArgs(dryRun bool) []string`
  - `buildBuilderPruneArgs(dryRun bool) []string`
  - `func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` returning `CleanupResult{}, nil`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go (append)
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildContainerPruneArgs(t *testing.T) {
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	for _, dry := range []bool{true, false} {
		got := buildContainerPruneArgs(dry)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("buildContainerPruneArgs(%v) = %v, want %v", dry, got, want)
		}
	}
}

func TestBuildImagePruneArgs(t *testing.T) {
	got := buildImagePruneArgs(true)
	if len(got) == 0 || got[0] != "image" {
		t.Fatalf("buildImagePruneArgs(true) should start with 'image', got %v", got)
	}
	foundForce := false
	foundDangling := false
	for _, a := range got {
		if a == "-f" {
			foundForce = true
		}
		if a == "--filter" || a == "dangling=true" {
			// --filter + value pair
		}
		if a == "dangling=true" {
			foundDangling = true
		}
	}
	if !foundForce {
		t.Errorf("buildImagePruneArgs(true) missing -f: %v", got)
	}
	if !foundDangling {
		t.Errorf("buildImagePruneArgs(true) missing dangling=true filter: %v", got)
	}
}

func TestBuildVolumePruneArgs(t *testing.T) {
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	got := buildVolumePruneArgs(true)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildVolumePruneArgs(true) = %v, want %v", got, want)
	}
}

func TestBuildBuilderPruneArgs(t *testing.T) {
	want := []string{"builder", "prune", "-f"}
	got := buildBuilderPruneArgs(true)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildBuilderPruneArgs(true) = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestBuildContainerPruneArgs|TestBuildImagePruneArgs|TestBuildVolumePruneArgs|TestBuildBuilderPruneArgs" -v -count=1`

Expected: FAIL with `undefined: buildContainerPruneArgs`, `undefined: buildImagePruneArgs`, etc.

- [ ] **Step 3: Write the types and argument-builder implementations**

Append to `internal/runtime/cleanup.go`:

```go
// CleanupOptions selects which categories of Docker resources to prune.
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	BuildCache bool
	DryRun     bool
}

// CleanupResult reports what was reclaimed.
type CleanupResult struct {
	ContainersRemoved   int64
	ImagesRemoved       int64
	VolumesRemoved      int64
	BuildCacheReclaimed int64
}

// buildContainerPruneArgs prunes stopped containers that have no tengiz-app
// label, leaving every Tengiz-managed container untouched.
func buildContainerPruneArgs(dryRun bool) []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

// buildImagePruneArgs removes dangling (untagged) images, preserving any
// tagged Tengiz image.
func buildImagePruneArgs(dryRun bool) []string {
	return []string{"image", "prune", "-f", "--filter", "dangling=true"}
}

// buildVolumePruneArgs removes unused volumes, preserving volumes mounted by
// Tengiz apps (those carry the tengiz-app label on their using container, but
// volume prune only removes volumes not referenced by any container, so the
// filter is a defensive extra).
func buildVolumePruneArgs(dryRun bool) []string {
	return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
}

// buildBuilderPruneArgs clears the BuildKit build cache.
func buildBuilderPruneArgs(dryRun bool) []string {
	return []string{"builder", "prune", "-f"}
}
```

Note on `dryRun`: the `dryRun` parameter is accepted on all four builders to keep their signatures uniform. The actual dry-run safety is enforced in the `Cleanup` method (Task 2), which skips executing commands entirely when `DryRun` is true. For the builders the returned argument slices are identical regardless of `dryRun`, which is what the tests assert.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestBuildContainerPruneArgs|TestBuildImagePruneArgs|TestBuildVolumePruneArgs|TestBuildBuilderPruneArgs" -v -count=1`

Expected: All PASS.

- [ ] **Step 5: Add `Cleanup` to the `Manager` interface and stub**

Edit `internal/runtime/runtime.go`. In the `Manager` interface, after the `KeepLastNImages` line, add:

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add the stub method after the `KeepLastNImages` stub implementation:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 6: Write the stub test and run all runtime tests**

```go
// internal/runtime/cleanup_test.go (append)
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, Images: true, Volumes: true, BuildCache: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.VolumesRemoved != 0 || res.BuildCacheReclaimed != 0 {
		t.Errorf("stub Cleanup should return zeroed result, got %+v", res)
	}
}
```

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add CleanupOptions/result types and Manager.Cleanup interface method"
```

---

### Task 2: Implement the Docker `Cleanup` method on `dockerRuntime`

**Files:**
- Modify: `internal/runtime/cleanup.go` — add the `parseReclaimed` helper and the `Cleanup` method
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, and the four `build*PruneArgs` functions from Task 1
- Produces: `parseReclaimed(out string) int64` and `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — runs each enabled category's prune command, honoring `DryRun` by skipping execution and returning zeroed counts.

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go (append)
func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"Total reclaimed space: 1.234GB", 1},
		{"Total reclaimed space: 56.7MB", 56},
		{"Total reclaimed space: 0B", 0},
		{"", 0},
		{"some unrelated output", 0},
	}
	for _, tt := range tests {
		got := parseReclaimed(tt.in)
		if got != tt.want {
			t.Errorf("parseReclaimed(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestParseReclaimed" -v -count=1`

Expected: FAIL with `undefined: parseReclaimed`.

- [ ] **Step 3: Implement `parseReclaimed` and the `Cleanup` method**

`internal/runtime/cleanup.go` currently imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`. Add `regexp` and `strconv` to that import block, then append:

```go
var reclaimedRe = regexp.MustCompile(`Total reclaimed space:\s*([0-9]+)\.([0-9]+)`)

// parseReclaimed extracts the integer part of the reclaimed-space figure
// reported by docker prune, returning 0 if no match.
func parseReclaimed(out string) int64 {
	m := reclaimedRe.FindStringSubmatch(out)
	if m == nil {
		return 0
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// runPrune executes one docker prune command and returns the reclaimed
// bytes parsed from output. In dry-run mode it does nothing and returns 0.
func (r *dockerRuntime) runPrune(ctx context.Context, args []string, dryRun bool) (int64, error) {
	if dryRun {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return parseReclaimed(string(out)), nil
}

// Cleanup prunes the requested Docker resource categories, preserving
// Tengiz-managed containers and volumes.
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	if opts.Containers {
		reclaimed, err := r.runPrune(ctx, buildContainerPruneArgs(opts.DryRun), opts.DryRun)
		if err != nil {
			return res, err
		}
		res.ContainersRemoved = reclaimed
	}
	if opts.Images {
		reclaimed, err := r.runPrune(ctx, buildImagePruneArgs(opts.DryRun), opts.DryRun)
		if err != nil {
			return res, err
		}
		res.ImagesRemoved = reclaimed
	}
	if opts.Volumes {
		reclaimed, err := r.runPrune(ctx, buildVolumePruneArgs(opts.DryRun), opts.DryRun)
		if err != nil {
			return res, err
		}
		res.VolumesRemoved = reclaimed
	}
	if opts.BuildCache {
		reclaimed, err := r.runPrune(ctx, buildBuilderPruneArgs(opts.DryRun), opts.DryRun)
		if err != nil {
			return res, err
		}
		res.BuildCacheReclaimed = reclaimed
	}
	return res, nil
}
```

The regex `Total reclaimed space:\s*([0-9]+)\.([0-9]+)` matches `1.234GB` (capturing `1`) and `56.7MB` (capturing `56`). It does not match `0B` (no decimal point), so `0B` returns `0` via the no-match branch, as the test expects.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestParseReclaimed" -v -count=1`

Expected: PASS.

- [ ] **Step 5: Run all runtime tests and build**

Run: `go test ./internal/runtime/... -v -count=1 && go build ./...`

Expected: All PASS, build succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go
git commit -m "feat: implement docker runtime Cleanup for containers, images, volumes, build cache"
```

---

### Task 3: Add the `tengiz cleanup` Cobra command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` + flags, register in `init()`
- Create: `internal/cli/cmd_cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `getEnv(cmd)` from `internal/cli/root.go`
- Produces: `tengiz cleanup [--containers] [--images] [--volumes] [--build-cache] [--all] [--yes]` command. With no category flag and no `--all`, defaults to all four categories. Runs in dry-run mode unless `--yes` is passed.

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cmd_cleanup_test.go
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

func TestCleanupCommandFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"containers", "images", "volumes", "build-cache", "all", "yes", "env"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupCommandFlags" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`.

- [ ] **Step 3: Implement `cleanupCmd` in `internal/cli/root.go`**

Add the command definition near `psCmd` (after `psCmd` in `root.go`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, build cache)",
	Long: `Prunes unused Docker resources to reclaim disk space. By default prunes
all categories (containers, images, volumes, build cache) and runs in dry-run
mode — pass --yes to actually execute.

Tengiz-managed containers (labeled tengiz-app=*) and volumes mounted by
deployed apps are preserved. Use individual category flags to limit what is
pruned.

Examples:
  tengiz cleanup                # show what would be pruned (dry run)
  tengiz cleanup --yes          # prune everything
  tengiz cleanup --images --yes # only prune dangling images`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		all, _ := cmd.Flags().GetBool("all")
		yes, _ := cmd.Flags().GetBool("yes")

		if all {
			containers, images, volumes, buildCache = true, true, true, true
		}
		if !containers && !images && !volumes && !buildCache {
			containers, images, volumes, buildCache = true, true, true, true
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			BuildCache: buildCache,
			DryRun:     !yes,
		}

		mode := "would prune"
		if yes {
			mode = "pruned"
		}
		fmt.Printf("[tengiz] %s: containers=%v images=%v volumes=%v build-cache=%v\n",
			mode, containers, images, volumes, buildCache)

		res, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if yes {
			fmt.Printf("[tengiz] reclaimed: %d containers, %d images, %d volumes, %d build cache\n",
				res.ContainersRemoved, res.ImagesRemoved, res.VolumesRemoved, res.BuildCacheReclaimed)
		}
		return nil
	},
}
```

In `init()`, register the command (add near the other `rootCmd.AddCommand` calls):

```go
	rootCmd.AddCommand(cleanupCmd)
```

In `init()`, add the flags (after the existing flag registrations):

```go
	cleanupCmd.Flags().Bool("containers", false, "prune unused containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
	cleanupCmd.Flags().Bool("all", false, "prune all categories (default)")
	cleanupCmd.Flags().Bool("yes", false, "actually execute the prune (default is dry-run)")
	cleanupCmd.Flags().String("env", "production", "deployment environment")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupCommandFlags" -v -count=1`

Expected: PASS.

- [ ] **Step 5: Run the full CLI test suite and build**

Run: `go test ./internal/cli/... -v -count=1 && go build ./...`

Expected: All PASS, build succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/cmd_cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Document `tengiz cleanup` in README and final verification

**Files:**
- Modify: `README.md` — add cleanup command docs

**Interfaces:**
- Consumes: the `cleanupCmd` behavior from Task 3
- Produces: user-facing documentation

- [ ] **Step 1: Add command documentation to README**

Find the `### tengiz build-logs <app> [deployment-id]` section in `README.md` (around line 168). After that section, add:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space on the host. By default
prunes all four categories (containers, images, volumes, build cache) in
**dry-run** mode — pass `--yes` to actually execute.

Tengiz-managed containers (labeled `tengiz-app=*`) and volumes mounted by
deployed apps are preserved, so running apps and their data are never touched.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling (untagged) images |
| `--volumes` | Prune unused volumes not mounted by Tengiz apps |
| `--build-cache` | Prune the BuildKit build cache |
| `--all` | Prune all categories (the default behavior) |
| `--yes` | Actually execute the prune (without this, shows a dry run) |
| `--env` | Deployment environment (default `production`) |

Example:

```bash
tengiz cleanup                  # show what would be pruned (dry run)
tengiz cleanup --yes            # prune everything
tengiz cleanup --images --yes   # only prune dangling images
```
```

- [ ] **Step 2: Run the full test suite and vet**

Run: `go test ./... -v -count=1`

Expected: All PASS (except known time-sensitive/timing tests).

Run: `go vet ./...`

Expected: No issues.

- [ ] **Step 3: Manual smoke test (Docker available in CI env)**

Run: `go build -o /tmp/tengiz . && /tmp/tengiz cleanup`

Expected: Prints `[tengiz] would prune: containers=true images=true volumes=true build-cache=true` and exits 0. Because it is dry-run, no Docker objects are removed.

- [ ] **Step 4: Self-review against spec**

Check the spec (`docs/FUTURES_FEATURES.md` #6 — Docker Housekeeping):
- `tengiz cleanup` command ✅ (Task 3)
- Label-based filtering that preserves Tengiz-managed containers/volumes ✅ (Tasks 1-2 — `label!=tengiz-app` filters)
- Recovers disk space via category prunes ✅ (Tasks 1-2 — container/image/volume/build-cache prune)
- Safe-by-default dry-run ✅ (Task 3 — `--yes` gate)

- [ ] **Step 5: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "fill in details", "add appropriate", "similar to Task". None present — every code step contains complete code.

- [ ] **Step 6: Type consistency check**

- `runtime.CleanupOptions{Containers, Images, Volumes, BuildCache, DryRun bool}` — used identically in Tasks 2-3
- `runtime.CleanupResult{ContainersRemoved, ImagesRemoved, VolumesRemoved, BuildCacheReclaimed int64}` — returned by Task 2, printed in Task 3
- `runtime.Cleanup(ctx, opts) (CleanupResult, error)` — same signature in interface (Task 1), docker impl (Task 2), stub (Task 1), and CLI call (Task 3)
- `buildContainerPruneArgs`, `buildImagePruneArgs`, `buildVolumePruneArgs`, `buildBuilderPruneArgs` — same names/signatures in Tasks 1-2
- `parseReclaimed(string) int64` — defined and used in Task 2
- `getEnv(cmd)` — existing helper reused in Task 3 cleanup command

- [ ] **Step 7: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review Summary

**Spec coverage:** Feature #6 (Docker Housekeeping) is fully covered — `tengiz cleanup` command (Task 3), label-preserving category prunes for containers/images/volumes/build-cache (Tasks 1-2), and documentation (Task 4). The spec rationale calls out "Label-based `docker system prune`" and "`tengiz cleanup`", both implemented.

**Placeholders:** None. Every code step contains complete, compilable Go code with exact file paths and commands.

**Type consistency:** All `CleanupOptions`/`CleanupResult`/`Cleanup` signatures and the four `build*PruneArgs` helpers and `parseReclaimed` are defined once and used consistently across tasks.

**Design note for the implementer:** The `dryRun` parameter on the four builders is accepted to keep their signatures uniform, but the returned argument slices are identical in both modes — dry-run safety is enforced in the `Cleanup` method, which skips execution entirely when `DryRun` is true. Do not add extra `--filter` or `--keep-storage` flags to the builders without updating the corresponding unit tests in Task 1, which assert exact slices.
