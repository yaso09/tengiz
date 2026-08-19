# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, networks, volumes, build cache) using label-based filters so Tengiz-managed resources are always preserved, freeing disk space on single-server deployments.

**Architecture:** Extend `runtime.Manager` with a `Prune(ctx, opts PruneOptions) (PruneReport, error)` method. `dockerRuntime` shells out to `docker <category> prune -f` with `--filter label!=tengiz-app` on containers/networks/volumes to protect Tengiz-managed resources (label `tengiz-app`), and counts removed items by parsing prune output with a pure helper. A new `tengiz cleanup` Cobra command reads category flags, optionally prompts for confirmation (skipped in non-TTY/CI or with `--yes`), calls `Prune`, and prints a summary report. `stubManager` and the three test mock runtime implementations gain a no-op `Prune` so the interface stays satisfied.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface, `docker` CLI via `os/exec` (no new dependencies).

## Global Constraints

- Tengiz-managed containers (labeled `tengiz-app=*`) must NEVER be pruned — use `--filter label!=tengiz-app`
- Image pruning must use `docker image prune -f` WITHOUT `-a` so tagged `tengiz-apps/*` images are never removed
- Default `tengiz cleanup` behavior mirrors `docker system prune`: containers + images + networks + build cache, NOT volumes
- Volumes are opt-in via `--volumes` (volumes may contain user data)
- `--all` enables all five categories including volumes
- Confirmation prompt only when stdin is a TTY AND `--yes` not passed; CI/non-TTY runs proceed without prompting
- No new external dependencies
- `--env` does not apply to cleanup — it is a system-wide operation
- All existing tests must continue to pass; mock implementations of `runtime.Manager` must be updated when the interface grows
- New feature requires branch `feat/cleanup` (per AGENTS.md) and README/doc updates (per AGENTS.md)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `countPruned()` pure parser; `dockerRuntime.Prune()` + `pruneCategory()` exec helpers |
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneReport` types; add `Prune` to `Manager` interface; add stub `Prune` |
| `internal/runtime/cleanup_test.go` | Table-driven `countPruned` tests; `TestStubPrune` |
| `internal/cli/cleanup.go` | New `cleanupCmd` + `cleanupOptions()` flag mapper + `stdinIsTerminal()` + `init()` registration |
| `internal/cli/cleanup_test.go` | Registration test, `cleanupOptions` table tests, `--yes` invocation test |
| `internal/cli/root_test.go` | Add `Prune` method to `mockRTForDeploy` (interface satisfaction) |
| `internal/idle/idle_test.go` | Add `Prune` method to `mockRuntime` (interface satisfaction) |
| `internal/proxy/proxy_test.go` | Add `Prune` method to `mockRuntime` (interface satisfaction) |
| `README.md` | Add `### tengiz cleanup` section under CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` line to CLI list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 `✅ Implemented` + add to Implemented table |

---

### Task 1: `countPruned` parser (pure function)

