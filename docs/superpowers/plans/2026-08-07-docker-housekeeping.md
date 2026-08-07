# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely prunes unused Docker resources (stopped containers, unused images, networks, volumes, build cache) to reclaim disk space while always protecting Tengiz-managed containers and images.

**Architecture:** The runtime package gains a `Prune(ctx, opts) (PruneResult, error)` method on the `runtime.Manager` interface. The Docker implementation runs `docker system prune -f --filter label!=tengiz-app [--all] [--volumes]` and parses the "Deleted …"/"Total reclaimed space" output. Tengiz resources are protected in two complementary ways: every Tengiz container already carries the `tengiz-app` label (the `label!=tengiz-app` filter keeps them), and every Tengiz build image will now also be labeled at build time so the `--all` (unused-image) mode cannot delete rollback-retained images. A `--dry-run` mode reports reclaimable space via `docker system df`. The CLI wires this up as `tengiz cleanup` with a confirmation prompt (skippable via `--force`).

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (no Docker SDK), existing `runtime.Manager`, `builder.Builder`, `internal/types` packages. No new dependencies.

## Global Constraints

- Docker CLI is invoked via `os/exec`; Docker must be installed and in `PATH` (checked by `runtime.NewDocker()`).
- The prune protection filter is exactly `label!=tengiz-app`, built from `types.LabelApp` — never a literal in a third place.
- New label constants `types.LabelApp = "tengiz-app"` and `types.LabelEnv = "tengiz-env"` are the single source of truth, used by both `runtime` (containers, prune filter) and `builder` (image labels). No duplicate label strings anywhere.
- Containers and images labeled `tengiz-app` are NEVER pruned by `tengiz cleanup`.
- Images built by Tengiz carry `tengiz-app` and `tengiz-env` labels so `--all` cannot remove images retained for rollback by `KeepLastNImages`.
- `tengiz cleanup` is interactive by default: it prompts before deleting unless `--force`/`-f` is given.
- No new external Go dependencies.
- Per AGENTS.md, feature work happens on a branch: `git checkout -b feat/docker-cleanup`.
- After each task: `go build -o tengiz .`, `go test ./... -v -count=1`, `go vet ./...` must all pass.
- Tests must not depend on a live Docker daemon. Unit tests exercise the pure arg-building/parsing helpers and the stub/mock managers.
- `docker system prune` is always invoked with `-f` (the CLI provides its own confirmation prompt).
- `--env` is NOT required on `cleanup` (it is a host-level operation, not app-scoped).

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | Add exported `LabelApp` / `LabelEnv` label-key constants |
| `internal/runtime/docker.go` | Replace local `labelKey`/`envLabelKey` consts with `types.LabelApp`/`types.LabelEnv` |
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneResult` types; add `Prune` to `Manager` interface + `stubManager` |
| `internal/runtime/cleanup.go` | Implement `Prune`: `buildPruneArgs`, `parsePruneOutput`, `pruneDryRun` |
| `internal/runtime/cleanup_test.go` | Unit tests for `buildPruneArgs`, `parsePruneOutput`, stub `Prune` |
| `internal/builder/builder.go` | Label images at build time; extract `buildImageArgs` / `nixpacksBuildArgs` helpers |
| `internal/builder/builder_test.go` | Tests that image build args carry `tengiz-app`/`tengiz-env` labels |
| `internal/cli/root.go` | Add `cleanupCmd`, `cleanupFlags`, `runCleanup`, `confirmCleanup` |
| `internal/cli/root_test.go` | Tests: command registration, flags, `runCleanup` behavior, `confirmCleanup`; add `Prune` to `mockRTForDeploy` + new cleanup mocks |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` (interface compliance) |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` (interface compliance) |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to the Commands list |
| `docs/FUTURES_FEATURES.md` | Mark P0 #6 Docker Housekeeping as implemented |

---

### Task 1: Shared label constants in `types` + refactor `runtime`

**Files:**
- Modify: `internal/types/types.go` (add constants after `import "time"`, ~line 3)
- Modify: `internal/runtime/docker.go:76-77` and the 10 call sites listed below
- Test: `internal/types/types_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `types.LabelApp string` (= `"tengiz-app"`), `types.LabelEnv string` (= `"tengiz-env"`). Later tasks depend on these exact exported names.

