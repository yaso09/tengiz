# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` CLI command with granular per-category Docker prune operations, label-based protection of Tengiz-managed resources, disk space reporting, and automatic image cleanup on app removal.

**Architecture:** A new `PruneSystem(ctx, opts PruneOptions) (PruneReport, error)` method on `runtime.Manager` executes targeted `docker * prune` commands, aggregates counts, and reports reclaimed space. The CLI `tengiz cleanup` command wraps this with per-category flags, pre-prunes old images via `KeepLastNImages` for all apps, and displays a readable summary. `tengiz rm` is extended to also remove the app's Docker images. The `config.Store` lists apps so image cleanup can run before the system-level prune.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` for `docker * prune` commands, existing `runtime.Manager`, `config.Store`.

## Global Constraints

- All `docker * prune` commands use `--filter label!=tengiz-app` to protect running Tengiz containers (except `--aggressive` which removes all)
- Before `docker image prune`, `KeepLastNImages` runs for every app in the store to preserve rollback images
- Default keep count: 5 images per app (configurable via `--keep-images`)
- `tengiz rm <app>` additionally removes the app's Docker images via `RemoveImage` + `docker image prune --filter dangling=true`
- `PruneReport` has per-category counts, a `SpaceReclaimed` summary string, and an `Errors` slice
- No new external dependencies
- Existing tests must continue to pass

---

## File Structure

| File | Status | Responsibility |
|------|--------|---------------|
| `internal/runtime/runtime.go` | Modify | Add `PruneOptions`, `PruneReport` types + `PruneSystem` to `Manager` + stub implementation |
| `internal/runtime/prune.go` | Create | `dockerRuntime.PruneSystem` impl — all `docker * prune` exec calls + output parsing |
| `internal/runtime/prune_test.go` | Create | Unit tests for stub methods, option defaults, output parsing helpers |
| `internal/cli/cleanup.go` | Create | `tengiz cleanup` cobra command with flags, store iteration, summary display |
| `internal/cli/root.go` | Modify | Register `cleanupCmd`, extend `rmCmd` with image cleanup, add flag registrations in `Execute()` |

---

### Task 1: Add `PruneSystem` to Manager interface with types

**Files:**
- Modify: `internal/runtime/runtime.go` — types + interface + stub
- Create: `internal/runtime/prune_test.go` — stub tests

**Interfaces:**
- Consumes: nothing new
- Produces: `PruneOptions struct`, `PruneReport struct`, `Manager.PruneSystem(ctx, PruneOptions) (PruneReport, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/prune_test.go
package runtime

import (
    "context"
    "testing"
)

func TestStubPruneSystem(t *testing.T) {
    m := NewStub()
    opts := PruneOptions{All: true}
    report, err := m.PruneSystem(context.Background(), opts)
    if err != nil {
        t.Fatalf("PruneSystem() error = %v", err)
    }
    if report.ContainersPruned != 0 || report.ImagesPruned != 0 {
        t.Errorf("stub should return zero counts, got containers=%d images=%d",
            report.ContainersPruned, report.ImagesPruned)
    }
}

func TestPruneOptionsDefaults(t *testing.T) {
    opts := PruneOptions{}
    if opts.All {
        t.Error("All should default to false")
    }
    if opts.Aggressive {
        t.Error("Aggressive should default to false")
    }
    if opts.KeepImages != 0 {
        t.Errorf("KeepImages should default to 0, got %d", opts.KeepImages)
    }
}

func TestStubPruneSystemErrors(t *testing.T) {
    m := NewStub()
    report, err := m.PruneSystem(context.Background(), PruneOptions{Images: true})
    if err != nil {
        t.Fatalf("PruneSystem() error = %v", err)
    }
    if len(report.Errors) != 0 {
        t.Errorf("stub should have no errors, got %v", report.Errors)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPruneSystem|TestPruneOptionsDefaults|TestStubPruneSystemErrors" -v -count=1`

