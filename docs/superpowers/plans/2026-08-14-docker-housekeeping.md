# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely reclaims disk space by pruning unused Docker containers, images, networks, volumes, and build cache while always preserving Tengiz-managed containers.

**Architecture:** A new `internal/housekeep` package wraps the `docker` CLI (exec-based, matching the `runtime` package pattern). It runs per-category prune commands (`docker container prune`, `docker image prune`, `docker network prune`, `docker volume prune`, `docker builder prune`) behind a small `Runner` interface so all orchestration is unit-testable with a fake runner — no real Docker needed in tests. Tengiz-managed containers are protected with the `--filter label!=tengiz-app` flag, which keeps every container carrying the `tengiz-app` label (including stopped scale-to-zero containers). A `--dry-run` mode inspects what would be removed via non-destructive list commands. The CLI command handles the confirmation prompt, category selection, and human-readable report printing.

**Tech Stack:** Go 1.26, Cobra (CLI), standard library only (`os/exec`, `regexp`, `strconv`, `strings`). No new external dependencies.

## Global Constraints

- No new external dependencies — standard library plus the existing `cobra`/`spf13` deps only
- All Docker invocations go through the `docker` binary via `os/exec` (no Docker SDK), consistent with `internal/runtime`
- Tengiz-managed containers must always be preserved: container prune must use `--filter label!=tengiz-app`
- `label!=tengiz-app` is an exclusion filter: objects *with* the `tengiz-app` label are kept, all others are candidates
- Default category set when no category flag is given = all five categories (`containers`, `images`, `networks`, `volumes`, `build-cache`)
- `--dry-run` must never issue a destructive command (nothing starting with `prune`, `rm`, `system prune`)
- Confirmation prompt is required unless `--force` or `--dry-run` is given
- Command output style follows the existing `[tengiz]` prefix convention
- Tests must not require a real Docker daemon — fake `Runner` for the package, stub runner for the CLI
- No command registration changes in `internal/cli/root.go`; `cmd_cleanup.go` registers itself in its own `init()` (pattern from `internal/cli/preview.go`)
- Cleanup is host-wide and environment-agnostic (no app arguments, ignores `--env`)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/housekeep/housekeep.go` | `Category`, `Options`, `Report`, `Runner` interface, `realRunner`, `Pruner` + `New()`/`NewWithRunner()`, `ParseCategories`, byte/size parsing (`parseBytes`, `parseReclaimedSpace`, `parsePruneItems`) |
| `internal/housekeep/housekeep_test.go` | Tests for Task 1 helpers |
| `internal/housekeep/plan.go` | Pure command builders `pruneCommand()`/`dryRunCommand()`, `inspectItems()` dispatcher, and dry-run output parsers (`parsePsContainers`, `parseDanglingImages`, `parseNetworks`, `parseVolumes`, `parseBuildCacheReclaimable`) |
| `internal/housekeep/plan_test.go` | Tests for Task 2 helpers |
| `internal/housekeep/prune.go` | `Pruner.Prune(ctx, Options)` orchestration — runs per-category commands, collects items + reclaimed bytes, records per-category errors, continues on failure |
| `internal/housekeep/prune_test.go` | Tests for `Prune` with a fake `Runner` |
| `internal/cli/cmd_cleanup.go` | `cleanupCmd` (cobra), flags, `newCleanupPruner` injection var, `selectedCleanupCategories`, `confirmCleanup`, `printCleanupReport`, `humanBytes` |
| `internal/cli/cmd_cleanup_test.go` | CLI tests: registration, flags, dry-run category filtering, abort-without-confirmation |
| `README.md` | Add `### tengiz cleanup` section to CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` line to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

The `runtime` package keeps its existing `RemoveImage`/`KeepLastNImages` (per-app, called during deploy). `housekeep` is deliberately separate: it handles host-wide, category-based pruning and is independent of app lifecycle.

---

### Task 1: Package scaffolding + core parsing (`internal/housekeep/housekeep.go`)

**Files:**
- Create: `internal/housekeep/housekeep.go`
- Test: `internal/housekeep/housekeep_test.go`

**Interfaces:**
- Consumes: nothing (new package, no imports from other tengiz packages)
- Produces: `type Category string` with constants `CategoryContainers`, `CategoryImages`, `CategoryNetworks`, `CategoryVolumes`, `CategoryBuildCache`; `var AllCategories = []Category{...}`; `func ParseCategories(names []string) ([]Category, error)`; `type Options struct { DryRun bool; Categories []Category }`; `type CategoryReport struct { Category Category; Items []string; ReclaimedBytes int64 }`; `type Report struct { DryRun bool; Categories []CategoryReport; TotalReclaimedBytes int64; Errors map[Category]error }`; `type Runner interface { Run(ctx context.Context, args ...string) (string, error) }`; `func New() (*Pruner, error)`; `func NewWithRunner(r Runner) *Pruner`; internal `func parseBytes(s string) (int64, error)`, `func parseReclaimedSpace(output string) int64`, `func parsePruneItems(output string) []string`

- [ ] **Step 1: Create the feature branch**

