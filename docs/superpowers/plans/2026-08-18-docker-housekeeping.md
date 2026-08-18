# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, networks, and build cache to reclaim disk space, with label-based protection so Tengiz-managed resources are never removed.

**Architecture:** Extend the `runtime.Manager` interface with two exec-based methods — `DiskUsage` (runs `docker system df`) and `Prune` (runs per-category `docker <type> prune` commands built by a pure `buildPruneArgs` helper). The `tengiz cleanup` CLI command defaults to a dry-run report and requires `--force` to act. Protection comes from the `label!=tengiz-app` Docker filter: all Tengiz containers already carry the `tengiz-app` label, and the builder is updated to also label every built image with `tengiz-app=<app>` so image pruning skips them. Volumes are only pruned when explicitly requested with `--volumes`.

**Tech Stack:** Go 1.26, Cobra (CLI), stdlib `os/exec` (Docker CLI), existing `runtime.Manager` / `builder.Builder` / `internal/cli` packages. No new external dependencies.

## Global Constraints

- No new external Go dependencies — stdlib + existing `cobra` only
- All destructive pruning is gated behind `--force`; the default invocation is a dry-run report only
- Every prune command that targets containers, images, volumes, or networks MUST include the `--filter label!=tengiz-app` argument (Tengiz containers and images carry the `tengiz-app` label and must never be pruned)
- Build-cache pruning (`docker builder prune`) does NOT use the label filter (build cache has no labels)
- Default target set with no category flags: `containers`, `images`, `networks`, `build-cache` — volumes excluded
- `--volumes` ADDS volumes to whatever target set is in effect; the other category flags (`--containers`, `--images`, `--networks`, `--build-cache`) are exact selectors that override the default set
- `--since <duration>` is passed through to Docker as `--filter until=<duration>` on every selected target
- Adding methods to `runtime.Manager` requires updating THREE types in the same change: `dockerRuntime` (impl), `stubManager` (runtime.go), and `mockRTForDeploy` (internal/cli/root_test.go) — otherwise the package does not compile
- The `docker system df` output is printed verbatim; no parsing is required
- Per project rules (AGENTS.md): create a `feat/docker-housekeeping` branch before starting, run `go test ./... -v -count=1`, `go vet ./...`, and `go build -o tengiz .` before committing each task
- Nixpacks-built images do not receive the `tengiz-app` label (nixpacks CLI cannot set image labels); when `--all` is used they may be pruned if unused — documented in README

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` (modify) | `PruneTarget`, `PruneOptions`, pure `buildPruneArgs` helper, `dockerRuntime.Prune`, `dockerRuntime.DiskUsage` |
| `internal/runtime/runtime.go` (modify) | Add `DiskUsage` + `Prune` to `Manager` interface; stub implementations on `stubManager` |
| `internal/runtime/cleanup_test.go` (modify) | Tests for `buildPruneArgs` and stub `Prune`/`DiskUsage` |
| `internal/cli/root_test.go` (modify) | Add `DiskUsage` + `Prune` methods to `mockRTForDeploy` (keeps the package compiling) |
| `internal/builder/builder.go` (modify) | `buildImageArgs` helper that adds `--label tengiz-app=<app>`; `buildWithDockerfile` uses it |
| `internal/builder/builder_test.go` (modify) | Tests for `buildImageArgs` label injection |
| `internal/cli/cleanup.go` (create) | `cleanupCmd`, `cleanupTargets`, `targetStrings`, `cleanupRunner` interface + `newDockerRuntime` seam, `init()` registration |
| `internal/cli/cleanup_test.go` (create) | Command registration/flags tests, `cleanupTargets` table tests, dry-run and `--force` behavior tests via mock |
| `README.md` (modify) | Feature bullet + `tengiz cleanup` CLI reference section |

---

### Task 1: Runtime prune primitives (`runtime` package)

**Files:**
- Modify: `internal/runtime/cleanup.go` (append new types + methods)
- Modify: `internal/runtime/runtime.go:31-49` — add methods to `Manager` interface; add stub methods on `stubManager` (after line 119)
- Modify: `internal/cli/root_test.go:98-100` — add `DiskUsage` + `Prune` to `mockRTForDeploy`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type PruneTarget string` with constants `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache`
  - `type PruneOptions struct { Targets []PruneTarget; AllImages bool; Since string }`
  - `buildPruneArgs(opts PruneOptions) [][]string` — pure function, one inner slice per `docker` command
  - `Manager.DiskUsage(ctx context.Context) (string, error)`
  - `Manager.Prune(ctx context.Context, opts PruneOptions) (string, error)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildPruneArgsEmptyTargets(t *testing.T) {
	got := buildPruneArgs(PruneOptions{})
	if len(got) != 0 {
		t.Fatalf("expected no commands for empty targets, got %v", got)
	}
}

func TestBuildPruneArgsContainers(t *testing.T) {
	got := buildPruneArgs(PruneOptions{Targets: []PruneTarget{PruneContainers}})
	want := [][]string{{"container", "prune", "-f", "--filter", "label!=tengiz-app"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildPruneArgsImages(t *testing.T) {
	got := buildPruneArgs(PruneOptions{Targets: []PruneTarget{PruneImages}})
	want := [][]string{{"image", "prune", "-f", "--filter", "label!=tengiz-app"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildPruneArgsImagesAll(t *testing.T) {
	got := buildPruneArgs(PruneOptions{Targets: []PruneTarget{PruneImages}, AllImages: true})
	want := [][]string{{"image", "prune", "-f", "-a", "--filter", "label!=tengiz-app"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildPruneArgsVolumes(t *testing.T) {
	got := buildPruneArgs(PruneOptions{Targets: []PruneTarget{PruneVolumes}})
	want := [][]string{{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildPruneArgsNetworks(t *testing.T) {
	got := buildPruneArgs(PruneOptions{Targets: []PruneTarget{PruneNetworks}})
	want := [][]string{{"network", "prune", "-f", "--filter", "label!=tengiz-app"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildPruneArgsBuildCache(t *testing.T) {
	got := buildPruneArgs(PruneOptions{Targets: []PruneTarget{PruneBuildCache}})
	want := [][]string{{"builder", "prune", "-f"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildPruneArgsSince(t *testing.T) {
	got := buildPruneArgs(PruneOptions{Targets: []PruneTarget{PruneContainers, PruneImages}, Since: "24h"})
	want := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app", "--filter", "until=24h"},
		{"image", "prune", "-f", "--filter", "label!=tengiz-app", "--filter", "until=24h"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildPruneArgsDefaultTargets(t *testing.T) {
	got := buildPruneArgs(PruneOptions{
		Targets: []PruneTarget{PruneContainers, PruneImages, PruneNetworks, PruneBuildCache},
	})
	want := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"builder", "prune", "-f"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestStubPruneAndDiskUsage(t *testing.T) {
	m := NewStub()
	if _, err := m.DiskUsage(context.Background()); err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if _, err := m.Prune(context.Background(), PruneOptions{}); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestBuildPruneArgs|TestStubPruneAndDiskUsage' -v -count=1`

