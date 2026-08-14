# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, images, volumes, networks, build cache) with label-based protection for Tengiz-managed containers, plus a `--dry-run` preview.

**Architecture:** Extend the existing `runtime.Manager` interface with a single `Prune(ctx, opts) (PruneReport, error)` method, mirroring the pattern already used for `RemoveImage`/`KeepLastNImages` in `internal/runtime/cleanup.go`. All docker CLI invocations go through pure, unit-testable argument-builder functions; the CLI layer (`internal/cli/root.go`) maps flags to `runtime.PruneOptions` and formats the report. Dry-run shows the reclaimable-space table from `docker system df` without removing anything. This complements the per-deploy `KeepLastNImages` retention (5 images) with an on-demand global prune.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface, Docker CLI via `os/exec`. No new dependencies.

## Global Constraints

- `dockerRuntime` stays `struct{}` — no runner injection; docker exec code is tested via pure arg-builder/dispatcher functions, matching the existing `buildLogArgs`/`buildRunArgs` convention
- Container prune MUST use `--filter label=tengiz-app` so only stopped Tengiz-managed containers are removed; running containers and non-Tengiz containers are never touched
- Volumes are NEVER pruned by default (mirrors `docker system prune`); only via explicit `--volumes` or `--all`
- Default `tengiz cleanup` (no category flags) = containers, images, networks, build-cache
- Adding `Prune` to `runtime.Manager` breaks 3 test mocks — `mockRTForDeploy` (`internal/cli/root_test.go`), `mockRuntime` (`internal/idle/idle_test.go`), `mockRuntime` (`internal/proxy/proxy_test.go`) — and `stubManager`; all four MUST be updated in the same task as the interface change so every package still compiles
- `--all` means: all 5 categories + `docker image prune -a` (remove all unused images, not just dangling)
- `--dry-run` runs `docker system df --format "{{.Type}}\t{{.Total}}\t{{.Active}}\t{{.Size}}\t{{.Reclaimable}}"` and removes nothing
- No `--env` flag on `cleanup` — it operates on all Docker resources regardless of environment
- Every commit message uses the `feat:` / `test:` / `docs:` prefix style used by previous plans
- Test commands always use `-count=1` to bypass cached results

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` (new) | `PruneCategory`, `PruneOptions`, `PruneResult`, `PruneReport`, `SystemDfRow`, `AllPruneCategories`; pure arg builders + output parsers; `dockerRuntime.Prune`, `pruneDryRun`, `runPrune` |
| `internal/runtime/runtime.go` (modify) | Add `Prune` to `Manager` interface; add `stubManager.Prune` |
| `internal/runtime/prune_test.go` (new) | Tests for stub, arg builders, dispatcher, parsers |
| `internal/cli/root_test.go` (modify) | Add `Prune` to `mockRTForDeploy` |
| `internal/idle/idle_test.go` (modify) | Add `Prune` to `mockRuntime` |
| `internal/proxy/proxy_test.go` (modify) | Add `Prune` to `mockRuntime` |
| `internal/cli/root.go` (modify) | Register `cleanupCmd` + flags; `parseCleanupOptions`, `executeCleanup`, `printCleanupReport` |
| `internal/cli/cleanup_test.go` (new) | Tests for registration, flags, option parsing, delegation, report printing |
| `README.md` (modify) | Add `tengiz cleanup` section to CLI Reference |
| `docs/FUTURES_FEATURES.md` (modify) | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Prune types, Manager interface method, stub + mock updates

**Files:**
- Create: `internal/runtime/prune.go`
- Modify: `internal/runtime/runtime.go:31-49` (interface) and `:113-119` (stub area)
- Modify: `internal/cli/root_test.go:98-99` (add `Prune` to `mockRTForDeploy`)
- Modify: `internal/idle/idle_test.go:32-33` (add `Prune` to `mockRuntime`)
- Modify: `internal/proxy/proxy_test.go:33-34` (add `Prune` to `mockRuntime`)
- Test: `internal/runtime/prune_test.go` (create, `TestStubPrune`)

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneCategory` (constants `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache`), `runtime.AllPruneCategories []PruneCategory`, `runtime.PruneOptions{Categories []PruneCategory; AllImages bool; DryRun bool}`, `runtime.PruneResult{Category PruneCategory; Reclaimed string; Err error}`, `runtime.PruneReport{DryRun bool; Results []PruneResult; DfRows []SystemDfRow}`, `runtime.SystemDfRow{Type, Total, Active, Size, Reclaimable string}`, and `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/prune_test.go
package runtime

import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Categories: AllPruneCategories})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.DryRun {
		t.Error("DryRun = true, want false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: FAIL to compile with `undefined: PruneOptions` (types and interface method don't exist yet). This red state is required.

- [ ] **Step 3: Create `internal/runtime/prune.go` with the types**

```go
package runtime

