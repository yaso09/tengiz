# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command and an optional periodic cleaner so single-server Tengiz instances reclaim disk space by pruning unused Docker containers, images, volumes, networks, and build cache — while always protecting Tengiz-managed containers.

**Architecture:** A new `internal/housekeeping` package owns all cleanup logic. It shells out to the `docker` CLI (matching the existing `internal/runtime` pattern) through an injectable `run` function so the full prune pipeline is unit-testable with fake Docker output — no Docker daemon needed in tests. Container pruning lists all stopped/created/dead containers and removes only those **without** the `tengiz-app` label; image pruning removes dangling images plus old `tengiz-apps/*` images beyond a per-app retention count (and all images of removed apps). The CLI wires `tengiz cleanup` flags into `housekeeping.Options` and runs the cleaner; the long-lived `tengiz proxy` process gets a `--cleanup-interval` flag that starts a background ticker calling the same cleaner.

**Tech Stack:** Go 1.26, `os/exec` (docker CLI), Cobra (CLI), existing `config.Store`, existing `notify`/`types` packages. No new external dependencies.

## Global Constraints

- All Docker interaction goes through the `docker` CLI via `os/exec` — never the Docker SDK
- Containers labeled `tengiz-app=*` are **always protected** from container pruning (this includes preview containers, which are created with `tengiz-app=<app>`)
- Non-Tengiz named images (e.g. `nginx`, `postgres`) are never removed by `--images` — only dangling images and `tengiz-apps/*` images
- Default image retention is **5** per app; the `{env}-latest` tag (and `latest`) is always kept, and preview images (`pr-*` tags, e.g. `tengiz-apps/myapp:pr-42-abc123`) are never pruned by `--images` — preview image cleanup is owned by `tengiz preview rm`
- Env is carried in image tags, never in the repo name: builds are `tengiz-apps/<app>:<env>-<deploymentID>` with a `tengiz-apps/<app>:<env>-latest` pointer tag (e.g. `production-latest`) — see `internal/builder/builder.go:61,84`
- `--keep-images` must be `>= 1`; invalid values are a CLI error
- No category flag given to `tengiz cleanup` ⇒ all categories run
- `--dry-run` never mutates state; `--force` skips the confirmation prompt
- Implement on branch `feat/cleanup` (AGENTS.md rule: create branch for new features)
- Add/update tests for every change and pass them before each commit (AGENTS.md rule)
- Update README.md, AGENTS.md CLI list, and docs/FUTURES_FEATURES.md (AGENTS.md documentation rule)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/housekeeping/housekeeping.go` | `Options`, `Result`, `Cleaner` struct, constructors, `dockerOutput` exec helper |
| `internal/housekeeping/containers.go` | `containerInfo` + `parseContainerLine`, `parseLabels`, `isManaged` (pure parsing/filtering) |
| `internal/housekeeping/images.go` | `imageInfo` + `parseImageLine`, `imagesToPrune` (retention/orphan/dangling decision logic) |
| `internal/housekeeping/cleaner.go` | `PruneContainers`, `PruneVolumes`, `PruneNetworks`, `PruneImages`, `PruneBuildCache`, `Run` orchestration, `registeredApps`, `parseBuildCacheReclaimable`, `parseReclaimedSpace`, `splitLines` |
| `internal/housekeeping/housekeeping_test.go` | Unit tests for all parse/filter logic + prune methods with fake Docker output |
| `internal/cli/cleanup.go` | `cleanupCmd`, flag registration (`addCleanupFlags`), `buildCleanupOptions`, `confirmCleanup`, `printCleanupResult`, `runPeriodicCleanup` |
| `internal/cli/cleanup_test.go` | CLI command registration, flag presence, option-building, confirmation, periodic-cancel tests |
| `internal/cli/root.go` | Modify: `proxyCmd` — add `--cleanup-interval` flag + periodic cleanup goroutine + `housekeeping` import |
| `README.md` | Modify: document `tengiz cleanup` + proxy `--cleanup-interval` |
| `AGENTS.md` | Modify: add cleanup to CLI command list |
| `docs/FUTURES_FEATURES.md` | Modify: mark feature #6 Docker Housekeeping implemented |

---

### Task 1: housekeeping package core — types, Options, container/image parsing helpers

**Files:**
- Create: `internal/housekeeping/housekeeping.go`
- Create: `internal/housekeeping/containers.go`
- Create: `internal/housekeeping/images.go`
- Test: `internal/housekeeping/housekeeping_test.go`

**Interfaces:**
- Consumes: nothing new (uses `config.Store` type only in the struct definition)
- Produces:
  - `type Options struct { Containers, Images, Volumes, Networks, BuildCache, DryRun bool; KeepImages int }`
  - `func (o Options) Any() bool`, `func (o Options) All() Options`
  - `type Result struct { Containers, Images, Volumes, Networks []string; BuildCache string; Errors []error }`
  - `type Cleaner struct` with fields `store *config.Store`, `env string`, `run func(ctx context.Context, args ...string) (string, error)`
  - `func New(store *config.Store) *Cleaner`, `func NewWithEnv(store *config.Store, env string) *Cleaner`
  - `func dockerOutput(ctx context.Context, args ...string) (string, error)`
  - `type containerInfo struct { ID, Name string; Labels map[string]string }`
  - `func parseContainerLine(line string) (containerInfo, error)`
  - `func parseLabels(s string) map[string]string`
  - `func isManaged(info containerInfo) bool`
  - `type imageInfo struct { Repository, Tag, ID, CreatedAt string }`
  - `func parseImageLine(line string) (imageInfo, error)`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/cleanup
```

- [ ] **Step 2: Write the failing tests**

