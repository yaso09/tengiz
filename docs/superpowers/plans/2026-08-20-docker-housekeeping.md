# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning unused Docker resources — containers, images, volumes, networks, and build cache — while protecting all Tengiz-managed containers via the `tengiz-app` label.

**Architecture:** New methods on the `runtime.Manager` interface (`PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache`, `SystemDF`) implemented in `internal/runtime/cleanup.go` via `os/exec` docker calls, exactly like the existing `dockerRuntime` code. Container pruning uses the `label!=tengiz-app` filter so Tengiz containers are never removed. Image pruning removes dangling images plus per-app old images beyond a keep count, reusing a refactored `imageLinesToRemove` helper extracted from the existing `KeepLastNImages`. The CLI wires these into `tengiz cleanup` with category flags, `--dry-run`, `--force`, and a confirmation prompt.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` docker CLI (no Docker SDK), existing `runtime.Manager`, `config.Store`. No new external dependencies.

## Global Constraints

- Protect every Tengiz-managed container: all container pruning MUST filter with `label!=tengiz-app` (Tengiz containers carry `tengiz-app=<app>` from `internal/runtime/docker.go`).
- Preserve rollback images: image pruning MUST keep the newest N images per app (default 5) using the same ordering/skip-`:latest` behavior as the existing `KeepLastNImages`.
- `--dry-run` MUST never modify anything; it lists what would be removed.
- Without `--force` (and not in dry-run), `tengiz cleanup` MUST prompt for confirmation before removing anything.
- No category flag + no `--all` → treat as `--all` (default behavior).
- Feature work happens on branch `feat/docker-housekeeping` (`git checkout -b feat/docker-housekeeping`).
- Every task ends with tests passing (`go test ./... -count=1`) and a commit.
- Existing tests must continue to pass — 3 test mocks (`internal/cli/root_test.go`, `internal/idle/idle_test.go`, `internal/proxy/proxy_test.go`) must gain the new interface methods.
- No new external dependencies.
- Docs updates required (README.md CLI reference + `docs/FUTURES_FEATURES.md` mark feature #6 implemented).

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Refactor `KeepLastNImages`; add pure helpers (`nonEmptyLines`, `isHexID`, `parsePruneReport`, `parseBuildCacheCount`, `imageLinesToRemove`) and dockerRuntime prune methods (`PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache`, `SystemDF`) |
| `internal/runtime/cleanup_test.go` | Tests for pure helpers + stub prune methods |
| `internal/runtime/runtime.go` | Add `PruneReport` usage + 6 new `Manager` interface methods + stub implementations |
| `internal/cli/root_test.go` | Add 6 new methods to `mockRTForDeploy` (interface conformance) |
| `internal/idle/idle_test.go` | Add 6 new methods to `mockRuntime` (interface conformance) |
| `internal/proxy/proxy_test.go` | Add 6 new methods to `mockRuntime` (interface conformance) |
| `internal/cli/cleanup.go` | New `tengiz cleanup` Cobra command + pure helpers `resolveCleanupCategories`, `confirmCleanup`; self-registers in its own `init()` (preview.go pattern) |
| `internal/cli/cleanup_test.go` | Tests for `resolveCleanupCategories`, `confirmCleanup`, command registration |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

---

### Task 1: Pure prune helper functions + `imageLinesToRemove` refactor

**Files:**
- Modify: `internal/runtime/cleanup.go` (refactor `KeepLastNImages` to use new helpers)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.nonEmptyLines(s string) []string`, `runtime.isHexID(s string) bool`, `runtime.PruneReport{Count int; Reclaimed string}`, `runtime.parsePruneReport(output string) PruneReport`, `runtime.parseBuildCacheCount(output string) int`, `runtime.imageLinesToRemove(lines []string, keep int) []string`

These helpers parse `docker ... prune` output and compute which app images to remove. They are pure (no docker daemon needed) so they are fully unit-testable.

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

Expected: branch switched to `feat/docker-housekeeping`.

- [ ] **Step 2: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
package runtime

import (
	"testing"
)

