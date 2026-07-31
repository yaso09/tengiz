# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning unused Docker containers, images, networks, volumes, and build cache while protecting every Tengiz-managed resource (labeled containers and `tengiz-apps/*` images used for rollback).

**Architecture:** Extend the `runtime.Manager` interface with `Prune(ctx, opts)`. The docker implementation runs one `os/exec` docker command per category (`docker container prune`, `docker image prune`, `docker network prune`, `docker volume prune`, `docker builder prune`) and counts removed items by parsing the command output. Protection is enforced with label filters (`label!=tengiz-app`, `label!=tengiz-env`) for containers and a `tengiz-apps/*` prefix check for images. A `--dry-run` mode lists what would be removed using read-only `docker ls`/`docker system df` commands. The prune pipeline is factored into `runPrune(ctx, opts, run)` where `run` is a `dockerRunner` function, so every category command and parser is unit-testable without a Docker daemon.

**Tech Stack:** Go 1.26, `os/exec` (existing pattern — no Docker SDK), Cobra (CLI), existing `runtime.Manager` interface, existing `config.Store` not required (cleanup is env-agnostic and host-wide).

## Global Constraints

- No new external Go dependencies (regexp/stdlib only; reuse existing `os/exec` pattern)
- Containers carrying the `tengiz-app` or `tengiz-env` label must NEVER be pruned — including stopped scale-to-zero containers and preview containers (they all receive these labels via `runtime.Create`/`CreateVersioned`)
- Images tagged `tengiz-apps/*` must NEVER be removed — they are used by rollback (`KeepLastNImages`) and preview deployments
- `tengiz cleanup` is environment-agnostic (no `--env` behavior; it operates on the whole Docker host)
- Command surface: `tengiz cleanup [--dry-run] [--all]`; granular per-category flags are explicitly out of scope (that is P1 #56)
- Default behavior prunes: stopped non-Tengiz containers, dangling images, unused networks, unused volumes, and build cache
- `--all` additionally removes all unused non-`tengiz-apps/*` images (mirrors `docker system prune -a`)
- Verify commands: `go build ./...`, `go vet ./...`, `go test ./... -v -count=1`
- Existing tests must continue to pass; all existing `runtime.Manager` mock implementations must gain the new `Prune` method

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/runtime/runtime.go` | Modify | Add `PruneOptions`, `PruneReport`, `Prune` to `Manager` interface + stub implementation |
| `internal/runtime/cleanup.go` | Modify | Pure command builders (`containerPruneArgs`, `pruneSteps`, ...), output parsers (`countPruned`, `reclaimedSpace`, ...), `runPrune`/`pruneAllImages` pipeline, `dockerRuntime.Prune` |
| `internal/runtime/cleanup_test.go` | Modify | Tests for stub `Prune`, all pure helpers, and `runPrune` with a fake runner |
| `internal/proxy/proxy_test.go` | Modify | Add `Prune` to `mockRuntime` |
| `internal/idle/idle_test.go` | Modify | Add `Prune` to `mockRuntime` |
| `internal/cli/root.go` | Modify | Register `cleanupCmd`, add `--dry-run`/`--all` flags, add `pruneReportString` helper |
| `internal/cli/root_test.go` | Modify | Add `Prune` to `mockRTForDeploy` |
| `internal/cli/cleanup_test.go` | Create | CLI registration/flag/RunE tests + `pruneReportString` output tests |
| `README.md` | Modify | Document `tengiz cleanup` in CLI Reference |
| `AGENTS.md` | Modify | Add `tengiz cleanup` to the CLI command list |

---

### Task 1: Add `Prune` to the `runtime.Manager` interface

Add the public types and interface method so later tasks can call `rt.Prune(ctx, opts)` from any package.

**Files:**
- Modify: `internal/runtime/runtime.go` — add types + interface method + stub impl
- Modify: `internal/proxy/proxy_test.go:35` — add `Prune` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:34` — add `Prune` to `mockRuntime`
- Modify: `internal/cli/root_test.go:100` — add `Prune` to `mockRTForDeploy`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new (only existing `context`, `runtime` package conventions)
- Produces:
  - `type PruneOptions struct { DryRun bool; AllImages bool }`
  - `type PruneReport struct { DryRun bool; Containers, Images, Networks, Volumes int; BuildCache string; Reclaimed map[string]string }`
  - `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`

- [ ] **Step 1: Add the types and interface method to `internal/runtime/runtime.go`**

Add directly after the existing `RunOptions` struct (after line 29):

```go
type PruneOptions struct {
	DryRun    bool
	AllImages bool
}

type PruneReport struct {
	DryRun     bool
	Containers int
	Images     int
	Networks   int
	Volumes    int
	BuildCache string
	Reclaimed  map[string]string
}
```

Add `Prune` to the `Manager` interface (after the `Run(...)` line 48):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

Add the stub implementation (after the existing `Run` stub at line 122):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 2: Update the three existing mock implementations**

`internal/proxy/proxy_test.go` (after line 35):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{DryRun: opts.DryRun}, nil
}
```

`internal/idle/idle_test.go` (after line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{DryRun: opts.DryRun}, nil
}
```

