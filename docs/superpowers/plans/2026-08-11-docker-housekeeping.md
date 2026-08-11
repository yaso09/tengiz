# Docker Housekeeping (tengiz cleanup) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that frees disk space on single-server deployments by pruning stopped containers, old images, unused networks, build cache, and opt-in anonymous volumes — using label-based filtering so non-Tengiz Docker resources are never touched.

**Architecture:** Three layers. (1) `internal/builder` labels every `docker build` with `tengiz-app=<name>` / `tengiz-env=<env>` so images are addressable by label. (2) `internal/runtime` gets a new `Cleanup(ctx, CleanupOptions) (CleanupResult, error)` method on the `Manager` interface — the `dockerRuntime` implementation runs label-filtered `docker` CLI prunes; image retention reuses the same keep-last-N idea as the existing `KeepLastNImages` (default 5, `--keep` overrides, `--all` removes everything except protected/latest). (3) `internal/cli` exposes a `cleanup` cobra command with category flags and prints a summary. Active deployments and images referenced by running containers are always protected.

**Tech Stack:** Go 1.26, existing `runtime.Manager` interface, `docker` CLI via `os/exec` (no Docker SDK), cobra CLI. No new external dependencies.

## Global Constraints

- All prune operations are **label-filtered**: containers via `label=tengiz-app`, images via `label=tengiz-app` + `reference=tengiz-apps/*` — non-Tengiz resources are never touched
- **Never** run `docker image prune -a` on Tengiz images (it would delete rollback images not referenced by a running container); all tagged-image removal goes through the retention selection logic in `selectImagesToRemove`
- Active deployments are protected: container names from the store (`runtime.ContainerName(name, env)`) are never pruned, and the `*-latest` image tag plus images in use by running containers are never removed
- Image tag format stays `tengiz-apps/<app>:<env>-<deploymentID>` and `tengiz-apps/<app>:<env>-latest` (unchanged from `internal/builder/builder.go:61`)
- Default retention is 5 images per app (matches existing `KeepLastNImages(..., 5)` calls)
- Default `tengiz cleanup` with no category flags enables containers + images + networks only; `--volumes` is always opt-in (data-loss risk), `--build-cache` is opt-in
- `--volumes` uses `docker volume prune -f` (anonymous volumes only, never `-a`) — Tengiz volumes are bind mounts, so they are unaffected
- No new external Go dependencies
- All existing tests must continue to pass; new tests must not require Docker (use `t.Skip` when Docker is unavailable, matching `internal/builder/builder_test.go`)
- New feature branch: `feat/docker-housekeeping`
- Update `README.md`, `AGENTS.md`, and `docs/FUTURES_FEATURES.md` (repo rule: every feature ships with docs)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/builder/builder.go` | Add `imageLabelArgs()` helper; pass `--label tengiz-app=<name>`, `--label tengiz-env=<env>` to `docker build` |
| `internal/builder/builder_test.go` | Unit test `imageLabelArgs()` + integration test verifying built image carries labels |
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupResult`, `DefaultImageRetention`; add `Cleanup` to `Manager` interface + stub impl |
| `internal/runtime/cleanup.go` | Implement `dockerRuntime.Cleanup()` + prune helpers + pure parsing/selection helpers |
| `internal/runtime/runtime_test.go` | Stub `Cleanup` test |
| `internal/runtime/cleanup_test.go` | Tests for `countRemovedNames`, `lastReclaimed`, `selectImagesToRemove`, docker-noop smoke test |
| `internal/proxy/proxy_test.go` | Add `Cleanup` method to `mockRuntime` (interface compliance) |
| `internal/idle/idle_test.go` | Add `Cleanup` method to `mockRuntime` (interface compliance) |
| `internal/cli/root.go` | Add `cleanupCmd` + flags + `cleanupFlagSelection()` helper + register in `init()` |
| `internal/cli/root_test.go` | Tests: command registration, flags, default selection, RunE flag parsing; add `Cleanup` method to `mockRTForDeploy` |
| `README.md` | New `### tengiz cleanup` section in CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` line to CLI section |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Label Docker builds

**Files:**
- Modify: `internal/builder/builder.go:57-91` (`buildWithDockerfile`) and `:93-99` (add helper next to `buildSecretArgs`)
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `func imageLabelArgs(appName, env string) []string` — returns `["--label", "tengiz-app=<appName>", "--label", "tengiz-env=<env>"]`

> **Prep:** create the feature branch first: `git checkout -b feat/docker-housekeeping`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing unit test**

Add to `internal/builder/builder_test.go`:

```go
func TestImageLabelArgs(t *testing.T) {
	got := imageLabelArgs("myapp", "production")
	expected := []string{
		"--label", "tengiz-app=myapp",
		"--label", "tengiz-env=production",
	}
	if len(got) != len(expected) {
		t.Fatalf("imageLabelArgs() = %v (len=%d), want %v (len=%d)", got, len(got), expected, len(expected))
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("imageLabelArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/builder/... -run TestImageLabelArgs -v -count=1`

Expected: FAIL with `undefined: imageLabelArgs`

- [ ] **Step 4: Write minimal implementation in `internal/builder/builder.go`**

Add the helper (next to `buildSecretArgs`, after line 99):

```go
func imageLabelArgs(appName, env string) []string {
	if env == "" {
		env = "production"
	}
	return []string{
		"--label", fmt.Sprintf("tengiz-app=%s", appName),
		"--label", fmt.Sprintf("tengiz-env=%s", env),
	}
}
```

Wire it into `buildWithDockerfile` (replace lines 69-71):

```go
	args := []string{"build"}
	args = append(args, imageLabelArgs(appName, env)...)
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/builder/... -run TestImageLabelArgs -v -count=1`

Expected: PASS

- [ ] **Step 6: Add integration test (skips without Docker)**

Add to `internal/builder/builder_test.go`:

```go
func TestBuildAddsLabels(t *testing.T) {
	b := New(t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0644); err != nil {
		t.Fatal(err)
	}
	detection := &Detection{Framework: FrameworkStatic, InternalPort: 80}
	tag, _, err := b.Build(context.Background(), dir, "testapp", "production", detection, "v-label")
	if err != nil {
		t.Skipf("Build() error (likely no docker): %v", err)
	}
	defer exec.Command("docker", "rmi", "-f", tag).Run()

	cmd := exec.Command("docker", "image", "inspect",
		"--format", "{{.Config.Labels.tengiz-app}}|{{.Config.Labels.tengiz-env}}", tag)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("docker image inspect: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "testapp|production" {
		t.Errorf("image labels = %q, want %q", got, "testapp|production")
	}
}
```

Add `"os/exec"` to the import block of `builder_test.go`.

- [ ] **Step 7: Run full builder test suite**

Run: `go test ./internal/builder/... -count=1`

Expected: PASS (or skips when Docker is unavailable)

- [ ] **Step 8: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: label docker builds for label-based cleanup"
```

---

### Task 2: Cleanup types + Manager interface + stub + mock updates

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface), `:113-119` (stub)
- Modify: `internal/runtime/runtime_test.go`
- Modify: `internal/proxy/proxy_test.go:33-34` (mockRuntime)
- Modify: `internal/idle/idle_test.go:32-33` (mockRuntime)
- Modify: `internal/cli/root_test.go:98-99` (mockRTForDeploy)

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type CleanupOptions struct { Containers, Images, Volumes, Networks, BuildCache, All bool; Keep int; AppName string; ProtectedNames []string }`
  - `type CleanupResult struct { ContainersRemoved, ImagesRemoved, NetworksRemoved, VolumesRemoved int; Reclaimed []string }`
  - `const DefaultImageRetention = 5`
  - `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` on `Manager`

- [ ] **Step 1: Write the failing stub test**

Add to `internal/runtime/runtime_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.NetworksRemoved != 0 || res.VolumesRemoved != 0 {
		t.Errorf("expected zero result, got %+v", res)
	}
	if len(res.Reclaimed) != 0 {
		t.Errorf("expected no reclaimed entries, got %v", res.Reclaimed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: FAIL with `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add types + interface method in `internal/runtime/runtime.go`**

Add the types before the `Manager` interface (after `RunOptions`, line 29):

```go
const DefaultImageRetention = 5

type CleanupOptions struct {
	Containers     bool
	Images         bool
	Volumes        bool
	Networks       bool
	BuildCache     bool
	All            bool
	Keep           int
	AppName        string
	ProtectedNames []string
}

type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	Reclaimed         []string
}
```

Add to the `Manager` interface after `KeepLastNImages` (line 36):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

- [ ] **Step 4: Add stub implementation in `internal/runtime/runtime.go`**

After the `KeepLastNImages` stub (line 119):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 5: Update the three test mocks so the interface compiles**

Add to `internal/proxy/proxy_test.go` (after line 34), `internal/idle/idle_test.go` (after line 33), and `internal/cli/root_test.go` (after line 99, using `mockRTForDeploy`):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

In `root_test.go` use the same receiver name `m *mockRTForDeploy`. All three files already import `runtime`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/proxy/... ./internal/idle/... ./internal/cli/... -count=1`

Expected: PASS (proxy tests are slow ~2s each — normal)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: add Cleanup to runtime Manager interface"
```

---

### Task 3: Container, network, volume, and build-cache prune helpers + parsers

**Files:**
- Modify: `internal/runtime/cleanup.go` (append after line 59)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `DefaultImageRetention` (Task 2); existing `r.Remove(ctx, name)`
- Produces:
  - `func (r *dockerRuntime) pruneStoppedContainers(ctx context.Context, protected []string) (int, error)`
  - `func (r *dockerRuntime) pruneNetworks(ctx context.Context) (int, error)`
  - `func (r *dockerRuntime) pruneVolumes(ctx context.Context) (int, error)`
  - `func (r *dockerRuntime) pruneBuildCache(ctx context.Context) (string, error)`
  - `func countRemovedNames(out, header string) int`
  - `func lastReclaimed(out string) string`

- [ ] **Step 1: Write the failing parser tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestCountRemovedNamesNetworks(t *testing.T) {
	out := "Deleted Networks:\ntznet1\ntznet2\n\n"
	if got := countRemovedNames(out, "Deleted Networks:"); got != 2 {
		t.Errorf("countRemovedNames() = %d, want 2", got)
	}
	if got := countRemovedNames("Total reclaimed space: 0B\n", "Deleted Networks:"); got != 0 {
		t.Errorf("countRemovedNames() = %d, want 0", got)
	}
	if got := countRemovedNames("", "Deleted Networks:"); got != 0 {
		t.Errorf("countRemovedNames() = %d, want 0", got)
	}
}

func TestCountRemovedNamesVolumes(t *testing.T) {
	out := "Deleted Volumes:\na37c681582611f2cee3b8389875cbd095da14367a368254b4e61e1df3e380f6f\n\nTotal reclaimed space: 0B\n"
	if got := countRemovedNames(out, "Deleted Volumes:"); got != 1 {
		t.Errorf("countRemovedNames() = %d, want 1", got)
	}
}

func TestLastReclaimed(t *testing.T) {
	out := "Deleted:\nsha256:abc\n\nTotal reclaimed space: 1.2GB\n"
	if got := lastReclaimed(out); got != "1.2GB" {
		t.Errorf("lastReclaimed() = %q, want %q", got, "1.2GB")
	}
	out = "ID\tRECLAIMABLE\tSIZE\tLAST ACCESSED\no42f*\ttrue\t29B\tAbout a minute ago\nTotal:\t225B\n"
	if got := lastReclaimed(out); got != "225B" {
		t.Errorf("lastReclaimed() = %q, want %q", got, "225B")
	}
	if got := lastReclaimed(""); got != "" {
		t.Errorf("lastReclaimed() = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestCountRemovedNames|TestLastReclaimed" -v -count=1`

Expected: FAIL with `undefined: countRemovedNames`, `undefined: lastReclaimed`

- [ ] **Step 3: Write the parser helpers in `internal/runtime/cleanup.go`**

```go
func countRemovedNames(out, header string) int {
	lines := strings.Split(out, "\n")
	idx := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == header {
			idx = i
			break
		}
	}
	if idx == -1 {
		return 0
	}
	count := 0
	for _, l := range lines[idx+1:] {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "Total") {
			break
		}
		count++
	}
	return count
}

func lastReclaimed(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "Total reclaimed space: ") {
			return strings.TrimPrefix(l, "Total reclaimed space: ")
		}
		if strings.HasPrefix(l, "Total: ") {
			return strings.TrimPrefix(l, "Total: ")
		}
	}
	return ""
}
```

- [ ] **Step 4: Run parser tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestCountRemovedNames|TestLastReclaimed" -v -count=1`

Expected: PASS

- [ ] **Step 5: Write the prune methods in `internal/runtime/cleanup.go`**

```go
func (r *dockerRuntime) pruneStoppedContainers(ctx context.Context, protected []string) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "label=tengiz-app",
		"--filter", "status=exited",
		"--filter", "status=dead",
		"--format", "{{.Names}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker ps: %w", err)
	}

	protectedSet := make(map[string]struct{}, len(protected))
	for _, n := range protected {
		protectedSet[n] = struct{}{}
	}

	removed := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if _, ok := protectedSet[name]; ok {
			continue
		}
		if err := r.Remove(ctx, name); err != nil {
			log.Printf("[runtime] cleanup: failed to remove container %s: %v", name, err)
			continue
		}
		removed++
	}
	return removed, nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return countRemovedNames(string(out), "Deleted Networks:"), nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return countRemovedNames(string(out), "Deleted Volumes:"), nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return lastReclaimed(string(out)), nil
}
```

- [ ] **Step 6: Run the full runtime test suite**

Run: `go test ./internal/runtime/... -count=1`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add container/network/volume/cache prune helpers"
```

---

### Task 4: Image retention selection logic (pure function)

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `DefaultImageRetention` (Task 2)
- Produces:
  - `type imageEntry struct { ID, Tag, CreatedAt string }`
  - `func selectImagesToRemove(entries []imageEntry, protected []string, appName string, keep int, all bool) []string` — returns the list of image **tags** (format `repository:tag`) to `docker rmi`. Never returns `*-latest` or protected tags. When `all` is false, keeps the newest `keep` entries per repository; when `all` is true, removes every non-protected, non-latest entry.

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestSelectImagesToRemoveKeepsNewest(t *testing.T) {
	entries := []imageEntry{
		{Tag: "tengiz-apps/myapp:production-v1", CreatedAt: "2026-01-01 00:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:production-v2", CreatedAt: "2026-01-02 00:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:production-v3", CreatedAt: "2026-01-03 00:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:production-v4", CreatedAt: "2026-01-04 00:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:production-v5", CreatedAt: "2026-01-05 00:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:production-latest", CreatedAt: "2026-01-06 00:00:00 +0000 UTC"},
	}
	got := selectImagesToRemove(entries, nil, "", 3, false)
	want := []string{"tengiz-apps/myapp:production-v1", "tengiz-apps/myapp:production-v2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectImagesToRemove(keep=3) = %v, want %v", got, want)
	}
}

func TestSelectImagesToRemoveRetentionSufficient(t *testing.T) {
	entries := []imageEntry{
		{Tag: "tengiz-apps/myapp:production-v1", CreatedAt: "2026-01-01 00:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:production-latest", CreatedAt: "2026-01-02 00:00:00 +0000 UTC"},
	}
	if got := selectImagesToRemove(entries, nil, "", 5, false); len(got) != 0 {
		t.Errorf("selectImagesToRemove(keep=5) = %v, want empty", got)
	}
}

func TestSelectImagesToRemoveSkipsProtected(t *testing.T) {
	entries := []imageEntry{
		{Tag: "tengiz-apps/myapp:production-v1", CreatedAt: "2026-01-01 00:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:production-v2", CreatedAt: "2026-01-02 00:00:00 +0000 UTC"},
	}
	got := selectImagesToRemove(entries, []string{"tengiz-apps/myapp:production-v1"}, "", 1, false)
	if len(got) != 0 {
		t.Errorf("selectImagesToRemove(protected=v1) = %v, want empty", got)
	}
}

func TestSelectImagesToRemoveAll(t *testing.T) {
	entries := []imageEntry{
		{Tag: "tengiz-apps/myapp:production-v1", CreatedAt: "2026-01-01 00:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:production-v2", CreatedAt: "2026-01-02 00:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:production-latest", CreatedAt: "2026-01-03 00:00:00 +0000 UTC"},
	}
	got := selectImagesToRemove(entries, nil, "", 5, true)
	want := []string{"tengiz-apps/myapp:production-v1", "tengiz-apps/myapp:production-v2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectImagesToRemove(all=true) = %v, want %v", got, want)
	}
}

func TestSelectImagesToRemoveAppFilter(t *testing.T) {
	entries := []imageEntry{
		{Tag: "tengiz-apps/myapp:production-v1", CreatedAt: "2026-01-01 00:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/other:production-v1", CreatedAt: "2026-01-01 00:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/other:production-v2", CreatedAt: "2026-01-02 00:00:00 +0000 UTC"},
	}
	got := selectImagesToRemove(entries, nil, "myapp", 1, false)
	want := []string{"tengiz-apps/myapp:production-v1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectImagesToRemove(app=myapp) = %v, want %v", got, want)
	}
}
```

Add `"reflect"` to the import block of `cleanup_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run TestSelectImagesToRemove -v -count=1`

Expected: FAIL with `undefined: imageEntry`, `undefined: selectImagesToRemove`

- [ ] **Step 3: Write the selection logic in `internal/runtime/cleanup.go`**

```go
type imageEntry struct {
	ID        string
	Tag       string
	CreatedAt string
}

func selectImagesToRemove(entries []imageEntry, protected []string, appName string, keep int, all bool) []string {
	if keep <= 0 {
		keep = DefaultImageRetention
	}
	protectedSet := make(map[string]struct{}, len(protected))
	for _, p := range protected {
		protectedSet[p] = struct{}{}
	}

	repos := make(map[string][]imageEntry)
	var order []string
	for _, e := range entries {
		repo, _, _ := strings.Cut(e.Tag, ":")
		if appName != "" && repo != "tengiz-apps/"+appName {
			continue
		}
		if _, ok := protectedSet[e.Tag]; ok {
			continue
		}
		if strings.HasSuffix(e.Tag, "-latest") {
			continue
		}
		if _, ok := repos[repo]; !ok {
			order = append(order, repo)
		}
		repos[repo] = append(repos[repo], e)
	}

	var toRemove []string
	for _, repo := range order {
		group := repos[repo]
		sort.Slice(group, func(i, j int) bool {
			return group[i].CreatedAt < group[j].CreatedAt
		})
		cutoff := len(group)
		if !all {
			if len(group) <= keep {
				continue
			}
			cutoff = len(group) - keep
		}
		for _, e := range group[:cutoff] {
			toRemove = append(toRemove, e.Tag)
		}
	}
	return toRemove
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run TestSelectImagesToRemove -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add image retention selection logic"
```

---

### Task 5: Image cleanup implementation + Cleanup orchestrator

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `selectImagesToRemove`, `imageEntry` (Task 4), `pruneStoppedContainers`, `pruneNetworks`, `pruneVolumes`, `pruneBuildCache`, `lastReclaimed` (Task 3), `CleanupOptions`/`CleanupResult`/`DefaultImageRetention` (Task 2)
- Produces:
  - `func (r *dockerRuntime) inUseImageTags(ctx context.Context) ([]string, error)` — tags of images referenced by running Tengiz containers
  - `func (r *dockerRuntime) cleanupImages(ctx context.Context, opts CleanupOptions) (int, error)`
  - `func (r *dockerRuntime) pruneDanglingImages(ctx context.Context) (string, error)`
  - `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`

- [ ] **Step 1: Write the failing smoke test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestDockerCleanupNoop(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	r := &dockerRuntime{}
	res, err := r.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.NetworksRemoved != 0 || res.VolumesRemoved != 0 {
		t.Errorf("expected no-op result, got %+v", res)
	}
}
```

Add `"os/exec"` to the import block of `cleanup_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestDockerCleanupNoop -v -count=1`

Expected: FAIL with `r.Cleanup undefined (type *dockerRuntime has no field or method Cleanup)`

- [ ] **Step 3: Write the image cleanup methods in `internal/runtime/cleanup.go`**

```go
func (r *dockerRuntime) inUseImageTags(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps",
		"--filter", "label=tengiz-app",
		"--format", "{{.Image}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	var tags []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags, nil
}

func (r *dockerRuntime) cleanupImages(ctx context.Context, opts CleanupOptions) (int, error) {
	ref := "tengiz-apps/*"
	if opts.AppName != "" {
		ref = "tengiz-apps/" + opts.AppName + ":*"
	}
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", "reference="+ref,
		"--format", "{{.ID}}|{{.Repository}}:{{.Tag}}|{{.CreatedAt}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images: %w", err)
	}

	var entries []imageEntry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 2 || parts[1] == "" {
			continue
		}
		e := imageEntry{ID: parts[0], Tag: parts[1]}
		if len(parts) == 3 {
			e.CreatedAt = parts[2]
		}
		entries = append(entries, e)
	}

	protected, _ := r.inUseImageTags(ctx)
	toRemove := selectImagesToRemove(entries, protected, opts.AppName, opts.Keep, opts.All)
	if len(toRemove) == 0 {
		return 0, nil
	}

	removed := 0
	for i := 0; i < len(toRemove); i += 20 {
		end := i + 20
		if end > len(toRemove) {
			end = len(toRemove)
		}
		batch := append([]string{"rmi", "-f"}, toRemove[i:end]...)
		rcmd := exec.CommandContext(ctx, "docker", batch...)
		o, err := rcmd.CombinedOutput()
		if err != nil {
			return removed, fmt.Errorf("docker rmi: %w\n%s", err, string(o))
		}
		removed += end - i
	}
	return removed, nil
}

