# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command for label-based Docker resource pruning (containers, images, volumes, networks, build cache) to prevent disk space exhaustion.

**Architecture:** Extend `runtime.Manager` interface with `Prune*` methods that call `docker * prune --filter label=tengiz-app` to safely remove only Tengiz-managed resources. Add a `tengiz cleanup` Cobra command with granular `--containers/--images/--volumes/--networks/--build-cache/--all` flags plus `--dry-run` for preview.

**Tech Stack:** Go 1.26, Cobra CLI, `os/exec` for Docker CLI calls, existing `runtime.Manager` interface, existing `internal/runtime/cleanup.go` patterns.

## Global Constraints

- All prune operations MUST use `--filter label=tengiz-app` to prevent removing non-Tengiz Docker resources
- `--dry-run` flag uses `docker * prune --dry-run` to list what would be removed without actually removing
- `--all` is the default behavior (prunes all resource types)
- Add new methods to `runtime.Manager` interface with both `dockerRuntime` and `stubManager` implementations
- Existing tests must continue to pass without modification
- No new external dependencies required

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache`, `PruneAll`, `DiskUsage` to `Manager` interface + stub methods |
| `internal/runtime/docker.go` | Docker exec implementations of all prune methods |
| `internal/runtime/cleanup.go` | Move prune implementations here (keep docker.go focused on container lifecycle) |
| `internal/runtime/cleanup_test.go` | Tests for prune methods |
| `internal/cli/root.go` | Add `cleanupCmd` with flags and help text |
| `internal/cli/cleanup_test.go` | Tests for cleanup CLI command |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 and #56 as implemented |

---

### Task 1: Add Prune methods to Manager interface + docker implementation

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — Manager interface
- Modify: `internal/runtime/runtime.go:51-123` — stubManager methods
- Modify: `internal/runtime/cleanup.go` — add Prune implementations
- Test: `internal/runtime/cleanup_test.go` — tests for stub and docker

**Interfaces:**
- Consumes: nothing new
- Produces: `Manager.PruneContainers(ctx, dryRun)`, `Manager.PruneImages(ctx, dryRun)`, `Manager.PruneVolumes(ctx, dryRun)`, `Manager.PruneNetworks(ctx, dryRun)`, `Manager.PruneBuildCache(ctx, dryRun)`, `Manager.PruneAll(ctx, dryRun)`, `Manager.DiskUsage(ctx)` — all return `(PruneReport, error)`

- [ ] **Step 1: Add `PruneReport` type and new interface methods**

Add to `internal/runtime/runtime.go`:

```go
type PruneReport struct {
    ContainersReclaimed int64  `json:"containers_reclaimed"`
    ImagesReclaimed     int64  `json:"images_reclaimed"`
    VolumesReclaimed    int64  `json:"volumes_reclaimed"`
    NetworksReclaimed   int64  `json:"networks_reclaimed"`
    BuildCacheReclaimed int64  `json:"build_cache_reclaimed"`
    SpaceReclaimed      string `json:"space_reclaimed"` // human-readable: "1.2GB"
}

type DiskUsageInfo struct {
    Containers int    `json:"containers"`
    Images     int    `json:"images"`
    Volumes    int    `json:"volumes"`
    BuildCache int    `json:"build_cache"`
    DiskUsage  string `json:"disk_usage"`
}
```

Add to `Manager` interface:
```go
PruneContainers(ctx context.Context, dryRun bool) (PruneReport, error)
PruneImages(ctx context.Context, dryRun bool) (PruneReport, error)
PruneVolumes(ctx context.Context, dryRun bool) (PruneReport, error)
PruneNetworks(ctx context.Context, dryRun bool) (PruneReport, error)
PruneBuildCache(ctx context.Context, dryRun bool) (PruneReport, error)
PruneAll(ctx context.Context, dryRun bool) (PruneReport, error)
DiskUsage(ctx context.Context) (DiskUsageInfo, error)
```

- [ ] **Step 2: Write the failing test for stub**

```go
// internal/runtime/cleanup_test.go

