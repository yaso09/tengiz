# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command and a periodic cleanup job (DockerCleanupJob equivalent) that prune unused Docker containers, images, volumes, and networks — protected by label-based filtering so Tengiz-managed containers are never removed.

**Architecture:** Extend the `runtime.Manager` interface with `Prune()` and `SystemDF()`, implemented on `dockerRuntime` by shelling out to `docker container/image/volume/network prune --force --filter label!=tengiz-app`. All parsing (deleted counts, reclaimed space) lives in pure, table-testable helpers. A new `internal/cleanup` package provides a periodic `Job` (default 24h) wired into the long-running webhook server. The `tengiz cleanup` CLI command exposes one-shot + dry-run pruning. No Docker SDK — the runtime keeps calling the `docker` CLI via `os/exec` per existing convention.

**Tech Stack:** Go 1.26, Cobra, Docker CLI via `os/exec` (no SDK). No new external dependencies — stdlib only.

## Global Constraints

- No new external dependencies (stdlib only)
- Every prune command MUST include `--force` (non-interactive; otherwise the CLI blocks on the y/n prompt)
- Every prune command MUST include `--filter label!=tengiz-app` so Tengiz-managed containers (running, idle-stopped, versioned, preview) are protected — this is the core safety requirement of the feature
- `--all` only affects image and volume pruning (`docker image prune --all`, `docker volume prune --all`); container/network prune have no `--all`
- `--dry-run` must never delete anything — it prints the planned commands and `docker system df` output
- Default behavior (no category flag) prunes all four categories
- Adding two methods to the `Manager` interface requires updating `stubManager` AND the three test mocks in one task, or the repo won't compile
- Docker-exec methods are not unit-testable without a daemon; all testable logic is isolated in pure helpers with table-driven tests (mirroring the existing `buildLogArgs`/`TestLogOptionsBuildArgs` pattern)
- Existing tests must continue to pass unchanged
- Default periodic interval is 24h; interval `<= 0` disables periodic cleanup and falls back to 24h when a job is constructed
- Follow AGENTS.md: create a `feat/` branch, add/update tests, run tests, then commit per task
- PR size/quality rule: a task is the smallest unit worth a fresh reviewer gate — each task below ends with independently testable deliverables

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`/`PruneReport` types; add `Prune()` + `SystemDF()` to `Manager`; implement on `stubManager` |
| `internal/runtime/cleanup.go` | `dockerRuntime.Prune()`/`SystemDF()`; pure helpers `buildPruneArgs`, `PruneCommandString`, `parsePruneOutput` |
| `internal/runtime/size.go` | Pure size helpers `parseSize`, `formatBytes`, `sumReclaimed` |
| `internal/runtime/cleanup_test.go` | Tests for prune args, command string, output parser, stub conformance |
| `internal/runtime/size_test.go` | Table tests for size helpers |
| `internal/cleanup/job.go` | New package. Periodic `Job` (DockerCleanupJob) |
| `internal/cleanup/job_test.go` | Job interval/run tests with a counting mock runtime |
| `internal/cli/cleanup.go` | New file. `tengiz cleanup` command + testable `cleanupCommandRun(cmd, rt)` helper |
| `internal/cli/cleanup_test.go` | CLI tests: registration, flags, default-category, category flags, `--all`, `--dry-run` |
| `internal/cli/root.go` | Register `cleanupCmd` + flags; wire periodic cleanup into `webhookCmd` via `--cleanup-interval` |
| `internal/cli/root_test.go` | Update `mockRTForDeploy` with new methods; add `TestWebhookCmdCleanupIntervalFlag` |
| `internal/proxy/proxy_test.go` | Update `mockRuntime` with new methods |
| `internal/idle/idle_test.go` | Update `mockRuntime` with new methods |
| `README.md` | Document `tengiz cleanup` command |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Extend the `runtime.Manager` interface with `Prune` and `SystemDF`

**Files:**
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/runtime/cleanup_test.go`
- Modify: `internal/proxy/proxy_test.go`
- Modify: `internal/idle/idle_test.go`
- Modify: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{Containers, Images, Volumes, Networks, All bool}`, `runtime.PruneReport{Containers, Images, Volumes, Networks int, Total string}`, `runtime.Manager.Prune(ctx, opts) (PruneReport, error)`, `runtime.Manager.SystemDF(ctx) (string, error)`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Add the types and interface methods (write the failing test)**

First add the new test to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.Total != "" {
		t.Errorf("Total = %q, want empty", report.Total)
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

Now edit `internal/runtime/runtime.go`. Add the two types immediately after the existing `RunOptions` struct (around line 29):

```go
type RunOptions struct {
	Interactive bool
	ExtraEnv    map[string]string
}

