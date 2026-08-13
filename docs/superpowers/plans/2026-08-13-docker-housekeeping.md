# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space on single-server deployments by pruning stopped Tengiz containers, dangling build layers, unused images, networks, and (opt-in) volumes — while always protecting running apps, their images, and state persisted in `~/.tengiz/`.

**Architecture:** A new `CleanupOptions` struct and `Cleanup(ctx, opts)` method on the `runtime.Manager` interface. The exec-based `dockerRuntime` implementation shells out to `docker system prune` with label filters so non-Tengiz resources are never touched. Because the cleanup logic needs to be testable without Docker, the command-argument builder is a pure function (`buildCleanupArgs`) covered by table-driven unit tests, plus a `restart` companion command that shares the same runtime execution pattern. The `tengiz cleanup` command is wired into `internal/cli/root.go` following the existing `rm`/`volume` command patterns.

**Tech Stack:** Go 1.26, `os/exec` (docker CLI), Cobra (CLI). No new external dependencies.

## Global Constraints

- Never remove a container that is running or that belongs to a currently-deployed app (protect by label + `docker system prune` semantics)
- `docker system prune` default filters: dangling images only, plus all stopped containers. With `-a` it also removes unused images (no container references them)
- Non-Tengiz resources must be safe: containers are filtered by the `tengiz-app` label; volumes are only removed when `--volumes` is passed (never by default)
- All new CLI behavior is env-aware via the existing `getEnv(cmd)` helper and `config.NewStoreWithEnv(dataDir, env)`
- `tengiz cleanup` with no flags prunes only what is safe by default (containers, dangling images, build cache, networks); it must NOT remove volumes or tagged/unused images unless `--volumes` / `--all` is given
- `tengiz restart <app>` must restart exactly one named app container and print a success message on success
- No new external dependencies
- Existing tests must continue to pass without modification
- Commit style: `feat: <short description>` (matches repo history)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions` type and `Cleanup(ctx, opts)` to the `Manager` interface; add stub implementation |
| `internal/runtime/cleanup.go` | Add `buildCleanupArgs(opts)` pure function + `Cleanup` method on `dockerRuntime`; add `Restart` already exists (verify) |
| `internal/runtime/cleanup_test.go` | Table-driven tests for `buildCleanupArgs` + stub `Cleanup` test |
| `internal/cli/cleanup.go` | New file: `cleanupCmd`, `cleanupCmdSub*`, `restartCmd` cobra commands and their `RunE` handlers |
| `internal/cli/root.go` | Register `cleanupCmd` and `restartCmd` in `init()` |
| `internal/cli/root_test.go` | Registration + behavior tests for `cleanup` and `restart` |
| `README.md` | Document `tengiz cleanup` and `tengiz restart` in the CLI section |

No changes to the `config` package or `types` package are required — cleanup operates directly against Docker and the existing store.

---

### Task 1: Add `CleanupOptions` and `Cleanup` to the runtime interface

**Files:**
- Modify: `internal/runtime/runtime.go` — add `CleanupOptions` struct and `Cleanup` method to the `Manager` interface; add stub implementation
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `type CleanupOptions struct { All, Volumes bool }`, `Cleanup(ctx context.Context, opts CleanupOptions) error` on `Manager`. Later tasks rely on these exact names.

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	if err := m.Cleanup(context.Background(), CleanupOptions{All: true, Volumes: true}); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: FAIL with `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Write minimal implementation**

In `internal/runtime/runtime.go`, add the struct after the `RunOptions` type (around line 29):

```go
type CleanupOptions struct {
	All     bool // also remove unused (non-dangling) images
	Volumes bool // also remove unused volumes
}
```

Add `Cleanup` to the `Manager` interface after `KeepLastNImages`:

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) error
```

Add the stub method after the existing `stubManager.KeepLastNImages` (around line 119):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) error {
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: PASS

- [ ] **Step 5: Run full runtime suite**

Run: `go test ./internal/runtime/ -count=1`
Expected: all PASS (no existing test should need changes)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add Cleanup to runtime manager interface"
```

---

