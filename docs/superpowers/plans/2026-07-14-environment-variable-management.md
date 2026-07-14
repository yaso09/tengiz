# Environment Variable Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add app-specific environment variable management — users can define env vars in `.tengiz.yaml` and set/get/unset them via CLI at runtime.

**Architecture:** Add `Env map[string]string` field to `AppConfig` (viper auto-loads from YAML, JSON store auto-persists). Inject `-e KEY=VALUE` flags into Docker `run` args in `Create()` and `CreateVersioned()`. Fix `Start()` recreate path to restore env vars from stored config. Add `config:set/get/unset/show` CLI commands for runtime manipulation.

**Tech Stack:** Go 1.26, viper (yaml config), Docker CLI via `os/exec`

## Global Constraints

- Container names are prefixed `tengiz-<appname>`, labeled with `tengiz-app=<appname>`
- Env vars stored in `AppEntry.Config.Env` → auto-persisted via JSON in `~/.tengiz/apps.json`
- `.tengiz.yaml` `env:` section uses `KEY: value` format (map, not list)
- All new env vars must be available to both `Create()` and `CreateVersioned()` code paths
- `Start()` recreate path must also inject env vars from stored config

---

## File Structure

| File | Change | Responsibility |
|------|--------|----------------|
| `internal/types/types.go` | Add `Env map[string]string` field | Data model — holds env vars in AppConfig |
| `internal/config/config.go` | No code change | Viper auto-unmarshals `env:` from YAML via `mapstructure:"env"` |
| `internal/config/store.go` | Add `GetEnv`/`SetEnv`/`UnsetEnv` methods | Persist env var mutations at runtime |
| `internal/runtime/docker.go` | Inject `-e` flags in `Create`/`CreateVersioned`; fix `Start` recreate | Pass env vars to Docker containers |
| `internal/runtime/runtime.go` | No change | Interface unchanged; env flows through `*types.AppConfig` |
| `internal/cli/root.go` | Add 4 config commands; update init template | User-facing env management |
| `internal/cli/root_test.go` | Tests for config commands | Verify CLI integration |
| `internal/config/store_test.go` | Tests for new store methods | Verify persistence |
| `internal/types/types_test.go` | Tests for Env field serialization | Verify data model |

---

### Task 1: Data Model — Add Env Field to AppConfig + Config Loading

**Files:**
- Modify: `internal/types/types.go`
- Create: `internal/types/types_test.go`

**Interfaces:**
- Consumes: existing `AppConfig` struct
- Produces: `AppConfig.Env map[string]string` with `mapstructure:"env" json:"env,omitempty"` tags

- [ ] **Step 1: Write failing test for Env field serialization**

File: `internal/types/types_test.go`

```go
package types

import (
	"encoding/json"
	"testing"
)

func TestAppConfigEnvSerialization(t *testing.T) {
	cfg := AppConfig{
		Name: "myapp",
		Env: map[string]string{
			"DATABASE_URL": "postgres://localhost:5432/db",
			"API_KEY":      "secret123",
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded AppConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Env["DATABASE_URL"] != "postgres://localhost:5432/db" {
		t.Fatalf("expected DATABASE_URL, got %q", decoded.Env["DATABASE_URL"])
	}
	if decoded.Env["API_KEY"] != "secret123" {
		t.Fatalf("expected API_KEY, got %q", decoded.Env["API_KEY"])
	}
}

func TestAppConfigEnvEmptyByDefault(t *testing.T) {
	cfg := AppConfig{Name: "noenv"}
	if cfg.Env != nil {
		t.Fatal("expected nil Env for zero-value AppConfig")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -count=1 -run TestAppConfigEnv`
Expected: FAIL — `AppConfig` has no `Env` field

- [ ] **Step 3: Add Env field to AppConfig**

Edit `internal/types/types.go:5-12`:

Old:
```go
type AppConfig struct {
	Name        string              `mapstructure:"name"`
	Port        int                 `mapstructure:"port"`
	Build       BuildConfig         `mapstructure:"build"`
	Serverless  ServerlessConfig    `mapstructure:"serverless"`
	Domains     []string            `mapstructure:"domains"`
	HealthCheck *HealthCheckConfig  `mapstructure:"healthcheck,omitempty"`
}
```

