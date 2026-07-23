# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Nixpacks as an alternative build system so Tengiz supports hundreds of frameworks (Ruby, Rust, PHP, Java, Elixir, etc.) instead of the current 6 hardcoded frameworks.

**Architecture:** Nixpacks (`railwayapp/nixpacks`) is invoked via the Docker CLI as a build strategy alongside the existing Dockerfile generation. A new `BuilderStrategy` interface abstracts `buildWithDockerfile` and `buildWithNixpacks`. The `.tengiz.yaml` config gains a `builder` field (`"docker"` | `"nixpacks"`). Detection is enhanced to not fail when Nixpacks is selected — instead of framework-specific Dockerfiles, Nixpacks auto-detects and generates its own Dockerfile.

**Tech Stack:** Go 1.26, `os/exec` to shell out to `nixpacks` CLI, existing `builder` package, Cobra CLI flags, Viper config.

## Global Constraints

- No new external Go dependencies (Nixpacks is a CLI binary, invoked via `os/exec`, just like Docker)
- Nixpacks binary must be installed separately on the host (documented in README; `tengiz doctor` will check for it in a future task)
- The existing `builder.Detection` struct must remain backward-compatible
- All existing tests must continue to pass
- New builder strategy follows the same `Build(ctx, dir, appName, env, detection, deploymentID)` signature
- Container naming conventions (`tengiz-*`, `tengiz-apps/*` tags) unchanged
- `.tengiz.yaml` `builder` field: valid values `"docker"` (default) or `"nixpacks"`

---

### Task 1: Add `Builder` field to Config Types

**Files:**
- Modify: `internal/types/types.go:42-46`
- Test: `internal/types/types_test.go` (append to existing)

**Interfaces:**
- Consumes: `BuildConfig` struct at `types.go:42`
- Produces: Updated `BuildConfig` with new `Builder` field; configs can now express `build.builder: "nixpacks"`

- [ ] **Step 1: Write the failing test**

```go
// internal/types/types_test.go
package types

import (
	"testing"
)

func TestBuildConfigBuilderFieldMapping(t *testing.T) {
	cfg := BuildConfig{
		Command: "npm run build",
		Output:  "dist",
		Builder: "nixpacks",
	}
	if cfg.Builder != "nixpacks" {
		t.Errorf("Builder = %q, want %q", cfg.Builder, "nixpacks")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/ -v -run TestBuildConfigBuilderFieldMapping`
Expected: FAIL — `types.BuildConfig` has no `Builder` field

- [ ] **Step 3: Add `Builder` field to `BuildConfig`**

```go
type BuildConfig struct {
	Command string `mapstructure:"command"`
	Output  string `mapstructure:"output"`
	Builder string `mapstructure:"builder"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/ -v -run TestBuildConfigBuilderFieldMapping`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat(build): add Builder field to BuildConfig for nixpacks support"
```

---

### Task 2: Add `BuilderStrategy` interface and Nixpacks build method

**Files:**
- Modify: `internal/builder/builder.go`
- Test: `internal/builder/builder_test.go` (append to existing)

**Interfaces:**
- Consumes: `Detection` struct, `BuildConfig.Builder` (`"docker"` | `"nixpacks"`), `Build` method signature
- Produces: `NixpacksDetected` boolean on `Detection`; `buildWithNixpacks` method; `Build()` dispatches to strategy based on config

- [ ] **Step 1: Add constants and helper for builder strategies**

```go
// in internal/builder/builder.go — add after package declaration
const (
	BuilderDocker   = "docker"
	BuilderNixpacks = "nixpacks"
)

// DefaultBuilder returns the default builder strategy.
func DefaultBuilder() string {
	return BuilderDocker
}
```

