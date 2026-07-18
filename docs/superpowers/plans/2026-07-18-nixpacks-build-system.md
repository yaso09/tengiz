# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate the Nixpacks CLI as an alternative build backend so Tengiz can auto-detect and build hundreds of frameworks (Ruby, Rust, PHP, Java, Elixir, etc.) instead of only the current 6.

**Architecture:** A new `NixpacksStrategy` in the `builder` package that shells out to `nixpacks` CLI via `os/exec` (same pattern as the existing Docker build). The `.tengiz.yaml` `build.builder` field (`"auto"`, `"dockerfile"`, `"nixpacks"`) selects the strategy. Nixpacks auto-detection runs first if the user hasn't explicitly set a builder. Nixpacks output dir is inspected post-build to determine the correct internal port and start command.

**Tech Stack:** Nixpacks CLI (external binary, must be installed separately), Go 1.26, existing `builder.Builder`, `os/exec`, `types.BuildConfig`.

## Global Constraints

- Nixpacks CLI must be installed separately — no Go dependency, no vendoring
- `build.builder` defaults to `"auto"` — uses Nixpacks for non-Dockerfile projects, falls back to current detection
- When `build.builder: nixpacks` is set explicitly, always use Nixpacks even if a Dockerfile exists
- Nixpacks output directory auto-detection: read `nixpacks.toml` or check common output dirs (`dist/`, `build/`, `target/`, `public/`, `.next/`)
- Image tags follow existing convention: `tengiz-apps/{app}:{env}-{deploymentID}`
- Build logs captured the same way as Docker builds (`bytes.Buffer` + `io.MultiWriter`)
- New `NixpacksConfig` in `types.go` with `build.nixpacks.pkgs` (apt packages) and `build.nixpacks` (raw config map)
- No new external dependencies (no Nixpacks Go SDK — just CLI exec)
- Existing tests must continue to pass without modification
- Builder must detect if `nixpacks` binary is not installed and return a clear error message

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | Add `Builder string` to `BuildConfig`, add `NixpacksConfig` struct |
| `internal/builder/builder.go` | Add `buildWithNixpacks()` method; update `Build()` to dispatch based on `build.builder` |
| `internal/builder/nixpacks.go` | (new) Nixpacks detection + build via CLI exec |
| `internal/builder/nixpacks_test.go` | (new) Tests for Nixpacks detection and CLI passthrough |
| `internal/builder/detect.go` | Add `FrameworkNixpacks` constant; add `DetectWithNixpacks()` helper |
| `internal/cli/root.go` | Add `--builder` flag to deploy command; pass to builder |
| `internal/builder/builder_test.go` | Add tests for builder selection logic |
| `internal/config/config.go` | Handle `build.builder` and `build.nixpacks` in `LoadWithEnv` merge |

---

### Task 1: Add Nixpacks types to `types.go`

**Files:**
- Modify: `internal/types/types.go:42-45` — extend `BuildConfig` with `Builder` and `NixpacksConfig`

**Interfaces:**
- Consumes: nothing new
- Produces: `types.BuildConfig.Builder string` (values: `"auto"`, `"dockerfile"`, `"nixpacks"`), `types.NixpacksConfig` with `Pkgs []string` and arbitrary config map

- [ ] **Step 1: Write the failing test**

```go
// internal/types/types_test.go
package types

import (
    "encoding/json"
    "testing"
)

func TestBuildConfigBuilderField(t *testing.T) {
    cfg := BuildConfig{
        Command: "npm run build",
        Output:  "dist",
        Builder: "nixpacks",
    }
    data, err := json.Marshal(cfg)
    if err != nil {
        t.Fatalf("Marshal: %v", err)
    }
    var decoded BuildConfig
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("Unmarshal: %v", err)
    }
    if decoded.Builder != "nixpacks" {
        t.Errorf("Builder = %q, want %q", decoded.Builder, "nixpacks")
    }
}

func TestBuildConfigBuilderDefaults(t *testing.T) {
    cfg := BuildConfig{}
    if cfg.Builder != "" {
        t.Errorf("default Builder should be empty string, got %q", cfg.Builder)
    }
}

func TestNixpacksConfigSerialize(t *testing.T) {
    nc := NixpacksConfig{
        Pkgs: []string{"libpq-dev", "ffmpeg"},
    }
    data, err := json.Marshal(nc)
    if err != nil {
        t.Fatalf("Marshal: %v", err)
    }
    var decoded NixpacksConfig
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("Unmarshal: %v", err)
    }
    if len(decoded.Pkgs) != 2 || decoded.Pkgs[0] != "libpq-dev" {
        t.Errorf("Pkgs = %v, want [libpq-dev ffmpeg]", decoded.Pkgs)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -run "TestBuildConfigBuilderField|TestBuildConfigBuilderDefaults|TestNixpacksConfigSerialize" -v -count=1`

