# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, networks, and build cache while guaranteeing that every Tengiz-managed container and tagged app image is never removed.

**Architecture:** Docker is invoked via `os/exec` (no Docker SDK), matching the existing `runtime` package pattern. The `Manager` interface gains two methods: `Cleanup(ctx, CleanupOptions) ([]CleanupResult, error)` and `DiskUsage(ctx) (DiskUsage, error)`. Container pruning uses `docker container prune -f --filter label!=tengiz-app` so stopped Tengiz containers (all carry the `tengiz-app` label) are protected. Image pruning targets only dangling images — tagged `tengiz-apps/...` images are owned by versioned deploys and are already trimmed by the existing `KeepLastNImages`. All docker-exec output parsing is extracted into pure, unit-testable functions.

**Tech Stack:** Go 1.26 standard library (`os/exec`, `regexp`, `strconv`, `encoding/json`), Cobra CLI, existing `internal/runtime` package. No new dependencies.

## Global Constraints

- Runtime calls the `docker` CLI via `os/exec` — never the Docker SDK
- Every Tengiz-managed container carries the `tengiz-app=<appname>` label; container pruning MUST use `--filter label!=tengiz-app` (negated label presence) so no Tengiz container is ever pruned
- Image pruning MUST use `docker image prune -f` (dangling images only) — never `--all`, never a `tengiz-apps/...` reference filter
- Tagged `tengiz-apps/<app>:<env>-<deploymentID>` and `-latest` images are handled by existing `RemoveImage`/`KeepLastNImages`; `tengiz cleanup` must not remove them
- Tengiz persistence uses host-path bind mounts, not named volumes, so `docker volume prune -f` is safe
- `CleanupOptions.Categories` empty = all categories; `EffectiveCategories()` resolves this
- `--dry-run` MUST NOT execute any modifying Docker command — only `docker system df --format {{json .}}`
- New public types (`CleanupCategory`, `CleanupOptions`, `CleanupResult`, `DiskUsage`) and the two new interface methods live in `internal/runtime`; `stubManager` and all existing `runtime.Manager` mocks (incl. `internal/cli/root_test.go:69` `mockRTForDeploy`) MUST still compile
- Category naming (singular, lowercase): `containers`, `images`, `volumes`, `networks`, `build-cache`
- CLI flag names: `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--dry-run`
- No new external dependencies; no changes to `.tengiz.yaml` schema
- Go 1.26; verify every task with `go test ./... -v -count=1` and `go vet ./...`
- Feature branch rule (AGENTS.md): create `feat/docker-cleanup` before starting
- Docs rule (AGENTS.md): update `README.md` CLI reference and `docs/FUTURES_FEATURES.md` status in the final task

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `Cleanup`/`DiskUsage` to `Manager` interface + `stubManager` implementations |
| `internal/runtime/cleanup.go` | Cleanup types, `dockerRuntime.Cleanup`/`DiskUsage`, docker-exec prune commands, pure parsing helpers |
| `internal/runtime/cleanup_test.go` | Unit tests for stub implementations, pure parsing helpers, and generated prune args |
| `internal/cli/cleanup.go` | New `cleanupCmd` Cobra command + pure output-formatting helpers |
| `internal/cli/root.go` | Register `cleanupCmd` + its flags in `init()` |
| `internal/cli/cleanup_test.go` | Tests: command registration, flags, `writeCleanupResults`/`writeDiskUsage`/`formatSize` output |
| `README.md` | New `### \`tengiz cleanup\`` section in CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (and P1 #56) as ✅ Implemented, add Implemented Features row |

---

### Task 1: Cleanup types + Manager interface + stub implementations

**Files:**
- Modify: `internal/runtime/cleanup.go` — add cleanup types (`CleanupCategory`, `AllCleanupCategories`, `CleanupOptions`, `CleanupResult`, `DiskUsage`) and update imports
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` + `DiskUsage` to `Manager` interface
- Modify: `internal/runtime/runtime.go:113-119` — add both methods to `stubManager`
- Modify: `internal/proxy/proxy_test.go:33-34` — add both methods to the local `mockRuntime`
- Modify: `internal/idle/idle_test.go:32-33` — add both methods to the local `mockRuntime`
- Modify: `internal/cli/root_test.go:98-99` — add both methods to `mockRTForDeploy`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type CleanupCategory string` with constants `CleanupContainers`, `CleanupImages`, `CleanupVolumes`, `CleanupNetworks`, `CleanupBuildCache`
  - `var AllCleanupCategories = []CleanupCategory{...}` (all five, in the order listed)
  - `type CleanupOptions struct { Categories []CleanupCategory; DryRun bool }`
  - `func (o CleanupOptions) EffectiveCategories() []CleanupCategory`
  - `type CleanupResult struct { Category CleanupCategory; RemovedCount int64; ReclaimedBytes int64; Err error }`
  - `type DiskUsage struct { ContainersCount, ContainersBytes, ImagesCount, ImagesBytes, VolumesCount, VolumesBytes, BuildCacheCount, BuildCacheBytes int64 }`
  - `Manager.Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupResult, error)`
  - `Manager.DiskUsage(ctx context.Context) (DiskUsage, error)`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-cleanup
```

- [ ] **Step 2: Write the failing tests**

```go
// internal/runtime/cleanup_test.go (append to the existing file)
func TestStubCleanupReturnsEmpty(t *testing.T) {
	m := NewStub()
	results, err := m.Cleanup(context.Background(), CleanupOptions{Categories: AllCleanupCategories})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestStubDiskUsageZero(t *testing.T) {
	m := NewStub()
	du, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if du != (DiskUsage{}) {
		t.Fatalf("expected zero DiskUsage, got %+v", du)
	}
}

func TestEffectiveCategories(t *testing.T) {
	if got := (CleanupOptions{}).EffectiveCategories(); len(got) != len(AllCleanupCategories) {
		t.Fatalf("empty options: expected %d categories, got %d", len(AllCleanupCategories), len(got))
	}
	for i, want := range AllCleanupCategories {
		if got[i] != want {
			t.Errorf("EffectiveCategories()[%d] = %q, want %q", i, got[i], want)
		}
	}
	sel := CleanupOptions{Categories: []CleanupCategory{CleanupVolumes}}
	if got := sel.EffectiveCategories(); len(got) != 1 || got[0] != CleanupVolumes {
		t.Fatalf("expected [volumes], got %v", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestStubCleanup|TestStubDiskUsage|TestEffectiveCategories' -v -count=1`
Expected: FAIL — compile error `m.Cleanup undefined` / `m.DiskUsage undefined` (interface has no such methods).

- [ ] **Step 4: Add cleanup types to `internal/runtime/cleanup.go`**

Replace the import block (lines 3-10) with:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

type CleanupCategory string

const (
	CleanupContainers CleanupCategory = "containers"
	CleanupImages     CleanupCategory = "images"
	CleanupVolumes    CleanupCategory = "volumes"
	CleanupNetworks   CleanupCategory = "networks"
	CleanupBuildCache CleanupCategory = "build-cache"
)

var AllCleanupCategories = []CleanupCategory{
	CleanupContainers,
	CleanupImages,
	CleanupVolumes,
	CleanupNetworks,
	CleanupBuildCache,
}

type CleanupOptions struct {
	Categories []CleanupCategory
	DryRun     bool
}

func (o CleanupOptions) EffectiveCategories() []CleanupCategory {
	if len(o.Categories) == 0 {
		return AllCleanupCategories
	}
	return o.Categories
}

type CleanupResult struct {
	Category       CleanupCategory
	RemovedCount   int64
	ReclaimedBytes int64
	Err            error
}

type DiskUsage struct {
	ContainersCount int64
	ContainersBytes int64
	ImagesCount     int64
	ImagesBytes     int64
	VolumesCount    int64
	VolumesBytes    int64
	BuildCacheCount int64
	BuildCacheBytes int64
}
```

(Leave the existing `RemoveImage` and `KeepLastNImages` functions untouched.)

- [ ] **Step 5: Extend the `Manager` interface in `internal/runtime/runtime.go`**

After the `KeepLastNImages(ctx context.Context, appName string, n int) error` line (line 36), add:

```go
	Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupResult, error)
	DiskUsage(ctx context.Context) (DiskUsage, error)
```

- [ ] **Step 6: Implement both methods on `stubManager`**

After the `KeepLastNImages` stub (line 117-119), add:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupResult, error) {
	return nil, nil
}

func (m *stubManager) DiskUsage(ctx context.Context) (DiskUsage, error) {
	return DiskUsage{}, nil
}
```

- [ ] **Step 8: Update the three existing `runtime.Manager` test mocks**

Adding methods to the `Manager` interface breaks compilation of every test mock until they implement the new methods. Add both methods to each mock.

In `internal/proxy/proxy_test.go`, after the `KeepLastNImages` mock (line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) ([]runtime.CleanupResult, error) { return nil, nil }
func (m *mockRuntime) DiskUsage(ctx context.Context) (runtime.DiskUsage, error) { return runtime.DiskUsage{}, nil }
```

In `internal/idle/idle_test.go`, after the `KeepLastNImages` mock (line 33):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) ([]runtime.CleanupResult, error) { return nil, nil }
func (m *mockRuntime) DiskUsage(ctx context.Context) (runtime.DiskUsage, error) { return runtime.DiskUsage{}, nil }
```

In `internal/cli/root_test.go`, after the `KeepLastNImages` mock (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) ([]runtime.CleanupResult, error) { return nil, nil }
func (m *mockRTForDeploy) DiskUsage(ctx context.Context) (runtime.DiskUsage, error) { return runtime.DiskUsage{}, nil }
```

- [ ] **Step 9: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestStubCleanup|TestStubDiskUsage|TestEffectiveCategories' -v -count=1`
Expected: PASS (all three).

- [ ] **Step 10: Full test + vet sweep**

Run: `go test ./... -count=1 && go vet ./...`
Expected: PASS, no vet findings. (Existing `TestStubRemoveImage`/`TestStubKeepLastNImages` and the proxy/idle/cli test suites still pass because all mocks now satisfy `Manager`.)

- [ ] **Step 11: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git add internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup and DiskUsage to Manager interface"
```

---

### Task 2: Docker-exec prune implementations + pure parsing helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.Cleanup`, `cleanupOne`, prune-arg builders, and pure parse helpers
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, `CleanupCategory` constants, `AllCleanupCategories`, `EffectiveCategories()` (Task 1)
- Produces:
  - `func containerPruneArgs() []string` → `["container","prune","-f","--filter","label!=tengiz-app"]`
  - `func imagePruneArgs() []string` → `["image","prune","-f"]`
  - `func volumePruneArgs() []string` → `["volume","prune","-f"]`
  - `func networkPruneArgs() []string` → `["network","prune","-f"]`
  - `func buildCachePruneArgs() []string` → `["builder","prune","-f"]`
  - `func parseReclaimedSpace(text string) int64` — bytes from `Total reclaimed space: 12.3MB` (b/kb/mb/gb/tb, case-insensitive, 1 KB = 1024 bytes)
  - `func parseDeletedCount(text, kind string) int64` — counts ID lines under `Deleted <kind>:` header
  - `func parseBuildCacheCount(text string) int64` — counts `Removed build cache:` lines
  - `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupResult, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go (append)
func TestContainerPruneArgs(t *testing.T) {
	got := strings.Join(containerPruneArgs(), " ")
	want := "container prune -f --filter label!=tengiz-app"
	if got != want {
		t.Fatalf("containerPruneArgs() = %q, want %q", got, want)
	}
}

func TestImagePruneArgs(t *testing.T) {
	got := strings.Join(imagePruneArgs(), " ")
	if got != "image prune -f" {
		t.Fatalf("imagePruneArgs() = %q, want %q", got, "image prune -f")
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"Total reclaimed space: 0B", 0},
		{"Total reclaimed space: 12.5kB", 12800},   // 12.5 * 1024
		{"Total reclaimed space: 2.5MB", 2621440},  // 2.5 * 1024 * 1024
		{"Total reclaimed space: 1GB", 1073741824}, // 1 * 1024^3
		{"no output here", 0},
		{"Deleted Containers:\nfoo\n\nTotal reclaimed space: 0B", 0},
	}
	for _, tc := range tests {
		if got := parseReclaimedSpace(tc.in); got != tc.want {
			t.Errorf("parseReclaimedSpace(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseDeletedCount(t *testing.T) {
	in := "Deleted Containers:\naaa\nbbb\n\nTotal reclaimed space: 10B"
	if got := parseDeletedCount(in, "Containers"); got != 2 {
		t.Errorf("parseDeletedCount Containers = %d, want 2", got)
	}
	if got := parseDeletedCount(in, "Images"); got != 0 {
		t.Errorf("parseDeletedCount Images = %d, want 0", got)
	}
	if got := parseDeletedCount("Deleted Images:\nsha256:abc\n", "Images"); got != 1 {
		t.Errorf("parseDeletedCount Images single = %d, want 1", got)
	}
}

func TestParseBuildCacheCount(t *testing.T) {
	in := "Removed build cache: 2EOGabc\n\ntmp build cache removed\n\nTotal reclaimed space: 5MB"
	if got := parseBuildCacheCount(in); got != 1 {
		t.Errorf("parseBuildCacheCount = %d, want 1", got)
	}
}
```

`clea`nup_test.go needs the `strings` import. Update its import block to:

```go
import (
	"context"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestContainerPruneArgs|TestImagePruneArgs|TestParseReclaimedSpace|TestParseDeletedCount|TestParseBuildCacheCount' -v -count=1`
Expected: FAIL — compile error `undefined: containerPruneArgs`, `undefined: parseReclaimedSpace`, etc.

- [ ] **Step 3: Add the prune command implementations and pure helpers to `internal/runtime/cleanup.go`**

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupResult, error) {
	var results []CleanupResult
	for _, cat := range opts.EffectiveCategories() {
		results = append(results, r.cleanupOne(ctx, cat, opts.DryRun))
	}
	return results, nil
}

func (r *dockerRuntime) cleanupOne(ctx context.Context, cat CleanupCategory, dryRun bool) CleanupResult {
	res := CleanupResult{Category: cat}
	if dryRun {
		return res
	}
	var args []string
	switch cat {
	case CleanupContainers:
		args = containerPruneArgs()
	case CleanupImages:
		args = imagePruneArgs()
	case CleanupVolumes:
		args = volumePruneArgs()
	case CleanupNetworks:
		args = networkPruneArgs()
	case CleanupBuildCache:
		args = buildCachePruneArgs()
	default:
		res.Err = fmt.Errorf("unknown cleanup category %q", cat)
		return res
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		res.Err = fmt.Errorf("docker %s: %w\n%s", cat, err, string(out))
		return res
	}
	text := string(out)
	res.ReclaimedBytes = parseReclaimedSpace(text)
	switch cat {
	case CleanupContainers:
		res.RemovedCount = parseDeletedCount(text, "Containers")
	case CleanupImages:
		res.RemovedCount = parseDeletedCount(text, "Images")
	case CleanupVolumes:
		res.RemovedCount = parseDeletedCount(text, "Volumes")
	case CleanupNetworks:
		res.RemovedCount = parseDeletedCount(text, "Networks")
	case CleanupBuildCache:
		res.RemovedCount = parseBuildCacheCount(text)
	}
	return res
}

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func imagePruneArgs() []string {
	return []string{"image", "prune", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

var reclaimedSpaceRe = regexp.MustCompile(`(?i)Total reclaimed space:\s*([0-9.]+)\s*([kmgt]?b)`)

func parseReclaimedSpace(text string) int64 {
	m := reclaimedSpaceRe.FindStringSubmatch(text)
	if len(m) != 3 {
		return 0
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	return int64(n * unitMultiplier(m[2]))
}

func unitMultiplier(unit string) float64 {
	switch strings.ToLower(unit) {
	case "kb":
		return 1 << 10
	case "mb":
		return 1 << 20
	case "gb":
		return 1 << 30
	case "tb":
		return 1 << 40
	default:
		return 1
	}
}

func parseDeletedCount(text, kind string) int64 {
	header := "Deleted " + kind + ":"
	lines := strings.Split(text, "\n")
	for idx, line := range lines {
		if strings.TrimSpace(line) != header {
			continue
		}
		var count int64
		for _, idLine := range lines[idx+1:] {
			idLine = strings.TrimSpace(idLine)
			if idLine == "" || strings.HasPrefix(strings.ToLower(idLine), "total reclaimed") {
				break
			}
			count++
		}
		return count
	}
	return 0
}

func parseBuildCacheCount(text string) int64 {
	var count int64
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Removed build cache:") {
			count++
		}
	}
	return count
}
```

Update the `internal/runtime/cleanup.go` import block to:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestContainerPruneArgs|TestImagePruneArgs|TestParseReclaimedSpace|TestParseDeletedCount|TestParseBuildCacheCount' -v -count=1`
Expected: PASS (all five).

- [ ] **Step 5: Full test + vet sweep**

Run: `go test ./internal/runtime/... -count=1 && go vet ./internal/runtime/...`
Expected: PASS, no vet findings.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker prune for containers/images/volumes/networks/build-cache"
```

---

### Task 3: `DiskUsage` via `docker system df`

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.DiskUsage` and `parseDiskUsage`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `DiskUsage` (Task 1)
- Produces: `func parseDiskUsage(data []byte) (DiskUsage, error)` — parses `docker system df --format {{json .}}` JSON Lines output; unknown/missing arrays are treated as empty.

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go (append)
func TestParseDiskUsage(t *testing.T) {
	data := []byte(`{"Images":[{"TotalCount":12,"TotalSize":300,"Reclaimable":true,"ReclaimableBytes":153600,"Time":0}],"Containers":[{"TotalCount":1,"TotalSize":5,"Reclaimable":true,"ReclaimableBytes":0,"Time":0}],"Volumes":[],"BuildCache":[{"TotalCount":2,"TotalSize":12,"Reclaimable":true,"ReclaimableBytes":10000,"Time":0}]}`)
	du, err := parseDiskUsage(data)
	if err != nil {
		t.Fatalf("parseDiskUsage() error = %v", err)
	}
	if du.ContainersCount != 1 || du.ContainersBytes != 0 {
		t.Errorf("containers = %d/%d, want 1/0", du.ContainersCount, du.ContainersBytes)
	}
	if du.ImagesCount != 12 || du.ImagesBytes != 153600 {
		t.Errorf("images = %d/%d, want 12/153600", du.ImagesCount, du.ImagesBytes)
	}
	if du.VolumesCount != 0 || du.VolumesBytes != 0 {
		t.Errorf("volumes = %d/%d, want 0/0", du.VolumesCount, du.VolumesBytes)
	}
	if du.BuildCacheCount != 2 || du.BuildCacheBytes != 10000 {
		t.Errorf("build cache = %d/%d, want 2/10000", du.BuildCacheCount, du.BuildCacheBytes)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestParseDiskUsage -v -count=1`
Expected: FAIL — compile error `undefined: parseDiskUsage`.

- [ ] **Step 3: Implement `DiskUsage` + `parseDiskUsage`**

```go
func (r *dockerRuntime) DiskUsage(ctx context.Context) (DiskUsage, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return DiskUsage{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseDiskUsage(out)
}

type dfEntry struct {
	TotalCount       int64 `json:"TotalCount"`
	ReclaimableBytes int64 `json:"ReclaimableBytes"`
}

type systemDF struct {
	Images     []dfEntry `json:"Images"`
	Containers []dfEntry `json:"Containers"`
	Volumes    []dfEntry `json:"Volumes"`
	BuildCache []dfEntry `json:"BuildCache"`
}

func parseDiskUsage(data []byte) (DiskUsage, error) {
	var df systemDF
	if err := json.Unmarshal(data, &df); err != nil {
		return DiskUsage{}, err
	}
	var du DiskUsage
	for _, e := range df.Containers {
		du.ContainersCount += e.TotalCount
		du.ContainersBytes += e.ReclaimableBytes
	}
	for _, e := range df.Images {
		du.ImagesCount += e.TotalCount
		du.ImagesBytes += e.ReclaimableBytes
	}
	for _, e := range df.Volumes {
		du.VolumesCount += e.TotalCount
		du.VolumesBytes += e.ReclaimableBytes
	}
	for _, e := range df.BuildCache {
		du.BuildCacheCount += e.TotalCount
		du.BuildCacheBytes += e.ReclaimableBytes
	}
	return du, nil
}
```

Update the `internal/runtime/cleanup.go` import block to add `"encoding/json"`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestParseDiskUsage -v -count=1`
Expected: PASS.

- [ ] **Step 5: Full test + vet sweep**

Run: `go test ./internal/runtime/... -count=1 && go vet ./internal/runtime/...`
Expected: PASS, no vet findings.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add DiskUsage via docker system df"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:32-89` — register `cleanupCmd` + flags in `init()`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager` (`NewDocker`, `Cleanup`, `DiskUsage`), `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.DiskUsage`, `runtime.AllCleanupCategories` (Tasks 1-2)
- Produces:
  - `var cleanupCmd *cobra.Command` (`Use: "cleanup"`)
  - `func writeCleanupResults(w io.Writer, results []runtime.CleanupResult)`
  - `func writeDiskUsage(w io.Writer, du runtime.DiskUsage)`
  - `func formatSize(b int64) string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go (new file)
package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, f := range []string{"containers", "images", "volumes", "networks", "build-cache", "dry-run"} {
		if cleanupCmd.Flags().Lookup(f) == nil {
			t.Errorf("missing --%s flag on cleanup command", f)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1536, "1.5 KiB"},
		{5 << 20, "5.0 MiB"},
		{3 << 30, "3.0 GiB"},
	}
	for _, tc := range tests {
		if got := formatSize(tc.in); got != tc.want {
			t.Errorf("formatSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWriteCleanupResults(t *testing.T) {
	var buf bytes.Buffer
	results := []runtime.CleanupResult{
		{Category: runtime.CleanupContainers, RemovedCount: 3, ReclaimedBytes: 1536},
		{Category: runtime.CleanupVolumes, RemovedCount: 0, ReclaimedBytes: 0, Err: errors.New("boom")},
	}
	writeCleanupResults(&buf, results)
	out := buf.String()
	if !strings.Contains(out, "containers") {
		t.Errorf("output missing containers row: %q", out)
	}
	if !strings.Contains(out, "1.5 KiB") {
		t.Errorf("output missing reclaimed size: %q", out)
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("output missing error detail: %q", out)
	}
	if !strings.Contains(out, "total reclaimed") {
		t.Errorf("output missing total: %q", out)
	}
}

func TestWriteCleanupResultsEmpty(t *testing.T) {
	var buf bytes.Buffer
	writeCleanupResults(&buf, nil)
	if !strings.Contains(buf.String(), "nothing to clean") {
		t.Errorf("empty results should print 'nothing to clean', got %q", buf.String())
	}
}

func TestWriteDiskUsage(t *testing.T) {
	var buf bytes.Buffer
	writeDiskUsage(&buf, runtime.DiskUsage{ImagesCount: 2, ImagesBytes: 1 << 20, ContainersCount: 4})
	out := buf.String()
	if !strings.Contains(out, "images") || !strings.Contains(out, "containers") {
		t.Errorf("output missing category rows: %q", out)
	}
	if !strings.Contains(out, "1.0 MiB") {
		t.Errorf("output missing size: %q", out)
	}
	if !strings.Contains(out, "reclaimable total") {
		t.Errorf("output missing total: %q", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestFormatSize|TestWriteCleanupResults|TestWriteDiskUsage' -v -count=1`
Expected: FAIL — `cleanupCmd` undefined / command not registered.

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Prune unused Docker resources on the host and reclaim disk space.

By default every category is pruned. Tengiz-managed containers (labeled
tengiz-app=...) are always protected and never removed. Tagged app images
(tengiz-apps/...) belong to the deploy/rollback history and are preserved;
only dangling images are pruned.

Examples:
  tengiz cleanup                  # prune all categories
  tengiz cleanup --containers     # prune only stopped non-Tengiz containers
  tengiz cleanup --dry-run        # show reclaimable totals, change nothing`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		var cats []runtime.CleanupCategory
		if containers {
			cats = append(cats, runtime.CleanupContainers)
		}
		if images {
			cats = append(cats, runtime.CleanupImages)
		}
		if volumes {
			cats = append(cats, runtime.CleanupVolumes)
		}
		if networks {
			cats = append(cats, runtime.CleanupNetworks)
		}
		if buildCache {
			cats = append(cats, runtime.CleanupBuildCache)
		}
		if len(cats) == 0 {
			cats = runtime.AllCleanupCategories
		}

		if dryRun {
			return previewCleanup(cmd.Context(), rt, os.Stdout)
		}

		results, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{Categories: cats})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		writeCleanupResults(os.Stdout, results)
		return nil
	},
}

func previewCleanup(ctx context.Context, rt runtime.Manager, w io.Writer) error {
	du, err := rt.DiskUsage(ctx)
	if err != nil {
		fmt.Fprintln(w, "[tengiz] warning: could not read Docker disk usage:", err)
		return nil
	}
	writeDiskUsage(w, du)
	fmt.Fprintln(w, "[tengiz] dry run complete (nothing was removed)")
	return nil
}

func writeCleanupResults(w io.Writer, results []runtime.CleanupResult) {
	if len(results) == 0 {
		fmt.Fprintln(w, "  nothing to clean")
		return
	}
	var total int64
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(w, "  %-12s error: %v\n", r.Category, r.Err)
			continue
		}
		fmt.Fprintf(w, "  %-12s removed %2d  %s\n", r.Category, r.RemovedCount, formatSize(r.ReclaimedBytes))
		total += r.ReclaimedBytes
	}
	fmt.Fprintf(w, "[tengiz] total reclaimed: %s\n", formatSize(total))
}

func writeDiskUsage(w io.Writer, du runtime.DiskUsage) {
	total := du.ContainersBytes + du.ImagesBytes + du.VolumesBytes + du.BuildCacheBytes
	fmt.Fprintln(w, "Docker disk usage (reclaimable):")
	fmt.Fprintf(w, "  %-12s %8d objects  %s\n", "containers", du.ContainersCount, formatSize(du.ContainersBytes))
	fmt.Fprintf(w, "  %-12s %8d objects  %s\n", "images", du.ImagesCount, formatSize(du.ImagesBytes))
	fmt.Fprintf(w, "  %-12s %8d objects  %s\n", "volumes", du.VolumesCount, formatSize(du.VolumesBytes))
	fmt.Fprintf(w, "  %-12s %8d objects  %s\n", "build cache", du.BuildCacheCount, formatSize(du.BuildCacheBytes))
	fmt.Fprintf(w, "[tengiz] reclaimable total: %s\n", formatSize(total))
}

func formatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
```

- [ ] **Step 4: Register the command and flags in `internal/cli/root.go`**

In `init()` (around line 70, after `rootCmd.AddCommand(notificationCmd)`), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused named volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune unused build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without changing anything")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestFormatSize|TestWriteCleanupResults|TestWriteDiskUsage' -v -count=1`
Expected: PASS (all).

- [ ] **Step 6: Full test + vet sweep**

Run: `go test ./... -count=1 && go vet ./...`
Expected: PASS, no vet findings. (The three existing test mocks were already extended in Task 1, so nothing else needs updating.)

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Documentation + feature tracking

**Files:**
- Modify: `README.md` — new `### \`tengiz cleanup\`` section in CLI Reference (after the `tengiz rollback` section, ~line 236)
- Modify: `docs/FUTURES_FEATURES.md` — mark P0 feature #6 and P1 feature #56 as ✅, add Implemented Features rows

(No Go code changes in this task.)

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

Immediately after the `### \`tengiz rollback <app>\`` section (ends at the `app` argument row, ~line 236), insert:

```markdown
### `tengiz cleanup`

Prune unused Docker resources on the host and reclaim disk space. By default every category is pruned.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling images |
| `--volumes` | Prune unused named volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune unused build cache |
| `--dry-run` | Show reclaimable totals without removing anything |

When no category flag is given, all five categories are pruned. Tengiz-managed containers (labeled `tengiz-app=...`) are never removed. Tagged app images

 (`tengiz-apps/...`) belong to the deploy/rollback history and are preserved — only dangling images are pruned. Old versioned images are already cleaned automatically during deploy via `KeepLastNImages`.

Examples:
```
tengiz cleanup                  # prune all categories
tengiz cleanup --containers     # prune only stopped non-Tengiz containers
tengiz cleanup --dry-run        # preview without changing anything
```
```

- [ ] **Step 2: Update `docs/FUTURES_FEATURES.md` — P0 table row #6**

Change line 19 from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. Implemented (2026-08-09). |
```

- [ ] **Step 3: Update `docs/FUTURES_FEATURES.md` — P1 table row #56**

Change line 74 (the P1 row) from:

```markdown
| 56 | **Granular Docker Prune Operations** ⬜ | Orta | Düşük | Mükemmel | Per-category prune: containers/networks/images/volumes/buildx cache. Surgical disk management. |
```

to:

```markdown
| 56 | **Granular Docker Prune Operations** ✅ | Orta | Düşük | Mükemmel | Per-category prune: containers/networks/images/volumes/buildx cache. Surgical disk management. Implemented (2026-08-09). |
```

- [ ] **Step 4: Add rows to the Implemented Features table**

In the `### ✅ Implemented Features (Not Pending)` table (after the "Webhook ile Otomatik Deploy" row, ~line 253), append:

```markdown
| — | **Docker Housekeeping (tengiz cleanup)** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-09) |
| — | **Granular Docker Prune Operations** | Orta | Düşük | Mükemmel | ✅ Implemented (2026-08-09) |
```

- [ ] **Step 5: Verify docs mention only intended changes**

Run: `git diff -- README.md docs/FUTURES_FEATURES.md`
Expected: diff contains only the new `cleanup` section and the feature-status edits above.

- [ ] **Step 6: Full verification sweep**

Run: `go build -o /tmp/tengiz . && go test ./... -count=1 && go vet ./...`
Expected: build succeeds, all tests pass, no vet findings.

- [ ] **Step 7: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark housekeeping features implemented"
```

---