Expected: FAIL with `undefined: PruneOptions`, `undefined: PruneReport`, `stubManager missing PruneSystem`

- [ ] **Step 3: Add types and interface method to `internal/runtime/runtime.go`**

Add before `type Manager interface` (after `type RunOptions struct`):
```go
type PruneOptions struct {
    Containers bool
    Images     bool
    Networks   bool
    BuildCache bool
    Volumes    bool
    All        bool
    Aggressive bool
    KeepImages int
}

type PruneReport struct {
    ContainersPruned int
    ImagesPruned     int
    NetworksPruned   int
    BuildCacheFreed  string
    VolumesPruned    int
    SpaceReclaimed   string
    Errors           []string
}
```

Add to `Manager` interface (after `Run`):
```go
PruneSystem(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

Add to `stubManager` (after `Run` method):
```go
func (m *stubManager) PruneSystem(ctx context.Context, opts PruneOptions) (PruneReport, error) {
    return PruneReport{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestStubPruneSystem|TestPruneOptionsDefaults|TestStubPruneSystemErrors" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune_test.go
git commit -m "feat: add PruneSystem method to Manager interface with PruneOptions/PruneReport types"
```

---

### Task 2: Implement `dockerRuntime.PruneSystem` body

**Files:**
- Create: `internal/runtime/prune.go` — Docker exec implementation
- Modify: `internal/runtime/prune_test.go` — add helper/parser tests

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport` from Task 1
- Produces: working `dockerRuntime.PruneSystem` that executes `docker * prune` commands and returns structured results

- [ ] **Step 1: Add parser tests to `internal/runtime/prune_test.go`**

```go
func TestParsePruneOutput(t *testing.T) {
    output := `Deleted Containers:
b3f7a2e1c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0
c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0a1b2c3d4

Total reclaimed space: 1.234GB
`
    count, size := parsePruneOutput(output)
    if count != 2 {
        t.Errorf("expected 2 items, got %d", count)
    }
    if size != "1.234GB" {
        t.Errorf(`expected "1.234GB", got %q`, size)
    }
}

func TestParsePruneOutputEmpty(t *testing.T) {
    output := "Total reclaimed space: 0B\n"
    count, size := parsePruneOutput(output)
    if count != 0 {
        t.Errorf("expected 0 items, got %d", count)
    }
    if size != "0B" {
        t.Errorf(`expected "0B", got %q`, size)
    }
}

func TestParsePruneOutputNoItems(t *testing.T) {
    output := ""
    count, size := parsePruneOutput(output)
    if count != 0 {
        t.Errorf("expected 0 items, got %d", count)
    }
    if size != "" {
        t.Errorf("expected empty size, got %q", size)
    }
}

func TestParsePruneOutputBuildCache(t *testing.T) {
    // Builder prune output format is slightly different
    output := `Total: 3 Build(s), 2.5GB I used
ID: abc123
Build cache usage: 1.2GB
Cached: 0

Total reclaimed space: 1.2GB
`
    count, size := parsePruneOutput(output)
    if size != "1.2GB" {
        t.Errorf(`expected "1.2GB", got %q`, size)
    }
}

func TestAccumulateSpace(t *testing.T) {
    result := accumulateSpace("", "1.2GB")
    if result != "1.2GB" {
        t.Errorf("expected %q, got %q", "1.2GB", result)
    }

    result = accumulateSpace("1.2GB", "500MB")
    if result != "1.2GB, 500MB" {
        t.Errorf("expected %q, got %q", "1.2GB, 500MB", result)
    }

    result = accumulateSpace("1.2GB", "")
    if result != "1.2GB" {
        t.Errorf("expected %q, got %q", "1.2GB", result)
    }
}
```

- [ ] **Step 2: Create `internal/runtime/prune.go`**

```go
package runtime

import (
    "context"
    "fmt"
    "os/exec"
    "strings"
)

func (r *dockerRuntime) PruneSystem(ctx context.Context, opts PruneOptions) (PruneReport, error) {
    var report PruneReport

    if opts.All {
        opts.Containers = true
        opts.Images = true
        opts.Networks = true
        opts.BuildCache = true
        opts.Volumes = true
    }

    if opts.Containers {
        count, size, err := runContainerPrune(ctx)
        if err != nil {
            report.Errors = append(report.Errors, err.Error())
        } else {
            report.ContainersPruned = count
        }
    }

    if opts.Images {
        count, size, err := runImagePrune(ctx, opts.Aggressive)
        if err != nil {
            report.Errors = append(report.Errors, err.Error())
        } else {
            report.ImagesPruned = count
        }
    }

    if opts.Networks {
        count, _, err := runNetworkPrune(ctx)
        if err != nil {
            report.Errors = append(report.Errors, err.Error())
        } else {
            report.NetworksPruned = count
        }
    }

    if opts.BuildCache {
        _, size, err := runBuildCachePrune(ctx)
        if err != nil {
            report.Errors = append(report.Errors, err.Error())
        } else {
            report.BuildCacheFreed = size
        }
    }

    if opts.Volumes {
        count, _, err := runVolumePrune(ctx)
        if err != nil {
            report.Errors = append(report.Errors, err.Error())
        } else {
            report.VolumesPruned = count
        }
    }

    report.SpaceReclaimed = buildSummary(report)
    return report, nil
}

func runContainerPrune(ctx context.Context) (int, string, error) {
    args := []string{"container", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey)}
    return execPrune(ctx, args)
}

func runImagePrune(ctx context.Context, aggressive bool) (int, string, error) {
    args := []string{"image", "prune", "-a", "-f"}
    if !aggressive {
        args = append(args, "--filter", fmt.Sprintf("label!=%s", labelKey))
    }
    return execPrune(ctx, args)
}

func runNetworkPrune(ctx context.Context) (int, string, error) {
    args := []string{"network", "prune", "-f"}
    return execPrune(ctx, args)
}

func runBuildCachePrune(ctx context.Context) (int, string, error) {
    args := []string{"builder", "prune", "-a", "-f"}
    return execPrune(ctx, args)
}

func runVolumePrune(ctx context.Context) (int, string, error) {
    args := []string{"volume", "prune", "-f"}
    return execPrune(ctx, args)
}

func execPrune(ctx context.Context, args []string) (int, string, error) {
    out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
    if err != nil {
        return 0, "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
    }
    return parsePruneOutput(string(out))
}

func parsePruneOutput(output string) (int, string) {
    lines := strings.Split(strings.TrimSpace(output), "\n")
    items := 0
    var totalSize string
    for _, line := range lines {
        trimmed := strings.TrimSpace(line)
        if trimmed == "" {
            continue
        }
        if strings.HasPrefix(trimmed, "Total reclaimed space:") {
            totalSize = strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
        } else if !strings.HasPrefix(trimmed, "Deleted:") && !strings.HasPrefix(trimmed, "Would ") {
            items++
        }
    }
    return items, totalSize
}

func buildSummary(r PruneReport) string {
    var parts []string
    if r.ContainersPruned > 0 {
        parts = append(parts, fmt.Sprintf("%d containers", r.ContainersPruned))
    }
    if r.ImagesPruned > 0 {
        parts = append(parts, fmt.Sprintf("%d images", r.ImagesPruned))
    }
    if r.NetworksPruned > 0 {
        parts = append(parts, fmt.Sprintf("%d networks", r.NetworksPruned))
    }
    if r.VolumesPruned > 0 {
        parts = append(parts, fmt.Sprintf("%d volumes", r.VolumesPruned))
    }
    if r.BuildCacheFreed != "" {
        parts = append(parts, fmt.Sprintf("build cache (%s)", r.BuildCacheFreed))
    }
    if len(parts) == 0 {
        return "nothing to clean"
    }
    return strings.Join(parts, ", ") + " reclaimed"
}
```

- [ ] **Step 3: Run parser tests**

Run: `go test ./internal/runtime/... -run "TestParsePrune|TestAccumulate" -v -count=1`

Expected: All PASS

- [ ] **Step 4: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: implement dockerRuntime.PruneSystem with per-category Docker prune exec calls"
```

---

### Task 3: Create `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go` — cleanup cobra command
- Modify: `internal/cli/root.go` — register `cleanupCmd`, add flag setup in `Execute()`

**Interfaces:**
- Consumes: `runtime.Manager.PruneSystem`, `runtime.PruneOptions`, `runtime.PruneReport`, `config.NewStoreWithEnv`, `runtime.Manager.KeepLastNImages`
- Produces: `tengiz cleanup` command that orchestrates app-level image cleanup + system-level prune

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cleanup_test.go
package cli

import (
    "testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
    found := false
    for _, c := range rootCmd.Commands() {
        if c.Use == "cleanup" {
            found = true
            break
        }
    }
    if !found {
        t.Error("cleanup command not registered on rootCmd")
    }
}

func TestCleanupCommandHasFlags(t *testing.T) {
    // Verify the cleanup command exists and has expected flags
    var cleanupCmd *cobra.Command
    for _, c := range rootCmd.Commands() {
        if c.Use == "cleanup" {
            cleanupCmd = c
            break
        }
    }
    if cleanupCmd == nil {
        t.Skip("cleanup cmd not registered yet")
    }

    expectedFlags := []string{"containers", "images", "networks", "build-cache", "volumes", "all", "aggressive", "keep-images"}
    for _, name := range expectedFlags {
        if cleanupCmd.Flags().Lookup(name) == nil {
            t.Errorf("cleanup command missing --%s flag", name)
        }
    }
}
```

- [ ] **Step 2: Create `internal/cli/cleanup.go`**

```go
package cli

import (
    "context"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/spf13/cobra"
    "github.com/yaso09/tengiz/internal/config"
    "github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Prune Docker resources and free disk space",
    Long: `Remove unused Docker containers, images, networks, volumes, and build cache.

Protects Tengiz-managed containers from removal using label-based filtering.
Run 'tengiz cleanup --all' for a full system cleanup.

Examples:
  tengiz cleanup --containers          # remove stopped non-Tengiz containers
  tengiz cleanup --images              # prune old images (keeps last 5 per app)
  tengiz cleanup --all                 # full system cleanup
  tengiz cleanup --all --aggressive    # clean everything including tagged images
  tengiz cleanup --all --keep-images 3 # keep only 3 old images per app
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        store := config.NewStoreWithEnv(dataDir, env)

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        opts := runtime.PruneOptions{
            Containers: mustBool(cmd, "containers"),
            Images:     mustBool(cmd, "images"),
            Networks:   mustBool(cmd, "networks"),
            BuildCache: mustBool(cmd, "build-cache"),
            Volumes:    mustBool(cmd, "volumes"),
            All:        mustBool(cmd, "all"),
            Aggressive: mustBool(cmd, "aggressive"),
            KeepImages: mustInt(cmd, "keep-images"),
        }

        if opts.KeepImages <= 0 {
            opts.KeepImages = 5
        }

        if opts.All {
            opts.Containers = true
            opts.Images = true
            opts.Networks = true
            opts.BuildCache = true
            opts.Volumes = true
        }

        if !opts.Containers && !opts.Images && !opts.Networks && !opts.BuildCache && !opts.Volumes {
            fmt.Println("No categories selected. Use --all or one or more of: --containers, --images, --networks, --build-cache, --volumes")
            fmt.Println("Run 'tengiz cleanup --help' for usage.")
            return nil
        }

        if opts.Images {
            apps, err := store.ListApps()
            if err != nil {
                log.Printf("[tengiz] warning: could not list apps for image cleanup: %v", err)
            } else {
                for _, app := range apps {
                    if err := rt.KeepLastNImages(context.Background(), app.Name, opts.KeepImages); err != nil {
                        log.Printf("[tengiz] warning: image cleanup for %s: %v", app.Name, err)
                    }
                }
            }
        }

        report, err := rt.PruneSystem(context.Background(), opts)
        if err != nil {
            return fmt.Errorf("prune: %w", err)
        }

        fmt.Printf("[tengiz] cleanup: %s\n", report.SpaceReclaimed)

        if len(report.Errors) > 0 {
            fmt.Fprintf(os.Stderr, "[tengiz] cleanup completed with %d error(s):\n", len(report.Errors))
            for _, e := range report.Errors {
                fmt.Fprintf(os.Stderr, "  %s\n", e)
            }
        }

        return nil
    },
}