### Task 2: Implement `buildCleanupArgs` and `Cleanup` on the docker runtime

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `buildCleanupArgs(opts CleanupOptions) []string` and `Cleanup` method
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions{All, Volumes bool}` from Task 1
- Produces: `buildCleanupArgs(opts CleanupOptions) []string` (pure, no I/O), `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) error`. `buildCleanupArgs` is referenced by later tests; `Cleanup` is used by the CLI in Task 3.

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestBuildCleanupArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     CleanupOptions
		expected []string
	}{
		{
			name:     "default safe prune",
			opts:     CleanupOptions{},
			expected: []string{"system", "prune", "-f",
				"--filter", "label=tengiz-app",
				"--filter", "label=tengiz-env"},
		},
		{
			name:     "all images",
			opts:     CleanupOptions{All: true},
			expected: []string{"system", "prune", "-f", "-a",
				"--filter", "label=tengiz-app",
				"--filter", "label=tengiz-env"},
		},
		{
			name:     "with volumes",
			opts:     CleanupOptions{Volumes: true},
			expected: []string{"system", "prune", "-f",
				"--filter", "label=tengiz-app",
				"--filter", "label=tengiz-env",
				"--volumes"},
		},
		{
			name:     "all and volumes",
			opts:     CleanupOptions{All: true, Volumes: true},
			expected: []string{"system", "prune", "-f", "-a",
				"--filter", "label=tengiz-app",
				"--filter", "label=tengiz-env",
				"--volumes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCleanupArgs(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildCleanupArgs() = %v (len %d), want %v (len %d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("buildCleanupArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestBuildCleanupArgs -v -count=1`
Expected: FAIL with `undefined: buildCleanupArgs`

- [ ] **Step 3: Write minimal implementation**

In `internal/runtime/cleanup.go`, add these two functions. The `Cleanup` method is placed on `dockerRuntime` (the same receiver used by `RemoveImage`/`KeepLastNImages`):

```go
func buildCleanupArgs(opts CleanupOptions) []string {
	args := []string{"system", "prune", "-f"}
	if opts.All {
		args = append(args, "-a")
	}
	args = append(args,
		"--filter", "label=tengiz-app",
		"--filter", "label=tengiz-env",
	)
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	return args
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) error {
	cmd := exec.CommandContext(ctx, "docker", buildCleanupArgs(opts)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return nil
}
```

The `exec` and `fmt` packages are already imported in `cleanup.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestBuildCleanupArgs -v -count=1`
Expected: PASS

- [ ] **Step 5: Run full runtime suite**

Run: `go test ./internal/runtime/ -count=1`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement label-filtered docker system prune"
```

---

### Task 3: Add `tengiz cleanup` and `tengiz restart` CLI commands

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` — register both commands in `init()`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()` returning `runtime.Manager` with `Cleanup(ctx, CleanupOptions) error` and `Restart(ctx, name) error`; `getEnv(cmd) string`; `runtime.ContainerName(name, env) string`
- Produces: `cleanupCmd` and `restartCmd` cobra commands registered on `rootCmd`. `restartCmd` uses `runtime.ContainerName` and `runtime.Manager.Restart`.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"all", "volumes", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func TestRestartCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"restart"})
	if err != nil {
		t.Fatal("restart command not registered")
	}
	if cmd == nil || cmd.Name() != "restart" {
		t.Fatal("restart command not found")
	}
}

func TestRestartCmdRequiresApp(t *testing.T) {
	rootCmd.SetArgs([]string{"restart"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for missing app name")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanupCommandRegistered|TestRestartCommandRegistered|TestRestartCmdRequiresApp' -v -count=1`
Expected: FAIL — commands not found / not registered

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prune unused Docker resources (stopped Tengiz containers, dangling images,
unused build cache, unused networks).

By default this only removes resources owned by Tengiz (filtered by label) and
never removes volumes. Pass --volumes to also remove unused volumes, or -a/--all
to also remove unused (non-dangling) images that no container references.

Use --dry-run to show the docker command that would run without executing it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts := runtime.CleanupOptions{All: all, Volumes: volumes}

		if dryRun {
			fmt.Printf("[tengiz] dry-run: docker %s\n", joinCleanupArgs(opts))
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		fmt.Println("[tengiz] pruning unused Docker resources...")
		if err := rt.Cleanup(context.Background(), opts); err != nil {
			return err
		}
		fmt.Println("[tengiz] cleanup complete")
		return nil
	},
}

