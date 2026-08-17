# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, networks, volumes, build cache) to reclaim disk space while protecting every Tengiz-managed container and image via label-based filtering.

**Architecture:** The `runtime.Manager` interface gains a `Cleanup(ctx, opts) (*CleanupReport, error)` method. A pure, exported function `runtime.BuildCleanupCommands(opts)` returns the ordered list of `docker` sub-commands to run; `dockerRuntime.Cleanup` executes them via `os/exec` (the same pattern as every other runtime method) and returns a report with per-command output. The `tengiz cleanup` cobra command wires flags (`--all`, `--volumes`, `--build-cache`, `--dry-run`) to the runtime. Protection rules: all Tengiz containers already carry the `tengiz-app=<app>` label (set in `docker.go` `Create`/`CreateVersioned`/`CreateFromImage`), so `docker container prune --filter label!=tengiz-app` never touches them; Tengiz images (`tengiz-apps/*`) are excluded from aggressive pruning via `reference!=tengiz-apps/*`, and per-app image retention remains the job of the existing `KeepLastNImages` during deploy. Periodic/scheduled execution is intentionally out of scope (future feature #57 Background Monitoring Scheduler).

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (docker CLI), existing `runtime.Manager` interface. No new external dependencies.

## Global Constraints

- No new external Go dependencies (stdlib + existing `cobra` only)
- All Tengiz-managed containers carry `tengiz-app=<app>` and `tengiz-env=<env>` labels — cleanup MUST NOT remove or stop them
- Tengiz images use `tengiz-apps/<app>:<env>-<deploymentID>` and `tengiz-apps/<app>:<env>-latest` tags — aggressive image pruning MUST NOT remove them
- Default `tengiz cleanup` touches only: stopped non-Tengiz containers, dangling images, unused networks
- `--all` additionally removes unused non-dangling images (except `tengiz-apps/*`) and all unused build cache
- `--volumes` additionally removes unused anonymous volumes (Tengiz uses bind mounts, which `docker volume prune` never removes)
- `--dry-run` prints the docker commands without executing them and MUST work when no Docker daemon is present
- Every task must keep `go build -o tengiz .` and `go test ./... -v -count=1` green
- Existing tests must continue to pass unchanged, EXCEPT the four `runtime.Manager` mock types listed in Task 2 that must gain the new `Cleanup` method

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Add `CleanupOptions`, `CleanupCommand`, `CleanupResult`, `CleanupReport` types; pure `BuildCleanupCommands()`; `dockerRuntime.Cleanup()` |
| `internal/runtime/runtime.go` | Add `Cleanup()` to the `Manager` interface + stub implementation |
| `internal/runtime/cleanup_test.go` | Unit tests for `BuildCleanupCommands()` + stub `Cleanup` |
| `internal/runtime/runtime_test.go` | (unchanged — `TestStubSatisfiesInterface` verifies the stub still satisfies the interface) |
| `internal/cli/cleanup.go` | New `tengiz cleanup` cobra command + flags |
| `internal/cli/cleanup_test.go` | CLI tests: command registration, flags, dry-run output |
| `internal/cli/root_test.go` | Add `Cleanup()` method to `mockRTForDeploy` |
| `internal/idle/idle_test.go` | Add `Cleanup()` method to its `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Cleanup()` method to its `mockRuntime` |
| `README.md` | Add `tengiz cleanup` to Features + CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Pure docker command builder (`BuildCleanupCommands`)

**Files:**
- Modify: `internal/runtime/cleanup.go` — add types + `BuildCleanupCommands()`
- Test: `internal/runtime/cleanup_test.go` — add tests

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type CleanupOptions struct { All, Volumes, BuildCache bool }`
  - `type CleanupCommand struct { Args []string }` — `Args` is the full `docker` sub-args (e.g. `["container", "prune", "-f", "--filter", "label!=tengiz-app"]`)
  - `func BuildCleanupCommands(opts CleanupOptions) []CleanupCommand`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func assertCleanupCommands(t *testing.T, got, want []CleanupCommand) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d commands, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if len(got[i].Args) != len(want[i].Args) {
			t.Fatalf("cmd[%d] args = %v, want %v", i, got[i].Args, want[i].Args)
		}
		for j := range want[i].Args {
			if got[i].Args[j] != want[i].Args[j] {
				t.Fatalf("cmd[%d].Args[%d] = %q, want %q", i, j, got[i].Args[j], want[i].Args[j])
			}
		}
	}
}

func TestBuildCleanupCommandsDefault(t *testing.T) {
	cmds := BuildCleanupCommands(CleanupOptions{})
	assertCleanupCommands(t, cmds, []CleanupCommand{
		{Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{Args: []string{"image", "prune", "-f"}},
		{Args: []string{"network", "prune", "-f"}},
	})
}

func TestBuildCleanupCommandsAll(t *testing.T) {
	cmds := BuildCleanupCommands(CleanupOptions{All: true})
	assertCleanupCommands(t, cmds, []CleanupCommand{
		{Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{Args: []string{"image", "prune", "-f", "-a", "--filter", "reference!=tengiz-apps/*"}},
		{Args: []string{"network", "prune", "-f"}},
		{Args: []string{"builder", "prune", "-f", "-a"}},
	})
}

func TestBuildCleanupCommandsVolumes(t *testing.T) {
	cmds := BuildCleanupCommands(CleanupOptions{Volumes: true})
	assertCleanupCommands(t, cmds, []CleanupCommand{
		{Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{Args: []string{"image", "prune", "-f"}},
		{Args: []string{"network", "prune", "-f"}},
		{Args: []string{"volume", "prune", "-f"}},
	})
}

func TestBuildCleanupCommandsBuildCache(t *testing.T) {
	cmds := BuildCleanupCommands(CleanupOptions{BuildCache: true})
	assertCleanupCommands(t, cmds, []CleanupCommand{
		{Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{Args: []string{"image", "prune", "-f"}},
		{Args: []string{"network", "prune", "-f"}},
		{Args: []string{"builder", "prune", "-f"}},
	})
}

func TestBuildCleanupCommandsAllVolumesBuildCache(t *testing.T) {
	cmds := BuildCleanupCommands(CleanupOptions{All: true, Volumes: true, BuildCache: true})
	assertCleanupCommands(t, cmds, []CleanupCommand{
		{Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{Args: []string{"image", "prune", "-f", "-a", "--filter", "reference!=tengiz-apps/*"}},
		{Args: []string{"network", "prune", "-f"}},
		{Args: []string{"volume", "prune", "-f"}},
		{Args: []string{"builder", "prune", "-f", "-a"}},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestBuildCleanupCommands -v -count=1`

Expected: FAIL with `undefined: CleanupOptions` / `undefined: BuildCleanupCommands`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/runtime/cleanup.go` (after the existing `KeepLastNImages` method):

```go
type CleanupOptions struct {
	All        bool
	Volumes    bool
	BuildCache bool
}

type CleanupCommand struct {
	Args []string
}

// BuildCleanupCommands returns the ordered list of docker sub-commands to run
// for the given options. Tengiz-managed containers (labeled tengiz-app) are
// always excluded, and tengiz-apps/* images are protected from aggressive
// pruning (their retention is handled by KeepLastNImages during deploy).
func BuildCleanupCommands(opts CleanupOptions) []CleanupCommand {
	cmds := []CleanupCommand{
		{Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
	}
	if opts.All {
		cmds = append(cmds, CleanupCommand{Args: []string{"image", "prune", "-f", "-a", "--filter", "reference!=tengiz-apps/*"}})
	} else {
		cmds = append(cmds, CleanupCommand{Args: []string{"image", "prune", "-f"}})
	}
	cmds = append(cmds, CleanupCommand{Args: []string{"network", "prune", "-f"}})
	if opts.Volumes {
		cmds = append(cmds, CleanupCommand{Args: []string{"volume", "prune", "-f"}})
	}
	if opts.BuildCache || opts.All {
		args := []string{"builder", "prune", "-f"}
		if opts.All {
			args = append(args, "-a")
		}
		cmds = append(cmds, CleanupCommand{Args: args})
	}
	return cmds
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestBuildCleanupCommands -v -count=1`

Expected: PASS (5 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add BuildCleanupCommands docker command builder"
```

---

### Task 2: `Cleanup` on the `Manager` interface + dockerRuntime implementation + all mocks

**Files:**
- Modify: `internal/runtime/runtime.go` — interface + stub
- Modify: `internal/runtime/cleanup.go` — `CleanupResult`, `CleanupReport`, `dockerRuntime.Cleanup`
- Modify: `internal/cli/root_test.go` — `mockRTForDeploy.Cleanup`
- Modify: `internal/idle/idle_test.go` — `mockRuntime.Cleanup`
- Modify: `internal/proxy/proxy_test.go` — `mockRuntime.Cleanup`
- Test: `internal/runtime/cleanup_test.go` — stub `Cleanup` test

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupCommand`, `BuildCleanupCommands` from Task 1
- Produces:
  - `type CleanupResult struct { Command CleanupCommand; Output string; Err error }`
  - `type CleanupReport struct { Results []CleanupResult }`
  - `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil || len(report.Results) != 0 {
		t.Fatalf("expected empty report from stub, got %+v", report)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`

Expected: FAIL with `stubManager.Cleanup undefined` (interface method missing on the stub)

- [ ] **Step 3: Add `Cleanup` to the `Manager` interface + stub**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after `Run`):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
```

And add to the stub (after the `Run` stub method):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return &CleanupReport{}, nil
}
```

- [ ] **Step 4: Add result/report types + `dockerRuntime.Cleanup`**

Add to `internal/runtime/cleanup.go` (after `BuildCleanupCommands`):

```go
type CleanupResult struct {
	Command CleanupCommand
	Output  string
	Err     error
}

type CleanupReport struct {
	Results []CleanupResult
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	cmds := BuildCleanupCommands(opts)
	report := &CleanupReport{Results: make([]CleanupResult, 0, len(cmds))}
	for _, c := range cmds {
		cmd := exec.CommandContext(ctx, "docker", c.Args...)
		out, err := cmd.CombinedOutput()
		report.Results = append(report.Results, CleanupResult{Command: c, Output: string(out), Err: err})
	}
	return report, nil
}
```

No new imports needed — `context`, `os/exec` are already imported in `cleanup.go`.

- [ ] **Step 5: Update the three remaining `Manager` mock types (compile fix)**

In `internal/cli/root_test.go`, add to `mockRTForDeploy` (after the `Run` method):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{}, nil
}
```

In `internal/idle/idle_test.go`, add to `mockRuntime` (after the `Run` method):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) { return &runtime.CleanupReport{}, nil }
```

In `internal/proxy/proxy_test.go`, add to `mockRuntime` (after the `Run` method):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) { return &runtime.CleanupReport{}, nil }
```

- [ ] **Step 6: Run all tests to verify they pass**

Run: `go test ./... -v -count=1`

Expected: PASS — including `TestStubSatisfiesInterface` (runtime), `TestMockRTForDeployImplementsManager` (cli), and the idle/proxy tests that pass mocks as `runtime.Manager`.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Cleanup method to Manager interface"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.BuildCleanupCommands`, `runtime.NewDocker()`, `runtime.Manager.Cleanup` from Tasks 1-2
- Produces: `cleanupCmd *cobra.Command` registered under `rootCmd`, with flags `--all/-a`, `--volumes`, `--build-cache`, `--dry-run`

- [ ] **Step 1: Write the failing test**

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
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"all", "volumes", "build-cache", "dry-run"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupDryRunPrintsCommands(t *testing.T) {
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all", "--volumes"})
		rootCmd.Execute()
	})
	for _, want := range []string{
		"docker container prune -f --filter label!=tengiz-app",
		"docker image prune -f -a --filter reference!=tengiz-apps/*",
		"docker network prune -f",
		"docker volume prune -f",
		"docker builder prune -f -a",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("dry-run output missing %q, got:\n%s", want, output)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestCleanupCmdFlags|TestCleanupDryRunPrintsCommands" -v -count=1`

Expected: FAIL with `cleanup command not registered: unknown command "cleanup"` and `undefined: cleanupCmd`

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, networks, volumes, build cache)",
	Long: "Removes unused Docker resources to reclaim disk space. Tengiz-managed containers " +
		"(labeled tengiz-app) and Tengiz images (tengiz-apps/*) are always protected. " +
		"Use --dry-run to preview the docker commands without running them.",
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts := runtime.CleanupOptions{All: all, Volumes: volumes, BuildCache: buildCache}

		if dryRun {
			fmt.Println("Would run the following docker commands:")
			for _, c := range runtime.BuildCleanupCommands(opts) {
				fmt.Printf("  docker %s\n", strings.Join(c.Args, " "))
			}
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		report, err := rt.Cleanup(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		for _, r := range report.Results {
			fmt.Printf("==> docker %s\n", strings.Join(r.Command.Args, " "))
			if r.Output != "" {
				fmt.Print(r.Output)
			}
			if r.Err != nil {
				fmt.Fprintf(os.Stderr, "[tengiz] cleanup error: %v\n", r.Err)
			}
		}
		fmt.Println("[tengiz] cleanup complete")
		return nil
	},
}

func init() {
	cleanupCmd.Flags().BoolP("all", "a", false, "remove all unused images (protects tengiz-apps/*) and all unused build cache")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused anonymous volumes")
	cleanupCmd.Flags().Bool("build-cache", false, "also prune the Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "print the docker commands that would run, without executing them")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestCleanupCmdFlags|TestCleanupDryRunPrintsCommands" -v -count=1`

Expected: PASS (3 tests)

- [ ] **Step 5: Verify the full suite still passes and the binary builds**

Run: `go test ./... -v -count=1 && go vet ./... && go build -o tengiz .`

Expected: PASS, no vet findings, build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation updates

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

- [ ] **Step 1: Add the feature bullet to `README.md` Features list**

After the line `- **Deployment history** — Track deploy versions with automatic rollback foundation (last 10 deployments preserved).` (README.md:20), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, images, networks, volumes, and build cache while protecting all Tengiz-managed resources.
```

- [ ] **Step 2: Add the `tengiz cleanup` section to `README.md` CLI Reference**

After the `### \`tengiz rollback <app>\`` section (ends at README.md:236, before `### \`tengiz domain\``), insert:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space. Always removes: stopped non-Tengiz containers, dangling images, and unused networks. Tengiz-managed containers (labeled `tengiz-app`) and Tengiz images (`tengiz-apps/*`) are never removed.

| Flag | Description |
|------|-------------|
| `-a`, `--all` | Also remove all unused images (excluding `tengiz-apps/*`) and all unused build cache |
| `--volumes` | Also remove unused anonymous volumes |
| `--build-cache` | Also prune the Docker build cache (dangling only unless `--all`) |
| `--dry-run` | Print the docker commands that would run, without executing them |
```

- [ ] **Step 3: Add the command to `AGENTS.md` CLI section**

After the line `tengiz rollback <app>           → rollback to previous deployment` (AGENTS.md:60), add:

```markdown
tengiz cleanup               → prune unused Docker resources (label-based, protects tengiz-app containers/images)
```

Also update the `runtime.Manager` row in the "Key architecture" table (AGENTS.md:15) by appending to it:

```
 Also: `Cleanup` for label-based `docker` resource pruning.
```

- [ ] **Step 4: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

In the Priority Ranking P0 table, change the status marker on the Docker Housekeeping row (line 19) from `⬜` to `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Then, in the "Docker Housekeeping (Otomatik Temizlik)" feature section (line 377), add a status line directly after the `- **Description:**` line:

```markdown
- **Status:** ✅ Implemented (2026-08-17)
```

- [ ] **Step 5: Verify documentation renders and build still passes**

Run: `go build -o tengiz . && tengiz cleanup --help`

Expected: build succeeds; help output lists the `cleanup` command and all four flags.

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage (feature #6 Docker Housekeeping):**
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → Task 1/2/3 prune containers, images, networks, volumes (volumes opt-in via `--volumes`)
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → `--filter label!=tengiz-app` on container prune (Task 1)
- "`tengiz cleanup` komutu eklenebilir" → Task 3 CLI command
- Image protection (`tengiz-apps/*` via `reference!=` filter + existing `KeepLastNImages`) → Task 1 Global Constraints + builder
- Build cache cleanup → `--build-cache`/`--all` (Task 1)
- "periyodik temizleme" (periodic/scheduled) → explicitly out of scope (Background Monitoring Scheduler #57); noted in Goal/Architecture

**2. Placeholder scan:** No TBD/TODO/"implement later". Every step contains exact code, exact commands, and expected output.

**3. Type consistency:**
- `CleanupOptions{All, Volumes, BuildCache bool}` — identical in Tasks 1, 2, 3
- `CleanupCommand{Args []string}` — identical in Tasks 1, 2, 3
- `CleanupReport{Results []CleanupResult}`, `CleanupResult{Command, Output, Err}` — defined Task 2, used Task 2/3
- `BuildCleanupCommands(opts CleanupOptions) []CleanupCommand` — same signature in Tasks 1, 2 (runtime), 3 (CLI dry-run)
- `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)` — same in stub, dockerRuntime, and all three test mocks
- `mockRTForDeploy`/`mockRuntime` gain one method each with identical signature in Task 2, Step 5