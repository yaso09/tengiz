# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, networks, and build cache while preserving all Tengiz-managed resources (labeled `tengiz-app=*`).

**Architecture:** The `runtime.Manager` interface gains a `Cleanup(ctx, opts)` method that runs `docker <subcommand> prune -f` per category and returns per-category results. Tengiz-managed containers are preserved via a `--filter label!=tengiz-app` prune filter. Images prune only dangling (untagged) images — tagged `tengiz-apps/*` images are never touched (old ones are already capped per app by the existing `KeepLastNImages`). A new `tengiz cleanup` Cobra command exposes category flags and `--dry-run`.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` interface, Docker CLI `prune` subcommands (`container`, `image`, `volume`, `network`, `builder`). No new external dependencies.

## Global Constraints

- Tengiz-managed containers carry the `tengiz-app=<name>` label (`internal/runtime/docker.go:76`) and MUST never be pruned — container prune always uses `--filter label!=tengiz-app`
- Tengiz images live in the `tengiz-apps/` repository (`internal/builder/builder.go:61`); image pruning MUST be dangling-only (`docker image prune -f` without `--all`) so tagged images survive
- No new external dependencies
- All Docker calls use `exec.CommandContext(ctx, "docker", args...)` and return wrapped errors including `string(out)` (existing pattern in `internal/runtime/docker.go`)
- Adding `Cleanup` to `runtime.Manager` requires updating both `stubManager` (`internal/runtime/runtime.go`) and `mockRTForDeploy` (`internal/cli/root_test.go:69-100`)
- Tests follow the repo pattern: unit-test pure helper functions and the stub; never execute real `docker` commands in tests
- Category constants and docker subcommand names: `containers`→`container`, `images`→`image`, `volumes`→`volume`, `networks`→`network`, `build-cache`→`builder`
- Existing tests must continue to pass without modification
- `tengiz cleanup` is a global (host-level) operation and does NOT take an `--env` flag

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupCategory`, `CleanupOptions`, `CleanupResult` types; add `Cleanup` to `Manager` interface; add stub implementation |
| `internal/runtime/cleanup.go` | `dockerRuntime.Cleanup` implementation + pure helpers `cleanupCategories`, `cleanupCommandArgs`, `parseReclaimedSpace`, `parseBytes` |
| `internal/runtime/cleanup_test.go` | Tests for stub `Cleanup`, `cleanupCategories`, `cleanupCommandArgs`, `parseReclaimedSpace` |
| `internal/cli/root.go` | New `cleanupCmd` Cobra command + registration in `init()` + `cleanupCategoryFlags`/`humanBytes` helpers |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy`; tests for command registration, flags, and `cleanupCategoryFlags` |
| `README.md` | Document `tengiz cleanup` in CLI Reference |

No new files created beyond the existing test file `cleanup_test.go` (already present). Changes touch 6 existing files.

---

### Task 1: Add cleanup types + `Cleanup` to Manager interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go` — types after `RunOptions` (line 29), interface method after `Run` (line 48), stub method after `Run` (line 121)

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type CleanupCategory string` with constants `CleanupContainers` (`"containers"`), `CleanupImages` (`"images"`), `CleanupVolumes` (`"volumes"`), `CleanupNetworks` (`"networks"`), `CleanupBuildCache` (`"build-cache"`)
  - `var AllCleanupCategories = []CleanupCategory{...}` (all five, in the order above)
  - `type CleanupOptions struct { Categories []CleanupCategory; DryRun bool }` — empty `Categories` means all
  - `type CleanupResult struct { Category CleanupCategory; Reclaimed uint64; DryRun bool; Error string }` with JSON tags
  - `Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupResult, error)` on `Manager`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go — add to existing file
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	results, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if results != nil {
		t.Errorf("Cleanup() = %v, want nil", results)
	}
}

func TestCleanupConstants(t *testing.T) {
	want := []CleanupCategory{
		CleanupContainers,
		CleanupImages,
		CleanupVolumes,
		CleanupNetworks,
		CleanupBuildCache,
	}
	if len(AllCleanupCategories) != len(want) {
		t.Fatalf("AllCleanupCategories len = %d, want %d", len(AllCleanupCategories), len(want))
	}
	for i := range want {
		if AllCleanupCategories[i] != want[i] {
			t.Errorf("AllCleanupCategories[%d] = %q, want %q", i, AllCleanupCategories[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestStubCleanup|TestCleanupConstants" -v -count=1`

