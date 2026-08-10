# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes stopped non-Tengiz containers, dangling images, unused volumes, unused networks, and the Docker build cache to reclaim disk space — the #1 production issue on single-server deployments — while never touching Tengiz-managed containers.

**Architecture:** Five new prune primitives are added to the `runtime.Manager` interface (exec-based implementations on `dockerRuntime`, no-ops on `stubManager`). A new `internal/cleanup` package orchestrates them: `cleanup.Manager` consumes a narrow `Pruner` interface (the Go pattern already used by `proxy` for its idle manager), applies an `Options` set, and returns a `Report` with per-category counts and a `docker system df` disk-usage snapshot. The Cobra command `tengiz cleanup` maps flags → `cleanup.Options`. Label-based protection is the safety guarantee: every prune path for containers uses `--filter label!=tengiz-app`, so scale-to-zero stopped apps are never deleted. Volumes/networks/build-cache are opt-in because they can hold data.

**Tech Stack:** Go 1.26 standard library only (no new dependencies), Cobra CLI, existing `runtime.Manager` interface, `os/exec` → `docker` CLI (matches the whole codebase's Docker approach).

## Global Constraints

- Work happens on a new branch: `git checkout -b feat/cleanup`
- Default environment is `"production"` — `cleanup` is env-agnostic (Docker labels are env-scoped already); no `--env` behavior change
- Tengiz-managed containers (label `tengiz-app=<name>`) MUST NEVER be pruned — every container prune path uses `--filter label!=tengiz-app`
- Default `tengiz cleanup` (no flags) prunes only stopped containers + dangling images; volumes, networks, and build cache are explicit opt-in flags
- `--dry-run` lists candidates without deleting; build-cache dry-run reports reclaimable bytes from `docker system df`
- No new external dependencies
- Prune errors are collected per-category and reported as warnings; one category failing never blocks the others
- All docker commands go through `os/exec` with `exec.CommandContext(ctx, "docker", ...)`
- Image reference format stays `tengiz-apps/<app>:<env>-<deploymentID>` and `tengiz-apps/<app>:<env>-latest` (unchanged)
- Commands to verify: `go build -o tengiz .`, `go test ./... -v -count=1`, `go vet ./...`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` (create) | Exec-based prune primitives + pure parsers: `PruneStoppedContainers`, `PruneDanglingImages`, `PruneUnusedVolumes`, `PruneUnusedNetworks`, `PruneBuildCache`, `DiskUsage`, `PruneOptions`, `DiskUsageRow` |
| `internal/runtime/runtime.go` (modify) | Add 6 methods to `Manager` interface + no-op implementations on `stubManager` |
| `internal/runtime/prune_test.go` (create) | Table-driven tests for all pure parser helpers |
| `internal/proxy/proxy_test.go` (modify) | Add 6 no-op methods to `mockRuntime` so it still satisfies `Manager` |
| `internal/idle/idle_test.go` (modify) | Add 6 no-op methods to `mockRuntime` so it still satisfies `Manager` |
| `internal/cli/root_test.go` (modify) | Add 6 no-op methods to `mockRTForDeploy` so it still satisfies `Manager` |
| `internal/cleanup/cleanup.go` (create) | Orchestration: `Options`, `Report`, `Pruner` interface, `Manager`, `Cleanup()`, `String()`, `humanSize()` |
| `internal/cleanup/cleanup_test.go` (create) | Mock `Pruner` + tests for aggregation, dry-run passthrough, error collection, formatting |
| `internal/cli/cleanup.go` (create) | `cleanupCmd` cobra command + `cleanupOptionsFromFlags()` helper |
| `internal/cli/cleanup_test.go` (create) | Command registration, flag registration, flag→Options mapping tests |
| `internal/cli/root.go` (modify) | `rootCmd.AddCommand(cleanupCmd)` + flag registration in `init()` |
| `README.md` (modify) | Add `tengiz cleanup` to CLI Reference |
| `docs/FUTURES_FEATURES.md` (modify) | Mark feature #6 Docker Housekeeping as ✅ Implemented |
| `AGENTS.md` (modify) | Add `tengiz cleanup` line to CLI section |

---

### Task 1: Runtime prune primitives

**Files:**
- Create: `internal/runtime/prune.go`
- Create: `internal/runtime/prune_test.go`
- Modify: `internal/runtime/runtime.go` (interface + stub)
- Modify: `internal/proxy/proxy_test.go`
- Modify: `internal/idle/idle_test.go`
- Modify: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: nothing new (pure package-level helpers + existing `dockerRuntime`)
- Produces:
  - `type PruneOptions struct { DryRun bool }`
  - `type DiskUsageRow struct { Type string; TotalCount int; Active int; Size string; Reclaimable string }`
  - `Manager` additions (must be implemented by `dockerRuntime` and `stubManager`):
    - `PruneStoppedContainers(ctx context.Context, opts PruneOptions) (int, error)` — returns count of removed (or, in dry-run, removable) stopped containers
    - `PruneDanglingImages(ctx context.Context, opts PruneOptions) (int, error)`
    - `PruneUnusedVolumes(ctx context.Context, opts PruneOptions) (int, error)`
    - `PruneUnusedNetworks(ctx context.Context, opts PruneOptions) (int, error)`
    - `PruneBuildCache(ctx context.Context, opts PruneOptions) (int64, error)` — returns freed bytes
    - `DiskUsage(ctx context.Context) ([]DiskUsageRow, error)`
  - Package-level parsers used by later tasks' tests: `splitLines(out string) []string`, `countPruneItems(out string) int`, `countImagePrune(out string) int`, `parseReclaimedSpace(out string) int64`, `parseSize(s string) int64`, `parseInt(s string) int`

- [ ] **Step 1: Create the branch**

```bash
git checkout -b feat/cleanup
```

- [ ] **Step 2: Write the failing parser tests**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import "testing"

func TestSplitLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"\n\n", 0},
		{"abc\ndef\n", 2},
		{"abc\n\ndef", 2},
	}
	for _, c := range cases {
		if got := len(splitLines(c.in)); got != c.want {
			t.Errorf("splitLines(%q) = %d lines, want %d", c.in, got, c.want)
		}
	}
}

func TestCountPruneItems(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"only total", "Total reclaimed space: 0B", 0},
		{"header only", "Deleted Containers:", 0},
		{"container ids", "abc123\ndef456\nTotal reclaimed space: 12.5kB", 2},
		{"volume names with header", "Deleted Volumes:\nmyvol1\nmyvol2\n\nTotal reclaimed space: 5B", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := countPruneItems(c.in); got != c.want {
				t.Errorf("countPruneItems(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestCountImagePrune(t *testing.T) {
	in := "Deleted Images:\nuntagged: foo:latest\ndeleted: sha256:abc\ndeleted: sha256:def\n\nTotal reclaimed space: 2MB"
	if got := countImagePrune(in); got != 2 {
		t.Errorf("countImagePrune = %d, want 2", got)
	}
	if got := countImagePrune(""); got != 0 {
		t.Errorf("countImagePrune empty = %d, want 0", got)
	}
}

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0B", 0},
		{"512B", 512},
		{"12.5kB", 12800},
		{"800MB", 800 << 20},
		{"800MB (66%)", 800 << 20},
		{"1.2GB", int64(1.2 * (1 << 30))},
		{"junk", 0},
	}
	for _, c := range cases {
		if got := parseSize(c.in); got != c.want {
			t.Errorf("parseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	in := "ID   RECLAIMABLE\nabc  1.5MB\n\nTotal reclaimed space: 1.5MB"
	if got := parseReclaimedSpace(in); got != int64(1.5*(1<<20)) {
		t.Errorf("parseReclaimedSpace = %d, want %d", got, int64(1.5*(1<<20)))
	}
	if got := parseReclaimedSpace("no matches"); got != 0 {
		t.Errorf("parseReclaimedSpace(no matches) = %d, want 0", got)
	}
}

func TestParseInt(t *testing.T) {
	if got := parseInt("5"); got != 5 {
		t.Errorf("parseInt(5) = %d, want 5", got)
	}
	if got := parseInt(" 3 "); got != 3 {
		t.Errorf("parseInt(' 3 ') = %d, want 3", got)
	}
	if got := parseInt("x"); got != 0 {
		t.Errorf("parseInt(x) = %d, want 0", got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestSplitLines|TestCountPruneItems|TestCountImagePrune|TestParseSize|TestParseReclaimedSpace|TestParseInt' -v -count=1`

Expected: FAIL — each test fails to compile with `undefined: splitLines` (and friends).

- [ ] **Step 4: Implement the pure parsers + prune primitives**

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

type PruneOptions struct {
	DryRun bool
}

type DiskUsageRow struct {
	Type        string
	TotalCount  int
	Active      int
	Size        string
	Reclaimable string
}

func (r *dockerRuntime) listStoppedContainers(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-aq",
		"--filter", "status=exited",
		"--filter", "label!=tengiz-app")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return splitLines(string(out)), nil
}