- [ ] **Step 1: Write the failing test**

Append to `internal/types/types_test.go`:

```go
func TestLabelConstants(t *testing.T) {
	if LabelApp != "tengiz-app" {
		t.Errorf("LabelApp = %q, want %q", LabelApp, "tengiz-app")
	}
	if LabelEnv != "tengiz-env" {
		t.Errorf("LabelEnv = %q, want %q", LabelEnv, "tengiz-env")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/ -run TestLabelConstants -v`
Expected: FAIL — `undefined: LabelApp`

- [ ] **Step 3: Implement constants and refactor `runtime`**

Add to `internal/types/types.go`, immediately after the `import "time"` block:

```go
const (
	LabelApp = "tengiz-app"
	LabelEnv = "tengiz-env"
)
```

In `internal/runtime/docker.go`, delete lines 76-77 (the local constants):

```go
const labelKey = "tengiz-app"
const envLabelKey = "tengiz-env"
```

Then replace every remaining use of the old identifiers with the shared constants. The exact old → new pairs are (use the Edit tool; identical pairs may use `replaceAll`):

| Old | New | Appears at lines |
|-----|-----|------------------|
| `fmt.Sprintf("%s=%s", labelKey, cfg.Name)` | `fmt.Sprintf("%s=%s", types.LabelApp, cfg.Name)` | 98, 125, 516 |
| `fmt.Sprintf("%s=%s", envLabelKey, cfg.Environment)` | `fmt.Sprintf("%s=%s", types.LabelEnv, cfg.Environment)` | 99, 126, 517 |
| `fmt.Sprintf("%s=%s", labelKey, name)` | `fmt.Sprintf("%s=%s", types.LabelApp, name)` | 160 |
| `"--filter", fmt.Sprintf("label=%s", labelKey)` | `"--filter", fmt.Sprintf("label=%s", types.LabelApp)` | 393 |
| `if len(kv) == 2 && kv[0] == labelKey {` | `if len(kv) == 2 && kv[0] == types.LabelApp {` | 413 |
| `args = append(args, "--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name))` | `args = append(args, "--label", fmt.Sprintf("%s=%s", types.LabelApp, cfg.Name))` | 456 |

`internal/runtime/docker.go` already imports `github.com/yaso09/tengiz/internal/types`, so no import changes are needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/types/ ./internal/runtime/ -count=1`
Expected: PASS for both packages (no other references to `labelKey`/`envLabelKey` remain).

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go internal/runtime/docker.go
git commit -m "feat(types): add shared Docker label constants for tengiz resources"
```

---

### Task 2: `Prune` API on `runtime.Manager` + Docker implementation

**Files:**
- Modify: `internal/runtime/runtime.go` — add `PruneOptions`, `PruneResult`, `Prune` to `Manager` + `stubManager`
- Modify: `internal/runtime/cleanup.go` — implement `Prune` (`buildPruneArgs`, `parsePruneOutput`, `pruneDryRun`), add `types` import
- Modify: `internal/runtime/cleanup_test.go` — add unit tests
- Modify: `internal/proxy/proxy_test.go:35` — add `Prune` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:34` — add `Prune` to `mockRuntime`
- Modify: `internal/cli/root_test.go:100` — add `Prune` to `mockRTForDeploy`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `types.LabelApp` (from Task 1)
- Produces:
  - `runtime.PruneOptions struct { All bool; Volumes bool; DryRun bool }`
  - `runtime.PruneResult struct { Containers, Images, Networks, Volumes, BuildCache []string; Reclaimed string; Reclaimable []string }`
  - `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`
  - package-private `buildPruneArgs(opts PruneOptions) []string`, `parsePruneOutput(out []byte) PruneResult`, and method `(*dockerRuntime).pruneDryRun(ctx context.Context) (PruneResult, error)`
  - Task 4 consumes `runtime.PruneOptions` and `runtime.PruneResult` with exactly these field names.

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		opts PruneOptions
		want []string
	}{
		{"default", PruneOptions{}, []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"all", PruneOptions{All: true}, []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "--all"}},
		{"volumes", PruneOptions{Volumes: true}, []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "--volumes"}},
		{"all+volumes", PruneOptions{All: true, Volumes: true}, []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "--all", "--volumes"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneArgs(tt.opts)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildPruneArgs(%+v) = %v, want %v", tt.opts, got, tt.want)
			}
		})
	}
}

func TestParsePruneOutput(t *testing.T) {
	out := `Deleted Containers:
abc123

Deleted Images:
untagged: foo:latest
deleted: sha256:xyz

Deleted Networks:
net1

Deleted build cache objects:
cache1

Total reclaimed space: 1.234GB
`
	res := parsePruneOutput([]byte(out))
	if len(res.Containers) != 1 || res.Containers[0] != "abc123" {
		t.Errorf("Containers = %v", res.Containers)
	}
	if len(res.Images) != 2 || res.Images[0] != "untagged: foo:latest" {
		t.Errorf("Images = %v", res.Images)
	}
	if len(res.Networks) != 1 || res.Networks[0] != "net1" {
		t.Errorf("Networks = %v", res.Networks)
	}
	if len(res.BuildCache) != 1 || res.BuildCache[0] != "cache1" {
		t.Errorf("BuildCache = %v", res.BuildCache)
	}
	if res.Reclaimed != "1.234GB" {
		t.Errorf("Reclaimed = %q, want %q", res.Reclaimed, "1.234GB")
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	res := parsePruneOutput([]byte("Total reclaimed space: 0B\n"))
	if len(res.Containers) != 0 || res.Reclaimed != "0B" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(res.Containers) != 0 || res.Reclaimed != "" {
		t.Errorf("expected empty result, got %+v", res)
	}
}
```

Add the `reflect` import to `internal/runtime/cleanup_test.go` (it currently imports only `context` and `testing`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestBuildPruneArgs|TestParsePruneOutput|TestStubPrune' -v`
Expected: FAIL — `buildPruneArgs undefined`, `parsePruneOutput undefined`, `Prune undefined`

- [ ] **Step 3: Add the types + interface method**

In `internal/runtime/runtime.go`, after the `RunOptions` struct (around line 29), add:

```go
type PruneOptions struct {
	All     bool
	Volumes bool
	DryRun  bool
}

type PruneResult struct {
	Containers  []string
	Images      []string
	Networks    []string
	Volumes     []string
	BuildCache  []string
	Reclaimed   string
	Reclaimable []string
}
```

Add to the `Manager` interface (after the `Run` line):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

Add to the `stubManager` (after its `Run` method):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}
```

- [ ] **Step 4: Implement `Prune` on `dockerRuntime`**

In `internal/runtime/cleanup.go`, replace the current import block with:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)
```

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	if opts.DryRun {
		return r.pruneDryRun(ctx)
	}
	args := buildPruneArgs(opts)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneResult{}, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(out), nil
}

func buildPruneArgs(opts PruneOptions) []string {
	args := []string{"system", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", types.LabelApp)}
	if opts.All {
		args = append(args, "--all")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	return args
}

func parsePruneOutput(out []byte) PruneResult {
	var res PruneResult
	sections := map[string]*[]string{
		"Deleted Containers:":        &res.Containers,
		"Deleted Images:":            &res.Images,
		"Deleted Networks:":          &res.Networks,
		"Deleted Volumes:":           &res.Volumes,
		"Deleted build cache objects:": &res.BuildCache,
	}
	var current *[]string
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if ptr, ok := sections[trimmed]; ok {
			current = ptr
			continue
		}
		if strings.HasPrefix(trimmed, "Total reclaimed space: ") {
			res.Reclaimed = strings.TrimPrefix(trimmed, "Total reclaimed space: ")
			current = nil
			continue
		}
		if current != nil && trimmed != "" {
			*current = append(*current, trimmed)
		}
	}
	return res
}

func (r *dockerRuntime) pruneDryRun(ctx context.Context) (PruneResult, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{.Type}}|{{.Reclaimable}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneResult{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	var res PruneResult
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			res.Reclaimable = append(res.Reclaimable, line)
		}
	}
	return res, nil
}
```

- [ ] **Step 5: Update the three test mocks so the interface compiles**

`internal/proxy/proxy_test.go`, after the `Run` method (line 35):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

`internal/idle/idle_test.go`, after the `Run` method (line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

`internal/cli/root_test.go`, after the `Run` method (line 100):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 6: Run the full test suite to verify everything passes**

Run: `go test ./... -count=1`
Expected: PASS (all packages, including the three updated mocks).

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add label-protected docker system prune (tengiz cleanup backend)"
```

