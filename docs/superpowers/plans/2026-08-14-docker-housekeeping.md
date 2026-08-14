# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, volumes, networks, build cache) with label-based filtering so Tengiz-managed containers and rollback images are never removed, plus optional periodic (`--interval`) runs.

**Architecture:** A new `Prune(ctx, opts)` method on the existing `runtime.Manager` interface (exec-based `dockerRuntime` impl + `stubManager` mock). Docker CLI is invoked per resource category (`docker container prune`, `docker image prune`, `docker volume prune`, `docker network prune`, `docker builder prune`). Safety comes from filters: `label!=tengiz-app` protects Tengiz-managed containers/volumes/networks, and `reference!=tengiz-apps/*` protects Tengiz rollback images. Pure arg-builder helpers keep command construction unit-testable without Docker. A `cleanupCmd` wires flags to the runtime and supports `--dry-run` (list candidates instead of deleting) and `--interval` (periodic loop until Ctrl+C).

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface, Docker CLI via `os/exec`. No new external dependencies.

## Global Constraints

- Docker CLI is invoked via `os/exec` only — no Docker SDK (repo convention)
- Tengiz-managed containers carry label `tengiz-app=<appname>`; these must NEVER be pruned (scale-to-zero stops them intentionally)
- Tengiz images are tagged `tengiz-apps/<app>:<env>-<deploymentID>`; these must NEVER be pruned (rollback depends on them)
- Cleanup is daemon-wide and env-agnostic: the `--env` global flag is accepted (persistent flag on rootCmd) but pruning protects ALL Tengiz-managed resources regardless of environment
- Container prune filter: `label!=tengiz-app` (removes only non-Tengiz stopped containers)
- Image prune filter: `reference!=tengiz-apps/*` (keeps Tengiz images; without `--all`, only dangling images are candidates)
- Volume/network prune filter: `label!=tengiz-app`
- Build cache prune: `docker builder prune -f`
- Default (no category flags): prune ALL categories; `--dry-run` swaps prune commands for equivalent `ls`/`du` listing commands
- No new external dependencies; `go test ./... -v -count=1` must pass
- Tests use the existing patterns: pure arg-builder tests (like `buildLogArgs`/`buildRunArgs`), stub tests, and registration/flag-parsing CLI tests

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneResult`, `Prune` to `Manager` interface + stub impl |
| `internal/runtime/cleanup.go` | `dockerRuntime.Prune` exec impl + pure helpers `buildPruneCommands`, `buildPruneListCommands` + `pruneCommand` type |
| `internal/runtime/cleanup_test.go` | Tests for arg builders + stub + integration test (skips without Docker) |
| `internal/cli/root.go` | Register `cleanupCmd` + flags + `pruneOptionsFromFlags`, `printPruneResult`, `runCleanupLoop` |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` (keeps `var m runtime.Manager = &mockRTForDeploy{}` compiling) |
| `internal/cli/cleanup_test.go` | Registration, flag, options-from-flags, and loop tests |
| `README.md` | CLI Reference section for `tengiz cleanup` |
| `AGENTS.md` | CLI list entry for `tengiz cleanup` |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Add `Prune` to the runtime Manager interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/cli/root_test.go:100` (add `Prune` method to `mockRTForDeploy` so the interface assertion compiles)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{Containers, Images, Volumes, Networks, BuildCache, All, DryRun bool}`, `runtime.PruneResult{DryRun bool; Outputs map[string]string}`, and `Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)` on `runtime.Manager`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil {
		t.Fatal("Prune() returned nil result")
	}
	if res.DryRun {
		t.Fatalf("expected DryRun=false, got %+v", res)
	}
}

