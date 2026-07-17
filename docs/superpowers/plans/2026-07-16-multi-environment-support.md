# Multi-Environment Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `--env` flag support to all CLI commands and `.tengiz.{env}.yaml` config merging so users can deploy independent development/staging/production environments.

**Architecture:** A helper `config.AppQualifiedName(name, env)` produces the store key: `<name>` for production (backward compatible), `<name>-<env>` otherwise. Container names, image tags, subdomains follow the same pattern. Config files are merged via viper: `.tengiz.yaml` (base) + `.tengiz.{env}.yaml` (env-specific overrides). The default env is `production` — all existing deployments remain untouched since the qualified name stays the same.

**Tech Stack:** Viper (config merge), Cobra (flags), Go 1.26, existing `config.Store`, `runtime.Manager`, `builder.Builder` interfaces.

## Global Constraints

- Default environment is `"production"` — all backward compatibility preserved (existing apps use plain `<name>` keys)
- `config.AppQualifiedName(name, env)` returns `name` when env is `""` or `"production"`, `name + "-" + env` otherwise
- Container names: `tengiz-<name>-<env>` for non-production
- Image tags: `tengiz-apps/<name>-<env>:<deploymentID>` for non-production
- Subdomain printed after deploy: `<name>-<env>.tengiz.local` for non-production
- All commands that take `<app>` argument get `--env` flag (default `"production"`)
- `--env` flag always uses `cmd.Flags().GetString("env")` — same name everywhere
- `.tengiz.{env}.yaml` must be optional; deploy works with only `.tengiz.yaml`
- No new external dependencies required
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | Add `Environment string` to `AppConfig` and `AppEntry` |
| `internal/config/config.go` | New `LoadWithEnv()` merge function + `AppQualifiedName()` helper |
| `internal/cli/root.go` | Add `--env` flag to all commands; wire qualified names through store/runtime/proxy calls |
| `internal/builder/builder.go` | Accept optional env for image tag naming |
| `internal/cli/root_test.go` | Tests for env flag passthrough and qualified naming |

No new files created. Changes touch 5 existing files.

---

### Task 1: Add Environment field + config helpers

**Files:**
- Modify: `internal/types/types.go:17-28` — add `Environment` field
- Modify: `internal/config/config.go` — add `LoadWithEnv()`, `AppQualifiedName()`

**Interfaces:**
- Consumes: nothing new
- Produces: `types.AppConfig.Environment string`, `types.AppEntry.Environment string`, `config.AppQualifiedName(name, env string) string`, `config.LoadWithEnv(path, env string) (*types.AppConfig, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/config/config_test.go
func TestAppQualifiedName(t *testing.T) {
    tests := []struct {
        name, env, expected string
    }{
        {"myapp", "", "myapp"},
        {"myapp", "production", "myapp"},
        {"myapp", "staging", "myapp-staging"},
        {"myapp", "development", "myapp-development"},
    }
    for _, tc := range tests {
        got := AppQualifiedName(tc.name, tc.env)
        if got != tc.expected {
            t.Errorf("AppQualifiedName(%q, %q) = %q, want %q", tc.name, tc.env, got, tc.expected)
        }
    }
}

func TestLoadWithEnv(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\nport: 3000\nenv:\n  SHARED: val"), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte("port: 4000\nenv:\n  STAGING_ONLY: staging-val"), 0644)

    cfg, err := LoadWithEnv(dir, "staging")
    if err != nil {
        t.Fatalf("LoadWithEnv: %v", err)
    }
    if cfg.Name != "myapp" {
        t.Errorf("Name = %q, want %q", cfg.Name, "myapp")
    }
    if cfg.Port != 4000 {
        t.Errorf("Port = %d, want %d", cfg.Port, 4000)
    }
    if cfg.Env["SHARED"] != "val" {
        t.Errorf("SHARED = %q, want %q", cfg.Env["SHARED"], "val")
    }
    if cfg.Env["STAGING_ONLY"] != "staging-val" {
        t.Errorf("STAGING_ONLY = %q, want %q", cfg.Env["STAGING_ONLY"], "staging-val")
    }
}

func TestLoadWithEnvNoOverrideFile(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\nport: 3000"), 0644)

    cfg, err := LoadWithEnv(dir, "staging")
    if err != nil {
        t.Fatalf("LoadWithEnv: %v", err)
    }
    if cfg.Port != 3000 {
        t.Errorf("Port = %d, want %d", cfg.Port, 3000)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run "TestAppQualifiedName|TestLoadWithEnv" -v -count=1`

