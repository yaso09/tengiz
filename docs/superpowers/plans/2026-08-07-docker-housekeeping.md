# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add label-based Docker housekeeping to the runtime and a `tengiz cleanup` command so users can reclaim disk space by pruning stopped non-app containers, dangling images, unused networks, and build cache — while always preserving Tengiz-managed apps (scale-to-zero idle stops) and the last N images per app for rollback.

**Architecture:** The `runtime.Manager` interface gains a `Cleanup(ctx, opts)` method. Pure helper functions (`buildPruneCommand`, `buildCleanupCategories`, `parsePruneOutput`) map category flags to `docker <category> prune` invocations and parse their output, keeping the docker-exec implementation thin and unit-testable (following the existing `buildLogArgs`/`buildRunArgs` pattern in `internal/runtime/docker.go`). The CLI wires this to a new `tengiz cleanup` command that reuses the existing `runtime.Manager.KeepLastNImages` for per-app image retention, and defaults to report-only (dry run) unless `--force` is passed. Container pruning uses the `--filter label!=tengiz-app` filter so stopped Tengiz containers are never removed.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (no SDK), existing `runtime.Manager`, `config.Store`.

## Global Constraints

- Create a feature branch `feat/docker-housekeeping` before starting (AGENTS.md rule: `git checkout -b feat/<name>`)
- No new external dependencies (no additions to `go.mod`)
- Never prune running or stopped Tengiz-managed containers — those carry the `tengiz-app=<appname>` label and must be excluded via `--filter label!=tengiz-app`
- Never prune the last N images per app (rollback retention, default 5) — enforced by reusing `KeepLastNImages`
- Default category set (no flags): `containers + images + networks + build-cache`; `volumes` is always opt-in via `--volumes` or `--all`
- Without `--force`, the command must NOT delete anything — report-only dry run; `KeepLastNImages` must also be skipped in dry-run mode
- `docker image prune` removes dangling images only (no `-a` flag) — old per-app images are handled solely by `KeepLastNImages`
- Existing tests must continue to pass after adding the `Cleanup` method to the three mocks that implement `runtime.Manager`
- Repo rules (AGENTS.md): update README.md on UI/UX changes; run `go test ./... -v -count=1` and `go vet ./...` before committing
- Commit messages use conventional format matching the repo style (`feat: ...`)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` (modify) | Add `CleanupOptions`, `CleanupReport`, pure helpers `buildPruneCommand`/`buildCleanupCategories`/`parsePruneOutput`, and `dockerRuntime.Cleanup`. Existing `RemoveImage`/`KeepLastNImages` stay unchanged. |
| `internal/runtime/runtime.go` (modify) | Add `Cleanup` to the `Manager` interface (line ~36) and to `stubManager` (after line ~118). |
| `internal/cli/cleanup.go` (create) | New `cleanupCmd` (registered to `rootCmd` via its own `init()`), plus `selectCleanupCategories` and `printCleanupReport`. |
| `internal/cli/cleanup_test.go` (create) | Tests for command registration, flags, category selection, and report printing. |
| `internal/runtime/cleanup_test.go` (modify) | Tests for the pure helpers and the stub `Cleanup`. |
| `internal/cli/root_test.go` (modify) | Add `Cleanup` to `mockRTForDeploy` (after line 99). |
| `internal/idle/idle_test.go` (modify) | Add `Cleanup` to `mockRuntime` (after line 33). |
| `internal/proxy/proxy_test.go` (modify) | Add `Cleanup` to `mockRuntime` (after line 34). |
| `README.md` (modify) | Add `tengiz cleanup` to the Features list and CLI Reference. |
| `docs/FUTURES_FEATURES.md` (modify) | Mark feature #6 as ✅ and add to the Implemented Features table. |

No new third-party dependencies. No changes to `internal/types`.

---

### Task 1: Runtime cleanup pure helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` (append after the final `}` at line 59)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type CleanupOptions struct { Containers, Images, Networks, Volumes, BuildCache, DryRun bool }`
  - `type CleanupReport struct { ContainersPruned, ImagesPruned, NetworksPruned, VolumesPruned int; BuildCacheFreed string }`
  - `buildPruneCommand(category string, dryRun bool) []string` — returns docker args for `docker <category> prune`
  - `parsePruneOutput(out []byte) (int, string)` — returns (items pruned, reclaimed space string)
  - `buildCleanupCategories(opts CleanupOptions) []string` — ordered list of enabled categories

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildPruneCommandContainers(t *testing.T) {
	got := buildPruneCommand("containers", false)
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !equalStrings(got, want) {
		t.Errorf("buildPruneCommand(\"containers\", false) = %v, want %v", got, want)
	}
}

func TestBuildPruneCommandContainersDryRun(t *testing.T) {
	got := buildPruneCommand("containers", true)
	want := []string{"container", "prune", "--dry-run", "--filter", "label!=tengiz-app"}
	if !equalStrings(got, want) {
		t.Errorf("buildPruneCommand(\"containers\", true) = %v, want %v", got, want)
	}
}

func TestBuildPruneCommandImages(t *testing.T) {
	got := buildPruneCommand("images", false)
	want := []string{"image", "prune", "-f"}
	if !equalStrings(got, want) {
		t.Errorf("buildPruneCommand(\"images\", false) = %v, want %v", got, want)
	}
}

func TestBuildPruneCommandNetworksDryRun(t *testing.T) {
	got := buildPruneCommand("networks", true)
	want := []string{"network", "prune", "--dry-run"}
	if !equalStrings(got, want) {
		t.Errorf("buildPruneCommand(\"networks\", true) = %v, want %v", got, want)
	}
}

func TestBuildPruneCommandVolumes(t *testing.T) {
	got := buildPruneCommand("volumes", false)
	want := []string{"volume", "prune", "-f"}
	if !equalStrings(got, want) {
		t.Errorf("buildPruneCommand(\"volumes\", false) = %v, want %v", got, want)
	}
}

func TestBuildPruneCommandBuildCache(t *testing.T) {
	got := buildPruneCommand("build-cache", false)
	want := []string{"builder", "prune", "-f"}
	if !equalStrings(got, want) {
		t.Errorf("buildPruneCommand(\"build-cache\", false) = %v, want %v", got, want)
	}
}

func TestBuildCleanupCategories(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want []string
	}{
		{"empty", CleanupOptions{}, nil},
		{"containers only", CleanupOptions{Containers: true}, []string{"containers"}},
		{"images and build-cache", CleanupOptions{Images: true, BuildCache: true}, []string{"images", "build-cache"}},
		{"all categories", CleanupOptions{Containers: true, Images: true, Networks: true, Volumes: true, BuildCache: true}, []string{"containers", "images", "networks", "volumes", "build-cache"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCleanupCategories(tt.opts)
			if !equalStrings(got, tt.want) {
				t.Errorf("buildCleanupCategories() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParsePruneOutputImages(t *testing.T) {
	out := "Deleted Images:\nuntagged: tengiz-apps/foo:production-v1\ndeleted: sha256:abc\ndeleted: sha256:def\nTotal reclaimed space: 10.3MB\n"
	count, reclaimed := parsePruneOutput([]byte(out))
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	if reclaimed != "10.3MB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "10.3MB")
	}
}

func TestParsePruneOutputContainers(t *testing.T) {
	out := "abc123\ndef456\n"
	count, reclaimed := parsePruneOutput([]byte(out))
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if reclaimed != "" {
		t.Errorf("reclaimed = %q, want empty", reclaimed)
	}
}

func TestParsePruneOutputBuildCache(t *testing.T) {
	out := "Removed build cache: abc\nRemoved build cache: def\nTotal reclaimed space: 2.1GB\n"
	count, reclaimed := parsePruneOutput([]byte(out))
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if reclaimed != "2.1GB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "2.1GB")
	}
}

func TestParsePruneOutputIgnoresWarnings(t *testing.T) {
	out := "WARNING! This will remove all dangling images.\nabc\ndef\n"
	count, reclaimed := parsePruneOutput([]byte(out))
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if reclaimed != "" {
		t.Errorf("reclaimed = %q, want empty", reclaimed)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	count, reclaimed := parsePruneOutput([]byte(""))
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if reclaimed != "" {
		t.Errorf("reclaimed = %q, want empty", reclaimed)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestBuildPruneCommand|TestBuildCleanupCategories|TestParsePruneOutput" -v -count=1`

