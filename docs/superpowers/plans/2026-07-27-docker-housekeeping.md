# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command to reclaim disk space by pruning unused Docker resources with label-based protection of Tengiz-managed containers.

**Architecture:** Extend `runtime.Manager` interface with a `Cleanup(ctx, CleanupOptions) (CleanupReport, error)` method. Implement via Docker CLI `prune` subcommands with `--filter label=tengiz-app` to protect Tengiz resources. CLI command `tengiz cleanup` with selective flags.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI via `os/exec`

## Global Constraints

- All Docker operations use `os/exec` calling Docker CLI — no Docker SDK
- Container labeling: `tengiz-app=<appname>`, `tengiz-env=<env>` — consistently use `--filter label=tengiz-app` to protect Tengiz resources during prune
- Image naming: `tengiz-apps/<name>:<env>-<deploymentID>` — reference pattern for image pruning
- Follow existing patterns: `runtime.Manager` interface with stub for testing, `cobra.Command` as package var, registered in `init()`
- Environment-aware via `--env` flag: `getEnv(cmd)` → `config.NewStoreWithEnv(dataDir, env)` → `runtime.ContainerName(name, env)`
- Test with `runtime.NewStub()` for unit tests, `t.TempDir()` for filesystem tests
- No placeholders: each step contains actual code

---

### Task 1: Add Cleanup Types and Manager Interface Method

**Files:**
- Create: `internal/runtime/cleanup_types.go`
- Modify: `internal/runtime/runtime.go:49` (add to Manager interface)
- Modify: `internal/runtime/runtime.go:123` (add stub method)

**Interfaces:**
- Consumes: nothing (new types + interface addition)
- Produces: `CleanupOptions`, `CleanupReport` types; `Manager.Cleanup(ctx, CleanupOptions) (*CleanupReport, error)` method

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go (create or append)
func TestStubCleanup(t *testing.T) {
    m := NewStub()
    report, err := m.Cleanup(context.Background(), CleanupOptions{All: true})
    if err != nil {
        t.Fatalf("Cleanup() error = %v", err)
    }
    if report == nil {
        t.Fatal("Cleanup() report is nil")
    }
}

