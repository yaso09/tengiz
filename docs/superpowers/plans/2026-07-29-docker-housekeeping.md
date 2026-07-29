# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command and runtime methods for label-aware Docker resource pruning (containers, images, volumes, networks, build cache) with per-category and `--all` modes.

**Architecture:** Extend `runtime.Manager` interface with `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache`, and `DiskUsage` methods. Implement via `docker` CLI `prune` commands. For containers/volumes/networks, use `--filter label!=tengiz-app` to protect Tengiz-managed resources while pruning everything else. Images and build cache don't use container labels — prune without filter. Add `tengiz cleanup` cobra command with `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, and `--all` flags. Report freed space.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI (via `os/exec`), `log/slog`

## Global Constraints

- Container/volume/network prune MUST use `--filter label!=tengiz-app` to protect Tengiz resources while pruning non-Tengiz. Image and build-cache prunes run without label filter (images lack container labels)
- `--all` flag prunes all categories (containers + images + volumes + networks + build cache)
- `tengiz cleanup` with no flags defaults to pruning only unused containers and dangling images
- All runtime methods must have stub implementations returning nil
- Follow existing patterns in `internal/runtime/cleanup.go` and `internal/runtime/docker.go`
- Commands must support `--env` global flag for env-scoped operations
- Report disk space freed (parse `docker system df` before/after or use `docker prune` output)
- Existing `RemoveImage` and `KeepLastNImages` remain unchanged

---

### Task 1: Extend Manager Interface with Prune Methods

**Files:**
- Modify: `internal/runtime/runtime.go` (Manager interface, stub methods)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: existing `Manager` interface, `context.Context`
- Produces: 6 new interface methods + 6 new stub methods

- [ ] **Step 1: Add 6 new methods to Manager interface**

```go
type Manager interface {
    // ... existing methods ...
    PruneContainers(ctx context.Context) (reclaimedBytes uint64, err error)
    PruneImages(ctx context.Context, all bool) (reclaimedBytes uint64, err error)
    PruneVolumes(ctx context.Context) (reclaimedBytes uint64, err error)
    PruneNetworks(ctx context.Context) (reclaimedBytes uint64, err error)
    PruneBuildCache(ctx context.Context, all bool) (reclaimedBytes uint64, err error)
    DiskUsage(ctx context.Context) (*DiskUsageInfo, error)
}

type DiskUsageInfo struct {
    Containers int
    Images     int
    Volumes    int
    BuildCache int
    Size       string // human-readable like "2.3GB"
}
```

- [ ] **Step 2: Add 6 stub methods on stubManager**

Add to `stubManager` in `internal/runtime/runtime.go`:

```go
func (m *stubManager) PruneContainers(ctx context.Context) (uint64, error) { return 0, nil }
func (m *stubManager) PruneImages(ctx context.Context, all bool) (uint64, error) { return 0, nil }
func (m *stubManager) PruneVolumes(ctx context.Context) (uint64, error) { return 0, nil }
func (m *stubManager) PruneNetworks(ctx context.Context) (uint64, error) { return 0, nil }
func (m *stubManager) PruneBuildCache(ctx context.Context, all bool) (uint64, error) { return 0, nil }
func (m *stubManager) DiskUsage(ctx context.Context) (*DiskUsageInfo, error) { return &DiskUsageInfo{}, nil }
```

- [ ] **Step 3: Write stub tests in `internal/runtime/cleanup_test.go`**

