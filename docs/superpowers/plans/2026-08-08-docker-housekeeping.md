# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, dangling images, build cache, and opt-in volumes/networks) with label-based protection so Tengiz-managed containers and rollback images are never removed.

**Architecture:** A new `Cleanup(ctx, opts) (CleanupReport, error)` method on the existing `runtime.Manager` interface. The `dockerRuntime` implementation shells out to the `docker` CLI (consistent with the rest of `internal/runtime`), using label filters (`label!=tengiz-app`, `label!=tengiz-env`) to protect Tengiz-owned containers and avoiding `--all` so tagged rollback images survive. A `--dry-run` mode runs `docker system prune --all --volumes --dry-run` and reports what would be freed without deleting anything. The CLI handler builds a `CleanupOptions` struct from flags and prints a summary report.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec`, existing `runtime.Manager` interface, no new external dependencies. Categories match the existing image cleanup helpers (`RemoveImage`, `KeepLastNImages`).

## Global Constraints

- The `tengiz cleanup` command protects every container carrying a `tengiz-app` or `tengiz-env` Docker label — those are Tengiz's scale-to-zero / rollout containers and must be filtered with `label!=tengiz-app` + `label!=tengiz-env` in all container prune and list commands.
- Image pruning is dangling-only (no `docker image prune -a`): tagged `tengiz-apps/<app>:<env>-<deployment>` images must survive for rollback (`KeepLastNImages`) and image restart.
- File-based, exec-based Docker CLI only. No Docker SDK. Docker must be installed; `tengiz cleanup` errors with a clear message when `docker` is not in PATH.
- No new external dependencies. Only stdlib `os`, `os/exec`, `strings`, `fmt`, `context` plus existing internal packages.
- `tengiz cleanup` operates across the whole host Docker daemon (all environments); it has NO `--env` flag.
- Bidirectional protection: containers, volumes, and networks held by any running container are never pruned by Docker; our count logic (before − after) reflects exact removed counts.
- Every task ends with a commit. Tests must pass before committing.
- After feature implementation, update `README.md` and `docs/FUTURES_FEATURES.md` per AGENTS.md ("UI/UX değişikliklerinde README.md ve dokümantasyonu güncelle").
- All new commands must be added to `README.md` command tables.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupReport` types; add `Cleanup` to `Manager` interface; stub implementation |
| `internal/runtime/housekeeping.go` (new) | `dockerRuntime.Cleanup`, docker CLI arg builders, prune-counting helpers |
| `internal/runtime/housekeeping_test.go` (new) | Unit tests for stub cleanup, docker arg builders, reclaimed-space parser |
| `internal/proxy/proxy_test.go` | Add `Cleanup` stub to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` stub to `mockRuntime` |
| `internal/cli/root_test.go` | Add `Cleanup` stub to `mockRTForDeploy` |
| `internal/cli/cleanup.go` (new) | `cleanupCmd` cobra command + `cleanupOptionsFromFlags` helper |
| `internal/cli/cleanup_test.go` (new) | CLI registration, flag defaults, option resolution tests |
| `internal/cli/root.go` | Register `cleanupCmd` + its flags |
| `README.md` | New `tengiz cleanup` section + command table row |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as implemented |

---

### Task 1: Runtime types, interface method, and stubs

**Files:**
- Modify: `internal/proxy/proxy_test.go` — `mockRuntime`
- Modify: `internal/idle/idle_test.go` — `mockRuntime`
- Modify: `internal/cli/root_test.go` — `mockRTForDeploy`
- Modify: `internal/runtime/runtime.go` — types + interface + stub

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{Containers, Images, Volumes, Networks, Cache, DryRun bool}`, `runtime.CleanupReport{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved int; Reclaimed string; DryRun string}`, `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)`

- [ ] **Step 1: Write the failing test for the stub**