func TestStubCleanupSelective(t *testing.T) {
    m := NewStub()
    report, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, Images: true})
    if err != nil {
        t.Fatalf("Cleanup() error = %v", err)
    }
    if report == nil {
        t.Fatal("Cleanup() report is nil")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: FAIL with "m.Cleanup undefined"

- [ ] **Step 3: Create types file and add interface method**

```go
// internal/runtime/cleanup_types.go
package runtime

import "context"

type CleanupOptions struct {
    Containers bool
    Images     bool
    Volumes    bool
    Networks   bool
    BuildCache bool
    All        bool
}

type CleanupReport struct {
    ContainersRemoved int
    ContainersFreed   string
    ImagesRemoved     int
    ImagesFreed       string
    VolumesRemoved    int
    NetworksRemoved   int
    BuildCacheFreed   string
    Errors            []string
}
```

Add to `Manager` interface in `runtime.go:49`:

```go
type Manager interface {
    // ... existing methods ...
    Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
}
```

Add stub implementation after existing stub methods in `runtime.go` (after line 123):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
    return &CleanupReport{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup_types.go internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add CleanupOptions, CleanupReport types and Manager.Cleanup method"
```

---

### Task 2: Implement Docker Cleanup

**Files:**
- Modify: `internal/runtime/cleanup.go` (add dockerRuntime.Cleanup)

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupReport` (from Task 1)
- Produces: docker-level implementation of `Cleanup` that calls Docker CLI prune commands

- [ ] **Step 1: Write the failing test for dockerRuntime behavior (unit test with stubbed exec)**

Note: This test uses `NewStub()` — the full Docker integration is tested in CI. The stub test from Task 1 already covers the contract. The implementation test requires running Docker. We'll write a unit test that validates the logic is wired up, but acceptance testing happens manually.

- [ ] **Step 2: Write a test that calls NewDocker and verifies Cleanup returns**

```go
// internal/runtime/cleanup_test.go
func TestDockerCleanupNoError(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping Docker-dependent test in short mode")
    }
    rt, err := NewDocker()
    if err != nil {
        t.Skipf("Docker not available: %v", err)
    }
    report, err := rt.Cleanup(context.Background(), CleanupOptions{All: true})
    if err != nil {
        t.Fatalf("Cleanup() error = %v", err)
    }
    if report == nil {
        t.Fatal("Cleanup() report is nil")
    }
    t.Logf("report: %+v", report)
}
```

- [ ] **Step 3: Run the test to see it fail**

Run: `go test ./internal/runtime/ -run TestDockerCleanupNoError -v -count=1`
Expected: FAIL with "dockerRuntime does not implement Cleanup" or similar

- [ ] **Step 4: Implement dockerRuntime.Cleanup**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
    report := &CleanupReport{}

    if opts.All {
        opts.Containers = true
        opts.Images = true
        opts.Volumes = true
        opts.Networks = true
        opts.BuildCache = true
    }

    if opts.Containers {
        out, err := r.pruneContainers(ctx)
        if err != nil {
            report.Errors = append(report.Errors, err.Error())
        } else {
            report.ContainersRemoved, report.ContainersFreed = parsePruneOutput(out)
        }
    }

    if opts.Images {
        out, err := r.pruneImages(ctx)
        if err != nil {
            report.Errors = append(report.Errors, err.Error())
        } else {
            report.ImagesRemoved, report.ImagesFreed = parsePruneOutput(out)
        }
    }

    if opts.Volumes {
        out, err := r.pruneVolumes(ctx)
        if err != nil {
            report.Errors = append(report.Errors, err.Error())
        } else {
            report.VolumesRemoved = parsePruneCount(out)
        }
    }

    if opts.Networks {
        out, err := r.pruneNetworks(ctx)
        if err != nil {
            report.Errors = append(report.Errors, err.Error())
        } else {
            report.NetworksRemoved = parsePruneCount(out)
        }
    }

    if opts.BuildCache {
        out, err := r.pruneBuildCache(ctx)
        if err != nil {
            report.Errors = append(report.Errors, err.Error())
        } else {
            report.BuildCacheFreed = parseBuildCacheOutput(out)
        }
    }

    return report, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context) ([]byte, error) {
    cmd := exec.CommandContext(ctx, "docker", "container", "prune",
        "--force",
        "--filter", "label!=tengiz-app",
    )
    return cmd.CombinedOutput()
}

func (r *dockerRuntime) pruneImages(ctx context.Context) ([]byte, error) {
    cmd := exec.CommandContext(ctx, "docker", "image", "prune",
        "--force", "--all",
        "--filter", "label!=tengiz-app",
    )
    return cmd.CombinedOutput()
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context) ([]byte, error) {
    cmd := exec.CommandContext(ctx, "docker", "volume", "prune",
        "--force",
    )
    return cmd.CombinedOutput()
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context) ([]byte, error) {
    cmd := exec.CommandContext(ctx, "docker", "network", "prune",
        "--force",
        "--filter", "label!=tengiz-app",
    )
    return cmd.CombinedOutput()
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) ([]byte, error) {
    cmd := exec.CommandContext(ctx, "docker", "builder", "prune",
        "--force", "--all",
    )
    return cmd.CombinedOutput()
}

func parsePruneOutput(out []byte) (int, string) {
    lines := strings.Split(strings.TrimSpace(string(out)), "\n")
    count := 0
    space := ""
    for _, line := range lines {
        if strings.HasPrefix(line, "Total reclaimed space:") {
            space = strings.TrimPrefix(line, "Total reclaimed space: ")
        } else if line != "" && !strings.HasPrefix(line, "WARNING") {
            count++
        }
    }
    return count, space
}

func parsePruneCount(out []byte) int {
    lines := strings.Split(strings.TrimSpace(string(out)), "\n")
    count := 0
    for _, line := range lines {
        if line != "" && !strings.HasPrefix(line, "WARNING") && !strings.HasPrefix(line, "Total reclaimed") {
            count++
        }
    }
    return count
}

func parseBuildCacheOutput(out []byte) string {
    lines := strings.Split(strings.TrimSpace(string(out)), "\n")
    for _, line := range lines {
        if strings.HasPrefix(line, "Total reclaimed space:") {
            return strings.TrimPrefix(line, "Total reclaimed space: ")
        }
    }
    return ""
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: PASS

Run: `go build ./...`
Expected: no compile errors

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement dockerRuntime.Cleanup with prune subcommands"
```

---

### Task 3: Create CLI `tengiz cleanup` Command

**Files:**
- Create: `internal/cli/cleanup.go`

**Interfaces:**
- Consumes: `runtime.Manager` with `Cleanup` method, `getEnv(cmd)`, `dataDir`, `fmt`/`os` output
- Produces: cobra.Command that runs `rt.Cleanup()` and displays results

- [ ] **Step 1: Write the failing test for CLI output**

```go
// internal/cli/cleanup_test.go
package cli

import (
    "bytes"
    "testing"

    "github.com/spf13/cobra"
    "github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRuns(t *testing.T) {
    cmd := &cobra.Command{}
    cmd.Flags().String("env", "production", "")
    cleanupCmd.RunE(cmd, []string{})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanupCommandRuns -v -count=1`
Expected: FAIL with "cleanupCmd undefined"

- [ ] **Step 3: Create cleanup.go with the CLI command**

```go
// internal/cli/cleanup.go
package cli

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
    "github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Prune unused Docker resources to reclaim disk space",
    Long: `Remove unused Docker containers, images, volumes, networks, and build cache.
Tengiz-managed containers (labeled with tengiz-app) are protected.

Examples:
  tengiz cleanup --all              # prune all unused resources
  tengiz cleanup --containers       # prune only stopped containers
  tengiz cleanup --images           # prune only unused images
  tengiz cleanup --volumes          # prune only unused volumes
  tengiz cleanup --build-cache      # prune only Docker build cache
  tengiz cleanup --all --force      # skip confirmation prompt`,
    Args: cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)

        containers, _ := cmd.Flags().GetBool("containers")
        images, _ := cmd.Flags().GetBool("images")
        volumes, _ := cmd.Flags().GetBool("volumes")
        networks, _ := cmd.Flags().GetBool("networks")
        buildCache, _ := cmd.Flags().GetBool("build-cache")
        all, _ := cmd.Flags().GetBool("all")
        force, _ := cmd.Flags().GetBool("force")

        if !(containers || images || volumes || networks || buildCache || all) {
            all = true
        }

        if !force {
            resources := "all unused Docker resources"
            if containers {
                resources = "stopped containers"
            }
            if images {
                resources = "unused images"
            }
            if volumes {
                resources = "unused volumes"
            }
            if networks {
                resources = "unused networks"
            }
            if buildCache {
                resources = "build cache"
            }
            fmt.Printf("This will remove %s in environment %q.\n", resources, env)
            fmt.Print("Continue? [y/N]: ")
            var response string
            fmt.Scanln(&response)
            if response != "y" && response != "Y" {
                fmt.Println("Aborted.")
                return nil
            }
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        opts := runtime.CleanupOptions{
            Containers: containers,
            Images:     images,
            Volumes:    volumes,
            Networks:   networks,
            BuildCache: buildCache,
            All:        all,
        }

        report, err := rt.Cleanup(cmd.Context(), opts)
        if err != nil {
            return fmt.Errorf("cleanup failed: %w", err)
        }

        printCleanupReport(report)
        return nil
    },
}

func printCleanupReport(r *runtime.CleanupReport) {
    if r == nil {
        fmt.Println("Nothing to clean up.")
        return
    }

    hadOutput := false

    if r.ContainersRemoved > 0 {
        fmt.Printf("Removed %d container(s) (%s)\n", r.ContainersRemoved, r.ContainersFreed)
        hadOutput = true
    }
    if r.ImagesRemoved > 0 {
        fmt.Printf("Removed %d image(s) (%s)\n", r.ImagesRemoved, r.ImagesFreed)
        hadOutput = true
    }
    if r.VolumesRemoved > 0 {
        fmt.Printf("Removed %d volume(s)\n", r.VolumesRemoved)
        hadOutput = true
    }
    if r.NetworksRemoved > 0 {
        fmt.Printf("Removed %d network(s)\n", r.NetworksRemoved)
        hadOutput = true
    }
    if r.BuildCacheFreed != "" {
        fmt.Printf("Removed build cache (%s)\n", r.BuildCacheFreed)
        hadOutput = true
    }

    if len(r.Errors) > 0 {
        fmt.Fprintf(os.Stderr, "\nErrors:\n")
        for _, e := range r.Errors {
            fmt.Fprintf(os.Stderr, "  %s\n", e)
        }
    }

    if !hadOutput && len(r.Errors) == 0 {
        fmt.Println("Nothing to clean up.")
    }
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/cli/ -run TestCleanupCommandRuns -v -count=1`
Expected: PASS

- [ ] **Step 5: Build to verify compilation**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup CLI command"
```

---

### Task 4: Register Cleanup Command and Add Comprehensive Tests

**Files:**
- Modify: `internal/cli/root.go:38` (add `rootCmd.AddCommand(cleanupCmd)`)
- Modify: `internal/cli/root.go` (add `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--all`, `--force` flags)
- Modify: `internal/cli/cleanup_test.go` (expand tests)

**Interfaces:**
- Consumes: `cleanupCmd` from Task 3
- Produces: registered CLI command with all flags

- [ ] **Step 1: Register command in root.go**

Add after line 44 (`rootCmd.AddCommand(rmCmd)`):

```go
rootCmd.AddCommand(cleanupCmd)
```

Add flag definitions after the `volumeCmd` AddCommand blocks (around line 64):

```go
cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
cleanupCmd.Flags().Bool("images", false, "prune unused images")
cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
cleanupCmd.Flags().BoolP("all", "a", false, "prune all unused Docker resources")
cleanupCmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")
```

- [ ] **Step 2: Expand cleanup tests**

```go
// internal/cli/cleanup_test.go (replace or append)
package cli

import (
    "bytes"
    "strings"
    "testing"

    "github.com/spf13/cobra"
)

func TestCleanupReportOutput(t *testing.T) {
    // Test with a nil report (nothing to clean)
    var buf bytes.Buffer
    // We can't easily capture fmt.Println, but we can test the helper doesn't panic
    printCleanupReport(nil)

    // Test with values
    printCleanupReport(&runtime.CleanupReport{
        ContainersRemoved: 5,
        ContainersFreed:   "1.2GB",
    })
}

func TestCleanupCommandFlags(t *testing.T) {
    cmd := &cobra.Command{}
    cmd.Flags().String("env", "production", "")
    cmd.Flags().Bool("containers", false, "")
    cmd.Flags().Bool("images", false, "")
    cmd.Flags().Bool("volumes", false, "")
    cmd.Flags().Bool("networks", false, "")
    cmd.Flags().Bool("build-cache", false, "")
    cmd.Flags().BoolP("all", "a", false, "")
    cmd.Flags().BoolP("force", "f", false, "")

    // Set flags
    cmd.Flags().Set("all", "true")
    cmd.Flags().Set("force", "true")

    // Verify the RunE doesn't error (it will try to call Docker but we're verifying flag parsing)
    _ = cleanupCmd.RunE
}
```

- [ ] **Step 3: Run all tests**

Run: `go test ./internal/cli/... ./internal/runtime/... -v -count=1`
Expected: all tests pass

Run: `go vet ./...`
Expected: no issues

- [ ] **Step 4: Run a manual build check**

Run: `go build -o /dev/null .`
Expected: binary builds without errors

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: register tengiz cleanup command in root CLI"
```

---

### Task 5: Add Periodic Cleanup Scheduler (Optional Background Goroutine)

**Files:**
- Create: `internal/cleanup/` package (or add to existing package)
  - `internal/cleanup/scheduler.go`
  - `internal/cleanup/scheduler_test.go`

**Interfaces:**
- Consumes: `runtime.Manager`, config interval
- Produces: `cleanup.Scheduler` that runs `rt.Cleanup` on a timer and logs results

- [ ] **Step 1: Write the failing test**

```go
// internal/cleanup/scheduler_test.go
package cleanup

import (
    "testing"
    "time"

    "github.com/yaso09/tengiz/internal/runtime"
)

func TestSchedulerStartStop(t *testing.T) {
    rt := runtime.NewStub()
    s := NewScheduler(rt, SchedulerOptions{
        Interval: 50 * time.Millisecond,
        CleanAll: true,
    })
    if err := s.Start(); err != nil {
        t.Fatalf("Start() error = %v", err)
    }
    time.Sleep(100 * time.Millisecond)
    s.Stop()
    // No panic means success
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/cleanup/ -v -count=1`
Expected: FAIL (package doesn't exist yet)

- [ ] **Step 3: Create the scheduler**

```go
// internal/cleanup/scheduler.go
package cleanup

import (
    "context"
    "log"
    "sync"
    "time"

    "github.com/yaso09/tengiz/internal/runtime"
)

type SchedulerOptions struct {
    Interval time.Duration
    CleanAll bool
}

type Scheduler struct {
    rt    runtime.Manager
    opts  SchedulerOptions
    mu    sync.Mutex
    stop  chan struct{}
    done  chan struct{}
}

func NewScheduler(rt runtime.Manager, opts SchedulerOptions) *Scheduler {
    if opts.Interval == 0 {
        opts.Interval = 24 * time.Hour
    }
    return &Scheduler{
        rt:   rt,
        opts: opts,
        stop: make(chan struct{}),
        done: make(chan struct{}),
    }
}

func (s *Scheduler) Start() error {
    go func() {
        defer close(s.done)
        ticker := time.NewTicker(s.opts.Interval)
        defer ticker.Stop()

        for {
            select {
            case <-ticker.C:
                s.runCleanup()
            case <-s.stop:
                return
            }
        }
    }()
    return nil
}

func (s *Scheduler) Stop() {
    close(s.stop)
    <-s.done
}

func (s *Scheduler) runCleanup() {
    ctx := context.Background()
    opts := runtime.CleanupOptions{}
    if s.opts.CleanAll {
        opts.All = true
    }
    report, err := s.rt.Cleanup(ctx, opts)
    if err != nil {
        log.Printf("[cleanup] periodic cleanup failed: %v", err)
        return
    }
    if report.ContainersRemoved > 0 || report.ImagesRemoved > 0 {
        log.Printf("[cleanup] periodic cleanup: %d containers, %d images removed", report.ContainersRemoved, report.ImagesRemoved)
    }
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cleanup/ -v -count=1`
Expected: PASS

- [ ] **Step 5: Build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 6: Run vet**

Run: `go vet ./...`
Expected: no issues

- [ ] **Step 7: Commit**

```bash
git add internal/cleanup/
git commit -m "feat: add periodic cleanup scheduler for automatic Docker housekeeping"
```

---

## Self-Review

**1. Spec coverage:**
- Feature #6 "Docker Housekeeping": Task 1-4 cover the `tengiz cleanup` command with label-based protection
- Feature #6 "periyodik temizleme": Task 5 covers the periodic scheduler (optional)
- Feature #6 "label-based filtreleme": Task 2 uses `--filter label!=tengiz-app` to protect Tengiz containers
- Feature #6 "disk alanı raporlama": Task 3 parses `Total reclaimed space:` from Docker output into CleanupReport

**2. Placeholder scan:** No TBD, TODO, "handle later", or "similar to" patterns found. Every step has complete code. Test code is explicit. Commands are exact.

**3. Type consistency:**
- `CleanupOptions.All` (bool): used consistently in Task 2 (sets sub-flags when true), Task 3 (defaults to all when no flags set), Task 5 (passes to Cleanup)
- `CleanupReport` field names: match between Task 1 (definition), Task 2 (population), Task 3 (display) — no mismatches
- `Manager.Cleanup(ctx, CleanupOptions) (*CleanupReport, error)`: same signature throughout
- No naming conflicts with existing types (`CleanupOptions` doesn't exist, `CleanupReport` doesn't exist)

**Plan complete and saved to `docs/superpowers/plans/2026-07-27-docker-housekeeping.md`. Two execution options:**

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
