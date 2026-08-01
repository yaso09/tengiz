# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, and networks (optionally volumes) using label-based filtering so Tengiz-managed containers are always preserved.

**Architecture:** A `Prune` method is added to `runtime.Manager` (exec-based `docker system prune` wrapper plus a pure `SystemPruneArgs` builder and `parseReclaimedSpace` parser). A new `internal/cleanup` package wraps `runtime.Manager` through a narrow `Pruner` interface and maps user options → runtime prune options, returning a `Result` with reclaimed space and the commands run. A new `tengiz cleanup` CLI command (`internal/cli/cleanup.go`) wires the two together with `--dry-run`, `--all`, `--volumes` flags. The Docker `label!=tengiz-app` filter excludes all containers carrying the `tengiz-app` label (app containers, preview containers, versioned containers) from pruning.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (docker CLI), existing `runtime.Manager` interface + `runtime.NewStub()`. No new external dependencies.

## Global Constraints

- Tengiz-managed containers MUST always be preserved: the prune filter is exactly `--filter label!=tengiz-app`
- The `docker` CLI is invoked via `os/exec` (no Docker SDK) — matches existing `runtime` package conventions
- `Prune` must be added to the `runtime.Manager` interface AND implemented on both `dockerRuntime` and `stubManager`
- The mock `mockRTForDeploy` in `internal/cli/root_test.go` must gain a `Prune` method to keep the interface satisfied
- Default prune (no flags) runs `docker system prune -f --filter label!=tengiz-app` — safe: stopped non-Tengiz containers, unused networks, dangling images only
- `--all` appends `-a` (all unused images), `--volumes` appends `--volumes` (unused volumes) — these require explicit user opt-in
- `--dry-run` builds the same command args but never executes them
- Command name: `tengiz cleanup` (lowercase, matching the FUTURES_FEATURES.md spec)
- No new files outside: `internal/cleanup/`, `internal/runtime/cleanup.go`, `internal/runtime/runtime.go`, `internal/cli/cleanup.go`, `internal/cli/root_test.go`, README.md, FUTURES_FEATURES.md
- Existing tests must continue to pass without modification (except the additive `mockRTForDeploy` change)
- Follow existing code style: no comments unless required, `fmt.Errorf("...: %w", err)` wrapping, `strings.Join`/`strings.Split` for exec output parsing

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneResult` types; add `Prune` to `Manager` interface; add stub implementation |
| `internal/runtime/cleanup.go` | Add `SystemPruneArgs()` builder, `parseReclaimedSpace()` parser, `dockerRuntime.Prune()` exec impl |
| `internal/runtime/cleanup_test.go` | Tests for `SystemPruneArgs`, `parseReclaimedSpace`, stub `Prune` |
| `internal/cleanup/cleanup.go` | New package: `Pruner` interface, `Manager`, `Options`, `Result`, `Prune()` |
| `internal/cleanup/cleanup_test.go` | Tests for `Manager.Prune` mapping and dry-run |
| `internal/cli/cleanup.go` | New `tengiz cleanup` command + flag registration |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` + cleanup command registration/flag tests |
| `README.md` | Document the `tengiz cleanup` command in CLI Reference + Features |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as implemented |

---

### Task 1: Add `Prune` to `runtime.Manager` (interface, stub, docker impl, args builder, parser)

**Files:**
- Modify: `internal/runtime/runtime.go` — add `PruneOptions`, `PruneResult` after `RunOptions` (line ~29); add `Prune` to `Manager` interface (line ~48); add stub impl after existing stub methods
- Modify: `internal/runtime/cleanup.go` — add `SystemPruneArgs`, `parseReclaimedSpace`, `dockerRuntime.Prune`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `runtime.PruneOptions struct { All, Volumes, DryRun bool }`
  - `runtime.PruneResult struct { Output string; ReclaimedSpace string; DryRun bool; Commands [][]string }`
  - `runtime.SystemPruneArgs(opts PruneOptions) []string` (pure, exported — used by CLI dry-run display)
  - `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
	"context"
	"testing"
)

func TestSystemPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     PruneOptions
		expected []string
	}{
		{
			name:     "default",
			opts:     PruneOptions{},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "all",
			opts:     PruneOptions{All: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a"},
		},
		{
			name:     "volumes",
			opts:     PruneOptions{Volumes: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "--volumes"},
		},
		{
			name:     "all and volumes",
			opts:     PruneOptions{All: true, Volumes: true},
			expected: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app", "-a", "--volumes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SystemPruneArgs(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("SystemPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("SystemPruneArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	output := `Deleted Containers:
abc123def456

Deleted Images:
img1

Total reclaimed space: 1.234GB
`
	got := parseReclaimedSpace(output)
	if got != "1.234GB" {
		t.Fatalf("parseReclaimedSpace() = %q, want %q", got, "1.234GB")
	}
}

func TestParseReclaimedSpaceEmpty(t *testing.T) {
	if got := parseReclaimedSpace("nothing here"); got != "" {
		t.Fatalf("parseReclaimedSpace() = %q, want empty", got)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(res.Commands) != 1 {
		t.Fatalf("Prune() Commands = %v, want 1 command", res.Commands)
	}
	if res.DryRun {
		t.Fatal("Prune() with DryRun=false returned DryRun=true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestSystemPruneArgs|TestParseReclaimedSpace|TestStubPrune" -v -count=1`

Expected: FAIL with `undefined: SystemPruneArgs`, `undefined: parseReclaimedSpace`, `undefined: PruneOptions`

- [ ] **Step 3: Add `PruneOptions`, `PruneResult`, and `Prune` to the `Manager` interface in `internal/runtime/runtime.go`**

After the existing `RunOptions` struct (line ~29), add:

```go
type PruneOptions struct {
	All     bool
	Volumes bool
	DryRun  bool
}

type PruneResult struct {
	Output         string
	ReclaimedSpace string
	DryRun         bool
	Commands       [][]string
}
```

In the `Manager` interface (after the `Run` method, line ~48), add:

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

Add the stub implementation at the end of `internal/runtime/runtime.go`:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{
		DryRun:   opts.DryRun,
		Commands: [][]string{SystemPruneArgs(opts)},
	}, nil
}
```

- [ ] **Step 4: Add `SystemPruneArgs`, `parseReclaimedSpace`, and `dockerRuntime.Prune` to `internal/runtime/cleanup.go`**

`cleanup.go` currently imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`. Add these functions to the end of the file:

```go
const tengizLabelFilter = "label!=tengiz-app"

func SystemPruneArgs(opts PruneOptions) []string {
	args := []string{"system", "prune", "-f", "--filter", tengizLabelFilter}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	return args
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	args := SystemPruneArgs(opts)
	if opts.DryRun {
		return PruneResult{DryRun: true, Commands: [][]string{args}}, nil
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneResult{}, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return PruneResult{
		Output:         string(out),
		ReclaimedSpace: parseReclaimedSpace(string(out)),
		Commands:       [][]string{args},
	}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestSystemPruneArgs|TestParseReclaimedSpace|TestStubPrune" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Run full build to catch interface breakage**

Run: `go build ./...`

Expected: Build succeeds (the `mockRTForDeploy` in `root_test.go` will NOT break a build since test files are excluded, but `gitdeploy`/`preview` only consume the interface so they compile fine)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add Prune to runtime.Manager with label-based docker system prune"
```

---

### Task 2: Create `internal/cleanup` package

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.SystemPruneArgs` from Task 1
- Produces:
  - `cleanup.Pruner interface { Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) }`
  - `cleanup.Options struct { AllImages, Volumes, DryRun bool }`
  - `cleanup.Result struct { ReclaimedSpace string; DryRun bool; Commands [][]string }`
  - `cleanup.New(pruner Pruner) *Manager`
  - `(*Manager).Prune(ctx context.Context, opts Options) (*Result, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cleanup/cleanup_test.go
package cleanup

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

type stubPruner struct {
	pruned bool
}

func (s *stubPruner) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	s.pruned = true
	args := runtime.SystemPruneArgs(opts)
	if opts.DryRun {
		return runtime.PruneResult{DryRun: true, Commands: [][]string{args}}, nil
	}
	return runtime.PruneResult{ReclaimedSpace: "2.5GB", Commands: [][]string{args}}, nil
}

func TestNew(t *testing.T) {
	m := New(&stubPruner{})
	if m == nil {
		t.Fatal("New() returned nil")
	}
}

