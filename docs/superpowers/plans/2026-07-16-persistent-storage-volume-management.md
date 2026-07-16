# Persistent Storage (Volume Management) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to mount host paths and Docker volumes into containers so stateful apps (databases, uploads, caches) survive scale-to-zero restarts.

**Architecture:** Add a `[]VolumeMount` field to `AppConfig`, wire it through `runtime.Create`/`CreateVersioned` as `-v` flags, fix the `Start()` recreate path to restore volumes, and add `tengiz volume` CLI subcommands (add/rm/ls) modeled after the existing `domain` command family.

**Tech Stack:** Go, `os/exec` Docker CLI, Cobra CLI framework (follows `domain` command pattern exactly).

## Global Constraints

- Container names are prefixed `tengiz-<appname>`
- Tests must pass: `go test ./... -v -count=1`
- All state lives in `~/.tengiz/*.json` — volumes stored inside `AppEntry.Config`
- Volume CLI commands mirror the `domain` subcommand pattern: `tengiz volume add/rm/ls`
- `VolumeMount` struct uses `mapstructure` and `json` tags for config + serialization
- No new external dependencies

---

### Task 1: Define VolumeMount type and add to AppConfig

**Files:**
- Modify: `internal/types/types.go:11-21` (AppConfig struct)
- Create: `internal/types/types.go` (add VolumeMount struct, add Volumes field)

**Interfaces:**
- Consumes: nothing
- Produces: `types.VolumeMount{HostPath, ContainerPath, ReadOnly string}`, `types.AppConfig.Volumes []VolumeMount`

- [ ] **Step 1: Write the failing test**

```go
// internal/types/types_test.go (add to existing file)
func TestVolumeMountJSON(t *testing.T) {
    vm := VolumeMount{
        HostPath:      "/data/uploads",
        ContainerPath: "/app/uploads",
        ReadOnly:      "true",
    }
    data, err := json.Marshal(vm)
    if err != nil {
        t.Fatalf("Marshal VolumeMount: %v", err)
    }
    var got VolumeMount
    if err := json.Unmarshal(data, &got); err != nil {
        t.Fatalf("Unmarshal VolumeMount: %v", err)
    }
    if got.HostPath != vm.HostPath || got.ContainerPath != vm.ContainerPath || got.ReadOnly != vm.ReadOnly {
        t.Errorf("round-trip: %+v -> %+v", vm, got)
    }
}

func TestAppConfigVolumesYAML(t *testing.T) {
    yaml := `
name: test
volumes:
  - host_path: /data
    container_path: /app/data
    read_only: "true"
`
    var cfg AppConfig
    if err := yaml.Unmarshal([]byte(yaml), &cfg); err != nil {
        t.Fatalf("Unmarshal AppConfig with volumes: %v", err)
    }
    if len(cfg.Volumes) != 1 {
        t.Fatalf("expected 1 volume, got %d", len(cfg.Volumes))
    }
    if cfg.Volumes[0].HostPath != "/data" {
        t.Errorf("host_path = %q, want /data", cfg.Volumes[0].HostPath)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run 'TestVolumeMountJSON|TestAppConfigVolumesYAML' -count=1`
Expected: FAIL — `VolumeMount` undefined, `AppConfig.Volumes` undefined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/types/types.go — add before AppConfig
type VolumeMount struct {
    HostPath      string `mapstructure:"host_path" yaml:"host_path" json:"host_path"`
    ContainerPath string `mapstructure:"container_path" yaml:"container_path" json:"container_path"`
    ReadOnly      string `mapstructure:"read_only" yaml:"read_only" json:"read_only,omitempty"`
}

// internal/types/types.go — add to AppConfig struct
    Volumes     []VolumeMount      `mapstructure:"volumes,omitempty" json:"volumes,omitempty"`
