# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend to support hundreds of frameworks (Rust, Ruby, PHP, Elixir, Deno, Bun, etc.) beyond the current 6 hardcoded frameworks.

**Architecture:** Nixpacks CLI (`nixpacks build`) is invoked via `os/exec` to produce a Docker image directly, bypassing Tengiz's internal Dockerfile generation. A new `buildWithNixpacks()` method on `Builder` handles the Nixpacks execution path. Config in `.tengiz.yaml` (`build.builder: "nixpacks"`) selects the backend and overrides framework detection to `FrameworkNixpacks`. The existing `ensureDockerfile()` → `buildWithDockerfile()` path is preserved as default.

**Tech Stack:** `nixpacks` CLI (external dependency, `npm install -g nixpacks` or `brew install nixpacks`), Go `os/exec`, existing `internal/builder` package.

## Global Constraints

- All `os/exec` calls must capture both stdout and stderr as build logs (matching existing `buildWithDockerfile` behavior in `builder.go:47-49`)
- Image tags must follow existing convention: `tengiz-apps/{appName}:{env}-{deploymentID}` (see `builder.go:44`)
- Nixpacks must NOT be a hard dependency — if `nixpacks` binary not in PATH when `build.builder: nixpacks` is set, return a clear error message (see `builder.go:257-258`)
- Default behavior (no config) must remain unchanged — existing frameworks continue working identically via `ensureDockerfile()` → `buildWithDockerfile()`
- `.tengiz.yaml` config structure: `build.builder` string field, `build.nixpacks` nested config block with `packages`, `apt_packages`, `cmd`, `pkg_manager`, `app_directory`
- All existing tests must continue to pass without modification
- Each task produces independently testable code

---

### Task 1: Types — Add Nixpacks config fields to BuildConfig

**Files:**
- Modify: `internal/types/types.go:42-45`
- Test: `internal/types/types_test.go` (create)

**Interfaces:**
- Consumes: existing `BuildConfig` struct
- Produces: extended `BuildConfig` with `Builder` string field and `NixpacksConfig` pointer field, new `NixpacksConfig` struct

- [ ] **Step 1: Write the failing test**

```go
// internal/types/types_test.go
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
            Packages:    []string{"ffmpeg"},
            AptPackages: []string{"libssl-dev"},
            Cmd:         "node app.js",
            PkgManager:  "yarn",
        },
    }
    if cfg.Builder != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", cfg.Builder)
    }
    if len(cfg.NixpacksConfig.Packages) != 1 || cfg.NixpacksConfig.Packages[0] != "ffmpeg" {
        t.Errorf("packages not set correctly: %v", cfg.NixpacksConfig.Packages)
    }
    if len(cfg.NixpacksConfig.AptPackages) != 1 || cfg.NixpacksConfig.AptPackages[0] != "libssl-dev" {
        t.Errorf("apt_packages not set correctly: %v", cfg.NixpacksConfig.AptPackages)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestNixpacksConfig" -count=1`
Expected: FAIL — `BuildConfig` has no `Builder` or `NixpacksConfig` fields, `NixpacksConfig` type not defined

- [ ] **Step 3: Extend `BuildConfig` and add `NixpacksConfig`**

Edit `internal/types/types.go:42-45`, replace `BuildConfig`:

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

### Task 2: Detection — Add FrameworkNixpacks constant

**Files:**
- Modify: `internal/builder/detect.go:12-20`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `Framework` string type from `internal/builder/detect.go`
- Produces: `FrameworkNixpacks` constant

- [ ] **Step 1: Write the failing test**

Append to `internal/builder/builder_test.go`:

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

In `internal/builder/detect.go`, add after `FrameworkDocker` (line 19):

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

### Task 3: Builder — Add `buildWithNixpacks()` method and dispatch

**Files:**
- Modify: `internal/builder/builder.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.NixpacksConfig` from Task 1, `FrameworkNixpacks` from Task 2
- Produces: `(*Builder).buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` — same signature as `buildWithDockerfile`
- Produces: `(*Builder).SetNixpacksConfig(*types.NixpacksConfig)` setter
- Produces: `Builder.nixpacksCfg` field

- [ ] **Step 1: Write tests**

Append to `internal/builder/builder_test.go`:

```go
func TestNixpacksDispatchWhenFrameworkSet(t *testing.T) {
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

func TestNixpacksConfigSetterAndField(t *testing.T) {
    b := New(t.TempDir())
    b.SetNixpacksConfig(&types.NixpacksConfig{
        Packages: []string{"curl"},
    })
    if b.nixpacksCfg == nil {
        t.Error("expected nixpacksCfg to be set")
    }
    if len(b.nixpacksCfg.Packages) != 1 || b.nixpacksCfg.Packages[0] != "curl" {
        t.Error("expected 1 package: curl")
    }
}

func TestNixpacksAvailableCheck(t *testing.T) {
    b := New(t.TempDir())
    available := b.nixpacksAvailable()
    // Just verify it doesn't panic and returns bool
    _ = available
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run "TestNixpacks" -count=1`
Expected: FAIL — `buildWithNixpacks` method not defined, `nixpacksCfg` field missing, `nixpacksAvailable` missing

