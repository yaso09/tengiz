# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (exited helper containers, dangling and old app images, unused volumes, unused networks, BuildKit build cache) with label-based protection for Tengiz-managed containers/images and a `--dry-run` mode.

**Architecture:** A new `internal/cleanup` package mirrors the existing `runtime` package pattern: a `Manager` interface with an exec-based `dockerRuntime` implementation (calls the `docker` CLI via `os/exec`) and a no-op `stubManager` for tests. The Docker implementation is built from small, pure, unit-testable helper functions (argument builders and output parsers) exactly like `buildLogArgs`/`buildRunArgs` in `internal/runtime/docker.go`. A `tengiz cleanup` Cobra command lives in its own `internal/cli/cleanup.go` file (same pattern as `preview.go`) and registers itself via its own `init()`, so `root.go` is untouched. Tengiz-managed containers are protected by skipping everything carrying the `tengiz-app` label; old app images are pruned per-app by retention window (default keep 5), always keeping `:latest`/`-latest` and preview (`pr-*`) tags.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (docker CLI). No new external dependencies.

## Global Constraints

- Exec-based Docker calls use `os/exec.CommandContext` — no Docker SDK (repo-wide rule)
- Tengiz-managed containers are identified by the `tengiz-app` label and must never be removed by `cleanup`
- Image tags built by the repo: `tengiz-apps/<app>:<env>-<deploymentID>` and `tengiz-apps/<app>:<env>-latest`; preview tags: `tengiz-apps/<app>:pr-<n>-<deploymentID>`
- Per-app image retention window default is `5` (same convention as `runtime.KeepLastNImages` usage in `deploy`)
- Default `tengiz cleanup` (no flags) prunes only `containers` + `images`; riskier categories require `--all` or an explicit flag
- `--dry-run` reports what would be removed without removing anything
- Every task ends with `go test ./internal/cleanup/...` (or the relevant package) passing and a commit; final task runs `go test ./... -count=1` and `go vet ./...`
- No modifications to `internal/runtime`, `internal/config`, or `internal/types` — new code lives in the new `cleanup` package
- New package `internal/cleanup` imports only stdlib + `github.com/yaso09/tengiz/internal/config` (CLI only); the cleanup package itself imports only stdlib
- Commit message style: conventional (`feat:`, `test:`, `docs:`)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/cleanup.go` | `Options`, `Report`, `Manager` interface, `NewDocker()`, `NewStub()` |
| `internal/cleanup/cleanup_test.go` | Tests for stub manager, interface satisfaction, `Report.Total()` |
| `internal/cleanup/docker.go` | `dockerRuntime`: docker CLI arg builders, output parsers, `pruneContainers/pruneImages/pruneVolumes/pruneNetworks/pruneBuildCache`, `Prune` orchestration |
| `internal/cleanup/docker_test.go` | Unit tests for every pure helper (builders + parsers) |
| `internal/cli/cleanup.go` | `cleanupCmd` + `addCleanupFlags` + `runCleanup` (testable, manager injected) |
| `internal/cli/cleanup_test.go` | CLI tests: registration, flags, default categories, `--dry-run`, output summary |
| `README.md` | New `### tengiz cleanup` section + command reference table row |
| `AGENTS.md` | Add `tengiz cleanup` to Commands + CLI list |
| `docs/FUTURES_FEATURES.md` | Mark P0 #6 Docker Housekeeping as implemented |

---

### Task 1: Cleanup package skeleton (types, interface, stub)

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: nothing (only stdlib)
- Produces: `cleanup.Options` (fields `All, Containers, Images, Volumes, Networks, BuildCache, DryRun bool`, `KeepLast int`, `Apps []string`), `cleanup.Report` (fields `Containers, Images, Volumes, Networks []string`, `BuildCache, DryRun bool`), `Report.Total() int`, `cleanup.Manager` interface with `Prune(ctx context.Context, opts Options) (Report, error)`, `cleanup.NewDocker() (Manager, error)`, `cleanup.NewStub() Manager`

- [ ] **Step 1: Write the failing test**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"testing"
)

