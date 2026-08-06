# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely reclaims disk space by pruning unused Docker containers, images, volumes, networks, and build cache — while never touching Tengiz-managed resources via label-based filtering.

**Architecture:** A new `internal/cleanup` package wraps the `docker` CLI (exec-based, matching the `runtime` package). It runs `docker container/image/volume/network/builder prune` with a `--filter label!=tengiz-app` guard on containers so stopped scale-to-zero containers are protected. Reclaimed space is parsed from each command's `Total reclaimed space:` line; `--dry-run` reads the reclaimable totals from `docker system df` without mutating anything. A `Cleaner` struct takes an injectable command runner so all parsing/decision logic is unit-testable without a Docker daemon. A `cleanupCmd` Cobra command wires it into the CLI.

**Tech Stack:** Go 1.26, `os/exec` (docker CLI — no Docker SDK, consistent with `internal/runtime`), Cobra flags. No new external Go dependencies.

## Global Constraints

- Command name: `tengiz cleanup`; sub-flag names exactly: `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`
- Container prune MUST use `--filter label!=tengiz-app` — protects ALL Tengiz containers (including stopped idle ones) from scale-to-zero
- Image prune MUST be limited to `--filter dangling=true` — Tengiz images are always tagged `tengiz-apps/<app>:<env>-<deploymentID>`, so dangling-only pruning never touches them (per-app retention is already handled by `runtime.KeepLastNImages`)
- `--dry-run` MUST NOT execute any mutating docker command; it only calls `docker system df`
- With no category flags set, ALL five categories run (default); setting any category flag restricts to those
- No interaction with `config.Store` / env files — cleanup is environment-agnostic
- No new external Go dependencies
- Existing tests must continue to pass without modification
- `README.md` CLI Reference must be updated (AGENTS.md rule)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/cleanup.go` | New package: `Options`, `CategoryResult`, `Result`, `Cleaner` (injectable runner), `Run()`, prune command construction, pure parsers (`parseSize`, `parseReclaimed`, `parsePruneResult`, `parseSystemDF`), `FormatBytes` |
| `internal/cleanup/cleanup_test.go` | Unit tests for parsers, dry-run safety, category selection, default behavior |
| `internal/cli/root.go` | Add `cleanupCmd` + registration in `init()`, import `internal/cleanup`, add `boolFlag` helper, `printCleanupTable` |
| `internal/cli/cleanup_test.go` | CLI tests: command registration, flags exist, table printer output |
| `README.md` | Add `tengiz cleanup` to CLI Reference |

3 files created, 3 files modified.

---

### Task 1: Cleanup package — size/reclaim parsers and formatter

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `parseSize(s string) (int64, error)`, `parseReclaimed(out string) (int64, error)`, `parsePruneResult(out, itemHeader, deletedPrefix string) (int, int64, error)`, `parseSystemDF(out string) ([]dfRow, error)`, `FormatBytes(b int64) string`, type `dfRow struct { Type string; Total, Active int; Size, Reclaimable int64 }`

- [ ] **Step 1: Create the package file with parser implementations**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"fmt"
	"strconv"
	"strings"
)

type dfRow struct {
	Type        string
	Total       int
	Active      int
	Size        int64
	Reclaimable int64
}

// parseSize converts a Docker human-readable size token ("1.2GB", "890.6MB",
// "512B") to bytes. Supports decimal (kB, MB, GB, TB) and binary (KiB, MiB,
// GiB, TiB) suffixes. Returns 0 for a missing/invalid token without error when
// the token is empty (used for the "0B" columns docker emits).
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	i := 0
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	var mult int64
	switch strings.ToLower(s[i:]) {
	case "", "b":
		mult = 1
	case "kb":
		mult = 1000
	case "mb":
		mult = 1000 * 1000
	case "gb":
		mult = 1000 * 1000 * 1000
	case "tb":
		mult = 1000 * 1000 * 1000 * 1000
	case "kib":
		mult = 1 << 10
	case "mib":
		mult = 1 << 20
	case "gib":
		mult = 1 << 30
	case "tib":
		mult = 1 << 40
	default:
		return 0, fmt.Errorf("unknown size suffix %q", s[i:])
	}
	return int64(num * float64(mult)), nil
}

// parseReclaimed extracts the "Total reclaimed space: <size>" value that every
// docker *prune command prints. Some docker versions print a bare "Total: <size>"
// line for `docker builder prune` instead, so that prefix is accepted as a
// fallback. Returns 0 when neither line is present.
func parseReclaimed(out string) (int64, error) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			token := strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			return parseSize(token)
		}
		if strings.HasPrefix(line, "Total:") {
			token := strings.TrimSpace(strings.TrimPrefix(line, "Total:"))
			return parseSize(token)
		}
	}
	return 0, nil
}

// parsePruneResult counts deleted items and parses reclaimed bytes from a
// prune command's output. itemHeader marks the "Deleted <X>:" section; only
// lines starting with deletedPrefix (when non-empty) inside that section are
// counted — used so image pruning counts only the "deleted: sha256:..." lines
// and not the paired "untagged: ..." lines.
func parsePruneResult(out, itemHeader, deletedPrefix string) (int, int64, error) {
	inSection := false
	items := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == itemHeader {
			inSection = true
			items = 0
			continue
		}
		if line == "" || strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		if inSection && (deletedPrefix == "" || strings.HasPrefix(line, deletedPrefix)) {
			items++
		}
	}
	reclaimed, err := parseReclaimed(out)
	return items, reclaimed, err
}

// parseSystemDF parses the "docker system df" table into rows. The TYPE column
// may itself contain spaces ("Local Volumes", "Build Cache"), so rows are
// parsed from the right: an optional trailing "(NN%)" token is dropped, then
// the last four tokens are TOTAL, ACTIVE, SIZE, RECLAIMABLE, and everything
// before them is the type name.
func parseSystemDF(out string) ([]dfRow, error) {
	var rows []dfRow
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "TYPE") {
			continue
		}
		tokens := strings.Fields(line)
		if strings.HasSuffix(tokens[len(tokens)-1], "%)") {
			tokens = tokens[:len(tokens)-1]
		}
		if len(tokens) < 5 {
			continue
		}
		data := tokens[len(tokens)-4:]
		total, err := strconv.Atoi(data[0])
		if err != nil {
			continue
		}
		active, err := strconv.Atoi(data[1])
		if err != nil {
			continue
		}
		size, err := parseSize(data[2])
		if err != nil {
			continue
		}
		reclaimable, err := parseSize(data[3])
		if err != nil {
			continue
		}
		rows = append(rows, dfRow{
			Type:        strings.Join(tokens[:len(tokens)-4], " "),
			Total:       total,
			Active:      active,
			Size:        size,
			Reclaimable: reclaimable,
		})
	}
	return rows, nil
}

// FormatBytes renders a byte count the way docker does (decimal units).
func FormatBytes(b int64) string {
	if b < 1000 {
		return fmt.Sprintf("%dB", b)
	}
	val := float64(b)
	for _, u := range []string{"kB", "MB", "GB", "TB"} {
		val /= 1000
		if val < 1000 {
			return fmt.Sprintf("%.1f%s", val, u)
		}
	}
	return fmt.Sprintf("%.1fPB", val/1000)
}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		in     string
		want   int64
	}{
		{"", 0},
		{"0B", 0},
		{"512B", 512},
		{"1.5kB", 1500},
		{"10MB", 10_000_000},
		{"1.2GB", 1_200_000_000},
		{"890.6MB", 890_600_000},
		{"1TB", 1_000_000_000_000},
		{"2KiB", 2048},
	}
	for _, tt := range tests {
		got, err := parseSize(tt.in)
		if err != nil {
			t.Errorf("parseSize(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseSizeInvalid(t *testing.T) {
	if _, err := parseSize("abc"); err == nil {
		t.Error("parseSize(\"abc\") expected error, got nil")
	}
	if _, err := parseSize("1.2XB"); err == nil {
		t.Error("parseSize(\"1.2XB\") expected error, got nil")
	}
}

func TestParseReclaimed(t *testing.T) {
	out := "Deleted Containers:\na1b2c3\n\nTotal reclaimed space: 1.234MB\n"
	got, err := parseReclaimed(out)
	if err != nil {
		t.Fatalf("parseReclaimed: %v", err)
	}
	if got != 1_234_000 {
		t.Errorf("parseReclaimed = %d, want 1234000", got)
	}
}

func TestParseReclaimedEmpty(t *testing.T) {
	got, err := parseReclaimed("nothing here\n")
	if err != nil {
		t.Fatalf("parseReclaimed: %v", err)
	}
	if got != 0 {
		t.Errorf("parseReclaimed = %d, want 0", got)
	}
}

func TestParseReclaimedBuilderTotalFallback(t *testing.T) {
	// Older docker prints "Total: <size>" for `docker builder prune`.
	got, err := parseReclaimed("ID   RECLAIMABLE   LAST USED\nabc  1MB            5 minutes ago\n\nTotal: 1MB\n")
	if err != nil {
		t.Fatalf("parseReclaimed: %v", err)
	}
	if got != 1_000_000 {
		t.Errorf("parseReclaimed = %d, want 1000000", got)
	}
}

func TestParsePruneResultContainers(t *testing.T) {
	out := `Deleted Containers:
