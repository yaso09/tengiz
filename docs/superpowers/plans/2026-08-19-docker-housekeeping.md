# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by removing stale Tengiz containers, old `tengiz-apps/*` images, dangling images, and (opt-in) unused Docker volumes/networks/build cache — while never touching active deployments.

**Architecture:** A new `runtime.Cleaner` interface (separate from `Manager`, implemented by `dockerRuntime`) exposes thin `docker` CLI wrappers. All decision logic lives in pure functions (`SelectStaleContainers`, `SelectImagesToRemove`, JSON parsers) that are unit-tested without Docker. The CLI command (`internal/cli/cleanup.go`) orchestrates: it builds an "active" set from the env-scoped store (`apps.json` + `previews.json`), computes stale containers and removable images, and either reports them (dry run, default) or removes them (`--force`).

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` Docker CLI (no SDK), existing `config.Store`, existing `runtime.Manager`/`dockerRuntime`. No new external dependencies.

## Global Constraints

- No new external Go dependencies
- Docker is always invoked via `os/exec` (never the Docker SDK)
- New `Cleaner` interface is defined separately from `Manager` — existing `stubManager`, `mockRTForDeploy`, and `mockRuntime` are NOT modified
- All containers/images are enumerated with `docker ps --format json` and `docker images --format json` (Docker 20.10+)
- Tengiz-managed containers carry the `tengiz-app` label (constant `labelKey` in `internal/runtime/docker.go`); environment containers carry `tengiz-env` (`envLabelKey`)
- Image tags are `tengiz-apps/<app>:<env>-<deploymentID>` plus `<env>-latest` (see `internal/builder/builder.go:61,84`)
- `tengiz cleanup` is a **dry run by default**; `--force` (`-f`) actually removes resources
- Only **stopped** containers that are **not the active container for any app/preview in the current env** are ever removed
- Only images under the `tengiz-apps/*` repository prefix are removed; `*-latest` tags and any image referenced by the store (app image, deployment history, previews) are always protected
- `--volumes`, `--networks`, `--cache` prune Docker-wide (not Tengiz-scoped) and are opt-in
- Default image retention is `--keep 5` (matches the `KeepLastNImages(ctx, name, 5)` calls in `deploy` at `internal/cli/root.go:346,466`)
- Follow existing repo rules: create branch `feat/docker-housekeeping`, add/update tests, pass `go test ./... -v -count=1`, run `go vet ./...`, then commit

---

## File Structure

| File | Responsibility | Action |
|------|---------------|--------|
| `internal/runtime/cleanup.go` | `ContainerEntry`/`ImageEntry` types, `CleanupReport`, `Cleaner` interface, JSON parsers, stale/keep selection logic, `dockerRuntime` exec methods, `NewCleaner()` | Modify (extends existing file that holds `RemoveImage`/`KeepLastNImages`) |
| `internal/runtime/cleanup_test.go` | Unit tests for parsers, selection logic, `countLines`, interface assertions | Modify |
| `internal/cli/cleanup.go` | `cleanupCmd` cobra command + `runCleanup` orchestration + `activeContainerNames`/`inUseImageTags`/`printCleanupReport` | Create |
| `internal/cli/cleanup_test.go` | `mockCleaner` + orchestration/registration/output tests | Create |
| `README.md` | Document `tengiz cleanup` in CLI Reference | Modify |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as implemented | Modify |

---

### Task 1: Container/Image entry types and JSON parsing

**Files:**
- Modify: `internal/runtime/cleanup.go` (add types + parse functions)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new (uses existing `labelKey`/`envLabelKey` constants from `internal/runtime/docker.go`)
- Produces:
  - `type ContainerEntry struct { ID string; Name string; Names []string; Image string; State string; Status string; Labels map[string]string }` (JSON tags: `ID`, `Names`, `Image`, `State`, `Status`, `Labels`)
  - `type ImageEntry struct { ID string; Repository string; Tag string; CreatedAt string; Size string }` (JSON tags: `ID`, `Repository`, `Tag`, `CreatedAt`, `Size`)
  - `ParseContainerList(out string) ([]ContainerEntry, error)` — parses one JSON object per line
  - `ParseImageList(out string) ([]ImageEntry, error)` — parses one JSON object per line
  - `parseContainerName(names []string) string` — returns first name with leading `/` trimmed

- [ ] **Step 1: Create branch and write the failing test**

```bash
git checkout -b feat/docker-housekeeping
```

Add to `internal/runtime/cleanup_test.go`:

```go
package runtime

import (
	"testing"
)

func TestParseContainerList(t *testing.T) {
	out := `{"ID":"abc123","Names":["/tengiz-myapp-1712345678"],"Image":"tengiz-apps/myapp:production-1712345678","State":"exited","Status":"Exited (0) 2 days ago","Labels":{"tengiz-app":"myapp","tengiz-env":"production"}}
{"ID":"def456","Names":["/tengiz-other"],"Image":"tengiz-apps/other:production-latest","State":"running","Status":"Up 2 hours","Labels":{"tengiz-app":"other","tengiz-env":"production"}}`
	entries, err := ParseContainerList(out)
	if err != nil {
		t.Fatalf("ParseContainerList() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	if entries[0].Name != "tengiz-myapp-1712345678" {
		t.Errorf("Name = %q, want tengiz-myapp-1712345678", entries[0].Name)
	}
	if entries[0].State != "exited" {
		t.Errorf("State = %q, want exited", entries[0].State)
	}
	if entries[0].Labels["tengiz-app"] != "myapp" {
		t.Errorf("label tengiz-app = %q, want myapp", entries[0].Labels["tengiz-app"])
	}
	if entries[1].Name != "tengiz-other" {
		t.Errorf("Name = %q, want tengiz-other", entries[1].Name)
	}
}

func TestParseContainerListEmpty(t *testing.T) {
	entries, err := ParseContainerList("")
	if err != nil {
		t.Fatalf("ParseContainerList() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len = %d, want 0", len(entries))
	}
}

func TestParseImageList(t *testing.T) {
	out := `{"ID":"sha256:aaa","Repository":"tengiz-apps/myapp","Tag":"production-1712345678","CreatedAt":"2026-08-17 10:00:00 +0000 UTC","Size":"123MB"}
{"ID":"sha256:bbb","Repository":"tengiz-apps/myapp","Tag":"production-latest","CreatedAt":"2026-08-19 10:00:00 +0000 UTC","Size":"123MB"}`
	entries, err := ParseImageList(out)
	if err != nil {
		t.Fatalf("ParseImageList() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	if entries[0].Repository != "tengiz-apps/myapp" || entries[0].Tag != "production-1712345678" {
		t.Errorf("entry 0 = %+v", entries[0])
	}
	if entries[1].Tag != "production-latest" {
		t.Errorf("entry 1 Tag = %q, want production-latest", entries[1].Tag)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParse' -count=1 -v`
Expected: FAIL — compile error `undefined: ParseContainerList` / `undefined: ParseImageList`

- [ ] **Step 3: Write the implementation**

Add to the top of `internal/runtime/cleanup.go` (keep the existing `RemoveImage`/`KeepLastNImages` methods below):

```go
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

type ContainerEntry struct {
	ID     string            `json:"ID"`
	Name   string            `json:"-"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}

type ImageEntry struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	CreatedAt  string `json:"CreatedAt"`
	Size       string `json:"Size"`
}

func ParseContainerList(out string) ([]ContainerEntry, error) {
	var entries []ContainerEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var e ContainerEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("parse container line %q: %w", line, err)
		}
		e.Name = parseContainerName(e.Names)
		entries = append(entries, e)
	}
	return entries, nil
}

func parseContainerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func ParseImageList(out string) ([]ImageEntry, error) {
	var entries []ImageEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var e ImageEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("parse image line %q: %w", line, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestParse' -count=1 -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add container and image entry parsing for cleanup"
```

---

### Task 2: Stale container and removable image selection logic

**Files:**
- Modify: `internal/runtime/cleanup.go` (add selection helpers)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `ContainerEntry`, `ImageEntry`, `ParseContainerList`, `ParseImageList` (Task 1)
- Produces:
  - `containerEnvMatches(labels map[string]string, env string) bool` — true when the container's `tengiz-env` label equals `env` or is empty (previews/legacy); containers with no `tengiz-env` label match only when `env == "production"`
  - `SelectStaleContainers(entries []ContainerEntry, active map[string]bool, env string) []ContainerEntry` — returns non-running, env-matching containers not in `active`
  - `SelectImagesToRemove(entries []ImageEntry, keep int, inUse map[string]bool) []ImageEntry` — per `tengiz-apps/*` repository, keep the newest `keep` tags (sorted by `CreatedAt`), skip `*-latest`/`latest` and anything in `inUse`
  - `ContainerNames(entries []ContainerEntry) []string` — exported; the names of the entries
  - `ImageRefs(entries []ImageEntry) []string` — exported; `repository:tag` of the entries

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestContainerEnvMatches(t *testing.T) {
	tests := []struct {
		labels map[string]string
		env    string
		want   bool
	}{
		{map[string]string{"tengiz-env": "production"}, "production", true},
		{map[string]string{"tengiz-env": "staging"}, "production", false},
		{map[string]string{"tengiz-env": "staging"}, "staging", true},
		{map[string]string{"tengiz-env": ""}, "production", true},
		{map[string]string{"tengiz-env": ""}, "staging", true},
		{nil, "production", true},
		{nil, "staging", false},
	}
	for _, tt := range tests {
		if got := containerEnvMatches(tt.labels, tt.env); got != tt.want {
			t.Errorf("containerEnvMatches(%v, %q) = %v, want %v", tt.labels, tt.env, got, tt.want)
		}
	}
}

func TestSelectStaleContainers(t *testing.T) {
	active := map[string]bool{"tengiz-myapp": true}
	entries := []ContainerEntry{
		{Name: "tengiz-myapp", State: "running", Labels: map[string]string{"tengiz-app": "myapp", "tengiz-env": "production"}},
		{Name: "tengiz-myapp", State: "exited", Labels: map[string]string{"tengiz-app": "myapp", "tengiz-env": "production"}},
		{Name: "tengiz-myapp-1712345678", State: "exited", Labels: map[string]string{"tengiz-app": "myapp", "tengiz-env": "production"}},
		{Name: "tengiz-other-1712340000", State: "exited", Labels: map[string]string{"tengiz-app": "other", "tengiz-env": "staging"}},
		{Name: "tengiz-old", State: "exited", Labels: map[string]string{"tengiz-app": "old"}},
	}
	stale := SelectStaleContainers(entries, active, "production")
	if len(stale) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(stale), stale)
	}
	got := map[string]bool{}
	for _, e := range stale {
		got[e.Name] = true
	}
	if !got["tengiz-myapp-1712345678"] {
		t.Error("expected stale versioned container to be selected")
	}
	if !got["tengiz-old"] {
		t.Error("expected legacy container (no env label) to be selected in production")
	}
}

func TestSelectImagesToRemove(t *testing.T) {
	entries := []ImageEntry{
		{Repository: "tengiz-apps/myapp", Tag: "production-1712340000", CreatedAt: "2026-08-17 10:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-1712345000", CreatedAt: "2026-08-18 10:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-1712349000", CreatedAt: "2026-08-19 10:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-latest", CreatedAt: "2026-08-19 11:00:00 +0000 UTC"},
		{Repository: "other/image", Tag: "v1", CreatedAt: "2026-08-01 10:00:00 +0000 UTC"},
	}
	toRemove := SelectImagesToRemove(entries, 2, nil)
	if len(toRemove) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(toRemove), toRemove)
	}
	if toRemove[0].Tag != "production-1712340000" {
		t.Errorf("removed tag = %q, want production-1712340000", toRemove[0].Tag)
	}
}

func TestSelectImagesToRemoveInUse(t *testing.T) {
	entries := []ImageEntry{
		{Repository: "tengiz-apps/myapp", Tag: "production-1712340000", CreatedAt: "2026-08-17 10:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-1712345000", CreatedAt: "2026-08-18 10:00:00 +0000 UTC"},
	}
	toRemove := SelectImagesToRemove(entries, 1, map[string]bool{"tengiz-apps/myapp:production-1712340000": true})
	if len(toRemove) != 0 {
		t.Fatalf("len = %d, want 0 (in-use image protected): %+v", len(toRemove), toRemove)
	}
}

func TestContainerNamesAndImageRefs(t *testing.T) {
	containers := []ContainerEntry{
		{Name: "tengiz-a-1"},
		{Name: "tengiz-a-2"},
	}
	names := ContainerNames(containers)
	if len(names) != 2 || names[0] != "tengiz-a-1" || names[1] != "tengiz-a-2" {
		t.Errorf("ContainerNames() = %v", names)
	}
	images := []ImageEntry{
		{Repository: "tengiz-apps/a", Tag: "production-1"},
	}
	refs := ImageRefs(images)
	if len(refs) != 1 || refs[0] != "tengiz-apps/a:production-1" {
		t.Errorf("ImageRefs() = %v", refs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestContainerEnvMatches|TestSelectStaleContainers|TestSelectImagesToRemove|TestContainerNamesAndImageRefs' -count=1 -v`
Expected: FAIL — compile error `undefined: containerEnvMatches` / `undefined: SelectStaleContainers` / `undefined: SelectImagesToRemove` / `undefined: ContainerNames` / `undefined: ImageRefs`

- [ ] **Step 3: Write the implementation**

Append to `internal/runtime/cleanup.go` (after `ParseImageList`):

```go
func containerEnvMatches(labels map[string]string, env string) bool {
	if v, ok := labels["tengiz-env"]; ok {
		return v == env || v == ""
	}
	return env == "production"
}

func SelectStaleContainers(entries []ContainerEntry, active map[string]bool, env string) []ContainerEntry {
	var stale []ContainerEntry
	for _, e := range entries {
		if e.State == "running" {
			continue
		}
		if !containerEnvMatches(e.Labels, env) {
			continue
		}
		if active[e.Name] {
			continue
		}
		stale = append(stale, e)
	}
	return stale
}

func SelectImagesToRemove(entries []ImageEntry, keep int, inUse map[string]bool) []ImageEntry {
	if keep < 1 {
		keep = 5
	}
	groups := make(map[string][]ImageEntry)
	for _, e := range entries {
		if !strings.HasPrefix(e.Repository, "tengiz-apps/") {
			continue
		}
		if e.Tag == "latest" || strings.HasSuffix(e.Tag, "-latest") {
			continue
		}
		ref := e.Repository + ":" + e.Tag
		if inUse[ref] {
			continue
		}
		groups[e.Repository] = append(groups[e.Repository], e)
	}
	var toRemove []ImageEntry
	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			return group[i].CreatedAt > group[j].CreatedAt
		})
		if len(group) <= keep {
			continue
		}
		toRemove = append(toRemove, group[:len(group)-keep]...)
	}
	return toRemove
}

func ContainerNames(entries []ContainerEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	return names
}

func ImageRefs(entries []ImageEntry) []string {
	refs := make([]string, 0, len(entries))
	for _, e := range entries {
		refs = append(refs, e.Repository+":"+e.Tag)
	}
	return refs
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestContainerEnvMatches|TestSelectStaleContainers|TestSelectImagesToRemove|TestContainerNamesAndImageRefs' -count=1 -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add stale container and image retention selection for cleanup"
```

---

### Task 3: Cleaner interface and Docker exec methods

**Files:**
- Modify: `internal/runtime/cleanup.go` (add `CleanupReport`, `Cleaner`, `NewCleaner`, `dockerRuntime` methods, `countLines`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `ContainerEntry`, `ImageEntry`, `ParseContainerList`, `ParseImageList` (Task 1); `SelectStaleContainers`, `ContainerNames`, `ImageRefs`, `SelectImagesToRemove` (Task 2)
- Produces:
  - `type CleanupReport struct { DryRun bool; ContainersRemoved []string; ImagesRemoved []string; DanglingImages int; Volumes int; Networks int; CacheCleaned bool }`
  - `type Cleaner interface { ListTengizContainers(ctx) ([]ContainerEntry, error); ListTengizImages(ctx) ([]ImageEntry, error); RemoveContainers(ctx, names []string) error; RemoveImages(ctx, refs []string) error; PruneDanglingImages(ctx) (int, error); PruneVolumes(ctx) (int, error); PruneNetworks(ctx) (int, error); PruneBuildCache(ctx) error }`
  - `NewCleaner() (Cleaner, error)` — verifies the `docker` binary exists, returns `*dockerRuntime`
  - `countLines(s string) int` — number of non-empty lines in trimmed output

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestCountLines(t *testing.T) {
	if got := countLines(""); got != 0 {
		t.Errorf("countLines(\"\") = %d, want 0", got)
	}
	if got := countLines("abc\ndef\n"); got != 2 {
		t.Errorf("countLines(abc\\ndef\\n) = %d, want 2", got)
	}
	if got := countLines("\n  \n"); got != 0 {
		t.Errorf("countLines(whitespace) = %d, want 0", got)
	}
}

func TestDockerRuntimeSatisfiesCleaner(t *testing.T) {
	var _ Cleaner = (*dockerRuntime)(nil)
}

func TestNewCleanerRequiresDocker(t *testing.T) {
	_, err := NewCleaner()
	if err == nil {
		return // docker present in CI — nothing to assert
	}
	if !strings.Contains(err.Error(), "docker not found") {
		t.Errorf("unexpected error: %v", err)
	}
}
```

Add the `strings` import to `internal/runtime/cleanup_test.go`:

```go
import (
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestCountLines|TestDockerRuntimeSatisfiesCleaner|TestNewCleanerRequiresDocker' -count=1 -v`
Expected: FAIL — compile error `undefined: Cleaner` / `undefined: countLines` / `undefined: NewCleaner`

- [ ] **Step 3: Write the implementation**

Append to `internal/runtime/cleanup.go` (after the Task 2 helpers):

```go
type CleanupReport struct {
	DryRun            bool
	ContainersRemoved []string
	ImagesRemoved     []string
	DanglingImages    int
	Volumes           int
	Networks          int
	CacheCleaned      bool
}

type Cleaner interface {
	ListTengizContainers(ctx context.Context) ([]ContainerEntry, error)
	ListTengizImages(ctx context.Context) ([]ImageEntry, error)
	RemoveContainers(ctx context.Context, names []string) error
	RemoveImages(ctx context.Context, refs []string) error
	PruneDanglingImages(ctx context.Context) (int, error)
	PruneVolumes(ctx context.Context) (int, error)
	PruneNetworks(ctx context.Context) (int, error)
	PruneBuildCache(ctx context.Context) error
}

var _ Cleaner = (*dockerRuntime)(nil)

func NewCleaner() (Cleaner, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerRuntime{}, nil
}

func (r *dockerRuntime) ListTengizContainers(ctx context.Context) ([]ContainerEntry, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "label="+labelKey,
		"--format", "json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return ParseContainerList(string(out))
}

func (r *dockerRuntime) ListTengizImages(ctx context.Context) ([]ImageEntry, error) {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", "reference=tengiz-apps/*",
		"--format", "json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return ParseImageList(string(out))
}

func (r *dockerRuntime) RemoveContainers(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	args := append([]string{"rm", "-f"}, names...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) RemoveImages(ctx context.Context, refs []string) error {
	if len(refs) == 0 {
		return nil
	}
	args := append([]string{"rmi", "-f"}, refs...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneDanglingImages(ctx context.Context) (int, error) {
	listCmd := exec.CommandContext(ctx, "docker", "images", "--filter", "dangling=true", "-q")
	out, err := listCmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker images dangling: %w\n%s", err, string(out))
	}
	count := countLines(string(out))
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
	if out, err := cmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return count, nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context) (int, error) {
	listCmd := exec.CommandContext(ctx, "docker", "volume", "ls", "--filter", "dangling=true", "-q")
	out, err := listCmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	count := countLines(string(out))
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	if out, err := cmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return count, nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context) (int, error) {
	listCmd := exec.CommandContext(ctx, "docker", "network", "ls", "--filter", "dangling=true", "-q")
	out, err := listCmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	count := countLines(string(out))
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	if out, err := cmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return count, nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}

func countLines(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestCountLines|TestDockerRuntimeSatisfiesCleaner|TestNewCleanerRequiresDocker' -count=1 -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Run the full runtime test suite and commit**

```bash
go test ./internal/runtime/ -count=1
```

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add Cleaner interface and docker exec methods for cleanup"
```

---

### Task 4: CLI cleanup orchestration (`runCleanup` + helpers)

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Cleaner`, `runtime.ContainerEntry`, `runtime.ImageEntry`, `runtime.CleanupReport`, `runtime.SelectStaleContainers`, `runtime.ContainerNames`, `runtime.SelectImagesToRemove`, `runtime.ImageRefs`, `runtime.ContainerName` (all from Tasks 1-3); `config.Store`, `config.NewStoreWithEnv`; `types.AppEntry`, `types.PreviewEntry`
- Produces:
  - `type cleanupOpts struct { Env string; Force bool; KeepImages int; WithVolumes bool; WithNetworks bool; WithCache bool }`
  - `runCleanup(ctx context.Context, cleaner runtime.Cleaner, store *config.Store, opts cleanupOpts) (*runtime.CleanupReport, error)` — builds the active set, computes stale containers/images, removes only when `opts.Force`, optionally prunes volumes/networks/cache
  - `activeContainerNames(store *config.Store, env string) map[string]bool` — active versioned/simple app containers + preview containers
  - `inUseImageTags(store *config.Store) map[string]bool` — app image tags, all deployment-history image tags, preview image tags

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type mockCleaner struct {
	containers        []runtime.ContainerEntry
	images            []runtime.ImageEntry
	dangling          int
	volumes           int
	networks          int
	removedContainers []string
	removedImages     []string
	cacheCleaned      bool
}

func (m *mockCleaner) ListTengizContainers(ctx context.Context) ([]runtime.ContainerEntry, error) {
	return m.containers, nil
}

func (m *mockCleaner) ListTengizImages(ctx context.Context) ([]runtime.ImageEntry, error) {
	return m.images, nil
}

func (m *mockCleaner) RemoveContainers(ctx context.Context, names []string) error {
	m.removedContainers = append(m.removedContainers, names...)
	return nil
}

func (m *mockCleaner) RemoveImages(ctx context.Context, refs []string) error {
	m.removedImages = append(m.removedImages, refs...)
	return nil
}

func (m *mockCleaner) PruneDanglingImages(ctx context.Context) (int, error) {
	return m.dangling, nil
}

func (m *mockCleaner) PruneVolumes(ctx context.Context) (int, error) {
	return m.volumes, nil
}

func (m *mockCleaner) PruneNetworks(ctx context.Context) (int, error) {
	return m.networks, nil
}

func (m *mockCleaner) PruneBuildCache(ctx context.Context) error {
	m.cacheCleaned = true
	return nil
}

func TestMockCleanerSatisfiesCleaner(t *testing.T) {
	var _ runtime.Cleaner = &mockCleaner{}
}

func newCleanupStore(t *testing.T) *config.Store {
	t.Helper()
	s := config.NewStore(t.TempDir())
	if err := s.SaveApp(types.AppEntry{
		Name:             "myapp",
		ImageTag:         "tengiz-apps/myapp:production-1712349000",
		DeploymentSuffix: "1712349000",
		Config:           types.AppConfig{Name: "myapp", Environment: "production"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDeployment("myapp", types.DeploymentEntry{
		ID: "1712349000", ImageTag: "tengiz-apps/myapp:production-1712349000", Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunCleanupDryRun(t *testing.T) {
	store := newCleanupStore(t)
	mc := &mockCleaner{
		containers: []runtime.ContainerEntry{
			{Name: "tengiz-myapp-1712345000", State: "exited", Labels: map[string]string{"tengiz-app": "myapp", "tengiz-env": "production"}},
		},
		images: []runtime.ImageEntry{
			{Repository: "tengiz-apps/myapp", Tag: "production-1712340000", CreatedAt: "2026-08-17 10:00:00 +0000 UTC"},
		},
	}
	report, err := runCleanup(context.Background(), mc, store, cleanupOpts{Env: "production"})
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if !report.DryRun {
		t.Error("expected DryRun = true")
	}
	if len(mc.removedContainers) != 0 {
		t.Errorf("removedContainers = %v, want none in dry run", mc.removedContainers)
	}
	if len(mc.removedImages) != 0 {
		t.Errorf("removedImages = %v, want none in dry run", mc.removedImages)
	}
	if len(report.ContainersRemoved) != 1 || report.ContainersRemoved[0] != "tengiz-myapp-1712345000" {
		t.Errorf("report.ContainersRemoved = %v", report.ContainersRemoved)
	}
	if len(report.ImagesRemoved) != 1 || report.ImagesRemoved[0] != "tengiz-apps/myapp:production-1712340000" {
		t.Errorf("report.ImagesRemoved = %v", report.ImagesRemoved)
	}
}

func TestRunCleanupForceRemovesStale(t *testing.T) {
	store := newCleanupStore(t)
	mc := &mockCleaner{
		containers: []runtime.ContainerEntry{
			{Name: "tengiz-myapp-1712345000", State: "exited", Labels: map[string]string{"tengiz-app": "myapp", "tengiz-env": "production"}},
			{Name: "tengiz-myapp-1712349000", State: "running", Labels: map[string]string{"tengiz-app": "myapp", "tengiz-env": "production"}},
		},
		images: []runtime.ImageEntry{
			{Repository: "tengiz-apps/myapp", Tag: "production-1712340000", CreatedAt: "2026-08-17 10:00:00 +0000 UTC"},
			{Repository: "tengiz-apps/myapp", Tag: "production-1712345000", CreatedAt: "2026-08-18 10:00:00 +0000 UTC"},
		},
	}
	report, err := runCleanup(context.Background(), mc, store, cleanupOpts{Env: "production", Force: true, KeepImages: 1})
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if report.DryRun {
		t.Error("expected DryRun = false")
	}
	if len(mc.removedContainers) != 1 || mc.removedContainers[0] != "tengiz-myapp-1712345000" {
		t.Errorf("removedContainers = %v, want [tengiz-myapp-1712345000]", mc.removedContainers)
	}
	if len(mc.removedImages) != 1 || mc.removedImages[0] != "tengiz-apps/myapp:production-1712340000" {
		t.Errorf("removedImages = %v, want [tengiz-apps/myapp:production-1712340000]", mc.removedImages)
	}
}

func TestRunCleanupProtectsActiveContainer(t *testing.T) {
	store := newCleanupStore(t)
	mc := &mockCleaner{
		containers: []runtime.ContainerEntry{
			{Name: "tengiz-myapp-1712349000", State: "exited", Labels: map[string]string{"tengiz-app": "myapp", "tengiz-env": "production"}},
		},
	}
	_, err := runCleanup(context.Background(), mc, store, cleanupOpts{Env: "production", Force: true})
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if len(mc.removedContainers) != 0 {
		t.Errorf("removedContainers = %v, want none (active container protected)", mc.removedContainers)
	}
}

func TestRunCleanupProtectsPreviewContainer(t *testing.T) {
	store := config.NewStore(t.TempDir())
	if err := store.AddPreview(types.PreviewEntry{AppName: "myapp", PRNumber: 42, ContainerName: "tengiz-myapp-pr-42"}); err != nil {
		t.Fatal(err)
	}
	mc := &mockCleaner{
		containers: []runtime.ContainerEntry{
			{Name: "tengiz-myapp-pr-42", State: "exited", Labels: map[string]string{"tengiz-app": "myapp", "tengiz-env": ""}},
		},
	}
	_, err := runCleanup(context.Background(), mc, store, cleanupOpts{Env: "production", Force: true})
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if len(mc.removedContainers) != 0 {
		t.Errorf("removedContainers = %v, want none (preview container protected)", mc.removedContainers)
	}
}

func TestRunCleanupEnvScoping(t *testing.T) {
	store := newCleanupStore(t)
	mc := &mockCleaner{
		containers: []runtime.ContainerEntry{
			{Name: "tengiz-myapp-1712345000", State: "exited", Labels: map[string]string{"tengiz-app": "myapp", "tengiz-env": "staging"}},
		},
	}
	_, err := runCleanup(context.Background(), mc, store, cleanupOpts{Env: "production", Force: true})
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if len(mc.removedContainers) != 0 {
		t.Errorf("removedContainers = %v, want none (different env)", mc.removedContainers)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestMockCleanerSatisfiesCleaner|TestRunCleanup' -count=1 -v`
Expected: FAIL — compile error `undefined: runCleanup` / `undefined: cleanupOpts`

- [ ] **Step 3: Write the implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"context"
	"fmt"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

type cleanupOpts struct {
	Env          string
	Force        bool
	KeepImages   int
	WithVolumes  bool
	WithNetworks bool
	WithCache    bool
}

func runCleanup(ctx context.Context, cleaner runtime.Cleaner, store *config.Store, opts cleanupOpts) (*runtime.CleanupReport, error) {
	report := &runtime.CleanupReport{DryRun: !opts.Force}

	active := activeContainerNames(store, opts.Env)

	containers, err := cleaner.ListTengizContainers(ctx)
	if err != nil {
		return report, fmt.Errorf("list containers: %w", err)
	}
	stale := runtime.SelectStaleContainers(containers, active, opts.Env)
	report.ContainersRemoved = runtime.ContainerNames(stale)
	if opts.Force && len(stale) > 0 {
		if err := cleaner.RemoveContainers(ctx, report.ContainersRemoved); err != nil {
			return report, fmt.Errorf("remove containers: %w", err)
		}
	}

	images, err := cleaner.ListTengizImages(ctx)
	if err != nil {
		return report, fmt.Errorf("list images: %w", err)
	}
	toRemove := runtime.SelectImagesToRemove(images, opts.KeepImages, inUseImageTags(store))
	report.ImagesRemoved = runtime.ImageRefs(toRemove)
	if opts.Force && len(toRemove) > 0 {
		if err := cleaner.RemoveImages(ctx, report.ImagesRemoved); err != nil {
			return report, fmt.Errorf("remove images: %w", err)
		}
	}

	if opts.Force {
		dangling, err := cleaner.PruneDanglingImages(ctx)
		if err != nil {
			return report, fmt.Errorf("prune dangling images: %w", err)
		}
		report.DanglingImages = dangling

		if opts.WithVolumes {
			vols, err := cleaner.PruneVolumes(ctx)
			if err != nil {
				return report, fmt.Errorf("prune volumes: %w", err)
			}
			report.Volumes = vols
		}
		if opts.WithNetworks {
			nets, err := cleaner.PruneNetworks(ctx)
			if err != nil {
				return report, fmt.Errorf("prune networks: %w", err)
			}
			report.Networks = nets
		}
		if opts.WithCache {
			if err := cleaner.PruneBuildCache(ctx); err != nil {
				return report, fmt.Errorf("prune build cache: %w", err)
			}
			report.CacheCleaned = true
		}
	}

	return report, nil
}

func activeContainerNames(store *config.Store, env string) map[string]bool {
	active := make(map[string]bool)
	apps, err := store.ListApps()
	if err == nil {
		for _, app := range apps {
			cn := runtime.ContainerName(app.Name, env)
			if app.DeploymentSuffix != "" {
				active[fmt.Sprintf("%s-%s", cn, app.DeploymentSuffix)] = true
			} else {
				active[cn] = true
			}
		}
	}
	previews, err := store.ListAllPreviews()
	if err == nil {
		for _, pv := range previews {
			active[fmt.Sprintf("tengiz-%s-pr-%d", pv.AppName, pv.PRNumber)] = true
		}
	}
	return active
}

func inUseImageTags(store *config.Store) map[string]bool {
	inUse := make(map[string]bool)
	apps, err := store.ListApps()
	if err == nil {
		for _, app := range apps {
			if app.ImageTag != "" {
				inUse[app.ImageTag] = true
			}
			if deps, err := store.GetDeployments(app.Name); err == nil {
				for _, d := range deps {
					if d.ImageTag != "" {
						inUse[d.ImageTag] = true
					}
				}
			}
		}
	}
	previews, err := store.ListAllPreviews()
	if err == nil {
		for _, pv := range previews {
			if pv.ImageTag != "" {
				inUse[pv.ImageTag] = true
			}
		}
	}
	return inUse
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestMockCleanerSatisfiesCleaner|TestRunCleanup' -count=1 -v`
Expected: PASS (6 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add cleanup orchestration that protects active deployments"
```

---

### Task 5: `cleanup` cobra command wiring and output

**Files:**
- Modify: `internal/cli/cleanup.go` (add `cleanupCmd` + `printCleanupReport`)
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runCleanup`, `cleanupOpts` (Task 4); `getEnv(cmd)` (existing in `internal/cli/root.go:97`); `dataDir` (package var in `internal/cli/root.go:32`); `runtime.NewCleaner`, `runtime.CleanupReport` (Task 3); `config.NewStoreWithEnv`
- Produces:
  - `var cleanupCmd = &cobra.Command{...}` — `Use: "cleanup"`, `Short: "Remove stale containers, old images, and unused Docker resources"`, `RunE` wiring
  - `func init()` in `internal/cli/cleanup.go` that registers `--force`/`-f`, `--keep`, `--volumes`, `--networks`, `--cache` flags and calls `rootCmd.AddCommand(cleanupCmd)`
  - `printCleanupReport(r *runtime.CleanupReport)` — human-readable summary to stdout

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/cleanup_test.go`:

```go
func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"force", "keep", "volumes", "networks", "cache"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func TestPrintCleanupReportDryRun(t *testing.T) {
	out := captureOutput(func() {
		printCleanupReport(&runtime.CleanupReport{
			DryRun:            true,
			ContainersRemoved: []string{"tengiz-myapp-1712345000"},
			ImagesRemoved:     []string{"tengiz-apps/myapp:production-1712340000"},
		})
	})
	for _, want := range []string{"dry run", "tengiz-myapp-1712345000", "tengiz-apps/myapp:production-1712340000", "--force"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintCleanupReportForce(t *testing.T) {
	out := captureOutput(func() {
		printCleanupReport(&runtime.CleanupReport{
			DryRun:         false,
			DanglingImages: 3,
			Volumes:        2,
			Networks:       1,
			CacheCleaned:   true,
		})
	})
	for _, want := range []string{"dangling images pruned: 3", "volumes pruned: 2", "networks pruned: 1", "build cache pruned"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
```

`captureOutput` and `strings` are already available in the `cli` test package (`captureOutput` is defined in `internal/cli/root_test.go:57`; add `"strings"` to the imports of `cleanup_test.go`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanupCmdRegistered|TestPrintCleanupReport' -count=1 -v`
Expected: FAIL — compile error `undefined: cleanupCmd` / `undefined: printCleanupReport`

- [ ] **Step 3: Write the implementation**

Append to `internal/cli/cleanup.go` (keeping the Task 4 code):

```go
import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove stale containers, old images, and unused Docker resources",
	Long: `Remove resources that are no longer in use:
- stopped, non-active Tengiz containers (old deployment versions)
- old tengiz-apps images beyond the retention limit (--keep N, default 5)
- dangling images

By default this is a dry run that only shows what would be removed.
Pass --force to actually remove. Add --volumes, --networks, or --cache
to also prune unused Docker volumes, networks, or the build cache
(these are Docker-wide, not limited to Tengiz-managed resources).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		force, _ := cmd.Flags().GetBool("force")
		keep, _ := cmd.Flags().GetInt("keep")
		withVolumes, _ := cmd.Flags().GetBool("volumes")
		withNetworks, _ := cmd.Flags().GetBool("networks")
		withCache, _ := cmd.Flags().GetBool("cache")

		cleaner, err := runtime.NewCleaner()
		if err != nil {
			return err
		}
		store := config.NewStoreWithEnv(dataDir, env)

		report, err := runCleanup(cmd.Context(), cleaner, store, cleanupOpts{
			Env:          env,
			Force:        force,
			KeepImages:   keep,
			WithVolumes:  withVolumes,
			WithNetworks: withNetworks,
			WithCache:    withCache,
		})
		if err != nil {
			return err
		}
		printCleanupReport(report)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().BoolP("force", "f", false, "actually remove resources (default is a dry run)")
	cleanupCmd.Flags().Int("keep", 5, "number of images to keep per app")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused Docker volumes")
	cleanupCmd.Flags().Bool("networks", false, "also prune unused Docker networks")
	cleanupCmd.Flags().Bool("cache", false, "also prune the Docker build cache")
	rootCmd.AddCommand(cleanupCmd)
}

func printCleanupReport(r *runtime.CleanupReport) {
	if r.DryRun {
		fmt.Println("[tengiz] cleanup: dry run (pass --force to apply)")
	} else {
		fmt.Println("[tengiz] cleanup:")
	}
	if len(r.ContainersRemoved) == 0 {
		fmt.Println("  containers: none")
	} else {
		fmt.Printf("  containers: %s\n", strings.Join(r.ContainersRemoved, ", "))
	}
	if len(r.ImagesRemoved) == 0 {
		fmt.Println("  images: none")
	} else {
		fmt.Printf("  images: %s\n", strings.Join(r.ImagesRemoved, ", "))
	}
	if r.DryRun {
		fmt.Println("  dangling images, unused volumes/networks, and build cache would also be pruned with --force")
		return
	}
	fmt.Printf("  dangling images pruned: %d\n", r.DanglingImages)
	fmt.Printf("  volumes pruned: %d\n", r.Volumes)
	fmt.Printf("  networks pruned: %d\n", r.Networks)
	if r.CacheCleaned {
		fmt.Println("  build cache pruned")
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestCleanupCmdRegistered|TestPrintCleanupReport' -count=1 -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 6: Documentation and feature status update

**Files:**
- Modify: `README.md` (add `tengiz cleanup` CLI reference section after the `tengiz rollback` section, i.e. after line 236, before `### tengiz domain` at line 238)
- Modify: `docs/FUTURES_FEATURES.md` (mark priority-table row #6 and the detail section as implemented)

- [ ] **Step 1: Document the command in README.md**

Insert the following section into `README.md` between the `tengiz rollback` section and the `### tengiz domain` section:

```markdown
### `tengiz cleanup [--force] [--keep N] [--volumes] [--networks] [--cache]`

Remove stale containers, old images, and unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `-f`, `--force` | Actually remove resources. Default is a dry run that only shows what would be removed. |
| `--keep N` | Number of images to keep per app (default: 5) |
| `--volumes` | Also prune unused Docker volumes (Docker-wide, not Tengiz-scoped) |
| `--networks` | Also prune unused Docker networks (Docker-wide, not Tengiz-scoped) |
| `--cache` | Also prune the Docker build cache (Docker-wide, not Tengiz-scoped) |

By default, `tengiz cleanup` prints a summary of what it *would* remove. Run it first to inspect, then pass `--force` to apply.

Always removed:
- **Stopped, non-active containers** — old deployment versions that are no longer the active container for any app or preview in the current environment (identified by the `tengiz-app` label). Active containers are protected, even when stopped by scale-to-zero.
- **Old `tengiz-apps/*` images** beyond the retention limit (`--keep`, default 5 per app). The `-latest` tag and any image referenced by the current app, deployment history, or a preview are always protected.
- **Dangling images** — untagged layers left over from interrupted or partial builds.

Opt-in (prune all unused Docker resources, including non-Tengiz ones): `--volumes`, `--networks`, `--cache`.

Example:
```bash
tengiz cleanup                      # dry run — shows what would be removed
tengiz cleanup --force              # actually remove stale containers and images
tengiz cleanup --force --volumes --cache   # also prune volumes and build cache
```
```

- [ ] **Step 2: Update the priority table and detail section in docs/FUTURES_FEATURES.md**

Edit the row at `docs/FUTURES_FEATURES.md:19` — change the ⬜ marker to ✅ and append the date:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. Implemented (2026-08-19). |
```

Edit the detail section starting at `docs/FUTURES_FEATURES.md:377` — add a status line after the "Why add to Tengiz" line:

```
## Docker Housekeeping (Otomatik Temizlik)
- **Source:** Coolify
- **Description:** `DockerCleanupJob` ile kullanılmayan volume, network, container ve image'leri periyodik temizleme. `CleanupHelperContainersJob` ile yardımcı container'ları temizler.
- **Why add to Tengiz:** Sürekli deploy ve scale-to-zero ortamında atık container/image'ler disk alanını tüketir. Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur. `tengiz cleanup` komutu eklenebilir.
- **Status:** ✅ Implemented (2026-08-19)
- **Detected:** 2026-07-14
```

- [ ] **Step 3: Full verification**

Run:

```bash
go build -o tengiz .
go vet ./...
go test ./... -count=1
```

Expected: build succeeds, `go vet` reports no issues, all tests pass (including the new `internal/runtime` and `internal/cli` tests).

- [ ] **Step 4: Verify the CLI manually if Docker is available**

If `docker` is installed on the machine, verify end-to-end:

```bash
./tengiz cleanup            # dry run — prints summary
./tengiz cleanup --force    # applies removals
./tengiz cleanup --help     # shows flag documentation
```

If docker is not installed, `./tengiz cleanup` must fail with `docker not found in PATH` — this is the expected behavior in that environment.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage (FUTURES_FEATURES.md #6 — "Docker Housekeeping (Otomatik Temizlik)"):**
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → Tasks 3-5 (`--volumes`, `--networks`, stale containers, old images)
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → Task 2 `SelectStaleContainers`/`containerEnvMatches` + Task 4 `activeContainerNames` (active containers protected even when stopped)
- "`tengiz cleanup` komutu" → Task 5 `cleanupCmd`
- Related P1 items folded in: #22 Container Retention Policy (`--keep`, default 5), #47 Stale Container Detection (stopped non-active containers), #56 Granular Docker Prune (per-category flags), #103 Build Cache (`--cache`)

**2. Placeholder scan:** No "TBD"/"TODO"/"add appropriate handling" patterns — every step contains complete compilable code and exact commands with expected output.

**3. Type consistency:**
- `ContainerEntry.Name` is computed in `ParseContainerList` and consumed by `SelectStaleContainers`/`ContainerNames` — consistent naming (`tengiz-<app>` / `tengiz-<app>-<env>[-<suffix>]` / `tengiz-<app>-pr-<n>`).
- `ContainerNames`/`ImageRefs` are exported (used from package `cli`); `containerEnvMatches`, `parseContainerName`, `countLines` are unexported (same package).
- `runCleanup` returns `*runtime.CleanupReport` with fields exactly matching `printCleanupReport` usage (`DryRun`, `ContainersRemoved`, `ImagesRemoved`, `DanglingImages`, `Volumes`, `Networks`, `CacheCleaned`).
- Image ref format is always `repository:tag`, matching `inUseImageTags` keys built from store `ImageTag` values.
- `cleanupOpts` field names used in `runCleanup` match the ones set in `cleanupCmd.RunE`.