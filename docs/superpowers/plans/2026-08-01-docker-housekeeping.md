# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, networks, build cache, optionally volumes) to reclaim disk space, while always protecting Tengiz-managed containers via labels.

**Architecture:** The `runtime` package gains two new `Manager` methods — `SystemDf()` (disk usage summary from `docker system df --format`) and `Prune()` (runs per-category `docker <cat> prune` with label-based `label!=tengiz-app` filters to protect Tengiz containers). Argument building and output parsing are pure, unit-testable functions. A new `internal/cli/cmd_cleanup.go` Cobra command wires them together with granular `--containers/--images/--networks/--cache/--volumes` category flags, `--dry-run`, and a confirmation prompt for the data-destructive volume prune.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface + `dockerRuntime`, existing `exec.CommandContext` pattern, stdlib only (no new dependencies).

## Global Constraints

- Default cleanup (no flags) = containers + images + networks + build cache. Volumes NEVER pruned unless `--volumes`.
- Tengiz-managed containers are always protected: container and network prunes use `--filter "label!=tengiz-app"`.
- Default image prune removes ONLY dangling images (`docker image prune -f`); tagged `tengiz-apps/*` images are never touched by the default.
- `--all` (only meaningful with `--images`) uses `docker image prune -a -f --filter "until=168h"` — document that this may remove tengiz rollback images unused for 7+ days.
- Volumes are data-destructive: `--volumes` without `-f/--force` requires a `y/N` confirmation read from stdin; EOF/non-y aborts.
- `--dry-run` runs no destructive command; it only prints `docker system df` and the selected categories.
- Every category maps to exactly one `docker` command built by the pure `buildPruneArgs(cat, opts)` function.
- `runtime.Manager` interface gains 2 methods → all mock types implementing it must be updated in the same task as the interface change (build breaks otherwise).
- No new external dependencies.
- Existing tests must continue to pass without modification (only additive changes to mocks/tests).
- README.md and docs/FUTURES_FEATURES.md must be updated (repo rule: UI changes update docs).

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add types `PruneCategory`, `PruneOptions`, `CategoryResult`, `PruneResult`, `DfEntry`; add `SystemDf`/`Prune` to `Manager` interface; stub implementations |
| `internal/runtime/cleanup.go` | Add `execCommand` var, pure builders `buildSystemDfArgs`/`buildPruneArgs`/`DefaultPruneCategories`, parsers `parseDfOutput`/`parseReclaimedSpace`, and `dockerRuntime.SystemDf`/`dockerRuntime.Prune` |
| `internal/runtime/runtime_test.go` | Stub tests for the two new methods |
| `internal/runtime/cleanup_test.go` | Pure-function tests + `dockerRuntime` exec tests via the `TestHelperProcess` fake-exec pattern |
| `internal/cli/cmd_cleanup.go` | `cleanupCmd` cobra command, `runCleanup`, `selectPruneCategories`, `confirmDestructive`, `formatDf`, `categoryNames` |
| `internal/cli/root.go` | Register `cleanupCmd` + its 8 flags in `init()` |
| `internal/cli/root_test.go` | Command registration, flag parsing, category selection, confirm, formatDf tests; add 2 methods to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add 2 no-op methods to `mockRuntime` |
| `internal/idle/idle_test.go` | Add 2 no-op methods to `mockRuntime` |
| `README.md` | New `### tengiz cleanup` section + command table row |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented (2026-08-01) |

---

