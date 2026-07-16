# Persistent Storage (Volume Management) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add volume mount management so stateful apps (databases, uploads) survive container restarts and scale-to-zero cycles.

**Architecture:** A new `VolumeBinding` type is added to `types` and a `Volumes` slice on `AppConfig`. The `runtime` package gets a `volumeArgs()` helper (mirroring `envArgs`/`resourceArgs`) that produces `-v` Docker flags, integrated into `Create()` and `CreateVersioned()`. A `tengiz volume` CLI command group (add/remove/list) manipulates volumes in the persisted store and sends proxy notifications. Config auto-persists volumes through the existing `AppEntry.Config` JSON round-trip.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI via `os/exec`, viper config

## Global Constraints

- No new dependencies beyond `cobra` and `viper`
- Container names are prefixed `tengiz-<appname>`, labeled with `tengiz-app=<appname>`
- All new fields on `AppConfig` get both `mapstructure` and `json` tags with `omitempty`
- Tests use `t.TempDir()` for isolation, same-package white-box pattern
- Volume bindings stored in `AppConfig.Volumes` auto-persist via `AppEntry.Config` JSON serialization

---

## File Structure

| File | Change | Responsibility |
|------|--------|----------------|
| `internal/types/types.go` | Create `VolumeBinding` type, add `Volumes []VolumeBinding` to `AppConfig` | Data model |
| `internal/runtime/docker.go` | Create `volumeArgs()` helper, integrate into `Create()` and `CreateVersioned()` | Docker CLI arg generation |
| `internal/config/store.go` | Add `AddVolume`/`RemoveVolume`/`ListVolumes` methods | Persistence layer |
| `internal/cli/root.go` | Add `volumeCmd` group with `volumeAddCmd`/`volumeRemoveCmd`/`volumeListCmd` subcommands | CLI entry points |
| `internal/cli/root_test.go` | Add tests for volume CLI commands | CLI tests |
| `internal/types/types_test.go` | Add tests for `VolumeBinding` serialization | Type tests |
| `internal/runtime/runtime_test.go` | Add tests for `volumeArgs()` helper | Runtime tests |
| `internal/config/store_test.go` | Add tests for volume store methods | Store tests |

---

### Task 1: Define VolumeBinding type and extend AppConfig

**Files:**
- Modify: `internal/types/types.go`
- Test: `internal/types/types_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `types.VolumeBinding` struct, `types.AppConfig.Volumes` field

- [ ] **Step 1: Write the failing test**

```go
// internal/types/types_test.go (append to file)

