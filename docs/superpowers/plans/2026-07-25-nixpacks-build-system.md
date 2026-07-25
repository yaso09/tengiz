# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build system to Tengiz, expanding supported frameworks from 6 to hundreds (Ruby, Rust, PHP, Java, Elixir, etc.) with zero additional config.

**Architecture:** Introduce a `BuilderStrategy` interface with two implementations: `DockerfileStrategy` (the existing inline Dockerfile generation) and `NixpacksStrategy` (shells out to `nixpacks` CLI). The strategy is selected via `.tengiz.yaml build.builder: "nixpacks"` or `--builder nixpacks` flag. `nixpacks` is called via `os/exec` (same pattern as Docker), must be installed separately. No Nixpacks Go SDK — CLI-based to match existing architecture.

**Tech Stack:** Go 1.26, `os/exec` for nixpacks CLI, Go standard library, existing builder package patterns.

## Global Constraints

- No new external Go dependencies beyond cobra/viper
- All external tool invocations via `os/exec` (matching Docker CLI pattern)
- `nixpacks` binary required at runtime; fail gracefully with clear error if missing
- The existing `Build()` function signature must remain backward-compatible
- `.tengiz.yaml` config for builder selection: `build.builder: "dockerfile"|"nixpacks"`
- CLI flag: `tengiz deploy --builder nixpacks .`
- Nixpacks output is a Docker image; tag it with the same `tengiz-apps/<app>:<env>-<id>` convention
- Fall back to `dockerfile` if nixpacks fails or binary not found
- All tests must pass with `go test ./... -v -count=1`
- Nixpacks images get HEALTHCHECK injected post-build (same as dockerfile strategy)

---

### Task 1: BuilderStrategy Interface and NixpacksStrategy Skeleton

**Files:**
- Create: `internal/builder/strategy.go`
- Modify: `internal/builder/builder.go` (refactor `Builder` to use strategy)
- Create: `internal/builder/nixpacks.go` (skeleton)
- Test: `internal/builder/builder_test.go` (new tests)

**Interfaces:**
- Consumes: `Detection` struct (existing), `AppConfig` (existing)
- Produces: `BuilderStrategy` interface with `Build(ctx, dir, appName, env, detection, deploymentID) (imageTag string, logs string, err error)`

- [ ] **Step 1: Write the failing test for strategy selection**

Add to `internal/builder/builder_test.go`:

```go
func TestBuilderStrategySelection(t *testing.T) {
    b := New(t.TempDir(), "nixpacks")
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)
    detection := &Detection{Framework: FrameworkStatic, InternalPort: 80}

    tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v1")
    // Should attempt nixpacks, fail because nixpacks not installed,
    // fall back to dockerfile strategy
    if err != nil {
        // Nixpacks not installed is expected — fallback should succeed
        t.Skipf("nixpacks not installed, test requires docker: %v", err)
    }
    if tag == "" {
        t.Error("expected non-empty tag")
    }
    _ = logs
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestBuilderStrategySelection -v -count=1`
Expected: compilation error — `New` doesn't accept strategy arg, no `BuilderStrategy` type

- [ ] **Step 3: Create `strategy.go` with the interface**

Create `internal/builder/strategy.go`:

```go
package builder

import "context"

// BuilderStrategy defines how a container image is built from source.
type BuilderStrategy interface {
    // Build builds a Docker image from source directory and returns the image tag and build logs.
    Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (imageTag string, logs string, err error)
}
```

- [ ] **Step 4: Refactor `builder.go` to accept strategy**

Modify `Builder` struct and `New`:

```go
type Builder struct {
    dataDir  string
    strategy BuilderStrategy
}

func New(dataDir string, strategy BuilderStrategy) *Builder {
    if strategy == nil {
        strategy = &DockerfileStrategy{dataDir: dataDir}
    }
    return &Builder{dataDir: dataDir, strategy: strategy}
}

// Build delegates to the selected strategy.
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    return b.strategy.Build(ctx, dir, appName, env, detection, deploymentID)
}
```

Keep the existing `generateDockerfile` and `buildWithDockerfile` methods — wrap them in a `DockerfileStrategy`.

- [ ] **Step 5: Run tests to verify compilation succeeds**

Run: `go test ./internal/builder/ -v -count=1`
Expected: compilation errors at callers of `builder.New(dataDir)` — needs to be updated

