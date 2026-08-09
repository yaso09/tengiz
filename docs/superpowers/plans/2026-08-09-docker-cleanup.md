# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, networks, volumes, build cache) using label-based filtering so Tengiz-managed containers are always preserved.

**Architecture:** Extend the `runtime.Manager` interface with a single `Prune(ctx, opts)` method implemented on the exec-based `dockerRuntime`. Containers are protected by label: only exited containers *without* a `tengiz-app` label are removed (Tengiz containers stop for scale-to-zero and must never be pruned). Images, networks, volumes, and build cache are pruned via the corresponding `docker <object> prune` subcommands; image pruning is dangling-only by default with a 24h grace window under `--all`. Pure argument/parsing helpers are unit-tested; the docker CLI stays at the edge via `os/exec`.

**Tech Stack:** Go 1.26 (single module `github.com/yaso09/tengiz`), Cobra (CLI), the `docker` CLI invoked via `os/exec` — no Docker SDK. No new dependencies.

## Global Constraints

These requirements are from `docs/FUTURES_FEATURES.md` (P0 #6) and AGENTS.md, and apply to every task:

- Feature: "Docker Housekeeping" — "Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`."
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur. `tengiz cleanup` komutu eklenebilir." — Tengiz-managed containers MUST be preserved by cleanup.
- No Docker SDK — runtime calls the `docker` CLI via `os/exec`; Docker must be installed separately.
- Container label key is exactly `tengiz-app` (`const labelKey = "tengiz-app"` in `internal/runtime/docker.go:76`).
- Built images are tagged `tengiz-apps/<app>:{env}-{deploymentID}` and `{env}-latest` (from `internal/builder`) and carry **no** labels — they are always tagged, so they are never "dangling" and must never be harmlessly pruned.
- `--all` uses a 24h grace window so recent images survive for rollback (filter `until=24h`).
- New feature work creates a `feat/<name>` branch (`git checkout -b feat/docker-cleanup`).
- Every change ships tests, tests pass, then commit; README.md/docs updated when command surface changes.
- Must pass `go build -o tengiz .`, `go vet ./...`, `go test ./... -v -count=1`.

---

## File Structure

| File | Responsibility | Change |
|------|---------------|--------|
| `internal/runtime/runtime.go` | `PruneCategory`, `PruneOptions`, `PruneSummary`, `PruneResult` types; add `Prune` to `Manager`; `stubManager.Prune` no-op | Modify |
| `internal/runtime/cleanup.go` | Pure helpers `resolvePruneCategories`, `pruneImageArgs`/`pruneNetworkArgs`/`pruneVolumeArgs`/`pruneCacheArgs`, `parseNonTengizExitedContainers`; exec-based `dockerRuntime.Prune`, `runDocker`, `pruneExitedContainers` | Modify |
| `internal/runtime/cleanup_test.go` | Stub test + pure-helper unit tests | Modify |
| `internal/cli/cleanup.go` | `tengiz cleanup` Cobra command + `parseCleanupObjects` flag parser | Create |
| `internal/cli/cleanup_test.go` | Command registration/flag tests + parser tests | Create |
| `internal/proxy/proxy_test.go` | `mockRuntime.Prune` no-op (interface compliance) | Modify |
| `internal/idle/idle_test.go` | `mockRuntime.Prune` no-op (interface compliance) | Modify |
| `internal/cli/root_test.go` | `mockRTForDeploy.Prune` no-op (interface compliance) | Modify |
| `README.md` | `tengiz cleanup` in Features list + CLI Reference | Modify |
| `docs/FUTURES_FEATURES.md` | Mark P0 #6 as implemented | Modify |

No new external dependencies. Every task leaves the module compilable and green.

---

### Task 1: Prune types + pure argument/parsing helpers (TDD, no interface change yet)

**Files:**
- Modify: `internal/runtime/runtime.go` — add types after the `RunOptions` struct (after line 29)
- Modify: `internal/runtime/cleanup.go` — add 5 pure helpers (imports already present: `context`, `fmt`, `os/exec`, `strings`)
- Modify: `internal/runtime/cleanup_test.go` — add helper tests

**Interfaces:**
- Consumes: nothing new
- Produces (used by Tasks 2–4):
  - `type PruneCategory string`; constants `PruneContainers = "containers"`, `PruneImages = "images"`, `PruneNetworks = "networks"`, `PruneVolumes = "volumes"`, `PruneCache = "cache"`
  - `var PruneCategories = []PruneCategory{PruneContainers, PruneImages, PruneNetworks, PruneVolumes, PruneCache}` (canonical execution order)
  - `type PruneOptions struct { All bool; Categories []PruneCategory }`
  - `type PruneSummary struct { Category PruneCategory; Output string }`
  - `type PruneResult struct { Summaries []PruneSummary }`
  - `func resolvePruneCategories(requested []PruneCategory) []PruneCategory`
  - `func pruneImageArgs(all bool) []string`
  - `func pruneNetworkArgs() []string`
  - `func pruneVolumeArgs() []string`
  - `func pruneCacheArgs() []string`
  - `func parseNonTengizExitedContainers(out string) []string`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-cleanup
```

- [ ] **Step 2: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestResolvePruneCategories(t *testing.T) {
	tests := []struct {
		name      string
		requested []PruneCategory
		expected  []PruneCategory
	}{
		{"empty means all", nil, PruneCategories},
		{"subset ordered canonically", []PruneCategory{PruneVolumes, PruneImages}, []PruneCategory{PruneImages, PruneVolumes}},
		{"unknown dropped, known preserved", []PruneCategory{"bogus", PruneCache}, []PruneCategory{PruneCache}},
		{"duplicates deduped by canonical order", []PruneCategory{PruneImages, PruneImages, PruneNetworks}, []PruneCategory{PruneImages, PruneNetworks}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePruneCategories(tt.requested)
			if len(got) != len(tt.expected) {
				t.Fatalf("len = %d, want %d (got %v)", len(got), len(tt.expected), got)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("got %v, want %v", got, tt.expected)
				}
			}
		})
	}
}

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{"images default", pruneImageArgs(false), []string{"image", "prune", "-f"}},
		{"images all", pruneImageArgs(true), []string{"image", "prune", "-f", "-a", "--filter", "until=24h"}},
		{"networks", pruneNetworkArgs(), []string{"network", "prune", "-f"}},
		{"volumes", pruneVolumeArgs(), []string{"volume", "prune", "-f"}},
		{"cache", pruneCacheArgs(), []string{"builder", "prune", "-f"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (got %v)", len(tt.got), len(tt.want), tt.got)
			}
			for i := range tt.got {
				if tt.got[i] != tt.want[i] {
					t.Fatalf("arg[%d] = %q, want %q", i, tt.got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseNonTengizExitedContainers(t *testing.T) {
	block := "abc123|myapp\n" +
		"def456|<no value>\n" +
		"ghi789|\n" +
		"jkl012|otherapp-staging\n"
	got := parseNonTengizExitedContainers(block)
	if len(got) != 2 || got[0] != "def456" || got[1] != "ghi789" {
		t.Fatalf("got %v, want [def456 ghi789]", got)
	}
}

func TestParseNonTengizExitedContainersEmpty(t *testing.T) {
	if got := parseNonTengizExitedContainers(""); len(got) != 0 {
		t.Fatalf("expected empty result, got %v", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestResolvePruneCategories|TestPruneArgs|TestParseNonTengizExitedContainers' -v -count=1`
Expected: FAIL — compile errors: `undefined: resolvePruneCategories`, `undefined: pruneImageArgs`, etc.

- [ ] **Step 4: Add the types**

In `internal/runtime/runtime.go`, immediately after the `RunOptions` struct (after line 29):

```go
type PruneCategory string

const (
	PruneContainers PruneCategory = "containers"
	PruneImages     PruneCategory = "images"
	PruneNetworks   PruneCategory = "networks"
	PruneVolumes    PruneCategory = "volumes"
	PruneCache      PruneCategory = "cache"
)

// PruneCategories is the canonical cleanup execution order.
var PruneCategories = []PruneCategory{PruneContainers, PruneImages, PruneNetworks, PruneVolumes, PruneCache}

type PruneOptions struct {
	// All also removes unused images (not referenced by any container), not just dangling images.
	// See pruneImageArgs in cleanup.go for the 24h grace window applied in this mode.
	All bool
	// Categories restricts the cleanup to these categories; empty means all (PruneCategories order).
	Categories []PruneCategory
}

type PruneSummary struct {
	// Category is the pruned object name (e.g. "containers").
	Category PruneCategory
	// Output is the trimmed raw docker output for this category; empty means nothing removed.
	Output string
}

type PruneResult struct {
	Summaries []PruneSummary
}
```

- [ ] **Step 5: Add the pure helpers**

Add to `internal/runtime/cleanup.go`:

```go
func resolvePruneCategories(requested []PruneCategory) []PruneCategory {
	if len(requested) == 0 {
		return PruneCategories
	}
	var result []PruneCategory
	for _, c := range PruneCategories {
		for _, want := range requested {
			if c == want {
				result = append(result, c)
			}
		}
	}
	return result
}

func pruneImageArgs(all bool) []string {
	args := []string{"image", "prune", "-f"}
	if all {
		args = append(args, "-a", "--filter", "until=24h")
	}
	return args
}

func pruneNetworkArgs() []string { return []string{"network", "prune", "-f"} }
func pruneVolumeArgs() []string  { return []string{"volume", "prune", "-f"} }
func pruneCacheArgs() []string   { return []string{"builder", "prune", "-f"} }

// parseNonTengizExitContainers extracts container IDs whose tengiz-app label is absent
// (<no value> or empty) from the output of:
//
//	docker ps -a --filter status=exited --format '{{.ID}}|{{.Label "tengiz-app"}}'
func parseNonTengizExitedContainers(out string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		label := parts[1]
		if label != "" && label != "<no value>" {
			continue
		}
		ids = append(ids, parts[0])
	}
	return ids
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestResolvePruneCategories|TestPruneArgs|TestParseNonTengizExitedContainers' -v -count=1`
Expected: PASS (all subtests), package still compiles.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add prune types and docker cleanup arg helpers"
```

---

### Task 2: Ship `Manager.Prune` + stub + mocks + exec backend

**Files:**
- Modify: `internal/runtime/runtime.go` — add `Prune` to `Manager` (after line 36) + `stubManager.Prune` (after line 119)
- Modify: `internal/runtime/cleanup.go` — add `runDocker`, `pruneExitedContainers`, `dockerRuntime.Prune`
- Modify: `internal/runtime/cleanup_test.go` — add `TestStubPrune`, `TestPruneUnknownCategoriesNoDocker`
- Modify: `internal/proxy/proxy_test.go:35` — `mockRuntime.Prune`
- Modify: `internal/idle/idle_test.go:34` — `mockRuntime.Prune`
- Modify: `internal/cli/root_test.go:100` — `mockRTForDeploy.Prune`

**Interfaces:**
- Consumes: Task 1 types + helpers
- Produces: `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`; the exec backend implementation that Tasks 3–4 call

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(res.Summaries) != 0 {
		t.Fatalf("expected 0 summaries, got %d", len(res.Summaries))
	}
}

func TestPruneUnknownCategoriesNoDocker(t *testing.T) {
	// Unknown categories resolve to an empty set, so Prune returns
	// success with zero summaries and never calls the docker CLI.
	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), PruneOptions{Categories: []PruneCategory{"bogus"}})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(res.Summaries) != 0 {
		t.Fatalf("expected 0 summaries, got %d", len(res.Summaries))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestStubPrune|TestPruneUnknownCategoriesNoDocker' -v -count=1`
Expected: FAIL — `m.Prune undefined` / `r.Prune undefined` (not yet on the interface or struct)

- [ ] **Step 3: Add the interface method + stub no-op**

In `internal/runtime/runtime.go`, add to the `Manager` interface after the `KeepLastNImages` line (line 36):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

And after `stubManager.KeepLastNImages` (after line 119):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}
```

- [ ] **Step 4: Implement the exec backend**

Append to `internal/runtime/cleanup.go`:

```go
func runDocker(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func pruneExitedContainers(ctx context.Context) (string, error) {
	out, err := runDocker(ctx, []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--format", `{{.ID}}|{{.Label "tengiz-app"}}`,
	})
	if err != nil {
		return "", err
	}
	ids := parseNonTengizExitedContainers(out)
	if len(ids) == 0 {
		return "", nil
	}
	for _, id := range ids {
		if _, rmErr := runDocker(ctx, []string{"rm", "-f", id}); rmErr != nil {
			return "", rmErr
		}
	}
	return fmt.Sprintf("removed %d exited non-tengiz containers", len(ids)), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var result PruneResult
	for _, cat := range resolvePruneCategories(opts.Categories) {
		var out string
		var err error
		switch cat {
		case PruneContainers:
			out, err = pruneExitedContainers(ctx)
		case PruneImages:
			out, err = runDocker(ctx, pruneImageArgs(opts.All))
		case PruneNetworks:
			out, err = runDocker(ctx, pruneNetworkArgs())
		case PruneVolumes:
			out, err = runDocker(ctx, pruneVolumeArgs())
		case PruneCache:
			out, err = runDocker(ctx, pruneCacheArgs())
		}
		if err != nil {
			return result, fmt.Errorf("cleanup %s: %w", cat, err)
		}
		result.Summaries = append(result.Summaries, PruneSummary{Category: cat, Output: strings.TrimSpace(out)})
	}
	return result, nil
}
```

- [ ] **Step 5: Fix the other `Manager` implementations so the module compiles**

`internal/proxy/proxy_test.go` — after line 34 (`KeepLastNImages`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

`internal/idle/idle_test.go` — after line 33 (`KeepLastNImages`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

`internal/cli/root_test.go` — after line 99 (`KeepLastNImages`):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -v -count=1 && go vet ./...`
Expected: PASS; `go vet` clean (all packages including mocked test files compile).

- [ ] **Step 7: Full module verification**

Run: `go build -o /tmp/tengiz-build . && go test ./... -v -count=1`
Expected: build succeeds; all packages PASS.

- [ ] **Step 8: Manual smoke check (optional, needs Docker)**

Run: `go build -o tengiz . && ./tengiz cleanup`
Expected: one line per category (`nothing to remove` when idle); containers labeled `tengiz-app` stay untouched. Not a CI gate.

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: implement docker cleanup prune in runtime"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: Task 2 `runtime.NewDocker().Prune`, Task 1 `runtime.PruneOptions`/`PruneCategory`/`PruneResult`
- Produces: registered Cobra command `tengiz cleanup` with flags `--all`, `--objects`; helper `parseCleanupObjects(values []string) ([]runtime.PruneCategory, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd.Use != "cleanup" {
		t.Fatalf("expected cleanup command, got %s", cmd.Use)
	}
	if cmd.Flags().Lookup("all") == nil {
		t.Error("cleanup missing --all flag")
	}
	if cmd.Flags().Lookup("objects") == nil {
		t.Error("cleanup missing --objects flag")
	}
}

func TestParseCleanupObjects(t *testing.T) {
	cats, err := parseCleanupObjects([]string{"containers", "IMAGES"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 || cats[0] != runtime.PruneContainers || cats[1] != runtime.PruneImages {
		t.Fatalf("got %v, want [containers images]", cats)
	}
}

func TestParseCleanupObjectsEmpty(t *testing.T) {
	cats, err := parseCleanupObjects(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 0 {
		t.Fatalf("expected no categories, got %v", cats)
	}
}

func TestParseCleanupObjectsUnknown(t *testing.T) {
	if _, err := parseCleanupObjects([]string{"containers", "bogus"}); err == nil {
		t.Fatal("expected error for unknown object")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanupCommandRegistered|TestParseCleanupObjects' -v -count=1`
