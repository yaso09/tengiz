# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, networks and build cache while protecting all Tengiz-managed resources, so disk space is reclaimed safely on single-server deployments.

**Architecture:** A new `Prune` method on the `runtime.Manager` interface drives the Docker CLI via `os/exec` (matching the existing exec-based runtime). Resource protection is filter-based: stopped containers carrying the `tengiz-app` label are never pruned, and images with the `tengiz-apps/*` reference (production + rollback versions) are excluded from `docker image prune -a`. The CLI command translates flags into `runtime.PruneOptions`, defaults to a safe set (everything except volumes), and offers `--dry-run` that prints the exact `docker` commands without executing them.

**Tech Stack:** Go 1.26, Cobra, `os/exec` docker CLI, existing `runtime.Manager` interface and `stubManager`. No new dependencies.

## Global Constraints

- No new external dependencies; use only the standard library and existing packages
- New interface method: `Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)` on `runtime.Manager`
- `PruneOptions` fields (all `bool`): `Containers`, `Images`, `Volumes`, `Networks`, `BuildCache`
- `PruneResult` field: `Reclaimed string` (aggregated "Total reclaimed space" values)
- Containers are protected by the `tengiz-app` label → prune filter `label!=tengiz-app`
- Images are protected by the `tengiz-apps/*` reference → prune filter `reference!=tengiz-apps/*` (preserves rollback/versioned images)
- Cleanup is host-level and env-agnostic; the `--env` flag is NOT used
- Default CLI behavior (no category flags): prune containers, images, networks, build cache; volumes require explicit `--volumes` or `--all`
- Every mock of `runtime.Manager` must gain the new `Prune` method or the package will not compile
- All existing tests must continue to pass
- Verification commands: `go build ./...`, `go vet ./...`, `go test ./... -v -count=1`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | `PruneOptions`, `PruneResult` types; add `Prune` to `Manager` interface; stub implementation |
| `internal/runtime/prune.go` | (new) Pure `buildPruneCommands`/`PrunePlan`/`parseReclaimed` helpers + `dockerRuntime.Prune` exec implementation |
| `internal/runtime/prune_test.go` | (new) Unit tests for stub, pure helpers, and prune plan |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` (required for compile) |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` (required for compile) |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` (required for compile) |
| `internal/cli/root.go` | `cleanupCmd`, `addCleanupFlags`, `cleanupOptions` helper, registration in `init()` |
| `internal/cli/cleanup_test.go` | (new) CLI command/flag/options/dry-run tests |
| `README.md` | CLI Reference entry for `tengiz cleanup` |
| `AGENTS.md` | Add `tengiz cleanup` line to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

Changes touch 6 existing files and create 2 new Go test files plus the runtime prune file.

---

### Task 1: Add `Prune` to the `runtime.Manager` interface

**Files:**
- Modify: `internal/runtime/runtime.go:29` (add types after `RunOptions`) and `:36` (add to interface) and `:119` (stub)
- Modify: `internal/proxy/proxy_test.go:34` (after `KeepLastNImages`)
- Modify: `internal/idle/idle_test.go:33` (after `KeepLastNImages`)
- Modify: `internal/cli/root_test.go:99` (after `KeepLastNImages`)
- Test: `internal/runtime/prune_test.go` (new)

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{Containers, Images, Volumes, Networks, BuildCache bool}`, `runtime.PruneResult{Reclaimed string}`, interface method `Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Reclaimed != "" {
		t.Errorf("Reclaimed = %q, want empty", res.Reclaimed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`

Expected: FAIL with a compile error such as `m.Prune undefined (type Manager has no field or method Prune)`.

- [ ] **Step 3: Add types and interface method in `internal/runtime/runtime.go`**

Add the types right after the existing `RunOptions` struct (after line 29):

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type PruneResult struct {
	Reclaimed string
}
```

Add the method to the `Manager` interface, directly after `KeepLastNImages` (line 36):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

Add the stub implementation right after `stubManager.KeepLastNImages` (line 119):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}
```

- [ ] **Step 4: Add `Prune` to every mock so the packages compile**

