# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning stopped containers, unused images, unused networks, and the Docker build cache (plus opt-in volumes), while protecting Tengiz-managed resources via Docker labels.

**Architecture:** A new `internal/cleanup` package owns all prune/estimate logic and docker execution, following the `builder` package precedent of domain-scoped `os/exec` calls. Instead of one monolithic `docker system prune`, it runs per-category `docker <type> prune -f` commands so label protection can be applied precisely: container pruning filters `label!=tengiz-app` (protecting idle-stopped Tengiz containers), and `--all` image pruning uses `label!=tengiz-app` to keep Tengiz images. The builder adds a `tengiz-app=<app>` label to every built image so that protection works. A thin `tengiz cleanup` cobra command wires the flags (`--dry-run`, `--all`, `--volumes`).

**Tech Stack:** Go 1.26 standard library only (`os/exec`, `encoding/json`, `regexp`, `strconv`, `strings`). Cobra for the CLI. No new external dependencies.

## Global Constraints

- Before starting, create a feature branch: `git checkout -b feat/docker-housekeeping`
- Tengiz-managed containers carry label `tengiz-app=<app>` and must NEVER be pruned (this includes idle-stopped scale-to-zero containers)
- Default `tengiz cleanup` prunes: stopped non-Tengiz containers, dangling images, unused networks, build cache — nothing else
- `--volumes` must be an explicit opt-in flag; volumes are never pruned by default
- `--dry-run` must not delete anything; it reports reclaimable space from `docker system df`
- `--all` prunes all unused images but only those NOT labeled `tengiz-app` (Tengiz images protected)
- Tengiz images are tagged under the `tengiz-apps/` repository and are tagged (never dangling) — default cleanup is already safe for them
- Nixpacks-built images cannot carry labels (nixpacks CLI has no label support); this caveat is documented in the README
- All docker exec happens in `internal/cleanup`; the `runtime.Manager` interface is NOT modified
- No new external dependencies
- Follow TDD: write failing test → run to confirm failure → implement → run to confirm pass → commit
- Run `go test ./... -v -count=1` and `go vet ./...` before the final commit

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/cleanup.go` (create) | All prune logic: types, size parse/format helpers, docker prune command construction, `docker system df` parser, `Manager` (Prune/Estimate), output formatters |
| `internal/cleanup/cleanup_test.go` (create) | Unit tests for all pure functions + Manager orchestration via a fake command runner |
| `internal/cli/cleanup.go` (create) | The `tengiz cleanup` cobra command with `--dry-run`, `--all`, `--volumes` flags; registers itself in `init()` |
| `internal/cli/cleanup_test.go` (create) | Tests that the command is registered and has the expected flags |
| `internal/builder/builder.go` (modify) | Label every built image `tengiz-app=<appName>` via a new `buildArgs()` helper |
| `internal/builder/builder_test.go` (modify) | Tests for `buildArgs()` label injection |
| `README.md` (modify) | Add cleanup to Features list + CLI Reference |
| `docs/FUTURES_FEATURES.md` (modify) | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Cleanup types and size helpers

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `cleanup.Options{All, Volumes bool}`, `cleanup.Result{Containers, Images, Networks, Volumes, BuildCache, Total string}`, `cleanup.Category{Type string; TotalCount, Active int; Size, Reclaimable string}`, `cleanup.Report{Categories []Category; Total string}`, and unexported `parseSize(s string) (int64, error)`, `formatBytes(n int64) string`, `parseReclaimed(output string) (string, error)` used by later tasks

- [ ] **Step 1: Create the test file and write the failing tests**

`internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"strings"
	"testing"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		in       string
		expected int64
		wantErr  bool
	}{
		{"0B", 0, false},
		{"100B", 100, false},
		{"13.07MB", 13070000, false},
		{"800MB", 800000000, false},
		{"1.5GB", 1500000000, false},
		{"10KiB", 10240, false},
		{"200.1MB", 200100000, false},
		{"", 0, true},
		{"abc", 0, true},
		{"12XB", 0, true},
	}
	for _, tt := range tests {
		got, err := parseSize(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseSize(%q) expected error, got %d", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSize(%q) error = %v", tt.in, err)
			continue
		}
		if got != tt.expected {
			t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.expected)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in       int64
		expected string
	}{
		{0, "0B"},
		{500, "500B"},
		{1024, "1.02kB"},
		{13070000, "13.07MB"},
		{2000000, "2MB"},
		{1500000000, "1.5GB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.in)
		if got != tt.expected {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.expected)
		}
	}
}

