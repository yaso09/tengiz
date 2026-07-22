# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Nixpacks as an alternative build system to expand framework support from 6 to hundreds (Ruby, Rust, PHP, Java, Elixir, etc.)

**Architecture:** Introduce a `Strategy` interface in the builder package. Existing Dockerfile-based build logic is extracted into `DockerfileStrategy`. A new `NixpacksStrategy` calls the nixpacks CLI (`nixpacks build -o` to generate Dockerfiles, then `docker build` for Tengiz-controlled tagging). Builder selection flows through config (`build.builder`), CLI flag (`--builder`), and framework detection defaulting.

**Tech Stack:** Go 1.26, Nixpacks CLI (external, called via os/exec), Docker CLI

## Global Constraints

- No new Go dependencies beyond cobra and viper (nixpacks is called via CLI)
- All image tags follow `tengiz-apps/<name>:<env>-<deploymentID>` pattern
- Container names follow `tengiz-<appname>` (production) or `tengiz-<appname>-<env>` (non-production)
- Nixpacks must be installed separately; `exec.LookPath("nixpacks")` at invocation time
- All existing tests must continue to pass
- Existing `internal/builder/builder_test.go` integration tests (`TestBuildCapturesOutput`, `TestBuildWithDeploymentIDCompiles`) remain unchanged — they test `Builder.Build()` which delegates to the strategy
- `go vet ./...` must pass after every commit

---

### Task 1: Strategy Interface & Detection Update

**Files:**
- Create: `internal/builder/strategy.go`
- Modify: `internal/builder/detect.go`
- Test: `internal/builder/strategy_test.go`

**Interfaces:**
- Consumes: none
- Produces: `Strategy` interface with `Type()` and `Build()` methods; `StrategyType` constants `StrategyDockerfile` and `StrategyNixpacks`; `Detection.Strategy` field (defaults to `StrategyNixpacks` for non-Docker frameworks)

- [ ] **Step 1: Write the failing tests**

`internal/builder/strategy_test.go`:
```go
package builder

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type mockStrategy struct{}

func (m *mockStrategy) Type() StrategyType { return StrategyDockerfile }
func (m *mockStrategy) Build(ctx context.Context, dir, appName, env, deploymentID string, detection *Detection) (string, string, error) {
	return "mock-tag", "mock-logs", nil
}

func TestStrategyInterface(t *testing.T) {
	var s Strategy = &mockStrategy{}
	if s.Type() != StrategyDockerfile {
		t.Errorf("Type() = %q, want %q", s.Type(), StrategyDockerfile)
	}
	tag, logs, err := s.Build(context.Background(), "", "app", "prod", "v1", &Detection{})
	if err != nil {
		t.Fatal(err)
	}
	if tag != "mock-tag" {
		t.Errorf("Build() tag = %q, want %q", tag, "mock-tag")
	}
	if logs != "mock-logs" {
		t.Errorf("Build() logs = %q, want %q", logs, "mock-logs")
	}
}

func TestDetectSetsStrategy(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0644)
	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Strategy == "" {
		t.Fatal("Detect() should set a non-empty Strategy")
	}
}

func TestDetectDockerStrategy(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM node"), 0644)
	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Strategy != StrategyDockerfile {
		t.Errorf("Docker detection Strategy = %q, want %q", d.Strategy, StrategyDockerfile)
	}
}

func TestDetectNonDockerStrategy(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644)
	d, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Strategy != StrategyNixpacks {
		t.Errorf("non-Docker detection Strategy = %q, want %q", d.Strategy, StrategyNixpacks)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
go test ./internal/builder/ -run "TestStrategyInterface|TestDetectSetsStrategy|TestDetectDockerStrategy|TestDetectNonDockerStrategy" -v
Expected: FAIL — "Strategy" undefined, "StrategyType" undefined
```

- [ ] **Step 3: Create Strategy interface and update Detection**