```go
// internal/housekeeping/housekeeping_test.go
package housekeeping

import (
	"testing"
)

func TestOptionsAny(t *testing.T) {
	if Options{}.Any() {
		t.Error("empty Options should have Any() == false")
	}
	if !Options{Containers: true}.Any() {
		t.Error("Options with Containers should have Any() == true")
	}
	if !Options{BuildCache: true}.Any() {
		t.Error("Options with BuildCache should have Any() == true")
	}
}

func TestOptionsAll(t *testing.T) {
	opts := Options{KeepImages: 3}.All()
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("All() did not enable every category: %+v", opts)
	}
	if opts.KeepImages != 3 {
		t.Errorf("All() changed KeepImages: %d", opts.KeepImages)
	}
}

func TestParseContainerLine(t *testing.T) {
	info, err := parseContainerLine("abc123|myapp|tengiz-app=myapp,tengiz-env=production,com.example=x")
	if err != nil {
		t.Fatalf("parseContainerLine error: %v", err)
	}
	if info.ID != "abc123" || info.Name != "myapp" {
		t.Errorf("got %+v", info)
	}
	if info.Labels["tengiz-app"] != "myapp" {
		t.Errorf("tengiz-app label = %q", info.Labels["tengiz-app"])
	}
	if info.Labels["tengiz-env"] != "production" {
		t.Errorf("tengiz-env label = %q", info.Labels["tengiz-env"])
	}
	if info.Labels["com.example"] != "x" {
		t.Errorf("com.example label = %q", info.Labels["com.example"])
	}
}

func TestParseContainerLineInvalid(t *testing.T) {
	if _, err := parseContainerLine("only-one-field"); err == nil {
		t.Error("expected error for malformed line")
	}
}

func TestParseLabelsEmpty(t *testing.T) {
	labels := parseLabels("")
	if len(labels) != 0 {
		t.Errorf("expected empty labels, got %v", labels)
	}
}

func TestIsManaged(t *testing.T) {
	if !isManaged(containerInfo{Labels: map[string]string{"tengiz-app": "myapp"}}) {
		t.Error("container with tengiz-app label should be managed")
	}
	if isManaged(containerInfo{Labels: map[string]string{"com.example": "x"}}) {
		t.Error("container without tengiz-app label should not be managed")
	}
	if isManaged(containerInfo{}) {
		t.Error("container without labels should not be managed")
	}
}

func TestParseImageLine(t *testing.T) {
	info, err := parseImageLine("tengiz-apps/myapp|1700000001|sha1|2023-11-14 10:00:00 +0000 UTC")
	if err != nil {
		t.Fatalf("parseImageLine error: %v", err)
	}
	if info.Repository != "tengiz-apps/myapp" || info.Tag != "1700000001" || info.ID != "sha1" {
		t.Errorf("got %+v", info)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/housekeeping/... -run "TestOptionsAny|TestOptionsAll|TestParseContainerLine|TestParseLabels|TestIsManaged|TestParseImageLine" -v -count=1`

Expected: FAIL — package `internal/housekeeping` does not exist.

- [ ] **Step 4: Create `internal/housekeeping/housekeeping.go`**

```go
package housekeeping

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
)

const managedLabel = "tengiz-app"

const appImagePrefix = "tengiz-apps/"

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
	KeepImages int
}

func (o Options) Any() bool {
	return o.Containers || o.Images || o.Volumes || o.Networks || o.BuildCache
}

func (o Options) All() Options {
	o.Containers = true
	o.Images = true
	o.Volumes = true
	o.Networks = true
	o.BuildCache = true
	return o
}

type Result struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
	BuildCache string
	Errors     []error
}

type Cleaner struct {
	store *config.Store
	env   string
	run   func(ctx context.Context, args ...string) (string, error)
}

func New(store *config.Store) *Cleaner {
	return NewWithEnv(store, "")
}

func NewWithEnv(store *config.Store, env string) *Cleaner {
	if env == "" {
		env = "production"
	}
	return &Cleaner{store: store, env: env, run: dockerOutput}
}

func dockerOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}
```

- [ ] **Step 5: Create `internal/housekeeping/containers.go`**

```go
package housekeeping

import (
	"fmt"
	"strings"
)

type containerInfo struct {
	ID     string
	Name   string
	Labels map[string]string
}

func parseContainerLine(line string) (containerInfo, error) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) != 3 {
		return containerInfo{}, fmt.Errorf("invalid container line %q", line)
	}
	return containerInfo{ID: parts[0], Name: parts[1], Labels: parseLabels(parts[2])}, nil
}

func parseLabels(s string) map[string]string {
	labels := make(map[string]string)
	if s == "" {
		return labels
	}
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			labels[kv[0]] = kv[1]
		}
	}
	return labels
}

func isManaged(info containerInfo) bool {
	_, ok := info.Labels[managedLabel]
	return ok
}
```

- [ ] **Step 6: Create `internal/housekeeping/images.go`**

```go
package housekeeping

import (
	"fmt"
	"strings"
)

type imageInfo struct {
	Repository string
	Tag        string
	ID         string
	CreatedAt  string
}

func parseImageLine(line string) (imageInfo, error) {
	parts := strings.SplitN(line, "|", 4)
	if len(parts) != 4 {
		return imageInfo{}, fmt.Errorf("invalid image line %q", line)
	}
	return imageInfo{Repository: parts[0], Tag: parts[1], ID: parts[2], CreatedAt: parts[3]}, nil
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/housekeeping/... -run "TestOptionsAny|TestOptionsAll|TestParseContainerLine|TestParseLabels|TestIsManaged|TestParseImageLine" -v -count=1`

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/housekeeping/
git commit -m "feat: add housekeeping package core types and parse helpers"
```

---

### Task 2: container, volume, and network pruning

**Files:**
- Create: `internal/housekeeping/cleaner.go`
- Test: `internal/housekeeping/housekeeping_test.go`

**Interfaces:**
- Consumes: `containerInfo`, `parseContainerLine`, `isManaged` from Task 1; `Cleaner.run` from Task 1
- Produces:
  - `func (c *Cleaner) PruneContainers(ctx context.Context, dryRun bool) ([]string, error)` — returns names of containers that were (or would be, in dry-run) removed
  - `func (c *Cleaner) removeContainers(ctx context.Context, candidates []containerInfo, dryRun bool) ([]string, error)`
  - `func (c *Cleaner) PruneVolumes(ctx context.Context, dryRun bool) ([]string, error)`
  - `func (c *Cleaner) PruneNetworks(ctx context.Context, dryRun bool) ([]string, error)`
  - `func splitLines(s string) []string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/housekeeping/housekeeping_test.go — append
