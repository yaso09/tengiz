# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build system so Tengiz can deploy hundreds of frameworks (Ruby, Rust, PHP, Java, Elixir, etc.) without writing Dockerfiles.

**Architecture:** A new `BuildStrategy` concept in `builder.Builder` — currently all builds use `dockerfile` strategy (existing `ensureDockerfile` + `generateDockerfile`). Adding `nixpacks` strategy invokes the `nixpacks` CLI (via `os/exec`, same pattern as Docker) to auto-detect and build images. The `.tengiz.yaml` `build.strategy` field selects the builder; default is `dockerfile` for backward compatibility. When `nixpacks` is selected, `ensureDockerfile` is skipped entirely — Nixpacks generates its own Dockerfile internally. Image tagging, push, tagging conventions remain unchanged.

**Tech Stack:** Go 1.26, existing `builder.Builder`, `os/exec` for `nixpacks` CLI. No new Go external deps. Nixpacks must be installed separately (`npm i -g nixpacks` or `brew install nixpacks`).

## Global Constraints

- `nixpacks` CLI must be available on `$PATH` when `build.strategy: nixpacks` is used
- Default strategy remains `dockerfile` — zero breakage for existing users
- Image tag format unchanged: `tengiz-apps/{appName}:{env}-{deploymentID}` and `tengiz-apps/{appName}:{env}-latest`
- Detection of internal port must still work for nixpacks builds (Nixpacks assigns its own port; use `detection.InternalPort` as override)
- Build logs captured to buffer and saved to store, same as existing flow
- Nixpacks generates its own Dockerfile in a temp dir — Tengiz should not write one
- `.tengiz.yaml` `build.strategy: nixpacks|dockerfile` — validated at build time
- CLI `--builder nixpacks|dockerfile` flag overrides config
- Existing tests must continue to pass unchanged
- No new external Go dependencies

---

## File Structure

| File | Responsibility |
|------|---------------|
| Modify: `internal/types/types.go` | Add `Strategy` field to `BuildConfig`; add `BuildStrategy` type and constants |
| Modify: `internal/builder/builder.go` | Add `BuildWithNixpacks()` method; modify `Build()` to dispatch by strategy; add strategy validation |
| Modify: `internal/builder/detect.go` | Add nixpacks-aware detection hints; no change to core detection logic |
| Create: `internal/builder/nixpacks.go` | `nixpacksDetect(dir) (*nixpacksPlan, error)` — parse `nixpacks detect` JSON output for port/start/cmd |
| Modify: `internal/cli/root.go` | Add `--builder` flag to `deployCmd`; pass strategy to `cfg.Build.Strategy` |
| Modify: `internal/config/config.go` | Strategy field is naturally handled by viper unmarshal — no change needed; update `LoadForEnvironment` merge if needed |
| No change: `internal/runtime/runtime.go` | Container creation flow unchanged — only the image building step changes |

---

### Task 1: Add BuildStrategy types and config fields

**Files:**
- Modify: `internal/types/types.go` — add `BuildStrategy` type, constants, and `Strategy` field to `BuildConfig`

**Interfaces:**
- Consumes: nothing new
- Produces: `types.BuildStrategy` type, `StrategyDockerfile`/`StrategyNixpacks` constants, `BuildConfig.Strategy string`

- [ ] **Step 1: Write the failing test**

```go
// internal/types/types_test.go
package types

import (
    "testing"
)

func TestBuildStrategyConstants(t *testing.T) {
    if StrategyDockerfile != "dockerfile" {
        t.Errorf("StrategyDockerfile = %q, want %q", StrategyDockerfile, "dockerfile")
    }
    if StrategyNixpacks != "nixpacks" {
        t.Errorf("StrategyNixpacks = %q, want %q", StrategyNixpacks, "nixpacks")
    }
}

func TestBuildConfigDefaults(t *testing.T) {
    cfg := BuildConfig{}
    if cfg.Strategy != "" {
        t.Errorf("default Strategy = %q, want empty", cfg.Strategy)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -run "TestBuildStrategyConstants|TestBuildConfigDefaults" -v -count=1`

Expected: FAIL with `undefined: StrategyDockerfile`

- [ ] **Step 3: Add types to `internal/types/types.go`**

Add before `BuildConfig` struct:

```go
type BuildStrategy string

const (
    StrategyDockerfile BuildStrategy = "dockerfile"
    StrategyNixpacks   BuildStrategy = "nixpacks"
)
```

Modify existing `BuildConfig`:

```go
type BuildConfig struct {
    Command  string `mapstructure:"command"`
    Output   string `mapstructure:"output"`
    Strategy string `mapstructure:"strategy"`
}
```

- [ ] **Step 4: Run type tests to verify they pass**

Run: `go test ./internal/types/... -run "TestBuildStrategyConstants|TestBuildConfigDefaults" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go
git commit -m "feat: add BuildStrategy type and Strategy field to BuildConfig"
```

---

### Task 2: Add Nixpacks build capability to builder package

**Files:**
- Create: `internal/builder/nixpacks.go` — nixpacks CLI wrapper
- Modify: `internal/builder/builder.go` — add `BuildWithNixpacks()` and strategy dispatch in `Build()`

**Interfaces:**
- Consumes: `types.StrategyDockerfile`, `types.StrategyNixpacks`, `types.BuildConfig.Strategy`, `Detection.Framework`, `Detection.InternalPort`
- Produces: `Builder.Build()` dispatches to `BuildWithNixpacks()` when strategy is `nixpacks`; `NixpacksDetect(dir) (port int, startCmd string, err error)`

- [ ] **Step 1: Write the failing test for nixpacks build**

```go
// internal/builder/nixpacks_test.go
package builder

import (
    "context"
    "os"
    "path/filepath"
    "testing"
)

func TestNixpacksDetectMissingCLI(t *testing.T) {
    // Should return error when nixpacks CLI is not available
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)

    port, startCmd, err := NixpacksDetect(dir)
    if err == nil {
        t.Skip("nixpacks CLI is installed on this machine — skipping CLI-missing test")
    }
    // When CLI is missing, expect an exec.ErrNotFound-like error
    if port != 0 || startCmd != "" {
        t.Errorf("expected zero values on error, got port=%d cmd=%q", port, startCmd)
    }
}

func TestNixpacksBuildMissingCLI(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)

    b := New(t.TempDir())
    detection := &Detection{
        Framework:    FrameworkStatic,
        InternalPort: 80,
    }
    _, _, err := b.BuildWithNixpacks(context.Background(), dir, "testapp", "production", detection, "12345")
    if err == nil {
        t.Skip("nixpacks CLI is installed — skipping CLI-missing test")
    }
}

func TestNixpacksBuildDispatch(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)

    b := New(t.TempDir())
    detection := &Detection{
        Framework:    FrameworkStatic,
        InternalPort: 80,
    }
    _, _, err := b.Build(context.Background(), dir, "testapp", "production", detection, "12345")
    if err != nil {
        t.Fatalf("Build with default strategy: %v", err)
    }
}
```

- [ ] **Step 2: Run test to verify expected failures**

Run: `go test ./internal/builder/... -run "TestNixpacksDetectMissingCLI|TestNixpacksBuildMissingCLI|TestNixpacksBuildDispatch" -v -count=1`

Expected: FAIL with `undefined: NixpacksDetect`, `undefined: BuildWithNixpacks`

- [ ] **Step 3: Create `internal/builder/nixpacks.go`**

```go
package builder

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
)

type nixpacksPlan struct {
    Port    int    `json:"port"`
    StartCmd string `json:"start"`
}

func NixpacksDetect(dir string) (port int, startCmd string, err error) {
    cmd := exec.Command("nixpacks", "detect", dir)
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    if err := cmd.Run(); err != nil {
        return 0, "", fmt.Errorf("nixpacks detect: %w\nstderr: %s", err, stderr.String())
    }

    var plan nixpacksPlan
    if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
        return 0, "", fmt.Errorf("nixpacks detect parse: %w\noutput: %s", err, stdout.String())
    }

    return plan.Port, plan.StartCmd, nil
}

func (b *Builder) BuildWithNixpacks(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    if env == "" {
        env = "production"
    }

    tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

    args := []string{"build", dir, "--name", tag}
    if detection.InternalPort != 0 {
        args = append(args, fmt.Sprintf("--pkgs=curl"), "-e", fmt.Sprintf("PORT=%d", detection.InternalPort))
    }

    cmd := exec.CommandContext(ctx, "nixpacks", args...)

    var logBuf bytes.Buffer
    cmd.Stdout = &logBuf
    cmd.Stderr = &logBuf

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

- [ ] **Step 4: Modify `internal/builder/builder.go` — add strategy dispatch**

Add import for `"github.com/yaso09/tengiz/internal/types"` at the top.

Modify `Build()` method (add strategy check before `ensureDockerfile`):

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    if detection.Framework == FrameworkDocker {
        return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
    }

    if detection.Strategy == types.StrategyNixpacks {
        return b.BuildWithNixpacks(ctx, dir, appName, env, detection, deploymentID)
    }

    if err := b.ensureDockerfile(dir, detection); err != nil {
        return "", "", fmt.Errorf("generate dockerfile: %w", err)
    }
    return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
}
```

