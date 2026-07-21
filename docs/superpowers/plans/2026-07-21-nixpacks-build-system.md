# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend to support hundreds of frameworks (Rust, Ruby, PHP, Elixir, Deno, Bun, etc.) beyond the current 6.

**Architecture:** Nixpacks is invoked as a subprocess (`nixpacks build`) to produce a Docker image directly. Config in `.tengiz.yaml` (`build.builder: "nixpacks"`) selects the backend. New `buildWithNixpacks()` method on `Builder` handles execution. When nixpacks is selected, detection framework is overridden to `FrameworkNixpacks` which dispatches to `buildWithNixpacks()`. Existing callers (CLI deploy, gitdeploy pipeline, preview manager) pass the config through.

**Tech Stack:** `nixpacks` CLI (external dependency: `npm install -g nixpacks` or `brew install nixpacks`), Go `os/exec`, existing `internal/builder` package.

## Global Constraints

- All `os/exec` calls must capture both stdout and stderr as build logs (matching existing `buildWithDockerfile` behavior in `internal/builder/builder.go:46-50`)
- Image tags must follow existing convention: `tengiz-apps/{appName}:{env}-{deploymentID}`
- Nixpacks must NOT be a hard dependency — if `nixpacks` binary not in PATH when `build.builder: nixpacks` is set, return a clear error message
- Default behavior (no config) must remain unchanged — existing frameworks continue working identically
- `.tengiz.yaml` config structure: `build.builder` string field (`"docker"` or `"nixpacks"`), `build.nixpacks` nested config block with `packages`, `apt_packages`, `cmd`, `pkg_manager`, `app_directory`
- All existing tests must continue to pass

---

### Task 1: Types — Add Nixpacks config fields to `BuildConfig`

**Files:**
- Modify: `internal/types/types.go:42-45`
- Test: `internal/types/types.go` (test in same package or `types_test.go`)

