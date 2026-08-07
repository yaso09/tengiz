# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, build cache, and opt-in volumes/networks) while always preserving Tengiz-managed apps, so a single-server deployment does not run out of disk.

**Architecture:** Feature #6 in `docs/FUTURES_FEATURES.md`. Extend `runtime.Manager` with a `Cleanup(ctx, CleanupOptions) (CleanupResult, error)` method. A pure, unit-testable function `cleanupCommands` builds the exact `docker ... prune` argv slices from the options (Tengiz containers are protected by the existing `label!=tengiz-app` filter). A `dockerRuntime.Cleanup` executes those commands via `os/exec` (the codebase's established exec-based pattern — no Docker SDK) and a pure `parseReclaimed` helper extracts the reclaimed-space summary. The CLI wires flags → `CleanupOptions` → report.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (no Docker SDK), `runtime.Manager` interface + `stubManager` test mock.

## Global Constraints

- Single Go module `github.com/yaso09/tengiz`; `runtime.Manager` interface is defined in `internal/runtime/runtime.go`.
- No Docker SDK — all Docker interaction via `exec.CommandContext(ctx, "docker", args...)`.
- Real `dockerRuntime` methods CANNOT run in unit tests (no Docker in CI). All testable logic must live in pure functions (`cleanupCommands`, `parseReclaimed`) or the stub manager.
- Tengiz-managed containers are labeled `tengiz-app=<name>`. Cleanup must never remove them — use prune filters `label!=tengiz-app`.
- Container names are prefixed `tengiz-<appname>` / `tengiz-<appname>-<env>`; images are tagged `tengiz-apps/<app>:<env>-<deploymentID>`.
- Run tests with `go test ./... -count=1`. Run vet with `go vet ./...`.
- Docs rule: update `README.md` for any CLI/UX change.
- Add tests for every change; each task ends green and with a commit.

---

### Task 1: Pure planning + reporting helpers

Add the option/result types and two pure functions to the runtime package. These carry all the testable logic and define the exact docker argv used later.

**Files:**
- Modify: `internal/runtime/cleanup.go` (append types + functions)
- Test: `internal/runtime/cleanup_test.go` (append tests)

**Interfaces:**
- Produces:
  - `type CleanupOptions struct { Containers bool; Images bool; Volumes bool; Networks bool; BuildCache bool; Force bool }`
  - `type CleanupResult struct { Categories []string; Reclaimed string }`
  - `func cleanupCommands(opts CleanupOptions) [][]string`
  - `func parseReclaimed(output string) string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestCleanupCommandsDefaultsNone(t *testing.T) {
	cmds := cleanupCommands(CleanupOptions{})
	if len(cmds) != 0 {
		t.Fatalf("expected no commands for empty options, got %d", len(cmds))
	}
}

func TestCleanupCommandsOrder(t *testing.T) {
	opts := CleanupOptions{Containers: true, Images: true, BuildCache: true, Volumes: true, Networks: true}
	cmds := cleanupCommands(opts)
	want := []string{
		"container", "image", "builder", "volume", "network",
	}
	if len(cmds) != len(want) {
		t.Fatalf("expected %d commands, got %d: %v", len(want), len(cmds), cmds)
	}
	for i, first := range want {
		if cmds[i][0] != first {
			t.Errorf("command %d = %q, want %q", i, cmds[i][0], first)
		}
	}
}

func TestCleanupCommandsProtectsTengizContainers(t *testing.T) {
	var containerCmd []string
	for _, cmds := range cleanupCommands(CleanupOptions{Containers: true}) {
		if cmds[0] == "container" {
			containerCmd = cmds
		}
	}
	if containerCmd == nil {
		t.Fatal("container prune command not generated")
	}
	found := false
	for _, a := range containerCmd {
		if a == "label!=tengiz-app" {
			found = true
		}
	}
	if !found {
		t.Fatal("container prune command missing label!=tengiz-app protection")
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	out := "Deleted Containers:\n...\nTotal reclaimed space: 247.4MB\n"
	got := parseReclaimed(out)
	if got != "Total reclaimed space: 247.4MB" {
		t.Fatalf("parseReclaimed() = %q, want %q", got, "Total reclaimed space: 247.4MB")
	}
}

func TestParseReclaimedEmpty(t *testing.T) {
	if got := parseReclaimed("nothing here\n"); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run TestCleanupCommands -count=1 -v`
Expected: FAIL with "undefined: cleanupCommands" and "undefined: parseReclaimed".

- [ ] **Step 3: Implement the types and functions**

Append to `internal/runtime/cleanup.go`:

```go
// CleanupOptions controls which Docker resources a Cleanup call prunes.
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	Force      bool
}

// CleanupResult reports what a Cleanup call pruned.
type CleanupResult struct {
	Categories []string
	Reclaimed  string
}

// cleanupCommands returns the docker prune argv vectors for each enabled
// category. Tengiz-managed containers are labeled tengiz-app, so the
// "label!=tengiz-app" filter guarantees a cleanup never deletes a managed app.
func cleanupCommands(opts CleanupOptions) [][]string {
	var cmds [][]string
	if opts.Containers {
		cmds = append(cmds, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.Images {
		cmds = append(cmds, []string{"image", "prune", "-af"})
	}
	if opts.BuildCache {
		cmds = append(cmds, []string{"builder", "prune", "-f"})
	}
	if opts.Volumes {
		cmds = append(cmds, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.Networks {
		cmds = append(cmds, []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	return cmds
}

// parseReclaimed extracts the human-readable "Total reclaimed space" line
// from a docker prune output, or "" if none is present.
func parseReclaimed(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(strings.ToLower(trimmed), "reclaimed") {
			return trimmed
		}
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestCleanupCommands -count=1 -v`
Expected: PASS for all `TestCleanupCommands*` and `TestParseReclaimed*` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add docker cleanups planning and parse helpers"
```

---

### Task 2: Add `Cleanup` to the Manager interface + implementations

Wire the planning layer into `runtime.Manager` with a real exec implementation and a stub, and keep all interface implementers (including the CLI test mock) compiling.

**Files:**
- Modify: `internal/runtime/cleanup.go` (add dockerRuntime method)
- Modify: `internal/runtime/runtime.go:31-49` (interface), `:113-119` (stub)
- Modify: `internal/cli/root_test.go` (`mockRTForDeploy`)

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, `cleanupCommands`, `parseReclaimed` (from Task 1)
- Produces:
  - `Manager.Cleanup(ctx, CleanupOptions) (CleanupResult, error)` — every implementer must be updated
  - CLI consumes `Cleanup` in Task 3.

- [ ] **Step 1: Write the failing stub test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(res.Categories) == 0 {
		t.Fatal("expected non-empty categories from stub Cleanup")
	}
}
```

- [ ] **Step 2: Run test to verify it fails to compile**

Run: `go test ./internal/runtime/ -run TestStubCleanup -count=1`
Expected: COMPILE FAIL "Manager does not implement" (stub lacks Cleanup). Recording this failure confirms the interface change is pending.

- [ ] **Step 3: Add the interface method and implementations**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after `KeepLastNImages`):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

In the same file, add to `stubManager`:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	cmdList := cleanupCommands(opts)
	categories := make([]string, 0, len(cmdList))
	for _, args := range cmdList {
		categories = append(categories, args[0])
	}
	return CleanupResult{Categories: categories}, nil
}
```

In `internal/runtime/cleanup.go`, add the real exec-based implementation:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var result CleanupResult
	var reclaimed []string
	for _, args := range cleanupCommands(opts) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		category := args[0]
		if err != nil {
			result.Categories = append(result.Categories, category+" (failed)")
			log.Printf("[runtime] cleanup failed: docker %s: %v", strings.Join(args, " "), err)
			continue
		}
		result.Categories = append(result.Categories, category)
		if got := parseReclaimed(string(out)); got != "" {
			reclaimed = append(reclaimed, got)
		}
	}
	result.Reclaimed = strings.Join(reclaimed, ", ")
	return result, nil
}
```

In `internal/cli/root_test.go`, add to `mockRTForDeploy` so it still satisfies the interface:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 4: Run the runtime + cli tests to verify everything compiles and passes**

Run: `go test ./internal/runtime/ ./internal/cli/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat: add Cleanup method to runtime Manager"
```

---

### Task 3: `tengiz cleanup` CLI command

Expose the manager as the `tengiz cleanup` command with category flags, opt-in confirmation for destructive categories, and a summary report.

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go:37` — (registration happens inside `cleanup.go`'s `init`, so no edit needed; verify registration test)

**Interfaces:**
- Consumes: `runtime.Manager.Cleanup`, `runtime.NewDocker`, `runtime.CleanupOptions`, `runtime.CleanupResult`
- Produces: global `cleanupCmd *cobra.Command`, helper `confirm(r io.Reader, prompt string) bool`

- [ ] **Step 1: Write failing CLI tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"containers", "images", "build-cache", "volumes", "networks", "all", "force"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestConfirmHTonYes(t *testing.T) {
	if !confirm(strings.NewReader("y\n"), "") {
		t.Fatal("expected 'y' to confirm")
	}
	if !confirm(strings.NewReader("YES\n"), "") {
		t.Fatal("expected 'YES' to confirm")
	}
}

func TestConfirmRejectsNo(t *testing.T) {
	if confirm(strings.NewReader("n\n"), "") {
		t.Fatal("expected 'n' to reject")
	}
	if confirm(strings.NewReader("\n"), "") {
		t.Fatal("expected empty input to reject")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestCleanup -count=1`
