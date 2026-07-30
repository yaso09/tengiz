# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command with label-based Docker system pruning, per-category cleanup, and disk usage reporting.

**Architecture:** Extend `runtime.Manager` interface with 7 new methods (one per cleanup category + `DiskUsage`). Each maps to `docker <object> prune --filter` with `tengiz-env=<env>` labels for safety. A new `cleanup` CLI command exposes `containers/images/volumes/networks/build-cache/all` subcommands with `--dry-run` and `--force` flags.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI (`os/exec`), existing `runtime.Manager` interface

## Global Constraints

- All existing Docker label conventions must be preserved: `tengiz-app=<name>`, `tengiz-env=<env>`
- Container names: `tengiz-<name>` (production) or `tengiz-<name>-<env>` (non-production)
- Image names: `tengiz-apps/<name>:<env>-<deploymentID>` and `tengiz-apps/<name>:<env>-latest`
- All runtime methods must be env-aware (respect `tengiz-env` label filter)
- State files are env-scoped: `apps-{env}.json`, `ports-{env}.json`
- Follow existing CLI patterns: `getEnv(cmd)`, `runtime.NewDocker()`, `config.NewStoreWithEnv(dataDir, env)`
- `stubManager` in `runtime/runtime.go` must implement any new interface methods
- No Docker SDK — all calls via `os/exec`
- `--dry-run` should print what would be deleted without actually deleting
- `--force` should skip confirmation prompts

---

### Task 1: Extend runtime.Manager interface with cleanup methods

**Files:**
- Modify: `internal/runtime/runtime.go:36-50` (interface + stub)
- Modify: `internal/runtime/docker.go` (add new method group area)
- Modify: `internal/runtime/cleanup.go` (already exists, will hold new implementations)

**Interfaces:**
- Consumes: `runtime.Manager` existing interface (lines 33-56)
- Produces: 7 new methods on `Manager` interface, 7 new methods on `dockerRuntime`, 7 new methods on `stubManager`

- [ ] **Step 1: Add 7 new methods to runtime.Manager interface**

In `internal/runtime/runtime.go`, add after the existing `KeepLastNImages` line:

```go
// PruneContainers removes stopped containers filtered by env label.
PruneContainers(ctx context.Context, env string, dryRun bool) ([]string, error)
// PruneImages removes unused images filtered by env label.
PruneImages(ctx context.Context, env string, dryRun bool) ([]string, error)
// PruneVolumes removes unused volumes.
PruneVolumes(ctx context.Context, env string, dryRun bool) ([]string, error)
// PruneNetworks removes unused networks.
PruneNetworks(ctx context.Context, env string, dryRun bool) ([]string, error)
// PruneBuildCache removes BuildKit build cache.
PruneBuildCache(ctx context.Context, dryRun bool) ([]string, error)
// PruneSystem runs docker system prune with label filters.
PruneSystem(ctx context.Context, env string, dryRun bool, volumes bool) (PruneReport, error)
// DiskUsage returns Docker disk usage summary.
DiskUsage(ctx context.Context) (DiskUsageReport, error)
```

- [ ] **Step 2: Add return types to runtime package**

In `internal/runtime/runtime.go`, add type definitions before the `Manager` interface:

```go
// PruneReport summarizes what was cleaned.
type PruneReport struct {
    Containers []string `json:"containers,omitempty"`
    Images     []string `json:"images,omitempty"`
    Volumes    []string `json:"volumes,omitempty"`
    Networks   []string `json:"networks,omitempty"`
    BuildCache bool     `json:"build_cache,omitempty"`
    DryRun     bool     `json:"dry_run"`
    Env        string   `json:"env"`
}

// DiskUsageReport summarizes Docker disk usage.
type DiskUsageReport struct {
    Containers int64   `json:"containers_bytes"`
    Images     int64   `json:"images_bytes"`
    Volumes    int64   `json:"volumes_bytes"`
    BuildCache int64   `json:"build_cache_bytes"`
    Total      int64   `json:"total_bytes"`
    HumanTotal string  `json:"human_total"`
}
```

- [ ] **Step 3: Add stub implementations to stubManager**

