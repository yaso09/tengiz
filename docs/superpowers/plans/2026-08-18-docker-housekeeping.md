# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped non-Tengiz containers, dangling images, unused networks, build cache, and — only on explicit opt-in — unused volumes) with label-based protection so Tengiz-managed containers and rollback images are never removed.

**Architecture:** A new `internal/housekeeping` package wraps the `docker` CLI via `os/exec` (same pattern as `internal/runtime/docker.go`), exposing a `Manager` interface with `DiskUsage` and `Prune` methods. Safety is enforced at the command level: container pruning uses the `label!=tengiz-app` negation filter (protects scale-to-zero stopped containers and versioned deploy containers), image pruning removes only dangling images (`docker image prune`, no `-a`), networks/cache are inherently safe, and volumes require an explicit `--volumes` flag. The CLI command is dry-run by default and performs deletion only with `--apply`. All Docker command construction and output parsing are pure functions so they can be unit-tested; exec paths are tested against a fake `docker` binary injected via `PATH`.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` for the `docker` CLI, existing `internal/cli` + `internal/runtime` patterns. No new external dependencies.

## Global Constraints

- Docker CLI must be installed (already a repo prerequisite) — cleanup calls `docker` via `os/exec`
- Docker CLI **20.10+** required for negated label filters (`label!=tengiz-app`)
- Never remove running containers; never remove containers with the `tengiz-app` label (includes scale-to-zero stopped containers and `tengiz-deployment` versioned containers)
- Never remove tagged (non-dangling) images — keeps rollback images (`tengiz-apps/<app>:<env>-<id>`) intact
- Volumes are **never** pruned unless the user passes `--volumes` (data risk)
- `tengiz cleanup` is dry-run by default; deletion happens only with `--apply`
- Cleanup is host-global and env-agnostic — it does NOT read `--env` and does not touch `~/.tengiz/` state
- Output uses the existing `[tengiz]` prefix convention; new package is `internal/housekeeping`
- No new external Go dependencies; existing tests must continue to pass without modification
- New exec-based tests MUST NOT use `t.Parallel()` because they mutate `PATH` via `t.Setenv`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/housekeeping/housekeeping.go` | `Category` constants, `DefaultCategories`, `Options`, `Usage`, `Candidate`, `PruneResult`, `Manager` interface, `NewStub()` |
| `internal/housekeeping/housekeeping_test.go` | Stub + interface + default-category tests |
| `internal/housekeeping/parse.go` | Pure output-parsing helpers: `parseSize`, `splitNumberUnit`, `parseDfOutput`, `parseReclaimed`, `parseCandidates` |
| `internal/housekeeping/parse_test.go` | Tests for all parse helpers |
| `internal/housekeeping/docker.go` | Docker impl: `NewDocker`, `dockerManager`, pure arg builders (`pruneArgs`, `containerCandidatesArgs`, `imageCandidatesArgs`, `networkCandidatesArgs`), `DiskUsage`, `Prune`, `listCandidates` |
| `internal/housekeeping/docker_test.go` | Arg-builder tests + fake-docker tests for `DiskUsage`/`Prune`/dry-run |
| `internal/cli/cleanup.go` | `cleanupCmd` cobra command + `printUsage`/`formatBytes`/`cleanupCategories` helpers |
| `internal/cli/root.go` | Register `cleanupCmd` and its flags in `init()` |
| `internal/cli/cleanup_test.go` | Command registration, flag registration, `formatBytes` tests |
| `README.md` | Add `tengiz cleanup` to Features + CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |
| `AGENTS.md` | Add `tengiz cleanup` to the Commands section |

New package `internal/housekeeping` (6 new Go files incl. tests), 2 new CLI files, 4 modified docs. No changes to existing Go files except `internal/cli/root.go` (two registration lines).

---

### Task 1: housekeeping package core (types, interface, stub)

**Files:**
- Create: `internal/housekeeping/housekeeping.go`
- Create: `internal/housekeeping/housekeeping_test.go`

**Interfaces:**
- Consumes: nothing new (only stdlib `context`)
- Produces: `Category` constants (`CategoryContainers`, `CategoryImages`, `CategoryNetworks`, `CategoryCache`, `CategoryVolumes`), `DefaultCategories []Category`, `Options{Categories []Category; Apply bool}`, `Usage{ContainersReclaimable, ImagesReclaimable, VolumesReclaimable, CacheReclaimable int64}`, `Candidate{Category Category; ID, Name string}`, `PruneResult{Applied bool; Candidates []Candidate; ReclaimedBytes int64; ReclaimedByCategory map[Category]int64}`, `Manager` interface with `DiskUsage(ctx context.Context) (*Usage, error)` and `Prune(ctx context.Context, opts Options) (*PruneResult, error)`, `NewStub() Manager`

- [ ] **Step 1: Write the failing test**

