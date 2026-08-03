# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped non-Tengiz containers, dangling images, unused volumes/networks, build cache) with label-based protection of Tengiz-managed containers, granular category flags, dry-run preview, and optional periodic mode.

**Architecture:** A `runtime.Manager.Prune(ctx, opts) (PruneSummary, error)` method runs category-scoped Docker CLI commands (`docker ps`, `docker images`, `docker volume prune`, `docker network prune`, `docker builder prune`, `docker system df`) via the existing `os/exec` pattern. Because `docker ps` does **not** support the negated `label!=` filter (only prune commands do), stopped-container candidates are listed with the format template `{{.ID}}|{{.Label "tengiz-app"}}` and filtered in Go so containers carrying the `tengiz-app` label (Tengiz-managed, including previews) are never removed. The CLI command lives in a new `internal/cli/cleanup.go` file (root.go is already 1780+ lines) with its own `init()` for registration.

**Tech Stack:** Go 1.26, `os/exec` (no Docker SDK), Cobra CLI. No new external dependencies.

## Global Constraints

- No new external dependencies — stdlib only (`context`, `encoding/json`, `fmt`, `log`, `os`, `os/exec`, `strconv`, `strings`, `time`)
- Follow the existing `os/exec` docker CLI pattern in `internal/runtime/` — never call the Docker SDK
- Tengiz-managed containers (those carrying the `tengiz-app=<name>` label, set in `Create`/`CreateVersioned`/`buildRunArgs`) must never be removed — they include scale-to-zero stopped apps and preview deployments
- `docker ps` does NOT support `--filter label!=tengiz-app` (it errors with "invalid filter 'label!'") — candidates must be listed with `--format '{{.ID}}|{{.Label "tengiz-app"}}'` and filtered in Go
- Containers are removed by per-ID `docker rm -f` (never `docker container prune`, which cannot exclude labeled containers)
- Default categories: `containers`, `images`, `networks`, `build cache` enabled; `volumes` disabled (opt-in for safety)
- `docker system df --format json` output is one JSON object per line; `.Type` values are `Images`, `Containers`, `Local Volumes`, `Build Cache`
- In `--interval` mode the confirmation prompt appears only once, before the first run
- `--dry-run` performs no confirmation prompt and no removal
- `go build -o tengiz .`, `go vet ./...`, and `go test ./... -v -count=1` must all pass
- The `Manager` interface change requires updating three test mocks (`root_test.go`, `idle_test.go`, `proxy_test.go`) in the same task to keep the build green
- Docs must be updated (README.md, AGENTS.md, FUTURES_FEATURES.md) per AGENTS.md rules

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/housekeeping.go` | **Create.** `PruneOptions`/`PruneSummary` types, pure arg-builders + parse helpers, and the `dockerRuntime.Prune` exec implementation |
| `internal/runtime/housekeeping_test.go` | **Create.** Table tests for all pure helpers + stub `Prune` test |
| `internal/runtime/runtime.go` | **Modify.** Add `Prune` to `Manager` interface + stub implementation |
| `internal/cli/cleanup.go` | **Create.** `tengiz cleanup` Cobra command + its own `init()` registration + `confirmCleanup`/`printCleanupSummary`/`humanBytes`/`cleanupOptsFromFlags` helpers |
| `internal/cli/cleanup_test.go` | **Create.** Tests for command registration, flag defaults, opts mapping, confirmation, summary printing, `humanBytes` |
| `internal/cli/root_test.go` | **Modify.** Add `Prune` to `mockRTForDeploy` (line ~100) so it still satisfies `Manager` |
| `internal/idle/idle_test.go` | **Modify.** Add `Prune` to `mockRuntime` (line ~34) |
| `internal/proxy/proxy_test.go` | **Modify.** Add `Prune` to `mockRuntime` (line ~35) |
| `README.md` | **Modify.** Add `tengiz cleanup` CLI reference section |
| `AGENTS.md` | **Modify.** Add `tengiz cleanup` to the Commands block |
| `docs/FUTURES_FEATURES.md` | **Modify.** Mark feature #6 Docker Housekeeping as ✅ Implemented |

---

### Task 1: Runtime prune types + pure Docker CLI helpers

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: nothing new (stdlib only)
- Produces: `runtime.PruneOptions{Containers, Images, Volumes, Networks, BuildCache, DryRun bool}`, `runtime.PruneSummary{Containers, Images, Volumes, Networks, BuildCache int; ReclaimedBytes int64}`, `runtime.DefaultPruneOptions() PruneOptions`, `candidateQueryArgs(kind string) []string`, `pruneCommandArgs(kind string) []string`, `parseContainerCandidates(output string) []string`, `parseSystemDF(output string) systemDiskStats`, `parseDiskSize(s string) (int64, error)`, `countNonEmptyLines(s string) int`, `systemDiskStats{buildCacheCount, buildCacheActive int; totalReclaimable int64}`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/housekeeping_test.go`:

```go
package runtime

import (
	"strings"
	"testing"
)

func TestCandidateQueryArgs(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{"containers", `ps -a --filter status=exited --format {{.ID}}|{{.Label "tengiz-app"}}`},
		{"images", "images -q --filter dangling=true"},
		{"volumes", "volume ls -q --filter dangling=true"},
		{"networks", "network ls -q --filter dangling=true"},
	}
	for _, tt := range tests {
		got := strings.Join(candidateQueryArgs(tt.kind), " ")
		if got != tt.want {
			t.Errorf("candidateQueryArgs(%q) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestPruneCommandArgs(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{"images", "image prune -f"},
		{"volumes", "volume prune -f"},
		{"networks", "network prune -f"},
		{"buildcache", "builder prune -f"},
	}
	for _, tt := range tests {
		got := strings.Join(pruneCommandArgs(tt.kind), " ")
		if got != tt.want {
			t.Errorf("pruneCommandArgs(%q) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestParseContainerCandidates(t *testing.T) {
	output := strings.Join([]string{
		"abc123|",      // no Tengiz label -> candidate
		"def456|myapp", // Tengiz-managed -> skipped
		"ghi789|",      // no label -> candidate
		"",             // blank line
		"jkl012|myapp", // Tengiz-managed -> skipped
	}, "\n")
	got := parseContainerCandidates(output)
	want := []string{"abc123", "ghi789"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseSystemDF(t *testing.T) {
	output := strings.Join([]string{
		`{"Active":"2","Reclaimable":"2.498GB (94%)","Size":"2.631GB","TotalCount":"6","Type":"Images"}`,
		`{"Active":"1","Reclaimable":"1.114kB (49%)","Size":"2.23kB","TotalCount":"7","Type":"Containers"}`,
		`{"Active":"0","Reclaimable":"256.5MB (100%)","Size":"256.5MB","TotalCount":"1","Type":"Local Volumes"}`,
		`{"Active":"0","Reclaimable":"158B","Size":"158B","TotalCount":"17","Type":"Build Cache"}`,
	}, "\n")
	stats := parseSystemDF(output)
	if stats.buildCacheCount != 17 {
		t.Errorf("buildCacheCount = %d, want 17", stats.buildCacheCount)
	}
	if stats.buildCacheActive != 0 {
		t.Errorf("buildCacheActive = %d, want 0", stats.buildCacheActive)
	}
	if stats.totalReclaimable <= 0 {
		t.Errorf("totalReclaimable = %d, want > 0", stats.totalReclaimable)
	}
}

func TestParseDiskSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0B", 0},
		{"512B", 512},
		{"1kB", 1024},
		{"1KB", 1024},
		{"1MB", 1024 * 1024},
		{"1GB", 1024 * 1024 * 1024},
		{"1.5GB (94%)", 1610612736},
		{"158B", 158},
	}
	for _, tt := range tests {
		got, err := parseDiskSize(tt.in)
		if err != nil {
			t.Fatalf("parseDiskSize(%q) error = %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("parseDiskSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestCountNonEmptyLines(t *testing.T) {
	if got := countNonEmptyLines(""); got != 0 {
		t.Errorf("countNonEmptyLines(\"\") = %d, want 0", got)
	}
	if got := countNonEmptyLines("\n\n"); got != 0 {
		t.Errorf("countNonEmptyLines(\"\\n\\n\") = %d, want 0", got)
	}
	if got := countNonEmptyLines("a\n\nb\n"); got != 2 {
		t.Errorf("countNonEmptyLines = %d, want 2", got)
	}
}

func TestDefaultPruneOptions(t *testing.T) {
	opts := DefaultPruneOptions()
	if !opts.Containers || !opts.Images || opts.Volumes || !opts.Networks || !opts.BuildCache || opts.DryRun {
		t.Errorf("unexpected DefaultPruneOptions: %+v", opts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestCandidateQueryArgs|TestPruneCommandArgs|TestParseContainerCandidates|TestParseSystemDF|TestParseDiskSize|TestCountNonEmptyLines|TestDefaultPruneOptions' -v -count=1`