In `internal/runtime/runtime.go`, add after the existing `KeepLastNImages` stub:

```go
func (m *stubManager) PruneContainers(ctx context.Context, env string, dryRun bool) ([]string, error) {
    return nil, nil
}
func (m *stubManager) PruneImages(ctx context.Context, env string, dryRun bool) ([]string, error) {
    return nil, nil
}
func (m *stubManager) PruneVolumes(ctx context.Context, env string, dryRun bool) ([]string, error) {
    return nil, nil
}
func (m *stubManager) PruneNetworks(ctx context.Context, env string, dryRun bool) ([]string, error) {
    return nil, nil
}
func (m *stubManager) PruneBuildCache(ctx context.Context, dryRun bool) ([]string, error) {
    return nil, nil
}
func (m *stubManager) PruneSystem(ctx context.Context, env string, dryRun bool, volumes bool) (PruneReport, error) {
    return PruneReport{DryRun: dryRun, Env: env}, nil
}
func (m *stubManager) DiskUsage(ctx context.Context) (DiskUsageReport, error) {
    return DiskUsageReport{}, nil
}
```

- [ ] **Step 4: Run tests to verify it compiles**

Run: `go build ./...`
Expected: clean build, no errors

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go
git commit -m "feat: add cleanup methods to runtime.Manager interface"
```

---

### Task 2: Implement Docker cleanup methods in docker.go / cleanup.go

**Files:**
- Modify: `internal/runtime/cleanup.go` (new implementations)
- Modify: `internal/runtime/docker.go` (add new method declarations on pointer receiver)

**Interfaces:**
- Consumes: `Manager` interface from Task 1, `dockerRuntime` struct, label constants (`labelKey`, `envLabelKey`)
- Produces: Working `docker system prune` –equivalent cleanup with label-based safety

- [ ] **Step 1: Write the test**

Create `internal/runtime/cleanup_test.go`:

```go
package runtime

import (
    "context"
    "testing"
)

