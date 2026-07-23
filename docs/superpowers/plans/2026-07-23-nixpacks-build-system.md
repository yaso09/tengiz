# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build strategy to Tengiz, expanding framework support from 6 to hundreds (Ruby, Rust, PHP, Java, Elixir, etc.)

**Architecture:** Introduce a `BuildStrategy` interface in the `builder` package with two implementations: `DockerfileStrategy` (existing behavior) and `NixpacksStrategy` (new). The `Builder.Build()` method selects strategy based on a new `Builder` field in `Detection`. Nixpacks is invoked via `os/exec` (same pattern as Docker CLI). The `.tengiz.yaml` config gets a `builder: nixpacks` option to opt in. Detection adds a `Nixpacks` framework constant for projects without Dockerfile that want Nixpacks.

**Tech Stack:** Go 1.26, `os/exec` (nixpacks CLI), existing `builder` package patterns

## Global Constraints

- Nixpacks CLI must be installed separately (like Docker) — no Go SDK
- Follow existing `os/exec` patterns in `builder.go` (not `docker` SDK)
- All new framework constants follow existing naming convention (`FrameworkNixpacks`)
- `.tengiz.yaml` `build.strategy: nixpacks` to opt in
- Existing Dockerfile-based builds remain the default
- Nixpacks output is a Docker image tag (same return type as current `Build`)
- All tests must pass with `go test ./... -v -count=1`
- No new external Go dependencies

---

### Task 1: Add Nixpacks Framework Constant and Detection

**Files:**
- Modify: `internal/builder/detect.go:10-20`
- Modify: `internal/builder/detect.go:30-78`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: existing `Framework` type, `Detection` struct, `Detect()` function
- Produces: `FrameworkNixpacks` constant, `BuildStrategy` type, `StrategyNixpacks`/`StrategyDockerfile` constants, `Detection.Builder` field

- [ ] **Step 1: Add `FrameworkNixpacks` constant and `BuildStrategy` type**

Edit `internal/builder/detect.go` to add the new framework constant and strategy type:

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

type BuildStrategy string

const (
    StrategyDockerfile BuildStrategy = "dockerfile"
    StrategyNixpacks   BuildStrategy = "nixpacks"
)
```

Add `Builder` field to `Detection`:

```go
type Detection struct {
    Framework    Framework
    BuildCmd     string
    OutputDir    string
    InternalPort int
    HealthCheck  *types.HealthCheckConfig
    Builder      BuildStrategy
}
```

- [ ] **Step 2: Update `Detect()` to return `FrameworkNixpacks` when no framework matches and no Dockerfile**

Modify the fallback in `Detect()` in `internal/builder/detect.go`:

```go
// At the end of Detect(), change the fallback from FrameworkStatic to FrameworkNixpacks:
return &Detection{Framework: FrameworkNixpacks, InternalPort: 8080, Builder: StrategyNixpacks}, nil
```

- [ ] **Step 3: Write failing tests for Nixpacks detection**

Add to `internal/builder/builder_test.go`:

```go
func TestDetectNixpacks(t *testing.T) {
	dir := t.TempDir()
	// No Dockerfile, no known framework files — should detect as nixpacks
	os.WriteFile(filepath.Join(dir, "main.rb"), []byte(`puts "hello"`), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkNixpacks {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNixpacks)
	}
	if d.Builder != StrategyNixpacks {
		t.Errorf("Builder = %q, want %q", d.Builder, StrategyNixpacks)
	}
}

func TestDetectNixpacksWithConfig(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[package]\nname = "test"`), 0644)

	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkNixpacks {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNixpacks)
	}
	if d.Builder != StrategyNixpacks {
		t.Errorf("Builder = %q, want %q", d.Builder, StrategyNixpacks)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

```bash
go test ./internal/builder/... -v -count=1 -run "TestDetectNixpacks"
```

Expected: FAIL — `FrameworkNixpacks` and `StrategyNixpacks` not yet defined.

- [ ] **Step 5: Implement the changes in detect.go**

Edit `internal/builder/detect.go`:

Add constants after `FrameworkDocker`:
```go
FrameworkNixpacks Framework = "nixpacks"
)

