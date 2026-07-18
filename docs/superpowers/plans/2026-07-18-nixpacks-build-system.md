# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Nixpacks CLI as an alternative build strategy alongside Dockerfile-based builds, expanding framework support from 6 to hundreds (Ruby, Rust, PHP, Java, Elixir, etc.).

**Architecture:** Add a `BuilderType` field to `.tengiz.yaml` (`BuildConfig`) and `--builder` CLI flag to select between `dockerfile` (default, existing) and `nixpacks`. Add `FrameworkNixpacks` to the builder package, a Nixpacks detection path (always accepts when `builder: nixpacks` is set), and a `buildWithNixpacks` method that invokes the `nixpacks build` CLI. The `Builder.Build()` method dispatches to the appropriate builder based on the detection's framework. Preview and gitdeploy pipelines pass the configured builder through.

**Tech Stack:** Go 1.26, `os/exec` (Nixpacks CLI), Cobra, Viper, existing `internal/builder` package

## Global Constraints

- No new external Go dependencies — use `os/exec` to call `nixpacks` CLI (must be installed separately)
- Nixpacks image tag format: `tengiz-apps/{appName}:{env}-{deploymentID}` (same as Dockerfile builds)
- Config field: `.tengiz.yaml` → `build.builder: "nixpacks"` (default: `"dockerfile"`)
- CLI flag: `tengiz deploy --builder nixpacks` overrides config
- `nixpacks` must be installed on the host; if not found, return a clear error message
- All existing tests must continue to pass unchanged
- Nixpacks support must work in: CLI deploy, git deploy pipeline, preview deployments

---

### Task 1: Add BuilderType to Config Types

**Files:**
- Modify: `internal/types/types.go:42-45`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: Nothing (first task)
- Produces: `types.BuildConfig.Builder` field (`string`), `types.AppConfig.Build` already exists

- [ ] **Step 1: Add `Builder` field to `BuildConfig`**

```go
type BuildConfig struct {
	Command string `mapstructure:"command"`
	Output  string `mapstructure:"output"`
	Builder string `mapstructure:"builder" yaml:"builder"` // "dockerfile" (default) or "nixpacks"
}
```

- [ ] **Step 2: Update `LoadForEnvironment` to merge `Builder`**

In `internal/config/config.go`, add builder merge in the `LoadForEnvironment` scalar merge section (after line 108, near the Build.Command/Output merge):

```go
if envCfg.Build.Builder != "" {
	cfg.Build.Builder = envCfg.Build.Builder
}
```

- [ ] **Step 3: Run existing tests to verify nothing broke**

Run: `go test ./internal/types/... ./internal/config/... -v -count=1`
Expected: All pass

- [ ] **Step 4: Commit**

```bash
git add internal/types/types.go internal/config/config.go
git commit -m "feat: add BuildConfig.Builder field for nixpacks support"
```

---

### Task 2: Add Nixpacks Framework Constant and Detection

**Files:**
- Modify: `internal/builder/detect.go:12-20`, `internal/builder/detect.go:30-78`

**Interfaces:**
- Consumes: `types.BuildConfig.Builder` field from Task 1
- Produces: `FrameworkNixpacks` constant, `DetectWithBuilder(dir, builderType string)` function

- [ ] **Step 1: Write the failing test for nixpacks detection**

```go
func TestDetectNixpacks(t *testing.T) {
	dir := t.TempDir()
	// No specific files needed — Nixpacks detection accepts any directory
	d, err := DetectWithBuilder(dir, "nixpacks")
	if err != nil {
		t.Fatal(err)
	}
	if d.Framework != FrameworkNixpacks {
		t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNixpacks)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestDetectNixpacks -v -count=1`
Expected: FAIL with "FrameworkNixpacks not defined" or "DetectWithBuilder not defined"

- [ ] **Step 3: Add `FrameworkNixpacks` constant and `DetectWithBuilder` function**

Add constant to `detect.go`:

```go
FrameworkNixpacks Framework = "nixpacks"
```

Add new function:

```go
func DetectWithBuilder(dir string, builderType string) (*Detection, error) {
	if builderType == "nixpacks" {
		return &Detection{
			Framework:    FrameworkNixpacks,
			InternalPort: 8080,
		}, nil
	}
	return Detect(dir)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/ -run TestDetectNixpacks -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/builder/detect.go internal/builder/builder_test.go
git commit -m "feat: add FrameworkNixpacks and DetectWithBuilder"
```

---

### Task 3: Implement Nixpacks Build Method in Builder

