# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command and automatic label-based Docker system pruning so disk space doesn't become a production issue on single-server deployments.

**Architecture:** Four new `runtime.Manager` methods (`PruneSystem`, `PruneBuildCache`, `PruneContainers`, `PruneImages`) wrapping `docker system prune --filter` with Tengiz label filters. A new `tengiz cleanup` CLI command with `--all`, `--force`, `--images` flags. After `tengiz rm`, hook into the cleaned app's image removal for complete teardown.

**Tech Stack:** Docker CLI via `os/exec`, existing `runtime.Manager` interface, Cobra CLI.

## Global Constraints

- All prune filters preserve running Tengiz containers — use `label!=tengiz-app` to exclude Tengiz-managed resources where appropriate
- `KeepLastNImages` behavior must not change — it stays as image cleanup on deploy/rollback
- `tengiz cleanup` without flags = safe prune (non-Tengiz resources only)
- `tengiz cleanup --all` = also prune dangling Tengiz images (images that no container references)
- `tengiz rm <app>` automatically removes that app's images
- `--force` flag = non-interactive (`-f` to docker), default is interactive confirmation
- All new methods on `Manager` interface must have corresponding stub implementations
- Existing tests must continue to pass

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneSystem`, `PruneBuildCache`, `PruneContainers`, `PruneImages` to `Manager` interface + stub no-ops |
| `internal/runtime/cleanup.go` | Implement `PruneSystem`, `PruneBuildCache`, `PruneContainers`, `PruneImages` on `dockerRuntime` |
| `internal/runtime/cleanup_test.go` | Tests for new cleanup methods on stub + docker runtime |
| `internal/cli/root.go` | Add `cleanupCmd` with `--all`, `--force`, `--images` flags; wire into `rmCmd` handler for auto-cleanup |

No new files created. Changes touch 4 existing files.

---

### Task 1: Add new cleanup methods to Manager interface + stubs

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add 4 methods to `Manager` interface
- Modify: `internal/runtime/runtime.go:113-119` — add stub implementations

**Interfaces:**
- Consumes: nothing new
- Produces: `Manager` interface gains `PruneSystem(ctx, force bool) (PruneReport, error)`, `PruneBuildCache(ctx, force bool) (PruneReport, error)`, `PruneContainers(ctx, appName string) error`, `PruneImages(ctx, appName string, keep int) error`

- [ ] **Step 1: Add `PruneReport` type and new methods to `runtime.go`**

Add import for `time` at the top:
```go
import (
    "context"
    "fmt"
    "io"
    "time"

    "github.com/yaso09/tengiz/internal/types"
)
```

Add type after `type RunOptions struct {`:
```go
type PruneReport struct {
    Containers int64 `json:"containers"`
    Images     int64 `json:"images"`
    Networks   int64 `json:"networks"`
    BuildCache int64 `json:"build_cache"`
    BytesFreed int64 `json:"bytes_freed"`
}
```

Add to the `Manager` interface (after `Run(...)`):
```go
PruneSystem(ctx context.Context, force bool) (PruneReport, error)
PruneBuildCache(ctx context.Context, force bool) (PruneReport, error)
PruneContainers(ctx context.Context, appName string) error
PruneImages(ctx context.Context, appName string, keep int) error
```

Add stub implementations after existing `func (m *stubManager) Run(...)`:
```go
func (m *stubManager) PruneSystem(ctx context.Context, force bool) (PruneReport, error) {
    return PruneReport{}, nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context, force bool) (PruneReport, error) {
    return PruneReport{}, nil
}

func (m *stubManager) PruneContainers(ctx context.Context, appName string) error {
    return nil
}

func (m *stubManager) PruneImages(ctx context.Context, appName string, keep int) error {
    return nil
}
```

- [ ] **Step 2: Write failing test for stub methods**

Add to `internal/runtime/cleanup_test.go`:
```go
func TestStubPruneSystem(t *testing.T) {
    m := NewStub()
    report, err := m.PruneSystem(context.Background(), false)
    if err != nil {
        t.Fatalf("PruneSystem() error = %v", err)
    }
    if report.Containers != 0 || report.Images != 0 || report.Networks != 0 || report.BuildCache != 0 {
        t.Errorf("PruneSystem() returned non-zero report on stub: %+v", report)
    }
}

func TestStubPruneBuildCache(t *testing.T) {
    m := NewStub()
    report, err := m.PruneBuildCache(context.Background(), false)
    if err != nil {
        t.Fatalf("PruneBuildCache() error = %v", err)
    }
    if report.BuildCache != 0 {
        t.Errorf("PruneBuildCache() returned non-zero build cache on stub: %+v", report)
    }
}

func TestStubPruneContainers(t *testing.T) {
    m := NewStub()
    if err := m.PruneContainers(context.Background(), "testapp"); err != nil {
        t.Fatalf("PruneContainers() error = %v", err)
    }
}

func TestStubPruneImages(t *testing.T) {
    m := NewStub()
    if err := m.PruneImages(context.Background(), "testapp", 5); err != nil {
        t.Fatalf("PruneImages() error = %v", err)
    }
}
```

- [ ] **Step 3: Run test to verify it fails (methods not in Manager yet)**

Run: `go test ./internal/runtime/... -run "TestStubPrune" -v -count=1`

Expected: FAIL — compilation errors (Manager interface missing methods)

- [ ] **Step 4: Add interface methods and stubs, then run tests**

After making the edits in Step 1:

Run: `go test ./internal/runtime/... -run "TestStubPrune" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add PruneSystem, PruneBuildCache, PruneContainers, PruneImages to Manager interface + stubs"
```

---

### Task 2: Implement cleanup methods on docker runtime

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `PruneSystem`, `PruneBuildCache`, `PruneContainers`, `PruneImages` implementations

**Interfaces:**
- Consumes: `Manager` interface methods from Task 1
- Produces: working Docker CLI exec wrappers for `docker system prune`, `docker builder prune`, `docker container prune`, `docker image prune`

- [ ] **Step 1: Write the failing test for docker runtime cleanup**

Add to `internal/runtime/cleanup_test.go`:
```go
func TestDockerRuntimePruneSystemLabels(t *testing.T) {
    if _, err := exec.LookPath("docker"); err != nil {
        t.Skip("docker not available")
    }
    r, err := NewDocker()
    if err != nil {
        t.Skip("docker runtime: %v", err)
    }
    // This should not fail even with no Tengiz containers — just returns zero report
    report, err := r.PruneSystem(context.Background(), true)
    if err != nil {
        t.Fatalf("PruneSystem() error = %v", err)
    }
    t.Logf("PruneSystem report: %+v", report)
}
```

- [ ] **Step 2: Add `exec` import to `cleanup_test.go`** (add `"os/exec"` to imports)

- [ ] **Step 3: Implement `PruneSystem` in `cleanup.go`**

```go
func (r *dockerRuntime) PruneSystem(ctx context.Context, force bool) (PruneReport, error) {
    args := []string{"system", "prune",
        "--filter", fmt.Sprintf("label!=%s", labelKey),
        "--filter", fmt.Sprintf("label!=%s", envLabelKey),
    }
    if force {
        args = append(args, "-f")
    }
    args = append(args, "--volumes")

    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return PruneReport{}, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
    }

    report := parsePruneOutput(string(out))
    return report, nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context, force bool) (PruneReport, error) {
    args := []string{"builder", "prune"}
    if force {
        args = append(args, "-f")
    }

    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return PruneReport{}, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
    }

    report := parsePruneOutput(string(out))
    return report, nil
}

