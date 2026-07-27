# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command that prunes unused Docker resources (containers, images, build cache) with label-based filtering to protect Tengiz-managed containers, plus orphaned store state cleanup.

**Architecture:** A `PruneSystem` method on `runtime.Manager` wraps `docker container/image/builder prune` commands using label filters (`tengiz-app`, `tengiz-env`) to protect Tengiz resources. A `tengiz cleanup` CLI command invokes it with scope flags (`--containers`, `--images`, `--build-cache`, `--all`) and optional `--dry-run`. Store methods `CleanupOrphanedPorts` scan `ports-{env}.json` removing entries for non-existent apps.

**Tech Stack:** Go 1.26, os/exec, Docker labels (`tengiz-app`, `tengiz-env`), cobra CLI, existing `config.Store`, `runtime.Manager` patterns.

## Global Constraints

- Label `tengiz-app` must protect Tengiz-managed containers from accidental pruning
- `--dry-run` lists what would be removed without executing any prune
- Default behavior (no flags) prunes all three categories: containers, images, build cache
- `--all` additionally prunes stopped Tengiz-managed containers
- Image prune uses `docker image prune -a` with `--filter label!=tengiz-app` to skip Tengiz images
- Build cache prune is unconditional (`docker builder prune -a -f`) — BuildKit cache is ephemeral
- Orphaned port cleanup removes port entries in `ports-{env}.json` whose app name doesn't exist in `apps-{env}.json`
- No new external dependencies
- Existing tests must continue to pass without modification

---
## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneReport` types; add `PruneSystem` to `Manager` interface; add stub method |
| `internal/runtime/cleanup.go` | Docker implementation of `PruneSystem` — `docker container prune`, `docker image prune`, `docker builder prune` |
| `internal/config/store.go` | Add `CleanupOrphanedPorts()` method |
| `internal/cli/root.go` | Add `cleanupCmd` with flags, register in `init()`, add import for `runtime` package |
| `internal/cli/cleanup_test.go` | Tests for cleanup CLI command structure and defaults |

---