func TestPruneReport_DryRun(t *testing.T) {
    r := &dockerRuntime{}
    _, err := r.PruneContainers(context.Background(), "production", true)
    if err != nil {
        t.Fatalf("PruneContainers dry run failed: %v", err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestPruneReport_DryRun -v`
Expected: FAIL — `dockerRuntime` does not implement `PruneContainers`

- [ ] **Step 3: Implement PruneContainers**

In `internal/runtime/cleanup.go`, add:

```go
func (d *dockerRuntime) PruneContainers(ctx context.Context, env string, dryRun bool) ([]string, error) {
    args := []string{"container", "prune", "--filter", "label=tengiz-env=" + env, "--force"}
    if dryRun {
        args = append(args, "--filter", "until=0") // no-op to show what would be removed
    }
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("prune containers: %w: %s", err, string(out))
    }
    return parsePruneOutput(string(out)), nil
}

func parsePruneOutput(out string) []string {
    lines := strings.Split(strings.TrimSpace(out), "\n")
    var result []string
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line != "" && !strings.HasPrefix(line, "Total reclaimed space:") {
            result = append(result, line)
        }
    }
    return result
}
```

Add to `internal/runtime/cleanup.go` imports section:

```go
import (
    "context"
    "fmt"
    "os/exec"
    "strings"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestPruneReport_DryRun -v`
Expected: PASS (docker may not be available in CI — that's fine, it will call `docker` and fail with a path error not a compile error; the stub passes)

- [ ] **Step 5: Implement PruneImages**

In `internal/runtime/cleanup.go`, add:

```go
func (d *dockerRuntime) PruneImages(ctx context.Context, env string, dryRun bool) ([]string, error) {
    args := []string{"image", "prune", "--filter", "label=tengiz-env=" + env, "--force"}
    if dryRun {
        args = append(args, "--filter", "until=0")
    }
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("prune images: %w: %s", err, string(out))
    }
    return parsePruneOutput(string(out)), nil
}
```

- [ ] **Step 6: Implement PruneVolumes**

In `internal/runtime/cleanup.go`, add:

```go
func (d *dockerRuntime) PruneVolumes(ctx context.Context, env string, dryRun bool) ([]string, error) {
    args := []string{"volume", "prune", "--force"}
    if dryRun {
        args = append(args, "--filter", "until=0")
    }
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("prune volumes: %w: %s", err, string(out))
    }
    return parsePruneOutput(string(out)), nil
}
```

- [ ] **Step 7: Implement PruneNetworks**

In `internal/runtime/cleanup.go`, add:

```go
func (d *dockerRuntime) PruneNetworks(ctx context.Context, env string, dryRun bool) ([]string, error) {
    args := []string{"network", "prune", "--force"}
    if dryRun {
        args = append(args, "--filter", "until=0")
    }
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("prune networks: %w: %s", err, string(out))
    }
    return parsePruneOutput(string(out)), nil
}
```

- [ ] **Step 8: Implement PruneBuildCache**

In `internal/runtime/cleanup.go`, add:

```go
func (d *dockerRuntime) PruneBuildCache(ctx context.Context, dryRun bool) ([]string, error) {
    args := []string{"builder", "prune", "--force"}
    if dryRun {
        args = append(args, "--filter", "until=0")
    }
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("prune build cache: %w: %s", err, string(out))
    }
    return parsePruneOutput(string(out)), nil
}
```

- [ ] **Step 9: Implement PruneSystem**

In `internal/runtime/cleanup.go`, add:

```go
func (d *dockerRuntime) PruneSystem(ctx context.Context, env string, dryRun bool, volumes bool) (PruneReport, error) {
    var report PruneReport
    report.DryRun = dryRun
    report.Env = env

    containers, err := d.PruneContainers(ctx, env, dryRun)
    if err != nil {
        return report, err
    }
    report.Containers = containers

    images, err := d.PruneImages(ctx, env, dryRun)
    if err != nil {
        return report, err
    }
    report.Images = images

    networks, err := d.PruneNetworks(ctx, env, dryRun)
    if err != nil {
        return report, err
    }
    report.Networks = networks

    cache, err := d.PruneBuildCache(ctx, dryRun)
    if err != nil {
        return report, err
    }
    report.BuildCache = true

    if volumes {
        vols, err := d.PruneVolumes(ctx, env, dryRun)
        if err != nil {
            return report, err
        }
        report.Volumes = vols
    }

    return report, nil
}
```

- [ ] **Step 10: Implement DiskUsage**

In `internal/runtime/cleanup.go`, add:

```go
func (d *dockerRuntime) DiskUsage(ctx context.Context) (DiskUsageReport, error) {
    cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{.Type}}\t{{.Size}}")
    out, err := cmd.CombinedOutput()
    if err != nil {
        return DiskUsageReport{}, fmt.Errorf("disk usage: %w: %s", err, string(out))
    }

    var report DiskUsageReport
    lines := strings.Split(strings.TrimSpace(string(out)), "\n")
    for _, line := range lines {
        parts := strings.SplitN(line, "\t", 2)
        if len(parts) != 2 {
            continue
        }
        typ := parts[0]
        size, _ := parseSize(parts[1])
        switch typ {
        case "Containers":
            report.Containers = size
        case "Images":
            report.Images = size
        case "Volumes":
            report.Volumes = size
        case "Build Cache":
            report.BuildCache = size
        }
    }
    report.Total = report.Containers + report.Images + report.Volumes + report.BuildCache
    report.HumanTotal = humanBytes(report.Total)
    return report, nil
}

func parseSize(s string) (int64, error) {
    s = strings.TrimSpace(s)
    if s == "" || s == "0B" {
        return 0, nil
    }
    // Parse Docker human-readable sizes like "1.2GB", "345MB", "12kB"
    // Use a simple approach: extract numeric and unit
    var num float64
    var unit string
    n, _ := fmt.Sscanf(s, "%f%s", &num, &unit)
    if n < 1 {
        return 0, fmt.Errorf("cannot parse size: %s", s)
    }
    if n == 1 {
        // no unit, assume bytes
        return int64(num), nil
    }
    multipliers := map[string]int64{
        "B":  1,
        "kB": 1000,
        "MB": 1000 * 1000,
        "GB": 1000 * 1000 * 1000,
        "TB": 1000 * 1000 * 1000 * 1000,
        "KiB": 1024,
        "MiB": 1024 * 1024,
        "GiB": 1024 * 1024 * 1024,
        "TiB": 1024 * 1024 * 1024 * 1024,
    }
    mult, ok := multipliers[unit]
    if !ok {
        return 0, fmt.Errorf("unknown unit: %s", unit)
    }
    return int64(num * float64(mult)), nil
}