```go
// internal/housekeeping/housekeeping_test.go
package housekeeping

import (
	"context"
	"testing"
)

func TestNewStub(t *testing.T) {
	m := NewStub()
	if m == nil {
		t.Fatal("NewStub() returned nil")
	}
}

func TestStubSatisfiesInterface(t *testing.T) {
	var iface Manager = NewStub()
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	u, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if u.ContainersReclaimable != 0 || u.ImagesReclaimable != 0 {
		t.Errorf("expected zero usage, got %+v", u)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), Options{Apply: true, Categories: DefaultCategories})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !res.Applied {
		t.Errorf("expected Applied=true, got false")
	}
}

func TestDefaultCategories(t *testing.T) {
	if len(DefaultCategories) != 4 {
		t.Fatalf("expected 4 default categories, got %d", len(DefaultCategories))
	}
	for _, c := range DefaultCategories {
		if c == CategoryVolumes {
			t.Errorf("volumes must not be in DefaultCategories")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/housekeeping/... -run "TestNewStub|TestStubSatisfiesInterface|TestStubDiskUsage|TestStubPrune|TestDefaultCategories" -v -count=1`

Expected: FAIL — `go: no packages to test` because `internal/housekeeping` does not exist yet.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/housekeeping/housekeeping.go
package housekeeping

import "context"

type Category string

const (
	CategoryContainers Category = "containers"
	CategoryImages     Category = "images"
	CategoryNetworks   Category = "networks"
	CategoryCache      Category = "cache"
	CategoryVolumes    Category = "volumes"
)

var DefaultCategories = []Category{
	CategoryContainers,
	CategoryImages,
	CategoryNetworks,
	CategoryCache,
}

type Options struct {
	Categories []Category
	Apply      bool
}

type Usage struct {
	ContainersReclaimable int64
	ImagesReclaimable     int64
	VolumesReclaimable    int64
	CacheReclaimable      int64
}

type Candidate struct {
	Category Category
	ID       string
	Name     string
}

type PruneResult struct {
	Applied             bool
	Candidates          []Candidate
	ReclaimedBytes      int64
	ReclaimedByCategory map[Category]int64
}

type Manager interface {
	DiskUsage(ctx context.Context) (*Usage, error)
	Prune(ctx context.Context, opts Options) (*PruneResult, error)
}

type stubManager struct{}

func NewStub() Manager {
	return &stubManager{}
}

func (m *stubManager) DiskUsage(ctx context.Context) (*Usage, error) {
	return &Usage{}, nil
}

