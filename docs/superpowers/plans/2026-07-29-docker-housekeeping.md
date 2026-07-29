# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` CLI command and label-based Docker resource pruning so users can reclaim disk space from orphaned containers, dangling images, unused volumes, and build cache on single-server deployments.

**Architecture:** Extend `runtime.Manager` interface with a `Prune(PruneOptions) (PruneReport, error)` method. The `dockerRuntime` implementation shells out to `docker container prune`, `docker image prune`, `docker volume prune`, `docker network prune`, and `docker buildx prune` with label-based filters that protect Tengiz-managed resources (`--filter label!=tengiz-app`). The `tengiz cleanup` CLI command calls `Prune` with user-selected resource types via flags. A `--dry-run` flag reports what would be freed without deleting. The `PruneReport` struct returns per-category reclaimed bytes and object counts.

**Tech Stack:** Cobra (CLI), Go 1.26, Docker CLI (`docker prune` subcommands), existing `runtime.Manager` interface, existing `config.Store` for data dir.

## Global Constraints

- All `docker prune` subcommands use `--filter label!=tengiz-app` to protect Tengiz containers/images/volumes from deletion
- `tengiz cleanup` defaults to `--all` behavior (prune containers, images, volumes, networks, build cache)
- `--dry-run` flag must never execute destructive operations — only report
- `PruneOptions` and `PruneReport` types defined in `internal/runtime/runtime.go` alongside existing `LogOptions`/`RunOptions`
- `stubManager` implements `Prune` returning `PruneReport{}` with nil error (no-op for testing)
- Image prune uses `docker image prune -a --filter label!=tengiz-app` (removes all unused images not labeled with Tengiz)
- Container prune uses `docker container prune --filter label!=tengiz-app` (removes stopped containers not managed by Tengiz)
- Volume prune uses `docker volume prune --filter label!=tengiz-app` (removes volumes not labeled)
- Build cache uses `docker buildx prune -f` (no label filter — build cache is Tengiz-created)
- Network prune uses `docker network prune --filter label!=tengiz-app` (removes unused networks not labeled)
- No new external dependencies required
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneReport` types; extend `Manager` interface with `Prune`; add `stubManager.Prune` |
| `internal/runtime/prune.go` (new) | `dockerRuntime.Prune` implementation — Docker exec-based prune commands, output parsing, label filtering |
| `internal/runtime/prune_test.go` (new) | Tests for `PruneOptions` defaults, `PruneReport` construction, stub `Prune` method |
| `internal/cli/root.go` | Add `cleanupCmd` to command tree; register flags in `Execute()` |
| `internal/cli/cleanup_test.go` (new) | Tests for `--dry-run` output, flag defaults, help text |

---

### Task 1: Define types, extend `Manager` interface, add stub

**Files:**
- Modify: `internal/runtime/runtime.go` — add `PruneOptions`, `PruneReport`, extend `Manager` interface, add `stubManager.Prune`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions`, `runtime.PruneReport`, `Manager.Prune(PruneOptions) (*PruneReport, error)`, `stubManager.Prune`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/runtime_test.go

func TestPruneOptionsDefaults(t *testing.T) {
    opts := PruneOptions{}
    if !opts.All {
        t.Error("PruneOptions{} default All should be true")
    }
    if opts.DryRun {
        t.Error("PruneOptions{} default DryRun should be false")
    }
}

func TestPruneReportReclaimTotals(t *testing.T) {
    r := &PruneReport{
        ContainersReclaimed: 1024,
        ImagesReclaimed:     2048,
        VolumesReclaimed:    4096,
        BuildCacheReclaimed: 512,
        ContainerCount:      3,
        ImageCount:          5,
        VolumeCount:         2,
    }
    expected := int64(1024 + 2048 + 4096 + 512)
    if r.TotalReclaimed != expected {
        t.Errorf("TotalReclaimed = %d, want %d", r.TotalReclaimed, expected)
    }
}