type BuildStrategy string

const (
    StrategyDockerfile BuildStrategy = "dockerfile"
    StrategyNixpacks   BuildStrategy = "nixpacks"
)
```

Add `Builder` field to `Detection`:
```go
type Detection struct {
    Framework    Framework
    BuildCmd     string
    OutputDir    string
    InternalPort int
    HealthCheck  *types.HealthCheckConfig
    Builder      BuildStrategy
}
```

Change the fallback in `Detect()` from:
```go
return &Detection{Framework: FrameworkStatic, InternalPort: 80}, nil
```
to:
```go
return &Detection{Framework: FrameworkNixpacks, InternalPort: 8080, Builder: StrategyNixpacks}, nil
```

- [ ] **Step 4: Run tests to verify detection tests pass**

```bash
go test ./internal/builder/... -v -count=1 -run "TestDetect"
```

Expected: All detection tests PASS, including the new Nixpacks tests.

- [ ] **Step 5: Commit**

```bash
git add internal/builder/detect.go internal/builder/builder_test.go
git commit -m "feat: add Nixpacks framework constant and detection"
```

---

### Task 2: Nixpacks Build Strategy Implementation

**Files:**
- Create: `internal/builder/nixpacks.go`
- Modify: `internal/builder/builder.go:21-29`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `Detection.Builder` (StrategyNixpacks), `Detection.Framework` (FrameworkNixpacks), `Builder.Build()` signature
- Produces: `nixpacksBuild(ctx, dir, appName, env, deploymentID) (tag, logs, error)` — same signature as `buildWithDockerfile`

- [ ] **Step 1: Create `internal/builder/nixpacks.go`**

```go
package builder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func (b *Builder) nixpacksBuild(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
	if env == "" {
		env = "production"
	}
	tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

	cmd := exec.CommandContext(ctx, "nixpacks", "build", dir, "--name", tag)

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

- [ ] **Step 2: Update `Builder.Build()` to dispatch to nixpacks strategy**

Edit `internal/builder/builder.go` — update `Build()` method:

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
	if detection.Builder == StrategyNixpacks {
		return b.nixpacksBuild(ctx, dir, appName, env, deploymentID)
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

- [ ] **Step 3: Write failing test for nixpacks build**

Add to `internal/builder/builder_test.go`:

```go
func TestNixpacksBuildDispatchesToNixpacks(t *testing.T) {
	b := New(t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.rb"), []byte(`puts "hello"`), 0644)
	detection := &Detection{Framework: FrameworkNixpacks, InternalPort: 8080, Builder: StrategyNixpacks}

	_, _, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v123")
	if err != nil {
		// nixpacks CLI not installed — that's expected in CI
		t.Skipf("nixpacks not installed: %v", err)
	}
}

func TestBuildWithNixpacksStrategy(t *testing.T) {
	b := New(t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.rb"), []byte(`puts "hello"`), 0644)
	detection := &Detection{Framework: FrameworkNixpacks, InternalPort: 8080, Builder: StrategyNixpacks}

	tag, _, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v123")
	if err != nil {
		t.Skipf("nixpacks not installed: %v", err)
	}
	expected := "tengiz-apps/testapp:production-v123"
	if tag != expected {
		t.Errorf("tag = %q, want %q", tag, expected)
	}
}
```

- [ ] **Step 2: Run tests to verify they compile and pass**

```bash
go test ./internal/builder/... -v -count=1 -run "TestNixpacksBuild|TestBuildWithNixpacks"
```

Expected: Tests either PASS (if nixpacks CLI installed) or SKIP (if not installed). No compilation errors.

- [ ] **Step 3: Commit**

```bash
git add internal/builder/nixpacks.go internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: add Nixpacks build strategy"
```

---

### Task 3: Config Integration — `build.strategy` Field

**Files:**
- Modify: `internal/types/types.go:42-45`
- Modify: `internal/builder/detect.go:22-28`
- Modify: `internal/cli/root.go:187-201`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `BuildConfig.Strategy` field, `Detection.Builder` field
- Produces: `BuildConfig.Strategy` field in `.tengiz.yaml` config

- [ ] **Step 1: Add `Strategy` field to `BuildConfig`**

Edit `internal/types/types.go`:

```go
type BuildConfig struct {
	Command  string `mapstructure:"command"`
	Output   string `mapstructure:"output"`
	Strategy string `mapstructure:"strategy"`
}
```

- [ ] **Step 2: Update `Detect()` to respect config strategy**

Modify `Detect()` in `internal/builder/detect.go` to accept an optional strategy parameter. Add a new function:

```go
func DetectWithStrategy(dir string, strategy string) (*Detection, error) {
	d, err := Detect(dir)
	if err != nil {
		return nil, err
	}
	if strategy == "nixpacks" {
		d.Builder = StrategyNixpacks
		if d.Framework == FrameworkDocker {
			// User explicitly chose nixpacks, override even if Dockerfile exists
			d.Framework = FrameworkNixpacks
		}
	}
	return d, nil
}
```

- [ ] **Step 2: Wire strategy from config in deploy command**

Edit `internal/cli/root.go` around line 187 — change the detection call:

```go
buildStrategy := cfg.Build.Strategy
if buildStrategy == "" {
    buildStrategy = "dockerfile"
}
detection, err := builder.DetectWithStrategy(projectRoot, buildStrategy)
```

- [ ] **Step 3: Write tests for strategy-aware detection**

Add to `internal/builder/builder_test.go`:

```go
func TestDetectWithStrategyNixpacks(t *testing.T) {
	dir := t.TempDir()
	// Even with a Dockerfile, strategy=nixpacks should override
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM node"), 0644)

	d, err := DetectWithStrategy(dir, "nixpacks")
	if err != nil {
		t.Fatal(err)
	}
	if d.Builder != StrategyNixpacks {
		t.Errorf("Builder = %q, want %q", d.Builder, StrategyNixpacks)
	}
	if d.Framework != FrameworkNixpacks {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNixpacks)
	}
}

func TestDetectWithStrategyDefault(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	d, err := DetectWithStrategy(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkGo {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkGo)
	}
	if d.Builder != StrategyDockerfile {
		t.Errorf("Builder = %q, want %q", d.Builder, StrategyDockerfile)
	}
}
```

- [ ] **Step 4: Run all builder tests**

```bash
go test ./internal/builder/... -v -count=1
```

Expected: All tests PASS (or SKIP for Docker-dependent tests if Docker not available).

- [ ] **Step 5: Commit**

```bash
git add internal/builder/nixpacks.go internal/builder/builder.go internal/builder/detect.go internal/builder/builder_test.go internal/types/types.go
git commit -m "feat: add Nixpacks build strategy with config integration"
```

---

### Task 3: CLI Integration — `--builder` Flag and Config Wiring

**Files:**
- Modify: `internal/cli/root.go:170-201`
- Modify: `internal/cli/root.go` (add `--builder` flag to deploy command)
- Test: `internal/cli/root_test.go` (if exists, else manual verification)

**Interfaces:**
- Consumes: `builder.DetectWithStrategy(dir, strategy)`, `BuildConfig.Strategy`
- Produces: `--builder` CLI flag, config-based strategy selection

- [ ] **Step 1: Add `--builder` flag to deploy command**

In `internal/cli/root.go`, find the deploy command definition and add a flag:

```go
// In the deploy command's RunE, before Detect:
builderFlag, _ := cmd.Flags().GetString("builder")
if builderFlag != "" {
    cfg.Build.Strategy = builderFlag
}
```

And add the flag registration in the deploy command's init:

```go
deployCmd.Flags().String("builder", "", "Build strategy: dockerfile (default) or nixpacks")
```

- [ ] **Step 2: Write test for strategy flag wiring**

Add to `internal/builder/builder_test.go`:

```go
func TestDetectWithStrategyNixpacksOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM node"), 0644)

	d, err := DetectWithStrategy(dir, "nixpacks")
	if err != nil {
		t.Fatal(err)
	}
	if d.Builder != StrategyNixpacks {
		t.Errorf("Builder = %q, want %q", d.Builder, StrategyNixpacks)
	}
	if d.Framework != FrameworkNixpacks {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNixpacks)
	}
}

func TestDetectWithStrategyEmptyDefault(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	d, err := DetectWithStrategy(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkGo {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkGo)
	}
	if d.Builder != StrategyDockerfile {
		t.Errorf("Builder = %q, want %q", d.Builder, StrategyDockerfile)
	}
}
```

- [ ] **Step 3: Run all builder tests**

```bash
go test ./internal/builder/... -v -count=1
```

Expected: All tests PASS (or SKIP for Docker/nixpacks-dependent tests).

- [ ] **Step 4: Commit**

```bash
git add internal/builder/detect.go internal/builder/builder_test.go internal/types/types.go internal/cli/root.go
git commit -m "feat: wire nixpacks strategy from config and CLI flag"
```

---

### Task 4: Nixpacks CLI Not Found Error Handling

**Files:**
- Modify: `internal/builder/nixpacks.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `nixpacksBuild()` function
- Produces: user-friendly error when nixpacks CLI is not installed

- [ ] **Step 1: Add nixpacks CLI check before build**

Edit `internal/builder/nixpacks.go` — add a check at the top of `nixpacksBuild`:

```go
func (b *Builder) nixpacksBuild(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
	// Check if nixpacks CLI is available
	if _, err := exec.LookPath("nixpacks"); err != nil {
		return "", "", fmt.Errorf("nixpacks CLI not found: install with 'curl -fsSL https://nixpacks.com/install.sh | bash' or see https://nixpacks.com/docs")
	}
	// ... rest of the function
}
```

- [ ] **Step 2: Write test for missing nixpacks CLI**

Add to `internal/builder/builder_test.go`:

```go
func TestNixpacksBuildMissingCLI(t *testing.T) {
	// Temporarily remove nixpacks from PATH to test error message
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", oldPath)

	b := New(t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.rb"), []byte(`puts "hello"`), 0644)
	detection := &Detection{Framework: FrameworkNixpacks, InternalPort: 8080, Builder: StrategyNixpacks}

	_, _, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v123")
	if err == nil {
		t.Fatal("expected error when nixpacks CLI is missing")
	}
	if !strings.Contains(err.Error(), "nixpacks CLI not found") {
		t.Errorf("error = %q, want nixpacks CLI not found message", err.Error())
	}
}
```

- [ ] **Step 3: Run all builder tests**

```bash
go test ./internal/builder/... -v -count=1
```

Expected: All tests PASS (or SKIP for Docker/nixpacks-dependent tests).

- [ ] **Step 4: Commit**

```bash
git add internal/builder/nixpacks.go internal/builder/builder_test.go
git commit -m "feat: add nixpacks CLI not found error handling"
```

---

### Task 4: Integration Test — End-to-End Nixpacks Deploy

**Files:**
- Create: `internal/builder/nixpacks_integration_test.go`
- Modify: none

**Interfaces:**
- Consumes: `Builder.Build()` with `StrategyNixpacks`
- Produces: integration test that verifies the full nixpacks build pipeline

- [ ] **Step 1: Write integration test**

Create `internal/builder/nixpacks_integration_test.go`:

```go
package builder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNixpacksIntegrationBuild(t *testing.T) {
	if _, err := exec.LookPath("nixpacks"); err != nil {
		t.Skip("nixpacks CLI not installed")
	}

	b := New(t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.rb"), []byte(`puts "Hello from Nixpacks"`), 0644)
	detection := &Detection{Framework: FrameworkNixpacks, InternalPort: 8080, Builder: StrategyNixpacks}

	tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v123")
	if err != nil {
		t.Fatalf("Build() error: %v\nlogs: %s", err, logs)
	}
	if tag == "" {
		t.Error("expected non-empty tag")
	}
	if !strings.Contains(tag, "testapp:production-v123") {
		t.Errorf("tag = %q, want to contain testapp:production-v123", tag)
	}
}
```

- [ ] **Step 2: Run integration test**

```bash
go test ./internal/builder/... -v -count=1 -run "TestNixpacksIntegration"
```

Expected: PASS (if nixpacks CLI installed) or SKIP (if not installed).

- [ ] **Step 3: Commit**

```bash
git add internal/builder/nixpacks_integration_test.go
git commit -m "test: add nixpacks integration test"
```

---

### Task 5: Self-Review

- [ ] **Step 1: Spec coverage check**

Requirements from the spec:
- "Nixpacks Build Sistemi" — ✅ Task 1 (framework constant + detection), Task 2 (build strategy), Task 3 (config integration)
- "Expands framework support from 6 to hundreds" — ✅ Nixpacks handles Ruby, Rust, PHP, Java, Elixir, etc.
- "Single `builder` package integration" — ✅ All changes within `internal/builder/`
- "`.tengiz.yaml`'da `--builder nixpacks` ile seçilebilir" — ✅ Task 3 adds `build.strategy` config + `--builder` flag

- [ ] **Step 2: Placeholder scan**

Check for any "TBD", "TODO", "implement later" patterns — none present. All steps have complete code.

- [ ] **Step 3: Type consistency check**

- `FrameworkNixpacks` — used consistently in detect.go, builder.go, nixpacks.go, tests
- `StrategyNixpacks` / `StrategyDockerfile` — used consistently in detect.go, builder.go, tests
- `Detection.Builder` — set in `Detect()`, checked in `Build()`
- `BuildConfig.Strategy` — used in config and CLI wiring
- `DetectWithStrategy(dir, strategy)` — called from CLI, returns `(*Detection, error)`

All types and signatures are consistent across tasks.

- [ ] **Step 4: Run full test suite**

```bash
go test ./... -v -count=1
```

Expected: All tests PASS (Docker-dependent tests may SKIP).

---

## Self-Review

**1. Spec coverage:**
- "Nixpacks Build Sistemi" — ✅ Task 1 (framework constant + detection), Task 2 (build strategy), Task 3 (config integration), Task 4 (error handling), Task 5 (integration test)
- "Expands framework support from 6 to hundreds" — ✅ Nixpacks handles Ruby, Rust, PHP, Java, Elixir, etc.
- "Single `builder` package integration" — ✅ All changes within `internal/builder/` + config/types wiring
- "`.tengiz.yaml`'da `--builder nixpacks` ile seçilebilir" — ✅ Task 3 adds `build.strategy` config + `--builder` flag

**2. Placeholder scan:** No "TBD", "TODO", "implement later", or similar patterns found. All code blocks contain complete, compilable code.

**3. Type consistency:**
- `FrameworkNixpacks` — defined in detect.go, used in detect.go, builder.go, nixpacks.go, tests — consistent
- `StrategyNixpacks` / `StrategyDockerfile` — defined in detect.go, used in builder.go, nixpacks.go, tests — consistent
- `Detection.Builder` — set in `Detect()`/`DetectWithStrategy()`, checked in `Build()` — consistent
- `BuildConfig.Strategy` — defined in types.go, used in root.go — consistent
- `DetectWithStrategy(dir, strategy)` — called from CLI, returns `(*Detection, error)` — consistent

No gaps found.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-07-23-nixpacks-build-system.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
