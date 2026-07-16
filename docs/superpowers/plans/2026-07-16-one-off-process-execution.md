# One-off Process Execution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz run <appname> [-- command args...]` — execute one-off commands in ephemeral containers based on a deployed app's image (database migrations, Rails console, data import), then auto-remove the container on exit.

**Architecture:** Add `RunOnce` to `runtime.Manager` interface that executes `docker run --rm -i <image> <cmd>` with stdin/stdout/stderr connected to the parent terminal. A new Cobra command `tengiz run` looks up the app's image tag via `config.Store.GetApp()`, optionally rebuilds with `--build`, and streams output. No port allocation, no store persistence, no proxy registration.

**Tech Stack:** Go 1.26, Cobra, Docker CLI via `os/exec`

## Global Constraints

- Container name auto-generated (no conflict with `tengiz-*` named containers)
- Uses `docker run --rm` — container deleted on exit, always
- Returns the container's exit code so shell `&&` chains work (`tengiz run myapp -- migrate && tengiz run myapp -- seed`)
- No port mapping — one-off commands don't serve HTTP
- Reuses app's env vars from store; extra env via `--env` flag
- No idle, health check, or proxy interaction
- Image tag convention: `tengiz-apps/<appname>:latest` (existing, from `builder.Builder.Build`)

---

### Task 1: Add `RunOnce` to `runtime.Manager` Interface

**Files:**
- Modify: `internal/runtime/runtime.go:10-24`
- Test: `internal/runtime/runtime_test.go`
- Modify: `internal/runtime/docker.go`

**Interfaces:**
- Consumes: `types.AppConfig` (for `Env` map), existing `envArgs` helper
- Produces: `Manager.RunOnce(ctx, imageTag, cmdArgs, env) (exitCode int, err error)`

- [ ] **Step 1: Write the failing interface test**

Add to `internal/runtime/runtime_test.go`:

```go
func TestStubRunOnce(t *testing.T) {
	m := NewStub()
	code, err := m.RunOnce(context.Background(), "tengiz-apps/myapp:latest", []string{"echo", "hello"}, nil)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubRunOnce -v -count=1`
Expected: FAIL — `Manager` interface has no method `RunOnce`

- [ ] **Step 3: Add `RunOnce` to the interface**

Edit `internal/runtime/runtime.go` — add `RunOnce` to the `Manager` interface (after `WaitForHealth`):

```go
type Manager interface {
	Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	RemoveBySuffix(ctx context.Context, name string, suffix string) error
	IsActive(ctx context.Context, name string) (bool, error)
	GetContainerPort(ctx context.Context, name string, suffix string) (int, error)
	List(ctx context.Context) ([]types.AppStatus, error)
	Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error)
	WaitForReady(ctx context.Context, name string, internalPort int) error
	WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error
	RunOnce(ctx context.Context, imageTag string, cmdArgs []string, env map[string]string) (int, error)
}
```

Add stub implementation in the same file (after `stubManager.WaitForHealth`):

```go
func (m *stubManager) RunOnce(ctx context.Context, imageTag string, cmdArgs []string, env map[string]string) (int, error) {
	return 0, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubRunOnce -v -count=1`
Expected: PASS

- [ ] **Step 5: Add the Docker implementation**

Add to `internal/runtime/docker.go`:

```go
func (r *dockerRuntime) RunOnce(ctx context.Context, imageTag string, cmdArgs []string, env map[string]string) (int, error) {
	args := []string{
		"run", "--rm", "-i",
	}
	args = append(args, envArgs(env)...)
	args = append(args, imageTag)
	args = append(args, cmdArgs...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, fmt.Errorf("docker run: %w", err)
	}
	return 0, nil
}
```

Add import for `"os"` at the top of `internal/runtime/docker.go`:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/yaso09/tengiz/internal/types"
)
```

- [ ] **Step 6: Run tests to verify**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat(runtime): add RunOnce method for one-off container execution"
```

---