---

### Task 3: Label Tengiz build images so `--all` pruning is safe

**Files:**
- Modify: `internal/builder/builder.go` — extract `buildImageArgs` / `nixpacksBuildArgs` helpers; add labels to both build paths
- Modify: `internal/builder/builder_test.go` — add tests
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.LabelApp`, `types.LabelEnv` (from Task 1)
- Produces: package-private helpers `buildImageArgs(dir, appName, env, deploymentID string, secretArgs []string) []string` and `nixpacksBuildArgs(dir, appName, env, deploymentID string, cfg *types.NixpacksConfig) []string`. Their return values must contain `--label tengiz-app=<app>` and `--label tengiz-env=<env>` exactly.

- [ ] **Step 1: Write the failing tests**

Append to `internal/builder/builder_test.go`:

```go
func TestBuildImageArgsIncludesLabels(t *testing.T) {
	args := buildImageArgs("/app", "myapp", "production", "v1", nil)
	want := []string{
		"build", "-t", "tengiz-apps/myapp:production-v1",
		"--label", "tengiz-app=myapp",
		"--label", "tengiz-env=production",
		"/app",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("buildImageArgs() = %v, want %v", args, want)
	}
}

func TestBuildImageArgsPreservesSecrets(t *testing.T) {
	args := buildImageArgs("/app", "myapp", "production", "v1", []string{"--secret", "id=TOKEN,src=/tmp/x"})
	found := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--secret" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --secret to be preserved, got %v", args)
	}
}

func TestNixpacksBuildArgsIncludesLabels(t *testing.T) {
	args := nixpacksBuildArgs("/app", "myapp", "staging", "v2", nil)
	want := []string{
		"build", "/app", "--name", "tengiz-apps/myapp:staging-v2",
		"--label", "tengiz-app=myapp",
		"--label", "tengiz-env=staging",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("nixpacksBuildArgs() = %v, want %v", args, want)
	}
}
```

Add the `reflect` import to `internal/builder/builder_test.go` if not already present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/builder/ -run 'TestBuildImageArgs|TestNixpacksBuildArgs' -v`
Expected: FAIL — `buildImageArgs undefined`, `nixpacksBuildArgs undefined`

- [ ] **Step 3: Extract helpers and add labels in `buildWithDockerfile`**

In `internal/builder/builder.go`, replace the argument-building portion of `buildWithDockerfile` (lines 69-71):

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

with:

```go
	args := buildImageArgs(dir, appName, env, deploymentID, b.buildSecretArgs())
```

Add the helper at package level (place it below `buildWithDockerfile`, above `buildSecretArgs`):

```go
func buildImageArgs(dir, appName, env, deploymentID string, secretArgs []string) []string {
	tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)
	args := []string{"build"}
	args = append(args, secretArgs...)
	args = append(args, "-t", tag)
	args = append(args, "--label", fmt.Sprintf("%s=%s", types.LabelApp, appName))
	args = append(args, "--label", fmt.Sprintf("%s=%s", types.LabelEnv, env))
	args = append(args, dir)
	return args
}
```

- [ ] **Step 4: Add labels in `buildWithNixpacks`**

In `internal/builder/builder.go`, in `buildWithNixpacks`, replace the arg-building block (lines 139-150):

```go
	args := []string{"build", dir, "--name", tag}
	if b.nixpacksCfg != nil {
		if len(b.nixpacksCfg.Packages) > 0 {
			args = append(args, "--pkgs", strings.Join(b.nixpacksCfg.Packages, ","))
		}
		if len(b.nixpacksCfg.AptPackages) > 0 {
			args = append(args, "--apt-pkgs", strings.Join(b.nixpacksCfg.AptPackages, ","))
		}
		if b.nixpacksCfg.Cmd != "" {
			args = append(args, "--cmd", b.nixpacksCfg.Cmd)
		}
	}
```

with:

```go
	args := nixpacksBuildArgs(dir, appName, env, deploymentID, b.nixpacksCfg)
```

Add the helper at package level (below `buildWithNixpacks`):

