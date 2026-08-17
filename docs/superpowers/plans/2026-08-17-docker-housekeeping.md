# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning stopped non-Tengiz containers, dangling images, unused anonymous volumes, unused networks, and the Docker build cache — while always preserving Tengiz-managed resources via label-based filtering.

**Architecture:** A new `runtime.Cleanup(ctx, opts CleanupOptions) (CleanupReport, error)` method on the `runtime.Manager` interface executes Docker prune commands per resource category. Tengiz-managed containers carry the `tengiz-app=<app>` label, so `docker container prune --filter "label!=tengiz-app"` safely skips them (preserving scale-to-zero cold starts and rollback containers). Image retention stays per-app via the existing `KeepLastNImages` (invoked by the CLI from the env-scoped store before pruning), so `--images` only prunes dangling images. The CLI command defaults to all categories when none are specified, and `--dry-run` reports reclaimable space via `docker system df` without pruning. Pure parse/format helpers (`parseHumanSize`, `parsePruneOutput`, `parseSystemDF`, `reportFromDF`, `FormatBytes`) keep the exec layer unit-testable without a Docker daemon.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface + `dockerRuntime` (os/exec), existing `config.Store` for env-scoped per-app image retention. No new external dependencies.

## Global Constraints

- Tengiz-managed containers must NEVER be pruned: `docker container prune` MUST use `--filter "label!=tengiz-app"` (all Tengiz containers are created with `--label tengiz-app=<appname>`, see `internal/runtime/docker.go:98`)
- Image pruning MUST only remove dangling images (`docker image prune -f`, no `-a`) so that tagged `tengiz-apps/<app>:<deploymentID>` images used for rollback are never removed; per-app retention is handled separately by the existing `KeepLastNImages`
- `tengiz cleanup` with NO category flags MUST prune all five categories (containers, images, volumes, networks, cache)
- `--dry-run` MUST NOT execute any prune command — it only runs `docker system df` and prints a report
- Command MUST be env-aware: `--env` scopes the `config.Store` used for per-app image retention (the Docker prune itself is daemon-global)
- Default per-app image retention is 5 (`--keep-images`, default 5), matching the retention used in `internal/cli/root.go:346`
- No new external dependencies — only stdlib (`os/exec`, `errors`, `regexp`, `strconv`, `strings`, `fmt`, `context`) and existing packages
- Existing tests must continue to pass without modification (mock types get one added method)
- README.md CLI Reference must be updated (AGENTS.md: "UI/UX değişikliklerinde README.md ve dokümantasyonu güncelle")
- Execution must happen on a feature branch created before Task 1: `git checkout -b feat/docker-housekeeping`

---

## File Structure

| File | Responsibility | Action |
|------|---------------|--------|
| `internal/runtime/cleanup.go` | `CleanupOptions`/`CleanupReport` types, `Cleanup` on `dockerRuntime` + `stubManager`, `runDocker`, pure helpers (`parseHumanSize`, `parsePruneOutput`, `parseSystemDF`, `reportFromDF`, `FormatBytes`), docker arg builders (`containerPruneArgs`, `imagePruneArgs`, `volumePruneArgs`, `networkPruneArgs`, `builderPruneArgs`, `systemDFArgs`) | Modify |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface | Modify |
| `internal/runtime/cleanup_test.go` | Tests for stub `Cleanup`, `FormatBytes`, `parseHumanSize`, `parsePruneOutput`, `parseSystemDF`, `reportFromDF`, arg builders | Modify |
| `internal/cli/root.go` | New `cleanupCmd`, flags, `runCleanup`, `printCleanupReport` | Modify |
| `internal/cli/root_test.go` | `Cleanup` on `mockRTForDeploy` + new `recordingCleanupRT` + CLI tests | Modify |
| `internal/idle/idle_test.go` | Add `Cleanup` to `mockRuntime` | Modify |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to `mockRuntime` | Modify |
| `README.md` | New `### tengiz cleanup` section in CLI Reference | Modify |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 implemented | Modify |

---

### Task 1: Cleanup types + `Manager` interface method + stub + mock updates

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `CleanupOptions`, `CleanupReport`
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface
- Modify: `internal/runtime/runtime.go:117-119` — add `Cleanup` to `stubManager`
- Modify: `internal/cli/root_test.go:99` — add `Cleanup` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:34` — add `Cleanup` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:35` — add `Cleanup` to `mockRuntime`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `runtime.CleanupOptions{Containers, Images, Volumes, Networks, Cache, DryRun bool}` with method `Any() bool` (true if any category enabled)
  - `runtime.CleanupReport{Containers, Images, Volumes, Networks, CacheItems int; Reclaimed uint64; DryRun bool}`
  - `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)` — later tasks rely on this signature