func (r *dockerRuntime) PruneStoppedContainers(ctx context.Context, opts PruneOptions) (int, error) {
	if opts.DryRun {
		ids, err := r.listStoppedContainers(ctx)
		if err != nil {
			return 0, err
		}
		return len(ids), nil
	}
	cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
		"--filter", "label!=tengiz-app")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return countPruneItems(string(out)), nil
}

func (r *dockerRuntime) PruneDanglingImages(ctx context.Context, opts PruneOptions) (int, error) {
	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "images", "-q", "--filter", "dangling=true")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return 0, fmt.Errorf("docker images: %w\n%s", err, string(out))
		}
		return len(splitLines(string(out))), nil
	}
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return countImagePrune(string(out)), nil
}

func (r *dockerRuntime) PruneUnusedVolumes(ctx context.Context, opts PruneOptions) (int, error) {
	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q", "--filter", "dangling=true")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return 0, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
		}
		return len(splitLines(string(out))), nil
	}
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return countPruneItems(string(out)), nil
}

func (r *dockerRuntime) listNetworkCount(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "ls", "-q")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	n := 0
	for _, name := range splitLines(string(out)) {
		switch name {
		case "bridge", "host", "none":
			continue
		}
		n++
	}
	return n, nil
}

func (r *dockerRuntime) PruneUnusedNetworks(ctx context.Context, opts PruneOptions) (int, error) {
	if opts.DryRun {
		return r.listNetworkCount(ctx)
	}
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return countPruneItems(string(out)), nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context, opts PruneOptions) (int64, error) {
	if opts.DryRun {
		return r.buildCacheReclaimable(ctx)
	}
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parseReclaimedSpace(string(out)), nil
}