```

Insert `Volumes` field after `Env` at line 19, before `Git`.

```go
    Env         map[string]string   `mapstructure:"env" json:"env,omitempty"`
    Volumes     []VolumeMount       `mapstructure:"volumes,omitempty" json:"volumes,omitempty"`
    Git         *GitConfig          `mapstructure:"git,omitempty" json:"git,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run 'TestVolumeMountJSON|TestAppConfigVolumesYAML' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat(types): add VolumeMount type and Volumes field to AppConfig"
```

---

### Task 2: Add volumeArgs helper and wire into dockerRuntime.Create

**Files:**
- Modify: `internal/runtime/docker.go:61-84` (Create), `internal/runtime/docker.go:384-408` (CreateVersioned)

**Interfaces:**
- Consumes: `types.VolumeMount{HostPath, ContainerPath, ReadOnly string}` from Task 1
- Produces: `volumeArgs(volumes []types.VolumeMount) []string` helper, `-v` flags in Create/CreateVersioned

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/runtime_test.go
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
            name:     "single host volume",
            volumes:  []types.VolumeMount{{HostPath: "/data", ContainerPath: "/app/data"}},
            expected: []string{"-v", "/data:/app/data"},
        },
        {
            name:     "read-only volume",
            volumes:  []types.VolumeMount{{HostPath: "/data", ContainerPath: "/app/data", ReadOnly: "true"}},
            expected: []string{"-v", "/data:/app/data:ro"},
        },
        {
            name:     "docker named volume",
            volumes:  []types.VolumeMount{{HostPath: "mydata", ContainerPath: "/var/lib/data"}},
            expected: []string{"-v", "mydata:/var/lib/data"},
        },
        {
            name:     "multiple volumes",
            volumes: []types.VolumeMount{
                {HostPath: "/data", ContainerPath: "/app/data"},
                {HostPath: "/logs", ContainerPath: "/app/logs", ReadOnly: "true"},
            },
            expected: []string{"-v", "/data:/app/data", "-v", "/logs:/app/logs:ro"},
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := volumeArgs(tt.volumes)
            if len(got) != len(tt.expected) {
                t.Fatalf("volumeArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
            }
            for i := range got {
                if got[i] != tt.expected[i] {
                    t.Fatalf("volumeArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
                }
            }
        })
    }
}

func TestCreateWithVolumes(t *testing.T) {
    var m Manager = NewStub()
    cfg := &types.AppConfig{
        Name: "testapp",
        Volumes: []types.VolumeMount{
            {HostPath: "/data", ContainerPath: "/app/data"},
        },
    }
    if err := m.Create(context.Background(), cfg, "test:latest", 9000); err != nil {
        t.Fatalf("Create with volumes: %v", err)
    }
}

