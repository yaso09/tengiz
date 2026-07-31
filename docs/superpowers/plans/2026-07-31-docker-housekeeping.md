# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command plus an optional periodic cleanup loop that prunes unused Docker resources (stopped containers, dangling/unused images, unused volumes, unused networks, build cache) while protecting all Tengiz-managed containers via label-based filtering.

**Architecture:** A new `internal/runtime/prune.go` adds `CleanupOptions`/`CleanupReport` types, exported per-category Docker arg builders (so the CLI can preview with `--dry-run`), and a `Prune` method on `dockerRuntime` + the `Manager` interface. A `cleanupCmd` in the CLI maps `--containers/--images/--volumes/--networks/--build-cache/--all/--app/--before` flags onto those builders and runs them. A small `internal/cleanup` package (mirroring the `health` background-manager pattern) runs the same `Prune` on a ticker and alerts via the existing notification system; `tengiz cleanup --interval 24h` runs it in the foreground. Safety invariant: default container pruning always passes `--filter label!=tengiz-app`, so scale-to-zero stopped containers (and preview/helper containers) are never removed; tagged Tengiz images are only removed with the explicit `--all` or `--app` opt-ins.

**Tech Stack:** Go 1.26, stdlib `os/exec` (existing Docker CLI pattern — no Docker SDK), Cobra (CLI), existing `runtime.Manager`, `notify.Manager`, `config.Store`. No new external dependencies.

## Global Constraints

