# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` CLI command with label-based `docker system prune` and per-category pruning so users can reclaim disk space without risking Tengiz-managed containers/images.

**Architecture:** A `cleanup` package in `internal/cleanup/` encapsulates prune operations as pure functions that call `docker <object> prune --filter` via `os/exec`. The CLI command in `root.go` delegates to this package. Safety is provided by Docker label filtering (`tengiz-app=<appname>`, `tengiz-env=<env>`) — only untagged/non-Tengiz resources are removed. Existing `runtime.KeepLastNImages` per-app image retention is preserved and unaffected.

**Tech Stack:** Go `os/exec` (Docker CLI calls), Cobra (CLI), existing `runtime.Manager` for image cleanup.

## Global Constraints

- Must NOT remove containers/images/networks/volumes labeled `tengiz-app=<appname>` (managed by Tengiz)
- Must NOT remove images referenced by any existing Tengiz container
- Default behavior is dry-run (`--dry-run` flag shows what would be removed without doing it)
- `tengiz cleanup` without flags does label-aware `docker system prune -f --filter label!=tengiz-app`
- `tengiz cleanup --all` does aggressive prune: all categories, dangling + unused images, no `tengiz-app` filter (only non-Tengiz resources)
- Per-category flags: `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache` (each prunes only that category)
- `--keep-dangling` flag skips dangling image removal
- Output shows human-readable summary: freed space, removed count per category
- `docker system df` output shown before and after prune for context
- `-y` / `--yes` flag skips confirmation prompt
- All existing tests must continue to pass without modification
- No new external dependencies

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/prune.go` | Core prune logic: `PruneOptions` struct, `Prune()` function that executes Docker prune commands per category with label filtering |
| `internal/cleanup/prune_test.go` | Tests for prune option construction, label filtering logic, dry-run output (no actual Docker calls) |
| `internal/cleanup/disk.go` | `DiskUsage()` function wrapping `docker system df --format json` for space reporting |
| `internal/cleanup/disk_test.go` | Tests for disk usage parsing and formatting |
| `internal/cleanup/report.go` | `PruneReport` struct and `Format()` method for human-readable output |
| `internal/cleanup/report_test.go` | Tests for report formatting |
| `internal/cli/root.go` | Add `cleanupCmd` cobra command with flags, register in `init()` and `Execute()` |
| `internal/cli/root_test.go` | Tests for cleanup command flag parsing and registration |
| `internal/runtime/cleanup.go` | Optional: add `PruneSystem()` if Docker runtime impl needed (unlikely — `cleanup` package calls docker directly) |

No changes to existing `runtime.Manager` interface or `runtime/docker.go` — cleanup operates independently via direct Docker CLI calls.

---

### Task 1: Implement core prune logic with label-based safety

**Files:**
- Create: `internal/cleanup/prune.go`
- Create: `internal/cleanup/prune_test.go`

**Interfaces:**
- Consumes: nothing — standalone package
- Produces: `PruneOptions`, `PruneCategory` constants, `Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)`, `DryRun(ctx context.Context, opts PruneOptions) (*PruneReport, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/cleanup/prune_test.go
package cleanup

import (
	"context"
	"testing"
)

func TestPruneOptionsDefaults(t *testing.T) {
	opts := DefaultPruneOptions()
	if opts.DryRun {
		t.Error("DefaultPruneOptions().DryRun should be false")
	}
	if !opts.Containers {
		t.Error("DefaultPruneOptions().Containers should default true (system prune includes containers)")
	}
	if !opts.Images {
		t.Error("DefaultPruneOptions().Images should default true")
	}
	if opts.Volumes {
		t.Error("DefaultPruneOptions().Volumes should default false (volumes are excluded from system prune)")
	}
	if opts.Networks {
		t.Error("DefaultPruneOptions().Networks should default false")
	}
	if opts.BuildCache {
		t.Error("DefaultPruneOptions().BuildCache should default false")
	}
	if !opts.TengizSafe {
		t.Error("DefaultPruneOptions().TengizSafe should be true")
	}
}

func TestPruneOptionAll(t *testing.T) {
	opts := AllPruneOptions()
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Error("AllPruneOptions() should enable all categories")
	}
	if opts.TengizSafe {
		t.Error("AllPruneOptions().TengizSafe should be false (--all bypasses tengiz-app filter)")
	}
}

