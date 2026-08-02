# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by removing stale stopped containers, unreferenced `tengiz-apps/*` images, unused anonymous volumes/networks, and Docker build cache — using label-based filtering so all Tengiz-managed containers and rollback-history images are protected.

**Architecture:** Runtime exposes a new `runtime.Cleaner` interface (kept OFF `runtime.Manager` so existing mocks stay untouched) implemented by `dockerRuntime` via `os/exec`. The decision logic is extracted into pure, unit-testable functions (`staleTengizContainers`, `tengizImagesToRemove`) that parse `docker ps`/`docker images` JSON output. The CLI command computes two protection sets (`keepContainers`, `keepImages`) from the env-scoped store — every current/versioned container name, preview container, current `ImageTag`, and every `DeploymentEntry.ImageTag` (needed for rollback) — then passes them into `Cleanup`. `--dry-run` reports without deleting.

**Tech Stack:** Go 1.26, Docker CLI via `os/exec` (no Docker SDK), Cobra (CLI), existing `config.Store`, existing `runtime` label constants (`tengiz-app`, `tengiz-env`). No new dependencies.

## Global Constraints

- New command is `tengiz cleanup` with flags `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`, `--cache`, `--all`
- With no category flag, or with `--all`, cleanup targets all five categories (containers, images, volumes, networks, build cache)
- Only containers carrying the `tengiz-app` label are candidates for removal
- Running containers are NEVER removed; stopped containers whose name is in the CLI's `KeepContainers` set are NEVER removed (protects idle/scale-to-zero current containers and active previews)
- Only images whose repository starts with `tengiz-apps/` are candidates for removal
- `latest` and `<env>-latest` tags are NEVER removed (they are cheap aliases recreated on every deploy)
- Images whose full `repo:tag` is in the CLI's `KeepImages` set are NEVER removed (protects current deploy + rollback history + preview images)
- Volume/network pruning only targets objects Docker reports as unused (`--filter dangling=true`); named volumes used by apps are untouched
- `--dry-run` must not execute any destructive Docker command
- `runtime.Manager` interface is NOT changed; cleanup is exposed via a new `runtime.Cleaner` interface with one method: `Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)`
- Existing tests must continue to pass without modification
- Multi-environment aware: `--env` flag scopes the store protection sets; Docker objects are global so labels are the source of truth
- No new external dependencies
- Follow TDD: write the failing test, verify it fails, implement, verify it passes, commit

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/housekeep.go` (create) | `CleanupOptions`, `CleanupResult`, `Cleaner` interface, pure filter functions (`parseJSONLines`, `staleTengizContainers`, `tengizImagesToRemove`), and the exec-based `dockerRuntime.Cleanup` implementation |
| `internal/runtime/housekeep_test.go` (create) | Tests for the pure filter functions and the `Cleaner` contract |
| `internal/cli/cleanup.go` (create) | `tengiz cleanup` cobra command, `addCleanupFlags`, `cleanupOptionsFromFlags`, `protectSetsFromStore`, `printCleanupResult`; registers itself via package `init()` (pattern used by `internal/cli/preview.go`) |
| `internal/cli/cleanup_test.go` (create) | Tests for command registration, flag parsing, options building, and protection-set computation |
| `README.md` (modify) | Document the new `tengiz cleanup` command in the CLI Reference section |

No changes to `runtime.Manager`, `root_test.go` mocks, `idle_test.go`, or `proxy_test.go` — the `Cleaner` interface design avoids touching them.

---

### Task 1: Runtime cleanup types + pure filter functions

**Files:**
- Create: `internal/runtime/housekeep.go`
- Test: `internal/runtime/housekeep_test.go`

**Interfaces:**
- Consumes: nothing new (uses existing `labelKey` const from `internal/runtime/docker.go:76`)
- Produces: `runtime.CleanupOptions` (struct), `runtime.CleanupResult` (struct), `runtime.Cleaner` (interface), `runtime.parseJSONLines[T any](out string) ([]T, error)`, `runtime.staleTengizContainers(entries []containerEntry, keep map[string]bool) []string`, `runtime.tengizImagesToRemove(images []imageEntry, keep map[string]bool) []string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/housekeep_test.go
package runtime

import (
	"testing"
)