Expected: FAIL — `undefined: candidateQueryArgs` (and the other helpers/types)

- [ ] **Step 3: Write minimal implementation**

Create `internal/runtime/housekeeping.go`:

```go
package runtime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type PruneSummary struct {
	Containers     int
	Images         int
	Volumes        int
	Networks       int
	BuildCache     int
	ReclaimedBytes int64
}

func DefaultPruneOptions() PruneOptions {
	return PruneOptions{Containers: true, Images: true, Volumes: false, Networks: true, BuildCache: true}
}

// candidateQueryArgs returns docker args that list candidate resource IDs for a kind.
func candidateQueryArgs(kind string) []string {
	switch kind {
	case "containers":
		return []string{"ps", "-a", "--filter", "status=exited", "--format", "{{.ID}}|{{.Label \"tengiz-app\"}}"}
	case "images":
		return []string{"images", "-q", "--filter", "dangling=true"}
	case "volumes":
		return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
	case "networks":
		return []string{"network", "ls", "-q", "--filter", "dangling=true"}
	}
	return nil
}

// pruneCommandArgs returns docker args that remove all candidates for a kind.
func pruneCommandArgs(kind string) []string {
	switch kind {
	case "images":
		return []string{"image", "prune", "-f"}
	case "volumes":
		return []string{"volume", "prune", "-f"}
	case "networks":
		return []string{"network", "prune", "-f"}
	case "buildcache":
		return []string{"builder", "prune", "-f"}
	}
	return nil
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}

func countNonEmptyLines(s string) int {
	return len(nonEmptyLines(s))
}

// parseContainerCandidates parses `docker ps --format '{{.ID}}|{{.Label "tengiz-app"}}'`
// output and returns IDs of stopped containers that do NOT carry the Tengiz label.
func parseContainerCandidates(output string) []string {
	var ids []string
	for _, line := range nonEmptyLines(output) {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && parts[1] == "" {
			ids = append(ids, parts[0])
		}
	}
	return ids
}

type dfRow struct {
	Active      string
	Reclaimable string
	Size        string
	TotalCount  string
	Type        string
}

type systemDiskStats struct {
	buildCacheCount  int
	buildCacheActive int
	totalReclaimable int64
}

// parseSystemDF parses `docker system df --format json` output (one JSON object per
// line) into per-type counts and total reclaimable bytes.
func parseSystemDF(output string) systemDiskStats {
	var stats systemDiskStats
	for _, line := range nonEmptyLines(output) {
		var row dfRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(row.TotalCount)); err == nil && row.Type == "Build Cache" {
			stats.buildCacheCount = n
		}
		if n, err := strconv.Atoi(strings.TrimSpace(row.Active)); err == nil && row.Type == "Build Cache" {
			stats.buildCacheActive = n
		}
		if n, err := parseDiskSize(row.Reclaimable); err == nil {
			stats.totalReclaimable += n
		}
	}
	return stats
}

// parseDiskSize parses Docker human-readable size strings ("512B", "1.5GB",
// "2.498GB (94%)") into bytes.
func parseDiskSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	var num float64
	var unit string
	if _, err := fmt.Sscanf(s, "%g%s", &num, &unit); err != nil {
		return 0, fmt.Errorf("parse disk size %q: %w", s, err)
	}
	var mult float64
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "B":
		mult = 1
	case "KB", "KIB":
		mult = 1 << 10
	case "MB", "MIB":
		mult = 1 << 20
	case "GB", "GIB":
		mult = 1 << 30
	case "TB", "TIB":
		mult = 1 << 40
	default:
		mult = 1
	}
	return int64(num * mult), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestCandidateQueryArgs|TestPruneCommandArgs|TestParseContainerCandidates|TestParseSystemDF|TestParseDiskSize|TestCountNonEmptyLines|TestDefaultPruneOptions' -v -count=1`