func (m *stubManager) Prune(ctx context.Context, opts Options) (*PruneResult, error) {
	return &PruneResult{Applied: opts.Apply}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/housekeeping/... -v -count=1`

Expected: PASS (all 5 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/housekeeping/housekeeping.go internal/housekeeping/housekeeping_test.go
git commit -m "feat: add housekeeping package types and Manager interface"
```

---

### Task 2: output parsing helpers

**Files:**
- Create: `internal/housekeeping/parse.go`
- Create: `internal/housekeeping/parse_test.go`

**Interfaces:**
- Consumes: `Category` from Task 1
- Produces:
  - `parseSize(s string) (int64, error)` — parses Docker human sizes (`1.5MB`, `23.5kB`, `1TiB`, bare bytes). SI base-1000 for `kB`/`MB`/`GB`/`TB`/`PB`, binary base-1024 for `KiB`/`MiB`/`GiB`/`TiB`. Used by `parseDfOutput`, `parseReclaimed`.
  - `parseDfOutput(output string) (*Usage, error)` — parses `docker system df` table into `Usage`.
  - `parseReclaimed(out string) (int64, error)` — extracts the number from a prune command's `Total reclaimed space: X` line.
  - `parseCandidates(out string, cat Category) []Candidate` — parses `ID Name` lines into `Candidate` entries.
  - `splitNumberUnit(s string) (number, unit string)` — internal helper used by `parseSize`.

- [ ] **Step 1: Write the failing test**

```go
// internal/housekeeping/parse_test.go
package housekeeping

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0", 0},
		{"0B", 0},
		{"512", 512},
		{"1kB", 1000},
		{"1.5MB", 1500000},
		{"2GB", 2000000000},
		{"1TB", 1000000000000},
		{"1KiB", 1024},
		{"10.49GB", 10490000000},
	}
	for _, tt := range tests {
		got, err := parseSize(tt.in)
		if err != nil {
			t.Fatalf("parseSize(%q) error = %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseSizeInvalid(t *testing.T) {
	if _, err := parseSize("12.5.5GB"); err == nil {
		t.Fatal("expected error for malformed size")
	}
}

func TestParseDfOutput(t *testing.T) {
	output := `TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE
Images          6         1         1.234GB   1.1GB (89%)
Containers      3         1         23.5kB    12.5kB (53%)
Local Volumes   1         1         12MB      0B (0%)
Build Cache     12        0         45.6MB    45.6MB
`
	u, err := parseDfOutput(output)
	if err != nil {
		t.Fatalf("parseDfOutput error = %v", err)
	}
	if u.ContainersReclaimable != 12500 {
		t.Errorf("ContainersReclaimable = %d, want 12500", u.ContainersReclaimable)
	}
	if u.ImagesReclaimable != 1100000000 {
		t.Errorf("ImagesReclaimable = %d, want 1100000000", u.ImagesReclaimable)
	}
	if u.VolumesReclaimable != 0 {
		t.Errorf("VolumesReclaimable = %d, want 0", u.VolumesReclaimable)
	}
	if u.CacheReclaimable != 45600000 {
		t.Errorf("CacheReclaimable = %d, want 45600000", u.CacheReclaimable)
	}
}

func TestParseDfOutputEmpty(t *testing.T) {
	u, err := parseDfOutput("")
	if err != nil {
		t.Fatalf("parseDfOutput error = %v", err)
	}
	if u.ImagesReclaimable != 0 {
		t.Errorf("expected zero usage, got %+v", u)
	}
}

func TestParseReclaimed(t *testing.T) {
	out := "Deleted Containers:\nabc123\n\nTotal reclaimed space: 1.25MB\n"
	got, err := parseReclaimed(out)
	if err != nil {
		t.Fatalf("parseReclaimed error = %v", err)
	}
	if got != 1250000 {
		t.Errorf("parseReclaimed = %d, want 1250000", got)
	}
}

func TestParseReclaimedNotFound(t *testing.T) {
	if _, err := parseReclaimed("nothing here\n"); err == nil {
		t.Fatal("expected error when 'Total reclaimed space' line is missing")
	}
}

func TestParseCandidates(t *testing.T) {
	out := "8f2a1bc9 nginx-proxy\n7d3e4f5a redis\n"
	cands := parseCandidates(out, CategoryContainers)
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(cands))
	}
	if cands[0].ID != "8f2a1bc9" || cands[0].Name != "nginx-proxy" {
		t.Errorf("candidate[0] = %+v", cands[0])
	}
	if cands[1].Category != CategoryContainers {
		t.Errorf("candidate[1].Category = %q, want %q", cands[1].Category, CategoryContainers)
	}
}

func TestParseCandidatesEmpty(t *testing.T) {
	if cands := parseCandidates("", CategoryImages); len(cands) != 0 {
		t.Errorf("expected no candidates, got %d", len(cands))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/housekeeping/... -run "TestParseSize|TestParseDfOutput|TestParseReclaimed|TestParseCandidates" -v -count=1`

Expected: FAIL — `undefined: parseSize`, `undefined: parseDfOutput`, `undefined: parseReclaimed`, `undefined: parseCandidates`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/housekeeping/parse.go
package housekeeping

import (
	"fmt"
	"strconv"
	"strings"
)

var sizeUnits = map[string]int64{
	"B":  1,
	"kB": 1000,
	"KB": 1024,
	"MB": 1000 * 1000,
	"GB": 1000 * 1000 * 1000,
	"TB": 1000 * 1000 * 1000 * 1000,
	"PB": 1000 * 1000 * 1000 * 1000 * 1000,
	"KiB": 1024,
	"MiB": 1024 * 1024,
	"GiB": 1024 * 1024 * 1024,
	"TiB": 1024 * 1024 * 1024 * 1024,
}

// longest-first so "1.5MB" matches "MB", not "B"
var unitOrder = []string{"TiB", "GiB", "MiB", "KiB", "PB", "TB", "GB", "MB", "kB", "KB", "B"}

func splitNumberUnit(s string) (string, string) {
	for _, u := range unitOrder {
		if strings.HasSuffix(s, u) {
			return strings.TrimSpace(strings.TrimSuffix(s, u)), u
		}
	}
	return strings.TrimSpace(s), ""
}

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" || s == "0B" {
		return 0, nil
	}
	numStr, unit := splitNumberUnit(s)
	mult, ok := sizeUnits[unit]
	if !ok {
		num, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("parse size %q: %w", s, err)
		}
		return int64(num), nil
	}
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("parse size %q: %w", s, err)
	}
	return int64(num * float64(mult)), nil
}

func isPureInt(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseDfOutput(output string) (*Usage, error) {
	usage := &Usage{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		// The type name may span multiple words ("Build Cache", "Local Volumes");
		// it ends where the first integer column (TOTAL) begins.
		typeTokens := []string{fields[0]}
		i := 1
		for i < len(fields) && !isPureInt(fields[i]) {
			typeTokens = append(typeTokens, fields[i])
			i++
		}
		// remaining fields: TOTAL ACTIVE SIZE [RECLAIMABLE [(PCT%)]]
		rest := fields[i:]
		if len(rest) < 3 {
			continue
		}
		reclaimable := rest[len(rest)-1]
		if strings.HasPrefix(reclaimable, "(") {
			reclaimable = rest[len(rest)-2]
		}
		typ := strings.Join(typeTokens, " ")
		switch {
		case strings.Contains(typ, "Containers"):
			v, err := parseSize(reclaimable)
			if err != nil {
				return nil, err
			}
			usage.ContainersReclaimable = v
		case strings.Contains(typ, "Images"):
			v, err := parseSize(reclaimable)
			if err != nil {
				return nil, err
			}
			usage.ImagesReclaimable = v
		case strings.Contains(typ, "Volumes"):
			v, err := parseSize(reclaimable)
			if err != nil {
				return nil, err
			}
			usage.VolumesReclaimable = v
		case strings.Contains(typ, "Cache"):
			v, err := parseSize(reclaimable)
			if err != nil {
				return nil, err
			}
			usage.CacheReclaimable = v
		}
	}
	return usage, nil
}

func parseReclaimed(out string) (int64, error) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			return parseSize(val)
		}
	}
	return 0, fmt.Errorf("no 'Total reclaimed space' line in prune output")
}

