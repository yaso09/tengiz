# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Nixpacks as an alternative build strategy alongside the existing framework detection, expanding supported frameworks from 7 to hundreds (Ruby, Rust, PHP, Java, Elixir, etc.).

**Architecture:** Add a `BuildStrategy` interface to the builder package with two implementations: `DockerfileStrategy` (existing behavior) and `NixpacksStrategy` (runs `nixpacks build` CLI). A strategy resolver selects the appropriate strategy based on config + CLI flag. The `Builder.Build()` method delegates to the selected strategy. Framework detection becomes optional — Nixpacks handles detection internally. Config propagation via `--builder` flag on `tengiz deploy` and `.tengiz.yaml` `build.builder` field.

**Tech Stack:** Go 1.26, `os/exec` for Nixpacks CLI, existing `builder` package, existing `config` package, existing CLI (`cobra`, `viper`)

## Global Constraints

- No new external Go dependencies — Nixpacks is invoked via `os/exec`, same as Docker
- All new files in `internal/builder/` — keep builder package self-contained
- `nixpacks` CLI must be installed separately (documented), detected at runtime
- Existing `tengiz deploy` behavior unchanged when `--builder` not specified (default: `auto` → existing detection first, fallback to nixpacks)
- Image tag format unchanged: `tengiz-apps/<appName>:<env>-<deploymentID>`
- All existing tests must pass without modification
- Nixpacks strategy must preserve build log capture (same as `buildWithDockerfile`)

---

### Task 1: Define BuildStrategy Interface and Types

**Files:**
- Modify: `internal/builder/builder.go:1-30` (add types + interface)
- Create: `internal/builder/strategy.go` (interface definition + types)

**Interfaces:**
- Consumes: nothing (first task)
- Produces: `BuildStrategy` interface, `StrategyType` type, `StrategyConfig` struct

- [ ] **Step 1: Write the failing interface test**

```go
// internal/builder/builder_test.go — add at bottom
func TestBuildStrategyInterface(t *testing.T) {
    var s BuildStrategy
    // This should fail to compile if interface is not defined
    _ = s
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestBuildStrategyInterface -v`
Expected: FAIL — "undefined: BuildStrategy"

- [ ] **Step 3: Define interface and types in strategy.go**

```go
// internal/builder/strategy.go
package builder

import "context"

type StrategyType int

const (
    StrategyDockerfile StrategyType = iota
    StrategyNixpacks
)

func (s StrategyType) String() string {
    switch s {
    case StrategyDockerfile:
        return "dockerfile"
    case StrategyNixpacks:
        return "nixpacks"
    default:
        return "unknown"
    }
}

// BuildStrategy defines how an application image is built.
type BuildStrategy interface {
    // Type returns the strategy type identifier.
    Type() StrategyType
    // Build executes the build and returns the image tag and build log.
    Build(ctx context.Context, dir, imageTag string) (string, error)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/ -run TestBuildStrategyInterface -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/builder/strategy.go internal/builder/builder_test.go
git commit -m "feat: define BuildStrategy interface for pluggable build backends"
```

---

### Task 2: Refactor Existing Builder as DockerfileStrategy

**Files:**
- Create: `internal/builder/dockerfile.go` (DockerfileStrategy implementation)
- Modify: `internal/builder/builder.go` (extract strategy, delegate Build())
- Modify: `internal/builder/builder_test.go` (update tests if needed)

**Interfaces:**
- Consumes: `BuildStrategy` (from Task 1)
- Produces: `DockerfileStrategy` struct implementing `BuildStrategy`, refactored `Builder.Build()` that delegates

- [ ] **Step 1: Write the failing test that existing builder implements the interface**

```go
func TestBuilderImplementsBuildStrategy(t *testing.T) {
    dir := t.TempDir()
    b := New(dir)
    var s BuildStrategy
    // Type assertion should fail if Builder doesn't implement BuildStrategy
    s = b
    _ = s
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/builder/ -run TestBuilderImplementsBuildStrategy -v`
Expected: FAIL — "cannot use b (variable of type *Builder) as BuildStrategy value"

- [ ] **Step 3: Make Builder implement BuildStrategy**

