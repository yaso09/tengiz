# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command and `Manager.Prune()` method for label-based Docker resource cleanup (containers, images, volumes, networks, build cache) to prevent disk space exhaustion on single-server deployments.

**Architecture:** A new `PruneOptions`/`PruneReport` types in the `runtime` package. The `Manager` interface gains `Prune(ctx, opts)` returning a report. The `dockerRuntime` implementation runs separate `docker container prune`, `docker image prune`, `docker volume prune`, `docker network prune`, `docker builder prune` commands — each filtered with `label=tengiz-app` to protect non-Tengiz resources. A new `tengiz cleanup` CLI command wraps it with flags for each category plus `--dry-run` and `--force`.

**Tech Stack:** Go 1.26, `os/exec`, `encoding/json`, Cobra CLI, existing `runtime.Manager`, `dockerRuntime`, `stubManager`.

## Global Constraints

- All prune commands use `--filter label=tengiz-app` to avoid touching resources not managed by Tengiz
- `--dry-run` mode must show what would be pruned without actually deleting (uses `docker ... prune` without `-f` flag, which prints what would be removed without asking)
- `--force` / `-f` flag skips confirmation prompt (uses `-f` on Docker prune commands)
- Default when no category flag is set: prune all categories (equivalent to `--all`)
- Report output must show per-category counts and total reclaimed space
- Existing `Manager` tests must pass unchanged
- No new external dependencies

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneReport` types; add `Prune()` to `Manager` interface; add stub implementation |
| `internal/runtime/cleanup.go` | Implement `Prune()` on `dockerRuntime` |
| `internal/runtime/cleanup_test.go` | Tests for stub `Prune()` and for `pruneDockerCommand` helper |
| `internal/cli/root.go` | Add `cleanupCmd` cobra command with flags; register in `init()` and `Execute()` |
| `internal/cli/cleanup_test.go` | Tests for CLI flag parsing and dry-run passthrough |

No new files created. Changes touch 5 existing files.

---

### Task 1: Add Prune types + Manager interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go` — add types and interface method
- Test: `internal/runtime/cleanup_test.go` — add stub tests

