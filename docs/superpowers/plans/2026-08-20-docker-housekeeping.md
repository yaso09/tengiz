# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely reclaims disk space by pruning unused Docker resources (stopped non-Tengiz containers, dangling/unused images, unused networks, opt-in volumes) while never touching Tengiz-managed containers or images.

**Architecture:** Extend the `runtime.Manager` interface with a `Cleanup` method, implemented on `dockerRuntime` exactly like the existing exec-based methods (`docker ... prune` / `docker rmi` via `os/exec`). All Docker command construction and the image keep-list logic are isolated in pure functions so they are unit-testable without a Docker daemon (CI has no Docker). A new Cobra `cleanup` command wires CLI flags to the runtime and prints a report. Tengiz images get a `tengiz-app` build label (defense in depth) and are additionally protected during `--all` pruning by a reference-based keep-list (`tengiz-apps/*`).

**Tech Stack:** Go 1.26, `spf13/cobra` (existing), `os/exec` docker CLI (existing). No new third-party dependencies.

## Global Constraints

- No new dependencies — Go stdlib + existing `cobra` only.
- Follow the existing exec pattern: `exec.CommandContext(ctx, "docker", args...)` with `CombinedOutput()`.
- Every docker-touching test must pass WITHOUT a Docker daemon (`.github/workflows/ci.yml` runs `go test ./...` on plain `ubuntu-latest`). Therefore: pure arg-builders, pure keep-list helpers, and stub manager tests only — never invoke `docker` in a test.
- Resource-safety: prune must NEVER remove running containers, containers labeled `tengiz-app=<app>`, or images referenced `tengiz-apps/*`.
- `--volumes` must be opt-in because `docker volume prune` can delete named volumes not currently mounted.
- Container label conventions from `internal/runtime/docker.go:76-77`: `tengiz-app` (labelKey), `tengiz-env` (envLabelKey). Image refs from `internal/builder/builder.go:61`: `tengiz-apps/<app>:<env>-<deploymentID>` plus `<env>-latest`.
- Update README.md and docs/FUTURES_FEATURES.md (AGENTS.md rule: document UI/UX + feature changes).
- Follow TDD per task: write failing test → verify it fails → implement → verify pass → commit.
- Commands to verify: `go build -o tengiz .`, `go vet ./...`, `go test ./... -v -count=1`.
- Work on a feature branch: `git checkout -b feat/docker-housekeeping`.

---

### Task 1: Add `Cleanup` to the `runtime.Manager` interface, types, and stub

Adds the `CleanupOptions`/`CleanupReport` types, a `Cleanup` method to the `Manager` interface, and a no-op stub implementation. This is the contract every later task builds on.

**Files:**
- Create: `internal/runtime/cleanup.go` (types only for now)
- Modify: `internal/runtime/runtime.go:31-49` (interface), `internal/runtime/runtime.go:113-119` (stub)
- Modify: `internal/cli/root_test.go:69-100` (mock must keep satisfying `Manager`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type CleanupOptions struct { Containers, Images, Networks, Volumes, AllImages, DryRun bool }`
  - `type CleanupReport struct { Containers, Images, Networks, Volumes int; DryRun bool; Output string }`
  - `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)` on `Manager`.
  - `stubManager.Cleanup` returns `(CleanupReport{}, nil)`.

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
	rep, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if rep.Containers != 0 || rep.Images != 0 || rep.Networks != 0 || rep.Volumes != 0 {
		t.Errorf("Cleanup() report = %+v, want all-zero counts", rep)
	}
	if rep.DryRun {
		t.Errorf("Cleanup() DryRun = true, want false")
	}
}

func TestStubCleanupOptions(t *testing.T) {
	m := NewStub()
	rep, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !rep.DryRun {
		t.Errorf("Cleanup() DryRun = false, want true")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: FAIL — compile error `m.Cleanup undefined (type Manager has no field or method Cleanup)`.

- [ ] **Step 3: Add types + interface method + stub implementation**

Create `internal/runtime/cleanup.go`:

```go
package runtime

import "context"

type CleanupOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	AllImages  bool
	DryRun     bool
}

type CleanupReport struct {
	Containers int
	Images     int
	Networks   int
	Volumes    int
	DryRun     bool
	Output     string
}
```

In `internal/runtime/runtime.go`, add to the `Manager` interface (after the `Run` line, `runtime.go:48`):

```go
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)
```

Add the stub method after `Run` in the stub section (`runtime.go:121-122`):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{DryRun: opts.DryRun}, nil
}
```

Update `internal/cli/root_test.go` — add this method to `mockRTForDeploy` (after its `Run` method, line 100):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return runtime.CleanupReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... -v -count=1`

Expected: PASS — `TestStubCleanup`, `TestStubCleanupOptions`, and all existing tests including `TestMockRTForDeployImplementsManager` (root_test.go:102).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup method to Manager interface and stub"
```

---

### Task 2: Pure Docker arg builders for prune and dry-run listing

Adds pure functions that build the exact `docker` CLI argument slices for every cleanup category and both modes (prune vs dry-run listing). The `label!=tengiz-app` filter is the safety mechanism that protects Tengiz-managed containers.

**Files:**
- Modify: `internal/runtime/cleanup.go` (add builders)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing (pure functions).
- Produces:
  - `buildContainerPruneArgs() []string` → `["container","prune","-f","--filter","label!=tengiz-app"]`
  - `buildContainerDryRunArgs() []string` → `["container","ls","-a","--filter","status=exited","--filter","status=created","--filter","status=dead","--filter","label!=tengiz-app","--format","{{.ID}}"]`
  - `buildDanglingImagePruneArgs() []string` → `["image","prune","-f"]`
  - `buildDanglingImageDryRunArgs() []string` → `["images","--no-trunc","--filter","dangling=true","-q"]`
  - `buildNetworkPruneArgs() []string` → `["network","prune","-f"]`
  - `buildNetworkDryRunArgs() []string` → `["network","ls","-q"]`
  - `buildVolumePruneArgs() []string` → `["volume","prune","-f"]`
  - `buildVolumeDryRunArgs() []string` → `["volume","ls","-q"]`
  - `nonDanglingImageIDArgs() []string` → `["images","--no-trunc","--filter","dangling=false","-q"]`
  - `imagesByReferenceArgs(ref string) []string` → `["images","--no-trunc","--filter","reference=<ref>","-q"]`
  - `removeImageArgs(id string) []string` → `["rmi","-f","<id>"]`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args = %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildContainerPruneArgs(t *testing.T) {
	assertArgs(t, buildContainerPruneArgs(), []string{
		"container", "prune", "-f", "--filter", "label!=tengiz-app",
	})
}

func TestBuildContainerDryRunArgs(t *testing.T) {
	assertArgs(t, buildContainerDryRunArgs(), []string{
		"container", "ls", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--filter", "status=dead",
		"--filter", "label!=tengiz-app",
		"--format", "{{.ID}}",
	})
}

func TestBuildDanglingImagePruneArgs(t *testing.T) {
	assertArgs(t, buildDanglingImagePruneArgs(), []string{"image", "prune", "-f"})
}

func TestBuildDanglingImageDryRunArgs(t *testing.T) {
	assertArgs(t, buildDanglingImageDryRunArgs(), []string{
		"images", "--no-trunc", "--filter", "dangling=true", "-q",
	})
}

func TestBuildNetworkPruneArgs(t *testing.T) {
	assertArgs(t, buildNetworkPruneArgs(), []string{"network", "prune", "-f"})
}

func TestBuildNetworkDryRunArgs(t *testing.T) {
	assertArgs(t, buildNetworkDryRunArgs(), []string{"network", "ls", "-q"})
}

func TestBuildVolumePruneArgs(t *testing.T) {
	assertArgs(t, buildVolumePruneArgs(), []string{"volume", "prune", "-f"})
}

func TestBuildVolumeDryRunArgs(t *testing.T) {
	assertArgs(t, buildVolumeDryRunArgs(), []string{"volume", "ls", "-q"})
}

