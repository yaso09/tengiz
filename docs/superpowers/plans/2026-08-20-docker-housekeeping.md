# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims Docker disk space (stopped containers, dangling images, unused networks/volumes, build cache) while protecting all Tengiz-managed resources via label-based filtering.

**Architecture:** Three layers. (1) The `builder` package adds `tengiz-app`/`tengiz-env` labels to every image it builds so pruning can protect them. (2) The `runtime` package gets a new `Cleanup(ctx, opts) (*CleanupStats, error)` method on `Manager` that shells out to per-category `docker <type> prune` commands guarded by `--filter "label!=tengiz-app"` (dry-run uses `docker <type> ls` variants and counts candidates). (3) The `cli` package exposes `tengiz cleanup` with per-category flags, a default of containers+images+build-cache, `--all` to add networks+volumes, and `--dry-run`. All docker-command construction and output parsing live in pure, unit-testable functions following the existing `buildLogArgs`/`buildRunArgs` pattern.

**Tech Stack:** Go 1.26, `os/exec` (no Docker SDK), `spf13/cobra`, `docker` CLI (`container/image/network/volume/builder prune`, `docker system` not used — categories are pruned individually so dry-run and stats stay exact).

## Global Constraints

- Go module `github.com/yaso09/tengiz`, Go 1.26.
- Runtime must shell out to the `docker` CLI via `os/exec` — never use a Docker SDK.
- Tengiz-managed containers are labeled `tengiz-app=<appname>` (const `labelKey` in `internal/runtime/docker.go:76`) and `tengiz-env=<env>` (const `envLabelKey`, same file). Reuse these constants; do not hardcode strings.
- Tengiz images are named `tengiz-apps/<app>:<env>-<deploymentID>` (see `internal/builder/builder.go:61`).
- Pruning must NEVER delete a running or stopped Tengiz container, a tagged Tengiz rollback image, or a volume/network that Tengiz manages. Protection rule: every prune command carries `--filter "label!=tengiz-app"`.
- Image pruning must NOT use `-a` on `docker image prune`: tagged images (`tengiz-apps/...:latest`, rollback tags) are preserved by per-app retention (`KeepLastNImages`, retain 5) and must survive cleanup. Dangling-only pruning is the safe default.
- CLI stdout messages use the `[tengiz] ` prefix.
- All category args/list commands, size parsers, and the CLI category resolver must be pure functions so tests run without Docker.
- Verification commands: `go build -o tengiz .`, `go test ./... -v -count=1`, `go vet ./...`. Every task must end with these passing before committing.
- Create branch `feat/docker-housekeeping` before starting (AGENTS.md rule: new features get a branch).
- Commit per task with a conventional `feat:` message (repo style).

---

### Task 1: Label Tengiz Images at Build Time

Give every image built by Tengiz the `tengiz-app` and `tengiz-env` labels so `tengiz cleanup` can protect them with the `label!=tengiz-app` filter. Both the Dockerfile builder and the Nixpacks builder must emit the labels (Nixpacks supports `--label`).

**Files:**
- Modify: `internal/builder/builder.go` (`buildWithDockerfile` at lines 57-91, `buildWithNixpacks` at lines 129-170)
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `imageLabelArgs(appName, env string) []string` — returns `[]string{"--label", "tengiz-app=<appName>", "--label", "tengiz-env=<env>"}`. Task 2's prune filters rely on this label being present.

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing test**

Append to `internal/builder/builder_test.go`:

```go
func TestImageLabelArgs(t *testing.T) {
	got := imageLabelArgs("testapp", "staging")
	expected := []string{"--label", "tengiz-app=testapp", "--label", "tengiz-env=staging"}
	if len(got) != len(expected) {
		t.Fatalf("imageLabelArgs() = %v (len=%d), want %v (len=%d)", got, len(got), expected, len(expected))
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Fatalf("imageLabelArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestImageLabelArgsProductionEnv(t *testing.T) {
	got := imageLabelArgs("myapp", "production")
	if len(got) != 4 {
		t.Fatalf("imageLabelArgs() len = %d, want 4: %v", len(got), got)
	}
	if got[1] != "tengiz-app=myapp" || got[3] != "tengiz-env=production" {
		t.Fatalf("imageLabelArgs() = %v, want tengiz-app=myapp and tengiz-env=production", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/builder/ -run TestImageLabelArgs -v -count=1`