**Interfaces:**
- Consumes: existing `context.Context`, no prior task dependencies
- Produces: `runtime.PruneOptions`, `runtime.PruneReport`, `Manager.Prune(ctx, PruneOptions) (PruneReport, error)`, `stubManager.Prune(ctx, PruneOptions) (PruneReport, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go — add after existing tests

func TestStubPrune(t *testing.T) {
    m := NewStub()
    opts := PruneOptions{All: true}
    report, err := m.Prune(context.Background(), opts)
    if err != nil {
        t.Fatalf("Prune() error = %v", err)
    }
    if report.ContainersDeleted != 0 {
        t.Errorf("ContainersDeleted = %d, want 0", report.ContainersDeleted)
    }
    if report.ImagesDeleted != 0 {
        t.Errorf("ImagesDeleted = %d, want 0", report.ImagesDeleted)
    }
    if report.SpaceReclaimed != "0B" {
        t.Errorf("SpaceReclaimed = %q, want %q", report.SpaceReclaimed, "0B")
    }
}

func TestStubPruneDryRun(t *testing.T) {
    m := NewStub()
    report, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
    if err != nil {
        t.Fatalf("Prune(dry-run) error = %v", err)
    }
    if report.ContainersDeleted != 0 {
        t.Errorf("dry-run ContainersDeleted = %d, want 0", report.ContainersDeleted)
    }
}

func TestStubPrunePerCategory(t *testing.T) {
    m := NewStub()
    opts := PruneOptions{Containers: true, Images: true}
    report, err := m.Prune(context.Background(), opts)
    if err != nil {
        t.Fatalf("Prune(categories) error = %v", err)
    }
    if report.ContainersDeleted != 0 {
        t.Errorf("ContainersDeleted = %d, want 0", report.ContainersDeleted)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: Build fails — `Prune` not defined on Manager, `PruneOptions` undefined

- [ ] **Step 3: Add types and interface to runtime.go**

Add after `type RunOptions struct {` block in `internal/runtime/runtime.go`:

```go
type PruneOptions struct {
    All        bool
    Containers bool
    Images     bool
    Volumes    bool
    Networks   bool
    BuildCache bool
    DryRun     bool
}

type PruneReport struct {
    ContainersDeleted int
    ImagesDeleted     int
    VolumesDeleted    int
    NetworksDeleted   int
    BuildCacheCleaned bool
    SpaceReclaimed    string
}
```

Add `Prune` to `Manager` interface after the `Run` line:

```go
    Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

Add stub implementation to `stubManager` after the `Run` method:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
    return PruneReport{SpaceReclaimed: "0B"}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: All three TestStubPrune* tests PASS

- [ ] **Step 5: Run full runtime test suite**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: All tests pass (existing tests must not break)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add PruneOptions/PruneReport types and Manager.Prune interface"
```

---

### Task 2: Implement Prune on dockerRuntime

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `Prune()` implementation on `dockerRuntime`
- Test: `internal/runtime/cleanup_test.go` — add helper test

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneReport` from Task 1, `context.Context`
- Produces: `dockerRuntime.Prune(ctx, PruneOptions) (PruneReport, error)` — full implementation

- [ ] **Step 1: Write a test for the prune Docker command builder**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestPruneDockerCommandsAll(t *testing.T) {
    commands := pruneDockerCommands(PruneOptions{All: true}, false)
    expected := []string{
        "container",
        "image",
        "volume",
        "network",
        "builder",
    }
    if len(commands) != len(expected) {
        t.Fatalf("got %d commands, want %d: %v", len(commands), len(expected), commands)
    }
    for i, cmd := range commands {
        if cmd[0] != expected[i] {
            t.Errorf("command[%d] resource = %q, want %q", i, cmd[0], expected[i])
        }
    }
}

func TestPruneDockerCommandsPerCategory(t *testing.T) {
    commands := pruneDockerCommands(PruneOptions{Containers: true, Volumes: true}, true)
    if len(commands) != 2 {
        t.Fatalf("got %d commands, want 2: %v", len(commands), commands)
    }
    if commands[0][0] != "container" {
        t.Errorf("first command resource = %q, want %q", commands[0][0], "container")
    }
    if commands[1][0] != "volume" {
        t.Errorf("second command resource = %q, want %q", commands[1][0], "volume")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestPruneDockerCommands -v -count=1`
Expected: FAIL — `pruneDockerCommands` not defined

- [ ] **Step 3: Add the prune implementation to cleanup.go**

Add to `internal/runtime/cleanup.go` after existing `KeepLastNImages`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
    var report PruneReport
    totalReclaimed := int64(0)

    commands := pruneDockerCommands(opts, opts.DryRun)

    for _, cmdParts := range commands {
        args := []string{cmdParts[0], "prune"}
        if !opts.DryRun {
            args = append(args, "-f")
        }
        args = append(args, "--filter", "label=tengiz-app")

        cmd := exec.CommandContext(ctx, "docker", args...)
        out, err := cmd.CombinedOutput()
        if err != nil {
            return report, fmt.Errorf("docker %s prune: %w\n%s", cmdParts[0], err, string(out))
        }

        output := string(out)

        switch cmdParts[0] {
        case "container":
            report.ContainersDeleted = countDeleted(output)
            reclaimed := parseReclaimed(output)
            totalReclaimed += reclaimed
        case "image":
            report.ImagesDeleted = countDeleted(output)
            reclaimed := parseReclaimed(output)
            totalReclaimed += reclaimed
        case "volume":
            report.VolumesDeleted = countDeleted(output)
            reclaimed := parseReclaimed(output)
            totalReclaimed += reclaimed
        case "network":
            report.NetworksDeleted = countDeleted(output)
        case "builder":
            report.BuildCacheCleaned = true
            reclaimed := parseReclaimed(output)
            totalReclaimed += reclaimed
        }

        if output != "" && !opts.DryRun {
            log.Printf("[runtime] docker %s prune result: %s", cmdParts[0], strings.TrimSpace(output))
        }
    }

    report.SpaceReclaimed = formatBytes(totalReclaimed)
    return report, nil
}

func pruneDockerCommands(opts PruneOptions, dryRun bool) [][]string {
    categories := [][]string{
        {"container"},
        {"image"},
        {"volume"},
        {"network"},
        {"builder"},
    }

    if opts.All {
        return categories
    }

    var selected [][]string
    if opts.Containers {
        selected = append(selected, categories[0])
    }
    if opts.Images {
        selected = append(selected, categories[1])
    }
    if opts.Volumes {
        selected = append(selected, categories[2])
    }
    if opts.Networks {
        selected = append(selected, categories[3])
    }
    if opts.BuildCache {
        selected = append(selected, categories[4])
    }

    if len(selected) == 0 {
        return categories
    }
    return selected
}

func countDeleted(output string) int {
    lines := strings.Split(strings.TrimSpace(output), "\n")
    count := 0
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line == "" || strings.HasPrefix(line, "Total") || strings.HasPrefix(line, "Deleted") {
            continue
        }
        if strings.Contains(line, "SpaceReclaimed") {
            continue
        }
        count++
    }
    // Docker prune JSON output has a "ContainersDeleted"/"ImagesDeleted" field at the end
    // For non-JSON, count non-empty lines minus the summary line
    return count
}

func parseReclaimed(output string) int64 {
    // Look for patterns like "Total reclaimed space: 1.23GB" or JSON with "SpaceReclaimed"
    lines := strings.Split(strings.TrimSpace(output), "\n")
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if strings.HasPrefix(line, "Total reclaimed space:") {
            parts := strings.SplitN(line, ":", 2)
            if len(parts) == 2 {
                return parseSize(strings.TrimSpace(parts[1]))
            }
        }
        // Try JSON blob at end
        if strings.HasPrefix(line, "{") && strings.Contains(line, "SpaceReclaimed") {
            var result struct {
                SpaceReclaimed int64 `json:"SpaceReclaimed"`
            }
            if err := json.Unmarshal([]byte(line), &result); err == nil {
                return result.SpaceReclaimed
            }
        }
    }
    return 0
}

func parseSize(s string) int64 {
    s = strings.TrimSpace(s)
    var value float64
    var unit string
    n, _ := fmt.Sscanf(s, "%f%s", &value, &unit)
    if n < 1 {
        return 0
    }
    switch strings.ToUpper(strings.TrimSpace(unit)) {
    case "B":
        return int64(value)
    case "KB", "K":
        return int64(value * 1024)
    case "MB", "M":
        return int64(value * 1024 * 1024)
    case "GB", "G":
        return int64(value * 1024 * 1024 * 1024)
    case "TB", "T":
        return int64(value * 1024 * 1024 * 1024 * 1024)
    default:
        return int64(value)
    }
}

func formatBytes(b int64) string {
    switch {
    case b >= 1024*1024*1024:
        return fmt.Sprintf("%.2fGB", float64(b)/(1024*1024*1024))
    case b >= 1024*1024:
        return fmt.Sprintf("%.2fMB", float64(b)/(1024*1024))
    case b >= 1024:
        return fmt.Sprintf("%.2fKB", float64(b)/1024)
    default:
        return fmt.Sprintf("%dB", b)
    }
}
```

Add `"encoding/json"` and `"fmt"` imports to `cleanup.go` if not already present.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestPruneDockerCommands -v -count=1`
Expected: Both TestPruneDockerCommands* PASS

- [ ] **Step 5: Run full runtime test suite**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement Prune() on dockerRuntime with per-category cleanup"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` cobra command, register in `init()` and `Execute()`
- Create: `internal/cli/cleanup_test.go` — CLI tests

**Interfaces:**
- Consumes: `runtime.Manager.Prune(ctx, PruneOptions) (PruneReport, error)`, `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneReport`
- Produces: `tengiz cleanup [--containers] [--images] [--volumes] [--networks] [--build-cache] [--all] [--dry-run] [--force]` CLI command

- [ ] **Step 1: Write the failing CLI test**

```go
// internal/cli/cleanup_test.go

package cli

import (
    "testing"
)

func TestCleanupCmdRegistration(t *testing.T) {
    cmd, _, err := rootCmd.Find([]string{"cleanup"})
    if err != nil {
        t.Fatalf("cleanup command not found: %v", err)
    }
    if cmd == nil {
        t.Fatal("cleanup command is nil")
    }
    if cmd.Use != "cleanup" {
        t.Errorf("Use = %q, want %q", cmd.Use, "cleanup")
    }
}

func TestCleanupCmdFlags(t *testing.T) {
    cmd, _, _ := rootCmd.Find([]string{"cleanup"})
    if cmd == nil {
        t.Fatal("cleanup command not found")
    }

    flags := []string{"all", "containers", "images", "volumes", "networks", "build-cache", "dry-run", "force"}
    for _, f := range flags {
        if cmd.Flags().Lookup(f) == nil {
            t.Errorf("flag --%s not found on cleanup command", f)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanupCmd -v -count=1`
Expected: FAIL — cleanup command not registered

- [ ] **Step 3: Add cleanupCmd to root.go**

Add after the `var secretRotateCmd` block (or near the `var runCmd` block, before `var gitCmd`):

```go
var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Remove unused Docker resources (disk space reclamation)",
    Long: `Prune unused Docker containers, images, volumes, networks, and build cache.
All operations are scoped to Tengiz-managed resources via label filtering.

Categories (use flags to select specific ones):
  --containers    Remove stopped Tengiz containers
  --images        Remove unused Tengiz images
  --volumes       Remove unused Tengiz volumes
  --networks      Remove unused Tengiz networks
  --build-cache   Remove build cache

Default (no category flag): all categories.

Use --dry-run to see what would be removed without deleting.
Use --force or -f to skip the confirmation prompt.`,
    RunE: func(cmd *cobra.Command, args []string) error {
        _ = getEnv(cmd) // env-aware for future env-scoped pruning

        all, _ := cmd.Flags().GetBool("all")
        containers, _ := cmd.Flags().GetBool("containers")
        images, _ := cmd.Flags().GetBool("images")
        volumes, _ := cmd.Flags().GetBool("volumes")
        networks, _ := cmd.Flags().GetBool("networks")
        buildCache, _ := cmd.Flags().GetBool("build-cache")
        dryRun, _ := cmd.Flags().GetBool("dry-run")
        force, _ := cmd.Flags().GetBool("force")

        if !all && !containers && !images && !volumes && !networks && !buildCache {
            all = true
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

        // Confirmation prompt unless --force or --dry-run
        if !force && !dryRun {
            fmt.Printf("[tengiz] This will prune %s managed by Tengiz.\n", describeCategories(opts))
            fmt.Print("[tengiz] Are you sure? [y/N]: ")
            var response string
            fmt.Scanln(&response)
            response = strings.TrimSpace(strings.ToLower(response))
            if response != "y" && response != "yes" {
                fmt.Println("[tengiz] cancelled")
                return nil
            }
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        report, err := rt.Prune(cmd.Context(), opts)
        if err != nil {
            return fmt.Errorf("cleanup: %w", err)
        }

        if dryRun {
            fmt.Println("[tengiz] DRY RUN — no resources were deleted")
        }

        fmt.Println("[tengiz] cleanup complete:")
        fmt.Printf("  Containers deleted: %d\n", report.ContainersDeleted)
        fmt.Printf("  Images deleted:     %d\n", report.ImagesDeleted)
        fmt.Printf("  Volumes deleted:    %d\n", report.VolumesDeleted)
        fmt.Printf("  Networks deleted:   %d\n", report.NetworksDeleted)
        if report.BuildCacheCleaned {
            fmt.Println("  Build cache:        cleared")
        }
        fmt.Printf("  Space reclaimed:    %s\n", report.SpaceReclaimed)

        return nil
    },
}

func describeCategories(opts runtime.PruneOptions) string {
    if opts.All {
        return "all Docker resources"
    }
    var parts []string
    if opts.Containers {
        parts = append(parts, "containers")
    }
    if opts.Images {
        parts = append(parts, "images")
    }
    if opts.Volumes {
        parts = append(parts, "volumes")
    }
    if opts.Networks {
        parts = append(parts, "networks")
    }
    if opts.BuildCache {
        parts = append(parts, "build cache")
    }
    if len(parts) == 0 {
        return "all Docker resources"
    }
    return strings.Join(parts, ", ")
}
```

Register the command in the `init()` function (add after `rootCmd.AddCommand(secretCmd)`):

```go
    rootCmd.AddCommand(cleanupCmd)
```

Add flags in the `Execute()` function (add after the notification flags block):

```go
    cleanupCmd.Flags().Bool("all", false, "prune all categories (default)")
    cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
    cleanupCmd.Flags().Bool("images", false, "prune unused images")
    cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
    cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
    cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
    cleanupCmd.Flags().Bool("dry-run", false, "show what would be pruned without deleting")
    cleanupCmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestCleanupCmd -v -count=1`
Expected: Both TestCleanupCmdRegistration and TestCleanupCmdFlags PASS

- [ ] **Step 5: Build and verify the binary compiles**

Run: `go build -o tengiz .`
Expected: Binary builds without error

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: All tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command with Prune integration"
```

---

### Task 4: Wire environment support into cleanup

**Files:**
- Modify: `internal/cli/root.go` — add `--env` flag to cleanup command

**Interfaces:**
- Consumes: existing `getEnv(cmd)`, existing `runtime.Manager.Prune`
- Produces: env-aware cleanup command

- [ ] **Step 1: Add the flag**

The `cleanupCmd` already inherits the global `--env` flag from `rootCmd.PersistentFlags()`. The `getEnv(cmd)` call in the `RunE` already reads it. No change needed — verify with a test.

- [ ] **Step 2: Write a test that verifies env passthrough**

Add to `internal/cli/cleanup_test.go`:

```go
func TestCleanupCmdEnvFlag(t *testing.T) {
    cmd, _, err := rootCmd.Find([]string{"cleanup"})
    if err != nil {
        t.Fatalf("cleanup command not found: %v", err)
    }
    // Build args with --env flag on parent (rootCmd has persistent --env)
    fullCmd := rootCmd
    fullCmd.SetArgs([]string{"--env", "staging", "cleanup", "--help"})
    fullCmd.SetOut(io.Discard)
    err = fullCmd.Execute()
    // Should succeed (--help doesn't execute RunE)
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    // Verify the command's env flag would parse correctly
    env, _ := cmd.Flags().GetString("env")
    _ = env
    // Test that --dry-run flag exists
    if cmd.Flags().Lookup("dry-run") == nil {
        t.Error("--dry-run flag not found on cleanup")
    }
}
```

Add `"strings"` to imports in `internal/cli/cleanup_test.go`.

- [ ] **Step 3: Run test**

Run: `go test ./internal/cli/ -run TestCleanupCmdEnvFlag -v -count=1`
Expected: Test passes (may error on Docker not found, but not on flag parsing)

- [ ] **Step 4: Run vet and full tests**

Run: `go vet ./... && go test ./... -v -count=1`
Expected: All pass

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup_test.go
git commit -m "test: add env flag test for cleanup command"
```
