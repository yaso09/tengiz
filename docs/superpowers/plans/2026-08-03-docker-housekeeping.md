# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, images, networks, build cache, optional volumes) using label-based protection so Tengiz-managed containers are never removed.

**Architecture:** Docker housekeeping lives in the `runtime` package, which already owns all `docker` CLI invocations. A new `Prune(ctx, opts)` method on the `runtime.Manager` interface executes a series of targeted `docker ... prune` sub-commands, each protected by `--filter label!=tengiz-app` so scale-to-zero stopped containers are never touched. Tengiz-built images are excluded from aggressive image pruning via `--filter reference!=tengiz-apps/*`. A new `cleanupCmd` in the CLI package wires flags (`--images`, `--volumes`, `--dry-run`) to `Prune` and prints a per-category report. All docker arg-building and output-parsing logic is factored into pure functions for unit testing without a live Docker daemon, matching the existing `buildLogArgs`/`resourceArgs` test pattern.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (docker CLI), existing `runtime.Manager` interface. No new external dependencies.

## Global Constraints

- All Tengiz-managed containers carry the `tengiz-app=<app>` label (`internal/runtime/docker.go:76`) and must NEVER be pruned — even stopped ones are scale-to-zero cold-start targets
- Every `docker container prune` invocation MUST include `--filter label!=tengiz-app`
- `docker network prune` and `docker volume prune` MUST also include `--filter label!=tengiz-app` for future-proofing
- Aggressive image pruning (`--images`) MUST exclude Tengiz-built images via `--filter reference!=tengiz-apps/*` (image prefix `tengiz-apps/` from `internal/builder/builder.go:61`)
- Volumes are ONLY pruned with the explicit `--volumes` flag — volumes may hold application data
- `--dry-run` MUST NEVER execute a prune command; it only runs `docker ... ls` listing commands and computes candidates
- Default cleanup (no flags) removes: stopped non-Tengiz containers, dangling images, unused networks, Docker build cache
- `docker builder prune` requires BuildKit (Docker 20.10+)
- `PruneOptions.AggressiveImages` maps to `--images`, `PruneOptions.Volumes` to `--volumes`, `PruneOptions.DryRun` to `--dry-run`
- Adding a method to `runtime.Manager` requires updating every implementation: `stubManager` (`internal/runtime/runtime.go`), `mockRTForDeploy` (`internal/cli/root_test.go`), `mockRuntime` (`internal/proxy/proxy_test.go`), `mockRuntime` (`internal/idle/idle_test.go`)
- No new external dependencies
- Verification commands: `go build -o /tmp/tengiz .`, `go test ./... -v -count=1`, `go vet ./...`
- New feature work happens on a feature branch: `feat/docker-housekeeping`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Pure arg builders + parsers, `PruneOptions`/`PruneResult` types, `dockerRuntime.Prune` and per-category helpers |
| `internal/runtime/runtime.go` | Add `Prune` to `Manager` interface + `stubManager.Prune` |
| `internal/runtime/prune_test.go` | Unit tests for all pure helpers + stub `Prune` |
| `internal/cli/cleanup.go` | New `cleanupCmd` (Cobra) + `printPruneResult` output helper |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` + define flags |
| `internal/cli/cleanup_test.go` | CLI tests: registration, flags, output formatting, flag parsing |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

New files: 3. Modified files: 9.

---

### Task 1: Runtime pure helpers (docker arg builders + output parsers)

**Files:**
- Modify: `internal/runtime/cleanup.go` (append helpers; keep existing `RemoveImage`/`KeepLastNImages`)
- Create: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces (pure functions, all in package `runtime`):
  - `func containerPruneArgs(dryRun bool) []string`
  - `func imagePruneArgs(aggressive, dryRun bool) []string`
  - `func networkPruneArgs(dryRun bool) []string`
  - `func volumePruneArgs(dryRun bool) []string`
  - `func buildCachePruneArgs() []string`
  - `func containerImageArgs() []string`
  - `func parseReclaimedSpace(output string) string`
  - `func splitLines(output string) []string`
  - `func unreferencedImages(all, used []string) []string`

- [ ] **Step 1: Create the feature branch**

Run:
```bash
git checkout -b feat/docker-housekeeping
```
Expected: branch created. Continue working on it.

- [ ] **Step 2: Write the failing tests**

Create `internal/runtime/prune_test.go` with the exact content below (place it in `package runtime`):

```go
package runtime

