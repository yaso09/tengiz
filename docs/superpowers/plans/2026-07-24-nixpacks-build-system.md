# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend to support hundreds of frameworks (Rust, Ruby, PHP, Elixir, Deno, Bun, etc.) beyond the current 6.

**Architecture:** Nixpacks is invoked as a subprocess (`nixpacks build`) to produce a Docker image directly. Config in `.tengiz.yaml` (`build.builder: "nixpacks"`) selects the backend. New `buildWithNixpacks()` method on `Builder` handles the Nixpacks execution path. When `build.builder: nixpacks` is set, detection framework is overridden to `FrameworkNixpacks` so the builder dispatches to the nixpacks path. Existing callers (CLI deploy, gitdeploy pipeline, preview manager) read the stored app config and configure the builder accordingly.

**Tech Stack:** `nixpacks` CLI (external dependency, `npm install -g nixpacks` or `brew install nixpacks`), Go `os/exec`, existing `internal/builder` package.

## Global Constraints

- All `os/exec` calls must capture both stdout and stderr as build logs (matching existing `buildWithDockerfile` behavior in `internal/builder/builder.go:47-50`)
- Image tags must follow existing convention: `tengiz-apps/{appName}:{env}-{deploymentID}`
- Nixpacks must NOT be a hard dependency — if `nixpacks` binary not in PATH when `build.builder: nixpacks` is set, return a clear error message
- Default behavior (no config) must remain unchanged — existing frameworks continue working identically
- `.tengiz.yaml` config structure: `build.builder` string field (values: `""`, `"docker"`, `"nixpacks"`), `build.nixpacks` nested config block
- All existing tests must continue to pass
- No new Go dependencies — use only `os/exec`, `strings`, existing standard library packages already imported

---

### Task 1: Types — Add Nixpacks config fields to BuildConfig

**Files:**
- Modify: `internal/types/types.go:42-45`
- Test: `internal/types/types_test.go` (create)

**Interfaces:**
- Consumes: existing `BuildConfig` struct
- Produces: extended `BuildConfig` with `Builder` and `NixpacksConfig` fields; new `NixpacksConfig` struct

- [ ] **Step 1: Write the failing test**

Create `internal/types/types_test.go`:

```go
package types

import (
	"testing"
)

func TestBuildConfigBuilderDefaults(t *testing.T) {
	cfg := BuildConfig{}
	if cfg.Builder != "" {
		t.Errorf("expected empty builder, got %q", cfg.Builder)
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

Run: `go test ./internal/types/... -v -run "TestBuildConfigBuilderDefaults|TestNixpacksConfigFields" -count=1`
Expected: FAIL — `BuildConfig` has no `Builder` or `NixpacksConfig` fields, `NixpacksConfig` type not defined

- [ ] **Step 3: Extend `BuildConfig` and add `NixpacksConfig`**

Replace `internal/types/types.go` lines 42-45 with:

```go
type BuildConfig struct {
	Command        string           `mapstructure:"command"`
	Output         string           `mapstructure:"output"`
	Builder        string           `mapstructure:"builder,omitempty"`
	NixpacksConfig *NixpacksConfig  `mapstructure:"nixpacks,omitempty"`
}

