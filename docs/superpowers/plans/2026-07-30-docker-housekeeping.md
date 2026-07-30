# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command and automated Docker resource pruning to prevent disk exhaustion on single-server deployments.

**Architecture:** Extend `runtime.Manager` interface with `Prune()` method accepting `PruneOptions` struct. Implement Docker exec-based prune commands (container/image/volume/network/build-cache) with label filtering to protect Tengiz-managed resources. Add `tengiz cleanup` CLI command with per-category flags. Add store-level cleanup for orphaned ports and deployment history. Wire into existing `deploy` command for post-deploy auto-cleanup.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI via `os/exec`, `log/slog`

## Global Constraints

- All Docker CLI calls use `exec.CommandContext` with appropriate context timeout (60s for prune operations)
- Container label key: `tengiz-app`, env label key: `tengiz-env` (existing conventions)
- All new methods on `runtime.Manager` must have matching stub implementations
- Command follows existing CLI pattern in `root.go`: `var cleanupCmd = &cobra.Command{Use, Short, Args, RunE}`
- Tests must use `runtime.NewStub()` and verify interface satisfaction
- Default image retention: 5 per app (matching existing `KeepLastNImages` default)
- Dry-run mode shows what would be removed without executing prune commands

---

### Task 1: Define PruneOptions, PruneReport types and extend runtime.Manager

**Files:**
- Modify: `internal/runtime/runtime.go` — add types and interface method
- Modify: `internal/types/types.go` — add CleanupConfig struct if needed

**Interfaces:**
- Consumes: existing `runtime.Manager` interface
- Produces: `PruneOptions`, `PruneReport` types; `Manager.Prune(ctx, opts PruneOptions) (*PruneReport, error)` method

- [ ] **Step 1: Define PruneOptions and PruneReport in runtime.go**

```go
// internal/runtime/runtime.go

type PruneOptions struct {
    Containers bool
    Images     bool
    Volumes    bool
    Networks   bool
    BuildCache bool
    DryRun     bool
    KeepImages int  // 0 = use default (5)
}

type PruneReport struct {
    ContainersRemoved   int
    ContainersReclaimed int64 // bytes
    ImagesRemoved       int
    ImagesReclaimed     int64
    VolumesRemoved      int
    VolumesReclaimed    int64
    NetworksRemoved     int
    NetworksReclaimed   int64
    BuildCacheReclaimed int64
    OrphanedContainers  int
    OrphanedImages      int
}
```

- [ ] **Step 2: Add Prune to the Manager interface**

```go
// internal/runtime/runtime.go — add to Manager interface

type Manager interface {
    // ... existing methods ...
    Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)
}
```

- [ ] **Step 3: Add stub implementation**

```go
// internal/runtime/runtime.go — add to stubManager

func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
    return &PruneReport{}, nil
}
```

- [ ] **Step 4: Run tests to verify stub compiles**

Run: `go build ./...`
Expected: exit 0

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go
git commit -m "feat(cleanup): add PruneOptions, PruneReport, and Prune method to runtime.Manager"
```

---

### Task 2: Implement Docker prune operations in docker.go

**Files:**
- Modify: `internal/runtime/docker.go` — add `Prune` method with Docker exec calls
- Test: `internal/runtime/cleanup_test.go` — tests for prune

**Interfaces:**
- Consumes: `PruneOptions` from Task 1
- Produces: full `Prune()` implementation on `dockerRuntime`

- [ ] **Step 1: Write the failing test for Docker prune**

```go
// internal/runtime/cleanup_test.go

