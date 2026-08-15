# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused volumes/networks, build cache) while always protecting Tengiz-managed containers and retaining rollback images.

**Architecture:** The `runtime.Manager` interface gains a `Prune(ctx, opts) (*PruneResult, error)` method. A pure, table-tested function `buildPruneCommands(opts)` maps a set of `PruneKind` categories to `docker <object> prune` CLI invocations using `--filter label!=tengiz-app` so Tengiz-labeled containers are never touched. `(*dockerRuntime).Prune` executes each command via `os/exec` (consistent with the rest of the package — no Docker SDK) and accumulates reclaimed bytes by parsing the `Total reclaimed space:` line. The CLI wires this to a new `cleanupCmd` with per-category flags, `--all`, `--dry-run`, and `--keep-images N` (retains the last N images per app via the existing `KeepLastNImages`).

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface + `dockerRuntime` exec impl, existing `config.Store` for env-scoped app listing.

## Global Constraints

- Feature spec (from `docs/FUTURES_FEATURES.md`, feature #6): "Label-based `docker system prune`. `tengiz cleanup`." and "kullanılmayan volume, network, container ve image'leri periyodik temizleme" (unused volumes, networks, containers, images)
- Must work via the `docker` CLI (`os/exec`), never the Docker SDK — no new external Go dependencies
- **Protection rule:** every prune filter must exclude objects labeled `tengiz-app` (label filter `label!=tengiz-app`); the label is `tengiz-app` from `internal/runtime/docker.go:76` (`labelKey`)
- Default `tengiz cleanup` (no category flag) prunes all five categories: containers, images, volumes, networks, cache
- `--dry-run` must remove NOTHING — no docker prune execution and no `KeepLastNImages` calls
- `--keep-images` defaults to `5`, matching the deploy-time retention calls in `internal/cli/root.go:346` and `root.go:466`
- `--env` must scope the app list used for image retention (via `config.NewStoreWithEnv`)
- Adding `Prune` to the `Manager` interface requires updating ALL types assigned to `runtime.Manager`: `dockerRuntime`, `stubManager`, `mockRTForDeploy` (`internal/cli/root_test.go`), `mockRuntime` (`internal/idle/idle_test.go`), `mockRuntime` (`internal/proxy/proxy_test.go`) — otherwise the packages won't compile
- New feature requires a feature branch per AGENTS.md Rules: `git checkout -b feat/docker-housekeeping`
- README.md and AGENTS.md and `docs/FUTURES_FEATURES.md` must be updated per AGENTS.md Rules
- Existing tests must continue to pass without modification (except adding the new `Prune` mock methods, which are required for compilation)
- No changes to `docs/superpowers/plans/*` other than this file

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneKind` constants, `PruneOptions`, `PruneResult`; add `Prune` to `Manager` interface; add stub implementation |
| `internal/runtime/prune.go` | NEW: `buildPruneCommands()`, `parseReclaimed()`, `(*dockerRuntime).Prune()` |
| `internal/runtime/prune_test.go` | NEW: table tests for `buildPruneCommands` and `parseReclaimed` |
| `internal/runtime/cleanup_test.go` | Add `TestStubPrune` |
| `internal/cli/root_test.go` | Add `Prune` method to `mockRTForDeploy` |
| `internal/idle/idle_test.go` | Add `Prune` method to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Prune` method to `mockRuntime` |
| `internal/cli/cleanup.go` | NEW: `cleanupCmd` + `cleanupKinds` + `humanBytes` + `printCleanupSummary` + `init()` registration |
| `internal/cli/cleanup_test.go` | NEW: CLI tests (registration, flags, `cleanupKinds`, `humanBytes`) |
| `README.md` | Add `tengiz cleanup` section to CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` line to CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as implemented |

---

### Task 1: Add `Prune` to the runtime Manager interface

**Files:**
- Modify: `internal/runtime/runtime.go` — add types, interface method, stub
- Modify: `internal/runtime/cleanup_test.go:1-20` — add `TestStubPrune`
- Modify: `internal/cli/root_test.go:69-100` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:14-34` — add `Prune` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:15-35` — add `Prune` to `mockRuntime`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type PruneKind string` with constants `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneCache` (values `"containers"`, `"images"`, `"volumes"`, `"networks"`, `"cache"`)
  - `type PruneOptions struct { Kinds []PruneKind; DryRun bool }` — empty `Kinds` means all categories
  - `type PruneResult struct { DryRun bool; ReclaimedBytes int64; Commands [][]string; Errors []string }`
  - `Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)` added to the interface

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

Expected: branch `feat/docker-housekeeping` created from the current branch.

- [ ] **Step 2: Write the failing stub test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Kinds: []PruneKind{PruneImages}, DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil {
		t.Fatal("Prune() returned nil result")
	}
	if !res.DryRun {
		t.Errorf("Prune() result DryRun = false, want true")
	}
}
```

The file already imports `context` and `testing` (see `internal/runtime/cleanup_test.go:3-6`).

- [ ] **Step 3: Run test to verify it fails to compile**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: FAIL — compile error `undefined: PruneOptions` (and `Prune` not implemented by `stubManager`).

- [ ] **Step 4: Add types and interface method to `internal/runtime/runtime.go`**

Add these type definitions before the `Manager` interface (after `RunOptions`, around `runtime.go:30`):

```go
type PruneKind string