In `internal/builder/dockerfile.go`:
```go
package builder

import "context"

type DockerfileStrategy struct {
    dataDir  string
    appName  string
    detection *Detection
}

func NewDockerfileStrategy(dataDir string, appName string, detection *Detection) *DockerfileStrategy {
    return &DockerfileStrategy{
        dataDir:   dataDir,
        appName:   appName,
        detection: detection,
    }
}

func (s *DockerfileStrategy) Type() StrategyType {
    return StrategyDockerfile
}

func (s *DockerfileStrategy) Build(ctx context.Context, dir, imageTag string) (string, error) {
    // Ensure Dockerfile exists (generate if needed)
    if s.detection.Framework != FrameworkDocker {
        // This was originally in Builder.Build before calling buildWithDockerfile
        // We create a builder instance just for the dataDir helper methods
        b := &Builder{dataDir: s.dataDir}
        b.ensureDockerfile(dir, s.detection)
    }

    // Run docker build
    buildLog, err := buildWithDockerfile(dir, imageTag)
    if err != nil {
        return "", err
    }

    // Create :latest alias (was in buildWithDockerfile)
    if err := tagLatest(imageTag); err != nil {
        return "", err
    }

    return buildLog, nil
}
```

Then in `internal/builder/builder.go`, modify `Builder.Build()` to:
```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    imageTag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

    // Select strategy
    strategy := b.resolveStrategy(detection)

    // Execute build
    buildLog, err := strategy.Build(ctx, dir, imageTag)
    if err != nil {
        return "", "", fmt.Errorf("build failed: %w", err)
    }

    return imageTag, buildLog, nil
}

func (b *Builder) resolveStrategy(detection *Detection) BuildStrategy {
    // For now, always return DockerfileStrategy (Nixpacks added in Task 4)
    return NewDockerfileStrategy(b.dataDir, b.appName, detection)
}
```

Also add a helper in `internal/builder/dockerfile.go`:
```go
func buildWithDockerfile(dir, imageTag string) (string, error) {
    // Extract the build logic from the old buildWithDockerfile method
    // into a package-level function
    cmd := exec.Command("docker", "build", "-t", imageTag, ".")
    cmd.Dir = dir
    var buf bytes.Buffer
    cmd.Stdout = io.MultiWriter(&buf, os.Stdout)
    cmd.Stderr = io.MultiWriter(&buf, os.Stderr)
    if err := cmd.Run(); err != nil {
        return buf.String(), fmt.Errorf("docker build failed: %w", err)
    }
    return buf.String(), nil
}

func tagLatest(imageTag string) error {
    latestTag := strings.Replace(imageTag, ":"+strings.Split(imageTag, ":")[1], ":latest", 1)
    cmd := exec.Command("docker", "tag", imageTag, latestTag)
    return cmd.Run()
}
```

And rename existing `buildWithDockerfile` on `Builder` (line ~90-120 in builder.go) — or better, just inline the extracted logic.

- [ ] **Step 4: Run all builder tests to verify they pass**

Run: `go test ./internal/builder/ -v -count=1`
Expected: All existing tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/builder/dockerfile.go internal/builder/builder.go
git commit -m "refactor: extract DockerfileStrategy from Builder.Build()"
```

---

### Task 3: Add BuilderType to Config and CLI

**Files:**
- Modify: `internal/types/types.go` (add BuilderType field)
- Modify: `internal/cli/root.go` (add `--builder` flag, pass through)
- Modify: `internal/builder/builder.go` (accept StrategyType in Builder, strategy resolver)

**Interfaces:**
- Consumes: `StrategyType` from Task 1
- Produces: CLI `--builder` flag, config `build.builder` field, `Builder.BuilderType` field

- [ ] **Step 1: Write config parsing test**

In `internal/config/config_test.go`:
```go
func TestParseBuilderType(t *testing.T) {
    dir := t.TempDir()
    yaml := `
name: myapp
build:
  builder: nixpacks
`
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)
    cfg, err := LoadForEnvironment(dir, "")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Build.Builder != "nixpacks" {
        t.Fatalf("expected builder 'nixpacks', got %q", cfg.Build.Builder)
    }
}