Run:
```bash
git checkout -b feat/docker-housekeeping
```
Expected: branch created, `git branch --show-current` prints `feat/docker-housekeeping`.

- [ ] **Step 2: Write the failing tests**

Create `internal/housekeep/housekeep_test.go`:

```go
package housekeep

import (
	"testing"
)

func TestParseBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0B", 0},
		{"100B", 100},
		{"512kB", 512000},
		{"1.5MB", 1500000},
		{"55.54MB", 55540000},
		{"1.2GB", 1200000000},
		{"2KiB", 2048},
	}
	for _, tt := range tests {
		got, err := parseBytes(tt.in)
		if err != nil {
			t.Errorf("parseBytes(%q) error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseBytes(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseBytesInvalid(t *testing.T) {
	if _, err := parseBytes("garbage"); err == nil {
		t.Error("expected error for unknown size unit")
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		out  string
		want int64
	}{
		{"Total reclaimed space: 100MB", 100000000},
		{"Total reclaimed space: 55.54MB", 55540000},
		{"Build Cache entries: 2, Space reclaimed: 10MB", 10000000},
		{"no numbers here", 0},
	}
	for _, tt := range tests {
		if got := parseReclaimedSpace(tt.out); got != tt.want {
			t.Errorf("parseReclaimedSpace(%q) = %d, want %d", tt.out, got, tt.want)
		}
	}
}

func TestParsePruneItemsContainerOutput(t *testing.T) {
	output := "abc123def456\ndef456abc123\nTotal reclaimed space: 100MB"
	items := parsePruneItems(output)
	if len(items) != 2 || items[0] != "abc123def456" || items[1] != "def456abc123" {
		t.Errorf("parsePruneItems = %v, want [abc123def456 def456abc123]", items)
	}
}

func TestParsePruneItemsImageOutput(t *testing.T) {
	output := "Deleted Images:\ndeleted: sha256:1234567890abcdef1234567890abcdef\nTotal reclaimed space: 55.54MB"
	items := parsePruneItems(output)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %v", items)
	}
	if items[0] != "1234567890ab" {
		t.Errorf("item = %q, want last 12 chars of sha", items[0])
	}
}

func TestParseCategories(t *testing.T) {
	all, err := ParseCategories(nil)
	if err != nil || len(all) != 5 {
		t.Fatalf("ParseCategories(nil) = %v, %v; want 5 categories", all, err)
	}
	cats, err := ParseCategories([]string{"containers", "volumes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 || cats[0] != CategoryContainers || cats[1] != CategoryVolumes {
		t.Errorf("ParseCategories = %v", cats)
	}
	if _, err := ParseCategories([]string{"bogus"}); err == nil {
		t.Error("expected error for unknown category")
	}
}

func TestNewMissingDocker(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := New(); err == nil {
		t.Fatal("expected error when docker is not in PATH")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/housekeep/ -v -count=1`

Expected: FAIL with `undefined: parseBytes`, `undefined: parseReclaimedSpace`, `undefined: parsePruneItems`, `undefined: ParseCategories`, `undefined: CategoryContainers`, `undefined: New`. (Package does not compile yet.)

- [ ] **Step 4: Write the minimal implementation**

Create `internal/housekeep/housekeep.go`:

```go
package housekeep

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type Category string

const (
	CategoryContainers Category = "containers"
	CategoryImages     Category = "images"
	CategoryNetworks   Category = "networks"
	CategoryVolumes    Category = "volumes"
	CategoryBuildCache Category = "build-cache"
)

var AllCategories = []Category{
	CategoryContainers,
	CategoryImages,
	CategoryNetworks,
	CategoryVolumes,
	CategoryBuildCache,
}

func ParseCategories(names []string) ([]Category, error) {
	if len(names) == 0 {
		return AllCategories, nil
	}
	valid := map[Category]bool{}
	for _, c := range AllCategories {
		valid[c] = true
	}
	seen := map[Category]bool{}
	var cats []Category
	for _, n := range names {
		c := Category(n)
		if !valid[c] {
			return nil, fmt.Errorf("unknown category %q (valid: %s)", n, strings.Join(categoryNames(), ", "))
		}
		if !seen[c] {
			seen[c] = true
			cats = append(cats, c)
		}
	}
	return cats, nil
}

func categoryNames() []string {
	names := make([]string, 0, len(AllCategories))
	for _, c := range AllCategories {
		names = append(names, string(c))
	}
	return names
}

type Options struct {
	DryRun     bool
	Categories []Category
}

type CategoryReport struct {
	Category       Category
	Items          []string
	ReclaimedBytes int64
}

type Report struct {
	DryRun              bool
	Categories          []CategoryReport
	TotalReclaimedBytes int64
	Errors              map[Category]error
}

type Runner interface {
	Run(ctx context.Context, args ...string) (string, error)
}

type realRunner struct{}

func (realRunner) Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

type Pruner struct {
	runner Runner
}

func New() (*Pruner, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &Pruner{runner: realRunner{}}, nil
}

func NewWithRunner(r Runner) *Pruner {
	return &Pruner{runner: r}
}

func parseBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0B" {
		return 0, nil
	}
	units := []struct {
		suffix string
		mult   int64
	}{
		{"TiB", 1 << 40}, {"TB", 1000000000000},
		{"GiB", 1 << 30}, {"GB", 1000000000},
		{"MiB", 1 << 20}, {"MB", 1000000},
		{"KiB", 1 << 10}, {"kB", 1000},
		{"B", 1},
	}
	for _, u := range units {
		if len(s) > len(u.suffix) && strings.EqualFold(s[len(s)-len(u.suffix):], u.suffix) {
			num := strings.TrimSpace(s[:len(s)-len(u.suffix)])
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, fmt.Errorf("parse size %q: %w", s, err)
			}
			return int64(f * float64(u.mult)), nil
		}
	}
	return 0, fmt.Errorf("unknown size unit in %q", s)
}

var reclaimedRe = regexp.MustCompile(`(?i)(?:total reclaimed space|space reclaimed)\s*[:=]?\s*([0-9]+(?:\.[0-9]+)?[kmgt]?i?b)`)

func parseReclaimedSpace(output string) int64 {
	m := reclaimedRe.FindStringSubmatch(output)
	if len(m) < 2 {
		return 0
	}
	b, err := parseBytes(m[1])
	if err != nil {
		return 0
	}
	return b
}

func parsePruneItems(output string) []string {
	var items []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "reclaimed") || strings.Contains(lower, "build cache entries") {
			continue
		}
		if lower == "deleted images:" {
			continue
		}
		if strings.HasPrefix(lower, "deleted:") {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) < 2 {
				continue
			}
			id := strings.TrimSpace(parts[1])
			if len(id) > 12 {
				id = id[len(id)-12:]
			}
			items = append(items, id)
			continue
		}
		items = append(items, line)
	}
	return items
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/housekeep/ -v -count=1`

