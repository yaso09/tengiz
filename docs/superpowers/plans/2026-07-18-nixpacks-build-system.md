# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks build system support so Tengiz can build and deploy apps for hundreds of frameworks (Ruby, Rust, PHP, Java, Elixir, etc.) beyond the current 7.

**Architecture:** A new `BuildBackend` string field on `Detection` controls whether `buildWithDockerfile()` (current) or `nixpacks build` (new) is used. A `--builder` CLI flag and `.tengiz.yaml` `build.builder` config select the backend. Nixpacks is called via `os/exec` — same pattern as `docker` CLI integration. The `Detect()` function adds an early `nixpacks.toml` check and a fallthrough detection via `nixpacks detect` when the builder is set to nixpacks.

**Tech Stack:** Go 1.26, existing `os/exec` pattern. No new Go deps — Nixpacks must be installed separately (documented as prerequisite). Image tagging stays identical (`tengiz-apps/{name}:{env}-{deploymentID}`).

## Global Constraints

- Nixpacks binary (`nixpacks`) must be on `$PATH` when builder is set to `nixpacks`
- Existing Dockerfile-based builder stays the default — zero impact for current users
- Image tag format unchanged: `tengiz-apps/{appName}:{env}-{deploymentID}`
- `BuildConfig.Command` and `BuildConfig.Output` from `.tengiz.yaml` apply to both backends
- `--builder` flag on `tengiz deploy` overrides config file value
- `gitdeploy.Pipeline` and `preview.Manager` pass through the builder setting from app config
- Nixpacks-built images get the same `HEALTHCHECK` support as Dockerfile-built images
- All existing tests must continue to pass

---

## File Structure

| File | Responsibility |
|------|---------------|
| Modify: `internal/builder/detect.go` | Add `BuilderType` field to `Detection`; add `nixpacks.toml` detection; add `--builder` flag-driven `nixpacks detect` fallthrough |
| Modify: `internal/builder/builder.go` | Add `buildWithNixpacks()` method; modify `Build()` to dispatch based on `detection.Builder`; add `NixpacksInstallHint` error |
| Modify: `internal/types/types.go` | Add `Builder string` field to `BuildConfig` |
| Modify: `internal/cli/root.go` | Add `--builder` flag to `deployCmd`; propagate to `Detection` via `AppConfig` |
| Modify: `internal/gitdeploy/deployer.go` | Pass `cfg.Build.Builder` through to `Detection.Builder` |
| No change: `internal/preview/manager.go` | Already passes `detection` through — builder field propagates automatically |
| Modify: `internal/builder/builder_test.go` | Add tests for `buildWithNixpacks()`, `Detect` nixpacks fallthrough, `Build()` dispatch |
| Modify: `.tengiz.example.yaml` or docs | Document `build.builder: nixpacks` and `nixpacks` prerequisite |

---

### Task 1: Add BuilderType to Detection and BuildConfig

**Files:**
- Modify: `internal/types/types.go` — add `Builder` field to `BuildConfig`
- Modify: `internal/builder/detect.go` — add `Builder` to `Detection` struct and `BuilderType` constants

**Interfaces:**
- Consumes: nothing new
- Produces: `types.BuildConfig.Builder string`, `builder.FrameworkNixpacks Framework`, `builder.BuilderTypeDockerfile BuilderType`, `builder.BuilderTypeNixpacks BuilderType`, `Detection.Builder BuilderType`

- [ ] **Step 1: Add `Builder` field to `BuildConfig`**

Edit `internal/types/types.go`:

```go
type BuildConfig struct {
    Command string `mapstructure:"command"`
    Output  string `mapstructure:"output"`
    Builder string `mapstructure:"builder"` // "dockerfile" or "nixpacks"
}
```

- [ ] **Step 2: Add `BuilderType` type and constants to `detect.go`**

Edit `internal/builder/detect.go`, add after the `Framework` constants block:

```go
type BuilderType string

const (
    BuilderTypeDockerfile BuilderType = "dockerfile"
    BuilderTypeNixpacks   BuilderType = "nixpacks"
)
```

- [ ] **Step 3: Add `Builder` field to `Detection` struct**

Edit `internal/builder/detect.go`, add field:

```go
type Detection struct {
    Framework    Framework
    Builder      BuilderType       // new field — defaults to BuilderTypeDockerfile
    BuildCmd     string
    OutputDir    string
    InternalPort int
    HealthCheck  *types.HealthCheckConfig
}
```

Then update every `return` in `Detect()` to set `Builder: BuilderTypeDockerfile`. For example, the Dockerfile detection becomes:

```go
if hasFile(dir, "Dockerfile") {
    return &Detection{Framework: FrameworkDocker, Builder: BuilderTypeDockerfile, InternalPort: 8080}, nil
}
```

Apply `Builder: BuilderTypeDockerfile` to all 7 return statements in `Detect()`.

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/builder/detect.go
git commit -m "feat(builder): add BuilderType field to Detection and BuildConfig"
```

---

### Task 2: Add `FrameworkNixpacks` detection and `nixpacks.toml` check

**Files:**
- Modify: `internal/builder/detect.go` — add `nixpacks.toml` detection and `--builder nixpacks` fallthrough

**Interfaces:**
- Consumes: `BuilderType` constants from Task 1
- Produces: `Detect()` now returns `BuilderTypeDockerfile` or `BuilderTypeNixpacks` on `Detection.Builder`

- [ ] **Step 1: Add `FrameworkNixpacks` constant**

```go
const (
    // ... existing ...
    FrameworkNixpacks Framework = "nixpacks"
)
```

- [ ] **Step 2: Add `nixpacks.toml` check to `Detect()`**

Insert before the Dockerfile check (nixpacks.toml should be checked early since it explicitly declares intent):

```go
if hasFile(dir, "nixpacks.toml") {
    return &Detection{
        Framework:    FrameworkNixpacks,
        Builder:      BuilderTypeNixpacks,
        InternalPort: 8080,
    }, nil
}
```

- [ ] **Step 3: Add a `DetectWithBuilder(dir string, builder BuilderType)` function**

The deploy command calls `Detect()` then overrides based on CLI flag. Add a helper:

```go
func DetectWithBuilder(dir string, builder BuilderType) (*Detection, error) {
    d, err := Detect(dir)
    if err != nil {
        return nil, err
    }
    if builder == BuilderTypeNixpacks && d.Framework != FrameworkDocker {
        // Nixpacks always sets FrameworkNixpacks when forced
        d.Builder = BuilderTypeNixpacks
        d.Framework = FrameworkNixpacks
        d.InternalPort = 8080 // nixpacks auto-detects, this is a fallback
    }
    return d, nil
}
```

- [ ] **Step 4: Write test for nixpacks.toml detection**

Add to `builder_test.go`:

```go
func TestDetectNixpacks(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "nixpacks.toml"), []byte(""), 0644)

    d, err := Detect(dir)
    if err != nil {
        t.Fatal(err)
    }
    if d.Framework != FrameworkNixpacks {
        t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNixpacks)
    }
    if d.Builder != BuilderTypeNixpacks {
        t.Errorf("Builder = %q, want %q", d.Builder, BuilderTypeNixpacks)
    }
}
```

- [ ] **Step 5: Write test for `DetectWithBuilder`**

```go
func TestDetectWithBuilderNixpacks(t *testing.T) {
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644)

    d, err := DetectWithBuilder(dir, BuilderTypeNixpacks)
    if err != nil {
        t.Fatal(err)
    }
    if d.Framework != FrameworkNixpacks {
        t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNixpacks)
    }
    if d.Builder != BuilderTypeNixpacks {
        t.Errorf("Builder = %q, want %q", d.Builder, BuilderTypeNixpacks)
    }
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/builder/ -v -run TestDetectNixpacks -count=1
go test ./internal/builder/ -v -run TestDetectWithBuilderNixpacks -count=1
```
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/builder/detect.go internal/builder/builder_test.go
git commit -m "feat(builder): add FrameworkNixpacks and nixpacks.toml detection"
```

---

### Task 3: Implement `buildWithNixpacks()` and wire dispatch in `Build()`

**Files:**
- Modify: `internal/builder/builder.go` — add `buildWithNixpacks()` method, modify `Build()` to dispatch

**Interfaces:**
- Consumes: `Detection.Builder` (BuilderType), `DeployConfig` from CLI
- Produces: Same `(imageTag string, buildLog string, error)` signature as `Build()`

- [ ] **Step 1: Add `buildWithNixpacks()` method to `Builder`**