Expected: FAIL — `cleanup command not found`; `undefined: parseCleanupObjects`

- [ ] **Step 3: Implement the command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Cleans leftover Docker resources: exited non-tengiz containers, dangling images,
unused networks and volumes, and build cache. Tengiz-managed containers are always preserved.

Examples:
  tengiz cleanup                       # all categories
  tengiz cleanup --objects images      # only images (dangling)
  tengiz cleanup --all                 # also remove unused images (24h grace window)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		all, _ := cmd.Flags().GetBool("all")
		objects, _ := cmd.Flags().GetStringSlice("objects")
		cats, err := parseCleanupObjects(objects)
		if err != nil {
			return err
		}
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		result, err := rt.Prune(cmd.Context(), runtime.PruneOptions{All: all, Categories: cats})
		if err != nil {
			return err
		}
		for _, s := range result.Summaries {
			line := "nothing to remove"
			if s.Output != "" {
				line = s.Output
			}
			fmt.Printf("[tengiz] cleanup %s: %s\n", s.Category, line)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("all", false, "also remove unused images not referenced by any container (24h grace window)")
	cleanupCmd.Flags().StringSlice("objects", nil, "only run these categories: containers, images, networks, volumes, cache")
}

func parseCleanupObjects(values []string) ([]runtime.PruneCategory, error) {
	if len(values) == 0 {
		return nil, nil
	}
	var cats []runtime.PruneCategory
	for _, v := range values {
		c := runtime.PruneCategory(strings.ToLower(strings.TrimSpace(v)))
		switch c {
		case runtime.PruneContainers, runtime.PruneImages, runtime.PruneNetworks, runtime.PruneVolumes, runtime.PruneCache:
			cats = append(cats, c)
		default:
			return nil, fmt.Errorf("unknown cleanup object %q (valid: containers, images, networks, volumes, cache)", v)
		}
	}
	return cats, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanupCommandRegistered|TestParseCleanupObjects' -v -count=1`
Expected: PASS

- [ ] **Step 5: Full module verification**

Run: `go build -o /tmp/tengiz-build . && go vet ./... && go test ./... -v -count=1`
Expected: all build, vet clean, all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Documentation + mark feature implemented

**Files:**
- Modify: `README.md` — feature bullet + CLI Reference subsection
- Modify: `docs/FUTURES_FEATURES.md` — P0 #6 row marked implemented

- [ ] **Step 1: Add a Features bullet**

In `README.md` `## Features` (after the "Deployment history" bullet, ~line 20) add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, images, networks, volumes, and build cache while always preserving Tengiz-managed containers.
```

- [ ] **Step 2: Add the CLI Reference section**

In `README.md` after the `tengiz rollback` section (after line 236) add:

```markdown
### `tengiz cleanup`

Remove unused Docker resources: exited containers that Tengiz does not manage, dangling images, unused networks and volumes, and the BuildKit cache.

| Flag | Description |
|------|-------------|
| `--all` | Also remove unused images (not referenced by any container), with a 24h grace window. |
| `--objects <list>` | Only run these categories: `containers`, `images`, `networks`, `volumes`, `cache` (comma-separated, repeatable). |

Tengiz-managed containers (labeled `tengiz-app`, including stopped scale-to-zero containers) are never removed. Non-Tengiz exited containers are removed. Dangling images are always pruned; `--all` additionally prunes unused tagged images older than 24 hours.

Examples:

```bash
# Clean everything (safe)
tengiz cleanup

# Only clean images, including unused ones
tengiz cleanup --objects images --all
```
```

- [ ] **Step 3: Mark P0 #6 implemented**

In `docs/FUTURES_FEATURES.md` change the P0 #6 row (line 19) status marker from ⬜ to ✅:

```markdown
| 6 | **Docker Housekeeping** ✅ Implemented (2026-08-09) | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

(Change the cell `**Docker Housekeeping** ⬜` to `**Docker Housekeeping** ✅ Implemented (2026-08-09)`; keep Impact/Effort/Alignment cells and rationale identical.)

- [ ] **Step 4: Final full verification**

Run: `go build -o /tmp/tengiz-build . && go vet ./... && go test ./... -v -count=1`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage.** P0 #6 requirements mapped to tasks: disk-reclaiming cleanup of containers/images/networks/volumes/build cache (Task 2), the `tengiz cleanup` command (Task 3), label-based protection of Tengiz containers (Task 1 parser + Task 2 container path), and expectations of AGENTS.md — feature branch (Task 1 Step 1), README update (Task 4), tests + commit per task. Every spec requirement has a task; no gaps.

**2. Placeholder scan.** No "TBD"/"TODO"/"implement later"; all code is inline. The Task 2 smoke step is an explicit manual check, not an in-suite placeholder. No task references a `FunctionName` it does not define.

**3. Type consistency.** The types `PruneCategory`/`PruneCategories`/`PruneOptions`/`PruneSummary`/`PruneResult` and the signatures `resolvePruneCategories`, `pruneImageArgs`, `parseNonTengizExitedContainers`, `Prune(ctx, PruneOptions) (PruneResult, error)` are defined once (Tasks 1–2) and used identically in later tasks. `dockerRuntime.Prune` (Task 2) is the only call site of the helpers. `parseCleanupObjects` is defined and consumed only within Task 3. Label key `tengiz-app` matches `labelKey` in `docker.go:76`.

**4. Build gates.** Task 1 leaves the module compile-clean (types + helpers + their tests). Task 2 adds the interface + all implementors (stub, dockerRuntime, 3 mocks) in one gate, so `go build ./...` and `go test ./...` stay green after every commit. Task 3 stays CLI-only; Task 4 docs only.