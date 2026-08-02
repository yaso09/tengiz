# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that frees disk space by pruning unused Docker containers, images, volumes, networks, and build cache while protecting Tengiz-managed containers via labels.

**Architecture:** Add a `Prune` method to the `runtime.Manager` interface (`internal/runtime/prune.go`) that shells out to `docker <category> prune` via `os/exec`, mirroring the existing `KeepLastNImages`/`RemoveImage` pattern in `internal/runtime/cleanup.go`. Default (safe) mode protects every container labeled `tengiz-app`; `--all` prunes everything. A new `cleanupCmd` Cobra command in `internal/cli/root.go` parses flags into a `runtime.PruneOptions` struct and prints a summary report.

**Tech Stack:** Go 1.26 (module `github.com/yaso09/tengiz`), Cobra CLI, Docker CLI via `os/exec` (no Docker SDK).

## Global Constraints

- No Docker SDK — every Docker operation goes through `os/exec` + `docker` CLI (verified: Docker 28.0.4 in this environment).
- Tengiz-managed containers are labeled `tengiz-app=<appname>` (see `internal/runtime/docker.go:76-77`). They must be protected from pruning unless `--all` is given.
- Image tags follow `tengiz-apps/<app>:<env>-<deploymentID>` (see `internal/builder/builder.go:61`).
- Every step must compile; after each task run `go test ./... -v -count=1` and `go vet ./...`.
- Command constants: `go build -o tengiz .`, `go test ./... -v -count=1`, `go vet ./...`.
- Commit after every task with a conventional message (`feat:`, `test:`, `docs:`).
- The `--env` global flag does NOT apply here — Docker is host-wide, cleanup is environment-agnostic.
- `runtime.Manager` is implemented by exactly 5 types that MUST all gain a `Prune` method together: `dockerRuntime` (`runtime.go`), `stubManager` (`runtime.go`), `mockRTForDeploy` (`internal/cli/root_test.go`), `mockRuntime` (`internal/idle/idle_test.go`), `mockRuntime` (`internal/proxy/proxy_test.go`).
- No new external dependencies. No UI screens; but AGENTS.md rule requires README/doc updates for user-facing command changes.

---
---

## Task 1: Size parsing & formatting helpers