func (r *dockerRuntime) buildCacheReclaimable(ctx context.Context) (int64, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{.Type}}|{{.Reclaimable}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	for _, line := range splitLines(string(out)) {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "Build Cache" {
			return parseSize(parts[1]), nil
		}
	}
	return 0, nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) ([]DiskUsageRow, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format",
		"{{.Type}}|{{.TotalCount}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	var rows []DiskUsageRow
	for _, line := range splitLines(string(out)) {
		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 5 {
			continue
		}
		rows = append(rows, DiskUsageRow{
			Type:        strings.TrimSpace(parts[0]),
			TotalCount:  parseInt(parts[1]),
			Active:      parseInt(parts[2]),
			Size:        parts[3],
			Reclaimable: parts[4],
		})
	}
	return rows, nil
}

func splitLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}

func countPruneItems(out string) int {
	n := 0
	for _, line := range splitLines(out) {
		if strings.HasPrefix(line, "Total reclaimed space:") || strings.HasPrefix(line, "Deleted ") {
			continue
		}
		n++
	}
	return n
}

func countImagePrune(out string) int {
	n := 0
	for _, line := range splitLines(out) {
		if strings.HasPrefix(line, "deleted:") {
			n++
		}
	}
	return n
}

func parseReclaimedSpace(out string) int64 {
	for _, line := range splitLines(out) {
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return parseSize(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return 0
}

func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " ("); i != -1 {
		s = s[:i]
	}
	if s == "" || s == "0B" {
		return 0
	}
	var num float64
	var unit string
	if _, err := fmt.Sscanf(s, "%f%s", &num, &unit); err != nil {
		return 0
	}
	var mult int64
	switch unit {
	case "B":
		mult = 1
	case "kB", "KB":
		mult = 1 << 10
	case "MB", "mB":
		mult = 1 << 20
	case "GB", "gB":
		mult = 1 << 30
	case "TB":
		mult = 1 << 40
	default:
		return 0
	}
	return int64(num * float64(mult))
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
```

Note: `splitLines` returns only non-empty trimmed lines. `countPruneItems` skips the `Deleted ...:` section header and the `Total reclaimed space:` trailer that `docker container prune` / `docker volume prune` / `docker network prune` print. `countImagePrune` counts `deleted:` lines (one per removed image). `parseSize` handles the ` (66%)` suffix that `docker system df` adds to reclaimable values.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestSplitLines|TestCountPruneItems|TestCountImagePrune|TestParseSize|TestParseReclaimedSpace|TestParseInt' -v -count=1`

Expected: PASS (all 6 test functions).

- [ ] **Step 6: Extend the Manager interface, stub, and test mocks**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after the `Run` method, line 48):