Expected: FAIL — `undefined: imageLabelArgs`.

- [ ] **Step 4: Add the helper and wire it into both builders**

Add the helper above `buildWithDockerfile` in `internal/builder/builder.go`:

```go
func imageLabelArgs(appName, env string) []string {
	return []string{"--label", fmt.Sprintf("tengiz-app=%s", appName), "--label", fmt.Sprintf("tengiz-env=%s", env)}
}
```

Modify `buildWithDockerfile` so the build args include the labels:

```go
	args := []string{"build"}
	args = append(args, imageLabelArgs(appName, env)...)
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

Modify `buildWithNixpacks` so the args include the labels (Nixpacks `--label`/`-l` accepts `key=value` pairs):

```go
	args := []string{"build", dir, "--name", tag}
	args = append(args, imageLabelArgs(appName, env)...)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/builder/ -run TestImageLabelArgs -v -count=1`
Expected: PASS.

- [ ] **Step 6: Verify build + vet + full test suite**

```bash
go build -o tengiz . && go vet ./... && go test ./... -v -count=1
```

Expected: build succeeds, `go vet` clean, all tests PASS. (The existing `TestBuildCapturesOutput`/`TestBuildWithNixpacksDispatches` integration tests skip when Docker/Nixpacks is unavailable — that is expected, not a failure.)

- [ ] **Step 7: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: label built images with tengiz-app and tengiz-env"
```

---

### Task 2: Runtime Prune Helpers (Pure Functions)

Create the pure, unit-testable building blocks for cleanup: per-category `docker` command construction, a candidate counter for prune/list output, and byte-size parsers for the "Total reclaimed space: X" lines docker prints.

**Files:**
- Create: `internal/runtime/prune.go` (helpers only — the `Cleanup` method comes in Task 3)
- Create: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `labelKey` const from `internal/runtime/docker.go:76`.
- Produces (used by Task 3 and Task 4):
  - `type CleanupOptions struct { Containers, Images, Networks, Volumes, BuildCache, DryRun bool }`
  - `type CleanupStats struct { ContainersRemoved, ImagesRemoved, NetworksRemoved, VolumesRemoved, BuildCacheRemoved int; ReclaimedBytes int64 }`
  - `pruneContainerArgs() []string` → `["container", "prune", "-f", "--filter", "label!=tengiz-app"]`
  - `pruneContainerListArgs() []string` → `["container", "ls", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.ID}}"]`
  - `pruneImageArgs() []string` → `["image", "prune", "-f", "--filter", "label!=tengiz-app"]` (dangling only — no `-a`, per Global Constraints)
  - `pruneImageListArgs() []string` → `["image", "ls", "-a", "--filter", "dangling=true", "--filter", "label!=tengiz-app", "--format", "{{.ID}}"]`
  - `pruneNetworkArgs() []string` → `["network", "prune", "-f", "--filter", "label!=tengiz-app"]`
  - `pruneNetworkListArgs() []string` → `["network", "ls", "--filter", "label!=tengiz-app", "--format", "{{.ID}}"]`
  - `pruneVolumeArgs() []string` → `["volume", "prune", "-f", "--filter", "label!=tengiz-app"]`
  - `pruneVolumeListArgs() []string` → `["volume", "ls", "--filter", "label!=tengiz-app", "--format", "{{.Name}}"]`
  - `pruneBuildCacheArgs() []string` → `["builder", "prune", "-f"]`
  - `pruneBuildCacheListArgs() []string` → `["builder", "du"]`
  - `runCommandOutput(ctx context.Context, name string, args ...string) (string, error)`
  - `countPruneCandidates(out string) int`
  - `parseSize(s string) (int64, error)`
  - `parseReclaimedSpace(line string) (int64, error)`
  - `reclaimedFromOutput(out string) int64`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import "testing"

