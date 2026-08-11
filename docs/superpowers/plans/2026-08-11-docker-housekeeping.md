# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that removes unused Docker resources (foreign stopped containers, dangling images, unused custom networks, build cache, and optionally unused volumes) while always protecting Tengiz-managed containers, images, and deployment history from deletion.

**Architecture:** Three layers mirror the existing codebase. `internal/runtime` gains Docker exec primitives (`ListContainers`, `ListImages`, `ListVolumes`, `ListNetworks`, `PruneBuildCache`, `RemoveVolume`, `RemoveNetwork`) plus info structs, following the existing `exec.CommandContext("docker", ...)` pattern in `docker.go`. A new `internal/cleanup` package holds the decision engine: pure, fully unit-testable candidate-selection functions plus a `Cleaner` that plans (lists candidates) and prunes (removes them). `internal/cli` adds the cobra command with `--dry-run`, `--yes`, `--volumes`, `--all` flags, a confirmation prompt, and a summary report. Tengiz apps are protected by the `tengiz-app` label they already receive at container creation; images are protected by container references (image IDs) and by deployment history recorded in `~/.tengiz/*.json` stores.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (no Docker SDK — consistent with the rest of the repo), existing `config.Store` for state. No new external dependencies.

## Global Constraints

- No new external dependencies
- Existing tests must continue to pass without modification except the three mock `runtime.Manager` implementations (which must gain the 5 new interface methods to keep compiling)
- Containers labeled `tengiz-app` (regular apps, versioned containers, preview deployments) are **always** protected, running or stopped — scale-to-zero stopped apps must never be pruned
- Images referenced by any container (by image ID) or by any record in `apps*.json`, `deployments*.json`, `previews*.json` are **never** removed
- Volumes are removed only when `--volumes` is passed; even then only volumes with refcount 0 (not mounted by any container)
- Default behavior removes: stopped non-Tengiz containers, dangling images (`<none>:<none>`), unused custom networks, and Docker build cache
- Built-in Docker networks (`bridge`, `host`, `none`) are never removed; custom networks in use by any container are never removed
- `--dry-run` lists candidates and never removes anything
- Docker commands use the existing `exec.CommandContext(ctx, "docker", args...)` pattern from `internal/runtime/docker.go`
- The new CLI command file follows the `internal/cli/preview.go` pattern: package-level command vars + its own `func init()` registration
- Keep code comment-free (repo convention — existing files have no comments)
- Image reference format is `tengiz-apps/<name>:<env>-<deploymentID>` (regular) and `tengiz-apps/<name>:pr-<n>-<deploymentID>` (preview)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` (new) | Docker exec primitives + `ContainerInfo`/`ImageInfo`/`VolumeInfo`/`NetworkInfo` structs + `AppLabel` const + `parseLabels`/`ImageRef` helpers |
| `internal/runtime/runtime.go` (modify) | Add 7 methods to `Manager` interface (lines 31-49) + stub implementations (after line 119) |
| `internal/runtime/docker.go` (modify) | Line 76: `const labelKey = "tengiz-app"` → `const labelKey = AppLabel` (DRY) |
| `internal/runtime/prune_test.go` (new) | Tests for `parseLabels`, `ImageRef`, and new stub methods |
| `internal/cli/root_test.go` (modify) | Add 7 methods to `mockRTForDeploy` (5 in Task 1, 2 in Task 5) |
| `internal/idle/idle_test.go` (modify) | Add 7 methods to `mockRuntime` (5 in Task 1, 2 in Task 5) |
| `internal/proxy/proxy_test.go` (modify) | Add 7 methods to `mockRuntime` (5 in Task 1, 2 in Task 5) |
| `internal/cleanup/cleanup.go` (new package dir) | `Options`/`Result` types, pure decision functions, `referencedImageRefs`, `Cleaner` with `Plan`/`Prune`, `PruneRuntime` interface |
| `internal/cleanup/cleanup_test.go` (new) | Unit tests for decision functions + `Plan`/`Prune` orchestration with a mock runtime |
| `internal/cli/cleanup.go` (new) | `cleanupCmd` cobra command, `runCleanup`, `confirmCleanup`, `printPlan`, `printResult`, `init()` registration |
| `internal/cli/cleanup_test.go` (new) | Command registration, flags, `runCleanup` dry-run/yes paths, `confirmCleanup` |
| `README.md` (modify) | Add `tengiz cleanup` CLI Reference section + Commands table row |
| `AGENTS.md` (modify) | Add `tengiz cleanup` to the CLI command list |

---

### Task 1: Runtime prune primitives

Add Docker exec primitives and info structs to the runtime package and wire them into the `Manager` interface and all mocks.

**Files:**
- Create: `internal/runtime/prune.go`
- Modify: `internal/runtime/runtime.go:31-49` (interface) and `internal/runtime/runtime.go:113-123` (stub)
- Modify: `internal/runtime/docker.go:76`
- Modify: `internal/cli/root_test.go:98-100`, `internal/idle/idle_test.go:18-34`, `internal/proxy/proxy_test.go:19-35`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.AppLabel string`, `runtime.ContainerInfo`, `runtime.ImageInfo`, `runtime.VolumeInfo`, `runtime.ImageRef(repository, tag string) string`, and `Manager` methods `ListContainers(ctx) ([]ContainerInfo, error)`, `ListImages(ctx) ([]ImageInfo, error)`, `ListVolumes(ctx) ([]VolumeInfo, error)`, `PruneBuildCache(ctx) error`, `RemoveVolume(ctx, name string) error`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/prune_test.go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestParseLabels(t *testing.T) {
	got := parseLabels("tengiz-app=myapp,tengiz-env=staging,foo=bar")
	want := map[string]string{
		"tengiz-app": "myapp",
		"tengiz-env": "staging",
		"foo":        "bar",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseLabels() = %v, want %v", got, want)
	}
}

func TestParseLabelsEmpty(t *testing.T) {
	got := parseLabels("")
	if len(got) != 0 {
		t.Errorf("parseLabels(\"\") = %v, want empty", got)
	}
}

func TestImageRef(t *testing.T) {
	tests := []struct {
		repo, tag, want string
	}{
		{"tengiz-apps/myapp", "production-v1", "tengiz-apps/myapp:production-v1"},
		{"tengiz-apps/myapp", "<none>", "tengiz-apps/myapp"},
		{"<none>", "<none>", "<none>"},
		{"alpine", "latest", "alpine:latest"},
	}
	for _, tt := range tests {
		if got := ImageRef(tt.repo, tt.tag); got != tt.want {
			t.Errorf("ImageRef(%q, %q) = %q, want %q", tt.repo, tt.tag, got, tt.want)
		}
	}
}