```go
	PruneStoppedContainers(ctx context.Context, opts PruneOptions) (int, error)
	PruneDanglingImages(ctx context.Context, opts PruneOptions) (int, error)
	PruneUnusedVolumes(ctx context.Context, opts PruneOptions) (int, error)
	PruneUnusedNetworks(ctx context.Context, opts PruneOptions) (int, error)
	PruneBuildCache(ctx context.Context, opts PruneOptions) (int64, error)
	DiskUsage(ctx context.Context) ([]DiskUsageRow, error)
```

In `internal/runtime/runtime.go`, add these no-op methods to `stubManager` (at the end of the file, after the `Run` method):

```go
func (m *stubManager) PruneStoppedContainers(ctx context.Context, opts PruneOptions) (int, error) {
	return 0, nil
}

func (m *stubManager) PruneDanglingImages(ctx context.Context, opts PruneOptions) (int, error) {
	return 0, nil
}

func (m *stubManager) PruneUnusedVolumes(ctx context.Context, opts PruneOptions) (int, error) {
	return 0, nil
}

func (m *stubManager) PruneUnusedNetworks(ctx context.Context, opts PruneOptions) (int, error) {
	return 0, nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context, opts PruneOptions) (int64, error) {
	return 0, nil
}

func (m *stubManager) DiskUsage(ctx context.Context) ([]DiskUsageRow, error) {
	return nil, nil
}
```

Add the same 6 no-op methods to the three test mocks so they still satisfy `Manager`:

`internal/proxy/proxy_test.go` — append after the existing `Run` method (line 35):

```go
func (m *mockRuntime) PruneStoppedContainers(ctx context.Context, opts runtime.PruneOptions) (int, error) { return 0, nil }
func (m *mockRuntime) PruneDanglingImages(ctx context.Context, opts runtime.PruneOptions) (int, error) { return 0, nil }
func (m *mockRuntime) PruneUnusedVolumes(ctx context.Context, opts runtime.PruneOptions) (int, error) { return 0, nil }
func (m *mockRuntime) PruneUnusedNetworks(ctx context.Context, opts runtime.PruneOptions) (int, error) { return 0, nil }
func (m *mockRuntime) PruneBuildCache(ctx context.Context, opts runtime.PruneOptions) (int64, error) { return 0, nil }
func (m *mockRuntime) DiskUsage(ctx context.Context) ([]runtime.DiskUsageRow, error) { return nil, nil }
```

`internal/idle/idle_test.go` — append after the existing `Run` method (line 34), same 6 lines as above.

`internal/cli/root_test.go` — append after the existing `Run` method (line 100), same 6 lines as above.

- [ ] **Step 7: Build and run the full runtime test package**

Run: `go build -o tengiz . && go test ./internal/runtime/ ./internal/proxy/ ./internal/idle/ ./internal/cli/ -v -count=1`

