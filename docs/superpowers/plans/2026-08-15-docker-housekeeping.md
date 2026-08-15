# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by removing stopped non-Tengiz containers, dangling images, and build cache — while protecting stopped Tengiz containers that scale-to-zero needs for cold-starts.

**Architecture:** Add `Prune(ctx, opts) (PruneResult, error)` to `runtime.Manager`. The Docker implementation lists stopped containers with labels via `docker ps -a --format "{{.ID}}|{{.Labels}}"`, filters out containers labeled `tengiz-app=...` (preserved for cold-starts) unless `--all` is set, removes the rest with `docker rm`, then prunes dangling images (`docker image prune -f`), build cache (`docker builder prune -f`), and optionally anonymous volumes (`docker volume prune -f`). The parsing/filtering logic lives in pure functions (`parseContainerLabelLine`, `selectContainersToPrune`) so it is unit-testable without Docker. A CLI helper `executeCleanup(ctx, pruner, opts)` is testable with a lightweight mock.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface, `os/exec` Docker CLI (no Docker SDK — matches existing runtime package).

## Global Constraints

- Tengiz-managed containers are labeled `tengiz-app=<appname>` (const `labelKey` in `internal/runtime/docker.go`) and MUST NOT be removed by default — they are kept for scale-to-zero cold-starts
- `--all` explicitly opts into removing stopped Tengiz-managed containers
- `--volumes` is required before any volume pruning; volumes are never pruned by default
- Container removal only targets exited/created/dead containers — never running ones
- Only dangling images are pruned (untagged `<none>`); tagged `tengiz-apps/...` images are left for `KeepLastNImages`/rollback
- Default `PruneResult` must be non-nil with empty slices/strings (no nil derefs)
- Adding `Prune` to the `Manager` interface requires updating all mock implementations: `internal/cli/root_test.go` (`mockRTForDeploy`), `internal/idle/idle_test.go` (`mockRuntime`), `internal/proxy/proxy_test.go` (`mockRuntime`)
- No new external dependencies
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneResult` types; add `Prune` to `Manager` interface; add stub implementation |
| `internal/runtime/prune.go` | **New.** Pure label-parsing/filter helpers, `pruneListArgs()`, and the `dockerRuntime.Prune` implementation |
| `internal/runtime/prune_test.go` | **New.** Unit tests for parsing/filter helpers and `pruneListArgs` |
| `internal/runtime/cleanup_test.go` | Add `TestStubPrune` |
| `internal/cli/root.go` | Add `cleanupCmd` (+ `--all`, `--volumes` flags), `containerPruner` interface, `executeCleanup` helper, registration in `init()` |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy`; add `mockRTPrune`; tests for registration/flags/`executeCleanup` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `README.md` | Add `### tengiz cleanup` section to CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

---

### Task 1: `runtime.Manager.Prune` interface, types, and stub

**Files:**
- Modify: `internal/runtime/runtime.go` — add types after the `RunOptions` block (line 29), add `Prune` to the interface (after `KeepLastNImages`, line 36), add stub method (after the `KeepLastNImages` stub, line 119)
- Modify: `internal/cli/root_test.go` — add `Prune` method to `mockRTForDeploy` (after line 99)
- Modify: `internal/idle/idle_test.go` — add `Prune` method to `mockRuntime` (after line 33)
- Modify: `internal/proxy/proxy_test.go` — add `Prune` method to `mockRuntime` (after line 34)
- Test: `internal/runtime/cleanup_test.go` — add `TestStubPrune`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type PruneOptions struct { All bool; Volumes bool }`
  - `type PruneResult struct { Containers []string; Output string }`
  - `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`

- [ ] **Step 1: Create a feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(res.Containers) != 0 || res.Output != "" {
		t.Fatalf("expected empty PruneResult, got %+v", res)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -count=1`
Expected: FAIL — `m.Prune undefined (type Manager has no field or method Prune)`

- [ ] **Step 4: Implement interface, types, and stub**

In `internal/runtime/runtime.go`, after the `RunOptions` block:

