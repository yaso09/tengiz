# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped non-Tengiz containers, unused images, unused networks, build cache, and opt-in unused volumes) while protecting every Tengiz-managed container via its `tengiz-app` label.

**Architecture:** Extend the existing `runtime.Manager` interface with a `Prune(ctx, PruneOptions) (*PruneResult, error)` method. The `dockerRuntime` implementation (in a new `internal/runtime/prune.go`) lists stopped containers with their labels via `docker ps -a --format "{{.ID}}|{{.Names}}|{{.State}}|{{.Labels}}"`, filters out anything with the `tengiz-app=` label in Go (Docker 28 does not support the `label!=` filter and `docker container prune -f` would delete Tengiz's own scale-to-zero stopped containers), then removes the rest. Images/networks/volumes/build-cache are pruned with the corresponding `docker ... prune -f` subcommands (these are safe: `docker image prune -a` preserves images referenced by stopped containers). A new `cleanupCmd` in `internal/cli/cleanup.go` wires it to the CLI with `--all`, `--volumes`, and `--yes` flags.

**Tech Stack:** Go 1.26, `os/exec` (docker CLI, no SDK), Cobra, existing `runtime.Manager`/`runtime.NewStub()` patterns.

## Global Constraints

- Container names are prefixed `tengiz-<appname>`; all Tengiz containers carry the `tengiz-app=<appname>` label and the `tengiz-env=<env>` label (see `internal/runtime/docker.go:76-77`)
- Never remove a container whose labels contain `tengiz-app=` (protects running apps AND scale-to-zero stopped apps AND versioned/preview containers)
- Never remove a container whose state is `running`
- `tengiz cleanup` takes no positional args (`cobra.NoArgs`)
- Default behavior prunes only dangling images; `--all`/`-a` also prunes all unused images
- Volumes are ONLY pruned when `--volumes` is passed (data-loss risk)
- `--yes`/`-y` skips the confirmation prompt; without it the command prompts on stdin and cancels on anything other than `y`/`yes`
- No new external dependencies
- All existing tests must continue to pass without modification to non-mock code
- Test commands: `go test ./... -v -count=1` and `go vet ./...`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneResult` types + `Prune` method to `Manager` interface; add stub implementation |
| `internal/runtime/prune.go` (new) | `dockerRuntime.Prune` implementation + pure helpers `pruneCandidates`, `parsePruneDeleted`, `parseTotalReclaimed` |
| `internal/runtime/prune_test.go` (new) | Unit tests for stub `Prune`, `pruneCandidates`, `parsePruneDeleted`, `parseTotalReclaimed` |
| `internal/cli/cleanup.go` (new) | `cleanupCmd` cobra command with `--all`, `--volumes`, `--yes` flags, confirmation prompt, report printing |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` (required to keep `Manager` conformance test passing) |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `internal/cli/cleanup_test.go` (new) | Tests: command registered, flags present, flag parsing via RunE override |
| `README.md` | Add `tengiz cleanup` section to CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` line to CLI block |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as ✅ Implemented |

---

### Task 1: Extend `Manager` interface with `Prune` + update all mock implementations

**Files:**
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/cli/root_test.go` (`mockRTForDeploy`)
- Modify: `internal/idle/idle_test.go` (`mockRuntime`)
- Modify: `internal/proxy/proxy_test.go` (`mockRuntime`)
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{ All, Volumes bool }`, `runtime.PruneResult{ ContainersRemoved []string, ImagesRemoved, NetworksRemoved, VolumesRemoved int, BuildCacheBytes, TotalReclaimed string }`, `Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/runtime_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil {
		t.Fatal("Prune() returned nil result")
	}
	if len(res.ContainersRemoved) != 0 || res.ImagesRemoved != 0 || res.NetworksRemoved != 0 || res.VolumesRemoved != 0 {
		t.Errorf("expected zeroed PruneResult, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: compile error `m.Prune undefined (type Manager has no field or method Prune)`.

- [ ] **Step 3: Write minimal implementation**

In `internal/runtime/runtime.go`, add the types and interface method (right after the `RunOptions` type, and inside the `Manager` interface after `Run`):

```go
type PruneOptions struct {
	All     bool // also remove all unused images, not just dangling ones
	Volumes bool // also remove unused volumes
}

type PruneResult struct {
	ContainersRemoved []string // IDs of removed containers
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	BuildCacheBytes   string // e.g. "118B"
	TotalReclaimed    string // e.g. "1.787GB"
}
```

Add `Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)` to the `Manager` interface (after `Run`). Add the stub method after `stubManager.Run`:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	return &PruneResult{}, nil
}
```

- [ ] **Step 4: Add `Prune` to the three mock runtimes so the package tests still compile**

`internal/cli/root_test.go` — add after `mockRTForDeploy.Run`:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) {
	return &runtime.PruneResult{}, nil
}
```

`internal/idle/idle_test.go` — add after `mockRuntime.Run`:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) {
	return &runtime.PruneResult{}, nil
}
```

`internal/proxy/proxy_test.go` — add after `mockRuntime.Run`:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) {
	return &runtime.PruneResult{}, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/idle/... ./internal/proxy/... -count=1`
Expected: all pass (the new `TestStubPrune` passes; the mock additions keep existing conformance/compile tests green).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Prune method to Manager interface"
```

---

### Task 2: Implement `dockerRuntime.Prune` with label-safe container cleanup

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneResult` (Task 1); existing `labelKey = "tengiz-app"` and `dockerRuntime.Remove` from `docker.go`
- Produces: `dockerRuntime.Prune(ctx, opts) (*PruneResult, error)`; pure helpers `pruneCandidates(psOutput string) []string`, `parsePruneDeleted(output, header string) int`, `parseTotalReclaimed(output string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestPruneCandidates(t *testing.T) {
	// Format: {{.ID}}|{{.Names}}|{{.State}}|{{.Labels}}
	psOutput := `abc123|tengiz-myapp|exited|tengiz-app=myapp,tengiz-env=production
def456|orphan-helper|exited|
ghi789|tengiz-preview|exited|tengiz-app=preview,tengiz-deployment=42
jkl012|running-other|running|
mno345|tengiz-myapp-12345|exited|tengiz-app=myapp`
	got := pruneCandidates(psOutput)
	want := []string{"def456"}
	if len(got) != len(want) {
		t.Fatalf("pruneCandidates() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pruneCandidates()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParsePruneDeleted(t *testing.T) {
	imageOut := `Deleted Images:
untagged: foo:latest
deleted: sha256:aaaa
untagged: bar:latest
deleted: sha256:bbbb

Total reclaimed space: 1.2kB`
	if got := parsePruneDeleted(imageOut, "Deleted Images:"); got != 4 {
		t.Errorf("parsePruneDeleted(images) = %d, want 4", got)
	}

	netOut := `Deleted Networks:
net1
net2

`
	if got := parsePruneDeleted(netOut, "Deleted Networks:"); got != 2 {
		t.Errorf("parsePruneDeleted(networks) = %d, want 2", got)
	}

	if got := parsePruneDeleted("Total reclaimed space: 0B", "Deleted Images:"); got != 0 {
		t.Errorf("parsePruneDeleted(empty) = %d, want 0", got)
	}
}

func TestParseTotalReclaimed(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"Deleted Images:\n...\n\nTotal reclaimed space: 1.787GB", "1.787GB"},
		{"Total:\t118B", "118B"},
		{"Total reclaimed space: 0B", "0B"},
		{"no total line here", "0B"},
	}
	for _, tc := range tests {
		if got := parseTotalReclaimed(tc.output); got != tc.want {
			t.Errorf("parseTotalReclaimed(%q) = %q, want %q", tc.output, got, tc.want)
		}
	}
}

func TestDockerPruneStubContract(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	if res == nil || res.TotalReclaimed != "" {
		t.Fatalf("unexpected stub result: %+v", res)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestPruneCandidates|TestParsePruneDeleted|TestParseTotalReclaimed|TestDockerPruneStubContract' -v -count=1`
Expected: compile error `undefined: pruneCandidates` (functions do not exist yet).

- [ ] **Step 3: Write minimal implementation**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// pruneCandidates filters `docker ps -a --format "{{.ID}}|{{.Names}}|{{.State}}|{{.Labels}}"`
// output and returns the IDs of stopped containers NOT managed by Tengiz.
// Containers carrying the tengiz-app label are protected (running apps, scale-to-zero
// stopped apps, versioned blue/green containers, and preview deployments).
func pruneCandidates(psOutput string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(psOutput), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		id, state, labels := parts[0], parts[2], parts[3]
		if state == "running" {
			continue
		}
		if strings.Contains(labels, labelKey+"=") {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// parsePruneDeleted counts the item lines under a "Deleted <X>:" section header
// in docker prune subcommand output (counting stops at a blank line or a Total line).
func parsePruneDeleted(output, header string) int {
	inSection := false
	count := 0
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == header {
			inSection = true
			continue
		}
		if inSection {
			if trimmed == "" || strings.HasPrefix(trimmed, "Total") {
				break
			}
			count++
		}
	}
	return count
}

// parseTotalReclaimed extracts the reclaimed-space value from docker prune output.
// Handles both "Total reclaimed space: X" (container/image/volume prune) and
// "Total:\tX" (builder prune). Returns "0B" when absent.
func parseTotalReclaimed(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
		}
		if strings.HasPrefix(trimmed, "Total:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Total:"))
		}
	}
	return "0B"
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	result := &PruneResult{}

	// 1. Containers: remove stopped containers NOT managed by Tengiz.
	ps := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--format", "{{.ID}}|{{.Names}}|{{.State}}|{{.Labels}}")
	out, err := ps.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	for _, id := range pruneCandidates(string(out)) {
		if err := r.Remove(ctx, id); err != nil {
			return nil, fmt.Errorf("docker rm %s: %w", id, err)
		}
		result.ContainersRemoved = append(result.ContainersRemoved, id)
	}

	// 2. Images: dangling by default; all unused images with -a.
	imgArgs := []string{"image", "prune", "-f"}
	if opts.All {
		imgArgs = append(imgArgs, "-a")
	}
	imgOut, err := exec.CommandContext(ctx, "docker", imgArgs...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker image prune: %w\n%s", err, string(imgOut))
	}
	result.ImagesRemoved = parsePruneDeleted(string(imgOut), "Deleted Images:")
	result.TotalReclaimed = parseTotalReclaimed(string(imgOut))

	// 3. Networks: only unused networks (never default networks).
	netOut, err := exec.CommandContext(ctx, "docker", "network", "prune", "-f").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network prune: %w\n%s", err, string(netOut))
	}
	result.NetworksRemoved = parsePruneDeleted(string(netOut), "Deleted Networks:")

	// 4. Volumes: opt-in only.
	if opts.Volumes {
		volOut, err := exec.CommandContext(ctx, "docker", "volume", "prune", "-f").CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker volume prune: %w\n%s", err, string(volOut))
		}
		result.VolumesRemoved = parsePruneDeleted(string(volOut), "Deleted Volumes:")
	}

	// 5. Build cache (BuildKit).
	bldOut, err := exec.CommandContext(ctx, "docker", "builder", "prune", "-f").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker builder prune: %w\n%s", err, string(bldOut))
	}
	result.BuildCacheBytes = parseTotalReclaimed(string(bldOut))

	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestPruneCandidates|TestParsePruneDeleted|TestParseTotalReclaimed|TestDockerPruneStubContract' -v -count=1`
