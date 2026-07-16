# Rollback System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to revert a deployment to a previous version with a single `tengiz rollback` command.

**Architecture:** Each deploy creates a unique image tag (`tengiz-apps/<app>:<unix-timestamp>`) plus a `latest` alias. Deployment history is already persisted in `~/.tengiz/deployments.json` (max 10 entries). The rollback command reads the previous active deployment from history, starts a container from that image on a new port, updates the proxy route, and marks the current deployment as rolled back.

**Tech Stack:** Go, Docker CLI (`os/exec`), JSON file-based state, existing `Store`/`runtime.Manager`/`proxy` packages.

## Global Constraints

- Container names prefixed `tengiz-<appname>` and `tengiz-<appname>-<suffix>`
- Port allocations: 9000-9999, persisted in `~/.tengiz/ports.json`
- State persisted in `~/.tengiz/apps.json` and `~/.tengiz/deployments.json`
- Max 10 deployment entries per app kept in history
- Existing `DeploymentStatus` constants: `DeployActive`, `DeployPrevious`, `DeployRolled`
- All tests must pass: `go test ./... -v -count=1`

---
### Task 1: Builder - Versioned Image Tags

**Files:**
- Modify: `internal/builder/builder.go:38-47`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `Builder.Build(ctx, dir, appName, detection) -> (string, error)` — currently returns `tengiz-apps/<app>:latest`
- Produces: `Builder.Build(ctx, dir, appName, detection, deploymentID) -> (string, error)` — returns `tengiz-apps/<app>:<deploymentID>` after also tagging as `latest`

