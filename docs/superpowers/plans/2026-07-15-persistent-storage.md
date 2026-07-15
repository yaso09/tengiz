# Persistent Storage (Volume Management) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add volume mount management so stateful Tengiz apps (databases, uploads, caches) persist data across container restarts and scale-to-zero cycles.

**Architecture:** Extend `AppConfig` with a `Volumes` field (list of volume specs), add a `volumeArgs()` helper (mirroring `envArgs()`/`resourceArgs()` patterns), add Docker `--volume` flags to both `Create()` and `CreateVersioned()`. New CLI commands `tengiz volume add/remove/list` wire into `Store` methods that persist volume entries in `apps.json`. Follow the exact patterns established by `env` and `domain` management.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI (`os/exec`), JSON file store (`~/.tengiz/apps.json`)

## Global Constraints

- Container names prefixed `tengiz-<appname>`, labeled with `tengiz-app=<appname>`
- Port allocations: 9000-9999, persisted in `~/.tengiz/ports.json`
- Env vars stored in `AppEntry.Config.Env` → auto-persisted via JSON in `~/.tengiz/apps.json`
- `.tengiz.yaml` `env:` section uses `KEY: value` format (map, not list)
- Follow established code style: no comments, concise, mimic existing patterns
- All tests must pass before commit: `go test ./... -v -count=1`
- No new dependencies beyond what's already in `go.mod` (cobra, viper only)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | Add `VolumeConfig` struct, add `Volumes []VolumeConfig` to `AppConfig` |
| `internal/runtime/runtime.go` | Add stub methods for volume operations to `Manager` interface if needed (no — `Store` handles CRUD, runtime only passes `--volume` flags) |
| `internal/runtime/docker.go` | Add `volumeArgs()` helper func. Wire it into `Create()`, `CreateVersioned()`, `Start()` (recreate path). |
| `internal/config/store.go` | Add `AddVolume()`, `RemoveVolume()`, `ListVolumes()` methods on `Store` |
| `internal/cli/root.go` | Add `volumeCmd` + `volumeAddCmd`, `volumeRemoveCmd`, `volumeListCmd` subcommands. Register in `init()`. |
| `internal/runtime/runtime_test.go` | Add tests for `volumeArgs()` |
| `internal/config/store_test.go` | Add tests for volume store CRUD |
| `internal/cli/root_test.go` | Add CLI integration tests for volume commands |

---

### Task 1: Add `VolumeConfig` type and `Volumes` field to `AppConfig`

**Files:**
- Modify: `internal/types/types.go:11-21`

**Interfaces:**
- Consumes: Nothing (first task)
- Produces: `types.VolumeConfig` struct, `types.AppConfig.Volumes` field

- [ ] **Step 1: Write the failing test in types_test.go**

```go
// internal/types/types_test.go
package types

import (
	"encoding/json"
	"testing"
)

func TestVolumeConfigMarshal(t *testing.T) {
	v := VolumeConfig{
		HostPath:      "/data/uploads",
		ContainerPath: "/app/uploads",
		ReadOnly:      true,
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got VolumeConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.HostPath != "/data/uploads" {
		t.Errorf("HostPath = %q, want /data/uploads", got.HostPath)
	}
	if got.ContainerPath != "/app/uploads" {
		t.Errorf("ContainerPath = %q, want /app/uploads", got.ContainerPath)
	}
	if !got.ReadOnly {
		t.Errorf("ReadOnly = false, want true")
	}
}

func TestAppConfigVolumesField(t *testing.T) {
	cfg := AppConfig{
		Name: "testapp",
		Volumes: []VolumeConfig{
			{HostPath: "/data/db", ContainerPath: "/var/lib/data"},
		},
	}
	if len(cfg.Volumes) != 1 {
		t.Fatalf("Volumes length = %d, want 1", len(cfg.Volumes))
	}
	if cfg.Volumes[0].HostPath != "/data/db" {
		t.Errorf("HostPath = %q, want /data/db", cfg.Volumes[0].HostPath)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -count=1 -run TestVolumeConfigMarshal`
Expected: FAIL — `VolumeConfig` undefined

- [ ] **Step 3: Write minimal implementation in types.go**

Add after `HealthCheckConfig` struct:

```go
type VolumeConfig struct {
	HostPath      string `mapstructure:"host_path" json:"host_path"`
	ContainerPath string `mapstructure:"container_path" json:"container_path"`
	ReadOnly      bool   `mapstructure:"read_only" json:"read_only,omitempty"`
}
```