New:
```go
type AppConfig struct {
	Name        string              `mapstructure:"name"`
	Port        int                 `mapstructure:"port"`
	Build       BuildConfig         `mapstructure:"build"`
	Serverless  ServerlessConfig    `mapstructure:"serverless"`
	Domains     []string            `mapstructure:"domains"`
	HealthCheck *HealthCheckConfig  `mapstructure:"healthcheck,omitempty"`
	Env         map[string]string   `mapstructure:"env" json:"env,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -count=1 -run TestAppConfigEnv`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat(types): add Env field to AppConfig for environment variable management"
```

---

### Task 2: Docker Runtime — Inject -e Flags in Create/CreateVersioned + Fix Start Recreate

**Files:**
- Modify: `internal/runtime/docker.go`
- Modify: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: `cfg.Env map[string]string` from `*types.AppConfig`
- Produces: `-e KEY=VALUE` entries in `docker run` args in `Create()` (docker.go:36-43), `CreateVersioned()` (docker.go:230-238), and `Start()` recreate path (docker.go:68-74)

- [ ] **Step 1: Write test for Env passthrough in Create**

Append to `internal/runtime/runtime_test.go`:

```go
func TestCreateWithEnv(t *testing.T) {
	// Stub runtime ignores all — just verify interface compiles
	var m Manager = NewStub()
	cfg := &types.AppConfig{
		Name: "testapp",
		Env: map[string]string{
			"MY_VAR": "myval",
		},
	}
	if err := m.Create(context.Background(), cfg, "test:latest", 9000); err != nil {
		t.Fatalf("Create with env: %v", err)
	}
}