Expected: PASS (all 7 test functions).

- [ ] **Step 6: Commit**

```bash
git add internal/housekeep/housekeep.go internal/housekeep/housekeep_test.go
git commit -m "feat: add housekeep package types and size parsing"
```

---

### Task 2: Command builders + dry-run inspect parsers (`internal/housekeep/plan.go`)

**Files:**
- Create: `internal/housekeep/plan.go`
- Test: `internal/housekeep/plan_test.go`

**Interfaces:**
- Consumes: `Category`, `parseBytes` from Task 1
- Produces: `func pruneCommand(cat Category) []string`, `func dryRunCommand(cat Category) []string`, `func inspectItems(cat Category, output string) []string`, `func parsePsContainers(output string) []string`, `func parseDanglingImages(output string) []string`, `func parseNetworks(output string) []string`, `func parseVolumes(output string) []string`, `func parseBuildCacheReclaimable(output string) int64`

- [ ] **Step 1: Write the failing tests**

Create `internal/housekeep/plan_test.go`:

```go
package housekeep

import (
	"testing"
)

func TestPruneCommand(t *testing.T) {
	tests := []struct {
		cat  Category
		want []string
	}{
		{CategoryContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{CategoryImages, []string{"image", "prune", "-f"}},
		{CategoryNetworks, []string{"network", "prune", "-f"}},
		{CategoryVolumes, []string{"volume", "prune", "-f"}},
		{CategoryBuildCache, []string{"builder", "prune", "-f"}},
	}
	for _, tt := range tests {
		got := pruneCommand(tt.cat)
		if len(got) != len(tt.want) {
			t.Errorf("pruneCommand(%s) = %v, want %v", tt.cat, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("pruneCommand(%s)[%d] = %q, want %q", tt.cat, i, got[i], tt.want[i])
			}
		}
	}
}

func TestDryRunCommandContainers(t *testing.T) {
	got := dryRunCommand(CategoryContainers)
	want := []string{"ps", "-a", "--filter", "label!=tengiz-app", "--format", "{{.ID}}|{{.Names}}|{{.State}}"}
	if len(got) != len(want) {
		t.Fatalf("dryRunCommand(containers) = %v", got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("dryRunCommand[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDryRunCommandBuildCache(t *testing.T) {
	got := dryRunCommand(CategoryBuildCache)
	want := []string{"system", "df"}
	if len(got) != len(want) {
		t.Fatalf("dryRunCommand(build-cache) = %v", got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("dryRunCommand[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParsePsContainers(t *testing.T) {
	output := "aaa111|web1|running\nbbb222|web2|exited\nccc333|web3|created"
	got := parsePsContainers(output)
	if len(got) != 2 || got[0] != "web2" || got[1] != "web3" {
		t.Errorf("parsePsContainers = %v, want [web2 web3]", got)
	}
}

func TestParseDanglingImages(t *testing.T) {
	output := "sha256:aaa|<none>:<none>|100MB\nsha256:bbb|<none>:<none>|50MB"
	got := parseDanglingImages(output)
	if len(got) != 2 || got[0] != "<none>:<none>" {
		t.Errorf("parseDanglingImages = %v, want [<none>:<none> <none>:<none>]", got)
	}
}

func TestParseNetworks(t *testing.T) {
	output := "n1|bridge|bridge\nn2|myapp-net|bridge\nn3|host|host\nn4|none|null"
	got := parseNetworks(output)
	if len(got) != 1 || got[0] != "myapp-net" {
		t.Errorf("parseNetworks = %v, want [myapp-net]", got)
	}
}

func TestParseVolumes(t *testing.T) {
	output := "myapp-data\nmyapp-cache"
	got := parseVolumes(output)
	if len(got) != 2 || got[0] != "myapp-data" || got[1] != "myapp-cache" {
		t.Errorf("parseVolumes = %v, want [myapp-data myapp-cache]", got)
	}
}

func TestParseBuildCacheReclaimable(t *testing.T) {
	output := "TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE\nImages          2         1         150MB     100MB\nBuild Cache     5         0         200MB     200MB\n"
	got := parseBuildCacheReclaimable(output)
	if got != 200000000 {
		t.Errorf("parseBuildCacheReclaimable = %d, want 200000000", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/housekeep/ -run 'TestPruneCommand|TestDryRunCommand|TestParsePsContainers|TestParseDanglingImages|TestParseNetworks|TestParseVolumes|TestParseBuildCacheReclaimable' -v -count=1`

