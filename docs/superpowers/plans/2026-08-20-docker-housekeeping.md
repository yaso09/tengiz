# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped non-Tengiz containers, dangling images, unused networks, unused volumes, build cache) with label-based protection so Tengiz-managed containers are never removed. Disk space is the #1 production issue on single-server deployments.

**Architecture:** A new `runtime.Manager` method `Prune(ctx, opts)` encapsulates all Docker pruning via the existing `os/exec` "docker" pattern. Container pruning uses `--filter label!=tengiz-app` so Tengiz-managed containers (including stopped ones needed for scale-to-zero cold starts) are always preserved. Image pruning is dangling-only by default, which never touches rollback images tagged `tengiz-apps/*`. A new `tengiz cleanup` Cobra command maps CLI flags to `PruneOptions` and prints a summary. Pure helper functions (arg builders + output parsers) keep the docker-exec logic unit-testable without a Docker daemon.

**Tech Stack:** Go 1.26, Cobra (command), `os/exec` (docker CLI), stdlib only. No new external dependencies.

## Global Constraints

- Never prune containers carrying the `tengiz-app` label — stopped Tengiz containers are cold-started on demand by the proxy (`internal/proxy/proxy.go` → `runtime.Start()`). Use `docker container prune -f --filter label!=tengiz-app`
- Never prune images tagged `tengiz-apps/*` — they are the rollback set (`KeepLastNImages`). Dangling-only image pruning is the default and never touches tagged images
- Volumes are opt-in (`--volumes`, default `false`) because pruning volumes can delete persistent data
- `--dry-run` flag previews removals without executing; must be default-safe (listing commands only)
- Follow the existing docker-exec pattern: `exec.CommandContext(ctx, "docker", args...)` + `CombinedOutput()` with wrapped errors, exactly like `RemoveImage`/`KeepLastNImages` in `internal/runtime/cleanup.go`
- The `runtime.Manager` interface change requires updating every implementation: `stubManager`, `mockRTForDeploy` (cli/root_test.go), `mockRuntime` (idle/idle_test.go), `mockRuntime` (proxy/proxy_test.go)
- No new external dependencies
- Follow TDD: write failing test first, then implement, then run `go build ./...` and `go test ./internal/runtime/... ./internal/cli/... -v -count=1`
- Create a feature branch `feat/docker-housekeeping` before starting (AGENTS.md rule)
- Commit after each task with tests passing (AGENTS.md rule)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` | **New.** `PruneOptions`, `PruneResult`, `Prune` on `Manager` interface + `stubManager`, dockerRuntime impl, pure arg builders + output parsers |
| `internal/runtime/runtime.go` | Add `Prune` to `Manager` interface |
| `internal/runtime/prune_test.go` | **New.** Tests for pure helpers + stub |
| `internal/cli/cleanup.go` | **New.** `tengiz cleanup` command + `printPruneResult` |
| `internal/cli/cleanup_test.go` | **New.** Flag default + output tests |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `README.md` | Feature bullet + CLI reference section for `tengiz cleanup` |
| `AGENTS.md` | Add `tengiz cleanup` to commands list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as ✅ Implemented |

---

### Task 1: Add `Prune` to the `runtime.Manager` interface + stub + all mocks

**Files:**
- Modify: `internal/runtime/runtime.go` — interface + stub
- Modify: `internal/cli/root_test.go` — `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go` — `mockRuntime`
- Modify: `internal/proxy/proxy_test.go` — `mockRuntime`
- New: `internal/runtime/prune.go` — types (interface method goes here next to types)

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.Manager.Prune`

- [ ] **Step 1: Write the failing test** (`internal/runtime/prune_test.go`)

```go
package runtime

import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !res.DryRun {
		t.Fatal("Prune() should preserve the DryRun option in the result")
	}
	if len(res.Containers) != 0 || len(res.Images) != 0 {
		t.Fatalf("stub Prune() should return empty result, got %+v", res)
	}
}
```

- [ ] **Step 2: Add types + interface method + stub implementation**

In `internal/runtime/prune.go` (new file):