const (
	PruneContainers PruneKind = "containers"
	PruneImages     PruneKind = "images"
	PruneVolumes    PruneKind = "volumes"
	PruneNetworks   PruneKind = "networks"
	PruneCache      PruneKind = "cache"
)

type PruneOptions struct {
	Kinds  []PruneKind
	DryRun bool
}

type PruneResult struct {
	DryRun         bool
	ReclaimedBytes int64
	Commands       [][]string
	Errors         []string
}
```

Add `Prune` to the `Manager` interface (after `KeepLastNImages`, `runtime.go:36`):

```go
	Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)
```

- [ ] **Step 5: Add stub implementation to `internal/runtime/runtime.go`**

Append after the `KeepLastNImages` stub (`runtime.go:117-119`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	return &PruneResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 6: Update the three mock implementations so packages still compile**

`internal/cli/root_test.go` — add after the `KeepLastNImages` mock (`root_test.go:99`):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) {
	return &runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

`internal/idle/idle_test.go` — add after the `KeepLastNImages` mock (`idle_test.go:33`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) {
	return &runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

`internal/proxy/proxy_test.go` — add after the `KeepLastNImages` mock (`proxy_test.go:34`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) {
	return &runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: PASS

- [ ] **Step 8: Build and vet**

Run: `go build ./... && go vet ./...`

Expected: No output, exit 0 (all mock updates compile).

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Prune to runtime.Manager interface with stub"
```

---

### Task 2: Implement `dockerRuntime.Prune` with testable command builders

**Files:**
- Create: `internal/runtime/prune.go`
- Create: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneResult`, `PruneKind` from Task 1
- Produces:
  - `buildPruneCommands(opts PruneOptions) [][]string` — one docker arg-slice per prune invocation
  - `parseReclaimed(out string) int64` — bytes reclaimed from prune output
  - `(*dockerRuntime).Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildPruneCommandsAll(t *testing.T) {
	got := buildPruneCommands(PruneOptions{})
	want := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-f"},
		{"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"builder", "prune", "-f"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneCommands() = %v, want %v", got, want)
	}
}