a1b2c3d4e5f6
b2c3d4e5f6a7

Total reclaimed space: 2MB`
	items, reclaimed, err := parsePruneResult(out, "Deleted Containers:", "")
	if err != nil {
		t.Fatalf("parsePruneResult: %v", err)
	}
	if items != 2 {
		t.Errorf("items = %d, want 2", items)
	}
	if reclaimed != 2_000_000 {
		t.Errorf("reclaimed = %d, want 2000000", reclaimed)
	}
}

func TestParsePruneResultImages(t *testing.T) {
	out := `Deleted Images:
untagged: foo:latest
deleted: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
deleted: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb

Total reclaimed space: 512.5MB`
	items, reclaimed, err := parsePruneResult(out, "Deleted Images:", "deleted:")
	if err != nil {
		t.Fatalf("parsePruneResult: %v", err)
	}
	if items != 2 {
		t.Errorf("items = %d, want 2 (untagged lines ignored)", items)
	}
	if reclaimed != 512_500_000 {
		t.Errorf("reclaimed = %d, want 512500000", reclaimed)
	}
}

func TestParsePruneResultEmpty(t *testing.T) {
	items, reclaimed, err := parsePruneResult("Total reclaimed space: 0B\n", "Deleted Images:", "deleted:")
	if err != nil {
		t.Fatalf("parsePruneResult: %v", err)
	}
	if items != 0 || reclaimed != 0 {
		t.Errorf("items = %d, reclaimed = %d, want 0,0", items, reclaimed)
	}
}

func TestParseSystemDF(t *testing.T) {
	out := `TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE
Images          8         2         1.2GB     700MB (87%)
Containers      4         1         234.6MB   100MB (42%)
Local Volumes   3         1         5GB       0B (0%)
Build Cache     12        0         2GB       2GB
Networks        2         1         1.2kB     0B (0%)`
	rows, err := parseSystemDF(out)
	if err != nil {
		t.Fatalf("parseSystemDF: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("len(rows) = %d, want 5", len(rows))
	}
	want := map[string]int64{
		"Images":        700_000_000,
		"Containers":    100_000_000,
		"Local Volumes": 0,
		"Build Cache":   2_000_000_000,
	}
	for _, r := range rows {
		if w, ok := want[r.Type]; ok {
			if r.Reclaimable != w {
				t.Errorf("%s reclaimable = %d, want %d", r.Type, r.Reclaimable, w)
			}
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1500, "1.5kB"},
		{10_000_000, "10.0MB"},
		{1_200_000_000, "1.2GB"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.in); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL with `no non-test Go files in ...` / `package cleanup is not in std`

- [ ] **Step 4: Implement the package (create the file from Step 1)**

Create `internal/cleanup/cleanup.go` exactly as shown in Step 1 (if not already created).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: ALL PASS (10 tests)

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add docker size/reclaim parsers for cleanup package"
```

---

### Task 2: Cleaner with injectable runner and category selection

**Files:**
- Modify: `internal/cleanup/cleanup.go` (append `Options`, `Result`, `Cleaner`, `Run`, prune helpers)
- Test: `internal/cleanup/cleanup_test.go` (append)

