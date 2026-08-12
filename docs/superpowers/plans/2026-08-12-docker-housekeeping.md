# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, unused images, unused networks, unused volumes, build cache) with label-based filters so Tengiz-managed containers are never removed.

**Architecture:** A new `runtime.Manager.Cleanup(ctx, CleanupOptions)` method wraps `docker <object> prune` subcommands with `-f`. Safety comes from the `--filter label!=tengiz-app` guard on container pruning — Tengiz app containers (running **or** stopped, since scale-to-zero keeps stopped containers) keep their labels and therefore are never pruned. Because `docker <object> prune` has no native dry-run, the dry-run path counts prune targets directly via read-only `docker ps/images/network/volume` listing commands plus small client-side pure parsers; the real path runs the prune commands and parses the `Total reclaimed space:` line each one emits for the reclaimed-bytes total. The CLI command is responsible for the interactive confirmation prompt and for per-app image retention (`--keep N`, calling the existing `KeepLastNImages`) before pruning. The periodic background cleanup scheduler (`DockerCleanupJob`) is deliberately out of scope — that maps to a separate feature (#57 Background Monitoring Scheduler).

**Tech Stack:** Go 1.26, Cobra (CLI), stdlib only for the new code (`os/exec`, `regexp`, `strconv`, `encoding/json`, `bufio`). Docker CLI invoked via `os/exec` — no Docker SDK, matching the existing `internal/runtime` convention.

## Global Constraints

- Target feature: **#6 Docker Housekeeping** — the first non-implemented (⬜) entry in the P0 section of `docs/FUTURES_FEATURES.md`
- New feature work must live on a branch: `git checkout -b feat/docker-housekeeping` (AGENTS.md rule)
- No new external dependencies — stdlib `os/exec`, `regexp`, `strconv`, `encoding/json`, `bufio` only
- Tengiz-managed containers must NEVER be pruned: every container prune must include `--filter label!=tengiz-app` (const `labelKey` already defined in `internal/runtime/docker.go:76`)
- Volumes are only pruned when the user explicitly passes `--volumes` (volume data is destructive)
- `--dry-run` may not execute any `docker * prune` command
- All prune commands pass `-f` (the CLI owns the confirmation prompt, Docker never prompts)
- Default image retention is 5 per app (`--keep 5`), matching existing deploy-time `KeepLastNImages(..., 5)` in `internal/cli/root.go`
- `--env` is respected by the retention step via `config.NewStoreWithEnv(dataDir, getEnv(cmd))`
- Dry-run `--all` image count is an upper-bound estimate (`docker images -q` includes every image; documented in summary output)
- Verification commands: `go test ./... -v -count=1` and `go vet ./...` (AGENTS.md)
- One commit per task, message style `feat: <summary>` (matches the existing plan's commit style)
- UI/UX changes: update `README.md` and supporting docs (AGENTS.md rule)
- Periodic scheduling of cleanup is NOT part of this plan (belongs to #57 Background Monitoring Scheduler)

---

## Scope Check

The FUTURES_FEATURES.md spec for #6 has two strands: (a) a `tengiz cleanup` command with label-based pruning, and (b) a *periodic* `DockerCleanupJob`. Strands (a) and (b) are independent subsystems; strand (b) overlaps separate feature #57 (Background Monitoring Scheduler) and is excluded here. Anyone who wants it should write a follow-up plan that reuses `runtime.Manager.Cleanup` from inside a scheduler goroutine — this plan produces the reusable, testable foundation.

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | `CleanupOptions` / `CleanupReport` types; `Manager.Cleanup` interface method; no-op `stubManager.Cleanup` |
| `internal/runtime/cleanup.go` | `dockerRuntime.Cleanup` implementation: prune arg builders, dry-run counters, prune execs, reclaimed-space parsing |
| `internal/runtime/cleanup_test.go` | Tests for the stub, pure parsers, and prune-arg builders |
| `internal/cli/cleanup.go` | `cleanupCmd` (new file): flags, `runCleanupWithManager`, `confirmCleanup`, `cleanupSummary`, `formatBytes` |
| `internal/cli/cleanup_test.go` | CLI tests: registration, flags, summary output, stub-manager dry-run |
| `internal/cli/root_test.go` | Add `Cleanup` method to the `mockRTForDeploy` test double (keeps `Manager` satisfied) |
| `README.md` | New `tengiz cleanup` command section |
| `docs/FUTURES_FEATURES.md` | Mark #6 ✅ Implemented |
| `AGENTS.md` | One-line `tengiz cleanup` entry in the Commands list |

Existing files that already exist and are reused (not modified): `internal/runtime/docker.go` (`labelKey` const, `dockerRuntime` type), `internal/config/store.go` (`NewStoreWithEnv`, `ListApps`), `internal/runtime/cleanup.go` existing `RemoveImage` / `KeepLastNImages` (the new code is **appended** to this same file).

---

### Task 1: Cleanup API — types, Manager method, stub

**Files:**
- Modify: `internal/runtime/runtime.go` — add `CleanupOptions`, `CleanupReport`, `Manager.Cleanup`, `stubManager.Cleanup`
- Modify: `internal/cli/root_test.go` — add `Cleanup` to `mockRTForDeploy` (keeps it satisfying `Manager`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{All bool; Volumes bool; DryRun bool}`, `runtime.CleanupReport{ContainersRemoved int; ImagesRemoved int; NetworksRemoved int; VolumesRemoved int; BytesReclaimed int64; DryRun bool}`, `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{All: true, Volumes: true, DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !report.DryRun {
		t.Errorf("report.DryRun = false, want true")
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 ||
		report.NetworksRemoved != 0 || report.VolumesRemoved != 0 || report.BytesReclaimed != 0 {
		t.Errorf("expected all-zero report, got %+v", report)
	}
}

func TestStubSatisfiesCleanupInterface(t *testing.T) {
	var iface Manager = NewStub()
	if _, err := iface.Cleanup(context.Background(), CleanupOptions{DryRun: true}); err != nil {
		t.Fatalf("stub Cleanup() error = %v", err)
	}
}
```

Add `Cleanup` to the `mockRTForDeploy` test double in `internal/cli/root_test.go` (after the existing `KeepLastNImages` line):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return runtime.CleanupReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... ./internal/cli/... -count=1`

Expected: FAIL with compile errors `undefined: CleanupOptions` and `undefined: runtime.CleanupOptions`

- [ ] **Step 3: Implement the API in `internal/runtime/runtime.go`**

Add the two types after the `RunOptions` struct (around line 30) and the method to the `Manager` interface (after `KeepLastNImages`, before the closing brace):

```go
type CleanupOptions struct {
	All     bool // also prune all unused images (not just dangling ones)
	Volumes bool // also prune unused volumes (destructive)
	DryRun  bool // count targets without removing anything
}

type CleanupReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	BytesReclaimed    int64
	DryRun            bool
}
```

In the `Manager` interface add:

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)
```

Append the stub method at the bottom of the file:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git checkout -b feat/docker-housekeeping
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat: add Cleanup method to runtime.Manager interface"
```

---

### Task 2: Pure parsing helpers for cleanup counting

**Files:**
- Modify: `internal/runtime/cleanup.go` — append `lines`, `isPrunableStatus`, `parseReclaimed`, `sizeMultiplier`, `countPrunableNames`; update imports to add `regexp` and `strconv`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing (pure functions)
- Produces: `lines(s string) []string`, `isPrunableStatus(status string) bool`, `parseReclaimed(output string) int64`, `sizeMultiplier(unit string) (float64, bool)`, `countPrunableNames(all []string, used map[string]bool, excluded map[string]bool) int` (used by Task 3 counters)

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestLines(t *testing.T) {
	got := lines("alpha\n beta \n\n")
	if !equalStrings(got, []string{"alpha", "beta"}) {
		t.Errorf("lines() = %v, want [alpha beta]", got)
	}
}

func TestLinesEmpty(t *testing.T) {
	if got := lines(""); len(got) != 0 {
		t.Errorf("lines(\"\") = %v, want empty", got)
	}
}

func TestIsPrunableStatus(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"Exited (0) 2 minutes ago", true},
		{"Created", true},
		{"Dead", true},
		{"Up 5 minutes", false},
		{"Restarting (1) 1 second ago", false},
		{"Paused", false},
	}
	for _, tt := range tests {
		if got := isPrunableStatus(tt.status); got != tt.want {
			t.Errorf("isPrunableStatus(%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		output string
		want   int64
	}{
		{"Total reclaimed space: 0B", 0},
		{"Total reclaimed space: 2MB", 2 << 20},
		{"Total reclaimed space: 1.5GB", int64(1.5 * (1 << 30))},
		{"Deleted Images:\ndeleted: sha256:abc\nTotal reclaimed space: 512kB", 512 << 10},
		{"nothing relevant here", 0},
		{"", 0},
	}
	for _, tt := range tests {
		if got := parseReclaimed(tt.output); got != tt.want {
			t.Errorf("parseReclaimed(%q) = %d, want %d", tt.output, got, tt.want)
		}
	}
}

func TestSizeMultiplier(t *testing.T) {
	tests := []struct {
		unit string
		mult float64
		ok   bool
	}{
		{"B", 1, true},
		{"KB", 1 << 10, true},
		{"MB", 1 << 20, true},
		{"GB", 1 << 30, true},
		{"TB", 1 << 40, true},
		{"KIB", 1 << 10, true},
		{"MIB", 1 << 20, true},
		{"GIB", 1 << 30, true},
		{"TIB", 1 << 40, true},
		{"XB", 0, false},
	}
	for _, tt := range tests {
		mult, ok := sizeMultiplier(tt.unit)
		if mult != tt.mult || ok != tt.ok {
			t.Errorf("sizeMultiplier(%q) = (%v, %v), want (%v, %v)", tt.unit, mult, ok, tt.mult, tt.ok)
		}
	}
}

func TestCountPrunableNames(t *testing.T) {
	all := []string{"vol-a", "vol-b", "vol-c", "bridge"}
	used := map[string]bool{"vol-a": true}
	excluded := map[string]bool{"bridge": true}
	got := countPrunableNames(all, used, excluded)
	if got != 2 {
		t.Errorf("countPrunableNames() = %d, want 2", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestLines|TestIsPrunableStatus|TestParseReclaimed|TestSizeMultiplier|TestCountPrunableNames" -count=1`

Expected: FAIL with `undefined: lines`, `undefined: isPrunableStatus`, `undefined: parseReclaimed`, `undefined: sizeMultiplier`, `undefined: countPrunableNames`

- [ ] **Step 3: Implement the helpers in `internal/runtime/cleanup.go`**

Update the imports of `internal/runtime/cleanup.go` to:

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

Append the helper functions:

```go
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func isPrunableStatus(status string) bool {
	return strings.HasPrefix(status, "Created") ||
		strings.HasPrefix(status, "Exited") ||
		strings.HasPrefix(status, "Dead")
}

var reclaimedPattern = regexp.MustCompile(`(?i)Total reclaimed space:\s*([0-9.]+)\s*([a-z]*b)`)

func parseReclaimed(output string) int64 {
	m := reclaimedPattern.FindStringSubmatch(output)
	if len(m) != 3 {
		return 0
	}
	value, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	mult, ok := sizeMultiplier(strings.ToUpper(m[2]))
	if !ok {
		return 0
	}
	return int64(value * mult)
}

func sizeMultiplier(unit string) (float64, bool) {
	switch unit {
	case "B":
		return 1, true
	case "KB", "KIB":
		return 1 << 10, true
	case "MB", "MIB":
		return 1 << 20, true
	case "GB", "GIB":
		return 1 << 30, true
	case "TB", "TIB":
		return 1 << 40, true
	default:
		return 0, false
	}
}

func countPrunableNames(all []string, used map[string]bool, excluded map[string]bool) int {
	n := 0
	for _, name := range all {
		if used[name] || excluded[name] {
			continue
		}
		n++
	}
	return n
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestLines|TestIsPrunableStatus|TestParseReclaimed|TestSizeMultiplier|TestCountPrunableNames" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add cleanup parsing helpers"
```

---

### Task 3: `dockerRuntime.Cleanup` implementation

**Files:**
- Modify: `internal/runtime/cleanup.go` — append `runOutput`, prune-arg builders, dry-run counters, prune executors, and `Cleanup`; update imports to add `encoding/json`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `lines`, `isPrunableStatus`, `parseReclaimed`, `countPrunableNames` (Task 2); `labelKey` (docker.go)
- Produces: `dockerRuntime.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)`; arg builders `containerPruneArgs() []string`, `imagePruneArgs(all bool) []string`, `networkPruneArgs() []string`, `volumePruneArgs() []string`, `buildCachePruneArgs() []string` (for other code/docs and tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestContainerPruneArgs(t *testing.T) {
	r := &dockerRuntime{}
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !equalStrings(r.containerPruneArgs(), want) {
		t.Errorf("containerPruneArgs() = %v, want %v", r.containerPruneArgs(), want)
	}
}

func TestImagePruneArgsDefaults(t *testing.T) {
	r := &dockerRuntime{}
	want := []string{"image", "prune", "-f"}
	if !equalStrings(r.imagePruneArgs(false), want) {
		t.Errorf("imagePruneArgs(false) = %v, want %v", r.imagePruneArgs(false), want)
	}
}

func TestImagePruneArgsAll(t *testing.T) {
	r := &dockerRuntime{}
	want := []string{"image", "prune", "-a", "-f"}
	if !equalStrings(r.imagePruneArgs(true), want) {
		t.Errorf("imagePruneArgs(true) = %v, want %v", r.imagePruneArgs(true), want)
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	r := &dockerRuntime{}
	want := []string{"network", "prune", "-f"}
	if !equalStrings(r.networkPruneArgs(), want) {
		t.Errorf("networkPruneArgs() = %v, want %v", r.networkPruneArgs(), want)
	}
}

func TestVolumePruneArgs(t *testing.T) {
	r := &dockerRuntime{}
	want := []string{"volume", "prune", "-f"}
	if !equalStrings(r.volumePruneArgs(), want) {
		t.Errorf("volumePruneArgs() = %v, want %v", r.volumePruneArgs(), want)
	}
}

func TestBuildCachePruneArgs(t *testing.T) {
	r := &dockerRuntime{}
	want := []string{"builder", "prune", "-f"}
	if !equalStrings(r.buildCachePruneArgs(), want) {
		t.Errorf("buildCachePruneArgs() = %v, want %v", r.buildCachePruneArgs(), want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestContainerPruneArgs|TestImagePruneArgs|TestNetworkPruneArgs|TestVolumePruneArgs|TestBuildCachePruneArgs" -count=1`

Expected: FAIL with `undefined: (*dockerRuntime).containerPruneArgs` (and the other arg builders)

- [ ] **Step 3: Implement in `internal/runtime/cleanup.go`**

Update the imports of `internal/runtime/cleanup.go` to:

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

Append the following (beyond the Helper functions from Task 2):

```go
func runOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func (r *dockerRuntime) containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=" + labelKey}
}

func (r *dockerRuntime) imagePruneArgs(all bool) []string {
	if all {
		return []string{"image", "prune", "-a", "-f"}
	}
	return []string{"image", "prune", "-f"}
}

func (r *dockerRuntime) networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func (r *dockerRuntime) volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func (r *dockerRuntime) buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func (r *dockerRuntime) countPrunableContainers(ctx context.Context) (int, error) {
	out, err := runOutput(ctx, "ps", "-a", "--filter", "label!="+labelKey, "--format", "{{.Status}}")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, l := range lines(out) {
		if isPrunableStatus(l) {
			n++
		}
	}
	return n, nil
}

func (r *dockerRuntime) countDanglingImages(ctx context.Context) (int, error) {
	out, err := runOutput(ctx, "images", "--filter", "dangling=true", "-q")
	if err != nil {
		return 0, err
	}
	return len(lines(out)), nil
}

func (r *dockerRuntime) allImageIDs(ctx context.Context) (map[string]bool, error) {
	out, err := runOutput(ctx, "images", "--no-trunc", "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, l := range lines(out) {
		ids[l] = true
	}
	return ids, nil
}

func (r *dockerRuntime) countPrunableImages(ctx context.Context, all bool) (int, error) {
	if !all {
		return r.countDanglingImages(ctx)
	}
	ids, err := r.allImageIDs(ctx)
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (r *dockerRuntime) listNetworkNames(ctx context.Context) ([]string, error) {
	out, err := runOutput(ctx, "network", "ls", "--format", "{{.Name}}")
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

func (r *dockerRuntime) networkInUse(ctx context.Context, name string) (bool, error) {
	out, err := runOutput(ctx, "network", "inspect", "-f", "{{json .Containers}}", name)
	if err != nil {
		return false, err
	}
	s := strings.TrimSpace(out)
	return s != "{}" && s != "null" && s != "", nil
}

func (r *dockerRuntime) usedNetworkNames(ctx context.Context) (map[string]bool, error) {
	names, err := r.listNetworkNames(ctx)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, n := range names {
		inUse, err := r.networkInUse(ctx, n)
		if err != nil {
			continue
		}
		if inUse {
			used[n] = true
		}
	}
	return used, nil
}

func (r *dockerRuntime) countPrunableNetworks(ctx context.Context) (int, error) {
	names, err := r.listNetworkNames(ctx)
	if err != nil {
		return 0, err
	}
	used, err := r.usedNetworkNames(ctx)
	if err != nil {
		return 0, err
	}
	return countPrunableNames(names, used, map[string]bool{"bridge": true, "host": true, "none": true}), nil
}

func (r *dockerRuntime) listVolumes(ctx context.Context) ([]string, error) {
	out, err := runOutput(ctx, "volume", "ls", "--format", "{{.Name}}")
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

func (r *dockerRuntime) usedVolumeNames(ctx context.Context) (map[string]bool, error) {
	out, err := runOutput(ctx, "ps", "-a", "--format", "{{json .Mounts}}")
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, l := range lines(out) {
		var mounts []struct {
			Name string `json:"Name"`
		}
		if err := json.Unmarshal([]byte(l), &mounts); err != nil {
			continue
		}
		for _, m := range mounts {
			if m.Name != "" {
				used[m.Name] = true
			}
		}
	}
	return used, nil
}

func (r *dockerRuntime) countPrunableVolumes(ctx context.Context) (int, error) {
	names, err := r.listVolumes(ctx)
	if err != nil {
		return 0, err
	}
	used, err := r.usedVolumeNames(ctx)
	if err != nil {
		return 0, err
	}
	return countPrunableNames(names, used, nil), nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context) (int64, error) {
	cmd := exec.CommandContext(ctx, "docker", r.containerPruneArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return parseReclaimed(string(out)), nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, all bool) (int64, error) {
	cmd := exec.CommandContext(ctx, "docker", r.imagePruneArgs(all)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return parseReclaimed(string(out)), nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context) (int64, error) {
	cmd := exec.CommandContext(ctx, "docker", r.networkPruneArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return parseReclaimed(string(out)), nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context) (int64, error) {
	cmd := exec.CommandContext(ctx, "docker", r.volumePruneArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return parseReclaimed(string(out)), nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) (int64, error) {
	cmd := exec.CommandContext(ctx, "docker", r.buildCachePruneArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parseReclaimed(string(out)), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	report := CleanupReport{DryRun: opts.DryRun}

	n, err := r.countPrunableContainers(ctx)
	if err != nil {
		return report, err
	}
	report.ContainersRemoved = n
	if !opts.DryRun {
		reclaimed, err := r.pruneContainers(ctx)
		if err != nil {
			return report, err
		}
		report.BytesReclaimed += reclaimed
	}

	n, err = r.countPrunableImages(ctx, opts.All)
	if err != nil {
		return report, err
	}
	report.ImagesRemoved = n
	if !opts.DryRun {
		reclaimed, err := r.pruneImages(ctx, opts.All)
		if err != nil {
			return report, err
		}
		report.BytesReclaimed += reclaimed
	}

	n, err = r.countPrunableNetworks(ctx)
	if err != nil {
		return report, err
	}
	report.NetworksRemoved = n
	if !opts.DryRun {
		reclaimed, err := r.pruneNetworks(ctx)
		if err != nil {
			return report, err
		}
		report.BytesReclaimed += reclaimed
	}

	if opts.Volumes {
		n, err := r.countPrunableVolumes(ctx)
		if err != nil {
			return report, err
		}
		report.VolumesRemoved = n
		if !opts.DryRun {
			reclaimed, err := r.pruneVolumes(ctx)
			if err != nil {
				return report, err
			}
			report.BytesReclaimed += reclaimed
		}
	}

	if !opts.DryRun {
		reclaimed, err := r.pruneBuildCache(ctx)
		if err != nil {
			return report, err
		}
		report.BytesReclaimed += reclaimed
	}

	return report, nil
}
```

- [ ] **Step 4: Run tests and vet to verify they pass**

Run: `go test ./internal/runtime/... -count=1`
Expected: PASS

Run: `go vet ./internal/runtime/...`
Expected: no output (clean)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement dockerRuntime.Cleanup with label-based pruning"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager.Cleanup` and `runtime.CleanupOptions`/`runtime.CleanupReport` (Task 1), `runtime.NewDocker()` / `runtime.NewStub()` (existing), `config.NewStoreWithEnv(dataDir, env)` + `Store.ListApps()` (existing), `runtime.Manager.KeepLastNImages` (existing), `getEnv(cmd)` (root.go)
- Produces: `cleanupCmd` registered on `rootCmd`; `runCleanupWithManager(mgr runtime.Manager, cmd *cobra.Command, opts runtime.CleanupOptions, keep int, force bool) error`; `confirmCleanup() bool`; `cleanupSummary(r runtime.CleanupReport) string`; `formatBytes(n int64) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, name := range []string{"all", "volumes", "dry-run", "keep", "force"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupSummaryDryRun(t *testing.T) {
	r := runtime.CleanupReport{DryRun: true, ContainersRemoved: 3, ImagesRemoved: 5}
	out := cleanupSummary(r)
	if !strings.Contains(out, "would remove") {
		t.Errorf("expected 'would remove' in summary, got: %q", out)
	}
	if !strings.Contains(out, "containers: 3") {
		t.Errorf("expected 'containers: 3' in summary, got: %q", out)
	}
	if !strings.Contains(out, "images:     5") {
		t.Errorf("expected 'images:     5' in summary, got: %q", out)
	}
	if !strings.Contains(out, "reclaimed:") {
		t.Errorf("expected reclaimed line in summary, got: %q", out)
	}
}

func TestCleanupSummaryReal(t *testing.T) {
	r := runtime.CleanupReport{ContainersRemoved: 0, BytesReclaimed: 2 << 30}
	out := cleanupSummary(r)
	if !strings.Contains(out, "removes:") {
		t.Errorf("expected 'removes:' in summary, got: %q", out)
	}
	if !strings.Contains(out, "reclaimed:  2.00GB") {
		t.Errorf("expected 'reclaimed:  2.00GB' in summary, got: %q", out)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{1024, "1.00KB"},
		{1536, "1.50KB"},
		{1 << 20, "1.00MB"},
		{1 << 30, "1.00GB"},
		{1 << 40, "1.00TB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.n); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestRunCleanupWithStub(t *testing.T) {
	dataDir = t.TempDir()
	m := runtime.NewStub()
	out := captureOutput(func() {
		if err := runCleanupWithManager(m, rootCmd, runtime.CleanupOptions{DryRun: true}, 5, true); err != nil {
			t.Fatalf("runCleanupWithManager() error = %v", err)
		}
	})
	if !strings.Contains(out, "[tengiz] cleanup would remove:") {
		t.Errorf("expected dry-run summary, got: %q", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup|TestFormatBytes|TestRunCleanup" -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: cleanupSummary`, `undefined: formatBytes`, `undefined: runCleanupWithManager`

- [ ] **Step 3: Implement `internal/cli/cleanup.go`**

```go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

func init() {
	cleanupCmd.Flags().BoolP("all", "a", false, "also prune all unused images (not just dangling ones)")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused volumes (destructive: deletes volume data)")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Int("keep", 5, "number of image versions to keep per app before pruning")
	cleanupCmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")
	rootCmd.AddCommand(cleanupCmd)
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, networks, volumes, build cache)",
	Long: "Prunes stopped containers, unused images/networks, and the Docker build cache. " +
		"Tengiz-managed containers (labeled tengiz-app) are always kept, including stopped ones. " +
		"Volumes are only pruned when --volumes is given. Use --dry-run to preview.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		keep, _ := cmd.Flags().GetInt("keep")
		force, _ := cmd.Flags().GetBool("force")

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		return runCleanupWithManager(rt, cmd, runtime.CleanupOptions{
			All:     all,
			Volumes: volumes,
			DryRun:  dryRun,
		}, keep, force)
	},
}

func runCleanupWithManager(mgr runtime.Manager, cmd *cobra.Command, opts runtime.CleanupOptions, keep int, force bool) error {
	env := getEnv(cmd)

	if keep > 0 && !opts.DryRun {
		store := config.NewStoreWithEnv(dataDir, env)
		apps, err := store.ListApps()
		if err != nil {
			return err
		}
		for _, app := range apps {
			if err := mgr.KeepLastNImages(context.Background(), app.Name, keep); err != nil {
				log.Printf("[tengiz] warning: keep images for %s: %v", app.Name, err)
			}
		}
	}

	if !force && !opts.DryRun && !confirmCleanup() {
		fmt.Println("[tengiz] cleanup aborted")
		return nil
	}

	report, err := mgr.Cleanup(context.Background(), opts)
	if err != nil {
		return err
	}

	fmt.Println(cleanupSummary(report))
	return nil
}

func confirmCleanup() bool {
	fmt.Print("[tengiz] Remove unused Docker resources? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	ans, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	ans = strings.ToLower(strings.TrimSpace(ans))
	return ans == "y" || ans == "yes"
}

func cleanupSummary(r runtime.CleanupReport) string {
	verb := "removes"
	if r.DryRun {
		verb = "would remove"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[tengiz] cleanup %s:\n", verb)
	fmt.Fprintf(&b, "  containers: %d\n", r.ContainersRemoved)
	fmt.Fprintf(&b, "  images:     %d\n", r.ImagesRemoved)
	fmt.Fprintf(&b, "  networks:   %d\n", r.NetworksRemoved)
	fmt.Fprintf(&b, "  volumes:    %d\n", r.VolumesRemoved)
	fmt.Fprintf(&b, "  reclaimed:  %s\n", formatBytes(r.BytesReclaimed))
	return strings.TrimRight(b.String(), "\n")
}

func formatBytes(n int64) string {
	const (
		kb = 1 << 10
		mb = 1 << 20
		gb = 1 << 30
		tb = 1 << 40
	)
	switch {
	case n >= tb:
		return fmt.Sprintf("%.2fTB", float64(n)/tb)
	case n >= gb:
		return fmt.Sprintf("%.2fGB", float64(n)/gb)
	case n >= mb:
		return fmt.Sprintf("%.2fMB", float64(n)/mb)
	case n >= kb:
		return fmt.Sprintf("%.2fKB", float64(n)/kb)
	default:
		return fmt.Sprintf("%dB", n)
	}
}
```

- [ ] **Step 4: Run all tests and vet to verify they pass**

Run: `go test ./... -count=1`
Expected: PASS

Run: `go vet ./...`
Expected: no output (clean)

- [ ] **Step 5: Manual smoke test (requires Docker but no deployed apps)**

```bash
go build -o tengiz .
./tengiz cleanup --dry-run --force
./tengiz cleanup --force
```

Expected: first prints `[tengiz] cleanup would remove:` with counts and `reclaimed: 0B`; second runs the actual prunes and prints `[tengiz] cleanup removes:` (both without touching any `tengiz-*` containers).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 5: Documentation and feature tracking updates

**Files:**
- Modify: `README.md` — add `tengiz cleanup` section after the `### tengiz rollback <app>` block (ends around line 236)
- Modify: `docs/FUTURES_FEATURES.md` — mark #6 as implemented in both the P0 table and the feature block
- Modify: `AGENTS.md` — add `tengiz cleanup` to the Commands list

**Interfaces:**
- Consumes: nothing (docs)

- [ ] **Step 1: Update `README.md`**

Insert after the rollback section (after the `| app | Application name (required) |` row that ends the `### tengiz rollback <app>` block, before `### tengiz domain`):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to keep disk usage under control on a single-server instance.

| Flag | Description |
|------|-------------|
| `-a`, `--all` | Also prune all unused images (not just dangling ones) |
| `--volumes` | Also prune unused volumes (destructive — deletes volume data) |
| `--dry-run` | Show what would be removed without removing anything |
| `--keep N` | Keep the last N image versions per app (default: 5) |
| `-f`, `--force` | Skip the confirmation prompt |

Tengiz-managed containers (labeled `tengiz-app`) are never removed, even when stopped. Prunes stopped containers that are not managed by Tengiz, dangling or unused images, unused networks, and the Docker build cache. Volumes are only pruned with `--volumes`. Run `tengiz cleanup --dry-run` first to preview.

Example: `tengiz cleanup --all --volumes` performs a full housekeeping pass (with a confirmation prompt unless `-f` is given).
```

- [ ] **Step 2: Update `docs/FUTURES_FEATURES.md`**

Replace the P0 table row (line 19):

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Replace the feature block (lines 377-381) to add the Status line:

```markdown
## Docker Housekeeping (Otomatik Temizlik)
- **Source:** Coolify
- **Description:** `DockerCleanupJob` ile kullanılmayan volume, network, container ve image'leri periyodik temizleme. `CleanupHelperContainersJob` ile yardımcı container'ları temizler.
- **Why add to Tengiz:** Sürekli deploy ve scale-to-zero ortamında atık container/image'ler disk alanını tüketir. Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur. `tengiz cleanup` komutu eklenebilir.
- **Detected:** 2026-07-14
- **Status:** ✅ Implemented (2026-08-12)
```

- [ ] **Step 3: Update `AGENTS.md`**

Add one line to the CLI Commands list, right after the `tengiz rollback` line:

```markdown
tengiz cleanup            → prune unused Docker resources (containers, images, networks, volumes, build cache)
```

- [ ] **Step 4: Verify nothing broke**

Run: `go build -o tengiz .`
Expected: builds successfully

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup and mark feature #6 implemented"
```

---

## Verification & Completion

1. Run `go test ./... -v -count=1` — all tests pass
2. Run `go vet ./...` — clean
3. Manual smoke test on a machine with Docker:
   - `./tengiz cleanup --dry-run` shows counts without removing anything
   - `./tengiz cleanup` prunes and reports reclaimed space
   - `./tengiz cleanup --all --volumes -f` full pass; verify no `tengiz-<app>` containers were removed (`docker ps -a | grep tengiz`)
4. Future work (out of scope, separate plan): periodic scheduling via a background goroutine calling `runtime.Manager.Cleanup` — maps to feature **#57 Background Monitoring Scheduler**

## Self-Review

**Spec coverage.** `docs/FUTURES_FEATURES.md` #6 requires (a) cleanup of unused containers/images/networks/volumes — covered by `dockerRuntime.Cleanup` (containers, images, networks always; volumes behind `--volumes`), (b) build cache — covered by `pruneBuildCache`, (c) label-based protection of Tengiz-managed containers — covered by `--filter label!=tengiz-app` in `containerPruneArgs` and enforced by test `TestContainerPruneArgs`, (d) `tengiz cleanup` command — covered by Task 4. The periodic `DockerCleanupJob` is scoped out (separate feature #57) and documented in the Scope Check + Verification sections, so it is a documented exclusion, not a gap.

**Placeholder scan.** Every code step contains complete compilable code; every test step contains full test bodies and exact commands with expected output. No "TBD"/"similar to Task N"/"handle edge cases" placeholders.

**Type consistency.** `CleanupOptions{All, Volumes, DryRun}` and `CleanupReport{ContainersRemoved, ImagesRemoved, NetworksRemoved, VolumesRemoved, BytesReclaimed, DryRun}` are defined once in Task 1 and referenced identically in Tasks 3 and 4. `runCleanupWithManager(mgr runtime.Manager, cmd *cobra.Command, opts runtime.CleanupOptions, keep int, force bool) error` is called with the same signature in `RunE` (Task 4 Step 3) and the stub test (Task 4 Step 1). `cleanupSummary`/`formatBytes` names match across the test and implementation. The `Cleanup` interface method added to `Manager` in Task 1 is implemented in Task 3 (`dockerRuntime`) and its test double updated in Task 1 Step 1 so the whole repo compiles at every commit boundary.