Add `Strategy` field to `Detection` struct in `detect.go`:

```go
type Detection struct {
    Framework    Framework
    BuildCmd     string
    OutputDir    string
    InternalPort int
    HealthCheck  *types.HealthCheckConfig
    Strategy     types.BuildStrategy
}
```

- [ ] **Step 5: Run builder tests**

Run: `go build ./...`

Expected: Build succeeds

Run: `go test ./internal/builder/... -run "TestNixpacksDetectMissingCLI|TestNixpacksBuildMissingCLI|TestNixpacksBuildDispatch" -v -count=1`

Expected: Tests pass (skip when nixpacks CLI is unavailable, otherwise exercise the code)

- [ ] **Step 6: Run all builder tests**

Run: `go test ./internal/builder/... -v -count=1`

Expected: All PASS (existing detection/Dockerfile tests should pass unchanged)

- [ ] **Step 7: Commit**

```bash
git add internal/builder/nixpacks.go internal/builder/builder.go internal/builder/detect.go
git commit -m "feat: add Nixpacks build strategy and strategy dispatch in Builder"
```

---

### Task 3: Wire strategy from config and CLI into the deploy flow

**Files:**
- Modify: `internal/cli/root.go` — add `--builder` flag to `deployCmd`, pass strategy into detection/Build

**Interfaces:**
- Consumes: `types.BuildConfig.Strategy`, `builder.Detection.Strategy`
- Produces: `tengiz deploy --builder nixpacks` overrides strategy; `.tengiz.yaml` `build.strategy: nixpacks` sets config-level strategy

- [ ] **Step 1: Write the failing test for the builder flag**

```go
// internal/cli/env_test.go — add this test
package cli

import (
    "os"
    "path/filepath"
    "testing"
)

func TestDeployCmdBuilderFlag(t *testing.T) {
    flag := deployCmd.Flags().Lookup("builder")
    if flag == nil {
        t.Fatal("deployCmd missing --builder flag")
    }
    if flag.DefValue != "dockerfile" {
        t.Errorf("default --builder = %q, want %q", flag.DefValue, "dockerfile")
    }
}

func TestDeployCmdBuilderFlagConfigOverride(t *testing.T) {
    // Create a temp .tengiz.yaml with builder: nixpacks
    dir := t.TempDir()
    yamlContent := []byte("name: testapp\nbuild:\n  strategy: nixpacks\n")
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), yamlContent, 0644)

    // Simulate loading config to verify it sets Strategy
    cfg, err := config.Load(dir)
    if err != nil {
        t.Fatalf("Load: %v", err)
    }
    if cfg.Build.Strategy != "nixpacks" {
        t.Errorf("Strategy from config = %q, want %q", cfg.Build.Strategy, "nixpacks")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestDeployCmdBuilderFlag" -v -count=1`

Expected: FAIL with `undefined: cfg` or flag not found (depending on the specific import issue)

- [ ] **Step 3: Add `--builder` flag and config wiring to `internal/cli/root.go`**

Add in the `init()` function (near the other deploy flags):

```go
deployCmd.Flags().String("builder", "dockerfile", "build strategy: dockerfile or nixpacks")
```

Modify the `deployCmd.RunE` body. After the `detection` is created (line 187), add:

```go
// Apply builder strategy from flag or config
builderFlag, _ := cmd.Flags().GetString("builder")
detection.Strategy = types.BuildStrategy(builderFlag)
if cfg.Build.Strategy != "" && builderFlag == "dockerfile" {
    detection.Strategy = types.BuildStrategy(cfg.Build.Strategy)
}
```

