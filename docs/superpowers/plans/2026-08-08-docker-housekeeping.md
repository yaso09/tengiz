# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup`, a label-protected Docker housekeeping command that prunes unused containers/images/volumes/networks/build cache to reclaim disk space on single-server deployments.

**Architecture:** The `runtime` package owns all Docker CLI interaction. It gains two new `Manager` methods — `Cleanup(ctx, opts)` and `SystemDF(ctx)` — plus pure, unit-testable command-builder functions (`containerPruneArgs()`, `imagePruneArgs()`, etc.). Containers carrying the `tengiz-app` label are never pruned. A new `tengiz cleanup` Cobra command maps flags to a `CleanupOptions` struct, enforces that at least one scope is selected, runs the prunes, performs per-app image retention via the existing `KeepLastNImages`, and prints a summary plus `docker system df`.

**Tech Stack:** Go 1.26, `os/exec` (no Docker SDK — shells out to `docker` CLI), `spf13/cobra`, existing `config.Store` and `runtime.Manager` interfaces.

## Global Constraints

- All Docker calls shell out via `os/exec` — no Docker SDK, no new dependencies
- Container pruning MUST exclude Tengiz-managed containers `--filter "label!=tengiz-app"` (label constant `labelKey` reused from `internal/runtime/docker.go`)
- Image repository format stays `tengiz-apps/<app>:<env>-<deploymentID>` — this plan never changes it
- Existing per-app retention is 5 images (`KeepLastNImages(ctx, appName, 5)`) — `--keep` defaults to 5, validated `>= 1`
- Adding methods to `runtime.Manager` requires updating every implementer: `stubManager` (`internal/runtime/runtime.go`), `mockRTForDeploy` (`internal/cli/root_test.go`), `mockRuntime` (`internal/proxy/proxy_test.go`), `mockRuntime` (`internal/idle/idle_test.go`)
- `--env` is inherited from the root persistent flag — do NOT define a local `--env` on `cleanupCmd`
- `--dry-run` never deletes: it lists candidates via read-only `docker` commands
- The default `--all` must behave as `--containers --images --volumes --networks --build-cache`
- Running `tengiz cleanup` with no scope flag MUST return an error before touching Docker

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` | NEW — `CleanupOptions`, `CleanupResult`, pure command-builder helpers, `parsePruneOutput`, `splitLines`, `filterBuiltinNetworks`, `runPruneOrList`, `dockerRuntime.Cleanup`, `dockerRuntime.SystemDF` |
| `internal/runtime/prune_test.go` | NEW — unit tests for all pure helpers + stub Cleanup/SystemDF |
| `internal/runtime/runtime.go` | MODIFY — add `Cleanup` + `SystemDF` to `Manager` interface; add stub implementations |
| `internal/cli/cleanup.go` | NEW — `cleanupCmd` cobra command, flag parsing, summary printing |
| `internal/cli/cleanup_test.go` | NEW — registration, flags, scope-validation tests |
| `internal/cli/root.go` | MODIFY — `rootCmd.AddCommand(cleanupCmd)` + flag definitions in `init()` |
| `internal/proxy/proxy_test.go` | MODIFY — `mockRuntime` gains `Cleanup` + `SystemDF` |
| `internal/idle/idle_test.go` | MODIFY — `mockRuntime` gains `Cleanup` + `SystemDF` |
| `internal/cli/root_test.go` | MODIFY — `mockRTForDeploy` gains `Cleanup` + `SystemDF` |
| `README.md` | MODIFY — document `tengiz cleanup` under CLI Reference; add to Features |
| `docs/FUTURES_FEATURES.md` | MODIFY — flip #6 to ✅ Implemented |

---

### Task 1: Purge helpers + options types in `runtime` (TDD)

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: existing `labelKey` const (`internal/runtime/docker.go:76`), `exec.CommandContext` pattern from `internal/runtime/docker.go`
- Produces: `type CleanupOptions struct { Containers, Images, Volumes, Networks, BuildCache, DryRun bool }`, `type CleanupResult struct { DryRun bool; Containers, Images, Volumes, Networks, BuildCache []string }`, pure funcs `containerPruneArgs() []string`, `imagePruneArgs() []string`, `volumePruneArgs() []string`, `networkPruneArgs() []string`, `buildCachePruneArgs() []string`, `containerPruneListArgs() []string`, `danglingImageListArgs() []string`, `danglingVolumeListArgs() []string`, `networkListArgs() []string`, `systemDFArgs() []string`, `buildCacheUsageArgs() []string`, `parsePruneOutput(string) []string`, `splitLines(string) []string`, `filterBuiltinNetworks([]string) []string`, `runCommandOutput(context.Context, []string) (string, error)`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-cleanup
```