**Files:**
- Modify: `internal/runtime/cleanup.go` (add `countPruned` at end)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `countPruned(out string) int` — counts deleted items from `docker <category> prune -f` output

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestCountPruned(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want int
	}{
		{
			name: "containers",
			out:  "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 1.5MB\n",
			want: 2,
		},
		{
			name: "images",
			out:  "Deleted Images:\nuntagged: repo:tag\ndeleted: sha256:abc\n\nTotal reclaimed space: 0B\n",
			want: 1,
		},
		{
			name: "networks",
			out:  "Deleted Networks:\nbridge\n\nTotal reclaimed space: 0B\n",
			want: 1,
		},
		{
			name: "volumes",
			out:  "Deleted Volumes:\nmyvolume\n",
			want: 1,
		},
		{
			name: "build cache",
			out:  "Deleted Build Cache Objects:\nobj1\nobj2\n\nTotal reclaimed space: 1MB\n",
			want: 2,
		},
		{
			name: "empty output",
			out:  "",
			want: 0,
		},
		{
			name: "only total line",
			out:  "Total reclaimed space: 0B\n",
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countPruned(tt.out); got != tt.want {
				t.Errorf("countPruned(%q) = %d, want %d", tt.out, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestCountPruned -v -count=1`
Expected: FAIL with `undefined: countPruned`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
// countPruned counts deleted items in `docker <category> prune -f` output.
// It skips blank lines, the "Total reclaimed space" summary, "untagged:" lines
// (tag removal only, the following "deleted:" line is the actual image), and
// section headers like "Deleted Containers:".
func countPruned(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed") {
			continue
		}
		if strings.HasPrefix(line, "untagged:") {
			continue
		}
		if strings.HasPrefix(line, "Deleted ") && strings.HasSuffix(line, ":") {
			continue
		}
		count++
	}
	return count
}
```

`strings` is already imported in `cleanup.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestCountPruned -v -count=1`
Expected: PASS (all subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add prune output counter for docker housekeeping"
```

---

### Task 2: `Prune` on the runtime `Manager` interface

**Files:**
- Modify: `internal/runtime/runtime.go:18-29` (add types after `RunOptions`), `:31-49` (interface), `:51-123` (stub)
- Modify: `internal/runtime/cleanup.go` (add `dockerRuntime.Prune` + `pruneCategory`)
- Test: `internal/runtime/cleanup_test.go` (stub test)
- Modify: `internal/cli/root_test.go:69-107` (mockRTForDeploy)
- Modify: `internal/idle/idle_test.go:14-34` (mockRuntime)
- Modify: `internal/proxy/proxy_test.go:15-35` (mockRuntime)

**Interfaces:**
- Consumes: `countPruned(out string) int` from Task 1
- Produces: `runtime.PruneOptions{Containers, Images, Networks, Volumes, BuildCache bool}`, `runtime.PruneReport{Containers, Images, Networks, Volumes, BuildCache int}`, `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`. The CLI (Task 3) relies on these exact names.

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.Containers != 0 || report.Images != 0 || report.Networks != 0 || report.Volumes != 0 || report.BuildCache != 0 {
		t.Errorf("stub Prune() report = %+v, want all zero values", report)
	}
}
```

`context` is already imported in `cleanup_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: FAIL — compile error: `Prune undefined` (type `stubManager` has no field or method `Prune`)

- [ ] **Step 3: Add types and interface method in `internal/runtime/runtime.go`**

After the `RunOptions` struct (line 29), add:

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
}

type PruneReport struct {
	Containers int
	Images     int
	Networks   int
	Volumes    int
	BuildCache int
}
```

Add to the `Manager` interface, right after `KeepLastNImages` (line 36):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

Add the stub method after `func (m *stubManager) KeepLastNImages` (line 119):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}
```

- [ ] **Step 4: Add `Prune` implementation to `internal/runtime/cleanup.go`**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport
	if opts.Containers {
		n, err := r.pruneCategory(ctx, "container", "--filter", "label!="+labelKey)
		if err != nil {
			return report, err
		}
		report.Containers = n
	}
	if opts.Images {
		n, err := r.pruneCategory(ctx, "image")
		if err != nil {
			return report, err
		}
		report.Images = n
	}
	if opts.Networks {
		n, err := r.pruneCategory(ctx, "network", "--filter", "label!="+labelKey)
		if err != nil {
			return report, err
		}
		report.Networks = n
	}
	if opts.Volumes {
		n, err := r.pruneCategory(ctx, "volume", "--filter", "label!="+labelKey)
		if err != nil {
			return report, err
		}
		report.Volumes = n
	}
	if opts.BuildCache {
		n, err := r.pruneCategory(ctx, "builder")
		if err != nil {
			return report, err
		}
		report.BuildCache = n
	}
	return report, nil
}

func (r *dockerRuntime) pruneCategory(ctx context.Context, category string, extra ...string) (int, error) {
	args := []string{category, "prune", "-f"}
	args = append(args, extra...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker %s prune: %w\n%s", category, err, string(out))
	}
	return countPruned(string(out)), nil
}
```

`labelKey` (`"tengiz-app"`) is already defined at the top of `internal/runtime/docker.go:76`. `exec` and `fmt` are already imported in `cleanup.go`.

- [ ] **Step 5: Update the three test mocks to satisfy the interface**

`internal/cli/root_test.go` — add after line 99 (`KeepLastNImages` method):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

`internal/idle/idle_test.go` — add after line 34 (`Run` method):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) { return runtime.PruneReport{}, nil }
```

`internal/proxy/proxy_test.go` — add after line 35 (`Run` method):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) { return runtime.PruneReport{}, nil }
```

`context` and `runtime` are already imported in all three test files.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/ ./internal/idle/ ./internal/proxy/ ./internal/cli/ -v -count=1`
Expected: PASS — `TestStubPrune`, `TestCountPruned`, all existing tests in those packages

- [ ] **Step 7: Verify the full build**

Run: `go build ./... && go vet ./...`
Expected: no errors

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/ internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Prune method to runtime Manager for docker housekeeping"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.NewDocker()`, `runtime.Manager.Prune` from Tasks 1-2
- Produces: `tengiz cleanup` command with flags `--all`, `--containers`, `--images`, `--networks`, `--volumes`, `--build-cache`, `-y/--yes`; helper `cleanupOptions(cmd *cobra.Command) runtime.PruneOptions`; helper `stdinIsTerminal() bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd.Name() != "cleanup" {
		t.Fatalf("expected cleanup command, got %q", cmd.Name())
	}
}

func cleanupFlagCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Bool("all", false, "")
	c.Flags().Bool("containers", false, "")
	c.Flags().Bool("images", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("build-cache", false, "")
	return c
}

func TestCleanupOptionsDefault(t *testing.T) {
	opts := cleanupOptions(cleanupFlagCmd())
	want := runtime.PruneOptions{Containers: true, Images: true, Networks: true, BuildCache: true}
	if opts != want {
		t.Errorf("cleanupOptions() = %+v, want %+v", opts, want)
	}
}

func TestCleanupOptionsAll(t *testing.T) {
	c := cleanupFlagCmd()
	c.Flags().Set("all", "true")
	opts := cleanupOptions(c)
	want := runtime.PruneOptions{Containers: true, Images: true, Networks: true, Volumes: true, BuildCache: true}
	if opts != want {
		t.Errorf("cleanupOptions() = %+v, want %+v", opts, want)
	}
}

func TestCleanupOptionsSpecific(t *testing.T) {
	c := cleanupFlagCmd()
	c.Flags().Set("volumes", "true")
	opts := cleanupOptions(c)
	want := runtime.PruneOptions{Volumes: true}
	if opts != want {
		t.Errorf("cleanupOptions() = %+v, want %+v", opts, want)
	}
}

func TestCleanupCmdInvokesWithYes(t *testing.T) {
	called := false
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("cleanup RunE was not called")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`
Expected: FAIL — compile error: `undefined: cleanupCmd`, `undefined: cleanupOptions`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, networks, volumes, build cache)",
	Long: `Remove unused Docker resources while protecting Tengiz-managed resources.
Tengiz-managed containers (labeled tengiz-app) are always preserved.

By default prunes stopped non-Tengiz containers, dangling images, unused networks and build cache.
Use --volumes to also prune unused volumes. Use --all to prune everything including volumes.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes && stdinIsTerminal() {
			fmt.Print("[cleanup] This will remove unused Docker resources. Continue? [y/N]: ")
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "y") {
				fmt.Println("[cleanup] cancelled")
				return nil
			}
		}

		opts := cleanupOptions(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		report, err := rt.Prune(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		fmt.Printf("[cleanup] removed: %d containers, %d images, %d networks, %d volumes, %d build cache objects\n",
			report.Containers, report.Images, report.Networks, report.Volumes, report.BuildCache)
		return nil
	},
}

func cleanupOptions(cmd *cobra.Command) runtime.PruneOptions {
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	buildCache, _ := cmd.Flags().GetBool("build-cache")

	if all {
		return runtime.PruneOptions{Containers: true, Images: true, Networks: true, Volumes: true, BuildCache: true}
	}
	if containers || images || networks || volumes || buildCache {
		return runtime.PruneOptions{
			Containers: containers,
			Images:     images,
			Networks:   networks,
			Volumes:    volumes,
			BuildCache: buildCache,
		}
	}
	return runtime.PruneOptions{Containers: true, Images: true, Networks: true, BuildCache: true}
}

func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func init() {
	cleanupCmd.Flags().Bool("all", false, "prune all categories including volumes")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (may contain data)")
	cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`
Expected: PASS — all four tests

- [ ] **Step 5: Verify the full build and full test suite**

Run: `go build ./... && go vet ./... && go test ./... -v -count=1`
Expected: PASS — no errors; `TestCleanupCmdInvokesWithYes` skips the TTY prompt because test stdin is not a char device

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: nothing (documentation of the Task 3 command)

- [ ] **Step 1: Add `tengiz cleanup` section to `README.md`**

Insert a new section after the `### tengiz ps` section (currently ends around line 150), before `### tengiz logs`:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to free disk space while protecting Tengiz-managed resources.

Tengiz-managed containers (labeled `tengiz-app`) are always preserved. By default prunes stopped
non-Tengiz containers, dangling images, unused networks and build cache — mirroring
`docker system prune`.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling images |
| `--networks` | Prune unused networks |
| `--volumes` | Prune unused volumes (may contain data — opt-in) |
| `--build-cache` | Prune build cache |
| `--all` | Prune all categories including volumes |
| `-y`, `--yes` | Skip the confirmation prompt |

With no category flags, all default categories are pruned. When any category flag is given, only
those categories are pruned. Prompts for confirmation unless `--yes` is passed or stdin is not a TTY.
```

- [ ] **Step 2: Add `tengiz cleanup` to `AGENTS.md` CLI list**

After the `tengiz rollback` line (currently line 60) in the CLI block, add:

```
tengiz cleanup           → prune unused Docker resources (containers/images/networks/volumes/build cache)
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 Priority Ranking table (line 19), change the status cell from `⬜` to `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "✅ Implemented Features (Not Pending)" table (after the last entry, around line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-19) |
```

- [ ] **Step 4: Verify the full test suite still passes**

Run: `go build -o tengiz . && go vet ./... && go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage.** FUTURES_FEATURES #6 ("Docker Housekeeping": label-based `docker system prune`, `tengiz cleanup`) is implemented by Task 2 (runtime prune with `label!=tengiz-app` filters protecting Tengiz resources) and Task 3 (the `tengiz cleanup` command). All four prune categories from the spec's rationale are covered plus build cache, with volumes as opt-in. Docs updated in Task 4.

**2. Placeholder scan.** Every step contains concrete code, exact commands, and expected output. No TBD/TODO/"add appropriate error handling" placeholders.

**3. Type consistency.** `PruneOptions`/`PruneReport` field names are identical across Tasks 1-3 (`Containers`, `Images`, `Networks`, `Volumes`, `BuildCache`). The interface signature `Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)` is used consistently in the interface, stub, dockerRuntime, all three test mocks, and the CLI caller. `countPruned` is consumed only by `pruneCategory` in Task 2.