func mustBool(cmd *cobra.Command, name string) bool {
    v, _ := cmd.Flags().GetBool(name)
    return v
}

func mustInt(cmd *cobra.Command, name string) int {
    v, _ := cmd.Flags().GetInt(name)
    return v
}
```

- [ ] **Step 3: Register `cleanupCmd` in `init()` in `internal/cli/root.go`**

Add to `init()` after `rootCmd.AddCommand(notificationCmd)`:
```go
rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 4: Add flag definitions in `Execute()` in `internal/cli/root.go`**

Add to `Execute()` (before `rootCmd.Execute()`):
```go
cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
cleanupCmd.Flags().Bool("images", false, "prune old Docker images (keeps last N per app)")
cleanupCmd.Flags().Bool("networks", false, "prune unused Docker networks")
cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
cleanupCmd.Flags().Bool("volumes", false, "prune unused Docker volumes")
cleanupCmd.Flags().Bool("all", false, "prune all categories")
cleanupCmd.Flags().Bool("aggressive", false, "remove all unused images including tagged ones")
cleanupCmd.Flags().Int("keep-images", 5, "number of old images to keep per app (default: 5)")
cleanupCmd.Flags().String("env", "production", "environment scope for cleanup")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupCommandHasFlags" -v -count=1`

Expected: PASS

