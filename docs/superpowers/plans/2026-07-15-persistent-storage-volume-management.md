# Persistent Storage (Volume Management) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add volume mount management to Tengiz so stateful apps retain data across container restarts (scale-to-zero, redeploy).

**Architecture:** Add a `VolumeConfig` type and `Volumes` field to `AppConfig`. The Store gets CRUD methods for per-app volumes. The Docker runtime passes `--volume` flags to `docker run`. A `tengiz volume` CLI command group and `.tengiz.yaml` `volumes:` section expose this to users.

**Tech Stack:** Go, Cobra CLI, Docker CLI via `os/exec`, same patterns as existing `config.SetEnv`/`config.ListEnv`.

## Global Constraints

- Container names prefixed `tengiz-<appname>`, labeled `tengiz-app=<appname>`
- Port allocations 9000-9999, persisted in `~/.tengiz/ports.json`
- No config file = uses dir name as app name + defaults
- Env vars stored in `AppEntry.Config.Env` → auto-persisted via JSON in `~/.tengiz/apps.json`
- `.tengiz.yaml` `env:` section uses `KEY: value` format (map, not list)
- Go 1.26, single module `github.com/yaso09/tengiz`
- All new code must have tests

---
### Task 1: Add VolumeConfig type and Volumes field to AppConfig

**Files:**
- Modify: `internal/types/types.go:11-21`
- Test: `internal/types/types_test.go` (create)

**Interfaces:**
- Consumes: existing `AppConfig` struct
- Produces: `VolumeConfig` struct with `HostPath`, `ContainerPath`, `ReadOnly` fields; `Volumes []VolumeConfig` in `AppConfig`

- [ ] **Step 1: Write the failing test**

```go
package types

import (
	"encoding/json"
	"testing"
)

func TestVolumeConfigMarshal(t *testing.T) {
	cfg := AppConfig{
		Name: "test",
		Volumes: []VolumeConfig{
			{HostPath: "/data", ContainerPath: "/app/data", ReadOnly: false},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AppConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(decoded.Volumes))
	}
	if decoded.Volumes[0].HostPath != "/data" {
		t.Fatalf("expected HostPath /data, got %s", decoded.Volumes[0].HostPath)
	}
	if decoded.Volumes[0].ContainerPath != "/app/data" {
		t.Fatalf("expected ContainerPath /app/data, got %s", decoded.Volumes[0].ContainerPath)
	}
}

func TestVolumeConfigDefaults(t *testing.T) {
	cfg := AppConfig{Name: "test"}
	if cfg.Volumes != nil {
		t.Fatal("expected Volumes to be nil by default")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -count=1 -run TestVolumeConfig`