- [ ] **Step 3: Implement `buildWithNixpacks` and update `Builder`**

In `internal/builder/builder.go`:

Add `"strings"` and `"os/exec"` to the import block (already imports `"os/exec"`). Add `"strings"`:

```go
import (
    "bytes"
    "context"
    "fmt"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "strings"  // <-- add this

    "github.com/yaso09/tengiz/internal/types"  // <-- add this
)
```

Replace the `Builder` struct with:

```go
type Builder struct {
    dataDir     string
    nixpacksCfg *types.NixpacksConfig
}
```

Replace `Build()` method:

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

Add the `SetNixpacksConfig`, `nixpacksAvailable`, and `buildWithNixpacks` methods:

```go
func (b *Builder) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    b.nixpacksCfg = cfg
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/... -v -run "TestNixpacks" -count=1`
Expected: PASS (TestNixpacksDispatchWhenFrameworkSet may skip if nixpacks CLI not installed, but the config setter and available check tests pass)

- [ ] **Step 5: Run all builder tests to ensure no regressions**

Run: `go test ./internal/builder/... -v -count=1`
Expected: PASS (existing tests like TestBuildCapturesOutput may skip if Docker not available, but all framework detection tests pass)

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: add buildWithNixpacks method and dispatch in Builder"
```

---

### Task 4: Config — Merge Nixpacks builder config in LoadForEnvironment

**Files:**
- Modify: `internal/config/config.go:96-143`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: extended `BuildConfig` from Task 1 with `Builder` and `NixpacksConfig`
- Produces: merged `cfg.Build.Builder` and `cfg.Build.NixpacksConfig` from env-specific config

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

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
      - libssl-dev
    cmd: node app.js
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
    if cfg.Build.NixpacksConfig.Cmd != "node app.js" {
        t.Errorf("expected cmd 'node app.js', got %q", cfg.Build.NixpacksConfig.Cmd)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesBuilderConfig" -count=1`
Expected: FAIL — `Build.Builder` and `Build.NixpacksConfig` not merged in `LoadForEnvironment`

- [ ] **Step 3: Implement the merge in `LoadForEnvironment`**

In `internal/config/config.go:96-143`, after the `Build.Output` merge (line 108), add:

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

### Task 5: CLI deploy — Wire nixpacks config to Builder

**Files:**
- Modify: `internal/cli/root.go:155-345`

**Interfaces:**
- Consumes: `cfg.Build.Builder` (string), `cfg.Build.NixpacksConfig` (pointer) from config load, `detection.Framework`
- Produces: `builder.Builder` with nixpacks config and detection override when `build.builder: nixpacks`

- [ ] **Step 1: Read current deploy command flow**

Read `internal/cli/root.go:155-345` to find exact line numbers for:
- `builder.Detect(projectRoot)` call
- `builder.New(dataDir)` call
- `b.Build(ctx, ...)` call

- [ ] **Step 2: Write the test**

All builder tests already cover the dispatch logic. The CLI change is wiring. Verify compilation.

- [ ] **Step 3: Modify `root.go` deploy command**

After `detection, err := builder.Detect(projectRoot)` and `log.Printf("[tengiz] detected: %s..."...)`, add:

```go
if cfg.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
}
```

After `b := builder.New(dataDir)`, add:

```go
if cfg.Build.NixpacksConfig != nil {
    b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
}
```

- [ ] **Step 4: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run all tests**