Expected: PASS (all 7 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat(runtime): add docker housekeeping prune types and helpers"
```

---

### Task 2: `Manager.Prune` interface method + docker exec implementation

**Files:**
- Modify: `internal/runtime/housekeeping.go` — add docker exec methods + imports
- Modify: `internal/runtime/runtime.go:31-49` — add `Prune` to interface; add stub method after line 119
- Modify: `internal/cli/root_test.go` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go` — add `Prune` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go` — add `Prune` to `mockRuntime`
- Test: `internal/runtime/housekeeping_test.go` — add `TestStubPrune`

**Interfaces:**
- Consumes: Task 1's `PruneOptions`, `PruneSummary`, `candidateQueryArgs`, `pruneCommandArgs`, `parseContainerCandidates`, `parseSystemDF`, `systemDiskStats`, `nonEmptyLines`
- Produces: `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error)` implemented by `dockerRuntime` and `stubManager`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/housekeeping_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	s, err := m.Prune(context.Background(), DefaultPruneOptions())
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if s != (PruneSummary{}) {
		t.Errorf("Prune() summary = %+v, want zero value", s)
	}
}
```

Add the missing `"context"` import to `housekeeping_test.go`.

Also add `Prune` to the three mock runtimes so the interface assertions still compile:

In `internal/cli/root_test.go`, after the `KeepLastNImages` line (line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneSummary, error) { return runtime.PruneSummary{}, nil }
```

In `internal/idle/idle_test.go`, after the `KeepLastNImages` line (line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneSummary, error) { return runtime.PruneSummary{}, nil }
```

In `internal/proxy/proxy_test.go`, after the `KeepLastNImages` line (line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneSummary, error) { return runtime.PruneSummary{}, nil }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: FAIL — `m.Prune undefined (type Manager has no field or method Prune)`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/runtime/runtime.go` interface (after the `KeepLastNImages` line):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error)
```

Add to `internal/runtime/runtime.go` stub (after the existing `KeepLastNImages` stub method):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error) {
	return PruneSummary{}, nil
}
```

Update `internal/runtime/housekeeping.go` imports to:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
)
```

Append to `internal/runtime/housekeeping.go`:

```go
func (r *dockerRuntime) dockerOutput(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) runPrune(ctx context.Context, kind string) error {
	args := pruneCommandArgs(kind)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return nil
}

func (r *dockerRuntime) candidatesFor(ctx context.Context, kind string) ([]string, error) {
	out, err := r.dockerOutput(ctx, candidateQueryArgs(kind))
	if err != nil {
		return nil, err
	}
	if kind == "containers" {
		return parseContainerCandidates(out), nil
	}
	return nonEmptyLines(out), nil
}

func (r *dockerRuntime) removeCandidates(ctx context.Context, kind string, ids []string) error {
	if kind == "containers" {
		for _, id := range ids {
			cmd := exec.CommandContext(ctx, "docker", "rm", "-f", id)
			if out, err := cmd.CombinedOutput(); err != nil {
				log.Printf("[runtime] cleanup: failed to remove container %s: %v\n%s", id, err, string(out))
			}
		}
		return nil
	}
	return r.runPrune(ctx, kind)
}

func (r *dockerRuntime) systemDiskUsage(ctx context.Context) (systemDiskStats, error) {
	out, err := r.dockerOutput(ctx, []string{"system", "df", "--format", "json"})
	if err != nil {
		return systemDiskStats{}, err
	}
	return parseSystemDF(out), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error) {
	var summary PruneSummary

	before, err := r.systemDiskUsage(ctx)
	if err != nil {
		return summary, err
	}
	if opts.BuildCache {
		if opts.DryRun {
			n := before.buildCacheCount - before.buildCacheActive
			if n < 0 {
				n = 0
			}
			summary.BuildCache = n
		} else {
			summary.BuildCache = before.buildCacheCount
		}
	}

	steps := []struct {
		kind    string
		enabled bool
	}{
		{"containers", opts.Containers},
		{"images", opts.Images},
		{"volumes", opts.Volumes},
		{"networks", opts.Networks},
	}
	for _, step := range steps {
		if !step.enabled {
			continue
		}
		ids, err := r.candidatesFor(ctx, step.kind)
		if err != nil {
			return summary, err
		}
		count := len(ids)
		switch step.kind {
		case "containers":
			summary.Containers = count
		case "images":
			summary.Images = count
		case "volumes":
			summary.Volumes = count
		case "networks":
			summary.Networks = count
		}
		if opts.DryRun {
			continue
		}
		if err := r.removeCandidates(ctx, step.kind, ids); err != nil {
			return summary, err
		}
	}

	if opts.BuildCache && !opts.DryRun {
		if err := r.runPrune(ctx, "buildcache"); err != nil {
			return summary, err
		}
	}

	if !opts.DryRun {
		after, aerr := r.systemDiskUsage(ctx)
		if aerr == nil {
			if opts.BuildCache && after.buildCacheCount < before.buildCacheCount {
				summary.BuildCache = before.buildCacheCount - after.buildCacheCount
			}
			if after.totalReclaimable < before.totalReclaimable {
				summary.ReclaimedBytes = before.totalReclaimable - after.totalReclaimable
			}
		}
	}
	return summary, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -count=1`
Expected: PASS (all packages, including the three updated mock runtimes)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go internal/runtime/runtime.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Manager.Prune for docker housekeeping"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`

**Interfaces:**
- Consumes: `runtime.NewDocker() (Manager, error)`, `Manager.Prune(ctx, opts) (PruneSummary, error)`, `runtime.PruneOptions`, `runtime.PruneSummary`, `runtime.DefaultPruneOptions()`
- Produces: `cleanupCmd *cobra.Command` (registered on `rootCmd` via its own `init()`), `cleanupOptsFromFlags(cmd *cobra.Command) runtime.PruneOptions`, `confirmCleanup(r io.Reader) bool`, `printCleanupSummary(w io.Writer, s runtime.PruneSummary, dryRun bool)`, `humanBytes(b int64) string`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCmdRegistered(t *testing.T) {
	c, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil || c.Name() != "cleanup" {
		t.Fatalf("cleanup command not found: cmd=%v err=%v", c, err)
	}
}

func TestCleanupFlagDefaults(t *testing.T) {
	tests := []struct {
		flag string
		want bool
	}{
		{"containers", true},
		{"images", true},
		{"volumes", false},
		{"networks", true},
		{"cache", true},
		{"dry-run", false},
		{"force", false},
	}
	for _, tt := range tests {
		got, err := cleanupCmd.Flags().GetBool(tt.flag)
		if err != nil {
			t.Fatalf("GetBool(%q): %v", tt.flag, err)
		}
		if got != tt.want {
			t.Errorf("flag %q default = %v, want %v", tt.flag, got, tt.want)
		}
	}
}