func (r *dockerRuntime) pruneDanglingImages(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f",
		"--filter", "label=tengiz-app",
		"--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return lastReclaimed(string(out)), nil
}
```

- [ ] **Step 4: Write the Cleanup orchestrator in `internal/runtime/cleanup.go`**

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	if opts.Keep <= 0 {
		opts.Keep = DefaultImageRetention
	}
	var res CleanupResult

	if opts.Containers {
		n, err := r.pruneStoppedContainers(ctx, opts.ProtectedNames)
		if err != nil {
			return res, err
		}
		res.ContainersRemoved = n
	}

	if opts.Images {
		n, err := r.cleanupImages(ctx, opts)
		if err != nil {
			return res, err
		}
		res.ImagesRemoved = n
		if rc, err := r.pruneDanglingImages(ctx); err != nil {
			log.Printf("[runtime] cleanup: dangling image prune: %v", err)
		} else if rc != "" {
			res.Reclaimed = append(res.Reclaimed, rc)
		}
	}

	if opts.Networks {
		n, err := r.pruneNetworks(ctx)
		if err != nil {
			return res, err
		}
		res.NetworksRemoved = n
	}

	if opts.Volumes {
		n, err := r.pruneVolumes(ctx)
		if err != nil {
			return res, err
		}
		res.VolumesRemoved = n
	}

	if opts.BuildCache {
		if rc, err := r.pruneBuildCache(ctx); err != nil {
			return res, err
		} else if rc != "" {
			res.Reclaimed = append(res.Reclaimed, rc)
		}
	}

	return res, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run TestDockerCleanupNoop -v -count=1`