func TestStubPruneDryRun(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune(dry run) error = %v", err)
	}
	if res == nil || !res.DryRun {
		t.Fatalf("expected DryRun=true, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestStubPrune" -v -count=1`

Expected: FAIL with `undefined: PruneOptions` / `undefined: Prune`

- [ ] **Step 3: Add types and interface method in `internal/runtime/runtime.go`**

Add after the `RunOptions` struct (line ~29):

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	All        bool
	DryRun     bool
}

type PruneResult struct {
	DryRun  bool
	Outputs map[string]string
}
```

Add `Prune` to the `Manager` interface (after `Run`, line ~48):

```go
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)
```

Add the stub implementation (after `stubManager.Run`, line ~122):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	return &PruneResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 4: Update `mockRTForDeploy` in `internal/cli/root_test.go`**

Add this method after the existing `Run` method (line ~100):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) {
	return &runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/ ./internal/cli/ -run "TestStubPrune|TestMockRTForDeployImplementsManager" -v -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Prune method to Manager interface"
```

---

### Task 2: Pure prune-command arg builders

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `PruneOptions` from Task 1
- Produces: `type pruneCommand struct { category, label string; args []string }`, `buildPruneCommands(opts PruneOptions) []pruneCommand`, `buildPruneListCommands(opts PruneOptions) []pruneCommand`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
	"reflect"
	"testing"
)

func sliceEq(a, b []string) bool {
	return len(a) == len(b) && reflect.DeepEqual(a, b)
}

func cmdMap(cmds []pruneCommand) map[string][]string {
	m := make(map[string][]string, len(cmds))
	for _, c := range cmds {
		m[c.label] = append([]string{c.category}, c.args...)
	}
	return m
}

func TestBuildPruneCommandsAllCategories(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	got := cmdMap(buildPruneCommands(opts))
	expected := map[string][]string{
		"containers":  {"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		"images":      {"image", "prune", "-f", "--filter", "reference!=tengiz-apps/*"},
		"volumes":     {"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
		"networks":    {"network", "prune", "-f", "--filter", "label!=tengiz-app"},
		"build-cache": {"builder", "prune", "-f"},
	}
	for label, want := range expected {
		if !sliceEq(got[label], want) {
			t.Errorf("buildPruneCommands()[%s] = %v, want %v", label, got[label], want)
		}
	}
}

func TestBuildPruneCommandsImageAll(t *testing.T) {
	opts := PruneOptions{Images: true, All: true}
	got := cmdMap(buildPruneCommands(opts))
	want := []string{"image", "prune", "-f", "-a", "--filter", "reference!=tengiz-apps/*"}
	if !sliceEq(got["images"], want) {
		t.Errorf("image --all args = %v, want %v", got["images"], want)
	}
}

func TestBuildPruneCommandsEmpty(t *testing.T) {
	cmds := buildPruneCommands(PruneOptions{})
	if len(cmds) != 0 {
		t.Errorf("expected no commands for empty opts, got %d: %v", len(cmds), cmds)
	}
}

func TestBuildPruneListCommands(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	got := cmdMap(buildPruneListCommands(opts))
	expected := map[string][]string{
		"containers":  {"container", "ls", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app"},
		"images":      {"image", "ls", "--filter", "dangling=true"},
		"volumes":     {"volume", "ls", "--filter", "label!=tengiz-app"},
		"networks":    {"network", "ls", "--filter", "label!=tengiz-app"},
		"build-cache": {"builder", "du"},
	}
	for label, want := range expected {
		if !sliceEq(got[label], want) {
			t.Errorf("buildPruneListCommands()[%s] = %v, want %v", label, got[label], want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestBuildPrune" -v -count=1`

Expected: FAIL with `undefined: buildPruneCommands` / `undefined: pruneCommand`

- [ ] **Step 3: Implement helpers in `internal/runtime/cleanup.go`**

Add to the top of the file (before `RemoveImage`):

```go
type pruneCommand struct {
	category string
	label    string
	args     []string
}

func buildPruneCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand
	if opts.Containers {
		cmds = append(cmds, pruneCommand{
			category: "container",
			label:    "containers",
			args:     []string{"prune", "-f", "--filter", "label!=tengiz-app"},
		})
	}
	if opts.Images {
		args := []string{"prune", "-f"}
		if opts.All {
			args = append(args, "-a")
		}
		args = append(args, "--filter", "reference!=tengiz-apps/*")
		cmds = append(cmds, pruneCommand{category: "image", label: "images", args: args})
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{
			category: "volume",
			label:    "volumes",
			args:     []string{"prune", "-f", "--filter", "label!=tengiz-app"},
		})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{
			category: "network",
			label:    "networks",
			args:     []string{"prune", "-f", "--filter", "label!=tengiz-app"},
		})
	}
	if opts.BuildCache {
		cmds = append(cmds, pruneCommand{
			category: "builder",
			label:    "build-cache",
			args:     []string{"prune", "-f"},
		})
	}
	return cmds
}

func buildPruneListCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand
	if opts.Containers {
		cmds = append(cmds, pruneCommand{
			category: "container",
			label:    "containers",
			args:     []string{"ls", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app"},
		})
	}
	if opts.Images {
		cmds = append(cmds, pruneCommand{
			category: "image",
			label:    "images",
			args:     []string{"ls", "--filter", "dangling=true"},
		})
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{
			category: "volume",
			label:    "volumes",
			args:     []string{"ls", "--filter", "label!=tengiz-app"},
		})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{
			category: "network",
			label:    "networks",
			args:     []string{"ls", "--filter", "label!=tengiz-app"},
		})
	}
	if opts.BuildCache {
		cmds = append(cmds, pruneCommand{
			category: "builder",
			label:    "build-cache",
			args:     []string{"du"},
		})
	}
	return cmds
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestBuildPrune" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add prune command arg builders"
```

---

### Task 3: Implement `dockerRuntime.Prune`

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `buildPruneCommands`, `buildPruneListCommands`, `pruneCommand` from Task 2
- Produces: `(r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)` — runs `docker <category> <args>` for each selected category and collects stdout into `PruneResult.Outputs[label]`

- [ ] **Step 1: Write the integration test (skips when Docker is unavailable)**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func dockerAvailable() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

func TestDockerRuntimePrune(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker not available")
	}
	rt, err := NewDocker()
	if err != nil {
		t.Skipf("NewDocker: %v", err)
	}
	// Dry-run only: must not fail and must list candidate resources.
	res, err := rt.Prune(context.Background(), PruneOptions{
		Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true,
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Prune(dry run) error = %v", err)
	}
	if res == nil || !res.DryRun {
		t.Fatalf("expected dry-run result, got %+v", res)
	}
	for _, label := range []string{"containers", "images", "volumes", "networks", "build-cache"} {
		if _, ok := res.Outputs[label]; !ok {
			t.Errorf("dry-run result missing output for %q", label)
		}
	}
}

func TestDockerRuntimePruneNoDocker(t *testing.T) {
	if dockerAvailable() {
		t.Skip("docker available; this test only checks the error path")
	}
	// Without docker, NewDocker fails before Prune can run.
	if _, err := NewDocker(); err == nil {
		t.Fatal("expected error when docker is missing")
	}
}

func TestPruneCommandAssembly(t *testing.T) {
	// Sanity: every assembled command starts with a valid docker subcommand.
	cmds := buildPruneCommands(PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true})
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(cmds))
	}
	for _, c := range cmds {
		if strings.TrimSpace(c.category) == "" {
			t.Fatalf("pruneCommand has empty category: %+v", c)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestDockerRuntimePrune" -v -count=1`

Expected: FAIL with `dockerRuntime does not implement Manager` (compile error) or `undefined: Prune` on `dockerRuntime`

- [ ] **Step 3: Implement `dockerRuntime.Prune` in `internal/runtime/cleanup.go`**

Add at the end of the file:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	var cmds []pruneCommand
	if opts.DryRun {
		cmds = buildPruneListCommands(opts)
	} else {
		cmds = buildPruneCommands(opts)
	}
	result := &PruneResult{DryRun: opts.DryRun, Outputs: make(map[string]string, len(cmds))}
	for _, c := range cmds {
		args := append([]string{c.category}, c.args...)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker %s: %w\n%s", c.label, err, string(out))
		}
		result.Outputs[c.label] = string(out)
	}
	return result, nil
}
```

- [ ] **Step 4: Run all runtime tests to verify they pass**

Run: `go test ./internal/runtime/ -count=1`

Expected: PASS (integration test skips when Docker is unavailable)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement dockerRuntime.Prune"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.NewDocker()`, `runtime.Prune(ctx, opts)` from Tasks 1-3
- Produces: `cleanupCmd` (registered on `rootCmd`), `pruneOptionsFromFlags(cmd *cobra.Command) (runtime.PruneOptions, error)`, `printPruneResult(res *runtime.PruneResult)`, `runCleanupLoop(ctx context.Context, interval time.Duration, fn func() error) error`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"dry-run", "all", "containers", "images", "volumes", "networks", "build-cache", "interval"} {
		if flags.Lookup(name) == nil {
			t.Fatalf("cleanup missing --%s flag", name)
		}
	}
}

func newCleanupFlagCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	return cmd
}

func TestPruneOptionsFromFlagsDefaultAll(t *testing.T) {
	cmd := newCleanupFlagCmd()
	cmd.ParseFlags([]string{"--dry-run"})
	opts, err := pruneOptionsFromFlags(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.DryRun {
		t.Error("expected DryRun=true")
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories by default, got %+v", opts)
	}
	if opts.All {
		t.Error("expected All=false by default")
	}
}

func TestPruneOptionsFromFlagsExplicitCategories(t *testing.T) {
	cmd := newCleanupFlagCmd()
	cmd.ParseFlags([]string{"--containers", "--images", "--all"})
	opts, err := pruneOptionsFromFlags(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Containers || !opts.Images {
		t.Errorf("expected containers+images, got %+v", opts)
	}
	if opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("expected volumes/networks/build-cache false when categories given, got %+v", opts)
	}
	if !opts.All {
		t.Error("expected All=true")
	}
}

func TestRunCleanupLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	done := make(chan struct{})
	fn := func() error {
		if calls.Add(1) >= 3 {
			cancel()
			close(done)
		}
		return nil
	}

	err := runCleanupLoop(ctx, 10*time.Millisecond, fn)
	if err != nil {
		t.Fatalf("runCleanupLoop() error = %v", err)
	}
	<-done
	if calls.Load() < 3 {
		t.Fatalf("expected at least 3 calls, got %d", calls.Load())
	}
}