```go
type PruneOptions struct {
	// All removes stopped Tengiz-managed containers too (they are normally
	// preserved for scale-to-zero cold-starts).
	All bool
	// Volumes additionally prunes anonymous Docker volumes.
	Volumes bool
}

type PruneResult struct {
	// Containers holds the IDs of stopped containers that were removed.
	Containers []string
	// Output collects stdout from the docker prune commands (e.g. "Total reclaimed space: 12.5MB").
	Output string
}
```

In the `Manager` interface, after `KeepLastNImages(ctx context.Context, appName string, n int) error`:

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

On the `stubManager`, after the `KeepLastNImages` stub:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}
```

Update the three mock types so the whole module still compiles:

`internal/cli/root_test.go` (after the `KeepLastNImages` method, line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

`internal/idle/idle_test.go` (after the `KeepLastNImages` method, line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

`internal/proxy/proxy_test.go` (after the `KeepLastNImages` method, line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./... -count=1`
Expected: PASS (all packages, including the updated mocks)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Prune interface method and types"
```

---

### Task 2: Pure container label-parsing and selection helpers

**Files:**
- Create: `internal/runtime/prune.go` — `ContainerEntry`, `parseContainerLabelLine`, `hasTengizLabel`, `selectContainersToPrune`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `labelKey` const from `internal/runtime/docker.go` (same package)
- Produces:
  - `type ContainerEntry struct { ID string; Labels map[string]string }`
  - `parseContainerLabelLine(line string) (ContainerEntry, error)`
  - `selectContainersToPrune(entries []ContainerEntry, all bool) []string`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"strings"
	"testing"
)

func TestParseContainerLabelLine(t *testing.T) {
	e, err := parseContainerLabelLine("a1b2c3|tengiz-app=myapp,tengiz-env=production")
	if err != nil {
		t.Fatalf("parseContainerLabelLine() error = %v", err)
	}
	if e.ID != "a1b2c3" {
		t.Errorf("ID = %q, want %q", e.ID, "a1b2c3")
	}
	if e.Labels["tengiz-app"] != "myapp" {
		t.Errorf("tengiz-app label = %q, want %q", e.Labels["tengiz-app"], "myapp")
	}
	if e.Labels["tengiz-env"] != "production" {
		t.Errorf("tengiz-env label = %q, want %q", e.Labels["tengiz-env"], "production")
	}
}

func TestParseContainerLabelLineNoLabels(t *testing.T) {
	e, err := parseContainerLabelLine("a1b2c3|")
	if err != nil {
		t.Fatalf("parseContainerLabelLine() error = %v", err)
	}
	if e.ID != "a1b2c3" || len(e.Labels) != 0 {
		t.Errorf("got %+v, want ID a1b2c3 with no labels", e)
	}
}

func TestParseContainerLabelLineInvalid(t *testing.T) {
	if _, err := parseContainerLabelLine("no-pipe-here"); err == nil {
		t.Error("expected error for malformed line")
	}
}

func TestSelectContainersToPrune(t *testing.T) {
	entries := []ContainerEntry{
		{ID: "aaa", Labels: map[string]string{labelKey: "myapp"}},
		{ID: "bbb", Labels: map[string]string{"other": "x"}},
		{ID: "ccc", Labels: map[string]string{}},
	}
	got := selectContainersToPrune(entries, false)
	want := []string{"bbb", "ccc"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSelectContainersToPruneAll(t *testing.T) {
	entries := []ContainerEntry{
		{ID: "aaa", Labels: map[string]string{labelKey: "myapp"}},
		{ID: "bbb", Labels: map[string]string{}},
	}
	got := selectContainersToPrune(entries, true)
	if len(got) != 2 {
		t.Fatalf("got %v, want both containers removed", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParse|TestSelect' -count=1`
Expected: FAIL — build error `undefined: parseContainerLabelLine`

- [ ] **Step 3: Write minimal implementation**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"fmt"
	"strings"
)

// ContainerEntry is a single stopped container discovered during cleanup.
type ContainerEntry struct {
	ID     string
	Labels map[string]string
}