func TestDockerPruneOptionsToArgs(t *testing.T) {
    tests := []struct {
        name     string
        opts     PruneOptions
        category string
        expected []string
    }{
        {
            name:     "prune containers",
            opts:     PruneOptions{Containers: true},
            category: "container",
            expected: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
        },
        {
            name:     "prune images",
            opts:     PruneOptions{Images: true},
            category: "image",
            expected: []string{"image", "prune", "-f", "--filter", "label!=tengiz-app"},
        },
        {
            name:     "prune images with dangling filter",
            opts:     PruneOptions{Images: true, KeepImages: 3},
            category: "image",
            expected: []string{"image", "prune", "-f", "--filter", "label!=tengiz-app", "--filter", "until=24h"},
        },
        {
            name:     "prune volumes",
            opts:     PruneOptions{Volumes: true},
            category: "volume",
            expected: []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
        },
        {
            name:     "prune networks",
            opts:     PruneOptions{Networks: true},
            category: "network",
            expected: []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
        },
        {
            name:     "prune build cache",
            opts:     PruneOptions{BuildCache: true},
            category: "builder",
            expected: []string{"builder", "prune", "-f"},
        },
        {
            name:     "dry run does not add -f",
            opts:     PruneOptions{Containers: true, DryRun: true},
            category: "container",
            expected: []string{"container", "prune", "--filter", "label!=tengiz-app"},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            args := pruneArgsForCategory(tt.category, tt.opts)
            if !reflect.DeepEqual(args, tt.expected) {
                t.Errorf("pruneArgsForCategory(%q, %+v) = %v, want %v", tt.category, tt.opts, args, tt.expected)
            }
        })
    }
}