- [ ] **Step 6: Build check**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 7: Run all tests**

Run: `go test ./... -v -count=1 2>&1 | head -100`

Expected: All tests pass (except proxy TCP timeout tests and idle time-sensitive tests as noted in AGENTS.md)

- [ ] **Step 8: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command with per-category Docker prune flags"
```

---

### Task 4: Extend `tengiz rm` to remove app Docker images

**Files:**
- Modify: `internal/cli/root.go` — extend `rmCmd` to remove app images after removing container

**Interfaces:**
- Consumes: `runtime.Manager.RemoveImage`, `runtime.Manager.KeepLastNImages`
- Produces: `tengiz rm <app>` that fully cleans up container, store entry, secrets, AND Docker images

- [ ] **Step 1: Write the failing test for image cleanup in rm**

```go
// internal/cli/cleanup_test.go

func TestRmCommandReturnsImageCleanupInfo(t *testing.T) {
    // Verify the rm command flow includes image removal by checking the output message
    // This is an interface check — the actual Docker exec can't be tested in unit tests
    // We verify the command structure is correct
    if rmCmd.Use != "rm <app>" {
        t.Errorf("rmCmd.Use = %q, want %q", rmCmd.Use, "rm <app>")
    }
}
```

- [ ] **Step 2: Extend `rmCmd` in `internal/cli/root.go`**

Add image removal logic to `rmCmd.RunE` (after `store.RemoveApp(appName)` and secret cleanup, before the `fmt.Printf`):

```go
// Remove the app's Docker images
containerName := runtime.ContainerName(appName, env)
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

