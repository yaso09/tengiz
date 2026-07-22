# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build strategy so Tengiz supports hundreds of frameworks (Ruby, Rust, PHP, Java, Elixir, etc.) instead of the current 6.

**Architecture:** Add a `builder` field to `types.BuildConfig` (values: `""`/`"dockerfile"` for existing behavior, `"nixpacks"` for Nixpacks). When `builder: "nixpacks"` is set, skip Tengiz's framework detection and Dockerfile generation — instead run `nixpacks build <dir> --name <image>` which auto-detects the framework and produces a Docker image. The rest of the deploy pipeline (tagging, `docker run`, proxy registration) stays unchanged. Detection is extended to return `FrameworkNixpacks` when `builder` is set, which skips Dockerfile generation.

**Tech Stack:** Go 1.26, Cobra, Viper, Nixpacks CLI (external binary, checked at runtime), Docker CLI.

## Global Constraints

- No new Go dependencies beyond existing cobra/viper
- All Docker interaction stays via `os/exec` — no Docker SDK
- Nixpacks must be checked for availability at build time (not at CLI startup)
- Image naming convention stays: `tengiz-apps/{appName}:{env}-{deploymentID}` and `:latest`
- `build.builder` field is optional — absent/empty defaults to current behavior
- Nixpacks output images are tagged with Tengiz conventions after build

---

## File Structure

| File | Responsibility | Status |
|------|---------------|--------|
| `internal/types/types.go` | Add `Builder` field to `BuildConfig` | Modify |
| `internal/builder/detect.go` | Add `FrameworkNixpacks` constant; detect when builder is nixpacks | Modify |
| `internal/builder/builder.go` | Add nixpacks build path in `Build()` | Modify |
| `internal/builder/nixpacks.go` | New: `nixpacksBuild()` helper — runs `nixpacks build`, re-tags output image | Create |
| `internal/builder/builder_test.go` | Tests for nixpacks detection and build flow | Modify |
| `internal/cli/root.go` | Add `--builder` flag to deploy command; pass to builder and config | Modify |
| `internal/config/config.go` | Merge `build.builder` field in `LoadForEnvironment` | Modify |

---

### Task 1: Add `Builder` field to BuildConfig and wire into config merge

**Files:**
- Modify: `internal/types/types.go:42-45`
- Modify: `internal/config/config.go:104-109`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: existing `BuildConfig` struct
- Produces: `BuildConfig.Builder string` field; env config merge handles `build.builder`

- [ ] **Step 1: Write the failing test for BuildConfig.Builder field**

```go
// Add to config_test.go
func TestLoadForEnvironmentMergesBuilder(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".tengiz.yaml")
	os.WriteFile(base, []byte(`
name: myapp
build:
  command: npm run build
`), 0644)
	envFile := filepath.Join(dir, ".tengiz.staging.yaml")
	os.WriteFile(envFile, []byte(`
build:
  builder: nixpacks
`), 0644)

	cfg, err := LoadForEnvironment(dir, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Build.Builder != "nixpacks" {
		t.Errorf("expected builder nixpacks, got %q", cfg.Build.Builder)
	}
	if cfg.Build.Command != "npm run build" {
		t.Errorf("expected command to survive merge, got %q", cfg.Build.Command)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadForEnvironmentMergesBuilder -v`
Expected: FAIL — `BuildConfig` has no `Builder` field

- [ ] **Step 3: Add `Builder` field to BuildConfig**

In `internal/types/types.go:42-45`, change:

```go
type BuildConfig struct {
	Command string `mapstructure:"command"`
	Output  string `mapstructure:"output"`
	Builder string `mapstructure:"builder"`
}
```

- [ ] **Step 4: Add builder field merge to LoadForEnvironment**

In `internal/config/config.go`, after the `envCfg.Build.Output` merge (line 108), add:

```go
if envCfg.Build.Builder != "" {
	cfg.Build.Builder = envCfg.Build.Builder
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestLoadForEnvironmentMergesBuilder -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/types/types.go internal/config/config.go internal/config/config_test.go
git commit -m "feat: add build.builder field to BuildConfig and config merge"
```

---

### Task 2: Extend framework detection for Nixpacks

**Files:**
- Modify: `internal/builder/detect.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: existing `Detect(dir)` signature, `BuildConfig.Builder`
- Produces: `FrameworkNixpacks` constant; `DetectWithConfig(dir, builder)` that returns `FrameworkNixpacks` when builder is `"nixpacks"`

- [ ] **Step 1: Write tests for Nixpacks detection**

```go
// Add to builder_test.go
func TestDetectWithNixpacksBuilder(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\"\n"), 0644)

	d, err := DetectWithConfig(dir, "nixpacks")
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkNixpacks {
		t.Errorf("expected nixpacks framework, got %v", d.Framework)
	}
}