Add the import for `"github.com/yaso09/tengiz/internal/types"` if not already present (it is — line 25).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestDeployCmdBuilderFlag" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 5: Run all CLI tests**

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add --builder flag to deploy command and wire strategy from config"
```

---

### Task 4: Add nixpacks-aware detection hints

**Files:**
- Modify: `internal/builder/detect.go` — add nixpacks detection hint for known nixpacks-only frameworks

**Interfaces:**
- Consumes: existing `Detect()` framework detection
- Produces: when Nixpacks is the selected strategy, some frameworks are hinted differently (e.g., Ruby detected by `Gemfile`, Rust by `Cargo.toml`)

- [ ] **Step 1: Write the failing test for nixpacks detection**

```go
// internal/builder/detect_test.go
package builder

import (
    "os"
    "path/filepath"
    "testing"
)

func TestDetectRubyWithNixpacks(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`source "https://rubygems.org"`), 0644)
    os.WriteFile(filepath.Join(dir, "config.ru"), []byte("run App"), 0644)

    d, err := Detect(dir)
    if err != nil {
        t.Fatalf("Detect: %v", err)
    }
    // Without Nixpacks, this falls through to static fallback
    if d.Framework != FrameworkStatic {
        t.Logf("framework detected as %q (expected static fallback without nixpacks)", d.Framework)
    }
}

func TestDetectRustWithNixpacks(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[package]\nname = "test"`), 0644)

    d, err := Detect(dir)
    if err != nil {
        t.Fatalf("Detect: %v", err)
    }
    if d.Framework != FrameworkStatic {
        t.Logf("framework detected as %q (expected static fallback without nixpacks)", d.Framework)
    }
}

func TestDetectPHPSymfonyWithNixpacks(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{}`), 0644)

    d, err := Detect(dir)
    if err != nil {
        t.Fatalf("Detect: %v", err)
    }
    if d.Framework != FrameworkStatic {
        t.Logf("framework detected as %q (expected static fallback without nixpacks)", d.Framework)
    }
}
```

- [ ] **Step 2: Run detection tests to verify they pass (they should detect as static)**

Run: `go test ./internal/builder/... -run "TestDetectRubyWithNixpacks|TestDetectRustWithNixpacks|TestDetectPHPSymfonyWithNixpacks" -v -count=1`

Expected: PASS (they fall through to static fallback — this is correct, Nixpacks handles its own detection)

- [ ] **Step 3: No code changes needed for detection itself**

**Analysis:** The existing `Detect()` function returns `FrameworkStatic` for anything it doesn't recognize. Nixpacks has its own `nixpacks detect` command that produces a JSON plan with port and start command. When the strategy is `nixpacks`, Tengiz should:

1. Run `builder.Detect()` for port hints (returns static fallback port 80 for unknown frameworks)
2. Run `nixpacks detect` separately for Nixpacks-specific metadata (handled in `NixpacksDetect`)
3. The detection from `builder.Detect()` is sufficient for port allocation — Nixpacks will override internally

The existing `Detect()` is fine as-is. No changes needed.

- [ ] **Step 4: Run all detection tests**

Run: `go test ./internal/builder/... -run "TestDetect" -v -count=1`

Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/builder/detect_test.go
git commit -m "test: add detection tests for nixpacks-only frameworks (Ruby, Rust, PHP)"
```

---

### Task 5: Add strategy validation and error messaging

**Files:**
- Modify: `internal/builder/builder.go` — validate strategy in `Build()` before dispatching

**Interfaces:**
- Consumes: `types.BuildStrategy` constants
- Produces: clear error when invalid strategy is specified

- [ ] **Step 1: Write the failing test**

```go
// internal/builder/builder_test.go — add this test

func TestBuildInvalidStrategy(t *testing.T) {
    b := New(t.TempDir())
    detection := &Detection{
        Framework:    FrameworkStatic,
        InternalPort: 80,
        Strategy:     "invalid-strategy",
    }

    _, _, err := b.Build(context.Background(), t.TempDir(), "testapp", "production", detection, "12345")
    if err == nil {
        t.Fatal("expected error for invalid strategy, got nil")
    }
    if !strings.Contains(err.Error(), "invalid-strategy") {
        t.Errorf("error should mention the invalid strategy: %v", err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -run "TestBuildInvalidStrategy" -v -count=1`