**Interfaces:**
- Consumes: `parseSize`, `parseReclaimed`, `parsePruneResult`, `parseSystemDF` from Task 1
- Produces: `Options{ DryRun, Containers, Images, Volumes, Networks, BuildCache bool }`, `CategoryResult{ Removed int; Reclaimed int64 }`, `Result{ DryRun bool; Containers, Images, Volumes, Networks, BuildCache CategoryResult }`, `Result.TotalReclaimed() int64`, `New() *Cleaner`, `NewWithRunner(run func(ctx context.Context, name string, args ...string) (string, error)) *Cleaner`, `Cleaner.Run(ctx context.Context, opts Options) (*Result, error)`. The injected runner returns stdout of the executed command; `New()` uses `exec.CommandContext(ctx, name, args...).CombinedOutput()`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cleanup/cleanup_test.go`:

```go
import (
	"context"
	"strings"
)

// recordingRunner records every docker command and returns a scripted response.
type recordingRunner struct {
	calls []string
	// respond maps a substring of the command line to its stdout.
	respond map[string]string
}

func (r *recordingRunner) run(ctx context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	for sub, out := range r.respond {
		if strings.Contains(r.calls[len(r.calls)-1], sub) {
			return out, nil
		}
	}
	return "", nil
}

func TestRunDryRunDoesNotPrune(t *testing.T) {
	dfOut := `TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE
Images          8         2         1.2GB     700MB (87%)
Containers      4         1         234.6MB   100MB (42%)
Local Volumes   3         1         5GB       0B (0%)
Build Cache     12        0         2GB       2GB
`
	rr := &recordingRunner{respond: map[string]string{"system df": dfOut}}
	c := NewWithRunner(rr.run)

	res, err := c.Run(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run(dry-run): %v", err)
	}
	if !res.DryRun {
		t.Error("res.DryRun = false, want true")
	}
	for _, call := range rr.calls {
		if strings.Contains(call, " prune ") {
			t.Errorf("dry-run executed a prune: %s", call)
		}
	}
	if res.Images.Reclaimed != 700_000_000 {
		t.Errorf("Images reclaimed = %d, want 700000000", res.Images.Reclaimed)
	}
	if res.Containers.Reclaimed != 100_000_000 {
		t.Errorf("Containers reclaimed = %d, want 100000000", res.Containers.Reclaimed)
	}
	if res.Volumes.Reclaimed != 0 {
		t.Errorf("Volumes reclaimed = %d, want 0", res.Volumes.Reclaimed)
	}
	if res.BuildCache.Reclaimed != 2_000_000_000 {
		t.Errorf("BuildCache reclaimed = %d, want 2000000000", res.BuildCache.Reclaimed)
	}
}

