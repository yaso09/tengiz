# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely reclaims disk space by pruning unused Docker containers, images, networks, and build cache while never touching Tengiz-managed or in-use resources.

**Architecture:** A new `internal/cleanup` package orchestrates housekeeping. It reads the env-scoped `config.Store` to build a protection set (active/previous deployment image tags, preview tags, live container images), enumerates Docker state via new `runtime.Manager` methods (`Prune`, `ListImages`, `ListContainers`, `DiskUsage`), and removes only resources outside the protection set. The `runtime` layer stays a thin `docker` CLI wrapper with pure, unit-testable arg/parse helpers. Safety is label-based: containers labeled `tengiz-app` (running or stopped, including scale-to-zero targets and previews) are never pruned, and volumes are only pruned with an explicit `--volumes` flag.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager`, `config.Store`, `os/exec` Docker CLI passthrough. No new external dependencies.

## Global Constraints

- All new code follows the existing single-module layout: `github.com/yaso09/tengiz`
- New runtime methods must be added to the `runtime.Manager` interface AND to every existing implementer (`stubManager` in `runtime.go`, `mockRuntime` in `proxy/proxy_test.go`, `mockRuntime` in `idle/idle_test.go`, `mockRTForDeploy` in `cli/root_test.go`) or compilation breaks
- Container protection rule (verbatim): never prune any container carrying the `tengiz-app` label, in any state; running/paused/restarting containers are always protected
- Image protection rule: never remove image tags referenced by the store (active/previous deployments, previews), by any live container, or by the `<env>-latest`/`latest` tags
- Image retention default: keep the newest 5 images per app (`--keep-images`, default 5)
- Default `tengiz cleanup` (no flags) = containers + images + networks + build cache; volumes are NEVER touched unless `--volumes` is passed
- Cleanup is env-scoped via `--env` (global flag); image tags are `tengiz-apps/<app>:<env>-<deploymentID>` so retention must never remove another env's images
- `--dry-run` performs zero destructive operations (no prune, no rmi)
- No new external dependencies; all tests use the stub/fake pattern already in the repo
- Follow TDD: failing test → minimal implementation → passing test → commit, per task

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/housekeeping.go` (new) | `PruneKind`, `ImageInfo`, `ContainerInfo`, `CategoryUsage`, `DiskUsage` types; pure helpers `pruneArgs`, `parseImageInfos`, `parseContainerInfos`, `parseDiskUsage`; dockerRuntime methods `Prune`, `ListImages`, `ListContainers`, `DiskUsage` |
| `internal/runtime/runtime.go` | Add 4 methods to `Manager` interface + `stubManager` no-op implementations |
| `internal/runtime/housekeeping_test.go` (new) | Unit tests for the pure helpers + stub coverage |
| `internal/proxy/proxy_test.go` | Add 4 no-op methods to `mockRuntime` (interface conformance) |
| `internal/idle/idle_test.go` | Add 4 no-op methods to `mockRuntime` (interface conformance) |
| `internal/cli/root_test.go` | Add 4 no-op methods to `mockRTForDeploy` (interface conformance) |
| `internal/cleanup/cleanup.go` (new) | `Options`, `Report`, `Housekeeper`, `New`, `Run`, `protectedTags`, `classifyImages`, `containerCandidates`, `protectedContainerCount` |
| `internal/cleanup/cleanup_test.go` (new) | Fake runtime tests: classification, env-scoping, app-scoping, dry-run, rollback protection, volume-only |
| `internal/cli/root.go` | Add `cleanupCmd`, register on `rootCmd`, define flags, `printCleanupReport` helper |
| `internal/cli/cleanup_test.go` (new) | Command registration + flag definition tests |
| `README.md` | Add `tengiz cleanup` to Features list + CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |
| `AGENTS.md` | Add `tengiz cleanup` line to the CLI section |

---