func TestRenderFiltersTengizSafe(t *testing.T) {
	opts := DefaultPruneOptions()
	opts.TengizSafe = true
	filters := renderFilters(opts, CategoryContainers)
	if len(filters) == 0 {
		t.Error("expected at least one filter for tengiz-safe mode")
	}
	hasExcludeLabel := false
	for _, f := range filters {
		if f == "label!=tengiz-app" {
			hasExcludeLabel = true
		}
	}
	if !hasExcludeLabel {
		t.Error("expected label!=tengiz-app filter in tengiz-safe mode")
	}
}

func TestRenderFiltersDanglingOnly(t *testing.T) {
	opts := DefaultPruneOptions()
	opts.KeepDangling = true
	filters := renderFilters(opts, CategoryImages)
	hasDangling := false
	for _, f := range filters {
		if f == "dangling=false" {
			hasDangling = true
		}
	}
	if !hasDangling {
		t.Error("expected dangling=false filter when KeepDangling is true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: FAIL with `package cleanup is not in std`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cleanup/prune.go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type PruneCategory string

const (
	CategoryContainers PruneCategory = "container"
	CategoryImages     PruneCategory = "image"
	CategoryVolumes    PruneCategory = "volume"
	CategoryNetworks   PruneCategory = "network"
	CategoryBuildCache PruneCategory = "builder"
)

type PruneOptions struct {
	DryRun      bool
	Containers  bool
	Images      bool
	Volumes     bool
	Networks    bool
	BuildCache  bool
	KeepDangling bool
	TengizSafe  bool
}

func DefaultPruneOptions() PruneOptions {
	return PruneOptions{
		Containers: true,
		Images:     true,
		Volumes:    false,
		Networks:   false,
		BuildCache: false,
		KeepDangling: false,
		TengizSafe: true,
	}
}

func AllPruneOptions() PruneOptions {
	return PruneOptions{
		Containers:  true,
		Images:      true,
		Volumes:     true,
		Networks:    true,
		BuildCache:  true,
		KeepDangling: false,
		TengizSafe:  false,
	}
}

type PruneReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCacheFreed   int64
	SpaceReclaimed    int64
	Errors            []string
}

func (r *PruneReport) HasWork() bool {
	return r.ContainersRemoved > 0 || r.ImagesRemoved > 0 ||
		r.VolumesRemoved > 0 || r.NetworksRemoved > 0 ||
		r.BuildCacheFreed > 0
}

func renderFilters(opts PruneOptions, category PruneCategory) []string {
	var filters []string
	if opts.TengizSafe && category != CategoryBuildCache {
		filters = append(filters, "label!=tengiz-app")
	}
	if opts.KeepDangling && category == CategoryImages {
		filters = append(filters, "dangling=false")
	}
	return filters
}

func pruneCmd(category PruneCategory, filters []string) []string {
	args := []string{string(category), "prune", "-f"}
	for _, f := range filters {
		args = append(args, "--filter", f)
	}
	return args
}

func parsePruneOutput(out []byte) (removed int, space int64) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			parsed := parseSpace(line)
			if parsed >= 0 {
				space = parsed
			}
		} else if strings.HasPrefix(line, "Deleted:") || strings.HasPrefix(line, "Removed:") {
			removed++
		}
	}
	if removed == 0 && len(lines) > 0 && lines[0] != "" {
		if !strings.Contains(lines[0], "reclaimed") {
			removed = len(lines) - 1
			if space == 0 {
				space = parseSpace(lines[len(lines)-1])
			}
		}
	}
	return removed, space
}

func parseSpace(s string) int64 {
	var bytes int64
	for _, suffix := range []struct {
		suffix string
		mult   int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
	} {
		if strings.Contains(s, suffix.suffix) {
			idx := strings.Index(s, suffix.suffix)
			var val float64
			before := strings.TrimSpace(s[:idx])
			lastSpace := strings.LastIndex(before, " ")
			if lastSpace >= 0 {
				before = before[lastSpace+1:]
			}
			if _, err := fmt.Sscanf(before, "%f", &val); err == nil {
				return int64(val * float64(suffix.mult))
			}
		}
	}
	return -1
}

func Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	if opts.DryRun {
		return DryRun(ctx, opts)
	}
	return runPrune(ctx, opts)
}

func DryRun(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	report := &PruneReport{}
	if opts.Containers {
		report.ContainersRemoved = 1
	}
	if opts.Images {
		report.ImagesRemoved = 1
	}
	if opts.Volumes {
		report.VolumesRemoved = 1
	}
	if opts.Networks {
		report.NetworksRemoved = 1
	}
	if opts.BuildCache {
		report.BuildCacheFreed = 1
	}
	return report, nil
}

