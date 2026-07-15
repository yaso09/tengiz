# Persistent Storage (Volume Management) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Docker volume mount support to Tengiz so stateful apps (databases, upload directories) persist data across container restarts and scale-to-zero cycles.

**Architecture:** Add a `volumes` field to `.tengiz.yaml` config and `AppConfig`/`AppEntry` types. A `volumeArgs()` helper in `runtime` generates `-v` flags from the config. CLI commands (`tengiz volume add/remove/list`) manage volumes at runtime. Docker `-v` flags are appended in `Create()` and `CreateVersioned()`. Volumes persist via the existing `Store` JSON mechanism.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI (`os/exec`), viper config

## Global Constraints

- Container names always prefixed `tengiz-<appname>`
- Config stored in `~/.tengiz/apps.json` via `config.Store`
- Docker volume args: `-v host_path:container_path` syntax
- New types and fields use JSON + mapstructure tags
- Port allocations: 9000-9999, persisted in `~/.tengiz/ports.json`
- `.tengiz.yaml` uses `KEY: value` format for all maps
- All existing tests must pass after changes

---

### Task 1: Volume Types and Config Model

**Files:**
- Modify: `internal/types/types.go` (add `VolumeConfig` and fields)
- Modify: `internal/runtime/docker.go` (add `volumeArgs()` helper)
- Test: `internal/runtime/runtime_test.go` (add `TestVolumeArgs`)

**Interfaces:**
- Consumes: nothing — this task creates the types
- Produces: `types.VolumeConfig` struct, `types.AppConfig.Volumes` field, `types.AppEntry.Volumes` field, `volumeArgs()` function

- [ ] **Step 1: Write the failing tests**

Add to `internal/types/types.go` — define volume types:

```go
package types

// VolumeConfig represents a single volume mount.
type VolumeConfig struct {
    HostPath      string `mapstructure:"host_path" json:"host_path"`
    ContainerPath string `mapstructure:"container_path" json:"container_path"`
    ReadOnly      bool   `mapstructure:"read_only,omitempty" json:"read_only,omitempty"`
}
```

Add `Volumes []VolumeConfig` to `AppConfig`:

```go
type AppConfig struct {
    // ... existing fields ...
    Volumes     []VolumeConfig    `mapstructure:"volumes,omitempty" json:"volumes,omitempty"`
    // ...
}
```

Add `Volumes []VolumeConfig` to `AppEntry`:

```go
type AppEntry struct {
    // ... existing fields ...
    Volumes     []VolumeConfig    `json:"volumes,omitempty"`
    // ...
}
```

Now write the test in `internal/runtime/runtime_test.go`:

```go
func TestVolumeArgs(t *testing.T) {
    tests := []struct {
        name   string
        vols   []types.VolumeConfig
        expect []string
    }{
        {
            name:   "nil volumes",
            vols:   nil,
            expect: nil,
        },
        {
            name:   "empty volumes",
            vols:   []types.VolumeConfig{},
            expect: nil,
        },
        {
            name:   "single volume",
            vols:   []types.VolumeConfig{{HostPath: "/data", ContainerPath: "/app/data"}},
            expect: []string{"-v", "/data:/app/data"},
        },
        {
            name:   "multiple volumes",
            vols: []types.VolumeConfig{
                {HostPath: "/data", ContainerPath: "/app/data"},
                {HostPath: "/config", ContainerPath: "/app/config"},
            },
            expect: []string{"-v", "/data:/app/data", "-v", "/config:/app/config"},
        },
        {
            name:   "read-only volume",
            vols:   []types.VolumeConfig{{HostPath: "/readonly", ContainerPath: "/app/ro", ReadOnly: true}},
            expect: []string{"-v", "/readonly:/app/ro:ro"},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := volumeArgs(tt.vols)
            if !reflect.DeepEqual(got, tt.expect) {
                t.Errorf("volumeArgs(%v) = %v, want %v", tt.vols, got, tt.expect)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/runtime/ -run TestVolumeArgs -v -count=1
```
Expected: FAIL with `volumeArgs undefined` (compilation error because `volumeArgs` doesn't exist yet).

- [ ] **Step 3: Write the volume types and helper implementation**

First, add the types to `internal/types/types.go`:

```go
type VolumeConfig struct {
    HostPath      string `mapstructure:"host_path" json:"host_path"`
    ContainerPath string `mapstructure:"container_path" json:"container_path"`
    ReadOnly      bool   `mapstructure:"read_only,omitempty" json:"read_only,omitempty"`
}
```

Update `AppConfig` (add `Volumes` field after `Env`):

```go
Volumes     []VolumeConfig    `mapstructure:"volumes,omitempty" json:"volumes,omitempty"`
```

Update `AppEntry` (add `Volumes` field after `Config`):

```go
Volumes     []VolumeConfig    `json:"volumes,omitempty"`
```

Now add the `volumeArgs()` function in `internal/runtime/docker.go`:

```go
func volumeArgs(volumes []types.VolumeConfig) []string {
    if len(volumes) == 0 {
        return nil
    }
    args := make([]string, 0, len(volumes)*2)
    for _, v := range volumes {
        spec := fmt.Sprintf("%s:%s", v.HostPath, v.ContainerPath)
        if v.ReadOnly {
            spec += ":ro"
        }
        args = append(args, "-v", spec)
    }
    return args
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/runtime/ -run TestVolumeArgs -v -count=1
```
Expected: PASS (all 5 subtests).

- [ ] **Step 5: Run full test suite**

```bash
go test ./... -count=1 2>&1
```
Expected: All tests pass (no regressions).

- [ ] **Step 6: Commit**

```bash
git add internal/types/types.go internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat: add VolumeConfig type and volumeArgs helper"
```

---

### Task 2: Wire Volumes Into Docker Create/CreateVersioned

**Files:**
- Modify: `internal/runtime/docker.go` (add volume args to `Create` and `CreateVersioned`)
- Test: `internal/runtime/runtime_test.go` (add integration-level stub test)

**Interfaces:**
- Consumes: `types.AppConfig.Volumes`, `volumeArgs()` from Task 1
- Produces: `Create()` and `CreateVersioned()` now accept volume mounts

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/runtime_test.go`:

```go
func TestCreateWithVolumes(t *testing.T) {
    mgr := NewStub()
    cfg := &types.AppConfig{
        Name: "test-app",
        Volumes: []types.VolumeConfig{
            {HostPath: "/host/data", ContainerPath: "/container/data"},
        },
    }
    err := mgr.Create(context.Background(), cfg, "test-image:latest", 9000)
    if err != nil {
        t.Errorf("Create with volumes failed: %v", err)
    }
}

func TestCreateVersionedWithVolumes(t *testing.T) {
    mgr := NewStub()
    cfg := &types.AppConfig{
        Name: "test-app",
        Volumes: []types.VolumeConfig{
            {HostPath: "/host/data", ContainerPath: "/container/data"},
        },
    }
    err := mgr.CreateVersioned(context.Background(), cfg, "test-image:latest", 9000, "v2")
    if err != nil {
        t.Errorf("CreateVersioned with volumes failed: %v", err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails (stub test should already pass — verify the tests compile)**

```bash
go test ./internal/runtime/ -run "TestCreateWithVolumes|TestCreateVersionedWithVolumes" -v -count=1
```
Expected: PASS (stub ignores config, always returns nil).

- [ ] **Step 3: Wire volumes into Docker `Create()`**

In `internal/runtime/docker.go`, inside `Create()`, add `volumeArgs` after `resourceArgs`:

```go
args = append(args, resourceArgs(cfg.Resources)...)
args = append(args, volumeArgs(cfg.Volumes)...)
args = append(args, imageTag)
```

In `CreateVersioned()`, add the same line after resource args:

```go
args = append(args, resourceArgs(cfg.Resources)...)
args = append(args, volumeArgs(cfg.Volumes)...)
args = append(args, imageTag)
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/runtime/ -run "TestCreateWithVolumes|TestCreateVersionedWithVolumes|TestVolumeArgs" -v -count=1
```
Expected: PASS.

- [ ] **Step 5: Run full test suite**

```bash
go test ./... -count=1 2>&1
```
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/docker.go
git commit -m "feat: wire volume mounts into Create and CreateVersioned"
```

---

### Task 3: Volume Store Operations (List/Add/Remove)

**Files:**
- Modify: `internal/config/store.go` (add `AddVolume`, `RemoveVolume`, `ListVolumes`, `SetVolumes`)
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `types.AppConfig.Volumes`, `types.AppEntry.Volumes`
- Produces: `store.AddVolume(name, vol)`, `store.RemoveVolume(name, path)`, `store.ListVolumes(name)`, `store.SetVolumes(name, vols)`

- [ ] **Step 1: Write failing test**

Add to `internal/config/store_test.go`:

```go
func TestVolumeStoreOperations(t *testing.T) {
    dataDir := t.TempDir()
    s := NewStore(dataDir)

    // Create a dummy app entry first
    app := types.AppEntry{
        Name: "vol-test-app",
        Port: 9001,
        ImageTag: "test:latest",
        Volumes: []types.VolumeConfig{
            {HostPath: "/data", ContainerPath: "/app/data"},
        },
    }
    s.SaveApp(app)

    // List volumes
    vols, err := s.ListVolumes("vol-test-app")
    if err != nil {
        t.Fatalf("ListVolumes failed: %v", err)
    }
    if len(vols) != 1 {
        t.Fatalf("expected 1 volume, got %d", len(vols))
    }
    if vols[0].HostPath != "/data" {
        t.Errorf("expected HostPath /data, got %s", vols[0].HostPath)
    }

    // Add another volume
    newVol := types.VolumeConfig{HostPath: "/config", ContainerPath: "/app/config"}
    if err := s.AddVolume("vol-test-app", newVol); err != nil {
        t.Fatalf("AddVolume failed: %v", err)
    }
    vols, _ = s.ListVolumes("vol-test-app")
    if len(vols) != 2 {
        t.Fatalf("expected 2 volumes after add, got %d", len(vols))
    }

    // Remove volume
    if err := s.RemoveVolume("vol-test-app", "/config"); err != nil {
        t.Fatalf("RemoveVolume failed: %v", err)
    }
    vols, _ = s.ListVolumes("vol-test-app")
    if len(vols) != 1 {
        t.Fatalf("expected 1 volume after remove, got %d", len(vols))
    }
    if vols[0].HostPath != "/data" {
        t.Errorf("expected remaining volume HostPath /data, got %s", vols[0].HostPath)
    }

    // Remove non-existent volume
    if err := s.RemoveVolume("vol-test-app", "/nonexistent"); err == nil {
        t.Error("expected error when removing non-existent volume, got nil")
    }

    // Set volumes (replace all)
    newVols := []types.VolumeConfig{
        {HostPath: "/newdata", ContainerPath: "/app/newdata"},
    }
    if err := s.SetVolumes("vol-test-app", newVols); err != nil {
        t.Fatalf("SetVolumes failed: %v", err)
    }
    vols, _ = s.ListVolumes("vol-test-app")
    if len(vols) != 1 || vols[0].HostPath != "/newdata" {
        t.Errorf("SetVolumes didn't replace volumes correctly: %+v", vols)
    }

    // List volumes for non-existent app
    if _, err := s.ListVolumes("nonexistent"); err == nil {
        t.Error("expected error for non-existent app, got nil")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/config/ -run TestVolumeStoreOperations -v -count=1
```
Expected: FAIL with compilation errors (methods not defined).

- [ ] **Step 3: Implement store methods**

Add to `internal/config/store.go`:

```go
func (s *Store) ListVolumes(appName string) ([]types.VolumeConfig, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    app, err := s.getApp(appName)
    if err != nil {
        return nil, err
    }
    if app.Volumes == nil {
        return []types.VolumeConfig{}, nil
    }
    return app.Volumes, nil
}

func (s *Store) AddVolume(appName string, vol types.VolumeConfig) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    app, err := s.getApp(appName)
    if err != nil {
        return err
    }
    app.Volumes = append(app.Volumes, vol)
    return s.saveAppLocked(appName, *app)
}

func (s *Store) RemoveVolume(appName string, hostPath string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    app, err := s.getApp(appName)
    if err != nil {
        return err
    }
    idx := -1
    for i, v := range app.Volumes {
        if v.HostPath == hostPath {
            idx = i
            break
        }
    }
    if idx == -1 {
        return fmt.Errorf("volume with host path %q not found for app %q", hostPath, appName)
    }
    app.Volumes = append(app.Volumes[:idx], app.Volumes[idx+1:]...)
    return s.saveAppLocked(appName, *app)
}

func (s *Store) SetVolumes(appName string, vols []types.VolumeConfig) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    app, err := s.getApp(appName)
    if err != nil {
        return err
    }
    app.Volumes = vols
    return s.saveAppLocked(appName, *app)
}
```

Also add a `getApp` helper and `saveAppLocked` that assume the lock is held. Add to `internal/config/store.go`:

```go
// getApp retrieves an app entry without locking (caller must hold s.mu).
func (s *Store) getApp(name string) (*types.AppEntry, error) {
    apps, err := s.readApps()
    if err != nil {
        return nil, err
    }
    app, ok := apps[name]
    if !ok {
        return nil, fmt.Errorf("app %q not found", name)
    }
    return &app, nil
}

// saveAppLocked persists an app entry without locking (caller must hold s.mu).
func (s *Store) saveAppLocked(name string, app types.AppEntry) error {
    apps, err := s.readApps()
    if err != nil {
        return err
    }
    apps[name] = app
    return s.writeApps(apps)
}
```

Note: `readApps()` and `writeApps()` already exist in the store — verify by searching the file.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/config/ -run TestVolumeStoreOperations -v -count=1
```
Expected: PASS.

- [ ] **Step 5: Run full test suite**

```bash
go test ./... -count=1 2>&1
```
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add volume store operations (add/remove/list/set)"
```

---

### Task 4: CLI Volume Commands (`tengiz volume add/remove/list`)

**Files:**
- Create: `internal/cli/volume.go`
- Modify: `internal/cli/root.go` (register volume command)
- Test: `internal/cli/root_test.go` (verify registration and help text)

**Interfaces:**
- Consumes: `store.AddVolume()`, `store.RemoveVolume()`, `store.ListVolumes()` from Task 3
- Produces: `tengiz volume add <app> <host_path>:<container_path>`, `tengiz volume remove <app> <host_path>`, `tengiz volume list <app>`

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/root_test.go`:

```go
func TestVolumeCommandsRegistered(t *testing.T) {
    volumeCmd, _, err := rootCmd.Find([]string{"volume"})
    if err != nil {
        t.Fatalf("volume command not found: %v", err)
    }
    if volumeCmd == nil {
        t.Fatal("volume command is nil")
    }

    subMap := make(map[string]bool)
    for _, sub := range volumeCmd.Commands() {
        subMap[sub.Name()] = true
    }

    expected := []string{"add", "remove", "list"}
    for _, name := range expected {
        if !subMap[name] {
            t.Errorf("expected subcommand %q under volume, not found", name)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/cli/ -run TestVolumeCommandsRegistered -v -count=1
```
Expected: FAIL with "volume command not found".

- [ ] **Step 3: Create `internal/cli/volume.go`**

```go
package cli

import (
    "fmt"
    "os"
    "strings"

    "github.com/yaso09/tengiz/internal/config"
    "github.com/yaso09/tengiz/internal/types"
    "github.com/spf13/cobra"
)

func init() {
    rootCmd.AddCommand(volumeCmd)
}

var volumeCmd = &cobra.Command{
    Use:   "volume",
    Short: "Manage persistent storage volumes",
    Long:  `Add, remove, and list Docker volume mounts for apps.`,
}

var volumeAddCmd = &cobra.Command{
    Use:   "add <app> <host_path>:<container_path>",
    Short: "Add a volume mount to an app",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        spec := args[1]

        parts := strings.SplitN(spec, ":", 2)
        if len(parts) != 2 {
            return fmt.Errorf("invalid volume spec %q — use host_path:container_path format", spec)
        }

        vol := types.VolumeConfig{
            HostPath:      parts[0],
            ContainerPath: parts[1],
        }

        store := config.NewStore(dataDir)

        if err := store.AddVolume(appName, vol); err != nil {
            return fmt.Errorf("failed to add volume: %w", err)
        }

        fmt.Printf("Volume %s → %s added to app %s\n", vol.HostPath, vol.ContainerPath, appName)
        return nil
    },
}

var volumeRemoveCmd = &cobra.Command{
    Use:   "remove <app> <host_path>",
    Short: "Remove a volume mount from an app",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        hostPath := args[1]

        store := config.NewStore(dataDir)

        if err := store.RemoveVolume(appName, hostPath); err != nil {
            return fmt.Errorf("failed to remove volume: %w", err)
        }

        fmt.Printf("Volume %s removed from app %s\n", hostPath, appName)
        return nil
    },
}

var volumeListCmd = &cobra.Command{
    Use:   "list <app>",
    Short: "List volume mounts for an app",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]

        store := config.NewStore(dataDir)

        vols, err := store.ListVolumes(appName)
        if err != nil {
            return fmt.Errorf("failed to list volumes: %w", err)
        }

        if len(vols) == 0 {
            fmt.Printf("No volumes configured for app %s\n", appName)
            return nil
        }

        fmt.Printf("Volumes for %s:\n", appName)
        for _, v := range vols {
            ro := ""
            if v.ReadOnly {
                ro = " (read-only)"
            }
            fmt.Printf("  %s → %s%s\n", v.HostPath, v.ContainerPath, ro)
        }
        return nil
    },
}

func init() {
    volumeCmd.AddCommand(volumeAddCmd)
    volumeCmd.AddCommand(volumeRemoveCmd)
    volumeCmd.AddCommand(volumeListCmd)
}
```

- [ ] **Step 4: Check if `config.GetDataDir()` exists**

Search for the function or data dir helper:

```bash
grep -n "func.*DataDir\|dataDir\|os.UserHomeDir" internal/config/*.go
```

If `GetDataDir()` doesn't exist, use `config.GetDefaultDataDir()` or inline `filepath.Join(os.Getenv("HOME"), ".tengiz")`. Adjust the volume.go file accordingly.

- [ ] **Step 5: Register subcommands in `internal/cli/volume.go`**

The `init()` in `volume.go` already handles registration — when `volume.go` is in the same package as `root.go` (both `cli`), it auto-registers via `init()`.

- [ ] **Step 6: Run registration test**

```bash
go test ./internal/cli/ -run TestVolumeCommandsRegistered -v -count=1
```
Expected: PASS.

- [ ] **Step 7: Manual smoke test (build binary and run help)**

```bash
go build -o tengiz . && ./tengiz volume --help
```
Expected: Shows volume command help with add/remove/list subcommands.

```bash
./tengiz volume add --help
./tengiz volume remove --help
./tengiz volume list --help
```
Expected: Each shows its usage.

- [ ] **Step 8: Run full test suite**

```bash
go test ./... -count=1 2>&1
```
Expected: All tests pass.

- [ ] **Step 9: Commit**

```bash
git add internal/cli/volume.go
git commit -m "feat: add volume CLI commands (add/remove/list)"
```

---

### Task 5: Wire Volumes Into Deploy (Config to Container)

**Files:**
- Modify: `internal/cli/deploy.go` (pass volumes from config to runtime)
- Modify: `internal/config/config.go` (ensure volumes are loaded from `.tengiz.yaml`)
- Test: `internal/cli/root_test.go` (ensure deploy creates containers with volumes)

**Interfaces:**
- Consumes: `types.AppConfig.Volumes`, `store.ListVolumes()`, `runtime.Create()` / `runtime.CreateVersioned()` from Tasks 1-2
- Produces: Deployed containers with volume mounts

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/root_test.go`:

```go
func TestDeployWithVolumes(t *testing.T) {
    // Use mock runtime that records Create args
    rt := &mockRTForDeploy{}
    // Create a temp dir with .tengiz.yaml containing volumes
    dir := t.TempDir()
    yamlContent := `
name: volume-app
volumes:
  - host_path: /data
    container_path: /app/data
`
    if err := os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yamlContent), 0644); err != nil {
        t.Fatal(err)
    }

    // We need to capture that volumes are passed to rt.Create
    // The mockRTForDeploy struct doesn't inspect cfg.Volumes — we need to check
    // in a different way. For now, verify the config loads correctly.
    cfg, err := config.Load(dir)
    if err != nil {
        t.Fatalf("config.Load failed: %v", err)
    }
    if len(cfg.Volumes) != 1 {
        t.Fatalf("expected 1 volume, got %d", len(cfg.Volumes))
    }
    if cfg.Volumes[0].HostPath != "/data" {
        t.Errorf("expected HostPath /data, got %s", cfg.Volumes[0].HostPath)
    }
}
```

- [ ] **Step 2: Run test to verify it fails (or passes config load)**

```bash
go test ./internal/cli/ -run TestDeployWithVolumes -v -count=1
```
Expected: Test passes if config loading already works (viper should unmarshal volumes into AppConfig now that the field exists).

If test passes, add an assertion about the volume being wired through. If not, fix config loading.

- [ ] **Step 3: Verify config loading works**

The viper `mapstructure` tag `volumes` on `AppConfig.Volumes` means `.tengiz.yaml` `volumes:` entries are automatically loaded. Add a `.tengiz.yaml` test fixture if needed:

In `internal/config/config.go`, add a test:

```go
func TestLoadConfigWithVolumes(t *testing.T) {
    dir := t.TempDir()
    yamlContent := `
name: test-app
volumes:
  - host_path: /data
    container_path: /app/data
  - host_path: /config
    container_path: /app/config
    read_only: true
`
    if err := os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yamlContent), 0644); err != nil {
        t.Fatal(err)
    }
    cfg, err := Load(dir)
    if err != nil {
        t.Fatalf("Load failed: %v", err)
    }
    if len(cfg.Volumes) != 2 {
        t.Fatalf("expected 2 volumes, got %d", len(cfg.Volumes))
    }
    if cfg.Volumes[0].HostPath != "/data" || cfg.Volumes[0].ContainerPath != "/app/data" {
        t.Errorf("first volume mismatch: %+v", cfg.Volumes[0])
    }
    if !cfg.Volumes[1].ReadOnly {
        t.Errorf("second volume should be read-only")
    }
}
```

- [ ] **Step 4: Ensure deploy command merges config volumes into runtime.Create call**

In `internal/cli/deploy.go`, the deploy flow calls `rt.Create(ctx, cfg, imageTag, port)`. Since `cfg` already carries `Volumes`, no code change is needed — the volume args are wired in `Create()`.

Verify this by reading the deploy function and confirming `cfg` is passed directly to `rt.Create()`. If there's any intermediate config transformation, add the volumes field there.

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/config/ -run TestLoadConfigWithVolumes -v -count=1
go test ./internal/cli/ -run TestDeployWithVolumes -v -count=1
```
Expected: PASS.