```go
func TestStubPruneContainers(t *testing.T) {
    m := NewStub()
    reclaimed, err := m.PruneContainers(context.Background())
    if err != nil {
        t.Fatalf("PruneContainers() error = %v", err)
    }
    if reclaimed != 0 {
        t.Errorf("PruneContainers() = %d, want 0", reclaimed)
    }
}

func TestStubPruneImages(t *testing.T) {
    m := NewStub()
    reclaimed, err := m.PruneImages(context.Background(), false)
    if err != nil {
        t.Fatalf("PruneImages() error = %v", err)
    }
    if reclaimed != 0 {
        t.Errorf("PruneImages() = %d, want 0", reclaimed)
    }
}

func TestStubPruneVolumes(t *testing.T) {
    m := NewStub()
    reclaimed, err := m.PruneVolumes(context.Background())
    if err != nil {
        t.Fatalf("PruneVolumes() error = %v", err)
    }
    if reclaimed != 0 {
        t.Errorf("PruneVolumes() = %d, want 0", reclaimed)
    }
}

func TestStubPruneNetworks(t *testing.T) {
    m := NewStub()
    reclaimed, err := m.PruneNetworks(context.Background())
    if err != nil {
        t.Fatalf("PruneNetworks() error = %v", err)
    }
    if reclaimed != 0 {
        t.Errorf("PruneNetworks() = %d, want 0", reclaimed)
    }
}

func TestStubPruneBuildCache(t *testing.T) {
    m := NewStub()
    reclaimed, err := m.PruneBuildCache(context.Background(), true)
    if err != nil {
        t.Fatalf("PruneBuildCache() error = %v", err)
    }
    if reclaimed != 0 {
        t.Errorf("PruneBuildCache() = %d, want 0", reclaimed)
    }
}

func TestStubDiskUsage(t *testing.T) {
    m := NewStub()
    info, err := m.DiskUsage(context.Background())
    if err != nil {
        t.Fatalf("DiskUsage() error = %v", err)
    }
    if info == nil {
        t.Fatal("DiskUsage() returned nil")
    }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add PruneContainers/PruneImages/PruneVolumes/PruneNetworks/PruneBuildCache/DiskUsage to Manager interface"
```

---

### Task 2: Implement Docker Prune Runtime Methods

**Files:**
- Create: `internal/runtime/prune.go`
- Modify: `internal/runtime/cleanup.go` (add helper `parsePruneOutput`)

**Interfaces:**
- Consumes: `context.Context`, `dockerRuntime` struct
- Produces: 6 methods on `*dockerRuntime` that implement the Manager interface

- [ ] **Step 1: Create `internal/runtime/prune.go` with dockerRuntime implementations**

```go
package runtime

import (
    "context"
    "fmt"
    "os/exec"
    "regexp"
    "strconv"
    "strings"
)

var reclaimPattern = regexp.MustCompile(`Total reclaimed space:\s*([\d.]+\s*\w*)`)

func parsePruneOutput(out []byte) uint64 {
    // Docker prune output format: "Total reclaimed space: 1.234GB"
    // or newer: "Total reclaimed space: 123MB"
    // We parse the bytes string and return the byte count
    // For simplicity, parse the human-readable string
    lines := strings.Split(string(out), "\n")
    for _, line := range lines {
        m := reclaimPattern.FindStringSubmatch(line)
        if len(m) >= 2 {
            return parseSize(m[1])
        }
    }
    return 0
}

func parseSize(s string) uint64 {
    s = strings.TrimSpace(s)
    if s == "" || s == "0" {
        return 0
    }
    multipliers := map[string]uint64{
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
    for suffix, mult := range multipliers {
        if strings.HasSuffix(s, suffix) {
            numStr := strings.TrimSpace(strings.TrimSuffix(s, suffix))
            val, err := strconv.ParseFloat(numStr, 64)
            if err != nil {
                return 0
            }
            return uint64(val * float64(mult))
        }
    }
    return 0
}

// protectLabelFilter returns docker filter args that EXCLUDE Tengiz-managed
// resources (`label!=tengiz-app`), so prune only affects non-Tengiz items.
// Only meaningful for containers, volumes, and networks (which carry labels).
func (r *dockerRuntime) protectLabelFilter() []string {
    return []string{
        "--filter", fmt.Sprintf("label!=%s", labelKey),
    }
}

func (r *dockerRuntime) PruneContainers(ctx context.Context) (uint64, error) {
    args := []string{"container", "prune", "-f"}
    args = append(args, r.protectLabelFilter()...)
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
    }
    return parsePruneOutput(out), nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, all bool) (uint64, error) {
    args := []string{"image", "prune", "-f"}
    if all {
        args = append(args, "-a")
    }
    // Images don't carry tengiz-app label; prune all unused.
    // Old Tengiz image versions are handled separately by KeepLastNImages.
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
    }
    return parsePruneOutput(out), nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context) (uint64, error) {
    args := []string{"volume", "prune", "-f"}
    args = append(args, r.protectLabelFilter()...)
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
    }
    return parsePruneOutput(out), nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context) (uint64, error) {
    args := []string{"network", "prune", "-f"}
    args = append(args, r.protectLabelFilter()...)
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
    }
    return parsePruneOutput(out), nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context, all bool) (uint64, error) {
    args := []string{"builder", "prune", "-f"}
    if all {
        args = append(args, "-a")
    }
    // Build cache has no tengiz labels; prune all.
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
    }
    return parsePruneOutput(out), nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (*DiskUsageInfo, error) {
    cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{.Type}}\t{{.TotalCount}}\t{{.Size}}")
    out, err := cmd.CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
    }
    info := &DiskUsageInfo{}
    lines := strings.Split(strings.TrimSpace(string(out)), "\n")
    for _, line := range lines {
        parts := strings.Split(line, "\t")
        if len(parts) < 3 {
            continue
        }
        typ := parts[0]
        count, _ := strconv.Atoi(parts[1])
        size := parts[2]
        switch typ {
        case "Containers":
            info.Containers = count
        case "Images":
            info.Images = count
        case "Volumes":
            info.Volumes = count
        case "Build Cache":
            info.BuildCache = count
        }
        if info.Size == "" || size > info.Size {
            info.Size = size // use last/all-encompassing size
        }
    }
    return info, nil
}
```