Expected: FAIL — `undefined: confirm` and cleanup command not registered.

- [ ] **Step 3: Implement the command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func init() {
	cleanupCmd.Flags().Bool("containers", true, "prune unused Docker containers (Tengiz apps are never touched)")
	cleanupCmd.Flags().Bool("images", true, "prune unused Docker images")
	cleanupCmd.Flags().Bool("build-cache", true, "prune Docker build cache")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused Docker volumes (opt-in, destructive)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused Docker networks (opt-in)")
	cleanupCmd.Flags().Bool("force", false, "skip the confirmation prompt for volumes/networks")
	rootCmd.AddCommand(cleanupCmd)
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources, protecting Tengiz apps",
	Long: `Prunes unused Docker resources to reclaim disk space.

By default it removes:
  - stopped containers not managed by Tengiz (label!=tengiz-app)
  - unused Docker images
  - the Docker build cache

Tengiz-managed containers and the images they reference are always preserved.

opt-in with:
  --volumes   prune unused Docker volumes
  --networks  prune unused Docker networks

Volumes/networks are destructive and prompt for confirmation unless
--force is passed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		force, _ := cmd.Flags().GetBool("force")

		if volumes || networks {
			if !force && !confirm(os.Stdin, "This removes Docker volumes/networks. Continue? [y/N] ") {
				fmt.Println("[tengiz] aborted.")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
			Force:      force,
		}

		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}

		if len(result.Categories) == 0 {
			fmt.Println("[tengiz] nothing to clean up. Enable categories with --containers, --images, --build-cache, --volumes, --networks.")
			return nil
		}

		fmt.Printf("[tengiz] cleanup complete: %s\n", strings.Join(result.Categories, ", "))
		if result.Reclaimed != "" {
			fmt.Printf("[tengiz] %s\n", result.Reclaimed)
		}
		return nil
	},
}

func confirm(r io.Reader, prompt string) bool {
	fmt.Print(prompt)
	answer, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		return false
	}
	a := strings.ToLower(strings.TrimSpace(answer))
	return a == "y" || a == "yes"
}
```

- [ ] **Step 4: Run the CLI tests to verify they pass**

Run: `go test ./internal/cli/ -run TestCleanup -count=1`
Expected: PASS (registration, flags, and confirm tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Documentation update

Document the new command per the repo's "update README for any CLI change" rule and mark feature #6 implemented in the feature backlog.

**Files:**
- Modify: `README.md` (add a `tengiz cleanup` command section between the `rollback` and `domain` sections)

**Interfaces:**
- Consumes: nothing new (doc-only)

- [ ] **Step 1: Add the command docs**

Insert this section after the `### tengiz rollback <app>` section (around `README.md:236`, before `### tengiz domain`):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space without ever touching Tengiz-managed applications. Protected by the `label!=tengiz-app` filter, so running and stopped Tengiz containers and their referencing images are always kept. The last N image versions per app are already retained via `KeepLastNImages` on deploy; this command handles the rest of the Docker daemon's cruft.

| Flag | Default | Description |
|------|---------|-------------|
| `--containers` | `true` | Prune stopped containers not managed by Tengiz |
| `--images` | `true` | Prune unused Docker images |
| `--build-cache` | `true` | Prune the Docker build cache |
| `--volumes` | `false` | Prune unused Docker volumes (destructive, prompts) |
| `--networks` | `false` | Prune unused Docker networks |
| `--force` | `false` | Skip confirmation for volumes/networks |

Example:

```
tengiz cleanup                 # default: containers / images / build-cache
tengiz cleanup --volumes       # also prune volumes (prompts unless --force)
```
```

- [ ] **Step 2: Update the feature backlog**

In `docs/FUTURES_FEATURES.md`, change the P0 table row for **#6 Docker Housekeeping** from ⬜ to ✅ and append a row to the "✅ Implemented Features" table:

```markdown
| — | **Docker Housekeeping** | Çok Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-07) |
```

- [ ] **Step 3: Verify build still passes**

Run: `go build ./... && go vet ./...`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping done"
```

---

## Execution Handoff

After saving the plan, offer execution choice:

**"Plan complete and saved to `docs/superpowers/plans/2026-08-07-docker-housekeeping.md`. Two execution options:**

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?"**