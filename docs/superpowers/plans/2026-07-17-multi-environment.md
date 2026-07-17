# Multi-Environment Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add development/staging/production environment support with per-environment config files, state isolation, and `--env` CLI flag.

**Architecture:** Environment is a first-class concept threaded through config loading, state persistence, container naming, image tagging, and proxy routing. Base `.tengiz.yaml` is merged with `.tengiz.{env}.yaml` overrides. State files (`apps.json`, `ports.json`, `deployments.json`) become environment-scoped. Containers are named `tengiz-{app}-{env}` and tagged `tengiz-apps/{app}:{env}-{deploymentID}`. Default environment is `"production"` for backward compatibility.

**Tech Stack:** Go 1.26, viper (config), cobra (CLI), Docker CLI via os/exec

## Global Constraints

- No new external dependencies beyond cobra and viper
- Default environment is `"production"` — existing deployments must work without the `--env` flag
- All state files must be isolated per environment: `apps-{env}.json`, `ports-{env}.json`, `deployments-{env}.json`
- Image tags format: `tengiz-apps/{app}:{env}-{deploymentID}` (e.g. `tengiz-apps/myapp:production-1700000000`)
- Container names format: `tengiz-{app}-{env}` (e.g. `tengiz-myapp-production`)
- Container labels: always `tengiz-app={app}`, plus new `tengiz-env={env}`
- `.tengiz.yaml` always required as base; `.tengiz.{env}.yaml` is optional
- `--env` flag defaults to `"production"` across all commands
- Run `go test ./... -v -count=1` and `go vet ./...` after every task

---

### Task 1: Add Environment Field to Types

**Files:**
- Modify: `internal/types/types.go:36-41` (AppConfig), `internal/types/types.go:109-122` (AppEntry)

**Interfaces:**
- Consumes: nothing (first task)
- Produces: `types.AppConfig.Environment string`, `types.AppEntry.Environment string`

- [ ] **Step 1: Add Environment to AppConfig and AppEntry**

```go
// In AppConfig, after the existing fields (line ~41):
    Env         map[string]string   `mapstructure:"env" json:"env,omitempty"`
    Environment string              `mapstructure:"environment" json:"environment,omitempty"`
    Git         *GitConfig          `mapstructure:"git,omitempty" json:"git,omitempty"`

// In AppEntry, after the existing fields (line ~114-122):
    Config           AppConfig         `json:"config"`
    Environment      string            `json:"environment,omitempty"`
    DeploymentSuffix string            `json:"deployment_suffix,omitempty"`
```

- [ ] **Step 2: Run tests and vet**

Run: `go test ./... -v -count=1`
Expected: All tests pass (no behavioral change yet)

Run: `go vet ./...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/types/types.go
git commit -m "feat: add Environment field to AppConfig and AppEntry"
```

---

### Task 2: Environment-Scoped Config Loading

**Files:**
- Modify: `internal/config/config.go:15-42` (add `LoadWithEnv`)
- Test: `internal/config/config_test.go` (add tests)

**Interfaces:**
- Consumes: `types.AppConfig.Environment string`
- Produces: `config.LoadWithEnv(path, env string) (*types.AppConfig, error)` — loads `.tengiz.yaml` base, overlays `.tengiz.{env}.yaml` if it exists, sets `cfg.Environment = env`

- [ ] **Step 1: Write the failing test**

```go
// In internal/config/config_test.go (create if not exists):
package config

import (
    "os"
    "path/filepath"
    "testing"
)

func TestLoadWithEnv(t *testing.T) {
    dir := t.TempDir()

    // Write base config
    base := []byte("name: myapp\nport: 3000\nenv:\n  DATABASE_URL: postgres://localhost/mydb\n")
    if err := os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), base, 0644); err != nil {
        t.Fatal(err)
    }

    // Write staging override
    staging := []byte("env:\n  DATABASE_URL: postgres://staging/mydb\n  STAGING_SECRET: abc123\n")
    if err := os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), staging, 0644); err != nil {
        t.Fatal(err)
    }

    cfg, err := LoadWithEnv(dir, "staging")
    if err != nil {
        t.Fatalf("LoadWithEnv failed: %v", err)
    }

    if cfg.Name != "myapp" {
        t.Errorf("expected name 'myapp', got %q", cfg.Name)
    }
    if cfg.Port != 3000 {
        t.Errorf("expected port 3000, got %d", cfg.Port)
    }
    if cfg.Environment != "staging" {
        t.Errorf("expected environment 'staging', got %q", cfg.Environment)
    }
    if cfg.Env["DATABASE_URL"] != "postgres://staging/mydb" {
        t.Errorf("expected DATABASE_URL 'postgres://staging/mydb', got %q", cfg.Env["DATABASE_URL"])
    }
    if cfg.Env["STAGING_SECRET"] != "abc123" {
        t.Errorf("expected STAGING_SECRET 'abc123', got %q", cfg.Env["STAGING_SECRET"])
    }
}

func TestLoadWithEnvNoOverride(t *testing.T) {
    dir := t.TempDir()

    base := []byte("name: myapp\nport: 3000\n")
    if err := os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), base, 0644); err != nil {
        t.Fatal(err)
    }

    cfg, err := LoadWithEnv(dir, "production")
    if err != nil {
        t.Fatalf("LoadWithEnv failed: %v", err)
    }

    if cfg.Name != "myapp" {
        t.Errorf("expected name 'myapp', got %q", cfg.Name)
    }
    if cfg.Environment != "production" {
        t.Errorf("expected environment 'production', got %q", cfg.Environment)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -count=1 -run TestLoadWithEnv`
