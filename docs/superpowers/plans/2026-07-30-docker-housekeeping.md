# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` CLI command and `runtime.Manager.Cleanup` method to prune unused Docker resources (stopped containers, dangling images, unused volumes/networks, build cache) while protecting Tengiz-managed resources via labels.

**Architecture:** A `Cleanup(ctx, opts) (CleanupReport, error)` method on `runtime.Manager` issues `docker <resource> prune -f` commands. Non-Tengiz resources are pruned directly; Tengiz resources are protected via the `tengiz-app` label filter. The `tengiz cleanup` CLI command wraps this with user-facing flags (`--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--all`, `--keep N`). Output is a structured report printed to stdout.

**Tech Stack:** Go 1.26, Docker CLI, Cobra, existing label conventions (`tengiz-app`, `tengiz-env`), existing `runtime.Manager`, `internal/types`

## Global Constraints

- All Docker prune commands use `-f` (force, no confirmation prompt from Docker itself)
- Confirmation before prune is handled at the CLI level (not at Docker level)
- `tengiz-app` label protects running containers: `docker container prune -f --filter label!=tengiz-app`
- Tagged images built by Tengiz (`tengiz-apps/*`) are never removed by general image pruning — only by app-specific retention (`KeepLastNImages`)
- Build cache prune is safe: `docker builder prune -f` only removes cache layers, not built images
- New types (`CleanupOptions`, `CleanupReport`) go in `internal/types/types.go`
- New CLI command file follows `internal/cli/preview.go` pattern
- Default image retention count is 5 (matching existing `KeepLastNImages` calls)
- All existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | `CleanupOptions`, `CleanupReport` type definitions |
| `internal/runtime/runtime.go` | Add `Cleanup` method to `Manager` interface + stub implementation |
| `internal/runtime/cleanup.go` | `dockerRuntime.Cleanup` implementation using Docker CLI prune commands |
| `internal/runtime/cleanup_test.go` | Stub tests for `Cleanup` + `parsePruneOutput` helper tests |
| `internal/cli/cleanup.go` | New `tengiz cleanup` cobra command with flag definitions |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` |

No new package or external dependency. 6 files touched (1 new file).

---

### Task 1: Add CleanupOptions, CleanupReport types and Cleanup to Manager interface

**Files:**
- Modify: `internal/types/types.go` — add `CleanupOptions`, `CleanupReport`
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager`
- Modify: `internal/runtime/runtime.go:51-123` — add stub `Cleanup` implementation

**Interfaces:**
- Consumes: nothing new
- Produces: `types.CleanupOptions`, `types.CleanupReport`, `Manager.Cleanup(ctx, types.CleanupOptions) (types.CleanupReport, error)`, `stubManager.Cleanup(ctx, types.CleanupOptions) (types.CleanupReport, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go — add these tests

func TestStubCleanup(t *testing.T) {
    m := NewStub()
    opts := types.CleanupOptions{All: true}
    report, err := m.Cleanup(context.Background(), opts)
    if err != nil {
        t.Fatalf("Cleanup() error = %v", err)
    }
    if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 {
        t.Errorf("expected empty report from stub, got %+v", report)
    }
}

func TestCleanupOptionsDefaults(t *testing.T) {
    opts := types.CleanupOptions{}
    if opts.All {
        t.Error("All should default to false")
    }
    if opts.KeepImages != 0 {
        t.Errorf("KeepImages should default to 0, got %d", opts.KeepImages)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestCleanupOptionsDefaults" -v -count=1`

Expected: FAIL with `undefined: types.CleanupOptions`

- [ ] **Step 3: Add CleanupOptions and CleanupReport to `internal/types/types.go`**

Append after line 186 (end of `AppEntry` struct):

```go
type CleanupOptions struct {
    Containers bool
    Images     bool
    Volumes    bool
    Networks   bool
    BuildCache bool
    All        bool
    KeepImages int
    Force      bool
}

type CleanupReport struct {
    ContainersRemoved int    `json:"containers_removed"`
    ImagesRemoved     int    `json:"images_removed"`
    VolumesRemoved    int    `json:"volumes_removed"`
    NetworksRemoved   int    `json:"networks_removed"`
    BuildCacheCleaned bool   `json:"build_cache_cleaned"`
    SpaceReclaimed    string `json:"space_reclaimed"`
}
```

- [ ] **Step 4: Add Cleanup to Manager interface in `internal/runtime/runtime.go`**