```go
// internal/runtime/housekeeping_test.go
package runtime

import (
	"context"
	"testing"
)

func TestStubManagerCleanup(t *testing.T) {
	m := NewStub()
	rep, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if rep.ContainersRemoved != 0 || rep.ImagesRemoved != 0 ||
		rep.VolumesRemoved != 0 || rep.NetworksRemoved != 0 {
		t.Errorf("stub Cleanup should report zero removals, got %+v", rep)
	}
	if rep.Reclaimed != "" || rep.DryRun != "" {
		t.Errorf("stub Cleanup should return empty strings, got %+v", rep)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubManagerCleanup -v -count=1`

Expected: FAIL with `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add types, interface method, and stub to `internal/runtime/runtime.go`**

Add after the `RunOptions` struct definition (around line 29):

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	Cache      bool
	DryRun     bool
}

type CleanupReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	Reclaimed         string
	DryRun            string
}
```

Add to the `Manager` interface (after `KeepLastNImages`):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)
```

Add the stub method to `stubManager` (after `KeepLastNImages`, line ~119):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
}
```

- [ ] **Step 4: Update the three test mock implementations**

In `internal/proxy/proxy_test.go`, add to `mockRuntime`:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return runtime.CleanupReport{}, nil
}
```

In `internal/idle/idle_test.go`, add to `mockRuntime`:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return runtime.CleanupReport{}, nil
}
```

In `internal/cli/root_test.go`, add to `mockRTForDeploy`:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return runtime.CleanupReport{}, nil
}
```

- [ ] **Step 5: Run tests to verify they pass and everything compiles**

Run: `go build ./...`

Run: `go test ./internal/runtime/... ./internal/proxy/... ./internal/idle/... ./internal/cli/... -count=1`

Expected: All PASS (proxy tests may take ~2s each due to TCP dial timeouts)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/housekeeping_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup method and report types to Manager interface"
```

---

### Task 2: `dockerRuntime.Cleanup` implementation

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go` (extend same file from Task 1)

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupReport` from Task 1
- Produces: `dockerRuntime.Cleanup(ctx, opts) (CleanupReport, error)`; pure helpers `containerPruneArgs()`, `imagePruneArgs()`, `volumePruneArgs()`, `networkPruneArgs()`, `cachePruneArgs()`, `systemPruneDryRunArgs()`, `findReclaimed(string) string`

- [ ] **Step 1: Write failing tests for the arg builders and reclaimed parser**

Add to `internal/runtime/housekeeping_test.go`:

```go
func TestContainerPruneArgs(t *testing.T) {
	want := []string{
		"container", "prune", "--force",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
	got := containerPruneArgs()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

func TestImagePruneArgs(t *testing.T) {
	got := imagePruneArgs()
	if got[0] != "image" && got[0] != "images" {
		t.Errorf("first arg = %q, want image prune", got[0])
	}
	if len(got) != 3 || got[1] != "prune" || got[2] != "--force" {
		t.Errorf("imagePruneArgs should be [image prune --force], got %v", got)
	}
}

func TestCachePruneArgs(t *testing.T) {
	got := cachePruneArgs()
	if len(got) != 3 || got[0] != "builder" || got[1] != "prune" || got[2] != "--force" {
		t.Errorf("cachePruneArgs = %v, want [builder prune --force]", got)
	}
}

func TestFindReclaimed(t *testing.T) {
	out := "Untagged: tengiz-apps/myapp:production-123\nDeleted: sha256:abc\nTotal reclaimed space: 12.5kB\n"
	if got := findReclaimed(out); got != "Total reclaimed space: 12.5kB" {
		t.Errorf("findReclaimed = %q, want the reclaimed line", got)
	}
	if got := findReclaimed("nothing here\n"); got != "" {
		t.Errorf("findReclaimed should return empty when absent, got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestContainerPruneArgs|TestImagePruneArgs|TestCachePruneArgs|TestFindReclaimed' -v -count=1`

Expected: FAIL with `undefined: containerPruneArgs` (etc.)

- [ ] **Step 3: Write `internal/runtime/housekeeping.go`**

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func containerListArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
		"--format", "{{.ID}}",
	}
}