func TestStaleTengizContainers(t *testing.T) {
	entries := []containerEntry{
		{Names: "tengiz-myapp", State: "running"},
		{Names: "tengiz-myapp-1700000000", State: "exited"},
		{Names: "tengiz-oldapp", State: "exited"},
		{Names: "/tengiz-leadingslash", State: "exited"},
		{Names: "tengiz-nolabels", State: "created"},
	}
	keep := map[string]bool{
		"tengiz-myapp-1700000000": true,
	}
	stale := staleTengizContainers(entries, keep)
	if len(stale) != 2 {
		t.Fatalf("expected 2 stale containers, got %d: %v", len(stale), stale)
	}
	if stale[0] != "tengiz-oldapp" {
		t.Errorf("stale[0] = %q, want %q", stale[0], "tengiz-oldapp")
	}
	if stale[1] != "tengiz-leadingslash" {
		t.Errorf("stale[1] = %q, want %q", stale[1], "tengiz-leadingslash")
	}
}

func TestStaleTengizContainersEmptyKeep(t *testing.T) {
	entries := []containerEntry{
		{Names: "tengiz-a", State: "exited"},
	}
	stale := staleTengizContainers(entries, nil)
	if len(stale) != 1 || stale[0] != "tengiz-a" {
		t.Errorf("expected [tengiz-a], got %v", stale)
	}
}

func TestTengizImagesToRemove(t *testing.T) {
	images := []imageEntry{
		{Repository: "tengiz-apps/myapp", Tag: "production-1700000000"},
		{Repository: "tengiz-apps/myapp", Tag: "production-1699999999"},
		{Repository: "tengiz-apps/myapp", Tag: "production-latest"},
		{Repository: "tengiz-apps/myapp", Tag: "<none>"},
		{Repository: "tengiz-apps/other", Tag: "latest"},
		{Repository: "nginx", Tag: "alpine"},
	}
	keep := map[string]bool{
		"tengiz-apps/myapp:production-1700000000": true,
	}
	toRemove := tengizImagesToRemove(images, keep)
	if len(toRemove) != 1 {
		t.Fatalf("expected 1 image to remove, got %d: %v", len(toRemove), toRemove)
	}
	if toRemove[0] != "tengiz-apps/myapp:production-1699999999" {
		t.Errorf("toRemove[0] = %q, want %q", toRemove[0], "tengiz-apps/myapp:production-1699999999")
	}
}