// parseContainerLabelLine parses a line from
// `docker ps -a --format "{{.ID}}|{{.Labels}}"`. Labels are comma-separated
// key=value pairs, e.g. "tengiz-app=myapp,tengiz-env=production".
func parseContainerLabelLine(line string) (ContainerEntry, error) {
	parts := strings.SplitN(line, "|", 2)
	if len(parts) != 2 {
		return ContainerEntry{}, fmt.Errorf("invalid container line %q", line)
	}
	entry := ContainerEntry{
		ID:     parts[0],
		Labels: map[string]string{},
	}
	for _, pair := range strings.Split(parts[1], ",") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			entry.Labels[kv[0]] = kv[1]
		}
	}
	return entry, nil
}

func hasTengizLabel(e ContainerEntry) bool {
	_, ok := e.Labels[labelKey]
	return ok
}

// selectContainersToPrune returns the IDs of containers to remove.
// Tengiz-managed containers are protected (kept for scale-to-zero
// cold-starts) unless all is true.
func selectContainersToPrune(entries []ContainerEntry, all bool) []string {
	var ids []string
	for _, e := range entries {
		if all || !hasTengizLabel(e) {
			ids = append(ids, e.ID)
		}
	}
	return ids
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestParse|TestSelect' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add container label parsing and prune selection helpers"
```

---

### Task 3: Docker `Prune` implementation

**Files:**
- Modify: `internal/runtime/prune.go` — add `pruneListArgs()` and `dockerRuntime.Prune`
- Test: `internal/runtime/prune_test.go` — add `TestPruneListArgs`

**Interfaces:**
- Consumes: `parseContainerLabelLine`, `selectContainersToPrune`, `ContainerEntry`, `PruneOptions`, `PruneResult` from Tasks 1–2
- Produces: `pruneListArgs() []string`, `func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/prune_test.go`:

```go
func TestPruneListArgs(t *testing.T) {
	got := strings.Join(pruneListArgs(), " ")
	for _, want := range []string{
		"ps",
		"-a",
		"--filter status=exited",
		"--filter status=created",
		"--filter status=dead",
		"--format {{.ID}}|{{.Labels}}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("pruneListArgs() missing %q in %q", want, got)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestPruneListArgs -count=1`
Expected: FAIL — build error `undefined: pruneListArgs`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/prune.go`:

```go
func pruneListArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--filter", "status=dead",
		"--format", "{{.ID}}|{{.Labels}}",
	}
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var result PruneResult

	listCmd := exec.CommandContext(ctx, "docker", pruneListArgs()...)
	out, err := listCmd.CombinedOutput()
	if err != nil {
		return result, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}

	var entries []ContainerEntry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		e, perr := parseContainerLabelLine(line)
		if perr != nil {
			continue
		}
		entries = append(entries, e)
	}

	for _, id := range selectContainersToPrune(entries, opts.All) {
		rmCmd := exec.CommandContext(ctx, "docker", "rm", id)
		if rmOut, rerr := rmCmd.CombinedOutput(); rerr != nil {
			log.Printf("[runtime] failed to remove container %s: %v\n%s", id, rerr, string(rmOut))
		} else {
			result.Containers = append(result.Containers, id)
		}
	}

	imagePrune := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
	imageOut, ierr := imagePrune.CombinedOutput()
	if ierr != nil {
		return result, fmt.Errorf("docker image prune: %w\n%s", ierr, string(imageOut))
	}
	result.Output += string(imageOut)

	builderPrune := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	builderOut, berr := builderPrune.CombinedOutput()
	if berr != nil {
		return result, fmt.Errorf("docker builder prune: %w\n%s", berr, string(builderOut))
	}
	result.Output += string(builderOut)

	if opts.Volumes {
		volumePrune := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
		volumeOut, verr := volumePrune.CombinedOutput()
		if verr != nil {
			return result, fmt.Errorf("docker volume prune: %w\n%s", verr, string(volumeOut))
		}
		result.Output += string(volumeOut)
	}

	return result, nil
}
```

Update the imports block at the top of `internal/runtime/prune.go`:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS

- [ ] **Step 5: Verify the full module builds**

Run: `go build -o tengiz . && go vet ./...`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): implement docker Prune for cleanup"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `containerPruner` interface, `executeCleanup` helper, `cleanupCmd`, flags, and registration in `init()` (after line 88)
- Test: `internal/cli/root_test.go` — add `mockRTPrune` and cleanup tests

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.Manager` (Task 1)
- Produces:
  - `type containerPruner interface { Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) }`
  - `func executeCleanup(ctx context.Context, rt containerPruner, opts runtime.PruneOptions) error`
  - `cleanupCmd` with `--all` and `--volumes` flags

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/root_test.go`:

```go
type mockRTPrune struct {
	result runtime.PruneResult
}