- Default cleanup categories match `docker system prune`: stopped containers, dangling images, unused networks, build cache. Volumes are excluded unless `--volumes` is passed.
- Default container prune MUST include `--filter label!=tengiz-app` so Tengiz-managed containers (running, scale-to-zero stopped, versioned, preview, `run --rm` helper) are never pruned. `--app <name>` overrides this to `--filter label=tengiz-app=<name>` and the CLI prints a warning.
- Tagged Tengiz images (`tengiz-apps/<app>:*`) are never removed by default; `--all` (all unused images) and `--app <name>` (that app's images, implies `-a`) are explicit opt-ins.
- All prune commands always pass `-f` to Docker — never prompt, stay scriptable. `--dry-run` prints the exact `docker` command strings without executing.
- No new Go module dependencies (only `github.com/spf13/cobra` + existing indirect deps).
- `docker` is invoked via `os/exec.CommandContext(ctx, "docker", args...)` — same pattern as `internal/runtime/docker.go`.
- Feature work must be on a branch named `feat/docker-housekeeping` (created via `superpowers:using-git-worktrees` skill at execution time or `git checkout -b feat/docker-housekeeping`).
- Existing tests must keep passing; run `go test ./... -v -count=1` and `go vet ./...` before committing each task.
- Per AGENTS.md rules: add/update tests for every change, pass them, then commit; update `README.md` and docs for UI/UX changes.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` | `CleanupOptions`, `CleanupReport`, exported per-category arg builders, `parseReclaimed`, `Prune` impl on `dockerRuntime` |
| `internal/runtime/runtime.go` | Add `Prune` to `Manager` interface + stub implementation |
| `internal/runtime/prune_test.go` | Arg-builder, `parseReclaimed`, and stub tests |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` (required to keep the package compiling) |
| `internal/cli/root.go` | `cleanupCmd` + flags + helpers (`cleanupOptions`, `printCleanupPlan`, `printCleanupReport`, `reclaimedOrEmpty`, `runCleanupLoop`) |
| `internal/cli/cleanup_test.go` | CLI tests (command registration, flags, default category logic, dry-run output) |
| `internal/cleanup/cleanup.go` | Periodic `Manager` (ticker loop + failure notifications) |
| `internal/cleanup/cleanup_test.go` | Periodic manager tests (fake pruner) |
| `README.md` | Add `tengiz cleanup` CLI reference section |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as implemented |

No new files are created except `internal/runtime/prune.go`, `internal/runtime/prune_test.go`, `internal/cli/cleanup_test.go`, `internal/cleanup/cleanup.go`, `internal/cleanup/cleanup_test.go`.

---

### Task 1: Runtime prune primitives

**Files:**
- Create: `internal/runtime/prune.go`
- Modify: `internal/runtime/runtime.go:31-49` — add `Prune` to `Manager` interface; add stub method near line 119
- Modify: `internal/cli/root_test.go:99` — add `Prune` to `mockRTForDeploy`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: existing `labelKey` const (`"tengiz-app"`) from `internal/runtime/docker.go:76`
- Produces:
  - `runtime.CleanupOptions` struct with fields `Containers, Images, Volumes, Networks, BuildCache, All bool`, `App, Before string`
  - `runtime.CleanupReport` struct with fields `ContainersSpace, ImagesSpace, VolumesSpace, NetworksSpace, BuildCacheSpace string`
  - `runtime.BuildContainerPruneArgs(opts CleanupOptions) []string`
  - `runtime.BuildImagePruneArgs(opts CleanupOptions) []string`
  - `runtime.BuildVolumePruneArgs(opts CleanupOptions) []string`
  - `runtime.BuildNetworkPruneArgs(opts CleanupOptions) []string`
  - `runtime.BuildBuilderPruneArgs(opts CleanupOptions) []string`
  - `func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)` (also on `Manager` interface and `stubManager`)

- [ ] **Step 1: Write the failing tests**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildContainerPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want []string
	}{
		{
			name: "default protects tengiz containers",
			opts: CleanupOptions{},
			want: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name: "app scoped",
			opts: CleanupOptions{App: "myapp"},
			want: []string{"container", "prune", "-f", "--filter", "label=tengiz-app=myapp"},
		},
		{
			name: "before duration",
			opts: CleanupOptions{Before: "72h"},
			want: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app", "--filter", "until=72h"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildContainerPruneArgs(tt.opts)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildContainerPruneArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildImagePruneArgs(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want []string
	}{
		{
			name: "default dangling only",
			opts: CleanupOptions{},
			want: []string{"image", "prune", "-f"},
		},
		{
			name: "all unused images",
			opts: CleanupOptions{All: true},
			want: []string{"image", "prune", "-f", "-a"},
		},
		{
			name: "app scoped implies all",
			opts: CleanupOptions{App: "myapp"},
			want: []string{"image", "prune", "-f", "-a", "--filter", "reference=tengiz-apps/myapp:*"},
		},
		{
			name: "before duration",
			opts: CleanupOptions{Before: "1w"},
			want: []string{"image", "prune", "-f", "--filter", "until=1w"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildImagePruneArgs(tt.opts)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildImagePruneArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildVolumePruneArgs(t *testing.T) {
	got := BuildVolumePruneArgs(CleanupOptions{})
	want := []string{"volume", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildVolumePruneArgs() = %v, want %v", got, want)
	}

	got = BuildVolumePruneArgs(CleanupOptions{Before: "24h"})
	want = []string{"volume", "prune", "-f", "--filter", "until=24h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildVolumePruneArgs() with before = %v, want %v", got, want)
	}
}

func TestBuildNetworkPruneArgs(t *testing.T) {
	got := BuildNetworkPruneArgs(CleanupOptions{})
	want := []string{"network", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildNetworkPruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildBuilderPruneArgs(t *testing.T) {
	got := BuildBuilderPruneArgs(CleanupOptions{})
	want := []string{"builder", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildBuilderPruneArgs() = %v, want %v", got, want)
	}
}

func TestParseReclaimed(t *testing.T) {
	output := `Deleted Containers:
abc123

Total reclaimed space: 1.2MB`
	if got := parseReclaimed(output); got != "1.2MB" {
		t.Errorf("parseReclaimed() = %q, want %q", got, "1.2MB")
	}

	if got := parseReclaimed("nothing deleted"); got != "" {
		t.Errorf("parseReclaimed() on empty = %q, want empty", got)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	rep, err := m.Prune(context.Background(), CleanupOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	if rep == nil {
		t.Fatal("stub Prune() returned nil report")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestBuild|TestParseReclaimed|TestStubPrune" -v -count=1`

Expected: FAIL with `undefined: BuildContainerPruneArgs`, `undefined: parseReclaimed`, `undefined: CleanupOptions`, etc.

- [ ] **Step 3: Create `internal/runtime/prune.go`**

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	All        bool
	App        string
	Before     string
}

type CleanupReport struct {
	ContainersSpace string
	ImagesSpace     string
	VolumesSpace    string
	NetworksSpace   string
	BuildCacheSpace string
}

func BuildContainerPruneArgs(opts CleanupOptions) []string {
	args := []string{"container", "prune", "-f"}
	if opts.App != "" {
		args = append(args, "--filter", fmt.Sprintf("label=%s=%s", labelKey, opts.App))
	} else {
		args = append(args, "--filter", fmt.Sprintf("label!=%s", labelKey))
	}
	if opts.Before != "" {
		args = append(args, "--filter", fmt.Sprintf("until=%s", opts.Before))
	}
	return args
}

func BuildImagePruneArgs(opts CleanupOptions) []string {
	args := []string{"image", "prune", "-f"}
	if opts.All || opts.App != "" {
		args = append(args, "-a")
	}
	if opts.App != "" {
		args = append(args, "--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", opts.App))
	}
	if opts.Before != "" {
		args = append(args, "--filter", fmt.Sprintf("until=%s", opts.Before))
	}
	return args
}

func BuildVolumePruneArgs(opts CleanupOptions) []string {
	args := []string{"volume", "prune", "-f"}
	if opts.Before != "" {
		args = append(args, "--filter", fmt.Sprintf("until=%s", opts.Before))
	}
	return args
}

func BuildNetworkPruneArgs(opts CleanupOptions) []string {
	args := []string{"network", "prune", "-f"}
	if opts.Before != "" {
		args = append(args, "--filter", fmt.Sprintf("until=%s", opts.Before))
	}
	return args
}

func BuildBuilderPruneArgs(opts CleanupOptions) []string {
	args := []string{"builder", "prune", "-f"}
	if opts.Before != "" {
		args = append(args, "--filter", fmt.Sprintf("until=%s", opts.Before))
	}
	return args
}

func parseReclaimed(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}

func (r *dockerRuntime) runPrune(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker %s %s: %w\n%s", args[0], args[1], err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	rep := &CleanupReport{}
	if opts.Containers {
		out, err := r.runPrune(ctx, BuildContainerPruneArgs(opts))
		if err != nil {
			return nil, err
		}
		rep.ContainersSpace = parseReclaimed(out)
	}
	if opts.Images {
		out, err := r.runPrune(ctx, BuildImagePruneArgs(opts))
		if err != nil {
			return nil, err
		}
		rep.ImagesSpace = parseReclaimed(out)
	}
	if opts.Volumes {
		out, err := r.runPrune(ctx, BuildVolumePruneArgs(opts))
		if err != nil {
			return nil, err
		}
		rep.VolumesSpace = parseReclaimed(out)
	}
	if opts.Networks {
		out, err := r.runPrune(ctx, BuildNetworkPruneArgs(opts))
		if err != nil {
			return nil, err
		}
		rep.NetworksSpace = parseReclaimed(out)
	}
	if opts.BuildCache {
		out, err := r.runPrune(ctx, BuildBuilderPruneArgs(opts))
		if err != nil {
			return nil, err
		}
		rep.BuildCacheSpace = parseReclaimed(out)
	}
	return rep, nil
}
```

- [ ] **Step 4: Add `Prune` to the `Manager` interface and stub**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after the `KeepLastNImages` line):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
```

Add the stub method (after the `stubManager.KeepLastNImages` method):

```go
func (m *stubManager) Prune(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return &CleanupReport{}, nil
}
```

- [ ] **Step 5: Add `Prune` to `mockRTForDeploy` in `internal/cli/root_test.go`**

Add after the `KeepLastNImages` method of `mockRTForDeploy`:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) { return &runtime.CleanupReport{}, nil }
```

- [ ] **Step 6: Run runtime tests and full build to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS (new + existing tests).

Run: `go build ./...`

Expected: Build succeeds — confirms no other type implements `runtime.Manager` without `Prune`.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go internal/runtime/runtime.go internal/cli/root_test.go
git commit -m "feat(runtime): add label-protected Docker prune primitives"
```

---

### Task 2: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — import `strings` (already imported), add `cleanupCmd`, register it and its flags in `init()`, add helper functions
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.BuildContainerPruneArgs`, `runtime.BuildImagePruneArgs`, `runtime.BuildVolumePruneArgs`, `runtime.BuildNetworkPruneArgs`, `runtime.BuildBuilderPruneArgs`, `runtime.NewDocker()` from Task 1
- Produces: `tengiz cleanup` command with flags `--containers --images --volumes --networks --build-cache --all --app <name> --before <dur> --dry-run`. Helper `cleanupOptions(cmd *cobra.Command) runtime.CleanupOptions` applies default-category logic (used by Task 3 tests too). `printCleanupPlan`, `printCleanupReport`, `reclaimedOrEmpty`.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{
		"all", "containers", "images", "volumes", "networks",
		"build-cache", "dry-run", "app", "before",
	} {
		if lookup := cleanupCmd.Flags().Lookup(flag); lookup == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupOptionsDefaults(t *testing.T) {
	cmd := cleanupCmd
	cmd.ParseFlags([]string{})
	opts := cleanupOptions(cmd)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Errorf("default should include containers, images, networks, build cache; got %+v", opts)
	}
	if opts.Volumes {
		t.Errorf("default should exclude volumes; got %+v", opts)
	}
}

func TestCleanupOptionsExplicit(t *testing.T) {
	cmd := cleanupCmd
	cmd.ParseFlags([]string{"--volumes", "--all", "--app", "myapp", "--before", "72h"})
	opts := cleanupOptions(cmd)
	if !opts.Volumes {
		t.Error("volumes not set")
	}
	if !opts.All {
		t.Error("all not set")
	}
	if opts.App != "myapp" {
		t.Errorf("app = %q, want %q", opts.App, "myapp")
	}
	if opts.Before != "72h" {
		t.Errorf("before = %q, want %q", opts.Before, "72h")
	}
}

func TestCleanupDryRunOutput(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	for _, want := range []string{
		"docker container prune -f --filter label!=tengiz-app",
		"docker image prune -f",
		"docker network prune -f",
		"docker builder prune -f",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("dry-run output missing %q; got:\n%s", want, output)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — `undefined: cleanupOptions`, `cleanupCmd` missing.

- [ ] **Step 3: Add the `cleanupCmd` command and helpers to `internal/cli/root.go`**

Add the command (place it after `logsCmd`, before `devCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Prunes unused Docker resources: stopped containers, dangling images,
unused volumes, unused networks, and the Docker build cache.

By default cleanup removes stopped containers that are NOT managed by Tengiz
(--filter label!=tengiz-app), so scale-to-zero stopped containers are always
protected. Tagged Tengiz images are never touched unless --all or --app is given.

Use --dry-run to print the exact docker commands that would be executed.

Examples:
  tengiz cleanup                          # containers + dangling images + networks + build cache
  tengiz cleanup --volumes --all          # also volumes and all unused images
  tengiz cleanup --app myapp --images     # remove unused images for one app
  tengiz cleanup --before 72h             # only resources older than 72 hours
  tengiz cleanup --dry-run                # preview without executing`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptions(cmd)

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		if dryRun {
			return printCleanupPlan(opts)
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if opts.App != "" && opts.Containers {
			fmt.Printf("[tengiz] warning: --app %s will prune stopped containers of that app\n", opts.App)
		}

		report, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return err
		}
		printCleanupReport(opts, report)
		return nil
	},
}
```

Register the command and its flags in `init()` (add after `logsCmd.Flags()` block):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune unused images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "print the docker commands without executing them")
	cleanupCmd.Flags().String("app", "", "limit pruning to resources of a specific app")
	cleanupCmd.Flags().String("before", "", "only prune resources older than this duration (e.g. 72h, 1w)")
```

Add the helper functions at the bottom of `internal/cli/root.go` (before `func Execute()`):

```go
func cleanupOptions(cmd *cobra.Command) runtime.CleanupOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	all, _ := cmd.Flags().GetBool("all")
	app, _ := cmd.Flags().GetString("app")
	before, _ := cmd.Flags().GetString("before")

	if !containers && !images && !volumes && !networks && !buildCache {
		containers, images, networks, buildCache = true, true, true, true
	}

	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		All:        all,
		App:        app,
		Before:     before,
	}
}

func printCleanupPlan(opts runtime.CleanupOptions) error {
	fmt.Println("[tengiz] dry-run: commands that would be executed")
	if opts.Containers {
		fmt.Printf("  docker %s\n", strings.Join(runtime.BuildContainerPruneArgs(opts), " "))
	}
	if opts.Images {
		fmt.Printf("  docker %s\n", strings.Join(runtime.BuildImagePruneArgs(opts), " "))
	}
	if opts.Volumes {
		fmt.Printf("  docker %s\n", strings.Join(runtime.BuildVolumePruneArgs(opts), " "))
	}
	if opts.Networks {
		fmt.Printf("  docker %s\n", strings.Join(runtime.BuildNetworkPruneArgs(opts), " "))
	}
	if opts.BuildCache {
		fmt.Printf("  docker %s\n", strings.Join(runtime.BuildBuilderPruneArgs(opts), " "))
	}
	return nil
}

func printCleanupReport(opts runtime.CleanupOptions, report *runtime.CleanupReport) {
	fmt.Println("[tengiz] cleanup complete:")
	if opts.Containers {
		fmt.Printf("  containers:  reclaimed %s\n", reclaimedOrEmpty(report.ContainersSpace))
	}
	if opts.Images {
		fmt.Printf("  images:      reclaimed %s\n", reclaimedOrEmpty(report.ImagesSpace))
	}
	if opts.Volumes {
		fmt.Printf("  volumes:     reclaimed %s\n", reclaimedOrEmpty(report.VolumesSpace))
	}
	if opts.Networks {
		fmt.Printf("  networks:    reclaimed %s\n", reclaimedOrEmpty(report.NetworksSpace))
	}
	if opts.BuildCache {
		fmt.Printf("  build cache: reclaimed %s\n", reclaimedOrEmpty(report.BuildCacheSpace))
	}
}

func reclaimedOrEmpty(s string) string {
	if s == "" {
		return "0B"
	}
	return s
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: All PASS.

Run: `go build ./...`

Expected: Build succeeds.

- [ ] **Step 5: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS (existing tests untouched).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command with label-protected pruning"
```

---

### Task 3: Periodic cleanup manager + `--interval`

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Create: `internal/cleanup/cleanup_test.go`
- Modify: `internal/cli/root.go` — add `--interval` and `--env` flags, add `runCleanupLoop`, wire the interval branch into `cleanupCmd.RunE`, import `internal/cleanup` and `os/signal` (already imported)

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.Manager` (as a `Pruner`), `notify.Manager` (existing API: `NewManager(dataDir, env)`, `LoadConfig()`, `GetConfig()`, `AddNotifier(...)`, `SendAsync(ctx, types.NotificationEvent)`), `types.NotificationEvent`, `types.EventSystemWarning`
- Produces: `cleanup.Config{Interval time.Duration; Options runtime.CleanupOptions}`, `cleanup.Pruner` interface, `cleanup.NewWithEnv(pruner Pruner, dataDir, env string, cfg Config) *Manager`, `(*Manager).RunOnce(ctx) (*runtime.CleanupReport, error)`, `(*Manager).Start()`, `(*Manager).Stop()`. CLI: `tengiz cleanup --interval <dur>` foreground loop.

- [ ] **Step 1: Write the failing tests**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
)

type fakePruner struct {
	prunes int
}

func (f *fakePruner) Prune(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	f.prunes++
	return &runtime.CleanupReport{}, nil
}

func TestRunOnce(t *testing.T) {
	f := &fakePruner{}
	m := NewWithEnv(f, t.TempDir(), "production", Config{Options: runtime.CleanupOptions{Containers: true}})
	if _, err := m.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if f.prunes != 1 {
		t.Errorf("prunes = %d, want 1", f.prunes)
	}
}

func TestStartStopPeriodic(t *testing.T) {
	f := &fakePruner{}
	m := NewWithEnv(f, t.TempDir(), "production", Config{
		Interval: 20 * time.Millisecond,
		Options:  runtime.CleanupOptions{Containers: true},
	})
	m.Start()
	time.Sleep(80 * time.Millisecond)
	m.Stop()
	if f.prunes == 0 {
		t.Error("expected at least one periodic prune")
	}
}

func TestStartTwiceIsNoop(t *testing.T) {
	f := &fakePruner{}
	m := NewWithEnv(f, t.TempDir(), "production", Config{
		Interval: 10 * time.Millisecond,
		Options:  runtime.CleanupOptions{Containers: true},
	})
	m.Start()
	m.Start()
	time.Sleep(30 * time.Millisecond)
	m.Stop()
	m.Stop()
	if f.prunes == 0 {
		t.Error("expected at least one periodic prune")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL — `no Go files in /home/.../internal/cleanup` (package does not exist yet), `undefined: NewWithEnv`.

- [ ] **Step 3: Create `internal/cleanup/cleanup.go`**

```go
package cleanup

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/yaso09/tengiz/internal/notify"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type Pruner interface {
	Prune(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error)
}

type Config struct {
	Interval time.Duration
	Options  runtime.CleanupOptions
}

type Manager struct {
	pruner Pruner
	cfg    Config
	env    string
	notify *notify.Manager
	mu     sync.Mutex
	cancel context.CancelFunc
}

func NewWithEnv(pruner Pruner, dataDir, env string, cfg Config) *Manager {
	if env == "" {
		env = "production"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 24 * time.Hour
	}

	nm := notify.NewManager(dataDir, env)
	nm.LoadConfig()
	ncfg := nm.GetConfig()
	if ncfg != nil && ncfg.Enabled {
		if ncfg.Discord != nil {
			nm.AddNotifier(notify.NewDiscordNotifier(*ncfg.Discord))
		}
		if ncfg.Slack != nil {
			nm.AddNotifier(notify.NewSlackNotifier(*ncfg.Slack))
		}
		if ncfg.Email != nil {
			nm.AddNotifier(notify.NewEmailNotifier(*ncfg.Email))
		}
	}

	return &Manager{pruner: pruner, cfg: cfg, env: env, notify: nm}
}

func (m *Manager) RunOnce(ctx context.Context) (*runtime.CleanupReport, error) {
	rep, err := m.pruner.Prune(ctx, m.cfg.Options)
	if err != nil {
		m.notify.SendAsync(ctx, types.NotificationEvent{
			Type:    types.EventSystemWarning,
			AppName: "",
			Message: "Docker cleanup failed: " + err.Error(),
			Metadata: map[string]string{
				"environment": m.env,
			},
		})
		return nil, err
	}
	log.Printf("[cleanup] disk reclaimed: containers=%s images=%s volumes=%s networks=%s buildcache=%s",
		rep.ContainersSpace, rep.ImagesSpace, rep.VolumesSpace, rep.NetworksSpace, rep.BuildCacheSpace)
	return rep, nil
}

func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.runLoop(ctx)
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m *Manager) runLoop(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := m.RunOnce(ctx); err != nil {
				log.Printf("[cleanup] periodic cleanup failed: %v", err)
			}
		}
	}
}
```

- [ ] **Step 4: Run cleanup package tests to verify they pass**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: All PASS. (These tests are time-sensitive — they sleep 30–80ms with 10–20ms intervals, matching the granularity used by the `idle` package tests.)

- [ ] **Step 5: Wire `--interval` into the CLI**

In `internal/cli/root.go`:

1. Add the import `"github.com/yaso09/tengiz/internal/cleanup"` to the import block.

2. Register the new flags in `init()` (after the flags added in Task 2):

```go
	cleanupCmd.Flags().Duration("interval", 0, "run cleanup periodically in the foreground (e.g. 24h)")
	cleanupCmd.Flags().String("env", "production", "deployment environment (used for --interval notifications)")