- [ ] **Step 2: Modify `Build()` to accept builder string and dispatch**

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string, builder string) (string, string, error) {
	if detection.Framework == FrameworkDocker {
		return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
	}
	if builder == BuilderNixpacks {
		return b.buildWithNixpacks(ctx, dir, appName, env, deploymentID)
	}
	if err := b.ensureDockerfile(dir, detection); err != nil {
		return "", "", fmt.Errorf("generate dockerfile: %w", err)
	}
	return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID)
}
```

- [ ] **Step 3: Implement `buildWithNixpacks`**

```go
func (b *Builder) buildWithNixpacks(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
	if env == "" {
		env = "production"
	}
	tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(os.Stdout, &logBuf)

	cmd := exec.CommandContext(ctx, "nixpacks", "build", dir, "--name", tag)

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

- [ ] **Step 4: Write test for Nixpacks build dispatch**

```go
func TestBuildDispatchNixpacks(t *testing.T) {
	b := New(t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644); err != nil {
		t.Fatal(err)
	}
	detection := &Detection{Framework: FrameworkStatic, InternalPort: 80}

	tag, logs, err := b.Build(context.Background(), dir, "testapp-nix", "production", detection, "v123", BuilderNixpacks)
	if err != nil {
		t.Skipf("Build() with nixpacks error (likely no nixpacks binary): %v", err)
	}
	if tag == "" {
		t.Error("expected non-empty tag")
	}
	_ = logs
}
```

- [ ] **Step 5: Update existing `Build` tests to pass the builder argument**

Update calls from `b.Build(ctx, dir, ...)` to `b.Build(ctx, dir, ..., DefaultBuilder())` in all existing tests.

```go
// In TestBuildCapturesOutput
tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v123", DefaultBuilder())

// In TestBuildWithDeploymentIDCompiles
tag, _, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v123", DefaultBuilder())
```

- [ ] **Step 6: Run all builder tests**

Run: `go test ./internal/builder/ -v -count=1`
Expected: All existing tests PASS (or skip if no Docker), new test PASS (or skip if no nixpacks)

- [ ] **Step 7: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat(build): add Nixpacks build strategy with BuilderDocker/BuilderNixpacks dispatch"
```

---

### Task 3: Add `--builder` CLI flag and wire into deploy command

**Files:**
- Modify: `internal/cli/root.go:155-345` (deploy command)

**Interfaces:**
- Consumes: `Build()` now takes `builder string` argument; `BuildConfig.Builder` from config
- Produces: CLI flag `--builder` (`"docker"` default); deploy passes the resolved builder to `Build()`

- [ ] **Step 1: Add `--builder` flag to deploy command**

```go
// after line 65 (deployCmd.Flags().String("env", ...))
deployCmd.Flags().String("builder", "docker", "build strategy: docker (default) or nixpacks")
```

- [ ] **Step 2: Resolve builder value in deploy command body**

```go
// inside deployCmd.RunE, after config loading (around line 197)
builderFlag, _ := cmd.Flags().GetString("builder")
if builderFlag == "" {
	builderFlag = builder.DefaultBuilder()
}
// Config's builder field overrides CLI flag if set
if cfg.Build.Builder != "" {
	builderFlag = cfg.Build.Builder
}
// Validate
if builderFlag != builder.BuilderDocker && builderFlag != builder.BuilderNixpacks {
	return fmt.Errorf("invalid builder %q: must be %q or %q", builderFlag, builder.BuilderDocker, builder.BuilderNixpacks)
}
```

- [ ] **Step 3: Pass builder to `b.Build()`**

Change line 201 from:
```go
imageTag, buildLog, err := b.Build(context.Background(), projectRoot, cfg.Name, cfg.Environment, detection, deploymentID)
```
to:
```go
imageTag, buildLog, err := b.Build(context.Background(), projectRoot, cfg.Name, cfg.Environment, detection, deploymentID, builderFlag)
```

- [ ] **Step 4: Write test for CLI flag wiring**

```go
// internal/cli/root_test.go
package cli

import (
	"testing"
)

func TestDeployCmdHasBuilderFlag(t *testing.T) {
	flag := deployCmd.Flags().Lookup("builder")
	if flag == nil {
		t.Fatal("deploy command missing --builder flag")
	}
	if flag.DefValue != "docker" {
		t.Errorf("default builder = %q, want %q", flag.DefValue, "docker")
	}
}
```

- [ ] **Step 5: Run CLI tests**

Run: `go test ./internal/cli/ -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add --builder flag (docker/nixpacks) to deploy command"
```

---

### Task 4: Update `AppConfig` merge in config package to handle `build.builder`

**Files:**
- Modify: `internal/config/config.go:101-135` (env config merge in `LoadForEnvironment`)

**Interfaces:**
- Consumes: `BuildConfig.Builder` field
- Produces: Env-specific configs can override the builder via `.tengiz.{env}.yaml` `build.builder`

- [ ] **Step 1: Write test for builder override in env config**

```go
// internal/config/config_test.go
func TestLoadForEnvironmentBuilderOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(`
name: testapp
build:
  builder: docker
`), 0644)
	os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(`
build:
  builder: nixpacks
`), 0644)

	cfg, err := LoadForEnvironment(dir, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Build.Builder != "nixpacks" {
		t.Errorf("Build.Builder = %q, want %q", cfg.Build.Builder, "nixpacks")
	}
}
```

- [ ] **Step 2: Add builder merge to `LoadForEnvironment`**

```go
// Add inside the merge block (around line 104-112)
if envCfg.Build.Builder != "" {
	cfg.Build.Builder = envCfg.Build.Builder
}
```

- [ ] **Step 3: Run config tests**

Run: `go test ./internal/config/ -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): merge build.builder from env config overrides"
```

---

### Task 5: Update `tengiz init` template to include builder option as comment

**Files:**
- Modify: `internal/cli/root.go:114-140` (init template)
- Modify: `docs/FUTURES_FEATURES.md` — mark Nixpacks Build System as ✅ Implemented

- [ ] **Step 1: Add builder comment to init template**

```go
// Change line ~134-139 to add builder option
content := fmt.Sprintf(`name: %s
environment: %s
# builder: nixpacks        # build strategy: docker (default) or nixpacks
# port: 3000            # container internal port (auto-detected if omitted)
`, name, env)
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cli/ -v -count=1 -run TestInitCmd`
Expected: PASS

- [ ] **Step 3: Update FUTURES_FEATURES.md**

Change `| 3 | **Nixpacks Build Sistemi** ⬜` to `| 3 | **Nixpacks Build Sistemi** ✅`

And add to the ✅ Implemented Features table at the bottom.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go docs/FUTURES_FEATURES.md
git commit -m "docs: update init template and features list for nixpacks support"
```

---

### Task 6: Documentation and verification

**Files:**
- Modify: `README.md` — add Nixpacks requirement and usage docs

- [ ] **Step 1: Add Nixpacks to README prerequisites section**

In the prerequisites/installation section, add:
```
- Nixpacks (optional, required for `--builder nixpacks`): `curl -fsSL https://nixpacks.com/install.sh | bash`
```

- [ ] **Step 2: Add usage examples to README**

In the deployment section, add:
```
# Deploy with Nixpacks (auto-detect any framework — Ruby, Rust, PHP, etc.)
tengiz deploy --builder nixpacks

# Or set in .tengiz.yaml:
# build:
#   builder: nixpacks
```

- [ ] **Step 3: Run final verification**

Run: `go vet ./...` — expected: clean
Run: `go test ./... -v -count=1` — expected: all pass

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: add nixpacks build system prerequisites and usage"
```