func TestStubSatisfiesInterface(t *testing.T) {
	m := NewStub()
	var iface Manager = m
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	rep, err := m.Prune(context.Background(), Options{Containers: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if rep.DryRun {
		t.Fatal("expected DryRun=false for default options")
	}
}

func TestStubPruneDryRun(t *testing.T) {
	m := NewStub()
	rep, err := m.Prune(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !rep.DryRun {
		t.Fatal("expected DryRun=true")
	}
}

func TestReportTotal(t *testing.T) {
	rep := Report{
		Containers: []string{"a"},
		Images:     []string{"b", "c"},
		Volumes:    nil,
		Networks:   []string{"d"},
	}
	if rep.Total() != 4 {
		t.Fatalf("Total() = %d, want 4", rep.Total())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: FAIL — build error "no required module provides package github.com/yaso09/tengiz/internal/cleanup" (package does not exist yet)

- [ ] **Step 3: Write minimal implementation**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
)

// Options controls which categories are pruned and how.
type Options struct {
	All        bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
	KeepLast   int
	Apps       []string
}

// Report lists what was removed (or, in dry-run mode, what would be removed).
type Report struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
	BuildCache bool
	DryRun     bool
}

// Total returns the number of individual items in the report.
func (r Report) Total() int {
	return len(r.Containers) + len(r.Images) + len(r.Volumes) + len(r.Networks)
}

// Manager prunes unused Docker resources.
type Manager interface {
	Prune(ctx context.Context, opts Options) (Report, error)
}

// NewDocker returns an exec-based Manager backed by the docker CLI.
func NewDocker() (Manager, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerRuntime{}, nil
}

// NewStub returns a Manager that does nothing, for tests.
func NewStub() Manager {
	return &stubManager{}
}

type stubManager struct{}

func (m *stubManager) Prune(ctx context.Context, opts Options) (Report, error) {
	return Report{DryRun: opts.DryRun}, nil
}
```

Note: `NewDocker` references `dockerRuntime`, which does not exist until Task 2. `NewStub` and the tests compile now because they never call `NewDocker`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: PASS (3 tests) — note `dockerRuntime` is unreferenced at compile time because only `NewStub` is exercised; the package still builds since `dockerRuntime` is only used inside `NewDocker`'s body.

If compilation fails with "undefined: dockerRuntime", add a temporary minimal stub in `cleanup.go`:

```go
type dockerRuntime struct{}
```

and delete it in Task 2 Step 3 when the real type is created.

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup package skeleton with Manager interface"
```

---

### Task 2: Docker core + container pruning

**Files:**
- Create: `internal/cleanup/docker.go`
- Test: `internal/cleanup/docker_test.go`

**Interfaces:**
- Consumes: `cleanup.Options`, `cleanup.Report` from Task 1
- Produces: unexported `dockerRuntime` type; helpers `runDocker(ctx, args...) (string, error)`, `splitLines(out string) []string`, `(r *dockerRuntime) removeAll(ctx, buildArgs func([]string) []string, items []string) []string`, `buildExitedContainerListArgs() []string`, `buildContainerRemoveArgs(names []string) []string`, `(r *dockerRuntime) pruneContainers(ctx, dryRun bool) ([]string, error)`, and `Prune` handling only the containers branch

- [ ] **Step 1: Write the failing test**

Create `internal/cleanup/docker_test.go`:

```go
package cleanup

import (
	"context"
	"reflect"
	"testing"
)

func TestSplitLines(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a\nb\nc", []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		got := splitLines(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitLines(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestBuildExitedContainerListArgs(t *testing.T) {
	got := buildExitedContainerListArgs()
	want := []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.Names}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildExitedContainerListArgs() = %v, want %v", got, want)
	}
}

func TestBuildContainerRemoveArgs(t *testing.T) {
	got := buildContainerRemoveArgs([]string{"c1", "c2"})
	want := []string{"rm", "c1", "c2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildContainerRemoveArgs() = %v, want %v", got, want)
	}
}

func TestStubPruneDoesNotCallDocker(t *testing.T) {
	m := NewStub()
	rep, err := m.Prune(context.Background(), Options{All: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if rep.Total() != 0 {
		t.Fatalf("expected empty report, got %+v", rep)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: FAIL — compile error "undefined: splitLines" / "undefined: buildExitedContainerListArgs" (functions not implemented yet)

- [ ] **Step 3: Write minimal implementation**

Create `internal/cleanup/docker.go` (full contents — this is the complete base file that Tasks 3-5 extend):

```go
package cleanup

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

const labelKey = "tengiz-app"

type dockerRuntime struct{}

// runDocker runs the docker CLI and returns trimmed combined output.
func runDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func splitLines(out string) []string {
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// removeAll removes items one at a time, logging (not failing on) individual errors.
func (r *dockerRuntime) removeAll(ctx context.Context, buildArgs func(item []string) []string, items []string) []string {
	var removed []string
	for _, item := range items {
		if _, err := runDocker(ctx, buildArgs([]string{item})...); err != nil {
			log.Printf("[cleanup] failed to remove %s: %v", item, err)
			continue
		}
		removed = append(removed, item)
	}
	return removed
}

// ---------- containers ----------

func buildExitedContainerListArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", fmt.Sprintf("label!=%s", labelKey),
		"--format", "{{.Names}}",
	}
}

func buildContainerRemoveArgs(names []string) []string {
	return append([]string{"rm"}, names...)
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) ([]string, error) {
	out, err := runDocker(ctx, buildExitedContainerListArgs()...)
	if err != nil {
		return nil, err
	}
	candidates := splitLines(out)
	if dryRun {
		return candidates, nil
	}
	return r.removeAll(ctx, buildContainerRemoveArgs, candidates), nil
}

// ---------- orchestration ----------

func (r *dockerRuntime) Prune(ctx context.Context, opts Options) (Report, error) {
	rep := Report{DryRun: opts.DryRun}

	if opts.All || opts.Containers {
		names, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("containers: %w", err)
		}
		rep.Containers = names
	}

	return rep, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: PASS (Task 1 + Task 2 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/docker.go internal/cleanup/docker_test.go
git commit -m "feat: prune exited non-tengiz containers in cleanup"
```

---

### Task 3: Image pruning (dangling + per-app retention)

**Files:**
- Modify: `internal/cleanup/docker.go` — add image helpers, `pruneImages`, and the images branch in `Prune`; add `sort` to imports
- Test: `internal/cleanup/docker_test.go` — add image tests

**Interfaces:**
- Consumes: `runDocker`, `splitLines`, `removeAll`, `Options.Apps`, `Options.KeepLast`, `Options.DryRun` from Tasks 1-2
- Produces: const `appImageRepo = "tengiz-apps"`, `buildDanglingImageListArgs() []string`, `buildAppImageListArgs(app string) []string`, `imageInfo{Tag, CreatedAt string}`, `isProtectedImageTag(tag string) bool`, `parseImageList(out string) []imageInfo` (sorted oldest-first, protected tags skipped), `selectImageTagsToRemove(infos []imageInfo, keep int) []string`, `buildImageRemoveArgs(tags []string) []string`, `(r *dockerRuntime) pruneImages(ctx, apps []string, keep int, dryRun bool) ([]string, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/cleanup/docker_test.go`:

```go
func TestIsProtectedImageTag(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{"tengiz-apps/demo:production-1720000000", false},
		{"tengiz-apps/demo:latest", true},
		{"tengiz-apps/demo:production-latest", true},
		{"tengiz-apps/demo:pr-42-1720000000", true},
		{"tengiz-apps/demo:pr-42", true},
	}
	for _, tt := range tests {
		if got := isProtectedImageTag(tt.tag); got != tt.want {
			t.Errorf("isProtectedImageTag(%q) = %v, want %v", tt.tag, got, tt.want)
		}
	}
}

func TestParseImageList(t *testing.T) {
	out := "tengiz-apps/demo:production-3|2026-08-01 10:00:00 +0000 UTC\n" +
		"tengiz-apps/demo:production-latest|2026-08-02 10:00:00 +0000 UTC\n" +
		"tengiz-apps/demo:production-1|2026-07-01 10:00:00 +0000 UTC\n" +
		"tengiz-apps/demo:pr-7-1720000000|2026-08-03 10:00:00 +0000 UTC"
	got := parseImageList(out)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries (latest + preview skipped), got %d: %+v", len(got), got)
	}
	if got[0].Tag != "tengiz-apps/demo:production-1" {
		t.Errorf("first (oldest) = %q, want production-1", got[0].Tag)
	}
	if got[1].Tag != "tengiz-apps/demo:production-3" {
		t.Errorf("second = %q, want production-3", got[1].Tag)
	}
}

func TestSelectImageTagsToRemove(t *testing.T) {
	infos := []imageInfo{
		{Tag: "tengiz-apps/demo:production-1", CreatedAt: "2026-07-01"},
		{Tag: "tengiz-apps/demo:production-2", CreatedAt: "2026-07-02"},
		{Tag: "tengiz-apps/demo:production-3", CreatedAt: "2026-07-03"},
	}
	got := selectImageTagsToRemove(infos, 1)
	want := []string{"tengiz-apps/demo:production-1", "tengiz-apps/demo:production-2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectImageTagsToRemove() = %v, want %v", got, want)
	}
	if got := selectImageTagsToRemove(infos, 5); got != nil {
		t.Errorf("keep=5 should remove nothing, got %v", got)
	}
	if got := selectImageTagsToRemove(infos, -1); len(got) != 3 {
		t.Errorf("keep=-1 clamps to 0, want all 3 removed, got %v", got)
	}
}

func TestBuildImageArgs(t *testing.T) {
	if got, want := buildDanglingImageListArgs(), []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildDanglingImageListArgs() = %v, want %v", got, want)
	}
	if got, want := buildAppImageListArgs("demo"), []string{"images", "--filter", "reference=tengiz-apps/demo:*", "--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildAppImageListArgs() = %v, want %v", got, want)
	}
	if got, want := buildImageRemoveArgs([]string{"a", "b"}), []string{"rmi", "-f", "a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildImageRemoveArgs() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: FAIL — compile error "undefined: isProtectedImageTag" (etc.)

- [ ] **Step 3: Write minimal implementation**

Modify `internal/cleanup/docker.go`:

1. Change the import block from:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)
```

to:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)
```

