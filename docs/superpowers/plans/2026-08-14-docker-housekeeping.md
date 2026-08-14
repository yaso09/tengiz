# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by safely removing unused Docker resources (stopped foreign containers, dangling/stale images, unused volumes/networks, build cache) while always protecting Tengiz-managed containers, in-use images, and rollback image retention.

**Architecture:** A new `runtime.Cleanup(ctx, opts)` method on the Docker runtime lists candidate resources with `docker ps`/`docker images`/`docker volume ls`/`docker network ls`, filters them with label-aware selection logic, then removes them one-by-one (or reports them under `--dry-run`). The selection rules live in pure helper functions (`selectContainersToRemove`, `selectImagesToRemove`) so they are unit-testable without Docker. The CLI `cleanup` command gathers the live app set from the env-scoped `config.Store` as "protected apps" and passes it to the runtime so per-app image retention (keep 5) is enforced. Category flags (`--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`) map to granular prune operations; with no category flag, all five run.

**Tech Stack:** Go 1.26, `os/exec` Docker CLI passthrough, Cobra (CLI), existing `config.Store` + `runtime.Manager`, existing `tengiz-app`/`tengiz-env` container labels.

## Global Constraints

- Tengiz containers carry `tengiz-app=<name>` and `tengiz-env=<env>` labels — constants `labelKey = "tengiz-app"` and `envLabelKey = "tengiz-env"` already exist at `internal/runtime/docker.go:76-77`
- Tengiz images live under repository `tengiz-apps/<app>` with tags `<env>-<deploymentID>` and `<env>-latest` (`internal/builder/builder.go:61,84`)
- Cleanup must NEVER remove running containers
- Cleanup must preserve stopped Tengiz containers (labeled `tengiz-app`) by default — scale-to-zero depends on them; `--all` opts in to removing them
- Cleanup must preserve images referenced by any container and images tagged `<env>-latest`
- Cleanup must preserve the newest `KeepImages` (default 5) images per still-registered app (rollback retention, matching `KeepLastNImages(..., 5)`)
- Images of apps no longer present in the store are removed entirely
- `--dry-run` performs read-only listing only — no removals
- No category flag = clean all five categories
- Env-aware: protected apps come from the env-scoped store (`NewStoreWithEnv`); cleanup runs under the current `--env`
- No new external dependencies
- Existing tests must continue to pass; `mockRTForDeploy` in `internal/cli/root_test.go` must be extended to keep satisfying `runtime.Manager`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/housekeeping.go` | **Create.** Pure selection helpers (`parseContainerRow`, `hasLabel`, `selectContainersToRemove`, `parseImageRow`, `selectImagesToRemove`, `extractTotalSpace`, `containsString`), then `CleanupOptions`/`CleanupResult` types, the `dockerRuntime.Cleanup` method, and the `remove` helper |
| `internal/runtime/runtime.go` | **Modify.** Add `Cleanup(ctx, opts) (*CleanupResult, error)` to the `Manager` interface + stub implementation |
| `internal/runtime/housekeeping_test.go` | **Create.** Unit tests for the selection helpers, stub `Cleanup`, and interface assertions |
| `internal/cli/root.go` | **Modify.** Add `cleanupCmd`, register it + flags in `init()`, add `runCleanup` and `withDefaultCategories` helpers |
| `internal/cli/root_test.go` | **Modify.** Add `Cleanup` to `mockRTForDeploy` so it keeps implementing `Manager` |
| `internal/cli/cleanup_test.go` | **Create.** Command registration, flag presence, `withDefaultCategories`, `runCleanup` protected-apps behavior |
| `README.md` | **Modify.** Document `tengiz cleanup` |
| `AGENTS.md` | **Modify.** Add `cleanup` to CLI list; mention `Cleanup` in the `runtime.Manager` row |
| `docs/FUTURES_FEATURES.md` | **Modify.** Mark #6 Docker Housekeeping and #56 Granular Docker Prune Operations as implemented |

---

### Task 1: Label-aware selection helpers (pure functions)

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `labelKey` constant (`internal/runtime/docker.go:76`)
- Produces:
  - `parseContainerRow(line string) (id, state, labels string)`
  - `hasLabel(labels, key string) bool`
  - `selectContainersToRemove(lines []string, all bool) []string`
  - `parseImageRow(line string) (repoTag, id, createdAt string)`
  - `selectImagesToRemove(lines, usedTags []string, protectedApps []string, keepN int, all bool) []string`
  - `extractTotalSpace(output string) string`
  - `containsString(list []string, s string) bool`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/housekeeping_test.go
package runtime

import (
	"reflect"
	"testing"
)

func TestParseContainerRow(t *testing.T) {
	id, state, labels := parseContainerRow("abc123|exited|tengiz-app=myapp,tengiz-env=production")
	if id != "abc123" {
		t.Errorf("id = %q, want %q", id, "abc123")
	}
	if state != "exited" {
		t.Errorf("state = %q, want %q", state, "exited")
	}
	if labels != "tengiz-app=myapp,tengiz-env=production" {
		t.Errorf("labels = %q", labels)
	}
}

func TestParseContainerRowMalformed(t *testing.T) {
	id, state, labels := parseContainerRow("abc123")
	if id != "" || state != "" || labels != "" {
		t.Errorf("expected empty fields, got id=%q state=%q labels=%q", id, state, labels)
	}
}

func TestHasLabel(t *testing.T) {
	if !hasLabel("tengiz-app=myapp,tengiz-env=production", labelKey) {
		t.Error("hasLabel should find tengiz-app label")
	}
	if hasLabel("foo=bar", labelKey) {
		t.Error("hasLabel should not match missing label")
	}
	if hasLabel("", labelKey) {
		t.Error("hasLabel should not match empty labels")
	}
}

func TestSelectContainersToRemove(t *testing.T) {
	lines := []string{
		"c1|running|tengiz-app=myapp",
		"c2|exited|tengiz-app=myapp",
		"c3|exited|foo=bar",
		"c4|created|",
		"c5|exited|tengiz-env=production",
	}

	got := selectContainersToRemove(lines, false)
	want := []string{"c3", "c4", "c5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectContainersToRemove(all=false) = %v, want %v", got, want)
	}

	gotAll := selectContainersToRemove(lines, true)
	wantAll := []string{"c2", "c3", "c4", "c5"}
	if !reflect.DeepEqual(gotAll, wantAll) {
		t.Errorf("selectContainersToRemove(all=true) = %v, want %v", gotAll, wantAll)
	}
}

func TestParseImageRow(t *testing.T) {
	repoTag, id, createdAt := parseImageRow("tengiz-apps/myapp:production-123|sha256:abc|2024-01-01 10:00:00 +0000 UTC")
	if repoTag != "tengiz-apps/myapp:production-123" {
		t.Errorf("repoTag = %q", repoTag)
	}
	if id != "sha256:abc" {
		t.Errorf("id = %q", id)
	}
	if createdAt != "2024-01-01 10:00:00 +0000 UTC" {
		t.Errorf("createdAt = %q", createdAt)
	}
}

func TestSelectImagesToRemove(t *testing.T) {
	lines := []string{
		"<none>:<none>|deadbeef0001|2024-01-01 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-latest|deadbeef0002|2024-01-02 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-100|deadbeef0003|2024-01-03 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-200|deadbeef0004|2024-01-04 00:00:00 +0000 UTC",
		"tengiz-apps/myapp:production-300|deadbeef0005|2024-01-05 00:00:00 +0000 UTC",
		"tengiz-apps/gone:production-1|deadbeef0006|2024-01-06 00:00:00 +0000 UTC",
		"node:22-alpine|deadbeef0007|2024-01-07 00:00:00 +0000 UTC",
	}
	used := []string{"tengiz-apps/myapp:production-300"}

	got := selectImagesToRemove(lines, used, []string{"myapp"}, 2, false)
	want := []string{"deadbeef0001", "tengiz-apps/gone:production-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectImagesToRemove(all=false) = %v, want %v", got, want)
	}

	gotAll := selectImagesToRemove(lines, used, []string{"myapp"}, 2, true)
	wantAll := []string{"deadbeef0001", "node:22-alpine", "tengiz-apps/gone:production-1"}
	if !reflect.DeepEqual(gotAll, wantAll) {
		t.Errorf("selectImagesToRemove(all=true) = %v, want %v", gotAll, wantAll)
	}
}

func TestExtractTotalSpace(t *testing.T) {
	output := "ID  RECLAIMABLE\nabc  123MB\n\nTotal:  3.2GB\n"
	got := extractTotalSpace(output)
	if got != "3.2GB" {
		t.Errorf("extractTotalSpace = %q, want %q", got, "3.2GB")
	}
}

func TestExtractTotalSpaceNoTotal(t *testing.T) {
	got := extractTotalSpace("  1.5GB  ")
	if got != "1.5GB" {
		t.Errorf("extractTotalSpace = %q, want %q", got, "1.5GB")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestParseContainerRow|TestHasLabel|TestSelectContainersToRemove|TestParseImageRow|TestSelectImagesToRemove|TestExtractTotalSpace" -v -count=1`

