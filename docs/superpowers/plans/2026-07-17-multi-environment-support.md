# Multi-Environment Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow deploying the same app with environment-specific config overrides via `.tengiz.{env}.yaml` files and a `--env` flag.

**Architecture:** Extend `config.Load()` to accept an optional environment name, load `.tengiz.yaml` as the base, then overlay `.tengiz.{env}.yaml` using a shallow merge for scalars and deep merge for the `Env` map. Add an `Environment` field to `AppConfig` for tracking. Wire a `--env` flag through the `deploy` command and display the environment in `tengiz ps`.

**Tech Stack:** Go 1.26, viper (existing), mapstructure (existing), cobra (existing)

## Global Constraints

- Zero new dependencies beyond `cobra` and `viper` (current direct deps only)
- Environment name must match `^[a-zA-Z0-9_-]+$` regex (alphanumeric, dashes, underscores)
- `.tengiz.{env}.yaml` must be optional — if the file doesn't exist, silently use base config only
- `Env` map merge must be additive: env file keys add/override, never replace entire map
- All scalar fields (name, port, resources, etc.) must use env-file-overrides-base behavior
- Feature must be testable without Docker (unit tests only)

---

## File Structure

| File | Change | Responsibility |
|------|--------|----------------|
| `internal/types/types.go` | Modify | Add `Environment string` field to `AppConfig` |
| `internal/config/config.go` | Modify | Add `LoadForEnvironment(path, env)` function with merge logic |
| `internal/config/config_test.go` | Modify | Add tests for env-specific config loading + merge |
| `internal/cli/root.go` | Modify | Add `--env` flag to `deployCmd`, wire through deploy flow, show env in `ps` |
| `internal/gitdeploy/deployer.go` | Modify | Carry over `Environment` from stored app entry |

---

### Task 1: Core Config Merge Logic

**Files:**
- Modify: `internal/types/types.go:17-28`
- Modify: `internal/config/config.go:15-42`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `types.AppConfig` (existing struct)
- Produces: `config.LoadForEnvironment(path, env string) (*types.AppConfig, error)` — new public function; `types.AppConfig.Environment string` — new field

- [ ] **Step 1: Add Environment field to AppConfig**

Edit `internal/types/types.go` — add `Environment` field after `Name`:

```go
type AppConfig struct {
	Name        string              `mapstructure:"name"`
	Environment string              `mapstructure:"environment,omitempty"`
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

- [ ] **Step 2: Write failing test for LoadForEnvironment**

Add to `internal/config/config_test.go`:

```go
func TestLoadForEnvironment_withEnvFile(t *testing.T) {
	dir := t.TempDir()
	base := `
name: myapp
port: 3000
env:
  APP_ENV: base
  SHARED_VAR: from-base
`
	env := `
port: 4000
env:
  APP_ENV: staging
  STAGING_SECRET: shh
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(base), 0644)
	os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(env), 0644)

	cfg, err := LoadForEnvironment(dir, "staging")
	if err != nil {
		t.Fatalf("LoadForEnvironment() error = %v", err)
	}

	if cfg.Name != "myapp" {
		t.Errorf("Name = %q, want %q", cfg.Name, "myapp")
	}
	if cfg.Port != 4000 {
		t.Errorf("Port = %d, want %d", cfg.Port, 4000)
	}
	if cfg.Environment != "staging" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "staging")
	}

	// Env map should be merged: env file overrides base, new keys added
	if cfg.Env["APP_ENV"] != "staging" {
		t.Errorf("APP_ENV = %q, want %q", cfg.Env["APP_ENV"], "staging")
	}
	if cfg.Env["SHARED_VAR"] != "from-base" {
		t.Errorf("SHARED_VAR = %q, want %q", cfg.Env["SHARED_VAR"], "from-base")
	}
	if cfg.Env["STAGING_SECRET"] != "shh" {
		t.Errorf("STAGING_SECRET = %q, want %q", cfg.Env["STAGING_SECRET"], "shh")
	}
}

func TestLoadForEnvironment_missingEnvFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\nport: 3000\n"), 0644)

	cfg, err := LoadForEnvironment(dir, "production")
	if err != nil {
		t.Fatalf("LoadForEnvironment() error = %v", err)
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want %d", cfg.Port, 3000)
	}
	if cfg.Environment != "production" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "production")
	}
}

func TestLoadForEnvironment_envMergePreservesBase(t *testing.T) {
	dir := t.TempDir()
	base := `
name: myapp
port: 3000
env:
  DATABASE_URL: postgres://localhost/mydb
  API_KEY: base-key
`
	env := `
env:
  API_KEY: staging-key
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(base), 0644)
	os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(env), 0644)

	cfg, err := LoadForEnvironment(dir, "staging")
	if err != nil {
		t.Fatalf("LoadForEnvironment() error = %v", err)
	}

	// Base key preserved, env key overridden
	if cfg.Env["DATABASE_URL"] != "postgres://localhost/mydb" {
		t.Errorf("DATABASE_URL = %q, want %q", cfg.Env["DATABASE_URL"], "postgres://localhost/mydb")
	}
	if cfg.Env["API_KEY"] != "staging-key" {
		t.Errorf("API_KEY = %q, want %q", cfg.Env["API_KEY"], "staging-key")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config/... -v -count=1 -run 'TestLoadForEnvironment'`
Expected: FAIL with `undefined: LoadForEnvironment`

- [ ] **Step 4: Implement LoadForEnvironment in config.go**

Edit `internal/config/config.go` — add the new function after `Load()`:

```go
func LoadForEnvironment(path, env string) (*types.AppConfig, error) {
	if env != "" {
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(env) {
			return nil, fmt.Errorf("invalid environment name %q: use only alphanumeric, dashes, and underscores", env)
		}
	}

	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	cfg.Environment = env

	if env == "" {
		return cfg, nil
	}

	envConfigPath := filepath.Join(path, fmt.Sprintf(".tengiz.%s.yaml", env))
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

	// Scalar overrides
	if envCfg.Port != 0 {
		cfg.Port = envCfg.Port
	}
	if envCfg.Build.Command != "" {
		cfg.Build.Command = envCfg.Build.Command
	}
	if envCfg.Build.Output != "" {
		cfg.Build.Output = envCfg.Build.Output
	}
	if envCfg.Name != "" {
		cfg.Name = envCfg.Name
	}
	if envCfg.Domains != nil {
		cfg.Domains = envCfg.Domains
	}
	if envCfg.HealthCheck != nil {
		cfg.HealthCheck = envCfg.HealthCheck
	}
	if envCfg.Resources != nil {
		cfg.Resources = envCfg.Resources
	}
	if envCfg.Serverless.Enabled != cfg.Serverless.Enabled || envCfg.Serverless.IdleTimeout != 0 {
		if envCfg.Serverless.IdleTimeout != 0 {
			cfg.Serverless = envCfg.Serverless
		} else if envCfg.Serverless.Enabled != cfg.Serverless.Enabled && envCfg.Serverless.IdleTimeout == 0 {
			cfg.Serverless = envCfg.Serverless
		}
	}
	if envCfg.Git != nil {
		cfg.Git = envCfg.Git
	}
	if envCfg.Volumes != nil {
		cfg.Volumes = envCfg.Volumes
	}

	// Deep merge for env map
	if envCfg.Env != nil {
		if cfg.Env == nil {
			cfg.Env = make(map[string]string)
		}
		for k, v := range envCfg.Env {
			cfg.Env[k] = v
		}
	}

	return cfg, nil
}
```

Also add the `regexp` import to the imports block:

```go
import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/spf13/viper"
	"github.com/yaso09/tengiz/internal/types"
)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/config/... -v -count=1 -run 'TestLoadForEnvironment'`
Expected: all 3 tests PASS

- [ ] **Step 6: Run all config tests to ensure no regression**

Run: `go test ./internal/config/... -v -count=1`
Expected: all tests PASS (including existing `TestLoadBasicConfig`, `TestLoadMissingNameField`, etc.)

- [ ] **Step 7: Commit**

```bash
git add internal/types/types.go internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add multi-environment config support with LoadForEnvironment"
```

---

### Task 2: Wire `--env` Flag Through Deploy and Display in `ps`

**Files:**
- Modify: `internal/cli/root.go:139-325` (deployCmd), `internal/cli/root.go:381-424` (psCmd)

**Interfaces:**
- Consumes: `config.LoadForEnvironment(path, env string) (*types.AppConfig, error)` from Task 1
- Produces: `tengiz deploy --env staging` CLI flag; environment column in `tengiz ps` output

- [ ] **Step 1: Write failing integration-style test for env in deploy flow**

Add to a test file (or extend existing CLI tests). Since CLI tests require building the command, we'll verify via unit test that `LoadForEnvironment` is called correctly. Add to `internal/config/config_test.go`:

```go
func TestLoadForEnvironment_validateEnvName(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\n"), 0644)

	_, err := LoadForEnvironment(dir, "staging/prod")
	if err == nil {
		t.Fatal("LoadForEnvironment() expected error for invalid env name")
	}

	_, err = LoadForEnvironment(dir, "good-env_123")
	if err != nil {
		t.Fatalf("LoadForEnvironment() unexpected error for valid env name: %v", err)
	}
}
```

- [ ] **Step 2: Run the new test to verify it fails (env validation not yet implemented in Step 4 of Task 1 — it IS implemented, so this should pass after Task 1. Run anyway)**

Run: `go test ./internal/config/... -v -count=1 -run 'TestLoadForEnvironment_validateEnvName'`
Expected: PASS (validation was included in Task 1)

- [ ] **Step 3: Add --env flag to deployCmd**

In `internal/cli/root.go`, in `init()` function, add the flag registration after existing flags:

```go
deployCmd.Flags().StringP("env", "e", "", "deployment environment (e.g. staging, production)")
```

- [ ] **Step 4: Wire env flag into deploy command RunE**

In `internal/cli/root.go`, in the `deployCmd.RunE` function, change the config loading from:

```go
cfg, err := config.Load(projectRoot)
```

To:

```go
envFlag, _ := cmd.Flags().GetString("env")
cfg, err := config.LoadForEnvironment(projectRoot, envFlag)
```

The full updated deploy `RunE` section (lines 143-164 of full file):

```go
RunE: func(cmd *cobra.Command, args []string) error {
    dir := "."
    if len(args) > 0 {
        dir = args[0]
    }

    projectRoot, err := config.FindProjectRoot(dir)
    if err != nil {
        abs, _ := filepath.Abs(dir)
        projectRoot = abs
    }

    envFlag, _ := cmd.Flags().GetString("env")
    cfg, err := config.LoadForEnvironment(projectRoot, envFlag)
    if err != nil {
        cfg = &types.AppConfig{
            Name: filepath.Base(projectRoot),
            Serverless: types.ServerlessConfig{
                Enabled:     true,
                IdleTimeout: 5 * time.Minute,
            },
            Environment: envFlag,
        }
    }
    // ... rest of deploy logic unchanged
```

- [ ] **Step 5: Add env display to ps output**

In `internal/cli/root.go`, update the `psCmd.RunE` function (lines 381-424). First, add env info by reading from store. Update the `storeApps` loop to capture environment, and update the header and row format:

```go
var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List deployed applications",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		apps, err := rt.List(context.Background())
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}

		if len(apps) == 0 {
			fmt.Println("No applications deployed.")
			return nil
		}

		store := config.NewStore(dataDir)
		storeApps, _ := store.ListApps()
		envMap := make(map[string]string, len(storeApps))
		healthMap := make(map[string]string, len(storeApps))
		for _, sa := range storeApps {
			envMap[sa.Name] = sa.Config.Environment
			healthMap[sa.Name] = sa.HealthStatus
			if healthMap[sa.Name] == "" {
				healthMap[sa.Name] = string(types.HealthUnknown)
			}
		}

		fmt.Printf("%-20s %-10s %-8s %-12s %-10s\n", "NAME", "STATE", "PORT", "ENVIRONMENT", "HEALTH")
		for _, a := range apps {
			portStr := fmt.Sprintf("%d", a.Port)
			if a.Port == 0 {
				portStr = "-"
			}
			health := healthMap[a.Name]
			if health == "" {
				health = string(types.HealthUnknown)
			}
			env := envMap[a.Name]
			if env == "" {
				env = "-"
			}
			fmt.Printf("%-20s %-10s %-8s %-12s %-10s\n", a.Name, a.State, portStr, env, health)
		}
		return nil
	},
}
```

- [ ] **Step 6: Build and verify compilation**

Run: `go build -o /dev/null .`
Expected: builds without error

- [ ] **Step 7: Run all tests to check for regressions**

Run: `go test ./... -v -count=1 2>&1 | head -80`
Expected: all tests pass

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat(cli): add --env flag to deploy and show environment in ps"
```