### Task 1: Add cleanup types + `Manager` interface methods + stub + mock updates

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add methods to `Manager` interface; add types; add stub impls
- Modify: `internal/runtime/runtime_test.go` — add stub tests
- Modify: `internal/cli/root_test.go:76-100` — add 2 methods to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:15-35` — add 2 methods to `mockRuntime`
- Modify: `internal/idle/idle_test.go:14-34` — add 2 methods to `mockRuntime`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `runtime.PruneCategory` (string): `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache`
  - `runtime.PruneOptions{Categories []PruneCategory; All bool}`
  - `runtime.CategoryResult{Category PruneCategory; ReclaimedSpace string}`
  - `runtime.PruneResult{Categories []CategoryResult}`
  - `runtime.DfEntry{Kind string; TotalCount, Active int; Size, Reclaimable string}`
  - `runtime.Manager.SystemDf(ctx context.Context) ([]DfEntry, error)`
  - `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/runtime_test.go`:

```go
func TestStubSystemDf(t *testing.T) {
	m := NewStub()
	entries, err := m.SystemDf(context.Background())
	if err != nil {
		t.Fatalf("SystemDf() error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("SystemDf() = %v, want empty", entries)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(res.Categories) != 0 {
		t.Errorf("Prune() = %+v, want empty result", res)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail to compile**

Run: `go test ./internal/runtime/ -run "TestStubSystemDf|TestStubPrune" -count=1`
Expected: FAIL — `m.SystemDf undefined` / `PruneOptions undefined`

- [ ] **Step 3: Add types, interface methods, and stub implementations**

Modify `internal/runtime/runtime.go`. Add the types right before the `Manager` interface (after the `RunOptions` struct, line 29):

```go
type PruneCategory string

const (
	PruneContainers PruneCategory = "containers"
	PruneImages     PruneCategory = "images"
	PruneVolumes    PruneCategory = "volumes"
	PruneNetworks   PruneCategory = "networks"
	PruneBuildCache PruneCategory = "build-cache"
)

type PruneOptions struct {
	Categories []PruneCategory
	All        bool
}

type CategoryResult struct {
	Category       PruneCategory
	ReclaimedSpace string
}

type PruneResult struct {
	Categories []CategoryResult
}

type DfEntry struct {
	Kind        string
	TotalCount  int
	Active      int
	Size        string
	Reclaimable string
}
```

Add two lines inside the `Manager` interface (after `KeepLastNImages`, line 36):

```go
	SystemDf(ctx context.Context) ([]DfEntry, error)
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

Add stub implementations at the end of the file (after `KeepLastNImages` stub, line 119):

```go
func (m *stubManager) SystemDf(ctx context.Context) ([]DfEntry, error) {
	return nil, nil
}

func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}
```

- [ ] **Step 4: Update the three mock types so the build compiles**

Modify `internal/cli/root_test.go`, adding to `mockRTForDeploy` (after its `KeepLastNImages` method, line 99):

```go
func (m *mockRTForDeploy) SystemDf(ctx context.Context) ([]runtime.DfEntry, error) { return nil, nil }
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

Modify `internal/proxy/proxy_test.go`, adding to `mockRuntime` (after `KeepLastNImages`, line 34):

```go
func (m *mockRuntime) SystemDf(ctx context.Context) ([]runtime.DfEntry, error) { return nil, nil }
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

Modify `internal/idle/idle_test.go`, adding to `mockRuntime` (after `KeepLastNImages`, line 33):

```go
func (m *mockRuntime) SystemDf(ctx context.Context) ([]runtime.DfEntry, error) { return nil, nil }
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) { return runtime.PruneResult{}, nil }
```

- [ ] **Step 5: Run the full test suite to verify it passes**

Run: `go test ./... -count=1`
Expected: PASS (all existing tests still pass, new stub tests pass)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add SystemDf and Prune to Manager interface"
```

---

### Task 2: Pure argument builders and output parsers

**Files:**
- Modify: `internal/runtime/cleanup.go` — add builders + parsers
- Modify: `internal/runtime/cleanup_test.go` — add tests

**Interfaces:**
- Consumes: `runtime.PruneCategory`, `runtime.PruneOptions`, `runtime.DfEntry` (from Task 1)
- Produces:
  - `func buildSystemDfArgs() []string`
  - `func buildPruneArgs(cat PruneCategory, opts PruneOptions) []string`
  - `func DefaultPruneCategories() []PruneCategory` (exported — used by CLI in Task 4)
  - `func parseDfOutput(output string) ([]DfEntry, error)`
  - `func parseReclaimedSpace(output string) string`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestBuildSystemDfArgs(t *testing.T) {
	args := buildSystemDfArgs()
	expected := []string{"system", "df", "--format", "{{.Type}}|{{.TotalCount}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}"}
	if len(args) != len(expected) {
		t.Fatalf("buildSystemDfArgs() = %v (len=%d), want len=%d", args, len(args), len(expected))
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		cat  PruneCategory
		opts PruneOptions
		want []string
	}{
		{
			name: "containers default",
			cat:  PruneContainers,
			want: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name: "images dangling only",
			cat:  PruneImages,
			want: []string{"image", "prune", "-f"},
		},
		{
			name: "images all",
			cat:  PruneImages,
			opts: PruneOptions{All: true},
			want: []string{"image", "prune", "-f", "-a", "--filter", "until=168h"},
		},
		{
			name: "volumes",
			cat:  PruneVolumes,
			want: []string{"volume", "prune", "-f"},
		},
		{
			name: "networks default",
			cat:  PruneNetworks,
			want: []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name: "build cache",
			cat:  PruneBuildCache,
			want: []string{"builder", "prune", "-f"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneArgs(tt.cat, tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("buildPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("arg[%d] = %q, want %q (full: %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

func TestDefaultPruneCategories(t *testing.T) {
	cats := DefaultPruneCategories()
	want := []PruneCategory{PruneContainers, PruneImages, PruneNetworks, PruneBuildCache}
	if len(cats) != len(want) {
		t.Fatalf("DefaultPruneCategories() = %v, want %v", cats, want)
	}
	for i := range want {
		if cats[i] != want[i] {
			t.Fatalf("cats[%d] = %q, want %q", i, cats[i], want[i])
		}
	}
}

func TestParseDfOutput(t *testing.T) {
	output := "Images|4|2|1.2GB|800MB (66.67%)\nContainers|3|1|2.5MB|0B (0%)\nLocal Volumes|2|1|10MB|5MB (50%)\nBuild Cache|1|0|50MB|40MB\n"
	entries, err := parseDfOutput(output)
	if err != nil {
		t.Fatalf("parseDfOutput() error = %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	img := entries[0]
	if img.Kind != "Images" || img.TotalCount != 4 || img.Active != 2 || img.Size != "1.2GB" || img.Reclaimable != "800MB (66.67%)" {
		t.Errorf("first entry = %+v, want Images/4/2/1.2GB/800MB (66.67%)", img)
	}
	if entries[2].Kind != "Local Volumes" {
		t.Errorf("kind[2] = %q, want Local Volumes", entries[2].Kind)
	}
}

func TestParseDfOutputEmpty(t *testing.T) {
	entries, err := parseDfOutput("")
	if err != nil {
		t.Fatalf("parseDfOutput(empty) error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestParseDfOutputBadLine(t *testing.T) {
	if _, err := parseDfOutput("Images|notanumber|2|1.2GB|800MB"); err == nil {
		t.Error("expected error for non-numeric count")
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"Deleted Containers:\nfoo\nbar\n\nTotal reclaimed space: 12.3MB\n", "12.3MB"},
		{"Total reclaimed space: 0B\n", "0B"},
		{"nothing deleted here", "0B"},
		{"", "0B"},
	}
	for _, tt := range tests {
		if got := parseReclaimedSpace(tt.output); got != tt.want {
			t.Errorf("parseReclaimedSpace(%q) = %q, want %q", tt.output, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestBuildSystemDfArgs|TestBuildPruneArgs|TestDefaultPruneCategories|TestParseDfOutput|TestParseReclaimedSpace" -count=1`
Expected: FAIL — `buildSystemDfArgs undefined` / `DefaultPruneCategories undefined`

- [ ] **Step 3: Implement the builders and parsers**

Modify `internal/runtime/cleanup.go`. Change the import block to add `strconv`:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)
```

Append to `internal/runtime/cleanup.go`:

```go
const pruneImageUntilAge = "168h"

func DefaultPruneCategories() []PruneCategory {
	return []PruneCategory{PruneContainers, PruneImages, PruneNetworks, PruneBuildCache}
}

func buildSystemDfArgs() []string {
	return []string{"system", "df", "--format", "{{.Type}}|{{.TotalCount}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}"}
}

func buildPruneArgs(cat PruneCategory, opts PruneOptions) []string {
	switch cat {
	case PruneContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case PruneImages:
		args := []string{"image", "prune", "-f"}
		if opts.All {
			args = append(args, "-a", "--filter", "until="+pruneImageUntilAge)
		}
		return args
	case PruneVolumes:
		return []string{"volume", "prune", "-f"}
	case PruneNetworks:
		return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	case PruneBuildCache:
		return []string{"builder", "prune", "-f"}
	default:
		return nil
	}
}

func parseDfOutput(output string) ([]DfEntry, error) {
	var entries []DfEntry
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 5 {
			return nil, fmt.Errorf("unexpected df line: %q", line)
		}
		total, errTotal := strconv.Atoi(strings.TrimSpace(parts[1]))
		active, errActive := strconv.Atoi(strings.TrimSpace(parts[2]))
		if errTotal != nil || errActive != nil {
			return nil, fmt.Errorf("invalid df counts in line: %q", line)
		}
		entries = append(entries, DfEntry{
			Kind:        strings.TrimSpace(parts[0]),
			TotalCount:  total,
			Active:      active,
			Size:        strings.TrimSpace(parts[3]),
			Reclaimable: strings.TrimSpace(parts[4]),
		})
	}
	return entries, nil
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Total reclaimed space:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			if val != "" {
				return val
			}
		}
	}
	return "0B"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestBuildSystemDfArgs|TestBuildPruneArgs|TestDefaultPruneCategories|TestParseDfOutput|TestParseReclaimedSpace" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add prune and df arg builders with parsers"
```

---

### Task 3: `dockerRuntime` exec implementations for `SystemDf` and `Prune`

**Files:**
- Modify: `internal/runtime/cleanup.go` — `execCommand` var + methods
- Modify: `internal/runtime/cleanup_test.go` — fake-exec tests

**Interfaces:**
- Consumes: `buildSystemDfArgs`, `buildPruneArgs`, `parseDfOutput`, `parseReclaimedSpace`, `DefaultPruneCategories` (Task 2), `execCommand` var
- Produces:
  - `var execCommand = exec.CommandContext` (package var, overridable in tests)
  - `func (r *dockerRuntime) SystemDf(ctx context.Context) ([]DfEntry, error)`
  - `func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)`
  - Test helper `TestHelperProcess(t *testing.T)` — fake docker CLI used by the child test process

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`. Update the import block to include `fmt`, `os`, and `os/exec`:

```go
import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
)
```

Append the fake-exec helper and the three tests:

```go
func fakeExecCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", name}
	cs = append(cs, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	if len(args) < 3 {
		os.Exit(1)
	}
	switch args[1] {
	case "system":
		if args[2] == "df" {
			fmt.Print("Images|4|2|1.2GB|800MB (66.67%)\nContainers|3|1|2.5MB|0B (0%)\nLocal Volumes|2|1|10MB|5MB (50%)\nBuild Cache|1|0|50MB|40MB\n")
			os.Exit(0)
		}
	case "container":
		fmt.Print("Total reclaimed space: 12.3MB\n")
		os.Exit(0)
	case "image":
		fmt.Print("Total reclaimed space: 800MB\n")
		os.Exit(0)
	case "network":
		fmt.Print("Total reclaimed space: 0B\n")
		os.Exit(0)
	case "builder":
		fmt.Print("Total reclaimed space: 40MB\n")
		os.Exit(0)
	case "volume":
		fmt.Print("Total reclaimed space: 5MB\n")
		os.Exit(0)
	}
	os.Exit(1)
}

func TestDockerSystemDf(t *testing.T) {
	old := execCommand
	execCommand = fakeExecCommand
	defer func() { execCommand = old }()

	r := &dockerRuntime{}
	entries, err := r.SystemDf(context.Background())
	if err != nil {
		t.Fatalf("SystemDf() error = %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	if entries[0].Kind != "Images" || entries[0].TotalCount != 4 || entries[0].Active != 2 || entries[0].Reclaimable != "800MB (66.67%)" {
		t.Errorf("first entry = %+v, want Images/4/2/.../800MB (66.67%)", entries[0])
	}
}

func TestDockerPruneDefaultCategories(t *testing.T) {
	old := execCommand
	execCommand = fakeExecCommand
	defer func() { execCommand = old }()

	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(res.Categories) != 4 {
		t.Fatalf("expected 4 category results, got %d", len(res.Categories))
	}
	if res.Categories[0].Category != PruneContainers || res.Categories[0].ReclaimedSpace != "12.3MB" {
		t.Errorf("first result = %+v, want containers/12.3MB", res.Categories[0])
	}
	if res.Categories[3].Category != PruneBuildCache || res.Categories[3].ReclaimedSpace != "40MB" {
		t.Errorf("last result = %+v, want build-cache/40MB", res.Categories[3])
	}
}

func TestDockerPruneAllImagesFlag(t *testing.T) {
	old := execCommand
	execCommand = fakeExecCommand
	defer func() { execCommand = old }()

	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), PruneOptions{Categories: []PruneCategory{PruneImages}, All: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(res.Categories) != 1 || res.Categories[0].ReclaimedSpace != "800MB" {
		t.Fatalf("Prune(images, all) = %+v, want one result with 800MB", res)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestDockerSystemDf|TestDockerPruneDefaultCategories|TestDockerPruneAllImagesFlag" -count=1`
Expected: FAIL — `execCommand undefined` (methods do not exist yet)

- [ ] **Step 3: Implement `execCommand` and the two methods**

Append to `internal/runtime/cleanup.go`:

```go
var execCommand = exec.CommandContext

func (r *dockerRuntime) SystemDf(ctx context.Context) ([]DfEntry, error) {
	cmd := execCommand(ctx, "docker", buildSystemDfArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseDfOutput(string(out))
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	cats := opts.Categories
	if len(cats) == 0 {
		cats = DefaultPruneCategories()
	}
	var result PruneResult
	for _, cat := range cats {
		cmd := execCommand(ctx, "docker", buildPruneArgs(cat, opts)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return result, fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out))
		}
		result.Categories = append(result.Categories, CategoryResult{
			Category:       cat,
			ReclaimedSpace: parseReclaimedSpace(string(out)),
		})
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS (all new + existing runtime tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement dockerRuntime SystemDf and Prune"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cmd_cleanup.go`
- Modify: `internal/cli/root.go:38-75` (init block) — register command + flags
- Modify: `internal/cli/root_test.go` — add tests

**Interfaces:**
- Consumes: `runtime.DefaultPruneCategories`, `runtime.PruneOptions`, `runtime.PruneCategory`, `runtime.PruneResult`, `runtime.DfEntry`, `runtime.NewDocker()` (from Tasks 1-3)
- Produces:
  - `var cleanupCmd *cobra.Command`
  - `func runCleanup(cmd *cobra.Command, args []string) error`
  - `func selectPruneCategories(containers, images, networks, cache, volumes bool) []runtime.PruneCategory`
  - `func confirmDestructive(r io.Reader) bool`
  - `func formatDf(entries []runtime.DfEntry) string`
  - `func categoryNames(cats []runtime.PruneCategory) []string`
  - `var cleanupInput io.Reader = os.Stdin` (injectable stdin for tests)

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"containers", "images", "networks", "cache", "volumes", "all", "force", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupFlagParsing(t *testing.T) {
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()

	var gotCats []runtime.PruneCategory
	var gotAll, gotForce, gotDryRun bool
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")
		volumes, _ := cmd.Flags().GetBool("volumes")
		gotAll, _ = cmd.Flags().GetBool("all")
		gotForce, _ = cmd.Flags().GetBool("force")
		gotDryRun, _ = cmd.Flags().GetBool("dry-run")
		gotCats = selectPruneCategories(containers, images, networks, cache, volumes)
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--images", "--cache", "--volumes", "--all", "--force", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(gotCats) != 3 {
		t.Fatalf("expected 3 categories, got %v", gotCats)
	}
	if !gotAll || !gotForce || !gotDryRun {
		t.Errorf("all=%v force=%v dry-run=%v, want all true", gotAll, gotForce, gotDryRun)
	}
}

func TestSelectPruneCategories(t *testing.T) {
	tests := []struct {
		name                         string
		containers, images, networks, cache, volumes bool
		expected                     int
	}{
		{"default (no flags)", false, false, false, false, false, 4},
		{"all categories explicit", true, true, true, true, true, 5},
		{"images only", false, true, false, false, false, 1},
		{"volumes only", false, false, false, false, true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectPruneCategories(tt.containers, tt.images, tt.networks, tt.cache, tt.volumes)
			if len(got) != tt.expected {
				t.Errorf("selectPruneCategories() = %v (len=%d), want len=%d", got, len(got), tt.expected)
			}
		})
	}
}

func TestConfirmDestructive(t *testing.T) {
	if !confirmDestructive(strings.NewReader("y\n")) {
		t.Error("expected yes for 'y'")
	}
	if !confirmDestructive(strings.NewReader("YES\n")) {
		t.Error("expected yes for 'YES'")
	}
	if confirmDestructive(strings.NewReader("n\n")) {
		t.Error("expected no for 'n'")
	}
	if confirmDestructive(strings.NewReader("")) {
		t.Error("expected no for EOF")
	}
}

func TestFormatDf(t *testing.T) {
	entries := []runtime.DfEntry{
		{Kind: "Images", TotalCount: 4, Active: 2, Size: "1.2GB", Reclaimable: "800MB (66.67%)"},
	}
	out := formatDf(entries)
	for _, want := range []string{"Images", "4 total", "2 active", "800MB (66.67%)"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatDf() missing %q in %q", want, out)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanup|TestSelectPruneCategories|TestConfirmDestructive|TestFormatDf" -count=1`
Expected: FAIL — `cleanupCmd undefined` / `selectPruneCategories undefined`

- [ ] **Step 3: Create `internal/cli/cmd_cleanup.go`**

```go
package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupInput io.Reader = os.Stdin

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (disk housekeeping)",
	Long: `Remove unused Docker containers, images, networks, and build cache to free disk space.

Tengiz-managed containers are always protected via the "tengiz-app" label.
Volumes are only pruned with --volumes (requires confirmation unless --force).

Examples:
  tengiz cleanup                     # prune containers, dangling images, networks, cache
  tengiz cleanup --volumes           # also prune unused volumes
  tengiz cleanup --images --cache    # only images and build cache
  tengiz cleanup --all --dry-run     # preview space from pruning all unused images`,
	Args: cobra.NoArgs,
	RunE: runCleanup,
}

func runCleanup(cmd *cobra.Command, args []string) error {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	cache, _ := cmd.Flags().GetBool("cache")
	volumes, _ := cmd.Flags().GetBool("volumes")
	all, _ := cmd.Flags().GetBool("all")
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	cats := selectPruneCategories(containers, images, networks, cache, volumes)

	rt, err := runtime.NewDocker()
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}

	before, err := rt.SystemDf(cmd.Context())
	if err != nil {
		return fmt.Errorf("system df: %w", err)
	}
	fmt.Println("[tengiz] before cleanup:")
	fmt.Print(formatDf(before))

	if dryRun {
		fmt.Printf("[tengiz] dry-run: would prune: %s\n", strings.Join(categoryNames(cats), ", "))
		return nil
	}

	if containsVolumes(cats) && !force {
		if !confirmDestructive(cleanupInput) {
			fmt.Println("[tengiz] cancelled.")
			return nil
		}
	}

	result, err := rt.Prune(cmd.Context(), runtime.PruneOptions{
		Categories: cats,
		All:        all,
	})
	if err != nil {
		return err
	}

	for _, c := range result.Categories {
		fmt.Printf("[tengiz] pruned %s (reclaimed %s)\n", c.Category, c.ReclaimedSpace)
	}

	after, err := rt.SystemDf(cmd.Context())
	if err != nil {
		return fmt.Errorf("system df: %w", err)
	}
	fmt.Println("[tengiz] after cleanup:")
	fmt.Print(formatDf(after))

	return nil
}

func selectPruneCategories(containers, images, networks, cache, volumes bool) []runtime.PruneCategory {
	var cats []runtime.PruneCategory
	if containers {
		cats = append(cats, runtime.PruneContainers)
	}
	if images {
		cats = append(cats, runtime.PruneImages)
	}
	if networks {
		cats = append(cats, runtime.PruneNetworks)
	}
	if cache {
		cats = append(cats, runtime.PruneBuildCache)
	}
	if volumes {
		cats = append(cats, runtime.PruneVolumes)
	}
	if len(cats) == 0 {
		return runtime.DefaultPruneCategories()
	}
	return cats
}

func containsVolumes(cats []runtime.PruneCategory) bool {
	for _, c := range cats {
		if c == runtime.PruneVolumes {
			return true
		}
	}
	return false
}

func confirmDestructive(r io.Reader) bool {
	fmt.Print("[tengiz] warning: this permanently deletes unused volumes. Continue? [y/N]: ")
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}

func formatDf(entries []runtime.DfEntry) string {
	var sb strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&sb, "  %-14s %d total, %d active, %s reclaimable\n", e.Kind, e.TotalCount, e.Active, e.Reclaimable)
	}
	return sb.String()
}

func categoryNames(cats []runtime.PruneCategory) []string {
	names := make([]string, len(cats))
	for i, c := range cats {
		names[i] = string(c)
	}
	return names
}
```

- [ ] **Step 4: Register the command and flags in `root.go`**

In `internal/cli/root.go` `init()`, immediately after the `notificationCmd` registration block (line 75), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("cache", false, "prune build cache")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused volumes (data-destructive; requires confirmation unless --force)")
	cleanupCmd.Flags().Bool("all", false, "with --images, also prune unused images older than 168h")
	cleanupCmd.Flags().BoolP("force", "f", false, "skip confirmation for volume pruning")
	cleanupCmd.Flags().Bool("dry-run", false, "show reclaimable space without deleting")
```

- [ ] **Step 5: Run the full test suite to verify it passes**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 6: Verify the CLI builds and shows help**

Run: `go build -o /tmp/opencode/tengiz . && /tmp/opencode/tengiz cleanup --help`
Expected: help output lists all 8 flags

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cmd_cleanup.go internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Documentation

**Files:**
- Modify: `README.md` — add `### tengiz cleanup` section + command table row
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented

**Interfaces:**
- Consumes: nothing
- Produces: nothing (docs only)

- [ ] **Step 1: Add the `tengiz cleanup` section to README.md**

Insert a new section after the `### tengiz ps` section (README.md line 150), following the existing flag-table format:

```markdown
### `tengiz cleanup`

Prune unused Docker resources (containers, images, networks, build cache) to free disk space. Tengiz-managed containers are always protected via the `tengiz-app` label.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped non-Tengiz containers |
| `--images` | Prune dangling images |
| `--networks` | Prune unused networks |
| `--cache` | Prune build cache |
| `--volumes` | Also prune unused volumes (data-destructive; requires confirmation unless `--force`) |
| `--all` | With `--images`, also prune unused images older than 168h |
| `-f`, `--force` | Skip confirmation for volume pruning |
| `--dry-run` | Show reclaimable space without deleting |

If no category flag is given, `containers`, `images`, `networks`, and `cache` are pruned. Volumes are never pruned unless `--volumes` is passed.
```

Add a row to the command reference table (README.md line 572 area, alongside the other `tengiz` commands):

```markdown
| `tengiz cleanup [flags]` | Prune unused Docker resources to free disk space |
```

- [ ] **Step 2: Mark the feature implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table (line 19), change the status cell of row 6 (Docker Housekeeping) from ⬜ to ✅:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "Implemented Features" section (after line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-01) |
```

- [ ] **Step 3: Verify nothing else references the feature as pending**

Run: `rg "Docker Housekeeping" docs/FUTURES_FEATURES.md`
Expected: only the two updated lines above

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage:**
- Feature #6 (Docker Housekeeping, `tengiz cleanup`): Task 4 creates the command; Task 2/3 implement label-based `docker system df` + per-category `docker <cat> prune`; `--volumes`/confirmation and `--dry-run` cover safe operation. ✅
- Feature #56 (Granular Docker Prune Operations, per-category: containers/networks/images/volumes/buildx cache): Task 2's `buildPruneArgs` maps exactly these 5 categories to `docker container/image/volume/network/builder prune`; Task 4 exposes them as flags. ✅
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur": `--filter "label!=tengiz-app"` on container and network prunes (Task 2). ✅

**2. Placeholder scan:** All steps contain complete code or exact commands; no TBD/TODO/"add error handling"/"similar to Task N" fragments. Task 3's exec methods are fully specified including the test helper. ✅

**3. Type consistency:**
- `PruneCategory` constants match between Task 1 (types), Task 2 (`buildPruneArgs` switch + `DefaultPruneCategories`), and Task 4 (`selectPruneCategories`/`containsVolumes`). ✅
- `DefaultPruneCategories()` is exported in Task 2 and consumed in Task 3 (`Prune`) and Task 4 (`selectPruneCategories`) — names match. ✅
- `PruneResult.Categories []CategoryResult` shape is produced in Task 3 and iterated in Task 4 (`result.Categories`, `.Category`, `.ReclaimedSpace`). ✅
- `DfEntry` fields `Kind/TotalCount/Active/Size/Reclaimable` match between Task 2 parser, Task 3 `SystemDf`, and Task 4 `formatDf`. ✅
- `execCommand` var is defined in Task 3 and used only there; no collision with existing identifiers (`exec` package import name is untouched). ✅
- Mock method signatures added in Task 1 match the interface added in the same task, so the build stays green throughout. ✅