Expected: PASS — everything compiles and all existing tests (including `TestStubSatisfiesInterface` and the three `...ImplementsManager` tests) pass with the interface extension.

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go internal/runtime/runtime.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: add docker prune primitives to runtime manager"
```

---

### Task 2: Cleanup orchestration package

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Create: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.DiskUsageRow` (Task 1); `Pruner` interface defined here
- Produces:
  - `type Options struct { Containers, Images, Volumes, Networks, BuildCache, DryRun bool }` with method `Any() bool`
  - `type Report struct { DryRun bool; ContainersRemoved int; ImagesRemoved int; VolumesRemoved int; NetworksRemoved int; BuildCacheFreed int64; DiskUsage []runtime.DiskUsageRow; Errors []string }` with method `String() string`
  - `type Pruner interface` (6 methods, matches the `Manager` additions)
  - `type Manager struct` + `NewManager(rt Pruner) *Manager` + `(m *Manager) Cleanup(ctx context.Context, opts Options) (*Report, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

type mockPruner struct {
	containersRemoved int
	imagesRemoved     int
	volumesRemoved    int
	networksRemoved   int
	cacheFreed        int64
	usage             []runtime.DiskUsageRow
	usageErr          error
	containersErr     error
	dryRuns           []bool
}

func (m *mockPruner) PruneStoppedContainers(ctx context.Context, opts runtime.PruneOptions) (int, error) {
	m.dryRuns = append(m.dryRuns, opts.DryRun)
	return m.containersRemoved, m.containersErr
}

func (m *mockPruner) PruneDanglingImages(ctx context.Context, opts runtime.PruneOptions) (int, error) {
	m.dryRuns = append(m.dryRuns, opts.DryRun)
	return m.imagesRemoved, nil
}

func (m *mockPruner) PruneUnusedVolumes(ctx context.Context, opts runtime.PruneOptions) (int, error) {
	m.dryRuns = append(m.dryRuns, opts.DryRun)
	return m.volumesRemoved, nil
}

func (m *mockPruner) PruneUnusedNetworks(ctx context.Context, opts runtime.PruneOptions) (int, error) {
	m.dryRuns = append(m.dryRuns, opts.DryRun)
	return m.networksRemoved, nil
}

func (m *mockPruner) PruneBuildCache(ctx context.Context, opts runtime.PruneOptions) (int64, error) {
	m.dryRuns = append(m.dryRuns, opts.DryRun)
	return m.cacheFreed, nil
}

func (m *mockPruner) DiskUsage(ctx context.Context) ([]runtime.DiskUsageRow, error) {
	return m.usage, m.usageErr
}

func TestOptionsAny(t *testing.T) {
	if Options{}.Any() {
		t.Error("empty Options should not be Any")
	}
	if !Options{Containers: true}.Any() {
		t.Error("Containers option should be Any")
	}
}

func TestCleanupAllCategories(t *testing.T) {
	m := &mockPruner{
		containersRemoved: 2,
		imagesRemoved:     4,
		volumesRemoved:    1,
		networksRemoved:   3,
		cacheFreed:        1 << 20,
		usage: []runtime.DiskUsageRow{
			{Type: "Images", TotalCount: 5, Active: 1, Size: "1GB", Reclaimable: "800MB (80%)"},
		},
	}
	mgr := NewManager(m)
	report, err := mgr.Cleanup(context.Background(), Options{
		Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.ContainersRemoved != 2 || report.ImagesRemoved != 4 ||
		report.VolumesRemoved != 1 || report.NetworksRemoved != 3 || report.BuildCacheFreed != 1<<20 {
		t.Errorf("report counts wrong: %+v", report)
	}
	if len(m.dryRuns) != 5 {
		t.Errorf("expected 5 prune calls, got %d", len(m.dryRuns))
	}
	for _, dry := range m.dryRuns {
		if dry {
			t.Error("prune calls should not be dry run")
		}
	}
	if len(report.DiskUsage) != 1 || report.DiskUsage[0].Type != "Images" {
		t.Errorf("disk usage not propagated: %+v", report.DiskUsage)
	}
}

func TestCleanupDryRunPassthrough(t *testing.T) {
	m := &mockPruner{containersRemoved: 7}
	mgr := NewManager(m)
	report, err := mgr.Cleanup(context.Background(), Options{Containers: true, DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !report.DryRun {
		t.Error("report.DryRun should be true")
	}
	if len(m.dryRuns) != 1 || !m.dryRuns[0] {
		t.Error("prune should be called with DryRun=true")
	}
	if !strings.Contains(report.String(), "dry run") {
		t.Errorf("String() missing dry run notice: %s", report.String())
	}
}

func TestCleanupNoCategories(t *testing.T) {
	m := &mockPruner{}
	mgr := NewManager(m)
	report, err := mgr.Cleanup(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 {
		t.Errorf("no categories should remove nothing: %+v", report)
	}
	if len(m.dryRuns) != 0 {
		t.Errorf("no prune calls expected, got %d", len(m.dryRuns))
	}
}

func TestCleanupCollectsErrors(t *testing.T) {
	m := &mockPruner{
		containersErr: context.DeadlineExceeded,
		usageErr:      context.Canceled,
	}
	mgr := NewManager(m)
	report, err := mgr.Cleanup(context.Background(), Options{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(report.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d: %v", len(report.Errors), report.Errors)
	}
	if !strings.Contains(report.String(), "warning:") {
		t.Errorf("String() missing warnings: %s", report.String())
	}
}

func TestReportString(t *testing.T) {
	r := &Report{
		ContainersRemoved: 1,
		ImagesRemoved:     2,
		VolumesRemoved:    3,
		NetworksRemoved:   4,
		BuildCacheFreed:   5 << 20,
		DiskUsage: []runtime.DiskUsageRow{
			{Type: "Images", TotalCount: 5, Active: 1, Size: "1GB", Reclaimable: "800MB"},
		},
	}
	s := r.String()
	for _, want := range []string{
		"containers removed: 1",
		"images removed: 2",
		"volumes removed: 3",
		"networks removed: 4",
		"build cache freed: 5.0MB",
		"Images",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("String() missing %q in:\n%s", want, s)
		}
	}
}

func TestHumanSize(t *testing.T) {
	if got := humanSize(0); got != "0B" {
		t.Errorf("humanSize(0) = %q, want 0B", got)
	}
	if got := humanSize(1024); got != "1.0kB" {
		t.Errorf("humanSize(1024) = %q, want 1.0kB", got)
	}
	if got := humanSize(5 << 20); got != "5.0MB" {
		t.Errorf("humanSize(5MB) = %q, want 5.0MB", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cleanup/ -v -count=1`

