# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, networks, volumes, build cache) to reclaim disk space, while never touching Tengiz-managed containers (labeled `tengiz-app`) or images (`tengiz-apps/*`) so deployed apps and rollback history are preserved.

**Architecture:** The `runtime` package gains a `Prune(ctx, opts) (PruneResult, error)` method on the `Manager` interface. The docker exec implementation collects candidates with `docker ps`/`docker images`/`docker network ls`/`docker volume ls`, filters them through pure, unit-testable helper functions that enforce the Tengiz protection rules, then either removes them (`docker rm`/`rmi`/`network rm`/`volume rm`/`builder prune`) or — in dry-run mode — only reports them. The CLI wires this to a new `cleanupCmd` cobra command with `--dry-run` and per-category flags.

**Tech Stack:** Go 1.26 stdlib only (`os/exec`, `context`, `strings`), existing `cobra`, existing `runtime.Manager` interface. No new external dependencies.

## Global Constraints

- Command name is exactly `tengiz cleanup` (as named in `docs/FUTURES_FEATURES.md` feature #6)
- Never remove containers that carry the `tengiz-app` label (protects every Tengiz-managed container, all environments)
- Never remove images whose repository starts with `tengiz-apps/` (protects deployed images + rollback history)
- Never remove the default Docker networks `bridge`, `host`, `none`
- Default run (no category flags) prunes containers + images + networks + build cache; volumes are **excluded** by default and only pruned with the explicit `--volumes` flag
- All pruning decisions live in pure helper functions in `internal/runtime/prune.go` so they are unit-testable without Docker
- No new external dependencies (stdlib + existing `cobra` only)
- Feature branch created first: `git checkout -b feat/docker-housekeeping`
- Every task adds/updates tests, runs them green, then commits (AGENTS.md rule)
- UI/docs change: update `README.md` and the `AGENTS.md` CLI reference (AGENTS.md rule)
- Verification: `go build ./...`, `go test ./... -v -count=1`, `go vet ./...`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` (new) | `PruneOptions`, `PruneResult`, `tengizImageRepo` const, pure filter helpers, `dockerRuntime` prune implementation |
| `internal/runtime/prune_test.go` (new) | Unit tests for pure helpers + stub prune |
| `internal/runtime/runtime.go` | Add `Prune` to `Manager` interface + stub implementation |
| `internal/runtime/cleanup.go` | No change (existing `RemoveImage`/`KeepLastNImages` remain) |
| `internal/cli/cleanup.go` (new) | `cleanupCmd`, `applyCleanupDefaults`, `printCleanupResult` |
| `internal/cli/cleanup_test.go` (new) | Tests for command registration, flags, defaults, output |
| `internal/cli/root.go` | Register `cleanupCmd` + its flags in `init()` |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` (interface compliance) |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` (interface compliance) |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` (interface compliance) |
| `README.md` | Add `tengiz cleanup` CLI reference section |
| `AGENTS.md` | Add `tengiz cleanup` line to CLI reference |

---

### Task 1: Runtime prune types + pure candidate helpers

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{DryRun, Containers, Images, Volumes, Networks, BuildCache bool}`, `runtime.PruneResult{DryRun bool; Containers, Images, Volumes, Networks []string; BuildCache string}`, and pure helpers `nonEmptyLines(string) []string`, `subtract([]string, []string) []string`, `lastLine(string) string`, `candidateContainers([]string, []string) []string`, `parsePrunableImages([]string) []string`, `candidateNetworks([]string, map[string]bool) []string`, `candidateVolumes([]string, map[string]bool) []string`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing tests**

```go
// internal/runtime/prune_test.go
package runtime

import "testing"

func TestNonEmptyLines(t *testing.T) {
	got := nonEmptyLines("aaa\n\nbbb\n")
	if len(got) != 2 || got[0] != "aaa" || got[1] != "bbb" {
		t.Errorf("nonEmptyLines = %v, want [aaa bbb]", got)
	}
	if got := nonEmptyLines(""); len(got) != 0 {
		t.Errorf("nonEmptyLines(\"\") = %v, want empty", got)
	}
}

func TestLastLine(t *testing.T) {
	if got := lastLine("a\nb\nTotal: 1.2GB"); got != "Total: 1.2GB" {
		t.Errorf("lastLine = %q, want %q", got, "Total: 1.2GB")
	}
	if got := lastLine(""); got != "" {
		t.Errorf("lastLine(\"\") = %q, want empty", got)
	}
}

func TestSubtract(t *testing.T) {
	got := subtract([]string{"a", "b", "c"}, []string{"b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("subtract = %v, want [a c]", got)
	}
	if got := subtract([]string{"a", "b"}, nil); len(got) != 2 {
		t.Errorf("subtract with nil excluded = %v, want 2 items", got)
	}
}

func TestCandidateContainers(t *testing.T) {
	got := candidateContainers([]string{"aaa", "bbb", "ccc"}, []string{"bbb"})
	if len(got) != 2 || got[0] != "aaa" || got[1] != "ccc" {
		t.Errorf("candidateContainers = %v, want [aaa ccc]", got)
	}
}

func TestCandidateContainersAllRunning(t *testing.T) {
	got := candidateContainers([]string{"aaa", "bbb"}, []string{"aaa", "bbb"})
	if len(got) != 0 {
		t.Errorf("candidateContainers = %v, want empty", got)
	}
}

func TestParsePrunableImages(t *testing.T) {
	lines := []string{
		"tengiz-apps/myapp:1700000000|sha256:aaa",
		"nginx:latest|sha256:bbb",
		"<none>:<none>|sha256:ccc",
		"tengiz-apps/myapp-staging:1700000001|sha256:ddd",
	}
	got := parsePrunableImages(lines)
	if len(got) != 2 || got[0] != "sha256:bbb" || got[1] != "sha256:ccc" {
		t.Errorf("parsePrunableImages = %v, want [sha256:bbb sha256:ccc]", got)
	}
}

func TestParsePrunableImagesEmpty(t *testing.T) {
	if got := parsePrunableImages(nil); len(got) != 0 {
		t.Errorf("parsePrunableImages(nil) = %v, want empty", got)
	}
}

func TestCandidateNetworks(t *testing.T) {
	inUse := map[string]bool{"webnet": true}
	got := candidateNetworks([]string{"bridge", "host", "none", "webnet", "mynet"}, inUse)
	if len(got) != 1 || got[0] != "mynet" {
		t.Errorf("candidateNetworks = %v, want [mynet]", got)
	}
}

func TestCandidateVolumes(t *testing.T) {
	inUse := map[string]bool{"dbdata": true}
	got := candidateVolumes([]string{"dbdata", "cachevol"}, inUse)
	if len(got) != 1 || got[0] != "cachevol" {
		t.Errorf("candidateVolumes = %v, want [cachevol]", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestNonEmptyLines|TestLastLine|TestSubtract|TestCandidateContainers|TestParsePrunableImages|TestCandidateNetworks|TestCandidateVolumes" -v -count=1`

Expected: FAIL with `undefined: candidateContainers` (prune.go does not exist yet)

- [ ] **Step 4: Write minimal implementation**

```go
// internal/runtime/prune.go
package runtime

import "strings"

// tengizImageRepo is the repository prefix for every image built by Tengiz.
// Images in this repository are never pruned so rollback history is preserved.
const tengizImageRepo = "tengiz-apps/"

// PruneOptions selects which Docker resource categories to prune.
type PruneOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

// PruneResult reports what was (or, in dry-run mode, would be) removed.
type PruneResult struct {
	DryRun     bool
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
	BuildCache string
}

// defaultNetworks are Docker's built-in networks that must never be removed.
var defaultNetworks = map[string]bool{"bridge": true, "host": true, "none": true}

// nonEmptyLines splits s into non-empty lines.
func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// subtract returns items without any that appear in excluded.
func subtract(items, excluded []string) []string {
	ex := make(map[string]bool, len(excluded))
	for _, e := range excluded {
		ex[e] = true
	}
	var out []string
	for _, it := range items {
		if !ex[it] {
			out = append(out, it)
		}
	}
	return out
}

// lastLine returns the last non-empty line of s.
func lastLine(s string) string {
	lines := nonEmptyLines(s)
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// candidateContainers returns container IDs safe to prune: every ID in allIDs
// that is not currently running. allIDs must already exclude Tengiz-managed
// containers (the caller lists them with the label!=tengiz-app filter).
func candidateContainers(allIDs, runningIDs []string) []string {
	running := make(map[string]bool, len(runningIDs))
	for _, id := range runningIDs {
		running[id] = true
	}
	var candidates []string
	for _, id := range allIDs {
		if !running[id] {
			candidates = append(candidates, id)
		}
	}
	return candidates
}

// parsePrunableImages parses "repo:tag|id" lines from `docker images --format`
// and returns the image IDs that may be pruned — anything not in the tengiz-apps
// repository, including dangling <none>:<none> images.
func parsePrunableImages(lines []string) []string {
	var ids []string
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.HasPrefix(parts[0], tengizImageRepo) {
			continue
		}
		ids = append(ids, parts[1])
	}
	return ids
}

// candidateNetworks returns network names safe to prune: non-default networks
// not currently used by any container.
func candidateNetworks(names []string, inUse map[string]bool) []string {
	var candidates []string
	for _, name := range names {
		if defaultNetworks[name] || inUse[name] {
			continue
		}
		candidates = append(candidates, name)
	}
	return candidates
}

// candidateVolumes returns volume names safe to prune: volumes not currently
// referenced by any container.
func candidateVolumes(names []string, inUse map[string]bool) []string {
	var candidates []string
	for _, name := range names {
		if !inUse[name] {
			candidates = append(candidates, name)
		}
	}
	return candidates
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestNonEmptyLines|TestLastLine|TestSubtract|TestCandidateContainers|TestParsePrunableImages|TestCandidateNetworks|TestCandidateVolumes" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add prune types and candidate filter helpers"
```

---

### Task 2: Add Prune to the runtime Manager interface + docker implementation

**Files:**
- Modify: `internal/runtime/prune.go` — add `dockerRuntime` prune methods (update import block)
- Modify: `internal/runtime/runtime.go:31-49` — add `Prune` to `Manager` interface; add stub method after `KeepLastNImages`
- Modify: `internal/cli/root_test.go:69-100` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:14-35` — add `Prune` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:15-36` — add `Prune` to `mockRuntime`
- Test: `internal/runtime/prune_test.go` — add stub prune test

**Interfaces:**
- Consumes: `PruneOptions`, `PruneResult`, and all pure helpers from Task 1
- Produces: `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)` — the CLI in Task 3 calls this exact signature

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/prune_test.go`. The file currently starts with `import "testing"` — replace that single import with a parenthesized block that adds `context`:

```go
import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{DryRun: true, Containers: true, Images: true})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	if !res.DryRun {
		t.Error("stub Prune() DryRun not propagated")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: FAIL with `m.Prune undefined (type *stubManager has no field or method Prune)`

- [ ] **Step 3: Add `Prune` to the `Manager` interface and stub**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after the `KeepLastNImages` line):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
```

In `internal/runtime/runtime.go`, add the stub method (after `func (m *stubManager) KeepLastNImages`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 4: Implement the docker prune methods**

In `internal/runtime/prune.go`, replace the import block with:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)
```

Append these methods to `internal/runtime/prune.go`:

```go
// Prune removes (or, with DryRun, reports) unused Docker resources in the
// categories selected by opts. Tengiz-managed containers (label tengiz-app)
// and images (repo tengiz-apps/*) are never candidates.
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	result := PruneResult{DryRun: opts.DryRun}
	var err error
	if opts.Containers {
		if result.Containers, err = r.pruneContainers(ctx, opts.DryRun); err != nil {
			return result, err
		}
	}
	if opts.Images {
		if result.Images, err = r.pruneImages(ctx, opts.DryRun); err != nil {
			return result, err
		}
	}
	if opts.Networks {
		if result.Networks, err = r.pruneNetworks(ctx, opts.DryRun); err != nil {
			return result, err
		}
	}
	if opts.Volumes {
		if result.Volumes, err = r.pruneVolumes(ctx, opts.DryRun); err != nil {
			return result, err
		}
	}
	if opts.BuildCache {
		if result.BuildCache, err = r.pruneBuildCache(ctx, opts.DryRun); err != nil {
			return result, err
		}
	}
	return result, nil
}

// pruneContainers removes stopped containers that are not managed by Tengiz.
func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) ([]string, error) {
	allCmd := exec.CommandContext(ctx, "docker", "ps", "-aq", "--no-trunc",
		"--filter", "label!=tengiz-app", "--format", "{{.ID}}")
	allOut, err := allCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(allOut))
	}
	allIDs := nonEmptyLines(string(allOut))

	runCmd := exec.CommandContext(ctx, "docker", "ps", "-q", "--no-trunc", "--format", "{{.ID}}")
	runOut, err := runCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(runOut))
	}
	runningIDs := nonEmptyLines(string(runOut))

	candidates := candidateContainers(allIDs, runningIDs)
	if dryRun || len(candidates) == 0 {
		return candidates, nil
	}
	rmArgs := append([]string{"rm"}, candidates...)
	rmCmd := exec.CommandContext(ctx, "docker", rmArgs...)
	if out, err := rmCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("docker rm: %w\n%s", err, string(out))
	}
	return candidates, nil
}

// pruneImages removes unused images that are not in the tengiz-apps repository.
// Images still referenced by a container (running or stopped) are kept.
func (r *dockerRuntime) pruneImages(ctx context.Context, dryRun bool) ([]string, error) {
	listCmd := exec.CommandContext(ctx, "docker", "images", "-a", "--no-trunc",
		"--format", "{{.Repository}}:{{.Tag}}|{{.ID}}")
	listOut, err := listCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(listOut))
	}
	candidates := parsePrunableImages(nonEmptyLines(string(listOut)))
	if len(candidates) == 0 {
		return nil, nil
	}

	used, err := r.usedImageIDs(ctx)
	if err != nil {
		return nil, err
	}
	prunable := subtract(candidates, used)
	if dryRun {
		return prunable, nil
	}

	var removed []string
	for _, id := range prunable {
		rmCmd := exec.CommandContext(ctx, "docker", "rmi", id)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			// Image is a parent of a retained image; skip it.
			log.Printf("[runtime] skip image %s: %v\n%s", id, err, string(out))
		} else {
			removed = append(removed, id)
		}
	}
	return removed, nil
}

// usedImageIDs returns the full image IDs referenced by any container.
func (r *dockerRuntime) usedImageIDs(ctx context.Context) ([]string, error) {
	psCmd := exec.CommandContext(ctx, "docker", "ps", "-aq", "--no-trunc", "--format", "{{.ID}}")
	psOut, err := psCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(psOut))
	}
	var used []string
	seen := make(map[string]bool)
	for _, cid := range nonEmptyLines(string(psOut)) {
		insCmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Image}}", cid)
		insOut, err := insCmd.CombinedOutput()
		if err != nil {
			continue
		}
		id := strings.TrimSpace(string(insOut))
		if id != "" && !seen[id] {
			seen[id] = true
			used = append(used, id)
		}
	}
	return used, nil
}

// pruneNetworks removes non-default networks not used by any container.
func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) ([]string, error) {
	listCmd := exec.CommandContext(ctx, "docker", "network", "ls", "--format", "{{.Name}}")
	listOut, err := listCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(listOut))
	}
	names := nonEmptyLines(string(listOut))
	inUse, err := r.networksInUse(ctx, names)
	if err != nil {
		return nil, err
	}
	candidates := candidateNetworks(names, inUse)
	if dryRun || len(candidates) == 0 {
		return candidates, nil
	}
	rmArgs := append([]string{"network", "rm"}, candidates...)
	rmCmd := exec.CommandContext(ctx, "docker", rmArgs...)
	if out, err := rmCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("docker network rm: %w\n%s", err, string(out))
	}
	return candidates, nil
}

// networksInUse marks which networks have at least one attached container.
func (r *dockerRuntime) networksInUse(ctx context.Context, names []string) (map[string]bool, error) {
	inUse := make(map[string]bool, len(names))
	for _, name := range names {
		insCmd := exec.CommandContext(ctx, "docker", "network", "inspect",
			"--format", "{{len .Containers}}", name)
		insOut, err := insCmd.CombinedOutput()
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(insOut)) != "0" {
			inUse[name] = true
		}
	}
	return inUse, nil
}

// pruneVolumes removes volumes not referenced by any container.
func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) ([]string, error) {
	listCmd := exec.CommandContext(ctx, "docker", "volume", "ls", "--format", "{{.Name}}")
	listOut, err := listCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(listOut))
	}
	names := nonEmptyLines(string(listOut))
	inUse, err := r.volumesInUse(ctx, names)
	if err != nil {
		return nil, err
	}
	candidates := candidateVolumes(names, inUse)
	if dryRun || len(candidates) == 0 {
		return candidates, nil
	}
	rmArgs := append([]string{"volume", "rm"}, candidates...)
	rmCmd := exec.CommandContext(ctx, "docker", rmArgs...)
	if out, err := rmCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("docker volume rm: %w\n%s", err, string(out))
	}
	return candidates, nil
}

// volumesInUse marks which volumes are referenced by at least one container.
// If a volume cannot be inspected, it is treated as in-use to be safe.
func (r *dockerRuntime) volumesInUse(ctx context.Context, names []string) (map[string]bool, error) {
	inUse := make(map[string]bool, len(names))
	for _, name := range names {
		insCmd := exec.CommandContext(ctx, "docker", "volume", "inspect",
			"--format", "{{.UsageData.RefCount}}", name)
		insOut, err := insCmd.CombinedOutput()
		if err != nil {
			inUse[name] = true
			continue
		}
		if ref := strings.TrimSpace(string(insOut)); ref != "" && ref != "0" {
			inUse[name] = true
		}
	}
	return inUse, nil
}

// pruneBuildCache prunes the BuildKit cache. In dry-run mode it returns the
// "Total: ..." line from `docker builder du` instead of removing anything.
func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) (string, error) {
	if dryRun {
		duCmd := exec.CommandContext(ctx, "docker", "builder", "du")
		out, err := duCmd.CombinedOutput()
		if err != nil {
			// BuildKit may be disabled; nothing to preview.
			return "", nil
		}
		return lastLine(string(out)), nil
	}
	pruneCmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := pruneCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}
```

- [ ] **Step 5: Update the test mock implementers so the package still compiles**

Adding `Prune` to the `Manager` interface breaks three test mocks that are passed to functions typed as `runtime.Manager`. Add this method to each:

In `internal/cli/root_test.go`, after `func (m *mockRTForDeploy) KeepLastNImages` (line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

In `internal/idle/idle_test.go`, after `func (m *mockRuntime) KeepLastNImages` (line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

In `internal/proxy/proxy_test.go`, after `func (m *mockRuntime) KeepLastNImages` (line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: PASS

- [ ] **Step 7: Build and vet**

Run: `go build ./...`

Expected: Build succeeds (proves all mocks implement the updated interface)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 8: Run all runtime, idle, proxy, and cli tests**

Run: `go test ./internal/runtime/... ./internal/idle/... ./internal/proxy/... ./internal/cli/... -v -count=1`

Expected: All PASS (proxy tests are slow, ~2s each)

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go internal/runtime/runtime.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Prune to Manager interface and docker runtime"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:34-89` — register `cleanupCmd` and its flags in `init()`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker() (Manager, error)` and `runtime.Manager.Prune(ctx, PruneOptions) (PruneResult, error)` from Task 2
- Produces: `tengiz cleanup` command with flags `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`, `--cache`; helper `applyCleanupDefaults(PruneOptions) PruneOptions`; helper `printCleanupResult(PruneResult)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"strings"
	"testing"

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
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache"} {
		if f := cleanupCmd.Flags().Lookup(flag); f == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestApplyCleanupDefaults(t *testing.T) {
	opts := applyCleanupDefaults(runtime.PruneOptions{})
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Errorf("defaults should enable containers/images/networks/cache, got %+v", opts)
	}
	if opts.Volumes {
		t.Error("defaults must not enable volumes")
	}
}

func TestApplyCleanupDefaultsPreservesSelection(t *testing.T) {
	opts := applyCleanupDefaults(runtime.PruneOptions{Images: true})
	if !opts.Images {
		t.Error("--images selection lost")
	}
	if opts.Containers || opts.Networks || opts.BuildCache || opts.Volumes {
		t.Errorf("explicit --images should not enable other categories, got %+v", opts)
	}
}

func TestPrintCleanupResultDryRun(t *testing.T) {
	out := captureOutput(func() {
		printCleanupResult(runtime.PruneResult{
			DryRun:     true,
			Containers: []string{"aaa"},
			Images:     []string{"bbb", "ccc"},
			Networks:   []string{"mynet"},
			Volumes:    []string{"vvv"},
			BuildCache: "1.2GB",
		})
	})
	for _, want := range []string{
		"1 would be removed",
		"2 would be removed",
		"build cache: would be pruned (1.2GB)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q, got:\n%s", want, out)
		}
	}
}

func TestPrintCleanupResult(t *testing.T) {
	out := captureOutput(func() {
		printCleanupResult(runtime.PruneResult{
			Containers: []string{"aaa"},
			Images:     []string{"bbb", "ccc"},
			Volumes:    []string{"vvv"},
			BuildCache: "cleaned",
		})
	})
	for _, want := range []string{
		"removed 1 containers",
		"removed 2 images",
		"removed 1 volumes",
		"build cache pruned",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}
```

Note: `captureOutput` is already defined in `internal/cli/root_test.go` (same package `cli`) and is reused here.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup|TestApplyCleanupDefaults|TestPrintCleanupResult" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: applyCleanupDefaults`, `undefined: printCleanupResult`

- [ ] **Step 3: Write the CLI command implementation**

```go
// internal/cli/cleanup.go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

// applyCleanupDefaults fills in the default category selection when no
// category flag was given: containers, images, networks, and build cache.
// Volumes are deliberately excluded by default.
func applyCleanupDefaults(opts runtime.PruneOptions) runtime.PruneOptions {
	if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.BuildCache {
		opts.Containers = true
		opts.Images = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts
}

// printCleanupResult prints a per-category summary of the cleanup result.
func printCleanupResult(r runtime.PruneResult) {
	if r.DryRun {
		fmt.Printf("  containers: %d would be removed\n", len(r.Containers))
		fmt.Printf("  images: %d would be removed\n", len(r.Images))
		fmt.Printf("  networks: %d would be removed\n", len(r.Networks))
		fmt.Printf("  volumes: %d would be removed\n", len(r.Volumes))
		if r.BuildCache != "" {
			fmt.Printf("  build cache: would be pruned (%s)\n", r.BuildCache)
		}
		return
	}
	fmt.Printf("  removed %d containers\n", len(r.Containers))
	fmt.Printf("  removed %d images\n", len(r.Images))
	fmt.Printf("  removed %d networks\n", len(r.Networks))
	fmt.Printf("  removed %d volumes\n", len(r.Volumes))
	if r.BuildCache != "" {
		fmt.Printf("  build cache pruned\n")
	}
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (protects Tengiz containers and images)",
	Long: `Prune unused Docker resources to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app) and images (tengiz-apps/*) are
never removed, so deployed apps and rollback history are preserved.

By default prunes stopped containers, unused images, unused networks, and the
build cache. Volumes are only pruned with the explicit --volumes flag. Use
--dry-run to preview what would be removed first.

Examples:
  tengiz cleanup --dry-run      # preview what would be removed
  tengiz cleanup                # prune containers, images, networks, build cache
  tengiz cleanup --volumes      # also prune unused volumes
  tengiz cleanup --images       # only prune unused images`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := runtime.PruneOptions{}
		var err error
		if opts.DryRun, err = cmd.Flags().GetBool("dry-run"); err != nil {
			return err
		}
		if opts.Containers, err = cmd.Flags().GetBool("containers"); err != nil {
			return err
		}
		if opts.Images, err = cmd.Flags().GetBool("images"); err != nil {
			return err
		}
		if opts.Volumes, err = cmd.Flags().GetBool("volumes"); err != nil {
			return err
		}
		if opts.Networks, err = cmd.Flags().GetBool("networks"); err != nil {
			return err
		}
		if opts.BuildCache, err = cmd.Flags().GetBool("cache"); err != nil {
			return err
		}
		opts = applyCleanupDefaults(opts)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if result.DryRun {
			fmt.Println("[tengiz] cleanup (dry-run) — nothing removed")
		} else {
			fmt.Println("[tengiz] cleanup complete")
		}
		printCleanupResult(result)
		return nil
	},
}
```

- [ ] **Step 4: Register the command and flags in `init()`**

In `internal/cli/root.go`, inside the existing `init()` function, after the line `rootCmd.AddCommand(runCmd)` (line 67), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

In `internal/cli/root.go`, inside `init()`, after the line `webhookCmd.Flags().String("config", "", ...)` (line 88), add:

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune unused images (keeps tengiz-apps/*)")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (excluded by default)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("cache", false, "prune build cache")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestApplyCleanupDefaults|TestPrintCleanupResult" -v -count=1`

Expected: PASS

- [ ] **Step 6: Build and run all CLI tests**

Run: `go build ./...`

Expected: Build succeeds

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation + full verification

**Files:**
- Modify: `README.md` — insert `tengiz cleanup` section between `tengiz rm` (ends line 228) and `tengiz rollback` (starts line 230)
- Modify: `AGENTS.md:47` — add `tengiz cleanup` line after the `tengiz stop/start/rm` line in the CLI reference

- [ ] **Step 1: Document the command in README.md**

Insert after the `### tengiz rm <app>` section (after the table ending at line 228, before `### tengiz rollback <app>` at line 230):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space. Tengiz-managed containers (labeled `tengiz-app`) and images (`tengiz-apps/*`) are never removed, so deployed apps and rollback history are preserved.

By default prunes stopped containers, unused images, unused networks, and the build cache. Volumes are only pruned with the explicit `--volumes` flag. Use `--dry-run` to preview what would be removed.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--containers` | Only prune stopped containers |
| `--images` | Only prune unused images (keeps `tengiz-apps/*`) |
| `--volumes` | Also prune unused volumes (excluded by default) |
| `--networks` | Only prune unused networks |
| `--cache` | Only prune build cache |

Examples:
```
tengiz cleanup --dry-run      # preview what would be removed
tengiz cleanup                # prune containers, images, networks, build cache
tengiz cleanup --volumes      # also prune unused volumes
tengiz cleanup --images       # only prune unused images
```
```

- [ ] **Step 2: Add the command to the AGENTS.md CLI reference**

In `AGENTS.md`, after the line `tengiz stop/start/rm  → lifecycle` (line 47), add:

```
tengiz cleanup          → prune unused Docker resources (--dry-run, per-category)
```

- [ ] **Step 3: Full test suite + vet**

Run: `go test ./... -v -count=1`

Expected: All PASS (except possibly the known time-sensitive `idle` and slow `proxy` tests, which still pass)

Run: `go vet ./...`

Expected: No issues

Run: `go build -o tengiz .`

Expected: Build succeeds

- [ ] **Step 4: Manual smoke test (requires Docker)**

```bash
./tengiz cleanup --dry-run
./tengiz cleanup
```

Expected: dry-run prints `[tengiz] cleanup (dry-run) — nothing removed` with per-category "would be removed" counts; the real run prints `[tengiz] cleanup complete` with per-category "removed N" counts.

- [ ] **Step 5: Self-review against spec**

- [ ] **Step 5a: Spec coverage** — Check `docs/FUTURES_FEATURES.md` feature #6 "Docker Housekeeping":
  - `tengiz cleanup` command ✅ (Task 3)
  - Label-based protection of Tengiz containers (`label!=tengiz-app`) ✅ (Task 2 `pruneContainers`)
  - Protection of Tengiz images (`tengiz-apps/*`) ✅ (Task 2 `parsePrunableImages`)
  - Disk-space focused pruning ✅ (Task 2, all five categories)
  - Related feature #56 "Granular Docker Prune Operations" partially covered by per-category flags ✅ (Task 3 flags)

- [ ] **Step 5b: Placeholder scan** — Search the plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task". None present; every step contains complete code.

- [ ] **Step 5c: Type consistency** — Verify names match across tasks:
  - `PruneOptions` / `PruneResult` defined in Task 1, used in Tasks 2-3 with identical fields ✅
  - `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)` — same signature in interface, stub, docker impl, and all three test mocks ✅
  - Helper names `candidateContainers`, `parsePrunableImages`, `candidateNetworks`, `candidateVolumes`, `subtract`, `nonEmptyLines`, `lastLine` — consistent between Task 1 definitions and Task 2 call sites ✅
  - CLI helpers `applyCleanupDefaults(PruneOptions) PruneOptions`, `printCleanupResult(PruneResult)` — defined and tested in Task 3 ✅

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```