**Files:**
- Create: `internal/runtime/prune.go` (this task adds the three helper functions only)
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (used by Task 2, Task 3, Task 4):
  - `func parseSize(s string) int64` — parses a Docker size string like `"1.234GB"` → bytes. Recognizes suffixes `TB`, `GB`, `MB`, `kB`, `B` (decimal multipliers: TB=1e12, GB=1e9, MB=1e6, kB=1e3, B=1). Returns 0 on parse failure.
  - `func parseReclaimedSpace(output string) int64` — sums the `Total reclaimed space: <size>` lines found anywhere in a docker prune command's output.
  - `func FormatBytes(n int64) string` — formats bytes as `"500B"`, `"2.50kB"`, `"1.23GB"`, `"3.00TB"` (exported — the CLI package uses it).

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"500B", 500},
		{"2.5kB", 2500},
		{"1.234GB", 1234000000},
		{"12MB", 12000000},
		{"3TB", 3000000000000},
		{"", 0},
		{"not-a-size", 0},
	}
	for _, tt := range tests {
		if got := parseSize(tt.in); got != tt.want {
			t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	out := "Deleted Containers:\nabc123\n\ndef456\n\nTotal reclaimed space: 100MB\n"
	if got := parseReclaimedSpace(out); got != 100000000 {
		t.Errorf("parseReclaimedSpace() = %d, want %d", got, 100000000)
	}
	if got := parseReclaimedSpace("Total reclaimed space: 0B"); got != 0 {
		t.Errorf("parseReclaimedSpace(0B) = %d, want 0", got)
	}
	// Network prune output has no reclaimed-space line.
	if got := parseReclaimedSpace("Deleted Networks:\nnet1\n"); got != 0 {
		t.Errorf("parseReclaimedSpace(no reclaim line) = %d, want 0", got)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{2500, "2.50kB"},
		{1234000000, "1.23GB"},
		{3000000000000, "3.00TB"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.in); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParseSize|TestParseReclaimedSpace|TestFormatBytes' -count=1`
Expected: FAIL — build errors `undefined: parseSize`, `undefined: parseReclaimedSpace`, `undefined: FormatBytes`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"strconv"
	"strings"
)

var sizeUnits = []struct {
	suffix string
	mult   int64
}{
	{"TB", 1_000_000_000_000},
	{"GB", 1_000_000_000},
	{"MB", 1_000_000},
	{"kB", 1_000},
	{"B", 1},
}

func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	for _, u := range sizeUnits {
		if strings.HasSuffix(s, u.suffix) {
			numStr := strings.TrimSuffix(s, u.suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0
			}
			return int64(num * float64(u.mult))
		}
	}
	return 0
}

func parseReclaimedSpace(output string) int64 {
	var total int64
	for _, line := range strings.Split(output, "\n") {
		const marker = "Total reclaimed space:"
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		val := strings.TrimSpace(line[idx+len(marker):])
		total += parseSize(val)
	}
	return total
}

func FormatBytes(n int64) string {
	if n < 1_000 {
		return strconv.FormatInt(n, 10) + "B"
	}
	for _, u := range sizeUnits {
		if n >= u.mult {
			return strconv.FormatFloat(float64(n)/float64(u.mult), 'f', 2, 64) + u.suffix
		}
	}
	return strconv.FormatInt(n, 10) + "B"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestParseSize|TestParseReclaimedSpace|TestFormatBytes' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add docker prune size parsing helpers"
```

---

## Task 2: `PruneOptions`/`PruneReport` types + command builder

**Files:**
- Modify: `internal/runtime/prune.go` (append types + `buildPruneCommands` + `pruneCommand`)
- Test: `internal/runtime/prune_test.go` (append tests)

**Interfaces:**
- Consumes: nothing from Task 1 yet (types are self-contained), but `formatBytes`-style helpers already exist in the package.
- Produces (used by Task 3 and Task 4):
  - `type PruneOptions struct { Containers bool; Images bool; Volumes bool; Networks bool; BuildCache bool; All bool; DryRun bool }`
  - `type PruneReport struct { ContainersPruned bool; ImagesPruned bool; VolumesPruned bool; NetworksPruned bool; BuildCachePruned bool; ReclaimedBytes int64; DryRun bool; Summary string }`
  - `type pruneCommand struct { name string; args []string }`
  - `func buildPruneCommands(opts PruneOptions) []pruneCommand` — returns the ordered list of docker prune invocations for the enabled categories. **The Tengiz protection rule lives here:** when `opts.All` is false, the containers command appends `--filter label!=tengiz-app`; when `opts.All` is true, containers get no filter and images use `-a` (prune all unused). Order is always: containers, images, volumes, networks, build-cache.

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/prune_test.go`:

```go
func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestBuildPruneCommandsDefault(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	cmds := buildPruneCommands(opts)
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d: %+v", len(cmds), cmds)
	}
	if cmds[0].name != "containers" {
		t.Errorf("expected first command to be containers, got %q", cmds[0].name)
	}
	if !containsArg(cmds[0].args, "label!=tengiz-app") {
		t.Errorf("containers prune missing Tengiz protection filter: %v", cmds[0].args)
	}
	if containsArg(cmds[1].args, "-a") {
		t.Errorf("default images prune must NOT use -a (keeps tagged images): %v", cmds[1].args)
	}
}

func TestBuildPruneCommandsAll(t *testing.T) {
	opts := PruneOptions{All: true, Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	cmds := buildPruneCommands(opts)
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(cmds))
	}
	if containsArg(cmds[0].args, "label!=tengiz-app") {
		t.Errorf("--all must NOT protect Tengiz containers: %v", cmds[0].args)
	}
	if !containsArg(cmds[1].args, "-a") {
		t.Errorf("--all images prune should include -a: %v", cmds[1].args)
	}
}

func TestBuildPruneCommandsCategory(t *testing.T) {
	opts := PruneOptions{Volumes: true}
	cmds := buildPruneCommands(opts)
	if len(cmds) != 1 || cmds[0].name != "volumes" {
		t.Fatalf("expected only volumes command, got %+v", cmds)
	}
	if !containsArg(cmds[0].args, "volume") || !containsArg(cmds[0].args, "prune") {
		t.Errorf("unexpected volumes args: %v", cmds[0].args)
	}
}

func TestBuildPruneCommandsEmpty(t *testing.T) {
	cmds := buildPruneCommands(PruneOptions{})
	if len(cmds) != 0 {
		t.Fatalf("expected no commands for empty options, got %+v", cmds)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestBuildPruneCommands' -count=1`
Expected: FAIL — build errors `undefined: PruneOptions`, `undefined: buildPruneCommands`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/prune.go`:

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

type PruneReport struct {
	ContainersPruned bool
	ImagesPruned     bool
	VolumesPruned    bool
	NetworksPruned   bool
	BuildCachePruned bool
	ReclaimedBytes   int64
	DryRun           bool
	Summary          string
}

type pruneCommand struct {
	name string
	args []string
}

func buildPruneCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand

	containerArgs := []string{"container", "prune", "-f"}
	if !opts.All {
		containerArgs = append(containerArgs, "--filter", "label!=tengiz-app")
	}
	if opts.Containers {
		cmds = append(cmds, pruneCommand{name: "containers", args: containerArgs})
	}

	imageArgs := []string{"image", "prune", "-f"}
	if opts.All {
		imageArgs = []string{"image", "prune", "-af"}
	}
	if opts.Images {
		cmds = append(cmds, pruneCommand{name: "images", args: imageArgs})
	}

	if opts.Volumes {
		cmds = append(cmds, pruneCommand{name: "volumes", args: []string{"volume", "prune", "-f"}})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{name: "networks", args: []string{"network", "prune", "-f"}})
	}
	if opts.BuildCache {
		cmds = append(cmds, pruneCommand{name: "build-cache", args: []string{"builder", "prune", "-f"}})
	}
	return cmds
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestBuildPruneCommands' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): define prune options and command builder with label protection"
```

---

## Task 3: `Manager.Prune` interface, `dockerRuntime.Prune`, and mock updates

**Files:**
- Modify: `internal/runtime/prune.go` (append `runDockerPrune` + `dockerRuntime.Prune`)
- Modify: `internal/runtime/runtime.go:36` (add `Prune` to `Manager` interface) and `internal/runtime/runtime.go:117` (add `stubManager.Prune`)
- Modify: `internal/cli/root_test.go:99` (add `Prune` to `mockRTForDeploy`)
- Modify: `internal/idle/idle_test.go:33` (add `Prune` to `mockRuntime`)
- Modify: `internal/proxy/proxy_test.go:34` (add `Prune` to `mockRuntime`)
- Test: `internal/runtime/prune_test.go` (append stub test + compile-time interface assertion)

**Interfaces:**
- Consumes:
  - From Task 1: `parseReclaimedSpace(output string) int64`
  - From Task 2: `PruneOptions`, `PruneReport`, `buildPruneCommands(opts) []pruneCommand`
- Produces (used by Task 4):
  - `Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)` — new interface method. When `opts.DryRun` is true, runs `docker system df` and returns `&PruneReport{DryRun: true, Summary: <raw output>}`. Otherwise runs each command from `buildPruneCommands(opts)`, accumulates `ReclaimedBytes`, and sets each `*Pruned` bool to true when that command's output contained `"Deleted"`.
  - `func runDockerPrune(ctx context.Context, args []string) (pruned bool, reclaimedBytes int64, err error)` — runs `docker <args...>`, returns `pruned = strings.Contains(out, "Deleted")`, `reclaimedBytes = parseReclaimedSpace(out)`.

- [ ] **Step 1: Write the failing tests and update the three test mocks**

Append to `internal/runtime/prune_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	if !report.DryRun {
		t.Error("stub Prune() should return a dry-run report")
	}
}

func TestDockerRuntimeImplementsManager(t *testing.T) {
	var _ Manager = &dockerRuntime{}
}
```

`TestStubPrune` needs the `context` import — `prune_test.go` already imports `"testing"` only; add `"context"` to its import block.

Add a no-op `Prune` to `mockRTForDeploy` in `internal/cli/root_test.go` (after line 99, the `KeepLastNImages` method):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{DryRun: opts.DryRun}, nil
}
```

Add a no-op `Prune` to `mockRuntime` in `internal/idle/idle_test.go` (after the `KeepLastNImages` method, line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{DryRun: opts.DryRun}, nil
}
```

Add a no-op `Prune` to `mockRuntime` in `internal/proxy/proxy_test.go` (after the `KeepLastNImages` method, line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ ./internal/cli/ ./internal/idle/ ./internal/proxy/ -run 'TestStubPrune|TestDockerRuntimeImplementsManager' -count=1`
Expected: FAIL — build error `cannot use &dockerRuntime{} (type *dockerRuntime) as type Manager in ...: missing method Prune` and `m.Prune undefined (type Manager has no field or method Prune)`.

- [ ] **Step 3: Write the implementation**

Append to `internal/runtime/prune.go`:

```go
func runDockerPrune(ctx context.Context, args []string) (bool, int64, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, 0, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.Contains(string(out), "Deleted"), parseReclaimedSpace(string(out)), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "system", "df")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		return &PruneReport{DryRun: true, Summary: string(out)}, nil
	}

	report := &PruneReport{}
	for _, pc := range buildPruneCommands(opts) {
		removed, reclaimed, err := runDockerPrune(ctx, pc.args)
		if err != nil {
			return nil, fmt.Errorf("prune %s: %w", pc.name, err)
		}
		report.ReclaimedBytes += reclaimed
		switch pc.name {
		case "containers":
			report.ContainersPruned = removed
		case "images":
			report.ImagesPruned = removed
		case "volumes":
			report.VolumesPruned = removed
		case "networks":
			report.NetworksPruned = removed
		case "build-cache":
			report.BuildCachePruned = removed
		}
	}
	return report, nil
}
```

Add the missing imports to `internal/runtime/prune.go` (its import block is currently `strconv` + `strings`; change it to):

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)
```