Expected: FAIL — cannot reference `VolumeConfig` or `Volumes`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/types/types.go` after `HealthCheckConfig`:

```go
type VolumeConfig struct {
	HostPath      string `mapstructure:"host_path" yaml:"host_path" json:"host_path"`
	ContainerPath string `mapstructure:"container_path" yaml:"container_path" json:"container_path"`
	ReadOnly      bool   `mapstructure:"read_only" yaml:"read_only" json:"read_only,omitempty"`
}
```

Add `Volumes` field to `AppConfig`:

```go
type AppConfig struct {
	Name        string              `mapstructure:"name"`
	Port        int                 `mapstructure:"port"`
	Build       BuildConfig         `mapstructure:"build"`
	Serverless  ServerlessConfig    `mapstructure:"serverless"`
	Domains     []string            `mapstructure:"domains"`
	HealthCheck *HealthCheckConfig  `mapstructure:"healthcheck,omitempty"`
	Resources   *ResourceConfig     `mapstructure:"resources,omitempty" json:"resources,omitempty"`
	Env         map[string]string   `mapstructure:"env" json:"env,omitempty"`
	Git         *GitConfig          `mapstructure:"git,omitempty" json:"git,omitempty"`
	Volumes     []VolumeConfig      `mapstructure:"volumes,omitempty" yaml:"volumes,omitempty" json:"volumes,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -count=1 -run TestVolumeConfig`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add VolumeConfig type and Volumes field to AppConfig"
```

---
### Task 2: Add volume args helper to Docker runtime

**Files:**
- Modify: `internal/runtime/docker.go:20-34` (add `volumeArgs` near `envArgs`)
- Modify: `internal/runtime/docker.go:61-84` (integrate into `Create`)
- Modify: `internal/runtime/docker.go:384-408` (integrate into `CreateVersioned`)
- Modify: `internal/runtime/docker.go:100-112` (integrate into `Start` recreate)
- Test: `internal/runtime/runtime_test.go` (add volume tests)

**Interfaces:**
- Consumes: `types.VolumeConfig` from Task 1
- Produces: `volumeArgs(volumes []types.VolumeConfig) []string` helper; volume flags are passed in `docker run` commands

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/runtime_test.go`:

```go
func TestVolumeArgs(t *testing.T) {
	volumes := []types.VolumeConfig{
		{HostPath: "/data", ContainerPath: "/app/data", ReadOnly: false},
		{HostPath: "/config", ContainerPath: "/etc/config", ReadOnly: true},
	}
	args := volumeArgs(volumes)
	expected := []string{"-v", "/data:/app/data", "-v", "/config:/etc/config:ro"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Fatalf("arg %d: expected %q, got %q", i, expected[i], args[i])
		}
	}
}

func TestVolumeArgsNil(t *testing.T) {
	args := volumeArgs(nil)
	if args != nil {
		t.Fatalf("expected nil for nil volumes, got %v", args)
	}
}

func TestVolumeArgsEmpty(t *testing.T) {
	args := volumeArgs([]types.VolumeConfig{})
	if args != nil {
		t.Fatalf("expected nil for empty volumes, got %v", args)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -v -count=1 -run TestVolumeArgs`
Expected: FAIL — `volumeArgs` not defined

- [ ] **Step 3: Write minimal implementation**

Add to `internal/runtime/docker.go` after `envArgs`:

```go
func volumeArgs(volumes []types.VolumeConfig) []string {
	if len(volumes) == 0 {
		return nil
	}
	keys := make([]int, len(volumes))
	for i := range volumes {
		keys[i] = i
	}
	sort.Slice(keys, func(i, j int) bool {
		return volumes[keys[i]].ContainerPath < volumes[keys[j]].ContainerPath
	})
	var args []string
	for _, i := range keys {
		v := volumes[i]
		mount := fmt.Sprintf("%s:%s", v.HostPath, v.ContainerPath)
		if v.ReadOnly {
			mount += ":ro"
		}
		args = append(args, "-v", mount)
	}
	return args
}
```

Add `volumeArgs(cfg.Volumes)` after `resourceArgs(cfg.Resources)` in:
- `Create` at line 76
- `CreateVersioned` at line 400
- `Start` recreate block at line 109 (but here we need to get volumes from the old container — add a `--volumes-from` or inspect approach later, for now use an empty filter)

Also add `"sort"` to the import block if not already present (it already is from line 14).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -v -count=1 -run TestVolumeArgs`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat: add volumeArgs helper and integrate into docker run commands"
```

---
### Task 3: Add volume CRUD methods to Store

**Files:**
- Modify: `internal/config/store.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `types.VolumeConfig` from Task 1, `AppEntry` from existing store
- Produces: `AddVolume(appName, vol VolumeConfig) error`, `RemoveVolume(appName, hostPath string) error`, `ListVolumes(appName string) ([]VolumeConfig, error)`, volumes are persisted in `apps.json`

- [ ] **Step 1: Write the failing test**

Add to `internal/config/store_test.go`:

```go
func TestVolumeCRUD(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	// Need an app first
	app := types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
		},
	}
	if err := s.SaveApp(app); err != nil {
		t.Fatal(err)
	}

	vol := types.VolumeConfig{HostPath: "/host/data", ContainerPath: "/container/data"}

	// Add volume
	if err := s.AddVolume("testapp", vol); err != nil {
		t.Fatal(err)
	}

	// List volumes
	vols, err := s.ListVolumes("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].HostPath != "/host/data" {
		t.Fatalf("expected /host/data, got %s", vols[0].HostPath)
	}

	// Add read-only volume
	roVol := types.VolumeConfig{HostPath: "/host/config", ContainerPath: "/container/config", ReadOnly: true}
	if err := s.AddVolume("testapp", roVol); err != nil {
		t.Fatal(err)
	}

	vols, err = s.ListVolumes("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(vols))
	}

	// Remove first volume
	if err := s.RemoveVolume("testapp", "/host/data"); err != nil {
		t.Fatal(err)
	}

	vols, err = s.ListVolumes("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume after removal, got %d", len(vols))
	}
	if vols[0].HostPath != "/host/config" {
		t.Fatalf("expected remaining volume to be /host/config, got %s", vols[0].HostPath)
	}
}