func parseCandidates(out string, cat Category) []Candidate {
	var cands []Candidate
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		id := fields[0]
		name := ""
		if len(fields) == 2 {
			name = fields[1]
		}
		cands = append(cands, Candidate{Category: cat, ID: id, Name: name})
	}
	return cands
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/housekeeping/... -v -count=1`

Expected: PASS (all tests including Task 1's)

- [ ] **Step 5: Commit**

```bash
git add internal/housekeeping/parse.go internal/housekeeping/parse_test.go
git commit -m "feat: add docker output parsing helpers for housekeeping"
```

---

### Task 3: Docker command arg builders

**Files:**
- Create: `internal/housekeeping/docker.go`
- Create: `internal/housekeeping/docker_test.go`

**Interfaces:**
- Consumes: `Category`, `Candidate`, `parseCandidates` from Tasks 1-2
- Produces (used by Task 4 and by the CLI):
  - `const tengizLabelKey = "tengiz-app"`
  - `pruneArgs(cat Category) ([]string, error)` — returns the full `docker` subcommand args for a category, e.g. `[]string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}`
  - `containerCandidatesArgs() []string` — `docker ps -a` args listing stopped non-Tengiz containers
  - `imageCandidatesArgs() []string` — `docker images` args listing dangling images
  - `networkCandidatesArgs() []string` — `docker network ls` args listing unused networks
  - `type dockerManager struct{}` (empty struct, defined here; methods added in Task 4)

- [ ] **Step 1: Write the failing test**

```go
// internal/housekeeping/docker_test.go
package housekeeping

import (
	"reflect"
	"strings"
	"testing"
)

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		cat      Category
		expected []string
	}{
		{CategoryContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{CategoryImages, []string{"image", "prune", "-f"}},
		{CategoryNetworks, []string{"network", "prune", "-f"}},
		{CategoryCache, []string{"builder", "prune", "-f"}},
		{CategoryVolumes, []string{"volume", "prune", "-f"}},
	}
	for _, tt := range tests {
		got, err := pruneArgs(tt.cat)
		if err != nil {
			t.Fatalf("pruneArgs(%s) error = %v", tt.cat, err)
		}
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("pruneArgs(%s) = %v, want %v", tt.cat, got, tt.expected)
		}
	}
}

func TestPruneArgsUnknownCategory(t *testing.T) {
	if _, err := pruneArgs(Category("bogus")); err == nil {
		t.Fatal("expected error for unknown category")
	}
}

func TestContainerCandidatesArgsProtectTengiz(t *testing.T) {
	args := containerCandidatesArgs()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "label!=tengiz-app") {
		t.Errorf("container candidates must exclude tengiz-app label, got: %v", args)
	}
	if !strings.Contains(joined, "status=exited") {
		t.Errorf("container candidates must target stopped containers, got: %v", args)
	}
}

func TestImageCandidatesArgsDanglingOnly(t *testing.T) {
	args := imageCandidatesArgs()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "dangling=true") {
		t.Errorf("image candidates must target dangling images, got: %v", args)
	}
}