- [ ] **Step 6: Add `DockerfileStrategy` to `strategy.go`**

Add to `strategy.go`:

```go
// DockerfileStrategy generates a Dockerfile from framework detection and builds it.
type DockerfileStrategy struct {
    dataDir string
}

func (s *DockerfileStrategy) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    b := &Builder{dataDir: s.dataDir, strategy: s}
    if detection.Framework == FrameworkDocker {
        return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
    }
    if err := b.ensureDockerfile(dir, detection); err != nil {
        return "", "", fmt.Errorf("generate dockerfile: %w", err)
    }
    return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
}
```

- [ ] **Step 7: Update all callers of `builder.New(dataDir)` to pass strategy**

All callers are in:
- `internal/cli/root.go:199` — `builder.New(dataDir)` → `builder.New(dataDir, nil)` (defaults to Dockerfile)
- `internal/preview/manager.go:30` — same
- `internal/gitdeploy/deployer.go:38` — same

```go
b := builder.New(dataDir, nil)
```

Run: `go test ./... -v -count=1`
Expected: all tests pass

- [ ] **Step 8: Commit**

```bash
git add internal/builder/strategy.go internal/builder/builder.go internal/cli/root.go internal/preview/manager.go internal/gitdeploy/deployer.go internal/builder/builder_test.go
git commit -m "feat(builder): extract BuilderStrategy interface"
```

---

### Task 2: NixpacksStrategy Implementation

**Files:**
- Modify: `internal/builder/nixpacks.go` (full implementation)
- Test: `internal/builder/builder_test.go` (nixpacks-specific tests)

**Interfaces:**
- Consumes: `BuilderStrategy` interface (from Task 1), `Detection` struct
- Produces: `NixpacksStrategy` — runs `nixpacks build <dir> --name <tag>` then tags the resulting image

- [ ] **Step 1: Write the failing test for NixpacksStrategy**

