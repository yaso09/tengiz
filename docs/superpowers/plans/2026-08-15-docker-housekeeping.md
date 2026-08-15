# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, networks, and build cache — with label-based protection so Tengiz-managed containers are never touched — to reclaim disk space on single-server deployments.

**Architecture:** The runtime layer (`internal/runtime`) gains two new `Manager` interface methods: `SystemDf` (parses `docker system df` into a structured report of reclaimable disk) and `Cleanup` (runs per-category `docker ... prune -f` commands and sums the reclaimed bytes). The CLI layer (`internal/cli`) adds a `cleanup` cobra command whose core logic lives in a testable `runCleanup(ctx, rt, apps, opts)` function: dry-run by default (prints the disk-usage report + what would be pruned), `--force` to actually prune. Per-app image retention reuses the existing `KeepLastNImages` method, driven by app names from the env-scoped config store. Tengiz containers are protected because every Tengiz container (including previews and one-off `run` containers) is created with the `tengiz-app` label, and the container prune uses `--filter label!=tengiz-app`.

**Tech Stack:** Go 1.26, Cobra, the existing `docker` CLI via `os/exec` (no Docker SDK — matches the existing `dockerRuntime` pattern), existing `config.Store` and `runtime.Manager` interfaces. No new external dependencies.

## Global Constraints