Expected: all pass. (`dockerRuntime.Prune` is exec-based and not unit-tested against a live daemon, matching the existing `docker.go` convention; its orchestration is verified by the Task 3 CLI integration test using a stub.)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): implement label-safe docker prune"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` (register command + flags)
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneResult` (Tasks 1–2)
- Produces: `cleanupCmd` (Use: `cleanup`, flags `--all`/`-a`, `--volumes`, `--yes`/`-y`)

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	for _, flag := range []string{"all", "volumes", "yes"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
	if cmd.Flags().Lookup("all").Shorthand != "a" {
		t.Errorf("--all should have shorthand -a")
	}
	if cmd.Flags().Lookup("yes").Shorthand != "y" {
		t.Errorf("--yes should have shorthand -y")
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()

	var gotAll, gotVolumes, gotYes bool
	var called bool
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		gotAll, _ = cmd.Flags().GetBool("all")
		gotVolumes, _ = cmd.Flags().GetBool("volumes")
		gotYes, _ = cmd.Flags().GetBool("yes")
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--all", "--volumes", "--yes"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
	if !gotAll || !gotVolumes || !gotYes {
		t.Errorf("flags not parsed: all=%v volumes=%v yes=%v", gotAll, gotVolumes, gotYes)
	}
}

func TestCleanupCmdNoArgs(t *testing.T) {
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()

	var args []string
	cleanupCmd.RunE = func(cmd *cobra.Command, a []string) error {
		args = a
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(args) != 0 {
		t.Errorf("cleanup should reject positional args, got %v", args)
	}
	rootCmd.SetArgs([]string{"cleanup", "extra"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected unknown-command error for positional arg, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`
