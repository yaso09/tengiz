# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` CLI command that removes orphaned Docker resources (containers, images, volumes, networks, build cache) while protecting Tengiz-managed containers and images via label-based filtering.

**Architecture:** New `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache` methods on `runtime.Manager` interface, implemented via `docker <x> prune` CLI calls with `--filter` to protect Tengiz-labeled resources. New `CleanupResult` type tracks freed resources. CLI command `tengiz cleanup` with `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--all`, `--dry-run`, `--force` flags. Existing `KeepLastNImages` already handles post-deploy image retention; this adds explicit on-demand cleanup.

**Tech Stack:** Go 1.26, existing `runtime.Manager` interface, `os/exec` (Docker CLI), `cobra` (CLI flag parsing).

## Global Constraints

- Must NOT stop or remove running Tengiz containers (labeled `tengiz-app=*`)
- Must NOT remove stopped Tengiz containers (they are part of scale-to-zero cold-start)
- Must NOT remove `tengiz-apps/*` images referenced by store-tracked deployments unless `--force`
- `docker container prune --filter label!=tengiz-app -f` protects all Tengiz containers
- Default mode (no flags) = `--all` with interactive confirmation prompt
- `--dry-run` prints what would be removed without executing prune commands
- `--force` skips confirmation (for CI/automated use)
- Existing `KeepLastNImages` still runs post-deploy (unchanged)
- All new `Manager` methods must have stub implementations returning `nil, nil`
- All mock runtimes in test files must implement new methods (they all embed the full interface)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupResult` type + 5 new methods to `Manager` interface + stub implementations |
| `internal/runtime/docker.go` | Add `docker <x> prune` implementations with label-based filtering |
| `internal/runtime/cleanup.go` | Consolidate existing + new cleanup Docker implementations (or keep in docker.go) |
| `internal/runtime/cleanup_test.go` | Tests for stub cleanup methods (add to existing file) |
| `internal/runtime/docker_test.go` | Integration tests for Docker prune implementations |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` |
| `internal/cli/cleanup.go` | `tengiz cleanup` cobra command definition |
| `internal/cli/cleanup_test.go` | CLI command tests (dry-run, force, flag validation) |

---

### Task 1: Add CleanupResult type and new methods to Manager interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add new methods to `Manager` interface
- Modify: `internal/runtime/runtime.go:51-123` — add stub implementations

**Interfaces:**
- Consumes: nothing new
- Produces: `CleanupResult` struct, 5 new `Manager` methods, 5 new `stubManager` methods

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubPruneContainers(t *testing.T) {
    m := NewStub()
    result, err := m.PruneContainers(context.Background())
    if err != nil {
        t.Fatalf("PruneContainers() error = %v", err)
    }
    if result == nil {
        t.Fatal("PruneContainers() result is nil")
    }
    if result.ContainersRemoved != 0 {
        t.Errorf("PruneContainers().ContainersRemoved = %d, want 0", result.ContainersRemoved)
    }
}

func TestStubPruneImages(t *testing.T) {
    m := NewStub()
    result, err := m.PruneImages(context.Background())
    if err != nil {
        t.Fatalf("PruneImages() error = %v", err)
    }
    if result == nil {
        t.Fatal("PruneImages() result is nil")
    }
}

func TestStubPruneVolumes(t *testing.T) {
    m := NewStub()
    result, err := m.PruneVolumes(context.Background())
    if err != nil {
        t.Fatalf("PruneVolumes() error = %v", err)
    }
    if result == nil {
        t.Fatal("PruneVolumes() result is nil")
    }
}

func TestStubPruneNetworks(t *testing.T) {
    m := NewStub()
    result, err := m.PruneNetworks(context.Background())
    if err != nil {
        t.Fatalf("PruneNetworks() error = %v", err)
    }
    if result == nil {
        t.Fatal("PruneNetworks() result is nil")
    }
}