```go
func nixpacksBuildArgs(dir, appName, env, deploymentID string, cfg *types.NixpacksConfig) []string {
	tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)
	args := []string{"build", dir, "--name", tag}
	args = append(args, "--label", fmt.Sprintf("%s=%s", types.LabelApp, appName))
	args = append(args, "--label", fmt.Sprintf("%s=%s", types.LabelEnv, env))
	if cfg != nil {
		if len(cfg.Packages) > 0 {
			args = append(args, "--pkgs", strings.Join(cfg.Packages, ","))
		}
		if len(cfg.AptPackages) > 0 {
			args = append(args, "--apt-pkgs", strings.Join(cfg.AptPackages, ","))
		}
		if cfg.Cmd != "" {
			args = append(args, "--cmd", cfg.Cmd)
		}
	}
	return args
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/builder/ -count=1`
Expected: PASS (new tests plus existing detection/Dockerfile tests).

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat(builder): label build images so cleanup --all preserves rollback images"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `bufio` import; add `cleanupCmd`, `cleanupFlags`, `runCleanup`, `confirmCleanup`; register command + flags in `init()`
- Modify: `internal/cli/root_test.go` — add cleanup tests + `mockCleanupRT` / `mockDryRunRT`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.Manager` (with `Prune(ctx, PruneOptions) (PruneResult, error)`), `runtime.PruneOptions{All, Volumes, DryRun bool}`, `runtime.PruneResult{Containers, Images, Networks, Volumes, BuildCache []string; Reclaimed string; Reclaimable []string}` (from Task 2)
- Produces:
  - `cleanupCmd *cobra.Command` (registered as `tengiz cleanup`)
  - `cleanupFlags struct { all, volumes, force, dryRun bool }`
  - `runCleanup(rt runtime.Manager, fl cleanupFlags, stdin io.Reader) (string, error)` — testable command logic
  - `confirmCleanup(r io.Reader) bool` — prompt helper

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"all", "volumes", "force", "dry-run"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanup missing --%s flag", name)
		}
	}
}

type mockCleanupRT struct {
	*mockRTForDeploy
	pruned   atomic.Int32
	lastOpts runtime.PruneOptions
}

func (m *mockCleanupRT) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	m.pruned.Add(1)
	m.lastOpts = opts
	return runtime.PruneResult{
		Containers: []string{"abc"},
		Images:     []string{"sha256:x"},
		Reclaimed:  "1.5GB",
	}, nil
}

type mockDryRunRT struct {
	*mockRTForDeploy
}

func (m *mockDryRunRT) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{
		Reclaimable: []string{"Images|4.446MB (100%)", "Containers|0B"},
	}, nil
}

func TestRunCleanupForcePrunes(t *testing.T) {
	m := &mockCleanupRT{mockRTForDeploy: &mockRTForDeploy{}}
	out, err := runCleanup(m, cleanupFlags{force: true, all: true, volumes: true}, strings.NewReader(""))
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if m.pruned.Load() != 1 {
		t.Fatalf("Prune called %d times, want 1", m.pruned.Load())
	}
	if !m.lastOpts.All || !m.lastOpts.Volumes {
		t.Errorf("lastOpts = %+v, want All and Volumes", m.lastOpts)
	}
	if !strings.Contains(out, "removed 1 containers, 1 images, 0 networks, 0 volumes") {
		t.Errorf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "total reclaimed space: 1.5GB") {
		t.Errorf("missing reclaimed space line: %q", out)
	}
}

func TestRunCleanupConfirmYes(t *testing.T) {
	m := &mockCleanupRT{mockRTForDeploy: &mockRTForDeploy{}}
	_, err := runCleanup(m, cleanupFlags{}, strings.NewReader("y\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.pruned.Load() != 1 {
		t.Errorf("expected prune after 'y', called %d times", m.pruned.Load())
	}
}

func TestRunCleanupConfirmNo(t *testing.T) {
	m := &mockCleanupRT{mockRTForDeploy: &mockRTForDeploy{}}
	out, err := runCleanup(m, cleanupFlags{}, strings.NewReader("n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.pruned.Load() != 0 {
		t.Errorf("expected no prune after 'n', called %d times", m.pruned.Load())
	}
	if !strings.Contains(out, "Aborted") {
		t.Errorf("expected Aborted output, got %q", out)
	}
}

func TestRunCleanupDryRun(t *testing.T) {
	m := &mockDryRunRT{mockRTForDeploy: &mockRTForDeploy{}}
	out, err := runCleanup(m, cleanupFlags{dryRun: true, force: true}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Reclaimable Docker resources:") {
		t.Errorf("expected dry-run header, got %q", out)
	}
	if !strings.Contains(out, "Images") || !strings.Contains(out, "4.446MB (100%)") {
		t.Errorf("expected per-type reclaimable lines, got %q", out)
	}
}

func TestConfirmCleanup(t *testing.T) {
	if !confirmCleanup(strings.NewReader("y\n")) {
		t.Error("confirmCleanup('y') = false, want true")
	}
	if !confirmCleanup(strings.NewReader("yes\n")) {
		t.Error("confirmCleanup('yes') = false, want true")
	}
	if confirmCleanup(strings.NewReader("n\n")) {
		t.Error("confirmCleanup('n') = true, want false")
	}
	if confirmCleanup(strings.NewReader("\n")) {
		t.Error("confirmCleanup(empty) = true, want false")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestRunCleanup|TestConfirmCleanup' -v`
