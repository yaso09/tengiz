# Persistent Storage (Volume Management) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Docker volume mount support so stateful apps (databases, uploads) survive container restarts under scale-to-zero.

**Architecture:** Extend `AppConfig` with a `Volumes` field (list of `host_path:container_path` strings). Add `volumeArgs()` helper in the runtime package emitting `--volume` flags. Add CLI commands `tengiz storage mount/unmount/list`. Persist volume mounts in `AppEntry.Config.Volumes` for full lifecycle management.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI (`os/exec`), viper config

## Global Constraints

- Container names prefixed `tengiz-<appname>`, labeled with `tengiz-app=<appname>`
- Port allocations: 9000-9999, persisted in `~/.tengiz/ports.json`
- Env vars stored in `AppEntry.Config` → auto-persisted via JSON in `~/.tengiz/apps.json`
- `.tengiz.yaml` uses map-style config sections
- No new external dependencies
- Tests must pass with `go test ./... -v -count=1`

---

### Task 1: Add Volume Types

**Files:**
- Modify: `internal/types/types.go`
- Test: `internal/types/types_test.go`

**Interfaces:**
- Produces: `types.VolumeConfig` struct, `Volumes` field on `types.AppConfig` and `types.AppEntry`

- [ ] **Step 1: Write the failing test**

```go
// internal/types/types_test.go — add to existing file
package types

import (
	"encoding/json"
	"testing"
)

func TestVolumeConfigMarshal(t *testing.T) {
	cfg := AppConfig{
		Name: "testapp",
		Volumes: []VolumeConfig{
			{HostPath: "/data/uploads", ContainerPath: "/app/uploads"},
			{HostPath: "mydbdata", ContainerPath: "/var/lib/data"},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	var decoded AppConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if len(decoded.Volumes) != 2 {
		t.Fatalf("Volumes length = %d, want 2", len(decoded.Volumes))
	}
	if decoded.Volumes[0].HostPath != "/data/uploads" {
		t.Errorf("Volumes[0].HostPath = %q, want /data/uploads", decoded.Volumes[0].HostPath)
	}
	if decoded.Volumes[0].ContainerPath != "/app/uploads" {
		t.Errorf("Volumes[0].ContainerPath = %q, want /app/uploads", decoded.Volumes[0].ContainerPath)
	}
}

func TestVolumeConfigEmpty(t *testing.T) {
	cfg := AppConfig{Name: "testapp"}
	if cfg.Volumes != nil {
		t.Fatal("Volumes should be nil when not set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -count=1 -run TestVolumeConfig`
Expected: compilation error — `VolumeConfig` undefined

- [ ] **Step 3: Write minimal implementation**

```go
// Add to internal/types/types.go (after ResourceConfig)
type VolumeConfig struct {
	HostPath      string `mapstructure:"host_path" yaml:"host_path" json:"host_path"`
	ContainerPath string `mapstructure:"container_path" yaml:"container_path" json:"container_path"`
}

// Add Volumes field to AppConfig struct after Resources:
	Volumes     []VolumeConfig     `mapstructure:"volumes,omitempty" json:"volumes,omitempty"`

// No change needed for AppEntry—it embeds AppConfig via Config field
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -count=1 -run TestVolumeConfig`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat(types): add VolumeConfig struct and Volumes field to AppConfig"
```

---

### Task 2: Add volumeArgs Helper to Runtime

**Files:**
- Modify: `internal/runtime/docker.go`
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: `types.VolumeConfig` (slice of `{HostPath, ContainerPath}`)
- Produces: `volumeArgs(volumes []types.VolumeConfig) []string` — returns `["--volume", "/host:/container", ...]`

- [ ] **Step 1: Write the failing test**

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
			name: "multiple volumes",
			volumes: []types.VolumeConfig{
				{HostPath: "/data/uploads", ContainerPath: "/app/uploads"},
				{HostPath: "mydbdata", ContainerPath: "/var/lib/data"},
			},
			expected: []string{
				"--volume", "/data/uploads:/app/uploads",
				"--volume", "mydbdata:/var/lib/data",
			},
		},
		{
			name: "named volume only",
			volumes: []types.VolumeConfig{
				{HostPath: "mydbdata", ContainerPath: "/var/lib/data"},
			},
			expected: []string{"--volume", "mydbdata:/var/lib/data"},
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

func TestStubCreateWithVolumes(t *testing.T) {
	var m Manager = NewStub()
	cfg := &types.AppConfig{
		Name: "testapp",
		Volumes: []types.VolumeConfig{
			{HostPath: "/data/uploads", ContainerPath: "/app/uploads"},
		},
	}
	if err := m.Create(context.Background(), cfg, "test:latest", 9000); err != nil {
		t.Fatalf("Create with volumes: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -v -count=1 -run TestVolumeArgs`
