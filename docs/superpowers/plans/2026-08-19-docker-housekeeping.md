# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning unused Docker resources (stopped containers, dangling images, old app images, unused volumes, build cache) using label-based filtering that protects Tengiz-managed workloads.

**Architecture:** A new `Cleanup(ctx, opts)` method on the existing `runtime.Manager` interface shells out to `docker` subcommands (`container prune`, `image prune`, `volume prune`, `builder prune`) with a `tengiz-app` label filter so only Tengiz-managed containers are touched. Pure helper functions build the docker argv (mirroring the existing `buildLogArgs`/`buildRunArgs` pattern) so they are unit-testable without a Docker daemon. The CLI `cleanup` command resolves flags into `types.CleanupOptions`, calls the runtime, then performs per-app image retention (keep last N) using the app list from `config.Store`. Dry-run mode lists candidates without executing destructive prunes.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface and `config.Store`, Docker CLI via `os/exec`. No new external dependencies.

## Global Constraints

- Containers are only ever pruned with `--filter label=tengiz-app` — never touch unlabeled containers
- Default keep count for per-app images is `5` (matches existing `KeepLastNImages` calls in `deploy`/`rollback`)
- `--volumes` permanently deletes volume data and therefore requires `--yes` confirmation; `--all` never implies `--volumes`
- `--dry-run` must never execute any destructive `docker prune` command
- `--all` means containers + images + build cache only (volumes stay explicit)
- Command follows the multi-environment pattern: `getEnv(cmd)` + `config.NewStoreWithEnv(dataDir, env)`
- No new external Go dependencies
- Existing tests must continue to pass without modification (adding a method to the `runtime.Manager` interface is the only exception — the two concrete implementations `stubManager` and `mockRTForDeploy` must gain the method in the same task)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | Add `CleanupOptions` and `CleanupReport` types |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager` implementation |
| `internal/runtime/docker.go` | Pure docker-arg helpers + `dockerRuntime.Cleanup` implementation |
| `internal/runtime/cleanup_test.go` | Tests for stub `Cleanup`, arg helpers, output parsers |
| `internal/cli/root.go` | `cleanupCmd`, `initCleanupFlags`, `resolveCleanupFlags`, `printCleanupReport`, wiring in `init()` |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy` |
| `internal/cli/cleanup_test.go` | New test file: registration, flags, flag resolution, report printing |
| `README.md` | Document the `tengiz cleanup` command |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as Implemented |

No new runtime packages created. All changes touch existing packages.

---

### Task 1: Cleanup types + Manager interface method

**Files:**
- Modify: `internal/types/types.go` — append `CleanupOptions` and `CleanupReport` after `AppEntry` (line 186)
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface; add stub method after `KeepLastNImages` (line 117-119)
- Modify: `internal/cli/root_test.go:69-107` — add `Cleanup` method to `mockRTForDeploy`
- Test: `internal/runtime/cleanup_test.go` — add `TestStubCleanup`

**Interfaces:**
- Consumes: nothing new
- Produces: `types.CleanupOptions{Containers, Images, Volumes, BuildCache, DryRun bool}`, `types.CleanupReport{DryRun bool, ContainersRemoved, ImagesRemoved, VolumesRemoved, BuildCacheRemoved int, Errors []string}`, `Manager.Cleanup(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go — append this test
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), types.CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.DryRun {
		t.Error("DryRun should be false for default options")
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 {
		t.Errorf("stub Cleanup should return zeroed report, got %+v", report)
	}
}
```

This test references `types.CleanupOptions` and `Manager.Cleanup`, neither of which exists yet. Because `Manager` is an interface, this also makes `go test ./internal/runtime/...` fail to compile, which is the expected red state.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: FAIL with compile error `undefined: types.CleanupOptions` and/or `stubManager does not implement Manager`.

- [ ] **Step 3: Add the types to `internal/types/types.go`**

Append at the end of the file (after `AppEntry`):

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	BuildCache bool
	DryRun     bool
}