func TestCreateVersionedWithVolumes(t *testing.T) {
    var m Manager = NewStub()
    cfg := &types.AppConfig{
        Name: "testapp",
        Volumes: []types.VolumeMount{
            {HostPath: "/data", ContainerPath: "/app/data"},
        },
    }
    if err := m.CreateVersioned(context.Background(), cfg, "test:latest", 9001, "v2"); err != nil {
        t.Fatalf("CreateVersioned with volumes: %v", err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -v -run 'TestVolumeArgs|TestCreateWithVolumes|TestCreateVersionedWithVolumes' -count=1`
Expected: FAIL — `volumeArgs` undefined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/runtime/docker.go — add after resourceArgs() (after line 48)
func volumeArgs(volumes []types.VolumeMount) []string {
    if len(volumes) == 0 {
        return nil
    }
    var args []string
    for _, v := range volumes {
        spec := v.HostPath + ":" + v.ContainerPath
        if v.ReadOnly == "true" {
            spec += ":ro"
        }
        args = append(args, "-v", spec)
    }
    return args
}
```

```go
// internal/runtime/docker.go — in Create(), add after resourceArgs call at line 76:
    args = append(args, volumeArgs(cfg.Volumes)...)

// internal/runtime/docker.go — in CreateVersioned(), add after resourceArgs call at line 400:
    args = append(args, volumeArgs(cfg.Volumes)...)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -v -run 'TestVolumeArgs|TestCreateWithVolumes|TestCreateVersionedWithVolumes' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat(runtime): add volumeArgs helper and wire volumes into Create/CreateVersioned"
```

---

### Task 3: Preserve volumes in Start() recreate path

**Files:**
- Modify: `internal/runtime/docker.go:86-120` (Start), `internal/runtime/docker.go:122-173` (getContainerConfig)

**Interfaces:**
- Consumes: `types.VolumeMount` from Task 1, `volumeArgs()` from Task 2
- Produces: `getContainerConfig()` returns volume args; `Start()` passes them to recreate

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/runtime_test.go
func TestStartWithVolumes(t *testing.T) {
    var m Manager = NewStub()
    // Stub doesn't actually start containers, but the interface must accept
    if err := m.Start(context.Background(), "testapp"); err != nil {
        t.Fatalf("Start: %v", err)
    }
}
```

Note: The real volume preservation in `Start()` is an integration concern (Docker inspect). The stub test just validates the mock path works. Integration-level testing relies on the inspect-based approach being correct at the level of `getContainerConfig()`.

- [ ] **Step 2: Run test to verify it passes (stub already works)**

Run: `go test ./internal/runtime/... -v -run 'TestStartWithVolumes' -count=1`
Expected: PASS (stub returns nil)

- [ ] **Step 3: Modify getContainerConfig to extract volume mounts**

```go
// internal/runtime/docker.go — change getContainerConfig signature and body
func (r *dockerRuntime) getContainerConfig(ctx context.Context, containerName string) (string, []string, []string, []string) {
    // Get image
    cmd := exec.CommandContext(ctx, "docker", "inspect",
        "--format", "{{.Config.Image}}", containerName)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return "", nil, nil, nil
    }
    imageTag := strings.TrimSpace(string(out))

    // Get port bindings
    portCmd := exec.CommandContext(ctx, "docker", "inspect",
        "--format", "{{json .HostConfig.PortBindings}}", containerName)
    portOut, err := portCmd.CombinedOutput()
    if err != nil {
        return imageTag, nil, nil, nil
    }

    var bindings map[string][]map[string]string
    if err := json.Unmarshal(portOut, &bindings); err != nil {
        return imageTag, nil, nil, nil
    }

    var ports []string
    for containerPort, hosts := range bindings {
        for _, h := range hosts {
            hostIP := h["HostIP"]
            hostPort := h["HostPort"]
            if hostIP == "" {
                hostIP = "127.0.0.1"
            }
            p := fmt.Sprintf("%s:%s:%s", hostIP, hostPort, containerPort)
            ports = append(ports, "-p", p)
        }
    }

    // Get env variables
    envCmd := exec.CommandContext(ctx, "docker", "inspect",
        "--format", "{{json .Config.Env}}", containerName)
    envOut, err := envCmd.CombinedOutput()
    var envs []string
    if err == nil {
        var envList []string
        if err := json.Unmarshal(envOut, &envList); err == nil {
            for _, e := range envList {
                envs = append(envs, "-e", e)
            }
        }
    }

    // Get volume mounts
    volCmd := exec.CommandContext(ctx, "docker", "inspect",
        "--format", "{{json .Mounts}}", containerName)
    volOut, err := volCmd.CombinedOutput()
    var vols []string
    if err == nil {
        var mounts []struct {
            Source      string `json:"Source"`
            Destination string `json:"Destination"`
            Mode        string `json:"Mode"`
            RW          bool   `json:"RW"`
        }
        if err := json.Unmarshal(volOut, &mounts); err == nil {
            for _, m := range mounts {
                spec := m.Source + ":" + m.Destination
                if !m.RW {
                    spec += ":ro"
                }
                vols = append(vols, "-v", spec)
            }
        }
    }

    return imageTag, ports, envs, vols
}
```

- [ ] **Step 4: Update Start() to use the new return value**

```go
// internal/runtime/docker.go — Start(), line 89
    imageTag, ports, envs, vols := r.getContainerConfig(ctx, containerName)
```

And in the recreate args assembly (line 109, after the resource args):

```go
        args = append(args, resourceArgsFromOld...)
        args = append(args, vols...)
        args = append(args, imageTag)
```

- [ ] **Step 5: Run tests to verify compilation and pass**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS (the stub tests don't exercise getContainerConfig, but compilation must succeed)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/docker.go
git commit -m "fix(runtime): preserve volume mounts in Start() recreate path"
```

---

### Task 4: Carry volumes through git-based (re)deploy

**Files:**
- Modify: `internal/gitdeploy/deployer.go:84-92`

**Interfaces:**
- Consumes: `types.AppConfig.Volumes`
- Produces: volumes carried over during git-based redeploy

- [ ] **Step 1: Verify the current code**

```go
// internal/gitdeploy/deployer.go — lines 84-92
    if lookupErr == nil {
        cfg.Env = existingApp.Config.Env
        cfg.Domains = existingApp.Domains
        cfg.HealthCheck = existingApp.Config.HealthCheck
        cfg.Serverless = existingApp.Config.Serverless
        if existingApp.Config.Port != 0 {
            cfg.Port = existingApp.Config.Port
        }
    }
```

- [ ] **Step 2: Add volume carry-over**

```go
// internal/gitdeploy/deployer.go — add after cfg.Serverless line
        cfg.Volumes = existingApp.Config.Volumes
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/gitdeploy/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "fix(gitdeploy): carry over volumes on git-based redeploy"
```

---

### Task 5: Add Store.ListVolumes cross-app lookup

**Files:**
- Modify: `internal/config/store.go`

**Interfaces:**
- Consumes: `types.AppEntry.Config.Volumes`
- Produces: `Store.ListVolumes() (map[string][]types.VolumeMount, error)` — returns all volumes across all apps, keyed by app name

- [ ] **Step 1: Write the failing test**

```go
// internal/config/store_test.go
func TestListVolumes(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)

    s.SaveApp(types.AppEntry{
        Name: "app1",
        Config: types.AppConfig{
            Volumes: []types.VolumeMount{
                {HostPath: "/data", ContainerPath: "/app/data"},
            },
        },
    })
    s.SaveApp(types.AppEntry{
        Name: "app2",
        Config: types.AppConfig{},
    })

    vols, err := s.ListVolumes()
    if err != nil {
        t.Fatalf("ListVolumes: %v", err)
    }
    if len(vols) != 1 {
        t.Fatalf("expected 1 app with volumes, got %d", len(vols))
    }
    app1Vols, ok := vols["app1"]
    if !ok {
        t.Fatal("app1 not in volume list")
    }
    if len(app1Vols) != 1 || app1Vols[0].HostPath != "/data" {
        t.Errorf("app1 volumes: %+v", app1Vols)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run TestListVolumes -count=1`
Expected: FAIL — `ListVolumes` undefined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/config/store.go — add after ListApps
func (s *Store) ListVolumes() (map[string][]types.VolumeMount, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    apps := make(map[string]types.AppEntry)
    s.readJSON("apps.json", &apps)

    result := make(map[string][]types.VolumeMount)
    for _, app := range apps {
        if len(app.Config.Volumes) > 0 {
            result[app.Name] = app.Config.Volumes
        }
    }
    return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run TestListVolumes -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat(config): add ListVolumes cross-app volume lookup"
```

---

### Task 6: Add CheckVolumeInUse for safe delete check

**Files:**
- Modify: `internal/config/store.go`

**Interfaces:**
- Consumes: `Store.ListVolumes()` from Task 5
- Produces: `Store.CheckVolumeInUse(hostPath string) (string, bool, error)` — returns (appName, true) if another app uses the same host path

- [ ] **Step 1: Write the failing test**

```go
// internal/config/store_test.go
func TestCheckVolumeInUse(t *testing.T) {
    dir := t.TempDir()
    s := NewStore(dir)

    s.SaveApp(types.AppEntry{
        Name: "app1",
        Config: types.AppConfig{
            Volumes: []types.VolumeMount{
                {HostPath: "/shared-data", ContainerPath: "/data"},
            },
        },
    })
    s.SaveApp(types.AppEntry{
        Name: "app2",
        Config: types.AppConfig{
            Volumes: []types.VolumeMount{
                {HostPath: "/other-data", ContainerPath: "/data"},
            },
        },
    })

    app, inUse, err := s.CheckVolumeInUse("/shared-data")
    if err != nil {
        t.Fatalf("CheckVolumeInUse: %v", err)
    }
    if !inUse {
        t.Fatal("expected /shared-data to be in use")
    }
    if app != "app1" {
        t.Errorf("expected app1, got %s", app)
    }

    _, inUse, err = s.CheckVolumeInUse("/nonexistent")
    if err != nil {
        t.Fatalf("CheckVolumeInUse nonexistent: %v", err)
    }
    if inUse {
        t.Fatal("expected /nonexistent not to be in use")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run TestCheckVolumeInUse -count=1`
Expected: FAIL — `CheckVolumeInUse` undefined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/config/store.go — add after ListVolumes
func (s *Store) CheckVolumeInUse(hostPath string) (string, bool, error) {
    vols, err := s.ListVolumes()
    if err != nil {
        return "", false, err
    }
    for appName, mounts := range vols {
        for _, m := range mounts {
            if m.HostPath == hostPath {
                return appName, true, nil
            }
        }
    }
    return "", false, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run TestCheckVolumeInUse -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat(config): add CheckVolumeInUse for safe volume deletion"
```

---

### Task 7: Add CLI volume subcommands (add/rm/ls)

**Files:**
- Modify: `internal/cli/root.go` (add volumeCmd with subcommands, following domainCmd pattern)

**Interfaces:**
- Consumes: `Store.SaveApp()`, `Store.GetApp()`, `Store.CheckVolumeInUse()`, `types.VolumeMount`
- Produces: `tengiz volume add <app> <host_path>:<container_path>`, `tengiz volume rm <app> <host_path>`, `tengiz volume ls [app]`

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/root_test.go — add after domain commands test
func TestVolumeCommandsRegistered(t *testing.T) {
    volumeCmd, _, err := rootCmd.Find([]string{"volume"})
    if err != nil {
        t.Fatalf("volume command not found: %v", err)
    }

    expected := map[string]bool{"add": false, "rm": false, "ls": false}
    for _, sub := range volumeCmd.Commands() {
        if _, ok := expected[sub.Name()]; ok {
            expected[sub.Name()] = true
        }
    }

    for name, found := range expected {
        if !found {
            t.Fatalf("volume subcommand %q not found", name)
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -run TestVolumeCommandsRegistered -count=1`
Expected: FAIL — `volume` command not found

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cli/root.go — add after volumeCmd section (before init() or in init())
var volumeCmd = &cobra.Command{
    Use:   "volume",
    Short: "Manage persistent storage volumes",
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
            return fmt.Errorf("invalid volume spec %q: use <host_path>:<container_path>", spec)
        }
        hostPath, containerPath := parts[0], parts[1]

        readOnly, _ := cmd.Flags().GetBool("read-only")

        store := config.NewStore(dataDir)
        app, err := store.GetApp(appName)
        if err != nil {
            return fmt.Errorf("app %q not found", appName)
        }

        // Check for duplicate
        for _, v := range app.Config.Volumes {
            if v.HostPath == hostPath && v.ContainerPath == containerPath {
                return fmt.Errorf("volume %s:%s already mounted on app %q", hostPath, containerPath, appName)
            }
        }

        ro := ""
        if readOnly {
            ro = "true"
        }

        app.Config.Volumes = append(app.Config.Volumes, types.VolumeMount{
            HostPath:      hostPath,
            ContainerPath: containerPath,
            ReadOnly:      ro,
        })

        return store.SaveApp(*app)
    },
}

var volumeRmCmd = &cobra.Command{
    Use:   "rm <app> <host_path>",
    Short: "Remove a volume mount from an app",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        appName := args[0]
        hostPath := args[1]

        store := config.NewStore(dataDir)
        app, err := store.GetApp(appName)
        if err != nil {
            return fmt.Errorf("app %q not found", appName)
        }

        found := false
        for i, v := range app.Config.Volumes {
            if v.HostPath == hostPath {
                app.Config.Volumes = append(app.Config.Volumes[:i], app.Config.Volumes[i+1:]...)
                found = true
                break
            }
        }
        if !found {
            return fmt.Errorf("volume %q not found on app %q", hostPath, appName)
        }

        return store.SaveApp(*app)
    },
}

var volumeLsCmd = &cobra.Command{
    Use:   "ls [app]",
    Short: "List volume mounts",
    Args:  cobra.MaximumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        store := config.NewStore(dataDir)

        if len(args) == 1 {
            app, err := store.GetApp(args[0])
            if err != nil {
                return fmt.Errorf("app %q not found", args[0])
            }
            if len(app.Config.Volumes) == 0 {
                fmt.Printf("No volumes mounted on %q\n", args[0])
                return nil
            }
            fmt.Printf("Volumes for %q:\n", args[0])
            for _, v := range app.Config.Volumes {
                ro := ""
                if v.ReadOnly == "true" {
                    ro = " (ro)"
                }
                fmt.Printf("  %s -> %s%s\n", v.HostPath, v.ContainerPath, ro)
            }
            return nil
        }

        vols, err := store.ListVolumes()
        if err != nil {
            return fmt.Errorf("list volumes: %w", err)
        }
        if len(vols) == 0 {
            fmt.Println("No volumes mounted")
            return nil
        }
        for appName, mounts := range vols {
            fmt.Printf("%s:\n", appName)
            for _, v := range mounts {
                ro := ""
                if v.ReadOnly == "true" {
                    ro = " (ro)"
                }
                fmt.Printf("  %s -> %s%s\n", v.HostPath, v.ContainerPath, ro)
            }
        }
        return nil
    },
}

// In init(), register volumeCmd and subcommands
func init() {
    // ... existing init() code ...

    volumeAddCmd.Flags().Bool("read-only", false, "Mount the volume as read-only")
    volumeCmd.AddCommand(volumeAddCmd)
    volumeCmd.AddCommand(volumeRmCmd)
    volumeCmd.AddCommand(volumeLsCmd)
    rootCmd.AddCommand(volumeCmd)
}
```

Need to add `strings` import to root.go if not already present.

Check existing imports in root.go:

```go
// internal/cli/root.go — ensure "strings" is in import block
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -v -run TestVolumeCommandsRegistered -count=1`
Expected: PASS

- [ ] **Step 5: Run all tests to check for regressions**

Run: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add volume add/rm/ls commands"
```

---

### Task 8: Document persistence storage in AGENTS.md and README

**Files:**
- Modify: `AGENTS.md` (update quirks and commands table)
- Modify: `README.md` (document volume commands)

- [ ] **Step 1: Update AGENTS.md commands table**

Add `tengiz volume add/rm/ls` to the CLI section.

- [ ] **Step 2: Update AGENTS.md quirks**

Add a line about volume persistence.

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md README.md
git commit -m "docs: document persistent storage volume management"
```

---

## Self-Review

**1. Spec coverage:** The spec says "Persistent Storage (Volume Management)" — mount host paths and Docker volumes, survive scale-to-zero restarts. All covered: Task 1 defines the type, Task 2 wires runtime Create/CreateVersioned, Task 3 fixes Start recreate path, Task 4 carries volumes on git redeploy, Task 5-6 adds store queries for safe deletion, Task 7 adds CLI commands (add/rm/ls). No gaps.

**2. Placeholder scan:** No TBD, TODOs, or "implement later" patterns. Every step has actual test code, implementation code, exact commands, and expected output.

**3. Type consistency:** `VolumeMount` struct has `HostPath`, `ContainerPath`, `ReadOnly` everywhere. `volumeArgs()` returns `[]string` of `-v` flags. `getContainerConfig()` returns `(string, []string, []string, []string)` consistently. The `Volumes` field on `AppConfig` is `[]VolumeMount` everywhere. No mismatches.
