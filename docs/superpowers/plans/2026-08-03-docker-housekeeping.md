# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that runs a label-protected `docker system prune` so operators can reclaim disk space on single-server deployments without risking Tengiz-managed containers.

**Architecture:** Add a `Cleanup(ctx, opts)` method to the `runtime.Manager` interface (following the existing `RemoveImage`/`KeepLastNImages` pattern in `internal/runtime/cleanup.go`). The Docker implementation shells out to `docker system prune -f --filter label!=tengiz-app` via `os/exec` — the `label!=tengiz-app` filter guarantees stopped Tengiz containers (which carry the `tengiz-app` label) are never removed. Optional `--all` and `--volumes` flags map to Docker's `-a` and `--volumes` system-prune flags. A pure `buildCleanupArgs()` helper keeps the command construction unit-testable without a Docker daemon, and `extractReclaimed()` parses Docker's "Total reclaimed space:" line from the output. A new `tengiz cleanup` cobra command wires it to the CLI.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` Docker CLI passthrough, existing `runtime.Manager` interface. No new external dependencies.

## Global Constraints

- No Docker SDK — runtime calls `docker` CLI via `os/exec`, matching every other runtime method
- Every prune is non-interactive: always pass `-f`
- Every prune protects Tengiz-managed containers with `--filter label!=tengiz-app` (label set by `Create`/`CreateVersioned`/`CreateFromImage` in `internal/runtime/docker.go:98-99`)
- Volumes are NEVER pruned by default — the `--volumes` flag must be passed explicitly (Tengiz uses bind mounts, so it creates no named volumes; `docker volume prune` also affects other tools' named volumes)
- `--all` adds Docker's `-a`, which removes ALL unused images including old `tengiz-apps/<name>:<tag>` rollback images kept by `KeepLastNImages` — this tradeoff is documented in the flag help and README
- No new external dependencies
- Adding a method to `runtime.Manager` breaks every implementer; all three implementers (`dockerRuntime`, `stubManager` in `internal/runtime/runtime.go`, `mockRTForDeploy` in `internal/cli/root_test.go`) must be updated in the same task the interface changes
- Existing tests must continue to pass: `go test ./... -count=1` and `go vet ./...` stay green after every task
- Periodic/scheduled cleanup (Coolify's `DockerCleanupJob`) is out of scope — the P0 ranking specifies only the `tengiz cleanup` command

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`/`CleanupResult` types, `Cleanup` method on `Manager` interface + `stubManager` |
| `internal/runtime/cleanup.go` | `dockerRuntime.Cleanup`, pure helpers `buildCleanupArgs()` + `extractReclaimed()` |
| `internal/runtime/cleanup_test.go` | Tests for stub, arg building, reclaim parsing |
| `internal/cli/root.go` | New `cleanupCmd` cobra command, registration + flags in `init()` |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy`; CLI registration/flag/RunE tests |
| `README.md` | Feature bullet + command section |
| `AGENTS.md` | CLI table row |

No new files created. Changes touch 6 existing files (3 code, 2 test, 2 docs — `root_test.go` and `README.md` overlap counts).

---

### Task 1: Add `CleanupOptions`/`CleanupResult` types, `Manager` interface method, stub, and `mockRTForDeploy` update

**Files:**
- Modify: `internal/runtime/runtime.go:18-49` — add types near `RunOptions` and `Cleanup` to the `Manager` interface
- Modify: `internal/runtime/runtime.go:113-122` — add stub implementation
- Modify: `internal/cli/root_test.go:69-100` — add `Cleanup` to `mockRTForDeploy`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `type CleanupOptions struct { All, Volumes bool }`, `type CleanupResult struct { Reclaimed string }`, `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`, `(*stubManager).Cleanup` returns `CleanupResult{}, nil`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.Reclaimed != "" {
		t.Errorf("Reclaimed = %q, want empty", res.Reclaimed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubCleanup" -v -count=1`

Expected: FAIL — compile error `stubManager has no method Cleanup`. (Note: this is the expected TDD red state; the whole module will also fail to compile until Step 3 adds the method to `mockRTForDeploy`.)

- [ ] **Step 3: Add types and interface method in `internal/runtime/runtime.go`**

After the `RunOptions` struct (line ~29), add:

```go
type CleanupOptions struct {
	All     bool
	Volumes bool
}

type CleanupResult struct {
	Reclaimed string
}
```

Add to the `Manager` interface, after the `KeepLastNImages` line (line ~36):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add the stub method after the existing `KeepLastNImages` stub (line ~118):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 4: Update `mockRTForDeploy` in `internal/cli/root_test.go`**

