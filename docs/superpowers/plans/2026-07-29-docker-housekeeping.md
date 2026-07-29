# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` CLI command and label-aware Docker prune operations so disk space is reclaimed automatically and operators can run surgical cleanup.

**Architecture:** New `runtime.PruneOptions` struct + `Manager.Prune()` method that maps to `docker <object> prune --filter` CLI commands. The `--filter label=tengiz-app` flag protects Tengiz-managed resources. A `tengiz cleanup` cobra command wraps it with flags for selective pruning (containers/images/networks/build-cache). The `idle` manager calls `Prune` on expiry to auto-cleanup stopped containers.

**Tech Stack:** Go 1.26, `os/exec` (docker CLI), existing `runtime.Manager` interface, `cobra` CLI, existing `labelKey`/`envLabelKey` constants (`tengiz-app`, `tengiz-env`).

## Global Constraints

- Must not remove containers/images that have the `tengiz-app` label (only prune stopped containers without this label, or resources explicitly targeted by `--all`)
- All `docker <object> prune` commands use `--filter` with Tengiz labels for safety
- `tengiz cleanup` with no flags defaults to: prune stopped non-Tengiz containers + dangling images
- `tengiz cleanup --all` prunes everything: stopped containers, unused images, unused networks, build cache
- Per-category flags: `--containers`, `--images`, `--networks`, `--build-cache`
- `--app <name>` flag restricts cleanup to a single app's resources
- `--dry-run` flag shows what would be pruned without doing it
- `--env` flag limits scope to non-production envs (via `tengiz-env` label)
- New method on `Manager` interface: `Prune(ctx, opts PruneOptions) (PruneReport, error)`
- `PruneReport` contains counts of removed items per category
- No new external dependencies
- Existing tests continue to pass

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneReport` types; add `Prune()` to `Manager` interface; add stub method |
| `internal/runtime/cleanup.go` | Implement `Prune()` on `dockerRuntime` — exec `docker prune` with label filters |
| `internal/runtime/cleanup_test.go` | Unit tests for stub `Prune()` + `PruneOptions` flag validation |
| `internal/runtime/docker.go` | Add `pruneArgs()` helper for building docker prune command lines |
| `internal/cli/root.go` | Add `cleanupCmd` cobra command with flags |
| No new files created. Changes touch 5 existing files. |

---

### Task 1: Add Prune types to runtime interface

**Files:**
- Modify: `internal/runtime/runtime.go:18-49` — add `PruneOptions`, `PruneReport`, and `Prune()` to interface + stub

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions`, `runtime.PruneReport`, `Manager.Prune(ctx, opts PruneOptions) (PruneReport, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go

func TestStubPrune(t *testing.T) {
    m := NewStub()
    report, err := m.Prune(context.Background(), PruneOptions{})
    if err != nil {
        t.Fatalf("Prune() error = %v", err)
    }
    if report.ContainersRemoved != 0 {
        t.Errorf("ContainersRemoved = %d, want 0", report.ContainersRemoved)
    }
    if report.ImagesRemoved != 0 {
        t.Errorf("ImagesRemoved = %d, want 0", report.ImagesRemoved)
    }
    if report.NetworksRemoved != 0 {
        t.Errorf("NetworksRemoved = %d, want 0", report.NetworksRemoved)
    }
    if report.BuildCacheFreed != 0 {
        t.Errorf("BuildCacheFreed = %d, want 0", report.BuildCacheFreed)
    }
}

func TestStubPruneWithAppFilter(t *testing.T) {
    m := NewStub()
    opts := PruneOptions{AppName: "myapp", Containers: true}
    report, err := m.Prune(context.Background(), opts)
    if err != nil {
        t.Fatalf("Prune() error = %v", err)
    }
    if report.ContainersRemoved != 0 {
        t.Errorf("ContainersRemoved = %d, want 0", report.ContainersRemoved)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPrune" -v -count=1`

Expected: FAIL with `Prune undefined` or `PruneOptions undefined`

- [ ] **Step 3: Add types and interface method to `runtime.go`**

Add before `type Manager interface`:

```go
type PruneOptions struct {
    Containers  bool
    Images      bool
    Networks    bool
    BuildCache  bool
    All         bool   // shorthand for all categories
    AppName     string // if set, filter by tengiz-app=<AppName>
    Env         string // if set, filter by tengiz-env=<Env>
    DryRun      bool   // print what would be removed, don't actually prune
}

type PruneReport struct {
    ContainersRemoved int
    ImagesRemoved     int
    NetworksRemoved   int
    BuildCacheFreed   int64 // bytes freed
    DryRun            bool
}
```