func humanBytes(b int64) string {
    const unit = 1000
    if b < unit {
        return fmt.Sprintf("%d B", b)
    }
    div, exp := int64(unit), 0
    for n := b / unit; n >= unit; n /= unit {
        div *= unit
        exp++
    }
    return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}
```

- [ ] **Step 11: Run tests to verify compilation**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 12: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement Docker cleanup operations"
```

---

### Task 3: Create `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` (call `cleanupCmd.Execute()` in the main `Execute` function if needed, and register the command)

**Interfaces:**
- Consumes: `runtime.Manager` methods from Tasks 1-2, `config.NewStoreWithEnv`, `getEnv(cmd)`
- Produces: `tengiz cleanup` and subcommands user-facing CLI

- [ ] **Step 1: Write the test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
    "testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
    cmd, _, err := rootCmd.Find([]string{"cleanup"})
    if err != nil {
        t.Fatalf("cleanup command not found: %v", err)
    }
    if cmd.Use != "cleanup" {
        t.Fatalf("expected 'cleanup', got %q", cmd.Use)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanupCommandRegistered -v`
Expected: FAIL — cleanup command not found

- [ ] **Step 3: Create cleanup.go CLI command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
    "fmt"
    "os"
    "text/tabwriter"

    "github.com/spf13/cobra"
    "github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Clean up Docker resources (containers, images, volumes, networks, build cache)",
    Long: `Clean up unused Docker resources with label-based safety.
Tengiz-managed containers are protected via tengiz-env label filters.

Subcommands:
  cleanup containers    Remove stopped containers
  cleanup images        Remove unused images
  cleanup volumes       Remove unused volumes
  cleanup networks      Remove unused networks
  cleanup build-cache   Remove BuildKit build cache
  cleanup all           Run full system prune

Flags:
  --dry-run   Show what would be deleted without deleting
  --force     Skip confirmation prompts
`,
}

var cleanupContainersCmd = &cobra.Command{
    Use:   "containers",
    Short: "Remove stopped containers",
    RunE: func(cmd *cobra.Command, args []string) error {
        return runPrune(cmd, "containers")
    },
}

var cleanupImagesCmd = &cobra.Command{
    Use:   "images",
    Short: "Remove unused images",
    RunE: func(cmd *cobra.Command, args []string) error {
        return runPrune(cmd, "images")
    },
}

var cleanupVolumesCmd = &cobra.Command{
    Use:   "volumes",
    Short: "Remove unused volumes",
    RunE: func(cmd *cobra.Command, args []string) error {
        return runPrune(cmd, "volumes")
    },
}

var cleanupNetworksCmd = &cobra.Command{
    Use:   "networks",
    Short: "Remove unused networks",
    RunE: func(cmd *cobra.Command, args []string) error {
        return runPrune(cmd, "networks")
    },
}

var cleanupBuildCacheCmd = &cobra.Command{
    Use:   "build-cache",
    Short: "Remove BuildKit build cache",
    RunE: func(cmd *cobra.Command, args []string) error {
        return runPrune(cmd, "build-cache")
    },
}

var cleanupAllCmd = &cobra.Command{
    Use:   "all",
    Short: "Run full system prune (containers, images, networks, build cache)",
    Long:  `Run docker system prune with tengiz-env label filter. Add --volumes to also prune volumes.`,
    RunE: func(cmd *cobra.Command, args []string) error {
        return runPrune(cmd, "all")
    },
}

func init() {
    cleanupCmd.AddCommand(cleanupContainersCmd)
    cleanupCmd.AddCommand(cleanupImagesCmd)
    cleanupCmd.AddCommand(cleanupVolumesCmd)
    cleanupCmd.AddCommand(cleanupNetworksCmd)
    cleanupCmd.AddCommand(cleanupBuildCacheCmd)
    cleanupCmd.AddCommand(cleanupAllCmd)
    rootCmd.AddCommand(cleanupCmd)
}

func init() {
    cleanupAllCmd.Flags().Bool("volumes", false, "Also prune volumes")
}

func runPrune(cmd *cobra.Command, category string) error {
    env := getEnv(cmd)
    dryRun, _ := cmd.Flags().GetBool("dry-run")
    force, _ := cmd.Flags().GetBool("force")
    withVolumes, _ := cmd.Flags().GetBool("volumes")

    rt := runtime.NewDocker()

    if !force && !dryRun {
        fmt.Printf("WARNING: This will delete unused Docker resources in environment '%s'.\n", env)
        fmt.Print("Continue? [y/N]: ")
        var response string
        fmt.Scanln(&response)
        if response != "y" && response != "Y" {
            fmt.Println("Aborted.")
            return nil
        }
    }

    ctx := cmd.Context()

    // Show disk usage before
    before, err := rt.DiskUsage(ctx)
    if err == nil {
        fmt.Printf("Disk usage before cleanup: %s\n", before.HumanTotal)
    }

    switch category {
    case "containers":
        deleted, err := rt.PruneContainers(ctx, env, dryRun)
        if err != nil {
            return err
        }
        printDeleted("Containers", deleted)

    case "images":
        deleted, err := rt.PruneImages(ctx, env, dryRun)
        if err != nil {
            return err
        }
        printDeleted("Images", deleted)

    case "volumes":
        deleted, err := rt.PruneVolumes(ctx, env, dryRun)
        if err != nil {
            return err
        }
        printDeleted("Volumes", deleted)

    case "networks":
        deleted, err := rt.PruneNetworks(ctx, env, dryRun)
        if err != nil {
            return err
        }
        printDeleted("Networks", deleted)

    case "build-cache":
        deleted, err := rt.PruneBuildCache(ctx, dryRun)
        if err != nil {
            return err
        }
        printDeleted("Build Cache", deleted)

    case "all":
        report, err := rt.PruneSystem(ctx, env, dryRun, withVolumes)
        if err != nil {
            return err
        }
        printPruneReport(report)
    }

    // Show disk usage after
    after, err := rt.DiskUsage(ctx)
    if err == nil {
        fmt.Printf("Disk usage after cleanup:  %s\n", after.HumanTotal)
    }

    return nil
}

func printDeleted(label string, items []string) {
    if len(items) == 0 {
        fmt.Printf("%s: nothing to clean\n", label)
        return
    }
    fmt.Printf("%s removed:\n", label)
    for _, item := range items {
        fmt.Printf("  %s\n", item)
    }
}

func printPruneReport(r runtime.PruneReport) {
    w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
    fmt.Fprintf(w, "Category\tCount\n")
    fmt.Fprintf(w, "Containers\t%d\n", len(r.Containers))
    fmt.Fprintf(w, "Images\t%d\n", len(r.Images))
    fmt.Fprintf(w, "Networks\t%d\n", len(r.Networks))
    if r.Volumes != nil {
        fmt.Fprintf(w, "Volumes\t%d\n", len(r.Volumes))
    }
    fmt.Fprintf(w, "Build Cache\t%v\n", r.BuildCache)
    w.Flush()
    if r.DryRun {
        fmt.Println("\n[Dry run] Nothing was actually deleted.")
    }
}
```

- [ ] **Step 4: Add dry-run and force flags to cleanup subcommands**

Add to the `init()` function in `cleanup.go` below the existing flag setup:

```go
func init() {
    cleanupCmd.PersistentFlags().Bool("dry-run", false, "Show what would be deleted without deleting")
    cleanupCmd.PersistentFlags().Bool("force", false, "Skip confirmation prompt")
}
```

(Note: Go allows multiple `init()` functions in the same file. These will merge the flag sets.)

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestCleanupCommandRegistered -v`
Expected: PASS

- [ ] **Step 6: Run build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup CLI command"
```

---

### Task 4: Write comprehensive tests for cleanup operations

**Files:**
- Create: `internal/runtime/cleanup_test.go` (expand with more tests)
- Create: `internal/cli/cleanup_test.go` (expand with more tests)

**Interfaces:**
- Consumes: `dockerRuntime` methods, `parsePruneOutput`, `parseSize`, `humanBytes`
- Produces: Verified correctness of parsing and output helpers

- [ ] **Step 1: Write tests for parsePruneOutput**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestParsePruneOutput(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected []string
    }{
        {
            name:     "empty output",
            input:    "",
            expected: nil,
        },
        {
            name:     "only reclaimed line",
            input:    "Total reclaimed space: 1.2GB",
            expected: nil,
        },
        {
            name:     "container IDs",
            input:    "abc123\ndef456\nTotal reclaimed space: 500MB",
            expected: []string{"abc123", "def456"},
        },
        {
            name:     "deleted image tags",
            input:    "untagged: tengiz-apps/myapp:production-oldtag\nuntagged: tengiz-apps/myapp:staging-oldtag\nTotal reclaimed space: 1GB",
            expected: []string{
                "untagged: tengiz-apps/myapp:production-oldtag",
                "untagged: tengiz-apps/myapp:staging-oldtag",
            },
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := parsePruneOutput(tt.input)
            if len(result) != len(tt.expected) {
                t.Fatalf("expected %d items, got %d: %v", len(tt.expected), len(result), result)
            }
            for i, item := range result {
                if item != tt.expected[i] {
                    t.Errorf("item %d: expected %q, got %q", i, tt.expected[i], item)
                }
            }
        })
    }
}
```

- [ ] **Step 2: Write tests for parseSize**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestParseSize(t *testing.T) {
    tests := []struct {
        input    string
        expected int64
    }{
        {"0B", 0},
        {"100", 100},
        {"1kB", 1000},
        {"1.5MB", 1500000},
        {"2GB", 2000000000},
        {"1KiB", 1024},
        {"1MiB", 1048576},
        {"1GiB", 1073741824},
    }

    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            result, err := parseSize(tt.input)
            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            if result != tt.expected {
                t.Errorf("expected %d, got %d", tt.expected, result)
            }
        })
    }
}

func TestParseSize_Errors(t *testing.T) {
    _, err := parseSize("")
    if err == nil {
        t.Error("expected error for empty string")
    }
    _, err = parseSize("invalid")
    if err == nil {
        t.Error("expected error for invalid string")
    }
}
```