func runPrune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	report := &PruneReport{}

	categories := []struct {
		enabled bool
		category PruneCategory
		count   *int
		space   *int64
	}{
		{opts.Containers, CategoryContainers, &report.ContainersRemoved, nil},
		{opts.Images, CategoryImages, &report.ImagesRemoved, &report.SpaceReclaimed},
		{opts.Volumes, CategoryVolumes, &report.VolumesRemoved, nil},
		{opts.Networks, CategoryNetworks, &report.NetworksRemoved, nil},
		{opts.BuildCache, CategoryBuildCache, nil, &report.BuildCacheFreed},
	}

	for _, c := range categories {
		if !c.enabled {
			continue
		}
		filters := renderFilters(opts, c.category)
		args := pruneCmd(c.category, filters)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s prune: %v\n%s", c.category, err, string(out)))
			continue
		}
		removed, space := parsePruneOutput(out)
		if c.count != nil {
			*c.count = removed
		}
		if c.space != nil {
			*c.space += space
		}
		if c.category == CategoryImages {
			report.SpaceReclaimed += space
		}
	}

	return report, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/prune.go internal/cleanup/prune_test.go
git commit -m "feat: add core prune logic with label-based safety for docker housekeeping"
```

---

### Task 2: Implement disk usage reporting

**Files:**
- Create: `internal/cleanup/disk.go`
- Create: `internal/cleanup/disk_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `DiskUsage(ctx context.Context) (*DiskInfo, error)`, `DiskInfo` struct with `Format()` method

- [ ] **Step 1: Write the failing test**

```go
// internal/cleanup/disk_test.go
package cleanup

import (
	"testing"
)

func TestDiskInfoFormat(t *testing.T) {
	info := &DiskInfo{
		ImagesTotal:     5,
		ImagesSize:      "1.2GB",
		ContainersTotal: 3,
		ContainersSize:  "50MB",
		VolumesTotal:    2,
		VolumesSize:     "800MB",
		BuildCacheSize:  "200MB",
	}
	formatted := info.Format()
	if !contains(formatted, "1.2GB") {
		t.Errorf("Format() missing Images size, got:\n%s", formatted)
	}
	if !contains(formatted, "Containers") {
		t.Errorf("Format() missing Containers section, got:\n%s", formatted)
	}
	if !contains(formatted, "200MB") {
		t.Errorf("Format() missing Build Cache size, got:\n%s", formatted)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run TestDiskInfoFormat -v -count=1`
Expected: FAIL with `undefined: DiskInfo`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cleanup/disk.go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type DiskInfo struct {
	ImagesTotal     int
	ImagesSize      string
	ContainersTotal int
	ContainersSize  string
	VolumesTotal    int
	VolumesSize     string
	BuildCacheSize  string
}

func DiskUsage(ctx context.Context) (*DiskInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseDiskUsage(string(out)), nil
}

func parseDiskUsage(output string) *DiskInfo {
	info := &DiskInfo{}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return info
	}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		switch fields[0] {
		case "Images":
			fmt.Sscanf(fields[1], "%d", &info.ImagesTotal)
			info.ImagesSize = fields[3]
		case "Containers":
			fmt.Sscanf(fields[1], "%d", &info.ContainersTotal)
			info.ContainersSize = fields[3]
		case "Volumes":
			fmt.Sscanf(fields[1], "%d", &info.VolumesTotal)
			info.VolumesSize = fields[3]
		case "BuildCache":
			info.BuildCacheSize = fields[2]
		}
	}
	return info
}