Expected: FAIL with `undefined: Cleanup` and `undefined: CleanupCategory`

- [ ] **Step 3: Write minimal implementation in `internal/runtime/runtime.go`**

Add after `RunOptions` (line 29):

```go
type CleanupCategory string

const (
	CleanupContainers CleanupCategory = "containers"
	CleanupImages     CleanupCategory = "images"
	CleanupVolumes    CleanupCategory = "volumes"
	CleanupNetworks   CleanupCategory = "networks"
	CleanupBuildCache CleanupCategory = "build-cache"
)

var AllCleanupCategories = []CleanupCategory{
	CleanupContainers,
	CleanupImages,
	CleanupVolumes,
	CleanupNetworks,
	CleanupBuildCache,
}

type CleanupOptions struct {
	Categories []CleanupCategory
	DryRun     bool
}

type CleanupResult struct {
	Category  CleanupCategory `json:"category"`
	Reclaimed uint64          `json:"reclaimed_bytes"`
	DryRun    bool            `json:"dry_run"`
	Error     string          `json:"error,omitempty"`
}
```

Add to the `Manager` interface after `Run` (line 48):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupResult, error)
```

Add to `stubManager` after `Run` (line 121):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupResult, error) {
	return nil, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestStubCleanup|TestCleanupConstants" -v -count=1`

Expected: PASS

> Note: after this task, `go build ./...` (and `go test ./internal/cli/...`) will fail to compile because `mockRTForDeploy` in `internal/cli/root_test.go` still lacks `Cleanup`. This is expected — Task 3 restores compilation by adding the method. The runtime package tests are the deliverable for this task.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add Cleanup types and method to runtime.Manager interface"
```

---

### Task 2: Implement `dockerRuntime.Cleanup` + pure helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `Cleanup`, `cleanupCategories`, `cleanupCommandArgs`, `parseReclaimedSpace`, `parseBytes`
- Modify: `internal/runtime/cleanup_test.go` — add helper tests

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, `AllCleanupCategories` from Task 1
- Produces:
  - `cleanupCategories(categories []CleanupCategory) []CleanupCategory` — returns `AllCleanupCategories` when empty, else the given slice
  - `cleanupCommandArgs(category CleanupCategory, dryRun bool) []string` — full docker args (without the leading `"docker"`)
  - `parseReclaimedSpace(output string) uint64` — parses "Total reclaimed space: X" line; returns 0 when absent
  - `parseBytes(s string) uint64` — parses `"12.5 MB"` → bytes; returns 0 on parse failure

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go — add to existing file
package runtime

import (
	"context"
	"testing"
)

func TestCleanupCategoriesDefault(t *testing.T) {
	got := cleanupCategories(nil)
	if len(got) != len(AllCleanupCategories) {
		t.Fatalf("cleanupCategories(nil) len = %d, want %d", len(got), len(AllCleanupCategories))
	}
	for i := range got {
		if got[i] != AllCleanupCategories[i] {
			t.Errorf("cleanupCategories(nil)[%d] = %q, want %q", i, got[i], AllCleanupCategories[i])
		}
	}
}

func TestCleanupCategoriesExplicit(t *testing.T) {
	want := []CleanupCategory{CleanupContainers, CleanupImages}
	got := cleanupCategories(want)
	if len(got) != 2 || got[0] != CleanupContainers || got[1] != CleanupImages {
		t.Errorf("cleanupCategories() = %v, want %v", got, want)
	}
}

func TestCleanupCommandArgs(t *testing.T) {
	tests := []struct {
		name     string
		category CleanupCategory
		dryRun   bool
		expected []string
	}{
		{
			name:     "containers",
			category: CleanupContainers,
			expected: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "containers dry run",
			category: CleanupContainers,
			dryRun:   true,
			expected: []string{"container", "prune", "-f", "--dry-run", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "images",
			category: CleanupImages,
			expected: []string{"image", "prune", "-f"},
		},
		{
			name:     "volumes",
			category: CleanupVolumes,
			expected: []string{"volume", "prune", "-f"},
		},
		{
			name:     "networks",
			category: CleanupNetworks,
			expected: []string{"network", "prune", "-f"},
		},
		{
			name:     "build cache",
			category: CleanupBuildCache,
			expected: []string{"builder", "prune", "-f"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanupCommandArgs(tt.category, tt.dryRun)
			if len(got) != len(tt.expected) {
				t.Fatalf("cleanupCommandArgs() = %v, want %v", got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("arg[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected uint64
	}{
		{"empty", "", 0},
		{"no marker", "Deleted Containers:\n", 0},
		{"bytes", "Deleted Containers:\nabc123\n\nTotal reclaimed space: 512 B\n", 512},
		{"kb", "Total reclaimed space: 12.5 kB\n", 12800},
		{"mb", "Total reclaimed space: 1.5 MB\n", 1572864},
		{"gb", "Total reclaimed space: 2 GB\n", 2147483648},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseReclaimedSpace(tt.output)
			if got != tt.expected {
				t.Errorf("parseReclaimedSpace(%q) = %d, want %d", tt.output, got, tt.expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestCleanupCategories|TestCleanupCommandArgs|TestParseReclaimedSpace" -v -count=1`