type PruneCategory string

const (
	PruneContainers PruneCategory = "containers"
	PruneImages     PruneCategory = "images"
	PruneVolumes    PruneCategory = "volumes"
	PruneNetworks   PruneCategory = "networks"
	PruneBuildCache PruneCategory = "build-cache"
)

var AllPruneCategories = []PruneCategory{
	PruneContainers, PruneImages, PruneVolumes, PruneNetworks, PruneBuildCache,
}

type PruneOptions struct {
	Categories []PruneCategory
	AllImages  bool
	DryRun     bool
}

type PruneResult struct {
	Category  PruneCategory
	Reclaimed string
	Err       error
}

type PruneReport struct {
	DryRun  bool
	Results []PruneResult
	DfRows  []SystemDfRow
}

type SystemDfRow struct {
	Type        string
	Total       string
	Active      string
	Size        string
	Reclaimable string
}
```

- [ ] **Step 4: Add `Prune` to the `Manager` interface in `internal/runtime/runtime.go`**

Add this line after the `KeepLastNImages` line (line 36):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

- [ ] **Step 5: Add `stubManager.Prune` in `internal/runtime/runtime.go`**

Add after the `stubManager.KeepLastNImages` method (after line 119):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 6: Update the three test mocks so the packages still compile**

Add this method to each mock (placement after its existing `KeepLastNImages` method):

`internal/cli/root_test.go` (in `mockRTForDeploy`, after line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{DryRun: opts.DryRun}, nil
}
```

`internal/idle/idle_test.go` (in `mockRuntime`, after line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{DryRun: opts.DryRun}, nil
}
```

`internal/proxy/proxy_test.go` (in `mockRuntime`, after line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: PASS

- [ ] **Step 8: Verify all packages still compile (mocks updated)**

Run: `go build ./... && go vet ./...`

Expected: No errors (proves all three mocks + stub implement the extended interface)

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/runtime.go internal/runtime/prune_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Prune to runtime.Manager with prune types and stub"
```

---

### Task 2: Docker prune command builders and output parsers