func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		in       string
		expected string
	}{
		{"Total reclaimed space: 13.07MB", "13.07MB"},
		{"Deleted Containers:\n...\n\nTotal reclaimed space: 50MB\n", "50MB"},
		{"Total reclaimed space: 0B", "0B"},
		{"", "0B"},
		{"No output here", "0B"},
	}
	for _, tt := range tests {
		got, err := parseReclaimed(tt.in)
		if err != nil {
			t.Errorf("parseReclaimed(%q) error = %v", tt.in, err)
			continue
		}
		if got != tt.expected {
			t.Errorf("parseReclaimed(%q) = %q, want %q", tt.in, got, tt.expected)
		}
	}
}

func TestTrimZeros(t *testing.T) {
	tests := []struct {
		in, expected string
	}{
		{"1.50", "1.5"},
		{"13.07", "13.07"},
		{"2.00", "2"},
		{"100", "100"},
	}
	for _, tt := range tests {
		if got := trimZeros(tt.in); got != tt.expected {
			t.Errorf("trimZeros(%q) = %q, want %q", tt.in, got, tt.expected)
		}
	}
}

var _ = strings.TrimSpace // placeholder to keep strings import for later tasks
```

Note: the `var _ = strings.TrimSpace` line is a temporary placeholder so the `strings` import compiles in Task 1. It is REMOVED in Task 2 once `parseSystemDF` uses `strings` for real.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL with `undefined: parseSize`, `undefined: formatBytes`, `undefined: parseReclaimed`, `undefined: trimZeros` (package does not exist yet)

- [ ] **Step 3: Create `internal/cleanup/cleanup.go` with types and size helpers**

```go
package cleanup

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Options controls what a cleanup run removes.
type Options struct {
	All     bool // prune all unused images, not just dangling ones
	Volumes bool // also prune unused volumes
}

// Result reports reclaimed space per Docker resource category.
type Result struct {
	Containers string
	Images     string
	Networks   string
	Volumes    string
	BuildCache string
	Total      string
}

// Category is one row of `docker system df` output.
type Category struct {
	Type        string `json:"Type"`
	TotalCount  int    `json:"TotalCount"`
	Active      int    `json:"Active"`
	Size        string `json:"Size"`
	Reclaimable string `json:"Reclaimable"`
}

// Report summarizes reclaimable disk space without removing anything.
type Report struct {
	Categories []Category
	Total      string
}