func TestNonDanglingImageIDArgs(t *testing.T) {
	assertArgs(t, nonDanglingImageIDArgs(), []string{
		"images", "--no-trunc", "--filter", "dangling=false", "-q",
	})
}

func TestImagesByReferenceArgs(t *testing.T) {
	assertArgs(t, imagesByReferenceArgs("tengiz-apps/*"), []string{
		"images", "--no-trunc", "--filter", "reference=tengiz-apps/*", "-q",
	})
}

func TestRemoveImageArgs(t *testing.T) {
	assertArgs(t, removeImageArgs("abc123"), []string{"rmi", "-f", "abc123"})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run 'TestBuild|TestNonDangling|TestImagesByReference|TestRemoveImage' -v -count=1`

Expected: FAIL — compile error `undefined: buildContainerPruneArgs` (and the others).

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
func buildContainerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=" + labelKey}
}

func buildContainerDryRunArgs() []string {
	return []string{
		"container", "ls", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--filter", "status=dead",
		"--filter", "label!=" + labelKey,
		"--format", "{{.ID}}",
	}
}

func buildDanglingImagePruneArgs() []string {
	return []string{"image", "prune", "-f"}
}

func buildDanglingImageDryRunArgs() []string {
	return []string{"images", "--no-trunc", "--filter", "dangling=true", "-q"}
}

func buildNetworkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func buildNetworkDryRunArgs() []string {
	return []string{"network", "ls", "-q"}
}

func buildVolumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func buildVolumeDryRunArgs() []string {
	return []string{"volume", "ls", "-q"}
}

func nonDanglingImageIDArgs() []string {
	return []string{"images", "--no-trunc", "--filter", "dangling=false", "-q"}
}

func imagesByReferenceArgs(ref string) []string {
	return []string{"images", "--no-trunc", "--filter", "reference=" + ref, "-q"}
}

func removeImageArgs(id string) []string {
	return []string{"rmi", "-f", id}
}
```

(`labelKey` is already defined as `"tengiz-app"` in `internal/runtime/docker.go:76`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run 'TestBuild|TestNonDangling|TestImagesByReference|TestRemoveImage' -v -count=1`

Expected: PASS — all builder tests.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add docker prune and dry-run arg builders"
```

---

### Task 3: Pure keep-list helpers for non-dangling image pruning

Adds `parseImageIDs` (normalizes image IDs, strips `sha256:` prefix, dedupes) and `removableImageIDs` (the set of non-dangling images that are neither currently used by a container nor Tengiz-managed). These are the testable core of the `--all` image cleanup path.

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing (pure functions).
- Produces:
  - `parseImageIDs(out string) []string` — split on newlines, trim, strip `sha256:` prefix, drop empties, dedupe preserving order.
  - `removableImageIDs(all, used, protected []string) []string` — elements of `all` not present in `used` or `protected` (order preserved).

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestParseImageIDs(t *testing.T) {
	out := "sha256:abc\ndef\n\nsha256:abc\n"
	got := parseImageIDs(out)
	want := []string{"abc", "def"}
	if len(got) != len(want) {
		t.Fatalf("parseImageIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseImageIDs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseImageIDsEmpty(t *testing.T) {
	if got := parseImageIDs(""); len(got) != 0 {
		t.Fatalf("parseImageIDs(\"\") = %v, want empty", got)
	}
}

func TestRemovableImageIDs(t *testing.T) {
	all := []string{"a", "b", "c", "d"}
	used := []string{"b"}
	protected := []string{"d"}
	got := removableImageIDs(all, used, protected)
	want := []string{"a", "c"}
	if len(got) != len(want) {
		t.Fatalf("removableImageIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("removableImageIDs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRemovableImageIDsKeepsAllInUse(t *testing.T) {
	got := removableImageIDs([]string{"a", "b"}, []string{"a", "b"}, nil)
	if len(got) != 0 {
		t.Fatalf("removableImageIDs() = %v, want empty (all in use)", got)
	}
}

func TestRemovableImageIDsEmptyAll(t *testing.T) {
	if got := removableImageIDs(nil, nil, nil); len(got) != 0 {
		t.Fatalf("removableImageIDs(nil,nil,nil) = %v, want empty", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run 'TestParseImageIDs|TestRemovableImageIDs' -v -count=1`

Expected: FAIL — compile error `undefined: parseImageIDs`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
import "strings"

func parseImageIDs(out string) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		l = strings.TrimSpace(l)
		l = strings.TrimPrefix(l, "sha256:")
		if l != "" && !seen[l] {
			seen[l] = true
			ids = append(ids, l)
		}
	}
	return ids
}

func removableImageIDs(all, used, protected []string) []string {
	keep := make(map[string]bool, len(used)+len(protected))
	for _, id := range used {
		keep[id] = true
	}
	for _, id := range protected {
		keep[id] = true
	}
	var out []string
	for _, id := range all {
		if !keep[id] {
			out = append(out, id)
		}
	}
	return out
}
```

Note: `strings` is already imported at the top of `cleanup.go`? No — `cleanup.go` currently only imports `context`. Change the import block in `internal/runtime/cleanup.go` to:

```go
import (
	"context"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run 'TestParseImageIDs|TestRemovableImageIDs' -v -count=1`

Expected: PASS — all keep-list tests.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add image ID parsing and keep-list helpers"
```

---

### Task 4: Implement `Cleanup` on `dockerRuntime`

Implements the real Docker-backed `Cleanup`. For each enabled category it (1) lists candidates to count them, (2) if not dry-run, runs the actual prune. The `--all` image path uses the Task 3 keep-list to protect `tengiz-apps/*` images and in-use images.

**Files:**
- Modify: `internal/runtime/cleanup.go` (docker implementation + `os/exec` helpers)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: all builders from Task 2, `parseImageIDs`/`removableImageIDs` from Task 3.
- Produces:
  - `dockerRuntime.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)` — satisfies `Manager`.
  - `dockerRuntime.containerImageIDs(ctx) ([]string, error)` — image IDs used by all containers (`docker ps -aq` + `docker inspect --format {{.Image}}`).
  - `dockerRuntime.nonDanglingImageIDs(ctx) ([]string, error)`
  - `dockerRuntime.imagesByReference(ctx, ref string) ([]string, error)`
  - `dockerRuntime.runCombinedOutput(ctx, args []string) (string, error)`
  - `dockerRuntime.runOutputLines(ctx, args []string) ([]string, error)`
  - `removeImageArgs(id string) []string` (already produced in Task 2).

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestDockerCleanupNoCategoriesRunsNothing(t *testing.T) {
	r := &dockerRuntime{}
	rep, err := r.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if rep.Containers != 0 || rep.Images != 0 || rep.Networks != 0 || rep.Volumes != 0 {
		t.Fatalf("Cleanup() report = %+v, want all-zero counts", rep)
	}
	if rep.Output != "" {
		t.Fatalf("Cleanup() Output = %q, want empty", rep.Output)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestDockerCleanupNoCategoriesRunsNothing -v -count=1`

Expected: FAIL — compile error `r.Cleanup undefined (type *dockerRuntime has no field or method Cleanup)`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go` (the file now imports `context`, `strings`, plus `fmt`, `os/exec`):

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	report := CleanupReport{DryRun: opts.DryRun}
	var out strings.Builder

	if opts.Containers {
		lines, err := r.runOutputLines(ctx, buildContainerDryRunArgs())
		if err != nil {
			return report, err
		}
		report.Containers = len(lines)
		if opts.DryRun {
			for _, l := range lines {
				out.WriteString("container: " + l + "\n")
			}
		} else {
			o, err := r.runCombinedOutput(ctx, buildContainerPruneArgs())
			if err != nil {
				return report, err
			}
			out.WriteString(o)
		}
	}

	if opts.Images {
		dangling, err := r.runOutputLines(ctx, buildDanglingImageDryRunArgs())
		if err != nil {
			return report, err
		}
		report.Images = len(dangling)
		if opts.DryRun {
			for _, l := range dangling {
				out.WriteString("image: " + l + "\n")
			}
		} else {
			o, err := r.runCombinedOutput(ctx, buildDanglingImagePruneArgs())
			if err != nil {
				return report, err
			}
			out.WriteString(o)
		}

		if opts.AllImages {
			used, err := r.containerImageIDs(ctx)
			if err != nil {
				return report, err
			}
			protected, err := r.imagesByReference(ctx, "tengiz-apps/*")
			if err != nil {
				return report, err
			}
			all, err := r.nonDanglingImageIDs(ctx)
			if err != nil {
				return report, err
			}
			removable := removableImageIDs(all, used, protected)
			report.Images += len(removable)
			for _, id := range removable {
				if opts.DryRun {
					out.WriteString("image: " + id + "\n")
					continue
				}
				o, err := r.runCombinedOutput(ctx, removeImageArgs(id))
				if err != nil {
					return report, err
				}
				out.WriteString(o)
			}
		}
	}

	if opts.Networks {
		lines, err := r.runOutputLines(ctx, buildNetworkDryRunArgs())
		if err != nil {
			return report, err
		}
		report.Networks = len(lines)
		if opts.DryRun {
			for _, l := range lines {
				out.WriteString("network: " + l + "\n")
			}
		} else {
			o, err := r.runCombinedOutput(ctx, buildNetworkPruneArgs())
			if err != nil {
				return report, err
			}
			out.WriteString(o)
		}
	}

	if opts.Volumes {
		lines, err := r.runOutputLines(ctx, buildVolumeDryRunArgs())
		if err != nil {
			return report, err
		}
		report.Volumes = len(lines)
		if opts.DryRun {
			for _, l := range lines {
				out.WriteString("volume: " + l + "\n")
			}
		} else {
			o, err := r.runCombinedOutput(ctx, buildVolumePruneArgs())
			if err != nil {
				return report, err
			}
			out.WriteString(o)
		}
	}

	report.Output = out.String()
	return report, nil
}

func (r *dockerRuntime) runCombinedOutput(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) runOutputLines(ctx context.Context, args []string) ([]string, error) {
	out, err := r.runCombinedOutput(ctx, args)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func (r *dockerRuntime) containerImageIDs(ctx context.Context) ([]string, error) {
	cids, err := r.runOutputLines(ctx, []string{"ps", "-aq"})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var ids []string
	for _, cid := range cids {
		cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Image}}", cid)
		b, err := cmd.CombinedOutput()
		if err != nil {
			continue
		}
		for _, id := range parseImageIDs(string(b)) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}

func (r *dockerRuntime) nonDanglingImageIDs(ctx context.Context) ([]string, error) {
	return r.runOutputLines(ctx, nonDanglingImageIDArgs())
}

func (r *dockerRuntime) imagesByReference(ctx context.Context, ref string) ([]string, error) {
	return r.runOutputLines(ctx, imagesByReferenceArgs(ref))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: PASS — all runtime tests including `TestDockerCleanupNoCategoriesRunsNothing`.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker Cleanup with tengiz image protection"
```

---

### Task 5: Label Tengiz images at build time

Adds `--label tengiz-app=<appName>` to `docker build` so every Tengiz-built image carries the same label used to protect containers. This is defense-in-depth: the `--all` keep-list (Task 3/4) already protects `tengiz-apps/*` images by reference regardless of label origin. Labels only need to be added on the Dockerfile build path; Nixpacks-built images remain protected by the keep-list.

**Files:**
- Modify: `internal/builder/builder.go:57-91` (`buildWithDockerfile`)
- Create: `internal/builder/builder_test.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `buildDockerfileArgs(appName, env, deploymentID string, secretArgs []string, dir string) []string` — a pure builder of the `docker build` invocation, including `--label tengiz-app=<appName>`, so it is unit-testable.

- [ ] **Step 1: Write the failing test**

Create `internal/builder/builder_test.go`:

```go
package builder

import (
	"strings"
	"testing"
)

func TestBuildDockerfileArgsIncludesAppLabel(t *testing.T) {
	args := buildDockerfileArgs("myapp", "production", "123", nil, "/tmp/proj")
	got := strings.Join(args, " ")
	for _, want := range []string{
		"--label",
		"tengiz-app=myapp",
		"-t",
		"tengiz-apps/myapp:production-123",
		"/tmp/proj",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("buildDockerfileArgs() = %q, missing %q", got, want)
		}
	}
}

func TestBuildDockerfileArgsIncludesSecrets(t *testing.T) {
	secrets := []string{"--secret", "id=npm_token,src=/tmp/tok"}
	args := buildDockerfileArgs("myapp", "prod", "1", secrets, ".")
	got := strings.Join(args, " ")
	if !strings.Contains(got, "--secret id=npm_token,src=/tmp/tok") {
		t.Errorf("buildDockerfileArgs() = %q, missing build secret args", got)
	}
}

func TestBuildDockerfileArgsDefaultsEnv(t *testing.T) {
	args := buildDockerfileArgs("myapp", "", "7", nil, ".")
	got := strings.Join(args, " ")
	if !strings.Contains(got, "-t tengiz-apps/myapp:production-7") {
		t.Errorf("buildDockerfileArgs() = %q, want default production env tag", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/builder/... -run TestBuildDockerfileArgs -v -count=1`

Expected: FAIL — compile error `undefined: buildDockerfileArgs`.

- [ ] **Step 3: Write minimal implementation**

In `internal/builder/builder.go`, add the pure builder (before `buildWithDockerfile`):

```go
func buildDockerfileArgs(appName, env, deploymentID string, secretArgs []string, dir string) []string {
	if env == "" {
		env = "production"
	}
	tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)
	args := []string{"build"}
	args = append(args, secretArgs...)
	args = append(args, "--label", fmt.Sprintf("tengiz-app=%s", appName))
	args = append(args, "-t", tag, dir)
	return args
}
```

Replace the arg construction inside `buildWithDockerfile` (`builder.go:69-71`):

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

with:

```go
	args := buildDockerfileArgs(appName, env, deploymentID, b.buildSecretArgs(), dir)
```

The `tag` variable (`builder.go:61`) is still used below for `docker tag` — keep it. After this edit, the function reads:

```go
	tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

	cleanup, err := b.writeBuildSecrets()
	if err != nil {
		return "", "", err
	}
	defer cleanup()

	args := buildDockerfileArgs(appName, env, deploymentID, b.buildSecretArgs(), dir)

	cmd := exec.CommandContext(ctx, "docker", args...)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/builder/... -v -count=1`

Expected: PASS — all three `TestBuildDockerfileArgs*` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat(builder): label tengiz images with tengiz-app label"
```

---

### Task 6: CLI `tengiz cleanup` command

Adds the `tengiz cleanup` Cobra command with flags `--all`, `--volumes`, `--dry-run`, `--containers`, `--images`, `--networks`. Category defaulting (clean containers + images + networks when no category flag is set; never default volumes) lives in a pure `resolveCleanupOptions` function for testability.

**Files:**
- Modify: `internal/cli/root.go:34-89` (register command + flags), add `cleanupCmd` near `psCmd`
- Modify: `internal/cli/root_test.go`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.NewDocker()`, `runtime.Manager.Cleanup`.
- Produces:
  - `resolveCleanupOptions(containers, images, networks, volumes, allImages, dryRun bool) runtime.CleanupOptions`
  - `cleanupCmd *cobra.Command` (registered on `rootCmd`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

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
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"all", "volumes", "dry-run", "containers", "images", "networks"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestResolveCleanupOptionsDefaults(t *testing.T) {
	opts := resolveCleanupOptions(false, false, false, false, false, false)
	if !opts.Containers || !opts.Images || !opts.Networks {
		t.Errorf("defaults: want containers+images+networks, got %+v", opts)
	}
	if opts.Volumes || opts.AllImages || opts.DryRun {
		t.Errorf("defaults: volumes/all/dry-run must be false, got %+v", opts)
	}
}

func TestResolveCleanupOptionsExplicitCategories(t *testing.T) {
	opts := resolveCleanupOptions(true, false, false, true, true, true)
	if !opts.Containers || opts.Images || opts.Networks || !opts.Volumes || !opts.AllImages || !opts.DryRun {
		t.Errorf("explicit opts = %+v, want Containers+Volumes+AllImages+DryRun only", opts)
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	var got runtime.CleanupOptions
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		networks, _ := cmd.Flags().GetBool("networks")
		volumes, _ := cmd.Flags().GetBool("volumes")
		all, _ := cmd.Flags().GetBool("all")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		got = resolveCleanupOptions(containers, images, networks, volumes, all, dryRun)
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--containers", "--images", "--all", "--dry-run", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !got.Containers || !got.Images || !got.Volumes || !got.AllImages || !got.DryRun {
		t.Errorf("flag parsing => %+v, want all selected categories true", got)
	}
	if got.Networks {
		t.Errorf("flag parsing => Networks = true, want false (not passed)")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run 'TestCleanup|TestResolveCleanup' -v -count=1`

Expected: FAIL — compile error `undefined: cleanupCmd` / `undefined: resolveCleanupOptions`.

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/root.go`, add the pure helper (place it near `getEnv`, after line 103):

```go
func resolveCleanupOptions(containers, images, networks, volumes, allImages, dryRun bool) runtime.CleanupOptions {
	opts := runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		AllImages:  allImages,
		DryRun:     dryRun,
	}
	if !containers && !images && !networks && !volumes {
		opts.Containers = true
		opts.Images = true
		opts.Networks = true
	}
	return opts
}
```

Add the command definition after `psCmd` (after line 601):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Remove stopped non-Tengiz containers, dangling images, unused networks and
volumes. Tengiz-managed containers and images are always preserved.

Flags:
  --all        also remove unused non-dangling images (Tengiz images preserved)
  --volumes    also remove unused Docker volumes (opt-in: volumes can hold data)
  --dry-run    show what would be removed without removing anything
  --containers only clean stopped containers
  --images     only clean unused images
  --networks   only clean unused networks

If none of --containers/--images/--networks/--volumes is set, containers,
images, and networks are all cleaned. Volumes are never cleaned implicitly.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		networks, _ := cmd.Flags().GetBool("networks")
		volumes, _ := cmd.Flags().GetBool("volumes")
		all, _ := cmd.Flags().GetBool("all")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts := resolveCleanupOptions(containers, images, networks, volumes, all, dryRun)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		mode := "removed"
		if report.DryRun {
			mode = "would remove"
		}
		fmt.Printf("[tengiz] %s: %d containers, %d images, %d networks, %d volumes\n",
			mode, report.Containers, report.Images, report.Networks, report.Volumes)
		if report.Output != "" {
			fmt.Print(report.Output)
		}
		return nil
	},
}
```

Register it in `init()` (after `rootCmd.AddCommand(psCmd)` at `root.go:42`):

```go
	rootCmd.AddCommand(cleanupCmd)
```

And register the flags at the end of `init()` (after the existing flag registrations, before the closing brace at line 89):

```go
	cleanupCmd.Flags().Bool("all", false, "also remove unused non-dangling images (Tengiz images preserved)")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused Docker volumes (opt-in: volumes can hold data)")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "only clean stopped containers")
	cleanupCmd.Flags().Bool("images", false, "only clean unused images")
	cleanupCmd.Flags().Bool("networks", false, "only clean unused networks")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -v -count=1`

Expected: PASS — all CLI tests including the new cleanup tests.

- [ ] **Step 5: Full verification + commit**

```bash
go vet ./...
go test ./... -v -count=1
```

Expected: both PASS. Then:

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 7: Documentation (README, FUTURES_FEATURES, AGENTS)

Documents the new command and marks the feature as implemented.

**Files:**
- Modify: `README.md` (add `### tengiz cleanup` after the `### tengiz rollback` section, i.e. between lines 236 and 238)
- Modify: `docs/FUTURES_FEATURES.md` (mark #6 implemented; add entry to the implemented table; add Status line to the feature detail section)
- Modify: `AGENTS.md` (add `tengiz cleanup` to the CLI list)

**Interfaces:**
- Consumes: the CLI surface from Task 6.

- [ ] **Step 1: Add the README CLI reference section**

In `README.md`, insert after the `### tengiz rollback <app>` section (after line 236) and before `### tengiz domain` (line 238):

```markdown
### `tengiz cleanup`

Reclaim disk space by removing unused Docker resources. Tengiz-managed containers and images are always preserved.

| Flag | Description |
|------|-------------|
| `--all` | Also remove unused non-dangling images (Tengiz images preserved) |
| `--volumes` | Also remove unused Docker volumes (opt-in — volumes can hold data) |
| `--dry-run` | Show what would be removed without removing anything |
| `--containers` | Only clean stopped containers |
| `--images` | Only clean unused images |
| `--networks` | Only clean unused networks |

If none of `--containers`/`--images`/`--networks`/`--volumes` is set, containers, images, and networks are all cleaned. Volumes are never cleaned implicitly.

Examples:
```
tengiz cleanup
tengiz cleanup --dry-run
tengiz cleanup --all --volumes
```
```

- [ ] **Step 2: Update `docs/FUTURES_FEATURES.md`**

1. In the P0 table, change row #6 (`Docker Housekeeping`) from `⬜` to `✅ Implemented (2026-08-20)`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

2. In the "✅ Implemented Features (Not Pending)" table (line ~237), add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-20) |
```

3. In the `## Docker Housekeeping (Otomatik Temizlik)` feature detail section (line ~377), add a Status line after `- **Detected:** 2026-07-14`:

```markdown
- **Status:** ✅ Implemented (2026-08-20)
```

- [ ] **Step 3: Update `AGENTS.md` CLI list**

In the CLI block, add `tengiz cleanup` after the `tengiz proxy` line:

```
tengiz proxy [-a app] → start reverse proxy on :8080 (use -a to route all traffic to one app)
tengiz cleanup       → prune unused Docker resources (containers/images/networks, opt-in volumes)
```

- [ ] **Step 4: Verify build**

Run: `go build -o tengiz .`

Expected: binary builds successfully.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**Spec coverage (from docs/FUTURES_FEATURES.md #6 "Docker Housekeeping"):**
- "Label-based `docker system prune`" → Task 2 (`label!=tengiz-app` filters) + Task 5 (build-time `tengiz-app` label). ✅
- "`tengiz cleanup`" → Task 6. ✅
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → containers/images/networks by default, volumes opt-in via `--volumes` (Task 4/6). ✅ Periodic scheduling deliberately excluded — YAGNI for the P0 scope, noted as future work. ✅
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → `label!=tengiz-app` protects all Tengiz containers (Task 2/4) + reference keep-list protects `tengiz-apps/*` images on `--all` (Task 3/4). ✅

**Placeholder scan:** No TBD/TODO/"similar to Task N" references; every step contains concrete code and expected output. ✅

**Type consistency:**
- `CleanupOptions`/`CleanupReport` fields defined once (Task 1) and reused identically in Tasks 2-6. ✅
- `labelKey` referenced from docker.go:76 (already exists). ✅
- Builder helper `buildDockerfileArgs(appName, env, deploymentID string, secretArgs []string, dir string) []string` signature used identically in the test (Task 5) and the implementation. ✅
- `resolveCleanupOptions(containers, images, networks, volumes, allImages, dryRun bool) runtime.CleanupOptions` signature identical in test and implementation (Task 6). ✅
- `buildContainerPruneArgs`/`buildContainerDryRunArgs` etc. called with no args in both `dockerRuntime.Cleanup` (Task 4) and tests (Task 2). ✅
- `docker runtime` method name collision check: no existing `Cleanup`/`runCombinedOutput`/`runOutputLines`/`imagesByReference` in the package. ✅

**Out of scope (documented, not implemented):** periodic `DockerCleanupJob` scheduling; per-category granular CLI for buildx cache (FUTURES_FEATURES #56). Both listed as future work if needed.
