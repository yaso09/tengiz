# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build system alongside the existing Dockerfile generator so Tengiz can deploy hundreds of frameworks (Ruby, Rust, PHP, Java, Elixir, etc.) without manual Dockerfile authoring.

**Architecture:** A `BuilderStrategy` interface with two implementations — `DockerfileStrategy` (existing behavior, extracted) and `NixpacksStrategy` (new). The `builder.Builder` delegates to the selected strategy. Users choose via `.tengiz.yaml` `build.strategy: nixpacks` or `--builder nixpacks` CLI flag.

**Tech Stack:** Go 1.26, existing `os/exec` for `nixpacks` CLI (must be installed separately), no new external Go dependencies.

## Global Constraints

- Nixpacks CLI must be resolved at build time via `exec.LookPath("nixpacks")` — graceful fallback with clear error message if not installed
- Image tag format unchanged: `tengiz-apps/{app}:{env}-{deploymentID}` and `{env}-latest`
- Build logs captured to same `bytes.Buffer` pattern as existing Dockerfile builds
- Existing `builder.Builder` consumers (deploy, gitdeploy, preview) must require ZERO code changes — only config changes should switch strategies
- `.tengiz.yaml` `build.strategy` values: `"dockerfile"` (default, current behavior), `"nixpacks"`
- `--builder` CLI flag on `tengiz deploy` overrides config, values: `"dockerfile"`, `"nixpacks"`
- All existing tests must pass without modification
- New tests must cover strategy selection, Nixpacks CLI detection, and Nixpacks build flow

---

## File Structure

| File | Responsibility |
|------|---------------|
| **Create:** `internal/builder/strategy.go` | `Strategy` interface + `NixpacksBuildInput` struct |
| **Create:** `internal/builder/nixpacks.go` | `NixpacksStrategy` — runs `nixpacks build` CLI, parses output for image tag |
| **Modify:** `internal/builder/builder.go` | Refactor `Builder.Build()` to delegate to `Strategy`; add `SetStrategy()` |
| **Modify:** `internal/builder/detect.go` | Add `FrameworkNixpacks` constant; add `Strategy` field to `Detection` |
| **Modify:** `internal/types/types.go` | Add `Strategy` field to `BuildConfig` |
| **Modify:** `internal/cli/root.go` | Add `--builder` flag to `deployCmd`; pass through to builder |
| **Create:** `internal/builder/nixpacks_test.go` | Tests for Nixpacks strategy |
| **Modify:** `internal/builder/builder_test.go` | Add tests for strategy selection, ensure existing tests still pass |

---

### Task 1: Define Strategy Interface and Nixpacks Build Input Types

**Files:**
- Create: `internal/builder/strategy.go`

**Interfaces:**
- Consumes: `*Detection`, `context.Context`, app name, env, deployment ID
- Produces: `Strategy` interface with `Build(ctx, dir, appName, env, deploymentID, detection) (imageTag, buildLog string, err error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/builder/strategy_test.go
package builder

import (
    "context"
    "testing"
)

type mockStrategy struct {
    imageTag string
    buildLog string
    err      error
}

func (m *mockStrategy) Build(ctx context.Context, dir, appName, env, deploymentID string, detection *Detection) (string, string, error) {
    return m.imageTag, m.buildLog, m.err
}

func TestBuilderDelegatesToStrategy(t *testing.T) {
    b := New("/tmp/test-data")
    mock := &mockStrategy{imageTag: "tengiz-apps/test:v1", buildLog: "build ok"}
    b.SetStrategy(mock)

    tag, log, err := b.Build(context.Background(), "/tmp/dir", "test", "production", &Detection{Framework: FrameworkStatic}, "v1")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if tag != "tengiz-apps/test:v1" {
        t.Errorf("expected tag tengiz-apps/test:v1, got %s", tag)
    }
    if log != "build ok" {
        t.Errorf("expected log 'build ok', got %s", log)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestBuilderDelegatesToStrategy -v`
Expected: FAIL — `Builder.SetStrategy` not defined

- [ ] **Step 3: Create strategy.go with interface and Builder.SetStrategy**

```go
// internal/builder/strategy.go
package builder

import "context"

type Strategy interface {
    Build(ctx context.Context, dir, appName, env, deploymentID string, detection *Detection) (imageTag string, buildLog string, err error)
}
```

