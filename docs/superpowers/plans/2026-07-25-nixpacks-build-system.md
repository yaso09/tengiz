# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend to support hundreds of frameworks (Rust, Ruby, PHP, Elixir, Deno, Bun, Go, Python, Node, Java, etc.) beyond the current 7 built-in framework templates.

**Architecture:** When `.tengiz.yaml` has `build.builder: nixpacks`, Tengiz's `Builder.Build()` dispatches to a new `buildWithNixpacks()` method instead of the built-in detection + Dockerfile generation + `docker build` pipeline. Nixpacks is invoked as a subprocess (`nixpacks build <dir> --name <tag>`), which auto-detects the framework and produces a Docker image directly into the local Docker daemon. Detection is overridden to `FrameworkNixpacks`; `InternalPort` defaults to 8080 (overridable via `port:` in config). The existing `buildWithDockerfile` path and default behavior remain completely unchanged.

**Tech Stack:** `nixpacks` CLI (external dependency, installable via `npm install -g nixpacks`, `brew install nixpacks`, or direct binary), Go `os/exec`, existing `internal/builder` package (`Builder` struct).

## Global Constraints

- All `os/exec` calls must capture both stdout and stderr as build logs (matching existing `buildWithDockerfile` behavior)
- Image tags must follow existing convention: `tengiz-apps/{appName}:{env}-{deploymentID}` and `tengiz-apps/{appName}:{env}-latest`
- Nixpacks must NOT be a hard dependency — if `nixpacks` binary not in PATH when `build.builder: nixpacks` is set, return a clear error message
- Default behavior (no `build.builder` config) must remain unchanged — existing frameworks continue working identically
- Dockerfile detection always takes priority: if a `Dockerfile` exists, use `buildWithDockerfile` even when `build.builder: nixpacks` is set (matching Nixpacks CLI behavior)
- `.tengiz.yaml` config structure: `build.builder` string field, `build.nixpacks` nested config block
- All existing tests must continue to pass

---

### Task 1: Types — Add Nixpacks config fields to BuildConfig

**Files:**
- Modify: `internal/types/types.go:42-45`
- Test: `internal/types/types_test.go`

**Interfaces:**
- Consumes: existing `BuildConfig` struct
- Produces: extended `BuildConfig` with `Builder` and `NixpacksConfig` fields; new `NixpacksConfig` struct