Expected: FAIL with `undefined: cleanupCategories`, `undefined: cleanupCommandArgs`, `undefined: parseReclaimedSpace`

- [ ] **Step 3: Write minimal implementation in `internal/runtime/cleanup.go`**

Add the following to `internal/runtime/cleanup.go` (after the existing `KeepLastNImages` function). Add `"strconv"` to the import block.

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupResult, error) {
	categories := cleanupCategories(opts.Categories)
	results := make([]CleanupResult, 0, len(categories))
	for _, cat := range categories {
		res := CleanupResult{Category: cat, DryRun: opts.DryRun}
		cmd := exec.CommandContext(ctx, "docker", cleanupCommandArgs(cat, opts.DryRun)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			res.Error = fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out)).Error()
			results = append(results, res)
			continue
		}
		res.Reclaimed = parseReclaimedSpace(string(out))
		results = append(results, res)
	}
	return results, nil
}

func cleanupCategories(categories []CleanupCategory) []CleanupCategory {
	if len(categories) == 0 {
		return AllCleanupCategories
	}
	return categories
}

func cleanupCommandArgs(category CleanupCategory, dryRun bool) []string {
	sub := string(category)
	if category == CleanupBuildCache {
		sub = "builder"
	}
	args := []string{sub, "prune", "-f"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	if category == CleanupContainers {
		args = append(args, "--filter", "label!=tengiz-app")
	}
	return args
}

func parseReclaimedSpace(output string) uint64 {
	const marker = "Total reclaimed space:"
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, marker)
		if idx == -1 {
			continue
		}
		return parseBytes(strings.TrimSpace(line[idx+len(marker):]))
	}
	return 0
}

func parseBytes(s string) uint64 {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return 0
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	switch strings.ToUpper(fields[1]) {
	case "B":
		return uint64(val)
	case "KB", "KIB":
		return uint64(val * 1024)
	case "MB", "MIB":
		return uint64(val * 1024 * 1024)
	case "GB", "GIB":
		return uint64(val * 1024 * 1024 * 1024)
	case "TB", "TIB":
		return uint64(val * 1024 * 1024 * 1024 * 1024)
	}
	return 0
}
```

Update the import block of `internal/runtime/cleanup.go`:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestCleanupCategories|TestCleanupCommandArgs|TestParseReclaimedSpace" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement dockerRuntime.Cleanup with per-category prune helpers"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` (place near `buildLogsCmd`, ~line 1018), helpers `cleanupCategoryFlags`/`humanBytes` (place near `maskSecret`, ~line 1761), registration in `init()` (~line 66)
- Modify: `internal/cli/root_test.go` — add `Cleanup` to `mockRTForDeploy`, add command/flag/helper tests