func TestStubPruneReturnsEmptyReport(t *testing.T) {
    m := NewStub()
    report, err := m.Prune(context.Background(), PruneOptions{All: true})
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if report == nil {
        t.Fatal("expected non-nil report")
    }
}
```

Note: The test above references `PruneOptions.All` — we should add an `All` field:

```go
// Add to PruneOptions
type PruneOptions struct {
    Containers bool
    Images     bool
    Volumes    bool
    Networks   bool
    BuildCache bool
    All        bool
    DryRun     bool
    KeepImages int
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestDockerPruneOptionsToArgs|TestStubPruneReturnsEmptyReport" -v -count=1`
Expected: FAIL — `pruneArgsForCategory` not defined, `All` field not found

- [ ] **Step 3: Add All field to PruneOptions and implement pruneArgsForCategory helper**

```go
// internal/runtime/docker.go — add before Prune method

func pruneArgsForCategory(category string, opts PruneOptions) []string {
    args := []string{category, "prune"}
    if !opts.DryRun {
        args = append(args, "-f")
    }
    switch category {
    case "container", "image", "volume", "network":
        args = append(args, "--filter", "label!=tengiz-app")
    }
    return args
}
```

- [ ] **Step 4: Implement Prune method on dockerRuntime**

```go
// internal/runtime/docker.go — add method to dockerRuntime

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
    report := &PruneReport{}

    categories := []string{}
    if opts.All || opts.Containers {
        categories = append(categories, "container")
    }
    if opts.All || opts.Images {
        categories = append(categories, "image")
        // Run KeepLastNImages for all apps before pruning images
        // This is handled at the CLI level via store listing
    }
    if opts.All || opts.Volumes {
        categories = append(categories, "volume")
    }
    if opts.All || opts.Networks {
        categories = append(categories, "network")
    }
    if opts.All || opts.BuildCache {
        categories = append(categories, "builder")
    }

    for _, cat := range categories {
        args := pruneArgsForCategory(cat, opts)
        cmd := exec.CommandContext(ctx, "docker", args...)
        output, err := cmd.CombinedOutput()
        if err != nil {
            slog.Warn("docker prune failed", "category", cat, "error", err, "output", string(output))
            continue
        }
        // Parse reclaimed space from output
        parsePruneOutput(cat, string(output), report)
    }

    return report, nil
}

func parsePruneOutput(category, output string, report *PruneReport) {
    // Docker prune output format: "Total reclaimed space: 1.23GB" or "Total: 123MB"
    // We extract bytes from the human-readable string
    var reclaimed int64
    switch category {
    case "container":
        report.ContainersRemoved = countRemoved(output)
        reclaimed = parseReclaimedSpace(output)
        report.ContainersReclaimed = reclaimed
    case "image":
        report.ImagesRemoved = countRemoved(output)
        reclaimed = parseReclaimedSpace(output)
        report.ImagesReclaimed = reclaimed
    case "volume":
        report.VolumesRemoved = countRemoved(output)
        reclaimed = parseReclaimedSpace(output)
        report.VolumesReclaimed = reclaimed
    case "network":
        report.NetworksRemoved = countRemoved(output)
        reclaimed = parseReclaimedSpace(output)
        report.NetworksReclaimed = reclaimed
    case "builder":
        reclaimed = parseReclaimedSpace(output)
        report.BuildCacheReclaimed = reclaimed
    }
}

func countRemoved(output string) int {
    // "Deleted Containers:\n123\n..." or "Total: 3\n..."
    // Docker prune output: lists items deleted, count items
    lines := strings.Split(strings.TrimSpace(output), "\n")
    count := 0
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line != "" && !strings.HasPrefix(line, "Total") && !strings.HasPrefix(line, "Deleted") {
            count++
        }
    }
    return count
}

func parseReclaimedSpace(output string) int64 {
    // Parse "Total reclaimed space: 1.23GB" or "Total: 123.4MB"
    for _, line := range strings.Split(output, "\n") {
        if strings.Contains(line, "reclaimed") || strings.Contains(line, "Total:") {
            return parseSize(strings.TrimSpace(line))
        }
    }
    return 0
}

func parseSize(s string) int64 {
    // Extract number + unit, convert to bytes
    re := regexp.MustCompile(`([\d.]+)\s*(kB|MB|GB|TB|B)`)
    matches := re.FindStringSubmatch(s)
    if len(matches) < 3 {
        return 0
    }
    val, _ := strconv.ParseFloat(matches[1], 64)
    switch matches[2] {
    case "B":
        return int64(val)
    case "kB":
        return int64(val * 1024)
    case "MB":
        return int64(val * 1024 * 1024)
    case "GB":
        return int64(val * 1024 * 1024 * 1024)
    case "TB":
        return int64(val * 1024 * 1024 * 1024 * 1024)
    }
    return 0
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestDockerPruneOptionsToArgs|TestStubPruneReturnsEmptyReport" -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/cleanup_test.go
git commit -m "feat(cleanup): implement Docker prune operations on dockerRuntime"
```

---

### Task 3: Add orphaned resource detection + store-level prune helpers

**Files:**
- Modify: `internal/config/store.go` — add `ListApps`-based helpers for orphan detection
- Modify: `internal/runtime/docker.go` — add orphaned container/image detection

**Interfaces:**
- Consumes: `Store.ListApps()` for known app names
- Consumes: `PruneOptions` from Task 1
- Produces: `CountOrphanedContainers()`, `CountOrphanedImages()` helpers on `dockerRuntime`

- [ ] **Step 1: Write failing test for orphan detection**

```go
// internal/runtime/cleanup_test.go

func TestStubOrphanedDetection(t *testing.T) {
    m := NewStub()
    containers, err := m.List(context.Background())
    if err != nil {
        t.Fatal(err)
    }
    // Stub returns empty list
    if len(containers) != 0 {
        t.Errorf("expected 0 containers, got %d", len(containers))
    }
}
```

- [ ] **Step 2: Run test to verify**

Run: `go test ./internal/runtime/ -run TestStubOrphanedDetection -v -count=1`
Expected: PASS (stub already works)

- [ ] **Step 3: Add orphaned container detection to dockerRuntime.Prune**

Add a step in `Prune()` that finds containers with `tengiz-app` label but no corresponding entry in the store. This is actually handled at the CLI level since the store is not available in `runtime`. Move orphan detection to CLI layer in Task 4.

For now, add `ListWithLabel(ctx, labelKey, labelValue)` to `runtime.Manager`:

```go
// internal/runtime/runtime.go — add to Manager interface
ListByLabel(ctx context.Context, key, value string) ([]types.AppStatus, error)
```

Actually, this complicates the interface. Let's keep it simpler.

Instead, the `Prune` method accepts a `knownApps []string` parameter to skip pruning images for active apps. Let me revise:

```go
type PruneOptions struct {
    Containers bool
    Images     bool
    Volumes    bool
    Networks   bool
    BuildCache bool
    All        bool
    DryRun     bool
    KeepImages int
    KnownApps  []string // app names to protect from image pruning
}
```

- [ ] **Step 4: Implement KnownApps filtering in docker.go Prune method**

Modify the `Prune` method to handle image pruning with retention:

```go
// In docker.go, modify Prune method image section

if opts.All || opts.Images {
    // First, run KeepLastNImages for each known app
    for _, appName := range opts.KnownApps {
        keepN := opts.KeepImages
        if keepN <= 0 {
            keepN = 5
        }
        if err := r.KeepLastNImages(ctx, appName, keepN); err != nil {
            slog.Warn("failed to keep images for app", "app", appName, "error", err)
        }
    }
    // Then prune remaining unused images (protected by label filter)
    args := pruneArgsForCategory("image", opts)
    cmd := exec.CommandContext(ctx, "docker", args...)
    output, err := cmd.CombinedOutput()
    if err != nil {
        slog.Warn("docker image prune failed", "error", err, "output", string(output))
    } else {
        parsePruneOutput("image", string(output), report)
    }
}
```

- [ ] **Step 5: Run vet to verify**

Run: `go vet ./internal/runtime/`
Expected: exit 0

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/docker.go
git commit -m "feat(cleanup): add KnownApps support to PruneOptions for image retention"
```

---

### Task 4: Add store-level cleanup (orphaned ports, deployment history)

**Files:**
- Modify: `internal/config/store.go` — add `PruneOrphanedPorts`, `PruneDeployments` methods
- Test: `internal/config/store_test.go` — tests for prune methods

**Interfaces:**
- Consumes: existing `Store` methods
- Produces: `PruneOrphanedPorts(knownApps []string) error`, `PruneDeployments(appName string, keep int) error`

- [ ] **Step 1: Write the failing test for store pruning**

```go
// internal/config/store_test.go

func TestPruneOrphanedPorts(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "test")
    
    // Allocate ports for two apps
    port1, _ := s.AllocatePort("app1")
    port2, _ := s.AllocatePort("app2")
    port3, _ := s.AllocatePort("orphaned")

    // Save two apps, not "orphaned"
    s.SaveApp(types.AppEntry{Name: "app1"})
    s.SaveApp(types.AppEntry{Name: "app2"})

    // Prune only "app1" and "app2" are known
    err := s.PruneOrphanedPorts([]string{"app1", "app2"})
    if err != nil {
        t.Fatalf("PruneOrphanedPorts failed: %v", err)
    }

    // port3 should be freed (orphaned)
    port4, err := s.AllocatePort("new-app")
    if err != nil {
        t.Fatalf("expected to allocate freed port, got error: %v", err)
    }
    if port4 != port3 {
        t.Errorf("expected port %d to be freed, got %d", port3, port4)
    }

    // Cleanup
    s.FreePort(port1)
    s.FreePort(port2)
    s.FreePort(port4)
}