func TestStubPruneMethods(t *testing.T) {
	m := NewStub()
	if _, err := m.ListContainers(context.Background()); err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if _, err := m.ListImages(context.Background()); err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	if _, err := m.ListVolumes(context.Background()); err != nil {
		t.Fatalf("ListVolumes: %v", err)
	}
	if err := m.PruneBuildCache(context.Background()); err != nil {
		t.Fatalf("PruneBuildCache: %v", err)
	}
	if err := m.RemoveVolume(context.Background(), "myvol"); err != nil {
		t.Fatalf("RemoveVolume: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestParseLabels|TestImageRef|TestStubPruneMethods" -v -count=1`

Expected: FAIL with `undefined: parseLabels`, `undefined: ImageRef`, `undefined: ListContainers`

- [ ] **Step 3: Add the structs, const, and helpers to `internal/runtime/prune.go`**

```go
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const AppLabel = "tengiz-app"

type ContainerInfo struct {
	ID     string
	Name   string
	Image  string
	State  string
	Labels map[string]string
}

type ImageInfo struct {
	ID         string
	Repository string
	Tag        string
	Size       int64
}

type VolumeInfo struct {
	Name  string
	InUse bool
}

func parseLabels(labels string) map[string]string {
	result := make(map[string]string)
	if labels == "" {
		return result
	}
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}
	return result
}

func ImageRef(repository, tag string) string {
	if tag == "" || tag == "<none>" {
		return repository
	}
	return repository + ":" + tag
}
```

- [ ] **Step 4: Add the docker exec methods to `internal/runtime/prune.go`** (append to the same file)

```go
func (r *dockerRuntime) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	var infos []ContainerInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var raw struct {
			ID     string `json:"ID"`
			Names  string `json:"Names"`
			Image  string `json:"Image"`
			State  string `json:"State"`
			Labels string `json:"Labels"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		infos = append(infos, ContainerInfo{
			ID:     raw.ID,
			Name:   strings.TrimPrefix(raw.Names, "/"),
			Image:  raw.Image,
			State:  raw.State,
			Labels: parseLabels(raw.Labels),
		})
	}
	return infos, nil
}

func (r *dockerRuntime) ListImages(ctx context.Context) ([]ImageInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	var infos []ImageInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var raw struct {
			ID         string `json:"ID"`
			Repository string `json:"Repository"`
			Tag        string `json:"Tag"`
			Size       int64  `json:"Size"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		infos = append(infos, ImageInfo{
			ID:         raw.ID,
			Repository: raw.Repository,
			Tag:        raw.Tag,
			Size:       raw.Size,
		})
	}
	return infos, nil
}

func (r *dockerRuntime) ListVolumes(ctx context.Context) ([]VolumeInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	var infos []VolumeInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var raw struct {
			Name string `json:"Name"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		infos = append(infos, VolumeInfo{Name: raw.Name, InUse: r.volumeInUse(ctx, raw.Name)})
	}
	return infos, nil
}

func (r *dockerRuntime) volumeInUse(ctx context.Context, name string) bool {
	cmd := exec.CommandContext(ctx, "docker", "volume", "inspect", "--format", "{{.UsageData.RefCount}}", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	ref, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return false
	}
	return ref > 0
}

func (r *dockerRuntime) RemoveVolume(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "volume", "rm", "-f", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}
```

- [ ] **Step 5: Add the 5 methods to the `Manager` interface in `internal/runtime/runtime.go`**

Insert after `KeepLastNImages(ctx context.Context, appName string, n int) error` (line 36):

```go
	ListContainers(ctx context.Context) ([]ContainerInfo, error)
	ListImages(ctx context.Context) ([]ImageInfo, error)
	ListVolumes(ctx context.Context) ([]VolumeInfo, error)
	PruneBuildCache(ctx context.Context) error
	RemoveVolume(ctx context.Context, name string) error
```

- [ ] **Step 6: Add the stub implementations in `internal/runtime/runtime.go`**

Insert after the `KeepLastNImages` stub (line 117-119):

```go
func (m *stubManager) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	return nil, nil
}

func (m *stubManager) ListImages(ctx context.Context) ([]ImageInfo, error) {
	return nil, nil
}

func (m *stubManager) ListVolumes(ctx context.Context) ([]VolumeInfo, error) {
	return nil, nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context) error {
	return nil
}

func (m *stubManager) RemoveVolume(ctx context.Context, name string) error {
	return nil
}
```

- [ ] **Step 7: Update `internal/runtime/docker.go:76` to use `AppLabel`**

```go
const labelKey = AppLabel
```

- [ ] **Step 8: Update the three mock `runtime.Manager` implementations**

`internal/cli/root_test.go` — add after line 99 (`KeepLastNImages`):

```go
func (m *mockRTForDeploy) ListContainers(ctx context.Context) ([]runtime.ContainerInfo, error) { return nil, nil }
func (m *mockRTForDeploy) ListImages(ctx context.Context) ([]runtime.ImageInfo, error) { return nil, nil }
func (m *mockRTForDeploy) ListVolumes(ctx context.Context) ([]runtime.VolumeInfo, error) { return nil, nil }
func (m *mockRTForDeploy) PruneBuildCache(ctx context.Context) error { return nil }
func (m *mockRTForDeploy) RemoveVolume(ctx context.Context, name string) error { return nil }
```

`internal/idle/idle_test.go` — add after line 34 (`Run`):

```go
func (m *mockRuntime) ListContainers(ctx context.Context) ([]runtime.ContainerInfo, error) { return nil, nil }
func (m *mockRuntime) ListImages(ctx context.Context) ([]runtime.ImageInfo, error) { return nil, nil }
func (m *mockRuntime) ListVolumes(ctx context.Context) ([]runtime.VolumeInfo, error) { return nil, nil }
func (m *mockRuntime) PruneBuildCache(ctx context.Context) error { return nil }
func (m *mockRuntime) RemoveVolume(ctx context.Context, name string) error { return nil }
```

`internal/proxy/proxy_test.go` — add after line 35 (`Run`):

```go
func (m *mockRuntime) ListContainers(ctx context.Context) ([]runtime.ContainerInfo, error) { return nil, nil }
func (m *mockRuntime) ListImages(ctx context.Context) ([]runtime.ImageInfo, error) { return nil, nil }
func (m *mockRuntime) ListVolumes(ctx context.Context) ([]runtime.VolumeInfo, error) { return nil, nil }
func (m *mockRuntime) PruneBuildCache(ctx context.Context) error { return nil }
func (m *mockRuntime) RemoveVolume(ctx context.Context, name string) error { return nil }
```

- [ ] **Step 9: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestParseLabels|TestImageRef|TestStubPruneMethods" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds (proves all 3 mocks still satisfy `Manager`)

- [ ] **Step 10: Run all runtime + affected package tests**

Run: `go test ./internal/runtime/ ./internal/idle/ ./internal/proxy/ ./internal/cli/ -count=1`

Expected: All PASS

- [ ] **Step 11: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go internal/runtime/runtime.go internal/runtime/docker.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add docker prune primitives for housekeeping"
```

---

### Task 2: Cleanup candidate selection (pure decision functions)

Create the `internal/cleanup` package with the pure, fully unit-testable functions that decide *what* is safe to remove.

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.AppLabel`, `runtime.ContainerInfo`, `runtime.ImageInfo`, `runtime.VolumeInfo`, `runtime.ImageRef`, `types.DeploymentEntry`, `types.AppEntry`, `types.PreviewEntry` (all from Task 1 / existing code)
- Produces: `cleanup.Options{DryRun, Yes, Volumes, AllImages bool}`, `cleanup.Result{ContainersRemoved, ImagesRemoved, VolumesRemoved []string; BuildCache bool; Errors []string}`, and pure functions `containerCandidates([]runtime.ContainerInfo) []runtime.ContainerInfo`, `imageCandidates([]runtime.ImageInfo, protectedIDs, protectedRefs map[string]bool, all bool) []runtime.ImageInfo`, `volumeCandidates([]runtime.VolumeInfo) []runtime.VolumeInfo`, `referencedImageRefs(dataDir string) (map[string]bool, error)`, `containerNames`, `volumeNames`, `imageTargets`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cleanup/cleanup_test.go
package cleanup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

func TestContainerCandidates(t *testing.T) {
	containers := []runtime.ContainerInfo{
		{Name: "foreign-stopped", State: "exited", Labels: map[string]string{"foo": "bar"}},
		{Name: "tengiz-myapp", State: "exited", Labels: map[string]string{runtime.AppLabel: "myapp"}},
		{Name: "tengiz-myapp-1700000000", State: "exited", Labels: map[string]string{runtime.AppLabel: "myapp", "tengiz-deployment": "1700000000"}},
		{Name: "foreign-running", State: "running", Labels: map[string]string{}},
		{Name: "tengiz-pr", State: "exited", Labels: map[string]string{runtime.AppLabel: "myapp"}},
	}
	got := containerCandidates(containers)
	if len(got) != 1 {
		t.Fatalf("containerCandidates() = %d candidates, want 1", len(got))
	}
	if got[0].Name != "foreign-stopped" {
		t.Errorf("candidate name = %q, want %q", got[0].Name, "foreign-stopped")
	}
}

func TestImageCandidatesDefaultOnlyDangling(t *testing.T) {
	images := []runtime.ImageInfo{
		{ID: "sha256:aaa", Repository: "<none>", Tag: "<none>"},
		{ID: "sha256:bbb", Repository: "tengiz-apps/myapp", Tag: "production-v1"},
		{ID: "sha256:ccc", Repository: "alpine", Tag: "latest"},
	}
	got := imageCandidates(images, map[string]bool{}, map[string]bool{}, false)
	if len(got) != 1 {
		t.Fatalf("default imageCandidates() = %d, want 1 (dangling only)", len(got))
	}
	if got[0].ID != "sha256:aaa" {
		t.Errorf("candidate ID = %q, want %q", got[0].ID, "sha256:aaa")
	}
}

func TestImageCandidatesAllSkipsProtected(t *testing.T) {
	images := []runtime.ImageInfo{
		{ID: "sha256:aaa", Repository: "<none>", Tag: "<none>"},
		{ID: "sha256:bbb", Repository: "tengiz-apps/myapp", Tag: "production-v1"},
		{ID: "sha256:ccc", Repository: "tengiz-apps/myapp", Tag: "production-v2"},
		{ID: "sha256:ddd", Repository: "alpine", Tag: "3.19"},
	}
	protectedIDs := map[string]bool{"sha256:ccc": true}
	protectedRefs := map[string]bool{"tengiz-apps/myapp:production-v1": true}
	got := imageCandidates(images, protectedIDs, protectedRefs, true)
	wantIDs := map[string]bool{"sha256:aaa": true, "sha256:ddd": true}
	if len(got) != len(wantIDs) {
		t.Fatalf("imageCandidates() = %d candidates, want %d", len(got), len(wantIDs))
	}
	for _, img := range got {
		if !wantIDs[img.ID] {
			t.Errorf("unexpected candidate %q", img.ID)
		}
	}
}

func TestVolumeCandidates(t *testing.T) {
	volumes := []runtime.VolumeInfo{
		{Name: "freevol", InUse: false},
		{Name: "usedvol", InUse: true},
	}
	got := volumeCandidates(volumes)
	if len(got) != 1 || got[0].Name != "freevol" {
		t.Fatalf("volumeCandidates() = %+v, want only freevol", got)
	}
}

func TestImageTargetsUsesIDForDangling(t *testing.T) {
	images := []runtime.ImageInfo{
		{ID: "sha256:aaa", Repository: "<none>", Tag: "<none>"},
		{ID: "sha256:bbb", Repository: "tengiz-apps/myapp", Tag: "production-v1"},
	}
	got := imageTargets(images)
	want := []string{"sha256:aaa", "tengiz-apps/myapp:production-v1"}
	if len(got) != len(want) {
		t.Fatalf("imageTargets() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("imageTargets()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReferencedImageRefsFromStore(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)

	store.SaveApp(types.AppEntry{Name: "myapp", ImageTag: "tengiz-apps/myapp:production-v1"})
	store.AddDeployment("myapp", types.DeploymentEntry{
		ID:       "1700000000",
		ImageTag: "tengiz-apps/myapp:production-v1",
		Status:   string(types.DeployActive),
	})
	store.AddDeployment("myapp", types.DeploymentEntry{
		ID:       "1700000001",
		ImageTag: "tengiz-apps/myapp:production-v2",
		Status:   string(types.DeployPrevious),
	})
	store.AddDeployment("otherapp", types.DeploymentEntry{
		ID:       "1700000002",
		ImageTag: "tengiz-apps/otherapp:production-v3",
		Status:   string(types.DeployActive),
	})

	refs, err := referencedImageRefs(dir)
	if err != nil {
		t.Fatalf("referencedImageRefs: %v", err)
	}
	for _, want := range []string{
		"tengiz-apps/myapp:production-v1",
		"tengiz-apps/myapp:production-v2",
		"tengiz-apps/otherapp:production-v3",
	} {
		if !refs[want] {
			t.Errorf("refs missing %q (got %v)", want, refs)
		}
	}
}

func TestReferencedImageRefsEmptyDir(t *testing.T) {
	refs, err := referencedImageRefs(t.TempDir())
	if err != nil {
		t.Fatalf("referencedImageRefs: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("refs = %v, want empty", refs)
	}
}

func TestReferencedImageRefsScansStagingEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "deployments-staging.json"),
		[]byte(`{"myapp":[{"id":"1","image_tag":"tengiz-apps/myapp:staging-v1","port":9001,"created_at":"2026-08-11T00:00:00Z","status":"active"}]}`),
		0644); err != nil {
		t.Fatal(err)
	}
	refs, err := referencedImageRefs(dir)
	if err != nil {
		t.Fatalf("referencedImageRefs: %v", err)
	}
	if !refs["tengiz-apps/myapp:staging-v1"] {
		t.Errorf("refs missing staging image, got %v", refs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run "TestContainerCandidates|TestImageCandidates|TestVolumeCandidates|TestImageTargets|TestReferencedImageRefs" -v -count=1`

Expected: FAIL with `undefined: containerCandidates` (package does not compile — file missing)

- [ ] **Step 3: Implement the types and decision functions in `internal/cleanup/cleanup.go`**

```go
package cleanup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type Options struct {
	DryRun    bool
	Yes       bool
	Volumes   bool
	AllImages bool
}

type Result struct {
	ContainersRemoved []string
	ImagesRemoved     []string
	VolumesRemoved    []string
	BuildCache        bool
	Errors            []string
}

func containerCandidates(containers []runtime.ContainerInfo) []runtime.ContainerInfo {
	var candidates []runtime.ContainerInfo
	for _, c := range containers {
		if c.State == "running" || c.State == "restarting" || c.State == "paused" {
			continue
		}
		if _, isTengiz := c.Labels[runtime.AppLabel]; isTengiz {
			continue
		}
		candidates = append(candidates, c)
	}
	return candidates
}

func imageCandidates(images []runtime.ImageInfo, protectedIDs, protectedRefs map[string]bool, all bool) []runtime.ImageInfo {
	var candidates []runtime.ImageInfo
	for _, img := range images {
		if img.Repository == "<none>" || img.Tag == "<none>" {
			candidates = append(candidates, img)
			continue
		}
		if !all {
			continue
		}
		if protectedIDs[img.ID] {
			continue
		}
		if protectedRefs[runtime.ImageRef(img.Repository, img.Tag)] {
			continue
		}
		candidates = append(candidates, img)
	}
	return candidates
}

func volumeCandidates(volumes []runtime.VolumeInfo) []runtime.VolumeInfo {
	var candidates []runtime.VolumeInfo
	for _, v := range volumes {
		if !v.InUse {
			candidates = append(candidates, v)
		}
	}
	return candidates
}

func containerNames(cs []runtime.ContainerInfo) []string {
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return names
}

func volumeNames(vs []runtime.VolumeInfo) []string {
	names := make([]string, 0, len(vs))
	for _, v := range vs {
		names = append(names, v.Name)
	}
	return names
}

func imageTargets(imgs []runtime.ImageInfo) []string {
	targets := make([]string, 0, len(imgs))
	for _, img := range imgs {
		if img.Repository == "<none>" || img.Tag == "<none>" {
			targets = append(targets, img.ID)
			continue
		}
		targets = append(targets, runtime.ImageRef(img.Repository, img.Tag))
	}
	return targets
}

func referencedImageRefs(dataDir string) (map[string]bool, error) {
	refs := make(map[string]bool)
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dataDir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		switch {
		case strings.HasPrefix(name, "deployments"):
			var deps map[string][]types.DeploymentEntry
			if readJSON(dataDir, name, &deps) {
				for _, list := range deps {
					for _, d := range list {
						if d.ImageTag != "" {
							refs[d.ImageTag] = true
						}
					}
				}
			}
		case strings.HasPrefix(name, "apps"):
			var apps map[string]types.AppEntry
			if readJSON(dataDir, name, &apps) {
				for _, a := range apps {
					if a.ImageTag != "" {
						refs[a.ImageTag] = true
					}
				}
			}
		case strings.HasPrefix(name, "previews"):
			var previews map[string]types.PreviewEntry
			if readJSON(dataDir, name, &previews) {
				for _, p := range previews {
					if p.ImageTag != "" {
						refs[p.ImageTag] = true
					}
				}
			}
		}
	}
	return refs, nil
}

func readJSON(dataDir, name string, v interface{}) bool {
	data, err := os.ReadFile(filepath.Join(dataDir, name))
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/ -run "TestContainerCandidates|TestImageCandidates|TestVolumeCandidates|TestImageTargets|TestReferencedImageRefs" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run the full cleanup package tests**

Run: `go test ./internal/cleanup/ -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add safe candidate selection for docker housekeeping"
```

---

### Task 3: Cleaner orchestration (`Plan` + `Prune`)

Add the `Cleaner` that lists resources, computes the plan, and executes removals. `Plan` is a no-op dry run; `Prune` removes candidates and reports per-resource errors without aborting.

**Files:**
- Modify: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.Options`, `cleanup.Result`, decision functions and `referencedImageRefs` from Task 2, `config.Store.DataDir()`
- Produces: `cleanup.PruneRuntime` interface (subset of `runtime.Manager`), `cleanup.New(rt PruneRuntime, store *config.Store) *Cleaner`, `(*Cleaner).Plan(ctx, opts Options) (Result, error)`, `(*Cleaner).Prune(ctx, opts Options) (Result, error)`. Later tasks rely on: `Plan` returns candidate counts/lists with `BuildCache=true` (build cache is always planned); `Prune` with `DryRun=true` removes nothing and returns the plan unchanged; `Prune` with `DryRun=false` removes each candidate, appends failures to `Result.Errors`, and continues

- [ ] **Step 1: Write the failing tests**

Add to `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"io"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type mockPruneRT struct {
	containers      []runtime.ContainerInfo
	images          []runtime.ImageInfo
	volumes         []runtime.VolumeInfo
	removed         []string
	imagesRemoved   []string
	volumesRemoved  []string
	buildCacheCalls int
}

func (m *mockPruneRT) ListContainers(ctx context.Context) ([]runtime.ContainerInfo, error) {
	return m.containers, nil
}

func (m *mockPruneRT) ListImages(ctx context.Context) ([]runtime.ImageInfo, error) {
	return m.images, nil
}

func (m *mockPruneRT) ListVolumes(ctx context.Context) ([]runtime.VolumeInfo, error) {
	return m.volumes, nil
}

func (m *mockPruneRT) PruneBuildCache(ctx context.Context) error {
	m.buildCacheCalls++
	return nil
}

func (m *mockPruneRT) Remove(ctx context.Context, name string) error {
	m.removed = append(m.removed, name)
	return nil
}

func (m *mockPruneRT) RemoveImage(ctx context.Context, imageTag string) error {
	m.imagesRemoved = append(m.imagesRemoved, imageTag)
	return nil
}

func (m *mockPruneRT) RemoveVolume(ctx context.Context, name string) error {
	m.volumesRemoved = append(m.volumesRemoved, name)
	return nil
}

func TestPruneDryRunRemovesNothing(t *testing.T) {
	m := &mockPruneRT{
		containers: []runtime.ContainerInfo{
			{Name: "foreign-stopped", State: "exited", Labels: map[string]string{}},
		},
		images: []runtime.ImageInfo{
			{ID: "sha256:aaa", Repository: "<none>", Tag: "<none>"},
		},
		volumes: []runtime.VolumeInfo{
			{Name: "freevol", InUse: false},
		},
	}
	c := New(m, config.NewStore(t.TempDir()))

	result, err := c.Prune(context.Background(), Options{DryRun: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune(dry-run): %v", err)
	}
	if len(result.ContainersRemoved) != 1 || result.ContainersRemoved[0] != "foreign-stopped" {
		t.Errorf("dry-run containers = %v, want [foreign-stopped]", result.ContainersRemoved)
	}
	if len(result.ImagesRemoved) != 1 || result.ImagesRemoved[0] != "sha256:aaa" {
		t.Errorf("dry-run images = %v, want [sha256:aaa]", result.ImagesRemoved)
	}
	if len(result.VolumesRemoved) != 1 || result.VolumesRemoved[0] != "freevol" {
		t.Errorf("dry-run volumes = %v, want [freevol]", result.VolumesRemoved)
	}
	if !result.BuildCache {
		t.Error("dry-run BuildCache = false, want true")
	}
	if len(m.removed) != 0 || len(m.imagesRemoved) != 0 || len(m.volumesRemoved) != 0 || m.buildCacheCalls != 0 {
		t.Errorf("dry-run mutated state: removed=%v images=%v volumes=%v cacheCalls=%d",
			m.removed, m.imagesRemoved, m.volumesRemoved, m.buildCacheCalls)
	}
}

func TestPruneRemovesCandidates(t *testing.T) {
	m := &mockPruneRT{
		containers: []runtime.ContainerInfo{
			{Name: "foreign-stopped", State: "exited", Labels: map[string]string{}},
			{Name: "tengiz-myapp", State: "exited", Labels: map[string]string{runtime.AppLabel: "myapp"}},
			{Name: "foreign-running", State: "running", Labels: map[string]string{}},
		},
		images: []runtime.ImageInfo{
			{ID: "sha256:aaa", Repository: "<none>", Tag: "<none>"},
			{ID: "sha256:bbb", Repository: "tengiz-apps/myapp", Tag: "production-v1"},
		},
		volumes: []runtime.VolumeInfo{
			{Name: "freevol", InUse: false},
			{Name: "usedvol", InUse: true},
		},
	}
	c := New(m, config.NewStore(t.TempDir()))

	result, err := c.Prune(context.Background(), Options{Volumes: true})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if len(m.removed) != 1 || m.removed[0] != "foreign-stopped" {
		t.Errorf("removed containers = %v, want [foreign-stopped]", m.removed)
	}
	if len(m.imagesRemoved) != 1 || m.imagesRemoved[0] != "sha256:aaa" {
		t.Errorf("removed images = %v, want [sha256:aaa]", m.imagesRemoved)
	}
	if len(m.volumesRemoved) != 1 || m.volumesRemoved[0] != "freevol" {
		t.Errorf("removed volumes = %v, want [freevol]", m.volumesRemoved)
	}
	if m.buildCacheCalls != 1 {
		t.Errorf("build cache calls = %d, want 1", m.buildCacheCalls)
	}
	if !result.BuildCache {
		t.Error("result.BuildCache = false, want true")
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

func TestPruneAllImagesProtectsDeploymentRefs(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)
	store.AddDeployment("myapp", types.DeploymentEntry{
		ID:       "1700000000",
		ImageTag: "tengiz-apps/myapp:production-v1",
		Status:   string(types.DeployActive),
	})

	m := &mockPruneRT{
		containers: []runtime.ContainerInfo{
			{Name: "tengiz-myapp", State: "running", Image: "sha256:bbb", Labels: map[string]string{runtime.AppLabel: "myapp"}},
		},
		images: []runtime.ImageInfo{
			{ID: "sha256:aaa", Repository: "<none>", Tag: "<none>"},
			{ID: "sha256:bbb", Repository: "tengiz-apps/myapp", Tag: "production-v1"},
			{ID: "sha256:ccc", Repository: "tengiz-apps/myapp", Tag: "production-v1"},
			{ID: "sha256:ddd", Repository: "tengiz-apps/oldapp", Tag: "production-v9"},
		},
	}
	c := New(m, store)

	result, err := c.Prune(context.Background(), Options{AllImages: true})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if len(result.ImagesRemoved) != 2 {
		t.Fatalf("images removed = %v, want 2 (dangling + unreferenced oldapp)", result.ImagesRemoved)
	}
	for _, img := range result.ImagesRemoved {
		if img == "sha256:bbb" || img == "tengiz-apps/myapp:production-v1" {
			t.Errorf("protected image removed: %s", img)
		}
	}
}

func TestPruneWithoutVolumesFlagKeepsVolumes(t *testing.T) {
	m := &mockPruneRT{
		volumes: []runtime.VolumeInfo{
			{Name: "freevol", InUse: false},
		},
	}
	c := New(m, config.NewStore(t.TempDir()))

	_, err := c.Prune(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(m.volumesRemoved) != 0 {
		t.Errorf("volumes removed without --volumes = %v, want none", m.volumesRemoved)
	}
}
```

Note: the Task 2 test file already imports `os`, `path/filepath`, `testing`, `config`, `runtime`, `types`. When appending these tests, add `context` to that import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -run "TestPrune|TestCleaner" -v -count=1`

Expected: FAIL with `undefined: New`, `undefined: mockPruneRT` (methods on `PruneRuntime` not yet satisfied)

- [ ] **Step 3: Add the `PruneRuntime` interface, `Cleaner`, `New`, `Plan`, and `Prune` to `internal/cleanup/cleanup.go`**

Append to `internal/cleanup/cleanup.go`. First update the import block written in Task 2 Step 3 to add `context` and `github.com/yaso09/tengiz/internal/config` — the final import block must be:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)
```

Then append:

```go
type PruneRuntime interface {
	ListContainers(ctx context.Context) ([]runtime.ContainerInfo, error)
	ListImages(ctx context.Context) ([]runtime.ImageInfo, error)
	ListVolumes(ctx context.Context) ([]runtime.VolumeInfo, error)
	PruneBuildCache(ctx context.Context) error
	Remove(ctx context.Context, name string) error
	RemoveImage(ctx context.Context, imageTag string) error
	RemoveVolume(ctx context.Context, name string) error
}

type Cleaner struct {
	rt    PruneRuntime
	store *config.Store
}

func New(rt PruneRuntime, store *config.Store) *Cleaner {
	return &Cleaner{rt: rt, store: store}
}

func (c *Cleaner) Plan(ctx context.Context, opts Options) (Result, error) {
	containers, err := c.rt.ListContainers(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list containers: %w", err)
	}
	images, err := c.rt.ListImages(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list images: %w", err)
	}

	protectedIDs := make(map[string]bool)
	for _, ctr := range containers {
		if ctr.Image != "" {
			protectedIDs[ctr.Image] = true
		}
	}
	protectedRefs, err := referencedImageRefs(c.store.DataDir())
	if err != nil {
		return Result{}, fmt.Errorf("load deployment refs: %w", err)
	}

	imgCandidates := imageCandidates(images, protectedIDs, protectedRefs, opts.AllImages)

	var volCandidates []runtime.VolumeInfo
	if opts.Volumes {
		vols, err := c.rt.ListVolumes(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("list volumes: %w", err)
		}
		volCandidates = volumeCandidates(vols)
	}

	return Result{
		ContainersRemoved: containerNames(containerCandidates(containers)),
		ImagesRemoved:     imageTargets(imgCandidates),
		VolumesRemoved:    volumeNames(volCandidates),
		BuildCache:        true,
	}, nil
}

func (c *Cleaner) Prune(ctx context.Context, opts Options) (Result, error) {
	plan, err := c.Plan(ctx, opts)
	if err != nil {
		return Result{}, err
	}
	if opts.DryRun {
		return plan, nil
	}

	result := Result{BuildCache: plan.BuildCache}

	for _, name := range plan.ContainersRemoved {
		if err := c.rt.Remove(ctx, name); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("container %s: %v", name, err))
			continue
		}
		result.ContainersRemoved = append(result.ContainersRemoved, name)
	}
	for _, img := range plan.ImagesRemoved {
		if err := c.rt.RemoveImage(ctx, img); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("image %s: %v", img, err))
			continue
		}
		result.ImagesRemoved = append(result.ImagesRemoved, img)
	}
	for _, vol := range plan.VolumesRemoved {
		if err := c.rt.RemoveVolume(ctx, vol); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("volume %s: %v", vol, err))
			continue
		}
		result.VolumesRemoved = append(result.VolumesRemoved, vol)
	}
	if plan.BuildCache {
		if err := c.rt.PruneBuildCache(ctx); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("build cache: %v", err))
			result.BuildCache = false
		}
	}
	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/ -v -count=1`

Expected: PASS (all Task 2 + Task 3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat(cleanup): add Plan/Prune orchestration for docker housekeeping"
```

---

### Task 4: `tengiz cleanup` CLI command

Add the cobra command with flags, confirmation prompt, and summary output. The run logic is factored into `runCleanup(cmd, rt)` so tests can inject `runtime.NewStub()`.

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.New(rt, store)`, `cleanup.Options`, `cleanup.Result`, `config.NewStoreWithEnv`, `runtime.NewDocker`, `getEnv(cmd)` (existing helper in `internal/cli/root.go:97`)
- Produces: `cleanupCmd` (registered on `rootCmd` via `init()` in `cleanup.go`), `runCleanup(cmd *cobra.Command, rt runtime.Manager) error`, `confirmCleanup(in io.Reader) (bool, error)`, `printPlan(w io.Writer, r cleanup.Result, dryRun bool)`, `printResult(w io.Writer, r cleanup.Result)`. Later tasks rely on the command's flags: `--dry-run`, `-y/--yes`, `--volumes`, `--all`

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
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	for _, flag := range []string{"dry-run", "yes", "volumes", "all"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestRunCleanupDryRun(t *testing.T) {
	dataDir = t.TempDir()
	cmd := cleanupCmd
	if err := cmd.ParseFlags([]string{"--dry-run"}); err != nil {
		t.Fatal(err)
	}
	output := captureOutput(func() {
		if err := runCleanup(cmd, runtime.NewStub()); err != nil {
			t.Errorf("runCleanup: %v", err)
		}
	})
	if !strings.Contains(output, "would remove") {
		t.Errorf("dry-run output missing 'would remove', got: %s", output)
	}
	if !strings.Contains(output, "build cache: yes") {
		t.Errorf("dry-run output missing build cache line, got: %s", output)
	}
}

func TestRunCleanupYes(t *testing.T) {
	dataDir = t.TempDir()
	cmd := cleanupCmd
	if err := cmd.ParseFlags([]string{"--yes"}); err != nil {
		t.Fatal(err)
	}
	output := captureOutput(func() {
		if err := runCleanup(cmd, runtime.NewStub()); err != nil {
			t.Errorf("runCleanup: %v", err)
		}
	})
	if !strings.Contains(output, "cleanup complete") {
		t.Errorf("output missing 'cleanup complete', got: %s", output)
	}
	if !strings.Contains(output, "build cache pruned: yes") {
		t.Errorf("output missing build cache pruned line, got: %s", output)
	}
}

func TestConfirmCleanup(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"n\n", false},
		{"\n", false},
		{"no\n", false},
	}
	for _, tt := range tests {
		got, err := confirmCleanup(strings.NewReader(tt.input))
		if err != nil {
			t.Fatalf("confirmCleanup(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("confirmCleanup(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanup|TestConfirmCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: runCleanup`, `undefined: confirmCleanup`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, build cache)",
	Long: `Remove unused Docker resources to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app), images referenced by
running containers or deployment history, and mounted volumes are always
protected. By default this removes stopped non-Tengiz containers, dangling
images, and the Docker build cache.

Flags:
  --dry-run   show what would be removed without removing anything
  -y, --yes   skip the confirmation prompt
  --volumes   also remove unused Docker volumes (destructive)
  --all       remove all unused images, not just dangling ones`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		return runCleanup(cmd, rt)
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused Docker volumes (destructive)")
	cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
	rootCmd.AddCommand(cleanupCmd)
}