import (
	"reflect"
	"testing"
)

func TestContainerPruneArgs(t *testing.T) {
	real := containerPruneArgs(false)
	wantReal := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(real, wantReal) {
		t.Errorf("containerPruneArgs(false) = %v, want %v", real, wantReal)
	}

	dry := containerPruneArgs(true)
	wantDry := []string{
		"container", "ls", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--filter", "status=dead",
		"--filter", "label!=tengiz-app",
		"--format", "{{.Names}}",
	}
	if !reflect.DeepEqual(dry, wantDry) {
		t.Errorf("containerPruneArgs(true) = %v, want %v", dry, wantDry)
	}
}

func TestImagePruneArgs(t *testing.T) {
	tests := []struct {
		name       string
		aggressive bool
		dryRun     bool
		expected   []string
	}{
		{
			name:     "real dangling",
			expected: []string{"image", "prune", "-f"},
		},
		{
			name:       "real aggressive",
			aggressive: true,
			expected:   []string{"image", "prune", "-f", "-a", "--filter", "reference!=tengiz-apps/*"},
		},
		{
			name:   "dry run dangling",
			dryRun: true,
			expected: []string{"image", "ls", "-f", "dangling=true", "--format", "{{.Repository}}:{{.Tag}}"},
		},
		{
			name:       "dry run aggressive",
			aggressive: true,
			dryRun:     true,
			expected:   []string{"image", "ls", "-a", "--format", "{{.Repository}}:{{.Tag}}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := imagePruneArgs(tt.aggressive, tt.dryRun)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("imagePruneArgs(%v, %v) = %v, want %v", tt.aggressive, tt.dryRun, got, tt.expected)
			}
		})
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	if got, want := networkPruneArgs(false), []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}; !reflect.DeepEqual(got, want) {
		t.Errorf("networkPruneArgs(false) = %v, want %v", got, want)
	}
	if got, want := networkPruneArgs(true), []string{"network", "ls", "--format", "{{.Name}}", "--filter", "label!=tengiz-app"}; !reflect.DeepEqual(got, want) {
		t.Errorf("networkPruneArgs(true) = %v, want %v", got, want)
	}
}

func TestVolumePruneArgs(t *testing.T) {
	if got, want := volumePruneArgs(false), []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}; !reflect.DeepEqual(got, want) {
		t.Errorf("volumePruneArgs(false) = %v, want %v", got, want)
	}
	if got, want := volumePruneArgs(true), []string{"volume", "ls", "--format", "{{.Name}}", "--filter", "label!=tengiz-app"}; !reflect.DeepEqual(got, want) {
		t.Errorf("volumePruneArgs(true) = %v, want %v", got, want)
	}
}

func TestBuildCachePruneArgs(t *testing.T) {
	if got, want := buildCachePruneArgs(), []string{"builder", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildCachePruneArgs() = %v, want %v", got, want)
	}
}

