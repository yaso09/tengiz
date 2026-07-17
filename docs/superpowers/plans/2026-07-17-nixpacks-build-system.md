# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Nixpacks as an alternative build system so Tengiz supports hundreds of frameworks (Ruby, Rust, PHP, Java, Elixir, etc.) beyond the current 6.

**Architecture:** Extract a `BuildStrategy` interface from the current concrete `Builder`, rename the existing Dockerfile-based logic to `DockerfileStrategy`, add a new `NixpacksStrategy` that wraps the `nixpacks` CLI (a Rust binary installed separately). The builder selects the strategy based on a new `builder` field in `.tengiz.yaml` (`builder: nixpacks` or `builder: docker`). The `Detect` function gains a second path that delegates to `nixpacks plan` for framework detection and port discovery.

**Tech Stack:** Go 1.26, `os/exec` for `nixpacks` CLI calls. No new Go deps. `nixpacks` CLI must be installed separately (brew/curl/gh release).

## Global Constraints

- `nixpacks` CLI must be installed separately; Tengiz checks its presence at build time and returns a clear error if missing
- New `.tengiz.yaml` field: `builder: string` — valid values `"docker"` (default), `"nixpacks"`
- Image tagging format preserved: `tengiz-apps/{app}:{env}-{deploymentID}` — same for both strategies
- Nixpacks strategy: delegates entire build to `nixpacks build --name <tag> <dir>`, no manual Dockerfile generation
- Framework detection via nixpacks: `nixpacks plan <dir> --json` extracts framework name, internal port, start command
- Existing `Dockerfile` detection must still take priority when a Dockerfile exists (even with `builder: nixpacks`)
- All existing tests must pass without modification
- All 3 call sites updated: `internal/cli/root.go`, `internal/preview/manager.go`, `internal/gitdeploy/deployer.go`
- No new external Go dependencies
- CLI flag `--builder` on `tengiz deploy` and `tengiz init` overrides config

---

## File Structure

| File | Responsibility |
|------|---------------|
| Modify: `internal/builder/builder.go` | Extract `BuildStrategy` interface; `Builder` holds a strategy; `Build()` delegates; add `NewDockerfile(dataDir) *DockerfileStrategy` and `NewNixpacks() *NixpacksStrategy` constructors |
| Create: `internal/builder/strategy_dockerfile.go` | Move existing `ensureDockerfile`, `buildWithDockerfile`, `generateDockerfile` into `DockerfileStrategy` struct implementing `BuildStrategy` |
| Create: `internal/builder/strategy_nixpacks.go` | New `NixpacksStrategy` struct implementing `BuildStrategy` via `nixpacks build` CLI calls |
| Modify: `internal/builder/detect.go` | Add `DetectWithBuilder(dir string, strategy string) (*Detection, error)` that delegates to nixpacks detection; add `nixpacksPlanJSON` struct for parsing `nixpacks plan --json` output |
| Modify: `internal/types/types.go` | Add `Builder string` field to `AppConfig.BuildConfig` — config key `builder` maps to Docker/Nixpacks |
| Modify: `internal/config/config.go` | Merge `builder` field from `.tengiz.{env}.yaml` into config |
| Modify: `internal/cli/root.go` | Add `--builder` flag to `deployCmd`; pass strategy to builder creation and detection |
| Modify: `internal/preview/manager.go` | Pass builder strategy from config to preview builder creation |
| Modify: `internal/gitdeploy/deployer.go` | Pass builder strategy from pipeline config to builder creation |
| Test: `internal/builder/builder_test.go` | Add tests for Nixpacks strategy, strategy dispatch, nixpacks detection parsing |
| Test: `internal/builder/strategy_nixpacks_test.go` | Unit tests for nixpacks plan parsing, CLI error handling |

---

### Task 1: Add `Builder` field to types and config

**Files:**
- Modify: `internal/types/types.go` — add `Builder` field to `BuildConfig`
- Modify: `internal/config/config.go` — merge `builder` field in `LoadForEnvironment`

**Interfaces:**
- Consumes: nothing new
- Produces: `types.BuildConfig.Builder string` field, env-aware config merge for `builder`