Expected: FAIL — `undefined: parseContainerRow`, `undefined: hasLabel`, `undefined: selectContainersToRemove`, `undefined: parseImageRow`, `undefined: selectImagesToRemove`, `undefined: extractTotalSpace`

- [ ] **Step 3: Write minimal implementation in `internal/runtime/housekeeping.go`**

```go
package runtime

import (
	"sort"
	"strings"
)

func parseContainerRow(line string) (id, state, labels string) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) < 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

func hasLabel(labels, key string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if kv[0] == key {
			return true
		}
	}
	return false
}

func selectContainersToRemove(lines []string, all bool) []string {
	var ids []string
	for _, line := range lines {
		id, state, labels := parseContainerRow(line)
		if id == "" {
			continue
		}
		if state == "running" {
			continue
		}
		if !all && hasLabel(labels, labelKey) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func parseImageRow(line string) (repoTag, id, createdAt string) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) < 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

func selectImagesToRemove(lines, usedTags []string, protectedApps []string, keepN int, all bool) []string {
	used := make(map[string]bool, len(usedTags))
	for _, t := range usedTags {
		used[t] = true
	}

	createdAt := make(map[string]string)
	byApp := make(map[string][]string)
	var toRemove []string

	for _, line := range lines {
		repoTag, id, created := parseImageRow(line)
		if repoTag == "" {
			continue
		}
		if strings.HasPrefix(repoTag, "<none>:") {
			toRemove = append(toRemove, id)
			continue
		}
		if used[repoTag] {
			continue
		}
		idx := strings.LastIndex(repoTag, ":")
		if idx < 0 {
			continue
		}
		repo, tag := repoTag[:idx], repoTag[idx+1:]
		if strings.HasPrefix(repo, "tengiz-apps/") {
			if strings.HasSuffix(tag, "-latest") {
				continue
			}
			createdAt[repoTag] = created
			byApp[repo] = append(byApp[repo], repoTag)
			continue
		}
		if all {
			toRemove = append(toRemove, repoTag)
		}
	}

	for repo, tags := range byApp {
		appName := strings.TrimPrefix(repo, "tengiz-apps/")
		if !containsString(protectedApps, appName) {
			toRemove = append(toRemove, tags...)
			continue
		}
		sort.Slice(tags, func(i, j int) bool {
			return createdAt[tags[i]] > createdAt[tags[j]]
		})
		for i := keepN; i < len(tags); i++ {
			toRemove = append(toRemove, tags[i])
		}
	}
	return toRemove
}

func extractTotalSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total:"))
		}
	}
	return strings.TrimSpace(output)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestParseContainerRow|TestHasLabel|TestSelectContainersToRemove|TestParseImageRow|TestSelectImagesToRemove|TestExtractTotalSpace" -v -count=1`