func TestNetworkCandidatesArgsUnusedOnly(t *testing.T) {
	args := networkCandidatesArgs()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "dangling=true") {
		t.Errorf("network candidates must target unused networks, got: %v", args)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/housekeeping/... -run "TestPruneArgs|TestContainerCandidatesArgsProtectTengiz|TestImageCandidatesArgsDanglingOnly|TestNetworkCandidatesArgsUnusedOnly" -v -count=1`

Expected: FAIL — `undefined: pruneArgs`, `undefined: containerCandidatesArgs`, `undefined: imageCandidatesArgs`, `undefined: networkCandidatesArgs`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/housekeeping/docker.go
package housekeeping

import (
	"fmt"
)

const tengizLabelKey = "tengiz-app"

type dockerManager struct{}

func pruneArgs(cat Category) ([]string, error) {
	switch cat {
	case CategoryContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=" + tengizLabelKey}, nil
	case CategoryImages:
		return []string{"image", "prune", "-f"}, nil
	case CategoryNetworks:
		return []string{"network", "prune", "-f"}, nil
	case CategoryCache:
		return []string{"builder", "prune", "-f"}, nil
	case CategoryVolumes:
		return []string{"volume", "prune", "-f"}, nil
	}
	return nil, fmt.Errorf("unknown category %q", cat)
}

func containerCandidatesArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", "label!=" + tengizLabelKey,
		"--format", "{{.ID}} {{.Names}}",
	}
}

func imageCandidatesArgs() []string {
	return []string{
		"images",
		"-f", "dangling=true",
		"--format", "{{.ID}} {{.Repository}}:{{.Tag}}",
	}
}

func networkCandidatesArgs() []string {
	return []string{
		"network", "ls",
		"--filter", "dangling=true",
		"--format", "{{.ID}} {{.Name}}",
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/housekeeping/... -v -count=1`

Expected: PASS (all tests from Tasks 1-3)

- [ ] **Step 5: Commit**

```bash
git add internal/housekeeping/docker.go internal/housekeeping/docker_test.go
git commit -m "feat: add docker prune and candidate-list arg builders"
```

---

### Task 4: Docker Manager implementation (`DiskUsage`, `Prune`)

**Files:**
- Modify: `internal/housekeeping/docker.go` (append methods + `NewDocker`)
- Modify: `internal/housekeeping/docker_test.go` (append fake-docker exec tests)

**Interfaces:**
- Consumes: `Options`, `Usage`, `PruneResult`, `Manager`, `DefaultCategories`, `parseCandidates`, `parseDfOutput`, `parseReclaimed`, `pruneArgs`, `containerCandidatesArgs`, `imageCandidatesArgs`, `networkCandidatesArgs` from Tasks 1-3
- Produces: `NewDocker() (Manager, error)` — errors if `docker` not in PATH. `dockerManager` implements `Manager` fully. Behavior:
  - `DiskUsage(ctx)` runs `docker system df`, returns `*Usage` parsed via `parseDfOutput`.
  - `Prune(ctx, opts)` — `opts.Categories` empty defaults to `DefaultCategories`; `Apply=false` lists candidates (containers/images/networks; cache reports nothing but is included in the df summary the CLI prints separately); `Apply=true` runs each category's prune command and accumulates `ReclaimedBytes` + `ReclaimedByCategory` from `Total reclaimed space: X` output.

- [ ] **Step 1: Write the failing test**

```go
// append to internal/housekeeping/docker_test.go
package housekeeping

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeDocker writes an executable `docker` shim into a temp dir and prepends it
// to PATH. The shim runs `script` for every invocation. Do NOT call t.Parallel().
func fakeDocker(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

const dfScript = `#!/bin/sh
if [ "$1" = "system" ] && [ "$2" = "df" ]; then
  printf '%s\n' \
    'TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE' \
    'Images          6         1         1.234GB   1.1GB (89%)' \
    'Containers      3         1         23.5kB    12.5kB (53%)' \
    'Local Volumes   1         1         12MB      0B (0%)' \
    'Build Cache     12        0         45.6MB    45.6MB'
  exit 0
fi
exit 1
`

const pruneScript = `#!/bin/sh
case "$1" in
  container|image|network|builder|volume)
    printf 'Total reclaimed space: 1.25MB\n'
    exit 0
    ;;
esac
exit 1
`

const listScript = `#!/bin/sh
case "$1" in
  ps)
    printf '8f2a1bc9 nginx-proxy\n'
    exit 0
    ;;
  images)
    printf '7d3e4f5a <none>:<none>\n'
    exit 0
    ;;
  network)
    printf '9c0b2da1 bridge-net\n'
    exit 0
    ;;
esac
exit 1
`

func TestNewDockerFindsFakeBinary(t *testing.T) {
	fakeDocker(t, "#!/bin/sh\nexit 0\n")
	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	if m == nil {
		t.Fatal("NewDocker() returned nil")
	}
}

func TestDockerDiskUsage(t *testing.T) {
	fakeDocker(t, dfScript)
	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	u, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if u.ContainersReclaimable != 12500 {
		t.Errorf("ContainersReclaimable = %d, want 12500", u.ContainersReclaimable)
	}
	if u.ImagesReclaimable != 1100000000 {
		t.Errorf("ImagesReclaimable = %d, want 1100000000", u.ImagesReclaimable)
	}
	if u.CacheReclaimable != 45600000 {
		t.Errorf("CacheReclaimable = %d, want 45600000", u.CacheReclaimable)
	}
}

func TestDockerPruneApply(t *testing.T) {
	fakeDocker(t, pruneScript)
	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	res, err := m.Prune(context.Background(), Options{Apply: true, Categories: []Category{CategoryContainers, CategoryImages}})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !res.Applied {
		t.Error("expected Applied=true")
	}
	if res.ReclaimedBytes != 2500000 {
		t.Errorf("ReclaimedBytes = %d, want 2500000", res.ReclaimedBytes)
	}
	if res.ReclaimedByCategory[CategoryContainers] != 1250000 {
		t.Errorf("ReclaimedByCategory[containers] = %d, want 1250000", res.ReclaimedByCategory[CategoryContainers])
	}
}

func TestDockerPruneDryRun(t *testing.T) {
	fakeDocker(t, listScript)
	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	res, err := m.Prune(context.Background(), Options{Apply: false})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Applied {
		t.Error("expected Applied=false for dry run")
	}
	if len(res.Candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d: %+v", len(res.Candidates), res.Candidates)
	}
	if res.Candidates[0].ID != "8f2a1bc9" || res.Candidates[0].Name != "nginx-proxy" {
		t.Errorf("candidate[0] = %+v", res.Candidates[0])
	}
}

func TestDockerPruneDefaultsToAllSafeCategories(t *testing.T) {
	fakeDocker(t, pruneScript)
	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	res, err := m.Prune(context.Background(), Options{Apply: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	// DefaultCategories excludes volumes, so 4 prune commands run
	if len(res.ReclaimedByCategory) != 4 {
		t.Errorf("expected 4 pruned categories, got %d: %+v", len(res.ReclaimedByCategory), res.ReclaimedByCategory)
	}
	if _, pruned := res.ReclaimedByCategory[CategoryVolumes]; pruned {
		t.Error("volumes must never be pruned by default")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/housekeeping/... -run "TestNewDockerFindsFakeBinary|TestDockerDiskUsage|TestDockerPruneApply|TestDockerPruneDryRun|TestDockerPruneDefaultsToAllSafeCategories" -v -count=1`

