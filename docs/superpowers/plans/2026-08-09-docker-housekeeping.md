# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely prunes unused Docker resources (stopped containers, dangling images, unused networks, build cache) to free disk space on single-server deployments, while always protecting Tengiz-managed containers via labels.

**Architecture:** A `runtime.Manager` gains two methods — `Prune(ctx, opts)` and `DiskUsage(ctx)` — implemented on `dockerRuntime` via `docker` CLI sub-commands (`docker container/image/network/volume/builder prune` and `docker system df`). Per-resource prune commands (not bare `docker system prune`, which cannot exclude by label reliably) are used so that `--filter label!=tengiz-app` excludes all Tengiz-managed containers/networks from deletion. The `tengiz cleanup` CLI command calls `DiskUsage` first (preview), then `Prune`, and prints a per-category + total reclaim report. `--dry-run` only prints disk usage; `--volumes` opts into the dangerous volume prune. No Docker SDK — all interaction stays `os/exec` per the codebase convention.

**Tech Stack:** Go 1.26 stdlib (`os/exec`, `encoding/json`, `regexp`), Cobra, existing `runtime.Manager` interface + `config.Store`. No new external dependencies.

## Global Constraints

- All Docker interaction via `os/exec` calling the `docker` CLI — no Docker SDK
- Tengiz containers are protected with `--filter label!=tengiz-app` (label key `tengiz-app`, already applied in `runtime/docker.go`)
- Image prune must NOT use `-a` — `tengiz-apps/*` images are the rollback source and must never be force-pruned; only dangling images are removed
- Volume pruning must be opt-in via `--volumes` (never default)
- No new external Go dependencies (stdlib + cobra only)
- Existing tests must continue to pass; adding methods to `runtime.Manager` requires updating all three test mocks (`internal/cli/root_test.go`, `internal/proxy/proxy_test.go`, `internal/idle/idle_test.go`) and the `stubManager`
- Interface additions: `Prune(ctx context.Context, opts types.PruneOptions) (types.PruneReport, error)` and `DiskUsage(ctx context.Context) (types.DockerDiskUsage, error)`
- The plan's scope is feature #6 only (base `tengiz cleanup`). Feature #56 (granular per-category prune flags) and #103 (build cache / git GC) are separate features — do NOT build them here

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | Add `PruneCategory`, `PruneOptions`, `PruneResult`, `PruneReport`, `DockerDiskEntry`, `DockerDiskUsage` |
| `internal/runtime/runtime.go` | Add `Prune`/`DiskUsage` to `Manager` interface + `stubManager` implementations |
| `internal/runtime/housekeeping.go` | **New** — `dockerRuntime.Prune`, `dockerRuntime.DiskUsage`, and pure helpers `pruneArgs`, `parseReclaimed`, `countDeleted`, `parseSizeBytes`, `formatBytes` |
| `internal/runtime/housekeeping_test.go` | **New** — stub tests + table tests for all pure helpers |
| `internal/cli/root_test.go` | Add `Prune`/`DiskUsage` to `mockRTForDeploy` (compiles against the grown interface) |
| `internal/proxy/proxy_test.go` | Add `Prune`/`DiskUsage` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Prune`/`DiskUsage` to `mockRuntime` |
| `internal/cli/cleanup.go` | **New** — `cleanupCmd` Cobra command with `--dry-run`/`--volumes` flags |
| `internal/cli/cleanup_test.go` | **New** — command registration + flag parsing tests |
| `README.md` | Document `tengiz cleanup` |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as ✅ Implemented |

---

### Task 1: Add housekeeping types and grow the `runtime.Manager` interface

**Files:**
- Modify: `internal/types/types.go` — append new types at end of file (after `AppEntry`, line 186)
- Modify: `internal/runtime/runtime.go:36` — add two methods to `Manager` interface; add stub methods near line 119
- Modify: `internal/cli/root_test.go:98-99` — add methods to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:33-34` — add methods to `mockRuntime`
- Modify: `internal/idle/idle_test.go:32-33` — add methods to `mockRuntime`
- Test: `internal/runtime/housekeeping_test.go` — **new**

**Interfaces:**
- Consumes: nothing new
- Produces: `types.PruneCategory` (consts `PruneContainers`, `PruneImages`, `PruneNetworks`, `PruneVolumes`, `PruneBuildCache`), `types.PruneOptions{ Categories []PruneCategory; IncludeVolumes bool }`, `types.PruneResult{ Deleted int; Reclaimed string }`, `types.PruneReport{ Categories map[PruneCategory]PruneResult; TotalReclaimed string }`, `types.DockerDiskEntry{ Type string; TotalCount int64; Active int64; Size string; Reclaimable string }`, `types.DockerDiskUsage{ Entries []DockerDiskEntry; TotalReclaimable string }`. `runtime.Manager` now includes `Prune(ctx, types.PruneOptions) (types.PruneReport, error)` and `DiskUsage(ctx) (types.DockerDiskUsage, error)`.

- [ ] **Step 1: Write the failing stub tests**

Create `internal/runtime/housekeeping_test.go`:

```go
package runtime

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), types.PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.TotalReclaimed != "" {
		t.Errorf("TotalReclaimed = %q, want empty", report.TotalReclaimed)
	}
	if len(report.Categories) != 0 {
		t.Errorf("Categories = %v, want empty", report.Categories)
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	usage, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if len(usage.Entries) != 0 {
		t.Errorf("Entries = %v, want empty", usage.Entries)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestStubDiskUsage" -count=1`

Expected: FAIL — compile error `undefined: types.PruneOptions` / `undefined: types.PruneReport` / `stubManager does not implement Manager (missing method Prune)`

- [ ] **Step 3: Add the types to `internal/types/types.go`**

Append at the end of `internal/types/types.go`:

```go
type PruneCategory string

const (
	PruneContainers PruneCategory = "containers"
	PruneImages     PruneCategory = "images"
	PruneNetworks   PruneCategory = "networks"
	PruneVolumes    PruneCategory = "volumes"
	PruneBuildCache PruneCategory = "build-cache"
)

type PruneOptions struct {
	Categories     []PruneCategory
	IncludeVolumes bool
}

type PruneResult struct {
	Deleted   int    `json:"deleted"`
	Reclaimed string `json:"reclaimed"`
}

type PruneReport struct {
	Categories     map[PruneCategory]PruneResult `json:"categories"`
	TotalReclaimed string                        `json:"total_reclaimed"`
}

type DockerDiskEntry struct {
	Type        string `json:"Type"`
	TotalCount  int64  `json:"TotalCount"`
	Active      int64  `json:"Active"`
	Size        string `json:"Size"`
	Reclaimable string `json:"Reclaimable"`
}

type DockerDiskUsage struct {
	Entries          []DockerDiskEntry `json:"entries"`
	TotalReclaimable string            `json:"total_reclaimable"`
}
```

- [ ] **Step 4: Add `Prune`/`DiskUsage` to the `Manager` interface + stub**

In `internal/runtime/runtime.go`, after the `KeepLastNImages` line (line 36), add to the interface:

```go
	Prune(ctx context.Context, opts types.PruneOptions) (types.PruneReport, error)
	DiskUsage(ctx context.Context) (types.DockerDiskUsage, error)
```

Add stub implementations after the `stubManager.KeepLastNImages` method (line 119):

```go
func (m *stubManager) Prune(ctx context.Context, opts types.PruneOptions) (types.PruneReport, error) {
	return types.PruneReport{Categories: map[types.PruneCategory]types.PruneResult{}}, nil
}

func (m *stubManager) DiskUsage(ctx context.Context) (types.DockerDiskUsage, error) {
	return types.DockerDiskUsage{}, nil
}
```

- [ ] **Step 5: Update the three test mocks so the package tree still compiles**

`internal/cli/root_test.go` — add after `mockRTForDeploy.KeepLastNImages` (line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts types.PruneOptions) (types.PruneReport, error) { return types.PruneReport{}, nil }
func (m *mockRTForDeploy) DiskUsage(ctx context.Context) (types.DockerDiskUsage, error) { return types.DockerDiskUsage{}, nil }
```

`internal/proxy/proxy_test.go` — add after `mockRuntime.KeepLastNImages` (line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts types.PruneOptions) (types.PruneReport, error) { return types.PruneReport{}, nil }
func (m *mockRuntime) DiskUsage(ctx context.Context) (types.DockerDiskUsage, error) { return types.DockerDiskUsage{}, nil }
```

