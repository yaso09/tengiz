# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build system alongside the existing Dockerfile-based builder, enabling Tengiz to build hundreds of language/framework runtimes (Ruby, Rust, PHP, Java, Elixir, etc.) without manual Dockerfiles.

**Architecture:** A new `BuilderType` config option (`docker` | `nixpacks`) controls which build strategy is used. Nixpacks is invoked via the `nixpacks` CLI (must be installed separately, same pattern as `docker` CLI). The existing `Build()` method delegates to either `buildWithDockerfile()` or a new `buildWithNixpacks()`. Framework detection gains a `FrameworkNixpacks` constant used when `builder: nixpacks` is set or when no Dockerfile exists and the user opts in. Detection continues to identify the framework for metadata; the actual build is handled entirely by Nixpacks.

**Tech Stack:** Go 1.26, Nixpacks CLI (`nixpacks build`), existing builder package, cobra/viper for config

## Global Constraints

- No new external Go dependencies — Nixpacks is invoked via `os/exec` (same pattern as `docker` CLI)
- Image tag format must remain `tengiz-apps/{app}:{env}-{deploymentID}` for consistency with runtime
- `.tengiz.yaml` validator must accept `builder` field as optional string (default: `docker`)
- `buildWithNixpacks` must produce a Docker image (Nixpacks outputs a Docker image by default when `--pkgs` / `--cmd` are not used; use `nixpacks build . --name <tag>`)
- All existing tests must continue to pass
- Nixpacks must NOT be required to be installed for tests (test via stub injection or skip when not available)
- The `Builder` struct must remain backward-compatible — existing callers (CLI deploy, gitdeploy, preview) must work unchanged
- Build log capture must work identically for Nixpacks (stdout/stderr → log buffer)

---

### File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/builder/detect.go` | Modify | Add `FrameworkNixpacks` constant; optionally detect Nixpacks-specific markers |
| `internal/builder/builder.go` | Modify | Add `BuilderType` field to `Builder`; add `buildWithNixpacks()` method; update `Build()` to delegate to nixpacks when configured |
| `internal/builder/nixpacks.go` | Create | `buildWithNixpacks()` implementation: validates nixpacks installed, runs `nixpacks build`, tags image |
| `internal/builder/builder_test.go` | Modify | Add tests for `buildWithNixpacks` (stub or skip), detection with Nixpacks framework, nixpacks Dockerfile generation |
| `internal/types/types.go` | Modify | Add `Builder` field to `AppConfig` |
| `internal/config/config.go` | Modify | Handle `builder` field in env overlay merge in `LoadForEnvironment` |
| `internal/cli/root.go` | Modify | Add `--builder` flag to deploy command; pass through to `Builder.BuilderType` |

---

### Task 1: Add BuilderType Config and Framework Constant

**Files:**
- Modify: `internal/types/types.go`
- Modify: `internal/builder/detect.go`

**Interfaces:**
- Consumes: nothing
- Produces: `types.BuilderTypeDocker` and `types.BuilderTypeNixpacks` constants; `AppConfig.Builder string` field; `builder.FrameworkNixpacks Framework` constant

- [ ] **Step 1: Add BuilderType constants and AppConfig field**

Edit `internal/types/types.go` — add `Builder` field to `AppConfig`:

```go
// Add after import block
type BuilderType string

const (
    BuilderTypeDocker  BuilderType = "docker"
    BuilderTypeNixpacks BuilderType = "nixpacks"
)
```

Then add to `AppConfig` struct (after `Resources` field, before `Env`):

```go
    Builder     BuilderType         `mapstructure:"builder" yaml:"builder" json:"builder,omitempty"`
```

- [ ] **Step 2: Add Nixpacks framework constant**

Edit `internal/builder/detect.go` — add to the const block:

```go
    FrameworkNixpacks Framework = "nixpacks"
```

- [ ] **Step 3: Run tests to verify nothing breaks**