2. Add the image constants and helpers after the containers section (i.e., right before the `// ---------- orchestration ----------` comment):

```go
// ---------- images ----------

const appImageRepo = "tengiz-apps"

func buildDanglingImageListArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func buildAppImageListArgs(app string) []string {
	return []string{
		"images",
		"--filter", fmt.Sprintf("reference=%s/%s:*", appImageRepo, app),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	}
}

type imageInfo struct {
	Tag       string
	CreatedAt string
}

// isProtectedImageTag reports whether a tag must never be pruned.
func isProtectedImageTag(tag string) bool {
	if strings.HasSuffix(tag, ":latest") || strings.HasSuffix(tag, "-latest") {
		return true
	}
	// preview deployments: tengiz-apps/<app>:pr-<n>-<deploymentID>
	if idx := strings.LastIndex(tag, ":"); idx >= 0 && strings.HasPrefix(tag[idx+1:], "pr-") {
		return true
	}
	return false
}

// parseImageList parses `repo:tag|createdAt` lines, skipping protected tags,
// and returns them sorted oldest-first.
func parseImageList(out string) []imageInfo {
	var infos []imageInfo
	for _, line := range splitLines(out) {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		tag, created := parts[0], parts[1]
		if isProtectedImageTag(tag) {
			continue
		}
		infos = append(infos, imageInfo{Tag: tag, CreatedAt: created})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].CreatedAt < infos[j].CreatedAt
	})
	return infos
}

// selectImageTagsToRemove returns the oldest tags beyond the keep window.
func selectImageTagsToRemove(infos []imageInfo, keep int) []string {
	if keep < 0 {
		keep = 0
	}
	if len(infos) <= keep {
		return nil
	}
	var tags []string
	for _, info := range infos[:len(infos)-keep] {
		tags = append(tags, info.Tag)
	}
	return tags
}

func buildImageRemoveArgs(tags []string) []string {
	return append([]string{"rmi", "-f"}, tags...)
}

func (r *dockerRuntime) pruneImages(ctx context.Context, apps []string, keep int, dryRun bool) ([]string, error) {
	var candidates []string

	danglingOut, err := runDocker(ctx, buildDanglingImageListArgs()...)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, splitLines(danglingOut)...)

	for _, app := range apps {
		out, err := runDocker(ctx, buildAppImageListArgs(app)...)
		if err != nil {
			log.Printf("[cleanup] failed to list images for %s: %v", app, err)
			continue
		}
		candidates = append(candidates, selectImageTagsToRemove(parseImageList(out), keep)...)
	}

	if dryRun {
		return candidates, nil
	}
	return r.removeAll(ctx, buildImageRemoveArgs, candidates), nil
}
```