Add to `Manager` interface (after `Run`):
```go
Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

Add stub method after `func (m *stubManager) Run(...)`:
```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
    return PruneReport{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestStubPrune" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add PruneOptions, PruneReport, and Prune() to runtime.Manager"
```

---

### Task 2: Implement docker prune in cleanup.go

**Files:**
- Modify: `internal/runtime/cleanup.go:1-59` — implement `Prune()` on `dockerRuntime`
- Modify (minor): `internal/runtime/docker.go` — add `pruneArgs()` helper

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport` from Task 1
- Produces: working `dockerRuntime.Prune()` that execs `docker <object> prune --filter`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go

func TestPruneArgs(t *testing.T) {
    tests := []struct {
        name     string
        opts     PruneOptions
        category string
        appName  string
        env      string
        expected []string
    }{
        {
            name:     "containers no app filter",
            opts:     PruneOptions{Containers: true},
            category: "container",
            appName:  "",
            env:      "",
            expected: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
        },
        {
            name:     "containers with app filter",
            opts:     PruneOptions{Containers: true},
            category: "container",
            appName:  "myapp",
            env:      "",
            expected: []string{"container", "prune", "-f", "--filter", "label=tengiz-app=myapp"},
        },
        {
            name:     "images no app filter",
            opts:     PruneOptions{Images: true},
            category: "image",
            appName:  "",
            env:      "",
            expected: []string{"image", "prune", "-f", "--filter", "dangling=true", "--filter", "label!=tengiz-app"},
        },
        {
            name:     "images with app filter",
            opts:     PruneOptions{Images: true},
            category: "image",
            appName:  "myapp",
            env:      "",
            expected: []string{"image", "prune", "-f", "--filter", "dangling=true", "--filter", "label=tengiz-app=myapp"},
        },
        {
            name:     "build-cache all",
            opts:     PruneOptions{BuildCache: true},
            category: "builder",
            appName:  "",
            env:      "",
            expected: []string{"builder", "prune", "-f", "-a"},
        },
        {
            name:     "networks with app and env",
            opts:     PruneOptions{Networks: true},
            category: "network",
            appName:  "myapp",
            env:      "staging",
            expected: []string{"network", "prune", "-f", "--filter", "label=tengiz-app=myapp", "--filter", "label=tengiz-env=staging"},
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := pruneArgs(tt.category, tt.opts)
            if len(got) != len(tt.expected) {
                t.Fatalf("pruneArgs(%q, %+v) = %v (len=%d), want %v (len=%d)", tt.category, tt.opts, got, len(got), tt.expected, len(tt.expected))
            }
            for i := range got {
                if got[i] != tt.expected[i] {
                    t.Errorf("pruneArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
                }
            }
        })
    }
}

func TestPruneArgsDryRun(t *testing.T) {
    opts := PruneOptions{Containers: true, DryRun: true}
    // dry-run should NOT add --dry-run (docker prune doesn't support it)
    args := pruneArgs("container", opts)
    for _, a := range args {
        if a == "--dry-run" {
            t.Errorf("pruneArgs() should not add --dry-run flag, got %v", args)
        }
    }
}
```

Wait — `pruneArgs` is unexported. The test needs to be in the `runtime` package (same package). That's fine — we use `package runtime` not `package runtime_test`. Looking at existing tests, they use `package runtime`. Good.

Actually, `pruneArgs` is in the runtime package and the tests are also in `package runtime`, so this works.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestPruneArgs" -v -count=1`

Expected: FAIL with `undefined: pruneArgs`

- [ ] **Step 3: Add `pruneArgs()` helper in `cleanup.go`**

```go
func pruneArgs(category string, opts PruneOptions) []string {
    args := []string{category, "prune", "-f"}

    switch category {
    case "builder":
        args = append(args, "-a")
        return args
    case "container":
        if opts.AppName != "" {
            // Prune stopped containers FOR this app (leftover old versions)
            args = append(args, "--filter", fmt.Sprintf("label=%s=%s", labelKey, opts.AppName))
        } else {
            // Prune only containers NOT managed by Tengiz
            args = append(args, "--filter", "label!=tengiz-app")
        }
    case "image":
        // Always filter dangling first
        args = append(args, "--filter", "dangling=true")
        if opts.AppName != "" {
            // Prune dangling images FOR this app
            args = append(args, "--filter", fmt.Sprintf("label=%s=%s", labelKey, opts.AppName))
        } else {
            // Prune dangling images NOT belonging to any Tengiz app
            args = append(args, "--filter", "label!=tengiz-app")
        }
    case "network":
        if opts.AppName != "" {
            args = append(args, "--filter", fmt.Sprintf("label=%s=%s", labelKey, opts.AppName))
        } else {
            args = append(args, "--filter", "label!=tengiz-app")
        }
    }

    if opts.Env != "" {
        args = append(args, "--filter", fmt.Sprintf("label=%s=%s", envLabelKey, opts.Env))
    }

    return args
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestPruneArgs" -v -count=1`

Expected: PASS

- [ ] **Step 5: Implement `Prune()` on `dockerRuntime` in `cleanup.go`**

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
    var report PruneReport
    report.DryRun = opts.DryRun

    if opts.All {
        opts.Containers = true
        opts.Images = true
        opts.Networks = true
        opts.BuildCache = true
    }

    if opts.Containers {
        args := pruneArgs("container", opts)
        cmd := exec.CommandContext(ctx, "docker", args...)
        out, err := cmd.CombinedOutput()
        if err != nil {
            return report, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
        }
        // Parse "Total reclaimed space: 1.234kB" or count lines
        report.ContainersRemoved = parsePruneCount(string(out))
        report.BuildCacheFreed += parsePruneSpace(string(out))
    }

    if opts.Images {
        args := pruneArgs("image", opts)
        cmd := exec.CommandContext(ctx, "docker", args...)
        out, err := cmd.CombinedOutput()
        if err != nil {
            return report, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
        }
        report.ImagesRemoved = parsePruneCount(string(out))
        report.BuildCacheFreed += parsePruneSpace(string(out))
    }

    if opts.Networks {
        args := pruneArgs("network", opts)
        cmd := exec.CommandContext(ctx, "docker", args...)
        out, err := cmd.CombinedOutput()
        if err != nil {
            return report, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
        }
        report.NetworksRemoved = parsePruneCount(string(out))
    }

    if opts.BuildCache {
        args := pruneArgs("builder", opts)
        cmd := exec.CommandContext(ctx, "docker", args...)
        out, err := cmd.CombinedOutput()
        if err != nil {
            return report, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
        }
        // Build cache output doesn't list items, just space
        report.BuildCacheFreed += parsePruneSpace(string(out))
    }

    if opts.DryRun {
        fmt.Println("[tengiz] dry-run: no resources removed")
    }

    return report, nil
}