In `internal/proxy/proxy_test.go`, after line 34 (`KeepLastNImages`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

In `internal/idle/idle_test.go`, after line 33 (`KeepLastNImages`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

In `internal/cli/root_test.go`, after line 99 (`KeepLastNImages`):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/ ./internal/proxy/ ./internal/idle/ ./internal/cli/ -v -count=1`

Expected: PASS, including the new `TestStubPrune` and the existing `TestMockRTForDeployImplementsManager`.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Prune to runtime.Manager interface"
```

---

### Task 2: Runtime prune helpers and `dockerRuntime.Prune`

**Files:**
- Create: `internal/runtime/prune.go`
- Modify: `internal/runtime/prune_test.go` (add tests)

**Interfaces:**
- Consumes: `PruneOptions`, `PruneResult`, `pruneCommand` (private struct defined below), `*dockerRuntime` from `runtime.go`
- Produces: `buildPruneCommands(opts PruneOptions) []pruneCommand`, `PrunePlan(opts PruneOptions) []string`, `parseReclaimed(out string) string`, and `(*dockerRuntime).Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/prune_test.go`:

```go
func TestBuildPruneCommandsAll(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	cmds := buildPruneCommands(opts)
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(cmds))
	}
}

func TestBuildPruneCommandsSelective(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true}
	cmds := buildPruneCommands(opts)
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
	if cmds[0].label != "containers" {
		t.Errorf("first command label = %q, want %q", cmds[0].label, "containers")
	}
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	for i, a := range cmds[0].args {
		if a != want[i] {
			t.Errorf("container args[%d] = %q, want %q", i, a, want[i])
		}
	}
	wantImg := []string{"image", "prune", "-af", "--filter", "reference!=tengiz-apps/*"}
	for i, a := range cmds[1].args {
		if a != wantImg[i] {
			t.Errorf("image args[%d] = %q, want %q", i, a, wantImg[i])
		}
	}
}

func TestBuildPruneCommandsNone(t *testing.T) {
	cmds := buildPruneCommands(PruneOptions{})
	if len(cmds) != 0 {
		t.Errorf("expected 0 commands, got %d", len(cmds))
	}
}

func TestPrunePlan(t *testing.T) {
	plan := PrunePlan(PruneOptions{Containers: true, BuildCache: true})
	if len(plan) != 2 {
		t.Fatalf("expected 2 plan lines, got %d", len(plan))
	}
	if plan[0] != "docker container prune -f --filter label!=tengiz-app" {
		t.Errorf("plan[0] = %q", plan[0])
	}
	if plan[1] != "docker builder prune -f" {
		t.Errorf("plan[1] = %q", plan[1])
	}
}

func TestParseReclaimed(t *testing.T) {
	out := "Deleted Containers:\nabc123\n\nTotal reclaimed space: 1.234MB\n"
	if got := parseReclaimed(out); got != "1.234MB" {
		t.Errorf("parseReclaimed = %q, want %q", got, "1.234MB")
	}
}

func TestParseReclaimedEmpty(t *testing.T) {
	if got := parseReclaimed("nothing here\n"); got != "" {
		t.Errorf("parseReclaimed = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestBuildPruneCommands|TestPrunePlan|TestParseReclaimed" -v -count=1`

Expected: FAIL with compile errors (`undefined: buildPruneCommands`, `undefined: PrunePlan`, `undefined: parseReclaimed`).

- [ ] **Step 3: Create `internal/runtime/prune.go`**

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type pruneCommand struct {
	label string
	args  []string
}

func buildPruneCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand
	if opts.Containers {
		cmds = append(cmds, pruneCommand{
			label: "containers",
			args:  []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		})
	}
	if opts.Images {
		cmds = append(cmds, pruneCommand{
			label: "images",
			args:  []string{"image", "prune", "-af", "--filter", "reference!=tengiz-apps/*"},
		})
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{
			label: "volumes",
			args:  []string{"volume", "prune", "-f"},
		})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{
			label: "networks",
			args:  []string{"network", "prune", "-f"},
		})
	}
	if opts.BuildCache {
		cmds = append(cmds, pruneCommand{
			label: "build cache",
			args:  []string{"builder", "prune", "-f"},
		})
	}
	return cmds
}

func PrunePlan(opts PruneOptions) []string {
	var plan []string
	for _, c := range buildPruneCommands(opts) {
		plan = append(plan, "docker "+strings.Join(c.args, " "))
	}
	return plan
}

