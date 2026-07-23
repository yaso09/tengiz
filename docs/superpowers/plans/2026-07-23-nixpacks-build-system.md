# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend to support hundreds of frameworks (Rust, Ruby, PHP, Elixir, Deno, Bun, etc.) beyond the current 6.

**Architecture:** Nixpacks is invoked as a subprocess (`nixpacks build`) to produce a Docker image directly. Config in `.tengiz.yaml` (`build.builder: "nixpacks"`) selects the backend. New `buildWithNixpacks()` method on `Builder` handles execution. The existing caller sites (CLI deploy, gitdeploy pipeline, preview manager) detect the builder selection in the config and override the detection framework to `FrameworkNixpacks`, then pass the nixpacks config to the builder.

**Tech Stack:** `nixpacks` CLI (external dependency - `npm install -g nixpacks`), Go `os/exec`, existing `internal/builder` package.

## Global Constraints

- All `os/exec` calls must capture both stdout and stderr as build logs (matching existing `buildWithDockerfile` behavior)
- Image tags must follow existing convention: `tengiz-apps/{appName}:{env}-{deploymentID}`
- Nixpacks must NOT be a hard dependency — if `nixpacks` binary is not in PATH when `build.builder: nixpacks` is set, return a clear error message
- Default behavior (no config) must remain unchanged — existing frameworks continue working identically
- `.tengiz.yaml` config structure: `build.builder` string field, `build.nixpacks` nested config block with `packages`, `apt_packages`, `cmd`, `pkg_manager`, `app_directory`
- All existing tests must continue to pass
- Every task produces an independently testable deliverable

---

### Task 1: Types — Add Nixpacks config fields

**Files:**
- Modify: `internal/types/types.go:42-45`

**Interfaces:**
- Consumes: existing `BuildConfig` struct
- Produces: extended `BuildConfig` with `Builder` and `NixpacksConfig` fields; new `NixpacksConfig` struct

- [ ] **Step 1: Write the failing test**

In `internal/types/types_test.go` (create if not exists):

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
Expected: FAIL — `BuildConfig` has no `Builder` or `NixpacksConfig` fields, `NixpacksConfig` type not defined, file may not exist

- [ ] **Step 3: Extend `BuildConfig` and add `NixpacksConfig`**

In `internal/types/types.go`, replace the `BuildConfig` struct (lines 42-45):

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

### Task 3: Builder — Add `buildWithNixpacks()` method

**Files:**
- Modify: `internal/builder/builder.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.NixpacksConfig` from Task 1, `FrameworkNixpacks` from Task 2
- Produces: `(*Builder).buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` — same signature as `buildWithDockerfile`
- Produces: `(*Builder).SetNixpacksConfig(*types.NixpacksConfig)` setter
- Produces: `(*Builder).nixpacksAvailable() bool` helper

- [ ] **Step 1: Write the tests**

In `internal/builder/builder_test.go`:

```go
func TestBuildWithNixpacksConfigSetter(t *testing.T) {
	b := New(t.TempDir())
	b.SetNixpacksConfig(&types.NixpacksConfig{
		Packages: []string{"curl"},
	})
	if b.nixpacksCfg == nil {
		t.Error("expected nixpacksCfg to be set after SetNixpacksConfig")
	}
	if len(b.nixpacksCfg.Packages) != 1 || b.nixpacksCfg.Packages[0] != "curl" {
		t.Error("expected nixpacksCfg.Packages to contain [curl]")
	}
}

func TestBuildNixpacksDispatchOverridesDetection(t *testing.T) {
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run "TestBuildWithNixpacks|TestBuildNixpacks" -count=1`
Expected: FAIL — `buildWithNixpacks` method not defined, `nixpacksCfg` field missing on `Builder`

- [ ] **Step 3: Implement `buildWithNixpacks` and update `Builder`**

In `internal/builder/builder.go`, add import for `"strings"` and `exec.LookPath` (already imports `"os/exec"`).