func TestIsHexID(t *testing.T) {
	tests := []struct {
		in       string
		expected bool
	}{
		{"abc123def456", true},
		{"c3279c2e0f2c45c0b4f", true},
		{"nginx-alpine", false},
		{"123", false},
		{"", false},
		{"abc-123", false},
	}
	for _, tt := range tests {
		if got := isHexID(tt.in); got != tt.expected {
			t.Errorf("isHexID(%q) = %v, want %v", tt.in, got, tt.expected)
		}
	}
}

func TestParsePruneReport(t *testing.T) {
	containerOut := `Deleted Containers:
c3279c2e0f2c45c0b4f2c4a3d0b1f9e8d7c6b5a4d3f2e1c0b9a8d7f6e5d4c3b2a1
453c2f1e0d9c8b7a6f5e4d3c2b1a0f9e8d7c6b5a4d3f2e1c0b9a8d7f6e5d4c3b2a1
Total reclaimed space: 45.2MB
`
	r := parsePruneReport(containerOut)
	if r.Count != 2 {
		t.Errorf("container Count = %d, want 2", r.Count)
	}
	if r.Reclaimed != "45.2MB" {
		t.Errorf("Reclaimed = %q, want %q", r.Reclaimed, "45.2MB")
	}

	imageOut := `Deleted Images:
untagged: tengiz-apps/myapp:production-123
deleted: sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc123def456
untagged: tengiz-apps/myapp:production-122
deleted: sha256:def456abc123def456abc123def456abc123def456abc123def456abc123def456abc123
Total reclaimed space: 123.4MB
`
	r = parsePruneReport(imageOut)
	if r.Count != 4 {
		t.Errorf("image Count = %d, want 4", r.Count)
	}

	volOut := `Deleted Volumes:
myapp-data
other-data
Total reclaimed space: 0B
`
	r = parsePruneReport(volOut)
	if r.Count != 2 {
		t.Errorf("volume Count = %d, want 2", r.Count)
	}
}

func TestParseBuildCacheCount(t *testing.T) {
	out := `ID                                    RECLAIMABLE          DESCRIPTION                        LAST USED
abc123def456                          1.234GB                                  2026-08-01 12:00:00
def456abc123                          512MB                                    2026-08-02 12:00:00
Total: 1.746GB
`
	if got := parseBuildCacheCount(out); got != 2 {
		t.Errorf("parseBuildCacheCount = %d, want 2", got)
	}
}