```

3. Add the interval branch to `cleanupCmd.RunE` (right after the `dry-run` check):

```go
		interval, _ := cmd.Flags().GetDuration("interval")
		if interval > 0 {
			return runCleanupLoop(cmd, opts, interval)
		}
```

4. Add `runCleanupLoop` next to the other cleanup helpers:

```go
func runCleanupLoop(cmd *cobra.Command, opts runtime.CleanupOptions, interval time.Duration) error {
	env, _ := cmd.Flags().GetString("env")
	rt, err := runtime.NewDocker()
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}

	mgr := cleanup.NewWithEnv(rt, dataDir, env, cleanup.Config{Interval: interval, Options: opts})

	fmt.Printf("[tengiz] starting periodic cleanup every %s (Ctrl+C to stop)\n", interval)
	mgr.Start()
	defer mgr.Stop()

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer cancel()
	<-ctx.Done()
	fmt.Println("[tengiz] cleanup loop stopped")
	return nil
}
```

Note: `os/signal` and `time` are already imported in `root.go` (used by `proxyCmd` and the deploy handler).

- [ ] **Step 6: Run the full build, CLI tests, and full test suite**

Run: `go build ./...`

Expected: Build succeeds.

Run: `go test ./... -v -count=1`

Expected: All PASS.

Run: `go vet ./...`

Expected: No issues.

- [ ] **Step 7: Manual smoke test (if Docker available)**

Run: `./tengiz cleanup --dry-run`

Expected output similar to:

```
[tengiz] dry-run: commands that would be executed
  docker container prune -f --filter label!=tengiz-app
  docker image prune -f
  docker network prune -f
  docker builder prune -f
```

- [ ] **Step 8: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go internal/cli/root.go
git commit -m "feat(cleanup): add periodic cleanup manager and --interval foreground loop"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md` — add `tengiz cleanup` section in the CLI Reference
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI list
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented

**Interfaces:**
- Consumes: the `tengiz cleanup` command surface from Tasks 2–3
- Produces: up-to-date user documentation and feature tracking

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

Insert after the `tengiz logs` section (which ends at line ~166, just before the `### tengiz build-logs` heading at line 168):

```markdown
### `tengiz cleanup [flags]`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--all` | Remove all unused images, not just dangling ones |
| `--containers` | Prune stopped containers |
| `--images` | Prune unused images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune the Docker build cache |
| `--app <name>` | Limit pruning to a specific app's resources |
| `--before <duration>` | Only prune resources older than this (e.g. `72h`, `1w`) |
| `--dry-run` | Print the docker commands without executing them |
| `--interval <duration>` | Run cleanup periodically in the foreground (e.g. `24h`) |

By default cleanup removes stopped non-Tengiz containers, dangling images, unused networks, and the build cache. Tengiz-managed containers (including scale-to-zero stopped ones) are always protected via the `label!=tengiz-app` filter. Tagged Tengiz images are untouched unless `--all` or `--app` is given. When `--interval` is set, the command stays in the foreground and runs cleanup on the tick; a failed run sends a `system:warning` notification.
```

- [ ] **Step 2: Add `tengiz cleanup` to `AGENTS.md`**

In the CLI section (after the `tengiz logs` line, before `tengiz run`):

```
tengiz cleanup [--all] [--volumes] [--app <name>] [--before 72h] [--dry-run] [--interval 24h] → prune unused Docker resources (Tengiz containers protected via labels)
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

