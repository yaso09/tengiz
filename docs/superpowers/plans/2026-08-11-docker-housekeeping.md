# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning unused Docker resources (stopped foreign containers, dangling/old images, unused volumes and networks, build cache) while never touching Tengiz-managed containers or active image tags.

**Architecture:** A new `runtime.Manager.Cleanup(ctx, CleanupOptions) (*CleanupResult, error)` method on the `dockerRuntime` orchestrates five categories (containers, images, volumes, networks, build cache). Each category lists candidate resources first, then removes them — this makes `--dry-run` trivial and the logic testable. Decision logic lives in pure selector helpers (`parseImageRows`, `selectImagesRemove`, `selectKeepImages`, `parseForeignContainers`) that parse `docker <subcommand> --format` output and are unit-tested with sample strings, since tests cannot run Docker. `KeepLastNImages` is refactored to share the retention selector and gains a bugfix: `-latest` tags (used by non-production envs) are now preserved. The CLI wires flags → options, loads app names from the env-scoped store for image retention, and prints a report.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` calling the `docker` CLI (no SDK), existing `runtime.Manager` interface and `config.Store`.

## Global Constraints

- New top-level command: `tengiz cleanup`; no new external dependencies
- Never remove anything carrying the `tengiz-app` label (containers) — scale-to-zero stopped containers are always protected
- Image pruning touches ONLY: dangling images (`<none>:<none>`) and repos matching `tengiz-apps/<app>` for apps passed in `opts.Apps`
- Image retention keeps the newest `--keep` deployment tags per app (default 5); tags suffixed `:latest` or `-latest` are NEVER removed
- If no category flag is given (and `--all` not set), all five categories run
- Confirmation prompt required unless `--yes`/`-y` or `--dry-run`
- `--dry-run` must show what would be removed without removing anything
- Env-aware: app list for image retention comes from `config.NewStoreWithEnv(dataDir, env)`; global `--env` flag
- Docker is invoked via `os/exec` (`docker` in PATH), matching every other runtime operation
- Existing tests must continue to pass without modification to their assertions

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/housekeeping.go` | (new) `CleanupOptions`/`CleanupResult` used by interface + all docker exec logic + pure selectors (`imageRow`, `parseImageRows`, `selectImagesRemove`, `selectKeepImages`, `parseForeignContainers`, `parseIDNameRows`) |
| `internal/runtime/housekeeping_test.go` | (new) Unit tests for selectors + stub `Cleanup` |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager` implementation; register options/result types (defined in housekeeping.go) |
| `internal/runtime/cleanup.go` | Refactor `KeepLastNImages` to use `selectKeepImages` |
| `internal/runtime/cleanup_test.go` | Add `-latest` preservation test for the retention selector |
| `internal/cli/cleanup.go` | (new) `cleanupCmd` (Cobra), flag registration, RunE, `buildCleanupOptions`, `confirmCleanup`, `formatCleanupReport` |
| `internal/cli/cleanup_test.go` | (new) CLI tests: registration, options mapping, report formatting |
| `internal/cli/root_test.go` | Add `Cleanup` stub to `mockRTForDeploy` (compilation) |
| `internal/proxy/proxy_test.go` | Add `Cleanup` stub to `mockRuntime` (compilation) |
| `internal/idle/idle_test.go` | Add `Cleanup` stub to `mockRuntime` (compilation) |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as Implemented |

---

### Task 1: Add `Cleanup` API to the runtime Manager interface

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Modify: `internal/runtime/runtime.go:31-49` (interface) and `:113-122` (stub)
- Modify: `internal/cli/root_test.go:100`, `internal/proxy/proxy_test.go:15-35`, `internal/idle/idle_test.go:14-34`
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{Containers, Images, Volumes, Networks, BuildCache, DryRun bool; Keep int; Apps []string}`, `runtime.CleanupResult{Containers, Images, Volumes, Networks, BuildCache int}`, `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)`

- [ ] **Step 1: Create feature branch and write the failing test**