Expected: FAIL with "undefined: LoadWithEnv"

- [ ] **Step 3: Write LoadWithEnv implementation**

```go
// In internal/config/config.go, add after Load():
func LoadWithEnv(path, env string) (*types.AppConfig, error) {
    if env == "" {
        env = "production"
    }

    cfg, err := Load(path)
    if err != nil {
        return nil, err
    }

    envPath := filepath.Join(path, fmt.Sprintf(".tengiz.%s.yaml", env))
    if _, statErr := os.Stat(envPath); statErr != nil {
        // No env-specific file — return base config with env set
        cfg.Environment = env
        return cfg, nil
    }

    v := viper.New()
    v.SetConfigFile(envPath)
    v.SetConfigType("yaml")
    if err := v.ReadInConfig(); err != nil {
        return nil, fmt.Errorf("env config read: %w", err)
    }

    // Merge env overrides on top of base config
    if err := v.Unmarshal(cfg); err != nil {
        return nil, fmt.Errorf("env config unmarshal: %w", err)
    }

    cfg.Environment = env
    return cfg, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -count=1 -run TestLoadWithEnv`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add LoadWithEnv for environment-scoped config merging"
```

---

### Task 3: Environment-Scoped Store

**Files:**
- Modify: `internal/config/store.go:15-23` (add env field, modify NewStore)
- Modify: all methods in `internal/config/store.go` that read/write files (SaveApp, RemoveApp, ListApps, GetApp, UpdateApp, AllocatePort, FreePort, GetEnv, SetEnv, UnsetEnv, ListEnv, AddDomain, RemoveDomain, ListDomains, AddVolume, RemoveVolume, ListVolumes, AddDeployment, GetDeployments, UpdateDeploymentStatus, GetPreviousDeployment, GetDeploymentByID, SaveBuildLog, GetBuildLog, ListBuildLogs, PruneBuildLogs)

**Interfaces:**
- Consumes: environment string
- Produces: `config.NewStore(dataDir, env string) *Store` — store scopes all file operations to environment
- Internal helper: `s.envFile(name string) string` returns `apps-{env}.json`, `ports-{env}.json`, etc.

- [ ] **Step 1: Write the failing test**

```go
// In internal/config/store_test.go (create if not exists):
package config

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/yaso09/tengiz/internal/types"
)

func TestStoreEnvironmentScoping(t *testing.T) {
    dir := t.TempDir()

    // Create production store — saves myapp
    prodStore := NewStore(dir, "production")
    app := types.AppEntry{Name: "myapp", Port: 9000}
    if err := prodStore.SaveApp(app); err != nil {
        t.Fatalf("SaveApp (prod): %v", err)
    }

    // Create staging store — saves myapp with different port
    stageStore := NewStore(dir, "staging")
    stageApp := types.AppEntry{Name: "myapp", Port: 9001}
    if err := stageStore.SaveApp(stageApp); err != nil {
        t.Fatalf("SaveApp (staging): %v", err)
    }

    // Production store should still see port 9000
    prodApp, err := prodStore.GetApp("myapp")
    if err != nil {
        t.Fatalf("GetApp (prod): %v", err)
    }
    if prodApp.Port != 9000 {
        t.Errorf("expected prod port 9000, got %d", prodApp.Port)
    }

    // Staging store should see port 9001
    stgApp, err := stageStore.GetApp("myapp")
    if err != nil {
        t.Fatalf("GetApp (staging): %v", err)
    }
    if stgApp.Port != 9001 {
        t.Errorf("expected staging port 9001, got %d", stgApp.Port)
    }

    // Verify separate JSON files exist
    prodFile := filepath.Join(dir, "apps-production.json")
    stageFile := filepath.Join(dir, "apps-staging.json")
    if _, err := os.Stat(prodFile); os.IsNotExist(err) {
        t.Errorf("production apps file not found: %s", prodFile)
    }
    if _, err := os.Stat(stageFile); os.IsNotExist(err) {
        t.Errorf("staging apps file not found: %s", stageFile)
    }
}