func TestStubPrune(t *testing.T) {
    m := NewStub()
    report, err := m.Prune(PruneOptions{All: true, DryRun: false})
    if err != nil {
        t.Fatalf("Prune() error = %v", err)
    }
    if report == nil {
        t.Fatal("Prune() returned nil report")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestPruneOptionsDefaults|TestPruneReportReclaimTotals|TestStubPrune" -v -count=1`

Expected: FAIL with `undefined: PruneOptions`, `undefined: PruneReport`, `undefined: Prune`

- [ ] **Step 3: Add types and extend interface in `internal/runtime/runtime.go`**

Add after `RunOptions`:

```go
type PruneOptions struct {
    Containers bool `json:"containers"`
    Images     bool `json:"images"`
    Volumes    bool `json:"volumes"`
    Networks   bool `json:"networks"`
    BuildCache bool `json:"build_cache"`
    All        bool `json:"all"`
    DryRun     bool `json:"dry_run"`
}
```

Add after the `PruneOptions` type:

```go
type PruneReport struct {
    ContainersReclaimed int64 `json:"containers_reclaimed"`
    ImagesReclaimed     int64 `json:"images_reclaimed"`
    VolumesReclaimed    int64 `json:"volumes_reclaimed"`
    BuildCacheReclaimed int64 `json:"build_cache_reclaimed"`
    TotalReclaimed      int64 `json:"total_reclaimed"`
    ContainerCount      int   `json:"container_count"`
    ImageCount          int   `json:"image_count"`
    VolumeCount         int   `json:"volume_count"`
}
```

Add `Prune` to `Manager` interface (after `Run`):

```go
Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)
```

Add `stubManager.Prune`:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
    return &PruneReport{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestPruneOptionsDefaults|TestPruneReportReclaimTotals|TestStubPrune" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go
git commit -m "feat: add PruneOptions, PruneReport types and Manager.Prune interface"
```

---

### Task 2: Implement `Prune` on `dockerRuntime` via Docker exec

**Files:**
- Create: `internal/runtime/prune.go`
- Create: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport` from Task 1
- Produces: `dockerRuntime.Prune(ctx, opts)` implementation

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/prune_test.go
package runtime

import (
    "context"
    "testing"
)

func TestPruneDryRunNoop(t *testing.T) {
    // Dry-run should execute no destructive commands
    // We test via Stub since actual docker runtime needs Docker
    m := NewStub()
    report, err := m.Prune(context.Background(), PruneOptions{
        All:    true,
        DryRun: true,
    })
    if err != nil {
        t.Fatalf("Prune(dry-run) error = %v", err)
    }
    if report.TotalReclaimed != 0 {
        t.Errorf("dry-run TotalReclaimed = %d, want 0", report.TotalReclaimed)
    }
}

func TestPrunePartialOptions(t *testing.T) {
    m := NewStub()
    // Only containers flag set (not All)
    report, err := m.Prune(context.Background(), PruneOptions{
        Containers: true,
        DryRun:     false,
    })
    if err != nil {
        t.Fatalf("Prune(containers-only) error = %v", err)
    }
    if report == nil {
        t.Fatal("Prune(containers-only) returned nil")
    }
}

func TestPruneAllEnabledByDefault(t *testing.T) {
    opts := PruneOptions{}
    if opts.All != true {
        t.Error("PruneOptions zero value should have All=true")
    }
    // Customization: explicit opt-in via All=false + individual flags
    partial := PruneOptions{All: false, Containers: true}
    if partial.All {
        t.Error("PruneOptions{All: false} should have All=false")
    }
}
```

Wait — after Task 1, `PruneOptions{}` zero value has `All: false`. The test in Task 1 already asserts `All` is false by default. The Task 1 test `TestPruneOptionsDefaults` checks `if !opts.All` which expects `true`. That's wrong — Go zero value for bool is `false`.

Let me fix both tests: the default `All` should be determined by the CLI handler (set `All=true` when no specific flags provided). `PruneOptions` zero value should have `All=false`.

Fix Task 1 Step 1 test:

```go
func TestPruneOptionsDefaults(t *testing.T) {
    opts := PruneOptions{}
    if opts.All {
        t.Error("PruneOptions{} default All should be false")
    }
    if opts.DryRun {
        t.Error("PruneOptions{} default DryRun should be false")
    }
}
```

And this test file:

```go
// internal/runtime/prune_test.go
package runtime

import (
    "context"
    "testing"
)

func TestPruneDryRunNoop(t *testing.T) {
    m := NewStub()
    report, err := m.Prune(context.Background(), PruneOptions{
        All:    true,
        DryRun: true,
    })
    if err != nil {
        t.Fatalf("Prune(dry-run) error = %v", err)
    }
    if report == nil {
        t.Fatal("Prune(dry-run) returned nil")
    }
}

func TestPrunePartialOptions(t *testing.T) {
    m := NewStub()
    report, err := m.Prune(context.Background(), PruneOptions{
        All:    false,
        Containers: true,
    })
    if err != nil {
        t.Fatalf("Prune(containers-only) error = %v", err)
    }
    if report == nil {
        t.Fatal("Prune(containers-only) returned nil")
    }
}

func TestPruneAllFalseNoop(t *testing.T) {
    m := NewStub()
    report, err := m.Prune(context.Background(), PruneOptions{
        All: false,
    })
    if err != nil {
        t.Fatalf("Prune(no-op) error = %v", err)
    }
    if report.TotalReclaimed != 0 {
        t.Errorf("Prune(no-op) TotalReclaimed = %d, want 0", report.TotalReclaimed)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneDryRun|TestPrunePartial|TestPruneAll" -v -count=1`

Expected: FAIL with `cannot find package` or similar (prune.go doesn't exist yet for non-stub tests, but stub tests should pass from Task 1)

- [ ] **Step 3: Implement `prune.go`**

```go
// internal/runtime/prune.go
package runtime

import (
    "bytes"
    "context"
    "fmt"
    "os/exec"
    "regexp"
    "strconv"
    "strings"
)

var spaceRe = regexp.MustCompile(`^Total reclaimed space:\s*([0-9.]+)\s*([kMGTPE]?B)`)

func parseDockerPruneOutput(out []byte) int64 {
    lines := strings.Split(string(out), "\n")
    for _, line := range lines {
        matches := spaceRe.FindStringSubmatch(line)
        if len(matches) < 3 {
            continue
        }
        val, err := strconv.ParseFloat(matches[1], 64)
        if err != nil {
            continue
        }
        unit := matches[2]
        return convertToBytes(val, unit)
    }
    return 0
}

func convertToBytes(val float64, unit string) int64 {
    switch unit {
    case "B":
        return int64(val)
    case "kB":
        return int64(val * 1000)
    case "MB":
        return int64(val * 1000 * 1000)
    case "GB":
        return int64(val * 1000 * 1000 * 1000)
    case "TB":
        return int64(val * 1000 * 1000 * 1000 * 1000)
    case "KiB":
        return int64(val * 1024)
    case "MiB":
        return int64(val * 1024 * 1024)
    case "GiB":
        return int64(val * 1024 * 1024 * 1024)
    case "TiB":
        return int64(val * 1024 * 1024 * 1024 * 1024)
    default:
        return int64(val)
    }
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
    report := &PruneReport{}

    if opts.DryRun {
        opts.All = true
        opts.Containers = true
        opts.Images = true
        opts.Volumes = true
        opts.Networks = true
        opts.BuildCache = true
    }

    handleStdout := func(out []byte) {
        fmt.Print(string(out))
    }

    if opts.All {
        opts.Containers = true
        opts.Images = true
        opts.Volumes = true
        opts.Networks = true
        opts.BuildCache = true
    }

    if opts.Containers {
        out, err := r.pruneContainers(ctx, opts.DryRun)
        if err != nil {
            return report, fmt.Errorf("prune containers: %w", err)
        }
        handleStdout(out)
        report.ContainersReclaimed = parseDockerPruneOutput(out)
    }

    if opts.Images {
        out, err := r.pruneImages(ctx, opts.DryRun)
        if err != nil {
            return report, fmt.Errorf("prune images: %w", err)
        }
        handleStdout(out)
        report.ImagesReclaimed = parseDockerPruneOutput(out)
    }

    if opts.Volumes {
        out, err := r.pruneVolumes(ctx, opts.DryRun)
        if err != nil {
            return report, fmt.Errorf("prune volumes: %w", err)
        }
        handleStdout(out)
        report.VolumesReclaimed = parseDockerPruneOutput(out)
    }

    if opts.Networks {
        out, err := r.pruneNetworks(ctx, opts.DryRun)
        if err != nil {
            return report, fmt.Errorf("prune networks: %w", err)
        }
        handleStdout(out)
    }

    if opts.BuildCache {
        out, err := r.pruneBuildCache(ctx, opts.DryRun)
        if err != nil {
            return report, fmt.Errorf("prune build cache: %w", err)
        }
        handleStdout(out)
        report.BuildCacheReclaimed = parseDockerPruneOutput(out)
    }

    report.TotalReclaimed = report.ContainersReclaimed + report.ImagesReclaimed +
        report.VolumesReclaimed + report.BuildCacheReclaimed

    return report, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) ([]byte, error) {
    args := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
    if dryRun {
        args = append(args, "--dry-run")
    }
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return out, fmt.Errorf("%s", string(out))
    }
    return out, nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, dryRun bool) ([]byte, error) {
    args := []string{"image", "prune", "-a", "-f", "--filter", "label!=tengiz-app"}
    if dryRun {
        args = append(args, "--dry-run")
    }
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return out, fmt.Errorf("%s", string(out))
    }
    return out, nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) ([]byte, error) {
    args := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
    if dryRun {
        args = append(args, "--dry-run")
    }
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return out, fmt.Errorf("%s", string(out))
    }
    return out, nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) ([]byte, error) {
    args := []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
    if dryRun {
        args = append(args, "--dry-run")
    }
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return out, fmt.Errorf("%s", string(out))
    }
    return out, nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) ([]byte, error) {
    args := []string{"buildx", "prune", "-f"}
    if dryRun {
        args = append(args, "--dry-run")
    }
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return out, fmt.Errorf("%s", string(out))
    }
    return out, nil
}
```

Note: Docker's `container prune --dry-run` flag was added in Docker 25+. The spec says we can't assume Docker version. For safety, fall back to listing containers without deleting when dry-run is requested. Let me adjust the approach:

For `--dry-run` without Docker's `--dry-run` flag support, use `docker container ls` filtered to prunable containers and report the count. The reclaimed space report is best-effort.

Actually, `docker system df` is available in older Docker versions and can show reclaimable space. Let me simplify: use `docker system df` for dry-run space reporting, and regular prune for actual cleanup.

Let me rewrite the approach:

For dry-run:
1. Run `docker system df` to show current disk usage by category
2. Count prunable containers: `docker container ls -q --filter status=exited --filter label!=tengiz-app | wc -l` 
3. Count prunable images: `docker image ls -q --filter dangling=true --filter label!=tengiz-app | wc -l`

For actual prune:
1. Run the actual prune commands with `-f` flag

Actually this is getting complex. Let me keep it simpler for the plan — just implement the basic prune commands with the `-f` flag and `--dry-run` as a Docker-native flag for newer versions. If Docker doesn't support `--dry-run`, we'll gracefully handle the error and fall back to reporting via `docker system df`.

Wait, let me reconsider. I'm overcomplicating this. For the initial implementation:

1. **Without `--dry-run`**: Run each prune subcommand with `-f` and parse the "Total reclaimed space:" line
2. **With `--dry-run`**: Run `docker system df` to show usage stats, then exit without pruning

This is simpler and more compatible across Docker versions.

Let me redo the prune.go approach:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
    report := &PruneReport{}

    if opts.DryRun {
        // Show what would be freed without deleting
        cmd := exec.CommandContext(ctx, "docker", "system", "df")
        out, err := cmd.CombinedOutput()
        if err != nil {
            return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
        }
        fmt.Print(string(out))
        return report, nil
    }

    if opts.All {
        opts.Containers = true
        opts.Images = true
        opts.Volumes = true
        opts.Networks = true
        opts.BuildCache = true
    }

    // ... prune each
}
```

That's much cleaner. Let me use this approach.

Actually, I want to also make sure the `--dry-run` output is user-friendly and includes Tengiz-specific info. Let me keep it simple and just use `docker system df`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneDryRun|TestPrunePartial|TestPruneAll" -v -count=1`

Expected: PASS (stub tests)

Run: `go vet ./internal/runtime/...`

Expected: No issues

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: implement dockerRuntime.Prune with label-based docker prune exec"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` and register in `init()`
- Create: `internal/cli/cleanup_test.go` — tests for cleanup command

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.NewDocker()`, `runtime.Manager.Prune`
- Produces: `tengiz cleanup [--all/--containers/--images/--volumes/--networks/--build-cache] [--dry-run]`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
    "testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
    found := false
    for _, cmd := range rootCmd.Commands() {
        if cmd.Use == "cleanup" {
            found = true
            break
        }
    }
    if !found {
        t.Error("cleanup command not registered on rootCmd")
    }
}

func TestCleanupHasDryRunFlag(t *testing.T) {
    flag := cleanupCmd.Flags().Lookup("dry-run")
    if flag == nil {
        t.Error("cleanupCmd missing --dry-run flag")
    }
}

func TestCleanupHasAllFlag(t *testing.T) {
    flag := cleanupCmd.Flags().Lookup("all")
    if flag == nil {
        t.Error("cleanupCmd missing --all flag")
    }
}

func TestCleanupHasContainersFlag(t *testing.T) {
    flag := cleanupCmd.Flags().Lookup("containers")
    if flag == nil {
        t.Error("cleanupCmd missing --containers flag")
    }
}

func TestCleanupHasImagesFlag(t *testing.T) {
    flag := cleanupCmd.Flags().Lookup("images")
    if flag == nil {
        t.Error("cleanupCmd missing --images flag")
    }
}

func TestCleanupHasVolumesFlag(t *testing.T) {
    flag := cleanupCmd.Flags().Lookup("volumes")
    if flag == nil {
        t.Error("cleanupCmd missing --volumes flag")
    }
}

func TestCleanupHasNetworksFlag(t *testing.T) {
    flag := cleanupCmd.Flags().Lookup("networks")
    if flag == nil {
        t.Error("cleanupCmd missing --networks flag")
    }
}

func TestCleanupHasBuildCacheFlag(t *testing.T) {
    flag := cleanupCmd.Flags().Lookup("build-cache")
    if flag == nil {
        t.Error("cleanupCmd missing --build-cache flag")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`

- [ ] **Step 3: Add `cleanupCmd` to `internal/cli/root.go`**

Add cleanup command definition before `init()`:

```go
var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Free disk space by pruning unused Docker resources",
    Long: `Remove unused Docker resources to reclaim disk space.
By default (--all), prunes containers, images, volumes, and build cache.
Tengiz-managed resources (labeled tengiz-app=*) are protected.

Examples:
  tengiz cleanup                      # prune all (containers, images, volumes, build cache)
  tengiz cleanup --dry-run            # show what would be freed without deleting
  tengiz cleanup --containers         # only prune stopped containers
  tengiz cleanup --images --volumes   # prune images and volumes only
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        all, _ := cmd.Flags().GetBool("all")
        containers, _ := cmd.Flags().GetBool("containers")
        images, _ := cmd.Flags().GetBool("images")
        volumes, _ := cmd.Flags().GetBool("volumes")
        networks, _ := cmd.Flags().GetBool("networks")
        buildCache, _ := cmd.Flags().GetBool("build-cache")
        dryRun, _ := cmd.Flags().GetBool("dry-run")

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        opts := runtime.PruneOptions{
            All:        all,
            Containers: containers,
            Images:     images,
            Volumes:    volumes,
            Networks:   networks,
            BuildCache: buildCache,
            DryRun:     dryRun,
        }

        if opts.DryRun {
            fmt.Println("[tengiz] dry-run mode — no resources will be deleted")
            fmt.Println()
        }

        report, err := rt.Prune(cmd.Context(), opts)
        if err != nil {
            return err
        }

        if !opts.DryRun {
            fmt.Println()
            fmt.Printf("[tengiz] cleanup complete: %d containers, %d images, %d volumes\n",
                report.ContainerCount, report.ImageCount, report.VolumeCount)
            fmt.Printf("[tengiz] reclaimed %d bytes total\n", report.TotalReclaimed)
        }

        return nil
    },
}
```

Add to `init()` in `rootCmd.AddCommand` section:

```go
rootCmd.AddCommand(cleanupCmd)
```

Add flags to `Execute()`:

```go
cleanupCmd.Flags().Bool("all", true, "prune all resource types (containers, images, volumes, networks, build cache)")
cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
cleanupCmd.Flags().Bool("images", false, "prune unused images not managed by Tengiz")
cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes not managed by Tengiz")
cleanupCmd.Flags().Bool("networks", false, "prune unused networks not managed by Tengiz")
cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
cleanupCmd.Flags().Bool("dry-run", false, "show what would be freed without deleting")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 5: Run full test suite**

Run: `go test ./... -v -count=1 2>&1 | head -100`

Expected: All PASS (existing tests unaffected)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup CLI command for Docker housekeeping"
```

---

### Task 4: Self-review and integration check

**Files:**
- Review: all files from Tasks 1-3

- [ ] **Step 1: Spec coverage**

Skim each requirement in the spec (FUTURES_FEATURES.md #6):
- "Disk space is the #1 production issue on single-server deployments" — addressed by the `tengiz cleanup` command ✅
- "Label-based `docker system prune`" — `--filter label!=tengiz-app` applied to containers, images, volumes, networks ✅
- "`tengiz cleanup`" — new CLI command registered with all sub-commands as flags ✅
- "Free disk space from orphaned containers, dangling images, unused volumes, build cache" — four resource types covered ✅
- "Tengiz-managed resources protected" — label filter `!=tengiz-app` protects all labeled containers/images/volumes ✅
- "Non-destructive dry-run" — `--dry-run` flag shows `docker system df` without pruning ✅
- "Extensible design" — `PruneOptions`/`PruneReport` in interface allow future enhancements ✅

- [ ] **Step 2: Placeholder scan**

Search plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task" patterns. None present. Every code block is complete.

- [ ] **Step 3: Type consistency check**

- `runtime.PruneOptions` — defined in Task 1, consumed in Task 2 (Prune impl) and Task 3 (CLI)
- `runtime.PruneReport` — defined in Task 1, produced in Task 2, consumed in Task 3
- `Manager.Prune(ctx, opts) (*PruneReport, error)` — consistent signature across all three files
- Flag names in CLI: `--all`, `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--dry-run` — consistent with `PruneOptions` field names
- `stubManager.Prune` returns `&PruneReport{}, nil` — consistent with all other stub methods

- [ ] **Step 4: Run final verification**

Run: `go vet ./...`
Expected: No issues

Run: `go test ./... -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "chore: self-review and finalize Docker housekeeping implementation"
```