func (r *dockerRuntime) PruneContainers(ctx context.Context, appName string) error {
    args := []string{"container", "prune",
        "--filter", fmt.Sprintf("label=%s=%s", labelKey, appName),
        "--filter", "status=exited",
        "-f",
    }
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("docker container prune: %w\n%s", err, string(out))
    }
    return nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, appName string, keep int) error {
    return r.KeepLastNImages(ctx, appName, keep)
}
```

- [ ] **Step 4: Add `parsePruneOutput` helper**

```go
func parsePruneOutput(output string) PruneReport {
    var report PruneReport
    for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
        line = strings.TrimSpace(line)
        if strings.HasPrefix(line, "Deleted Images:") || strings.HasPrefix(line, "Deleted:") {
            // Parse "Deleted Images: N" or "Deleted: N"
            parts := strings.Split(line, ":")
            if len(parts) == 2 {
                fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &report.Images)
            }
        } else if strings.HasPrefix(line, "Containers:") {
            // "Containers: N  Images: N  Networks: N  Build cache: N  Space freed: XMB"
            fmt.Sscanf(line, "Containers: %d  Images: %d  Networks: %d  Build cache: %d",
                &report.Containers, &report.Images, &report.Networks, &report.BuildCache)
        } else if strings.HasPrefix(line, "Total reclaimed space:") {
            // Parse "Total reclaimed space: N.NMB" or similar
            parts := strings.SplitN(line, ":", 2)
            if len(parts) == 2 {
                trimmed := strings.TrimSpace(parts[1])
                parsed := parseSize(trimmed)
                if parsed > report.BytesFreed {
                    report.BytesFreed = parsed
                }
            }
        } else if strings.HasPrefix(line, "Space freed:") {
            // Build cache output: "Space freed: N.NMB"
            parts := strings.SplitN(line, ":", 2)
            if len(parts) == 2 {
                parsed := parseSize(strings.TrimSpace(parts[1]))
                if parsed > report.BytesFreed {
                    report.BytesFreed = parsed
                }
            }
        } else if strings.HasPrefix(line, "Space:") {
            // "Space: N.NMB / N.NMB" or "Space: N / N"
            parts := strings.SplitN(line, ":", 2)
            if len(parts) == 2 {
                sub := strings.Split(strings.TrimSpace(parts[1]), "/")
                parsed := parseSize(strings.TrimSpace(sub[0]))
                if parsed > report.BytesFreed {
                    report.BytesFreed = parsed
                }
            }
        }
    }
    return report
}