Expected: FAIL — `undefined: NewDocker`, `undefined: (*dockerManager).DiskUsage`, `undefined: (*dockerManager).Prune`

- [ ] **Step 3: Write minimal implementation**

Update the import block in `internal/housekeeping/docker.go` from:

```go
import (
	"fmt"
)
```

to:

```go
import (
	"context"
	"fmt"
	"os/exec"
)
```

Then append to `internal/housekeeping/docker.go` (no duplicate import block — `fmt` is already imported):

```go
func NewDocker() (Manager, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerManager{}, nil
}

func (m *dockerManager) DiskUsage(ctx context.Context) (*Usage, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseDfOutput(string(out))
}

func (m *dockerManager) Prune(ctx context.Context, opts Options) (*PruneResult, error) {
	cats := opts.Categories
	if len(cats) == 0 {
		cats = DefaultCategories
	}

	result := &PruneResult{
		Applied:             opts.Apply,
		ReclaimedByCategory: make(map[Category]int64),
	}

	if !opts.Apply {
		for _, cat := range cats {
			switch cat {
			case CategoryContainers:
				cands, err := m.listCandidates(ctx, cat, containerCandidatesArgs())
				if err != nil {
					return nil, err
				}
				result.Candidates = append(result.Candidates, cands...)
			case CategoryImages:
				cands, err := m.listCandidates(ctx, cat, imageCandidatesArgs())
				if err != nil {
					return nil, err
				}
				result.Candidates = append(result.Candidates, cands...)
			case CategoryNetworks:
				cands, err := m.listCandidates(ctx, cat, networkCandidatesArgs())
				if err != nil {
					return nil, err
				}
				result.Candidates = append(result.Candidates, cands...)
			case CategoryCache:
				// build cache cannot be enumerated cheaply; the CLI reports
				// its reclaimable bytes from DiskUsage
			}
		}
		return result, nil
	}

	for _, cat := range cats {
		args, err := pruneArgs(cat)
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
		}
		if reclaimed, perr := parseReclaimed(string(out)); perr == nil {
			result.ReclaimedBytes += reclaimed
			result.ReclaimedByCategory[cat] = reclaimed
		}
	}
	return result, nil
}

func (m *dockerManager) listCandidates(ctx context.Context, cat Category, args []string) ([]Candidate, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return parseCandidates(string(out), cat), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/housekeeping/... -v -count=1`

Expected: PASS (all tests including the new fake-docker exec tests)

- [ ] **Step 5: Run the full suite to confirm nothing broke**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: build OK, vet clean, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/housekeeping/docker.go internal/housekeeping/docker_test.go
git commit -m "feat: implement docker housekeeping DiskUsage and Prune"
```

---

### Task 5: CLI command `tengiz cleanup`

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go` — add `rootCmd.AddCommand(cleanupCmd)` and its flags in `init()`

**Interfaces:**
- Consumes: `housekeeping.NewDocker`, `housekeeping.Manager`, `housekeeping.Options`, `housekeeping.Category`, `housekeeping.DefaultCategories` from Tasks 1-4
- Produces: `cleanupCmd` cobra command (package-level var) and helpers `printUsage(u *housekeeping.Usage)`, `formatBytes(b int64) string`, and `cleanupCategories(wantContainers, wantImages, wantNetworks, wantCache, includeVolumes bool) []housekeeping.Category` (all package-private, reused only within `internal/cli`). `--volumes` ADDS volumes to the resulting category set; it never replaces the selected set.

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cleanup_test.go
package cli

import "testing"