package housekeeping

import (
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
)

type fakeDocker struct {
	outputs map[string]string
	errs    map[string]error
	calls   []string
}

func (f *fakeDocker) run(ctx context.Context, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	f.calls = append(f.calls, joined)
	for prefix, err := range f.errs {
		if strings.HasPrefix(joined, prefix) {
			return "", err
		}
	}
	for prefix, out := range f.outputs {
		if strings.HasPrefix(joined, prefix) {
			return out, nil
		}
	}
	return "", nil
}

func TestPruneContainersSkipsManaged(t *testing.T) {
	fake := &fakeDocker{outputs: map[string]string{
		"ps": "abc123|notengiz|com.example=1\ndef456|myapp|tengiz-app=myapp,tengiz-env=production\nghi789|helper|org.foo=bar",
	}}
	c := NewWithEnv(config.NewStore(t.TempDir()), "production")
	c.run = fake.run

	removed, err := c.PruneContainers(context.Background(), true)
	if err != nil {
		t.Fatalf("PruneContainers error: %v", err)
	}
	if len(removed) != 2 || removed[0] != "notengiz" || removed[1] != "helper" {
		t.Errorf("removed = %v, want [notengiz helper]", removed)
	}
}

func TestPruneContainersRemoves(t *testing.T) {
	fake := &fakeDocker{outputs: map[string]string{
		"ps": "abc123|notengiz|com.example=1",
	}}
	c := NewWithEnv(config.NewStore(t.TempDir()), "production")
	c.run = fake.run

	removed, err := c.PruneContainers(context.Background(), false)
	if err != nil {
		t.Fatalf("PruneContainers error: %v", err)
	}
	if len(removed) != 1 || removed[0] != "notengiz" {
		t.Errorf("removed = %v, want [notengiz]", removed)
	}
	found := false
	for _, call := range fake.calls {
		if call == "rm abc123" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected docker rm call, calls: %v", fake.calls)
	}
}

func TestPruneVolumes(t *testing.T) {
	fake := &fakeDocker{outputs: map[string]string{
		"volume ls": "v1\nv2",
	}}
	c := NewWithEnv(config.NewStore(t.TempDir()), "production")
	c.run = fake.run

	removed, err := c.PruneVolumes(context.Background(), true)
	if err != nil {
		t.Fatalf("PruneVolumes error: %v", err)
	}
	if len(removed) != 2 || removed[0] != "v1" || removed[1] != "v2" {
		t.Errorf("removed = %v, want [v1 v2]", removed)
	}
}