func TestPruneContainerArgs(t *testing.T) {
	assertArgsEqual(t, pruneContainerArgs(), []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"})
}

func TestPruneContainerListArgs(t *testing.T) {
	assertArgsEqual(t, pruneContainerListArgs(), []string{"container", "ls", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.ID}}"})
}

func TestPruneImageArgs(t *testing.T) {
	assertArgsEqual(t, pruneImageArgs(), []string{"image", "prune", "-f", "--filter", "label!=tengiz-app"})
}

func TestPruneImageListArgs(t *testing.T) {
	assertArgsEqual(t, pruneImageListArgs(), []string{"image", "ls", "-a", "--filter", "dangling=true", "--filter", "label!=tengiz-app", "--format", "{{.ID}}"})
}

func TestPruneNetworkArgs(t *testing.T) {
	assertArgsEqual(t, pruneNetworkArgs(), []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"})
}

func TestPruneNetworkListArgs(t *testing.T) {
	assertArgsEqual(t, pruneNetworkListArgs(), []string{"network", "ls", "--filter", "label!=tengiz-app", "--format", "{{.ID}}"})
}

func TestPruneVolumeArgs(t *testing.T) {
	assertArgsEqual(t, pruneVolumeArgs(), []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
}

func TestPruneVolumeListArgs(t *testing.T) {
	assertArgsEqual(t, pruneVolumeListArgs(), []string{"volume", "ls", "--filter", "label!=tengiz-app", "--format", "{{.Name}}"})
}

func TestPruneBuildCacheArgs(t *testing.T) {
	assertArgsEqual(t, pruneBuildCacheArgs(), []string{"builder", "prune", "-f"})
}

func TestPruneBuildCacheListArgs(t *testing.T) {
	assertArgsEqual(t, pruneBuildCacheListArgs(), []string{"builder", "du"})
}

func TestCountPruneCandidatesContainers(t *testing.T) {
	out := `Deleted Containers:
abc123def456
fed654cba321

Total reclaimed space: 3.2kB`
	if got := countPruneCandidates(out); got != 2 {
		t.Fatalf("countPruneCandidates() = %d, want 2", got)
	}
}

func TestCountPruneCandidatesBuildCache(t *testing.T) {
	out := `Removed build cache entry 8c4d2f
Removed build cache entry 9a1b3c

Total reclaimed space: 50.52MB`
	if got := countPruneCandidates(out); got != 2 {
		t.Fatalf("countPruneCandidates() = %d, want 2", got)
	}
}

func TestCountPruneCandidatesEmpty(t *testing.T) {
	if got := countPruneCandidates(""); got != 0 {
		t.Fatalf("countPruneCandidates() = %d, want 0", got)
	}
}

func TestCountPruneCandidatesNothingToDo(t *testing.T) {
	if got := countPruneCandidates("Total reclaimed space: 0B"); got != 0 {
		t.Fatalf("countPruneCandidates() = %d, want 0", got)
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"512B", 512},
		{"1kB", 1000},
		{"3.2kB", 3200},
		{"1MB", 1000000},
		{"1.5MB", 1500000},
		{"2GB", 2000000000},
		{"1MiB", 1048576},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseSize(tt.in)
			if err != nil {
				t.Fatalf("parseSize(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseSizeUnknown(t *testing.T) {
	if _, err := parseSize("wat"); err == nil {
		t.Fatal("parseSize() expected error for unknown format")
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	got, err := parseReclaimedSpace("Total reclaimed space: 1.2GB")
	if err != nil {
		t.Fatalf("parseReclaimedSpace() error = %v", err)
	}
	if got != 1200000000 {
		t.Fatalf("parseReclaimedSpace() = %d, want 1200000000", got)
	}
}

func TestParseReclaimedSpaceZero(t *testing.T) {
	got, err := parseReclaimedSpace("Total reclaimed space: 0B")
	if err != nil {
		t.Fatalf("parseReclaimedSpace() error = %v", err)
	}
	if got != 0 {
		t.Fatalf("parseReclaimedSpace() = %d, want 0", got)
	}
}

func TestReclaimedFromOutput(t *testing.T) {
	out := `Deleted Containers:
abc123def456

Total reclaimed space: 1.2kB

Total reclaimed space: 0B`
	if got := reclaimedFromOutput(out); got != 1200 {
		t.Fatalf("reclaimedFromOutput() = %d, want 1200", got)
	}
}

func assertArgsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v (len=%d), want %v (len=%d)", got, len(got), want, len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestPrune -v -count=1`
Expected: FAIL — build error, `undefined: pruneContainerArgs` etc.

- [ ] **Step 3: Write the helpers**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// CleanupOptions controls which categories of Docker resources are pruned.
type CleanupOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	DryRun     bool
}

// CleanupStats reports what a Cleanup call removed (or, in dry-run mode, would remove).
type CleanupStats struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	BuildCacheRemoved int
	ReclaimedBytes    int64
}

func pruneContainerArgs() []string {
	return []string{"container", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey)}
}

func pruneContainerListArgs() []string {
	return []string{"container", "ls", "-a", "--filter", "status=exited", "--filter", fmt.Sprintf("label!=%s", labelKey), "--format", "{{.ID}}"}
}

// pruneImageArgs prunes dangling images only. The -a flag is intentionally
// omitted so tagged Tengiz rollback images are never removed.
func pruneImageArgs() []string {
	return []string{"image", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey)}
}

func pruneImageListArgs() []string {
	return []string{"image", "ls", "-a", "--filter", "dangling=true", "--filter", fmt.Sprintf("label!=%s", labelKey), "--format", "{{.ID}}"}
}

func pruneNetworkArgs() []string {
	return []string{"network", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey)}
}

func pruneNetworkListArgs() []string {
	return []string{"network", "ls", "--filter", fmt.Sprintf("label!=%s", labelKey), "--format", "{{.ID}}"}
}

func pruneVolumeArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", labelKey)}
}