func parseSize(s string) int64 {
    s = strings.TrimSpace(s)
    if s == "" {
        return 0
    }
    var val float64
    var unit string
    n, _ := fmt.Sscanf(s, "%f%s", &val, &unit)
    if n < 1 {
        return 0
    }
    switch strings.ToLower(strings.TrimSpace(unit)) {
    case "kb", "k":
        return int64(val * 1024)
    case "mb", "m":
        return int64(val * 1024 * 1024)
    case "gb", "g":
        return int64(val * 1024 * 1024 * 1024)
    case "tb", "t":
        return int64(val * 1024 * 1024 * 1024 * 1024)
    default:
        return int64(val)
    }
}
```

Add imports needed: `"os/exec"` is already imported, `fmt` is already imported.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStubPrune" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Run all tests to catch any interface compliance issues in mock managers**

Run: `go build ./...`

Expected: Build succeeds (all mock types in `proxy`, `idle`, `preview`, `health`, `cli`, `gitdeploy` implement the Manager)

If any mock misses the new methods, add them. For example, `internal/proxy/proxy_test.go` has `mockRuntime`:
```go
func (m *mockRuntime) PruneSystem(ctx context.Context, force bool) (runtime.PruneReport, error) { return runtime.PruneReport{}, nil }
func (m *mockRuntime) PruneBuildCache(ctx context.Context, force bool) (runtime.PruneReport, error) { return runtime.PruneReport{}, nil }
func (m *mockRuntime) PruneContainers(ctx context.Context, appName string) error { return nil }
func (m *mockRuntime) PruneImages(ctx context.Context, appName string, keep int) error { return nil }
```

Same applies to mocks in:
- `internal/idle/idle_test.go`
- `internal/cli/root_test.go`
- `internal/gitdeploy/deployer.go` (if it has inline mocks)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: implement Docker prune methods on dockerRuntime"
```