- [ ] **Step 1: Create a feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.Reclaimed != 0 || report.Containers != 0 || report.Images != 0 {
		t.Errorf("expected empty report, got %+v", report)
	}
	if report.DryRun {
		t.Error("expected DryRun=false for stub")
	}
}

func TestCleanupOptionsAny(t *testing.T) {
	if CleanupOptions{}.Any() {
		t.Error("empty CleanupOptions.Any() = true, want false")
	}
	if !CleanupOptions{Images: true}.Any() {
		t.Error("CleanupOptions{Images:true}.Any() = false, want true")
	}
	if !CleanupOptions{Containers: true, Networks: true}.Any() {
		t.Error("CleanupOptions with two categories.Any() = false, want true")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestCleanupOptionsAny" -v -count=1`

Expected: FAIL with `undefined: CleanupOptions` and `undefined: Cleanup`

- [ ] **Step 4: Add the types to `internal/runtime/cleanup.go`**

Append at the top of `internal/runtime/cleanup.go` (after the `import` block):

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	Cache      bool
	DryRun     bool
}

func (o CleanupOptions) Any() bool {
	return o.Containers || o.Images || o.Volumes || o.Networks || o.Cache
}

type CleanupReport struct {
	Containers int
	Images     int
	Volumes    int
	Networks   int
	CacheItems int
	Reclaimed  uint64
	DryRun     bool
}
```

- [ ] **Step 5: Add `Cleanup` to the `Manager` interface in `internal/runtime/runtime.go`**

In the `Manager` interface (after the `KeepLastNImages` line at `runtime.go:36`):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)
```

- [ ] **Step 6: Add `Cleanup` to `stubManager` in `internal/runtime/runtime.go`**

After the `KeepLastNImages` stub method (after `runtime.go:119`):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
}
```

- [ ] **Step 7: Add `Cleanup` to the three mock types**

In `internal/cli/root_test.go` (after the `KeepLastNImages` method at line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) { return runtime.CleanupReport{}, nil }
```

In `internal/idle/idle_test.go` (after the `KeepLastNImages` method at line 33):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) { return runtime.CleanupReport{}, nil }
```

In `internal/proxy/proxy_test.go` (after the `KeepLastNImages` method at line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) { return runtime.CleanupReport{}, nil }
```

- [ ] **Step 8: Run the tests**

Run: `go build ./... && go test ./... -count=1`

