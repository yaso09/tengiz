# One-off Process Execution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz run <app> <command> [args...]` — run a one-off command in a temporary container from the app's image, auto-removed on exit (`docker run --rm`).

**Architecture:** Extend `runtime.Manager` interface with `RunOnce(ctx, cfg, imageTag, cmd)`. The docker implementation shells out to `docker run --rm -it` with the app's env vars, volumes, and resource limits, passing through stdin/stdout/stderr. A new `runCmd` CLI command looks up the app in the store, gets the image tag, and calls `RunOnce`. No port allocation, no proxy registration, no persistence.

**Tech Stack:** Go 1.26, Cobra CLI, `os/exec` for Docker CLI calls.

## Global Constraints

- Container names are prefixed `tengiz-<appname>`, labeled with `tengiz-app=<appname>`
- All existing `Manager` methods must continue working unchanged
- `stubManager` must implement `RunOnce` (return nil)
- `mockRTForDeploy` in tests must implement `RunOnce` (return nil)
- `RunOnce` must not allocate ports, register proxy routes, or persist state

---

## File Structure

| File | Change | Responsibility |
|------|--------|---------------|
| `internal/runtime/runtime.go` | Modify | Add `RunOnce` to `Manager` interface + `stubManager.RunOnce` |
| `internal/runtime/run.go` | Create | `dockerRuntime.RunOnce` implementation |
| `internal/cli/root.go` | Modify | Add `runCmd` cobra command, register in `init()` |
| `internal/cli/root_test.go` | Modify | Add tests for `runCmd` registration and behavior |
| `internal/runtime/runtime_test.go` | Modify | Add test for `stubManager.RunOnce` |

---

### Task 1: Add `RunOnce` to Manager interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go:18-35` (Manager interface)
- Modify: `internal/runtime/runtime.go:37-105` (stubManager)
- Modify: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: `types.AppConfig`, `types.VolumeConfig`, `types.ResourceConfig` (unchanged)
- Produces: `Manager.RunOnce(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string) error`

- [ ] **Step 1: Write the failing test for stub**

```go
// add to internal/runtime/runtime_test.go
func TestStubRunOnce(t *testing.T) {
	m := NewStub()
	cfg := &types.AppConfig{Name: "testapp", Port: 3000}
	err := m.RunOnce(context.Background(), cfg, "test:latest", []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubRunOnce -v -count=1`
Expected: FAIL with "m.RunOnce undefined (type Manager has no field or method RunOnce)"

- [ ] **Step 3: Add `RunOnce` to `Manager` interface**

```go
// in internal/runtime/runtime.go, add to Manager interface after WaitForHealth:
	RunOnce(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string) error
```

- [ ] **Step 4: Add `RunOnce` to `stubManager`**

```go
// in internal/runtime/runtime.go, add to stubManager:
func (m *stubManager) RunOnce(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string) error {
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubRunOnce -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go
git commit -m "feat: add RunOnce to Manager interface and stub"
```

---

### Task 2: Implement `dockerRuntime.RunOnce`

**Files:**
- Create: `internal/runtime/run.go`

**Interfaces:**
- Consumes: `Manager.RunOnce(ctx, cfg, imageTag, cmd)` signature, `envArgs`, `volumeArgs`, `resourceArgs`, `sanitizeContainerName` helpers from `docker.go`, `labelKey` constant
- Produces: `dockerRuntime.RunOnce` implementation

- [ ] **Step 1: Write the failing test for docker implementation**

For unit-testing the implementation, we can't easily test Docker without Docker. But we can test that `RunOnce` on the stub passes. The real Docker test is integration-level. Add a compile-time interface check:

```go
// add to internal/runtime/run.go initially (will be replaced)
// Placeholder to make test compile
```

Actually, the stub test from Task 1 already passes. For this task, we just implement. The existing `TestStubSatisfiesInterface` test will also catch interface satisfaction.

- [ ] **Step 2: Create `internal/runtime/run.go` with implementation**

```go
package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/yaso09/tengiz/internal/types"
)

func (r *dockerRuntime) RunOnce(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string) error {
	containerName := fmt.Sprintf("tengiz-%s-run-%d", sanitizeContainerName(cfg.Name), time.Now().Unix())

	args := []string{
		"run", "--rm",
		"--name", containerName,
		"--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name),
	}
	args = append(args, "-i")
	fi, _ := os.Stdin.Stat()
	if fi != nil && fi.Mode()&os.ModeCharDevice != 0 {
		args = append(args, "-t")
	}
	args = append(args, envArgs(cfg.Env)...)
	args = append(args, resourceArgs(cfg.Resources)...)
	args = append(args, volumeArgs(cfg.Volumes)...)
	args = append(args, imageTag)
	args = append(args, cmd...)

	dcmd := exec.CommandContext(ctx, "docker", args...)
	dcmd.Stdin = os.Stdin
	dcmd.Stdout = os.Stdout
	dcmd.Stderr = os.Stderr

	if err := dcmd.Run(); err != nil {
		return fmt.Errorf("run once: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Run tests to verify**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: PASS (all existing tests + TestStubRunOnce)

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/run.go
git commit -m "feat: implement dockerRuntime.RunOnce"
```

---

### Task 3: Add CLI command `tengiz run <app> <cmd> [args...]`

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `config.NewStore(dataDir)`, `store.GetApp(name)`, `app.Config`, `app.ImageTag`, `runtime.Manager.RunOnce`
- Produces: `runCmd` cobra command registered on `rootCmd`