**Interfaces:**
- Consumes: existing `BuildConfig` struct at `types.go:42`
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
            Packages: []string{"ffmpeg"},
            Cmd:      "node index.js",
        },
    }
    if cfg.Builder != "nixpacks" {
        t.Errorf("expected nixpacks, got %q", cfg.Builder)
    }
    if len(cfg.NixpacksConfig.Packages) != 1 || cfg.NixpacksConfig.Packages[0] != "ffmpeg" {
        t.Errorf("packages = %v, want [ffmpeg]", cfg.NixpacksConfig.Packages)
    }
    if cfg.NixpacksConfig.Cmd != "node index.js" {
        t.Errorf("cmd = %q, want %q", cfg.NixpacksConfig.Cmd, "node index.js")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run "TestNixpacksConfig" -count=1`
Expected: FAIL — `BuildConfig.Builder` and `BuildConfig.NixpacksConfig` don't exist, `NixpacksConfig` type not defined

- [ ] **Step 3: Extend `BuildConfig` and add `NixpacksConfig`**

Replace the existing `BuildConfig` struct at `internal/types/types.go:42-45`:

```go
type BuildConfig struct {
	Command        string           `mapstructure:"command"`
	Output         string           `mapstructure:"output"`
	Builder        string           `mapstructure:"builder"`
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

- [ ] **Step 5: Run all existing types tests**

Run: `go test ./internal/types/... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/types/types.go
git commit -m "feat: add Builder and NixpacksConfig fields to BuildConfig"
```

---

### Task 2: Detection — Add `FrameworkNixpacks` constant

**Files:**
- Modify: `internal/builder/detect.go:12-20`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `Framework` string type from `internal/builder/detect.go:10`
- Produces: `FrameworkNixpacks Framework = "nixpacks"` constant

- [ ] **Step 1: Write the failing test**

In `internal/builder/builder_test.go`:

```go
func TestFrameworkNixpacksConstant(t *testing.T) {
    if FrameworkNixpacks != "nixpacks" {
        t.Errorf("FrameworkNixpacks = %q, want %q", FrameworkNixpacks, "nixpacks")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run "TestFrameworkNixpacksConstant" -count=1`
Expected: FAIL — `FrameworkNixpacks` undefined

- [ ] **Step 3: Add the constant**

In `internal/builder/detect.go`, after `FrameworkDocker` on line 19:

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

### Task 3: Builder — Add `buildWithNixpacks()` method with dispatch

**Files:**
- Modify: `internal/builder/builder.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.NixpacksConfig` (Task 1), `FrameworkNixpacks` (Task 2)
- Produces: `(*Builder).buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` — same signature as `buildWithDockerfile`
- Produces: `(*Builder).SetNixpacksConfig(*types.NixpacksConfig)` setter
- Produces: `(*Builder).nixpacksCfg` unexported field

- [ ] **Step 1: Write the tests**

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

func TestBuildWithNixpacksSetsConfig(t *testing.T) {
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

func TestBuildWithNixpacksNoConfigIsNil(t *testing.T) {
    b := New(t.TempDir())
    if b.nixpacksCfg != nil {
        t.Error("expected nixpacksCfg to be nil initially")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run "TestBuildWithNixpacks" -count=1`
Expected: FAIL — `buildWithNixpacks` method not defined, `nixpacksCfg` field missing

- [ ] **Step 3: Implement `buildWithNixpacks`**

In `internal/builder/builder.go`, add `"os/exec"` and `"strings"` imports, then:

Modify the `Builder` struct after line 14:

```go
type Builder struct {
	dataDir     string
	nixpacksCfg *types.NixpacksConfig
}
```

Add after line 18 (`return &Builder{dataDir: dataDir}`):

```go
func (b *Builder) SetNixpacksConfig(cfg *types.NixpacksConfig) {
	b.nixpacksCfg = cfg
}
```

Replace the `Build` method (lines 21-29) and add helper + `buildWithNixpacks`:

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
			args = append(args, "--pkgs-manager", b.nixpacksCfg.PkgManager)
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/... -v -run "TestBuildWithNixpacks" -count=1`
Expected: PASS (may skip if nixpacks CLI not installed, but compile-check and setter tests pass)

- [ ] **Step 5: Run all builder tests**

Run: `go test ./internal/builder/... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: add buildWithNixpacks and dispatch in Builder"
```

---

### Task 4: Config — Merge `build.builder` and `build.nixpacks` in environment config

**Files:**
- Modify: `internal/config/config.go:100-134` (inside `LoadForEnvironment`)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: extended `BuildConfig` with `Builder` and `NixpacksConfig` fields (Task 1)
- Produces: `LoadForEnvironment` correctly merges `build.builder` and `build.nixpacks` from env-specific overrides

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

func TestLoadForEnvironmentPreservesDefaultBuilder(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
build:
  builder: docker
`), 0644)

	cfg, err := LoadForEnvironment(dir, "production")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Build.Builder != "docker" {
		t.Errorf("expected 'docker', got %q", cfg.Build.Builder)
	}
	if cfg.Build.NixpacksConfig != nil {
		t.Error("expected nil NixpacksConfig for production")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesBuilder|TestLoadForEnvironmentPreservesDefaultBuilder" -count=1`
Expected: FAIL — `Build.Builder` and `Build.NixpacksConfig` not merged in `LoadForEnvironment`

- [ ] **Step 3: Implement the merge in `LoadForEnvironment`**

In `internal/config/config.go`, after the volume merge block (after line 134), and before the env merge block (before line 136):

```go
	if envCfg.Build.Builder != "" {
		cfg.Build.Builder = envCfg.Build.Builder
	}
	if envCfg.Build.NixpacksConfig != nil {
		cfg.Build.NixpacksConfig = envCfg.Build.NixpacksConfig
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run "TestLoadForEnvironmentMergesBuilder|TestLoadForEnvironmentPreservesDefaultBuilder" -count=1`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: merge build.builder and build.nixpacks in LoadForEnvironment"
```

---

### Task 5: CLI deploy — Wire nixpacks config from `.tengiz.yaml` to Builder

**Files:**
- Modify: `internal/cli/root.go:187-201`

**Interfaces:**
- Consumes: `cfg.Build.Builder` from config load (line 173), `cfg.Build.NixpacksConfig`
- Produces: detection framework overridden to `FrameworkNixpacks` when `builder == "nixpacks"`, builder receives nixpacks config

- [ ] **Step 1: Understand the current deploy flow in `root.go:155-345`**

The deploy command:
1. Loads config via `config.LoadForEnvironment` (line 173)
2. Runs `builder.Detect` (line 187)
3. Creates builder via `builder.New(dataDir)` (line 199)
4. Calls `b.Build(ctx, ..., detection, ...)` (line 201)

We need to intercept between steps 2-3 to override detection framework when nixpacks is selected.

- [ ] **Step 2: Write the code changes in `root.go`**

After `fmt.Printf("[tengiz] detected: %s (port %d)\n", detection.Framework, detection.InternalPort)` on line 191, add:

```go
	if cfg.Build.Builder == "nixpacks" {
		detection.Framework = builder.FrameworkNixpacks
		fmt.Printf("[tengiz] using nixpacks builder for %s\n", detection.Framework)
	}
```

After `b := builder.New(dataDir)` on line 199, add:

```go
	if cfg.Build.NixpacksConfig != nil {
		b.SetNixpacksConfig(cfg.Build.NixpacksConfig)
	}
```

- [ ] **Step 3: Verify the change compiles**

Run: `go build ./internal/cli/...`
Expected: exit 0

- [ ] **Step 4: Run full build**

Run: `go build -o /dev/null .`
Expected: exit 0

- [ ] **Step 5: Run existing tests**

Run: `go test ./internal/cli/... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: wire nixpacks config from .tengiz.yaml into deploy command"
```

---

### Task 6: GitDeploy — Wire nixpacks config from stored app config

**Files:**
- Modify: `internal/gitdeploy/deployer.go:73-102`

**Interfaces:**
- Consumes: `existingApp.Config.Build.Builder` and `existingApp.Config.Build.NixpacksConfig` from stored app entry (lines 93-102)
- Produces: detection framework override + builder config for nixpacks when appropriate

- [ ] **Step 1: Modify `deployer.go` to pass nixpacks config to builder**

After the existing app config merge block at line 102 (after `}` of the `if lookupErr == nil` block, before `deploymentID` assignment on line 104), add:

```go
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

- [ ] **Step 3: Run gitdeploy tests**

Run: `go test ./internal/gitdeploy/... -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "feat: wire nixpacks config in gitdeploy pipeline"
```

---

### Task 7: Preview — Wire nixpacks config to preview Manager

**Files:**
- Modify: `internal/preview/manager.go:18-32` (struct + constructor)

**Interfaces:**
- Consumes: none from config (preview creates config from scratch)
- Produces: `Manager` with nixpacks config setter, `Create` and `Update` methods override detection for nixpacks

- [ ] **Step 1: Modify the `Manager` struct and constructor**

Replace the `Manager` struct at line 18-23:

```go
type Manager struct {
	dataDir     string
	store       *config.Store
	rt          runtime.Manager
	builder     *builder.Builder
	nixpacksCfg *types.NixpacksConfig
}
```

Add setter after `NewManager` (after line 32):

```go
func (m *Manager) SetNixpacksConfig(cfg *types.NixpacksConfig) {
	m.nixpacksCfg = cfg
	m.builder.SetNixpacksConfig(cfg)
}
```

- [ ] **Step 2: Override detection in `Create` and `Update`**

In `Create` (after line 64 `detection, err := builder.Detect(cloneDir)`), add:

```go
	if m.nixpacksCfg != nil {
		detection.Framework = builder.FrameworkNixpacks
	}
```

In `Update` (after line 143 `detection, err := builder.Detect(cloneDir)`), add:

```go
	if m.nixpacksCfg != nil {
		detection.Framework = builder.FrameworkNixpacks
	}
```

- [ ] **Step 3: Compile-check preview changes**

Run: `go build ./internal/preview/...`
Expected: exit 0

- [ ] **Step 4: Run preview tests**

Run: `go test ./internal/preview/... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/preview/manager.go
git commit -m "feat: wire nixpacks config into preview manager"
```

---

### Task 8: Full verification and docs

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS (tests that require Docker or nixpacks CLI may be skipped)

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: exit 0

- [ ] **Step 3: Verify build**

Run: `go build -o tengiz .`
Expected: binary created successfully

- [ ] **Step 4: Update `AGENTS.md` with nixpacks info**

Add to the builder section in `AGENTS.md`:

```
- `nixpacks` CLI required for `build.builder: nixpacks` (install: `npm install -g nixpacks`)
- Config: `.tengiz.yaml` → `build.builder: nixpacks`, `build.nixpacks.packages`, `build.nixpacks.apt_packages`, etc.
- Dispatch: `detection.Framework == FrameworkNixpacks` → `buildWithNixpacks()` produces image via `nixpacks build`
```

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document nixpacks build backend in AGENTS.md"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1 covers `BuildConfig.Builder` + `NixpacksConfig` types and their mapstructure tags
- Task 2 covers `FrameworkNixpacks` constant
- Task 3 covers `buildWithNixpacks()` method, `nixpacksAvailable()` check, config passing, and `nixpacksCfg` field
- Task 4 covers `LoadForEnvironment` merge for `build.builder` and `build.nixpacks`
- Task 5 covers CLI deploy command wiring
- Task 6 covers gitdeploy pipeline wiring
- Task 7 covers preview manager wiring
- Task 8 covers full test suite, vet, build, and docs

**2. Placeholder scan:** No TODOs, TBDs, "add later", "fill in details", "add error handling" without code, or "similar to..." patterns. Every code step has actual compilable Go code.

**3. Type consistency:** `buildWithNixpacks` returns `(string, string, error)` matching `buildWithDockerfile`. `SetNixpacksConfig` name is consistent across Builder, Pipeline (via builder field), and Manager. `FrameworkNixpacks` is `"nixpacks"` everywhere. All mapstructure tags use same casing convention as existing fields. `NixpacksConfig.AptPackages` matches existing `mapstructure:"apt_packages"` pattern.