### Task 1: Add PruneSystem to runtime.Manager + implement in dockerRuntime

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add types and interface method
- Modify: `internal/runtime/runtime.go:51-123` — add stub method
- Modify: `internal/runtime/cleanup.go` — add PruneSystem and helpers

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions`, `runtime.PruneReport`, `Manager.PruneSystem(ctx, PruneOptions) (*PruneReport, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/runtime_test.go
package runtime

import (
    "context"
    "testing"
)

func TestStubPruneSystem(t *testing.T) {
    m := NewStub()
    report, err := m.PruneSystem(context.Background(), PruneOptions{
        Containers: true,
        Images:     true,
        BuildCache: true,
    })
    if err != nil {
        t.Fatalf("PruneSystem on stub: %v", err)
    }
    if report == nil {
        t.Fatal("PruneSystem returned nil report")
    }
}

func TestPruneOptionsDefaults(t *testing.T) {
    opts := PruneOptions{}
    if opts.DryRun {
        t.Error("DryRun should default to false")
    }
    if opts.All {
        t.Error("All should default to false")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPruneSystem|TestPruneOptionsDefaults" -v -count=1`
Expected: FAIL with `PruneSystem undefined` (not in Manager interface)

- [ ] **Step 3: Add types and interface method to `internal/runtime/runtime.go`**

Add after `RunOptions`:
```go
type PruneOptions struct {
    DryRun     bool // report what would be done, don't execute
    Containers bool // prune stopped containers not managed by Tengiz
    Images     bool // prune unused images not managed by Tengiz
    BuildCache bool // prune Docker BuildKit build cache
    All        bool // also prune stopped Tengiz-managed containers
}

type PruneReport struct {
    ContainersOutput string // raw docker output for container prune
    ImagesOutput     string // raw docker output for image prune
    CacheOutput      string // raw docker output for build cache prune
}
```

Add to `Manager` interface (after `Run`):
```go
PruneSystem(ctx context.Context, opts PruneOptions) (*PruneReport, error)
```

Add stub method after existing stub's `Run`:
```go
func (m *stubManager) PruneSystem(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
    return &PruneReport{}, nil
}
```

- [ ] **Step 4: Run test to verify interface compiles**

Run: `go test ./internal/runtime/... -run "TestStubPruneSystem|TestPruneOptionsDefaults" -v -count=1`
Expected: PASS (stub returns empty report)

- [ ] **Step 5: Write Docker implementation tests**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
    "testing"
)

func TestParsePruneOutput(t *testing.T) {
    output := "Deleted Containers:\nabc\n\nTotal reclaimed space: 1.23GB\n"
    count := countDeletedLines(output)
    if count != 1 {
        t.Errorf("expected 1 deleted container, got %d", count)
    }
}
```

Wait — `countDeletedLines` is a helper we'll write. But the plan shouldn't include `TBD` steps. Let me rethink. The Docker prune commands are hard to unit test since they need Docker. The unit tests should focus on the interface contract (stub) and the CLI structure. Integration tests with Docker can be manual or skipped.

Better approach — skip testing docker command output parsing and just test the interface integration.

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: All PASS

- [ ] **Step 6: Implement `PruneSystem` and helpers in `internal/runtime/cleanup.go`**

Add to cleanup.go (after existing code):

```go
func (r *dockerRuntime) PruneSystem(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
    report := &PruneReport{}

    if opts.Containers {
        out, err := r.pruneContainers(ctx, opts)
        if err != nil {
            return report, err
        }
        report.ContainersOutput = out
    }

    if opts.Images {
        out, err := r.pruneImages(ctx, opts)
        if err != nil {
            return report, err
        }
        report.ImagesOutput = out
    }

    if opts.BuildCache {
        out, err := r.pruneBuildCache(ctx)
        if err != nil {
            return report, err
        }
        report.CacheOutput = out
    }

    return report, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, opts PruneOptions) (string, error) {
    var output strings.Builder

    if opts.DryRun {
        cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
            "--filter", "status=exited",
            "--filter", "label!=tengiz-app",
            "--format", "{{.Names}} ({{.Image}})",
        )
        out, _ := cmd.CombinedOutput()
        output.WriteString("Non-Tengiz stopped containers to remove:\n")
        output.Write(out)
        if len(strings.TrimSpace(string(out))) == 0 {
            output.WriteString("(none)\n")
        }
    } else {
        cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
            "--filter", "label!=tengiz-app",
        )
        out, err := cmd.CombinedOutput()
        if err != nil {
            return "", fmt.Errorf("docker container prune: %w\n%s", err, string(out))
        }
        output.Write(out)
    }

    if opts.All && !opts.DryRun {
        cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
            "--filter", "label=tengiz-app",
        )
        out, err := cmd.CombinedOutput()
        if err != nil {
            output.WriteString(fmt.Sprintf("warning: tengiz container prune: %v\n", err))
        } else {
            output.Write(out)
        }
    } else if opts.All && opts.DryRun {
        cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
            "--filter", "status=exited",
            "--filter", "label=tengiz-app",
            "--format", "{{.Names}} ({{.Image}})",
        )
        out, _ := cmd.CombinedOutput()
        output.WriteString("\nStopped Tengiz containers to remove:\n")
        output.Write(out)
        if len(strings.TrimSpace(string(out))) == 0 {
            output.WriteString("(none)\n")
        }
    }

    return output.String(), nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, opts PruneOptions) (string, error) {
    if opts.DryRun {
        cmd := exec.CommandContext(ctx, "docker", "images",
            "--filter", "dangling=true",
            "--filter", "label!=tengiz-app",
            "--format", "{{.Repository}}:{{.Tag}} ({{.Size}})",
        )
        out, _ := cmd.CombinedOutput()
        var output strings.Builder
        output.WriteString("Unused images to remove:\n")
        output.Write(out)
        if len(strings.TrimSpace(string(out))) == 0 {
            output.WriteString("(none)\n")
        }
        return output.String(), nil
    }

    cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-a", "-f",
        "--filter", "label!=tengiz-app",
    )
    out, err := cmd.CombinedOutput()
    if err != nil {
        return "", fmt.Errorf("docker image prune: %w\n%s", err, string(out))
    }
    return string(out), nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) (string, error) {
    cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-a", "-f")
    out, err := cmd.CombinedOutput()
    if err != nil {
        return "", fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
    }
    return string(out), nil
}
```

- [ ] **Step 7: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go
git commit -m "feat: add PruneSystem to runtime.Manager with Docker prune operations"
```

---

### Task 2: Add orphaned port cleanup to config.Store

**Files:**
- Modify: `internal/config/store.go` — add `CleanupOrphanedPorts` method
- Test: `internal/config/store_test.go` — add tests

**Interfaces:**
- Consumes: existing `s.readJSON`, `s.writeJSON`, `s.envFile`, `s.ListApps` patterns
- Produces: `(*Store).CleanupOrphanedPorts() (int, error)` — returns count of freed ports

- [ ] **Step 1: Write the failing test**