- [ ] **Step 1: Update `Build` signature to accept `deploymentID` parameter**

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, detection *Detection, deploymentID string) (string, error) {
```

- [ ] **Step 2: Update `buildWithDockerfile` to tag with both `<app>:<deploymentID>` and `<app>:latest`**

```go
func (b *Builder) buildWithDockerfile(ctx context.Context, dir string, appName string, deploymentID string) (string, error) {
    tag := fmt.Sprintf("tengiz-apps/%s:%s", appName, deploymentID)
    cmd := exec.CommandContext(ctx, "docker", "build", "-t", tag, dir)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("docker build: %w", err)
    }

    // Also tag as latest for convenience
    latestTag := fmt.Sprintf("tengiz-apps/%s:latest", appName)
    tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
    if out, err := tagCmd.CombinedOutput(); err != nil {
        return "", fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
    }

    return tag, nil
}
```

- [ ] **Step 3: Run tests to verify they fail (old callers don't pass deploymentID)**

Run: `go test ./internal/builder/... -v -count=1`
Expected: FAIL — compilation error in `builder.go` or callers

- [ ] **Step 4: Update callers of `Build` to pass `deploymentID`**

In `internal/cli/root.go`, both the first-deploy and zero-downtime paths call `b.Build(...)`. Generate a deployment ID before the build call:

```go
// At the top of deployCmd RunE, before the build call
deploymentID := fmt.Sprintf("%d", time.Now().Unix())
```

Then update the first-deploy path (~line 169):
```go
imageTag, err := b.Build(context.Background(), projectRoot, cfg.Name, detection, deploymentID)
```

And the zero-downtime path (the existing `deploymentID := fmt.Sprintf(...)` at line 215 can be moved before the build, and the build call updated).

**Important**: In the first-deploy path, also record the deployment with `store.AddDeployment` (currently only the zero-downtime path does this). Add after `store.SaveApp`:

```go
store.AddDeployment(cfg.Name, types.DeploymentEntry{
    ID:        deploymentID,
    ImageTag:  imageTag,
    Port:      port,
    CreatedAt: time.Now(),
    Status:    string(types.DeployActive),
})
```

- [ ] **Step 5: Write failing test for versioned build**

```go
func TestBuildWithDeploymentID(t *testing.T) {
    b := New(t.TempDir())
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)
    detection := &Detection{Framework: FrameworkStatic, InternalPort: 80}
    tag, err := b.Build(context.Background(), dir, "testapp", detection, "v123")
    if err != nil {
        t.Fatalf("Build() error = %v", err)
    }
    expected := "tengiz-apps/testapp:v123"
    if tag != expected {
        t.Errorf("tag = %q, want %q", tag, expected)
    }
}
```

Run: `go test ./internal/builder/... -v -count=1`
Expected: FAIL (docker not available in test env) — this is an integration test; skip with `*testing.Short()` check or mark as manual.

**Alternative:** Skip full Docker test; test only the `buildWithDockerfile` call structure by using `exec.Command` tracking (keep it simple — just test compilation).

- [ ] **Step 6: Verify compilation**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 7: Commit**

```bash
git add internal/builder/builder.go internal/cli/root.go internal/builder/builder_test.go
git commit -m "feat: add versioned image tagging to builder"
```

---
### Task 2: Store - Add Deployment Retrieval Methods

**Files:**
- Modify: `internal/config/store.go:288-310`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `Store.AddDeployment(appName, DeploymentEntry) error`, `Store.GetDeployments(appName) ([]DeploymentEntry, error)` — already exist
- Produces: `Store.GetPreviousDeployment(appName) (*DeploymentEntry, error)` — returns the most recent non-active deployment with status `previous`

- [ ] **Step 1: Add `GetPreviousDeployment` method to Store**

```go
func (s *Store) GetPreviousDeployment(appName string) (*types.DeploymentEntry, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    deployments := make(map[string][]types.DeploymentEntry)
    s.readJSON("deployments.json", &deployments)
    entries, ok := deployments[appName]
    if !ok || len(entries) == 0 {
        return nil, fmt.Errorf("no deployment history for app %q", appName)
    }

    // Scan most recent first for a previous (non-active, non-rolled) deployment
    for i := len(entries) - 1; i >= 0; i-- {
        if entries[i].Status == string(types.DeployPrevious) {
            return &entries[i], nil
        }
    }
    return nil, fmt.Errorf("no previous deployment found for app %q", appName)
}
```

- [ ] **Step 2: Write tests**

```go
func TestGetPreviousDeployment(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)

    // No history
    _, err := s.GetPreviousDeployment("testapp")
    if err == nil {
        t.Fatal("expected error for no history")
    }

    // Add entries: one active, one previous
    s.AddDeployment("testapp", types.DeploymentEntry{
        ID: "v1", ImageTag: "img:v1", Port: 9001,
        CreatedAt: time.Now(), Status: string(types.DeployPrevious),
    })
    s.AddDeployment("testapp", types.DeploymentEntry{
        ID: "v2", ImageTag: "img:v2", Port: 9002,
        CreatedAt: time.Now(), Status: string(types.DeployActive),
    })

    dep, err := s.GetPreviousDeployment("testapp")
    if err != nil {
        t.Fatalf("GetPreviousDeployment() error = %v", err)
    }
    if dep.ID != "v1" {
        t.Errorf("expected v1, got %s", dep.ID)
    }
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Add `GetDeploymentByID` method (needed by rollback command to handle 0-downtime deployments with DeploymentSuffix)**

```go
func (s *Store) GetDeploymentByID(appName, deploymentID string) (*types.DeploymentEntry, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    deployments := make(map[string][]types.DeploymentEntry)
    s.readJSON("deployments.json", &deployments)
    entries, ok := deployments[appName]
    if !ok {
        return nil, fmt.Errorf("no deployment history for app %q", appName)
    }
    for i := len(entries) - 1; i >= 0; i-- {
        if entries[i].ID == deploymentID {
            return &entries[i], nil
        }
    }
    return nil, fmt.Errorf("deployment %q not found for app %q", deploymentID, appName)
}
```

- [ ] **Step 5: Write test for `GetDeploymentByID`**

```go
func TestGetDeploymentByID(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    s.AddDeployment("testapp", types.DeploymentEntry{ID: "v1", ImageTag: "img:v1", Port: 9001, Status: string(types.DeployPrevious)})
    dep, err := s.GetDeploymentByID("testapp", "v1")
    if err != nil {
        t.Fatalf("GetDeploymentByID() error = %v", err)
    }
    if dep.ID != "v1" {
        t.Errorf("got %s, want v1", dep.ID)
    }
    _, err = s.GetDeploymentByID("testapp", "nonexistent")
    if err == nil {
        t.Fatal("expected error for nonexistent ID")
    }
}
```