type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	All        bool
}

type PruneReport struct {
	Containers int
	Images     int
	Volumes    int
	Networks   int
	Total      string
}
```

Add two methods to the `Manager` interface, immediately after `KeepLastNImages`:

```go
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
	SystemDF(ctx context.Context) (string, error)
```

Add the stub implementations, immediately after the `stubManager.KeepLastNImages` method:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}

func (m *stubManager) SystemDF(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 3: Run the test to verify it fails to compile**

Run: `go test ./internal/runtime/... -run 'TestStubPrune|TestStubSystemDF' -count=1`
Expected: FAIL — undefined `PruneOptions` (and compile errors in `proxy`/`idle`/`cli` test packages still lacking the new methods).

- [ ] **Step 4: Update the three test mocks**

In `internal/proxy/proxy_test.go`, after the `mockRuntime.KeepLastNImages` method (line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}

func (m *mockRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

In `internal/idle/idle_test.go`, after the `mockRuntime.KeepLastNImages` method (line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}

func (m *mockRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

In `internal/cli/root_test.go`, after the `mockRTForDeploy.KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}

func (m *mockRTForDeploy) SystemDF(ctx context.Context) (string, error) { return "", nil }
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/runtime/... ./internal/proxy/... ./internal/idle/... ./internal/cli/... -count=1`
Expected: PASS (all packages compile and tests pass).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: add Prune and SystemDF to runtime.Manager interface"
```

---

