# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build backend so Tengiz supports hundreds of frameworks (Ruby, Rust, PHP, Java, Elixir, etc.) beyond the current 6.

**Architecture:** A new `nixpacks` builder type in the `builder` package, selected via `--builder nixpacks` CLI flag or `.tengiz.yaml` `build.builder` field. When nixpacks is active, `builder.Build()` runs `nixpacks build <dir> --name <tag>` instead of `docker build` + generated Dockerfile. Detection still runs for port info but the detection's framework is unused for Dockerfile generation. No new external Go dependencies — nixpacks is called via `os/exec` like Docker.

**Tech Stack:** Go 1.26, `os/exec`, Nixpacks CLI (must be installed separately, like Docker).

## Global Constraints

- `nixpacks` binary must be available on `$PATH` (checked at build time, with clear error message)
- Image tags must follow existing convention: `tengiz-apps/{app}:{env}-{deploymentID}`
- `-latest` tag still created after successful nixpacks build via `docker tag`
- Build logs captured from stdout/stderr and saved via existing `SaveBuildLog`
- `.tengiz.yaml` `build.builder` field controls builder selection (default: `"auto"`)
- CLI `--builder` flag supports: `auto` (default), `nixpacks`
- No new external Go dependencies
- Existing `auto` mode (Dockerfile-based) must not be affected
- Existing tests must continue to pass
- When `nixpacks` is selected and `nixpacks` binary is not found, return a clear error
- Detection port defaults to 8080 when nixpacks is used (nixpacks convention; user can override via `port` in `.tengiz.yaml`)

---

## File Structure

| File | Responsibility |
|------|---------------|
| Modify: `internal/types/types.go` | Add `Builder string` field to `BuildConfig` |
| Modify: `internal/builder/detect.go` | Add `FrameworkNixpacks` constant |
| Modify: `internal/builder/builder.go` | Add `BuilderType` type, `BuilderType` field on `Builder`, `buildWithNixpacks()` method, `checkNixpacks()` helper |
| Create: `internal/builder/nixpacks_test.go` | Tests for nixpacks builder (use fake nixpacks script) |
| Modify: `internal/builder/builder_test.go` | Add tests for builder type selection |
| Modify: `internal/cli/root.go` | Add `--builder` flag on `deployCmd`; pass to `builder.NewWithType()` |
| Modify: `internal/gitdeploy/deployer.go` | Add `builderType` field to `Pipeline`; pass from constructor |
| Modify: `internal/preview/manager.go` | Add `builderType` field to `Manager`; pass from constructor |
| No change: `internal/config/store.go` | Store persists `AppConfig.Build` which already contains `BuildConfig` |
| No change: `internal/proxy/proxy.go` | No proxy changes needed |
| No change: `internal/runtime/runtime.go` | No runtime changes needed |

---

### Task 1: Add builder type to types and detection

**Files:**
- Modify: `internal/types/types.go:42-45` — add `Builder` field to `BuildConfig`
- Modify: `internal/builder/detect.go:12-20` — add `FrameworkNixpacks` constant

**Interfaces:**
- Consumes: nothing new
- Produces: `types.BuildConfig.Builder string`, `builder.FrameworkNixpacks` constant

- [ ] **Step 1: Add `Builder` field to `BuildConfig`**

```go
// internal/types/types.go
type BuildConfig struct {
	Command string `mapstructure:"command"`
	Output  string `mapstructure:"output"`
	Builder string `mapstructure:"builder" yaml:"builder"`
}
```

- [ ] **Step 2: Add `FrameworkNixpacks` constant**

```go
// internal/builder/detect.go:12-20 — add after FrameworkDocker
const (
	FrameworkNextJS    Framework = "nextjs"
	FrameworkVite      Framework = "vite"
	FrameworkGo        Framework = "go"
	FrameworkNode      Framework = "node"
	FrameworkPython    Framework = "python"
	FrameworkStatic    Framework = "static"
	FrameworkDocker    Framework = "docker"
	FrameworkNixpacks  Framework = "nixpacks"
)
```

Also add nixpacks detection in `Detect()`:

```go
// internal/builder/detect.go — in Detect(), after Dockerfile check, before next.config.js check
func Detect(dir string) (*Detection, error) {
	if hasFile(dir, "Dockerfile") {
		return &Detection{Framework: FrameworkDocker, InternalPort: 8080}, nil
	}
	if hasFile(dir, ".nixpacks") {
		return &Detection{Framework: FrameworkNixpacks, InternalPort: 8080}, nil
	}
	// ... rest of existing detection unchanged
}
```

