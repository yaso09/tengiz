# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely prunes unused Docker resources (containers, images, networks, volumes, build cache) to reclaim disk space while always protecting Tengiz-managed containers via label-based filtering.

**Architecture:** The `runtime.Manager` interface gains two methods: `Prune(ctx, PruneOptions)` which runs category-specific `docker * prune -f` commands with a `label!=tengiz-app` filter so scale-to-zero containers (which cold-start from a stopped state) are never removed, and `SystemDF(ctx)` which returns the `docker system df` summary. A pure function `prunePlan(opts)` builds the exact docker command list so it is unit-testable without Docker. The CLI command `tengiz cleanup` wires the runtime, prompts for confirmation only when `--volumes` is requested (data-loss risk), and prints per-category reclaimed-space lines plus the post-cleanup disk usage.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` for the `docker` CLI (same pattern as the existing `dockerRuntime`). No new dependencies.

## Global Constraints

- Tengiz-managed containers carry the `tengiz-app=<name>` label (set in `Create`, `CreateFromImage`, `CreateVersioned`, and `Run` in `internal/runtime/docker.go`) — cleanup MUST exclude them via `--filter label!=tengiz-app`
- `tengiz cleanup` must work with zero arguments on a healthy Docker host (safe default: containers, dangling images, unused networks, build cache)
- `--volumes` is opt-in and MUST require confirmation (type "yes") unless `--yes`/`-y` is passed; volumes can permanently delete data
- Image pruning is dangling-only (`docker image prune -f`, no `-a`) so tagged rollback images retained by `KeepLastNImages` are never removed
- No new external Go dependencies
- All existing tests must continue to pass (`go test ./... -count=1`)
- Commits follow the repo style: `feat:` prefix, lowercase imperative subject (see `git log --oneline`)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneResult` types; add `Prune` and `SystemDF` to the `Manager` interface; add no-op stub implementations |
| `internal/runtime/prune.go` | New file: `pruneStep` struct, `prunePlan(opts)` pure builder, `dockerRuntime.Prune`, `dockerRuntime.SystemDF` |
| `internal/runtime/prune_test.go` | New file: tests for `prunePlan` arg construction + stub methods |
| `internal/idle/idle_test.go` | Add `Prune`/`SystemDF` to the `mockRuntime` (interface change fix) |
| `internal/proxy/proxy_test.go` | Add `Prune`/`SystemDF` to the `mockRuntime` (interface change fix) |
| `internal/cli/root_test.go` | Add `Prune`/`SystemDF` to `mockRTForDeploy` (interface change fix) |
| `internal/cli/cleanup.go` | New file: `cleanupCmd`, `requestVolumeConfirmation` helper, `newCleanupRuntime` injectable constructor |
| `internal/cli/root.go` | Register `cleanupCmd` + its flags in `init()` |
| `internal/cli/cleanup_test.go` | New file: registration, flags, confirmation helper, command flow with mock runtime |
| `README.md` | Add `tengiz cleanup` to the CLI Reference (UI/UX change — required by AGENTS.md) |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark Docker Housekeeping (#6) as implemented |

---

### Task 1: Add `Prune` and `SystemDF` to the runtime Manager

**Files:**
- Modify: `internal/runtime/runtime.go` — types after `RunOptions` (line 29), interface methods after `KeepLastNImages` (line 36), stub methods after `KeepLastNImages` stub (line 119)
- Create: `internal/runtime/prune.go`
- Create: `internal/runtime/prune_test.go`
- Modify: `internal/idle/idle_test.go:34` (after the `Run` mock)
- Modify: `internal/proxy/proxy_test.go:35` (after the `Run` mock)
- Modify: `internal/cli/root_test.go:100` (after the `Run` mock)

**Interfaces:**
- Consumes: existing `Manager` interface, `dockerRuntime`, `stubManager` in `internal/runtime/runtime.go`
- Produces:
  - `type PruneOptions struct { Volumes bool }`
  - `type PruneResult struct { Containers, Images, Networks, Volumes, BuildCache string }`
  - `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`
  - `Manager.SystemDF(ctx context.Context) (string, error)`
  - `prunePlan(opts PruneOptions) []pruneStep` where `pruneStep{label string, args []string}`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	result, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if result.Containers != "" || result.Images != "" || result.Networks != "" || result.Volumes != "" || result.BuildCache != "" {
		t.Errorf("expected empty PruneResult, got %+v", result)
	}
}