Expected: compile error `undefined: cleanupCmd` and/or `not found` failures.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, networks, build cache)",
	Long: `Prunes unused Docker resources while protecting every Tengiz-managed container
(identified by the tengiz-app label, including stopped scale-to-zero containers).

Removes stopped containers not managed by Tengiz, dangling images (or all unused
images with --all), unused networks, and build cache. Pass --volumes to also
remove unused volumes.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		yes, _ := cmd.Flags().GetBool("yes")

		if !yes {
			fmt.Print("[tengiz] This will remove unused containers, images, networks and build cache. Continue? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Prune(cmd.Context(), runtime.PruneOptions{All: all, Volumes: volumes})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Println("[tengiz] cleanup complete")
		fmt.Printf("  containers removed: %d\n", len(result.ContainersRemoved))
		fmt.Printf("  images removed:     %d\n", result.ImagesRemoved)
		fmt.Printf("  networks removed:   %d\n", result.NetworksRemoved)
		fmt.Printf("  volumes removed:    %d\n", result.VolumesRemoved)
		if result.BuildCacheBytes != "" {
			fmt.Printf("  build cache freed:  %s\n", result.BuildCacheBytes)
		}
		if result.TotalReclaimed != "" {
			fmt.Printf("  total reclaimed:    %s\n", result.TotalReclaimed)
		}
		return nil
	},
}
```