func TestDetectWithEmptyBuilder(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644)

	// empty builder = default detection
	d, err := DetectWithConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkGo {
		t.Errorf("expected go framework, got %v", d.Framework)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run "TestDetectWithNixpacksBuilder|TestDetectWithEmptyBuilder" -v`
Expected: FAIL — `DetectWithConfig` not defined, `FrameworkNixpacks` not defined

- [ ] **Step 3: Add FrameworkNixpacks constant and DetectWithConfig**

In `internal/builder/detect.go`, add to the const block:

```go
FrameworkNixpacks Framework = "nixpacks"
```

Add new function:

```go
func DetectWithConfig(dir string, builder string) (*Detection, error) {
	if builder == "nixpacks" {
		return &Detection{
			Framework:    FrameworkNixpacks,
			InternalPort: 8080,
		}, nil
	}
	return Detect(dir)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/ -run "TestDetectWithNixpacksBuilder|TestDetectWithEmptyBuilder" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/builder/detect.go internal/builder/builder_test.go
git commit -m "feat: add FrameworkNixpacks and DetectWithConfig"
```

---

### Task 3: Create nixpacks build helper

**Files:**
- Create: `internal/builder/nixpacks.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `context.Context`, `dir`, `appName`, `env`, `deploymentID` (same as `Build`)
- Produces: `nixpacksBuild(ctx, dir, appName, env, deploymentID) (fullImageTag string, logs string, error)`

- [ ] **Step 1: Write the failing test for nixpacksBuild**

```go
// Add to builder_test.go
func TestNixpacksBuild(t *testing.T) {
	if _, err := exec.LookPath("nixpacks"); err != nil {
		t.Skip("nixpacks CLI not available")
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname=\"test\"\nversion=\"0.1.0\"\nedition=\"2021\"\n"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "main.rs"), []byte("fn main() { println!(\"hello\"); }\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "src"), 0755)

	b := New(t.TempDir())
	tag, logs, err := b.nixpacksBuild(context.Background(), dir, "testapp", "production", "test123")
	if err != nil {
		t.Fatalf("nixpacksBuild failed: %v\nlogs: %s", err, logs)
	}
	expected := "tengiz-apps/testapp:production-test123"
	if tag != expected {
		t.Errorf("expected tag %q, got %q", expected, tag)
	}
	// cleanup
	exec.Command("docker", "rmi", tag).Run()
}

func TestNixpacksBuildFailsWhenNixpacksMissing(t *testing.T) {
	// Override PATH to simulate missing nixpacks
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", "/dev/null")
	defer os.Setenv("PATH", oldPath)

	dir := t.TempDir()
	b := New(t.TempDir())
	_, _, err := b.nixpacksBuild(context.Background(), dir, "testapp", "production", "test123")
	if err == nil {
		t.Fatal("expected error when nixpacks is missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run "TestNixpacksBuild" -v`
Expected: FAIL — `nixpacksBuild` method not defined

- [ ] **Step 3: Create `internal/builder/nixpacks.go`**

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

func (b *Builder) nixpacksBuild(ctx context.Context, dir, appName, env, deploymentID string) (string, string, error) {
	if _, err := exec.LookPath("nixpacks"); err != nil {
		return "", "", fmt.Errorf("nixpacks not found in PATH: %w", err)
	}
	if env == "" {
		env = "production"
	}

	imageName := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

	cmd := exec.CommandContext(ctx, "nixpacks", "build", dir, "--name", imageName)
	var logBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &logBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &logBuf)

	if err := cmd.Run(); err != nil {
		return "", logBuf.String(), fmt.Errorf("nixpacks build: %w", err)
	}

	latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", appName, env)
	tagCmd := exec.CommandContext(ctx, "docker", "tag", imageName, latestTag)
	if out, err := tagCmd.CombinedOutput(); err != nil {
		return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
	}

	return imageName, logBuf.String(), nil
}
```

- [ ] **Step 4: Run test**

Run: `go test ./internal/builder/ -run "TestNixpacksBuildFailsWhenNixpacksMissing" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/builder/nixpacks.go internal/builder/builder_test.go
git commit -m "feat: add nixpacksBuild helper"
```

---

### Task 4: Wire Nixpacks into the Builder.Build method

**Files:**
- Modify: `internal/builder/builder.go:21-29`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: existing `Builder.Build()` signature unchanged
- Produces: When detection is `FrameworkNixpacks`, `Build()` calls `nixpacksBuild()` instead of `ensureDockerfile()` / `buildWithDockerfile()`

- [ ] **Step 1: Write the test for Build dispatching to nixpacks**

```go
// Modify builder_test.go — add
func TestBuildDispatchesToNixpacks(t *testing.T) {
	if _, err := exec.LookPath("nixpacks"); err != nil {
		t.Skip("nixpacks CLI not available")
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname=\"test\"\nversion=\"0.1.0\"\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "main.rs"), []byte("fn main() { println!(\"hello\"); }\n"), 0644)

	b := New(t.TempDir())
	detection := &Detection{
		Framework:    FrameworkNixpacks,
		InternalPort: 8080,
	}
	tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "test456")
	if err != nil {
		t.Fatalf("Build failed: %v\nlogs: %s", err, logs)
	}
	expected := "tengiz-apps/testapp:production-test456"
	if tag != expected {
		t.Errorf("expected tag %q, got %q", expected, tag)
	}
	// cleanup
	exec.Command("docker", "rmi", tag).Run()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestBuildDispatchesToNixpacks -v`
Expected: FAIL — Build doesn't handle FrameworkNixpacks (falls through to default Dockerfile generation)

- [ ] **Step 3: Modify `Build()` in `builder.go`**

Replace lines 21-29:

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
	if detection.Framework == FrameworkNixpacks {
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/ -run TestBuildDispatchesToNixpacks -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: wire nixpacks build path into Builder.Build"
```

---

### Task 5: Add `--builder` CLI flag and wire into deploy command

**Files:**
- Modify: `internal/cli/root.go`
- Test: manual verification (integration)

**Interfaces:**
- Consumes: `--builder` flag on deploy command, `cfg.Build.Builder` from config
- Produces: detection uses `DetectWithConfig(dir, builder)`, builder dispatches to nixpacks

- [ ] **Step 1: Add `--builder` flag to deploy command**

In `internal/cli/root.go`, in the `init()` function or near the deployCmd registration, add:

```go
deployCmd.Flags().String("builder", "", "Build strategy: \"nixpacks\" or \"\" (default, auto-detect)")
```

- [ ] **Step 2: Modify deploy command to read builder flag and use DetectWithConfig**

In the deploy command's `RunE` (around line 187), replace:

```go
detection, err := builder.Detect(projectRoot)
```

with:

```go
	builderFlag, _ := cmd.Flags().GetString("builder")
	effectiveBuilder := builderFlag
	if effectiveBuilder == "" {
		effectiveBuilder = cfg.Build.Builder
	}
	detection, err := builder.DetectWithConfig(projectRoot, effectiveBuilder)
```

Also pass `cfg.Build.Builder` to `DetectWithConfig` when `--builder` flag is not set.

Also update the `initCmd` to recognize the flag reference — add a comment or doc entry.

Update the `cfg.Port` fallback to use the nixpacks default if needed.

Wait, actually — for nixpacks, the internal port is unknown until build. We need to handle this. The simplest approach: for Nixpacks, default to port 3000 (common for most web frameworks), and let users override via `.tengiz.yaml` `port:`.

In the detection, we already return `InternalPort: 8080` for nixpacks. That's fine as a default.

Let me refine the deploy command change. Around line 187-195:

```go
		builderFlag, _ := cmd.Flags().GetString("builder")
		effectiveBuilder := builderFlag
		if effectiveBuilder == "" {
			effectiveBuilder = cfg.Build.Builder
		}
		detection, err := builder.DetectWithConfig(projectRoot, effectiveBuilder)
		if err != nil {
			return fmt.Errorf("detect: %w", err)
		}
		fmt.Printf("[tengiz] detected: %s (port %d)\n", detection.Framework, detection.InternalPort)

		if cfg.Port == 0 {
			cfg.Port = detection.InternalPort
		}
```

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add --builder flag to deploy command for nixpacks support"
```

---

### Task 6: Add Nixpacks detection support for native sentinel files

**Files:**
- Modify: `internal/builder/detect.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `Detect(dir)` — existing function
- Produces: Even without `builder: nixpacks` config, if no known framework is detected but nixpacks-supported sentinels exist, suggest nixpacks

This is optional — the main path uses `builder: nixpacks` config or `--builder` flag. However, we should at least make sure nixpacks is discoverable. For now, when `builder` is set to `"nixpacks"`, we return `FrameworkNixpacks` unconditionally. The plan is sufficient.

- [ ] **Step 1: Write test for nixpacks detection with Rust project**

```go
// Add to builder_test.go — already covered in Task 2
```

- [ ] **Step 2: Already passing from Task 2**

- [ ] **Step 3: No additional code changes needed**

- [ ] **Step 4: Run all builder tests**

Run: `go test ./internal/builder/ -v -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/builder/detect.go
git commit -m "test: add nixpacks detection test coverage"
```

---

### Task 7: Run full test suite and verify

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: All PASS (except tests that skip due to Docker/nixpacks CLI unavailability)

- [ ] **Step 2: Run vet**

Run: `go vet ./...`
Expected: No warnings

- [ ] **Step 3: Build binary**

Run: `go build -o tengiz .`
Expected: Binary compiles

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: nixpacks build system integration"
```