In `internal/runtime/runtime.go`, add `Prune` to the `Manager` interface (after the `KeepLastNImages` line, line 36):

```go
	Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)
```

In `internal/runtime/runtime.go`, add the stub implementation (after `stubManager.KeepLastNImages`, line 119):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	return &PruneReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 4: Run all tests to verify they pass**

Run: `go test ./... -count=1`
Expected: PASS for every package (the new stub test passes; all mock-updated packages still compile and their existing tests pass).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/runtime.go internal/runtime/prune_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Prune to Manager interface with docker exec implementation"
```

---

## Task 4: `cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` (register command in `init()` at ~line 46; define `cleanupCmd` + `cleanupOptions` after `buildLogsCmd`/near `runCmd`)
- Test: `internal/cli/root_test.go` (append CLI tests)

**Interfaces:**
- Consumes:
  - `runtime.NewDocker()` → `runtime.Manager`
  - `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.FormatBytes`
- Produces:
  - `cleanupCmd *cobra.Command` with flags: `--all`, `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache` (all bool, default false). Registered on `rootCmd` in `init()`.
  - `func cleanupOptions(cmd *cobra.Command) runtime.PruneOptions` — reads the flags. `--all` returns every category enabled plus `All: true`. When no category flag is given (and not `--all`), every category defaults to true. `--dry-run` is preserved in both branches.
  - CLI behavior: dry-run prints `report.Summary` verbatim; otherwise prints `[tengiz] cleanup complete — reclaimed <FormatBytes>GB` style line and a per-category status line (`containers: pruned` / `nothing`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

```go
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
	for _, f := range []string{"all", "dry-run", "containers", "images", "volumes", "networks", "build-cache"} {
		if cleanupCmd.Flags().Lookup(f) == nil {
			t.Errorf("cleanupCmd missing --%s flag", f)
		}
	}
}