Expected: PASS (all 10 subtests)

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add label-aware Docker cleanup selection helpers"
```

---

### Task 2: `Cleanup` on the runtime Manager

**Files:**
- Modify: `internal/runtime/housekeeping.go` — add `CleanupOptions`, `CleanupResult`, `dockerRuntime.Cleanup`, `remove`
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface; add stub impl after line 119
- Modify: `internal/cli/root_test.go:98-100` — add `Cleanup` to `mockRTForDeploy`
- Test: `internal/runtime/housekeeping_test.go` — add stub + interface tests

**Interfaces:**
- Consumes: the Task 1 helpers
- Produces:
  - `type CleanupOptions struct { DryRun bool; All bool; Containers bool; Images bool; Volumes bool; Networks bool; BuildCache bool; ProtectedApps []string; KeepImages int }`
  - `type CleanupResult struct { DryRun bool; Containers []string; Images []string; Volumes []string; Networks []string; BuildCacheReclaimed string }`
  - `(m Manager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)`
  - `(r *dockerRuntime) remove(ctx context.Context, kind, target string) error`

- [ ] **Step 1: Write the failing test + extend the mock**

Add to `internal/runtime/housekeeping_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	result, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result == nil {
		t.Fatal("Cleanup() returned nil result")
	}
	if result.DryRun {
		t.Error("DryRun = true, want false")
	}
}

