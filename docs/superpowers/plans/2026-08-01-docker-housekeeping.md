# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that frees disk space by pruning unused Docker containers, images, networks, volumes, and build cache while protecting Tengiz-managed resources via label-based filtering.

**Architecture:** A new `internal/cleanup` package provides a `Pruner` that orchestrates per-category prune operations through the existing `runtime.Manager` interface. The `runtime` package gains a single `Prune(ctx, category, opts)` method (plus `PruneReport`/`PruneOptions` types) implemented by `dockerRuntime` via `os/exec`, with pure argument-building functions (`buildPruneArgs`, `buildListArgs`) kept unit-testable without a Docker daemon — matching the existing `buildRunArgs`/`resourceArgs` test pattern. Dry-run mode uses list commands (`docker ps -aq`, `docker images -q`, etc.) to report counts without deleting. Image retention reuses the existing `KeepLastNImages` per app (default 5, mirroring the deploy path). Volumes are deliberately excluded from `--all` to protect data; users opt in with `--volumes`.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (Docker CLI), existing `runtime.Manager` and `config.Store` interfaces. No new external dependencies.

## Global Constraints

- Only edit files listed in each task; do not restructure unrelated code
- Work happens on branch `feat/docker-housekeeping` (`git checkout -b feat/docker-housekeeping` first)
- All Docker operations go through `os/exec` — no Docker SDK
- Containers carrying the `tengiz-app` label must NEVER be pruned (label filter `label!=tengiz-app`)
- Volumes are NOT included in `--all`; they require the explicit `--volumes` flag (may hold data)
- Dry-run (`--dry-run`) must never call a destructive prune command — only list commands
- Image retention default is 5 per app (matches `KeepLastNImages` used at deploy time in `internal/cli/root.go`)
- Existing tests must continue to pass without modification (except the mock type updated in Task 2)
- No new external dependencies
- Commit after every green test cycle

---

## File Structure

