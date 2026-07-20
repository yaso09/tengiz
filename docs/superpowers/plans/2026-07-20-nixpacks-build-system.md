# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend so Tengiz supports hundreds of frameworks (Rust, Ruby, PHP, Elixir, Deno, Bun, etc.) beyond the current 6.

**Architecture:** Nixpacks is invoked as a subprocess (`nixpacks build`) to produce a Docker image directly. Config in `.tengiz.yaml` (`build.builder: "nixpacks"`) selects the backend. A new `buildWithNixpacks()` method on `Builder` handles the Nixpacks execution path. Detection returns `FrameworkNixpacks` when config says so, overriding the default detect result. Existing callers (CLI deploy, gitdeploy pipeline, preview manager) are minimally modified to pass the config through.

**Tech Stack:** `nixpacks` CLI (external dependency, `npm install -g nixpacks` or `brew install nixpacks`), Go `os/exec`, existing `internal/builder` package.

## Global Constraints

- All `os/exec` calls must capture both stdout and stderr as build logs (matching existing `buildWithDockerfile` behavior at `internal/builder/builder.go:47-50`)
- Image tags must follow existing convention: `tengiz-apps/{appName}:{env}-{deploymentID}` (see `internal/builder/builder.go:44`)
- Nixpacks must NOT be a hard dependency — if `nixpacks` binary is not in PATH when `build.builder: nixpacks` is set, return a clear error message
- Default behavior (no config) must remain unchanged — existing frameworks continue working identically
- `.tengiz.yaml` config structure: `build.builder` string field, `build.nixpacks` nested config block
- All existing tests must continue to pass
- No new Go dependencies — use only `os/exec` and existing stdlib

---

### Task 1: Types — Add Nixpacks config fields

**Files:**
- Modify: `internal/types/types.go:42-45`

**Interfaces:**
- Consumes: existing `BuildConfig` struct at `internal/types/types.go:42-45`
- Produces: extended `BuildConfig` with `Builder` and `NixpacksConfig` fields