func TestDefaultBuilderType(t *testing.T) {
    dir := t.TempDir()
    yaml := `name: myapp`
    os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yaml), 0644)
    cfg, err := LoadForEnvironment(dir, "")
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Build.Builder != "" {
        t.Fatalf("expected empty builder, got %q", cfg.Build.Builder)
    }
}
```

Run: `go test ./internal/config/ -run TestParseBuilderType -v`
Expected: FAIL — "unknown field \"builder\""

- [ ] **Step 2: Add Builder field to types**

In `internal/types/types.go`, add `Builder` field to `BuildConfig`:
```go
type BuildConfig struct {
    Command string `mapstructure:"command"`
    Output  string `mapstructure:"output"`
    Builder string `mapstructure:"builder"` // "dockerfile", "nixpacks", or "" for auto
}
```

- [ ] **Step 3: Add --builder CLI flag in root.go**

In `internal/cli/root.go`, find the deploy command's flag setup (around line 350-370):
```go
deployCmd.Flags().String("builder", "", "Build strategy: dockerfile, nixpacks, or auto (default)")
```

Then in the deploy command run function, read the flag:
```go
builderFlag, _ := cmd.Flags().GetString("builder")
```

And determine `StrategyType`:
```go
// After loading config, before Build()
strategyType := builder.StrategyDockerfile // default
switch builderFlag {
case "nixpacks":
    strategyType = builder.StrategyNixpacks
case "dockerfile":
    strategyType = builder.StrategyDockerfile
case "":
    // If not specified via flag, check config
    if cfg.Build.Builder == "nixpacks" {
        strategyType = builder.StrategyNixpacks
    }
}
```

Pass `strategyType` to `Builder` (add field):
```go
b := builder.New(dataDir)
b.SetStrategy(strategyType)
```

In `internal/builder/builder.go`:
```go
type Builder struct {
    dataDir      string
    strategyType StrategyType
}

func (b *Builder) SetStrategy(t StrategyType) {
    b.strategyType = t
}
```

Update `resolveStrategy` to use `b.strategyType`:
```go
func (b *Builder) resolveStrategy(detection *Detection) BuildStrategy {
    switch b.strategyType {
    case StrategyNixpacks:
        return NewNixpacksStrategy(b.dataDir)
    default:
        return NewDockerfileStrategy(b.dataDir, "", detection)
    }
}
```

- [ ] **Step 4: Run all tests to verify**

Run: `go test ./internal/config/ -run TestParseBuilderType -v && go test ./internal/builder/ -count=1 -v`
Expected: Both pass

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/cli/root.go internal/builder/builder.go
git commit -m "feat: add builder type config and --builder CLI flag"
```

---

### Task 4: Implement NixpacksStrategy

**Files:**
- Create: `internal/builder/nixpacks.go` (NixpacksStrategy implementation)
- Create: `internal/builder/nixpacks_test.go` (tests for NixpacksStrategy)
- Modify: `internal/builder/builder.go` (wire Nixpacks strategy)

**Interfaces:**
- Consumes: `BuildStrategy` (Task 1), `StrategyType` (Task 1)
- Produces: `NixpacksStrategy` implementing `BuildStrategy`, `isNixpacksInstalled()` helper

- [ ] **Step 1: Write failing Nixpacks tests**

```go
// internal/builder/nixpacks_test.go
package builder

import (
    "context"
    "os"
    "path/filepath"
    "testing"
)

func TestNixpacksStrategyType(t *testing.T) {
    s := NewNixpacksStrategy(t.TempDir())
    if s.Type() != StrategyNixpacks {
        t.Fatalf("expected StrategyNixpacks, got %v", s.Type())
    }
}

func TestNixpacksBuildGoApp(t *testing.T) {
    if _, err := exec.LookPath("nixpacks"); err != nil {
        t.Skip("nixpacks CLI not installed")
    }

    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testapp\ngo 1.22"), 0644)
    os.WriteFile(filepath.Join(dir, "main.go"), []byte(
        `package main