| File | Responsibility |
|------|---------------|
| Create: `internal/runtime/prune.go` | `PruneReport`, `PruneOptions`, `buildPruneArgs()`, `buildListArgs()`, `parseRemovedCount()`, `countNonEmptyLines()`, `dockerRuntime.Prune()` |
| Create: `internal/runtime/prune_test.go` | Unit tests for the arg builders and `parseRemovedCount` |
| Modify: `internal/runtime/runtime.go` | Add `Prune` to `Manager` interface + `stubManager.Prune` |
| Modify: `internal/runtime/cleanup_test.go` | Add stub `Prune` test |
| Create: `internal/cleanup/cleanup.go` | `Category`, `Options`, `Options.Categories()`, `Result`, `Pruner`, `Pruner.Run()` |
| Create: `internal/cleanup/cleanup_test.go` | Tests for `Categories()` and `Pruner.Run()` with a recording stub |
| Modify: `internal/cli/root.go` | Register `cleanupCmd`, flags, and wiring |
| Modify: `internal/cli/root_test.go` | Add `Prune` method to `mockRTForDeploy` |
| Create: `internal/cli/cleanup_test.go` | Registration/flag/validation tests for the CLI command |
| Modify: `README.md` | Document `tengiz cleanup` command |
| Modify: `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Prune arg builders + report types in `runtime` package

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `type PruneReport struct { Category string; Removed int }`, `type PruneOptions struct { DryRun bool; AllImages bool }`, `buildPruneArgs(category string, opts PruneOptions) ([]string, error)`, `buildListArgs(category string, opts PruneOptions) ([]string, error)`, `countNonEmptyLines(s string) int`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

Expected: branch created, still on current worktree.

- [ ] **Step 2: Write the failing test**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import "testing"

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		category string
		opts     PruneOptions
		expected []string
		wantErr  bool
	}{
		{
			name:     "containers protects tengiz apps",
			category: "containers",
			opts:     PruneOptions{},
			expected: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "images dangling only by default",
			category: "images",
			opts:     PruneOptions{},
			expected: []string{"image", "prune", "-f"},
		},
		{
			name:     "images with AllImages",
			category: "images",
			opts:     PruneOptions{AllImages: true},
			expected: []string{"image", "prune", "-f", "-a"},
		},
		{
			name:     "networks",
			category: "networks",
			opts:     PruneOptions{},
			expected: []string{"network", "prune", "-f"},
		},
		{
			name:     "volumes",
			category: "volumes",
			opts:     PruneOptions{},
			expected: []string{"volume", "prune", "-f"},
		},
		{
			name:     "buildcache",
			category: "buildcache",
			opts:     PruneOptions{},
			expected: []string{"builder", "prune", "-f"},
		},
		{
			name:     "unknown category errors",
			category: "bogus",
			opts:     PruneOptions{},
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := buildPruneArgs(tt.category, tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("buildPruneArgs: %v", err)
			}
			if !pruneArgsEqual(args, tt.expected) {
				t.Fatalf("buildPruneArgs() = %v, want %v", args, tt.expected)
			}
		})
	}
}

func TestBuildListArgs(t *testing.T) {
	tests := []struct {
		name     string
		category string
		opts     PruneOptions
		expected []string
		wantErr  bool
	}{
		{
			name:     "containers",
			category: "containers",
			opts:     PruneOptions{},
			expected: []string{"ps", "-aq", "--filter", "status=exited", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "images dangling",
			category: "images",
			opts:     PruneOptions{},
			expected: []string{"images", "-q", "--filter", "dangling=true"},
		},
		{
			name:     "images all",
			category: "images",
			opts:     PruneOptions{AllImages: true},
			expected: []string{"images", "-q", "-a"},
		},
		{
			name:     "networks",
			category: "networks",
			opts:     PruneOptions{},
			expected: []string{"network", "ls", "-q", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "volumes",
			category: "volumes",
			opts:     PruneOptions{},
			expected: []string{"volume", "ls", "-q", "--filter", "dangling=true"},
		},
		{
			name:     "buildcache",
			category: "buildcache",
			opts:     PruneOptions{},
			expected: []string{"builder", "du"},
		},
		{
			name:     "unknown category errors",
			category: "bogus",
			opts:     PruneOptions{},
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := buildListArgs(tt.category, tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("buildListArgs: %v", err)
			}
			if !pruneArgsEqual(args, tt.expected) {
				t.Fatalf("buildListArgs() = %v, want %v", args, tt.expected)
			}
		})
	}
}

func TestCountNonEmptyLines(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expected int
	}{
		{"empty", "", 0},
		{"blank lines ignored", "\n\n", 0},
		{"three ids", "abc\n  \ndef\nghi", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countNonEmptyLines(tt.input); got != tt.expected {
				t.Fatalf("countNonEmptyLines(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func pruneArgsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestBuildListArgs|TestCountNonEmptyLines" -v -count=1`

Expected: FAIL with `undefined: buildPruneArgs`, `undefined: buildListArgs`, `undefined: countNonEmptyLines`

- [ ] **Step 4: Write minimal implementation**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"fmt"
	"strings"
)

type PruneReport struct {
	Category string `json:"category"`
	Removed  int    `json:"removed"`
}

type PruneOptions struct {
	DryRun    bool
	AllImages bool
}

func buildPruneArgs(category string, opts PruneOptions) ([]string, error) {
	switch category {
	case "containers":
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}, nil
	case "images":
		args := []string{"image", "prune", "-f"}
		if opts.AllImages {
			args = append(args, "-a")
		}
		return args, nil
	case "networks":
		return []string{"network", "prune", "-f"}, nil
	case "volumes":
		return []string{"volume", "prune", "-f"}, nil
	case "buildcache":
		return []string{"builder", "prune", "-f"}, nil
	default:
		return nil, fmt.Errorf("unknown prune category %q", category)
	}
}

func buildListArgs(category string, opts PruneOptions) ([]string, error) {
	switch category {
	case "containers":
		return []string{"ps", "-aq", "--filter", "status=exited", "--filter", "label!=tengiz-app"}, nil
	case "images":
		args := []string{"images", "-q"}
		if opts.AllImages {
			args = append(args, "-a")
		} else {
			args = append(args, "--filter", "dangling=true")
		}
		return args, nil
	case "networks":
		return []string{"network", "ls", "-q", "--filter", "label!=tengiz-app"}, nil
	case "volumes":
		return []string{"volume", "ls", "-q", "--filter", "dangling=true"}, nil
	case "buildcache":
		return []string{"builder", "du"}, nil
	default:
		return nil, fmt.Errorf("unknown prune category %q", category)
	}
}