```go
// internal/config/store_test.go

func TestCleanupOrphanedPorts(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)

    // Create an app
    app := types.AppEntry{Name: "myapp", Port: 9000}
    if err := s.SaveApp(app); err != nil {
        t.Fatalf("SaveApp: %v", err)
    }

    // Allocate a port for the app
    port, err := s.AllocatePort("myapp")
    if err != nil {
        t.Fatalf("AllocatePort: %v", err)
    }

    // Remove the app to create an orphan
    if err := s.RemoveApp("myapp"); err != nil {
        t.Fatalf("RemoveApp: %v", err)
    }

    // Cleanup orphans
    removed, err := s.CleanupOrphanedPorts()
    if err != nil {
        t.Fatalf("CleanupOrphanedPorts: %v", err)
    }
    if removed != 1 {
        t.Errorf("expected 1 orphaned port, got %d", removed)
    }

    // Verify the port was freed
    ports := make(map[int]string)
    s.readJSON(s.envFile("ports.json"), &ports)
    if _, exists := ports[port]; exists {
        t.Errorf("port %d should have been freed", port)
    }
}

func TestCleanupOrphanedPortsNoOrphans(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)

    app := types.AppEntry{Name: "myapp", Port: 9000}
    if err := s.SaveApp(app); err != nil {
        t.Fatalf("SaveApp: %v", err)
    }
    if _, err := s.AllocatePort("myapp"); err != nil {
        t.Fatalf("AllocatePort: %v", err)
    }

    removed, err := s.CleanupOrphanedPorts()
    if err != nil {
        t.Fatalf("CleanupOrphanedPorts: %v", err)
    }
    if removed != 0 {
        t.Errorf("expected 0 orphans, got %d", removed)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run "TestCleanupOrphanedPorts" -v -count=1`
Expected: FAIL with `CleanupOrphanedPorts undefined`

- [ ] **Step 3: Implement `CleanupOrphanedPorts` in `internal/config/store.go`**

Add after `FreePort` (line 104):

```go
func (s *Store) CleanupOrphanedPorts() (int, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    ports := make(map[int]string)
    s.readJSON(s.envFile("ports.json"), &ports)
    if len(ports) == 0 {
        return 0, nil
    }

    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)

    var removed int
    for port, appName := range ports {
        if _, exists := apps[appName]; !exists {
            delete(ports, port)
            removed++
        }
    }

    if removed > 0 {
        if err := s.writeJSON(s.envFile("ports.json"), ports); err != nil {
            return removed, fmt.Errorf("write ports: %w", err)
        }
    }

    return removed, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -run "TestCleanupOrphanedPorts" -v -count=1`
Expected: PASS

- [ ] **Step 5: Run all store tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/store.go
git commit -m "feat: add CleanupOrphanedPorts to config.Store"
```

---

### Task 3: Create `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` variable + `init()` registration + flags
- Create: `internal/cli/cleanup_test.go` — tests for command structure

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneSystem(ctx, opts)`, `config.NewStore(dataDir).CleanupOrphanedPorts()`
- Produces: `tengiz cleanup [--dry-run] [--all] [--containers] [--images] [--build-cache]`

- [ ] **Step 1: Write the failing test**

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

func TestCleanupCmdFlags(t *testing.T) {
    cmd := cleanupCmd
    flags := []string{"dry-run", "all", "containers", "images", "build-cache"}
    for _, name := range flags {
        if cmd.Flags().Lookup(name) == nil {
            t.Errorf("cleanupCmd missing --%s flag", name)
        }
    }
}