func TestParseJSONLines(t *testing.T) {
	out := "{\"Repository\":\"tengiz-apps/myapp\",\"Tag\":\"production-1700000000\"}\n{\"Repository\":\"nginx\",\"Tag\":\"alpine\"}\n"
	entries, err := parseJSONLines[imageEntry](out)
	if err != nil {
		t.Fatalf("parseJSONLines: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Repository != "tengiz-apps/myapp" || entries[0].Tag != "production-1700000000" {
		t.Errorf("entries[0] = %+v, want tengiz-apps/myapp:production-1700000000", entries[0])
	}
}

func TestStubNotRequiredToImplementCleaner(t *testing.T) {
	// The stub manager intentionally does NOT implement Cleaner.
	// This guards the design decision that Cleaner is separate from Manager.
	var m interface{} = NewStub()
	if _, ok := m.(Cleaner); ok {
		t.Error("stub should not implement Cleaner (keep Cleaner off Manager interface)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStaleTengizContainers|TestTengizImagesToRemove|TestParseJSONLines|TestStubNotRequiredToImplementCleaner" -v -count=1`

Expected: FAIL with `undefined: staleTengizContainers`, `undefined: tengizImagesToRemove`, `undefined: parseJSONLines`, `undefined: Cleaner`

- [ ] **Step 3: Write minimal implementation in `internal/runtime/housekeep.go`**

```go
package runtime

import (
	"context"
	"encoding/json"
	"strings"
)

// CleanupOptions selects which categories of Docker objects to remove.
type CleanupOptions struct {
	Containers    bool
	Images        bool
	Volumes       bool
	Networks      bool
	BuildCache    bool
	DryRun        bool
	KeepContainers map[string]bool
	KeepImages     map[string]bool
}

// CleanupResult reports what was removed (or, in dry-run mode, what would be).
type CleanupResult struct {
	RemovedContainers []string
	RemovedImages     []string
	RemovedVolumes    []string
	RemovedNetworks   []string
	RemovedBuildCache []string
}

// Cleaner is implemented by runtimes that support disk housekeeping.
// It is deliberately separate from Manager so test mocks stay unaffected.
type Cleaner interface {
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
}

type containerEntry struct {
	ID     string
	Names  string
	State  string
	Labels string
}

type imageEntry struct {
	Repository string
	Tag        string
	ID         string
}

func parseJSONLines[T any](out string) ([]T, error) {
	var result []T
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, nil
}

// staleTengizContainers returns stopped tengiz container names to remove.
// Running containers and names present in keep are always protected.
func staleTengizContainers(entries []containerEntry, keep map[string]bool) []string {
	var out []string
	for _, e := range entries {
		name := strings.TrimPrefix(e.Names, "/")
		if name == "" {
			continue
		}
		if e.State == "running" {
			continue
		}
		if keep[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}

// tengizImagesToRemove returns tengiz-apps image tags to remove.
// latest and <env>-latest aliases plus tags in keep are always protected.
func tengizImagesToRemove(images []imageEntry, keep map[string]bool) []string {
	var out []string
	for _, img := range images {
		if !strings.HasPrefix(img.Repository, "tengiz-apps/") {
			continue
		}
		if img.Tag == "" || img.Tag == "<none>" || img.Tag == "latest" {
			continue
		}
		if strings.HasSuffix(img.Tag, "-latest") {
			continue
		}
		full := img.Repository + ":" + img.Tag
		if keep[full] {
			continue
		}
		out = append(out, full)
	}
	return out
}
```

(Do NOT add the `var _ Cleaner = (*dockerRuntime)(nil)` assertion here — `dockerRuntime` does not implement `Cleanup` until Task 2. That assertion is added at the end of Task 2 Step 3.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestStaleTengizContainers|TestTengizImagesToRemove|TestParseJSONLines|TestStubNotRequiredToImplementCleaner" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeep.go internal/runtime/housekeep_test.go
git commit -m "feat(runtime): add cleanup filter functions and Cleaner interface for housekeeping"
```

---

### Task 2: Exec-based `Cleanup` on dockerRuntime

**Files:**
- Modify: `internal/runtime/housekeep.go` (append the exec-based methods)
- Test: `internal/runtime/housekeep_test.go`

**Interfaces:**
- Consumes: `parseJSONLines`, `staleTengizContainers`, `tengizImagesToRemove`, `CleanupOptions`, `CleanupResult`, `Cleaner` from Task 1; `RemoveImage` from `internal/runtime/cleanup.go:12`; `labelKey` from `internal/runtime/docker.go:76`
- Produces: `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)` satisfying `runtime.Cleaner`

- [ ] **Step 1: Write the failing contract test**

```go
// internal/runtime/housekeep_test.go — add
func TestDockerRuntimeImplementsCleaner(t *testing.T) {
	var c Cleaner = (*dockerRuntime)(nil)
	if c == nil {
		t.Fatal("dockerRuntime does not implement Cleaner")
	}
}
```

- [ ] **Step 2: Run test to verify it fails to compile**

Run: `go test ./internal/runtime/... -run "TestDockerRuntimeImplementsCleaner" -v -count=1`

Expected: FAIL (compile error — `*dockerRuntime does not implement Cleaner (missing method Cleanup)`)

- [ ] **Step 3: Append the exec-based implementation to `internal/runtime/housekeep.go`**

Add the following methods and imports at the end of `internal/runtime/housekeep.go` (change the import block to include `fmt`, `log`, `os/exec`, `sort`), and add the compile-time contract assertion after the imports:

```go
var _ Cleaner = (*dockerRuntime)(nil)
```

Implementation methods:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	if opts.KeepContainers == nil {
		opts.KeepContainers = map[string]bool{}
	}
	if opts.KeepImages == nil {
		opts.KeepImages = map[string]bool{}
	}

	result := &CleanupResult{}
	if opts.Containers {
		if err := r.cleanupContainers(ctx, opts, result); err != nil {
			log.Printf("[runtime] container cleanup: %v", err)
		}
	}
	if opts.Images {
		if err := r.cleanupImages(ctx, opts, result); err != nil {
			log.Printf("[runtime] image cleanup: %v", err)
		}
	}
	if opts.Volumes {
		if err := r.cleanupVolumes(ctx, opts, result); err != nil {
			log.Printf("[runtime] volume cleanup: %v", err)
		}
	}
	if opts.Networks {
		if err := r.cleanupNetworks(ctx, opts, result); err != nil {
			log.Printf("[runtime] network cleanup: %v", err)
		}
	}
	if opts.BuildCache {
		if err := r.cleanupBuildCache(ctx, opts, result); err != nil {
			log.Printf("[runtime] build cache cleanup: %v", err)
		}
	}
	return result, nil
}

func (r *dockerRuntime) cleanupContainers(ctx context.Context, opts CleanupOptions, result *CleanupResult) error {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s", labelKey),
		"--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker ps -a: %w\n%s", err, string(out))
	}
	entries, err := parseJSONLines[containerEntry](string(out))
	if err != nil {
		return fmt.Errorf("parse containers: %w", err)
	}
	stale := staleTengizContainers(entries, opts.KeepContainers)
	sort.Strings(stale)
	for _, name := range stale {
		if opts.DryRun {
			result.RemovedContainers = append(result.RemovedContainers, name)
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "rm", "-f", name)
		if out, err := rm.CombinedOutput(); err != nil {
			log.Printf("[runtime] failed to remove container %s: %v\n%s", name, err, string(out))
			continue
		}
		result.RemovedContainers = append(result.RemovedContainers, name)
	}
	return nil
}

func (r *dockerRuntime) cleanupImages(ctx context.Context, opts CleanupOptions, result *CleanupResult) error {
	cmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	entries, err := parseJSONLines[imageEntry](string(out))
	if err != nil {
		return fmt.Errorf("parse images: %w", err)
	}
	stale := tengizImagesToRemove(entries, opts.KeepImages)
	sort.Strings(stale)
	for _, tag := range stale {
		if opts.DryRun {
			result.RemovedImages = append(result.RemovedImages, tag)
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove image %s: %v", tag, err)
			continue
		}
		result.RemovedImages = append(result.RemovedImages, tag)
	}

	dangling, err := r.danglingImageIDs(ctx)
	if err != nil {
		log.Printf("[runtime] dangling image list: %v", err)
		return nil
	}
	sort.Strings(dangling)
	for _, id := range dangling {
		if opts.DryRun {
			result.RemovedImages = append(result.RemovedImages, id)
			continue
		}
		if err := r.RemoveImage(ctx, id); err != nil {
			log.Printf("[runtime] failed to remove dangling image %s: %v", id, err)
			continue
		}
		result.RemovedImages = append(result.RemovedImages, id)
	}
	return nil
}

func (r *dockerRuntime) danglingImageIDs(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images -q: %w\n%s", err, string(out))
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

func (r *dockerRuntime) cleanupVolumes(ctx context.Context, opts CleanupOptions, result *CleanupResult) error {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if opts.DryRun {
			result.RemovedVolumes = append(result.RemovedVolumes, name)
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "volume", "rm", "-f", name)
		if out, err := rm.CombinedOutput(); err != nil {
			log.Printf("[runtime] failed to remove volume %s: %v\n%s", name, err, string(out))
			continue
		}
		result.RemovedVolumes = append(result.RemovedVolumes, name)
	}
	return nil
}

func (r *dockerRuntime) cleanupNetworks(ctx context.Context, opts CleanupOptions, result *CleanupResult) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if opts.DryRun {
			result.RemovedNetworks = append(result.RemovedNetworks, name)
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "network", "rm", "-f", name)
		if out, err := rm.CombinedOutput(); err != nil {
			log.Printf("[runtime] failed to remove network %s: %v\n%s", name, err, string(out))
			continue
		}
		result.RemovedNetworks = append(result.RemovedNetworks, name)
	}
	return nil
}

func (r *dockerRuntime) cleanupBuildCache(ctx context.Context, opts CleanupOptions, result *CleanupResult) error {
	if opts.DryRun {
		result.RemovedBuildCache = append(result.RemovedBuildCache, "build-cache")
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	result.RemovedBuildCache = append(result.RemovedBuildCache, "build-cache")
	return nil
}
```

- [ ] **Step 4: Run tests and build to verify they pass**

Run: `go test ./internal/runtime/... -run "TestDockerRuntimeImplementsCleaner" -v -count=1`

Expected: PASS

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

Run: `go build ./...`

Expected: succeeds

- [ ] **Step 5: Run vet**

Run: `go vet ./internal/runtime/...`

Expected: no issues

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeep.go
git commit -m "feat(runtime): implement exec-based docker cleanup with dry-run support"
```

---

### Task 3: CLI `tengiz cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Cleaner` + `runtime.CleanupOptions`/`runtime.CleanupResult` (Tasks 1-2), `config.NewStoreWithEnv(dataDir, env)`, `getEnv(cmd)` from `internal/cli/root.go:97`, package var `dataDir` from `internal/cli/root.go:32`
- Produces: `cleanupCmd` (registered via package `init()`), `addCleanupFlags(cmd *cobra.Command)`, `cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions`, `protectSetsFromStore(store *config.Store) (map[string]bool, map[string]bool)`, `printCleanupResult(result *runtime.CleanupResult, opts runtime.CleanupOptions)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache", "all"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func newCleanupTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	addCleanupFlags(cmd)
	return cmd
}

func TestCleanupOptionsDefaultAll(t *testing.T) {
	cmd := newCleanupTestCmd(t)
	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("default should enable all categories, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("default should not be dry-run")
	}
}

func TestCleanupOptionsAllFlag(t *testing.T) {
	cmd := newCleanupTestCmd(t)
	cmd.Flags().Set("all", "true")
	cmd.Flags().Set("dry-run", "true")
	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("--all should enable all categories, got %+v", opts)
	}
	if !opts.DryRun {
		t.Error("dry-run should be enabled")
	}
}

func TestCleanupOptionsSelective(t *testing.T) {
	cmd := newCleanupTestCmd(t)
	cmd.Flags().Set("images", "true")
	cmd.Flags().Set("dry-run", "true")
	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Images {
		t.Error("images should be enabled")
	}
	if opts.Containers || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("only images should be enabled, got %+v", opts)
	}
	if !opts.DryRun {
		t.Error("dry-run should be enabled")
	}
}

func TestProtectSetsFromStore(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)

	store.SaveApp(types.AppEntry{
		Name:             "myapp",
		ImageTag:         "tengiz-apps/myapp:production-1700000000",
		Port:             9000,
		DeploymentSuffix: "1700000000",
		Config: types.AppConfig{
			Name:        "myapp",
			Environment: "production",
		},
	})
	store.AddDeployment("myapp", types.DeploymentEntry{
		ID:       "1700000000",
		ImageTag: "tengiz-apps/myapp:production-1700000000",
		Status:   string(types.DeployActive),
	})
	store.AddDeployment("myapp", types.DeploymentEntry{
		ID:       "1699999999",
		ImageTag: "tengiz-apps/myapp:production-1699999999",
		Status:   string(types.DeployPrevious),
	})

	keepContainers, keepImages := protectSetsFromStore(store)

	if !keepContainers["tengiz-myapp"] {
		t.Error("expected current container tengiz-myapp to be protected")
	}
	if !keepContainers["tengiz-myapp-1700000000"] {
		t.Error("expected versioned container tengiz-myapp-1700000000 to be protected")
	}
	if !keepImages["tengiz-apps/myapp:production-1700000000"] {
		t.Error("expected current image tag to be protected")
	}
	if !keepImages["tengiz-apps/myapp:production-1699999999"] {
		t.Error("expected rollback (previous deployment) image tag to be protected")
	}
}

func TestProtectSetsFromStoreStagingEnv(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStoreWithEnv(dir, "staging")

	store.SaveApp(types.AppEntry{
		Name: "myapp",
		Config: types.AppConfig{
			Name:        "myapp",
			Environment: "staging",
		},
	})

	keepContainers, _ := protectSetsFromStore(store)
	if !keepContainers["tengiz-myapp-staging"] {
		t.Error("expected staging container tengiz-myapp-staging to be protected")
	}
}

func TestProtectSetsFromStoreIncludesPreviews(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)

	store.AddPreview(types.PreviewEntry{
		AppName:       "myapp",
		PRNumber:      42,
		ImageTag:      "tengiz-apps/myapp:pr-42-1700000000",
		ContainerName: "tengiz-myapp-pr-42",
		Status:        string(types.PreviewActive),
	})

	keepContainers, keepImages := protectSetsFromStore(store)
	if !keepContainers["tengiz-myapp-pr-42"] {
		t.Error("expected preview container tengiz-myapp-pr-42 to be protected")
	}
	if !keepImages["tengiz-apps/myapp:pr-42-1700000000"] {
		t.Error("expected preview image tag to be protected")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: addCleanupFlags`, `undefined: cleanupOptionsFromFlags`, `undefined: protectSetsFromStore`

- [ ] **Step 3: Write minimal implementation in `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove stale containers, unused images, volumes, networks, and build cache",
	Long: "Removes disk space consumed by old zero-downtime containers, unreferenced Tengiz " +
		"images, unused volumes/networks, and Docker build cache. Containers and images still " +
		"referenced by active deployments or rollback history are protected. " +
		"Use --dry-run to preview.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)

		opts := cleanupOptionsFromFlags(cmd)
		opts.KeepContainers, opts.KeepImages = protectSetsFromStore(store)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		cleaner, ok := rt.(runtime.Cleaner)
		if !ok {
			return fmt.Errorf("runtime does not support cleanup")
		}

		result, err := cleaner.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}
		printCleanupResult(result, opts)
		return nil
	},
}

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cmd.Flags().Bool("containers", false, "remove stale stopped containers only")
	cmd.Flags().Bool("images", false, "remove unused Tengiz images only")
	cmd.Flags().Bool("volumes", false, "remove unused anonymous volumes only")
	cmd.Flags().Bool("networks", false, "remove unused Docker networks only")
	cmd.Flags().Bool("cache", false, "prune Docker build cache only")
	cmd.Flags().Bool("all", false, "remove everything (this is the default)")
}

func init() {
	addCleanupFlags(cleanupCmd)
	rootCmd.AddCommand(cleanupCmd)
}

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	cache, _ := cmd.Flags().GetBool("cache")

	opts := runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: cache,
		DryRun:     dryRun,
	}
	noneSelected := !containers && !images && !volumes && !networks && !cache
	if all || noneSelected {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts
}

func protectSetsFromStore(store *config.Store) (map[string]bool, map[string]bool) {
	keepContainers := make(map[string]bool)
	keepImages := make(map[string]bool)

	apps, err := store.ListApps()
	if err != nil {
		return keepContainers, keepImages
	}
	for _, app := range apps {
		env := app.Config.Environment
		if env == "" {
			env = app.Environment
		}
		cn := runtime.ContainerName(app.Name, env)
		keepContainers[cn] = true
		if app.DeploymentSuffix != "" {
			keepContainers[fmt.Sprintf("%s-%s", cn, app.DeploymentSuffix)] = true
		}
		if app.ImageTag != "" {
			keepImages[app.ImageTag] = true
		}
		deps, depErr := store.GetDeployments(app.Name)
		if depErr == nil {
			for _, d := range deps {
				if d.ImageTag != "" {
					keepImages[d.ImageTag] = true
				}
			}
		}
	}

	previews, pErr := store.ListAllPreviews()
	if pErr == nil {
		for _, pv := range previews {
			if pv.ContainerName != "" {
				keepContainers[pv.ContainerName] = true
			} else {
				keepContainers[fmt.Sprintf("tengiz-%s-pr-%d", pv.AppName, pv.PRNumber)] = true
			}
			if pv.ImageTag != "" {
				keepImages[pv.ImageTag] = true
			}
		}
	}

	return keepContainers, keepImages
}

func printCleanupResult(result *runtime.CleanupResult, opts runtime.CleanupOptions) {
	verb := "removed"
	if opts.DryRun {
		verb = "would remove"
	}
	if opts.Containers {
		printCleanupSection("containers", result.RemovedContainers, verb)
	}
	if opts.Images {
		printCleanupSection("images", result.RemovedImages, verb)
	}
	if opts.Volumes {
		printCleanupSection("volumes", result.RemovedVolumes, verb)
	}
	if opts.Networks {
		printCleanupSection("networks", result.RemovedNetworks, verb)
	}
	if opts.BuildCache {
		if opts.DryRun {
			fmt.Println("[tengiz] would prune Docker build cache")
		} else {
			fmt.Println("[tengiz] pruned Docker build cache")
		}
	}
}

func printCleanupSection(kind string, items []string, verb string) {
	if len(items) == 0 {
		fmt.Printf("[tengiz] no %s to %s\n", kind, verb)
		return
	}
	fmt.Printf("[tengiz] %s %d %s:\n", verb, len(items), kind)
	for _, item := range items {
		fmt.Printf("  - %s\n", item)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all CLI tests**

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

Run: `go build ./...`

Expected: succeeds

- [ ] **Step 6: Run vet**

Run: `go vet ./...`

Expected: no issues

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command with dry-run and per-category flags"
```

---

### Task 4: Documentation + full verification

**Files:**
- Modify: `README.md` (add `tengiz cleanup` section in CLI Reference, after `tengiz rollback`)

**Interfaces:**
- Consumes: nothing new
- Produces: documented command behavior matching the implementation

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

Insert this section right after the `### \`tengiz rollback <app>\`` block (which ends at README.md line 236):

```markdown
### `tengiz cleanup`

Reclaim disk space by removing stale containers, unused images, volumes, networks, and Docker build cache. The most common disk-space fix on single-server deployments.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--containers` | Remove stale stopped Tengiz containers only |
| `--images` | Remove unused `tengiz-apps/*` images only |
| `--volumes` | Remove unused anonymous volumes only |
| `--networks` | Remove unused Docker networks only |
| `--cache` | Prune Docker build cache only |
| `--all` | Remove everything (this is the default) |

Running `tengiz cleanup` with no category flag removes everything. Containers that are currently running, containers referenced by active deployments or preview environments, images referenced by the current deployment or rollback history, and the `-latest` image aliases are never removed. Always use `--dry-run` first to preview the changes:

```
tengiz cleanup --dry-run
tengiz cleanup
tengiz cleanup --containers --images
```
```

- [ ] **Step 2: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS (the slow proxy TCP-timeout tests and time-sensitive idle tests may take a few seconds each but must pass)

- [ ] **Step 3: Run static analysis and build**

Run: `go vet ./...`

Expected: no issues

Run: `go build -o /tmp/tengiz-check .`

Expected: succeeds

- [ ] **Step 4: Manual smoke test (requires Docker)**

Run: `tengiz cleanup --dry-run`

Expected: prints sections for containers/images/volumes/networks/build-cache with `would remove` wording; no Docker objects deleted.

- [ ] **Step 5: Self-review against the feature spec**

Check against the spec from `docs/FUTURES_FEATURES.md`:
- Label-based `docker system prune` ✅ (Task 2 — `docker ps -a --filter label=tengiz-app`, protects via `KeepContainers`/`KeepImages`)
- `tengiz cleanup` command ✅ (Task 3 — `cleanupCmd`)
- Periodic cleaning of unused volume/network/container/images ✅ (Task 2 — all five categories)
- Tengiz-managed containers protected by labels ✅ (Task 2 — `labelKey` filter + keep sets)
- Granular per-category operations ✅ (Task 3 — `--containers`/`--images`/`--volumes`/`--networks`/`--cache` flags, also satisfying FUTURES #56)
- Dry-run safety ✅ (Task 2 — `opts.DryRun` skips all destructive commands)
- Rollback history preserved ✅ (Task 3 — `protectSetsFromStore` keeps all `DeploymentEntry.ImageTag`)

- [ ] **Step 6: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task", "add error handling", "add validation". Every code step contains complete, compilable code; error handling is written inline in the provided code.

- [ ] **Step 7: Type consistency check**

- `runtime.CleanupOptions` fields: `Containers`, `Images`, `Volumes`, `Networks`, `BuildCache`, `DryRun`, `KeepContainers`, `KeepImages` — used identically in Tasks 1, 2, 3
- `runtime.CleanupResult` fields: `RemovedContainers`, `RemovedImages`, `RemovedVolumes`, `RemovedNetworks`, `RemovedBuildCache` — set in Task 2, read in Task 3 `printCleanupResult`
- `runtime.Cleaner` interface method `Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)` — Task 1 declares, Task 2 implements, Task 3 type-asserts
- `staleTengizContainers(entries []containerEntry, keep map[string]bool) []string` — Task 1 defines, Task 2 calls
- `tengizImagesToRemove(images []imageEntry, keep map[string]bool) []string` — Task 1 defines, Task 2 calls
- `containerEntry` fields `ID/Names/State/Labels` match Docker's `docker ps --format {{json .}}` output keys
- `imageEntry` fields `Repository/Tag/ID` match Docker's `docker images --format {{json .}}` output keys
- `protectSetsFromStore(store *config.Store) (map[string]bool, map[string]bool)` — Task 3 defines and calls
- Container names in Task 3 keep sets use `runtime.ContainerName(app.Name, env)` + `-<DeploymentSuffix>` (matches `CreateVersioned` naming in `internal/runtime/docker.go:505-511`) and `tengiz-<app>-pr-<n>` for previews (matches `internal/preview/manager.go:40-41`)
- Image tags use the `tengiz-apps/<app>:<env>-<deploymentID>` convention from `internal/builder/builder.go:61`

- [ ] **Step 8: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```
