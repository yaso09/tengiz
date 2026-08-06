# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that removes unused Docker containers, images, volumes, and networks to reclaim disk space, while never touching Tengiz-managed resources.

**Architecture:** New pure helper functions in `internal/runtime/prune.go` build the exact `docker` CLI arguments and parse their output; these are unit-tested without Docker. `dockerRuntime.Prune(ctx, opts)` runs one prune command per enabled category. Containers are protected with the `label!=tengiz-app` prune filter (verified: stops with that label survive, unlabeled stopped containers are removed). Images are protected by construction: pass 1 prunes only dangling (untagged) images, pass 2 lists tagged images and removes only those that are **not** in the `tengiz-apps/*` repository and **not** referenced by any container. `--dry-run` runs `docker system df` to report reclaimable space. The `tengiz cleanup` Cobra command wires these together.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` docker CLI (no Docker SDK), existing `runtime.Manager` interface, `log/slog`-style `log` package. No new external dependencies.

## Global Constraints

- Never remove a container that carries the `tengiz-app` label (verified with Docker 28: `docker container prune -f --filter "label!=tengiz-app"` removes only unlabeled stopped containers)
- Never remove an image in the `tengiz-apps/*` repository (handled by `KeepLastNImages`/rollback)
- Never remove an image referenced by any container (running or stopped)
- `docker image prune` does **not** support the `reference` filter — do not attempt `--filter reference=...`
- Image cleanup must prune dangling images via `docker image prune -f` (no `-a`), because `-a` would remove tagged tengiz-apps images
- Default command behavior (no category flags) prunes all four categories
- `--dry-run` must remove nothing
- All docker CLI arg builders and output parsers must be pure functions unit-tested without a Docker daemon
- No new external Go dependencies
- All existing tests must continue to pass without modification (except adding the two new methods to `mockRTForDeploy`)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` (create) | `PruneOptions`/`PruneReport`/`DiskReport` types; pure docker arg builders; `parsePruneOutput`, `parseSystemDF`, `splitRepoTag`, `selectUnusedImages`, `pruneOrder`, `nonEmptyLines`, `categorySpace`; `dockerRuntime.Prune`, `pruneImages`, `listTaggedImages`, `listInUseImages`, `runPrune`, `DiskUsage` |
| `internal/runtime/prune_test.go` (create) | Unit tests for every pure function + stub `Prune`/`DiskUsage` |
| `internal/runtime/runtime.go` (modify) | Add `Prune(ctx, PruneOptions) (PruneReport, error)` and `DiskUsage(ctx) (DiskReport, error)` to the `Manager` interface (lines 31-49) and to `stubManager` |
| `internal/cli/root.go` (modify) | New `cleanupCmd` Cobra command, `cleanupOptions` and `orDash` helpers, flag registration in `init()` |
| `internal/cli/root_test.go` (modify) | Add `Prune`/`DiskUsage` to `mockRTForDeploy` (lines 98-100); cleanup command/flags/options tests |
| `README.md` (modify) | Add `tengiz cleanup` to Features list (after line 23) and CLI Reference (after line 237) |

---

### Task 1: Pure prune primitives (types, arg builders, parsers, image selector)

**Files:**
- Create: `internal/runtime/prune.go`
- Create: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `PruneOptions{Containers, Images, Volumes, Networks bool}`, `PruneReport{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved int, SpaceReclaimed []string}`, `DiskReport{Images, Containers, Volumes, BuildCache string}`, and pure functions `pruneOrder(opts PruneOptions) []string`, `pruneContainerArgs() []string`, `pruneVolumeArgs() []string`, `pruneNetworkArgs() []string`, `pruneDanglingImagesArgs() []string`, `listImagesArgs() []string`, `listInUseImagesArgs() []string`, `systemDFArgs() []string`, `parsePruneOutput(output string) (int, string)`, `parseSystemDF(output string) (DiskReport, error)`, `splitRepoTag(ref string) (string, string)`, `selectUnusedImages(images, inUse []string) []string` — used by Task 2.

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/prune_test.go
package runtime

import (
	"reflect"
	"testing"
)

func TestPruneContainerArgs(t *testing.T) {
	got := pruneContainerArgs()
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneContainerArgs() = %v, want %v", got, want)
	}
}

func TestPruneVolumeArgs(t *testing.T) {
	got := pruneVolumeArgs()
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneVolumeArgs() = %v, want %v", got, want)
	}
}

func TestPruneNetworkArgs(t *testing.T) {
	got := pruneNetworkArgs()
	want := []string{"network", "prune", "-f", "--filter", "label!=tengiz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneNetworkArgs() = %v, want %v", got, want)
	}
}

func TestPruneDanglingImagesArgs(t *testing.T) {
	got := pruneDanglingImagesArgs()
	want := []string{"image", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneDanglingImagesArgs() = %v, want %v", got, want)
	}
}

func TestListImagesArgs(t *testing.T) {
	got := listImagesArgs()
	want := []string{"images", "--filter", "dangling=false", "--format", "{{.Repository}}:{{.Tag}}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listImagesArgs() = %v, want %v", got, want)
	}
}

func TestListInUseImagesArgs(t *testing.T) {
	got := listInUseImagesArgs()
	want := []string{"ps", "-a", "--format", "{{.Image}}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listInUseImagesArgs() = %v, want %v", got, want)
	}
}

func TestSystemDFArgs(t *testing.T) {
	got := systemDFArgs()
	want := []string{"system", "df", "--format", "{{.Type}}|{{.Reclaimable}}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("systemDFArgs() = %v, want %v", got, want)
	}
}

func TestPruneOrder(t *testing.T) {
	tests := []struct {
		name     string
		opts     PruneOptions
		expected []string
	}{
		{"none", PruneOptions{}, []string{}},
		{"all", PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true}, []string{"containers", "images", "volumes", "networks"}},
		{"images only", PruneOptions{Images: true}, []string{"images"}},
		{"containers and networks", PruneOptions{Containers: true, Networks: true}, []string{"containers", "networks"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneOrder(tt.opts)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("pruneOrder() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParsePruneOutputContainers(t *testing.T) {
	const output = `Deleted Containers:
71eee59407deea0367cff00b0a0399a332661a2d9568f477851f8db55ee02985

Total reclaimed space: 0B
`
	removed, space := parsePruneOutput(output)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if space != "0B" {
		t.Fatalf("space = %q, want %q", space, "0B")
	}
}

func TestParsePruneOutputImages(t *testing.T) {
	const output = `Deleted Images:
untagged: foo:latest
deleted: sha256:abc123
untagged: bar:latest
deleted: sha256:def456

Total reclaimed space: 1.5GB
`
	removed, space := parsePruneOutput(output)
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if space != "1.5GB" {
		t.Fatalf("space = %q, want %q", space, "1.5GB")
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	removed, space := parsePruneOutput("")
	if removed != 0 || space != "" {
		t.Fatalf("removed = %d, space = %q; want 0, empty", removed, space)
	}
}

func TestParseSystemDF(t *testing.T) {
	const output = `Images|1.851GB (100%)
Containers|0B
Local Volumes|0B
Build Cache|0B`
	report, err := parseSystemDF(output)
	if err != nil {
		t.Fatal(err)
	}
	if report.Images != "1.851GB (100%)" {
		t.Errorf("Images = %q, want %q", report.Images, "1.851GB (100%)")
	}
	if report.Containers != "0B" {
		t.Errorf("Containers = %q, want %q", report.Containers, "0B")
	}
	if report.Volumes != "0B" {
		t.Errorf("Volumes = %q, want %q", report.Volumes, "0B")
	}
	if report.BuildCache != "0B" {
		t.Errorf("BuildCache = %q, want %q", report.BuildCache, "0B")
	}
}

func TestParseSystemDFEmpty(t *testing.T) {
	if _, err := parseSystemDF(""); err == nil {
		t.Fatal("expected error for empty output")
	}
}

func TestSplitRepoTag(t *testing.T) {
	tests := []struct {
		ref      string
		wantRepo string
		wantTag  string
	}{
		{"alpine:latest", "alpine", "latest"},
		{"ghcr.io/org/img", "ghcr.io/org/img", ""},
		{"ghcr.io:5000/org/img", "ghcr.io:5000/org/img", ""},
		{"tengiz-apps/myapp:123", "tengiz-apps/myapp", "123"},
		{"redis", "redis", ""},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			repo, tag := splitRepoTag(tt.ref)
			if repo != tt.wantRepo || tag != tt.wantTag {
				t.Fatalf("splitRepoTag(%q) = (%q, %q), want (%q, %q)", tt.ref, repo, tag, tt.wantRepo, tt.wantTag)
			}
		})
	}
}

func TestSelectUnusedImages(t *testing.T) {
	images := []string{"alpine:latest", "tengiz-apps/myapp:123", "redis:7", "busybox:1.36"}
	inUse := []string{"alpine", "tengiz-apps/myapp:123"}
	got := selectUnusedImages(images, inUse)
	want := []string{"redis:7", "busybox:1.36"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectUnusedImages() = %v, want %v", got, want)
	}
}

func TestSelectUnusedImagesEmpty(t *testing.T) {
	if got := selectUnusedImages(nil, nil); len(got) != 0 {
		t.Fatalf("selectUnusedImages(nil, nil) = %v, want empty", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestPruneContainerArgs|TestParsePruneOutput|TestSelectUnusedImages' -count=1 -v`

Expected: FAIL with undefined references (e.g. `undefined: pruneContainerArgs`).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/runtime/prune.go
package runtime

import (
	"errors"
	"strings"
)

const (
	pruneContainerFilter = "label!=tengiz-app"
	pruneVolumeFilter    = "label!=tengiz"
	pruneNetworkFilter   = "label!=tengiz"
	tengizImagePrefix    = "tengiz-apps/"
)

// PruneOptions selects which Docker resource categories to clean up.
type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

// PruneReport summarizes the results of a cleanup run.
type PruneReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	SpaceReclaimed    []string
}

// DiskReport shows how much space is reclaimable per category (docker system df).
type DiskReport struct {
	Images     string
	Containers string
	Volumes    string
	BuildCache string
}

// pruneOrder returns the enabled categories in a stable order.
func pruneOrder(opts PruneOptions) []string {
	var cats []string
	if opts.Containers {
		cats = append(cats, "containers")
	}
	if opts.Images {
		cats = append(cats, "images")
	}
	if opts.Volumes {
		cats = append(cats, "volumes")
	}
	if opts.Networks {
		cats = append(cats, "networks")
	}
	return cats
}

func pruneContainerArgs() []string {
	return []string{"container", "prune", "-f", "--filter", pruneContainerFilter}
}

func pruneVolumeArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", pruneVolumeFilter}
}

func pruneNetworkArgs() []string {
	return []string{"network", "prune", "-f", "--filter", pruneNetworkFilter}
}

func pruneDanglingImagesArgs() []string {
	return []string{"image", "prune", "-f"}
}

func listImagesArgs() []string {
	return []string{"images", "--filter", "dangling=false", "--format", "{{.Repository}}:{{.Tag}}"}
}

func listInUseImagesArgs() []string {
	return []string{"ps", "-a", "--format", "{{.Image}}"}
}

func systemDFArgs() []string {
	return []string{"system", "df", "--format", "{{.Type}}|{{.Reclaimable}}"}
}

// parsePruneOutput extracts the number of removed items and the reclaimed
// space from a single `docker <type> prune` invocation.
func parsePruneOutput(output string) (int, string) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	removed := 0
	space := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Deleted ") && strings.HasSuffix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			space = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			continue
		}
		if strings.HasPrefix(line, "untagged:") {
			continue
		}
		if strings.HasPrefix(line, "deleted:") {
			removed++
			continue
		}
		removed++
	}
	return removed, space
}

// parseSystemDF parses `docker system df --format "{{.Type}}|{{.Reclaimable}}"`.
func parseSystemDF(output string) (DiskReport, error) {
	var report DiskReport
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.TrimSpace(parts[0]) {
		case "Images":
			report.Images = strings.TrimSpace(parts[1])
		case "Containers":
			report.Containers = strings.TrimSpace(parts[1])
		case "Local Volumes":
			report.Volumes = strings.TrimSpace(parts[1])
		case "Build Cache":
			report.BuildCache = strings.TrimSpace(parts[1])
		}
	}
	if report.Images == "" && report.Containers == "" && report.Volumes == "" && report.BuildCache == "" {
		return report, errors.New("no docker system df data parsed")
	}
	return report, nil
}

// splitRepoTag splits an image reference into repository and tag.
// A colon immediately followed by a slash is a registry port, not a tag.
func splitRepoTag(ref string) (string, string) {
	i := strings.LastIndex(ref, ":")
	if i < 0 || strings.Contains(ref[i+1:], "/") {
		return ref, ""
	}
	return ref[:i], ref[i+1:]
}

// selectUnusedImages returns image refs that are safe to remove: not in the
// tengiz-apps repository and not referenced by any container (by exact ref
// or by repository name).
func selectUnusedImages(images, inUse []string) []string {
	exact := make(map[string]bool, len(inUse))
	repos := make(map[string]bool, len(inUse))
	for _, ref := range inUse {
		exact[ref] = true
		if repo, _ := splitRepoTag(ref); repo != "" {
			repos[repo] = true
		}
	}
	var unused []string
	for _, img := range images {
		if strings.HasPrefix(img, tengizImagePrefix) {
			continue
		}
		if exact[img] {
			continue
		}
		if repo, _ := splitRepoTag(img); repos[repo] {
			continue
		}
		unused = append(unused, img)
	}
	return unused
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -count=1 -v`

Expected: PASS for all prune tests, and `TestStubSatisfiesInterface` still passes (no interface change yet).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: add docker prune primitives for cleanup command"
```

---

### Task 2: Wire `Prune` and `DiskUsage` into the runtime layer

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add two methods to the `Manager` interface; add them to `stubManager` (after line 118)
- Modify: `internal/runtime/prune.go` — add `dockerRuntime` implementations
- Modify: `internal/cli/root_test.go:98-100` — add both methods to `mockRTForDeploy` (required so the package still compiles)
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: all types and pure functions from Task 1: `PruneOptions`, `PruneReport`, `DiskReport`, `pruneOrder`, `pruneContainerArgs`, `pruneVolumeArgs`, `pruneNetworkArgs`, `pruneDanglingImagesArgs`, `listImagesArgs`, `listInUseImagesArgs`, `systemDFArgs`, `parsePruneOutput`, `parseSystemDF`, `selectUnusedImages`
- Produces: `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`, `Manager.DiskUsage(ctx context.Context) (DiskReport, error)` — consumed by Task 3; pure helpers `nonEmptyLines(s string) []string` and `categorySpace(cat, space string) string` for testing

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/prune_test.go — append these tests
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 || len(report.SpaceReclaimed) != 0 {
		t.Fatalf("expected empty report, got %+v", report)
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	report, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if report.Images != "" || report.Containers != "" || report.Volumes != "" || report.BuildCache != "" {
		t.Fatalf("expected empty report, got %+v", report)
	}
}

func TestCategorySpace(t *testing.T) {
	if got := categorySpace("containers", "3.2MB"); got != "containers: 3.2MB" {
		t.Errorf("categorySpace() = %q, want %q", got, "containers: 3.2MB")
	}
	if got := categorySpace("images", ""); got != "images: 0B" {
		t.Errorf("categorySpace() empty = %q, want %q", got, "images: 0B")
	}
}

func TestNonEmptyLines(t *testing.T) {
	got := nonEmptyLines("a\n\nb\n")
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nonEmptyLines() = %v, want %v", got, want)
	}
	if got := nonEmptyLines(""); len(got) != 0 {
		t.Fatalf("nonEmptyLines(\"\") = %v, want empty", got)
	}
}
```

The test file imports `context` — add it to the existing import block (`import ( "context"; "reflect"; "testing" )`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestStubPrune|TestStubDiskUsage|TestCategorySpace|TestNonEmptyLines' -count=1 -v`

Expected: FAIL — `stubManager does not implement Manager (missing method Prune)` (compile error) or `undefined: categorySpace`.

- [ ] **Step 3: Add the two methods to the `Manager` interface and stub**

In `internal/runtime/runtime.go`, after the `Run(...)` line (line 48), add:

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
	DiskUsage(ctx context.Context) (DiskReport, error)
```

In `internal/runtime/runtime.go`, after the `stubManager.Run` method (line 122), add:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}

func (m *stubManager) DiskUsage(ctx context.Context) (DiskReport, error) {
	return DiskReport{}, nil
}
```

- [ ] **Step 4: Add the methods to `mockRTForDeploy` in the CLI test package**

In `internal/cli/root_test.go`, after the `Run` method of `mockRTForDeploy` (line 100), add:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
func (m *mockRTForDeploy) DiskUsage(ctx context.Context) (runtime.DiskReport, error) {
	return runtime.DiskReport{}, nil
}
```

- [ ] **Step 5: Implement the `dockerRuntime` methods**

Update `internal/runtime/prune.go` imports to:

```go
import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
)
```

Append to `internal/runtime/prune.go`:

```go
// Prune runs the cleanup for each enabled category in a stable order.
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport
	for _, cat := range pruneOrder(opts) {
		switch cat {
		case "containers":
			removed, space, err := r.runPrune(ctx, pruneContainerArgs())
			if err != nil {
				return report, err
			}
			report.ContainersRemoved = removed
			report.SpaceReclaimed = append(report.SpaceReclaimed, categorySpace("containers", space))
		case "images":
			removed, space, err := r.pruneImages(ctx)
			if err != nil {
				return report, err
			}
			report.ImagesRemoved = removed
			report.SpaceReclaimed = append(report.SpaceReclaimed, categorySpace("images", space))
		case "volumes":
			removed, space, err := r.runPrune(ctx, pruneVolumeArgs())
			if err != nil {
				return report, err
			}
			report.VolumesRemoved = removed
			report.SpaceReclaimed = append(report.SpaceReclaimed, categorySpace("volumes", space))
		case "networks":
			removed, space, err := r.runPrune(ctx, pruneNetworkArgs())
			if err != nil {
				return report, err
			}
			report.NetworksRemoved = removed
			report.SpaceReclaimed = append(report.SpaceReclaimed, categorySpace("networks", space))
		}
	}
	return report, nil
}

func categorySpace(cat, space string) string {
	if space == "" {
		return cat + ": 0B"
	}
	return cat + ": " + space
}

// runPrune runs one docker prune command and parses its output.
func (r *dockerRuntime) runPrune(ctx context.Context, args []string) (int, string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	removed, space := parsePruneOutput(string(out))
	return removed, space, nil
}

// pruneImages removes dangling images, then unused tagged images outside the
// tengiz-apps repository that are not referenced by any container.
func (r *dockerRuntime) pruneImages(ctx context.Context) (int, string, error) {
	removed, space, err := r.runPrune(ctx, pruneDanglingImagesArgs())
	if err != nil {
		return 0, "", err
	}
	images, err := r.listTaggedImages(ctx)
	if err != nil {
		return 0, "", err
	}
	inUse, err := r.listInUseImages(ctx)
	if err != nil {
		return 0, "", err
	}
	for _, img := range selectUnusedImages(images, inUse) {
		if err := r.RemoveImage(ctx, img); err != nil {
			log.Printf("[runtime] cleanup: failed to remove image %s: %v", img, err)
			continue
		}
		removed++
	}
	return removed, space, nil
}

func (r *dockerRuntime) listTaggedImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", listImagesArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return nonEmptyLines(string(out)), nil
}

func (r *dockerRuntime) listInUseImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", listInUseImagesArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return nonEmptyLines(string(out)), nil
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// DiskUsage reports reclaimable space per category via `docker system df`.
func (r *dockerRuntime) DiskUsage(ctx context.Context) (DiskReport, error) {
	cmd := exec.CommandContext(ctx, "docker", systemDFArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return DiskReport{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseSystemDF(string(out))
}
```

- [ ] **Step 6: Run all tests to verify they pass**

Run: `go test ./... -count=1`

Expected: PASS for all packages (the whole module compiles because `mockRTForDeploy` was updated in Step 4).

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune.go internal/runtime/prune_test.go internal/cli/root_test.go
git commit -m "feat: add Prune and DiskUsage to runtime manager"
```

---

### Task 3: `tengiz cleanup` CLI command + docs

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, `cleanupOptions`, `orDash`; register command and flags in `init()`
- Modify: `internal/cli/root_test.go` — command registration, flags, `cleanupOptions`, `orDash` tests
- Modify: `README.md` — Features list bullet + CLI Reference section

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `Manager.Prune`, `Manager.DiskUsage`, `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.DiskReport`
- Produces: none

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/root_test.go — append these tests
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, name := range []string{"containers", "images", "volumes", "networks", "dry-run"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup command missing --%s flag", name)
		}
	}
}

func TestCleanupOptionsDefaults(t *testing.T) {
	all := cleanupOptions(false, false, false, false)
	if !all.Containers || !all.Images || !all.Volumes || !all.Networks {
		t.Errorf("no flags should enable all categories, got %+v", all)
	}
	imgs := cleanupOptions(false, true, false, false)
	if imgs.Containers || !imgs.Images || imgs.Volumes || imgs.Networks {
		t.Errorf("--images should enable only images, got %+v", imgs)
	}
}

func TestOrDash(t *testing.T) {
	if got := orDash(""); got != "-" {
		t.Errorf("orDash(\"\") = %q, want %q", got, "-")
	}
	if got := orDash("1.5GB"); got != "1.5GB" {
		t.Errorf("orDash(%q) = %q, want %q", "1.5GB", got, "1.5GB")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestOrDash' -count=1 -v`

Expected: FAIL — `undefined: cleanupCmd` (compile error).

- [ ] **Step 3: Add the command, flags, and helpers**

In `internal/cli/root.go`, in `init()`, right after the line `rootCmd.AddCommand(notificationCmd)` (line 75), add:

```go
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove unused images not in tengiz-apps/*")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("dry-run", false, "report reclaimable space without removing anything")
	rootCmd.AddCommand(cleanupCmd)
```

In `internal/cli/root.go`, after the `runCmd` definition (after line 1162, before `var gitCmd`), add:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to free disk space",
	Long: `Remove unused Docker containers, images, volumes, and networks.

Tengiz-managed resources are always protected: containers with the
tengiz-app label and images in the tengiz-apps/* repository are never
removed, even when stopped or unused.

Use --containers, --images, --volumes, or --networks to prune a specific
category. With no category flag, all four categories are pruned.

Use --dry-run to report how much space could be reclaimed without
removing anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if dryRun {
			usage, err := rt.DiskUsage(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Println("[tengiz] dry run — nothing removed. Reclaimable space:")
			fmt.Printf("  images:     %s\n", orDash(usage.Images))
			fmt.Printf("  containers: %s\n", orDash(usage.Containers))
			fmt.Printf("  volumes:    %s\n", orDash(usage.Volumes))
			fmt.Printf("  build:      %s\n", orDash(usage.BuildCache))
			return nil
		}

		report, err := rt.Prune(cmd.Context(), cleanupOptions(containers, images, volumes, networks))
		if err != nil {
			return err
		}
		fmt.Println("[tengiz] cleanup complete")
		fmt.Printf("  containers removed: %d\n", report.ContainersRemoved)
		fmt.Printf("  images removed:     %d\n", report.ImagesRemoved)
		fmt.Printf("  volumes removed:    %d\n", report.VolumesRemoved)
		fmt.Printf("  networks removed:   %d\n", report.NetworksRemoved)
		for _, line := range report.SpaceReclaimed {
			fmt.Printf("  reclaimed %s\n", line)
		}
		return nil
	},
}

// cleanupOptions returns the requested categories, or all categories when
// none are explicitly requested.
func cleanupOptions(containers, images, volumes, networks bool) runtime.PruneOptions {
	if containers || images || volumes || networks {
		return runtime.PruneOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
		}
	}
	return runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -count=1`

Expected: PASS for all packages.

- [ ] **Step 5: Manually verify end-to-end against Docker (dev machine with Docker)**

Run:
```bash
go build -o tengiz .
docker pull alpine:latest
docker run -d --name tengiz-protected --label tengiz-app=myapp alpine sleep 30
docker run -d --name foreign-junk alpine sleep 30
docker stop tengiz-protected foreign-junk
./tengiz cleanup --dry-run
./tengiz cleanup
docker ps -a --format "{{.Names}}"
docker rm -f tengiz-protected
docker rmi alpine:latest
```

Expected: `--dry-run` prints reclaimable space and removes nothing; `cleanup` reports `containers removed: 1` (only `foreign-junk`, not `tengiz-protected`) and the final `docker ps -a` lists only `tengiz-protected`.

- [ ] **Step 6: Update README.md**

In the Features list (after the `**Self-contained**` bullet on line 23), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` removes unused containers, images, volumes, and networks; Tengiz-managed resources are always protected.
```

In the CLI Reference, after the `tengiz rollback` section (after line 237, before `### tengiz domain`), add:

```markdown
### `tengiz cleanup [--containers] [--images] [--volumes] [--networks] [--dry-run]`

Remove unused Docker resources to free disk space. Tengiz-managed resources are always protected: containers with the `tengiz-app` label and images in the `tengiz-apps/*` repository are never removed, even when stopped or unused.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove unused images outside `tengiz-apps/*` |
| `--volumes` | Remove unused volumes |
| `--networks` | Remove unused networks |
| `--dry-run` | Report reclaimable space without removing anything |

With no category flag, all four categories are pruned.

Examples:

```bash
tengiz cleanup
tengiz cleanup --images --volumes
tengiz cleanup --dry-run
```
```

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go README.md
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

## Out of Scope (future work)

- Periodic/automatic cleanup scheduler (the `DockerCleanupJob` from the source feature) — the manual `tengiz cleanup` command is the P0 deliverable
- Build cache pruning (`docker builder prune`) and git GC — tracked separately as P2 features #56/#103
- Per-category dry-run reporting of item counts — `--dry-run` currently reports reclaimable space only