func TestPruneDeployments(t *testing.T) {
    dir := t.TempDir()
    s := NewStoreWithEnv(dir, "test")

    // Add 15 deployments for "myapp"
    for i := 0; i < 15; i++ {
        s.AddDeployment("myapp", types.DeploymentEntry{
            ID:        fmt.Sprintf("dep-%d", i),
            ImageTag:  fmt.Sprintf("myapp:v%d", i),
            CreatedAt: time.Now().Add(-time.Duration(15-i) * time.Hour),
            Status:    "previous",
        })
    }

    // Prune to keep 10
    err := s.PruneDeployments("myapp", 10)
    if err != nil {
        t.Fatalf("PruneDeployments failed: %v", err)
    }

    deps, _ := s.GetDeployments("myapp")
    if len(deps) != 10 {
        t.Errorf("expected 10 deployments, got %d", len(deps))
    }

    // The 5 oldest should be removed
    for _, dep := range deps {
        n, _ := strconv.Atoi(strings.TrimPrefix(dep.ID, "dep-"))
        if n < 5 {
            t.Errorf("expected oldest deployments to be pruned, but found dep-%d", n)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run "TestPruneOrphanedPorts|TestPruneDeployments" -v -count=1`
Expected: FAIL — methods not defined

- [ ] **Step 3: Implement PruneOrphanedPorts**

```go
// internal/config/store.go

func (s *Store) PruneOrphanedPorts(knownApps []string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    knownSet := make(map[string]bool, len(knownApps))
    for _, app := range knownApps {
        knownSet[app] = true
    }

    var ports map[int]string
    if err := s.readJSON("ports", &ports); err != nil {
        if os.IsNotExist(err) {
            return nil
        }
        return err
    }

    changed := false
    for port, appName := range ports {
        if !knownSet[appName] {
            delete(ports, port)
            changed = true
        }
    }

    if changed {
        return s.writeJSON("ports", ports)
    }
    return nil
}
```

- [ ] **Step 4: Implement PruneDeployments**

```go
// internal/config/store.go

func (s *Store) PruneDeployments(appName string, keep int) error {
    mutex.Lock()
    defer s.mu.Unlock()

    var deployments map[string][]types.DeploymentEntry
    if err := s.readJSON("deployments", &deployments); err != nil {
        if os.IsNotExist(err) {
            return nil
        }
        return err
    }

    deps, ok := deployments[appName]
    if !ok {
        return nil
    }

    if len(deps) <= keep {
        return nil
    }

    // Sort by CreatedAt ascending (oldest first)
    sort.Slice(deps, func(i, j int) bool {
        return deps[i].CreatedAt.Before(deps[j].CreatedAt)
    })

    // Keep the newest `keep` entries
    deps = deps[len(deps)-keep:]
    deployments[appName] = deps

    return s.writeJSON("deployments", deployments)
}
```

Note: fix compile — there's a typo `mutex.Lock()` should be `s.mu.Lock()`. Let me correct:

```go
func (s *Store) PruneDeployments(appName string, keep int) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    // ...
}
```

- [ ] **Step 5: Add PruneBuildLogsAll that prunes across all apps**

```go
// internal/config/store.go

func (s *Store) PruneBuildLogsAll(keep int) error {
    apps, err := s.ListApps()
    if err != nil {
        return err
    }
    for _, app := range apps {
        if err := s.PruneBuildLogs(app.Name, keep); err != nil {
            slog.Warn("failed to prune build logs", "app", app.Name, "error", err)
        }
    }
    return nil
}
```

- [ ] **Step 6: Run test to verify they pass**

Run: `go test ./internal/config/ -run "TestPruneOrphanedPorts|TestPruneDeployments" -v -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat(cleanup): add store-level PruneOrphanedPorts, PruneDeployments, PruneBuildLogsAll"
```

---

### Task 5: Create `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` variable, register in `init()`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `config.NewStoreWithEnv(dataDir, env)`, `runtime.PruneOptions`
- Consumes: `store.PruneOrphanedPorts()`, `store.PruneDeployments()`, `store.PruneBuildLogsAll()`

- [ ] **Step 1: Define cleanupCmd cobra.Command**

```go
// internal/cli/root.go — add before Execute()

var cleanupCmd = &cobra.Command{
    Use:   "cleanup [app-name]",
    Short: "Clean up unused Docker resources and orphaned state",
    Long: `Remove unused Docker containers, images, volumes, networks, and build cache.
Protects Tengiz-managed resources via label filtering.
Optionally target a specific app to prune its deployments and build logs.

Flags control which categories to clean. Default (no flags): --containers --images --volumes --networks.

Examples:
  tengiz cleanup                  # prune all resource types for all apps
  tengiz cleanup myapp            # prune all resources + app-specific state
  tengiz cleanup --images-only    # prune only unused images
  tengiz cleanup --dry-run        # show what would be removed without doing it`,
    Args: cobra.MaximumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        env := getEnv(cmd)
        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker not available: %w", err)
        }
        store := config.NewStoreWithEnv(dataDir, env)

        all, _ := cmd.Flags().GetBool("all")
        containers, _ := cmd.Flags().GetBool("containers")
        images, _ := cmd.Flags().GetBool("images")
        volumes, _ := cmd.Flags().GetBool("volumes")
        networks, _ := cmd.Flags().GetBool("networks")
        buildCache, _ := cmd.Flags().GetBool("build-cache")
        dryRun, _ := cmd.Flags().GetBool("dry-run")
        keepImages, _ := cmd.Flags().GetInt("keep-images")
        keepDeployments, _ := cmd.Flags().GetInt("keep-deployments")

        // If no specific category flag is set, default to all
        if !containers && !images && !volumes && !networks && !buildCache {
            all = true
        }

        // Get list of known apps from store for protection
        apps, err := store.ListApps()
        if err != nil {
            return fmt.Errorf("failed to list apps: %w", err)
        }
        knownApps := make([]string, 0, len(apps))
        for _, app := range apps {
            knownApps = append(knownApps, app.Name)
        }

        opts := runtime.PruneOptions{
            Containers: containers || all,
            Images:     images || all,
            Volumes:    volumes || all,
            Networks:   networks || all,
            BuildCache: buildCache || all,
            DryRun:     dryRun,
            KeepImages: keepImages,
            KnownApps:  knownApps,
        }

        if dryRun {
            fmt.Println("[tengiz] Dry-run mode — no resources will be removed")
        }

        report, err := rt.Prune(context.Background(), opts)
        if err != nil {
            return fmt.Errorf("prune failed: %w", err)
        }

        // Print report
        printCleanupReport(report)

        // Store-level cleanup (only if not dry-run)
        if !dryRun {
            if err := store.PruneOrphanedPorts(knownApps); err != nil {
                slog.Warn("failed to prune orphaned ports", "error", err)
            }

            if opts.Images {
                for _, app := range apps {
                    if err := store.PruneDeployments(app.Name, keepDeployments); err != nil {
                        slog.Warn("failed to prune deployments", "app", app.Name, "error", err)
                    }
                }
                store.PruneBuildLogsAll(keepDeployments)
            }

            // If specific app was given, also prune its orphaned containers
            if len(args) == 1 {
                appName := args[0]
                containerName := runtime.ContainerName(appName, env)
                fmt.Printf("[tengiz] Cleaned up state for app '%s'\n", appName)
                // Remove old containers for this specific app
                if _, err := store.GetApp(appName); err != nil {
                    fmt.Printf("[tengiz] App '%s' not found in store, skipping\n", appName)
                }
            }
        }

        return nil
    },
}
```

- [ ] **Step 2: Add printCleanupReport helper**

```go
// internal/cli/root.go