func runCleanup(cmd *cobra.Command, rt runtime.Manager) error {
	env := getEnv(cmd)
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	vols, _ := cmd.Flags().GetBool("volumes")
	all, _ := cmd.Flags().GetBool("all")

	store := config.NewStoreWithEnv(dataDir, env)
	c := cleanup.New(rt, store)

	opts := cleanup.Options{DryRun: dryRun, Yes: yes, Volumes: vols, AllImages: all}

	plan, err := c.Plan(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("cleanup plan: %w", err)
	}

	if dryRun {
		printPlan(os.Stdout, plan, true)
		return nil
	}

	if !yes {
		printPlan(os.Stdout, plan, false)
		confirmed, err := confirmCleanup(os.Stdin)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("[tengiz] cleanup aborted")
			return nil
		}
	}

	result, err := c.Prune(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	printResult(os.Stdout, result)
	return nil
}

func confirmCleanup(in io.Reader) (bool, error) {
	fmt.Print("Proceed? [y/N]: ")
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

func printPlan(w io.Writer, r cleanup.Result, dryRun bool) {
	verb := "would remove"
	if !dryRun {
		verb = "will remove"
	}
	fmt.Fprintf(w, "[tengiz] cleanup %s:\n", verb)
	fmt.Fprintf(w, "  containers:  %d\n", len(r.ContainersRemoved))
	fmt.Fprintf(w, "  images:      %d\n", len(r.ImagesRemoved))
	fmt.Fprintf(w, "  volumes:     %d\n", len(r.VolumesRemoved))
	buildCache := "no"
	if r.BuildCache {
		buildCache = "yes"
	}
	fmt.Fprintf(w, "  build cache: %s\n", buildCache)
}

func printResult(w io.Writer, r cleanup.Result) {
	fmt.Fprintf(w, "[tengiz] cleanup complete:\n")
	fmt.Fprintf(w, "  containers removed: %d\n", len(r.ContainersRemoved))
	fmt.Fprintf(w, "  images removed:     %d\n", len(r.ImagesRemoved))
	fmt.Fprintf(w, "  volumes removed:    %d\n", len(r.VolumesRemoved))
	buildCache := "no"
	if r.BuildCache {
		buildCache = "yes"
	}
	fmt.Fprintf(w, "  build cache pruned: %s\n", buildCache)
	for _, e := range r.Errors {
		fmt.Fprintf(w, "  error: %s\n", e)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run "TestCleanup|TestConfirmCleanup" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 5: Run all CLI tests**

Run: `go test ./internal/cli/ -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Unused network pruning

Extend all layers to also remove unused custom Docker networks (matching `docker system prune` default behavior). Built-in networks and networks in use by any container are never removed.

**Files:**
- Modify: `internal/runtime/prune.go`, `internal/runtime/runtime.go:31-49` (interface) and stub section
- Modify: `internal/cli/root_test.go`, `internal/idle/idle_test.go`, `internal/proxy/proxy_test.go` (mock `runtime.Manager` implementations)
- Modify: `internal/cleanup/cleanup.go`, `internal/cleanup/cleanup_test.go`
- Modify: `internal/cli/cleanup.go`
- Test: `internal/runtime/prune_test.go`, `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NetworkInfo` (new), `cleanup.Result` from Task 2, `PruneRuntime`/`Cleaner` from Task 3
- Produces: `runtime.NetworkInfo{ID, Name string; InUse bool}`, `Manager` methods `ListNetworks(ctx) ([]NetworkInfo, error)`, `RemoveNetwork(ctx, name string) error`, `cleanup.Result.NetworksRemoved []string`, `networkCandidates([]runtime.NetworkInfo) []runtime.NetworkInfo`. Later tasks rely on: `Plan` always includes `NetworksRemoved` (networks are cleaned by default, like build cache); `Prune` removes them via `RemoveNetwork` and records failures in `Result.Errors`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/prune_test.go`:

```go
func TestStubNetworkMethods(t *testing.T) {
	m := NewStub()
	if _, err := m.ListNetworks(context.Background()); err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if err := m.RemoveNetwork(context.Background(), "mybridge"); err != nil {
		t.Fatalf("RemoveNetwork: %v", err)
	}
}
```

Add to `internal/cleanup/cleanup_test.go`:

```go
func TestNetworkCandidates(t *testing.T) {
	networks := []runtime.NetworkInfo{
		{Name: "bridge", InUse: false},
		{Name: "host", InUse: false},
		{Name: "none", InUse: false},
		{Name: "mybridge", InUse: false},
		{Name: "usedbridge", InUse: true},
	}
	got := networkCandidates(networks)
	if len(got) != 1 || got[0].Name != "mybridge" {
		t.Fatalf("networkCandidates() = %+v, want only mybridge", got)
	}
}

func TestPruneRemovesUnusedNetworks(t *testing.T) {
	m := &mockPruneRT{
		networks: []runtime.NetworkInfo{
			{Name: "mybridge", InUse: false},
			{Name: "usedbridge", InUse: true},
		},
	}
	c := New(m, config.NewStore(t.TempDir()))

	result, err := c.Prune(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(m.networksRemoved) != 1 || m.networksRemoved[0] != "mybridge" {
		t.Errorf("networks removed = %v, want [mybridge]", m.networksRemoved)
	}
	if len(result.NetworksRemoved) != 1 || result.NetworksRemoved[0] != "mybridge" {
		t.Errorf("result.NetworksRemoved = %v, want [mybridge]", result.NetworksRemoved)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestStubNetworkMethods" -v -count=1`
Run: `go test ./internal/cleanup/ -run "TestNetworkCandidates|TestPruneRemovesUnusedNetworks" -v -count=1`

Expected: FAIL with `undefined: ListNetworks`, `undefined: networkCandidates`, `undefined: networksRemoved`

- [ ] **Step 3: Add `NetworkInfo`, `ListNetworks`, `RemoveNetwork` to `internal/runtime/prune.go`**

Append:

```go
type NetworkInfo struct {
	ID    string
	Name  string
	InUse bool
}

func (r *dockerRuntime) ListNetworks(ctx context.Context) ([]NetworkInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "ls", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	var infos []NetworkInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var raw struct {
			ID   string `json:"ID"`
			Name string `json:"Name"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		infos = append(infos, NetworkInfo{ID: raw.ID, Name: raw.Name, InUse: r.networkInUse(ctx, raw.Name)})
	}
	return infos, nil
}

func (r *dockerRuntime) networkInUse(ctx context.Context, name string) bool {
	cmd := exec.CommandContext(ctx, "docker", "network", "inspect", "--format", "{{len .Containers}}", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return true
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return true
	}
	return count > 0
}

func (r *dockerRuntime) RemoveNetwork(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "rm", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network rm: %w\n%s", err, string(out))
	}
	return nil
}
```

- [ ] **Step 4: Add the 2 methods to the `Manager` interface and stub in `internal/runtime/runtime.go`**

Add to the interface (after `RemoveVolume`):

```go
	ListNetworks(ctx context.Context) ([]NetworkInfo, error)
	RemoveNetwork(ctx context.Context, name string) error
