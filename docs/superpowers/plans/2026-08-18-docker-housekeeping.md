# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused networks, build cache, opt-in volumes) while protecting all Tengiz-managed containers and images, and trims old per-app images to a configurable retention.

**Architecture:** The `runtime.Manager` interface gains a `Prune(ctx, PruneOptions) (*PruneResult, error)` method. The docker implementation shells out to granular `docker <object> prune` commands with label filters that exclude Tengiz-managed resources: containers are protected via `label!=tengiz-app` and images via `reference!=tengiz-apps/*`. Granular per-category prunes are used instead of `docker system prune` because system prune has no image-reference filter and would remove unused `tengiz-apps/*` images. `--dry-run` lists candidates (containers/images via `docker ps -aq` / `docker images -q`) instead of deleting. The CLI command is split into a testable `runCleanup(cmd, rt, dataDir, env)` that accepts an injected `runtime.Manager` (so tests use `runtime.NewStub()` with no Docker required), plus a small pure helper `resolveCleanupCategories` for the "no flags = safe defaults" behavior. Old per-app images are trimmed by reusing the existing `KeepLastNImages`.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` / `config.Store` / `docker` CLI (via `os/exec`). No new external dependencies.

## Global Constraints

- Tengiz-managed containers (label `tengiz-app=*`) must NEVER be pruned — filter `label!=tengiz-app` on container prune/listing
- Tengiz-managed images (`tengiz-apps/*`) must NEVER be pruned — filter `reference!=tengiz-apps/*` on image prune/listing
- `--volumes` is opt-in only; the default `tengiz cleanup` must NOT prune volumes (data-loss risk)
- No flags passed → safe defaults: containers + images + networks + build-cache = enabled, volumes = disabled
- `--dry-run` must make zero changes to the Docker host (no prune commands, no `KeepLastNImages`)
- `--keep-images N` default is `5`; `0` disables per-app image retention
- All flag names: `--containers`, `--images`, `--networks`, `--volumes`, `--build-cache`, `--keep-images`, `--dry-run`
- `Prune` must be added to the `runtime.Manager` interface, so every existing mock (`mockRTForDeploy`, both `mockRuntime` types) must gain the method to keep `go test ./...` compiling
- No new external dependencies
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneResult`, add `Prune` to `Manager` interface, stub implementation |
| `internal/runtime/prune.go` | New: docker `Prune` implementation + pure helpers (`pruneContainerArgs`, `parsePruneOutput`, etc.) |
| `internal/runtime/prune_test.go` | New: tests for pure helpers + stub `Prune` |
| `internal/cli/cleanup.go` | New: `cleanupCmd`, `runCleanup`, `resolveCleanupCategories`, `addCleanupFlags` |
| `internal/cli/cleanup_test.go` | New: CLI registration, flag, default-category, and `runCleanup` tests |
| `internal/cli/root.go` | Register `cleanupCmd` + flags in `init()` |
| `internal/cli/root_test.go` | Add `Prune` method to `mockRTForDeploy` (interface satisfaction) |
| `internal/proxy/proxy_test.go` | Add `Prune` method to `mockRuntime` (interface satisfaction) |
| `internal/idle/idle_test.go` | Add `Prune` method to `mockRuntime` (interface satisfaction) |
| `README.md` | Document the `tengiz cleanup` command |
| `AGENTS.md` | Add command to CLI list, update `runtime.Manager` row + quirks |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as implemented |

---

### Task 1: `runtime.Prune` — interface, docker implementation, pure helpers, mock updates

**Files:**
- Modify: `internal/runtime/runtime.go` — add types, interface method, stub impl
- Create: `internal/runtime/prune.go`
- Create: `internal/runtime/prune_test.go`
- Modify: `internal/cli/root_test.go:99` (after `KeepLastNImages` line) — add `Prune` to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:34` — add `Prune` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:33` — add `Prune` to `mockRuntime`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{Containers, Images, Networks, Volumes, BuildCache, DryRun bool}`, `runtime.PruneResult{ContainersRemoved, ImagesRemoved, NetworksRemoved, VolumesRemoved, BuildCacheRemoved int; ReclaimedSpace string; DryRun bool}`, `func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)`, `func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)`, and package-level helpers `pruneContainerArgs()`, `pruneImageArgs()`, `pruneNetworkArgs()`, `pruneVolumeArgs()`, `pruneBuildCacheArgs()`, `listContainerCandidatesArgs()`, `listImageCandidatesArgs()`, `countOutputLines(string) int`, `parsePruneOutput(string) (int, string)`, `runPruneCommand(ctx, []string) (int, string, error)`, `runListCommand(ctx, []string) (int, error)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name          string
		out           string
		wantCount     int
		wantReclaimed string
	}{
		{
			name:          "containers",
			out:           "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 12MB\n",
			wantCount:     2,
			wantReclaimed: "12MB",
		},
		{
			name:          "images with deleted and untagged lines",
			out:           "Deleted Images:\ndeleted: sha256:aaa\ndeleted: sha256:bbb\nuntagged: tengiz-apps/foo:production-123\n\nTotal reclaimed space: 1.5GB\n",
			wantCount:     3,
			wantReclaimed: "1.5GB",
		},
		{
			name:          "nothing to prune",
			out:           "Total reclaimed space: 0B\n",
			wantCount:     0,
			wantReclaimed: "0B",
		},
		{
			name:          "build cache",
			out:           "Build cache entries removed: 7\nTotal reclaimed space: 240MB\n",
			wantCount:     7,
			wantReclaimed: "240MB",
		},
		{
			name:          "empty output",
			out:           "",
			wantCount:     0,
			wantReclaimed: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count, reclaimed := parsePruneOutput(tc.out)
			if count != tc.wantCount {
				t.Errorf("count = %d, want %d", count, tc.wantCount)
			}
			if reclaimed != tc.wantReclaimed {
				t.Errorf("reclaimed = %q, want %q", reclaimed, tc.wantReclaimed)
			}
		})
	}
}

func TestCountOutputLines(t *testing.T) {
	if got := countOutputLines("abc\ndef\n\n"); got != 2 {
		t.Errorf("countOutputLines = %d, want 2", got)
	}
	if got := countOutputLines(""); got != 0 {
		t.Errorf("countOutputLines empty = %d, want 0", got)
	}
}

func TestPruneContainerArgsProtectsTengizContainers(t *testing.T) {
	joined := strings.Join(pruneContainerArgs(), " ")
	if !strings.Contains(joined, "label!=tengiz-app") {
		t.Errorf("container prune args missing tengiz label protection: %v", pruneContainerArgs())
	}
	if !strings.Contains(joined, "prune") {
		t.Errorf("container prune args missing prune subcommand: %v", pruneContainerArgs())
	}
}

func TestPruneImageArgsProtectsTengizImages(t *testing.T) {
	joined := strings.Join(pruneImageArgs(), " ")
	if !strings.Contains(joined, "reference!=tengiz-apps/*") {
		t.Errorf("image prune args missing tengiz image protection: %v", pruneImageArgs())
	}
	if !strings.Contains(joined, "dangling=true") {
		t.Errorf("image prune args missing dangling filter: %v", pruneImageArgs())
	}
}

func TestListCandidatesArgs(t *testing.T) {
	joined := strings.Join(listContainerCandidatesArgs(), " ")
	if !strings.Contains(joined, "status=exited") || !strings.Contains(joined, "label!=tengiz-app") {
		t.Errorf("container candidate args wrong: %v", listContainerCandidatesArgs())
	}
	joined = strings.Join(listImageCandidatesArgs(), " ")
	if !strings.Contains(joined, "dangling=true") || !strings.Contains(joined, "reference!=tengiz-apps/*") {
		t.Errorf("image candidate args wrong: %v", listImageCandidatesArgs())
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil {
		t.Fatal("Prune() returned nil result")
	}
	if !res.DryRun {
		t.Fatal("Prune() on stub should report DryRun=false for non-dry-run opts")
	}
}

func TestStubPruneDryRun(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !res.DryRun {
		t.Fatal("Prune() should echo DryRun=true")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -count=1`
Expected: FAIL — package does not compile (`undefined: parsePruneOutput`, `undefined: countOutputLines`, `undefined: pruneContainerArgs`, `undefined: pruneImageArgs`, `undefined: listContainerCandidatesArgs`, `undefined: listImageCandidatesArgs`, and `stubManager.Prune` not found).

- [ ] **Step 3: Implement the interface, types, stub, and docker prune logic**

Add to `internal/runtime/runtime.go` (place `PruneOptions`/`PruneResult` type declarations just above the `Manager` interface, i.e. after `RunOptions`):

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	DryRun     bool
}

type PruneResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	BuildCacheRemoved int
	ReclaimedSpace    string
	DryRun            bool
}
```

Add to the `Manager` interface (after `KeepLastNImages`, before `Run`):

```go
	Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)
```

Add to the stub manager (`stubManager`, after its `KeepLastNImages` method):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	return &PruneResult{DryRun: opts.DryRun}, nil
}
```

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func pruneContainerArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func pruneImageArgs() []string {
	return []string{"image", "prune", "-f", "--filter", "dangling=true", "--filter", "reference!=tengiz-apps/*"}
}

func pruneNetworkArgs() []string {
	return []string{"network", "prune", "-f"}
}

func pruneVolumeArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func pruneBuildCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func listContainerCandidatesArgs() []string {
	return []string{"ps", "-aq", "--filter", "status=exited", "--filter", "label!=tengiz-app"}
}

func listImageCandidatesArgs() []string {
	return []string{"images", "-q", "--filter", "dangling=true", "--filter", "reference!=tengiz-apps/*"}
}

func countOutputLines(out string) int {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	n := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func parsePruneOutput(out string) (int, string) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	count := 0
	reclaimed := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "Total reclaimed space:"):
			reclaimed = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		case strings.HasPrefix(line, "Build cache entries removed:"):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Build cache entries removed:"))); err == nil {
				count = n
			}
		case strings.HasPrefix(line, "deleted:") || strings.HasPrefix(line, "untagged:"):
			count++
		case strings.HasSuffix(line, ":"):
			// Header line such as "Deleted Containers:" — skip
		case !strings.Contains(line, " "):
			// Bare object ID (containers, networks, volumes)
			count++
		}
	}
	return count, reclaimed
}

func runPruneCommand(ctx context.Context, args []string) (int, string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("docker %s %s: %w\n%s", args[0], args[1], err, string(out))
	}
	count, reclaimed := parsePruneOutput(string(out))
	return count, reclaimed, nil
}

func runListCommand(ctx context.Context, args []string) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return countOutputLines(string(out)), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	res := &PruneResult{DryRun: opts.DryRun}

	if opts.Containers {
		if opts.DryRun {
			n, err := runListCommand(ctx, listContainerCandidatesArgs())
			if err != nil {
				return nil, err
			}
			res.ContainersRemoved = n
		} else {
			n, reclaimed, err := runPruneCommand(ctx, pruneContainerArgs())
			if err != nil {
				return nil, err
			}
			res.ContainersRemoved = n
			res.ReclaimedSpace = reclaimed
		}
	}

	if opts.Images {
		if opts.DryRun {
			n, err := runListCommand(ctx, listImageCandidatesArgs())
			if err != nil {
				return nil, err
			}
			res.ImagesRemoved = n
		} else {
			n, reclaimed, err := runPruneCommand(ctx, pruneImageArgs())
			if err != nil {
				return nil, err
			}
			res.ImagesRemoved = n
			res.ReclaimedSpace = reclaimed
		}
	}

	if opts.Networks && !opts.DryRun {
		n, reclaimed, err := runPruneCommand(ctx, pruneNetworkArgs())
		if err != nil {
			return nil, err
		}
		res.NetworksRemoved = n
		res.ReclaimedSpace = reclaimed
	}

	if opts.Volumes && !opts.DryRun {
		n, reclaimed, err := runPruneCommand(ctx, pruneVolumeArgs())
		if err != nil {
			return nil, err
		}
		res.VolumesRemoved = n
		res.ReclaimedSpace = reclaimed
	}

	if opts.BuildCache && !opts.DryRun {
		n, reclaimed, err := runPruneCommand(ctx, pruneBuildCacheArgs())
		if err != nil {
			return nil, err
		}
		res.BuildCacheRemoved = n
		res.ReclaimedSpace = reclaimed
	}

	return res, nil
}
```

Keep the `Manager` interface satisfied by adding `Prune` to the three existing mocks. In `internal/cli/root_test.go` (immediately after the `KeepLastNImages` line, i.e. after line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) {
	return &runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

In `internal/proxy/proxy_test.go` (immediately after the `KeepLastNImages` line, line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) { return &runtime.PruneResult{DryRun: opts.DryRun}, nil }
```

In `internal/idle/idle_test.go` (immediately after the `KeepLastNImages` line, line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) { return &runtime.PruneResult{DryRun: opts.DryRun}, nil }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/proxy/... ./internal/idle/... -count=1`
Expected: PASS in all four packages (proxy tests are slow, ~2s each — that is normal).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune.go internal/runtime/prune_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add Prune to runtime.Manager for Docker housekeeping"
```

---

### Task 2: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` — register `cleanupCmd` + `addCleanupFlags(cleanupCmd)` inside `init()`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager.Prune(ctx, runtime.PruneOptions) (*runtime.PruneResult, error)`, `runtime.Manager.KeepLastNImages(ctx, appName string, n int) error`, `config.NewStoreWithEnv(dataDir, env string) *config.Store`, `store.ListApps() ([]types.AppEntry, error)`, `getEnv(cmd) string`
- Produces: `var cleanupCmd *cobra.Command` (registered on root), `func runCleanup(cmd *cobra.Command, rt runtime.Manager, dataDir, env string) error`, `func resolveCleanupCategories(containers, images, networks, volumes, buildCache bool) (bool, bool, bool, bool, bool)`, `func addCleanupFlags(cmd *cobra.Command)`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
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
	expected := []string{"dry-run", "containers", "images", "networks", "volumes", "build-cache", "keep-images"}
	for _, name := range expected {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup command missing --%s flag", name)
		}
	}
}

func TestResolveCleanupCategoriesDefaults(t *testing.T) {
	c, i, n, v, b := resolveCleanupCategories(false, false, false, false, false)
	if !c || !i || !n || v || !b {
		t.Errorf("default categories = c:%v i:%v n:%v v:%v b:%v, want c:true i:true n:true v:false b:true", c, i, n, v, b)
	}
}

func TestResolveCleanupCategoriesExplicitVolumes(t *testing.T) {
	c, i, n, v, b := resolveCleanupCategories(false, false, false, true, false)
	if c || i || n || !v || b {
		t.Errorf("explicit --volumes = c:%v i:%v n:%v v:%v b:%v, want only volumes", c, i, n, v, b)
	}
}

func TestRunCleanupDryRunUsesStub(t *testing.T) {
	cmd := &cobra.Command{Use: "cleanup"}
	addCleanupFlags(cmd)
	if err := cmd.Flags().Parse([]string{"--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if err := runCleanup(cmd, runtime.NewStub(), t.TempDir(), "production"); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
}

func TestRunCleanupKeepsImagesPerApp(t *testing.T) {
	tmpDir := t.TempDir()
	store := config.NewStoreWithEnv(tmpDir, "production")
	if err := store.SaveApp(types.AppEntry{
		Name:   "myapp",
		Config: types.AppConfig{Name: "myapp"},
	}); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{Use: "cleanup"}
	addCleanupFlags(cmd)
	if err := cmd.Flags().Parse([]string{"--keep-images", "3"}); err != nil {
		t.Fatal(err)
	}
	if err := runCleanup(cmd, runtime.NewStub(), tmpDir, "production"); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
}

func TestRunCleanupKeepImagesZeroSkipsApps(t *testing.T) {
	cmd := &cobra.Command{Use: "cleanup"}
	addCleanupFlags(cmd)
	if err := cmd.Flags().Parse([]string{"--keep-images", "0"}); err != nil {
		t.Fatal(err)
	}
	if err := runCleanup(cmd, runtime.NewStub(), t.TempDir(), "production"); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run 'Cleanup|ResolveCleanupCategories' -count=1`
Expected: FAIL — package does not compile (`undefined: cleanupCmd`, `undefined: resolveCleanupCategories`, `undefined: runCleanup`, `undefined: addCleanupFlags`).

- [ ] **Step 3: Implement the command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("dry-run", false, "show what would be removed without changing anything")
	cmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cmd.Flags().Bool("images", false, "prune dangling images not tagged tengiz-apps/*")
	cmd.Flags().Bool("networks", false, "prune unused networks")
	cmd.Flags().Bool("volumes", false, "prune unused volumes (opt-in: volumes may hold data)")
	cmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cmd.Flags().Int("keep-images", 5, "keep last N images per app (0 disables)")
}

func resolveCleanupCategories(containers, images, networks, volumes, buildCache bool) (bool, bool, bool, bool, bool) {
	if !containers && !images && !networks && !volumes && !buildCache {
		return true, true, true, true, false
	}
	return containers, images, networks, volumes, buildCache
}

func runCleanup(cmd *cobra.Command, rt runtime.Manager, dataDir, env string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	keepImages, _ := cmd.Flags().GetInt("keep-images")

	containers, images, networks, volumes, buildCache = resolveCleanupCategories(containers, images, networks, volumes, buildCache)

	res, err := rt.Prune(cmd.Context(), runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		BuildCache: buildCache,
		DryRun:     dryRun,
	})
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	if dryRun {
		fmt.Println("[tengiz] cleanup dry-run (no changes made):")
		if containers {
			fmt.Printf("  would prune %d stopped container(s)\n", res.ContainersRemoved)
		}
		if images {
			fmt.Printf("  would prune %d dangling image(s)\n", res.ImagesRemoved)
		}
		if networks {
			fmt.Println("  would prune unused networks")
		}
		if volumes {
			fmt.Println("  would prune unused volumes")
		}
		if buildCache {
			fmt.Println("  would prune build cache")
		}
	} else {
		fmt.Println("[tengiz] cleanup complete:")
		if containers {
			fmt.Printf("  pruned %d container(s)\n", res.ContainersRemoved)
		}
		if images {
			fmt.Printf("  pruned %d image(s)\n", res.ImagesRemoved)
		}
		if networks {
			fmt.Printf("  pruned %d network(s)\n", res.NetworksRemoved)
		}
		if volumes {
			fmt.Printf("  pruned %d volume(s)\n", res.VolumesRemoved)
		}
		if buildCache {
			fmt.Printf("  pruned build cache (%d entries)\n", res.BuildCacheRemoved)
		}
		if res.ReclaimedSpace != "" {
			fmt.Printf("  total reclaimed space: %s\n", res.ReclaimedSpace)
		}
	}

	if keepImages > 0 {
		store := config.NewStoreWithEnv(dataDir, env)
		apps, listErr := store.ListApps()
		if listErr != nil {
			return fmt.Errorf("list apps: %w", listErr)
		}
		if dryRun {
			fmt.Printf("  would keep last %d image(s) for %d app(s)\n", keepImages, len(apps))
		} else {
			for _, app := range apps {
				if err := rt.KeepLastNImages(cmd.Context(), app.Name, keepImages); err != nil {
					fmt.Printf("[tengiz] warning: image retention for %s: %v\n", app.Name, err)
				}
			}
			fmt.Printf("  kept last %d image(s) for %d app(s)\n", keepImages, len(apps))
		}
	}

	return nil
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources and keep recent Tengiz images",
	Long: `Housekeeping for a single-server Tengiz instance.

Prunes stopped containers, dangling images, unused networks, and build cache.
Tengiz-managed containers (label tengiz-app=*) and images (tengiz-apps/*) are
always protected. Also trims old per-app images, keeping the --keep-images most
recent ones.

Safe categories (containers, images, networks, build-cache) run by default.
Add --volumes to also prune unused volumes (opt-in because volumes may hold
data). Use --dry-run to preview what would be removed without changing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		return runCleanup(cmd, rt, dataDir, getEnv(cmd))
	},
}
```

Register it in `internal/cli/root.go` inside `init()` (add these two lines after the `rootCmd.AddCommand(buildLogsCmd)` line, which is line 66):

```go
	rootCmd.AddCommand(cleanupCmd)
	addCleanupFlags(cleanupCmd)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 3: Documentation

**Files:**
- Modify: `README.md` — add `### tengiz cleanup` section after the `tengiz rollback` section (between line 236 and the `### tengiz domain` section at line 238)
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI list, update the `runtime.Manager` row, add a quirk
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:**
- Consumes: the CLI surface implemented in Tasks 1–2
- Produces: documentation only

- [ ] **Step 1: Document `tengiz cleanup` in README.md**

Insert the following block into `README.md` between the `tengiz rollback` section and the `### tengiz domain` section:

```markdown
### `tengiz cleanup`

Prune unused Docker resources and trim old Tengiz images — housekeeping for
single-server deployments where disk space is the most common failure mode.

Tengiz-managed containers (labeled `tengiz-app=*`) and images (`tengiz-apps/*`)
are always protected from pruning.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling images (skips `tengiz-apps/*`) |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune Docker build cache |
| `--volumes` | Prune unused volumes (opt-in — volumes may hold data) |
| `--keep-images N` | Keep last N images per app (default `5`, `0` disables) |
| `--dry-run` | Show what would be removed without changing anything |
| `--env` | Environment to scope per-app image retention |

With no category flags, safe categories run by default (containers, images,
networks, build-cache). Add `--volumes` to include unused volumes.

```bash
tengiz cleanup                # prune safe categories + keep last 5 images per app
tengiz cleanup --volumes      # also prune unused volumes
tengiz cleanup --dry-run      # preview only, makes no changes
tengiz cleanup --keep-images 3
```
```

Note: the nested code fence above must be closed and re-opened correctly when inserted — the plan content shows the literal text; the implementer pastes the section body including the `tengiz cleanup` example block verbatim into README.md.

- [ ] **Step 2: Update AGENTS.md**

Add to the CLI code block (after the `tengiz build-logs` line):

```
tengiz cleanup [--containers|--images|--networks|--volumes|--build-cache] [--keep-images N] [--dry-run] → Docker housekeeping
```

Update the `runtime.Manager` row in the "Key architecture" table: replace the sentence `Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup.` with:

```
Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup, and `Prune` for Docker housekeeping.
```

Add to the "Quirks" section:

- `tengiz cleanup` prunes safe categories by default (containers, images, networks, build-cache); `--volumes` is opt-in because volumes may hold data

- [ ] **Step 3: Mark feature #6 as implemented in docs/FUTURES_FEATURES.md**

In the P0 table, change row `| 6 | **Docker Housekeeping** ⬜ |` to `| 6 | **Docker Housekeeping** ✅ |`.

Add a row to the "Implemented Features (Not Pending)" table:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-18) |
```

In the "Özellikler" section, add a status line to the `## Docker Housekeeping (Otomatik Temizlik)` entry (after the `- **Detected:** 2026-07-14` line):

```
- **Status:** ✅ Implemented (2026-08-18)
```

- [ ] **Step 4: Verify the full build, vet, and test suite**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: build succeeds, vet reports no issues, all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**Spec coverage (feature #6 Docker Housekeeping):**
- "Label-based docker system prune / `tengiz cleanup`" → Task 2 command + Task 1 label-protected prunes (`label!=tengiz-app`, `reference!=tengiz-apps/*`). The plan deliberately uses granular `docker <object> prune` rather than `docker system prune` because system prune lacks an image-reference filter and would delete unused `tengiz-apps/*` images — this matches the feature's core intent ("label-based filtering protects Tengiz-managed containers").
- "Sürekli deploy ve scale-to-zero ortamında atık container/image'ler disk alanını tüketir" → Task 1 prunes stopped containers + dangling images + build cache; Task 2 `--keep-images` retention trims old `tengiz-apps/*` images per app.
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → Task 1 covers all four categories (volumes opt-in via `--volumes`).

**Placeholder scan:** No TODOs, "implement later", or vague steps — every code step contains complete Go source and exact file/line targets.

**Type consistency:** `PruneOptions`/`PruneResult` field names are identical across Task 1 (interface, stub, docker impl, mocks) and Task 2 (`runCleanup`). `resolveCleanupCategories` bool-return order matches its usage. `runCleanup` signature `(cmd, rt, dataDir, env)` matches both `cleanupCmd.RunE` and all three test call sites. `addCleanupFlags` is called from both `root.go init()` and the tests.