- [ ] **Step 2: Write the failing tests (builder args — first half)**

```go
// internal/runtime/prune_test.go
package runtime

import (
	"reflect"
	"testing"
)

func TestContainerPruneArgs(t *testing.T) {
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if got := containerPruneArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("containerPruneArgs() = %v, want %v", got, want)
	}
}

func TestContainerPruneListArgs(t *testing.T) {
	want := []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.Names}}"}
	if got := containerPruneListArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("containerPruneListArgs() = %v, want %v", got, want)
	}
}

func TestImagePruneArgs(t *testing.T) {
	want := []string{"image", "prune", "-f"}
	if got := imagePruneArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("imagePruneArgs() = %v, want %v", got, want)
	}
}

func TestDanglingImageListArgs(t *testing.T) {
	want := []string{"images", "-q", "--filter", "dangling=true"}
	if got := danglingImageListArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("danglingImageListArgs() = %v, want %v", got, want)
	}
}

func TestVolumePruneArgs(t *testing.T) {
	want := []string{"volume", "prune", "-f"}
	if got := volumePruneArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("volumePruneArgs() = %v, want %v", got, want)
	}
}

func TestDanglingVolumeListArgs(t *testing.T) {
	want := []string{"volume", "ls", "-q", "--filter", "dangling=true"}
	if got := danglingVolumeListArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("danglingVolumeListArgs() = %v, want %v", got, want)
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	want := []string{"network", "prune", "-f"}
	if got := networkPruneArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("networkPruneArgs() = %v, want %v", got, want)
	}
}

func TestNetworkListArgs(t *testing.T) {
	want := []string{"network", "ls", "--format", "{{.Name}}"}
	if got := networkListArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("networkListArgs() = %v, want %v", got, want)
	}
}

func TestBuildCachePruneArgs(t *testing.T) {
	want := []string{"builder", "prune", "-f"}
	if got := buildCachePruneArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("buildCachePruneArgs() = %v, want %v", got, want)
	}
}

func TestSystemDFArgs(t *testing.T) {
	want := []string{"system", "df"}
	if got := systemDFArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("systemDFArgs() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestContainerPruneArgs|TestImagePruneArgs|TestVolumePruneArgs|TestNetworkPruneArgs|TestBuildCachePruneArgs|TestSystemDFArgs|TestDanglingImageListArgs|TestDanglingVolumeListArgs|TestNetworkListArgs' -v -count=1`

Expected: FAIL with `undefined: containerPruneArgs` etc.

- [ ] **Step 4: Implement the builder functions + types in `internal/runtime/prune.go`**

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CleanupOptions controls which resource categories `tengiz cleanup` prunes.
type CleanupOptions struct {
	Containers bool // prune stopped containers NOT managed by Tengiz
	Images     bool // prune dangling images
	Volumes    bool // prune volumes not used by any container
	Networks   bool // prune networks not used by any container
	BuildCache bool // prune the Docker build cache
	DryRun     bool // only report, never delete
}

// CleanupResult reports per-category artifacts removed (or that would be
// removed, when DryRun is true).
type CleanupResult struct {
	DryRun     bool
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
	BuildCache []string
}

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey)}
}