Add after the existing `KeepLastNImages` mock method (line ~99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... -count=1`

Expected: PASS (all runtime and cli package tests compile and pass)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface and stub"
```

---

### Task 2: Implement `dockerRuntime.Cleanup` with `buildCleanupArgs` and `extractReclaimed`

**Files:**
- Modify: `internal/runtime/cleanup.go` — add helpers + method
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult` from Task 1
- Produces: `buildCleanupArgs(opts CleanupOptions) []string` (pure), `extractReclaimed(output string) string` (pure), `(*dockerRuntime).Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`, and a compile-time assertion `var _ Manager = (*dockerRuntime)(nil)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestBuildCleanupArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     CleanupOptions
		expected []string
	}{
		{
			name:     "default",
			opts:     CleanupOptions{},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all",
			opts:     CleanupOptions{All: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a"},
		},
		{
			name:     "volumes",
			opts:     CleanupOptions{Volumes: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "--volumes"},
		},
		{
			name:     "all and volumes",
			opts:     CleanupOptions{All: true, Volumes: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a", "--volumes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCleanupArgs(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildCleanupArgs() len = %d, want %d; got %v, want %v", len(got), len(tt.expected), got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("buildCleanupArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestExtractReclaimed(t *testing.T) {
	output := "Deleted Containers:\n...\n\nTotal reclaimed space: 3.407GB\n"
	if got := extractReclaimed(output); got != "3.407GB" {
		t.Errorf("extractReclaimed() = %q, want %q", got, "3.407GB")
	}
	if got := extractReclaimed("nothing to delete\n"); got != "" {
		t.Errorf("extractReclaimed() = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestBuildCleanupArgs|TestExtractReclaimed" -v -count=1`

Expected: FAIL — compile error `undefined: buildCleanupArgs`, `undefined: extractReclaimed`

- [ ] **Step 3: Write minimal implementation in `internal/runtime/cleanup.go`**

Add at the end of the file (after the `KeepLastNImages` method, line ~59):

```go
func buildCleanupArgs(opts CleanupOptions) []string {
	args := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	return args
}

func extractReclaimed(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
		}
	}
	return ""
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	args := buildCleanupArgs(opts)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return CleanupResult{}, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return CleanupResult{Reclaimed: extractReclaimed(string(out))}, nil
}
```

All imports (`context`, `fmt`, `os/exec`, `strings`) are already present in `cleanup.go`. Add a compile-time assertion after the `dockerRuntime` type in `internal/runtime/docker.go` (after line 79):

```go
var _ Manager = (*dockerRuntime)(nil)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestBuildCleanupArgs|TestExtractReclaimed|TestStubCleanup" -v -count=1`

Expected: PASS

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 5: Manual verification with Docker (optional, requires Docker daemon)**

Run: `docker system prune -f --filter label!=tengiz-app`

Expected: outputs deleted-objects summary ending with a `Total reclaimed space: ...` line, and no container with the `tengiz-app` label is listed under `Deleted Containers`.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/docker.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement label-protected docker system prune cleanup"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go:67` — register `cleanupCmd` + flags in `init()`
- Modify: `internal/cli/root.go:1164` — define `cleanupCmd` before `var gitCmd`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions{All, Volumes bool}`, `runtime.CleanupResult{Reclaimed string}` from Tasks 1-2
- Produces: `cleanupCmd *cobra.Command` (name `cleanup`, flags `--all`, `--volumes`), registered on `rootCmd`

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, flag := range []string{"all", "volumes"} {
		if flags.Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupRunEFlags(t *testing.T) {
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()

	called := false
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		if !all {
			t.Error("all = false, want true")
		}
		if !volumes {
			t.Error("volumes = false, want true")
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--all", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupCommandFlags|TestCleanupRunEFlags" -v -count=1`

Expected: FAIL — `cleanup command not registered` and `cleanupCmd` undefined (compile error in the flag/RunE tests)

- [ ] **Step 3: Define the command in `internal/cli/root.go`**

Insert `var cleanupCmd = &cobra.Command{...}` between the end of `runCmd` (line ~1162) and `var gitCmd` (line ~1164):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to free disk space",
	Long:  "Runs a label-protected `docker system prune`. Removes stopped containers, unused networks, dangling images, and build cache. Tengiz-managed containers (labeled tengiz-app) are never removed.",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		res, err := rt.Cleanup(context.Background(), runtime.CleanupOptions{
			All:     all,
			Volumes: volumes,
		})
		if err != nil {
			return err
		}
		if res.Reclaimed != "" {
			fmt.Printf("[tengiz] cleanup complete: reclaimed %s\n", res.Reclaimed)
		} else {
			fmt.Println("[tengiz] cleanup complete")
		}
		return nil
	},
}
```

- [ ] **Step 4: Register command + flags in `init()` in `internal/cli/root.go`**

After `rootCmd.AddCommand(runCmd)` (line ~67), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones (removes old Tengiz rollback images)")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupCommandFlags|TestCleanupRunEFlags" -v -count=1`

Expected: PASS

Run: `go test ./... -count=1`

Expected: All PASS (except possibly the known slow proxy TCP tests and time-sensitive idle tests)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command for label-protected docker system prune"
```

---

### Task 4: Update documentation (README.md + AGENTS.md)

**Files:**
- Modify: `README.md` — Features bullet list + new command section
- Modify: `AGENTS.md` — CLI table row

**Interfaces:**
- Consumes: `tengiz cleanup [--all] [--volumes]` command from Task 3

- [ ] **Step 1: Add the feature bullet in `README.md`**

In the Features bullet list (after the "**Deployment history**" bullet, line ~20), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` reclaims disk space with a label-protected `docker system prune` (stopped containers, unused networks, dangling images, build cache). Tengiz-managed containers are never removed.
```

- [ ] **Step 2: Add the command section in `README.md`**

After the `### tengiz rollback <app>` section (lines ~230-237, just before `### tengiz domain` at line 238), add:

```markdown
### `tengiz cleanup [--all] [--volumes]`

Run a label-protected `docker system prune` to free disk space. Removes stopped containers, unused networks, dangling images, and build cache. Containers managed by Tengiz (labeled `tengiz-app`) are never removed.

| Flag | Description |
|------|-------------|
| `--all` | Also remove all unused images, not just dangling ones. **Caution:** removes old `tengiz-apps/<name>:<tag>` rollback images kept by the retention policy. |
| `--volumes` | Also remove unused named volumes. **Caution:** affects volumes from any tool, not just Tengiz. Off by default. |

Example:

```bash
tengiz cleanup            # safe prune, keeps Tengiz containers + rollback images
tengiz cleanup --volumes  # also prune unused named volumes
```
```

- [ ] **Step 3: Add the CLI table row in `AGENTS.md`**

In the CLI code block (after `tengiz logs ...` line), add:

```bash
tengiz cleanup [--all] [--volumes] → label-protected docker system prune to free disk space
```

- [ ] **Step 4: Verify everything still builds and passes**

Run: `go build -o tengiz . && go test ./... -count=1 && go vet ./...`

Expected: build succeeds, all tests pass, vet clean

- [ ] **Step 5: Self-review against spec**

Check against `docs/FUTURES_FEATURES.md` #6 "Docker Housekeeping":
- "Label-based `docker system prune`" ✅ (Task 2 — `--filter label!=tengiz-app`)
- "`tengiz cleanup`" command ✅ (Task 3)
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" (label-based filtering protects Tengiz-managed containers) ✅ (global constraint + `label!=tengiz-app`)
- Cleanup of unused volumes/networks/containers/images ✅ (system prune covers all four)

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage:** The P0 ranking entry (#6) asks for exactly two things — label-based `docker system prune` (Task 2) and a `tengiz cleanup` command (Task 3). The detailed description's mention of volumes/networks/containers/images is fully covered by `docker system prune`. Periodic scheduled cleanup is explicitly documented as out of scope in Global Constraints.

**2. Placeholder scan:** No TBD/TODO/"similar to" patterns. Every step has complete code. The docker-exec path itself (Task 2 Step 5) is a manual verification step since the codebase does not unit-test docker-exec methods (consistent with `RemoveImage`, `Restart`, etc.); the pure `buildCleanupArgs`/`extractReclaimed` functions carry the automated coverage.

**3. Type consistency:** `CleanupOptions{All, Volumes bool}` and `CleanupResult{Reclaimed string}` are defined in Task 1 and used identically in Tasks 2-3. `buildCleanupArgs(opts CleanupOptions) []string` and `extractReclaimed(output string) string` match across Task 2 test + implementation. `runtime.CleanupOptions`/`runtime.CleanupResult` qualifiers in `root_test.go` and `root.go` match the `runtime` package import alias. `cleanupCmd` is the single command variable name used in both registration and tests. Flag names `all`/`volumes` are consistent between `init()` and all three CLI tests.

**4. Interface ripple:** Adding `Cleanup` to `Manager` breaks `dockerRuntime`, `stubManager`, and `mockRTForDeploy` — all three are updated inside Task 1 (stub + mock) and Task 2 (docker runtime), keeping `go build`/`go test ./...` green at the end of every task.

**Execution Handoff:** Plan complete and saved. Two execution options:

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints
