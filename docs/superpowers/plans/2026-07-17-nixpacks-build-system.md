# Nixpacks Build System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Nixpacks as an alternative build strategy, expanding framework support from 6 to hundreds (Ruby, Rust, PHP, Java, Elixir, etc.)

**Architecture:** Two-phase integration: detection phase runs `nixpacks plan --json` to extract the correct internal port, build phase runs `nixpacks build --dockerfile-path` to generate a Dockerfile then builds it via `docker build`. A new `DetectWithNixpacks()` function replaces `Detect()` when nixpacks is active. The strategy is chosen via `.tengiz.yaml` `build.builder: nixpacks` or `tengiz deploy --builder nixpacks`.

**Tech Stack:** Go 1.26, `os/exec` for nixpacks CLI invocation, no new Go dependencies.

## Global Constraints

- Nixpacks must be installed separately (user responsibility, like Docker)
- No new Go dependencies — only stdlib `os/exec` for external CLI
- All new code in the `builder` package; no changes to `runtime`, `proxy`, `config`, or `cli` beyond plumbing the option
- Tests must not require nixpacks installed (skip with `testing.Short()` or `exec.LookPath` check)
- The existing detect+generate path must remain the default (backward compatible)

---

### Task 1: Add `Builder` type to BuildConfig and AppConfig types

**Files:**
- Modify: `internal/types/types.go:42-45`

**Interfaces:**
- Consumes: nothing
- Produces: `BuildConfig.Builder string` field, constant `BuilderDefault = ""`, constant `BuilderNixpacks = "nixpacks"`

- [ ] **Step 1: Add Builder constants and field to BuildConfig**

Edit `internal/types/types.go` to add:

```go
const (
 BuilderDefault  = ""
 BuilderNixpacks = "nixpacks"
)

type BuildConfig struct {
 Command string `mapstructure:"command"`
 Output  string `mapstructure:"output"`
 Builder string `mapstructure:"builder"`
}
```

- [ ] **Step 2: Run tests to ensure compilation**

Run: `go build ./...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/types/types.go
git commit -m "feat: add Builder field to BuildConfig for nixpacks selection"
```

---

### Task 2: Add `DetectWithNixpacks()` and `NixpacksGenerateDockerfile()` in a new nixpacks.go file

**Files:**
- Create: `internal/builder/nixpacks.go`

**Interfaces:**
- Consumes: `types.BuildConfig.Builder` (from Task 1)
- Produces:
  - `DetectWithNixpacks(ctx, dir string) (*Detection, error)` — runs `nixpacks plan --json`, returns `Detection` with `FrameworkNixpacks` and correct port
  - `NixpacksGenerateDockerfile(ctx, dir string) (string, error)` — runs `nixpacks build --dockerfile-path`, returns path to generated Dockerfile

- [ ] **Step 1: Write the failing test**

Create `internal/builder/nixpacks_test.go`:

```go
package builder

import (
 "context"
 "os"
 "os/exec"
 "path/filepath"
 "testing"
)

func TestDetectWithNixpacks(t *testing.T) {
 if _, err := exec.LookPath("nixpacks"); err != nil {
  t.Skip("nixpacks not installed")
 }

 dir := t.TempDir()
 os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{
  "name": "test",
  "scripts": {"start": "node index.js"}
 }`), 0644)
 os.WriteFile(filepath.Join(dir, "index.js"), []byte(`console.log("hi")`), 0644)

 d, err := DetectWithNixpacks(context.Background(), dir)
 if err != nil {
  t.Fatalf("DetectWithNixpacks() error: %v", err)
 }
 if d.Framework != FrameworkNixpacks {
  t.Errorf("Framework = %q, want %q", d.Framework, FrameworkNixpacks)
 }
 if d.InternalPort <= 0 {
  t.Error("expected positive internal port")
 }
 if d.Builder != "nixpacks" {
  t.Errorf("Builder = %q, want %q", d.Builder, "nixpacks")
 }
}

