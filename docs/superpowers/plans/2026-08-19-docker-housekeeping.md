# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command (plus optional periodic `--interval` mode) that removes unused Docker resources — stopped containers, unused images, anonymous volumes, unused networks, and build cache — while always protecting Tengiz-managed containers and images.

**Architecture:** A label-based cleanup built on Docker's per-type `prune` subcommands. Tengiz marks every image it builds with the label `tengiz-managed=true`; every container it creates already carries the `tengiz-app` label. Cleanup runs `docker container prune --filter label!=tengiz-app` and `docker image prune -a --filter label!=tengiz-managed`, so rollback images and scale-to-zero (cold-start) containers survive. Containers are pruned before images so images referenced only by deleted stopped containers become eligible. A `Prune(ctx, opts) (PruneResult, error)` method on `runtime.Manager` centralizes exec-based docker calls; pure helper functions (`buildPruneArgs`, `extractReclaimedSpace`) carry the testable logic.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface + `dockerRuntime` exec implementation, existing `builder.Builder`. No new external dependencies. Docker CLI must be installed (already a Tengiz requirement).

## Global Constraints

- Tengiz-managed containers (label `tengiz-app`) are always protected from pruning
- Tengiz-built images (label `tengiz-managed`) are always protected from pruning; rollback and cold-start behavior must never be broken
- `docker image prune` supports ONLY the `until` and `label` filters — `reference` is NOT supported, so image protection MUST use the `label!=tengiz-managed` filter
- Cleanup order is fixed: containers → images → volumes → networks → build-cache (containers first so their now-unreferenced images become pruneable)
- Default categories when no flags are passed: `containers` + `images`; `--all` enables all five categories; category flags are additive to the default set
- `--dry-run` must perform no destructive operations and must work without Docker present
- Confirmation is required unless `--yes` is passed; non-interactive terminals (pipe / file stdin) always decline the prompt, so they effectively require `--yes`
- `docker volume prune` only removes unused anonymous volumes by default — named volumes are never auto-removed (data safety)
- Known Docker 29.x regression: negative `label!=` filters on `docker image prune` may no-op (deletes nothing — a SAFE failure). The fix is merged upstream. Verify image pruning actually removes an image in the integration test; if it no-ops on the target Docker version, the feature degrades gracefully (nothing is deleted) — document rather than work around.
- The `runtime.Manager` interface change adds two methods (`Prune`, `DiskUsage`); every mock implementing the interface MUST be updated in the same task or the package will not compile
- All existing tests must continue to pass unmodified except for the mock additions
- No new external dependencies

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | Add `CleanupCategory` consts, `ManagedImageLabel`, `PruneOptions`, `PruneResult` |
| `internal/runtime/prune.go` | NEW — pure arg-builders + output parsers + `dockerRuntime.Prune`/`DiskUsage` |
| `internal/runtime/prune_test.go` | NEW — unit tests for the pure helpers and the no-categories error path |
| `internal/runtime/runtime.go` | Add `Prune`/`DiskUsage` to `Manager` interface + `stubManager` methods |
| `internal/builder/builder.go` | Tag every built image with `tengiz-managed=true` (docker build + nixpacks) |
| `internal/cli/root.go` | Add `cleanupCmd`, `buildCleanupOptions`, `confirmCleanup`, `confirmWithReader`, `optsStrings`, `runCleanupOnce` |
| `internal/cli/root_test.go` | Command registration/flag tests, option-mapping tests, dry-run/cancel/interval tests; add new methods to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Prune`/`DiskUsage` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Prune`/`DiskUsage` to `mockRuntime` |
| `internal/builder/builder_test.go` | Tests that build args include the managed label |
| `internal/types/types_test.go` | Tests for the new cleanup types |
| `README.md` | Document the `tengiz cleanup` command |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI list; note `Prune`/`DiskUsage` in runtime row |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as implemented |

---

### Task 1: Add the cleanup data model to `types`

**Files:**
- Modify: `internal/types/types.go` — append the new types after `AppEntry` (end of file)
- Test: `internal/types/types_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `types.CleanupCategory` with consts `CleanupContainers`, `CleanupImages`, `CleanupVolumes`, `CleanupNetworks`, `CleanupBuildCache`; `types.ManagedImageLabel string` (`"tengiz-managed"`); `types.PruneOptions{Containers, Images, Volumes, Networks, BuildCache bool}`; `types.PruneResult{Categories []CleanupCategory, TotalReclaimed string, Detail []string}`

- [ ] **Step 1: Write the failing test**

Add to `internal/types/types_test.go`:

```go
func TestCleanupCategoryConstants(t *testing.T) {
	want := map[CleanupCategory]string{
		CleanupContainers: "containers",
		CleanupImages:     "images",
		CleanupVolumes:    "volumes",
		CleanupNetworks:   "networks",
		CleanupBuildCache: "build-cache",
	}
	for cat, str := range want {
		if string(cat) != str {
			t.Errorf("CleanupCategory %q: expected %q", cat, str)
		}
	}
	if ManagedImageLabel != "tengiz-managed" {
		t.Errorf("ManagedImageLabel = %q, want %q", ManagedImageLabel, "tengiz-managed")
	}
}

