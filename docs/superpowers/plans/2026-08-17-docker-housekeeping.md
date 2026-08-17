# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add label-based Docker resource cleanup so users can reclaim disk space with `tengiz cleanup`, without ever removing Tengiz-managed containers or images. Disk space is the #1 production issue on single-server deployments.

**Architecture:** A new `Cleanup(ctx, opts)` method on `runtime.Manager` runs `docker system prune` with a `--filter "label!=tengiz-app"` guard so Tengiz-managed containers, images, and networks (all labeled `tengiz-app=<app>`) are never pruned. Options control `--all` (all unused images, not just dangling) and `--volumes` (opt-in, data-loss risk). `--dry-run` prints the exact commands and reports current disk usage via `docker system df` without deleting anything. Images built by Tengiz gain the `tengiz-app` label at build time (Docker + Nixpacks backends) so they are protected by the same filter. A `tengiz cleanup` Cobra command exposes the feature.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (docker CLI passthrough — no SDK, matching existing runtime). No new external dependencies.

## Global Constraints

- Label filter constant `labelKey = "tengiz-app"` (already exists in `internal/runtime/docker.go:76`, unexported) is the single source of truth for protection
- `Cleanup` must protect ALL resources labeled `tengiz-app` — containers, images, and networks
- Volume pruning is destructive → only enabled via `--volumes` opt-in flag, matching `docker system prune` semantics
- `--dry-run` never executes destructive commands; it only prints commands + runs read-only `docker system df`
- Image tags stay `tengiz-apps/<app>:<env>-<deploymentID>`; `KeepLastNImages` rollback protection is unchanged
- No new dependencies; all new args/parse logic implemented as pure functions for unit testing without Docker
- The four mock `Manager` implementations (`stubManager`, `mockRTForDeploy`, idle `mockRuntime`, proxy `mockRuntime`) must all gain `Cleanup` or compilation fails
- Full `go test ./... -count=1` must pass; `go vet ./...` must be clean
- Update README CLI reference, AGENTS.md CLI list, and mark feature #6 implemented in `docs/FUTURES_FEATURES.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupResult` types; add `Cleanup` to `Manager` interface; `stubManager.Cleanup` |
| `internal/runtime/cleanup.go` | Pure helpers `cleanupArgs()`, `extractReclaimed()`; `dockerRuntime.Cleanup` exec implementation |
| `internal/runtime/cleanup_test.go` | Tests for arg builder, parser, stub Cleanup |
| `internal/builder/builder.go` | Add `--label tengiz-app=<app>` to Docker + Nixpacks build args via `appLabelArgs()` helper |
| `internal/builder/builder_test.go` | Test `appLabelArgs()` |
| `internal/cli/cleanup.go` | **NEW** — `cleanupCmd` Cobra command with `--dry-run`, `--all`, `--volumes` flags |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` |
| `internal/cli/root_test.go` | `mockRTForDeploy.Cleanup`; registration + flags tests |
| `internal/idle/idle_test.go` | `mockRuntime.Cleanup` |
| `internal/proxy/proxy_test.go` | `mockRuntime.Cleanup` |
| `README.md` | CLI Reference: add `tengiz cleanup` |
| `AGENTS.md` | CLI list: add `tengiz cleanup` |
| `docs/FUTURES_FEATURES.md` | Mark #6 as ✅ Implemented |

One new file created; 11 existing files modified.

---

### Task 1: Runtime types, interface method, stub, and pure helpers

**Files:**
- Modify: `internal/runtime/runtime.go` — add `CleanupOptions`/`CleanupResult` after `RunOptions` (line 29), add `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` to `Manager` interface (after `Run` at line 48), add `stubManager.Cleanup` (after `Run` at line 121)
- Modify: `internal/runtime/cleanup.go` — add pure helpers + `dockerRuntime.Cleanup`
- Modify: `internal/runtime/cleanup_test.go` — tests

**Interfaces:**
- Consumes: existing `labelKey` const (`internal/runtime/docker.go:76`)
- Produces: `runtime.CleanupOptions{DryRun, All, Volumes bool}`, `runtime.CleanupResult{DryRun bool, Commands []string, Reclaimed string}`, `cleanupArgs(opts) []string`, `extractReclaimed(out string) string`, `Manager.Cleanup`

- [ ] **Step 1: Write the failing tests** (TDD — run `go test ./internal/runtime/ -run 'TestCleanup' -count=1` and confirm they fail to compile)

```go
// internal/runtime/cleanup_test.go
func TestCleanupArgs(t *testing.T) {
    tests := []struct {
        name     string
        opts     CleanupOptions
        expected string
    }{
        {"default", CleanupOptions{}, "system prune -f --filter label!=tengiz-app"},
        {"all", CleanupOptions{All: true}, "system prune -af --filter label!=tengiz-app"},
        {"volumes", CleanupOptions{Volumes: true}, "system prune -f --volumes --filter label!=tengiz-app"},
        {"all+volumes", CleanupOptions{All: true, Volumes: true}, "system prune -af --volumes --filter label!=tengiz-app"},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            args := cleanupArgs(tc.opts)
            if strings.Join(args, " ") != tc.expected {
                t.Errorf("cleanupArgs(%+v) = %q, want %q", tc.opts, strings.Join(args, " "), tc.expected)
            }
        })
    }
}