### Task 2: Prune argument builder and output parser

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport` (Task 1)
- Produces: `buildPruneArgs(category string, all bool) []string`, `PruneCommandString(category string, all bool) string`, `parsePruneOutput(category, output string) (int, string)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		category string
		all      bool
		expected []string
	}{
		{
			name:     "container default",
			category: "container",
			all:      false,
			expected: []string{"container", "prune", "--force", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "image default",
			category: "image",
			all:      false,
			expected: []string{"image", "prune", "--force", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "image all",
			category: "image",
			all:      true,
			expected: []string{"image", "prune", "--force", "--filter", "label!=tengiz-app", "--all"},
		},
		{
			name:     "volume default",
			category: "volume",
			all:      false,
			expected: []string{"volume", "prune", "--force", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "volume all",
			category: "volume",
			all:      true,
			expected: []string{"volume", "prune", "--force", "--filter", "label!=tengiz-app", "--all"},
		},
		{
			name:     "network default",
			category: "network",
			all:      false,
			expected: []string{"network", "prune", "--force", "--filter", "label!=tengiz-app"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneArgs(tt.category, tt.all)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildPruneArgs(%q, %v) = %v, want %v", tt.category, tt.all, got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("buildPruneArgs(%q, %v)[%d] = %q, want %q", tt.category, tt.all, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestPruneCommandString(t *testing.T) {
	got := PruneCommandString("container", false)
	want := "container prune --force --filter label!=tengiz-app"
	if got != want {
		t.Errorf("PruneCommandString() = %q, want %q", got, want)
	}
}

func TestParsePruneOutput(t *testing.T) {
	const containerOutput = `Deleted Containers:
abc123
def456

Total reclaimed space: 12.5MB
`
	n, reclaimed := parsePruneOutput("container", containerOutput)
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if reclaimed != "12.5MB" {
		t.Errorf("reclaimed = %q, want 12.5MB", reclaimed)
	}

	const imageOutput = `Deleted Images:
untagged: tengiz-apps/foo:production-123
deleted: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
deleted: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb

Total reclaimed space: 234.5MB
`
	n, reclaimed = parsePruneOutput("image", imageOutput)
	if n != 2 {
		t.Errorf("image deleted = %d, want 2", n)
	}
	if reclaimed != "234.5MB" {
		t.Errorf("image reclaimed = %q, want 234.5MB", reclaimed)
	}

	const emptyOutput = "Total reclaimed space: 0B\n"
	n, reclaimed = parsePruneOutput("container", emptyOutput)
	if n != 0 {
		t.Errorf("empty deleted = %d, want 0", n)
	}
	if reclaimed != "0B" {
		t.Errorf("empty reclaimed = %q, want 0B", reclaimed)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/runtime/... -run 'TestBuildPruneArgs|TestPruneCommandString|TestParsePruneOutput' -count=1`
Expected: FAIL — `buildPruneArgs`, `PruneCommandString`, `parsePruneOutput` undefined.

- [ ] **Step 3: Write the minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
// buildPruneArgs returns the docker subcommand args for a single prune
// category. The label filter protects Tengiz-managed containers, and --force
// keeps the command non-interactive.
func buildPruneArgs(category string, all bool) []string {
	args := []string{category, "prune", "--force", "--filter", "label!=tengiz-app"}
	if all && (category == "image" || category == "volume") {
		args = append(args, "--all")
	}
	return args
}

// PruneCommandString returns the human-readable docker command for a prune
// category (used by `tengiz cleanup --dry-run`).
func PruneCommandString(category string, all bool) string {
	return strings.Join(buildPruneArgs(category, all), " ")
}

// parsePruneOutput extracts the deleted-item count and reclaimed-space string
// from a single `docker <category> prune` command's output.
func parsePruneOutput(category, output string) (int, string) {
	lines := strings.Split(output, "\n")
	deleted := 0
	reclaimed := ""
	inSection := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "Deleted") && strings.HasSuffix(line, ":"):
			inSection = true
		case strings.HasPrefix(line, "Total reclaimed space:"):
			reclaimed = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			inSection = false
		case inSection && line != "":
			if category == "image" {
				if strings.HasPrefix(line, "deleted:") {
					deleted++
				}
			} else {
				deleted++
			}
		}
	}
	return deleted, reclaimed
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/runtime/... -run 'TestBuildPruneArgs|TestPruneCommandString|TestParsePruneOutput' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add prune arg builder and output parser"
```

---

### Task 3: Size parsing, formatting, and summing helpers

**Files:**
- Create: `internal/runtime/size.go`
- Create: `internal/runtime/size_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `parseSize(s string) (int64, error)`, `formatBytes(n int64) string`, `sumReclaimed(values []string) (string, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/size_test.go`:

```go
package runtime

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		expected int64
		wantErr  bool
	}{
		{"empty string", "", 0, true},
		{"zero bytes", "0B", 0, false},
		{"bytes", "512B", 512, false},
		{"lowercase kb", "34.56kB", int64(34.56 * 1024), false},
		{"uppercase kb", "2KB", 2048, false},
		{"mb", "10.8MB", int64(10.8 * 1024 * 1024), false},
		{"gb", "1.2GB", int64(1.2 * 1024 * 1024 * 1024), false},
		{"tb", "1TB", int64(1024 * 1024 * 1024 * 1024), false},
		{"whitespace", " 12MB ", int64(12 * 1024 * 1024), false},
		{"garbage", "abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSize(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSize(%q) = %d, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSize(%q) error = %v", tt.in, err)
			}
			if got != tt.expected {
				t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.expected)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		want string
	}{
		{"zero", 0, "0B"},
		{"negative clamped", -5, "0B"},
		{"bytes", 512, "512B"},
		{"exact kb", 1024, "1.0KB"},
		{"kb", 2048, "2.0KB"},
		{"mb", int64(10.8 * 1024 * 1024), "10.8MB"},
		{"gb", int64(1.2 * 1024 * 1024 * 1024), "1.2GB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatBytes(tt.in); got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSumReclaimed(t *testing.T) {
	tests := []struct {
		name     string
		in       []string
		expected string
		wantErr  bool
	}{
		{"empty list", nil, "0B", false},
		{"zero entries", []string{"0B", "0B"}, "0B", false},
		{"skips empty", []string{"10.8MB", ""}, "10.8MB", false},
		{"sums", []string{"10.8MB", "1.2GB"}, "1.2GB", false},
		{"invalid", []string{"nope"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sumReclaimed(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("sumReclaimed(%v) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("sumReclaimed(%v) error = %v", tt.in, err)
			}
			if got != tt.expected {
				t.Errorf("sumReclaimed(%v) = %q, want %q", tt.in, got, tt.expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/runtime/... -run 'TestParseSize|TestFormatBytes|TestSumReclaimed' -count=1`
Expected: FAIL — `parseSize`, `formatBytes`, `sumReclaimed` undefined.

- [ ] **Step 3: Write the minimal implementation**

Create `internal/runtime/size.go`:

```go
package runtime

import (
	"fmt"
	"strconv"
	"strings"
)

// parseSize converts a Docker human-readable size string ("10.8MB", "1.2GB",
// "34.56kB", "0B", "512B") to a byte count.
func parseSize(s string) (int64, error) {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "" {
		return 0, fmt.Errorf("empty size")
	}
	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"tb", 1 << 40},
		{"gb", 1 << 30},
		{"mb", 1 << 20},
		{"kb", 1 << 10},
		{"b", 1},
	}
	for _, m := range multipliers {
		if strings.HasSuffix(lower, m.suffix) {
			numPart := strings.TrimSpace(strings.TrimSuffix(lower, m.suffix))
			f, err := strconv.ParseFloat(numPart, 64)
			if err != nil {
				return 0, err
			}
			return int64(f * float64(m.mult)), nil
		}
	}
	f, err := strconv.ParseFloat(lower, 64)
	if err != nil {
		return 0, err
	}
	return int64(f), nil
}

// formatBytes converts a byte count to a human-readable size string.
func formatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	units := []struct {
		div    int64
		suffix string
	}{
		{1 << 40, "TB"},
		{1 << 30, "GB"},
		{1 << 20, "MB"},
		{1 << 10, "KB"},
		{1, "B"},
	}
	for _, u := range units {
		if n >= u.div {
			if u.suffix == "B" {
				return fmt.Sprintf("%dB", n)
			}
			return fmt.Sprintf("%.1f%s", float64(n)/float64(u.div), u.suffix)
		}
	}
	return "0B"
}

// sumReclaimed sums a set of Docker reclaimed-space strings into one
// human-readable total. Empty and "0B" entries are skipped.
func sumReclaimed(values []string) (string, error) {
	var total int64
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || trimmed == "0B" {
			continue
		}
		n, err := parseSize(trimmed)
		if err != nil {
			return "", fmt.Errorf("parse size %q: %w", v, err)
		}
		total += n
	}
	return formatBytes(total), nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/runtime/... -run 'TestParseSize|TestFormatBytes|TestSumReclaimed' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/size.go internal/runtime/size_test.go
git commit -m "feat: add docker size parsing helpers"
```

---

### Task 4: Implement `dockerRuntime.Prune` and `SystemDF`

**Files:**
- Modify: `internal/runtime/cleanup.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport` (Task 1), `buildPruneArgs`/`parsePruneOutput` (Task 2), `sumReclaimed` (Task 3)
- Produces: `(*dockerRuntime).Prune(ctx, opts) (PruneReport, error)`, `(*dockerRuntime).SystemDF(ctx) (string, error)`

- [ ] **Step 1: Write the minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
// SystemDF returns the raw `docker system df` table, used by
// `tengiz cleanup --dry-run` to show current disk usage.
func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w", err)
	}
	return string(out), nil
}

// Prune runs the enabled prune categories and aggregates a report.
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport
	var reclaimed []string

	pruneCategory := func(category string, enabled bool) error {
		if !enabled {
			return nil
		}
		args := buildPruneArgs(category, opts.All)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("docker %s prune: %w\n%s", category, err, string(out))
		}
		n, space := parsePruneOutput(category, string(out))
		reclaimed = append(reclaimed, space)
		switch category {
		case "container":
			report.Containers = n
		case "image":
			report.Images = n
		case "volume":
			report.Volumes = n
		case "network":
			report.Networks = n
		}
		return nil
	}

	for _, cat := range []struct {
		name    string
		enabled bool
	}{
		{"container", opts.Containers},
		{"image", opts.Images},
		{"volume", opts.Volumes},
		{"network", opts.Networks},
	} {
		if err := pruneCategory(cat.name, cat.enabled); err != nil {
			return report, err
		}
	}

	total, sumErr := sumReclaimed(reclaimed)
	if sumErr != nil {
		total = ""
	}
	report.Total = total
	return report, nil
}
```

- [ ] **Step 2: Verify build and tests**

Run: `go build ./... && go vet ./... && go test ./internal/runtime/... -count=1`
Expected: PASS.

- [ ] **Step 3: Manual smoke test (requires a Docker daemon — skip if unavailable)**

Run: `go run . cleanup --dry-run`
Expected: prints `[tengiz] dry run - no resources will be deleted`, the four `docker <category> prune ...` commands, `[tengiz] current disk usage:`, and the `docker system df` table.

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/cleanup.go
git commit -m "feat: implement dockerRuntime Prune and SystemDF"
```

---

### Task 5: Periodic cleanup job (`internal/cleanup`)

**Files:**
- Create: `internal/cleanup/job.go`
- Create: `internal/cleanup/job_test.go`

**Interfaces:**
- Consumes: `runtime.Manager`, `runtime.PruneOptions`, `runtime.PruneReport`
- Produces: `cleanup.NewJob(rt runtime.Manager, opts runtime.PruneOptions, interval time.Duration) *cleanup.Job`, `(*cleanup.Job).Interval() time.Duration`, `(*cleanup.Job).RunOnce(ctx) (runtime.PruneReport, error)`, `(*cleanup.Job).Run(ctx)`

- [ ] **Step 1: Write the failing test**

Create `internal/cleanup/job_test.go`:

```go
package cleanup

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type countingRuntime struct {
	prunes atomic.Int32
}

func (m *countingRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	m.prunes.Add(1)
	return runtime.PruneReport{Total: "0B"}, nil
}

func (m *countingRuntime) SystemDF(ctx context.Context) (string, error) { return "", nil }
func (m *countingRuntime) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	return nil
}
func (m *countingRuntime) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	return nil
}
func (m *countingRuntime) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error {
	return nil
}
func (m *countingRuntime) RemoveImage(ctx context.Context, imageTag string) error { return nil }
func (m *countingRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	return nil
}
func (m *countingRuntime) Start(ctx context.Context, name string) error { return nil }
func (m *countingRuntime) Stop(ctx context.Context, name string) error { return nil }
func (m *countingRuntime) Restart(ctx context.Context, name string) error { return nil }
func (m *countingRuntime) Remove(ctx context.Context, name string) error { return nil }
func (m *countingRuntime) RemoveBySuffix(ctx context.Context, name string, suffix string) error {
	return nil
}
func (m *countingRuntime) IsActive(ctx context.Context, name string) (bool, error) { return false, nil }
func (m *countingRuntime) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) {
	return 0, nil
}
func (m *countingRuntime) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *countingRuntime) Logs(ctx context.Context, name string, opts runtime.LogOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *countingRuntime) WaitForReady(ctx context.Context, name string, internalPort int) error {
	return nil
}
func (m *countingRuntime) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error {
	return nil
}
func (m *countingRuntime) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts runtime.RunOptions) error {
	return nil
}

func TestCountingRuntimeImplementsManager(t *testing.T) {
	var m runtime.Manager = &countingRuntime{}
	if m == nil {
		t.Fatal("countingRuntime does not implement Manager")
	}
}

func TestNewJobDefaultInterval(t *testing.T) {
	job := NewJob(runtime.NewStub(), runtime.PruneOptions{}, 0)
	if job.Interval() != defaultInterval {
		t.Errorf("default interval = %v, want %v", job.Interval(), defaultInterval)
	}
}

func TestJobRunOnce(t *testing.T) {
	rt := &countingRuntime{}
	job := NewJob(rt, runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true}, time.Hour)
	report, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if report.Total != "0B" {
		t.Errorf("Total = %q, want 0B", report.Total)
	}
	if rt.prunes.Load() != 1 {
		t.Errorf("Prune called %d times, want 1", rt.prunes.Load())
	}
}

func TestJobRunsPeriodically(t *testing.T) {
	rt := &countingRuntime{}
	job := NewJob(rt, runtime.PruneOptions{Containers: true}, 30*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		job.Run(ctx)
		close(done)
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	if got := rt.prunes.Load(); got < 2 {
		t.Errorf("Prune called %d times, want >= 2 (initial + interval runs)", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cleanup/... -count=1`
Expected: FAIL — package `cleanup` has no Go files to build.

- [ ] **Step 3: Write the minimal implementation**

Create `internal/cleanup/job.go`:

```go
package cleanup

import (
	"context"
	"log"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
)

const defaultInterval = 24 * time.Hour

// Job periodically prunes unused Docker resources, mirroring DockerCleanupJob.
type Job struct {
	runtime  runtime.Manager
	opts     runtime.PruneOptions
	interval time.Duration
}

// NewJob creates a periodic cleanup job. An interval of 0 defaults to 24h.
func NewJob(rt runtime.Manager, opts runtime.PruneOptions, interval time.Duration) *Job {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Job{runtime: rt, opts: opts, interval: interval}
}

// Interval returns the configured run interval.
func (j *Job) Interval() time.Duration {
	return j.interval
}

// RunOnce performs a single cleanup pass and returns the report.
func (j *Job) RunOnce(ctx context.Context) (runtime.PruneReport, error) {
	return j.runtime.Prune(ctx, j.opts)
}

// Run prunes immediately, then re-runs every interval until ctx is cancelled.
func (j *Job) Run(ctx context.Context) {
	j.runOnceAndLog(ctx)
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.runOnceAndLog(ctx)
		}
	}
}

func (j *Job) runOnceAndLog(ctx context.Context) {
	report, err := j.runtime.Prune(ctx, j.opts)
	if err != nil {
		log.Printf("[cleanup] prune failed: %v", err)
		return
	}
	log.Printf("[cleanup] pruned containers=%d images=%d volumes=%d networks=%d reclaimed=%s",
		report.Containers, report.Images, report.Volumes, report.Networks, report.Total)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cleanup/... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/job.go internal/cleanup/job_test.go
git commit -m "feat: add periodic docker cleanup job"
```

---

### Task 6: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.Manager.Prune/SystemDF`, `runtime.PruneCommandString`, `runtime.PruneOptions`, `runtime.PruneReport`
- Produces: `cleanupCmd` (registered as `tengiz cleanup`), `cleanupCommandRun(cmd *cobra.Command, rt runtime.Manager) error`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type cleanupTestRuntime struct {
	opts runtime.PruneOptions
}

func (m *cleanupTestRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	m.opts = opts
	return runtime.PruneReport{Total: "0B"}, nil
}

func (m *cleanupTestRuntime) SystemDF(ctx context.Context) (string, error) { return "TABLE", nil }
func (m *cleanupTestRuntime) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	return nil
}
func (m *cleanupTestRuntime) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	return nil
}
func (m *cleanupTestRuntime) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error {
	return nil
}
func (m *cleanupTestRuntime) RemoveImage(ctx context.Context, imageTag string) error { return nil }
func (m *cleanupTestRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	return nil
}
func (m *cleanupTestRuntime) Start(ctx context.Context, name string) error { return nil }
func (m *cleanupTestRuntime) Stop(ctx context.Context, name string) error { return nil }
func (m *cleanupTestRuntime) Restart(ctx context.Context, name string) error { return nil }
func (m *cleanupTestRuntime) Remove(ctx context.Context, name string) error { return nil }
func (m *cleanupTestRuntime) RemoveBySuffix(ctx context.Context, name string, suffix string) error {
	return nil
}
func (m *cleanupTestRuntime) IsActive(ctx context.Context, name string) (bool, error) { return false, nil }
func (m *cleanupTestRuntime) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) {
	return 0, nil
}
func (m *cleanupTestRuntime) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *cleanupTestRuntime) Logs(ctx context.Context, name string, opts runtime.LogOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *cleanupTestRuntime) WaitForReady(ctx context.Context, name string, internalPort int) error {
	return nil
}
func (m *cleanupTestRuntime) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error {
	return nil
}
func (m *cleanupTestRuntime) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts runtime.RunOptions) error {
	return nil
}

func newCleanupTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.SetContext(context.Background())
	return cmd
}

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
	for _, flag := range []string{"dry-run", "all", "containers", "images", "volumes", "networks"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupCommandRunPrunesAllByDefault(t *testing.T) {
	rt := &cleanupTestRuntime{}
	cmd := newCleanupTestCmd(t)

	output := captureOutput(func() {
		if err := cleanupCommandRun(cmd, rt); err != nil {
			t.Errorf("cleanupCommandRun() error = %v", err)
		}
	})
	for _, want := range []string{
		"containers removed: 0",
		"images removed: 0",
		"volumes removed: 0",
		"networks removed: 0",
		"total reclaimed space: 0B",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %s", want, output)
		}
	}
	if !rt.opts.Containers || !rt.opts.Images || !rt.opts.Volumes || !rt.opts.Networks {
		t.Errorf("expected all four categories enabled, got %+v", rt.opts)
	}
}

func TestCleanupCommandRunHonorsCategoryFlags(t *testing.T) {
	rt := &cleanupTestRuntime{}
	cmd := newCleanupTestCmd(t)
	cmd.Flags().Set("images", "true")

	captureOutput(func() {
		if err := cleanupCommandRun(cmd, rt); err != nil {
			t.Errorf("cleanupCommandRun() error = %v", err)
		}
	})
	if !rt.opts.Images {
		t.Error("expected Images enabled")
	}
	if rt.opts.Containers || rt.opts.Volumes || rt.opts.Networks {
		t.Errorf("expected only Images, got %+v", rt.opts)
	}
}

func TestCleanupCommandRunAllFlag(t *testing.T) {
	rt := &cleanupTestRuntime{}
	cmd := newCleanupTestCmd(t)
	cmd.Flags().Set("all", "true")

	captureOutput(func() {
		if err := cleanupCommandRun(cmd, rt); err != nil {
			t.Errorf("cleanupCommandRun() error = %v", err)
		}
	})
	if !rt.opts.All {
		t.Error("expected All enabled")
	}
}

func TestCleanupCommandRunDryRun(t *testing.T) {
	rt := &cleanupTestRuntime{}
	cmd := newCleanupTestCmd(t)
	cmd.Flags().Set("dry-run", "true")

	output := captureOutput(func() {
		if err := cleanupCommandRun(cmd, rt); err != nil {
			t.Errorf("cleanupCommandRun() error = %v", err)
		}
	})
	for _, want := range []string{
		"dry run",
		"docker container prune",
		"docker image prune",
		"docker volume prune",
		"docker network prune",
		"current disk usage:",
		"TABLE",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %s", want, output)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/... -run 'TestCleanup' -count=1`
Expected: FAIL — `cleanupCmd`, `cleanupCommandRun`, and `cleanupTestRuntime` undefined.

- [ ] **Step 3: Write the minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Remove unused Docker resources (stopped containers, unused images, volumes, and networks).

Tengiz-managed containers are protected via the tengiz-app label filter, so
idle-stopped apps and preview deployments are never removed.

Flags:
  --containers   prune stopped containers
  --images       prune unused images
  --volumes      prune unused volumes
  --networks     prune unused networks
  --all          also remove all unused images (not just dangling) and all unused volumes
  --dry-run      show what would be pruned and current disk usage without deleting

With no category flag, all four categories are pruned.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		return cleanupCommandRun(cmd, rt)
	},
}

func cleanupCommandRun(cmd *cobra.Command, rt runtime.Manager) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")

	if !containers && !images && !volumes && !networks {
		containers, images, volumes, networks = true, true, true, true
	}

	if dryRun {
		usage, err := rt.SystemDF(cmd.Context())
		if err != nil {
			return err
		}
		fmt.Println("[tengiz] dry run - no resources will be deleted")
		fmt.Println("[tengiz] would run (label-filtered to protect tengiz apps):")
		if containers {
			fmt.Printf("  docker %s\n", runtime.PruneCommandString("container", all))
		}
		if images {
			fmt.Printf("  docker %s\n", runtime.PruneCommandString("image", all))
		}
		if volumes {
			fmt.Printf("  docker %s\n", runtime.PruneCommandString("volume", all))
		}
		if networks {
			fmt.Printf("  docker %s\n", runtime.PruneCommandString("network", all))
		}
		fmt.Println("[tengiz] current disk usage:")
		fmt.Print(usage)
		return nil
	}

	opts := runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		All:        all,
	}
	report, err := rt.Prune(cmd.Context(), opts)
	if err != nil {
		return err
	}

	fmt.Println("[tengiz] cleanup complete")
	fmt.Printf("[tengiz] containers removed: %d\n", report.Containers)
	fmt.Printf("[tengiz] images removed: %d\n", report.Images)
	fmt.Printf("[tengiz] volumes removed: %d\n", report.Volumes)
	fmt.Printf("[tengiz] networks removed: %d\n", report.Networks)
	fmt.Printf("[tengiz] total reclaimed space: %s\n", report.Total)
	return nil
}
```

Register the command and its flags in `internal/cli/root.go` `init()`, next to the other `rootCmd.AddCommand` calls:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be pruned without deleting anything")
	cleanupCmd.Flags().Bool("all", false, "remove all unused images and volumes, not just dangling/anonymous")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune unused images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cli/... -run 'TestCleanup' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 7: Wire periodic cleanup into the webhook server

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `cleanup.NewJob`, `runtime.PruneOptions` (Task 5)
- Produces: `webhookCmd --cleanup-interval` flag; a `cleanup.Job` goroutine started when the flag is > 0

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/root_test.go`:

```go
func TestWebhookCmdCleanupIntervalFlag(t *testing.T) {
	flag := webhookCmd.Flags().Lookup("cleanup-interval")
	if flag == nil {
		t.Error("webhookCmd missing --cleanup-interval flag")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/... -run 'TestWebhookCmdCleanupIntervalFlag' -count=1`
Expected: FAIL — flag not found.

- [ ] **Step 3: Write the minimal implementation**

In `internal/cli/root.go`:

Add the import (alphabetically among the other `github.com/yaso09/tengiz/internal/...` imports):

```go
	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/cleanup"
```

In `init()`, next to the other `webhookCmd` flags (line ~86-88):

```go
	webhookCmd.Flags().Duration("cleanup-interval", 0, "run periodic Docker cleanup every interval (e.g. 24h); 0 disables")
```

In `webhookCmd`'s `RunE`, immediately after `defer cancel()` (the line just before `fmt.Printf("[tengiz] starting webhook server on :%d\n", port)`):

```go
		cleanupInterval, _ := cmd.Flags().GetDuration("cleanup-interval")
		if cleanupInterval > 0 {
			cleanupJob := cleanup.NewJob(rt, runtime.PruneOptions{
				Containers: true,
				Images:     true,
				Volumes:    true,
				Networks:   true,
			}, cleanupInterval)
			go cleanupJob.Run(ctx)
		}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go build ./... && go vet ./... && go test ./internal/cli/... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: run periodic docker cleanup from webhook server"
```

