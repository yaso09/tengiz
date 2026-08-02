# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a label-based `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused networks, build cache) while never touching containers managed by Tengiz.

**Architecture:** A new `Prune(ctx, PruneOptions)` method on the `runtime.Manager` interface wraps `docker system prune -f` with `--filter label!=tengiz-app` and `--filter label!=tengiz-env` so scale-to-zero stopped Tengiz containers are always protected. The CLI `cleanupCmd` reads `--all` (adds `-a` to remove all unused images) and prints the docker output. Env-aware via the global `--env` flag, matching every other command.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` interface + `os/exec` docker CLI. No new external dependencies.

## Global Constraints

- Tengiz-managed containers carry labels `tengiz-app=<appname>` and `tengiz-env=<env>` (`labelKey`/`envLabelKey` in `internal/runtime/docker.go:76-77`) — these are NEVER pruned
- Prune must use `docker system prune -f` (non-interactive) with `--filter label!=tengiz-app` and `--filter label!=tengiz-env`
- `--all` appends `-a` to `docker system prune` (removes all unused images, not just dangling)
- `Prune` returns the docker command's combined output as `(string, error)` so the CLI can echo it
- `PruneOptions{ All bool }` — only one option; no other flags in scope
- New CLI command name is exactly `cleanup`; short help string starts with "Prune unused Docker resources"
- Adding a method to the `runtime.Manager` interface REQUIRES updating the stub and all test mocks (4 sites) or the repo won't compile
- All existing tests must continue to pass without modification
- No new external dependencies required

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions` type, `Prune` method to `Manager` interface, stub impl |
| `internal/runtime/cleanup.go` | `dockerRuntime.Prune` + pure `buildPruneArgs` helper |
| `internal/runtime/cleanup_test.go` | Tests for `buildPruneArgs` + stub `Prune` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` + CLI registration/flag tests |
| `internal/cli/cleanup.go` | NEW — `cleanupCmd` cobra command + its own `init()` that registers the command and `--all` flag on `rootCmd` (follows the `preview.go` pattern, `internal/cli/preview.go:83-88`) |
| `README.md` | Add `### \`tengiz cleanup\`` to CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as Implemented |

---

### Task 1: Add `Prune` to the runtime interface, stub, and docker implementation