func TestExtractReclaimed(t *testing.T) {
    sample := "Deleted Containers:\n1a2b3c4d5e6f\n\nTotal reclaimed space: 512MB\n"
    if got := extractReclaimed(sample); got != "512MB" {
        t.Errorf("extractReclaimed() = %q, want %q", got, "512MB")
    }
    if got := extractReclaimed("Total reclaimed space: 0B\n"); got != "0B" {
        t.Errorf("extractReclaimed() = %q, want %q", got, "0B")
    }
    if got := extractReclaimed(""); got != "" {
        t.Errorf("extractReclaimed(\"\") = %q, want empty", got)
    }
}

func TestStubCleanup(t *testing.T) {
    m := NewStub()
    res, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true})
    if err != nil {
        t.Fatalf("Cleanup() error = %v", err)
    }
    if !res.DryRun {
        t.Error("CleanupResult.DryRun = false, want true")
    }
}
```

- [ ] **Step 2: Implement `CleanupOptions`/`CleanupResult` types** in `internal/runtime/runtime.go`

```go
type CleanupOptions struct {
    DryRun  bool // print commands + docker system df without pruning
    All     bool // prune all unused images, not just dangling (-a)
    Volumes bool // also prune unused volumes (destructive, opt-in)
}

type CleanupResult struct {
    DryRun    bool
    Commands  []string // commands that were (or would be) run
    Reclaimed string   // "Total reclaimed space: X" summary (empty when DryRun)
}
```

- [ ] **Step 3: Add `Cleanup` to `Manager` interface** in `internal/runtime/runtime.go`

```go
Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

- [ ] **Step 4: Implement pure helpers + `dockerRuntime.Cleanup`** in `internal/runtime/cleanup.go`

```go
func cleanupArgs(opts CleanupOptions) []string {
    args := []string{"system", "prune", "-f"}
    if opts.All {
        args = append(args, "-a")
    }
    if opts.Volumes {
        args = append(args, "--volumes")
    }
    args = append(args, "--filter", "label!="+labelKey)
    return args
}

func extractReclaimed(out string) string {
    const prefix = "Total reclaimed space:"
    for _, line := range strings.Split(out, "\n") {
        line = strings.TrimSpace(line)
        if strings.HasPrefix(line, prefix) {
            return strings.TrimSpace(strings.TrimPrefix(line, prefix))
        }
    }
    return ""
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
    args := cleanupArgs(opts)
    cmdLine := "docker " + strings.Join(args, " ")
    res := CleanupResult{DryRun: opts.DryRun, Commands: []string{cmdLine}}

    if opts.DryRun {
        df := exec.CommandContext(ctx, "docker", "system", "df")
        out, err := df.CombinedOutput()
        if err != nil {
            return res, fmt.Errorf("docker system df: %w\n%s", err, string(out))
        }
        res.Commands = append(res.Commands, "docker system df")
        res.Reclaimed = strings.TrimSpace(string(out))
        return res, nil
    }

    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return res, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
    }
    res.Reclaimed = extractReclaimed(string(out))
    return res, nil
}
```

- [ ] **Step 5: Add `stubManager.Cleanup`** in `internal/runtime/runtime.go` (after `Run`)

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
    return CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 6: Run tests** — `go test ./internal/runtime/ -run 'TestCleanup' -count=1` passes; then full `go test ./internal/runtime/ -count=1`