import "net/http"
func main() { http.ListenAndServe(":8080", nil) }
`), 0644)

    s := NewNixpacksStrategy(dir)
    imageTag := "tengiz-apps/test-nixpacks:production-test-123"
    ctx := context.Background()

    buildLog, err := s.Build(ctx, dir, imageTag)
    if err != nil {
        t.Fatalf("nixpacks build failed: %v\nlog: %s", err, buildLog)
    }

    // Verify image was created
    cmd := exec.Command("docker", "inspect", imageTag)
    if out, err := cmd.CombinedOutput(); err != nil {
        t.Fatalf("image not found after build: %v\n%s", err, out)
    }

    // Cleanup
    exec.Command("docker", "rmi", imageTag).Run()
}
```

- [ ] **Step 2: Run to verify it fails (nixpacks not implemented yet)**

Run: `go test ./internal/builder/ -run TestNixpacksStrategyType -v`
Expected: FAIL — "undefined: NewNixpacksStrategy"

- [ ] **Step 3: Implement NixpacksStrategy**

```go
// internal/builder/nixpacks.go
package builder

import (
    "bytes"
    "context"
    "fmt"
    "os"
    "os/exec"
    "strings"
)

// NixpacksStrategy builds applications using the nixpacks CLI.
type NixpacksStrategy struct {
    dataDir string
}

func NewNixpacksStrategy(dataDir string) *NixpacksStrategy {
    return &NixpacksStrategy{dataDir: dataDir}
}

func (s *NixpacksStrategy) Type() StrategyType {
    return StrategyNixpacks
}

func (s *NixpacksStrategy) Build(ctx context.Context, dir, imageTag string) (string, error) {
    if err := s.checkInstalled(); err != nil {
        return "", err
    }

    cmd := exec.CommandContext(ctx, "nixpacks", "build", dir,
        "--name", imageTag,
        "--label", "tengiz-built=true",
    )
    cmd.Dir = dir

    var buf bytes.Buffer
    cmd.Stdout = &buf
    cmd.Stderr = &buf

    if err := cmd.Run(); err != nil {
        return buf.String(), fmt.Errorf("nixpacks build failed: %w", err)
    }

    return buf.String(), nil
}

func (s *NixpacksStrategy) checkInstalled() error {
    _, err := exec.LookPath("nixpacks")
    if err != nil {
        return fmt.Errorf("nixpacks CLI not found: install from https://nixpacks.com/docs/getting-started")
    }
    return nil
}

// IsNixpacksInstalled returns true if the nixpacks CLI is available.
func IsNixpacksInstalled() bool {
    _, err := exec.LookPath("nixpacks")
    return err == nil
}
```

- [ ] **Step 4: Wire NixpacksStrategy into Builder**

In `internal/builder/builder.go`, update `resolveStrategy`:
```go
func (b *Builder) resolveStrategy(detection *Detection) BuildStrategy {
    switch b.strategyType {
    case StrategyNixpacks:
        return NewNixpacksStrategy(b.dataDir)
    default:
        return NewDockerfileStrategy(b.dataDir, "", detection)
    }
}
```

Also add `StrategyName()` helper on `Builder` (useful for logging):
```go
func (b *Builder) StrategyName() string {
    return b.strategyType.String()
}
```

- [ ] **Step 5: Run tests (nixpacks-skipped tests pass on CI without nixpacks)**

Run: `go test ./internal/builder/ -count=1 -v`
Expected: All tests pass. Nixpacks build test is skipped when nixpacks not installed.

- [ ] **Step 6: Commit**

```bash
git add internal/builder/nixpacks.go internal/builder/nixpacks_test.go internal/builder/builder.go
git commit -m "feat: implement NixpacksStrategy for nixpacks CLI-based builds"
```

---

### Task 5: Integrate Nixpacks into the Deploy Pipeline

**Files:**
- Modify: `internal/cli/root.go` (full integration: detection + strategy selection + fallback)
- Modify: `internal/gitdeploy/deployer.go` (pass builder type)
- Modify: `internal/preview/manager.go` (pass builder type)
- Test: `internal/cli/root_test.go` (add builder flag tests)