Change the `Builder` struct (lines 13-15):

```go
type Builder struct {
	dataDir     string
	nixpacksCfg *types.NixpacksConfig
}
```

Add setter after `New`:

```go
func (b *Builder) SetNixpacksConfig(cfg *types.NixpacksConfig) {
	b.nixpacksCfg = cfg
}
```

Replace the `Build` method (lines 21-29):

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

Add helper and nixpacks build method:

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

Run: `go test ./internal/builder/... -v -run "TestBuildWithNixpacks|TestBuildNixpacks" -count=1`
Expected: PASS (may skip if nixpacks CLI not installed, but the config setter and compile tests pass)

- [ ] **Step 5: Run all builder tests**

Run: `go test ./internal/builder/... -v -count=1`
Expected: PASS (existing tests plus new ones)

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: add buildWithNixpacks method to Builder"
```

---

### Task 4: Config — Merge new build config fields in environment config

**Files:**
- Modify: `internal/config/config.go:101-109`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: extended `BuildConfig` from Task 1
- Produces: merged `Build.Builder` and `Build.NixpacksConfig` from env-specific config in `LoadForEnvironment`

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go` (create if not exists):

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

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

In `internal/config/config.go`, in `LoadForEnvironment` (after line 109 `cfg.Build.Output = envCfg.Build.Output`), add:

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

### Task 5: CLI deploy — Wire build config to Builder

**Files:**
- Modify: `internal/cli/root.go:187-206`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` from config load, `detection.Framework`
- Produces: `builder.Builder` with nixpacks config set when appropriate

- [ ] **Step 1: Write the failing test**

In `internal/cli/root_test.go` (create if not exists):

```go
package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/types"
)

func TestNixpacksConfigWiredToBuilderInDeploy(t *testing.T) {
	// This is a compile-check test — the real verification is in builder tests
	b := builder.New("/tmp/test")
	cfg := &types.AppConfig{
		Build: types.BuildConfig{
			Builder: "nixpacks",
			NixpacksConfig: &types.NixpacksConfig{
				Packages: []string{"curl"},
			},
		},
	}
	if cfg.Build.Builder == "nixpacks" && cfg.Build.NixpacksConfig != nil {
		b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
	}
}
```

- [ ] **Step 2: Modify `root.go` deploy command**

After `detection, err := builder.Detect(projectRoot)` (line 187) and its error check, add:

```go
	// After line 191 (fmt.Printf detected output), before the cfg.Port check:
	if cfg.Build.Builder == "nixpacks" {
		detection.Framework = builder.FrameworkNixpacks
	}
```

After `b := builder.New(dataDir)` (line 199), add:

```go
	// After line 199:
	if cfg.Build.NixpacksConfig != nil {
		b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
	}
```

- [ ] **Step 3: Verify the change compiles and tests pass**

Run: `go build -o /dev/null . && go vet ./...`
Expected: exit 0

- [ ] **Step 4: Run existing tests**

Run: `go test ./... -count=1 2>&1 | head -20`
Expected: no test failures

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire nixpacks config from .tengiz.yaml into deploy command"
```

---

### Task 6: GitDeploy — Wire Nixpacks config to Builder

**Files:**
- Modify: `internal/gitdeploy/deployer.go:52-105`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` — read from stored app config
- Produces: builder correctly configured for nixpacks when the app's stored config specifies it

- [ ] **Step 1: Modify `deployer.go`**

In `Pipeline.Deploy`, after the block that copies stored config into `cfg` (lines 93-102), add:

```go
	// After line 102 (closing brace of lookupErr == nil block):
	if existingApp.Config.Build.Builder == "nixpacks" {
		detection.Framework = builder.FrameworkNixpacks
	}
	if existingApp.Config.Build.NixpacksConfig != nil {
		p.b.SetNixpacksConfig(existingApp.Config.Build.NixpacksConfig)
	}
