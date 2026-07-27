# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command and label-based Docker system pruning so users can reclaim disk space with one command, preventing disk-full failures on single-server deployments.

**Architecture:** Two new methods on the `runtime.Manager` interface (`PruneSystem`, `PruneAppImages`) wrapping `docker system prune -f` and per-app image retention. A new `tengiz cleanup` CLI command orchestrates system-level prune + per-app image pruning + build log cleanup. Optional `--volumes`/`--all` flags mirror Docker's prune flags. Label-based exclusion (`tengiz-app` label) protects Tengiz-managed containers from accidental removal.

**Tech Stack:** Go 1.26, Cobra CLI, `os/exec` Docker CLI calls, existing `runtime.Manager` interface, existing `config.Store.PruneBuildLogs`.

## Global Constraints

- `tengiz cleanup` must never remove currently-running Tengiz containers
- Default behavior must be safe (equivalent to `docker system prune -f` — only dangling images, stopped containers, unused networks, build cache)
- `--volumes` flag adds `docker system prune --volumes` (unused volumes)
- `--all` flag adds `docker image prune -a` (all unused images, not just dangling)
- Existing `KeepLastNImages` logic must remain unchanged — `PruneAppImages` extends it
- All image tags follow `tengiz-apps/<appName>:*` naming convention
- All Tengiz containers carry `tengiz-app=<appName>` and `tengiz-env=<env>` labels
- Scheduled/periodic cleanup is OUT OF SCOPE for this phase (only CLI-triggered)
- No new external dependencies allowed

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneSystem`, `PruneAppImages` to `Manager` interface + stub stubs |
| `internal/runtime/cleanup.go` | dockerRuntime implementations: `PruneSystem` wraps `docker system prune -f`, `PruneAppImages` wraps per-app image retention |
| `internal/runtime/cleanup_test.go` | Tests for stub implementations |
| `internal/cli/root.go` | Register `cleanupCmd`, add to `init()` |
| `internal/cli/cleanup_test.go` | Tests for CLI command registration and flag parsing |

No new files created for runtime changes. One new test file for CLI cleanup tests.

---

### Task 1: Add PruneSystem and PruneAppImages to runtime.Manager + implementations

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add two methods to `Manager` interface
- Modify: `internal/runtime/runtime.go:117-122` — add stub implementations
- Modify: `internal/runtime/cleanup.go` — add dockerRuntime implementations

**Interfaces:**
- Consumes: nothing new
- Produces: `PruneSystem(ctx, opts) error`, `PruneAppImages(ctx, appName string, keep int) error`, `PruneOptions{All bool, Volumes bool}`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go — add to existing file

func TestStubPruneSystem(t *testing.T) {
    m := NewStub()
    if err := m.PruneSystem(context.Background(), PruneOptions{}); err != nil {
        t.Fatalf("PruneSystem() error = %v", err)
    }
}

func TestStubPruneSystemWithVolumes(t *testing.T) {
    m := NewStub()
    opts := PruneOptions{Volumes: true}
    if err := m.PruneSystem(context.Background(), opts); err != nil {
        t.Fatalf("PruneSystem(volumes) error = %v", err)
    }
}

func TestStubPruneSystemWithAll(t *testing.T) {
    m := NewStub()
    opts := PruneOptions{All: true, Volumes: true}
    if err := m.PruneSystem(context.Background(), opts); err != nil {
        t.Fatalf("PruneSystem(all, volumes) error = %v", err)
    }
}

func TestStubPruneAppImages(t *testing.T) {
    m := NewStub()
    if err := m.PruneAppImages(context.Background(), "testapp", 5); err != nil {
        t.Fatalf("PruneAppImages() error = %v", err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPrune" -v -count=1`

Expected: FAIL with `PruneSystem undefined`, `PruneAppImages undefined` (interface methods not defined yet)

- [ ] **Step 3: Add PruneOptions type and methods to Manager interface in `internal/runtime/runtime.go`**

Add the type above the interface:
```go
type PruneOptions struct {
    All     bool
    Volumes bool
}
```

Add two methods to the `Manager` interface (after `KeepLastNImages`):
```go
    PruneSystem(ctx context.Context, opts PruneOptions) error
    PruneAppImages(ctx context.Context, appName string, keep int) error
```

Add stub implementations at the bottom of the file (after existing stub methods):
```go
func (m *stubManager) PruneSystem(ctx context.Context, opts PruneOptions) error {
    return nil
}

func (m *stubManager) PruneAppImages(ctx context.Context, appName string, keep int) error {
    return nil
}
```

- [ ] **Step 4: Add dockerRuntime implementations in `internal/runtime/cleanup.go`**