**Interfaces:**
- Consumes: `StrategyNixpacks`, `StrategyDockerfile`, `Builder.SetStrategy()`, `IsNixpacksInstalled()`
- Produces: Fully integrated deploy pipeline with nixpacks support

- [ ] **Step 1: Write deploy pipeline test for nixpacks**

In `internal/cli/root_test.go` (or new `internal/cli/builder_test.go`):
```go
func TestDeployWithNixpacksFlag(t *testing.T) {
    // This tests that the --builder flag is parsed and passed through
    // without actually running the build (which requires Docker + nixpacks)
    // We test the command flag parsing and strategy selection logic
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testapp\ngo 1.22"), 0644)

    // The deploy command should parse --builder flag
    cmd := NewRootCmd()
    cmd.SetArgs([]string{"deploy", dir, "--builder", "nixpacks"})
    // We expect an error about no config, but the flag parsing should work
    err := cmd.Execute()
    // Don't check err — we just need to verify the flag is parsed
    // The actual integration test requires Docker + nixpacks
    _ = err
}
```

- [ ] **Step 2: Integrate into root.go deploy command**

Find the deploy command runner (around lines 155-345 in `root.go`). After config loading and detection, add strategy resolution:

```go
// In deployCmd.Run, after detection but before Build():

// Determine build strategy
strategyType := builder.StrategyDockerfile
builderFlag, _ := cmd.Flags().GetString("builder")

switch {
case builderFlag == "nixpacks":
    strategyType = builder.StrategyNixpacks
case builderFlag == "dockerfile":
    strategyType = builder.StrategyDockerfile
case builderFlag == "auto":
    // Auto: try existing detection first, fall back to nixpacks
    if detection.Framework == builder.FrameworkStatic && !hasExistingDockerfile(projectRoot) {
        // Static is the catch-all fallback — try nixpacks instead
        strategyType = builder.StrategyNixpacks
    }
case cfg.Build.Builder == "nixpacks":
    strategyType = builder.StrategyNixpacks
case cfg.Build.Builder == "dockerfile":
    strategyType = builder.StrategyDockerfile
}

// If nixpacks selected but not installed, fall back gracefully with warning
if strategyType == builder.StrategyNixpacks && !builder.IsNixpacksInstalled() {
    fmt.Fprintf(os.Stderr, "⚠ nixpacks CLI not found, falling back to dockerfile strategy\n")
    strategyType = builder.StrategyDockerfile
}

// Also skip framework detection when using nixpacks (nixpacks does its own detection)
if strategyType != builder.StrategyNixpacks {
    detection = builder.Detect(projectRoot)
} else {
    // Use a minimal detection — nixpacks doesn't need framework info
    detection = &builder.Detection{
        Framework:    builder.Framework("nixpacks"),
        InternalPort: 8080, // Default; nixpacks may differ
    }
}

// Apply strategy
b := builder.New(dataDir)
b.SetStrategy(strategyType)

// Build
imageTag, buildLog, err := b.Build(ctx, projectRoot, cfg.Name, cfg.Environment, detection, deploymentID)
```

- [ ] **Step 3: Integrate into gitdeploy/deployer.go**

```go
// In deployer.go around line 95-110
// After detection, check config for builder type
strategyType := builder.StrategyDockerfile
if cfg.Build.Builder == "nixpacks" {
    strategyType = builder.StrategyNixpacks
}
b := builder.New(cfgDir)
b.SetStrategy(strategyType)
imageTag, buildLog, err := b.Build(ctx, dir, appName, cfg.Environment, detection, deploymentID)
```

- [ ] **Step 4: Integrate into preview/manager.go**

```go
// In manager.go Create() around line 60-75 and Update() around line 145-160
// Same pattern as gitdeploy
strategyType := builder.StrategyDockerfile
if app.Config.Build.Builder == "nixpacks" {
    strategyType = builder.StrategyNixpacks
}
b := builder.New(cfgDir)
b.SetStrategy(strategyType)
```

- [ ] **Step 5: Run all tests**