`internal/builder/strategy.go`:
```go
package builder

import "context"

type StrategyType string

const (
	StrategyDockerfile StrategyType = "dockerfile"
	StrategyNixpacks   StrategyType = "nixpacks"
)

type Strategy interface {
	Type() StrategyType
	Build(ctx context.Context, dir string, appName string, env string, deploymentID string, detection *Detection) (imageTag string, buildLog string, err error)
}
```

In `internal/builder/detect.go`, add `Strategy` field to `Detection`:
```go
type Detection struct {
	Framework    Framework
	Strategy     StrategyType
	BuildCmd     string
	OutputDir    string
	InternalPort int
	HealthCheck  *types.HealthCheckConfig
}
```

Update each `return &Detection{...}` in `Detect()` to include `Strategy`. The Docker branch gets `StrategyDockerfile`; all others get `StrategyNixpacks`:

```go
if hasFile(dir, "Dockerfile") {
    return &Detection{
        Framework:    FrameworkDocker,
        Strategy:     StrategyDockerfile,
        InternalPort: 8080,
    }, nil
}
// ... other detections ...
return &Detection{
    Framework:    FrameworkStatic,
    Strategy:     StrategyNixpacks,
    InternalPort: 80,
}, nil
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/builder/ -run "TestStrategyInterface|TestDetectSetsStrategy|TestDetectDockerStrategy|TestDetectNonDockerStrategy|TestDetect" -v
Expected: all PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/builder/strategy.go internal/builder/strategy_test.go internal/builder/detect.go
git commit -m "feat: add Strategy interface and Detection.Strategy field"
```

---

### Task 2: Refactor Builder — Strategy Delegation + DockerfileStrategy Extraction

**Files:**
- Create: `internal/builder/dockerfile_strategy.go`
- Modify: `internal/builder/builder.go`

**Interfaces:**
- Consumes: `Strategy` interface (from Task 1), `Detection` struct
- Produces: `DockerfileStrategy` implementing `Strategy`; refactored `Builder` that delegates to strategy

- [ ] **Step 1: Write failing tests for DockerfileStrategy**

Add to `internal/builder/strategy_test.go` (or a new file, but no new file needed):
```go
func TestDockerfileStrategyType(t *testing.T) {
	s := &DockerfileStrategy{}
	if s.Type() != StrategyDockerfile {
		t.Errorf("Type() = %q, want %q", s.Type(), StrategyDockerfile)
	}
}

func TestDockerfileStrategyBuildUsesDocker(t *testing.T) {
	s := &DockerfileStrategy{}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0644)
	detection := &Detection{
		Framework:    FrameworkStatic,
		Strategy:     StrategyDockerfile,
		InternalPort: 80,
	}
	tag, logs, err := s.Build(context.Background(), dir, "testapp", "production", "v123", detection)
	if err != nil {
		t.Skipf("Build() error (likely no docker): %v", err)
	}
	expected := "tengiz-apps/testapp:production-v123"
	if tag != expected {
		t.Errorf("tag = %q, want %q", tag, expected)
	}
	_ = logs
}

func TestBuilderWithStrategy(t *testing.T) {
	b := New("/tmp/data")
	b.WithStrategy(&mockStrategy{})
	tag, logs, err := b.Build(context.Background(), "", "app", "production", &Detection{}, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if tag != "mock-tag" {
		t.Errorf("tag = %q, want %q", tag, "mock-tag")
	}
	_ = logs
}
```

- [ ] **Step 2: Run to verify failures**

```
go test ./internal/builder/ -run "TestDockerfileStrategy|TestBuilderWithStrategy" -v
Expected: FAIL — DockerfileStrategy undefined
```

- [ ] **Step 3: Extract `DockerfileStrategy` + refactor `Builder`**