func pruneVolumeListArgs() []string {
	return []string{"volume", "ls", "--filter", fmt.Sprintf("label!=%s", labelKey), "--format", "{{.Name}}"}
}

func pruneBuildCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func pruneBuildCacheListArgs() []string {
	return []string{"builder", "du"}
}

func runCommandOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w\n%s", name, err, string(out))
	}
	return string(out), nil
}

// countPruneCandidates counts the per-item lines in docker prune or ls output,
// ignoring headers, blank lines, and the "Total reclaimed space:" trailer.
func countPruneCandidates(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space") {
			continue
		}
		switch line {
		case "Deleted Containers:", "Deleted Images:", "Deleted Networks:", "Deleted Volumes:":
			continue
		}
		count++
	}
	return count
}

var sizeSuffixes = []struct {
	suffix string
	mult   int64
}{
	{"TiB", 1024 * 1024 * 1024 * 1024},
	{"GiB", 1024 * 1024 * 1024},
	{"MiB", 1024 * 1024},
	{"KiB", 1024},
	{"TB", 1000 * 1000 * 1000 * 1000},
	{"GB", 1000 * 1000 * 1000},
	{"MB", 1000 * 1000},
	{"kB", 1000},
	{"B", 1},
}

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	for _, suf := range sizeSuffixes {
		if strings.HasSuffix(s, suf.suffix) {
			numStr := strings.TrimSpace(strings.TrimSuffix(s, suf.suffix))
			f, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("parse size %q: %w", s, err)
			}
			return int64(f * float64(suf.mult)), nil
		}
	}
	return 0, fmt.Errorf("unknown size format %q", s)
}

func parseReclaimedSpace(line string) (int64, error) {
	if idx := strings.Index(line, ":"); idx >= 0 {
		line = line[idx+1:]
	}
	return parseSize(line)
}

// reclaimedFromOutput sums every "Total reclaimed space: X" value in prune output.
func reclaimedFromOutput(out string) int64 {
	var total int64
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Total reclaimed space") {
			if n, err := parseReclaimedSpace(line); err == nil {
				total += n
			}
		}
	}
	return total
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestPrune -v -count=1`
Expected: PASS.

- [ ] **Step 5: Verify build + vet + full test suite**

```bash
go build -o tengiz . && go vet ./... && go test ./... -v -count=1
```

Expected: build succeeds, `go vet` clean, all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: add runtime prune command builders and output parsers"
```