**Interfaces:**
- Consumes: `runtime.Cleanup`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.CleanupCategory` constants from Tasks 1-2
- Produces:
  - `cleanupCmd *cobra.Command` — `tengiz cleanup [--containers] [--images] [--volumes] [--networks] [--build-cache] [--dry-run]`
  - `cleanupCategoryFlags(cmd *cobra.Command) []runtime.CleanupCategory` — returns non-empty slice only when a category flag is set
  - `humanBytes(n uint64) string` — formats bytes as `B`/`KB`/`MB`/`GB`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/root_test.go — add to existing file
package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, f := range []string{"containers", "images", "volumes", "networks", "build-cache", "dry-run"} {
		if flags.Lookup(f) == nil {
			t.Errorf("cleanupCmd missing --%s flag", f)
		}
	}
}

func TestCleanupCategoryFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	cmd.Flags().Bool("dry-run", false, "")

	if err := cmd.ParseFlags([]string{"--containers", "--build-cache"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	got := cleanupCategoryFlags(cmd)
	want := []runtime.CleanupCategory{runtime.CleanupContainers, runtime.CleanupBuildCache}
	if len(got) != len(want) {
		t.Fatalf("cleanupCategoryFlags() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cleanupCategoryFlags()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCleanupCategoryFlagsNone(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	cmd.Flags().Bool("dry-run", false, "")

	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	got := cleanupCategoryFlags(cmd)
	if len(got) != 0 {
		t.Errorf("cleanupCategoryFlags() = %v, want empty", got)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		input    uint64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{2048, "2.0 KB"},
		{3 * 1024 * 1024, "3.0 MB"},
		{5 * 1024 * 1024 * 1024, "5.0 GB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.input); got != tt.expected {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanup|TestHumanBytes" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: cleanupCategoryFlags`, `undefined: humanBytes`, and a compile error for `mockRTForDeploy` no longer satisfying `runtime.Manager`

- [ ] **Step 3: Update `mockRTForDeploy` in `internal/cli/root_test.go`**

Add after the `Run` method (line 100):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) ([]runtime.CleanupResult, error) {
	return nil, nil
}
```

The `context` and `runtime` packages are already imported in `root_test.go`.

- [ ] **Step 4: Add `cleanupCmd` + helpers to `internal/cli/root.go`**

Add the command (place after `buildLogsCmd`, around line 1090):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Prune unused Docker containers, images, volumes, networks, and build cache.

Tengiz-managed containers (labeled tengiz-app=*) are always preserved.
Only dangling (untagged) images are removed, so tagged Tengiz images survive.
Old Tengiz image versions are capped per app automatically at deploy time.

Use category flags to clean specific resources. With no flags, all categories run.
Use --dry-run to preview what would be reclaimed without deleting anything.

Examples:
  tengiz cleanup
  tengiz cleanup --containers --images
  tengiz cleanup --dry-run`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		results, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			Categories: cleanupCategoryFlags(cmd),
			DryRun:     dryRun,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if len(results) == 0 {
			fmt.Println("[tengiz] nothing to clean")
			return nil
		}

		fmt.Printf("%-12s %-14s %s\n", "CATEGORY", "RECLAIMED", "STATUS")
		for _, res := range results {
			status := "done"
			if res.Error != "" {
				status = "error: " + res.Error
			} else if res.DryRun {
				status = "would reclaim (dry-run)"
			}
			fmt.Printf("%-12s %-14s %s\n", res.Category, humanBytes(res.Reclaimed), status)
		}
		return nil
	},
}
```

Add the helper functions (place near `maskSecret`, around line 1761):

```go
func cleanupCategoryFlags(cmd *cobra.Command) []runtime.CleanupCategory {
	var categories []runtime.CleanupCategory
	for _, c := range []struct {
		name string
		cat  runtime.CleanupCategory
	}{
		{"containers", runtime.CleanupContainers},
		{"images", runtime.CleanupImages},
		{"volumes", runtime.CleanupVolumes},
		{"networks", runtime.CleanupNetworks},
		{"build-cache", runtime.CleanupBuildCache},
	} {
		if on, _ := cmd.Flags().GetBool(c.name); on {
			categories = append(categories, c.cat)
		}
	}
	return categories
}

func humanBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
```

Add registration and flags in `init()` (after the `secretCmd` block, line 69):

```go
	rootCmd.AddCommand(cleanupCmd)
```

```go
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers (preserves Tengiz-managed containers)")
	cleanupCmd.Flags().Bool("images", false, "prune dangling (untagged) images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune BuildKit build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be reclaimed without deleting")
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -run "TestCleanup|TestHumanBytes" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all CLI tests + build**

Run: `go test ./internal/cli/... -v -count=1`
Run: `go build ./...`

Expected: All PASS, build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 4: Documentation + full verification + self-review

**Files:**
- Modify: `README.md` — add `tengiz cleanup` section to CLI Reference (after `tengiz rm`, ~line 228)

**Interfaces:**
- Consumes: final CLI surface from Task 3
- Produces: user-facing documentation

- [ ] **Step 1: Add `tengiz cleanup` documentation to `README.md`**

Insert after the `### tengiz rm <app>` section (line 228):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers (Tengiz-managed containers are always preserved) |
| `--images` | Prune dangling (untagged) images only — tagged `tengiz-apps/*` images are never removed |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune BuildKit build cache |
| `--dry-run` | Preview what would be reclaimed without deleting anything |

With no category flags, all categories run. Tengiz-managed containers are protected by their `tengiz-app=*` label, and old Tengiz image versions are capped per app automatically at deploy time.

Examples:
```
tengiz cleanup
tengiz cleanup --containers --images
tengiz cleanup --dry-run
```
```

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except known time-sensitive proxy/idle tests)

- [ ] **Step 3: Run static analysis**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 4: Self-review against spec**

Check against `docs/FUTURES_FEATURES.md` feature #6 (Docker Housekeeping):
- `tengiz cleanup` command ✅ (Task 3)
- Unused volume, network, container, and image cleanup ✅ (Tasks 1-2 — five categories)
- Label-based filtering protects Tengiz-managed containers ✅ (Task 2 — `--filter label!=tengiz-app`)
- Disk-space reclamation reporting (reclaimed bytes per category) ✅ (Tasks 2-3 — `parseReclaimedSpace` + CLI table)
- Helper-container/build-cache cleanup ✅ (Task 2 — `builder` prune category)

Placeholder scan: no "TBD", "TODO", "implement later", or "Similar to Task" patterns; every code step contains complete code.

Type consistency check:
- `runtime.Cleanup(ctx, opts CleanupOptions) ([]CleanupResult, error)` — identical signature on interface, `dockerRuntime`, `stubManager`, and `mockRTForDeploy`
- `runtime.CleanupCategory` constants `CleanupContainers`/`CleanupImages`/`CleanupVolumes`/`CleanupNetworks`/`CleanupBuildCache` — consistent between Task 1 constants, Task 2 `cleanupCommandArgs`, and Task 3 `cleanupCategoryFlags`
- `cleanupCategoryFlags(cmd *cobra.Command) []runtime.CleanupCategory` — same signature in Task 3 test and implementation
- `humanBytes(uint64) string` — same in test and implementation
- `CleanupOptions{Categories, DryRun}` field names match between Tasks 1, 2, and 3

- [ ] **Step 5: Manual smoke test (requires Docker)**

Run: `go build -o tengiz . && ./tengiz cleanup --dry-run`

Expected: Prints the CATEGORY/RECLAIMED/STATUS table with `would reclaim (dry-run)` statuses (or `nothing to clean`).

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```