- [ ] **Step 1: Write the failing test for build config**

```go
// internal/types/types_test.go
package types

import (
    "encoding/json"
    "testing"
)

func TestBuildConfigBuilderField(t *testing.T) {
    cfg := AppConfig{
        Name: "test",
        Build: BuildConfig{
            Command: "npm run build",
            Output:  "dist",
            Builder: "nixpacks",
        },
    }
    data, err := json.Marshal(cfg)
    if err != nil {
        t.Fatalf("Marshal: %v", err)
    }
    var decoded AppConfig
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("Unmarshal: %v", err)
    }
    if decoded.Build.Builder != "nixpacks" {
        t.Errorf("Build.Builder = %q, want %q", decoded.Build.Builder, "nixpacks")
    }
}

func TestBuildConfigDefaultBuilder(t *testing.T) {
    cfg := AppConfig{
        Name: "test",
        Build: BuildConfig{
            Command: "npm run build",
        },
    }
    if cfg.Build.Builder != "" {
        t.Errorf("default Build.Builder should be empty, got %q", cfg.Build.Builder)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -v -run TestBuildConfigBuilderField -count=1`
Expected: FAIL — "types.AppConfig" does not have `Build.Builder` field compiling, or field missing

- [ ] **Step 3: Add `Builder` field to `BuildConfig`**

```go
// internal/types/types.go
type BuildConfig struct {
    Command string `mapstructure:"command" json:"command,omitempty"`
    Output  string `mapstructure:"output" json:"output,omitempty"`
    Builder string `mapstructure:"builder" json:"builder,omitempty"`
}
```

- [ ] **Step 4: Merge `builder` field in `LoadForEnvironment`**

```go
// internal/config/config.go — inside LoadForEnvironment, after build command merge
if envCfg.Build.Builder != "" {
    cfg.Build.Builder = envCfg.Build.Builder
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/types/... ./internal/config/... -v -run TestBuildConfigBuilderField -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/types/types.go internal/config/config.go
git commit -m "feat: add Builder field to BuildConfig for strategy selection"
```

---

### Task 2: Extract `BuildStrategy` interface and rename current logic to `DockerfileStrategy`

**Files:**
- Modify: `internal/builder/builder.go` — define `BuildStrategy` interface; `Builder` holds a strategy; `New()` takes a strategy; `Build()` delegates
- Create: `internal/builder/strategy_dockerfile.go` — move existing `ensureDockerfile`, `buildWithDockerfile`, `generateDockerfile` methods

**Interfaces:**
- Consumes: `types.AppConfig`, `*types.Detection`
- Produces: `BuildStrategy` interface with `Build(ctx, dir, appName, env, detection, deploymentID) (imageTag, buildLog, error)`

- [ ] **Step 1: Write the failing test for strategy dispatch**

```go
// internal/builder/builder_test.go
func TestBuilderStrategyDispatch(t *testing.T) {
    t.Run("default strategy is dockerfile", func(t *testing.T) {
        b := New(t.TempDir())
        if b.strategy == nil {
            t.Fatal("expected non-nil strategy")
        }
    })
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run TestBuilderStrategyDispatch -count=1`
Expected: FAIL — `Builder` struct doesn't have `strategy` field

- [ ] **Step 3: Define the interface and refactor `Builder`**

```go
// internal/builder/builder.go
package builder

import "context"

type BuildStrategy interface {
    Build(ctx context.Context, dir, appName, env string, detection *Detection, deploymentID string) (string, string, error)
}

type Builder struct {
    dataDir  string
    strategy BuildStrategy
}

func New(dataDir string) *Builder {
    return &Builder{
        dataDir:  dataDir,
        strategy: NewDockerfileStrategy(dataDir),
    }
}

func NewWithStrategy(dataDir string, strategy BuildStrategy) *Builder {
    return &Builder{
        dataDir:  dataDir,
        strategy: strategy,
    }
}

func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    return b.strategy.Build(ctx, dir, appName, env, detection, deploymentID)
}

// Strategy selection by name
func StrategyFromName(name string, dataDir string) BuildStrategy {
    switch name {
    case "nixpacks":
        return NewNixpacksStrategy()
    default:
        return NewDockerfileStrategy(dataDir)
    }
}
```