Add after existing fields but before the closing `}` of `AppConfig`:

```go
	Volumes     []VolumeConfig     `mapstructure:"volumes,omitempty" json:"volumes,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add VolumeConfig type and Volumes field to AppConfig"
```

---

### Task 2: Add `volumeArgs()` helper and wire into Docker run commands

**Files:**
- Modify: `internal/runtime/docker.go` (add `volumeArgs()` func, wire into `Create()`, `CreateVersioned()`, `Start()`)
- Modify: `internal/runtime/runtime.go` (no interface change needed — volume config lives in `AppConfig`)

**Interfaces:**
- Consumes: `types.VolumeConfig`, `types.AppConfig.Volumes`
- Produces: `volumeArgs(volumes []types.VolumeConfig) []string` — converts volumes to `--volume host:container[:ro]` args

- [ ] **Step 1: Write the failing test in runtime_test.go**

```go
// Add to internal/runtime/runtime_test.go

func TestVolumeArgs(t *testing.T) {
	tests := []struct {
		name     string
		volumes  []types.VolumeConfig
		expected []string
	}{
		{
			name:     "nil slice",
			volumes:  nil,
			expected: nil,
		},
		{
			name:     "empty slice",
			volumes:  []types.VolumeConfig{},
			expected: nil,
		},
		{
			name: "single volume",
			volumes: []types.VolumeConfig{
				{HostPath: "/data/uploads", ContainerPath: "/app/uploads"},
			},
			expected: []string{"--volume", "/data/uploads:/app/uploads"},
		},
		{
			name: "readonly volume",
			volumes: []types.VolumeConfig{
				{HostPath: "/data/config", ContainerPath: "/etc/config", ReadOnly: true},
			},
			expected: []string{"--volume", "/data/config:/etc/config:ro"},
		},
		{
			name: "multiple volumes",
			volumes: []types.VolumeConfig{
				{HostPath: "/data/db", ContainerPath: "/var/lib/data"},
				{HostPath: "/data/uploads", ContainerPath: "/app/uploads", ReadOnly: true},
			},
			expected: []string{"--volume", "/data/db:/var/lib/data", "--volume", "/data/uploads:/app/uploads:ro"},
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -v -count=1 -run TestVolumeArgs`
Expected: FAIL — `volumeArgs` undefined

- [ ] **Step 3: Write minimal implementation in docker.go**

Add after `func resourceArgs(...)`:

```go
func volumeArgs(volumes []types.VolumeConfig) []string {
	if len(volumes) == 0 {
		return nil
	}
	var args []string
	for _, v := range volumes {
		spec := fmt.Sprintf("%s:%s", v.HostPath, v.ContainerPath)
		if v.ReadOnly {
			spec += ":ro"
		}
		args = append(args, "--volume", spec)
	}
	return args
}
```

Wire into `Create()` by adding after the `resourceArgs` line (line 76):

```go
	args = append(args, volumeArgs(cfg.Volumes)...)
```

Wire into `CreateVersioned()` similarly, after the `resourceArgs` line (line 400):

```go
	args = append(args, volumeArgs(cfg.Volumes)...)
```

Wire into `Start()` — the recreate path uses stored config from `getContainerConfig`. We need to persist volume info so recreation works. Modify the recreate block in `Start()` (lines 102-112) to also capture and re-apply volume args. Add this after the `resourceArgsFromOld` line:

```go
		volumeArgsFromOld := r.getVolumeArgs(ctx, containerName)
		args = append(args, volumeArgsFromOld...)
```

Add the `getVolumeArgs` helper after `getResourceArgs`:

```go
func (r *dockerRuntime) getVolumeArgs(ctx context.Context, containerName string) []string {
	mountsCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{json .Mounts}}", containerName)
	mountsOut, err := mountsCmd.CombinedOutput()
	if err != nil {
		return nil
	}
	var mounts []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Mode        string `json:"Mode"`
		RW          bool   `json:"RW"`
	}
	if err := json.Unmarshal(mountsOut, &mounts); err != nil {
		return nil
	}
	var args []string
	for _, m := range mounts {
		if m.Type != "bind" && m.Type != "volume" {
			continue
		}
		spec := fmt.Sprintf("%s:%s", m.Source, m.Destination)
		if !m.RW {
			spec += ":ro"
		}
		args = append(args, "--volume", spec)
	}
	return args
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat: add volumeArgs helper and wire into Docker run commands"
```

---

### Task 3: Add volume CRUD methods to Store

**Files:**
- Modify: `internal/config/store.go`

**Interfaces:**
- Consumes: `types.VolumeConfig`, `types.AppEntry`
- Produces: `(*Store).AddVolume(appName, hostPath, containerPath, readOnly)`, `(*Store).RemoveVolume(appName, index)`, `(*Store).ListVolumes(appName)`

- [ ] **Step 1: Write the failing test in store_test.go**

```go
// Add to internal/config/store_test.go

func TestStoreAddVolume(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:   "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	if err := s.AddVolume("testapp", "/data/uploads", "/app/uploads", false); err != nil {
		t.Fatalf("AddVolume: %v", err)
	}
	if err := s.AddVolume("testapp", "/data/config", "/etc/config", true); err != nil {
		t.Fatalf("AddVolume: %v", err)
	}

	vols, err := s.ListVolumes("testapp")
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(vols) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(vols))
	}
	if vols[0].HostPath != "/data/uploads" || vols[0].ContainerPath != "/app/uploads" || vols[0].ReadOnly {
		t.Errorf("first volume mismatch: %+v", vols[0])
	}
	if !vols[1].ReadOnly {
		t.Errorf("second volume should be read-only")
	}
}