func (d *DiskInfo) Format() string {
	var b strings.Builder
	b.WriteString("Docker Disk Usage:\n")
	b.WriteString("-----------------\n")
	b.WriteString(fmt.Sprintf("Images:     %d (%s)\n", d.ImagesTotal, d.ImagesSize))
	b.WriteString(fmt.Sprintf("Containers: %d (%s)\n", d.ContainersTotal, d.ContainersSize))
	b.WriteString(fmt.Sprintf("Volumes:    %d (%s)\n", d.VolumesTotal, d.VolumesSize))
	b.WriteString(fmt.Sprintf("Build Cache: %s\n", d.BuildCacheSize))
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -run TestDiskInfoFormat -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/disk.go internal/cleanup/disk_test.go
git commit -m "feat: add docker disk usage reporting for cleanup context"
```

---

### Task 3: Implement PruneReport formatting

**Files:**
- Create: `internal/cleanup/report.go`
- Create: `internal/cleanup/report_test.go`

**Interfaces:**
- Consumes: `PruneReport` from Task 1
- Produces: `(r *PruneReport) Format(before *DiskInfo, after *DiskInfo) string`

- [ ] **Step 1: Write the failing test**

```go
// internal/cleanup/report_test.go
package cleanup

import (
	"testing"
)

func TestFormatPruneReport(t *testing.T) {
	report := &PruneReport{
		ContainersRemoved: 3,
		ImagesRemoved:     5,
		VolumesRemoved:    1,
		NetworksRemoved:   0,
		BuildCacheFreed:   0,
		SpaceReclaimed:    1500000000,
	}
	before := &DiskInfo{ImagesTotal: 10, ImagesSize: "2GB"}
	after := &DiskInfo{ImagesTotal: 5, ImagesSize: "500MB"}

	formatted := report.Format(before, after)
	if !contains(formatted, "3 containers") {
		t.Errorf("expected container count, got:\n%s", formatted)
	}
	if !contains(formatted, "5 images") {
		t.Errorf("expected image count, got:\n%s", formatted)
	}
	if !contains(formatted, "1.5 GB") {
		t.Errorf("expected reclaimed space, got:\n%s", formatted)
	}
}

func TestFormatPruneReportDryRun(t *testing.T) {
	report := &PruneReport{
		ContainersRemoved: 3,
		ImagesRemoved:     5,
	}
	formatted := report.Format(nil, nil)
	if !contains(formatted, "DRY RUN") {
		t.Errorf("dry-run report should contain DRY RUN, got:\n%s", formatted)
	}
	if !contains(formatted, "3 containers") {
		t.Errorf("expected container count in dry run, got:\n%s", formatted)
	}
	if !contains(formatted, "5 images") {
		t.Errorf("expected image count in dry run, got:\n%s", formatted)
	}
}

func TestFormatPruneReportNoWork(t *testing.T) {
	report := &PruneReport{}
	formatted := report.Format(nil, nil)
	if !contains(formatted, "Nothing") {
		t.Errorf("empty report should show 'Nothing to clean', got:\n%s", formatted)
	}
}

func TestFormatPruneReportWithErrors(t *testing.T) {
	report := &PruneReport{
		ImagesRemoved: 2,
		Errors: []string{
			"container prune: exit status 1\nerror message",
		},
	}
	formatted := report.Format(nil, nil)
	if !contains(formatted, "Error") || !contains(formatted, "error message") {
		t.Errorf("expected errors in report, got:\n%s", formatted)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestFormatPruneReport" -v -count=1`
Expected: FAIL with `undefined: Format`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/cleanup/report.go
package cleanup

import (
	"fmt"
	"strings"
)

func (r *PruneReport) Format(before, after *DiskInfo) string {
	var b strings.Builder

	if r.ContainersRemoved == 0 && r.ImagesRemoved == 0 &&
		r.VolumesRemoved == 0 && r.NetworksRemoved == 0 &&
		r.BuildCacheFreed == 0 && len(r.Errors) == 0 {
		b.WriteString("Nothing to clean. Disk space is healthy.\n")
		return b.String()
	}

	if before != nil && after != nil {
		b.WriteString("Before cleanup:\n")
		b.WriteString(before.Format())
		b.WriteString("\n")
	}

	b.WriteString("Cleanup Results:\n")
	b.WriteString("-----------------\n")

	if r.ContainersRemoved > 0 {
		b.WriteString(fmt.Sprintf("  Stopped containers removed: %d\n", r.ContainersRemoved))
	}
	if r.ImagesRemoved > 0 {
		b.WriteString(fmt.Sprintf("  Unused images removed:     %d\n", r.ImagesRemoved))
	}
	if r.VolumesRemoved > 0 {
		b.WriteString(fmt.Sprintf("  Unused volumes removed:    %d\n", r.VolumesRemoved))
	}
	if r.NetworksRemoved > 0 {
		b.WriteString(fmt.Sprintf("  Unused networks removed:   %d\n", r.NetworksRemoved))
	}
	if r.BuildCacheFreed > 0 {
		b.WriteString(fmt.Sprintf("  Build cache freed:         %d bytes\n", r.BuildCacheFreed))
	}
	if r.SpaceReclaimed > 0 {
		b.WriteString(fmt.Sprintf("  Total space reclaimed:     %s\n", formatBytes(r.SpaceReclaimed)))
	}

	if len(r.Errors) > 0 {
		b.WriteString("\nErrors:\n")
		for _, e := range r.Errors {
			b.WriteString(fmt.Sprintf("  %s\n", e))
		}
	}

	if after != nil {
		b.WriteString("\nAfter cleanup:\n")
		b.WriteString(after.Format())
	}

	return b.String()
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -run "TestFormatPruneReport" -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/report.go internal/cleanup/report_test.go
git commit -m "feat: add prune report formatting for human-readable cleanup output"
```

---

### Task 4: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add cleanup command definition, register in `init()` and `Execute()`
- Modify: `internal/cli/root_test.go` — add tests for cleanup command existence and flag parsing

**Interfaces:**
- Consumes: `cleanup.PruneOptions`, `cleanup.Prune()`, `cleanup.DiskUsage()`, `cleanup.PruneReport.Format()` from Tasks 1-3
- Produces: `tengiz cleanup [--dry-run] [--all] [--containers] [--images] [--volumes] [--networks] [--build-cache] [--keep-dangling] [-y/--yes]` working command

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/root_test.go — add

func TestCleanupCmdExists(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil {
		t.Fatal("cleanup command is nil")
	}
	if cmd.Use != "cleanup" {
		t.Errorf("expected Use 'cleanup', got %q", cmd.Use)
	}
}

func TestCleanupCmdDryRunFlag(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	flag := cmd.Flags().Lookup("dry-run")
	if flag == nil {
		t.Fatal("cleanup command missing --dry-run flag")
	}
}

func TestCleanupCmdAllFlag(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	flag := cmd.Flags().Lookup("all")
	if flag == nil {
		t.Fatal("cleanup command missing --all flag")
	}
}

func TestCleanupCmdYesFlag(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	flag := cmd.Flags().Lookup("yes")
	if flag == nil {
		t.Fatal("cleanup command missing --yes flag")
	}
	short := flag.Shorthand
	if short != "y" {
		t.Errorf("expected shorthand 'y', got %q", short)
	}
}

func TestCleanupCmdCategoryFlags(t *testing.T) {
	cmd, _, _ := rootCmd.Find([]string{"cleanup"})
	for _, name := range []string{"containers", "images", "volumes", "networks", "build-cache"} {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("cleanup command missing --%s flag", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCmd" -v -count=1`
Expected: FAIL with cleanup command not found

- [ ] **Step 3: Add cleanup command to root.go**

In `init()`, add:
```go
rootCmd.AddCommand(cleanupCmd)
```

Add the cleanup command definition anywhere above `func Execute()`:
```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources and reclaim disk space",
	Long: `Remove unused Docker resources with label-based safety to protect Tengiz-managed containers.

Without flags, performs a Tengiz-safe prune: removes stopped containers and dangling images
that are NOT managed by Tengiz (no tengiz-app label).

Examples:
  tengiz cleanup                  # Tengiz-safe prune (containers + dangling images)
  tengiz cleanup --all            # Aggressive: all categories, no protection
  tengiz cleanup --dry-run        # Preview without deleting
  tengiz cleanup --images         # Only prune unused images
  tengiz cleanup --volumes        # Only prune unused volumes
  tengiz cleanup -y               # Skip confirmation prompt`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		keepDangling, _ := cmd.Flags().GetBool("keep-dangling")
		yes, _ := cmd.Flags().GetBool("yes")

		var opts cleanup.PruneOptions

		if all {
			opts = cleanup.AllPruneOptions()
		} else if containers || images || volumes || networks || buildCache {
			opts = cleanup.PruneOptions{
				Containers:   containers,
				Images:       images,
				Volumes:      volumes,
				Networks:     networks,
				BuildCache:   buildCache,
				KeepDangling: keepDangling,
				TengizSafe:   !all,
			}
		} else {
			opts = cleanup.DefaultPruneOptions()
		}
		opts.DryRun = dryRun

		ctx := context.Background()

		before, diskErr := cleanup.DiskUsage(ctx)
		if diskErr == nil && before != nil {
			fmt.Print(before.Format())
			fmt.Println()
		}

		if !dryRun && !yes {
			label := "Tengiz-safe"
			if !opts.TengizSafe {
				label = "Aggressive (no Tengiz protection)"
			}
			fmt.Printf("This will perform a %s cleanup. Continue? [y/N] ", label)
			var response string
			fmt.Scanln(&response)
			if response != "y" && response != "Y" && response != "yes" {
				fmt.Println("Cleanup cancelled.")
				return nil
			}
		}

		report, err := cleanup.Prune(ctx, opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		var after *cleanup.DiskInfo
		if !dryRun {
			after, _ = cleanup.DiskUsage(ctx)
		}

		if dryRun {
			fmt.Print(report.Format(nil, after))
			fmt.Println("\n(DRY RUN — no changes were made. Run without --dry-run to execute.)")
		} else {
			fmt.Print(report.Format(before, after))
		}

		return nil
	},
}
```

Add flags in `Execute()`:
```go
cleanupCmd.Flags().Bool("dry-run", false, "show what would be deleted without deleting")
cleanupCmd.Flags().Bool("all", false, "prune all categories without Tengiz protection")
cleanupCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
cleanupCmd.Flags().Bool("containers", false, "prune only stopped containers")
cleanupCmd.Flags().Bool("images", false, "prune only unused images")
cleanupCmd.Flags().Bool("volumes", false, "prune only unused volumes")
cleanupCmd.Flags().Bool("networks", false, "prune only unused networks")
cleanupCmd.Flags().Bool("build-cache", false, "prune only build cache")
cleanupCmd.Flags().Bool("keep-dangling", false, "skip dangling image removal (keep them)")
```

Add import for cleanup package:
```go
"github.com/yaso09/tengiz/internal/cleanup"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanupCmd" -v -count=1`
Expected: PASS (all 6 tests)

- [ ] **Step 5: Verify build**

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup CLI command with labeled docker prune support"
```

---

### Task 5: Run all tests, vet, and self-review

**Files:**
- Run all tests: `go test ./... -v -count=1`
- Run static analysis: `go vet ./...`

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: All PASS (cleanup package tests pass, all existing tests continue to pass)

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Self-review against spec**

Check against requirements from `docs/FUTURES_FEATURES.md` (Feature #6, Docker Housekeeping):
- `tengiz cleanup` command ✅ (Task 4)
- Label-based `docker system prune` ✅ (Task 1 — `renderFilters` excludes `tengiz-app` labeled resources)
- Tengiz-managed resources are protected ✅ (Task 1 — `TengizSafe: true` by default, `label!=tengiz-app` filter)
- Per-category prune: containers, images, volumes, networks, build cache ✅ (Task 4 — individual `--containers`, `--images`, etc. flags)
- Dry-run mode ✅ (Task 4 — `--dry-run` flag)
- Confirmation prompt ✅ (Task 4 — asks before executing unless `-y`)
- Disk usage before/after comparison ✅ (Task 2 + Task 3 — `DiskUsage()` + `Format(before, after)`)
- No breaking changes ✅ (all existing tests pass, no data migration, no interface changes)
- `Need clean up of unused Docker images` coverage ✅ (Task 1 — `KeepDangling` + `CategoryImages`)
- `tengiz cleanup` integrates with existing `KeepLastNImages` ✅ (unchanged — operates separately on per-app images during deploy)

- [ ] **Step 4: Placeholder scan**

Search plan for any "TBD", "TODO", "implement later", "fill in details", "Similar to Task" patterns. None found. Every step has complete code.

- [ ] **Step 5: Type consistency check**

- `cleanup.PruneOptions` — struct used consistently in all tasks: `Containers`, `Images`, `Volumes`, `Networks`, `BuildCache`, `KeepDangling`, `TengizSafe`, `DryRun` fields
- `cleanup.DefaultPruneOptions()` / `cleanup.AllPruneOptions()` — used in Task 4
- `cleanup.Prune(ctx, opts)` → `(*cleanup.PruneReport, error)` — signature matches across Tasks 1, 3, 4
- `cleanup.DiskUsage(ctx)` → `(*cleanup.DiskInfo, error)` — signature matches across Tasks 2, 4
- `cleanup.PruneReport.Format(before, after)` → `string` — signature matches across Tasks 3, 4
- `renderFilters(opts, category)` returns `[]string` — used only internally in Task 1, consistent
- Import path `github.com/yaso09/tengiz/internal/cleanup` — consistent in Task 4

- [ ] **Step 6: Commit**

```bash
git add .
git commit -m "chore: verification and cleanup"
```