Expected: FAIL — `buildPruneArgs` undefined, and `stubManager` no longer satisfies `Manager` (compile errors: `cannot use m (type *stubManager) as type Manager in assignment`).

- [ ] **Step 3: Implement `buildPruneArgs`, `Prune`, `DiskUsage` in cleanup.go**

Append to `internal/runtime/cleanup.go` (imports `context`, `fmt`, `os/exec`, `strings` are already present):

```go
const pruneLabelFilter = "label!=tengiz-app"

type PruneTarget string

const (
	PruneContainers PruneTarget = "containers"
	PruneImages     PruneTarget = "images"
	PruneVolumes    PruneTarget = "volumes"
	PruneNetworks   PruneTarget = "networks"
	PruneBuildCache PruneTarget = "build-cache"
)

type PruneOptions struct {
	Targets   []PruneTarget
	AllImages bool
	Since     string
}

func buildPruneArgs(opts PruneOptions) [][]string {
	var cmds [][]string
	for _, target := range opts.Targets {
		switch target {
		case PruneContainers:
			args := []string{"container", "prune", "-f", "--filter", pruneLabelFilter}
			args = appendUntil(args, opts.Since)
			cmds = append(cmds, args)
		case PruneImages:
			args := []string{"image", "prune", "-f"}
			if opts.AllImages {
				args = append(args, "-a")
			}
			args = append(args, "--filter", pruneLabelFilter)
			args = appendUntil(args, opts.Since)
			cmds = append(cmds, args)
		case PruneVolumes:
			args := []string{"volume", "prune", "-f", "--filter", pruneLabelFilter}
			args = appendUntil(args, opts.Since)
			cmds = append(cmds, args)
		case PruneNetworks:
			args := []string{"network", "prune", "-f", "--filter", pruneLabelFilter}
			args = appendUntil(args, opts.Since)
			cmds = append(cmds, args)
		case PruneBuildCache:
			args := []string{"builder", "prune", "-f"}
			args = appendUntil(args, opts.Since)
			cmds = append(cmds, args)
		}
	}
	return cmds
}

func appendUntil(args []string, since string) []string {
	if since != "" {
		args = append(args, "--filter", "until="+since)
	}
	return args
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (string, error) {
	cmds := buildPruneArgs(opts)
	var sb strings.Builder
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		sb.Write(out)
		if err != nil {
			return sb.String(), fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
	}
	return sb.String(), nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
```

