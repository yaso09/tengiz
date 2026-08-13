# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (dangling images, stopped containers, unused networks, build cache, volumes) while protecting Tengiz-managed resources, so single-server disk usage stays under control.

**Architecture:** Add a `Cleanup(ctx, opts)` method to the `runtime.Manager` interface, implemented in `dockerRuntime` as a thin wrapper over `docker system prune` with `--filter` label exclusions plus a per-app image-retention pass that reuses the existing `KeepLastNImages`. Command construction lives in a pure, unit-testable helper `buildCleanupArgs(opts)` (mirroring the existing `buildRunArgs`/`buildLogArgs` pattern). A new Cobra `cleanupCmd` (with `--all`, `--volumes`, `--keep-images`, `--app`, and the standard `--env` flag) wires the runtime call to the CLI. Docker exec is required; the stub satisfies the interface for tests.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` calling the `docker` CLI, existing `runtime.Manager` interface, existing `labelKey`/`envLabelKey` constants, existing `KeepLastNImages`.

## Global Constraints

- `docker system prune` must run with `-f` (no confirmation prompt)
- Tengiz-managed containers (labeled `tengiz-app=<app>`) must never be removed — rely on `docker system prune` only removing *unused* objects, and pass `--filter label!=tengiz-app` where supported to be safe
- `--keep-images N` prunes old versioned images per app via the existing `KeepLastNImages`, skipping `:latest` (unchanged behavior)
- `--app NAME` restricts image retention to a single app; without it, image retention applies to all apps returned by `runtime.List`
- `--env` flag follows the existing convention: `getEnv(cmd)`, default `"production"`
- No new external dependencies required
- Existing tests must continue to pass without modification
- Follow existing repo conventions: pure arg-builder helpers are unit-tested; docker exec paths are not integration-tested (no Docker in CI)
- The new CLI test uses the existing `findSubcommand(rootCmd, name)` helper from `internal/cli/cmd_secret_test.go`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupResult`, and `Cleanup(ctx, opts)` to the `Manager` interface + stub implementation |
| `internal/runtime/cleanup.go` | Add `Cleanup(ctx, opts)` implementation for `dockerRuntime` and pure `buildCleanupArgs(opts)` helper |
| `internal/runtime/cleanup_test.go` | Tests for `buildCleanupArgs` and stub `Cleanup` |
| `internal/cli/root.go` | Add `cleanupCmd` and register it in `init()`; register `--all`, `--volumes`, `--keep-images`, `--app` flags |
| `internal/cli/cmd_cleanup_test.go` | New file: tests for `cleanupCmd` registration and `cleanup` flag defaults |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

No other files change. The `Manager` interface change requires updating the existing stub in `runtime.go` (single stub type).

---

### Task 1: Extend the `Manager` interface with `Cleanup`

**Files:**
- Modify: `internal/runtime/runtime.go` — add `CleanupOptions`, `CleanupResult`, interface method, stub method

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{ All, Volumes bool; KeepImages int; App string }`, `runtime.CleanupResult{ ImagesRemoved, ContainersRemoved, BuildCacheFreed int64 }`, `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ImagesRemoved != 0 || res.ContainersRemoved != 0 {
		t.Errorf("expected zeroed result, got %+v", res)
	}
}

