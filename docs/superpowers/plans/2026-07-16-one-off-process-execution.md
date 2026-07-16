# One-off Process Execution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz run <app> -- <cmd>` to execute one-off commands in ephemeral containers from a deployed app's image (for migrations, consoles, data imports).

**Architecture:** New `RunOneOff` method on `runtime.Manager` interface. Docker implementation uses `docker run --rm` with stdin/stdout/stderr wired to the terminal. CLI command resolves app image via `config.Store`, passes env/volumes from the app config.

**Tech Stack:** Go 1.26, Cobra CLI, os/exec, Docker CLI

## Global Constraints

- Container name NOT used (ephemeral `--rm`)
- No port mapping (one-off, no need for persistent routing)
- Must pass env vars, volumes, and resource limits from `AppConfig`
- Interactive mode (`-it`) vs non-interactive (`-i`) controlled by `--interactive`/`-t` flag
- `docker run --rm` for automatic cleanup on exit
- Stub must return nil immediately (no-op)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `RunOneOff` to `Manager` interface + `RunOneOffOptions` struct + stub implementation |
| `internal/runtime/docker.go` | Docker implementation of `RunOneOff` using `docker run --rm` |
| `internal/cli/root.go` | New `runCmd` cobra command registered in `init()` |
| `internal/cli/root_test.go` | Tests for CLI flag parsing, command registration, args validation |
| `README.md` | Document `tengiz run` command usage |

---

### Task 1: Add `RunOneOff` to Manager Interface + Stub

**Files:**
- Modify: `internal/runtime/runtime.go`
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: `Manager` interface (existing), `types.AppConfig` (existing)
- Produces: `RunOneOffOptions` struct, `Manager.RunOneOff(ctx, cfg, imageTag, cmd, opts) error`

- [ ] **Step 1: Add `RunOneOffOptions` struct and `RunOneOff` to interface**

Edit `internal/runtime/runtime.go`. After the `LogOptions` struct (line 15), add:

```go
type RunOneOffOptions struct {
	Interactive bool
}
```

Add to the `Manager` interface after `WaitForHealth` (line 34):

```go
	RunOneOff(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOneOffOptions) error
```

- [ ] **Step 2: Add stub implementation**

After `KeepLastNImages` stub (line 105), add:

```go
func (m *stubManager) RunOneOff(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOneOffOptions) error {
	return nil
}
```

- [ ] **Step 3: Write failing test for the stub**

Create `internal/runtime/runtime_test.go`:

```go
package runtime

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestStubRunOneOffReturnsNil(t *testing.T) {
	m := NewStub()
	err := m.RunOneOff(context.Background(), &types.AppConfig{Name: "test"}, "tengiz-apps/test:latest", []string{"echo", "hi"}, RunOneOffOptions{})
	if err != nil {
		t.Fatalf("stub RunOneOff returned error: %v", err)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

```bash
go test ./internal/runtime/ -run TestStubRunOneOffReturnsNil -v -count=1
```
Expected: FAIL — stubManager does not have RunOneOff

- [ ] **Step 5: Add stub method and run test to verify it passes**

After adding stub method in Step 2, run again:

```bash
go test ./internal/runtime/ -run TestStubRunOneOffReturnsNil -v -count=1
```
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go
git commit -m "feat: add RunOneOff to Manager interface + stub"
```

---

### Task 2: Implement `RunOneOff` in Docker Runtime

**Files:**
- Create: `internal/runtime/oneoff.go`
- Modify: `internal/runtime/docker.go`
- Test: `internal/runtime/oneoff_test.go`

**Interfaces:**
- Consumes: `RunOneOffOptions` from Task 1, helper functions `envArgs`, `volumeArgs`, `resourceArgs` (existing in `docker.go`)
- Produces: `dockerRuntime.RunOneOff()` — runs `docker run --rm` with proper I/O wiring

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/oneoff_test.go`:

```go
package runtime