func containerPruneArgs() []string {
	return []string{
		"container", "prune", "--force",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
}

func imagePruneArgs() []string {
	// Dangling-only: never -a, so tagged rollback images survive.
	return []string{"image", "prune", "--force"}
}

func imageListArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "--force"}
}

func volumeListArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "--force"}
}

func cachePruneArgs() []string {
	return []string{"builder", "prune", "--force"}
}

func systemPruneDryRunArgs() []string {
	return []string{"system", "prune", "--all", "--volumes", "--dry-run"}
}

func findReclaimed(output string) string {
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(strings.ToLower(line), "reclaimed") {
			return line
		}
	}
	return ""
}

func (r *dockerRuntime) dockerOut(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

func (r *dockerRuntime) countLines(ctx context.Context, args ...string) (int, error) {
	out, err := r.dockerOut(ctx, args...)
	if err != nil {
		return 0, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n, nil
}

func (r *dockerRuntime) pruneCounted(ctx context.Context, listArgs, pruneArgs []string) (int, string, error) {
	before, err := r.countLines(ctx, listArgs...)
	if err != nil {
		return 0, "", err
	}
	out, err := r.dockerOut(ctx, pruneArgs...)
	if err != nil {
		return 0, "", fmt.Errorf("docker %s: %w\n%s", strings.Join(pruneArgs, " "), err, string(out))
	}
	after, err := r.countLines(ctx, listArgs...)
	if err != nil {
		return 0, "", err
	}
	removed := before - after
	if removed < 0 {
		removed = 0
	}
	return removed, findReclaimed(string(out)), nil
}

func (r *dockerRuntime) countNetworks(ctx context.Context) (int, error) {
	out, err := r.dockerOut(ctx, "network", "ls", "--format", "{{.Name}}")
	if err != nil {
		return 0, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	n := 0
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name = strings.TrimSpace(name)
		switch name {
		case "", "bridge", "host", "none":
			continue
		}
		n++
	}
	return n, nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	var rep CleanupReport

	if opts.DryRun {
		out, err := r.dockerOut(ctx, systemPruneDryRunArgs()...)
		if err != nil {
			return rep, fmt.Errorf("docker system prune --dry-run: %w\n%s", err, string(out))
		}
		rep.DryRun = string(out)
		return rep, nil
	}

	var reclaimed []string

	if opts.Containers {
		n, sum, err := r.pruneCounted(ctx, containerListArgs(), containerPruneArgs())
		if err != nil {
			return rep, err
		}
		rep.ContainersRemoved = n
		if sum != "" {
			reclaimed = append(reclaimed, sum)
		}
	}
	if opts.Images {
		n, sum, err := r.pruneCounted(ctx, imageListArgs(), imagePruneArgs())
		if err != nil {
			return rep, err
		}
		rep.ImagesRemoved = n
		if sum != "" {
			reclaimed = append(reclaimed, sum)
		}
	}
	if opts.Volumes {
		n, sum, err := r.pruneCounted(ctx, volumeListArgs(), volumePruneArgs())
		if err != nil {
			return rep, err
		}
		rep.VolumesRemoved = n
		if sum != "" {
			reclaimed = append(reclaimed, sum)
		}
	}
	if opts.Networks {
		before, err := r.countNetworks(ctx)
		if err != nil {
			return rep, err
		}
		out, err := r.dockerOut(ctx, networkPruneArgs()...)
		if err != nil {
			return rep, fmt.Errorf("docker %s: %w\n%s", strings.Join(networkPruneArgs(), " "), err, string(out))
		}
		after, err := r.countNetworks(ctx)
		if err != nil {
			return rep, err
		}
		removed := before - after
		if removed < 0 {
			removed = 0
		}
		rep.NetworksRemoved = removed
		if sum := findReclaimed(string(out)); sum != "" {
			reclaimed = append(reclaimed, sum)
		}
	}
	if opts.Cache {
		out, err := r.dockerOut(ctx, cachePruneArgs()...)
		if err != nil {
			return rep, fmt.Errorf("docker %s: %w\n%s", strings.Join(cachePruneArgs(), " "), err, string(out))
		}
		if sum := findReclaimed(string(out)); sum != "" {
			reclaimed = append(reclaimed, sum)
		}
	}

	rep.Reclaimed = strings.Join(reclaimed, ", ")
	return rep, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestContainerPruneArgs|TestImagePruneArgs|TestCachePruneArgs|TestFindReclaimed' -v -count=1`

Expected: PASS

- [ ] **Step 5: Run full runtime tests**

Run: `go test ./internal/runtime/... -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat(runtime): implement label-safe docker cleanup with dry-run support"
```

---

### Task 3: CLI `cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/cleanup_test.go` (new)

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.Manager.Cleanup`
- Produces: `cleanupCmd *cobra.Command` (registered on root), `cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"

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

func TestCleanupCommandHasFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache", "all"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanup command missing --%s flag", name)
		}
	}
}