- [ ] **Step 7: Commit** — `git add -A && git commit -m "feat(runtime): add label-based Docker cleanup (Cleanup)"`

---

### Task 2: Builder image labels

**Files:**
- Modify: `internal/builder/builder.go` — add `appLabelArgs()` helper; inject into `buildWithDockerfile` (line 69-71) and `buildWithNixpacks` (line 139)
- Modify: `internal/builder/builder_test.go` — test the helper

**Interfaces:**
- Produces: `appLabelArgs(appName string) []string` returning `["--label", "tengiz-app=<appName>"]`

**Rationale:** `docker system prune --filter "label!=tengiz-app"` only protects images that carry the `tengiz-app` label. Today only containers are labeled (docker.go:98,125,160,456,516-517). Images must be labeled at build time so the prune filter never deletes rollback/current images.

- [ ] **Step 1: Write the failing test** in `internal/builder/builder_test.go`

```go
func TestAppLabelArgs(t *testing.T) {
    args := appLabelArgs("myapp")
    if len(args) != 2 || args[0] != "--label" || args[1] != "tengiz-app=myapp" {
        t.Errorf("appLabelArgs(myapp) = %v, want [--label tengiz-app=myapp]", args)
    }
}
```

- [ ] **Step 2: Implement `appLabelArgs`** in `internal/builder/builder.go`

```go
const appLabelKey = "tengiz-app"

func appLabelArgs(appName string) []string {
    return []string{"--label", fmt.Sprintf("%s=%s", appLabelKey, appName)}
}
```

- [ ] **Step 3: Inject into `buildWithDockerfile`** (builder.go:69-71) — append before `-t`:

```go
args := []string{"build"}
args = append(args, b.buildSecretArgs()...)
args = append(args, appLabelArgs(appName)...)
args = append(args, "-t", tag, dir)
```

- [ ] **Step 4: Inject into `buildWithNixpacks`** (builder.go:139):

```go
args := []string{"build", dir, "--name", tag}
args = append(args, appLabelArgs(appName)...)
```

> Nixpacks `build` CLI supports `--label`/`-l` (verified via nixpacks.com/docs/cli). Keep existing `--pkgs`/`--apt-pkgs`/`--cmd` appends after the label.

- [ ] **Step 5: Run tests** — `go test ./internal/builder/ -count=1`

- [ ] **Step 6: Commit** — `git commit -am "feat(builder): label images with tengiz-app for prune protection"`

---

### Task 3: CLI `tengiz cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` — `rootCmd.AddCommand(cleanupCmd)` in `init()` (near line 65)
- Modify: `internal/cli/root_test.go` — registration + flags tests; `mockRTForDeploy.Cleanup`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`
- Produces: `cleanupCmd` (registered), flags `--dry-run`, `--all`, `--volumes`

- [ ] **Step 1: Write the failing tests** in `internal/cli/root_test.go`

```go
func TestCleanupCmdRegistered(t *testing.T) {
    cmd, _, err := rootCmd.Find([]string{"cleanup"})
    if err != nil {
        t.Fatal("cleanup command not registered")
    }
    if cmd == nil || cmd.Name() != "cleanup" {
        t.Fatal("cleanup command not found")
    }
}

func TestCleanupCmdFlags(t *testing.T) {
    cmd, _, _ := rootCmd.Find([]string{"cleanup"})
    for _, flag := range []string{"dry-run", "all", "volumes"} {
        if cmd.Flags().Lookup(flag) == nil {
            t.Errorf("cleanupCmd missing --%s flag", flag)
        }
    }
}
```

- [ ] **Step 2: Implement `internal/cli/cleanup.go`**

```go
package cli