3. Update `Prune` — replace the whole method so it reads:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts Options) (Report, error) {
	rep := Report{DryRun: opts.DryRun}

	if opts.All || opts.Containers {
		names, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("containers: %w", err)
		}
		rep.Containers = names
	}

	if opts.All || opts.Images {
		keep := opts.KeepLast
		if keep <= 0 {
			keep = 5
		}
		tags, err := r.pruneImages(ctx, opts.Apps, keep, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("images: %w", err)
		}
		rep.Images = tags
	}

	return rep, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/docker.go internal/cleanup/docker_test.go
git commit -m "feat: prune dangling and old app images in cleanup"
```

---

### Task 4: Volume pruning

**Files:**
- Modify: `internal/cleanup/docker.go` — add volume helpers, `pruneVolumes`, and the volumes branch in `Prune`
- Test: `internal/cleanup/docker_test.go` — add volume tests

**Interfaces:**
- Consumes: `runDocker`, `splitLines`, `removeAll`, `Options.Volumes`, `Options.DryRun` from Tasks 1-2
- Produces: `buildDanglingVolumeListArgs() []string`, `buildVolumeRemoveArgs(names []string) []string`, `(r *dockerRuntime) pruneVolumes(ctx, dryRun bool) ([]string, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/cleanup/docker_test.go`:

```go
func TestBuildVolumeArgs(t *testing.T) {
	if got, want := buildDanglingVolumeListArgs(), []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildDanglingVolumeListArgs() = %v, want %v", got, want)
	}
	if got, want := buildVolumeRemoveArgs([]string{"v1"}), []string{"volume", "rm", "v1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildVolumeRemoveArgs() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: FAIL — compile error "undefined: buildDanglingVolumeListArgs"

- [ ] **Step 3: Write minimal implementation**

Modify `internal/cleanup/docker.go`:

1. Add the volume helpers after the images section (before `// ---------- orchestration ----------`):

```go
// ---------- volumes ----------

func buildDanglingVolumeListArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func buildVolumeRemoveArgs(names []string) []string {
	return append([]string{"volume", "rm"}, names...)
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) ([]string, error) {
	out, err := runDocker(ctx, buildDanglingVolumeListArgs()...)
	if err != nil {
		return nil, err
	}
	candidates := splitLines(out)
	if dryRun {
		return candidates, nil
	}
	return r.removeAll(ctx, buildVolumeRemoveArgs, candidates), nil
}
```

2. Update `Prune` — add the volumes branch between the images branch and the final `return rep, nil`:

```go
	if opts.All || opts.Volumes {
		vols, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("volumes: %w", err)
		}
		rep.Volumes = vols
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/docker.go internal/cleanup/docker_test.go
git commit -m "feat: prune unused anonymous volumes in cleanup"
```

---

### Task 5: Network + build cache pruning, complete orchestration

**Files:**
- Modify: `internal/cleanup/docker.go` — add network + build cache helpers, `pruneNetworks`, `pruneBuildCache`, and the final `Prune` branches
- Test: `internal/cleanup/docker_test.go` — add network + cache tests

**Interfaces:**
- Consumes: `runDocker`, `splitLines`, `Options.Networks`, `Options.BuildCache`, `Options.DryRun` from Tasks 1-2
- Produces: `builtinNetworks map[string]bool`, `buildNetworkListArgs() []string`, `parseNetworks(out string, exclude map[string]bool) []string`, `parsePruneNetworksOutput(out string) []string`, `buildPruneNetworkArgs() []string`, `(r *dockerRuntime) pruneNetworks(ctx, dryRun bool) ([]string, error)`, `buildPruneCacheArgs() []string`, `(r *dockerRuntime) pruneBuildCache(ctx, dryRun bool) (bool, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/cleanup/docker_test.go`:

```go
func TestBuildNetworkArgs(t *testing.T) {
	if got, want := buildNetworkListArgs(), []string{"network", "ls", "--format", "{{.Name}}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildNetworkListArgs() = %v, want %v", got, want)
	}
	if got, want := buildPruneNetworkArgs(), []string{"network", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneNetworkArgs() = %v, want %v", got, want)
	}
	if got, want := buildPruneCacheArgs(), []string{"builder", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneCacheArgs() = %v, want %v", got, want)
	}
}

func TestParseNetworks(t *testing.T) {
	out := "bridge\nhost\nnone\nmyapp-net"
	got := parseNetworks(out, builtinNetworks)
	want := []string{"myapp-net"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseNetworks() = %v, want %v", got, want)
	}
}

func TestParsePruneNetworksOutput(t *testing.T) {
	out := "WARNING! This will remove all custom networks not used by at least one container.\n" +
		"Deleted Networks:\n" +
		"\"myapp-net\"\n" +
		"\"old-net\"\n"
	got := parsePruneNetworksOutput(out)
	want := []string{"myapp-net", "old-net"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePruneNetworksOutput() = %v, want %v", got, want)
	}
}

func TestParsePruneNetworksOutputNoHeader(t *testing.T) {
	if got := parsePruneNetworksOutput("nothing here"); got != nil {
		t.Errorf("expected nil for output without header, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: FAIL — compile error "undefined: buildNetworkListArgs" (etc.)

- [ ] **Step 3: Write minimal implementation**

Modify `internal/cleanup/docker.go`:

1. Add the `builtinNetworks` var after the `labelKey` const:

```go
// builtinNetworks must never be pruned.
var builtinNetworks = map[string]bool{
	"bridge": true,
	"host":   true,
	"none":   true,
}
```

2. Add the network and build cache helpers after the volumes section (before `// ---------- orchestration ----------`):

```go
// ---------- networks ----------

func buildNetworkListArgs() []string {
	return []string{"network", "ls", "--format", "{{.Name}}"}
}

func parseNetworks(out string, exclude map[string]bool) []string {
	var names []string
	for _, name := range splitLines(out) {
		if !exclude[name] {
			names = append(names, name)
		}
	}
	return names
}

func parsePruneNetworksOutput(out string) []string {
	var names []string
	collect := false
	for _, line := range splitLines(out) {
		if strings.HasPrefix(line, "Deleted Networks:") {
			collect = true
			continue
		}
		if collect && line != "" {
			names = append(names, strings.Trim(line, `"'`))
		}
	}
	return names
}

