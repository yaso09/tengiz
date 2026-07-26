# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend so Tengiz supports hundreds of frameworks (Rust, Ruby, PHP, Elixir, Deno, Bun, etc.) beyond the current 6.

**Architecture:** When `build.builder: nixpacks` is set in `.tengiz.yaml`, the `Build()` method dispatches to a new `buildWithNixpacks()` that runs `nixpacks build` instead of `docker build`. The build produces a Docker image with the same tagging convention (`tengiz-apps/{app}:{env}-{id}`). The rest of the pipeline (container creation, proxy, idle, health) is unchanged. A new `FrameworkNixpacks` framework constant signals the dispatch. The nixpacks CLI is not a hard dependency — clean error if missing.

**Tech Stack:** `nixpacks` CLI (external, `npm install -g nixpacks` or `brew install nixpacks`), Go `os/exec`, existing `internal/builder` package.

## Global Constraints

- All `os/exec` calls must capture both stdout and stderr as build logs (matching existing `buildWithDockerfile` behavior)
- Image tags must follow existing convention: `tengiz-apps/{appName}:{env}-{deploymentID}`
- Nixpacks must NOT be a hard dependency — if `nixpacks` binary not in PATH when `build.builder: nixpacks` is set, return a clear error message
- Default behavior (no config, or `build.builder: docker`) must remain unchanged — existing frameworks continue working identically
- `.tengiz.yaml` config structure: `build.builder` string field (defaults to `"docker"`), `build.nixpacks` nested config block
- All existing tests must continue to pass

---

## File Structure

| File | Action | Purpose |
|------|--------|---------|
| `internal/types/types.go` | Modify | Add `Builder` and `NixpacksConfig` fields to `BuildConfig` |
| `internal/builder/detect.go` | Modify | Add `FrameworkNixpacks` constant |
| `internal/builder/nixpacks.go` | **Create** | `buildWithNixpacks()` implementation — nixpacks CLI invocation |
| `internal/builder/builder.go` | Modify | Add `nixpacksCfg` field, `SetNixpacksConfig()`, dispatch in `Build()`, `nixpacksAvailable()` |
| `internal/builder/builder_test.go` | Modify/Add | Tests for nixpacks dispatch, build, and config setter |
| `internal/config/config.go` | Modify | Merge `Build.Builder` and `Build.NixpacksConfig` in `LoadForEnvironment` |
| `internal/config/config_test.go` | Modify | Test for builder config merge |
| `internal/cli/root.go` | Modify | Wire config to builder in deploy command |
| `internal/gitdeploy/deployer.go` | Modify | Wire stored nixpacks config to builder in git deploy |
| `internal/preview/manager.go` | Modify | Wire nixpacks config to builder in preview CRUD |


---

### Task 1: Types — Add Nixpacks config fields

**Files:**
- Modify: `internal/types/types.go:42-45`

**Interfaces:**
- Consumes: existing `BuildConfig` struct
- Produces: extended `BuildConfig` with `Builder` and `NixpacksConfig` fields

- [ ] **Step 1: Write the failing test**

```go
// internal/types/types_test.go (create if it doesn't exist)
package types

import "testing"

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
            Cmd:      "npm run start",
        },
    }
    if cfg.Builder != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", cfg.Builder)
    }
    if len(cfg.NixpacksConfig.Packages) != 1 || cfg.NixpacksConfig.Packages[0] != "ffmpeg" {
        t.Error("packages not set correctly")
    }
    if cfg.NixpacksConfig.Cmd != "npm run start" {
        t.Errorf("expected 'npm run start', got %q", cfg.NixpacksConfig.Cmd)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestNixpacksConfig" -count=1`
Expected: FAIL — `BuildConfig` has no `Builder` or `NixpacksConfig` fields, `NixpacksConfig` type not defined

- [ ] **Step 3: Extend `BuildConfig` and add `NixpacksConfig`**

In `internal/types/types.go`, replace the `BuildConfig` struct (lines 42-45):

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
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add Nixpacks config fields to BuildConfig"
```

---

### Task 2: Builder — Add FrameworkNixpacks constant and nixpacks file

**Files:**
- Modify: `internal/builder/detect.go:12-20`
- Create: `internal/builder/nixpacks.go`

**Interfaces:**
- Consumes: `types.NixpacksConfig` from Task 1
- Produces: `FrameworkNixpacks` constant, `buildWithNixpacks()` method, `SetNixpacksConfig()` setter

- [ ] **Step 1: Write the failing tests**

In `internal/builder/builder_test.go`:

```go
func TestFrameworkNixpacksConstant(t *testing.T) {
    if FrameworkNixpacks != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", FrameworkNixpacks)
    }
}