- [ ] **Step 4: Add the two methods to the `Manager` interface**

In `internal/runtime/runtime.go`, after the `KeepLastNImages` line (line 36), add:

```go
	DiskUsage(ctx context.Context) (string, error)
	Prune(ctx context.Context, opts PruneOptions) (string, error)
```

Then add stub methods after the existing `KeepLastNImages` stub (after line 119):

```go
func (m *stubManager) DiskUsage(ctx context.Context) (string, error) {
	return "", nil
}

func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (string, error) {
	return "", nil
}
```

- [ ] **Step 5: Add the two methods to `mockRTForDeploy` in the CLI test file**

In `internal/cli/root_test.go`, after the `KeepLastNImages` method (line 99), add:

```go
func (m *mockRTForDeploy) DiskUsage(ctx context.Context) (string, error) { return "", nil }
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (string, error) { return "", nil }
```

- [ ] **Step 6: Run all tests to verify they pass**

Run: `go test ./... -v -count=1`
Expected: ALL PASS.

Run: `go vet ./...`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add DiskUsage and Prune to runtime Manager"
```

---

### Task 2: Label Tengiz images at build time (`builder` package)

**Files:**
- Modify: `internal/builder/builder.go:69-73` — build args are currently built inline in `buildWithDockerfile`; replace with the new helper
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `buildImageArgs(tag, appName, dir string, secretArgs []string) []string` — pure function returning `docker build` args that include `--label tengiz-app=<app>`

- [ ] **Step 1: Write the failing test**

Append to `internal/builder/builder_test.go` (imports `strings` and `testing` are already present):

```go
func TestBuildImageArgsIncludesAppLabel(t *testing.T) {
	args := buildImageArgs("tengiz-apps/myapp:production-123", "myapp", "/tmp/app", nil)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--label", "tengiz-app=myapp", "-t", "tengiz-apps/myapp:production-123", "/tmp/app"} {
		if !strings.Contains(joined, want) {
			t.Errorf("buildImageArgs() missing %q in %q", want, joined)
		}
	}
}