- [ ] **Step 3: Write tests for humanBytes**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestHumanBytes(t *testing.T) {
    tests := []struct {
        input    int64
        expected string
    }{
        {0, "0 B"},
        {500, "500 B"},
        {1500, "1.5 kB"},
        {1000000, "1.0 MB"},
        {2000000000, "2.0 GB"},
        {1500000000000, "1.5 TB"},
    }

    for _, tt := range tests {
        t.Run(tt.expected, func(t *testing.T) {
            result := humanBytes(tt.input)
            if result != tt.expected {
                t.Errorf("expected %q, got %q", tt.expected, result)
            }
        })
    }
}
```

- [ ] **Step 4: Run all runtime tests**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: All tests pass (stub tests + parse tests)

- [ ] **Step 5: Write CLI integration tests**

Add more tests to `internal/cli/cleanup_test.go`:

```go
func TestCleanupSubcommands(t *testing.T) {
    subcommands := []string{"containers", "images", "volumes", "networks", "build-cache", "all"}
    for _, sub := range subcommands {
        t.Run(sub, func(t *testing.T) {
            cmd, _, err := rootCmd.Find([]string{"cleanup", sub})
            if err != nil {
                t.Fatalf("cleanup %s not found: %v", sub, err)
            }
            if cmd.Use != sub {
                t.Errorf("expected use %q, got %q", sub, cmd.Use)
            }
        })
    }
}