Expected: FAIL — `cleanupCmd undefined`, `runCleanup undefined`, `confirmCleanup undefined`

- [ ] **Step 3: Add `bufio` to the import block**

In `internal/cli/root.go`, change the import block to add `bufio` as the first stdlib import:

```go
import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"
```

- [ ] **Step 4: Register the command and flags in `init()`**

In `internal/cli/root.go` `init()`, add after `rootCmd.AddCommand(runCmd)` (line 67):

```go
	rootCmd.AddCommand(cleanupCmd)
```

And add the flag definitions at the end of `init()` (after line 88):

```go
	cleanupCmd.Flags().BoolP("all", "a", false, "also remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	cleanupCmd.Flags().BoolP("force", "f", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Bool("dry-run", false, "show reclaimable space per category without removing anything")
```

- [ ] **Step 5: Add the command and helpers**

Add `cleanupFlags` right after the `getEnv` helper (after line 103):

```go
type cleanupFlags struct {
	all     bool
	volumes bool
	force   bool
	dryRun  bool
}
```

Add the command (place it before `var stopCmd`, after the `psCmd` block at line 601):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Remove stopped containers, dangling images, unused networks, and unused build cache
from the local Docker daemon to reclaim disk space.

Containers and images managed by Tengiz (labeled "tengiz-app") are always protected.

Flags:
  -a, --all      Also remove all unused images, not just dangling ones
      --volumes  Also remove unused volumes
  -f, --force    Skip the confirmation prompt
      --dry-run  Show reclaimable space per category without removing anything

Examples:
  tengiz cleanup                # prompts before deleting anything
  tengiz cleanup --force        # non-interactive, CI-friendly
  tengiz cleanup --all --force  # aggressive: also removes unused images`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		out, err := runCleanup(rt, cleanupFlags{all: all, volumes: volumes, force: force, dryRun: dryRun}, os.Stdin)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}
```

Add the two helpers right after the `cleanupCmd` block:

```go
func runCleanup(rt runtime.Manager, fl cleanupFlags, stdin io.Reader) (string, error) {
	var sb strings.Builder
	if fl.dryRun {
		res, err := rt.Prune(context.Background(), runtime.PruneOptions{DryRun: true})
		if err != nil {
			return "", err
		}
		sb.WriteString("Reclaimable Docker resources:\n")
		if len(res.Reclaimable) == 0 {
			sb.WriteString("  (nothing)\n")
		}
		for _, line := range res.Reclaimable {
			parts := strings.SplitN(line, "|", 2)
			if len(parts) == 2 {
				fmt.Fprintf(&sb, "  %-12s %s\n", parts[0], parts[1])
			} else {
				fmt.Fprintf(&sb, "  %s\n", line)
			}
		}
		return sb.String(), nil
	}

	if !fl.force {
		if !confirmCleanup(stdin) {
			return "Aborted.\n", nil
		}
	}

	res, err := rt.Prune(context.Background(), runtime.PruneOptions{All: fl.all, Volumes: fl.volumes})
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&sb, "[tengiz] removed %d containers, %d images, %d networks, %d volumes\n",
		len(res.Containers), len(res.Images), len(res.Networks), len(res.Volumes))
	fmt.Fprintf(&sb, "[tengiz] total reclaimed space: %s\n", res.Reclaimed)
	return sb.String(), nil
}