- Container pruning must use `docker container prune -f --filter label!=tengiz-app` so stopped Tengiz containers (scale-to-zero idle state) are never removed
- Default mode is **dry-run**: `tengiz cleanup` without `--force` prints the `docker system df` report and a description of what would be pruned, and deletes nothing
- `--force` is required to actually prune; no interactive confirmation prompts (non-TTY / CI friendly, matching the CLI-first design)
- With no category flag given, all five categories are selected (equivalent to `--all`)
- Image retention keeps the last `--keep N` images per app (default `5`), reusing the existing `runtime.Manager.KeepLastNImages`
- `--env` (the existing persistent flag) scopes which apps get image retention via `config.NewStoreWithEnv`
- All Docker sizes are treated as binary units (Docker's decimal "kB/MB/GB" are within ~2% of binary; acceptable for disk estimates)
- No new external dependencies; `go vet ./...`, `go build ./...`, and `go test ./... -count=1` must pass after each task
- Existing tests must continue to pass; the `mockRTForDeploy` in `internal/cli/root_test.go` must be extended whenever the `runtime.Manager` interface grows (it is asserted against the interface in `TestMockRTForDeployImplementsManager`)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/housekeeping.go` (create) | Size parse/format helpers, `DfReport`/`DfRow` types, `parseDfOutput`, `parsePruneOutput`, and the `dockerRuntime.SystemDf` + `dockerRuntime.Cleanup` methods |
| `internal/runtime/runtime.go` (modify) | Add `SystemDf` and `Cleanup` to the `Manager` interface; add stub implementations to `stubManager` |
| `internal/runtime/housekeeping_test.go` (create) | Table-driven tests for the pure parsers/helpers + stub method tests |
| `internal/cli/root_test.go` (modify) | Add `SystemDf` and `Cleanup` methods to `mockRTForDeploy` (interface grows) |
| `internal/cli/cleanup.go` (create) | `cleanupCmd` cobra command, flag registration via `init()`, selection/description helpers, and the testable `runCleanup` core |
| `internal/cli/cleanup_test.go` (create) | Tests for command registration, flags, category selection, and `runCleanup` dry-run/force paths using `runtime.NewStub()` |
| `README.md` (modify) | New `### tengiz cleanup` section in the CLI Reference |
| `AGENTS.md` (modify) | Add the `tengiz cleanup` line to the CLI command list |

Task boundaries: each task produces a compiling, independently-testable deliverable. The `runtime.Manager` interface is only extended in the same task that implements the method on `dockerRuntime` (so `NewDocker` still satisfies the interface) and that updates the `cli` mock (so `go test ./internal/cli` still compiles).

---

### Task 1: Runtime size parsing and formatting helpers

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: nothing (pure functions)
- Produces: `parseSize(s string) (int64, error)` and `FormatSize(bytes int64) string` in package `runtime` (unexported `parseSize` for runtime-internal use; exported `FormatSize` for the CLI to print human-readable reclaimed bytes)

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/housekeeping_test.go
package runtime

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"0B", 0, false},
		{"10B", 10, false},
		{"1.5KB", 1536, false},
		{"500MB", 524288000, false},
		{"1.2GB", 1288490188, false},
		{"2TB", 2199023255552, false},
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		got, err := parseSize(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseSize(%q) expected error, got %d", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSize(%q) error = %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 kB"},
		{1536, "1.5 kB"},
		{1610612736, "1.5 GB"},
	}
	for _, tt := range tests {
		if got := FormatSize(tt.in); got != tt.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestParseSize|TestFormatSize" -v -count=1`

Expected: FAIL — `undefined: parseSize`, `undefined: FormatSize` (housekeeping_test.go does not compile).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/runtime/housekeeping.go
package runtime

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var sizeRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*([kKmMgGtT]?i?[bB])$`)

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	m := sizeRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid size: %q", s)
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, err
	}
	mult := 1.0
	switch strings.ToLower(m[2])[0] {
	case 'k':
		mult = 1024
	case 'm':
		mult = 1024 * 1024
	case 'g':
		mult = 1024 * 1024 * 1024
	case 't':
		mult = 1024 * 1024 * 1024 * 1024
	}
	return int64(val * mult), nil
}

func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "kMGTPE"[exp])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestParseSize|TestFormatSize" -v -count=1`

Expected: PASS (both tests green).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add runtime size parsing and formatting helpers"
```

---

### Task 2: Runtime disk-usage report (`SystemDf`)

**Files:**
- Create: `internal/runtime/housekeeping.go` (append)
- Modify: `internal/runtime/runtime.go:31-49` — add `SystemDf` to `Manager` interface; `internal/runtime/runtime.go:51-122` — add stub to `stubManager`
- Modify: `internal/cli/root_test.go:69-100` — add `SystemDf` method to `mockRTForDeploy` (interface assertion requires it)
- Test: `internal/runtime/housekeeping_test.go` (append)

**Interfaces:**
- Consumes: `parseSize(s string) (int64, error)` from Task 1
- Produces: `type DfRow struct { Type, Total, Active, Size, Reclaimable string }`, `type DfReport struct { Rows []DfRow }`, method `(*DfReport).ReclaimableBytes() int64`, unexported `parseDfOutput(out string) (*DfReport, error)`, and `SystemDf(ctx context.Context) (*DfReport, error)` on the `runtime.Manager` interface (implemented by both `dockerRuntime` and `stubManager`)

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/housekeeping_test.go (append)
func TestParseDfOutput(t *testing.T) {
	out := "Images|7|2|1.5GB|1.1GB\nContainers|3|1|0B|0B\nLocal Volumes|1|0|0B|0B\nBuild Cache|4|0|120MB|120MB"
	report, err := parseDfOutput(out)
	if err != nil {
		t.Fatalf("parseDfOutput error = %v", err)
	}
	if len(report.Rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(report.Rows))
	}
	if report.Rows[0].Type != "Images" || report.Rows[0].Reclaimable != "1.1GB" {
		t.Errorf("row0 = %+v", report.Rows[0])
	}
}

func TestParseDfOutputBadLine(t *testing.T) {
	if _, err := parseDfOutput("Images|1|1|0B|0B|EXTRA"); err == nil {
		t.Error("expected error for malformed df line")
	}
}

func TestReclaimableBytes(t *testing.T) {
	report := &DfReport{
		Rows: []DfRow{
			{Type: "Images", Reclaimable: "1.1GB"},
			{Type: "Containers", Reclaimable: "0B"},
			{Type: "Build Cache", Reclaimable: "120MB"},
		},
	}
	// 1.1 * 1024^3 = 1181116006.4 -> 1181116006 ; 120 * 1024^2 = 125829120
	want := int64(1181116006) + int64(125829120)
	if got := report.ReclaimableBytes(); got != want {
		t.Errorf("ReclaimableBytes() = %d, want %d", got, want)
	}
}

func TestStubSystemDf(t *testing.T) {
	m := NewStub()
	report, err := m.SystemDf(context.Background())
	if err != nil {
		t.Fatalf("SystemDf error = %v", err)
	}
	if report == nil {
		t.Fatal("SystemDf returned nil report")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestParseDfOutput|TestReclaimableBytes|TestStubSystemDf" -v -count=1`

Expected: FAIL — `undefined: DfRow`, `undefined: DfReport`, `undefined: parseDfOutput`, and `stubManager does not implement Manager (missing method SystemDf)`.

- [ ] **Step 3: Add `SystemDf` to the `Manager` interface and stub**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after the `KeepLastNImages` line, line 36):