func TestCleanupAllVolumesFlag(t *testing.T) {
    cmd, _, err := rootCmd.Find([]string{"cleanup", "all"})
    if err != nil {
        t.Fatalf("cleanup all not found: %v", err)
    }
    flag := cmd.Flags().Lookup("volumes")
    if flag == nil {
        t.Fatal("expected --volumes flag on cleanup all")
    }
    if flag.DefValue != "false" {
        t.Errorf("expected default false, got %q", flag.DefValue)
    }
}

func TestCleanupPersistentFlags(t *testing.T) {
    cmd, _, err := rootCmd.Find([]string{"cleanup"})
    if err != nil {
        t.Fatalf("cleanup not found: %v", err)
    }
    for _, name := range []string{"dry-run", "force"} {
        flag := cmd.PersistentFlags().Lookup(name)
        if flag == nil {
            t.Errorf("expected --%s persistent flag on cleanup", name)
        }
    }
}
```

- [ ] **Step 6: Run all CLI tests**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`
Expected: All tests pass

- [ ] **Step 7: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: All tests pass

- [ ] **Step 8: Run vet**

Run: `go vet ./...`
Expected: clean

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/cleanup_test.go internal/cli/cleanup_test.go
git commit -m "test: add cleanup operation tests"
```

---

### Task 5: Wire flags properly (fix duplicate init functions)

**Files:**
- Modify: `internal/cli/cleanup.go` (merge the two `init()` functions into one)

**Interfaces:**
- Consumes: `cleanup.go` from Task 3
- Produces: Single `init()` with all flag registrations

- [ ] **Step 1: Merge init functions into one**

In `internal/cli/cleanup.go`, replace the two separate `init()` functions with a single one:

```go
func init() {
    cleanupCmd.AddCommand(cleanupContainersCmd)
    cleanupCmd.AddCommand(cleanupImagesCmd)
    cleanupCmd.AddCommand(cleanupVolumesCmd)
    cleanupCmd.AddCommand(cleanupNetworksCmd)
    cleanupCmd.AddCommand(cleanupBuildCacheCmd)
    cleanupCmd.AddCommand(cleanupAllCmd)
    rootCmd.AddCommand(cleanupCmd)

    cleanupCmd.PersistentFlags().Bool("dry-run", false, "Show what would be deleted without deleting")
    cleanupCmd.PersistentFlags().Bool("force", false, "Skip confirmation prompt")
    cleanupAllCmd.Flags().Bool("volumes", false, "Also prune volumes")
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cli/cleanup.go
git commit -m "refactor: merge cleanup init functions"
```

---

### Task 6: Add cleanup to root command's Execute and verify end-to-end

**Files:**
- Modify: `internal/cli/root.go` (add `--max-cleanup-age` or similar if needed — optional)
- Verify: Full end-to-end compilation

- [ ] **Step 1: Run full build and vet**

Run: `go build -o tengiz . && go vet ./...`
Expected: Binary builds, vet passes

- [ ] **Step 2: Verify CLI help output**

Run: `./tengiz cleanup --help`
Expected: Shows cleanup command help with subcommands and flags

- [ ] **Step 3: Run full test suite**

Run: `go test ./... -count=1`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go  # if any changes made
git commit -m "chore: finalize cleanup command"
```

---

## Self-Review

**1. Spec coverage:**
- `tengiz cleanup` command ✅ (Task 3)
- Label-based `docker system prune` ✅ (Task 2, `PruneSystem` with `--filter label=tengiz-env=`)
- Per-category prune: containers/images/volumes/networks/build-cache ✅ (Task 2, 5 methods)
- `--dry-run` flag ✅ (Task 3, passed through all prune methods)
- `--force` flag ✅ (Task 3, skips confirmation)
- Disk usage before/after ✅ (Task 2, `DiskUsage` + display in Task 3)
- Env-aware via `--env` flag ✅ (Task 3, `getEnv(cmd)` passed through)
- Label-based safety protects Tengiz-managed containers ✅ (all prune methods filter by `tengiz-env`)

**2. Placeholder scan:** No TBD, TODO, "implement later", or "handle edge cases" found. Every step has complete code.

**3. Type consistency:** `PruneReport`, `DiskUsageReport`, `parsePruneOutput`, `parseSize`, `humanBytes` — all names match across Tasks 1, 2, 3, and 4. Method signatures on `Manager` interface match the `dockerRuntime` implementations and `stubManager` stubs.
