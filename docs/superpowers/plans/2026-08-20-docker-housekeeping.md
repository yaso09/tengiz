# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused networks) with label-based protection for Tengiz-managed containers, plus a disk-usage report.

**Architecture:** New pruning methods live on the existing `runtime.Manager` interface (alongside the existing `RemoveImage`/`KeepLastNImages` in `internal/runtime/cleanup.go`), so all Docker exec logic stays in the runtime package and the CLI stays a thin cobra wrapper. The implementation shells out to `docker system prune` with the `--filter label!=tengiz-app` protection filter (Tengiz-managed containers always carry the `tengiz-app` label). Pure arg-builder functions (`buildPruneArgs`, `buildBuilderPruneArgs`) are unit-tested without Docker, mirroring the existing `buildLogArgs`/`buildRunArgs` pattern. A `tengiz cleanup` cobra command (new file `internal/cli/cleanup.go`) exposes `--dry-run`, `--all`, `--volumes`, `--build-cache` flags and prints a `docker system df` summary.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (docker CLI — no Docker SDK, matching repo convention). No new external dependencies.

## Global Constraints

- No new external dependencies (stdlib + existing `cobra` only)
- Docker must be present at runtime; the CLI shells out to `docker` via `os/exec` (existing convention, no Docker SDK)
- Tengiz-managed containers always carry the `tengiz-app=<app>` label and `tengiz-env=<env>` label (set in `internal/runtime/docker.go`) — cleanup MUST never remove them
- Protection is enforced by passing `--filter label!=tengiz-app` to `docker system prune` (prunes only resources WITHOUT the label)
- Default cleanup removes: stopped non-Tengiz containers, dangling images, unused networks. It NEVER removes volumes or build cache unless explicitly requested
- Rollback depends on retained images (`KeepLastNImages`, keeps 5 per app). Dangling-only image pruning (default) never touches them; `--all` (`-a`) is an explicit, documented aggressive option
- Cleanup is a system-wide operation — it is NOT scoped by the global `--env` flag
- `docker system prune` has no `--dry-run` flag — dry-run is implemented by printing the exact commands that would run plus a `docker system df` summary, without executing
- Adding methods to `runtime.Manager` requires updating all three implementers: `dockerRuntime`, `stubManager`, and `mockRTForDeploy` in `internal/cli/root_test.go` (the repo's existing compile-time interface check `TestMockRTForDeployImplementsManager` will fail otherwise)
- Existing tests must continue to pass; follow the existing pure-function test pattern (`resourceArgs`, `buildLogArgs`, `buildRunArgs`)
- Periodically scheduled cleanup (a Coolify `DockerCleanupJob` equivalent) is OUT OF SCOPE — it belongs to future scheduler features (#57 Background Monitoring Scheduler). This plan delivers the `tengiz cleanup` command named in the feature rationale
- Per AGENTS.md: create branch `feat/docker-housekeeping` before implementing; commit after each task with tests green

---

## File Structure

| File | Responsibility | Action |
|------|---------------|--------|
| `internal/runtime/cleanup.go` | Add `PruneOptions`, `PruneReport`, `buildPruneArgs`, `buildBuilderPruneArgs`, `pruneCommands`, `dockerRuntime.Prune`, `dockerRuntime.DiskUsage` | Modify |
| `internal/runtime/runtime.go` | Add `Prune` + `DiskUsage` to `Manager` interface + `stubManager` implementations | Modify |
| `internal/runtime/cleanup_test.go` | Tests: arg builders (table-driven), stub `Prune`/`Prune` dry-run/`DiskUsage` | Modify |
| `internal/cli/cleanup.go` | New `tengiz cleanup` cobra command + flag registration in `init()` | Create |
| `internal/cli/root_test.go` | Add `Prune`/`DiskUsage` to `mockRTForDeploy` so it keeps satisfying `runtime.Manager` | Modify |
| `internal/cli/cleanup_test.go` | Tests: command registration + flags + flag-parsing via RunE override (pattern: `TestLogsCmdWithFlags`) | Create |
| `README.md` | Add `### tengiz cleanup` section after the `tengiz rm <app>` section | Modify |
| `AGENTS.md` | Add `tengiz cleanup` line to the CLI command list | Modify |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as ✅ Implemented (2026-08-20); add row to the implemented table | Modify |

No package-level restructure: the existing `internal/runtime/cleanup.go` already owns image cleanup, so housekeeping methods belong there. The CLI gets its own file, matching `preview.go` / `secret_rotate.go`.

---

### Task 1: Prune argument builders (pure functions) + tests

**Files:**
- Modify: `internal/runtime/cleanup.go:59` (append after the `KeepLastNImages` method)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `type PruneOptions struct { All bool; Volumes bool; BuildCache bool; DryRun bool }`, `type PruneReport struct { DryRun bool; Commands []string; Output string }`, `buildPruneArgs(opts PruneOptions) []string`, `buildBuilderPruneArgs() []string` — consumed by Task 2 (`dockerRuntime.Prune`) and Task 3 (`tengiz cleanup` command)

- [ ] **Step 0: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
import (
	"context"
	"reflect"
	"testing"
)

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     PruneOptions
		expected []string
	}{
		{
			name:     "default",
			opts:     PruneOptions{},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all",
			opts:     PruneOptions{All: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a"},
		},
		{
			name:     "volumes",
			opts:     PruneOptions{Volumes: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "--volumes"},
		},
		{
			name:     "all and volumes",
			opts:     PruneOptions{All: true, Volumes: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a", "--volumes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneArgs(tt.opts)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("buildPruneArgs() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildBuilderPruneArgs(t *testing.T) {
	expected := []string{"builder", "prune", "-f"}
	got := buildBuilderPruneArgs()
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("buildBuilderPruneArgs() = %v, want %v", got, expected)
	}
}
```

Note: the existing `cleanup_test.go` already imports `context` and `testing` — merge the new imports into the existing `import ( ... )` block instead of adding a second block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestBuildBuilderPruneArgs" -v -count=1`

Expected: FAIL with `undefined: PruneOptions`, `undefined: buildPruneArgs`, `undefined: buildBuilderPruneArgs`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
type PruneOptions struct {
	All        bool // remove all unused images (-a), volumes, and build cache
	Volumes    bool // also remove unused volumes
	BuildCache bool // also prune the Docker build cache
	DryRun     bool // report commands without executing them
}

type PruneReport struct {
	DryRun   bool
	Commands []string // docker commands run (or that would run in dry-run)
	Output   string   // combined stdout/stderr from executed commands
}

func buildPruneArgs(opts PruneOptions) []string {
	args := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	return args
}

func buildBuilderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestBuildBuilderPruneArgs" -v -count=1`

Expected: PASS (both tests, 5 subtests total)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add docker prune argument builders"
```

---

### Task 2: Add `Prune` and `DiskUsage` to the runtime Manager

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (Manager interface), `internal/runtime/runtime.go:113-119` (stub area)
- Modify: `internal/runtime/cleanup.go` (dockerRuntime implementation)
- Modify: `internal/cli/root_test.go:69-107` (`mockRTForDeploy`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport`, `buildPruneArgs`, `buildBuilderPruneArgs` (from Task 1)
- Produces: `Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)` and `Manager.DiskUsage(ctx context.Context) (string, error)` — consumed by Task 3 (`tengiz cleanup` RunE). `stubManager.Prune` returns a report with `DryRun: opts.DryRun` and `Commands` populated; `stubManager.DiskUsage` returns `"stub disk usage"`.

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report == nil {
		t.Fatal("Prune() returned nil report")
	}
	if report.DryRun {
		t.Error("DryRun = true, want false")
	}
}

func TestStubPruneDryRun(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune(dry-run) error = %v", err)
	}
	if !report.DryRun {
		t.Error("DryRun = false, want true")
	}
	if len(report.Commands) == 0 {
		t.Error("expected non-empty Commands in dry-run report")
	}
}

func TestStubPruneCommandsIncludeBuildCache(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{BuildCache: true, DryRun: true})
	if err != nil {
		t.Fatalf("Prune(build-cache) error = %v", err)
	}
	if len(report.Commands) != 2 {
		t.Errorf("expected 2 commands (system prune + builder prune), got %v", report.Commands)
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	usage, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if usage == "" {
		t.Error("expected non-empty disk usage string")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestStubDiskUsage" -v -count=1`

Expected: FAIL to compile with `cannot use m (type Manager) as type Manager: missing method Prune` — the interface additions do not exist yet. (If the package compiles because the interface wasn't extended yet, run the full suite after Step 3 to confirm.)

- [ ] **Step 3: Write minimal implementation**

3a. Extend the interface in `internal/runtime/runtime.go` (add before the closing `}` of the `Manager` interface, after the `Run` method):

```go
type Manager interface {
	Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
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
	Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)
	DiskUsage(ctx context.Context) (string, error)
}
```

3b. Add the stub implementations in `internal/runtime/runtime.go` (after the `KeepLastNImages` stub method). Add `"strings"` to the existing import block:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	cmds := []string{fmt.Sprintf("docker %s", strings.Join(buildPruneArgs(opts), " "))}
	if opts.BuildCache {
		cmds = append(cmds, fmt.Sprintf("docker %s", strings.Join(buildBuilderPruneArgs(), " ")))
	}
	return &PruneReport{DryRun: opts.DryRun, Commands: cmds}, nil
}

func (m *stubManager) DiskUsage(ctx context.Context) (string, error) {
	return "stub disk usage", nil
}
```

3c. Add the dockerRuntime implementations in `internal/runtime/cleanup.go` (after the `buildBuilderPruneArgs` function from Task 1):

```go
func (r *dockerRuntime) pruneCommands(opts PruneOptions) []string {
	cmds := []string{fmt.Sprintf("docker %s", strings.Join(buildPruneArgs(opts), " "))}
	if opts.BuildCache {
		cmds = append(cmds, fmt.Sprintf("docker %s", strings.Join(buildBuilderPruneArgs(), " ")))
	}
	return cmds
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	report := &PruneReport{DryRun: opts.DryRun, Commands: r.pruneCommands(opts)}
	if opts.DryRun {
		return report, nil
	}

	args := buildPruneArgs(opts)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	report.Output = string(out)
	if err != nil {
		return report, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}

	if opts.BuildCache {
		bargs := buildBuilderPruneArgs()
		bcmd := exec.CommandContext(ctx, "docker", bargs...)
		bout, berr := bcmd.CombinedOutput()
		report.Output += "\n" + string(bout)
		if berr != nil {
			return report, fmt.Errorf("docker %s: %w\n%s", strings.Join(bargs, " "), berr, string(bout))
		}
	}
	return report, nil
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

3d. Keep `mockRTForDeploy` in `internal/cli/root_test.go` satisfying the interface (add after the `Run` method, ~line 100):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) { return nil, nil }
func (m *mockRTForDeploy) DiskUsage(ctx context.Context) (string, error) { return "", nil }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... -count=1`

Expected: PASS — all runtime tests (existing + new) and all cli tests (including the compile-time `TestMockRTForDeployImplementsManager`) pass. No changes needed to `health`, `idle`, `proxy`, `preview`, or `gitdeploy` — they consume `runtime.Manager` but never implement it.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Prune and DiskUsage to runtime manager"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions{All, Volumes, BuildCache, DryRun}`, `runtime.PruneReport{Commands, Output}`, `Manager.Prune`, `Manager.DiskUsage` (Tasks 1–2)
- Produces: package-level `var cleanupCmd = &cobra.Command{...}` registered via `rootCmd.AddCommand(cleanupCmd)` in `init()`; flags `--dry-run`, `--all`, `--volumes`, `--build-cache` — consumed by Task 4 (docs) and by the tests in this task

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"dry-run", "all", "volumes", "build-cache"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	var called bool

	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		if !dryRun || !all || !volumes || !buildCache {
			t.Errorf("flags not parsed: dry-run=%v all=%v volumes=%v build-cache=%v", dryRun, all, volumes, buildCache)
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all", "--volumes", "--build-cache"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupCmdFlagParsing" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd` (file does not exist yet).

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: "Removes stopped non-Tengiz containers, dangling images, and unused networks.\n" +
		"Tengiz-managed containers (labeled tengiz-app) are always protected.\n" +
		"Volumes and build cache are only removed when explicitly requested.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.PruneOptions{All: all, Volumes: volumes, BuildCache: buildCache, DryRun: dryRun}

		if dryRun {
			report, err := rt.Prune(context.Background(), opts)
			if err != nil {
				return err
			}
			fmt.Println("DRY RUN — nothing will be removed. Commands that would run:")
			for _, c := range report.Commands {
				fmt.Printf("  %s\n", c)
			}
			usage, usageErr := rt.DiskUsage(context.Background())
			if usageErr == nil {
				fmt.Println("\nCurrent disk usage:")
				fmt.Print(usage)
			}
			return nil
		}

		report, err := rt.Prune(context.Background(), opts)
		if err != nil {
			return err
		}
		fmt.Print(report.Output)
		usage, usageErr := rt.DiskUsage(context.Background())
		if usageErr == nil {
			fmt.Println("\nDisk usage after cleanup:")
			fmt.Print(usage)
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("all", false, "also remove all unused images, volumes, and build cache")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	cleanupCmd.Flags().Bool("build-cache", false, "also prune the Docker build cache")
	rootCmd.AddCommand(cleanupCmd)
}
```

Note: `init()` runs once per package init in order — `rootCmd` is already defined by the time this file's `init()` runs, and registration from a dedicated `init()` matches the existing `internal/cli/preview.go` pattern.

- [ ] **Step 4: Run tests and build to verify**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupCmdFlagParsing" -v -count=1`
Expected: PASS (both tests)

Run: `go build -o tengiz . && ./tengiz cleanup --help`
Expected: binary builds; help shows `--all`, `--build-cache`, `--dry-run`, `--volumes` flags and `Short` description

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation updates

**Files:**
- Modify: `README.md` (insert `### tengiz cleanup` after the `tengiz rm <app>` section, after line 228, before `### tengiz rollback <app>`)
- Modify: `AGENTS.md` (CLI section, after the `tengiz stop/start/rm` line, ~line 47)
- Modify: `docs/FUTURES_FEATURES.md` (mark #6 implemented)

**Interfaces:**
- Consumes: `tengiz cleanup` command surface from Task 3
- Produces: docs reflecting the new command; no code interfaces

- [ ] **Step 1: Update `README.md`**

Insert this section between the `tengiz rm <app>` section (ends line 228) and the `### tengiz rollback <app>` heading (line 230):

```markdown
### `tengiz cleanup`

Reclaim disk space by pruning unused Docker resources. Removes stopped non-Tengiz containers, dangling images, and unused networks. Tengiz-managed containers (labeled `tengiz-app`) are always protected.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show the commands that would run and current disk usage without removing anything |
| `--all` | Also remove all unused images, volumes, and build cache |
| `--volumes` | Also remove unused volumes |
| `--build-cache` | Also prune the Docker build cache |

By default only stopped non-Tengiz containers, dangling images, and unused networks are removed — safe for rollback, since retained deployment images are untouched. `--volumes` removes unreferenced named volumes and `--build-cache` prunes BuildKit cache layers.
```

- [ ] **Step 2: Update `AGENTS.md`**

Add this line to the CLI command list (after the `tengiz stop/start/rm` line):

```
tengiz cleanup [--all] [--volumes] [--build-cache] [--dry-run] → prune unused Docker resources (protects labeled Tengiz containers)
```

- [ ] **Step 3: Update `docs/FUTURES_FEATURES.md`**

3a. Mark feature #6 implemented in the P0 table (line 19). Change:

```
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

3b. Add a row to the "✅ Implemented Features" table (after the `Webhook ile Otomatik Deploy` row, ~line 253):

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-20) |
```

- [ ] **Step 4: Run the full verification suite**

Run: `go vet ./...`
Expected: no output (clean)

Run: `go build -o tengiz .`
Expected: builds successfully

Run: `go test ./... -v -count=1`
Expected: PASS — all packages, including existing `runtime`, `cli`, `health`, `idle`, `proxy`, `preview`, `gitdeploy`, `config`, `notify`, `secrets`, `encrypt`, `builder`, `types` tests. Confirm no regressions from the interface additions.

- [ ] **Step 5: Manual smoke test (optional, requires Docker)**

If Docker is available on the machine, verify label protection end-to-end:

```bash
docker run -d --name cleanup-probe --label tengiz-app=probe alpine sleep 300
docker stop cleanup-probe
./tengiz cleanup --dry-run
./tengiz cleanup
docker ps -a --filter name=cleanup-probe
docker rm -f cleanup-probe
```

Expected: `--dry-run` prints the prune commands + `docker system df`; the real run completes; the `cleanup-probe` container still exists afterwards (protected by its `tengiz-app` label).

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Verification Checklist (run before finishing)

```bash
go vet ./...
go build -o tengiz .
go test ./... -v -count=1
```

All must pass. Then the feature branch `feat/docker-housekeeping` is ready for merge.