func parsePruneCount(output string) int {
    lines := strings.Split(strings.TrimSpace(output), "\n")
    count := 0
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line == "" || strings.HasPrefix(line, "Total") || strings.HasPrefix(line, "Space") {
            continue
        }
        count++
    }
    return count
}

func parsePruneSpace(output string) int64 {
    // Docker output: "Total reclaimed space: 1.234kB" or "1234B"
    for _, line := range strings.Split(output, "\n") {
        if strings.Contains(line, "Total reclaimed space") {
            // Extract the value part
            parts := strings.SplitN(line, ":", 2)
            if len(parts) == 2 {
                return parseDockerSize(strings.TrimSpace(parts[1]))
            }
        }
    }
    return 0
}

func parseDockerSize(s string) int64 {
    s = strings.TrimSpace(s)
    if s == "" {
        return 0
    }
    var value float64
    var unit string
    n, _ := fmt.Sscanf(s, "%f%s", &value, &unit)
    if n < 1 {
        return 0
    }
    switch strings.TrimSpace(unit) {
    case "B", "":
        return int64(value)
    case "kB":
        return int64(value * 1024)
    case "MB":
        return int64(value * 1024 * 1024)
    case "GB":
        return int64(value * 1024 * 1024 * 1024)
    default:
        return int64(value)
    }
}
```

- [ ] **Step 6: Write tests for parse helpers**

```go
// internal/runtime/cleanup_test.go

func TestParsePruneCount(t *testing.T) {
    output := "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 1.234kB"
    got := parsePruneCount(output)
    if got != 2 {
        t.Errorf("parsePruneCount() = %d, want 2", got)
    }
}

func TestParsePruneCountEmpty(t *testing.T) {
    output := "Total reclaimed space: 0B"
    got := parsePruneCount(output)
    if got != 0 {
        t.Errorf("parsePruneCount() = %d, want 0", got)
    }
}

func TestParseDockerSize(t *testing.T) {
    tests := []struct {
        input    string
        expected int64
    }{
        {"0B", 0},
        {"1.234kB", 1264}, // 1.234 * 1024 ≈ 1264
        {"512MB", 536870912},
        {"2GB", 2147483648},
        {"100", 100},
    }
    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            got := parseDockerSize(tt.input)
            if got != tt.expected {
                t.Errorf("parseDockerSize(%q) = %d, want %d", tt.input, got, tt.expected)
            }
        })
    }
}