func TestPrintPruneResult(t *testing.T) {
	res := &runtime.PruneResult{
		DryRun:  true,
		Outputs: map[string]string{"containers": "abc123\ndef456\n"},
	}
	out := captureOutput(func() {
		printPruneResult(res)
	})
	if out == "" {
		t.Fatal("printPruneResult produced no output")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanup|TestPruneOptionsFromFlags|TestRunCleanupLoop|TestPrintPruneResult" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` / `undefined: pruneOptionsFromFlags` / `undefined: runCleanupLoop`

- [ ] **Step 3: Implement the command in `internal/cli/root.go`**

Add the command definition (place it after `psCmd`, around line 601):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Prunes unused Docker resources while protecting Tengiz-managed containers and
rollback images via label-based filtering.

By default all categories are pruned. Use category flags to limit scope:
--containers, --images, --volumes, --networks, --build-cache.

Use --dry-run to see what would be removed without deleting anything.
Use --all to also remove all unused (non-dangling) images.
Use --interval to run cleanup periodically (e.g. --interval 24h); Ctrl+C to stop.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := pruneOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		interval, _ := cmd.Flags().GetDuration("interval")

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}

		runOnce := func() error {
			res, err := rt.Prune(cmd.Context(), opts)
			if err != nil {
				return err
			}
			printPruneResult(res)
			return nil
		}

		if interval <= 0 {
			return runOnce()
		}
		return runCleanupLoop(cmd.Context(), interval, runOnce)
	},
}
```

Add helpers near the bottom of `internal/cli/root.go` (after `getwd`, around line 1774):

```go
func pruneOptionsFromFlags(cmd *cobra.Command) (runtime.PruneOptions, error) {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")

	opts := runtime.PruneOptions{DryRun: dryRun, All: all}
	if containers || images || volumes || networks || buildCache {
		opts.Containers = containers
		opts.Images = images
		opts.Volumes = volumes
		opts.Networks = networks
		opts.BuildCache = buildCache
	} else {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts, nil
}

func printPruneResult(res *runtime.PruneResult) {
	mode := "Pruned"
	if res.DryRun {
		mode = "Would prune"
	}
	for _, label := range []string{"containers", "images", "volumes", "networks", "build-cache"} {
		if out, ok := res.Outputs[label]; ok && strings.TrimSpace(out) != "" {
			fmt.Printf("[tengiz] %s %s:\n%s", mode, label, out)
		}
	}
	fmt.Println("[tengiz] cleanup complete")
}

func runCleanupLoop(ctx context.Context, interval time.Duration, fn func() error) error {
	if err := fn(); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := fn(); err != nil {
				log.Printf("[tengiz] cleanup: %v", err)
			}
		}
	}
}
```

Register the command and flags in `init()` (after `rootCmd.AddCommand(psCmd)` line 42, and after the existing flag setup):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without deleting")
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune unused images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
	cleanupCmd.Flags().Duration("interval", 0, "run cleanup periodically (e.g. 24h); 0 = run once")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanup|TestPruneOptionsFromFlags|TestRunCleanupLoop|TestPrintPruneResult" -v -count=1`