func containerPruneListArgs() []string {
	return []string{"ps", "-a", "--filter", "status=exited", "--filter", fmt.Sprintf("label!=%s", labelKey), "--format", "{{.Names}}"}
}

func imagePruneArgs() []string {
	return []string{"image", "prune", "-f"}
}

func danglingImageListArgs() []string {
	return []string{"images", "-q", "--filter", "dangling=true"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func danglingVolumeListArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func networkListArgs() []string {
	return []string{"network", "ls", "--format", "{{.Name}}"}
}

func buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func buildCacheUsageArgs() []string {
	return []string{"builder", "du"}
}

func systemDFArgs() []string {
	return []string{"system", "df"}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestContainerPruneArgs|TestImagePruneArgs|TestVolumePruneArgs|TestNetworkPruneArgs|TestBuildCachePruneArgs|TestSystemDFArgs|TestDanglingImageListArgs|TestDanglingVolumeListArgs|TestNetworkListArgs' -v -count=1`

Expected: PASS (10 tests)

- [ ] **Step 6: Write failing tests for output parsers + network filter**

```go
// append to internal/runtime/prune_test.go
func TestParsePruneOutput(t *testing.T) {
	out := `deleted_container_1
deleted_container_2

Total reclaimed space: 1.2kB
`
	got := parsePruneOutput(out)
	want := []string{"deleted_container_1", "deleted_container_2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePruneOutput() = %v, want %v", got, want)
	}
}

func TestParsePruneOutputSkipsHeaders(t *testing.T) {
	out := `Deleted Containers:
  a1b2c3d4e5
Total reclaimed space: 5B
Untagged: tengiz-apps/demo:production-1
Deleted: sha256:abc
`
	got := parsePruneOutput(out)
	want := []string{"a1b2c3d4e5", "sha256:abc"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePruneOutput() = %v, want %v", got, want)
	}
}

func TestSplitLines(t *testing.T) {
	got := splitLines("one\n\n two \nthree\n")
	want := []string{"one", "two", "three"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitLines() = %v, want %v", got, want)
	}
}

func TestFilterBuiltinNetworks(t *testing.T) {
	got := filterBuiltinNetworks([]string{"bridge", "custom-net", "host", "none"})
	want := []string{"custom-net"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filterBuiltinNetworks() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 7: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestParsePruneOutput|TestSplitLines|TestFilterBuiltinNetworks' -v -count=1`

Expected: FAIL with `undefined: parsePruneOutput`

- [ ] **Step 8: Implement parsers + filter + exec helper**

Append to `internal/runtime/prune.go`:

```go
// parsePruneOutput extracts removed artifact identifiers from `docker ... prune`
// stdout. Header and summary lines are skipped.
func parsePruneOutput(out string) []string {
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		low := strings.ToLower(l)
		if strings.HasPrefix(low, "deleted") || strings.HasPrefix(low, "untagged") ||
			strings.HasPrefix(low, "total") || strings.HasPrefix(l, "-") {
			continue
		}
		ids = append(ids, l)
	}
	return ids
}

func splitLines(out string) []string {
	var lines []string
	for _, s := range strings.Split(out, "\n") {
		if t := strings.TrimSpace(s); t != "" {
			lines = append(lines, t)
		}
	}
	return lines
}

// filterBuiltinNetworks removes Docker's implicit networks, which are never
// candidates for pruning.
func filterBuiltinNetworks(nets []string) []string {
	result := nets[:0]
	for _, n := range nets {
		switch n {
		case "bridge", "host", "none":
			continue
		}
		result = append(result, n)
	}
	return result
}

// runCommandOutput executes `docker <args...>` and returns combined output.
func runCommandOutput(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}
```

- [ ] **Step 9: Run the two parser test groups together**

Run: `go test ./internal/runtime/ -run 'TestContainerPruneArgs|TestParsePruneOutput|TestSplitLines|TestFilterBuiltinNetworks' -v -count=1`

Expected: PASS

- [ ] **Step 10: Run all runtime package tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS (existing stub tests unaffected)

- [ ] **Step 11: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: add purge helpers and CleanupOptions/CleanupResult types to runtime"
```

---

### Task 2: Wire `Cleanup` + `SystemDF` through the `Manager` interface

**Files:**
- Modify: `internal/runtime/runtime.go` — interface + `stubManager`
- Modify: `internal/runtime/prune.go` — add `dockerRuntime.Cleanup` + `dockerRuntime.SystemDF`
- Modify: `internal/proxy/proxy_test.go:34-35`
- Modify: `internal/idle/idle_test.go:33-34`
- Modify: `internal/cli/root_test.go:98-100`
- Test: `internal/runtime/prune_test.go` (stub tests)

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, builders from Task 1
- Produces: `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` and `Manager.SystemDF(ctx context.Context) (string, error)` implemented by `dockerRuntime` and `stubManager`; acceptable by every existing mock

- [ ] **Step 1: Write the failing stub tests**

```go
// append to internal/runtime/prune_test.go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Stub Cleanup() error = %v", err)
	}
	if res.DryRun {
		t.Error("Stub Cleanup() DryRun set, want false")
	}
}

func TestStubSystemDF(t *testing.T) {
	m := NewStub()
	out, err := m.SystemDF(context.Background())
	if err != nil {
		t.Fatalf("Stub SystemDF() error = %v", err)
	}
	if out != "" {
		t.Errorf("Stub SystemDF() = %q, want empty", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestStubCleanup|TestStubSystemDF' -v -count=1`

Expected: FAIL with `undefined: Cleanup` / `undefined: SystemDF`

- [ ] **Step 3: Add the two methods to the `Manager` interface**

In `internal/runtime/runtime.go`, inside the `Manager` interface (after `Run`):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
	SystemDF(ctx context.Context) (string, error)
```

- [ ] **Step 4: Implement on `stubManager`**

Add to `internal/runtime/runtime.go` (after the `Run` method of `stubManager`):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{DryRun: opts.DryRun}, nil
}

func (m *stubManager) SystemDF(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 5: Implement on `dockerRuntime`**

Append to `internal/runtime/prune.go`:

```go
// runPruneOrList runs a real prune (or, when dryRun, the equivalent list command)
// and returns the removed/candidate artifact identifiers, parsed from stdout.
func runPruneOrList(ctx context.Context, pruneArgs, listArgs []string, dryRun bool) ([]string, error) {
	args := pruneArgs
	if dryRun {
		args = listArgs
	}
	out, err := runCommandOutput(ctx, args)
	if err != nil {
		return nil, err
	}
	if dryRun {
		return splitLines(out), nil
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	result := CleanupResult{DryRun: opts.DryRun}

	if opts.Containers {
		ids, err := runPruneOrList(ctx, containerPruneArgs(), containerPruneListArgs(), opts.DryRun)
		if err != nil {
			return result, err
		}
		result.Containers = ids
	}
	if opts.Images {
		ids, err := runPruneOrList(ctx, imagePruneArgs(), danglingImageListArgs(), opts.DryRun)
		if err != nil {
			return result, err
		}
		result.Images = ids
	}
	if opts.Volumes {
		ids, err := runPruneOrList(ctx, volumePruneArgs(), danglingVolumeListArgs(), opts.DryRun)
		if err != nil {
			return result, err
		}
		result.Volumes = ids
	}
	if opts.Networks {
		ids, err := runPruneOrList(ctx, networkPruneArgs(), networkListArgs(), opts.DryRun)
		if err != nil {
			return result, err
		}
		result.Networks = filterBuiltinNetworks(ids)
	}
	if opts.BuildCache {
		if opts.DryRun {
			out, err := runCommandOutput(ctx, buildCacheUsageArgs())
			if err != nil {
				return result, err
			}
			result.BuildCache = splitLines(out)
		} else {
			out, err := runCommandOutput(ctx, buildCachePruneArgs())
			if err != nil {
				return result, err
			}
			result.BuildCache = parsePruneOutput(out)
		}
	}
	return result, nil
}

func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error) {
	return runCommandOutput(ctx, systemDFArgs())
}
```

- [ ] **Step 6: Update the three mock structs**

In `internal/proxy/proxy_test.go` (after the `Run` method, line 35):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
func (m *mockRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

In `internal/idle/idle_test.go` (after the `Run` method, line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
func (m *mockRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

In `internal/cli/root_test.go` (after the `Run` method, line 100):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
func (m *mockRTForDeploy) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

- [ ] **Step 7: Compile the whole module**

Run: `go build ./...`

Expected: BUILD SUCCESS (no errors)

- [ ] **Step 8: Run all runtime, proxy, idle, cli tests**

Run: `go test ./internal/runtime/... ./internal/proxy/... ./internal/idle/... ./internal/cli/... -count=1`

Expected: PASS (per AGENTS.md, proxy tests are slow ~2s each and idle tests are time-sensitive — allow them to finish)

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune.go internal/runtime/prune_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: add Cleanup and SystemDF to runtime Manager interface"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` (`init()` adds command; `Execute()` defines flags)
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `config.NewStoreWithEnv(dataDir, env)`, `rt.KeepLastNImages`, `getEnv(cmd)` (existing), `dataDir` package var
- Produces: `tengiz cleanup [--all] [--dry-run] [--containers] [--images] [--volumes] [--networks] [--build-cache] [--keep N] [--verbose]` command registered on the root command

- [ ] **Step 1: Write the failing CLI tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command lookup error: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not registered")
	}
}

func TestCleanupFlagsExist(t *testing.T) {
	expected := []string{"all", "dry-run", "containers", "images", "volumes", "networks", "build-cache", "keep", "verbose"}
	for _, name := range expected {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupRunERequiresScope(t *testing.T) {
	err := cleanupRunE(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("expected error when no scope is selected")
	}
	if !strings.Contains(err.Error(), "containers") {
		t.Errorf("error %q should mention available scopes", err)
	}
}

func TestCleanupRunERejectsKeepZero(t *testing.T) {
	cmd := cleanupCmd
	cmd.ParseFlags([]string{"--images", "--keep", "0"})
	cmd.SetArgs([]string{})
	err := cleanupRunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error when --keep < 1")
	}
}

func TestActionWord(t *testing.T) {
	if got := actionWord(true); got != "would remove" {
		t.Errorf("actionWord(true) = %q, want %q", got, "would remove")
	}
	if got := actionWord(false); got != "removed" {
		t.Errorf("actionWord(false) = %q, want %q", got, "removed")
	}
}
```

Note: `cleanupRunE(&cobra.Command{}, nil)` and `cleanupRunE(cmd, nil)` return BEFORE `runtime.NewDocker()` is called (scope/keep validation happens first), so these tests pass without Docker installed. In `TestCleanupRunERequiresScope`, `getEnv` is not reached — no `env` flag is read when validation fails.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: cleanupRunE`, `undefined: actionWord`

- [ ] **Step 3: Implement `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (disk housekeeping)",
	Long: `Remove unused Docker resources to reclaim disk space on the host.

Containers managed by Tengiz (those labeled tengiz-app=<name>) are always
protected and never removed. Non-Tengiz stopped containers, dangling images,
unused volumes, unused networks, and the build cache can be pruned.

Use --dry-run to preview what would be removed without deleting anything.

Examples:
  tengiz cleanup                        # error: no scope selected
  tengiz cleanup --all                   # remove every unused resource
  tengiz cleanup --dry-run --all         # preview only (no deletion)
  tengiz cleanup --containers --images   # prune containers and images
  tengiz cleanup --images --keep 2       # prune dangling images, keep last 2 tags per app`,
	Args: cobra.NoArgs,
	RunE: cleanupRunE,
}

func cleanupRunE(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	keep, _ := cmd.Flags().GetInt("keep")
	verbose, _ := cmd.Flags().GetBool("verbose")

	if all {
		containers, images, volumes, networks, buildCache = true, true, true, true, true
	}

	if !scopeSelected(containers, images, volumes, networks, buildCache) {
		return fmt.Errorf("select a scope: one or more of --containers, --images, --volumes, --networks, --build-cache, or --all")
	}
	if keep < 1 {
		return fmt.Errorf("--keep must be at least 1")
	}

	env := getEnv(cmd)
	rt, err := runtime.NewDocker()
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}

	if dryRun {
		fmt.Println("[tengiz] dry run — nothing will be removed")
	}

	res, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		DryRun:     dryRun,
	})
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	if images && !dryRun {
		store := config.NewStoreWithEnv(dataDir, env)
		apps, listErr := store.ListApps()
		if listErr != nil {
			log.Printf("[tengiz] warning: listing apps for image retention failed: %v", listErr)
		} else {
			for _, app := range apps {
				if keepErr := rt.KeepLastNImages(cmd.Context(), app.Name, keep); keepErr != nil {
					log.Printf("[tengiz] warning: image retention for %s: %v", app.Name, keepErr)
				}
			}
		}
	}

	categories := []struct {
		enabled bool
		name    string
		items   []string
	}{
		{containers, "containers", res.Containers},
		{images, "images", res.Images},
		{volumes, "volumes", res.Volumes},
		{networks, "networks", res.Networks},
		{buildCache, "build cache", res.BuildCache},
	}
	for _, c := range categories {
		if !c.enabled {
			continue
		}
		if verbose && len(c.items) > 0 {
			fmt.Printf("%s:\n", c.name)
			for _, item := range c.items {
				fmt.Printf("  %s\n", item)
			}
			continue
		}
		fmt.Printf("%s %s: %d\n", c.name, actionWord(dryRun), len(c.items))
	}
	if images {
		fmt.Printf("old images retained: last %d per app\n", keep)
	}

	if info, err := rt.SystemDF(cmd.Context()); err == nil {
		fmt.Print(info)
	}
	return nil
}