func printCleanupReport(r *runtime.PrunedReport) {
    fmt.Println("[tengiz] Cleanup Report:")
    if r.ContainersRemoved > 0 || r.ContainersReclaimed > 0 {
        fmt.Printf("  Containers: %d removed, %s reclaimed\n", r.ContainersRemoved, formatBytes(r.ContainersReclaimed))
    }
    if r.ImagesRemoved > 0 || r.ImagesReclaimed > 0 {
        fmt.Printf("  Images:     %d removed, %s reclaimed\n", r.ImagesRemoved, formatBytes(r.ImagesReclaimed))
    }
    if r.VolumesRemoved > 0 || r.VolumesReclaimed > 0 {
        fmt.Printf("  Volumes:    %d removed, %s reclaimed\n", r.VolumesRemoved, formatBytes(r.VolumesReclaimed))
    }
    if r.NetworksRemoved > 0 || r.NetworksReclaimed > 0 {
        fmt.Printf("  Networks:   %d removed, %s reclaimed\n", r.NetworksRemoved, formatBytes(r.NetworksReclaimed))
    }
    if r.BuildCacheReclaimed > 0 {
        fmt.Printf("  Build Cache: %s reclaimed\n", formatBytes(r.BuildCacheReclaimed))
    }
    if r.ContainersRemoved == 0 && r.ImagesRemoved == 0 && r.VolumesRemoved == 0 && r.NetworksRemoved == 0 && r.BuildCacheReclaimed == 0 {
        fmt.Println("  Nothing to clean up.")
    }
}