func (m *mockRTPrune) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return m.result, nil
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"all", "volumes"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func TestExecuteCleanupNoContainers(t *testing.T) {
	rt := &mockRTPrune{result: runtime.PruneResult{}}
	output := captureOutput(func() {
		if err := executeCleanup(context.Background(), rt, runtime.PruneOptions{}); err != nil {
			t.Fatalf("executeCleanup() error = %v", err)
		}
	})
	if !strings.Contains(output, "no stopped containers to remove") {
		t.Errorf("expected 'no stopped containers to remove', got %q", output)
	}
}

func TestExecuteCleanupPrintsRemoved(t *testing.T) {
	rt := &mockRTPrune{result: runtime.PruneResult{
		Containers: []string{"aaa", "bbb"},
		Output:     "Total reclaimed space: 12.5MB\n",
	}}
	output := captureOutput(func() {
		if err := executeCleanup(context.Background(), rt, runtime.PruneOptions{}); err != nil {
			t.Fatalf("executeCleanup() error = %v", err)
		}
	})
	if !strings.Contains(output, "removed 2 stopped container(s)") {
		t.Errorf("expected 'removed 2 stopped container(s)', got %q", output)
	}
	if !strings.Contains(output, "aaa") || !strings.Contains(output, "bbb") {
		t.Errorf("expected container IDs in output, got %q", output)
	}
	if !strings.Contains(output, "Total reclaimed space: 12.5MB") {
		t.Errorf("expected prune output, got %q", output)
	}
}

func TestExecuteCleanupPropagatesError(t *testing.T) {
	rt := &mockRTPruneErr{}
	if err := executeCleanup(context.Background(), rt, runtime.PruneOptions{}); err == nil {
		t.Fatal("expected error to propagate from Prune")
	}
}
```

Add the error mock type:

```go
type mockRTPruneErr struct{}

func (m *mockRTPruneErr) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{}, fmt.Errorf("docker ps: boom")
}
```

`fmt` is already imported in `internal/cli/root_test.go`. The file already imports `context`, `strings`, `runtime` — no new imports needed.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestExecuteCleanup' -count=1`
Expected: FAIL — build error `undefined: cleanupCmd` (also `executeCleanup`, `mockRTPrune`)

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/root.go`, after the `runCmd` variable block (ends at line 1162, before `gitCmd`), add:

```go
type containerPruner interface {
	Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error)
}

func executeCleanup(ctx context.Context, rt containerPruner, opts runtime.PruneOptions) error {
	result, err := rt.Prune(ctx, opts)
	if err != nil {
		return err
	}
	if len(result.Containers) == 0 {
		fmt.Println("[tengiz] no stopped containers to remove")
	} else {
		fmt.Printf("[tengiz] removed %d stopped container(s):\n", len(result.Containers))
		for _, id := range result.Containers {
			fmt.Printf("  %s\n", id)
		}
	}
	if strings.TrimSpace(result.Output) != "" {
		fmt.Print(result.Output)
		if !strings.HasSuffix(result.Output, "\n") {
			fmt.Println()
		}
	}
	return nil
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Remove stopped non-Tengiz containers, dangling images, and build cache.

Tengiz-managed containers (labeled tengiz-app=...) are preserved even when
stopped, because scale-to-zero cold-starts need them. Use --all to also
remove stopped Tengiz containers. Use --volumes to also remove anonymous
Docker volumes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		return executeCleanup(cmd.Context(), rt, runtime.PruneOptions{All: all, Volumes: volumes})
	},
}
```

In `init()`, after `rootCmd.AddCommand(runCmd)` (line 67), add:

```go
	cleanupCmd.Flags().Bool("all", false, "also remove stopped Tengiz-managed containers")
	cleanupCmd.Flags().Bool("volumes", false, "also prune anonymous Docker volumes")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestExecuteCleanup' -count=1`