func scopeSelected(flags ...bool) bool {
	for _, f := range flags {
		if f {
			return true
		}
	}
	return false
}

func actionWord(dryRun bool) string {
	if dryRun {
		return "would remove"
	}
	return "removed"
}
```

- [ ] **Step 4: Register the command and define its flags in `init()`**

Flag definitions MUST live in `init()` (like `logsCmd`, `webhookCmd`, `domainCmd` flags) — not in `Execute()` — because the tests call `rootCmd.Execute()` directly and call `cleanupCmd.Flags().Lookup(...)` before `Execute()` runs.

In `internal/cli/root.go`, `init()` — add after `rootCmd.AddCommand(notificationCmd)` (line 75):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("all", false, "remove every supported resource category")
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without deleting anything")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune unused images and keep the last --keep tags per app")
	cleanupCmd.Flags().Bool("volumes", false, "prune volumes not used by any container")
	cleanupCmd.Flags().Bool("networks", false, "prune networks not used by any container")
	cleanupCmd.Flags().Bool("build-cache", false, "prune the Docker build cache")
	cleanupCmd.Flags().Int("keep", 5, "number of image tags to keep per app (only with --images)")
	cleanupCmd.Flags().Bool("verbose", false, "print the exact artifacts removed")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`

Expected: PASS (5 tests)