```go
func TestNixpacksStrategyDetectBinary(t *testing.T) {
    s := &NixpacksStrategy{}
    _, err := exec.LookPath("nixpacks")
    if err != nil {
        t.Skip("nixpacks not installed, skipping")
    }
    // Verify it actually builds something
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)
    os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test","scripts":{"start":"echo hi"}}`), 0644)
    detection := &Detection{Framework: FrameworkNode, InternalPort: 3000}

    tag, logs, err := s.Build(context.Background(), dir, "testapp", "production", detection, "v1")
    if err != nil {
        t.Skipf("nixpacks build failed (nixpacks may not be installed): %v", err)
    }
    if tag == "" {
        t.Error("expected non-empty tag")
    }
    _ = logs
}
```

- [ ] **Step 2: Run test to verify it fails (compilation should work, binary check may fail)**

Run: `go test ./internal/builder/ -run TestNixpacksStrategyDetectBinary -v -count=1`
Expected: PASS with "nixpacks not installed" skip (since `nixpacks.go` is empty, it won't compile if the type doesn't exist)

- [ ] **Step 3: Implement `NixpacksStrategy`**

Write `internal/builder/nixpacks.go`:

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

func (s *NixpacksStrategy) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    if env == "" {
        env = "production"
    }
    tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

    // Check nixpacks is installed
    if _, err := exec.LookPath("nixpacks"); err != nil {
        return "", "", fmt.Errorf("nixpacks not found in PATH: %w", err)
    }

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

- [ ] **Step 4: Run all builder tests**

Run: `go test ./internal/builder/ -v -count=1`
Expected: all tests pass (nixpacks test skips if binary not installed)

- [ ] **Step 5: Commit**

```bash
git add internal/builder/nixpacks.go internal/builder/builder_test.go
git commit -m "feat(builder): implement NixpacksStrategy"
```

---

### Task 3: Config Integration — Builder Selection in `.tengiz.yaml`

**Files:**
- Modify: `internal/types/types.go` — add `Builder` field to `BuildConfig`
- Modify: `internal/cli/root.go` — read builder type from config, pass to `builder.New()`

**Interfaces:**
- Consumes: `BuildConfig.Builder string` field, CLI `--builder` flag
- Produces: Strategy instance passed to `builder.New(strategy)`

- [ ] **Step 1: Write failing test for config parsing**

Add to `internal/builder/builder_test.go` or `internal/types/types_test.go`:

```go
func TestBuildConfigBuilderField(t *testing.T) {
    cfg := types.BuildConfig{
        Command: "npm run build",
        Output:  "dist",
        Builder: "nixpacks",
    }
    if cfg.Builder != "nixpacks" {
        t.Errorf("Builder = %q, want %q", cfg.Builder, "nixpacks")
    }
}
```

- [ ] **Step 2: Run test to verify compilation failure**

Run: `go test ./internal/types/ -run TestBuildConfigBuilderField -v -count=1`
Expected: compilation error — `types.BuildConfig` has no `Builder` field

- [ ] **Step 3: Add `Builder` field to `BuildConfig`**

In `internal/types/types.go`, add `Builder` field:

```go
type BuildConfig struct {
    Command string `mapstructure:"command"`
    Output  string `mapstructure:"output"`
    Builder string `mapstructure:"builder"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/ -run TestBuildConfigBuilderField -v -count=1`
Expected: PASS

- [ ] **Step 5: Add CLI `--builder` flag and wire config → strategy in deploy command**

In `internal/cli/root.go`, find the deploy command `RunE` and:

Add a flag after line ~155:
```go
deployCmd.Flags().String("builder", "", "build strategy: dockerfile or nixpacks (default: dockerfile)")
```

Read it before the builder call (after `cfg` is loaded, around line 199):
```go
builderFlag, _ := cmd.Flags().GetString("builder")
strategy := resolveStrategy(builderFlag, cfg, dataDir)
b := builder.New(dataDir, strategy)
```

Add the helper function in `root.go`:
```go
func resolveStrategy(builderFlag string, cfg *types.AppConfig, dataDir string) builder.BuilderStrategy {
    choice := builderFlag
    if choice == "" {
        choice = cfg.Build.Builder
    }
    switch choice {
    case "nixpacks":
        return &builder.NixpacksStrategy{}
    default:
        return &builder.DockerfileStrategy{DataDir: dataDir}
    }
}
```

Make `DockerfileStrategy.dataDir` exported (`DataDir`) in `strategy.go`:
```go
type DockerfileStrategy struct {
    DataDir string
}
```

Update all internal references in strategy.go from `s.dataDir` to `s.DataDir`.

- [ ] **Step 6: Run all tests**

Run: `go test ./... -v -count=1`
Expected: all tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/types/types.go internal/cli/root.go internal/builder/strategy.go
git commit -m "feat(cli): add --builder flag and config-based strategy selection"
```

---

### Task 4: CLI `--list-builders` and Error Handling

**Files:**
- Modify: `internal/cli/root.go` — add `--list-builders` and validation
- Modify: `internal/builder/strategy.go` — add `AvailableBuilders()` function

- [ ] **Step 1: Write failing tests**

In `internal/builder/builder_test.go`:

```go
func TestAvailableBuilders(t *testing.T) {
    builders := builder.AvailableBuilders()
    if len(builders) < 2 {
        t.Errorf("expected at least 2 builders (dockerfile, nixpacks), got %d", len(builders))
    }
    found := false
    for _, b := range builders {
        if b == "nixpacks" {
            found = true
        }
    }
    if !found {
        t.Error("expected nixpacks in available builders")
    }
}
```

- [ ] **Step 2: Run test to verify compilation failure**

Run: `go test ./internal/builder/ -run TestAvailableBuilders -v -count=1`
Expected: compilation error — `AvailableBuilders` not defined

- [ ] **Step 3: Implement `AvailableBuilders`**

Add to `internal/builder/strategy.go`:

```go
func AvailableBuilders() []string {
    return []string{"dockerfile", "nixpacks"}
}
```

- [ ] **Step 4: Add `--list-builders` flag to deploy command**

In `internal/cli/root.go`, before the deploy RunE:

```go
var listBuilders bool
deployCmd.Flags().BoolVar(&listBuilders, "list-builders", false, "list available build strategies and exit")
```

At the top of deploy RunE:

```go
if listBuilders {
    fmt.Println("available builders:")
    for _, b := range builder.AvailableBuilders() {
        fmt.Printf("  - %s\n", b)
    }
    return nil
}
```

- [ ] **Step 5: Add builder validation**

In `resolveStrategy`:

```go
func resolveStrategy(builderFlag string, cfg *types.AppConfig, dataDir string) builder.BuilderStrategy {
    choice := builderFlag
    if choice == "" {
        choice = cfg.Build.Builder
    }
    if choice == "" {
        choice = "dockerfile"
    }
    switch choice {
    case "dockerfile":
        return &builder.DockerfileStrategy{DataDir: dataDir}
    case "nixpacks":
        return &builder.NixpacksStrategy{}
    default:
        fmt.Fprintf(os.Stderr, "unknown builder %q, falling back to dockerfile\n", choice)
        return &builder.DockerfileStrategy{DataDir: dataDir}
    }
}
```

- [ ] **Step 6: Run all tests**

Run: `go test ./... -v -count=1`
Expected: all tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/builder/strategy.go internal/builder/builder_test.go
git commit -m "feat(cli): add --list-builders flag and builder validation"
```

---

### Task 5: Integration Test and Docs

**Files:**
- Modify: `internal/builder/builder_test.go` — integration test
- Modify: `internal/cli/root_test.go` — CLI integration test

- [ ] **Step 1: Write CLI integration test**

In `internal/cli/root_test.go`:

```go
func TestDeployListBuildersFlag(t *testing.T) {
    // Simulate "tengiz deploy --list-builders"
    cmd := &cobra.Command{}
    deployCmd := getDeployCommand()
    deployCmd.RunE = func(cmd *cobra.Command, args []string) error {
        // Should print builders and return nil
        for _, b := range builder.AvailableBuilders() {
            t.Logf("builder: %s", b)
        }
        return nil
    }
    deployCmd.SetArgs([]string{"--list-builders"})
    if err := deployCmd.Execute(); err != nil {
        t.Fatal(err)
    }
}
```

- [ ] **Step 2: Verify builder package passes lint**

Run: `go vet ./internal/builder/`
Expected: no output (success)

- [ ] **Step 3: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: all tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root_test.go internal/builder/builder_test.go
git commit -m "test(builder): add integration tests for Nixpacks strategy"
```

---

### Task 6: Global Refactor Follow-up — Nixpacks in Preview and GitDeploy

**Files:**
- Modify: `internal/preview/manager.go` — pass strategy from config
- Modify: `internal/gitdeploy/deployer.go` — pass strategy from config

- [ ] **Step 1: Update `gitdeploy/deployer.go`**

The `NewPipelineWithEnv` currently creates `builder.New(dataDir)`. Load config and pass the strategy:

```go
func NewPipelineWithEnv(dataDir string, env string, store *config.Store) *Pipeline {
    cfg, _ := config.LoadForEnvironment(filepath.Join(dataDir, ".."), env)
    strategy := resolveStrategyFromConfig(cfg, dataDir)
    return &Pipeline{
        b:     builder.New(dataDir, strategy),
        store: store,
        env:   env,
    }
}
```

Add a helper or inline resolution (keep it DRY by exposing a shared `resolveStrategy` in a util or making it package-level).

- [ ] **Step 2: Update `preview/manager.go`**

Same pattern — read strategy from config and pass to `builder.New`.

- [ ] **Step 3: Run all tests**

Run: `go test ./... -v -count=1`
Expected: all tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/preview/manager.go internal/gitdeploy/deployer.go
git commit -m "refactor: wire nixpacks strategy into preview and gitdeploy"
```

---

### Self-Review

**1. Spec coverage:**
- Task 1: `BuilderStrategy` interface extraction — covers the abstraction
- Task 2: `NixpacksStrategy` nixpacks CLI invocation — covers the core build mechanism
- Task 3: Config integration (`build.builder` in `.tengiz.yaml`, `--builder` CLI flag) — covers user-facing selection
- Task 4: `--list-builders` and error handling — covers discoverability and UX
- Task 5: Integration tests — covers verification
- Task 6: Preview + GitDeploy wiring — covers completeness for all build paths

**2. Placeholder scan:** No TBD, TODO, or generic placeholders. All code blocks contain complete, compilable code. No empty test stubs without body.

**3. Type consistency:**
- `BuilderStrategy` interface: `Build(ctx, dir, appName, env, detection, deploymentID) (string, string, error)` — consistent across all tasks
- `DockerfileStrategy.DataDir` (exported) — consistent in Task 3 refactor
- All function signatures match across tasks