Expected: FAIL with `undefined: buildPruneCommand`, `undefined: buildCleanupCategories`, `undefined: parsePruneOutput`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go` (after line 59, the closing `}` of `KeepLastNImages`):

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	DryRun     bool
}

type CleanupReport struct {
	ContainersPruned int
	ImagesPruned     int
	NetworksPruned   int
	VolumesPruned    int
	BuildCacheFreed  string
}

// buildPruneCommand returns the args for a `docker <category> prune` invocation.
// Supported categories: containers, images, networks, volumes, build-cache.
// Container pruning excludes Tengiz-managed containers via the tengiz-app label.
func buildPruneCommand(category string, dryRun bool) []string {
	args := []string{category, "prune"}
	if dryRun {
		args = append(args, "--dry-run")
	} else {
		args = append(args, "-f")
	}
	if category == "containers" {
		args = append(args, "--filter", "label!=tengiz-app")
	}
	return args
}

// buildCleanupCategories returns the ordered list of prune categories enabled by opts.
func buildCleanupCategories(opts CleanupOptions) []string {
	var cats []string
	if opts.Containers {
		cats = append(cats, "containers")
	}
	if opts.Images {
		cats = append(cats, "images")
	}
	if opts.Networks {
		cats = append(cats, "networks")
	}
	if opts.Volumes {
		cats = append(cats, "volumes")
	}
	if opts.BuildCache {
		cats = append(cats, "build-cache")
	}
	return cats
}

// parsePruneOutput counts pruned items and extracts the reclaimed space string
// from a `docker ... prune` invocation's combined output.
func parsePruneOutput(out []byte) (int, string) {
	count := 0
	reclaimed := ""
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "WARNING") {
			continue
		}
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			reclaimed = strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
			continue
		}
		if trimmed == "Deleted Images:" {
			continue
		}
		count++
	}
	return count, reclaimed
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestBuildPruneCommand|TestBuildCleanupCategories|TestParsePruneOutput" -v -count=1`