func TestDockerRuntimeImplementsManager(t *testing.T) {
	var _ Manager = (*dockerRuntime)(nil)
}
```

Add to `internal/cli/root_test.go` after the `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{DryRun: opts.DryRun}, nil
}
```

Update the imports of `internal/runtime/housekeeping_test.go` to include `"context"`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... ./internal/cli/... -run "TestStubCleanup|TestDockerRuntimeImplementsManager|TestMockRTForDeployImplementsManager" -v -count=1`

Expected: FAIL — compile errors: `Manager` has no method `Cleanup`; `dockerRuntime` has no method `Cleanup`

- [ ] **Step 3: Add `CleanupOptions`/`CleanupResult` types, the `Cleanup` method, and `remove` to `internal/runtime/housekeeping.go`**

Append to `internal/runtime/housekeeping.go` (add `context`, `fmt`, `log`, `os/exec` to the imports):

```go
type CleanupOptions struct {
	DryRun        bool
	All           bool
	Containers    bool
	Images        bool
	Volumes       bool
	Networks      bool
	BuildCache    bool
	ProtectedApps []string
	KeepImages    int
}

type CleanupResult struct {
	DryRun              bool
	Containers          []string
	Images              []string
	Volumes             []string
	Networks            []string
	BuildCacheReclaimed string
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	result := &CleanupResult{DryRun: opts.DryRun}

	if opts.Containers {
		out, err := exec.CommandContext(ctx, "docker", "ps", "-a",
			"--format", "{{.ID}}|{{.State}}|{{.Labels}}").Output()
		if err != nil {
			return result, fmt.Errorf("docker ps: %w", err)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		ids := selectContainersToRemove(lines, opts.All)
		result.Containers = ids
		if !opts.DryRun {
			for _, id := range ids {
				if err := r.remove(ctx, "container", id); err != nil {
					log.Printf("[runtime] failed to remove container %s: %v", id, err)
				}
			}
		}
	}

	if opts.Images {
		out, err := exec.CommandContext(ctx, "docker", "images",
			"--format", "{{.Repository}}:{{.Tag}}|{{.ID}}|{{.CreatedAt}}").Output()
		if err != nil {
			return result, fmt.Errorf("docker images: %w", err)
		}
		imageLines := strings.Split(strings.TrimSpace(string(out)), "\n")

		usedOut, err := exec.CommandContext(ctx, "docker", "ps", "-a",
			"--format", "{{.Image}}").Output()
		if err != nil {
			return result, fmt.Errorf("docker ps: %w", err)
		}
		var usedTags []string
		for _, l := range strings.Split(strings.TrimSpace(string(usedOut)), "\n") {
			if l != "" {
				usedTags = append(usedTags, l)
			}
		}

		keep := opts.KeepImages
		if keep <= 0 {
			keep = 5
		}
		tags := selectImagesToRemove(imageLines, usedTags, opts.ProtectedApps, keep, opts.All)
		result.Images = tags
		if !opts.DryRun {
			for _, tag := range tags {
				if err := r.remove(ctx, "image", tag); err != nil {
					log.Printf("[runtime] failed to remove image %s: %v", tag, err)
				}
			}
		}
	}

	if opts.Volumes {
		out, err := exec.CommandContext(ctx, "docker", "volume", "ls",
			"--filter", "dangling=true", "--format", "{{.Name}}").Output()
		if err != nil {
			return result, fmt.Errorf("docker volume ls: %w", err)
		}
		var vols []string
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if l != "" {
				vols = append(vols, l)
			}
		}
		result.Volumes = vols
		if !opts.DryRun {
			for _, v := range vols {
				if err := r.remove(ctx, "volume", v); err != nil {
					log.Printf("[runtime] failed to remove volume %s: %v", v, err)
				}
			}
		}
	}

	if opts.Networks {
		out, err := exec.CommandContext(ctx, "docker", "network", "ls",
			"--filter", "dangling=true", "--format", "{{.ID}}|{{.Name}}").Output()
		if err != nil {
			return result, fmt.Errorf("docker network ls: %w", err)
		}
		var nets []string
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			id, name, ok := strings.Cut(l, "|")
			if !ok {
				continue
			}
			if name == "bridge" || name == "host" || name == "none" {
				continue
			}
			nets = append(nets, id)
		}
		result.Networks = nets
		if !opts.DryRun {
			for _, n := range nets {
				if err := r.remove(ctx, "network", n); err != nil {
					log.Printf("[runtime] failed to remove network %s: %v", n, err)
				}
			}
		}
	}

	if opts.BuildCache {
		if opts.DryRun {
			out, err := exec.CommandContext(ctx, "docker", "builder", "du").Output()
			if err == nil {
				result.BuildCacheReclaimed = extractTotalSpace(string(out))
			}
		} else {
			out, err := exec.CommandContext(ctx, "docker", "builder", "prune", "-f").CombinedOutput()
			if err != nil {
				return result, fmt.Errorf("docker builder prune: %w", err)
			}
			result.BuildCacheReclaimed = extractTotalSpace(string(out))
		}
	}

	return result, nil
}

func (r *dockerRuntime) remove(ctx context.Context, kind, target string) error {
	var args []string
	switch kind {
	case "container":
		args = []string{"rm", "-f", target}
	case "image":
		args = []string{"rmi", "-f", target}
	case "volume":
		args = []string{"volume", "rm", target}
	case "network":
		args = []string{"network", "rm", target}
	default:
		return fmt.Errorf("unknown resource kind %q", kind)
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}
```