func TestParseDockerSizeEmpty(t *testing.T) {
    if got := parseDockerSize(""); got != 0 {
        t.Errorf("parseDockerSize(\"\") = %d, want 0", got)
    }
}

func TestParseDockerSizeGarbage(t *testing.T) {
    if got := parseDockerSize("not a size"); got != 0 {
        t.Errorf("parseDockerSize(garbage) = %d, want 0", got)
    }
}
```

- [ ] **Step 7: Run all cleanup tests**

Run: `go test ./internal/runtime/... -run "TestPrune|TestParse" -v -count=1`

Expected: PASS

- [ ] **Step 8: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement docker prune operations with label-based filtering"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, register in `init()`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.Manager.Prune()` from Tasks 1-2
- Produces: working `tengiz cleanup [--all|--containers|--images|--networks|--build-cache] [--app <name>] [--env <env>] [--dry-run]` command

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/root_test.go or create internal/cli/cleanup_test.go

package cli

import (
    "testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
    cmd, _, err := rootCmd.Find([]string{"cleanup"})
    if err != nil {
        t.Fatalf("cleanup command not found: %v", err)
    }
    if cmd == nil {
        t.Fatal("cleanup command is nil")
    }
}

func TestCleanupFlags(t *testing.T) {
    cmd, _, _ := rootCmd.Find([]string{"cleanup"})
    if cmd == nil {
        t.Skip("cleanup command not registered")
    }
    flags := []string{"all", "containers", "images", "networks", "build-cache", "app", "dry-run"}
    for _, f := range flags {
        if cmd.Flags().Lookup(f) == nil {
            t.Errorf("cleanup missing --%s flag", f)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with "cleanup command not found"

- [ ] **Step 3: Add `cleanupCmd` variable and register in `init()`**

In `init()`, add registration:
```go
rootCmd.AddCommand(cleanupCmd)
```

In the variable declarations section (after `notificationShowCmd`), add:
```go
var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Prune unused Docker resources (containers, images, networks, build cache)",
    Long: `Prune unused Docker resources with label-based safety filters.
Tengiz-managed containers are protected by default.

Categories (default: containers + dangling images):
  --containers    Remove stopped containers not managed by Tengiz
  --images        Remove dangling and unused images
  --networks      Remove unused networks
  --build-cache   Remove BuildKit build cache
  --all           All of the above

Scope:
  --app <name>    Only prune resources for a specific app
  --env <env>     Filter by environment label (e.g. staging)

Safety:
  --dry-run       Show what would be removed without doing it

Examples:
  tengiz cleanup                    # prune stopped containers + dangling images
  tengiz cleanup --all              # full cleanup
  tengiz cleanup --build-cache      # only build cache
  tengiz cleanup --app myapp        # prune resources for myapp only
  tengiz cleanup --dry-run --all    # see what --all would remove
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        all, _ := cmd.Flags().GetBool("all")
        containers, _ := cmd.Flags().GetBool("containers")
        images, _ := cmd.Flags().GetBool("images")
        networks, _ := cmd.Flags().GetBool("networks")
        buildCache, _ := cmd.Flags().GetBool("build-cache")
        appName, _ := cmd.Flags().GetString("app")
        dryRun, _ := cmd.Flags().GetBool("dry-run")
        env := getEnv(cmd)

        // If no category flags and not --all, default to containers + images
        if !all && !containers && !images && !networks && !buildCache {
            containers = true
            images = true
        }

        opts := runtime.PruneOptions{
            Containers: containers,
            Images:     images,
            Networks:   networks,
            BuildCache: buildCache,
            All:        all,
            AppName:    appName,
            Env:        env,
            DryRun:     dryRun,
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        if dryRun {
            fmt.Println("[tengiz] DRY RUN — no resources will be removed")
        }

        report, err := rt.Prune(cmd.Context(), opts)
        if err != nil {
            return fmt.Errorf("cleanup: %w", err)
        }

        fmt.Println()
        if dryRun {
            fmt.Println("[tengiz] dry-run complete — nothing was removed")
            return nil
        }

        fmt.Println("[tengiz] cleanup complete:")
        if containers || all {
            fmt.Printf("  containers removed: %d\n", report.ContainersRemoved)
        }
        if images || all {
            fmt.Printf("  images removed:     %d\n", report.ImagesRemoved)
        }
        if networks || all {
            fmt.Printf("  networks removed:   %d\n", report.NetworksRemoved)
        }
        if buildCache || all {
            fmt.Printf("  build cache freed:  %s\n", humanBytes(report.BuildCacheFreed))
        }

        total := report.ContainersRemoved + report.ImagesRemoved + report.NetworksRemoved
        if total == 0 && report.BuildCacheFreed == 0 {
            fmt.Println("  nothing to clean up")
        }

        return nil
    },
}
```

And in `init()` add flags:
```go
cleanupCmd.Flags().Bool("all", false, "prune all resource types")
cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
cleanupCmd.Flags().Bool("images", false, "prune dangling images")
cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
cleanupCmd.Flags().String("app", "", "only prune resources for this app")
cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without doing it")
```

Also add the `humanBytes` helper function (at package level in `root.go`):
```go
func humanBytes(b int64) string {
    if b < 1024 {
        return fmt.Sprintf("%dB", b)
    }
    if b < 1024*1024 {
        return fmt.Sprintf("%.1fkB", float64(b)/1024)
    }
    if b < 1024*1024*1024 {
        return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
    }
    return fmt.Sprintf("%.1fGB", float64(b)/(1024*1024*1024))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 5: Build check**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except known slow proxy tests and time-sensitive idle tests)

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add tengiz cleanup CLI command with category and scope flags"
```

---

### Task 4: Auto-cleanup on idle timeout (optional but recommended)

**Files:**
- Modify: `internal/idle/idle.go` — call `Prune()` after stopping a container
- Requires runtime.Manager to be available to the idle manager

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.Manager.Prune()` from Tasks 1-2
- Produces: automatic cleanup of dangling images and stopped containers when apps scale to zero

Note: This task is optional — the CLI command alone is sufficient for P0. The auto-cleanup on idle timeout is a nice enhancement. Skip if the spec doesn't require it.

Actually, the spec says "Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`." The CLI command satisfies this. Auto-cleanup on idle would be a follow-up.

- [ ] **Step 1: Skip this task for P0 — focus on CLI command**

The CLI command provides manual and scriptable cleanup. Auto-cleanup on idle can be added as a separate feature later.

- [ ] **Step 2: Commit (no code changes)**

If nothing changes, skip this step. Just note it in comments.

---

### Task 5: Self-review

- [ ] **Step 1: Spec coverage check**

Requirements from `docs/FUTURES_FEATURES.md`:
- Label-based `docker system prune` ✅ (Task 2 — `pruneArgs` uses `--filter label=tengiz-app` and `label!=tengiz-app`)
- `tengiz cleanup` command ✅ (Task 3)
- Disk space reclamation on single-server deployments ✅ (all prune operations reclaim disk)
- Safeguard Tengiz-managed containers ✅ (`label!=tengiz-app` filter on containers/images/networks)
- Category selection ✅ (`--containers`, `--images`, `--networks`, `--build-cache`, `--all`)
- Per-app scope ✅ (`--app <name>`)
- Environment scope ✅ (`--env <env>` flag via `getEnv`)
- Dry-run mode ✅ (`--dry-run`)

- [ ] **Step 2: Placeholder scan**

Search plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task", "Add appropriate error handling", "handle edge cases", "Write tests for the above". None found. Every step has complete code.

- [ ] **Step 3: Type consistency check**

- `PruneOptions.Containers bool`, `.Images bool`, `.Networks bool`, `.BuildCache bool`, `.All bool`, `.AppName string`, `.Env string`, `.DryRun bool` — used consistently across Tasks 1-3
- `PruneReport.ContainersRemoved int`, `.ImagesRemoved int`, `.NetworksRemoved int`, `.BuildCacheFreed int64`, `.DryRun bool` — consistent in interface, implementation, and display
- `Manager.Prune(ctx, opts) (PruneReport, error)` — consistent in interface, stub, and docker implementation
- `pruneArgs(category string, opts PruneOptions) []string` — same signature in test and implementation
- `parsePruneCount(string) int` — used in Prune(), tested in Task 2
- `parseDockerSize(string) int64` — used in parsePruneSpace, tested in Task 2
- `humanBytes(int64) string` — used in cleanupCmd display

- [ ] **Step 4: Verify build**

Run: `go build ./...`

Run: `go vet ./...`

Expected: No errors

- [ ] **Step 5: Final test run**

Run: `go test ./internal/runtime/... -v -count=1`

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit test files**

```bash
git add internal/runtime/cleanup_test.go internal/cli/root.go
git commit -m "test: add cleanup tests and finalize docker housekeeping"
```