`internal/cli/root_test.go` (after line 100):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 3: Write the failing stub test in `internal/runtime/cleanup_test.go`**

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !report.DryRun {
		t.Error("Prune() did not propagate DryRun to report")
	}
}
```

- [ ] **Step 4: Run the runtime tests to verify the stub test passes and mocks compile**

Run: `go test ./internal/runtime/... ./internal/proxy/... ./internal/idle/... ./internal/cli/... -v -count=1`
Expected: `TestStubPrune` PASS; proxy/idle/cli packages compile (mocks updated); no failures.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: add Prune method to runtime Manager interface"
```

---

### Task 2: Add pure Docker prune helpers to `internal/runtime/cleanup.go`

Add the docker command builders and output parsers as pure functions. These carry all the protection logic (label filters, `tengiz-apps/*` image exclusion) and are fully unit-testable without Docker.

**Files:**
- Modify: `internal/runtime/cleanup.go` — add helpers below the existing `KeepLastNImages`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: existing package constants `labelKey = "tengiz-app"` and `envLabelKey = "tengiz-env"` (defined in `internal/runtime/docker.go:76-77`)
- Produces:
  - `pruneSteps() []pruneStep` where `pruneStep struct { name string; pruneArgs []string; dryRunArgs []string }`
  - `containerPruneArgs(dryRun bool) []string`, `danglingImagePruneArgs(dryRun bool) []string`, `networkPruneArgs(dryRun bool) []string`, `volumePruneArgs(dryRun bool) []string`, `buildCachePruneArgs(dryRun bool) []string`
  - `countPruned(output string) int`, `reclaimedSpace(output string) string`, `countLines(output string) int`, `countPrunableContainers(output string) int`, `filterUnprotectedImages(output string) []string`, `buildCacheSize(output string) string`
  - `const appImagePrefix = "tengiz-apps/"`

- [ ] **Step 1: Add the `regexp` import to `internal/runtime/cleanup.go`**

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)
```

- [ ] **Step 2: Write the failing parser tests in `internal/runtime/cleanup_test.go`**

Add `"strings"` and `"errors"` to the existing imports in `cleanup_test.go` (currently only `context` and `testing`), then append:

```go
func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args = %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q (args: %v)", i, got[i], want[i], got)
		}
	}
}

func TestContainerPruneArgs(t *testing.T) {
	assertArgs(t, containerPruneArgs(false), []string{
		"container", "prune", "-f",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	})
	assertArgs(t, containerPruneArgs(true), []string{
		"container", "ls", "-a",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
		"--format", "{{.ID}}\t{{.Status}}",
	})
}

func TestDanglingImagePruneArgs(t *testing.T) {
	assertArgs(t, danglingImagePruneArgs(false), []string{"image", "prune", "-f"})
	assertArgs(t, danglingImagePruneArgs(true), []string{"image", "ls", "--filter", "dangling=true", "-q"})
}

func TestNetworkPruneArgs(t *testing.T) {
	assertArgs(t, networkPruneArgs(false), []string{"network", "prune", "-f"})
	assertArgs(t, networkPruneArgs(true), []string{"network", "ls", "-q"})
}

func TestVolumePruneArgs(t *testing.T) {
	assertArgs(t, volumePruneArgs(false), []string{"volume", "prune", "-f"})
	assertArgs(t, volumePruneArgs(true), []string{"volume", "ls", "-q"})
}

func TestBuildCachePruneArgs(t *testing.T) {
	assertArgs(t, buildCachePruneArgs(false), []string{"builder", "prune", "-f"})
	assertArgs(t, buildCachePruneArgs(true), []string{"system", "df", "--format", "{{.Type}}|{{.Size}}"})
}

func TestCountPruned(t *testing.T) {
	containerOut := "Deleted Containers:\n8e3f6c2b5a71\na1b2c3d4e5f6\n\nTotal reclaimed space: 123.4MB\n"
	if got := countPruned(containerOut); got != 2 {
		t.Errorf("countPruned(container) = %d, want 2", got)
	}
	imageOut := "Deleted Images:\nuntagged: busybox:latest\ndeleted: sha256:1234567890abcdef\n\nTotal reclaimed space: 456.7MB\n"
	if got := countPruned(imageOut); got != 2 {
		t.Errorf("countPruned(image) = %d, want 2", got)
	}
	emptyOut := "Deleted Networks:\n\nTotal reclaimed space: 0B\n"
	if got := countPruned(emptyOut); got != 0 {
		t.Errorf("countPruned(empty) = %d, want 0", got)
	}
}

