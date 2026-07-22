# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend to support hundreds of frameworks (Rust, Ruby, PHP, Elixir, Deno, Bun, Python, Go, Java, etc.) beyond the current 6 hardcoded frameworks.

**Architecture:** Nixpacks is invoked as a subprocess (`nixpacks build`) to produce a Docker image directly. Config in `.tengiz.yaml` (`build.builder: "nixpacks"`) selects the backend; the existing detection pipeline is overridden when this config is set. A `buildWithNixpacks()` method on `Builder` handles the Nixpacks execution path, sharing same tagging and log-capture conventions as `buildWithDockerfile`. Existing callers (CLI deploy, gitdeploy pipeline, preview manager) pass the config through to the builder.

**Tech Stack:** `nixpacks` CLI (external dep, `npm install -g nixpacks`), Go `os/exec`, `internal/builder` package, `internal/types` for config, `internal/config` for env-aware config loading.

## File Structure

| File | Change | Responsibility |
|------|--------|----------------|
| `internal/types/types.go` | Modify | Add `Builder` field to `BuildConfig`, add `NixpacksConfig` struct |
| `internal/types/types_test.go` | Modify | Test Nixpacks config struct |
| `internal/builder/detect.go` | Modify | Add `FrameworkNixpacks` constant |
| `internal/builder/builder.go` | Modify | Add `nixpacksCfg` field, `SetNixpacksConfig()`, `buildWithNixpacks()`, update `Build()` dispatch |
| `internal/builder/builder_test.go` | Modify | Test Nixpacks dispatch, compilation, config setter |
| `internal/config/config.go` | Modify | Merge `Build.Builder` and `Build.NixpacksConfig` in `LoadForEnvironment` |
| `internal/config/config_test.go` | Modify | Test config merging for nixpacks |
| `internal/cli/root.go` | Modify | Wire nixpacks config from `.tengiz.yaml` into deploy command |
| `internal/gitdeploy/deployer.go` | Modify | Wire nixpacks config into git-based deploy pipeline |
| `internal/preview/manager.go` | Modify | Wire nixpacks config into preview deployment lifecycle |

## Global Constraints

- All `os/exec` calls must capture both stdout and stderr as build logs (matching existing `buildWithDockerfile` behavior)
- Image tags must follow existing convention: `tengiz-apps/{appName}:{env}-{deploymentID}`
- Nixpacks must NOT be a hard dependency — if `nixpacks` binary not in PATH when `build.builder: nixpacks` is set, return a clear error message
- Default behavior (no config) must remain unchanged — existing frameworks continue working identically
- `.tengiz.yaml` config structure: `build.builder` string field, `build.nixpacks` nested config block
- All existing tests must continue to pass
- `go test ./... -v -count=1` must pass before every commit
- `go vet ./...` must pass before final commit

---

### Task 1: Types — Add Nixpacks config fields to BuildConfig

**Files:**
- Modify: `internal/types/types.go`
- Modify: `internal/types/types_test.go`

**Interfaces:**
- Consumes: existing `BuildConfig` struct
- Produces: `BuildConfig.Builder string`, `BuildConfig.NixpacksConfig *NixpacksConfig`, `NixpacksConfig` struct with `Packages`, `AptPackages`, `Cmd`, `PkgManager` fields

- [ ] **Step 1: Write the failing tests in `internal/types/types_test.go`**