Expected: compilation error — `volumeArgs` undefined (or FAIL if you write test after helper)

- [ ] **Step 3: Write minimal implementation**

```go
// Add to internal/runtime/docker.go (near envArgs/resourceArgs)
func volumeArgs(volumes []types.VolumeConfig) []string {
	if len(volumes) == 0 {
		return nil
	}
	var args []string
	for _, v := range volumes {
		args = append(args, "--volume", fmt.Sprintf("%s:%s", v.HostPath, v.ContainerPath))
	}
	return args
}
```

- [ ] **Step 4: Wire into Create and CreateVersioned**

```go
// In docker.go Create(), after resourceArgs line:
	args = append(args, resourceArgs(cfg.Resources)...)
	args = append(args, volumeArgs(cfg.Volumes)...)  // ADD THIS LINE

// In docker.go CreateVersioned(), after resourceArgs line:
	args = append(args, resourceArgs(cfg.Resources)...)
	args = append(args, volumeArgs(cfg.Volumes)...)  // ADD THIS LINE
```

- [ ] **Step 5: Run all tests to verify**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat(runtime): add volumeArgs helper and wire into container creation"
```

---

### Task 3: Config Loading for Volumes

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `types.AppConfig.Volumes`
- Volumes loaded from `.tengiz.yaml` via viper `volumes` key

- [ ] **Step 1: Write the failing test**

```go
// Add to internal/config/config_test.go
func TestLoadWithVolumes(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: volapp
port: 8080
volumes:
  - host_path: /data/uploads
    container_path: /app/uploads
  - host_path: mydbdata
    container_path: /var/lib/data
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Volumes) != 2 {
		t.Fatalf("Volumes length = %d, want 2", len(cfg.Volumes))
	}
	if cfg.Volumes[0].HostPath != "/data/uploads" {
		t.Errorf("Volumes[0].HostPath = %q, want /data/uploads", cfg.Volumes[0].HostPath)
	}
	if cfg.Volumes[0].ContainerPath != "/app/uploads" {
		t.Errorf("Volumes[0].ContainerPath = %q, want /app/uploads", cfg.Volumes[0].ContainerPath)
	}
	if cfg.Volumes[1].HostPath != "mydbdata" {
		t.Errorf("Volumes[1].HostPath = %q, want mydbdata", cfg.Volumes[1].HostPath)
	}
	if cfg.Volumes[1].ContainerPath != "/var/lib/data" {
		t.Errorf("Volumes[1].ContainerPath = %q, want /var/lib/data", cfg.Volumes[1].ContainerPath)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -count=1 -run TestLoadWithVolumes`
Expected: FAIL — `Volumes` field not populated (viper unmarshal should work because `mapstructure` tag is set on `VolumeConfig`, but verify)

- [ ] **Step 3: Verify viper unmarshal works (this should already work because viper handles slices of structs via mapstructure tags — run test to see)**

If test passes, skip to step 5. If test fails, add viper configuration support:

No code change needed if `mapstructure` tags are present (they were added in Task 1). Viper's `Unmarshal` handles `[]VolumeConfig` automatically via `mapstructure` tags. The test should pass.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -count=1 -run TestLoadWithVolumes`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config_test.go
git commit -m "test(config): add config loading test for volumes"
```

---

### Task 4: Storage CRUD in Store

**Files:**
- Modify: `internal/config/store.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `types.AppEntry`, `types.VolumeConfig`, `types.AppConfig`
- Produces: `AddVolume(appName, hostPath, containerPath) error`, `RemoveVolume(appName, hostPath) error`, `ListVolumes(appName) ([]types.VolumeConfig, error)`

- [ ] **Step 1: Write the failing tests**

```go
// Add to internal/config/store_test.go
func TestStoreAddVolume(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:   "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	if err := s.AddVolume("testapp", "/data/uploads", "/app/uploads"); err != nil {
		t.Fatalf("AddVolume: %v", err)
	}
	if err := s.AddVolume("testapp", "mydbdata", "/var/lib/data"); err != nil {
		t.Fatalf("AddVolume: %v", err)
	}

	app, err := s.GetApp("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(app.Config.Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(app.Config.Volumes))
	}
	if app.Config.Volumes[0].HostPath != "/data/uploads" {
		t.Errorf("Volumes[0].HostPath = %q, want /data/uploads", app.Config.Volumes[0].HostPath)
	}
}

func TestStoreAddVolumeDuplicate(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name:   "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	s.AddVolume("testapp", "/data/uploads", "/app/uploads")
	err := s.AddVolume("testapp", "/data/uploads", "/app/uploads")
	if err == nil {
		t.Fatal("expected error for duplicate volume host_path")
	}
}

func TestStoreAddVolumeNoApp(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	err := s.AddVolume("nonexistent", "/data", "/data")
	if err == nil {
		t.Fatal("expected error for nonexistent app")
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
				{HostPath: "/data/uploads", ContainerPath: "/app/uploads"},
				{HostPath: "mydbdata", ContainerPath: "/var/lib/data"},
			},
		},
	})

	if err := s.RemoveVolume("testapp", "/data/uploads"); err != nil {
		t.Fatalf("RemoveVolume: %v", err)
	}

	vols, err := s.ListVolumes("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].HostPath != "mydbdata" {
		t.Errorf("vols[0].HostPath = %q, want mydbdata", vols[0].HostPath)
	}
}

func TestStoreRemoveVolumeNotFound(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	err := s.RemoveVolume("testapp", "/nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent volume")
	}
}

func TestStoreListVolumes(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
			Volumes: []types.VolumeConfig{
				{HostPath: "/data/uploads", ContainerPath: "/app/uploads"},
			},
		},
	})

	vols, err := s.ListVolumes("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].HostPath != "/data/uploads" {
		t.Errorf("vols[0].HostPath = %q, want /data/uploads", vols[0].HostPath)
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -v -count=1 -run TestStore.*[Vv]olume`
Expected: compilation errors — `AddVolume`, `RemoveVolume`, `ListVolumes` undefined

- [ ] **Step 3: Write minimal implementation**

```go
// Add to internal/config/store.go

func (s *Store) AddVolume(appName, hostPath, containerPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}

	for _, v := range app.Config.Volumes {
		if v.HostPath == hostPath {
			return fmt.Errorf("volume %q already mounted on app %q", hostPath, appName)
		}
	}

	app.Config.Volumes = append(app.Config.Volumes, types.VolumeConfig{
		HostPath:      hostPath,
		ContainerPath: containerPath,
	})
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
		return fmt.Errorf("volume %q not found for app %q", hostPath, appName)
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v -count=1 -run TestStore.*[Vv]olume`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat(store): add volume CRUD methods (AddVolume, RemoveVolume, ListVolumes)"
```

---

### Task 5: CLI Storage Commands

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `store.AddVolume(appName, hostPath, containerPath)`, `store.RemoveVolume(appName, hostPath)`, `store.ListVolumes(appName)`

- [ ] **Step 1: Write failing tests for CLI commands**

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

func TestStorageMountCmd(t *testing.T) {
	dir := t.TempDir()
	dataDir = dir

	store := config.NewStore(dir)
	store.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{Name: "testapp"},
	})

	// Execute: tengiz storage mount testapp /data/uploads /app/uploads
	storageMountCmd.SetArgs([]string{"testapp", "/data/uploads", "/app/uploads"})
	if err := storageMountCmd.Execute(); err != nil {
		t.Fatalf("storage mount error: %v", err)
	}

	vols, _ := store.ListVolumes("testapp")
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].HostPath != "/data/uploads" {
		t.Errorf("HostPath = %q, want /data/uploads", vols[0].HostPath)
	}
}

func TestStorageUnmountCmd(t *testing.T) {
	dir := t.TempDir()
	dataDir = dir

	store := config.NewStore(dir)
	store.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
			Volumes: []types.VolumeConfig{
				{HostPath: "/data/uploads", ContainerPath: "/app/uploads"},
			},
		},
	})

	storageUnmountCmd.SetArgs([]string{"testapp", "/data/uploads"})
	if err := storageUnmountCmd.Execute(); err != nil {
		t.Fatalf("storage unmount error: %v", err)
	}

	vols, _ := store.ListVolumes("testapp")
	if len(vols) != 0 {
		t.Fatalf("expected 0 volumes, got %d", len(vols))
	}
}

func TestStorageListCmd(t *testing.T) {
	dir := t.TempDir()
	dataDir = dir

	store := config.NewStore(dir)
	store.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
			Volumes: []types.VolumeConfig{
				{HostPath: "/data/uploads", ContainerPath: "/app/uploads"},
			},
		},
	})

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	storageListCmd.SetArgs([]string{"testapp"})
	if err := storageListCmd.Execute(); err != nil {
		t.Fatalf("storage list error: %v", err)
	}

	w.Close()
	os.Stdout = old

	var buf [1024]byte
	n, _ := r.Read(buf[:])
	output := string(buf[:n])

	if !contains(output, "/data/uploads") {
		t.Errorf("output should contain /data/uploads, got: %s", output)
	}
	if !contains(output, "/app/uploads") {
		t.Errorf("output should contain /app/uploads, got: %s", output)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -count=1 -run TestStorage`
Expected: compilation errors — `storageMountCmd`, `storageUnmountCmd`, `storageListCmd` undefined

- [ ] **Step 3: Add storageCmd and subcommands to root.go**

```go
// Add after the configCmd block in internal/cli/root.go (before gitCmd)

var storageCmd = &cobra.Command{
	Use:   "storage",
	Short: "Manage persistent storage volumes for applications",
}

var storageMountCmd = &cobra.Command{
	Use:   "mount <app> <host_path> <container_path>",
	Short: "Mount a persistent volume to an application",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName, hostPath, containerPath := args[0], args[1], args[2]
		store := config.NewStore(dataDir)

		if err := store.AddVolume(appName, hostPath, containerPath); err != nil {
			return err
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			fmt.Printf("[tengiz] volume added to store, but Docker not available: %v\n", err)
			return nil
		}

		if err := rt.Restart(cmd.Context(), appName); err != nil {
			fmt.Printf("[tengiz] warning: failed to restart app (restart manually to pick up volume): %v\n", err)
		}

		fmt.Printf("[tengiz] volume mounted: %s -> %s on %s\n", hostPath, containerPath, appName)
		return nil
	},
}

var storageUnmountCmd = &cobra.Command{
	Use:   "unmount <app> <host_path>",
	Short: "Unmount a persistent volume from an application",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName, hostPath := args[0], args[1]
		store := config.NewStore(dataDir)

		if err := store.RemoveVolume(appName, hostPath); err != nil {
			return err
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			fmt.Printf("[tengiz] volume removed from store, but Docker not available: %v\n", err)
			return nil
		}

		if err := rt.Restart(cmd.Context(), appName); err != nil {
			fmt.Printf("[tengiz] warning: failed to restart app (restart manually to pick up change): %v\n", err)
		}

		fmt.Printf("[tengiz] volume unmounted: %s from %s\n", hostPath, appName)
		return nil
	},
}

var storageListCmd = &cobra.Command{
	Use:   "list <app>",
	Short: "List persistent volumes mounted on an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		store := config.NewStore(dataDir)

		vols, err := store.ListVolumes(appName)
		if err != nil {
			return err
		}
		if len(vols) == 0 {
			fmt.Printf("No volumes mounted on %s.\n", appName)
			return nil
		}

		fmt.Printf("%-30s %-30s\n", "HOST PATH / VOLUME NAME", "CONTAINER PATH")
		for _, v := range vols {
			fmt.Printf("%-30s %-30s\n", v.HostPath, v.ContainerPath)
		}
		return nil
	},
}
```

- [ ] **Step 4: Register storageCmd and subcommands in init()**

```go
// In root.go init(), add these lines with the other commands:
	storageCmd.AddCommand(storageMountCmd)
	storageCmd.AddCommand(storageUnmountCmd)
	storageCmd.AddCommand(storageListCmd)
	rootCmd.AddCommand(storageCmd)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -v -count=1 -run TestStorage`
Expected: PASS

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: ALL PASS

- [ ] **Step 7: Run vet**

Run: `go vet ./...`
Expected: no errors

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add storage mount/unmount/list commands for volume management"
```

---

### Self-Review

**1. Spec coverage:**
- `types.VolumeConfig` with `HostPath` + `ContainerPath` fields → Task 1
- `.tengiz.yaml` volume config section → Task 3 (viper auto-unmarshal via mapstructure tags)
- `runtime.Run()` volume flag integration → Task 2 (volumeArgs + wiring in Create/CreateVersioned)
- CLI commands `tengiz storage mount/unmount/list` → Task 5
- Volume persistence in `AppEntry` → Task 4 (AddVolume/RemoveVolume/ListVolumes in Store)
- Auto-restart on mount/unmount change → Task 5 (runtime.Restart after storage mutation)

**2. Placeholder scan:** No TBD, TODOs, or incomplete code — every step has real code.

**3. Type consistency:** `VolumeConfig` used consistently across all 5 tasks. `AddVolume(appName, hostPath, containerPath)` matches the CLI args. `ListVolumes` returns `[]VolumeConfig` matching the `AppConfig.Volumes` type.