- [ ] **Step 4: Add `Cleanup` to the `Manager` interface and stub in `internal/runtime/runtime.go`**

Add to the interface (after the `KeepLastNImages` line, line 36):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
```

Add to the stub (after the `KeepLastNImages` stub method, line 118):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... -run "TestStubCleanup|TestDockerRuntimeImplementsManager|TestMockRTForDeployImplementsManager" -v -count=1`

Expected: PASS

- [ ] **Step 6: Build + run full runtime and cli test suites**

Run: `go build ./...`

Run: `go test ./internal/runtime/... ./internal/cli/... -v -count=1`

Expected: Build succeeds; all tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/runtime.go internal/cli/root_test.go internal/runtime/housekeeping_test.go
git commit -m "feat: add Cleanup method to runtime for Docker housekeeping"
```

---

### Task 3: `tengiz cleanup` CLI command + docs

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, register in `init()`, add `runCleanup` + `withDefaultCategories`
- Test: `internal/cli/cleanup_test.go`
- Modify: `README.md:228-230` — insert `tengiz cleanup` section between `rm` and `rollback`
- Modify: `AGENTS.md` — CLI list + `runtime.Manager` row

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.Manager.Cleanup` (Task 2), `config.NewStoreWithEnv`, `store.ListApps` (existing)
- Produces:
  - `tengiz cleanup [--dry-run] [--all] [--containers] [--images] [--volumes] [--networks] [--build-cache]`
  - `func withDefaultCategories(opts runtime.CleanupOptions) runtime.CleanupOptions`
  - `func runCleanup(ctx context.Context, rt runtime.Manager, store *config.Store, opts runtime.CleanupOptions) (*runtime.CleanupResult, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"context"
	"reflect"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
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
	for _, flag := range []string{"dry-run", "all", "containers", "images", "volumes", "networks", "build-cache"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestWithDefaultCategories(t *testing.T) {
	empty := withDefaultCategories(runtime.CleanupOptions{})
	if !empty.Containers || !empty.Images || !empty.Volumes || !empty.Networks || !empty.BuildCache {
		t.Errorf("expected all categories enabled by default, got %+v", empty)
	}

	partial := withDefaultCategories(runtime.CleanupOptions{Images: true})
	if !partial.Images {
		t.Error("Images should stay enabled")
	}
	if partial.Containers {
		t.Error("Containers should not be auto-enabled when any category is set")
	}
}

type capturingCleanupRT struct {
	runtime.Manager
	got runtime.CleanupOptions
}

func (c *capturingCleanupRT) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	c.got = opts
	return &runtime.CleanupResult{DryRun: opts.DryRun}, nil
}

func TestRunCleanupCollectsProtectedApps(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStoreWithEnv(dir, "production")
	if err := store.SaveApp(types.AppEntry{Name: "alpha", Config: types.AppConfig{Name: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveApp(types.AppEntry{Name: "beta", Config: types.AppConfig{Name: "beta"}}); err != nil {
		t.Fatal(err)
	}

	rt := &capturingCleanupRT{Manager: runtime.NewStub()}
	opts := runtime.CleanupOptions{Containers: true}
	if _, err := runCleanup(context.Background(), rt, store, opts); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(rt.got.ProtectedApps, []string{"alpha", "beta"}) {
		t.Errorf("ProtectedApps = %v, want [alpha beta]", rt.got.ProtectedApps)
	}
	if rt.got.KeepImages != 5 {
		t.Errorf("KeepImages = %d, want 5", rt.got.KeepImages)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCommand|TestWithDefaultCategories|TestRunCleanupCollectsProtectedApps" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd`, `undefined: withDefaultCategories`, `undefined: runCleanup`

