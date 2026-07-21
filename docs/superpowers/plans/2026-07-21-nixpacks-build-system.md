# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend to expand framework support from 6 to hundreds (Rust, Ruby, PHP, Elixir, Deno, Bun, Java, etc.).

**Architecture:** Nixpacks CLI runs as a subprocess (`nixpacks build`) producing a Docker image directly. A new `FrameworkNixpacks` constant and `buildWithNixpacks()` method on `Builder` handle the new execution path. Users opt in via `.tengiz.yaml` `build.builder: nixpacks`, or detection triggers when `.nixpacks/config.toml` exists. The existing `Detect()` function, `Build()` dispatch, and all three callers (CLI deploy, gitdeploy pipeline, preview manager) get minimal modifications.

**Tech Stack:** `nixpacks` CLI (external dep, `npm install -g nixpacks` or `brew install nixpacks`), Go `os/exec`, existing `internal/builder` package.

## Global Constraints

- All `os/exec` calls must capture both stdout and stderr as build logs (matching existing `buildWithDockerfile`)
- Image tags must follow existing convention: `tengiz-apps/{appName}:{env}-{deploymentID}`
- Nixpacks must NOT be a hard dependency — if the binary is missing and `build.builder: nixpacks` is set, return a clear error message with install instructions
- Default behavior (no config, no `.nixpacks/` dir) must remain unchanged — existing 6 frameworks continue working identically
- All existing tests must continue to pass with no modification

---

## File Structure

| File | Change | Responsibility |
|------|--------|---------------|
| `internal/types/types.go:42-45` | Modify `BuildConfig` — add `Builder string` field | Config option to select build backend |
| `internal/builder/detect.go:12-20` | Add `FrameworkNixpacks` constant to const block | New framework enum value |
| `internal/builder/detect.go:30-78` | Add detection branch for `.nixpacks/config.toml` | Auto-detect Nixpacks projects |
| `internal/builder/builder.go` | Add `nixpacksCfg` field, `SetNixpacksConfig()`, `buildWithNixpacks()` | New build execution path |
| `internal/builder/builder.go:21-29` | Modify `Build()` dispatch — add `FrameworkNixpacks` case | Route nixpacks builds to new method |
| `internal/builder/builder_test.go` | Add tests for detection + nixpacks build path | Test coverage |
| `internal/config/config.go:104-109` | Add merge for `cfg.Build.Builder` in `LoadForEnvironment` | Env-specific builder override |
| `internal/cli/root.go:187-201` | Add framework override + nixpacks config wiring | CLI deploy uses nixpacks when configured |
| `internal/cli/root.go:567-578` | Add `FrameworkNixpacks` case to `devCmd` | Dev mode handling |
| `internal/gitdeploy/deployer.go:73-105` | Add nixpacks override from stored config | Git deploy uses nixpacks |
| `internal/preview/manager.go` | Add `SetNixpacksConfig()`, override in `Create`/`Update` | Preview deployments use nixpacks |

---

### Task 1: Types — Add `Builder` field to `BuildConfig`

**Files:**
- Modify: `internal/types/types.go:42-45`

**Interfaces:**
- Consumes: existing `BuildConfig` struct with `Command`, `Output` fields
- Produces: `BuildConfig` with new `Builder string` field

- [ ] **Step 1: Write the failing test**

In `internal/types/types_test.go` (create if needed):

```go
package types

import "testing"

func TestBuildConfigBuilderField(t *testing.T) {
	cfg := BuildConfig{Builder: "nixpacks"}
	if cfg.Builder != "nixpacks" {
		t.Errorf("Builder = %q, want %q", cfg.Builder, "nixpacks")
	}
}

func TestBuildConfigDefaultBuilder(t *testing.T) {
	cfg := BuildConfig{}
	if cfg.Builder != "" {
		t.Errorf("expected empty builder, got %q", cfg.Builder)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestBuildConfigBuilder|TestBuildConfigDefault" -count=1`
Expected: FAIL — `BuildConfig` has no `Builder` field

- [ ] **Step 3: Add the `Builder` field**

In `internal/types/types.go:42-45`, change from:

```go
type BuildConfig struct {
	Command string `mapstructure:"command"`
	Output  string `mapstructure:"output"`
}
```

To:

```go
type BuildConfig struct {
	Command string `mapstructure:"command"`
	Output  string `mapstructure:"output"`
	Builder string `mapstructure:"builder"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -v -run "TestBuildConfigBuilder|TestBuildConfigDefault" -count=1`
Expected: PASS

- [ ] **Step 5: Run all type tests to ensure no regressions**

Run: `go test ./internal/types/... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add Builder field to BuildConfig for backend selection"
```

---

### Task 2: Detection — Add `FrameworkNixpacks` constant and detection path

**Files:**
- Modify: `internal/builder/detect.go:12-20` (constant), `internal/builder/detect.go:30-78` (detection logic)

**Interfaces:**
- Consumes: `Framework` string type, `Detect(dir string) (*Detection, error)` function signature
- Produces: `FrameworkNixpacks Framework = "nixpacks"` constant; `Detect()` returns `FrameworkNixpacks` when `.nixpacks/config.toml` exists

- [ ] **Step 1: Write the failing tests**

Add to `internal/builder/builder_test.go`:

```go
func TestFrameworkNixpacksConstant(t *testing.T) {
	if FrameworkNixpacks != "nixpacks" {
		t.Errorf("FrameworkNixpacks = %q, want %q", FrameworkNixpacks, "nixpacks")
	}
}

func TestDetectNixpacks(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".nixpacks"), 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, ".nixpacks", "config.toml"), []byte(`[phases.setup]\nnixPkgs = ["openssl"]`), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkNixpacks {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNixpacks)
	}
	if d.InternalPort != 8080 {
		t.Errorf("InternalPort = %d, want 8080", d.InternalPort)
	}
}