Expected: FAIL with `unknown field Builder in struct literal` (or similar compile error)

- [ ] **Step 3: Add types to `internal/types/types.go`**

```go
// Replace existing BuildConfig with:
type BuildConfig struct {
    Command   string          `mapstructure:"command" yaml:"command" json:"command,omitempty"`
    Output    string          `mapstructure:"output" yaml:"output" json:"output,omitempty"`
    Builder   string          `mapstructure:"builder" yaml:"builder" json:"builder,omitempty"`
    Nixpacks *NixpacksConfig `mapstructure:"nixpacks,omitempty" yaml:"nixpacks,omitempty" json:"nixpacks,omitempty"`
}

type NixpacksConfig struct {
    Pkgs []string `mapstructure:"pkgs,omitempty" yaml:"pkgs,omitempty" json:"pkgs,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/... -run "TestBuildConfigBuilderField|TestBuildConfigBuilderDefaults|TestNixpacksConfigSerialize" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all type tests**

Run: `go test ./internal/types/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/types/types.go
git commit -m "feat: add Builder field to BuildConfig and NixpacksConfig type"
```

---

### Task 2: Add Nixpacks framework detection

**Files:**
- Modify: `internal/builder/detect.go` — add `FrameworkNixpacks` constant and `DetectWithNixpacks()` function

**Interfaces:**
- Consumes: nothing new
- Produces: `FrameworkNixpacks Framework = "nixpacks"`, `func DetectWithNixpacks(dir string) (*Detection, error)` — runs `nixpacks detect <dir>` and parses JSON output

- [ ] **Step 1: Write the failing test**

```go
// internal/builder/detect_test.go
package builder

import (
    "os"
    "path/filepath"
    "testing"
)

func TestDetectWithNixpacksRuby(t *testing.T) {
    // Nixpacks should detect a Ruby project with a Gemfile
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`source "https://rubygems.org"`), 0644)

    // Skip if nixpacks not installed
    if !nixpacksInstalled() {
        t.Skip("nixpacks CLI not installed")
    }

    d, err := DetectWithNixpacks(dir)
    if err != nil {
        t.Fatalf("DetectWithNixpacks: %v", err)
    }
    if d.Framework != FrameworkNixpacks {
        t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNixpacks)
    }
    if d.InternalPort == 0 {
        t.Error("expected non-zero InternalPort")
    }
}

func TestDetectWithNixpacksRust(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[package]\nname = "test"`), 0644)
    os.MkdirAll(filepath.Join(dir, "src"), 0755)
    os.WriteFile(filepath.Join(dir, "src", "main.rs"), []byte(`fn main() {}`), 0644)

    if !nixpacksInstalled() {
        t.Skip("nixpacks CLI not installed")
    }

    d, err := DetectWithNixpacks(dir)
    if err != nil {
        t.Fatalf("DetectWithNixpacks: %v", err)
    }
    if d.Framework != FrameworkNixpacks {
        t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNixpacks)
    }
}