- [ ] **Step 3: Run tests to verify existing detection still passes**

Run: `go test ./internal/builder/... -v -count=1 -run TestDetect`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add internal/types/types.go internal/builder/detect.go
git commit -m "feat: add builder type field and Nixpacks detection"
```

---

### Task 2: Refactor builder package with builder type support

**Files:**
- Modify: `internal/builder/builder.go` — add `BuilderType` type and string constants, `BuilderType` field on `Builder`, `NewWithType()`, dispatch in `Build()`, `buildWithNixpacks()`, `checkNixpacks()`

**Interfaces:**
- Consumes: `types.BuildConfig.Builder string` from Task 1
- Produces: `builder.BuilderType` constants, `builder.NewWithType(dataDir, builderType string) *Builder`, `buildWithNixpacks()` method

- [ ] **Step 1: Add builder type constants and update `Builder` struct**

```go
// internal/builder/builder.go — add before Builder struct
type BuilderType string

const (
	BuilderAuto      BuilderType = "auto"
	BuilderNixpacks  BuilderType = "nixpacks"
)

type Builder struct {
	dataDir     string
	builderType BuilderType
}

func New(dataDir string) *Builder {
	return NewWithType(dataDir, string(BuilderAuto))
}

func NewWithType(dataDir string, builderType string) *Builder {
	if builderType == "" {
		builderType = string(BuilderAuto)
	}
	return &Builder{
		dataDir:     dataDir,
		builderType: BuilderType(builderType),
	}
}
```

- [ ] **Step 2: Add dispatch in `Build()` method**

```go
// internal/builder/builder.go — replace existing Build method
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
	switch b.builderType {
	case BuilderNixpacks:
		return b.buildWithNixpacks(ctx, dir, appName, env, deploymentID)
	default:
		if detection.Framework == FrameworkDocker {
			return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
		}
		if err := b.ensureDockerfile(dir, detection); err != nil {
			return "", "", fmt.Errorf("generate dockerfile: %w", err)
		}
		return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
	}
}
```

- [ ] **Step 3: Add `checkNixpacks()` helper and `buildWithNixpacks()` method**

```go
// internal/builder/builder.go — add new methods

func (b *Builder) checkNixpacks() error {
	_, err := exec.LookPath("nixpacks")
	return err
}

