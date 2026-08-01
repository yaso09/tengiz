# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by removing stopped foreign containers, dangling images, build cache, and unused volumes — while never touching containers managed by Tengiz (label-based protection).

**Architecture:** Extend the `runtime.Manager` interface with a `Cleanup(ctx, CleanupOptions) (CleanupReport, error)` method. The `dockerRuntime` implementation shells out to granular Docker commands (`docker ps -a`, `docker images`, `docker builder prune`, `docker volume prune`) with pure, unit-testable arg-builder and output-parsing helpers. A new `tengiz cleanup` Cobra command in `internal/cli/cleanup.go` selects categories via flags (`--containers`, `--images`, `--cache`, `--volumes`, `--all`, `--dry-run`). Category selection defaults to "everything" when no category flag is passed.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` interface + `stubManager`, `os/exec` Docker CLI calls. No new external dependencies.

## Global Constraints

- Containers labeled `tengiz-app=<name>` are ALWAYS preserved — cleanup only removes stopped containers WITHOUT that label
- Image tags: `tengiz-apps/<app>:<env>-<deploymentID>` — cleanup only touches dangling (untagged) images; old versioned images are handled by the existing `KeepLastNImages` (retain 5)
- Existing `dockerRuntime` methods are reused: `Remove(ctx, id)` for containers, `RemoveImage(ctx, id)` for images
- `stubManager` must gain a `Cleanup` method returning a zero `CleanupReport` and nil error
- All three test mocks implementing `runtime.Manager` (`internal/proxy/proxy_test.go`, `internal/idle/idle_test.go`, `internal/cli/root_test.go`) must gain a `Cleanup` method or the build breaks
- `tengiz cleanup` requires no app argument and no `.tengiz.yaml` (host-level operation)
- The `--env` flag is NOT needed for this command (cleans the whole Docker host)
- No new external dependencies; no Docker SDK
- Commands: `go build -o tengiz .`, `go test ./... -v -count=1`, `go vet ./...`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupReport` types; add `Cleanup` to `Manager` interface; add stub method |
| `internal/runtime/cleanup.go` | Pure arg-builder + parse helpers; `dockerRuntime.Cleanup` + per-category exec methods |
| `internal/runtime/cleanup_test.go` | Tests for stub `Cleanup`, `parseForeignContainers`, `hasLabel`, `parseImageIDs`, arg builders |
| `internal/proxy/proxy_test.go` | Add `Cleanup` method to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` method to `mockRuntime` |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy` |
| `internal/cli/cleanup.go` | New `tengiz cleanup` Cobra command + `formatCleanupReport` |
| `internal/cli/cleanup_test.go` | Tests for command registration, flags, report formatting |
| `README.md` | Document `tengiz cleanup` under CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

---

### Task 1: Add Cleanup types + Manager interface method + stub + update all mocks

**Files:**
- Modify: `internal/runtime/runtime.go` — add types near `RunOptions` (line 26-29), add method to `Manager` interface (line 31-49), add stub method near line 113
- Modify: `internal/runtime/cleanup_test.go` — add stub test
- Modify: `internal/proxy/proxy_test.go:35` — add mock method
- Modify: `internal/idle/idle_test.go:34` — add mock method
- Modify: `internal/cli/root_test.go:100` — add mock method

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{Containers, Images, BuildCache, Volumes, DryRun bool}`, `runtime.CleanupReport{ContainersRemoved, ImagesRemoved int, BuildCachePruned, VolumesPruned, DryRun bool}`, `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go` (keep the two existing tests):

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0", report.ContainersRemoved)
	}
	if report.ImagesRemoved != 0 {
		t.Errorf("ImagesRemoved = %d, want 0", report.ImagesRemoved)
	}
	if report.BuildCachePruned {
		t.Error("BuildCachePruned = true, want false")
	}
	if report.VolumesPruned {
		t.Error("VolumesPruned = true, want false")
	}
	if report.DryRun {
		t.Error("DryRun = true, want false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`

Expected: FAIL with `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add types to `internal/runtime/runtime.go`**

Insert after the `RunOptions` struct (line 29):

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	BuildCache bool
	Volumes    bool
	DryRun     bool
}

type CleanupReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	BuildCachePruned  bool
	VolumesPruned     bool
	DryRun            bool
}
```