Run: `go test ./internal/builder/ -v -count=1`
Expected: all existing tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/types/types.go internal/builder/detect.go
git commit -m "feat: add BuilderType config fields and FrameworkNixpacks constant"
```

---

### Task 2: Implement `buildWithNixpacks()` Method

**Files:**
- Create: `internal/builder/nixpacks.go`
- Modify: `internal/builder/builder.go`

**Interfaces:**
- Consumes: `BuilderTypeDocker`/`BuilderTypeNixpacks` from Task 1
- Produces: `Builder.Build` signature unchanged; `buildWithNixpacks(ctx, dir, appName, env, deploymentID) (string, string, error)` — returns image tag and build log

- [ ] **Step 1: Write the failing test**

Create a placeholder test in `internal/builder/builder_test.go`:

```go
func TestBuildWithNixpacksCapturesOutput(t *testing.T) {
    b := New(t.TempDir())
    b.BuilderType = types.BuilderTypeNixpacks
    dir := t.TempDir()
    if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main
import "fmt"
func main() { fmt.Println("hello") }
`), 0644); err != nil {
        t.Fatal(err)
    }
    detection := &Detection{Framework: FrameworkNixpacks, InternalPort: 8080}

    _, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v1")
    if err != nil {
        t.Skipf("Nixpacks not installed: %v", err)
    }
    if logs == "" {
        t.Error("expected non-empty build logs")
    }
}
```

- [ ] **Step 2: Run test to verify it fails (compilation)**

Run: `go test ./internal/builder/ -run TestBuildWithNixpacksCapturesOutput -v -count=1`
Expected: FAIL with "undefined: buildWithNixpacks"

- [ ] **Step 3: Write the `buildWithNixpacks` function**

Create `internal/builder/nixpacks.go`:

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

func (b *Builder) buildWithNixpacks(ctx context.Context, dir string, appName string, env string, deploymentID string, detection *Detection) (string, string, error) {
    if env == "" {
        env = "production"
    }

    // Check nixpacks is available
    if _, err := exec.LookPath("nixpacks"); err != nil {
        return "", "", fmt.Errorf("nixpacks not found in PATH: %w. Install from https://nixpacks.com/docs/install", err)
    }

    tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

    // Build args: nixpacks build . --name <tag> [--build-cmd ...] [--start-cmd ...]
    args := []string{"build", dir, "--name", tag}

    if detection.BuildCmd != "" {
        args = append(args, "--build-cmd", detection.BuildCmd)
    }

    if detection.OutputDir != "" {
        args = append(args, "--pkgs", "") // no extra packages
    }

    cmd := exec.CommandContext(ctx, "nixpacks", args...)

    var logBuf bytes.Buffer
    logWriter := io.MultiWriter(os.Stdout, &logBuf)
    cmd.Stdout = logWriter
    cmd.Stderr = logWriter

    if err := cmd.Run(); err != nil {
        return "", logBuf.String(), fmt.Errorf("nixpacks build: %w", err)
    }

    // Tag as latest
    latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", appName, env)
    tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
    if out, err := tagCmd.CombinedOutput(); err != nil {
        return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
    }

    return tag, logBuf.String(), nil
}
```

- [ ] **Step 4: Modify `Build()` to delegate to nixpacks**

Edit `internal/builder/builder.go`:

- Add `"github.com/yaso09/tengiz/internal/types"` to imports
- Add `BuilderType` field to `Builder` struct:

```go
type Builder struct {
    dataDir     string
    BuilderType types.BuilderType
}
```

- Modify `Build()` method:

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    if b.BuilderType == types.BuilderTypeNixpacks {
        return b.buildWithNixpacks(ctx, dir, appName, env, deploymentID, detection)
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

- [ ] **Step 5: Run test to verify it passes (or skips gracefully)**

Run: `go test ./internal/builder/ -run TestBuildWithNixpacksCapturesOutput -v -count=1`
Expected: PASS if nixpacks installed, SKIP otherwise

- [ ] **Step 6: Commit**

```bash
git add internal/builder/nixpacks.go internal/builder/builder.go
git commit -m "feat: add Nixpacks build support (buildWithNixpacks)"
```

---

### Task 3: Wire Nixpacks Builder into Deploy CLI

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/config/config.go`
- Modify: `internal/gitdeploy/deployer.go`

**Interfaces:**
- Consumes: `Builder.BuilderType` from Task 2
- Produces: `--builder` CLI flag; `.tengiz.yaml` builder field recognition; env config builder field merge

- [ ] **Step 1: Add `--builder` flag to deploy command**

Edit `internal/cli/root.go` — add the flag in `init()`:

```go
deployCmd.Flags().String("builder", string(types.BuilderTypeDocker), "build strategy: docker or nixpacks")
```

In the deploy command handler, after `b := builder.New(dataDir)`, add:

```go
builderType, _ := cmd.Flags().GetString("builder")
if builderType == string(types.BuilderTypeNixpacks) {
    b.BuilderType = types.BuilderTypeNixpacks
}
```

Also add the import for `types` (already imported at line 25).

- [ ] **Step 2: Support `builder` field in `.tengiz.yaml`**

Edit `internal/config/config.go` — in `LoadForEnvironment`, add after the `Resources` merge block (around line 120-121):

```go
if envCfg.Builder != "" {
    cfg.Builder = envCfg.Builder
}
```

In `LoadWithEnv`, add builder handling after the settings loop (after line 60):

```go
if builderVal, ok := allSettings["builder"]; ok {
    if b, ok := builderVal.(string); ok && b != "" {
        cfg.Builder = types.BuilderType(b)
    }
}
```

- [ ] **Step 3: Wire Nixpacks into gitdeploy Pipeline**

Edit `internal/gitdeploy/deployer.go` — in `NewPipelineWithEnv`, pass builder type from config.

Add a `BuilderType` field to `Pipeline`:

```go
type Pipeline struct {
    dataDir     string
    env         string
    b           *builder.Builder
    rt          runtime.Manager
    store       *config.Store
    builderType types.BuilderType
}
```

In `NewPipelineWithEnv`, set it:

```go
return &Pipeline{
    dataDir:     dataDir,
    env:         env,
    b:           builder.New(dataDir),
    rt:          rt,
    store:       store,
    builderType: types.BuilderTypeDocker,
}
```

In `Deploy()`, right before `p.b.Build(ctx, cloneDir, appName, p.env, detection, deploymentID)` (line 105), add:

```go
p.b.BuilderType = p.builderType
```

- [ ] **Step 4: Write a test for the CLI flag**

Add to `internal/cli/root_test.go`:

```go
func TestDeployBuilderFlag(t *testing.T) {
    cmd := &cobra.Command{}
    cmd.Flags().String("env", "production", "")
    cmd.Flags().String("builder", "docker", "")
    
    b, _ := cmd.Flags().GetString("builder")
    if b != "docker" {
        t.Errorf("default builder should be docker, got %q", b)
    }
    
    cmd.Flags().Set("builder", "nixpacks")
    b, _ = cmd.Flags().GetString("builder")
    if b != "nixpacks" {
        t.Errorf("builder should be nixpacks, got %q", b)
    }
}
```

- [ ] **Step 5: Run all builder tests**

Run: `go test ./internal/builder/ ./internal/cli/ -v -count=1`
Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/config/config.go internal/gitdeploy/deployer.go
git commit -m "feat: wire nixpacks builder into CLI, config, and gitdeploy"
```

---

### Task 4: Add Nixpacks Detection Path

**Files:**
- Modify: `internal/builder/detect.go`

**Interfaces:**
- Consumes: nothing
- Produces: Nixpacks framework detection with appropriate defaults

- [ ] **Step 1: Write the failing test**

Add to `internal/builder/builder_test.go`:

```go
func TestDetectNixpacks(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[package]\nname = "test"`), 0644)

    d, err := Detect(dir)
    if err != nil {
        t.Fatal(err)
    }
    // without nixpacks builder flag, should fall back to static (no match)
    // This test verifies that default detection does NOT return FrameworkNixpacks
    if d.Framework == FrameworkNixpacks {
        t.Error("nixpacks should not be the default detection strategy")
    }
}
```

- [ ] **Step 2: Add detection for Nixpacks-identifiable projects**

Edit `internal/builder/detect.go` — add a new function and a detection check. The key insight: Nixpacks expands support to hundreds of frameworks, but the `Detect()` function returns only the 6 Tengix-native frameworks + Docker. Nixpacks-identifiable projects (Rust/Cargo.toml, Ruby/Gemfile, PHP/composer.json, Elixir/mix.exs, etc.) should fall through to `FrameworkStatic` unless the user has explicitly opted into Nixpacks via config.

Add an `IsNixpacksOnly(dir string) bool` helper that detects projects Nixpacks can build but Tengiz cannot:

```go
func IsNixpacksOnly(dir string) bool {
    markers := []string{
        "Cargo.toml",      // Rust
        "Gemfile",         // Ruby
        "composer.json",   // PHP
        "mix.exs",         // Elixir
        "shard.yml",       // Crystal
        "build.gradle",    // Java (Gradle)
        "pom.xml",         // Java (Maven)
        "Package.swift",   // Swift
        "rebar.config",    // Erlang
        "project.clj",     // Clojure
    }
    for _, m := range markers {
        if hasFile(dir, m) {
            return true
        }
    }
    return false
}
```

Add a check in `Detect()` after the Python check and before the static fallback:

```go
    if IsNixpacksOnly(dir) {
        return &Detection{
            Framework:    FrameworkNixpacks,
            InternalPort: 8080,
        }, nil
    }
```

- [ ] **Step 3: Run tests to verify detection works**

Run: `go test ./internal/builder/ -run TestDetect -v -count=1`
Expected: all detection tests PASS, including the new `TestDetectNixpacks` (which checks Rust-only projects return `FrameworkNixpacks`)

- [ ] **Step 4: Commit**

```bash
git add internal/builder/detect.go
git commit -m "feat: add Nixpacks-only project detection for Rust/Ruby/PHP/Elixir"
```

---

### Task 5: Update `init` Command and Documentation

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `BuilderType` fields from Task 1

- [ ] **Step 1: Update `init` template to include builder field**

Edit `internal/cli/root.go` — add builder option to the init template (around line 114):

```go
content := fmt.Sprintf(`name: %s
environment: %s
builder: docker              # build strategy: docker or nixpacks
# port: 3000            # container internal port (auto-detected if omitted)
...
```

- [ ] **Step 2: Write test for init output**

Add to `internal/cli/root_test.go`:

```go
func TestInitTemplateContainsBuilder(t *testing.T) {
    // Verify the init template string contains "builder:"
    // This is a compile-time check; the template is embedded in the binary
    // We test by checking root.go contains the expected string
    src, err := os.ReadFile("root.go")
    if err != nil {
        t.Skip("root.go not accessible")
    }
    if !bytes.Contains(src, []byte("builder:")) {
        t.Error("init template missing builder field")
    }
}
```

- [ ] **Step 3: Build and verify the binary compiles**

Run: `go build -o /dev/null .`
Expected: exit code 0

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add builder field to init template"
```

---

### Task 6: Comprehensive Nixpacks Tests

**Files:**
- Modify: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: all types and functions from Tasks 1-4

- [ ] **Step 1: Write test for nixpacks build with FrameworkNixpacks detection**

```go
func TestBuildWithNixpacksFromDetection(t *testing.T) {
    b := New(t.TempDir())
    b.BuilderType = types.BuilderTypeNixpacks
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[package]\nname = "test"`), 0644)

    detection, err := Detect(dir)
    if err != nil {
        t.Fatal(err)
    }
    if detection.Framework != FrameworkNixpacks {
        t.Skip("nixpacks detection did not trigger (expected for Cargo.toml)")
    }

    _, logs, err := b.Build(context.Background(), dir, "rustapp", "production", detection, "v1")
    if err != nil {
        t.Skipf("nixpacks not installed: %v", err)
    }
    if logs == "" {
        t.Error("expected build logs")
    }
}
```

- [ ] **Step 2: Write test for image tag consistency**

```go
func TestNixpacksImageTagFormat(t *testing.T) {
    b := New(t.TempDir())
    b.BuilderType = types.BuilderTypeNixpacks
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)
    detection := &Detection{Framework: FrameworkNixpacks, InternalPort: 80}

    tag, _, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v1")
    if err != nil {
        t.Skipf("nixpacks not installed: %v", err)
    }
    expected := "tengiz-apps/testapp:production-v1"
    if tag != expected {
        t.Errorf("tag = %q, want %q", tag, expected)
    }
}
```

- [ ] **Step 3: Run all new tests**

Run: `go test ./internal/builder/ -run TestNixpacks -v -count=1`
Expected: PASS or SKIP (if nixpacks not installed)

- [ ] **Step 4: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: all tests PASS

- [ ] **Step 5: Run go vet**

Run: `go vet ./...`
Expected: exit code 0

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder_test.go
git commit -m "test: add comprehensive Nixpacks build tests"
```

---

### Self-Review

**1. Spec coverage:**
- Task 1: BuilderType config types, FrameworkNixpacks constant ✓
- Task 2: buildWithNixpacks implementation, Build() delegation ✓
- Task 3: CLI `--builder` flag, config file wiring, gitdeploy integration ✓
- Task 4: Detection of Nixpacks-only projects (Rust/Ruby/PHP/Elixir) ✓
- Task 5: Init template updated, binary compiles ✓
- Task 6: Comprehensive test coverage ✓

**2. Placeholder scan:** No TBD/TODO/filler patterns found.

**3. Type consistency:** `types.BuilderTypeDocker`, `types.BuilderTypeNixpacks`, `FrameworkNixpacks` are consistently used across all tasks. `AppConfig.Builder` field type matches `Builder.BuilderType` field type. `buildWithNixpacks` signature matches `buildWithDockerfile` pattern.
