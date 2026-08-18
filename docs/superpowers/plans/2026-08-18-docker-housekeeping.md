# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that frees host disk space by pruning unused Docker resources (stopped containers, dangling images, old deployment images, unused networks, unused volumes, build cache) while never touching running or Tengiz-managed containers.

**Architecture:** A new `runtime.Housekeeper` interface (implemented by `dockerRuntime`) exposes a single `Prune(ctx, CleanupOptions) (CleanupResult, error)` method that wraps the `docker` CLI via `os/exec` (same pattern as the existing `runtime.Manager` methods). All `docker` command construction and output parsing live in pure, unit-testable functions. The CLI `cleanupCmd` type-asserts the runtime to `Housekeeper` and delegates to a testable `runCleanup(cmd, hk)` helper. A separate interface (not extending `runtime.Manager`) keeps the change surface small — existing mocks in `idle`, `proxy`, and `cli` tests are untouched.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `os/exec` Docker CLI pattern. No new external dependencies. No Docker SDK.

## Global Constraints

- No new external dependencies — stdlib + existing `cobra`
- Docker invoked only via `os/exec` `docker` CLI — never the Docker SDK
- Tengiz-managed resources (labeled `tengiz-app=<appname>`) are **never** pruned: every container/network/volume prune uses `--filter label!=tengiz-app`
- Running containers are never touched — container prune only targets stopped (`exited`/`created`/`dead`) containers
- Volumes are **never** part of the safe default or `--all` set — only removed when `--volumes` is passed explicitly
- Old deployment images: keep the `keepN` most recent per app (default `5`, matching existing `KeepLastNImages` behavior); never remove a tag ending in `:latest`
- `CleanupResult.Empty()` is `false` when any category has items or build cache was reported — used by the CLI to skip "nothing to remove"
- CLI tests must not require a Docker daemon — they use a `mockHousekeeper`
- `runtime.Manager` interface, `stubManager`, and existing mocks (`idle`, `proxy`, `cli/root_test.go`) are **not** modified
- Existing tests must continue to pass without modification
- New files follow existing conventions: package `runtime` (housekeeping) and package `cli` (cleanup command) with tabs for indentation, `fmt.Errorf("docker <cmd>: %w\n%s", err, out)` error wrapping

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/housekeeping.go` (create) | `CleanupOptions`, `CleanupResult`, `Empty()`, `Housekeeper` interface (Task 1); pure arg builders `pruneContainersArgs`/`pruneDanglingImagesArgs`/`pruneNetworksArgs`/`pruneVolumesArgs`, parsers `parsePruneItems`/`parseListLines`, `oldImageTags`, `tengizImageListArgs` (Task 2); `(*dockerRuntime).Prune` + private exec helpers (Task 3) |
| `internal/runtime/housekeeping_test.go` (create) | Unit tests for the types and every pure function + no-docker `Prune` paths |
| `internal/cli/cleanup.go` (create) | `cleanupCmd` + flags (self-registering via `init()`, like `preview.go`), `cleanupOptionsFromFlags`, `runCleanup`, `confirmCleanup`, `printCleanupResult`, `printCleanupItems` |
| `internal/cli/cleanup_test.go` (create) | `mockHousekeeper`, command registration/flags, option-building, `runCleanup` output tests |
| `README.md` (modify) | Document `tengiz cleanup` in CLI Reference |
| `AGENTS.md` (modify) | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` (modify) | Mark feature #6 as ✅ Implemented |

`internal/cli/root.go` is **not** modified (cleanupCmd self-registers). `internal/runtime/runtime.go` is **not** modified (Housekeeper is a separate interface).

---

### Task 1: Cleanup types + Housekeeper interface

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `runtime.CleanupOptions{Containers, Images, Networks, Volumes, BuildCache bool; KeepImages int; DryRun bool}`, `runtime.CleanupResult{Containers, Images, Networks, Volumes []string; BuildCache, DryRun bool}`, `(CleanupResult) Empty() bool`, `runtime.Housekeeper interface { Prune(ctx context.Context, opts CleanupOptions) (CleanupResult, error) }`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/housekeeping_test.go
package runtime

import "testing"