Add to the `Manager` interface (after the `Run` method, line 48):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)
```

Add the stub method (after `stubManager.Run`, line 121):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
}
```

- [ ] **Step 4: Add `Cleanup` to the three test mocks**

In `internal/proxy/proxy_test.go` after line 35 (`Run`):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) { return runtime.CleanupReport{}, nil }
```

In `internal/idle/idle_test.go` after line 34 (`Run`):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) { return runtime.CleanupReport{}, nil }
```

In `internal/cli/root_test.go` after line 100 (`Run`):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) { return runtime.CleanupReport{}, nil }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/ ./internal/proxy/ ./internal/idle/ ./internal/cli/ -count=1`

Expected: PASS (all packages compile; mock interface satisfaction confirmed)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: add Cleanup method to runtime Manager interface"
```

---

### Task 2: Pure helper functions for container/image/volume/cache cleanup

**Files:**
- Modify: `internal/runtime/cleanup.go` — append helper functions
- Modify: `internal/runtime/cleanup_test.go` — add helper tests

**Interfaces:**
- Consumes: `labelKey` (`"tengiz-app"`) and `envLabelKey` (`"tengiz-env"`) from `internal/runtime/docker.go:76-77`
- Produces:
  - `stoppedContainerArgs() []string` — `docker ps -a` args returning `ID|Labels` rows for stopped/created containers
  - `parseForeignContainers(output string) []string` — IDs of rows WITHOUT a `tengiz-app=` label
  - `hasLabel(labels, key string) bool` — true if comma-separated label string contains `key=`
  - `danglingImageArgs() []string` — `docker images --filter dangling=true -q` args
  - `parseImageIDs(output string) []string` — non-empty trimmed lines
  - `buildCachePruneArgs() []string` — `docker builder prune -f` args
  - `volumePruneArgs() []string` — `docker volume prune -f` args

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`. Add `"strings"` to the import block first:

```go
import (
	"context"
	"strings"
	"testing"
)
```

```go
func TestHasLabel(t *testing.T) {
	tests := []struct {
		name   string
		labels string
		key    string
		want   bool
	}{
		{"tengiz app label present", "tengiz-app=myapp,tengiz-env=production", "tengiz-app", true},
		{"tengiz env only", "tengiz-env=production", "tengiz-app", false},
		{"unrelated labels", "maintainer=dev,role=web", "tengiz-app", false},
		{"empty", "", "tengiz-app", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasLabel(tt.labels, tt.key); got != tt.want {
				t.Errorf("hasLabel(%q, %q) = %v, want %v", tt.labels, tt.key, got, tt.want)
			}
		})
	}
}