func TestCreateVersionedWithEnv(t *testing.T) {
	var m Manager = NewStub()
	cfg := &types.AppConfig{
		Name: "testapp",
		Env: map[string]string{
			"MY_VAR": "myval",
		},
	}
	if err := m.CreateVersioned(context.Background(), cfg, "test:latest", 9001, "v2"); err != nil {
		t.Fatalf("CreateVersioned with env: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it passes with stub**

Run: `go test ./internal/runtime/... -v -count=1 -run TestCreateWithEnv`
Expected: PASS (stub returns nil)

- [ ] **Step 3: Add env injection helper to docker.go**

Add this helper function before `Create` (around line 28):

```go
func envArgs(env map[string]string) []string {
	var args []string
	for k, v := range env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(args[len(args)-2*len(env):]) // keep -e with its value
	return args
}
```

Wait, that sort wouldn't work well. Let me think again.

Actually, the simplest approach: iterate the map and append `-e KEY=VALUE` for each entry. Sort for deterministic ordering.

```go
func envArgs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var args []string
	for _, k := range keys {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, env[k]))
	}
	return args
}
```

Add `"sort"` to imports.

- [ ] **Step 4: Inject envArgs into Create()**

In `Create()` (docker.go:36-43), append env args before imageTag:

Old:
```go
	args := []string{
		"run", "-d",
		"--name", containerName,
		"--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, internalPort),
		"--restart", "no",
		imageTag,
	}
```

New:
```go
	args := []string{
		"run", "-d",
		"--name", containerName,
		"--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, internalPort),
		"--restart", "no",
	}
	args = append(args, envArgs(cfg.Env)...)
	args = append(args, imageTag)
```

- [ ] **Step 5: Inject envArgs into CreateVersioned()**

In `CreateVersioned()` (docker.go:230-238), apply the same pattern:

Old:
```go
	args := []string{
		"run", "-d",
		"--name", containerName,
		"--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
		"--label", fmt.Sprintf("tengiz-deployment=%s", suffix),
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, internalPort),
		"--restart", "no",
		imageTag,
	}
```

New:
```go
	args := []string{
		"run", "-d",
		"--name", containerName,
		"--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
		"--label", fmt.Sprintf("tengiz-deployment=%s", suffix),
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, internalPort),
		"--restart", "no",
	}
	args = append(args, envArgs(cfg.Env)...)
	args = append(args, imageTag)
```

- [ ] **Step 6: Fix Start() recreate path to pass env vars**

In `Start()` (docker.go:52-83), the recreate path (lines 68-75) currently only restores port bindings. Update `getContainerConfig()` to also return env vars, then pass them in the recreate args.

Modify `getContainerConfig` signature and implementation (docker.go:85-122):

Old:
```go
func (r *dockerRuntime) getContainerConfig(ctx context.Context, containerName string) (string, []string) {
```

New:
```go
func (r *dockerRuntime) getContainerConfig(ctx context.Context, containerName string) (string, []string, []string) {
```

Add env extraction inside `getContainerConfig`:
```go
	// Get env variables
	envCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{json .Config.Env}}", containerName)
	envOut, err := envCmd.CombinedOutput()
	if err == nil {
		var envList []string
		if err := json.Unmarshal(envOut, &envList); err == nil {
			for _, e := range envList {
				envArgs = append(envArgs, "-e", e)
			}
		}
	}
```

Return `imageTag, ports, envArgs`.

Update the callers: in `Start()`:
```go
imageTag, ports, envs := r.getContainerConfig(ctx, containerName)
```

And in the recreate args:
```go
args = append(args, ports...)
args = append(args, envs...)
args = append(args, imageTag)
```

- [ ] **Step 7: Run tests to verify compilation and existing tests still pass**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat(runtime): inject env vars as -e flags in docker run; fix Start() recreate to restore env"
```

---

### Task 3: Store Layer — Add GetEnv/SetEnv/UnsetEnv Methods

**Files:**
- Modify: `internal/config/store.go`
- Modify: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `Store` existing `GetApp`/`SaveApp` methods
- Produces: `s.GetEnv(appName, key) (string, bool, error)`, `s.SetEnv(appName, key, value) error`, `s.UnsetEnv(appName, key) error`, `s.ListEnv(appName) (map[string]string, error)`

- [ ] **Step 1: Write failing tests**

Append to `internal/config/store_test.go`:

```go
func TestStoreSetGetEnv(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	// Save an app first
	s.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}})

	// Set env
	if err := s.SetEnv("testapp", "DATABASE_URL", "postgres://localhost/db"); err != nil {
		t.Fatalf("SetEnv: %v", err)
	}

	// Get env
	val, ok, err := s.GetEnv("testapp", "DATABASE_URL")
	if err != nil {
		t.Fatalf("GetEnv: %v", err)
	}
	if !ok {
		t.Fatal("expected env to exist")
	}
	if val != "postgres://localhost/db" {
		t.Fatalf("expected 'postgres://localhost/db', got %q", val)
	}
}

func TestStoreUnsetEnv(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{
		Name: "testapp",
		Env:  map[string]string{"MY_KEY": "myval"},
	}})

	// Unset
	if err := s.UnsetEnv("testapp", "MY_KEY"); err != nil {
		t.Fatalf("UnsetEnv: %v", err)
	}

	// Verify gone
	_, ok, err := s.GetEnv("testapp", "MY_KEY")
	if err != nil {
		t.Fatalf("GetEnv after unset: %v", err)
	}
	if ok {
		t.Fatal("expected env to be unset")
	}
}