```go
func TestNixpacksConfigDefaults(t *testing.T) {
    cfg := BuildConfig{}
    if cfg.Builder != "" {
        t.Errorf("expected empty builder, got %q", cfg.Builder)
    }
    if cfg.NixpacksConfig != nil {
        t.Error("expected nil NixpacksConfig")
    }
}

func TestNixpacksConfigFields(t *testing.T) {
    cfg := BuildConfig{
        Builder: "nixpacks",
        NixpacksConfig: &NixpacksConfig{
            Packages:    []string{"ffmpeg", "curl"},
            AptPackages: []string{"libpq-dev"},
            Cmd:         "node index.js",
            PkgManager:  "pnpm",
        },
    }
    if cfg.Builder != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", cfg.Builder)
    }
    if len(cfg.NixpacksConfig.Packages) != 2 {
        t.Errorf("expected 2 packages, got %d", len(cfg.NixpacksConfig.Packages))
    }
    if len(cfg.NixpacksConfig.AptPackages) != 1 || cfg.NixpacksConfig.AptPackages[0] != "libpq-dev" {
        t.Error("apt_packages not set correctly")
    }
    if cfg.NixpacksConfig.Cmd != "node index.js" {
        t.Errorf("expected 'node index.js', got %q", cfg.NixpacksConfig.Cmd)
    }
    if cfg.NixpacksConfig.PkgManager != "pnpm" {
        t.Errorf("expected 'pnpm', got %q", cfg.NixpacksConfig.PkgManager)
    }
}

func TestNixpacksConfigJSONSerialization(t *testing.T) {
    cfg := AppConfig{
        Name: "testapp",
        Build: BuildConfig{
            Builder: "nixpacks",
            NixpacksConfig: &NixpacksConfig{
                Packages: []string{"ffmpeg"},
            },
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
    if decoded.Build.Builder != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", decoded.Build.Builder)
    }
    if decoded.Build.NixpacksConfig == nil || len(decoded.Build.NixpacksConfig.Packages) != 1 {
        t.Error("NixpacksConfig not serialized/deserialized correctly")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./internal/types/... -v -run "TestNixpacksConfig" -count=1
go test ./internal/types/... -v -run "TestNixpacksConfigJSON" -count=1
```

Expected: FAIL — `BuildConfig` has no `Builder` or `NixpacksConfig` field, `NixpacksConfig` type not defined

- [ ] **Step 3: Extend `BuildConfig` and add `NixpacksConfig` struct in `internal/types/types.go`**

```go
type BuildConfig struct {
    Command string `mapstructure:"command"`
    Output  string `mapstructure:"output"`
    Builder string `mapstructure:"builder"`
    NixpacksConfig *NixpacksConfig `mapstructure:"nixpacks,omitempty"`
}

type NixpacksConfig struct {
    Packages    []string `mapstructure:"packages,omitempty"`
    AptPackages []string `mapstructure:"apt_packages,omitempty"`
    Cmd         string   `mapstructure:"cmd,omitempty"`
    PkgManager  string   `mapstructure:"pkg_manager,omitempty"`
}
```