type CleanupReport struct {
	DryRun            bool     `json:"dry_run"`
	ContainersRemoved int      `json:"containers_removed"`
	ImagesRemoved     int      `json:"images_removed"`
	VolumesRemoved    int      `json:"volumes_removed"`
	BuildCacheRemoved int      `json:"build_cache_removed"`
	Errors            []string `json:"errors,omitempty"`
}
```

- [ ] **Step 4: Add `Cleanup` to the `Manager` interface in `internal/runtime/runtime.go`**

In the interface block, add after the `KeepLastNImages` line:

```go
type Manager interface {
	Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error)
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	RemoveBySuffix(ctx context.Context, name string, suffix string) error
	IsActive(ctx context.Context, name string) (bool, error)
	GetContainerPort(ctx context.Context, name string, suffix string) (int, error)
	List(ctx context.Context) ([]types.AppStatus, error)
	Logs(ctx context.Context, name string, opts LogOptions) (io.ReadCloser, error)
	WaitForReady(ctx context.Context, name string, internalPort int) error
	WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
}
```

Add the stub method after the `stubManager.KeepLastNImages` method:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error) {
	return types.CleanupReport{}, nil
}
```

- [ ] **Step 5: Add `Cleanup` to `mockRTForDeploy` in `internal/cli/root_test.go`**