**Files:**
- Modify: `internal/runtime/runtime.go` (Manager interface ~line 31-49, stubManager ~line 51-123)
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `labelKey` (`"tengiz-app"`), `envLabelKey` (`"tengiz-env"`) from `internal/runtime/docker.go:76-77` (same package, no import needed)
- Produces: `runtime.PruneOptions{ All bool }`, `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (string, error)`, `dockerRuntime.Prune`, unexported `buildPruneArgs(opts PruneOptions) []string` — later tasks rely on `Prune` and `PruneOptions`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		opts PruneOptions
		want []string
	}{
		{
			name: "default keeps tengiz containers",
			opts: PruneOptions{All: false},
			want: []string{
				"system", "prune", "-f",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env",
			},
		},
		{
			name: "all appends -a",
			opts: PruneOptions{All: true},
			want: []string{
				"system", "prune", "-f",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env",
				"-a",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildPruneArgs(tc.opts)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildPruneArgs(%+v) = %v, want %v", tc.opts, got, tc.want)
			}
		})
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	out, err := m.Prune(context.Background(), PruneOptions{All: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if out != "" {
		t.Errorf("stub Prune() = %q, want empty string", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestStubPrune" -v -count=1`

Expected: FAIL with `undefined: PruneOptions` and `undefined: buildPruneArgs`

- [ ] **Step 3: Add `PruneOptions` and `Prune` to the interface + stub in `internal/runtime/runtime.go`**

Add the options type above the `Manager` interface:
```go
type PruneOptions struct {
	All bool
}
```

Add the method to the `Manager` interface, right after `KeepLastNImages`:
```go
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts PruneOptions) (string, error)
```

Add the stub implementation in the same file, right after `KeepLastNImages`:
```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (string, error) {
	return "", nil
}
```

- [ ] **Step 4: Implement `buildPruneArgs` + `Prune` in `internal/runtime/cleanup.go`**

Append to `internal/runtime/cleanup.go` (after the existing `KeepLastNImages`):
```go
func buildPruneArgs(opts PruneOptions) []string {
	args := []string{
		"system", "prune", "-f",
		"--filter", fmt.Sprintf("label!=%s", labelKey),
		"--filter", fmt.Sprintf("label!=%s", envLabelKey),
	}
	if opts.All {
		args = append(args, "-a")
	}
	return args
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildPruneArgs(opts)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return string(out), nil
}
```

`fmt` and `os/exec` are already imported by `internal/runtime/cleanup.go`. Verify with `goimports` if needed.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestStubPrune" -v -count=1`

Expected: PASS (2/2)

- [ ] **Step 6: Run the full runtime package test suite**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: PASS (the dockerRuntime.Prune calls real docker only when invoked, so no new docker dependency in unit tests)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add Prune for label-based docker system prune"
```

---

### Task 2: Update the test mocks that implement `runtime.Manager`

Adding `Prune` to the `Manager` interface breaks compilation of the three mock types in other packages. This task adds the method to each.

**Files:**
- Modify: `internal/proxy/proxy_test.go:34`
- Modify: `internal/idle/idle_test.go:33`
- Modify: `internal/cli/root_test.go:99`

**Interfaces:**
- Consumes: `runtime.PruneOptions` (from Task 1)
- Produces: nothing new — keeps existing mocks conforming to `runtime.Manager`

- [ ] **Step 1: Add the method to `internal/proxy/proxy_test.go`**

Insert after the `KeepLastNImages` line:
```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (string, error) { return "", nil }
```

- [ ] **Step 2: Add the method to `internal/idle/idle_test.go`**

Insert after the `KeepLastNImages` line:
```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (string, error) { return "", nil }
```

- [ ] **Step 3: Add the method to `internal/cli/root_test.go`**

Insert after the `KeepLastNImages` line (inside `mockRTForDeploy`):
```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (string, error) { return "", nil }
```

- [ ] **Step 4: Run all tests to verify compilation**

Run: `go test ./... -count=1`

Expected: PASS — every package compiles and all existing tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "test: add Prune to runtime.Manager test mocks"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go` (contains `cleanupCmd` + its own `init()` registering the command and `--all` flag on `rootCmd`, mirroring `internal/cli/preview.go:83-88`)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.Manager.Prune` (Task 1)
- Produces: `cleanupCmd` cobra command registered on `rootCmd` with a `--all` bool flag; no other packages reference it

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/root_test.go
func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	flag := cmd.Flags().Lookup("all")
	if flag == nil {
		t.Error("cleanup command missing --all flag")
	}
}

func TestCleanupCmdDryRunFlagAbsent(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if flag := cmd.Flags().Lookup("dry-run"); flag != nil {
		t.Error("cleanup should not have a --dry-run flag (docker system prune has no dry-run mode)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCmdRegistered|TestCleanupCmdDryRunFlagAbsent" -v -count=1`

Expected: FAIL with `cleanup command not registered: unknown command "cleanup"`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources",
	Long: `Runs label-based "docker system prune". Removes stopped containers, dangling
images, unused networks, and build cache while protecting every container
managed by Tengiz (labels tengiz-app / tengiz-env). Use --all to also remove
all unused images, not just dangling ones.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		out, err := rt.Prune(cmd.Context(), runtime.PruneOptions{All: all})
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().BoolP("all", "a", false, "remove all unused images, not just dangling ones")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanupCmdRegistered|TestCleanupCmdDryRunFlagAbsent" -v -count=1`

Expected: PASS (2/2)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Update documentation

**Files:**
- Modify: `README.md` (CLI Reference; add `### \`tengiz cleanup\`` after the `tengiz rm` section which ends at line 229)
- Modify: `docs/FUTURES_FEATURES.md` (row #6 line 19: `⬜` → `✅`; add an entry to the Implemented Features table)

**Interfaces:**
- Consumes: nothing
- Produces: documentation only

- [ ] **Step 1: Add the `cleanup` section to `README.md`**

Insert after the `tengiz rm` section (line 228-229), before `### \`tengiz rollback <app>\``:

```markdown
### `tengiz cleanup`

Prune unused Docker resources with a label-based `docker system prune`.

| Flag | Description |
|------|-------------|
| `-a`, `--all` | Also remove all unused images, not just dangling ones |

Removes stopped containers, dangling images, unused networks, and build cache. Containers managed by Tengiz (labeled `tengiz-app` / `tengiz-env`) are always protected — including stopped scale-to-zero containers. Run `tengiz cleanup --env <env>` to scope to an environment.
```

- [ ] **Step 2: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

Change line 19 from:
```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```
to:
```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the Implemented Features table (after the "Webhook ile Otomatik Deploy" row at line 253):
```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-02) |
```

- [ ] **Step 3: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

### Task 5: Run the full verification suite

**Files:**
- None — verification only

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: PASS — every test in every package passes

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`

Expected: no warnings

- [ ] **Step 3: Verify the command is wired end-to-end**

Run: `go build -o /tmp/tengiz . && /tmp/tengiz cleanup --help`

Expected: output shows the `cleanup` command's usage line, `Short`, `Long`, and the `-a, --all` flag

- [ ] **Step 4: Final self-review against the spec**

- [ ] `tengiz cleanup` exists and prints docker output
- [ ] Default run protects Tengiz containers via `label!=tengiz-app` + `label!=tengiz-env`
- [ ] `--all` appends `-a` to `docker system prune`
- [ ] Feature #6 in `docs/FUTURES_FEATURES.md` is marked ✅
- [ ] `README.md` documents the new command
- [ ] No new dependencies added; all mocks compile

- [ ] **Step 5: Commit any remaining changes**

```bash
git add -A
git commit -m "chore: final verification for tengiz cleanup"  # only if there are uncommitted changes
```

---

## Out of Scope (Future Features)

- `--cache` / `--gc` (feature #103: build cache + git GC) — separate plan
- Per-category pruning (feature #56: granular containers/networks/images/volumes/buildx) — separate plan
- Stale container detection (feature #47) — separate plan
- Volumes are never pruned by `docker system prune` by default; a `--volumes` flag is deliberately NOT added
- Scheduled/automatic cleanup (feature #57 monitoring scheduler) — separate plan