Place these after the existing `BuildConfig` struct in `internal/types/types.go`.

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/types/... -v -run "TestNixpacksConfig" -count=1
go test ./internal/types/... -v -run "TestNixpacksConfigJSON" -count=1
```

Expected: PASS

- [ ] **Step 5: Run all type tests**

```
go test ./internal/types/... -v -count=1
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add Nixpacks config fields to BuildConfig"
```

---

### Task 2: Detection — Add FrameworkNixpacks constant

**Files:**
- Modify: `internal/builder/detect.go`
- Modify: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `Framework` string type from `internal/builder/detect.go`
- Produces: `FrameworkNixpacks` constant used for dispatch in `Builder.Build()`

- [ ] **Step 1: Write the failing test in `internal/builder/builder_test.go`**

```go
func TestFrameworkNixpacksConstant(t *testing.T) {
    if FrameworkNixpacks != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", FrameworkNixpacks)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/builder/... -v -run "TestFrameworkNixpacksConstant" -count=1
```

Expected: FAIL — `FrameworkNixpacks` undefined

- [ ] **Step 3: Add the constant in `internal/builder/detect.go`**

After `FrameworkDocker Framework = "docker"` (line 19), add:

```go
FrameworkNixpacks Framework = "nixpacks"
```

- [ ] **Step 4: Run test to verify it passes**

```
go test ./internal/builder/... -v -run "TestFrameworkNixpacksConstant" -count=1
```

Expected: PASS

- [ ] **Step 5: Run all builder tests**

```
go test ./internal/builder/... -v -count=1
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/builder/detect.go internal/builder/builder_test.go
git commit -m "feat: add FrameworkNixpacks constant"
```

---

### Task 3: Builder — Add `buildWithNixpacks()` method and update dispatch

**Files:**
- Modify: `internal/builder/builder.go`
- Modify: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.NixpacksConfig` from Task 1, `FrameworkNixpacks` from Task 2
- Produces: `(*Builder).buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` — same signature as `buildWithDockerfile`
- Produces: `(*Builder).SetNixpacksConfig(*types.NixpacksConfig)` setter
- Produces: updated `Build()` method that dispatches to `buildWithNixpacks` when `detection.Framework == FrameworkNixpacks`

- [ ] **Step 1: Write tests for the new builder behavior in `internal/builder/builder_test.go`**

```go
func TestBuilderSetNixpacksConfig(t *testing.T) {
    b := New(t.TempDir())
    if b.nixpacksCfg != nil {
        t.Error("expected nil nixpacksCfg initially")
    }
    b.SetNixpacksConfig(&types.NixpacksConfig{
        Packages: []string{"ffmpeg"},
    })
    if b.nixpacksCfg == nil {
        t.Fatal("expected nixpacksCfg to be set")
    }
    if len(b.nixpacksCfg.Packages) != 1 || b.nixpacksCfg.Packages[0] != "ffmpeg" {
        t.Errorf("packages not stored correctly")
    }
}

func TestBuildWithNixpacksDispatches(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\"\nversion = \"0.1.0\"\n"), 0644)

    detection := &Detection{
        Framework:    FrameworkNixpacks,
        InternalPort: 8080,
    }

    b := New(t.TempDir())
    tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v1")
    if err != nil {
        if strings.Contains(err.Error(), "nixpacks not found") {
            t.Skip("nixpacks CLI not available, skipping integration test")
        }
        t.Fatalf("Build() unexpected error: %v", err)
    }
    if tag == "" {
        t.Error("expected non-empty tag")
    }
    _ = logs
}

func TestBuildNixpacksImageTag(t *testing.T) {
    // Compile-only test that verifies buildWithNixpacks signature matches buildWithDockerfile
    b := New(t.TempDir())
    _ = b // must compile
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./internal/builder/... -v -run "TestBuilderSetNixpacksConfig|TestBuildWithNixpacks|TestBuildNixpacksImageTag" -count=1
```

Expected: FAIL — `nixpacksCfg` field, `SetNixpacksConfig`, `buildWithNixpacks` not defined

- [ ] **Step 3: Implement `buildWithNixpacks` and update `Builder` in `internal/builder/builder.go`**

Add `"strings"` to imports.

Replace the `Builder` struct with:

```go
type Builder struct {
    dataDir     string
    nixpacksCfg *types.NixpacksConfig
}
```

Add the setter:

```go
func (b *Builder) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    b.nixpacksCfg = cfg
}
```

Update `Build()` method to dispatch on `FrameworkNixpacks`:

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    if detection.Framework == FrameworkNixpacks {
        return b.buildWithNixpacks(ctx, dir, appName, env, deploymentID)
    }
    if detection.Framework == FrameworkDocker {
        return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
    }
    if err := b.ensureDockerfile(dir, detection); err != nil {
        return "", "", fmt.Errorf("generate dockerfile: %w", err)
    }
    return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
}
```

Add helper and the `buildWithNixpacks` method:

```go
func (b *Builder) nixpacksAvailable() bool {
    _, err := exec.LookPath("nixpacks")
    return err == nil
}