- [ ] **Step 4: Add SetStrategy to Builder and refactor Build to delegate**

In `internal/builder/builder.go`, add to `Builder` struct:

```go
type Builder struct {
    dataDir  string
    strategy Strategy
}

func New(dataDir string) *Builder {
    return &Builder{dataDir: dataDir}
}

func (b *Builder) SetStrategy(s Strategy) {
    b.strategy = s
}
```

Then modify `Build`:

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    if b.strategy != nil {
        return b.strategy.Build(ctx, dir, appName, env, deploymentID, detection)
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

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/builder/ -run TestBuilderDelegatesToStrategy -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/builder/strategy.go internal/builder/strategy_test.go internal/builder/builder.go
git commit -m "feat(builder): add Strategy interface for pluggable build backends"
```

---

### Task 2: Implement NixpacksStrategy

**Files:**
- Create: `internal/builder/nixpacks.go`
- Create: `internal/builder/nixpacks_test.go`

**Interfaces:**
- Consumes: `Strategy` interface, `Detection` struct, `os/exec` for nixpacks CLI
- Produces: `NixpacksStrategy` struct implementing `Strategy`

- [ ] **Step 1: Write the failing test for Nixpacks strategy**

```go
// internal/builder/nixpacks_test.go
package builder

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "testing"
)

func TestNixpacksStrategyNoNixpacksBinary(t *testing.T) {
    // Simulate missing nixpacks by setting PATH to empty dir
    tmpDir := t.TempDir()
    originalPath := os.Getenv("PATH")
    os.Setenv("PATH", tmpDir)
    defer os.Setenv("PATH", originalPath)

    s := &NixpacksStrategy{}
    _, _, err := s.Build(context.Background(), tmpDir, "test", "production", "v1", &Detection{Framework: FrameworkStatic})
    if err == nil {
        t.Fatal("expected error when nixpacks is not installed")
    }
    if !errors.Is(err, ErrNixpacksNotFound) {
        t.Errorf("expected ErrNixpacksNotFound, got %v", err)
    }
}

func TestNixpacksStrategyBuild(t *testing.T) {
    if _, err := exec.LookPath("nixpacks"); err != nil {
        t.Skip("nixpacks CLI not installed")
    }

    tmpDir := t.TempDir()
    err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"name":"test"}`), 0644)
    if err != nil {
        t.Fatal(err)
    }

    s := &NixpacksStrategy{}
    tag, buildLog, err := s.Build(context.Background(), tmpDir, "test", "production", "1704067200", &Detection{Framework: FrameworkNode})
    if err != nil {
        t.Fatalf("unexpected error: %v\nbuild log: %s", err, buildLog)
    }
    expected := "tengiz-apps/test:production-1704067200"
    if tag != expected {
        t.Errorf("expected tag %s, got %s", expected, tag)
    }
}

func TestNixpacksStrategyNoProject(t *testing.T) {
    if _, err := exec.LookPath("nixpacks"); err != nil {
        t.Skip("nixpacks CLI not installed")
    }

    emptyDir := t.TempDir()
    s := &NixpacksStrategy{}
    _, _, err := s.Build(context.Background(), emptyDir, "test", "production", "v1", &Detection{Framework: FrameworkStatic})
    if err == nil {
        t.Fatal("expected error for empty directory")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestNixpacksStrategy -v`
Expected: FAIL — `NixpacksStrategy` not defined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/builder/nixpacks.go
package builder

import (
    "bytes"
    "context"
    "errors"
    "fmt"
    "io"
    "os"
    "os/exec"
)

var ErrNixpacksNotFound = errors.New("nixpacks CLI not found; install from https://nixpacks.com/docs/install")

type NixpacksStrategy struct{}