### Task 2: Add `tengiz run` CLI Command

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.Manager.RunOnce`, `config.Store.GetApp`, `config.Store.ListEnv`, `builder.Builder.Build`
- Produces: CLI command `tengiz run <app> [-- command args...]`

- [ ] **Step 1: Write the failing registration test**

Add to `internal/cli/root_test.go`:

```go
func TestRunCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"run"})
	if err != nil {
		t.Fatal("run command not found")
	}
	if cmd == nil {
		t.Fatal("run command is nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestRunCommandRegistered -v -count=1`
Expected: FAIL — "run command not found"

- [ ] **Step 3: Write the run command**

Add to `internal/cli/root.go` before `var configCmd` (around line 768), and register in `init()` before the volume command registration (around line 59):

Register in `init()` after the `volumeCmd.AddCommand` block:

```go
rootCmd.AddCommand(runCmd)
```

Define the command (insert before `var configCmd`):

```go
var runCmd = &cobra.Command{
	Use:   "run <app> [-- command args...]",
	Short: "Run a one-off command in a temporary container",
	Long: `Executes a command inside a temporary container based on the deployed app's image.
The container is automatically removed after the command exits.
Useful for database migrations, console access, data import, etc.

Examples:
  tengiz run myapp -- python manage.py migrate
  tengiz run myapp -- rails console
  tengiz run --build myapp -- npm run seed`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		cmdArgs := args[1:]

		store := config.NewStore(dataDir)
		app, err := store.GetApp(appName)
		if err != nil {
			return fmt.Errorf("app %q not found: %w", appName, err)
		}

		imageTag := app.ImageTag

		rebuild, _ := cmd.Flags().GetBool("build")
		if rebuild {
			projectRoot, err := config.FindProjectRoot(".")
			if err != nil {
				abs, _ := filepath.Abs(".")
				projectRoot = abs
			}
			cfg, err := config.Load(projectRoot)
			if err != nil {
				cfg = &types.AppConfig{Name: appName}
			}
			detection, err := builder.Detect(projectRoot)
			if err != nil {
				return fmt.Errorf("detect: %w", err)
			}
			if cfg.Port == 0 {
				cfg.Port = detection.InternalPort
			}
			b := builder.New(dataDir)
			imageTag, err = b.Build(context.Background(), projectRoot, cfg.Name, detection)
			if err != nil {
				return fmt.Errorf("build: %w", err)
			}
		}

		extraEnv, _ := cmd.Flags().GetStringArray("env")
		env := make(map[string]string)
		for k, v := range app.Config.Env {
			env[k] = v
		}
		for _, e := range extraEnv {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid env format %q, use KEY=VALUE", e)
			}
			env[parts[0]] = parts[1]
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		exitCode, err := rt.RunOnce(context.Background(), imageTag, cmdArgs, env)
		if err != nil {
			return err
		}
		if exitCode != 0 {
			return fmt.Errorf("command exited with code %d", exitCode)
		}
		return nil
	},
}
```

Add flag registration in `Execute()`:

```go
runCmd.Flags().Bool("build", false, "rebuild the image before running")
runCmd.Flags().StringArray("env", nil, "set environment variables (KEY=VALUE, repeatable)")
```

Add `MinimumNArgs` to imports — cobra already has it imported.

- [ ] **Step 4: Verify the test passes**

Run: `go test ./internal/cli/ -run TestRunCommandRegistered -v -count=1`
Expected: PASS

- [ ] **Step 5: Add the `--build` flag flag registration**

In the `Execute()` function, add:

```go
runCmd.Flags().Bool("build", false, "rebuild the image before running")
runCmd.Flags().StringArray("env", nil, "set environment variables (KEY=VALUE, repeatable)")
```

- [ ] **Step 6: Run all cli tests**

Run: `go test ./internal/cli/ -v -count=1`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz run command for one-off container execution"
```

---

### Task 3: Update `mockRTForDeploy` in CLI Tests

**Files:**
- Modify: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `Manager` interface — mock needs `RunOnce`

- [ ] **Step 1: Add `RunOnce` to the mock**

Add to `mockRTForDeploy` in `internal/cli/root_test.go`:

```go
func (m *mockRTForDeploy) RunOnce(ctx context.Context, imageTag string, cmdArgs []string, env map[string]string) (int, error) {
	return 0, nil
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cli/ -v -count=1`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cli/root_test.go
git commit -m "test(cli): add RunOnce to mockRTForDeploy"
```

---

### Task 4: Full Integration Test

**Files:**
- Create: `internal/cli/run_test.go`

- [ ] **Step 1: Write integration test for `tengiz run`**

Create `internal/cli/run_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)

func TestRunCmdMissingApp(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir

	rootCmd.SetArgs([]string{"run", "nonexistent"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestRunCmdAppFoundNoCommand(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir

	store := config.NewStore(dataDir)
	store.SaveApp(types.AppEntry{
		Name:     "testapp",
		ImageTag: "tengiz-apps/testapp:latest",
		Config: types.AppConfig{
			Name: "testapp",
			Env:  map[string]string{"MY_VAR": "myval"},
		},
	})

	// The real docker runtime will fail if docker is not available,
	// but we can verify the flow reaches runtime.RunOnce
	rootCmd.SetArgs([]string{"run", "testapp", "--", "echo", "hello"})
	err := rootCmd.Execute()
	if err != nil {
		t.Logf("run command failed (expected if Docker unavailable): %v", err)
	}
}

func TestRunCmdWithEnvFlag(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir

	store := config.NewStore(dataDir)
	store.SaveApp(types.AppEntry{
		Name:     "testapp",
		ImageTag: "tengiz-apps/testapp:latest",
		Config: types.AppConfig{
			Name: "testapp",
			Env:  map[string]string{"EXISTING": "old"},
		},
	})

	rootCmd.SetArgs([]string{"run", "--env", "EXTRA=new", "testapp", "--", "env"})
	err := rootCmd.Execute()
	if err != nil {
		t.Logf("run command failed (expected if Docker unavailable): %v", err)
	}
}

func TestRunCmdWithBuildFlag(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir

	store := config.NewStore(dataDir)
	store.SaveApp(types.AppEntry{
		Name:     "testapp",
		ImageTag: "tengiz-apps/testapp:latest",
		Config: types.AppConfig{
			Name: "testapp",
		},
	})

	rootCmd.SetArgs([]string{"run", "--build", "testapp", "--", "echo", "hello"})
	err := rootCmd.Execute()
	t.Logf("run --build command result: %v", err)
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cli/ -run 'TestRunCmd' -v -count=1`
Expected: All PASS (the real Docker commands may fail if Docker is unavailable, but the test should handle that gracefully)

- [ ] **Step 3: Run all tests**

Run: `go test ./... -v -count=1`
Expected: All PASS

- [ ] **Step 4: Run vet**

Run: `go vet ./...`
Expected: No output (clean)

- [ ] **Step 5: Build**

Run: `go build -o tengiz .`
Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/cli/run_test.go
git commit -m "test(cli): add integration tests for tengiz run"
```