func TestContainerImageArgs(t *testing.T) {
	if got, want := containerImageArgs(), []string{"container", "ls", "-a", "--format", "{{.Image}}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("containerImageArgs() = %v, want %v", got, want)
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "typical container prune output",
			output: "Deleted Containers:\nxyz\n\nTotal reclaimed space: 1.234kB\n",
			want:   "1.234kB",
		},
		{
			name:   "buildx output",
			output: "Total: 2\n\nTotal reclaimed space: 12.5MB\n",
			want:   "12.5MB",
		},
		{
			name:   "no reclaimed line",
			output: "Deleted Containers:\nxyz\n",
			want:   "",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseReclaimedSpace(tt.output); got != tt.want {
				t.Errorf("parseReclaimedSpace() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{"empty", "", nil},
		{"whitespace only", "  \n\t\n", nil},
		{"single line", "app-x\n", []string{"app-x"}},
		{"multiple lines", "a\nb\nc\n", []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.output)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitLines() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnreferencedImages(t *testing.T) {
	all := []string{
		"tengiz-apps/myapp:production-123",
		"tengiz-apps/myapp:production-latest",
		"node:22-alpine",
		"nginx:alpine",
		"<none>:<none>",
	}
	used := []string{"tengiz-apps/myapp:production-latest"}

	got := unreferencedImages(all, used)
	want := []string{"node:22-alpine", "nginx:alpine", "<none>:<none>"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unreferencedImages() = %v, want %v", got, want)
	}

	if got := unreferencedImages(all, all); len(got) != 0 {
		t.Errorf("unreferencedImages(all, all) = %v, want empty", got)
	}
	if got := unreferencedImages(nil, nil); len(got) != 0 {
		t.Errorf("unreferencedImages(nil, nil) = %v, want empty", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestContainerPruneArgs|TestImagePruneArgs|TestNetworkPruneArgs|TestVolumePruneArgs|TestBuildCachePruneArgs|TestContainerImageArgs|TestParseReclaimedSpace|TestSplitLines|TestUnreferencedImages" -v -count=1`

Expected: FAIL with `undefined: containerPruneArgs` (and the other helpers).

- [ ] **Step 4: Write minimal implementation**

Append the following to `internal/runtime/cleanup.go` (below the existing `KeepLastNImages`):

```go
func containerPruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{
			"container", "ls", "-a",
			"--filter", "status=exited",
			"--filter", "status=created",
			"--filter", "status=dead",
			"--filter", "label!=tengiz-app",
			"--format", "{{.Names}}",
		}
	}
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func imagePruneArgs(aggressive, dryRun bool) []string {
	if dryRun {
		if aggressive {
			return []string{"image", "ls", "-a", "--format", "{{.Repository}}:{{.Tag}}"}
		}
		return []string{"image", "ls", "-f", "dangling=true", "--format", "{{.Repository}}:{{.Tag}}"}
	}
	args := []string{"image", "prune", "-f"}
	if aggressive {
		args = append(args, "-a", "--filter", "reference!=tengiz-apps/*")
	}
	return args
}

func networkPruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"network", "ls", "--format", "{{.Name}}", "--filter", "label!=tengiz-app"}
	}
	return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func volumePruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"volume", "ls", "--format", "{{.Name}}", "--filter", "label!=tengiz-app"}
	}
	return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func containerImageArgs() []string {
	return []string{"container", "ls", "-a", "--format", "{{.Image}}"}
}

func parseReclaimedSpace(output string) string {
	const marker = "Total reclaimed space:"
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, marker); idx >= 0 {
			return strings.TrimSpace(line[idx+len(marker):])
		}
	}
	return ""
}

func splitLines(output string) []string {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}

func unreferencedImages(all, used []string) []string {
	usedSet := make(map[string]struct{}, len(used))
	for _, u := range used {
		usedSet[u] = struct{}{}
	}
	var result []string
	for _, img := range all {
		if _, ok := usedSet[img]; ok {
			continue
		}
		if strings.HasPrefix(img, "tengiz-apps/") {
			continue
		}
		result = append(result, img)
	}
	return result
}
```

Note: `strings` is already imported in `internal/runtime/cleanup.go`. Do not add any comments.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestContainerPruneArgs|TestImagePruneArgs|TestNetworkPruneArgs|TestVolumePruneArgs|TestBuildCachePruneArgs|TestContainerImageArgs|TestParseReclaimedSpace|TestSplitLines|TestUnreferencedImages" -v -count=1`

Expected: PASS (all 9 test functions).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add docker prune argument builders and parsers"
```

---

### Task 2: Add `Prune` to the runtime Manager + dockerRuntime implementation + mock updates

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (Manager interface) and `internal/runtime/runtime.go:113-123` (stubManager)
- Modify: `internal/runtime/cleanup.go` (add types + `dockerRuntime` methods)
- Modify: `internal/runtime/prune_test.go` (add stub test)
- Modify: `internal/cli/root_test.go` (add `Prune` to `mockRTForDeploy`)
- Modify: `internal/proxy/proxy_test.go` (add `Prune` to `mockRuntime`)
- Modify: `internal/idle/idle_test.go` (add `Prune` to `mockRuntime`)

**Interfaces:**
- Consumes: `containerPruneArgs`, `imagePruneArgs`, `networkPruneArgs`, `volumePruneArgs`, `buildCachePruneArgs`, `containerImageArgs`, `parseReclaimedSpace`, `splitLines`, `unreferencedImages` (all from Task 1)
- Produces:
  - `type PruneOptions struct { AggressiveImages bool; Volumes bool; DryRun bool }`
  - `type PruneResult struct { Category string; Items []string; ReclaimedSpace string; Command string; DryRun bool }`
  - `Manager.Prune(ctx context.Context, opts PruneOptions) ([]PruneResult, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/prune_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	results, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Prune() results = %v, want empty", results)
	}
}
```

Update the imports at the top of `internal/runtime/prune_test.go` to add `"context"`:

```go
import (
	"context"
	"reflect"
	"testing"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`

Expected: FAIL — either `undefined: PruneOptions` or `undefined: ...Prune` (compilation error).

- [ ] **Step 3: Implement interface, types, stub, and dockerRuntime methods**

**3a.** Add `Prune` to the `Manager` interface in `internal/runtime/runtime.go` (append after the `KeepLastNImages` line):

```go
	Prune(ctx context.Context, opts PruneOptions) ([]PruneResult, error)
```

**3b.** Add `stubManager.Prune` in `internal/runtime/runtime.go` (after `KeepLastNImages`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) ([]PruneResult, error) {
	return nil, nil
}
```

**3c.** Add the types and `dockerRuntime` implementation to `internal/runtime/cleanup.go` (after the Task 1 helpers):

```go
type PruneOptions struct {
	AggressiveImages bool
	Volumes          bool
	DryRun           bool
}

type PruneResult struct {
	Category       string
	Items          []string
	ReclaimedSpace string
	Command        string
	DryRun         bool
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) ([]PruneResult, error) {
	var results []PruneResult
	var res PruneResult
	var err error

	if res, err = r.pruneContainers(ctx, opts.DryRun); err != nil {
		return results, err
	}
	results = append(results, res)

	if res, err = r.pruneImages(ctx, opts.AggressiveImages, opts.DryRun); err != nil {
		return results, err
	}
	results = append(results, res)

	if res, err = r.pruneNetworks(ctx, opts.DryRun); err != nil {
		return results, err
	}
	results = append(results, res)

	if res, err = r.pruneBuildCache(ctx, opts.DryRun); err != nil {
		return results, err
	}
	results = append(results, res)

	if opts.Volumes {
		if res, err = r.pruneVolumes(ctx, opts.DryRun); err != nil {
			return results, err
		}
		results = append(results, res)
	}

	return results, nil
}

func (r *dockerRuntime) execDocker(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *dockerRuntime) runPruneCommand(ctx context.Context, category string, args []string, dryRun bool) (PruneResult, error) {
	res := PruneResult{
		Category: category,
		Command:  "docker " + strings.Join(args, " "),
		DryRun:   dryRun,
	}
	out, err := r.execDocker(ctx, args)
	if err != nil {
		return res, fmt.Errorf("%s: %w\n%s", res.Command, err, out)
	}
	if dryRun {
		res.Items = splitLines(out)
	} else {
		res.ReclaimedSpace = parseReclaimedSpace(out)
	}
	return res, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) (PruneResult, error) {
	return r.runPruneCommand(ctx, "containers", containerPruneArgs(dryRun), dryRun)
}

func (r *dockerRuntime) pruneImages(ctx context.Context, aggressive, dryRun bool) (PruneResult, error) {
	if dryRun && aggressive {
		res := PruneResult{
			Category: "images",
			Command:  "docker " + strings.Join(imagePruneArgs(true, false), " "),
			DryRun:   true,
		}
		allOut, err := r.execDocker(ctx, imagePruneArgs(true, true))
		if err != nil {
			return res, fmt.Errorf("docker image ls: %w\n%s", err, allOut)
		}
		usedOut, err := r.execDocker(ctx, containerImageArgs())
		if err != nil {
			return res, fmt.Errorf("docker container ls: %w\n%s", err, usedOut)
		}
		res.Items = unreferencedImages(splitLines(allOut), splitLines(usedOut))
		return res, nil
	}
	return r.runPruneCommand(ctx, "images", imagePruneArgs(aggressive, dryRun), dryRun)
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) (PruneResult, error) {
	return r.runPruneCommand(ctx, "networks", networkPruneArgs(dryRun), dryRun)
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) (PruneResult, error) {
	return r.runPruneCommand(ctx, "volumes", volumePruneArgs(dryRun), dryRun)
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) (PruneResult, error) {
	if dryRun {
		return PruneResult{
			Category: "build-cache",
			Command:  "docker " + strings.Join(buildCachePruneArgs(), " "),
			DryRun:   true,
		}, nil
	}
	return r.runPruneCommand(ctx, "build-cache", buildCachePruneArgs(), false)
}
```

**3d.** Update the three mock implementations so the package still compiles:

In `internal/cli/root_test.go`, after the `KeepLastNImages` method on `mockRTForDeploy` (line ~99), add:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) ([]runtime.PruneResult, error) { return nil, nil }
```

In `internal/proxy/proxy_test.go`, after the `KeepLastNImages` method on `mockRuntime` (line ~34), add:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) ([]runtime.PruneResult, error) { return nil, nil }
```

In `internal/idle/idle_test.go`, after the `KeepLastNImages` method on `mockRuntime` (line ~33), add:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) ([]runtime.PruneResult, error) { return nil, nil }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -count=1`

Expected: PASS — `TestStubPrune` passes, and the existing `TestStubSatisfiesInterface` (`internal/runtime/runtime_test.go`) still passes with the new interface method.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/prune_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add Prune to runtime Manager"
```

---

### Task 3: CLI `tengiz cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go` (register `cleanupCmd` + flags in `init()`)

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions{AggressiveImages, Volumes, DryRun}`, `runtime.PruneResult{Category, Items, ReclaimedSpace, Command, DryRun}`
- Produces: `var cleanupCmd *cobra.Command`, `func printPruneResult(res runtime.PruneResult)`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go` with the exact content below (package `cli`). It reuses `captureOutput` from `internal/cli/root_test.go`:

```go
package cli

import (
	"strings"
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
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	for _, flag := range []string{"images", "volumes", "dry-run"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestPrintPruneResultReal(t *testing.T) {
	out := captureOutput(func() {
		printPruneResult(runtime.PruneResult{
			Category:       "containers",
			ReclaimedSpace: "1.234kB",
			Command:        "docker container prune -f --filter label!=tengiz-app",
		})
	})
	if !strings.Contains(out, "containers: reclaimed 1.234kB") {
		t.Errorf("real output = %q, want reclaimed line", out)
	}
}

func TestPrintPruneResultRealNoSpace(t *testing.T) {
	out := captureOutput(func() {
		printPruneResult(runtime.PruneResult{
			Category: "networks",
			Command:  "docker network prune -f",
		})
	})
	if !strings.Contains(out, "networks: done") {
		t.Errorf("no-space output = %q, want done line", out)
	}
}

func TestPrintPruneResultDryRunWithItems(t *testing.T) {
	out := captureOutput(func() {
		printPruneResult(runtime.PruneResult{
			Category: "networks",
			Items:    []string{"net-a", "net-b"},
			DryRun:   true,
			Command:  "docker network prune -f",
		})
	})
	if !strings.Contains(out, "networks (dry-run): net-a, net-b") {
		t.Errorf("dry-run output = %q, want item list", out)
	}
}

func TestPrintPruneResultDryRunNoItems(t *testing.T) {
	out := captureOutput(func() {
		printPruneResult(runtime.PruneResult{
			Category: "build-cache",
			DryRun:   true,
			Command:  "docker builder prune -f",
		})
	})
	if !strings.Contains(out, "build-cache (dry-run): would run: docker builder prune -f") {
		t.Errorf("dry-run empty output = %q, want would-run line", out)
	}
}

func TestCleanupFlagsParsed(t *testing.T) {
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()

	var captured runtime.PruneOptions
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		aggressive, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		captured = runtime.PruneOptions{AggressiveImages: aggressive, Volumes: volumes, DryRun: dryRun}
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--images", "--volumes", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !captured.AggressiveImages || !captured.Volumes || !captured.DryRun {
		t.Errorf("captured opts = %+v, want all flags true", captured)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanup|TestPrintPruneResult" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` (and `undefined: printPruneResult`).

- [ ] **Step 3: Write minimal implementation**

**3a.** Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, networks, build cache)",
	Long: `Remove unused Docker resources to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app) are always protected and
never removed, even when stopped for scale-to-zero cold starts.

By default removes: stopped non-Tengiz containers, dangling images,
unused networks, and the Docker build cache.

Flags:
  --images    also remove all unused images (not just dangling),
              excluding Tengiz-built images (tengiz-apps/*)
  --volumes   also remove unused volumes (opt-in — volumes may hold data)
  --dry-run   show what would be removed without removing anything
`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		aggressiveImages, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.PruneOptions{
			AggressiveImages: aggressiveImages,
			Volumes:          volumes,
			DryRun:           dryRun,
		}

		results, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return err
		}

		for _, res := range results {
			printPruneResult(res)
		}

		if dryRun {
			fmt.Println("[tengiz] dry-run complete — nothing was removed")
		} else {
			fmt.Println("[tengiz] cleanup complete")
		}
		return nil
	},
}

func printPruneResult(res runtime.PruneResult) {
	prefix := fmt.Sprintf("[cleanup] %s", res.Category)
	switch {
	case res.DryRun:
		if len(res.Items) > 0 {
			fmt.Printf("%s (dry-run): %s\n", prefix, strings.Join(res.Items, ", "))
		} else {
			fmt.Printf("%s (dry-run): would run: %s\n", prefix, res.Command)
		}
	default:
		if res.ReclaimedSpace != "" {
			fmt.Printf("%s: reclaimed %s\n", prefix, res.ReclaimedSpace)
		} else {
			fmt.Printf("%s: done\n", prefix)
		}
	}
}
```

**3b.** Register the command and flags in `internal/cli/root.go`. In `init()` add the registration after `rootCmd.AddCommand(notificationCmd)` (line ~75) and the flags at the end of `init()` (after line ~88):

```go
	rootCmd.AddCommand(cleanupCmd)
```

```go
	cleanupCmd.Flags().Bool("images", false, "prune all unused images (not just dangling), excluding tengiz-apps images")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused volumes (opt-in: volumes may contain data)")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be pruned without removing anything")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanup|TestPrintPruneResult" -v -count=1`