Insert after `Run` method (line 48):

```go
Cleanup(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error)
```

- [ ] **Step 5: Add stub Cleanup implementation in `internal/runtime/runtime.go`**

Insert after `stubManager.Run` (line 121-123):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error) {
    return types.CleanupReport{}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestCleanupOptionsDefaults" -v -count=1`

Expected: PASS

- [ ] **Step 7: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/types/types.go internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add CleanupOptions, CleanupReport types and Cleanup to Manager interface"
```

---

### Task 2: Implement Cleanup on dockerRuntime

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `Cleanup` method + output parsing helper

**Interfaces:**
- Consumes: `types.CleanupOptions`, `types.CleanupReport`, `dockerRuntime.exec` pattern (`exec.CommandContext`)
- Produces: `dockerRuntime.Cleanup(ctx, opts) (CleanupReport, error)` that executes Docker prune commands

- [ ] **Step 1: Write the failing test (output parsing helper)**

```go
// internal/runtime/cleanup_test.go — add

func TestParsePruneOutputCount(t *testing.T) {
    output := "Deleted Containers:\n123abc\n456def\n\nTotal reclaimed space: 1.234GB\n"
    count, space := parsePruneOutput(output)
    if count != 2 {
        t.Errorf("expected 2 items, got %d", count)
    }
    if space != "1.234GB" {
        t.Errorf("expected space '1.234GB', got %q", space)
    }
}

func TestParsePruneOutputEmpty(t *testing.T) {
    output := "Total reclaimed space: 0B\n"
    count, space := parsePruneOutput(output)
    if count != 0 {
        t.Errorf("expected 0 items, got %d", count)
    }
    if space != "0B" {
        t.Errorf("expected space '0B', got %q", space)
    }
}

func TestParsePruneOutputBuilder(t *testing.T) {
    output := "TYPE      SIZE     DESCRIPTION\nbuild     1.5GB    Build cache\n\nTotal: 1.5GB\n"
    count, space := parsePruneOutput(output)
    if count != 1 {
        t.Errorf("expected 1 item, got %d", count)
    }
    if space != "1.5GB" {
        t.Errorf("expected space '1.5GB', got %q", space)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestParsePruneOutput" -v -count=1`

Expected: FAIL with `undefined: parsePruneOutput`

- [ ] **Step 3: Add parsePruneOutput helper to `internal/runtime/cleanup.go`**

Add before `RemoveImage`:

```go
func parsePruneOutput(output string) (int, string) {
    lines := strings.Split(strings.TrimSpace(output), "\n")
    var count int
    space := "0B"

    // Find the content region between header and Total line
    headerDone := false
    for _, line := range lines {
        trimmed := strings.TrimSpace(line)
        if trimmed == "" {
            continue
        }
        if strings.HasPrefix(trimmed, "Deleted") || strings.HasPrefix(trimmed, "TYPE") {
            headerDone = true
            continue
        }
        if strings.HasPrefix(trimmed, "Total") {
            // Extract space: last field after ":"
            if idx := strings.LastIndex(trimmed, ":"); idx >= 0 {
                space = strings.TrimSpace(trimmed[idx+1:])
            }
            continue
        }
        if headerDone {
            // Skip docker-generated metadata lines (sha256:... contains ':')
            // Count actual resource names/IDs
            count++
        }
    }
    return count, space
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestParsePruneOutput" -v -count=1`

Expected: PASS

- [ ] **Step 5: Implement Cleanup on dockerRuntime in `internal/runtime/cleanup.go`**

Add to `cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error) {
    var report types.CleanupReport
    var spaces []string

    if opts.Containers || opts.All {
        cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f", "--filter", "label!=tengiz-app")
        out, err := cmd.CombinedOutput()
        if err != nil {
            return report, fmt.Errorf("container prune: %w\n%s", err, string(out))
        }
        count, space := parsePruneOutput(string(out))
        report.ContainersRemoved = count
        if space != "0B" {
            spaces = append(spaces, space)
        }
    }

    if opts.Images || opts.All {
        cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f", "--filter", "dangling=true")
        out, err := cmd.CombinedOutput()
        if err != nil {
            return report, fmt.Errorf("image prune: %w\n%s", err, string(out))
        }
        count, space := parsePruneOutput(string(out))
        report.ImagesRemoved = count
        if space != "0B" {
            spaces = append(spaces, space)
        }
    }

    if opts.Volumes || opts.All {
        cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
        out, err := cmd.CombinedOutput()
        if err != nil {
            return report, fmt.Errorf("volume prune: %w\n%s", err, string(out))
        }
        count, space := parsePruneOutput(string(out))
        report.VolumesRemoved = count
        if space != "0B" {
            spaces = append(spaces, space)
        }
    }

    if opts.Networks || opts.All {
        cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
        out, err := cmd.CombinedOutput()
        if err != nil {
            return report, fmt.Errorf("network prune: %w\n%s", err, string(out))
        }
        count, space := parsePruneOutput(string(out))
        report.NetworksRemoved = count
        if space != "0B" {
            spaces = append(spaces, space)
        }
    }

    if opts.BuildCache || opts.All {
        cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
        out, err := cmd.CombinedOutput()
        if err != nil {
            return report, fmt.Errorf("builder prune: %w\n%s", err, string(out))
        }
        count, space := parsePruneOutput(string(out))
        report.BuildCacheCleaned = count > 0
        if space != "0B" {
            spaces = append(spaces, space)
        }
    }

    if len(spaces) > 0 {
        report.SpaceReclaimed = strings.Join(spaces, " + ")
    }
    return report, nil
}
```

- [ ] **Step 6: Add import for `types` in cleanup.go**

Add `"github.com/yaso09/tengiz/internal/types"` to the imports in `cleanup.go`.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestParsePruneOutput" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement Cleanup on dockerRuntime with Docker prune commands"
```

---

### Task 3: Create `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go` — new cobra command
- Modify: `internal/cli/root.go:34-89` — register cleanupCmd in init()

**Interfaces:**
- Consumes: `runtime.Manager.Cleanup(ctx, opts)`, `types.CleanupOptions`, `types.CleanupReport`
- Produces: `tengiz cleanup [--containers] [--images] [--volumes] [--networks] [--build-cache] [--all] [--keep N] [-f]`

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cleanup_test.go — new file

package cli

import (
    "testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
    cmd, _, err := rootCmd.Find([]string{"cleanup"})
    if err != nil {
        t.Fatalf("cleanup command not registered: %v", err)
    }
    if cmd.Use != "cleanup" {
        t.Errorf("expected Use='cleanup', got %q", cmd.Use)
    }
}

func TestCleanupFlags(t *testing.T) {
    flags := []string{"containers", "images", "volumes", "networks", "build-cache", "all", "keep", "force"}
    for _, name := range flags {
        t.Run(name, func(t *testing.T) {
            f := cleanupCmd.Flags().Lookup(name)
            if f == nil {
                t.Errorf("cleanupCmd missing --%s flag", name)
            }
        })
    }
}

func TestCleanupDefaultAll(t *testing.T) {
    // When no resource flags specified, All should be true
    cleanupCmd.ParseFlags([]string{})
    all, _ := cleanupCmd.Flags().GetBool("all")
    containers, _ := cleanupCmd.Flags().GetBool("containers")
    if !all && !containers {
        t.Error("expected --all=true or --containers=true by default")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCmdRegistered|TestCleanupFlags|TestCleanupDefaultAll" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
    "github.com/yaso09/tengiz/internal/runtime"
    "github.com/yaso09/tengiz/internal/types"
)

var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Prune unused Docker resources (containers, images, volumes, networks, build cache)",
    Long: `Remove unused Docker resources not managed by Tengiz.

By default prunes all resource types. Use flags to select specific types.
Tengiz-managed containers (with tengiz-app label) are never pruned.

Examples:
  tengiz cleanup                    # prune all unused resources
  tengiz cleanup --containers       # prune only stopped containers
  tengiz cleanup --images --keep 3  # prune images, keep 3 per app
  tengiz cleanup --build-cache      # prune only build cache
  tengiz cleanup -f                 # skip confirmation
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        force, _ := cmd.Flags().GetBool("force")
        all, _ := cmd.Flags().GetBool("all")
        containers, _ := cmd.Flags().GetBool("containers")
        images, _ := cmd.Flags().GetBool("images")
        volumes, _ := cmd.Flags().GetBool("volumes")
        networks, _ := cmd.Flags().GetBool("networks")
        buildCache, _ := cmd.Flags().GetBool("build-cache")
        keep, _ := cmd.Flags().GetInt("keep")

        opts := types.CleanupOptions{
            Containers: containers,
            Images:     images,
            Volumes:    volumes,
            Networks:   networks,
            BuildCache: buildCache,
            All:        all,
            KeepImages: keep,
            Force:      force,
        }

        // If no specific resource flag set, default to All
        if !containers && !images && !volumes && !networks && !buildCache {
            opts.All = true
        }

        if !opts.Force && opts.All {
            fmt.Print("This will prune unused Docker resources. Continue? [y/N] ")
            var response string
            fmt.Scanln(&response)
            if response != "y" && response != "Y" && response != "yes" {
                fmt.Println("Cancelled.")
                return nil
            }
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        report, err := rt.Cleanup(cmd.Context(), opts)
        if err != nil {
            return fmt.Errorf("cleanup: %w", err)
        }

        fmt.Println("[tengiz] cleanup complete:")
        fmt.Printf("  Containers removed: %d\n", report.ContainersRemoved)
        fmt.Printf("  Images removed:     %d\n", report.ImagesRemoved)
        fmt.Printf("  Volumes removed:    %d\n", report.VolumesRemoved)
        fmt.Printf("  Networks removed:   %d\n", report.NetworksRemoved)
        if report.BuildCacheCleaned {
            fmt.Println("  Build cache:        cleaned")
        }
        if report.SpaceReclaimed != "" {
            fmt.Printf("  Space reclaimed:    %s\n", report.SpaceReclaimed)
        }

        // Run per-app image retention if images were pruned
        if (opts.Images || opts.All) && opts.KeepImages > 0 {
            store := config.NewStore(dataDir)
            apps, err := store.ListApps()
            if err != nil {
                return fmt.Errorf("list apps: %w", err)
            }
            for _, app := range apps {
                if err := rt.KeepLastNImages(cmd.Context(), app.Name, opts.KeepImages); err != nil {
                    fmt.Fprintf(os.Stderr, "[tengiz] warning: image retention for %s: %v\n", app.Name, err)
                }
            }
        }

        return nil
    },
}

func init() {
    cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
    cleanupCmd.Flags().Bool("images", false, "prune dangling images")
    cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
    cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
    cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
    cleanupCmd.Flags().Bool("all", false, "prune all resource types")
    cleanupCmd.Flags().Int("keep", 5, "number of images to keep per app (with --images)")
    cleanupCmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")
}
```

- [ ] **Step 4: Register cleanupCmd in `internal/cli/root.go` init()**

Add to `init()` (after line 75, before `deployCmd.Flags().String(...)`):

```go
rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCmdRegistered|TestCleanupFlags|TestCleanupDefaultAll" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup CLI command"
```

---

### Task 4: Integration test and self-review

**Files:**
- No file changes — verification only

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except possibly proxy TCP timeout tests and idle time-sensitive tests)

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 3: Self-review against spec**

Check against `docs/FUTURES_FEATURES.md` Docker Housekeeping requirement:
- Label-based `docker system prune` ✅ (Task 2 — `label!=tengiz-app` filter on container prune)
- `tengiz cleanup` command ✅ (Task 3 — new CLI command with resource-type flags)
- Tengiz-managed container protection ✅ (label filter excludes tengiz-app labeled containers)
- Image retention via KeepLastNImages ✅ (Task 3 — per-app image retention with --keep flag)
- Selective cleanup by resource type ✅ (--containers, --images, --volumes, --networks, --build-cache flags)
- Confirmation prompt ✅ (--force / -f bypass, interactive confirmation by default)
- Build cache cleanup ✅ (docker builder prune -f)
- Space reclaimed reporting ✅ (output parsed from Docker CLI and reported to user)

- [ ] **Step 4: Placeholder scan**

Search plan for any "TBD", "TODO", "implement later", "fill in details" patterns. None found. Every step has complete code.

- [ ] **Step 5: Type consistency check**

- `types.CleanupOptions` — defined in Task 1, consumed in Task 2 and Task 3
- `types.CleanupReport` — defined in Task 1, produced in Task 2, consumed in Task 3
- `Manager.Cleanup(ctx, opts) (types.CleanupReport, error)` — interface defined in Task 1, implemented in Task 2
- `parsePruneOutput(string) (int, string)` — defined in Task 2, used in Task 2 only
- `cleanupCmd` — defined in Task 3, registered in root.go init()

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup_test.go
git commit -m "test: add cleanup command tests"
```