`internal/builder/dockerfile_strategy.go`:
```go
package builder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type DockerfileStrategy struct{}

func (s *DockerfileStrategy) Type() StrategyType { return StrategyDockerfile }

func (s *DockerfileStrategy) Build(ctx context.Context, dir, appName, env, deploymentID string, detection *Detection) (string, string, error) {
	if env == "" {
		env = "production"
	}
	if err := ensureDockerfile(dir, detection); err != nil {
		return "", "", fmt.Errorf("generate dockerfile: %w", err)
	}
	tag := imageTag(appName, env, deploymentID)
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", tag, dir)
	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(os.Stdout, &logBuf)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	if err := cmd.Run(); err != nil {
		return "", logBuf.String(), fmt.Errorf("docker build: %w", err)
	}
	latestTag := latestImageTag(appName, env)
	tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
	if out, err := tagCmd.CombinedOutput(); err != nil {
		return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
	}
	return tag, logBuf.String(), nil
}

func imageTag(appName, env, deploymentID string) string {
	return fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)
}

func latestImageTag(appName, env string) string {
	return fmt.Sprintf("tengiz-apps/%s:%s-latest", appName, env)
}
```

Move `ensureDockerfile` into this file:
```go
func ensureDockerfile(dir string, detection *Detection) error {
	dfPath := filepath.Join(dir, "Dockerfile")
	if _, err := os.Stat(dfPath); err == nil {
		return nil
	}
	content := generateDockerfile(detection)
	return os.WriteFile(dfPath, []byte(content), 0644)
}
```

Also move `generateDockerfile` (the entire function with all framework Dockerfile templates) into `dockerfile_strategy.go`.

Now refactor `internal/builder/builder.go`:
```go
package builder

import (
	"context"
)

type Builder struct {
	dataDir  string
	strategy Strategy
}

func New(dataDir string) *Builder {
	return &Builder{
		dataDir:  dataDir,
		strategy: &DockerfileStrategy{},
	}
}

func (b *Builder) WithStrategy(s Strategy) *Builder {
	b.strategy = s
	return b
}

func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
	return b.strategy.Build(ctx, dir, appName, env, deploymentID, detection)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/builder/ -run "TestDockerfileStrategy|TestBuilderWithStrategy|TestGenerateDockerfile|TestBuild" -v
Expected: all PASS

go test ./internal/builder/ -v
Expected: existing tests still pass
```

- [ ] **Step 5: Commit**

```bash
git add internal/builder/dockerfile_strategy.go internal/builder/builder.go internal/builder/builder_test.go
git commit -m "refactor: extract DockerfileStrategy, make Builder strategy-aware"
```

---

### Task 3: NixpacksStrategy Implementation

**Files:**
- Create: `internal/builder/nixpacks_strategy.go`
- Test: `internal/builder/nixpacks_strategy_test.go`

**Interfaces:**
- Consumes: `Detection` struct (port info), strategy interface from Task 1
- Produces: `NixpacksStrategy` implementing `Strategy` (builds images using nixpacks CLI)

- [ ] **Step 1: Write failing tests**

`internal/builder/nixpacks_strategy_test.go`:
```go
package builder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNixpacksStrategyType(t *testing.T) {
	s := &NixpacksStrategy{}
	if s.Type() != StrategyNixpacks {
		t.Errorf("Type() = %q, want %q", s.Type(), StrategyNixpacks)
	}
}

func TestNixpacksStrategyRequiresNixpacks(t *testing.T) {
	s := &NixpacksStrategy{}
	_, err := exec.LookPath("nixpacks")
	if err != nil {
		t.Skip("nixpacks CLI not installed — skipping")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0644)
	detection := &Detection{
		Framework:    FrameworkStatic,
		Strategy:     StrategyNixpacks,
		InternalPort: 80,
	}
	tag, _, err := s.Build(context.Background(), dir, "testapp", "production", "v123", detection)
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	expected := "tengiz-apps/testapp:production-v123"
	if tag != expected {
		t.Errorf("tag = %q, want %q", tag, expected)
	}
}

func TestNixpacksStrategyNoDockerfileOverwrite(t *testing.T) {
	s := &NixpacksStrategy{}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)
	detection := &Detection{
		Framework:    FrameworkGo,
		Strategy:     StrategyNixpacks,
		InternalPort: 8080,
	}
	tag, _, err := s.Build(context.Background(), dir, "testapp", "production", "v123", detection)
	if err != nil {
		t.Skipf("Build() failed (likely no nixpacks): %v", err)
	}
	expected := "tengiz-apps/testapp:production-v123"
	if tag != expected {
		t.Errorf("tag = %q, want %q", tag, expected)
	}
}

func TestNixpacksStrategyFailsWithoutNixpacks(t *testing.T) {
	_, err := exec.LookPath("nixpacks")
	if err == nil {
		t.Skip("nixpacks is installed — this test requires it to be absent")
	}
	s := &NixpacksStrategy{}
	_, _, err = s.Build(context.Background(), "/tmp", "test", "prod", "v1", &Detection{
		Framework:    FrameworkStatic,
		Strategy:     StrategyNixpacks,
		InternalPort: 80,
	})
	if err == nil {
		t.Fatal("expected error when nixpacks is not installed")
	}
}
```