Expected: PASS (all subtests green)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add docker prune command builders and output parser"
```

---

### Task 2: Wire Cleanup into runtime.Manager

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to the `Manager` interface (after the `KeepLastNImages` line at 36)
- Modify: `internal/runtime/runtime.go:113-119` — add `stubManager.Cleanup` (after the `KeepLastNImages` stub at line 118)
- Modify: `internal/runtime/cleanup.go` — append `dockerRuntime.Cleanup`
- Modify: `internal/cli/root_test.go` — add `Cleanup` to `mockRTForDeploy` (after line 99)
- Modify: `internal/idle/idle_test.go` — add `Cleanup` to `mockRuntime` (after line 33)
- Modify: `internal/proxy/proxy_test.go` — add `Cleanup` to `mockRuntime` (after line 34)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupReport`, `buildPruneCommand`, `buildCleanupCategories`, `parsePruneOutput` from Task 1
- Produces: `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)`, `dockerRuntime.Cleanup`, `stubManager.Cleanup`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, Images: true, DryRun: true})
	if err != nil {
		t.Fatalf("StubCleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("StubCleanup() returned nil report")
	}
	if report.ContainersPruned != 0 || report.ImagesPruned != 0 {
		t.Errorf("stub report should be zero-valued, got %+v", report)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: FAIL with `stubManager.Cleanup undefined (type Manager has no field or method Cleanup)` (or a compile error listing `Cleanup` as missing from the interface). Also `go build ./...` fails because the three mocks no longer satisfy `runtime.Manager`.

- [ ] **Step 3: Write minimal implementation**

**3a.** In `internal/runtime/runtime.go`, add to the `Manager` interface after the `KeepLastNImages` line:

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
```

**3b.** In `internal/runtime/runtime.go`, add after the `stubManager.KeepLastNImages` stub (line 118):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return &CleanupReport{}, nil
}
```

**3c.** Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{}
	for _, cat := range buildCleanupCategories(opts) {
		cmd := exec.CommandContext(ctx, "docker", buildPruneCommand(cat, opts.DryRun)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out))
		}
		count, reclaimed := parsePruneOutput(out)
		switch cat {
		case "containers":
			report.ContainersPruned = count
		case "images":
			report.ImagesPruned = count
		case "networks":
			report.NetworksPruned = count
		case "volumes":
			report.VolumesPruned = count
		case "build-cache":
			report.BuildCacheFreed = reclaimed
		}
	}
	return report, nil
}
```

**3d.** In `internal/cli/root_test.go`, add after the `mockRTForDeploy.KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{}, nil
}
```

**3e.** In `internal/idle/idle_test.go`, add after the `mockRuntime.KeepLastNImages` method (line 33):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{}, nil
}
```