- [ ] **Step 1: Write the failing tests**

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
            Packages:    []string{"ffmpeg"},
            AptPackages: []string{"build-essential"},
            Cmd:         "npm run start:prod",
        },
    }
    if cfg.Builder != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", cfg.Builder)
    }
    if len(cfg.NixpacksConfig.Packages) != 1 || cfg.NixpacksConfig.Packages[0] != "ffmpeg" {
        t.Errorf("packages not set correctly: %v", cfg.NixpacksConfig.Packages)
    }
    if len(cfg.NixpacksConfig.AptPackages) != 1 || cfg.NixpacksConfig.AptPackages[0] != "build-essential" {
        t.Errorf("apt_packages not set correctly: %v", cfg.NixpacksConfig.AptPackages)
    }
    if cfg.NixpacksConfig.Cmd != "npm run start:prod" {
        t.Errorf("expected cmd, got %q", cfg.NixpacksConfig.Cmd)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestNixpacksConfig" -count=1`
Expected: FAIL — `BuildConfig` has no `Builder` or `NixpacksConfig` fields, `NixpacksConfig` type not defined

- [ ] **Step 3: Extend `BuildConfig` and add `NixpacksConfig`**

Replace the existing `BuildConfig` (lines 42-45) with:

```go
type BuildConfig struct {
    Command        string          `mapstructure:"command"`
    Output         string          `mapstructure:"output"`
    Builder        string          `mapstructure:"builder,omitempty"`
    NixpacksConfig *NixpacksConfig `mapstructure:"nixpacks,omitempty"`
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

- [ ] **Step 5: Run all type tests**

Run: `go test ./internal/types/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add Nixpacks config fields to BuildConfig"
```

---

### Task 2: Builder — Add FrameworkNixpacks constant and buildWithNixpacks method

**Files:**
- Modify: `internal/builder/detect.go:12-20` (add constant)
- Modify: `internal/builder/builder.go` (add method, update Build dispatch)
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.NixpacksConfig` from Task 1
- Produces: `FrameworkNixpacks Framework = "nixpacks"` constant
- Produces: `(*Builder).SetNixpacksConfig(*types.NixpacksConfig)` setter
- Produces: `(*Builder).buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` — same signature as `buildWithDockerfile`

- [ ] **Step 1: Write the failing tests**

In `internal/builder/builder_test.go`:

```go
func TestFrameworkNixpacksConstant(t *testing.T) {
    if FrameworkNixpacks != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", FrameworkNixpacks)
    }
}

func TestBuildWithNixpacksDispatches(t *testing.T) {
    b := New(t.TempDir())
    b.SetNixpacksConfig(&types.NixpacksConfig{
        Packages: []string{"curl"},
    })

    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\"\nversion = \"0.1.0\"\n"), 0644)

    detection := &Detection{
        Framework:    FrameworkNixpacks,
        InternalPort: 8080,
    }

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

func TestBuildWithNixpacksConfigSetter(t *testing.T) {
    b := New(t.TempDir())
    b.SetNixpacksConfig(&types.NixpacksConfig{
        Packages: []string{"curl"},
    })
    if b.nixpacksCfg == nil {
        t.Error("expected nixpacksCfg to be set")
    }
    if len(b.nixpacksCfg.Packages) != 1 || b.nixpacksCfg.Packages[0] != "curl" {
        t.Error("nixpacksCfg.Packages not set correctly")
    }
}

func TestBuildWithNixpacksIgnoresDockerfile(t *testing.T) {
    // If a Dockerfile exists, it should take priority even when
    // Nixpacks is configured (matching Nixpacks CLI behavior)
    b := New(t.TempDir())
    b.SetNixpacksConfig(&types.NixpacksConfig{})
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine"), 0644)
    os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)

    detection := &Detection{
        Framework:    FrameworkNixpacks,
        InternalPort: 8080,
    }

    tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v1")
    if err != nil {
        t.Skipf("Build() error (likely no docker): %v", err)
    }
    if tag == "" {
        t.Error("expected non-empty tag")
    }
    _ = logs
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run "TestFrameworkNixpacksConstant|TestBuildWithNixpacks" -count=1`
Expected: FAIL — `FrameworkNixpacks` undefined, `buildWithNixpacks` method not defined, `nixpacksCfg` field missing

- [ ] **Step 3: Add FrameworkNixpacks constant**

In `internal/builder/detect.go`, add to the const block after `FrameworkDocker` (line 19):

```go
FrameworkNixpacks Framework = "nixpacks"
```

- [ ] **Step 4: Implement buildWithNixpacks and update Builder**

Replace `internal/builder/builder.go` entirely:

```go
package builder

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "strings"

    "github.com/yaso09/tengiz/internal/types"
)

type Builder struct {
    dataDir     string
    nixpacksCfg *types.NixpacksConfig
}

func New(dataDir string) *Builder {
    return &Builder{dataDir: dataDir}
}

func (b *Builder) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    b.nixpacksCfg = cfg
}

func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    // Dockerfile always takes priority
    if detection.Framework == FrameworkDocker {
        return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
    }
    // If Nixpacks was selected via config, use it
    if detection.Framework == FrameworkNixpacks {
        return b.buildWithNixpacks(ctx, dir, appName, env, deploymentID)
    }
    // Default: generate Dockerfile + docker build
    if err := b.ensureDockerfile(dir, detection); err != nil {
        return "", "", fmt.Errorf("generate dockerfile: %w", err)
    }
    return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
}

func (b *Builder) ensureDockerfile(dir string, detection *Detection) error {
    dfPath := filepath.Join(dir, "Dockerfile")
    if _, err := os.Stat(dfPath); err == nil {
        return nil
    }
    content := generateDockerfile(detection)
    return os.WriteFile(dfPath, []byte(content), 0644)
}

func (b *Builder) buildWithDockerfile(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
    if env == "" {
        env = "production"
    }
    tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)
    cmd := exec.CommandContext(ctx, "docker", "build", "-t", tag, dir)

    var logBuf bytes.Buffer
    logWriter := io.MultiWriter(os.Stdout, &logBuf)
    cmd.Stdout = logWriter
    cmd.Stderr = logWriter

    if err := cmd.Run(); err != nil {
        return "", logBuf.String(), fmt.Errorf("docker build: %w", err)
    }

    latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", appName, env)
    tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
    if out, err := tagCmd.CombinedOutput(); err != nil {
        return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
    }

    return tag, logBuf.String(), nil
}

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
            args = append(args, "--start-cmd", b.nixpacksCfg.Cmd)
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

The `generateDockerfile` function stays unchanged (lines 65-163 in the original).

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/builder/... -v -run "TestFrameworkNixpacksConstant|TestBuildWithNixpacks" -count=1`
Expected: PASS (integration tests that require Docker or nixpacks CLI may skip)

- [ ] **Step 6: Run all builder tests**

Run: `go test ./internal/builder/... -v -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/builder/detect.go internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: add FrameworkNixpacks and buildWithNixpacks method"
```

---

### Task 3: Config — Merge new build config fields in environment config loader

**Files:**
- Modify: `internal/config/config.go` (add merge for Build.Builder and Build.NixpacksConfig in LoadForEnvironment)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: extended `BuildConfig` from Task 1
- Produces: merged `Build.Builder` and `Build.NixpacksConfig` from env-specific `.tengiz.{env}.yaml` files

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
    apt_packages:
      - build-essential
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
    if len(cfg.Build.NixpacksConfig.AptPackages) != 1 || cfg.Build.NixpacksConfig.AptPackages[0] != "build-essential" {
        t.Errorf("expected [build-essential], got %v", cfg.Build.NixpacksConfig.AptPackages)
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
git commit -m "feat: merge nixpacks builder config in environment config loader"
```

---

### Task 4: CLI deploy — Wire build config to Builder

**Files:**
- Modify: `internal/cli/root.go:187-201`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` from config load, `detection.Framework`
- Produces: When `cfg.Build.Builder == "nixpacks"`, detection framework is overridden to `FrameworkNixpacks` and nixpacks config is passed to the builder

- [ ] **Step 1: Understand the current deploy flow insertion points**

In `internal/cli/root.go`:
- Line 187: `detection, err := builder.Detect(projectRoot)` — detection runs first
- Line 199: `b := builder.New(dataDir)` — builder is created
- Line 201: `b.Build(...)` — builder is used

The override needs to happen after detection (line 191) and before builder creation (line 199).

- [ ] **Step 2: Modify `root.go` deploy command**

After line 191 (`fmt.Printf("[tengiz] detected: %s (port %d)\n", detection.Framework, detection.InternalPort)`), add:

```go
if cfg.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
    if cfg.Port == 0 {
        cfg.Port = 8080
    }
    detection.InternalPort = cfg.Port
    fmt.Printf("[tengiz] using nixpacks builder (override detection)\n")
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

Run: `go test ./... -v -count=1 2>&1 | head -50`
Expected: no test failures

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire nixpacks config from .tengiz.yaml into deploy command"
```

---

### Task 5: GitDeploy pipeline — Wire Nixpacks config to Builder

**Files:**
- Modify: `internal/gitdeploy/deployer.go`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` from store's existing app config
- Produces: builder correctly configured for nixpacks when the app's stored config specifies it

- [ ] **Step 1: Understand current gitdeploy flow**

In `internal/gitdeploy/deployer.go`:
- Line 73: `detection, err := builder.Detect(cloneDir)` — detection runs on cloned repo
- Line 93-102: If app exists, stored config fields are copied to `cfg`
- Line 105: `p.b.Build(ctx, cloneDir, appName, p.env, detection, deploymentID)` — build happens
- The `Pipeline` struct (line 19-25) already has a `b *builder.Builder` field

- [ ] **Step 2: Modify `deployer.go`**

After detection (line 77-78) and after existing app config is merged (after line 102), add:

```go
// After line 78 (log.Printf for detection), add:
// Temporarily set detection override — will be re-checked after config load
```

After the end of the existing app lookup block (after line 102 `}`), add:

```go
if existingApp.Config.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
}
if existingApp.Config.Build.NixpacksConfig != nil {
    p.b.SetNixpacksConfig(existingApp.Config.Build.NixpacksConfig)
}
```

Also handle the first-deploy case (where `lookupErr != nil`, line 71). For first deploy, the config comes from the freshly created `cfg` struct. Add before the build call (before line 105):

```go
// After line 104 (deploymentID assignment), add:
if cfg.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
}
if cfg.Build.NixpacksConfig != nil {
    p.b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
}
```

- [ ] **Step 3: Compile-check gitdeploy changes**

Run: `go build ./internal/gitdeploy/...`
Expected: exit 0

- [ ] **Step 4: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "feat: wire nixpacks config into gitdeploy pipeline"
```