- [ ] **Step 3: Add the `cleanupCmd` command and register it + flags in `internal/cli/root.go`**

In `init()` after `rootCmd.AddCommand(runCmd)` (line 67), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

In `init()` after the `runCmd` flags block (after line 78), add:

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("all", false, "also remove stopped Tengiz containers and unused non-Tengiz images")
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz (scale-to-zero containers are preserved)")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images and stale tengiz-apps images (newest 5 per app kept)")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "remove Docker build cache")
```

- [ ] **Step 4: Add the `cleanupCmd` command, `runCleanup`, and `withDefaultCategories`**

Insert the command between `rmCmd` and `logsCmd` (after line 662) in `internal/cli/root.go`:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Remove unused Docker resources (containers, images, volumes, networks, build cache).

By default every category is cleaned. Use the category flags to clean only
specific resources:
  --containers   stopped containers NOT managed by Tengiz (scale-to-zero
                 containers are preserved)
  --images       dangling (untagged) images + stale tengiz-apps images
                 (newest 5 per app kept for rollback)
  --volumes      unused volumes
  --networks     unused networks
  --build-cache  Docker BuildKit cache

--all additionally removes stopped Tengiz containers and unused non-Tengiz
images. Use --dry-run to preview what would be removed without changing
anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")

		opts := runtime.CleanupOptions{
			DryRun:     dryRun,
			All:        all,
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
		}
		opts = withDefaultCategories(opts)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		store := config.NewStoreWithEnv(dataDir, env)

		result, err := runCleanup(cmd.Context(), rt, store, opts)
		if err != nil {
			return err
		}

		if result.DryRun {
			fmt.Println("[tengiz] DRY RUN — no resources were removed.")
		}
		fmt.Printf("[tengiz] containers removed: %d\n", len(result.Containers))
		fmt.Printf("[tengiz] images removed: %d\n", len(result.Images))
		fmt.Printf("[tengiz] volumes removed: %d\n", len(result.Volumes))
		fmt.Printf("[tengiz] networks removed: %d\n", len(result.Networks))
		if result.BuildCacheReclaimed != "" {
			fmt.Printf("[tengiz] build cache reclaimed: %s\n", result.BuildCacheReclaimed)
		}
		return nil
	},
}

func withDefaultCategories(opts runtime.CleanupOptions) runtime.CleanupOptions {
	if opts.Containers || opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		return opts
	}
	opts.Containers = true
	opts.Images = true
	opts.Volumes = true
	opts.Networks = true
	opts.BuildCache = true
	return opts
}

func runCleanup(ctx context.Context, rt runtime.Manager, store *config.Store, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	apps, err := store.ListApps()
	if err != nil {
		return nil, err
	}
	protected := make([]string, 0, len(apps))
	for _, a := range apps {
		protected = append(protected, a.Name)
	}
	sort.Strings(protected)
	opts.ProtectedApps = protected
	opts.KeepImages = 5
	return rt.Cleanup(ctx, opts)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCommand|TestWithDefaultCategories|TestRunCleanupCollectsProtectedApps" -v -count=1`

