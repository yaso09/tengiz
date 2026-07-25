# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend to expand framework support from 6 to hundreds (Rust, Ruby, PHP, Java, Elixir, Deno, Bun, etc.).

**Architecture:** Nixpacks is invoked as a subprocess (`nixpacks build` CLI) to produce a Docker image directly. Config in `.tengiz.yaml` (`build.builder: "nixpacks"`) selects the backend. A new `buildWithNixpacks()` method on `Builder` handles the Nixpacks execution path. The existing `Detect()` function is unchanged — when the config selects nixpacks, the detection framework is overridden after detection. All three caller sites (CLI deploy, gitdeploy pipeline, preview manager) wire the config through to the builder.

**Tech Stack:** `nixpacks` CLI (external dep, installed separately), Go `os/exec`, existing `internal/builder` package, `internal/types` config structs.

## Global Constraints

- All `os/exec` calls must capture stdout+stderr as build logs (matching existing `buildWithDockerfile` behavior)
- Image tags must follow existing convention: `tengiz-apps/{appName}:{env}-{deploymentID}`
- Nixpacks must NOT be a hard dependency — if `nixpacks` binary not in PATH when `build.builder: nixpacks` is set, return a clear error message
- Default behavior (no config) must remain unchanged — existing 6 frameworks continue working identically
- `.tengiz.yaml` config structure: `build.builder` (string), `build.nixpacks` (nested config block with packages, apt_packages, cmd, etc.)
- All existing tests must continue to pass

---

### Task 1: Types — Add Nixpacks config fields to `BuildConfig`

**Files:**
- Modify: `internal/types/types.go:42-45`

**Interfaces:**
- Consumes: existing `BuildConfig` struct
- Produces: extended `BuildConfig` with `Builder` string field and `NixpacksConfig` pointer

- [ ] **Step 1: Write the failing test**

Add to `internal/types/types_test.go`:

```go
func TestBuildConfigNixpacksDefaults(t *testing.T) {
    var cfg BuildConfig
    if cfg.Builder != "" {
        t.Errorf("expected empty builder, got %q", cfg.Builder)
    }
    if cfg.NixpacksConfig != nil {
        t.Error("expected nil NixpacksConfig")
    }
}

func TestBuildConfigNixpacksFields(t *testing.T) {
    cfg := BuildConfig{
        Builder: "nixpacks",
        NixpacksConfig: &NixpacksConfig{
            Packages:    []string{"ffmpeg", "curl"},
            AptPackages: []string{"libssl-dev"},
            Cmd:         "node server.js",
        },
    }
    if cfg.Builder != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", cfg.Builder)
    }
    if len(cfg.NixpacksConfig.Packages) != 2 {
        t.Errorf("expected 2 packages, got %d", len(cfg.NixpacksConfig.Packages))
    }
    if cfg.NixpacksConfig.Cmd != "node server.js" {
        t.Errorf("expected 'node server.js', got %q", cfg.NixpacksConfig.Cmd)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/types/... -v -run "TestBuildConfigNixpacks" -count=1
```
Expected: FAIL — `BuildConfig` has no `Builder` or `NixpacksConfig` fields, `NixpacksConfig` type not defined

- [ ] **Step 3: Extend `BuildConfig` and add `NixpacksConfig` in `internal/types/types.go`**

Replace existing `BuildConfig` struct (lines 42-45) with:

```go
type BuildConfig struct {
    Command string `mapstructure:"command"`
    Output  string `mapstructure:"output"`
    Builder string `mapstructure:"builder"`
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

```
go test ./internal/types/... -v -run "TestBuildConfigNixpacks" -count=1
```
Expected: PASS

- [ ] **Step 5: Commit**

```
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add Nixpacks config fields to BuildConfig"
```

---

### Task 2: Builder — Add `nixpacksAvailable()` check and `buildWithNixpacks()` method

**Files:**
- Modify: `internal/builder/builder.go` (full file, 163 lines)
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.NixpacksConfig` from Task 1
- Produces: `(*Builder).buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` — same signature as `buildWithDockerfile`
- Produces: `(*Builder).SetNixpacksConfig(*types.NixpacksConfig)` setter

- [ ] **Step 1: Write the failing tests**

Add to `internal/builder/builder_test.go`:

```go
func TestNixpacksAvailableCheck(t *testing.T) {
    b := New(t.TempDir())
    // On CI without nixpacks, this should be false
    available := b.nixpacksAvailable()
    // Just verify it returns a bool without panic
    _ = available
}

func TestBuildWithNixpacksDispatches(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\"\nversion = \"0.1.0\"\n"), 0644)

    detection := &Detection{
        Framework:    FrameworkNixpacks,
        InternalPort: 8080,
    }

    b := New(t.TempDir())
    b.SetNixpacksConfig(&types.NixpacksConfig{
        Packages: []string{"curl"},
    })

    tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v1")
    if err != nil {
        if strings.Contains(err.Error(), "nixpacks not found") {
            t.Skip("nixpacks CLI not installed, skipping integration test")
        }
        t.Fatalf("Build() unexpected error: %v", err)
    }
    if tag == "" {
        t.Error("expected non-empty tag")
    }
    _ = logs
}

func TestBuildWithNixpacksRejectsMissingCLI(t *testing.T) {
    // Force nixpacks detection path
    dir := t.TempDir()
    detection := &Detection{
        Framework:    FrameworkNixpacks,
        InternalPort: 8080,
    }
    b := New(t.TempDir())
    tag, _, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v1")
    if err == nil {
        t.Skip("nixpacks CLI is installed, can't test missing CLI path")
    }
    if tag != "" {
        t.Error("expected empty tag on error")
    }
    if !strings.Contains(err.Error(), "nixpacks not found") {
        t.Errorf("expected 'nixpacks not found' error, got: %v", err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/builder/... -v -run "TestBuildWithNixpacks|TestNixpacksAvailable" -count=1
```
Expected: FAIL — `FrameworkNixpacks` constant undefined, `buildWithNixpacks` method not found, `nixpacksCfg` field and `SetNixpacksConfig` method missing

- [ ] **Step 3: Add `FrameworkNixpacks` to `internal/builder/detect.go`**

Add to the const block (after `FrameworkDocker` on line 19):

```go
FrameworkNixpacks Framework = "nixpacks"
```

- [ ] **Step 4: Implement `buildWithNixpacks` and update `Builder` in `internal/builder/builder.go`**

Replace existing file content. Key changes:

1. Add `nixpacksCfg` field to `Builder` struct
2. Add `SetNixpacksConfig` setter
3. Add `nixpacksAvailable()` check  
4. Update `Build()` to dispatch to `buildWithNixpacks` when `detection.Framework == FrameworkNixpacks`
5. Add `buildWithNixpacks()` method

Updated `Builder` struct and constructor (keep `dataDir`):

```go
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

func (b *Builder) nixpacksAvailable() bool {
    _, err := exec.LookPath("nixpacks")
    return err == nil
}
```

Updated `Build()` — insert nixpacks dispatch before the Docker check:

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

New `buildWithNixpacks()` method:

```go
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

Add `"strings"` to imports. Ensure `exec.LookPath` from `"os/exec"` is available.

- [ ] **Step 5: Run test to verify it passes**

```
go test ./internal/builder/... -v -run "TestBuildWithNixpacks|TestNixpacksAvailable" -count=1
```
Expected: PASS (may skip if nixpacks CLI not installed, but the `TestBuildWithNixpacksRejectsMissingCLI` passes because it expects the error)

- [ ] **Step 6: Run existing tests to confirm no regression**

```
go test ./internal/builder/... -v -count=1
```
Expected: all existing tests pass

- [ ] **Step 7: Commit**

```
git add internal/builder/builder.go internal/builder/detect.go internal/builder/builder_test.go
git commit -m "feat: add buildWithNixpacks method and FrameworkNixpacks constant"
```

---

### Task 3: Config — Merge `build.builder` and `build.nixpacks` in environment config loader

**Files:**
- Modify: `internal/config/config.go:100-145`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: extended `BuildConfig` from Task 1
- Produces: `cfg.Build.Builder` and `cfg.Build.NixpacksConfig` merged from env-specific `.tengiz.{env}.yaml`

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestLoadForEnvironmentOverridesBuilder(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: testapp\nbuild:\n  builder: docker\n"), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte("build:\n  builder: nixpacks\n  nixpacks:\n    packages:\n      - ffmpeg\n    cmd: node server.js\n"), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Build.Builder != "nixpacks" {
        t.Errorf("Builder = %q, want %q", cfg.Build.Builder, "nixpacks")
    }
    if cfg.Build.NixpacksConfig == nil {
        t.Fatal("NixpacksConfig is nil")
    }
    if len(cfg.Build.NixpacksConfig.Packages) != 1 || cfg.Build.NixpacksConfig.Packages[0] != "ffmpeg" {
        t.Errorf("Packages = %v, want [ffmpeg]", cfg.Build.NixpacksConfig.Packages)
    }
    if cfg.Build.NixpacksConfig.Cmd != "node server.js" {
        t.Errorf("Cmd = %q, want %q", cfg.Build.NixpacksConfig.Cmd, "node server.js")
    }
}

func TestLoadForEnvironmentPreservesBuilderDefault(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: testapp\nbuild:\n  command: npm run build\n"), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte("env:\n  NODE_ENV: staging\n"), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Build.Builder != "" {
        t.Errorf("expected empty builder, got %q", cfg.Build.Builder)
    }
    if cfg.Build.NixpacksConfig != nil {
        t.Error("expected nil NixpacksConfig")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/config/... -v -run "TestLoadForEnvironmentOverridesBuilder|TestLoadForEnvironmentPreservesBuilder" -count=1
```
Expected: FAIL — `Build.Builder` and `Build.NixpacksConfig` are not merged in `LoadForEnvironment`