---

### Task 3: Runtime Cleanup Method

Add `Cleanup` to the `runtime.Manager` interface, implement it on `dockerRuntime`, and stub it on `stubManager`. Update every mock in the repo that implements `Manager` so the interface change compiles.

**Files:**
- Modify: `internal/runtime/runtime.go` (interface lines 31-49, stub ~line 121)
- Modify: `internal/runtime/prune.go` (append the `dockerRuntime.Cleanup` implementation)
- Modify: `internal/runtime/cleanup_test.go` (stub test)
- Modify: `internal/cli/root_test.go` (`mockRTForDeploy`, line 98 area)
- Modify: `internal/proxy/proxy_test.go` (`mockRuntime`, line 33 area)
- Modify: `internal/idle/idle_test.go` (`mockRuntime`, line 32 area)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupStats`, `prune*Args`, `runPrune` logic from Task 2.
- Produces: `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupStats, error)` and `func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupStats, error)` returning `&CleanupStats{}, nil`. Task 4 calls this through the `Manager` interface.

- [ ] **Step 1: Write the failing stub test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	stats, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if stats == nil {
		t.Fatal("Cleanup() returned nil stats")
	}
	if stats.Total() != 0 {
		t.Fatalf("Cleanup() stats.Total() = %d, want 0", stats.Total())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: FAIL — `m.Cleanup undefined` (interface has no `Cleanup`), and `stats.Total undefined`.

- [ ] **Step 3: Add `Total` to `CleanupStats` and implement `Cleanup`**

In `internal/runtime/prune.go`, add the `Total` method to `CleanupStats` and the `Cleanup` implementation. Append after the `CleanupStats` struct:

```go
// Total returns the number of resources removed across all categories.
func (s *CleanupStats) Total() int {
	return s.ContainersRemoved + s.ImagesRemoved + s.NetworksRemoved + s.VolumesRemoved + s.BuildCacheRemoved
}
```

Append at the end of `internal/runtime/prune.go`:

```go
// Cleanup prunes the requested categories of Docker resources, protecting
// every Tengiz-managed resource (those carrying the tengiz-app label). In
// dry-run mode it lists candidates instead of deleting anything.
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupStats, error) {
	stats := &CleanupStats{}
	if opts.Containers {
		removed, reclaimed, err := runPrune(ctx, opts.DryRun, pruneContainerArgs(), pruneContainerListArgs())
		if err != nil {
			return stats, err
		}
		stats.ContainersRemoved = removed
		stats.ReclaimedBytes += reclaimed
	}
	if opts.Images {
		removed, reclaimed, err := runPrune(ctx, opts.DryRun, pruneImageArgs(), pruneImageListArgs())
		if err != nil {
			return stats, err
		}
		stats.ImagesRemoved = removed
		stats.ReclaimedBytes += reclaimed
	}
	if opts.Networks {
		removed, reclaimed, err := runPrune(ctx, opts.DryRun, pruneNetworkArgs(), pruneNetworkListArgs())
		if err != nil {
			return stats, err
		}
		stats.NetworksRemoved = removed
		stats.ReclaimedBytes += reclaimed
	}
	if opts.Volumes {
		removed, reclaimed, err := runPrune(ctx, opts.DryRun, pruneVolumeArgs(), pruneVolumeListArgs())
		if err != nil {
			return stats, err
		}
		stats.VolumesRemoved = removed
		stats.ReclaimedBytes += reclaimed
	}
	if opts.BuildCache {
		removed, reclaimed, err := runPrune(ctx, opts.DryRun, pruneBuildCacheArgs(), pruneBuildCacheListArgs())
		if err != nil {
			return stats, err
		}
		stats.BuildCacheRemoved = removed
		stats.ReclaimedBytes += reclaimed
	}
	return stats, nil
}