Expected: PASS

- [ ] **Step 6: Build + run all CLI tests**

Run: `go build ./...`

Run: `go test ./internal/cli/... -v -count=1`

Expected: Build succeeds; all tests PASS

- [ ] **Step 7: Document the command in `README.md`**

Insert a new section after the `### tengiz rm <app>` section (after line 228) and before `### tengiz rollback <app>`:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--all` | Also remove stopped Tengiz containers and unused non-Tengiz images |
| `--containers` | Remove stopped containers not managed by Tengiz (scale-to-zero containers are preserved) |
| `--images` | Remove dangling images and stale `tengiz-apps` images (newest 5 per app kept) |
| `--volumes` | Remove unused volumes |
| `--networks` | Remove unused networks |
| `--build-cache` | Remove Docker build cache |

If no category flag is given, all categories are cleaned. Tengiz-managed containers (labeled `tengiz-app`) and the newest 5 images per registered app are always preserved, so scale-to-zero and rollback keep working.
```

- [ ] **Step 8: Update `AGENTS.md`**

In the CLI list (after the `tengiz run ...` line, line 47), add:

```markdown
tengiz cleanup [--dry-run] [--all] [--containers|--images|--volumes|--networks|--build-cache] → remove unused Docker resources (label-protected, keeps newest 5 images per app)
```

In the Key architecture table, update the `runtime.Manager` row (line 15) to mention cleanup:

```markdown
| `runtime.Manager` | Interface for container lifecycle. `NewDocker()` = exec-based impl, `NewStub()` = test mock. Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages`, `Cleanup` for rollback + image/housekeeping cleanup. `ContainerName(name, env)` helper. |
```

- [ ] **Step 9: Verify docs didn't break the build and commit**

Run: `go build ./...`

Expected: Build succeeds

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go README.md AGENTS.md
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 4: Mark features implemented + full verification

**Files:**
- Modify: `docs/FUTURES_FEATURES.md:19` — mark #6 ✅
- Modify: `docs/FUTURES_FEATURES.md:74` — mark #56 ✅
- Modify: `docs/FUTURES_FEATURES.md:377-381` — add Status to the Docker Housekeeping feature section
- Modify: `docs/FUTURES_FEATURES.md:1518-1522` — add Status to the Granular Docker Prune feature section
- Modify: `docs/FUTURES_FEATURES.md:237-254` — add both to the Implemented table

**Interfaces:**
- Consumes: nothing new — documentation-only task

- [ ] **Step 1: Mark the priority-table rows as implemented**

In `docs/FUTURES_FEATURES.md` line 19, replace `⬜` with `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In line 74, replace `⬜` with `✅`:

```markdown
| 56 | **Granular Docker Prune Operations** ✅ | Orta | Düşük | Mükemmel | Per-category prune: containers/networks/images/volumes/buildx cache. Surgical disk management. |
```

- [ ] **Step 2: Add Status lines to the feature sections**

After the `- **Detected:** 2026-07-14` line (line 381) of the `## Docker Housekeeping (Otomatik Temizlik)` section, add:

```markdown
- **Status:** ✅ Implemented (2026-08-14)
```

After the `- **Detected:** 2026-07-17` line (line 1522) of the `## Granular Docker Prune Operations` section, add:

```markdown
- **Status:** ✅ Implemented (2026-08-14)
```

- [ ] **Step 3: Add both entries to the Implemented Features table**

In the `### ✅ Implemented Features (Not Pending)` table (lines 237-254), add two rows (keep the same column style as neighbors):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-14) |
| — | **Granular Docker Prune Operations** | Orta | Düşük | Mükemmel | ✅ Implemented (2026-08-14) |
```

- [ ] **Step 4: Run the full verification suite**

Run: `go build ./...`

Run: `go test ./... -v -count=1`

Expected: All tests PASS

Run: `go vet ./...`

Expected: No issues reported

- [ ] **Step 5: Self-review against spec**

Check against `docs/FUTURES_FEATURES.md` #6 (Docker Housekeeping):
- `tengiz cleanup` command ✅ (Task 3)
- Label-based filtering protects Tengiz-managed containers ✅ (Task 1 `selectContainersToRemove` uses `labelKey`)
- Cleans unused volumes/networks/containers/images ✅ (Task 2 `Cleanup`)
- Build cache cleanup ✅ (Task 2, `docker builder prune`)
- Env-aware protected apps ✅ (Task 3 `runCleanup` via `NewStoreWithEnv`)

Check against #56 (Granular Docker Prune Operations):
- Per-category prune: `--containers`, `--networks`, `--images`, `--volumes`, `--build-cache` ✅ (Task 3)
- Surgical control via category flags ✅
- `--dry-run` preview ✅

- [ ] **Step 6: Placeholder scan**

Search the plan for any `TBD`, `TODO`, `implement later`, `fill in details`, or `Similar to Task` patterns. None present — every step contains complete code and exact commands.

- [ ] **Step 7: Type consistency check**

- `runtime.CleanupOptions{DryRun, All, Containers, Images, Volumes, Networks, BuildCache, ProtectedApps, KeepImages}` — identical field set used in Tasks 2-3
- `runtime.CleanupResult{DryRun, Containers, Images, Volumes, Networks, BuildCacheReclaimed}` — produced by `Cleanup`, consumed by `runCleanup` return
- `(Manager) Cleanup(ctx, opts) (*CleanupResult, error)` — same signature in interface, stub, `dockerRuntime`, `mockRTForDeploy`, `capturingCleanupRT`
- `withDefaultCategories(opts) CleanupOptions` and `runCleanup(ctx, rt, store, opts) (*CleanupResult, error)` — used consistently in Task 3
- `selectContainersToRemove(lines, all)` / `selectImagesToRemove(lines, usedTags, protectedApps, keepN, all)` — signatures match between Task 1 definitions and Task 2 `Cleanup` calls
- `hasLabel(labels, labelKey)` uses the existing `labelKey = "tengiz-app"` constant — no new label constant introduced

- [ ] **Step 8: Commit**

```bash
git add docs/FUTURES_FEATURES.md
git commit -m "docs: mark Docker Housekeeping and Granular Prune as implemented"
```