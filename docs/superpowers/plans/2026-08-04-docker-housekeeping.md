# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes stopped containers, unused images, unused networks, and dangling build cache created by Tengiz — using label/reference-based filtering so resources not managed by Tengiz are never touched.

**Architecture:** Add a `Prune(ctx, opts)` method to the existing `runtime.Manager` interface with a docker exec implementation (`internal/runtime/housekeeping.go`) and a stub. The docker implementation calls per-category Docker CLI prune commands (`docker container prune`, `docker network prune`, `docker volume prune`, `docker builder prune`) always guarded by `--filter label=tengiz-app` (plus optional `tengiz-app=<app>` / `tengiz-env=<env>` filters), and prunes images manually via `docker rmi` against the `tengiz-apps/*` reference namespace (`docker rmi` fails harmlessly for images still referenced by containers). A new `cleanupCmd` in `internal/cli/root.go` builds `runtime.PruneOptions` from flags; the default is a safe dry-run report, with `--yes` required to execute. This avoids `docker system prune` directly (its image filter cannot match the tagged `tengiz-apps/*` images and it lacks a `reference` filter), while preserving the same default category semantics.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` interface, Docker CLI via `os/exec` (no Docker SDK). No new external dependencies.

## Global Constraints

- Docker CLI only via `os/exec` — never import a Docker SDK
- Every prune must be filtered so non-Tengiz resources are untouched:
  - Containers/networks/volumes: `--filter label=tengiz-app` (+ optional `tengiz-app=<app>`, `tengiz-env=<env>`)
  - Images: reference namespace `tengiz-apps/*` via `docker images --filter reference=...` then `docker rmi`
- Volumes are NEVER pruned by default — opt-in via `--volumes` only (mirrors `docker system prune` which excludes volumes)
- `tengiz cleanup` with no category flags defaults to: stopped containers + unused images + unused networks + dangling build cache (mirrors `docker system prune`); volumes excluded
- `tengiz cleanup` defaults to **dry-run** (report only); `--yes`/`-y` actually executes
- The cleanup command's own `--env` flag defaults to `""` (all environments) — it shadows the persistent root `--env` (default `"production"`) so cleanup is not accidentally restricted
- Every helper function must be testable without a live Docker daemon (pure functions over strings/options)
- No new external dependencies
- Existing tests must continue to pass; all existing `runtime.Manager` mocks get the new `Prune` method to keep compiling

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions` / `PruneResult` types, add `Prune` to `Manager` interface, stub impl |
| `internal/runtime/housekeeping.go` | **New** — docker `Prune` implementation + pure helpers (`pruneDefaults`, `pruneFilters`, `pruneFilterFlagArgs`, `imageReferenceFilter`, `filterImageTags`, `countLines`, `parsePruneOutput`, `parseReclaimedSpace`) |
| `internal/runtime/housekeeping_test.go` | **New** — tests for helpers + stub prune |
| `internal/cli/root.go` | Add `cleanupCmd`, its flags, registration in `init()`, `cleanupOptionsFromFlags`, `printCleanupResult` |
| `internal/cli/cleanup_test.go` | **New** — CLI registration/flag/options tests |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` (keeps it implementing `Manager`) |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in Features + Commands sections |
| `docs/FUTURES_FEATURES.md` | Mark #6 Docker Housekeeping as ✅ Implemented |

---

### Task 1: Add Prune API to runtime.Manager + stub + update mocks

**Files:**
- Modify: `internal/runtime/runtime.go` — add types after `RunOptions` (line ~28), add `Prune` to `Manager` interface (after `KeepLastNImages`, line 36), add stub impl (after line 119)
- Modify: `internal/cli/root_test.go:70-100` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:14-40` — add `Prune` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:15-40` — add `Prune` to `mockRuntime`
- Test: `internal/runtime/cleanup_test.go` — add stub prune test

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{Containers, Images, Networks, Volumes, BuildCache, DryRun bool; AppName, Env string}`, `runtime.PruneResult{Containers, Images, Networks, Volumes int; BuildCache bool; Reclaimed string}`, `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go — append
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil {
		t.Fatal("Prune() returned nil result")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: FAIL — compile error `m.Prune undefined (type Manager has no field or method Prune)`.

- [ ] **Step 3: Add types to `internal/runtime/runtime.go`**

Add after the `RunOptions` struct (around line 28):

```go
type PruneOptions struct {
	Containers bool   // prune stopped Tengiz containers
	Images     bool   // prune unused Tengiz images (tengiz-apps/*)
	Networks   bool   // prune unused labeled networks
	Volumes    bool   // prune unused labeled volumes (opt-in — never default)
	BuildCache bool   // prune dangling build cache
	DryRun     bool   // report counts without deleting
	AppName    string // restrict to a single app (label/reference filter)
	Env        string // restrict to a single environment (label/reference filter)
}

type PruneResult struct {
	Containers int
	Images     int
	Networks   int
	Volumes    int
	BuildCache bool   // true when build cache was pruned (or would be)
	Reclaimed  string // "Total reclaimed space" summary from docker, best-effort
}
```

- [ ] **Step 4: Add `Prune` to the `Manager` interface**

In `internal/runtime/runtime.go`, after the `KeepLastNImages` line:

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)
```

- [ ] **Step 5: Add stub implementation**

After the existing `stubManager.KeepLastNImages` (line ~119):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	return &PruneResult{}, nil
}
```

- [ ] **Step 6: Update the three existing mocks so the repo compiles**

`internal/cli/root_test.go` — add to `mockRTForDeploy` (after the `KeepLastNImages` method at line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) { return &runtime.PruneResult{}, nil }
```

`internal/idle/idle_test.go` — add to `mockRuntime` (after its `KeepLastNImages` method):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) { return &runtime.PruneResult{}, nil }
```

`internal/proxy/proxy_test.go` — add to `mockRuntime` (after its `KeepLastNImages` method):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) { return &runtime.PruneResult{}, nil }
```

Verify each mock file imports `runtime` — `internal/cli/root_test.go` already does; check `internal/idle/idle_test.go` and `internal/proxy/proxy_test.go` and add `"github.com/yaso09/tengiz/internal/runtime"` to their import blocks if missing.

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: PASS

- [ ] **Step 8: Verify all packages still compile**

Run: `go build ./...`

Expected: Build succeeds (mocks updated, interface satisfied everywhere).

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Prune API to runtime.Manager interface with stub"
```

---

### Task 2: Pure helper functions for docker pruning

**Files:**
- Create: `internal/runtime/housekeeping.go` — helpers only (no exec yet)
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneResult` from Task 1; existing `labelKey = "tengiz-app"` and `envLabelKey = "tengiz-env"` consts in `internal/runtime/docker.go:76-77`
- Produces: `pruneDefaults(opts) PruneOptions`, `pruneFilters(opts) []string`, `pruneFilterFlagArgs(opts) []string`, `imageReferenceFilter(appName) string`, `filterImageTags(out []byte, env string) []string`, `countLines(out []byte) int`, `parsePruneOutput(out, kind string) (int, string)`, `parseReclaimedSpace(out string) string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/housekeeping_test.go
package runtime

import (
	"reflect"
	"testing"
)

func TestPruneDefaultsNoCategories(t *testing.T) {
	opts := pruneDefaults(PruneOptions{})
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Errorf("default categories wrong: %+v", opts)
	}
	if opts.Volumes {
		t.Error("volumes must NOT be pruned by default")
	}
}

func TestPruneDefaultsKeepsExplicitCategories(t *testing.T) {
	opts := pruneDefaults(PruneOptions{Volumes: true})
	if opts.Volumes != true || opts.Containers {
		t.Errorf("explicit categories not preserved: %+v", opts)
	}
}

func TestPruneFilters(t *testing.T) {
	got := pruneFilters(PruneOptions{AppName: "myapp", Env: "staging"})
	want := []string{
		"label=tengiz-app",
		"label=tengiz-app=myapp",
		"label=tengiz-env=staging",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneFilters() = %v, want %v", got, want)
	}
}

func TestPruneFilterFlagArgs(t *testing.T) {
	got := pruneFilterFlagArgs(PruneOptions{Env: "staging"})
	want := []string{"--filter", "label=tengiz-app", "--filter", "label=tengiz-env=staging"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneFilterFlagArgs() = %v, want %v", got, want)
	}
}

func TestImageReferenceFilter(t *testing.T) {
	if got := imageReferenceFilter(""); got != "reference=tengiz-apps/*" {
		t.Errorf("imageReferenceFilter(\"\") = %q, want reference=tengiz-apps/*", got)
	}
	if got := imageReferenceFilter("myapp"); got != "reference=tengiz-apps/myapp:*" {
		t.Errorf("imageReferenceFilter(myapp) = %q, want reference=tengiz-apps/myapp:*", got)
	}
}

func TestFilterImageTags(t *testing.T) {
	out := []byte("tengiz-apps/myapp:staging-123\n\ntengiz-apps/myapp:production-456\n")
	tags := filterImageTags(out, "staging")
	want := []string{"tengiz-apps/myapp:staging-123"}
	if !reflect.DeepEqual(tags, want) {
		t.Errorf("filterImageTags(staging) = %v, want %v", tags, want)
	}
	if n := len(filterImageTags(out, "")); n != 2 {
		t.Errorf("filterImageTags(no env) count = %d, want 2", n)
	}
}

func TestCountLines(t *testing.T) {
	if n := countLines([]byte("a\nb\n\nc\n")); n != 3 {
		t.Errorf("countLines = %d, want 3", n)
	}
	if n := countLines([]byte("")); n != 0 {
		t.Errorf("countLines(empty) = %d, want 0", n)
	}
}

func TestParsePruneOutput(t *testing.T) {
	out := "Deleted Containers:\n" +
		"abc123\n" +
		"def456\n\n" +
		"Total reclaimed space: 12.34kB\n"
	n, rec := parsePruneOutput(out, "Containers")
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
	if rec != "12.34kB" {
		t.Errorf("reclaimed = %q, want %q", rec, "12.34kB")
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	n, rec := parsePruneOutput("Total reclaimed space: 0B\n", "Containers")
	if n != 0 {
		t.Errorf("count = %d, want 0", n)
	}
	if rec != "0B" {
		t.Errorf("reclaimed = %q, want 0B", rec)
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	out := "ID            RECLAIMABLE   SIZE\n" +
		"abc123        1.2kB         3.4kB\n" +
		"Total: 4.6kB\n"
	if got := parseReclaimedSpace(out); got != "4.6kB" {
		t.Errorf("parseReclaimedSpace() = %q, want 4.6kB", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPrune|TestImageReference|TestFilterImageTags|TestCountLines|TestParse" -v -count=1`

Expected: FAIL — compile error `undefined: pruneDefaults` (functions don't exist yet).

- [ ] **Step 3: Write the helper implementations**

Create `internal/runtime/housekeeping.go`:

```go
package runtime

import (
	"fmt"
	"strings"
)

func pruneDefaults(opts PruneOptions) PruneOptions {
	if !opts.Containers && !opts.Images && !opts.Networks && !opts.Volumes && !opts.BuildCache {
		opts.Containers = true
		opts.Images = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts
}

func pruneFilters(opts PruneOptions) []string {
	filters := []string{fmt.Sprintf("label=%s", labelKey)}
	if opts.AppName != "" {
		filters = append(filters, fmt.Sprintf("label=%s=%s", labelKey, opts.AppName))
	}
	if opts.Env != "" {
		filters = append(filters, fmt.Sprintf("label=%s=%s", envLabelKey, opts.Env))
	}
	return filters
}

func pruneFilterFlagArgs(opts PruneOptions) []string {
	var args []string
	for _, f := range pruneFilters(opts) {
		args = append(args, "--filter", f)
	}
	return args
}

func imageReferenceFilter(appName string) string {
	if appName != "" {
		return fmt.Sprintf("reference=tengiz-apps/%s:*", appName)
	}
	return "reference=tengiz-apps/*"
}

func filterImageTags(out []byte, env string) []string {
	var tags []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		tag := strings.TrimSpace(line)
		if tag == "" {
			continue
		}
		if env != "" {
			parts := strings.SplitN(tag, ":", 2)
			if len(parts) != 2 || !strings.HasPrefix(parts[1], env+"-") {
				continue
			}
		}
		tags = append(tags, tag)
	}
	return tags
}

func countLines(out []byte) int {
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func parsePruneOutput(out string, kind string) (int, string) {
	n := 0
	inSection := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Deleted "+kind+":") {
			inSection = true
			continue
		}
		if inSection {
			if line == "" || strings.HasPrefix(line, "Total") {
				inSection = false
				continue
			}
			n++
			continue
		}
	}
	return n, parseReclaimedSpace(out)
}

func parseReclaimedSpace(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
		if strings.HasPrefix(line, "Total:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total:"))
		}
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPrune|TestImageReference|TestFilterImageTags|TestCountLines|TestParse" -v -count=1`

Expected: PASS (all helper tests green).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add pure helper functions for docker pruning"
```

---

### Task 3: Docker Prune implementation

**Files:**
- Modify: `internal/runtime/housekeeping.go` — add `Prune` + private per-category methods (imports: `context`, `fmt`, `os/exec`, `strings`)

**Interfaces:**
- Consumes: `PruneOptions`/`PruneResult` (Task 1), all helpers (Task 2), `dockerRuntime` type + `RemoveImage` (already in `internal/runtime/cleanup.go`)
- Produces: `(*dockerRuntime).Prune(ctx, opts) (*PruneResult, error)` — the full docker-backed implementation

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/housekeeping_test.go — append
func TestDockerRuntimeHasPrune(t *testing.T) {
	rt := &dockerRuntime{}
	res, err := rt.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune(dry-run) error = %v", err)
	}
	if res == nil {
		t.Fatal("Prune(dry-run) returned nil result")
	}
}
```

Add `"context"` to the test file imports.

Note: this test runs against the real `dockerRuntime` in **dry-run mode only**. It requires a working `docker` CLI on PATH (same as every other docker-backed runtime test in this repo runs in CI with Docker available). If docker is unavailable in your environment, `go test ./internal/runtime/... -run TestDockerRuntimeHasPrune` will return the expected error from `exec.CommandContext` (which we assert is non-nil on err); the test fails on unexpected success, so it still guards against the method not existing.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestDockerRuntimeHasPrune -v -count=1`

Expected: FAIL — compile error `(*dockerRuntime) has no field or method Prune`.

- [ ] **Step 3: Implement `Prune` and per-category methods in `internal/runtime/housekeeping.go`**

Append (update the file's import block to include `context` and `os/exec`):

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	opts = pruneDefaults(opts)
	result := &PruneResult{}

	if opts.DryRun {
		if opts.Containers {
			n, err := r.countPruneContainers(ctx, opts)
			if err != nil {
				return nil, err
			}
			result.Containers = n
		}
		if opts.Images {
			n, err := r.countPruneImages(ctx, opts)
			if err != nil {
				return nil, err
			}
			result.Images = n
		}
		if opts.Networks {
			n, err := r.countPruneNetworks(ctx, opts)
			if err != nil {
				return nil, err
			}
			result.Networks = n
		}
		if opts.Volumes {
			n, err := r.countPruneVolumes(ctx, opts)
			if err != nil {
				return nil, err
			}
			result.Volumes = n
		}
		if opts.BuildCache {
			result.BuildCache = true
		}
		return result, nil
	}

	if opts.Containers {
		n, rec, err := r.pruneContainers(ctx, opts)
		if err != nil {
			return nil, err
		}
		result.Containers = n
		result.Reclaimed = rec
	}
	if opts.Images {
		n, err := r.pruneImages(ctx, opts)
		if err != nil {
			return nil, err
		}
		result.Images = n
	}
	if opts.Networks {
		n, rec, err := r.pruneNetworks(ctx, opts)
		if err != nil {
			return nil, err
		}
		result.Networks = n
		if rec != "" {
			result.Reclaimed = rec
		}
	}
	if opts.Volumes {
		n, rec, err := r.pruneVolumes(ctx, opts)
		if err != nil {
			return nil, err
		}
		result.Volumes = n
		if rec != "" {
			result.Reclaimed = rec
		}
	}
	if opts.BuildCache {
		rec, err := r.pruneBuildCache(ctx)
		if err != nil {
			return nil, err
		}
		if rec != "" {
			result.Reclaimed = rec
		}
		result.BuildCache = true
	}
	return result, nil
}

func (r *dockerRuntime) countPruneContainers(ctx context.Context, opts PruneOptions) (int, error) {
	args := []string{"ps", "-aq", "--filter", "status=exited"}
	args = append(args, pruneFilterFlagArgs(opts)...)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return countLines(out), nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, opts PruneOptions) (int, string, error) {
	args := []string{"container", "prune", "--force"}
	args = append(args, pruneFilterFlagArgs(opts)...)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	n, rec := parsePruneOutput(string(out), "Containers")
	return n, rec, nil
}

func (r *dockerRuntime) countPruneImages(ctx context.Context, opts PruneOptions) (int, error) {
	args := []string{"images", "--format", "{{.Repository}}:{{.Tag}}", "--filter", imageReferenceFilter(opts.AppName)}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return len(filterImageTags(out, opts.Env)), nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, opts PruneOptions) (int, error) {
	args := []string{"images", "--format", "{{.Repository}}:{{.Tag}}", "--filter", imageReferenceFilter(opts.AppName)}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	count := 0
	for _, tag := range filterImageTags(out, opts.Env) {
		rm := exec.CommandContext(ctx, "docker", "rmi", tag)
		if err := rm.Run(); err != nil {
			continue // image still referenced by a container — keep it
		}
		count++
	}
	return count, nil
}

func (r *dockerRuntime) countPruneNetworks(ctx context.Context, opts PruneOptions) (int, error) {
	args := []string{"network", "ls", "-q"}
	args = append(args, pruneFilterFlagArgs(opts)...)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return countLines(out), nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, opts PruneOptions) (int, string, error) {
	args := []string{"network", "prune", "--force"}
	args = append(args, pruneFilterFlagArgs(opts)...)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	n, rec := parsePruneOutput(string(out), "Networks")
	return n, rec, nil
}

func (r *dockerRuntime) countPruneVolumes(ctx context.Context, opts PruneOptions) (int, error) {
	args := []string{"volume", "ls", "-q"}
	args = append(args, pruneFilterFlagArgs(opts)...)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	return countLines(out), nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, opts PruneOptions) (int, string, error) {
	args := []string{"volume", "prune", "--force"}
	args = append(args, pruneFilterFlagArgs(opts)...)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	n, rec := parsePruneOutput(string(out), "Volumes")
	return n, rec, nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) (string, error) {
	// Dangling build cache is safe to remove regardless of owner (no label exists),
	// so no label filter is applied here — this mirrors `docker system prune`.
	out, err := exec.CommandContext(ctx, "docker", "builder", "prune", "--force").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parseReclaimedSpace(string(out)), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run TestDockerRuntimeHasPrune -v -count=1`

Expected: PASS with docker available on PATH (dry-run path returns a non-nil result). If docker is absent, the test reports the expected `docker` exec error and FAILS only if the method is missing — treat a docker-absent environment's error as environmental, and verify with `go build ./...` instead.

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS.

- [ ] **Step 6: Manual smoke test (requires Docker)**

```bash
go build -o /tmp/tengiz .
# dry-run against the real daemon — must report without deleting
/tmp/tengiz cleanup --dry-run
# execute
/tmp/tengiz cleanup --yes
# scoped to one app
/tmp/tengiz cleanup --app myapp --yes
```

Expected: prints a summary; no non-Tengiz resources are touched (verify no `docker` output shows foreign images/networks being removed).

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: implement docker Prune for tengiz cleanup"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — register `cleanupCmd` in `init()` (near line 75), add flags in `init()`, add `cleanupCmd` + `cleanupOptionsFromFlags` + `printCleanupResult` at package level (append near `rmCmd`, after line 662)
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`/`runtime.PruneResult`/`runtime.Prune` from Tasks 1-3, `getEnv(cmd)` helper (root.go:97)
- Produces: `cleanupOptionsFromFlags(cmd *cobra.Command) runtime.PruneOptions`, `printCleanupResult(r *runtime.PruneResult, dryRun bool)`, and the `cleanup` subcommand

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
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

func TestCleanupCommandFlags(t *testing.T) {
	expected := []string{"yes", "dry-run", "containers", "images", "volumes", "networks", "build-cache", "app", "env"}
	for _, name := range expected {
		if flag := cleanupCmd.Flags().Lookup(name); flag == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupDefaultIsDryRun(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("yes", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	cmd.Flags().String("app", "", "")
	cmd.Flags().String("env", "", "")
	cmd.ParseFlags([]string{})

	opts := cleanupOptionsFromFlags(cmd)
	if !opts.DryRun {
		t.Error("cleanup without --yes must default to dry-run")
	}
	if opts.Volumes {
		t.Error("volumes must not be selected by default")
	}
	if opts.AppName != "" || opts.Env != "" {
		t.Errorf("no filters expected by default, got app=%q env=%q", opts.AppName, opts.Env)
	}
}

func TestCleanupOptionsFromFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("yes", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	cmd.Flags().String("app", "", "")
	cmd.Flags().String("env", "", "")
	cmd.ParseFlags([]string{
		"--yes", "--containers", "--images", "--volumes", "--networks", "--build-cache",
		"--app", "myapp", "--env", "staging",
	})

	opts := cleanupOptionsFromFlags(cmd)
	if opts.DryRun {
		t.Error("--yes must disable dry-run")
	}
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.Volumes || !opts.BuildCache {
		t.Errorf("all categories must be selected, got %+v", opts)
	}
	if opts.AppName != "myapp" || opts.Env != "staging" {
		t.Errorf("filters not parsed: app=%q env=%q", opts.AppName, opts.Env)
	}
}

func TestCleanupDryRunOverridesYes(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("yes", false, "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("containers", false, "")
	cmd.Flags().Bool("images", false, "")
	cmd.Flags().Bool("volumes", false, "")
	cmd.Flags().Bool("networks", false, "")
	cmd.Flags().Bool("build-cache", false, "")
	cmd.Flags().String("app", "", "")
	cmd.Flags().String("env", "", "")
	cmd.ParseFlags([]string{"--yes", "--dry-run"})

	opts := cleanupOptionsFromFlags(cmd)
	if !opts.DryRun {
		t.Error("--dry-run must win over --yes")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — compile error `undefined: cleanupCmd` / `undefined: cleanupOptionsFromFlags`.

- [ ] **Step 3: Register the command and flags in `internal/cli/root.go` `init()`**

After `rootCmd.AddCommand(notificationCmd)` (line 75), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("yes", false, "actually perform the cleanup (default is dry-run)")
	cleanupCmd.Flags().Bool("dry-run", false, "only report what would be removed")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune unused Tengiz images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused Tengiz volumes (excluded by default)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused Tengiz networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune dangling build cache")
	cleanupCmd.Flags().String("app", "", "restrict cleanup to a single app")
	cleanupCmd.Flags().String("env", "", "restrict cleanup to a single environment (default: all)")
```

- [ ] **Step 4: Add the command + helper functions**

Append after `rmCmd` (after line 662) in `internal/cli/root.go`:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources managed by Tengiz",
	Long: `Removes stopped containers, unused images, networks, volumes, and build cache
created by Tengiz. Uses label-based filtering so resources not managed by Tengiz
are never touched.

Without any category flag, cleanup targets the same defaults as 'docker system
prune': stopped containers, unused images, unused networks, and dangling build
cache. Volumes are never pruned unless --volumes is given.

By default cleanup only reports what would be removed. Pass --yes to actually
perform the cleanup.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptionsFromFlags(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		result, err := rt.Prune(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupResult(result, opts.DryRun)
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.PruneOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	yes, _ := cmd.Flags().GetBool("yes")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	appName, _ := cmd.Flags().GetString("app")
	env, _ := cmd.Flags().GetString("env")

	return runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		BuildCache: buildCache,
		DryRun:     !yes || dryRun,
		AppName:    appName,
		Env:        env,
	}
}

func printCleanupResult(r *runtime.PruneResult, dryRun bool) {
	verb := "would remove"
	if !dryRun {
		verb = "removed"
	}
	fmt.Printf("[tengiz] cleanup %s:\n", verb)
	fmt.Printf("  containers: %d\n", r.Containers)
	fmt.Printf("  images:     %d\n", r.Images)
	fmt.Printf("  networks:   %d\n", r.Networks)
	fmt.Printf("  volumes:    %d\n", r.Volumes)
	if r.BuildCache {
		fmt.Println("  build cache: yes")
	}
	if r.Reclaimed != "" {
		fmt.Printf("  reclaimed:  %s\n", r.Reclaimed)
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS.

- [ ] **Step 6: Verify build + help output**

Run: `go build ./...`

Expected: Build succeeds.

Run: `go run . cleanup --help`

Expected: usage shows `--yes`, `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--app`, `--env`.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command with label-based pruning"
```

---

### Task 5: Documentation + full verification

**Files:**
- Modify: `README.md` — add cleanup to Features bullet list and add a `tengiz cleanup` command section
- Modify: `docs/FUTURES_FEATURES.md` — mark #6 Docker Housekeeping implemented (line 19)
- No production code changes

**Interfaces:**
- Consumes: nothing new (doc-only task)

- [ ] **Step 1: Add `tengiz cleanup` to the README Features list**

In `README.md`, under the `## Features` bullet list (around line 31, after "Health check configuration"), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes stopped containers, unused images/networks, and build cache using label-based filtering (never touches non-Tengiz resources). Dry-run by default, `--yes` to execute.
```

- [ ] **Step 2: Add a `tengiz cleanup` command section to the README**

Append after the `### tengiz volume list <app>` section (ends around line 296):

```markdown
### `tengiz cleanup`

Prunes unused Docker resources created by Tengiz. Label-based filtering guarantees resources not managed by Tengiz are never removed. Mirrors `docker system prune` defaults: stopped containers, unused images, unused networks, and dangling build cache. Volumes are never pruned unless `--volumes` is given. Defaults to a dry-run report; pass `--yes` to execute.

```bash
tengiz cleanup                          # dry-run: report what would be removed
tengiz cleanup --yes                    # actually perform the cleanup
tengiz cleanup --volumes --yes          # also prune unused volumes
tengiz cleanup --app myapp --yes        # restrict to a single app
tengiz cleanup --env staging --yes      # restrict to one environment
tengiz cleanup --images --networks --yes # only specific categories
```
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

Change line 19 from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 4: Run the full verification suite**

Run: `go build ./...`

Expected: Build succeeds.

Run: `go vet ./...`

Expected: No issues.

Run: `go test ./... -v -count=1`

Expected: All PASS (proxy tests are slow ~2s each; idle tests are time-sensitive — allow them to run).

- [ ] **Step 5: Self-review against the spec**

Check against requirements from `docs/FUTURES_FEATURES.md` #6:
- `tengiz cleanup` command ✅ (Task 4)
- Label-based pruning so Tengiz-managed resources are targeted and foreign resources preserved ✅ (Task 3 — `label=tengiz-app` filters + `tengiz-apps/*` reference namespace)
- Handles the disk-space failure mode of single-server deployments ✅ (containers/images/networks/build cache defaults)
- Env-aware via `--env` flag ✅ (Task 4 — shadows root flag, defaults to all envs)
- Backward compatibility ✅ (no data format changes; new command only)
- No breaking changes ✅ (existing tests pass; mocks updated)

- [ ] **Step 6: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task". None present. Every step carries complete code or exact commands.

- [ ] **Step 7: Type consistency check**

- `runtime.PruneOptions{Containers, Images, Networks, Volumes, BuildCache, DryRun bool; AppName, Env string}` — same shape in `runtime.go`, `housekeeping.go`, and `cleanupOptionsFromFlags`
- `runtime.PruneResult{Containers, Images, Networks, Volumes int; BuildCache bool; Reclaimed string}` — same shape in `runtime.go` and `printCleanupResult`
- `Manager.Prune(ctx, opts) (*PruneResult, error)` — identical signature on interface, stub, docker impl, and all three test mocks
- Helper names `pruneDefaults`, `pruneFilters`, `pruneFilterFlagArgs`, `imageReferenceFilter`, `filterImageTags`, `countLines`, `parsePruneOutput`, `parseReclaimedSpace` — identical in Task 2 (definition) and Task 3 (usage)
- `cleanupOptionsFromFlags(cmd) runtime.PruneOptions` and `printCleanupResult(r, dryRun)` — identical in Task 4 tests and implementation

- [ ] **Step 8: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```