- [ ] **Step 6: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add GetPreviousDeployment and GetDeploymentByID to store"
```

---
### Task 3: Runtime - Add CreateFromImage Method

**Files:**
- Modify: `internal/runtime/runtime.go:10-24` (add to Manager interface)
- Modify: `internal/runtime/runtime.go:28-82` (add stub implementation)
- Modify: `internal/runtime/docker.go` (add Docker implementation after Create)
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Produces: `Manager.CreateFromImage(ctx, cfg, imageTag, port) error` — creates a container from a specific image tag (not just latest) using the app's existing config

- [ ] **Step 1: Add `CreateFromImage` to Manager interface**

```go
type Manager interface {
    Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
    CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
    CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error
    // ... existing methods unchanged
}
```

- [ ] **Step 2: Add stub implementation**

```go
func (m *stubManager) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
    return nil
}
```

- [ ] **Step 3: Add Docker implementation** (identical to `Create` but takes exact imageTag)

```go
func (r *dockerRuntime) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
    internalPort := cfg.Port
    if internalPort == 0 {
        internalPort = 8080
    }
    containerName := fmt.Sprintf("tengiz-%s", cfg.Name)

    args := []string{
        "run", "-d",
        "--name", containerName,
        "--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
        "-p", fmt.Sprintf("127.0.0.1:%d:%d", port, internalPort),
        "--restart", "no",
    }
    args = append(args, envArgs(cfg.Env)...)
    args = append(args, resourceArgs(cfg.Resources)...)
    args = append(args, volumeArgs(cfg.Volumes)...)
    args = append(args, imageTag)
    cmd := exec.CommandContext(ctx, "docker", args...)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("docker create from image: %w\n%s", err, string(out))
    }
    return nil
}
```

- [ ] **Step 4: Write test for stub**

```go
func TestStubCreateFromImage(t *testing.T) {
    m := NewStub()
    cfg := &types.AppConfig{Name: "testapp", Port: 3000}
    err := m.CreateFromImage(context.Background(), cfg, "tengiz-apps/testapp:v1", 9001)
    if err != nil {
        t.Fatalf("CreateFromImage() error = %v", err)
    }
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat: add CreateFromImage to runtime Manager"
```

---
### Task 4: Rollback Logic in Config Store - UpdateDeploymentStatus

**Files:**
- Modify: `internal/config/store.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Produces: `Store.UpdateDeploymentStatus(appName, deploymentID, status) error` — updates the status of a specific deployment entry

- [ ] **Step 1: Add `UpdateDeploymentStatus` method**

```go
func (s *Store) UpdateDeploymentStatus(appName, deploymentID, status string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    deployments := make(map[string][]types.DeploymentEntry)
    s.readJSON("deployments.json", &deployments)
    entries := deployments[appName]
    found := false
    for i := range entries {
        if entries[i].ID == deploymentID {
            entries[i].Status = status
            found = true
            break
        }
    }
    if !found {
        return fmt.Errorf("deployment %q not found for app %q", deploymentID, appName)
    }
    deployments[appName] = entries
    return s.writeJSON("deployments.json", deployments)
}
```

- [ ] **Step 2: Write tests**

```go
func TestUpdateDeploymentStatus(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)

    s.AddDeployment("testapp", types.DeploymentEntry{
        ID: "v1", ImageTag: "img:v1", Port: 9001, Status: string(types.DeployActive),
    })

    if err := s.UpdateDeploymentStatus("testapp", "v1", string(types.DeployRolled)); err != nil {
        t.Fatalf("UpdateDeploymentStatus() error = %v", err)
    }

    deps, _ := s.GetDeployments("testapp")
    if deps[0].Status != string(types.DeployRolled) {
        t.Errorf("status = %q, want %q", deps[0].Status, types.DeployRolled)
    }

    // Non-existent deployment
    err := s.UpdateDeploymentStatus("testapp", "v999", "rolled")
    if err == nil {
        t.Fatal("expected error for non-existent deployment")
    }
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add UpdateDeploymentStatus to store"
```

---
### Task 5: CLI - Add `tengiz rollback` Command

**Files:**
- Modify: `internal/cli/root.go` (add new command + wire it to root)
- Test: (manual testing with actual Docker)

**Interfaces:**
- Consumes: `Store.GetApp()`, `Store.GetPreviousDeployment()`, `Store.UpdateDeploymentStatus()`, `Store.SaveApp()`, `Store.AllocatePort()`, `Store.FreePort()`, `runtime.Manager.CreateFromImage()`, `runtime.Manager.Remove()`, `proxy.RegisterRouteWithProxy()`

- [ ] **Step 1: Add `rollbackCmd` variable and register it**

```go
var rollbackCmd = &cobra.Command{
    Use:   "rollback <app>",
    Short: "Rollback to the previous deployment",
    Long:  "Reverses the most recent deployment. The previous active container is started and the current one is stopped.",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        store := config.NewStore(dataDir)

        // Get current app entry
        app, err := store.GetApp(appName)
        if err != nil {
            return fmt.Errorf("app %q not found: %w", appName, err)
        }

        // Find previous deployment
        prevDep, err := store.GetPreviousDeployment(appName)
        if err != nil {
            return fmt.Errorf("no previous deployment found: %w", err)
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        // Allocate a new port for the rollback container
        newPort, err := store.AllocatePort(appName)
        if err != nil {
            return fmt.Errorf("port allocation: %w", err)
        }

        // Create container from the previous image
        if err := rt.CreateFromImage(cmd.Context(), &app.Config, prevDep.ImageTag, newPort); err != nil {
            store.FreePort(newPort)
            return fmt.Errorf("create rollback container: %w", err)
        }

        // Wait for the container to be ready
        if err := rt.WaitForReady(cmd.Context(), appName, app.Config.Port); err != nil {
            log.Printf("[tengiz] warning: rollback container may not be ready: %v", err)
        }

        // Update proxy route to new port
        if err := proxy.RegisterRouteWithProxy(appName, newPort); err != nil {
            log.Printf("[tengiz] proxy not available: %v", err)
        }

        // Remove the current container
        if app.DeploymentSuffix != "" {
            if err := rt.RemoveBySuffix(cmd.Context(), appName, app.DeploymentSuffix); err != nil {
                log.Printf("[tengiz] warning: failed to remove current container: %v", err)
            }
        } else {
            if err := rt.Remove(cmd.Context(), appName); err != nil {
                log.Printf("[tengiz] warning: failed to remove current container: %v", err)
            }
        }

        // Free old port
        store.FreePort(app.Port)

        // Mark current deployment as rolled
        if app.DeploymentSuffix != "" {
            store.UpdateDeploymentStatus(appName, app.DeploymentSuffix, string(types.DeployRolled))
        }

        // Mark previous deployment as active
        store.UpdateDeploymentStatus(appName, prevDep.ID, string(types.DeployActive))

        // Update AppEntry
        store.SaveApp(types.AppEntry{
            Name:             app.Name,
            ImageTag:         prevDep.ImageTag,
            Port:             newPort,
            Domains:          app.Domains,
            Config:           app.Config,
            DeploymentSuffix: prevDep.ID,
        })

        fmt.Printf("[tengiz] rolled back %s to deployment %s (port %d)\n", appName, prevDep.ID, newPort)
        return nil
    },
}
```

- [ ] **Step 2: Register command with root**

In `init()` function, add:
```go
rootCmd.AddCommand(rollbackCmd)
```

- [ ] **Step 3: Verify compilation**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add tengiz rollback command"
```

---
### Task 6: Image Retention and Cleanup

**Files:**
- Create: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Produces: `Manager.RemoveImage(ctx, imageTag) error`, `Manager.KeepLastNImages(ctx, appName, n) error`

- [ ] **Step 1: Add `RemoveImage` and `KeepLastNImages` to Manager interface**

```go
type Manager interface {
    // ... existing methods
    RemoveImage(ctx context.Context, imageTag string) error
    KeepLastNImages(ctx context.Context, appName string, n int) error
}
```

- [ ] **Step 2: Add stub implementations**

```go
func (m *stubManager) RemoveImage(ctx context.Context, imageTag string) error {
    return nil
}
func (m *stubManager) KeepLastNImages(ctx context.Context, appName string, n int) error {
    return nil
}
```

- [ ] **Step 3: Add Docker implementations**

```go
func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
    cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
    }
    return nil
}

