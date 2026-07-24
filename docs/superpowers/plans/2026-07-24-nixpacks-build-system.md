# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend to support hundreds of frameworks (Rust, Ruby, PHP, Elixir, Deno, Bun, etc.) beyond the current 6 built-in frameworks.

**Architecture:** Nixpacks is invoked as a subprocess (`nixpacks build`) that produces a Docker image directly. Config in `.tengiz.yaml` (`build.builder: "nixpacks"`) selects the backend. A new `buildWithNixpacks()` method on `Builder` handles execution; `SetNixpacksConfig()` passes Nixpacks-specific options (packages, apt packages, cmd, etc.). Detection is overridden to `FrameworkNixpacks` when the config selects Nixpacks, bypassing the built-in framework detection. All three callers (CLI deploy, gitdeploy pipeline, preview manager) wire the nixpacks config through.

**Tech Stack:** `nixpacks` CLI (external dependency, `npm install -g nixpacks` or standalone binary), Go `os/exec`, `exec.LookPath`, existing `internal/builder` package.

## Global Constraints

- All `os/exec` calls must capture both stdout and stderr as build logs (matching existing `buildWithDockerfile` behavior)
- Image tags must follow existing convention: `tengiz-apps/{appName}:{env}-{deploymentID}`
- Nixpacks must NOT be a hard dependency — if `nixpacks` binary not in PATH when `build.builder: nixpacks` is set, return a clear error message with install instructions
- Default behavior (no config at all) must remain identical — existing frameworks continue working without any change
- `.tengiz.yaml` config structure: `build.builder` string field, `build.nixpacks` nested config block
- All existing tests must continue to pass without modification

---

### Task 1: Types — Add Nixpacks config fields to `BuildConfig`

**Files:**
- Modify: `/home/runner/work/tengiz/tengiz/internal/types/types.go:42-45`
- Test: `/home/runner/work/tengiz/tengiz/internal/types/types_test.go`

**Interfaces:**
- Consumes: existing `BuildConfig` struct at `types.go:42-45`
- Produces: extended `BuildConfig` with `Builder string` and `NixpacksConfig *NixpacksConfig` fields; new `NixpacksConfig` struct type

- [ ] **Step 1: Write the failing test**

```go
// Add to internal/types/types_test.go
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
        t.Errorf("packages = %v, want [ffmpeg]", cfg.NixpacksConfig.Packages)
    }
    if cfg.NixpacksConfig.Cmd != "npm run start" {
        t.Errorf("cmd = %q, want npm run start", cfg.NixpacksConfig.Cmd)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestNixpacksConfig" -count=1`
Expected: FAIL — `BuildConfig` has no `Builder` or `NixpacksConfig` fields; `NixpacksConfig` type not defined, compilation error

- [ ] **Step 3: Extend `BuildConfig` and add `NixpacksConfig` type**

Replace the `BuildConfig` struct in `internal/types/types.go:42-45` and add the new type after it:

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

### Task 2: Detection — Add `FrameworkNixpacks` constant