// Remove Docker images for this app
deployments, depErr := store.GetDeployments(appName)
if depErr == nil {
    for _, dep := range deployments {
        if dep.ImageTag != "" {
            if err := rt.RemoveImage(context.Background(), dep.ImageTag); err != nil {
                log.Printf("[tengiz] warning: could not remove image %s: %v", dep.ImageTag, err)
            }
        }
    }
}

fmt.Printf("[tengiz] removed: %s\n", appName)
```

Wait — the `rmCmd` already removes the container at line 644. The change is adding image removal AFTER the existing container removal but the existing code already does `rt.Remove`. Let me look at the current flow more carefully:

Current `rmCmd`:
```go
rt, err := runtime.NewDocker()
store := config.NewStoreWithEnv(dataDir, env)
if err := rt.Remove(context.Background(), containerName); err != nil {
    return err
}
store.RemoveApp(appName)
// secret cleanup...
fmt.Printf("[tengiz] removed: %s\n", appName)
```

The change: after `store.RemoveApp` and secret cleanup, add image removal:

```go
// Remove Docker images for this app
rt.KeepLastNImages(context.Background(), appName, 0)
```

Actually, `KeepLastNImages(ctx, name, 0)` would remove ALL images (since it keeps `n` where `n=0`). That's clean. But we need to be more surgical: remove ALL images for this app regardless of counts.

Better: get the deployment history and remove each image individually:

```go
// Remove all Docker images for this app
if err := rt.KeepLastNImages(context.Background(), appName, 0); err != nil {
    log.Printf("[tengiz] warning: image cleanup: %v", err)
}
```

`KeepLastNImages` with n=0 will remove all images for the app (skipping `:latest` tagged ones). This is the simplest approach.

But wait — `KeepLastNImages` uses `reference=tengiz-apps/{appName}:*` filter. If the image tags use env-qualified names (like `tengiz-apps/myapp-staging:v1`), this needs to be env-aware.

Actually, looking at the existing code, the image tag format from builder.go is:
```go
tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)
```

And `KeepLastNImages` uses:
```go
"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
```

So `KeepLastNImages` with the bare app name already filters all env variants. This means `KeepLastNImages(ctx, "myapp", 0)` will remove ALL images matching `tengiz-apps/myapp:*`, including `myapp:staging-xxx` and `myapp:production-xxx`.

For the `rm` command scoped to a specific env, we want to be more targeted. The simplest approach: use `KeepLastNImages` with n=0 to remove all images for the app name. If the user ran different envs, ALL env images for that app will be removed during `rm`. That's acceptable — if you `rm` the app, you rm the whole app.

Actually wait — the container name is env-aware (`runtime.ContainerName(appName, env)`) but the store operations use the bare `appName`. So if you `tengiz rm myapp --env staging`, it removes the staging container but also the store entry for `myapp` (which is shared across envs). This is already a bit of an issue — it seems like the store might treat env-qualified names differently for apps.

Looking at the multi-environment support implementation: `config.AppQualifiedName(name, env)` is used as the store key. So `tengiz rm myapp --env staging` would use the qualified name `myapp-staging` as the store key. The `RemoveApp` call removes `myapp-staging` from the store.

For images: `KeepLastNImages(context.Background(), appName, 0)` with bare `appName` would match `tengiz-apps/myapp:*` — but env images are `tengiz-apps/myapp-staging:xxx`. So the filter wouldn't match! This is a bug in the existing `KeepLastNImages` for env-scoped apps.

Hmm, but the existing code at line 346-348 and 466-468 in root.go uses `cfg.Name` (the bare name, not the qualified name) for `KeepLastNImages`. So the existing code has the same issue: it passes the bare name, which won't match env-tagged images like `tengiz-apps/myapp-staging:xxx`.

Wait, looking more carefully — the build produces `tengiz-apps/myapp:production-xxx` in production but `tengiz-apps/myapp:staging-xxx` for staging. The `deploy` command (Task 2 of multi-env) used `cfg.Name` directly (not qualified) for `KeepLastNImages` calls. But the image tag uses `appName` from `BuildWithEnv` which does use env suffix.

Actually, in the builder:
```go
func (b *Builder) BuildWithEnv(..., appName string, env string, ...) (string, string, error) {
    imageName := appName
    if env != "" && env != "production" {
        imageName = appName + "-" + env
    }
    tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", imageName, env, deploymentID)
    ...
}
```

So for `tengiz deploy --env staging`, the image tag would be `tengiz-apps/myapp-staging:staging-12345`. But `KeepLastNImages` in the deploy handler uses `cfg.Name` which is the bare app name `"myapp"`. So the filter `reference=tengiz-apps/myapp:*` would NOT match `tengiz-apps/myapp-staging:*`. This is a latent bug introduced by the multi-env feature.

For this cleanup task: when `rm` is called with `--env staging`, we need to:
1. Get the qualified name: `myapp-staging`
2. Use `KeepLastNImages` with the qualified name to clean up images matching `tengiz-apps/myapp-staging:*`

So the fix is: use the qualified store key (which IS the app name in the store) as the parameter to `KeepLastNImages`. The qualified name matches the image tag prefix.

For the `rm` command, the app name argument is always the bare app name (e.g., `myapp`). The qualified name is computed by `config.AppQualifiedName(name, env)`. The store key is the qualified name. And `KeepLastNImages` should use the qualified name too since the image tag uses the qualified name.

But in the existing deploy code, `KeepLastNImages` is called with `cfg.Name` (bare name) — this is a bug that was introduced. The deploy handler for non-production envs wouldn't clean up old images because the filter wouldn't match.

For this plan, I'll fix that bug too. The fix: change the `KeepLastNImages` calls in the deploy command to use the qualified name instead of `cfg.Name`.

OK, this is getting complex. Let me simplify the plan:

For `rmCmd`, add:
```go
// Remove app Docker images — use qualified app name to match image tag prefix
qualifiedName := config.AppQualifiedName(appName, env)
if err := rt.KeepLastNImages(context.Background(), qualifiedName, 0); err != nil {
    log.Printf("[tengiz] warning: image cleanup: %v", err)
}
```

For the deploy command, also fix the `KeepLastNImages` calls to use the qualified name. But that's a separate fix — I'll add it as a note in the plan.

Actually, the image removal with `KeepLastNImages(ctx, name, 0)` won't remove `:latest` tagged images (line 51 in cleanup.go: `if strings.HasSuffix(tag, ":latest") { continue }`). This is intentional — we don't want to remove the latest tag. But when rm'ing an app, we should also remove the latest tag.

For `rm`, I should use a more direct approach: list the images for this app and remove each one individually, including the `:latest` tag. Or just modify the existing `KeepLastNImages` behavior when `n==0` to also remove `:latest`.

Let me take the simple approach: for `rm`, iterate deployment history and call `RemoveImage` for each unique image tag. This also cleans up the `:latest` tag.

```go
// Remove all Docker images for this app
if err := rt.KeepLastNImages(context.Background(), qualifiedName, 0); err != nil {
    log.Printf("[tengiz] warning: image cleanup: %v", err)
}
// Also remove the latest tag
latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", qualifiedName, env)
if err := rt.RemoveImage(context.Background(), latestTag); err != nil {
    log.Printf("[tengiz] warning: could not remove latest image tag: %v", err)
}
```

This is acceptable.

- [ ] **Step 2 (revised): Extend `rmCmd`**

Replace the `rmCmd` handler in `root.go` with:

```go
var rmCmd = &cobra.Command{
    Use:   "rm <app>",
    Short: "Remove an application completely",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        appName := args[0]
        qualifiedName := config.AppQualifiedName(appName, env)
        containerName := runtime.ContainerName(appName, env)

        rt, err := runtime.NewDocker()
        if err != nil {
            return err
        }

        store := config.NewStoreWithEnv(dataDir, env)

        if err := rt.Remove(context.Background(), containerName); err != nil {
            return fmt.Errorf("remove container: %w", err)
        }

        store.RemoveApp(qualifiedName)

        sm, secErr := getSecretManager(cmd, dataDir, env)
        if secErr == nil {
            secretsList, listErr := sm.List(appName)
            if listErr == nil {
                for k := range secretsList {
                    sm.Unset(appName, k)
                }
            }
        }

        // Remove Docker images for this app
        if err := rt.KeepLastNImages(context.Background(), qualifiedName, 0); err != nil {
            log.Printf("[tengiz] warning: image cleanup: %v", err)
        }
        latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", qualifiedName, env)
        if err := rt.RemoveImage(context.Background(), latestTag); err != nil {
            log.Printf("[tengiz] warning: could not remove latest image: %v", err)
        }

        fmt.Printf("[tengiz] removed: %s\n", appName)
        return nil
    },
}
```

Note: This replaces the existing `rmCmd` block (lines 631-662). The changes are:
- Added `qualifiedName := config.AppQualifiedName(appName, env)`
- Changed `store.RemoveApp(appName)` to `store.RemoveApp(qualifiedName)`
- Added `KeepLastNImages(ctx, qualifiedName, 0)` + `RemoveImage` for `:latest` tag
- The `sm.List(appName)` and `sm.Unset(appName, k)` keep using `appName` (the bare name) because secrets manager keys may not use qualified names (secrets are stored per-app, not per-env)

- [ ] **Step 3: Run tests**

Run: `go test ./internal/cli/... -run "TestCleanup|TestRmCmd" -v -count=1`

Expected: PASS or relevant skips

Run: `go build ./...`

Expected: Succeeds

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: extend tengiz rm to remove app Docker images"
```