func TestCleanupCommandRegistered(t *testing.T) {
	cmd := findSubcommand(rootCmd, "cleanup")
	if cmd == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	cmd := findSubcommand(rootCmd, "cleanup")
	if cmd == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
	for _, flag := range []string{"apply", "df", "volumes", "containers", "images", "networks", "cache"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("cleanup flag --%s not registered", flag)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1250000, "1.25 MB"},
		{2000000000, "2.00 GB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.in)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCleanupCategories(t *testing.T) {
	defaults := cleanupCategories(false, false, false, false, false)
	if len(defaults) != 4 {
		t.Fatalf("expected 4 default categories, got %d: %v", len(defaults), defaults)
	}
	withVolumes := cleanupCategories(false, false, false, false, true)
	if len(withVolumes) != 5 {
		t.Fatalf("expected 5 categories with --volumes, got %d: %v", len(withVolumes), withVolumes)
	}
	specific := cleanupCategories(true, false, false, false, false)
	if len(specific) != 1 || specific[0] != "containers" {
		t.Fatalf("expected only containers, got %v", specific)
	}
	specificWithVolumes := cleanupCategories(true, false, false, false, true)
	if len(specificWithVolumes) != 2 {
		t.Fatalf("expected containers + volumes, got %v", specificWithVolumes)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupFlagsRegistered|TestFormatBytes|TestCleanupCategories" -v -count=1`

Expected: FAIL — `undefined: formatBytes`, `undefined: cleanupCategories`, and the registration tests fail (cleanup command not registered)

- [ ] **Step 3: Write the CLI command**

```go
// internal/cli/cleanup.go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/housekeeping"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prune unused Docker resources on the host. Defaults to a dry run —
lists what would be removed without deleting anything. Use --apply to delete.

Tengiz-managed containers (tengiz-app label) are always protected, including
scale-to-zero stopped containers. Tagged Tengiz images (needed for rollback)
are never removed. Volumes are never touched unless --volumes is passed.

Categories (default: containers, images, networks, cache):
  --containers   remove stopped non-Tengiz containers
  --images       remove dangling (untagged) images
  --networks     remove unused networks
  --cache        remove build cache
  --volumes      also remove unused volumes (DATA RISK — adds to the set,
                 never enabled by default)

Use --df to print a disk usage summary and exit.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		apply, _ := cmd.Flags().GetBool("apply")
		dfOnly, _ := cmd.Flags().GetBool("df")
		includeVolumes, _ := cmd.Flags().GetBool("volumes")
		wantContainers, _ := cmd.Flags().GetBool("containers")
		wantImages, _ := cmd.Flags().GetBool("images")
		wantNetworks, _ := cmd.Flags().GetBool("networks")
		wantCache, _ := cmd.Flags().GetBool("cache")

		mgr, err := housekeeping.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if dfOnly {
			usage, err := mgr.DiskUsage(cmd.Context())
			if err != nil {
				return err
			}
			printUsage(usage)
			return nil
		}

		cats := cleanupCategories(wantContainers, wantImages, wantNetworks, wantCache, includeVolumes)

		result, err := mgr.Prune(cmd.Context(), housekeeping.Options{Categories: cats, Apply: apply})
		if err != nil {
			return err
		}

		if !result.Applied {
			fmt.Println("[tengiz] dry run — nothing removed (use --apply to prune)")
			if usage, uErr := mgr.DiskUsage(cmd.Context()); uErr == nil {
				printUsage(usage)
			}
			if len(result.Candidates) == 0 {
				fmt.Println("[tengiz] nothing to prune.")
				return nil
			}
			fmt.Println("[tengiz] would prune:")
			for _, c := range result.Candidates {
				fmt.Printf("  %-11s %s %s\n", c.Category, c.ID, c.Name)
			}
			return nil
		}

		fmt.Printf("[tengiz] reclaimed %s\n", formatBytes(result.ReclaimedBytes))
		for _, cat := range cats {
			if v, ok := result.ReclaimedByCategory[cat]; ok {
				fmt.Printf("  %-11s %s\n", cat, formatBytes(v))
			}
		}
		return nil
	},
}

func printUsage(u *housekeeping.Usage) {
	fmt.Println("[tengiz] Docker disk usage (reclaimable):")
	fmt.Printf("  %-14s %s\n", "Containers:", formatBytes(u.ContainersReclaimable))
	fmt.Printf("  %-14s %s\n", "Images:", formatBytes(u.ImagesReclaimable))
	fmt.Printf("  %-14s %s\n", "Volumes:", formatBytes(u.VolumesReclaimable))
	fmt.Printf("  %-14s %s\n", "Build Cache:", formatBytes(u.CacheReclaimable))
}

func formatBytes(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}

// cleanupCategories resolves the requested prune categories. Category flags
// select specific categories; when none are given, the safe defaults apply.
// --volumes always ADDS volumes to the resulting set (never replaces it).
func cleanupCategories(wantContainers, wantImages, wantNetworks, wantCache, includeVolumes bool) []housekeeping.Category {
	cats := make([]housekeeping.Category, 0, 5)
	if wantContainers {
		cats = append(cats, housekeeping.CategoryContainers)
	}
	if wantImages {
		cats = append(cats, housekeeping.CategoryImages)
	}
	if wantNetworks {
		cats = append(cats, housekeeping.CategoryNetworks)
	}
	if wantCache {
		cats = append(cats, housekeeping.CategoryCache)
	}
	if len(cats) == 0 {
		cats = append(cats, housekeeping.DefaultCategories...)
	}
	if includeVolumes {
		for _, c := range cats {
			if c == housekeeping.CategoryVolumes {
				return cats
			}
		}
		cats = append(cats, housekeeping.CategoryVolumes)
	}
	return cats
}
```

- [ ] **Step 4: Register the command in `internal/cli/root.go`**

Add to the `init()` function, after the other `rootCmd.AddCommand(...)` calls (e.g. after `rootCmd.AddCommand(secretCmd)` on line 69):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("apply", false, "actually prune resources (default: dry run)")
	cleanupCmd.Flags().Bool("df", false, "print Docker disk usage summary only")
	cleanupCmd.Flags().Bool("volumes", false, "include unused volumes (data risk)")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers only")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images only")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks only")
	cleanupCmd.Flags().Bool("cache", false, "prune build cache only")
```

Registering flags in `init()` (not `Execute()`) matters: CLI tests in this repo never call `Execute()`, so flags registered only there are invisible to `TestCleanupFlagsRegistered`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupFlagsRegistered|TestFormatBytes|TestCleanupCategories" -v -count=1`

Expected: PASS (all 4 tests)

- [ ] **Step 6: Manual smoke test**

Run: `go build -o /tmp/tengiz-cleanup . && /tmp/tengiz-cleanup cleanup --df`

Expected: if Docker is available on this machine, prints a `[tengiz] Docker disk usage (reclaimable):` block; if Docker is missing, prints `docker: docker not found in PATH: exec: "docker": executable file not found in $PATH` and exits non-zero. Both are correct behavior. Then run `/tmp/tengiz-cleanup cleanup` — dry run output starting with `[tengiz] dry run — nothing removed (use --apply to prune)` (or a candidate list).

- [ ] **Step 7: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: build OK, vet clean, all tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 6: Documentation

**Files:**
- Modify: `README.md` — add a bullet to Features (around line 22, after `- **Health check configuration**`) and a `### \`tengiz cleanup\`` section in CLI Reference (after the `### \`tengiz secret list <app>\`` section)
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 Docker Housekeeping as implemented
- Modify: `AGENTS.md` — add `tengiz cleanup` to the Commands section

**Interfaces:**
- Consumes: final CLI surface from Task 5 (`tengiz cleanup`, `--apply`, `--df`, `--volumes`, `--containers`, `--images`, `--networks`, `--cache`)

- [ ] **Step 1: Update README Features list**

Insert after the `- **Health check configuration** ...` line:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes stopped non-Tengiz containers, dangling images, unused networks, and build cache with label-based protection. Dry-run by default; `--apply` to reclaim disk space.
```

- [ ] **Step 2: Add README CLI Reference section**

Insert a new `### `tengiz cleanup`` section after the `### \`tengiz secret list <app>\`` section:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space on the host. Defaults to a **dry run** that lists what would be removed — pass `--apply` to actually delete.

| Flag | Description |
|------|-------------|
| `--apply` | Actually prune (default: dry run) |
| `--df` | Print a Docker disk usage summary and exit |
| `--volumes` | Also remove unused volumes (data risk; never enabled by default) |
| `--containers` | Only remove stopped non-Tengiz containers |
| `--images` | Only remove dangling (untagged) images |
| `--networks` | Only remove unused networks |
| `--cache` | Only remove build cache |

Safety guarantees: containers managed by Tengiz (identified by the `tengiz-app` label, including scale-to-zero stopped containers) are never removed; tagged images used for rollback are never removed; volumes are only touched with `--volumes`. When no category flags are given, the default categories are `containers`, `images`, `networks`, and `cache`.

```bash
tengiz cleanup            # dry run: show what would be freed
tengiz cleanup --apply    # reclaim disk space
tengiz cleanup --df       # disk usage summary only
```
```

- [ ] **Step 3: Update `docs/FUTURES_FEATURES.md`**

In the P0 table, change feature #6 row from `⬜` to `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

And add a row to the `### ✅ Implemented Features (Not Pending)` table:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-18) |
```

- [ ] **Step 4: Update `AGENTS.md` Commands section**

Add to the CLI command list, after `tengiz rollback <app>`:

```markdown
tengiz cleanup [--apply] [--df] [--containers|--images|--networks|--cache|--volumes] → prune unused Docker resources (dry-run by default)
```

- [ ] **Step 5: Final verification**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: build OK, vet clean, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage.** The FUTURES_FEATURES.md feature #6 requires "Label-based `docker system prune`. `tengiz cleanup`." — Task 3/Task 4 implement label-based container pruning via the `label!=tengiz-app` filter, Task 5 adds the `tengiz cleanup` command, Task 6 documents it. The related granularity note (per-category prune) from #56 is covered by the `--containers/--images/--networks/--cache/--volumes` category flags. No spec requirement is left without a task.

**2. Placeholder scan.** All steps contain complete code or exact commands with expected output. No "TBD"/"TODO"/"implement later"/"handle edge cases" placeholders.

**3. Type consistency.** `Category`, `Options{Categories, Apply}`, `Usage{ContainersReclaimable, ImagesReclaimable, VolumesReclaimable, CacheReclaimable}`, `Candidate{Category, ID, Name}`, `PruneResult{Applied, Candidates, ReclaimedBytes, ReclaimedByCategory}` are defined once in Task 1 and referenced identically in Tasks 2-5. `pruneArgs`, `containerCandidatesArgs`, `imageCandidatesArgs`, `networkCandidatesArgs`, `parseSize`, `parseDfOutput`, `parseReclaimed`, `parseCandidates`, `isPureInt`, `NewDocker`, `DiskUsage`, `Prune`, `printUsage`, `formatBytes`, `cleanupCategories`, `cleanupCmd`, and the `tengiz-app` label constant keep the same names/signatures throughout. Flag names (`--apply`, `--df`, `--volumes`, `--containers`, `--images`, `--networks`, `--cache`) match between the CLI implementation (Task 5) and tests/docs (Tasks 5-6). `cleanupCategories` is the single source of truth for category selection, used by both the CLI (Task 5) and its test — so `--volumes` "adds to the set" semantics stay consistent.