Add this method after `KeepLastNImages` (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error) {
	return types.CleanupReport{}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: PASS

Run: `go test ./internal/cli/... -run TestMockRTForDeployImplementsManager -v -count=1`

Expected: PASS (mock still satisfies the interface)

- [ ] **Step 7: Run all tests in the two touched packages**

Run: `go test ./internal/runtime/... ./internal/cli/... -count=1`

Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/types/types.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat: add CleanupOptions/CleanupReport types and Manager.Cleanup interface method"
```

---

### Task 2: Docker prune argument helpers (pure functions)

**Files:**
- Modify: `internal/runtime/docker.go` — add 8 pure functions after `buildRunArgs` (line 470)
- Test: `internal/runtime/cleanup_test.go` — add arg-builder tests

**Interfaces:**
- Consumes: `labelKey` constant (`internal/runtime/docker.go:76`)
- Produces: pure functions used by `dockerRuntime.Cleanup` in Task 3:
  - `cleanupContainerListArgs() []string`
  - `cleanupContainerPruneArgs() []string`
  - `cleanupImageListArgs() []string`
  - `cleanupImagePruneArgs() []string`
  - `cleanupVolumeListArgs() []string`
  - `cleanupVolumePruneArgs() []string`
  - `cleanupBuilderListArgs() []string`
  - `cleanupBuilderPruneArgs() []string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go — append these tests
func requireArgsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args = %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestCleanupContainerListArgs(t *testing.T) {
	requireArgsEqual(t, cleanupContainerListArgs(),
		[]string{"ps", "-a", "--filter", "label=" + labelKey, "--filter", "status=exited", "--format", "{{.Names}}"})
}

func TestCleanupContainerPruneArgs(t *testing.T) {
	requireArgsEqual(t, cleanupContainerPruneArgs(),
		[]string{"container", "prune", "-f", "--filter", "label=" + labelKey})
}

func TestCleanupImageListArgs(t *testing.T) {
	requireArgsEqual(t, cleanupImageListArgs(),
		[]string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"})
}

func TestCleanupImagePruneArgs(t *testing.T) {
	requireArgsEqual(t, cleanupImagePruneArgs(),
		[]string{"image", "prune", "-f"})
}

func TestCleanupVolumeListArgs(t *testing.T) {
	requireArgsEqual(t, cleanupVolumeListArgs(),
		[]string{"volume", "ls", "-q", "--filter", "dangling=true"})
}

func TestCleanupVolumePruneArgs(t *testing.T) {
	requireArgsEqual(t, cleanupVolumePruneArgs(),
		[]string{"volume", "prune", "-f"})
}

func TestCleanupBuilderListArgs(t *testing.T) {
	requireArgsEqual(t, cleanupBuilderListArgs(),
		[]string{"builder", "du", "--format", "{{.ID}}"})
}

func TestCleanupBuilderPruneArgs(t *testing.T) {
	requireArgsEqual(t, cleanupBuilderPruneArgs(),
		[]string{"builder", "prune", "-f"})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestCleanup|TestCleanupBuilder" -v -count=1`

Expected: FAIL with compile error `undefined: cleanupContainerListArgs` (and the other helpers).

- [ ] **Step 3: Add the pure helpers to `internal/runtime/docker.go`**

Add after `buildRunArgs` (before the `Run` method):

```go
func cleanupContainerListArgs() []string {
	return []string{"ps", "-a", "--filter", "label=" + labelKey, "--filter", "status=exited", "--format", "{{.Names}}"}
}

func cleanupContainerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label=" + labelKey}
}

func cleanupImageListArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func cleanupImagePruneArgs() []string {
	return []string{"image", "prune", "-f"}
}

func cleanupVolumeListArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
}

func cleanupVolumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func cleanupBuilderListArgs() []string {
	return []string{"builder", "du", "--format", "{{.ID}}"}
}

func cleanupBuilderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestCleanup|TestCleanupBuilder" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/cleanup_test.go
git commit -m "feat: add docker cleanup argument builders"
```

---

### Task 3: `dockerRuntime.Cleanup` implementation

**Files:**
- Modify: `internal/runtime/docker.go` — add `runDocker`, `nonEmptyLines`, `countBuilderPruneOutput`, `dockerRuntime.Cleanup`
- Test: `internal/runtime/cleanup_test.go` — add parser tests

**Interfaces:**
- Consumes: arg builders from Task 2, `types.CleanupOptions`/`types.CleanupReport` from Task 1
- Produces: `dockerRuntime.Cleanup(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error)` — in dry-run it lists candidates and returns counts; otherwise it runs prunes and counts output lines. Pure helpers `nonEmptyLines(out string) []string` and `countBuilderPruneOutput(out string) int` are tested directly.

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go — append these tests
func TestNonEmptyLines(t *testing.T) {
	input := "\n  line one \n\n line two \n\n"
	got := nonEmptyLines(input)
	want := []string{"line one", "line two"}
	if len(got) != len(want) {
		t.Fatalf("nonEmptyLines(%q) = %v, want %v", input, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("nonEmptyLines[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNonEmptyLinesEmpty(t *testing.T) {
	if got := nonEmptyLines("  \n\n  "); len(got) != 0 {
		t.Fatalf("nonEmptyLines(whitespace) = %v, want empty", got)
	}
}

func TestCountBuilderPruneOutput(t *testing.T) {
	input := "abcdef123456\nfedcba654321\n\nTotal reclaimed space: 1.2GB\n"
	if got := countBuilderPruneOutput(input); got != 2 {
		t.Errorf("countBuilderPruneOutput() = %d, want 2", got)
	}
}

func TestCountBuilderPruneOutputEmpty(t *testing.T) {
	if got := countBuilderPruneOutput("\n"); got != 0 {
		t.Errorf("countBuilderPruneOutput(empty) = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestNonEmptyLines|TestCountBuilderPruneOutput" -v -count=1`

Expected: FAIL with compile error `undefined: nonEmptyLines` / `undefined: countBuilderPruneOutput`.

- [ ] **Step 3: Add the implementation to `internal/runtime/docker.go`**

Add these helpers after the arg builders:

```go
func (r *dockerRuntime) runDocker(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

func nonEmptyLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func countBuilderPruneOutput(out string) int {
	count := 0
	for _, line := range nonEmptyLines(out) {
		if !strings.HasPrefix(line, "Total reclaimed") {
			count++
		}
	}
	return count
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error) {
	report := types.CleanupReport{DryRun: opts.DryRun}

	if opts.Containers {
		if opts.DryRun {
			out, err := r.runDocker(ctx, cleanupContainerListArgs())
			if err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("list containers: %v", err))
			} else {
				report.ContainersRemoved = len(nonEmptyLines(string(out)))
			}
		} else {
			if out, err := r.runDocker(ctx, cleanupContainerPruneArgs()); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("prune containers: %v", err))
			} else {
				report.ContainersRemoved = len(nonEmptyLines(string(out)))
			}
		}
	}

	if opts.Images {
		if opts.DryRun {
			out, err := r.runDocker(ctx, cleanupImageListArgs())
			if err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("list images: %v", err))
			} else {
				report.ImagesRemoved = len(nonEmptyLines(string(out)))
			}
		} else {
			if out, err := r.runDocker(ctx, cleanupImagePruneArgs()); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("prune images: %v", err))
			} else {
				report.ImagesRemoved = len(nonEmptyLines(string(out)))
			}
		}
	}

	if opts.Volumes {
		if opts.DryRun {
			out, err := r.runDocker(ctx, cleanupVolumeListArgs())
			if err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("list volumes: %v", err))
			} else {
				report.VolumesRemoved = len(nonEmptyLines(string(out)))
			}
		} else {
			if out, err := r.runDocker(ctx, cleanupVolumePruneArgs()); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("prune volumes: %v", err))
			} else {
				report.VolumesRemoved = len(nonEmptyLines(string(out)))
			}
		}
	}

	if opts.BuildCache {
		if opts.DryRun {
			out, err := r.runDocker(ctx, cleanupBuilderListArgs())
			if err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("list build cache: %v", err))
			} else {
				report.BuildCacheRemoved = len(nonEmptyLines(string(out)))
			}
		} else {
			if out, err := r.runDocker(ctx, cleanupBuilderPruneArgs()); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("prune build cache: %v", err))
			} else {
				report.BuildCacheRemoved = countBuilderPruneOutput(string(out))
			}
		}
	}

	return report, nil
}
```

`fmt`, `os/exec`, `strings`, and `types` are already imported by `docker.go` (see lines 3-21).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestNonEmptyLines|TestCountBuilderPruneOutput|TestStubCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/cleanup_test.go
git commit -m "feat: implement dockerRuntime.Cleanup with dry-run support"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, `initCleanupFlags`, `resolveCleanupFlags`, `printCleanupReport`; register in `init()` after line 75
- Test: `internal/cli/cleanup_test.go` — new file

**Interfaces:**
- Consumes: `types.CleanupOptions`/`types.CleanupReport` (Task 1), `Manager.Cleanup` and `Manager.KeepLastNImages` (Tasks 1-3), `getEnv(cmd)`, `config.NewStoreWithEnv(dataDir, env)`
- Produces: `initCleanupFlags(cmd *cobra.Command)` (also used by tests for a fresh command), `resolveCleanupFlags(cmd *cobra.Command) (types.CleanupOptions, int, error)` (returns options + keep count), `printCleanupReport(r types.CleanupReport)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/types"
)

func newCleanupTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "cleanup"}
	initCleanupFlags(c)
	return c
}

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, name := range []string{"all", "containers", "images", "volumes", "build-cache", "dry-run", "yes", "keep"} {
		if flag := cleanupCmd.Flags().Lookup(name); flag == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestResolveCleanupFlagsDefault(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.ParseFlags([]string{})
	opts, keep, err := resolveCleanupFlags(cmd)
	if err != nil {
		t.Fatalf("resolveCleanupFlags: %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.BuildCache {
		t.Errorf("default opts = %+v, want Containers/Images/BuildCache true", opts)
	}
	if opts.Volumes {
		t.Error("volumes should default to false")
	}
	if opts.DryRun {
		t.Error("dry-run should default to false")
	}
	if keep != 5 {
		t.Errorf("keep = %d, want 5", keep)
	}
}

func TestResolveCleanupFlagsAll(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.ParseFlags([]string{"--all"})
	opts, _, err := resolveCleanupFlags(cmd)
	if err != nil {
		t.Fatalf("resolveCleanupFlags: %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.BuildCache {
		t.Errorf("--all opts = %+v, want Containers/Images/BuildCache true", opts)
	}
	if opts.Volumes {
		t.Error("--all must not enable volumes")
	}
}

func TestResolveCleanupFlagsExplicitVolumesRequiresYes(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.ParseFlags([]string{"--volumes"})
	_, _, err := resolveCleanupFlags(cmd)
	if err == nil {
		t.Fatal("expected error for --volumes without --yes")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error should mention --yes, got: %v", err)
	}
}

func TestResolveCleanupFlagsVolumesWithYes(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.ParseFlags([]string{"--volumes", "--yes"})
	opts, _, err := resolveCleanupFlags(cmd)
	if err != nil {
		t.Fatalf("resolveCleanupFlags: %v", err)
	}
	if !opts.Volumes {
		t.Error("volumes should be enabled")
	}
}

func TestResolveCleanupFlagsVolumesDryRunNoConfirmNeeded(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.ParseFlags([]string{"--volumes", "--dry-run"})
	opts, _, err := resolveCleanupFlags(cmd)
	if err != nil {
		t.Fatalf("resolveCleanupFlags: %v", err)
	}
	if !opts.Volumes || !opts.DryRun {
		t.Errorf("opts = %+v, want Volumes and DryRun true", opts)
	}
}

func TestResolveCleanupFlagsKeep(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.ParseFlags([]string{"--images", "--keep", "10"})
	opts, keep, err := resolveCleanupFlags(cmd)
	if err != nil {
		t.Fatalf("resolveCleanupFlags: %v", err)
	}
	if !opts.Images {
		t.Error("images should be enabled")
	}
	if keep != 10 {
		t.Errorf("keep = %d, want 10", keep)
	}
}

func TestPrintCleanupReport(t *testing.T) {
	out := captureOutput(func() {
		printCleanupReport(types.CleanupReport{
			DryRun:            true,
			ContainersRemoved: 3,
			ImagesRemoved:     7,
		})
	})
	for _, want := range []string{"dry-run summary", "containers removed: 3", "images removed: 7"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
```

`captureOutput` already exists in `internal/cli/root_test.go:57`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup|TestResolveCleanupFlags|TestPrintCleanupReport" -v -count=1`

Expected: FAIL with compile error `undefined: cleanupCmd` / `undefined: initCleanupFlags` / `undefined: resolveCleanupFlags` / `undefined: printCleanupReport`.

- [ ] **Step 3: Add the implementation to `internal/cli/root.go`**

Add the `cleanupCmd` command definition after `runCmd` (after line 1162):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Removes unused Docker resources (stopped Tengiz containers, dangling images,
old app images, unused volumes, and build cache) to reclaim disk space.

By default prunes stopped Tengiz containers, dangling images, build cache, and
old app images (keeping the last 5 per app). Use --volumes to also prune unused
volumes (this permanently deletes volume data — requires --yes). Use --dry-run
to preview what would be removed without removing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		opts, keep, err := resolveCleanupFlags(cmd)
		if err != nil {
			return err
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}

		if opts.Images {
			store := config.NewStoreWithEnv(dataDir, env)
			apps, listErr := store.ListApps()
			if listErr == nil {
				for _, app := range apps {
					if opts.DryRun {
						fmt.Printf("[tengiz] dry-run: would keep last %d images for %s\n", keep, app.Name)
					} else if err := rt.KeepLastNImages(cmd.Context(), app.Name, keep); err != nil {
						log.Printf("[tengiz] warning: image retention for %s: %v", app.Name, err)
					}
				}
			}
		}

		printCleanupReport(report)
		return nil
	},
}

func initCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("all", false, "prune containers, images, and build cache")
	cmd.Flags().Bool("containers", false, "prune stopped Tengiz containers")
	cmd.Flags().Bool("images", false, "prune dangling images and old app images")
	cmd.Flags().Bool("volumes", false, "prune unused volumes (requires --yes)")
	cmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cmd.Flags().Bool("yes", false, "confirm destructive operations (required with --volumes)")
	cmd.Flags().Int("keep", 0, "keep the last N images per app (default 5)")
}

func resolveCleanupFlags(cmd *cobra.Command) (types.CleanupOptions, int, error) {
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	keep, _ := cmd.Flags().GetInt("keep")
	if keep <= 0 {
		keep = 5
	}

	if !containers && !images && !volumes && !buildCache && !all {
		containers, images, buildCache = true, true, true
	}
	if all {
		containers, images, buildCache = true, true, true
	}

	if volumes && !dryRun {
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			return types.CleanupOptions{}, 0, fmt.Errorf("--volumes permanently removes unused volumes; pass --yes to confirm")
		}
	}

	return types.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		BuildCache: buildCache,
		DryRun:     dryRun,
	}, keep, nil
}