- [ ] **Step 2: Write unit tests for `parseSize` and `parsePruneOutput` helpers**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestParseSize(t *testing.T) {
    tests := []struct {
        input string
        want  uint64
    }{
        {"0", 0},
        {"", 0},
        {"1.5GB", 1500000000},
        {"234MB", 234000000},
        {"1GiB", 1073741824},
        {"500KiB", 512000},
        {"100B", 100},
        {"2.5TB", 2500000000000},
    }
    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            got := parseSize(tt.input)
            if got != tt.want {
                t.Errorf("parseSize(%q) = %d, want %d", tt.input, got, tt.want)
            }
        })
    }
}

func TestParsePruneOutput(t *testing.T) {
    output := []byte("Deleted Containers:\nabc123\nTotal reclaimed space: 1.234GB\n")
    got := parsePruneOutput(output)
    if got != 1234000000 {
        t.Errorf("parsePruneOutput() = %d, want 1234000000", got)
    }

    outputNoMatch := []byte("Nothing to clean")
    got = parsePruneOutput(outputNoMatch)
    if got != 0 {
        t.Errorf("parsePruneOutput() expected 0, got %d", got)
    }
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestParseSize|TestParsePruneOutput|TestStubPrune" -v -count=1`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/cleanup_test.go
git commit -m "feat: implement Docker prune runtime methods with label-aware filtering"
```

---

### Task 3: Add `tengiz cleanup` CLI Command

**Files:**
- Modify: `internal/cli/root.go` (add cleanupCmd, register in init(), add flags in Execute())

**Interfaces:**
- Consumes: `runtime.Manager` (with new prune methods), environment flag
- Produces: CLI command `tengiz cleanup [--containers] [--images] [--volumes] [--networks] [--build-cache] [--all] [--dry-run]`

- [ ] **Step 1: Add cleanup command variable after the notification commands section**

Insert after `notificationShowCmd` in `internal/cli/root.go` (around line 1481):

```go
var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Remove unused Docker resources (containers, images, volumes, networks, build cache)",
    Long: `Remove unused Docker resources and reclaim disk space.

By default, prunes only stopped containers and dangling images.
Use --all to prune all unused resources across all categories.
Use individual flags for surgical pruning.

Uses label-based filtering to protect Tengiz-managed containers,
volumes, and networks — only non-Tengiz resources are pruned.
Images and build cache are always pruned without restrictions.`,
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)

        all, _ := cmd.Flags().GetBool("all")
        pruneContainers, _ := cmd.Flags().GetBool("containers")
        pruneImages, _ := cmd.Flags().GetBool("images")
        pruneVolumes, _ := cmd.Flags().GetBool("volumes")
        pruneNetworks, _ := cmd.Flags().GetBool("networks")
        pruneBuildCache, _ := cmd.Flags().GetBool("build-cache")
        dryRun, _ := cmd.Flags().GetBool("dry-run")

        // If no specific flags, default to containers and dangling images
        hasSpecific := pruneContainers || pruneImages || pruneVolumes || pruneNetworks || pruneBuildCache
        if !hasSpecific && !all {
            pruneContainers = true
            pruneImages = true
        }
        if all {
            pruneContainers = true
            pruneImages = true
            pruneVolumes = true
            pruneNetworks = true
            pruneBuildCache = true
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        // Show disk usage before
        info, err := rt.DiskUsage(cmd.Context())
        if err == nil {
            fmt.Printf("[tengiz] disk usage before cleanup:\n")
            fmt.Printf("  Containers: %d\n", info.Containers)
            fmt.Printf("  Images:     %d\n", info.Images)
            fmt.Printf("  Volumes:    %d\n", info.Volumes)
            fmt.Printf("  Build Cache:%d\n", info.BuildCache)
        }

        var totalReclaimed uint64

        if pruneContainers {
            if dryRun {
                fmt.Println("[tengiz] [dry-run] would prune stopped containers")
            } else {
                reclaimed, err := rt.PruneContainers(cmd.Context())
                if err != nil {
                    log.Printf("[tengiz] warning: container prune: %v", err)
                } else {
                    totalReclaimed += reclaimed
                }
            }
        }
        if pruneImages {
            if dryRun {
                fmt.Println("[tengiz] [dry-run] would prune unused images")
            } else {
                reclaimed, err := rt.PruneImages(cmd.Context(), all)
                if err != nil {
                    log.Printf("[tengiz] warning: image prune: %v", err)
                } else {
                    totalReclaimed += reclaimed
                }
            }
        }
        if pruneVolumes {
            if dryRun {
                fmt.Println("[tengiz] [dry-run] would prune unused volumes")
            } else {
                reclaimed, err := rt.PruneVolumes(cmd.Context())
                if err != nil {
                    log.Printf("[tengiz] warning: volume prune: %v", err)
                } else {
                    totalReclaimed += reclaimed
                }
            }
        }
        if pruneNetworks {
            if dryRun {
                fmt.Println("[tengiz] [dry-run] would prune unused networks")
            } else {
                reclaimed, err := rt.PruneNetworks(cmd.Context())
                if err != nil {
                    log.Printf("[tengiz] warning: network prune: %v", err)
                } else {
                    totalReclaimed += reclaimed
                }
            }
        }
        if pruneBuildCache {
            if dryRun {
                fmt.Println("[tengiz] [dry-run] would prune build cache")
            } else {
                reclaimed, err := rt.PruneBuildCache(cmd.Context(), all)
                if err != nil {
                    log.Printf("[tengiz] warning: build cache prune: %v", err)
                } else {
                    totalReclaimed += reclaimed
                }
            }
        }

        if totalReclaimed > 0 {
            fmt.Printf("[tengiz] total reclaimed: %s\n", formatBytes(totalReclaimed))
        } else if !dryRun {
            fmt.Println("[tengiz] nothing to clean")
        }

        // Show disk usage after
        infoAfter, err := rt.DiskUsage(cmd.Context())
        if err == nil {
            fmt.Printf("[tengiz] disk usage after cleanup:\n")
            fmt.Printf("  Containers: %d\n", infoAfter.Containers)
            fmt.Printf("  Images:     %d\n", infoAfter.Images)
            fmt.Printf("  Volumes:    %d\n", infoAfter.Volumes)
            fmt.Printf("  Build Cache:%d\n", infoAfter.BuildCache)
        }

        return nil
    },
}

