# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command family that prunes unused Docker resources (stopped non-Tengiz containers, dangling images, unused volumes, unused networks, build cache) with label-based protection for Tengiz-managed containers, plus `--dry-run`, `--keep-images`, and `--interval` options.

**Architecture:** A new `internal/cleanup` package owns Docker maintenance (it shells `docker` via an injectable runner for testability) and reuses the existing `runtime.Manager.KeepLastNImages` for per-app image retention using the env-scoped `config.Store`. The `runtime.Manager` interface is intentionally NOT modified — it stays focused on app lifecycle. A `tengiz cleanup [category]` CLI command (registered in `root.go`) orchestrates the manager and prints a per-category report with bytes reclaimed.

**Tech Stack:** Go 1.26 stdlib only (`os/exec`, `encoding/json`), existing `cobra`, existing `internal/runtime` (`ImageRetainer` via `KeepLastNImages`), existing `internal/config.Store`. No new external dependencies.

## Global Constraints

- No new external dependencies — stdlib + existing packages only
- `runtime.Manager` interface is NOT modified; cleanup depends only on `runtime.ImageRetainer` (the `KeepLastNImages` method)
- Tengiz-managed containers (Docker label `tengiz-app`) are NEVER removed: the container prune always runs with `--filter label!=tengiz-app`
- `--dry-run` must never modify Docker state (no `* prune` or `rmi` executed)
- Default environment is `"production"`; env-scoping for image retention uses `config.NewStoreWithEnv(dataDir, env)`
- `--keep-images` defaults to `5` (matches existing deploy-time `KeepLastNImages(ctx, app, 5)` calls)
- Every destructive prune uses `--format "{{json .}}"`; `parsePruneResult` falls back to plain-text line parsing for Docker versions without `--format` support
- Error style matches codebase: `fmt.Errorf("docker <op>: %w\n%s", err, string(out))`
- All new files follow existing package layout: one responsibility per file, tests colocated as `*_test.go`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/types.go` | `Category`, `PruneResult`, `DiskUsage`, `CategoryResult`, `Report`, `Options`, pure parsers `parsePruneResult`/`parseDfOutput`, `FormatBytes` |
| `internal/cleanup/cleanup.go` | `Manager` with injectable `dockerRunner`; `Run()` per-category prune/dry-run logic; `DiskUsage()`; per-app image retention via `ImageRetainer` |
| `internal/cleanup/cleanup_test.go` | Unit tests using a fake runner + fake retainer (no real docker) |
| `internal/cli/cleanup.go` | `tengiz cleanup [category]` Cobra command + `printCleanupReport` |
| `internal/cli/cleanup_test.go` | Command registration, flag presence, category validation, report formatting tests |
| `internal/cli/root.go` | Register `cleanupCmd` and its flags in `init()` |
| `README.md` | New `### tengiz cleanup [category]` section in CLI Reference |
| `AGENTS.md` | Add `cleanup` to CLI command list and architecture table |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

---

### Task 1: Cleanup types and pure parsing helpers

**Files:**
- Create: `internal/cleanup/types.go`
- Test: `internal/cleanup/types_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `Category` (constants `CategoryContainers`, `CategoryImages`, `CategoryVolumes`, `CategoryNetworks`, `CategoryCache`, slice `AllCategories`), `ParseCategory(s string) (Category, error)`, `PruneResult{Deleted []string; SpaceReclaimed int64}`, `DiskUsage` + `(*DiskUsage).TotalSize() int64`, `CategoryResult{Category Category; Deleted []string; SpaceReclaimed int64}`, `Report{DryRun bool; Results []*CategoryResult; SpaceReclaimed int64}` + `(*Report).TotalDeleted() int` + `(*Report).Find(Category) *CategoryResult`, `parsePruneResult(out []byte) *PruneResult`, `parseDfOutput(out []byte) (*DiskUsage, error)`, `FormatBytes(b int64) string`

- [ ] **Step 1: Write the failing test**

Create `internal/cleanup/types_test.go`:

```go
package cleanup

import "testing"

func TestParsePruneResultEmpty(t *testing.T) {
	res := parsePruneResult(nil)
	if len(res.Deleted) != 0 || res.SpaceReclaimed != 0 {
		t.Fatalf("expected empty result, got %+v", res)
	}
}