func TestPruneNetworks(t *testing.T) {
	fake := &fakeDocker{outputs: map[string]string{
		"network ls": "mynet\nbridge\nhost\nnone",
	}}
	c := NewWithEnv(config.NewStore(t.TempDir()), "production")
	c.run = fake.run

	removed, err := c.PruneNetworks(context.Background(), true)
	if err != nil {
		t.Fatalf("PruneNetworks error: %v", err)
	}
	if len(removed) != 1 || removed[0] != "mynet" {
		t.Errorf("removed = %v, want [mynet]", removed)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/housekeeping/... -run "TestPruneContainers|TestPruneVolumes|TestPruneNetworks" -v -count=1`

Expected: FAIL — `PruneContainers`, `PruneVolumes`, `PruneNetworks` undefined.

- [ ] **Step 3: Create `internal/housekeeping/cleaner.go`**

```go
package housekeeping

import (
	"context"
	"log"
	"strings"
)

func splitLines(s string) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func (c *Cleaner) PruneContainers(ctx context.Context, dryRun bool) ([]string, error) {
	out, err := c.run(ctx, "ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--filter", "status=dead",
		"--format", `{{.ID}}|{{.Names}}|{{.Labels}}`)
	if err != nil {
		return nil, err
	}
	var candidates []containerInfo
	for _, line := range splitLines(out) {
		info, err := parseContainerLine(line)
		if err != nil {
			continue
		}
		if !isManaged(info) {
			candidates = append(candidates, info)
		}
	}
	return c.removeContainers(ctx, candidates, dryRun)
}

func (c *Cleaner) removeContainers(ctx context.Context, candidates []containerInfo, dryRun bool) ([]string, error) {
	var removed []string
	for _, cand := range candidates {
		if dryRun {
			removed = append(removed, cand.Name)
			continue
		}
		if _, err := c.run(ctx, "rm", cand.ID); err != nil {
			log.Printf("[housekeeping] rm container %s failed: %v", cand.Name, err)
			continue
		}
		removed = append(removed, cand.Name)
	}
	return removed, nil
}

func (c *Cleaner) PruneVolumes(ctx context.Context, dryRun bool) ([]string, error) {
	out, err := c.run(ctx, "volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}")
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, name := range splitLines(out) {
		if dryRun {
			removed = append(removed, name)
			continue
		}
		if _, err := c.run(ctx, "volume", "rm", name); err != nil {
			log.Printf("[housekeeping] rm volume %s failed: %v", name, err)
			continue
		}
		removed = append(removed, name)
	}
	return removed, nil
}

func (c *Cleaner) PruneNetworks(ctx context.Context, dryRun bool) ([]string, error) {
	out, err := c.run(ctx, "network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}")
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, name := range splitLines(out) {
		if name == "bridge" || name == "host" || name == "none" {
			continue
		}
		if dryRun {
			removed = append(removed, name)
			continue
		}
		if _, err := c.run(ctx, "network", "rm", name); err != nil {
			log.Printf("[housekeeping] rm network %s failed: %v", name, err)
			continue
		}
		removed = append(removed, name)
	}
	return removed, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/housekeeping/... -run "TestPruneContainers|TestPruneVolumes|TestPruneNetworks" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/housekeeping/cleaner.go internal/housekeeping/housekeeping_test.go
git commit -m "feat: add container, volume, and network pruning to housekeeping"
```

---

### Task 3: image pruning with per-app retention and orphan detection

**Files:**
- Modify: `internal/housekeeping/images.go`
- Modify: `internal/housekeeping/cleaner.go`
- Test: `internal/housekeeping/housekeeping_test.go`

**Interfaces:**
- Consumes: `imageInfo`, `parseImageLine`, `appImagePrefix`, `managedLabel` from Task 1; `Cleaner.run`, `Cleaner.store`, `Cleaner.env`
- Produces:
  - `func imagesToPrune(imgs []imageInfo, keep int, registered map[string]bool) []string` — deterministic order: dangling image IDs first, then orphaned-app images (apps sorted), then oldest-beyond-retention per registered app
  - `func (c *Cleaner) PruneImages(ctx context.Context, dryRun bool, keep int) ([]string, error)`
  - `func (c *Cleaner) registeredApps() map[string]bool`

- [ ] **Step 1: Write the failing tests**

```go
// internal/housekeeping/housekeeping_test.go — append
package housekeeping

import (
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)

func TestImagesToPrune(t *testing.T) {
	imgs := []imageInfo{
		{Repository: "tengiz-apps/myapp", Tag: "production-1700000001", ID: "sha1", CreatedAt: "2023-11-14 10:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-1700000002", ID: "sha2", CreatedAt: "2023-11-14 11:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-1700000003", ID: "sha3", CreatedAt: "2023-11-14 12:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-latest", ID: "sha4", CreatedAt: "2023-11-14 13:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "pr-42-1700000001", ID: "sha4b", CreatedAt: "2023-11-14 12:30:00 +0000 UTC"},
		{Repository: "tengiz-apps/gone", Tag: "production-1700000004", ID: "sha5", CreatedAt: "2023-11-14 12:00:00 +0000 UTC"},
		{Repository: "<none>", Tag: "<none>", ID: "sha6", CreatedAt: "2023-11-14 12:00:00 +0000 UTC"},
		{Repository: "nginx", Tag: "latest", ID: "sha7", CreatedAt: "2023-11-14 12:00:00 +0000 UTC"},
	}
	registered := map[string]bool{"myapp": true}

	got := imagesToPrune(imgs, 2, registered)
	want := []string{
		"sha6",
		"tengiz-apps/gone:production-1700000004",
		"tengiz-apps/myapp:production-1700000001",
	}
	if len(got) != len(want) {
		t.Fatalf("imagesToPrune = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("imagesToPrune[%d] = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestImagesToPruneKeepsAllWithinRetention(t *testing.T) {
	imgs := []imageInfo{
		{Repository: "tengiz-apps/myapp", Tag: "production-1700000001", ID: "sha1", CreatedAt: "2023-11-14 10:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-1700000002", ID: "sha2", CreatedAt: "2023-11-14 11:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-latest", ID: "sha3", CreatedAt: "2023-11-14 12:00:00 +0000 UTC"},
	}
	got := imagesToPrune(imgs, 5, map[string]bool{"myapp": true})
	if len(got) != 0 {
		t.Errorf("imagesToPrune = %v, want []", got)
	}
}

func TestImagesToPruneNeverPrunesPreview(t *testing.T) {
	imgs := []imageInfo{
		{Repository: "tengiz-apps/myapp", Tag: "pr-42-1700000001", ID: "sha1", CreatedAt: "2023-11-14 10:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "pr-7-1700000002", ID: "sha2", CreatedAt: "2023-11-14 11:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-1700000003", ID: "sha3", CreatedAt: "2023-11-14 12:00:00 +0000 UTC"},
	}
	got := imagesToPrune(imgs, 1, map[string]bool{"myapp": true})
	if len(got) != 0 {
		t.Errorf("preview images must never be pruned, got %v", got)
	}
}

func TestPruneImagesDryRun(t *testing.T) {
	fake := &fakeDocker{outputs: map[string]string{
		"images": strings.Join([]string{
			"tengiz-apps/myapp|production-1700000001|sha1|2023-11-14 10:00:00 +0000 UTC",
			"tengiz-apps/myapp|production-1700000002|sha2|2023-11-14 11:00:00 +0000 UTC",
			"tengiz-apps/myapp|production-latest|sha2b|2023-11-14 12:00:00 +0000 UTC",
			"tengiz-apps/myapp|pr-42-1700000001|sha2c|2023-11-14 12:30:00 +0000 UTC",
			"tengiz-apps/gone|production-1700000003|sha3|2023-11-14 12:00:00 +0000 UTC",
			"<none>|<none>|sha4|2023-11-14 12:00:00 +0000 UTC",
		}, "\n"),
	}}
	store := config.NewStore(t.TempDir())
	store.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp"}})
	c := NewWithEnv(store, "production")
	c.run = fake.run

	removed, err := c.PruneImages(context.Background(), true, 5)
	if err != nil {
		t.Fatalf("PruneImages error: %v", err)
	}
	want := []string{"sha4", "tengiz-apps/gone:production-1700000003"}
	if len(removed) != len(want) {
		t.Fatalf("removed = %v, want %v", removed, want)
	}
	for i := range want {
		if removed[i] != want[i] {
			t.Fatalf("removed[%d] = %q, want %q", i, removed[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/housekeeping/... -run "TestImagesToPrune|TestPruneImages" -v -count=1`

Expected: FAIL — `imagesToPrune`, `PruneImages`, `registeredApps` undefined.

- [ ] **Step 3: Add `imagesToPrune` to `internal/housekeeping/images.go`**

Append to `internal/housekeeping/images.go`:

```go
func imagesToPrune(imgs []imageInfo, keep int, registered map[string]bool) []string {
	var toRemove []string
	var dangling []imageInfo
	byApp := make(map[string][]imageInfo)

	for _, img := range imgs {
		if img.Repository == "<none>" || img.Tag == "<none>" {
			dangling = append(dangling, img)
			continue
		}
		if !strings.HasPrefix(img.Repository, appImagePrefix) {
			continue
		}
		if strings.HasPrefix(img.Tag, "pr-") {
			continue
		}
		if img.Tag == "latest" || strings.HasSuffix(img.Tag, "-latest") {
			continue
		}
		app := strings.TrimPrefix(img.Repository, appImagePrefix)
		byApp[app] = append(byApp[app], img)
	}

	for _, d := range dangling {
		toRemove = append(toRemove, d.ID)
	}

	apps := make([]string, 0, len(byApp))
	for app := range byApp {
		apps = append(apps, app)
	}
	sort.Strings(apps)

	for _, app := range apps {
		appImgs := byApp[app]
		if !registered[app] {
			for _, img := range appImgs {
				toRemove = append(toRemove, img.Repository+":"+img.Tag)
			}
			continue
		}
		sort.Slice(appImgs, func(i, j int) bool {
			return appImgs[i].CreatedAt < appImgs[j].CreatedAt
		})
		excess := len(appImgs) - keep
		if excess > 0 {
			for _, img := range appImgs[:excess] {
				toRemove = append(toRemove, img.Repository+":"+img.Tag)
			}
		}
	}
	return toRemove
}
```

Update the import block of `internal/housekeeping/images.go` to add `"sort"`:

```go
import (
	"fmt"
	"sort"
	"strings"
)
```

- [ ] **Step 4: Add `PruneImages` and `registeredApps` to `internal/housekeeping/cleaner.go`**

Append to `internal/housekeeping/cleaner.go`:

```go
func (c *Cleaner) PruneImages(ctx context.Context, dryRun bool, keep int) ([]string, error) {
	if keep <= 0 {
		keep = 5
	}
	out, err := c.run(ctx, "images", "--no-trunc",
		"--format", "{{.Repository}}|{{.Tag}}|{{.ID}}|{{.CreatedAt}}")
	if err != nil {
		return nil, err
	}
	var imgs []imageInfo
	for _, line := range splitLines(out) {
		info, err := parseImageLine(line)
		if err != nil {
			continue
		}
		imgs = append(imgs, info)
	}
	targets := imagesToPrune(imgs, keep, c.registeredApps())
	var removed []string
	for _, target := range targets {
		if dryRun {
			removed = append(removed, target)
			continue
		}
		if _, err := c.run(ctx, "rmi", target); err != nil {
			log.Printf("[housekeeping] rmi %s failed: %v", target, err)
			continue
		}
		removed = append(removed, target)
	}
	return removed, nil
}

func (c *Cleaner) registeredApps() map[string]bool {
	apps, err := c.store.ListApps()
	if err != nil {
		return map[string]bool{}
	}
	set := make(map[string]bool, len(apps))
	for _, a := range apps {
		set[a.Name] = true
	}
	return set
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/housekeeping/... -run "TestImagesToPrune|TestPruneImages" -v -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/housekeeping/images.go internal/housekeeping/cleaner.go internal/housekeeping/housekeeping_test.go
git commit -m "feat: add image pruning with per-app retention and orphan cleanup"
```

---

### Task 4: build cache pruning and `Run` orchestration

**Files:**
- Modify: `internal/housekeeping/cleaner.go`
- Test: `internal/housekeeping/housekeeping_test.go`

**Interfaces:**
- Consumes: all `Prune*` methods from Tasks 2-3; `Options`, `Result` from Task 1
- Produces:
  - `func (c *Cleaner) PruneBuildCache(ctx context.Context, dryRun bool) (string, error)` — dry-run returns reclaimable size string; real run returns "Total reclaimed space" string
  - `func (c *Cleaner) Run(ctx context.Context, opts Options) (Result, error)` — runs each enabled category, aggregates per-category errors, returns non-nil error if any occurred
  - `func parseBuildCacheReclaimable(out string) string`
  - `func parseReclaimedSpace(out string) string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/housekeeping/housekeeping_test.go — append
package housekeeping

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)

func TestParseBuildCacheReclaimable(t *testing.T) {
	out := `Images|12|5|1.234GB|890.2MB
Containers|4|3|1.1kB|0B
Local Volumes|2|1|123.4MB|0B
Build Cache|6|0|45.6MB|45.6MB`
	if got := parseBuildCacheReclaimable(out); got != "45.6MB" {
		t.Errorf("parseBuildCacheReclaimable = %q, want 45.6MB", got)
	}
}

func TestPruneBuildCacheDryRun(t *testing.T) {
	fake := &fakeDocker{outputs: map[string]string{
		"system df": "Images|12|5|1.234GB|890.2MB\nBuild Cache|6|0|45.6MB|45.6MB",
	}}
	c := NewWithEnv(config.NewStore(t.TempDir()), "production")
	c.run = fake.run

	info, err := c.PruneBuildCache(context.Background(), true)
	if err != nil {
		t.Fatalf("PruneBuildCache error: %v", err)
	}
	if info != "45.6MB" {
		t.Errorf("info = %q, want 45.6MB", info)
	}
}

func TestPruneBuildCache(t *testing.T) {
	fake := &fakeDocker{outputs: map[string]string{
		"builder": "Total reclaimed space: 45.6MB",
	}}
	c := NewWithEnv(config.NewStore(t.TempDir()), "production")
	c.run = fake.run

	info, err := c.PruneBuildCache(context.Background(), false)
	if err != nil {
		t.Fatalf("PruneBuildCache error: %v", err)
	}
	if info != "45.6MB" {
		t.Errorf("info = %q, want 45.6MB", info)
	}
	found := false
	for _, call := range fake.calls {
		if call == "builder prune -af" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected builder prune call, calls: %v", fake.calls)
	}
}

func TestRunCategories(t *testing.T) {
	fake := &fakeDocker{outputs: map[string]string{
		"ps":        "abc123|notengiz|com.example=1",
		"volume ls": "v1",
	}}
	c := NewWithEnv(config.NewStore(t.TempDir()), "production")
	c.run = fake.run

	res, err := c.Run(context.Background(), Options{Containers: true, Volumes: true, DryRun: true, KeepImages: 5})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(res.Containers) != 1 || res.Containers[0] != "notengiz" {
		t.Errorf("Containers = %v", res.Containers)
	}
	if len(res.Volumes) != 1 || res.Volumes[0] != "v1" {
		t.Errorf("Volumes = %v", res.Volumes)
	}
	if len(res.Images) != 0 || len(res.Networks) != 0 || res.BuildCache != "" {
		t.Errorf("unexpected categories pruned: %+v", res)
	}
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "images") || strings.HasPrefix(call, "network ls") ||
			call == "builder prune -af" || strings.HasPrefix(call, "system df") {
			t.Errorf("unexpected docker call: %q", call)
		}
	}
}

func TestRunAggregatesErrors(t *testing.T) {
	fake := &fakeDocker{
		outputs: map[string]string{
			"ps":        "abc123|notengiz|com.example=1",
			"volume ls": "v1",
		},
		errs: map[string]error{
			"network ls": errors.New("docker network ls failed"),
		},
	}
	c := NewWithEnv(config.NewStore(t.TempDir()), "production")
	c.run = fake.run

	res, err := c.Run(context.Background(), Options{
		Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true,
		DryRun: true, KeepImages: 5,
	})
	if err == nil {
		t.Fatal("expected Run to return error")
	}
	if len(res.Errors) != 1 {
		t.Errorf("Errors = %v, want 1 error", res.Errors)
	}
	if len(res.Containers) != 1 || len(res.Volumes) != 1 {
		t.Errorf("other categories should still succeed: %+v", res)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/housekeeping/... -run "TestParseBuildCacheReclaimable|TestPruneBuildCache|TestRunCategories|TestRunAggregatesErrors" -v -count=1`

Expected: FAIL — `PruneBuildCache`, `Run`, `parseBuildCacheReclaimable`, `parseReclaimedSpace` undefined.

- [ ] **Step 3: Append to `internal/housekeeping/cleaner.go`**

`fmt` was removed from the import block in Task 2 because it was unused there. `Run` below uses `fmt.Errorf`, so first update the import block of `internal/housekeeping/cleaner.go` to:

```go
import (
	"context"
	"fmt"
	"log"
	"strings"
)
```

Then append:

```go
func (c *Cleaner) PruneBuildCache(ctx context.Context, dryRun bool) (string, error) {
	if dryRun {
		out, err := c.run(ctx, "system", "df",
			"--format", "{{.Type}}|{{.TotalCount}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}")
		if err != nil {
			return "", err
		}
		return parseBuildCacheReclaimable(out), nil
	}
	out, err := c.run(ctx, "builder", "prune", "-af")
	if err != nil {
		return "", err
	}
	return parseReclaimedSpace(out), nil
}

func (c *Cleaner) Run(ctx context.Context, opts Options) (Result, error) {
	res := Result{}
	if opts.Containers {
		removed, err := c.PruneContainers(ctx, opts.DryRun)
		res.Containers = removed
		if err != nil {
			res.Errors = append(res.Errors, err)
		}
	}
	if opts.Images {
		removed, err := c.PruneImages(ctx, opts.DryRun, opts.KeepImages)
		res.Images = removed
		if err != nil {
			res.Errors = append(res.Errors, err)
		}
	}
	if opts.Volumes {
		removed, err := c.PruneVolumes(ctx, opts.DryRun)
		res.Volumes = removed
		if err != nil {
			res.Errors = append(res.Errors, err)
		}
	}
	if opts.Networks {
		removed, err := c.PruneNetworks(ctx, opts.DryRun)
		res.Networks = removed
		if err != nil {
			res.Errors = append(res.Errors, err)
		}
	}
	if opts.BuildCache {
		info, err := c.PruneBuildCache(ctx, opts.DryRun)
		res.BuildCache = info
		if err != nil {
			res.Errors = append(res.Errors, err)
		}
	}
	if len(res.Errors) > 0 {
		return res, fmt.Errorf("cleanup completed with %d error(s)", len(res.Errors))
	}
	return res, nil
}

func parseBuildCacheReclaimable(out string) string {
	for _, line := range splitLines(out) {
		parts := strings.Split(line, "|")
		if len(parts) >= 5 && parts[0] == "Build Cache" {
			return parts[4]
		}
	}
	return ""
}

func parseReclaimedSpace(out string) string {
	for _, line := range splitLines(out) {
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/housekeeping/... -run "TestParseBuildCacheReclaimable|TestPruneBuildCache|TestRunCategories|TestRunAggregatesErrors" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run the full housekeeping test suite**

Run: `go test ./internal/housekeeping/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/housekeeping/cleaner.go internal/housekeeping/housekeeping_test.go
git commit -m "feat: add build cache pruning and cleanup orchestration"
```

---

### Task 5: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `housekeeping.NewWithEnv(store, env)`, `housekeeping.Options`, `housekeeping.Result`, `cleaner.Run(ctx, opts)`; `config.NewStoreWithEnv(dataDir, env)`; `getEnv(cmd)` from `root.go`
- Produces:
  - `func addCleanupFlags(cmd *cobra.Command)` — registers `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--dry-run`, `-f/--force`, `--keep-images` (default 5)
  - `var cleanupCmd *cobra.Command` — registered on `rootCmd`
  - `func buildCleanupOptions(cmd *cobra.Command) (housekeeping.Options, error)`
  - `func confirmCleanup(cmd *cobra.Command) (bool, error)`
  - `func printCleanupResult(res housekeeping.Result)`
  - `func runPeriodicCleanup(ctx context.Context, c *housekeeping.Cleaner, interval time.Duration)` — used by Task 6

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "build-cache", "dry-run", "force", "keep-images"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func newCleanupTestCmd(args ...string) *cobra.Command {
	cmd := &cobra.Command{Use: "cleanup"}
	addCleanupFlags(cmd)
	cmd.ParseFlags(args)
	return cmd
}

func TestBuildCleanupOptionsDefaultsToAll(t *testing.T) {
	cmd := newCleanupTestCmd()
	opts, err := buildCleanupOptions(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories enabled by default, got %+v", opts)
	}
	if opts.KeepImages != 5 {
		t.Errorf("KeepImages = %d, want 5", opts.KeepImages)
	}
}

func TestBuildCleanupOptionsSingleCategory(t *testing.T) {
	cmd := newCleanupTestCmd("--images", "--dry-run", "--keep-images", "3")
	opts, err := buildCleanupOptions(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Images || !opts.DryRun {
		t.Errorf("Images/DryRun should be enabled: %+v", opts)
	}
	if opts.Containers || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("only Images should be enabled: %+v", opts)
	}
	if opts.KeepImages != 3 {
		t.Errorf("KeepImages = %d, want 3", opts.KeepImages)
	}
}

func TestBuildCleanupOptionsRejectsZeroKeep(t *testing.T) {
	cmd := newCleanupTestCmd("--keep-images", "0")
	if _, err := buildCleanupOptions(cmd); err == nil {
		t.Error("expected error for --keep-images 0")
	}
}

func TestConfirmCleanupYes(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.SetIn(strings.NewReader("y\n"))
	ok, err := confirmCleanup(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected confirmCleanup true for 'y'")
	}
}

func TestConfirmCleanupNo(t *testing.T) {
	cmd := newCleanupTestCmd()
	cmd.SetIn(strings.NewReader("n\n"))
	ok, err := confirmCleanup(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected confirmCleanup false for 'n'")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup|TestBuildCleanupOptions|TestConfirmCleanup" -v -count=1`

Expected: FAIL — `cleanupCmd`, `addCleanupFlags`, `buildCleanupOptions`, `confirmCleanup` undefined.

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/housekeeping"
)

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("containers", false, "remove stopped non-Tengiz containers")
	cmd.Flags().Bool("images", false, "remove dangling and old app images")
	cmd.Flags().Bool("volumes", false, "remove unused volumes")
	cmd.Flags().Bool("networks", false, "remove unused networks")
	cmd.Flags().Bool("build-cache", false, "remove Docker build cache")
	cmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cmd.Flags().BoolP("force", "f", false, "skip the confirmation prompt")
	cmd.Flags().Int("keep-images", 5, "keep last N images per app")
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
	addCleanupFlags(cleanupCmd)
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources",
	Long: `Prune unused Docker resources to reclaim disk space.

By default (no category flags) all categories are cleaned. Containers
managed by Tengiz (labeled tengiz-app=*) are always protected.

Categories:
  --containers   remove stopped containers not managed by Tengiz
  --images       remove dangling images and old per-app images beyond retention
  --volumes      remove unused volumes
  --networks     remove unused networks
  --build-cache  remove Docker build cache

Use --dry-run to preview what would be removed, --force to skip the
confirmation prompt.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := buildCleanupOptions(cmd)
		if err != nil {
			return err
		}

		if !opts.DryRun && !opts.Force {
			confirmed, err := confirmCleanup(cmd)
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("[tengiz] cleanup aborted.")
				return nil
			}
		}

		env := getEnv(cmd)
		store := config.NewStoreWithEnv(dataDir, env)
		cleaner := housekeeping.NewWithEnv(store, env)

		res, err := cleaner.Run(cmd.Context(), opts)
		printCleanupResult(res)
		return err
	},
}

func buildCleanupOptions(cmd *cobra.Command) (housekeeping.Options, error) {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	keep, _ := cmd.Flags().GetInt("keep-images")

	if keep < 1 {
		return housekeeping.Options{}, fmt.Errorf("--keep-images must be at least 1")
	}

	opts := housekeeping.Options{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		DryRun:     dryRun,
		KeepImages: keep,
	}
	if !opts.Any() {
		opts = opts.All()
	}
	return opts, nil
}

func confirmCleanup(cmd *cobra.Command) (bool, error) {
	fmt.Print("This will remove unused Docker resources. Continue? [y/N]: ")
	reader := bufio.NewReader(cmd.InOrStdin())
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "y" || input == "yes", nil
}

func printCleanupResult(res housekeeping.Result) {
	if len(res.Containers) == 0 && len(res.Images) == 0 &&
		len(res.Volumes) == 0 && len(res.Networks) == 0 && res.BuildCache == "" {
		fmt.Println("[tengiz] nothing to clean.")
		return
	}
	if res.BuildCache != "" {
		fmt.Printf("[tengiz] build cache reclaimable: %s\n", res.BuildCache)
	}
	for _, name := range res.Containers {
		fmt.Printf("[tengiz] removed container: %s\n", name)
	}
	for _, name := range res.Images {
		fmt.Printf("[tengiz] removed image: %s\n", name)
	}
	for _, name := range res.Volumes {
		fmt.Printf("[tengiz] removed volume: %s\n", name)
	}
	for _, name := range res.Networks {
		fmt.Printf("[tengiz] removed network: %s\n", name)
	}
}

func runPeriodicCleanup(ctx context.Context, c *housekeeping.Cleaner, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			opts := housekeeping.Options{KeepImages: 5}.All()
			res, err := c.Run(ctx, opts)
			if err != nil {
				log.Printf("[tengiz] periodic cleanup error: %v", err)
				continue
			}
			log.Printf("[tengiz] periodic cleanup done: %d containers, %d images, %d volumes, %d networks removed",
				len(res.Containers), len(res.Images), len(res.Volumes), len(res.Networks))
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestBuildCleanupOptions|TestConfirmCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 5: Build the binary**

Run: `go build -o tengiz .`

Expected: Build succeeds.

- [ ] **Step 6: Manual smoke test (requires a working docker daemon)**

Run: `./tengiz cleanup --dry-run --force`

Expected: prints `[tengiz] nothing to clean.` (or a list of would-be-removed items).

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 6: periodic cleanup in proxy + documentation

**Files:**
- Modify: `internal/cli/root.go` — add `--cleanup-interval` flag to `proxyCmd`, start cleanup goroutine, add `housekeeping` import
- Test: `internal/cli/cleanup_test.go`
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: `runPeriodicCleanup(ctx, cleaner, interval)` from Task 5; `housekeeping.NewWithEnv(store, env)`
- Produces: `tengiz proxy --cleanup-interval 24h` runs periodic cleanup; docs reflect the new feature

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go — append
package cli

import (
	"context"
	"testing"
	"time"
)

func TestProxyCmdCleanupIntervalFlag(t *testing.T) {
	if proxyCmd.Flags().Lookup("cleanup-interval") == nil {
		t.Error("proxyCmd missing --cleanup-interval flag")
	}
}

func TestRunPeriodicCleanupCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runPeriodicCleanup(ctx, nil, time.Hour)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runPeriodicCleanup did not stop on ctx cancel")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestProxyCmdCleanupIntervalFlag|TestRunPeriodicCleanupCancels" -v -count=1`

Expected: FAIL — `--cleanup-interval` flag not registered.

- [ ] **Step 3: Register the flag and start the goroutine in `internal/cli/root.go`**

In `init()` (after the `webhookCmd` flags block ending at line 88, just before the closing `}` at line 89). Note: proxyCmd's other flags (`-a`, `-p`, `--env`) are registered in `Execute()` at lines 1786-1788, but this new flag **must** be registered in `init()` — otherwise `TestProxyCmdCleanupIntervalFlag` won't see it (tests never call `Execute()`):

```go
proxyCmd.Flags().Duration("cleanup-interval", 0, "periodically clean unused Docker resources (e.g. 24h, 0 = disabled)")
```

Add the import to the import block (after the `health` import):

```go
	"github.com/yaso09/tengiz/internal/health"
	"github.com/yaso09/tengiz/internal/housekeeping"
```

In `proxyCmd`'s `RunE`, immediately after the `ctx` is created (after the `signal.Notify` goroutine, just before `return p.Start(ctx)`), add:

```go
		cleanupInterval, _ := cmd.Flags().GetDuration("cleanup-interval")
		if cleanupInterval > 0 {
			cleaner := housekeeping.NewWithEnv(store, env)
			go runPeriodicCleanup(ctx, cleaner, cleanupInterval)
			fmt.Printf("[tengiz] periodic cleanup enabled every %s\n", cleanupInterval)
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestProxyCmdCleanupIntervalFlag|TestRunPeriodicCleanupCancels" -v -count=1`

Expected: PASS

- [ ] **Step 5: Update `README.md`**

Add a new section after the `tengiz run` section (after line 204):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling images and old per-app images beyond retention |
| `--volumes` | Remove unused volumes |
| `--networks` | Remove unused networks |
| `--build-cache` | Remove Docker build cache |
| `--dry-run` | Show what would be removed without removing anything |
| `-f`, `--force` | Skip the confirmation prompt |
| `--keep-images N` | Keep last N images per app (default: 5) |

With no category flags, all categories are cleaned. Containers managed by Tengiz (labeled `tengiz-app=*`) are always protected.
```

Update the proxy section's flag table (after line 140) with a new row:

```markdown
| `--cleanup-interval` | Periodically clean unused Docker resources (e.g. `24h`, `0` = disabled) |
```

- [ ] **Step 6: Update `AGENTS.md`**

Add to the CLI command list (after the `tengiz notification show` line):

```markdown
tengiz cleanup           → prune unused Docker resources (--containers/--images/--volumes/--networks/--build-cache/--dry-run/--force)
```

- [ ] **Step 7: Update `docs/FUTURES_FEATURES.md`**

Change the priority table row for #6 (line 19) from ⬜ to ✅:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the Implemented Features table (after line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-11) |
```

- [ ] **Step 8: Full verification**

Run: `go build -o tengiz .`

Expected: Build succeeds.

Run: `go vet ./...`

Expected: No issues.

Run: `go test ./... -count=1`

Expected: All tests pass (proxy package tests may take ~2s each due to TCP dial timeouts, which is expected per AGENTS.md).

- [ ] **Step 9: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "feat: add periodic cleanup to proxy and document docker housekeeping"
```