func TestReclaimedSpace(t *testing.T) {
	out := "Deleted Containers:\nabc123\n\nTotal reclaimed space: 234.5MB\n"
	if got := reclaimedSpace(out); got != "234.5MB" {
		t.Errorf("reclaimedSpace() = %q, want 234.5MB", got)
	}
	if got := reclaimedSpace("Deleted Networks:\n\nTotal reclaimed space: 0B\n"); got != "0B" {
		t.Errorf("reclaimedSpace(0B) = %q, want 0B", got)
	}
	if got := reclaimedSpace(""); got != "" {
		t.Errorf("reclaimedSpace(empty) = %q, want empty", got)
	}
}

func TestCountLines(t *testing.T) {
	out := "abc123\ndef456\n\nxyz789\n"
	if got := countLines(out); got != 3 {
		t.Errorf("countLines() = %d, want 3", got)
	}
	if got := countLines(""); got != 0 {
		t.Errorf("countLines(empty) = %d, want 0", got)
	}
}

func TestCountPrunableContainers(t *testing.T) {
	out := "9f4c1a2b3c4d\tUp 3 hours\n7a1b2c3d4e5f\tExited (0) 2 hours ago\n6b5a4c3d2e1f\tCreated 5 minutes ago\n5a1b2c3d4e5f\tRestarting (1) 1 second ago\n"
	if got := countPrunableContainers(out); got != 2 {
		t.Errorf("countPrunableContainers() = %d, want 2", got)
	}
}