func captureCleanupOptions(t *testing.T, args []string) runtime.PruneOptions {
	t.Helper()
	var got runtime.PruneOptions
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, a []string) error {
		got = cleanupOptions(cmd)
		return nil
	}
	rootCmd.SetArgs(append([]string{"cleanup"}, args...))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	return got
}

func TestCleanupOptionsDefault(t *testing.T) {
	got := captureCleanupOptions(t, nil)
	if got.All || got.DryRun {
		t.Fatalf("expected All=false DryRun=false, got %+v", got)
	}
	if !got.Containers || !got.Images || !got.Volumes || !got.Networks || !got.BuildCache {
		t.Fatalf("expected all categories true by default, got %+v", got)
	}
}

func TestCleanupOptionsCategory(t *testing.T) {
	got := captureCleanupOptions(t, []string{"--containers"})
	if !got.Containers {
		t.Fatal("expected Containers=true")
	}
	if got.Images || got.Volumes || got.Networks || got.BuildCache {
		t.Fatalf("expected only containers, got %+v", got)
	}
}

func TestCleanupOptionsAll(t *testing.T) {
	got := captureCleanupOptions(t, []string{"--all"})
	if !got.All {
		t.Fatal("expected All=true")
	}
	if !got.Containers || !got.Images || !got.Volumes || !got.Networks || !got.BuildCache {
		t.Fatalf("expected all categories with --all, got %+v", got)
	}
}

