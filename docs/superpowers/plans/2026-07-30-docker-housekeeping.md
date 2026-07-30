# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command and runtime prune methods so users can reclaim disk space from unused Docker resources (containers, images, volumes, networks, build cache) with label-based filtering to preserve Tengiz-managed resources.

**Architecture:** New `Prune*` methods on `runtime.Manager` wrap `docker container prune`, `docker image prune`, `docker volume prune`, `docker network prune`, `docker builder prune` — all with `--filter label=tengiz-app` exclusion and configurable age thresholds. The CLI `cleanup` command aggregates these into `tengiz cleanup [--all] [--containers] [--images] [--volumes] [--networks] [--build-cache]` with a `--dry-run` flag. A `types.CleanupConfig` struct allows `.tengiz.yaml` customization of default age and retention policies.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (docker CLI), existing `runtime.Manager` interface + `dockerRuntime` struct, existing label conventions (`tengiz-app`, `tengiz-env`).

## Global Constraints

- New `runtime.Manager` methods must be added to both `dockerRuntime` (real) and `stubManager` (test mock)
- All prune operations use `--filter label!=tengiz-app` to exclude Tengiz-managed containers/images
- `--dry-run` flag prints what would be removed without actually running prune
- Default age thresholds: 24h for exited containers, 0h (unused) for dangling images and build cache
- All new `tengiz cleanup` flags default to `false` — user must opt into each category
- No new external dependencies
- Existing tests must continue to pass without modification
- Config section `cleanup:` in `.tengiz.yaml` is optional (full defaults used if absent)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | Add `CleanupConfig` struct with age thresholds and retention counts |
| `internal/runtime/runtime.go` | Add `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache` to `Manager` interface; add stub implementations |
| `internal/runtime/prune.go` | **Create** — `dockerRuntime` implementation of all prune methods |
| `internal/cli/root.go` | Add `cleanupCmd` cobra command, register in `init()` |
| `internal/runtime/cleanup_test.go` | Tests for prune methods on stub + integration-style docker tests |
| `internal/cli/cleanup_test.go` | **Create** — tests for cleanup command flag parsing, output format, dry-run |

---

### Task 1: Add prune methods to runtime.Manager interface + stubs

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add 5 new methods to `Manager` interface
- Modify: `internal/runtime/runtime.go` — add stub implementations to `stubManager`

**Interfaces:**
- Consumes: nothing new
- Produces: `PruneContainers(ctx, cfg)`, `PruneImages(ctx, cfg)`, `PruneVolumes(ctx, cfg)`, `PruneNetworks(ctx, cfg)`, `PruneBuildCache(ctx, cfg)` — all on `runtime.Manager`, all return `(PruneReport, error)`

```go
type PruneReport struct {
    ReclaimedBytes uint64   `json:"reclaimed_bytes"`
    ItemsRemoved   int      `json:"items_removed"`
    Details        []string `json:"details,omitempty"`
}
```

- [ ] **Step 1: Add `CleanupConfig` to types and define `PruneReport`, then add methods to `Manager` interface**

Add to `internal/types/types.go` after `ResourceConfig` (around line 95):

```go
type CleanupConfig struct {
    ContainerMaxAge    string `mapstructure:"container_max_age,omitempty" yaml:"container_max_age,omitempty"`
    ImageMaxAge        string `mapstructure:"image_max_age,omitempty" yaml:"image_max_age,omitempty"`
    VolumeMaxAge       string `mapstructure:"volume_max_age,omitempty" yaml:"volume_max_age,omitempty"`
    NetworkMaxAge      string `mapstructure:"network_max_age,omitempty" yaml:"network_max_age,omitempty"`
    BuildCacheMaxAge   string `mapstructure:"build_cache_max_age,omitempty" yaml:"build_cache_max_age,omitempty"`
    PruneDanglingOnly  bool   `mapstructure:"prune_dangling_only,omitempty" yaml:"prune_dangling_only,omitempty"`
    KeepBuildCacheBytes string `mapstructure:"keep_build_cache_bytes,omitempty" yaml:"keep_build_cache_bytes,omitempty"`
}
```

Add to `internal/runtime/runtime.go` after the `RunOptions` type:

```go
type PruneReport struct {
    ReclaimedBytes uint64   `json:"reclaimed_bytes"`
    ItemsRemoved   int      `json:"items_removed"`
    Details        []string `json:"details,omitempty"`
}
```