func TestStubSystemDF(t *testing.T) {
	m := NewStub()
	out, err := m.SystemDF(context.Background())
	if err != nil {
		t.Fatalf("SystemDF() error = %v", err)
	}
	if out != "" {
		t.Errorf("SystemDF() = %q, want empty string", out)
	}
}

func TestPrunePlanDefaultLabels(t *testing.T) {
	steps := prunePlan(PruneOptions{})
	want := []string{"containers", "images", "networks", "build-cache"}
	if len(steps) != len(want) {
		t.Fatalf("len(steps) = %d, want %d: %+v", len(steps), len(want), steps)
	}
	for i, label := range want {
		if steps[i].label != label {
			t.Errorf("steps[%d].label = %q, want %q", i, steps[i].label, label)
		}
	}
}

func TestPrunePlanProtectsTengizContainers(t *testing.T) {
	steps := prunePlan(PruneOptions{})
	for _, step := range steps {
		if step.label != "containers" {
			continue
		}
		joined := strings.Join(step.args, " ")
		if !strings.Contains(joined, "--filter") || !strings.Contains(joined, "label!=tengiz-app") {
			t.Errorf("container prune args %q must exclude tengiz-app label", joined)
		}
	}
}

func TestPrunePlanWithVolumesAppendsVolumeStep(t *testing.T) {
	steps := prunePlan(PruneOptions{Volumes: true})
	found := false
	for _, step := range steps {
		if step.label == "volumes" {
			found = true
			joined := strings.Join(step.args, " ")
			if !strings.Contains(joined, "label!=tengiz-app") {
				t.Errorf("volume prune args %q must exclude tengiz-app label", joined)
			}
		}
	}
	if !found {
		t.Fatal("expected a volumes prune step when PruneOptions.Volumes is true")
	}
}

func TestPrunePlanWithoutVolumesOmitsVolumeStep(t *testing.T) {
	for _, step := range prunePlan(PruneOptions{}) {
		if step.label == "volumes" {
			t.Fatal("default prune plan must not include volumes")
		}
	}
}

