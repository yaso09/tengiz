# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning unused Docker containers, images, volumes, and networks, while protecting all Tengiz-managed containers via label-based filtering.

**Architecture:** A new `Cleanup(ctx, opts)` method on `runtime.Manager` (alongside the existing `RemoveImage`/`KeepLastNImages` cleanup methods) is implemented in `dockerRuntime` by running `docker` subcommands. For each resource category we first run a *list* command (with the same filters `prune` uses) to count candidates, then — unless `--dry-run` — run the matching *prune* command. Tengiz containers are protected by the `label!=tengiz-app` filter on container prune. The CLI `tengiz cleanup` command is a thin Cobra wrapper over `runtime.Manager.Cleanup`, with `--dry-run` and `--all` flags.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` exec-based Docker impl (`os/exec`), existing `runtime.NewStub()` test mock. No new external dependencies.

## Global Constraints

- Tengiz-managed containers (labeled `tengiz-app=*`, set in `internal/runtime/docker.go:98`) must never be pruned — container prune uses `--filter label!=tengiz-app`
- Container names are prefixed `tengiz-`, images tagged `tengiz-apps/<app>:<tag>` — image prune is **dangling-only** by default so rollback images (`KeepLastNImages`) are never removed
- Volumes are only pruned with `--all` (they may hold app data) — mirrors `docker system prune` requiring `--volumes`
- `--dry-run` must never call a `prune` command — only list commands
- The `Cleanup` method is added to the `runtime.Manager` interface, so all existing mock implementations must be updated in the same commit
- No config file changes, no `.tengiz.yaml` schema changes, no new dependencies
- Default report counts come from the list command so `--dry-run` and real runs report the same numbers
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupReport` types; add `Cleanup` to `Manager` interface + `stubManager` |
| `internal/runtime/cleanup.go` | Implement `dockerRuntime.Cleanup` + arg-builder helpers (`cleanupContainerListArgs` etc.) + `countNonEmptyLines` + `cleanupCategory` |
| `internal/runtime/cleanup_test.go` | Tests for arg builders, `countNonEmptyLines`, stub `Cleanup` |
| `internal/cli/cleanup.go` | **New file:** `cleanupCmd`, `cleanupOptionsFromFlags`, `runCleanupCommand`, `formatCleanupReport` |
| `internal/cli/cleanup_test.go` | **New file:** tests for command registration, flags, report formatting, stub-driven execution |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()`; set `--dry-run`/`--all` flags |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` method to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` method to `mockRuntime` |
| `README.md` | Add `tengiz cleanup` to CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as ✅ Implemented |

---

### Task 1: Add `Cleanup` to the `runtime.Manager` interface + stub + all mocks

**Files:**
- Modify: `internal/runtime/runtime.go:18-49` (types + interface), `internal/runtime/runtime.go:113-119` (stub)
- Modify: `internal/cli/root_test.go:69-100` (`mockRTForDeploy`)
- Modify: `internal/proxy/proxy_test.go:15-35` (`mockRuntime`)
- Modify: `internal/idle/idle_test.go:14-34` (`mockRuntime`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: existing `runtime.Manager` interface pattern (types in same file as interface)
- Produces: `runtime.CleanupOptions{DryRun bool; All bool}`, `runtime.CleanupReport{Containers int; Images int; Volumes int; Networks int}`, interface method `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true, All: false})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.Containers != 0 || report.Images != 0 || report.Volumes != 0 || report.Networks != 0 {
		t.Errorf("expected zero report, got %+v", report)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: FAIL with `m.Cleanup undefined (type Manager has no field or method Cleanup)` and a compile error because `CleanupOptions`/`CleanupReport` are undefined.

- [ ] **Step 3: Add types and interface method in `internal/runtime/runtime.go`**

After the `RunOptions` struct (currently ends at line 29), add:

```go
type CleanupOptions struct {
	DryRun bool
	All    bool
}

type CleanupReport struct {
	Containers int
	Images     int
	Volumes    int
	Networks   int
}
```

Add `Cleanup` to the `Manager` interface (after the `KeepLastNImages` line, currently line 36):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)
```

Add the stub method after `stubManager.KeepLastNImages` (currently line 117):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
}
```