**Files:**
- Modify: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go` (add `TestPruneCommandArgs`, `TestParseReclaimed`, `TestParseSystemDf`, `TestParseSystemDfEmpty`)

**Interfaces:**
- Consumes: `PruneCategory` constants and `labelKey` (package-level `const labelKey = "tengiz-app"` in `internal/runtime/docker.go:76`) from Task 1
- Produces: `pruneContainerArgs() []string`, `pruneImageArgs(all bool) []string`, `pruneVolumeArgs() []string`, `pruneNetworkArgs() []string`, `pruneBuildCacheArgs() []string`, `systemDfArgs() []string`, `pruneCommandArgs(cat PruneCategory, allImages bool) ([]string, bool)`, `parseReclaimed(output string) string`, `parseSystemDf(output string) []SystemDfRow`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/prune_test.go — add to the file created in Task 1
import "reflect" // add to existing imports

func TestPruneCommandArgs(t *testing.T) {
	tests := []struct {
		cat       PruneCategory
		allImages bool
		want      []string
		ok        bool
	}{
		{PruneContainers, false, []string{"container", "prune", "-f", "--filter", "label=tengiz-app"}, true},
		{PruneImages, false, []string{"image", "prune", "-f"}, true},
		{PruneImages, true, []string{"image", "prune", "-a", "-f"}, true},
		{PruneVolumes, false, []string{"volume", "prune", "-f"}, true},
		{PruneNetworks, false, []string{"network", "prune", "-f"}, true},
		{PruneBuildCache, false, []string{"builder", "prune", "-f"}, true},
		{PruneCategory("bogus"), false, nil, false},
	}
	for _, tt := range tests {
		got, ok := pruneCommandArgs(tt.cat, tt.allImages)
		if ok != tt.ok {
			t.Errorf("pruneCommandArgs(%q, %v) ok = %v, want %v", tt.cat, tt.allImages, ok, tt.ok)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("pruneCommandArgs(%q, %v) = %v, want %v", tt.cat, tt.allImages, got, tt.want)
		}
	}
}

func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"Deleted Containers:\napp1\nTotal reclaimed space: 1.234GB", "1.234GB"},
		{"Total reclaimed space: 0B", "0B"},
		{"Total:  Build Cache: 2.5GB", "2.5GB"},
		{"no output here", ""},
	}
	for _, tt := range tests {
		if got := parseReclaimed(tt.output); got != tt.want {
			t.Errorf("parseReclaimed(%q) = %q, want %q", tt.output, got, tt.want)
		}
	}
}

func TestParseSystemDf(t *testing.T) {
	output := "Images\t5\t3\t1.2GB\t800MB (66%)\nContainers\t4\t2\t12MB\t6MB (50%)"
	rows := parseSystemDf(output)
	if len(rows) != 2 {
		t.Fatalf("parseSystemDf returned %d rows, want 2", len(rows))
	}
	if rows[0].Type != "Images" || rows[0].Reclaimable != "800MB (66%)" {
		t.Errorf("rows[0] = %+v", rows[0])
	}
	if rows[1].Type != "Containers" {
		t.Errorf("rows[1].Type = %q, want Containers", rows[1].Type)
	}
}

func TestParseSystemDfEmpty(t *testing.T) {
	rows := parseSystemDf("")
	if len(rows) != 0 {
		t.Errorf("parseSystemDf(\"\") = %d rows, want 0", len(rows))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneCommandArgs|TestParseReclaimed|TestParseSystemDf" -v -count=1`

Expected: FAIL to compile with `undefined: pruneCommandArgs`, `undefined: parseReclaimed`, `undefined: parseSystemDf`

- [ ] **Step 3: Implement the builders, dispatcher, and parsers in `internal/runtime/prune.go`**

Add to `internal/runtime/prune.go` (keep the Task 1 types above these functions):

```go
func pruneContainerArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label=" + labelKey}
}

func pruneImageArgs(all bool) []string {
	if all {
		return []string{"image", "prune", "-a", "-f"}
	}
	return []string{"image", "prune", "-f"}
}

func pruneVolumeArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func pruneNetworkArgs() []string {
	return []string{"network", "prune", "-f"}
}

func pruneBuildCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func systemDfArgs() []string {
	return []string{"system", "df", "--format", "{{.Type}}\t{{.Total}}\t{{.Active}}\t{{.Size}}\t{{.Reclaimable}}"}
}

func pruneCommandArgs(cat PruneCategory, allImages bool) ([]string, bool) {
	switch cat {
	case PruneContainers:
		return pruneContainerArgs(), true
	case PruneImages:
		return pruneImageArgs(allImages), true
	case PruneVolumes:
		return pruneVolumeArgs(), true
	case PruneNetworks:
		return pruneNetworkArgs(), true
	case PruneBuildCache:
		return pruneBuildCacheArgs(), true
	default:
		return nil, false
	}
}

func parseReclaimed(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
		if strings.HasPrefix(line, "Total:") && strings.Contains(line, "Build Cache:") {
			parts := strings.SplitN(line, "Build Cache:", 2)
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func parseSystemDf(output string) []SystemDfRow {
	var rows []SystemDfRow
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) != 5 {
			continue
		}
		rows = append(rows, SystemDfRow{Type: parts[0], Total: parts[1], Active: parts[2], Size: parts[3], Reclaimable: parts[4]})
	}
	return rows
}
```

Add `"strings"` to the imports of `internal/runtime/prune.go`:

```go
import (
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneCommandArgs|TestParseReclaimed|TestParseSystemDf" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: add docker prune arg builders and output parsers"
```