```bash
git checkout -b feat/docker-housekeeping
```

```go
// internal/runtime/housekeeping_test.go
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil {
		t.Fatal("Cleanup() = nil, want non-nil *CleanupResult")
	}
	if res.Containers != 0 || res.Images != 0 || res.Volumes != 0 || res.Networks != 0 || res.BuildCache != 0 {
		t.Fatalf("stub Cleanup() = %+v, want all-zero result", res)
	}
}

func TestCleanupOptionsDefaultKeep(t *testing.T) {
	if opts := CleanupOptions{}; opts.Keep != 0 {
		t.Fatal("zero-value CleanupOptions should have Keep=0 (CLI supplies default)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestStubCleanup|TestCleanupOptionsDefaultKeep' -v -count=1`

Expected: FAIL with `undefined: CleanupOptions` / `undefined: Cleanup`

- [ ] **Step 3: Create the types in `internal/runtime/housekeeping.go`**

```go
package runtime

import "context"

type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
	Keep       int
	Apps       []string
}

type CleanupResult struct {
	Containers int
	Images     int
	Volumes    int
	Networks   int
	BuildCache int
}

func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{}, nil
}
```

- [ ] **Step 4: Add the method to the `Manager` interface in `internal/runtime/runtime.go`**

After `Run(...) error` in the interface block (line 48), add:

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
```

- [ ] **Step 5: Update the three test mocks to satisfy the interface (compilation)**

`internal/cli/root_test.go` — inside `mockRTForDeploy` (after line 100):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{}, nil
}
```

`internal/proxy/proxy_test.go` — inside `mockRuntime` (after line 35):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{}, nil
}
```

`internal/idle/idle_test.go` — inside `mockRuntime` (after line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestStubCleanup|TestCleanupOptionsDefaultKeep' -v -count=1`

Expected: PASS

- [ ] **Step 7: Run all affected packages**

Run: `go test ./internal/runtime/ ./internal/cli/ ./internal/proxy/ ./internal/idle/ -count=1`

Expected: PASS (note: proxy tests take ~2s each, idle tests sleep ~50ms granularity)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go internal/runtime/runtime.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add Cleanup API to Manager for docker housekeeping"
```

---

### Task 2: Pure image retention & container selectors

**Files:**
- Create: `internal/runtime/housekeeping.go` (append selectors; no docker exec yet)
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult` (Task 1)
- Produces: `imageRow{Repo, Tag, ID, CreatedAt string}`, `parseImageRows(out string) []imageRow`, `parseForeignContainers(out string) []string`, `parseIDNameRows(out string) []idNameRow`, `selectImagesRemove(rows []imageRow, apps []string, keep int) []string`, `selectKeepImages(rows []imageRow, appName string, n int) []string`, `keepTag(tag string) bool`

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/runtime/housekeeping_test.go
import "reflect"