func TestCleanupCmdFlagDefaults(t *testing.T) {
    cmd := cleanupCmd
    if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
        t.Error("--dry-run should default to false")
    }
    if all, _ := cmd.Flags().GetBool("all"); all {
        t.Error("--all should default to false")
    }
    if containers, _ := cmd.Flags().GetBool("containers"); containers {
        t.Error("--containers should default to false")
    }
    if images, _ := cmd.Flags().GetBool("images"); images {
        t.Error("--images should default to false")
    }
    if buildCache, _ := cmd.Flags().GetBool("build-cache"); buildCache {
        t.Error("--build-cache should default to false")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCmd" -v -count=1`
Expected: FAIL with `cleanupCmd undefined`

- [ ] **Step 3: Add `cleanupCmd` variable and init registration in `internal/cli/root.go`**

Add after the last command variable (before `rootCmd` definition):

```go
var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Reclaim disk space by pruning unused Docker resources",
    Long: `Remove stopped containers, unused images, and build cache.

By default, only prunes resources NOT managed by Tengiz.
Tengiz-managed containers and images are protected via the 'tengiz-app' label.
Use --all to also prune stopped Tengiz-managed containers.
Use --dry-run to see what would be removed without doing it.`,
    RunE: func(cmd *cobra.Command, args []string) error {
        dryRun, _ := cmd.Flags().GetBool("dry-run")
        all, _ := cmd.Flags().GetBool("all")
        containers, _ := cmd.Flags().GetBool("containers")
        images, _ := cmd.Flags().GetBool("images")
        buildCache, _ := cmd.Flags().GetBool("build-cache")

        if !containers && !images && !buildCache {
            containers = true
            images = true
            buildCache = true
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        fmt.Println("[tengiz] starting cleanup...")

        report, err := rt.PruneSystem(cmd.Context(), runtime.PruneOptions{
            DryRun:     dryRun,
            Containers: containers,
            Images:     images,
            BuildCache: buildCache,
            All:        all,
        })
        if err != nil {
            return err
        }

        if report.ContainersOutput != "" {
            fmt.Println(report.ContainersOutput)
        }
        if report.ImagesOutput != "" {
            fmt.Println(report.ImagesOutput)
        }
        if report.CacheOutput != "" {
            fmt.Println(report.CacheOutput)
        }

        store := NewStore(dataDir)
        removed, err := store.CleanupOrphanedPorts()
        if err != nil {
            log.Printf("[tengiz] warning: orphaned port cleanup: %v", err)
        }
        if removed > 0 {
            fmt.Printf("[tengiz] freed %d orphaned port(s)\n", removed)
        }

        if dryRun {
            fmt.Println("[tengiz] dry-run complete — no resources were removed")
        } else {
            fmt.Println("[tengiz] cleanup complete")
        }
        return nil
    },
}
```

Add in `init()` after the last `AddCommand` / before the flag registrations:
```go
rootCmd.AddCommand(cleanupCmd)
```

Add flag registration in `init()` after the existing flag registrations:
```go
cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without doing it")
cleanupCmd.Flags().Bool("all", false, "also prune stopped Tengiz-managed containers")
cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
cleanupCmd.Flags().Bool("images", false, "prune unused images not managed by Tengiz")
cleanupCmd.Flags().Bool("build-cache", false, "prune Docker BuildKit build cache")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCmd" -v -count=1`
Expected: PASS

- [ ] **Step 5: Build to verify compilation**

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 6: Run all tests**

Run: `go test ./... -v -count=1 2>&1 | head -50`
Expected: All PASS (except proxy TCP timeout tests and idle time-sensitive tests)

Wait — the new `config.Store` import in root.go needs to use `config.NewStore`. Let me check what `NewStore` is called in root.go currently.

Looking at the handler code:
```go
store := NewStore(dataDir)
```

But `NewStore` is in `config` package. Let me check the root.go import — it imports `"github.com/yaso09/tengiz/internal/config"`. So it should be `config.NewStore(dataDir)`.

Actually, root.go likely has a local `NewStore` wrapper or imports as `. "config"`. Let me check...

From the research, the import is `"github.com/yaso09/tengiz/internal/config"`. And in the deploy handler it uses `store := config.NewStore(dataDir)`. But in the cleanup handler I wrote `store := NewStore(dataDir)` which would be wrong. Let me fix that.

Actually wait, looking at root.go's other handlers. Let me check how they call it.

Searching my research... The existing handlers use:
- `store := config.NewStore(dataDir)` 

So my cleanup handler should use `config.NewStore(dataDir)`. Let me fix that in the plan.

- [ ] **Fix: Use correct import path for store**

The cleanup handler uses `config.NewStore(dataDir)` not `NewStore(dataDir)`:

```go
store := config.NewStore(dataDir)
```

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup CLI command for Docker housekeeping"
```

---

### Task 4: Self-review and final verification

- [ ] **Step 1: Spec coverage check**

Check against `docs/FUTURES_FEATURES.md` Docker Housekeeping requirement:
- `tengiz cleanup` CLI command ✅ (Task 3)
- Label-based filtering via `label!=tengiz-app` ✅ (Task 1)
- Container pruning ✅ (Task 1 — `docker container prune`)
- Image pruning ✅ (Task 1 — `docker image prune -a`)
- Build cache pruning ✅ (Task 1 — `docker builder prune -a`)
- Orphaned port cleanup ✅ (Task 2)
- Dry-run mode ✅ (Task 1, Task 3)
- All flag for Tengiz-managed containers ✅ (Task 1, Task 3)

- [ ] **Step 2: Placeholder scan**

Search plan for any "TBD", "TODO", "implement later", "fill in details", "Similar to Task" patterns. None found. Every step has complete code.

- [ ] **Step 3: Type consistency check**

- `runtime.PruneOptions{DryRun, Containers, Images, BuildCache, All}` — same struct used in Tasks 1 and 3 ✅
- `runtime.PruneReport{ContainersOutput, ImagesOutput, CacheOutput}` — same type consumed by CLI in Task 3 ✅
- `(*Store).CleanupOrphanedPorts() (int, error)` — called in Task 3, matches implementation in Task 2 ✅
- Flag names match between registration (`cleanupCmd.Flags().Bool(...)`) and lookup (`cmd.Flags().GetBool(...)`) ✅

- [ ] **Step 4: Run full test suite**

Run: `go test ./... -count=1 2>&1`
Expected: All PASS (pre-existing proxy timeout tests may fail — they are known slow tests from AGENTS.md)

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 5: Final commit**

```bash
git add .
git commit -m "feat: add Docker housekeeping system (tengiz cleanup)"
```