func TestCleanupOptionsFromFlagsDefaults(t *testing.T) {
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.Containers {
		t.Error("Containers should default to true")
	}
	if !opts.Images {
		t.Error("Images should default to true")
	}
	if !opts.Cache {
		t.Error("Cache should default to true")
	}
	if opts.Volumes || opts.Networks {
		t.Error("Volumes and Networks should default to false")
	}
	if opts.DryRun {
		t.Error("DryRun should default to false")
	}
}

func TestCleanupOptionsFromFlagsAll(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", true, "")
	cmd.Flags().Bool("images", true, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("cache", true, "")
	cmd.Flags().Bool("all", false, "")
	cmd.ParseFlags([]string{"--all"})

	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Cache || !opts.Volumes || !opts.Networks {
		t.Errorf("--all should enable every category, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsDedicated(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", true, "")
	cmd.Flags().Bool("images", true, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("cache", true, "")
	cmd.Flags().Bool("all", false, "")
	cmd.ParseFlags([]string{"--volumes", "--dry-run"})

	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Volumes {
		t.Error("--volumes should enable volumes")
	}
	if !opts.DryRun {
		t.Error("--dry-run should be true")
	}
	if opts.Containers != true || opts.Images != true || opts.Cache != true {
		t.Errorf("--volumes should not disable other defaults, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsDisable(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", true, "")
	cmd.Flags().Bool("images", true, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("cache", true, "")
	cmd.Flags().Bool("all", false, "")
	cmd.ParseFlags([]string{"--containers=false"})

	opts := cleanupOptionsFromFlags(cmd)
	if opts.Containers {
		t.Error("--containers=false should disable containers")
	}
	if !opts.Images {
		t.Error("Images should remain enabled")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanupCommand|TestCleanupOptionsFromFlags' -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` and `undefined: cleanupOptionsFromFlags`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	flags := cmd.Flags()
	dryRun, _ := flags.GetBool("dry-run")
	opts := runtime.CleanupOptions{
		Containers: true,
		Images:     true,
		Cache:      true,
		DryRun:     dryRun,
	}
	if all, _ := flags.GetBool("all"); all {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.Cache = true
	}
	for _, name := range []string{"containers", "images", "volumes", "networks", "cache"} {
		if flags.Changed(name) {
			v, _ := flags.GetBool(name)
			switch name {
			case "containers":
				opts.Containers = v
			case "images":
				opts.Images = v
			case "volumes":
				opts.Volumes = v
			case "networks":
				opts.Networks = v
			case "cache":
				opts.Cache = v
			}
		}
	}
	return opts
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources and reclaim disk space",
	Long: `Remove unused Docker resources and reclaim disk space.

Runs label-based pruning that never removes Tengiz-managed containers
(those tagged with tengiz-app or tengiz-env) and never touches tagged
rollback images.

By default it prunes:
  containers   unused containers not managed by Tengiz
  images       dangling images only (rollback images are kept)
  cache        Docker build cache

Opt-in categories:
  --volumes    prune unused local volumes (data loss risk)
  --networks   prune unused custom networks

Examples:
  tengiz cleanup                # containers + images + build cache
  tengiz cleanup --volumes      # also prune unused volumes
  tengiz cleanup --networks     # also prune unused networks
  tengiz cleanup --all          # prune everything including volumes/networks
  tengiz cleanup --dry-run      # show what would be removed without pruning`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptionsFromFlags(cmd)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		rep, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if opts.DryRun {
			fmt.Print(rep.DryRun)
			return nil
		}

		fmt.Printf("[tengiz] containers removed: %d\n", rep.ContainersRemoved)
		fmt.Printf("[tengiz] dangling images removed: %d\n", rep.ImagesRemoved)
		fmt.Printf("[tengiz] volumes removed: %d\n", rep.VolumesRemoved)
		fmt.Printf("[tengiz] networks removed: %d\n", rep.NetworksRemoved)
		if rep.Reclaimed != "" {
			fmt.Printf("[tengiz] reclaimed: %s\n", rep.Reclaimed)
		}
		return nil
	},
}
```

- [ ] **Step 4: Register the command and its flags**

In `internal/cli/root.go` `init()` (after `rootCmd.AddCommand(runCmd)`, line ~67):

```go
	rootCmd.AddCommand(cleanupCmd)
```

Add these in `init()` (after the `webhookCmd.Flags()` block, line ~88) so the flags are registered before tests run:

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without pruning")
	cleanupCmd.Flags().Bool("containers", true, "prune unused containers")
	cleanupCmd.Flags().Bool("images", true, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused named volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused custom networks")
	cleanupCmd.Flags().Bool("cache", true, "prune Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "prune everything including volumes and networks")
```

> **Note:** Flags must be registered in `init()`, NOT in `Execute()`. Tests call `rootCmd.Execute()` (the cobra method) directly, which does not invoke the package-level `Execute()` where `notificationConfigCmd`/`addSecretProviderFlags` flags live. Registering in `init()` matches the pattern used by `logsCmd`/`webhookCmd` and makes `TestCleanupCommandHasFlags` and `TestCleanupOptionsFromFlagsDefaults` pass.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanupCommand|TestCleanupOptionsFromFlags' -v -count=1`

Expected: PASS

- [ ] **Step 6: Run build and cli tests**

Run: `go build ./...`

Run: `go test ./internal/cli/... -count=1`

Expected: Build succeeds, all cli tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Documentation updates

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`
- Modify: `internal/cli/root_test.go` (add registration test)

**Interfaces:**
- Consumes: the `cleanup` command from Task 3
- Produces: user-facing docs for `tengiz cleanup`

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/cleanup_test.go`:

```go
func TestCleanupHelpListsFlags(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--help"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("cleanup --help failed: %v", err)
	}
	for _, flag := range []string{"--dry-run", "--volumes", "--networks", "--all"} {
		// flags are registered on the command — verify presence after Execute
		if cleanupCmd.Flags().Lookup(strings.TrimPrefix(flag, "--")) == nil {
			t.Errorf("cleanup missing flag %q", flag)
		}
	}
}
```

Add the `strings` import to `internal/cli/cleanup_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanupHelpListsFlags -v -count=1`

Expected: FAIL — `undefined: strings` (compiler error); `strings` package not imported in `cleanup_test.go`

- [ ] **Step 3: Add the import and run**

Add `"strings"` to the import block of `internal/cli/cleanup_test.go`, then:

Run: `go test ./internal/cli/ -run TestCleanupHelpListsFlags -v -count=1`

Expected: PASS

- [ ] **Step 4: Add the `tengiz cleanup` section to `README.md`**

Insert after the existing `tengiz run` section (around line 204): a new `### tengiz cleanup` section:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space. Label-based pruning keeps
Tengiz-managed containers and rollback images intact.

```
tengiz cleanup                 # containers + dangling images + build cache
tengiz cleanup --volumes       # also prune unused named volumes
tengiz cleanup --networks      # also prune unused custom networks
tengiz cleanup --all           # prune everything including volumes/networks
tengiz cleanup --dry-run       # show what would be freed without pruned
```

Options:
- `--containers` (default true) — prune unused containers not labeled by Tengiz
- `--images` (default true) — prune dangling images only (keeps rollback images)
- `--cache` (default true) — prune Docker build cache
- `--volumes` — prune unused named volumes (data loss risk)
- `--networks` — prune unused custom networks
- `--all` — enable all categories
- `--dry-run` — print `docker system prune --dry-run` output without pruning
```

- [ ] **Step 5: Add row to the README command table**

In the "Commands" table under the Git Deploy section (around line 572):

```markdown
| `tengiz cleanup` | Prune unused Docker containers/images/volumes/networks and build cache |
```

- [ ] **Step 6: Update `docs/FUTURES_FEATURES.md`**

Change the Priority table row #6 (line ~19) to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based prune. `tengiz cleanup`. Implemented (2026-08-08). |
```

- [ ] **Step 7: Run all tests and vet**

Run: `go test ./... -v -count=1`

Run: `go vet ./...`

Expected: All PASS (excluding known time-sensitive idle tests and slow proxy tests); vet clean

- [ ] **Step 8: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md internal/cli/cleanup_test.go
git commit -m "docs: document tengiz cleanup command"
```

---

### Task 5: Integration verification and self-review

**Files:**
- Tests exist from Tasks 1-4; no new source files
- Optional manual verification (requires real Docker)

**Interfaces:**
- Consumes: everything produced in Tasks 1-4
- Produces: verified `tengiz cleanup` flows

- [ ] **Step 1: Full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS. Note `internal/idle` tests are time-sensitive (50ms granularity) and `internal/proxy` tests take ~2s each; if those are flaky in a given run, re-run the package before concluding failure.

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`

Run: `go build -o tengiz .`

Expected: No issues

- [ ] **Step 3: Manual smoke test (if Docker available)**

```bash
./tengiz cleanup --dry-run
./tengiz cleanup
./tengiz cleanup --volumes --networks
```

Expected: first prints what would be removed; second prints removed counts + reclaimed; third includes volume/network pruning. Tienable depending on the machine's Docker state.

- [ ] **Step 4: Spec coverage self-review**

Check the feature requirements from `docs/FUTURES_FEATURES.md` #6:

- Label-based prune ✔ — `containerListArgs`/`containerPruneArgs` use `label!=tengiz-app` + `label!=tengiz-env`
- Reclaims disk for unused containers/images/volumes/networks/build cache ✔ — `dockerRuntime.Cleanup` runs each category
- Tengiz-managed containers protected ✔ — label filters exclude the tags used by `dockerRuntime.Create`/`CreateVersioned` (both set `tengiz-app` and `tengiz-env`)
- Rollback/scale-to-start images protected ✔ — `imagePruneArgs` is dangling-only; tagged `tengiz-apps/...` images are untouched
- `tengiz cleanup` command ✔ — Task 3
- Docs updated for UI change ✔ — Task 4

- [ ] **Step 5: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "Similar to Task N", "handle edge cases" — none present; every code step contains complete code.

- [ ] **Step 6: Type/signature consistency check**

- `runtime.CleanupOptions{Containers, Images, Volumes, Networks, Cache, DryRun bool}` — same name/fields in Task 1 (types + interface) and Task 3 (`cleanupOptionsFromFlags`)
- `runtime.CleanupReport{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved int; Reclaimed, DryRun string}` — used by `dockerRuntime.Cleanup` and the CLI printout
- `Manager.Cleanup(ctx, opts) (CleanupReport, error)` — stubbed in `stubManager` (Task 1), three test mocks (Task 1), and implemented for `dockerRuntime` (Task 2)
- Flag names (`--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`, `--cache`, `--all`) — identical in tests, helper, and `Execute()` registration

- [ ] **Step 7: Commit any follow-up fixes**

If a test failed in Step 1, fix the source, re-run, then:

```bash
git add -A
git commit -m "fix(runtime): corrections after test suite run"
```

(The commit step is skipped if no fixes were needed.)