- [ ] **Step 4: Update the three mock implementations so the package still compiles**

In `internal/cli/root_test.go`, add to `mockRTForDeploy` (after the `KeepLastNImages` method, line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return runtime.CleanupReport{}, nil
}
```

In `internal/proxy/proxy_test.go`, add to `mockRuntime` (after the `KeepLastNImages` method, line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return runtime.CleanupReport{}, nil
}
```

In `internal/idle/idle_test.go`, add to `mockRuntime` (after the `KeepLastNImages` method, line 33):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return runtime.CleanupReport{}, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./... -count=1`
Expected: PASS (all packages; the new `TestStubCleanup` passes and the interface additions compile everywhere).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add Cleanup method to Manager interface"
```

---

### Task 2: Implement `dockerRuntime.Cleanup`

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupReport`, `Manager` from Task 1
- Produces: `dockerRuntime.Cleanup(ctx, opts) (CleanupReport, error)`, helpers `cleanupContainerListArgs() []string`, `cleanupContainerPruneArgs() []string`, `cleanupImageListArgs() []string`, `cleanupImagePruneArgs() []string`, `cleanupVolumeListArgs() []string`, `cleanupVolumePruneArgs() []string`, `cleanupNetworkListArgs() []string`, `cleanupNetworkPruneArgs() []string`, `countNonEmptyLines(out string) int`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go
func TestCleanupContainerArgs(t *testing.T) {
	wantList := []string{"ps", "-aq", "--filter", "status=exited", "--filter", "label!=tengiz-app"}
	wantPrune := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	assertStringSlicesEqual(t, cleanupContainerListArgs(), wantList)
	assertStringSlicesEqual(t, cleanupContainerPruneArgs(), wantPrune)
}

func TestCleanupImageArgs(t *testing.T) {
	assertStringSlicesEqual(t, cleanupImageListArgs(), []string{"images", "-aq", "--filter", "dangling=true"})
	assertStringSlicesEqual(t, cleanupImagePruneArgs(), []string{"image", "prune", "-f"})
}

func TestCleanupVolumeArgs(t *testing.T) {
	assertStringSlicesEqual(t, cleanupVolumeListArgs(), []string{"volume", "ls", "-q", "--filter", "dangling=true"})
	assertStringSlicesEqual(t, cleanupVolumePruneArgs(), []string{"volume", "prune", "-f"})
}

func TestCleanupNetworkArgs(t *testing.T) {
	assertStringSlicesEqual(t, cleanupNetworkListArgs(), []string{"network", "ls", "-q", "--filter", "dangling=true"})
	assertStringSlicesEqual(t, cleanupNetworkPruneArgs(), []string{"network", "prune", "-f"})
}

func TestCountNonEmptyLines(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect int
	}{
		{"empty", "", 0},
		{"only blank", "\n  \n\n", 0},
		{"one id", "abc123\n", 1},
		{"three ids with blank", "abc\n\ndef\nghi\n", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countNonEmptyLines(tt.input); got != tt.expect {
				t.Errorf("countNonEmptyLines(%q) = %d, want %d", tt.input, got, tt.expect)
			}
		})
	}
}

func assertStringSlicesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q (got %v)", i, got[i], want[i], got)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestCleanupContainerArgs|TestCleanupImageArgs|TestCleanupVolumeArgs|TestCleanupNetworkArgs|TestCountNonEmptyLines" -v -count=1`
Expected: FAIL with `undefined: cleanupContainerListArgs`, `undefined: countNonEmptyLines`, etc.

- [ ] **Step 3: Implement in `internal/runtime/cleanup.go`**

Append to `cleanup.go` (imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings` already present):

```go
func cleanupContainerListArgs() []string {
	return []string{"ps", "-aq", "--filter", "status=exited", "--filter", "label!=tengiz-app"}
}

func cleanupContainerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func cleanupImageListArgs() []string {
	return []string{"images", "-aq", "--filter", "dangling=true"}
}

func cleanupImagePruneArgs() []string {
	return []string{"image", "prune", "-f"}
}

func cleanupVolumeListArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
}

func cleanupVolumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func cleanupNetworkListArgs() []string {
	return []string{"network", "ls", "-q", "--filter", "dangling=true"}
}

func cleanupNetworkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func countNonEmptyLines(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func (r *dockerRuntime) cleanupCategory(ctx context.Context, listArgs, pruneArgs []string, dryRun bool) (int, error) {
	list := exec.CommandContext(ctx, "docker", listArgs...)
	listOut, err := list.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker %s: %w\n%s", strings.Join(listArgs, " "), err, string(listOut))
	}
	count := countNonEmptyLines(string(listOut))
	if dryRun || count == 0 {
		return count, nil
	}
	prune := exec.CommandContext(ctx, "docker", pruneArgs...)
	if _, err := prune.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker %s: %w", strings.Join(pruneArgs, " "), err)
	}
	return count, nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	var report CleanupReport
	var err error

	report.Containers, err = r.cleanupCategory(ctx, cleanupContainerListArgs(), cleanupContainerPruneArgs(), opts.DryRun)
	if err != nil {
		return report, err
	}
	report.Images, err = r.cleanupCategory(ctx, cleanupImageListArgs(), cleanupImagePruneArgs(), opts.DryRun)
	if err != nil {
		return report, err
	}
	report.Networks, err = r.cleanupCategory(ctx, cleanupNetworkListArgs(), cleanupNetworkPruneArgs(), opts.DryRun)
	if err != nil {
		return report, err
	}
	if opts.All {
		report.Volumes, err = r.cleanupCategory(ctx, cleanupVolumeListArgs(), cleanupVolumePruneArgs(), opts.DryRun)
		if err != nil {
			return report, err
		}
	}
	return report, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker cleanup pruning"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:38-88` (`init()` registration)
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager`, `runtime.CleanupOptions`, `runtime.CleanupReport` from Tasks 1-2
- Produces: `cleanupCmd *cobra.Command`, `cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error)`, `runCleanupCommand(rt runtime.Manager, opts runtime.CleanupOptions) (string, error)`, `formatCleanupReport(report runtime.CleanupReport, dryRun bool) string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd := findSubcommand(rootCmd, "cleanup")
	if cmd == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	cmd := findSubcommand(rootCmd, "cleanup")
	if cmd == nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd.Flags().Lookup("dry-run") == nil {
		t.Error("expected --dry-run flag")
	}
	if cmd.Flags().Lookup("all") == nil {
		t.Error("expected --all flag")
	}
}

func TestCleanupOptionsFromFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Set("dry-run", "true")
	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.DryRun {
		t.Error("expected DryRun true")
	}
	if opts.All {
		t.Error("expected All false")
	}
}

func TestFormatCleanupReport(t *testing.T) {
	report := runtime.CleanupReport{Containers: 3, Images: 5, Volumes: 2, Networks: 1}

	real := formatCleanupReport(report, false)
	if !strings.Contains(real, "removed") {
		t.Errorf("real report = %q, want 'removed'", real)
	}
	if !strings.Contains(real, "3 containers") || !strings.Contains(real, "5 images") {
		t.Errorf("real report = %q, want container/image counts", real)
	}

	dry := formatCleanupReport(report, true)
	if !strings.Contains(dry, "would remove") {
		t.Errorf("dry report = %q, want 'would remove'", dry)
	}
}