1. In the P0 priority table (line 19), change the status marker from ⬜ to ✅:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

2. Add a status line to the `## Docker Housekeeping (Otomatik Temizlik)` feature section (after the `- **Why add to Tengiz:**` line at line 380):

```markdown
- **Status:** ✅ Implemented (2026-07-31)
```

3. Add a row to the `### ✅ Implemented Features (Not Pending)` table (after the Webhook row at line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-07-31) |
```

- [ ] **Step 4: Verify the docs render correctly**

Run: `grep -n "tengiz cleanup" README.md AGENTS.md`

Expected: matches in all three files (`README.md` + `AGENTS.md`); then check `docs/FUTURES_FEATURES.md` for the three edits.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark Docker Housekeeping implemented"
```

---

## Self-Review

Run this checklist with fresh eyes against `docs/FUTURES_FEATURES.md` (feature #6) before handing off for implementation.

**1. Spec coverage**

| Spec requirement | Plan task |
|---|---|
| `tengiz cleanup` command | Task 2 |
| Label-based filtering protects Tengiz containers | Task 1 (`label!=tengiz-app`), Task 2 (warning for `--app`), verified by `TestBuildContainerPruneArgs` |
| Clean unused volumes, networks, containers, images | Task 1 (per-category builders + `Prune`), Task 2 (category flags) |
| Periodic cleanup (`DockerCleanupJob` equivalent) | Task 3 (`--interval` loop + `cleanup.Manager`) |
| Helper-container safety (`CleanupHelperContainersJob` note) | Covered by `label!=tengiz-app` protection — helper `docker run --rm` containers carry the `tengiz-app` label (see `buildRunArgs`) and are excluded |

**2. Placeholder scan**

No "TBD", "TODO", "implement later", "fill in details", or "Similar to Task N" present. Every code step contains complete code; every test step contains complete test code; every run step gives the exact command and expected result.

**3. Type consistency**

- `runtime.CleanupOptions` fields `Containers, Images, Volumes, Networks, BuildCache, All bool`, `App, Before string` — used identically in Task 1 (`prune.go`, tests), Task 2 (`cleanupOptions`, `printCleanupPlan`, `printCleanupReport`), Task 3 (`cleanup.Config.Options`).
- `runtime.CleanupReport` fields `ContainersSpace, ImagesSpace, VolumesSpace, NetworksSpace, BuildCacheSpace string` — produced by `Prune`, consumed by `printCleanupReport` and the `cleanup` manager log line.
- Builder names: `BuildContainerPruneArgs`, `BuildImagePruneArgs`, `BuildVolumePruneArgs`, `BuildNetworkPruneArgs`, `BuildBuilderPruneArgs` — Task 1 exports them, Task 2 `printCleanupPlan` calls them, tests reference the same names.
- `Manager.Prune(ctx, opts) (*CleanupReport, error)` — added to the interface, `stubManager`, and `mockRTForDeploy` in the same task so nothing compiles broken mid-plan.
- `cleanup.NewWithEnv(pruner Pruner, dataDir, env string, cfg Config) *Manager` — Task 3 defines it; the CLI passes a `runtime.Manager` which satisfies `Pruner` (verified by `go build ./...`).
- Label constant used is the existing `labelKey = "tengiz-app"` from `docker.go:76` — no string drift.