import (
    "fmt"
    "strings"

    "github.com/spf13/cobra"
    "github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Prune unused Docker resources (containers, images, networks, volumes)",
    Long: `Prune unused Docker resources while protecting all Tengiz-managed containers, images and networks.

By default removes stopped containers, dangling images and unused networks that are NOT
labeled with tengiz-app=<app>. Use flags for deeper cleanup:

  --dry-run   show the exact commands and current disk usage without deleting anything
  --all       also remove all unused images (not just dangling ones)
  --volumes   also remove unused volumes (DESTRUCTIVE - irreversible data loss)`,
    RunE: func(cmd *cobra.Command, args []string) error {
        dryRun, _ := cmd.Flags().GetBool("dry-run")
        all, _ := cmd.Flags().GetBool("all")
        volumes, _ := cmd.Flags().GetBool("volumes")

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("cleanup: %w", err)
        }

        res, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
            DryRun:  dryRun,
            All:     all,
            Volumes: volumes,
        })
        if err != nil {
            return fmt.Errorf("cleanup: %w", err)
        }

        if res.DryRun {
            fmt.Println("Dry run — no resources will be removed. Commands that would run:")
            for _, c := range res.Commands {
                fmt.Println("  $ " + c)
            }
            fmt.Println("\nCurrent disk usage:")
            fmt.Println(res.Reclaimed)
            return nil
        }

        for _, c := range res.Commands {
            fmt.Println("$ " + c)
        }
        fmt.Printf("Reclaimed: %s\n", res.Reclaimed)
        return nil
    },
}

func init() {
    cleanupCmd.Flags().Bool("dry-run", false, "show commands + disk usage without pruning")
    cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling")
    cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes (destructive)")

    rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 3: Add `Cleanup` to `mockRTForDeploy`** in `internal/cli/root_test.go` (after line 100):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
    return runtime.CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 4: Run tests** — `go test ./internal/cli/ -count=1`

- [ ] **Step 5: Commit** — `git commit -am "feat(cli): add tengiz cleanup command"`

---

### Task 4: Update remaining Manager mocks

**Files:**
- Modify: `internal/idle/idle_test.go` — add `Cleanup` to `mockRuntime` (after line 34)
- Modify: `internal/proxy/proxy_test.go` — add `Cleanup` to `mockRuntime` (after line 34)

**Interfaces:** Same signature as Task 3 Step 3.

- [ ] **Step 1: Add `Cleanup` to idle `mockRuntime`** (`internal/idle/idle_test.go`):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
    return runtime.CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 2: Add `Cleanup` to proxy `mockRuntime`** (`internal/proxy/proxy_test.go`):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
    return runtime.CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 3: Run full test suite** — `go test ./... -count=1` (all packages compile; proxy tests ~2s each)

- [ ] **Step 4: Commit** — `git commit -am "test: add Cleanup to runtime Manager mocks"`

---

### Task 5: Docs + feature marking

**Files:**
- Modify: `README.md` — CLI Reference section (~line 103): add `tengiz cleanup` line
- Modify: `AGENTS.md` — CLI list: add `tengiz cleanup` line
- Modify: `docs/FUTURES_FEATURES.md` — line 19: change `⬜` to `✅` and append implemented date; add status line to section at line 377

- [ ] **Step 1: Update `README.md`** CLI Reference — add after the notification block:

```markdown
tengiz cleanup [--dry-run] [--all] [--volumes] → prune unused Docker resources (Tengiz-managed containers/images/networks are protected via labels)
```

- [ ] **Step 2: Update `AGENTS.md`** CLI list — add:

```markdown
tengiz cleanup [--dry-run] [--all] [--volumes] → prune unused Docker resources (label-protected)
```

- [ ] **Step 3: Update `docs/FUTURES_FEATURES.md`**:
- Line 19 row #6: `| 6 | **Docker Housekeeping** ✅ | ... | Disk space is the #1 production issue on single-server deployments. Label-based \`docker system prune\`. \`tengiz cleanup\`. |`
- Section at line 377: add `- **Status:** ✅ Implemented (2026-08-17)`

- [ ] **Step 4: Final verification** — `go build ./...`, `go vet ./...`, `go test ./... -count=1`

- [ ] **Step 5: Commit** — `git commit -am "docs: document tengiz cleanup and mark Docker Housekeeping implemented"`

---

### Task 6: Self-review

- [ ] **Step 1: Review the full diff** — `git diff` against the previous commit; verify: no secrets, labels never changed for existing containers, `KeepLastNImages` untouched, no `--volumes` default-on (destructive opt-in preserved), dry-run never executes a prune
- [ ] **Step 2: Manual smoke test (if Docker available)** — `tengiz cleanup --dry-run` prints commands + `docker system df`; `tengiz cleanup` reclaims space and leaves `tengiz-app`-labeled containers/images untouched
- [ ] **Step 3: Confirm** `go test ./... -count=1` green one final time before declaring done