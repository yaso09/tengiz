# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, networks, build cache, and optionally volumes) using label-based filtering so Tengiz-managed resources are never deleted, solving disk-space exhaustion on single-server deployments.

**Architecture:** A new `internal/cleanup` package shells out to the `docker` CLI via `exec.CommandContext` (matching the existing `runtime` package pattern). The `Runner` struct holds an injectable command function (`run`) so orchestration is unit-testable without Docker. Container pruning uses `docker container prune --filter label!=tengiz-app` so containers labeled `tengiz-app=<name>` are protected. Image pruning is custom Go logic: list all images and all image references from `docker ps -a`, then `docker rmi` only images that are neither referenced by any container, nor dangling (`<none>:<none>`), nor prefixed `tengiz-apps/`. Networks/build cache are pruned unconditionally; volumes only with `--volumes` (data-safety). A `--dry-run` mode lists candidates without deleting; `--interval` runs cleanup repeatedly for systemd-timer-style operation.

**Tech Stack:** Go 1.26, standard library only (no new dependencies), `os/exec` for Docker CLI calls, Cobra for the CLI command.

## Global Constraints

- New package `internal/cleanup` — standard library only, no new external dependencies (go.mod unchanged)
- Container protection label key: `tengiz-app` (must match `labelKey` in `internal/runtime/docker.go:76`)
- Image protection repository prefix: `tengiz-apps/` (must match builder image repo in `internal/builder/builder.go:61,84`)
- Volumes are NEVER pruned by default; `--volumes` flag required (volumes hold user data)
- `--dry-run` defaults to `false`; when set, no destructive command is executed
- `--interval` defaults to `0` (run once); when > 0, runs until interrupted (SIGINT/SIGTERM)
- All docker commands executed with `exec.CommandContext` so they respect context cancellation
- CLI output uses the existing `[tengiz] ` prefix convention (see `internal/cli/root.go`)
- README.md must be updated (AGENTS.md rule: UI/UX changes require docs updates)
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/cleanup.go` (new) | `Runner`, `Options`, `Result`, injected `run` func, all docker arg-builder helpers, `Run`/`dryRun` orchestration, image-selection and output-parsing helpers |
| `internal/cleanup/cleanup_test.go` (new) | Unit tests for pure helpers and orchestration via injected fake command runner |
| `internal/cli/cleanup.go` (new) | `cleanupCmd` cobra command, `--interval` loop, result printing |
| `internal/cli/cmd_cleanup_test.go` (new) | CLI registration and flag tests (matches `cmd_secret_test.go` convention) |
| `internal/cli/root.go` (modify) | Register `cleanupCmd` and its flags in `init()` |
| `README.md` (modify) | Add Docker Housekeeping to Features list + `tengiz cleanup` to CLI Reference |

`internal/runtime/cleanup.go` already contains `RemoveImage`/`KeepLastNImages` (rollback image cleanup) and is left unchanged — the new `cleanup` package is a separate, broader housekeeping layer.

---

### Task 1: `internal/cleanup` core types and pure helpers

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `cleanup.Options{DryRun, Volumes bool}`, `cleanup.Result{...}`, `cleanup.New() *Runner`, `cleanup.Runner` with unexported `run func(ctx context.Context, args ...string) (string, error)` field, plus pure helpers `containerPruneArgs()`, `containerCandidatesArgs()`, `networkPruneArgs()`, `networkCandidatesArgs()`, `buildCachePruneArgs()`, `volumePruneArgs()`, `volumeCandidatesArgs()`, `usedImagesArgs()`, `imagesListArgs()`, `parseReclaimed(string) string`, `lines(string) []string`, `countDeleted(string, string) int`, `selectImagesForRemoval(images, used []string, protectRepo string) []string`

- [ ] **Step 1: Write the failing test**

```go
// internal/cleanup/cleanup_test.go
package cleanup

import (
	"reflect"
	"testing"
)