func buildPruneNetworkArgs() []string {
	return []string{"network", "prune", "-f"}
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) ([]string, error) {
	if dryRun {
		out, err := runDocker(ctx, buildNetworkListArgs()...)
		if err != nil {
			return nil, err
		}
		return parseNetworks(out, builtinNetworks), nil
	}
	out, err := runDocker(ctx, buildPruneNetworkArgs()...)
	if err != nil {
		return nil, err
	}
	return parsePruneNetworksOutput(out), nil
}

// ---------- build cache ----------

func buildPruneCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) (bool, error) {
	if dryRun {
		return true, nil
	}
	if _, err := runDocker(ctx, buildPruneCacheArgs()...); err != nil {
		return false, err
	}
	return true, nil
}
```

3. Update `Prune` — add the networks and build cache branches so the final method is:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts Options) (Report, error) {
	rep := Report{DryRun: opts.DryRun}

	if opts.All || opts.Containers {
		names, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("containers: %w", err)
		}
		rep.Containers = names
	}

	if opts.All || opts.Images {
		keep := opts.KeepLast
		if keep <= 0 {
			keep = 5
		}
		tags, err := r.pruneImages(ctx, opts.Apps, keep, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("images: %w", err)
		}
		rep.Images = tags
	}

	if opts.All || opts.Volumes {
		vols, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("volumes: %w", err)
		}
		rep.Volumes = vols
	}

	if opts.All || opts.Networks {
		nets, err := r.pruneNetworks(ctx, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("networks: %w", err)
		}
		rep.Networks = nets
	}

	if opts.All || opts.BuildCache {
		ok, err := r.pruneBuildCache(ctx, opts.DryRun)
		if err != nil {
			return rep, fmt.Errorf("build cache: %w", err)
		}
		rep.BuildCache = ok
	}

	return rep, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/docker.go internal/cleanup/docker_test.go
git commit -m "feat: prune unused networks and build cache in cleanup"
```