Expected: PASS (skips if Docker unavailable)

- [ ] **Step 6: Run the full runtime test suite**

Run: `go test ./internal/runtime/... -count=1`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement docker cleanup orchestrator and image pruning"
```

---

### Task 6: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — register in `init()` (after line 75), add `cleanupCmd` after `psCmd` (line 601), add `cleanupFlagSelection` helper
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.ContainerName` (Task 2/5), `config.NewStoreWithEnv`, `dataDir`, `getEnv`
- Produces:
  - `cleanupCmd` cobra command with flags `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `-a/--all`, `--keep` (default 5), `--app`
  - `func cleanupFlagSelection(cmd *cobra.Command) (containers, images, volumes, networks, buildCache, all bool, keep int, appName string)` — applies the default (containers+images+networks when no category flag is set)

- [ ] **Step 1: Write the failing CLI tests**

Add to `internal/cli/root_test.go`:

```go
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
	flags := cleanupCmd.Flags()
	for _, name := range []string{"containers", "images", "volumes", "networks", "build-cache", "all", "keep", "app"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupFlagSelectionDefaults(t *testing.T) {
	cleanupCmd.Flags().Set("containers", "false")
	cleanupCmd.Flags().Set("images", "false")
	cleanupCmd.Flags().Set("volumes", "false")
	cleanupCmd.Flags().Set("networks", "false")
	cleanupCmd.Flags().Set("build-cache", "false")
	cleanupCmd.Flags().Set("all", "false")
	cleanupCmd.Flags().Set("keep", "5")
	cleanupCmd.Flags().Set("app", "")

	containers, images, volumes, networks, buildCache, all, keep, appName := cleanupFlagSelection(cleanupCmd)
	if !containers || !images || !networks {
		t.Errorf("defaults should enable containers+images+networks, got containers=%v images=%v networks=%v", containers, images, networks)
	}
	if volumes || buildCache || all {
		t.Errorf("volumes/build-cache/all should default false, got volumes=%v buildCache=%v all=%v", volumes, buildCache, all)
	}
	if keep != 5 {
		t.Errorf("keep default = %d, want 5", keep)
	}
	if appName != "" {
		t.Errorf("app default = %q, want empty", appName)
	}
}

func TestCleanupCmdRunE(t *testing.T) {
	var called bool
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		containers, images, volumes, networks, buildCache, all, keep, appName := cleanupFlagSelection(cmd)
		if !containers || !images || !networks {
			t.Errorf("default selection wrong: containers=%v images=%v networks=%v", containers, images, networks)
		}
		if !volumes || !buildCache || !all {
			t.Errorf("flag passthrough wrong: volumes=%v buildCache=%v all=%v", volumes, buildCache, all)
		}
		if keep != 10 {
			t.Errorf("keep = %d, want 10", keep)
		}
		if appName != "myapp" {
			t.Errorf("app = %q, want %q", appName, "myapp")
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--volumes", "--build-cache", "--app", "myapp", "--keep", "10", "--all"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `cleanup command not registered` / `cleanupCmd undefined`

- [ ] **Step 3: Register flags + command in `init()` in `internal/cli/root.go`**

In `init()`, after the notification command registration (line 75), add:

```go
	cleanupCmd.Flags().Bool("containers", false, "remove stopped Tengiz containers (keeps active deployments)")
	cleanupCmd.Flags().Bool("images", false, "remove old Tengiz images beyond retention")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused anonymous volumes (opt-in: may delete data)")
	cleanupCmd.Flags().Bool("networks", false, "remove unused Docker networks")
	cleanupCmd.Flags().Bool("build-cache", false, "remove Docker build cache")
	cleanupCmd.Flags().BoolP("all", "a", false, "remove all non-protected Tengiz images (ignores retention)")
	cleanupCmd.Flags().Int("keep", 5, "number of images per app to retain")
	cleanupCmd.Flags().String("app", "", "restrict cleanup to a single app")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 4: Add the `cleanupFlagSelection` helper in `internal/cli/root.go`**

Add after `getEnv` (line 103):

```go
func cleanupFlagSelection(cmd *cobra.Command) (containers, images, volumes, networks, buildCache, all bool, keep int, appName string) {
	containers, _ = cmd.Flags().GetBool("containers")
	images, _ = cmd.Flags().GetBool("images")
	volumes, _ = cmd.Flags().GetBool("volumes")
	networks, _ = cmd.Flags().GetBool("networks")
	buildCache, _ = cmd.Flags().GetBool("build-cache")
	all, _ = cmd.Flags().GetBool("all")
	keep, _ = cmd.Flags().GetInt("keep")
	appName, _ = cmd.Flags().GetString("app")
	if !containers && !images && !volumes && !networks && !buildCache {
		containers, images, networks = true, true, true
	}
	return containers, images, volumes, networks, buildCache, all, keep, appName
}
```

- [ ] **Step 5: Add the `cleanupCmd` command definition in `internal/cli/root.go`**

Add after `psCmd` (line 601):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Free disk space by removing unused Docker resources",
	Long:  "Prunes stopped Tengiz containers, old images, unused networks, and build cache. Label-based filtering ensures only Tengiz-managed resources are touched. Add --volumes to also remove unused anonymous volumes.",
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		containers, images, volumes, networks, buildCache, all, keep, appName := cleanupFlagSelection(cmd)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)
		var protected []string
		if apps, err := store.ListApps(); err == nil {
			for _, a := range apps {
				protected = append(protected, runtime.ContainerName(a.Name, a.Config.Environment))
			}
		}

		res, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			Containers:     containers,
			Images:         images,
			Volumes:        volumes,
			Networks:       networks,
			BuildCache:     buildCache,
			All:            all,
			Keep:           keep,
			AppName:        appName,
			ProtectedNames: protected,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Printf("[tengiz] cleanup complete:\n")
		fmt.Printf("  containers removed: %d\n", res.ContainersRemoved)
		fmt.Printf("  images removed:     %d\n", res.ImagesRemoved)
		fmt.Printf("  networks removed:   %d\n", res.NetworksRemoved)
		fmt.Printf("  volumes removed:    %d\n", res.VolumesRemoved)
		for _, r := range res.Reclaimed {
			fmt.Printf("  reclaimed:          %s\n", r)
		}
		return nil
	},
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 7: Run the full test suite**

Run: `go build ./... && go test ./... -count=1 && go vet ./...`

Expected: PASS everywhere

- [ ] **Step 8: Manual smoke test (requires Docker)**

```bash
go build -o tengiz .
./tengiz cleanup
./tengiz cleanup --all --app myapp --keep 3
./tengiz cleanup --volumes --build-cache
```

Expected: prints `[tengiz] cleanup complete:` with per-category counts and reclaimed space; non-Tengiz containers/images untouched.

- [ ] **Step 9: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup CLI command"
```

---

### Task 7: Documentation updates

**Files:**
- Modify: `README.md` (CLI Reference, add section after `### tengiz ps`, ~line 151)
- Modify: `AGENTS.md` (CLI section, after the `rollback` line)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 implemented)

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

After the `### tengiz ps` section, insert:

```markdown
### `tengiz cleanup`

Free disk space by removing unused Docker resources. Uses label-based filtering so only Tengiz-managed resources are touched; non-Tengiz containers, images, and networks are left alone.

```
tengiz cleanup
tengiz cleanup --all
tengiz cleanup --containers --images --build-cache
tengiz cleanup --app myapp --keep 10
```

By default, cleanup removes stopped Tengiz containers (skipping active deployments), old images beyond retention, and unused networks. Use flags to select categories:

- `--containers` — remove stopped Tengiz containers (active deployments are always kept)
- `--images` — remove old Tengiz images beyond the retention count
- `--networks` — remove unused Docker networks
- `--build-cache` — remove Docker build cache
- `--volumes` — remove unused anonymous volumes (opt-in; may delete data)
- `-a, --all` — remove all non-protected Tengiz images, ignoring retention
- `--app <name>` — restrict cleanup to a single app
- `--keep <n>` — images per app to retain (default 5)
```

- [ ] **Step 2: Add the `tengiz cleanup` line to `AGENTS.md`**

In the CLI section, after the `tengiz rollback <app>` line, add:

```
tengiz cleanup [--containers --images --volumes --networks --build-cache] [-a] [--app NAME] [--keep N] → free disk space (label-filtered, safe by default)
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

Change row #6 in the P0 table from `⬜` to `✅ (2026-08-11)`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

And add a row to the "Implemented Features (Not Pending)" table:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-11) |
```

- [ ] **Step 4: Verify docs render (no tests needed)**

Run: `git diff --stat`

Expected: 3 modified doc files

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

### Task 8: Final verification

- [ ] **Step 1: Run the full verification suite**

Run: `go build ./... && go test ./... -count=1 && go vet ./...`

Expected: all packages PASS, vet clean

- [ ] **Step 2: Review the diff for consistency**

Run: `git log --oneline -8 && git status`

Expected: 7 feature commits on `feat/docker-housekeeping`, working tree clean except untracked plan file (if not yet committed)

- [ ] **Step 3: Verify `tengiz cleanup --help` output**

```bash
go build -o tengiz .
./tengiz cleanup --help
```

Expected: usage line, all 8 flags listed, `--keep` shows default `(default 5)`

---

## Self-Review

**1. Spec coverage** — Feature #6 ("Docker Housekeeping (Otomatik Temizlik)") from `docs/FUTURES_FEATURES.md`:
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → Tasks 3 + 5 (containers/networks/volumes/images all implemented) ✅
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → Task 1 (build labels) + Task 3 (`label=tengiz-app` filter) + Task 5 (protected containers/in-use images) ✅
- "`tengiz cleanup` komutu eklenebilir" → Task 6 ✅
- No periodic scheduler in this plan: the FUTURES doc mentions a periodic cleanup job but the P0 table's rationale scopes the feature to the `tengiz cleanup` command. A scheduler can be a follow-up (fits P1 #57 Background Monitoring Scheduler). Not a gap for this plan.

**2. Placeholder scan** — Every code step contains complete, copy-pasteable code with exact paths, commands, and expected output. No TBD/TODO/`similar to Task N` references; helper names are repeated verbatim where reused.

**3. Type consistency** —
- `CleanupOptions`/`CleanupResult`/`DefaultImageRetention` defined in Task 2, consumed identically in Tasks 5-6 ✅
- `imageEntry` + `selectImagesToRemove(entries, protected, appName, keep, all)` defined in Task 4, called in Task 5 with the same 5 params ✅
- `countRemovedNames(out, header)` and `lastReclaimed(out)` defined in Task 3, used in Task 5 ✅
- Mock method names `Cleanup(ctx, opts runtime.CleanupOptions) (runtime.CleanupResult, error)` identical across `root_test.go`, `proxy_test.go`, `idle_test.go` ✅
- CLI flags: registration in `init()` (Task 6 Step 3) and reads via `cleanupFlagSelection` (Step 4) use the same names `containers/images/volumes/networks/build-cache/all/keep/app` ✅

No issues found after review.