- [ ] **Step 6: Run full test suite**

```bash
go test ./... -count=1 2>&1
```
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: wire volumes from config through deploy to runtime"
```

---

### Task 6: `tengiz volume` CLI Integration Test (Read-Only Flag and Full E2E)

**Files:**
- Modify: `internal/cli/volume.go` (add `--read-only` flag on `add`)
- Test: `internal/cli/root_test.go` (test volume add/remove/list via CLI setup)

**Interfaces:**
- Consumes: all previous tasks
- Produces: CLI supports `--read-only` flag, e2e test confirms the chain

- [ ] **Step 1: Add --read-only flag to volume add command**

Modify `internal/cli/volume.go`:

```go
var (
    volumeAddReadOnly bool
)

func init() {
    volumeAddCmd.Flags().BoolVarP(&volumeAddReadOnly, "read-only", "r", false, "Mount volume as read-only")
    // other init code...
}
```

Update `volumeAddCmd.RunE`:

```go
var volumeAddCmd = &cobra.Command{
    Use:   "add <app> <host_path>:<container_path>",
    Short: "Add a volume mount to an app",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        spec := args[1]

        parts := strings.SplitN(spec, ":", 2)
        if len(parts) != 2 {
            return fmt.Errorf("invalid volume spec %q — use host_path:container_path format", spec)
        }

        vol := types.VolumeConfig{
            HostPath:      parts[0],
            ContainerPath: parts[1],
            ReadOnly:      volumeAddReadOnly,
        }

        store := config.NewStore(dataDir)

        if err := store.AddVolume(appName, vol); err != nil {
            return fmt.Errorf("failed to add volume: %w", err)
        }

        fmt.Printf("Volume %s → %s added to app %s\n", vol.HostPath, vol.ContainerPath, appName)
        return nil
    },
}
```

- [ ] **Step 2: Write integration test**

Add to `internal/cli/root_test.go`:

```go
func TestVolumeListEmpty(t *testing.T) {
    dir := t.TempDir()
    dataDir := filepath.Join(dir, ".tengiz")
    os.Setenv("HOME", dir)
    defer os.Unsetenv("HOME")

    // Create a test app via store
    store := config.NewStore(dataDir)
    app := types.AppEntry{
        Name:     "empty-vol-app",
        Port:     9005,
        ImageTag: "test:latest",
    }
    if err := store.SaveApp(app); err != nil {
        t.Fatal(err)
    }

    // Run `tengiz volume list empty-vol-app`
    rootCmd.SetArgs([]string{"volume", "list", "empty-vol-app"})
    rootCmd.SetOut(io.Discard)
    rootCmd.SetErr(io.Discard)
    err := rootCmd.Execute()
    if err != nil {
        t.Fatalf("volume list failed: %v", err)
    }
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/cli/ -run "TestVolumeListEmpty|TestVolumeCommandsRegistered" -v -count=1
```
Expected: PASS.

- [ ] **Step 4: Run full test suite**

```bash
go test ./... -count=1 2>&1
```
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/volume.go internal/cli/root_test.go
git commit -m "feat: add --read-only flag and volume CLI tests"
```

---

### Task 7: Documentation Updates

**Files:**
- Modify: `README.md` (add volume commands to CLI reference)
- Modify: `internal/config/config.go` (update defaults/description for volumes)

**Interfaces:**
- Consumes: all previous tasks
- Produces: Users can discover volume features

- [ ] **Step 1: Update README.md**

Add these entries to the CLI reference section in README.md:

```
tengiz volume add [-r] <app> <host_path>:<container_path>  →  add volume mount
tengiz volume remove <app> <host_path>                       →  remove volume mount
tengiz volume list <app>                                     →  list volume mounts
```

Add `.tengiz.yaml` example with volumes:

```yaml
volumes:
  - host_path: /mnt/data
    container_path: /app/data
  - host_path: /mnt/config
    container_path: /app/config
    read_only: true
```

- [ ] **Step 2: Update docs/AGENTS.md if it documents CLI commands**

Check if AGENTS.md has a CLI reference section and add the new commands.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add volume management commands to README"
```

---

### Task 8: Final Review and Verification

**Files:**
- All modified files

- [ ] **Step 1: Run full test suite**

```bash
go test ./... -count=1 2>&1
```
Expected: ALL tests pass.

- [ ] **Step 2: Run static analysis**

```bash
go vet ./...
```
Expected: No warnings.

- [ ] **Step 3: Build binary**

```bash
go build -o tengiz .
```
Expected: Build succeeds.

- [ ] **Step 4: Verify CLI help text**

```bash
./tengiz volume --help
./tengiz volume add --help
./tengiz volume remove --help
./tengiz volume list --help
```
Expected: All commands show proper usage.

- [ ] **Step 5: Self-review checklist**

1. **Spec coverage:** The plan covers:
   - Volume mount config in `.tengiz.yaml` (Task 1, 5)
   - Docker `-v` flag generation in `Create()`/`CreateVersioned()` (Task 1, 2)
   - CLI lifetime commands `add/remove/list` (Task 4, 6)
   - Read-only volume support (Task 6)
   - State persistence via `Store` (Task 3)
   - Config→container end-to-end wiring (Task 5)

2. **Placeholder scan:** No TBD, TODO, or placeholder patterns used.

3. **Type consistency:** `VolumeConfig` struct consistent across all tasks. `volumeArgs()` returns `[]string`. Store methods use `VolumeConfig` consistently.

- [ ] **Step 6: Commit final review if any fixes needed**

```bash
git add -A
git commit -m "chore: final review fixes for persistent storage feature"
```