func printCleanupReport(r types.CleanupReport) {
	if r.DryRun {
		fmt.Println("[tengiz] dry-run summary (nothing removed):")
	} else {
		fmt.Println("[tengiz] cleanup summary:")
	}
	fmt.Printf("containers removed: %d\n", r.ContainersRemoved)
	fmt.Printf("images removed: %d\n", r.ImagesRemoved)
	fmt.Printf("volumes removed: %d\n", r.VolumesRemoved)
	fmt.Printf("build cache entries removed: %d\n", r.BuildCacheRemoved)
	for _, e := range r.Errors {
		fmt.Printf("warning: %s\n", e)
	}
}
```

Register the command and its flags in `init()` — add right after `rootCmd.AddCommand(runCmd)` (line 67):

```go
	rootCmd.AddCommand(cleanupCmd)
	initCleanupFlags(cleanupCmd)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestResolveCleanupFlags|TestPrintCleanupReport" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all CLI tests**

Run: `go test ./internal/cli/... -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 5: Documentation, feature tracking, and full verification

**Files:**
- Modify: `README.md` — add a `### tengiz cleanup` section after the `tengiz secret list` section (after line 416) and a row in the top-level quick reference near line 100
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented in the Priority Ranking table and the Özellikler section
- No code changes

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