func TestVolumeBindingSerialization(t *testing.T) {
	cfg := AppConfig{
		Name: "testapp",
		Volumes: []VolumeBinding{
			{HostPath: "/data/db", ContainerPath: "/var/lib/mysql", ReadOnly: false},
			{HostPath: "myvolume", ContainerPath: "/data", ReadOnly: true},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var got AppConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(got.Volumes) != 2 {
		t.Fatalf("got %d volumes, want 2", len(got.Volumes))
	}
	if got.Volumes[0].HostPath != "/data/db" {
		t.Errorf("HostPath = %q, want /data/db", got.Volumes[0].HostPath)
	}
	if got.Volumes[1].ReadOnly != true {
		t.Errorf("ReadOnly = %v, want true", got.Volumes[1].ReadOnly)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run TestVolumeBindingSerialization -count=1`
Expected: FAIL — `VolumeBinding` not defined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/types/types.go — add after ResourceConfig

type VolumeBinding struct {
	HostPath      string `mapstructure:"host_path" json:"host_path,omitempty"`
	ContainerPath string `mapstructure:"container_path" json:"container_path,omitempty"`
	ReadOnly      bool   `mapstructure:"read_only" json:"read_only,omitempty"`
}
```

```go
// internal/types/types.go — add Volumes field to AppConfig

type AppConfig struct {
	// ... existing fields ...
	Volumes     []VolumeBinding     `mapstructure:"volumes,omitempty" json:"volumes,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run TestVolumeBindingSerialization -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat(types): add VolumeBinding type and Volumes field to AppConfig"
```

---

### Task 2: Add volumeArgs() helper and integrate into Create/CreateVersioned

**Files:**
- Modify: `internal/runtime/docker.go`
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: `types.VolumeBinding`, `types.AppConfig.Volumes`
- Produces: `volumeArgs([]types.VolumeBinding) []string` helper function

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/runtime_test.go (append)

func TestVolumeArgs(t *testing.T) {
	tests := []struct {
		name     string
		volumes  []types.VolumeBinding
		expected []string
	}{
		{
			name:     "nil volumes",
			volumes:  nil,
			expected: nil,
		},
		{
			name:     "empty volumes",
			volumes:  []types.VolumeBinding{},
			expected: nil,
		},
		{
			name: "host path mount",
			volumes: []types.VolumeBinding{
				{HostPath: "/data/db", ContainerPath: "/var/lib/mysql"},
			},
			expected: []string{"-v", "/data/db:/var/lib/mysql"},
		},
		{
			name: "named volume mount",
			volumes: []types.VolumeBinding{
				{HostPath: "myvolume", ContainerPath: "/data"},
			},
			expected: []string{"-v", "myvolume:/data"},
		},
		{
			name: "read-only mount",
			volumes: []types.VolumeBinding{
				{HostPath: "/host", ContainerPath: "/container", ReadOnly: true},
			},
			expected: []string{"-v", "/host:/container:ro"},
		},
		{
			name: "multiple volumes",
			volumes: []types.VolumeBinding{
				{HostPath: "/vol1", ContainerPath: "/c1"},
				{HostPath: "vol2", ContainerPath: "/c2", ReadOnly: true},
			},
			expected: []string{"-v", "/vol1:/c1", "-v", "vol2:/c2:ro"},
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

Run: `go test ./internal/runtime/... -v -run TestVolumeArgs -count=1`
Expected: FAIL — `volumeArgs` not defined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/runtime/docker.go — add after resourceArgs()

func volumeArgs(volumes []types.VolumeBinding) []string {
	if len(volumes) == 0 {
		return nil
	}
	var args []string
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

```go
// internal/runtime/docker.go — add volumeArgs() call in Create(), after resourceArgs on line 76

	args = append(args, volumeArgs(cfg.Volumes)...)
```

```go
// internal/runtime/docker.go — add volumeArgs() call in CreateVersioned(), after resourceArgs on line 400

	args = append(args, volumeArgs(cfg.Volumes)...)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -v -run TestVolumeArgs -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat(runtime): add volumeArgs helper and pass volumes to docker run"
```

---

### Task 3: Add volume store methods

**Files:**
- Modify: `internal/config/store.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `types.VolumeBinding`
- Produces: `(*Store) AddVolume(appName, hostPath, containerPath string, readOnly bool) error`
- Produces: `(*Store) RemoveVolume(appName, containerPath string) error`
- Produces: `(*Store) ListVolumes(appName string) ([]types.VolumeBinding, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/config/store_test.go (append)

func TestStoreVolumes(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{Name: "myapp"})

	// AddVolume
	if err := s.AddVolume("myapp", "/host/data", "/container/data", false); err != nil {
		t.Fatalf("AddVolume error: %v", err)
	}

	// ListVolumes
	vols, err := s.ListVolumes("myapp")
	if err != nil {
		t.Fatalf("ListVolumes error: %v", err)
	}
	if len(vols) != 1 {
		t.Fatalf("got %d volumes, want 1", len(vols))
	}
	if vols[0].HostPath != "/host/data" || vols[0].ContainerPath != "/container/data" || vols[0].ReadOnly {
		t.Errorf("volume = %+v, want {/host/data /container/data false}", vols[0])
	}

	// AddVolume with read-only
	if err := s.AddVolume("myapp", "myvol", "/data", true); err != nil {
		t.Fatalf("AddVolume read-only error: %v", err)
	}
	vols, _ = s.ListVolumes("myapp")
	if len(vols) != 2 {
		t.Fatalf("got %d volumes, want 2", len(vols))
	}

	// RemoveVolume
	if err := s.RemoveVolume("myapp", "/container/data"); err != nil {
		t.Fatalf("RemoveVolume error: %v", err)
	}
	vols, _ = s.ListVolumes("myapp")
	if len(vols) != 1 {
		t.Fatalf("after remove got %d volumes, want 1", len(vols))
	}
	if vols[0].ContainerPath != "/data" {
		t.Errorf("remaining volume ContainerPath = %q, want /data", vols[0].ContainerPath)
	}

	// RemoveVolume nonexistent
	err = s.RemoveVolume("myapp", "/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent volume")
	}

	// ListVolumes for nonexistent app
	_, err = s.ListVolumes("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run TestStoreVolumes -count=1`
Expected: FAIL — `AddVolume` not defined on Store

- [ ] **Step 3: Write minimal implementation**

```go
// internal/config/store.go — append after ListDomains

func (s *Store) AddVolume(appName, hostPath, containerPath string, readOnly bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	for _, v := range app.Config.Volumes {
		if v.ContainerPath == containerPath {
			return fmt.Errorf("volume with container path %q already exists for app %q", containerPath, appName)
		}
	}
	app.Config.Volumes = append(app.Config.Volumes, types.VolumeBinding{
		HostPath:      hostPath,
		ContainerPath: containerPath,
		ReadOnly:      readOnly,
	})
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) RemoveVolume(appName, containerPath string) error {
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
		if v.ContainerPath == containerPath {
			app.Config.Volumes = append(app.Config.Volumes[:i], app.Config.Volumes[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("volume with container path %q not found for app %q", containerPath, appName)
	}
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) ListVolumes(appName string) ([]types.VolumeBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return nil, fmt.Errorf("app %q not found", appName)
	}
	result := make([]types.VolumeBinding, len(app.Config.Volumes))
	copy(result, app.Config.Volumes)
	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run TestStoreVolumes -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat(config): add volume store methods (AddVolume/RemoveVolume/ListVolumes)"
```

---

### Task 4: Add volume CLI command group

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `config.Store.AddVolume`, `config.Store.RemoveVolume`, `config.Store.ListVolumes`
- Produces: `tengiz volume add <app> <host_path>:<container_path>`, `tengiz volume remove <app> <container_path>`, `tengiz volume list <app>` commands

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/root_test.go (create or append)

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)

func setupTestDir(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".tengiz")
	return dir, func() {
		os.RemoveAll(dataDir)
	}
}

func TestVolumeCommands(t *testing.T) {
	dir := t.TempDir()
	s := config.NewStore(dir)
	s.SaveApp(types.AppEntry{Name: "testapp"})

	// Override dataDir for CLI commands
	origDataDir := dataDir
	dataDir = dir
	defer func() { dataDir = origDataDir }()

	// Test volume add
	addCmd := volumeAddCmd
	addCmd.SetArgs([]string{"testapp", "/host/path:/container/path"})
	buf := new(bytes.Buffer)
	addCmd.SetOut(buf)
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("volume add error: %v", err)
	}

	vols, _ := s.ListVolumes("testapp")
	if len(vols) != 1 {
		t.Fatalf("got %d volumes, want 1 after add", len(vols))
	}
	if vols[0].HostPath != "/host/path" || vols[0].ContainerPath != "/container/path" {
		t.Errorf("volume = %+v", vols[0])
	}

	// Test volume add with :ro suffix
	addCmd.SetArgs([]string{"testapp", "/host/ro:/container/ro:ro"})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("volume add ro error: %v", err)
	}
	vols, _ = s.ListVolumes("testapp")
	if len(vols) != 2 {
		t.Fatalf("got %d volumes, want 2", len(vols))
	}
	if !vols[1].ReadOnly {
		t.Errorf("expected read-only volume")
	}

	// Test volume list
	listCmd := volumeListCmd
	listCmd.SetArgs([]string{"testapp"})
	listBuf := new(bytes.Buffer)
	listCmd.SetOut(listBuf)
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("volume list error: %v", err)
	}

	// Test volume remove
	rmCmd := volumeRemoveCmd
	rmCmd.SetArgs([]string{"testapp", "/container/path"})
	if err := rmCmd.Execute(); err != nil {
		t.Fatalf("volume remove error: %v", err)
	}
	vols, _ = s.ListVolumes("testapp")
	if len(vols) != 1 {
		t.Fatalf("got %d volumes, want 1 after remove", len(vols))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -run TestVolumeCommands -count=1`
Expected: FAIL — `volumeAddCmd` not defined

- [ ] **Step 3: Write minimal implementation**

Add volume command group with subcommands to `internal/cli/root.go`:

```go
// internal/cli/root.go — add after domainCmd group (around line 607)

var volumeCmd = &cobra.Command{
	Use:   "volume",
	Short: "Manage persistent storage volumes for applications",
}

var volumeAddCmd = &cobra.Command{
	Use:   "add <app> <host_path>:<container_path>[:ro]",
	Short: "Mount a volume to an application",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		spec := args[1]

		hostPath, containerPath, readOnly := parseVolumeSpec(spec)
		if hostPath == "" || containerPath == "" {
			return fmt.Errorf("invalid volume spec: use <host_path>:<container_path>[:ro]")
		}

		store := config.NewStore(dataDir)
		if err := store.AddVolume(appName, hostPath, containerPath, readOnly); err != nil {
			return err
		}
		mode := "rw"
		if readOnly {
			mode = "ro"
		}
		fmt.Printf("[tengiz] volume mounted: %s:%s (%s)\n", hostPath, containerPath, mode)
		return nil
	},
}