Expected: PASS

- [ ] **Step 5: Verify the whole project builds**

Run: `go build -o /tmp/tengiz . && go vet ./...`

Expected: build succeeds, vet clean

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Documentation updates

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the `cleanupCmd` surface from Task 4 (`tengiz cleanup`, flags `--dry-run --all --containers --images --volumes --networks --build-cache --interval`)

- [ ] **Step 1: Add `tengiz cleanup` to `README.md` CLI Reference**

Insert a new section after the `### tengiz ps` section (after line 150 in `README.md`):

```markdown
### `tengiz cleanup`

Prune unused Docker resources (containers, images, volumes, networks, build cache) while protecting Tengiz-managed containers and rollback images via label-based filtering. Non-Tengiz stopped containers, dangling images, unused volumes/networks, and build cache are removed.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without deleting anything |
| `--all` | Also remove all unused (non-dangling) images |
| `--containers` | Prune stopped non-Tengiz containers |
| `--images` | Prune unused images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune build cache |
| `--interval <dur>` | Run cleanup periodically (e.g. `24h`); Ctrl+C to stop |

By default all categories are pruned; Tengiz-managed containers (labeled `tengiz-app`) and images tagged `tengiz-apps/*` are always preserved. Use category flags to limit scope. Example:

```bash
tengiz cleanup --dry-run          # preview what would be pruned
tengiz cleanup --containers       # only prune stopped non-Tengiz containers
tengiz cleanup --interval 24h     # run daily until interrupted
```
```

- [ ] **Step 2: Add `tengiz cleanup` to `AGENTS.md` CLI section**

Add this line to the CLI list in `AGENTS.md` (after the `tengiz ps` line):

```
tengiz cleanup [--dry-run] [--all] [--containers] [--images] [--volumes] [--networks] [--build-cache] [--interval DUR]  → prune unused Docker resources (label-based protection for Tengiz containers/images)
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

Replace the feature #6 row (line 19):

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

with:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "✅ Implemented Features (Not Pending)" table (after line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-14) |
```

- [ ] **Step 4: Verify docs render**

Run: `go test ./... -count=1` (full suite — docs change should not break anything)

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Verification (run before declaring done)

```bash
go build -o tengiz .
go vet ./...
go test ./... -v -count=1
```

All three must succeed. The `TestDockerRuntimePrune` integration test skips automatically when Docker is unavailable (as in CI).

## Manual smoke test (optional, requires Docker)

```bash
go build -o tengiz .
./tengiz cleanup --dry-run        # should list candidate resources without deleting
./tengiz cleanup --containers     # should prune only non-Tengiz stopped containers
./tengiz cleanup                  # should prune all categories, protecting tengiz-app containers
```