---

### Task 8: Documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the CLI surface produced in Tasks 6-7
- Produces: user-facing documentation for `tengiz cleanup`

- [ ] **Step 1: Document the command in README.md**

Insert after the `### tengiz rollback <app>` section (after line 237, before `### tengiz domain`):

```markdown
### `tengiz cleanup`

Clean up unused Docker resources to reclaim disk space. Removes stopped containers, unused images, unused volumes, and unused networks. Tengiz-managed containers (including idle-stopped apps and preview deployments) are protected via the `tengiz-app` label filter and are never removed.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers |
| `--images` | Prune unused images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--all` | Also remove all unused images (not just dangling) and all unused volumes |
| `--dry-run` | Show what would be pruned and current disk usage without deleting anything |

With no category flag, all four categories are pruned:

```bash
tengiz cleanup                     # prune all four categories
tengiz cleanup --images --volumes  # prune only images and volumes
tengiz cleanup --dry-run           # preview without deleting
```

The webhook server can also run this automatically on an interval with `tengiz webhook --cleanup-interval 24h`.
```

- [ ] **Step 2: Add the command to AGENTS.md**

In `AGENTS.md`, after the `tengiz rollback <app>` line, add:

```
tengiz cleanup           → prune unused Docker resources (label-filtered, protects tengiz apps)
```

- [ ] **Step 3: Mark the feature implemented in docs/FUTURES_FEATURES.md**

- In the P0 priority table, change row #6 `| 6 | **Docker Housekeeping** ⬜ |` to `| 6 | **Docker Housekeeping** ✅ |`
- In the `### ✅ Implemented Features (Not Pending)` table, add:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-02) |
```

- In the detailed `## Docker Housekeeping (Otomatik Temizlik)` section, after the `- **Detected:** 2026-07-14` line, add `- **Status:** ✅ Implemented (2026-08-02)`.