func TestPruneOptionsZeroValueAllFalse(t *testing.T) {
	var opts PruneOptions
	if opts.Containers || opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Error("zero-value PruneOptions should have all categories false")
	}
}

func TestPruneResultJSONRoundTrip(t *testing.T) {
	pr := PruneResult{
		Categories:     []CleanupCategory{CleanupContainers, CleanupImages},
		TotalReclaimed: "1.2GB",
		Detail:         []string{"Deleted Containers:", "abc123"},
	}
	data, err := json.Marshal(pr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded PruneResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Categories) != 2 || decoded.Categories[0] != CleanupContainers {
		t.Fatalf("categories mismatch: %v", decoded.Categories)
	}
	if decoded.TotalReclaimed != "1.2GB" {
		t.Fatalf("total reclaimed mismatch: %q", decoded.TotalReclaimed)
	}
	if len(decoded.Detail) != 2 || decoded.Detail[0] != "Deleted Containers:" {
		t.Fatalf("detail mismatch: %v", decoded.Detail)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/ -v -count=1`
Expected: FAIL — compile error `undefined: CleanupContainers` (and the rest)

- [ ] **Step 3: Write the minimal implementation**

Append to `internal/types/types.go`:

```go
type CleanupCategory string

const (
	CleanupContainers CleanupCategory = "containers"
	CleanupImages     CleanupCategory = "images"
	CleanupVolumes    CleanupCategory = "volumes"
	CleanupNetworks   CleanupCategory = "networks"
	CleanupBuildCache CleanupCategory = "build-cache"
)

const ManagedImageLabel = "tengiz-managed"

type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type PruneResult struct {
	Categories     []CleanupCategory `json:"categories"`
	TotalReclaimed string            `json:"total_reclaimed"`
	Detail         []string          `json:"detail"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types/ -v -count=1`
Expected: PASS (all tests including pre-existing ones)

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat(types): add cleanup categories and prune result types"
```

---

### Task 2: Runtime pure helpers for cleanup

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `types.CleanupCategory`, `types.PruneOptions`, `types.PruneResult`, `types.ManagedImageLabel` (from Task 1); `labelKey` (existing `const labelKey = "tengiz-app"` in `internal/runtime/docker.go`)
- Produces: `buildPruneArgs(cat types.CleanupCategory) []string`, `buildDiskUsageArgs() []string`, `cleanupCategories(opts types.PruneOptions) []types.CleanupCategory`, `extractReclaimedSpace(output string) (string, bool)`, `summarizeReclaimed(spaces []string) string`, `appendDetail(detail []string, out string) []string`. These are used by Task 4's `Prune`/`DiskUsage` and tested directly here.

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"reflect"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestBuildPruneArgsContainers(t *testing.T) {
	got := buildPruneArgs(types.CleanupContainers)
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneArgs(containers) = %v, want %v", got, want)
	}
}

func TestBuildPruneArgsImages(t *testing.T) {
	got := buildPruneArgs(types.CleanupImages)
	want := []string{"image", "prune", "-f", "-a", "--filter", "label!=tengiz-managed"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneArgs(images) = %v, want %v", got, want)
	}
}

func TestBuildPruneArgsVolumesNetworksBuildCache(t *testing.T) {
	if got := buildPruneArgs(types.CleanupVolumes); !reflect.DeepEqual(got, []string{"volume", "prune", "-f"}) {
		t.Errorf("volume args = %v", got)
	}
	if got := buildPruneArgs(types.CleanupNetworks); !reflect.DeepEqual(got, []string{"network", "prune", "-f"}) {
		t.Errorf("network args = %v", got)
	}
	if got := buildPruneArgs(types.CleanupBuildCache); !reflect.DeepEqual(got, []string{"builder", "prune", "-f"}) {
		t.Errorf("build-cache args = %v", got)
	}
}

func TestBuildDiskUsageArgs(t *testing.T) {
	got := buildDiskUsageArgs()
	want := []string{"system", "df"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildDiskUsageArgs() = %v, want %v", got, want)
	}
}

func TestCleanupCategories(t *testing.T) {
	got := cleanupCategories(types.PruneOptions{Containers: true, Images: true, BuildCache: true})
	want := []types.CleanupCategory{types.CleanupContainers, types.CleanupImages, types.CleanupBuildCache}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cleanupCategories() = %v, want %v", got, want)
	}
	if got := cleanupCategories(types.PruneOptions{}); len(got) != 0 {
		t.Errorf("expected no categories for empty options, got %v", got)
	}
}

func TestExtractReclaimedSpace(t *testing.T) {
	got, ok := extractReclaimedSpace("Deleted Containers:\nabc\n\nTotal reclaimed space: 212 B\n")
	if !ok || got != "212 B" {
		t.Errorf("extractReclaimedSpace() = %q, %v; want %q, true", got, ok, "212 B")
	}
	got, ok = extractReclaimedSpace("nothing here")
	if ok || got != "" {
		t.Errorf("extractReclaimedSpace() = %q, %v; want \"\", false", got, ok)
	}
}

func TestSummarizeReclaimed(t *testing.T) {
	got := summarizeReclaimed([]string{"1.2GB", "212 B", "1.2GB"})
	if got != "1.2GB + 212 B" {
		t.Errorf("summarizeReclaimed() = %q, want %q", got, "1.2GB + 212 B")
	}
	if got := summarizeReclaimed(nil); got != "" {
		t.Errorf("summarizeReclaimed(nil) = %q, want empty", got)
	}
}

func TestAppendDetail(t *testing.T) {
	got := appendDetail(nil, "  line one  \n\nline two\n")
	want := []string{"line one", "line two"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendDetail() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestBuild|TestCleanup|TestExtract|TestSummarize|TestAppend' -v -count=1`
Expected: FAIL — compile error `undefined: buildPruneArgs` (and the other helpers)

- [ ] **Step 3: Write the minimal implementation**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

var reclaimedSpaceRe = regexp.MustCompile(`Total reclaimed space:\s*(.+)$`)

func buildPruneArgs(cat types.CleanupCategory) []string {
	switch cat {
	case types.CleanupContainers:
		return []string{"container", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey)}
	case types.CleanupImages:
		return []string{"image", "prune", "-f", "-a", "--filter", fmt.Sprintf("label!=%s", types.ManagedImageLabel)}
	case types.CleanupVolumes:
		return []string{"volume", "prune", "-f"}
	case types.CleanupNetworks:
		return []string{"network", "prune", "-f"}
	case types.CleanupBuildCache:
		return []string{"builder", "prune", "-f"}
	default:
		return nil
	}
}

func buildDiskUsageArgs() []string {
	return []string{"system", "df"}
}

func cleanupCategories(opts types.PruneOptions) []types.CleanupCategory {
	var cats []types.CleanupCategory
	if opts.Containers {
		cats = append(cats, types.CleanupContainers)
	}
	if opts.Images {
		cats = append(cats, types.CleanupImages)
	}
	if opts.Volumes {
		cats = append(cats, types.CleanupVolumes)
	}
	if opts.Networks {
		cats = append(cats, types.CleanupNetworks)
	}
	if opts.BuildCache {
		cats = append(cats, types.CleanupBuildCache)
	}
	return cats
}

func extractReclaimedSpace(output string) (string, bool) {
	m := reclaimedSpaceRe.FindStringSubmatch(strings.TrimSpace(output))
	if len(m) < 2 {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

func summarizeReclaimed(spaces []string) string {
	seen := make(map[string]bool)
	var parts []string
	for _, s := range spaces {
		if s != "" && !seen[s] {
			seen[s] = true
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " + ")
}

func appendDetail(detail []string, out string) []string {
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			detail = append(detail, l)
		}
	}
	return detail
}
```

Note: `Prune` and `DiskUsage` methods on `*dockerRuntime` are added in Task 4; that task also adds the `context` and `os/exec` imports this file needs.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestBuild|TestCleanup|TestExtract|TestSummarize|TestAppend' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add cleanup arg builders and output parsers"
```

---

### Task 3: Label all Tengiz-built images

**Files:**
- Modify: `internal/builder/builder.go:69-71` (docker build args) and `:139` (nixpacks args)
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.ManagedImageLabel` (Task 1)
- Produces: `dockerBuildArgs(secretArgs []string, tag, dir string) []string`, `nixpacksBuildArgs(tag, dir string, extra []string) []string` — both MUST include `--label tengiz-managed=true`

- [ ] **Step 1: Write the failing test**

Add to `internal/builder/builder_test.go`:

```go
func TestDockerBuildArgsIncludeManagedLabel(t *testing.T) {
	got := dockerBuildArgs([]string{"--secret", "id=foo,src=/tmp/foo"}, "tengiz-apps/testapp:v1", ".")
	found := false
	for _, a := range got {
		if strings.HasPrefix(a, types.ManagedImageLabel+"=") {
			found = true
		}
	}
	if !found {
		t.Errorf("dockerBuildArgs() missing managed label, got %v", got)
	}
	if got[len(got)-1] != "." {
		t.Errorf("expected dir to be the last arg, got %v", got)
	}
}

func TestNixpacksBuildArgsIncludeManagedLabel(t *testing.T) {
	got := nixpacksBuildArgs("tengiz-apps/testapp:v1", ".", nil)
	found := false
	for _, a := range got {
		if strings.HasPrefix(a, types.ManagedImageLabel+"=") {
			found = true
		}
	}
	if !found {
		t.Errorf("nixpacksBuildArgs() missing managed label, got %v", got)
	}
	if got[0] != "build" {
		t.Errorf("expected first arg 'build', got %v", got)
	}
}

func TestNixpacksBuildArgsExtraAppended(t *testing.T) {
	got := nixpacksBuildArgs("tag", ".", []string{"--pkgs", "curl"})
	last := got[len(got)-1]
	if last != "curl" {
		t.Errorf("expected extra args appended, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run 'TestDockerBuildArgs|TestNixpacksBuildArgs' -v -count=1`
Expected: FAIL — compile error `undefined: dockerBuildArgs`

- [ ] **Step 3: Write the minimal implementation**

Add these helper functions to `internal/builder/builder.go` (place them just above `buildWithDockerfile`):

```go
func dockerBuildArgs(secretArgs []string, tag, dir string) []string {
	args := []string{"build"}
	args = append(args, secretArgs...)
	args = append(args, "-t", tag)
	args = append(args, "--label", fmt.Sprintf("%s=true", types.ManagedImageLabel))
	args = append(args, dir)
	return args
}

func nixpacksBuildArgs(tag, dir string, extra []string) []string {
	args := []string{"build", dir, "--name", tag, "--label", fmt.Sprintf("%s=true", types.ManagedImageLabel)}
	args = append(args, extra...)
	return args
}
```

Then replace the inline arg construction in `buildWithDockerfile` (currently `builder.go:69-71`):

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

with:

```go
	args := dockerBuildArgs(b.buildSecretArgs(), tag, dir)
```

And replace the inline arg construction in `buildWithNixpacks` (currently `builder.go:139`):

```go
	args := []string{"build", dir, "--name", tag}
```

with:

```go
	args := nixpacksBuildArgs(tag, dir, nil)
```

The subsequent `if b.nixpacksCfg != nil { ... args = append(args, ...) ... }` block stays unchanged.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/ -v -count=1`
Expected: PASS (all builder tests, including pre-existing ones)

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat(builder): label built images as tengiz-managed for cleanup protection"
```

---

### Task 4: Add `Prune` and `DiskUsage` to the runtime

**Files:**
- Modify: `internal/runtime/runtime.go` (interface + stub)
- Modify: `internal/runtime/prune.go` (dockerRuntime methods)
- Modify: `internal/proxy/proxy_test.go` (mockRuntime)
- Modify: `internal/idle/idle_test.go` (mockRuntime)
- Modify: `internal/cli/root_test.go` (mockRTForDeploy)

**Interfaces:**
- Consumes: `buildPruneArgs`, `buildDiskUsageArgs`, `cleanupCategories`, `extractReclaimedSpace`, `summarizeReclaimed`, `appendDetail` (Task 2); `types.PruneOptions`, `types.PruneResult` (Task 1)
- Produces: `Manager.Prune(ctx context.Context, opts types.PruneOptions) (types.PruneResult, error)` and `Manager.DiskUsage(ctx context.Context) (string, error)` — both implemented on `*dockerRuntime` and on `stubManager`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/prune_test.go` (and add `"context"` to its imports):

```go
func TestDockerRuntimePruneNoCategories(t *testing.T) {
	r := &dockerRuntime{}
	if _, err := r.Prune(context.Background(), types.PruneOptions{}); err == nil {
		t.Error("Prune() with no categories should return an error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestDockerRuntimePruneNoCategories -v -count=1`
Expected: FAIL — compile error `(*dockerRuntime).Prune undefined` (the interface/stub changes below fix this)

- [ ] **Step 3: Write the minimal implementation**

Add to the `Manager` interface in `internal/runtime/runtime.go` (inside the interface block, after the `KeepLastNImages` line at `runtime.go:36`):

```go
	Prune(ctx context.Context, opts types.PruneOptions) (types.PruneResult, error)
	DiskUsage(ctx context.Context) (string, error)
```

Add to `stubManager` in `internal/runtime/runtime.go` (after `KeepLastNImages`, `runtime.go:117-119`):

```go
func (m *stubManager) Prune(ctx context.Context, opts types.PruneOptions) (types.PruneResult, error) {
	return types.PruneResult{}, nil
}

func (m *stubManager) DiskUsage(ctx context.Context) (string, error) {
	return "", nil
}
```

Add to `internal/runtime/prune.go` (this is where the file's `context` and `os/exec` imports are added — merge them into the import block from Task 2):

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts types.PruneOptions) (types.PruneResult, error) {
	cats := cleanupCategories(opts)
	if len(cats) == 0 {
		return types.PruneResult{}, fmt.Errorf("no cleanup categories selected")
	}

	var result types.PruneResult
	var reclaimed []string

	for _, cat := range cats {
		cmd := exec.CommandContext(ctx, "docker", buildPruneArgs(cat)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return result, fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out))
		}
		result.Categories = append(result.Categories, cat)
		result.Detail = appendDetail(result.Detail, string(out))
		if space, ok := extractReclaimedSpace(string(out)); ok {
			reclaimed = append(reclaimed, space)
		}
	}

	result.TotalReclaimed = summarizeReclaimed(reclaimed)
	return result, nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildDiskUsageArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
```

Update the three mock implementations so the package compiles:

In `internal/proxy/proxy_test.go`, add after the `KeepLastNImages` line (`proxy_test.go:34`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts types.PruneOptions) (types.PruneResult, error) { return types.PruneResult{}, nil }
func (m *mockRuntime) DiskUsage(ctx context.Context) (string, error) { return "", nil }
```

In `internal/idle/idle_test.go`, add after the `KeepLastNImages` line (`idle_test.go:33`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts types.PruneOptions) (types.PruneResult, error) { return types.PruneResult{}, nil }
func (m *mockRuntime) DiskUsage(ctx context.Context) (string, error) { return "", nil }
```

In `internal/cli/root_test.go`, add after the `KeepLastNImages` line (`root_test.go:99`):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts types.PruneOptions) (types.PruneResult, error) { return types.PruneResult{}, nil }
func (m *mockRTForDeploy) DiskUsage(ctx context.Context) (string, error) { return "", nil }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -v -count=1`
Expected: PASS across all packages (the three mock updates keep every package compiling)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune.go internal/runtime/prune_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Prune and DiskUsage to the runtime manager"
```

---

### Task 5: Add the `tengiz cleanup` command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, helpers, and registration in `init()`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker`, `Manager.DiskUsage`, `Manager.Prune` (Task 4); `types.PruneOptions`, `types.PruneResult` (Task 1)
- Produces: `cleanupCmd *cobra.Command` (Use: `cleanup`, flags `--containers --images --volumes --networks --build-cache --all --dry-run --yes/-y`); `buildCleanupOptions(cmd *cobra.Command) (types.PruneOptions, error)`; `confirmCleanup() bool`; `confirmWithReader(r io.Reader) bool`; `optsStrings(opts types.PruneOptions) []string`. Task 6 refactors the RunE body into `runCleanupOnce`.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run", "yes"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func newTestCleanupCmd() *cobra.Command {
	c := &cobra.Command{Use: "cleanup"}
	c.Flags().Bool("containers", true, "")
	c.Flags().Bool("images", true, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("build-cache", false, "")
	c.Flags().Bool("all", false, "")
	return c
}

func TestBuildCleanupOptionsDefaults(t *testing.T) {
	c := newTestCleanupCmd()
	opts, err := buildCleanupOptions(c)
	if err != nil {
		t.Fatalf("buildCleanupOptions() error = %v", err)
	}
	if !opts.Containers || !opts.Images {
		t.Error("default cleanup should include containers and images")
	}
	if opts.Volumes || opts.Networks || opts.BuildCache {
		t.Error("default cleanup should NOT include volumes/networks/build-cache")
	}
}

func TestBuildCleanupOptionsAll(t *testing.T) {
	c := newTestCleanupCmd()
	if err := c.Flags().Set("all", "true"); err != nil {
		t.Fatal(err)
	}
	opts, err := buildCleanupOptions(c)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Error("--all should enable every category")
	}
}

func TestBuildCleanupOptionsVolumesAdditive(t *testing.T) {
	c := newTestCleanupCmd()
	if err := c.Flags().Set("volumes", "true"); err != nil {
		t.Fatal(err)
	}
	opts, err := buildCleanupOptions(c)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes {
		t.Error("category flags should add to the default containers+images set")
	}
}

func TestBuildCleanupOptionsDisableContainers(t *testing.T) {
	c := newTestCleanupCmd()
	if err := c.Flags().Set("containers", "false"); err != nil {
		t.Fatal(err)
	}
	opts, err := buildCleanupOptions(c)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Containers {
		t.Error("containers should be disabled when --containers=false")
	}
	if !opts.Images {
		t.Error("images should still be enabled")
	}
}

func TestBuildCleanupOptionsNoCategoriesError(t *testing.T) {
	c := newTestCleanupCmd()
	if err := c.Flags().Set("containers", "false"); err != nil {
		t.Fatal(err)
	}
	if err := c.Flags().Set("images", "false"); err != nil {
		t.Fatal(err)
	}
	if _, err := buildCleanupOptions(c); err == nil {
		t.Error("expected error when no categories selected")
	}
}

func TestConfirmWithReader(t *testing.T) {
	if !confirmWithReader(strings.NewReader("y\n")) {
		t.Error("expected confirm for 'y'")
	}
	if !confirmWithReader(strings.NewReader("YES\n")) {
		t.Error("expected confirm for 'YES'")
	}
	if confirmWithReader(strings.NewReader("n\n")) {
		t.Error("expected decline for 'n'")
	}
	if confirmWithReader(strings.NewReader("")) {
		t.Error("expected decline on EOF")
	}
}

func TestOptsStrings(t *testing.T) {
	got := optsStrings(types.PruneOptions{Containers: true, Images: true, Volumes: true})
	want := []string{"containers", "images", "volumes"}
	if len(got) != 3 || got[0] != "containers" || got[1] != "images" || got[2] != "volumes" {
		t.Errorf("optsStrings() = %v, want %v", got, want)
	}
}

func TestCleanupDryRunWithoutDocker(t *testing.T) {
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
		if err := rootCmd.Execute(); err != nil {
			t.Errorf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(output, "dry-run") || !strings.Contains(output, "containers, images") {
		t.Errorf("unexpected dry-run output: %s", output)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestConfirmWithReader|TestOptsStrings' -v -count=1`
Expected: FAIL — compile error `undefined: cleanupCmd` / `undefined: buildCleanupOptions`

- [ ] **Step 3: Write the minimal implementation**

Add `bufio` to the imports of `internal/cli/root.go` (import block is `root.go:3-30`):

```go
import (
	"bufio"
	"context"
	...
)
```

Add the command definition and helpers. Place `cleanupCmd` and its helpers after `volumeListCmd` (after `root.go:939`) and before `rollbackCmd`:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Removes unused Docker resources to free disk space on the host.

By default removes stopped containers (Tengiz-managed containers are always
protected) and unused images (Tengiz-built images are always protected). Add
--volumes, --networks, or --build-cache to include more categories. Use --all
to clean every category.

Confirmation is required unless --yes is passed; non-interactive terminals
cannot confirm and must pass --yes. Use --dry-run to preview what would be
removed without removing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := buildCleanupOptions(cmd)
		if err != nil {
			return err
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		yes, _ := cmd.Flags().GetBool("yes")

		if dryRun {
			if rt, err := runtime.NewDocker(); err == nil {
				if usage, err := rt.DiskUsage(cmd.Context()); err == nil && strings.TrimSpace(usage) != "" {
					fmt.Println(usage)
				}
			}
			fmt.Printf("[tengiz] dry-run: would prune %s\n", strings.Join(optsStrings(opts), ", "))
			return nil
		}

		if !yes && !confirmCleanup() {
			fmt.Println("[tengiz] cleanup cancelled")
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}

		if usage, err := rt.DiskUsage(cmd.Context()); err == nil && strings.TrimSpace(usage) != "" {
			fmt.Println(usage)
		}

		result, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return err
		}

		for _, l := range result.Detail {
			fmt.Println(l)
		}
		if result.TotalReclaimed != "" {
			fmt.Printf("[tengiz] total reclaimed space: %s\n", result.TotalReclaimed)
		}
		return nil
	},
}

func buildCleanupOptions(cmd *cobra.Command) (types.PruneOptions, error) {
	all, _ := cmd.Flags().GetBool("all")
	if all {
		return types.PruneOptions{
			Containers: true,
			Images:     true,
			Volumes:    true,
			Networks:   true,
			BuildCache: true,
		}, nil
	}

	opts := types.PruneOptions{Containers: true, Images: true}
	if cmd.Flags().Changed("containers") {
		opts.Containers, _ = cmd.Flags().GetBool("containers")
	}
	if cmd.Flags().Changed("images") {
		opts.Images, _ = cmd.Flags().GetBool("images")
	}
	if cmd.Flags().Changed("volumes") {
		opts.Volumes, _ = cmd.Flags().GetBool("volumes")
	}
	if cmd.Flags().Changed("networks") {
		opts.Networks, _ = cmd.Flags().GetBool("networks")
	}
	if cmd.Flags().Changed("build-cache") {
		opts.BuildCache, _ = cmd.Flags().GetBool("build-cache")
	}

	if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.BuildCache {
		return opts, fmt.Errorf("cleanup cancelled: no categories selected (pass --all or a category flag)")
	}
	return opts, nil
}

func confirmCleanup() bool {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	fmt.Print("[tengiz] continue with cleanup? [y/N] ")
	return confirmWithReader(os.Stdin)
}

func confirmWithReader(r io.Reader) bool {
	answer, _ := bufio.NewReader(r).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func optsStrings(opts types.PruneOptions) []string {
	var s []string
	if opts.Containers {
		s = append(s, "containers")
	}
	if opts.Images {
		s = append(s, "images")
	}
	if opts.Volumes {
		s = append(s, "volumes")
	}
	if opts.Networks {
		s = append(s, "networks")
	}
	if opts.BuildCache {
		s = append(s, "build-cache")
	}
	return s
}
```

Register the command and its flags in `init()` (`root.go:34-89`). Add after the `rootCmd.AddCommand(volumeCmd)` line (`root.go:64`):

```go
	rootCmd.AddCommand(cleanupCmd)
```

And add the flag definitions at the end of `init()` (after `root.go:88`):

```go
	cleanupCmd.Flags().Bool("containers", true, "remove stopped containers (Tengiz-managed containers are protected)")
	cleanupCmd.Flags().Bool("images", true, "remove unused images (Tengiz-built images are protected)")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused anonymous volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "remove build cache")
	cleanupCmd.Flags().Bool("all", false, "remove every category of unused resources")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -v -count=1`
Expected: PASS (all CLI tests, including pre-existing ones)

Run the full suite: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command for docker housekeeping"
```

---

### Task 6: Add periodic mode via `--interval`

**Files:**
- Modify: `internal/cli/root.go` — refactor RunE into `runCleanupOnce`, add `--interval` flag and loop
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `cleanupCmd`, `buildCleanupOptions`, `confirmCleanup` (Task 5)
- Produces: `runCleanupOnce(cmd *cobra.Command) error` — a reusable single-cleanup body; the `--interval <duration>` flag repeats it until interrupted

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/root_test.go`:

```go
func TestCleanupIntervalFlagRegistered(t *testing.T) {
	if cleanupCmd.Flags().Lookup("interval") == nil {
		t.Error("cleanupCmd missing --interval flag")
	}
}

func TestCleanupInvalidInterval(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--interval", "bogus"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid --interval") {
		t.Errorf("expected invalid interval error, got %v", err)
	}
}

func TestCleanupIntervalRequiresYes(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--interval", "1h"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("expected --yes required error, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanupInvalidInterval|TestCleanupIntervalRequiresYes' -v -count=1`
Expected: FAIL — `cleanupCmd` has no `--interval` flag, so `Parse` errors (or RunE returns nil when the flag is absent — either way the assertions fail)

- [ ] **Step 3: Write the minimal implementation**

Add the `--interval` flag registration in `init()` (after the `--yes` line from Task 5):

```go
	cleanupCmd.Flags().String("interval", "", "repeat cleanup every <duration> (e.g. 24h) until stopped (requires --yes)")
```

Replace the entire `RunE` of `cleanupCmd` (the block added in Task 5) with:

```go
	RunE: func(cmd *cobra.Command, args []string) error {
		intervalStr, _ := cmd.Flags().GetString("interval")
		if intervalStr != "" {
			interval, err := time.ParseDuration(intervalStr)
			if err != nil {
				return fmt.Errorf("invalid --interval %q: %w", intervalStr, err)
			}
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				return fmt.Errorf("cleanup --interval requires --yes (non-interactive periodic runs cannot confirm)")
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				if err := runCleanupOnce(cmd); err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
				}
			}
		}
		return runCleanupOnce(cmd)
	},
```

Add the extracted one-shot body just below `cleanupCmd` (the code moved verbatim out of the old RunE):

```go
func runCleanupOnce(cmd *cobra.Command) error {
	opts, err := buildCleanupOptions(cmd)
	if err != nil {
		return err
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")

	if dryRun {
		if rt, err := runtime.NewDocker(); err == nil {
			if usage, err := rt.DiskUsage(cmd.Context()); err == nil && strings.TrimSpace(usage) != "" {
				fmt.Println(usage)
			}
		}
		fmt.Printf("[tengiz] dry-run: would prune %s\n", strings.Join(optsStrings(opts), ", "))
		return nil
	}

	if !yes && !confirmCleanup() {
		fmt.Println("[tengiz] cleanup cancelled")
		return nil
	}

	rt, err := runtime.NewDocker()
	if err != nil {
		return err
	}

	if usage, err := rt.DiskUsage(cmd.Context()); err == nil && strings.TrimSpace(usage) != "" {
		fmt.Println(usage)
	}

	result, err := rt.Prune(cmd.Context(), opts)
	if err != nil {
		return err
	}

	for _, l := range result.Detail {
		fmt.Println(l)
	}
	if result.TotalReclaimed != "" {
		fmt.Printf("[tengiz] total reclaimed space: %s\n", result.TotalReclaimed)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -v -count=1`
Expected: PASS

Run the full suite: `go test ./... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): support periodic cleanup with --interval"
```

---

### Task 7: Documentation

**Files:**
- Modify: `README.md` (add a `tengiz cleanup` section after the `tengiz secret list` section, `README.md:410-416`)
- Modify: `AGENTS.md` (CLI list + runtime architecture row)
- Modify: `docs/FUTURES_FEATURES.md` (mark #6 implemented)

**Interfaces:**
- Consumes: the final CLI surface from Tasks 5-6
- Produces: user-facing documentation consistent with existing README/AGENTS.md style

- [ ] **Step 1: Add the README section**

Insert after the `#### `tengiz secret list <app>`` block (ends at `README.md:416`) and before `## Configuration` (`README.md:418`):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to free disk space on the host.

| Flag | Default | Description |
|------|---------|-------------|
| `--containers` | `true` | Remove stopped containers (Tengiz-managed containers are always protected) |
| `--images` | `true` | Remove unused images (Tengiz-built images are always protected) |
| `--volumes` | `false` | Remove unused anonymous volumes |
| `--networks` | `false` | Remove unused networks |
| `--build-cache` | `false` | Remove build cache |
| `--all` | `false` | Remove every category of unused resources |
| `--dry-run` | `false` | Show what would be removed without removing anything |
| `-y`, `--yes` | `false` | Skip the confirmation prompt |
| `--interval <duration>` | | Repeat cleanup every `<duration>` (e.g. `24h`) until stopped (requires `--yes`) |

Tengiz always protects the resources it manages: containers labeled `tengiz-app` and images it built (labeled `tengiz-managed`, tagged `tengiz-apps/*`). Rollback and scale-to-zero (cold start) behavior is never broken by cleanup. Named volumes are never auto-removed.

Non-interactive terminals (CI, scripts) must pass `--yes`.

```bash
tengiz cleanup                        # prune containers + images
tengiz cleanup --all --yes            # prune everything, no prompt
tengiz cleanup --dry-run              # preview what would be removed
tengiz cleanup --interval 24h --yes   # housekeeping job, repeat daily
```
```

- [ ] **Step 2: Update AGENTS.md**

In the CLI list (after the `tengiz secret rotate-key` line at `AGENTS.md:54`), add:

```
tengiz cleanup [--all] [--volumes] [--networks] [--build-cache] [--dry-run] [-y] [--interval DURATION] → prune unused Docker resources (Tengiz-managed containers/images always protected)
```

In the architecture table `runtime` row (`AGENTS.md` line with `runtime.Manager`), append `Prune`, `DiskUsage` to the method list so it reads:

```
| `runtime.Manager` | Interface for container lifecycle. `NewDocker()` = exec-based impl, `NewStub()` = test mock. Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages`, `Prune`, `DiskUsage` for rollback + image cleanup + housekeeping. `ContainerName(name, env)` helper. |
```

- [ ] **Step 3: Update FUTURES_FEATURES.md**

In the P0 table (`docs/FUTURES_FEATURES.md:19`), change:

```
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the "✅ Implemented Features (Not Pending)" table (after the Nixpacks row, `docs/FUTURES_FEATURES.md:253`), add:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-19) |
```

- [ ] **Step 4: Verify**

Run: `go build -o tengiz .` and `go test ./... -v -count=1` and `go vet ./...`
Expected: build succeeds, all tests pass, vet clean

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Integration Verification (manual, requires Docker)

Run against a real Docker daemon to confirm the label-based protection works end-to-end:

```bash
# 1. Deploy a tiny app so a tengiz-managed image exists
tengiz deploy /path/to/some/app
docker image ls --filter "label=tengiz-managed=true"   # must show the app's images

# 2. Start a stopped non-tengiz container (to prove it gets pruned)
docker run -d --name orphan alpine sleep 1000 && docker stop orphan

# 3. Dry run (no destructive action)
tengiz cleanup --dry-run

# 4. Real run, non-interactive
tengiz cleanup --all --yes

# 5. Assertions
docker ps -a --filter "name=orphan"     # empty -> orphan pruned
docker ps -a --filter "label=tengiz-app" # unchanged -> tengiz containers protected
docker images --filter "label=tengiz-managed=true"  # unchanged -> tengiz images protected
```

If step 4 reclaims nothing for images on Docker 29.x (negative-filter regression, moby/moby#52334), confirm `docker image prune -a -f --filter label!=tengiz-managed` also no-ops manually; this is the documented safe-degradation path and the container pruning will still work.

## Self-Review

**Spec coverage:** Feature #6 (Docker Housekeeping — `tengiz cleanup`, label-based prune, protects Tengiz-managed containers) is covered: Task 3 labels images, Task 4 implements pruning, Task 5 adds the command, Task 6 adds periodic mode, Task 7 documents it. #56 (granular prune operations) is intentionally not in scope — this plan is scoped to the P0 feature only.

**Placeholder scan:** No TBD/TODO items; every step contains exact code, file paths, and commands with expected output.

**Type consistency:** `PruneOptions`/`PruneResult`/`CleanupCategory`/`ManagedImageLabel` are defined once in Task 1 and referenced with identical names in Tasks 2-7. `buildPruneArgs`, `buildDiskUsageArgs`, `cleanupCategories`, `extractReclaimedSpace`, `summarizeReclaimed`, `appendDetail` are defined in Task 2 and consumed identically in Task 4. `buildCleanupOptions`, `confirmCleanup`, `confirmWithReader`, `optsStrings`, `runCleanupOnce` are defined in Tasks 5-6 and used consistently. The three mock types (`mockRuntime` x2, `mockRTForDeploy`) all receive the same two interface methods in Task 4.