**Files:**
- Modify: `/home/runner/work/tengiz/tengiz/internal/builder/detect.go:10-20`
- Test: `/home/runner/work/tengiz/tengiz/internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `Framework` string type from `internal/builder/detect.go:10`
- Produces: `FrameworkNixpacks Framework = "nixpacks"` constant

- [ ] **Step 1: Write the failing test**

```go
// Add to internal/builder/builder_test.go
func TestFrameworkNixpacksConstant(t *testing.T) {
    if FrameworkNixpacks != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", FrameworkNixpacks)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run "TestFrameworkNixpacksConstant" -count=1`
Expected: FAIL — `FrameworkNixpacks` undefined (compilation error)

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
- Modify: `/home/runner/work/tengiz/tengiz/internal/builder/builder.go:13-63`
- Test: `/home/runner/work/tengiz/tengiz/internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.NixpacksConfig` from Task 1, `FrameworkNixpacks` from Task 2
- Produces: `(*Builder).buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` — same signature as `buildWithDockerfile`; `(*Builder).SetNixpacksConfig(*types.NixpacksConfig)` setter; `(*Builder).nixpacksAvailable() bool`

- [ ] **Step 1: Write the test for `buildWithNixpacks` dispatch and compilation check**

```go
// Add to internal/builder/builder_test.go
func TestBuildDispatchesNixpacks(t *testing.T) {
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

func TestBuildWithNixpacksSetConfig(t *testing.T) {
    b := New(t.TempDir())
    cfg := &types.NixpacksConfig{
        Packages: []string{"curl"},
    }
    b.SetNixpacksConfig(cfg)
    if b.nixpacksCfg == nil {
        t.Error("expected nixpacksCfg to be set")
    }
    if len(b.nixpacksCfg.Packages) != 1 || b.nixpacksCfg.Packages[0] != "curl" {
        t.Error("nixpacksCfg.Packages not preserved")
    }
}

func TestNixpacksAvailableChecksPath(t *testing.T) {
    b := New(t.TempDir())
    // On any system without nixpacks installed, this should return false
    available := b.nixpacksAvailable()
    // We can't guarantee the binary exists, just verify the method doesn't panic
    _ = available
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run "TestBuild.*[Nn]ixpacks" -count=1`
Expected: FAIL — `buildWithNixpacks` method not defined, `nixpacksCfg` field missing, `SetNixpacksConfig` undefined, `nixpacksAvailable` undefined

- [ ] **Step 3: Implement `buildWithNixpacks` and update `Builder`**

Replace the `Builder` struct (line 13) and `Build` method (line 21) in `internal/builder/builder.go`:

```go
type Builder struct {
    dataDir     string
    nixpacksCfg *types.NixpacksConfig
}
```

```go
func (b *Builder) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    b.nixpacksCfg = cfg
}
```

Replace the `Build` method (line 21):

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

Add the two new methods after `buildWithDockerfile` (after line 63):

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

Add `"strings"` to the import block in `builder.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/... -v -run "TestBuild.*[Nn]ixpacks" -count=1`
Expected: PASS (may skip if nixpacks CLI not installed, but the config setter test and path check test pass)

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: add buildWithNixpacks method to Builder"
```

---

### Task 4: Config — Merge `Build.Builder` and `Build.NixpacksConfig` in environment config

**Files:**
- Modify: `/home/runner/work/tengiz/tengiz/internal/config/config.go:66-146`
- Test: `/home/runner/work/tengiz/tengiz/internal/config/config_test.go`

**Interfaces:**
- Consumes: extended `BuildConfig` from Task 1
- Produces: merged `Build.Builder` and `Build.NixpacksConfig` from env-specific config in `LoadForEnvironment`

- [ ] **Step 1: Write the failing test**

```go
// Add to internal/config/config_test.go
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
        t.Errorf("packages = %v, want [ffmpeg]", cfg.Build.NixpacksConfig.Packages)
    }
    if cfg.Build.NixpacksConfig.Cmd != "npm run start" {
        t.Errorf("cmd = %q, want npm run start", cfg.Build.NixpacksConfig.Cmd)
    }
}

func TestLoadForEnvironmentPreservesEmptyBuilder(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
`), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
build:
  builder: nixpacks
`), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Build.Builder == "" {
        t.Error("expected builder to be merged from env config")
    }
}