---

### Task 6: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Note: `root.go` is NOT modified — `cleanup.go` registers itself via its own `init()` (same pattern as `internal/cli/preview.go` which calls `rootCmd.AddCommand(previewCmd)` from its own init)

**Interfaces:**
- Consumes: `cleanup.NewDocker()`, `cleanup.Manager`, `cleanup.Options`, `cleanup.Report`, `Report.Total()`; package vars `dataDir` and helper `getEnv(cmd)` from `internal/cli/root.go`; `config.NewStoreWithEnv(dataDir, env)` + `Store.ListApps()`
- Produces: `cleanupCmd *cobra.Command` (registered on `rootCmd`), `addCleanupFlags(cmd *cobra.Command)`, `runCleanup(cmd *cobra.Command, mgr cleanup.Manager) error`, `printRemoved(kind string, items []string)`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go` (note: `captureOutput` is already defined in `internal/cli/root_test.go` in the same package and is reused here):

```go
package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

type mockCleanupManager struct {
	opts cleanup.Options
	rep  cleanup.Report
}

func (m *mockCleanupManager) Prune(ctx context.Context, opts cleanup.Options) (cleanup.Report, error) {
	m.opts = opts
	return m.rep, nil
}

func newTestCleanupCmd(mgr cleanup.Manager) *cobra.Command {
	c := &cobra.Command{Use: "cleanup"}
	addCleanupFlags(c)
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return runCleanup(cmd, mgr)
	}
	return c
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
	for _, flag := range []string{"all", "containers", "images", "volumes", "networks", "builder-cache", "dry-run", "keep-last"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func TestRunCleanupDefaultsToContainersAndImages(t *testing.T) {
	dataDir = t.TempDir()
	m := &mockCleanupManager{rep: cleanup.Report{}}
	c := newTestCleanupCmd(m)
	out := captureOutput(func() {
		c.SetArgs(nil)
		if err := c.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !m.opts.Containers {
		t.Error("expected Containers=true by default")
	}
	if !m.opts.Images {
		t.Error("expected Images=true by default")
	}
	if m.opts.Volumes || m.opts.Networks || m.opts.BuildCache {
		t.Error("expected volumes/networks/cache to be false by default")
	}
	if !strings.Contains(out, "nothing to clean") {
		t.Errorf("expected 'nothing to clean' in output, got: %q", out)
	}
}

func TestRunCleanupCategoryFlags(t *testing.T) {
	m := &mockCleanupManager{rep: cleanup.Report{}}
	c := newTestCleanupCmd(m)
	c.SetArgs([]string{"--volumes", "--networks"})
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if m.opts.Containers || m.opts.Images {
		t.Error("expected containers/images to stay false when explicit flags are given")
	}
	if !m.opts.Volumes || !m.opts.Networks {
		t.Error("expected volumes/networks to be true")
	}
}

func TestRunCleanupAllAndDryRun(t *testing.T) {
	m := &mockCleanupManager{rep: cleanup.Report{DryRun: true}}
	c := newTestCleanupCmd(m)
	c.SetArgs([]string{"--all", "--dry-run"})
	if err := c.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !m.opts.All {
		t.Error("expected All=true")
	}
	if !m.opts.DryRun {
		t.Error("expected DryRun=true")
	}
}

func TestRunCleanupPrintsSummary(t *testing.T) {
	m := &mockCleanupManager{
		rep: cleanup.Report{
			Containers: []string{"old-helper"},
			Images:     []string{"tengiz-apps/demo:production-123"},
		},
	}
	c := newTestCleanupCmd(m)
	out := captureOutput(func() {
		c.SetArgs(nil)
		if err := c.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(out, "removed 2 items") {
		t.Errorf("summary line missing, got: %q", out)
	}
	if !strings.Contains(out, "containers: old-helper") {
		t.Errorf("container line missing, got: %q", out)
	}
	if !strings.Contains(out, "images: tengiz-apps/demo:production-123") {
		t.Errorf("image line missing, got: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v -count=1 -run Cleanup`
Expected: FAIL — compile error "undefined: cleanupCmd" (etc.)

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/config"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Clean up unused Docker resources to free disk space.

Protected: Tengiz never removes containers or images it manages
(label tengiz-app). Old app images beyond the retention window
(--keep-last, default 5) are removed.

Default mode prunes exited helper containers and stale/dangling images.
Use --all, or explicit category flags, to include riskier categories.

Categories:
  --containers     remove exited containers not managed by Tengiz
  --images         remove dangling images + old app images (keeps last N)
  --volumes        remove unused anonymous volumes
  --networks       remove unused custom networks
  --builder-cache  prune BuildKit build cache

Examples:
  tengiz cleanup
  tengiz cleanup --all
  tengiz cleanup --volumes --networks --dry-run
  tengiz cleanup --images --keep-last 10`,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := cleanup.NewDocker()
		if err != nil {
			return err
		}
		return runCleanup(cmd, mgr)
	},
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
	addCleanupFlags(cleanupCmd)
}

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("all", false, "clean all categories (containers, images, volumes, networks, build cache)")
	cmd.Flags().Bool("containers", false, "remove exited containers not managed by Tengiz")
	cmd.Flags().Bool("images", false, "remove dangling images and old app images")
	cmd.Flags().Bool("volumes", false, "remove unused anonymous volumes")
	cmd.Flags().Bool("networks", false, "remove unused custom networks")
	cmd.Flags().Bool("builder-cache", false, "prune BuildKit build cache")
	cmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cmd.Flags().Int("keep-last", 5, "number of app image versions to keep per app")
}

func runCleanup(cmd *cobra.Command, mgr cleanup.Manager) error {
	env := getEnv(cmd)

	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	cache, _ := cmd.Flags().GetBool("builder-cache")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	keepLast, _ := cmd.Flags().GetInt("keep-last")

	opts := cleanup.Options{
		All:        all,
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: cache,
		DryRun:     dryRun,
		KeepLast:   keepLast,
	}

	if !all && !containers && !images && !volumes && !networks && !cache {
		opts.Containers = true
		opts.Images = true
	}

	if opts.Images {
		store := config.NewStoreWithEnv(dataDir, env)
		apps, listErr := store.ListApps()
		if listErr == nil {
			names := make([]string, 0, len(apps))
			for _, a := range apps {
				names = append(names, a.Name)
			}
			sort.Strings(names)
			opts.Apps = names
		}
	}

	rep, err := mgr.Prune(cmd.Context(), opts)
	if err != nil {
		return err
	}

	total := rep.Total()
	if rep.DryRun {
		fmt.Printf("[tengiz] dry-run: would remove %d items\n", total)
	} else {
		fmt.Printf("[tengiz] removed %d items\n", total)
	}
	printRemoved("containers", rep.Containers)
	printRemoved("images", rep.Images)
	printRemoved("volumes", rep.Volumes)
	printRemoved("networks", rep.Networks)
	if rep.BuildCache {
		action := "would clean"
		if !rep.DryRun {
			action = "cleaned"
		}
		fmt.Printf("[tengiz] build cache: %s\n", action)
	}
	if total == 0 && !rep.BuildCache {
		fmt.Println("[tengiz] nothing to clean")
	}
	return nil
}

func printRemoved(kind string, items []string) {
	for _, it := range items {
		fmt.Printf("  %s: %s\n", kind, it)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -v -count=1 -run Cleanup`
Expected: PASS (6 tests). Then run the full suite to confirm nothing regressed:
Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 5: Build and smoke-check help output**

```bash
go build -o tengiz .
./tengiz cleanup --help
```

Expected: `tengiz cleanup` usage with all 8 flags listed and the category descriptions. (Do not run `./tengiz cleanup` for real — it would prune Docker resources on this host.)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 7: Documentation + full verification

**Files:**
- Modify: `README.md` — add `### tengiz cleanup` section after `### tengiz run ...` section (ends at README.md:204) and add a row to the command reference table near README.md:574
- Modify: `AGENTS.md` — add `tengiz cleanup [...flags]` to the Commands code block
- Modify: `docs/FUTURES_FEATURES.md` — mark P0 #6 Docker Housekeeping implemented (table row at line 19 and Implemented Features table near line 237)

**Interfaces:**
- Consumes: nothing code-wise; documents the CLI added in Task 6

- [ ] **Step 1: Update README.md command section**

Add the following section after the `### tengiz run ...` section (after line 204, the closing ``` of the run examples):

```markdown
### `tengiz cleanup [flags]`

Remove unused Docker resources to free disk space. Tengiz never removes containers or images it manages (anything labeled `tengiz-app`). Old app images beyond the retention window (`--keep-last`, default 5) are removed, always keeping the `:latest` tag and preview (`pr-*`) tags.

Default mode prunes exited helper containers and stale/dangling images. Use `--all` or explicit category flags for riskier categories.

| Flag | Description |
|------|-------------|
| `--all` | Clean all categories (containers, images, volumes, networks, build cache) |
| `--containers` | Remove exited containers not managed by Tengiz |
| `--images` | Remove dangling images + old app images (keeps last N) |
| `--volumes` | Remove unused anonymous volumes |
| `--networks` | Remove unused custom networks |
| `--builder-cache` | Prune BuildKit build cache |
| `--dry-run` | Show what would be removed without removing anything |
| `--keep-last N` | Number of app image versions to keep per app (default: 5) |

Examples:

```bash
tengiz cleanup                 # safe default: containers + images
tengiz cleanup --all           # everything
tengiz cleanup --volumes --networks --dry-run   # preview risky categories
tengiz cleanup --images --keep-last 10
```
```

Also add a row to the command reference table near line 574 (after the `tengiz webhook` row):

```markdown
| `tengiz cleanup [--all] [--containers] [--images] [--volumes] [--networks] [--builder-cache] [--dry-run] [--keep-last N]` | Prune unused Docker resources with label-based protection |
```

- [ ] **Step 2: Update AGENTS.md**

In the Commands block (AGENTS.md, `## Commands` section, after the `tengiz notification show` line), add:

```bash
tengiz cleanup            → prune unused Docker resources (containers/images by default; --all for volumes/networks/build cache; --dry-run to preview)
```

Also add to the CLI block (AGENTS.md, `## CLI` section, after the `tengiz notification show` line):

```
tengiz cleanup [--all] [--containers] [--images] [--volumes] [--networks] [--builder-cache] [--dry-run] [--keep-last N] → prune unused Docker resources
```

- [ ] **Step 3: Update docs/FUTURES_FEATURES.md**

1. In the P0 table (line 19), change:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

2. In the `✅ Implemented Features (Not Pending)` table (after the line 253 row for **Webhook ile Otomatik Deploy**), add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-12) |
```

- [ ] **Step 4: Full verification**

Run:
```bash
go vet ./...
go test ./... -count=1
```

Expected: `go vet` clean; all tests PASS (including the existing `internal/runtime`, `internal/cli`, `internal/config` suites — none were modified, so none should break).

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage** (from `docs/FUTURES_FEATURES.md` #6 Docker Housekeeping):
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → Task 2 (containers), Task 3 (images), Task 4 (volumes), Task 5 (networks). ✓
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → Task 2 uses `label!=tengiz-app`; Task 3 `isProtectedImageTag` protects latest/preview. ✓
- "`tengiz cleanup` komutu" → Task 6. ✓
- `--dry-run` and `--keep-last` are supporting UX from the rationale; included as Global Constraints and implemented in Tasks 2-6. ✓
- Out of scope (intentionally, separate features): `DockerCleanupJob`-style periodic scheduling (#57 Background Monitoring Scheduler) and per-category CRUD pruning command surface (#56) — the category flags cover the pruning surface; scheduling is a separate P1 feature.

**2. Placeholder scan:** All steps contain complete code; no "TBD"/"add error handling" placeholders. Arg builders and parsers are concrete. ✓

**3. Type consistency:**
- `Options`/`Report` field names (`Containers/Images/Volumes/Networks/BuildCache/DryRun/KeepLast/Apps/All`) are identical in cleanup.go (Task 1), docker.go Prune branches (Tasks 2-5), and cli/cleanup.go (Task 6). ✓
- `Manager.Prune(ctx, Options) (Report, error)` matches `runCleanup`'s call and the stub/mock signatures. ✓
- `Report.Total()` used in both `runCleanup` and `TestReportTotal`. ✓
- Helper names referenced in tests (`splitLines`, `buildExitedContainerListArgs`, `buildContainerRemoveArgs`, `buildDanglingImageListArgs`, `buildAppImageListArgs`, `isProtectedImageTag`, `parseImageList`, `selectImageTagsToRemove`, `buildImageRemoveArgs`, `buildDanglingVolumeListArgs`, `buildVolumeRemoveArgs`, `buildNetworkListArgs`, `buildPruneNetworkArgs`, `buildPruneCacheArgs`, `parseNetworks`, `parsePruneNetworksOutput`) all match their definitions. ✓
- `addCleanupFlags` is shared between `init()` (Task 6) and `newTestCleanupCmd` (cleanup_test.go), so flag names can't drift. ✓
- `captureOutput`, `dataDir`, `getEnv`, `rootCmd` are existing package-level symbols in `internal/cli` reused by Task 6 without modification. ✓

## Execution Handoff

Two execution options:

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