Expected: FAIL with `undefined: pruneCommand`, `undefined: dryRunCommand`, `undefined: parsePsContainers`, etc.

- [ ] **Step 3: Write the minimal implementation**

Create `internal/housekeep/plan.go`:

```go
package housekeep

import "strings"

func pruneCommand(cat Category) []string {
	switch cat {
	case CategoryContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case CategoryImages:
		return []string{"image", "prune", "-f"}
	case CategoryNetworks:
		return []string{"network", "prune", "-f"}
	case CategoryVolumes:
		return []string{"volume", "prune", "-f"}
	case CategoryBuildCache:
		return []string{"builder", "prune", "-f"}
	default:
		return nil
	}
}

func dryRunCommand(cat Category) []string {
	switch cat {
	case CategoryContainers:
		return []string{"ps", "-a", "--filter", "label!=tengiz-app", "--format", "{{.ID}}|{{.Names}}|{{.State}}"}
	case CategoryImages:
		return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}|{{.Repository}}:{{.Tag}}|{{.Size}}"}
	case CategoryNetworks:
		return []string{"network", "ls", "--format", "{{.ID}}|{{.Name}}|{{.Driver}}"}
	case CategoryVolumes:
		return []string{"volume", "ls", "--format", "{{.Name}}"}
	case CategoryBuildCache:
		return []string{"system", "df"}
	default:
		return nil
	}
}

func inspectItems(cat Category, output string) []string {
	switch cat {
	case CategoryContainers:
		return parsePsContainers(output)
	case CategoryImages:
		return parseDanglingImages(output)
	case CategoryNetworks:
		return parseNetworks(output)
	case CategoryVolumes:
		return parseVolumes(output)
	default:
		return nil
	}
}

func parsePsContainers(output string) []string {
	var items []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		if parts[2] != "running" {
			items = append(items, parts[1])
		}
	}
	return items
}

func parseDanglingImages(output string) []string {
	var items []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 2 {
			continue
		}
		items = append(items, parts[1])
	}
	return items
}

func parseNetworks(output string) []string {
	var items []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 2 {
			continue
		}
		switch parts[1] {
		case "bridge", "host", "none":
			continue
		}
		items = append(items, parts[1])
	}
	return items
}

func parseVolumes(output string) []string {
	var items []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "VOLUME NAME" {
			continue
		}
		items = append(items, line)
	}
	return items
}

func parseBuildCacheReclaimable(output string) int64 {
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "Build Cache") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 5 {
			if b, err := parseBytes(fields[len(fields)-1]); err == nil {
				return b
			}
		}
	}
	return 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/housekeep/ -v -count=1`

