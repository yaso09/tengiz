# One-off Process Execution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz run <app> <cmd>` command that executes one-off commands inside a temporary container created from a deployed app's Docker image, with automatic cleanup on exit.

**Architecture:** Extend `runtime.Manager` interface with a `Run` method that shells out to `docker run --rm`. The CLI command resolves the app's image tag from the config store, then calls `Run` which streams stdout/stderr directly to the terminal. No port allocation needed — the container is ephemeral and immediately removed.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI (`os/exec`)

## Global Constraints

- Container naming: `tengiz-<appname>` (existing convention, not applicable to --rm containers)
- All Docker calls via `os/exec` — no Docker SDK
- App config loaded via `config.NewStore(dataDir)` then `store.GetApp(name)`
- Env vars from `AppEntry.Config.Env` passed via `-e` flags
- Volumes from `AppEntry.Config.Volumes` passed via `-v` flags
- Resources from `AppEntry.Config.Resources` passed via `--memory`/`--cpus` flags
- Image tag from `AppEntry.ImageTag`

---

### Task 1: Extend Runtime Interface and Stub

**Files:**
- Modify: `internal/runtime/runtime.go` (add `RunOptions` type, `Run` to `Manager` interface, stub implementation)
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: nothing (new addition)
- Produces: `RunOptions` struct, `Manager.Run(ctx, cfg, imageTag, cmd, opts) error`

- [ ] **Step 1: Write the failing tests**

```go
// In internal/runtime/runtime_test.go

func TestStubRun(t *testing.T) {
    m := NewStub()
    cfg := &types.AppConfig{Name: "testapp"}
    err := m.Run(context.Background(), cfg, "tengiz-apps/testapp:latest", []string{"echo", "hello"}, RunOptions{})
    if err != nil {
        t.Fatalf("Run() error = %v", err)
    }
}

func TestStubRunInteractive(t *testing.T) {
    m := NewStub()
    cfg := &types.AppConfig{Name: "testapp", Env: map[string]string{"FOO": "bar"}}
    err := m.Run(context.Background(), cfg, "tengiz-apps/testapp:v1", []string{"bash"}, RunOptions{Interactive: true})
    if err != nil {
        t.Fatalf("Run(interactive) error = %v", err)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestStubRun" -v -count=1`
Expected: FAIL — `Manager` interface has no method `Run`

- [ ] **Step 3: Add `RunOptions` type and add `Run` to `Manager` interface**

Add before `Manager` interface in `internal/runtime/runtime.go`:

```go
type RunOptions struct {
    Interactive bool
}
```

Add to `Manager` interface (after `WaitForHealth`):

```go
Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
```

Add to `stubManager`:

```go
func (m *stubManager) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error {
    return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestStubRun" -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go
git commit -m "feat(runtime): add Run method to Manager interface with stub"
```

---

### Task 2: Implement `Run` on Docker Runtime

**Files:**
- Modify: `internal/runtime/docker.go` (implement `Run` on `dockerRuntime`)
- Modify: `internal/runtime/runtime_test.go` (add Docker args test)

**Interfaces:**
- Consumes: `RunOptions`, `types.AppConfig`, image tag
- Produces: Docker `run --rm` execution

- [ ] **Step 1: Write the failing test for Docker args construction**