### Task 1: Runtime housekeeping primitives

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Create: `internal/runtime/housekeeping_test.go`
- Modify: `internal/runtime/runtime.go` (interface + stub)
- Modify: `internal/proxy/proxy_test.go:15-35` (mockRuntime)
- Modify: `internal/idle/idle_test.go:14-34` (mockRuntime)
- Modify: `internal/cli/root_test.go:69-100` (mockRTForDeploy)

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type PruneKind string` with constants `PruneContainers = "container"`, `PruneImages = "image"`, `PruneNetworks = "network"`, `PruneVolumes = "volume"`, `PruneBuildCache = "builder"`
  - `type ImageInfo struct { ID, Repository, Tag, CreatedAt, Size string }`
  - `type ContainerInfo struct { ID, Name, State string; Labels map[string]string; Image string }`
  - `type CategoryUsage struct { Total, Active int; Size, Reclaimable string }`
  - `type DiskUsage struct { Images, Containers, Volumes, BuildCache *CategoryUsage; TotalReclaimable string }`
  - `func pruneArgs(kind PruneKind, all bool, filters []string) []string`
  - `func parseImageInfos(out string) ([]ImageInfo, error)`
  - `func parseContainerInfos(out string) ([]ContainerInfo, error)`
  - `func parseDiskUsage(out string) (*DiskUsage, error)`
  - Manager methods: `Prune(ctx context.Context, kind PruneKind, all bool, filters []string) (string, error)`, `ListImages(ctx context.Context, filter string) ([]ImageInfo, error)`, `ListContainers(ctx context.Context) ([]ContainerInfo, error)`, `DiskUsage(ctx context.Context) (*DiskUsage, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/runtime/housekeeping_test.go`:

```go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		name    string
		kind    PruneKind
		all     bool
		filters []string
		want    []string
	}{
		{
			name: "containers no filters",
			kind: PruneContainers,
			want: []string{"container", "prune", "-f"},
		},
		{
			name:    "containers with label filter",
			kind:    PruneContainers,
			filters: []string{"label!=tengiz-app"},
			want:    []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name: "images default keeps dangling only",
			kind: PruneImages,
			want: []string{"image", "prune", "-f"},
		},
		{
			name: "images all removes all unused",
			kind: PruneImages,
			all:  true,
			want: []string{"image", "prune", "-f", "-a"},
		},
		{
			name: "builder all",
			kind: PruneBuildCache,
			all:  true,
			want: []string{"builder", "prune", "-f", "-a"},
		},
		{
			name: "network ignores all flag",
			kind: PruneNetworks,
			all:  true,
			want: []string{"network", "prune", "-f"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneArgs(tt.kind, tt.all, tt.filters)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("pruneArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseImageInfos(t *testing.T) {
	out := `{"ID":"sha256:aaa","Repository":"tengiz-apps/myapp","Tag":"production-1","CreatedAt":"2026-07-30 12:00:00 +0000 UTC","Size":"250MB"}
{"ID":"sha256:bbb","Repository":"tengiz-apps/myapp","Tag":"production-latest","CreatedAt":"2026-07-31 12:00:00 +0000 UTC","Size":"250MB"}`
	images, err := parseImageInfos(out)
	if err != nil {
		t.Fatalf("parseImageInfos: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("got %d images, want 2", len(images))
	}
	if images[0].Repository != "tengiz-apps/myapp" || images[0].Tag != "production-1" {
		t.Errorf("first image = %+v", images[0])
	}
	if images[1].Tag != "production-latest" {
		t.Errorf("second tag = %q, want production-latest", images[1].Tag)
	}
}

func TestParseContainerInfos(t *testing.T) {
	out := `{"ID":"abc","Names":["/tengiz-myapp"],"Image":"tengiz-apps/myapp:production-1","State":"running","Labels":"tengiz-app=myapp,tengiz-env=production"}
{"ID":"def","Names":["/nginx-proxy"],"Image":"nginx:alpine","State":"exited","Labels":"com.example=x"}`
	cs, err := parseContainerInfos(out)
	if err != nil {
		t.Fatalf("parseContainerInfos: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d containers, want 2", len(cs))
	}
	if cs[0].Name != "tengiz-myapp" {
		t.Errorf("name = %q, want tengiz-myapp", cs[0].Name)
	}
	if cs[0].Labels["tengiz-app"] != "myapp" {
		t.Errorf("label tengiz-app = %q, want myapp", cs[0].Labels["tengiz-app"])
	}
	if cs[0].Image != "tengiz-apps/myapp:production-1" {
		t.Errorf("image = %q", cs[0].Image)
	}
	if cs[1].State != "exited" || cs[1].Name != "nginx-proxy" {
		t.Errorf("second container = %+v", cs[1])
	}
}

func TestParseDiskUsage(t *testing.T) {
	out := `TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE
Images          10        3         2.5GB     1.8GB (72%)
Containers      4         2         200MB     150MB (75%)
Local Volumes   2         1         100MB     50MB (50%)
Build Cache     8         0         300MB     300MB

Total Reclaimable: 2.3GB`
	du, err := parseDiskUsage(out)
	if err != nil {
		t.Fatalf("parseDiskUsage: %v", err)
	}
	if du.Images == nil || du.Images.Total != 10 || du.Images.Active != 3 {
		t.Errorf("Images = %+v", du.Images)
	}
	if du.Volumes == nil || du.Volumes.Reclaimable != "50MB" {
		t.Errorf("Volumes = %+v", du.Volumes)
	}
	if du.BuildCache == nil || du.BuildCache.Total != 8 {
		t.Errorf("BuildCache = %+v", du.BuildCache)
	}
	if du.TotalReclaimable != "2.3GB" {
		t.Errorf("TotalReclaimable = %q, want 2.3GB", du.TotalReclaimable)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	if out, err := m.Prune(context.Background(), PruneContainers, false, nil); err != nil || out != "" {
		t.Fatalf("Prune() = %q, %v", out, err)
	}
}

func TestStubListImages(t *testing.T) {
	m := NewStub()
	imgs, err := m.ListImages(context.Background(), "reference=tengiz-apps/*")
	if err != nil || imgs != nil {
		t.Fatalf("ListImages() = %v, %v", imgs, err)
	}
}

func TestStubListContainers(t *testing.T) {
	m := NewStub()
	cs, err := m.ListContainers(context.Background())
	if err != nil || cs != nil {
		t.Fatalf("ListContainers() = %v, %v", cs, err)
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	du, err := m.DiskUsage(context.Background())
	if err != nil || du != nil {
		t.Fatalf("DiskUsage() = %v, %v", du, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneArgs|TestParseImageInfos|TestParseContainerInfos|TestParseDiskUsage|TestStubPrune|TestStubListImages|TestStubListContainers|TestStubDiskUsage" -v -count=1`

Expected: FAIL with `undefined: pruneArgs`, `undefined: parseImageInfos`, `undefined: parseContainerInfos`, `undefined: parseDiskUsage`, and stub calls fail to compile because the `Manager` interface has no such methods.

- [ ] **Step 3: Implement `internal/runtime/housekeeping.go`**

```go
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type PruneKind string

const (
	PruneContainers PruneKind = "container"
	PruneImages     PruneKind = "image"
	PruneNetworks   PruneKind = "network"
	PruneVolumes    PruneKind = "volume"
	PruneBuildCache PruneKind = "builder"
)

type ImageInfo struct {
	ID         string
	Repository string
	Tag        string
	CreatedAt  string
	Size       string
}

type ContainerInfo struct {
	ID     string
	Name   string
	State  string
	Labels map[string]string
	Image  string
}

type CategoryUsage struct {
	Total       int
	Active      int
	Size        string
	Reclaimable string
}

type DiskUsage struct {
	Images           *CategoryUsage
	Containers       *CategoryUsage
	Volumes          *CategoryUsage
	BuildCache       *CategoryUsage
	TotalReclaimable string
}

func pruneArgs(kind PruneKind, all bool, filters []string) []string {
	args := []string{string(kind), "prune", "-f"}
	if all && (kind == PruneImages || kind == PruneBuildCache) {
		args = append(args, "-a")
	}
	for _, f := range filters {
		args = append(args, "--filter", f)
	}
	return args
}

func parseImageInfos(out string) ([]ImageInfo, error) {
	var images []ImageInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e struct {
			ID         string `json:"ID"`
			Repository string `json:"Repository"`
			Tag        string `json:"Tag"`
			CreatedAt  string `json:"CreatedAt"`
			Size       string `json:"Size"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("parse image line %q: %w", line, err)
		}
		images = append(images, ImageInfo{
			ID:         e.ID,
			Repository: e.Repository,
			Tag:        e.Tag,
			CreatedAt:  e.CreatedAt,
			Size:       e.Size,
		})
	}
	return images, nil
}

func parseContainerInfos(out string) ([]ContainerInfo, error) {
	var containers []ContainerInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e struct {
			ID     string   `json:"ID"`
			Names  []string `json:"Names"`
			State  string   `json:"State"`
			Labels string   `json:"Labels"`
			Image  string   `json:"Image"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("parse container line %q: %w", line, err)
		}
		name := ""
		if len(e.Names) > 0 {
			name = strings.TrimPrefix(e.Names[0], "/")
		}
		labels := make(map[string]string)
		for _, part := range strings.Split(e.Labels, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				labels[kv[0]] = kv[1]
			} else if len(kv) == 1 && kv[0] != "" {
				labels[kv[0]] = ""
			}
		}
		containers = append(containers, ContainerInfo{
			ID:     e.ID,
			Name:   name,
			State:  e.State,
			Labels: labels,
			Image:  e.Image,
		})
	}
	return containers, nil
}

func parseDiskUsage(out string) (*DiskUsage, error) {
	d := &DiskUsage{}
	typeRe := regexp.MustCompile(`^(Images|Containers|Local Volumes|Build Cache)\s+(\d+)\s+(\d+)\s+(\S+)\s+(\S+)`)
	reclaimRe := regexp.MustCompile(`(?i)Total Reclaimable:\s*(\S+)`)
	for _, line := range strings.Split(out, "\n") {
		if m := typeRe.FindStringSubmatch(line); m != nil {
			total, _ := strconv.Atoi(m[2])
			active, _ := strconv.Atoi(m[3])
			u := &CategoryUsage{Total: total, Active: active, Size: m[4], Reclaimable: m[5]}
			switch m[1] {
			case "Images":
				d.Images = u
			case "Containers":
				d.Containers = u
			case "Local Volumes":
				d.Volumes = u
			case "Build Cache":
				d.BuildCache = u
			}
		} else if m := reclaimRe.FindStringSubmatch(line); m != nil {
			d.TotalReclaimable = m[1]
		}
	}
	return d, nil
}

func (r *dockerRuntime) Prune(ctx context.Context, kind PruneKind, all bool, filters []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", pruneArgs(kind, all, filters)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker prune %s: %w\n%s", kind, err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) ListImages(ctx context.Context, filter string) ([]ImageInfo, error) {
	args := []string{"images", "--format", "{{json .}}"}
	if filter != "" {
		args = append(args, "--filter", filter)
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return parseImageInfos(string(out))
}

func (r *dockerRuntime) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps -a: %w\n%s", err, string(out))
	}
	return parseContainerInfos(string(out))
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (*DiskUsage, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseDiskUsage(string(out))
}
```

- [ ] **Step 4: Add methods to the `Manager` interface and stub**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after the `Run` line at line 48):

```go
	Prune(ctx context.Context, kind PruneKind, all bool, filters []string) (string, error)
	ListImages(ctx context.Context, filter string) ([]ImageInfo, error)
	ListContainers(ctx context.Context) ([]ContainerInfo, error)
	DiskUsage(ctx context.Context) (*DiskUsage, error)
```

Add these no-op implementations to `stubManager` at the end of the file (after the existing `Run` stub at line 121):

```go
func (m *stubManager) Prune(ctx context.Context, kind PruneKind, all bool, filters []string) (string, error) {
	return "", nil
}

func (m *stubManager) ListImages(ctx context.Context, filter string) ([]ImageInfo, error) {
	return nil, nil
}

func (m *stubManager) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	return nil, nil
}

func (m *stubManager) DiskUsage(ctx context.Context) (*DiskUsage, error) {
	return nil, nil
}
```

- [ ] **Step 5: Add the 4 new methods to the three test mocks (required for compilation)**

In `internal/proxy/proxy_test.go`, append after the `Run` method (line 35):

```go
func (m *mockRuntime) Prune(ctx context.Context, kind runtime.PruneKind, all bool, filters []string) (string, error) { return "", nil }
func (m *mockRuntime) ListImages(ctx context.Context, filter string) ([]runtime.ImageInfo, error) { return nil, nil }
func (m *mockRuntime) ListContainers(ctx context.Context) ([]runtime.ContainerInfo, error) { return nil, nil }
func (m *mockRuntime) DiskUsage(ctx context.Context) (*runtime.DiskUsage, error) { return nil, nil }
```

In `internal/idle/idle_test.go`, append after the `Run` method (line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, kind runtime.PruneKind, all bool, filters []string) (string, error) { return "", nil }
func (m *mockRuntime) ListImages(ctx context.Context, filter string) ([]runtime.ImageInfo, error) { return nil, nil }
func (m *mockRuntime) ListContainers(ctx context.Context) ([]runtime.ContainerInfo, error) { return nil, nil }
func (m *mockRuntime) DiskUsage(ctx context.Context) (*runtime.DiskUsage, error) { return nil, nil }
```

In `internal/cli/root_test.go`, append after the `Run` method (line 100):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, kind runtime.PruneKind, all bool, filters []string) (string, error) { return "", nil }
func (m *mockRTForDeploy) ListImages(ctx context.Context, filter string) ([]runtime.ImageInfo, error) { return nil, nil }
func (m *mockRTForDeploy) ListContainers(ctx context.Context) ([]runtime.ContainerInfo, error) { return nil, nil }
func (m *mockRTForDeploy) DiskUsage(ctx context.Context) (*runtime.DiskUsage, error) { return nil, nil }
```

- [ ] **Step 6: Run all runtime tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/proxy/... ./internal/idle/... -count=1`

Expected: PASS (all new tests + existing proxy/idle tests still pass, proving the mocks conform to the extended interface).

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go internal/runtime/runtime.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: add docker housekeeping runtime primitives (prune, list, disk usage)"
```

---

### Task 2: Cleanup package (classification + orchestration)

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Create: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager` (extended in Task 1), `config.Store`, `types.DeployActive`/`types.DeployPrevious`, `runtime.ImageInfo`/`runtime.ContainerInfo`/`runtime.PruneKind`/`runtime.DiskUsage`
- Produces:
  - `type Options struct { DryRun, All, Containers, Images, Networks, Volumes, BuildCache bool; KeepImages int; App, Env string }`
  - `type Report struct { DryRun bool; Containers, Images, DanglingImages []string; ProtectedContainers, ProtectedImages int; NetworksPruned, VolumesPruned, BuildCachePruned bool; ReclaimableBefore, ReclaimableAfter string; PruneOutputs []string }`
  - `func New(store *config.Store, rt runtime.Manager, opts Options) *Housekeeper`
  - `func (h *Housekeeper) Run(ctx context.Context) (*Report, error)`
  - Pure helpers: `func classifyImages(images []runtime.ImageInfo, protected, scope map[string]bool, env string, keep int) []string`, `func containerCandidates(containers []runtime.ContainerInfo) []string`, `func protectedContainerCount(containers []runtime.ContainerInfo) int`

- [ ] **Step 1: Write the failing tests**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type fakeRT struct {
	runtime.Manager
	images     []runtime.ImageInfo
	containers []runtime.ContainerInfo
	duCalls    int
	removed    []string
	pruneCalls []runtime.PruneKind
}

func (f *fakeRT) ListImages(ctx context.Context, filter string) ([]runtime.ImageInfo, error) {
	if strings.Contains(filter, "dangling") {
		return []runtime.ImageInfo{{ID: "sha256:dangling1"}, {ID: "sha256:dangling2"}}, nil
	}
	return f.images, nil
}

func (f *fakeRT) ListContainers(ctx context.Context) ([]runtime.ContainerInfo, error) {
	return f.containers, nil
}

func (f *fakeRT) RemoveImage(ctx context.Context, imageTag string) error {
	f.removed = append(f.removed, imageTag)
	return nil
}

func (f *fakeRT) Prune(ctx context.Context, kind runtime.PruneKind, all bool, filters []string) (string, error) {
	f.pruneCalls = append(f.pruneCalls, kind)
	return "Total reclaimed space: 1.5GB\n", nil
}

func (f *fakeRT) DiskUsage(ctx context.Context) (*runtime.DiskUsage, error) {
	f.duCalls++
	if f.duCalls == 1 {
		return &runtime.DiskUsage{TotalReclaimable: "3.0GB"}, nil
	}
	return &runtime.DiskUsage{TotalReclaimable: "1.2GB"}, nil
}

func newTestStore(t *testing.T) *config.Store {
	t.Helper()
	return config.NewStoreWithEnv(t.TempDir(), "production")
}

func TestClassifyImagesKeepsProtectedAndNewest(t *testing.T) {
	images := []runtime.ImageInfo{
		{Repository: "tengiz-apps/myapp", Tag: "production-4", CreatedAt: "2026-07-30 12:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-3", CreatedAt: "2026-07-29 12:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-2", CreatedAt: "2026-07-28 12:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-1", CreatedAt: "2026-07-27 12:00:00 +0000 UTC"},
	}
	protected := map[string]bool{
		"tengiz-apps/myapp:production-1": true,
		"tengiz-apps/myapp:production-2": true,
	}
	scope := map[string]bool{"tengiz-apps/myapp": true}
	got := classifyImages(images, protected, scope, "production", 1)
	want := []string{"tengiz-apps/myapp:production-3"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("classifyImages() = %v, want %v", got, want)
	}
}

func TestClassifyImagesSkipsOtherEnvs(t *testing.T) {
	images := []runtime.ImageInfo{
		{Repository: "tengiz-apps/myapp", Tag: "production-9", CreatedAt: "2026-07-30 12:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-8", CreatedAt: "2026-07-29 12:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "staging-7", CreatedAt: "2026-07-31 12:00:00 +0000 UTC"},
	}
	scope := map[string]bool{"tengiz-apps/myapp": true}
	got := classifyImages(images, nil, scope, "production", 1)
	if len(got) != 1 || got[0] != "tengiz-apps/myapp:production-8" {
		t.Fatalf("classifyImages() = %v, want only production-8 removed", got)
	}
}

func TestClassifyImagesKeepsLatest(t *testing.T) {
	images := []runtime.ImageInfo{
		{Repository: "tengiz-apps/myapp", Tag: "production-latest", CreatedAt: "2026-07-31 12:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-5", CreatedAt: "2026-07-30 12:00:00 +0000 UTC"},
		{Repository: "tengiz-apps/myapp", Tag: "production-4", CreatedAt: "2026-07-29 12:00:00 +0000 UTC"},
	}
	scope := map[string]bool{"tengiz-apps/myapp": true}
	got := classifyImages(images, nil, scope, "production", 1)
	want := []string{"tengiz-apps/myapp:production-4"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("classifyImages() = %v, want %v", got, want)
	}
}

func TestContainerCandidatesExcludesRunningAndTengiz(t *testing.T) {
	containers := []runtime.ContainerInfo{
		{Name: "tengiz-myapp", State: "running", Labels: map[string]string{"tengiz-app": "myapp"}},
		{Name: "tengiz-myapp-pr-42", State: "exited", Labels: map[string]string{"tengiz-app": "myapp"}},
		{Name: "nginx-proxy", State: "exited", Labels: nil},
		{Name: "redis-cache", State: "running", Labels: nil},
	}
	got := containerCandidates(containers)
	if len(got) != 1 || got[0] != "nginx-proxy" {
		t.Fatalf("containerCandidates() = %v, want [nginx-proxy]", got)
	}
	if n := protectedContainerCount(containers); n != 3 {
		t.Fatalf("protectedContainerCount() = %d, want 3", n)
	}
}

func TestRunDryRunRemovesNothing(t *testing.T) {
	store := newTestStore(t)
	store.SaveApp(types.AppEntry{Name: "myapp", ImageTag: "tengiz-apps/myapp:production-1"})
	rt := &fakeRT{
		images: []runtime.ImageInfo{
			{Repository: "tengiz-apps/myapp", Tag: "production-2", CreatedAt: "2026-07-30 12:00:00 +0000 UTC"},
			{Repository: "tengiz-apps/myapp", Tag: "production-1", CreatedAt: "2026-07-29 12:00:00 +0000 UTC"},
		},
		containers: []runtime.ContainerInfo{
			{Name: "tengiz-myapp", State: "running", Labels: map[string]string{"tengiz-app": "myapp"}, Image: "tengiz-apps/myapp:production-1"},
			{Name: "orphan-nginx", State: "exited", Labels: nil},
		},
	}
	h := New(store, rt, Options{DryRun: true, KeepImages: 1})
	rep, err := h.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rt.removed) != 0 {
		t.Errorf("dry-run removed images: %v", rt.removed)
	}
	if len(rt.pruneCalls) != 0 {
		t.Errorf("dry-run performed prunes: %v", rt.pruneCalls)
	}
	if len(rep.Containers) != 1 || rep.Containers[0] != "orphan-nginx" {
		t.Errorf("rep.Containers = %v, want [orphan-nginx]", rep.Containers)
	}
	if rep.ReclaimableBefore != "3.0GB" || rep.ReclaimableAfter != "1.2GB" {
		t.Errorf("reclaimable before/after = %q/%q", rep.ReclaimableBefore, rep.ReclaimableAfter)
	}
}

func TestRunRemovesStaleImagesAndPrunes(t *testing.T) {
	store := newTestStore(t)
	store.SaveApp(types.AppEntry{Name: "myapp", ImageTag: "tengiz-apps/myapp:production-1"})
	rt := &fakeRT{
		images: []runtime.ImageInfo{
			{Repository: "tengiz-apps/myapp", Tag: "production-4", CreatedAt: "2026-07-30 12:00:00 +0000 UTC"},
			{Repository: "tengiz-apps/myapp", Tag: "production-3", CreatedAt: "2026-07-29 12:00:00 +0000 UTC"},
			{Repository: "tengiz-apps/myapp", Tag: "production-2", CreatedAt: "2026-07-28 12:00:00 +0000 UTC"},
			{Repository: "tengiz-apps/myapp", Tag: "production-1", CreatedAt: "2026-07-27 12:00:00 +0000 UTC"},
		},
		containers: []runtime.ContainerInfo{
			{Name: "tengiz-myapp", State: "running", Labels: map[string]string{"tengiz-app": "myapp"}, Image: "tengiz-apps/myapp:production-1"},
			{Name: "orphan-nginx", State: "exited", Labels: nil},
		},
	}
	h := New(store, rt, Options{KeepImages: 1})
	rep, err := h.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantRemoved := []string{"tengiz-apps/myapp:production-2", "tengiz-apps/myapp:production-3"}
	if len(rt.removed) != 2 {
		t.Fatalf("removed = %v, want %v", rt.removed, wantRemoved)
	}
	for _, w := range wantRemoved {
		found := false
		for _, r := range rt.removed {
			if r == w {
				found = true
			}
		}
		if !found {
			t.Errorf("missing removed image %s in %v", w, rt.removed)
		}
	}
	if len(rt.pruneCalls) != 3 {
		t.Fatalf("pruneCalls = %v, want 3 (containers, networks, builder)", rt.pruneCalls)
	}
	if len(rep.Containers) != 1 || rep.Containers[0] != "orphan-nginx" {
		t.Errorf("rep.Containers = %v", rep.Containers)
	}
	if !rep.NetworksPruned || !rep.BuildCachePruned {
		t.Error("networks/build cache not marked pruned")
	}
	if rep.ProtectedContainers != 1 {
		t.Errorf("ProtectedContainers = %d, want 1", rep.ProtectedContainers)
	}
}

func TestRunProtectsPreviousDeploymentForRollback(t *testing.T) {
	store := newTestStore(t)
	store.SaveApp(types.AppEntry{Name: "myapp", ImageTag: "tengiz-apps/myapp:production-3"})
	store.AddDeployment("myapp", types.DeploymentEntry{ID: "3", ImageTag: "tengiz-apps/myapp:production-3", Status: string(types.DeployActive)})
	store.AddDeployment("myapp", types.DeploymentEntry{ID: "2", ImageTag: "tengiz-apps/myapp:production-2", Status: string(types.DeployPrevious)})
	rt := &fakeRT{
		images: []runtime.ImageInfo{
			{Repository: "tengiz-apps/myapp", Tag: "production-3", CreatedAt: "2026-07-30 12:00:00 +0000 UTC"},
			{Repository: "tengiz-apps/myapp", Tag: "production-2", CreatedAt: "2026-07-29 12:00:00 +0000 UTC"},
			{Repository: "tengiz-apps/myapp", Tag: "production-1", CreatedAt: "2026-07-28 12:00:00 +0000 UTC"},
		},
	}
	h := New(store, rt, Options{KeepImages: 1})
	if _, err := h.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rt.removed) != 1 || rt.removed[0] != "tengiz-apps/myapp:production-1" {
		t.Fatalf("removed = %v, want [tengiz-apps/myapp:production-1]", rt.removed)
	}
}

func TestRunVolumesOnlyWhenRequested(t *testing.T) {
	store := newTestStore(t)
	rt := &fakeRT{}
	h := New(store, rt, Options{Volumes: true})
	rep, err := h.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.VolumesPruned {
		t.Error("volumes not pruned")
	}
	if rep.NetworksPruned || rep.BuildCachePruned || len(rep.Containers) > 0 {
		t.Error("non-volume operations ran when only volumes requested")
	}
	if len(rt.pruneCalls) != 1 || rt.pruneCalls[0] != runtime.PruneVolumes {
		t.Fatalf("pruneCalls = %v, want [volume]", rt.pruneCalls)
	}
}

func TestRunAppScopeDoesNotTouchOtherApps(t *testing.T) {
	store := newTestStore(t)
	store.SaveApp(types.AppEntry{Name: "myapp", ImageTag: "tengiz-apps/myapp:production-1"})
	store.SaveApp(types.AppEntry{Name: "otherapp", ImageTag: "tengiz-apps/otherapp:production-1"})
	rt := &fakeRT{
		images: []runtime.ImageInfo{
			{Repository: "tengiz-apps/myapp", Tag: "production-2", CreatedAt: "2026-07-30 12:00:00 +0000 UTC"},
			{Repository: "tengiz-apps/myapp", Tag: "production-1", CreatedAt: "2026-07-29 12:00:00 +0000 UTC"},
			{Repository: "tengiz-apps/otherapp", Tag: "production-2", CreatedAt: "2026-07-28 12:00:00 +0000 UTC"},
			{Repository: "tengiz-apps/otherapp", Tag: "production-1", CreatedAt: "2026-07-27 12:00:00 +0000 UTC"},
		},
	}
	h := New(store, rt, Options{App: "myapp", KeepImages: 1})
	if _, err := h.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rt.removed) != 0 {
		t.Fatalf("removed = %v, want none (otherapp out of scope)", rt.removed)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cleanup/... -count=1`

Expected: FAIL to compile with `undefined: cleanup` package / cannot find package `github.com/yaso09/tengiz/internal/cleanup`.

- [ ] **Step 3: Implement `internal/cleanup/cleanup.go`**

```go
package cleanup

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

const defaultKeepImages = 5

type Options struct {
	DryRun     bool
	All        bool
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	KeepImages int
	App        string
	Env        string
}

type Report struct {
	DryRun              bool
	Containers          []string
	Images              []string
	DanglingImages      []string
	ProtectedContainers int
	ProtectedImages     int
	NetworksPruned      bool
	VolumesPruned       bool
	BuildCachePruned    bool
	ReclaimableBefore   string
	ReclaimableAfter    string
	PruneOutputs        []string
}

type Housekeeper struct {
	store *config.Store
	rt    runtime.Manager
	opts  Options
}

func New(store *config.Store, rt runtime.Manager, opts Options) *Housekeeper {
	if opts.KeepImages <= 0 {
		opts.KeepImages = defaultKeepImages
	}
	if !opts.Containers && !opts.Images && !opts.Networks && !opts.Volumes && !opts.BuildCache {
		opts.Containers = true
		opts.Images = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return &Housekeeper{store: store, rt: rt, opts: opts}
}

func (h *Housekeeper) Run(ctx context.Context) (*Report, error) {
	rep := &Report{DryRun: h.opts.DryRun}
	if du, err := h.rt.DiskUsage(ctx); err == nil && du != nil {
		rep.ReclaimableBefore = du.TotalReclaimable
	}

	var containers []runtime.ContainerInfo
	if h.opts.Containers || h.opts.Images {
		containers, _ = h.rt.ListContainers(ctx)
	}

	if h.opts.Containers {
		rep.Containers = containerCandidates(containers)
		rep.ProtectedContainers = protectedContainerCount(containers)
		if !h.opts.DryRun && len(rep.Containers) > 0 {
			if out, err := h.rt.Prune(ctx, runtime.PruneContainers, false, []string{"label!=tengiz-app"}); err == nil {
				rep.PruneOutputs = append(rep.PruneOutputs, out)
			}
		}
	}

	if h.opts.Images {
		protected, scope := h.protectedTags(ctx)
		for _, c := range containers {
			if c.Image != "" {
				protected[c.Image] = true
			}
		}
		rep.ProtectedImages = len(protected)

		images, _ := h.rt.ListImages(ctx, "reference=tengiz-apps/*")
		rep.Images = classifyImages(images, protected, scope, h.opts.Env, h.opts.KeepImages)

		if h.opts.All {
			dangling, _ := h.rt.ListImages(ctx, "dangling=true")
			for _, d := range dangling {
				if d.ID != "" {
					rep.DanglingImages = append(rep.DanglingImages, d.ID)
				}
			}
			sort.Strings(rep.DanglingImages)
		}

		if !h.opts.DryRun {
			for _, tag := range rep.Images {
				if err := h.rt.RemoveImage(ctx, tag); err != nil {
					rep.PruneOutputs = append(rep.PruneOutputs, fmt.Sprintf("failed to remove image %s: %v", tag, err))
				}
			}
			for _, id := range rep.DanglingImages {
				if err := h.rt.RemoveImage(ctx, id); err != nil {
					rep.PruneOutputs = append(rep.PruneOutputs, fmt.Sprintf("failed to remove image %s: %v", id, err))
				}
			}
		}
	}

	if h.opts.Networks {
		rep.NetworksPruned = true
		if !h.opts.DryRun {
			if out, err := h.rt.Prune(ctx, runtime.PruneNetworks, false, nil); err == nil {
				rep.PruneOutputs = append(rep.PruneOutputs, out)
			}
		}
	}

	if h.opts.Volumes {
		rep.VolumesPruned = true
		if !h.opts.DryRun {
			if out, err := h.rt.Prune(ctx, runtime.PruneVolumes, false, nil); err == nil {
				rep.PruneOutputs = append(rep.PruneOutputs, out)
			}
		}
	}

	if h.opts.BuildCache {
		rep.BuildCachePruned = true
		if !h.opts.DryRun {
			if out, err := h.rt.Prune(ctx, runtime.PruneBuildCache, h.opts.All, nil); err == nil {
				rep.PruneOutputs = append(rep.PruneOutputs, out)
			}
		}
	}

	if du, err := h.rt.DiskUsage(ctx); err == nil && du != nil {
		rep.ReclaimableAfter = du.TotalReclaimable
	}
	return rep, nil
}

func (h *Housekeeper) protectedTags(ctx context.Context) (map[string]bool, map[string]bool) {
	protected := make(map[string]bool)
	scope := make(map[string]bool)
	apps, err := h.store.ListApps()
	if err != nil {
		return protected, scope
	}
	for _, app := range apps {
		if h.opts.App != "" && app.Name != h.opts.App {
			continue
		}
		scope["tengiz-apps/"+app.Name] = true
		if app.ImageTag != "" {
			protected[app.ImageTag] = true
		}
		if deps, err := h.store.GetDeployments(app.Name); err == nil {
			for _, d := range deps {
				if (d.Status == string(types.DeployActive) || d.Status == string(types.DeployPrevious)) && d.ImageTag != "" {
					protected[d.ImageTag] = true
				}
			}
		}
		if previews, err := h.store.ListPreviews(app.Name); err == nil {
			for _, p := range previews {
				if p.ImageTag != "" {
					protected[p.ImageTag] = true
				}
			}
		}
	}
	return protected, scope
}

func classifyImages(images []runtime.ImageInfo, protected, scope map[string]bool, env string, keep int) []string {
	byRepo := make(map[string][]runtime.ImageInfo)
	for _, img := range images {
		if img.Repository == "" || img.Tag == "" || img.Tag == "<none>" {
			continue
		}
		if !scope[img.Repository] {
			continue
		}
		byRepo[img.Repository] = append(byRepo[img.Repository], img)
	}
	var toRemove []string
	for repo, imgs := range byRepo {
		sort.Slice(imgs, func(i, j int) bool {
			return imgs[i].CreatedAt > imgs[j].CreatedAt
		})
		kept := 0
		for _, img := range imgs {
			tag := repo + ":" + img.Tag
			if protected[tag] || img.Tag == "latest" || strings.HasSuffix(img.Tag, "-latest") {
				continue
			}
			if env != "" && !strings.HasPrefix(img.Tag, env+"-") {
				continue
			}
			if kept < keep {
				kept++
				continue
			}
			toRemove = append(toRemove, tag)
		}
	}
	sort.Strings(toRemove)
	return toRemove
}

func containerCandidates(containers []runtime.ContainerInfo) []string {
	var names []string
	for _, c := range containers {
		switch c.State {
		case "running", "paused", "restarting":
			continue
		}
		if _, ok := c.Labels["tengiz-app"]; ok {
			continue
		}
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return names
}

func protectedContainerCount(containers []runtime.ContainerInfo) int {
	n := 0
	for _, c := range containers {
		if c.State == "running" {
			n++
			continue
		}
		if _, ok := c.Labels["tengiz-app"]; ok {
			n++
		}
	}
	return n
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all 10 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add docker housekeeping orchestration with label-based protection"
```

---

### Task 3: CLI command `tengiz cleanup`

**Files:**
- Modify: `internal/cli/root.go` (import `internal/cleanup`, add `cleanupCmd`, register in `init()`, define flags in `init()`, add `printCleanupReport`)
- Modify: `internal/cli/root_test.go` (mockRTForDeploy already updated in Task 1)
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.New`, `cleanup.Options`, `cleanup.Report`, `runtime.NewDocker`, `config.NewStoreWithEnv`, `getEnv(cmd)`, global `dataDir`
- Produces: `cleanupCmd *cobra.Command` (registered as `tengiz cleanup`), `printCleanupReport(rep *cleanup.Report)` helper (unexported)

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import "testing"

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsDefined(t *testing.T) {
	expected := []string{"dry-run", "all", "containers", "images", "networks", "volumes", "cache", "keep-images", "app"}
	for _, name := range expected {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Fatalf("cleanupCmd missing --%s flag", name)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupFlagsDefined" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` and "cleanup command not registered".

- [ ] **Step 3: Implement the command in `internal/cli/root.go`**

Add the import (alphabetical, after `builder`):

```go
	"github.com/yaso09/tengiz/internal/cleanup"
```

Add `rootCmd.AddCommand(cleanupCmd)` in `init()` (after `rootCmd.AddCommand(rmCmd)` at line 43) and define its flags right after the existing flag definitions in `init()` (after the `webhookCmd.Flags()` lines at line 86-88):

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("all", false, "also remove dangling images and all build cache")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune stale Tengiz images (keeps --keep-images per app)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (WARNING: destroys data)")
	cleanupCmd.Flags().Bool("cache", false, "prune build cache")
	cleanupCmd.Flags().Int("keep-images", 5, "number of images to keep per app")
	cleanupCmd.Flags().String("app", "", "scope cleanup to a single app")
```

Add the command definition (place after `rmCmd`'s closing brace, before `logsCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Reclaim disk space by pruning unused Docker resources",
	Long: `Docker housekeeping: safely remove unused containers, images, networks,
and build cache.

Tengiz-managed resources are always protected:
  - Containers labeled tengiz-app (running or stopped, including scale-to-zero targets and previews) are never pruned.
  - Image tags referenced by active or previous deployments, previews, live containers, and the <env>-latest tag are kept.
  - Stale per-app images beyond --keep-images (default 5) are removed.

By default (no flags): containers + images + networks + build cache.
Volumes are NEVER pruned unless --volumes is passed (they hold persistent data).

Use --dry-run to preview what would be removed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		networks, _ := cmd.Flags().GetBool("networks")
		volumes, _ := cmd.Flags().GetBool("volumes")
		cache, _ := cmd.Flags().GetBool("cache")
		keep, _ := cmd.Flags().GetInt("keep-images")
		app, _ := cmd.Flags().GetString("app")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		store := config.NewStoreWithEnv(dataDir, env)
		hk := cleanup.New(store, rt, cleanup.Options{
			DryRun:     dryRun,
			All:        all,
			Containers: containers,
			Images:     images,
			Networks:   networks,
			Volumes:    volumes,
			BuildCache: cache,
			KeepImages: keep,
			App:        app,
			Env:        env,
		})
		rep, err := hk.Run(cmd.Context())
		if err != nil {
			return err
		}
		printCleanupReport(rep)
		return nil
	},
}
```

Add the report printer helper (place after `getEnv`):

```go
func printCleanupReport(rep *cleanup.Report) {
	mode := "cleanup"
	if rep.DryRun {
		mode = "dry-run"
	}
	fmt.Printf("[tengiz] Docker housekeeping (%s)\n", mode)
	if len(rep.Containers) > 0 {
		fmt.Printf("[tengiz] containers: %d to remove: %s (protected: %d)\n", len(rep.Containers), strings.Join(rep.Containers, ", "), rep.ProtectedContainers)
	} else {
		fmt.Printf("[tengiz] containers: none to remove (protected: %d)\n", rep.ProtectedContainers)
	}
	if len(rep.Images) > 0 {
		fmt.Printf("[tengiz] images: %d stale to remove: %s (protected: %d)\n", len(rep.Images), strings.Join(rep.Images, ", "), rep.ProtectedImages)
	} else {
		fmt.Printf("[tengiz] images: none stale to remove (protected: %d)\n", rep.ProtectedImages)
	}
	if len(rep.DanglingImages) > 0 {
		fmt.Printf("[tengiz] dangling images: %d\n", len(rep.DanglingImages))
	}
	if rep.NetworksPruned {
		fmt.Println("[tengiz] networks: prune")
	}
	if rep.VolumesPruned {
		fmt.Println("[tengiz] volumes: prune")
	}
	if rep.BuildCachePruned {
		fmt.Println("[tengiz] build cache: prune")
	}
	fmt.Printf("[tengiz] reclaimable space: before=%s after=%s\n", rep.ReclaimableBefore, rep.ReclaimableAfter)
	for _, out := range rep.PruneOutputs {
		if trimmed := strings.TrimSpace(out); trimmed != "" {
			fmt.Println(trimmed)
		}
	}
}
```

- [ ] **Step 4: Run tests, build, and vet**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupFlagsDefined" -v -count=1`

Expected: PASS.

Run: `go build -o tengiz .`

Expected: builds with no errors.

Run: `go vet ./...`

Expected: no findings.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md` (Features list line 14-23, CLI Reference after `### tengiz rm <app>` at line 228)
- Modify: `docs/FUTURES_FEATURES.md` (line 19 P0 table row, line 377 feature section, Implemented Features table)
- Modify: `AGENTS.md` (CLI section)

**Interfaces:** Consumes the final `tengiz cleanup` CLI surface produced in Task 3. Produces documentation only.

- [ ] **Step 1: Add the feature bullet to `README.md`**

In the Features list (after line 20, the "Deployment history" bullet), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` reclaims disk space by pruning unused containers, stale images, networks, and build cache. Label-based protection never touches running or Tengiz-managed resources.
```

- [ ] **Step 2: Add the CLI reference section to `README.md`**

After the `### tengiz rm <app>` section (line 228), insert:

```markdown
### `tengiz cleanup`

Reclaim disk space by pruning unused Docker resources. Tengiz-managed resources are always protected: containers labeled `tengiz-app` (including scale-to-zero targets and previews) are never pruned, and image tags referenced by active/previous deployments, live containers, or the `<env>-latest` tag are kept. Stale per-app images beyond `--keep-images` (default 5) are removed.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--all` | Also remove dangling images and all build cache |
| `--containers` | Prune stopped non-Tengiz containers |
| `--images` | Prune stale Tengiz images (keeps `--keep-images` per app) |
| `--networks` | Prune unused networks |
| `--volumes` | Prune unused volumes (**WARNING: destroys data**; never touched by default) |
| `--cache` | Prune build cache |
| `--keep-images N` | Number of images to keep per app (default 5) |
| `--app <name>` | Scope cleanup to a single app |

With no flags, runs containers + images + networks + build cache. Volumes are never touched unless `--volumes` is passed.
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

Change line 19 from `⬜` to `✅ Implemented (2026-08-05)`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the feature detail section (line 377-381), add a status line after the "Why add to Tengiz" bullet:

```markdown
- **Status:** ✅ Implemented (2026-08-05)
```

In the "Implemented Features (Not Pending)" table (after line 252, the "Webhook ile Otomatik Deploy" row), add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-05) |
```

- [ ] **Step 4: Add the command to `AGENTS.md`**

In the CLI section, after the `tengiz build-logs` line, add:

```markdown
tengiz cleanup [--dry-run] [--all] [--containers|--images|--networks|--volumes|--cache] [--app <app>] [--keep-images N] → prune unused Docker resources with label-based protection
```

- [ ] **Step 5: Verify documentation consistency**

Run: `grep -n "cleanup" README.md AGENTS.md docs/FUTURES_FEATURES.md`

Expected: the `tengiz cleanup` command appears in all three files; feature #6 shows ✅ Implemented.

Run: `go build -o tengiz . && go test ./... -count=1`

Expected: builds; all tests pass.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document docker housekeeping (tengiz cleanup)"
```

---

## Self-Review

**Spec coverage:** Feature #6 (Docker Housekeeping) is fully covered — label-based pruning (Task 1 `label!=tengiz-app` filter + Task 2 `containerCandidates`), stale image retention with rollback protection (Task 2 `classifyImages` + `protectedTags`), unused network/volume/build-cache cleanup (Task 2 `Run`), and the `tengiz cleanup` command (Task 3). Granular per-category flags (`--containers`/`--images`/`--networks`/`--volumes`/`--cache`) foreshadow feature #56 without expanding scope. Marked implemented in FUTURES_FEATURES.md (Task 4).

**Placeholder scan:** Every step contains complete, runnable code and exact commands with expected output. No TBD/TODO/"add error handling" placeholders. All helper functions referenced in later tasks are defined in earlier tasks with identical signatures.

**Type consistency:** `runtime.PruneKind`/`PruneContainers`/`PruneImages`/`PruneNetworks`/`PruneVolumes`/`PruneBuildCache`, `runtime.ImageInfo`, `runtime.ContainerInfo`, `runtime.DiskUsage`, `cleanup.Options`, `cleanup.Report`, `cleanup.New`, `Housekeeper.Run`, `classifyImages(images, protected, scope, env, keep)`, `containerCandidates`, `protectedContainerCount`, and `printCleanupReport` are used with the exact same names and types across Tasks 1-4. `dockerRuntime` methods `Prune`, `ListImages`, `ListContainers`, `DiskUsage` match the Manager interface additions.

**Known pre-existing quirk (out of scope):** the `preview` package stores `pr-<n>-<id>` tags that differ from the actual built `production-<id>` tags. This plan does not fix it; active preview images are still protected via the live-container image-reference rule in `Housekeeper.Run`.