func TestNonEmptyLines(t *testing.T) {
	got := nonEmptyLines("one\n\ntwo\n  \nthree\n")
	want := []string{"one", "two", "three"}
	if len(got) != len(want) {
		t.Fatalf("nonEmptyLines = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("nonEmptyLines[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestImageLinesToRemove(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:production-123|2026-08-01 10:00:00",
		"tengiz-apps/myapp:production-124|2026-08-02 10:00:00",
		"tengiz-apps/myapp:production-125|2026-08-03 10:00:00",
		"tengiz-apps/myapp:production-latest|2026-08-04 10:00:00",
	}
	got := imageLinesToRemove(lines, 2)
	want := []string{"tengiz-apps/myapp:production-123", "tengiz-apps/myapp:production-124"}
	if len(got) != len(want) {
		t.Fatalf("imageLinesToRemove = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("imageLinesToRemove[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if got := imageLinesToRemove(lines, 5); len(got) != 0 {
		t.Errorf("keep=5 should remove nothing, got %v", got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestIsHexID|TestParsePruneReport|TestParseBuildCacheCount|TestNonEmptyLines|TestImageLinesToRemove' -count=1`

Expected: FAIL to compile with errors like `undefined: isHexID`, `undefined: parsePruneReport`, `undefined: parseBuildCacheCount`, `undefined: nonEmptyLines`, `undefined: imageLinesToRemove`, `undefined: PruneReport`.

- [ ] **Step 4: Implement the helpers and refactor `KeepLastNImages`**

Replace the entire content of `internal/runtime/cleanup.go` with:

```go
package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	lines := r.listAppImages(ctx, appName)
	for _, tag := range imageLinesToRemove(lines, n) {
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
		}
	}
	return nil
}

// PruneReport summarizes the outcome of a single prune operation.
type PruneReport struct {
	Count     int    // number of items pruned (or that would be pruned in dry-run)
	Reclaimed string // human-readable space reclaimed, from docker output (may be "")
}

// nonEmptyLines splits s on newlines and returns non-blank lines, trimmed.
func nonEmptyLines(s string) []string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// isHexID reports whether s looks like a docker object ID (12+ lowercase hex chars).
func isHexID(s string) bool {
	if len(s) < 12 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// parsePruneReport parses `docker <resource> prune -f` output into a PruneReport.
// Docker prints "Deleted <Resource>:", one line per item, then
// "Total reclaimed space: <size>". Image prune prints "untagged: ..." and
// "deleted: sha256:..." lines. Counting is best-effort.
func parsePruneReport(output string) PruneReport {
	var r PruneReport
	inSection := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "total reclaimed space:"):
			r.Reclaimed = strings.TrimSpace(line[len("Total reclaimed space:"):])
			inSection = false
		case strings.HasPrefix(lower, "deleted ") && strings.HasSuffix(line, ":"):
			inSection = true // section header like "Deleted Containers:"
		case strings.HasPrefix(lower, "untagged:"), strings.HasPrefix(lower, "deleted:"), strings.HasPrefix(lower, "removed"):
			r.Count++
		case inSection:
			r.Count++
		case isHexID(line):
			r.Count++
		}
	}
	return r
}

// parseBuildCacheCount parses `docker builder prune -f` table output. Each cache
// entry line starts with a 12-char hex ID.
func parseBuildCacheCount(output string) int {
	var n int
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields[0]) == 12 && isHexID(fields[0]) {
			n++
		}
	}
	return n
}

// imageLinesToRemove returns the tags to remove from a `docker images --format
// "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}"` listing: all but the newest `keep`
// entries, preserving any tag ending in ":latest".
func imageLinesToRemove(lines []string, keep int) []string {
	type imageLine struct {
		tag       string
		createdAt string
	}
	parsed := make([]imageLine, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		parsed = append(parsed, imageLine{tag: parts[0], createdAt: parts[1]})
	}
	sort.Slice(parsed, func(i, j int) bool {
		return parsed[i].createdAt < parsed[j].createdAt
	})
	var toRemove []string
	for i := 0; i < len(parsed)-keep; i++ {
		if strings.HasSuffix(parsed[i].tag, ":latest") {
			continue
		}
		toRemove = append(toRemove, parsed[i].tag)
	}
	return toRemove
}
```

Note: `listAppImages` referenced above is defined in Task 2. The refactor preserves the existing `KeepLastNImages` behavior (oldest-first removal, `:latest` preserved).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestIsHexID|TestParsePruneReport|TestParseBuildCacheCount|TestNonEmptyLines|TestImageLinesToRemove|TestStubRemoveImage|TestStubKeepLastNImages' -count=1`

Expected: FAIL to compile — `listAppImages` is undefined (it is added in Task 2). This is expected: the package cannot build until Task 2 lands. Proceed to Task 2 before running the full suite.

- [ ] **Step 6: Commit (after Task 2 makes the package build)**

Run: `git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go && git commit -m "feat(runtime): add prune output parsing helpers"`

---

### Task 2: Manager interface methods, dockerRuntime prune implementations, stub + test-mock updates

**Files:**
- Modify: `internal/runtime/runtime.go` — add 6 methods to `Manager` interface + stub implementations
- Modify: `internal/runtime/cleanup.go` — add dockerRuntime prune methods + `listAppImages` + `runPrune` + `dockerList`
- Modify: `internal/cli/root_test.go`, `internal/idle/idle_test.go`, `internal/proxy/proxy_test.go` — add 6 methods to each mock
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.imageLinesToRemove`, `runtime.parsePruneReport`, `runtime.parseBuildCacheCount`, `runtime.PruneReport` (all from Task 1)
- Produces:
  - `runtime.PruneContainers(ctx context.Context, dryRun bool) (*PruneReport, error)`
  - `runtime.PruneImages(ctx context.Context, appNames []string, keep int, dryRun bool) (*PruneReport, error)`
  - `runtime.PruneVolumes(ctx context.Context, dryRun bool) (*PruneReport, error)`
  - `runtime.PruneNetworks(ctx context.Context, dryRun bool) (*PruneReport, error)`
  - `runtime.PruneBuildCache(ctx context.Context, dryRun bool) (*PruneReport, error)`
  - `runtime.SystemDF(ctx context.Context) (string, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubPruneMethods(t *testing.T) {
	ctx := context.Background()
	m := NewStub()

	rep, err := m.PruneContainers(ctx, false)
	if err != nil {
		t.Fatalf("PruneContainers: %v", err)
	}
	if rep == nil {
		t.Fatal("PruneContainers returned nil report")
	}

	rep, err = m.PruneImages(ctx, []string{"testapp"}, 5, true)
	if err != nil {
		t.Fatalf("PruneImages: %v", err)
	}
	if rep == nil {
		t.Fatal("PruneImages returned nil report")
	}

	if _, err := m.PruneVolumes(ctx, true); err != nil {
		t.Fatalf("PruneVolumes: %v", err)
	}
	if _, err := m.PruneNetworks(ctx, true); err != nil {
		t.Fatalf("PruneNetworks: %v", err)
	}
	if _, err := m.PruneBuildCache(ctx, true); err != nil {
		t.Fatalf("PruneBuildCache: %v", err)
	}
	if _, err := m.SystemDF(ctx); err != nil {
		t.Fatalf("SystemDF: %v", err)
	}
}
```

Update the imports at the top of `internal/runtime/cleanup_test.go`:

```go
package runtime

import (
	"context"
	"testing"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run TestStubPruneMethods -count=1`

Expected: FAIL to compile — `m.PruneContainers undefined (type Manager has no field or method PruneContainers)`, and the same for the other five methods.

- [ ] **Step 3: Add the 6 methods to the `Manager` interface + stub**

In `internal/runtime/runtime.go`, extend the `Manager` interface (after the `KeepLastNImages` line, before the closing `}` of the interface at line 49):

```go
	PruneContainers(ctx context.Context, dryRun bool) (*PruneReport, error)
	PruneImages(ctx context.Context, appNames []string, keep int, dryRun bool) (*PruneReport, error)
	PruneVolumes(ctx context.Context, dryRun bool) (*PruneReport, error)
	PruneNetworks(ctx context.Context, dryRun bool) (*PruneReport, error)
	PruneBuildCache(ctx context.Context, dryRun bool) (*PruneReport, error)
	SystemDF(ctx context.Context) (string, error)
```

In the same file, add stub implementations after the existing `func (m *stubManager) KeepLastNImages(...)` method:

```go
func (m *stubManager) PruneContainers(ctx context.Context, dryRun bool) (*PruneReport, error) {
	return &PruneReport{}, nil
}

func (m *stubManager) PruneImages(ctx context.Context, appNames []string, keep int, dryRun bool) (*PruneReport, error) {
	return &PruneReport{}, nil
}

func (m *stubManager) PruneVolumes(ctx context.Context, dryRun bool) (*PruneReport, error) {
	return &PruneReport{}, nil
}

func (m *stubManager) PruneNetworks(ctx context.Context, dryRun bool) (*PruneReport, error) {
	return &PruneReport{}, nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context, dryRun bool) (*PruneReport, error) {
	return &PruneReport{}, nil
}

func (m *stubManager) SystemDF(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 4: Add the dockerRuntime prune implementations**

Update the imports in `internal/runtime/cleanup.go`:

```go
import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
)
```

Append to `internal/runtime/cleanup.go`:

```go
// cleanupLabelFilter selects objects that are NOT managed by Tengiz. Tengiz
// containers always carry the "tengiz-app" label (see docker.go).
const cleanupLabelFilter = "label!=tengiz-app"

// runPrune streams a `docker <resource> prune` command to the terminal and
// captures its output for reporting.
func (r *dockerRuntime) runPrune(ctx context.Context, args []string) (*PruneReport, error) {
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	report := parsePruneReport(buf.String())
	return &report, nil
}

// dockerList runs a read-only docker command and returns its stdout, or "" on error.
func (r *dockerRuntime) dockerList(ctx context.Context, args ...string) string {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

func (r *dockerRuntime) PruneContainers(ctx context.Context, dryRun bool) (*PruneReport, error) {
	if dryRun {
		out := r.dockerList(ctx, "ps", "-a",
			"--filter", cleanupLabelFilter,
			"--filter", "status=exited",
			"--format", "{{.Names}}")
		names := nonEmptyLines(out)
		for _, n := range names {
			fmt.Printf("[tengiz] would remove container %s\n", n)
		}
		return &PruneReport{Count: len(names)}, nil
	}
	return r.runPrune(ctx, []string{"container", "prune", "-f", "--filter", cleanupLabelFilter})
}

// listAppImages lists all image tags for an app, newest-to-oldest not required;
// ordering is handled by imageLinesToRemove.
func (r *dockerRuntime) listAppImages(ctx context.Context, appName string) []string {
	out := r.dockerList(ctx, "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}")
	return nonEmptyLines(out)
}

func (r *dockerRuntime) PruneImages(ctx context.Context, appNames []string, keep int, dryRun bool) (*PruneReport, error) {
	report := &PruneReport{}

	// 1. Dangling (untagged) images — safe to remove, never referenced by a container.
	if dryRun {
		out := r.dockerList(ctx, "images", "--filter", "dangling=true", "--format", "{{.ID}}")
		ids := nonEmptyLines(out)
		for _, id := range ids {
			fmt.Printf("[tengiz] would remove dangling image %s\n", id)
		}
		report.Count += len(ids)
	} else {
		rep, err := r.runPrune(ctx, []string{"image", "prune", "-f"})
		if err != nil {
			return report, err
		}
		report.Count += rep.Count
		report.Reclaimed = rep.Reclaimed
	}

	// 2. Old per-app deployment images beyond the keep count (rollback preserved).
	for _, app := range appNames {
		lines := r.listAppImages(ctx, app)
		for _, tag := range imageLinesToRemove(lines, keep) {
			if dryRun {
				fmt.Printf("[tengiz] would remove image %s\n", tag)
				report.Count++
				continue
			}
			if err := r.RemoveImage(ctx, tag); err != nil {
				log.Printf("[tengiz] failed to remove image %s: %v", tag, err)
				continue
			}
			report.Count++
		}
	}
	return report, nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context, dryRun bool) (*PruneReport, error) {
	if dryRun {
		out := r.dockerList(ctx, "volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}")
		names := nonEmptyLines(out)
		for _, n := range names {
			fmt.Printf("[tengiz] would remove volume %s\n", n)
		}
		return &PruneReport{Count: len(names)}, nil
	}
	return r.runPrune(ctx, []string{"volume", "prune", "-f"})
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context, dryRun bool) (*PruneReport, error) {
	if dryRun {
		out := r.dockerList(ctx, "network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}")
		names := nonEmptyLines(out)
		for _, n := range names {
			fmt.Printf("[tengiz] would remove network %s\n", n)
		}
		return &PruneReport{Count: len(names)}, nil
	}
	return r.runPrune(ctx, []string{"network", "prune", "-f"})
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context, dryRun bool) (*PruneReport, error) {
	args := []string{"builder", "prune", "-f"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker builder prune: %w", err)
	}
	return &PruneReport{Count: parseBuildCacheCount(buf.String())}, nil
}

func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "system", "df").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w", err)
	}
	return string(out), nil
}
```

- [ ] **Step 5: Update the 3 test mocks to satisfy the extended interface**

In `internal/cli/root_test.go`, after the `func (m *mockRTForDeploy) Run(...)` method (line 100), add:

```go
func (m *mockRTForDeploy) PruneContainers(ctx context.Context, dryRun bool) (*runtime.PruneReport, error) { return &runtime.PruneReport{}, nil }
func (m *mockRTForDeploy) PruneImages(ctx context.Context, appNames []string, keep int, dryRun bool) (*runtime.PruneReport, error) { return &runtime.PruneReport{}, nil }
func (m *mockRTForDeploy) PruneVolumes(ctx context.Context, dryRun bool) (*runtime.PruneReport, error) { return &runtime.PruneReport{}, nil }
func (m *mockRTForDeploy) PruneNetworks(ctx context.Context, dryRun bool) (*runtime.PruneReport, error) { return &runtime.PruneReport{}, nil }
func (m *mockRTForDeploy) PruneBuildCache(ctx context.Context, dryRun bool) (*runtime.PruneReport, error) { return &runtime.PruneReport{}, nil }
func (m *mockRTForDeploy) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

In `internal/idle/idle_test.go`, after the `func (m *mockRuntime) Run(...)` method (line 35), add the same six methods (receiver `*mockRuntime`, using the same bodies as above).

In `internal/proxy/proxy_test.go`, after the `func (m *mockRuntime) Run(...)` method (line 35), add the same six methods (receiver `*mockRuntime`, using the same bodies as above).

- [ ] **Step 6: Run all tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: all packages build; `go vet` reports nothing; all tests PASS (including the Task 1 helper tests and the new `TestStubPruneMethods`).

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/ internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add docker housekeeping prune methods"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneContainers/PruneImages/PruneVolumes/PruneNetworks/PruneBuildCache/SystemDF`, `config.NewStoreWithEnv(dataDir, env).ListApps()`, package vars `dataDir` and `getEnv(cmd)` from `root.go`
- Produces: `cleanupCmd` (Cobra command, self-registered via its own `init()`), `cli.resolveCleanupCategories(all, containers, images, volumes, networks, buildCache bool) []cleanupCategory`, `cli.confirmCleanup(in io.Reader) (bool, error)`, `cli.cleanupCategory` constants

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestResolveCleanupCategories(t *testing.T) {
	// No flags -> all categories
	cats := resolveCleanupCategories(false, false, false, false, false, false)
	if len(cats) != 5 {
		t.Fatalf("no flags should resolve to all 5 categories, got %v", cats)
	}

	// --all -> all categories
	cats = resolveCleanupCategories(true, false, false, false, false, false)
	if len(cats) != 5 {
		t.Fatalf("--all should resolve to all 5 categories, got %v", cats)
	}

	// explicit containers only
	cats = resolveCleanupCategories(false, true, false, false, false, false)
	if len(cats) != 1 || cats[0] != cleanupContainers {
		t.Fatalf("expected [containers], got %v", cats)
	}

	// explicit images + build-cache
	cats = resolveCleanupCategories(false, false, true, false, false, true)
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories, got %v", cats)
	}
	if cats[0] != cleanupImages || cats[1] != cleanupBuildCache {
		t.Fatalf("expected [images build-cache], got %v", cats)
	}
}

func TestConfirmCleanup(t *testing.T) {
	if ok, _ := confirmCleanup(strings.NewReader("y\n")); !ok {
		t.Error("expected yes for 'y'")
	}
	if ok, _ := confirmCleanup(strings.NewReader("yes\n")); !ok {
		t.Error("expected yes for 'yes'")
	}
	if ok, _ := confirmCleanup(strings.NewReader("n\n")); ok {
		t.Error("expected no for 'n'")
	}
	if ok, _ := confirmCleanup(strings.NewReader("\n")); ok {
		t.Error("expected no for empty input")
	}
	if ok, _ := confirmCleanup(strings.NewReader("")); ok {
		t.Error("expected no for EOF")
	}
	if ok, _ := confirmCleanup(strings.NewReader("anything\n")); ok {
		t.Error("expected no for arbitrary input")
	}
}

func TestCleanupCmdRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "cleanup" {
			found = true
			break
		}
	}
	if !found {
		t.Error("cleanup command not registered on root")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestResolveCleanupCategories|TestConfirmCleanup|TestCleanupCmdRegistered' -count=1`

Expected: FAIL to compile — `undefined: resolveCleanupCategories`, `undefined: confirmCleanup`, `undefined: cleanupContainers`, `undefined: cleanupImages`, `undefined: cleanupBuildCache`, `undefined: cleanupCmd`.

- [ ] **Step 3: Implement the command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

type cleanupCategory int

const (
	cleanupContainers cleanupCategory = iota
	cleanupImages
	cleanupVolumes
	cleanupNetworks
	cleanupBuildCache
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long: `Remove unused Docker resources to reclaim disk space.

By default removes: stopped containers not managed by Tengiz (protected by the
tengiz-app label), dangling images, old app images beyond the keep count,
unused volumes, unused networks, and build cache.

Use category flags to limit what is removed. Use --dry-run to preview.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")
		keep, _ := cmd.Flags().GetInt("keep")

		cats := resolveCleanupCategories(all, containers, images, volumes, networks, buildCache)

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}

		store := config.NewStoreWithEnv(dataDir, env)
		apps, _ := store.ListApps()
		appNames := make([]string, 0, len(apps))
		for _, a := range apps {
			appNames = append(appNames, a.Name)
		}
		sort.Strings(appNames)

		if df, dfErr := rt.SystemDF(cmd.Context()); dfErr == nil {
			fmt.Print(df)
		}

		if !dryRun && !force {
			ok, err := confirmCleanup(os.Stdin)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		total := 0
		for _, c := range cats {
			switch c {
			case cleanupContainers:
				rep, err := rt.PruneContainers(cmd.Context(), dryRun)
				if err != nil {
					return fmt.Errorf("prune containers: %w", err)
				}
				total += rep.Count
			case cleanupImages:
				rep, err := rt.PruneImages(cmd.Context(), appNames, keep, dryRun)
				if err != nil {
					return fmt.Errorf("prune images: %w", err)
				}
				total += rep.Count
			case cleanupVolumes:
				rep, err := rt.PruneVolumes(cmd.Context(), dryRun)
				if err != nil {
					return fmt.Errorf("prune volumes: %w", err)
				}
				total += rep.Count
			case cleanupNetworks:
				rep, err := rt.PruneNetworks(cmd.Context(), dryRun)
				if err != nil {
					return fmt.Errorf("prune networks: %w", err)
				}
				total += rep.Count
			case cleanupBuildCache:
				rep, err := rt.PruneBuildCache(cmd.Context(), dryRun)
				if err != nil {
					return fmt.Errorf("prune build cache: %w", err)
				}
				total += rep.Count
			}
		}

		if dryRun {
			fmt.Printf("[tengiz] dry-run: %d item(s) would be removed\n", total)
		} else {
			fmt.Printf("[tengiz] cleanup complete: %d item(s) removed\n", total)
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "remove stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images and old app images (keeps newest N per app)")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "remove build cache")
	cleanupCmd.Flags().BoolP("all", "a", false, "remove all unused resources (default)")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("force", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Int("keep", 5, "keep the newest N images per app when pruning images")
	rootCmd.AddCommand(cleanupCmd)
}

// resolveCleanupCategories returns the cleanup categories to run. No category
// flag (or --all) means all categories.
func resolveCleanupCategories(all, containers, images, volumes, networks, buildCache bool) []cleanupCategory {
	if all || (!containers && !images && !volumes && !networks && !buildCache) {
		return []cleanupCategory{cleanupContainers, cleanupImages, cleanupVolumes, cleanupNetworks, cleanupBuildCache}
	}
	var cats []cleanupCategory
	if containers {
		cats = append(cats, cleanupContainers)
	}
	if images {
		cats = append(cats, cleanupImages)
	}
	if volumes {
		cats = append(cats, cleanupVolumes)
	}
	if networks {
		cats = append(cats, cleanupNetworks)
	}
	if buildCache {
		cats = append(cats, cleanupBuildCache)
	}
	return cats
}

// confirmCleanup prompts for y/N confirmation. Returns (false, nil) on any
// answer other than y/yes, and on EOF.
func confirmCleanup(in io.Reader) (bool, error) {
	fmt.Print("This will remove unused Docker resources. Continue? [y/N] ")
	var answer string
	if _, err := fmt.Fscanln(in, &answer); err != nil {
		if err == io.EOF {
			return false, nil
		}
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
```

Note: this follows the `internal/cli/preview.go` pattern — the command self-registers in its own `init()`; `dataDir` and `getEnv` come from `root.go` (same package).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/cli/ -run 'TestResolveCleanupCategories|TestConfirmCleanup|TestCleanupCmdRegistered' -count=1`

Expected: builds cleanly; `go vet` reports nothing; all three tests PASS.

- [ ] **Step 5: Verify the full suite**

Run: `go test ./... -count=1`

Expected: all tests PASS.

- [ ] **Step 6: Manual smoke test (requires docker installed)**

```bash
go build -o tengiz .
./tengiz cleanup --dry-run
```

Expected: prints `docker system df` table, then lists "would remove ..." items (or none), then `[tengiz] dry-run: N item(s) would be removed`. Nothing is actually removed.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md` (add `tengiz cleanup` to CLI Reference)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 Docker Housekeeping implemented)

**Interfaces:**
- Consumes: nothing
- Produces: updated user-facing documentation reflecting the new command

- [ ] **Step 1: Document `tengiz cleanup` in README.md**

In `README.md`, after the `### tengiz rm <app>` section (ends around line 229), insert:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `-a`, `--all` | Remove all unused resources (default) |
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling images and old app images (keeps newest `--keep` per app) |
| `--volumes` | Remove unused volumes |
| `--networks` | Remove unused networks |
| `--build-cache` | Remove build cache |
| `--dry-run` | Show what would be removed without removing anything |
| `--force` | Skip the confirmation prompt |
| `--keep N` | Keep the newest N images per app (default: 5) |

Tengiz-managed containers are protected via the `tengiz-app` label and are never
pruned. Old deployment images are preserved for rollback (default 5 per app).
Without `--force`, prompts for confirmation before removing anything.
```

- [ ] **Step 2: Mark feature #6 implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`:

a) In the P0 table (line 19), change the #6 row so the feature name gains `✅`:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

b) In the `### ✅ Implemented Features (Not Pending)` table (after the existing rows), add:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-20) |
```

c) In the `## Docker Housekeeping (Otomatik Temizlik)` detailed section (around line 377), add a Status line after the `- **Why add to Tengiz:**` line:

```
- **Status:** ✅ Implemented (2026-08-20)
```

- [ ] **Step 3: Verify the full suite one final time**

Run: `go build -o tengiz . && go vet ./... && go test ./... -count=1`

Expected: binary builds; `go vet` reports nothing; all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**Spec coverage:**
- Feature #6 (Docker Housekeeping): label-based protection (`label!=tengiz-app`) ✓ (Task 2), `tengiz cleanup` command ✓ (Task 3), prunes containers/images/volumes/networks/build cache ✓ (Task 2+3), disk reclaim via `docker system df` display ✓ (Task 2+3).
- Coolify rationale ("Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur"): container prune filter protects all Tengiz containers ✓.
- AGENTS.md rule "Yeni özellik geliştirirken branch oluştur": Task 1 Step 1 ✓.
- AGENTS.md rule "test ekle/güncelle, testleri geçir, sonra commit et": every task has TDD + commit ✓.
- AGENTS.md rule "UI/UX değişikliklerinde README.md ve dokümantasyonu güncelle": Task 4 ✓.

**Placeholder scan:** No TBD/TODO/lazy instructions; every code step shows complete code; every command has expected output.

**Type consistency:**
- `runtime.PruneReport{Count int; Reclaimed string}` — defined Task 1, used in all Task 2 method signatures and stub returns, referenced as `*runtime.PruneReport` in Task 3 CLI and Task 2 mock updates. Consistent.
- `runtime.PruneContainers(ctx, dryRun bool) (*PruneReport, error)` — identical across interface, dockerRuntime, stub, mocks, and CLI call site.
- `runtime.PruneImages(ctx, appNames []string, keep int, dryRun bool) (*PruneReport, error)` — identical everywhere.
- `runtime.SystemDF(ctx) (string, error)` — identical everywhere.
- `imageLinesToRemove(lines []string, keep int) []string` — used by both `KeepLastNImages` (Task 1 refactor) and `PruneImages` (Task 2). Consistent.
- `resolveCleanupCategories(all, containers, images, volumes, networks, buildCache bool) []cleanupCategory` and `confirmCleanup(in io.Reader) (bool, error)` — Task 3 tests and implementation use identical signatures.
- `cleanupContainers/cleanupImages/cleanupVolumes/cleanupNetworks/cleanupBuildCache` constants — defined in Task 3, referenced consistently in both test and implementation.