```go
package runtime

import "context"

// PruneOptions controls which Docker resources are pruned by Prune.
type PruneOptions struct {
	Containers bool // prune stopped containers not managed by Tengiz
	Images     bool // prune dangling images (never touches tengiz-apps/* rollback images)
	Networks   bool // prune unused networks
	Volumes    bool // prune unused volumes (may delete data — opt-in)
	BuildCache bool // prune the Docker build cache
	DryRun     bool // report what would be removed without removing
}

// PruneResult reports what Prune removed (or would remove in dry-run mode).
type PruneResult struct {
	Containers     []string // container IDs removed
	Images         []string // image IDs removed
	Networks       []string // network names removed
	Volumes        []string // volume names removed
	SpaceReclaimed string   // human-readable total reclaimed space ("" if unknown)
	DryRun         bool     // mirrors PruneOptions.DryRun
}
```

In `internal/runtime/runtime.go`, add to the `Manager` interface:

```go
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

Add to `stubManager` (in `internal/runtime/runtime.go`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 3: Add `Prune` to every mock implementing `Manager`** (one method each, identical body)

`internal/cli/root_test.go` (after the `Run` method on `mockRTForDeploy`):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

`internal/idle/idle_test.go` (after the `Run` method on `mockRuntime`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

`internal/proxy/proxy_test.go` (after the `Run` method on `mockRuntime`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 4: Verify**

```bash
go build ./...
go test ./internal/runtime/... ./internal/cli/... ./internal/idle/... ./internal/proxy/... -v -count=1
```

Commit: `feat: add Prune to runtime.Manager interface for docker housekeeping`

---

### Task 2: Implement `dockerRuntime.Prune` + pure helper functions

**Files:**
- Modify: `internal/runtime/prune.go` — dockerRuntime impl + arg builders + output parsers
- Modify: `internal/runtime/prune_test.go` — tests for pure helpers

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult` (from Task 1)
- Produces: `dockerRuntime.Prune`, pure helpers `buildContainerPruneArgs`, `buildImagePruneArgs`, `buildNetworkPruneArgs`, `buildVolumePruneArgs`, `buildBuildCachePruneArgs`, `parsePruneOutput`, `parseReclaimedSpace`, `parseSystemDFReclaimable`, `parseSize`, `formatBytes`, `splitLines`, `filterPrunableNetworks`

- [ ] **Step 1: Write the failing tests** (`internal/runtime/prune_test.go`)

```go
package runtime

import (
	"context"
	"math"
	"reflect"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !res.DryRun {
		t.Fatal("Prune() should preserve the DryRun option in the result")
	}
	if len(res.Containers) != 0 || len(res.Images) != 0 {
		t.Fatalf("stub Prune() should return empty result, got %+v", res)
	}
}

func TestBuildContainerPruneArgs(t *testing.T) {
	got := buildContainerPruneArgs(false)
	want := []string{"container", "prune", "-f", "--filter", "label!=" + labelKey}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildContainerPruneArgs(false) = %v, want %v", got, want)
	}
	got = buildContainerPruneArgs(true)
	want = []string{"container", "ls", "-a",
		"--filter", "label!=" + labelKey,
		"--filter", "status=exited",
		"--format", "{{.ID}}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildContainerPruneArgs(true) = %v, want %v", got, want)
	}
}

func TestBuildImagePruneArgs(t *testing.T) {
	got := buildImagePruneArgs(false)
	if want := []string{"image", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("buildImagePruneArgs(false) = %v, want %v", got, want)
	}
	got = buildImagePruneArgs(true)
	if want := []string{"images", "-q", "--filter", "dangling=true"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("buildImagePruneArgs(true) = %v, want %v", got, want)
	}
}

func TestBuildNetworkPruneArgs(t *testing.T) {
	got := buildNetworkPruneArgs(false)
	if want := []string{"network", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("buildNetworkPruneArgs(false) = %v, want %v", got, want)
	}
	got = buildNetworkPruneArgs(true)
	if want := []string{"network", "ls", "--format", "{{.Name}}"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("buildNetworkPruneArgs(true) = %v, want %v", got, want)
	}
}

func TestBuildVolumePruneArgs(t *testing.T) {
	got := buildVolumePruneArgs(false)
	if want := []string{"volume", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("buildVolumePruneArgs(false) = %v, want %v", got, want)
	}
	got = buildVolumePruneArgs(true)
	if want := []string{"volume", "ls", "-q", "--filter", "dangling=true"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("buildVolumePruneArgs(true) = %v, want %v", got, want)
	}
}

func TestBuildBuildCachePruneArgs(t *testing.T) {
	got := buildBuildCachePruneArgs(false)
	if want := []string{"builder", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("buildBuildCachePruneArgs(false) = %v, want %v", got, want)
	}
	if got := buildBuildCachePruneArgs(true); got != nil {
		t.Fatalf("buildBuildCachePruneArgs(true) = %v, want nil (no dry-run listing)", got)
	}
}

func TestParsePruneOutputContainers(t *testing.T) {
	out := "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 1.2GB\n"
	got := parsePruneOutput(out, "Containers")
	want := []string{"abc123", "def456"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsePruneOutput = %v, want %v", got, want)
	}
}

func TestParsePruneOutputImages(t *testing.T) {
	out := "Deleted Images:\nuntagged: nginx:latest\nuntagged: nginx@sha256:111\ndeleted: sha256:aaa\n\nTotal reclaimed space: 100MB\n"
	got := parsePruneOutput(out, "Images")
	want := []string{"sha256:aaa"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsePruneOutput(images) = %v, want %v", got, want)
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	out := "Deleted Containers:\nabc\n\nTotal reclaimed space: 1.234GB\n"
	if got := parseReclaimedSpace(out); got != "1.234GB" {
		t.Fatalf("parseReclaimedSpace = %q, want %q", got, "1.234GB")
	}
	if got := parseReclaimedSpace("Deleted Containers:\nabc\n"); got != "" {
		t.Fatalf("parseReclaimedSpace (missing) = %q, want empty", got)
	}
}

func TestParseSystemDFReclaimable(t *testing.T) {
	out := `TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE
Images          5         2         1.2GB     800MB (66%)
Containers      3         1         0B        0B (0%)
Local Volumes   2         1         100MB     50MB (50%)
Build Cache     0         0         0B        0B (0%)
`
	got, err := parseSystemDFReclaimable(out)
	if err != nil {
		t.Fatalf("parseSystemDFReclaimable error = %v", err)
	}
	want := 850e6 // 800MB + 0B + 50MB + 0B
	if math.Abs(got-want) > 1 {
		t.Fatalf("parseSystemDFReclaimable = %v, want %v", got, want)
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]float64{
		"0B": 0, "100B": 100, "1kB": 1e3, "1.5MB": 1.5e6,
		"800MB": 8e8, "1.2GB": 1.2e9, "2TB": 2e12,
	}
	for in, want := range cases {
		got, err := parseSize(in)
		if err != nil {
			t.Fatalf("parseSize(%q) error = %v", in, err)
		}
		if math.Abs(got-want) > 1 {
			t.Errorf("parseSize(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[float64]string{
		0: "0B", 500: "500B", 1e3: "1.00kB", 1.5e6: "1.50MB",
		1.2e9: "1.20GB", 2e12: "2.00TB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitLines(t *testing.T) {
	got := splitLines("\nabc\n  def\n\n")
	want := []string{"abc", "def"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitLines = %v, want %v", got, want)
	}
}

func TestFilterPrunableNetworks(t *testing.T) {
	got := filterPrunableNetworks([]string{"bridge", "host", "none", "my-net"})
	want := []string{"my-net"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filterPrunableNetworks = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Implement** (`internal/runtime/prune.go`)

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// PruneOptions controls which Docker resources are pruned by Prune.
type PruneOptions struct {
	Containers bool // prune stopped containers not managed by Tengiz
	Images     bool // prune dangling images (never touches tengiz-apps/* rollback images)
	Networks   bool // prune unused networks
	Volumes    bool // prune unused volumes (may delete data — opt-in)
	BuildCache bool // prune the Docker build cache
	DryRun     bool // report what would be removed without removing
}

// PruneResult reports what Prune removed (or would remove in dry-run mode).
type PruneResult struct {
	Containers     []string // container IDs removed
	Images         []string // image IDs removed
	Networks       []string // network names removed
	Volumes        []string // volume names removed
	SpaceReclaimed string   // human-readable total reclaimed space ("" if unknown)
	DryRun         bool     // mirrors PruneOptions.DryRun
}

// defaultNetworks are created by Docker and never pruned by docker network prune.
var defaultNetworks = map[string]bool{"bridge": true, "host": true, "none": true}

// buildContainerPruneArgs returns docker args to prune stopped containers that
// are NOT managed by Tengiz. The label!=tengiz-app filter protects Tengiz
// containers (including stopped ones used for scale-to-zero cold starts).
func buildContainerPruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"container", "ls", "-a",
			"--filter", "label!=" + labelKey,
			"--filter", "status=exited",
			"--format", "{{.ID}}"}
	}
	return []string{"container", "prune", "-f", "--filter", "label!=" + labelKey}
}

// buildImagePruneArgs returns docker args to prune dangling images. Dangling-only
// pruning never touches tagged tengiz-apps/* rollback images.
func buildImagePruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"images", "-q", "--filter", "dangling=true"}
	}
	return []string{"image", "prune", "-f"}
}

// buildNetworkPruneArgs returns docker args to prune unused networks.
func buildNetworkPruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"network", "ls", "--format", "{{.Name}}"}
	}
	return []string{"network", "prune", "-f"}
}

// buildVolumePruneArgs returns docker args to prune unused volumes.
func buildVolumePruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
	}
	return []string{"volume", "prune", "-f"}
}

// buildBuildCachePruneArgs returns docker args to prune the build cache.
// There is no non-destructive listing command for build cache, so dry-run
// returns nil (build cache is excluded from dry-run previews).
func buildBuildCachePruneArgs(dryRun bool) []string {
	if dryRun {
		return nil
	}
	return []string{"builder", "prune", "-f"}
}

// parsePruneOutput extracts removed item IDs/names from `docker <x> prune -f`
// output. The section begins with "Deleted <header>:" and ends at the blank
// line. Image prune output contains "untagged:" and "deleted:" lines; only the
// "deleted:" lines represent actually removed images.
func parsePruneOutput(output, header string) []string {
	var ids []string
	lines := strings.Split(output, "\n")
	inSection := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Deleted "+header+":") {
			inSection = true
			continue
		}
		if inSection {
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "untagged:") {
				continue
			}
			if strings.HasPrefix(line, "deleted:") {
				ids = append(ids, strings.TrimSpace(strings.TrimPrefix(line, "deleted:")))
				continue
			}
			ids = append(ids, line)
		}
	}
	return ids
}

// parseReclaimedSpace extracts the "Total reclaimed space:" value from prune output.
func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}

// parseSystemDFReclaimable sums the RECLAIMABLE column of `docker system df`.
// The RECLAIMABLE value is the field immediately before the trailing "(NN%)".
func parseSystemDFReclaimable(output string) (float64, error) {
	var total float64
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		last := fields[len(fields)-1]
		if !strings.HasPrefix(last, "(") || !strings.HasSuffix(last, "%)") {
			continue
		}
		reclaimable := fields[len(fields)-2]
		if !strings.HasSuffix(reclaimable, "B") {
			continue
		}
		n, err := parseSize(reclaimable)
		if err != nil {
			continue
		}
		total += n
	}
	return total, nil
}

// parseSize converts a Docker human-readable size ("1.2GB", "800MB", "0B") to bytes.
func parseSize(s string) (float64, error) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		mult   float64
	}{
		{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			num, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			if err != nil {
				return 0, err
			}
			return num * u.mult, nil
		}
	}
	if strings.HasSuffix(s, "B") {
		num, err := strconv.ParseFloat(strings.TrimSuffix(s, "B"), 64)
		if err != nil {
			return 0, err
		}
		return num, nil
	}
	return 0, fmt.Errorf("cannot parse size %q", s)
}

// formatBytes converts bytes to a human-readable string in Docker's style.
func formatBytes(b float64) string {
	switch {
	case b >= 1e12:
		return fmt.Sprintf("%.2fTB", b/1e12)
	case b >= 1e9:
		return fmt.Sprintf("%.2fGB", b/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.2fMB", b/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.2fkB", b/1e3)
	default:
		return fmt.Sprintf("%.0fB", b)
	}
}

// splitLines splits command output into non-empty trimmed lines.
func splitLines(output string) []string {
	var out []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// filterPrunableNetworks removes Docker default networks (never pruned) from a list.
func filterPrunableNetworks(names []string) []string {
	var out []string
	for _, n := range names {
		if !defaultNetworks[n] {
			out = append(out, n)
		}
	}
	return out
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	result := PruneResult{DryRun: opts.DryRun}

	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
		return string(out), nil
	}

	var reclaimed float64

	if opts.Containers {
		out, err := run(buildContainerPruneArgs(opts.DryRun)...)
		if err != nil {
			return result, err
		}
		if opts.DryRun {
			result.Containers = splitLines(out)
		} else {
			result.Containers = parsePruneOutput(out, "Containers")
			if v := parseReclaimedSpace(out); v != "" {
				if n, err := parseSize(v); err == nil {
					reclaimed += n
				}
			}
		}
	}

	if opts.Images {
		out, err := run(buildImagePruneArgs(opts.DryRun)...)
		if err != nil {
			return result, err
		}
		if opts.DryRun {
			result.Images = splitLines(out)
		} else {
			result.Images = parsePruneOutput(out, "Images")
			if v := parseReclaimedSpace(out); v != "" {
				if n, err := parseSize(v); err == nil {
					reclaimed += n
				}
			}
		}
	}

	if opts.Networks {
		out, err := run(buildNetworkPruneArgs(opts.DryRun)...)
		if err != nil {
			return result, err
		}
		if opts.DryRun {
			result.Networks = filterPrunableNetworks(splitLines(out))
		} else {
			result.Networks = parsePruneOutput(out, "Networks")
			if v := parseReclaimedSpace(out); v != "" {
				if n, err := parseSize(v); err == nil {
					reclaimed += n
				}
			}
		}
	}

	if opts.Volumes {
		out, err := run(buildVolumePruneArgs(opts.DryRun)...)
		if err != nil {
			return result, err
		}
		if opts.DryRun {
			result.Volumes = splitLines(out)
		} else {
			result.Volumes = parsePruneOutput(out, "Volumes")
			if v := parseReclaimedSpace(out); v != "" {
				if n, err := parseSize(v); err == nil {
					reclaimed += n
				}
			}
		}
	}

	if opts.BuildCache && !opts.DryRun {
		if out, err := run(buildBuildCachePruneArgs(false)...); err != nil {
			return result, err
		} else if v := parseReclaimedSpace(out); v != "" {
			if n, err := parseSize(v); err == nil {
				reclaimed += n
			}
		}
	}

	if opts.DryRun {
		// Use `docker system df` for the dry-run space estimate (best-effort).
		if out, err := run("system", "df"); err == nil {
			if n, err := parseSystemDFReclaimable(out); err == nil {
				result.SpaceReclaimed = formatBytes(n)
			}
		}
	} else if reclaimed > 0 {
		result.SpaceReclaimed = formatBytes(reclaimed)
	}

	return result, nil
}
```