- [ ] **Step 3: Add merge logic to `LoadForEnvironment` in `internal/config/config.go`**

After the existing `Build.Output` merge (line 109), add:

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
go test ./internal/config/... -v -run "TestLoadForEnvironmentOverridesBuilder|TestLoadForEnvironmentPreservesBuilder" -count=1
```
Expected: PASS

- [ ] **Step 5: Run all config tests to ensure no regression**

```
go test ./internal/config/... -v -count=1
```
Expected: PASS

- [ ] **Step 6: Commit**

```
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge nixpacks builder config in LoadForEnvironment"
```

---

### Task 4: CLI deploy — Wire nixpacks config from `.tengiz.yaml` to Builder

**Files:**
- Modify: `internal/cli/root.go:155-345`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` from config load, `detection.Framework` from `builder.Detect()`
- Produces: builder configured with nixpacks when config specifies it

- [ ] **Step 1: Understand the deploy flow in `internal/cli/root.go`**

Line 173: `cfg` loaded via `config.LoadForEnvironment(projectRoot, envFlag)`
Line 187: `detection` from `builder.Detect(projectRoot)`
Line 199: `b := builder.New(dataDir)`
Line 201: `b.Build(ctx, ..., detection, ...)` — detection framework determines build path

Strategy: after loading config but before building, if `cfg.Build.Builder == "nixpacks"`, override `detection.Framework` to `FrameworkNixpacks` and pass the nixpacks config to the builder.

- [ ] **Step 2: Modify `root.go` deploy command**

After line 191 (`fmt.Printf("[tengiz] detected: ...")`), add:

```go
if cfg.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
    fmt.Printf("[tengiz] using nixpacks builder (overriding detection)\n")
}
```

After line 199 (`b := builder.New(dataDir)`), add:

```go
if cfg.Build.NixpacksConfig != nil {
    b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
}
```

- [ ] **Step 3: Verify the change compiles**

```
go build -o /dev/null .
```
Expected: exit 0

- [ ] **Step 4: Run all tests**

```
go test ./... -v -count=1 2>&1 | head -60
```
Expected: no test failures

- [ ] **Step 5: Commit**

```
git add internal/cli/root.go
git commit -m "feat: wire nixpacks config from .tengiz.yaml into deploy command"
```

---

### Task 5: GitDeploy pipeline — Wire nixpacks config

**Files:**
- Modify: `internal/gitdeploy/deployer.go:52-229`

**Interfaces:**
- Consumes: `existingApp.Config.Build.Builder` and `existingApp.Config.Build.NixpacksConfig` from stored app
- Produces: builder configured for nixpacks when stored config specifies it

- [ ] **Step 1: Understand the gitdeploy flow**

`Pipeline.Deploy()` clones repo, detects framework, loads existing app from store (if exists), then builds. When `existingApp.Config.Build.Builder == "nixpacks"`, we need to override the detection framework and pass the nixpacks config.

- [ ] **Step 2: Modify `deployer.go`**

After the `lookupErr == nil` block (after line 102), add:

```go
if lookupErr == nil && existingApp.Config.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
    log.Printf("[tengiz] using nixpacks builder from stored config")
}

if lookupErr == nil && existingApp.Config.Build.NixpacksConfig != nil {
    p.b.SetNixpacksConfig(existingApp.Config.Build.NixpacksConfig)
}
```

- [ ] **Step 3: Verify compile**

```
go build ./internal/gitdeploy/...
```
Expected: exit 0

- [ ] **Step 4: Run tests**

```
go test ./internal/gitdeploy/... -v -count=1
```
Expected: PASS

- [ ] **Step 5: Commit**