func confirmCleanup(r io.Reader) bool {
	fmt.Print("This will remove unused Docker resources. Continue? [y/N]: ")
	reader := bufio.NewReader(r)
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cli/ -count=1`
Expected: PASS (all existing CLI tests plus the new cleanup tests).

- [ ] **Step 7: Build and vet**

Run: `go build -o tengiz . && go vet ./...`
Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command with label-protected docker prune"
```

---

### Task 5: Documentation updates

**Files:**
- Modify: `README.md` — add `### tengiz cleanup` under CLI Reference (after `### tengiz rm <app>`, before `### tengiz rollback <app>`)
- Modify: `AGENTS.md` — add `tengiz cleanup` to the Commands list
- Modify: `docs/FUTURES_FEATURES.md` — mark P0 #6 Docker Housekeeping as implemented

**Interfaces:**
- Consumes: the final CLI behavior from Task 4 (flags `--all`, `--volumes`, `--force`, `--dry-run`; confirmation prompt; `tengiz-app` label protection)

- [ ] **Step 1: Add the README section**

In `README.md`, insert between the `### tengiz rm <app>` block (ends line 228) and the `### tengiz rollback <app>` heading (line 230):

```markdown
### `tengiz cleanup`

Clean up unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `-a`, `--all` | Also remove all unused images, not just dangling ones |
| `--volumes` | Also remove unused volumes |
| `-f`, `--force` | Skip the confirmation prompt (CI-friendly) |
| `--dry-run` | Show reclaimable space per category without removing anything |

Removes stopped containers, dangling images, unused networks, and unused build cache. Containers and images managed by Tengiz (labeled `tengiz-app`) are always protected. Without `--force`, you are prompted to confirm before anything is deleted.

```
- [ ] **Step 2: Update AGENTS.md Commands list**

In `AGENTS.md`, add to the Commands block after the `tengiz stop/start/rm  → lifecycle` line:

```text
tengiz cleanup [-f] [-a] [--volumes] [--dry-run]  → prune unused Docker resources (Tengiz-labeled containers/images are protected)
```

- [ ] **Step 3: Mark the feature implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`:

1. In the P0 table, change row #6 from `⬜` to `✅` and append the completion note to the rationale:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. Implemented 2026-08-07. |
```

2. Add a row to the `### ✅ Implemented Features (Not Pending)` table (after the Webhook row, line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-07) |
```

- [ ] **Step 4: Verify no code changed and run the full suite**

Run: `git diff --stat` (should show only the three doc files) and `go test ./... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review Checklist

**Spec coverage** (against FUTURES_FEATURES.md P0 #6 + "Docker Housekeeping (Otomatik Temizlik)" section):
- Label-based `docker system prune` → Task 2 (`--filter label!=tengiz-app`)
- `tengiz cleanup` command → Task 4
- Protect Tengiz-managed containers via labels → Task 1 (shared constants) + Task 2 (filter)
- Protect Tengiz images (so rollback retention survives `--all`) → Task 3 (build-time image labels)
- Periodic/cleanup operations are covered by `docker system prune` semantics; no separate background job is added (out of scope for this P0 item, which specifies the command only)
- Docs (README, AGENTS.md, FUTURES_FEATURES.md) → Task 5

**Placeholder scan:** No TBD/TODO, no "add error handling"-style steps; every code step shows exact code; every test shows exact assertions; every command shows expected output.

**Type consistency:** `types.LabelApp`/`types.LabelEnv` (Task 1) used in Tasks 2-3; `runtime.PruneOptions{All, Volumes, DryRun}` and `runtime.PruneResult{Containers, Images, Networks, Volumes, BuildCache, Reclaimed, Reclaimable}` (Task 2) used in Task 4 with identical field names; `Manager.Prune(ctx, opts) (PruneResult, error)` consistent across stub (Task 2) and all three mocks; CLI helper names `runCleanup`, `confirmCleanup`, `cleanupFlags` used consistently between Step 5 (definition) and Step 1 (tests).