func TestCleanupOptsFromFlags(t *testing.T) {
	opts := cleanupOptsFromFlags(cleanupCmd)
	if !opts.Containers || !opts.Images || opts.Volumes || !opts.Networks || !opts.BuildCache || opts.DryRun {
		t.Errorf("unexpected defaults: %+v", opts)
	}

	cleanupCmd.Flags().Set("containers", "false")
	cleanupCmd.Flags().Set("volumes", "true")
	cleanupCmd.Flags().Set("cache", "false")
	cleanupCmd.Flags().Set("dry-run", "true")
	opts = cleanupOptsFromFlags(cleanupCmd)
	if opts.Containers {
		t.Error("opts.Containers = true, want false")
	}
	if !opts.Volumes {
		t.Error("opts.Volumes = false, want true")
	}
	if opts.BuildCache {
		t.Error("opts.BuildCache = true, want false")
	}
	if !opts.DryRun {
		t.Error("opts.DryRun = false, want true")
	}

	cleanupCmd.Flags().Set("containers", "true")
	cleanupCmd.Flags().Set("volumes", "false")
	cleanupCmd.Flags().Set("cache", "true")
	cleanupCmd.Flags().Set("dry-run", "false")
}

func TestConfirmCleanup(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := confirmCleanup(strings.NewReader(tt.input)); got != tt.want {
			t.Errorf("confirmCleanup(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestPrintCleanupSummary(t *testing.T) {
	s := runtime.PruneSummary{Containers: 3, Images: 12, Volumes: 0, Networks: 1, BuildCache: 284, ReclaimedBytes: 1610612736}

	var buf bytes.Buffer
	printCleanupSummary(&buf, s, false)
	out := buf.String()
	for _, want := range []string{"containers:  3", "images:      12", "build cache: 284", "reclaimed 1.5 GiB"} {
		if !strings.Contains(out, want) {
			t.Errorf("real-run output missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "would remove") {
		t.Error("dry-run wording present in real run")
	}

	buf.Reset()
	printCleanupSummary(&buf, s, true)
	out = buf.String()
	if !strings.Contains(out, "would remove") {
		t.Errorf("dry-run wording missing in:\n%s", out)
	}
	if strings.Contains(out, "reclaimed") {
		t.Error("reclaimed line present in dry run")
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1610612736, "1.5 GiB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.in); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`
Expected: FAIL — `undefined: cleanupCmd`

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func init() {
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", true, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", true, "remove dangling (untagged) images")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused Docker volumes")
	cleanupCmd.Flags().Bool("networks", true, "remove unused Docker networks")
	cleanupCmd.Flags().Bool("cache", true, "remove Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")
	cleanupCmd.Flags().Duration("interval", 0, "run cleanup repeatedly at this interval (e.g. 24h)")
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources to reclaim disk space",
	Long: `Removes stopped containers not managed by Tengiz, dangling images, unused
networks, and the Docker build cache. Tengiz-managed containers (those labeled
tengiz-app=*) and tagged deployment images are always protected.

Use --dry-run to preview what would be removed, --volumes to also prune unused
volumes, and --interval to run cleanup periodically (e.g. tengiz cleanup --interval 24h).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptsFromFlags(cmd)
		force, _ := cmd.Flags().GetBool("force")
		interval, _ := cmd.Flags().GetDuration("interval")

		if !opts.DryRun && !force {
			if !confirmCleanup(os.Stdin) {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		run := func() error {
			summary, err := rt.Prune(cmd.Context(), opts)
			if err != nil {
				return err
			}
			printCleanupSummary(os.Stdout, summary, opts.DryRun)
			return nil
		}

		if err := run(); err != nil {
			return err
		}

		if interval > 0 {
			fmt.Printf("[tengiz] running cleanup every %s (Ctrl+C to stop)\n", interval)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-cmd.Context().Done():
					return nil
				case <-ticker.C:
					if err := run(); err != nil {
						return err
					}
				}
			}
		}
		return nil
	},
}

func cleanupOptsFromFlags(cmd *cobra.Command) runtime.PruneOptions {
	opts := runtime.DefaultPruneOptions()
	if cmd.Flags().Changed("containers") {
		opts.Containers, _ = cmd.Flags().GetBool("containers")
	}
	if cmd.Flags().Changed("images") {
		opts.Images, _ = cmd.Flags().GetBool("images")
	}
	if cmd.Flags().Changed("volumes") {
		opts.Volumes, _ = cmd.Flags().GetBool("volumes")
	}
	if cmd.Flags().Changed("networks") {
		opts.Networks, _ = cmd.Flags().GetBool("networks")
	}
	if cmd.Flags().Changed("cache") {
		opts.BuildCache, _ = cmd.Flags().GetBool("cache")
	}
	opts.DryRun, _ = cmd.Flags().GetBool("dry-run")
	return opts
}

func confirmCleanup(r io.Reader) bool {
	fmt.Print("[tengiz] Remove unused containers, images, networks, and build cache? [y/N] ")
	line, _ := bufio.NewReader(r).ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(line), "y")
}

func printCleanupSummary(w io.Writer, s runtime.PruneSummary, dryRun bool) {
	verb := "removed"
	if dryRun {
		verb = "would remove"
	}
	fmt.Fprintf(w, "[tengiz] cleanup (%s):\n", verb)
	fmt.Fprintf(w, "  containers:  %d\n", s.Containers)
	fmt.Fprintf(w, "  images:      %d\n", s.Images)
	fmt.Fprintf(w, "  volumes:     %d\n", s.Volumes)
	fmt.Fprintf(w, "  networks:    %d\n", s.Networks)
	fmt.Fprintf(w, "  build cache: %d\n", s.BuildCache)
	if !dryRun && s.ReclaimedBytes > 0 {
		fmt.Fprintf(w, "[tengiz] reclaimed %s\n", humanBytes(s.ReclaimedBytes))
	}
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`
Expected: PASS (all 6 tests)

- [ ] **Step 5: Build + vet + full test suite, then commit**

Run: `go build -o tengiz . && go vet ./... && go test ./... -count=1`
Expected: PASS

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Manual smoke-test the command (requires Docker)

**Files:**
- No code changes. Verify end-to-end behavior on a host with Docker.

**Interfaces:**
- Consumes: `tengiz cleanup` from Task 3 and `runtime.Prune` from Task 2

- [ ] **Step 1: Verify protection of Tengiz containers**

Run: `docker ps -aq --filter status=exited` and confirm any stopped Tengiz containers exist (or start a dummy one with `docker run -d --label tengiz-app=keepme alpine sleep 10000` then `docker stop` it).
Expected: containers labeled `tengiz-app=keepme` are listed.

- [ ] **Step 2: Verify a non-Tengiz stopped container is a candidate**

Run: `docker run -d --name leftover-test alpine sleep 10000 && docker stop leftover-test`
Expected: container `leftover-test` is created and stopped (it has no `tengiz-app` label).

- [ ] **Step 3: Dry-run must report the leftover but not remove anything**

Run: `./tengiz cleanup --dry-run --force`
Expected: output shows `containers:  1` (the leftover) and no removal occurred — `docker ps -a --filter name=leftover-test` still lists it.

- [ ] **Step 4: Real run removes only the leftover**

Run: `./tengiz cleanup --force`
Expected: output shows `containers:  1` was removed; `docker ps -a --filter name=leftover-test` is empty; `docker ps -a --filter label=tengiz-app=keepme` still lists the Tengiz container.

- [ ] **Step 5: Clean up the scratch container**

Run: `docker rm -f keepme 2>/dev/null; docker rm -f leftover-test 2>/dev/null; true`
Expected: no error, scratch containers removed.

- [ ] **Step 6: Commit (if any incidental changes were made)**

```bash
git status
```

Expected: clean working tree (no code changes in this task).

---

### Task 5: Documentation updates

**Files:**
- Modify: `README.md` — add `tengiz cleanup` section after `### tengiz rm <app>` (after line 228, before `### tengiz rollback <app>`)
- Modify: `AGENTS.md` — add cleanup line to Commands block
- Modify: `docs/FUTURES_FEATURES.md` — mark #6 implemented

**Interfaces:**
- Consumes: the final CLI surface from Task 3

- [ ] **Step 1: Add CLI reference to README.md**

Insert after the `tengiz rm <app>` section (line 228) and before `### tengiz rollback <app>`:

```markdown
### `tengiz cleanup`

Clean up unused Docker resources to reclaim disk space. Tengiz-managed containers (labeled `tengiz-app=*`) and tagged deployment images are always protected.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz (default: enabled) |
| `--images` | Remove dangling (untagged) images (default: enabled) |
| `--volumes` | Remove unused Docker volumes (default: disabled) |
| `--networks` | Remove unused Docker networks (default: enabled) |
| `--cache` | Remove Docker build cache (default: enabled) |
| `--dry-run` | Show what would be removed without removing anything |
| `-f`, `--force` | Skip the confirmation prompt |
| `--interval <duration>` | Run cleanup repeatedly at this interval (e.g. `24h`) |

With `--interval`, cleanup runs once immediately, then repeats every interval until interrupted.
```

- [ ] **Step 2: Add the command to AGENTS.md**

In the Commands code block, after the `tengiz stop/start/rm  → lifecycle` line:

```
tengiz cleanup [--dry-run] [--volumes] [--force] → prune unused containers/images/networks/build cache
```

- [ ] **Step 3: Mark feature #6 implemented in FUTURES_FEATURES.md**

In the P0 Priority Ranking table, change row #6 so the `#` column reads `**Docker Housekeeping** ✅` instead of `**Docker Housekeeping** ⬜`.

In the `## Docker Housekeeping (Otomatik Temizlik)` feature section, add after the `- **Why add to Tengiz:** ...` line:

```markdown
- **Status:** ✅ Implemented (2026-08-03)
```

In the `### ✅ Implemented Features (Not Pending)` table, add a row:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-03) |
```

- [ ] **Step 4: Verify build and tests still pass**

Run: `go build -o tengiz . && go vet ./... && go test ./... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**Spec coverage:**
- "Disk space is the #1 production issue" → whole feature addresses this; `PruneSummary.ReclaimedBytes` gives concrete reclaim feedback (Task 2, Task 3)
- "Label-based `docker system prune`" → Task 2 label-based protection via `{{.Label "tengiz-app"}}` filtering + per-ID `docker rm`; prune commands for the rest
- "`tengiz cleanup` command" → Task 3
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → Task 2 (volumes/networks/containers/images) + `--interval` periodic mode (Task 3)
- "Tengiz yönetimindeki container'lar korunur" → `parseContainerCandidates` excludes any container whose `tengiz-app` label is non-empty (includes scale-to-zero stopped apps and preview deployments)

**Placeholder scan:** No TBD/TODO/placeholder steps — every step has concrete code or commands with expected output.

**Type consistency:** `PruneOptions`/`PruneSummary`/`DefaultPruneOptions`/`candidateQueryArgs`/`pruneCommandArgs`/`parseContainerCandidates`/`parseSystemDF`/`parseDiskSize`/`systemDiskStats`/`nonEmptyLines` are all defined in Task 1 and referenced identically in Tasks 2-3. `Manager.Prune(ctx, opts) (PruneSummary, error)` is defined in Task 2 and consumed in Task 3. Field names (`Containers`, `Images`, `Volumes`, `Networks`, `BuildCache`, `DryRun`, `ReclaimedBytes`) are identical everywhere. The three mock runtimes all get the identical `Prune` stub method.

**Notable design decisions (why, for the implementer):**
- `docker ps` rejects `label!=tengiz-app` ("invalid filter 'label!'") — only prune commands support negated label filters, and `docker container prune` cannot safely exclude labeled containers. Hence the `{{.Label "tengiz-app"}}` format-template + Go-side filtering + per-ID `docker rm -f`. Verified against docker/cli docs and issue docker/cli#6021.
- Volumes default to disabled because a named volume temporarily unreferenced by a container is still data; pruning is opt-in.
- `--dry-run` reports counts only (no reclaimed-bytes estimate) to avoid overstating what would actually be freed.
- Build-cache dry-run uses `TotalCount - Active` from `docker system df` as an approximation of unused entries.