var volumeRemoveCmd = &cobra.Command{
	Use:   "remove <app> <container_path>",
	Short: "Unmount a volume from an application",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		containerPath := args[1]

		store := config.NewStore(dataDir)
		if err := store.RemoveVolume(appName, containerPath); err != nil {
			return err
		}
		fmt.Printf("[tengiz] volume unmounted: %s from %s\n", containerPath, appName)
		return nil
	},
}

var volumeListCmd = &cobra.Command{
	Use:   "list <app>",
	Short: "List volumes mounted to an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		store := config.NewStore(dataDir)

		volumes, err := store.ListVolumes(appName)
		if err != nil {
			return err
		}
		if len(volumes) == 0 {
			fmt.Printf("No volumes mounted for %s.\n", appName)
			return nil
		}
		fmt.Printf("%-30s %-30s %-6s\n", "HOST PATH / VOLUME", "CONTAINER PATH", "MODE")
		for _, v := range volumes {
			mode := "rw"
			if v.ReadOnly {
				mode = "ro"
			}
			fmt.Printf("%-30s %-30s %-6s\n", v.HostPath, v.ContainerPath, mode)
		}
		return nil
	},
}

// parseVolumeSpec parses "host:container" or "host:container:ro"
func parseVolumeSpec(spec string) (hostPath, containerPath string, readOnly bool) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) < 2 {
		return "", "", false
	}
	hostPath = parts[0]
	containerPath = parts[1]
	if len(parts) >= 3 && parts[2] == "ro" {
		readOnly = true
	}
	return
}
```

Add to `init()` in `root.go` — register the command group:
```go
// internal/cli/root.go — add in init() after domainCmd.AddCommand lines (around line 47)

	volumeCmd.AddCommand(volumeAddCmd)
	volumeCmd.AddCommand(volumeRemoveCmd)
	volumeCmd.AddCommand(volumeListCmd)
	rootCmd.AddCommand(volumeCmd)