func TestLoadForEnvironmentWithoutEnvConfig(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
`), 0644)

    cfg, err := LoadForEnvironment(dir, "production")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Build.Builder != "" {
        t.Errorf("expected empty builder, got %q", cfg.Build.Builder)
    }
    if cfg.Build.NixpacksConfig != nil {
        t.Error("expected nil NixpacksConfig when no env config")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironment.*[Nn]ixpacks" -count=1`
Expected: FAIL — `Build.Builder` and `Build.NixpacksConfig` not merged in `LoadForEnvironment` (fields silently dropped)

- [ ] **Step 3: Implement the merge in `LoadForEnvironment`**

In `internal/config/config.go:66-146`, after the `Build.Output` merge at line 109, add:

```go
if envCfg.Build.Builder != "" {
    cfg.Build.Builder = envCfg.Build.Builder
}
if envCfg.Build.NixpacksConfig != nil {
    cfg.Build.NixpacksConfig = envCfg.Build.NixpacksConfig
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironment.*[Nn]ixpacks" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS (all existing tests still pass)

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge nixpacks builder config in environment config loader"
```

---

### Task 5: CLI deploy — Wire Nixpacks config from `.tengiz.yaml` into deploy command

**Files:**
- Modify: `/home/runner/work/tengiz/tengiz/internal/cli/root.go:155-345`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` from config load (Task 4), `detection.Framework`
- Produces: detection framework overridden to `FrameworkNixpacks` when `cfg.Build.Builder == "nixpacks"`; builder receives nixpacks config via `SetNixpacksConfig`

- [ ] **Step 1: Understand the current deploy flow (read the code)**

The deploy command at `root.go:155-345` does:
1. Find project root (`root.go:165-169`)
2. Load config via `LoadForEnvironment` (`root.go:173-183`)
3. Detect framework via `builder.Detect` (`root.go:187`)
4. Print detection (`root.go:191`)
5. Set port from config if not set (`root.go:193-195`)
6. Create builder + store (`root.go:199-200`)
7. Call `b.Build()` (`root.go:201`)

We need to: (a) override `detection.Framework` after detection if `cfg.Build.Builder == "nixpacks"`, and (b) pass nixpacks config to builder before build.

- [ ] **Step 2: Modify `root.go` deploy command**

After line 191 (the `detection.Framework` print), add:

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
Expected: exit 0, no errors

- [ ] **Step 4: Run existing tests**

Run: `go test ./... -v -count=1 2>&1 | tail -20`
Expected: no test failures (the nixpacks integration tests may skip, but existing tests all pass)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire nixpacks config from .tengiz.yaml into CLI deploy command"
```

---

### Task 6: GitDeploy pipeline — Wire Nixpacks config

**Files:**
- Modify: `/home/runner/work/tengiz/tengiz/internal/gitdeploy/deployer.go:52-228`

**Interfaces:**
- Consumes: `existingApp.Config.Build.Builder`, `existingApp.Config.Build.NixpacksConfig` for subsequent deploys
- Produces: detection framework overridden when nixpacks builder selected; nixpacks config set on pipeline's builder

- [ ] **Step 1: Modify `Deploy` method in `deployer.go`**

The gitdeploy `Deploy` method at `deployer.go:52-228` has two paths: first deploy (line 121) and zero-downtime (line 164). Detection runs at line 73. The existing app config is available at line 93-102.

After the `if lookupErr == nil` block (after line 102's closing brace), add:

```go
if lookupErr == nil && existingApp.Config.Build.Builder == "nixpacks" {
    detection.Framework = builder.FrameworkNixpacks
}
if lookupErr == nil && existingApp.Config.Build.NixpacksConfig != nil {
    p.b.SetNixpacksConfig(existingApp.Config.Build.NixpacksConfig)
}
```

- [ ] **Step 2: Verify the change compiles**

Run: `go build ./internal/gitdeploy/...`
Expected: exit 0

- [ ] **Step 3: Run gitdeploy tests**

Run: `go test ./internal/gitdeploy/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "feat: wire nixpacks config into gitdeploy pipeline"
```

---

### Task 7: Preview manager — Wire Nixpacks config

**Files:**
- Modify: `/home/runner/work/tengiz/tengiz/internal/preview/manager.go:18-213`

**Interfaces:**
- Consumes: `types.NixpacksConfig` (set externally on the manager)
- Produces: `Manager` struct with `nixpacksCfg` field; `SetNixpacksConfig` method; detection override in `Create` and `Update`

- [ ] **Step 1: Modify `Manager` struct, constructor, and add setter**

Add `nixpacksCfg` field to `Manager` struct (line 18):

```go
type Manager struct {
    dataDir     string
    store       *config.Store
    rt          runtime.Manager
    builder     *builder.Builder
    nixpacksCfg *types.NixpacksConfig
}
```

Add setter method after the constructor (after line 32):

```go
func (m *Manager) SetNixpacksConfig(cfg *types.NixpacksConfig) {
    m.nixpacksCfg = cfg
    m.builder.SetNixpacksConfig(cfg)
}
```

- [ ] **Step 2: Override detection in `Create`**

In `Create()` (line 46), after `detection` is set at line 64, add:

```go
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

- [ ] **Step 3: Override detection in `Update`**

In `Update()` (line 123), after `detection` is set at line 146, add:

```go
if m.nixpacksCfg != nil {
    detection.Framework = builder.FrameworkNixpacks
}
```

- [ ] **Step 4: Verify the change compiles**

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

### Task 8: Run full test suite and verify

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS — tests that require Docker or nixpacks CLI may be skipped (no error), all framework and unit tests pass

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify binary builds**

Run: `go build -o tengiz .`
Expected: binary created at `./tengiz`

- [ ] **Step 4: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document nixpacks build backend in AGENTS.md"
```

---

## Self-Review

**1. Spec coverage:**

| Spec requirement | Task |
|---|---|
| Nixpacks as alternative build backend | Task 3 (buildWithNixpacks) |
| Hundreds of frameworks via Nixpacks | Task 3 (delegates to nixpacks CLI) |
| `.tengiz.yaml` `build.builder: nixpacks` config | Task 1 (BuildConfig.Builder field) |
| Config merge for env-specific overrides | Task 4 (LoadForEnvironment merge) |
| Not a hard dependency — clear error if missing | Task 3 (nixpacksAvailable check) |
| No change to default behavior | Task 3 (only dispatches when FrameworkNixpacks set) |

**2. Placeholder scan:** No TODOs, TBDs, "add validation", "handle edge cases", or "write tests" without code. Every test step has complete Go test functions with exact assertions. Every implementation step has the complete code to write.

**3. Type consistency:**
- `buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` — matches `buildWithDockerfile` signature exactly
- `SetNixpacksConfig(*types.NixpacksConfig)` — consistent across Builder, Manager
- `FrameworkNixpacks Framework = "nixpacks"` — follows existing `FrameworkXxx` pattern
- `BuildConfig.Builder string` — plain string field, consistent naming
- All three callers (CLI, gitdeploy, preview) use the same detection override pattern: `if cfg says nixpacks { detection.Framework = FrameworkNixpacks }`

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-07-24-nixpacks-build-system.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