Expected: FAIL — package does not exist (`open internal/cleanup/cleanup.go: no such file` or `build constraints exclude all Go files`).

- [ ] **Step 3: Implement the cleanup package**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"strings"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

func (o Options) Any() bool {
	return o.Containers || o.Images || o.Volumes || o.Networks || o.BuildCache
}

type Report struct {
	DryRun            bool
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCacheFreed   int64
	DiskUsage         []runtime.DiskUsageRow
	Errors            []string
}

func (r *Report) String() string {
	var b strings.Builder
	if r.DryRun {
		fmt.Fprintln(&b, "[tengiz] dry run — nothing was deleted")
	}
	fmt.Fprintf(&b, "[tengiz] containers removed: %d\n", r.ContainersRemoved)
	fmt.Fprintf(&b, "[tengiz] images removed: %d\n", r.ImagesRemoved)
	fmt.Fprintf(&b, "[tengiz] volumes removed: %d\n", r.VolumesRemoved)
	fmt.Fprintf(&b, "[tengiz] networks removed: %d\n", r.NetworksRemoved)
	fmt.Fprintf(&b, "[tengiz] build cache freed: %s\n", humanSize(r.BuildCacheFreed))
	for _, row := range r.DiskUsage {
		fmt.Fprintf(&b, "[tengiz] %-12s total=%-4d active=%-4d size=%s reclaimable=%s\n",
			row.Type, row.TotalCount, row.Active, row.Size, row.Reclaimable)
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "[tengiz] warning: %s\n", e)
	}
	return b.String()
}

type Pruner interface {
	PruneStoppedContainers(ctx context.Context, opts runtime.PruneOptions) (int, error)
	PruneDanglingImages(ctx context.Context, opts runtime.PruneOptions) (int, error)
	PruneUnusedVolumes(ctx context.Context, opts runtime.PruneOptions) (int, error)
	PruneUnusedNetworks(ctx context.Context, opts runtime.PruneOptions) (int, error)
	PruneBuildCache(ctx context.Context, opts runtime.PruneOptions) (int64, error)
	DiskUsage(ctx context.Context) ([]runtime.DiskUsageRow, error)
}

type Manager struct {
	rt Pruner
}

func NewManager(rt Pruner) *Manager {
	return &Manager{rt: rt}
}

func (m *Manager) Cleanup(ctx context.Context, opts Options) (*Report, error) {
	report := &Report{DryRun: opts.DryRun}

	usage, err := m.rt.DiskUsage(ctx)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("disk usage: %v", err))
	} else {
		report.DiskUsage = usage
	}

	po := runtime.PruneOptions{DryRun: opts.DryRun}

	if opts.Containers {
		n, err := m.rt.PruneStoppedContainers(ctx, po)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("containers: %v", err))
		} else {
			report.ContainersRemoved = n
		}
	}
	if opts.Images {
		n, err := m.rt.PruneDanglingImages(ctx, po)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("images: %v", err))
		} else {
			report.ImagesRemoved = n
		}
	}
	if opts.Volumes {
		n, err := m.rt.PruneUnusedVolumes(ctx, po)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("volumes: %v", err))
		} else {
			report.VolumesRemoved = n
		}
	}
	if opts.Networks {
		n, err := m.rt.PruneUnusedNetworks(ctx, po)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("networks: %v", err))
		} else {
			report.NetworksRemoved = n
		}
	}
	if opts.BuildCache {
		freed, err := m.rt.PruneBuildCache(ctx, po)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("build cache: %v", err))
		} else {
			report.BuildCacheFreed = freed
		}
	}
	return report, nil
}