func TestCleanupOptionsDryRun(t *testing.T) {
	got := captureCleanupOptions(t, []string{"--dry-run"})
	if !got.DryRun {
		t.Fatal("expected DryRun=true")
	}
	if !got.Containers {
		t.Fatal("expected categories still defaulted to true with --dry-run")
	}
}
```

`root_test.go` already imports `runtime` and `cobra`, so no new imports are needed.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup' -count=1`
Expected: FAIL — build errors `undefined: cleanupCmd`, `undefined: cleanupOptions`.

- [ ] **Step 3: Write the implementation**

In `internal/cli/root.go` `init()`, after `rootCmd.AddCommand(devCmd)` (line 46), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("all", false, "prune ALL Docker resources, including Tengiz-managed containers and all unused images")
	cleanupCmd.Flags().Bool("dry-run", false, "show disk usage summary without pruning anything")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune unused images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
```

Define the command and options function in `internal/cli/root.go` (place after the `buildLogsCmd` block, before `runCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to free disk space",
	Long: `Prune unused Docker resources to free disk space.

By default, prunes stopped non-Tengiz containers, dangling images, unused
volumes, unused networks, and build cache. Tengiz-managed containers
(labeled tengiz-app) are always protected unless --all is given.

Use --dry-run to print the current disk usage summary (docker system df)
without pruning anything. Combine category flags (--containers, --images,
--volumes, --networks, --build-cache) to prune specific resource types.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptions(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		report, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		if report.DryRun {
			fmt.Print(report.Summary)
			return nil
		}
		fmt.Printf("[tengiz] cleanup complete — reclaimed %s\n", runtime.FormatBytes(report.ReclaimedBytes))
		printPruned := func(label string, pruned bool) {
			status := "nothing"
			if pruned {
				status = "pruned"
			}
			fmt.Printf("  %-14s %s\n", label+":", status)
		}
		printPruned("containers", report.ContainersPruned)
		printPruned("images", report.ImagesPruned)
		printPruned("volumes", report.VolumesPruned)
		printPruned("networks", report.NetworksPruned)
		printPruned("build-cache", report.BuildCachePruned)
		return nil
	},
}

func cleanupOptions(cmd *cobra.Command) runtime.PruneOptions {
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")

	if all {
		return runtime.PruneOptions{
			All:        true,
			DryRun:     dryRun,
			Containers: true,
			Images:     true,
			Volumes:    true,
			Networks:   true,
			BuildCache: true,
		}
	}

	if !containers && !images && !volumes && !networks && !buildCache {
		containers, images, volumes, networks, buildCache = true, true, true, true, true
	}

	return runtime.PruneOptions{
		DryRun:     dryRun,
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
	}
}
```

- [ ] **Step 4: Run all tests to verify they pass**

Run: `go test ./... -count=1 && go vet ./...`
Expected: PASS for every package; `go vet` clean.

- [ ] **Step 5: Manual smoke test against real Docker**