var (
	sizeRe      = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*([a-zA-Z]*)$`)
	reclaimedRe = regexp.MustCompile(`(?i)Total reclaimed space:\s*(.+)`)
)

var sizeUnits = map[string]int64{
	"":    1,
	"b":   1,
	"kb":  1000,
	"kib": 1024,
	"mb":  1000 * 1000,
	"mib": 1024 * 1024,
	"gb":  1000 * 1000 * 1000,
	"gib": 1024 * 1024 * 1024,
	"tb":  1000 * 1000 * 1000 * 1000,
	"tib": 1024 * 1024 * 1024 * 1024,
}

// parseSize converts a human-readable docker size ("13.07MB", "800MB", "0B")
// to a byte count.
func parseSize(s string) (int64, error) {
	m := sizeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("cannot parse size %q", s)
	}
	num, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse size %q: %w", s, err)
	}
	unit := strings.ToLower(m[2])
	mult, ok := sizeUnits[unit]
	if !ok {
		return 0, fmt.Errorf("unknown size unit %q", m[2])
	}
	return int64(num * float64(mult)), nil
}

// formatBytes renders byte counts in docker's decimal style ("13.07MB").
func formatBytes(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%dB", n)
	}
	val := float64(n)
	for _, unit := range []string{"kB", "MB", "GB", "TB"} {
		val /= 1000
		if val < 1000 {
			return trimZeros(strconv.FormatFloat(val, 'f', 2, 64)) + unit
		}
	}
	return trimZeros(strconv.FormatFloat(val, 'f', 2, 64)) + "TB"
}

// trimZeros removes trailing zeros from a fixed-point string, then a trailing dot.
func trimZeros(s string) string {
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

// parseReclaimed extracts the "Total reclaimed space: X" value from docker
// prune output, defaulting to "0B" when nothing was reclaimed.
func parseReclaimed(output string) (string, error) {
	m := reclaimedRe.FindStringSubmatch(output)
	if m == nil {
		return "0B", nil
	}
	return strings.TrimSpace(m[1]), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all size/format tests green; the `var _ = strings.TrimSpace` placeholder compiles)

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup types and size parse/format helpers"
```

---

### Task 2: Prune command construction and `docker system df` parser

**Files:**
- Modify: `internal/cleanup/cleanup.go` — add `tengizAppLabel`, `pruneCommand`, `pruneCommands`, `parseSystemDF`
- Modify: `internal/cleanup/cleanup_test.go` — add tests, remove the `strings` placeholder

**Interfaces:**
- Consumes: `cleanup.Options` from Task 1
- Produces: `const tengizAppLabel = "tengiz-app"`, `type pruneCommand struct{ category string; args []string }`, `pruneCommands(opts Options) []pruneCommand`, `parseSystemDF(output string) (*Report, error)` (all unexported; consumed by Task 3)

- [ ] **Step 1: Write the failing tests**

Append to `internal/cleanup/cleanup_test.go` and REMOVE the line `var _ = strings.TrimSpace`:

```go
func TestPruneCommandsDefault(t *testing.T) {
	cmds := pruneCommands(Options{})
	if len(cmds) != 4 {
		t.Fatalf("expected 4 prune commands, got %d", len(cmds))
	}
	got := map[string][]string{}
	for _, c := range cmds {
		got[c.category] = c.args
	}
	if strings.Join(got["containers"], " ") != "container prune -f --filter label!=tengiz-app" {
		t.Errorf("containers args = %v", got["containers"])
	}
	if strings.Join(got["images"], " ") != "image prune -f" {
		t.Errorf("images args = %v, want dangling-only by default", got["images"])
	}
	if strings.Join(got["networks"], " ") != "network prune -f" {
		t.Errorf("networks args = %v", got["networks"])
	}
	if strings.Join(got["build-cache"], " ") != "builder prune -f" {
		t.Errorf("build-cache args = %v", got["build-cache"])
	}
	if _, ok := got["volumes"]; ok {
		t.Error("volumes should not be pruned by default")
	}
}

func TestPruneCommandsAllProtectsTengizImages(t *testing.T) {
	cmds := pruneCommands(Options{All: true})
	for _, c := range cmds {
		if c.category == "images" {
			want := "image prune -af --filter label!=tengiz-app"
			if strings.Join(c.args, " ") != want {
				t.Errorf("images args = %v, want %q", c.args, want)
			}
		}
	}
}

func TestPruneCommandsVolumes(t *testing.T) {
	cmds := pruneCommands(Options{Volumes: true})
	found := false
	for _, c := range cmds {
		if c.category == "volumes" {
			found = true
			if strings.Join(c.args, " ") != "volume prune -f" {
				t.Errorf("volumes args = %v", c.args)
			}
		}
	}
	if !found {
		t.Error("volumes command missing when Volumes=true")
	}
}

func TestParseSystemDFArray(t *testing.T) {
	out := `[{"Type":"Images","TotalCount":6,"Active":4,"Size":"538.1MB","Reclaimable":"280.3MB"},{"Type":"Containers","TotalCount":4,"Active":1,"Size":"10.2MB","Reclaimable":"2.1MB"}]`
	rep, err := parseSystemDF(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(rep.Categories))
	}
	if rep.Categories[0].Type != "Images" || rep.Categories[0].Reclaimable != "280.3MB" {
		t.Errorf("unexpected first category: %+v", rep.Categories[0])
	}
}

func TestParseSystemDFLines(t *testing.T) {
	out := `{"Type":"Images","TotalCount":6,"Active":4,"Size":"538.1MB","Reclaimable":"280.3MB"}
{"Type":"Containers","TotalCount":4,"Active":1,"Size":"10.2MB","Reclaimable":"2.1MB"}`
	rep, err := parseSystemDF(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(rep.Categories))
	}
	if rep.Categories[1].Type != "Containers" {
		t.Errorf("second category Type = %q, want Containers", rep.Categories[1].Type)
	}
}

func TestParseSystemDFEmpty(t *testing.T) {
	rep, err := parseSystemDF("")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Categories) != 0 {
		t.Errorf("expected 0 categories, got %d", len(rep.Categories))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL with `undefined: tengizAppLabel`, `undefined: pruneCommands`, `undefined: parseSystemDF`

- [ ] **Step 3: Add the label constant, prune command builder, and df parser**

Append to `internal/cleanup/cleanup.go`:

```go
// tengizAppLabel is the Docker label applied to Tengiz-managed containers and
// images. Prune filters use it to protect Tengiz resources from removal.
const tengizAppLabel = "tengiz-app"

// pruneCommand is a single docker prune invocation for one resource category.
type pruneCommand struct {
	category string
	args     []string
}

// pruneCommands returns the docker prune commands to run for the given options,
// in docker system prune order (containers → images → networks → build cache → volumes).
func pruneCommands(opts Options) []pruneCommand {
	imageArgs := []string{"image", "prune", "-f"}
	if opts.All {
		imageArgs = []string{"image", "prune", "-af", "--filter", "label!=" + tengizAppLabel}
	}
	cmds := []pruneCommand{
		{category: "containers", args: []string{"container", "prune", "-f", "--filter", "label!=" + tengizAppLabel}},
		{category: "images", args: imageArgs},
		{category: "networks", args: []string{"network", "prune", "-f"}},
		{category: "build-cache", args: []string{"builder", "prune", "-f"}},
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{category: "volumes", args: []string{"volume", "prune", "-f"}})
	}
	return cmds
}