```

Add to the stub (after the `RemoveVolume` stub):

```go
func (m *stubManager) ListNetworks(ctx context.Context) ([]NetworkInfo, error) {
	return nil, nil
}

func (m *stubManager) RemoveNetwork(ctx context.Context, name string) error {
	return nil
}
```

- [ ] **Step 5: Update the three mock `runtime.Manager` implementations**

`internal/cli/root_test.go` — add after the `RemoveVolume` mock line:

```go
func (m *mockRTForDeploy) ListNetworks(ctx context.Context) ([]runtime.NetworkInfo, error) { return nil, nil }
func (m *mockRTForDeploy) RemoveNetwork(ctx context.Context, name string) error { return nil }
```

`internal/idle/idle_test.go` — add after the `RemoveVolume` mock line:

```go
func (m *mockRuntime) ListNetworks(ctx context.Context) ([]runtime.NetworkInfo, error) { return nil, nil }
func (m *mockRuntime) RemoveNetwork(ctx context.Context, name string) error { return nil }
```

`internal/proxy/proxy_test.go` — add after the `RemoveVolume` mock line:

```go
func (m *mockRuntime) ListNetworks(ctx context.Context) ([]runtime.NetworkInfo, error) { return nil, nil }
func (m *mockRuntime) RemoveNetwork(ctx context.Context, name string) error { return nil }
```

- [ ] **Step 6: Add `NetworksRemoved` to `Result` and `networkCandidates`/`networkNames` to `internal/cleanup/cleanup.go`**

Add `NetworksRemoved []string` to the `Result` struct in Task 2:

```go
type Result struct {
	ContainersRemoved []string
	ImagesRemoved     []string
	NetworksRemoved   []string
	VolumesRemoved    []string
	BuildCache        bool
	Errors            []string
}
```

Append:

```go
func networkCandidates(networks []runtime.NetworkInfo) []runtime.NetworkInfo {
	var candidates []runtime.NetworkInfo
	for _, n := range networks {
		if n.Name == "bridge" || n.Name == "host" || n.Name == "none" {
			continue
		}
		if !n.InUse {
			candidates = append(candidates, n)
		}
	}
	return candidates
}