```

- [ ] **Step 2: Compile-check gitdeploy changes**

Run: `go build ./internal/gitdeploy/...`
Expected: exit 0

- [ ] **Step 3: Run existing tests**

Run: `go test ./internal/gitdeploy/... -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "feat: wire nixpacks config into gitdeploy pipeline"
```

---

### Task 7: Preview — Wire Nixpacks config to Builder

**Files:**
- Modify: `internal/preview/manager.go:18-151`

**Interfaces:**
- Consumes: stored app config with `Build.Builder` and `Build.NixpacksConfig`
- Produces: manager that sets nixpacks config on builder when the app's config specifies it

- [ ] **Step 1: Modify `Manager` struct and `NewManager`**

Add `nixpacksCfg` field to `Manager` struct (line 18-23):

```go
type Manager struct {
	dataDir     string
	store       *config.Store
	rt          runtime.Manager
	builder     *builder.Builder
	nixpacksCfg *types.NixpacksConfig
}
```

Add setter after `NewManager`:

```go
func (m *Manager) SetNixpacksConfig(cfg *types.NixpacksConfig) {
	m.nixpacksCfg = cfg
	m.builder.SetNixpacksConfig(cfg)
}
```

- [ ] **Step 2: Modify `Create` and `Update` methods**

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

- [ ] **Step 3: Wire webhook to pass nixpacks config to preview manager**

In `internal/cli/root.go` (webhook command, line 1072), after `previewMgr := preview.NewManager(dataDir, store, rt)`:

```go
	previewMgr := preview.NewManager(dataDir, store, rt)
	// Load nixpacks config from webhook's .tengiz.yaml or env config
	if configPath != "" {
		appCfg, cfgErr := config.LoadForEnvironment(configPath, env)
		if cfgErr == nil && appCfg.Build.NixpacksConfig != nil {
			previewMgr.SetNixpacksConfig(appCfg.Build.NixpacksConfig)
		}
	}
```

- [ ] **Step 4: Compile-check preview changes**

Run: `go build ./internal/preview/... && go build .`
Expected: exit 0

- [ ] **Step 5: Run all tests**

Run: `go test ./... -count=1`
Expected: all existing tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/preview/manager.go internal/cli/root.go
git commit -m "feat: wire nixpacks config into preview manager"
```

---

### Task 8: Full verification and documentation

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: all PASS (tests that require Docker or nixpacks CLI may be skipped, but framework tests all pass)

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Document in AGENTS.md**

Read `AGENTS.md` and add the nixpacks option to the builder section. Add a line under the `builder` package row:

```
| `buildWithNixpacks` | Exec-based Nixpacks build; invoked when `.tengiz.yaml` sets `build.builder: nixpacks`. Supports hundreds of frameworks (Rust, Ruby, PHP, etc.). |
```

- [ ] **Step 5: Final commit**

```bash
git add AGENTS.md
git commit -m "docs: document nixpacks build backend"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers `BuildConfig.Builder` and `NixpacksConfig` types and their struct fields
- Task 2 covers `FrameworkNixpacks` constant addition
- Task 3 covers `buildWithNixpacks()` method, builder struct extension, dispatch logic, and nixpacks CLI availability check
- Task 4 covers config merge of `Builder` and `NixpacksConfig` fields in `LoadForEnvironment`
- Task 5 covers CLI deploy command wiring
- Task 6 covers gitdeploy pipeline wiring
- Task 7 covers preview manager wiring and webhook integration
- Task 8 covers verification and documentation

**2. Placeholder scan:** No TODOs, TBDs, "add validation", or "write tests for the above" found. Every step has complete code or exact CLI commands.

**3. Type consistency:** All method signatures use the established `(string, string, error)` return pattern. `buildWithNixpacks` matches `buildWithDockerfile` signature. `SetNixpacksConfig` accepts `*types.NixpacksConfig` consistently. Detection framework override (`FrameworkNixpacks`) is consistently applied across all 3 caller sites.