func TestBuildImageArgsWithSecrets(t *testing.T) {
	secretArgs := []string{"--secret", "id=TOKEN,src=/tmp/TOKEN"}
	args := buildImageArgs("tengiz-apps/myapp:production-123", "myapp", "/tmp/app", secretArgs)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--secret id=TOKEN,src=/tmp/TOKEN --label tengiz-app=myapp") {
		t.Errorf("buildImageArgs() secret+label ordering wrong: %q", joined)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/builder/ -run TestBuildImageArgs -v -count=1`
Expected: FAIL — `buildImageArgs` undefined.

- [ ] **Step 3: Add the helper and use it in `buildWithDockerfile`**

In `internal/builder/builder.go`, add after the `buildSecretArgs` method (line 99):

```go
func buildImageArgs(tag, appName, dir string, secretArgs []string) []string {
	args := []string{"build"}
	args = append(args, secretArgs...)
	args = append(args, "--label", fmt.Sprintf("tengiz-app=%s", appName))
	args = append(args, "-t", tag, dir)
	return args
}
```

Replace the inline arg construction in `buildWithDockerfile` (lines 69-71):

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

with:

```go
	args := buildImageArgs(tag, appName, dir, b.buildSecretArgs())
```

(`fmt` is already imported in builder.go.)

- [ ] **Step 4: Run all tests to verify they pass**

Run: `go test ./... -v -count=1`
Expected: ALL PASS.

Run: `go vet ./...`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat(builder): label built images with tengiz-app for cleanup protection"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager.DiskUsage(ctx) (string, error)`, `runtime.Manager.Prune(ctx, opts runtime.PruneOptions) (string, error)`, `runtime.PruneOptions{Targets []runtime.PruneTarget; AllImages bool; Since string}`, `runtime.PruneTarget` constants
- Produces:
  - `cleanupTargets(fs *pflag.FlagSet) []runtime.PruneTarget` — resolves flags to target set
  - `targetStrings(targets []runtime.PruneTarget) []string` — human-readable names for the dry-run message
  - `var newDockerRuntime = func() (cleanupRunner, error) { return runtime.NewDocker() }` — test seam
  - `type cleanupRunner interface { DiskUsage(ctx context.Context) (string, error); Prune(ctx context.Context, opts runtime.PruneOptions) (string, error) }`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/yaso09/tengiz/internal/runtime"
)

func newCleanupFlagSet() *pflag.FlagSet {
	fs := pflag.NewFlagSet("cleanup-test", pflag.ContinueOnError)
	for _, name := range []string{"force", "all", "volumes", "containers", "images", "networks", "build-cache"} {
		fs.Bool(name, false, "")
	}
	fs.String("since", "", "")
	return fs
}

func TestCleanupTargetsDefault(t *testing.T) {
	got := cleanupTargets(newCleanupFlagSet())
	want := []runtime.PruneTarget{runtime.PruneContainers, runtime.PruneImages, runtime.PruneNetworks, runtime.PruneBuildCache}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanupTargets() = %v, want %v", got, want)
	}
}

func TestCleanupTargetsSelectorOverridesDefault(t *testing.T) {
	fs := newCleanupFlagSet()
	fs.Set("containers", "true")
	got := cleanupTargets(fs)
	want := []runtime.PruneTarget{runtime.PruneContainers}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanupTargets() = %v, want %v", got, want)
	}
}

func TestCleanupTargetsVolumesAddsToDefault(t *testing.T) {
	fs := newCleanupFlagSet()
	fs.Set("volumes", "true")
	got := cleanupTargets(fs)
	want := []runtime.PruneTarget{runtime.PruneContainers, runtime.PruneImages, runtime.PruneNetworks, runtime.PruneBuildCache, runtime.PruneVolumes}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanupTargets() = %v, want %v", got, want)
	}
}