func formatBytes(b uint64) string {
    switch {
    case b >= 1000*1000*1000:
        return fmt.Sprintf("%.2fGB", float64(b)/(1000*1000*1000))
    case b >= 1000*1000:
        return fmt.Sprintf("%.2fMB", float64(b)/(1000*1000))
    case b >= 1000:
        return fmt.Sprintf("%.2fkB", float64(b)/1000)
    default:
        return fmt.Sprintf("%dB", b)
    }
}
```

- [ ] **Step 2: Register cleanupCmd in `init()`**

Add to `internal/cli/root.go` init() function (around line 38-89):

```go
rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 3: Add flags for cleanupCmd in `Execute()`**

Add to `internal/cli/root.go` Execute() function (around line 1785-1809):

```go
cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
cleanupCmd.Flags().Bool("images", false, "prune unused images")
cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
cleanupCmd.Flags().Bool("all", false, "prune all unused resources")
cleanupCmd.Flags().Bool("dry-run", false, "show what would be pruned without actually pruning")
```

- [ ] **Step 4: Verify it builds**

Run: `go build -o /dev/null .`
Expected: Build succeeds

- [ ] **Step 5: Run all tests**

Run: `go test ./... -v -count=1`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add 'tengiz cleanup' command with --all and per-category flags"
```

---

### Task 4: Add Integration Test for CLI Cleanup Command

**Files:**
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager` (stub)
- Produces: Tests that verify the cleanup command parses flags correctly and calls runtime methods

