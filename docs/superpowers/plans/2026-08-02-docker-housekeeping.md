# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning unused Docker containers, images, networks, volumes, and build cache while protecting Tengiz-managed resources via label-based filtering.

**Architecture:** Extend the `runtime.Manager` interface with `Cleanup(ctx, CleanupOptions) (*CleanupResult, error)` and `SystemDF(ctx) (string, error)`. The exec-based `dockerRuntime` implementation (same `os/exec` pattern as `internal/runtime/docker.go`) runs one `docker <category> prune` command per enabled category. Containers are always pruned with `--filter label!=tengiz-app` so Tengiz-managed containers are never touched. Dry-run mode lists removal candidates instead of pruning. A new `tengiz cleanup` Cobra command maps flags to `CleanupOptions` and prints a summary.

**Tech Stack:** Go 1.26, Cobra, Docker CLI via `os/exec` (no new Go dependencies). Mirrors the existing arg-builder + parser testing pattern in `internal/runtime`.

## Global Constraints

- Tengiz containers always carry `--label tengiz-app=<appname>` (see `internal/runtime/docker.go:98`); versioned containers also carry `tengiz-deployment=<suffix>`. Cleanup must never remove them — the container prune always uses `--filter label!=tengiz-app`.
- Tengiz images use the `tengiz-apps/<app>:<tag>` reference. Image pruning removes ONLY dangling images (`docker image prune -f`); old Tengiz images are governed by `KeepLastNImages` (5 retained per app) at deploy time, never by `cleanup`.
- Tengiz persistent volumes are bind mounts (`host_path:container_path`), never named volumes, so `docker volume prune` cannot touch app data.
- No category flags → prune ALL categories (containers, images, networks, volumes, build cache). Individual flags select a subset.
- `--status` shows `docker system df` output instead of pruning.
- No new external Go dependencies.
- Follow existing patterns: pure arg-builder/parser functions are unit-tested; docker CLI invocations are not unit-tested.
- Every task ends with the specified `go test` commands passing and a commit.
- Existing tests must continue to pass without modification (except the mechanical mock additions required by the interface change).

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`/`CleanupResult` types; extend `Manager` interface with `Cleanup` + `SystemDF`; add no-op implementations to `stubManager` |
| `internal/runtime/cleanup.go` | `dockerRuntime.Cleanup` + `SystemDF` implementations, per-category prune/list arg builders, prune-output parsers |
| `internal/runtime/cleanup_test.go` | Unit tests for arg builders, parsers, stub, and `TestCleanupNoCategories` |
| `internal/cli/cleanup.go` | `cleanupCmd` (Cobra), `cleanupOptions()` helper, `init()` registration |
| `internal/cli/cleanup_test.go` | CLI registration, flag, and option-mapping tests |
| `internal/cli/root_test.go` | Add `Cleanup`/`SystemDF` to `mockRTForDeploy` (keeps it implementing `Manager`) |
| `internal/idle/idle_test.go` | Add `Cleanup`/`SystemDF` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Cleanup`/`SystemDF` to `mockRuntime` |
| `README.md` | Document the `tengiz cleanup` command after the `rollback` section |

No new package directories. The four mock/stub Manager implementations must all learn the two new interface methods in the same task as the interface change, or the cli/idle/proxy test packages stop compiling.

---

### Task 1: Cleanup types, Manager interface extension, stub + all mock updates

**Files:**
- Modify: `internal/runtime/runtime.go:18-123` — add types before the `Manager` interface, add interface methods, add stub methods
- Test: `internal/runtime/cleanup_test.go` — add stub tests
- Modify: `internal/cli/root_test.go:100` — add methods to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:34` — add methods to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:35` — add methods to `mockRuntime`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions` struct, `runtime.CleanupResult` struct, `Manager.Cleanup(ctx, CleanupOptions) (*CleanupResult, error)`, `Manager.SystemDF(ctx) (string, error)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go` (create the file):

```go
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil {
		t.Fatal("Cleanup() returned nil result")
	}
	if len(res.Containers) != 0 || len(res.Images) != 0 || len(res.Networks) != 0 || len(res.Volumes) != 0 || len(res.BuildCache) != 0 {
		t.Errorf("expected empty result, got %+v", res)
	}
}