`internal/idle/idle_test.go` — add after `mockRuntime.KeepLastNImages` (line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts types.PruneOptions) (types.PruneReport, error) { return types.PruneReport{}, nil }
func (m *mockRuntime) DiskUsage(ctx context.Context) (types.DockerDiskUsage, error) { return types.DockerDiskUsage{}, nil }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestStubDiskUsage" -count=1`
Expected: PASS

Run: `go build ./...`
Expected: build succeeds (all mocks updated)

- [ ] **Step 7: Commit**

```bash
git add internal/types/types.go internal/runtime/runtime.go internal/runtime/housekeeping_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add housekeeping types and Prune/DiskUsage to runtime.Manager"
```

---

### Task 2: Implement `dockerRuntime` housekeeping with pure helpers

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go` — extend with helper table tests

**Interfaces:**
- Consumes: `types.PruneOptions`, `types.PruneReport`, `types.DockerDiskUsage`, `types.PruneCategory` from Task 1; `labelKey` const (`"tengiz-app"`) already defined in `docker.go`
- Produces: `dockerRuntime.Prune(ctx, types.PruneOptions) (types.PruneReport, error)`, `dockerRuntime.DiskUsage(ctx) (types.DockerDiskUsage, error)`, and unexported helpers `pruneArgs(cat types.PruneCategory) ([]string, error)`, `parseReclaimed(out string) string`, `countDeleted(out string) int`, `parseSizeBytes(s string) (int64, error)`, `formatBytes(b int64) string`

- [ ] **Step 1: Write the failing helper tests**

Append to `internal/runtime/housekeeping_test.go`:

```go
func TestPruneArgs(t *testing.T) {
	tests := []struct {
		cat      types.PruneCategory
		expected string
	}{
		{types.PruneContainers, "container prune -f --filter label!=tengiz-app --format {{.ID}}"},
		{types.PruneImages, "image prune -f --format {{.ID}}"},
		{types.PruneNetworks, "network prune -f --filter label!=tengiz-app --format {{.ID}}"},
		{types.PruneVolumes, "volume prune -f --format {{.ID}}"},
		{types.PruneBuildCache, "builder prune -f -a"},
	}
	for _, tt := range tests {
		got, err := pruneArgs(tt.cat)
		if err != nil {
			t.Fatalf("pruneArgs(%q) error = %v", tt.cat, err)
		}
		if strings.Join(got, " ") != tt.expected {
			t.Errorf("pruneArgs(%q) = %q, want %q", tt.cat, strings.Join(got, " "), tt.expected)
		}
	}
	if _, err := pruneArgs("bogus"); err == nil {
		t.Error("pruneArgs(bogus) expected an error")
	}
}