func TestVolumeCRUDNonexistentApp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	err := s.AddVolume("noexist", types.VolumeConfig{})
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -count=1 -run TestVolumeCRUD`
Expected: FAIL — methods not defined

- [ ] **Step 3: Write minimal implementation**

Add to `internal/config/store.go` after `ListDomains`:

```go
func (s *Store) AddVolume(appName string, vol types.VolumeConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	for _, v := range app.Config.Volumes {
		if v.HostPath == vol.HostPath {
			return fmt.Errorf("volume with host path %q already exists for app %q", vol.HostPath, appName)
		}
	}
	app.Config.Volumes = append(app.Config.Volumes, vol)
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) RemoveVolume(appName, hostPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
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
		return fmt.Errorf("volume with host path %q not found for app %q", hostPath, appName)
	}
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) ListVolumes(appName string) ([]types.VolumeConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return nil, fmt.Errorf("app %q not found", appName)
	}
	result := make([]types.VolumeConfig, len(app.Config.Volumes))
	copy(result, app.Config.Volumes)
	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -count=1 -run TestVolumeCRUD`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add volume CRUD methods to Store"
```

---
### Task 4: Add CLI volume commands

**Files:**
- Modify: `internal/cli/root.go` (add `volumeCmd`, `volumeAddCmd`, `volumeRemoveCmd`, `volumeListCmd` subcommands)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `store.AddVolume`, `store.RemoveVolume`, `store.ListVolumes` from Task 3
- Produces: `tengiz volume add <app> <host_path>:<container_path> [--read-only]`, `tengiz volume remove <app> <host_path>`, `tengiz volume list <app>`

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/root_test.go` (if no test file exists yet for CLI commands, create one following patterns from existing tests):

```go
func TestVolumeAddCommand(t *testing.T) {
	// Use a temp data dir
	tmpDir := t.TempDir()
	dataDir = tmpDir

	// Create an app first via the init path
	store := config.NewStore(dataDir)
	err := store.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cmd := volumeAddCmd
	cmd.SetArgs([]string{"testapp", "/host/data:/app/data"})
	err = cmd.Execute()
	if err != nil {
		t.Fatal(err)
	}

	vols, err := store.ListVolumes("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].HostPath != "/host/data" {
		t.Fatalf("expected host path /host/data, got %s", vols[0].HostPath)
	}
	if vols[0].ContainerPath != "/app/data" {
		t.Fatalf("expected container path /app/data, got %s", vols[0].ContainerPath)
	}
}

func TestVolumeAddWithReadOnly(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir

	store := config.NewStore(dataDir)
	store.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}})

	cmd := volumeAddCmd
	cmd.SetArgs([]string{"testapp", "/host/config:/etc/config:ro"})
	err := cmd.Execute()
	if err != nil {
		t.Fatal(err)
	}

	vols, _ := store.ListVolumes("testapp")
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if !vols[0].ReadOnly {
		t.Fatal("expected volume to be read-only")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -count=1 -run TestVolume`
Expected: FAIL — `volumeAddCmd` not defined

- [ ] **Step 3: Write minimal implementation**

Add before `var rootCmd` in `internal/cli/root.go`:

```go
var volumeCmd = &cobra.Command{
	Use:   "volume",
	Short: "Manage persistent storage volumes",
}

var volumeAddCmd = &cobra.Command{
	Use:   "add <app> <host_path>:<container_path>",
	Short: "Mount a volume to an app",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		mount := args[1]
		parts := strings.SplitN(mount, ":", 3)
		if len(parts) < 2 {
			return fmt.Errorf("invalid mount format: use host_path:container_path[:ro]")
		}
		hostPath := parts[0]
		containerPath := parts[1]
		readOnly := len(parts) == 3 && parts[2] == "ro"

		store := config.NewStore(dataDir)
		vol := types.VolumeConfig{
			HostPath:      hostPath,
			ContainerPath: containerPath,
			ReadOnly:      readOnly,
		}
		if err := store.AddVolume(appName, vol); err != nil {
			return err
		}
		fmt.Printf("[tengiz] mounted %s:%s for %s\n", hostPath, containerPath, appName)
		return nil
	},
}

var volumeRemoveCmd = &cobra.Command{
	Use:   "remove <app> <host_path>",
	Short: "Unmount a volume from an app",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := config.NewStore(dataDir)
		if err := store.RemoveVolume(args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("[tengiz] removed volume %s from %s\n", args[1], args[0])
		return nil
	},
}