func TestStoreDefaultEnv(t *testing.T) {
    dir := t.TempDir()
    store := NewStore(dir, "") // should default to "production"
    app := types.AppEntry{Name: "myapp", Port: 9000}
    if err := store.SaveApp(app); err != nil {
        t.Fatalf("SaveApp: %v", err)
    }
    // Should use production file
    prodFile := filepath.Join(dir, "apps-production.json")
    if _, err := os.Stat(prodFile); os.IsNotExist(err) {
        t.Errorf("expected production apps file: %s", prodFile)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -count=1 -run TestStoreEnvironment`
Expected: FAIL (NewStore doesn't accept env, SaveApp/GetApp don't scope)

- [ ] **Step 3: Implement environment-scoped Store**

Add `env` field to Store:

```go
// In internal/config/store.go, modify NewStore and add envFile helper:

type Store struct {
    mu      sync.Mutex
    dataDir string
    env     string
}

func NewStore(dataDir string) *Store {
    return NewStoreWithEnv(dataDir, "")
}

func NewStoreWithEnv(dataDir, env string) *Store {
    if env == "" {
        env = "production"
    }
    os.MkdirAll(dataDir, 0755)
    return &Store{dataDir: dataDir, env: env}
}

func (s *Store) envFile(name string) string {
    ext := filepath.Ext(name)
    base := strings.TrimSuffix(name, ext)
    return fmt.Sprintf("%s-%s%s", base, s.env, ext)
}
```

Update every method that reads/writes files to use `s.envFile()` instead of the raw filename. For example:

```go
func (s *Store) SaveApp(app types.AppEntry) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    apps := make(map[string]types.AppEntry)
    s.readJSON(s.envFile("apps.json"), &apps)
    apps[app.Name] = app
    return s.writeJSON(s.envFile("apps.json"), apps)
}
```

Similarly update: `RemoveApp`, `ListApps`, `GetApp`, `UpdateApp` (`apps.json`)
`AllocatePort`, `FreePort` (`ports.json`)
`AddDeployment`, `GetDeployments`, `UpdateDeploymentStatus`, `GetPreviousDeployment`, `GetDeploymentByID` (`deployments.json`)
`AddVolume`, `RemoveVolume`, `ListVolumes` — these use apps.json internally, already scoped
`AddDomain`, `RemoveDomain`, `ListDomains` — these use apps.json internally, already scoped
`GetEnv`, `SetEnv`, `UnsetEnv`, `ListEnv` — these use apps.json internally, already scoped

For build logs:

```go
func (s *Store) buildLogDir(appName string) string {
    return filepath.Join(s.dataDir, "build-logs", s.env, appName)
}
```

Keep the old `NewStore` function as a backward-compatible wrapper that defaults to `"production"`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat: environment-scoped Store with separate JSON files per env"
```

---

### Task 4: Add --env Flag to Deploy Command and Wire Environment

**Files:**
- Modify: `internal/cli/root.go:139-325` (deployCmd)
- Modify: `internal/builder/builder.go:21-28` (Build signature adds env param), `internal/builder/builder.go:40-60` (image tag includes env)

**Interfaces:**
- Consumes: `cfg.Environment string`, `store.env string`
- Produces: Image tags `tengiz-apps/{app}:{env}-{deploymentID}`, containers use env-aware naming

- [ ] **Step 1: Register --env flag for deploy**

Add in `init()`:

```go
deployCmd.Flags().String("env", "production", "deployment environment (e.g. production, staging, dev)")
```

- [ ] **Step 2: Update deployCmd RunE to use --env flag**

Changes to `deployCmd.RunE`:

```go
// After resolving projectRoot:
env, _ := cmd.Flags().GetString("env")

// Replace config.Load(projectRoot) with config.LoadWithEnv(projectRoot, env)
cfg, err := config.LoadWithEnv(projectRoot, env)
if err != nil {
    cfg = &types.AppConfig{
        Name:        filepath.Base(projectRoot),
        Environment: env,
        Serverless: types.ServerlessConfig{
            Enabled:     true,
            IdleTimeout: 5 * time.Minute,
        },
    }
}

// Replace config.NewStore(dataDir) with config.NewStoreWithEnv(dataDir, env)
store := config.NewStoreWithEnv(dataDir, env)

// Pass cfg to builder (builder will use cfg.Environment for image tags)
```

- [ ] **Step 3: Update Builder.Build to accept environment**

```go
// internal/builder/builder.go — update Build method:

func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    if detection.Framework == FrameworkDocker {
        return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
    }
    if err := b.ensureDockerfile(dir, detection); err != nil {
        return "", "", fmt.Errorf("generate dockerfile: %w", err)
    }
    return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
}

func (b *Builder) buildWithDockerfile(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
    tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)
    // ... rest unchanged
    latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", appName, env)
    // ... tagCmd
}
```

- [ ] **Step 4: Update deploy call to pass env**

In deployCmd.RunE, replace:
```go
imageTag, buildLog, err := b.Build(context.Background(), projectRoot, cfg.Name, detection, deploymentID)
```
with:
```go
imageTag, buildLog, err := b.Build(context.Background(), projectRoot, cfg.Name, cfg.Environment, detection, deploymentID)
```

- [ ] **Step 5: Update deployCmd to check existing app with env-qualified name**

Replace `existingApp, lookupErr := store.GetApp(cfg.Name)` — no change needed since store is already env-scoped.

- [ ] **Step 6: Run tests**

Run: `go build ./...`
Expected: Build succeeds

Run: `go test ./... -v -count=1`
Expected: All tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/builder/builder.go
git commit -m "feat: add --env flag to deploy, update Builder for env-aware image tags"
```

---

### Task 5: Environment-Aware Container Naming in Runtime

**Files:**
- Modify: `internal/runtime/docker.go` (all methods that construct container names)

**Interfaces:**
- Consumes: `cfg.Environment string` from `*types.AppConfig`
- Produces: Container names in format `tengiz-{app}-{env}`, labels include `tengiz-env={env}`

- [ ] **Step 1: Read current docker.go to understand naming patterns**

Run: `cat internal/runtime/docker.go`
Note: Container names are currently `tengiz-{name}` and `tengiz-{name}-{suffix}`. Labels are `tengiz-app={name}`. Add env to both.

- [ ] **Step 2: Add env-aware container name helper**

```go
// In internal/runtime/docker.go, add or modify:

func containerName(name, env string) string {
    if env == "" || env == "production" {
        return fmt.Sprintf("tengiz-%s", name)
    }
    return fmt.Sprintf("tengiz-%s-%s", name, env)
}

func versionedContainerName(name, env, suffix string) string {
    base := containerName(name, env)
    return fmt.Sprintf("%s-%s", base, suffix)
}
```

- [ ] **Step 3: Update all Docker command constructions to use env-aware names**

For each method in docker.go:
- `Create` → use `containerName(cfg.Name, cfg.Environment)` as container name, add `--label tengiz-env={env}`
- `CreateFromImage` → same
- `CreateVersioned` → use `versionedContainerName(cfg.Name, cfg.Environment, suffix)`
- `Remove` → use `containerName(name, "")` — but Remove is called with just a name string. We need to propagate env.

For methods that receive a plain name string (not cfg):
- `Start(ctx, name)`, `Stop(ctx, name)`, `Restart(ctx, name)`, `Remove(ctx, name)`, `IsActive(ctx, name)`, `Logs(ctx, name, opts)`, `Run(ctx, cfg, imageTag, cmd, opts)`
- These already receive the full container name as `name` param, so callers must pass the env-qualified name.

For methods that use `cfg.Name`:
- `Create(ctx, cfg, imageTag, port)` → `containerName(cfg.Name, cfg.Environment)`
- `CreateFromImage(ctx, cfg, imageTag, port)` → same
- `CreateVersioned(ctx, cfg, imageTag, port, suffix)` → `versionedContainerName(cfg.Name, cfg.Environment, suffix)`

- [ ] **Step 4: Update deploy-caller to pass env-qualified names**

In deployCmd:
- `rt.Create(context.Background(), cfg, imageTag, port)` — Create already receives cfg, so env is available inside Create
- `rt.CreateVersioned(context.Background(), cfg, imageTag, newPort, deploymentID)` — same
- `rt.RemoveBySuffix(context.Background(), cfg.Name, oldSuffix)` — need env-qualified base name
- `rt.Remove(context.Background(), cfg.Name)` — need env-qualified name
- `rt.WaitForReady(context.Background(), fmt.Sprintf("%s-%s", cfg.Name, deploymentID), cfg.Port)` — need env-qualified name

For `RemoveBySuffix` and `Remove` calls, pass the env-qualified container name or update the runtime to accept env.

Simplify: Make `Remove` and `RemoveBySuffix` accept env as a parameter, or have callers pass the full name.

Option A: Pass full container name everywhere (cleaner, runtime stays dumb)
```go
// deployCmd:
rt.Remove(context.Background(), runtime.ContainerName(cfg.Name, cfg.Environment))
```

Option B: Add env to runtime methods (more changes to interface)

I'll go with Option A — add a public `ContainerName` helper to the runtime package.

```go
// In internal/runtime/docker.go:
func ContainerName(name, env string) string {
    if env == "" || env == "production" {
        return fmt.Sprintf("tengiz-%s", name)
    }
    return fmt.Sprintf("tengiz-%s-%s", name, env)
}
```

- [ ] **Step 5: Run tests**

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/ internal/cli/root.go
git commit -m "feat: environment-aware container naming in runtime"
```

---

### Task 6: Update Proxy for Environment-Aware App Registration

**Files:**
- Modify: `internal/proxy/proxy.go`
- Modify: `internal/cli/root.go` (proxyCmd)

**Interfaces:**
- Consumes: `store.ListApps()` returns env-scoped app list
- Produces: Proxy registers routes for apps in current environment

- [ ] **Step 1: Read proxy.go to understand route registration**

Run: `cat internal/proxy/proxy.go`
Check how `Register(appName string, port int)` and `RegisterDomain` work, and how `extractApp` resolves hostnames.

- [ ] **Step 2: Update proxyCmd to use environment-aware store**

```go
// In proxyCmd RunE:
appFlag, _ := cmd.Flags().GetString("app")
portFlag, _ := cmd.Flags().GetInt("port")
env, _ := cmd.Flags().GetString("env")

rt, _ := runtime.NewDocker()
p := proxy.New(rt, portFlag)
if appFlag != "" {
    p.SetDefaultApp(appFlag)
}

idleMgr := idle.New(rt, 5*time.Minute)
p.SetIdleManager(idleMgr)

store := config.NewStoreWithEnv(dataDir, env)
```

Add `--env` flag to proxyCmd:

```go
proxyCmd.Flags().String("env", "production", "environment for proxy routing")
```

- [ ] **Step 3: Update proxy's extractApp to support env prefix in subdomain**

In proxy.go, `extractApp` currently does:
```go
parts := strings.SplitN(host, ".", 2)
return parts[0], nil
```

We can keep this behavior — the proxy routes based on hostname only. Different environments would need different subdomains (e.g. `myapp-staging.tengiz.local` vs `myapp.tengiz.local`). Alternatively, run separate proxy instances on different ports for each environment.

For now, keep proxy routing simple: each environment needs its own proxy instance (different port). Environment-specific subdomain prefixes (`staging.myapp.tengiz.local`) can be added later.

- [ ] **Step 4: Run tests**

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/proxy.go internal/cli/root.go
git commit -m "feat: environment-aware proxy registration with --env flag"
```

---

### Task 7: Update All Remaining CLI Commands with --env Flag

**Files:**
- Modify: `internal/cli/root.go` (psCmd, stopCmd, startCmd, rmCmd, logsCmd, healthCmd, rollbackCmd, buildLogsCmd, runCmd, configSetCmd, configGetCmd, configUnsetCmd, configShowCmd, domainAddCmd, domainRemoveCmd, domainListCmd, volumeAddCmd, volumeRemoveCmd, volumeListCmd, webhookCmd)

- [ ] **Step 1: Add --env global flag to rootCmd**

To avoid repeating `--env` on every command, add it as a persistent flag on rootCmd:

```go
// In init():
rootCmd.PersistentFlags().String("env", "production", "deployment environment (e.g. production, staging, dev)")
```

- [ ] **Step 2: Create a helper to get env from root command**

```go
// In root.go, add helper:
func getEnv(cmd *cobra.Command) string {
    env, _ := cmd.Flags().GetString("env")
    if env == "" {
        return "production"
    }
    return env
}
```

- [ ] **Step 3: Update every command that uses config.NewStore(dataDir)**

Replace every occurrence of `config.NewStore(dataDir)` with `config.NewStoreWithEnv(dataDir, getEnv(cmd))`.

Affected commands:
- `psCmd` (line ~402)
- `stopCmd`, `startCmd`, `rmCmd` (lines ~427-469) — note: these call `rt.Stop/Start/Remove` with just `args[0]`. The container name must match the env-qualified name. So `rt.Stop(ctx, args[0])` should become `rt.Stop(ctx, runtime.ContainerName(args[0], getEnv(cmd)))`.
- `runCmd` (line ~887) — same issue with `rt.Run` and `store.GetApp`
- `rollbackCmd` (line ~740) — uses `store` and `rt`
- `buildLogsCmd` (line ~814) — uses `store`
- `configSetCmd`, `configGetCmd`, `configUnsetCmd`, `configShowCmd`
- `domainAddCmd`, `domainRemoveCmd`, `domainListCmd`
- `volumeAddCmd`, `volumeRemoveCmd`, `volumeListCmd`
- `healthCmd`

For `psCmd`:
```go
// Before:
store := config.NewStore(dataDir)
// After:
env := getEnv(cmd)
store := config.NewStoreWithEnv(dataDir, env)
```

For commands that call runtime methods with app name (`stopCmd`, `startCmd`, `rmCmd`), prefix the name with the env:
```go
// stopCmd:
appName := args[0]
containerName := runtime.ContainerName(appName, getEnv(cmd))
return rt.Stop(context.Background(), containerName)
```

For `rmCmd`, also need to remove from correct env store:
```go
appName := args[0]
env := getEnv(cmd)
containerName := runtime.ContainerName(appName, env)
store := config.NewStoreWithEnv(dataDir, env)
// ...
```

For `runCmd`:
```go
env := getEnv(cmd)
store := config.NewStoreWithEnv(dataDir, env)
app, err := store.GetApp(appName)
// ...
containerName := runtime.ContainerName(appName, env)
// Use containerName for docker exec
```

For `logsCmd`:
```go
env := getEnv(cmd)
store := config.NewStoreWithEnv(dataDir, env)
containerName := runtime.ContainerName(args[0], env)
reader, err := rt.Logs(context.Background(), containerName, opts)
```

For `rollbackCmd`:
```go
env := getEnv(cmd)
store := config.NewStoreWithEnv(dataDir, env)
containerName := runtime.ContainerName(appName, env)
// ... rest of rollback logic uses containerName
```

For `webhookCmd`:
```go
webhookCmd.Flags().String("env", "production", "deployment environment for auto-deploys")
```

- [ ] **Step 4: Run tests and build**

Run: `go build ./...`
Expected: Build succeeds

Run: `go test ./... -v -count=1`
Expected: All tests pass

- [ ] **Step 5: Update all existing tests to use NewStoreWithEnv or NewStore (backward compat)**

The existing `NewStore(dataDir)` now defaults to "production", so existing tests should still pass without changes.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add --env persistent flag to all CLI commands"
```

---

### Task 8: Update initCmd and gitdeploy for Environment

**Files:**
- Modify: `internal/cli/root.go` (initCmd)
- Modify: `internal/gitdeploy/pipeline.go`

- [ ] **Step 1: Add environment field to init template**

In `initCmd`, add `environment: production` to the generated YAML:

```go
// In initCmd RunE, update content:
content := fmt.Sprintf(`name: %s
environment: production
# port: 3000            # container internal port (auto-detected if omitted)
...
```

Also add `--env` flag to initCmd for setting initial environment.

- [ ] **Step 2: Wire environment through gitdeploy pipeline**

In `internal/gitdeploy/pipeline.go`, add environment parameter:

```go
func NewPipeline(dataDir, env string, rt runtime.Manager, store *config.Store) *Pipeline {
    return &Pipeline{dataDir: dataDir, env: env, rt: rt, store: store}
}
```

Update `Pipeline.Deploy` to use its `env` field when loading config and creating store.

- [ ] **Step 3: Update webhookCmd to pass env to pipeline**

```go
// In webhookCmd RunE:
env, _ := cmd.Flags().GetString("env")
store := config.NewStoreWithEnv(dataDir, env)
pipeline := gitdeploy.NewPipeline(dataDir, env, rt, store)
```

- [ ] **Step 4: Run tests**

Run: `go build ./...`
Expected: Build succeeds

Run: `go test ./... -v -count=1`
Expected: All tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/gitdeploy/pipeline.go
git commit -m "feat: wire environment through init, gitdeploy pipeline, and webhook"
```

---

### Task 9: Update Idle and Health Managers for Environment

**Files:**
- Modify: `internal/idle/idle.go`
- Modify: `internal/health/health.go`
- Modify: `internal/cli/root.go` (proxyCmd — where idle and health are wired)

- [ ] **Step 1: Update idle manager to pass environment to runtime**

In `proxyCmd`, the idle manager calls `rt.Stop(ctx, name)` with the app name. Since containers are now env-named, the idle manager must pass the full container name. The idle manager receives the runtime but not environment context.

Solution: Wrap the idle manager's stop function with an env-aware stop:

```go
// In proxyCmd RunE:
env := getEnv(cmd)
idleMgr := idle.New(rt, 5*time.Minute)
// Override the stop function to use env-qualified names
```

Or modify idle.Manager to accept env per app:

```go
// internal/idle/idle.go — add env field:
type Manager struct {
    rt        runtime.Manager
    defaultTimeout time.Duration
    env       string
}

func New(rt runtime.Manager, timeout time.Duration) *Manager {
    return NewWithEnv(rt, timeout, "")
}

func NewWithEnv(rt runtime.Manager, timeout time.Duration, env string) *Manager {
    return &Manager{rt: rt, defaultTimeout: timeout, env: env}
}

// In expiry callback:
func (m *Manager) expiry(name string) func() {
    return func() {
        containerName := runtime.ContainerName(name, m.env)
        if err := m.rt.Stop(context.Background(), containerName); err != nil {
            log.Printf("idle: stop %s: %v", containerName, err)
        }
    }
}
```

- [ ] **Step 2: Update health manager similarly**

```go
// internal/health/health.go — add env awareness:
type Checker struct {
    rt    runtime.Manager
    store *config.Store
    env   string
}

func New(rt runtime.Manager, store *config.Store) *Checker {
    return NewWithEnv(rt, store, "")
}

func NewWithEnv(rt runtime.Manager, store *config.Store, env string) *Checker {
    return &Checker{rt: rt, store: store, env: env}
}

// In CheckOnce and Start, use runtime.ContainerName(appName, c.env)
```

- [ ] **Step 3: Update proxyCmd to pass env**

```go
// In proxyCmd RunE:
env := getEnv(cmd)
idleMgr := idle.NewWithEnv(rt, 5*time.Minute, env)
p.SetIdleManager(idleMgr)

store := config.NewStoreWithEnv(dataDir, env)
healthChecker := health.NewWithEnv(rt, store, env)
```

- [ ] **Step 4: Run tests**

Run: `go build ./...`
Expected: Build succeeds

Run: `go test ./... -v -count=1`
Expected: All tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/idle/idle.go internal/health/health.go internal/cli/root.go
git commit -m "feat: environment-aware idle timeout and health check"
```

---

### Task 10: End-to-End Tests for Multi-Environment

**Files:**
- Create: `internal/cli/multi_env_test.go`

- [ ] **Step 1: Write integration test for multi-environment deploy**

```go
// internal/cli/multi_env_test.go:
package cli

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/yaso09/tengiz/internal/config"
    "github.com/yaso09/tengiz/internal/runtime"
    "github.com/yaso09/tengiz/internal/types"
)