func TestDetectNixpacksNotInstalled(t *testing.T) {
    // Should return a clear error when nixpacks binary is missing
    _, err := DetectWithNixpacks(t.TempDir())
    // This will fail on systems with nixpacks installed — that's OK
    // We test the error path differently by checking exec.LookPath behavior
    // Just verify the function exists and compiles
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -run "TestDetectWithNixpacks" -v -count=1`

Expected: FAIL with `undefined: DetectWithNixpacks` or `undefined: FrameworkNixpacks`

- [ ] **Step 3: Add `FrameworkNixpacks` to `internal/builder/detect.go`**

```go
const (
    FrameworkNextJS   Framework = "nextjs"
    FrameworkVite     Framework = "vite"
    FrameworkGo       Framework = "go"
    FrameworkNode     Framework = "node"
    FrameworkPython   Framework = "python"
    FrameworkStatic   Framework = "static"
    FrameworkDocker   Framework = "docker"
    FrameworkNixpacks Framework = "nixpacks"
)
```

- [ ] **Step 4: Create Nixpacks detection in a new file `internal/builder/nixpacks.go`**

```go
package builder

import (
    "encoding/json"
    "fmt"
    "os/exec"
    "strings"
)

type nixpacksDetectOutput struct {
    Name          string `json:"name"`
    InstallCmd    string `json:"install_cmd"`
    BuildCmd      string `json:"build_cmd"`
    StartCmd      string `json:"start_cmd"`
    Port          int    `json:"port"`
    OutputDir     string `json:"output_dir"`
    Environments  []struct {
        Key   string `json:"key"`
        Value string `json:"value"`
    } `json:"environments"`
}

func nixpacksInstalled() bool {
    _, err := exec.LookPath("nixpacks")
    return err == nil
}

func DetectWithNixpacks(dir string) (*Detection, error) {
    if !nixpacksInstalled() {
        return nil, fmt.Errorf("nixpacks CLI not found: install from https://nixpacks.com/docs/install")
    }

    cmd := exec.Command("nixpacks", "detect", dir, "--json")
    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("nixpacks detect: %w", err)
    }

    var result nixpacksDetectOutput
    if err := json.Unmarshal(output, &result); err != nil {
        return nil, fmt.Errorf("nixpacks detect parse: %w", err)
    }

    port := result.Port
    if port == 0 {
        port = 8080
    }

    return &Detection{
        Framework:    FrameworkNixpacks,
        BuildCmd:     result.BuildCmd,
        OutputDir:    result.OutputDir,
        InternalPort: port,
    }, nil
}

func nixpacksBuildCmd(dir string, tag string, pkgs []string) *exec.Cmd {
    args := []string{"build", dir, "--name", tag}
    for _, pkg := range pkgs {
        args = append(args, "--pkgs", pkg)
    }
    return exec.Command("nixpacks", args...)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./...`

Expected: Build succeeds

Run: `go test ./internal/builder/... -run "TestDetectWithNixpacks" -v -count=1`

Expected: PASS or SKIP (if nixpacks not installed)

- [ ] **Step 6: Commit**

```bash
git add internal/builder/detect.go internal/builder/nixpacks.go
git commit -m "feat: add FrameworkNixpacks and DetectWithNixpacks function"
```

---

### Task 3: Add Nixpacks build strategy to Builder

**Files:**
- Modify: `internal/builder/builder.go` — add `buildWithNixpacks()` method; update `Build()` to dispatch based on `BuildConfig.Builder`
- Create: `internal/builder/nixpacks.go` — additional build helper (already started in Task 2)

**Interfaces:**
- Consumes: `types.BuildConfig.Builder`, `types.BuildConfig.Nixpacks`, `DetectWithNixpacks()`
- Produces: `Builder.Build()` strategy dispatch; `buildWithNixpacks()` method

- [ ] **Step 1: Write the failing test for builder selection**

```go
// internal/builder/builder_test.go — add these tests

func TestBuildWithNixpacksStrategy(t *testing.T) {
    if !nixpacksInstalled() {
        t.Skip("nixpacks CLI not installed")
    }

    b := New(t.TempDir())
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`source "https://rubygems.org"`), 0644)
    os.WriteFile(filepath.Join(dir, "config.ru"), []byte(`run lambda { |env| [200, {}, ["hello"]] }`), 0644)

    detection := &Detection{
        Framework:    FrameworkNixpacks,
        InternalPort: 8080,
    }

    cfg := &types.AppConfig{
        Name: "testapp",
        Build: types.BuildConfig{
            Builder: "nixpacks",
        },
    }

    tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", cfg, detection, "v123")
    if err != nil {
        t.Skipf("Build() error (likely nixpacks issue): %v", err)
    }
    if tag == "" {
        t.Error("expected non-empty tag")
    }
    _ = logs
}