func TestStoreRemoveVolume(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
			Volumes: []types.VolumeConfig{
				{HostPath: "/data/a", ContainerPath: "/app/a"},
				{HostPath: "/data/b", ContainerPath: "/app/b"},
			},
		},
	})

	// Remove first volume
	if err := s.RemoveVolume("testapp", 0); err != nil {
		t.Fatalf("RemoveVolume: %v", err)
	}

	vols, err := s.ListVolumes("testapp")
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].HostPath != "/data/b" {
		t.Errorf("remaining volume HostPath = %q, want /data/b", vols[0].HostPath)
	}
}

func TestStoreRemoveVolumeIndexOutOfRange(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:   "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	err := s.RemoveVolume("testapp", 0)
	if err == nil {
		t.Fatal("expected error for out-of-range index")
	}
}

func TestStoreListVolumesNoApp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	_, err := s.ListVolumes("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
}

func TestStoreListVolumesEmpty(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:   "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	vols, err := s.ListVolumes("testapp")
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(vols) != 0 {
		t.Fatalf("expected 0 volumes, got %d", len(vols))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -count=1 -run TestStoreAddVolume`
Expected: FAIL — `AddVolume` undefined

- [ ] **Step 3: Write minimal implementation in store.go**

Add after `ListDomains` method:

```go
func (s *Store) AddVolume(appName, hostPath, containerPath string, readOnly bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	if app.Config.Volumes == nil {
		app.Config.Volumes = make([]types.VolumeConfig, 0)
	}
	app.Config.Volumes = append(app.Config.Volumes, types.VolumeConfig{
		HostPath:      hostPath,
		ContainerPath: containerPath,
		ReadOnly:      readOnly,
	})
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) RemoveVolume(appName string, index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	if index < 0 || index >= len(app.Config.Volumes) {
		return fmt.Errorf("volume index %d out of range (len=%d)", index, len(app.Config.Volumes))
	}
	app.Config.Volumes = append(app.Config.Volumes[:index], app.Config.Volumes[index+1:]...)
	if len(app.Config.Volumes) == 0 {
		app.Config.Volumes = nil
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

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: add volume CRUD methods to Store"
```

---

### Task 4: Add `tengiz volume` CLI commands

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `(*Store).AddVolume`, `(*Store).RemoveVolume`, `(*Store).ListVolumes`
- Produces: CLI commands `tengiz volume add <app> <host_path>:<container_path>`, `tengiz volume remove <app> <index>`, `tengiz volume list <app>`

- [ ] **Step 1: Write the failing test in root_test.go**

```go
// Add to internal/cli/root_test.go

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)

func TestVolumeAddRemoveList(t *testing.T) {
	dataDir = t.TempDir()
	store := config.NewStore(dataDir)

	// Pre-create an app
	store.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
		},
	})

	// Add volume via store directly (CLI uses same store underneath)
	if err := store.AddVolume("testapp", "/host/data", "/container/data", false); err != nil {
		t.Fatalf("AddVolume: %v", err)
	}

	vols, err := store.ListVolumes("testapp")
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(vols) != 1 || vols[0].HostPath != "/host/data" || vols[0].ContainerPath != "/container/data" {
		t.Fatalf("unexpected volumes: %+v", vols)
	}

	// Remove volume
	if err := store.RemoveVolume("testapp", 0); err != nil {
		t.Fatalf("RemoveVolume: %v", err)
	}

	vols, err = store.ListVolumes("testapp")
	if err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if len(vols) != 0 {
		t.Fatalf("expected 0 volumes after remove, got %d", len(vols))
	}
}