Expected: PASS

- [ ] **Step 5: Run the full test suite**

Run: `go test ./... -count=1`
Expected: PASS (all packages)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Documentation updates

**Files:**
- Modify: `README.md` — add `### tengiz cleanup` section after the `tengiz ps` section (after line 150, before `### tengiz logs`)
- Modify: `docs/FUTURES_FEATURES.md` — mark #6 as implemented

- [ ] **Step 1: Update README.md**

Insert after the `tengiz ps` section (after the line `Output: `NAME`, `STATE` (running/stopped), `PORT`, `ENVIRONMENT`, `HEALTH`.`), before `### tengiz logs`:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--all` | Also remove stopped Tengiz-managed containers (normally preserved for scale-to-zero cold-starts) |
| `--volumes` | Also prune anonymous Docker volumes |

Removes stopped containers that are **not** labeled `tengiz-app=...` (i.e. not managed by Tengiz), prunes dangling images, and clears the Docker build cache. Tengiz-managed stopped containers are kept so cold-starts stay fast — use `--all` only when you want to remove them too (e.g. stale versioned containers from failed deployments). `--volumes` is required before any volume pruning. Prints the reclaimed space reported by Docker.
```

- [ ] **Step 2: Update FUTURES_FEATURES.md**

In the P0 table, change feature #6 row:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the "✅ Implemented Features (Not Pending)" table, add a row:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-15) |
```

In the "## Özellikler" section, add a Status line to the "## Docker Housekeeping (Otomatik Temizlik)" entry (after the `- **Why add to Tengiz:**` line):

```markdown
- **Status:** ✅ Implemented (2026-08-15)
```

- [ ] **Step 3: Verify the full build and test suite**

Run: `go build -o tengiz . && go test ./... -count=1 && go vet ./...`
Expected: build OK, all tests PASS, vet clean

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage:** The spec (#6) requires: label-based protection, `tengiz cleanup` command, disk-space reclamation. Tasks 1–3 build the label-protected prune (containers, images, cache, opt-in volumes); Task 4 adds the `tengiz cleanup` CLI; Task 5 documents it. The scale-to-zero safety requirement (never remove stopped Tengiz containers by default) is enforced in `selectContainersToPrune` and covered by tests. Covered.

**2. Placeholder scan:** All steps contain complete code; no TBD/TODO/placeholder patterns. Every code step shows full file content or exact append blocks. Commands include expected output. Covered.

**3. Type consistency:** `PruneOptions{All, Volumes}`, `PruneResult{Containers []string, Output string}`, `ContainerEntry{ID, Labels}`, `parseContainerLabelLine(line) (ContainerEntry, error)`, `selectContainersToPrune(entries, all) []string`, `pruneListArgs() []string`, `executeCleanup(ctx, rt containerPruner, opts) error`, and `containerPruner` are defined once and reused consistently across Tasks 1–4. `runtime.Manager` gains `Prune` exactly once (Task 1) and all three mock implementations are updated in the same task to keep the module compiling. Covered.

---

## Manual Verification (optional, requires Docker)

```bash
go build -o tengiz .
# deploy an app, then stop it — it must survive cleanup
./tengiz deploy .
./tengiz stop myapp
docker run -d --name scratch-nginx nginx 2>/dev/null && docker stop scratch-nginx
./tengiz cleanup          # removes scratch-nginx, keeps tengiz-myapp
./tengiz cleanup --all    # also removes stopped tengiz-myapp
./tengiz cleanup --volumes # additionally prunes anonymous volumes
```