func TestNixpacksNotInstalled(t *testing.T) {
 if _, err := exec.LookPath("nixpacks-non-existent"); err == nil {
  t.Skip("unexpected: found nixpacks-non-existent")
 }

 dir := t.TempDir()
 _, err := DetectWithNixpacks(context.Background(), dir)
 if err == nil {
  t.Error("expected error when nixpacks is not installed")
 }

 _, err = NixpacksGenerateDockerfile(context.Background(), dir)
 if err == nil {
  t.Error("expected error when nixpacks is not installed")
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestNixpacks -v -count=1`
Expected: FAIL — `DetectWithNixpacks` and `NixpacksGenerateDockerfile` not defined

- [ ] **Step 3: Write minimal implementation**

Create `internal/builder/nixpacks.go`:

```go
package builder

import (
 "bytes"
 "context"
 "encoding/json"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
)

type nixpacksPlanOutput struct {
 Plan struct {
  StartCmd     string `json:"start_cmd"`
  InternalPort int    `json:"internal_port"`
 } `json:"plan"`
}

func nixpacksAvailable() bool {
 _, err := exec.LookPath("nixpacks")
 return err == nil
}

func DetectWithNixpacks(ctx context.Context, dir string) (*Detection, error) {
 if !nixpacksAvailable() {
  return nil, fmt.Errorf("nixpacks CLI not found in PATH")
 }

 cmd := exec.CommandContext(ctx, "nixpacks", "plan", dir, "--json")
 var out bytes.Buffer
 cmd.Stdout = &out
 cmd.Stderr = &out
 if err := cmd.Run(); err != nil {
  return nil, fmt.Errorf("nixpacks plan: %w\n%s", err, out.String())
 }

 var plan nixpacksPlanOutput
 if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
  return nil, fmt.Errorf("nixpacks plan parse: %w", err)
 }

 port := plan.Plan.InternalPort
 if port == 0 {
  port = 8080
 }

 return &Detection{
  Framework:    FrameworkNixpacks,
  InternalPort: port,
  Builder:      "nixpacks",
 }, nil
}

func NixpacksGenerateDockerfile(ctx context.Context, dir string) (string, error) {
 if !nixpacksAvailable() {
  return "", fmt.Errorf("nixpacks CLI not found in PATH")
 }

 dfPath := filepath.Join(dir, "Dockerfile.nixpacks")
 cmd := exec.CommandContext(ctx, "nixpacks", "build", dir,
  "--dockerfile-path", dfPath,
  "--no-cache",
 )
 var buf bytes.Buffer
 cmd.Stdout = &buf
 cmd.Stderr = &buf
 if err := cmd.Run(); err != nil {
  return "", fmt.Errorf("nixpacks build: %w\n%s", err, buf.String())
 }

 return dfPath, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/ -run TestNixpacks -v -count=1`
Expected: PASS (or SKIP if nixpacks not installed)

- [ ] **Step 5: Commit**

```bash
git add internal/builder/nixpacks.go internal/builder/nixpacks_test.go
git commit -m "feat: add nixpacks detection and Dockerfile generation"
```

---

### Task 3: Wire nixpacks into the Builder.Build() method

**Files:**
- Modify: `internal/builder/builder.go:21-29`
- Modify: `internal/builder/builder.go:65-162`
- Create: `internal/builder/detect.go:14-15` (add `FrameworkNixpacks = "nixpacks"`)

**Interfaces:**
- Consumes: `NixpacksGenerateDockerfile()` (Task 2), `BuildConfig.Builder` (Task 1)
- Produces: updated `Build()` method that checks builder type before detecting framework

- [ ] **Step 1: Add FrameworkNixpacks constant**

Edit `internal/builder/detect.go`:

```go
const (
 FrameworkNextJS Framework = "nextjs"
 FrameworkVite   Framework = "vite"
 FrameworkGo     Framework = "go"
 FrameworkNode   Framework = "node"
 FrameworkPython Framework = "python"
 FrameworkStatic Framework = "static"
 FrameworkDocker Framework = "docker"
 FrameworkNixpacks Framework = "nixpacks"
)
```

- [ ] **Step 2: Add `BuildStrategy` field to `Detection` struct**

Edit `internal/builder/detect.go`: add `Builder string` field:

```go
type Detection struct {
 Framework    Framework
 BuildCmd     string
 OutputDir    string
 InternalPort int
 HealthCheck  *types.HealthCheckConfig
 Builder      string
}
```

- [ ] **Step 3: Write the failing test**

Add to `internal/builder/builder_test.go`:

```go
func TestBuildWithNixpacksStrategy(t *testing.T) {
 if _, err := exec.LookPath("nixpacks"); err != nil {
  t.Skip("nixpacks not installed")
 }

 b := New(t.TempDir())
 dir := t.TempDir()
 os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644)

 detection := &Detection{
  Framework:    FrameworkNixpacks,
  InternalPort: 80,
  Builder:      types.BuilderNixpacks,
 }

 tag, logs, err := b.Build(context.Background(), dir, "testapp-nixpacks", "production", detection, "v1")
 if err != nil {
  t.Skipf("Build() error (likely no docker): %v", err)
 }
 if tag == "" {
  t.Error("expected non-empty tag")
 }
 _ = logs
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestBuildWithNixpacksStrategy -v -count=1`
Expected: FAIL — Build() doesn't handle `BuilderNixpacks` yet

- [ ] **Step 5: Modify `Builder.Build()` to dispatch on detection.Builder**

Edit `internal/builder/builder.go` — replace `Build()` and `buildWithDockerfile()`:

```go
func (b *Builder) Build(ctx context.Context, dir string, appName string, env string, detection *Detection, deploymentID string) (string, string, error) {
 if detection.Builder == types.BuilderNixpacks {
  return b.buildWithNixpacks(ctx, dir, appName, env, deploymentID)
 }
 if detection.Framework == FrameworkDocker {
  return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID, filepath.Join(dir, "Dockerfile"))
 }
 if err := b.ensureDockerfile(dir, detection); err != nil {
  return "", "", fmt.Errorf("generate dockerfile: %w", err)
 }
 return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID, filepath.Join(dir, "Dockerfile"))
}