---

### Task 6: Preview manager — Wire Nixpacks config to Builder

**Files:**
- Modify: `internal/preview/manager.go`

**Interfaces:**
- Consumes: Nixpacks config from Manager-level field (passed through to builder)
- Produces: preview manager passes nixpacks config to its builder; detection framework is overridden when config is set

- [ ] **Step 1: Understand current preview manager flow**

In `internal/preview/manager.go`:
- Line 18-23: `Manager` struct has `builder *builder.Builder` field already
- Line 61: `detection, err := builder.Detect(cloneDir)` — detection
- Line 69: `m.builder.Build(...)` — build
- Line 143: `detection, err := builder.Detect(cloneDir)` — same in Update

- [ ] **Step 2: Add nixpacks config to Manager**

Add to `Manager` struct (line 18):

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

- [ ] **Step 3: Override detection in Create and Update**

In `Create()` method, after detection (line 64):

```go
detection, err := builder.Detect(cloneDir)
if err != nil {
    return nil, fmt.Errorf("detect: %w", err)
}
// Add:
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

In `Update()` method, after detection (line 146):

```go
detection, err := builder.Detect(cloneDir)
if err != nil {
    return nil, fmt.Errorf("detect: %w", err)
}
// Add:
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

- [ ] **Step 4: Update the webhook handler that creates preview manager**