func TestParseImageRows(t *testing.T) {
	out := "tengiz-apps/myapp|2026-01-01-100|sha1|2026-01-01 00:00:00 +0000 UTC\n" +
		"<none>|<none>|sha2|2026-01-02 00:00:00 +0000 UTC\n" +
		"nginx|latest|sha3|2026-01-03 00:00:00 +0000 UTC\n"
	rows := parseImageRows(out)
	want := []imageRow{
		{"tengiz-apps/myapp", "2026-01-01-100", "sha1", "2026-01-01 00:00:00 +0000 UTC"},
		{"<none>", "<none>", "sha2", "2026-01-02 00:00:00 +0000 UTC"},
		{"nginx", "latest", "sha3", "2026-01-03 00:00:00 +0000 UTC"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("parseImageRows() = %+v, want %+v", rows, want)
	}
}

func TestSelectImagesRemove(t *testing.T) {
	rows := []imageRow{
		{"tengiz-apps/myapp", "2026-01-01-100", "sha-oldest", "2026-01-01 00:00:00 +0000 UTC"},
		{"tengiz-apps/myapp", "2026-01-02-101", "sha-mid", "2026-01-02 00:00:00 +0000 UTC"},
		{"tengiz-apps/myapp", "2026-01-03-102", "sha-newest", "2026-01-03 00:00:00 +0000 UTC"},
		{"tengiz-apps/myapp", "production-latest", "sha-latest", "2026-01-04 00:00:00 +0000 UTC"},
		{"<none>", "<none>", "sha-dangling", "2026-01-05 00:00:00 +0000 UTC"},
		{"nginx", "latest", "sha-foreign", "2026-01-06 00:00:00 +0000 UTC"},
	}
	got := selectImagesRemove(rows, []string{"myapp"}, 2)
	want := []string{"sha-dangling", "tengiz-apps/myapp:2026-01-01-100"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectImagesRemove() = %v, want %v", got, want)
	}
}

func TestSelectKeepImagesPreservesLatest(t *testing.T) {
	rows := []imageRow{
		{"tengiz-apps/myapp", "2026-01-01-100", "sha1", "2026-01-01 00:00:00 +0000 UTC"},
		{"tengiz-apps/myapp", "2026-01-02-101", "sha2", "2026-01-02 00:00:00 +0000 UTC"},
		{"tengiz-apps/myapp", "production-latest", "sha3", "2026-01-03 00:00:00 +0000 UTC"},
	}
	got := selectKeepImages(rows, "myapp", 1)
	want := []string{"tengiz-apps/myapp:2026-01-01-100"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectKeepImages() = %v, want %v", got, want)
	}
}

func TestParseForeignContainers(t *testing.T) {
	out := "abc123|<no value>\n" +
		"def456|myapp\n" +
		"ghi789|\n"
	got := parseForeignContainers(out)
	want := []string{"abc123", "ghi789"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseForeignContainers() = %v, want %v", got, want)
	}
}

func TestParseIDNameRows(t *testing.T) {
	out := "n1|bridge\nn2|tengiz-net\nn3|host\n" +
		"v0|myvol\n"
	rows := parseIDNameRows(out)
	if len(rows) != 4 {
		t.Fatalf("parseIDNameRows() len = %d, want 4", len(rows))
	}
	if rows[0].ID != "n1" || rows[0].Name != "bridge" {
		t.Fatalf("parseIDNameRows()[0] = %+v", rows[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParseImageRows|TestSelectImagesRemove|TestSelectKeepImagesPreservesLatest|TestParseForeignContainers|TestParseIDNameRows' -v -count=1`

Expected: FAIL with `undefined: parseImageRows` (etc.)

- [ ] **Step 3: Implement the selectors (append to `internal/runtime/housekeeping.go`)**

```go
import "strings"

type imageRow struct {
	Repo      string
	Tag       string
	ID        string
	CreatedAt string
}

func parseImageRows(out string) []imageRow {
	var rows []imageRow
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			continue
		}
		rows = append(rows, imageRow{
			Repo:      parts[0],
			Tag:       parts[1],
			ID:        parts[2],
			CreatedAt: parts[3],
		})
	}
	return rows
}

type idNameRow struct {
	ID   string
	Name string
}

func parseIDNameRows(out string) []idNameRow {
	var rows []idNameRow
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		rows = append(rows, idNameRow{ID: strings.TrimSpace(parts[0]), Name: strings.TrimSpace(parts[1])})
	}
	return rows
}

// parseForeignContainers returns container IDs that carry no tengiz-app label,
// given output of: docker ps -a --format "{{.ID}}|{{.Label \"tengiz-app\"}}"
func parseForeignContainers(out string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		labelVal := parts[1]
		if labelVal == "" || labelVal == "<no value>" {
			ids = append(ids, parts[0])
		}
	}
	return ids
}

func keepTag(tag string) bool {
	return tag == "" || tag == "<none>" ||
		strings.HasSuffix(tag, ":latest") || strings.HasSuffix(tag, "-latest")
}

// selectImagesRemove returns image targets to delete: dangling image IDs plus
// old per-app deployment tags beyond the newest `keep` for each app in apps.
func selectImagesRemove(rows []imageRow, apps []string, keep int) []string {
	if keep <= 0 {
		keep = 5
	}
	wanted := make(map[string]bool, len(apps))
	for _, a := range apps {
		wanted["tengiz-apps/"+a] = true
	}
	var targets []string
	appRows := make(map[string][]imageRow)
	for _, r := range rows {
		if r.Repo == "<none>" {
			targets = append(targets, r.ID)
			continue
		}
		if !wanted[r.Repo] {
			continue
		}
		appRows[r.Repo] = append(appRows[r.Repo], r)
	}
	for repo, group := range appRows {
		targets = append(targets, selectKeepImages(group, strings.TrimPrefix(repo, "tengiz-apps/"), keep)...)
	}
	return targets
}

// selectKeepImages returns old tags (repo:tag) for appName to delete so that
// only the newest `n` non-latest deployment images remain retained.
func selectKeepImages(rows []imageRow, appName string, n int) []string {
	if n <= 0 {
		n = 5
	}
	var removable []imageRow
	for _, r := range rows {
		if r.Repo == "tengiz-apps/"+appName && !keepTag(r.Tag) {
			removable = append(removable, r)
		}
	}
	// Oldest first — we remove the oldest excess images and keep the newest n.
	sort.SliceStable(removable, func(i, j int) bool {
		return removable[i].CreatedAt < removable[j].CreatedAt
	})
	excess := len(removable) - n
	var targets []string
	for i := 0; i < excess && i < len(removable); i++ {
		r := removable[i]
		targets = append(targets, r.Repo+":"+r.Tag)
	}
	return targets
}
```

Note: `sort` and `strings` are already imported in the package; add imports as needed using `goimports`/manual.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestParseImageRows|TestSelectImagesRemove|TestSelectKeepImagesPreservesLatest|TestParseForeignContainers|TestParseIDNameRows' -v -count=1`

Expected: PASS

- [ ] **Step 5: Run go vet**

Run: `go vet ./internal/runtime/`

Expected: No issues

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat(runtime): add image retention and container selectors for housekeeping"
```

---

### Task 3: Refactor `KeepLastNImages` to share the retention selector (bugfix for `-latest`)

**Files:**
- Modify: `internal/runtime/cleanup.go:21-59`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `selectKeepImages`, `parseImageRows` (Task 2)
- Produces: refactored `KeepLastNImages(ctx, appName string, n int) error` (same signature, now preserves `-latest` tags)

- [ ] **Step 1: Write the failing test for `-latest` preservation**

```go
// internal/runtime/cleanup_test.go (append)
import "reflect"

func TestSelectImagesRetentionRefactor(t *testing.T) {
	// Mirrors every row KeepLastNImages can now handle (no reference filter).
	rows := []imageRow{
		{"tengiz-apps/myapp", "production-100", "sha1", "2026-01-01 00:00:00 +0000 UTC"},
		{"tengiz-apps/myapp", "production-101", "sha2", "2026-01-02 00:00:00 +0000 UTC"},
		{"tengiz-apps/myapp", "production-latest", "sha3", "2026-01-03 00:00:00 +0000 UTC"},
		{"tengiz-apps/other", "production-999", "sha4", "2026-01-01 00:00:00 +0000 UTC"},
	}
	got := selectKeepImages(rows, "myapp", 1)
	// production-100 removed; production-101 kept; -latest preserved; other app untouched
	want := []string{"tengiz-apps/myapp:production-100"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectKeepImages() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestSelectImagesRetentionRefactor -v -count=1`

Expected: FAIL (`undefined: imageRow` — selector not yet wired into this file's compile unit test) — the selector exists, so this should actually compile; it fails only if `selectKeepImages`/`imageRow` are missing. If they exist, the assertion already passes — this step validates the intended behavior is captured.

- [ ] **Step 3: Rewrite `KeepLastNImages` in `internal/runtime/cleanup.go`**

Replace the body of `KeepLastNImages` (lines 21-58) with:

```go
func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	if n <= 0 {
		n = 5
	}
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--format", "{{.Repository}}|{{.Tag}}|{{.ID}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}
	rows := parseImageRows(string(out))
	if len(rows) == 0 {
		return nil
	}
	for _, target := range selectKeepImages(rows, appName, n) {
		if err := r.RemoveImage(ctx, target); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", target, err)
		}
	}
	return nil
}
```

Now remove the now-unused `sort` import from `internal/runtime/cleanup.go` (it was only used by the old implementation).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestSelectImagesRetentionRefactor|TestStubKeepLastNImages' -v -count=1`

Expected: PASS

Run: `go test ./internal/runtime/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "fix(runtime): KeepLastNImages preserves env -latest tags via shared selector"
```

---

### Task 4: Implement `dockerRuntime.Cleanup` (exec-based housekeeping)

**Files:**
- Modify: `internal/runtime/housekeeping.go` (append exec methods)
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `selectImagesRemove`, `parseForeignContainers`, `parseIDNameRows`
- Produces: `dockerRuntime.Cleanup(ctx, opts) (*CleanupResult, error)` plus private helpers `pruneContainers`, `pruneImages`, `pruneVolumes`, `pruneNetworks`, `pruneBuildCache`

- [ ] **Step 1: Write the failing plan-command test (no docker executed)**

```go
// append to internal/runtime/housekeeping_test.go
func TestCleanupCommandBuilders(t *testing.T) {
	cmd := containerListCmd()
	if len(cmd) == 0 || cmd[0] != "ps" {
		t.Fatalf("containerListCmd() = %v", cmd)
	}
	img := imageListCmd()
	if len(img) == 0 || img[0] != "images" {
		t.Fatalf("imageListCmd() = %v", img)
	}
	if flags := networkPruneCmdFlags(); len(flags) == 0 {
		t.Fatal("networkPruneCmdFlags() empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestCleanupCommandBuilders -v -count=1`

Expected: FAIL with `undefined: containerListCmd`

- [ ] **Step 3: Implement the exec methods (append to `internal/runtime/housekeeping.go`)**

```go
import (
	"os/exec"
	"sort"
	"strings"
)
```

Note: the code below uses `fmt.Errorf`, `log.Printf`, `strings.Fields`, `sort.SliceStable`, `os/exec`. `context` (Task 1) and `strings` (Task 2) are already imported; this task adds `os/exec` and `sort`; `fmt` and `log` must also be added to `housekeeping.go`'s imports. Consolidate with `goimports` before committing.

```go
func containerListCmd() []string {
	return []string{"ps", "-a", "--format", "{{.ID}}|{{.Label \"tengiz-app\"}}"}
}

func imageListCmd() []string {
	return []string{"images", "--format", "{{.Repository}}|{{.Tag}}|{{.ID}}|{{.CreatedAt}}"}
}

func networkPruneCmdFlags() []string {
	return []string{"network", "ls", "--format", "{{.ID}}|{{.Name}}"}
}

var defaultNetworks = map[string]bool{"bridge": true, "host": true, "none": true}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	res := &CleanupResult{}
	if opts.Containers {
		n, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return nil, fmt.Errorf("cleanup containers: %w", err)
		}
		res.Containers = n
	}
	if opts.Images {
		n, err := r.pruneImages(ctx, opts.Apps, opts.Keep, opts.DryRun)
		if err != nil {
			return nil, fmt.Errorf("cleanup images: %w", err)
		}
		res.Images = n
	}
	if opts.Volumes {
		n, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return nil, fmt.Errorf("cleanup volumes: %w", err)
		}
		res.Volumes = n
	}
	if opts.Networks {
		n, err := r.pruneNetworks(ctx, opts.DryRun)
		if err != nil {
			return nil, fmt.Errorf("cleanup networks: %w", err)
		}
		res.Networks = n
	}
	if opts.BuildCache {
		n, err := r.pruneBuildCache(ctx, opts.DryRun)
		if err != nil {
			return nil, fmt.Errorf("cleanup build cache: %w", err)
		}
		res.BuildCache = n
	}
	return res, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", containerListCmd()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	ids := parseForeignContainers(string(out))
	if dryRun {
		return len(ids), nil
	}
	removed := 0
	for _, id := range ids {
		rm := exec.CommandContext(ctx, "docker", "rm", "-f", id)
		if o, err := rm.CombinedOutput(); err != nil {
			log.Printf("[runtime] cleanup: cannot remove container %s: %v", id, err)
			_ = o
			continue
		}
		removed++
	}
	return removed, nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, apps []string, keep int, dryRun bool) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", imageListCmd()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	targets := selectImagesRemove(parseImageRows(string(out)), apps, keep)
	if dryRun {
		return len(targets), nil
	}
	removed := 0
	for _, t := range targets {
		if err := r.RemoveImage(ctx, t); err != nil {
			log.Printf("[runtime] cleanup: cannot remove image %s: %v", t, err)
			continue
		}
		removed++
	}
	return removed, nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) (int, error) {
	// Only unused (dangling) volumes are removed — volumes attached to any
	// container (running or stopped) are excluded by --filter dangling=true.
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	names := strings.Fields(string(out))
	if dryRun {
		return len(names), nil
	}
	removed := 0
	for _, n := range names {
		rm := exec.CommandContext(ctx, "docker", "volume", "rm", n)
		if o, err := rm.CombinedOutput(); err != nil {
			log.Printf("[runtime] cleanup: cannot remove volume %s: %v", n, err)
			_ = o
			continue
		}
		removed++
	}
	return removed, nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", networkPruneCmdFlags()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	rows := parseIDNameRows(string(out))
	candidates := make([]string, 0)
	for _, n := range rows {
		if !defaultNetworks[n.Name] {
			candidates = append(candidates, n.ID)
		}
	}
	if dryRun {
		return len(candidates), nil
	}
	removed := 0
	for _, id := range candidates {
		rm := exec.CommandContext(ctx, "docker", "network", "rm", id)
		if o, err := rm.CombinedOutput(); err != nil {
			log.Printf("[runtime] cleanup: cannot remove network %s: %v", id, err)
			_ = o
			continue
		}
		removed++
	}
	return removed, nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) (int, error) {
	if dryRun {
		return 1, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return 1, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestCleanupCommandBuilders -v -count=1`

Expected: PASS

Run: `go vet ./internal/runtime/`

Expected: No issues

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat(runtime): implement dockerRuntime.Cleanup with dry-run support"
```

---

### Task 5: Add `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.NewDocker()`, `config.NewStoreWithEnv`, `getEnv(cmd)`, `dataDir`
- Produces: `cleanupCmd` registered on `rootCmd`, `buildCleanupOptions(cmd *cobra.Command) runtime.CleanupOptions`, `confirmCleanup() bool`, `formatCleanupReport(res *runtime.CleanupResult, opts runtime.CleanupOptions) string`

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

func TestBuildCleanupOptionsDefaultsAll(t *testing.T) {
	c := newCleanupTestCommand()
	opts := buildCleanupOptions(c)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Fatalf("default options = %+v, want all five categories enabled", opts)
	}
	if opts.Keep != 5 {
		t.Fatalf("default Keep = %d, want 5", opts.Keep)
	}
}

func TestBuildCleanupOptionsFlags(t *testing.T) {
	c := newCleanupTestCommand()
	c.ParseFlags([]string{"--containers", "--images", "--dry-run", "--keep", "3"})
	opts := buildCleanupOptions(c)
	if !opts.Containers || !opts.Images {
		t.Fatalf("opts = %+v, want Containers+Images enabled", opts)
	}
	if opts.Volumes || opts.Networks || opts.BuildCache {
		t.Fatalf("opts = %+v, want Volumes/Networks/BuildCache disabled", opts)
	}
	if !opts.DryRun {
		t.Error("DryRun = false, want true")
	}
	if opts.Keep != 3 {
		t.Errorf("Keep = %d, want 3", opts.Keep)
	}
}

func TestFormatCleanupReport(t *testing.T) {
	res := &runtime.CleanupResult{Containers: 2, Images: 3, Volumes: 1, Networks: 1, BuildCache: 1}
	opts := runtime.CleanupOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true, Keep: 5}
	report := formatCleanupReport(res, opts)
	for _, want := range []string{"containers: 2", "images: 3", "volumes: 1", "networks: 1", "build cache: 1"} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
}

func TestFormatCleanupReportDryRun(t *testing.T) {
	res := &runtime.CleanupResult{Containers: 1}
	opts := runtime.CleanupOptions{Containers: true, DryRun: true}
	report := formatCleanupReport(res, opts)
	if !strings.Contains(report, "(dry-run)") {
		t.Errorf("dry-run report missing header marker:\n%s", report)
	}
	if !strings.Contains(report, "containers: 1") {
		t.Errorf("dry-run report missing category line:\n%s", report)
	}
}

func newCleanupTestCommand() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Bool("all", false, "")
	c.Flags().Bool("containers", false, "")
	c.Flags().Bool("images", false, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("build-cache", false, "")
	c.Flags().Bool("dry-run", false, "")
	c.Flags().Int("keep", 5, "")
	c.Flags().BoolP("yes", "y", false, "")
	return c
}
```

Add import `"strings"` to the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestBuildCleanupOptions|TestFormatCleanupReport' -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` / `undefined: buildCleanupOptions`

- [ ] **Step 3: Implement `internal/cli/cleanup.go`**

```go
package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

func init() {
	cleanupCmd.Flags().Bool("all", false, "run all cleanup categories")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling and old app images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks (excluding docker defaults)")
	cleanupCmd.Flags().Bool("build-cache", false, "prune docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be pruned without removing anything")
	cleanupCmd.Flags().Int("keep", 5, "number of deployment images to keep per app")
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	rootCmd.AddCommand(cleanupCmd)
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources to reclaim disk space",
	Long: `Prunes unused Docker resources. Tengiz-managed containers (labeled
tengiz-app=...) and active image tags are never removed.

Categories (default: all when no category flag is given):
  --containers    stopped containers not managed by Tengiz
  --images        dangling images + old deployment images beyond --keep per app
  --volumes       unused volumes
  --networks      unused networks (docker bridge/host/none excluded)
  --build-cache   docker builder cache

Use --dry-run to preview, or -y to skip the confirmation.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := buildCleanupOptions(cmd)
		env := getEnv(cmd)

		if opts.Images {
			store := config.NewStoreWithEnv(dataDir, env)
			apps, err := store.ListApps()
			if err == nil {
				for _, a := range apps {
					opts.Apps = append(opts.Apps, a.Name)
				}
			}
		}

		if !opts.DryRun {
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes && !confirmCleanup() {
				fmt.Println("[tengiz] cleanup cancelled.")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		res, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}

		fmt.Print(formatCleanupReport(res, opts))
		return nil
	},
}

func buildCleanupOptions(cmd *cobra.Command) runtime.CleanupOptions {
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	keep, _ := cmd.Flags().GetInt("keep")
	if keep <= 0 {
		keep = 5
	}
	if all || !(containers || images || volumes || networks || buildCache) {
		containers, images, volumes, networks, buildCache = true, true, true, true, true
	}
	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		DryRun:     dryRun,
		Keep:       keep,
	}
}

func confirmCleanup() bool {
	fmt.Print("[tengiz] Remove unused Docker resources? Only Tengiz-managed containers are protected. [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func formatCleanupReport(res *runtime.CleanupResult, opts runtime.CleanupOptions) string {
	verb := "pruned"
	if opts.DryRun {
		verb = "would be pruned"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[tengiz] cleanup (%s):\n", verb))
	if opts.Containers {
		fmt.Fprintf(&b, "  containers: %d\n", res.Containers)
	}
	if opts.Images {
		fmt.Fprintf(&b, "  images: %d\n", res.Images)
	}
	if opts.Volumes {
		fmt.Fprintf(&b, "  volumes: %d\n", res.Volumes)
	}
	if opts.Networks {
		fmt.Fprintf(&b, "  networks: %d\n", res.Networks)
	}
	if opts.BuildCache {
		fmt.Fprintf(&b, "  build cache: %d\n", res.BuildCache)
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestBuildCleanupOptions|TestFormatCleanupReport' -v -count=1`

Expected: PASS

- [ ] **Step 5: Run the full CLI test suite**

Run: `go test ./internal/cli/ -count=1`

Expected: PASS

- [ ] **Step 6: Compile the binary and smoke-check help**

```bash
go build -o tengiz .
./tengiz cleanup --help
```

Expected: help text listing `--all --containers --images --volumes --networks --build-cache --dry-run --keep --yes`

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 6: Documentation, feature-flag update, and final verification

**Files:**
- Modify: `README.md` (CLI Reference section, after the `tengiz volume list <app>` section at line 296)
- Modify: `docs/FUTURES_FEATURES.md` (table row #6 at line 19 + Implemented section)
- Verify: full test suite + vet

- [ ] **Step 1: Add `tengiz cleanup` to README CLI Reference**

Insert after the `tengiz volume list <app>` section (around line 302):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space. Tengiz-managed containers (labeled `tengiz-app=...`) and active image tags are always protected.

```bash
tengiz cleanup                # prune all categories
tengiz cleanup --dry-run      # preview without removing
tengiz cleanup --images --keep 3
tengiz cleanup --containers --volumes -y
```

Flags: `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--all`, `--keep N` (images kept per app, default 5), `--dry-run`, `-y/--yes`.

When no category flag is given, all five categories run. A confirmation prompt appears unless `-y` or `--dry-run` is used.
```

- [ ] **Step 2: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

In the Priority Ranking P0 table, change row #6 from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

And in the Implemented Features table, add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-11) |
```

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -count=1`

Expected: PASS (accept the known-slow proxy TCP-dial tests and time-sensitive idle tests)

- [ ] **Step 4: Run go vet**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 5: Self-review against the spec**

Checklist:
- `tengiz cleanup` command ✅ (Task 5)
- Label-based protection of Tengiz containers (`tengiz-app` label) ✅ (Task 4 `parseForeignContainers`)
- Image retention keeps newest `--keep` per app, dangling images pruned ✅ (Task 2 `selectImagesRemove`)
- `-latest` tags preserved (env-aware bugfix) ✅ (Task 3)
- Volumes, networks (excluding defaults), build cache categories ✅ (Task 4)
- `--dry-run` shows counts without removing ✅ (Task 4 + Task 5)
- Confirmation prompt with `-y` skip ✅ (Task 5)
- Env-aware app discovery (env-scoped store) ✅ (Task 5)
- No new external dependencies ✅ (all `os/exec` + stdlib)

- [ ] **Step 6: Placeholder scan**

Search plan for `TBD|TODO|implement later|Similar to Task`. All steps contain complete code. (Verified: every code step above has full implementations.)

- [ ] **Step 7: Type consistency check**

- `Cleanup(ctx, opts CleanupOptions) (*CleanupResult, error)` — same signature in Task 1 (interface/stub/mocks), Task 4 (dockerRuntime), Task 5 (CLI call). Checked.
- `selectImagesRemove(rows, apps, keep)` and `selectKeepImages(rows, appName, n)` — Task 2 defines, Task 3 refactor consumes, Task 4 `pruneImages` consumes. Checked.
- `Keep` field default handled at CLI (`buildCleanupOptions`) and inside selectors (`keep <= 0` guard). Checked.

- [ ] **Step 8: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```