- [ ] **Step 3: Verify**

```bash
go build ./...
go test ./internal/runtime/... -v -count=1
```

Commit: `feat: implement label-protected docker prune in runtime package`

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- New: `internal/cli/cleanup.go`
- New: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneResult`
- Produces: `cleanupCmd` (registered on `rootCmd`), `printPruneResult(w io.Writer, r runtime.PruneResult)`

- [ ] **Step 1: Write the failing tests** (`internal/cli/cleanup_test.go`)

```go
package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupFlagDefaults(t *testing.T) {
	f := cleanupCmd.Flags()

	if v, _ := f.GetBool("dry-run"); v {
		t.Fatal("dry-run should default to false")
	}
	if v, _ := f.GetBool("containers"); !v {
		t.Fatal("containers should default to true")
	}
	if v, _ := f.GetBool("images"); !v {
		t.Fatal("images should default to true")
	}
	if v, _ := f.GetBool("networks"); !v {
		t.Fatal("networks should default to true")
	}
	if v, _ := f.GetBool("build-cache"); !v {
		t.Fatal("build-cache should default to true")
	}
	if v, _ := f.GetBool("volumes"); v {
		t.Fatal("volumes should default to false (data loss risk)")
	}
}

func TestCleanupRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "cleanup" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestPrintPruneResult(t *testing.T) {
	var buf bytes.Buffer
	printPruneResult(&buf, runtime.PruneResult{
		Containers:     []string{"abc123"},
		Images:         []string{"sha256:aaa"},
		SpaceReclaimed: "1.20GB",
	})
	out := buf.String()
	for _, want := range []string{
		"[tengiz] cleanup (done)",
		"containers: 1 removed",
		"images: 1 removed",
		"space: 1.20GB",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}

	buf.Reset()
	printPruneResult(&buf, runtime.PruneResult{
		Containers:     []string{"abc123"},
		Images:         []string{"sha256:aaa"},
		SpaceReclaimed: "1.20GB",
		DryRun:         true,
	})
	out = buf.String()
	for _, want := range []string{"[tengiz] cleanup (dry-run)", "containers: 1 would be removed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Implement** (`internal/cli/cleanup.go`)

```go
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prune unused Docker resources to reclaim disk space.

Removes stopped containers not managed by Tengiz, dangling images, unused
networks, and the Docker build cache. Tengiz-managed containers are always
protected via the tengiz-app label, so stopped containers can still be
cold-started on demand. Use --dry-run to preview what would be removed first.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		networks, _ := cmd.Flags().GetBool("networks")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Prune(cmd.Context(), runtime.PruneOptions{
			Containers: containers,
			Images:     images,
			Networks:   networks,
			Volumes:    volumes,
			BuildCache: buildCache,
			DryRun:     dryRun,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		printPruneResult(cmd.OutOrStdout(), result)
		return nil
	},
}

// printPruneResult writes a human-readable summary of a PruneResult to w.
func printPruneResult(w io.Writer, r runtime.PruneResult) {
	status, verb := "done", "removed"
	if r.DryRun {
		status, verb = "dry-run", "would be removed"
	}
	fmt.Fprintf(w, "[tengiz] cleanup (%s)\n", status)
	fmt.Fprintf(w, "  containers: %d %s\n", len(r.Containers), verb)
	fmt.Fprintf(w, "  images:     %d %s\n", len(r.Images), verb)
	fmt.Fprintf(w, "  networks:   %d %s\n", len(r.Networks), verb)
	fmt.Fprintf(w, "  volumes:    %d %s\n", len(r.Volumes), verb)
	if r.SpaceReclaimed != "" {
		fmt.Fprintf(w, "  space:      %s\n", r.SpaceReclaimed)
	}
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without removing")
	cleanupCmd.Flags().Bool("containers", true, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", true, "prune dangling images")
	cleanupCmd.Flags().Bool("networks", true, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (may delete data)")
	cleanupCmd.Flags().Bool("build-cache", true, "prune the Docker build cache")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 3: Verify**

```bash
go build ./...
go test ./internal/cli/... -v -count=1
```

Commit: `feat: add tengiz cleanup command for docker housekeeping`

---

### Task 4: Update documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

- [ ] **Step 1: Update `README.md`**

Add a feature bullet in the Features section (after the "Deployment history" bullet, line 20):

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes stopped non-Tengiz containers, dangling images, unused networks/volumes, and build cache with label-based protection so scale-to-zero containers are never removed.
```

Add a CLI reference section after `### tengiz ps` (after line 150):

```markdown
### `tengiz cleanup [--dry-run] [--containers|--images|--networks|--volumes|--build-cache]`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview what would be removed without removing anything (default: `false`) |
| `--containers` | Prune stopped containers not managed by Tengiz (default: `true`) |
| `--images` | Prune dangling images (default: `true`) |
| `--networks` | Prune unused networks (default: `true`) |
| `--volumes` | Prune unused volumes — may delete persistent data (default: `false`) |
| `--build-cache` | Prune the Docker build cache (default: `true`) |

Containers labeled `tengiz-app` are always protected — stopped containers are
kept so they can be cold-started on demand. Dangling-only image pruning never
removes `tengiz-apps/*` rollback images.
```

- [ ] **Step 2: Update `AGENTS.md`**

Add to the CLI commands list (after the `tengiz ps` line):

```
tengiz cleanup [--dry-run] → prune unused Docker resources (label-protected, containers/networks/images/build-cache on, volumes opt-in)
```

- [ ] **Step 3: Update `docs/FUTURES_FEATURES.md`**

Change feature #6 (line 19) from pending to implemented:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Implemented 2026-08-20: label-protected `tengiz cleanup` prunes stopped non-Tengiz containers, dangling images, unused networks/volumes, and build cache. |
```

- [ ] **Step 4: Full verification**

```bash
go build ./...
go vet ./...
go test ./... -count=1
```

Commit: `docs: document tengiz cleanup command and mark docker housekeeping implemented`

---

## Out of Scope

- Granular per-category prune persistence / scheduler (feature #22 Container Retention Policy, #56 Granular Docker Prune Operations)
- `docker system prune`-style combined single call (we run per-category commands to keep label filtering and reporting precise)
- Pruning running containers or images referenced by running containers
- Confirmation prompt (CLI is non-interactive; `--dry-run` is the safety mechanism)