func networkNames(ns []runtime.NetworkInfo) []string {
	names := make([]string, 0, len(ns))
	for _, n := range ns {
		names = append(names, n.Name)
	}
	return names
}
```

- [ ] **Step 7: Add `ListNetworks`/`RemoveNetwork` to `PruneRuntime`, and wire networks into `Plan`/`Prune` in `internal/cleanup/cleanup.go`**

Add to the `PruneRuntime` interface:

```go
	ListNetworks(ctx context.Context) ([]runtime.NetworkInfo, error)
	RemoveNetwork(ctx context.Context, name string) error
```

In `Plan`, after the images/protected-refs block and before `imgCandidates`, add:

```go
	networks, err := c.rt.ListNetworks(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list networks: %w", err)
	}
```

And add `NetworksRemoved: networkNames(networkCandidates(networks)),` to the returned `Result`.

In `Prune`, after the containers loop, add:

```go
	for _, name := range plan.NetworksRemoved {
		if err := c.rt.RemoveNetwork(ctx, name); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("network %s: %v", name, err))
			continue
		}
		result.NetworksRemoved = append(result.NetworksRemoved, name)
	}
```

- [ ] **Step 8: Extend the `mockPruneRT` in `internal/cleanup/cleanup_test.go`**

Add fields and methods to `mockPruneRT`:

```go
type mockPruneRT struct {
	containers      []runtime.ContainerInfo
	images          []runtime.ImageInfo
	networks        []runtime.NetworkInfo
	volumes         []runtime.VolumeInfo
	removed         []string
	imagesRemoved   []string
	networksRemoved []string
	volumesRemoved  []string
	buildCacheCalls int
}