Expected: PASS (all Task 1 + Task 2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/housekeep/plan.go internal/housekeep/plan_test.go
git commit -m "feat: add prune and dry-run docker command builders"
```

---

### Task 3: Prune orchestration (`internal/housekeep/prune.go`)

**Files:**
- Create: `internal/housekeep/prune.go`
- Test: `internal/housekeep/prune_test.go`

**Interfaces:**
- Consumes: `Pruner.runner Runner`, `Options`, `CategoryReport`, `Report`, `AllCategories`, `pruneCommand`, `dryRunCommand`, `inspectItems`, `parsePruneItems`, `parseReclaimedSpace`, `parseBuildCacheReclaimable` (all from Tasks 1–2)
- Produces: `func (p *Pruner) Prune(ctx context.Context, opts Options) (*Report, error)` — contract: runs one docker command per enabled category; on a category failure records it in `report.Errors[cat]` and continues; `report.TotalReclaimedBytes` sums all categories' `ReclaimedBytes`; returns `(report, ctx.Err())` if the context is canceled, else `(report, nil)` even when some categories failed

- [ ] **Step 1: Write the failing tests**

Create `internal/housekeep/prune_test.go`:

```go
package housekeep

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type fakeRunner struct {
	responses map[string]string
	commands  [][]string
}

func (f *fakeRunner) Run(ctx context.Context, args ...string) (string, error) {
	key := strings.Join(args, " ")
	f.commands = append(f.commands, args)
	out, ok := f.responses[key]
	if !ok {
		return "", fmt.Errorf("no canned response for: docker %s", key)
	}
	return out, nil
}

func TestPruneRealRunsAllCategories(t *testing.T) {
	f := &fakeRunner{responses: map[string]string{
		"container prune -f --filter label!=tengiz-app": "abc123\ndef456\nTotal reclaimed space: 10MB",
		"image prune -f":                                "Deleted Images:\ndeleted: sha256:1234567890abcdef1234567890abcdef\nTotal reclaimed space: 50MB",
		"network prune -f":                              "myapp-net\nTotal reclaimed space: 0B",
		"volume prune -f":                               "vol1\nTotal reclaimed space: 20MB",
		"builder prune -f":                              "Total reclaimed space: 100MB",
	}}
	p := NewWithRunner(f)
	report, err := p.Prune(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(report.Categories) != 5 {
		t.Fatalf("expected 5 category reports, got %d", len(report.Categories))
	}
	wantTotal := int64(10+50+20+100) * 1000 * 1000
	if report.TotalReclaimedBytes != wantTotal {
		t.Errorf("TotalReclaimedBytes = %d, want %d", report.TotalReclaimedBytes, wantTotal)
	}
	if len(report.Errors) != 0 {
		t.Errorf("unexpected errors: %v", report.Errors)
	}
	containers := report.Categories[0]
	if containers.Category != CategoryContainers || len(containers.Items) != 2 {
		t.Errorf("containers report = %+v, want 2 items", containers)
	}
}

func TestPruneDryRunUsesInspectCommands(t *testing.T) {
	f := &fakeRunner{responses: map[string]string{
		"ps -a --filter label!=tengiz-app --format {{.ID}}|{{.Names}}|{{.State}}":           "aaa|web1|running\nbbb|web2|exited",
		"images --filter dangling=true --format {{.ID}}|{{.Repository}}:{{.Tag}}|{{.Size}}": "sha256:aaa|<none>:<none>|100MB",
		"network ls --format {{.ID}}|{{.Name}}|{{.Driver}}":                                 "n1|myapp-net|bridge",
		"volume ls --format {{.Name}}":                                                      "vol1\nvol2",
		"system df":                                                                         "TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE\nBuild Cache     5         0         200MB     200MB\n",
	}}
	p := NewWithRunner(f)
	report, err := p.Prune(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !report.DryRun {
		t.Error("report.DryRun = false, want true")
	}
	if len(report.Categories[0].Items) != 1 || report.Categories[0].Items[0] != "web2" {
		t.Errorf("dry containers items = %v, want [web2]", report.Categories[0].Items)
	}
	if report.Categories[4].ReclaimedBytes != 200000000 {
		t.Errorf("build cache reclaimable = %d, want 200000000", report.Categories[4].ReclaimedBytes)
	}
	if report.TotalReclaimedBytes != 200000000 {
		t.Errorf("dry-run total = %d, want 200000000", report.TotalReclaimedBytes)
	}
	for _, cmd := range f.commands {
		if strings.HasPrefix(strings.Join(cmd, " "), "prune") {
			t.Errorf("dry-run issued destructive command: %v", cmd)
		}
	}
}

func TestPruneContinuesOnCategoryError(t *testing.T) {
	f := &fakeRunner{responses: map[string]string{
		"container prune -f --filter label!=tengiz-app": "abc\nTotal reclaimed space: 10MB",
	}}
	p := NewWithRunner(f)
	report, err := p.Prune(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(report.Errors) != 4 {
		t.Errorf("expected 4 category errors, got %d: %v", len(report.Errors), report.Errors)
	}
	if len(report.Categories) != 1 || report.Categories[0].ReclaimedBytes != 10000000 {
		t.Errorf("categories = %+v, want single containers report of 10MB", report.Categories)
	}
}

func TestPruneRespectsCategoryFilter(t *testing.T) {
	f := &fakeRunner{responses: map[string]string{
		"volume prune -f": "vol1\nTotal reclaimed space: 5MB",
	}}
	p := NewWithRunner(f)
	report, err := p.Prune(context.Background(), Options{Categories: []Category{CategoryVolumes}})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(report.Categories) != 1 || report.Categories[0].Category != CategoryVolumes {
		t.Errorf("categories = %v, want only volumes", report.Categories)
	}
	if len(f.commands) != 1 {
		t.Fatalf("expected 1 docker command, got %d: %v", len(f.commands), f.commands)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/housekeep/ -run TestPrune -v -count=1`

Expected: FAIL with `undefined: (*Pruner).Prune` / compile error.

- [ ] **Step 3: Write the minimal implementation**

Create `internal/housekeep/prune.go`:

```go
package housekeep

import "context"

func (p *Pruner) Prune(ctx context.Context, opts Options) (*Report, error) {
	cats := opts.Categories
	if len(cats) == 0 {
		cats = AllCategories
	}

	report := &Report{
		DryRun: opts.DryRun,
		Errors: map[Category]error{},
	}

	for _, cat := range cats {
		cr := CategoryReport{Category: cat}
		var out string
		var err error
		if opts.DryRun {
			out, err = p.runner.Run(ctx, dryRunCommand(cat)...)
			if err == nil {
				if cat == CategoryBuildCache {
					cr.ReclaimedBytes = parseBuildCacheReclaimable(out)
				} else {
					cr.Items = inspectItems(cat, out)
				}
			}
		} else {
			out, err = p.runner.Run(ctx, pruneCommand(cat)...)
			if err == nil {
				cr.ReclaimedBytes = parseReclaimedSpace(out)
				if cat != CategoryBuildCache {
					cr.Items = parsePruneItems(out)
				}
			}
		}
		if err != nil {
			report.Errors[cat] = err
			continue
		}
		report.TotalReclaimedBytes += cr.ReclaimedBytes
		report.Categories = append(report.Categories, cr)
	}

	if ctx.Err() != nil {
		return report, ctx.Err()
	}
	return report, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/housekeep/ -v -count=1`

Expected: PASS (all housekeep tests).

- [ ] **Step 5: Commit**

```bash
git add internal/housekeep/prune.go internal/housekeep/prune_test.go
git commit -m "feat: add Pruner.Prune orchestration with per-category error handling"
```

---

### Task 4: CLI command `tengiz cleanup` (`internal/cli/cmd_cleanup.go`)

**Files:**
- Create: `internal/cli/cmd_cleanup.go`
- Test: `internal/cli/cmd_cleanup_test.go`

**Interfaces:**
- Consumes: `housekeep.Pruner`, `housekeep.New()`, `housekeep.NewWithRunner(r housekeep.Runner)`, `housekeep.Options{DryRun, Categories}`, `housekeep.Report`, `housekeep.Category*` constants, `housekeep.ParseCategories`
- Produces: `cleanupCmd *cobra.Command` (registered on `rootCmd` via `init()`), package var `newCleanupPruner func() (*housekeep.Pruner, error)` (override in tests), `func selectedCleanupCategories(cmd *cobra.Command) ([]housekeep.Category, error)`, `func confirmCleanup() bool`, `func printCleanupReport(r *housekeep.Report)`, `func humanBytes(b int64) string`

Command surface:
```
tengiz cleanup [--force] [--dry-run] [--containers] [--images] [--networks] [--volumes] [--build-cache]
```
Flags (defaults in parentheses): `--force` (false), `--dry-run` (false), `--containers` (false), `--images` (false), `--networks` (false), `--volumes` (false), `--build-cache` (false). `Args: cobra.NoArgs`.

Behavior: no category flag → all five categories. Real run without `--force` → show warning and prompt `Are you sure you want to continue? [y/N]`, reading one line from stdin; anything other than `y`/`yes` (or EOF) aborts and prints `[tengiz] cleanup aborted`. After pruning, print the report; if `report.Errors` is non-empty, print each warning to stderr and return an error.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cmd_cleanup_test.go`:

```go
package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/housekeep"
)

type recordingStubRunner struct {
	commands [][]string
}

func (r *recordingStubRunner) Run(ctx context.Context, args ...string) (string, error) {
	r.commands = append(r.commands, args)
	return "Total reclaimed space: 10MB", nil
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

func TestCleanupFlags(t *testing.T) {
	for _, flag := range []string{"force", "dry-run", "containers", "images", "networks", "volumes", "build-cache"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupDryRunOnlySelectedCategory(t *testing.T) {
	original := newCleanupPruner
	defer func() { newCleanupPruner = original }()
	runner := &recordingStubRunner{}
	newCleanupPruner = func() (*housekeep.Pruner, error) {
		return housekeep.NewWithRunner(runner), nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("expected 1 docker command, got %d: %v", len(runner.commands), runner.commands)
	}
	if got := strings.Join(runner.commands[0], " "); got != "volume ls --format {{.Name}}" {
		t.Errorf("command = %q, want %q", got, "volume ls --format {{.Name}}")
	}
}

func TestCleanupDryRunPrintsReportWithoutConfirmation(t *testing.T) {
	original := newCleanupPruner
	defer func() { newCleanupPruner = original }()
	newCleanupPruner = func() (*housekeep.Pruner, error) {
		return housekeep.NewWithRunner(&recordingStubRunner{}), nil
	}

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "dry-run") {
		t.Errorf("expected dry-run report, got: %s", output)
	}
	if strings.Contains(output, "aborted") {
		t.Errorf("dry-run should not be aborted, got: %s", output)
	}
}

func TestCleanupAbortsWithoutConfirmation(t *testing.T) {
	original := newCleanupPruner
	defer func() { newCleanupPruner = original }()
	called := false
	newCleanupPruner = func() (*housekeep.Pruner, error) {
		called = true
		return housekeep.NewWithRunner(&recordingStubRunner{}), nil
	}

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup"})
		rootCmd.Execute()
	})
	if called {
		t.Fatal("pruner should not run when confirmation is declined")
	}
	if !strings.Contains(output, "aborted") {
		t.Errorf("expected abort message, got: %s", output)
	}
}
```

Note: `captureOutput` is already defined in `internal/cli/root_test.go` and reused here. `TestCleanupAbortsWithoutConfirmation` relies on stdin being non-interactive during `go test` (reads EOF → not confirmed).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` / `undefined: newCleanupPruner`.

- [ ] **Step 3: Write the minimal implementation**

Create `internal/cli/cmd_cleanup.go`:

```go
package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/housekeep"
)

var newCleanupPruner = func() (*housekeep.Pruner, error) {
	return housekeep.New()
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker containers, images, networks, volumes, and build cache",
	Long: `Remove unused Docker resources to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app, including stopped
scale-to-zero containers) are always preserved.

By default all categories are cleaned. Select categories with the
--containers, --images, --networks, --volumes, and --build-cache flags.
Use --dry-run to preview without deleting anything. Use --force to skip
the confirmation prompt.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")

		cats, err := selectedCleanupCategories(cmd)
		if err != nil {
			return err
		}

		if !dryRun && !force && !confirmCleanup() {
			fmt.Println("[tengiz] cleanup aborted")
			return nil
		}

		pruner, err := newCleanupPruner()
		if err != nil {
			return err
		}

		report, err := pruner.Prune(cmd.Context(), housekeep.Options{
			DryRun:     dryRun,
			Categories: cats,
		})
		if err != nil {
			return err
		}

		printCleanupReport(report)

		if len(report.Errors) > 0 {
			for cat, cerr := range report.Errors {
				fmt.Fprintf(os.Stderr, "[tengiz] warning: failed to clean %s: %v\n", cat, cerr)
			}
			return fmt.Errorf("cleanup completed with errors")
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("force", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "clean stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "clean dangling images")
	cleanupCmd.Flags().Bool("networks", false, "clean unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "clean unused volumes")
	cleanupCmd.Flags().Bool("build-cache", false, "clean build cache")
	rootCmd.AddCommand(cleanupCmd)
}

func selectedCleanupCategories(cmd *cobra.Command) ([]housekeep.Category, error) {
	flagToCat := []struct {
		flag string
		cat  housekeep.Category
	}{
		{"containers", housekeep.CategoryContainers},
		{"images", housekeep.CategoryImages},
		{"networks", housekeep.CategoryNetworks},
		{"volumes", housekeep.CategoryVolumes},
		{"build-cache", housekeep.CategoryBuildCache},
	}
	var names []string
	for _, fc := range flagToCat {
		if on, _ := cmd.Flags().GetBool(fc.flag); on {
			names = append(names, string(fc.cat))
		}
	}
	return housekeep.ParseCategories(names)
}

func confirmCleanup() bool {
	fmt.Println("WARNING: This will remove:")
	fmt.Println("  - all stopped Docker containers not managed by Tengiz")
	fmt.Println("  - all dangling (untagged) images")
	fmt.Println("  - all Docker networks not used by any container")
	fmt.Println("  - all Docker volumes not used by any container")
	fmt.Println("  - all Docker build cache")
	fmt.Print("Are you sure you want to continue? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func printCleanupReport(r *housekeep.Report) {
	if r.DryRun {
		fmt.Println("[tengiz] Cleanup preview (dry-run):")
	} else {
		fmt.Println("[tengiz] Cleanup report:")
	}
	for _, cr := range r.Categories {
		switch cr.Category {
		case housekeep.CategoryContainers:
			fmt.Printf("  containers: %d stopped container(s) %s\n", len(cr.Items), cleanupAction(r.DryRun))
		case housekeep.CategoryImages:
			fmt.Printf("  images: %d dangling image(s) %s\n", len(cr.Items), cleanupAction(r.DryRun))
		case housekeep.CategoryNetworks:
			fmt.Printf("  networks: %d unused network(s) %s\n", len(cr.Items), cleanupAction(r.DryRun))
		case housekeep.CategoryVolumes:
			fmt.Printf("  volumes: %d unused volume(s) %s\n", len(cr.Items), cleanupAction(r.DryRun))
		case housekeep.CategoryBuildCache:
			fmt.Printf("  build-cache: %s %s\n", humanBytes(cr.ReclaimedBytes), cleanupVerb(r.DryRun))
		}
	}
	fmt.Printf("  total: %s %s\n", humanBytes(r.TotalReclaimedBytes), func() string {
		if r.DryRun {
			return "would be reclaimed"
		}
		return "reclaimed"
	}())
}

func cleanupAction(dryRun bool) string {
	if dryRun {
		return "would be removed"
	}
	return "removed"
}

func cleanupVerb(dryRun bool) string {
	if dryRun {
		return "would be reclaimed"
	}
	return "reclaimed"
}

func humanBytes(b int64) string {
	if b < 1000 {
		return fmt.Sprintf("%dB", b)
	}
	units := []string{"kB", "MB", "GB", "TB"}
	v := float64(b)
	i := -1
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	return fmt.Sprintf("%.1f%s", v, units[i])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`

Expected: PASS (all 5 cleanup tests).

- [ ] **Step 5: Run the full cli test package to check for regressions**

Run: `go test ./internal/cli/ -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cmd_cleanup.go internal/cli/cmd_cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 5: Documentation + final verification

**Files:**
- Modify: `README.md` (insert `### tengiz cleanup` section after the `#### tengiz secret list <app>` section, before `## Configuration`, around line 417)
- Modify: `AGENTS.md` (add a `tengiz cleanup` line to the CLI command list, after the `tengiz rollback <app>` line)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 as implemented)

**Interfaces:**
- Consumes: the implemented `tengiz cleanup` command surface (flags `--force`, `--dry-run`, `--containers`, `--images`, `--networks`, `--volumes`, `--build-cache`)

- [ ] **Step 1: Add the CLI Reference section to `README.md`**

Using the Edit tool, insert this block immediately before the `## Configuration` heading in `README.md` (after the `tengiz secret list` section):

````markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space. Tengiz-managed containers (labeled `tengiz-app`, including stopped scale-to-zero containers) are always preserved.

| Flag | Description |
|------|-------------|
| `--force` | Skip the confirmation prompt |
| `--dry-run` | Show what would be removed without removing anything |
| `--containers` | Clean stopped containers not managed by Tengiz |
| `--images` | Clean dangling images |
| `--networks` | Clean unused networks |
| `--volumes` | Clean unused volumes |
| `--build-cache` | Clean build cache |

With no category flags, all categories are cleaned. Cleanup is host-wide and ignores `--env`.

```bash
tengiz cleanup                        # clean everything (prompts for confirmation)
tengiz cleanup --dry-run              # preview what would be removed
tengiz cleanup --force                # clean everything without prompting
tengiz cleanup --containers --volumes --force
```
````

Verify: `grep -n "tengiz cleanup" README.md` shows the new section.

- [ ] **Step 2: Add the command to `AGENTS.md`**

Using the Edit tool, add this line to the CLI block in `AGENTS.md`, directly after the `tengiz rollback <app>` line:

```
tengiz cleanup [--force|--dry-run|--containers|--images|--networks|--volumes|--build-cache] → label-based prune of unused Docker resources (preserves tengiz-app containers)
```

Verify: `grep -n "tengiz cleanup" AGENTS.md` shows the new line.

- [ ] **Step 3: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

Using the Edit tool:
1. Change the feature #6 row (line 19) from `| 6 | **Docker Housekeeping** ⬜ | ...` to `| 6 | **Docker Housekeeping** ✅ | ...`
2. Add a row to the `### ✅ Implemented Features (Not Pending)` table (after the `Webhook ile Otomatik Deploy` row):
   `| — | **Docker Housekeeping** | Çok Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-14) |`

Verify: `grep -n "Docker Housekeeping" docs/FUTURES_FEATURES.md` shows both the P0 row (✅) and the implemented table row.

- [ ] **Step 4: Run the full verification suite**

Run:
```bash
go build -o tengiz . && go vet ./... && go test ./... -count=1
```

Expected: build succeeds, `go vet` clean, all tests PASS (including the existing `runtime`, `cli`, `proxy`, `config`, etc. packages — no regressions).

- [ ] **Step 5: Smoke-test the command against a real Docker daemon (if available)**

Run:
```bash
./tengiz cleanup --dry-run --volumes
```

Expected: prints a dry-run preview (`[tengiz] Cleanup preview (dry-run):` with a volumes line and total). If Docker is not installed/available, `./tengiz cleanup --dry-run` should exit with the `docker not found in PATH` error — acceptable, since the unit tests do not require Docker.

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** — Spec: "Label-based `docker system prune`. `tengiz cleanup`" + "kullanılmayan volume, network, container ve image'leri temizleme" (cleanup of unused volumes/networks/containers/images) + "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" (label-based filtering preserves Tengiz containers).
- Containers: Task 2 `pruneCommand(CategoryContainers)` uses `--filter label!=tengiz-app` → preserves `tengiz-app`-labeled containers (incl. stopped scale-to-zero). ✅
- Images: `docker image prune -f` (dangling). ✅
- Networks: `docker network prune -f`. ✅
- Volumes: `docker volume prune -f`. ✅
- Build cache: `docker builder prune -f` (matches "helper container"/cache cleanup intent). ✅
- `tengiz cleanup` CLI: Task 4. ✅
- Documentation: Task 5. ✅
- Note: the spec's "periyodik temizleme" (periodic/scheduled cleanup) is intentionally out of scope — it belongs to the separate P1 "Background Monitoring Scheduler" feature (#57). YAGNI.

**2. Placeholder scan** — Every code step contains complete, compilable code; every test step has full test code and an exact expected result; no "TBD"/"add error handling"/"similar to Task N" patterns.

**3. Type consistency** — `Category` constants, `Options.DryRun/Categories`, `CategoryReport{Category, Items, ReclaimedBytes}`, `Report{DryRun, Categories, TotalReclaimedBytes, Errors}`, `Runner.Run(ctx, args...) (string, error)`, `Pruner.New()/NewWithRunner()`, `Pruner.Prune(ctx, Options) (*Report, error)`, `ParseCategories`, `pruneCommand`, `dryRunCommand`, `inspectItems`, and the parse helpers are spelled identically across all tasks and both packages. CLI uses `housekeep.Options{DryRun: ..., Categories: ...}` and `housekeep.NewWithRunner(...)` consistently in tests. The `newCleanupPruner` var name is used in both `cmd_cleanup.go` and `cmd_cleanup_test.go`. No drift found.
