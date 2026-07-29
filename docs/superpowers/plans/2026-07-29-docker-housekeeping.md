# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command and runtime methods for safe, label-based Docker resource pruning — containers, dangling images, unused volumes, and build cache.

**Architecture:** Extend the existing `runtime.Manager` interface with a single `Cleanup` method that runs Docker prune commands (container prune / image prune / volume prune / builder prune) with label-based filters to protect Tengiz-managed resources. Expose via `tengiz cleanup` CLI with `--containers`, `--images`, `--volumes`, `--cache`, `--dry-run` flags.

**Tech Stack:** Go 1.26, Cobra CLI, `os/exec` for Docker CLI, label-based filtering (`label!=tengiz-app`)

## Global Constraints

- All Docker commands use `os/exec.CommandContext` (no Docker SDK)
- Container label keys: `tengiz-app` and `tengiz-env` (existing constants `labelKey`/`envLabelKey` in `docker.go`)
- Image naming pattern: `tengiz-apps/{appName}:{env}-{deploymentID}`
- Data directory: `~/.tengiz/` (env-scoped files)
- CLI command registration pattern: `rootCmd.AddCommand(cmd)` in `init()` with flags set in `Execute()`
- Stub manager for tests returns `nil` for all methods (no-op)

---
### File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/types/types.go` | Modify | Add `CleanupOptions` and `CleanupReport` types |
| `internal/runtime/runtime.go` | Modify | Add `Cleanup` to `Manager` interface + stub impl |
| `internal/runtime/cleanup.go` | Modify | Add `Cleanup` method on `dockerRuntime` — all prune logic |
| `internal/runtime/cleanup_test.go` | Create | Unit tests for `Cleanup` method |
| `internal/cli/cleanup.go` | Create | `tengiz cleanup` cobra command with flags |
| `internal/cli/root.go` | Modify | Register `cleanupCmd` in `init()` |

### Task N: Add Cleanup Types

**Files:**
- Modify: `internal/types/types.go:1-186`

**Interfaces:**
- Produces: `types.CleanupOptions{All, Containers, Images, Volumes, BuildCache, DryRun bool}` and `types.CleanupReport{ContainersRemoved, ImagesRemoved, VolumesRemoved int; BuildCacheFreed, TotalSpaceFreed int64; DryRun bool}`

- [ ] **Step 1: Add types to types.go**

Add before the closing of the file (before line 186):

```go
type CleanupOptions struct {
	All        bool
	Containers bool
	Images     bool
	Volumes    bool
	BuildCache bool
	DryRun     bool
}

type CleanupReport struct {
	ContainersRemoved int   `json:"containers_removed"`
	ImagesRemoved     int   `json:"images_removed"`
	VolumesRemoved    int   `json:"volumes_removed"`
	BuildCacheFreed   int64 `json:"build_cache_freed_bytes"`
	TotalSpaceFreed   int64 `json:"total_space_freed_bytes"`
	DryRun            bool  `json:"dry_run"`
}
```

- [ ] **Step 3: Write tests for the types**

In `internal/types/types_test.go`:

```go
func TestCleanupOptionsDefaults(t *testing.T) {
	opts := types.CleanupOptions{}
	if opts.All || opts.Containers || opts.Images || opts.Volumes || opts.BuildCache || opts.DryRun {
		t.Error("all fields should default to false")
	}
}

func TestCleanupReportZeroValues(t *testing.T) {
	r := types.CleanupReport{}
	if r.ContainersRemoved != 0 || r.ImagesRemoved != 0 || r.VolumesRemoved != 0 || r.TotalSpaceFreed != 0 {
		t.Error("all numeric fields should be zero")
	}
	if r.DryRun {
		t.Error("DryRun should be false")
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/types/ -v -count=1 -run TestCleanup`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add CleanupOptions and CleanupReport types"
```

### Task N+1: Add Cleanup to Manager Interface + Stub

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface) and `runtime.go:113-119` (stub)

**Interfaces:**
- Consumes: `types.CleanupOptions`, `types.CleanupReport`
- Produces: `Manager.Cleanup(ctx, types.CleanupOptions) (*types.CleanupReport, error)` on both `Manager` interface and `stubManager`

- [ ] **Step 1: Add Cleanup to Manager interface**

Edit `internal/runtime/runtime.go`, add after `KeepLastNImages` line:

```go
Cleanup(ctx context.Context, opts types.CleanupOptions) (*types.CleanupReport, error)
```

- [ ] **Step 2: Add stub implementation**

Add to `stubManager` in `runtime.go`:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts types.CleanupOptions) (*types.CleanupReport, error) {
	return &types.CleanupReport{}, nil
}
```