func TestPrunePlanExactArgs(t *testing.T) {
	steps := prunePlan(PruneOptions{Volumes: true})
	want := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-f"},
		{"network", "prune", "-f"},
		{"builder", "prune", "-f"},
		{"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
	}
	if len(steps) != len(want) {
		t.Fatalf("len(steps) = %d, want %d", len(steps), len(want))
	}
	for i := range want {
		if len(steps[i].args) != len(want[i]) {
			t.Fatalf("step %d args = %v, want %v", i, steps[i].args, want[i])
		}
		for j := range want[i] {
			if steps[i].args[j] != want[i][j] {
				t.Fatalf("step %d arg %d = %q, want %q", i, j, steps[i].args[j], want[i][j])
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestStubSystemDF|TestPrunePlan" -v -count=1`

Expected: FAIL (build error) with `undefined: PruneOptions`, `undefined: Prune`, `undefined: SystemDF`, `undefined: prunePlan`

- [ ] **Step 3: Add types, interface methods, and stub implementations to `internal/runtime/runtime.go`**

After the `RunOptions` struct (line 29) add:

```go
type PruneOptions struct {
	// Volumes also prunes unused Docker volumes. Off by default because it
	// permanently deletes data not referenced by any container.
	Volumes bool
}

type PruneResult struct {
	Containers string
	Images     string
	Networks   string
	Volumes    string
	BuildCache string
}
```

After the `KeepLastNImages` line in the `Manager` interface (line 36) add:

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
	SystemDF(ctx context.Context) (string, error)
```

After the `KeepLastNImages` stub method (line 119) add:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}

func (m *stubManager) SystemDF(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 4: Implement the Docker runtime prune logic**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const protectLabelFilter = "label!=tengiz-app"

type pruneStep struct {
	label string
	args  []string
}

func prunePlan(opts PruneOptions) []pruneStep {
	steps := []pruneStep{
		{label: "containers", args: []string{"container", "prune", "-f", "--filter", protectLabelFilter}},
		{label: "images", args: []string{"image", "prune", "-f"}},
		{label: "networks", args: []string{"network", "prune", "-f"}},
		{label: "build-cache", args: []string{"builder", "prune", "-f"}},
	}
	if opts.Volumes {
		steps = append(steps, pruneStep{
			label: "volumes",
			args:  []string{"volume", "prune", "-f", "--filter", protectLabelFilter},
		})
	}
	return steps
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var result PruneResult
	for _, step := range prunePlan(opts) {
		cmd := exec.CommandContext(ctx, "docker", step.args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return result, fmt.Errorf("docker %s prune: %w\n%s", step.label, err, string(out))
		}
		line := strings.TrimSpace(string(out))
		switch step.label {
		case "containers":
			result.Containers = line
		case "images":
			result.Images = line
		case "networks":
			result.Networks = line
		case "volumes":
			result.Volumes = line
		case "build-cache":
			result.BuildCache = line
		}
	}
	return result, nil
}

func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
```

- [ ] **Step 5: Run runtime tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestStubSystemDF|TestPrunePlan" -v -count=1`

Expected: PASS (7 subtests). The package still compiles because `dockerRuntime` now implements the full `Manager` interface.

- [ ] **Step 6: Update the other `Manager` mocks so the whole module compiles**

Adding methods to the `Manager` interface breaks the test mocks in three other packages. Add these two lines to each mock (place them right after the existing `Run` method):

In `internal/idle/idle_test.go` after line 34:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
func (m *mockRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

In `internal/proxy/proxy_test.go` after line 35:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
func (m *mockRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

In `internal/cli/root_test.go` after line 100:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
func (m *mockRTForDeploy) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

- [ ] **Step 7: Run the full test suite and vet**

Run: `go test ./... -count=1`

Expected: PASS (all packages build and tests pass)

Run: `go vet ./...`

Expected: no output (exit 0)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune.go internal/runtime/prune_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go internal/cli/root_test.go
git commit -m "feat: add label-protected docker prune operations to runtime"
```

---

### Task 2: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:76-88` — register `cleanupCmd` and its flags in `init()`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager.Prune`, `runtime.Manager.SystemDF`, `runtime.PruneOptions`, `runtime.PruneResult` (from Task 1); `runtime.NewDocker()` (real constructor); `getEnv` in `internal/cli/root.go`
- Produces: `cleanupCmd *cobra.Command` (registered at root as `tengiz cleanup`), `requestVolumeConfirmation(r io.Reader, w io.Writer) (bool, error)`, `newCleanupRuntime func() (runtime.Manager, error)` (package var, overridable in tests)

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

type mockCleanupRuntime struct {
	runtime.Manager
	pruneCalls []runtime.PruneOptions
	dfCalls    int
}

func (m *mockCleanupRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	m.pruneCalls = append(m.pruneCalls, opts)
	return runtime.PruneResult{Containers: "Total reclaimed space: 0B"}, nil
}

func (m *mockCleanupRuntime) SystemDF(ctx context.Context) (string, error) {
	m.dfCalls++
	return "TYPE\tTOTAL\tACTIVE\tSIZE\tRECLAIMABLE\n", nil
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	if cleanupCmd.Flags().Lookup("yes") == nil {
		t.Fatal("cleanup missing --yes flag")
	}
	if cleanupCmd.Flags().Lookup("volumes") == nil {
		t.Fatal("cleanup missing --volumes flag")
	}
}

func TestRequestVolumeConfirmationYes(t *testing.T) {
	var out bytes.Buffer
	ok, err := requestVolumeConfirmation(strings.NewReader("yes\n"), &out)
	if err != nil {
		t.Fatalf("requestVolumeConfirmation() error = %v", err)
	}
	if !ok {
		t.Error("expected confirmation for 'yes'")
	}
}

func TestRequestVolumeConfirmationNo(t *testing.T) {
	var out bytes.Buffer
	ok, err := requestVolumeConfirmation(strings.NewReader("no\n"), &out)
	if err != nil {
		t.Fatalf("requestVolumeConfirmation() error = %v", err)
	}
	if ok {
		t.Error("expected no confirmation for 'no'")
	}
}

func TestCleanupDefaultRunsSafePrune(t *testing.T) {
	orig := newCleanupRuntime
	defer func() { newCleanupRuntime = orig }()

	mock := &mockCleanupRuntime{}
	newCleanupRuntime = func() (runtime.Manager, error) { return mock, nil }

	rootCmd.SetArgs([]string{"cleanup"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("cleanup Execute() error = %v", err)
	}

	if len(mock.pruneCalls) != 1 {
		t.Fatalf("Prune called %d times, want 1", len(mock.pruneCalls))
	}
	if mock.pruneCalls[0].Volumes {
		t.Error("default cleanup passed Volumes=true, want false")
	}
	if mock.dfCalls != 1 {
		t.Errorf("SystemDF called %d times, want 1", mock.dfCalls)
	}
}

func TestCleanupWithVolumesAndYes(t *testing.T) {
	orig := newCleanupRuntime
	defer func() { newCleanupRuntime = orig }()

	mock := &mockCleanupRuntime{}
	newCleanupRuntime = func() (runtime.Manager, error) { return mock, nil }

	rootCmd.SetArgs([]string{"cleanup", "--volumes", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("cleanup Execute() error = %v", err)
	}

	if len(mock.pruneCalls) != 1 {
		t.Fatalf("Prune called %d times, want 1", len(mock.pruneCalls))
	}
	if !mock.pruneCalls[0].Volumes {
		t.Error("cleanup --volumes --yes did not pass Volumes=true")
	}
}

func TestCleanupVolumesCancelledWithoutConfirmation(t *testing.T) {
	orig := newCleanupRuntime
	defer func() { newCleanupRuntime = orig }()

	mock := &mockCleanupRuntime{}
	newCleanupRuntime = func() (runtime.Manager, error) { return mock, nil }

	origStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	cleanup := func() { os.Stdin = origStdin }
	defer cleanup()
	go func() {
		w.Write([]byte("no\n"))
		w.Close()
	}()

	rootCmd.SetArgs([]string{"cleanup", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("cleanup Execute() error = %v", err)
	}

	if len(mock.pruneCalls) != 0 {
		t.Errorf("Prune called %d times, want 0 after cancelled confirmation", len(mock.pruneCalls))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup|TestRequestVolumeConfirmation" -v -count=1`

Expected: FAIL (build error) with `undefined: cleanupCmd`, `undefined: requestVolumeConfirmation`, `undefined: newCleanupRuntime`

- [ ] **Step 3: Implement the command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var newCleanupRuntime = func() (runtime.Manager, error) { return runtime.NewDocker() }

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources and reclaim disk space",
	Long: `Removes stopped non-Tengiz containers, dangling images, unused networks, and
build cache. Containers and volumes labeled tengiz-app are always protected, so
deployed apps keep working (including scale-to-zero cold starts).

Use --volumes to also prune unused volumes; it is dangerous (data is deleted)
and requires confirmation unless --yes is given.

Examples:
  tengiz cleanup                # safe default: containers, images, networks, build cache
  tengiz cleanup --volumes      # also prune unused volumes (prompts for confirmation)
  tengiz cleanup --volumes --yes  # prune volumes without prompting`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		volumes, _ := cmd.Flags().GetBool("volumes")
		yes, _ := cmd.Flags().GetBool("yes")

		if volumes && !yes {
			confirmed, err := requestVolumeConfirmation(os.Stdin, os.Stdout)
			if err != nil {
				return fmt.Errorf("read confirmation: %w", err)
			}
			if !confirmed {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		rt, err := newCleanupRuntime()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Prune(cmd.Context(), runtime.PruneOptions{Volumes: volumes})
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] cleanup results:")
		for _, line := range []struct {
			label string
			value string
		}{
			{"containers", result.Containers},
			{"images", result.Images},
			{"networks", result.Networks},
			{"build-cache", result.BuildCache},
			{"volumes", result.Volumes},
		} {
			if line.value != "" {
				fmt.Printf("  %-12s %s\n", line.label, line.value)
			}
		}

		df, err := rt.SystemDF(cmd.Context())
		if err != nil {
			fmt.Printf("[tengiz] (disk usage unavailable: %v)\n", err)
		} else if df != "" {
			fmt.Println("[tengiz] disk usage after cleanup:")
			fmt.Print(df)
		}
		return nil
	},
}

func requestVolumeConfirmation(r io.Reader, w io.Writer) (bool, error) {
	fmt.Fprintln(w, "WARNING: pruning volumes permanently deletes data not referenced by any container.")
	fmt.Fprint(w, "Type 'yes' to continue: ")
	var answer string
	if _, err := fmt.Fscanln(r, &answer); err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(answer), "yes"), nil
}
```

- [ ] **Step 4: Register the command in `internal/cli/root.go`**

In `init()`, before the closing brace (after the `webhookCmd.Flags()` block at line 88), add:

```go
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip the volume confirmation prompt")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused Docker volumes (dangerous; requires confirmation unless --yes)")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run CLI tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestRequestVolumeConfirmation" -v -count=1`

Expected: PASS (8 subtests)

- [ ] **Step 6: Run the full test suite and vet**

Run: `go test ./... -count=1`

Expected: PASS

Run: `go vet ./...`

Expected: no output (exit 0)

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 3: Update documentation

**Files:**
- Modify: `README.md:237` — insert a `tengiz cleanup` section after `tengiz rollback`
- Modify: `AGENTS.md:59` — add the command to the CLI list
- Modify: `docs/FUTURES_FEATURES.md:19` — mark feature #6 implemented

**Interfaces:**
- Consumes: the finalized `tengiz cleanup [-y] [--volumes]` command surface from Task 2

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

Insert after line 237 (end of the `tengiz rollback <app>` section) and before `### tengiz domain`:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space.

```text
tengiz cleanup [-y] [--volumes]
```

| Flag | Description |
|------|-------------|
| `-y, --yes` | Skip the volume confirmation prompt |
| `--volumes` | Also prune unused Docker volumes (dangerous — permanently deletes data not referenced by any container) |

Removes stopped containers that are **not** managed by Tengiz (containers labeled `tengiz-app` are protected so scale-to-zero cold starts keep working), dangling images, unused networks, and the build cache. Tagged rollback images are never removed. Prints per-category reclaimed space and a `docker system df` summary after cleanup.

Example:
```bash
tengiz cleanup
tengiz cleanup --volumes --yes   # aggressive: also prune volumes
```
```

- [ ] **Step 2: Add the command to `AGENTS.md`**

In the CLI block (line 59), after the `tengiz preview deploy` line, add:

```
tengiz cleanup [-y] [--volumes] → prune unused Docker resources (label-protected; volumes require confirmation)
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table, change the status marker on the Docker Housekeeping row (line 19):

From: `| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based \`docker system prune\`. \`tengiz cleanup\`. |`

To: `| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based \`docker system prune\`. \`tengiz cleanup\`. |`

Then, in the `## Docker Housekeeping (Otomatik Temizlik)` feature section (line 377-381), add a status line after the `**Why add to Tengiz:**` line:

```
- **Status:** ✅ Implemented (2026-08-17)
```

- [ ] **Step 4: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup docker housekeeping command"
```

---

## Self-Review

**Spec coverage** (`docs/FUTURES_FEATURES.md` #6):
- `tengiz cleanup` command → Task 2
- Label-based protection of Tengiz-managed containers → `prunePlan` uses `--filter label!=tengiz-app` (Task 1), tested by `TestPrunePlanProtectsTengizContainers`
- Cleanup of unused containers/images/networks/volumes/build cache → `prunePlan` categories; volumes opt-in via `--volumes` (Task 1 + Task 2)
- Disk space reporting → `SystemDF` + post-cleanup summary in the CLI (Tasks 1 + 2)

**Placeholder scan:** every step contains exact code, file paths, and expected commands/output. No "TBD", "implement later", or "add error handling" phrasing.

**Type consistency:** `PruneOptions{Volumes bool}`, `PruneResult{Containers/Images/Networks/Volumes/BuildCache string}`, `Prune(ctx, PruneOptions) (PruneResult, error)`, `SystemDF(ctx) (string, error)`, `prunePlan(PruneOptions) []pruneStep`, `requestVolumeConfirmation(io.Reader, io.Writer) (bool, error)`, and `newCleanupRuntime func() (runtime.Manager, error)` are used with identical names and signatures in every task.

**Buildability note:** Task 1 adds the interface methods and stub/`dockerRuntime` implementations together so `internal/runtime` compiles before the test run in Step 5; the mock updates in Step 6 are the only cross-package fallout and are listed with exact insertion points.