func countNonEmptyLines(s string) int {
	count := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestBuildListArgs|TestCountNonEmptyLines" -v -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(cleanup): add docker prune arg builders and report types"
```

---

### Task 2: `parseRemovedCount` + `dockerRuntime.Prune` + `Manager` interface

**Files:**
- Modify: `internal/runtime/prune.go`
- Modify: `internal/runtime/runtime.go` (interface + stub)
- Modify: `internal/cli/root_test.go` (mock)
- Modify: `internal/runtime/cleanup_test.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `PruneReport`, `PruneOptions`, `buildPruneArgs`, `buildListArgs`, `countNonEmptyLines` from Task 1
- Produces: `parseRemovedCount(output string) int`, `Manager.Prune(ctx context.Context, category string, opts PruneOptions) (PruneReport, error)`

- [ ] **Step 1: Write the failing test for `parseRemovedCount`**

Append to `internal/runtime/prune_test.go`:

```go
func TestParseRemovedCount(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
	}{
		{"empty output", "", 0},
		{"container prune", "Deleted Containers:\n1a2b3c\n7d8e9f\nTotal reclaimed space: 5kB", 2},
		{"image prune counts deleted lines not untagged", "Deleted Images:\nuntagged: tengiz-apps/foo:v1\ndeleted: sha256:abc\ndeleted: sha256:def\nTotal reclaimed space: 1MB", 2},
		{"volume prune", "Deleted Volumes:\nmyapp_data\nTotal reclaimed space: 1kB", 1},
		{"network prune", "Deleted Networks:\nmy_network\nTotal reclaimed space: 0B", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRemovedCount(tt.output); got != tt.want {
				t.Fatalf("parseRemovedCount(%q) = %d, want %d", tt.output, got, tt.want)
			}
		})
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), "containers", PruneOptions{})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	if report.Category != "containers" {
		t.Errorf("Category = %q, want %q", report.Category, "containers")
	}
}
```

Add `context` to the test file imports if not already present (Task 1's test file only imports `testing`):

```go
import (
	"context"
	"testing"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestParseRemovedCount|TestStubPrune" -v -count=1`

Expected: FAIL — `undefined: parseRemovedCount` and compile error for the missing `Prune` method on `Manager`/`stubManager`.

- [ ] **Step 3: Implement `parseRemovedCount` and `dockerRuntime.Prune`**

Append to `internal/runtime/prune.go`:

```go
func parseRemovedCount(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space") {
			continue
		}
		if strings.HasPrefix(line, "Deleted ") {
			continue
		}
		if strings.HasPrefix(line, "Untagged:") {
			continue
		}
		count++
	}
	return count
}
```

Add the `exec`/`context` imports to `prune.go` (it currently imports `fmt` and `strings` only):

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)
```

Append the `dockerRuntime.Prune` method to `internal/runtime/prune.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, category string, opts PruneOptions) (PruneReport, error) {
	if opts.DryRun {
		args, err := buildListArgs(category, opts)
		if err != nil {
			return PruneReport{}, err
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.Output()
		if err != nil {
			return PruneReport{Category: category}, fmt.Errorf("docker %s list: %w", category, err)
		}
		return PruneReport{Category: category, Removed: countNonEmptyLines(string(out))}, nil
	}

	args, err := buildPruneArgs(category, opts)
	if err != nil {
		return PruneReport{}, err
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{Category: category}, fmt.Errorf("docker %s prune: %w\n%s", category, err, string(out))
	}
	return PruneReport{Category: category, Removed: parseRemovedCount(string(out))}, nil
}
```

- [ ] **Step 4: Add `Prune` to the `Manager` interface + stub**

Modify `internal/runtime/runtime.go` — add to the `Manager` interface after the `KeepLastNImages` line (line 36):

```go
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, category string, opts PruneOptions) (PruneReport, error)
```

Add the stub implementation to `internal/runtime/runtime.go` after the `stubManager.KeepLastNImages` method (line 119):

```go
func (m *stubManager) Prune(ctx context.Context, category string, opts PruneOptions) (PruneReport, error) {
	return PruneReport{Category: category}, nil
}
```

- [ ] **Step 5: Update the CLI mock type**

Modify `internal/cli/root_test.go` — add the `Prune` method after the `mockRTForDeploy.KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, category string, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{Category: category}, nil
}
```

- [ ] **Step 6: Add stub prune test to existing cleanup test file**

Modify `internal/runtime/cleanup_test.go` — add:

```go
func TestStubPruneAllCategories(t *testing.T) {
	m := NewStub()
	for _, category := range []string{"containers", "images", "networks", "volumes", "buildcache"} {
		report, err := m.Prune(context.Background(), category, PruneOptions{DryRun: true})
		if err != nil {
			t.Fatalf("Prune(%q) error = %v", category, err)
		}
		if report.Category != category {
			t.Errorf("Prune(%q) Category = %q", category, report.Category)
		}
	}
}
```

The file already imports `context` and `testing` (check current imports — it imports `context` and `testing`).

- [ ] **Step 7: Run full test suite to verify everything passes**

Run: `go test ./internal/runtime/... ./internal/cli/... -v -count=1`

Expected: PASS (interface satisfied by stub + mock)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(cleanup): add Prune to runtime.Manager with dry-run support"
```