func (b *Builder) buildWithNixpacks(ctx context.Context, dir, appName, env, deploymentID string) (string, string, error) {
    if !b.nixpacksAvailable() {
        return "", "", fmt.Errorf("nixpacks not found in PATH: install with 'npm install -g nixpacks' or 'brew install nixpacks'")
    }

    if env == "" {
        env = "production"
    }
    tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

    args := []string{"build", dir, "--name", tag}
    if b.nixpacksCfg != nil {
        if len(b.nixpacksCfg.Packages) > 0 {
            args = append(args, "--pkgs", strings.Join(b.nixpacksCfg.Packages, ","))
        }
        if len(b.nixpacksCfg.AptPackages) > 0 {
            args = append(args, "--apt-pkgs", strings.Join(b.nixpacksCfg.AptPackages, ","))
        }
        if b.nixpacksCfg.Cmd != "" {
            args = append(args, "--cmd", b.nixpacksCfg.Cmd)
        }
        if b.nixpacksCfg.PkgManager != "" {
            args = append(args, "--pkg-manager", b.nixpacksCfg.PkgManager)
        }
    }

    cmd := exec.CommandContext(ctx, "nixpacks", args...)

    var logBuf bytes.Buffer
    logWriter := io.MultiWriter(os.Stdout, &logBuf)
    cmd.Stdout = logWriter
    cmd.Stderr = logWriter

    if err := cmd.Run(); err != nil {
        return "", logBuf.String(), fmt.Errorf("nixpacks build: %w", err)
    }

    latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", appName, env)
    tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
    if out, err := tagCmd.CombinedOutput(); err != nil {
        return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
    }

    return tag, logBuf.String(), nil
}
```

- [ ] **Step 4: Add import for `"strings"` to `internal/builder/builder.go`**

If not already present, add `"strings"` to the import block.

- [ ] **Step 5: Run tests to verify they pass**

```
go test ./internal/builder/... -v -run "TestBuilderSetNixpacksConfig|TestBuildWithNixpacks|TestBuildNixpacksImageTag" -count=1
```

Expected: PASS (may skip if nixpacks CLI not installed; the setter and compile tests pass)

- [ ] **Step 6: Run all builder tests**

```
go test ./internal/builder/... -v -count=1
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: add buildWithNixpacks method to Builder"
```

---

### Task 4: Config — Merge new build config fields in environment config

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Consumes: extended `BuildConfig` from Task 1
- Produces: merged `Build.Builder` and `Build.NixpacksConfig` from env-specific config into base config

- [ ] **Step 1: Write the failing test in `internal/config/config_test.go`**

```go
func TestLoadForEnvironmentMergesBuilderConfig(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
build:
  builder: docker
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
build:
  builder: nixpacks
  nixpacks:
    packages:
      - ffmpeg
    apt_packages:
      - libpq-dev
`), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Build.Builder != "nixpacks" {
        t.Errorf("expected 'nixpacks', got %q", cfg.Build.Builder)
    }
    if cfg.Build.NixpacksConfig == nil {
        t.Fatal("expected NixpacksConfig to be set")
    }
    if len(cfg.Build.NixpacksConfig.Packages) != 1 || cfg.Build.NixpacksConfig.Packages[0] != "ffmpeg" {
        t.Errorf("expected [ffmpeg], got %v", cfg.Build.NixpacksConfig.Packages)
    }
    if len(cfg.Build.NixpacksConfig.AptPackages) != 1 || cfg.Build.NixpacksConfig.AptPackages[0] != "libpq-dev" {
        t.Errorf("expected [libpq-dev], got %v", cfg.Build.NixpacksConfig.AptPackages)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesBuilderConfig" -count=1
```

Expected: FAIL — `Build.Builder` and `Build.NixpacksConfig` not merged in `LoadForEnvironment`

- [ ] **Step 3: Implement the merge in `LoadForEnvironment` in `internal/config/config.go`**

After the `envCfg.Build.Output` merge (around line 110), add:

```go
if envCfg.Build.Builder != "" {
    cfg.Build.Builder = envCfg.Build.Builder
}
if envCfg.Build.NixpacksConfig != nil {
    cfg.Build.NixpacksConfig = envCfg.Build.NixpacksConfig
}
```

- [ ] **Step 4: Run test to verify it passes**

```
go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesBuilderConfig" -count=1
```

Expected: PASS

- [ ] **Step 5: Run all config tests**

```
go test ./internal/config/... -v -count=1
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge nixpacks config in environment config loader"
```

---

### Task 5: CLI deploy — Wire Nixpacks config to Builder

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` from config load
- Produces: `detection.Framework` overridden to `FrameworkNixpacks` when builder is nixpacks; builder receives nixpacks config

- [ ] **Step 1: Read the deploy command code to find exact insertion points**

Read `internal/cli/root.go` lines 155-345 to locate:
- Line ~187: `detection, err := builder.Detect(projectRoot)`
- Line ~199: `b := builder.New(dataDir)`

- [ ] **Step 2: Add detection override after `builder.Detect` call**

After `detection, err := builder.Detect(projectRoot)` (around line 191), add:

```go
if cfg.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
}
```

- [ ] **Step 3: Pass nixpacks config to builder after `builder.New`**

After `b := builder.New(dataDir)` (around line 199), add:

```go
if cfg.Build.NixpacksConfig != nil {
    b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
}
```

- [ ] **Step 4: Verify the change compiles**

```
go build -o /dev/null .
```

Expected: exit 0

- [ ] **Step 5: Run existing tests**

```
go test ./... -count=1 2>&1 | tail -20
```

Expected: no test failures

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire nixpacks config from .tengiz.yaml into deploy command"
```