```
git add internal/gitdeploy/deployer.go
git commit -m "feat: wire nixpacks config into gitdeploy pipeline"
```

---

### Task 6: Preview manager — Wire nixpacks config

**Files:**
- Modify: `internal/preview/manager.go:18-213`

**Interfaces:**
- Consumes: `*types.NixpacksConfig` passed into manager
- Produces: builder configured for nixpacks when config present

- [ ] **Step 1: Understand the preview flow**

`Manager.Create()` clones, detects, builds. `Manager.Update()` same pattern. The preview manager doesn't have access to the app config — it creates a minimal config. We need to pass nixpacks config through the manager.

- [ ] **Step 2: Modify `Manager` struct and constructor**

Add `nixpacksCfg` field to `Manager`:

```go
type Manager struct {
    dataDir     string
    store       *config.Store
    rt          runtime.Manager
    builder     *builder.Builder
    nixpacksCfg *types.NixpacksConfig
}
```

Add a setter:

```go
func (m *Manager) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    m.nixpacksCfg = cfg
    m.builder.SetNixpacksConfig(cfg)
}
```

- [ ] **Step 3: Add nixpacks detection override in `Create()`**

After line 64 (`detection, err := builder.Detect(cloneDir)`), add:

```go
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

- [ ] **Step 4: Add same override in `Update()`**

After line 146 (after detection), add:

```go
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

- [ ] **Step 5: Verify compile**

```
go build ./internal/preview/...
```
Expected: exit 0

- [ ] **Step 6: Run tests**

```
go test ./internal/preview/... -v -count=1
```
Expected: PASS

- [ ] **Step 7: Commit**

```
git add internal/preview/manager.go
git commit -m "feat: wire nixpacks config into preview manager"
```

---

### Task 7: Init template — Add nixpacks builder option to `.tengiz.yaml` template

**Files:**
- Modify: `internal/cli/root.go:114-140` (initCmd template)

- [ ] **Step 1: Update the init template**

In `initCmd` content string (line 114-140), after the `build:` comment line, add an optional `builder` field. Also add a nixpacks example config section:

```go
content := fmt.Sprintf(`name: %s
environment: %s
# port: 3000
# build:
#   builder: nixpacks     # use nixpacks for auto-detection (npm install -g nixpacks)
#   command: npm run build
#   output: dist
serverless:
  enabled: true
  idle_timeout: 5m
`, name, env)
```

- [ ] **Step 2: Verify build**

```
go build -o /dev/null .
```
Expected: exit 0

- [ ] **Step 3: Commit**

```
git add internal/cli/root.go
git commit -m "docs: add nixpacks builder option to init template"
```

---

### Task 8: Full verification suite

**Files:** N/A — verification only

- [ ] **Step 1: Run all tests**

```
go test ./... -v -count=1
```
Expected: PASS (tests requiring Docker or nixpacks CLI may skip, but all framework tests pass)

- [ ] **Step 2: Run vet**

```
go vet ./...
```
Expected: exit 0

- [ ] **Step 3: Build binary**

```
go build -o tengiz .
```
Expected: binary created

- [ ] **Step 4: Run build tests specifically**

```
go test ./internal/builder/... -v -count=1
```
Expected: PASS — all builder tests including the new nixpacks tests pass

- [ ] **Step 5: Commit**

```
git add -A
git commit -m "chore: final verification — all tests pass, binary builds"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1: Types — covers `BuildConfig.Builder` and `NixpacksConfig` struct
- Task 2: Builder — covers `FrameworkNixpacks`, `buildWithNixpacks()`, `nixpacksAvailable()`, `SetNixpacksConfig()`
- Task 3: Config — covers merging `build.builder` and `build.nixpacks` in `LoadForEnvironment`
- Task 4: CLI deploy — covers overriding detection framework and wiring config to builder
- Task 5: GitDeploy — covers reading stored nixpacks config and wiring to builder
- Task 6: Preview — covers passing nixpacks config through manager and overriding detection
- Task 7: Init template — updates the generated `.tengiz.yaml` to document the nixpacks option
- Task 8: Verification — full test suite, vet, build

**2. Placeholder scan:** No TODOs, TBDs, or "add validation". Every step has actual code with exact file paths, test code, and expected output.

**3. Type consistency:** `buildWithNixpacks` matches `buildWithDockerfile` signature `(ctx, dir, appName, env, deploymentID) (string, string, error)`. `SetNixpacksConfig` matches across all callers. `FrameworkNixpacks` constant is `"nixpacks"` matching the config value. `NixpacksConfig` fields use `mapstructure` tags consistent with the codebase convention.