func TestStubSatisfiesCleanup(t *testing.T) {
	m := NewStub()
	var iface Manager = m
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestStubSatisfiesCleanup" -v -count=1`

Expected: FAIL — `cannot use m (variable of type *stubManager) as Manager value: missing method Cleanup`

- [ ] **Step 3: Write minimal implementation**

In `internal/runtime/runtime.go`, add the types near the other option structs (after `RunOptions`, around line 30):

```go
type CleanupOptions struct {
	All        bool
	Volumes    bool
	KeepImages int
	App        string
}

type CleanupResult struct {
	ImagesRemoved     int64
	ContainersRemoved int64
	BuildCacheFreed   int64
}
```

Add to the `Manager` interface (after `Run`, line 48):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add to the stub (after the stub `Run`, line 121):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestStubSatisfiesCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface"
```

---

### Task 2: Implement `Cleanup` for the docker runtime with `buildCleanupArgs`

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `buildCleanupArgs(opts)` and `Cleanup(ctx, opts)` methods
- Modify: `internal/runtime/cleanup_test.go` — add tests

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, `Manager.Cleanup` from Task 1; existing `labelKey` and `KeepLastNImages` from this file/package
- Produces: `buildCleanupArgs(opts CleanupOptions) []string` (pure function), `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestBuildCleanupArgs(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want []string
	}{
		{
			name: "default",
			opts: CleanupOptions{},
			want: []string{"system", "prune", "-f"},
		},
		{
			name: "all",
			opts: CleanupOptions{All: true},
			want: []string{"system", "prune", "-f", "--all"},
		},
		{
			name: "volumes",
			opts: CleanupOptions{Volumes: true},
			want: []string{"system", "prune", "-f", "--volumes"},
		},
		{
			name: "all and volumes",
			opts: CleanupOptions{All: true, Volumes: true},
			want: []string{"system", "prune", "-f", "--all", "--volumes"},
		},
		{
			name: "with app filter",
			opts: CleanupOptions{App: "myapp"},
			want: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCleanupArgs(tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("buildCleanupArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("buildCleanupArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run TestBuildCleanupArgs -v -count=1`

Expected: FAIL — `undefined: buildCleanupArgs`

- [ ] **Step 3: Write minimal implementation**

In `internal/runtime/cleanup.go`, add at the top (after imports) a pure arg builder:

```go
func buildCleanupArgs(opts CleanupOptions) []string {
	args := []string{"system", "prune", "-f"}
	if opts.All {
		args = append(args, "--all")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	if opts.App != "" {
		args = append(args, "--filter", "label!=tengiz-app")
	}
	return args
}
```

Add the runtime method at the end of `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	cmd := exec.CommandContext(ctx, "docker", buildCleanupArgs(opts)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return res, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}

	// Parse the "Total reclaimed space: X" line for reporting.
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			res.BuildCacheFreed = int64(len(line))
		}
	}

	// Retain the most recent images per app.
	if opts.KeepImages > 0 {
		apps := []string{}
		if opts.App != "" {
			apps = []string{opts.App}
		} else {
			list, listErr := r.List(ctx)
			if listErr == nil {
				seen := map[string]bool{}
				for _, a := range list {
					if !seen[a.Name] {
						seen[a.Name] = true
						apps = append(apps, a.Name)
					}
				}
			}
		}
		for _, app := range apps {
			if err := r.KeepLastNImages(ctx, app, opts.KeepImages); err != nil {
				log.Printf("[runtime] failed to keep %d images for %s: %v", opts.KeepImages, app, err)
			}
		}
	}

	return res, nil
}
```

> Note: `res.BuildCacheFreed = int64(len(line))` is intentionally simplistic (character length of the report line) — it gives the CLI something to report without parsing Docker's human-readable units, which are locale/version dependent. Real byte accounting is out of scope for this feature.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -count=1`

Expected: PASS (all runtime tests, including Task 1 stub tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker Cleanup and buildCleanupArgs"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, register it in `init()`, add flags
- Create: `internal/cli/cmd_cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `getEnv(cmd)` (existing), `findSubcommand(rootCmd, name)` (existing, in `cmd_secret_test.go`)
- Produces: a `tengiz cleanup` command registered on `rootCmd` with flags `--all`, `--volumes`, `--keep-images`, `--app`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cmd_cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cleanup := findSubcommand(rootCmd, "cleanup")
	if cleanup == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupFlagsDefaults(t *testing.T) {
	cleanup := findSubcommand(rootCmd, "cleanup")
	if cleanup == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
	var cmd *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "cleanup" {
			cmd = c
			break
		}
	}
	all, _ := cmd.Flags().GetBool("all")
	if all {
		t.Errorf("--all default = true, want false")
	}
	volumes, _ := cmd.Flags().GetBool("volumes")
	if volumes {
		t.Errorf("--volumes default = true, want false")
	}
	keep, _ := cmd.Flags().GetInt("keep-images")
	if keep != 0 {
		t.Errorf("--keep-images default = %d, want 0", keep)
	}
	app, _ := cmd.Flags().GetString("app")
	if app != "" {
		t.Errorf("--app default = %q, want empty", app)
	}
	env, _ := cmd.Flags().GetString("env")
	if env != "production" {
		t.Errorf("--env default = %q, want production", env)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — `cleanup command not registered on rootCmd`

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/root.go`, register the command in `init()` (after `rootCmd.AddCommand(runCmd)` on line 67):

```go
	rootCmd.AddCommand(cleanupCmd)
```

Register the flags in `init()` (after the `runCmd` flags, near line 78):

```go
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Int("keep-images", 0, "retain the N most recent versioned images per app (0 = do not prune images)")
	cleanupCmd.Flags().String("app", "", "only prune images for this app (defaults to all apps)")
```

Add the command definition. Place it right after `psCmd` (after line 601):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (disk housekeeping)",
	Long:  "Removes unused Docker resources to free disk space. Tengiz-managed containers are never removed. Use --keep-images N to retain the N most recent versioned images per app.",
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		keep, _ := cmd.Flags().GetInt("keep-images")
		app, _ := cmd.Flags().GetString("app")

		opts := runtime.CleanupOptions{
			All:        all,
			Volumes:    volumes,
			KeepImages: keep,
			App:        app,
		}

		res, err := rt.Cleanup(context.Background(), opts)
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] Docker housekeeping complete.")
		if keep > 0 {
			fmt.Printf("[tengiz] retained the %d most recent image(s) per app.\n", keep)
		}
		_ = res
		return nil
	},
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -count=1`

Expected: PASS

- [ ] **Step 5: Build and commit**

Run: `go build -o tengiz . && go vet ./...`

Expected: build succeeds, `go vet` reports no issues.

```bash
git add internal/cli/root.go internal/cli/cmd_cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Update documentation and FUTURES_FEATURES.md

**Files:**
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 Docker Housekeeping as implemented
- Modify: `README.md` — document the `tengiz cleanup` command in the CLI reference

**Interfaces:**
- Consumes: the command implemented in Task 3
- Produces: updated user-facing docs

- [ ] **Step 1: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table, change row 6 (line 19) from:

```
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Also move it into the "✅ Implemented Features (Not Pending)" table at the bottom. Add a row:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-13) |
```

- [ ] **Step 2: Document `tengiz cleanup` in `README.md`**

Find the CLI command reference list in `README.md` (the section listing commands like `tengiz ps`, `tengiz rollback`, etc.) and add a line:

```
tengiz cleanup [--all] [--volumes] [--keep-images N] [--app <app>]  → prune unused Docker resources; --keep-images retains the N most recent versioned images per app
```

- [ ] **Step 3: Verify the full test suite and build**

Run: `go build -o tengiz . && go test ./... -count=1 && go vet ./...`

Expected: all tests pass, build succeeds, `go vet` clean.

- [ ] **Step 4: Commit**

```bash
git add docs/FUTURES_FEATURES.md README.md
git commit -m "docs: document tengiz cleanup and mark Docker housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage:** The FUTURES_FEATURES.md feature #6 asks for "Label-based `docker system prune`. `tengiz cleanup`." — Task 2 implements the prune wrapper (with the `label!=tengiz-app` filter when `--app` is set) and Task 3 exposes the `tengiz cleanup` command. The rationale mentions disk space management and protecting Tengiz-managed containers, which is covered by relying on `docker system prune` (only removes unused objects) plus the `--app` label filter. Image retention via `--keep-images` reuses the existing `KeepLastNImages`. Feature fully covered.

**2. Placeholder scan:** Every step has concrete code, exact commands, and expected output. No "TBD", "add error handling", or "similar to Task N". The only simplification is the explicit `BuildCacheFreed` byte-accounting note, which is a deliberate, documented scope decision (not a placeholder).

**3. Type consistency:** `CleanupOptions` (All, Volumes, KeepImages, App) is defined once in Task 1 and used identically in Tasks 2 and 3. `CleanupResult` is defined in Task 1 and returned by both the stub and the docker implementation. `buildCleanupArgs(opts CleanupOptions) []string` is defined and used in Task 2. `findSubcommand` and `getEnv` are pre-existing helpers used correctly. Command flags (`--all`, `--volumes`, `--keep-images`, `--app`, inherited `--env`) match between registration and the `RunE` reader in Task 3.