```go
	SystemDf(ctx context.Context) (*DfReport, error)
```

Add to `stubManager` (after `KeepLastNImages`, line 119):

```go
func (m *stubManager) SystemDf(ctx context.Context) (*DfReport, error) {
	return &DfReport{}, nil
}
```

- [ ] **Step 4: Add `SystemDf` to `mockRTForDeploy` in the CLI tests**

In `internal/cli/root_test.go`, add after the `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) SystemDf(ctx context.Context) (*runtime.DfReport, error) { return &runtime.DfReport{}, nil }
```

- [ ] **Step 5: Write the minimal implementation**

Append to `internal/runtime/housekeeping.go`:

```go
type DfRow struct {
	Type        string
	Total       string
	Active      string
	Size        string
	Reclaimable string
}

type DfReport struct {
	Rows []DfRow
}

func (r *DfReport) ReclaimableBytes() int64 {
	var total int64
	for _, row := range r.Rows {
		if b, err := parseSize(row.Reclaimable); err == nil {
			total += b
		}
	}
	return total
}

func parseDfOutput(out string) (*DfReport, error) {
	var rows []DfRow
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 5 {
			return nil, fmt.Errorf("unexpected docker system df line: %q", line)
		}
		rows = append(rows, DfRow{
			Type:        parts[0],
			Total:       parts[1],
			Active:      parts[2],
			Size:        parts[3],
			Reclaimable: parts[4],
		})
	}
	return &DfReport{Rows: rows}, nil
}

func (r *dockerRuntime) SystemDf(ctx context.Context) (*DfReport, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df",
		"--format", "{{.Type}}|{{.Total}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseDfOutput(string(out))
}
```

Add `"context"` and `"os/exec"` to the imports of `internal/runtime/housekeeping.go` (currently only `fmt`, `regexp`, `strconv`, `strings` from Task 1).

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... -count=1`

Expected: PASS — all runtime and CLI tests pass (the CLI mock now satisfies the extended interface).

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/runtime.go internal/runtime/housekeeping_test.go internal/cli/root_test.go
git commit -m "feat: add runtime SystemDf disk-usage report"
```

---

### Task 3: Runtime cleanup (`Cleanup`)

**Files:**
- Create: `internal/runtime/housekeeping.go` (append)
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface; `internal/runtime/runtime.go` — add stub to `stubManager`
- Modify: `internal/cli/root_test.go:69-100` — add `Cleanup` method to `mockRTForDeploy`
- Test: `internal/runtime/housekeeping_test.go` (append)

**Interfaces:**
- Consumes: `parseSize` from Task 1
- Produces: `type CleanupOptions struct { Containers, Images, Volumes, Networks, Cache bool }`, `type CleanupResult struct { ReclaimedBytes int64 }`, unexported `parsePruneOutput(out string) int64`, and `Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)` on the `runtime.Manager` interface (implemented by both `dockerRuntime` and `stubManager`)

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/housekeeping_test.go (append)
func TestParsePruneOutput(t *testing.T) {
	out := "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 1.2GB"
	want := int64(1288490188) // 1.2 * 1024^3
	if got := parsePruneOutput(out); got != want {
		t.Errorf("parsePruneOutput() = %d, want %d", got, want)
	}
}

func TestParsePruneOutputNoMatch(t *testing.T) {
	if got := parsePruneOutput("nothing to prune"); got != 0 {
		t.Errorf("parsePruneOutput() = %d, want 0", got)
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup error = %v", err)
	}
	if res == nil {
		t.Fatal("Cleanup returned nil result")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestParsePruneOutput|TestStubCleanup" -v -count=1`

Expected: FAIL — `undefined: parsePruneOutput`, `undefined: CleanupOptions`, and `stubManager does not implement Manager (missing method Cleanup)`.

- [ ] **Step 3: Add `Cleanup` to the `Manager` interface and stub**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after `SystemDf`):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
```

Add to `stubManager` (after `SystemDf`):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{}, nil
}
```

- [ ] **Step 4: Add `Cleanup` to `mockRTForDeploy` in the CLI tests**

In `internal/cli/root_test.go`, add after the `SystemDf` method:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) { return &runtime.CleanupResult{}, nil }
```

- [ ] **Step 5: Write the minimal implementation**

Append to `internal/runtime/housekeeping.go`:

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	Cache      bool
}

type CleanupResult struct {
	ReclaimedBytes int64
}

var pruneReclaimedRe = regexp.MustCompile(`(?i)Total reclaimed space:\s*([0-9.]+[kKmMgGtT]?i?[bB])`)

func parsePruneOutput(out string) int64 {
	m := pruneReclaimedRe.FindStringSubmatch(out)
	if len(m) < 2 {
		return 0
	}
	b, err := parseSize(m[1])
	if err != nil {
		return 0
	}
	return b
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	result := &CleanupResult{}
	prunes := []struct {
		enabled bool
		args    []string
	}{
		{opts.Containers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{opts.Images, []string{"image", "prune", "-f"}},
		{opts.Volumes, []string{"volume", "prune", "-f"}},
		{opts.Networks, []string{"network", "prune", "-f"}},
		{opts.Cache, []string{"builder", "prune", "-f"}},
	}
	for _, p := range prunes {
		if !p.enabled {
			continue
		}
		cmd := exec.CommandContext(ctx, "docker", p.args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return result, fmt.Errorf("docker %s: %w\n%s", p.args[0], err, string(out))
		}
		result.ReclaimedBytes += parsePruneOutput(string(out))
	}
	return result, nil
}
```