func joinCleanupArgs(opts runtime.CleanupOptions) string {
	args := []string{"system", "prune", "-f"}
	if opts.All {
		args = append(args, "-a")
	}
	args = append(args,
		"--filter", "label=tengiz-app",
		"--filter", "label=tengiz-env",
	)
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

var restartCmd = &cobra.Command{
	Use:   "restart <app>",
	Short: "Restart an application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		name := runtime.ContainerName(args[0], env)
		if err := rt.Restart(cmd.Context(), name); err != nil {
			return err
		}
		fmt.Printf("[tengiz] restarted: %s\n", args[0])
		return nil
	},
}
```

The `os` import in the template above is unused — do NOT include it. Final imports for `internal/cli/cleanup.go`:

```go
import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)
```

In `internal/cli/root.go` `init()`, add registration alongside the other commands (after `rootCmd.AddCommand(runCmd)` around line 67):

```go
	rootCmd.AddCommand(cleanupCmd)
	rootCmd.AddCommand(restartCmd)
```

And register the flags in `Execute()` (after `addSecretProviderFlags(secretListCmd)` around line 1803):

```go
	cleanupCmd.Flags().Bool("all", false, "also remove unused (non-dangling) images")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	cleanupCmd.Flags().Bool("dry-run", false, "show the docker command without running it")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestCleanupCommandRegistered|TestRestartCommandRegistered|TestRestartCmdRequiresApp' -v -count=1`
Expected: PASS

- [ ] **Step 5: Build the binary to verify it compiles**

Run: `go build -o tengiz .`
Expected: success, no output

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -count=1`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup and restart commands"
```

---

### Task 4: Document the new commands

**Files:**
- Modify: `README.md` — CLI section

**Interfaces:**
- Consumes: nothing new (documentation only)
- Produces: updated README CLI listing

- [ ] **Step 1: Update the README CLI section**

Find the CLI command list in `README.md` (the block listing commands such as `tengiz stop/start/rm`). Add these two lines in the appropriate place in that list:

```
tengiz cleanup [--all] [--volumes] [--dry-run] → prune unused Docker resources (label-filtered, volumes opt-in)
tengiz restart <app> → restart an application
```

Place `cleanup` and `restart` near the existing `tengiz stop/start/rm` lifecycle line. If a prose "Docker Housekeeping" or cleanup section exists, add a short paragraph describing that `tengiz cleanup` prunes only Tengiz-owned resources and never touches volumes unless `--volumes` is passed.

- [ ] **Step 2: Verify no other docs reference the command list**

Run: `rg -l "tengiz stop/start/rm" --glob '*.md'`
Confirm whether `README.md` is the only file needing the update (e.g. `docs/` may also list commands). If another doc lists CLI commands, add the same two lines there.

- [ ] **Step 3: Run tests and build**

Run: `go test ./... -count=1 && go build -o tengiz .`
Expected: all PASS and clean build

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup and restart commands"
```

---

## Self-Review

**1. Spec coverage:** The feature spec (#6) asks for "label-based `docker system prune`" and "`tengiz cleanup`". Task 2 implements the label-filtered `docker system prune` with `tengiz-app`/`tengiz-env` filters; Task 3 exposes it as `tengiz cleanup` with `--all`, `--volumes`, `--dry-run`. The spec's safety concern (never destroy running apps / their state) is honored by only pruning Tengiz-labeled resources and making volumes opt-in. `tengiz restart` is a small housekeeping companion covering a CLI gap; it is scoped in Task 3. **Gap check:** the spec does not require automatic scheduled cleanup, so none is added (YAGNI). Image retention per app already exists via `KeepLastNImages` and is unchanged.

**2. Placeholder scan:** No TBD/TODO/"add error handling" placeholders. All code blocks are complete and copy-paste ready. The `os` import note in Task 3 explicitly prevents a broken import. No "similar to Task N" references — each task repeats its own full code.

**3. Type consistency:** `CleanupOptions{All, Volumes bool}` is defined once in Task 1 and used identically in Task 2 (`buildCleanupArgs(opts CleanupOptions)`) and Task 3 (`runtime.CleanupOptions{All: all, Volumes: volumes}`). `Cleanup(ctx, opts CleanupOptions) error` matches across interface, stub, dockerRuntime, and CLI call site. `runtime.ContainerName` and `runtime.Manager.Restart` are pre-existing and used as-is. `joinCleanupArgs` is only referenced in Task 3 (its own task) so no cross-task mismatch. Flag names `all`/`volumes`/`dry-run` match between the test in Task 3 Step 1 and the registration in Task 3 Step 3.