func TestStubSystemDF(t *testing.T) {
	m := NewStub()
	out, err := m.SystemDF(context.Background())
	if err != nil {
		t.Fatalf("SystemDF() error = %v", err)
	}
	if out != "" {
		t.Errorf("SystemDF() = %q, want empty", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestStubSystemDF" -count=1`

Expected: FAIL — package does not compile with `undefined: CleanupOptions`, `undefined: Cleanup`, `undefined: SystemDF`

- [ ] **Step 3: Add types and interface methods in `internal/runtime/runtime.go`**

Insert the two structs before the `Manager` interface (before line 18, after the `RunOptions` struct):

```go
type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
}

type CleanupResult struct {
	Containers []string
	Images     []string
	Networks   []string
	Volumes    []string
	BuildCache []string
	Reclaimed  string
}
```

Add these two lines to the `Manager` interface (after the `KeepLastNImages` line at line 36):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
	SystemDF(ctx context.Context) (string, error)
```

- [ ] **Step 4: Add no-op methods to `stubManager`**

Add after the existing `KeepLastNImages` stub (after line 119, before `Run`):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{}, nil
}

func (m *stubManager) SystemDF(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 5: Update the three test mocks so their packages still compile**

In `internal/cli/root_test.go`, add after the `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{}, nil
}
func (m *mockRTForDeploy) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

In `internal/idle/idle_test.go`, add after the `KeepLastNImages` method (line 33):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) { return &runtime.CleanupResult{}, nil }
func (m *mockRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

In `internal/proxy/proxy_test.go`, add after the `KeepLastNImages` method (line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) { return &runtime.CleanupResult{}, nil }
func (m *mockRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/idle/... ./internal/proxy/... -count=1`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Cleanup and SystemDF methods to runtime.Manager interface"
```

---

### Task 2: Prune/list arg builders and output parsers

**Files:**
- Create: `internal/runtime/cleanup.go` — helper functions only (dockerRuntime methods come in Task 3)
- Test: `internal/runtime/cleanup_test.go` — add builder/parser tests

**Interfaces:**
- Consumes: nothing (pure functions)
- Produces: `pruneArgs(category string) []string`, `listArgs(category string) []string`, `parseListOutput(output string) []string`, `filterNetworks(names []string) []string`, `parseReclaimedSpace(line string) (int64, error)`, `extractReclaimedSpace(output string) string`, `humanBytes(b int64) string`, `sumReclaimed(lines []string) string`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestPruneArgs(t *testing.T) {
	tests := []struct {
		category string
		expected []string
	}{
		{"container", []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"image", []string{"image", "prune", "-f"}},
		{"network", []string{"network", "prune", "-f"}},
		{"volume", []string{"volume", "prune", "-f"}},
		{"builder", []string{"builder", "prune", "-f"}},
		{"unknown", nil},
	}
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			got := pruneArgs(tt.category)
			if len(got) != len(tt.expected) {
				t.Fatalf("pruneArgs(%q) = %v (len %d), want %v (len %d)", tt.category, got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("pruneArgs(%q)[%d] = %q, want %q", tt.category, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestListArgs(t *testing.T) {
	tests := []struct {
		category string
		expected []string
	}{
		{"container", []string{"container", "ls", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.Names}}"}},
		{"image", []string{"images", "-a", "--filter", "dangling=true", "--format", "{{.Repository}}:{{.Tag}}"}},
		{"network", []string{"network", "ls", "--format", "{{.Name}}"}},
		{"volume", []string{"volume", "ls", "--format", "{{.Name}}"}},
		{"builder", []string{"builder", "du", "--format", "{{.ID}}"}},
		{"unknown", nil},
	}
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			got := listArgs(tt.category)
			if len(got) != len(tt.expected) {
				t.Fatalf("listArgs(%q) = %v (len %d), want %v (len %d)", tt.category, got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("listArgs(%q)[%d] = %q, want %q", tt.category, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseListOutput(t *testing.T) {
	got := parseListOutput("foo\nbar\n\nbaz\n")
	expected := []string{"foo", "bar", "baz"}
	if len(got) != len(expected) {
		t.Fatalf("parseListOutput() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("parseListOutput()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestFilterNetworks(t *testing.T) {
	got := filterNetworks([]string{"bridge", "my-net", "host", "none", "tengiz-net"})
	expected := []string{"my-net", "tengiz-net"}
	if len(got) != len(expected) {
		t.Fatalf("filterNetworks() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("filterNetworks()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		line     string
		expected int64
	}{
		{"Total reclaimed space: 0B", 0},
		{"Total reclaimed space: 532.1kB", 532100},
		{"Total reclaimed space: 5.203MB", 5203000},
		{"Total reclaimed space: 1.2GB", 1200000000},
	}
	for _, tt := range tests {
		got, err := parseReclaimedSpace(tt.line)
		if err != nil {
			t.Fatalf("parseReclaimedSpace(%q) error = %v", tt.line, err)
		}
		if got != tt.expected {
			t.Fatalf("parseReclaimedSpace(%q) = %d, want %d", tt.line, got, tt.expected)
		}
	}
}

func TestParseReclaimedSpaceInvalid(t *testing.T) {
	if _, err := parseReclaimedSpace("no colon here"); err == nil {
		t.Error("expected error for line without reclaimed space marker")
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		b        int64
		expected string
	}{
		{0, "0B"},
		{500, "500B"},
		{1500, "1.5kB"},
		{5203000, "5.2MB"},
		{1200000000, "1.2GB"},
	}
	for _, tt := range tests {
		got := humanBytes(tt.b)
		if got != tt.expected {
			t.Fatalf("humanBytes(%d) = %q, want %q", tt.b, got, tt.expected)
		}
	}
}

func TestSumReclaimed(t *testing.T) {
	got := sumReclaimed([]string{
		"Total reclaimed space: 5MB",
		"Total reclaimed space: 1.2GB",
	})
	if got != "1.2GB" {
		t.Fatalf("sumReclaimed() = %q, want %q", got, "1.2GB")
	}
}

func TestExtractReclaimedSpace(t *testing.T) {
	output := "Deleted Containers:\n9e4a2f...\n\nTotal reclaimed space: 5.203MB\n"
	got := extractReclaimedSpace(output)
	if got != "Total reclaimed space: 5.203MB" {
		t.Fatalf("extractReclaimedSpace() = %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneArgs|TestListArgs|TestParseListOutput|TestFilterNetworks|TestParseReclaimedSpace|TestHumanBytes|TestSumReclaimed|TestExtractReclaimedSpace" -count=1`

Expected: FAIL — `undefined: pruneArgs`, `undefined: listArgs`, etc.

- [ ] **Step 3: Implement the helper functions in `internal/runtime/cleanup.go`**

Create the file with only these functions (the dockerRuntime methods arrive in Task 3):

```go
package runtime

import (
	"fmt"
	"strconv"
	"strings"
)

func pruneArgs(category string) []string {
	switch category {
	case "container":
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "image":
		return []string{"image", "prune", "-f"}
	case "network":
		return []string{"network", "prune", "-f"}
	case "volume":
		return []string{"volume", "prune", "-f"}
	case "builder":
		return []string{"builder", "prune", "-f"}
	}
	return nil
}

func listArgs(category string) []string {
	switch category {
	case "container":
		return []string{"container", "ls", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.Names}}"}
	case "image":
		return []string{"images", "-a", "--filter", "dangling=true", "--format", "{{.Repository}}:{{.Tag}}"}
	case "network":
		return []string{"network", "ls", "--format", "{{.Name}}"}
	case "volume":
		return []string{"volume", "ls", "--format", "{{.Name}}"}
	case "builder":
		return []string{"builder", "du", "--format", "{{.ID}}"}
	}
	return nil
}

func parseListOutput(output string) []string {
	var names []string
	for _, line := range strings.Split(output, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		names = append(names, t)
	}
	return names
}

func filterNetworks(names []string) []string {
	var filtered []string
	for _, n := range names {
		switch n {
		case "bridge", "host", "none":
			continue
		}
		filtered = append(filtered, n)
	}
	return filtered
}

func parseReclaimedSpace(line string) (int64, error) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return 0, fmt.Errorf("no ':' in %q", line)
	}
	rest := strings.TrimSpace(line[idx+1:])
	for _, unit := range []struct {
		suffix string
		mult   int64
	}{
		{"TB", 1000000000000},
		{"GB", 1000000000},
		{"MB", 1000000},
		{"kB", 1000},
		{"B", 1},
	} {
		if strings.HasSuffix(rest, unit.suffix) {
			numStr := strings.TrimSpace(strings.TrimSuffix(rest, unit.suffix))
			f, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("parse number %q: %w", numStr, err)
			}
			return int64(f * float64(unit.mult)), nil
		}
	}
	return 0, fmt.Errorf("unknown unit in %q", rest)
}

func extractReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		t := strings.TrimSpace(line)
		if strings.Contains(t, "Total reclaimed space:") {
			return t
		}
	}
	return ""
}

func humanBytes(b int64) string {
	if b < 1000 {
		return fmt.Sprintf("%dB", b)
	}
	units := []string{"B", "kB", "MB", "GB", "TB"}
	f := float64(b)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	return fmt.Sprintf("%.1f%s", f, units[i])
}

func sumReclaimed(lines []string) string {
	var total int64
	for _, l := range lines {
		if l == "" {
			continue
		}
		b, err := parseReclaimedSpace(l)
		if err == nil {
			total += b
		}
	}
	if total == 0 && len(lines) == 0 {
		return ""
	}
	return humanBytes(total)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneArgs|TestListArgs|TestParseListOutput|TestFilterNetworks|TestParseReclaimedSpace|TestHumanBytes|TestSumReclaimed|TestExtractReclaimedSpace" -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add cleanup arg builders and prune output parsers"
```

---

### Task 3: dockerRuntime.Cleanup and SystemDF implementations

**Files:**
- Modify: `internal/runtime/cleanup.go` — add the dockerRuntime methods
- Test: `internal/runtime/cleanup_test.go` — add `TestCleanupNoCategories`

**Interfaces:**
- Consumes: `pruneArgs`, `listArgs`, `parseListOutput`, `filterNetworks`, `extractReclaimedSpace`, `sumReclaimed` from Task 2; `CleanupOptions`/`CleanupResult` from Task 1
- Produces: `(*dockerRuntime).Cleanup(ctx, CleanupOptions) (*CleanupResult, error)`, `(*dockerRuntime).SystemDF(ctx) (string, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestCleanupNoCategories(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil {
		t.Fatal("Cleanup() returned nil result")
	}
	if res.Reclaimed != "" {
		t.Errorf("Reclaimed = %q, want empty", res.Reclaimed)
	}
}
```

This test constructs `&dockerRuntime{}` directly (not via `NewDocker()`, which requires the docker binary) and enables no categories, so `Cleanup` returns without invoking any `docker` command.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestCleanupNoCategories" -count=1`

Expected: FAIL — `undefined: (*dockerRuntime).Cleanup`

- [ ] **Step 3: Implement the methods in `internal/runtime/cleanup.go`**

Append to the file. Add `"os/exec"` to the import block:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)
```

Add the methods:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	result := &CleanupResult{}
	var reclaimed []string

	categories := []struct {
		name    string
		enabled bool
		target  *[]string
	}{
		{"container", opts.Containers, &result.Containers},
		{"image", opts.Images, &result.Images},
		{"network", opts.Networks, &result.Networks},
		{"volume", opts.Volumes, &result.Volumes},
		{"builder", opts.BuildCache, &result.BuildCache},
	}

	for _, cat := range categories {
		if !cat.enabled {
			continue
		}
		if opts.DryRun {
			cmd := exec.CommandContext(ctx, "docker", listArgs(cat.name)...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return nil, fmt.Errorf("list %s: %w\n%s", cat.name, err, string(out))
			}
			candidates := parseListOutput(string(out))
			if cat.name == "network" {
				candidates = filterNetworks(candidates)
			}
			*cat.target = candidates
			continue
		}
		cmd := exec.CommandContext(ctx, "docker", pruneArgs(cat.name)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("prune %s: %w\n%s", cat.name, err, string(out))
		}
		if line := extractReclaimedSpace(string(out)); line != "" {
			reclaimed = append(reclaimed, line)
		}
	}

	result.Reclaimed = sumReclaimed(reclaimed)
	return result, nil
}

func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -count=1`

Expected: PASS (including the new `TestCleanupNoCategories` and all Task 1/2 tests)

- [ ] **Step 5: Run build to verify compilation**

Run: `go build ./...`

Expected: succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement dockerRuntime.Cleanup and SystemDF"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go` — command + helper + registration
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `rt.Cleanup(ctx, opts)`, `rt.SystemDF(ctx)` from Tasks 1-3
- Produces: `cleanupCmd *cobra.Command`, `cleanupOptions(dryRun, containers, images, networks, volumes, buildCache bool) runtime.CleanupOptions`

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
		t.Fatalf("cleanup command not found")
	}
}

func TestCleanupFlagsExist(t *testing.T) {
	for _, f := range []string{"dry-run", "containers", "images", "networks", "volumes", "build-cache", "status"} {
		if cleanupCmd.Flags().Lookup(f) == nil {
			t.Errorf("cleanupCmd missing --%s flag", f)
		}
	}
}

func TestCleanupDefaultsToAllCategories(t *testing.T) {
	opts := cleanupOptions(false, false, false, false, false, false)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.Volumes || !opts.BuildCache {
		t.Errorf("expected all categories enabled, got %+v", opts)
	}
}

func TestCleanupRespectsSelectedCategories(t *testing.T) {
	opts := cleanupOptions(false, true, false, false, false, false)
	if !opts.Containers {
		t.Error("containers should be enabled")
	}
	if opts.Images || opts.Networks || opts.Volumes || opts.BuildCache {
		t.Errorf("only containers should be enabled, got %+v", opts)
	}
}

func TestCleanupDryRunPassthrough(t *testing.T) {
	opts := cleanupOptions(true, false, false, false, false, false)
	if !opts.DryRun {
		t.Error("dry-run should be enabled")
	}
	if !opts.Containers {
		t.Error("default categories should still be enabled in dry-run")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -count=1`

Expected: FAIL — `undefined: cleanupCmd`, `undefined: cleanupOptions`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Removes unused Docker resources (containers, images, networks, volumes, build cache)
to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app=...) are always protected and are never pruned.
With no category flags, all categories are pruned. Use --dry-run to preview what would be
removed, and --status to show current disk usage instead of pruning.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		networks, _ := cmd.Flags().GetBool("networks")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		status, _ := cmd.Flags().GetBool("status")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if status {
			out, err := rt.SystemDF(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		}

		opts := cleanupOptions(dryRun, containers, images, networks, volumes, buildCache)

		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}

		if opts.DryRun {
			fmt.Println("[tengiz] dry run: nothing was removed")
		}

		fmt.Printf("[tengiz] containers: %d\n", len(result.Containers))
		fmt.Printf("[tengiz] images: %d\n", len(result.Images))
		fmt.Printf("[tengiz] networks: %d\n", len(result.Networks))
		fmt.Printf("[tengiz] volumes: %d\n", len(result.Volumes))
		fmt.Printf("[tengiz] build cache: %d\n", len(result.BuildCache))
		if !opts.DryRun && result.Reclaimed != "" {
			fmt.Printf("[tengiz] reclaimed: %s\n", result.Reclaimed)
		}
		return nil
	},
}

func cleanupOptions(dryRun, containers, images, networks, volumes, buildCache bool) runtime.CleanupOptions {
	if !containers && !images && !networks && !volumes && !buildCache {
		containers, images, networks, volumes, buildCache = true, true, true, true, true
	}
	return runtime.CleanupOptions{
		DryRun:     dryRun,
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		BuildCache: buildCache,
	}
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "prune unused stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused named volumes")
	cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
	cleanupCmd.Flags().Bool("status", false, "show docker system df instead of pruning")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -count=1`

Expected: PASS

- [ ] **Step 5: Run build and the full cli test package**

Run: `go build ./... && go test ./internal/cli/... -count=1`

Expected: build succeeds, all cli tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 5: Update README, full verification, self-review

**Files:**
- Modify: `README.md` — document `tengiz cleanup` after the rollback section (after line 236, before `### tengiz domain` at line 238)
- Test: none new

- [ ] **Step 1: Document the command in `README.md`**

Insert after the `### tengiz rollback <app>` section:

```markdown
### `tengiz cleanup`

Remove unused Docker resources (containers, images, networks, volumes, build cache) to reclaim disk space.

Tengiz-managed containers (labeled `tengiz-app=<appname>`) are always protected and are never pruned. With no category flags, all categories are pruned.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--containers` | Prune unused stopped containers |
| `--images` | Prune dangling images |
| `--networks` | Prune unused networks |
| `--volumes` | Prune unused named volumes |
| `--build-cache` | Prune build cache |
| `--status` | Show `docker system df` disk usage instead of pruning |

Examples:
```
tengiz cleanup
tengiz cleanup --dry-run
tengiz cleanup --containers --images
tengiz cleanup --status
```
```

- [ ] **Step 2: Run the full test suite**

Run: `go test ./... -count=1`

Expected: All packages PASS (proxy tests are slow ~2s each — allow time; idle tests are time-sensitive with 50ms granularity)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 3: Self-review against the spec**

Check each requirement from `docs/FUTURES_FEATURES.md`:

- "Label-based `docker system prune`" — ✅ container prune uses `--filter label!=tengiz-app` (Task 3); the container categories never touch labeled Tengiz containers
- "`tengiz cleanup` komutu" — ✅ command added (Task 4)
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" — ✅ per-category prune for containers/images/networks/volumes (+ build cache), addressing the related Granular Docker Prune Operations (#56) via category flags
- "Tengiz yönetimindeki container'lar korunur" — ✅ label filter guarantees this

- [ ] **Step 4: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "fill in details", "add appropriate", "Similar to Task". None present — every step has complete code, exact commands, and expected output.

- [ ] **Step 5: Type consistency check**

- `runtime.CleanupOptions{DryRun, Containers, Images, Networks, Volumes, BuildCache bool}` — defined Task 1, consumed Task 3 (`Cleanup`) and Task 4 (`cleanupOptions`)
- `runtime.CleanupResult{Containers, Images, Networks, Volumes, BuildCache []string, Reclaimed string}` — defined Task 1, produced Task 3, read Task 4
- `Manager.Cleanup(ctx, CleanupOptions) (*CleanupResult, error)` — same signature on `dockerRuntime` (Task 3), `stubManager` (Task 1), and all three test mocks (Task 1)
- `Manager.SystemDF(ctx) (string, error)` — same signature everywhere
- Helper names: `pruneArgs`, `listArgs`, `parseListOutput`, `filterNetworks`, `parseReclaimedSpace`, `extractReclaimedSpace`, `humanBytes`, `sumReclaimed` — consistent between Task 2 (definitions + tests) and Task 3 (usage)
- CLI: `cleanupOptions(dryRun, containers, images, networks, volumes, buildCache bool) runtime.CleanupOptions` — defined and tested identically in Task 4

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```