The `label!=tengiz-app` filter is what protects Tengiz-managed containers (apps, previews, and one-off `run` containers all carry the `tengiz-app` label) from being pruned.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... -count=1`

Expected: PASS — all runtime and CLI tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/runtime.go internal/runtime/housekeeping_test.go internal/cli/root_test.go
git commit -m "feat: add runtime Cleanup with label-protected container pruning"
```

---

### Task 4: CLI `tengiz cleanup` command + documentation

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `README.md:150-152` — insert `### tengiz cleanup` section between `tengiz ps` and `tengiz logs`
- Modify: `AGENTS.md:43-44` — add `tengiz cleanup` line after the `tengiz ps` line

**Interfaces:**
- Consumes: `runtime.Manager` methods `SystemDf`, `Cleanup`, `KeepLastNImages`; `runtime.FormatSize`; `config.NewStoreWithEnv(dataDir, env)` and `Store.ListApps()`; package-level `dataDir` and `getEnv(cmd)` from `internal/cli/root.go`
- Produces: `cleanupCmd` registered on `rootCmd`; package-private `type cleanupOptions`, `type cleanupSelection`, `selectCleanupCategories(opts cleanupOptions) cleanupSelection`, `cleanupDescriptions(sel cleanupSelection) []string`, and `runCleanup(ctx context.Context, rt runtime.Manager, apps []types.AppEntry, opts cleanupOptions) error`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
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