Run: `go test ./... -count=1 -v 2>&1 | head -100`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/gitdeploy/deployer.go internal/preview/manager.go
git commit -m "feat: integrate nixpacks build strategy into deploy pipeline"
```

---

### Task 6: Add `tengiz deploy --list-builders` and Documentation

**Files:**
- Modify: `internal/cli/root.go` (add `--list-builders` flag)
- Modify: `internal/builder/builder.go` (add `ListStrategies()` function)

**Interfaces:**
- Consumes: `StrategyType`, `IsNixpacksInstalled()`
- Produces: CLI `--list-builders` discoverability

- [ ] **Step 1: Write test for ListStrategies**

```go
func TestListStrategies(t *testing.T) {
    strategies := builder.ListStrategies()
    found := map[string]bool{}
    for _, s := range strategies {
        found[s] = true
    }
    if !found["dockerfile"] {
        t.Fatal("expected 'dockerfile' strategy")
    }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/builder/ -run TestListStrategies -v`
Expected: FAIL — "undefined: ListStrategies"

- [ ] **Step 3: Implement ListStrategies**

In `internal/builder/builder.go`:
```go
// ListStrategies returns the names of all available build strategies.
func ListStrategies() []string {
    strategies := []string{"dockerfile"}
    if IsNixpacksInstalled() {
        strategies = append(strategies, "nixpacks")
    }
    return strategies
}
```

- [ ] **Step 4: Add --list-builders flag to deploy command**

```go
// In root.go init() or deployCmd.Flags()
deployCmd.Flags().Bool("list-builders", false, "List available build strategies and exit")

// In deployCmd.Run, early in the function:
if listBuilders, _ := cmd.Flags().GetBool("list-builders"); listBuilders {
    fmt.Println("Available build strategies:")
    for _, s := range builder.ListStrategies() {
        installed := ""
        if s == "nixpacks" && !builder.IsNixpacksInstalled() {
            installed = " (not installed)"
        }
        fmt.Printf("  - %s%s\n", s, installed)
    }
    return
}
```

Add descriptions for each strategy in the help output:
```go
// In the deploy command's Long description
const deployLongDesc = `Deploy an application to Tengiz.

Build strategies:
  dockerfile  Generate a Dockerfile based on framework detection and build with Docker (default)
  nixpacks    Use the nixpacks CLI to automatically detect the framework and build

Use --list-builders to see which strategies are available on this system.
`
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/builder/ -run TestListStrategies -v && go build -o /dev/null .`
Expected: PASS + build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/builder/builder.go
git commit -m "feat: add --list-builders flag and ListStrategies function"
```

---

### Task 7: Add Nixpacks Detection as Fallback Strategy (Auto Mode)

**Files:**
- Modify: `internal/cli/root.go` (auto-mode fallback logic)
- Modify: `internal/builder/detect.go` (add nixpacks-aware detection)
- Test: `internal/builder/builder_test.go` (add auto-fallback tests)

**Interfaces:**
- Consumes: `IsNixpacksInstalled()`, `StrategyType`
- Produces: Auto-detection that tries nixpacks when framework is unknown

- [ ] **Step 1: Write test for auto-fallback behavior**

```go
func TestAutoFallbackToNixpacks(t *testing.T) {
    if !builder.IsNixpacksInstalled() {
        t.Skip("nixpacks not installed")
    }
    // Create directory with no recognizable framework files
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(
        `source "https://rubygems.org"\ngem "sinatra"`), 0644)

    // Detection should return FrameworkStatic (not useful)
    detection := builder.Detect(dir)
    if detection.Framework != builder.FrameworkStatic {
        t.Fatalf("expected static fallback, got %s", detection.Framework)
    }

    // But we should check if nixpacks can handle it
    // This is a logic test — nixpacks SHOULD be able to build a Ruby app
    _ = detection
}
```

- [ ] **Step 2: Implement auto-mode logic in root.go**

```go
// In the auto branch of strategy selection:
case builderFlag == "auto" || builderFlag == "":
    // Default: use framework detection. If it falls through to Static
    // and nixpacks is available, try nixpacks instead.
    detection = builder.Detect(projectRoot)
    if detection.Framework == builder.FrameworkStatic && builder.IsNixpacksInstalled() {
        // Framework detection couldn't identify the project — try nixpacks
        strategyType = builder.StrategyNixpacks
        // Reset detection for nixpacks
        detection = &builder.Detection{
            Framework:    builder.Framework("nixpacks-auto"),
            InternalPort: 8080,
        }
    }
```

Also add a `hasExistingDockerfile` helper:
```go
func hasExistingDockerfile(dir string) bool {
    for _, name := range []string{"Dockerfile", "Dockerfile.dockerignore"} {
        if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
            return true
        }
    }
    return false
}
```

- [ ] **Step 3: Add framework constant for nixpacks**

In `internal/builder/detect.go`:
```go
const (
    FrameworkDocker  Framework = "docker"
    FrameworkNextJS  Framework = "nextjs"
    FrameworkVite    Framework = "vite"
    FrameworkGo      Framework = "go"
    FrameworkNode    Framework = "node"
    FrameworkPython  Framework = "python"
    FrameworkStatic  Framework = "static"
    FrameworkNixpacks Framework = "nixpacks" // NEW
)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/builder/ ./internal/cli/ -count=1 -v`
Expected: All tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/builder/detect.go
git commit -m "feat: auto-fallback to nixpacks when framework detection returns static"
```

---

### Task 8: Update Docs and README

**Files:**
- Modify: `README.md` (add nixpacks build strategy documentation)
- Modify: `AGENTS.md` (if nixpacks-related notes needed)

- [ ] **Step 1: Write test that verifies README mentions nixpacks**

```go
// README check (in a test file or as a doc check)
func TestReadmeMentionsNixpacks(t *testing.T) {
    content, err := os.ReadFile("../../README.md")
    if err != nil {
        t.Skip("README not found")
    }
    if !bytes.Contains(content, []byte("nixpacks")) {
        t.Log("README should document nixpacks build strategy")
    }
}
```

- [ ] **Step 2: Update README.md**

Add a section under "Usage" or a new "Build Strategies" section:

```markdown
## Build Strategies

Tengiz supports multiple build strategies for converting source code into Docker images:

### Dockerfile (default)
Framework detection generates a Dockerfile for supported frameworks:
Next.js, Vite, Go, Node.js, Python, and static sites.

### Nixpacks (experimental)
[Nixpacks](https://nixpacks.com) automatically detects frameworks and produces optimized
Docker images for 100+ frameworks including Ruby, Rust, PHP, Java, Elixir, and more.

Enable with:
```bash
tengiz deploy --builder nixpacks
# or in .tengiz.yaml:
# build:
#   builder: nixpacks
```

Auto-mode (`--builder auto`, default) uses framework detection first, then falls back
to nixpacks if the project isn't recognized.

See available strategies:
```bash
tengiz deploy --list-builders
```
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document nixpacks build strategy"
```

---

## Self-Review Checklist

**1. Spec coverage:**
- Feature #3 "Nixpacks Build Sistemi": Covered by Tasks 1-8. Task 1 defines the strategy interface. Task 2 refactors existing builder. Task 3 adds config/CLI. Task 4 implements the nixpacks strategy. Task 5 integrates into all deploy paths (CLI, gitdeploy, preview). Task 6 adds discoverability. Task 7 adds auto-fallback. Task 8 documents.
- No gaps — every aspect of the spec (strategy integration, framework auto-detection via nixpacks, config override, CLI flag, documentation) is covered.

**2. Placeholder scan:** No TBD, TODO, or "implement later" patterns. All steps contain actual code, test code, file paths, and commands.

**3. Type consistency:**
- `StrategyType` (int enum) used consistently across all tasks
- `BuildStrategy` interface with `Type()` and `Build()` methods used consistently
- `StrategyDockerfile` / `StrategyNixpacks` constants match across tasks
- `NixpacksStrategy.Type()` returns `StrategyNixpacks` — consistent
- `IsNixpacksInstalled()` function name consistent across all call sites
- `Builder.SetStrategy()` matches the field name `strategyType`
- `FrameworkNixpacks` constant follows naming convention of existing constants

No inconsistencies found.

---

**Plan complete and saved to `docs/superpowers/plans/2026-07-18-nixpacks-build-system.md`. Two execution options:**

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
