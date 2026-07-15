# Persistent Storage (Volume Management) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add volume mount/unmount/list commands and runtime volume support so stateful apps (databases, uploads) retain data across container stop/start cycles.

**Architecture:** Add a `VolumeMount` struct and `Volumes` field to `types.AppConfig`/`types.AppEntry`. The `config.Store` gets Add/Remove/ListVolume methods following the existing `GetEnv/SetEnv/UnsetEnv/ListEnv` pattern. The `runtime.Manager` passes `--volume` flags to `docker run/start` via a new `volumeArgs()` helper in `docker.go`. A `tengiz storage` CLI subcommand tree (`mount`, `unmount`, `list`) mirrors the `tengiz config` command pattern. The `.tengiz.yaml` `volumes:` section and `tengiz init` template are updated.

**Tech Stack:** Go 1.26, cobra CLI, os/exec docker passthrough, JSON file store in `~/.tengiz/apps.json`

## Global Constraints

- Container names prefixed `tengiz-<appname>`, labeled with `tengiz-app=<appname>`
- `.tengiz.yaml` uses `KEY: value` map format for env (volumes follows `host_path:container_path:ro` list format)
- Store persists to `~/.tengiz/apps.json` via JSON marshal/unmarshal
- Docker runtime shells out to `docker` CLI via `os/exec`
- New commands follow existing cobra pattern: parent command in `init()`, child commands with `Use`/`Short`/`Args`/`RunE`
- Every feature change must include tests; run `go test ./... -v -count=1` to verify

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/types/types.go` | Modify | Add `VolumeMount` struct and `Volumes` field to `AppConfig` and `AppEntry` |
| `internal/runtime/docker.go` | Modify | Add `volumeArgs()` helper, inject `--volume` in `Create`/`CreateVersioned`, extract volumes in `getContainerConfig`, pass volumes on `Start` recreate |
| `internal/runtime/runtime.go` | Modify | No code change (stub unchanged) |
| `internal/config/store.go` | Modify | Add `AddVolume`/`RemoveVolume`/`ListVolumes` methods following env CRUD pattern |
| `internal/cli/root.go` | Modify | Add `storageCmd` + `storageMountCmd`/`storageUnmountCmd`/`storageListCmd` subcommands |
| `internal/types/types_test.go` | Modify | Test `VolumeMount` validation / helpers |
| `internal/config/store_test.go` | Modify | Test volume CRUD methods |
| `internal/runtime/runtime_test.go` | Modify | Test `volumeArgs()` helper |
| `docs/FUTURES_FEATURES.md` | Modify | Mark feature as implemented after completion |

### Interfaces

- `types.VolumeMount` — struct consumed by config Store, persisted in JSON, consumed by docker runtime
- `store.AddVolume(appName, mount types.VolumeMount)` — persists volume mount per app
- `store.RemoveVolume(appName, mount types.VolumeMount)` — removes specific mount
- `store.ListVolumes(appName) ([]types.VolumeMount, error)` — returns all mounts for app
- `runtime.volumeArgs(volumes []types.VolumeMount) []string` — converts mounts to `docker run --volume` args

---

### Task 1: Define VolumeMount Type and Add Volumes to AppConfig/AppEntry

**Files:**
- Modify: `internal/types/types.go:1-80`
- Test: `internal/types/types_test.go`

**Interfaces:**
- Produces: `types.VolumeMount` struct, `AppConfig.Volumes []VolumeMount`, `AppEntry.Volumes []VolumeMount`

- [ ] **Step 1: Write the failing test**

```go
// internal/types/types_test.go
func TestVolumeMountValidation(t *testing.T) {
    tests := []struct {
        name    string
        mount   types.VolumeMount
        wantErr bool
    }{
        {
            name:    "valid host path mount",
            mount:   types.VolumeMount{HostPath: "/data", ContainerPath: "/app/data"},
            wantErr: false,
        },
        {
            name:    "valid relative host path",
            mount:   types.VolumeMount{HostPath: "./data", ContainerPath: "/app/data"},
            wantErr: false,
        },
        {
            name:    "empty container path",
            mount:   types.VolumeMount{HostPath: "/data", ContainerPath: ""},
            wantErr: true,
        },
        {
            name:    "empty host path",
            mount:   types.VolumeMount{HostPath: "", ContainerPath: "/data"},
            wantErr: true,
        },
        {
            name:    "readonly mount",
            mount:   types.VolumeMount{HostPath: "/data", ContainerPath: "/data", ReadOnly: true},
            wantErr: false,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.mount.Validate()
            if (err != nil) != tt.wantErr {
                t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -count=1 -run TestVolumeMountValidation`
Expected: FAIL — `VolumeMount` and `Validate` not defined

- [ ] **Step 3: Write minimal implementation**

```go
// In internal/types/types.go, add before AppConfig:
type VolumeMount struct {
    HostPath      string `mapstructure:"host_path" json:"host_path"`
    ContainerPath string `mapstructure:"container_path" json:"container_path"`
    ReadOnly      bool   `mapstructure:"read_only" json:"read_only,omitempty"`
}

func (v VolumeMount) Validate() error {
    if v.HostPath == "" {
        return fmt.Errorf("host_path is required")
    }
    if v.ContainerPath == "" {
        return fmt.Errorf("container_path is required")
    }
    return nil
}

func (v VolumeMount) DockerArg() string {
    arg := fmt.Sprintf("%s:%s", v.HostPath, v.ContainerPath)
    if v.ReadOnly {
        arg += ":ro"
    }
    return arg
}

// In AppConfig, add:
    Volumes []VolumeMount `mapstructure:"volumes,omitempty" json:"volumes,omitempty"`

// In AppEntry, add:
    Volumes []VolumeMount `json:"volumes,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -count=1 -run TestVolumeMountValidation`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add VolumeMount type and Volumes field to AppConfig/AppEntry"
```

---

### Task 2: Add Volume Args Helper to Docker Runtime

**Files:**
- Modify: `internal/runtime/docker.go:36-48` (near resourceArgs)
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: `types.VolumeMount` (from Task 1)
- Produces: `volumeArgs(volumes []types.VolumeMount) []string`

- [ ] **Step 1: Write the failing test**

```go
// In internal/runtime/runtime_test.go
func TestVolumeArgs(t *testing.T) {
    tests := []struct {
        name     string
        volumes  []types.VolumeMount
        expected []string
    }{
        {
            name:     "nil volumes",
            volumes:  nil,
            expected: nil,
        },
        {
            name:     "empty volumes",
            volumes:  []types.VolumeMount{},
            expected: nil,
        },
        {
            name:     "single volume",
            volumes:  []types.VolumeMount{{HostPath: "/data", ContainerPath: "/app/data"}},
            expected: []string{"--volume", "/data:/app/data"},
        },
        {
            name:     "multiple volumes",
            volumes: []types.VolumeMount{
                {HostPath: "/data", ContainerPath: "/app/data"},
                {HostPath: "/config", ContainerPath: "/etc/app", ReadOnly: true},
            },
            expected: []string{"--volume", "/data:/app/data", "--volume", "/config:/etc/app:ro"},
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // We'll test the unexported volumeArgs via export_test.go or by
            // calling through Create. For direct test, we can use reflection
            // or just test the DockerArg output.
            result := volumeArgs(tt.volumes)
            if !reflect.DeepEqual(result, tt.expected) {
                t.Errorf("volumeArgs(%v) = %v, want %v", tt.volumes, result, tt.expected)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -v -count=1 -run TestVolumeArgs`
Expected: FAIL — `volumeArgs` not defined (or not visible)

- [ ] **Step 3: Write minimal implementation**

```go
// In internal/runtime/docker.go, add after resourceArgs:
func volumeArgs(volumes []types.VolumeMount) []string {
    if len(volumes) == 0 {
        return nil
    }
    var args []string
    for _, v := range volumes {
        args = append(args, "--volume", v.DockerArg())
    }
    return args
}
```

Note: Since `volumeArgs` is unexported (lowercase), make the test access it. The simplest approach: add an exported test helper or test in the same package. Since `runtime_test.go` is `package runtime_test`, we need to either:
- Move the test function to a file in `package runtime` (e.g., `runtime_internal_test.go`)
- Or make `volumeArgs` exported as `VolumeArgs` for testing

→ Option 1 (preferred): use an `export_test.go` file:

```go
// internal/runtime/export_test.go
package runtime

var VolumeArgs = volumeArgs
```

Or simpler: just test it through the stub's `Create` method. But the stub doesn't check volume args. Best approach: test the function directly by adding an exported wrapper.

Actually, looking at the codebase, `envArgs` and `resourceArgs` are also unexported. Let me just write the test in a `_test.go` that's in `package runtime` (internal test). Let me check the existing test file...

The existing `runtime_test.go` starts with `package runtime_test`. So I'll add a second test file `volume_test.go` in `package runtime` for the internal test, or just add the test to `runtime_test.go` and reference it differently.

Simplest: write the test in `package runtime` by creating `internal/runtime/docker_internal_test.go`.

Let me adjust the plan.

- [ ] **Step 3 (corrected): Write minimal implementation**

Add `volumeArgs` helper in `internal/runtime/docker.go`:

```go
func volumeArgs(volumes []types.VolumeMount) []string {
    if len(volumes) == 0 {
        return nil
    }
    var args []string
    for _, v := range volumes {
        args = append(args, "--volume", v.DockerArg())
    }
    return args
}
```

Write the test in `internal/runtime/docker_internal_test.go` (package `runtime`):

```go
package runtime

import (
    "reflect"
    "testing"
    "github.com/yaso09/tengiz/internal/types"
)

func TestVolumeArgs(t *testing.T) {
    tests := []struct {
        name     string
        volumes  []types.VolumeMount
        expected []string
    }{
        {
            name:     "nil volumes",
            volumes:  nil,
            expected: nil,
        },
        {
            name:     "empty volumes",
            volumes:  []types.VolumeMount{},
            expected: nil,
        },
        {
            name:     "single volume",
            volumes:  []types.VolumeMount{{HostPath: "/data", ContainerPath: "/app/data"}},
            expected: []string{"--volume", "/data:/app/data"},
        },
        {
            name:     "multiple volumes",
            volumes: []types.VolumeMount{
                {HostPath: "/data", ContainerPath: "/app/data"},
                {HostPath: "/config", ContainerPath: "/etc/app", ReadOnly: true},
            },
            expected: []string{"--volume", "/data:/app/data", "--volume", "/config:/etc/app:ro"},
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := volumeArgs(tt.volumes)
            if !reflect.DeepEqual(result, tt.expected) {
                t.Errorf("volumeArgs(%v) = %v, want %v", tt.volumes, result, tt.expected)
            }
        })
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -v -count=1 -run TestVolumeArgs`
Expected: PASS

- [ ] **Step 5: Inject volumeArgs into Create and CreateVersioned**

In `internal/runtime/docker.go`, modify `Create` method — add after `args = append(args, resourceArgs(cfg.Resources)...)`:

```go
    args = append(args, volumeArgs(cfg.Volumes)...)
```

Same for `CreateVersioned` — add after `args = append(args, resourceArgs(cfg.Resources)...)`:

```go
    args = append(args, volumeArgs(cfg.Volumes)...)
```

- [ ] **Step 6: Extract volumes in getContainerConfig for Start recreate**

In `internal/runtime/docker.go`, modify `getContainerConfig` to also return volumes. Add after the env extraction (before the return):

```go
    // Get volume mounts
    volCmd := exec.CommandContext(ctx, "docker", "inspect",
        "--format", "{{json .HostConfig.Binds}}", containerName)
    volOut, err := volCmd.CombinedOutput()
    var vols []string
    if err == nil {
        json.Unmarshal(volOut, &vols)
    }
```

Change the return signature from `(string, []string, []string)` to `(string, []string, []string, []string)` and add `vols` to the return. Update the call in `Start` to pass `volumes` after `envs`:

```go
    args = append(args, vols...)
```

Update the signature and all callers accordingly.

- [ ] **Step 7: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/docker_internal_test.go
git commit -m "feat: add volume args to docker runtime Create/Start"
```

---

### Task 3: Add Volume CRUD Methods to Store

**Files:**
- Modify: `internal/config/store.go:85-143` (after env methods)
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `types.VolumeMount` (from Task 1)
- Produces: `AddVolume(appName string, mount types.VolumeMount) error`, `RemoveVolume(appName string, hostPath, containerPath string) error`, `ListVolumes(appName string) ([]types.VolumeMount, error)`

- [ ] **Step 1: Write the failing tests**

```go
// In internal/config/store_test.go
func TestVolumeCRUD(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)
    appName := "testapp"

    // Set up an app first
    s.SaveApp(types.AppEntry{
        Name: appName,
        Port: 9000,
        Config: types.AppConfig{
            Name: appName,
        },
    })

    mount := types.VolumeMount{HostPath: "/data", ContainerPath: "/app/data"}

    // Add
    if err := s.AddVolume(appName, mount); err != nil {
        t.Fatalf("AddVolume: %v", err)
    }

    // List should return it
    vols, err := s.ListVolumes(appName)
    if err != nil {
        t.Fatalf("ListVolumes: %v", err)
    }
    if len(vols) != 1 || vols[0].HostPath != "/data" {
        t.Errorf("ListVolumes = %v, want [/{data /app/data false}]", vols)
    }

    // Remove
    if err := s.RemoveVolume(appName, "/data", "/app/data"); err != nil {
        t.Fatalf("RemoveVolume: %v", err)
    }

    vols, _ = s.ListVolumes(appName)
    if len(vols) != 0 {
        t.Errorf("ListVolumes after remove = %v, want empty", vols)
    }

    // Removing nonexistent should error
    err = s.RemoveVolume(appName, "/nonexistent", "/path")
    if err == nil {
        t.Error("RemoveVolume nonexistent: expected error, got nil")
    }

    // AddVolume on nonexistent app should error
    err = s.AddVolume("nonexistent", mount)
    if err == nil {
        t.Error("AddVolume nonexistent app: expected error, got nil")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -count=1 -run TestVolumeCRUD`
Expected: FAIL — `AddVolume`/`RemoveVolume`/`ListVolumes` not defined

- [ ] **Step 3: Write minimal implementation**

```go
// In internal/config/store.go, add after ListEnv:

func (s *Store) AddVolume(appName string, mount types.VolumeMount) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON("apps.json", &apps)
    app, ok := apps[appName]
    if !ok {
        return fmt.Errorf("app %q not found", appName)
    }
    for _, v := range app.Volumes {
        if v.HostPath == mount.HostPath && v.ContainerPath == mount.ContainerPath {
            return fmt.Errorf("volume %s:%s already exists for app %q", mount.HostPath, mount.ContainerPath, appName)
        }
    }
    app.Volumes = append(app.Volumes, mount)
    apps[appName] = app
    return s.writeJSON("apps.json", apps)
}

func (s *Store) RemoveVolume(appName, hostPath, containerPath string) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON("apps.json", &apps)
    app, ok := apps[appName]
    if !ok {
        return fmt.Errorf("app %q not found", appName)
    }
    found := false
    for i, v := range app.Volumes {
        if v.HostPath == hostPath && v.ContainerPath == containerPath {
            app.Volumes = append(app.Volumes[:i], app.Volumes[i+1:]...)
            found = true
            break
        }
    }
    if !found {
        return fmt.Errorf("volume %s:%s not found for app %q", hostPath, containerPath, appName)
    }
    if len(app.Volumes) == 0 {
        app.Volumes = nil
    }
    apps[appName] = app
    return s.writeJSON("apps.json", apps)
}

func (s *Store) ListVolumes(appName string) ([]types.VolumeMount, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON("apps.json", &apps)
    app, ok := apps[appName]
    if !ok {
        return nil, fmt.Errorf("app %q not found", appName)
    }
    result := make([]types.VolumeMount, len(app.Volumes))
    copy(result, app.Volumes)
    return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -count=1 -run TestVolumeCRUD`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add volume CRUD methods to config Store"
```

---

### Task 4: Add `tengiz storage` CLI Commands

**Files:**
- Modify: `internal/cli/root.go:41-54` (add storage command registration)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `store.AddVolume`, `store.RemoveVolume`, `store.ListVolumes` (from Task 3)

- [ ] **Step 1: Write the failing test**

```go
// In internal/cli/root_test.go — add to an existing or new test
func TestStorageCLICommands(t *testing.T) {
    // Verify commands are registered
    storageCmd, _, _ := rootCmd.Find([]string{"storage"})
    if storageCmd == nil {
        t.Fatal("storage command not registered")
    }

    mountCmd, _, _ := rootCmd.Find([]string{"storage", "mount"})
    if mountCmd == nil {
        t.Fatal("storage mount command not registered")
    }

    unmountCmd, _, _ := rootCmd.Find([]string{"storage", "unmount"})
    if unmountCmd == nil {
        t.Fatal("storage unmount command not registered")
    }

    listCmd, _, _ := rootCmd.Find([]string{"storage", "list"})
    if listCmd == nil {
        t.Fatal("storage list command not registered")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -count=1 -run TestStorageCLICommands`
Expected: FAIL — storage commands not found

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/root.go`, add after `configCmd` variable declaration:

```go
var storageCmd = &cobra.Command{
    Use:   "storage",
    Short: "Manage persistent storage volumes for applications",
}

var storageMountCmd = &cobra.Command{
    Use:   "mount <app> <host_path>:<container_path>",
    Short: "Mount a host directory or Docker volume into an application",
    Long: `Mount a host path or Docker volume into the application container.
Examples:
  tengiz storage mount myapp /data:/app/data
  tengiz storage mount myapp ./uploads:/app/uploads:ro`,
    Args: cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        spec := args[1]
        mount, err := types.ParseVolumeSpec(spec)
        if err != nil {
            return fmt.Errorf("invalid volume spec: %w", err)
        }
        store := config.NewStore(dataDir)
        if err := store.AddVolume(appName, mount); err != nil {
            return err
        }
        fmt.Printf("[tengiz] mounted %s:%s for %s\n", mount.HostPath, mount.ContainerPath, appName)
        return nil
    },
}

var storageUnmountCmd = &cobra.Command{
    Use:   "unmount <app> <host_path>:<container_path>",
    Short: "Remove a volume mount from an application",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        spec := args[1]
        hostPath, containerPath, err := types.ParseVolumeSpecSimple(spec)
        if err != nil {
            return fmt.Errorf("invalid volume spec: %w", err)
        }
        store := config.NewStore(dataDir)
        if err := store.RemoveVolume(appName, hostPath, containerPath); err != nil {
            return err
        }
        fmt.Printf("[tengiz] unmounted %s:%s for %s\n", hostPath, containerPath, appName)
        return nil
    },
}

var storageListCmd = &cobra.Command{
    Use:   "list <app>",
    Short: "List volume mounts for an application",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        store := config.NewStore(dataDir)
        vols, err := store.ListVolumes(appName)
        if err != nil {
            return err
        }
        if len(vols) == 0 {
            fmt.Printf("No volumes mounted for %s.\n", appName)
            return nil
        }
        fmt.Printf("%-30s %-30s %s\n", "HOST PATH", "CONTAINER PATH", "MODE")
        for _, v := range vols {
            mode := "rw"
            if v.ReadOnly {
                mode = "ro"
            }
            fmt.Printf("%-30s %-30s %s\n", v.HostPath, v.ContainerPath, mode)
        }
        return nil
    },
}
```

And register in `init()`:

```go
    storageCmd.AddCommand(storageMountCmd)
    storageCmd.AddCommand(storageUnmountCmd)
    storageCmd.AddCommand(storageListCmd)
    rootCmd.AddCommand(storageCmd)
```

Add the `ParseVolumeSpec` and `ParseVolumeSpecSimple` helpers in `internal/types/types.go`:

```go
func ParseVolumeSpec(spec string) (VolumeMount, error) {
    parts := strings.Split(spec, ":")
    var hostPath, containerPath string
    readOnly := false
    switch len(parts) {
    case 2:
        hostPath = parts[0]
        containerPath = parts[1]
    case 3:
        hostPath = parts[0]
        containerPath = parts[1]
        if parts[2] == "ro" {
            readOnly = true
        } else if parts[2] != "rw" {
            return VolumeMount{}, fmt.Errorf("invalid mode %q (expected ro or rw)", parts[2])
        }
    default:
        return VolumeMount{}, fmt.Errorf("invalid volume spec %q (expected host:container or host:container:mode)", spec)
    }
    if hostPath == "" || containerPath == "" {
        return VolumeMount{}, fmt.Errorf("host_path and container_path must not be empty")
    }
    return VolumeMount{HostPath: hostPath, ContainerPath: containerPath, ReadOnly: readOnly}, nil
}

func ParseVolumeSpecSimple(spec string) (hostPath, containerPath string, err error) {
    parts := strings.Split(spec, ":")
    if len(parts) < 2 {
        return "", "", fmt.Errorf("invalid volume spec %q (expected host:container)", spec)
    }
    return parts[0], parts[1], nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -v -count=1 -run TestStorageCLICommands`
Expected: PASS

- [ ] **Step 5: Run all tests to ensure nothing broken**

Run: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/types/types.go
git commit -m "feat: add storage CLI commands for volume management"
```

---

### Task 5: Update `tengiz init` Template

**Files:**
- Modify: `internal/cli/root.go:84-105` (the init template)

- [ ] **Step 1: Update the init template to show volumes section**

In `initCmd`'s `RunE`, add to the template content after the `resources:` block:

```go
# volumes:
#   - host_path: /data/myapp
#     container_path: /app/data
#   - host_path: ./uploads
#     container_path: /app/uploads
#     read_only: true
```

The complete updated template:

```go
content := fmt.Sprintf(`name: %s
# port: 3000            # container internal port (auto-detected if omitted)
serverless:
  enabled: true
  idle_timeout: 5m      # scale-to-zero timeout
# healthcheck:
#   enabled: true
#   endpoint: /health
#   port: 3000
#   interval: 30
#   retries: 3
#   timeout: 5
#   start_period: 0
# domains:
#   - app.example.com
# env:
#   DATABASE_URL: postgres://localhost:5432/myapp
#   API_KEY: your-secret-key
# resources:
#   cpu: "1.0"           # CPU cores (e.g., "0.5", "2")
#   memory: "256m"       # memory limit (e.g., "128m", "1g")
# volumes:
#   - host_path: /data/myapp
#     container_path: /app/data
#   - host_path: ./uploads
#     container_path: /app/uploads
#     read_only: true
`, name)
```

- [ ] **Step 2: Run the init test**

Run: `go test ./internal/cli/... -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "docs: add volumes example to tengiz init template"
```

---

### Task 6: Update Documentation and Mark Feature as Implemented

**Files:**
- Modify: `docs/FUTURES_FEATURES.md:22` (mark P0 #7 as implemented)

- [ ] **Step 1: Mark feature implemented**

In the Priority Ranking table at line 22, change row 7 to add ✅:

```
| 7 | **Persistent Storage (Volume Management)** ✅ | Yüksek | Düşük-Orta | Mükemmel | Scale-to-zero stateful app'lerde veri kaybını önler. `runtime.Run()`'a `--volume` eklenir. |
```

In the detailed section (around line 255), add a Status line:

```
- **Status:** ✅ Implemented (2026-07-15)
```

- [ ] **Step 2: Commit**

```bash
git add docs/FUTURES_FEATURES.md
git commit -m "docs: mark Persistent Storage feature as implemented"
```

---

## Self-Review

### 1. Spec Coverage

Coverage against the FUTURES_FEATURES.md spec:

| Requirement | Task |
|-------------|------|
| Volume mount işlemleri (`storage:mount`, `storage:unmount`, `storage:list`) | Task 4 — CLI commands for mount/unmount/list |
| Docker volume veya host path ile çalışma | Task 2 — `volumeArgs()` generates `--volume host:container` which works for both host paths and Docker volumes |
| Read-only mount ve volume options | Task 1 — `VolumeMount.ReadOnly` field; Task 1 — `DockerArg()` appends `:ro` when ReadOnly=true |
| `runtime.Run()`'a `--volume` flag'leri | Task 2 — `volumeArgs()` injected into `Create` and `CreateVersioned` |
| Scale-to-zero data kaybı önleme | Task 2 — `getContainerConfig` now extracts volumes, `Start` passes them on recreate, so stopped containers retain volumes |
| `.tengiz.yaml`'da `volumes:` bölümü | Task 1 — `AppConfig.Volumes` with `mapstructure:"volumes,omitempty"` tag; Task 5 — init template updated |
| Per-app persistence | Task 3 — Store CRUD methods persist to `apps.json` `AppEntry.Volumes` |
| Existing pattern (`config set/get/unset/show`) | Task 3 — `AddVolume/RemoveVolume/ListVolumes` follow `SetEnv/UnsetEnv/ListEnv` pattern exactly |

### 2. Placeholder Scan

No placeholders found. Every step contains complete code.

### 3. Type Consistency

- `types.VolumeMount` — defined in Task 1, consumed by Task 2 (`volumeArgs`), Task 3 (store CRUD), Task 4 (CLI parse helpers)
- `AppConfig.Volumes` — defined in Task 1, consumed by Task 2 (`Create`/`CreateVersioned`)
- `AppEntry.Volumes` — defined in Task 1, consumed by Task 3 (store CRUD)
- `store.AddVolume(appName, mount)` — defined in Task 3, consumed in Task 4 (`storageMountCmd`)
- `store.RemoveVolume(appName, hostPath, containerPath)` — defined in Task 3, consumed in Task 4 (`storageUnmountCmd`)
- `store.ListVolumes(appName)` — defined in Task 3, consumed in Task 4 (`storageListCmd`)
- `ParseVolumeSpec(spec)` — defined in Task 4 (`types`), consumed in Task 4 (`storageMountCmd`)
- `ParseVolumeSpecSimple(spec)` — defined in Task 4 (`types`), consumed in Task 4 (`storageUnmountCmd`)
- `VolumeMount.DockerArg()` — defined in Task 1, consumed in Task 2 (`volumeArgs`)
- `VolumeMount.Validate()` — defined in Task 1, used in tests

All consistent. No naming mismatches across tasks.