func humanSize(bytes int64) string {
	if bytes < 1<<10 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1<<20 {
		return fmt.Sprintf("%.1fkB", float64(bytes)/(1<<10))
	}
	if bytes < 1<<30 {
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1<<20))
	}
	return fmt.Sprintf("%.1fGB", float64(bytes)/(1<<30))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cleanup/ -v -count=1`

Expected: PASS (all 7 test functions).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup orchestration package"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go` (`init()` — register command + flags)

**Interfaces:**
- Consumes: `cleanup.Options`, `cleanup.NewManager`, `cleanup.Options.Any()` (Task 2); `runtime.NewDocker()` (existing)
- Produces:
  - `var cleanupCmd *cobra.Command` (registered on `rootCmd`)
  - `func cleanupOptionsFromFlags(cmd *cobra.Command) cleanup.Options` — pure helper, directly testable without Docker

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import "testing"

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	for _, name := range []string{"dry-run", "all", "containers", "images", "volumes", "networks", "build-cache"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupOptionsDefaults(t *testing.T) {
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.Containers || !opts.Images {
		t.Error("defaults should prune containers and images")
	}
	if opts.Volumes || opts.Networks || opts.BuildCache || opts.DryRun {
		t.Error("volumes/networks/build-cache/dry-run should be off by default")
	}
}

func TestCleanupOptionsAll(t *testing.T) {
	cleanupCmd.Flags().Set("all", "true")
	defer cleanupCmd.Flags().Set("all", "false")
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.Any() {
		t.Error("--all should enable all categories")
	}
	if !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Error("--all should enable volumes, networks, and build cache")
	}
}

func TestCleanupOptionsVolumesOnly(t *testing.T) {
	cleanupCmd.Flags().Set("volumes", "true")
	defer cleanupCmd.Flags().Set("volumes", "false")
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.Volumes {
		t.Error("--volumes should enable volumes")
	}
	if opts.Containers || opts.Images {
		t.Error("explicit flags should not default containers/images on")
	}
}