func parseReclaimed(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var reclaimed []string
	for _, c := range buildPruneCommands(opts) {
		cmd := exec.CommandContext(ctx, "docker", c.args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return PruneResult{}, fmt.Errorf("docker %s prune: %w\n%s", c.label, err, string(out))
		}
		if sp := parseReclaimed(string(out)); sp != "" {
			reclaimed = append(reclaimed, sp)
		}
	}
	return PruneResult{Reclaimed: strings.Join(reclaimed, ", ")}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -v -count=1`

Expected: PASS for `TestStubPrune`, `TestBuildPruneCommandsAll`, `TestBuildPruneCommandsSelective`, `TestBuildPruneCommandsNone`, `TestPrunePlan`, `TestParseReclaimed`, `TestParseReclaimedEmpty`, and all pre-existing runtime tests.

- [ ] **Step 5: Verify build and vet**

Run: `go build ./... && go vet ./...`

Expected: no output (success), exit code 0.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): implement label-based docker prune with PrunePlan dry-run"
```

---

### Task 3: CLI `tengiz cleanup` command

**Files:**
- Modify: `internal/cli/root.go:75` (register in `init()`) and insert the command block after line 939 (between `volumeListCmd` and `rollbackCmd`)
- Test: `internal/cli/cleanup_test.go` (new)

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.PrunePlan`, `runtime.NewDocker`, `runtime.Manager.Prune`
- Produces: `addCleanupFlags(cmd *cobra.Command)`, `cleanupOptions(cmd *cobra.Command) runtime.PruneOptions`, `cleanupCmd` (registered on `rootCmd`)

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func newCleanupTestCmd() *cobra.Command {
	c := &cobra.Command{}
	addCleanupFlags(c)
	return c
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupOptionsDefaultExcludesVolumes(t *testing.T) {
	c := newCleanupTestCmd()
	opts := cleanupOptions(c)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Errorf("default should enable containers/images/networks/build-cache, got %+v", opts)
	}
	if opts.Volumes {
		t.Error("default should NOT enable volumes")
	}
}

func TestCleanupOptionsAllIncludesVolumes(t *testing.T) {
	c := newCleanupTestCmd()
	c.Flags().Set("all", "true")
	opts := cleanupOptions(c)
	if !opts.Volumes {
		t.Error("--all should enable volumes")
	}
}

func TestCleanupOptionsVolumesOnly(t *testing.T) {
	c := newCleanupTestCmd()
	c.Flags().Set("volumes", "true")
	opts := cleanupOptions(c)
	if !opts.Volumes {
		t.Error("--volumes should enable volumes")
	}
	if opts.Containers || opts.Images || opts.Networks || opts.BuildCache {
		t.Errorf("only --volumes set, expected other categories off, got %+v", opts)
	}
}

func TestCleanupDryRunPrintsDockerCommands(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	out := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(out, "docker container prune -f --filter label!=tengiz-app") {
		t.Errorf("dry-run output missing container prune command, got:\n%s", out)
	}
	if strings.Contains(out, "volume prune") {
		t.Errorf("dry-run should not include volume prune by default, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`

Expected: FAIL with compile errors (`undefined: addCleanupFlags`, `undefined: cleanupOptions`, `undefined: cleanupCmd`).

- [ ] **Step 3: Register the command and flags in `init()`**

In `internal/cli/root.go`, inside `init()` after the notification registration (line 75), add:

```go
	addCleanupFlags(cleanupCmd)
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 4: Add the command, flag helper, and options helper**

Insert this block in `internal/cli/root.go` after `volumeListCmd` (after line 939), directly before `var rollbackCmd`:

```go
func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cmd.Flags().Bool("images", false, "prune unused images not built by Tengiz")
	cmd.Flags().Bool("volumes", false, "prune unused volumes")
	cmd.Flags().Bool("networks", false, "prune unused networks")
	cmd.Flags().Bool("build-cache", false, "prune build cache")
	cmd.Flags().Bool("all", false, "prune all categories including volumes")
	cmd.Flags().Bool("dry-run", false, "print the docker commands without running them")
}