// runPrune executes either the prune command or (in dry-run mode) the list
// command, returning the number of candidates removed and the reclaimed bytes.
func runPrune(ctx context.Context, dryRun bool, pruneArgs, listArgs []string) (int, int64, error) {
	if dryRun {
		out, err := runCommandOutput(ctx, "docker", listArgs...)
		if err != nil {
			return 0, 0, err
		}
		return countPruneCandidates(out), 0, nil
	}
	out, err := runCommandOutput(ctx, "docker", pruneArgs...)
	if err != nil {
		return 0, 0, err
	}
	return countPruneCandidates(out), reclaimedFromOutput(out), nil
}
```

Note: in dry-run mode, the network/volume list counts include in-use non-Tengiz networks/volumes; the real prune only removes unused ones, so dry-run counts for those two categories are an upper bound. This is intentional — see the CLI help text in Task 4.

- [ ] **Step 4: Add `Cleanup` to the interface and stub**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after `KeepLastNImages`, line 36):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupStats, error)
```

Add the stub method after the stub's `KeepLastNImages` (line 117):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupStats, error) {
	return &CleanupStats{}, nil
}
```

- [ ] **Step 5: Update the three mock implementations**

These mocks must implement the interface or the package tests won't compile.

`internal/cli/root_test.go` — add after the `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupStats, error) { return &runtime.CleanupStats{}, nil }
```

`internal/proxy/proxy_test.go` — add after the `KeepLastNImages` method (line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupStats, error) { return &runtime.CleanupStats{}, nil }
```

`internal/idle/idle_test.go` — add after the `KeepLastNImages` method (line 33):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupStats, error) { return &runtime.CleanupStats{}, nil }
```

(Confirm each of these files imports `github.com/yaso09/tengiz/internal/runtime` — they already do because they use `runtime.Manager` / `runtime.LogOptions`.)

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: PASS.

- [ ] **Step 7: Verify build + vet + full test suite**

```bash
go build -o tengiz . && go vet ./... && go test ./... -v -count=1
```

Expected: build succeeds, `go vet` clean, all tests PASS (including the `TestMockRTForDeployImplementsManager` interface assertion in `internal/cli/root_test.go:103`).

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add Cleanup method to runtime Manager"
```

---

### Task 4: `tengiz cleanup` CLI Command

Expose the cleanup feature as `tengiz cleanup` with per-category flags, safe defaults, `--all`, `--dry-run`, and a summary of removed resources / reclaimed bytes.

**Files:**
- Modify: `internal/cli/root.go` (register command in `init()` ~line 54; add `cleanupCmd` near `rollbackCmd` ~line 1016; add `resolveCleanupOptions` and `formatBytes` helpers near `maskSecret` ~line 1761)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupStats`, `runtime.Cleanup(ctx, opts)`, `runtime.NewDocker()`.
- Produces: `resolveCleanupOptions(containers, images, networks, volumes, buildCache, all bool) runtime.CleanupOptions` and `formatBytes(n int64) string`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsDefined(t *testing.T) {
	for _, flag := range []string{"dry-run", "all", "containers", "images", "networks", "volumes", "build-cache"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestResolveCleanupOptionsDefaults(t *testing.T) {
	opts := resolveCleanupOptions(false, false, false, false, false, false)
	if !opts.Containers || !opts.Images || !opts.BuildCache {
		t.Errorf("defaults should enable containers+images+build-cache, got %+v", opts)
	}
	if opts.Networks || opts.Volumes {
		t.Errorf("defaults should not enable networks/volumes, got %+v", opts)
	}
}

func TestResolveCleanupOptionsExplicit(t *testing.T) {
	opts := resolveCleanupOptions(true, false, false, false, false, false)
	if !opts.Containers || opts.Images || opts.BuildCache {
		t.Errorf("explicit --containers should enable only containers, got %+v", opts)
	}
}

func TestResolveCleanupOptionsAll(t *testing.T) {
	opts := resolveCleanupOptions(false, false, false, false, false, true)
	if !opts.Networks || !opts.Volumes {
		t.Errorf("--all should enable networks+volumes, got %+v", opts)
	}
}

func TestResolveCleanupOptionsDryRunFlagNotAffected(t *testing.T) {
	// DryRun is set by the command from --dry-run, not by resolveCleanupOptions.
	opts := resolveCleanupOptions(false, false, false, false, false, true)
	if opts.DryRun {
		t.Error("resolveCleanupOptions must not set DryRun")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{999, "999B"},
		{1500, "1.5kB"},
		{2500000, "2.5MB"},
		{1200000000, "1.2GB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestResolveCleanupOptions|TestFormatBytes' -v -count=1`
