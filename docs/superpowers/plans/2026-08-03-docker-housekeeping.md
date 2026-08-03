# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused networks, build cache, and opt-in volumes) using label-based protection so Tengiz-managed containers are never removed.

**Architecture:** A pure helper `runtime.PruneCommands(opts)` builds the ordered list of `docker` CLI command args for each enabled category. The `runtime.Manager` interface gains a `Prune(ctx, opts)` method — the Docker implementation executes each command and aggregates output, the stub returns `""`. The CLI `tengiz cleanup` command always protects Tengiz containers via the `label!=tengiz-app` filter, exposes `--volumes` (opt-in, risky) and `--dry-run` (prints commands without executing) flags, and mirrors the repo pattern of a dedicated command file (`internal/cli/cleanup.go`).

**Tech Stack:** Go 1.26, existing `runtime.Manager` interface, Cobra (CLI), Docker CLI via `os/exec` (no Docker SDK).

## Global Constraints

- No new external dependencies — Docker is invoked via `os/exec` (repo rule)
- Tengiz-managed containers (label `tengiz-app`) MUST be protected from pruning at all times
- Container prune filter MUST be `label!=tengiz-app` so stopped versioned containers (rollback state) survive
- Volume pruning MUST be opt-in (`--volumes` flag, default off) because volumes hold persistent data
- Image pruning MUST only target dangling images (no `-a`) so tagged `tengiz-apps/<app>:<id>` rollback images are never touched
- `PruneCommands(opts)` returns commands WITHOUT the leading `docker` binary (callers prepend it)
- Command ordering MUST be deterministic: containers, images, networks, volumes, build cache
- `--dry-run` MUST NOT require Docker to be installed (returns before `runtime.NewDocker()`)
- All existing tests must continue to pass — every `runtime.Manager` test mock must gain the `Prune` method
- Commands follow repo style: `cobra.Command` var, registered in `root.go` `init()`, printed with `[tengiz]` prefix
- `go build -o tengiz .`, `go test ./... -v -count=1`, `go vet ./...` must all pass after the final task

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Add `PruneOptions` type, pure `PruneCommands(opts)` builder, `dockerRuntime.Prune` implementation |
| `internal/runtime/cleanup_test.go` | Unit tests for `PruneCommands` and stub `Prune` |
| `internal/runtime/runtime.go` | Add `Prune` to `Manager` interface + stub implementation |
| `internal/cli/cleanup.go` (create) | `tengiz cleanup` cobra command with `--dry-run` / `--volumes` flags |
| `internal/cli/cleanup_test.go` (create) | CLI tests: command registration, dry-run output, flag presence |
| `internal/cli/root.go` | Register `cleanupCmd` + define its flags in `init()` |
| `internal/cli/root_test.go` | Add `Prune` method to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Prune` method to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Prune` method to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in CLI reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: PruneOptions type + PruneCommands pure builder

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions` struct with fields `Containers, Images, Networks, Volumes, BuildCache bool`; `runtime.PruneCommands(opts PruneOptions) [][]string` returning command arg slices WITHOUT the leading `docker` binary, in the fixed order containers → images → networks → volumes → build cache

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestPruneCommandsDefault(t *testing.T) {
	cmds := PruneCommands(PruneOptions{
		Containers: true,
		Images:     true,
		Networks:   true,
		BuildCache: true,
	})
	want := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-f"},
		{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"builder", "prune", "-f"},
	}
	if len(cmds) != len(want) {
		t.Fatalf("PruneCommands() len = %d, want %d", len(cmds), len(want))
	}
	for i := range want {
		if len(cmds[i]) != len(want[i]) {
			t.Errorf("cmd[%d] = %v, want %v", i, cmds[i], want[i])
			continue
		}
		for j := range want[i] {
			if cmds[i][j] != want[i][j] {
				t.Errorf("cmd[%d][%d] = %q, want %q", i, j, cmds[i][j], want[i][j])
			}
		}
	}
}

func TestPruneCommandsAllDisabled(t *testing.T) {
	cmds := PruneCommands(PruneOptions{})
	if len(cmds) != 0 {
		t.Errorf("PruneCommands() with all disabled = %v, want empty", cmds)
	}
}

func TestPruneCommandsVolumes(t *testing.T) {
	cmds := PruneCommands(PruneOptions{Volumes: true})
	if len(cmds) != 1 {
		t.Fatalf("PruneCommands(volumes) len = %d, want 1", len(cmds))
	}
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	if strings.Join(cmds[0], " ") != strings.Join(want, " ") {
		t.Errorf("volume cmd = %v, want %v", cmds[0], want)
	}
}

func TestPruneCommandsProtectsTengizContainers(t *testing.T) {
	cmds := PruneCommands(PruneOptions{Containers: true})
	found := false
	for _, c := range cmds {
		for _, arg := range c {
			if arg == "label!=tengiz-app" {
				found = true
			}
		}
	}
	if !found {
		t.Error("container prune command missing label!=tengiz-app protection filter")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneCommands" -v -count=1`

