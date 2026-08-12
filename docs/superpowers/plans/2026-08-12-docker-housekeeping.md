# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes stopped containers, unused images, volumes, networks, and build cache via label-based `docker ... prune` so a single-server Tengiz host reclaims disk space without ever touching Tengiz-managed resources.

**Architecture:** The `runtime.Manager` interface gains a `Cleanup(ctx, CleanupOptions) (CleanupResult, error)` method, implemented in a new `internal/runtime/housekeeping.go` file by shelling out to `docker container prune`, `docker image prune -a`, `docker volume prune`, `docker network prune`, and `docker builder prune`. Every Tengiz container already carries the `tengiz-app=<app>` label, so every prune uses the `--filter label!=tengiz-app` guard. Images only become safe to protect after the `builder` starts tagging them with `--label tengiz-app=<app>` / `--label tengiz-env=<env>` at build time (Task 4 must land with or before the CLI command). A new `tengiz cleanup` Cobra command in `internal/cli/cleanup.go` maps flags to options and prints a per-category summary with total reclaimed space.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (docker CLI calls — no Docker SDK), existing `runtime.Manager`, `builder.Builder` interfaces.

## Global Constraints

- All pruning must protect Tengiz resources: every command uses `--filter label!=tengiz-app` (containers/images/volumes/networks) — never prune anything labeled `tengiz-app`.
- Default `tengiz cleanup` (no flags) prunes **containers + images + networks + build cache**. Volumes are **never** pruned by default (data safety); enable via `--volumes` or `--all`.
- Image tags and build labels: tag stays `tengiz-apps/<app>:<env>-<deploymentID>`; new labels `tengiz-app=<app>` and `tengiz-env=<env>` are added to both Dockerfile and nixpacks builds.
- Caveat (must be documented in README): images built *before* this feature do not carry the `tengiz-app` label, so the first `tengiz cleanup --images` removes unused unlabeled images including pre-existing rollback images. Re-deploy apps once to re-tag with labels.
- No new external Go dependencies. Docker CLI must be installed (already a repo requirement).
- The `tengiz-app`/`tengiz-env` label constants live in `internal/runtime/docker.go` (`labelKey`, `envLabelKey`) — reuse them; do not define new literals.
- Docker reports reclaimed space in decimal SI units (`kB` = 1000 bytes, `MB` = 10^6, etc.) — the parser must match this.
- Existing tests must keep passing. Adding `Cleanup` to the `Manager` interface breaks `mockRTForDeploy` in `internal/cli/root_test.go` — that mock must be updated in the same task.
- Every task ends with `go build ./...` and `go test ./internal/<pkg>/... -v -count=1` green, then a commit.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupResult` types; add `Cleanup` to `Manager` interface; add stub impl |
| `internal/runtime/housekeeping.go` (new) | `dockerRuntime.Cleanup` + per-category prune helpers + `parsePruneOutput`, `parseReclaimedSize`, `HumanSize` |
| `internal/runtime/housekeeping_test.go` (new) | Unit tests for parse/format helpers and stub `Cleanup` |
| `internal/builder/builder.go` | Add `buildLabels()` helper; pass labels to Dockerfile and nixpacks builds |
| `internal/builder/builder_test.go` | Test `buildLabels` |
| `internal/cli/cleanup.go` (new) | `cleanupCmd` command + `cleanupOptionsFromFlags` helper |
| `internal/cli/cleanup_test.go` (new) | Registration, flag presence, options-mapping tests |
| `internal/cli/root.go` | Register `cleanupCmd` + its flags in `init()` |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy` (interface conformance) |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Add `Cleanup` to the `runtime.Manager` interface