- [ ] **Step 1: Write the failing test for CLI command**

```go
// add to internal/cli/root_test.go
func TestRunCmdRegistration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"run"})
	if err != nil {
		t.Fatal("run command not registered")
	}
	if cmd == nil || cmd.Name() != "run" {
		t.Fatal("run command not found")
	}
}

func TestRunCmdFailsWithNoArgs(t *testing.T) {
	rootCmd.SetArgs([]string{"run"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for missing args")
	}
}

func TestRunCmdFailsWithOneArg(t *testing.T) {
	rootCmd.SetArgs([]string{"run", "myapp"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for missing command")
	}
}

func TestRunCmdFailsWithUnknownApp(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir
	rootCmd.SetArgs([]string{"run", "nonexistent", "echo", "hi"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for unknown app")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestRunCmd" -v -count=1`
Expected: FAIL — "run command not registered"

- [ ] **Step 3: Add `runCmd` to `root.go`**

Add the command variable (place near `buildLogsCmd` around line 882):

```go
var runCmd = &cobra.Command{
	Use:   "run <app> <command> [args...]",
	Short: "Run a one-off command in the app's container",
	Long: `Run a one-off command in a temporary container built from the app's image.

The container is created with the same environment variables, volumes, and
resource limits as the deployed app, runs the specified command, and is
automatically removed on exit.

Examples:
  tengiz run myapp python manage.py migrate
  tengiz run myapp -- python manage.py shell
  tengiz run myapp node -e "console.log('hello')"`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		command := args[1:]

		store := config.NewStore(dataDir)
		app, err := store.GetApp(appName)
		if err != nil {
			return fmt.Errorf("app %q not found: %w", appName, err)
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		fmt.Printf("[tengiz] running in %s: %s\n", appName, strings.Join(command, " "))
		if err := rt.RunOnce(cmd.Context(), &app.Config, app.ImageTag, command); err != nil {
			return err
		}
		return nil
	},
}
```

Register in `init()` (add to the block around lines 33-61):

```go
rootCmd.AddCommand(runCmd)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestRunCmd" -v -count=1`
Expected: PASS (registration test passes, missing args test passes, unknown app test passes)

Note: `TestRunCmdFailsWithNoArgs` — `cobra.MinimumNArgs(2)` will fail `rootCmd.Execute()` when no args are given since cobra validates args before running `RunE`. So `err` will be non-nil. This is correct behavior.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add tengiz run CLI command"
```

---

### Task 4: Update mockRTForDeploy and add edge case tests

**Files:**
- Modify: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `mockRTForDeploy` must implement `Manager` (already used as `interface{}` in `TestDeployZeroDowntimeCreatesVersionedContainer`)
- Produces: `mockRTForDeploy.RunOnce` method

- [ ] **Step 1: Write test — verify mockRTForDeploy now has RunOnce**

```go
// add to internal/cli/root_test.go, near existing mockRTForDeploy tests
func TestMockRTForDeployImplementsRunOnce(t *testing.T) {
	m := &mockRTForDeploy{}
	err := m.RunOnce(context.Background(), &types.AppConfig{}, "img", []string{"echo", "hi"})
	if err != nil {
		t.Fatal("mockRTForDeploy.RunOnce should return nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestMockRTForDeployImplementsRunOnce -v -count=1`
Expected: FAIL — "m.RunOnce undefined"

- [ ] **Step 3: Add `RunOnce` to `mockRTForDeploy`**

```go
// add to mockRTForDeploy in internal/cli/root_test.go
func (m *mockRTForDeploy) RunOnce(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string) error {
	return nil
}
```

Also add missing methods that `mockRTForDeploy` doesn't implement:

```go
func (m *mockRTForDeploy) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	return nil
}
func (m *mockRTForDeploy) RemoveImage(ctx context.Context, imageTag string) error {
	return nil
}
func (m *mockRTForDeploy) KeepLastNImages(ctx context.Context, appName string, n int) error {
	return nil
}
```

Otherwise the project won't compile because `mockRTForDeploy` doesn't satisfy the `Manager` interface (these methods were added before but `mockRTForDeploy` was never updated).

- [ ] **Step 4: Run all tests to verify they all pass**

Run: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root_test.go
git commit -m "test: add RunOnce to mockRTForDeploy and add run cmd tests"
```

---

## Self-Review

**1. Spec coverage:**
- Add `RunOnce` to `Manager` interface → Task 1
- Add `RunOnce` to `stubManager` → Task 1
- Implement `dockerRuntime.RunOnce` (`docker run --rm -it` with env/volumes/resources) → Task 2
- Add `tengiz run <app> <cmd>` CLI command with `cobra.MinimumNArgs(2)` + store lookup + error handling → Task 3
- Register command in `init()` → Task 3
- Update `mockRTForDeploy` for interface compliance → Task 4
- Tests for CLI registration, arg validation, unknown app → Task 3

**2. Placeholder scan:** No placeholders, TODOs, or "handle appropriately" found.

**3. Type consistency:** 
- `RunOnce(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string) error` is used consistently across all 4 tasks
- `types.AppConfig` and `types.VolumeConfig` use existing types
- Container naming follows `tengiz-<appname>-run-<timestamp>` pattern, consistent with existing `tengiz-<appname>` convention