func TestFilterUnprotectedImages(t *testing.T) {
	out := "tengiz-apps/myapp:1700000000|sha256:aaaa\nbusybox:latest|sha256:bbbb\nubuntu:22.04|sha256:cccc\n<none>:<none>|sha256:dddd\n"
	got := filterUnprotectedImages(out)
	want := []string{"sha256:bbbb", "sha256:cccc"}
	if len(got) != len(want) {
		t.Fatalf("filterUnprotectedImages() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filterUnprotectedImages()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildCacheSize(t *testing.T) {
	out := "Images|2.4GB\nContainers|10MB\nLocal Volumes|0B\nBuild Cache|1.2GB\n"
	if got := buildCacheSize(out); got != "1.2GB" {
		t.Errorf("buildCacheSize() = %q, want 1.2GB", got)
	}
	if got := buildCacheSize("Images|2.4GB\n"); got != "" {
		t.Errorf("buildCacheSize(no build cache) = %q, want empty", got)
	}
}

func TestPruneStepsNames(t *testing.T) {
	steps := pruneSteps()
	want := []string{"containers", "images", "networks", "volumes", "build-cache"}
	if len(steps) != len(want) {
		t.Fatalf("pruneSteps() has %d steps, want %d", len(steps), len(want))
	}
	for i, name := range want {
		if steps[i].name != name {
			t.Errorf("pruneSteps()[%d].name = %q, want %q", i, steps[i].name, name)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestContainerPruneArgs|TestCountPruned|TestReclaimedSpace|TestCountLines|TestCountPrunableContainers|TestFilterUnprotectedImages|TestBuildCacheSize|TestPruneStepsNames' -v -count=1`
Expected: FAIL with "undefined: containerPruneArgs" (or similar) — helpers do not exist yet.

- [ ] **Step 4: Implement the helpers in `internal/runtime/cleanup.go`**

Append below the existing `KeepLastNImages` function (end of file):

```go
const appImagePrefix = "tengiz-apps/"

var reclaimedSpaceRe = regexp.MustCompile(`(?m)Total reclaimed space:\s+(.+)`)

// pruneStep describes one docker cleanup operation. dryRunArgs lists what
// would be removed instead of removing it.
type pruneStep struct {
	name       string
	pruneArgs  []string
	dryRunArgs []string
}

func pruneSteps() []pruneStep {
	return []pruneStep{
		{name: "containers", pruneArgs: containerPruneArgs(false), dryRunArgs: containerPruneArgs(true)},
		{name: "images", pruneArgs: danglingImagePruneArgs(false), dryRunArgs: danglingImagePruneArgs(true)},
		{name: "networks", pruneArgs: networkPruneArgs(false), dryRunArgs: networkPruneArgs(true)},
		{name: "volumes", pruneArgs: volumePruneArgs(false), dryRunArgs: volumePruneArgs(true)},
		{name: "build-cache", pruneArgs: buildCachePruneArgs(false), dryRunArgs: buildCachePruneArgs(true)},
	}
}

// containerPruneArgs prunes stopped containers (or lists them in dry-run mode).
// Containers carrying the tengiz-app or tengiz-env label are always excluded.
func containerPruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"container", "ls", "-a",
			"--filter", fmt.Sprintf("label!=%s", labelKey),
			"--filter", fmt.Sprintf("label!=%s", envLabelKey),
			"--format", "{{.ID}}\t{{.Status}}"}
	}
	return []string{"container", "prune", "-f",
		"--filter", fmt.Sprintf("label!=%s", labelKey),
		"--filter", fmt.Sprintf("label!=%s", envLabelKey)}
}

func danglingImagePruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"image", "ls", "--filter", "dangling=true", "-q"}
	}
	return []string{"image", "prune", "-f"}
}

func networkPruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"network", "ls", "-q"}
	}
	return []string{"network", "prune", "-f"}
}

func volumePruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"volume", "ls", "-q"}
	}
	return []string{"volume", "prune", "-f"}
}

func buildCachePruneArgs(dryRun bool) []string {
	if dryRun {
		return []string{"system", "df", "--format", "{{.Type}}|{{.Size}}"}
	}
	return []string{"builder", "prune", "-f"}
}

// countPruned counts removed items in a docker * prune output.
func countPruned(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Deleted ") && strings.HasSuffix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		count++
	}
	return count
}

// reclaimedSpace extracts the reclaimed space line from a docker prune output.
func reclaimedSpace(output string) string {
	m := reclaimedSpaceRe.FindStringSubmatch(output)
	if len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// countLines counts non-empty lines in output.
func countLines(output string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// countPrunableContainers counts lines in a "{{.ID}}\t{{.Status}}" listing whose
// status is not running. Only used for dry-run previews.
func countPrunableContainers(output string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		status := parts[1]
		if strings.HasPrefix(status, "Up ") || strings.HasPrefix(status, "Restarting ") {
			continue
		}
		count++
	}
	return count
}

// filterUnprotectedImages parses "{{.Repository}}:{{.Tag}}|{{.ID}}" output and
// returns the IDs of images safe to remove: not tagged tengiz-apps/* and not
// intermediate build images (<none>).
func filterUnprotectedImages(output string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		ref := parts[0]
		if strings.HasPrefix(ref, appImagePrefix) {
			continue
		}
		if strings.Contains(ref, "<none>") {
			continue
		}
		ids = append(ids, parts[1])
	}
	return ids
}

// buildCacheSize extracts the Build Cache size from `docker system df` output.
func buildCacheSize(output string) string {
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "Build Cache" {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestContainerPruneArgs|TestCountPruned|TestReclaimedSpace|TestCountLines|TestCountPrunableContainers|TestFilterUnprotectedImages|TestBuildCacheSize|TestPruneStepsNames' -v -count=1`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add docker cleanup command and parser helpers"
```

---

### Task 3: Implement the prune pipeline (`runPrune`) and `dockerRuntime.Prune`

Wire the pure helpers into an executable pipeline. The pipeline takes a `dockerRunner` so tests can fake docker; the production `dockerRuntime.Prune` binds it to `exec.CommandContext`.

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `dockerRunner`, `execDocker`, `runPrune`, `pruneAllImages`, `dockerRuntime.Prune`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: all helpers from Task 2 (`pruneSteps`, `containerPruneArgs`, ..., `countPruned`, `reclaimedSpace`, `countLines`, `countPrunableContainers`, `filterUnprotectedImages`, `buildCacheSize`); `PruneOptions`/`PruneReport` from Task 1
- Produces:
  - `type dockerRunner func(ctx context.Context, args ...string) (string, error)`
  - `execDocker(ctx context.Context, args ...string) (string, error)`
  - `runPrune(ctx context.Context, opts PruneOptions, run dockerRunner) (PruneReport, error)`
  - `pruneAllImages(ctx context.Context, dryRun bool, run dockerRunner, report *PruneReport) error`
  - `(*dockerRuntime).Prune(ctx, opts) (PruneReport, error)`

- [ ] **Step 1: Write the failing `runPrune` tests in `internal/runtime/cleanup_test.go`**

Append (imports `strings` and `errors` were added in Task 2, Step 2):

```go
func TestRunPruneReal(t *testing.T) {
	outputs := map[string]string{
		"container prune -f --filter label!=tengiz-app --filter label!=tengiz-env": "Deleted Containers:\n8e3f6c2b5a71\na1b2c3d4e5f6\n\nTotal reclaimed space: 123.4MB\n",
		"image prune -f":       "Deleted Images:\nuntagged: busybox:latest\ndeleted: sha256:1234567890abcdef\n\nTotal reclaimed space: 456.7MB\n",
		"network prune -f":     "Deleted Networks:\n\nTotal reclaimed space: 0B\n",
		"volume prune -f":      "Deleted Volumes:\nvoldata1234567890\n\nTotal reclaimed space: 0B\n",
		"builder prune -f":     "Total reclaimed space: 1.1GB\n",
	}
	var calls [][]string
	run := func(ctx context.Context, args ...string) (string, error) {
		calls = append(calls, args)
		return outputs[strings.Join(args, " ")], nil
	}

	report, err := runPrune(context.Background(), PruneOptions{}, run)
	if err != nil {
		t.Fatalf("runPrune() error = %v", err)
	}
	if report.DryRun {
		t.Error("report.DryRun = true, want false")
	}
	if report.Containers != 2 {
		t.Errorf("Containers = %d, want 2", report.Containers)
	}
	if report.Images != 2 {
		t.Errorf("Images = %d, want 2", report.Images)
	}
	if report.Networks != 0 {
		t.Errorf("Networks = %d, want 0", report.Networks)
	}
	if report.Volumes != 1 {
		t.Errorf("Volumes = %d, want 1", report.Volumes)
	}
	if report.BuildCache != "1.1GB" {
		t.Errorf("BuildCache = %q, want 1.1GB", report.BuildCache)
	}
	if report.Reclaimed["containers"] != "123.4MB" {
		t.Errorf("Reclaimed[containers] = %q, want 123.4MB", report.Reclaimed["containers"])
	}
	if len(calls) != 5 {
		t.Fatalf("expected 5 docker calls, got %d: %v", len(calls), calls)
	}
	assertArgs(t, calls[0], []string{"container", "prune", "-f", "--filter", "label!=tengiz-app", "--filter", "label!=tengiz-env"})
	assertArgs(t, calls[4], []string{"builder", "prune", "-f"})
}

func TestRunPruneDryRun(t *testing.T) {
	outputs := map[string]string{
		"container ls -a --filter label!=tengiz-app --filter label!=tengiz-env --format {{.ID}}\t{{.Status}}": "9f4c1a2b3c4d\tUp 3 hours\n7a1b2c3d4e5f\tExited (0) 2 hours ago\n",
		"image ls --filter dangling=true -q": "abc123def456\n",
		"network ls -q":                     "xyz789\nabc123\n",
		"volume ls -q":                      "",
		"system df --format {{.Type}}|{{.Size}}": "Images|2.4GB\nContainers|10MB\nLocal Volumes|0B\nBuild Cache|1.2GB\n",
	}
	var calls [][]string
	run := func(ctx context.Context, args ...string) (string, error) {
		calls = append(calls, args)
		return outputs[strings.Join(args, " ")], nil
	}

	report, err := runPrune(context.Background(), PruneOptions{DryRun: true}, run)
	if err != nil {
		t.Fatalf("runPrune() error = %v", err)
	}
	if !report.DryRun {
		t.Error("report.DryRun = false, want true")
	}
	if report.Containers != 1 {
		t.Errorf("Containers = %d, want 1 (only the Exited container)", report.Containers)
	}
	if report.Images != 1 {
		t.Errorf("Images = %d, want 1", report.Images)
	}
	if report.Networks != 2 {
		t.Errorf("Networks = %d, want 2", report.Networks)
	}
	if report.Volumes != 0 {
		t.Errorf("Volumes = %d, want 0", report.Volumes)
	}
	if report.BuildCache != "1.2GB" {
		t.Errorf("BuildCache = %q, want 1.2GB", report.BuildCache)
	}
	assertArgs(t, calls[0], []string{"container", "ls", "-a", "--filter", "label!=tengiz-app", "--filter", "label!=tengiz-env", "--format", "{{.ID}}\t{{.Status}}"})
	assertArgs(t, calls[4], []string{"system", "df", "--format", "{{.Type}}|{{.Size}}"})
}

func TestRunPruneAllImages(t *testing.T) {
	outputs := map[string]string{
		"container prune -f --filter label!=tengiz-app --filter label!=tengiz-env": "Deleted Containers:\n\nTotal reclaimed space: 0B\n",
		"network prune -f": "Deleted Networks:\n\nTotal reclaimed space: 0B\n",
		"volume prune -f":  "Deleted Volumes:\n\nTotal reclaimed space: 0B\n",
		"builder prune -f": "Total reclaimed space: 0B\n",
		"image ls --format {{.Repository}}:{{.Tag}}|{{.ID}}": "tengiz-apps/myapp:1700000000|sha256:aaaa\nbusybox:latest|sha256:bbbb\nubuntu:22.04|sha256:cccc\n",
		"image rm -f sha256:bbbb": "Deleted Images:\ndeleted: sha256:bbbb\n\nTotal reclaimed space: 25MB\n",
		"image rm -f sha256:cccc": "Deleted Images:\ndeleted: sha256:cccc\n\nTotal reclaimed space: 300MB\n",
	}
	var calls [][]string
	run := func(ctx context.Context, args ...string) (string, error) {
		calls = append(calls, args)
		return outputs[strings.Join(args, " ")], nil
	}

	report, err := runPrune(context.Background(), PruneOptions{AllImages: true}, run)
	if err != nil {
		t.Fatalf("runPrune() error = %v", err)
	}
	if report.Images != 2 {
		t.Errorf("Images = %d, want 2 (sha256:bbbb and sha256:cccc)", report.Images)
	}
	if report.Reclaimed["images"] != "300MB" {
		t.Errorf("Reclaimed[images] = %q, want 300MB", report.Reclaimed["images"])
	}
	if len(calls) != 7 {
		t.Fatalf("expected 7 docker calls (4 prune + image ls + 2 rm), got %d: %v", len(calls), calls)
	}
	assertArgs(t, calls[4], []string{"image", "ls", "--format", "{{.Repository}}:{{.Tag}}|{{.ID}}"})
	assertArgs(t, calls[5], []string{"image", "rm", "-f", "sha256:bbbb"})
	assertArgs(t, calls[6], []string{"image", "rm", "-f", "sha256:cccc"})
}

func TestRunPruneBuildCacheErrorIsNonFatal(t *testing.T) {
	outputs := map[string]string{
		"container prune -f --filter label!=tengiz-app --filter label!=tengiz-env": "Deleted Containers:\n\nTotal reclaimed space: 0B\n",
		"image prune -f":   "Deleted Images:\n\nTotal reclaimed space: 0B\n",
		"network prune -f": "Deleted Networks:\n\nTotal reclaimed space: 0B\n",
		"volume prune -f":  "Deleted Volumes:\n\nTotal reclaimed space: 0B\n",
	}
	run := func(ctx context.Context, args ...string) (string, error) {
		if strings.Join(args, " ") == "builder prune -f" {
			return "docker: 'builder' is not a docker command\n", errors.New("exit status 1")
		}
		return outputs[strings.Join(args, " ")], nil
	}

	report, err := runPrune(context.Background(), PruneOptions{}, run)
	if err != nil {
		t.Fatalf("runPrune() error = %v, want nil (build cache failure must not abort cleanup)", err)
	}
	if report.BuildCache != "0B" {
		t.Errorf("BuildCache = %q, want 0B (default when builder unavailable)", report.BuildCache)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestRunPrune' -v -count=1`
Expected: FAIL with "undefined: runPrune".

- [ ] **Step 3: Implement `runPrune`, `pruneAllImages`, `execDocker`, and `dockerRuntime.Prune` in `internal/runtime/cleanup.go`**

Append to `internal/runtime/cleanup.go`:

```go
// dockerRunner abstracts `docker <args...>` for testability.
type dockerRunner func(ctx context.Context, args ...string) (string, error)

func execDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return runPrune(ctx, opts, execDocker)
}

func runPrune(ctx context.Context, opts PruneOptions, run dockerRunner) (PruneReport, error) {
	report := PruneReport{DryRun: opts.DryRun}
	report.Reclaimed = make(map[string]string)

	for _, step := range pruneSteps() {
		if opts.AllImages && step.name == "images" {
			continue
		}
		args := step.pruneArgs
		if opts.DryRun {
			args = step.dryRunArgs
		}
		out, err := run(ctx, args...)
		if err != nil {
			if step.name == "build-cache" {
				log.Printf("[runtime] cleanup: build cache prune failed: %v", err)
				report.BuildCache = "0B"
				continue
			}
			return report, fmt.Errorf("%s prune: %w\n%s", step.name, err, out)
		}
		switch step.name {
		case "containers":
			if opts.DryRun {
				report.Containers = countPrunableContainers(out)
			} else {
				report.Containers = countPruned(out)
				report.Reclaimed["containers"] = reclaimedSpace(out)
			}
		case "images":
			if opts.DryRun {
				report.Images = countLines(out)
			} else {
				report.Images = countPruned(out)
				report.Reclaimed["images"] = reclaimedSpace(out)
			}
		case "networks":
			if opts.DryRun {
				report.Networks = countLines(out)
			} else {
				report.Networks = countPruned(out)
				report.Reclaimed["networks"] = reclaimedSpace(out)
			}
		case "volumes":
			if opts.DryRun {
				report.Volumes = countLines(out)
			} else {
				report.Volumes = countPruned(out)
				report.Reclaimed["volumes"] = reclaimedSpace(out)
			}
		case "build-cache":
			if opts.DryRun {
				report.BuildCache = buildCacheSize(out)
			} else {
				report.BuildCache = reclaimedSpace(out)
				if report.BuildCache == "" {
					report.BuildCache = "0B"
				}
				report.Reclaimed["build-cache"] = report.BuildCache
			}
		}
	}

	if opts.AllImages {
		if err := pruneAllImages(ctx, opts.DryRun, run, &report); err != nil {
			return report, err
		}
	}
	return report, nil
}

// pruneAllImages removes every image except those tagged tengiz-apps/* (kept for
// rollback) and intermediate <none> images. Requires docker to have already
// pruned dangling images.
func pruneAllImages(ctx context.Context, dryRun bool, run dockerRunner, report *PruneReport) error {
	lsArgs := []string{"image", "ls", "--format", "{{.Repository}}:{{.Tag}}|{{.ID}}"}
	out, err := run(ctx, lsArgs...)
	if err != nil {
		return fmt.Errorf("image list: %w\n%s", err, out)
	}
	ids := filterUnprotectedImages(out)
	if dryRun {
		report.Images += len(ids)
		return nil
	}
	for _, id := range ids {
		rmOut, rmErr := run(ctx, "image", "rm", "-f", id)
		if rmErr != nil {
			log.Printf("[runtime] cleanup: failed to remove image %s: %v", id, rmErr)
			continue
		}
		if r := reclaimedSpace(rmOut); r != "" {
			report.Reclaimed["images"] = r
		}
		report.Images++
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: all runtime tests PASS (including the four new `TestRunPrune*` tests).

- [ ] **Step 5: Run the full test suite and vet**

Run: `go test ./... -v -count=1 && go vet ./...`
Expected: all packages PASS, `go vet` reports nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement docker resource pruning pipeline"
```

---

### Task 4: Add the `tengiz cleanup` CLI command

Expose the prune pipeline as a first-class CLI command with `--dry-run` and `--all` flags and a human-readable report.

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, `pruneReportString`, registration and flags
- Create: `internal/cli/cleanup_test.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.PruneOptions{DryRun, AllImages bool}`, `runtime.PruneReport`, `runtime.Manager.Prune(ctx, opts) (PruneReport, error)`
- Produces: `cleanupCmd *cobra.Command` (Use: `cleanup`, flags `--dry-run`, `--all`), `pruneReportString(report runtime.PruneReport) string`

- [ ] **Step 1: Register the command and flags in `internal/cli/root.go`**

Add `cleanupCmd` to the command list in `init()` — insert `rootCmd.AddCommand(cleanupCmd)` after `rootCmd.AddCommand(runCmd)` (line 67):

```go
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(cleanupCmd)
```

Add the flags in `init()` after the `webhookCmd` flags block (after line 88):

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("all", false, "also remove all unused images (keeps tengiz-apps/* images for rollback)")
```

- [ ] **Step 2: Write the failing CLI tests in `internal/cli/cleanup_test.go`**

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
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	if cleanupCmd.Flags().Lookup("dry-run") == nil {
		t.Error("cleanup missing --dry-run flag")
	}
	if cleanupCmd.Flags().Lookup("all") == nil {
		t.Error("cleanup missing --all flag")
	}
}

func TestCleanupRunEParsesFlags(t *testing.T) {
	var gotDryRun, gotAll bool
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		gotDryRun, _ = cmd.Flags().GetBool("dry-run")
		gotAll, _ = cmd.Flags().GetBool("all")
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !gotDryRun {
		t.Error("--dry-run not parsed")
	}
	if !gotAll {
		t.Error("--all not parsed")
	}
}

func TestPruneReportStringDryRun(t *testing.T) {
	report := runtime.PruneReport{
		DryRun:     true,
		Containers: 2,
		Images:     1,
		Networks:   0,
		Volumes:    1,
		BuildCache: "1.2GB",
	}
	out := pruneReportString(report)
	for _, want := range []string{
		"docker cleanup (dry-run)",
		"containers: 2 would be removed",
		"images: 1 would be removed",
		"networks: 0 would be removed",
		"volumes: 1 would be removed",
		"build cache: 1.2GB present",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
}

func TestPruneReportStringReal(t *testing.T) {
	report := runtime.PruneReport{
		Containers: 2,
		Images:     1,
		Networks:   0,
		Volumes:    1,
		BuildCache: "1.1GB",
		Reclaimed: map[string]string{
			"containers":  "123.4MB",
			"images":      "456.7MB",
			"build-cache": "1.1GB",
		},
	}
	out := pruneReportString(report)
	for _, want := range []string{
		"cleanup complete",
		"containers removed: 2",
		"images removed: 1",
		"networks removed: 0",
		"volumes removed: 1",
		"build cache: 1.1GB reclaimed",
		"123.4MB (containers)",
		"456.7MB (images)",
		"1.1GB (build-cache)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("real output missing %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestPruneReportString' -v -count=1`
Expected: FAIL — `cleanupCmd` and `pruneReportString` are undefined.

- [ ] **Step 4: Implement `cleanupCmd` and `pruneReportString` in `internal/cli/root.go`**

Add the command definition after `runCmd` (i.e., after line 1162, before `var gitCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Remove unused Docker containers, images, networks, volumes, and build cache
to reclaim disk space.

Tengiz-managed resources are always protected:
  - Containers with the tengiz-app label are never removed, even when stopped
    by scale-to-zero or running as preview deployments.
  - Images tagged tengiz-apps/* are kept for rollback.

Use --dry-run to preview what would be removed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		allImages, _ := cmd.Flags().GetBool("all")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Prune(cmd.Context(), runtime.PruneOptions{DryRun: dryRun, AllImages: allImages})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		fmt.Print(pruneReportString(report))
		return nil
	},
}
```

Add the report formatter after `cleanupCmd`:

```go
func pruneReportString(report runtime.PruneReport) string {
	var b strings.Builder
	if report.DryRun {
		b.WriteString("[tengiz] docker cleanup (dry-run)\n")
		b.WriteString(fmt.Sprintf("  containers: %d would be removed\n", report.Containers))
		b.WriteString(fmt.Sprintf("  images: %d would be removed\n", report.Images))
		b.WriteString(fmt.Sprintf("  networks: %d would be removed\n", report.Networks))
		b.WriteString(fmt.Sprintf("  volumes: %d would be removed\n", report.Volumes))
		b.WriteString(fmt.Sprintf("  build cache: %s present\n", report.BuildCache))
		return b.String()
	}

	b.WriteString("[tengiz] docker cleanup complete\n")
	b.WriteString(fmt.Sprintf("  containers removed: %d\n", report.Containers))
	b.WriteString(fmt.Sprintf("  images removed: %d\n", report.Images))
	b.WriteString(fmt.Sprintf("  networks removed: %d\n", report.Networks))
	b.WriteString(fmt.Sprintf("  volumes removed: %d\n", report.Volumes))
	b.WriteString(fmt.Sprintf("  build cache: %s reclaimed\n", report.BuildCache))

	var reclaimed []string
	for _, cat := range []string{"containers", "images", "networks", "volumes", "build-cache"} {
		if v := report.Reclaimed[cat]; v != "" && v != "0B" {
			reclaimed = append(reclaimed, fmt.Sprintf("%s (%s)", v, cat))
		}
	}
	if len(reclaimed) > 0 {
		b.WriteString(fmt.Sprintf("  total reclaimed space: %s\n", strings.Join(reclaimed, ", ")))
	}
	return b.String()
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestPruneReportString' -v -count=1`
Expected: all PASS.

- [ ] **Step 6: Run the full test suite, vet, and a manual smoke check**

Run: `go test ./... -v -count=1 && go vet ./... && go build -o tengiz .`
Expected: all PASS, vet clean, binary builds.

Then run the command against a live Docker host (if available) to verify real output:

```bash
./tengiz cleanup --dry-run
```

Expected output resembling (exact counts depend on the host):

```
[tengiz] docker cleanup (dry-run)
  containers: 0 would be removed
  images: 0 would be removed
  networks: 0 would be removed
  volumes: 0 would be removed
  build cache: 1.2GB present
```

Note: if Docker is not installed in this environment, skip the smoke check and rely on the unit tests.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 5: Document the `tengiz cleanup` command

Update user-facing documentation so the command surface stays in sync.

**Files:**
- Modify: `README.md` — add a CLI Reference section for `tengiz cleanup`
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI command list

**Interfaces:**
- Consumes: the `tengiz cleanup [--dry-run] [--all]` command surface from Task 4

- [ ] **Step 1: Add the CLI Reference section to `README.md`**

Insert after the `### tengiz rollback <app>` section (after line 237, immediately before `### tengiz domain`):

```markdown
### `tengiz cleanup [--dry-run] [--all]`

Remove unused Docker resources to reclaim disk space: stopped containers, dangling images, unused networks, unused volumes, and the build cache.

Tengiz-managed resources are always protected:
- Containers with the `tengiz-app` label are never removed, even when stopped by scale-to-zero or running as preview deployments.
- Images tagged `tengiz-apps/*` are kept for rollback.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--all` | Also remove all unused images (keeps `tengiz-apps/*` images) |

Examples:
```
tengiz cleanup              # remove unused Docker resources
tengiz cleanup --dry-run    # preview what would be removed
tengiz cleanup --all        # also remove all unused non-app images
```
```

- [ ] **Step 2: Add the command to the `AGENTS.md` CLI list**

Insert after the `tengiz ps` line in the CLI code block:

```markdown
tengiz cleanup [--dry-run] [--all] → prune unused Docker resources (protects tengiz-* containers and images)
```

- [ ] **Step 3: Verify no Markdown/code issues and commit**

Run: `git diff --stat`
Expected: `README.md` and `AGENTS.md` show only the additions above.

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage:** Feature P0 #6 "Docker Housekeeping" requires `tengiz cleanup`, label-based pruning that protects Tengiz-managed containers/images, and periodic cleanup of unused volumes, networks, containers, and images. Covered: Task 3 prunes all five categories (containers, images, networks, volumes, build cache); label filters (`label!=tengiz-app`, `label!=tengiz-env`) and the `tengiz-apps/*` prefix check implement the required protection; Task 4 adds the `tengiz cleanup` command. The `--all` flag mirrors `docker system prune -a`. Granular per-category flags are deliberately out of scope (P1 #56). Periodic/scheduled cleanup is out of scope for this plan (the doc lists `tengiz cleanup` as the deliverable, not a background job).

**2. Placeholder scan:** Every step contains complete code or exact commands with expected output. No "TBD", no "add error handling" without code. Build-cache failure handling, image-in-use failure handling, and dry-run counting are all concretely specified.

**3. Type consistency:** `PruneOptions{DryRun, AllImages}` and `PruneReport{DryRun, Containers, Images, Networks, Volumes, BuildCache, Reclaimed map[string]string}` are defined in Task 1 and used identically in Tasks 3 and 4. `runPrune(ctx, opts, run dockerRunner)` signature matches all test calls. `pruneSteps()` names (`containers`, `images`, `networks`, `volumes`, `build-cache`) match the `runPrune` switch cases and the `pruneReportString` reclaimed loop. All mock implementations (`stubManager`, two `mockRuntime`, `mockRTForDeploy`) gain the same `Prune` method in Task 1.