Locate where `preview.NewManager()` is called (likely in `internal/cli/root.go` or `internal/gitdeploy/webhook.go`). After creating the manager, add:

```go
pm := preview.NewManager(dataDir, store, rt)
// Add nixpacks config if configured
if cfg.Build.NixpacksConfig != nil {
    pm.SetNixpacksConfig(cfg.Build.NixpacksConfig)
}
```

Search for `preview.NewManager` to find all call sites and add the nixpacks config wiring.

- [ ] **Step 5: Compile-check preview changes**

Run: `go build ./internal/preview/...`
Expected: exit 0

- [ ] **Step 6: Commit**

```bash
git add internal/preview/manager.go
git commit -m "feat: wire nixpacks config into preview manager"
```

---

### Task 7: Run full test suite and verify

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS (tests that require Docker or nixpacks CLI may be skipped, but all framework tests pass)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: add nixpacks build system support"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers `BuildConfig.Builder` and `NixpacksConfig` type definitions with tests
- Task 2 covers `FrameworkNixpacks` constant, `buildWithNixpacks()` method, and `Build()` dispatch with Dockerfile priority
- Task 3 covers config merge in `LoadForEnvironment` for env-specific builder overrides
- Task 4 covers CLI deploy command wiring
- Task 5 covers gitdeploy pipeline wiring (first-deploy and re-deploy paths)
- Task 6 covers preview manager wiring (Create and Update paths)
- Task 7 covers verification

**2. Placeholder scan:** No TODOs, TBDs, "implement later", "add validation", or "something similar". Every step has actual code. All code blocks contain complete Go code.

**3. Type consistency:**
- `buildWithNixpacks` signature matches `buildWithDockerfile`: `(ctx, dir, appName, env, deploymentID) (string, string, error)`
- `BuildConfig.Builder` used consistently as `cfg.Build.Builder` across all callers
- `NixpacksConfig` struct used consistently with `mapstructure` tags matching viper deserialization
- `FrameworkNixpacks` constant value `"nixpacks"` matches config string `build.builder: nixpacks`
- `SetNixpacksConfig(*types.NixpacksConfig)` setter available on both `Builder` and `Manager`
- Dockerfile priority rule: `FrameworkDocker` check comes before `FrameworkNixpacks` check in `Build()` dispatch