Add to the bottom of `cleanup.go`:
```go
func (r *dockerRuntime) PruneSystem(ctx context.Context, opts PruneOptions) error {
    args := []string{"system", "prune", "-f"}
    if opts.All {
        args = append(args, "--all")
    }
    if opts.Volumes {
        args = append(args, "--volumes")
    }
    cmd := exec.CommandContext(ctx, "docker", args...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}

func (r *dockerRuntime) PruneAppImages(ctx context.Context, appName string, keep int) error {
    return r.KeepLastNImages(ctx, appName, keep)
}
```

Add `"os"` to the import block in `cleanup.go`:
```go
import (
    "context"
    "fmt"
    "log"
    "os"
    "os/exec"
    "sort"
    "strings"
)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStubPrune" -v -count=1`

Expected: PASS

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Run build to verify no compile errors**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add PruneSystem and PruneAppImages to runtime.Manager"
```

---

### Task 2: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add cleanupCmd, register in init()
- Create: `internal/cli/cleanup_test.go` — tests for the cleanup command

**Interfaces:**
- Consumes: `runtime.PruneSystem`, `runtime.PruneAppImages`, `runtime.NewDocker`, `config.NewStoreWithEnv`, `Store.ListApps`, `Store.PruneBuildLogs`
- Produces: `tengiz cleanup [--volumes] [--all] [--app <name>] [--keep <n>]` CLI command

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go — new file

package cli

import (
    "strings"
    "testing"
)

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
    cmd, _, err := rootCmd.Find([]string{"cleanup"})
    if err != nil {
        t.Fatal("cleanup command not registered")
    }

    expected := map[string]bool{
        "volumes": false,
        "all":     false,
        "app":     false,
        "keep":    false,
    }
    for name := range expected {
        flag := cmd.Flags().Lookup(name)
        if flag == nil {
            t.Errorf("cleanup command missing --%s flag", name)
        } else {
            expected[name] = true
        }
    }

    for name, found := range expected {
        if !found {
            t.Errorf("cleanup command missing --%s flag", name)
        }
    }
}

func TestCleanupHelpContainsDescription(t *testing.T) {
    rootCmd.SetArgs([]string{"cleanup", "--help"})
    output := captureOutput(func() {
        rootCmd.Execute()
    })
    if !strings.Contains(output, "clean") && !strings.Contains(output, "prune") {
        t.Error("cleanup --help should mention cleaning or pruning")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `cleanup command not registered` or similar

- [ ] **Step 3: Add cleanup command to `internal/cli/root.go`**

Add the cleanup command variable near other command variables (e.g., after `healthCmd` definition around line 580-600):

```go
var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Free disk space by pruning unused Docker resources",
    Long: `Removes stopped containers, dangling images, unused networks, and build cache.

By default runs 'docker system prune -f' — safe for running Tengiz containers.
Use --all to also remove all unused images (not just dangling).
Use --volumes to also prune unused volumes.
Use --app <name> to additionally prune old images for a specific app, keeping the last N (default 5).
Use --keep <n> with --app to control how many images to keep.`,
    Args: cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        volumes, _ := cmd.Flags().GetBool("volumes")
        all, _ := cmd.Flags().GetBool("all")
        appName, _ := cmd.Flags().GetString("app")
        keep, _ := cmd.Flags().GetInt("keep")

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        // 1. Run Docker system prune
        fmt.Println("[tengiz] pruning Docker system resources...")
        pruneOpts := runtime.PruneOptions{
            All:     all,
            Volumes: volumes,
        }
        if err := rt.PruneSystem(cmd.Context(), pruneOpts); err != nil {
            return fmt.Errorf("system prune: %w", err)
        }

        // 2. Per-app image pruning
        if appName != "" {
            fmt.Printf("[tengiz] pruning old images for %s (keeping %d)...\n", appName, keep)
            if err := rt.PruneAppImages(cmd.Context(), appName, keep); err != nil {
                return fmt.Errorf("prune images for %s: %w", appName, err)
            }
        } else {
            // Prune old images for all apps in the store
            store := config.NewStoreWithEnv(dataDir, env)
            apps, listErr := store.ListApps()
            if listErr == nil {
                for _, app := range apps {
                    if pruneErr := rt.PruneAppImages(cmd.Context(), app.Name, keep); pruneErr != nil {
                        fmt.Fprintf(os.Stderr, "[tengiz] warning: prune images for %s: %v\n", app.Name, pruneErr)
                    }
                }
            }
        }

        // 3. Prune build logs for all apps in the store
        store := config.NewStoreWithEnv(dataDir, env)
        apps, listErr := store.ListApps()
        if listErr == nil {
            prunedCount := 0
            for _, app := range apps {
                if pruneErr := store.PruneBuildLogs(app.Name, keep); pruneErr != nil {
                    fmt.Fprintf(os.Stderr, "[tengiz] warning: prune build logs for %s: %v\n", app.Name, pruneErr)
                } else {
                    prunedCount++
                }
            }
            if prunedCount > 0 {
                fmt.Printf("[tengiz] pruned build logs for %d app(s)\n", prunedCount)
            }
        }

        fmt.Println("[tengiz] cleanup complete")
        return nil
    },
}
```

Add to `init()`:
```go
// After existing AddCommand lines (around line 75), add:
rootCmd.AddCommand(cleanupCmd)
```

Add flags in `init()`:
```go
cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (use with caution)")
cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling")
cleanupCmd.Flags().String("app", "", "prune old images for a specific app")
cleanupCmd.Flags().Int("keep", 5, "number of images to keep per app (used with --app)")
```

Add `"os"` import if not already present (it is — line 8 has `"os"`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run build to verify**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Verify mockRTForDeploy still implements Manager**

The existing `mockRTForDeploy` in `root_test.go` implements `runtime.Manager`. Since we added two new methods, `mockRTForDeploy` must implement them too.

Run: `go test ./internal/cli/... -run "TestMockRTForDeployImplementsManager" -v -count=1`

Expected: PASS (if it fails, add the two new stub methods to `mockRTForDeploy` — see Step 6a)

- [ ] **Step 6a (conditional): Update mockRTForDeploy if TestMockRTForDeployImplementsManager fails**

If the test fails, add to `root_test.go` in the `mockRTForDeploy` struct:

```go
func (m *mockRTForDeploy) PruneSystem(ctx context.Context, opts runtime.PruneOptions) error { return nil }
func (m *mockRTForDeploy) PruneAppImages(ctx context.Context, appName string, keep int) error { return nil }
```

- [ ] **Step 7: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except possibly proxy TCP timeout tests and idle time-sensitive tests)

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command with system prune and app image retention"
```