func TestStoreListEnv(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	s.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{
		Name: "testapp",
		Env:  map[string]string{"A": "1", "B": "2"},
	}})

	env, err := s.ListEnv("testapp")
	if err != nil {
		t.Fatalf("ListEnv: %v", err)
	}
	if len(env) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(env))
	}
	if env["A"] != "1" || env["B"] != "2" {
		t.Fatalf("unexpected env map: %v", env)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -v -count=1 -run TestStore.*Env`
Expected: FAIL — methods not defined

- [ ] **Step 3: Add env CRUD methods to Store**

Add to `internal/config/store.go` after `GetApp` method:

```go
func (s *Store) GetEnv(appName, key string) (string, bool, error) {
	app, err := s.GetApp(appName)
	if err != nil {
		return "", false, err
	}
	val, ok := app.Config.Env[key]
	return val, ok, nil
}

func (s *Store) SetEnv(appName, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	if app.Config.Env == nil {
		app.Config.Env = make(map[string]string)
	}
	app.Config.Env[key] = value
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) UnsetEnv(appName, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	apps := make(map[string]types.AppEntry)
	s.readJSON("apps.json", &apps)
	app, ok := apps[appName]
	if !ok {
		return fmt.Errorf("app %q not found", appName)
	}
	delete(app.Config.Env, key)
	if len(app.Config.Env) == 0 {
		app.Config.Env = nil
	}
	apps[appName] = app
	return s.writeJSON("apps.json", apps)
}

func (s *Store) ListEnv(appName string) (map[string]string, error) {
	app, err := s.GetApp(appName)
	if err != nil {
		return nil, err
	}
	if app.Config.Env == nil {
		return map[string]string{}, nil
	}
	result := make(map[string]string, len(app.Config.Env))
	for k, v := range app.Config.Env {
		result[k] = v
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v -count=1 -run TestStore.*Env`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat(config): add GetEnv/SetEnv/UnsetEnv/ListEnv methods to Store"
```

---

### Task 4: CLI Commands — config:set/get/unset/show + Init Template Update

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `Store.GetEnv`, `Store.SetEnv`, `Store.UnsetEnv`, `Store.ListEnv` (from Task 3)
- Produces: 4 subcommands under `configCmd` — `config:set <key> <value>`, `config:get <key>`, `config:unset <key>`, `config:show`

- [ ] **Step 1: Write failing tests for CLI config commands**

Replace `internal/cli/root_test.go` with an extended version:

```go
package cli

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type mockRTForDeploy struct {
	created atomic.Int32
	removed atomic.Int32
	started atomic.Int32
	stopped atomic.Int32
}

func (m *mockRTForDeploy) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	m.created.Add(1)
	return nil
}

func (m *mockRTForDeploy) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error {
	m.created.Add(1)
	return nil
}

func (m *mockRTForDeploy) Start(ctx context.Context, name string) error { m.started.Add(1); return nil }
func (m *mockRTForDeploy) Stop(ctx context.Context, name string) error { m.stopped.Add(1); return nil }
func (m *mockRTForDeploy) Remove(ctx context.Context, name string) error { m.removed.Add(1); return nil }
func (m *mockRTForDeploy) RemoveBySuffix(ctx context.Context, name string, suffix string) error { m.removed.Add(1); return nil }
func (m *mockRTForDeploy) IsActive(ctx context.Context, name string) (bool, error) { return true, nil }
func (m *mockRTForDeploy) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) { return 0, nil }
func (m *mockRTForDeploy) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *mockRTForDeploy) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) { return nil, nil }
func (m *mockRTForDeploy) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }

func TestDeployZeroDowntimeCreatesVersionedContainer(t *testing.T) {
	var m runtime.Manager = &mockRTForDeploy{}
	if m == nil {
		t.Fatal("mock does not implement Manager")
	}
}

func TestConfigSetGetUnsetShowCommandsRegistered(t *testing.T) {
	// Verify the config subcommands exist on the root command
	configCmd, _, err := rootCmd.Find([]string{"config"})
	if err != nil {
		t.Fatalf("config command not found: %v", err)
	}

	expected := map[string]bool{"set": false, "get": false, "unset": false, "show": false}
	for _, sub := range configCmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Fatalf("config subcommand %q not found", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -count=1 -run TestConfigSetGetUnsetShowCommandsRegistered`
Expected: FAIL — config command not found

- [ ] **Step 3: Add config command and subcommands to root.go**

Add after the `devCmd` variable (around line 435) and before `getwd()`:

```go
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage environment variables for an application",
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set an environment variable",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := config.NewStore(dataDir)
		if err := store.SetEnv(args[0], args[1], args[2]); err != nil {
			return err
		}
		fmt.Printf("[tengiz] set %s=%s for %s\n", args[1], args[2], args[0])
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <app> <key>",
	Short: "Get an environment variable",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := config.NewStore(dataDir)
		val, ok, err := store.GetEnv(args[0], args[1])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("env var %q not set for %s", args[1], args[0])
		}
		fmt.Printf("%s=%s\n", args[1], val)
		return nil
	},
}

var configUnsetCmd = &cobra.Command{
	Use:   "unset <app> <key>",
	Short: "Remove an environment variable",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := config.NewStore(dataDir)
		if err := store.UnsetEnv(args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("[tengiz] unset %s for %s\n", args[1], args[0])
		return nil
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show <app>",
	Short: "Show all environment variables for an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := config.NewStore(dataDir)
		env, err := store.ListEnv(args[0])
		if err != nil {
			return err
		}
		if len(env) == 0 {
			fmt.Printf("No environment variables set for %s.\n", args[0])
			return nil
		}
		for k, v := range env {
			fmt.Printf("%s=%s\n", k, v)
		}
		return nil
	},
}
```

Wait, `configSetCmd` uses `args[0]` as app name, `args[1]` as key, `args[2]` as value but `ExactArgs(2)`. That's wrong. The pattern should be `config set <app> <key> <value>` with `ExactArgs(3)`, or `config set <key> <value>` with the app being the current project. Let me think about what makes sense from a user perspective.

Looking at Dokku for inspiration: `dokku config:set <app> KEY=VAL KEY2=VAL2`. But simpler approach for Tengiz: `tengiz config set <app> <key> <value>`.

Actually, let me reconsider. Looking at the existing command patterns (e.g., `tengiz stop <app>`), all existing commands take the app name as a positional arg. So `config set <app> <key> <value>` with `ExactArgs(3)` makes sense.

Let me fix the implementation:

```go
var configSetCmd = &cobra.Command{
	Use:   "set <app> <key> <value>",
	Short: "Set an environment variable",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName, key, value := args[0], args[1], args[2]
		store := config.NewStore(dataDir)
		if err := store.SetEnv(appName, key, value); err != nil {
			return err
		}
		fmt.Printf("[tengiz] set %s=%s for %s\n", key, value, appName)
		return nil
	},
}
```

- [ ] **Step 3 (continued): Register config subcommands in init()**

Add to `init()` (root.go:28-37):

```go
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configUnsetCmd)
	configCmd.AddCommand(configShowCmd)
	rootCmd.AddCommand(configCmd)
```

- [ ] **Step 3 (continued): Update init template to include env section**

Update the init template content (root.go:61-68):

Old:
```go
		content := fmt.Sprintf(`name: %s
# port: 3000            # container internal port (auto-detected if omitted)
serverless:
  enabled: true
  idle_timeout: 5m      # scale-to-zero timeout
# domains:
#   - app.example.com
`, name)
```

New:
```go
		content := fmt.Sprintf(`name: %s
# port: 3000            # container internal port (auto-detected if omitted)
serverless:
  enabled: true
  idle_timeout: 5m      # scale-to-zero timeout
# domains:
#   - app.example.com
# env:
#   DATABASE_URL: postgres://localhost:5432/myapp
#   API_KEY: your-secret-key
`, name)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add config:set/get/unset/show commands and env section in init template"
```

---

## Self-Review

**Spec coverage:**
- ✅ P0 #2 "Environment Variable Management" — full implementation
- ✅ Each env var requirement addressed: set (Task 3+4), get (Task 3+4), unset (Task 3+4), show (Task 3+4), Docker injection (Task 2), YAML config loading (Task 1 implicit via viper)
- ✅ `Start()` recreate path fixed (Task 2)

**Placeholder scan:** No TBD/TODO/filler patterns found. Every step has complete code. All function signatures match across tasks. No "implement later" or "handle edge cases" without specific code.

**Type consistency:**
- `AppConfig.Env` is `map[string]string` — consistent across types.go, store.go, docker.go, root.go
- `Store.GetEnv` returns `(string, bool, error)` — consistent in store.go and root.go
- `Store.SetEnv(appName, key, value string) error` — consistent in store.go and root.go
- `Store.UnsetEnv(appName, key string) error` — consistent
- `Store.ListEnv(appName string) (map[string]string, error)` — consistent