func TestContainerPruneArgs(t *testing.T) {
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if got := containerPruneArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("containerPruneArgs() = %v, want %v", got, want)
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	if got := networkPruneArgs(); !reflect.DeepEqual(got, []string{"network", "prune", "-f"}) {
		t.Errorf("networkPruneArgs() = %v", got)
	}
}

func TestBuildCachePruneArgs(t *testing.T) {
	if got := buildCachePruneArgs(); !reflect.DeepEqual(got, []string{"builder", "prune", "-f"}) {
		t.Errorf("buildCachePruneArgs() = %v", got)
	}
}

func TestVolumePruneArgs(t *testing.T) {
	if got := volumePruneArgs(); !reflect.DeepEqual(got, []string{"volume", "prune", "-f"}) {
		t.Errorf("volumePruneArgs() = %v", got)
	}
}

func TestContainerCandidatesArgs(t *testing.T) {
	want := []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.Names}}"}
	if got := containerCandidatesArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("containerCandidatesArgs() = %v, want %v", got, want)
	}
}

func TestSelectImagesForRemoval(t *testing.T) {
	images := []string{
		"<none>:<none>",
		"tengiz-apps/myapp:prod-latest",
		"tengiz-apps/myapp:prod-1700000000",
		"node:20-alpine",
		"nginx:alpine",
		"postgres:16",
	}
	used := []string{"tengiz-apps/myapp:prod-1700000000", "postgres:16"}
	got := selectImagesForRemoval(images, used, "tengiz-apps/")
	want := []string{"node:20-alpine", "nginx:alpine"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectImagesForRemoval() = %v, want %v", got, want)
	}
}

func TestParseReclaimed(t *testing.T) {
	out := "Deleted Containers:\nabc\n\nTotal reclaimed space: 12.4MB\n"
	if got := parseReclaimed(out); got != "12.4MB" {
		t.Errorf("parseReclaimed() = %q, want %q", got, "12.4MB")
	}
	if got := parseReclaimed("no output"); got != "0B" {
		t.Errorf("parseReclaimed() = %q, want %q", got, "0B")
	}
}