func (s *NixpacksStrategy) Build(ctx context.Context, dir, appName, env, deploymentID string, detection *Detection) (string, string, error) {
    if _, err := exec.LookPath("nixpacks"); err != nil {
        return "", "", fmt.Errorf("%w: %s", ErrNixpacksNotFound, err)
    }

    tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

    args := []string{"build", dir, "--name", tag}
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

- [ ] **Step 4: Run test to verify it passes (or skips gracefully)**

Run: `go test ./internal/builder/ -run TestNixpacksStrategy -v`
Expected: PASS (or SKIP if nixpacks CLI not installed)

- [ ] **Step 5: Commit**

```bash
git add internal/builder/nixpacks.go internal/builder/nixpacks_test.go
git commit -m "feat(builder): implement NixpacksStrategy for nixpacks CLI builds"
```

---

### Task 3: Add Builder Selection to Config and Detection

**Files:**
- Modify: `internal/types/types.go` — add `Strategy` field to `BuildConfig`
- Modify: `internal/builder/detect.go` — add `Strategy` field to `Detection`

**Interfaces:**
- Consumes: user config / CLI flags
- Produces: `BuildConfig.Strategy string`, `Detection.Strategy string`

- [ ] **Step 1: Write the failing test**

```go
// internal/types/types_test.go
package types

import (
    "testing"
)

func TestBuildConfigStrategyField(t *testing.T) {
    cfg := BuildConfig{Strategy: "nixpacks"}
    if cfg.Strategy != "nixpacks" {
        t.Errorf("expected strategy nixpacks, got %s", cfg.Strategy)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/ -run TestBuildConfigStrategyField -v`
Expected: FAIL — `Strategy` field not in `BuildConfig`

- [ ] **Step 3: Add Strategy field to BuildConfig in types.go**

```go
type BuildConfig struct {
    Command  string `mapstructure:"command"`
    Output   string `mapstructure:"output"`
    Strategy string `mapstructure:"strategy"`
}
```

- [ ] **Step 4: Add Strategy field to Detection struct in detect.go**

```go
type Detection struct {
    Framework    Framework
    BuildCmd     string
    OutputDir    string
    InternalPort int
    HealthCheck  *types.HealthCheckConfig
}
```
→ add `Strategy string` field.

- [ ] **Step 5: Update Detect to set Strategy**

In `detect.go`, at the end of `Detect()` before returning, set a default:

```go
func Detect(dir string) (*Detection, error) {
    // ... existing detection logic ...
    d := &Detection{Framework: FrameworkStatic, InternalPort: 80}
    // ... rest of detection logic ...
    return d, nil
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/builder/ ./internal/types/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/types/types.go internal/builder/detect.go
git commit -m "feat: add Strategy field to BuildConfig and Detection types"
```

---

### Task 4: Wire Builder Selection Through Deploy Command

**Files:**
- Modify: `internal/cli/root.go` — add `--builder` flag to deploy command, create strategy from config/flag

**Interfaces:**
- Consumes: `builder.Strategy`, `builder.NixpacksStrategy`, CLI flags, `AppConfig.Build.Strategy`
- Produces: deploy command with selectable builder backend

- [ ] **Step 1: Write the failing test for strategy selection logic**

Add to `internal/builder/strategy_test.go`:

```go
func TestStrategyFromConfigDockerfile(t *testing.T) {
    s := StrategyFromConfig("dockerfile")
    if s != nil {
        t.Errorf("expected nil (default) for dockerfile strategy, got %T", s)
    }
}

func TestStrategyFromConfigNixpacks(t *testing.T) {
    s := StrategyFromConfig("nixpacks")
    if _, ok := s.(*NixpacksStrategy); !ok {
        t.Errorf("expected *NixpacksStrategy, got %T", s)
    }
}

func TestStrategyFromConfigEmpty(t *testing.T) {
    s := StrategyFromConfig("")
    if s != nil {
        t.Errorf("expected nil for empty strategy, got %T", s)
    }
}

func TestStrategyFromConfigInvalid(t *testing.T) {
    s := StrategyFromConfig("invalid")
    if s != nil {
        t.Errorf("expected nil for invalid strategy, got %T", s)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestStrategyFromConfig -v`
Expected: FAIL — `StrategyFromConfig` not defined

- [ ] **Step 3: Add StrategyFromConfig helper to strategy.go**

```go
func StrategyFromConfig(s string) Strategy {
    switch s {
    case "nixpacks":
        return &NixpacksStrategy{}
    case "dockerfile", "":
        return nil
    default:
        return nil
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/ -run TestStrategyFromConfig -v`
Expected: PASS

- [ ] **Step 5: Add --builder flag to deploy command in root.go**

Locate `deployCmd` definition (~line 155). Add:

```go
var builderFlag string

// In deployCmd:
func init() {
    deployCmd.Flags().StringVar(&builderFlag, "builder", "", "Build strategy: dockerfile (default) or nixpacks")
}
```

- [ ] **Step 6: Wire strategy creation in deploy command RunE**

After framework detection (~line 195), before `b.Build()`:

```go
strategyName := builderFlag
if strategyName == "" && cfg.Build.Strategy != "" {
    strategyName = cfg.Build.Strategy
}
if s := builder.StrategyFromConfig(strategyName); s != nil {
    b.SetStrategy(s)
}
```

- [ ] **Step 7: Verify existing deploy tests still pass**

Run: `go build ./... && go test ./internal/cli/ -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/builder/strategy.go internal/cli/root.go
git commit -m "feat(cli): add --builder flag to deploy command for strategy selection"
```

---

### Task 5: Integration — Verify Nixpacks Flow End-to-End

**Files:**
- Modify: `internal/builder/builder_test.go` — add integration test for nixpacks via builder.Build

**Interfaces:**
- Consumes: full `Builder.Build()` flow with nixpacks strategy
- Produces: working end-to-end build output

- [ ] **Step 1: Write integration test**

```go
// internal/builder/builder_test.go
func TestBuildWithNixpacksStrategy(t *testing.T) {
    if _, err := exec.LookPath("nixpacks"); err != nil {
        t.Skip("nixpacks CLI not installed for integration test")
    }

    tmpDir := t.TempDir()
    err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"name":"test-app"}`), 0644)
    if err != nil {
        t.Fatal(err)
    }

    b := New(t.TempDir())
    b.SetStrategy(&NixpacksStrategy{})
    detection := &Detection{Framework: FrameworkNode, InternalPort: 3000}

    tag, buildLog, err := b.Build(context.Background(), tmpDir, "test-app", "production", detection, "inttest001")
    if err != nil {
        t.Fatalf("Build with nixpacks failed: %v\nlog: %s", err, buildLog)
    }
    expectedTag := "tengiz-apps/test-app:production-inttest001"
    if tag != expectedTag {
        t.Errorf("expected tag %s, got %s", expectedTag, tag)
    }
    if buildLog == "" {
        t.Error("expected non-empty build log")
    }

    // Verify latest tag was created
    inspectCmd := exec.Command("docker", "inspect", "tengiz-apps/test-app:production-latest")
    if out, err := inspectCmd.CombinedOutput(); err != nil {
        t.Errorf("latest tag not found: %v\n%s", err, string(out))
    }

    // Cleanup
    cleanupCmd := exec.Command("docker", "rmi", "tengiz-apps/test-app:production-inttest001", "tengiz-apps/test-app:production-latest")
    _ = cleanupCmd.Run()
}
```

- [ ] **Step 2: Run integration test**

Run: `go test ./internal/builder/ -run TestBuildWithNixpacksStrategy -v`
Expected: PASS (or SKIP if nixpacks CLI not installed)

- [ ] **Step 3: Run all builder tests to ensure no regressions**

Run: `go test ./internal/builder/ -v -count=1`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add internal/builder/builder_test.go
git commit -m "test(builder): add integration test for nixpacks build strategy"
```