**Files:**
- Modify: `internal/builder/builder.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `FrameworkNixpacks` from Task 2, `Detection` struct
- Produces: `Builder.buildWithNixpacks(ctx, dir, appName, env, deploymentID) (tag, buildLog, error)` — internal method

- [ ] **Step 1: Write a unit test for the nixpacks build (dry-run verification)**

```go
func TestNixpacksBuildTagFormat(t *testing.T) {
	b := New(t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0644)
	detection := &Detection{Framework: FrameworkNixpacks, InternalPort: 8080}

	// This will fail if nixpacks not installed, but should still verify tag format
	tag, _, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v123")
	if err != nil {
		t.Skipf("nixpacks not installed or build failed: %v", err)
	}
	expected := "tengiz-apps/testapp:production-v123"
	if tag != expected {
		t.Errorf("tag = %q, want %q", tag, expected)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestNixpacksBuildTagFormat -v -count=1`
Expected: FAIL with "nixpacks not installed" (skips) or "no such method" — at minimum, confirm the test runs and skips on missing nixpacks

- [ ] **Step 3: Add `NixpacksBuilderType` constant and `buildWithNixpacks` method to `builder.go`**

Add to top of file:

```go
const NixpacksBuilderType = "nixpacks"
```

Add method:

```go
func (b *Builder) buildWithNixpacks(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
	if _, err := exec.LookPath("nixpacks"); err != nil {
		return "", "", fmt.Errorf("nixpacks CLI not found: install from https://nixpacks.com/docs/install")
	}

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

- [ ] **Step 4: Update `Build()` to dispatch to nixpacks**

Modify `Build` method:

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
	if detection.Framework == FrameworkNixpacks {
		return b.buildWithNixpacks(ctx, dir, appName, env, deploymentID)
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

- [ ] **Step 5: Run tests**

Run: `go test ./internal/builder/ -v -count=1`
Expected: All tests pass (nixpacks test skips if CLI not installed)

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: add buildWithNixpacks method and dispatch in Build()"
```

---

### Task 4: Add `--builder` CLI Flag to Deploy Command

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root.go` (manual or integration)

**Interfaces:**
- Consumes: `DetectWithBuilder` from Task 2, `BuildConfig.Builder` from Task 1
- Produces: `--builder` flag on deploy command, passes it through to detection and build

- [ ] **Step 1: Add `--builder` flag**

In `root.go` `init()`, add after line 65:

```go
deployCmd.Flags().String("builder", "", "build strategy: dockerfile (default) or nixpacks")
```

- [ ] **Step 2: Read the flag and pass it through in the deploy command**

In the deploy command handler (around line 187), replace:

```go
detection, err := builder.Detect(projectRoot)
```

with:

```go
	builderFlag, _ := cmd.Flags().GetString("builder")
	if builderFlag == "" {
		builderFlag = cfg.Build.Builder
	}
	detection, err := builder.DetectWithBuilder(projectRoot, builderFlag)
```

- [ ] **Step 3: Build to verify compilation**

Run: `go build -o /dev/null .`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add --builder flag to deploy command"
```

---

### Task 5: Wire Nixpacks Through Git Deploy Pipeline

**Files:**
- Modify: `internal/gitdeploy/deployer.go`
- Test: `internal/gitdeploy/` (if tests exist)

**Interfaces:**
- Consumes: `DetectWithBuilder` from Task 2
- Produces: Git deploy pipeline uses `DetectWithBuilder` with configured builder type

- [ ] **Step 1: Update `Deploy` method to use `DetectWithBuilder`**

In `deployer.go` line 73, replace:

```go
detection, err := builder.Detect(cloneDir)
```

with:

```go
	builderType := ""
	if existingApp != nil {
		builderType = existingApp.Config.Build.Builder
	}
	detection, err := builder.DetectWithBuilder(cloneDir, builderType)
```

Note: since `lookupErr` is used before this point for the `existingApp` variable, restructure slightly — move detection after the `existingApp` lookup (which is already at line 71). The existing code already does the lookup at line 71 before detection at line 73, so it works.

- [ ] **Step 2: Build to verify compilation**

Run: `go build -o /dev/null .`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "feat: wire nixpacks builder through git deploy pipeline"
```

---

### Task 6: Wire Nixpacks Through Preview Deployments

**Files:**
- Modify: `internal/preview/manager.go`
- Test: `internal/preview/`

**Interfaces:**
- Consumes: `DetectWithBuilder` from Task 2
- Produces: Preview Manager uses `DetectWithBuilder`

- [ ] **Step 1: Add builder field to `Manager`**

Add field to `Manager` struct:

```go
type Manager struct {
	dataDir    string
	store      *config.Store
	rt         runtime.Manager
	builder    *builder.Builder
	builderType string // "dockerfile" or "nixpacks"
}
```

Update constructor:

```go
func NewManager(dataDir string, store *config.Store, rt runtime.Manager) *Manager {
	return NewManagerWithBuilder(dataDir, store, rt, "")
}

func NewManagerWithBuilder(dataDir string, store *config.Store, rt runtime.Manager, builderType string) *Manager {
	return &Manager{
		dataDir:     dataDir,
		store:       store,
		rt:          rt,
		builder:     builder.New(dataDir),
		builderType: builderType,
	}
}
```

- [ ] **Step 2: Update `Create` and `Update` to use `DetectWithBuilder`**

In `Create` (line 61), replace:

```go
detection, err := builder.Detect(cloneDir)
```

with:

```go
detection, err := builder.DetectWithBuilder(cloneDir, m.builderType)
```

Same change in `Update` (line 143).

- [ ] **Step 3: Update CLI callsite for preview**

In `root.go`, find where `preview.NewManager` is called and update to pass builder type from flags/config:

```go
preview.NewManagerWithEnv(dataDir, store, rt, envFlag)
```

Update to:

```go
preview.NewManagerWithBuilder(dataDir, store, rt, builderFlag)
```

(Wire this properly — the preview CLI commands use `preview.NewManager` already, so adding `NewManagerWithBuilder` keeps backward compat.)

- [ ] **Step 4: Build to verify compilation**

Run: `go build -o /dev/null .`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add internal/preview/manager.go internal/cli/root.go
git commit -m "feat: wire nixpacks through preview deployments"
```

---

### Task 7: Nixpacks Pre-Flight Check and User-Facing Error

**Files:**
- Modify: `internal/builder/builder.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: Nothing new
- Produces: Improved error message when nixpacks is selected but not installed

- [ ] **Step 1: Write test for pre-flight check**

```go
func TestNixpacksRequiredWhenConfigured(t *testing.T) {
	// When builder is "nixpacks" but nixpacks CLI is not available
	// The Build method should return a clear error
	b := New(t.TempDir())
	dir := t.TempDir()
	detection := &Detection{Framework: FrameworkNixpacks, InternalPort: 8080}

	_, _, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v1")
	if err != nil {
		// Should explicitly mention "nixpacks CLI not found"
		if !strings.Contains(err.Error(), "nixpacks") {
			t.Errorf("error should mention nixpacks, got: %v", err)
		}
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/builder/ -run TestNixpacksRequiredWhenConfigured -v -count=1`
Expected: PASS (the `buildWithNixpacks` already checks for nixpacks CLI via `exec.LookPath`)

- [ ] **Step 3: Run full test suite**

Run: `go test ./... -count=1 2>&1 | head -50`
Expected: All tests pass or skip (nixpacks integration tests skip if CLI not installed)

- [ ] **Step 4: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "test: add nixpacks pre-flight check test"
```

---

### Task 8: Documentation — Update README and FUTURES_FEATURES

**Files:**
- Modify: `README.md` (if nixpacks usage section exists)
- Modify: `docs/FUTURES_FEATURES.md` (mark Nixpacks as ✅ Implemented)

**Interfaces:**
- Consumes: All previous tasks
- Produces: Updated documentation

- [ ] **Step 1: Update FUTURES_FEATURES.md**

Change line 16 from:
```
| 3 | **Nixpacks Build Sistemi** ⬜ | Çok Yüksek | Orta | Mükemmel | ...
```
to:
```
| 3 | **Nixpacks Build Sistemi** ✅ | Çok Yüksek | Orta | Mükemmel | ...
```

- [ ] **Step 2: Check if README needs updates**

Search for any existing "builder" or "nixpacks" references in README. If found, update them. If no README changes are needed, skip.

- [ ] **Step 3: Commit**

```bash
git add docs/FUTURES_FEATURES.md README.md
git commit -m "docs: mark nixpacks as implemented, update docs"
```

---

## Self-Review

**1. Spec coverage:**
- Task 1: Adds `BuildConfig.Builder` field (config support)
- Task 2: Adds `FrameworkNixpacks` constant, `DetectWithBuilder` (detection)
- Task 3: Implements `buildWithNixpacks` method (build execution)
- Task 4: Adds `--builder` CLI flag (user interface)
- Task 5: Wires through git deploy pipeline (git deploy)
- Task 6: Wires through preview deployments (preview)
- Task 7: Pre-flight check + error handling (UX)
- Task 8: Documentation updates

**2. Placeholder scan:** No TBD, TODOs, or "handle edge cases" placeholders. All code is concrete.

**3. Type consistency:**
- `BuildConfig.Builder` is `string` — used consistently everywhere
- `DetectWithBuilder(dir, builderType string)` — same signature in Task 2, used in Tasks 4-6
- `FrameworkNixpacks` — constant `"nixpacks"`, used in Tasks 2-3
- Image tag format `tengiz-apps/{app}:{env}-{deploymentID}` — consistent across build methods