func TestCountDeleted(t *testing.T) {
	out := "Deleted Containers:\nc1\nc2\n\nTotal reclaimed space: 1.2MB\n"
	if got := countDeleted(out, "Deleted Containers:"); got != 2 {
		t.Errorf("countDeleted() = %d, want 2", got)
	}
	if got := countDeleted("no section", "Deleted Containers:"); got != 0 {
		t.Errorf("countDeleted() = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL — the package does not exist yet (`open .../internal/cleanup: no such file or directory`)

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/cleanup/cleanup.go
package cleanup

import (
	"context"
	"os/exec"
	"strings"
)

const (
	tengizAppLabel = "tengiz-app"
	tengizImgRepo  = "tengiz-apps/"
)

// Options controls what a cleanup run removes.
type Options struct {
	DryRun  bool // preview what would be removed without removing anything
	Volumes bool // also prune unused anonymous volumes
}

// Result summarizes a cleanup run.
type Result struct {
	DryRun              bool
	ContainersRemoved   int
	ImagesRemoved       int
	NetworksRemoved     int
	VolumesRemoved      int
	BuildCachePruned    bool
	Reclaimed           []string // "containers: 1.2MB" style lines
	ContainerCandidates []string // dry-run mode only
	ImageCandidates     []string // dry-run mode only
	NetworkCandidates   []string // dry-run mode only
	VolumeCandidates    []string // dry-run mode only
}

// Runner executes Docker housekeeping commands while protecting
// Tengiz-managed resources (labeled containers and tengiz-apps/* images).
type Runner struct {
	run func(ctx context.Context, args ...string) (string, error)
}

// New returns a Runner that shells out to the docker CLI.
func New() *Runner {
	return &Runner{
		run: func(ctx context.Context, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, "docker", args...)
			out, err := cmd.CombinedOutput()
			return string(out), err
		},
	}
}

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=" + tengizAppLabel}
}

func containerCandidatesArgs() []string {
	return []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=" + tengizAppLabel, "--format", "{{.Names}}"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func networkCandidatesArgs() []string {
	return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func volumeCandidatesArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func usedImagesArgs() []string {
	return []string{"ps", "-a", "--format", "{{.Image}}"}
}

func imagesListArgs() []string {
	return []string{"images", "--format", "{{.Repository}}:{{.Tag}}"}
}

func parseReclaimed(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return "0B"
}

func lines(out string) []string {
	var result []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

func countDeleted(out, section string) int {
	parts := strings.Split(out, "\n")
	start := -1
	for i, l := range parts {
		if strings.TrimSpace(l) == section {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return 0
	}
	count := 0
	for i := start; i < len(parts); i++ {
		if strings.TrimSpace(parts[i]) == "" {
			break
		}
		count++
	}
	return count
}

// selectImagesForRemoval returns the image refs that are safe to remove:
// not dangling, not referenced by any (running or stopped) container,
// and not part of the Tengiz image repository.
func selectImagesForRemoval(images, used []string, protectRepo string) []string {
	usedSet := make(map[string]struct{}, len(used))
	for _, u := range used {
		usedSet[u] = struct{}{}
	}
	var result []string
	for _, img := range images {
		if img == "<none>:<none>" {
			continue
		}
		if _, ok := usedSet[img]; ok {
			continue
		}
		if strings.HasPrefix(img, protectRepo) {
			continue
		}
		result = append(result, img)
	}
	return result
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all 9 tests)

- [ ] **Step 5: Run go vet and build**

Run: `go vet ./internal/cleanup/... && go build ./...`

Expected: no issues, build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add internal/cleanup package core types and docker arg helpers"
```

---

### Task 2: Prune execution methods and `Run` orchestration

**Files:**
- Modify: `internal/cleanup/cleanup.go` — add `pruneContainers`, `pruneImages`, `pruneNetworks`, `pruneBuildCache`, `pruneVolumes`, `Run`
- Modify: `internal/cleanup/cleanup_test.go` — add orchestration tests

**Interfaces:**
- Consumes: `Runner.run` (Task 1), pure arg builders and helpers from Task 1
- Produces: `func (r *Runner) Run(ctx context.Context, opts Options) (*Result, error)` — later tasks and the CLI rely on this exact signature

- [ ] **Step 1: Write the failing tests**

```go
// internal/cleanup/cleanup_test.go — append these tests
// Update the import block first to include "context" and "strings":
package cleanup

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func fakeRun(calls *[]string, out map[string]string) func(ctx context.Context, args ...string) (string, error) {
	return func(ctx context.Context, args ...string) (string, error) {
		key := strings.Join(args, " ")
		*calls = append(*calls, key)
		return out[key], nil
	}
}

func TestRunExecutesPruneCommands(t *testing.T) {
	var calls []string
	r := &Runner{run: fakeRun(&calls, map[string]string{
		"container prune -f --filter label!=tengiz-app": "Deleted Containers:\nc1\n\nTotal reclaimed space: 1.2MB\n",
		"ps -a --format {{.Image}}":                     "",
		"images --format {{.Repository}}:{{.Tag}}":      "",
		"image prune -f":                                "Total reclaimed space: 500MB\n",
		"network prune -f":                              "Total reclaimed space: 0B\n",
		"builder prune -f":                              "Total reclaimed space: 2.1GB\n",
	})}

	res, err := r.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.DryRun {
		t.Error("DryRun = true, want false")
	}
	if res.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", res.ContainersRemoved)
	}
	if !res.BuildCachePruned {
		t.Error("BuildCachePruned = false, want true")
	}
	if res.VolumesRemoved != 0 {
		t.Errorf("VolumesRemoved = %d, want 0 (volumes off by default)", res.VolumesRemoved)
	}
	for _, want := range []string{"container prune -f --filter label!=tengiz-app", "image prune -f", "network prune -f", "builder prune -f"} {
		if !contains(calls, want) {
			t.Errorf("Run() calls missing %q; got %v", want, calls)
		}
	}
	if contains(calls, "volume prune -f") {
		t.Errorf("Run() pruned volumes without --volumes; calls = %v", calls)
	}
}

func TestRunWithVolumes(t *testing.T) {
	var calls []string
	r := &Runner{run: fakeRun(&calls, map[string]string{
		"container prune -f --filter label!=tengiz-app": "Total reclaimed space: 0B\n",
		"ps -a --format {{.Image}}":                     "",
		"images --format {{.Repository}}:{{.Tag}}":      "",
		"image prune -f":                                "Total reclaimed space: 0B\n",
		"network prune -f":                              "Total reclaimed space: 0B\n",
		"builder prune -f":                              "Total reclaimed space: 0B\n",
		"volume prune -f":                               "Deleted Volumes:\nvol1\n\nTotal reclaimed space: 100MB\n",
	})}

	res, err := r.Run(context.Background(), Options{Volumes: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", res.VolumesRemoved)
	}
	if !contains(calls, "volume prune -f") {
		t.Errorf("Run() did not call volume prune; calls = %v", calls)
	}
}

func TestRunPruneImagesRemovesForeignOnly(t *testing.T) {
	var calls []string
	r := &Runner{run: fakeRun(&calls, map[string]string{
		"container prune -f --filter label!=tengiz-app": "Total reclaimed space: 0B\n",
		"ps -a --format {{.Image}}":                     "tengiz-apps/myapp:prod-latest\npostgres:16\n",
		"images --format {{.Repository}}:{{.Tag}}":      "tengiz-apps/myapp:prod-latest\ntengiz-apps/myapp:prod-1700000000\nnode:20-alpine\npostgres:16\n",
		"rmi -f node:20-alpine":                         "Untagged: node:20-alpine\n",
		"image prune -f":                                "Total reclaimed space: 0B\n",
		"network prune -f":                              "Total reclaimed space: 0B\n",
		"builder prune -f":                              "Total reclaimed space: 0B\n",
	})}

	res, err := r.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1", res.ImagesRemoved)
	}
	if !contains(calls, "rmi -f node:20-alpine") {
		t.Errorf("Run() did not remove foreign image; calls = %v", calls)
	}
	for _, forbidden := range []string{"rmi -f tengiz-apps/myapp:prod-1700000000", "rmi -f tengiz-apps/myapp:prod-latest", "rmi -f postgres:16"} {
		if contains(calls, forbidden) {
			t.Errorf("Run() removed protected image via %q; calls = %v", forbidden, calls)
		}
	}
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestRun" -v -count=1`

Expected: FAIL with `r.Run undefined (type *Runner has no field or method Run)`

- [ ] **Step 3: Write the minimal implementation**

Append to `internal/cleanup/cleanup.go`:

```go
func (r *Runner) pruneContainers(ctx context.Context) (int, string, error) {
	out, err := r.run(ctx, containerPruneArgs()...)
	if err != nil {
		return 0, "", err
	}
	return countDeleted(out, "Deleted Containers:"), parseReclaimed(out), nil
}

func (r *Runner) pruneNetworks(ctx context.Context) (int, string, error) {
	out, err := r.run(ctx, networkPruneArgs()...)
	if err != nil {
		return 0, "", err
	}
	return countDeleted(out, "Deleted Networks:"), parseReclaimed(out), nil
}

func (r *Runner) pruneVolumes(ctx context.Context) (int, string, error) {
	out, err := r.run(ctx, volumePruneArgs()...)
	if err != nil {
		return 0, "", err
	}
	return countDeleted(out, "Deleted Volumes:"), parseReclaimed(out), nil
}

func (r *Runner) pruneBuildCache(ctx context.Context) (bool, string, error) {
	out, err := r.run(ctx, buildCachePruneArgs()...)
	if err != nil {
		return false, "", err
	}
	return true, parseReclaimed(out), nil
}

func (r *Runner) pruneImages(ctx context.Context) (int, string, error) {
	usedOut, err := r.run(ctx, usedImagesArgs()...)
	if err != nil {
		return 0, "", err
	}
	imgOut, err := r.run(ctx, imagesListArgs()...)
	if err != nil {
		return 0, "", err
	}
	removed := 0
	for _, img := range selectImagesForRemoval(lines(imgOut), lines(usedOut), tengizImgRepo) {
		if _, err := r.run(ctx, "rmi", "-f", img); err != nil {
			continue
		}
		removed++
	}
	out, err := r.run(ctx, "image", "prune", "-f")
	if err != nil {
		return removed, "", err
	}
	return removed, parseReclaimed(out), nil
}

// Run prunes unused Docker resources. Tengiz-managed containers (labeled
// tengiz-app) and tengiz-apps/* images are always protected. Volumes are
// only pruned when opts.Volumes is set.
func (r *Runner) Run(ctx context.Context, opts Options) (*Result, error) {
	res := &Result{}

	n, reclaimed, err := r.pruneContainers(ctx)
	if err != nil {
		return nil, err
	}
	res.ContainersRemoved = n
	res.Reclaimed = append(res.Reclaimed, "containers: "+reclaimed)

	n, reclaimed, err = r.pruneImages(ctx)
	if err != nil {
		return nil, err
	}
	res.ImagesRemoved = n
	res.Reclaimed = append(res.Reclaimed, "images: "+reclaimed)

	n, reclaimed, err = r.pruneNetworks(ctx)
	if err != nil {
		return nil, err
	}
	res.NetworksRemoved = n
	res.Reclaimed = append(res.Reclaimed, "networks: "+reclaimed)

	pruned, reclaimed, err := r.pruneBuildCache(ctx)
	if err != nil {
		return nil, err
	}
	res.BuildCachePruned = pruned
	res.Reclaimed = append(res.Reclaimed, "build cache: "+reclaimed)

	if opts.Volumes {
		n, reclaimed, err := r.pruneVolumes(ctx)
		if err != nil {
			return nil, err
		}
		res.VolumesRemoved = n
		res.Reclaimed = append(res.Reclaimed, "volumes: "+reclaimed)
	}
	return res, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -run "TestRun" -v -count=1`

Expected: PASS (all 3 new tests)

- [ ] **Step 5: Run all cleanup tests**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: ALL PASS (12 tests)

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: implement cleanup Run orchestration with tengiz-app image protection"
```

---

### Task 3: Dry-run mode

**Files:**
- Modify: `internal/cleanup/cleanup.go` — add `dryRun` method and wire into `Run`
- Modify: `internal/cleanup/cleanup_test.go` — add dry-run tests

**Interfaces:**
- Consumes: `Run` from Task 2 (adds `DryRun` branch), candidate arg builders from Task 1
- Produces: `Result.ContainerCandidates`, `Result.ImageCandidates`, `Result.NetworkCandidates`, `Result.VolumeCandidates` populated when `Options.DryRun` is true

- [ ] **Step 1: Write the failing tests**

```go
// internal/cleanup/cleanup_test.go — append these tests
package cleanup

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestRunDryRunListsCandidates(t *testing.T) {
	var calls []string
	r := &Runner{run: fakeRun(&calls, map[string]string{
		"ps -a --filter status=exited --filter label!=tengiz-app --format {{.Names}}": "orphan1\norphan2\n",
		"ps -a --format {{.Image}}":                "",
		"images --format {{.Repository}}:{{.Tag}}": "tengiz-apps/myapp:prod-latest\nnode:20-alpine\n",
		"network ls --filter dangling=true --format {{.Name}}": "bridge_x\n",
		"volume ls --filter dangling=true --format {{.Name}}":  "vol1\n",
	})}

	res, err := r.Run(context.Background(), Options{DryRun: true, Volumes: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !res.DryRun {
		t.Error("DryRun = false, want true")
	}
	if !res.BuildCachePruned {
		t.Error("BuildCachePruned = false, want true (build cache would be cleared)")
	}
	if !reflect.DeepEqual(res.ContainerCandidates, []string{"orphan1", "orphan2"}) {
		t.Errorf("ContainerCandidates = %v", res.ContainerCandidates)
	}
	if !reflect.DeepEqual(res.ImageCandidates, []string{"node:20-alpine"}) {
		t.Errorf("ImageCandidates = %v", res.ImageCandidates)
	}
	if !reflect.DeepEqual(res.NetworkCandidates, []string{"bridge_x"}) {
		t.Errorf("NetworkCandidates = %v", res.NetworkCandidates)
	}
	if !reflect.DeepEqual(res.VolumeCandidates, []string{"vol1"}) {
		t.Errorf("VolumeCandidates = %v", res.VolumeCandidates)
	}
	for _, c := range calls {
		if strings.Contains(c, " prune") {
			t.Errorf("dry-run executed destructive prune command %q; calls = %v", c, calls)
		}
	}
}

func TestRunDryRunWithoutVolumesListsNoVolumes(t *testing.T) {
	var calls []string
	r := &Runner{run: fakeRun(&calls, map[string]string{
		"ps -a --filter status=exited --filter label!=tengiz-app --format {{.Names}}": "",
		"ps -a --format {{.Image}}":                "",
		"images --format {{.Repository}}:{{.Tag}}": "",
		"network ls --filter dangling=true --format {{.Name}}": "",
	})}

	res, err := r.Run(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.VolumeCandidates != nil {
		t.Errorf("VolumeCandidates = %v, want nil", res.VolumeCandidates)
	}
	if contains(calls, "volume ls --filter dangling=true --format {{.Name}}") {
		t.Errorf("dry-run listed volumes without --volumes; calls = %v", calls)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/... -run "TestRunDryRun" -v -count=1`

Expected: FAIL — `Run` currently ignores `DryRun` and executes prune commands (dry-run asserts `!res.DryRun` etc. will trip)

- [ ] **Step 3: Write the minimal implementation**

Append to `internal/cleanup/cleanup.go` and add the dry-run branch at the top of `Run`:

```go
func (r *Runner) dryRun(ctx context.Context, opts Options) (*Result, error) {
	res := &Result{DryRun: true, BuildCachePruned: true}

	out, err := r.run(ctx, containerCandidatesArgs()...)
	if err != nil {
		return nil, err
	}
	res.ContainerCandidates = lines(out)

	usedOut, err := r.run(ctx, usedImagesArgs()...)
	if err != nil {
		return nil, err
	}
	imgOut, err := r.run(ctx, imagesListArgs()...)
	if err != nil {
		return nil, err
	}
	res.ImageCandidates = selectImagesForRemoval(lines(imgOut), lines(usedOut), tengizImgRepo)

	out, err = r.run(ctx, networkCandidatesArgs()...)
	if err != nil {
		return nil, err
	}
	res.NetworkCandidates = lines(out)

	if opts.Volumes {
		out, err = r.run(ctx, volumeCandidatesArgs()...)
		if err != nil {
			return nil, err
		}
		res.VolumeCandidates = lines(out)
	}
	return res, nil
}
```

Change the first line of `Run` to:

```go
func (r *Runner) Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.DryRun {
		return r.dryRun(ctx, opts)
	}
	res := &Result{}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/... -run "TestRun" -v -count=1`

Expected: PASS (5 tests)

- [ ] **Step 5: Run all cleanup tests**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: ALL PASS (14 tests)

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup dry-run mode that lists candidates without pruning"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:38-44` (command registration in `init()`) and flags near the other `cmd.Flags()` calls in `init()`
- Test: `internal/cli/cmd_cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.New()`, `cleanup.Options{DryRun, Volumes bool}`, `(*cleanup.Runner).Run(ctx, opts) (*cleanup.Result, error)` from Tasks 1-3
- Produces: `tengiz cleanup` command with `--dry-run`, `--volumes`, `--interval` flags; `printCleanupResult(res *cleanup.Result)`; `runCleanupLoop(cmd, runner, opts, interval)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cmd_cleanup_test.go
package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"dry-run", "volumes", "interval"} {
		if f := cleanupCmd.Flags().Lookup(flag); f == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdDryRunDefaultFalse(t *testing.T) {
	dryRun, _ := cleanupCmd.Flags().GetBool("dry-run")
	if dryRun {
		t.Error("--dry-run should default to false")
	}
}

func TestCleanupCmdNoArgs(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed; skipping")
	}
	rootCmd.SetArgs([]string{"cleanup"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal("cleanup with no args should not error, got:", err)
	}
}
```

`TestCleanupCmdNoArgs` shells out to `docker` — it is skipped when docker is absent so the suite stays hermetic. The import block is:

```go
import (
	"os/exec"
	"testing"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources",
	Long: `Remove unused Docker resources to reclaim disk space.

Prunes stopped non-Tengiz containers, unused images (protecting all
tengiz-apps/* images and images referenced by any container), dangling
networks, and the Docker build cache. Containers labeled tengiz-app are
never removed.

Flags:
  --dry-run    preview what would be removed without removing anything
  --volumes    also prune unused anonymous volumes
  --interval   run cleanup repeatedly at this interval (e.g. 1h, 30m)
               until interrupted — for cron/systemd timer processes`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		volumes, _ := cmd.Flags().GetBool("volumes")
		interval, _ := cmd.Flags().GetDuration("interval")

		runner := cleanup.New()
		opts := cleanup.Options{DryRun: dryRun, Volumes: volumes}

		if interval > 0 {
			return runCleanupLoop(cmd, runner, opts, interval)
		}

		res, err := runner.Run(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupResult(res)
		return nil
	},
}

func runCleanupLoop(cmd *cobra.Command, runner *cleanup.Runner, opts cleanup.Options, interval time.Duration) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		res, err := runner.Run(ctx, opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupResult(res)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func printCleanupResult(res *cleanup.Result) {
	if res.DryRun {
		fmt.Println("[tengiz] DRY RUN — nothing was removed")
		fmt.Printf("[tengiz] containers to remove: %s\n", listOrNone(res.ContainerCandidates))
		fmt.Printf("[tengiz] images to remove: %s\n", listOrNone(res.ImageCandidates))
		fmt.Printf("[tengiz] networks to remove: %s\n", listOrNone(res.NetworkCandidates))
		if res.VolumeCandidates != nil {
			fmt.Printf("[tengiz] volumes to remove: %s\n", listOrNone(res.VolumeCandidates))
		}
		if res.BuildCachePruned {
			fmt.Println("[tengiz] build cache would be cleared")
		}
		return
	}
	fmt.Printf("[tengiz] containers removed: %d\n", res.ContainersRemoved)
	fmt.Printf("[tengiz] images removed: %d\n", res.ImagesRemoved)
	fmt.Printf("[tengiz] networks removed: %d\n", res.NetworksRemoved)
	if res.BuildCachePruned {
		fmt.Println("[tengiz] build cache cleared")
	}
	if res.VolumesRemoved > 0 {
		fmt.Printf("[tengiz] volumes removed: %d\n", res.VolumesRemoved)
	}
	if len(res.Reclaimed) > 0 {
		for _, line := range res.Reclaimed {
			fmt.Printf("[tengiz] reclaimed %s\n", line)
		}
	}
	fmt.Println("[tengiz] cleanup complete")
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}
```

- [ ] **Step 4: Register the command in `internal/cli/root.go`**

In `init()` (next to the other `rootCmd.AddCommand(...)` calls, e.g. after line 45 `rootCmd.AddCommand(logsCmd)`), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused anonymous volumes")
	cleanupCmd.Flags().Duration("interval", 0, "run cleanup repeatedly at this interval (e.g. 1h, 30m) until interrupted")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS (4 tests)

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Run all CLI tests**

Run: `go test ./internal/cli/... -v -count=1`

Expected: ALL PASS (existing tests unaffected)

- [ ] **Step 7: Manual smoke test (docker required)**

Run: `go run . cleanup --dry-run`

Expected (with docker installed and no resources to clean):
```
[tengiz] DRY RUN — nothing was removed
[tengiz] containers to remove: (none)
[tengiz] images to remove: (none)
[tengiz] networks to remove: (none)
[tengiz] build cache would be cleared
```

- [ ] **Step 8: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cmd_cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command with dry-run, volumes, and interval flags"
```

---

### Task 5: README documentation and full verification

**Files:**
- Modify: `README.md` — Features list + CLI Reference section

**Interfaces:**
- Consumes: the `tengiz cleanup` command and its flags from Task 4

- [ ] **Step 1: Add the feature to the Features list in `README.md`**

After the `- **Self-contained** — Auto-generates Dockerfiles when none exist.` line (README.md:23), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused images, stopped non-Tengiz containers, dangling networks, and the build cache. Label-based filtering keeps every `tengiz-app` container and `tengiz-apps/*` image safe. `--dry-run` previews, `--volumes` opts into volume pruning, `--interval` runs on a schedule.
```

- [ ] **Step 2: Add the CLI Reference section in `README.md`**

After the `### tengiz preview` subsection (README.md:333) and before `### tengiz config`, add:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space. Protects all Tengiz-managed containers (label `tengiz-app`) and images (`tengiz-apps/*`).

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--volumes` | Also prune unused anonymous volumes |
| `--interval <duration>` | Run repeatedly at this interval (e.g. `1h`, `30m`) until interrupted |

~~~sh
tengiz cleanup                  # prune containers, images, networks, build cache
tengiz cleanup --dry-run        # preview what would be removed
tengiz cleanup --volumes        # also prune unused anonymous volumes
tengiz cleanup --interval 1h    # keep running; prunes every hour
~~~
```

- [ ] **Step 3: Run full test suite**

Run: `go test ./... -v -count=1`

Expected: ALL PASS (the two proxy TCP-dial-timeout tests and idle time-sensitive tests may be slow but must pass)

- [ ] **Step 4: Run static analysis and build**

Run: `go vet ./...`

Expected: no issues

Run: `go build -o tengiz .`

Expected: build succeeds

- [ ] **Step 5: Self-review against spec**

Check against `docs/FUTURES_FEATURES.md` Docker Housekeeping (#6):
- Label-based `docker system prune` protecting Tengiz resources → `container prune --filter label!=tengiz-app` + custom image selection ✅ (Tasks 2-3)
- `tengiz cleanup` command ✅ (Task 4)
- Periodic cleaning → `--interval` loop ✅ (Task 4)
- Unused volumes/networks/containers/images cleaned ✅ (Tasks 2-3; volumes opt-in)
- No new dependencies ✅ (std library only)

- [ ] **Step 6: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "fill in details", "add appropriate error handling", "similar to Task". None present — every code step contains complete, compilable code.

- [ ] **Step 7: Type consistency check**

- `cleanup.Options{DryRun, Volumes bool}` — same struct in Tasks 1, 3, 4
- `cleanup.Result` fields (`DryRun`, `ContainersRemoved`, `ImagesRemoved`, `NetworksRemoved`, `VolumesRemoved`, `BuildCachePruned`, `Reclaimed`, `*Candidates`) — same names in Tasks 1-4
- `(*cleanup.Runner).Run(ctx, opts) (*Result, error)` — called identically in Tasks 2, 3, 4
- `printCleanupResult`, `runCleanupLoop`, `listOrNone` — defined in Task 4, used only there
- Label constant `tengiz-app` matches `labelKey` in `runtime/docker.go:76`; prefix `tengiz-apps/` matches builder tags in `builder/builder.go:61`

- [ ] **Step 8: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command and Docker housekeeping feature"
```