Expected: FAIL with `undefined: PruneOptions` and `undefined: PruneCommands`

- [ ] **Step 3: Write the minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
}

// PruneCommands returns the ordered docker CLI command args (without the
// leading "docker" binary) for each enabled prune category. Tengiz-managed
// containers (label tengiz-app) are always protected from container pruning.
func PruneCommands(opts PruneOptions) [][]string {
	var cmds [][]string
	if opts.Containers {
		cmds = append(cmds, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.Images {
		cmds = append(cmds, []string{"image", "prune", "-f"})
	}
	if opts.Networks {
		cmds = append(cmds, []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.Volumes {
		cmds = append(cmds, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.BuildCache {
		cmds = append(cmds, []string{"builder", "prune", "-f"})
	}
	return cmds
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneCommands" -v -count=1`

Expected: PASS for all four `TestPruneCommands*` tests

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add PruneCommands builder for docker housekeeping"
```

---

### Task 2: Add Prune to Manager interface + implementations

**Files:**
- Modify: `internal/runtime/runtime.go` — add `Prune` to `Manager` interface + stub method
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.Prune` implementation
- Modify: `internal/runtime/cleanup_test.go` — add `TestStubPrune`
- Modify: `internal/cli/root_test.go:98-100` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:33-35` — add `Prune` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:32-34` — add `Prune` to `mockRuntime`

**Interfaces:**
- Consumes: `runtime.PruneOptions` + `runtime.PruneCommands` from Task 1
- Produces: `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (string, error)` — raw combined docker output; stub returns `""`, nil

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	out, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if out != "" {
		t.Errorf("Prune() output = %q, want empty", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPrune" -v -count=1`

Expected: FAIL (compile) with `stubManager does not implement Manager (missing method Prune)`

- [ ] **Step 3: Implement the interface, stub, and docker implementation**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after the `KeepLastNImages` line, `runtime.go:36`):

```go
	Prune(ctx context.Context, opts PruneOptions) (string, error)
```

In `internal/runtime/runtime.go`, add to the stub (after the `KeepLastNImages` stub method, `runtime.go:117-119`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (string, error) {
	return "", nil
}
```

In `internal/runtime/cleanup.go`, append:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (string, error) {
	var output strings.Builder
	for _, args := range PruneCommands(opts) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		output.Write(out)
		if err != nil {
			return output.String(), fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
	}
	return output.String(), nil
}
```

Add the `Prune` method to every test mock that implements `runtime.Manager`, or the `cli`, `proxy`, and `idle` packages will fail to compile:

In `internal/cli/root_test.go`, after the `KeepLastNImages` mock method (line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (string, error) { return "", nil }
```

In `internal/proxy/proxy_test.go`, after the `KeepLastNImages` mock method (line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (string, error) { return "", nil }
```

In `internal/idle/idle_test.go`, after the `KeepLastNImages` mock method (line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (string, error) { return "", nil }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/... -count=1`

Expected: PASS for all packages (runtime, cli, proxy, idle)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add Prune method to runtime Manager interface"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go:46` — register `cleanupCmd` next to the other top-level commands

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneCommands`, `runtime.Manager.Prune`
- Produces: `cleanupCmd *cobra.Command` registered as `tengiz cleanup` with flags `--dry-run` (bool) and `--volumes` (bool)

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdDryRunPrintsCommands(t *testing.T) {
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("cleanup --dry-run: %v", err)
		}
	})
	for _, want := range []string{
		"docker container prune -f --filter label!=tengiz-app",
		"docker image prune -f",
		"docker network prune -f --filter label!=tengiz-app",
		"docker builder prune -f",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("cleanup --dry-run output missing %q; got:\n%s", want, output)
		}
	}
	if strings.Contains(output, "docker volume prune") {
		t.Error("volume prune should be disabled by default")
	}
}

func TestCleanupCmdDryRunVolumes(t *testing.T) {
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--volumes"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("cleanup --dry-run --volumes: %v", err)
		}
	})
	if !strings.Contains(output, "docker volume prune -f --filter label!=tengiz-app") {
		t.Errorf("cleanup --dry-run --volumes output missing volume prune; got:\n%s", output)
	}
}

func TestCleanupCmdHelpListsFlags(t *testing.T) {
	help := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--help"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("cleanup --help: %v", err)
		}
	})
	for _, flag := range []string{"--dry-run", "--volumes"} {
		if !strings.Contains(help, flag) {
			t.Errorf("cleanup help missing flag %q", flag)
		}
	}
}
```

Note: `captureOutput` already exists in `internal/cli/root_test.go` (same package, so it is shared).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCmd" -v -count=1`

Expected: FAIL — `TestCleanupCmdRegistered` with `cleanup command not found: unknown command "cleanup"`

- [ ] **Step 3: Write the minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Prune stopped non-Tengiz containers, dangling images, unused networks,
and the Docker build cache to reclaim disk space.

Containers managed by Tengiz (labeled tengiz-app) are always protected and
are never removed.

Volumes are only pruned when explicitly requested with --volumes, because
they may contain persistent application data.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		pruneVolumes, _ := cmd.Flags().GetBool("volumes")

		opts := runtime.PruneOptions{
			Containers: true,
			Images:     true,
			Networks:   true,
			Volumes:    pruneVolumes,
			BuildCache: true,
		}

		if dryRun {
			for _, c := range runtime.PruneCommands(opts) {
				fmt.Println(strings.Join(append([]string{"docker"}, c...), " "))
			}
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		out, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return err
		}
		if out != "" {
			fmt.Print(out)
		}
		fmt.Println("[tengiz] cleanup complete")
		return nil
	},
}
```

In `internal/cli/root.go` `init()`, after `rootCmd.AddCommand(rmCmd)` (line 44), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

and after `rootCmd.AddCommand(runCmd)` (line 67), add:

```go
	cleanupCmd.Flags().Bool("dry-run", false, "print the docker commands that would be executed")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes (may delete persistent data)")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCmd" -v -count=1`

Expected: PASS for all four `TestCleanupCmd*` tests

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Documentation update + full verification

**Files:**
- Modify: `README.md` — add `tengiz cleanup` section to CLI reference after the `tengiz rollback` section (line 230-236, before `### tengiz domain` at line 238)
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented

**Interfaces:**
- Consumes: the final CLI surface from Task 3
- Produces: accurate user documentation

- [ ] **Step 1: Document `tengiz cleanup` in README.md**

Insert after line 236 (end of the `tengiz rollback` section) and before line 238 (`### tengiz domain`):

```markdown
### `tengiz cleanup`

Clean up unused Docker resources to reclaim disk space: stopped containers, dangling images, unused networks, and the Docker build cache.

Containers managed by Tengiz (labeled `tengiz-app`) are always protected and never removed. Volumes hold persistent data, so they are only pruned when explicitly requested with `--volumes`.

| Flag | Description |
|------|-------------|
| `--dry-run` | Print the `docker` commands that would be executed, without running them |
| `--volumes` | Also remove unused volumes (may delete persistent data) |

```bash
tengiz cleanup          # prune containers, images, networks, build cache
tengiz cleanup --volumes
tengiz cleanup --dry-run
```
```

- [ ] **Step 2: Mark feature #6 as implemented in docs/FUTURES_FEATURES.md**

In the P0 table (line 19), change the `# 6` row marker from `⬜` to `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the `## Docker Housekeeping (Otomatik Temizlik)` feature section (line 377-381), add a status line after the `**Detected:** 2026-07-14` line:

```markdown
- **Status:** ✅ Implemented (2026-08-03)
```

- [ ] **Step 3: Run full verification**

Run:

```bash
go build -o tengiz .
go test ./... -v -count=1
go vet ./...
```

Expected: build succeeds, all tests PASS (no `-count=1` skips caching), `go vet` reports no issues.

- [ ] **Step 4: Manually smoke-test the command (optional, requires Docker)**

If Docker is available:

```bash
./tengiz cleanup --dry-run
# expect the four docker commands printed, no execution
```

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage (FUTURES_FEATURES.md #6 Docker Housekeeping):**
- "Label-based `docker system prune`" → Task 1/2/3 implement per-category prune with `label!=tengiz-app` protection (safest form of the label requirement; avoids `docker system prune`'s lack of reliable `label!=` forwarding). Covered.
- "`tengiz cleanup` komutu" → Task 3 adds the command. Covered.
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → containers/images/networks prune by default; volumes via opt-in `--volumes`; build cache included. Covered.
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → `label!=tengiz-app` on both container and network prune. Covered.
- "CleanupHelperContainersJob ile yardımcı container'ları temizler" → one-off `tengiz run` containers use `--rm` (self-cleaning); any leftover stopped non-Tengiz containers are removed by the container prune. Covered.
- Periodic/scheduled execution is NOT in this plan — it belongs to feature #57 (Background Monitoring Scheduler); noted as out of scope intentionally. Manual `tengiz cleanup` matches the P0 rationale's concrete deliverable.

**2. Placeholder scan:** Every code step contains complete, compilable code with exact file paths and expected test output. No "TBD", "add validation", or "similar to Task N" references remain.

**3. Type consistency:**
- `PruneOptions` (Containers/Images/Networks/Volumes/BuildCache bool) is defined once in Task 1 and used identically in Tasks 2 and 3.
- `PruneCommands(opts) [][]string` returns args WITHOUT `docker`; the dockerRuntime.Prune prepends `docker`, the CLI dry-run prepends `docker` too. Consistent.
- `Prune(ctx, opts) (string, error)` signature is identical across interface, stub, dockerRuntime, and all three test mocks.
- Flag names `--dry-run` and `--volumes` are consistent between CLI tests, the command implementation, and README.