func TestNixpacksConfigSetter(t *testing.T) {
    b := New(t.TempDir())
    cfg := &types.NixpacksConfig{
        Packages: []string{"curl"},
    }
    b.SetNixpacksConfig(cfg)
    if b.nixpacksCfg == nil {
        t.Fatal("expected nixpacksCfg to be set")
    }
    if len(b.nixpacksCfg.Packages) != 1 || b.nixpacksCfg.Packages[0] != "curl" {
        t.Error("packages not set correctly")
    }
}

func TestNixpacksAvailable(t *testing.T) {
    b := New(t.TempDir())
    // This should not panic, just return true/false
    _ = b.nixpacksAvailable()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run "TestFrameworkNixpacksConstant|TestNixpacksConfigSetter" -count=1`
Expected: FAIL — `FrameworkNixpacks` undefined, `nixpacksCfg` field missing

- [ ] **Step 3: Add the constant**

In `internal/builder/detect.go`, add to the const block after `FrameworkDocker`:

```go
FrameworkNixpacks Framework = "nixpacks"
```

- [ ] **Step 4: Update `Builder` struct and `Build()` dispatch**

In `internal/builder/builder.go`:

```go
type Builder struct {
    dataDir     string
    nixpacksCfg *types.NixpacksConfig
}

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

func (b *Builder) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    b.nixpacksCfg = cfg
}

func (b *Builder) nixpacksAvailable() bool {
    _, err := exec.LookPath("nixpacks")
    return err == nil
}
```

Add `"github.com/yaso09/tengiz/internal/types"` to the import block.

- [ ] **Step 5: Create `internal/builder/nixpacks.go`**

```go
package builder

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "os"
    "os/exec"
    "strings"
)

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
        if b.nixpacksCfg.AppDirectory != "" {
            args = append(args, "--app-dir", b.nixpacksCfg.AppDirectory)
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

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/builder/... -v -run "TestFrameworkNixpacksConstant|TestNixpacksConfigSetter" -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/builder/detect.go internal/builder/builder.go internal/builder/nixpacks.go internal/builder/builder_test.go
git commit -m "feat: add FrameworkNixpacks constant and buildWithNixpacks method"
```

---

### Task 3: Config — Merge build builder config in environment loader

**Files:**
- Modify: `internal/config/config.go`

**Interfaces:**
- Consumes: extended `BuildConfig` from Task 1
- Produces: merged `Build.Builder` and `Build.NixpacksConfig` from env-specific config

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go` (create if not exists; check if a test file already exists first):

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
    cmd: npm run start
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
    if cfg.Build.NixpacksConfig.Cmd != "npm run start" {
        t.Errorf("expected 'npm run start', got %q", cfg.Build.NixpacksConfig.Cmd)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesBuilderConfig" -count=1`
Expected: FAIL — `Build.Builder` and `Build.NixpacksConfig` not merged in `LoadForEnvironment`

- [ ] **Step 3: Read the current `LoadForEnvironment` to find the right insertion point**

```bash
grep -n "Build.Output\|envCfg.Build\|cfg.Build =" internal/config/config.go
```

- [ ] **Step 4: Add the builder config merge**

In `internal/config/config.go`, inside `LoadForEnvironment`, after the `cfg.Build.Output = envCfg.Build.Output` line (or in the sequence of `Build` scalar merges), add:

```go
if envCfg.Build.Builder != "" {
    cfg.Build.Builder = envCfg.Build.Builder
}
if envCfg.Build.NixpacksConfig != nil {
    cfg.Build.NixpacksConfig = envCfg.Build.NixpacksConfig
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesBuilderConfig" -count=1`
Expected: PASS

- [ ] **Step 6: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge nixpacks builder config in environment config loader"
```

---

### Task 4: CLI deploy — Wire build config to Builder

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` from config load, `detection.Framework`
- Produces: builder with nixpacks config set, detection overridden when `build.builder: nixpacks`

- [ ] **Step 1: Understand the current deploy flow**

Read lines 155-345 of `internal/cli/root.go`. Detection runs at line 187, build at line 201. We need to:
1. After detection (line 191), override `detection.Framework` if `cfg.Build.Builder == "nixpacks"`
2. After builder creation (line 199), set nixpacks config if present

- [ ] **Step 2: After detection output (line 191), add nixpacks framework override**

```go
// After line 191: fmt.Printf("[tengiz] detected: %s (port %d)\n"...)
if cfg.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
    fmt.Printf("[tengiz] using nixpacks builder\n")
}
```

- [ ] **Step 3: After builder creation (line 199), set nixpacks config**

```go
// Replace line 199: b := builder.New(dataDir)
// With:
b := builder.New(dataDir)
if cfg.Build.NixpacksConfig != nil {
    b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
}
```

- [ ] **Step 4: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run existing tests**

Run: `go test ./... -v -count=1 2>&1 | head -30`
Expected: no test failures

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire nixpacks config from .tengiz.yaml into deploy command"
```

---

### Task 5: GitDeploy — Wire stored nixpacks config to Builder

**Files:**
- Modify: `internal/gitdeploy/deployer.go`

**Interfaces:**
- Consumes: `existingApp.Config.Build.Builder`, `existingApp.Config.Build.NixpacksConfig` from stored app config
- Produces: detection override and builder config for nixpacks when stored config specifies it

- [ ] **Step 1: Read `Deploy()` method to find insertion points**

Lines 52-229. Detection runs at line 73. Existing config is loaded at line 93-102. Build runs at line 105.

- [ ] **Step 2: After the existing config merge block (line 102 closing brace), add nixpacks override**

```go
// After line 102 closing brace of the if lookupErr == nil block, add:
if lookupErr == nil && existingApp.Config.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
}
if existingApp.Config.Build.NixpacksConfig != nil {
    p.b.SetNixpacksConfig(existingApp.Config.Build.NixpacksConfig)
}
```

- [ ] **Step 3: Compile-check gitdeploy changes**

Run: `go build ./internal/gitdeploy/...`
Expected: exit 0

- [ ] **Step 4: Run gitdeploy tests**

Run: `go test ./internal/gitdeploy/... -v -count=1`
Expected: PASS (or skip if docker-dependent tests are skipped)

- [ ] **Step 5: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "feat: wire nixpacks config into gitdeploy pipeline"
```

---

### Task 6: Preview — Wire nixpacks config to Builder

**Files:**
- Modify: `internal/preview/manager.go`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` from app config
- Produces: builder with nixpacks config for preview CRUD operations

- [ ] **Step 1: Add nixpacks fields to Manager**

Replace `Manager` struct and `NewManager`:

```go
type Manager struct {
    dataDir     string
    store       *config.Store
    rt          runtime.Manager
    builder     *builder.Builder
    nixpacksCfg *types.NixpacksConfig
}

func NewManager(dataDir string, store *config.Store, rt runtime.Manager) *Manager {
    return &Manager{
        dataDir: dataDir,
        store:   store,
        rt:      rt,
        builder: builder.New(dataDir),
    }
}

func (m *Manager) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    m.nixpacksCfg = cfg
    m.builder.SetNixpacksConfig(cfg)
}
```

- [ ] **Step 2: In `Create()` (line 61), after detection (line 64), add nixpacks override**

```go
// After line 64: closing brace of detection error check
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