func TestRunPrunesContainersAndImages(t *testing.T) {
	containerOut := "Deleted Containers:\na1b2c3\nb2c3d4\n\nTotal reclaimed space: 2MB\n"
	imageOut := "Deleted Images:\nuntagged: foo:latest\ndeleted: sha256:aaaa\ndeleted: sha256:bbbb\n\nTotal reclaimed space: 512.5MB\n"
	rr := &recordingRunner{respond: map[string]string{
		"container prune": containerOut,
		"image prune":     imageOut,
	}}
	c := NewWithRunner(rr.run)

	res, err := c.Run(context.Background(), Options{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rr.calls) != 2 {
		t.Fatalf("got %d commands, want 2: %v", len(rr.calls), rr.calls)
	}
	if !strings.Contains(rr.calls[0], "label!=tengiz-app") {
		t.Errorf("container prune missing label!=tengiz-app filter: %s", rr.calls[0])
	}
	if !strings.Contains(rr.calls[1], "dangling=true") {
		t.Errorf("image prune missing dangling=true filter: %s", rr.calls[1])
	}
	if res.Containers.Removed != 2 || res.Containers.Reclaimed != 2_000_000 {
		t.Errorf("Containers = %+v, want {2 2000000}", res.Containers)
	}
	if res.Images.Removed != 2 || res.Images.Reclaimed != 512_500_000 {
		t.Errorf("Images = %+v, want {2 512500000}", res.Images)
	}
	if got := res.TotalReclaimed(); got != 514_500_000 {
		t.Errorf("TotalReclaimed = %d, want 514500000", got)
	}
}

func TestRunDefaultPrunesAllCategories(t *testing.T) {
	rr := &recordingRunner{respond: map[string]string{
		"container prune": "Total reclaimed space: 1MB\n",
		"image prune":     "Total reclaimed space: 2MB\n",
		"volume prune":    "Total reclaimed space: 3MB\n",
		"network prune":   "Total reclaimed space: 4MB\n",
		"builder prune":   "Total reclaimed space: 5MB\n",
	}}
	c := NewWithRunner(rr.run)

	res, err := c.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rr.calls) != 5 {
		t.Fatalf("got %d commands, want 5: %v", len(rr.calls), rr.calls)
	}
	for _, want := range []string{"container prune", "image prune", "volume prune", "network prune", "builder prune"} {
		found := false
		for _, call := range rr.calls {
			if strings.Contains(call, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %q in calls %v", want, rr.calls)
		}
	}
	if got := res.TotalReclaimed(); got != 15_000_000 {
		t.Errorf("TotalReclaimed = %d, want 15000000", got)
	}
}

func TestRunSelectionIgnoresUnselected(t *testing.T) {
	rr := &recordingRunner{respond: map[string]string{"builder prune": "Total reclaimed space: 5MB\n"}}
	c := NewWithRunner(rr.run)

	res, err := c.Run(context.Background(), Options{BuildCache: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rr.calls) != 1 {
		t.Fatalf("got %d commands, want 1: %v", len(rr.calls), rr.calls)
	}
	if res.Images.Reclaimed != 0 {
		t.Errorf("Images.Reclaimed = %d, want 0 (not selected)", res.Images.Reclaimed)
	}
	if res.BuildCache.Reclaimed != 5_000_000 {
		t.Errorf("BuildCache.Reclaimed = %d, want 5000000", res.BuildCache.Reclaimed)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cleanup/... -run "TestRun" -v -count=1`

Expected: FAIL with `undefined: Options`, `undefined: NewWithRunner`, `undefined: Cleaner`

- [ ] **Step 3: Implement the Cleaner, Options, Result, and Run**

Append to `internal/cleanup/cleanup.go` (add imports `context` and `os/exec` to the existing import block):

```go
const appLabel = "tengiz-app"

type Options struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

// normalize enables every category when no category flag was set.
func (o *Options) normalize() {
	any := o.Containers || o.Images || o.Volumes || o.Networks || o.BuildCache
	if !any {
		o.Containers = true
		o.Images = true
		o.Volumes = true
		o.Networks = true
		o.BuildCache = true
	}
}

type CategoryResult struct {
	Removed   int
	Reclaimed int64
}

type Result struct {
	DryRun     bool
	Containers CategoryResult
	Images     CategoryResult
	Volumes    CategoryResult
	Networks   CategoryResult
	BuildCache CategoryResult
}

func (r *Result) TotalReclaimed() int64 {
	return r.Containers.Reclaimed + r.Images.Reclaimed +
		r.Volumes.Reclaimed + r.Networks.Reclaimed + r.BuildCache.Reclaimed
}

type Cleaner struct {
	run func(ctx context.Context, name string, args ...string) (string, error)
}

func New() *Cleaner {
	return &Cleaner{
		run: func(ctx context.Context, name string, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, string(out))
			}
			return string(out), nil
		},
	}
}

func NewWithRunner(run func(ctx context.Context, name string, args ...string) (string, error)) *Cleaner {
	return &Cleaner{run: run}
}

// Run prunes the selected categories. In dry-run mode it only reads
// "docker system df" and reports reclaimable totals per category.
func (c *Cleaner) Run(ctx context.Context, opts Options) (*Result, error) {
	opts.normalize()
	res := &Result{DryRun: opts.DryRun}

	if opts.DryRun {
		out, err := c.run(ctx, "docker", "system", "df")
		if err != nil {
			return nil, fmt.Errorf("system df: %w", err)
		}
		rows, err := parseSystemDF(out)
		if err != nil {
			return nil, fmt.Errorf("parse system df: %w", err)
		}
		for _, row := range rows {
			switch row.Type {
			case "Images":
				if opts.Images {
					res.Images.Reclaimed = row.Reclaimable
				}
			case "Containers":
				if opts.Containers {
					res.Containers.Reclaimed = row.Reclaimable
				}
			case "Local Volumes":
				if opts.Volumes {
					res.Volumes.Reclaimed = row.Reclaimable
				}
			case "Build Cache":
				if opts.BuildCache {
					res.BuildCache.Reclaimed = row.Reclaimable
				}
			}
		}
		return res, nil
	}

	if opts.Containers {
		out, err := c.run(ctx, "docker", "container", "prune", "-f",
			"--filter", fmt.Sprintf("label!=%s", appLabel))
		if err != nil {
			return nil, fmt.Errorf("container prune: %w", err)
		}
		items, reclaimed, err := parsePruneResult(out, "Deleted Containers:", "")
		if err != nil {
			return nil, fmt.Errorf("parse container prune: %w", err)
		}
		res.Containers = CategoryResult{Removed: items, Reclaimed: reclaimed}
	}

	if opts.Images {
		out, err := c.run(ctx, "docker", "image", "prune", "-f",
			"--filter", "dangling=true")
		if err != nil {
			return nil, fmt.Errorf("image prune: %w", err)
		}
		items, reclaimed, err := parsePruneResult(out, "Deleted Images:", "deleted:")
		if err != nil {
			return nil, fmt.Errorf("parse image prune: %w", err)
		}
		res.Images = CategoryResult{Removed: items, Reclaimed: reclaimed}
	}

	if opts.Volumes {
		out, err := c.run(ctx, "docker", "volume", "prune", "-f")
		if err != nil {
			return nil, fmt.Errorf("volume prune: %w", err)
		}
		items, reclaimed, err := parsePruneResult(out, "Deleted Volumes:", "")
		if err != nil {
			return nil, fmt.Errorf("parse volume prune: %w", err)
		}
		res.Volumes = CategoryResult{Removed: items, Reclaimed: reclaimed}
	}

	if opts.Networks {
		out, err := c.run(ctx, "docker", "network", "prune", "-f")
		if err != nil {
			return nil, fmt.Errorf("network prune: %w", err)
		}
		items, reclaimed, err := parsePruneResult(out, "Deleted Networks:", "")
		if err != nil {
			return nil, fmt.Errorf("parse network prune: %w", err)
		}
		res.Networks = CategoryResult{Removed: items, Reclaimed: reclaimed}
	}

	if opts.BuildCache {
		out, err := c.run(ctx, "docker", "builder", "prune", "-f")
		if err != nil {
			return nil, fmt.Errorf("builder prune: %w", err)
		}
		reclaimed, err := parseReclaimed(out)
		if err != nil {
			return nil, fmt.Errorf("parse builder prune: %w", err)
		}
		res.BuildCache = CategoryResult{Reclaimed: reclaimed}
	}

	return res, nil
}
```

The final import block of `internal/cleanup/cleanup.go` must be:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: ALL PASS (14 tests)

- [ ] **Step 5: Run vet and build**

Run: `go vet ./internal/cleanup/... && go build ./...`

Expected: no output from vet, build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add Cleaner with label-protected docker prune and dry-run"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — import `internal/cleanup`, add `cleanupCmd` + helpers + registration
- Test: `internal/cli/cleanup_test.go` (new)

**Interfaces:**
- Consumes: `cleanup.New()`, `cleanup.Options`, `cleanup.Result`, `cleanup.CategoryResult`, `cleanup.FormatBytes` from Tasks 1-2
- Produces: `cleanupCmd *cobra.Command` (Use: `cleanup`), registered on `rootCmd` in `init()`; helper `boolFlag(cmd *cobra.Command, name string) bool`; printer `printCleanupTable(res *cleanup.Result) string` (returns rendered table string for testability, CLI prints it)

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/cleanup"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdHasFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "build-cache"} {
		if flag := cleanupCmd.Flags().Lookup(name); flag == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupTable(t *testing.T) {
	res := &cleanup.Result{
		DryRun: false,
		Containers: cleanup.CategoryResult{Removed: 2, Reclaimed: 2_000_000},
		Images:     cleanup.CategoryResult{Removed: 3, Reclaimed: 512_500_000},
		Volumes:    cleanup.CategoryResult{Removed: 1, Reclaimed: 5_000_000},
		Networks:   cleanup.CategoryResult{Removed: 0, Reclaimed: 0},
		BuildCache: cleanup.CategoryResult{Removed: 0, Reclaimed: 1_000_000},
	}
	table := printCleanupTable(res)
	for _, want := range []string{"CONTAINERS", "IMAGES", "VOLUMES", "NETWORKS", "BUILD CACHE", "2", "3", "1"} {
		if !strings.Contains(table, want) {
			t.Errorf("cleanup table missing %q:\n%s", want, table)
		}
	}
	if strings.Contains(table, "DRY RUN") {
		t.Errorf("cleanup table should not show dry-run marker for non-dry run:\n%s", table)
	}
}

func TestCleanupTableDryRun(t *testing.T) {
	res := &cleanup.Result{DryRun: true, Images: cleanup.CategoryResult{Reclaimed: 700_000_000}}
	table := printCleanupTable(res)
	if !strings.Contains(table, "DRY RUN") {
		t.Errorf("cleanup table missing dry-run marker:\n%s", table)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` and `undefined: printCleanupTable`

- [ ] **Step 3: Add the import**

In `internal/cli/root.go`, add to the import block (alphabetical, before `github.com/yaso09/tengiz/internal/config`):

```go
	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/config"
```

- [ ] **Step 4: Add helpers and the command**

Add the `boolFlag` helper right after `getEnv` (which is at `internal/cli/root.go:97-103`):

```go
func boolFlag(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name)
	return v
}
```

Add `printCleanupTable` and `cleanupCmd` at the end of `internal/cli/root.go`:

```go
func printCleanupTable(res *cleanup.Result) string {
	var b strings.Builder
	title := "TENGIZ CLEANUP"
	if res.DryRun {
		title += " (DRY RUN)"
	}
	fmt.Fprintf(&b, "[tengiz] %s\n", title)
	fmt.Fprintf(&b, "%-14s %8s %12s\n", "CATEGORY", "REMOVED", "RECLAIMED")
	fmt.Fprintf(&b, "%-14s %8d %12s\n", "CONTAINERS", res.Containers.Removed, cleanup.FormatBytes(res.Containers.Reclaimed))
	fmt.Fprintf(&b, "%-14s %8d %12s\n", "IMAGES", res.Images.Removed, cleanup.FormatBytes(res.Images.Reclaimed))
	fmt.Fprintf(&b, "%-14s %8d %12s\n", "VOLUMES", res.Volumes.Removed, cleanup.FormatBytes(res.Volumes.Reclaimed))
	fmt.Fprintf(&b, "%-14s %8d %12s\n", "NETWORKS", res.Networks.Removed, cleanup.FormatBytes(res.Networks.Reclaimed))
	fmt.Fprintf(&b, "%-14s %8s %12s\n", "BUILD CACHE", "-", cleanup.FormatBytes(res.BuildCache.Reclaimed))
	return b.String()
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Reclaim disk space by pruning unused Docker resources",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanup.Options{
			DryRun:     boolFlag(cmd, "dry-run"),
			Containers: boolFlag(cmd, "containers"),
			Images:     boolFlag(cmd, "images"),
			Volumes:    boolFlag(cmd, "volumes"),
			Networks:   boolFlag(cmd, "networks"),
			BuildCache: boolFlag(cmd, "build-cache"),
		}

		c := cleanup.New()
		res, err := c.Run(cmd.Context(), opts)
		if err != nil {
			return err
		}

		fmt.Print(printCleanupTable(res))

		if res.DryRun {
			fmt.Printf("[tengiz] dry run — nothing was deleted. Run `tengiz cleanup` to reclaim the space above.\n")
			return nil
		}
		fmt.Printf("[tengiz] cleanup complete: reclaimed %s\n", cleanup.FormatBytes(res.TotalReclaimed()))
		return nil
	},
}
```

- [ ] **Step 5: Register the command and its flags**

In `init()` at `internal/cli/root.go:34-89`, add the registration right after `rootCmd.AddCommand(notificationCmd)` (line 75):

```go
	rootCmd.AddCommand(cleanupCmd)
```

And add the flags after the existing flag registrations (after line 88):

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be cleaned up without deleting anything")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling (untagged) images")
	cleanupCmd.Flags().Bool("volumes", false, "prune volumes not used by any container")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: ALL PASS (4 tests)

- [ ] **Step 7: Run full build and package tests**

Run: `go build ./... && go test ./internal/cleanup/... ./internal/cli/... -count=1`

Expected: build succeeds, all cleanup and cli tests pass

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command with dry-run and category flags"
```

---

### Task 4: Documentation update (README + feature tracker)

**Files:**
- Modify: `README.md` — add `tengiz cleanup` to CLI Reference
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:**
- Consumes: the command surface from Task 3
- Produces: user-facing docs consistent with the shipped command

- [ ] **Step 1: Read the README CLI Reference section**

Run: `grep -n "tengiz ps\|tengiz rollback\|CLI Reference" README.md`

Then open `README.md` around the `## CLI Reference` section (line 103) and find the last command listed before it (e.g. `tengiz volume ...`).

- [ ] **Step 2: Add the cleanup section to README.md**

Append a new section after the last CLI command section (the `tengiz volume ...` section ending around line 310):

```markdown
### `tengiz cleanup`

Reclaim disk space by pruning unused Docker resources. Tengiz-managed
containers (including stopped scale-to-zero ones) are always protected via
label-based filtering and are never removed.

```bash
tengiz cleanup                    # prune all five categories
tengiz cleanup --dry-run          # show reclaimable space without deleting
tengiz cleanup --images --volumes # only prune images and volumes
```

Flags:

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be reclaimed without deleting anything |
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling (untagged) images |
| `--volumes` | Prune volumes not used by any container |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune Docker build cache |

With no category flags, all categories are pruned.
```

- [ ] **Step 3: Update the feature tracker**

In `docs/FUTURES_FEATURES.md`, change the feature #6 row at line 19 from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Also add a row to the `### ✅ Implemented Features (Not Pending)` table (after line 253, the last row `Webhook ile Otomatik Deploy`):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-06) |
```

- [ ] **Step 4: Verify docs render (markdown references valid)**

Run: `go build ./... && go test ./... -count=1 2>&1 | tail -20`

Expected: build succeeds; test suite reports `ok` for all packages (proxy TCP-timeout and idle time-sensitive tests may take ~2s each but must pass)

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark feature implemented"
```

---

### Task 5: Final verification and self-review

**Files:**
- No code changes — verification only

- [ ] **Step 1: Full test suite**

Run: `go test ./... -v -count=1 2>&1 | tail -40`

Expected: every package reports `ok` (proxy and idle tests pass, just slower)

- [ ] **Step 2: Static analysis**

Run: `go vet ./...`

Expected: no output

- [ ] **Step 3: Build**

Run: `go build -o /tmp/tengiz-cleanup-test .`

Expected: binary produced; run `/tmp/tengiz-cleanup-test cleanup --help` and confirm usage lists `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`.

- [ ] **Step 4: Self-review against spec**

Check against `docs/FUTURES_FEATURES.md` feature #6 requirements:
- `tengiz cleanup` command ✅ (Task 3)
- Label-based filtering protecting Tengiz containers ✅ (`--filter label!=tengiz-app` in Task 2)
- Prunes containers, images, volumes, networks, build cache ✅ (Task 2)
- Disk-space reporting (`docker system df` + `Total reclaimed space` parsing) ✅ (Tasks 1-2)
- Dry-run safety ✅ (dry-run only reads `docker system df`, enforced by test in Task 2)
- README update ✅ (Task 4, AGENTS.md rule)
- No new external deps ✅ (only `os/exec`)

- [ ] **Step 5: Placeholder scan**

Search the plan for any "TBD", "TODO", "implement later", "fill in details", "Similar to Task", "add appropriate error handling". Every step above contains complete code. If any pattern is found, replace it with concrete code before executing.

- [ ] **Step 6: Type consistency check**

- `Options{ DryRun, Containers, Images, Volumes, Networks, BuildCache bool }` — same fields in Task 2 def, Task 2 tests, and Task 3 handler
- `Result{ DryRun bool; Containers, Images, Volumes, Networks, BuildCache CategoryResult }` — used in Task 2 tests, Task 3 `printCleanupTable`, Task 3 table tests
- `CategoryResult{ Removed int; Reclaimed int64 }` — consistent across Tasks 2-3
- `Cleaner.Run(ctx context.Context, opts Options) (*Result, error)` — single call site in Task 3 matches Task 2 signature
- `NewWithRunner(run func(ctx context.Context, name string, args ...string) (string, error))` — matches the `recordingRunner.run` method used in Task 2 tests
- Parsers `parseSize(string) (int64, error)`, `parseReclaimed(string) (int64, error)`, `parsePruneResult(string, string, string) (int, int64, error)`, `parseSystemDF(string) ([]dfRow, error)` — same signatures in Tasks 1-2
- `FormatBytes(int64) string` — same in Task 1 impl, Task 1 test, Task 3 printer
- Docker flags `label!=tengiz-app` and `dangling=true` — identical strings in Task 2 impl and Task 2 test assertions

If any inconsistency is found, fix it inline before executing.
