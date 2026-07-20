# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend to support hundreds of frameworks (Rust, Ruby, PHP, Elixir, Deno, Bun, etc.) beyond the current 6 (Next.js, Vite, Go, Node, Python, Static, Docker).

**Architecture:** Nixpacks is invoked as a subprocess (`nixpacks build dir --name tag`) to produce a Docker image directly, bypassing the existing Dockerfile generation pipeline. A `build.builder` string field in `.tengiz.yaml` selects the backend — empty/default means current behavior, `"nixpacks"` means use Nixpacks. When Nixpacks is selected, the `Detect()` result's Framework is overridden to `FrameworkNixpacks`, which routes `Builder.Build()` to a new `buildWithNixpacks()` method that mirrors the existing `buildWithDockerfile()` signature and image-tag conventions.

**Tech Stack:** `nixpacks` CLI (external dep, `npm install -g nixpacks` or `brew install nixpacks`), Go `os/exec`, existing `internal/builder` package.

## Global Constraints

- All `os/exec` calls must capture both stdout and stderr as build logs (matching `buildWithDockerfile` behavior)
- Image tags must follow existing convention: `tengiz-apps/{appName}:{env}-{deploymentID}`
- Nixpacks must NOT be a hard dependency — if `nixpacks` binary not in PATH when `build.builder: nixpacks` is set, return clear error message
- Default behavior (no config) must remain identical — existing frameworks continue working
- `.tengiz.yaml` config structure: `build.builder` string field, `build.nixpacks` nested config block
- All existing tests must continue to pass

---

### Task 1: Types — Add Nixpacks config fields to BuildConfig

**Files:**
- Modify: `internal/types/types.go:42-45`

**Interfaces:**
- Consumes: existing `BuildConfig` struct
- Produces: extended `BuildConfig` with `Builder` string field and `NixpacksConfig` pointer field

- [ ] **Step 1: Write the failing test**

In `internal/types/types_test.go` (create if missing):

```go
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

Expected: FAIL — `BuildConfig.Builder` and `BuildConfig.NixpacksConfig` fields missing, `NixpacksConfig` type not defined

- [ ] **Step 3: Implement the new types**

In `internal/types/types.go`, replace the existing `BuildConfig` (lines 42-45):

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

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add Nixpacks config fields to BuildConfig"
```

---

### Task 2: Detection — Add FrameworkNixpacks constant

**Files:**
- Modify: `internal/builder/detect.go:12-20`

**Interfaces:**
- Consumes: `Framework` string type from `internal/builder/detect.go`
- Produces: `FrameworkNixpacks Framework = "nixpacks"` constant

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

In `internal/builder/detect.go`, inside the const block after `FrameworkDocker` (line 19):

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
- Modify: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.NixpacksConfig` from Task 1, `FrameworkNixpacks` from Task 2
- Produces: `(*Builder).buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` — same signature as `buildWithDockerfile`
- Produces: `(*Builder).SetNixpacksConfig(*types.NixpacksConfig)` setter

- [ ] **Step 1: Write the failing tests**

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

func TestSetNixpacksConfig(t *testing.T) {
    b := New(t.TempDir())
    cfg := &types.NixpacksConfig{
        Packages: []string{"curl"},
    }
    b.SetNixpacksConfig(cfg)
    if b.nixpacksCfg == nil {
        t.Error("expected nixpacksCfg to be set")
    }
    if len(b.nixpacksCfg.Packages) != 1 || b.nixpacksCfg.Packages[0] != "curl" {
        t.Error("nixpacksCfg not set correctly")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/builder/... -v -run "TestBuildWithNixpacks|TestSetNixpacksConfig" -count=1`

Expected: FAIL — `buildWithNixpacks` method not defined, `nixpacksCfg` field missing on Builder

- [ ] **Step 3: Implement `buildWithNixpacks`, update `Builder` struct and `Build` dispatch**

In `internal/builder/builder.go`:

Add imports for `"strings"` and `"github.com/yaso09/tengiz/internal/types"`.

Replace the `Builder` struct:

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
```

Replace the `Build` method to dispatch on FrameworkNixpacks:

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

Add the new methods after `buildWithDockerfile`:

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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/builder/... -v -run "TestBuildWithNixpacks|TestSetNixpacksConfig|TestFrameworkNixpacksConstant" -count=1`

Expected: PASS (Docker-dependent tests may skip gracefully)

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: add buildWithNixpacks method to Builder"
```

---

### Task 4: Config — Merge new build config fields in LoadForEnvironment

**Files:**
- Modify: `internal/config/config.go:104-109`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Consumes: extended `BuildConfig` from Task 1
- Produces: merged `Build.Builder` and `Build.NixpacksConfig` from env-specific config

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`:

```go
func TestLoadForEnvironmentMergesNixpacksConfig(t *testing.T) {
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

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesNixpacksConfig" -count=1`

Expected: FAIL — `Build.Builder` and `Build.NixpacksConfig` not merged in `LoadForEnvironment`

- [ ] **Step 3: Add merge logic in LoadForEnvironment**

In `internal/config/config.go`, after line 109 (`cfg.Build.Output = envCfg.Build.Output`), add:

```go
if envCfg.Build.Builder != "" {
    cfg.Build.Builder = envCfg.Build.Builder
}
if envCfg.Build.NixpacksConfig != nil {
    cfg.Build.NixpacksConfig = envCfg.Build.NixpacksConfig
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesNixpacksConfig" -count=1`

Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`

Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge nixpacks config in environment config loader"
```

