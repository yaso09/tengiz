# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, networks, and build cache to reclaim disk space, while always preserving Tengiz-managed resources (containers carrying the `tengiz-app` label and images under `tengiz-apps/*`), with a `--dry-run` mode.

**Architecture:** Decompose Docker housekeeping into a new `runtime.Pruner` interface (separate from the existing `runtime.Manager` so the three existing test mocks stay untouched) implemented on the existing exec-based `dockerRuntime`. Pure, table-driven Go helpers (`splitNonEmpty`, `parseContainerRow`, `isRunning`, `parseImageRow`, `selectImagesToRemove`) make every policy decision unit-testable without Docker. `dockerRuntime` only executes `docker` CLI commands via `os/exec`. A `tengiz cleanup` Cobra command wires the pruner to flags `--dry-run` / `--containers` / `--images` / `--volumes` / `--networks` / `--cache` (default: all categories).

**Tech Stack:** Go 1.26, Cobra CLI, existing `os/exec` Docker CLI access in `internal/runtime`. No new external dependencies.

## Global Constraints

- New command name: `tengiz cleanup` (from feature #6: "Label-based `docker system prune`. `tengiz cleanup`").
- Tengiz-managed containers are never removed: any container with the label key `tengiz-app` (any value) is protected — this includes scale-to-zero stopped containers needed for cold-start.
- Tengiz-managed images are never removed: all images matching repository prefix `tengiz-apps/*` are protected, plus the image referenced by every `tengiz-app`-labeled container.
- `runtime.Manager` interface stays unchanged — new behavior is exposed via a separate `runtime.Pruner` interface, so `mockRTForDeploy` (cli), `mockRuntime` (proxy), and `mockRuntime` (idle) do not need to change.
- No new Go module dependencies. All Docker interaction uses `os/exec` (`docker ...`).
- No unit test may require a Docker daemon — only pure helpers and the stub are tested.
- README.md, AGENTS.md, and `docs/FUTURES_FEATURES.md` must be updated (AGENTS.md rule: update docs on CLI/UI changes).
- Verification before commit: `go build -o tengiz .`, `go vet ./...`, `go test ./... -v -count=1` all pass.
- Periodic/scheduled auto-cleanup (Coolify's `DockerCleanupJob`) is explicitly out of scope — it belongs to P1 feature #57 "Background Monitoring Scheduler" and will reuse `runtime.Pruner`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` | New — `PruneOptions`, `PruneResult`, `Pruner` interface, `NewPruner()`, pure helpers, and the `dockerRuntime` prune backends |
| `internal/runtime/runtime.go` | Modify — add `Prune` to `stubManager` so `NewStub()` satisfies `Pruner` |
| `internal/runtime/prune_test.go` | New — tests for pure helpers and the stub pruner |
| `internal/cli/cleanup.go` | New — `cleanupCmd`, `cleanupOptions(cmd)`, `runCleanup(...)`, `renderCleanupSummary(...)` |
| `internal/cli/root.go` | Modify — register `cleanupCmd` in `init()` |
| `internal/cli/cmd_cleanup_test.go` | New — tests for command registration, flags, option defaults, and summary rendering |
| `README.md` | Modify — add `### tengiz cleanup` doc section |
| `AGENTS.md` | Modify — add `tengiz cleanup` line to the CLI list |
| `docs/FUTURES_FEATURES.md` | Modify — mark feature #6 as implemented |

---

### Task 1: Prune types, interface, stub, and pure decision helpers

**Files:**
- Create: `internal/runtime/prune.go`
- Modify: `internal/runtime/runtime.go` — `stubManager` adds `Prune`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing new (existing `dockerRuntime`, `stubManager` from `runtime.go`)
- Produces:
  - `PruneOptions struct { DryRun, Containers, Images, Volumes, Networks, BuildCache bool }`
  - `PruneResult struct { DryRun bool; ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved []string; BuildCachePruned bool }`
  - `Pruner interface { Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) }`
  - `NewStub()` returns a `stubManager` that also satisfies `Pruner`
  - package-level helpers: `splitNonEmpty(out string) []string`, `parseContainerRow(line string) (containerRow, bool)`, `isRunning(status string) bool`, `parseImageRow(line string) (imageInfo, bool)`, `selectImagesToRemove(images []imageInfo, protected map[string]bool) []string`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing tests**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"context"
	"testing"
)

func TestSplitNonEmpty(t *testing.T) {
	got := splitNonEmpty("  a  \n b \n\n c ")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("splitNonEmpty() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitNonEmpty()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseContainerRow(t *testing.T) {
	row, ok := parseContainerRow("web01|Up 2 hours")
	if !ok {
		t.Fatal("parseContainerRow() returned ok=false")
	}
	if row.Name != "web01" {
		t.Errorf("Name = %q, want %q", row.Name, "web01")
	}
	if row.Status != "Up 2 hours" {
		t.Errorf("Status = %q, want %q", row.Status, "Up 2 hours")
	}
}

func TestParseContainerRowMalformed(t *testing.T) {
	if _, ok := parseContainerRow("no-pipe"); ok {
		t.Error("single-field line should parse as ok=false")
	}
	if _, ok := parseContainerRow("|"); ok {
		t.Error("empty-field line should parse as ok=false")
	}
}

func TestIsRunning(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"Up 2 hours", true},
		{"Up 3 seconds", true},
		{"Exited (0) 2 hours ago", false},
		{"Created", false},
	}
	for _, c := range cases {
		if got := isRunning(c.status); got != c.want {
			t.Errorf("isRunning(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}

func TestParseImageRow(t *testing.T) {
	img, ok := parseImageRow("sha256:abc123|tengiz-apps/foo:prod-123")
	if !ok {
		t.Fatal("parseImageRow() returned ok=false")
	}
	if img.ID != "sha256:abc123" {
		t.Errorf("ID = %q, want %q", img.ID, "sha256:abc123")
	}
	if img.RepoTag != "tengiz-apps/foo:prod-123" {
		t.Errorf("RepoTag = %q, want %q", img.RepoTag, "tengiz-apps/foo:prod-123")
	}
}

func TestParseImageRowSkipsDangling(t *testing.T) {
	if _, ok := parseImageRow("sha256:abc123|<none>:<none>"); ok {
		t.Error("dangling image row should parse as ok=false")
	}
	if _, ok := parseImageRow("sha256:abc123|"); ok {
		t.Error("empty repo:tag should parse as ok=false")
	}
}

func TestSelectImagesToRemove(t *testing.T) {
	images := []imageInfo{
		{ID: "sha256:aaa", RepoTag: "tengiz-apps/foo:prod-1"},
		{ID: "sha256:bbb", RepoTag: "redis:7-alpine"},
		{ID: "sha256:ccc", RepoTag: "ubuntu:latest"},
	}
	protected := map[string]bool{
		"tengiz-apps/foo:prod-1": true,
		"sha256:ccc":             true,
	}
	got := selectImagesToRemove(images, protected)
	if len(got) != 1 || got[0] != "redis:7-alpine" {
		t.Errorf("selectImagesToRemove() = %v, want [redis:7-alpine]", got)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	if !res.DryRun {
		t.Error("stub Prune() should echo DryRun")
	}
	if len(res.ContainersRemoved) != 0 || len(res.ImagesRemoved) != 0 {
		t.Errorf("stub Prune() should return empty removal lists, got %+v", res)
	}
}

func TestStubImplementsPruner(t *testing.T) {
	var p Pruner = NewStub()
	if p == nil {
		t.Fatal("NewStub() does not implement Pruner")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./internal/runtime/... -run "TestSplitNonEmpty|TestParseContainerRow|TestIsRunning|TestParseImageRow|TestSelectImagesToRemove|TestStubPrune|TestStubImplementsPruner" -v -count=1
```

Expected: FAIL with `undefined: splitNonEmpty`, `undefined: PruneOptions`, `undefined: Pruner`, etc. — the types and functions do not exist yet.

- [ ] **Step 4: Create `internal/runtime/prune.go` (core types + pure helpers)**

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const (
	appLabelKey  = "tengiz-app"   // label placed on every Tengiz container
	appImageRepo = "tengiz-apps/" // image repository prefix for all Tengiz builds
)

type PruneOptions struct {
	DryRun     bool // report candidates without removing anything
	Containers bool // stopped containers not managed by Tengiz
	Images     bool // dangling images + unused images outside tengiz-apps/*
	Volumes    bool // unused docker volumes
	Networks   bool // unused docker networks
	BuildCache bool // builder cache
}

type PruneResult struct {
	DryRun            bool
	ContainersRemoved []string
	ImagesRemoved     []string
	VolumesRemoved    []string
	NetworksRemoved   []string
	BuildCachePruned  bool
}

type Pruner interface {
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
}

func runDockerCommand(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return string(out), nil
}

func splitNonEmpty(out string) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			result = append(result, s)
		}
	}
	return result
}

type containerRow struct {
	Name   string
	Status string
}

func parseContainerRow(line string) (containerRow, bool) {
	parts := strings.SplitN(line, "|", 2)
	if len(parts) != 2 {
		return containerRow{}, false
	}
	name := strings.TrimSpace(parts[0])
	status := strings.TrimSpace(parts[1])
	if name == "" || status == "" {
		return containerRow{}, false
	}
	return containerRow{Name: name, Status: status}, true
}

func isRunning(status string) bool {
	return strings.HasPrefix(status, "Up")
}

type imageInfo struct {
	ID      string
	RepoTag string
}

func parseImageRow(line string) (imageInfo, bool) {
	parts := strings.SplitN(line, "|", 2)
	if len(parts) != 2 {
		return imageInfo{}, false
	}
	id := strings.TrimSpace(parts[0])
	tag := strings.TrimSpace(parts[1])
	if id == "" || tag == "" || tag == "<none>:<none>" {
		return imageInfo{}, false
	}
	return imageInfo{ID: id, RepoTag: tag}, true
}

func selectImagesToRemove(images []imageInfo, protected map[string]bool) []string {
	var remove []string
	for _, img := range images {
		if protected[img.ID] || protected[img.RepoTag] {
			continue
		}
		remove = append(remove, img.RepoTag)
	}
	return remove
}
```

- [ ] **Step 5: Add `Prune` to `stubManager` in `internal/runtime/runtime.go`**

Add to the `stubManager` method list (after `func (m *stubManager) KeepLastNImages(...)`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

```bash
go test ./internal/runtime/... -run "TestSplitNonEmpty|TestParseContainerRow|TestIsRunning|TestParseImageRow|TestSelectImagesToRemove|TestStubPrune|TestStubImplementsPruner" -v -count=1
```

Expected: PASS. Then run the whole runtime package to confirm no regressions:

```bash
go test ./internal/runtime/... -v -count=1
```

Expected: All PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/runtime.go internal/runtime/prune_test.go
git commit -m "feat: add prune types, stub pruner, and pure housekeeping helpers"
```

---

### Task 2: Implement the Docker pruner backend

**Files:**
- Modify: `internal/runtime/prune.go` — add `NewPruner()` and the `dockerRuntime` category methods
- Test: `internal/runtime/prune_test.go` — append interface guard tests

**Interfaces:**
- Consumes: helpers from Task 1 (`splitNonEmpty`, `parseContainerRow`, `isRunning`, `parseImageRow`, `selectImagesToRemove`), `Pruner`, `PruneOptions`, `PruneResult`
- Produces:
  - `runtime.NewPruner() (Pruner, error)` — returns `&dockerRuntime{}` after verifying `docker` is on PATH
  - `(*dockerRuntime).Prune(ctx, opts) (PruneResult, error)` — full implementation
  - private methods: `runDockerCommand` (already defined), `pruneContainers(ctx, dryRun bool)`, `protectedImages(ctx context.Context) (map[string]bool, error)`, `pruneImages(ctx, dryRun bool)`, `pruneVolumes(ctx, dryRun bool)`, `pruneNetworks(ctx, dryRun bool)`, `pruneBuildCache(ctx, dryRun bool)`

- [ ] **Step 1: Write the failing interface-guard tests**

Append to `internal/runtime/prune_test.go`:

```go
func TestDockerRuntimeImplementsPruner(t *testing.T) {
	var p Pruner = &dockerRuntime{}
	if p == nil {
		t.Fatal("dockerRuntime does not implement Pruner")
	}
}

func TestSelectImagesToRemoveUnprotectedAll(t *testing.T) {
	images := []imageInfo{
		{ID: "sha256:zzz", RepoTag: "busybox:1"},
	}
	got := selectImagesToRemove(images, map[string]bool{})
	if len(got) != 1 || got[0] != "busybox:1" {
		t.Errorf("selectImagesToRemove = %v, want [busybox:1]", got)
	}
}
```

- [ ] **Step 2: Run tests to verify failures**

```bash
go test ./internal/runtime/... -run "TestDockerRuntimeImplementsPruner|TestSelectImagesToRemoveUnprotectedAll" -v -count=1
```

Expected: FAIL because `dockerRuntime.Prune` does not exist yet (`TestDockerRuntimeImplementsPruner` fails to compile). `TestSelectImagesToRemoveUnprotectedAll` is present but the package fails to build. After Step 3, run the same command again — both PASS.

- [ ] **Step 3: Add `NewPruner()` and the `dockerRuntime.Prune` entry point**

Append to the end of `internal/runtime/prune.go`:

```go
func NewPruner() (Pruner, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerRuntime{}, nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	res := PruneResult{DryRun: opts.DryRun}

	if opts.Containers {
		names, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return res, err
		}
		res.ContainersRemoved = names
	}
	if opts.Images {
		names, err := r.pruneImages(ctx, opts.DryRun)
		if err != nil {
			return res, err
		}
		res.ImagesRemoved = names
	}
	if opts.Volumes {
		names, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return res, err
		}
		res.VolumesRemoved = names
	}
	if opts.Networks {
		names, err := r.pruneNetworks(ctx, opts.DryRun)
		if err != nil {
			return res, err
		}
		res.NetworksRemoved = names
	}
	if opts.BuildCache {
		pruned, err := r.pruneBuildCache(ctx, opts.DryRun)
		if err != nil {
			return res, err
		}
		res.BuildCachePruned = pruned
	}
	return res, nil
}
```

- [ ] **Step 4: Implement the per-category backends**

Append to the end of `internal/runtime/prune.go`:

```go
func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) ([]string, error) {
	out, err := runDockerCommand(ctx, []string{
		"container", "ls", "-a",
		"--filter", "label!=" + appLabelKey,
		"--format", "{{.Names}}|{{.Status}}",
	})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range splitNonEmpty(out) {
		row, ok := parseContainerRow(line)
		if !ok || isRunning(row.Status) {
			continue
		}
		names = append(names, row.Name)
	}
	if dryRun || len(names) == 0 {
		return names, nil
	}
	if _, err := runDockerCommand(ctx, []string{
		"container", "prune", "-f", "--filter", "label!=" + appLabelKey,
	}); err != nil {
		return nil, err
	}
	return names, nil
}

func (r *dockerRuntime) protectedImages(ctx context.Context) (map[string]bool, error) {
	protected := make(map[string]bool)
	out, err := runDockerCommand(ctx, []string{
		"container", "ls", "-a",
		"--filter", "label=" + appLabelKey,
		"--format", "{{.Image}}",
	})
	if err != nil {
		return nil, err
	}
	for _, ref := range splitNonEmpty(out) {
		protected[ref] = true
	}
	imgOut, err := runDockerCommand(ctx, []string{
		"images",
		"--filter", "reference=" + appImageRepo + "*",
		"--format", "{{.Repository}}:{{.Tag}}",
	})
	if err != nil {
		return nil, err
	}
	for _, tag := range splitNonEmpty(imgOut) {
		protected[tag] = true
	}
	return protected, nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, dryRun bool) ([]string, error) {
	if !dryRun {
		if _, err := runDockerCommand(ctx, []string{"image", "prune", "-f"}); err != nil {
			return nil, err
		}
	}
	protected, err := r.protectedImages(ctx)
	if err != nil {
		return nil, err
	}
	out, err := runDockerCommand(ctx, []string{
		"images",
		"--format", "{{.ID}}|{{.Repository}}:{{.Tag}}",
	})
	if err != nil {
		return nil, err
	}
	var all []imageInfo
	for _, line := range splitNonEmpty(out) {
		if info, ok := parseImageRow(line); ok {
			all = append(all, info)
		}
	}
	toRemove := selectImagesToRemove(all, protected)
	if dryRun || len(toRemove) == 0 {
		return toRemove, nil
	}
	var removed []string
	for _, tag := range toRemove {
		if _, err := runDockerCommand(ctx, []string{"rmi", "-f", tag}); err != nil {
			log.Printf("[runtime] cleanup: failed to remove image %s: %v", tag, err)
			continue
		}
		removed = append(removed, tag)
	}
	return removed, nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) ([]string, error) {
	out, err := runDockerCommand(ctx, []string{
		"volume", "ls", "-f", "dangling=true", "--format", "{{.Name}}",
	})
	if err != nil {
		return nil, err
	}
	candidates := splitNonEmpty(out)
	if dryRun || len(candidates) == 0 {
		return candidates, nil
	}
	if _, err := runDockerCommand(ctx, []string{"volume", "prune", "-f"}); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) ([]string, error) {
	out, err := runDockerCommand(ctx, []string{
		"network", "ls", "-f", "dangling=true", "--format", "{{.Name}}",
	})
	if err != nil {
		return nil, err
	}
	candidates := splitNonEmpty(out)
	if dryRun || len(candidates) == 0 {
		return candidates, nil
	}
	if _, err := runDockerCommand(ctx, []string{"network", "prune", "-f"}); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) (bool, error) {
	if dryRun {
		return false, nil
	}
	if _, err := runDockerCommand(ctx, []string{"builder", "prune", "-f"}); err != nil {
		return false, err
	}
	return true, nil
}
```

Add `"log"` to the import block at the top of `prune.go`:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)
```

- [ ] **Step 5: Build and run runtime tests**

```bash
go build ./internal/runtime/... && go vet ./internal/runtime/...
```

Expected: No errors.

```bash
go test ./internal/runtime/... -run "TestDockerRuntimeImplementsPruner|TestSelectImagesToRemoveUnprotectedAll" -v -count=1
```

Expected: PASS.

```bash
go test ./internal/runtime/... -v -count=1
```

Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: implement exec-based docker pruner backend for housekeeping"
```

---

### Task 3: CLI `tengiz cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:38-89` — register `cleanupCmd` in `init()`
- Test: `internal/cli/cmd_cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewPruner() (Pruner, error)`, `runtime.Pruner.Prune(ctx, opts) (PruneResult, error)`, `runtime.NewStub()` (for tests)
- Produces:
  - `cleanupCmd` — registered `cobra.Command` with `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`, `--cache` flags
  - `cleanupOptions(cmd *cobra.Command) (runtime.PruneOptions, error)` — maps flags to options; if no category flag is set, all categories are enabled
  - `runCleanup(ctx context.Context, p runtime.Pruner, opts runtime.PruneOptions) error` — prints the summary
  - `renderCleanupSummary(res runtime.PruneResult) string` — pure text formatting
  - `writeCleanupSection(b *strings.Builder, name string, items []string)` — helper used by the renderer

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cmd_cleanup_test.go`:

```go
package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func newCleanupTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "cleanup-test"}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("cache", false, "")
	return cmd
}

func TestCleanupOptionsDefaultAll(t *testing.T) {
	opts, err := cleanupOptions(newCleanupTestCmd(t))
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories by default, got %+v", opts)
	}
}

func TestCleanupOptionsSingleCategory(t *testing.T) {
	cmd := newCleanupTestCmd(t)
	if err := cmd.Flags().Set("containers", "true"); err != nil {
		t.Fatal(err)
	}
	opts, err := cleanupOptions(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Containers {
		t.Error("expected containers true when --containers set")
	}
	if opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("expected other categories false, got %+v", opts)
	}
}

func TestCleanupOptionsDryRun(t *testing.T) {
	cmd := newCleanupTestCmd(t)
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	opts, err := cleanupOptions(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.DryRun {
		t.Error("expected dry-run true")
	}
}

func TestRenderCleanupSummaryDryRun(t *testing.T) {
	res := runtime.PruneResult{
		DryRun:            true,
		ContainersRemoved: []string{"braqui-orphan"},
		ImagesRemoved:     []string{"node:20-alpine"},
	}
	out := renderCleanupSummary(res)
	if !strings.Contains(out, "dry run") {
		t.Errorf("summary missing dry-run marker: %s", out)
	}
	if !strings.Contains(out, "braqui-orphan") {
		t.Errorf("summary missing container name: %s", out)
	}
	if !strings.Contains(out, "node:20-alpine") {
		t.Errorf("summary missing image name: %s", out)
	}
}

func TestRunCleanupWithStub(t *testing.T) {
	output := captureOutput(func() {
		err := runCleanup(context.Background(), runtime.NewStub(), runtime.PruneOptions{DryRun: true})
		if err != nil {
			t.Errorf("runCleanup() error = %v", err)
		}
	})
	if !strings.Contains(output, "dry run") {
		t.Errorf("expected dry-run output, got %q", output)
	}
}

func TestCleanupHelpText(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--help"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("cleanup --help failed: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/cli/... -run "TestCleanup" -v -count=1
```

Expected: FAIL — `undefined: cleanupCmd`, `undefined: cleanupOptions`, `undefined: renderCleanupSummary`, `undefined: runCleanup`.

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Removes unused containers, images, volumes, networks, and build caches.

Tengiz-managed resources are always preserved:
  - containers with the "tengiz-app" label (including scale-to-zero stopped containers)
  - images under the "tengiz-apps/" repository plus images referenced by Tengiz containers

By default all resource categories are cleaned. Use the per-category flags to
limit the cleanup, e.g. 'tengiz cleanup --images'.
Use '--dry-run' to see what would be removed without removing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptions(cmd)
		if err != nil {
			return err
		}
		pruner, err := runtime.NewPruner()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		return runCleanup(cmd.Context(), pruner, opts)
	},
}

func cleanupOptions(cmd *cobra.Command) (runtime.PruneOptions, error) {
	opts := runtime.PruneOptions{}
	opts.DryRun, _ = cmd.Flags().GetBool("dry-run")

	categories := []struct {
		name string
		dst  *bool
	}{
		{"containers", &opts.Containers},
		{"images", &opts.Images},
		{"volumes", &opts.Volumes},
		{"networks", &opts.Networks},
		{"cache", &opts.BuildCache},
	}
	anySet := false
	for _, c := range categories {
		if v, _ := cmd.Flags().GetBool(c.name); v {
			*c.dst = true
			anySet = true
		}
	}
	if !anySet {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts, nil
}

func runCleanup(ctx context.Context, p runtime.Pruner, opts runtime.PruneOptions) error {
	res, err := p.Prune(ctx, opts)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	fmt.Print(renderCleanupSummary(res))
	return nil
}

func renderCleanupSummary(res runtime.PruneResult) string {
	var b strings.Builder
	if res.DryRun {
		fmt.Fprintf(&b, "[dry run] nothing was removed. Candidates:\n")
	} else {
		fmt.Fprintf(&b, "cleanup complete.\n")
	}
	writeCleanupSection(&b, "containers", res.ContainersRemoved)
	writeCleanupSection(&b, "images", res.ImagesRemoved)
	writeCleanupSection(&b, "volumes", res.VolumesRemoved)
	writeCleanupSection(&b, "networks", res.NetworksRemoved)
	if res.BuildCachePruned {
		fmt.Fprintf(&b, "  build cache: pruned\n")
	}
	return b.String()
}

func writeCleanupSection(b *strings.Builder, name string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "  %s (%d):\n", name, len(items))
	for _, it := range items {
		fmt.Fprintf(b, "    - %s\n", it)
	}
}
```

- [ ] **Step 4: Register the command and its flags in `internal/cli/root.go`**

In `init()`, right after the `rootCmd.AddCommand(volumeCmd)` line (around line 64 in `root.go`), add:

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune unused non-Tengiz images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused docker volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused docker networks")
	cleanupCmd.Flags().Bool("cache", false, "prune build caches")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run the CLI tests**

```bash
go test ./internal/cli/... -run "TestCleanup" -v -count=1
```

Expected: PASS.

```bash
go test ./internal/cli/... -v -count=1
```

Expected: All PASS (no regressions from the new registration).

- [ ] **Step 6: Build and smoke-test**

```bash
go build -o tengiz .
./tengiz cleanup --help
```

Expected: `cleanup` help shows all six flags and the description. Then:

```bash
./tengiz cleanup --dry-run || true
```

On a host with no Docker daemon this prints `Error: docker not found in PATH` — expected. On a host with Docker it prints the dry-run candidate list.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root.go internal/cli/cmd_cleanup_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Documentation and final verification

**Files:**
- Modify: `README.md` — add `### tengiz cleanup` section (after `tengiz rollback`)
- Modify: `AGENTS.md` — add the `tengiz cleanup` command to the CLI list
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:**
- Consumes: the CLI command and flags shipped in Task 3
- Produces: documentation users and agents can rely on; the feature marked implemented

- [ ] **Step 1: Add the cleanup section to `README.md`**

Insert directly after the `### tengiz rollback <app>` section (after its argument table, just before the `### tengiz domain` section):

````markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space. Tengiz-managed resources
are always preserved: containers carrying the `tengiz-app` label (including
scale-to-zero stopped containers) and all images under `tengiz-apps/`.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling or unused non-Tengiz images |
| `--volumes` | Prune unused Docker volumes |
| `--networks` | Prune unused Docker networks |
| `--cache` | Prune Docker builder caches |

With no category flag, all categories are cleaned.

```bash
tengiz cleanup                  # prune everything safe
tengiz cleanup --dry-run        # preview candidates
tengiz cleanup --images         # only unused images
```
````

- [ ] **Step 2: Add the command to `AGENTS.md`**

After the `tengiz rollback <app>` line in the CLI command list, add:

```
tengiz cleanup [--dry-run|--containers|--images|--volumes|--networks|--cache] → prune unused Docker resources (Tengiz-managed containers/images always preserved)
```

- [ ] **Step 3: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

Replace the P0 table row for #6:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Implemented (2026-08-08) — `tengiz cleanup`, per-category with label-guards and `--dry-run` |
```

Add a row to the "Implemented Features (Not Pending)" table:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-08) |
```

In the detailed `## Docker Housekeeping (Otomatik Temizlik)` section, add after the `**Why add to Tengiz:**` paragraph:

```
- **Status:** ✅ Implemented (2026-08-08)
```

- [ ] **Step 4: Run the full verification suite**

```bash
go build -o tengiz .
go vet ./...
go test ./... -v -count=1
```

Expected: build succeeds, vet clean, all tests PASS. (`proxy` tests take ~2s each due to TCP dial timeouts; allow the run to complete.)

- [ ] **Step 5: Manual smoke test on a Docker host (recommended)**

```bash
./tengiz cleanup --dry-run
./tengiz cleanup --images --dry-run
```

Expected: prints candidate lists (possibly empty) and confirms nothing was removed.

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document docker housekeeping cleanup command"
```

---

## Self-Review

**1. Spec coverage**

- **Feature #6 "Docker Housekeeping"** — Ratio: "Disk space is the #1 production problem ... `docker system prune`. `tengiz cleanup`." ✅ Fully addressed:
  - `tengiz cleanup` command (Task 3)
  - per-category prune: containers, images, volumes, networks, build cache (Task 2)
  - label-based protection of Tengiz containers/images (Task 2: `--filter label=tengiz-app`, `--filter label!=tengiz-app`, `appImageRepo` prefix protection)
  - dry-run preview (Task 2 + Task 3)
- Feature detail "Label-based `docker system prune`" is implemented per-category rather than as one literal `docker system prune` call; this is a deliberate safety decision (preserves scale-to-zero stopped containers and Tengiz images while still reclaiming disk) and is documented in Global Constraints.
- Coolify's **periodic** cleanup job (`DockerCleanupJob`) is intentionally NOT implemented here — it requires a background scheduler, which is P1 feature #57. Reuses `runtime.Pruner` when built.

**2. Placeholder scan.** No TODO/TBD/"implement later"/"add appropriate..." language. Every code step carries complete, compilable code and exact verification commands.

**3. Type consistency**

- `PruneOptions` field names used identically in all tasks: `DryRun`, `Containers`, `Images`, `Volumes`, `Networks`, `BuildCache`. CLI maps `--containers`→`Containers`, `--images`→`Images`, `--volumes`→`Volumes`, `--networks`→`Networks`, `--cache`→`BuildCache`. ✅
- `PruneResult` field names consistent and used in `renderCleanupSummary`: `ContainersRemoved`, `ImagesRemoved`, `VolumesRemoved`, `NetworksRemoved`, `BuildCachePruned`, `DryRun`. ✅
- `Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)` identical interface sign-extensions everywhere; stub, docker, and CLI callers match. ✅
- Helper signatures: `parseContainerRow`/`parseImageRow` return `(<struct>, bool)`; `isRunning` takes `status string`; `selectImagesToRemove(images []imageInfo, protected map[string]bool) []string`. ✅
- `runDockerCommand(ctx, []string) (string, error)` used by all exec paths. ✅