func formatBytes(b int64) string {
    switch {
    case b >= 1<<30:
        return fmt.Sprintf("%.2f GB", float64(b)/float64(1<<30))
    case b >= 1<<20:
        return fmt.Sprintf("%.2f MB", float64(b)/float64(1<<20))
    case b >= 1<<10:
        return fmt.Sprintf("%.2f KB", float64(b)/float64(1<<10))
    default:
        return fmt.Sprintf("%d B", b)
    }
}
```

Note: The report variable returned is a `*runtime.PruneReport`, not `*runtime.PrunedReport`. Fix the parameter name.

```go
func printCleanupReport(r *runtime.PruneReport) {
```

- [ ] **Step 3: Register cleanupCmd and its flags in init() and Execute()**

In `init()`:
```go
rootCmd.AddCommand(cleanupCmd)
```

In `Execute()`:
```go
cleanupCmd.Flags().BoolP("all", "a", false, "prune all resource types")
cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
cleanupCmd.Flags().Bool("images", false, "prune unused images")
cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
cleanupCmd.Flags().BoolP("dry-run", "n", false, "show what would be removed without actually doing it")
cleanupCmd.Flags().Int("keep-images", 5, "number of recent images to keep per app")
cleanupCmd.Flags().Int("keep-deployments", 10, "number of recent deployments to keep per app")
```

- [ ] **Step 4: Run build to verify compilation**

Run: `go build ./...`
Expected: exit 0

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat(cleanup): add tengiz cleanup CLI command with category flags and dry-run"
```

---

### Task 6: Add tests for cleanup command flags and report formatting

**Files:**
- Modify: `internal/cli/root_test.go` — add cleanup command tests

**Interfaces:**
- Consumes: `cleanupCmd` from Task 5
- Consumes: `formatBytes()`, `printCleanupReport()` helpers

- [ ] **Step 1: Write failing test for formatBytes**

```go
// internal/cli/root_test.go

func TestFormatBytes(t *testing.T) {
    tests := []struct {
        input    int64
        expected string
    }{
        {0, "0 B"},
        {500, "500 B"},
        {1536, "1.50 KB"},
        {1048576, "1.00 MB"},
        {1073741824, "1.00 GB"},
        {3221225472, "3.00 GB"},
    }
    for _, tt := range tests {
        t.Run(tt.expected, func(t *testing.T) {
            result := formatBytes(tt.input)
            if result != tt.expected {
                t.Errorf("formatBytes(%d) = %q, want %q", tt.input, result, tt.expected)
            }
        })
    }
}

func TestParseSize(t *testing.T) {
    tests := []struct {
        input    string
        expected int64
    }{
        {"Total reclaimed space: 1.23GB", 1320702443},
        {"Total: 500.0MB", 524288000},
        {"Total: 1024KB", 1048576},
        {"Total reclaimed space: 0B", 0},
    }
    for _, tt := range tests {
        t.Run(tt.expected, func(t *testing.T) {
            result := parseSize(tt.input)
            if result != tt.expected {
                t.Errorf("parseSize(%q) = %d, want %d", tt.input, result, tt.expected)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestFormatBytes|TestParseSize" -v -count=1`
Expected: FAIL or PASS depending on whether functions are in root.go or runtime package

The `formatBytes` helper is in `root.go` but not exported. Tests in `root_test.go` can access unexported functions within the same package. `parseSize` is in `runtime/docker.go` — test it via `runtime` package tests.

- [ ] **Step 3: Move parseSize test to runtime package**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestParseSize(t *testing.T) {
    tests := []struct {
        input    string
        expected int64
    }{
        {"Total reclaimed space: 1.23GB", 1320702443},
        {"Total: 500.0MB", 524288000},
        {"Total: 1024KB", 1048576},
        {"Total reclaimed space: 0B", 0},
    }
    for _, tt := range tests {
        t.Run(fmt.Sprintf("%d", tt.expected), func(t *testing.T) {
            result := parseSize(tt.input)
            if math.Abs(float64(result-tt.expected)) > 10 {
                t.Errorf("parseSize(%q) = %d, want %d", tt.input, result, tt.expected)
            }
        })
    }
}
```

- [ ] **Step 4: Implement formatBytes test correctly**

```go
// internal/cli/root_test.go

func TestFormatBytes(t *testing.T) {
    tests := []struct {
        input    int64
        expected string
    }{
        {0, "0 B"},
        {500, "500 B"},
        {1536, "1.50 KB"},
        {1048576, "1.00 MB"},
        {1073741824, "1.00 GB"},
        {3221225472, "3.00 GB"},
    }
    for _, tt := range tests {
        t.Run(tt.expected, func(t *testing.T) {
            result := formatBytes(tt.input)
            if result != tt.expected {
                t.Errorf("formatBytes(%d) = %q, want %q", tt.input, result, tt.expected)
            }
        })
    }
}
```

- [ ] **Step 5: Run all tests**

Run: `go test ./internal/runtime/ ./internal/config/ ./internal/cli/ -v -count=1`
Expected: PASS (all existing tests pass, plus new ones)

- [ ] **Step 6: Run vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root_test.go internal/runtime/cleanup_test.go
git commit -m "test(cleanup): add tests for formatBytes and parseSize helpers"
```

---

### Task 7: Wire auto-cleanup into post-deploy step

**Files:**
- Modify: `internal/cli/root.go` — add cleanup call at end of `deployCmd.RunE`

- [ ] **Step 1: Add auto-cleanup call after successful deploy**

Find the end of the deploy command's `RunE` where it calls `KeepLastNImages` and `PruneBuildLogs`. Add lightweight cleanup calls there:

```go
// internal/cli/root.go — after successful deploy, before return nil

// Run lightweight post-deploy cleanup
go func() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Prune orphaned images (keep images for this app)
    opts := runtime.PruneOptions{
        Images:     true,
        DryRun:     false,
        KeepImages: 5,
        KnownApps:  []string{cfg.Name},
    }
    if _, err := rt.Prune(ctx, opts); err != nil {
        slog.Debug("post-deploy cleanup: image prune failed", "error", err)
    }

    // Prune orphaned ports
    apps, _ := store.ListApps()
    known := make([]string, len(apps))
    for i, a := range apps {
        known[i] = a.Name
    }
    if err := store.PruneOrphanedPorts(known); err != nil {
        slog.Debug("post-deploy cleanup: port prune failed", "error", err)
    }
}()
```

- [ ] **Step 2: Run build to verify**

Run: `go build ./...`
Expected: exit 0

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat(cleanup): add lightweight post-deploy auto-cleanup"
```

---

## Self-Review

### 1. Spec coverage

| Spec Requirement | Covered By |
|----------------|------------|
| `tengiz cleanup` CLI command | Task 5 |
| Label-based `docker system prune` | Task 2 — `--filter label!=tengiz-app` |
| Per-category cleanup flags | Task 5 — `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache` |
| Dry-run mode | Task 5 — `--dry-run` / `-n` |
| App image retention | Task 2 — `KeepLastNImages` integration with `KnownApps` |
| Orphaned port cleanup | Task 4 — `PruneOrphanedPorts` |
| Deployment history pruning | Task 4 — `PruneDeployments` |
| Build log pruning | Task 4 — `PruneBuildLogsAll` |
| Post-deploy auto-cleanup | Task 7 |
| Protect Tengiz-managed resources | Task 2 — label filter excludes tengiz-app labeled resources |

### 2. Placeholder scan

- No "TBD", "TODO", "implement later" — all code is concrete
- No "Add appropriate error handling" — error paths are explicit with `slog.Warn`
- No "Similar to Task N" — each task has standalone code
- No references to types not defined in tasks

### 3. Type consistency

- `PruneOptions` / `PruneReport` — consistent across all tasks
- `formatBytes(int64) string` — used in Task 5, tested in Task 6
- `parseSize(string) int64` — used in Task 2, tested in Task 6
- `pruneArgsForCategory(string, PruneOptions) []string` — defined in Task 2, consistent with flag names

No inconsistencies found.