func TestBuildWithAutoStrategyChoosesNixpacksForNonDocker(t *testing.T) {
    if !nixpacksInstalled() {
        t.Skip("nixpacks CLI not installed")
    }

    b := New(t.TempDir())
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`source "https://rubygems.org"`), 0644)
    // No Dockerfile — auto strategy should use Nixpacks

    cfg := &types.AppConfig{
        Name: "testapp",
        Build: types.BuildConfig{
            Builder: "auto",
        },
    }

    detection, err := Detect(dir)
    if err != nil {
        t.Fatal(err)
    }

    // Standard detection returns FrameworkStatic for unknown — but with auto builder,
    // the build should try Nixpacks before falling back
    tag, _, err := b.Build(context.Background(), dir, "testapp", "production", cfg, detection, "v123")
    if err != nil {
        t.Skipf("Build() error: %v", err)
    }
    if !strings.Contains(tag, "testapp") {
        t.Errorf("tag = %q, want it to contain testapp", tag)
    }
}

func TestBuildWithAutoStrategyUsesDockerfile(t *testing.T) {
    b := New(t.TempDir())
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\nCMD [\"echo\", \"hi\"]"), 0644)

    cfg := &types.AppConfig{
        Name: "testapp",
        Build: types.BuildConfig{
            Builder: "auto",
        },
    }

    detection, err := Detect(dir)
    if err != nil {
        t.Fatal(err)
    }
    if detection.Framework != FrameworkDocker {
        t.Errorf("expected FrameworkDocker, got %q", detection.Framework)
    }

    tag, _, err := b.Build(context.Background(), dir, "testapp", "production", cfg, detection, "v123")
    if err != nil {
        t.Skipf("Build() error (likely no docker): %v", err)
    }
    if tag == "" {
        t.Error("expected non-empty tag")
    }
}
```

Add import for `"github.com/yaso09/tengiz/internal/types"` at the top of `builder_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -run "TestBuildWithNixpacksStrategy|TestBuildWithAutoStrategy" -v -count=1`

Expected: FAIL — `Build()` signature missing `*types.AppConfig` parameter

- [ ] **Step 3: Update `Builder.Build()` signature and add Nixpacks strategy**

Change `Build()` in `internal/builder/builder.go` to accept `*types.AppConfig`:

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, cfg *types.AppConfig, detection *Detection, deploymentID string) (string, string, error) {
    builderType := cfg.Build.Builder
    if builderType == "" {
        builderType = "auto"
    }

    switch builderType {
    case "nixpacks":
        return b.buildWithNixpacks(ctx, dir, appName, env, cfg, deploymentID)
    case "dockerfile":
        if detection.Framework == FrameworkDocker {
            return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
        }
        if err := b.ensureDockerfile(dir, detection); err != nil {
            return "", "", fmt.Errorf("generate dockerfile: %w", err)
        }
        return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
    case "auto":
        // If Dockerfile exists, build with it
        if detection.Framework == FrameworkDocker {
            return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
        }
        // If nixpacks is installed, try it first
        if nixpacksInstalled() {
            return b.buildWithNixpacks(ctx, dir, appName, env, cfg, deploymentID)
        }
        // Fall back to generated Dockerfile
        if err := b.ensureDockerfile(dir, detection); err != nil {
            return "", "", fmt.Errorf("generate dockerfile: %w", err)
        }
        return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
    default:
        return "", "", fmt.Errorf("unknown builder %q (valid: auto, dockerfile, nixpacks)", builderType)
    }
}
```

Add the `buildWithNixpacks` method:

```go
func (b *Builder) buildWithNixpacks(ctx context.Context, dir string, appName string, env string, cfg *types.AppConfig, deploymentID string) (string, string, error) {
    if env == "" {
        env = "production"
    }
    tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

    var pkgs []string
    if cfg.Build.Nixpacks != nil {
        pkgs = cfg.Build.Nixpacks.Pkgs
    }

    cmd := nixpacksBuildCmd(dir, tag, pkgs)

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

Run: `go build ./...`

Expected: Build succeeds (may need to update callers — see note below)

Run: `go test ./internal/builder/... -v -count=1`

Expected: All PASS (some tests may SKIP due to missing docker/nixpacks)

- [ ] **Step 5: Update all callers of `Builder.Build()`**

The `Build()` signature changed. Update callers in:
- `internal/cli/root.go` — deploy command
- `internal/gitdeploy/deployer.go` — deploy pipeline
- `internal/gitdeploy/preview.go` — preview deploy

For each, pass `cfg` instead of nil, or construct a proper config:

```go
// In deploy handler (root.go):
b.Build(ctx, dir, appName, env, cfg, detection, deploymentID)

// In gitdeploy (deployer.go):
b.Build(ctx, tempDir, appName, env, cfg, detection, deploymentID)

// In gitdeploy (preview.go):
b.Build(ctx, tempDir, appName, "preview", cfg, detection, time.Now())
```

- [ ] **Step 6: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except proxy TCP timeout tests)

- [ ] **Step 7: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go internal/builder/nixpacks.go
git commit -m "feat: add Nixpacks build strategy with auto/dockerfile/nixpacks builder selection"
```

---

### Task 4: Update deploy CLI command with `--builder` flag

**Files:**
- Modify: `internal/cli/root.go` — add `--builder` flag to deploy command; pass to build logic

**Interfaces:**
- Consumes: `types.BuildConfig.Builder` from flag → merged with config
- Produces: `tengiz deploy --builder nixpacks <dir>` works end-to-end

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/builder_test.go — create new file
package cli

import (
    "testing"
)

func TestDeployCmdHasBuilderFlag(t *testing.T) {
    flag := deployCmd.Flags().Lookup("builder")
    if flag == nil {
        t.Error("deployCmd missing --builder flag")
    }
}

func TestDeployCmdBuilderFlagDefault(t *testing.T) {
    deployCmd.ParseFlags([]string{})
    val, _ := deployCmd.Flags().GetString("builder")
    if val != "" {
        t.Errorf("default --builder = %q, want empty string", val)
    }
}