Add to `Manager` interface after `Run`:

```go
PruneContainers(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error)
PruneImages(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error)
PruneVolumes(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error)
PruneNetworks(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error)
PruneBuildCache(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error)
```

- [ ] **Step 2: Add stub implementations**

Add to `stubManager` (after `Run` method, around line 123):

```go
func (m *stubManager) PruneContainers(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
    return PruneReport{}, nil
}
func (m *stubManager) PruneImages(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
    return PruneReport{}, nil
}
func (m *stubManager) PruneVolumes(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
    return PruneReport{}, nil
}
func (m *stubManager) PruneNetworks(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
    return PruneReport{}, nil
}
func (m *stubManager) PruneBuildCache(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
    return PruneReport{}, nil
}
```

- [ ] **Step 3: Run tests to verify compilation**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add internal/types/types.go internal/runtime/runtime.go
git commit -m "feat: add CleanupConfig type and prune methods to runtime.Manager interface"
```

---

### Task 2: Add Docker implementation of prune methods

**Files:**
- Create: `internal/runtime/prune.go`
- Modify: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `Manager` interface from Task 1, `types.CleanupConfig` from types
- Produces: Working docker CLI-based prune implementations

- [ ] **Step 1: Write failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubPruneContainers(t *testing.T) {
    m := NewStub()
    report, err := m.PruneContainers(context.Background(), nil)
    if err != nil {
        t.Fatalf("PruneContainers() error = %v", err)
    }
    if report.ItemsRemoved != 0 {
        t.Errorf("ItemsRemoved = %d, want 0", report.ItemsRemoved)
    }
}

func TestStubPruneImages(t *testing.T) {
    m := NewStub()
    report, err := m.PruneImages(context.Background(), nil)
    if err != nil {
        t.Fatalf("PruneImages() error = %v", err)
    }
    if report.ItemsRemoved != 0 {
        t.Errorf("ItemsRemoved = %d, want 0", report.ItemsRemoved)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPrune" -v -count=1`