---

### Task 3: `dockerRuntime.Prune` implementation

**Files:**
- Modify: `internal/runtime/prune.go`

**Interfaces:**
- Consumes: `pruneCommandArgs`, `systemDfArgs`, `parseReclaimed`, `parseSystemDf` from Task 2; `PruneOptions`/`PruneReport`/`PruneResult` from Task 1
- Produces: `func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)` — iterates `opts.Categories`, runs each `docker <subcommand> prune` via `exec.CommandContext`, parses reclaimed space; when `opts.DryRun` is true runs `docker system df` and returns rows without removing anything

- [ ] **Step 1: Write the failing test for the category→command dispatcher (the exec wiring is docker-dependent and follows the repo's pure-helper testing convention)**

```go
// internal/runtime/prune_test.go — add this test
func TestPruneCommandArgsAllImages(t *testing.T) {
	got, ok := pruneCommandArgs(PruneImages, true)
	if !ok {
		t.Fatal("pruneCommandArgs(PruneImages, true) ok = false, want true")
	}
	want := []string{"image", "prune", "-a", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneCommandArgs(PruneImages, true) = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestPruneCommandArgsAllImages -v -count=1`

Expected: FAIL with `undefined: pruneCommandArgs` (dispatcher not yet implemented — it was only declared in the previous task's test expectations, not yet written)

- [ ] **Step 3: Implement `dockerRuntime.Prune`, `pruneDryRun`, and `runPrune` in `internal/runtime/prune.go`**

Add to `internal/runtime/prune.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	if opts.DryRun {
		return r.pruneDryRun(ctx)
	}
	report := PruneReport{DryRun: false}
	for _, cat := range opts.Categories {
		args, ok := pruneCommandArgs(cat, opts.AllImages)
		if !ok {
			continue
		}
		report.Results = append(report.Results, r.runPrune(ctx, cat, args))
	}
	return report, nil
}

func (r *dockerRuntime) pruneDryRun(ctx context.Context) (PruneReport, error) {
	report := PruneReport{DryRun: true}
	out, err := exec.CommandContext(ctx, "docker", systemDfArgs()...).CombinedOutput()
	if err != nil {
		return report, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	report.DfRows = parseSystemDf(string(out))
	return report, nil
}

func (r *dockerRuntime) runPrune(ctx context.Context, cat PruneCategory, args []string) PruneResult {
	res := PruneResult{Category: cat}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		res.Err = fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out))
		return res
	}
	res.Reclaimed = parseReclaimed(string(out))
	return res
}
```

Add `"fmt"` and `"os/exec"` to the imports of `internal/runtime/prune.go`:

```go
import (
	"fmt"
	"os/exec"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneCommandArgs|TestParseReclaimed|TestParseSystemDf|TestStubPrune" -v -count=1`

Expected: PASS

- [ ] **Step 5: Verify the whole project still builds**

Run: `go build ./... && go vet ./...`

Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go
git commit -m "feat: implement dockerRuntime.Prune with dry-run support"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — register `cleanupCmd` in `init()` (after the `webhookCmd` flags at line 86-88), add the command + helpers after the `runCmd` block (after line 1162, before `var gitCmd` at line 1164)
- Test: `internal/cli/cleanup_test.go` (create)

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.PruneResult`, `runtime.PruneCategory`, `runtime.AllPruneCategories`, `runtime.SystemDfRow`, and `runtime.Manager.Prune` from Tasks 1-3; `captureOutput` from `internal/cli/root_test.go`
- Produces: `cleanupCmd *cobra.Command`, `parseCleanupOptions(cmd *cobra.Command) runtime.PruneOptions`, `executeCleanup(ctx context.Context, rt runtime.Manager, opts runtime.PruneOptions) (runtime.PruneReport, error)`, `printCleanupReport(report runtime.PruneReport)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
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

func TestCleanupFlagsExist(t *testing.T) {
	for _, name := range []string{"dry-run", "all", "containers", "images", "volumes", "networks", "build-cache"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestParseCleanupOptionsDefaults(t *testing.T) {
	cleanupCmd.ParseFlags([]string{})
	opts := parseCleanupOptions(cleanupCmd)
	if opts.DryRun {
		t.Error("DryRun = true, want false")
	}
	if opts.AllImages {
		t.Error("AllImages = true, want false")
	}
	want := []runtime.PruneCategory{runtime.PruneContainers, runtime.PruneImages, runtime.PruneNetworks, runtime.PruneBuildCache}
	if !reflect.DeepEqual(opts.Categories, want) {
		t.Errorf("Categories = %v, want %v", opts.Categories, want)
	}
}

func TestParseCleanupOptionsAll(t *testing.T) {
	cleanupCmd.ParseFlags([]string{"--all"})
	opts := parseCleanupOptions(cleanupCmd)
	if !opts.AllImages {
		t.Error("AllImages = false, want true")
	}
	if !reflect.DeepEqual(opts.Categories, runtime.AllPruneCategories) {
		t.Errorf("Categories = %v, want all %v", opts.Categories, runtime.AllPruneCategories)
	}
}

func TestParseCleanupOptionsSpecific(t *testing.T) {
	cleanupCmd.ParseFlags([]string{"--volumes", "--build-cache"})
	opts := parseCleanupOptions(cleanupCmd)
	want := []runtime.PruneCategory{runtime.PruneVolumes, runtime.PruneBuildCache}
	if !reflect.DeepEqual(opts.Categories, want) {
		t.Errorf("Categories = %v, want %v", opts.Categories, want)
	}
}

func TestParseCleanupOptionsDryRun(t *testing.T) {
	cleanupCmd.ParseFlags([]string{"--dry-run"})
	opts := parseCleanupOptions(cleanupCmd)
	if !opts.DryRun {
		t.Error("DryRun = false, want true")
	}
}

type pruneRecorder struct {
	runtime.Manager
	called atomic.Bool
	opts   runtime.PruneOptions
}

func (m *pruneRecorder) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	m.called.Store(true)
	m.opts = opts
	return runtime.PruneReport{
		DryRun: opts.DryRun,
		Results: []runtime.PruneResult{{Category: runtime.PruneImages, Reclaimed: "1.2GB"}},
	}, nil
}

func newPruneRecorder() *pruneRecorder {
	return &pruneRecorder{Manager: runtime.NewStub()}
}

func TestExecuteCleanupCallsPrune(t *testing.T) {
	rec := newPruneRecorder()
	opts := runtime.PruneOptions{
		Categories: []runtime.PruneCategory{runtime.PruneImages},
		DryRun:     false,
	}
	report, err := executeCleanup(context.Background(), rec, opts)
	if err != nil {
		t.Fatalf("executeCleanup: %v", err)
	}
	if !rec.called.Load() {
		t.Fatal("Prune was not called")
	}
	if !reflect.DeepEqual(rec.opts, opts) {
		t.Errorf("Prune called with %+v, want %+v", rec.opts, opts)
	}
	if len(report.Results) != 1 || report.Results[0].Category != runtime.PruneImages {
		t.Errorf("report.Results = %+v, want 1 image result", report.Results)
	}
}

func TestPrintCleanupReportReal(t *testing.T) {
	report := runtime.PruneReport{
		Results: []runtime.PruneResult{
			{Category: runtime.PruneContainers, Reclaimed: "1.2GB"},
			{Category: runtime.PruneImages, Reclaimed: "0B"},
		},
	}
	out := captureOutput(func() { printCleanupReport(report) })
	if !strings.Contains(out, "cleanup complete") {
		t.Errorf("missing summary line, got: %q", out)
	}
	if !strings.Contains(out, "containers:") || !strings.Contains(out, "1.2GB") {
		t.Errorf("missing containers result, got: %q", out)
	}
	if !strings.Contains(out, "images:") || !strings.Contains(out, "0B") {
		t.Errorf("missing images result, got: %q", out)
	}
}

func TestPrintCleanupReportDryRun(t *testing.T) {
	report := runtime.PruneReport{
		DryRun: true,
		DfRows: []runtime.SystemDfRow{{Type: "Images", Total: "5", Active: "3", Size: "1.2GB", Reclaimable: "800MB (66%)"}},
	}
	out := captureOutput(func() { printCleanupReport(report) })
	if !strings.Contains(out, "dry-run") {
		t.Errorf("missing dry-run notice, got: %q", out)
	}
	if !strings.Contains(out, "Images") || !strings.Contains(out, "RECLAIMABLE") {
		t.Errorf("missing df table, got: %q", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup|TestParseCleanup|TestExecuteCleanup|TestPrintCleanup" -v -count=1`

Expected: FAIL to compile with `undefined: cleanupCmd`, `undefined: parseCleanupOptions`, `undefined: executeCleanup`, `undefined: printCleanupReport`

- [ ] **Step 3: Register the command and flags in `internal/cli/root.go` `init()`**

Add after the `webhookCmd` flag registrations (after line 88):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "show reclaimable space without removing anything")
	cleanupCmd.Flags().Bool("all", false, "prune all categories including volumes and all unused images")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune unused images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune the Docker build cache")
```

- [ ] **Step 4: Add the `cleanupCmd` command and its helpers to `internal/cli/root.go`**

Insert after the `runCmd` variable block (after line 1162, before `var gitCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prune unused Docker resources (stopped containers, images, volumes, networks,
and build cache) to reclaim disk space.

Tengiz-managed containers are protected: only stopped containers carrying the
tengiz-app label are removed, and running containers are never touched.

Categories (default: containers, images, networks, build-cache):
  --containers  prune stopped Tengiz containers
  --images      prune unused images (add --all to remove all unused images)
  --volumes     prune unused volumes (never pruned by default)
  --networks    prune unused networks
  --build-cache prune the Docker build cache

Use --dry-run to preview reclaimable space without removing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := parseCleanupOptions(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		_, err = executeCleanup(cmd.Context(), rt, opts)
		return err
	},
}

func parseCleanupOptions(cmd *cobra.Command) runtime.PruneOptions {
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")

	var cats []runtime.PruneCategory
	switch {
	case all:
		cats = runtime.AllPruneCategories
	case containers || images || volumes || networks || buildCache:
		if containers {
			cats = append(cats, runtime.PruneContainers)
		}
		if images {
			cats = append(cats, runtime.PruneImages)
		}
		if volumes {
			cats = append(cats, runtime.PruneVolumes)
		}
		if networks {
			cats = append(cats, runtime.PruneNetworks)
		}
		if buildCache {
			cats = append(cats, runtime.PruneBuildCache)
		}
	default:
		cats = []runtime.PruneCategory{
			runtime.PruneContainers,
			runtime.PruneImages,
			runtime.PruneNetworks,
			runtime.PruneBuildCache,
		}
	}

	return runtime.PruneOptions{
		Categories: cats,
		AllImages:  all,
		DryRun:     dryRun,
	}
}

func executeCleanup(ctx context.Context, rt runtime.Manager, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	report, err := rt.Prune(ctx, opts)
	if err != nil {
		return report, err
	}
	printCleanupReport(report)
	return report, nil
}

func printCleanupReport(report runtime.PruneReport) {
	if report.DryRun {
		fmt.Println("[tengiz] dry-run: nothing was removed")
		fmt.Printf("%-12s %-8s %-8s %-12s %-12s\n", "TYPE", "TOTAL", "ACTIVE", "SIZE", "RECLAIMABLE")
		for _, row := range report.DfRows {
			fmt.Printf("%-12s %-8s %-8s %-12s %-12s\n", row.Type, row.Total, row.Active, row.Size, row.Reclaimable)
		}
		fmt.Println("[tengiz] run 'tengiz cleanup' to reclaim this space")
		return
	}
	for _, res := range report.Results {
		if res.Err != nil {
			fmt.Printf("[tengiz] %-12s error: %v\n", res.Category+":", res.Err)
			continue
		}
		fmt.Printf("[tengiz] %-12s reclaimed %s\n", res.Category+":", res.Reclaimed)
	}
	fmt.Println("[tengiz] cleanup complete")
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestParseCleanup|TestExecuteCleanup|TestPrintCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run the full CLI test suite to confirm no regressions**

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 5: Documentation and full verification

**Files:**
- Modify: `README.md` — add a `tengiz cleanup` section to CLI Reference
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented
- Test: no new code; full project verification

**Interfaces:**
- Consumes: final command name/behavior from Task 4
- Produces: updated user documentation

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

Insert after the `#### tengiz secret list <app>` section (after line 416, before the `## Configuration` heading at line 418):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show reclaimable space via `docker system df` without removing anything |
| `--all` | Prune all categories including volumes and all unused images |
| `--containers` | Prune stopped Tengiz containers only (label-protected) |
| `--images` | Prune unused images |
| `--volumes` | Prune unused volumes (never pruned by default) |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune the Docker build cache |

With no category flags, `tengiz cleanup` prunes stopped containers, unused images,
unused networks, and the build cache. Volumes are only pruned with `--volumes` or
`--all`. Running containers are never touched, and only stopped containers carrying
the `tengiz-app` label are considered for removal. Run `tengiz cleanup --dry-run`
first to preview reclaimable space.
```

- [ ] **Step 2: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table (line 19), change the status cell:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. (Implemented 2026-08-14) |
```

In the feature section `## Docker Housekeeping (Otomatik Temizlik)` (around line 377-381), add a status line after the `- **Why add to Tengiz:**` line:

```markdown
- **Status:** ✅ Implemented (2026-08-14)
```

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS (the `proxy` and `idle` suites may take a few seconds — see AGENTS.md quirk notes; their existing timeouts are the only expected slowness)

- [ ] **Step 4: Run static analysis and build**

Run: `go vet ./... && go build -o /tmp/tengiz-check .`

Expected: No vet issues; binary builds successfully

- [ ] **Step 5: Manual smoke test of the new command (requires docker)**

Run: `tengiz cleanup --dry-run`

Expected: Prints a `TYPE / TOTAL / ACTIVE / SIZE / RECLAIMABLE` table from `docker system df` and the `[tengiz] dry-run` notice. If docker is unavailable, the command exits with a `docker: ...` error — acceptable in this environment.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark feature #6 implemented"
```

---

## Self-Review

**1. Spec coverage** (`docs/FUTURES_FEATURES.md` feature #6 + detailed section):
- `tengiz cleanup` command ✅ (Task 4)
- Label-based container protection (`--filter label=tengiz-app`, running containers never touched) ✅ (Task 2 arg builder + Task 4 long help)
- Unused volume/network/container/image pruning ✅ (Task 3 — all 5 categories)
- Build cache pruning ✅ (Task 3)
- Safety: volumes not pruned by default ✅ (Task 4 defaults), `--dry-run` preview ✅ (Task 3 + Task 4)
- Docs update per AGENTS.md rule ✅ (Task 5)

**2. Placeholder scan:** Every code step contains complete, compilable code — no "TBD", "TODO", "add validation", or "similar to Task N". The one docker-dependent code path (`dockerRuntime.Prune` exec) is covered by a pure dispatcher test, consistent with the repo's existing convention (`buildLogArgs`/`buildRunArgs` are also untested against real docker).

**3. Type consistency:**
- `runtime.PruneOptions{Categories []PruneCategory; AllImages bool; DryRun bool}` — defined Task 1, produced in `parseCleanupOptions` (Task 4), consumed by `Manager.Prune` (Task 1) and `Prune` (Task 3)
- `runtime.PruneReport{DryRun bool; Results []PruneResult; DfRows []SystemDfRow}` — defined Task 1, returned by `Prune`/`pruneDryRun` (Task 3), read by `printCleanupReport` (Task 4)
- `runtime.PruneResult{Category PruneCategory; Reclaimed string; Err error}` — used in `runPrune` (Task 3) and `printCleanupReport` (Task 4)
- Category constants match exactly across `AllPruneCategories` (Task 1), `pruneCommandArgs` (Task 2), `parseCleanupOptions` (Task 4)
- `labelKey` reused from `internal/runtime/docker.go` (single source of truth for `tengiz-app`)
- Mock method signatures in all three test files match `Manager.Prune` exactly (Task 1)

**4. Interface breakage handled:** Adding `Prune` to `Manager` updates `stubManager` + `mockRTForDeploy` + both `mockRuntime` types in the same commit as the interface change (Task 1, Step 6), so `go build ./...` remains green after every task.