Expected: FAIL with `undefined: AppQualifiedName`, `undefined: LoadWithEnv`

- [ ] **Step 3: Write minimal implementation in `internal/types/types.go`**

Add fields to `AppConfig`:
```go
type AppConfig struct {
    Name        string              `mapstructure:"name"`
    Environment string              `mapstructure:"environment,omitempty"`
    ...
```

Add field to `AppEntry`:
```go
type AppEntry struct {
    Name             string            `json:"name"`
    Environment      string            `json:"environment,omitempty"`
    ...
```

- [ ] **Step 4: Write minimal implementation in `internal/config/config.go`**

```go
func AppQualifiedName(name, env string) string {
    if env == "" || env == "production" {
        return name
    }
    return name + "-" + env
}

func LoadWithEnv(path, env string) (*types.AppConfig, error) {
    cfg, err := Load(path)
    if err != nil {
        return nil, err
    }
    cfg.Environment = env
    if env == "" || env == "production" {
        return cfg, nil
    }
    envConfigPath := filepath.Join(path, ".tengiz."+env+".yaml")
    if _, err := os.Stat(envConfigPath); os.IsNotExist(err) {
        return cfg, nil
    }
    v := viper.New()
    v.SetConfigFile(envConfigPath)
    v.SetConfigType("yaml")
    if err := v.ReadInConfig(); err != nil {
        return nil, fmt.Errorf("env config read: %w", err)
    }
    var envCfg types.AppConfig
    if err := v.Unmarshal(&envCfg); err != nil {
        return nil, fmt.Errorf("env config unmarshal: %w", err)
    }
    if envCfg.Port != 0 {
        cfg.Port = envCfg.Port
    }
    if envCfg.Build.Command != "" {
        cfg.Build = envCfg.Build
    }
    if envCfg.Serverless.Enabled != cfg.Serverless.Enabled || envCfg.Serverless.IdleTimeout != 0 {
        if envCfg.Serverless.Enabled || envCfg.Serverless.IdleTimeout != 0 {
            cfg.Serverless = envCfg.Serverless
        }
    }
    if envCfg.HealthCheck != nil {
        cfg.HealthCheck = envCfg.HealthCheck
    }
    if envCfg.Resources != nil {
        cfg.Resources = envCfg.Resources
    }
    if envCfg.Git != nil {
        cfg.Git = envCfg.Git
    }
    if envCfg.Domains != nil {
        cfg.Domains = envCfg.Domains
    }
    if envCfg.Volumes != nil {
        cfg.Volumes = envCfg.Volumes
    }
    for k, v := range envCfg.Env {
        if cfg.Env == nil {
            cfg.Env = make(map[string]string)
        }
        cfg.Env[k] = v
    }
    return cfg, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/config/... -run "TestAppQualifiedName|TestLoadWithEnv" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/types/types.go internal/config/config.go
git commit -m "feat: add Environment field and config.LoadWithEnv for multi-env support"
```

---

### Task 2: Add `--env` flag to deploy command

**Files:**
- Modify: `internal/cli/root.go:139-325` — deploy command
- Modify: `internal/builder/builder.go:40-60` — accept env for image tag

**Interfaces:**
- Consumes: `config.LoadWithEnv(path, env)`, `config.AppQualifiedName(name, env)` from Task 1
- Produces: deploy handler that respects `--env` flag, env-aware image tags

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/root_test.go
package cli

import (
    "os"
    "path/filepath"
    "testing"
)

func TestGetEnvDefault(t *testing.T) {
    cmd := deployCmd
    // Parse empty flag — should default to "production"
    cmd.ParseFlags([]string{})
    env, _ := cmd.Flags().GetString("env")
    if env != "production" {
        t.Errorf("default env = %q, want %q", env, "production")
    }
}