func TestParsePruneResultJSON(t *testing.T) {
	out := []byte(`{"Deleted":["abc123"],"SpaceReclaimed":1024}`)
	res := parsePruneResult(out)
	if len(res.Deleted) != 1 || res.Deleted[0] != "abc123" {
		t.Fatalf("Deleted = %v, want [abc123]", res.Deleted)
	}
	if res.SpaceReclaimed != 1024 {
		t.Fatalf("SpaceReclaimed = %d, want 1024", res.SpaceReclaimed)
	}
}

func TestParsePruneResultPlainLines(t *testing.T) {
	out := []byte("Deleted Networks:\nabc\nTotal reclaimed space: 0B\n")
	res := parsePruneResult(out)
	if len(res.Deleted) != 1 || res.Deleted[0] != "abc" {
		t.Fatalf("Deleted = %v, want [abc]", res.Deleted)
	}
}

func TestParsePruneResultInvalidJSONFallsBack(t *testing.T) {
	out := []byte(`{"Deleted":[` + "\n")
	res := parsePruneResult(out)
	if res.Deleted == nil {
		t.Fatal("expected non-nil Deleted slice from fallback")
	}
}

func TestParseDfOutput(t *testing.T) {
	out := []byte(`{"Containers":1,"Images":2,"Volumes":1,"BuildCache":0,"ContainersSize":100,"ImagesSize":200,"VolumesSize":50,"BuildCacheSize":0}`)
	du, err := parseDfOutput(out)
	if err != nil {
		t.Fatalf("parseDfOutput: %v", err)
	}
	if du.TotalSize() != 350 {
		t.Fatalf("TotalSize() = %d, want 350", du.TotalSize())
	}
}

func TestParseDfOutputEmpty(t *testing.T) {
	du, err := parseDfOutput(nil)
	if err != nil {
		t.Fatalf("parseDfOutput(nil): %v", err)
	}
	if du == nil || du.TotalSize() != 0 {
		t.Fatalf("expected empty DiskUsage, got %+v", du)
	}
}

func TestParseCategory(t *testing.T) {
	for _, cat := range AllCategories {
		if _, err := ParseCategory(string(cat)); err != nil {
			t.Errorf("ParseCategory(%q) error = %v", cat, err)
		}
	}
	if _, err := ParseCategory("bogus"); err == nil {
		t.Error("ParseCategory(bogus) expected error, got nil")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KB"},
		{2048, "2.0KB"},
		{52428800, "50.0MB"},
	}
	for _, tc := range tests {
		if got := FormatBytes(tc.in); got != tc.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReportFind(t *testing.T) {
	rep := &Report{Results: []*CategoryResult{
		{Category: CategoryContainers, Deleted: []string{"a", "b"}},
		{Category: CategoryCache, SpaceReclaimed: 5},
	}}
	if got := rep.TotalDeleted(); got != 2 {
		t.Fatalf("TotalDeleted() = %d, want 2", got)
	}
	if rep.Find(CategoryContainers) == nil {
		t.Fatal("Find(containers) = nil, want result")
	}
	if rep.Find(CategoryVolumes) != nil {
		t.Fatal("Find(volumes) should be nil for missing category")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL — package `internal/cleanup` does not exist yet (`open internal/cleanup/types_test.go: no such file or directory`).

- [ ] **Step 3: Write the implementation**

Create `internal/cleanup/types.go`:

```go
package cleanup

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Category string

const (
	CategoryContainers Category = "containers"
	CategoryImages     Category = "images"
	CategoryVolumes    Category = "volumes"
	CategoryNetworks   Category = "networks"
	CategoryCache      Category = "cache"
)

var AllCategories = []Category{
	CategoryContainers,
	CategoryImages,
	CategoryVolumes,
	CategoryNetworks,
	CategoryCache,
}

func ParseCategory(s string) (Category, error) {
	for _, c := range AllCategories {
		if Category(s) == c {
			return c, nil
		}
	}
	return "", fmt.Errorf("unknown category %q (supported: containers, images, volumes, networks, cache)", s)
}

type PruneResult struct {
	Deleted        []string `json:"Deleted"`
	SpaceReclaimed int64    `json:"SpaceReclaimed"`
}

func parsePruneResult(out []byte) *PruneResult {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return &PruneResult{}
	}
	var res PruneResult
	if err := json.Unmarshal([]byte(trimmed), &res); err == nil {
		return &res
	}
	lines := strings.Split(trimmed, "\n")
	res.Deleted = make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "Deleted") || strings.HasPrefix(l, "Total reclaimed") {
			continue
		}
		res.Deleted = append(res.Deleted, l)
	}
	return &res
}

type DiskUsage struct {
	Containers     int64 `json:"Containers"`
	Images         int64 `json:"Images"`
	Volumes        int64 `json:"Volumes"`
	BuildCache     int64 `json:"BuildCache"`
	ContainersSize int64 `json:"ContainersSize"`
	ImagesSize     int64 `json:"ImagesSize"`
	VolumesSize    int64 `json:"VolumesSize"`
	BuildCacheSize int64 `json:"BuildCacheSize"`
}

func (d *DiskUsage) TotalSize() int64 {
	return d.ContainersSize + d.ImagesSize + d.VolumesSize + d.BuildCacheSize
}

func parseDfOutput(out []byte) (*DiskUsage, error) {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return &DiskUsage{}, nil
	}
	var du DiskUsage
	if err := json.Unmarshal([]byte(trimmed), &du); err != nil {
		return nil, fmt.Errorf("parse docker system df output: %w", err)
	}
	return &du, nil
}

type CategoryResult struct {
	Category       Category
	Deleted        []string
	SpaceReclaimed int64
}

type Report struct {
	DryRun         bool
	Results        []*CategoryResult
	SpaceReclaimed int64
}

func (r *Report) TotalDeleted() int {
	n := 0
	for _, res := range r.Results {
		n += len(res.Deleted)
	}
	return n
}

func (r *Report) Find(cat Category) *CategoryResult {
	for _, res := range r.Results {
		if res.Category == cat {
			return res
		}
	}
	return nil
}

func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/types.go internal/cleanup/types_test.go
git commit -m "feat: add cleanup types and parsing helpers for docker housekeeping"
```

---

### Task 2: Cleanup Manager (prune + dry-run + image retention)

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: from Task 1 — `Options`-free; uses `Category`, `CategoryResult`, `Report`, `parsePruneResult`, `parseDfOutput`, `AllCategories`. Uses `config.NewStoreWithEnv(dataDir, env)` and `runtime.ImageRetainer`.
- Produces: `Options{DryRun bool; KeepImages int; Categories []Category}`, `Manager`, `NewWithEnv(rt ImageRetainer, dataDir, env string) *Manager`, `(*Manager).Run(ctx, Options) (*Report, error)`, `(*Manager).DiskUsage(ctx) (*DiskUsage, error)`, `dockerRunner` func type. Also produces `runtime.ImageRetainer` (must be added to `internal/runtime/runtime.go` — interface containing only `KeepLastNImages`).

- [ ] **Step 1: Write the failing test**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)

type fakeRunner struct {
	mu    sync.Mutex
	calls []string
	out   map[string][]byte
	err   error
}

func (f *fakeRunner) key(args []string) string {
	if len(args) >= 2 {
		return strings.Join(args[:2], " ")
	}
	if len(args) == 1 {
		return args[0]
	}
	return ""
}

func (f *fakeRunner) run(ctx context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, strings.Join(args, " "))
	if f.err != nil {
		return nil, f.err
	}
	if out, ok := f.out[f.key(args)]; ok {
		return out, nil
	}
	return nil, nil
}

func (f *fakeRunner) contains(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

type fakeRetainer struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeRetainer) KeepLastNImages(ctx context.Context, appName string, n int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, appName)
	return nil
}

func (f *fakeRetainer) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

func TestManagerRunPrunesContainersWithTengizProtection(t *testing.T) {
	runner := &fakeRunner{out: map[string][]byte{
		"container prune": []byte(`{"Deleted":["abc123"],"SpaceReclaimed":1024}`),
	}}
	m := NewWithEnv(nil, t.TempDir(), "production")
	m.run = runner.run

	rep, err := m.Run(context.Background(), Options{Categories: []Category{CategoryContainers}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.SpaceReclaimed != 1024 {
		t.Errorf("SpaceReclaimed = %d, want 1024", rep.SpaceReclaimed)
	}
	res := rep.Find(CategoryContainers)
	if res == nil || len(res.Deleted) != 1 || res.Deleted[0] != "abc123" {
		t.Fatalf("containers result = %+v", res)
	}
	if !runner.contains("label!=tengiz-app") {
		t.Error("container prune missing label!=tengiz-app protection filter")
	}
}

func TestManagerRunImagesPrunesDanglingAndKeepsPerApp(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)
	if err := store.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveApp(types.AppEntry{Name: "other", Config: types.AppConfig{Name: "other"}}); err != nil {
		t.Fatal(err)
	}

	retainer := &fakeRetainer{}
	runner := &fakeRunner{out: map[string][]byte{
		"image prune": []byte(`{"Deleted":["sha256:1"],"SpaceReclaimed":2048}`),
	}}
	m := NewWithEnv(retainer, dir, "production")
	m.run = runner.run

	rep, err := m.Run(context.Background(), Options{Categories: []Category{CategoryImages}, KeepImages: 5})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.SpaceReclaimed != 2048 {
		t.Errorf("SpaceReclaimed = %d, want 2048", rep.SpaceReclaimed)
	}
	if got := retainer.names(); len(got) != 2 || got[0] != "myapp" || got[1] != "other" {
		t.Fatalf("KeepLastNImages calls = %v, want [myapp other]", got)
	}
	if !runner.contains("dangling=true") {
		t.Error("image prune missing dangling=true filter")
	}
}

func TestManagerRunDryRunDoesNotPrune(t *testing.T) {
	runner := &fakeRunner{out: map[string][]byte{
		"ps -aq":     []byte("abc\ndef\n"),
		"images -f":  []byte("sha256:1\nsha256:2\n"),
		"volume ls":  []byte("vol1\n"),
		"network ls": []byte("net1\n"),
		"builder du": []byte("52428800\n"),
	}}
	m := NewWithEnv(nil, t.TempDir(), "production")
	m.run = runner.run

	rep, err := m.Run(context.Background(), Options{DryRun: true, Categories: AllCategories, KeepImages: 5})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.DryRun {
		t.Error("report.DryRun = false, want true")
	}
	if runner.contains("prune") || runner.contains("rmi") {
		t.Errorf("dry-run executed a destructive docker command: %v", runner.calls)
	}
	if got := rep.Find(CategoryContainers); got == nil || len(got.Deleted) != 2 {
		t.Fatalf("containers dry-run result = %+v", got)
	}
	if got := rep.Find(CategoryCache); got == nil || got.SpaceReclaimed != 52428800 {
		t.Fatalf("cache dry-run result = %+v", got)
	}
	if rep.SpaceReclaimed != 0 {
		t.Errorf("dry-run report.SpaceReclaimed = %d, want 0", rep.SpaceReclaimed)
	}
}

func TestManagerRunPropagatesError(t *testing.T) {
	runner := &fakeRunner{err: context.DeadlineExceeded}
	m := NewWithEnv(nil, t.TempDir(), "production")
	m.run = runner.run

	_, err := m.Run(context.Background(), Options{Categories: []Category{CategoryContainers}})
	if err == nil {
		t.Fatal("expected error from runner, got nil")
	}
}

func TestManagerDiskUsage(t *testing.T) {
	runner := &fakeRunner{out: map[string][]byte{
		"system df": []byte(`{"Containers":1,"Images":2,"Volumes":1,"BuildCache":0,"ContainersSize":100,"ImagesSize":200,"VolumesSize":50,"BuildCacheSize":0}`),
	}}
	m := NewWithEnv(nil, t.TempDir(), "production")
	m.run = runner.run

	du, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}
	if du.TotalSize() != 350 {
		t.Fatalf("TotalSize() = %d, want 350", du.TotalSize())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL with `undefined: Options`, `undefined: NewWithEnv`, `undefined: ImageRetainer` (in `internal/runtime`).

- [ ] **Step 3: Add `ImageRetainer` to the runtime package**

Modify `internal/runtime/runtime.go` — add this interface above the `Manager` interface (near line 31):

```go
type ImageRetainer interface {
	KeepLastNImages(ctx context.Context, appName string, n int) error
}
```

`runtime.Manager` already embeds this method (line 36), and both `NewDocker()` and `NewStub()` return values satisfy `ImageRetainer`. No other runtime changes needed.

- [ ] **Step 4: Write the implementation**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

type Options struct {
	DryRun     bool
	KeepImages int
	Categories []Category
}

type dockerRunner func(ctx context.Context, args ...string) ([]byte, error)

type Manager struct {
	dataDir string
	env     string
	rt      runtime.ImageRetainer
	run     dockerRunner
}

func NewWithEnv(rt runtime.ImageRetainer, dataDir, env string) *Manager {
	return &Manager{
		dataDir: dataDir,
		env:     env,
		rt:      rt,
		run: func(ctx context.Context, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, "docker", args...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return out, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
			}
			return out, nil
		},
	}
}

func (m *Manager) Run(ctx context.Context, opts Options) (*Report, error) {
	report := &Report{DryRun: opts.DryRun}
	for _, cat := range opts.Categories {
		res, err := m.runCategory(ctx, cat, opts)
		if err != nil {
			return nil, err
		}
		report.Results = append(report.Results, res)
		if !opts.DryRun {
			report.SpaceReclaimed += res.SpaceReclaimed
		}
	}
	return report, nil
}

func (m *Manager) runCategory(ctx context.Context, cat Category, opts Options) (*CategoryResult, error) {
	res := &CategoryResult{Category: cat}
	switch cat {
	case CategoryContainers:
		filter := "label!=tengiz-app"
		if opts.DryRun {
			out, err := m.run(ctx, "ps", "-aq", "--filter", "status=exited", "--filter", filter)
			if err != nil {
				return nil, err
			}
			res.Deleted = listLines(out)
			return res, nil
		}
		out, err := m.run(ctx, "container", "prune", "-f", "--format", "{{json .}}", "--filter", filter)
		if err != nil {
			return nil, err
		}
		pr := parsePruneResult(out)
		res.Deleted, res.SpaceReclaimed = pr.Deleted, pr.SpaceReclaimed
		return res, nil
	case CategoryImages:
		if opts.DryRun {
			out, err := m.run(ctx, "images", "-f", "dangling=true", "--format", "{{.ID}}")
			if err != nil {
				return nil, err
			}
			res.Deleted = listLines(out)
			return res, nil
		}
		out, err := m.run(ctx, "image", "prune", "-f", "--filter", "dangling=true", "--format", "{{json .}}")
		if err != nil {
			return nil, err
		}
		pr := parsePruneResult(out)
		res.Deleted, res.SpaceReclaimed = pr.Deleted, pr.SpaceReclaimed
		if opts.KeepImages > 0 {
			m.retainAppImages(ctx, opts.KeepImages)
		}
		return res, nil
	case CategoryVolumes:
		if opts.DryRun {
			out, err := m.run(ctx, "volume", "ls", "-f", "dangling=true", "--format", "{{.Name}}")
			if err != nil {
				return nil, err
			}
			res.Deleted = listLines(out)
			return res, nil
		}
		out, err := m.run(ctx, "volume", "prune", "-f", "--format", "{{json .}}")
		if err != nil {
			return nil, err
		}
		pr := parsePruneResult(out)
		res.Deleted, res.SpaceReclaimed = pr.Deleted, pr.SpaceReclaimed
		return res, nil
	case CategoryNetworks:
		if opts.DryRun {
			out, err := m.run(ctx, "network", "ls", "-f", "dangling=true", "--format", "{{.ID}}")
			if err != nil {
				return nil, err
			}
			res.Deleted = listLines(out)
			return res, nil
		}
		out, err := m.run(ctx, "network", "prune", "-f", "--format", "{{json .}}")
		if err != nil {
			return nil, err
		}
		pr := parsePruneResult(out)
		res.Deleted, res.SpaceReclaimed = pr.Deleted, pr.SpaceReclaimed
		return res, nil
	case CategoryCache:
		if opts.DryRun {
			out, err := m.run(ctx, "builder", "du", "--format", "{{.TotalSize}}")
			if err != nil {
				return nil, err
			}
			fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &res.SpaceReclaimed)
			return res, nil
		}
		out, err := m.run(ctx, "builder", "prune", "-f", "--format", "{{json .}}")
		if err != nil {
			return nil, err
		}
		pr := parsePruneResult(out)
		res.Deleted, res.SpaceReclaimed = pr.Deleted, pr.SpaceReclaimed
		return res, nil
	}
	return nil, fmt.Errorf("unhandled category %q", cat)
}

func (m *Manager) retainAppImages(ctx context.Context, keep int) {
	store := config.NewStoreWithEnv(m.dataDir, m.env)
	apps, err := store.ListApps()
	if err != nil {
		log.Printf("[cleanup] list apps: %v", err)
		return
	}
	for _, app := range apps {
		if err := m.rt.KeepLastNImages(ctx, app.Name, keep); err != nil {
			log.Printf("[cleanup] keep last %d images for %s: %v", keep, app.Name, err)
		}
	}
}

func (m *Manager) DiskUsage(ctx context.Context) (*DiskUsage, error) {
	out, err := m.run(ctx, "system", "df", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	return parseDfOutput(out)
}

func listLines(out []byte) []string {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS

- [ ] **Step 6: Run runtime tests to confirm no breakage**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: PASS (only the `ImageRetainer` interface was added)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup manager for docker housekeeping with dry-run and image retention"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go:76` (after `rootCmd.AddCommand(notificationCmd)` in `init()`)

**Interfaces:**
- Consumes: from Task 2 — `cleanup.NewWithEnv(rt runtime.ImageRetainer, dataDir, env string)`, `(*cleanup.Manager).Run(ctx, cleanup.Options)`, `cleanup.AllCategories`, `cleanup.ParseCategory`, `cleanup.FormatBytes`. From root.go — `dataDir`, `getEnv(cmd)`, `captureOutput` (test helper).
- Produces: `cleanupCmd` (Cobra command `cleanup [category]`), `printCleanupReport(report *cleanup.Report, env string)`, `sizeSuffix(size int64, verb string) string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/cleanup"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupHasFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "keep-images", "interval", "env"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupInvalidCategory(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "bogus"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for unknown category")
	}
}

func TestCleanupValidCategoryParses(t *testing.T) {
	cat, err := cleanup.ParseCategory("images")
	if err != nil {
		t.Fatalf("ParseCategory(images): %v", err)
	}
	if cat != cleanup.CategoryImages {
		t.Fatalf("cat = %q, want images", cat)
	}
}

func TestPrintCleanupReport(t *testing.T) {
	report := &cleanup.Report{
		DryRun: true,
		Results: []*cleanup.CategoryResult{
			{Category: cleanup.CategoryContainers, Deleted: []string{"abc"}},
			{Category: cleanup.CategoryImages, Deleted: []string{"sha256:1", "sha256:2"}, SpaceReclaimed: 2048},
			{Category: cleanup.CategoryCache, SpaceReclaimed: 52428800},
		},
	}
	output := captureOutput(func() { printCleanupReport(report, "production") })
	for _, want := range []string{
		"[tengiz] cleanup (dry-run) — environment: production",
		"containers: 1 would be removed",
		"images: 2 would be removed (2.0KB)",
		"build cache: 50.0MB would be freed",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}

func TestPrintCleanupReportReal(t *testing.T) {
	report := &cleanup.Report{
		Results: []*cleanup.CategoryResult{
			{Category: cleanup.CategoryContainers, Deleted: []string{"abc"}, SpaceReclaimed: 1024},
			{Category: cleanup.CategoryNetworks, Deleted: nil},
		},
		SpaceReclaimed: 1024,
	}
	output := captureOutput(func() { printCleanupReport(report, "staging") })
	for _, want := range []string{
		"[tengiz] cleanup — environment: staging",
		"containers: 1 removed (1.0KB)",
		"networks: 0 removed",
		"total reclaimed: 1.0KB",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup|TestPrintCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: printCleanupReport`

- [ ] **Step 3: Write the CLI command implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup [category]",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Removes stopped non-Tengiz containers, dangling images, unused volumes,
networks, and build cache. Containers managed by Tengiz (labeled
tengiz-app) are always protected from removal.

Categories: containers, images, volumes, networks, cache (default: all).

--dry-run previews what would be removed without deleting anything.
--keep-images sets how many old images per app to retain (default 5).
--interval re-runs cleanup periodically until interrupted (e.g. 1h).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		keep, _ := cmd.Flags().GetInt("keep-images")
		interval, _ := cmd.Flags().GetDuration("interval")

		categories := cleanup.AllCategories
		if len(args) == 1 {
			cat, err := cleanup.ParseCategory(args[0])
			if err != nil {
				return err
			}
			categories = []cleanup.Category{cat}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		mgr := cleanup.NewWithEnv(rt, dataDir, env)

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()

		for {
			report, err := mgr.Run(ctx, cleanup.Options{
				DryRun:     dryRun,
				KeepImages: keep,
				Categories: categories,
			})
			if err != nil {
				return err
			}
			printCleanupReport(report, env)
			if interval <= 0 {
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(interval):
			}
		}
	},
}

func printCleanupReport(report *cleanup.Report, env string) {
	mode := "cleanup"
	if report.DryRun {
		mode = "cleanup (dry-run)"
	}
	fmt.Printf("[tengiz] %s — environment: %s\n", mode, env)
	for _, res := range report.Results {
		verb := "removed"
		if report.DryRun {
			verb = "would be removed"
		}
		switch res.Category {
		case cleanup.CategoryContainers:
			fmt.Printf("[tengiz] containers: %d %s\n", len(res.Deleted), sizeSuffix(res.SpaceReclaimed, verb))
		case cleanup.CategoryImages:
			fmt.Printf("[tengiz] images: %d %s\n", len(res.Deleted), sizeSuffix(res.SpaceReclaimed, verb))
		case cleanup.CategoryVolumes:
			fmt.Printf("[tengiz] volumes: %d %s\n", len(res.Deleted), sizeSuffix(res.SpaceReclaimed, verb))
		case cleanup.CategoryNetworks:
			fmt.Printf("[tengiz] networks: %d %s\n", len(res.Deleted), sizeSuffix(res.SpaceReclaimed, verb))
		case cleanup.CategoryCache:
			if res.SpaceReclaimed > 0 {
				verb2 := "freed"
				if report.DryRun {
					verb2 = "would be freed"
				}
				fmt.Printf("[tengiz] build cache: %s %s\n", cleanup.FormatBytes(res.SpaceReclaimed), verb2)
			} else {
				fmt.Printf("[tengiz] build cache: clean\n")
			}
		}
	}
	if report.SpaceReclaimed > 0 {
		fmt.Printf("[tengiz] total reclaimed: %s\n", cleanup.FormatBytes(report.SpaceReclaimed))
	}
}

func sizeSuffix(size int64, verb string) string {
	if size <= 0 {
		return verb
	}
	return fmt.Sprintf("%s (%s)", verb, cleanup.FormatBytes(size))
}
```

- [ ] **Step 4: Register the command in `init()`**

Modify `internal/cli/root.go` — after line 76 (`rootCmd.AddCommand(notificationCmd)`), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without deleting anything")
	cleanupCmd.Flags().Int("keep-images", 5, "number of old images to retain per app")
	cleanupCmd.Flags().Duration("interval", 0, "re-run cleanup periodically (e.g. 1h); 0 = run once")
	cleanupCmd.Flags().String("env", "production", "deployment environment (e.g. production, staging, dev)")
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanup|TestPrintCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 6: Build and vet**

Run: `go build ./... && go vet ./...`

Expected: no errors

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Documentation and full verification

**Files:**
- Modify: `README.md:103-115` area (add section after line 237, before `### `tengiz domain``)
- Modify: `AGENTS.md:36-66` (CLI list) and `AGENTS.md:9-27` (architecture table)
- Modify: `docs/FUTURES_FEATURES.md:19` (mark feature #6 implemented) and the Implemented list

**Interfaces:**
- Consumes: the command shape finalized in Task 3 (`tengiz cleanup [category]`, flags `--dry-run`, `--keep-images`, `--interval`, `--env`)

- [ ] **Step 1: Update README CLI Reference**

Add this section to `README.md` right before `### `tengiz domain`` (line 238):

```markdown
### `tengiz cleanup [category]`

Prune unused Docker resources to reclaim disk space. Tengiz-managed containers (labeled `tengiz-app`) are always protected — the container prune only removes stopped non-Tengiz containers. Volumes and networks in use by any container are never removed by Docker.

Categories: `containers`, `images`, `volumes`, `networks`, `cache`. Default: all.

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview what would be removed without deleting anything |
| `--keep-images <n>` | Number of old images to retain per app (default `5`) |
| `--interval <dur>` | Re-run cleanup periodically (e.g. `1h`); `0` runs once (default `0`) |
| `--env <env>` | Environment scope for per-app image retention |

Examples:

```bash
tengiz cleanup                # prune all categories
tengiz cleanup images         # only dangling + old-version images
tengiz cleanup --dry-run      # preview without deleting
tengiz cleanup --interval 1h  # run hourly until interrupted
```
```

- [ ] **Step 2: Update AGENTS.md CLI list and architecture table**

Add to the CLI block in `AGENTS.md` (after the `tengiz notification show` line, line 65):

```
tengiz cleanup [category]     → prune unused Docker resources (containers/images/volumes/networks/cache) with tengiz-app label protection (--dry-run, --keep-images, --interval)
```

Add a row to the architecture table in `AGENTS.md` (after the `idle` row):

```
| `cleanup` | Docker housekeeping. Exec-based `docker <category> prune` + `docker system df` via an injectable runner. Protects `tengiz-app` labeled containers. Reuses `runtime.KeepLastNImages` for per-app image retention. Env-aware via `NewWithEnv`. |
```

- [ ] **Step 3: Mark the feature implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`, change line 19 from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "✅ Implemented Features (Not Pending)" table:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-17) |
```

- [ ] **Step 4: Run the full test suite**

Run: `go build ./...`

Expected: builds successfully

Run: `go vet ./...`

Expected: no issues

Run: `go test ./... -v -count=1`

Expected: All PASS (proxy and idle tests may take longer; results must not regress)

- [ ] **Step 5: Manual smoke test (if docker is available)**

Run: `go build -o tengiz . && ./tengiz cleanup --dry-run`

Expected: prints a dry-run report for all categories with counts and sizes, no resources deleted.

Run: `./tengiz cleanup images --keep-images 3`

Expected: prunes dangling images and keeps the 3 newest images per app.

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document docker housekeeping cleanup command and mark feature implemented"
```

---

## Self-Review

**1. Spec coverage** — `docs/FUTURES_FEATURES.md` feature #6 requires: label-based protection of Tengiz containers (Task 2 container filter `label!=tengiz-app`), cleanup of unused volumes/networks/containers/images (Task 2 categories), and a `tengiz cleanup` command (Task 3). Coolify's `DockerCleanupJob` periodic aspect is covered by the optional `--interval` loop in Task 3; manual/cron usage is documented in README. No gaps.

**2. Placeholder scan** — No "TBD"/"TODO"/"Similar to Task N"/"add appropriate error handling" patterns. Every code step contains complete, copy-pastable code and every test step includes the full test body.

**3. Type consistency** —
- `cleanup.NewWithEnv(rt runtime.ImageRetainer, dataDir, env string) *Manager` — used identically in Task 2 tests and Task 3 CLI.
- `runtime.ImageRetainer` (Task 2 Step 3) — satisfied by `runtime.Manager`; used as the constructor param type everywhere.
- `cleanup.Options{DryRun, KeepImages, Categories}` — Task 2 defines, Task 3 constructs with the same field names.
- `(*cleanup.Report).Find(cat)` and `(*Report).TotalDeleted()` — defined in Task 1, used in Task 2 tests and Task 3 (`printCleanupReport` iterates `report.Results` directly, matching the `Results []*CategoryResult` field).
- `cleanup.FormatBytes` — exported in Task 1, used in Task 3 report printer; `humanBytes` was renamed to the exported `FormatBytes` consistently.
- `printCleanupReport(report *cleanup.Report, env string)` — signature matches both the CLI call and tests.
- `sizeSuffix(size int64, verb string) string` — Task 3 defines and uses it for the four object categories; the cache category uses its own branch.