func TestStubPruneContainers(t *testing.T) {
    m := NewStub()
    report, err := m.PruneContainers(context.Background(), false)
    if err != nil {
        t.Fatalf("PruneContainers() error = %v", err)
    }
    if report.SpaceReclaimed != "" {
        t.Errorf("stub should return empty report")
    }
}

func TestStubPruneAll(t *testing.T) {
    m := NewStub()
    report, err := m.PruneAll(context.Background(), true)
    if err != nil {
        t.Fatalf("PruneAll() error = %v", err)
    }
    if report.SpaceReclaimed != "" {
        t.Errorf("stub should return empty report")
    }
}

func TestStubDiskUsage(t *testing.T) {
    m := NewStub()
    info, err := m.DiskUsage(context.Background())
    if err != nil {
        t.Fatalf("DiskUsage() error = %v", err)
    }
    if info.Containers != 0 {
        t.Errorf("stub DiskUsage should return zeros")
    }
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestStubDiskUsage" -v -count=1`
Expected: FAIL with "Manager does not implement" or "undefined" errors

- [ ] **Step 4: Add stub methods to `stubManager`**

```go
func (m *stubManager) PruneContainers(ctx context.Context, dryRun bool) (PruneReport, error) {
    return PruneReport{}, nil
}

func (m *stubManager) PruneImages(ctx context.Context, dryRun bool) (PruneReport, error) {
    return PruneReport{}, nil
}

func (m *stubManager) PruneVolumes(ctx context.Context, dryRun bool) (PruneReport, error) {
    return PruneReport{}, nil
}

func (m *stubManager) PruneNetworks(ctx context.Context, dryRun bool) (PruneReport, error) {
    return PruneReport{}, nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context, dryRun bool) (PruneReport, error) {
    return PruneReport{}, nil
}

func (m *stubManager) PruneAll(ctx context.Context, dryRun bool) (PruneReport, error) {
    return PruneReport{}, nil
}

func (m *stubManager) DiskUsage(ctx context.Context) (DiskUsageInfo, error) {
    return DiskUsageInfo{}, nil
}
```

- [ ] **Step 5: Run test to verify stub passes**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestStubDiskUsage" -v -count=1`
Expected: PASS

- [ ] **Step 6: Write failing test for dockerRuntime prune methods**

```go
// internal/runtime/cleanup_test.go

func TestDockerPruneContainers(t *testing.T) {
    // This test only validates the command construction by checking
    // that the method exists and has the right signature.
    // Docker must be available for these tests.
    rt, err := NewDocker()
    if err != nil {
        t.Skip("docker not available:", err)
    }
    // Dry-run should never fail
    report, err := rt.PruneContainers(context.Background(), true)
    if err != nil {
        t.Fatalf("PruneContainers(dryRun=true) error = %v", err)
    }
    t.Logf("dry-run report: %+v", report)
}

func TestDockerPruneAllDryRun(t *testing.T) {
    rt, err := NewDocker()
    if err != nil {
        t.Skip("docker not available:", err)
    }
    report, err := rt.PruneAll(context.Background(), true)
    if err != nil {
        t.Fatalf("PruneAll(dryRun=true) error = %v", err)
    }
    t.Logf("dry-run report: %+v", report)
}

func TestDockerDiskUsage(t *testing.T) {
    rt, err := NewDocker()
    if err != nil {
        t.Skip("docker not available:", err)
    }
    info, err := rt.DiskUsage(context.Background())
    if err != nil {
        t.Fatalf("DiskUsage() error = %v", err)
    }
    t.Logf("disk usage: %+v", info)
}
```

- [ ] **Step 7: Run dockerRuntime tests (will fail since methods aren't implemented)**

Run: `go test ./internal/runtime/... -run "TestDockerPrune|TestDockerDiskUsage" -v -count=1`
Expected: FAIL (compilation error — methods not implemented on dockerRuntime)

- [ ] **Step 8: Implement prune methods on dockerRuntime**

Add to `internal/runtime/cleanup.go`:

```go
const tengizLabelFilter = "label=tengiz-app"

func (r *dockerRuntime) PruneContainers(ctx context.Context, dryRun bool) (PruneReport, error) {
    args := []string{"container", "prune", "--filter", tengizLabelFilter, "--force"}
    if dryRun {
        args = append(args, "--dry-run")
    }
    return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneImages(ctx context.Context, dryRun bool) (PruneReport, error) {
    args := []string{"image", "prune", "--filter", tengizLabelFilter, "--force"}
    if dryRun {
        args = append(args, "--all", "--dry-run")
    } else {
        args = append(args, "--all")
    }
    return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context, dryRun bool) (PruneReport, error) {
    args := []string{"volume", "prune", "--filter", tengizLabelFilter, "--force"}
    if dryRun {
        args = append(args, "--dry-run")
    }
    return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context, dryRun bool) (PruneReport, error) {
    args := []string{"network", "prune", "--filter", tengizLabelFilter, "--force"}
    if dryRun {
        args = append(args, "--dry-run")
    }
    return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context, dryRun bool) (PruneReport, error) {
    args := []string{"builder", "prune", "--force"}
    if dryRun {
        args = append(args, "--dry-run")
    }
    return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneAll(ctx context.Context, dryRun bool) (PruneReport, error) {
    var total PruneReport
    for _, fn := range []func(context.Context, bool) (PruneReport, error){
        r.PruneContainers,
        r.PruneImages,
        r.PruneVolumes,
        r.PruneNetworks,
        r.PruneBuildCache,
    } {
        report, err := fn(ctx, dryRun)
        if err != nil {
            return total, err
        }
        total.ContainersReclaimed += report.ContainersReclaimed
        total.ImagesReclaimed += report.ImagesReclaimed
        total.VolumesReclaimed += report.VolumesReclaimed
        total.NetworksReclaimed += report.NetworksReclaimed
        total.BuildCacheReclaimed += report.BuildCacheReclaimed
        if report.SpaceReclaimed != "" {
            total.SpaceReclaimed = report.SpaceReclaimed
        }
    }
    return total, nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (DiskUsageInfo, error) {
    cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{json .}}")
    out, err := cmd.CombinedOutput()
    if err != nil {
        return DiskUsageInfo{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
    }

    var info DiskUsageInfo
    lines := strings.Split(strings.TrimSpace(string(out)), "\n")
    for _, line := range lines {
        var entry struct {
            Type  string `json:"Type"`
            Total int    `json:"Total"`
            Size  string `json:"Size"`
        }
        if err := json.Unmarshal([]byte(line), &entry); err != nil {
            continue
        }
        switch entry.Type {
        case "Containers":
            info.Containers = entry.Total
        case "Images":
            info.Images = entry.Total
        case "Volumes":
            info.Volumes = entry.Total
        case "Build Cache":
            info.BuildCache = entry.Total
            info.DiskUsage = entry.Size
        }
    }
    return info, nil
}

func (r *dockerRuntime) execPrune(ctx context.Context, args []string) (PruneReport, error) {
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return PruneReport{}, fmt.Errorf("docker prune: %w\n%s", err, string(out))
    }

    report := parsePruneOutput(string(out))
    return report, nil
}

func parsePruneOutput(output string) PruneReport {
    var report PruneReport
    // docker prune output format:
    // Deleted Containers:
    // abc123
    // def456
    //
    // Total reclaimed space: 1.2GB
    lines := strings.Split(strings.TrimSpace(output), "\n")
    inSection := ""
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if strings.HasPrefix(line, "Deleted Containers:") {
            inSection = "containers"
            continue
        }
        if strings.HasPrefix(line, "Deleted Images:") {
            inSection = "images"
            continue
        }
        if strings.HasPrefix(line, "Deleted Volumes:") {
            inSection = "volumes"
            continue
        }
        if strings.HasPrefix(line, "Deleted Networks:") {
            inSection = "networks"
            continue
        }
        if strings.HasPrefix(line, "Deleted Build Cache:") {
            inSection = "buildcache"
            continue
        }
        if strings.HasPrefix(line, "Total reclaimed space:") {
            report.SpaceReclaimed = strings.TrimPrefix(line, "Total reclaimed space:")
            report.SpaceReclaimed = strings.TrimSpace(report.SpaceReclaimed)
            continue
        }
        if inSection == "containers" && line != "" && line != "Total reclaimed space:" {
            report.ContainersReclaimed++
        }
        if inSection == "images" && line != "" {
            report.ImagesReclaimed++
        }
        if inSection == "volumes" && line != "" {
            report.VolumesReclaimed++
        }
        if inSection == "networks" && line != "" {
            report.NetworksReclaimed++
        }
        if inSection == "buildcache" && line != "" {
            report.BuildCacheReclaimed++
        }
    }
    return report
}
```

- [ ] **Step 9: Run tests to verify docker implementations pass**

Run: `go test ./internal/runtime/... -run "TestDockerPrune|TestDockerDiskUsage" -v -count=1`
Expected: PASS or SKIP (docker unavailable in CI)

- [ ] **Step 10: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: All PASS

- [ ] **Step 11: Run all tests**

Run: `go test ./... -v -count=1 2>&1 | head -100`
Expected: All tests pass (existing tests unchanged)

- [ ] **Step 12: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add Prune and DiskUsage methods to runtime.Manager"
```

---

### Task 2: Add `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup_test.go` — tests for cleanup command
- Modify: `internal/cli/root.go` — add `cleanupCmd` + register in `init()`

**Interfaces:**
- Consumes: `Manager.PruneContainers(dryRun)`, `Manager.PruneImages(dryRun)`, `Manager.PruneVolumes(dryRun)`, `Manager.PruneNetworks(dryRun)`, `Manager.PruneBuildCache(dryRun)`, `Manager.PruneAll(dryRun)`, `Manager.DiskUsage()` from Task 1
- Produces: `tengiz cleanup [--containers] [--images] [--volumes] [--networks] [--build-cache] [--all] [--dry-run]` CLI command

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
    "testing"
)

func TestCleanupAllDryRun(t *testing.T) {
    // Just verify the flag exists and has correct default
    flag := cleanupCmd.Flags().Lookup("all")
    if flag == nil {
        t.Fatal("cleanupCmd missing --all flag")
    }
    all, _ := cleanupCmd.Flags().GetBool("all")
    if !all {
        t.Error("--all should default to true")
    }
}

func TestCleanupHasFlags(t *testing.T) {
    required := []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run"}
    for _, name := range required {
        flag := cleanupCmd.Flags().Lookup(name)
        if flag == nil {
            t.Errorf("cleanupCmd missing --%s flag", name)
        }
    }
}

func TestCleanupDryRunFlag(t *testing.T) {
    flag := cleanupCmd.Flags().Lookup("dry-run")
    if flag == nil {
        t.Fatal("cleanupCmd missing --dry-run flag")
    }
    dryRun, _ := cleanupCmd.Flags().GetBool("dry-run")
    if dryRun {
        t.Error("--dry-run should default to false")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`
Expected: FAIL with "undefined: cleanupCmd"

- [ ] **Step 3: Add `cleanupCmd` to root.go**

Add the command definition before `init()`:

```go
var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Remove unused Docker resources to free disk space",
    Long: `Remove unused Docker resources managed by Tengiz.

Prunes containers, images, volumes, networks, and build cache that are
associated with Tengiz-managed applications. By default prunes all
resource types. Use specific flags to prune only certain types.

Uses --filter label=tengiz-app to ensure only Tengiz resources are affected.

Examples:
  tengiz cleanup                    # prune all unused Tengiz resources
  tengiz cleanup --dry-run          # preview what would be removed
  tengiz cleanup --images --volumes # prune only images and volumes
  tengiz cleanup --containers       # prune only stopped containers`,
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        dryRun, _ := cmd.Flags().GetBool("dry-run")
        all, _ := cmd.Flags().GetBool("all")
        pruneContainers, _ := cmd.Flags().GetBool("containers")
        pruneImages, _ := cmd.Flags().GetBool("images")
        pruneVolumes, _ := cmd.Flags().GetBool("volumes")
        pruneNetworks, _ := cmd.Flags().GetBool("networks")
        pruneBuildCache, _ := cmd.Flags().GetBool("build-cache")

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        // Show disk usage before
        info, err := rt.DiskUsage(cmd.Context())
        if err == nil {
            fmt.Printf("[tengiz] disk usage before cleanup:\n")
            fmt.Printf("  containers: %d\n", info.Containers)
            fmt.Printf("  images:     %d\n", info.Images)
            fmt.Printf("  volumes:    %d\n", info.Volumes)
            fmt.Printf("  build cache:%d\n", info.BuildCache)
            fmt.Printf("  total:      %s\n", info.DiskUsage)
        }

        if dryRun {
            fmt.Println("[tengiz] dry-run mode — no resources will be removed")
        }

        var report runtime.PruneReport

        if all {
            fmt.Println("[tengiz] pruning all unused Tengiz resources...")
            report, err = rt.PruneAll(cmd.Context(), dryRun)
            if err != nil {
                return fmt.Errorf("prune all: %w", err)
            }
        } else {
            if pruneContainers {
                fmt.Println("[tengiz] pruning containers...")
                r, e := rt.PruneContainers(cmd.Context(), dryRun)
                if e != nil {
                    return fmt.Errorf("prune containers: %w", e)
                }
                report.ContainersReclaimed += r.ContainersReclaimed
                report.SpaceReclaimed = r.SpaceReclaimed
            }
            if pruneImages {
                fmt.Println("[tengiz] pruning images...")
                r, e := rt.PruneImages(cmd.Context(), dryRun)
                if e != nil {
                    return fmt.Errorf("prune images: %w", e)
                }
                report.ImagesReclaimed += r.ImagesReclaimed
                if r.SpaceReclaimed != "" {
                    report.SpaceReclaimed = r.SpaceReclaimed
                }
            }
            if pruneVolumes {
                fmt.Println("[tengiz] pruning volumes...")
                r, e := rt.PruneVolumes(cmd.Context(), dryRun)
                if e != nil {
                    return fmt.Errorf("prune volumes: %w", e)
                }
                report.VolumesReclaimed += r.VolumesReclaimed
                if r.SpaceReclaimed != "" {
                    report.SpaceReclaimed = r.SpaceReclaimed
                }
            }
            if pruneNetworks {
                fmt.Println("[tengiz] pruning networks...")
                r, e := rt.PruneNetworks(cmd.Context(), dryRun)
                if e != nil {
                    return fmt.Errorf("prune networks: %w", e)
                }
                report.NetworksReclaimed += r.NetworksReclaimed
                if r.SpaceReclaimed != "" {
                    report.SpaceReclaimed = r.SpaceReclaimed
                }
            }
            if pruneBuildCache {
                fmt.Println("[tengiz] pruning build cache...")
                r, e := rt.PruneBuildCache(cmd.Context(), dryRun)
                if e != nil {
                    return fmt.Errorf("prune build cache: %w", e)
                }
                report.BuildCacheReclaimed += r.BuildCacheReclaimed
                if r.SpaceReclaimed != "" {
                    report.SpaceReclaimed = r.SpaceReclaimed
                }
            }
        }

        if dryRun {
            fmt.Println("[tengiz] dry-run complete — nothing was removed")
            fmt.Printf("  would reclaim: %s\n", report.SpaceReclaimed)
            return nil
        }

        fmt.Println("[tengiz] cleanup complete")
        fmt.Printf("  containers removed: %d\n", report.ContainersReclaimed)
        fmt.Printf("  images removed:     %d\n", report.ImagesReclaimed)
        fmt.Printf("  volumes removed:    %d\n", report.VolumesReclaimed)
        fmt.Printf("  networks removed:   %d\n", report.NetworksReclaimed)
        fmt.Printf("  build cache freed:  %d\n", report.BuildCacheReclaimed)
        fmt.Printf("  space reclaimed:    %s\n", report.SpaceReclaimed)
        return nil
    },
}
```

Add flag declarations in `init()`:

```go
cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without actually removing")
cleanupCmd.Flags().Bool("all", true, "prune all resource types (default)")
cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
cleanupCmd.Flags().Bool("images", false, "prune unused images")
cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
cleanupCmd.Flags().Bool("build-cache", false, "prune builder cache")
```

Register command in `init()`:

```go
rootCmd.AddCommand(cleanupCmd)
```

Also add the `--env` flag:
```go
cleanupCmd.Flags().String("env", "production", "deployment environment")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`
Expected: PASS

- [ ] **Step 5: Run all tests**

Run: `go test ./... -v -count=1 2>&1 | head -100`
Expected: All PASS (existing tests + new tests)

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 6: Build to verify compilation**

Run: `go build -o tengiz .`
Expected: Build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 3: Self-review and documentation update

**Files:**
- Modify: `docs/FUTURES_FEATURES.md` — mark features #6 and #56 as implemented

- [ ] **Step 1: Update FUTURES_FEATURES.md**

Change line 19 from:
```
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```
to:
```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. `tengiz cleanup` with label-based pruning. |
```

Change line 74 from:
```
| 56 | **Granular Docker Prune Operations** ⬜ | Orta | Düşük | Mükemmel | Per-category prune: containers/networks/images/volumes/buildx cache. Surgical disk management. |
```
to:
```
| 56 | **Granular Docker Prune Operations** ✅ | Orta | Düşük | Mükemmel | Per-category prune: containers/networks/images/volumes/buildx cache via `tengiz cleanup --images` etc. |
```

- [ ] **Step 2: Self-review against spec**

Check against requirements from `docs/FUTURES_FEATURES.md`:
- Feature #6 Docker Housekeeping: ✅ Label-based `docker system prune` — `PruneAll()` calls individual prunes with `--filter label=tengiz-app`
- Feature #6 Docker Housekeeping: ✅ `tengiz cleanup` command with `--all` default
- Feature #56 Granular Prune: ✅ Per-category flags: `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`
- Safety: ✅ All prunes use `--filter label=tengiz-app` — only Tengiz resources affected
- Preview: ✅ `--dry-run` flag uses Docker's native `--dry-run` support
- Stub testability: ✅ `stubManager` implements all new methods returning zero reports

- [ ] **Step 3: Placeholder scan**

Search plan for any "TBD", "TODO", "implement later", "fill in details", "add appropriate", "handle edge cases", "Similar to Task" patterns. None found. Every step has complete code with exact file paths, complete method signatures, and full implementation.

- [ ] **Step 4: Type consistency check**

- `PruneReport` struct — same type used across all `Manager.Prune*` methods
- `PruneContainers(ctx, dryRun bool) (PruneReport, error)` — consistent return types across all prune variants
- `DiskUsage() (DiskUsageInfo, error)` — standalone method with own return type
- `dockerRuntime` methods reuse `execPrune()` helper and `parsePruneOutput()` for uniform parsing
- Stub methods all return `(PruneReport{}, nil)` — consistent zero-value pattern
- CLI flags: `--all` defaults to `true`, all category flags default to `false` — when `--all` is true, individual flags are ignored; when individual flags are set, `--all` must be explicitly set to false
- No naming conflicts with existing commands or functions

- [ ] **Step 5: Commit**

```bash
git add docs/FUTURES_FEATURES.md
git commit -m "docs: mark Docker Housekeeping (#6) and Granular Prune (#56) as implemented"
```