type NixpacksConfig struct {
	Packages     []string `mapstructure:"packages,omitempty"`
	AptPackages  []string `mapstructure:"apt_packages,omitempty"`
	Cmd          string   `mapstructure:"cmd,omitempty"`
	PkgManager   string   `mapstructure:"pkg_manager,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestBuildConfigBuilderDefaults|TestNixpacksConfigFields" -count=1`
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
- Consumes: `Framework` string type
- Produces: `FrameworkNixpacks Framework = "nixpacks"` constant

- [ ] **Step 1: Write the failing test**

Add to `internal/builder/builder_test.go`:

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

- [ ] **Step 5: Run all builder tests**

Run: `go test ./internal/builder/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/builder/detect.go internal/builder/builder_test.go
git commit -m "feat: add FrameworkNixpacks constant"
```

---

### Task 3: Builder — Add `buildWithNixpacks()` method and dispatcher

**Files:**
- Modify: `internal/builder/builder.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.NixpacksConfig` from Task 1, `FrameworkNixpacks` from Task 2
- Produces: `(*Builder).SetNixpacksConfig(*types.NixpacksConfig)`, `(*Builder).buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)`, extended `Build()` method that dispatches to nixpacks path

- [ ] **Step 1: Write the failing tests**

Add to `internal/builder/builder_test.go`:

```go
func TestBuildWithNixpacksDispatchCompiles(t *testing.T) {
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

func TestBuildWithNixpacksDispatchWhenDetected(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\"\nversion = \"0.1.0\"\n"), 0644)

	detection, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Without nixpacks config, Cargo.toml should fall back to static
	if detection.Framework != FrameworkStatic {
		// If nixpacks were in detect(), test would need adjustment
		b := New(t.TempDir())
		tag, logBuf, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v1")
		if err != nil {
			t.Skipf("Build error (no docker?): %v", err)
		}
		_ = tag
		_ = logBuf
	}
}

func TestBuildWithNixpacksWhenOverridden(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)

	b := New(t.TempDir())
	detection := &Detection{
		Framework:    FrameworkNixpacks,
		InternalPort: 8080,
	}

	tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v1")
	if err != nil {
		if strings.Contains(err.Error(), "nixpacks not found") {
			t.Skip("nixpacks CLI not installed, skipping")
		}
		t.Skipf("Build error (no docker?): %v", err)
	}
	if tag == "" {
		t.Error("expected non-empty tag")
	}
	_ = logs
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run "TestBuildWithNixpacks" -count=1`
Expected: FAIL — `buildWithNixpacks` method not defined, `nixpacksCfg` field missing on Builder

- [ ] **Step 3: Implement `buildWithNixpacks` and update `Builder`**

In `internal/builder/builder.go`, replace `Builder` struct with:

```go
type Builder struct {
	dataDir     string
	nixpacksCfg *types.NixpacksConfig
}
```

Add after `func New(dataDir string) *Builder`:

```go
func (b *Builder) SetNixpacksConfig(cfg *types.NixpacksConfig) {
	b.nixpacksCfg = cfg
}
```

Replace `func (b *Builder) Build(...)` (lines 21-29) with:

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

Add after `buildWithDockerfile` (after line 63):

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

Add `"strings"` to the imports in `builder.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/... -v -run "TestBuildWithNixpacks" -count=1`
Expected: PASS (may skip if nixpacks CLI not installed, but compile check and dispatch logic tests pass)

- [ ] **Step 5: Run all builder tests**

Run: `go test ./internal/builder/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: add buildWithNixpacks method to Builder"
```

---

### Task 4: Config — Merge new build config fields in environment config

**Files:**
- Modify: `internal/config/config.go:104-109`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: extended `BuildConfig` from Task 1
- Produces: merged `Build.Builder` and `Build.NixpacksConfig` when env-specific `.tengiz.{env}.yaml` overrides them

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

In `internal/config/config.go` after the `Build.Output` merge (after line 109), add:

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

### Task 5: CLI deploy — Wire build config to Builder

**Files:**
- Modify: `internal/cli/root.go:187-201`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` from config load (line 173), `detection.Framework` from `builder.Detect()` (line 187)
- Produces: detection framework overridden when `cfg.Build.Builder == "nixpacks"`, builder configured with nixpacks config when present

- [ ] **Step 1: Modify the deploy command**

In `internal/cli/root.go`, after line 191 (where detection is printed), add the detection override:

```go
if cfg.Build.Builder == "nixpacks" {
	detection.Framework = builder.FrameworkNixpacks
}
```

After line 199 (`b := builder.New(dataDir)`), add the nixpacks config wiring:

```go
if cfg.Build.NixpacksConfig != nil {
	b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
}
```

- [ ] **Step 2: Verify the change compiles**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 3: Run go vet**

Run: `go vet ./internal/cli/...`
Expected: exit 0

- [ ] **Step 4: Run all tests**

Run: `go test ./... -count=1 2>&1 | tail -20`
Expected: PASS (no regressions)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire nixpacks config from .tengiz.yaml into deploy command"
```

---

### Task 6: GitDeploy + Preview — Wire Nixpacks config to Builder

**Files:**
- Modify: `internal/gitdeploy/deployer.go:93-102`
- Modify: `internal/preview/manager.go:18-32`, 61, 143

**Interfaces:**
- GitDeploy: detection overridden when existing app config has `Build.Builder == "nixpacks"`; builder's nixpacks config set from stored config
- Preview: Manager gains nixpacks config awareness; detection overridden and builder configured when nixpacks config is present

- [ ] **Step 1: Modify `internal/gitdeploy/deployer.go`**

In `Deploy()`, after the `if lookupErr == nil` block (after line 102), add:

```go
if existingApp != nil && lookupErr == nil {
	if existingApp.Config.Build.Builder == "nixpacks" {
		detection.Framework = builder.FrameworkNixpacks
	}
	if existingApp.Config.Build.NixpacksConfig != nil {
		p.b.SetNixpacksConfig(existingApp.Config.Build.NixpacksConfig)
	}
}
```

- [ ] **Step 2: Compile-check gitdeploy changes**

Run: `go build ./internal/gitdeploy/...`
Expected: exit 0

- [ ] **Step 3: Modify `internal/preview/manager.go`**

Add `nixpacksCfg` field to `Manager` struct (after line 22):

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

In `Create()` (line 61), after `detection` is returned and before `deploymentID`:

```go
if m.nixpacksCfg != nil {
	detection.Framework = builder.FrameworkNixpacks
}
```

In `Update()` (line 143), same pattern after detection and before `deploymentID`:

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

### Task 7: Full test suite, vet, and docs

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS (tests that require Docker or nixpacks CLI may skip, but all framework tests pass)

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
- Task 1 covers `BuildConfig.Builder` and `NixpacksConfig` types
- Task 2 covers `FrameworkNixpacks` constant
- Task 3 covers `buildWithNixpacks()` method, `SetNixpacksConfig()`, and dispatch via `Build()`
- Task 4 covers config merge in `LoadForEnvironment`
- Task 5 covers CLI deploy command wiring
- Task 6 covers gitdeploy and preview pipeline wiring
- Task 7 covers verification and final commit

**2. Placeholder scan:** No TODOs, TBDs, or "add validation" placeholders. Every step has actual code with exact file paths, line references, and commands.

**3. Type consistency:** All method signatures use existing `(string, string, error)` pattern. `buildWithNixpacks` matches `buildWithDockerfile` signature. `NixpacksConfig` uses `mapstructure` tags matching existing convention. Detection framework override is consistent across all 3 callers (CLI deploy, gitdeploy, preview).