Insert after the `#### tengiz secret list <app>` section (after line 416):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space. Uses label-based filtering so only Tengiz-managed containers are pruned.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped Tengiz containers (labeled `tengiz-app`) |
| `--images` | Prune dangling images and old app images (keeps the last `--keep N` per app) |
| `--build-cache` | Prune Docker build cache |
| `--volumes` | Prune unused volumes (permanently deletes volume data — requires `--yes`) |
| `--all` | Prune containers, images, and build cache (does NOT include volumes) |
| `--dry-run` | Show what would be removed without removing anything |
| `--yes` | Confirm destructive operations (required with `--volumes`) |
| `--keep N` | Keep the last N images per app (default 5) |

With no category flags, `tengiz cleanup` prunes stopped containers, dangling images, build cache, and old app images.

```bash
tengiz cleanup              # safe defaults (containers + images + build cache)
tengiz cleanup --dry-run    # preview before deleting
tengiz cleanup --all --yes  # full prune including unused volumes
```
```

Also add a row to the quick-reference command list near line 100, after `tengiz rollback`:

```markdown
tengiz cleanup          # prune unused Docker resources to reclaim disk space
```

- [ ] **Step 2: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the Priority Ranking table, change row #6 from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the "Implemented Features" table (around line 241), add a row:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-19) |
```

In the detailed "## Docker Housekeeping (Otomatik Temizlik)" section, add a status line after `- **Detected:** 2026-07-14`:

```markdown
- **Status:** ✅ Implemented (2026-08-19)
```

- [ ] **Step 3: Build the binary**

Run: `go build -o tengiz .`

Expected: Build succeeds, `tengiz cleanup --help` is available (verify with `./tengiz cleanup --help`).

- [ ] **Step 4: Run static analysis**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 5: Run the full test suite**

Run: `go test ./... -count=1`

Expected: All PASS. Note: `internal/proxy` tests are slow (~2s each) due to TCP dial timeouts — they are expected to pass, just not instantly.

- [ ] **Step 6: Self-review against the spec**

Check against `docs/FUTURES_FEATURES.md` feature #6:
- `tengiz cleanup` command ✅ (Task 4)
- Label-based filtering protects Tengiz-managed containers ✅ (Task 2 — `cleanupContainerPruneArgs` uses `--filter label=tengiz-app`)
- Disk reclaim for the scale-to-zero/continuous-deploy workload ✅ (containers, dangling images, old app images, build cache)
- Per-app image retention ✅ (Task 4 — `KeepLastNImages` with default keep=5)
- Dry-run preview ✅ (Task 3 — dry-run lists candidates, never prunes)

- [ ] **Step 7: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task". None present — every step has complete code.

- [ ] **Step 8: Type consistency check**

- `types.CleanupOptions{Containers, Images, Volumes, BuildCache, DryRun bool}` — same field set in Tasks 1, 3, 4
- `types.CleanupReport{DryRun bool, ContainersRemoved, ImagesRemoved, VolumesRemoved, BuildCacheRemoved int, Errors []string}` — Task 1 defines, Task 3 populates, Task 4 prints
- `Manager.Cleanup(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error)` — same signature on interface (Task 1), stub (Task 1), dockerRuntime (Task 3), mock (Task 1)
- Helper names `cleanupContainerListArgs`, `cleanupContainerPruneArgs`, `cleanupImageListArgs`, `cleanupImagePruneArgs`, `cleanupVolumeListArgs`, `cleanupVolumePruneArgs`, `cleanupBuilderListArgs`, `cleanupBuilderPruneArgs` — identical in Tasks 2 and 3
- `resolveCleanupFlags` returns `(types.CleanupOptions, int, error)` — Task 4 definition matches its test and the RunE handler

- [ ] **Step 9: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping as implemented"
```

---

## Execution Handoff

After the plan is executed, run the verification suite (`go build -o tengiz .`, `go vet ./...`, `go test ./... -count=1`) before declaring the feature complete.