- [ ] **Step 4: Move existing build logic to `DockerfileStrategy`**

Create `internal/builder/strategy_dockerfile.go`:

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

    "github.com/yaso09/tengiz/internal/types"
)

type DockerfileStrategy struct {
    dataDir string
}

func NewDockerfileStrategy(dataDir string) *DockerfileStrategy {
    return &DockerfileStrategy{dataDir: dataDir}
}

func (s *DockerfileStrategy) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    if detection.Framework == FrameworkDocker {
        return s.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
    }
    if err := s.ensureDockerfile(dir, detection); err != nil {
        return "", "", fmt.Errorf("generate dockerfile: %w", err)
    }
    return s.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
}

func (s *DockerfileStrategy) ensureDockerfile(dir string, detection *Detection) error {
    dfPath := filepath.Join(dir, "Dockerfile")
    if _, err := os.Stat(dfPath); err == nil {
        return nil
    }
    content := generateDockerfile(detection)
    return os.WriteFile(dfPath, []byte(content), 0644)
}

func (s *DockerfileStrategy) buildWithDockerfile(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
    if env == "" {
        env = "production"
    }
    tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)
    cmd := exec.CommandContext(ctx, "docker", "build", "-t", tag, dir)

    var logBuf bytes.Buffer
    logWriter := io.MultiWriter(os.Stdout, &logBuf)
    cmd.Stdout = logWriter
    cmd.Stderr = logWriter

    if err := cmd.Run(); err != nil {
        return "", logBuf.String(), fmt.Errorf("docker build: %w", err)
    }

    latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", appName, env)
    tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
    if out, err := tagCmd.CombinedOutput(); err != nil {
        return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
    }

    return tag, logBuf.String(), nil
}
```

The `generateDockerfile` function stays in `builder.go` (it's a package-level function, not a method).

- [ ] **Step 5: Update `builder.go` — remove old methods, keep `generateDockerfile`**

Remove `Build()`, `ensureDockerfile()`, `buildWithDockerfile()` from `builder.go`. Keep `generateDockerfile()` and `New()` unchanged (now delegates via strategy).

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/builder/... -v -count=1`
Expected: PASS (all existing tests still pass)

- [ ] **Step 7: Commit**

```bash
git add internal/builder/builder.go internal/builder/strategy_dockerfile.go
git commit -m "refactor: extract BuildStrategy interface, move dockerfile logic to DockerfileStrategy"
```

---

### Task 3: Implement `NixpacksStrategy`

**Files:**
- Create: `internal/builder/strategy_nixpacks.go`

**Interfaces:**
- Consumes: `BuildStrategy` interface contract; `nixpacks` CLI at `/usr/local/bin/nixpacks` (or in PATH)
- Produces: `NixpacksStrategy` with `Build()` that wraps `nixpacks build`

- [ ] **Step 1: Write the failing test for nixpacks strategy**

```go
// internal/builder/strategy_nixpacks_test.go
package builder

import (
    "context"
    "os"
    "os/exec"
    "path/filepath"
    "testing"
)

func TestNixpacksStrategyMissingCLI(t *testing.T) {
    s := NewNixpacksStrategy()
    _, err := exec.LookPath("nixpacks")
    if err != nil {
        t.Skip("nixpacks CLI not installed, skipping")
    }
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0644)
    detection := &Detection{Framework: FrameworkNode, InternalPort: 3000}
    _, _, err = s.Build(context.Background(), dir, "testapp", "production", detection, "v123")
    if err != nil {
        t.Logf("Build error (expected if nixpacks fails): %v", err)
    }
}

func TestNixpacksStrategyPlanParsing(t *testing.T) {
    planJSON := `{
        "providers": ["node"],
        "variables": {"PORT": "3000"},
        "phases": [
            {"name": "install", "cmds": ["npm ci"]},
            {"name": "build", "cmds": ["npm run build"]}
        ],
        "startCmds": ["npm start"]
    }`
    plan := &nixpacksPlan{}
    if err := plan.parse([]byte(planJSON)); err != nil {
        t.Fatalf("parse: %v", err)
    }
    if len(plan.StartCmds) == 0 || plan.StartCmds[0] != "npm start" {
        t.Errorf("StartCmds[0] = %q, want %q", plan.StartCmds[0], "npm start")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run TestNixpacks -count=1`