Expected: FAIL — `undefined: cleanupCmd`, `undefined: resolveCleanupOptions`, `undefined: formatBytes`.

- [ ] **Step 3: Register the command**

In `internal/cli/root.go`, add the command to `init()` after `rootCmd.AddCommand(buildLogsCmd)` (line 66):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("all", false, "also prune unused networks and volumes")
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks not managed by Tengiz")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes not managed by Tengiz")
	cleanupCmd.Flags().Bool("build-cache", false, "remove the Docker build cache")
```

- [ ] **Step 4: Add the command, resolver, and formatter**

Add `cleanupCmd` right after the `rollbackCmd` block (after line 1016 in `internal/cli/root.go`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Remove unused Docker resources while protecting every Tengiz-managed app.

Tengiz-managed containers and images carry the "tengiz-app" label and are never
touched. By default cleanup removes stopped non-Tengiz containers, dangling
images, and the Docker build cache. Use --all to also prune unused networks and
volumes, or select categories explicitly. Use --dry-run to report what would be
removed without deleting anything.

Note: dry-run counts for networks/volumes are an upper bound — docker only
removes unused networks/volumes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		networks, _ := cmd.Flags().GetBool("networks")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		all, _ := cmd.Flags().GetBool("all")

		opts := resolveCleanupOptions(containers, images, networks, volumes, buildCache, all)
		opts.DryRun = dryRun

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		stats, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "removed"
		if dryRun {
			verb = "would be removed"
		}
		if opts.Containers {
			fmt.Printf("[tengiz] containers %s: %d\n", verb, stats.ContainersRemoved)
		}
		if opts.Images {
			fmt.Printf("[tengiz] images %s: %d\n", verb, stats.ImagesRemoved)
		}
		if opts.Networks {
			fmt.Printf("[tengiz] networks %s: %d\n", verb, stats.NetworksRemoved)
		}
		if opts.Volumes {
			fmt.Printf("[tengiz] volumes %s: %d\n", verb, stats.VolumesRemoved)
		}
		if opts.BuildCache {
			fmt.Printf("[tengiz] build cache entries %s: %d\n", verb, stats.BuildCacheRemoved)
		}
		if !dryRun {
			fmt.Printf("[tengiz] total reclaimed: %s\n", formatBytes(stats.ReclaimedBytes))
		}
		return nil
	},
}

func resolveCleanupOptions(containers, images, networks, volumes, buildCache, all bool) runtime.CleanupOptions {
	if !containers && !images && !networks && !volumes && !buildCache {
		containers = true
		images = true
		buildCache = true
	}
	if all {
		networks = true
		volumes = true
	}
	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		BuildCache: buildCache,
	}
}
```

Add `formatBytes` next to `maskSecret` (after line 1766 in `internal/cli/root.go`):

```go
func formatBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div := int64(unit)
	exp := 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "kMGTPE"[exp])
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestResolveCleanupOptions|TestFormatBytes' -v -count=1`
Expected: PASS.

- [ ] **Step 6: Verify build + vet + full test suite**

```bash
go build -o tengiz . && go vet ./... && go test ./... -v -count=1
```

Expected: build succeeds, `go vet` clean, all tests PASS.

- [ ] **Step 7: Manual smoke test (optional — requires Docker)**

```bash
./tengiz cleanup --dry-run
./tengiz cleanup
```

Expected: dry-run lists candidate counts with "would be removed"; real run prints "removed" counts and "total reclaimed: XB". Tengiz apps (if any are running) are left untouched.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 5: Documentation