func TestCleanupFlagsRegistered(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"containers", "images", "volumes", "networks", "cache", "all", "force", "keep"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestSelectCleanupCategoriesDefaultsAll(t *testing.T) {
	sel := selectCleanupCategories(cleanupOptions{})
	if !sel.containers || !sel.images || !sel.volumes || !sel.networks || !sel.cache {
		t.Errorf("expected all categories selected by default, got %+v", sel)
	}
}

func TestSelectCleanupCategoriesAll(t *testing.T) {
	sel := selectCleanupCategories(cleanupOptions{all: true, containers: true})
	if !sel.containers || !sel.images || !sel.volumes || !sel.networks || !sel.cache {
		t.Errorf("expected all categories with --all, got %+v", sel)
	}
}

func TestSelectCleanupCategoriesSubset(t *testing.T) {
	sel := selectCleanupCategories(cleanupOptions{cache: true})
	if sel.containers || sel.images || sel.volumes || sel.networks {
		t.Errorf("expected only cache selected, got %+v", sel)
	}
	if !sel.cache {
		t.Error("expected cache selected")
	}
}

func TestRunCleanupDryRun(t *testing.T) {
	rt := runtime.NewStub()
	out := captureOutput(func() {
		if err := runCleanup(context.Background(), rt, nil, cleanupOptions{keep: 5}); err != nil {
			t.Fatalf("runCleanup error = %v", err)
		}
	})
	if !strings.Contains(out, "Dry run") {
		t.Errorf("expected dry-run message, got: %s", out)
	}
	if strings.Contains(out, "Reclaimed") {
		t.Errorf("dry-run should not reclaim, got: %s", out)
	}
}

func TestRunCleanupForce(t *testing.T) {
	rt := runtime.NewStub()
	apps := []types.AppEntry{{Name: "app1"}, {Name: "app2"}}
	out := captureOutput(func() {
		if err := runCleanup(context.Background(), rt, apps, cleanupOptions{force: true, keep: 3}); err != nil {
			t.Fatalf("runCleanup error = %v", err)
		}
	})
	if !strings.Contains(out, "Reclaimed") {
		t.Errorf("expected reclaimed message, got: %s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup|TestSelectCleanupCategories|TestRunCleanup" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd`, `undefined: cleanupOptions`, `undefined: selectCleanupCategories`, `undefined: runCleanup`.

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/cli/cleanup.go
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type cleanupOptions struct {
	containers bool
	images     bool
	volumes    bool
	networks   bool
	cache      bool
	all        bool
	force      bool
	keep       int
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prune unused Docker containers, images, volumes, networks, and build cache.

By default runs in dry-run mode: prints current disk usage and what would be
pruned without deleting anything. Pass --force to actually perform the cleanup.

Tengiz-managed containers (labeled tengiz-app) are always protected and are
never pruned, even when stopped.

Examples:
  tengiz cleanup                       dry-run: show reclaimable space
  tengiz cleanup --force               prune all categories
  tengiz cleanup --force --cache       prune only build cache
  tengiz cleanup --force --keep 10     keep 10 images per app instead of 5`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")
		all, _ := cmd.Flags().GetBool("all")
		force, _ := cmd.Flags().GetBool("force")
		keep, _ := cmd.Flags().GetInt("keep")
		opts := cleanupOptions{
			containers: containers,
			images:     images,
			volumes:    volumes,
			networks:   networks,
			cache:      cache,
			all:        all,
			force:      force,
			keep:       keep,
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		store := config.NewStoreWithEnv(dataDir, env)
		apps, _ := store.ListApps()

		return runCleanup(cmd.Context(), rt, apps, opts)
	},
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune unused containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images and old per-app images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("cache", false, "prune build cache")
	cleanupCmd.Flags().Bool("all", false, "prune all categories (default)")
	cleanupCmd.Flags().BoolP("force", "f", false, "actually perform the cleanup (default is dry-run)")
	cleanupCmd.Flags().Int("keep", 5, "keep last N images per app")
	rootCmd.AddCommand(cleanupCmd)
}

type cleanupSelection struct {
	containers bool
	images     bool
	volumes    bool
	networks   bool
	cache      bool
}

func selectCleanupCategories(opts cleanupOptions) cleanupSelection {
	if opts.all || !(opts.containers || opts.images || opts.volumes || opts.networks || opts.cache) {
		return cleanupSelection{containers: true, images: true, volumes: true, networks: true, cache: true}
	}
	return cleanupSelection{
		containers: opts.containers,
		images:     opts.images,
		volumes:    opts.volumes,
		networks:   opts.networks,
		cache:      opts.cache,
	}
}

func cleanupDescriptions(sel cleanupSelection) []string {
	var out []string
	if sel.containers {
		out = append(out, "unused containers (Tengiz-managed apps are protected)")
	}
	if sel.images {
		out = append(out, "dangling images and old per-app images (keeping last N)")
	}
	if sel.volumes {
		out = append(out, "unused volumes")
	}
	if sel.networks {
		out = append(out, "unused networks")
	}
	if sel.cache {
		out = append(out, "build cache")
	}
	return out
}

func runCleanup(ctx context.Context, rt runtime.Manager, apps []types.AppEntry, opts cleanupOptions) error {
	if opts.keep <= 0 {
		opts.keep = 5
	}

	report, err := rt.SystemDf(ctx)
	if err != nil {
		return fmt.Errorf("disk usage: %w", err)
	}

	sel := selectCleanupCategories(opts)

	fmt.Println("Docker disk usage:")
	fmt.Printf("%-15s %-8s %-8s %-12s %-12s\n", "TYPE", "TOTAL", "ACTIVE", "SIZE", "RECLAIMABLE")
	for _, row := range report.Rows {
		fmt.Printf("%-15s %-8s %-8s %-12s %-12s\n", row.Type, row.Total, row.Active, row.Size, row.Reclaimable)
	}
	fmt.Printf("Total reclaimable: %s\n", runtime.FormatSize(report.ReclaimableBytes()))
	fmt.Println()

	if !opts.force {
		fmt.Println("Dry run: nothing was deleted. Re-run with --force to prune:")
		for _, desc := range cleanupDescriptions(sel) {
			fmt.Printf("  - %s\n", desc)
		}
		return nil
	}

	result, err := rt.Cleanup(ctx, runtime.CleanupOptions{
		Containers: sel.containers,
		Images:     sel.images,
		Volumes:    sel.volumes,
		Networks:   sel.networks,
		Cache:      sel.cache,
	})
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	if sel.images {
		for _, app := range apps {
			if err := rt.KeepLastNImages(ctx, app.Name, opts.keep); err != nil {
				fmt.Printf("[tengiz] warning: image retention for %s: %v\n", app.Name, err)
			}
		}
	}

	fmt.Printf("Reclaimed: %s\n", runtime.FormatSize(result.ReclaimedBytes))
	return nil
}
```

Note: `cleanup.go` registers the command and its flags in its own `init()` (the same pattern `internal/cli/preview.go:83` uses), so flags are present when tests invoke `rootCmd.Execute()` directly.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestSelectCleanupCategories|TestRunCleanup" -v -count=1`

Expected: PASS.

- [ ] **Step 5: Update documentation — README.md**

Insert a new section after the `### tengiz ps` block (after the `Output: NAME, STATE ...` line at `README.md:150`, immediately before `### tengiz logs` at `README.md:152`):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space on a single-server deployment.

By default runs in **dry-run** mode: prints current disk usage (`docker system df`) and what would be pruned without deleting anything. Pass `--force` to actually perform the cleanup.

Tengiz-managed containers (labeled `tengiz-app`) are always protected and are never pruned, even when stopped.

| Flag | Description |
|------|-------------|
| `--containers` | Prune unused (stopped) containers |
| `--images` | Prune dangling images and old per-app images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--cache` | Prune build cache |
| `--all` | Prune all categories (default when no category flag is given) |
| `-f`, `--force` | Actually perform the cleanup (default is dry-run) |
| `--keep N` | Keep last N images per app when pruning images (default 5) |

Examples:
```
tengiz cleanup                     # dry-run: show reclaimable space
tengiz cleanup --force             # prune all categories
tengiz cleanup --force --cache     # prune only the build cache
tengiz cleanup --force --keep 10   # keep 10 images per app instead of 5
```
```

- [ ] **Step 6: Update documentation — AGENTS.md**

In `AGENTS.md`, insert after the `tengiz ps` line (line 43) and before the `tengiz logs` line (line 44):

```
tengiz cleanup [-f] [--containers/--images/--volumes/--networks/--cache] [--keep N] → prune unused Docker resources (dry-run by default)
```

- [ ] **Step 7: Run the full verification suite**

Run:
```bash
go build -o /dev/null .
go vet ./...
go test ./... -count=1
```

Expected: build succeeds, `go vet` reports no issues, and all packages' tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go README.md AGENTS.md
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

## Self-Review

**1. Spec coverage.** Feature #6 ("Docker Housekeeping", P0): "Label-based `docker system prune`. `tengiz cleanup`" → Task 3's `docker container prune -f --filter label!=tengiz-app` implements the label-based protection, and Task 4 adds the `tengiz cleanup` command. "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → all five categories (containers, images, volumes, networks, cache) are pruned by `Cleanup`. Related #56 (granular per-category prune) is satisfied by the `--containers/--images/--volumes/--networks/--cache` flags. Related #103's build-cache half is covered by `--cache` (its git-GC half is out of scope and would be its own plan). Scope check: this plan is a single cohesive subsystem (one CLI command + one runtime capability), so no further split is needed.

**2. Placeholder scan.** No "TBD"/"TODO"/"similar to Task N"/"add validation" placeholders — every code step contains complete, copy-pasteable code and every command lists its exact expected output.

**3. Type consistency.** `parseSize`/`FormatSize` (Task 1) are used by `ReclaimableBytes` and `parsePruneOutput` (Tasks 2–3) and by the CLI via `runtime.FormatSize` (Task 4). `DfRow`/`DfReport`/`SystemDf` (Task 2) match the interface and CLI usage. `CleanupOptions`/`CleanupResult`/`Cleanup` (Task 3) match the interface and CLI usage. `stubManager` (Tasks 2–3) and `mockRTForDeploy` (Tasks 2–3) both gain exactly the two new interface methods so the compile-time interface assertions in `runtime_test.go` (`TestStubSatisfiesInterface`) and `root_test.go` (`TestMockRTForDeployImplementsManager`) keep compiling. The CLI keeps flag names identical between registration (`init()`), parsing (`RunE`), and the test (`TestCleanupFlagsRegistered`). `opts.keep` default of `5` is enforced both by the flag default and the `keep <= 0` guard in `runCleanup`.