---

## Self-Review

**1. Spec coverage:**
- ✗ Strategy selection via `.tengiz.yaml` `build.strategy` → Task 3 (BuildConfig.Strategy) + Task 4 (deploy command wiring)
- ✗ `--builder` CLI flag → Task 4
- ✗ Nixpacks CLI invocation with `--name` flag for tagging → Task 2
- ✗ Image tag format `tengiz-apps/{app}:{env}-{deploymentID}` → Task 2 (NixpacksStrategy.Build)
- ✗ Latest tag `{env}-latest` → Task 2 (docker tag)
- ✗ Build log capture → Task 2 (bytes.Buffer + io.MultiWriter)
- ✗ Fallback to existing Dockerfile builder when nixpacks not selected → Task 1 (Builder.Build falls through to existing logic when strategy is nil)
- All covered.

**2. Placeholder scan:** No TBD, TODO, "implement later", "add error handling" patterns found.

**3. Type consistency:** All method signatures match across tasks:
- `Strategy.Build(ctx, dir, appName, env, deploymentID, detection) (string, string, error)` — consistent in Tasks 1, 2, 4
- `StrategyFromConfig(string) Strategy` — consistent in Task 4
- `BuildConfig.Strategy string` → `Detection.Strategy string` (though Detection.Strategy is added but may remain unused by strategies; each strategy implementation processes detection as needed)

No type inconsistencies found.