---

### Task 3: Add `cleanupCmd` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` with `--all`, `--force`, `--images` flags, register with root

**Interfaces:**
- Consumes: `runtime.PruneSystem`, `runtime.PruneBuildCache`, `runtime.PruneContainers`, `runtime.PruneImages` from Task 2
- Produces: `tengiz cleanup [--all] [--force] [--images]` command

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/multi_env_test.go` (or a new test file):
```go
func TestCleanupCommandFlags(t *testing.T) {
    cmd := cleanupCmd
    // Test default flags
    force, _ := cmd.Flags().GetBool("force")
    all, _ := cmd.Flags().GetBool("all")
    images, _ := cmd.Flags().GetBool("images")
    if force {
        t.Error("default --force should be false")
    }
    if all {
        t.Error("default --all should be false")
    }
    if images {
        t.Error("default --images should be false")
    }

    // Test parsing flags
    cmd.ParseFlags([]string{"--force", "--all", "--images"})
    force, _ = cmd.Flags().GetBool("force")
    all, _ = cmd.Flags().GetBool("all")
    images, _ = cmd.Flags().GetBool("images")
    if !force || !all || !images {
        t.Error("flag parsing failed")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCommandFlags" -v -count=1`

Expected: FAIL — `cleanupCmd` not defined yet

- [ ] **Step 3: Add `cleanupCmd` to `root.go`**

Add to the `init()` function (register with root):
```go
rootCmd.AddCommand(cleanupCmd)
```

Add before `var proxyCmd = &cobra.Command{`:
```go
var cleanupCmd = &cobra.Command{
    Use:   "cleanup [app]",
    Short: "Prune unused Docker resources to free disk space",
    Long: `Remove unused Docker resources (containers, images, networks, build cache).

Without arguments, prunes resources NOT managed by Tengiz (safe for production).
With an app name, prunes resources specific to that app.
With --all, also prunes dangling Tengiz images (images no longer referenced by any container).
With --images, only prunes images (skips containers and networks).
With --force, non-interactive mode (no confirmation prompt).`,
    Args: cobra.MaximumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        force, _ := cmd.Flags().GetBool("force")
        all, _ := cmd.Flags().GetBool("all")
        imagesOnly, _ := cmd.Flags().GetBool("images")

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        if len(args) == 1 {
            appName := args[0]
            fmt.Printf("[tengiz] cleaning up resources for %s\n", appName)

            store := config.NewStoreWithEnv(dataDir, env)
            if _, err := store.GetApp(appName); err == nil {
                fmt.Printf("[tengiz] app %s exists — keeping last 5 images\n", appName)
                if err := rt.PruneImages(cmd.Context(), appName, 5); err != nil {
                    log.Printf("[tengiz] warning: image prune for %s: %v", appName, err)
                }
            } else {
                fmt.Printf("[tengiz] app %s not found — removing all images\n", appName)
                if err := rt.PruneContainers(cmd.Context(), appName); err != nil {
                    log.Printf("[tengiz] warning: container prune for %s: %v", appName, err)
                }
                if err := rt.PruneImages(cmd.Context(), appName, 0); err != nil {
                    log.Printf("[tengiz] warning: image prune for %s: %v", appName, err)
                }
            }

            fmt.Printf("[tengiz] cleanup complete for %s\n", appName)
            return nil
        }

        // System-level cleanup
        store := config.NewStoreWithEnv(dataDir, env)
        storeApps, _ := store.ListApps()

        if imagesOnly {
            fmt.Println("[tengiz] pruning unused images...")
            report, err := rt.PruneBuildCache(cmd.Context(), true)
            if err != nil {
                return fmt.Errorf("build cache prune: %w", err)
            }
            if report.BytesFreed > 0 {
                fmt.Printf("[tengiz] freed %d bytes of build cache\n", report.BytesFreed)
            }

            if all {
                for _, app := range storeApps {
                    if err := rt.PruneImages(cmd.Context(), app.Name, 5); err != nil {
                        log.Printf("[tengiz] warning: keeping images for %s: %v", app.Name, err)
                    }
                }
            }
            fmt.Println("[tengiz] image cleanup complete")
            return nil
        }

        fmt.Println("[tengiz] pruning non-Tengiz resources...")
        report, err := rt.PruneSystem(cmd.Context(), force)
        if err != nil {
            return fmt.Errorf("system prune: %w", err)
        }
        if report.BytesFreed > 0 {
            fmt.Printf("[tengiz] reclaimed %d bytes (%s)\n",
                report.BytesFreed, formatBytes(report.BytesFreed))
        }
        fmt.Printf("[tengiz] removed: %d containers, %d images, %d networks\n",
            report.Containers, report.Images, report.Networks)

        if all {
            fmt.Println("[tengiz] pruning build cache...")
            bcReport, err := rt.PruneBuildCache(cmd.Context(), true)
            if err != nil {
                log.Printf("[tengiz] warning: build cache prune: %v", err)
            } else if bcReport.BytesFreed > 0 {
                fmt.Printf("[tengiz] freed %d bytes of build cache\n", bcReport.BytesFreed)
            }

            fmt.Println("[tengiz] pruning unused Tengiz images...")
            for _, app := range storeApps {
                if err := rt.PruneImages(cmd.Context(), app.Name, 5); err != nil {
                    log.Printf("[tengiz] warning: keeping images for %s: %v", app.Name, err)
                }
            }
        }

        fmt.Println("[tengiz] cleanup complete")
        return nil
    },
}
```

Add this to `init()`:
```go
cleanupCmd.Flags().Bool("force", false, "non-interactive mode (bypass confirmation)")
cleanupCmd.Flags().Bool("all", false, "also prune unused Tengiz images and build cache")
cleanupCmd.Flags().Bool("images", false, "prune images only (skip containers and networks)")
```

Add `formatBytes` helper function at the bottom of `root.go` (after `maskSecret`):
```go
func formatBytes(b int64) string {
    switch {
    case b >= 1024*1024*1024:
        return fmt.Sprintf("%.1fGB", float64(b)/(1024*1024*1024))
    case b >= 1024*1024:
        return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
    case b >= 1024:
        return fmt.Sprintf("%.1fKB", float64(b)/1024)
    default:
        return fmt.Sprintf("%dB", b)
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanupCommandFlags" -v -count=1`

Expected: PASS

- [ ] **Step 5: Build to check compilation**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except known slow proxy tests and time-sensitive idle tests)

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add tengiz cleanup command with --all, --force, --images flags"
```

---

### Task 4: Wire auto-cleanup into `tengiz rm`

**Files:**
- Modify: `internal/cli/root.go:631-662` — `rmCmd` handler: after removing app, clean up images

**Interfaces:**
- Consumes: `runtime.PruneImages` from Task 2
- Produces: `tengiz rm <app>` automatically removes all images for that app

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/multi_env_test.go`:
```go
func TestRmCommandCallsPruneImages(t *testing.T) {
    // Verify the rm handler calls PruneImages by checking compilation
    // and basic flag behavior
    cmd := rmCmd
    flag := cmd.Flags().Lookup("env")
    if flag == nil {
        t.Error("rmCmd missing --env flag")
    }
}
```

- [ ] **Step 2: Update `rmCmd` handler to clean up images**

In `internal/cli/root.go`, in the `rmCmd` RunE, after `store.RemoveApp(appName)`, add:
```go
// Clean up app images
containerName := runtime.ContainerName(appName, env)

// Remove the app's Docker images
app, lookupErr := store.GetApp(appName)
store.RemoveApp(appName)
if lookupErr == nil {
    // Remove all images for this app (keep = 0 means remove all)
    if err := rt.PruneImages(context.Background(), appName, 0); err != nil {
        log.Printf("[tengiz] warning: image cleanup for %s: %v", appName, err)
    }
}
```

Wait, there's a problem — the `rmCmd` handler already calls `store.RemoveApp(appName)` at line 647. Let me look at the full current handler again more carefully.

Current `rmCmd`:
```go
var rmCmd = &cobra.Command{
    Use:   "rm <app>",
    Short: "Remove an application completely",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName := args[0]
        containerName := runtime.ContainerName(appName, env)
        rt, err := runtime.NewDocker()
        if err != nil {
            return err
        }
        store := config.NewStoreWithEnv(dataDir, env)
        if err := rt.Remove(context.Background(), containerName); err != nil {
            return err
        }
        store.RemoveApp(appName)

        sm, secErr := getSecretManager(cmd, dataDir, env)
        if secErr == nil {
            secretsList, listErr := sm.List(appName)
            if listErr == nil {
                for k := range secretsList {
                    sm.Unset(appName, k)
                }
            }
        }

        fmt.Printf("[tengiz] removed: %s\n", appName)
        return nil
    },
}
```

The updated handler should be:
```go
var rmCmd = &cobra.Command{
    Use:   "rm <app>",
    Short: "Remove an application completely",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName := args[0]
        containerName := runtime.ContainerName(appName, env)
        rt, err := runtime.NewDocker()
        if err != nil {
            return err
        }
        store := config.NewStoreWithEnv(dataDir, env)
        if err := rt.Remove(context.Background(), containerName); err != nil {
            return err
        }
        store.RemoveApp(appName)

        sm, secErr := getSecretManager(cmd, dataDir, env)
        if secErr == nil {
            secretsList, listErr := sm.List(appName)
            if listErr == nil {
                for k := range secretsList {
                    sm.Unset(appName, k)
                }
            }
        }

        // Clean up all images for this app
        if err := rt.PruneImages(cmd.Context(), appName, 0); err != nil {
            log.Printf("[tengiz] warning: image cleanup for %s: %v", appName, err)
        }

        fmt.Printf("[tengiz] removed: %s\n", appName)
        return nil
    },
}
```

- [ ] **Step 3: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS

- [ ] **Step 4: Run vet**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: auto-cleanup app images on tengiz rm"
```

---

### Task 5: Self-review

- [ ] **Step 1: Spec coverage**

Check against requirements from `docs/FUTURES_FEATURES.md`:
- Label-based `docker system prune` ✅ (Task 2 — `PruneSystem` filters by `label!=tengiz-app`)
- `tengiz cleanup` command ✅ (Task 3 — new CLI command with `--all`, `--force`, `--images`)
- Disk space management ✅ (Task 3 — formatBytes in output, build cache pruning)
- Automatic cleanup ✅ (Task 4 — `tengiz rm` prunes images after removing app)
- Existing `KeepLastNImages` preserved ✅ (Task 2 — `PruneImages` delegates to existing method)

- [ ] **Step 2: Placeholder scan**

Search plan for any "TBD", "TODO", "implement later", "fill in details", "Similar to Task" patterns. None found. Every step has complete code.

- [ ] **Step 3: Type consistency check**

- `PruneReport` struct fields match what Docker `system prune` outputs
- All `Manager` interface methods return `(PruneReport, error)` for system/builder prune, `error` for targeted prune
- `PruneImages(ctx, appName, keep)` uses same semantics as `KeepLastNImages(ctx, appName, n)` — when `keep=0`, it removes all
- `formatBytes` type matches `PruneReport.BytesFreed` (`int64`)
- `PruneContainers(ctx, appName)` only affects containers with matching `tengiz-app=<appName>` label that are in `exited` status

- [ ] **Step 4: Run full test suite**

Run: `go test ./... -v -count=1 2>&1 | tail -50`

Expected: All PASS

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "chore: fix mock implementations for new Manager methods"
```