**Files:**
- Modify: `internal/runtime/runtime.go` — add types, interface method, stub method
- Modify: `internal/cli/root_test.go` — add `Cleanup` to `mockRTForDeploy` so it keeps satisfying `runtime.Manager`
- Test: `internal/runtime/housekeeping_test.go` (create; add stub test here)

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `runtime.CleanupOptions{Containers, Images, Volumes, Networks, BuildCache bool}`
  - `runtime.CleanupResult{ContainersPruned, ImagesPruned, VolumesPruned, NetworksPruned int; BuildCachePruned bool; ReclaimedBytes int64; Summary []string}`
  - `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` on `Manager`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/housekeeping_test.go`:

```go
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersPruned != 0 || res.ImagesPruned != 0 {
		t.Errorf("Cleanup() = %+v, want all-zero result", res)
	}
	if len(res.Summary) != 0 {
		t.Errorf("Cleanup() summary = %v, want empty", res.Summary)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`

Expected: FAIL with `undefined: CleanupOptions` (and the package will not compile until the interface changes below are complete).

- [ ] **Step 3: Add types and interface method to `internal/runtime/runtime.go`**

Add the two types before the `Manager` interface (after `RunOptions`, line ~29):

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type CleanupResult struct {
	ContainersPruned int
	ImagesPruned     int
	VolumesPruned    int
	NetworksPruned   int
	BuildCachePruned bool
	ReclaimedBytes   int64
	Summary          []string
}
```

Add the method to the `Manager` interface (after `KeepLastNImages`, line ~36):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

- [ ] **Step 4: Add the stub method in `internal/runtime/runtime.go`**

Add after the `KeepLastNImages` stub (line ~119):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 5: Update `mockRTForDeploy` in `internal/cli/root_test.go`**

The interface change breaks compilation of the existing mock. Add after `KeepLastNImages` (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

Run: `go test ./internal/cli/... -run TestMockRTForDeployImplementsManager -v -count=1`

Expected: PASS

- [ ] **Step 7: Run full runtime + cli test packages**

Run: `go test ./internal/runtime/... ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/housekeeping_test.go internal/cli/root_test.go
git commit -m "feat: add Cleanup method to runtime Manager interface"
```

---

### Task 2: Parse and format helpers for prune output

**Files:**
- Create: `internal/runtime/housekeeping.go` — parse/format helpers only (Task 3 adds the docker calls to this same file)
- Test: `internal/runtime/housekeeping_test.go` — add helper tests

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `parsePruneOutput(out []byte) (count int, reclaimed int64)` — unexported, used by Task 3
  - `parseReclaimedSize(s string) int64` — unexported, used by `parsePruneOutput`
  - `HumanSize(n int64) string` — **exported**, used by the CLI in Task 5

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/housekeeping_test.go`:

```go
func TestParseReclaimedSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"32B", 32},
		{"0B", 0},
		{"1.234kB", 1234},
		{"12.4MB", 12400000},
		{"1.1GB", 1100000000},
		{"", 0},
		{"garbage", 0},
	}
	for _, c := range cases {
		if got := parseReclaimedSize(c.in); got != c.want {
			t.Errorf("parseReclaimedSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParsePruneOutputContainers(t *testing.T) {
	out := []byte(`Deleted Containers:
38db60a4f83b
2e3a4b5c6d7e

Total reclaimed space: 32B
`)
	count, reclaimed := parsePruneOutput(out)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if reclaimed != 32 {
		t.Errorf("reclaimed = %d, want 32", reclaimed)
	}
}

func TestParsePruneOutputImages(t *testing.T) {
	out := []byte(`Deleted Images:
untagged: foo:latest
deleted: sha256:abc
untagged: bar:v2
deleted: sha256:def

Total reclaimed space: 12.4MB
`)
	count, reclaimed := parsePruneOutput(out)
	if count != 4 {
		t.Errorf("count = %d, want 4", count)
	}
	if reclaimed != 12400000 {
		t.Errorf("reclaimed = %d, want 12400000", reclaimed)
	}
}

func TestParsePruneOutputBuildCache(t *testing.T) {
	out := []byte(`Build cache entries:
TYPE    DIGEST    SIZE     SHARED SIZE    CONTENT SIZE
internal    abc123    10MB    5MB    5MB
mounted     def456    1MB     0B     1MB

Total reclaimed space: 11MB
`)
	count, reclaimed := parsePruneOutput(out)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if reclaimed != 11000000 {
		t.Errorf("reclaimed = %d, want 11000000", reclaimed)
	}
}

func TestParsePruneOutputNothingDeleted(t *testing.T) {
	out := []byte(`Total reclaimed space: 0B
`)
	count, reclaimed := parsePruneOutput(out)
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if reclaimed != 0 {
		t.Errorf("reclaimed = %d, want 0", reclaimed)
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{32, "32B"},
		{1500, "1.5kB"},
		{12400000, "12.4MB"},
		{1100000000, "1.1GB"},
	}
	for _, c := range cases {
		if got := HumanSize(c.in); got != c.want {
			t.Errorf("HumanSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestParseReclaimedSize|TestParsePruneOutput|TestHumanSize' -v -count=1`

Expected: FAIL with `undefined: parsePruneOutput` / `undefined: HumanSize`

- [ ] **Step 3: Write the helpers in `internal/runtime/housekeeping.go`**

Create the file with the following content (Task 3 appends the docker calls):

```go
package runtime

import (
	"fmt"
	"strconv"
	"strings"
)

// parsePruneOutput extracts the number of deleted entries and the total
// reclaimed space from the output of a docker prune command.
func parsePruneOutput(out []byte) (count int, reclaimed int64) {
	lines := strings.Split(string(out), "\n")
	inBody := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Deleted ") || strings.HasPrefix(line, "Build cache entries:") {
			inBody = true
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			reclaimed = parseReclaimedSize(strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:")))
			break
		}
		if inBody {
			if strings.HasPrefix(line, "TYPE ") && strings.Contains(line, "DIGEST") {
				continue
			}
			count++
		}
	}
	return count, reclaimed
}

// parseReclaimedSize parses a docker size string ("32B", "1.234kB", "12.4MB",
// "1.1GB", "2TB") into bytes. Docker reports decimal SI units (kB = 1000).
func parseReclaimedSize(s string) int64 {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		mult   int64
	}{
		{"TB", 1000 * 1000 * 1000 * 1000},
		{"GB", 1000 * 1000 * 1000},
		{"MB", 1000 * 1000},
		{"kB", 1000},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0
			}
			return int64(f * float64(u.mult))
		}
	}
	return 0
}

// HumanSize formats a byte count using decimal SI units to match docker's
// "Total reclaimed space" convention (e.g. 12400000 -> "12.4MB").
func HumanSize(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%dB", n)
	}
	units := []struct {
		suffix string
		div    float64
	}{
		{"kB", 1000},
		{"MB", 1000 * 1000},
		{"GB", 1000 * 1000 * 1000},
		{"TB", 1000 * 1000 * 1000 * 1000},
	}
	f := float64(n)
	for _, u := range units {
		if f/float64(u.div) < 1000 {
			return fmt.Sprintf("%.1f%s", f/float64(u.div), u.suffix)
		}
	}
	return fmt.Sprintf("%.1fPB", f/(1000*1000*1000*1000*1000))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestParseReclaimedSize|TestParsePruneOutput|TestHumanSize' -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add docker prune output parsing and size formatting helpers"
```

---

### Task 3: Implement `dockerRuntime.Cleanup`

**Files:**
- Modify: `internal/runtime/housekeeping.go` — append `Cleanup` and per-category prune methods

**Interfaces:**
- Consumes: `parsePruneOutput`, `HumanSize` from Task 2; `labelKey`/`envLabelKey` constants from `internal/runtime/docker.go`
- Produces: `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` on the docker runtime; the CLI in Task 5 consumes it

- [ ] **Step 1: Write the failing test (interface-level)**

This method shells out to real docker, so it cannot run in CI without Docker. The test asserts the exported contract via the stub (already covered in Task 1) and verifies the docker runtime satisfies the interface:

```go
// Append to internal/runtime/housekeeping_test.go
func TestDockerRuntimeSatisfiesCleanupInterface(t *testing.T) {
	var m Manager = &dockerRuntime{}
	if m == nil {
		t.Fatal("dockerRuntime does not implement Manager")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestDockerRuntimeSatisfiesCleanupInterface -v -count=1`

Expected: FAIL with `missing method Cleanup` on `*dockerRuntime`.

- [ ] **Step 3: Implement `Cleanup` in `internal/runtime/housekeeping.go`**

Append to the file. Add `os/exec` and `context` to the imports:

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)
```

Then append:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	if opts.Containers {
		count, reclaimed, err := r.pruneContainers(ctx)
		if err != nil {
			return res, err
		}
		res.ContainersPruned = count
		res.ReclaimedBytes += reclaimed
		res.Summary = append(res.Summary, fmt.Sprintf("containers: pruned %d, reclaimed %s", count, HumanSize(reclaimed)))
	}

	if opts.Images {
		count, reclaimed, err := r.pruneImages(ctx)
		if err != nil {
			return res, err
		}
		res.ImagesPruned = count
		res.ReclaimedBytes += reclaimed
		res.Summary = append(res.Summary, fmt.Sprintf("images: pruned %d, reclaimed %s", count, HumanSize(reclaimed)))
	}

	if opts.Volumes {
		count, reclaimed, err := r.pruneVolumes(ctx)
		if err != nil {
			return res, err
		}
		res.VolumesPruned = count
		res.ReclaimedBytes += reclaimed
		res.Summary = append(res.Summary, fmt.Sprintf("volumes: pruned %d, reclaimed %s", count, HumanSize(reclaimed)))
	}

	if opts.Networks {
		count, reclaimed, err := r.pruneNetworks(ctx)
		if err != nil {
			return res, err
		}
		res.NetworksPruned = count
		res.ReclaimedBytes += reclaimed
		res.Summary = append(res.Summary, fmt.Sprintf("networks: pruned %d, reclaimed %s", count, HumanSize(reclaimed)))
	}

	if opts.BuildCache {
		count, reclaimed, err := r.pruneBuildCache(ctx)
		if err != nil {
			return res, err
		}
		res.BuildCachePruned = true
		res.ReclaimedBytes += reclaimed
		res.Summary = append(res.Summary, fmt.Sprintf("build cache: pruned %d entries, reclaimed %s", count, HumanSize(reclaimed)))
	}

	return res, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context) (int, int64, error) {
	cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f", "--filter", "label!="+labelKey)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	count, reclaimed := parsePruneOutput(out)
	return count, reclaimed, nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context) (int, int64, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-af", "--filter", "label!="+labelKey)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	count, reclaimed := parsePruneOutput(out)
	return count, reclaimed, nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context) (int, int64, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f", "--filter", "label!="+labelKey)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	count, reclaimed := parsePruneOutput(out)
	return count, reclaimed, nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context) (int, int64, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f", "--filter", "label!="+labelKey)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	count, reclaimed := parsePruneOutput(out)
	return count, reclaimed, nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) (int, int64, error) {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	count, reclaimed := parsePruneOutput(out)
	return count, reclaimed, nil
}
```

Note: `labelKey` is `"tengiz-app"` (defined in `internal/runtime/docker.go`), so `--filter label!=tengiz-app` protects every Tengiz container and (once Task 4 lands) every Tengiz image.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestDockerRuntimeSatisfiesCleanupInterface -v -count=1`

Expected: PASS

- [ ] **Step 5: Run full runtime test package**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeeping.go
git commit -m "feat: implement label-based docker cleanup in runtime"
```

---

### Task 4: Tag Tengiz images with protection labels

**Files:**
- Modify: `internal/builder/builder.go` — add `buildLabels()` and pass labels to both build paths
- Test: `internal/builder/builder_test.go` — test `buildLabels`

**Interfaces:**
- Consumes: nothing new
- Produces: `buildLabels(appName, env string) []string` returning `[]string{"--label", "tengiz-app=<app>", "--label", "tengiz-env=<env>"}`; every image built from this point forward carries the `tengiz-app` label so Task 3's `label!=tengiz-app` image prune protects them.

- [ ] **Step 1: Write the failing test**

Append to `internal/builder/builder_test.go`:

```go
func TestBuildLabels(t *testing.T) {
	got := buildLabels("myapp", "staging")
	want := []string{"--label", "tengiz-app=myapp", "--label", "tengiz-env=staging"}
	if len(got) != len(want) {
		t.Fatalf("buildLabels() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("buildLabels() = %v, want %v", got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestBuildLabels -v -count=1`

Expected: FAIL with `undefined: buildLabels`

- [ ] **Step 3: Add the `buildLabels` helper and wire it into both builds**

Add to `internal/builder/builder.go` (after `buildSecretArgs`, line ~99):

```go
func buildLabels(appName, env string) []string {
	return []string{
		"--label", fmt.Sprintf("tengiz-app=%s", appName),
		"--label", fmt.Sprintf("tengiz-env=%s", env),
	}
}
```

In `buildWithDockerfile` (line ~69), replace:

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

with:

```go
	args := []string{"build"}
	args = append(args, buildLabels(appName, env)...)
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

In `buildWithNixpacks` (line ~139), replace:

```go
	args := []string{"build", dir, "--name", tag}
```

with:

```go
	args := []string{"build", dir, "--name", tag}
	args = append(args, buildLabels(appName, env)...)
```

(`nixpacks build` accepts `--label <label...>` as a repeatable flag, so two separate `--label` args are valid.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/builder/ -run TestBuildLabels -v -count=1`

Expected: PASS

- [ ] **Step 5: Run full builder test package**

Run: `go test ./internal/builder/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: label tengiz images at build time for cleanup protection"
```

---

### Task 5: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go` — `cleanupCmd` + `cleanupOptionsFromFlags`
- Create: `internal/cli/cleanup_test.go` — registration, flags, options mapping
- Modify: `internal/cli/root.go` — register command and flags in `init()`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.Cleanup(ctx, opts)`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.HumanSize` from Tasks 1-3
- Produces: `tengiz cleanup` command; `cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions` (exported test hook)

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
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
	for _, flag := range []string{"containers", "images", "volumes", "networks", "build-cache", "all"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func newCleanupTestCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Bool("containers", false, "")
	c.Flags().Bool("images", false, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("build-cache", false, "")
	c.Flags().Bool("all", false, "")
	return c
}

func TestCleanupOptionsDefaults(t *testing.T) {
	c := newCleanupTestCmd()
	c.ParseFlags([]string{})
	opts := cleanupOptionsFromFlags(c)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Errorf("default opts = %+v, want containers+images+networks+build-cache", opts)
	}
	if opts.Volumes {
		t.Errorf("default opts should not prune volumes, got %+v", opts)
	}
}

func TestCleanupOptionsVolumes(t *testing.T) {
	c := newCleanupTestCmd()
	c.ParseFlags([]string{"--volumes"})
	opts := cleanupOptionsFromFlags(c)
	if !opts.Volumes {
		t.Errorf("opts = %+v, want Volumes=true", opts)
	}
	if !opts.Containers {
		t.Errorf("opts = %+v, want Containers=true (volumes is additive)", opts)
	}
}

func TestCleanupOptionsAll(t *testing.T) {
	c := newCleanupTestCmd()
	c.ParseFlags([]string{"--all"})
	opts := cleanupOptionsFromFlags(c)
	want := runtime.CleanupOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	if opts != want {
		t.Errorf("opts = %+v, want %+v", opts, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` / `undefined: cleanupOptionsFromFlags`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prunes stopped containers, unused images, networks and build cache.
Tengiz-managed containers and images (labeled tengiz-app=*) are always protected.

With no flags, prunes containers, images, networks and build cache.
Add --volumes (or --all) to also prune unused Docker volumes.

Examples:
  tengiz cleanup              # containers + images + networks + build cache
  tengiz cleanup --volumes    # also prune unused volumes
  tengiz cleanup --all        # prune everything including volumes`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptionsFromFlags(cmd)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		res, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] cleanup complete:")
		if len(res.Summary) == 0 {
			fmt.Println("  nothing selected to prune")
			return nil
		}
		for _, line := range res.Summary {
			fmt.Printf("  %s\n", line)
		}
		fmt.Printf("[tengiz] total reclaimed: %s\n", runtime.HumanSize(res.ReclaimedBytes))
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	all, _ := cmd.Flags().GetBool("all")

	if all {
		return runtime.CleanupOptions{
			Containers: true,
			Images:     true,
			Volumes:    true,
			Networks:   true,
			BuildCache: true,
		}
	}

	if !containers && !images && !volumes && !networks && !buildCache {
		return runtime.CleanupOptions{
			Containers: true,
			Images:     true,
			Networks:   true,
			BuildCache: true,
		}
	}

	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
	}
}
```

- [ ] **Step 4: Register the command and flags in `internal/cli/root.go`**

In `init()` (after `rootCmd.AddCommand(notificationCmd)`, line ~75), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers (default when no flags given)")
	cleanupCmd.Flags().Bool("images", false, "prune unused images (default when no flags given)")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (opt-in for data safety)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks (default when no flags given)")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache (default when no flags given)")
	cleanupCmd.Flags().Bool("all", false, "prune everything including volumes")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`

Expected: PASS

- [ ] **Step 6: Build and run the full cli test package**

Run: `go build ./...`

Expected: Build succeeds

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 6: Documentation and final verification

**Files:**
- Modify: `README.md` — add `tengiz cleanup` section to CLI Reference (after `### tengiz ps`, ~line 150)
- Modify: `AGENTS.md` — add cleanup to the CLI command list
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

- [ ] **Step 1: Document `tengiz cleanup` in `README.md`**

After the `### tengiz ps` section (line ~150), insert:

```markdown
### `tengiz cleanup [flags]`

Prune unused Docker resources to reclaim disk space. Tengiz-managed containers and images (labeled `tengiz-app=*`) are always protected from pruning.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers (default when no flags given) |
| `--images` | Prune unused images (default when no flags given) |
| `--networks` | Prune unused networks (default when no flags given) |
| `--build-cache` | Prune Docker build cache (default when no flags given) |
| `--volumes` | Prune unused volumes (opt-in — data safety) |
| `--all` | Prune everything including volumes |

Examples:
```
tengiz cleanup
tengiz cleanup --volumes
tengiz cleanup --all
```

Running `tengiz cleanup` with no flags prunes stopped containers, unused images, unused networks, and build cache. Image pruning uses the `label!=tengiz-app` filter, so images built by Tengiz (labeled `tengiz-app` at build time) and images referenced by running containers survive. Note: images built before this feature do not carry the `tengiz-app` label and may be pruned if unused — re-deploy an app to re-tag its image with protection labels.
```

- [ ] **Step 2: Update `AGENTS.md`**

In the CLI command list (after the `tengiz notification show` line), add:

```
tengiz cleanup [--containers|--images|--volumes|--networks|--build-cache|--all] → prune unused Docker resources (label-based, protects tengiz-app resources)
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

1. In the P0 table, change the row for `#6` from `⬜` to `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

2. In the `✅ Implemented Features (Not Pending)` table, add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-12) |
```

3. In the `## Docker Housekeeping (Otomatik Temizlik)` feature section, add a Status line after the `- **Description:**` line:

```markdown
- **Status:** ✅ Implemented (2026-08-12)
```

- [ ] **Step 4: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS. Note: the `proxy` tests are slow (~2s each, TCP dial timeout) and `idle` tests are time-sensitive — allow them to run; failures in those packages unrelated to this change indicate an environment issue, not this plan.

- [ ] **Step 5: Run static analysis**

Run: `go vet ./...`

Expected: No issues reported

- [ ] **Step 6: Manual smoke test (if Docker is available)**

Run: `go build -o tengiz .`

Then:
```bash
./tengiz cleanup
```

Expected output (with a Docker daemon):
```
[tengiz] cleanup complete:
  containers: pruned N, reclaimed X
  images: pruned N, reclaimed X
  networks: pruned N, reclaimed X
  build cache: pruned N entries, reclaimed X
[tengiz] total reclaimed: X
```

Verify protection: deploy a dummy app (`./tengiz deploy --help` for usage), run `docker ps -a` and confirm its container (labeled `tengiz-app=<app>`) is still present after `./tengiz cleanup`. If Docker is not available, skip this step.

- [ ] **Step 7: Self-review against spec**

- Spec ("Label-based `docker system prune`. `tengiz cleanup`"): every prune call uses `--filter label!=tengiz-app` and the command is `tengiz cleanup`. ✅ (Tasks 3, 5)
- Spec ("kullanılmayan volume, network, container ve image'leri periyodik temizleme"): all four categories covered; periodic scheduling is intentionally out of scope for this plan (the CLI command is the deliverable; a scheduler can be added later on top of `runtime.Cleanup`). ✅
- Spec ("Tengiz yönetimindeki container'lar korunur"): containers protected via existing `tengiz-app` label. ✅
- Placeholder scan: no TBD/TODO/"similar to task" patterns — every step has complete code. ✅
- Type consistency: `CleanupOptions`/`CleanupResult` field names identical in runtime (Tasks 1-3) and CLI (Task 5); `Cleanup(ctx, opts)` signature identical; `HumanSize` exported and used by the CLI. ✅

- [ ] **Step 8: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```