Run: `go test ./... -count=1`
Expected: all tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire nixpacks config from .tengiz.yaml into CLI deploy command"
```

---

### Task 6: GitDeploy pipeline — Wire nixpacks config to Builder

**Files:**
- Modify: `internal/gitdeploy/deployer.go:52-229`

**Interfaces:**
- Consumes: `existingApp.Config.Build.Builder` and `existingApp.Config.Build.NixpacksConfig` from stored app entry
- Produces: detection override and builder config for nixpacks when stored app config specifies it

- [ ] **Step 1: Understand the gitdeploy flow**

The gitdeploy pipeline:
1. Does NOT load `.tengiz.yaml` — it constructs a minimal `cfg` from detection + stored app state
2. Detection happens at line 73 before the app lookup
3. Stored app config is merged at lines 93-102
4. Builder is created at line 38 via `builder.New(dataDir)` and is a field on `Pipeline`

The nixpacks config must come from the stored app entry (`existingApp.Config.Build`).

- [ ] **Step 2: Implement the changes**

After `b := builder.New(dataDir)` is not possible (it's at line 38, before detection). Instead, configure the builder after the existing app config is merged.

After the closing brace of the `if lookupErr == nil` block (line 102), add:

```go
if lookupErr == nil {
    if existingApp.Config.Build.Builder == "nixpacks" {
        detection.Framework = builder.FrameworkNixpacks
    }
    if existingApp.Config.Build.NixpacksConfig != nil {
        p.b.SetNixpacksConfig(existingApp.Config.Build.NixpacksConfig)
    }
}
```

For first-time deploys (when `lookupErr != nil`), there's no stored config to read. The framework detection runs before config is available. This is acceptable — first-time gitdeploy without a `.tengiz.yaml` in the repo will use the existing detection path. If users want nixpacks, they must either:
- Include `.tengiz.yaml` in their repo, or
- First deploy via CLI with nixpacks config, then subsequent git pushes pick up the stored config

- [ ] **Step 3: Compile-check gitdeploy changes**

Run: `go build ./internal/gitdeploy/...`
Expected: exit 0

- [ ] **Step 4: Run all tests**

Run: `go test ./internal/gitdeploy/... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "feat: wire nixpacks config into gitdeploy pipeline"
```

---

### Task 7: Preview manager — Wire nixpacks config to Builder

**Files:**
- Modify: `internal/preview/manager.go:18-213`

**Interfaces:**
- Consumes: nixpacks config passed via `NewManager` or setter on `Manager`
- Produces: detection override in `Create()` and `Update()` for nixpacks

- [ ] **Step 1: Extend `Manager` struct and `NewManager`**

The preview manager doesn't have access to app config during `Create()`/`Update()`. It needs nixpacks config passed through the manager.

Change `Manager` struct from:

```go
type Manager struct {
    dataDir string
    store   *config.Store
    rt      runtime.Manager
    builder *builder.Builder
}
```

to:

```go
type Manager struct {
    dataDir     string
    store       *config.Store
    rt          runtime.Manager
    builder     *builder.Builder
    nixpacksCfg *types.NixpacksConfig
}
```

- [ ] **Step 2: Add `SetNixpacksConfig` method**

```go
func (m *Manager) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    m.nixpacksCfg = cfg
    m.builder.SetNixpacksConfig(cfg)
}
```

- [ ] **Step 3: Override detection in `Create()` and `Update()`**

In `Create()` (line 61-69), after `detection, err := builder.Detect(cloneDir)` and after the error check, add:

```go
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

In `Update()` (line 143-151), same pattern — after `detection, err := builder.Detect(cloneDir)` and error check, add:

```go
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

- [ ] **Step 4: Compile-check preview changes**

Run: `go build ./internal/preview/...`
Expected: exit 0

- [ ] **Step 5: Run all tests**

Run: `go test ./internal/preview/... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/preview/manager.go
git commit -m "feat: wire nixpacks config into preview manager"
```

---

### Task 8: Run full test suite and verify

> **Note:** Webhook handler (`internal/webhook/server.go`) delegates to `gitdeploy.Pipeline.Deploy()` via `DeployFunc`—it does NOT call `builder.Build()` directly. No changes needed there; Task 6 covers the gitdeploy path.

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS (tests that require Docker or nixpacks CLI may skip, but all framework tests pass)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created at `./tengiz`

- [ ] **Step 4: Document nixpacks in AGENTS.md**

Edit `AGENTS.md` to add nixpacks to the builder documentation section.

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document nixpacks build backend"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers `BuildConfig.Builder` and `NixpacksConfig` types
- Task 2 covers `FrameworkNixpacks` constant
- Task 3 covers `buildWithNixpacks()` method, `nixpacksAvailable()` check, config setter, and dispatch in `Build()`
- Task 4 covers config merge in `LoadForEnvironment`
- Task 5 covers CLI deploy wiring
- Task 6 covers gitdeploy pipeline wiring
- Task 7 covers preview manager wiring
- Task 8 covers verification and docs (webhook handler delegates to gitdeploy — covered by Task 6)

**2. Placeholder scan:** No TODOs, TBDs, "fill in later", "add validation", or "write tests" without actual code. Every step has real Go code, real commands, or real file edits.

**3. Type consistency:**
- All method signatures use `(string, string, error)` return pattern (matching `buildWithDockerfile`)
- `FrameworkNixpacks` constant value is `"nixpacks"` (consistent with `FrameworkDocker = "docker"`)
- `SetNixpacksConfig` takes `*types.NixpacksConfig` (pointer, matches nil check pattern)
- Detection override sets `detection.Framework = builder.FrameworkNixpacks` consistently across all 3 callers (CLI, gitdeploy, preview)