func TestCleanupResultEmpty(t *testing.T) {
	if !(CleanupResult{}).Empty() {
		t.Error("CleanupResult{} should be empty")
	}
	if (CleanupResult{Containers: []string{"abc"}}).Empty() {
		t.Error("result with containers should not be empty")
	}
	if (CleanupResult{Images: []string{"tag"}}).Empty() {
		t.Error("result with images should not be empty")
	}
	if (CleanupResult{Networks: []string{"net"}}).Empty() {
		t.Error("result with networks should not be empty")
	}
	if (CleanupResult{Volumes: []string{"vol"}}).Empty() {
		t.Error("result with volumes should not be empty")
	}
	if (CleanupResult{BuildCache: true}).Empty() {
		t.Error("result with build cache should not be empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestCleanupResultEmpty -v -count=1`
Expected: FAIL with `undefined: CleanupResult`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/runtime/housekeeping.go
package runtime

import "context"

// CleanupOptions selects which Docker resource categories to clean.
type CleanupOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	KeepImages int  // most recent deployment images to keep per app (<=0 means 5)
	DryRun     bool // report what would be removed without removing anything
}

// CleanupResult lists what was (or, for DryRun, would be) removed.
type CleanupResult struct {
	Containers []string
	Images     []string
	Networks   []string
	Volumes    []string
	BuildCache bool
	DryRun     bool
}

// Empty reports whether nothing was (or would be) removed.
func (c CleanupResult) Empty() bool {
	return len(c.Containers) == 0 && len(c.Images) == 0 &&
		len(c.Networks) == 0 && len(c.Volumes) == 0 && !c.BuildCache
}

// Housekeeper is the host-level Docker maintenance capability. The docker
// runtime implements it; the CLI type-asserts a Manager to it for cleanup.
type Housekeeper interface {
	Prune(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run TestCleanupResultEmpty -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add runtime cleanup types and Housekeeper interface"
```

---

### Task 2: Pure docker-command builders and output parsers

**Files:**
- Modify: `internal/runtime/housekeeping.go` (append)
- Test: `internal/runtime/housekeeping_test.go` (append)

**Interfaces:**
- Consumes: `runtime.CleanupOptions`/`CleanupResult` from Task 1
- Produces (all used by Task 3): `pruneContainersArgs(dryRun bool) []string`, `pruneDanglingImagesArgs(dryRun bool) []string`, `pruneNetworksArgs(dryRun bool) []string`, `pruneVolumesArgs(dryRun bool) []string`, `tengizImageListArgs() []string`, `parsePruneItems(out string) []string`, `parseListLines(out string) []string`, `oldImageTags(lines []string, keepN int) []string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/housekeeping_test.go (append)
func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v (len=%d), want %v (len=%d)", got, len(got), want, len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q (full: got %v want %v)", i, got[i], want[i], got, want)
		}
	}
}

func TestPruneContainersArgs(t *testing.T) {
	assertArgs(t, pruneContainersArgs(false),
		[]string{"container", "prune", "-f", "--filter", "label!=tengiz-app"})
	assertArgs(t, pruneContainersArgs(true),
		[]string{"ps", "-a",
			"--filter", "status=exited",
			"--filter", "status=created",
			"--filter", "status=dead",
			"--filter", "label!=tengiz-app",
			"--format", "{{.Names}}"})
}

