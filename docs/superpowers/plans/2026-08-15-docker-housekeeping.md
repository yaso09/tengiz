# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling/unused images, unused networks, anonymous volumes, build cache) while protecting Tengiz-managed containers via the `tengiz-app` label.

**Architecture:** A pure helper `pruneCommands(opts PruneOptions) [][]string` (in `internal/runtime`) builds the list of `docker <subcommand> prune` invocations for the selected categories. The `dockerRuntime.Prune` method executes them and concatenates the output; the `stubManager` returns an empty string for tests. A new `runtime.PruneOptions` struct drives behavior (default categories, `--all`, `--volumes`, `--dry-run`). The CLI `cleanup` command builds the options from flags and calls `rt.Prune`. The label filter `label!=tengiz-app` guarantees stopped Tengiz containers are never removed unless `--all` is passed.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (docker CLI passthrough — no Docker SDK), existing `runtime.Manager` interface, existing label constants `labelKey = "tengiz-app"` and `envLabelKey = "tengiz-env"`.

## Global Constraints

- Only use the Docker CLI via `os/exec` — no Docker SDK dependency
- The `tengiz-app` label filter (`--filter label!=tengiz-app`) MUST protect stopped Tengiz-managed containers unless `--all` is passed
- Default behavior (no category flags): prune containers + images + build cache
- `--volumes` is always opt-in (destructive to anonymous volumes)
- `--dry-run` prints the exact `docker ...` commands that would run WITHOUT executing them
- `Prune(ctx, opts) (string, error)` returns the concatenated docker output for display
- Adding `Prune` to the `runtime.Manager` interface requires updating ALL existing mock implementations so the repo still compiles
- No new external dependencies
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions` struct and `Prune` method to `Manager` interface; add stub implementation |
| `internal/runtime/docker.go` | Add `dockerRuntime.Prune` exec-based implementation |
| `internal/runtime/cleanup.go` | Add pure `pruneCommands(opts PruneOptions) [][]string` helper |
| `internal/runtime/cleanup_test.go` | Unit tests for `pruneCommands` + stub `Prune` test |
| `internal/cli/root.go` | Add `cleanupCmd`, register it, define flags and handler |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy`; tests for cleanup command registration/flags/handler |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` (required to keep compiling) |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` (required to keep compiling) |
| `README.md` | Document the `tengiz cleanup` command and flags |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |

---