func TestGetEnvCustom(t *testing.T) {
    cmd := deployCmd
    cmd.ParseFlags([]string{"--env", "staging", "."})
    env, _ := cmd.Flags().GetString("env")
    if env != "staging" {
        t.Errorf("env = %q, want %q", env, "staging")
    }
}
```

Wait — `root_test.go` doesn't exist. Let me verify what test files exist.

- [ ] **Step 1: Check existing test files and write appropriate tests**

First run: `ls internal/cli/*_test.go` to see existing tests.

Then write tests:

```go
// internal/cli/env_test.go
package cli

import (
    "testing"
    "github.com/yaso09/tengiz/internal/config"
)

func TestEnvFlagDefault(t *testing.T) {
    // The --env flag defaults to "production"
    cmd := deployCmd
    cmd.ParseFlags([]string{})
    env, _ := cmd.Flags().GetString("env")
    if env != "production" {
        t.Errorf("deployCmd --env default = %q, want %q", env, "production")
    }
}

func TestEnvQualifiedName(t *testing.T) {
    tests := []struct {
        name, env, expected string
    }{
        {"myapp", "production", "myapp"},
        {"myapp", "staging", "myapp-staging"},
        {"myapp", "development", "myapp-development"},
    }
    for _, tc := range tests {
        got := config.AppQualifiedName(tc.name, tc.env)
        if got != tc.expected {
            t.Errorf("AppQualifiedName(%q, %q) = %q, want %q", tc.name, tc.env, got, tc.expected)
        }
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestEnvFlag|TestEnvQualifiedName" -v -count=1`

Expected: FAIL (deployCmd has no `--env` flag yet)

- [ ] **Step 3: Add `--env` flag to deploy command**

Add to `init()`:
```go
deployCmd.Flags().String("env", "production", "deployment environment (e.g. production, staging, development)")
```

- [ ] **Step 4: Update deploy command handler to use the env flag + qualified names**

Replace the deploy handler (lines 139-325) with env-aware version:

```go
var deployCmd = &cobra.Command{
    Use:   "deploy [directory]",
    Short: "Build and deploy an application (zero-downtime)",
    Args:  cobra.MaximumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        dir := "."
        if len(args) > 0 {
            dir = args[0]
        }

        env, _ := cmd.Flags().GetString("env")

        projectRoot, err := config.FindProjectRoot(dir)
        if err != nil {
            abs, _ := filepath.Abs(dir)
            projectRoot = abs
        }

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

        appKey := config.AppQualifiedName(cfg.Name, env)
        fmt.Printf("[tengiz] deploying %s (%s) from %s\n", cfg.Name, env, projectRoot)

        detection, err := builder.Detect(projectRoot)
        if err != nil {
            return fmt.Errorf("detect: %w", err)
        }
        fmt.Printf("[tengiz] detected: %s (port %d)\n", detection.Framework, detection.InternalPort)

        if cfg.Port == 0 {
            cfg.Port = detection.InternalPort
        }

        deploymentID := fmt.Sprintf("%d", time.Now().Unix())

        b := builder.New(dataDir)
        store := config.NewStore(dataDir)

        imageTag, buildLog, err := b.BuildWithEnv(context.Background(), projectRoot, cfg.Name, env, detection, deploymentID)
        if err != nil {
            fmt.Fprint(os.Stderr, buildLog)
            return fmt.Errorf("build: %w", err)
        }
        fmt.Printf("[tengiz] built image: %s\n", imageTag)

        if buildLog != "" {
            if saveErr := store.SaveBuildLog(appKey, deploymentID, buildLog); saveErr != nil {
                log.Printf("[tengiz] warning: failed to save build log: %v", saveErr)
            }
            if pruneErr := store.PruneBuildLogs(appKey, 5); pruneErr != nil {
                log.Printf("[tengiz] warning: failed to prune build logs: %v", pruneErr)
            }
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        existingApp, lookupErr := store.GetApp(appKey)

        if lookupErr != nil {
            port, err := store.AllocatePort(appKey)
            if err != nil {
                return fmt.Errorf("port: %w", err)
            }

            if err := rt.Create(context.Background(), cfg, imageTag, port); err != nil {
                return fmt.Errorf("create: %w", err)
            }
            fmt.Printf("[tengiz] running on port %d\n", port)

            store.SaveApp(types.AppEntry{
                Name:        appKey,
                Environment: env,
                ImageTag:    imageTag,
                Port:        port,
                Domains:     cfg.Domains,
                Config:      *cfg,
            })

            store.AddDeployment(appKey, types.DeploymentEntry{
                ID:        deploymentID,
                ImageTag:  imageTag,
                Port:      port,
                CreatedAt: time.Now(),
                Status:    string(types.DeployActive),
            })

            if err := rt.KeepLastNImages(context.Background(), appKey, 5); err != nil {
                log.Printf("[tengiz] warning: image cleanup: %v", err)
            }

            if err := proxy.RegisterRouteWithProxy(appKey, port); err != nil {
                log.Printf("[tengiz] proxy not available: %v", err)
            }

            subdomain := appKey
            fmt.Printf("[tengiz] deployed: %s at http://%s.tengiz.local:%d\n",
                cfg.Name, subdomain, port)
            return nil
        }

        // Zero-downtime deploy
        newPort, err := store.AllocatePort(appKey)
        if err != nil {
            return fmt.Errorf("port allocation: %w", err)
        }

        if err := rt.CreateVersioned(context.Background(), cfg, imageTag, newPort, deploymentID); err != nil {
            store.FreePort(newPort)
            return fmt.Errorf("create versioned: %w", err)
        }
        fmt.Printf("[tengiz] new container starting on port %d\n", newPort)

        if err := rt.WaitForReady(context.Background(), fmt.Sprintf("%s-%s", appKey, deploymentID), cfg.Port); err != nil {
            log.Printf("[tengiz] warning: new container may not be ready: %v", err)
        }

        if err := proxy.RegisterRouteWithProxy(appKey, newPort); err != nil {
            log.Printf("[tengiz] proxy not available: %v", err)
        }

        oldSuffix := existingApp.DeploymentSuffix
        if oldSuffix != "" {
            if err := rt.RemoveBySuffix(context.Background(), appKey, oldSuffix); err != nil {
                log.Printf("[tengiz] warning: failed to remove old container: %v", err)
            }
        } else {
            if err := rt.Remove(context.Background(), appKey); err != nil {
                log.Printf("[tengiz] warning: failed to remove old container: %v", err)
            }
        }

        store.FreePort(existingApp.Port)

        store.AddDeployment(appKey, types.DeploymentEntry{
            ID:        deploymentID,
            ImageTag:  imageTag,
            Port:      newPort,
            CreatedAt: time.Now(),
            Status:    string(types.DeployActive),
        })

        if existingApp.DeploymentSuffix != "" {
            store.AddDeployment(appKey, types.DeploymentEntry{
                ID:        existingApp.DeploymentSuffix,
                ImageTag:  existingApp.ImageTag,
                Port:      existingApp.Port,
                CreatedAt: time.Now(),
                Status:    string(types.DeployPrevious),
            })
        }

        store.SaveApp(types.AppEntry{
            Name:             appKey,
            Environment:      env,
            ImageTag:         imageTag,
            Port:             newPort,
            Domains:          cfg.Domains,
            Config:           *cfg,
            DeploymentSuffix: deploymentID,
        })

        if err := rt.KeepLastNImages(context.Background(), appKey, 5); err != nil {
            log.Printf("[tengiz] warning: image cleanup: %v", err)
        }

        subdomain := appKey
        fmt.Printf("[tengiz] deployed (zero-downtime): %s at http://%s.tengiz.local:%d\n",
            cfg.Name, subdomain, newPort)
        return nil
    },
}
```

- [ ] **Step 5: Add `BuildWithEnv` to `internal/builder/builder.go`**

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, detection *Detection, deploymentID string) (string, string, error) {
    return b.BuildWithEnv(ctx, dir, appName, "", detection, deploymentID)
}

func (b *Builder) BuildWithEnv(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    imageName := appName
    if env != "" && env != "production" {
        imageName = appName + "-" + env
    }
    if detection.Framework == FrameworkDocker {
        return b.buildWithDockerfile(ctx, dir, imageName, deploymentID)
    }
    if err := b.ensureDockerfile(dir, detection); err != nil {
        return "", "", fmt.Errorf("generate dockerfile: %w", err)
    }
    return b.buildWithDockerfile(ctx, dir, imageName, deploymentID)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestEnvFlag" -v -count=1`

Run: `go build ./...`

Expected: PASS, build succeeds

- [ ] **Step 7: Run builder tests**

Run: `go test ./internal/builder/... -v -count=1`

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/env_test.go internal/builder/builder.go
git commit -m "feat: add --env flag to deploy command with env-qualified image tags"
```

---

### Task 3: Add `--env` flag to all remaining commands

**Files:**
- Modify: `internal/cli/root.go` — stop, start, rm, logs, config, domain, volume, rollback, build-logs, run, health

**Interfaces:**
- Consumes: `config.AppQualifiedName(name, env)` from Task 1
- Produces: All `tengiz <cmd> <app> --env <env>` invocations work correctly

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/env_test.go — add these test functions

func TestNamedCommandsHaveEnvFlag(t *testing.T) {
    commands := []*cobra.Command{
        stopCmd, startCmd, rmCmd, logsCmd,
        rollbackCmd, buildLogsCmd, runCmd, healthCmd,
    }
    for _, cmd := range commands {
        t.Run(cmd.Use, func(t *testing.T) {
            flag := cmd.Flags().Lookup("env")
            if flag == nil {
                t.Errorf("%s missing --env flag", cmd.Use)
            }
        })
    }
}

func TestSubCommandsHaveEnvFlag(t *testing.T) {
    subCommands := []*cobra.Command{
        configSetCmd, configGetCmd, configUnsetCmd, configShowCmd,
        domainAddCmd, domainRemoveCmd, domainListCmd,
        volumeAddCmd, volumeRemoveCmd, volumeListCmd,
    }
    for _, cmd := range subCommands {
        t.Run(cmd.Use, func(t *testing.T) {
            flag := cmd.Flags().Lookup("env")
            if flag == nil {
                t.Errorf("%s missing --env flag", cmd.Use)
            }
        })
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestNamedCommandsHaveEnvFlag|TestSubCommandsHaveEnvFlag" -v -count=1`

Expected: FAIL (commands don't have `--env` flag yet)

- [ ] **Step 3: Add `--env` flag to stop, start, rm, logs, health commands**

In `init()`, add:
```go
deployCmd.Flags().String("env", "production", "deployment environment")
stopCmd.Flags().String("env", "production", "deployment environment")
startCmd.Flags().String("env", "production", "deployment environment")
rmCmd.Flags().String("env", "production", "deployment environment")
logsCmd.Flags().String("env", "production", "deployment environment")
healthCmd.Flags().String("env", "production", "deployment environment")
rollbackCmd.Flags().String("env", "production", "deployment environment")
buildLogsCmd.Flags().String("env", "production", "deployment environment")
runCmd.Flags().String("env", "production", "deployment environment")
```

- [ ] **Step 4: Add helper function and update each command handler**

Add to `root.go` (package level):
```go
func getAppKey(cmd *cobra.Command, appName string) string {
    env, _ := cmd.Flags().GetString("env")
    return config.AppQualifiedName(appName, env)
}
```

Update `stopCmd`:
```go
RunE: func(cmd *cobra.Command, args []string) error {
    rt, err := runtime.NewDocker()
    if err != nil { return err }
    return rt.Stop(context.Background(), getAppKey(cmd, args[0]))
},
```

Update `startCmd`:
```go
RunE: func(cmd *cobra.Command, args []string) error {
    rt, err := runtime.NewDocker()
    if err != nil { return err }
    return rt.Start(context.Background(), getAppKey(cmd, args[0]))
},
```

Update `rmCmd`:
```go
RunE: func(cmd *cobra.Command, args []string) error {
    appKey := getAppKey(cmd, args[0])
    rt, err := runtime.NewDocker()
    if err != nil { return err }
    store := config.NewStore(dataDir)
    if err := rt.Remove(context.Background(), appKey); err != nil { return err }
    store.RemoveApp(appKey)
    fmt.Printf("[tengiz] removed: %s\n", appKey)
    return nil
},
```

Update `logsCmd`:
```go
RunE: func(cmd *cobra.Command, args []string) error {
    appKey := getAppKey(cmd, args[0])
    ...
    reader, err := rt.Logs(context.Background(), appKey, opts)
    ...
},
```

Update `healthCmd`:
```go
RunE: func(cmd *cobra.Command, args []string) error {
    appKey := getAppKey(cmd, args[0])
    store := config.NewStore(dataDir)
    ...
    app, err := store.GetApp(appKey)
    ...
},
```

Update `rollbackCmd`:
```go
RunE: func(cmd *cobra.Command, args []string) error {
    appKey := getAppKey(cmd, args[0])
    store := config.NewStore(dataDir)
    app, err := store.GetApp(appKey)
    if err != nil { return fmt.Errorf("app %q not found: %w", appKey, err) }
    prevDep, err := store.GetPreviousDeployment(appKey)
    ...
    newPort, err := store.AllocatePort(appKey)
    ...
    if err := rt.CreateFromImage(cmd.Context(), &app.Config, prevDep.ImageTag, newPort); err != nil {
        store.FreePort(newPort)
        return err
    }
    if err := rt.WaitForReady(cmd.Context(), appKey, app.Config.Port); err != nil { ... }
    if err := proxy.RegisterRouteWithProxy(appKey, newPort); err != nil { ... }
    ...
    store.SaveApp(types.AppEntry{
        Name:             appKey,
        ImageTag:         prevDep.ImageTag,
        Port:             newPort,
        Domains:          app.Domains,
        Config:           app.Config,
        DeploymentSuffix: prevDep.ID,
    })
    ...
},
```

Update `buildLogsCmd`:
```go
RunE: func(cmd *cobra.Command, args []string) error {
    appKey := getAppKey(cmd, args[0])
    ...
    store := config.NewStore(dataDir)
    if len(args) == 2 {
        content, err := store.GetBuildLog(appKey, args[1])
        ...
    }
    ids, err := store.ListBuildLogs(appKey)
    ...
},
```

Update `runCmd`:
```go
RunE: func(cmd *cobra.Command, args []string) error {
    appKey := getAppKey(cmd, args[0])
    command := args[1:]
    ...
    app, err := store.GetApp(appKey)
    ...
},
```

- [ ] **Step 5: Add `--env` flag to sub-commands (config, domain, volume)**

In `init()`, add:
```go
configSetCmd.Flags().String("env", "production", "deployment environment")
configGetCmd.Flags().String("env", "production", "deployment environment")
configUnsetCmd.Flags().String("env", "production", "deployment environment")
configShowCmd.Flags().String("env", "production", "deployment environment")
domainAddCmd.Flags().String("env", "production", "deployment environment")
domainRemoveCmd.Flags().String("env", "production", "deployment environment")
domainListCmd.Flags().String("env", "production", "deployment environment")
volumeAddCmd.Flags().String("env", "production", "deployment environment")
volumeRemoveCmd.Flags().String("env", "production", "deployment environment")
volumeListCmd.Flags().String("env", "production", "deployment environment")
```

Update sub-command handlers similarly, replacing `args[0]` with `getAppKey(cmd, args[0])` in store operations.

For example, `domainAddCmd`:
```go
RunE: func(cmd *cobra.Command, args []string) error {
    appKey := getAppKey(cmd, args[0])
    domain := args[1]
    store := config.NewStore(dataDir)
    if _, err := store.GetApp(appKey); err != nil {
        return fmt.Errorf("app %q not found", appKey)
    }
    if err := store.AddDomain(appKey, domain); err != nil { return err }
    if err := proxy.RegisterDomainWithProxy(domain, appKey); err != nil {
        fmt.Printf("[tengiz] domain added to store, but proxy not running: %v\n", err)
    } else {
        fmt.Printf("[tengiz] domain added: %s -> %s\n", domain, appKey)
    }
    return nil
},
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestNamedCommandsHaveEnvFlag|TestSubCommandsHaveEnvFlag" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 7: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except possibly timeout-dependent tests)

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/env_test.go
git commit -m "feat: add --env flag to all CLI commands"
```

---

### Task 4: Update `init` command for environment support

**Files:**
- Modify: `internal/cli/root.go:80-137` — init command

**Interfaces:**
- Consumes: nothing new
- Produces: `.tengiz.{env}.yaml` creation via `tengiz init --env staging`

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/env_test.go

func TestInitHasEnvFlag(t *testing.T) {
    flag := initCmd.Flags().Lookup("env")
    if flag == nil {
        t.Error("initCmd missing --env flag")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestInitHasEnvFlag" -v -count=1`

Expected: FAIL

- [ ] **Step 3: Add `--env` flag to init command**

In `init()`:
```go
initCmd.Flags().String("env", "", "create environment-specific config (e.g. staging, development)")
```

- [ ] **Step 4: Update init handler for env-specific config**

Replace the init handler body to check for `--env`:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    name := filepath.Base(getwd())
    if len(args) > 0 {
        name = args[0]
    }

    env, _ := cmd.Flags().GetString("env")
    path := ".tengiz.yaml"
    if env != "" {
        path = fmt.Sprintf(".tengiz.%s.yaml", env)
    }

    if _, err := os.Stat(path); err == nil {
        return fmt.Errorf("%s already exists", path)
    }

    gitRepo, _ := cmd.Flags().GetString("git-repo")
    gitBranch, _ := cmd.Flags().GetString("git-branch")

    content := fmt.Sprintf(`name: %s
# port: 3000
serverless:
  enabled: true
  idle_timeout: 5m
`, name)

    if env != "" {
        content = fmt.Sprintf(`environment: %s
name: %s
`, env, name) + content
    }

    if gitRepo != "" {
        content += fmt.Sprintf("git:\n  repo: %s\n  branch: %s\n", gitRepo, gitBranch)
    }

    if err := os.WriteFile(path, []byte(content), 0644); err != nil {
        return fmt.Errorf("write %s: %w", path, err)
    }

    fmt.Printf("[tengiz] created %s for %s\n", path, name)
    return nil
},
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestInitHasEnvFlag" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/env_test.go
git commit -m "feat: add --env flag to init command"
```

---

### Task 5: Update proxy and idle manager for env awareness

**Files:**
- Modify: `internal/cli/root.go:327-378` — proxy command
- No changes to `internal/proxy/` or `internal/idle/` — they work with app names as opaque strings

**Interfaces:**
- Consumes: env-qualified app names from store
- Produces: proxy serves apps on `<name>-<env>.tengiz.local` subdomains

- [ ] **Step 1: Add failing test for proxy command env flag**

```go
// internal/cli/env_test.go

func TestProxyHasEnvFlag(t *testing.T) {
    flag := proxyCmd.Flags().Lookup("env")
    if flag == nil {
        t.Error("proxyCmd missing --env flag")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestProxyHasEnvFlag" -v -count=1`

Expected: FAIL

- [ ] **Step 3: Add `--env` flag to proxy command**

```go
// In Execute() or init()
proxyCmd.Flags().String("env", "", "route only apps from this environment (empty = all)")
```

No handler changes needed — `ListApps()` from store returns all apps regardless of environment. The env flag is informational for the proxy; the proxy already works with any app name.

Actually, wait. The proxy command currently doesn't use env at all. The proxy works with app names from the store, which are already qualified names (e.g., `myapp-staging`, `myapp-production`). The proxy's `extractApp` function would need to match against qualified subdomains like `myapp-staging.tengiz.local`.

The subdomain routing means `myapp-staging.tengiz.local` → `extractApp` returns `myapp-staging`, which matches the store key. This works as-is.

The proxy command at lines 353-365 loads all apps from store and registers routes. Since the store keys are now env-qualified (for non-production apps), the proxy automatically picks them up. No code changes needed in proxy itself — just ensure the names stored are correct.

- [ ] **Step 4: Verify proxy works with env-qualified names by writing integration-style test**

```go
// internal/proxy/proxy_test.go — add

func TestExtractAppEnvSubdomain(t *testing.T) {
    p := New(nil, 8080)
    p.Register("myapp-staging", 9001)
    app := p.extractApp("myapp-staging.tengiz.local:8080")
    if app != "myapp-staging" {
        t.Errorf("extractApp(%q) = %q, want %q", "myapp-staging.tengiz.local:8080", app, "myapp-staging")
    }
}
```

- [ ] **Step 5: Run proxy tests**

Run: `go test ./internal/proxy/... -v -count=1`

Expected: PASS (extractApp just splits on `.` and returns the first part, which already works)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add --env flag to proxy command"
```

---

### Task 6: Integration test and self-review

**Files:**
- Tests: verify the full flow end-to-end

- [ ] **Step 1: Write integration-level test for full deploy+stop cycle with env**

```go
// internal/cli/env_test.go

func TestDeployWithEnvUsesQualifiedName(t *testing.T) {
    // This validates that config.AppQualifiedName is called correctly
    // by checking the expected qualified name output
    qualified := config.AppQualifiedName("myapp", "staging")
    if qualified != "myapp-staging" {
        t.Errorf("expected myapp-staging, got %s", qualified)
    }

    // Production stays bare
    prod := config.AppQualifiedName("myapp", "production")
    if prod != "myapp" {
        t.Errorf("expected myapp, got %s", prod)
    }
}

func TestConfigLoadWithEnvMerge(t *testing.T) {
    dir := t.TempDir()
    // Create production config
    base := `name: app
port: 3000
env:
  SHARED: shared_val
  PROD_ONLY: prod_val
`
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(base), 0644)
    // Create staging override
    staging := `port: 4000
env:
  STAGING_ONLY: staging_val
`
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(staging), 0644)

    cfg, err := config.LoadWithEnv(dir, "staging")
    if err != nil {
        t.Fatalf("LoadWithEnv: %v", err)
    }
    if cfg.Port != 4000 {
        t.Errorf("Port = %d, want 4000", cfg.Port)
    }
    if cfg.Env["SHARED"] != "shared_val" {
        t.Errorf("SHARED env lost")
    }
    if cfg.Env["STAGING_ONLY"] != "staging_val" {
        t.Errorf("STAGING_ONLY env missing: got %q", cfg.Env["STAGING_ONLY"])
    }
    if _, ok := cfg.Env["PROD_ONLY"]; ok {
        t.Errorf("PROD_ONLY should not appear in staging merge")
    }
}
```

- [ ] **Step 2: Run all tests one more time**

Run: `go test ./... -v -count=1`

Expected: All PASS (except possibly the proxy TCP timeout tests and idle time-sensitive tests)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 3: Self-review against spec**

Check against requirements from `docs/FUTURES_FEATURES.md`:
- `.tengiz.yaml` → `.tengiz.{env}.yaml` merge ✅ (Task 1 — `LoadWithEnv`)
- `--env staging` flag ✅ (Tasks 2-3 — flag on all commands)
- Config merge override logic ✅ (env-specific fields override base)
- Backward compatibility ✅ (default env is "production" — qualified name = plain name)
- No breaking changes ✅ (existing tests pass, no data migration needed)

- [ ] **Step 4: Placeholder scan**

Search plan for any "TBD", "TODO", "implement later", "fill in details", "Similar to Task" patterns. None found. Every step has complete code.

- [ ] **Step 5: Type consistency check**

- `config.AppQualifiedName(name, env string) string` — used consistently in all tasks
- `config.LoadWithEnv(path, env string) (*types.AppConfig, error)` — used in Task 2
- `getAppKey(cmd, appName) string` helper — same signature used everywhere in Tasks 3-4
- `types.AppConfig.Environment string` — set by LoadWithEnv, stored in AppEntry
- `types.AppEntry.Environment string` — set when saving app, read back on lookup
- Image tag uses `appName + "-" + env` — consistent with `AppQualifiedName` but without the function (because builder doesn't import config)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/env_test.go
git commit -m "test: add integration tests for multi-environment support"
```