func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
    // List all images with the app prefix, sorted by creation time
    cmd := exec.CommandContext(ctx, "docker", "images",
        "--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
        "--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
    )
    out, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("docker images: %w", err)
    }

    lines := strings.Split(strings.TrimSpace(string(out)), "\n")
    if len(lines) <= n {
        return nil // Nothing to prune
    }

    // Sort by creation time (line format: tag|timestamp)
    sort.Slice(lines, func(i, j int) bool {
        partsI := strings.SplitN(lines[i], "|", 2)
        partsJ := strings.SplitN(lines[j], "|", 2)
        if len(partsI) < 2 || len(partsJ) < 2 {
            return false
        }
        return partsI[1] < partsJ[1]
    })

    // Remove all but the newest n images (skip latest tag which is an alias)
    for i := 0; i < len(lines)-n; i++ {
        parts := strings.SplitN(lines[i], "|", 2)
        if len(parts) < 1 {
            continue
        }
        tag := parts[0]
        if strings.HasSuffix(tag, ":latest") {
            continue // Don't remove the latest alias
        }
        if err := r.RemoveImage(ctx, tag); err != nil {
            log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
        }
    }
    return nil
}
```

- [ ] **Step 4: Write stub tests**

```go
func TestStubRemoveImage(t *testing.T) {
    m := NewStub()
    if err := m.RemoveImage(context.Background(), "tengiz-apps/testapp:v1"); err != nil {
        t.Fatalf("RemoveImage() error = %v", err)
    }
}
func TestStubKeepLastNImages(t *testing.T) {
    m := NewStub()
    if err := m.KeepLastNImages(context.Background(), "testapp", 5); err != nil {
        t.Fatalf("KeepLastNImages() error = %v", err)
    }
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Hook `KeepLastNImages` into deploy command**

In `internal/cli/root.go`, at the end of the zero-downtime deploy path (after `SaveApp`), add:
```go
// Prune old images — keep last 5
if err := rt.KeepLastNImages(context.Background(), cfg.Name, 5); err != nil {
    log.Printf("[tengiz] warning: image cleanup: %v", err)
}
```

Also add to the first-deploy path.

- [ ] **Step 7: Verify compilation**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/runtime/runtime.go internal/runtime/docker.go internal/cli/root.go
git commit -m "feat: add image retention and cleanup for rollback"
```

---
### Task 7: Integration — Wire Up Rollback in Deploy Pipeline

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/builder/builder.go` (already done in Task 1)

**No separate code step.** This task is a checklist to verify the full deploy → rollback workflow compiles and passes review.

- [ ] **Step 1: Verify full build**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 3: Code review pass on the full diff**

Key things to verify:
1. The first-deploy path records a deployment entry (Task 1 Step 4)
2. Image tags are unique timestamps, not just `latest`
3. `rollback` correctly uses `GetPreviousDeployment` to find the right target
4. Old ports are freed, new ports are allocated
5. Proxy route is updated to point to the rollback container
6. Deployment statuses are updated correctly (current → rolled, previous → active)
7. Image cleanup keeps last N and doesn't remove `latest` alias

- [ ] **Step 4: Commit any remaining fixes**

```bash
git add -A
git commit -m "fix: wire up rollback system in deploy pipeline"
```

---
### Task 8: Documentation and README Update

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md` (if needed)

- [ ] **Step 1: Add rollback usage to README**

Add to the Commands section in README.md:
```
tengiz rollback <app>  → rollback to the previous deployment
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add rollback command to README"
```