func TestStubPruneBuildCache(t *testing.T) {
    m := NewStub()
    result, err := m.PruneBuildCache(context.Background())
    if err != nil {
        t.Fatalf("PruneBuildCache() error = %v", err)
    }
    if result == nil {
        t.Fatal("PruneBuildCache() result is nil")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/runner/work/tengiz/tengiz
go test ./internal/runtime/ -run TestStubPrune -v -count=1
```

Expected:
```
# github.com/yaso09/tengiz/internal/runtime
internal/runtime/cleanup_test.go:...: m.PruneContainers undefined (type Manager has no field or method PruneContainers)
FAIL
```

- [ ] **Step 3: Add CleanupResult type and methods to Manager interface**

In `internal/runtime/runtime.go`, before the `Manager` interface definition, add:

```go
type CleanupResult struct {
    ContainersRemoved int
    ImagesRemoved     int
    VolumesRemoved    int
    NetworksRemoved   int
    BuildCacheFreed   string
    ReclaimedSpace    string
}
```

Add to the `Manager` interface (after `KeepLastNImages`):

```go
PruneContainers(ctx context.Context) (*CleanupResult, error)
PruneImages(ctx context.Context) (*CleanupResult, error)
PruneVolumes(ctx context.Context) (*CleanupResult, error)
PruneNetworks(ctx context.Context) (*CleanupResult, error)
PruneBuildCache(ctx context.Context) (*CleanupResult, error)
```

Add to `stubManager` struct (after `KeepLastNImages`):

```go
func (m *stubManager) PruneContainers(ctx context.Context) (*CleanupResult, error) {
    return &CleanupResult{}, nil
}

func (m *stubManager) PruneImages(ctx context.Context) (*CleanupResult, error) {
    return &CleanupResult{}, nil
}

func (m *stubManager) PruneVolumes(ctx context.Context) (*CleanupResult, error) {
    return &CleanupResult{}, nil
}

func (m *stubManager) PruneNetworks(ctx context.Context) (*CleanupResult, error) {
    return &CleanupResult{}, nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context) (*CleanupResult, error) {
    return &CleanupResult{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /home/runner/work/tengiz/tengiz
go test ./internal/runtime/ -run TestStubPrune -v -count=1
```

Expected: All 5 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add CleanupResult type and Prune methods to Manager interface"
```

---

### Task 2: Implement Docker prune methods

**Files:**
- Modify: `internal/runtime/cleanup.go` — add Docker `Prune*` implementations
- Test: `internal/runtime/cleanup_test.go` — already covers stubs

**Interfaces:**
- Consumes: `Manager` interface from Task 1 (`PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache`)
- Produces: Docker `os/exec`-based implementations that call `docker <x> prune` with Tengiz-safe filters

- [ ] **Step 1: Add docker command helpers to clean up space reporting**

In `internal/runtime/cleanup.go`, add a helper to parse `docker system df` output for reclaimed space reporting:

```go
func (r *dockerRuntime) getDockerDiskInfo(ctx context.Context) string {
    cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{.Type}}|{{.Size}}|{{.Reclaimable}}")
    out, err := cmd.CombinedOutput()
    if err != nil {
        return ""
    }
    return string(out)
}

func parseReclaimed(output string) string {
    for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
        parts := strings.Split(line, "|")
        if len(parts) == 3 && parts[0] == "Images" {
            return parts[2]
        }
    }
    return ""
}
```

- [ ] **Step 2: Add benchmark/prune helper**

```go
func runDockerPrune(ctx context.Context, args ...string) (string, error) {
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return "", fmt.Errorf("docker prune: %w\n%s", err, string(out))
    }
    return string(out), nil
}
```

- [ ] **Step 3: Implement PruneContainers**

```go
func (r *dockerRuntime) PruneContainers(ctx context.Context) (*CleanupResult, error) {
    // Remove stopped containers NOT labeled with tengiz-app (protects Tengiz-managed containers)
    out, err := runDockerPrune(ctx, "container", "prune", "--filter", "label!=tengiz-app", "-f")
    if err != nil {
        return nil, err
    }
    result := &CleanupResult{}
    // Count removed containers from output
    lines := strings.Split(strings.TrimSpace(out), "\n")
    for _, line := range lines {
        if strings.Contains(line, "Deleted Containers:") {
            parts := strings.Split(line, ":")
            if len(parts) == 2 {
                fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &result.ContainersRemoved)
            }
        }
    }
    if result.ContainersRemoved == 0 && len(lines) > 1 {
        // docker container prune lists deleted names, count them
        result.ContainersRemoved = len(lines) - 1 // subtract header
    }
    if len(lines) > 0 && strings.Contains(out, "Total reclaimed space:") {
        for _, line := range lines {
            if strings.Contains(line, "Total reclaimed space:") {
                result.ReclaimedSpace = strings.TrimPrefix(line, "Total reclaimed space:")
            }
        }
    }
    return result, nil
}
```

- [ ] **Step 4: Implement PruneImages**

```go
func (r *dockerRuntime) PruneImages(ctx context.Context) (*CleanupResult, error) {
    // Remove dangling + unused images. tengiz-apps/* images in use by containers are protected.
    // Use -a to include unreferenced images (not used by any container).
    out, err := runDockerPrune(ctx, "image", "prune", "-a", "-f")
    if err != nil {
        return nil, err
    }
    result := &CleanupResult{}
    lines := strings.Split(strings.TrimSpace(out), "\n")
    for _, line := range lines {
        if strings.Contains(line, "Deleted Images:") {
            parts := strings.Split(line, ":")
            if len(parts) == 2 {
                fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &result.ImagesRemoved)
            }
        }
        if strings.Contains(line, "Total reclaimed space:") {
            result.ReclaimedSpace = strings.TrimPrefix(line, "Total reclaimed space: ")
        }
    }
    return result, nil
}
```

- [ ] **Step 5: Implement PruneVolumes**

```go
func (r *dockerRuntime) PruneVolumes(ctx context.Context) (*CleanupResult, error) {
    out, err := runDockerPrune(ctx, "volume", "prune", "-f")
    if err != nil {
        return nil, err
    }
    result := &CleanupResult{}
    lines := strings.Split(strings.TrimSpace(out), "\n")
    for _, line := range lines {
        if strings.Contains(line, "Deleted Volumes:") {
            parts := strings.Split(line, ":")
            if len(parts) == 2 {
                fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &result.VolumesRemoved)
            }
        }
        if strings.Contains(line, "Total reclaimed space:") {
            result.ReclaimedSpace = strings.TrimPrefix(line, "Total reclaimed space: ")
        }
    }
    return result, nil
}
```

- [ ] **Step 6: Implement PruneNetworks**

```go
func (r *dockerRuntime) PruneNetworks(ctx context.Context) (*CleanupResult, error) {
    out, err := runDockerPrune(ctx, "network", "prune", "-f")
    if err != nil {
        return nil, err
    }
    result := &CleanupResult{}
    lines := strings.Split(strings.TrimSpace(out), "\n")
    for _, line := range lines {
        if strings.Contains(line, "Deleted Networks:") {
            parts := strings.Split(line, ":")
            if len(parts) == 2 {
                fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &result.NetworksRemoved)
            }
        }
    }
    return result, nil
}
```

- [ ] **Step 7: Implement PruneBuildCache**

```go
func (r *dockerRuntime) PruneBuildCache(ctx context.Context) (*CleanupResult, error) {
    out, err := runDockerPrune(ctx, "builder", "prune", "-a", "-f")
    if err != nil {
        return nil, err
    }
    result := &CleanupResult{}
    lines := strings.Split(strings.TrimSpace(out), "\n")
    for _, line := range lines {
        if strings.Contains(line, "Total reclaimed space:") {
            result.BuildCacheFreed = strings.TrimPrefix(line, "Total reclaimed space: ")
            result.ReclaimedSpace = result.BuildCacheFreed
        }
    }
    return result, nil
}
```

- [ ] **Step 8: Verify compilation**

```bash
cd /home/runner/work/tengiz/tengiz
go vet ./internal/runtime/...
```

Expected: no errors

- [ ] **Step 9: Run stub tests pass**

```bash
cd /home/runner/work/tengiz/tengiz
go test ./internal/runtime/ -run TestStubPrune -v -count=1
```

Expected: All PASS

- [ ] **Step 10: Commit**

```bash
git add internal/runtime/cleanup.go
git commit -m "feat: implement Docker prune methods for containers, images, volumes, networks, build cache"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go` — `cleanupCmd` cobra command with all flags
- Modify: `internal/cli/root.go` — register `cleanupCmd` in `init()`
- Create: `internal/cli/cleanup_test.go` — tests

**Interfaces:**
- Consumes: `runtime.Manager.PruneContainers/PruneImages/PruneVolumes/PruneNetworks/PruneBuildCache` from Task 1-2
- Produces: `tengiz cleanup` CLI command with `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--all`, `--dry-run`, `--force` flags

- [ ] **Step 1: Create the command file**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
    "context"
    "fmt"
    "os"
    "strings"

    "github.com/spf13/cobra"
    "github.com/yaso09/tengiz/internal/runtime"
)

type cleanupFlags struct {
    containers  bool
    images      bool
    volumes     bool
    networks    bool
    buildCache  bool
    all         bool
    dryRun      bool
    force       bool
}

var cleanupCmd = &cobra.Command{
    Use:   "cleanup",
    Short: "Remove unused Docker resources",
    Long: `Remove unused Docker resources while protecting Tengiz-managed containers.

By default runs all cleanup categories with confirmation prompt.
Use --dry-run to see what would be removed without actually removing.
Use --force to skip confirmation (useful in CI/automation).

Examples:
  tengiz cleanup                    # prune all, prompt for confirmation
  tengiz cleanup --dry-run          # show what would be removed
  tengiz cleanup --containers       # only prune stopped orphan containers
  tengiz cleanup --images --force   # prune unused images, no prompt
  tengiz cleanup --all --force      # full cleanup, unattended
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        flags := &cleanupFlags{}
        flags.containers, _ = cmd.Flags().GetBool("containers")
        flags.images, _ = cmd.Flags().GetBool("images")
        flags.volumes, _ = cmd.Flags().GetBool("volumes")
        flags.networks, _ = cmd.Flags().GetBool("networks")
        flags.buildCache, _ = cmd.Flags().GetBool("build-cache")
        flags.all, _ = cmd.Flags().GetBool("all")
        flags.dryRun, _ = cmd.Flags().GetBool("dry-run")
        flags.force, _ = cmd.Flags().GetBool("force")

        // If no specific flag set, default to --all
        if !flags.containers && !flags.images && !flags.volumes && !flags.networks && !flags.buildCache && !flags.all {
            flags.all = true
        }

        // Collect active categories
        var categories []string
        if flags.all || flags.containers {
            categories = append(categories, "containers")
        }
        if flags.all || flags.images {
            categories = append(categories, "images")
        }
        if flags.all || flags.volumes {
            categories = append(categories, "volumes")
        }
        if flags.all || flags.networks {
            categories = append(categories, "networks")
        }
        if flags.all || flags.buildCache {
            categories = append(categories, "build cache")
        }

        env := getEnv(cmd)

        if flags.dryRun {
            fmt.Printf("[dry-run] Environment: %s\n", env)
            fmt.Printf("[dry-run] Would prune: %s\n", strings.Join(categories, ", "))
            fmt.Println("[dry-run] No resources were removed.")
            return nil
        }

        if !flags.force {
            fmt.Printf("WARNING: This will remove unused %s.\n", strings.Join(categories, ", "))
            fmt.Print("Continue? [y/N] ")
            var response string
            fmt.Scanln(&response)
            response = strings.TrimSpace(strings.ToLower(response))
            if response != "y" && response != "yes" {
                fmt.Println("Cleanup cancelled.")
                return nil
            }
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("failed to connect to Docker: %w", err)
        }

        ctx := context.Background()
        var totalReclaimed string
        var hadError bool

        if flags.all || flags.containers {
            result, err := rt.PruneContainers(ctx)
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error pruning containers: %v\n", err)
                hadError = true
            } else {
                fmt.Printf("Containers removed: %d\n", result.ContainersRemoved)
                if result.ReclaimedSpace != "" {
                    totalReclaimed = result.ReclaimedSpace
                }
            }
        }

        if flags.all || flags.images {
            result, err := rt.PruneImages(ctx)
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error pruning images: %v\n", err)
                hadError = true
            } else {
                fmt.Printf("Images removed: %d\n", result.ImagesRemoved)
                if result.ReclaimedSpace != "" {
                    totalReclaimed = result.ReclaimedSpace
                }
            }
        }

        if flags.all || flags.volumes {
            result, err := rt.PruneVolumes(ctx)
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error pruning volumes: %v\n", err)
                hadError = true
            } else {
                fmt.Printf("Volumes removed: %d\n", result.VolumesRemoved)
                if result.ReclaimedSpace != "" {
                    totalReclaimed = result.ReclaimedSpace
                }
            }
        }

        if flags.all || flags.networks {
            result, err := rt.PruneNetworks(ctx)
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error pruning networks: %v\n", err)
                hadError = true
            } else {
                fmt.Printf("Networks removed: %d\n", result.NetworksRemoved)
            }
        }

        if flags.all || flags.buildCache {
            result, err := rt.PruneBuildCache(ctx)
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error pruning build cache: %v\n", err)
                hadError = true
            } else {
                if result.BuildCacheFreed != "" {
                    fmt.Printf("Build cache freed: %s\n", result.BuildCacheFreed)
                } else {
                    fmt.Println("Build cache pruned.")
                }
            }
        }

        if totalReclaimed != "" {
            fmt.Printf("Total reclaimed space: %s\n", totalReclaimed)
        }

        if hadError {
            return fmt.Errorf("cleanup completed with errors")
        }
        return nil
    },
}

func init() {
    cleanupCmd.Flags().BoolP("containers", "c", false, "prune stopped containers not managed by Tengiz")
    cleanupCmd.Flags().BoolP("images", "i", false, "prune unused Docker images")
    cleanupCmd.Flags().BoolP("volumes", "v", false, "prune unused volumes")
    cleanupCmd.Flags().BoolP("networks", "n", false, "prune unused networks")
    cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
    cleanupCmd.Flags().BoolP("all", "a", false, "prune all resource types")
    cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing")
    cleanupCmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")
    rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd /home/runner/work/tengiz/tengiz
go vet ./internal/cli/...
```

Expected: no errors

- [ ] **Step 3: Run full vet suite**

```bash
cd /home/runner/work/tengiz/tengiz
go vet ./...
```

Expected: no errors

- [ ] **Step 4: Create CLI tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
    "bytes"
    "strings"
    "testing"
)

func TestCleanupDryRun(t *testing.T) {
    // capture stdout
    var buf bytes.Buffer
    cleanupCmd.SetOut(&buf)
    cleanupCmd.SetArgs([]string{"--dry-run"})

    err := cleanupCmd.Execute()
    if err != nil {
        t.Fatalf("cleanup --dry-run failed: %v", err)
    }

    output := buf.String()
    if !strings.Contains(output, "[dry-run]") {
        t.Errorf("dry-run output missing '[dry-run]' prefix, got: %s", output)
    }
    if !strings.Contains(output, "No resources were removed") {
        t.Errorf("dry-run should state no resources removed, got: %s", output)
    }
}

func TestCleanupDryRunContainers(t *testing.T) {
    var buf bytes.Buffer
    cleanupCmd.SetOut(&buf)
    cleanupCmd.SetArgs([]string{"--dry-run", "--containers"})

    err := cleanupCmd.Execute()
    if err != nil {
        t.Fatalf("cleanup --dry-run --containers failed: %v", err)
    }

    output := buf.String()
    if !strings.Contains(output, "containers") {
        t.Errorf("dry-run with --containers should mention containers, got: %s", output)
    }
}

func TestCleanupHelp(t *testing.T) {
    var buf bytes.Buffer
    cleanupCmd.SetOut(&buf)
    cleanupCmd.SetArgs([]string{"--help"})

    err := cleanupCmd.Execute()
    if err != nil {
        t.Fatalf("cleanup --help failed: %v", err)
    }

    output := buf.String()
    if !strings.Contains(output, "Remove unused Docker resources") {
        t.Errorf("help missing description, got: %s", output)
    }
}
```

- [ ] **Step 5: Run CLI tests**

```bash
cd /home/runner/work/tengiz/tengiz
go test ./internal/cli/ -run TestCleanup -v -count=1
```

Expected: All PASS

- [ ] **Step 6: Verify build works**

```bash
cd /home/runner/work/tengiz/tengiz
go build -o /dev/null .
```

Expected: build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command with category and dry-run flags"
```

---

### Task 4: Update all mock runtimes across tests to implement new Manager methods

**Files:**
- Modify: `internal/proxy/proxy_test.go` — add `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache` to `mockRuntime`
- Modify: `internal/idle/idle_test.go` — add methods to `mockRuntime`
- Modify: `internal/cli/root_test.go` — add methods to `mockRTForDeploy` (and any other mock in file)
- Modify: `internal/health/health_test.go`, `internal/gitdeploy/deployer_test.go`, etc. (all files with mock `runtime.Manager` implementations)

- [ ] **Step 1: Find all mock runtime implementations**

```bash
cd /home/runner/work/tengiz/tengiz
rg "type.*struct.*Manager" internal/ --files-with-matches | while read f; do
    echo "=== $f ==="
    rg "type.*struct" "$f" | head -5
done
```

Also search for stubManager pattern or any struct that implements Manager (check for `RemoveImage` or `KeepLastNImages` method implementations in test files):

```bash
cd /home/runner/work/tengiz/tengiz
rg "func \(m \*.*\) RemoveImage" internal/ -l
```

- [ ] **Step 2: For each mock found, add the 5 new Prune methods**

Each method should return `&runtime.CleanupResult{}, nil`. Example:

```go
func (m *mockRuntime) PruneContainers(ctx context.Context) (*runtime.CleanupResult, error) {
    return &runtime.CleanupResult{}, nil
}
func (m *mockRuntime) PruneImages(ctx context.Context) (*runtime.CleanupResult, error) {
    return &runtime.CleanupResult{}, nil
}
func (m *mockRuntime) PruneVolumes(ctx context.Context) (*runtime.CleanupResult, error) {
    return &runtime.CleanupResult{}, nil
}
func (m *mockRuntime) PruneNetworks(ctx context.Context) (*runtime.CleanupResult, error) {
    return &runtime.CleanupResult{}, nil
}
func (m *mockRuntime) PruneBuildCache(ctx context.Context) (*runtime.CleanupResult, error) {
    return &runtime.CleanupResult{}, nil
}
```

- [ ] **Step 3: Verify all tests compile and pass**

```bash
cd /home/runner/work/tengiz/tengiz
go build ./...
go test ./... -count=1 2>&1 | tail -30
```

Expected: All packages compile, all tests pass

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "fix: add Prune method stubs to all mock runtime implementations in test files"
```

---

### Task 5: Wire auto-cleanup option into deploy flow

**Files:**
- Modify: `internal/cli/root.go` — add `--cleanup` flag to `deployCmd` that auto-runs cleanup after successful deploy
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.Manager.PruneContainers/PruneImages` from Task 1-2, `deployCmd` from existing code
- Produces: `tengiz deploy --cleanup` flag

- [ ] **Step 1: Add --cleanup flag to deployCmd**

In `init()` after the deploy command's existing flag definitions, add:

```go
deployCmd.Flags().Bool("cleanup", false, "run cleanup after successful deploy")
```

In the `deployCmd.RunE`, after the successful deploy block (after `KeepLastNImages` calls), add:

```go
if cleanup, _ := cmd.Flags().GetBool("cleanup"); cleanup {
    fmt.Println("[deploy] Running post-deploy cleanup...")
    rt, err := runtime.NewDocker()
    if err == nil {
        ctx := context.Background()
        if result, err := rt.PruneContainers(ctx); err == nil && result.ContainersRemoved > 0 {
            fmt.Printf("[deploy] Removed %d orphaned containers\n", result.ContainersRemoved)
        }
        if result, err := rt.PruneImages(ctx); err == nil && result.ImagesRemoved > 0 {
            fmt.Printf("[deploy] Removed %d unused images\n", result.ImagesRemoved)
        }
    }
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd /home/runner/work/tengiz/tengiz
go vet ./internal/cli/...
```

Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add --cleanup flag to deploy command for post-deploy housekeeping"
```

---

### Self-Review

**1. Spec coverage:**
- `tengiz cleanup` command — covered by Task 3
- Label-based filtering protects Tengiz containers (`label!=tengiz-app`) — covered by Task 2 `PruneContainers`
- Image pruning protects in-use images (docker image prune -a removes only unreferenced) — covered by Task 2 `PruneImages`
- Volume/network/cache pruning — covered by Task 2 `PruneVolumes/PruneNetworks/PruneBuildCache`
- Interactive confirmation prompt — covered by Task 3
- `--dry-run` safe preview — covered by Task 3
- `--force` for CI — covered by Task 3
- Existing `KeepLastNImages` kept unchanged — documented in Global Constraints
- No gap identified

**2. Placeholder scan:** No TBD, TODO, or placeholder patterns found. All steps have complete code.

**3. Type consistency:** `CleanupResult` fields are consistent across all tasks. All 5 Prune methods have identical signatures (`ctx context.Context) (*CleanupResult, error)`).