func TestParseReclaimed(t *testing.T) {
	cases := []struct {
		out  string
		want string
	}{
		{"Deleted Containers:\nTotal reclaimed space: 12.3MB", "12.3MB"},
		{"Total reclaimed space: 0B", "0B"},
		{"nothing relevant here", ""},
	}
	for _, c := range cases {
		if got := parseReclaimed(c.out); got != c.want {
			t.Errorf("parseReclaimed(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

func TestCountDeleted(t *testing.T) {
	containerOut := "abc123\ndef456\n\nTotal reclaimed space: 2.1GB"
	if got := countDeleted(containerOut); got != 2 {
		t.Errorf("countDeleted(container) = %d, want 2", got)
	}
	builderOut := "Deleted build cache objects:\nxyz789\n\nTotal reclaimed space: 1GB"
	if got := countDeleted(builderOut); got != 1 {
		t.Errorf("countDeleted(builder) = %d, want 1", got)
	}
	emptyOut := "Total reclaimed space: 0B"
	if got := countDeleted(emptyOut); got != 0 {
		t.Errorf("countDeleted(empty) = %d, want 0", got)
	}
}

func TestParseSizeBytes(t *testing.T) {
	cases := []struct {
		s    string
		want int64
	}{
		{"", 0},
		{"0", 0},
		{"100B", 100},
		{"1kB", 1000},
		{"1.5MB", 1500000},
		{"2GB", 2000000000},
		{"1GiB", 1073741824},
	}
	for _, c := range cases {
		got, err := parseSizeBytes(c.s)
		if err != nil {
			t.Fatalf("parseSizeBytes(%q) error = %v", c.s, err)
		}
		if got != c.want {
			t.Errorf("parseSizeBytes(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		b    int64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{1500, "1.5kB"},
		{2500000, "2.5MB"},
		{3000000000, "3.0GB"},
	}
	for _, c := range cases {
		if got := formatBytes(c.b); got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.b, got, c.want)
		}
	}
}
```

Also add the `strings` import to `housekeeping_test.go`:

```go
import (
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneArgs|TestParseReclaimed|TestCountDeleted|TestParseSizeBytes|TestFormatBytes" -count=1`

Expected: FAIL — compile error `undefined: pruneArgs` / `undefined: parseReclaimed` / etc.

- [ ] **Step 3: Write the implementation in `internal/runtime/housekeeping.go`**

```go
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

var reclaimedLineRe = regexp.MustCompile(`(?i)total reclaimed space:\s*(\S+)`)

func pruneArgs(cat types.PruneCategory) ([]string, error) {
	switch cat {
	case types.PruneContainers:
		return []string{"container", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey), "--format", "{{.ID}}"}, nil
	case types.PruneImages:
		return []string{"image", "prune", "-f", "--format", "{{.ID}}"}, nil
	case types.PruneNetworks:
		return []string{"network", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey), "--format", "{{.ID}}"}, nil
	case types.PruneVolumes:
		return []string{"volume", "prune", "-f", "--format", "{{.ID}}"}, nil
	case types.PruneBuildCache:
		return []string{"builder", "prune", "-f", "-a"}, nil
	default:
		return nil, fmt.Errorf("unknown prune category: %s", cat)
	}
}

func parseReclaimed(out string) string {
	m := reclaimedLineRe.FindStringSubmatch(out)
	if len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func countDeleted(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "reclaimed") || strings.Contains(line, "Deleted") {
			continue
		}
		n++
	}
	return n
}

func parseSizeBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	units := []struct {
		suffix string
		mult   int64
	}{
		{"TiB", 1 << 40}, {"TB", 1000000000000},
		{"GiB", 1 << 30}, {"GB", 1000000000},
		{"MiB", 1 << 20}, {"MB", 1000000},
		{"KiB", 1 << 10}, {"kB", 1000}, {"KB", 1000},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, fmt.Errorf("parse size %q: %w", s, err)
			}
			return int64(f * float64(u.mult)), nil
		}
	}
	return strconv.ParseInt(s, 10, 64)
}

func formatBytes(b int64) string {
	if b < 0 {
		b = 0
	}
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div := int64(unit)
	exp := 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "kMGTPE"[exp])
}

func (r *dockerRuntime) Prune(ctx context.Context, opts types.PruneOptions) (types.PruneReport, error) {
	cats := opts.Categories
	if len(cats) == 0 {
		cats = []types.PruneCategory{
			types.PruneContainers,
			types.PruneImages,
			types.PruneNetworks,
			types.PruneBuildCache,
		}
		if opts.IncludeVolumes {
			cats = append(cats, types.PruneVolumes)
		}
	}
	report := types.PruneReport{Categories: make(map[types.PruneCategory]types.PruneResult, len(cats))}
	var total int64
	for _, cat := range cats {
		res, err := r.pruneCategory(ctx, cat)
		if err != nil {
			return report, err
		}
		report.Categories[cat] = res
		if b, perr := parseSizeBytes(res.Reclaimed); perr == nil {
			total += b
		}
	}
	report.TotalReclaimed = formatBytes(total)
	return report, nil
}

func (r *dockerRuntime) pruneCategory(ctx context.Context, cat types.PruneCategory) (types.PruneResult, error) {
	args, err := pruneArgs(cat)
	if err != nil {
		return types.PruneResult{}, err
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return types.PruneResult{}, fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out))
	}
	return types.PruneResult{
		Deleted:   countDeleted(string(out)),
		Reclaimed: parseReclaimed(string(out)),
	}, nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (types.DockerDiskUsage, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return types.DockerDiskUsage{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	var usage types.DockerDiskUsage
	var total int64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var e types.DockerDiskEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		usage.Entries = append(usage.Entries, e)
		if b, perr := parseSizeBytes(e.Reclaimable); perr == nil {
			total += b
		}
	}
	usage.TotalReclaimable = formatBytes(total)
	return usage, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneArgs|TestParseReclaimed|TestCountDeleted|TestParseSizeBytes|TestFormatBytes|TestStubPrune|TestStubDiskUsage" -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: implement label-protected docker housekeeping in runtime"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go` — **new**

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.Manager.Prune`, `runtime.Manager.DiskUsage`, `types.PruneOptions`, `types.PruneCategory` from Tasks 1-2
- Produces: `tengiz cleanup` and `tengiz cleanup --dry-run|--volumes` CLI commands

- [ ] **Step 1: Write the failing CLI tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
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

func TestCleanupFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "volumes"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	var dryRun, volumes bool
	original := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = original }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		dryRun, _ = cmd.Flags().GetBool("dry-run")
		volumes, _ = cmd.Flags().GetBool("volumes")
		return nil
	}
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !dryRun {
		t.Error("--dry-run flag was not parsed as true")
	}
	if !volumes {
		t.Error("--volumes flag was not parsed as true")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -count=1`

Expected: FAIL — compile error `undefined: cleanupCmd`

- [ ] **Step 3: Write the command in `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune Docker resources to free disk space",
	Long: `Prune unused Docker resources: stopped containers, dangling images,
unused networks, and build cache. Containers and networks managed by Tengiz
are always protected via labels.

Use --dry-run to preview reclaimable space without removing anything.
Use --volumes to also prune unused named volumes (DANGEROUS — may delete data).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		volumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		usage, err := rt.DiskUsage(cmd.Context())
		if err != nil {
			return fmt.Errorf("disk usage: %w", err)
		}
		fmt.Println("[tengiz] Docker disk usage:")
		for _, e := range usage.Entries {
			fmt.Printf("  %-12s %d total, %d active, %s reclaimable\n", e.Type, e.TotalCount, e.Active, e.Reclaimable)
		}
		fmt.Printf("  Total reclaimable: %s\n", usage.TotalReclaimable)

		if dryRun {
			fmt.Println("[tengiz] dry-run: nothing was pruned")
			return nil
		}

		report, err := rt.Prune(cmd.Context(), types.PruneOptions{IncludeVolumes: volumes})
		if err != nil {
			return fmt.Errorf("prune: %w", err)
		}
		for _, cat := range []types.PruneCategory{
			types.PruneContainers,
			types.PruneImages,
			types.PruneNetworks,
			types.PruneBuildCache,
			types.PruneVolumes,
		} {
			res, ok := report.Categories[cat]
			if !ok {
				continue
			}
			fmt.Printf("[tengiz] pruned %-12s deleted %d, reclaimed %s\n", cat, res.Deleted, res.Reclaimed)
		}
		fmt.Printf("[tengiz] cleanup complete: reclaimed %s total\n", report.TotalReclaimed)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show reclaimable disk space without pruning anything")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused named volumes (dangerous)")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -count=1`

Expected: PASS

- [ ] **Step 5: Run full build + vet**

Run: `go build ./... && go vet ./...`

Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Update documentation and mark feature #6 implemented

**Files:**
- Modify: `README.md` — add `tengiz cleanup` docs
- Modify: `docs/FUTURES_FEATURES.md` — mark #6 ✅ Implemented

**Interfaces:**
- Consumes: the CLI surface from Task 3
- Produces: up-to-date user docs and feature tracker

- [ ] **Step 1: Document `tengiz cleanup` in `README.md`**

Add a row to the Commands table in the webhook/git section (around line 568-575):

```markdown
| `tengiz cleanup [--dry-run] [--volumes]` | Prune unused Docker resources (stopped containers, dangling images, networks, build cache) |
```

Then insert a new section immediately before `## Architecture` (line 577):

```markdown
## Disk Housekeeping

Deployments and scale-to-zero leave behind stopped containers, dangling images, and build cache. Run `tengiz cleanup` to reclaim disk space:

```bash
tengiz cleanup               # show disk usage, then prune containers/images/networks/build cache
tengiz cleanup --dry-run     # only show reclaimable space, prune nothing
tengiz cleanup --volumes     # also prune unused named volumes (dangerous — may delete data)
```

Tengiz-managed containers and networks are always protected via the `tengiz-app` label and are never removed. Image pruning only removes dangling images; tagged rollback images are preserved.
```

- [ ] **Step 2: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table, change the row for feature #6 (currently line 19) from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ (2026-08-09) | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the `## Docker Housekeeping (Otomatik Temizlik)` feature section (currently line 377-381), add a Status line after the `Why add to Tengiz` line:

```markdown
- **Status:** ✅ Implemented (2026-08-09)
```

Add a row to the `## ✅ Implemented Features (Not Pending)` table (after the Nixpacks row, ~line 252):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-09) |
```

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -count=1`

Expected: All PASS (proxy TCP-dial tests and idle timing tests may be slow/flaky — that is pre-existing and acceptable; rerun once if needed)

Run: `go vet ./...`

Expected: no issues

- [ ] **Step 4: Verify the command works end-to-end (optional, requires Docker)**

Run: `go build -o tengiz . && ./tengiz cleanup --dry-run`

Expected: prints `[tengiz] Docker disk usage:` with a `Total reclaimable` line and `[tengiz] dry-run: nothing was pruned`

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**Spec coverage** — feature #6 from `docs/FUTURES_FEATURES.md`:
- `tengiz cleanup` command ✅ (Task 3)
- Label-based protection of Tengiz containers ✅ (Task 2 — `--filter label!=tengiz-app` on container/network prune)
- Disk reclaim for stopped containers, dangling images, unused networks, build cache ✅ (Task 2 — all four safe categories)
- Disk-usage preview via `docker system df` ✅ (Task 2 `DiskUsage`, Task 3 CLI)
- Coolify's `DockerCleanupJob` behavior (prune unused volumes/networks/containers/images) ✅ (volumes behind `--volumes` opt-in flag)
- NOT built (out of scope): feature #56 granular per-category prune flags, feature #103 build cache/git GC, periodic scheduler.

**Placeholder scan** — no "TBD"/"TODO"/"implement later"/"similar to Task N" anywhere; every code step contains complete, compilable code. The only optional step (Task 4 Step 4) is explicitly labeled optional and does not gate anything.

**Type consistency** —
- `types.PruneOptions{ Categories []PruneCategory; IncludeVolumes bool }` — defined Task 1, consumed Task 2 `Prune` and Task 3 `types.PruneOptions{IncludeVolumes: volumes}`. Consistent.
- `types.PruneReport.Categories map[types.PruneCategory]types.PruneResult` + `TotalReclaimed string` — produced Task 2, consumed Task 3. Consistent.
- `types.DockerDiskUsage.Entries []DockerDiskEntry` + `TotalReclaimable string` — produced Task 2 `DiskUsage`, consumed Task 3. Consistent.
- `pruneArgs(cat) ([]string, error)`, `parseReclaimed(string) string`, `countDeleted(string) int`, `parseSizeBytes(string) (int64, error)`, `formatBytes(int64) string` — names identical in Task 2 tests and implementation.
- `runtime.Manager.Prune(ctx, types.PruneOptions) (types.PruneReport, error)` and `runtime.Manager.DiskUsage(ctx) (types.DockerDiskUsage, error)` — identical signatures in interface (Task 1), stub, dockerRuntime (Task 2), and all three test mocks.
- `labelKey` reused from `docker.go` (value `tengiz-app`) — no hardcoded string drift.