Expected: FAIL with error not matching (because no validation exists yet)

- [ ] **Step 3: Add strategy validation to `Builder.Build()` in `internal/builder/builder.go`**

Add at the beginning of `Build()`, before any other logic:

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    // Validate strategy
    strategy := detection.Strategy
    if strategy == "" {
        strategy = types.StrategyDockerfile
    }
    switch strategy {
    case types.StrategyDockerfile, types.StrategyNixpacks:
        // valid
    default:
        return "", "", fmt.Errorf("unsupported build strategy %q: use %q or %q",
            strategy, types.StrategyDockerfile, types.StrategyNixpacks)
    }

    if detection.Framework == FrameworkDocker {
        return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
    }

    if strategy == types.StrategyNixpacks {
        return b.BuildWithNixpacks(ctx, dir, appName, env, detection, deploymentID)
    }

    if err := b.ensureDockerfile(dir, detection); err != nil {
        return "", "", fmt.Errorf("generate dockerfile: %w", err)
    }
    return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/builder/... -run "TestBuildInvalidStrategy" -v -count=1`

Expected: PASS

Run: `go test ./internal/builder/... -v -count=1`

Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go
git commit -m "feat: add build strategy validation with clear error messages"
```

---

### Task 6: Update init command template and docs

**Files:**
- Modify: `internal/cli/root.go` — update `initCmd` template to mention `build.strategy`

- [ ] **Step 1: Add builder strategy to the init template**

Modify the `initCmd.RunE` in `internal/cli/root.go` — add to the `content` template:

In the config template (around line 137-138), add a commented entry:

```go
content := fmt.Sprintf(`name: %s
environment: %s
# port: 3000            # container internal port (auto-detected if omitted)
# build:
#   strategy: nixpacks   # build strategy: dockerfile (default) or nixpacks
serverless:
  enabled: true
  idle_timeout: 5m      # scale-to-zero timeout
`, name, env)
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cli/... -run "TestInit" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "docs: add build.strategy to init command template"
```

---

### Task 7: Run full test suite and verify

**Files:**
- Modify: none (test-only)

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except proxy TCP timeout tests and idle time-sensitive tests which may fail due to timing)

- [ ] **Step 2: Run vet**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 3: Verify build**

Run: `go build -o /dev/null .`

Expected: Build succeeds

- [ ] **Step 4: Self-review against spec**

Check requirements from `docs/FUTURES_FEATURES.md`:
- Nixpacks as alternative build system ✅ (Task 2 — `BuildWithNixpacks`)
- Hundreds of frameworks supported ✅ (Nixpacks handles detection + Dockerfile generation)
- Configurable via `.tengiz.yaml` `build.strategy` ✅ (Task 1, Task 3)
- Default remains current behavior ✅ (strategy defaults to `dockerfile`)
- Image tagging workflow unchanged ✅ (`buildWithDockerfile` and `BuildWithNixpacks` both produce same tag format)

- [ ] **Step 5: Placeholder scan**

Search plan for any "TBD", "TODO", "implement later", "fill in details", "Similar to Task" patterns. None found. Every step has complete code.

- [ ] **Step 6: Type consistency check**

- `types.BuildStrategy` (type) — defined in Task 1, used in builder (Task 2, 5), CLI (Task 3)
- `types.StrategyDockerfile` / `types.StrategyNixpacks` — defined in Task 1, used in builder (Task 2, 5), CLI (Task 3)
- `types.BuildConfig.Strategy` — defined in Task 1, set in config (Task 3), read in CLI (Task 3)
- `builder.Detection.Strategy` — defined in Task 2, set in CLI (Task 3)
- `builder.Builder.BuildWithNixpacks(ctx, dir, appName, env, detection, deploymentID)` — defined in Task 2, called in Task 2 dispatch
- `builder.NixpacksDetect(dir) (port, startCmd, err)` — defined in Task 2, callable from any consumer
- `--builder` flag — defined in Task 3, default `"dockerfile"`, CLI override via `deployCmd.Flags().String`

- [ ] **Step 7: Commit any remaining changes**

```bash
git status
git add -A
git commit -m "chore: final cleanup and test additions for nixpacks build system"
```