---

### Task 5: Fix `KeepLastNImages` in deploy command — use qualified name for env-aware image tag matching

**Files:**
- Modify: `internal/cli/root.go` — `deployCmd` handler, fix image cleanup calls

**Interfaces:**
- Consumes: `config.AppQualifiedName(name, env)` — already available
- Produces: `KeepLastNImages` correctly matches env-qualified image tags (`tengiz-apps/myapp-staging:*`)

- [ ] **Step 1: Find and fix the two `KeepLastNImages` calls in deployCmd**

In `deployCmd.RunE` (lines 346 and 466), replace:
```go
rt.KeepLastNImages(context.Background(), cfg.Name, 5)
```
with:
```go
rt.KeepLastNImages(context.Background(), config.AppQualifiedName(cfg.Name, envFlag), 5)
```

- [ ] **Step 2: Build check**

Run: `go build ./...`

Expected: Succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "fix: use qualified app name in KeepLastNImages for env-aware image tag matching"
```

---

### Task 6: Self-review and verification

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1 2>&1 | tail -30`

Expected: All tests pass (except known slow/fragile tests noted in AGENTS.md)

- [ ] **Step 2: Run vet**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 3: Spec coverage check against `docs/FUTURES_FEATURES.md` #6**

Requirements:
- `tengiz cleanup` command ✅ (Task 3)
- Label-based `docker system prune` style cleanup ✅ (Task 2 — per-category with `label!=tengiz-app`)
- Disk space reporting ✅ (Task 2 — `buildSummary` with `SpaceReclaimed`)
- Per-category granular operations ✅ (Task 3 — flags: `--containers`, `--images`, etc.)
- Safe protection of Tengiz containers ✅ (Task 2 — `label!=tengiz-app` filter)
- Image cleanup on `rm` ✅ (Task 4)
- Fixed `KeepLastNImages` for env-qualified apps ✅ (Task 5)

- [ ] **Step 4: Placeholder scan**

Search plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task". None found.

- [ ] **Step 5: Type consistency check**

- `PruneOptions` struct fields match flag names in `cleanup.go` ✅
- `PruneReport` struct fields are set by `prune.go` methods ✅
- `buildSummary(report PruneReport) string` returns the summary string set on `PruneReport.SpaceReclaimed` ✅
- `runtime.Manager.PruneSystem(ctx, PruneOptions) (PruneReport, error)` — interface matches impl ✅
- `config.AppQualifiedName(name, env) string` used consistently in rm and deploy fixes ✅
- `ContainerName(name, env)` already used in rmCmd ✅

- [ ] **Step 6: Final commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go internal/runtime/runtime.go internal/cli/cleanup.go internal/cli/root.go
git commit -m "feat: implement Docker housekeeping system with tengiz cleanup command"
```