func (m *mockPruneRT) ListNetworks(ctx context.Context) ([]runtime.NetworkInfo, error) {
	return m.networks, nil
}

func (m *mockPruneRT) RemoveNetwork(ctx context.Context, name string) error {
	m.networksRemoved = append(m.networksRemoved, name)
	return nil
}
```

- [ ] **Step 9: Update `printPlan` and `printResult` in `internal/cli/cleanup.go`**

Add to `printPlan` (after the containers line):

```go
	fmt.Fprintf(w, "  networks:     %d\n", len(r.NetworksRemoved))
```

Add to `printResult` (after the containers line):

```go
	fmt.Fprintf(w, "  networks removed:   %d\n", len(r.NetworksRemoved))
```

- [ ] **Step 10: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestStubNetworkMethods" -v -count=1`
Run: `go test ./internal/cleanup/ -v -count=1`
Run: `go test ./internal/cli/ -run "TestCleanup|TestConfirmCleanup" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 11: Run all affected package tests**

Run: `go test ./internal/runtime/ ./internal/idle/ ./internal/proxy/ ./internal/cli/ ./internal/cleanup/ -count=1`

Expected: All PASS

- [ ] **Step 12: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go internal/runtime/runtime.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go internal/cli/cleanup.go
git commit -m "feat(cleanup): prune unused custom docker networks"
```