### Task 1: Add `PruneOptions`, `Prune` interface method, and `pruneCommands` helper

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add `PruneOptions` struct + `Prune` to `Manager` interface
- Modify: `internal/runtime/runtime.go:113-121` — add stub `Prune` implementation
- Modify: `internal/runtime/cleanup.go` — add pure `pruneCommands` helper
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions struct { All, Volumes, Containers, Images, Networks, BuildCache, DryRun bool }`, `Manager.Prune(ctx context.Context, opts PruneOptions) (string, error)`, `pruneCommands(opts PruneOptions) [][]string` (pure, unexported, testable within package `runtime`)

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestPruneCommandsDefault(t *testing.T) {
	cmds := pruneCommands(PruneOptions{})
	if len(cmds) != 3 {
		t.Fatalf("expected 3 default commands (containers, images, build cache), got %d: %v", len(cmds), cmds)
	}
	// Containers: protected by label!=tengiz-app, no --all
	if got := cmds[0]; len(got) != 5 || got[0] != "container" || got[1] != "prune" || got[2] != "-f" || got[3] != "--filter" || got[4] != "label!=tengiz-app" {
		t.Errorf("containers command = %v, want [container prune -f --filter label!=tengiz-app]", got)
	}
	// Images: no --all
	if got := cmds[1]; len(got) != 3 || got[0] != "image" || got[1] != "prune" || got[2] != "-f" {
		t.Errorf("images command = %v, want [image prune -f]", got)
	}
	// Build cache: no --all
	if got := cmds[2]; len(got) != 3 || got[0] != "builder" || got[1] != "prune" || got[2] != "-f" {
		t.Errorf("build cache command = %v, want [builder prune -f]", got)
	}
}

func TestPruneCommandsAll(t *testing.T) {
	cmds := pruneCommands(PruneOptions{All: true})
	if len(cmds) != 3 {
		t.Fatalf("expected 3 commands with --all, got %d: %v", len(cmds), cmds)
	}
	// Containers with --all: NO label filter (allows Tengiz containers)
	if got := cmds[0]; len(got) != 3 || got[0] != "container" || got[1] != "prune" || got[2] != "-f" {
		t.Errorf("containers command = %v, want [container prune -f]", got)
	}
	// Images with --all
	if got := cmds[1]; len(got) != 4 || got[0] != "image" || got[1] != "prune" || got[2] != "-f" || got[3] != "--all" {
		t.Errorf("images command = %v, want [image prune -f --all]", got)
	}
	// Build cache with --all
	if got := cmds[2]; len(got) != 4 || got[0] != "builder" || got[1] != "prune" || got[2] != "-f" || got[3] != "--all" {
		t.Errorf("build cache command = %v, want [builder prune -f --all]", got)
	}
}

func TestPruneCommandsVolumes(t *testing.T) {
	cmds := pruneCommands(PruneOptions{Volumes: true})
	// Default 3 + volumes = 4
	if len(cmds) != 4 {
		t.Fatalf("expected 4 commands with --volumes, got %d: %v", len(cmds), cmds)
	}
	if got := cmds[3]; len(got) != 3 || got[0] != "volume" || got[1] != "prune" || got[2] != "-f" {
		t.Errorf("volumes command = %v, want [volume prune -f]", got)
	}
}

func TestPruneCommandsExplicitCategory(t *testing.T) {
	// Explicit Images=true only -> exactly one command, no default expansion
	cmds := pruneCommands(PruneOptions{Images: true})
	if len(cmds) != 1 {
		t.Fatalf("expected exactly 1 command when Images=true, got %d: %v", len(cmds), cmds)
	}
	if got := cmds[0]; got[0] != "image" {
		t.Errorf("expected image prune, got %v", got)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	out, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	if out != "" {
		t.Errorf("stub Prune() output = %q, want empty", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneCommands|TestStubPrune" -v -count=1`

Expected: FAIL with compile error `undefined: PruneOptions`, `undefined: pruneCommands`, and `stubManager does not implement Manager (missing method Prune)`.

- [ ] **Step 3: Add `PruneOptions` struct and `Prune` to the `Manager` interface**

In `internal/runtime/runtime.go`, add before the `Manager` interface (near `RunOptions`):

```go
type PruneOptions struct {
	All        bool // also remove Tengiz-managed stopped containers / all unused images & build cache
	Volumes    bool // also prune anonymous volumes
	Containers bool // prune stopped containers
	Images     bool // prune unused images
	Networks   bool // prune unused networks
	BuildCache bool // prune build cache
	DryRun     bool // print commands without executing
}
```

In the `Manager` interface (line 36, after `KeepLastNImages`):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts PruneOptions) (string, error)
```

- [ ] **Step 4: Add the stub implementation**

In `internal/runtime/runtime.go`, after the existing `stubManager.KeepLastNImages` method:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (string, error) {
	return "", nil
}
```

- [ ] **Step 5: Add the pure `pruneCommands` helper to `internal/runtime/cleanup.go`**

Add the following to `internal/runtime/cleanup.go` (keep the existing imports; no new imports needed):