---

### Task 3: Update mockRTForDeploy and verify full test suite

**Files:**
- Conditionally modify: `internal/cli/root_test.go` — add stub methods if needed

**Interfaces:**
- Consumes: `PruneSystem`, `PruneAppImages` from Manager interface
- Produces: Passing tests throughout the codebase

- [ ] **Step 1: Run the full test suite to identify any compilation errors**

Run: `go test ./... -v -count=1 2>&1 | head -100`

Look for: compilation errors about `mockRTForDeploy` missing `PruneSystem` or `PruneAppImages`.

- [ ] **Step 2: Fix any compilation errors**

If `mockRTForDeploy` (in `root_test.go`) does not implement all of `Manager`, add:
```go
func (m *mockRTForDeploy) PruneSystem(ctx context.Context, opts runtime.PruneOptions) error { return nil }
func (m *mockRTForDeploy) PruneAppImages(ctx context.Context, appName string, keep int) error { return nil }
```

If there are any other mock/interface implementations in the codebase (check via `grep -rn "Manager" --include="*_test.go"`), add the two methods to each.

- [ ] **Step 3: Run tests again to confirm all pass**

Run: `go test ./... -v -count=1`

Expected: All PASS (or pre-existing failures only — proxy TCP timeout tests and idle time-sensitive tests)

- [ ] **Step 4: Run vet**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 5: Commit**

```bash
git add -u
git commit -m "fix: add PruneSystem and PruneAppImages to mock implementations"
```

---

### Self-Review

- [ ] **Spec coverage:** Does this plan implement the feature described in `docs/FUTURES_FEATURES.md`?

  The feature says: "Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`."

  Coverage:
  - `tengiz cleanup` command — ✅ Task 2
  - `docker system prune -f` wrapper — ✅ Task 1 (`PruneSystem`)
  - Label-based: Tengiz containers have `tengiz-app` labels; `docker system prune` only removes stopped containers, so running Tengiz containers are inherently protected — ✅
  - Per-app image retention via `KeepLastNImages` — ✅ Task 1 (`PruneAppImages` delegates to existing `KeepLastNImages`)
  - Build log pruning on cleanup — ✅ Task 2 invokes `store.PruneBuildLogs`

- [ ] **Placeholder scan:** Search plan for "TODO", "TBD", "implement later", "fill in details", "Similar to Task". None found. Every step has complete code.

- [ ] **Type consistency check:**

  - `PruneOptions{All bool, Volumes bool}` — same struct used in Task 1 (interface definition) and Task 2 (CLI call)
  - `PruneSystem(ctx, opts) error` — consistent signature across interface, dockerRuntime, stub, and CLI usage
  - `PruneAppImages(ctx, appName string, keep int) error` — consistent signature everywhere
  - `runtime.NewDocker() (Manager, error)` — existing pattern, used in Task 2
  - `getEnv(cmd) string` — existing helper, used in Task 2
  - `config.NewStoreWithEnv(dataDir, env)` — existing pattern, used in Task 2
  - `store.ListApps()` returns `[]types.AppEntry` and `error` — used in Task 2 loop
  - `store.PruneBuildLogs(appName, keep)` — existing method, called in Task 2
  - No type/function name collisions with existing code