Update the user-facing documentation and the feature tracker to reflect the new command and its safety guarantees.

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the command surface produced in Task 4 (`tengiz cleanup`, flags `--dry-run`, `--all`, `--containers`, `--images`, `--networks`, `--volumes`, `--build-cache`).
- Produces: nothing for later tasks (documentation only).

- [ ] **Step 1: Add a Features bullet in README.md**

In `README.md`, after the "No daemon required" bullet (line 22), insert:

```markdown
- **Docker housekeeping** — `tengiz cleanup` reclaims disk space from stopped containers, dangling images, unused networks/volumes, and the build cache while protecting every running app.
```

- [ ] **Step 2: Add the `tengiz cleanup` section to the CLI Reference**

In `README.md`, after the `### tengiz rollback <app>` section (ends line 236, before `### tengiz domain` at line 238), insert:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space. Tengiz-managed containers and images (labeled `tengiz-app`) are never touched. By default removes stopped non-Tengiz containers, dangling images, and the Docker build cache.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--all` | Also prune unused networks and volumes |
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling images |
| `--networks` | Remove unused networks not managed by Tengiz |
| `--volumes` | Remove unused volumes not managed by Tengiz |
| `--build-cache` | Remove the Docker build cache |

If no category flag is given, `--containers --images --build-cache` is implied. Rollback images are preserved (tagged images are never removed by cleanup).
```

- [ ] **Step 3: Add the command to AGENTS.md**

In `AGENTS.md`, after the `tengiz stop/start/rm  → lifecycle` line (line 47), insert:

```
tengiz cleanup           → reclaim Docker disk space (containers/images/build cache; --all for networks/volumes, --dry-run)
```

- [ ] **Step 4: Mark the feature implemented in the tracker**

In `docs/FUTURES_FEATURES.md`:

1. Change row #6 in the P0 table from `⬜` to `✅` and append the date:

   `| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |`

2. Move the "## Docker Housekeeping (Otomatik Temizlik)" feature entry (lines 377-381) into the "### ✅ Implemented Features (Not Pending)" table by adding a row:

   `| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-20) |`

   and change its `- **Detected:** 2026-07-14` line to `- **Status:** ✅ Implemented (2026-08-20)`.

- [ ] **Step 5: Verify docs build and tests still pass**

```bash
go build -o tengiz . && go vet ./... && go test ./... -v -count=1
```

Expected: build succeeds, `go vet` clean, all tests PASS (docs changes do not affect tests; this confirms nothing else broke).

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**Spec coverage** (FUTURES_FEATURES.md #6 — "Label-based docker system prune. `tengiz cleanup`"):
- Label-based protection → Task 1 (labels on images) + every prune command's `--filter "label!=tengiz-app"` (Task 2).
- `tengiz cleanup` command → Task 4.
- Disk-space reclamation across containers/images/networks/volumes/build cache → Tasks 2-3 (per-category prune). Also covers the core of #56 "Granular Docker Prune Operations" (per-category flags).
- Protection of running Tengiz apps / rollback images → Global Constraints + `pruneImageArgs` omits `-a`; docs task documents it.
- Documentation + tracker → Task 5.
- Gap check: periodic/scheduled cleanup (Coolify's `DockerCleanupJob`) is intentionally out of scope — the spec row for #6 names the `tengiz cleanup` command only. Auto-scheduling is a separate feature (#57 Background Monitoring Scheduler).

**Placeholder scan:** Every step contains complete code or exact commands; no "TBD", "similar to", or "add appropriate handling" placeholders. Verified while writing.

**Type consistency:** `CleanupOptions`/`CleanupStats` fields are defined once in Task 2 and referenced identically in Tasks 3-4 (`ContainersRemoved`, `ImagesRemoved`, `NetworksRemoved`, `VolumesRemoved`, `BuildCacheRemoved`, `ReclaimedBytes`, `Total()`). `Cleanup(ctx, opts) (*CleanupStats, error)` signature matches across interface, stub, docker impl, and all mocks. `resolveCleanupOptions` returns `runtime.CleanupOptions` matching Task 2's struct. `imageLabelArgs(appName, env)` from Task 1 is used by both builders with the same signature. Mock method names (`Cleanup`) are consistent in all three test files.