```go
func (o PruneOptions) effective() PruneOptions {
	e := o
	if !e.Containers && !e.Images && !e.Networks && !e.Volumes && !e.BuildCache {
		e.Containers = true
		e.Images = true
		e.BuildCache = true
	}
	return e
}

func pruneCommands(opts PruneOptions) [][]string {
	opts = opts.effective()
	var cmds [][]string
	add := func(args ...string) {
		cmds = append(cmds, args)
	}
	if opts.Containers {
		a := []string{"container", "prune", "-f"}
		if !opts.All {
			a = append(a, "--filter", "label!=tengiz-app")
		}
		add(a...)
	}
	if opts.Networks {
		add("network", "prune", "-f")
	}
	if opts.Images {
		a := []string{"image", "prune", "-f"}
		if opts.All {
			a = append(a, "--all")
		}
		add(a...)
	}
	if opts.Volumes {
		add("volume", "prune", "-f")
	}
	if opts.BuildCache {
		a := []string{"builder", "prune", "-f"}
		if opts.All {
			a = append(a, "--all")
		}
		add(a...)
	}
	return cmds
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneCommands|TestStubPrune" -v -count=1`

Expected: PASS

- [ ] **Step 7: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add runtime.PruneOptions and pruneCommands helper for docker housekeeping"
```

---

### Task 2: Implement `dockerRuntime.Prune` exec-based method

**Files:**
- Modify: `internal/runtime/docker.go` — add `dockerRuntime.Prune`
- Test: `internal/runtime/cleanup_test.go` (stub already covered in Task 1; this adds the exec wrapper that reuses `pruneCommands`)

**Interfaces:**
- Consumes: `pruneCommands(opts PruneOptions) [][]string` from Task 1
- Produces: `(*dockerRuntime).Prune(ctx context.Context, opts PruneOptions) (string, error)` executing each command and returning concatenated output

- [ ] **Step 1: Write the failing test (compilation gate)**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestDockerPruneCompiles(t *testing.T) {
	// Not a real docker invocation — verifies the dockerRuntime implements Manager
	// after the interface change. Real exec behavior is covered by pruneCommands tests.
	var _ Manager = &dockerRuntime{}
}
```

Run: `go test ./internal/runtime/... -run TestDockerPruneCompiles -v -count=1`

Expected: PASS (this test passes immediately; it exists to lock the compile-time contract that `dockerRuntime` satisfies the expanded `Manager` interface).

- [ ] **Step 2: Write the `dockerRuntime.Prune` implementation**