- [ ] **Step 1: Create `internal/cli/cleanup_test.go`**

```go
package cli

import (
    "bytes"
    "context"
    "strings"
    "testing"

    "github.com/yaso09/tengiz/internal/runtime"
)

// mockManager wraps runtime.Stub but tracks which prune methods were called
type mockCleanupManager struct {
    runtime.Manager
    pruneContainersCalled bool
    pruneImagesCalled     bool
    pruneVolumesCalled    bool
    pruneNetworksCalled   bool
    pruneBuildCacheCalled bool
}

func (m *mockCleanupManager) PruneContainers(ctx context.Context) (uint64, error) {
    m.pruneContainersCalled = true
    return 0, nil
}

func (m *mockCleanupManager) PruneImages(ctx context.Context, all bool) (uint64, error) {
    m.pruneImagesCalled = true
    return 0, nil
}

func (m *mockCleanupManager) PruneVolumes(ctx context.Context) (uint64, error) {
    m.pruneVolumesCalled = true
    return 0, nil
}

func (m *mockCleanupManager) PruneNetworks(ctx context.Context) (uint64, error) {
    m.pruneNetworksCalled = true
    return 0, nil
}

func (m *mockCleanupManager) PruneBuildCache(ctx context.Context, all bool) (uint64, error) {
    m.pruneBuildCacheCalled = true
    return 0, nil
}

func (m *mockCleanupManager) DiskUsage(ctx context.Context) (*runtime.DiskUsageInfo, error) {
    return &runtime.DiskUsageInfo{}, nil
}
```

- [ ] **Step 2: Add test for `formatBytes`**

```go
func TestFormatBytes(t *testing.T) {
    tests := []struct {
        input uint64
        want  string
    }{
        {0, "0B"},
        {500, "500B"},
        {1500, "1.50kB"},
        {1048576, "1.05MB"},
        {1073741824, "1.07GB"},
    }
    for _, tt := range tests {
        t.Run(tt.want, func(t *testing.T) {
            got := formatBytes(tt.input)
            if got != tt.want {
                t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
            }
        })
    }
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/cli/ -run TestFormatBytes -v -count=1`
Expected: PASS

- [ ] **Step 4: Run all tests**

Run: `go test ./... -v -count=1`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup_test.go
git commit -m "test: add formatBytes unit tests for cleanup command"
```

---

### Task 5: Run Final Verification

**Files:** (no changes)

- [ ] **Step 1: Build and vet**

Run: `go build -o tengiz . && go vet ./...`
Expected: No errors

- [ ] **Step 2: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: All tests PASS

- [ ] **Step 3: Verify the CLI help works**

Run: `go run . cleanup --help`
Expected: Shows help text with all flags (--containers, --images, --volumes, --networks, --build-cache, --all, --dry-run)

- [ ] **Step 4: Final commit if any fixes were needed**

```bash
git add -A
git commit -m "feat: implement Docker housekeeping with tengiz cleanup command"
```