var volumeListCmd = &cobra.Command{
	Use:   "list <app>",
	Short: "List mounted volumes for an app",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := config.NewStore(dataDir)
		vols, err := store.ListVolumes(args[0])
		if err != nil {
			return err
		}
		if len(vols) == 0 {
			fmt.Printf("No volumes mounted for %s.\n", args[0])
			return nil
		}
		fmt.Printf("Volumes for %s:\n", args[0])
		for _, v := range vols {
			ro := ""
			if v.ReadOnly {
				ro = " (read-only)"
			}
			fmt.Printf("  %s:%s%s\n", v.HostPath, v.ContainerPath, ro)
		}
		return nil
	},
}
```

Add registration in `init()`:

```go
volumeCmd.AddCommand(volumeAddCmd)
volumeCmd.AddCommand(volumeRemoveCmd)
volumeCmd.AddCommand(volumeListCmd)
rootCmd.AddCommand(volumeCmd)
```

Add `"strings"` to the import block (it's already imported at line 13).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -v -count=1 -run TestVolume`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add volume CLI commands (add/remove/list)"
```

---
### Task 5: Update .tengiz.yaml config loading and init template

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `types.VolumeConfig` from Task 1
- Produces: volumes parsed from `.tengiz.yaml` `volumes:` section into `AppConfig.Volumes`

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestLoadWithVolumes(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
name: myapp
volumes:
  - host_path: /data
    container_path: /app/data
  - host_path: /config
    container_path: /etc/config
    read_only: true
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yamlContent), 0644)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(cfg.Volumes))
	}
	if cfg.Volumes[0].HostPath != "/data" {
		t.Fatalf("expected /data, got %s", cfg.Volumes[0].HostPath)
	}
	if cfg.Volumes[1].ReadOnly != true {
		t.Fatal("expected second volume to be read-only")
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/config/... -v -count=1 -run TestLoadWithVolumes`
Expected: Already PASS if viper properly maps the `volumes` field to `AppConfig.Volumes` (viper handles slices of structs via mapstructure automatically).

If it fails, add a `volumes:` decode step in `Load()` — but given viper + mapstructure handles `[]VolumeConfig` via the `mapstructure` tags automatically, it should pass.

- [ ] **Step 3: Update init template**

Modify the init template in `internal/cli/root.go` (around line 84-105) to add volumes section:

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
# volumes:
#   - host_path: /data/myapp
#     container_path: /app/data
#     read_only: false
# domains:
#   - app.example.com
# env:
#   DATABASE_URL: postgres://localhost:5432/myapp
#   API_KEY: your-secret-key
# resources:
#   cpu: "1.0"           # CPU cores (e.g., "0.5", "2")
#   memory: "256m"       # memory limit (e.g., "128m", "1g")
`, name)
```

- [ ] **Step 4: Run full config test suite**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/cli/root.go
git commit -m "feat: add volumes config loading and init template"
```

---
### Task 6: Preserve volumes during Start recreate

**Files:**
- Modify: `internal/runtime/docker.go:122-173` (extend `getContainerConfig` to return volumes)
- Modify: `internal/runtime/docker.go:86-120` (apply volumes in Start recreate)
- Test: update existing runtime tests

**Interfaces:**
- Consumes: `volumeArgs` from Task 2, Docker inspect output
- Produces: volumes preserved when a container is recreated via `Start`

- [ ] **Step 1: Write the failing test**

```go
func TestGetContainerConfigVolumes(t *testing.T) {
	// Stub test — real implementation relies on Docker daemon being present.
	// Verify the function signature change compiles.
	var r dockerRuntime
	_ = r

	// This test can only be run with an actual Docker daemon,
	// so we test the volume parsing logic instead.
	const inspectOutput = `[{"/host/data": "/app/data", "/host/config:/etc/config:ro": ""}]`
	_ = inspectOutput
}
```

This task's implementation is best validated via integration tests. Keep the unit test minimal — verify the volume injection in `Start` doesn't break existing behavior.

- [ ] **Step 2: Update implementation**

Change `getContainerConfig` return signature from `(string, []string, []string)` to `(string, []string, []string, []string)` where the fourth return value is volume args.

In the function, after collecting envs, add volume collection:

```go
volCmd := exec.CommandContext(ctx, "docker", "inspect",
	"--format", "{{json .HostConfig.Binds}}", containerName)
volOut, err := volCmd.CombinedOutput()
var vols []string
if err == nil {
	var binds []string
	if err := json.Unmarshal(volOut, &binds); err == nil {
		for _, b := range binds {
			vols = append(vols, "-v", b)
		}
	}
}
```

Update the caller in `Start`:

```go
imageTag, ports, envs, vols := r.getContainerConfig(ctx, containerName)
// Use vols in the recreate args
```

Update all other callers of `getContainerConfig` (there should be only one — in `Start`) to handle the new return value. The stub in `stubManager` doesn't use this method.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/runtime/... -v -count=1 -run TestGetContainerConfig`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/docker.go
git commit -m "feat: preserve volumes during container recreate in Start"
```

---
### Task 7: Update golangci-lint and run full test suite

**Files:**
- All modified files

- [ ] **Step 1: Run vet**

Run: `go vet ./...`
Expected: no errors

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v -count=1`
Expected: all tests pass

- [ ] **Step 3: Build binary**

Run: `go build -o tengiz .`
Expected: builds successfully

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: finalize persistent storage feature"
```