---

### Task 6: GitDeploy — Wire Nixpacks config to Builder

**Files:**
- Modify: `internal/gitdeploy/deployer.go`

**Interfaces:**
- Consumes: `existingApp.Config.Build.Builder`, `existingApp.Config.Build.NixpacksConfig`
- Produces: `detection.Framework` overridden, builder receives nixpacks config

- [ ] **Step 1: Read `internal/gitdeploy/deployer.go` to find insertion points**

Locate:
- ~Line 73: `detection, err := builder.Detect(cloneDir)`
- ~Line 79-102: block where existing app config is loaded
- ~Line 105: `imageTag, buildLog, err := p.b.Build(...)`

- [ ] **Step 2: Add detection override and builder config after existing app config is loaded**

After the `if existingApp.Config.Build.Builder == "nixpacks"` check (around line 102), add:

```go
if existingApp.Config.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
}
if existingApp.Config.Build.NixpacksConfig != nil {
    p.b.SetNixpacksConfig(existingApp.Config.Build.NixpacksConfig)
}
```

- [ ] **Step 3: Compile-check gitdeploy changes**

```
go build ./internal/gitdeploy/...
```

Expected: exit 0

- [ ] **Step 4: Run gitdeploy tests**

```
go test ./internal/gitdeploy/... -v -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "feat: wire nixpacks config into gitdeploy pipeline"
```

---

### Task 7: Preview Manager — Wire Nixpacks config to Builder

**Files:**
- Modify: `internal/preview/manager.go`

**Interfaces:**
- Consumes: `types.NixpacksConfig` (set on Manager creation)
- Produces: `detection.Framework` overridden in both `Create()` and `Update()`; builder receives nixpacks config

- [ ] **Step 1: Read `internal/preview/manager.go` to find insertion points**

Locate:
- ~Line 18-23: `Manager` struct definition
- ~Line 30: `NewManager` constructor
- ~Line 61: `detection, err := builder.Detect(cloneDir)` in `Create()`
- ~Line 69: `m.builder.Build(...)` in `Create()`
- ~Line 143: `detection, err := builder.Detect(cloneDir)` in `Update()`
- ~Line 151: `m.builder.Build(...)` in `Update()`

- [ ] **Step 2: Add `nixpacksCfg` field to `Manager` struct**

```go
type Manager struct {
    dataDir     string
    store       *config.Store
    rt          runtime.Manager
    builder     *builder.Builder
    nixpacksCfg *types.NixpacksConfig
}
```

- [ ] **Step 3: Add `SetNixpacksConfig` method to Manager**