- [ ] **Step 4: Full verification**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: PASS — all packages compile and all tests pass.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage** — Feature #6 "Docker Housekeeping": periodic cleanup (`internal/cleanup.Job` + `--cleanup-interval`, Task 5/7), unused volumes/networks/containers/images pruning (Task 4), label-based protection of Tengiz containers (`label!=tengiz-app` in every prune, Task 2), and the `tengiz cleanup` command (Task 6). The `CleanupHelperContainersJob` reference is covered by container prune removing non-Tengiz stopped containers; dedicated helper-container cleanup is out of scope (matches #56, a separate pending feature). No spec gaps.

**2. Placeholder scan** — No TBDs, no "similar to Task N", no vague validation steps. Every code step contains complete, compilable code; every test step contains full test bodies.

**3. Type consistency** — `PruneOptions{Containers, Images, Volumes, Networks, All bool}` and `PruneReport{Containers, Images, Volumes, Networks int, Total string}` are defined once in Task 1 and used identically in Tasks 2, 4, 5, 6, 7. `buildPruneArgs`, `PruneCommandString`, `parsePruneOutput`, `parseSize`, `formatBytes`, `sumReclaimed` signatures are defined in Tasks 2-3 and referenced with the same names/types in Task 4. `cleanup.NewJob`/`Interval`/`RunOnce`/`Run` are consistent between Tasks 5 and 7. Stub and all three test mocks are updated in Task 1 so the repo compiles at every checkpoint.