func TestCleanupTargetsSelectorPlusVolumes(t *testing.T) {
	fs := newCleanupFlagSet()
	fs.Set("images", "true")
	fs.Set("volumes", "true")
	got := cleanupTargets(fs)
	want := []runtime.PruneTarget{runtime.PruneImages, runtime.PruneVolumes}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanupTargets() = %v, want %v", got, want)
	}
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil || cmd == nil {
		t.Fatalf("cleanup command not registered: err=%v", err)
	}
	for _, flag := range []string{"force", "all", "volumes", "containers", "images", "networks", "build-cache", "since"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

type mockCleanupRT struct {
	prunedWith []runtime.PruneOptions
	diskCalls  int
}

func (m *mockCleanupRT) DiskUsage(ctx context.Context) (string, error) {
	m.diskCalls++
	return "TYPE\tTOTAL\tRECLAIMABLE\nImages\t3\t2\n", nil
}

func (m *mockCleanupRT) Prune(ctx context.Context, opts runtime.PruneOptions) (string, error) {
	m.prunedWith = append(m.prunedWith, opts)
	return "Total reclaimed space: 250MB\n", nil
}

func TestCleanupDryRunDoesNotPrune(t *testing.T) {
	mock := &mockCleanupRT{}
	orig := newDockerRuntime
	newDockerRuntime = func() (cleanupRunner, error) { return mock, nil }
	defer func() { newDockerRuntime = orig }()

	rootCmd.SetArgs([]string{"cleanup"})
	output := captureOutput(func() { rootCmd.Execute() })

	if len(mock.prunedWith) != 0 {
		t.Fatalf("dry run called Prune %d times, want 0", len(mock.prunedWith))
	}
	if mock.diskCalls != 1 {
		t.Errorf("DiskUsage called %d times, want 1", mock.diskCalls)
	}
	if !strings.Contains(output, "dry run") {
		t.Errorf("output missing dry-run notice: %q", output)
	}
}

func TestCleanupForcePrunesDefaultTargets(t *testing.T) {
	mock := &mockCleanupRT{}
	orig := newDockerRuntime
	newDockerRuntime = func() (cleanupRunner, error) { return mock, nil }
	defer func() { newDockerRuntime = orig }()

	rootCmd.SetArgs([]string{"cleanup", "--force"})
	output := captureOutput(func() { rootCmd.Execute() })

	if len(mock.prunedWith) != 1 {
		t.Fatalf("Prune called %d times, want 1", len(mock.prunedWith))
	}
	opts := mock.prunedWith[0]
	want := []runtime.PruneTarget{runtime.PruneContainers, runtime.PruneImages, runtime.PruneNetworks, runtime.PruneBuildCache}
	if !reflect.DeepEqual(opts.Targets, want) {
		t.Errorf("Prune targets = %v, want %v", opts.Targets, want)
	}
	if !strings.Contains(output, "reclaimed") {
		t.Errorf("output missing prune result: %q", output)
	}
	if mock.diskCalls != 2 {
		t.Errorf("DiskUsage called %d times, want 2 (before+after)", mock.diskCalls)
	}
}

func TestCleanupForceAllSinceVolumes(t *testing.T) {
	mock := &mockCleanupRT{}
	orig := newDockerRuntime
	newDockerRuntime = func() (cleanupRunner, error) { return mock, nil }
	defer func() { newDockerRuntime = orig }()

	rootCmd.SetArgs([]string{"cleanup", "--force", "-a", "--since", "24h", "--volumes"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(mock.prunedWith) != 1 {
		t.Fatalf("Prune called %d times, want 1", len(mock.prunedWith))
	}
	opts := mock.prunedWith[0]
	if !opts.AllImages {
		t.Error("AllImages = false, want true")
	}
	if opts.Since != "24h" {
		t.Errorf("Since = %q, want %q", opts.Since, "24h")
	}
	found := false
	for _, tr := range opts.Targets {
		if tr == runtime.PruneVolumes {
			found = true
		}
	}
	if !found {
		t.Errorf("volumes not in targets: %v", opts.Targets)
	}
}
```

Note: `mockCleanupRT` satisfies the narrow `cleanupRunner` interface (2 methods), so it does NOT need to implement the full `runtime.Manager` interface.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`
Expected: FAIL — `cleanup.go` does not exist yet (undefined `cleanupTargets`, `newDockerRuntime`, `cleanupCmd`).

- [ ] **Step 3: Implement `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/yaso09/tengiz/internal/runtime"
)

type cleanupRunner interface {
	DiskUsage(ctx context.Context) (string, error)
	Prune(ctx context.Context, opts runtime.PruneOptions) (string, error)
}

var newDockerRuntime = func() (cleanupRunner, error) {
	return runtime.NewDocker()
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, networks, build cache)",
	Long: `Prune unused Docker resources to reclaim disk space.

Tengiz-managed containers and images carry the "tengiz-app" label and are
always protected from pruning. By default only containers, images, networks,
and build cache are pruned; volumes require --volumes.

Without --force this prints a dry-run report (docker system df) and prunes
nothing.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		all, _ := cmd.Flags().GetBool("all")
		since, _ := cmd.Flags().GetString("since")
		targets := cleanupTargets(cmd.Flags())

		rt, err := newDockerRuntime()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if !force {
			usage, err := rt.DiskUsage(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Print(usage)
			fmt.Printf("\n[tengiz] dry run: nothing pruned. Run 'tengiz cleanup --force' to prune: %s.\n",
				strings.Join(targetStrings(targets), ", "))
			return nil
		}

		before, err := rt.DiskUsage(cmd.Context())
		if err != nil {
			return err
		}
		fmt.Println("[tengiz] disk usage before:")
		fmt.Print(before)

		result, err := rt.Prune(cmd.Context(), runtime.PruneOptions{
			Targets:   targets,
			AllImages: all,
			Since:     since,
		})
		if err != nil {
			return err
		}
		fmt.Println("[tengiz] pruned:")
		fmt.Print(result)

		after, err := rt.DiskUsage(cmd.Context())
		if err != nil {
			return err
		}
		fmt.Println("[tengiz] disk usage after:")
		fmt.Print(after)
		return nil
	},
}

func cleanupTargets(fs *pflag.FlagSet) []runtime.PruneTarget {
	selectors := []struct {
		flag   string
		target runtime.PruneTarget
	}{
		{"containers", runtime.PruneContainers},
		{"images", runtime.PruneImages},
		{"networks", runtime.PruneNetworks},
		{"build-cache", runtime.PruneBuildCache},
	}
	anySet := false
	for _, s := range selectors {
		if v, _ := fs.GetBool(s.flag); v {
			anySet = true
			break
		}
	}
	var targets []runtime.PruneTarget
	if anySet {
		for _, s := range selectors {
			if v, _ := fs.GetBool(s.flag); v {
				targets = append(targets, s.target)
			}
		}
	} else {
		for _, s := range selectors {
			targets = append(targets, s.target)
		}
	}
	if v, _ := fs.GetBool("volumes"); v {
		targets = append(targets, runtime.PruneVolumes)
	}
	return targets
}

func targetStrings(targets []runtime.PruneTarget) []string {
	out := make([]string, len(targets))
	for i, t := range targets {
		out[i] = string(t)
	}
	return out
}

func init() {
	cleanupCmd.Flags().Bool("force", false, "actually prune (default: dry-run report only)")
	cleanupCmd.Flags().BoolP("all", "a", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused volumes")
	cleanupCmd.Flags().Bool("containers", false, "prune only stopped containers (overrides default target set)")
	cleanupCmd.Flags().Bool("images", false, "prune only unused images (overrides default target set)")
	cleanupCmd.Flags().Bool("networks", false, "prune only unused networks (overrides default target set)")
	cleanupCmd.Flags().Bool("build-cache", false, "prune only build cache (overrides default target set)")
	cleanupCmd.Flags().String("since", "", "only prune resources older than this duration (e.g. 24h, 7d)")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run all tests to verify they pass**

Run: `go test ./... -v -count=1`
Expected: ALL PASS.

Run: `go vet ./...`
Expected: no output.

Run: `go build -o tengiz .`
Expected: binary builds successfully.

- [ ] **Step 5: Manual smoke test (requires a running Docker daemon)**

```bash
./tengiz cleanup
```

Expected: prints `docker system df` output followed by `[tengiz] dry run: nothing pruned. Run 'tengiz cleanup --force' to prune: containers, images, networks, build-cache.`

```bash
./tengiz cleanup --force
```

Expected: prints disk usage before, per-category prune output, then disk usage after. Tengiz-managed containers (running or stopped) and labeled Tengiz images remain.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command with label-based pruning"
```

---

### Task 4: Documentation update

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: the `tengiz cleanup` command from Task 3
- Produces: nothing consumed by code

- [ ] **Step 1: Add the feature bullet to the Features list**

In `README.md`, insert between the `Health check configuration` bullet and the `No daemon required` bullet (lines 22-23):

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, images, networks, and build cache, with label-based protection so Tengiz-managed apps are never removed.
```

- [ ] **Step 2: Add the CLI reference section**

In `README.md`, insert the following section immediately before the `### `tengiz domain`` heading (after the rollback section):

```markdown
### `tengiz cleanup`

Prune unused Docker resources (containers, images, networks, build cache) to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--force` | Actually prune. Without it, prints a dry-run report only |
| `-a`, `--all` | Remove all unused images, not just dangling ones |
| `--volumes` | Also prune unused volumes |
| `--containers` | Prune only stopped containers (overrides default target set) |
| `--images` | Prune only unused images (overrides default target set) |
| `--networks` | Prune only unused networks (overrides default target set) |
| `--build-cache` | Prune only build cache (overrides default target set) |
| `--since <duration>` | Only prune resources older than this duration (e.g. `24h`, `7d`) |

Default target set (no flags): containers, images, networks, and build cache. Tengiz-managed containers and images carry the `tengiz-app` label and are always protected from pruning. Volumes are excluded unless `--volumes` is given. Run without `--force` to see what is reclaimable first:
```

- [ ] **Step 3: Verify the build and tests still pass**

Run: `go build -o tengiz . && go test ./... -v -count=1 && go vet ./...`
Expected: build succeeds, ALL tests pass, vet clean.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage.** The FUTURES_FEATURES.md entry (#6, P0 "Docker Housekeeping") requires: disk-space cleanup for single-server deploys, label-based `docker system prune` behavior, protection of Tengiz-managed containers, and a `tengiz cleanup` command. All are covered: label protection via `--filter label!=tengiz-app` (Task 1) applied to containers/images/volumes/networks, image labeling so Tengiz images are protected under `--all` (Task 2), the `tengiz cleanup` command with dry-run default + `--force` and granular category flags (Task 3), and documentation (Task 4). Feature #56 "Granular Docker Prune Operations" is intentionally NOT in scope — its per-category flags overlap, but this plan only adds the granularity needed for the base housekeeping feature.

**2. Placeholder scan.** No TBD/TODO/"handle edge cases" placeholders. Every code step contains complete, runnable code; every test step contains full test functions; every commit step contains the exact command.

**3. Type consistency.** Type names are consistent across tasks: `PruneTarget` / `PruneOptions{Targets, AllImages, Since}` / `buildPruneArgs(opts PruneOptions) [][]string` / `Manager.DiskUsage` / `Manager.Prune` are defined in Task 1 and referenced with identical signatures in Task 3. `cleanupRunner` (narrow interface in Task 3) is a subset of `Manager`, satisfied by both `dockerRuntime` and `mockCleanupRT`. `cleanupTargets(fs *pflag.FlagSet) []runtime.PruneTarget` and `targetStrings` are defined and used only in Task 3. `buildImageArgs(tag, appName, dir, secretArgs) []string` is defined and used only in Task 2. The stub manager, `mockRTForDeploy`, and `mockCleanupRT` all implement the methods they claim to, and `mockCleanupRT` deliberately implements only the 2-method `cleanupRunner` (not the full `Manager`).