- [ ] **Step 3: Run tests to verify compilation**

Run: `go build ./...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/runtime.go
git commit -m "feat: add Cleanup method to Manager interface and stub"
```

### Task N+2: Implement Cleanup on Docker Runtime

**Files:**
- Modify: `internal/runtime/cleanup.go`

**Interfaces:**
- Consumes: `types.CleanupOptions`
- Produces: Docker CLI calls for `container prune`, `image prune`, `volume prune`, `builder prune`
- Returns: `*types.CleanupReport`

- [ ] **Step 1: Add Cleanup implementation to cleanup.go**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts types.CleanupOptions) (*types.CleanupReport, error) {
	report := &types.CleanupReport{DryRun: opts.DryRun}

	if opts.All {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.BuildCache = true
	}

	if opts.Containers {
		n, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return report, fmt.Errorf("prune containers: %w", err)
		}
		report.ContainersRemoved = n
	}

	if opts.Images {
		n, freed, err := r.pruneImages(ctx, opts.DryRun)
		if err != nil {
			return report, fmt.Errorf("prune images: %w", err)
		}
		report.ImagesRemoved = n
		report.TotalSpaceFreed += freed
	}

	if opts.Volumes {
		n, freed, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return report, fmt.Errorf("prune volumes: %w", err)
		}
		report.VolumesRemoved = n
		report.TotalSpaceFreed += freed
	}

	if opts.BuildCache {
		freed, err := r.pruneBuildCache(ctx, opts.DryRun)
		if err != nil {
			return report, fmt.Errorf("prune build cache: %w", err)
		}
		report.BuildCacheFreed = freed
		report.TotalSpaceFreed += freed
	}

	return report, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) (int, error) {
	args := []string{"container", "prune", "-f"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	// Protect Tengiz containers: only remove stopped containers that are NOT Tengiz-managed
	args = append(args, "--filter", "label!=tengiz-app", "--filter", "label!=tengiz-env")
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return countPrunedLines(string(out)), nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, dryRun bool) (int, int64, error) {
	args := []string{"image", "prune", "-f", "--all"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	n, freed := parsePruneOutput(string(out))
	return n, freed, nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) (int, int64, error) {
	args := []string{"volume", "prune", "-f"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	n, freed := parsePruneOutput(string(out))
	return n, freed, nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) (int64, error) {
	args := []string{"builder", "prune", "-f", "--all"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	// Builder output is like: "Total: 123.4MB" — extract just the freed space
	freed := parseBuildCacheOutput(string(out))
	return freed, nil
}

// countPrunedLines counts non-empty, non-header lines in prune output
func countPrunedLines(output string) int {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "Total reclaimed space: 0B" || strings.HasPrefix(line, "Deleted") || strings.HasPrefix(line, "Total reclaimed space:") {
			if strings.HasPrefix(line, "Total reclaimed space:") {
				continue
			}
			continue
		}
		count++
	}
	return count
}

// parsePruneOutput extracts count and space from docker prune output
// Output format: "Deleted Images:\ndeleted: sha256:...\ndeleted: sha256:...\n\nTotal reclaimed space: 123.4MB"
func parsePruneOutput(output string) (int, int64) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	count := 0
	var space int64
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "deleted:") || strings.HasPrefix(line, "untagged:") {
			count++
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			space = parseSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return count, space
}

// parseBuildCacheOutput extracts freed bytes from builder output
// Output format: "123.4MB\n" or "Total: 123.4MB"
func parseBuildCacheOutput(output string) int64 {
	output = strings.TrimSpace(output)
	// Remove "Total: " prefix if present
	output = strings.TrimPrefix(output, "Total: ")
	return parseSpace(output)
}

// parseSpace converts Docker size string like "123.4MB", "5GB", "100KB" to bytes
func parseSpace(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0B" {
		return 0
	}

	var value float64
	var unit string
	n, _ := fmt.Sscanf(s, "%f%s", &value, &unit)
	if n < 2 {
		return 0
	}

	multipliers := map[string]int64{
		"B":  1,
		"kB": 1000,
		"MB": 1000 * 1000,
		"GB": 1000 * 1000 * 1000,
		"TB": 1000 * 1000 * 1000 * 1000,
		"KiB": 1024,
		"MiB": 1024 * 1024,
		"GiB": 1024 * 1024 * 1024,
		"TiB": 1024 * 1024 * 1024 * 1024,
		"KB": 1000,
		"K":  1000,
		"M":  1000 * 1000,
		"G":  1000 * 1000 * 1000,
		"T":  1000 * 1000 * 1000 * 1000,
	}

	if mult, ok := multipliers[unit]; ok {
		return int64(value * float64(mult))
	}
	return 0
}
```

- [ ] **Step 2: Run tests to verify compilation**

Run: `go build ./...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/cleanup.go
git commit -m "feat: implement Cleanup method with docker prune operations"
```

### Task N+3: Write Tests for Cleanup Implementation

**Files:**
- Create: `internal/runtime/cleanup_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package runtime

import (
	"testing"
)

func TestParseSpace(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"0B", 0},
		{"", 0},
		{"100B", 100},
		{"1KB", 1000},
		{"1MB", 1000 * 1000},
		{"1GB", 1000 * 1000 * 1000},
		{"1.5MB", 1500000},
		{"2KiB", 2048},
		{"3MiB", 3 * 1024 * 1024},
	}
	for _, tt := range tests {
		got := parseSpace(tt.input)
		if got != tt.want {
			t.Errorf("parseSpace(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParsePruneOutput(t *testing.T) {
	output := "Deleted Images:\nuntagged: foo:latest\ndeleted: sha256:abc\ndeleted: sha256:def\n\nTotal reclaimed space: 1.5GB"
	n, freed := parsePruneOutput(output)
	if n != 3 {
		t.Errorf("count = %d, want 3", n)
	}
	if freed != 1500000000 {
		t.Errorf("space = %d, want 1500000000", freed)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	output := "Total reclaimed space: 0B"
	n, freed := parsePruneOutput(output)
	if n != 0 {
		t.Errorf("count = %d, want 0", n)
	}
	if freed != 0 {
		t.Errorf("space = %d, want 0", freed)
	}
}

func TestParseBuildCacheOutput(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"123.4MB", 123400000},
		{"Total: 5GB", 5000000000},
		{"0B", 0},
	}
	for _, tt := range tests {
		got := parseBuildCacheOutput(tt.input)
		if got != tt.want {
			t.Errorf("parseBuildCacheOutput(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestCountPrunedLines(t *testing.T) {
	output := "Deleted Containers:\ntengiz-helper\nbuild-cache-abc\n\nTotal reclaimed space: 100MB"
	count := countPrunedLines(output)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestCountPrunedLinesEmpty(t *testing.T) {
	output := "Total reclaimed space: 0B"
	count := countPrunedLines(output)
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}
```

- [ ] **Step 2: Run test to verify it fails (compile check)**

Run: `go test ./internal/runtime/ -v -count=1 -run TestParse`
Expected: Tests pass (we're testing pure functions with no Docker dependency)

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/cleanup_test.go
git commit -m "test: add unit tests for cleanup parse helpers"
```

### Task N+4: Create Cleanup CLI Command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:34-89`

**Interfaces:**
- Consumes: `runtime.Manager.Cleanup()`, `types.CleanupOptions`
- Produces: `tengiz cleanup` CLI command

- [ ] **Step 1: Create cleanup command in cleanup.go**

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, build cache)",
	Long: `Removes Docker resources that are not managed by Tengiz.
	
By default prunes all categories: stopped non-Tengiz containers, dangling images, 
unused volumes, and build cache. Uses label-based filtering to protect 
Tengiz-managed containers.

Flags allow targeting specific resource types and dry-run mode.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		cache, _ := cmd.Flags().GetBool("cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		// If no specific category flags set, default to all
		if !containers && !images && !volumes && !cache {
			all = true
		}

		opts := types.CleanupOptions{
			All:        all,
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			BuildCache: cache,
			DryRun:     dryRun,
		}

		mgr, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("failed to initialize Docker runtime: %w", err)
		}

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		report, err := mgr.Cleanup(ctx, opts)
		if err != nil {
			return fmt.Errorf("cleanup failed: %w", err)
		}

		printCleanupReport(report)
		return nil
	},
}

func printCleanupReport(r *types.CleanupReport) {
	if r.DryRun {
		fmt.Println("Dry-run mode — no resources were removed.")
	}

	lines := []string{}
	if r.ContainersRemoved > 0 || r.DryRun {
		lines = append(lines, fmt.Sprintf("  Containers: %d removed", r.ContainersRemoved))
	}
	if r.ImagesRemoved > 0 || r.DryRun {
		lines = append(lines, fmt.Sprintf("  Images:     %d removed", r.ImagesRemoved))
	}
	if r.VolumesRemoved > 0 || r.DryRun {
		lines = append(lines, fmt.Sprintf("  Volumes:    %d removed", r.VolumesRemoved))
	}
	if r.BuildCacheFreed > 0 || r.DryRun {
		lines = append(lines, fmt.Sprintf("  Build Cache: %s freed", formatBytes(r.BuildCacheFreed)))
	}
	if r.TotalSpaceFreed > 0 || r.DryRun {
		lines = append(lines, fmt.Sprintf("  Total:      %s reclaimed", formatBytes(r.TotalSpaceFreed)))
	}

	if len(lines) == 0 {
		fmt.Println("Nothing to clean up.")
		return
	}

	fmt.Println("Cleanup summary:")
	for _, l := range lines {
		fmt.Println(l)
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1000*1000*1000:
		return fmt.Sprintf("%.2f GB", float64(b)/(1000*1000*1000))
	case b >= 1000*1000:
		return fmt.Sprintf("%.2f MB", float64(b)/(1000*1000))
	case b >= 1000:
		return fmt.Sprintf("%.2f KB", float64(b)/1000)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func init() {
	cleanupCmd.Flags().Bool("all", false, "prune all resource types (default)")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("cache", false, "prune build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without doing it")
}
```

- [ ] **Step 2: Register cleanupCmd in root.go**

Add to `init()` in `internal/cli/root.go`, after the volume commands (after line 64):

```go
rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 3: Build to verify compilation**

Run: `go build ./...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup CLI command"
```

### Task N+5: Write Integration-Style Tests for Cleanup CLI

**Files:**
- Modify: `internal/cli/cleanup.go` (already created)

- [ ] **Step 1: Write tests for helper functions**

Add to `internal/cli/cleanup.go` — make `printCleanupReport` and `formatBytes` testable by keeping them package-exported (already visible within package). Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1500, "1.50 KB"},
		{2500000, "2.50 MB"},
		{3500000000, "3.50 GB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPrintCleanupReportEmpty(t *testing.T) {
	// Just verify it doesn't panic
	r := &types.CleanupReport{}
	printCleanupReport(r)
}

func TestPrintCleanupReportDryRun(t *testing.T) {
	r := &types.CleanupReport{
		ContainersRemoved: 0,
		ImagesRemoved:     0,
		VolumesRemoved:    0,
		BuildCacheFreed:   0,
		TotalSpaceFreed:   0,
		DryRun:            true,
	}
	printCleanupReport(r)
}

func TestPrintCleanupReportFull(t *testing.T) {
	r := &types.CleanupReport{
		ContainersRemoved: 3,
		ImagesRemoved:     12,
		VolumesRemoved:    1,
		BuildCacheFreed:   500000000,
		TotalSpaceFreed:   1500000000,
	}
	printCleanupReport(r)
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cli/ -v -count=1 -run TestFormatBytes`
Expected: PASS

Run: `go test ./internal/cli/ -v -count=1 -run TestPrintCleanup`
Expected: PASS

- [ ] **Step 3: Run all tests**

Run: `go test ./... -v -count=1`
Expected: PASS (all packages)

- [ ] **Step 4: Commit**

```bash
git add internal/cli/cleanup_test.go
git commit -m "test: add tests for cleanup CLI helpers"
```

---