---

### Task 3: `cleanup` package — `Pruner` orchestrator

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager` (`Prune`, `KeepLastNImages`), `config.Store` (`ListApps`)
- Produces: `type Category string` with constants, `type Options struct{...}` + `(o Options) Categories() []Category`, `type Result struct{ Reports []runtime.PruneReport; DryRun bool; RetainedApps int }`, `type Pruner struct`, `New(rt runtime.Manager, store *config.Store) *Pruner`, `(p *Pruner) Run(ctx context.Context, opts Options) (*Result, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

func TestOptionsCategories(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want []Category
	}{
		{"none", Options{}, nil},
		{"containers only", Options{Containers: true}, []Category{CategoryContainers}},
		{"all excludes volumes", Options{All: true}, []Category{CategoryContainers, CategoryImages, CategoryNetworks, CategoryBuildCache}},
		{"all plus explicit volumes", Options{All: true, Volumes: true}, []Category{CategoryContainers, CategoryImages, CategoryNetworks, CategoryVolumes, CategoryBuildCache}},
		{"images plus volumes", Options{Images: true, Volumes: true}, []Category{CategoryImages, CategoryVolumes}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.opts.Categories()
			if len(got) != len(tt.want) {
				t.Fatalf("Categories() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("Categories()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

type recordingManager struct {
	runtime.Manager
	pruned   []string
	retained int
}

func (m *recordingManager) Prune(ctx context.Context, category string, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	m.pruned = append(m.pruned, category)
	return runtime.PruneReport{Category: category, Removed: 1}, nil
}

func (m *recordingManager) KeepLastNImages(ctx context.Context, appName string, n int) error {
	m.retained++
	return nil
}

func TestPrunerRunAll(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)
	if err := store.SaveApp(types.AppEntry{Name: "myapp", ImageTag: "tengiz-apps/myapp:1"}); err != nil {
		t.Fatal(err)
	}

	m := &recordingManager{}
	p := New(m, store)
	res, err := p.Run(context.Background(), Options{All: true, Images: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Reports) != 4 {
		t.Fatalf("expected 4 reports, got %d: %v", len(res.Reports), res.Reports)
	}
	want := []string{"containers", "images", "networks", "buildcache"}
	for i, w := range want {
		if m.pruned[i] != w {
			t.Errorf("pruned[%d] = %q, want %q", i, m.pruned[i], w)
		}
	}
	if res.RetainedApps != 1 {
		t.Errorf("RetainedApps = %d, want 1", res.RetainedApps)
	}
}

func TestPrunerRunDryRun(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)
	if err := store.SaveApp(types.AppEntry{Name: "myapp", ImageTag: "tengiz-apps/myapp:1"}); err != nil {
		t.Fatal(err)
	}

	m := &recordingManager{}
	p := New(m, store)
	res, err := p.Run(context.Background(), Options{All: true, DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.DryRun {
		t.Errorf("DryRun = %v, want true", res.DryRun)
	}
	if res.RetainedApps != 0 {
		t.Errorf("RetainedApps = %d, want 0 (no retention in dry run)", res.RetainedApps)
	}
	for _, r := range res.Reports {
		if r.Removed != 1 {
			t.Errorf("report %s Removed = %d, want 1", r.Category, r.Removed)
		}
	}
}

func TestPrunerRunNoCategories(t *testing.T) {
	p := New(&recordingManager{}, nil)
	if _, err := p.Run(context.Background(), Options{}); err == nil {
		t.Fatal("expected error when no category selected")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL with `package cleanup is not in std` / undefined types (package does not exist yet)

- [ ] **Step 3: Write minimal implementation**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"context"
	"fmt"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

type Category string

const (
	CategoryContainers Category = "containers"
	CategoryImages     Category = "images"
	CategoryNetworks   Category = "networks"
	CategoryVolumes    Category = "volumes"
	CategoryBuildCache Category = "buildcache"
)

type Options struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	All        bool
	DryRun     bool
	AllImages  bool
	KeepImages int
}

func (o Options) keepN() int {
	if o.KeepImages <= 0 {
		return 5
	}
	return o.KeepImages
}

func (o Options) Categories() []Category {
	var cats []Category
	if o.All || o.Containers {
		cats = append(cats, CategoryContainers)
	}
	if o.All || o.Images {
		cats = append(cats, CategoryImages)
	}
	if o.All || o.Networks {
		cats = append(cats, CategoryNetworks)
	}
	if o.Volumes {
		cats = append(cats, CategoryVolumes)
	}
	if o.All || o.BuildCache {
		cats = append(cats, CategoryBuildCache)
	}
	return cats
}

type Result struct {
	Reports      []runtime.PruneReport
	DryRun       bool
	RetainedApps int
}

type Pruner struct {
	rt    runtime.Manager
	store *config.Store
}

func New(rt runtime.Manager, store *config.Store) *Pruner {
	return &Pruner{rt: rt, store: store}
}

func (p *Pruner) Run(ctx context.Context, opts Options) (*Result, error) {
	res := &Result{DryRun: opts.DryRun}
	cats := opts.Categories()
	if len(cats) == 0 {
		return nil, fmt.Errorf("nothing to clean: specify a category flag (--containers, --images, --networks, --volumes, --build-cache) or --all")
	}

	for _, c := range cats {
		report, err := p.rt.Prune(ctx, string(c), runtime.PruneOptions{DryRun: opts.DryRun, AllImages: opts.AllImages})
		if err != nil {
			return res, fmt.Errorf("prune %s: %w", c, err)
		}
		res.Reports = append(res.Reports, report)
	}

	if (opts.Images || opts.All) && !opts.DryRun && p.store != nil {
		apps, err := p.store.ListApps()
		if err == nil {
			for _, app := range apps {
				if keepErr := p.rt.KeepLastNImages(ctx, app.Name, opts.keepN()); keepErr == nil {
					res.RetainedApps++
				}
			}
		}
	}
	return res, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add Pruner orchestrator for docker housekeeping"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` (imports, `init()` registration + flags, new command var)
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.New(rt, store)`, `cleanup.Options`, `cleanup.Pruner.Run`, `runtime.NewDocker()`, `config.NewStoreWithEnv(dataDir, env)`
- Produces: `cleanupCmd *cobra.Command` registered on `rootCmd`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not registered")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "networks", "volumes", "build-cache", "all", "all-images", "dry-run", "keep-images"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func TestCleanupCommandRejectsNoCategories(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no category flag provided")
	}
	if !strings.Contains(err.Error(), "nothing to clean") {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCommand" -v -count=1`

Expected: FAIL — `cleanup command not found` (command not registered yet)

- [ ] **Step 3: Implement the command**

Add the `cleanup` package import to `internal/cli/root.go` (alphabetical order, after `builder` on line 17):

```go
	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/cleanup"
```

Register the command in `internal/cli/root.go` `init()` — add after `rootCmd.AddCommand(runCmd)` (line 67), with its flags registered inline so they exist before tests call `rootCmd.Execute()`:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images and old app images")
	cleanupCmd.Flags().Bool("networks", false, "remove unused custom networks")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused named volumes (may hold data)")
	cleanupCmd.Flags().Bool("build-cache", false, "remove build cache")
	cleanupCmd.Flags().Bool("all", false, "clean containers, images, networks, and build cache")
	cleanupCmd.Flags().Bool("all-images", false, "remove ALL unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("dry-run", false, "report what would be removed without removing anything")
	cleanupCmd.Flags().Int("keep-images", 5, "number of images to keep per app")
```

Add the command definition in `internal/cli/root.go` after `runCmd` (after line 1162):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Clean up unused Docker resources to free disk space.

By default protects Tengiz-managed containers (tengiz-app label) and keeps the
last 5 images per app for rollback.

Category flags:
  --containers   remove stopped containers not managed by Tengiz
  --images       remove dangling images and old app images beyond --keep-images
  --networks     remove unused custom networks
  --volumes      remove unused named volumes (NOT included in --all - may hold data)
  --build-cache  remove build cache
  --all          containers, images, networks, and build cache (no volumes)
  --all-images   with --images/--all: remove ALL unused images, not just dangling

Use --dry-run to report what would be removed without removing anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		allImages, _ := cmd.Flags().GetBool("all-images")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		networks, _ := cmd.Flags().GetBool("networks")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		keepImages, _ := cmd.Flags().GetInt("keep-images")

		opts := cleanup.Options{
			Containers: containers,
			Images:     images,
			Networks:   networks,
			Volumes:    volumes,
			BuildCache: buildCache,
			All:        all,
			DryRun:     dryRun,
			AllImages:  allImages,
			KeepImages: keepImages,
		}
		if len(opts.Categories()) == 0 {
			return fmt.Errorf("nothing to clean: use --all or one of --containers, --images, --networks, --volumes, --build-cache")
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		store := config.NewStoreWithEnv(dataDir, env)
		pruner := cleanup.New(rt, store)

		res, err := pruner.Run(cmd.Context(), opts)
		if err != nil {
			return err
		}

		if res.DryRun {
			fmt.Println("[tengiz] dry-run: resources that would be cleaned:")
		} else {
			fmt.Println("[tengiz] cleanup complete:")
		}
		for _, r := range res.Reports {
			fmt.Printf("[tengiz]   %-12s removed: %d\n", r.Category, r.Removed)
		}
		if keepImages <= 0 {
			keepImages = 5
		}
		if res.RetainedApps > 0 {
			fmt.Printf("[tengiz]   retained last %d images for %d apps\n", keepImages, res.RetainedApps)
		}
		return nil
	},
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanupCommand" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run the full test suite + vet**

Run: `go test ./... -count=1 && go vet ./...`

Expected: PASS (no failures, no vet warnings)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Documentation (README + feature tracking)

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the finalized CLI surface from Task 4
- Produces: documented `tengiz cleanup` command; feature #6 marked ✅ Implemented

- [ ] **Step 1: Document the command in README**

Add a section in `README.md` after the `### tengiz run` section (after line 203, before `### tengiz rollback`):

````markdown
### `tengiz cleanup`

Clean up unused Docker resources to free disk space. Protects Tengiz-managed containers (any container with the `tengiz-app` label) and keeps the last 5 images per app for rollback.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling images and old app images beyond `--keep-images` |
| `--networks` | Remove unused custom networks |
| `--volumes` | Remove unused named volumes (NOT included in `--all` — may hold data) |
| `--build-cache` | Remove build cache |
| `--all` | Containers, images, networks, and build cache (no volumes) |
| `--all-images` | Remove ALL unused images, not just dangling ones |
| `--dry-run` | Report what would be removed without removing anything |
| `--keep-images` | Number of images to keep per app (default 5) |

Examples:

```bash
tengiz cleanup --all                  # containers, images, networks, build cache
tengiz cleanup --volumes              # explicitly prune unused named volumes
tengiz cleanup --all --dry-run        # see what would be freed
tengiz cleanup --all-images --all     # also remove all unused images, not just dangling
```
````

- [ ] **Step 2: Verify the README change renders correctly**

Run: `grep -n "tengiz cleanup" README.md`

Expected: at least one match line with `### \`tengiz cleanup\``

- [ ] **Step 3: Mark the feature as implemented**

Modify `docs/FUTURES_FEATURES.md` line 19 — change feature #6 to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Verification Checklist

- [ ] `go test ./... -count=1` passes
- [ ] `go vet ./...` passes
- [ ] `go build -o tengiz .` succeeds
- [ ] `tengiz cleanup` with no flags prints "nothing to clean" error
- [ ] `tengiz cleanup --dry-run --all` runs without deleting anything
- [ ] `tengiz cleanup --all` prunes containers/images/networks/build-cache but NOT volumes
- [ ] `tengiz cleanup --volumes` prunes volumes only when explicitly requested
- [ ] Containers with the `tengiz-app` label are never removed
- [ ] README documents all cleanup flags