```go
// In internal/runtime/runtime_test.go

func TestRunArgs(t *testing.T) {
    tests := []struct {
        name     string
        cfg      *types.AppConfig
        imageTag string
        cmd      []string
        opts     RunOptions
        expected string // substring expected in docker args
    }{
        {
            name:     "simple command",
            cfg:      &types.AppConfig{Name: "myapp"},
            imageTag: "tengiz-apps/myapp:latest",
            cmd:      []string{"echo", "hello"},
            opts:     RunOptions{},
            expected: "docker run --rm --label tengiz-app=myapp tengiz-apps/myapp:latest echo hello",
        },
        {
            name: "interactive with env",
            cfg:  &types.AppConfig{Name: "myapp", Env: map[string]string{"DATABASE_URL": "postgres://localhost:5432/db"}},
            imageTag: "tengiz-apps/myapp:v1",
            cmd:      []string{"bash"},
            opts:     RunOptions{Interactive: true},
            expected: "docker run --rm -it --label tengiz-app=myapp -e DATABASE_URL=postgres://localhost:5432/db tengiz-apps/myapp:v1 bash",
        },
        {
            name: "with volumes",
            cfg: &types.AppConfig{
                Name: "myapp",
                Volumes: []types.VolumeConfig{
                    {HostPath: "/data", ContainerPath: "/app/data"},
                },
            },
            imageTag: "tengiz-apps/myapp:latest",
            cmd:      []string{"ls", "/app/data"},
            opts:     RunOptions{},
            expected: "-v /data:/app/data",
        },
        {
            name: "with resources",
            cfg: &types.AppConfig{
                Name:      "myapp",
                Resources: &types.ResourceConfig{Memory: "512m", CPU: "1.0"},
            },
            imageTag: "tengiz-apps/myapp:latest",
            cmd:      []string{"node", "script.js"},
            opts:     RunOptions{},
            expected: "--memory 512m --cpus 1.0",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            args := buildRunArgs(tt.cfg, tt.imageTag, tt.cmd, tt.opts)
            got := strings.Join(args, " ")
            if !strings.Contains(got, tt.expected) {
                t.Errorf("buildRunArgs() = %q, want substring %q", got, tt.expected)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to see it fail**

Run: `go test ./internal/runtime/ -run "TestRunArgs" -v -count=1`
Expected: FAIL — `buildRunArgs` not defined

- [ ] **Step 3: Implement `buildRunArgs` helper and `Run` method**

Add to `internal/runtime/docker.go`:

```go
func buildRunArgs(cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) []string {
    args := []string{"run", "--rm"}
    if opts.Interactive {
        args = append(args, "-it")
    }
    args = append(args, "--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name))
    // Do NOT map ports — ephemeral container doesn't need host port
    args = append(args, envArgs(cfg.Env)...)
    args = append(args, resourceArgs(cfg.Resources)...)
    args = append(args, volumeArgs(cfg.Volumes)...)
    args = append(args, imageTag)
    args = append(args, cmd...)
    return args
}

func (r *dockerRuntime) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error {
    args := buildRunArgs(cfg, imageTag, cmd, opts)
    dcmd := exec.CommandContext(ctx, "docker", args...)
    dcmd.Stdout = os.Stdout
    dcmd.Stderr = os.Stderr
    if opts.Interactive {
        dcmd.Stdin = os.Stdin
    }
    if err := dcmd.Run(); err != nil {
        if ctx.Err() != nil {
            return ctx.Err()
        }
        return fmt.Errorf("docker run: %w", err)
    }
    return nil
}
```

Add import for `"os"` at the top of `internal/runtime/docker.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestRunArgs|TestStubRun" -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/runtime_test.go
git commit -m "feat(runtime): implement Run method on Docker runtime"
```

---

### Task 3: Add `tengiz run` CLI Command

**Files:**
- Modify: `internal/cli/root.go` (add `runCmd` cobra command + register)
- Test: verify build with `go vet` and `go build`

**Interfaces:**
- Consumes: `runtime.Manager.Run()`, `config.Store.GetApp()`
- Produces: `tengiz run <app> [--] <cmd...>` CLI command

- [ ] **Step 1: Add `runCmd` to `internal/cli/root.go`**

Add before `Execute()` function (near `logsCmd` or `devCmd`):

```go
var runCmd = &cobra.Command{
    Use:   "run <app> [--] <command> [args...]",
    Short: "Run a one-off command in a temporary container",
    Long: `Run a one-off command inside a temporary container created from the
app's deployed image. The container is automatically removed on exit.

Useful for database migrations, console access, and data import tasks.

Examples:
  tengiz run myapp -- python manage.py migrate
  tengiz run myapp -- rails console
  tengiz run -i myapp -- bash`,
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

        imageTag := app.ImageTag
        if imageTag == "" {
            return fmt.Errorf("app %q has no image tag — deploy it first", appName)
        }

        rt, err := runtime.NewDocker()
        if err != nil {
            return fmt.Errorf("docker: %w", err)
        }

        fmt.Printf("[tengiz] running: %s (%s)\n", strings.Join(command, " "), imageTag)

        opts := runtime.RunOptions{
            Interactive: interactive,
        }

        if err := rt.Run(cmd.Context(), &app.Config, imageTag, command, opts); err != nil {
            return fmt.Errorf("run: %w", err)
        }

        return nil
    },
}
```

Register it in `init()`:

```go
// Add after: rootCmd.AddCommand(buildLogsCmd)
rootCmd.AddCommand(runCmd)
```

Add flags after the other flag definitions in `init()`:

```go
runCmd.Flags().BoolP("interactive", "i", false, "enable interactive TTY mode")
```

- [ ] **Step 2: Verify build compiles**

Run: `go vet ./...`
Expected: clean output (no errors)

Run: `go build -o /dev/null .`
Expected: builds successfully

- [ ] **Step 3: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: all tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat(cli): add tengiz run command for one-off process execution"
```