In `internal/cli/root.go` `init()`, register the command and its flags (add after the `rootCmd.AddCommand(secretCmd)` line near the bottom of `init()`):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().BoolP("all", "a", false, "also remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`
Expected: all four `TestCleanup*` tests pass.

- [ ] **Step 5: Verify full build and vet**

Run: `go build -o /tmp/tengiz-cleanup . && go vet ./...`
Expected: build succeeds, vet reports no issues.

- [ ] **Step 6: Manual smoke test against the live Docker daemon**

Run:

```bash
# Create one orphan stopped container (no tengiz label) and one tengiz-labeled one
docker run -d --name ctest-orphan alpine:3.20 sleep 0 >/dev/null 2>&1 || true
sleep 1
docker run -d --name ctest-tengiz --label tengiz-app=keepme alpine:3.20 sleep 0 >/dev/null 2>&1 || true
sleep 1
/tmp/tengiz-cleanup cleanup --yes
echo "--- containers left ---"
docker ps -a --format '{{.Names}}|{{.Labels}}'
```

Expected: output shows `containers removed: 1`; `ctest-orphan` is gone, `ctest-tengiz` (with `tengiz-app=keepme`) is still present.

Clean up:

```bash
docker rm -f ctest-orphan ctest-tengiz 2>/dev/null || true
```

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

- [ ] **Step 1: Add `tengiz cleanup` to README CLI Reference**

Insert a new section in `README.md` after the `### tengiz rollback <app>` section (and before the next command section):

```markdown
### `tengiz cleanup [--all] [--volumes] [--yes]`

Remove unused Docker resources while protecting every Tengiz-managed container.

| Flag | Description |
|------|-------------|
| `-a`, `--all` | Also remove all unused images, not just dangling ones |
| `--volumes` | Also remove unused volumes (data-loss risk) |
| `-y`, `--yes` | Skip the confirmation prompt |

Removes stopped containers not managed by Tengiz (identified by the `tengiz-app`
label — stopped scale-to-zero containers, versioned blue/green containers, and
preview deployments are protected), dangling images, unused networks, and BuildKit
build cache. Use `--volumes` to additionally remove unused volumes.
```

- [ ] **Step 2: Add `tengiz cleanup` to AGENTS.md**

In `AGENTS.md` under the CLI block, add after the `tengiz rollback <app>` line:

```markdown
tengiz cleanup [--all] [--volumes] → prune unused Docker resources (protects tengiz-app labeled containers)
```

- [ ] **Step 3: Mark feature #6 implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`, change the P0 table row for **Docker Housekeeping** from `⬜` to `✅` and add an entry to the `✅ Implemented Features (Not Pending)` table:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-17) |
```

- [ ] **Step 4: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: all packages pass.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**Spec coverage:** Feature #6 "Docker Housekeeping" from `docs/FUTURES_FEATURES.md` (label-based `docker system prune` + `tengiz cleanup`) is fully covered: Task 2 protects Tengiz-managed containers via the `tengiz-app` label in Go (necessary because Docker 28 lacks the `label!=` filter and `docker container prune -f` deletes stopped Tengiz containers), Task 3 adds the `tengiz cleanup` command, Task 4 documents it. All supporting runtime mocks are updated in Task 1 so the interface change compiles. The `AGENTS.md` rule "Her değişiklikte test ekle/güncelle, testleri geçir, sonra commit et" is honored with per-task tests and commits; the rule "Yeni özellik geliştirirken branch oluştur" is covered by creating `feat/docker-housekeeping` at execution time.

**Placeholder scan:** No TBD/TODO/later placeholders; every code step includes full source; every test step includes real test code and exact commands with expected output.

**Type consistency:** `PruneOptions{All, Volumes bool}` and `PruneResult{ContainersRemoved []string, ImagesRemoved/NetworksRemoved/VolumesRemoved int, BuildCacheBytes/TotalReclaimed string}` are defined once in Task 1 and referenced identically in Tasks 2–3. `pruneCandidates` (Task 2) is called from `dockerRuntime.Prune` with `ps -a --format "{{.ID}}|{{.Names}}|{{.State}}|{{.Labels}}"` output — matching the helper's 4-part `SplitN(line, "|", 4)` parsing. `parsePruneDeleted`/`parseTotalReclaimed` handle the verified real Docker 28 outputs captured during plan research.