---

### Task 6: Documentation and full verification

Update the CLI reference docs and run the complete suite.

**Files:**
- Modify: `README.md` (CLI Reference section after line 228, `tengiz rm` block; Commands table at line 568)
- Modify: `AGENTS.md` (CLI command list)
- Test: none new — run full suite

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

Insert after the `### tengiz rm <app>` block (ends line 228), before `### tengiz rollback <app>`:

```markdown
### `tengiz cleanup [--dry-run] [--volumes] [--all]`

Remove unused Docker resources to reclaim disk space on single-server deployments.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `-y`, `--yes` | Skip the confirmation prompt |
| `--volumes` | Also remove unused Docker volumes (destructive) |
| `--all` | Remove all unused images, not just dangling ones |

By default removes stopped non-Tengiz containers, dangling images, unused custom networks, and the Docker build cache. **Tengiz-managed containers are always protected** — apps labeled `tengiz-app` (including scale-to-zero stopped containers, versioned containers, and preview deployments) are never removed. Images referenced by any container or by deployment history in `~/.tengiz/` are never removed. Prompts for confirmation unless `--dry-run` or `--yes` is given.
```

- [ ] **Step 2: Add the row to the Commands table in `README.md` (line 568)**

Insert after the `tengiz init --git-repo URL` row:

```markdown
| `tengiz cleanup [--dry-run] [--volumes] [--all]` | Remove unused Docker resources (protects Tengiz-managed containers) |
```

- [ ] **Step 3: Add the command to `AGENTS.md`**

Insert after the `tengiz preview deploy` line in the CLI code block:

```
tengiz cleanup [--dry-run] [--volumes] [--all] → remove unused Docker resources (protects Tengiz-managed containers via labels)
```

- [ ] **Step 4: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS (note: `internal/proxy` tests are slow ~2s each due to TCP dial timeout; `internal/idle` tests are time-sensitive with 50ms granularity — these pass unchanged)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 5: Self-review against the spec**

Check each requirement from `docs/FUTURES_FEATURES.md` #6 ("Docker Housekeeping — Label-based `docker system prune`. `tengiz cleanup`."):
- `tengiz cleanup` command ✅ (Task 4)
- Label-based filtering protects Tengiz containers ✅ (`containerCandidates` skips `tengiz-app`-labeled containers, Task 2)
- Cleans unused containers/images/networks/build cache ✅ (Tasks 1-3, 5)
- Optional `--volumes` destructive mode ✅ (Task 4)
- No breaking changes ✅ (existing tests pass; only additive interface methods on mocks)
- `README.md` + `AGENTS.md` updated ✅ (Steps 1-3)

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```