func TestParseForeignContainers(t *testing.T) {
	output := "abc123|tengiz-app=myapp,tengiz-env=production\n" +
		"def456|maintainer=dev\n" +
		"ghi789|tengiz-app=other\n" +
		"jkl012|\n"
	ids := parseForeignContainers(output)
	want := []string{"def456", "jkl012"}
	if len(ids) != len(want) {
		t.Fatalf("parseForeignContainers() = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestParseForeignContainersEmpty(t *testing.T) {
	ids := parseForeignContainers("")
	if len(ids) != 0 {
		t.Fatalf("expected no IDs, got %v", ids)
	}
}

func TestParseImageIDs(t *testing.T) {
	output := "sha256:aaa\n\nsha256:bbb\nsha256:ccc\n"
	ids := parseImageIDs(output)
	want := []string{"sha256:aaa", "sha256:bbb", "sha256:ccc"}
	if len(ids) != len(want) {
		t.Fatalf("parseImageIDs() = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestStoppedContainerArgs(t *testing.T) {
	args := stoppedContainerArgs()
	joined := strings.Join(args, " ")
	for _, want := range []string{"ps", "-a", "status=exited", "status=created", "{{.ID}}|{{.Labels}}"} {
		if !strings.Contains(joined, want) {
			t.Errorf("stoppedContainerArgs() = %v, missing %q", args, want)
		}
	}
}

func TestDanglingImageArgs(t *testing.T) {
	args := danglingImageArgs()
	joined := strings.Join(args, " ")
	for _, want := range []string{"images", "dangling=true", "-q"} {
		if !strings.Contains(joined, want) {
			t.Errorf("danglingImageArgs() = %v, missing %q", args, want)
		}
	}
}

func TestPruneArgs(t *testing.T) {
	if got := strings.Join(buildCachePruneArgs(), " "); !strings.Contains(got, "builder") || !strings.Contains(got, "prune") {
		t.Errorf("buildCachePruneArgs() = %v", buildCachePruneArgs())
	}
	if got := strings.Join(volumePruneArgs(), " "); !strings.Contains(got, "volume") || !strings.Contains(got, "prune") {
		t.Errorf("volumePruneArgs() = %v", volumePruneArgs())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestHasLabel|TestParseForeignContainers|TestParseImageIDs|TestStoppedContainerArgs|TestDanglingImageArgs|TestPruneArgs" -v -count=1`

Expected: FAIL with `undefined: hasLabel`, `undefined: parseForeignContainers`, etc.

- [ ] **Step 3: Write the helper implementations**

Append to `internal/runtime/cleanup.go` (after `KeepLastNImages`):

```go
func stoppedContainerArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--format", "{{.ID}}|{{.Labels}}",
	}
}

func hasLabel(labels, key string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] == key {
			return true
		}
	}
	return false
}

func parseForeignContainers(output string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		if hasLabel(parts[1], labelKey) {
			continue
		}
		ids = append(ids, parts[0])
	}
	return ids
}

func danglingImageArgs() []string {
	return []string{"images", "--filter", "dangling=true", "-q"}
}

func parseImageIDs(output string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line != "" {
			ids = append(ids, strings.TrimSpace(line))
		}
	}
	return ids
}

func buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestHasLabel|TestParseForeignContainers|TestParseImageIDs|TestStoppedContainerArgs|TestDanglingImageArgs|TestPruneArgs" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add cleanup arg-builder and parse helpers"
```

---

### Task 3: Implement dockerRuntime.Cleanup (exec glue)

**Files:**
- Modify: `internal/runtime/cleanup.go` — append `Cleanup` + per-category exec methods

**Interfaces:**
- Consumes: `stoppedContainerArgs`, `parseForeignContainers`, `danglingImageArgs`, `parseImageIDs`, `buildCachePruneArgs`, `volumePruneArgs`, `r.Remove(ctx, id)`, `r.RemoveImage(ctx, id)` (all already exist)
- Produces: `(*dockerRuntime).Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)`

- [ ] **Step 1: Write the failing test**

There is no way to exec Docker in CI, so the exec glue is verified indirectly: append a compile-time interface assertion to `internal/runtime/cleanup_test.go` and run the whole package.

```go
func TestDockerRuntimeImplementsCleanup(t *testing.T) {
	var _ Manager = (*dockerRuntime)(nil)
}
```

Run: `go test ./internal/runtime/ -run TestDockerRuntimeImplementsCleanup -v -count=1`

Expected: FAIL with `undefined: Cleanup` (dockerRuntime missing the method) — confirming the method does not exist yet.

- [ ] **Step 2: Write the Cleanup implementation**

Append to `internal/runtime/cleanup.go` (after the helpers from Task 2):

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	report := CleanupReport{DryRun: opts.DryRun}

	if opts.Containers {
		n, err := r.cleanupContainers(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.ContainersRemoved = n
	}

	if opts.Images {
		n, err := r.cleanupDanglingImages(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.ImagesRemoved = n
	}

	if opts.BuildCache {
		pruned, err := r.cleanupBuildCache(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.BuildCachePruned = pruned
	}

	if opts.Volumes {
		pruned, err := r.cleanupVolumes(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.VolumesPruned = pruned
	}

	return report, nil
}

func (r *dockerRuntime) cleanupContainers(ctx context.Context, dryRun bool) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", stoppedContainerArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	ids := parseForeignContainers(string(out))
	if dryRun {
		return len(ids), nil
	}
	removed := 0
	for _, id := range ids {
		if err := r.Remove(ctx, id); err != nil {
			log.Printf("[runtime] cleanup: failed to remove container %s: %v", id, err)
			continue
		}
		removed++
	}
	return removed, nil
}

func (r *dockerRuntime) cleanupDanglingImages(ctx context.Context, dryRun bool) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", danglingImageArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	ids := parseImageIDs(string(out))
	if dryRun {
		return len(ids), nil
	}
	removed := 0
	for _, id := range ids {
		if err := r.RemoveImage(ctx, id); err != nil {
			log.Printf("[runtime] cleanup: failed to remove image %s: %v", id, err)
			continue
		}
		removed++
	}
	return removed, nil
}

func (r *dockerRuntime) cleanupBuildCache(ctx context.Context, dryRun bool) (bool, error) {
	if dryRun {
		return false, nil
	}
	cmd := exec.CommandContext(ctx, "docker", buildCachePruneArgs()...)
	if _, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("docker builder prune: %w", err)
	}
	return true, nil
}

func (r *dockerRuntime) cleanupVolumes(ctx context.Context, dryRun bool) (bool, error) {
	if dryRun {
		return false, nil
	}
	cmd := exec.CommandContext(ctx, "docker", volumePruneArgs()...)
	if _, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("docker volume prune: %w", err)
	}
	return true, nil
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -count=1`

Expected: PASS (interface assertion satisfied, stub + helper tests green)

- [ ] **Step 4: Verify no lint regressions**

Run: `go vet ./internal/runtime/`

Expected: no output

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go
git commit -m "feat: implement dockerRuntime.Cleanup housekeeping"
```

---

### Task 4: Add `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupReport`, `rootCmd` (from `internal/cli/root.go`)
- Produces: `cleanupCmd` cobra command registered on `rootCmd` via its own `init()`; `formatCleanupReport(report runtime.CleanupReport) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil {
		t.Fatal("cleanup command is nil")
	}
	expectedFlags := []string{"dry-run", "all", "containers", "images", "cache", "volumes"}
	for _, flag := range expectedFlags {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func TestFormatCleanupReport(t *testing.T) {
	report := runtime.CleanupReport{
		ContainersRemoved: 2,
		ImagesRemoved:     3,
		BuildCachePruned:  true,
		VolumesPruned:     true,
		DryRun:            false,
	}
	out := formatCleanupReport(report)
	for _, want := range []string{"cleanup complete", "containers removed:  2", "images removed:      3", "build cache pruned:  true", "volumes pruned:      true"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatCleanupReport() = %q, missing %q", out, want)
		}
	}
}

func TestFormatCleanupReportDryRun(t *testing.T) {
	report := runtime.CleanupReport{DryRun: true}
	out := formatCleanupReport(report)
	if !strings.Contains(out, "dry-run") {
		t.Errorf("formatCleanupReport() = %q, expected dry-run marker", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestFormatCleanupReport" -v -count=1`

Expected: FAIL with `cleanup command not found: unknown command "cleanup" for "tengiz"` and `undefined: formatCleanupReport`

- [ ] **Step 3: Write the CLI command**

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
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Remove unused Docker resources to reclaim disk space.

Runs label-based housekeeping that never removes containers managed by Tengiz
(those labeled tengiz-app=...). Select categories with flags; the default runs
all categories. Use --dry-run to preview what would be removed.

Examples:
  tengiz cleanup                     # clean containers, images, cache, volumes
  tengiz cleanup --containers        # only stopped foreign containers
  tengiz cleanup --images --cache    # only dangling images and build cache
  tengiz cleanup --dry-run           # preview without removing anything`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		cache, _ := cmd.Flags().GetBool("cache")
		volumes, _ := cmd.Flags().GetBool("volumes")

		if all || (!containers && !images && !cache && !volumes) {
			containers, images, cache, volumes = true, true, true, true
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			BuildCache: cache,
			Volumes:    volumes,
			DryRun:     dryRun,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Print(formatCleanupReport(report))
		return nil
	},
}

func formatCleanupReport(report runtime.CleanupReport) string {
	var b strings.Builder
	if report.DryRun {
		b.WriteString("[tengiz] cleanup dry-run — nothing removed\n")
	} else {
		b.WriteString("[tengiz] cleanup complete\n")
	}
	fmt.Fprintf(&b, "  containers removed:  %d\n", report.ContainersRemoved)
	fmt.Fprintf(&b, "  images removed:      %d\n", report.ImagesRemoved)
	fmt.Fprintf(&b, "  build cache pruned:  %v\n", report.BuildCachePruned)
	fmt.Fprintf(&b, "  volumes pruned:      %v\n", report.VolumesPruned)
	return b.String()
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without removing anything")
	cleanupCmd.Flags().Bool("all", false, "clean containers, images, build cache, and volumes")
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images")
	cleanupCmd.Flags().Bool("cache", false, "prune the Docker build cache")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused Docker volumes")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestFormatCleanupReport" -v -count=1`

Expected: PASS

- [ ] **Step 5: Build the binary and smoke-check help output**

Run: `go build -o tengiz . && ./tengiz cleanup --help`

Expected: prints the command's Long help text including all six flags

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 5: Update documentation

**Files:**
- Modify: `README.md` — add `tengiz cleanup` section after `tengiz rollback` (after line 236)
- Modify: `AGENTS.md` — add CLI line after `tengiz rm` (line 50)
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:**
- Consumes: the `cleanupCmd` behavior implemented in Task 4 (flags: `--dry-run`, `--all`, `--containers`, `--images`, `--cache`, `--volumes`; label-based protection of `tengiz-app` containers)

- [ ] **Step 1: Add the command to README.md**

Insert after line 236 (the `tengiz rollback` table, before `### tengiz domain`):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space. Containers managed by Tengiz (labeled `tengiz-app=<name>`) are never removed.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling (untagged) images |
| `--cache` | Prune the Docker build cache |
| `--volumes` | Prune unused Docker volumes |
| `--all` | Clean all four categories (default when no category flag is given) |
| `--dry-run` | Preview what would be removed without removing anything |

```bash
tengiz cleanup              # clean everything
tengiz cleanup --dry-run    # preview first
tengiz cleanup --containers # only stopped foreign containers
```
```

- [ ] **Step 2: Add the command to AGENTS.md**

After the line `tengiz rm  → lifecycle` (line 50), add:

```
tengiz cleanup             → prune stopped foreign containers, dangling images, build cache, unused volumes (--dry-run to preview)
```

- [ ] **Step 3: Mark feature #6 implemented in FUTURES_FEATURES.md**

Change the P0 row (line 19) from `⬜` to `✅` and append the date:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. Implemented (2026-08-01). |
```

- [ ] **Step 4: Run full test suite + vet**

Run: `go test ./... -v -count=1 && go vet ./...`

Expected: all tests PASS, vet clean

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage:**
- Feature #6 (Docker Housekeeping) core deliverable `tengiz cleanup` → Tasks 1-4
- "Label-based docker system prune" / Tengiz-managed containers protected → `parseForeignContainers` skips `tengiz-app=` labels (Task 2), CLI doc mentions it (Task 5)
- "Disk space is #1 issue" → command covers containers + images + build cache + volumes (Task 3)
- AGENTS.md rule "Her değişiklikte test ekle/güncelle, testleri geçir, sonra commit et" → every task has a test cycle + commit
- AGENTS.md rule "UI/UX değişikliklerinde README.md ve dokümantasyonu güncelle" → Task 5
- Scope check: this is a single subsystem (host-level housekeeping); no need to split into multiple plans.

**2. Placeholder scan:** All steps contain complete code and exact commands. No TBD/TODO/"similar to Task N" references. Arg-builder and parse helper code is fully written out (not deferred to a later task).

**3. Type consistency:** `CleanupOptions{Containers, Images, BuildCache, Volumes, DryRun}` and `CleanupReport{ContainersRemoved, ImagesRemoved, BuildCachePruned, VolumesPruned, DryRun}` are defined once in Task 1 and used identically in Tasks 3 and 4. Helper names (`parseForeignContainers`, `hasLabel`, `parseImageIDs`, `stoppedContainerArgs`, `danglingImageArgs`, `buildCachePruneArgs`, `volumePruneArgs`) match across Tasks 2 and 3. The `formatCleanupReport` signature is consistent between Task 4 test and implementation.