---

### Task 3: Environment Carry-Over in Git Deploy and Rollback

**Files:**
- Modify: `internal/gitdeploy/deployer.go:82-92` (existing app config carry-over)
- Verify: `internal/cli/root.go:740-812` (rollback — no change needed, Config is already preserved)

**Interfaces:**
- Consumes: `types.AppConfig.Environment` from Task 1
- Produces: git-deployed apps preserve their environment setting

- [ ] **Step 1: Add Environment carry-over to git deploy pipeline**

In `internal/gitdeploy/deployer.go`, in the `Deploy` method, add environment carry-over in the existing app config block (around line 84-92):

```go
if lookupErr == nil {
    cfg.Env = existingApp.Config.Env
    cfg.Domains = existingApp.Domains
    cfg.HealthCheck = existingApp.Config.HealthCheck
    cfg.Serverless = existingApp.Config.Serverless
    cfg.Environment = existingApp.Config.Environment
    if existingApp.Config.Port != 0 {
        cfg.Port = existingApp.Config.Port
    }
}
```

- [ ] **Step 2: Build and verify**

Run: `go build -o /dev/null .`
Expected: builds without error

- [ ] **Step 3: Run tests**

Run: `go test ./internal/gitdeploy/... -v -count=1`
Expected: all tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "feat(gitdeploy): preserve environment setting on git-based deployments"
```

---

## Self-Review

**1. Spec coverage:**
- `.tengiz.yaml` → `.tengiz.{env}.yaml` merge: Task 1, `LoadForEnvironment` in `config.go`
- `--env` flag: Task 2, `deployCmd.Flags().StringP("env", "e", "", ...)`
- Merge behavior (scalar override, env map additive): Task 1 merge logic in `LoadForEnvironment`
- Git deploy integration: Task 3, environment carry-over in `gitdeploy/deployer.go`
- Environment visibility: Task 2, env column in `ps` output
- No other spec requirement uncovered

**2. Placeholder scan:** No TBD, TODO, "similar to", "add appropriate error handling" found. All steps have complete code.

**3. Type consistency:** `Environment` field added to `AppConfig` in Task 1, used in Tasks 2 and 3. Same field name throughout. `LoadForEnvironment` signature consistent across all references.