- [ ] **Step 6: Compile the whole module**

Run: `go build ./...`

Expected: BUILD SUCCESS

- [ ] **Step 7: Run all CLI tests**

Run: `go test ./internal/cli/... -count=1`

Expected: All PASS

- [ ] **Step 8: Manual smoke test (docker required — skip if no dockerd)**

Run: `./tengiz cleanup --dry-run --all`

Expected: prints dry-run banner + `containers would remove: 0` style lines (or candidate lists) + `docker system df` table. Nothing deleted.

- [ ] **Step 9: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for protected Docker housekeeping"
```

---

### Task 4: Documentation (`README.md`, `FUTURES_FEATURES.md`)

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: final flag set from Task 3 (exact names above)
- Produces: documented CLI surface + feature tracker updated

- [ ] **Step 1: Add the command to the README CLI Reference**

In `README.md`, insert a new section directly after the `### \`tengiz ps\`` block (which currently ends around line 151 with the HEALTH description, before `### \`tengiz logs ...\``).

Insert:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space on the host. Tengiz-managed containers (labeled `tengiz-app=<name>`) are always protected and never pruned.

| Flag | Description |
|------|-------------|
| `--all` | Prune every supported category below |
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune unused images, keeping the last `--keep` tags per app |
| `--volumes` | Prune volumes not used by any container |
| `--networks` | Prune networks not used by any container |
| `--build-cache` | Prune the Docker build cache |
| `--keep <n>` | Number of image tags to keep per app (default: 5) |
| `--dry-run` | Report what would be removed without deleting anything |
| `--verbose` | Print the exact artifacts removed |