func TestBuildPruneCommandsSubset(t *testing.T) {
	got := buildPruneCommands(PruneOptions{Kinds: []PruneKind{PruneImages, PruneCache}})
	want := [][]string{
		{"image", "prune", "-f"},
		{"builder", "prune", "-f"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneCommands() = %v, want %v", got, want)
	}
}

func TestBuildPruneCommandsPreservesOrder(t *testing.T) {
	got := buildPruneCommands(PruneOptions{Kinds: []PruneKind{PruneCache, PruneContainers}})
	want := [][]string{
		{"builder", "prune", "-f"},
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneCommands() = %v, want %v", got, want)
	}
}

func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"Total reclaimed space: 0B", 0},
		{"Total reclaimed space: 512B", 512},
		{"Total reclaimed space: 1.5kB", 1536},
		{"Total reclaimed space: 2MB", 2 * 1024 * 1024},
		{"Total reclaimed space: 3GB", 3 * 1024 * 1024 * 1024},
		{"no summary line here", 0},
	}
	for _, tt := range tests {
		if got := parseReclaimed(tt.in); got != tt.want {
			t.Errorf("parseReclaimed(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail to compile**

Run: `go test ./internal/runtime/... -run "TestBuildPruneCommands|TestParseReclaimed" -v -count=1`

Expected: FAIL — compile error `undefined: buildPruneCommands`, `undefined: parseReclaimed`.

- [ ] **Step 3: Write the minimal implementation**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var reclaimedSpaceRe = regexp.MustCompile(`Total reclaimed space: ([0-9.]+)\s*([a-zA-Z]*B)`)

func buildPruneCommands(opts PruneOptions) [][]string {
	kinds := opts.Kinds
	if len(kinds) == 0 {
		kinds = []PruneKind{PruneContainers, PruneImages, PruneVolumes, PruneNetworks, PruneCache}
	}
	var cmds [][]string
	for _, k := range kinds {
		switch k {
		case PruneContainers:
			cmds = append(cmds, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"})
		case PruneImages:
			cmds = append(cmds, []string{"image", "prune", "-f"})
		case PruneVolumes:
			cmds = append(cmds, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
		case PruneNetworks:
			cmds = append(cmds, []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"})
		case PruneCache:
			cmds = append(cmds, []string{"builder", "prune", "-f"})
		}
	}
	return cmds
}

func parseReclaimed(out string) int64 {
	m := reclaimedSpaceRe.FindStringSubmatch(out)
	if len(m) != 3 {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(m[2]) {
	case "b":
		return int64(val)
	case "kb":
		return int64(val * 1024)
	case "mb":
		return int64(val * 1024 * 1024)
	case "gb":
		return int64(val * 1024 * 1024 * 1024)
	}
	return 0
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	cmds := buildPruneCommands(opts)
	res := &PruneResult{
		DryRun:   opts.DryRun,
		Commands: cmds,
	}
	for _, args := range cmds {
		if opts.DryRun {
			continue
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("docker %s: %v", strings.Join(args, " "), err))
			continue
		}
		res.ReclaimedBytes += parseReclaimed(string(out))
	}
	return res, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestBuildPruneCommands|TestParseReclaimed|TestStubPrune" -v -count=1`

Expected: PASS (5 test functions)

- [ ] **Step 5: Run the full runtime test suite**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: implement label-protected docker prune in runtime"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.PruneKind`, `runtime.Manager.Prune` from Tasks 1-2; `config.NewStoreWithEnv`; global `dataDir`; `getEnv(cmd)` from `internal/cli/root.go:97`
- Produces:
  - `cleanupCmd` — a `*cobra.Command` named `cleanup`, registered on `rootCmd` via `init()`
  - `cleanupKinds(all, containers, images, volumes, networks, cache bool) []runtime.PruneKind`
  - `humanBytes(b int64) string`
  - `printCleanupSummary(res *runtime.PruneResult)`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"reflect"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

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
	flags := cleanupCmd.Flags()
	for _, name := range []string{"containers", "images", "volumes", "networks", "cache", "all", "dry-run", "keep-images"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupKinds(t *testing.T) {
	tests := []struct {
		name                              string
		all, containers, images, volumes, networks, cache bool
		want                              []runtime.PruneKind
	}{
		{
			name: "all flag",
			all:  true,
			want: []runtime.PruneKind{
				runtime.PruneContainers, runtime.PruneImages,
				runtime.PruneVolumes, runtime.PruneNetworks, runtime.PruneCache,
			},
		},
		{
			name: "no flags defaults to all",
			want: []runtime.PruneKind{
				runtime.PruneContainers, runtime.PruneImages,
				runtime.PruneVolumes, runtime.PruneNetworks, runtime.PruneCache,
			},
		},
		{
			name:        "single category",
			containers:  true,
			want:        []runtime.PruneKind{runtime.PruneContainers},
		},
		{
			name:   "multiple categories preserve flag order",
			images: true,
			cache:  true,
			want:   []runtime.PruneKind{runtime.PruneImages, runtime.PruneCache},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanupKinds(tt.all, tt.containers, tt.images, tt.volumes, tt.networks, tt.cache)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("cleanupKinds() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{2 * 1024 * 1024, "2.00 MB"},
		{3 * 1024 * 1024 * 1024, "3.00 GB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.in); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail to compile**

Run: `go test ./internal/cli/... -run "TestCleanup|TestHumanBytes" -v -count=1`

Expected: FAIL — compile error `undefined: cleanupCmd`, `undefined: cleanupKinds`, `undefined: humanBytes`.

- [ ] **Step 3: Write the minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Removes unused Docker resources on this host: stopped non-Tengiz containers, dangling images, unused volumes and networks, and the Docker BuildKit build cache.

Tengiz-managed containers (labeled tengiz-app=*) are always protected. The last --keep-images images of each app are retained for rollback.

Run with --dry-run to preview what would be removed without removing anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		all, _ := cmd.Flags().GetBool("all")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		keepImages, _ := cmd.Flags().GetInt("keep-images")

		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")

		kinds := cleanupKinds(all, containers, images, volumes, networks, cache)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)

		if dryRun {
			fmt.Printf("[tengiz] dry-run: %d resource categories would be cleaned\n", len(kinds))
			fmt.Printf("[tengiz] dry-run: would keep last %d image(s) per app for rollback\n", keepImages)
		} else {
			apps, _ := store.ListApps()
			for _, app := range apps {
				if err := rt.KeepLastNImages(cmd.Context(), app.Name, keepImages); err != nil {
					log.Printf("[tengiz] warning: image retention for %s: %v", app.Name, err)
				}
			}
		}

		res, err := rt.Prune(cmd.Context(), runtime.PruneOptions{Kinds: kinds, DryRun: dryRun})
		if err != nil {
			return fmt.Errorf("prune: %w", err)
		}

		printCleanupSummary(res)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling (unreferenced) images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes not used by Tengiz apps")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks not used by Tengiz apps")
	cleanupCmd.Flags().Bool("cache", false, "prune the Docker BuildKit build cache")
	cleanupCmd.Flags().Bool("all", false, "prune all categories (default when no category flag is given)")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Int("keep-images", 5, "number of images to keep per app for rollback")
	rootCmd.AddCommand(cleanupCmd)
}

func cleanupKinds(all, containers, images, volumes, networks, cache bool) []runtime.PruneKind {
	allKinds := []runtime.PruneKind{
		runtime.PruneContainers, runtime.PruneImages,
		runtime.PruneVolumes, runtime.PruneNetworks, runtime.PruneCache,
	}
	if all || !(containers || images || volumes || networks || cache) {
		return allKinds
	}
	var kinds []runtime.PruneKind
	if containers {
		kinds = append(kinds, runtime.PruneContainers)
	}
	if images {
		kinds = append(kinds, runtime.PruneImages)
	}
	if volumes {
		kinds = append(kinds, runtime.PruneVolumes)
	}
	if networks {
		kinds = append(kinds, runtime.PruneNetworks)
	}
	if cache {
		kinds = append(kinds, runtime.PruneCache)
	}
	return kinds
}

func printCleanupSummary(res *runtime.PruneResult) {
	if res.DryRun {
		fmt.Println("[tengiz] dry-run: would run these docker prune commands:")
		for _, c := range res.Commands {
			fmt.Printf("  docker %s\n", strings.Join(c, " "))
		}
		return
	}
	fmt.Printf("[tengiz] cleanup complete: reclaimed %s\n", humanBytes(res.ReclaimedBytes))
	for _, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "[tengiz] warning: %s\n", e)
	}
}

func humanBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestHumanBytes" -v -count=1`

Expected: PASS (4 test functions, `TestCleanupKinds` runs 4 subtests)

- [ ] **Step 5: Build, vet, and run the full CLI suite**

Run: `go build ./... && go vet ./... && go test ./internal/cli/... -count=1`

Expected: Build succeeds, vet clean, all CLI tests PASS.

- [ ] **Step 6: Manually verify flag registration**

Run: `go run . cleanup --help`

Expected: help text lists `--containers`, `--images`, `--volumes`, `--networks`, `--cache`, `--all`, `--dry-run`, `--keep-images`, plus the global `--env`.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Update documentation and final verification

**Files:**
- Modify: `README.md` — add `tengiz cleanup` to CLI Reference (after the `### tengiz rollback <app>` section, around line 236)
- Modify: `AGENTS.md` — add `tengiz cleanup` line to the CLI command list
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:**
- Consumes: `cleanupCmd`, flags `--containers/--images/--volumes/--networks/--cache/--all/--dry-run/--keep-images` from Task 3
- Produces: updated user + agent documentation

- [ ] **Step 1: Add the CLI Reference section to `README.md`**

Insert after the `### tengiz rollback <app>` section (ends at the blank line after the "| `app` | Application name (required) |" row for rollback, `README.md:236`):

````markdown
### `tengiz cleanup`

Clean up unused Docker resources on the host to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling (unreferenced) images |
| `--volumes` | Prune unused volumes not used by Tengiz apps |
| `--networks` | Prune unused networks not used by Tengiz apps |
| `--cache` | Prune the Docker BuildKit build cache |
| `--all` | Prune all categories (default when no category flag is given) |
| `--dry-run` | Show what would be removed without removing anything |
| `--keep-images N` | Keep the last N images per app for rollback (default: 5) |

Tengiz-managed containers (labeled `tengiz-app=*`) are always protected, and the last `--keep-images` images of each app are retained for rollback. Examples:

```bash
tengiz cleanup --dry-run          # preview what would be removed
tengiz cleanup                    # full cleanup (all categories)
tengiz cleanup --cache --images   # only build cache + dangling images
```
````

- [ ] **Step 2: Add `tengiz cleanup` to the `AGENTS.md` CLI list**

Insert after the `tengiz rollback <app>` line in the CLI command list:

```markdown
tengiz cleanup [--all|--containers|--images|--volumes|--networks|--cache] [--dry-run] [--keep-images N]  → prune unused Docker resources (label-protected, keeps N images/app for rollback)
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

Change the feature #6 row in the P0 Priority Ranking table (line 19):

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the `✅ Implemented Features (Not Pending)` table (after the Webhook row, around line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-15) |
```

Add a status line to the `## Docker Housekeeping (Otomatik Temizlik)` detail section (before the `- **Detected:** 2026-07-14` line):

```markdown
- **Status:** ✅ Implemented (2026-08-15)
```

- [ ] **Step 4: Run the full test suite and static analysis**

Run: `go build ./... && go vet ./... && go test ./... -v -count=1`

Expected: Build succeeds, vet clean, all tests PASS. (Note: proxy TCP-dial tests and idle time-sensitive tests may be slow but must pass; any pre-existing failures are out of scope.)

- [ ] **Step 5: Self-review against the spec**

Check against the feature #6 requirements from `docs/FUTURES_FEATURES.md`:
- `tengiz cleanup` command ✅ (Task 3 — `cleanupCmd`)
- Label-based protection of Tengiz containers ✅ (Task 2 — `--filter label!=tengiz-app` on container/volume/network prunes)
- Cleans unused volumes, networks, containers, images ✅ (Task 2 — all five `PruneKind` categories, build cache included)
- Rollback images retained ✅ (Task 3 — `KeepLastNImages` per store app, default 5)
- `--dry-run` removes nothing ✅ (Task 2 dry-run skips exec; Task 3 skips `KeepLastNImages`)
- Works with `--env` ✅ (Task 3 — `config.NewStoreWithEnv(dataDir, env)` + `getEnv(cmd)`)

- [ ] **Step 6: Placeholder scan**

Search the plan for `TBD`, `TODO`, `implement later`, `fill in details`, `Similar to Task`. None present — every code step contains complete code and exact commands.

- [ ] **Step 7: Type consistency check**

- `runtime.PruneKind` constants (`PruneContainers` etc.) used identically in Tasks 2 and 3
- `runtime.PruneOptions{Kinds: kinds, DryRun: dryRun}` — `kinds` is `[]runtime.PruneKind`, `dryRun` is `bool`, matching the Task 1 struct definition
- `runtime.PruneResult{DryRun, ReclaimedBytes, Commands, Errors}` — field names match between `dockerRuntime.Prune`, `printCleanupSummary`, and the stubs
- `cleanupKinds(all, containers, images, volumes, networks, cache bool) []runtime.PruneKind` — same signature in the command handler and the test
- `humanBytes(int64) string` — same signature in `printCleanupSummary` and the test

- [ ] **Step 8: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---