**3f.** In `internal/proxy/proxy_test.go`, add after the `mockRuntime.KeepLastNImages` method (line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/idle/... ./internal/proxy/... -count=1`

Expected: PASS (including `TestStubSatisfiesInterface` and `TestMockRTForDeployImplementsManager`, which now verify the interface is fully satisfied)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Cleanup to runtime.Manager interface and docker implementation"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.Manager.Cleanup`, `runtime.Manager.KeepLastNImages`, `runtime.CleanupOptions`, `runtime.CleanupReport`, `config.NewStoreWithEnv`, `getEnv(cmd)` and `dataDir` (both already exist in package `cli`)
- Produces:
  - `cleanupCmd` (registered to `rootCmd` from its own `init()` — same pattern as `internal/cli/preview.go`)
  - `selectCleanupCategories(cmd *cobra.Command, dryRun bool) runtime.CleanupOptions`
  - `printCleanupReport(report *runtime.CleanupReport, dryRun bool)`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	expected := []string{"force", "keep", "all", "containers", "images", "networks", "volumes", "build-cache"}
	for _, name := range expected {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func newCleanupTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cleanup-test"}
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	return cmd
}

func TestSelectCleanupCategoriesDefault(t *testing.T) {
	opts := selectCleanupCategories(newCleanupTestCmd(), true)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Errorf("default set should include containers/images/networks/build-cache, got %+v", opts)
	}
	if opts.Volumes {
		t.Error("default set should NOT include volumes")
	}
	if !opts.DryRun {
		t.Error("dryRun should be true")
	}
}