Examples:

```bash
tengiz cleanup --dry-run --all     # preview everything
tengiz cleanup --all               # reclaim all unused disk space
tengiz cleanup --containers --images --keep 3
```

After running, `docker system df` summarizes the reclaimed disk usage.
```

- [ ] **Step 2: Add `tengiz cleanup` to the README Features list**

In `README.md`, under `## Features` (around line 12), add one bullet to the existing list, e.g.:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, images, volumes, networks and the build cache while never touching Tengiz-managed containers.
```

Match the existing bullet style (start with `- **` and short description).

- [ ] **Step 3: Mark the feature implemented in `FUTURES_FEATURES.md`**

In `docs/FUTURES_FEATURES.md` P0 table, replace row #6 (line 19):

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based pruning ('tengiz cleanup' command) keeps Tengiz containers while reclaiming space. |
```

And add a row to the `✅ Implemented Features (Not Pending)` table (after the Webhook row, around line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-08) |
```

- [ ] **Step 4: Verify the docs render**

Run: `git diff README.md docs/FUTURES_FEATURES.md`

Expected: only the intended additions; no markdown tables broken

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

### Task 5: Final verification and self-review

**Files:**
- None (verification only)

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -count=1`

Expected: All PASS (proxy tests ~2s each, idle time-sensitive tests, as documented in AGENTS.md)

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`

Expected: No findings

- [ ] **Step 3: Build the binary**

Run: `go build -o tengiz . && ./tengiz cleanup --help`

Expected: help text shows all cleanup flags; binary builds

- [ ] **Step 4: Self-review against the spec**

Check against `docs/FUTURES_FEATURES.md` # 6 and the "Docker Housekeeping" prose section:

- Label-based filtering protects Tengiz-managed containers — ✅ Task 1 (`--filter "label!=tengiz-app"` in `containerPruneArgs`)
- `tengiz cleanup` command — ✅ Task 3
- Reclaims disk from volumes, networks, containers, images, build cache — ✅ Task 1-3
- Per-app old-image retention — ✅ Task 3 (`--images` uses existing `KeepLastNImages`)
- Dry-run safety — ✅ Task 1/3 (`--dry-run` lists, never deletes)
- Docs updated — Task 4

- [ ] **Step 5: Placeholder scan**

Search the plan for any red-flag phrases. None authored — every code step shows complete code, every expected output is stated.

- [ ] **Step 6: Type consistency check**

- `CleanupOptions{Containers, Images, Volumes, Networks, BuildCache, DryRun bool}` defined Task 1, used identically in Task 2 `Cleanup` and Task 3 `cleanupRunE`
- `CleanupResult{DryRun; Containers; Images; Volumes; Networks; BuildCache}` defined Task 1, read by Task 3 summary printer
- Helper arg-builders (Task 1) and `runPruneOrList`/`runCommandOutput` (Task 2) share the same signatures
- `runtime.CleanupOptions`/`runtime.CleanupResult` qualified names match exactly in all three mock updates (Task 2 Step 6)
- `actionWord(dryRun bool) string` used only in Task 3, tested in Task 3 Step 1

- [ ] **Step 7: Final commit if any stragglers**

```bash
git status
```

If clean, skip. If modified files remain, commit them with an appropriate message.