---

### Task 4: Integration — `tengiz run` with `--env` Override Support

**Files:**
- Modify: `internal/cli/root.go` (add `--env` flag to `runCmd`)
- Modify: `internal/runtime/runtime.go` (add `ExtraEnv` to `RunOptions`)
- Modify: `internal/runtime/docker.go` (pass extra env in `buildRunArgs`)
- Modify: `internal/runtime/runtime_test.go` (test extra env in args)

- [ ] **Step 1: Write the failing test**

```go
// In internal/runtime/runtime_test.go

func TestRunArgsWithExtraEnv(t *testing.T) {
    cfg := &types.AppConfig{
        Name: "myapp",
        Env:  map[string]string{"BASE_URL": "http://localhost"},
    }
    opts := RunOptions{
        ExtraEnv: map[string]string{"MIGRATION_STEP": "001", "FORCE": "true"},
    }
    args := buildRunArgs(cfg, "tengiz-apps/myapp:v1", []string{"python", "migrate.py"}, opts)
    got := strings.Join(args, " ")
    for _, want := range []string{"-e BASE_URL=http://localhost", "-e MIGRATION_STEP=001", "-e FORCE=true"} {
        if !strings.Contains(got, want) {
            t.Errorf("buildRunArgs() missing %q in %q", want, got)
        }
    }
}
```

- [ ] **Step 2: Extend `RunOptions`**

In `internal/runtime/runtime.go`:

```go
type RunOptions struct {
    Interactive bool
    ExtraEnv    map[string]string // additional env vars for this one-off execution
}
```

- [ ] **Step 3: Update `buildRunArgs` in `internal/runtime/docker.go`**

Replace the `envArgs(cfg.Env)` call with a merge of `cfg.Env` and `opts.ExtraEnv`:

```go
func buildRunArgs(cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) []string {
    args := []string{"run", "--rm"}
    if opts.Interactive {
        args = append(args, "-it")
    }
    args = append(args, "--label", fmt.Sprintf("%s=%s", labelKey, cfg.Name))
    // Merge app env with run-specific extra env (extra overrides app)
    mergedEnv := make(map[string]string, len(cfg.Env)+len(opts.ExtraEnv))
    for k, v := range cfg.Env {
        mergedEnv[k] = v
    }
    for k, v := range opts.ExtraEnv {
        mergedEnv[k] = v
    }
    args = append(args, envArgs(mergedEnv)...)
    args = append(args, resourceArgs(cfg.Resources)...)
    args = append(args, volumeArgs(cfg.Volumes)...)
    args = append(args, imageTag)
    args = append(args, cmd...)
    return args
}
```

- [ ] **Step 4: Add `--env` flag to CLI command**

In `internal/cli/root.go` in the `runCmd` RunE, add before calling `rt.Run`:

```go
extraEnv := make(map[string]string)
envFlags, _ := cmd.Flags().GetStringArray("env")
for _, e := range envFlags {
    parts := strings.SplitN(e, "=", 2)
    if len(parts) != 2 {
        return fmt.Errorf("invalid env format %q, use KEY=VALUE", e)
    }
    extraEnv[parts[0]] = parts[1]
}
opts := runtime.RunOptions{
    Interactive: interactive,
    ExtraEnv:    extraEnv,
}
```

Register flag in `init()`:

```go
runCmd.Flags().StringArrayP("env", "e", nil, "set additional env vars (can be repeated: -e KEY=VALUE)")
```

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./internal/runtime/ -run "TestRunArgs" -v -count=1`
Expected: PASS

Run: `go vet ./...`
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/docker.go internal/runtime/runtime_test.go internal/cli/root.go
git commit -m "feat(cli): add --env flag to tengiz run for extra env vars"
```

---

### Task 5: Documentation & Testing

**Files:**
- Modify: `README.md` (add `tengiz run` to command reference)
- Modify: `internal/cli/root.go` (verify help text renders properly)

- [ ] **Step 1: Verify CLI help text**

Run: `go build -o /tmp/tengiz . && /tmp/tengiz run --help`
Expected: displays command usage, examples, and flags

Run: `/tmp/tengiz run nonexistentapp echo hi`
Expected: error "app nonexistentapp not found"

- [ ] **Step 2: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add tengiz run command reference"
```