func TestCleanupOptionsDryRun(t *testing.T) {
	cleanupCmd.Flags().Set("dry-run", "true")
	defer cleanupCmd.Flags().Set("dry-run", "false")
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.DryRun {
		t.Error("--dry-run should set DryRun")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`

Expected: FAIL — `undefined: cleanupCmd` (and `root.go` has not registered it, so `TestCleanupCommandRegistered` fails too).

- [ ] **Step 3: Implement the command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/runtime"
)

func cleanupOptionsFromFlags(cmd *cobra.Command) cleanup.Options {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")

	opts := cleanup.Options{
		Containers: containers || all,
		Images:     images || all,
		Volumes:    volumes || all,
		Networks:   networks || all,
		BuildCache: buildCache || all,
		DryRun:     dryRun,
	}
	if !opts.Any() {
		opts.Containers = true
		opts.Images = true
	}
	return opts
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Removes stopped non-Tengiz containers, dangling images, unused volumes,
unused networks, and the Docker build cache.

Tengiz-managed containers (labeled tengiz-app=*) are always protected — the
label filter prevents them from being pruned, so scale-to-zero stopped apps
are never deleted.

By default only stopped containers and dangling images are pruned. Enable
volume, network, and build-cache pruning explicitly with their flags
(volumes can contain data, so they are opt-in).

Use --dry-run to preview what would be removed.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptionsFromFlags(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		mgr := cleanup.NewManager(rt)
		report, err := mgr.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}
		fmt.Print(report.String())
		return nil
	},
}
```

In `internal/cli/root.go` `init()`, after the line `rootCmd.AddCommand(rollbackCmd)` (line 65), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without deleting anything")
	cleanupCmd.Flags().Bool("all", false, "prune containers, images, volumes, networks, and build cache")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (opt-in: volumes can hold data)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune the Docker build cache")
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`

Expected: PASS (all 6 test functions).

- [ ] **Step 5: Manual smoke test (requires Docker)**

Run: `go build -o tengiz . && ./tengiz cleanup --dry-run`

Expected: prints a `[tengiz] dry run — nothing was deleted` header, per-category counts, a `docker system df` disk-usage table, and no warnings. If Docker is not available, this step is skipped — the unit tests cover the logic.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: nothing new
- Produces: no code — documentation only

- [ ] **Step 1: Update README.md CLI Reference**

Add a new section to `README.md` right after the `### \`tengiz rollback <app>\`` section (which ends at line 236, before `### \`tengiz domain\``):

````markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space. This is the #1 operational fix for single-server deployments that fill up over time from builds and scale-to-zero churn.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without deleting anything |
| `--all` | Prune containers, images, volumes, networks, and build cache |
| `--containers` | Prune stopped containers that are **not** managed by Tengiz |
| `--images` | Prune dangling (untagged) images |
| `--volumes` | Prune unused volumes (opt-in — volumes can hold data) |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune the Docker build cache |

By default (`tengiz cleanup`) only stopped non-Tengiz containers and dangling images are pruned. Tengiz-managed containers are protected by their `tengiz-app` label — scale-to-zero stopped apps are never deleted. Always run `tengiz cleanup --dry-run` first to preview.
````

- [ ] **Step 2: Update docs/FUTURES_FEATURES.md**

In the P0 table (line 19), change the feature name cell from `**Docker Housekeeping** ⬜` to `**Docker Housekeeping** ✅`.

In the detail section `## Docker Housekeeping (Otomatik Temizlik)`, after the `- **Detected:** 2026-07-14` line, add:

```
- **Status:** ✅ Implemented (2026-08-10)
```

In the `### ✅ Implemented Features (Not Pending)` table, add a row:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-10) |
```

- [ ] **Step 3: Update AGENTS.md CLI section**

Add the following line to the CLI command list in `AGENTS.md` (alphabetically near `tengiz config`):

```
tengiz cleanup [--dry-run] [--all] [--containers] [--images] [--volumes] [--networks] [--build-cache] → prune unused Docker resources
```

- [ ] **Step 4: Run the full verification suite**

Run: `go build -o tengiz . && go vet ./... && go test ./... -v -count=1`

Expected: build succeeds, `go vet` reports no issues, and all tests pass with `-count=1` (no cached results).

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage.** The FUTURES_FEATURES.md #6 spec calls for: periodic cleanup of unused volumes, networks, containers, and images (→ Tasks 1-3: `PruneUnusedVolumes`, `PruneUnusedNetworks`, `PruneStoppedContainers`, `PruneDanglingImages`, `PruneBuildCache`); label-based protection of Tengiz-managed containers (→ Task 1: every container path uses `--filter label!=tengiz-app`); a `tengiz cleanup` command (→ Task 3). The "periodic" (periyodik) aspect maps to Coolify's `DockerCleanupJob`; this plan deliberately scopes to the interactive command and dry-run preview, leaving a background scheduler as a follow-up (the sibling feature #56 Granular Docker Prune Operations / #103 Cleanup & GC remain separate plans). Rationale column "Disk space is the #1 production issue" is directly addressed by `--build-cache` (the largest reclaimable) plus per-category pruning.

**2. Placeholder scan.** No TBD/TODO/placeholders. Every code step contains complete, compilable code. Test steps include full test code and exact expected outputs. Commands are exact with expected results.

**3. Type consistency.** Names are consistent across tasks: `PruneStoppedContainers`, `PruneDanglingImages`, `PruneUnusedVolumes`, `PruneUnusedNetworks`, `PruneBuildCache`, `DiskUsage`, `PruneOptions`, `DiskUsageRow` (Task 1) are consumed identically by `cleanup.Pruner` (Task 2) and the CLI (Task 3). `cleanup.Options.Any()`, `cleanup.NewManager`, `cleanup.Options` match between Tasks 2 and 3. `cleanupOptionsFromFlags` signature is stable. The `Manager` interface extension is applied to all four implementations (`dockerRuntime`, `stubManager`, and the three test mocks) in Task 1 Step 6 so compilation stays green.

**Known limitation (documented, not a gap):** count reporting relies on `docker prune` output formats ("Deleted X:", "deleted:", "Total reclaimed space:") which vary slightly across Docker versions; a count may be under/over-reported but pruning correctness is never affected — the label filter and docker's own prune semantics guarantee nothing Tengiz-managed is ever removed. Dry-run network count is an estimate (it lists non-default networks, some of which may be in use).