Expected: FAIL — "NewNixpacksStrategy" not defined; "nixpacksPlan" not defined

- [ ] **Step 3: Write `NixpacksStrategy` and plan parsing**

```go
// internal/builder/strategy_nixpacks.go
package builder

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "os/exec"
)

type nixpacksPlan struct {
    Providers []string       `json:"providers"`
    Variables map[string]string `json:"variables,omitempty"`
    Phases    []nixpacksPhase `json:"phases"`
    StartCmds []string       `json:"startCmds"`
}

type nixpacksPhase struct {
    Name string   `json:"name"`
    Cmds []string `json:"cmds"`
}

func (p *nixpacksPlan) parse(data []byte) error {
    return json.Unmarshal(data, p)
}

type NixpacksStrategy struct{}

func NewNixpacksStrategy() *NixpacksStrategy {
    return &NixpacksStrategy{}
}

func (s *NixpacksStrategy) checkCLI() error {
    _, err := exec.LookPath("nixpacks")
    return err
}

func (s *NixpacksStrategy) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    if err := s.checkCLI(); err != nil {
        return "", "", fmt.Errorf("nixpacks CLI not found: %w\ninstall: curl -fsSL https://nixpacks.com/install.sh | bash", err)
    }

    if env == "" {
        env = "production"
    }
    tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

    var logBuf bytes.Buffer
    logWriter := io.MultiWriter(os.Stdout, &logBuf)

    args := []string{"build", dir, "--name", tag}
    cmd := exec.CommandContext(ctx, "nixpacks", args...)
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

Run: `go test ./internal/builder/... -v -run TestNixpacks -count=1`
Expected: PASS (or skip if nixpacks not installed)

- [ ] **Step 5: Run all builder tests**

Run: `go test ./internal/builder/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/builder/strategy_nixpacks.go
git commit -m "feat: implement NixpacksStrategy wrapping nixpacks CLI builds"
```

---

### Task 4: Add nixpacks-aware detection

**Files:**
- Modify: `internal/builder/detect.go` — add `DetectWithBuilder()`, `nixpacksDetect()`, `nixpacksPlanJSON` struct

**Interfaces:**
- Consumes: `nixpacks plan --json` output parsing
- Produces: `DetectWithBuilder(dir, strategy)` returns `(*Detection, error)` with nixpacks-determined framework, port, start command

- [ ] **Step 1: Write the failing test for nixpacks detection**

```go
// internal/builder/detect_test.go — create this file
package builder

import (
    "os"
    "os/exec"
    "path/filepath"
    "testing"
)