Expected: PASS (all 6 test functions). Then run the full suite: `go test ./... -count=1` — expected PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation + feature status update

**Files:**
- Modify: `README.md` (add `tengiz cleanup` section after the `volume` section, before `tengiz preview`, line ~304)
- Modify: `AGENTS.md` (add `tengiz cleanup` to the CLI list, after the volume line)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 as implemented)

**Interfaces:** No code changes — this task only edits documentation.

- [ ] **Step 1: Add `tengiz cleanup` to the README CLI Reference**

In `README.md`, insert the following section between the `tengiz volume list <app>` block (ends line ~303) and the `### \`tengiz preview\`` heading (line ~304):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--images` | Also remove all unused images (not just dangling), excluding Tengiz-built images (`tengiz-apps/*`) |
| `--volumes` | Also remove unused volumes (opt-in — volumes may hold data) |
| `--dry-run` | Show what would be removed without removing anything |

Tengiz-managed containers (labeled `tengiz-app`) are always protected and are never removed, even when stopped for scale-to-zero cold starts.

By default, `tengiz cleanup` removes stopped non-Tengiz containers, dangling images, unused networks, and the Docker build cache.

Examples:
```
tengiz cleanup            # safe sweep
tengiz cleanup --dry-run  # preview what would be removed
tengiz cleanup --images --volumes  # aggressive: also unreferenced images + volumes
```
```

- [ ] **Step 2: Add `tengiz cleanup` to AGENTS.md**

In `AGENTS.md`, after the `tengiz volume add/remove/list   → persistent storage volumes` line, add:

```markdown
tengiz cleanup            → prune unused Docker resources (containers/images/networks/build cache; --images --volumes --dry-run)
```

- [ ] **Step 3: Mark feature #6 as implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`, line 19, change:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 4: Full verification**

Run:
```bash
go build -o /tmp/tengiz .
go vet ./...
go test ./... -v -count=1
```

Expected: build succeeds, `go vet` reports no issues, all tests PASS.

Manual smoke test (requires Docker):
```bash
/tmp/tengiz cleanup --dry-run
/tmp/tengiz cleanup --dry-run --images --volumes
```
Expected: prints per-category `(dry-run)` lines and the `[tengiz] dry-run complete — nothing was removed` footer.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup"
```

---

## Self-Review

**Spec coverage.** Feature #6 (Docker Housekeeping) requires: label-based Docker pruning and a `tengiz cleanup` command.
- Label-based protection → Global Constraints + `containerPruneArgs`/`networkPruneArgs`/`volumePruneArgs` (Task 1) always pass `--filter label!=tengiz-app`; aggressive image pruning excludes `tengiz-apps/*` via `--filter reference!=tengiz-apps/*`.
- `tengiz cleanup` command → Task 3 (`cleanupCmd` with `--images`, `--volumes`, `--dry-run`).
- Safe-by-default behavior (volumes opt-in, dry-run never prunes) → Task 1/2.
- Manual command covers `docker system prune` equivalents via targeted `docker container/image/network/builder/volume prune` calls.

No other FUTURES_FEATURES requirement is in scope for this plan.

**Placeholder scan.** Every step contains exact code, exact commands, and expected output. No TBD/TODO items. No "similar to Task N" references — Task 2 restates the full implementation it needs from Task 1.

**Type consistency.** `PruneOptions{AggressiveImages, Volumes, DryRun}` and `PruneResult{Category, Items, ReclaimedSpace, Command, DryRun}` are defined once in Task 2 and used identically in Task 3's CLI tests (`TestCleanupFlagsParsed`, `TestPrintPruneResult*`) and `printPruneResult`. Helper names (`containerPruneArgs`, `imagePruneArgs`, `parseReclaimedSpace`, `splitLines`, `unreferencedImages`, `containerImageArgs`) match across Tasks 1 and 2. Mock `Prune` signatures match the interface exactly in all three test files.