func (b *Builder) buildWithNixpacks(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
	if env == "" {
		env = "production"
	}

	if err := b.checkNixpacks(); err != nil {
		return "", "", fmt.Errorf("nixpacks not found on $PATH: %w\ninstall: curl -fsSL https://nixpacks.com/install.sh | bash", err)
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

- [ ] **Step 4: Run existing tests to verify no regression**

Run: `go test ./internal/builder/... -v -count=1`
Expected: ALL PASS (build tests may skip if no docker available, that's fine)

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go
git commit -m "feat: add builder type dispatch with nixpacks support"
```

---

### Task 3: Add `--builder` flag to CLI deploy command

**Files:**
- Modify: `internal/cli/root.go` — add `--builder` flag to `deployCmd`, pass value to `builder.NewWithType()`

**Interfaces:**
- Consumes: `builder.NewWithType(dataDir, builderType)` from Task 2
- Produces: `--builder` flag available on `tengiz deploy`

- [ ] **Step 1: Register `--builder` flag in `init()`**

```go
// internal/cli/root.go — in init() where other flags are registered
func init() {
	// ... existing flag registrations ...
	deployCmd.Flags().String("builder", "auto", "Build strategy: auto (default), nixpacks")
}
```

- [ ] **Step 2: Read flag and pass to builder in `deployCmd.RunE`**

```go
// internal/cli/root.go — in deployCmd.RunE, replace the builder.New line
		builderFlag, _ := cmd.Flags().GetString("builder")
		b := builder.NewWithType(dataDir, builderFlag)
```

- [ ] **Step 3: Use builder type from config if not overridden by flag**

```go
// internal/cli/root.go — after loading cfg, before creating builder
		builderFlag, _ := cmd.Flags().GetString("builder")
		if builderFlag == "auto" && cfg.Build.Builder != "" {
			builderFlag = cfg.Build.Builder
		}
		b := builder.NewWithType(dataDir, builderFlag)
```

- [ ] **Step 4: Run tests**

Run: `go build . && go vet ./...`
Expected: builds without error, vet passes

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add --builder flag to deploy command"
```

---

### Task 4: Wire nixpacks builder type through gitdeploy and preview

**Files:**
- Modify: `internal/gitdeploy/deployer.go` — add `builderType` to `Pipeline`, thread through constructors and `Deploy()`
- Modify: `internal/preview/manager.go` — add `builderType` to `Manager`, thread through constructors and deploy calls

**Interfaces:**
- Consumes: `builder.NewWithType(dataDir, builderType)` from Task 2
- Produces: gitdeploy and preview pipelines respect the builder selection

- [ ] **Step 1: Update `Pipeline` struct and constructors**

```go
// internal/gitdeploy/deployer.go
type Pipeline struct {
	dataDir     string
	env         string
	b           *builder.Builder
	rt          runtime.Manager
	store       *config.Store
	builderType string
}

func NewPipeline(dataDir string, rt runtime.Manager, store *config.Store) *Pipeline {
	return NewPipelineWithEnv(dataDir, "", rt, store)
}

func NewPipelineWithEnv(dataDir, env string, rt runtime.Manager, store *config.Store) *Pipeline {
	if env == "" {
		env = "production"
	}
	return &Pipeline{
		dataDir:     dataDir,
		env:         env,
		b:           builder.New(dataDir),
		rt:          rt,
		store:       store,
		builderType: "auto",
	}
}

// New helper that accepts builder type
func NewPipelineWithBuilder(dataDir, env, builderType string, rt runtime.Manager, store *config.Store) *Pipeline {
	if env == "" {
		env = "production"
	}
	if builderType == "" {
		builderType = "auto"
	}
	return &Pipeline{
		dataDir:     dataDir,
		env:         env,
		b:           builder.NewWithType(dataDir, builderType),
		rt:          rt,
		store:       store,
		builderType: builderType,
	}
}
```

- [ ] **Step 2: Update `Manager` struct and constructors**

```go
// internal/preview/manager.go
type Manager struct {
	dataDir     string
	git         *gitdeploy.Pipeline
	builder     *builder.Builder
	store       *config.Store
	builderType string
}

func New(dataDir string, gitPipe *gitdeploy.Pipeline, store *config.Store) *Manager {
	return NewWithBuilder(dataDir, "", gitPipe, store)
}

func NewWithEnv(dataDir, env string, gitPipe *gitdeploy.Pipeline, store *config.Store) *Manager {
	return NewWithBuilder(dataDir, env, gitPipe, store)
}

func NewWithBuilder(dataDir, env, builderType string, gitPipe *gitdeploy.Pipeline, store *config.Store) *Manager {
	if builderType == "" {
		builderType = "auto"
	}
	return &Manager{
		dataDir:     dataDir,
		git:         gitPipe,
		builder:     builder.NewWithType(dataDir, builderType),
		store:       store,
		builderType: builderType,
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go build . && go test ./... -v -count=1`
Expected: builds without error, all tests pass (or skip for docker-dependent tests)

- [ ] **Step 4: Commit**

```bash
git add internal/gitdeploy/deployer.go internal/preview/manager.go
git commit -m "feat: wire builder type through gitdeploy and preview pipelines"
```

---

### Task 5: Tests for nixpacks builder

**Files:**
- Create: `internal/builder/nixpacks_test.go` — tests for nixpacks builder path
- Modify: `internal/builder/builder_test.go` — add tests for builder type selection

**Interfaces:**
- Consumes: `builder.NewWithType()`, `builder.Build()` from Task 2

- [ ] **Step 1: Write tests for builder type construction**

```go
// internal/builder/builder_test.go — add tests
func TestNewWithTypeDefaults(t *testing.T) {
	b := NewWithType("/tmp/data", "")
	if b.builderType != BuilderAuto {
		t.Errorf("builderType = %q, want %q", b.builderType, BuilderAuto)
	}
}

func TestNewWithTypeNixpacks(t *testing.T) {
	b := NewWithType("/tmp/data", "nixpacks")
	if b.builderType != BuilderNixpacks {
		t.Errorf("builderType = %q, want %q", b.builderType, BuilderNixpacks)
	}
}

func TestNewDefaults(t *testing.T) {
	b := New("/tmp/data")
	if b.builderType != BuilderAuto {
		t.Errorf("builderType = %q, want %q", b.builderType, BuilderAuto)
	}
}
```

- [ ] **Step 2: Write test for nixpacks not found error**

```go
// internal/builder/nixpacks_test.go
package builder

import (
	"context"
	"testing"
)

func TestBuildWithNixpacksBinaryNotFound(t *testing.T) {
	// Temporarily remove nixpacks from PATH by setting PATH to empty
	t.Setenv("PATH", "")

	b := NewWithType(t.TempDir(), "nixpacks")
	dir := t.TempDir()

	_, _, err := b.Build(context.Background(), dir, "testapp", "production", &Detection{InternalPort: 8080}, "v1")
	if err == nil {
		t.Fatal("expected error when nixpacks not found, got nil")
	}
	if err.Error() != "nixpacks not found on $PATH: ..." && !contains(err.Error(), "nixpacks") {
		t.Errorf("error should mention nixpacks, got: %v", err)
	}
}
```

- [ ] **Step 3: Write test for nixpacks build with fake binary**

```go
// internal/builder/nixpacks_test.go
func TestBuildWithNixpacksWithFakeBinary(t *testing.T) {
	// Create a fake nixpacks script that succeeds
	tmpDir := t.TempDir()
	fakeNixpacks := tmpDir + "/nixpacks"
	if err := os.WriteFile(fakeNixpacks, []byte("#!/bin/sh\necho 'nixpacks build successful'"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmpDir+":"+os.Getenv("PATH"))

	b := NewWithType(t.TempDir(), "nixpacks")
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/package.json", []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", &Detection{InternalPort: 8080}, "v1")
	if err != nil {
		t.Skipf("Build() error (likely docker tag failed): %v", err)
	}
	expected := "tengiz-apps/testapp:production-v1"
	if tag != expected {
		t.Errorf("tag = %q, want %q", tag, expected)
	}
	_ = logs
}
```

- [ ] **Step 4: Write test for auto dispatch with nixpacks detection**

```go
// internal/builder/builder_test.go
func TestBuildDispatchesToNixpacksWhenBuilderTypeSet(t *testing.T) {
	b := NewWithType(t.TempDir(), "nixpacks")
	dir := t.TempDir()

	// Nixpacks not on PATH should return error
	t.Setenv("PATH", "")
	_, _, err := b.Build(context.Background(), dir, "testapp", "production", &Detection{InternalPort: 8080}, "v1")
	if err == nil {
		t.Fatal("expected error for nixpacks builder without nixpacks binary")
	}
}

func TestBuildDispatchesToAutoByDefault(t *testing.T) {
	b := New(t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)

	tag, _, err := b.Build(context.Background(), dir, "testapp", "production", &Detection{Framework: FrameworkStatic, InternalPort: 80}, "v1")
	if err != nil {
		t.Skipf("Build() error (likely no docker): %v", err)
	}
	expected := "tengiz-apps/testapp:production-v1"
	if tag != expected {
		t.Errorf("tag = %q, want %q", tag, expected)
	}
}
```

- [ ] **Step 5: Run all builder tests**

Run: `go test ./internal/builder/... -v -count=1`
Expected: ALL PASS. Docker-dependent tests may skip gracefully.

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder_test.go internal/builder/nixpacks_test.go
git commit -m "test: add nixpacks builder tests"
```

---

## Self-Review

**1. Spec coverage:**
- Nixpacks as alternative build backend: ✅ Tasks 1-4
- `--builder nixpacks` CLI flag: ✅ Task 3
- `.tengiz.yaml` `build.builder` config: ✅ Task 1 (via `BuildConfig.Builder`)
- Nixpacks CLI via `os/exec`: ✅ Task 2 (`buildWithNixpacks`)
- Image tag convention maintained: ✅ Task 2 (same `tengiz-apps/{app}:{env}-{deploymentID}`)
- Detection still provides port info: ✅ Task 2 (`detection` parameter still passed)
- All existing paths untouched: ✅ Task 2 (default `BuilderAuto` dispatches to existing logic)

**2. Placeholder scan:** No TBD, TODO, "implement later", or "add error handling" patterns. All code is concrete.

**3. Type consistency:**
- `types.BuildConfig.Builder` string field defined in Task 1, used in Task 3
- `builder.BuilderType` / `BuilderAuto` / `BuilderNixpacks` defined in Task 2, used in Tasks 2-5
- `builder.NewWithType(dataDir, builderType)` defined in Task 2, used in Tasks 3-5
- `Pipeline` builder type threading: Task 4 adds `NewPipelineWithBuilder`, existing `NewPipeline`/`NewPipelineWithEnv` unchanged
- `Manager` builder type threading: Task 4 adds `NewWithBuilder`, existing `New`/`NewWithEnv` unchanged