- [ ] **Step 1: Write the failing test**

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
            Packages: []string{"ffmpeg"},
        },
    }
    if cfg.Builder != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", cfg.Builder)
    }
    if len(cfg.NixpacksConfig.Packages) != 1 || cfg.NixpacksConfig.Packages[0] != "ffmpeg" {
        t.Error("packages not set correctly")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestNixpacksConfig" -count=1`
Expected: FAIL — `BuildConfig` has no `Builder` or `NixpacksConfig` fields, `NixpacksConfig` type not defined

- [ ] **Step 3: Extend `BuildConfig` and add `NixpacksConfig` type**

In `internal/types/types.go`, replace the `BuildConfig` struct (lines 42-45) and add the new type after it:

```go
type BuildConfig struct {
    Command        string           `mapstructure:"command"`
    Output         string           `mapstructure:"output"`
    Builder        string           `mapstructure:"builder"`
    NixpacksConfig *NixpacksConfig  `mapstructure:"nixpacks,omitempty"`
}

type NixpacksConfig struct {
    Packages     []string `mapstructure:"packages,omitempty"`
    AptPackages  []string `mapstructure:"apt_packages,omitempty"`
    Cmd          string   `mapstructure:"cmd,omitempty"`
    PkgManager   string   `mapstructure:"pkg_manager,omitempty"`
    AppDirectory string   `mapstructure:"app_directory,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestNixpacksConfig" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go
git commit -m "feat: add Nixpacks config fields to BuildConfig"
```

---

### Task 2: Detection — Add FrameworkNixpacks constant

**Files:**
- Modify: `internal/builder/detect.go:12-20`

**Interfaces:**
- Consumes: `Framework` string type from `internal/builder/detect.go`
- Produces: `FrameworkNixpacks` constant

- [ ] **Step 1: Write the failing test**

In `internal/builder/builder_test.go`:

```go
func TestFrameworkNixpacksConstant(t *testing.T) {
    if FrameworkNixpacks != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", FrameworkNixpacks)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run "TestFrameworkNixpacksConstant" -count=1`
Expected: FAIL — `FrameworkNixpacks` undefined

- [ ] **Step 3: Add the constant**

In `internal/builder/detect.go`, add to the const block after `FrameworkDocker` (line 19):

```go
FrameworkNixpacks Framework = "nixpacks"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/... -v -run "TestFrameworkNixpacksConstant" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/builder/detect.go internal/builder/builder_test.go
git commit -m "feat: add FrameworkNixpacks constant"
```

---

### Task 3: Builder — Add `buildWithNixpacks()` method and dispatch logic

**Files:**
- Modify: `internal/builder/builder.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.NixpacksConfig` from Task 1, `FrameworkNixpacks` from Task 2
- Produces: `(*Builder).buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` — same signature as `buildWithDockerfile`
- Produces: `(*Builder).SetNixpacksConfig(*types.NixpacksConfig)` setter

- [ ] **Step 1: Write the test for `buildWithNixpacks` dispatch logic**

In `internal/builder/builder_test.go`:

```go
func TestBuildWithNixpacksDispatches(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "main.rs"), []byte("fn main() {}"), 0644)

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

func TestBuildWithNixpacksDetectionFallback(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\"\n"), 0644)

    detection, err := Detect(dir)
    if err != nil {
        t.Fatal(err)
    }
    if detection.Framework == FrameworkNixpacks {
        t.Skip("nixpacks detected, skipping")
    }
}

func TestBuildWithNixpacksConfigSetter(t *testing.T) {
    b := New(t.TempDir())
    b.SetNixpacksConfig(&types.NixpacksConfig{
        Packages: []string{"curl"},
    })
    if b.nixpacksCfg == nil {
        t.Error("expected nixpacksCfg to be set")
    }
    if len(b.nixpacksCfg.Packages) != 1 {
        t.Error("expected 1 package")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run "TestBuildWithNixpacks" -count=1`
Expected: FAIL — `buildWithNixpacks` method not defined, `nixpacksCfg` field missing, `SetNixpacksConfig` missing

- [ ] **Step 3: Implement `buildWithNixpacks` and update `Builder`**

In `internal/builder/builder.go`:

Add `"strings"` to the imports. Then add `exec.LookPath` import (already have `"os/exec"`).

Update `Builder` struct:
```go
type Builder struct {
    dataDir     string
    nixpacksCfg *types.NixpacksConfig
}
```

Add setter:
```go
func (b *Builder) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    b.nixpacksCfg = cfg
}
```

Update `Build` method to dispatch on `FrameworkNixpacks`:
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

Add the helper and build method:
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/... -v -run "TestBuildWithNixpacks" -count=1`
Expected: PASS (may skip integration subtest if nixpacks CLI not installed, but the config setter and fallback tests pass)

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: add buildWithNixpacks method and dispatch to Builder"
```

---

### Task 4: Config — Merge new build config fields in environment config

**Files:**
- Modify: `internal/config/config.go:101-110`

**Interfaces:**
- Consumes: extended `BuildConfig` from Task 1
- Produces: merged `Build.Builder` and `Build.NixpacksConfig` from env-specific config

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`:

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
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesBuilderConfig" -count=1`
Expected: FAIL — `Build.Builder` and `Build.NixpacksConfig` not merged in `LoadForEnvironment`

- [ ] **Step 3: Implement the merge in `LoadForEnvironment`**

In `internal/config/config.go`, after the `Build.Output` merge (line 109), add:

```go
    if envCfg.Build.Builder != "" {
        cfg.Build.Builder = envCfg.Build.Builder
    }
    if envCfg.Build.NixpacksConfig != nil {
        cfg.Build.NixpacksConfig = envCfg.Build.NixpacksConfig
    }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesBuilderConfig" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge nixpacks config in environment config loader"
```

---

### Task 5: CLI deploy — Wire Nixpacks config from config to Builder

**Files:**
- Modify: `internal/cli/root.go:187-205`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` from config load, `detection.Framework`
- Produces: `builder.Builder` with nixpacks config set when appropriate, detection framework overridden when `build.builder: nixpacks`

- [ ] **Step 1: Understand the deploy flow insertion points**

In `internal/cli/root.go`:
- Line 187: `detection, err := builder.Detect(projectRoot)` — detection runs
- Line 199: `b := builder.New(dataDir)` — builder is created
- Line 201: `imageTag, buildLog, err := b.Build(...)` — builder is used

When `cfg.Build.Builder == "nixpacks"`:
- Override `detection.Framework` to `FrameworkNixpacks` after detection
- Pass the `NixpacksConfig` to the builder before Build is called

- [ ] **Step 2: Modify `root.go` deploy command**

After line 191 (`fmt.Printf("[tengiz] detected: %s (port %d)\n", detection.Framework, detection.InternalPort)`), add:

```go
    if cfg.Build.Builder == "nixpacks" {
        detection.Framework = builder.FrameworkNixpacks
    }
```

After line 199 (`b := builder.New(dataDir)`), add:

```go
    if cfg.Build.NixpacksConfig != nil {
        b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
    }
```

- [ ] **Step 3: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 4: Run existing tests**

Run: `go test ./... -count=1 2>&1 | head -20`
Expected: no test failures

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire nixpacks config from .tengiz.yaml into CLI deploy command"
```

---

### Task 6: GitDeploy + Preview — Wire Nixpacks config to Builder in automated pipelines

**Files:**
- Modify: `internal/gitdeploy/deployer.go:71-106`
- Modify: `internal/preview/manager.go:18-70`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` from stored app config (gitdeploy); detection-based override (preview)
- Produces: builder correctly configured for nixpacks when the app's config specifies it

- [ ] **Step 1: Modify `gitdeploy/deployer.go`**

Add imports for `"github.com/yaso09/tengiz/internal/builder"` (already present at line 11).

After line 102 (closing brace of `if lookupErr == nil { ... }` block, after existing config is loaded), add:

```go
    if lookupErr == nil {
        if existingApp.Config.Build.Builder == "nixpacks" {
            detection.Framework = builder.FrameworkNixpacks
            if existingApp.Config.Build.NixpacksConfig != nil {
                p.b.SetNixpacksConfig(existingApp.Config.Build.NixpacksConfig)
            }
        }
    }
```

- [ ] **Step 2: Compile-check gitdeploy changes**

Run: `go build ./internal/gitdeploy/...`
Expected: exit 0

- [ ] **Step 3: Modify `preview/manager.go`**

Add import for `"github.com/yaso09/tengiz/internal/types"` (already present at line 15).

Update `Manager` struct to store nixpacks config:

```go
type Manager struct {
    dataDir     string
    store       *config.Store
    rt          runtime.Manager
    builder     *builder.Builder
    nixpacksCfg *types.NixpacksConfig
}
```

Add setter method:

```go
func (m *Manager) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    m.nixpacksCfg = cfg
    m.builder.SetNixpacksConfig(cfg)
}
```

In `Create()` (line 61-64), after detection and before Build:

```go
    if m.nixpacksCfg != nil {
        detection.Framework = builder.FrameworkNixpacks
    }
```

In `Update()` (line 143-146), same insertion point after detection and before Build:

```go
    if m.nixpacksCfg != nil {
        detection.Framework = builder.FrameworkNixpacks
    }
```

- [ ] **Step 4: Compile-check preview changes**

Run: `go build ./internal/preview/...`
Expected: exit 0

- [ ] **Step 5: Run all tests**

Run: `go test ./... -count=1`
Expected: all existing tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/gitdeploy/deployer.go internal/preview/manager.go
git commit -m "feat: wire nixpacks config into gitdeploy and preview pipelines"
```

---

### Task 7: Build, vet, and final verification

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS (tests that require Docker or nixpacks CLI may be skipped, but all unit tests pass)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Verify existing frameworks still work**

Run: `go test ./internal/builder/... -v -run "TestDetect|TestGenerate|TestBuild" -count=1`
Expected: all existing detection and Dockerfile generation tests pass

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "feat: final build verification for nixpacks build system"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers `BuildConfig.Builder` and `NixpacksConfig` type definitions — matches the `.tengiz.yaml` `build.builder` and `build.nixpacks` config structure from spec
- Task 2 covers `FrameworkNixpacks` constant — enables framework-level dispatch
- Task 3 covers `buildWithNixpacks()` method, dispatch in `Build()`, `nixpacksAvailable()` check, and `SetNixpacksConfig()` setter — core implementation matching the "Nixpacks invoked as subprocess" architecture
- Task 4 covers config merge in `LoadForEnvironment` — enables per-environment builder selection (e.g., docker for production, nixpacks for staging)
- Task 5 covers CLI deploy wiring — connects `.tengiz.yaml` config to builder for manual deploys
- Task 6 covers gitdeploy and preview wiring — connects config for automated deploys (webhook-triggered and preview)
- Task 7 covers verification — build, test, vet

**2. Placeholder scan:** No "TBD", "TODO", "add validation", "handle edge cases", or "similar to Task N" patterns. Every step has actual code with exact file paths, line references, and expected test output.

**3. Type consistency:** All method signatures use the existing `(string, string, error)` return pattern matching `buildWithDockerfile`. `buildWithNixpacks` signature is identical: `func (b *Builder) buildWithNixpacks(ctx context.Context, dir, appName, env, deploymentID string) (string, string, error)`. `SetNixpacksConfig(*types.NixpacksConfig)` matches the setter pattern. `FrameworkNixpacks` constant value `"nixpacks"` is consistent across all tasks.