func TestDetectDockerTakesPriorityOverNixpacks(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM node"), 0644)
	os.MkdirAll(filepath.Join(dir, ".nixpacks"), 0755)
	os.WriteFile(filepath.Join(dir, ".nixpacks", "config.toml"), []byte(""), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkDocker {
		t.Errorf("Framework = %q, want %q (Dockerfile takes priority)", d.Framework, FrameworkDocker)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/builder/... -v -run "TestFrameworkNixpacksConstant|TestDetectNixpacks|TestDetectDockerTakesPriority" -count=1`
Expected: FAIL — `FrameworkNixpacks` undefined

- [ ] **Step 3: Add the constant and detection logic**

In `internal/builder/detect.go:12-20`, add after `FrameworkDocker`:

```go
FrameworkNixpacks Framework = "nixpacks"
```

In `internal/builder/detect.go:30-78`, edit `Detect()` to insert a nixpacks check right after the Dockerfile check (after line 32, before the NextJS check on line 34):

```go
func Detect(dir string) (*Detection, error) {
	if hasFile(dir, "Dockerfile") {
		return &Detection{Framework: FrameworkDocker, InternalPort: 8080}, nil
	}
	if hasFile(filepath.Join(dir, ".nixpacks"), "config.toml") {
		return &Detection{Framework: FrameworkNixpacks, InternalPort: 8080}, nil
	}
	// ... rest unchanged
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/builder/... -v -run "TestFrameworkNixpacksConstant|TestDetectNixpacks|TestDetectDockerTakesPriority" -count=1`
Expected: PASS

- [ ] **Step 5: Run all existing detection tests to verify no regressions**

Run: `go test ./internal/builder/... -v -run "TestDetect" -count=1`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/builder/detect.go internal/builder/builder_test.go
git commit -m "feat: add FrameworkNixpacks and auto-detection via .nixpacks/config.toml"
```

---

### Task 3: Builder — Add `buildWithNixpacks()` method and wire dispatch in `Build()`

**Files:**
- Modify: `internal/builder/builder.go`

**Interfaces:**
- Consumes: `FrameworkNixpacks` constant (Task 2), `Detection` struct with `Framework` field
- Produces: `(*Builder).buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` — same signature as `buildWithDockerfile`
- Produces: `(*Builder).SetNixpacksConfig(*types.NixpacksConfig)` method (accepts `nil` to clear)
- Produces: Updated `Build()` method that dispatches to `buildWithNixpacks` when `detection.Framework == FrameworkNixpacks`

- [ ] **Step 1: Write the failing tests**

Add to `internal/builder/builder_test.go`:

```go
func TestBuildWithNixpacksDispatches(t *testing.T) {
	b := New(t.TempDir())
	dir := t.TempDir()
	detection := &Detection{Framework: FrameworkNixpacks, InternalPort: 8080}

	tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v1")
	if err != nil {
		if strings.Contains(err.Error(), "nixpacks not found") {
			t.Skip("nixpacks CLI not installed, skipping")
		}
		t.Fatalf("Build() error: %v\nlogs: %s", err, logs)
	}
	if tag == "" {
		t.Error("expected non-empty tag")
	}
	_ = logs
}

func TestSetNixpacksConfig(t *testing.T) {
	b := New(t.TempDir())
	cfg := &types.NixpacksConfig{Packages: []string{"curl"}}
	b.SetNixpacksConfig(cfg)
	if b.nixpacksCfg == nil {
		t.Fatal("expected nixpacksCfg to be set after SetNixpacksConfig")
	}
	if len(b.nixpacksCfg.Packages) != 1 || b.nixpacksCfg.Packages[0] != "curl" {
		t.Errorf("packages = %v, want [curl]", b.nixpacksCfg.Packages)
	}
}

func TestSetNixpacksConfigNil(t *testing.T) {
	b := New(t.TempDir())
	b.SetNixpacksConfig(nil)
	if b.nixpacksCfg != nil {
		t.Error("expected nixpacksCfg to be nil after SetNixpacksConfig(nil)")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/builder/... -v -run "TestBuildWithNixpacksDispatches|TestSetNixpacksConfig" -count=1`
Expected: FAIL — `buildWithNixpacks` method not defined, `nixpacksCfg` field missing, `NixpacksConfig` type not found

- [ ] **Step 3: Add the `NixpacksConfig` type and implement the methods**

In `internal/types/types.go`, after `BuildConfig` (line 45), add:

```go
type NixpacksConfig struct {
	Packages     []string `mapstructure:"packages,omitempty"`
	AptPackages  []string `mapstructure:"apt_packages,omitempty"`
	Cmd          string   `mapstructure:"cmd,omitempty"`
}
```

In `internal/builder/builder.go`, first add the imports needed at the top:

Add `"strings"` to the existing import block:

```go
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
```

Update the `Builder` struct:

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

Update the `Build()` method — change from the existing hardcoded Dockerfile dispatch to:

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

Add the `nixpacksAvailable` and `buildWithNixpacks` methods after `buildWithDockerfile`:

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

Run: `go test ./internal/builder/... -v -run "TestBuildWithNixpacksDispatches|TestSetNixpacksConfig" -count=1`
Expected: PASS (may skip `TestBuildWithNixpacksDispatches` if nixpacks CLI not installed; `TestSetNixpacksConfig` must pass unconditionally)

- [ ] **Step 5: Run all existing builder tests to verify no regressions**

Run: `go test ./internal/builder/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/types/types.go internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: add buildWithNixpacks method and dispatch in Builder.Build()"
```

---

### Task 4: Config — Merge `Build.Builder` in env-specific config loader

**Files:**
- Modify: `internal/config/config.go:104-109`

**Interfaces:**
- Consumes: extended `BuildConfig.Builder` from Task 1
- Produces: `cfg.Build.Builder` merged from env-specific `.tengiz.{env}.yaml`

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`, add:

```go
func TestLoadForEnvironmentMergesBuilderField(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
build:
  builder: docker
`), 0644)
	os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
build:
  builder: nixpacks
`), 0644)

	cfg, err := LoadForEnvironment(dir, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Build.Builder != "nixpacks" {
		t.Errorf("Build.Builder = %q, want %q", cfg.Build.Builder, "nixpacks")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesBuilderField" -count=1`
Expected: FAIL — `Build.Builder` not merged in `LoadForEnvironment`

- [ ] **Step 3: Add the merge logic**

In `internal/config/config.go`, after the `Build.Output` merge (line 109), add:

```go
	if envCfg.Build.Builder != "" {
		cfg.Build.Builder = envCfg.Build.Builder
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesBuilderField" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge Build.Builder in LoadForEnvironment"
```

---

### Task 5: CLI — Wire nixpacks config into deploy command and `devCmd`

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `cfg.Build.Builder`, `cfg.Build.NixpacksConfig` from config load, `builder.FrameworkNixpacks` constant
- Produces: Detection framework override when `cfg.Build.Builder == "nixpacks"`; nixpacks config passed to Builder; devCmd handles `FrameworkNixpacks`

- [ ] **Step 1: Write failing test for the deploy wiring**

This is integration-level (can't easily unit-test CLI). Verify via compilation + existing tests.

- [ ] **Step 2: Edit deploy command**

In `internal/cli/root.go`, after detection output (line 191 `fmt.Printf("[tengiz] detected: %s (port %d)\n"...`), add:

```go
	if cfg.Build.Builder == "nixpacks" {
		detection.Framework = builder.FrameworkNixpacks
		fmt.Printf("[tengiz] using nixpacks builder\n")
	}
```

After `b := builder.New(dataDir)` (line 199), add:

```go
	if cfg.Build.NixpacksConfig != nil {
		b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
	}
```

- [ ] **Step 3: Edit dev command**

In `internal/cli/root.go:566-577`, add to the switch statement (after `case builder.FrameworkDocker:` on line 573):

```go
		case builder.FrameworkNixpacks:
			return fmt.Errorf("dev mode not supported for nixpacks-based projects; use 'tengiz deploy' or run your dev server directly")
```

- [ ] **Step 4: Verify compilation**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire nixpacks config into deploy and dev CLI commands"
```

---

### Task 6: GitDeploy + Preview — Wire nixpacks config into pipelines

**Files:**
- Modify: `internal/gitdeploy/deployer.go`
- Modify: `internal/preview/manager.go`

**Interfaces:**
- Consumes: `existingApp.Config.Build.Builder`, `existingApp.Config.Build.NixpacksConfig` from stored app state
- Produces: Detection override + builder config for nixpacks in both pipelines

- [ ] **Step 1: Modify `gitdeploy/deployer.go`**

After line 101 (the block that copies config from existingApp), after the closing `}` of `if lookupErr == nil { ... }` (line 102), add:

```go
	if existingApp.Config.Build.Builder == "nixpacks" {
		detection.Framework = builder.FrameworkNixpacks
		if existingApp.Config.Build.NixpacksConfig != nil {
			p.b.SetNixpacksConfig(existingApp.Config.Build.NixpacksConfig)
		}
	}
```

- [ ] **Step 2: Modify `preview/manager.go`**

Add `nixpacksCfg` field to the `Manager` struct (line 18-23):

```go
type Manager struct {
	dataDir     string
	store       *config.Store
	rt          runtime.Manager
	builder     *builder.Builder
	nixpacksCfg *types.NixpacksConfig
}
```

Add `SetNixpacksConfig` method to `Manager`:

```go
func (m *Manager) SetNixpacksConfig(cfg *types.NixpacksConfig) {
	m.nixpacksCfg = cfg
	m.builder.SetNixpacksConfig(cfg)
}
```

In `Create()` (after line 64, after `detection, err := builder.Detect(cloneDir)`), add:

```go
	if m.nixpacksCfg != nil {
		detection.Framework = builder.FrameworkNixpacks
	}
```

In `Update()` (after line 143, after `detection, err := builder.Detect(cloneDir)`), add the same:

```go
	if m.nixpacksCfg != nil {
		detection.Framework = builder.FrameworkNixpacks
	}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/gitdeploy/... ./internal/preview/...`
Expected: exit 0

- [ ] **Step 4: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gitdeploy/deployer.go internal/preview/manager.go
git commit -m "feat: wire nixpacks config into gitdeploy and preview pipelines"
```

---

### Task 7: Final verification and docs

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Build binary**

Run: `go build -o tengiz .`
Expected: binary `tengiz` created successfully

- [ ] **Step 4: Verify the build output**

Run: `ls -la tengiz`
Expected: a non-empty executable binary

- [ ] **Step 5: Update AGENTS.md builder documentation**

Read `AGENTS.md` and add the nixpacks option to the `builder` package documentation section. Add:

```
| Nixpacks | `--builder nixpacks` or `.tengiz.yaml` `build.builder: nixpacks` | Rust, Ruby, PHP, Elixir, Java, Deno, Bun, 100+ more via `nixpacks build` CLI |
```

- [ ] **Step 6: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document nixpacks build backend in AGENTS.md"
```

---

## Self-Review

**1. Spec coverage:** The FUTURES_FEATURES.md entry for Nixpacks Build System (P0, #3) specifies:
- "Expands framework support from 6 to hundreds" — ✅ Task 2, 3 implement detection + build dispatch
- "Single `builder` package integration" — ✅ All builder logic in `internal/builder/`
- "`.tengiz.yaml`'da `--builder nixpacks` ile seçilebilir" — ✅ Task 1 adds `BuildConfig.Builder`, Task 4 merges it, Task 5 wires it
- "Ruby, Rust, PHP, Java, Elixir" — ✅ nixpacks handles all of these; Tengiz delegates entirely

**2. Placeholder scan:** No TODOs, TBDs, "implement later", or "add validation" patterns. Every code step has the exact code to write. No "similar to Task N" patterns — each step is self-contained. All method signatures match across tasks.

**3. Type consistency:**
- `buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` matches `buildWithDockerfile(ctx, dir, appName, env, deploymentID) (string, string, error)`
- `SetNixpacksConfig(*types.NixpacksConfig)` used consistently in all 3 callers
- `BuildConfig.Builder string` field name consistent across types, config merge, and CLI wiring
- `FrameworkNixpacks Framework = "nixpacks"` constant referenced by name everywhere