```

Add `strings` import if not already present (check imports — already imported at line 15).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -v -run TestVolumeCommands -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat(cli): add volume command group (add/remove/list)"
```

---

### Task 5: Update deploy command to pass stored volumes

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `config.Store.ListVolumes`
- Produces: volumes loaded from store are set on `cfg.Volumes` before `rt.Create()`/`rt.CreateVersioned()`

- [ ] **Step 1: Locate the deploy flow in root.go**

The deploy command at lines 120-280 loads `cfg` from config, then calls `rt.Create()` or `rt.CreateVersioned()`. Stored volumes need to be merged into `cfg.Volumes` before the create call.

- [ ] **Step 2: Write the failing test** — this is a behavioral change. The test needs to verify that volumes from the store are passed through to `rt.Create()`.

Since `rt.Create()` calls `volumeArgs(cfg.Volumes)` which we already test, the integration test can verify end-to-end using the store:

```go
// internal/cli/root_test.go (append to TestVolumeCommands)

func TestDeployLoadsStoredVolumes(t *testing.T) {
	dir := t.TempDir()
	s := config.NewStore(dir)
	s.SaveApp(types.AppEntry{Name: "volapp"})
	s.AddVolume("volapp", "/host/data", "/container/data", false)

	app, err := s.GetApp("volapp")
	if err != nil {
		t.Fatalf("GetApp error: %v", err)
	}
	if len(app.Config.Volumes) != 1 {
		t.Fatalf("got %d volumes, want 1", len(app.Config.Volumes))
	}
	if app.Config.Volumes[0].HostPath != "/host/data" {
		t.Errorf("volume HostPath = %q, want /host/data", app.Config.Volumes[0].HostPath)
	}
}
```

- [ ] **Step 3: Write minimal implementation**