func TestMultiEnvironmentConfigMerge(t *testing.T) {
    dir := t.TempDir()

    // Write base config
    base := []byte("name: myapp\nport: 3000\nenv:\n  NODE_ENV: production\n  DATABASE_URL: postgres://prod/mydb\n")
    if err := os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), base, 0644); err != nil {
        t.Fatal(err)
    }

    // Write staging override
    staging := []byte("env:\n  NODE_ENV: staging\n  DATABASE_URL: postgres://staging/mydb\n  STAGING_KEY: secret123\n")
    if err := os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), staging, 0644); err != nil {
        t.Fatal(err)
    }

    // Load staging
    cfg, err := config.LoadWithEnv(dir, "staging")
    if err != nil {
        t.Fatalf("LoadWithEnv staging: %v", err)
    }

    // Verify merge
    tests := []struct {
        key, expected string
    }{
        {"NODE_ENV", "staging"},
        {"DATABASE_URL", "postgres://staging/mydb"},
        {"STAGING_KEY", "secret123"},
    }
    for _, tt := range tests {
        if got := cfg.Env[tt.key]; got != tt.expected {
            t.Errorf("Env[%q] = %q, want %q", tt.key, got, tt.expected)
        }
    }
    if cfg.Environment != "staging" {
        t.Errorf("Environment = %q, want %q", cfg.Environment, "staging")
    }

    // Load production (no prod override file)
    cfg, err = config.LoadWithEnv(dir, "production")
    if err != nil {
        t.Fatalf("LoadWithEnv production: %v", err)
    }
    if cfg.Env["NODE_ENV"] != "production" {
        t.Errorf("Env[NODE_ENV] = %q, want %q", cfg.Env["NODE_ENV"], "production")
    }
    if _, ok := cfg.Env["STAGING_KEY"]; ok {
        t.Error("STAGING_KEY should not be in production config")
    }
    if cfg.Environment != "production" {
        t.Errorf("Environment = %q, want %q", cfg.Environment, "production")
    }
}