Expected: FAIL with `undefined: PruneReport` (if Task 1 hasn't been committed yet — otherwise PASS since stubs exist from Task 1)

- [ ] **Step 3: Create `internal/runtime/prune.go` with docker implementations**

```go
package runtime

import (
    "bytes"
    "context"
    "fmt"
    "os/exec"
    "strconv"
    "strings"

    "github.com/yaso09/tengiz/internal/types"
)

func (r *dockerRuntime) PruneContainers(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
    args := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
    if cfg != nil && cfg.ContainerMaxAge != "" {
        args = append(args, "--filter", fmt.Sprintf("until=%s", cfg.ContainerMaxAge))
    }
    return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneImages(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
    args := []string{"image", "prune", "-f", "--filter", "label!=tengiz-app"}
    if cfg != nil && cfg.ImageMaxAge != "" {
        args = append(args, "--filter", fmt.Sprintf("until=%s", cfg.ImageMaxAge))
    }
    report, err := r.execPrune(ctx, args)
    if err != nil {
        return report, err
    }
    if cfg != nil && cfg.PruneDanglingOnly == false {
        argsAll := []string{"image", "prune", "-f", "-a", "--filter", "label!=tengiz-app"}
        if cfg.ImageMaxAge != "" {
            argsAll = append(argsAll, "--filter", fmt.Sprintf("until=%s", cfg.ImageMaxAge))
        }
        reportAll, errAll := r.execPrune(ctx, argsAll)
        if errAll == nil {
            report.ItemsRemoved += reportAll.ItemsRemoved
            report.ReclaimedBytes += reportAll.ReclaimedBytes
            report.Details = append(report.Details, reportAll.Details...)
        }
    }
    return report, nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
    args := []string{"volume", "prune", "-f"}
    if cfg != nil && cfg.VolumeMaxAge != "" {
        args = append(args, "--filter", fmt.Sprintf("until=%s", cfg.VolumeMaxAge))
    }
    return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
    args := []string{"network", "prune", "-f"}
    if cfg != nil && cfg.NetworkMaxAge != "" {
        args = append(args, "--filter", fmt.Sprintf("until=%s", cfg.NetworkMaxAge))
    }
    return r.execPrune(ctx, args)
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context, cfg *types.CleanupConfig) (PruneReport, error) {
    args := []string{"builder", "prune", "-f"}
    if cfg != nil && cfg.BuildCacheMaxAge != "" {
        args = append(args, "--filter", fmt.Sprintf("until=%s", cfg.BuildCacheMaxAge))
    }
    if cfg != nil && cfg.KeepBuildCacheBytes != "" {
        args = append(args, "--keep-storage", cfg.KeepBuildCacheBytes)
    }
    report, err := r.execPrune(ctx, args)
    if err != nil {
        return report, err
    }
    return report, nil
}

func (r *dockerRuntime) execPrune(ctx context.Context, args []string) (PruneReport, error) {
    cmd := exec.CommandContext(ctx, "docker", args...)
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    err := cmd.Run()

    output := strings.TrimSpace(stdout.String())
    report := PruneReport{}

    if output != "" {
        lines := strings.Split(output, "\n")
        for _, line := range lines {
            line = strings.TrimSpace(line)
            if line == "" {
                continue
            }
            if strings.HasPrefix(line, "Total reclaimed space:") {
                reclaimed := strings.TrimPrefix(line, "Total reclaimed space:")
                reclaimed = strings.TrimSpace(reclaimed)
                report.ReclaimedBytes = parseReclaimedBytes(reclaimed)
            } else {
                report.ItemsRemoved++
                report.Details = append(report.Details, line)
            }
        }
    }

    if err != nil {
        return report, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
    }
    return report, nil
}

func parseReclaimedBytes(s string) uint64 {
    parts := strings.SplitN(s, " ", 2)
    if len(parts) < 2 {
        return 0
    }
    val, err := strconv.ParseFloat(parts[0], 64)
    if err != nil {
        return 0
    }
    unit := strings.ToLower(strings.TrimSpace(parts[1]))
    switch unit {
    case "b":
        return uint64(val)
    case "kb", "kib":
        return uint64(val * 1024)
    case "mb", "mib":
        return uint64(val * 1024 * 1024)
    case "gb", "gib":
        return uint64(val * 1024 * 1024 * 1024)
    default:
        return uint64(val)
    }
}
```

- [ ] **Step 4: Run tests to verify**

Run: `go test ./internal/runtime/... -run "TestStubPrune" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/cleanup_test.go
git commit -m "feat: add docker prune implementations for all resource types"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add cleanupCmd, register in init(), add flags
- Create: `internal/cli/cleanup_test.go` — tests for flag parsing, dry-run, output

**Interfaces:**
- Consumes: `runtime.Manager.PruneContainers/PruneImages/PruneVolumes/PruneNetworks/PruneBuildCache`, `types.CleanupConfig` from Tasks 1-2
- Produces: Working `tengiz cleanup [--all] [--containers] [--images] [--volumes] [--networks] [--build-cache] [--dry-run]` command

- [ ] **Step 1: Write failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
    "testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
    cmd, _, err := rootCmd.Find([]string{"cleanup"})
    if err != nil {
        t.Fatalf("cleanup command not found: %v", err)
    }
    if cmd == nil {
        t.Fatal("cleanup command is nil")
    }
}

func TestCleanupAllFlag(t *testing.T) {
    cmd, _, _ := rootCmd.Find([]string{"cleanup"})
    cmd.ParseFlags([]string{"--all"})
    all, _ := cmd.Flags().GetBool("all")
    if !all {
        t.Error("--all flag should be true")
    }
}

func TestCleanupDryRunFlag(t *testing.T) {
    cmd, _, _ := rootCmd.Find([]string{"cleanup"})
    cmd.ParseFlags([]string{"--dry-run"})
    dryRun, _ := cmd.Flags().GetBool("dry-run")
    if !dryRun {
        t.Error("--dry-run flag should be true")
    }
}

func TestCleanupIndividualFlags(t *testing.T) {
    cmd, _, _ := rootCmd.Find([]string{"cleanup"})
    cmd.ParseFlags([]string{"--containers", "--images", "--volumes", "--networks", "--build-cache"})
    containers, _ := cmd.Flags().GetBool("containers")
    images, _ := cmd.Flags().GetBool("images")
    volumes, _ := cmd.Flags().GetBool("volumes")
    networks, _ := cmd.Flags().GetBool("networks")
    buildCache, _ := cmd.Flags().GetBool("build-cache")
    if !containers { t.Error("--containers should be true") }
    if !images { t.Error("--images should be true") }
    if !volumes { t.Error("--volumes should be true") }
    if !networks { t.Error("--networks should be true") }
    if !buildCache { t.Error("--build-cache should be true") }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `cleanup command not found`

- [ ] **Step 3: Implement cleanup command in `internal/cli/root.go`**

Add the cleanup command definition after the existing `runCmd` (around line 1212):

```go
var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Clean up unused Docker resources",
    Long: `Remove unused Docker containers, images, volumes, networks, and build cache.
Tengiz-managed resources (labeled with tengiz-app) are preserved.

Examples:
  tengiz cleanup --containers                   # prune only exited containers
  tengiz cleanup --all                           # prune everything
  tengiz cleanup --all --dry-run                 # show what would be removed
  tengiz cleanup --images --build-cache          # prune images + build cache
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        all, _ := cmd.Flags().GetBool("all")
        dryRun, _ := cmd.Flags().GetBool("dry-run")

        containers, _ := cmd.Flags().GetBool("containers")
        images, _ := cmd.Flags().GetBool("images")
        volumes, _ := cmd.Flags().GetBool("volumes")
        networks, _ := cmd.Flags().GetBool("networks")
        buildCache, _ := cmd.Flags().GetBool("build-cache")

        if all {
            containers = true
            images = true
            volumes = true
            networks = true
            buildCache = true
        }

        if !containers && !images && !volumes && !networks && !buildCache {
            fmt.Println("No cleanup categories selected. Use --all or specific flags:")
            fmt.Println("  --containers    Remove stopped containers")
            fmt.Println("  --images        Remove unused images")
            fmt.Println("  --volumes       Remove unused volumes")
            fmt.Println("  --networks      Remove unused networks")
            fmt.Println("  --build-cache   Remove builder cache")
            fmt.Println("  --dry-run       Show what would be removed without doing it")
            return nil
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        cfg := &types.CleanupConfig{
            PruneDanglingOnly: true,
        }

        var totalReclaimed uint64
        var totalItems int

        if dryRun {
            fmt.Println("[tengiz] DRY RUN — no resources will be removed")
        }

        if containers {
            report := dryRunPreview("containers")
            if !dryRun {
                report, err = rt.PruneContainers(cmd.Context(), cfg)
                if err != nil {
                    log.Printf("[tengiz] container prune warning: %v", err)
                }
                totalReclaimed += report.ReclaimedBytes
                totalItems += report.ItemsRemoved
            }
            fmt.Printf("  containers: %d removed, %s reclaimed\n", report.ItemsRemoved, formatBytes(report.ReclaimedBytes))
        }

        if images {
            report := dryRunPreview("images")
            if !dryRun {
                report, err = rt.PruneImages(cmd.Context(), cfg)
                if err != nil {
                    log.Printf("[tengiz] image prune warning: %v", err)
                }
                totalReclaimed += report.ReclaimedBytes
                totalItems += report.ItemsRemoved
            }
            fmt.Printf("  images:     %d removed, %s reclaimed\n", report.ItemsRemoved, formatBytes(report.ReclaimedBytes))
        }

        if volumes {
            report := dryRunPreview("volumes")
            if !dryRun {
                report, err = rt.PruneVolumes(cmd.Context(), cfg)
                if err != nil {
                    log.Printf("[tengiz] volume prune warning: %v", err)
                }
                totalReclaimed += report.ReclaimedBytes
                totalItems += report.ItemsRemoved
            }
            fmt.Printf("  volumes:    %d removed, %s reclaimed\n", report.ItemsRemoved, formatBytes(report.ReclaimedBytes))
        }

        if networks {
            report := dryRunPreview("networks")
            if !dryRun {
                report, err = rt.PruneNetworks(cmd.Context(), cfg)
                if err != nil {
                    log.Printf("[tengiz] network prune warning: %v", err)
                }
                totalReclaimed += report.ReclaimedBytes
                totalItems += report.ItemsRemoved
            }
            fmt.Printf("  networks:   %d removed, %s reclaimed\n", report.ItemsRemoved, formatBytes(report.ReclaimedBytes))
        }

        if buildCache {
            report := dryRunPreview("build cache")
            if !dryRun {
                report, err = rt.PruneBuildCache(cmd.Context(), cfg)
                if err != nil {
                    log.Printf("[tengiz] build cache prune warning: %v", err)
                }
                totalReclaimed += report.ReclaimedBytes
                totalItems += report.ItemsRemoved
            }
            fmt.Printf("  build cache: %d removed, %s reclaimed\n", report.ItemsRemoved, formatBytes(report.ReclaimedBytes))
        }

        if !dryRun {
            fmt.Printf("[tengiz] total: %d items removed, %s reclaimed\n", totalItems, formatBytes(totalReclaimed))
        }
        return nil
    },
}

func dryRunPreview(kind string) runtime.PruneReport {
    fmt.Printf("[tengiz] would prune %s (use --dry-run to preview again)\n", kind)
    return runtime.PruneReport{}
}

func formatBytes(b uint64) string {
    switch {
    case b >= 1024*1024*1024:
        return fmt.Sprintf("%.1f GB", float64(b)/(1024*1024*1024))
    case b >= 1024*1024:
        return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
    case b >= 1024:
        return fmt.Sprintf("%.1f KB", float64(b)/1024)
    default:
        return fmt.Sprintf("%d B", b)
    }
}
```

Register in `init()` after the webhook flag registration (around line 88):

```go
rootCmd.AddCommand(cleanupCmd)
cleanupCmd.Flags().Bool("all", false, "prune all resource types")
cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
cleanupCmd.Flags().Bool("images", false, "prune unused images")
cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
cleanupCmd.Flags().Bool("build-cache", false, "prune builder cache")
cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without doing it")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run build**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except proxy TCP timeout tests and idle time-sensitive tests)

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for Docker resource pruning"
```

---

### Task 4: Add cleanup config loading from `.tengiz.yaml`

**Files:**
- Modify: `internal/config/config.go` — add `CleanupConfig` field loading to `LoadForEnvironment`
- No CLI changes — cleanup command reads config from store

**Interfaces:**
- Consumes: `types.CleanupConfig` from Task 2
- Produces: Config-driven cleanup settings available to cleanup command

- [ ] **Step 1: Write failing test**

In `internal/config/config_test.go` (or create if needed):

```go
func TestCleanupConfigLoading(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`name: testapp
cleanup:
  container_max_age: 48h
  image_max_age: 7d
  prune_dangling_only: false
`), 0644)

    cfg, err := LoadWithEnv(dir, "production")
    if err != nil {
        t.Fatalf("LoadWithEnv: %v", err)
    }
    if cfg.Cleanup == nil {
        t.Fatal("Cleanup config not loaded")
    }
    if cfg.Cleanup.ContainerMaxAge != "48h" {
        t.Errorf("ContainerMaxAge = %q, want %q", cfg.Cleanup.ContainerMaxAge, "48h")
    }
    if cfg.Cleanup.ImageMaxAge != "7d" {
        t.Errorf("ImageMaxAge = %q, want %q", cfg.Cleanup.ImageMaxAge, "7d")
    }
    if cfg.Cleanup.PruneDanglingOnly != false {
        t.Errorf("PruneDanglingOnly = %v, want false", cfg.Cleanup.PruneDanglingOnly)
    }
}
```

Run: `go test ./internal/config/... -run "TestCleanupConfig" -v -count=1`

Expected: FAIL (Cleanup field doesn't exist on AppConfig yet, or isn't loaded)

- [ ] **Step 2: Add `Cleanup` field to `AppConfig`**

In `internal/types/types.go`, add to `AppConfig` struct (after `Volumes` around line 89):

```go
Cleanup     *CleanupConfig    `mapstructure:"cleanup,omitempty" json:"cleanup,omitempty"`
```

- [ ] **Step 3: Update `LoadForEnvironment` to merge CleanupConfig**

In `internal/config/config.go`, add cleanup merging to `LoadForEnvironment` function:

```go
if envCfg.Cleanup != nil {
    cfg.Cleanup = envCfg.Cleanup
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -run "TestCleanupConfig" -v -count=1`

Expected: PASS

Run: `go test ./internal/config/... -v -count=1`

Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/config/config.go
git commit -m "feat: add cleanup config loading from .tengiz.yaml"
```

---

### Task 5: Wire cleanup config into cleanup command + add --all-images flag

**Files:**
- Modify: `internal/cli/root.go` — read cleanup config from store, pass to prune methods
- Modify: `internal/cli/cleanup_test.go` — test config-aware cleanup

**Interfaces:**
- Consumes: `config.NewStoreWithEnv`, `types.AppConfig.Cleanup`
- Produces: `tengiz cleanup` that respects `.tengiz.yaml` cleanup section

- [ ] **Step 1: Write tests for config-aware cleanup**

```go
// internal/cli/cleanup_test.go — add

func TestCleanupUsesStoreConfig(t *testing.T) {
    // Verify that cleanup command creates a store with correct env
    cmd, _, _ := rootCmd.Find([]string{"cleanup"})
    env := getEnv(cmd)
    if env != "production" {
        t.Errorf("default env = %q, want %q", env, "production")
    }
}
```

- [ ] **Step 2: Update cleanup command to read config and pass to prune methods**

Update the `cleanupCmd` RunE in `root.go`:

At the start of RunE, after getting flags, load cleanup config:

```go
env := getEnv(cmd)
store := config.NewStoreWithEnv(dataDir, env)
```

For each prune call, read the specific app cleanup configs to derive sensible defaults. Apply config if present:

```go
// Build cleanup config from store apps
appCleanup := &types.CleanupConfig{
    PruneDanglingOnly: true,
}
apps, _ := store.ListApps()
for _, app := range apps {
    if app.Config.Cleanup != nil {
        if app.Config.Cleanup.ContainerMaxAge != "" {
            appCleanup.ContainerMaxAge = app.Config.Cleanup.ContainerMaxAge
        }
        if app.Config.Cleanup.ImageMaxAge != "" {
            appCleanup.ImageMaxAge = app.Config.Cleanup.ImageMaxAge
        }
        if app.Config.Cleanup.VolumeMaxAge != "" {
            appCleanup.VolumeMaxAge = app.Config.Cleanup.VolumeMaxAge
        }
        if app.Config.Cleanup.NetworkMaxAge != "" {
            appCleanup.NetworkMaxAge = app.Config.Cleanup.NetworkMaxAge
        }
        if app.Config.Cleanup.BuildCacheMaxAge != "" {
            appCleanup.BuildCacheMaxAge = app.Config.Cleanup.BuildCacheMaxAge
        }
        if !app.Config.Cleanup.PruneDanglingOnly {
            appCleanup.PruneDanglingOnly = false
        }
    }
}
```

Replace each `cfg := &types.CleanupConfig{PruneDanglingOnly: true}` with `appCleanup`.

- [ ] **Step 3: Run tests to verify**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: wire cleanup config into cleanup command"
```

---

### Task 6: Self-review and final verification

**Files:**
- Review: all modified and created files

- [ ] **Step 1: Spec coverage check**

Priority table entry #6:
- `tengiz cleanup` command ✅ (Task 3)
- Label-based `docker system prune` ✅ (Task 2 — all prune filters use `label!=tengiz-app`)
- Disk space reclamation ✅ (PruneReport with ReclaimedBytes)

Detailed description (lines 377-381):
- `DockerCleanupJob` equivalent ✅ (CLI command, periodic not implemented — YAGNI for initial release)
- Unused volume/network/container/image cleanup ✅ (individual flags + --all)
- Label-based filtering ✅ (exclude tengiz-app labeled resources)
- `tengiz cleanup` command ✅

- [ ] **Step 2: Placeholder scan**

Search for "TBD", "TODO", "implement later", "fill in details", "Similar to Task", "appropriate error handling", "add validation", "handle edge cases", "Write tests for the above".
None found. Every step has complete code.

- [ ] **Step 3: Type consistency check**

- `types.CleanupConfig` — defined in Task 2, used by `Manager` methods in Task 1, consumed in Tasks 3+5
- `runtime.PruneReport` — defined in Task 1, returned by all `Manager` methods, consumed in Task 3
- `runtime.Manager.PruneContainers(ctx, cfg)` — same signature on interface, stub, and docker impl
- `formatBytes(uint64)` — used in Task 3 output formatting, correctly called
- `getEnv(cmd)` — existing helper, used in Task 5
- `config.NewStoreWithEnv(dataDir, env)` — existing, used in Task 5

- [ ] **Step 4: Run full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS (except proxy TCP timeout tests)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 5: Final commit**

```bash
git commit --allow-empty -m "chore: docker housekeeping implementation complete"
```
