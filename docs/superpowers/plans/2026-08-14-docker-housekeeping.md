# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, and networks while protecting all Tengiz-managed resources via label-based filtering.

**Architecture:** Extend the existing `runtime.Manager` interface with a `Cleanup(ctx, opts)` method. The exec-based `dockerRuntime` implementation lists candidate resources (`docker ps -aq --filter status=exited`, dangling `images`/`volumes`/`networks`), inspects each container's labels, and skips any container carrying the `tengiz-app` label. A new `cleanup` Cobra command builds `CleanupOptions` from flags (defaulting to "all categories" when no scope flag is set) and prints per-category counts. `--dry-run` previews removals without changing state.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (no SDK), existing `runtime.Manager` interface + `NewStub()`/`NewDocker()` factories.

## Global Constraints

- Tengiz-managed containers are identified by the `tengiz-app` label (`internal/runtime/docker.go:76`, `labelKey = "tengiz-app"`) — cleanup must NEVER remove a container bearing this label
- Only stopped/exited containers are candidates; running containers are never touched
- Image, volume, and network pruning only targets `dangling=true` resources
- `runtime.NewDocker()` fails if `docker` is not in PATH — `tengiz cleanup` surfaces this as a `docker: <err>` error
- Default behavior with no scope flags = clean all four categories; adding any scope flag restricts to those categories
- `--dry-run` must not modify Docker state; it only counts what would be removed
- No new external Go dependencies
- Existing tests must keep passing; the `Manager` interface addition requires updating the `mockRTForDeploy` test double in `internal/cli/root_test.go`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `CleanupOptions`, `CleanupResult` types; pure helpers `nonEmptyLines`, `isTengizManaged`; `dockerRuntime.Cleanup` + helpers `runList`, `remove`, `containerLabels` |
| `internal/runtime/cleanup_test.go` | Tests for pure helpers + stub `Cleanup` |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface; add stub implementation |
| `internal/cli/cleanup.go` | New `cleanup` Cobra command + `cleanupOptionsForFlags` helper |
| `internal/cli/root.go` | Register `cleanup` command + flags in `init()` |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy` so it still implements `Manager` |
| `internal/cli/cleanup_test.go` | Tests for command registration + `cleanupOptionsForFlags` |
| `README.md`, `AGENTS.md`, `docs/FUTURES_FEATURES.md` | Documentation of the new command; mark feature #6 implemented |

---

### Task 1: Cleanup types + pure helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` — add types and pure helpers (keep existing `RemoveImage`/`KeepLastNImages`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `CleanupOptions` struct (`Containers`, `Images`, `Volumes`, `Networks`, `DryRun` bools), `CleanupResult` struct (`ContainersRemoved`, `ImagesRemoved`, `VolumesRemoved`, `NetworksRemoved`, `Protected` ints), `nonEmptyLines(s string) []string`, `isTengizManaged(labels map[string]string) bool`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go — append to existing file
func TestNonEmptyLines(t *testing.T) {
	got := nonEmptyLines("a\n\n  b  \n")
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("nonEmptyLines() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("nonEmptyLines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNonEmptyLinesEmpty(t *testing.T) {
	if got := nonEmptyLines("   \n\n"); len(got) != 0 {
		t.Fatalf("nonEmptyLines() = %v, want empty", got)
	}
}

func TestIsTengizManaged(t *testing.T) {
	if !isTengizManaged(map[string]string{"tengiz-app": "myapp", "tengiz-env": "production"}) {
		t.Error("expected tengiz-app label to be managed")
	}
	if isTengizManaged(map[string]string{"com.example.owner": "someone"}) {
		t.Error("expected non-tengiz labels to not be managed")
	}
	if isTengizManaged(nil) {
		t.Error("expected nil labels to not be managed")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestNonEmptyLines|TestIsTengizManaged' -count=1`
Expected: FAIL with `undefined: nonEmptyLines` / `undefined: isTengizManaged`

- [ ] **Step 3: Add types and helpers**

Add `encoding/json` to the imports of `internal/runtime/cleanup.go`, then append:

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	DryRun     bool
}

type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	Protected         int
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func isTengizManaged(labels map[string]string) bool {
	_, ok := labels["tengiz-app"]
	return ok
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestNonEmptyLines|TestIsTengizManaged' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add cleanup types and label-protection helpers"
```

---

### Task 2: Add `Cleanup` to the `Manager` interface + stub + docker implementation

**Files:**
- Modify: `internal/runtime/runtime.go` — add `Cleanup` to `Manager` interface and `stubManager`
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.Cleanup` + `runList`, `remove`, `containerLabels`
- Modify: `internal/cli/root_test.go` — add `Cleanup` method to `mockRTForDeploy` (keeps it implementing `Manager`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, `nonEmptyLines`, `isTengizManaged` from Task 1
- Produces: `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — later tasks rely on this signature
- **Important:** `mockRTForDeploy` in `internal/cli/root_test.go` implements `Manager` (verified by `TestMockRTForDeployImplementsManager` at root_test.go:102). Adding `Cleanup` to the interface breaks that test double — it MUST gain the method in this same task or `go test ./...` will not compile.

- [ ] **Step 1: Add `Cleanup` to the `Manager` interface**

In `internal/runtime/runtime.go`, after `KeepLastNImages(ctx context.Context, appName string, n int) error` (line 36):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

- [ ] **Step 2: Add stub implementation**

In `internal/runtime/runtime.go`, after the `KeepLastNImages` stub (line 117-119):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	res := CleanupResult{}
	if opts.Containers {
		res.ContainersRemoved = 1
	}
	if opts.Images {
		res.ImagesRemoved = 1
	}
	if opts.Volumes {
		res.VolumesRemoved = 1
	}
	if opts.Networks {
		res.NetworksRemoved = 1
	}
	return res, nil
}
```

- [ ] **Step 3: Add `Cleanup` method to `mockRTForDeploy`**

In `internal/cli/root_test.go`, after the `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 4: Write the failing stub test**

```go
// internal/runtime/cleanup_test.go — append
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersRemoved != 1 || res.ImagesRemoved != 1 || res.VolumesRemoved != 1 || res.NetworksRemoved != 1 {
		t.Fatalf("Cleanup() = %+v, want all counts = 1", res)
	}
}
```

- [ ] **Step 5: Run tests to verify the interface change compiles and stub passes**

Run: `go test ./internal/runtime/ ./internal/cli/ -run 'TestStubCleanup|TestMockRTForDeployImplementsManager' -count=1`
Expected: PASS (this confirms the interface + mock updates compile together)

- [ ] **Step 6: Implement `dockerRuntime.Cleanup`**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) runList(ctx context.Context, args ...string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nonEmptyLines(string(out)), nil
}

func (r *dockerRuntime) remove(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}

func (r *dockerRuntime) containerLabels(ctx context.Context, id string) map[string]string {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{json .Config.Labels}}", id)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil {
		return nil
	}
	return m
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	if opts.Containers {
		ids, err := r.runList(ctx, "ps", "-aq", "--filter", "status=exited")
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			if isTengizManaged(r.containerLabels(ctx, id)) {
				res.Protected++
				continue
			}
			if opts.DryRun {
				res.ContainersRemoved++
				continue
			}
			if err := r.remove(ctx, "rm", id); err != nil {
				return res, err
			}
			res.ContainersRemoved++
		}
	}

	if opts.Images {
		ids, err := r.runList(ctx, "images", "-q", "--filter", "dangling=true")
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			if opts.DryRun {
				res.ImagesRemoved++
				continue
			}
			if err := r.remove(ctx, "rmi", "-f", id); err != nil {
				return res, err
			}
			res.ImagesRemoved++
		}
	}

	if opts.Volumes {
		ids, err := r.runList(ctx, "volume", "ls", "-q", "--filter", "dangling=true")
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			if opts.DryRun {
				res.VolumesRemoved++
				continue
			}
			if err := r.remove(ctx, "volume", "rm", id); err != nil {
				return res, err
			}
			res.VolumesRemoved++
		}
	}

	if opts.Networks {
		ids, err := r.runList(ctx, "network", "ls", "-q", "--filter", "dangling=true")
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			if opts.DryRun {
				res.NetworksRemoved++
				continue
			}
			if err := r.remove(ctx, "network", "rm", id); err != nil {
				return res, err
			}
			res.NetworksRemoved++
		}
	}

	return res, nil
}
```

- [ ] **Step 7: Run the full runtime and cli test suites**

Run: `go test ./internal/runtime/ ./internal/cli/ -count=1`
Expected: PASS (no compile errors; `TestMockRTForDeployImplementsManager` still passes)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface with docker implementation"
```

---

### Task 3: Add the `cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` — register command + flags in `init()`
- Test: Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult` from Tasks 1-2
- Produces: `cleanupCmd *cobra.Command`, `cleanupOptionsForFlags(containers, images, volumes, networks, dryRun bool) runtime.CleanupOptions`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"
)

func TestCleanupOptionsForFlags(t *testing.T) {
	all := cleanupOptionsForFlags(false, false, false, false, false)
	if !all.Containers || !all.Images || !all.Volumes || !all.Networks {
		t.Errorf("no flags set should clean all categories, got %+v", all)
	}

	imagesOnly := cleanupOptionsForFlags(false, true, false, false, false)
	if !imagesOnly.Images || imagesOnly.Containers || imagesOnly.Volumes || imagesOnly.Networks {
		t.Errorf("only --images should clean images, got %+v", imagesOnly)
	}

	dry := cleanupOptionsForFlags(false, false, false, false, true)
	if !dry.DryRun {
		t.Error("dry-run flag should pass through to options")
	}
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"containers", "images", "volumes", "networks", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanupOptionsForFlags|TestCleanupCommandRegistered' -count=1`
Expected: FAIL with `undefined: cleanupOptionsForFlags` / `cleanup command not registered`

- [ ] **Step 3: Implement the command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func cleanupOptionsForFlags(containers, images, volumes, networks, dryRun bool) runtime.CleanupOptions {
	none := !containers && !images && !volumes && !networks
	return runtime.CleanupOptions{
		Containers: containers || none,
		Images:     images || none,
		Volumes:    volumes || none,
		Networks:   networks || none,
		DryRun:     dryRun,
	}
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long: `Remove unused Docker containers, images, volumes, and networks.

Tengiz-managed containers are protected by their tengiz-app label. By
default all four resource types are cleaned. Use flags to limit scope.
Use --dry-run to preview what would be removed without changing anything.

Examples:
  tengiz cleanup              # clean all resource types
  tengiz cleanup --images     # only dangling images
  tengiz cleanup --dry-run    # preview only, remove nothing`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		res, err := rt.Cleanup(cmd.Context(), cleanupOptionsForFlags(containers, images, volumes, networks, dryRun))
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "removed"
		if dryRun {
			verb = "would remove"
		}
		fmt.Printf("[tengiz] containers %s: %d\n", verb, res.ContainersRemoved)
		fmt.Printf("[tengiz] images %s: %d\n", verb, res.ImagesRemoved)
		fmt.Printf("[tengiz] volumes %s: %d\n", verb, res.VolumesRemoved)
		fmt.Printf("[tengiz] networks %s: %d\n", verb, res.NetworksRemoved)
		if res.Protected > 0 {
			fmt.Printf("[tengiz] protected Tengiz containers: %d\n", res.Protected)
		}
		return nil
	},
}
```

- [ ] **Step 4: Register the command and flags**

In `internal/cli/root.go` inside `init()` (after `rootCmd.AddCommand(runCmd)` at line 67):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "remove dangling volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove dangling networks")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanupOptionsForFlags|TestCleanupCommandRegistered' -count=1`
Expected: PASS

- [ ] **Step 6: Build the binary to confirm registration**

Run: `go build -o tengiz .`
Expected: no errors; `./tengiz cleanup --help` shows the command and its five flags.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation + full verification

**Files:**
- Modify: `README.md` — document `tengiz cleanup` in the CLI command list
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI section
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:** No code interfaces. Documents the `tengiz cleanup` command from Task 3.

- [ ] **Step 1: Update the README CLI list**

Find the command block in `README.md` (the `tengiz ...` list) and add after the `tengiz ps` line:

```text
tengiz cleanup [-a] [--containers] [--images] [--volumes] [--networks] [--dry-run]  → prune unused Docker resources (protects Tengiz containers)
```

- [ ] **Step 2: Update AGENTS.md CLI list**

In `AGENTS.md`, add after the `tengiz ps` line:

```text
tengiz cleanup [--containers] [--images] [--volumes] [--networks] [--dry-run] → prune unused Docker resources (protects Tengiz containers)
```

- [ ] **Step 3: Mark the feature implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md` line 19, change the P0 row to:

```text
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

And add a row to the `### ✅ Implemented Features (Not Pending)` table (after the Webhook row at line 253):

```text
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-14) |
```

- [ ] **Step 4: Run the full test suite, vet, and build**

Run: `go test ./... -count=1 && go vet ./... && go build -o tengiz .`
Expected: all tests PASS, `go vet` reports no issues, build succeeds.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Self-Review

**Spec coverage:** The P0 feature #6 ("Docker Housekeeping", `tengiz cleanup`) is covered: `tengiz cleanup` command (Task 3), label-based protection of Tengiz containers via `isTengizManaged`/`containerLabels` (Tasks 1-2), per-category pruning of containers/images/volumes/networks (Task 2), and dry-run preview (Tasks 2-3). The interface change + test-double update is handled in Task 2 Step 3 to avoid breaking `TestMockRTForDeployImplementsManager`. Docs are updated in Task 4.

**Placeholder scan:** No "TBD"/"TODO"/"implement later" placeholders; every code step contains full code; every test step contains the exact test body; commands include exact expected output.

**Type consistency:** `CleanupOptions`/`CleanupResult` fields are defined once (Task 1) and used identically in the stub, docker impl (Task 2), `cleanupOptionsForFlags`, and the CLI command (Task 3). `Manager.Cleanup(ctx, opts) (CleanupResult, error)` is the single shared signature. `mockRTForDeploy.Cleanup` and `stubManager.Cleanup` match it.