Expected: all packages compile and all tests PASS (including the two new tests)

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Cleanup types and Manager interface method"
```

---

### Task 2: Pure parsing/formatting helpers and Docker CLI arg builders

**Files:**
- Modify: `internal/runtime/cleanup.go` — add helpers and arg builders
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupReport` from Task 1
- Produces (used by Task 3's `dockerRuntime.Cleanup` and Task 4's CLI printer):
  - `FormatBytes(n uint64) string` — exported, used by `cli.printCleanupReport`
  - `parseHumanSize(s string) (uint64, error)` — parse Docker human sizes ("5.123kB", "45.6MB", "0B")
  - `parsePruneOutput(output string) (int, uint64, error)` — (item count, reclaimed bytes) from a Docker prune stdout
  - `dfEntry{Type string; TotalCount, ActiveCount int; Size, Reclaimable uint64}`
  - `parseSystemDF(output string) ([]dfEntry, error)`
  - `reportFromDF(entries []dfEntry, opts CleanupOptions) CleanupReport`
  - `containerPruneArgs() []string`, `imagePruneArgs() []string`, `volumePruneArgs() []string`, `networkPruneArgs() []string`, `builderPruneArgs() []string`, `systemDFArgs() []string`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestFormatBytes(t *testing.T) {
	tests := []struct {
		n    uint64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{5 * 1024 * 1024, "5.0 MiB"},
		{3 * 1024 * 1024 * 1024, "3.0 GiB"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.n); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestParseHumanSize(t *testing.T) {
	tests := []struct {
		in      string
		want    uint64
		wantErr bool
	}{
		{"0B", 0, false},
		{"123", 123, false},
		{"1.2kB", 1228, false},
		{"5.123kB", 5245, false},
		{"12.3MB", 12897484, false},
		{"45.6GB", 48962627174, false},
		{"1.2 kB", 1228, false},
		{"", 0, false},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		got, err := parseHumanSize(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseHumanSize(%q) expected error, got %d", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseHumanSize(%q) error = %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseHumanSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParsePruneOutput(t *testing.T) {
	output := `Deleted Containers:
abcdef123456
fedcba654321
Total reclaimed space: 5.123kB`
	count, reclaimed, err := parsePruneOutput(output)
	if err != nil {
		t.Fatalf("parsePruneOutput() error = %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if reclaimed != 5245 {
		t.Errorf("reclaimed = %d, want 5245", reclaimed)
	}
}

func TestParsePruneOutputNoReclaimLine(t *testing.T) {
	count, reclaimed, err := parsePruneOutput("Deleted Networks:\nabcdef123456\n")
	if err != nil {
		t.Fatalf("parsePruneOutput() error = %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if reclaimed != 0 {
		t.Errorf("reclaimed = %d, want 0", reclaimed)
	}
}

func TestParseSystemDF(t *testing.T) {
	input := "Images\t12\t6\t1.5GB\t800.2MB\n" +
		"Containers\t8\t2\t2.1GB\t600.3MB\n" +
		"Local Volumes\t5\t3\t500MB\t200MB\n" +
		"Build Cache\t24\t0\t45.6MB\t45.6MB\n"
	entries, err := parseSystemDF(input)
	if err != nil {
		t.Fatalf("parseSystemDF() error = %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4", len(entries))
	}
	if entries[0].Type != "Images" || entries[0].TotalCount != 12 || entries[0].ActiveCount != 6 {
		t.Errorf("Images entry = %+v", entries[0])
	}
	if entries[3].Type != "Build Cache" || entries[3].TotalCount != 24 || entries[3].ActiveCount != 0 {
		t.Errorf("Build Cache entry = %+v", entries[3])
	}
	if entries[0].Reclaimable != 839074611 {
		t.Errorf("Images Reclaimable = %d, want 839074611", entries[0].Reclaimable)
	}
}

func TestReportFromDF(t *testing.T) {
	entries := []dfEntry{
		{Type: "Images", TotalCount: 12, ActiveCount: 6, Reclaimable: 100},
		{Type: "Containers", TotalCount: 8, ActiveCount: 2, Reclaimable: 50},
		{Type: "Local Volumes", TotalCount: 5, ActiveCount: 3, Reclaimable: 25},
		{Type: "Build Cache", TotalCount: 24, ActiveCount: 0, Reclaimable: 10},
	}
	report := reportFromDF(entries, CleanupOptions{
		Containers: true, Images: true, Volumes: true, Networks: true, Cache: true,
	})
	if report.DryRun != true {
		t.Error("expected DryRun=true")
	}
	if report.Containers != 6 || report.Images != 6 || report.Volumes != 2 || report.CacheItems != 24 {
		t.Errorf("counts = containers:%d images:%d volumes:%d cache:%d, want 6/6/2/24",
			report.Containers, report.Images, report.Volumes, report.CacheItems)
	}
	if report.Networks != 0 {
		t.Errorf("Networks = %d, want 0 (docker system df has no networks row)", report.Networks)
	}
	if report.Reclaimed != 185 {
		t.Errorf("Reclaimed = %d, want 185", report.Reclaimed)
	}

	partial := reportFromDF(entries, CleanupOptions{Containers: true})
	if partial.Images != 0 || partial.Volumes != 0 || partial.CacheItems != 0 {
		t.Errorf("partial report should only count containers, got %+v", partial)
	}
	if partial.Containers != 6 {
		t.Errorf("partial.Containers = %d, want 6", partial.Containers)
	}
	if partial.Reclaimed != 50 {
		t.Errorf("partial.Reclaimed = %d, want 50", partial.Reclaimed)
	}
}

func TestCleanupArgBuilders(t *testing.T) {
	containerArgs := containerPruneArgs()
	wantContainer := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(containerArgs) != len(wantContainer) {
		t.Fatalf("containerPruneArgs() = %v, want %v", containerArgs, wantContainer)
	}
	for i := range wantContainer {
		if containerArgs[i] != wantContainer[i] {
			t.Errorf("containerPruneArgs()[%d] = %q, want %q", i, containerArgs[i], wantContainer[i])
		}
	}

	checks := map[string][]string{
		"image":   imagePruneArgs(),
		"volume":  volumePruneArgs(),
		"network": networkPruneArgs(),
		"builder": builderPruneArgs(),
	}
	wants := map[string][]string{
		"image":   {"image", "prune", "-f"},
		"volume":  {"volume", "prune", "-f"},
		"network": {"network", "prune", "-f"},
		"builder": {"builder", "prune", "-f"},
	}
	for name, args := range checks {
		want := wants[name]
		if len(args) != len(want) {
			t.Errorf("%sPruneArgs() = %v, want %v", name, args, want)
			continue
		}
		for i := range want {
			if args[i] != want[i] {
				t.Errorf("%sPruneArgs()[%d] = %q, want %q", name, i, args[i], want[i])
			}
		}
	}

	dfArgs := systemDFArgs()
	wantDF := []string{"system", "df", "--format", "{{.Type}}\t{{.TotalCount}}\t{{.ActiveCount}}\t{{.Size}}\t{{.Reclaimable}}"}
	if len(dfArgs) != len(wantDF) {
		t.Fatalf("systemDFArgs() = %v, want %v", dfArgs, wantDF)
	}
	for i := range wantDF {
		if dfArgs[i] != wantDF[i] {
			t.Errorf("systemDFArgs()[%d] = %q, want %q", i, dfArgs[i], wantDF[i])
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestFormatBytes|TestParseHumanSize|TestParsePruneOutput|TestParseSystemDF|TestReportFromDF|TestCleanupArgBuilders" -v -count=1`

Expected: FAIL with `undefined: FormatBytes`, `undefined: parseHumanSize`, etc.

- [ ] **Step 3: Implement the helpers in `internal/runtime/cleanup.go`**

Add the following to `internal/runtime/cleanup.go` (the file already imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`; add `errors`? No — add `regexp` and `strconv`):

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// FormatBytes renders a byte count as a human-readable size (e.g. "5.0 MiB").
func FormatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// parseHumanSize converts a Docker size string ("5.123kB", "45.6MB", "0B",
// "1.2 kB", or a bare byte count) to bytes.
func parseHumanSize(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	parts := strings.Fields(s)
	if len(parts) == 1 {
		if n, err := strconv.ParseUint(parts[0], 10, 64); err == nil {
			return n, nil
		}
	}
	numStr := parts[0]
	unit := ""
	if len(parts) > 1 {
		unit = parts[1]
	} else {
		i := 0
		for i < len(numStr) && (numStr[i] == '.' || (numStr[i] >= '0' && numStr[i] <= '9')) {
			i++
		}
		unit = numStr[i:]
		numStr = numStr[:i]
	}
	f, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("parse size %q: %w", s, err)
	}
	mult := uint64(1)
	switch strings.ToLower(unit) {
	case "", "b":
		mult = 1
	case "kb", "kib":
		mult = 1 << 10
	case "mb", "mib":
		mult = 1 << 20
	case "gb", "gib":
		mult = 1 << 30
	case "tb", "tib":
		mult = 1 << 40
	default:
		return 0, fmt.Errorf("unknown size unit %q in %q", unit, s)
	}
	return uint64(f * float64(mult)), nil
}

var reHexID = regexp.MustCompile(`(?i)\b[0-9a-f]{12,64}\b`)

// parsePruneOutput extracts (deleted item count, reclaimed bytes) from a
// `docker <category> prune` stdout. Item IDs are hex tokens; reclaimed space
// is read from the "Total reclaimed space:" line.
func parsePruneOutput(output string) (int, uint64, error) {
	var reclaimed uint64
	for _, line := range strings.Split(output, "\n") {
		const marker = "Total reclaimed space:"
		if idx := strings.Index(line, marker); idx >= 0 {
			val := strings.TrimSpace(line[idx+len(marker):])
			n, err := parseHumanSize(val)
			if err != nil {
				return 0, 0, err
			}
			reclaimed += n
		}
	}
	count := len(reHexID.FindAllString(output, -1))
	return count, reclaimed, nil
}

// dfEntry is one row from `docker system df`.
type dfEntry struct {
	Type        string
	TotalCount  int
	ActiveCount int
	Size        uint64
	Reclaimable uint64
}

// parseSystemDF parses tab-separated `docker system df --format` output.
func parseSystemDF(output string) ([]dfEntry, error) {
	var entries []dfEntry
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 5 {
			continue
		}
		total, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("parse df total %q: %w", parts[1], err)
		}
		active, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("parse df active %q: %w", parts[2], err)
		}
		size, err := parseHumanSize(parts[3])
		if err != nil {
			return nil, err
		}
		reclaimable, err := parseHumanSize(parts[4])
		if err != nil {
			return nil, err
		}
		entries = append(entries, dfEntry{
			Type:        parts[0],
			TotalCount:  total,
			ActiveCount: active,
			Size:        size,
			Reclaimable: reclaimable,
		})
	}
	return entries, nil
}

// reportFromDF builds a dry-run CleanupReport from `docker system df` rows.
// Unused count per category is TotalCount - ActiveCount. Docker system df has
// no networks row, so Networks stays 0.
func reportFromDF(entries []dfEntry, opts CleanupOptions) CleanupReport {
	report := CleanupReport{DryRun: true}
	find := func(t string) (int, uint64, bool) {
		for _, e := range entries {
			if e.Type == t {
				return e.TotalCount - e.ActiveCount, e.Reclaimable, true
			}
		}
		return 0, 0, false
	}
	if opts.Containers {
		if n, r, ok := find("Containers"); ok {
			report.Containers = n
			report.Reclaimed += r
		}
	}
	if opts.Images {
		if n, r, ok := find("Images"); ok {
			report.Images = n
			report.Reclaimed += r
		}
	}
	if opts.Volumes {
		if n, r, ok := find("Local Volumes"); ok {
			report.Volumes = n
			report.Reclaimed += r
		}
	}
	if opts.Cache {
		if n, r, ok := find("Build Cache"); ok {
			report.CacheItems = n
			report.Reclaimed += r
		}
	}
	return report
}

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func imagePruneArgs() []string {
	return []string{"image", "prune", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func builderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func systemDFArgs() []string {
	return []string{"system", "df", "--format", "{{.Type}}\t{{.TotalCount}}\t{{.ActiveCount}}\t{{.Size}}\t{{.Reclaimable}}"}
}
```

Note: `cleanup.go` already imports `sort` (used by `KeepLastNImages`) and `os/exec` (used by `RemoveImage`). The new import block adds `regexp` and `strconv` while keeping the rest.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestFormatBytes|TestParseHumanSize|TestParsePruneOutput|TestParseSystemDF|TestReportFromDF|TestCleanupArgBuilders" -v -count=1`

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add Docker cleanup parsing helpers and arg builders"
```

---

### Task 3: Implement `dockerRuntime.Cleanup`

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `runDocker`, `prune`, `Cleanup`, `cleanupDryRun` on `dockerRuntime`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupReport` (Task 1); `parsePruneOutput`, `parseSystemDF`, `reportFromDF`, `containerPruneArgs`/`imagePruneArgs`/`volumePruneArgs`/`networkPruneArgs`/`builderPruneArgs`/`systemDFArgs` (Task 2)
- Produces: the concrete `dockerRuntime.Cleanup(ctx, opts) (CleanupReport, error)` implementation used by the CLI in Task 4

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestDockerRuntimeImplementsCleanup(t *testing.T) {
	rt := &dockerRuntime{}
	var m Manager = rt
	if m == nil {
		t.Fatal("dockerRuntime does not implement Manager")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestDockerRuntimeImplementsCleanup -v -count=1`

Expected: FAIL with `does not implement Manager (missing method Cleanup)`

- [ ] **Step 3: Implement `runDocker`, `prune`, `cleanupDryRun`, and `Cleanup`**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) runDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

func (r *dockerRuntime) prune(ctx context.Context, args []string) (int, uint64, error) {
	out, err := r.runDocker(ctx, args...)
	if err != nil {
		return 0, 0, err
	}
	n, reclaimed, err := parsePruneOutput(out)
	if err != nil {
		return 0, 0, err
	}
	return n, reclaimed, nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	if opts.DryRun {
		return r.cleanupDryRun(ctx, opts)
	}

	var report CleanupReport
	var errs []error

	if opts.Containers {
		n, re, err := r.prune(ctx, containerPruneArgs())
		if err != nil {
			errs = append(errs, fmt.Errorf("container prune: %w", err))
		} else {
			report.Containers = n
			report.Reclaimed += re
		}
	}
	if opts.Images {
		n, re, err := r.prune(ctx, imagePruneArgs())
		if err != nil {
			errs = append(errs, fmt.Errorf("image prune: %w", err))
		} else {
			report.Images = n
			report.Reclaimed += re
		}
	}
	if opts.Volumes {
		n, re, err := r.prune(ctx, volumePruneArgs())
		if err != nil {
			errs = append(errs, fmt.Errorf("volume prune: %w", err))
		} else {
			report.Volumes = n
			report.Reclaimed += re
		}
	}
	if opts.Networks {
		n, _, err := r.prune(ctx, networkPruneArgs())
		if err != nil {
			errs = append(errs, fmt.Errorf("network prune: %w", err))
		} else {
			report.Networks = n
		}
	}
	if opts.Cache {
		n, re, err := r.prune(ctx, builderPruneArgs())
		if err != nil {
			errs = append(errs, fmt.Errorf("builder prune: %w", err))
		} else {
			report.CacheItems = n
			report.Reclaimed += re
		}
	}

	if len(errs) > 0 {
		return report, fmt.Errorf("cleanup: %w", errors.Join(errs...))
	}
	return report, nil
}

func (r *dockerRuntime) cleanupDryRun(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	out, err := r.runDocker(ctx, systemDFArgs()...)
	if err != nil {
		return CleanupReport{DryRun: true}, fmt.Errorf("system df: %w", err)
	}
	entries, err := parseSystemDF(out)
	if err != nil {
		return CleanupReport{DryRun: true}, err
	}
	return reportFromDF(entries, opts), nil
}
```

Update the import block in `internal/runtime/cleanup.go` to add `errors`:

```go
import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/runtime/... -count=1`

Expected: build succeeds, all tests PASS

- [ ] **Step 5: Manual smoke test against a real Docker daemon (if available)**

Run: `go run . cleanup --dry-run`

Expected (with Docker installed and `~/.tengiz/apps.json` empty):
```
Docker cleanup report:
  total reclaimable: 0 B
```
This exercises the `docker system df` → `parseSystemDF` → `reportFromDF` path end-to-end. If Docker is not installed, skip this step (the unit tests above already cover all parsing logic).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement dockerRuntime.Cleanup"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — `cleanupCmd`, flags in `init()`, `runCleanup`, `printCleanupReport`
- Modify: `internal/cli/root_test.go` — `recordingCleanupRT` mock + CLI tests
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.FormatBytes`, `runtime.Manager.Cleanup`, `runtime.Manager.KeepLastNImages`, `config.NewStoreWithEnv`, `config.Store.ListApps` (all from Tasks 1-3 or existing code)
- Produces: `cleanupCmd` (registered on `rootCmd`), `runCleanup(cmd *cobra.Command, rt runtime.Manager) error`, `printCleanupReport(w io.Writer, report runtime.CleanupReport)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/root_test.go`:

```go
type recordingCleanupRT struct {
	mockRTForDeploy
	gotOpts []runtime.CleanupOptions
}

func (m *recordingCleanupRT) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	m.gotOpts = append(m.gotOpts, opts)
	return runtime.CleanupReport{Containers: 3, Images: 5, Reclaimed: 12288}, nil
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

func TestCleanupCommandFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"containers", "images", "volumes", "networks", "cache", "dry-run", "keep-images"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func newCleanupTestCmd(t *testing.T) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("cache", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Int("keep-images", 5, "")
	return cmd
}

func TestRunCleanupDefaultsToAll(t *testing.T) {
	oldDataDir := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = oldDataDir }()

	mock := &recordingCleanupRT{}
	cmd := newCleanupTestCmd(t)
	if err := runCleanup(cmd, mock); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if len(mock.gotOpts) != 1 {
		t.Fatalf("Cleanup called %d times, want 1", len(mock.gotOpts))
	}
	opts := mock.gotOpts[0]
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.Cache {
		t.Errorf("expected all categories with no flags, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("expected DryRun=false")
	}
}

func TestRunCleanupCategoryFlags(t *testing.T) {
	oldDataDir := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = oldDataDir }()

	mock := &recordingCleanupRT{}
	cmd := newCleanupTestCmd(t)
	cmd.Flags().Set("containers", "true")
	cmd.Flags().Set("networks", "true")
	if err := runCleanup(cmd, mock); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	opts := mock.gotOpts[0]
	if !opts.Containers || !opts.Networks {
		t.Errorf("expected containers+networks enabled, got %+v", opts)
	}
	if opts.Images || opts.Volumes || opts.Cache {
		t.Errorf("unexpected categories enabled, got %+v", opts)
	}
}

func TestRunCleanupDryRun(t *testing.T) {
	oldDataDir := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = oldDataDir }()

	mock := &recordingCleanupRT{}
	cmd := newCleanupTestCmd(t)
	cmd.Flags().Set("images", "true")
	cmd.Flags().Set("dry-run", "true")
	if err := runCleanup(cmd, mock); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	opts := mock.gotOpts[0]
	if !opts.DryRun {
		t.Error("expected DryRun=true")
	}
	if !opts.Images || opts.Containers {
		t.Errorf("expected images only, got %+v", opts)
	}
}

func TestRunCleanupAppliesImageRetention(t *testing.T) {
	oldDataDir := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = oldDataDir }()

	store := config.NewStore(dataDir)
	if err := store.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}}); err != nil {
		t.Fatal(err)
	}

	mock := &recordingCleanupRT{}
	cmd := newCleanupTestCmd(t)
	cmd.Flags().Set("images", "true")
	if err := runCleanup(cmd, mock); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if len(mock.gotOpts) != 1 {
		t.Fatalf("Cleanup called %d times, want 1", len(mock.gotOpts))
	}
}

func TestPrintCleanupReport(t *testing.T) {
	var buf bytes.Buffer
	printCleanupReport(&buf, runtime.CleanupReport{
		Containers: 2,
		Images:     1,
		Volumes:    3,
		Reclaimed:  12288,
	})
	out := buf.String()
	for _, want := range []string{"Docker cleanup report:", "containers removed: 2", "images removed: 1", "volumes removed: 3", "total removed: 12.0 KiB"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}

	buf.Reset()
	printCleanupReport(&buf, runtime.CleanupReport{DryRun: true, Reclaimed: 4096})
	out = buf.String()
	if !strings.Contains(out, "total reclaimable: 4.0 KiB") {
		t.Errorf("dry-run output missing reclaimable line, got:\n%s", out)
	}
}
```

Note: `TestRunCleanupAppliesImageRetention` verifies the retention + cleanup path with a non-dry-run `--images` call. The retention count itself is asserted implicitly (no error); the recording mock records `Cleanup` opts, and `KeepLastNImages` on `mockRTForDeploy` is a no-op — the test guards against regressions in flag parsing/error handling.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup|TestRunCleanup|TestPrintCleanupReport" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: runCleanup`, `undefined: printCleanupReport`

- [ ] **Step 3: Implement the command in `internal/cli/root.go`**

Add the `cleanupCmd` definition and its registration in `init()` (after `rootCmd.AddCommand(rmCmd)` at `root.go:44`):

```go
	rootCmd.AddCommand(rmCmd)
	rootCmd.AddCommand(cleanupCmd)
```

And the flag definitions in `init()` (after the existing `logsCmd` flags at `root.go:85`):

```go
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images and enforce per-app image retention")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused anonymous volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "report reclaimable space without pruning")
	cleanupCmd.Flags().Int("keep-images", 5, "number of images to retain per app when --images is set")
```

Add the command definition, `runCleanup`, and `printCleanupReport` after the `psCmd` definition (after `root.go:601`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Reclaim disk space by pruning unused Docker resources",
	Long: "Prunes stopped non-Tengiz containers, dangling images, unused anonymous volumes, " +
		"unused networks, and the Docker build cache. Tengiz-managed containers (labeled " +
		"tengiz-app) are always preserved. With no category flags, all categories are pruned. " +
		"Use --dry-run to preview what would be reclaimed.",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		return runCleanup(cmd, rt)
	},
}

func runCleanup(cmd *cobra.Command, rt runtime.Manager) error {
	env := getEnv(cmd)
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	cache, _ := cmd.Flags().GetBool("cache")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	keepImages, _ := cmd.Flags().GetInt("keep-images")

	opts := runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		Cache:      cache,
		DryRun:     dryRun,
	}
	if !opts.Any() {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.Cache = true
	}

	if opts.Images && !opts.DryRun && keepImages > 0 {
		store := config.NewStoreWithEnv(dataDir, env)
		apps, err := store.ListApps()
		if err == nil {
			for _, app := range apps {
				if err := rt.KeepLastNImages(cmd.Context(), app.Name, keepImages); err != nil {
					log.Printf("[tengiz] warning: image retention for %s: %v", app.Name, err)
				}
			}
		}
	}

	report, err := rt.Cleanup(cmd.Context(), opts)
	if err != nil {
		return err
	}
	printCleanupReport(os.Stdout, report)
	return nil
}

func printCleanupReport(w io.Writer, report runtime.CleanupReport) {
	verb := "removed"
	if report.DryRun {
		verb = "reclaimable"
	}
	fmt.Fprintf(w, "Docker cleanup report:\n")
	if report.Containers > 0 {
		fmt.Fprintf(w, "  containers %s: %d\n", verb, report.Containers)
	}
	if report.Images > 0 {
		fmt.Fprintf(w, "  images %s: %d\n", verb, report.Images)
	}
	if report.Volumes > 0 {
		fmt.Fprintf(w, "  volumes %s: %d\n", verb, report.Volumes)
	}
	if report.Networks > 0 {
		fmt.Fprintf(w, "  networks %s: %d\n", verb, report.Networks)
	}
	if report.CacheItems > 0 {
		fmt.Fprintf(w, "  build cache entries %s: %d\n", verb, report.CacheItems)
	}
	fmt.Fprintf(w, "  total %s: %s\n", verb, runtime.FormatBytes(report.Reclaimed))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/cli/... ./internal/runtime/... -count=1`

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Documentation (README + feature tracking)

**Files:**
- Modify: `README.md` — new `### tengiz cleanup` section in CLI Reference
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:**
- Consumes: the final CLI behavior from Task 4 (`tengiz cleanup` flags and output)

- [ ] **Step 1: Add the CLI reference section to `README.md`**

Insert a new section between the `tengiz rollback` section and `### tengiz domain` (the anchor is the `### tengiz domain` heading at `README.md:238`):

```markdown
### `tengiz cleanup`

Reclaim disk space by pruning unused Docker resources. Tengiz-managed containers (labeled `tengiz-app`) are always preserved, so scale-to-zero cold starts and rollback containers are safe.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling images and enforce per-app image retention |
| `--volumes` | Prune unused anonymous volumes |
| `--networks` | Prune unused networks |
| `--cache` | Prune the Docker build cache |
| `--dry-run` | Report reclaimable space without pruning |
| `--keep-images <n>` | Images retained per app when `--images` is set (default: 5) |

With no category flags, all five categories are cleaned:

```bash
tengiz cleanup              # clean everything
tengiz cleanup --dry-run    # preview what would be reclaimed
tengiz cleanup --cache      # build cache only
```

The command honors `--env` for per-app image retention: when `--images` is set, the five most recent images per app in that environment are kept before dangling images are pruned. For `--dry-run`, the reported image reclaimable space is an approximation from `docker system df` (it includes all unreferenced images, while a real `--images` run only removes dangling images).
```

- [ ] **Step 2: Update `docs/FUTURES_FEATURES.md`**

Mark feature #6 as implemented in the P0 table (line 19). Change:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "✅ Implemented Features (Not Pending)" table (after the first row, line 241):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-17) |
```

- [ ] **Step 3: Verify docs changes**

Run: `git diff --stat README.md docs/FUTURES_FEATURES.md`

Expected: README has 1 new section; FUTURES_FEATURES.md has the `✅` marker and 1 new implemented row

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

### Task 6: Full verification

**Files:** none (verification only — no commit, since an empty-commit must never be created)

- [ ] **Step 1: Build the binary**

Run: `go build -o tengiz .`

Expected: builds without error, produces `./tengiz`

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`

Expected: no findings

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: all tests PASS (note: proxy tests are slow, ~2s each, per AGENTS.md)

- [ ] **Step 4: Verify the CLI help output**

Run: `./tengiz cleanup --help`

Expected: help shows all seven flags (`--containers`, `--images`, `--volumes`, `--networks`, `--cache`, `--dry-run`, `--keep-images`)

After this gate passes, the branch is ready to merge (`feat/docker-housekeeping`).

---

## Self-Review

**1. Spec coverage** — Feature #6 (Docker Housekeeping): `tengiz cleanup` command ✅ (Task 4), label-based protection of Tengiz containers via `--filter "label!=tengiz-app"` ✅ (Task 2 arg builder + Task 3), disk-space reclamation report ✅ (Tasks 2-4), `--dry-run` preview ✅ (Tasks 3-4), per-app image retention tied to `--env` ✅ (Task 4). Doc update ✅ (Task 5). The Coolify-derived periodic background job is intentionally out of scope (that is feature #57 Background Monitoring Scheduler, a separate P1 item) — this plan delivers the manual `tengiz cleanup` operator command the feature rationale names explicitly.

**2. Placeholder scan** — Every code step contains complete, compilable code; every run step has an exact command and expected outcome; no "TBD"/"add validation"/"similar to Task N" placeholders. The only optional step is the real-Docker smoke test, which is explicitly marked skippable and is not required for the deliverable.

**3. Type consistency** — `runtime.CleanupOptions` fields (`Containers/Images/Volumes/Networks/Cache/DryRun`) and `Any()` are defined in Task 1 and used identically in Tasks 3-4. `runtime.CleanupReport` fields are defined in Task 1 and consumed by `printCleanupReport` in Task 4. `reportFromDF` returns `CleanupReport{DryRun: true}` consistently with `cleanupDryRun`. `FormatBytes` is exported (Task 2) and called as `runtime.FormatBytes` from the CLI (Task 4). Arg builder names are stable across Tasks 2-3. The `Manager` interface method signature `Cleanup(ctx, opts) (CleanupReport, error)` is identical in Tasks 1, 3, and the mock additions. No name drift.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-17-docker-housekeeping.md`. Two execution options:

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?