func TestVolumeAddRequiresApp(t *testing.T) {
	dataDir = t.TempDir()
	store := config.NewStore(dataDir)

	err := store.AddVolume("nonexistent", "/host/data", "/container/data", false)
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
}
```

- [ ] **Step 2: Run test to verify the store-level operations pass (since we already implemented store, just verifying CLI integration)**

Run: `go test ./internal/cli/... -v -count=1 -run TestVolumeAddRemoveList`
Expected: PASS (since store methods are already implemented)

- [ ] **Step 3: Add volume command tree to init() and command definitions in root.go**

In `init()` after `domainCmd` registrations (around line 47), add:

```go
	volumeCmd.AddCommand(volumeAddCmd)
	volumeCmd.AddCommand(volumeRemoveCmd)
	volumeCmd.AddCommand(volumeListCmd)
	rootCmd.AddCommand(volumeCmd)
```

Add command definitions after `domainListCmd` (around line 607):

```go
var volumeCmd = &cobra.Command{
	Use:   "volume",
	Short: "Manage persistent storage volumes for applications",
}

var volumeAddCmd = &cobra.Command{
	Use:   "add <app> <host_path:container_path>",
	Short: "Add a volume mount to an application",
	Long: `Mount a host path or Docker volume into the application container.
Format: <host_path>:<container_path>[:ro]

Examples:
  tengiz volume add myapp /data/uploads:/app/uploads
  tengiz volume add myapp myvolume:/var/lib/data:ro`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		spec := args[1]
		parts := strings.SplitN(spec, ":", 3)
		if len(parts) < 2 {
			return fmt.Errorf("invalid volume spec %q: expected host_path:container_path[:ro]", spec)
		}
		hostPath := parts[0]
		containerPath := parts[1]
		readOnly := false
		if len(parts) == 3 && parts[2] == "ro" {
			readOnly = true
		}

		store := config.NewStore(dataDir)
		if err := store.AddVolume(appName, hostPath, containerPath, readOnly); err != nil {
			return err
		}
		fmt.Printf("[tengiz] volume added: %s\n", spec)
		return nil
	},
}

var volumeRemoveCmd = &cobra.Command{
	Use:   "remove <app> <index>",
	Short: "Remove a volume mount from an application by index",
	Long: `Remove a volume mount by its index. Use 'tengiz volume list <app>' to see indices.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		index, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid index %q: must be a number", args[1])
		}

		store := config.NewStore(dataDir)
		if err := store.RemoveVolume(appName, index); err != nil {
			return err
		}
		fmt.Printf("[tengiz] volume #%d removed from %s\n", index, appName)
		return nil
	},
}

var volumeListCmd = &cobra.Command{
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
		fmt.Printf("%-3s %-30s %-30s %-6s\n", "#", "HOST PATH", "CONTAINER PATH", "MODE")
		for i, v := range vols {
			mode := "rw"
			if v.ReadOnly {
				mode = "ro"
			}
			fmt.Printf("%-3d %-30s %-30s %-6s\n", i, v.HostPath, v.ContainerPath, mode)
		}
		return nil
	},
}
```

Add to imports in root.go (add `"strconv"` and `"strings"` to the import block):

```go
import (
	...
	"strconv"
	"strings"
	...
)
```

- [ ] **Step 4: Run test to verify it compiles and passes**

Run: `go build ./... && go test ./internal/cli/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `go vet ./... && go test ./... -v -count=1`
Expected: PASS (all existing tests, no regressions)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add volume CLI commands (add/remove/list)"
```

---

## Self-Review

**1. Spec coverage:**
- Volume type and config field: Task 1
- Docker runtime integration: Task 2
- Persistent CRUD in store: Task 3
- CLI commands: Task 4
- Tests for each layer: each task includes tests

**2. Placeholder scan:** All steps contain complete code, exact file paths, and expected test output. No TBD/TODO/filler patterns.

**3. Type consistency:** `types.VolumeConfig` defined in Task 1 with `HostPath`, `ContainerPath`, `ReadOnly` fields. Used consistently in `volumeArgs()` (Task 2), `Store` methods (Task 3), and CLI commands (Task 4). All method signatures match across tasks.