---

### Task 5: CLI deploy — Wire build config to Builder

**Files:**
- Modify: `internal/cli/root.go:187-201`

**Interfaces:**
- Consumes: `cfg.Build.Builder` and `cfg.Build.NixpacksConfig` from config load
- Produces: `detection.Framework` overridden to `FrameworkNixpacks` when config says so; `builder.Builder` with nixpacks config set

- [ ] **Step 1: Read the current deploy flow in root.go**

The deploy command (around line 170-210):
1. Loads config via `config.LoadForEnvironment(projectRoot, env)` → `cfg`
2. Runs `builder.Detect(projectRoot)` → `detection`
3. Creates `builder.New(dataDir)` → `b`
4. Calls `b.Build(ctx, projectRoot, cfg.Name, env, detection, deploymentID)`

We'll intercept between step 2 and step 3.

- [ ] **Step 2: Write a compile-check test**

No pure unit test for the CLI (it's in `package main`). Verification is via `go vet` and `go build`.

- [ ] **Step 3: Modify root.go deploy command**

After detection (around line 191, after `log.Printf("[tengiz] detected: %s (port %d)", detection.Framework, detection.InternalPort)`), add:

```go
if cfg.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
}
```

After builder creation (around line 199 `b := builder.New(dataDir)`), add:

```go
if cfg.Build.NixpacksConfig != nil {
    b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
}
```

- [ ] **Step 4: Verify changes compile**

Run: `go build -o /dev/null .`

Expected: exit 0

- [ ] **Step 5: Run existing tests**

Run: `go test ./... -count=1 2>&1 | tail -20`

Expected: no test failures (Docker-dependent tests may skip)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire nixpacks config from .tengiz.yaml into deploy command"
```

---

### Task 6: GitDeploy + Preview — Wire Nixpacks config to Builder

**Files:**
- Modify: `internal/gitdeploy/deployer.go:73-102`
- Modify: `internal/preview/manager.go:46-69`

**Interfaces:**
- Consumes: stored app config's `Build.Builder` and `Build.NixpacksConfig` for gitdeploy; `build.builder` from `.tengiz.yaml` for preview
- Produces: builder correctly configured for nixpacks when the app's config specifies it

- [ ] **Step 1: Modify `gitdeploy/deployer.go`**

The `Pipeline.Deploy()` method loads existing app config at line 71 (`existingApp, lookupErr := p.store.GetApp(appName)`). After the block that merges existing config into `cfg` (lines 93-102), add:

```go
if lookupErr == nil && existingApp.Config.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
    if existingApp.Config.Build.NixpacksConfig != nil {
        p.b.SetNixpacksConfig(existingApp.Config.Build.NixpacksConfig)
    }
}
```

- [ ] **Step 2: Compile-check gitdeploy changes**

Run: `go build ./internal/gitdeploy/...`

Expected: exit 0

- [ ] **Step 3: Modify `preview/manager.go`**

The preview manager creates a fresh `Manager` per usage. Add nixpacks config field to the struct and a setter.

Add field to `Manager` struct (line 18-23):

```go
type Manager struct {
    dataDir     string
    store       *config.Store
    rt          runtime.Manager
    builder     *builder.Builder
    nixpacksCfg *types.NixpacksConfig
}
```

Add setter method after `NewManager`:

```go
func (m *Manager) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    m.nixpacksCfg = cfg
    m.builder.SetNixpacksConfig(cfg)
}
```

In `Create()` (line 61), after detection:

```go
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

In `Update()` (line 143), after detection:

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

### Task 7: Integration verification and documentation

**Files:**
- Modify: `AGENTS.md`
- Modify: `README.md` (if nixpacks CLI requirement should be documented)

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -v -count=1 2>&1 | tail -30`

Expected: all tests PASS (Docker-dependent tests may Skip)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`

Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`

Expected: binary created successfully at `./tengiz`

- [ ] **Step 4: Update AGENTS.md**

Read `AGENTS.md` and add Nixpacks build option to the `builder` section. Append under the builder row:

```
| `builder` | Nixpacks backend (optional). Set `build.builder: nixpacks` in `.tengiz.yaml` to enable. Nixpacks CLI must be installed separately. |
| `nixpacks` | Nested config for `build.nixpacks`: `packages`, `apt_packages`, `cmd`, `pkg_manager`, `app_directory`. |
```

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document nixpacks build backend in AGENTS.md"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers `BuildConfig.Builder` + `NixpacksConfig` type definitions
- Task 2 covers `FrameworkNixpacks` constant
- Task 3 covers `buildWithNixpacks()` method and dispatch in `Builder.Build()`
- Task 4 covers config merge in `LoadForEnvironment`
- Task 5 covers CLI deploy command wiring
- Task 6 covers gitdeploy and preview manager wiring
- Task 7 covers integration verification and docs

**2. Placeholder scan:** No TODOs, TBDs, "add validation", or "write tests for above". Every step has complete code with specific file paths, exact method signatures, and full test implementations.

**3. Type consistency:** All method signatures use the existing `(string, string, error)` return pattern matching `buildWithDockerfile`. `buildWithNixpacks` shares identical parameters. `FrameworkNixpacks` constant name is consistent across detect.go (`const` block), builder.go (`switch case`), and all callers (root.go, deployer.go, manager.go). The `SetNixpacksConfig` setter is available on `*Builder` and is called identically in all three callers.