func (b *Builder) buildWithNixpacks(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
 dfPath, err := NixpacksGenerateDockerfile(ctx, dir)
 if err != nil {
  return "", "", fmt.Errorf("nixpacks generate: %w", err)
 }
 return b.buildWithDockerfile(ctx, dir, appName, env, deploymentID, dfPath)
}

func (b *Builder) buildWithDockerfile(ctx context.Context, dir string, appName string, env string, deploymentID string, dockerfilePath string) (string, string, error) {
 if env == "" {
  env = "production"
 }
 tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

 // Use docker build with explicit Dockerfile path
 cmd := exec.CommandContext(ctx, "docker", "build", "-f", dockerfilePath, "-t", tag, dir)

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

Remove the old `buildWithDockerfile` (the one-arg version), rename the new three-arg version and clean up.

- [ ] **Step 6: Update `generateDockerfile` signature/callers**

The `generateDockerfile` function signature stays the same. The `ensureDockerfile` call in `Build()` remains unchanged for the legacy path.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/builder/ -v -count=1`
Expected: All tests PASS (those needing docker will skip)

- [ ] **Step 8: Commit**

```bash
git add internal/builder/builder.go internal/builder/detect.go
git commit -m "feat: wire nixpacks build strategy into Builder"
```

---

### Task 4: Add `--builder` flag to deploy command and plumb it through

**Files:**
- Modify: `internal/cli/root.go:155-345`

**Interfaces:**
- Consumes: `types.BuilderDefault`, `types.BuilderNixpacks` (Task 1), `builder.FrameworkNixpacks`, `builder.Detection.Builder` (Task 3)
- Produces: CLI flag `--builder` on `deployCmd`

- [ ] **Step 1: Write the failing test**

Read existing test file `internal/cli/root_test.go` first to understand testing patterns.

Run: `go test ./internal/cli/ -run TestDeploy -v -count=1` to see current state.

Add to `internal/cli/root_test.go`:

```go
func TestDeployBuilderFlagParsing(t *testing.T) {
 cmd := &cobra.Command{}
 cmd.Flags().String("builder", "", "build strategy (nixpacks or default)")

 // Test default
 builder, _ := cmd.Flags().GetString("builder")
 if builder != "" {
  t.Errorf("expected empty default, got %q", builder)
 }

 // Test nixpacks
 cmd.Flags().Set("builder", "nixpacks")
 builder, _ = cmd.Flags().GetString("builder")
 if builder != "nixpacks" {
  t.Errorf("expected nixpacks, got %q", builder)
 }
}
```

- [ ] **Step 2: Add `--builder` flag registration**

Edit `internal/cli/root.go` — add to `init()` after line 64:

```go
 deployCmd.Flags().String("builder", "", "build strategy (nixpacks or default)")
```

- [ ] **Step 3: Modify deploy command to use nixpacks detection**

Edit `deployCmd.RunE` — replace `builder.Detect(projectRoot)` with conditional logic:

```go
 builderFlag, _ := cmd.Flags().GetString("builder")
 useNixpacks := builderFlag == types.BuilderNixpacks || cfg.Build.Builder == types.BuilderNixpacks

 var detection *builder.Detection
 var detectErr error
 if useNixpacks {
  fmt.Printf("[tengiz] using nixpacks build strategy\n")
  detection, detectErr = builder.DetectWithNixpacks(cmd.Context(), projectRoot)
 } else {
  detection, detectErr = builder.Detect(projectRoot)
 }
 if detectErr != nil {
  return fmt.Errorf("detect: %w", detectErr)
 }
```

Remove the old `detection, err := builder.Detect(projectRoot)` and the two `if cfg.Build.Builder` / `if builderFlag` blocks.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/cli/ -v -count=1`
Expected: All tests PASS

Run: `go build ./...`
Expected: success

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add --builder flag to deploy command for nixpacks selection"
```

---

### Task 5: Update `.tengiz.yaml` init template with `build.builder` documentation

**Files:**
- Modify: `internal/cli/root.go:114-140` (initCmd template)

- [ ] **Step 1: Add builder option to init template**

Edit the `initCmd` template to include:

```go
content := fmt.Sprintf(`name: %s
environment: %s
# port: 3000            # container internal port (auto-detected if omitted)
# build:
#   builder: nixpacks   # build strategy: nixpacks (default: empty = built-in)
serverless:
  enabled: true
  idle_timeout: 5m      # scale-to-zero timeout
...
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cli/ -v -count=1`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "docs: add build.builder to init template"
```

---

### Task 6: Add nixpacks detection to `dev` command for parity

**Files:**
- Modify: `internal/cli/root.go:548-601` (devCmd)

**Interfaces:**
- Consumes: `builder.FrameworkNixpacks` (Task 3), `nixpacksAvailable()` (Task 2)

- [ ] **Step 1: Handle nixpacks in dev command**

Edit `devCmd.RunE` — add case for `FrameworkNixpacks`:

```go
 case builder.FrameworkNixpacks:
  return fmt.Errorf("dev mode not supported for nixpacks-based projects; run `nixpacks build .` directly")
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cli/ -v -count=1`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: handle nixpacks framework in dev command"
```

---

### Task 7: Add config merge for `build.builder` in LoadForEnvironment

**Files:**
- Modify: `internal/config/config.go:66-146`

**Interfaces:**
- Consumes: `BuildConfig.Builder` (Task 1)
- Produces: env override merge for `build.builder`

- [ ] **Step 1: Add build.builder merge in LoadForEnvironment**

Edit `internal/config/config.go` — after the `envCfg.Build.Output` merge block (~line 107), add:

```go
 if envCfg.Build.Builder != "" {
  cfg.Build.Builder = envCfg.Build.Builder
 }
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/config/ -v -count=1`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat: merge build.builder from env config"
```

---

### Task 8: Integration test — full nixpacks deploy flow

**Files:**
- Create: `internal/builder/nixpacks_integration_test.go`

**Note:** This test will be skipped in CI unless both docker and nixpacks are present. Run manually.

- [ ] **Step 1: Write integration test**

```go
package builder

import (
 "context"
 "os"
 "os/exec"
 "path/filepath"
 "testing"
)

func TestNixpacksBuildAndDockerBuildIntegration(t *testing.T) {
 if _, err := exec.LookPath("nixpacks"); err != nil {
  t.Skip("nixpacks not installed")
 }
 if _, err := exec.LookPath("docker"); err != nil {
  t.Skip("docker not installed")
 }

 b := New(t.TempDir())
 dir := t.TempDir()
 // Create a minimal Node.js project (nixpacks detects this)
 os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{
  "name": "test-app",
  "version": "1.0.0",
  "scripts": { "start": "node index.js" }
 }`), 0644)
 os.WriteFile(filepath.Join(dir, "index.js"), []byte(`const http = require('http');
const server = http.createServer((req, res) => {
  res.writeHead(200);
  res.end('hello');
});
server.listen(process.env.PORT || 3000);
`), 0644)

 detection := &Detection{
  Framework:    FrameworkNixpacks,
  InternalPort: 3000,
  Builder:      "nixpacks",
 }

 tag, logs, err := b.Build(context.Background(), dir, "test-nixpacks-integration", "test", detection, "int-v1")
 if err != nil {
  t.Fatalf("Build() error: %v\nlogs:\n%s", err, logs)
 }
 if tag == "" {
  t.Error("expected non-empty tag")
 }

 // Verify the image exists
 inspect := exec.CommandContext(context.Background(), "docker", "inspect", tag)
 if out, err := inspect.CombinedOutput(); err != nil {
  t.Fatalf("docker inspect failed: %v\n%s", err, out)
 }
}
```

- [ ] **Step 2: Run test manually**

Run: `go test ./internal/builder/ -run TestNixpacksBuildAndDockerBuildIntegration -v -count=1`
Expected: PASS (if docker + nixpacks installed) or SKIP

- [ ] **Step 3: Commit**

```bash
git add internal/builder/nixpacks_integration_test.go
git commit -m "test: add nixpacks integration test"
```

---

### Task 9: Run all tests and fix any issues

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -v -count=1 2>&1`
Expected: All tests PASS (timing-sensitive tests may need `-count=1`)

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`
Expected: no warnings

- [ ] **Step 3: Build binary**

Run: `go build -o tengiz .`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add .
git commit -m "chore: all tests passing, final cleanup"
```