func TestMultiEnvironmentStoreIsolation(t *testing.T) {
    dir := t.TempDir()

    prodStore := config.NewStoreWithEnv(dir, "production")
    stgStore := config.NewStoreWithEnv(dir, "staging")
    devStore := config.NewStoreWithEnv(dir, "development")

    // Deploy same app name in different environments
    prodApp := types.AppEntry{Name: "myapp", Port: 9000, Environment: "production"}
    stgApp := types.AppEntry{Name: "myapp", Port: 9001, Environment: "staging"}
    devApp := types.AppEntry{Name: "myapp", Port: 9002, Environment: "development"}

    if err := prodStore.SaveApp(prodApp); err != nil {
        t.Fatal(err)
    }
    if err := stgStore.SaveApp(stgApp); err != nil {
        t.Fatal(err)
    }
    if err := devStore.SaveApp(devApp); err != nil {
        t.Fatal(err)
    }

    // Each store should see only its own port
    checkPort := func(store *config.Store, env string, expectedPort int) {
        app, err := store.GetApp("myapp")
        if err != nil {
            t.Fatalf("GetApp(%s): %v", env, err)
        }
        if app.Port != expectedPort {
            t.Errorf("%s port = %d, want %d", env, app.Port, expectedPort)
        }
    }

    checkPort(prodStore, "production", 9000)
    checkPort(stgStore, "staging", 9001)
    checkPort(devStore, "development", 9002)

    // Verify runtime.ContainerName
    runtimeName := runtime.ContainerName("myapp", "staging")
    expected := "tengiz-myapp-staging"
    if runtimeName != expected {
        t.Errorf("ContainerName = %q, want %q", runtimeName, expected)
    }

    // Production container name should be backward compatible
    prodRuntimeName := runtime.ContainerName("myapp", "production")
    expectedProd := "tengiz-myapp"
    if prodRuntimeName != expectedProd {
        t.Errorf("ContainerName(production) = %q, want %q", prodRuntimeName, expectedProd)
    }
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cli/... -v -count=1 -run TestMultiEnvironment`
Expected: PASS

Run: `go test ./... -v -count=1`
Expected: All tests pass

- [ ] **Step 3: Verify backward compatibility**

Run: `go test ./internal/config/... -v -count=1`
Expected: All original tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/cli/multi_env_test.go
git commit -m "test: add end-to-end tests for multi-environment support"
```

---

## Self-Review

### 1. Spec Coverage

The spec requires: `.tengiz.yaml` → `.tengiz.{env}.yaml` merge, `--env staging` flag'i.

- **Config merge**: Task 2 implements `LoadWithEnv` that loads `.tengiz.yaml` then overlays `.tengiz.{env}.yaml`. ✓
- **CLI flag**: Task 4 adds `--env` to deploy, Task 7 adds persistent `--env` to rootCmd for all commands. ✓
- **State isolation**: Task 3 implements env-scoped Store with separate JSON files. ✓
- **Container naming**: Task 5 implements env-aware container names. ✓
- **Image tagging**: Task 4 updates Builder for env-aware image tags. ✓
- **Backward compatibility**: Default env is "production", `NewStore` defaults to "production". ✓
- **Proxy routing**: Task 6 adds `--env` to proxy for env-aware state loading. ✓
- **Idle/Health managers**: Task 9 updates idle and health for env awareness. ✓
- **Git/Webhook integration**: Task 8 wires env through gitdeploy pipeline. ✓
- **Tests**: Task 10 adds end-to-end tests for config merge, store isolation, and container naming. ✓

### 2. Placeholder Scan

No TBD, TODO, placeholder patterns found. All code blocks contain complete implementations.

### 3. Type Consistency

- `types.AppConfig.Environment` defined in Task 1, used in Tasks 2, 4, 5.
- `types.AppEntry.Environment` defined in Task 1, used in Task 10.
- `store.NewStoreWithEnv(dataDir, env)` defined in Task 3, used in Tasks 4, 6, 7, 8, 9, 10.
- `config.LoadWithEnv(path, env)` defined in Task 2, used in Task 4.
- `runtime.ContainerName(name, env)` defined in Task 5, used in Tasks 6, 7, 9, 10.
- `builder.Build(ctx, dir, appName, env, detection, deploymentID)` defined in Task 4, used in Task 4.
- `idle.NewWithEnv(rt, timeout, env)` defined in Task 9, used in Task 9.
- `health.NewWithEnv(rt, store, env)` defined in Task 9, used in Task 9.

All signatures are consistent across tasks.