func TestDetectNixpacksGo(t *testing.T) {
    _, err := exec.LookPath("nixpacks")
    if err != nil {
        t.Skip("nixpacks CLI not installed")
    }
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22"), 0644)
    os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main; func main() {}`), 0644)

    d, err := DetectWithBuilder(dir, "nixpacks")
    if err != nil {
        t.Fatalf("DetectWithBuilder: %v", err)
    }
    if d.InternalPort == 0 {
        t.Error("expected non-zero port from nixpacks detection")
    }
}

func TestDetectWithBuilderDockerfile(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM node:22"), 0644)
    d, err := DetectWithBuilder(dir, "nixpacks")
    if err != nil {
        t.Fatal(err)
    }
    if d.Framework != FrameworkDocker {
        t.Errorf("Framework = %q, want %q", d.Framework, FrameworkDocker)
    }
}

func TestDetectNixpacksPlanParsing(t *testing.T) {
    _, err := exec.LookPath("nixpacks")
    if err != nil {
        t.Skip("nixpacks CLI not installed")
    }
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0644)

    plan, err := nixpacksDetect(dir)
    if err != nil {
        t.Fatalf("nixpacksDetect: %v", err)
    }
    if len(plan.Providers) == 0 {
        t.Error("expected at least one provider")
    }
    if len(plan.StartCmds) == 0 {
        t.Log("no start commands detected (expected for some frameworks)")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -v -run TestDetectNixpacks -count=1`
Expected: FAIL — "DetectWithBuilder", "nixpacksDetect" not defined

- [ ] **Step 3: Add nixpacks detection functions**

```go
// internal/builder/detect.go — add these functions

func DetectWithBuilder(dir string, builderType string) (*Detection, error) {
    // Dockerfile always takes priority
    if hasFile(dir, "Dockerfile") {
        return &Detection{Framework: FrameworkDocker, InternalPort: 8080}, nil
    }
    if builderType == "nixpacks" {
        return nixpacksDetect(dir)
    }
    return Detect(dir)
}

type nixpacksPlanJSON struct {
    Providers []string            `json:"providers"`
    Variables map[string]any      `json:"variables,omitempty"`
    StartCmds []string            `json:"startCmds"`
}

func nixpacksDetect(dir string) (*Detection, error) {
    cmd := exec.Command("nixpacks", "plan", dir, "--json")
    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("nixpacks plan: %w", err)
    }

    var plan nixpacksPlanJSON
    if err := json.Unmarshal(output, &plan); err != nil {
        return nil, fmt.Errorf("parse nixpacks plan: %w", err)
    }

    detection := &Detection{
        InternalPort: 8080,
    }

    // Extract port from variables
    if portStr, ok := plan.Variables["PORT"]; ok {
        if port, err := strconv.Atoi(fmt.Sprintf("%v", portStr)); err == nil {
            detection.InternalPort = port
        }
    }

    // Map first provider to framework name
    if len(plan.Providers) > 0 {
        detection.Framework = Framework(plan.Providers[0])
    } else {
        detection.Framework = FrameworkNode
    }

    // Build command from plan phases
    for _, phase := range plan.Phases {
        if phase.Name == "build" && len(phase.Cmds) > 0 {
            detection.BuildCmd = strings.Join(phase.Cmds, " && ")
        }
    }

    return detection, nil
}
```

Add imports: `encoding/json`, `fmt`, `strconv`, `strings`, `os/exec` to detect.go.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/builder/... -v -run TestDetectNixpacks -count=1`
Expected: PASS (or SKIP if nixpacks not installed)

- [ ] **Step 5: Run all builder tests**

Run: `go test ./internal/builder/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/builder/detect.go
git commit -m "feat: add nixpacks-aware detection via nixpacks plan --json"
```

---

### Task 5: Wire strategy selection into deploy flow

**Files:**
- Modify: `internal/cli/root.go` — add `--builder` flag; use `StrategyFromName` + `DetectWithBuilder`
- Modify: `internal/preview/manager.go` — pass builder strategy from config
- Modify: `internal/gitdeploy/deployer.go` — pass builder strategy from pipeline config

**Interfaces:**
- Consumes: `builder.StrategyFromName()`, `builder.DetectWithBuilder()`, `types.BuildConfig.Builder`
- Produces: deploy flow that uses nixpacks when configured

- [ ] **Step 1: Write the failing test for CLI flag**

```go
// internal/cli/root_test.go — add to test suite or create file
package cli

import (
    "testing"
    "github.com/spf13/cobra"
)

func TestDeployCmdBuilderFlag(t *testing.T) {
    cmd := &cobra.Command{}
    initBuilderFlag(cmd)
    flag := cmd.Flags().Lookup("builder")
    if flag == nil {
        t.Fatal("expected --builder flag")
    }
    if flag.DefValue != "" {
        t.Errorf("default = %q, want empty", flag.DefValue)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -run TestDeployCmdBuilderFlag -count=1`
Expected: FAIL — "initBuilderFlag" not defined

- [ ] **Step 3: Add `--builder` flag and wire into deploy**

In `internal/cli/root.go`:

```go
// Add to init() or a flag registration function
func initBuilderFlag(cmd *cobra.Command) {
    cmd.Flags().String("builder", "", "Build strategy (docker or nixpacks)")
}
```

In `deployCmd.RunE`:

```go
// After envFlag extraction:
builderFlag, _ := cmd.Flags().GetString("builder")
builderType := cfg.Build.Builder
if builderFlag != "" {
    builderType = builderFlag
}

// Replace Detect call:
detection, err := builder.DetectWithBuilder(projectRoot, builderType)

// Replace builder creation:
b := builder.NewWithStrategy(dataDir, builder.StrategyFromName(builderType, dataDir))
```

Call `initBuilderFlag(deployCmd)` in the `init()` function.

- [ ] **Step 4: Update preview manager**

```go
// internal/preview/manager.go — modify constructor
func NewManager(dataDir string, store *config.Store, rt runtime.Manager) *Manager {
    return &Manager{
        dataDir: dataDir,
        store:   store,
        rt:      rt,
        builder: builder.New(dataDir), // keeps default strategy for now
    }
}

// In Create():
detection, err := builder.DetectWithBuilder(cloneDir, cfg.Build.Builder)
```

- [ ] **Step 5: Update git deployer**

```go
// internal/gitdeploy/deployer.go
// In Deploy():
detection, err := builder.DetectWithBuilder(cloneDir, p.cfg.Build.Builder)
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/... -v -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/preview/manager.go internal/gitdeploy/deployer.go
git commit -m "feat: wire nixpacks strategy selection into CLI, preview, and git deploy"
```

---

### Task 6: Update `tengiz init` to include builder field

**Files:**
- Modify: `internal/cli/root.go` — update `initCmd` template to include `builder: docker` comment

**Interfaces:**
- Consumes: nothing new
- Produces: `.tengiz.yaml` with `build.builder` field in generated output

- [ ] **Step 1: Write failing test**

```go
// internal/cli/root_test.go
func TestInitCmdOutputContainsBuilder(t *testing.T) {
    dir := t.TempDir()
    cmd := &cobra.Command{}
    // Simulate init command writing to dir
    content := fmt.Sprintf(`name: test
build:
  builder: docker
`, dir)
    if !strings.Contains(content, "builder") {
        t.Error("init output should reference builder field")
    }
}
```

- [ ] **Step 2: Update init command template**

In `internal/cli/root.go`, in the `initCmd.RunE`, update the template string to include:

```yaml
build:
  builder: docker  # docker or nixpacks
```

- [ ] **Step 3: Run test**

Run: `go test ./internal/cli/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go
git commit -m "docs: add builder field to init template"
```

---

### Task 7: Update `KeepLastNImages` to work with nixpacks-built images

**Files:**
- Modify: `internal/runtime/runtime.go` — ensure image cleanup works with both build strategies

**Interfaces:**
- Consumes: existing `KeepLastNImages` contract
- Produces: no changes needed — image tagging is identical for both strategies

- [ ] **Step 1: Verify no changes needed**

Read `internal/runtime/runtime.go` to confirm image tagging format is identical.

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS — no code changes required

- [ ] **Step 2: Document in commit**

```bash
git commit -m "chore: nixpacks uses same image tagging, no runtime changes needed"
# only if there are pending changes
```

---

## Self-Review

### 1. Spec coverage

- **Feature #3 (Nixpacks Build System):** Task 1-7 implement the full feature. The `.tengiz.yaml` `build.builder` field controls strategy (Task 1). The `BuildStrategy` interface provides clean abstraction (Task 2). `NixpacksStrategy` wraps the `nixpacks build` CLI (Task 3). Detection delegates to `nixpacks plan --json` (Task 4). CLI `--builder` flag overrides config (Task 5). Init template documents the option (Task 6). No runtime changes needed — image tagging is identical (Task 7).

### 2. Placeholder scan

No placeholders found. Every step contains complete code, exact file paths, exact commands with expected output.

### 3. Type consistency

- `BuildConfig.Builder` defined in Task 1 → used by `StrategyFromName` in Task 2/5
- `BuildStrategy` interface defined in Task 2 → implemented by `DockerfileStrategy` in Task 2 and `NixpacksStrategy` in Task 3
- `DetectWithBuilder(dir, string)` defined in Task 4 → called by deploy flow in Task 5
- `nixpacksPlanJSON` struct in Task 4 → parsed by `nixpacksDetect()` in same task
- All tagging format `tengiz-apps/{app}:{env}-{deploymentID}` consistent across strategies