func TestSelectCleanupCategoriesVolumesOnly(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.Flags().Set("volumes", "true")
	opts := selectCleanupCategories(cmd, false)
	if !opts.Volumes {
		t.Error("volumes should be enabled")
	}
	if opts.Containers || opts.Images || opts.Networks || opts.BuildCache {
		t.Errorf("only volumes should be enabled, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("dryRun should be false")
	}
}

func TestSelectCleanupCategoriesAll(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.Flags().Set("all", "true")
	opts := selectCleanupCategories(cmd, true)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.Volumes || !opts.BuildCache {
		t.Errorf("--all should enable every category, got %+v", opts)
	}
	if !opts.DryRun {
		t.Error("dryRun should be true")
	}
}

func TestSelectCleanupCategoriesMixed(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.Flags().Set("containers", "true")
	cmd.Flags().Set("build-cache", "true")
	opts := selectCleanupCategories(cmd, false)
	if !opts.Containers || !opts.BuildCache {
		t.Errorf("containers+build-cache should be enabled, got %+v", opts)
	}
	if opts.Images || opts.Networks || opts.Volumes {
		t.Errorf("only containers+build-cache should be enabled, got %+v", opts)
	}
}

func TestPrintCleanupReportDryRun(t *testing.T) {
	report := &runtime.CleanupReport{ContainersPruned: 3, ImagesPruned: 5, NetworksPruned: 1, VolumesPruned: 0, BuildCacheFreed: "10.3MB"}
	out := captureOutput(func() {
		printCleanupReport(report, true)
	})
	for _, want := range []string{"(dry run)", "3 would be removed", "5 would be removed", "10.3MB freed", "--force"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run report missing %q, got:\n%s", want, out)
		}
	}
}

func TestPrintCleanupReportForce(t *testing.T) {
	report := &runtime.CleanupReport{ContainersPruned: 2}
	out := captureOutput(func() {
		printCleanupReport(report, false)
	})
	if !strings.Contains(out, "2 removed") {
		t.Errorf("force report missing %q, got:\n%s", "2 removed", out)
	}
	if strings.Contains(out, "--force") {
		t.Errorf("force report should not suggest --force, got:\n%s", out)
	}
	if strings.Contains(out, "would be removed") {
		t.Errorf("force report should not say 'would be removed', got:\n%s", out)
	}
}
```

Note: `captureOutput` is defined in `internal/cli/root_test.go` in the same package and is reused here.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCommand|TestSelectCleanupCategories|TestPrintCleanupReport" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: selectCleanupCategories`, `undefined: printCleanupReport`

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"context"
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to free disk space",
	Long: `Removes stopped containers (excluding Tengiz-managed apps), dangling images,
unused networks, and Docker build cache. Keeps the last N images per app for rollback.

By default only reports what would be removed. Use --force to actually remove.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		keep, _ := cmd.Flags().GetInt("keep")
		if keep <= 0 {
			keep = 5
		}

		env := getEnv(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)
		apps, _ := store.ListApps()
		if force {
			for _, app := range apps {
				if err := rt.KeepLastNImages(context.Background(), app.Name, keep); err != nil {
					log.Printf("[tengiz] keep images for %s: %v", app.Name, err)
				}
			}
		} else if len(apps) > 0 {
			fmt.Printf("[tengiz] would keep the last %d images per app\n", keep)
		}

		opts := selectCleanupCategories(cmd, !force)
		report, err := rt.Cleanup(context.Background(), opts)
		if err != nil {
			return err
		}
		printCleanupReport(report, !force)
		return nil
	},
}

// selectCleanupCategories maps CLI flags to runtime.CleanupOptions.
// When no category flag is given, the safe default set (all except volumes) is used.
func selectCleanupCategories(cmd *cobra.Command, dryRun bool) runtime.CleanupOptions {
	all, _ := cmd.Flags().GetBool("all")
	if all {
		return runtime.CleanupOptions{
			Containers: true,
			Images:     true,
			Networks:   true,
			Volumes:    true,
			BuildCache: true,
			DryRun:     dryRun,
		}
	}
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	buildCache, _ := cmd.Flags().GetBool("build-cache")

	if !containers && !images && !networks && !volumes && !buildCache {
		return runtime.CleanupOptions{
			Containers: true,
			Images:     true,
			Networks:   true,
			BuildCache: true,
			DryRun:     dryRun,
		}
	}
	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		BuildCache: buildCache,
		DryRun:     dryRun,
	}
}

func printCleanupReport(report *runtime.CleanupReport, dryRun bool) {
	verb := "removed"
	suffix := ""
	hint := ""
	if dryRun {
		verb = "would be removed"
		suffix = " (dry run)"
		hint = "\nRun with --force to actually remove these resources."
	}
	fmt.Printf("[tengiz] cleanup summary%s:\n", suffix)
	fmt.Printf("  containers: %d %s\n", report.ContainersPruned, verb)
	fmt.Printf("  images:     %d %s\n", report.ImagesPruned, verb)
	fmt.Printf("  networks:   %d %s\n", report.NetworksPruned, verb)
	fmt.Printf("  volumes:    %d %s\n", report.VolumesPruned, verb)
	if report.BuildCacheFreed != "" {
		fmt.Printf("  build cache: %s freed\n", report.BuildCacheFreed)
	}
	fmt.Print(hint)
}

func init() {
	cleanupCmd.Flags().BoolP("force", "f", false, "actually remove resources (default: dry-run report only)")
	cleanupCmd.Flags().Int("keep", 5, "number of recent images to keep per app for rollback")
	cleanupCmd.Flags().Bool("all", false, "clean all categories including volumes (dangerous)")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (dangerous)")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCommand|TestSelectCleanupCategories|TestPrintCleanupReport" -v -count=1`