import (
	"context"
	"os/exec"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestRunOneOffDockerCommandShape(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	// We just verify the dockerRuntime can be created
	rt, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() failed: %v", err)
	}

	cfg := &types.AppConfig{
		Name: "testapp",
		Env:  map[string]string{"FOO": "bar"},
	}

	err = rt.RunOneOff(context.Background(), cfg, "alpine:latest", []string{"echo", "hello"}, RunOneOffOptions{Interactive: false})
	if err != nil {
		t.Fatalf("RunOneOff failed: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/runtime/ -run TestRunOneOffDockerCommandShape -v -count=1
```
Expected: FAIL — dockerRuntime has no RunOneOff method

- [ ] **Step 3: Create `internal/runtime/oneoff.go` with the implementation**

```go
package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/yaso09/tengiz/internal/types"
)

func (r *dockerRuntime) RunOneOff(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOneOffOptions) error {
	args := []string{
		"run", "--rm",
		"-i",
	}
	if opts.Interactive {
		args = append(args, "-t")
	}
	args = append(args, envArgs(cfg.Env)...)
	args = append(args, volumeArgs(cfg.Volumes)...)
	args = append(args, resourceArgs(cfg.Resources)...)
	args = append(args, imageTag)
	args = append(args, cmd...)

	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if err := c.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("command exited with code %d", exitErr.ExitCode())
		}
		return fmt.Errorf("docker run: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/runtime/ -run TestRunOneOffDockerCommandShape -v -count=1
```
Expected: PASS (docker runs `alpine:latest echo hello`)

- [ ] **Step 5: Add container name label for tracking**

Modify `oneoff.go` to add label to the container for consistent tracking. After the `--rm` argument, add:

```go
		"--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
```

And also add an extra label to identify one-off runs:

```go
		"--label", "tengiz-oneoff=true",
```

- [ ] **Step 6: Update test to expect non-zero exit code propagation**

Add a test in `oneoff_test.go`:

```go
func TestRunOneOffPropagatesExitCode(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	rt, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() failed: %v", err)
	}

	cfg := &types.AppConfig{Name: "testapp"}

	err = rt.RunOneOff(context.Background(), cfg, "alpine:latest", []string{"sh", "-c", "exit 42"}, RunOneOffOptions{})
	if err == nil {
		t.Fatal("expected error for exit code 42")
	}
	if err.Error() != "command exited with code 42" {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 7: Run all runtime tests**

```bash
go test ./internal/runtime/ -v -count=1
```
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/oneoff.go internal/runtime/oneoff_test.go
git commit -m "feat: implement RunOneOff in Docker runtime"
```

---

### Task 3: Create CLI Command `tengiz run`

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `config.NewStore`, `config.Store.GetApp()`, `runtime.NewDocker()`, `runtime.RunOneOff()`, `runtime.RunOneOffOptions`
- Produces: `tengiz run <app> [--interactive] -- <command>`

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/root_test.go`:

```go
func TestRunCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"run"})
	if err != nil {
		t.Fatal("run command not registered")
	}
	if cmd == nil || cmd.Name() != "run" {
		t.Fatal("run command not found")
	}
}

func TestRunCmdRejectsLessThanTwoArgs(t *testing.T) {
	// Without enough args, cobra should error
	rootCmd.SetArgs([]string{"run", "myapp"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for missing command")
	}
}

func TestRunCmdFlagInteractiveRegistered(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"run"})
	flag := cmd.Flags().Lookup("interactive")
	if flag == nil {
		t.Fatal("--interactive flag not found on run command")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/cli/ -run "TestRunCmd" -v -count=1
```
Expected: FAIL — run command not registered

- [ ] **Step 3: Add the `runCmd` variable and register it**

In `internal/cli/root.go`, in `init()` (after line 61), add:

```go
rootCmd.AddCommand(runCmd)
```

Before `Execute()` (around line 1041), define the command:

```go
var runCmd = &cobra.Command{
	Use:   "run <app> <command> [args...]",
	Short: "Run a one-off command in a temporary container from the app's image",
	Long: `Run a command in an ephemeral container using the app's deployed image.
Useful for database migrations, interactive consoles, and data imports.

Examples:
  tengiz run myapp -- python manage.py migrate
  tengiz run myapp -it -- rails console
  tengiz run myapp -- sh

The container is automatically removed after the command exits.`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		command := args[1:]

		interactive, _ := cmd.Flags().GetBool("interactive")

		store := config.NewStore(dataDir)
		app, err := store.GetApp(appName)
		if err != nil {
			return fmt.Errorf("app %q not found: %w", appName, err)
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.RunOneOffOptions{
			Interactive: interactive,
		}

		fmt.Fprintf(os.Stderr, "[tengiz] running one-off command in %s container...\n", appName)

		if err := rt.RunOneOff(cmd.Context(), &app.Config, app.ImageTag, command, opts); err != nil {
			return err
		}

		return nil
	},
}
```

Register flags in `init()` (after other flag registrations, around line 68):

```go
runCmd.Flags().BoolP("interactive", "t", false, "attach with TTY (docker -it)")
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/cli/ -run "TestRunCmd" -v -count=1
```
Expected: PASS

- [ ] **Step 5: Run all tests**

```bash
go test ./... -count=1
```
Expected: All tests pass (run tests will skip if no Docker)

- [ ] **Step 6: Build to verify compilation**

```bash
go build -o tengiz .
```
Expected: Binary builds without error

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz run command for one-off process execution"
```

---

### Task 4: Update mockRTForDeploy to Satisfy Manager Interface

**Files:**
- Modify: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `Manager` interface (now includes `RunOneOff`)
- Produces: `mockRTForDeploy.RunOneOff` method

- [ ] **Step 1: Ensure mockRTForDeploy implements Manager**

The `mockRTForDeploy` struct (lines 69-96) is used in deploy tests. It already misses some methods (`CreateFromImage`, `RemoveImage`, `KeepLastNImages`) but those are called by the rollback command, not the deploy command. However, Go will NOT enforce interface satisfaction unless we assign to a Manager variable.

Since our new `runCmd` creates its own `runtime.NewDocker()` directly, the mock is not directly used by `run` tests. But to be safe and keep the codebase clean, add the missing methods.

In `root_test.go`, after the existing `mockRTForDeploy` methods (after line 96), add:

```go
func (m *mockRTForDeploy) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *mockRTForDeploy) RemoveImage(ctx context.Context, imageTag string) error { return nil }
func (m *mockRTForDeploy) KeepLastNImages(ctx context.Context, appName string, n int) error { return nil }
func (m *mockRTForDeploy) RunOneOff(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts runtime.RunOneOffOptions) error { return nil }
```

- [ ] **Step 2: Run tests to verify**

```bash
go test ./internal/cli/ -v -count=1
```
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root_test.go
git commit -m "chore: add missing Manager methods to mockRTForDeploy"
```

---

### Task 5: Update README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Read README to find the commands section**

Open `README.md` and find the section listing commands. Add `run` entry:

```markdown
- **`tengiz run <app> <command> [args...]`** — Run a one-off command in a temporary container using the deployed image. Use `-t` for interactive TTY (e.g., `tengiz run myapp -t -- sh`). Container is auto-removed on exit.
```

Place it alphabetically or logically near the lifecycle commands (`stop`, `start`, `rm`).

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz run command in README"
```

---

## Self-Review

**1. Spec coverage:**
- `tengiz run <app> <cmd>` — Task 3 (CLI command)
- `docker run --rm` auto-cleanup — Task 2 (Docker implementation)
- Env vars passed from AppConfig — Task 2 (`envArgs(cfg.Env)`)
- Volumes passed from AppConfig — Task 2 (`volumeArgs(cfg.Volumes)`)
- Interactive mode via `-t` flag — Task 2 + Task 3 (`opts.Interactive`)
- Exit code propagation — Task 2, Step 6
- Label tracking — Task 2, Step 5
- Stub for tests — Task 1
- README documentation — Task 5

**2. Placeholder scan:** No TBD, TODO, "implement later", or "add appropriate error handling" patterns found. All steps have complete code.

**3. Type consistency:**
- `RunOneOffOptions{Interactive: bool}` — used consistently across Tasks 1, 2, 3
- `RunOneOff(ctx, *AppConfig, string, []string, RunOneOffOptions) error` — same signature everywhere
- `envArgs`, `volumeArgs`, `resourceArgs` — existing helpers reused directly
- `labelKey` — existing constant reused