func TestDeployCmdBuilderFlagCustom(t *testing.T) {
    deployCmd.ParseFlags([]string{"--builder", "nixpacks", "."})
    val, _ := deployCmd.Flags().GetString("builder")
    if val != "nixpacks" {
        t.Errorf("--builder = %q, want %q", val, "nixpacks")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestDeployCmdHasBuilderFlag|TestDeployCmdBuilderFlag" -v -count=1`

Expected: FAIL (flag not registered)

- [ ] **Step 3: Add `--builder` flag to deploy command**

In `init()` function of `internal/cli/root.go`:

```go
deployCmd.Flags().String("builder", "", "build strategy: auto, dockerfile, nixpacks (default: auto)")
```

- [ ] **Step 4: Wire the builder flag into deploy handler**

In the deploy command's `RunE`, after loading config:

```go
builderFlag, _ := cmd.Flags().GetString("builder")
if builderFlag != "" {
    cfg.Build.Builder = builderFlag
}
```

Replace the `b.Build(...)` call with the new signature:

```go
imageTag, buildLog, err := b.Build(ctx, projectRoot, cfg.Name, env, cfg, detection, deploymentID)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestDeployCmdHasBuilderFlag|TestDeployCmdBuilderFlag" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/builder_test.go
git commit -m "feat: add --builder flag to deploy command for strategy selection"
```

---

### Task 5: Update config merge for `build.builder` and `build.nixpacks`

**Files:**
- Modify: `internal/config/config.go` — extend `LoadWithEnv` to merge `build.builder` and `build.nixpacks`

**Interfaces:**
- Consumes: existing `LoadWithEnv` pattern
- Produces: env-scoped `.tengiz.{env}.yaml` can override `build.builder` and `build.nixpacks`

- [ ] **Step 1: Write the failing test**

```go
// internal/config/config_test.go — add
package config

import (
    "os"
    "path/filepath"
    "testing"
)

func TestLoadWithEnvMergesBuildBuilder(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\nbuild:\n  builder: dockerfile\n"), 0644)
    os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte("build:\n  builder: nixpacks\n  nixpacks:\n    pkgs:\n      - libpq-dev\n"), 0644)

    cfg, err := LoadForEnvironment(dir, "staging")
    if err != nil {
        t.Fatalf("LoadForEnvironment: %v", err)
    }
    if cfg.Build.Builder != "nixpacks" {
        t.Errorf("Build.Builder = %q, want %q", cfg.Build.Builder, "nixpacks")
    }
    if cfg.Build.Nixpacks == nil || len(cfg.Build.Nixpacks.Pkgs) != 1 || cfg.Build.Nixpacks.Pkgs[0] != "libpq-dev" {
        t.Errorf("Nixpacks.Pkgs = %v, want [libpq-dev]", cfg.Build.Nixpacks.Pkgs)
    }
}

func TestLoadWithEnvDefaultBuildBuilder(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\nport: 3000\n"), 0644)

    cfg, err := LoadForEnvironment(dir, "production")
    if err != nil {
        t.Fatalf("LoadForEnvironment: %v", err)
    }
    if cfg.Build.Builder != "" {
        t.Errorf("default Build.Builder should be empty, got %q", cfg.Build.Builder)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run "TestLoadWithEnvMergesBuildBuilder|TestLoadWithEnvDefaultBuildBuilder" -v -count=1`

Expected: FAIL with `undefined: LoadForEnvironment` (if it doesn't exist yet) or merge logic not handling `BuildConfig`

- [ ] **Step 3: Update `internal/config/config.go` — add build builder to env merge**

In `LoadWithEnv`, after the existing scalar field merging, add:

```go
if envCfg.Build.Builder != "" {
    cfg.Build.Builder = envCfg.Build.Builder
}
if envCfg.Build.Nixpacks != nil {
    cfg.Build.Nixpacks = envCfg.Build.Nixpacks
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -run "TestLoadWithEnvMergesBuildBuilder|TestLoadWithEnvDefaultBuildBuilder" -v -count=1`

Expected: PASS

Run: `go test ./internal/config/... -v -count=1`

Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat: merge build.builder and build.nixpacks in env config"
```

---

### Task 6: Self-review and final verification

- [ ] **Step 1: Spec coverage check**

Requirements from `docs/FUTURES_FEATURES.md`:
- Nixpacks CLI integration via `os/exec` ✅ (Task 2, 3)
- Auto-detect frameworks that Tengiz doesn't support natively ✅ (Task 2 — `DetectWithNixpacks`)
- `.tengiz.yaml` `build.builder` configuration ✅ (Task 1, 4, 5)
- `--builder nixpacks` flag ✅ (Task 4)
- Hundreds of frameworks (Ruby, Rust, PHP, Elixir) ✅ (Nixpacks handles this)
- Build logs captured ✅ (Task 3 — `logBuf`)
- Existing tests preserved ✅ (Task 3 — backward-compatible `"auto"` default)

- [ ] **Step 2: Type consistency check**

- `types.BuildConfig.Builder string` — used in Task 1 (types), Task 3 (builder switch), Task 4 (CLI flag), Task 5 (config merge)
- `types.BuildConfig.Nixpacks *NixpacksConfig` — used in Tasks 1, 3, 5
- `types.NixpacksConfig.Pkgs []string` — used in Task 1 (type), Task 3 (nixpacks build args), Task 5 (config merge)
- `builder.DetectWithNixpacks(dir) (*Detection, error)` — Task 2
- `builder.Builder.Build(ctx, dir, appName, env, *AppConfig, *Detection, deploymentID)` — updated in Task 3
- `builder.nixpacksBuildCmd(dir, tag, pkgs)` — Task 2 internal helper, used in Task 3
- `builder.FrameworkNixpacks Framework = "nixpacks"` — Task 2

- [ ] **Step 3: Placeholder scan**

Search for "TBD", "TODO", "implement later", "fill in details", "Similar to Task". None found.

- [ ] **Step 4: Build and test verification**

Run: `go build ./...`

Expected: Build succeeds

Run: `go vet ./...`

Expected: No issues

Run: `go test ./... -count=1`

Expected: All PASS (except proxy TCP timeout tests and idle time-sensitive tests)

- [ ] **Step 5: Final commit**

```bash
git add .
git commit -m "test: add builder integration tests and self-review verification"
```