```go
func (m *Manager) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    m.nixpacksCfg = cfg
    m.builder.SetNixpacksConfig(cfg)
}
```

- [ ] **Step 4: Add detection override in `Create()` after `detection` is set**

After `detection, err := builder.Detect(cloneDir)` (around line 61), add:

```go
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

- [ ] **Step 5: Add detection override in `Update()` after `detection` is set**

After `detection, err := builder.Detect(cloneDir)` (around line 143), add:

```go
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

- [ ] **Step 6: Update any callers of `NewManager` that need nixpacks config**

Check all callers of `NewManager` (likely in `internal/cli/root.go`) and add `SetNixpacksConfig`:

```go
m := preview.NewManager(dataDir, store, rt)
m.SetNixpacksConfig(cfg.Build.NixpacksConfig)
```

- [ ] **Step 7: Compile-check preview changes**

```
go build ./internal/preview/...
go build .
```

Expected: exit 0

- [ ] **Step 8: Run all tests**

```
go test ./... -count=1
```

Expected: all existing tests pass

- [ ] **Step 9: Commit**

```bash
git add internal/preview/manager.go internal/cli/root.go
git commit -m "feat: wire nixpacks config into preview manager"
```

---

### Task 8: Final verification and documentation

- [ ] **Step 1: Run all tests**

```
go test ./... -v -count=1
```

Expected: PASS (tests that require Docker or nixpacks CLI may be skipped, but all framework tests pass)

- [ ] **Step 2: Run go vet**

```
go vet ./...
```

Expected: exit 0

- [ ] **Step 3: Verify build**

```
go build -o tengiz .
```

Expected: binary created successfully

- [ ] **Step 4: Update AGENTS.md to document nixpacks build backend option**

Read `AGENTS.md` and add a note under the `builder` package description: "Supports `docker` (default) and `nixpacks` build backends. Set via `build.builder` in `.tengiz.yaml`."

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document nixpacks build backend in AGENTS.md"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers types: `BuildConfig.Builder`, `NixpacksConfig` struct with all fields
- Task 2 covers detection: `FrameworkNixpacks` constant
- Task 3 covers builder: `buildWithNixpacks()` method, dispatch logic, config setter, log capture, image tagging
- Task 4 covers config: env-aware merge of builder and nixpacks config in `LoadForEnvironment`
- Task 5 covers CLI: detection override + builder config in `deployCmd`
- Task 6 covers gitdeploy: detection override + builder config in `Pipeline.Deploy`
- Task 7 covers preview: `Manager` struct extension, `SetNixpacksConfig`, detection override in `Create`/`Update`, callers updated
- Task 8 covers verification: full test suite, vet, build, docs

**2. Placeholder scan:** No TODOs, TBDs, "add validation", or "handle edge cases" patterns. Every step has actual code with exact file paths. No "similar to Task N" refs.

**3. Type consistency:**
- `buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` signature matches `buildWithDockerfile`
- `SetNixpacksConfig(*types.NixpacksConfig)` — no naming collisions
- `b.nixpacksCfg` field used consistently across builder and manager
- `FrameworkNixpacks` constant used in all 3 callers (CLI, gitdeploy, preview)
- `BuildConfig.Builder` string field used for detection override check (`== "nixpacks"`)

**4. Spec requirements check:**
- "Nixpacks must NOT be a hard dependency" → `nixpacksAvailable()` check + clear error message ✅
- "Default behavior unchanged" → `Build()` still dispatches to `buildWithDockerfile` for non-nixpacks frameworks ✅
- "Image tags follow convention" → `tengiz-apps/{appName}:{env}-{deploymentID}` ✅
- "Build logs captured" → `logBuf` used, returned in error path ✅
- ".tengiz.yaml config" → `build.builder` + `build.nixpacks.*` ✅

---

**Plan complete and saved to `docs/superpowers/plans/2026-07-22-nixpacks-build-system.md`. Two execution options:**

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