Expected: PASS (all subtests green)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Documentation updates

**Files:**
- Modify: `README.md` — add a Features bullet and a `tengiz cleanup` section to the CLI Reference
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented

- [ ] **Step 1: Add a Features bullet to README.md**

In `README.md`, after the "Self-contained" bullet (line 23), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes stopped non-app containers, dangling images, unused networks, and build cache while always preserving Tengiz-managed apps and rollback images.
```

- [ ] **Step 2: Add the `tengiz cleanup` CLI Reference section**

In `README.md`, after the `tengiz rollback` section (line 236) and before `### tengiz domain` (line 238), insert:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to free disk space. By default it reports what would be removed without deleting anything — add `--force` to actually clean up. Tengiz-managed containers (scale-to-zero idle stops) and the last N images per app are always preserved.

| Flag | Description |
|------|-------------|
| `--force`, `-f` | Actually remove resources (default: dry-run report only) |
| `--keep N` | Images to keep per app for rollback (default: 5) |
| `--all` | Clean every category, including volumes (dangerous) |
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling images |
| `--networks` | Prune unused networks |
| `--volumes` | Prune unused volumes |
| `--build-cache` | Prune Docker build cache |

Examples:

```
tengiz cleanup
tengiz cleanup --force
tengiz cleanup --force --containers --images
tengiz cleanup --force --all
```
```

- [ ] **Step 3: Mark feature #6 as implemented in docs/FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`, the feature #6 row (line 19) currently reads:

```
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Change its status marker from `⬜` to `✅` (keep everything else on the line identical, including the rationale column text).

Then add a row to the "✅ Implemented Features (Not Pending)" table after the webhook row (line 253):

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-07) |
```

- [ ] **Step 4: Verify the full build and test suite**

Run:

```bash
go build -o tengiz .
go vet ./...
go test ./... -v -count=1
```

Expected: build succeeds, `go vet` clean, all tests pass.

Also verify the new command is wired in:

```bash
./tengiz cleanup --help
```

Expected: help text listing all cleanup flags (`--force`, `--keep`, `--all`, `--containers`, `--images`, `--networks`, `--volumes`, `--build-cache`).

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark docker housekeeping implemented"
```

---

## Verification Summary

After all tasks, run from the repo root:

```bash
go build -o tengiz .
go vet ./...
go test ./... -v -count=1
```

Manual smoke test (requires Docker): deploy any app, then:

```bash
./tengiz cleanup                 # prints a dry-run summary, deletes nothing
./tengiz cleanup --force         # prunes containers/images/networks/build-cache, keeps last 5 images/app
./tengiz cleanup --force --all   # additionally prunes unused volumes
```

Confirm that `tengiz ps` still lists all apps and that a stopped (scale-to-zero) app's container survives `./tengiz cleanup --force`.