Add to `internal/runtime/docker.go` (near the other docker exec methods, e.g. after `KeepLastNImages` is defined in `cleanup.go` — but as a method on `dockerRuntime` it belongs with the exec code; place it in `docker.go`):

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (string, error) {
	var out strings.Builder
	for _, args := range pruneCommands(opts) {
		cmdStr := strings.Join(args, " ")
		if opts.DryRun {
			out.WriteString("docker " + cmdStr + "\n")
			continue
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		o, err := cmd.CombinedOutput()
		out.Write(o)
		if !strings.HasSuffix(out.String(), "\n") && len(o) > 0 {
			out.WriteByte('\n')
		}
		if err != nil {
			return out.String(), fmt.Errorf("docker %s: %w\n%s", cmdStr, err, string(o))
		}
	}
	return out.String(), nil
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 4: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/docker.go
git commit -m "feat: implement dockerRuntime.Prune for docker housekeeping"
```

---

### Task 3: Add `Prune` to the remaining mock implementations

**Files:**
- Modify: `internal/cli/root_test.go:99` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:33` — add `Prune` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:34` — add `Prune` to `mockRuntime`

**Interfaces:**
- Consumes: `Manager.Prune(ctx context.Context, opts PruneOptions) (string, error)` from Task 1
- Produces: all mocks satisfy the expanded `Manager` interface; repo compiles

- [ ] **Step 1: Write the failing test (interface assertion)**

Add to `internal/cli/root_test.go` (near `TestMockRTForDeployImplementsManager`):

```go
func TestMockRTForDeployImplementsPrune(t *testing.T) {
	var m runtime.Manager = &mockRTForDeploy{}
	_, err := m.Prune(context.Background(), runtime.PruneOptions{})
	if err != nil {
		t.Fatalf("mock Prune() error = %v", err)
	}
}
```

Run: `go test ./internal/cli/... -run TestMockRTForDeployImplementsPrune -v -count=1`

Expected: FAIL with `cannot use &mockRTForDeploy{} (value of type *mockRTForDeploy) as runtime.Manager value in variable declaration: missing method Prune`.

- [ ] **Step 2: Add `Prune` to `mockRTForDeploy`**

In `internal/cli/root_test.go`, after the `KeepLastNImages` method (line 99), add:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (string, error) { return "", nil }
```

- [ ] **Step 3: Add `Prune` to `mockRuntime` in `internal/idle/idle_test.go`**

After the `KeepLastNImages` method (line 33), add:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (string, error) { return "", nil }
```

- [ ] **Step 4: Add `Prune` to `mockRuntime` in `internal/proxy/proxy_test.go`**

After the `KeepLastNImages` method (line 34), add:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (string, error) { return "", nil }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... ./internal/idle/... ./internal/proxy/... -run TestMockRTForDeployImplementsPrune -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "test: add Prune to runtime.Manager mock implementations"
```

---

### Task 4: Add the `cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, register it, define flags and handler
- Test: `internal/cli/root_test.go` — registration, flags, and handler tests

**Interfaces:**
- Consumes: `Manager.Prune(ctx context.Context, opts runtime.PruneOptions) (string, error)` and `runtime.PruneOptions` from Tasks 1-2
- Produces: `tengiz cleanup` command with flags `--all`, `--volumes`, `--dry-run`, `--containers`, `--images`, `--networks`, `--build-cache`

- [ ] **Step 1: Write the failing tests**

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
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"all", "volumes", "dry-run", "containers", "images", "networks", "build-cache"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdPassesOptions(t *testing.T) {
	var got runtime.PruneOptions
	var called bool
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		opts, err := pruneOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		got = opts
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--all", "--volumes", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
	if !got.All || !got.Volumes || !got.DryRun {
		t.Errorf("prune options = %+v, want All, Volumes, DryRun all true", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` (and `undefined: pruneOptionsFromFlags`).

- [ ] **Step 3: Add the `pruneOptionsFromFlags` helper**

Add to `internal/cli/root.go` (package-level function, near the other helpers):

```go
func pruneOptionsFromFlags(cmd *cobra.Command) (runtime.PruneOptions, error) {
	var opts runtime.PruneOptions
	var err error
	if opts.All, err = cmd.Flags().GetBool("all"); err != nil {
		return opts, err
	}
	if opts.Volumes, err = cmd.Flags().GetBool("volumes"); err != nil {
		return opts, err
	}
	if opts.Containers, err = cmd.Flags().GetBool("containers"); err != nil {
		return opts, err
	}
	if opts.Images, err = cmd.Flags().GetBool("images"); err != nil {
		return opts, err
	}
	if opts.Networks, err = cmd.Flags().GetBool("networks"); err != nil {
		return opts, err
	}
	if opts.BuildCache, err = cmd.Flags().GetBool("build-cache"); err != nil {
		return opts, err
	}
	if opts.DryRun, err = cmd.Flags().GetBool("dry-run"); err != nil {
		return opts, err
	}
	return opts, nil
}
```

- [ ] **Step 4: Add the `cleanupCmd` command and register it**

Add the command variable after the existing command variables (e.g. after `configShowCmd` definition) and register it in `init()`:

Command variable (add near the other `var xCmd = &cobra.Command{...}` definitions):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, networks, volumes, build cache)",
	Long: `Prune unused Docker resources to reclaim disk space.

By default prunes stopped containers (protecting Tengiz-managed ones via the
tengiz-app label), dangling images, and build cache. Use flags to select
specific categories or --all to also remove Tengiz-managed stopped containers
and all unused images.

Use --dry-run to print the exact docker commands without executing them.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := pruneOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		out, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return err
		}
		if opts.DryRun {
			fmt.Println("[tengiz] dry run — no resources removed. Commands:")
		} else {
			fmt.Println("[tengiz] docker housekeeping complete:")
		}
		fmt.Print(out)
		return nil
	},
}
```

In `init()`, register the command and flags (add after `rootCmd.AddCommand(notificationCmd)`):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("all", false, "also remove Tengiz-managed stopped containers and all unused images/build cache")
	cleanupCmd.Flags().Bool("volumes", false, "also prune anonymous volumes")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune unused images")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "print the docker commands without executing them")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Run all CLI tests**

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 5: Update documentation

**Files:**
- Modify: `README.md` — document the `cleanup` command
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI command list

**Interfaces:**
- Consumes: the `tengiz cleanup` command and flags from Task 4
- Produces: accurate user-facing documentation

- [ ] **Step 1: Add the `cleanup` command to the CLI list in `README.md`**

Find the CLI commands section in `README.md` and add a line matching the existing command-list style (after the `tengiz rollback <app>` line):

```markdown
tengiz cleanup [--all] [--volumes] [--dry-run] [--containers] [--images] [--networks] [--build-cache] → prune unused Docker resources (protects Tengiz-managed containers)
```

Also add a short usage example block near the other command examples:

```markdown
tengiz cleanup            → prune stopped containers (non-Tengiz), dangling images, build cache
tengiz cleanup --all      → also remove Tengiz-managed stopped containers and all unused images
tengiz cleanup --volumes  → additionally prune anonymous volumes
tengiz cleanup --dry-run  → show the docker commands without running them
```

- [ ] **Step 2: Add the `cleanup` command to `AGENTS.md`**

Find the CLI section in `AGENTS.md` and add after the `tengiz rollback <app>` line:

```markdown
tengiz cleanup [--all] [--volumes] [--dry-run] → prune unused Docker resources (housekeeping)
```

- [ ] **Step 3: Verify the markdown renders / files are well-formed**

Run: `go build ./...`

Expected: Build succeeds (docs changes are non-code, this just confirms nothing else broke)

- [ ] **Step 4: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

### Task 6: Final verification and self-review

**Files:**
- No code changes — verification only

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS. Note: proxy tests are slow (~2s each) and idle tests are time-sensitive — allow for that; no failures expected.

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 3: Manual smoke check (optional, requires docker)**

If docker is available, run:

```bash
go build -o tengiz .
./tengiz cleanup --dry-run
```

Expected output prints three `docker ...` commands (container prune with `label!=tengiz-app` filter, image prune `-f`, builder prune `-f`) and does NOT execute them.

- [ ] **Step 4: Self-review against spec**

Check against the feature requirement from `docs/FUTURES_FEATURES.md` #6:
- "Label-based `docker system prune`" — ✅ Task 1-2: `pruneCommands` uses `--filter label!=tengiz-app` to protect Tengiz containers
- "`tengiz cleanup`" command — ✅ Task 4: `cleanupCmd` registered with `--all/--volumes/--dry-run` and per-category flags
- "Disk space is the #1 production issue" — ✅ default behavior prunes the common waste (containers, images, build cache)
- Scope boundary: per-app image retention via `KeepLastNImages` already exists and is untouched; this feature covers system-wide housekeeping

- [ ] **Step 5: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "fill in details", "add appropriate error handling" (without code), "similar to Task N". All steps contain complete code. None present.

- [ ] **Step 6: Type consistency check**

- `runtime.PruneOptions{All, Volumes, Containers, Images, Networks, BuildCache, DryRun bool}` — identical struct referenced in Tasks 1, 2, 3, 4
- `Manager.Prune(ctx context.Context, opts PruneOptions) (string, error)` — same signature in interface (Task 1), stub (Task 1), docker impl (Task 2), and all three mocks (Task 3)
- `pruneCommands(opts PruneOptions) [][]string` — defined and tested in Task 1, consumed only by `dockerRuntime.Prune` in Task 2
- `pruneOptionsFromFlags(cmd *cobra.Command) (runtime.PruneOptions, error)` — defined in Task 4, used by both the real handler and the test

- [ ] **Step 7: Commit (no code changes in this task — only run if a fix was needed)**

```bash
git add -A
git commit -m "test: final verification for docker housekeeping"
```