func TestRunCleanupCommandWithStub(t *testing.T) {
	out, err := runCleanupCommand(runtime.NewStub(), runtime.CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("runCleanupCommand() error = %v", err)
	}
	if !strings.Contains(out, "0 containers") {
		t.Errorf("output = %q, want zero counts", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestCleanupFlagsRegistered|TestCleanupOptionsFromFlags|TestFormatCleanupReport|TestRunCleanupCommandWithStub" -v -count=1`
Expected: FAIL with `undefined: cleanupCmd`, `undefined: cleanupOptionsFromFlags`, `undefined: formatCleanupReport`, `undefined: runCleanupCommand`.

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources",
	Long:  "Removes stopped foreign containers, dangling images, and unused networks. " +
		"Tengiz-managed containers (labeled tengiz-app) are always protected. " +
		"Use --all to also prune unused volumes. Use --dry-run to preview.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		out, err := runCleanupCommand(rt, opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		fmt.Println(out)
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return runtime.CleanupOptions{}, err
	}
	all, err := cmd.Flags().GetBool("all")
	if err != nil {
		return runtime.CleanupOptions{}, err
	}
	return runtime.CleanupOptions{DryRun: dryRun, All: all}, nil
}

func runCleanupCommand(rt runtime.Manager, opts runtime.CleanupOptions) (string, error) {
	report, err := rt.Cleanup(context.Background(), opts)
	if err != nil {
		return "", err
	}
	return formatCleanupReport(report, opts.DryRun), nil
}

func formatCleanupReport(report runtime.CleanupReport, dryRun bool) string {
	verb := "removed"
	if dryRun {
		verb = "would remove"
	}
	return fmt.Sprintf("[tengiz] cleanup %s: %d containers, %d images, %d volumes, %d networks",
		verb, report.Containers, report.Images, report.Volumes, report.Networks)
}
```

- [ ] **Step 4: Register the command and flags in `internal/cli/root.go`**

In `init()` (after `rootCmd.AddCommand(runCmd)` at line 67), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

Add the flag definitions at the end of `init()` (after the `webhookCmd.Flags()` block at line 88):

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("all", false, "also prune unused volumes")
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`
Expected: PASS.

- [ ] **Step 6: Build and run the full suite**

Run: `go build -o tengiz . && go vet ./... && go test ./... -count=1`
Expected: build succeeds, `go vet` reports nothing, all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Update documentation

**Files:**
- Modify: `README.md` (CLI Reference)
- Modify: `AGENTS.md` (command list)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 implemented)

**Interfaces:**
- Consumes: the `tengiz cleanup` command and its flags from Task 3
- Produces: updated user-facing docs

- [ ] **Step 1: Add `tengiz cleanup` to README CLI Reference**

In `README.md`, add a new section after the `### tengiz ps` section (currently around line 150):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--all` | Also prune unused volumes |

Removes stopped containers **not** managed by Tengiz (Tengiz containers are protected via the `tengiz-app` label), dangling images, and unused networks. Volumes are only pruned with `--all`. Old deployment images kept for rollback are never pruned.
```

- [ ] **Step 2: Add `tengiz cleanup` to AGENTS.md command list**

In `AGENTS.md`, after the `tengiz ps` line (currently line 43), add:

```
tengiz cleanup          → prune unused Docker containers/images/networks (--all for volumes, --dry-run to preview)
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table (currently line 19), change the status marker from ⬜ to ✅:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the "✅ Implemented Features (Not Pending)" table (starting around line 237), add a row:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-15) |
```

- [ ] **Step 4: Verify the full build and test suite still passes**

Run: `go build -o tengiz . && go vet ./... && go test ./... -count=1`
Expected: build succeeds, `go vet` reports nothing, all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Manual Verification

With a running Docker daemon (outside automated tests):

```bash
go build -o tengiz .
./tengiz cleanup --dry-run        # preview counts, nothing removed
docker run -d --name tmp-alpine alpine sleep 300   # create a foreign stopped container
./tengiz cleanup                  # removes the foreign container, keeps tengiz-* containers
./tengiz cleanup --all            # also prunes unused volumes
```

## Self-Review

**1. Spec coverage:** Feature #6 (Docker Housekeeping) is fully covered: `tengiz cleanup` command (Task 3), label-based pruning protecting Tengiz containers (Task 2, `label!=tengiz-app`), volumes opt-in via `--all` (Task 2, `opts.All` guard), and documentation (Task 4). Granular per-category flags (#56) and build-cache/git GC (#103) are deliberately out of scope and left as separate plans.

**2. Placeholder scan:** No TBD/TODO/“implement later” markers. Every code step shows full source. Every test step shows real assertions and expected output.

**3. Type consistency:** `CleanupOptions{DryRun, All}` and `CleanupReport{Containers, Images, Volumes, Networks}` are defined once in Task 1 and referenced identically in Tasks 2-3 (`opts.DryRun`, `report.Containers`, etc.). The `Cleanup` method signature matches across interface, stub, three mocks, docker implementation, and CLI caller.