Modify the deploy command in `root.go` to merge stored volumes into `cfg.Volumes` before calling `rt.Create()` or `rt.CreateVersioned()`. Add after the `store := config.NewStore(dataDir)` line (line 171):

```go
		// Load stored volumes into config
		if storedVols, err := store.ListVolumes(cfg.Name); err == nil && len(storedVols) > 0 {
			cfg.Volumes = append(cfg.Volumes, storedVols...)
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -v -run TestDeployLoadsStoredVolumes -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat(cli): load stored volumes during deploy"
```

---

### Task 6: Update volume command to notify proxy and handle redeploy

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `proxy.RegisterRouteWithProxy`
- Produces: proxy notifications on volume change

- [ ] **Step 1: Add proxy notification to volume add/remove**

After store operations in `volumeAddCmd` and `volumeRemoveCmd`, notify proxy if running. Similar to how `domainAddCmd` does it at line 554.

In `volumeAddCmd`, after successful store save:

```go
		// Notify proxy to pick up new volume config (restart container)
		if err := proxy.RegisterRouteWithProxy(appName, 0); err != nil {
			fmt.Printf("[tengiz] volume saved, but proxy not running: %v\n", err)
			fmt.Printf("[tengiz] run 'tengiz redeploy %s' to apply volume changes\n", appName)
		}
```

For `volumeRemoveCmd`, after successful store save:

```go
		// Notify proxy of volume change
		if err := proxy.RegisterRouteWithProxy(appName, 0); err != nil {
			fmt.Printf("[tengiz] volume saved, but proxy not running: %v\n", err)
			fmt.Printf("[tengiz] run 'tengiz redeploy %s' to apply volume changes\n", appName)
		}
```

- [ ] **Step 2: Run existing tests**

Run: `go test ./internal/cli/... -v -count=1`
Expected: PASS (proxy error is handled gracefully)

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat(cli): notify proxy on volume add/remove"
```

---

### Task 7: Update tengiz rm to handle volume cleanup

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `(*Store).RemoveApp`

- [ ] **Step 1: Add Docker volume cleanup on app removal**

Modify the `rmCmd` run function to also prompt or notify about Docker volumes. Add after `store.RemoveApp(args[0])` (line 420):

```go
		// List volumes and notify user to clean them up
		if vols, err := store.ListVolumes(args[0]); err == nil && len(vols) > 0 {
			fmt.Printf("[tengiz] warning: %d volume(s) are still mounted. Remove with:\n", len(vols))
			for _, v := range vols {
				fmt.Printf("  docker volume rm %s  (or: rm -rf %s)\n", v.HostPath, v.HostPath)
			}
		}
```

- [ ] **Step 2: Run existing tests**

Run: `go build ./...`
Expected: OK

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat(cli): warn about dangling volumes on app removal"
```

---

### Task 8: Run all tests and verify

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: All tests pass

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: No warnings

- [ ] **Step 3: Commit any accumulated changes**

```bash
git add -A
git commit -m "test: add volume management tests for all layers"
```

---

## Self-Review

### 1. Spec coverage
- Spec requires volume mount management (`storage:mount`, `storage:unmount`, `storage:list`) → Task 1-6 cover `VolumeBinding` type, store methods, CLI commands
- Spec requires `runtime.Run()`'a `--volume` eklenir → Task 2 adds `volumeArgs()` to `Create()` and `CreateVersioned()`
- Spec requires persistence → Task 3, Task 5 cover store persistence and loading
- Spec requires Docker volume or host path → Task 1 `VolumeBinding.HostPath` supports both absolute paths and named volumes
- Spec mentions read-only mount → Task 1 includes `ReadOnly` field
- All spec requirements are covered

### 2. Placeholder scan
- No "TBD", "TODO", or placeholder patterns found
- All code blocks contain complete, compilable Go code
- All test inputs are concrete and specific
- All file paths are exact

### 3. Type consistency
- `VolumeBinding` struct used consistently across all tasks
- `AppConfig.Volumes` field name consistent across types, runtime, store, and CLI
- `volumeArgs()` function signature: `([]types.VolumeBinding) []string` — same in tests and implementation
- `AddVolume(appName, hostPath, containerPath, readOnly)` — identical in store definition and CLI call site
- No inconsistencies found

---

## Verification

Before committing any task, always run:
```bash
go build ./...
go test ./internal/<affected-package>/... -v -count=1
```

Final verification before completion:
```bash
go build ./...
go test ./... -v -count=1
go vet ./...
```