func TestPruneDanglingImagesArgs(t *testing.T) {
	assertArgs(t, pruneDanglingImagesArgs(false),
		[]string{"image", "prune", "-f"})
	assertArgs(t, pruneDanglingImagesArgs(true),
		[]string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"})
}

func TestPruneNetworksArgs(t *testing.T) {
	assertArgs(t, pruneNetworksArgs(false),
		[]string{"network", "prune", "-f", "--filter", "label!=tengiz-app"})
	assertArgs(t, pruneNetworksArgs(true),
		[]string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"})
}

func TestPruneVolumesArgs(t *testing.T) {
	assertArgs(t, pruneVolumesArgs(false),
		[]string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
	assertArgs(t, pruneVolumesArgs(true),
		[]string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"})
}

func TestTengizImageListArgs(t *testing.T) {
	assertArgs(t, tengizImageListArgs(),
		[]string{"images", "--filter", "reference=tengiz-apps/*", "--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}"})
}

func TestParsePruneItems(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want []string
	}{
		{
			name: "container prune",
			out:  "ff33306bf7d5\nfb06e66a7d36\nTotal reclaimed space: 1.2kB\n",
			want: []string{"ff33306bf7d5", "fb06e66a7d36"},
		},
		{
			name: "image prune with header",
			out:  "Deleted Images:\nuntagged: tengiz-apps/myapp:1700000000\ndeleted: sha256:abc\nTotal reclaimed space: 2.1kB\n",
			want: []string{"untagged: tengiz-apps/myapp:1700000000", "deleted: sha256:abc"},
		},
		{
			name: "volume prune",
			out:  "local-data\nTotal reclaimed space: 0B\n",
			want: []string{"local-data"},
		},
		{
			name: "nothing to prune",
			out:  "Total reclaimed space: 0B\n",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePruneItems(tt.out)
			if len(got) != len(tt.want) {
				t.Fatalf("parsePruneItems(%q) = %v, want %v", tt.out, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("parsePruneItems(%q)[%d] = %q, want %q", tt.out, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseListLines(t *testing.T) {
	out := "container-a\ncontainer-b\n\n"
	got := parseListLines(out)
	if len(got) != 2 || got[0] != "container-a" || got[1] != "container-b" {
		t.Fatalf("parseListLines(%q) = %v", out, got)
	}
	if got := parseListLines(""); len(got) != 0 {
		t.Fatalf("parseListLines(\"\") = %v, want empty", got)
	}
}

func TestOldImageTags(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		keepN int
		want  []string
	}{
		{
			name: "keeps most recent per app",
			lines: []string{
				"tengiz-apps/myapp:1700000000|2026-01-01 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:1700000001|2026-01-02 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:1700000002|2026-01-03 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:1700000003|2026-01-04 00:00:00 +0000 UTC",
			},
			keepN: 2,
			want:  []string{"tengiz-apps/myapp:1700000000", "tengiz-apps/myapp:1700000001"},
		},
		{
			name: "never removes latest",
			lines: []string{
				"tengiz-apps/myapp:latest|2026-01-01 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:1700000001|2026-01-02 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:1700000002|2026-01-03 00:00:00 +0000 UTC",
			},
			keepN: 1,
			want:  []string{"tengiz-apps/myapp:1700000001"},
		},
		{
			name: "keeps all when under retention",
			lines: []string{
				"tengiz-apps/myapp:1700000000|2026-01-01 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:1700000001|2026-01-02 00:00:00 +0000 UTC",
			},
			keepN: 5,
			want:  nil,
		},
		{
			name: "groups by app",
			lines: []string{
				"tengiz-apps/alpha:1|2026-01-01 00:00:00 +0000 UTC",
				"tengiz-apps/alpha:2|2026-01-02 00:00:00 +0000 UTC",
				"tengiz-apps/alpha:3|2026-01-03 00:00:00 +0000 UTC",
				"tengiz-apps/beta:5|2026-01-01 00:00:00 +0000 UTC",
				"tengiz-apps/beta:6|2026-01-02 00:00:00 +0000 UTC",
			},
			keepN: 2,
			want:  []string{"tengiz-apps/alpha:1"},
		},
		{
			name: "defaults keepN to 5",
			lines: []string{
				"tengiz-apps/myapp:1|2026-01-01 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:2|2026-01-02 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:3|2026-01-03 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:4|2026-01-04 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:5|2026-01-05 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:6|2026-01-06 00:00:00 +0000 UTC",
			},
			keepN: 0,
			want:  []string{"tengiz-apps/myapp:1"},
		},
		{
			name: "skips malformed lines",
			lines: []string{
				"",
				"tengiz-apps/myapp:2|2026-01-02 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:3|2026-01-03 00:00:00 +0000 UTC",
				"tengiz-apps/myapp:no-timestamp",
				"tengiz-apps/untagged:|2026-01-01 00:00:00 +0000 UTC",
			},
			keepN: 1,
			want:  []string{"tengiz-apps/myapp:2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := oldImageTags(tt.lines, tt.keepN)
			if len(got) != len(tt.want) {
				t.Fatalf("oldImageTags() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("oldImageTags()[%d] = %q, want %q (full: %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneContainersArgs|TestPruneDanglingImagesArgs|TestPruneNetworksArgs|TestPruneVolumesArgs|TestTengizImageListArgs|TestParsePruneItems|TestParseListLines|TestOldImageTags" -v -count=1`
Expected: FAIL with `undefined: pruneContainersArgs` (and the other undefined functions)

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/housekeeping.go`:

```go
func pruneContainersArgs(dryRun bool) []string {
	if dryRun {
		return []string{"ps", "-a",
			"--filter", "status=exited",
			"--filter", "status=created",
			"--filter", "status=dead",
			"--filter", "label!=tengiz-app",
			"--format", "{{.Names}}",
		}
	}
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func pruneDanglingImagesArgs(dryRun bool) []string {
	if dryRun {
		return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
	}
	return []string{"image", "prune", "-f"}
}

func pruneNetworksArgs(dryRun bool) []string {
	if dryRun {
		return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
	}
	return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func pruneVolumesArgs(dryRun bool) []string {
	if dryRun {
		return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
	}
	return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
}

// tengizImageListArgs lists Tengiz app images with their creation time.
func tengizImageListArgs() []string {
	return []string{"images", "--filter", "reference=tengiz-apps/*", "--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}"}
}

// parsePruneItems extracts the removed-item lines from a docker prune command's
// stdout, skipping the "Deleted ..." headers and the "Total reclaimed space"
// summary line.
func parsePruneItems(out string) []string {
	var items []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Deleted ") || strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		items = append(items, line)
	}
	return items
}

// parseListLines extracts non-empty lines from a docker ls command's stdout.
func parseListLines(out string) []string {
	var items []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		items = append(items, line)
	}
	return items
}

// oldImageTags returns which tagged Tengiz app images to remove: for each app
// (image repository), all but the keepN most recent by creation time, skipping
// any tag ending in ":latest".
func oldImageTags(lines []string, keepN int) []string {
	if keepN <= 0 {
		keepN = 5
	}
	type imgEntry struct {
		tag     string
		created string
	}
	byRepo := make(map[string][]imgEntry)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		repoTag := parts[0]
		idx := strings.LastIndex(repoTag, ":")
		if idx < 0 || idx == len(repoTag)-1 {
			continue
		}
		repo, tag := repoTag[:idx], repoTag[idx+1:]
		byRepo[repo] = append(byRepo[repo], imgEntry{tag: tag, created: parts[1]})
	}
	var toRemove []string
	for repo, entries := range byRepo {
		if len(entries) <= keepN {
			continue
		}
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].created < entries[j].created
		})
		for i := 0; i < len(entries)-keepN; i++ {
			if entries[i].tag == "latest" {
				continue
			}
			toRemove = append(toRemove, repo+":"+entries[i].tag)
		}
	}
	sort.Strings(toRemove)
	return toRemove
}
```

Add `"sort"` and `"strings"` to the imports of `internal/runtime/housekeeping.go`:

```go
import (
	"context"
	"sort"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneContainersArgs|TestPruneDanglingImagesArgs|TestPruneNetworksArgs|TestPruneVolumesArgs|TestTengizImageListArgs|TestParsePruneItems|TestParseListLines|TestOldImageTags" -v -count=1`
Expected: PASS (all sub-tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add docker prune arg builders and output parsers"
```

---

### Task 3: `Prune` orchestration on the docker runtime

**Files:**
- Modify: `internal/runtime/housekeeping.go` (append)
- Test: `internal/runtime/housekeeping_test.go` (append)

**Interfaces:**
- Consumes: `CleanupOptions`/`CleanupResult` (Task 1), all pure helpers (Task 2), existing `(*dockerRuntime).RemoveImage` from `internal/runtime/cleanup.go`
- Produces: `func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — satisfies `runtime.Housekeeper`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/housekeeping_test.go (append)
func TestPruneNothingSelected(t *testing.T) {
	r := &dockerRuntime{}
	result, err := r.Prune(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !result.Empty() {
		t.Errorf("Prune() with no selection should return empty result, got %+v", result)
	}
	if result.DryRun {
		t.Error("DryRun should be false")
	}
}

func TestPruneDryRunBuildCache(t *testing.T) {
	r := &dockerRuntime{}
	result, err := r.Prune(context.Background(), CleanupOptions{BuildCache: true, DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !result.BuildCache {
		t.Error("BuildCache should be reported in dry-run")
	}
	if !result.DryRun {
		t.Error("DryRun should be true")
	}
	if !result.Empty() {
		t.Errorf("dry-run build cache only should still be empty of items, got %+v", result)
	}
}
```

Note: both tests only exercise paths that never invoke the `docker` binary (empty selection, or dry-run build cache), so they run safely in CI without a Docker daemon.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneNothingSelected|TestPruneDryRunBuildCache" -v -count=1`
Expected: FAIL with `undefined: (*dockerRuntime).Prune` (and it does not satisfy `Housekeeper`)

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/housekeeping.go`:

```go
// Prune removes the Docker resources selected by opts. In dry-run mode nothing
// is removed; the result reports what would be removed instead.
func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var result CleanupResult
	result.DryRun = opts.DryRun

	var err error
	if opts.Containers {
		result.Containers, err = r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return result, fmt.Errorf("containers: %w", err)
		}
	}
	if opts.Images {
		result.Images, err = r.pruneImages(ctx, opts.KeepImages, opts.DryRun)
		if err != nil {
			return result, fmt.Errorf("images: %w", err)
		}
	}
	if opts.Networks {
		result.Networks, err = r.pruneNetworks(ctx, opts.DryRun)
		if err != nil {
			return result, fmt.Errorf("networks: %w", err)
		}
	}
	if opts.Volumes {
		result.Volumes, err = r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return result, fmt.Errorf("volumes: %w", err)
		}
	}
	if opts.BuildCache {
		if !opts.DryRun {
			if err := r.pruneBuildCache(ctx); err != nil {
				return result, fmt.Errorf("build cache: %w", err)
			}
		}
		result.BuildCache = true
	}
	return result, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) ([]string, error) {
	args := pruneContainersArgs(dryRun)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	if dryRun {
		return parseListLines(string(out)), nil
	}
	return parsePruneItems(string(out)), nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, keepN int, dryRun bool) ([]string, error) {
	var removed []string

	args := pruneDanglingImagesArgs(dryRun)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	if dryRun {
		removed = append(removed, parseListLines(string(out))...)
	} else {
		removed = append(removed, parsePruneItems(string(out))...)
	}

	listOut, err := exec.CommandContext(ctx, "docker", tengizImageListArgs()...).CombinedOutput()
	if err != nil {
		return removed, fmt.Errorf("docker images: %w\n%s", err, string(listOut))
	}
	lines := strings.Split(strings.TrimSpace(string(listOut)), "\n")
	for _, tag := range oldImageTags(lines, keepN) {
		if dryRun {
			removed = append(removed, tag)
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			return removed, fmt.Errorf("remove old image %s: %w", tag, err)
		}
		removed = append(removed, tag)
	}
	return removed, nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) ([]string, error) {
	args := pruneNetworksArgs(dryRun)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	if dryRun {
		return parseListLines(string(out)), nil
	}
	return parsePruneItems(string(out)), nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) ([]string, error) {
	args := pruneVolumesArgs(dryRun)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	if dryRun {
		return parseListLines(string(out)), nil
	}
	return parsePruneItems(string(out)), nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-af")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}
```

Add `"fmt"` and `"os/exec"` to the imports of `internal/runtime/housekeeping.go`:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneNothingSelected|TestPruneDryRunBuildCache" -v -count=1`
Expected: PASS

- [ ] **Step 5: Verify the whole runtime package builds and tests pass**

Run: `go build ./... && go test ./internal/runtime/... -count=1`
Expected: build succeeds, all runtime tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: implement runtime Prune for docker housekeeping"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Housekeeper` (with `Prune(ctx, CleanupOptions) (CleanupResult, error)`) and `runtime.CleanupOptions`/`runtime.CleanupResult` from Tasks 1-3
- Produces: `cleanupCmd *cobra.Command` (self-registered via `init()`), `runCleanup(cmd *cobra.Command, hk runtime.Housekeeper) error`, `cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions`, `printCleanupResult(opts runtime.CleanupOptions, result runtime.CleanupResult)`, `confirmCleanup() bool`, `printCleanupItems(label string, items []string, dryRun bool)`

Behavior contract: bare `tengiz cleanup` selects the safe default (containers + images + networks + build cache, **never** volumes). Any explicit category flag replaces the default with exactly the requested categories. `--dry-run` previews without removing; `--force` skips the confirmation prompt.

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

type mockHousekeeper struct {
	result runtime.CleanupResult
	err    error
	opts   runtime.CleanupOptions
	called bool
}

func (m *mockHousekeeper) Prune(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	m.called = true
	m.opts = opts
	return m.result, m.err
}

func newCleanupTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cleanup"}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	cmd.Flags().Int("keep-images", 5, "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("force", false, "")
	return cmd
}

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
	for _, flag := range []string{"containers", "images", "networks", "volumes", "build-cache", "keep-images", "dry-run", "force"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing flag --%s", flag)
		}
	}
}

func TestCleanupOptionsSafeDefault(t *testing.T) {
	cmd := newCleanupTestCmd()
	var opts runtime.CleanupOptions
	cmd.RunE = func(c *cobra.Command, args []string) error {
		opts = cleanupOptionsFromFlags(c)
		return nil
	}
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Errorf("safe default should include containers/images/networks/build-cache, got %+v", opts)
	}
	if opts.Volumes {
		t.Error("volumes must never be in the safe default")
	}
	if opts.KeepImages != 5 {
		t.Errorf("KeepImages = %d, want 5", opts.KeepImages)
	}
	if opts.DryRun {
		t.Error("DryRun should be false by default")
	}
}

func TestCleanupOptionsExplicit(t *testing.T) {
	cmd := newCleanupTestCmd()
	var opts runtime.CleanupOptions
	cmd.RunE = func(c *cobra.Command, args []string) error {
		opts = cleanupOptionsFromFlags(c)
		return nil
	}
	cmd.SetArgs([]string{"--containers", "--dry-run", "--keep-images", "3"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !opts.Containers || opts.Images || opts.Networks || opts.Volumes || opts.BuildCache {
		t.Errorf("only containers should be selected, got %+v", opts)
	}
	if opts.KeepImages != 3 {
		t.Errorf("KeepImages = %d, want 3", opts.KeepImages)
	}
	if !opts.DryRun {
		t.Error("DryRun should be true")
	}
}

func TestCleanupOptionsVolumesOnly(t *testing.T) {
	cmd := newCleanupTestCmd()
	var opts runtime.CleanupOptions
	cmd.RunE = func(c *cobra.Command, args []string) error {
		opts = cleanupOptionsFromFlags(c)
		return nil
	}
	cmd.SetArgs([]string{"--volumes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !opts.Volumes {
		t.Error("volumes should be selected")
	}
	if opts.Containers || opts.Images || opts.Networks || opts.BuildCache {
		t.Errorf("other categories should not be selected, got %+v", opts)
	}
}

func TestRunCleanupDryRunEmpty(t *testing.T) {
	cmd := newCleanupTestCmd()
	mock := &mockHousekeeper{}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return runCleanup(c, mock)
	}
	cmd.SetArgs([]string{"--dry-run"})
	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !mock.called {
		t.Fatal("Prune was not called")
	}
	if !mock.opts.DryRun {
		t.Error("Prune should receive DryRun=true")
	}
	if !strings.Contains(output, "dry-run") {
		t.Errorf("output missing dry-run marker: %s", output)
	}
	if !strings.Contains(output, "nothing to remove") {
		t.Errorf("output missing 'nothing to remove': %s", output)
	}
}

func TestRunCleanupDryRunResult(t *testing.T) {
	cmd := newCleanupTestCmd()
	mock := &mockHousekeeper{result: runtime.CleanupResult{
		Containers: []string{"abc123"},
		Images:     []string{"tengiz-apps/myapp:1700000000"},
		BuildCache: true,
		DryRun:     true,
	}}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return runCleanup(c, mock)
	}
	cmd.SetArgs([]string{"--dry-run"})
	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	for _, want := range []string{"1 containers would be removed", "1 images would be removed", "build cache would be removed"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %s", want, output)
		}
	}
	if strings.Contains(output, "nothing to remove") {
		t.Errorf("output should not say 'nothing to remove': %s", output)
	}
}

func TestRunCleanupForce(t *testing.T) {
	cmd := newCleanupTestCmd()
	mock := &mockHousekeeper{}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return runCleanup(c, mock)
	}
	cmd.SetArgs([]string{"--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !mock.called {
		t.Fatal("Prune was not called")
	}
	if mock.opts.DryRun {
		t.Error("DryRun should be false")
	}
	if !mock.opts.Containers || !mock.opts.Images || !mock.opts.Networks || !mock.opts.BuildCache {
		t.Errorf("expected safe default selection, got %+v", mock.opts)
	}
	if mock.opts.Volumes {
		t.Error("volumes must not be selected without --volumes")
	}
}

func TestRunCleanupPruneError(t *testing.T) {
	cmd := newCleanupTestCmd()
	mock := &mockHousekeeper{err: context.DeadlineExceeded}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return runCleanup(c, mock)
	}
	cmd.SetArgs([]string{"--force"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cleanup:") {
		t.Fatalf("expected cleanup-wrapped error, got %v", err)
	}
}
```

Note: `captureOutput` is defined in `internal/cli/root_test.go` (same package `cli`) and is reused here. All tests use `--dry-run` or `--force` so no test reads stdin.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup|TestRunCleanup" -v -count=1`
Expected: FAIL with `undefined: cleanupCmd`, `undefined: cleanupOptionsFromFlags`, `undefined: runCleanup`, `undefined: printCleanupResult`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cli/cleanup.go
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
	Short: "Remove unused Docker resources to free disk space",
	Long: `Remove unused Docker resources to free disk space on the host.

Safe default (no flags): removes stopped containers not managed by Tengiz,
dangling images, old deployment images beyond the retention window, unused
networks, and build cache. Resources managed by Tengiz (labeled
tengiz-app=<name>) are never removed.

Volumes are never removed unless --volumes is passed explicitly.

Use --dry-run to preview what would be removed before changing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		hk, ok := rt.(runtime.Housekeeper)
		if !ok {
			return fmt.Errorf("docker runtime does not support cleanup")
		}
		return runCleanup(cmd, hk)
	},
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images and old deployment images")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes (never part of the default set)")
	cleanupCmd.Flags().Bool("build-cache", false, "remove build cache")
	cleanupCmd.Flags().Int("keep-images", 5, "number of most recent deployment images to keep per app")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("force", false, "skip the confirmation prompt")
	rootCmd.AddCommand(cleanupCmd)
}

// cleanupOptionsFromFlags builds the cleanup selection from parsed flags.
// With no category flags, the safe default set is used (volumes excluded).
func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	keepImages, _ := cmd.Flags().GetInt("keep-images")

	opts := runtime.CleanupOptions{
		KeepImages: keepImages,
		DryRun:     dryRun,
	}
	if v, _ := cmd.Flags().GetBool("containers"); v {
		opts.Containers = true
	}
	if v, _ := cmd.Flags().GetBool("images"); v {
		opts.Images = true
	}
	if v, _ := cmd.Flags().GetBool("networks"); v {
		opts.Networks = true
	}
	if v, _ := cmd.Flags().GetBool("volumes"); v {
		opts.Volumes = true
	}
	if v, _ := cmd.Flags().GetBool("build-cache"); v {
		opts.BuildCache = true
	}

	if !opts.Containers && !opts.Images && !opts.Networks && !opts.Volumes && !opts.BuildCache {
		opts.Containers = true
		opts.Images = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts
}

// runCleanup executes the selected cleanup against a Housekeeper.
// Extracted from the cobra RunE so it can be tested with a mock.
func runCleanup(cmd *cobra.Command, hk runtime.Housekeeper) error {
	opts := cleanupOptionsFromFlags(cmd)

	if !opts.DryRun {
		force, _ := cmd.Flags().GetBool("force")
		if !force && !confirmCleanup() {
			fmt.Println("[tengiz] cleanup aborted")
			return nil
		}
	}

	result, err := hk.Prune(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	printCleanupResult(opts, result)
	return nil
}

// confirmCleanup asks the user to confirm a destructive operation.
func confirmCleanup() bool {
	fmt.Print("[tengiz] remove unused Docker resources? This cannot be undone. Continue? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(answer)) == "y"
}

// printCleanupResult prints what was (or would be) removed.
func printCleanupResult(opts runtime.CleanupOptions, result runtime.CleanupResult) {
	if opts.DryRun {
		fmt.Println("[tengiz] dry-run: no changes made")
	} else {
		fmt.Println("[tengiz] cleanup complete")
	}
	if result.BuildCache {
		printCleanupItems("build cache", []string{"(all)"}, opts.DryRun)
	}
	printCleanupItems("containers", result.Containers, opts.DryRun)
	printCleanupItems("images", result.Images, opts.DryRun)
	printCleanupItems("networks", result.Networks, opts.DryRun)
	printCleanupItems("volumes", result.Volumes, opts.DryRun)
	if result.Empty() {
		fmt.Println("[tengiz] nothing to remove")
	}
}

func printCleanupItems(label string, items []string, dryRun bool) {
	if len(items) == 0 {
		return
	}
	verb := "removed"
	if dryRun {
		verb = "would be removed"
	}
	fmt.Printf("[tengiz] %d %s %s: %s\n", len(items), label, verb, strings.Join(items, ", "))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestRunCleanup" -v -count=1`
Expected: PASS (all tests, including `TestRunCleanupDryRunResult` which asserts the build-cache line `1 build cache would be removed: (all)`)

- [ ] **Step 5: Verify the whole project builds and full test suite passes**

Run: `go build -o tengiz . && go vet ./... && go test ./... -count=1`
Expected: build succeeds, `go vet` clean, all tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 5: Documentation and feature tracking

**Files:**
- Modify: `README.md` (insert new section after the `### tengiz rm <app>` section, before `### tengiz rollback <app>`)
- Modify: `AGENTS.md` (CLI command list)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 implemented)

**Interfaces:**
- Consumes: the public `tengiz cleanup` CLI surface from Task 4
- Produces: no code — documentation

- [ ] **Step 1: Document the command in README.md**

Insert after the `tengiz rm <app>` section (after line 228, before `### tengiz rollback <app>`):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to free disk space on the host. Resources managed by Tengiz (labeled `tengiz-app=<name>`) are never removed; stopped containers not managed by Tengiz, dangling images, old deployment images beyond the retention window, unused networks, and build cache are cleaned.

With no category flags, runs the safe default set. Volumes are only removed when `--volumes` is passed explicitly.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling images and old deployment images |
| `--networks` | Remove unused networks |
| `--volumes` | Remove unused volumes (explicit opt-in, never in the default set) |
| `--build-cache` | Remove build cache |
| `--keep-images N` | Number of most recent deployment images to keep per app (default: 5) |
| `--dry-run` | Show what would be removed without removing anything |
| `--force` | Skip the confirmation prompt |

Examples:
```
tengiz cleanup --dry-run
tengiz cleanup
tengiz cleanup --volumes --force
```
```

- [ ] **Step 2: Add the command to AGENTS.md**

Insert after the `tengiz stop/start/rm  → lifecycle` line (line 50):

```markdown
tengiz cleanup [--containers|--images|--networks|--volumes|--build-cache] [--dry-run] [--force] → prune unused Docker resources
```

- [ ] **Step 3: Mark feature #6 as implemented in docs/FUTURES_FEATURES.md**

Change the P0 table row (line 19):

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the `✅ Implemented Features (Not Pending)` table (after the `Webhook ile Otomatik Deploy` row, line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-18) |
```

Add a status line to the `## Docker Housekeeping (Otomatik Temizlik)` detailed section (after the `- **Detected:** 2026-07-14` line):

```markdown
- **Status:** ✅ Implemented (2026-08-18)
```

- [ ] **Step 4: Verify no code changed and docs are consistent**

Run: `git status --short`
Expected: only `README.md`, `AGENTS.md`, `docs/FUTURES_FEATURES.md` modified (no `.go` files)

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

### Task 6: End-to-end manual verification (requires Docker)

**Files:**
- No code changes — manual verification only

**Interfaces:**
- Consumes: full feature from Tasks 1-5

- [ ] **Step 1: Build and smoke-test the CLI surface**

Run: `go build -o tengiz .`
Run: `./tengiz cleanup --help`
Expected: prints usage with all eight flags (`--containers`, `--images`, `--networks`, `--volumes`, `--build-cache`, `--keep-images`, `--dry-run`, `--force`)

- [ ] **Step 2: Dry-run against a live Docker daemon**

Run: `./tengiz cleanup --dry-run`
Expected: prints `[tengiz] dry-run: no changes made` followed by the would-be-removed items (or `[tengiz] nothing to remove`), and **no** Docker resources are actually removed

- [ ] **Step 3: Real cleanup with force**

Run: `./tengiz cleanup --force`
Expected: prints `[tengiz] cleanup complete` with per-category counts; Tengiz-managed containers (e.g. `tengiz-<app>`) and their images remain intact

- [ ] **Step 4: Full project verification**

Run: `go vet ./... && go test ./... -count=1`
Expected: `go vet` clean, all tests pass

- [ ] **Step 5: Commit any verification fixes**

If the manual run revealed a bug, fix it with a test (TDD) and commit:

```bash
git add internal/ ...
git commit -m "fix: <describe the issue found during manual verification>"
```

---

## Self-Review

**1. Spec coverage (docs/FUTURES_FEATURES.md #6 — Docker Housekeeping):**
- Label-based prune protecting Tengiz-managed resources → Task 2 arg builders always use `--filter label!=tengiz-app`; Task 3 wires them into `Prune`; documented in README (Task 5)
- `tengiz cleanup` command → Task 4
- Stale stopped containers, dangling images, old deployment images, unused networks, volumes (opt-in), build cache → Task 2/3 cover every category
- Disk-space motivation → CLI `Long` help and README section (Tasks 4-5)
- No gaps found.

**2. Placeholder scan:** All steps contain complete code and exact commands with expected output. No "TBD"/"TODO"/"add error handling"/"similar to Task N" placeholders. The three runtime mock files (`idle`, `proxy`, `cli/root_test.go`) and `stubManager` are deliberately untouched — noted in Global Constraints and File Structure.

**3. Type consistency:**
- `CleanupOptions`/`CleanupResult` fields match across Tasks 1, 3, 4 (`Containers`, `Images`, `Networks`, `Volumes`, `BuildCache`, `KeepImages`, `DryRun`)
- `CleanupResult.Empty()` used in `printCleanupResult` (Task 4) and tested in Task 1
- `oldImageTags(lines, keepN) []string` signature identical in Tasks 2 and 3
- `runCleanup(cmd *cobra.Command, hk runtime.Housekeeper) error` matches between Task 4 implementation and tests
- `pruneContainersArgs`/`pruneDanglingImagesArgs`/`pruneNetworksArgs`/`pruneVolumesArgs(dryRun bool) []string` names consistent between Task 2 (definition) and Task 3 (consumption)
- Test names referenced in run commands match the exact function names in each task