// parseSystemDF parses `docker system df --format '{{json .}}'` output. Newer
// Docker versions emit a single JSON array; older versions emit one JSON object
// per line. Both forms are accepted.
func parseSystemDF(output string) (*Report, error) {
	output = strings.TrimSpace(output)
	rep := &Report{Categories: []Category{}}
	if output == "" {
		return rep, nil
	}
	if strings.HasPrefix(output, "[") {
		var cats []Category
		if err := json.Unmarshal([]byte(output), &cats); err != nil {
			return nil, fmt.Errorf("parse system df: %w", err)
		}
		rep.Categories = cats
		return rep, nil
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c Category
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}
		rep.Categories = append(rep.Categories, c)
	}
	return rep, nil
}
```

Update the import block in `internal/cleanup/cleanup.go` to add `encoding/json`:

```go
import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all Task 1 and Task 2 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: build label-protected docker prune commands and system df parser"
```

---

### Task 3: Cleanup Manager (Prune, Estimate) and output formatters

**Files:**
- Modify: `internal/cleanup/cleanup.go` — add `commandRunner`, `execRunner`, `Manager`, `New`, `Prune`, `Estimate`, `applyCategory`, `FormatResult`, `FormatReport`
- Modify: `internal/cleanup/cleanup_test.go` — add fake runner + Manager and formatter tests

**Interfaces:**
- Consumes: `pruneCommands(opts Options) []pruneCommand`, `parseSystemDF(output string) (*Report, error)`, `parseSize`, `parseReclaimed`, `formatBytes`, `types` `Options`/`Result`/`Report` from Tasks 1-2
- Produces: `cleanup.New() (*Manager, error)`, `(*Manager).Prune(ctx context.Context, opts Options) (*Result, error)`, `(*Manager).Estimate(ctx context.Context) (*Report, error)`, `FormatResult(res *Result, opts Options) string`, `FormatReport(rep *Report) string`. The unexported `type commandRunner interface{ run(ctx context.Context, args ...string) ([]byte, error) }` allows tests to inject a fake.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cleanup/cleanup_test.go`:

```go
type fakeRunner struct {
	responses map[string]string
	calls     []string
	err       error
}

func (f *fakeRunner) run(ctx context.Context, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if f.err != nil {
		return nil, f.err
	}
	out, ok := f.responses[key]
	if !ok {
		out = "Total reclaimed space: 0B"
	}
	return []byte(out), nil
}

func TestManagerPruneAggregates(t *testing.T) {
	m := &Manager{runner: &fakeRunner{responses: map[string]string{
		"container prune -f --filter label!=tengiz-app": "Deleted Containers:\n...\n\nTotal reclaimed space: 50MB",
		"image prune -f":                                "Total reclaimed space: 13.07MB",
		"network prune -f":                              "Total reclaimed space: 0B",
		"builder prune -f":                              "Total reclaimed space: 200.1MB",
	}}}
	res, err := m.Prune(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Containers != "50MB" {
		t.Errorf("Containers = %q, want %q", res.Containers, "50MB")
	}
	if res.Images != "13.07MB" {
		t.Errorf("Images = %q, want %q", res.Images, "13.07MB")
	}
	if res.Networks != "0B" {
		t.Errorf("Networks = %q, want %q", res.Networks, "0B")
	}
	if res.BuildCache != "200.1MB" {
		t.Errorf("BuildCache = %q, want %q", res.BuildCache, "200.1MB")
	}
	if res.Volumes != "" {
		t.Errorf("Volumes = %q, want empty (not selected)", res.Volumes)
	}
	if res.Total != "263.17MB" {
		t.Errorf("Total = %q, want %q", res.Total, "263.17MB")
	}
}

func TestManagerPruneWithVolumes(t *testing.T) {
	m := &Manager{runner: &fakeRunner{responses: map[string]string{
		"volume prune -f": "Total reclaimed space: 1.2GB",
	}}}
	res, err := m.Prune(context.Background(), Options{Volumes: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Volumes != "1.2GB" {
		t.Errorf("Volumes = %q, want %q", res.Volumes, "1.2GB")
	}
	if res.Total != "1.2GB" {
		t.Errorf("Total = %q, want %q", res.Total, "1.2GB")
	}
}

func TestManagerEstimate(t *testing.T) {
	m := &Manager{runner: &fakeRunner{responses: map[string]string{
		"system df --format {{json .}}": `[{"Type":"Images","TotalCount":6,"Active":4,"Size":"538.1MB","Reclaimable":"280.3MB"},{"Type":"Containers","TotalCount":4,"Active":1,"Size":"10.2MB","Reclaimable":"2.1MB"},{"Type":"Volumes","TotalCount":1,"Active":1,"Size":"100MB","Reclaimable":"0B"},{"Type":"Build Cache","TotalCount":15,"Active":0,"Size":"200.1MB","Reclaimable":"200.1MB"}]`,
	}}}
	rep, err := m.Estimate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Categories) != 4 {
		t.Fatalf("expected 4 categories, got %d", len(rep.Categories))
	}
	if rep.Total != "482.5MB" {
		t.Errorf("Total = %q, want %q", rep.Total, "482.5MB")
	}
}

func TestFormatResult(t *testing.T) {
	res := &Result{Containers: "50MB", Images: "13.07MB", Networks: "0B", BuildCache: "200.1MB", Total: "263.17MB"}
	got := FormatResult(res, Options{})
	for _, want := range []string{
		"[tengiz] cleanup complete\n",
		"[tengiz] containers:   reclaimed 50MB\n",
		"[tengiz] images:       reclaimed 13.07MB\n",
		"[tengiz] networks:     reclaimed 0B\n",
		"[tengiz] build cache:  reclaimed 200.1MB\n",
		"[tengiz] total reclaimed: 263.17MB\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatResult() missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "volumes") {
		t.Errorf("FormatResult() should not mention volumes by default: %q", got)
	}
}

func TestFormatResultWithVolumes(t *testing.T) {
	res := &Result{Containers: "50MB", Images: "13.07MB", Networks: "0B", Volumes: "1.2GB", BuildCache: "200.1MB", Total: "1.46GB"}
	got := FormatResult(res, Options{Volumes: true})
	if !strings.Contains(got, "[tengiz] volumes:      reclaimed 1.2GB\n") {
		t.Errorf("FormatResult() missing volumes line: %q", got)
	}
}

func TestFormatReport(t *testing.T) {
	rep := &Report{Categories: []Category{
		{Type: "Images", Reclaimable: "280.3MB"},
		{Type: "Build Cache", Reclaimable: "200.1MB"},
	}, Total: "482.5MB"}
	got := FormatReport(rep)
	for _, want := range []string{
		"[tengiz] dry run: no resources will be removed\n",
		"[tengiz] Images:       280.3MB\n",
		"[tengiz] Build Cache:  200.1MB\n",
		"[tengiz] total reclaimable: 482.5MB\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatReport() missing %q in %q", want, got)
		}
	}
}
```

Update the imports in `internal/cleanup/cleanup_test.go`:

```go
import (
	"context"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL with `undefined: Manager`, `undefined: New`, `undefined: FormatResult`, `undefined: FormatReport` (and `context` unused import error until implementation lands — the imports must be added together with the implementation in Step 3)

- [ ] **Step 3: Add the Manager, runner, and formatters**

Append to `internal/cleanup/cleanup.go`:

```go
// commandRunner abstracts docker execution so Manager logic is testable.
type commandRunner interface {
	run(ctx context.Context, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
}

// Manager prunes unused Docker resources while protecting Tengiz-managed ones.
type Manager struct {
	runner commandRunner
}

// New returns a Manager ready to prune the local Docker daemon.
func New() (*Manager, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &Manager{runner: execRunner{}}, nil
}

// Prune removes unused Docker resources and reports reclaimed space per category.
func (m *Manager) Prune(ctx context.Context, opts Options) (*Result, error) {
	res := &Result{}
	var total int64
	for _, pc := range pruneCommands(opts) {
		out, err := m.runner.run(ctx, pc.args...)
		if err != nil {
			return nil, fmt.Errorf("docker %s: %w\n%s", strings.Join(pc.args, " "), err, string(out))
		}
		reclaimed, err := parseReclaimed(string(out))
		if err != nil {
			return nil, err
		}
		n, err := parseSize(reclaimed)
		if err != nil {
			return nil, fmt.Errorf("%s reclaimed size: %w", pc.category, err)
		}
		total += n
		applyCategory(res, pc.category, reclaimed)
	}
	res.Total = formatBytes(total)
	return res, nil
}

func applyCategory(res *Result, category, reclaimed string) {
	switch category {
	case "containers":
		res.Containers = reclaimed
	case "images":
		res.Images = reclaimed
	case "networks":
		res.Networks = reclaimed
	case "volumes":
		res.Volumes = reclaimed
	case "build-cache":
		res.BuildCache = reclaimed
	}
}

// Estimate reports how much disk space each category could reclaim, without
// removing anything.
func (m *Manager) Estimate(ctx context.Context) (*Report, error) {
	out, err := m.runner.run(ctx, "system", "df", "--format", "{{json .}}")
	if err != nil {
		return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	rep, err := parseSystemDF(string(out))
	if err != nil {
		return nil, err
	}
	var total int64
	for _, c := range rep.Categories {
		n, perr := parseSize(c.Reclaimable)
		if perr != nil {
			continue
		}
		total += n
	}
	rep.Total = formatBytes(total)
	return rep, nil
}

// FormatResult renders a prune run summary for terminal display.
func FormatResult(res *Result, opts Options) string {
	var b strings.Builder
	b.WriteString("[tengiz] cleanup complete\n")
	fmt.Fprintf(&b, "[tengiz] %-14s reclaimed %s\n", "containers:", res.Containers)
	fmt.Fprintf(&b, "[tengiz] %-14s reclaimed %s\n", "images:", res.Images)
	fmt.Fprintf(&b, "[tengiz] %-14s reclaimed %s\n", "networks:", res.Networks)
	if opts.Volumes {
		fmt.Fprintf(&b, "[tengiz] %-14s reclaimed %s\n", "volumes:", res.Volumes)
	}
	fmt.Fprintf(&b, "[tengiz] %-14s reclaimed %s\n", "build cache:", res.BuildCache)
	fmt.Fprintf(&b, "[tengiz] total reclaimed: %s\n", res.Total)
	return b.String()
}

// FormatReport renders a dry-run (estimate) summary for terminal display.
func FormatReport(rep *Report) string {
	var b strings.Builder
	b.WriteString("[tengiz] dry run: no resources will be removed\n")
	b.WriteString("[tengiz] reclaimable disk space by category:\n")
	for _, c := range rep.Categories {
		fmt.Fprintf(&b, "[tengiz] %-14s %s\n", c.Type+":", c.Reclaimable)
	}
	fmt.Fprintf(&b, "[tengiz] total reclaimable: %s\n", rep.Total)
	return b.String()
}
```

Update the import block in `internal/cleanup/cleanup.go` to add `context` and `os/exec`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all Task 1-3 tests, including Manager orchestration and formatters)

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup Manager with label-protected prune and dry-run estimate"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.New() (*Manager, error)`, `(*Manager).Prune(ctx, opts) (*Result, error)`, `(*Manager).Estimate(ctx) (*Report, error)`, `FormatResult(res, opts) string`, `FormatReport(rep) string` from Task 3
- Produces: a registered `tengiz cleanup` command with `--dry-run`, `--all`/`-a`, and `--volumes` flags. No changes to `internal/cli/root.go` are needed (the new file's `init()` registers the command and flags).

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"all", "volumes", "dry-run"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
	if f := flags.Lookup("all"); f.Shorthand != "a" {
		t.Errorf("--all shorthand = %q, want %q", f.Shorthand, "a")
	}
}

func TestCleanupHelpListsFlags(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--help"})
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("cleanup --help failed: %v", err)
	}
	helpText := buf.String()
	for _, flag := range []string{"--dry-run", "--all", "-a", "--volumes"} {
		if !strings.Contains(helpText, flag) {
			t.Errorf("help text missing flag %q", flag)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Removes stopped containers, unused images, unused networks, and the Docker
build cache to reclaim disk space. Tengiz-managed containers (labeled tengiz-app)
and Tengiz-built images are never removed.

Flags:
  --dry-run   show reclaimable space without removing anything
  --all, -a   also remove all unused images, not just dangling ones
  --volumes   also remove unused volumes (opt-in, potentially destructive)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")

		m, err := cleanup.New()
		if err != nil {
			return err
		}

		opts := cleanup.Options{All: all, Volumes: volumes}

		if dryRun {
			rep, err := m.Estimate(cmd.Context())
			if err != nil {
				return fmt.Errorf("cleanup --dry-run: %w", err)
			}
			fmt.Print(cleanup.FormatReport(rep))
			return nil
		}

		res, err := m.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		fmt.Print(cleanup.FormatResult(res, opts))
		return nil
	},
}

func init() {
	cleanupCmd.Flags().BoolP("all", "a", false, "also remove all unused images (not just dangling)")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	cleanupCmd.Flags().Bool("dry-run", false, "show reclaimable space without removing anything")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run the full CLI test suite**

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS (no regressions)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command with dry-run, all, and volumes flags"
```

---

### Task 5: Label Tengiz-built images for `--all` protection

**Files:**
- Modify: `internal/builder/builder.go:69-71` — use new `buildArgs` helper in `buildWithDockerfile`
- Modify: `internal/builder/builder_test.go` — add `buildArgs` tests

**Interfaces:**
- Consumes: nothing new
- Produces: `buildArgs(secretArgs []string, appName, tag, dir string) []string` (unexported); every image built via `docker build` now carries label `tengiz-app=<appName>` so `tengiz cleanup --all` (which filters `label!=tengiz-app`) protects them

- [ ] **Step 1: Write the failing tests**

Append to `internal/builder/builder_test.go`:

```go
func TestBuildArgsAddsTengizLabel(t *testing.T) {
	args := buildArgs(nil, "testapp", "tengiz-apps/testapp:production-v1", "/tmp/app")
	got := strings.Join(args, " ")
	if !strings.Contains(got, "--label tengiz-app=testapp") {
		t.Errorf("buildArgs() missing --label tengiz-app=testapp in %q", got)
	}
	if !strings.Contains(got, "-t tengiz-apps/testapp:production-v1 /tmp/app") {
		t.Errorf("buildArgs() missing tag and dir in %q", got)
	}
}

func TestBuildArgsKeepsSecrets(t *testing.T) {
	args := buildArgs([]string{"--secret", "id=NPM_TOKEN,src=/tmp/token"}, "testapp", "tengiz-apps/testapp:v1", "/app")
	got := strings.Join(args, " ")
	want := "--secret id=NPM_TOKEN,src=/tmp/token --label tengiz-app=testapp -t tengiz-apps/testapp:v1 /app"
	if got != want {
		t.Errorf("buildArgs() = %q, want %q", got, want)
	}
}
```

(`builder_test.go` already imports `strings` and `testing`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/builder/... -run "TestBuildArgs" -v -count=1`

Expected: FAIL with `undefined: buildArgs`

- [ ] **Step 3: Add `buildArgs` and use it in `buildWithDockerfile`**

In `internal/builder/builder.go`, replace the arg-building block inside `buildWithDockerfile` (currently lines 69-71):

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

with:

```go
	args := buildArgs(b.buildSecretArgs(), appName, tag, dir)
```

Then add the helper function at the end of the file (after `generateDockerfile` or next to `buildSecretArgs`):

```go
// buildArgs assembles the docker build invocation. Every Tengiz-built image is
// labeled tengiz-app=<appName> so `tengiz cleanup --all` can protect it.
func buildArgs(secretArgs []string, appName, tag, dir string) []string {
	args := []string{"build"}
	args = append(args, secretArgs...)
	args = append(args, "--label", fmt.Sprintf("tengiz-app=%s", appName))
	args = append(args, "-t", tag, dir)
	return args
}
```

Note: `fmt` is already imported in `builder.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/builder/... -v -count=1`

Expected: PASS (all builder tests, including `TestBuildArgsAddsTengizLabel` and `TestBuildArgsKeepsSecrets`)

- [ ] **Step 5: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: label tengiz-built images so cleanup --all protects them"
```

---

### Task 6: Documentation and final verification

**Files:**
- Modify: `README.md` — add cleanup to the Features list and CLI Reference
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 Docker Housekeeping as implemented

**Interfaces:**
- Consumes: all prior tasks (the feature is complete)
- Produces: up-to-date user documentation; FUTURES_FEATURES.md accurately reflects the implemented feature

- [ ] **Step 1: Add cleanup to the README Features list**

In `README.md`, after the "Self-contained" bullet (line 23) and before `## Prerequisites` (line 25), insert:

```markdown
- **Docker housekeeping** — `tengiz cleanup` reclaims disk space while protecting Tengiz-managed containers and images.
```

- [ ] **Step 2: Add the cleanup command to the README CLI Reference**

In `README.md`, after the `#### tengiz volume list <app>` section (which ends at line 303) and before `### tengiz preview` (line 304), insert:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show reclaimable space without removing anything |
| `-a`, `--all` | Also remove all unused images, not just dangling ones |
| `--volumes` | Also remove unused volumes (opt-in) |

Removes stopped containers (except Tengiz-managed containers labeled `tengiz-app`), dangling images, unused networks, and the Docker build cache. Run with `--all` to also remove unused images and `--volumes` to also remove unused volumes. Use `--dry-run` to preview what would be reclaimed. Images built by Tengiz are labeled `tengiz-app` at build time and protected from `--all` pruning. Nixpacks-built images do not carry the label, so `--all` may prune older Nixpacks deployment images not referenced by a running container.
```

- [ ] **Step 3: Update `docs/FUTURES_FEATURES.md` Priority Ranking**

Change row #6 in the P0 table from `⬜` to `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 4: Add an entry to the Implemented Features table**

In `docs/FUTURES_FEATURES.md`, in the `✅ Implemented Features (Not Pending)` table (near line 241), add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-07-31) |
```

- [ ] **Step 5: Add a Status line to the feature description**

In `docs/FUTURES_FEATURES.md`, in the `## Docker Housekeeping (Otomatik Temizlik)` section (currently has `- **Detected:** 2026-07-14`), add a Status line right after the description:

```markdown
- **Status:** ✅ Implemented (2026-07-31)
```

- [ ] **Step 6: Run the full test suite and vet**

Run: `go test ./... -v -count=1`

Expected: All PASS

Run: `go vet ./...`

Expected: No issues

Run: `go build -o tengiz .`

Expected: Build succeeds

- [ ] **Step 7: Manual smoke test (if docker is available)**

```bash
./tengiz cleanup --dry-run
```

Expected: prints `[tengiz] dry run: no resources will be removed` plus per-category reclaimable lines.

```bash
./tengiz cleanup
```

Expected: prints `[tengiz] cleanup complete` plus per-category reclaimed lines and a total.

If docker is not installed, skip this step — the unit tests cover the logic.

- [ ] **Step 8: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** (from `docs/FUTURES_FEATURES.md`, feature #6):
- `tengiz cleanup` command → Task 4 ✅
- Label-based `docker system prune` semantics (label protection for Tengiz resources) → Tasks 2-3 (`label!=tengiz-app` filters on containers and `--all` images) ✅
- Unused volume, network, container, and image cleanup → Tasks 2-3 (per-category prune commands; volumes opt-in via `--volumes`) ✅
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" (Tengiz-managed containers protected) → Task 2 container filter + Task 5 image label ✅

**2. Placeholder scan:** No TBD/TODO/"implement later"/"add error handling" steps. Every step has complete code. No "Similar to Task N" references — the formatter tests repeat full assertions rather than deferring.

**3. Type consistency:**
- `cleanup.Options{All, Volumes bool}` — defined Task 1, consumed in Tasks 2-4, used consistently
- `cleanup.Result{Containers, Images, Networks, Volumes, BuildCache, Total string}` — defined Task 1, fields written by `applyCategory` in Task 3, printed by `FormatResult` in Task 3
- `pruneCommands(opts Options) []pruneCommand` with `pruneCommand.category` ∈ `{"containers", "images", "networks", "volumes", "build-cache"}` — Task 2 builds, Task 3 `applyCategory` switches on the same keys
- `parseSize`/`formatBytes` round-trip: `formatBytes(13070000)` = `"13.07MB"` and `parseSize("13.07MB")` = `13070000` — verified in Task 1 tests
- `New() (*Manager, error)` — Task 3 produces, Task 4 consumes the same signature
- `FormatResult(res *Result, opts Options) string` and `FormatReport(rep *Report) string` — Task 3 defines, Task 4 calls with matching argument order
- `buildArgs(secretArgs []string, appName, tag, dir string) []string` — Task 5 defines and uses in one place