func TestPruneMapsOptions(t *testing.T) {
	pruner := &stubPruner{}
	m := New(pruner)
	res, err := m.Prune(context.Background(), Options{AllImages: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !pruner.pruned {
		t.Fatal("Prune() did not call the runtime Pruner")
	}
	if res.ReclaimedSpace != "2.5GB" {
		t.Fatalf("ReclaimedSpace = %q, want %q", res.ReclaimedSpace, "2.5GB")
	}
	if res.DryRun {
		t.Fatal("Prune() with DryRun=false returned DryRun=true")
	}
	if len(res.Commands) != 1 {
		t.Fatalf("Commands = %v, want 1 command", res.Commands)
	}
}

func TestPruneDryRun(t *testing.T) {
	pruner := &stubPruner{}
	m := New(pruner)
	res, err := m.Prune(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !res.DryRun {
		t.Fatal("expected DryRun = true")
	}
	if len(res.Commands) != 1 {
		t.Fatalf("Commands = %v, want 1 command", res.Commands)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL with `undefined: New`, `undefined: Options`

- [ ] **Step 3: Write minimal implementation in `internal/cleanup/cleanup.go`**

```go
package cleanup

import (
	"context"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Pruner interface {
	Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error)
}

type Options struct {
	AllImages bool
	Volumes   bool
	DryRun    bool
}

type Result struct {
	ReclaimedSpace string
	DryRun         bool
	Commands       [][]string
}

type Manager struct {
	pruner Pruner
}

func New(pruner Pruner) *Manager {
	return &Manager{pruner: pruner}
}

func (m *Manager) Prune(ctx context.Context, opts Options) (*Result, error) {
	res, err := m.pruner.Prune(ctx, runtime.PruneOptions{
		All:     opts.AllImages,
		Volumes: opts.Volumes,
		DryRun:  opts.DryRun,
	})
	if err != nil {
		return nil, err
	}
	return &Result{
		ReclaimedSpace: res.ReclaimedSpace,
		DryRun:         res.DryRun,
		Commands:       res.Commands,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup package wrapping runtime.Prune"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root_test.go` — add `Prune` method to `mockRTForDeploy`; add command registration/flag tests

**Interfaces:**
- Consumes: `runtime.NewDocker()` (returns `runtime.Manager`, which satisfies `cleanup.Pruner`), `cleanup.New`, `cleanup.Options`, `cleanup.Result` from Task 2
- Produces: `tengiz cleanup [--dry-run] [--all] [--volumes]` command registered on `rootCmd`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/root_test.go — add these test functions at the end of the file
package cli

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
	for _, flag := range []string{"dry-run", "all", "volumes"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCommand" -v -count=1`

Expected: FAIL with `cleanup command not registered` / `undefined: cleanupCmd`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Removes unused Docker containers, images, and networks (optionally volumes).
Tengiz-managed containers are always preserved via label filtering.

Flags:
  --dry-run   show the docker command that would run without executing it
  --all       also remove all unused images, not just dangling ones
  --volumes   also remove unused volumes`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		m := cleanup.New(rt)
		res, err := m.Prune(cmd.Context(), cleanup.Options{
			AllImages: all,
			Volumes:   volumes,
			DryRun:    dryRun,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		for _, c := range res.Commands {
			fmt.Printf("[tengiz] docker %s\n", strings.Join(c, " "))
		}
		if res.DryRun {
			fmt.Println("[tengiz] dry-run: nothing was removed")
			return nil
		}
		if res.ReclaimedSpace != "" {
			fmt.Printf("[tengiz] reclaimed %s\n", res.ReclaimedSpace)
		} else {
			fmt.Println("[tengiz] nothing to clean up")
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Add `Prune` to `mockRTForDeploy` in `internal/cli/root_test.go`**

The `mockRTForDeploy` struct implements `runtime.Manager`. Adding `Prune` to the interface requires this method. Add after the `Run` method (line ~100):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{DryRun: opts.DryRun, Commands: [][]string{runtime.SystemPruneArgs(opts)}}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanupCommand|TestMockRTForDeployImplementsManager" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all CLI tests**

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command with label-based docker prune"
```

---

### Task 4: Update documentation and feature status

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: command shape from Task 3 (`tengiz cleanup [--dry-run] [--all] [--volumes]`)
- Produces: documented command + feature marked implemented

- [ ] **Step 1: Add `tengiz cleanup` to README Features list**

In `README.md`, in the `## Features` list (after the "**Deployment history**" bullet, line ~20), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, images, and networks while preserving Tengiz-managed apps via label filtering.
```

- [ ] **Step 2: Add `tengiz cleanup` to README CLI Reference**

In `README.md`, after the `### tengiz ps` section (after line ~150), add:

```markdown
### `tengiz cleanup`

Clean up unused Docker resources (containers, images, networks). Tengiz-managed containers are always preserved via label filtering.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show the docker command that would run without executing it |
| `--all` | Also remove all unused images, not just dangling ones |
| `--volumes` | Also remove unused volumes |

Example:
```
tengiz cleanup
tengiz cleanup --all --volumes
tengiz cleanup --dry-run
```
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 priority table (row for feature #6), change:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a status line to the `## Docker Housekeeping (Otomatik Temizlik)` feature section (after the `- **Description:**` line, ~line 379):

```markdown
- **Status:** ✅ Implemented (2026-08-01)
```

Add a row to the `### ✅ Implemented Features (Not Pending)` table (at the top of that table, before the existing rows):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-01) |
```

- [ ] **Step 4: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except the known slow proxy TCP-timeout tests and time-sensitive idle tests)

- [ ] **Step 5: Run static analysis and build**

Run: `go vet ./...`

Expected: No issues

Run: `go build -o tengiz .`

Expected: Build succeeds

- [ ] **Step 6: Manual smoke test (docker required)**

```bash
# Build and show the dry-run command
./tengiz cleanup --dry-run
# Expect: [tengiz] docker system prune -f --filter label!=tengiz-app
#         [tengiz] dry-run: nothing was removed

# Run a real (safe) prune
./tengiz cleanup
# Expect: [tengiz] docker system prune -f --filter label!=tengiz-app
#         [tengiz] reclaimed <size> OR [tengiz] nothing to clean up
```

- [ ] **Step 7: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping as implemented"
```

---

### Task 5: Self-review

- [ ] **Step 1: Verify spec coverage against `docs/FUTURES_FEATURES.md` feature #6**

- "Label-based `docker system prune`" ✅ — `SystemPruneArgs` always emits `--filter label!=tengiz-app` (Task 1)
- "`tengiz cleanup` command" ✅ — CLI command added and registered (Task 3)
- "kullanılmayan volume, network, container ve image'leri temizleme" (unused volumes/networks/containers/images) ✅ — `docker system prune` covers stopped containers, unused networks, dangling images; `--all` covers all unused images; `--volumes` covers unused volumes (Task 1 flags)
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" (label filtering preserves Tengiz containers) ✅ — `label!=tengiz-app` excludes all containers carrying the `tengiz-app` label, which `Create`, `CreateFromImage`, `CreateVersioned`, and `Run` all set (verified in `docker.go` lines 98, 125, 456, 516)

- [ ] **Step 2: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "fill in details", "add appropriate error handling", "Similar to Task". None present — every step contains complete code and exact commands.

- [ ] **Step 3: Type consistency check**

- `runtime.PruneOptions{All, Volumes, DryRun bool}` — defined Task 1, used Tasks 1, 2, 3, 4 identically
- `runtime.PruneResult{Output, ReclaimedSpace string, DryRun bool, Commands [][]string}` — defined Task 1, consumed Task 2
- `runtime.SystemPruneArgs(opts PruneOptions) []string` — defined Task 1, used in Task 2 test and Task 3 mock
- `runtime.Manager.Prune(ctx, opts) (PruneResult, error)` — interface (Task 1), docker impl + stub (Task 1), mock (Task 3)
- `cleanup.Pruner` / `cleanup.New` / `cleanup.Options{AllImages, Volumes, DryRun bool}` / `cleanup.Result` — defined Task 2, consumed Task 3
- `runtime.Manager` returned by `runtime.NewDocker()` satisfies `cleanup.Pruner` because its `Prune` signature matches exactly ✅
- All method names consistent across tasks; no renames

- [ ] **Step 4: Final full-suite verification**

Run: `go test ./... -v -count=1`

Expected: All PASS

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 5: Final commit**

```bash
git add docs/superpowers/plans/2026-08-01-docker-housekeeping.md
git commit -m "docs: add docker housekeeping implementation plan"
```