- [ ] **Step 3: In `Update()` (line 143), after detection (line 146), add nixpacks override**

```go
// After line 146: closing brace of detection error check
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

- [ ] **Step 4: Compile-check preview changes**

Run: `go build ./internal/preview/...`
Expected: exit 0

- [ ] **Step 5: Run preview tests**

Run: `go test ./internal/preview/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/preview/manager.go
git commit -m "feat: wire nixpacks config into preview manager"
```

---

### Task 7: Update `tengiz init` template to document nixpacks

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: none (documentation change)
- Produces: `.tengiz.yaml` template with nixpacks builder commented out

- [ ] **Step 1: Update the init template**

In `internal/cli/root.go`, around line 114-140, add to the template content after `# resources:` section:

```
# build:
#   builder: nixpacks      # use nixpacks build backend (default: docker)
#   nixpacks:
#     packages:             # additional packages to install during build
#       - ffmpeg
#     apt_packages:
#       - libsecret-1-dev
#     cmd: npm run custom   # override start command
```

- [ ] **Step 2: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "docs: document nixpacks builder in init template"
```

---

### Task 8: Full verification

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
git add .
git commit -m "feat: nixpacks build system implementation complete"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers `BuildConfig.Builder` and `NixpacksConfig` types
- Task 2 covers `FrameworkNixpacks` constant, `SetNixpacksConfig()`, `buildWithNixpacks()` in its own file
- Task 3 covers config merge in `LoadForEnvironment`
- Task 4 covers CLI deploy wiring
- Task 5 covers gitdeploy wiring
- Task 6 covers preview manager wiring
- Task 7 covers init template documentation
- Task 8 covers full verification

**2. Placeholder scan:** No TODOs, TBDs, or "add validation" placeholders. Every step has actual code. All method signatures use existing `(string, string, error)` pattern matching `buildWithDockerfile`.

**3. Type consistency:** `FrameworkNixpacks` is consistently used across all 3 callers (CLI deploy, gitdeploy, preview). `NixpacksConfig` struct fields match the nixpacks CLI flags (`--pkgs`, `--apt-pkgs`, `--cmd`, `--pkg-manager`, `--app-dir`). The `SetNixpacksConfig()` setter is present on both `Builder` and `Manager`.