func cleanupOptions(cmd *cobra.Command) runtime.PruneOptions {
	getBool := func(name string) bool {
		v, _ := cmd.Flags().GetBool(name)
		return v
	}

	if getBool("all") {
		return runtime.PruneOptions{
			Containers: true,
			Images:     true,
			Volumes:    true,
			Networks:   true,
			BuildCache: true,
		}
	}

	opts := runtime.PruneOptions{
		Containers: getBool("containers"),
		Images:     getBool("images"),
		Volumes:    getBool("volumes"),
		Networks:   getBool("networks"),
		BuildCache: getBool("build-cache"),
	}

	if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.BuildCache {
		opts.Containers = true
		opts.Images = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to free disk space",
	Long:  "Prunes unused containers, images, volumes, networks, and build cache while preserving resources managed by Tengiz (containers labeled tengiz-app, images tagged tengiz-apps/*).",
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptions(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if dryRun {
			for _, line := range runtime.PrunePlan(opts) {
				fmt.Println(line)
			}
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		result, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return err
		}
		if result.Reclaimed != "" {
			fmt.Printf("[tengiz] cleanup complete, reclaimed: %s\n", result.Reclaimed)
		} else {
			fmt.Println("[tengiz] cleanup complete.")
		}
		return nil
	},
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanup|TestDomainCommandsRegistered|TestVolumeAddCommand" -v -count=1`

Expected: PASS. `TestCleanupDryRunPrintsDockerCommands` runs the real command without a Docker daemon because `--dry-run` returns before `runtime.NewDocker()`.

- [ ] **Step 6: Verify build and vet**

Run: `go build ./... && go vet ./...`

Expected: no output (success), exit code 0.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command with dry-run"
```

---

### Task 4: Documentation updates

**Files:**
- Modify: `README.md` — add CLI Reference section after `### tengiz ps` (after line 150)
- Modify: `AGENTS.md` — add `tengiz cleanup` to the command list (after the `tengiz ps` line)
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:**
- Consumes: the final `tengiz cleanup` flag surface from Task 3
- Produces: documentation consistent with the shipped CLI

- [ ] **Step 1: Add CLI Reference section to `README.md`**

Insert after the `### tengiz ps` section (after line 150, before `### tengiz logs`):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to free disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune unused images not built by Tengiz |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune build cache |
| `--all` | Prune all categories, including volumes |
| `--dry-run` | Print the docker commands without running them |

With no category flags, prunes containers, images, networks and build cache (volumes are excluded for safety). Resources managed by Tengiz are always preserved: containers labeled `tengiz-app` and images tagged `tengiz-apps/*` (including rollback versions). Example: `tengiz cleanup --all` also removes unused volumes.
```

- [ ] **Step 2: Add the command to `AGENTS.md`**

In `AGENTS.md`, after the line `tengiz ps             → list apps from Docker`, add:

```markdown
tengiz cleanup          → prune unused Docker resources (containers/images/networks/volumes/build cache), preserving Tengiz-managed resources
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

Change the P0 table row for Docker Housekeeping (row 19, `| 6 | **Docker Housekeeping** ⬜ |`) to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the `### ✅ Implemented Features (Not Pending)` table (after the `Webhook ile Otomatik Deploy` row):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-08) |
```

- [ ] **Step 4: Verify the full test suite**

Run: `go build ./... && go vet ./... && go test ./... -v -count=1`

Expected: all packages build, vet passes, and every test passes.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**Spec coverage:** Feature #6 Docker Housekeeping from `docs/FUTURES_FEATURES.md` (label-based `docker system prune`, `tengiz cleanup`, protects Tengiz-managed resources) is fully covered: Task 2 implements the label/reference-filtered prune commands; Task 3 ships the CLI; Task 4 documents it. Env-awareness is intentionally omitted (host-level operation, documented in Global Constraints).

**Placeholder scan:** No TBD/TODO. Every code step shows the full source. Expected outputs are concrete.

**Type consistency:** `PruneOptions`/`PruneResult` are defined once in Task 1 and used identically in Tasks 2–3. The interface method signature `Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)` matches the stub, the `dockerRuntime` implementation, and all three mocks. `PrunePlan(opts PruneOptions) []string` and `parseReclaimed(out string) string` are referenced in Tasks 2–3 with matching signatures.