Run:
```bash
go build -o tengiz .
./tengiz cleanup --dry-run
```
Expected: prints the `docker system df` table (Images/Containers/Local Volumes/Build Cache rows).

Run:
```bash
./tengiz cleanup
```
Expected: prints `[tengiz] cleanup complete — reclaimed 0B` (or similar) followed by five `nothing` status lines. Must NOT remove any `tengiz-app`-labeled container.

Run:
```bash
./tengiz cleanup --containers --dry-run
```
Expected: still prints the dry-run summary (proves flags parse and dry-run wins).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command for docker housekeeping"
```

---

## Task 5: Documentation and final verification

**Files:**
- Modify: `README.md` (add CLI Reference section after the `tengiz rm <app>` section)
- Modify: `AGENTS.md` (add `tengiz cleanup` to the CLI list; mention `Prune` in the `runtime` architecture row)

**Interfaces:**
- Consumes: the completed `tengiz cleanup` command surface from Task 4.
- Produces: documentation only.

- [ ] **Step 1: Update README.md**

After the `### tengiz rm <app>` section (ends ~line 228), insert:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to free disk space.

By default, removes stopped non-Tengiz containers, dangling images, unused volumes, unused networks, and build cache. Tengiz-managed containers (labeled `tengiz-app`) are always protected unless `--all` is given.

| Flag | Description |
|------|-------------|
| `--all` | Prune ALL Docker resources, including Tengiz-managed containers and all unused images |
| `--dry-run` | Print the disk usage summary (`docker system df`) without pruning anything |
| `--containers` | Prune stopped containers only |
| `--images` | Prune unused images only |
| `--volumes` | Prune unused volumes only |
| `--networks` | Prune unused networks only |
| `--build-cache` | Prune build cache only |

When no category flag is given, all categories are pruned. If `--all` is given, all categories are pruned regardless of category flags.
```

- [ ] **Step 2: Update AGENTS.md**

In the `## CLI` code block, after the `tengiz notification show` line, add:

```
tengiz cleanup [--all] [--dry-run] [--containers|--images|--volumes|--networks|--build-cache] → prune unused Docker resources (Tengiz containers protected by default)
```

In the `## Key architecture` table, in the `runtime.Manager` row, extend the sentence after "`KeepLastNImages` for rollback + image cleanup." with:

```
 Also: `Prune` for `tengiz cleanup` (label-based docker prune, protects `tengiz-app` containers unless `--all`).
```

- [ ] **Step 3: Full verification**

Run:
```bash
go build -o tengiz . && go vet ./... && go test ./... -v -count=1
```
Expected: build succeeds, `go vet` prints nothing, all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage** — FUTURES_FEATURES.md feature #6 ("Docker Housekeeping", P0): "Label-based `docker system prune`. `tengiz cleanup`." and the full feature entry ("kullanılmayan volume, network, container ve image'leri ... Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur"). Covered:
- `tengiz cleanup` command → Task 4.
- Label-based protection of Tengiz containers (`label!=tengiz-app` filter) → Task 2/3.
- Prunes containers, images, volumes, networks, build cache → Task 2/3.
- Disk-usage visibility before/without pruning (`--dry-run` → `docker system df`) → Task 3/4.
- The Coolify `DockerCleanupJob` periodic scheduling is a separate concern (runtime daemon); the manual command is the P0 deliverable. Not included by design — flagged here as a follow-up candidate.

**2. Placeholder scan** — no TBD/TODO, no "handle errors" without code, every step has full code, no "similar to Task N" references.

**3. Type consistency** — `PruneOptions`/`PruneReport` field names are identical across runtime.go, prune.go, all three test mocks, and the CLI. `runtime.FormatBytes` (exported) is used by the CLI; the unexported `parseSize`/`parseReclaimedSpace` are used only in `runtime`. `buildPruneCommands` returns `[]pruneCommand` with `name`/`args` fields consistent with `Prune`'s switch. `cleanupOptions` returns `runtime.PruneOptions` (no error) matching `cleanupCmd.RunE` usage.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-08-02-docker-housekeeping.md`. Two execution options:**

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