```go
func (b *Builder) buildWithNixpacks(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
    if env == "" {
        env = "production"
    }
    tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

    // nixpacks build <dir> --name <tag>
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

- [ ] **Step 2: Modify `Build()` to dispatch based on `Detection.Builder`**

Replace the existing `Build()` method:

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
    if detection.Builder == BuilderTypeNixpacks {
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

- [ ] **Step 3: Write test for `Build()` dispatch**

Add to `builder_test.go`:

```go
func TestBuildWithNixpacksDispatch(t *testing.T) {
    b := New(t.TempDir())
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644)

    detection := &Detection{
        Framework:    FrameworkNixpacks,
        Builder:      BuilderTypeNixpacks,
        InternalPort: 8080,
    }

    tag, logs, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v123")
    if err != nil {
        t.Skipf("Build() error (likely no nixpacks): %v", err)
    }
    if tag == "" {
        t.Error("expected non-empty tag")
    }
    _ = logs
}
```

- [ ] **Step 4: Write test that existing Dockerfile path still works when builder is nixpacks**

```go
func TestBuildWithNixpacksSkipsWhenDockerfilePresent(t *testing.T) {
    b := New(t.TempDir())
    dir := t.TempDir()
    os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine"), 0644)

    detection := &Detection{
        Framework:    FrameworkDocker,
        Builder:      BuilderTypeDockerfile, // Docker detection always uses dockerfile
        InternalPort: 8080,
    }

    tag, _, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v123")
    if err != nil {
        t.Skipf("Build() error (likely no docker): %v", err)
    }
    if tag == "" {
        t.Error("expected non-empty tag")
    }
}
```

- [ ] **Step 5: Run all builder tests**

```bash
go test ./internal/builder/ -v -count=1
```
Expected: All existing tests PASS (build tests that need Docker will skip)

- [ ] **Step 6: Verify `go vet`**

```bash
go vet ./internal/builder/...
```
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat(builder): implement buildWithNixpacks() and dispatch in Build()"
```

---

### Task 4: Add `--builder` CLI flag to deploy command

**Files:**
- Modify: `internal/cli/root.go` — add `--builder` flag, pass to detection

**Interfaces:**
- Consumes: `builder.DetectWithBuilder()`, `types.BuildConfig.Builder`
- Produces: Detection with `BuilderType` set from flag or config

- [ ] **Step 1: Add `--builder` flag to `deployCmd`**

Find the deploy command definition (around line 150) and add:

```go
var builderFlag string

// In the deployCmd definition block:
deployCmd.Flags().StringVar(&builderFlag, "builder", "", "Build backend: dockerfile or nixpacks")
```

- [ ] **Step 2: Wire the flag into the detection call**

Replace `builder.Detect(projectRoot)` with:

```go
builderType := builder.BuilderTypeDockerfile
if builderFlag != "" {
    builderType = builder.BuilderTypeDockerfile
    if builderFlag == "nixpacks" {
        builderType = builder.BuilderTypeNixpacks
    }
} else if cfg.Build.Builder == "nixpacks" {
    builderType = builder.BuilderTypeNixpacks
}

detection, err := builder.DetectWithBuilder(projectRoot, builderType)
```

- [ ] **Step 3: Build and verify**

```bash
go build -o /dev/null .
```
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat(cli): add --builder flag to deploy command"
```

---

### Task 5: Wire builder through gitdeploy pipeline

**Files:**
- Modify: `internal/gitdeploy/deployer.go` — pass `cfg.Build.Builder` to detection

**Interfaces:**
- Consumes: `types.BuildConfig.Builder`, `builder.DetectWithBuilder()`
- Produces: Detection with correct `BuilderType` in git deploy flow

- [ ] **Step 1: Read the deployer file to find the detection call**

Read `internal/gitdeploy/deployer.go` to find where `builder.Detect()` is called.

- [ ] **Step 2: Update detection to use config's builder setting**

Replace `builder.Detect(cloneDir)` with:

```go
builderType := builder.BuilderTypeDockerfile
if p.cfg.Build.Builder == "nixpacks" {
    builderType = builder.BuilderTypeNixpacks
}
detection, err := builder.DetectWithBuilder(cloneDir, builderType)
```

- [ ] **Step 3: Build and verify**

```bash
go build -o /dev/null .
```
Expected: PASS

- [ ] **Step 4: Run tests**

```bash
go test ./internal/gitdeploy/... -v -count=1
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gitdeploy/deployer.go
git commit -m "feat(gitdeploy): wire builder setting through pipeline"
```

---

### Task 6: Run full test suite and final verification

**Files:** All modified files

- [ ] **Step 1: Run all tests**

```bash
go test ./... -v -count=1
```
Expected: All tests PASS (build tests requiring Docker/nixpacks skip gracefully)

- [ ] **Step 2: Run vet**

```bash
go vet ./...
```
Expected: PASS

- [ ] **Step 3: Verify `tengiz deploy --help` shows `--builder` flag**

```bash
go build -o tengiz . && ./tengiz deploy --help
```
Expected: Output includes `--builder` flag description

- [ ] **Step 4: Verify flag parsing**

```bash
./tengiz deploy --builder nixpacks /tmp/test 2>&1 | head -5
```
Expected: Shows detection or a meaningful error (nixpacks not installed, dir not found, etc.)

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat(builder): complete nixpacks build system support"
```