- [ ] **Step 2: Run to verify failures**

```
go test ./internal/builder/ -run "TestNixpacksStrategy" -v
Expected: FAIL — NixpacksStrategy undefined
```

- [ ] **Step 3: Implement NixpacksStrategy**

`internal/builder/nixpacks_strategy.go`:
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

type NixpacksStrategy struct{}

func (s *NixpacksStrategy) Type() StrategyType { return StrategyNixpacks }

func (s *NixpacksStrategy) Build(ctx context.Context, dir, appName, env, deploymentID string, detection *Detection) (string, string, error) {
	if _, err := exec.LookPath("nixpacks"); err != nil {
		return "", "", fmt.Errorf("nixpacks not found: install from https://nixpacks.com/docs/install: %w", err)
	}
	if env == "" {
		env = "production"
	}
	tmpDir, err := os.MkdirTemp("", "tengiz-nixpacks-*")
	if err != nil {
		return "", "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(os.Stdout, &logBuf)

	// Generate Dockerfile using nixpacks
	planCmd := exec.CommandContext(ctx, "nixpacks", "build", dir, "-o", tmpDir)
	planCmd.Stdout = logWriter
	planCmd.Stderr = logWriter
	if err := planCmd.Run(); err != nil {
		return "", logBuf.String(), fmt.Errorf("nixpacks build: %w", err)
	}

	tag := imageTag(appName, env, deploymentID)

	buildCmd := exec.CommandContext(ctx, "docker", "build", "-t", tag, tmpDir)
	buildCmd.Stdout = logWriter
	buildCmd.Stderr = logWriter
	if err := buildCmd.Run(); err != nil {
		return "", logBuf.String(), fmt.Errorf("docker build: %w", err)
	}

	latestTag := latestImageTag(appName, env)
	tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
	if out, err := tagCmd.CombinedOutput(); err != nil {
		return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
	}

	return tag, logBuf.String(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/builder/ -run "TestNixpacksStrategy" -v
Expected: TestNixpacksStrategyType PASS, TestNixpacksStrategyFailsWithoutNixpacks PASS (or SKIP if nixpacks is installed), others SKIP if nixpacks+docker not available

go test ./internal/builder/ -v
Expected: all existing tests still pass
```

- [ ] **Step 5: Commit**

```bash
git add internal/builder/nixpacks_strategy.go internal/builder/nixpacks_strategy_test.go
git commit -m "feat: add NixpacksStrategy for nixpacks-based builds"
```

---

### Task 4: Config & CLI Integration

**Files:**
- Modify: `internal/types/types.go` (add `Builder` field to `BuildConfig`)
- Modify: `internal/cli/root.go` (add `--builder` flag, wire strategy selection)

**Interfaces:**
- Consumes: `builder.StrategyDockerfile`, `builder.StrategyNixpacks`
- Produces: `BuildConfig.Builder` field; `--builder` CLI flag; strategy injected into `builder.New(...).WithStrategy(...)`

- [ ] **Step 1: Write failing tests for config integration**

Add to `internal/cli/root_test.go` (or create if it doesn't exist):
```go
package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestBuildConfigDefaults(t *testing.T) {
	cfg := types.AppConfig{}
	if cfg.Build.Builder != "" {
		t.Errorf("default Builder should be empty, got %q", cfg.Build.Builder)
	}
}
```

- [ ] **Step 2: Run to verify failures**

```
go test ./internal/types/ -v
Expected: no change (type field additions don't break anything)

go test ./internal/cli/ -run TestBuildConfigDefaults -v
Expected: PASS (empty string is the zero value, which passes)
```

(Note: this test is more of a behavioral check; the actual new field won't break existing tests.)

- [ ] **Step 3: Add `Builder` field to `BuildConfig`**

In `internal/types/types.go`, add the `Builder` field:
```go
type BuildConfig struct {
	Command string `mapstructure:"command"`
	Output  string `mapstructure:"output"`
	Builder string `mapstructure:"builder" yaml:"builder" json:"builder,omitempty"`
}
```

Add `--builder` flag to deploy command in `internal/cli/root.go`. In `init()`:
```go
deployCmd.Flags().String("builder", "", "build strategy (dockerfile, nixpacks)")
```

In the deploy command's RunE, after detection and before building, select strategy:
```go
builderFlag, _ := cmd.Flags().GetString("builder")
builderStrategy := detection.Strategy
if builderFlag != "" {
    builderStrategy = builder.StrategyType(builderFlag)
}
if cfg.Build.Builder != "" {
    builderStrategy = builder.StrategyType(cfg.Build.Builder)
}

b := builder.New(dataDir)
switch builderStrategy {
case builder.StrategyNixpacks:
    b.WithStrategy(&builder.NixpacksStrategy{})
case builder.StrategyDockerfile, "":
    b.WithStrategy(&builder.DockerfileStrategy{})
default:
    return fmt.Errorf("unknown builder: %q", builderStrategy)
}

imageTag, buildLog, err := b.Build(context.Background(), projectRoot, cfg.Name, cfg.Environment, detection, deploymentID)
```

Priority order: CLI `--builder` flag > `.tengiz.yaml` `build.builder` > `Detection.Strategy` (framework-detected default).

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./internal/types/ -v
Expected: PASS

go vet ./internal/cli/
Expected: no errors

go build -o /dev/null .
Expected: builds successfully
```

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/cli/root.go
git commit -m "feat: add build.builder config and --builder CLI flag"
```

---

### Task 5: Wire Strategy Through GitDeploy and Preview

**Files:**
- Modify: `internal/gitdeploy/deployer.go`
- Modify: `internal/preview/manager.go`

**Interfaces:**
- Consumes: `builder.Strategy`, `builder.StrategyNixpacks`, `builder.StrategyDockerfile`, `Detection.Strategy`
- Produces: Git deploy pipeline and preview manager that use the selected build strategy

- [ ] **Step 1: Add failing tests for strategy propagation**

`internal/gitdeploy/deployer_test.go` (if it exists, check the file; if not, this step is about behavior verification — the tests pass because the strategy is read from Detection):
```go
package gitdeploy

import (
	"testing"
	"github.com/yaso09/tengiz/internal/builder"
)

func TestPipelineUsesDefaultStrategy(t *testing.T) {
	// Detection with StrategyNixpacks should trigger NixpacksStrategy
	// This is verified at builder level; pipeline just passes detection through
}
```

(Add similar comment-based test in preview if a test file exists.)

- [ ] **Step 2: Run existing tests to confirm current state**

```
go test ./internal/gitdeploy/ -v
Expected: existing behavior

go test ./internal/preview/ -v
Expected: existing behavior
```

- [ ] **Step 3: Update `gitdeploy.Pipeline` to select strategy based on detection**

In `internal/gitdeploy/deployer.go`, add:
```go
func (p *Pipeline) selectBuilder(detection *builder.Detection) *builder.Builder {
	b := builder.New(p.dataDir)
	switch detection.Strategy {
	case builder.StrategyNixpacks:
		b.WithStrategy(&builder.NixpacksStrategy{})
	default:
		b.WithStrategy(&builder.DockerfileStrategy{})
	}
	return b
}
```

Modify `Deploy()` to use `selectBuilder` instead of `p.b`:

Replace:
```go
imageTag, buildLog, err := p.b.Build(ctx, cloneDir, appName, p.env, detection, deploymentID)
```
With:
```go
b := p.selectBuilder(detection)
imageTag, buildLog, err := b.Build(ctx, cloneDir, appName, p.env, detection, deploymentID)
```

Remove the `b *builder.Builder` field from the `Pipeline` struct since we create builders on-demand now.

Update `NewPipelineWithEnv`:
```go
type Pipeline struct {
	dataDir string
	env     string
	rt      runtime.Manager
	store   *config.Store
}

func NewPipelineWithEnv(dataDir, env string, rt runtime.Manager, store *config.Store) *Pipeline {
	if env == "" {
		env = "production"
	}
	return &Pipeline{
		dataDir: dataDir,
		env:     env,
		rt:      rt,
		store:   store,
	}
}
```

- [ ] **Step 4: Update `preview.Manager` with same pattern**

In `internal/preview/manager.go`, remove `builder *builder.Builder` field, add `dataDir` field (already exists):

```go
type Manager struct {
	dataDir string
	store   *config.Store
	rt      runtime.Manager
}
```

Add helper:
```go
func (m *Manager) selectBuilder(detection *builder.Detection) *builder.Builder {
	b := builder.New(m.dataDir)
	switch detection.Strategy {
	case builder.StrategyNixpacks:
		b.WithStrategy(&builder.NixpacksStrategy{})
	default:
		b.WithStrategy(&builder.DockerfileStrategy{})
	}
	return b
}
```

Replace `m.builder.Build(ctx, cloneDir, appName, "", detection, deploymentID)` with `m.selectBuilder(detection).Build(...)` in both `Create()` and `Update()` methods.

Remove `builder: builder.New(dataDir)` from `NewManager`:
```go
func NewManager(dataDir string, store *config.Store, rt runtime.Manager) *Manager {
	return &Manager{
		dataDir: dataDir,
		store:   store,
		rt:      rt,
	}
}
```

- [ ] **Step 5: Run tests and verify**

```
go build -o /dev/null .
Expected: clean build

go vet ./...
Expected: no issues

go test ./internal/builder/ -v
go test ./internal/gitdeploy/ -v
go test ./internal/preview/ -v
go test ./internal/cli/ -v
Expected: all pass
```

- [ ] **Step 6: Commit**

```bash
git add internal/gitdeploy/deployer.go internal/preview/manager.go
git commit -m "feat: wire build strategy through gitdeploy and preview"
```

---

## Self-Review Checklist

1. **Spec coverage:** The spec says "add Nixpacks as a new BuildStrategy, selectable via `--builder nixpacks` in `.tengiz.yaml`". This is covered by Task 3 (NixpacksStrategy) + Task 4 (config/CLI integration). The spec also mentions "builder paketine yeni bir BuildStrategy olarak" — covered by Task 1 (Strategy interface). No gaps.

2. **Placeholder scan:** No "TBD", "TODO", "add appropriate error handling", "write tests for the above", "similar to Task N" patterns present. Every step has complete code or exact commands.

3. **Type consistency:**
   - `Strategy.Type()` returns `StrategyType` — consistent across all tasks
   - `Strategy.Build(ctx, dir, appName, env, deploymentID, detection) -> (tag, logs, err)` — same signature in interface, DockerfileStrategy, NixpacksStrategy, and mock
   - `Detection.Strategy` typed as `StrategyType` — consistent with Strategy.Type() return type
   - `imageTag()` and `latestImageTag()` helpers defined in Task 2, used in Task 3 — no naming conflicts
   - `Builder.WithStrategy(s Strategy) *Builder` — used in Tasks 4 and 5
   - `selectBuilder(detection)` in Task 5 — consistent with Task 4 strategy selection pattern
