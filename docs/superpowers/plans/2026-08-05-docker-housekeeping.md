# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, networks, and build cache to reclaim disk space, with label-based protection so Tengiz-managed apps are never accidentally removed.

**Architecture:** A new `internal/cleanup` package orchestrates cleanup through a narrow `runtime.CleanupManager` interface (so the core `runtime.Manager` interface and its existing mocks are untouched). The runtime package owns all `docker` CLI exec and provides pure, unit-testable argument builders and output parsers. The cleanup package holds pure selection logic (`SelectContainers`, `SelectImages`, `SelectNetworks`) and a `Runner` that lists → selects → removes, tolerating per-item failures. The CLI command surfaces a human-readable report with dry-run support.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` docker CLI calls (no Docker SDK), existing `types` package. No new external dependencies.

## Global Constraints

- Go module `github.com/yaso09/tengiz`, Go 1.26
- All Docker interaction via `os/exec` in `internal/runtime` (no Docker SDK) — must not conflict with existing `docker` CLI approach
- Container labels: `tengiz-app=<name>`, `tengiz-env=<env>`, `tengiz-deployment=<deploymentID>` — must be reused verbatim
- Deployment IDs are Unix timestamps (`fmt.Sprintf("%d", time.Now().Unix())`) — string comparison of equal-length numeric strings is a valid sort
- Do NOT modify the core `runtime.Manager` interface; define a separate `runtime.CleanupManager` so existing mocks in `internal/cli/root_test.go`, `internal/idle/idle_test.go`, `internal/proxy/proxy_test.go` keep compiling unchanged
- Protection rules are non-negotiable: never select running containers; never select the current (non-versioned) container of a Tengiz app (scale-to-zero keeps it stopped for on-demand restart); always keep the newest images and newest `--keep-deployments` deployment containers per app
- `--dry-run` must never invoke any removal; it lists the targets that would be removed and skips `docker system df`
- Default command behavior (`tengiz cleanup` with no category flag) cleans all categories
- Existing tests must continue to pass; run `go test ./... -v -count=1` and `go vet ./...` before committing
- README.md must be updated (CLI/UX change per AGENTS.md)
- Work on a feature branch: `git checkout -b feat/docker-housekeeping`
- No comments in code beyond exported-doc conventions already used in the repo

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | Add `ContainerEntry`, `ImageEntry`, `CleanupOptions`, `CleanupReport` data types |
| `internal/runtime/cleanup.go` | Add `CleanupManager` interface + `NewCleanup()` constructor (keeps existing `RemoveImage`/`KeepLastNImages`) |
| `internal/runtime/docker_cleanup.go` | New: docker exec methods + pure arg builders + pure output parsers |
| `internal/runtime/docker_cleanup_test.go` | New: unit tests for arg builders + parsers (no live docker needed) |
| `internal/cleanup/select.go` | New: pure selection logic for containers, images, networks |
| `internal/cleanup/select_test.go` | New: unit tests for selection logic |
| `internal/cleanup/runner.go` | New: orchestration `Runner` (list → select → remove, error-tolerant) |
| `internal/cleanup/runner_test.go` | New: runner tests with a fake `runtime.CleanupManager` |
| `internal/cli/root.go` | Add `cleanupCmd`, `addCleanupFlags`, `cleanupOptionsFromFlags`, `printCleanupSection`; register command |
| `internal/cli/root_test.go` | Add tests for cleanup command registration, flags, and option parsing |
| `README.md` | Add `tengiz cleanup` to Features list and CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Runtime cleanup layer (types + interface + docker exec)

**Files:**
- Modify: `internal/types/types.go` — append four types after `type AppEntry`
- Modify: `internal/runtime/cleanup.go` — append `CleanupManager` interface + `NewCleanup()`
- Create: `internal/runtime/docker_cleanup.go` — arg builders, parsers, exec methods
- Create: `internal/runtime/docker_cleanup_test.go` — tests
- Test: `go test ./internal/runtime/... -v -count=1`

**Interfaces:**
- Consumes: `internal/types` (defines the new data types), `os/exec`
- Produces: `types.ContainerEntry`, `types.ImageEntry`, `types.CleanupOptions`, `types.CleanupReport`; `runtime.CleanupManager` interface (with methods `ListContainers`, `ListImages`, `ListDanglingVolumes`, `ListNetworks`, `RemoveContainer`, `RemoveImage`, `RemoveVolume`, `RemoveNetwork`, `PruneBuildCache`, `SystemDF`); `runtime.NewCleanup() (CleanupManager, error)`; package-level unexported functions `listContainersArgs`, `listImagesArgs`, `listDanglingVolumesArgs`, `listNetworksArgs`, `pruneBuildCacheArgs`, `systemDFArgs`, `removeContainerArgs`, `removeVolumeArgs`, `removeNetworkArgs`, `parseContainerEntries`, `parseImageEntries`, `parseNameLines`

Start on the feature branch:

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 1: Write the failing arg-builder tests**

```go
// internal/runtime/docker_cleanup_test.go
package runtime

import (
	"reflect"
	"testing"
)

func TestListContainersArgs(t *testing.T) {
	got := listContainersArgs()
	want := []string{"ps", "-a", "--format", "{{json .}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listContainersArgs() = %v, want %v", got, want)
	}
}

func TestListImagesArgs(t *testing.T) {
	got := listImagesArgs()
	want := []string{"images", "--format", "{{json .}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listImagesArgs() = %v, want %v", got, want)
	}
}

func TestListDanglingVolumesArgs(t *testing.T) {
	got := listDanglingVolumesArgs()
	want := []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listDanglingVolumesArgs() = %v, want %v", got, want)
	}
}

func TestListNetworksArgs(t *testing.T) {
	got := listNetworksArgs()
	want := []string{"network", "ls", "--format", "{{.Name}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listNetworksArgs() = %v, want %v", got, want)
	}
}

func TestPruneBuildCacheArgs(t *testing.T) {
	got := pruneBuildCacheArgs()
	want := []string{"builder", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneBuildCacheArgs() = %v, want %v", got, want)
	}
}

func TestSystemDFArgs(t *testing.T) {
	got := systemDFArgs()
	want := []string{"system", "df"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("systemDFArgs() = %v, want %v", got, want)
	}
}

func TestRemoveContainerArgs(t *testing.T) {
	got := removeContainerArgs("tengiz-myapp-1712340000")
	want := []string{"rm", "-f", "tengiz-myapp-1712340000"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("removeContainerArgs() = %v, want %v", got, want)
	}
}

func TestRemoveVolumeArgs(t *testing.T) {
	got := removeVolumeArgs("myvol")
	want := []string{"volume", "rm", "myvol"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("removeVolumeArgs() = %v, want %v", got, want)
	}
}

func TestRemoveNetworkArgs(t *testing.T) {
	got := removeNetworkArgs("custom-net")
	want := []string{"network", "rm", "custom-net"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("removeNetworkArgs() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run the arg-builder tests to verify they fail**

Run: `go test ./internal/runtime/ -run TestListContainersArgs -count=1`
Expected: FAIL with `undefined: listContainersArgs`

- [ ] **Step 3: Write the failing parser tests**

```go
// appended to internal/runtime/docker_cleanup_test.go
func TestParseContainerEntries(t *testing.T) {
	data := []byte(`{"ContainerID":"abc123","Names":"/tengiz-myapp-1712340000","Status":"Exited (0) 3 days ago","Labels":"tengiz-app=myapp,tengiz-env=production,tengiz-deployment=1712340000"}
{"ContainerID":"def456","Names":"/tengiz-myapp","Status":"Up 2 hours","Labels":"tengiz-app=myapp,tengiz-env=production"}
{"ContainerID":"ghi789","Names":"/helper-xyz","Status":"Exited (0) 1 day ago","Labels":"com.example.role=helper"}
`)
	entries := parseContainerEntries(data)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	old := entries[0]
	if old.Name != "tengiz-myapp-1712340000" || old.App != "myapp" || old.Deployment != "1712340000" || old.Env != "production" {
		t.Errorf("unexpected old deployment entry: %+v", old)
	}
	if old.State != "exited" {
		t.Errorf("old deployment state = %q, want exited", old.State)
	}
	current := entries[1]
	if current.Name != "tengiz-myapp" || current.App != "myapp" || current.Deployment != "" {
		t.Errorf("unexpected current entry: %+v", current)
	}
	if current.State != "running" {
		t.Errorf("current state = %q, want running", current.State)
	}
	helper := entries[2]
	if helper.Name != "helper-xyz" || helper.App != "" {
		t.Errorf("unexpected helper entry: %+v", helper)
	}
}

func TestParseImageEntries(t *testing.T) {
	data := []byte(`{"ID":"sha256:aaaa","Repository":"tengiz-apps/myapp","Tag":"1712340000","CreatedAt":"2026-07-01 10:00:00 +0000 UTC"}
{"ID":"sha256:bbbb","Repository":"tengiz-apps/myapp","Tag":"latest","CreatedAt":"2026-07-02 10:00:00 +0000 UTC"}
{"ID":"sha256:cccc","Repository":"<none>","Tag":"<none>","CreatedAt":"2026-07-03 10:00:00 +0000 UTC"}
{"ID":"sha256:dddd","Repository":"node","Tag":"18-alpine","CreatedAt":"2026-06-01 10:00:00 +0000 UTC"}
`)
	entries := parseImageEntries(data)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	if entries[0].Repository != "tengiz-apps/myapp" || entries[0].Tag != "1712340000" {
		t.Errorf("unexpected image entry 0: %+v", entries[0])
	}
	if entries[2].Repository != "<none>" {
		t.Errorf("dangling repository = %q, want <none>", entries[2].Repository)
	}
	if entries[3].Repository != "node" {
		t.Errorf("base image repository = %q, want node", entries[3].Repository)
	}
}

func TestParseNameLines(t *testing.T) {
	data := []byte("vol-a\nvol-b\n")
	got := parseNameLines(data)
	want := []string{"vol-a", "vol-b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseNameLines() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 4: Run the parser tests to verify they fail**

Run: `go test ./internal/runtime/ -run TestParseContainerEntries -count=1`
Expected: FAIL with `undefined: parseContainerEntries`

- [ ] **Step 5: Write the failing interface/type compile check**

Because these are compilations rather than runtime assertions, write a compile-time guard test that proves the concrete docker runtime satisfies the interface once implemented:

```go
// appended to internal/runtime/docker_cleanup_test.go
func TestDockerRuntimeSatisfiesCleanupManager(t *testing.T) {
	var m CleanupManager = &dockerRuntime{}
	if m == nil {
		t.Fatal("dockerRuntime does not implement CleanupManager")
	}
}
```

- [ ] **Step 6: Implement the data types**

Append to `internal/types/types.go` (after the `AppEntry` type):

```go
// ContainerEntry is a single container discovered for cleanup, with its
// Tengiz labels decoded.
type ContainerEntry struct {
	Name       string
	App        string // value of tengiz-app label; "" if not Tengiz-managed
	Deployment string // value of tengiz-deployment label; "" if not a versioned deployment
	Env        string // value of tengiz-env label
	State      string // running | exited | dead | ...
}

// ImageEntry is a single image discovered for cleanup.
type ImageEntry struct {
	ID         string
	Repository string
	Tag        string
	CreatedAt  string
}

// CleanupOptions selects which resource categories to prune.
type CleanupOptions struct {
	Containers      bool
	Images          bool
	Volumes         bool
	Networks        bool
	BuildCache      bool
	DryRun          bool
	KeepDeployments int // versioned deployment containers kept per app (default 1)
	KeepImages      int // images kept per app (default 5)
}

// CleanupReport summarizes what a cleanup run removed.
type CleanupReport struct {
	DryRun              bool     `json:"dry_run"`
	ContainersRemoved   []string `json:"containers_removed,omitempty"`
	ImagesRemoved       []string `json:"images_removed,omitempty"`
	VolumesRemoved      []string `json:"volumes_removed,omitempty"`
	NetworksRemoved     []string `json:"networks_removed,omitempty"`
	BuildCacheReclaimed string   `json:"build_cache_reclaimed,omitempty"`
	SystemDF            string   `json:"system_df,omitempty"`
	Errors              []string `json:"errors,omitempty"`
}
```

Run: `go test ./internal/runtime/ -run TestDockerRuntimeSatisfiesCleanupManager -count=1`
Expected: FAIL with `dockerRuntime does not implement CleanupManager` (or undefined method compile error) until the interface and exec methods exist.

- [ ] **Step 7: Add the `CleanupManager` interface and constructor**

Append to `internal/runtime/cleanup.go`:

```go
// CleanupManager abstracts the subset of docker operations needed by the
// cleanup runner. Keeping it separate from Manager means the cleanup feature
// does not force unrelated Manager implementations (test stubs/mocks) to add
// methods they do not need.
type CleanupManager interface {
	ListContainers(ctx context.Context) ([]types.ContainerEntry, error)
	ListImages(ctx context.Context) ([]types.ImageEntry, error)
	ListDanglingVolumes(ctx context.Context) ([]string, error)
	ListNetworks(ctx context.Context) ([]string, error)
	RemoveContainer(ctx context.Context, name string) error
	RemoveImage(ctx context.Context, imageTag string) error
	RemoveVolume(ctx context.Context, name string) error
	RemoveNetwork(ctx context.Context, name string) error
	PruneBuildCache(ctx context.Context) (string, error)
	SystemDF(ctx context.Context) (string, error)
}

// NewCleanup returns a CleanupManager backed by the local Docker daemon.
func NewCleanup() (CleanupManager, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerRuntime{}, nil
}
```

`internal/runtime/cleanup.go` already imports `"context"`, `"fmt"`, `"os/exec"`, `"log"`, `"sort"`, `"strings"` and `"github.com/yaso09/tengiz/internal/types"` — no import changes needed here.

- [ ] **Step 8: Implement the arg builders, parsers, and exec methods**

Create `internal/runtime/docker_cleanup.go`:

```go
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

const deploymentLabelKey = "tengiz-deployment"

func listContainersArgs() []string {
	return []string{"ps", "-a", "--format", "{{json .}}"}
}

func listImagesArgs() []string {
	return []string{"images", "--format", "{{json .}}"}
}

func listDanglingVolumesArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func listNetworksArgs() []string {
	return []string{"network", "ls", "--format", "{{.Name}}"}
}

func pruneBuildCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func systemDFArgs() []string {
	return []string{"system", "df"}
}

func removeContainerArgs(name string) []string {
	return []string{"rm", "-f", name}
}

func removeVolumeArgs(name string) []string {
	return []string{"volume", "rm", name}
}

func removeNetworkArgs(name string) []string {
	return []string{"network", "rm", name}
}

type dockerPSFull struct {
	ContainerID string `json:"ContainerID"`
	Image       string `json:"Image"`
	ImageID     string `json:"ImageID"`
	Command     string `json:"Command"`
	CreatedAt   string `json:"CreatedAt"`
	RunningFor  string `json:"RunningFor"`
	Ports       string `json:"Ports"`
	Status      string `json:"Status"`
	Size        string `json:"Size"`
	Names       string `json:"Names"`
	Labels      string `json:"Labels"`
	Mounts      string `json:"Mounts"`
	Networks    string `json:"Networks"`
}

func containerStateFromStatus(status string) string {
	switch {
	case strings.HasPrefix(status, "Up "):
		return "running"
	case strings.HasPrefix(status, "Dead"):
		return "dead"
	case strings.HasPrefix(status, "Exited"):
		return "exited"
	default:
		return status
	}
}

func parseContainerEntries(data []byte) []types.ContainerEntry {
	var entries []types.ContainerEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var raw dockerPSFull
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		name := strings.TrimPrefix(raw.Names, "/")
		if name == "" {
			name = raw.ContainerID
		}
		var app, deployment, env string
		for _, part := range strings.Split(raw.Labels, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case labelKey:
				app = kv[1]
			case deploymentLabelKey:
				deployment = kv[1]
			case envLabelKey:
				env = kv[1]
			}
		}
		entries = append(entries, types.ContainerEntry{
			Name:       name,
			App:        app,
			Deployment: deployment,
			Env:        env,
			State:      containerStateFromStatus(raw.Status),
		})
	}
	return entries
}

type dockerImage struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	CreatedAt  string `json:"CreatedAt"`
}

func parseImageEntries(data []byte) []types.ImageEntry {
	var entries []types.ImageEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var raw dockerImage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		entries = append(entries, types.ImageEntry{
			ID:         raw.ID,
			Repository: raw.Repository,
			Tag:        raw.Tag,
			CreatedAt:  raw.CreatedAt,
		})
	}
	return entries
}

func parseNameLines(data []byte) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names
}

func (r *dockerRuntime) ListContainers(ctx context.Context) ([]types.ContainerEntry, error) {
	cmd := exec.CommandContext(ctx, "docker", listContainersArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return parseContainerEntries(out), nil
}

func (r *dockerRuntime) ListImages(ctx context.Context) ([]types.ImageEntry, error) {
	cmd := exec.CommandContext(ctx, "docker", listImagesArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return parseImageEntries(out), nil
}

func (r *dockerRuntime) ListDanglingVolumes(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", listDanglingVolumesArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	return parseNameLines(out), nil
}

func (r *dockerRuntime) ListNetworks(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", listNetworksArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return parseNameLines(out), nil
}

func (r *dockerRuntime) RemoveContainer(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", removeContainerArgs(name)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm %s: %w\n%s", name, err, string(out))
	}
	return nil
}

func (r *dockerRuntime) RemoveVolume(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", removeVolumeArgs(name)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume rm %s: %w\n%s", name, err, string(out))
	}
	return nil
}

func (r *dockerRuntime) RemoveNetwork(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", removeNetworkArgs(name)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network rm %s: %w\n%s", name, err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", pruneBuildCacheArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *dockerRuntime) SystemDF(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", systemDFArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
```

Note: `labelKey` and `envLabelKey` are already declared as package-level constants in `internal/runtime/docker.go`. `RemoveImage` already exists on `*dockerRuntime` in `internal/runtime/cleanup.go`; the new `deploymentLabelKey` constant is defined here.

- [ ] **Step 9: Run the runtime tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS for the cleanup arg-builder, parser, and interface-guard tests (plus all pre-existing runtime tests)

- [ ] **Step 10: Commit**

```bash
git add internal/types/types.go internal/runtime/cleanup.go internal/runtime/docker_cleanup.go internal/runtime/docker_cleanup_test.go
git commit -m "feat(cleanup): add runtime docker cleanup layer and types"
```

---

### Task 2: Cleanup selection logic

**Files:**
- Create: `internal/cleanup/select.go`
- Create: `internal/cleanup/select_test.go`
- Test: `go test ./internal/cleanup/... -v -count=1`

**Interfaces:**
- Consumes: `types.ContainerEntry`, `types.ImageEntry` (from Task 1)
- Produces: package functions `SelectContainers(entries []types.ContainerEntry, keepDeployments int) []string`, `SelectImages(entries []types.ImageEntry, keepPerApp int) []string`, `SelectNetworks(all []string) []string` — consumed by `Runner` in Task 3

- [ ] **Step 1: Write the failing selection tests**

```go
// internal/cleanup/select_test.go
package cleanup

import (
	"reflect"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestSelectContainers(t *testing.T) {
	entries := []types.ContainerEntry{
		{Name: "helper-running", State: "running"},
		{Name: "helper-stopped", State: "exited"},
		{Name: "tengiz-myapp", App: "myapp", State: "exited"},
		{Name: "tengiz-api", App: "api", State: "running"},
		{Name: "tengiz-myapp-1712340000", App: "myapp", Deployment: "1712340000", State: "exited"},
		{Name: "tengiz-myapp-1712341000", App: "myapp", Deployment: "1712341000", State: "exited"},
		{Name: "tengiz-myapp-1712342000", App: "myapp", Deployment: "1712342000", State: "exited"},
	}

	got := SelectContainers(entries, 1)
	want := []string{"helper-stopped", "tengiz-myapp-1712340000", "tengiz-myapp-1712341000"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SelectContainers() = %v, want %v", got, want)
	}
}

func TestSelectContainersKeepMultipleDeployments(t *testing.T) {
	entries := []types.ContainerEntry{
		{Name: "tengiz-myapp-100", App: "myapp", Deployment: "100", State: "exited"},
		{Name: "tengiz-myapp-200", App: "myapp", Deployment: "200", State: "exited"},
		{Name: "tengiz-myapp-300", App: "myapp", Deployment: "300", State: "exited"},
	}
	got := SelectContainers(entries, 2)
	want := []string{"tengiz-myapp-100"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SelectContainers(keep=2) = %v, want %v", got, want)
	}
}

func TestSelectContainersRunningProtected(t *testing.T) {
	entries := []types.ContainerEntry{
		{Name: "tengiz-myapp-100", App: "myapp", Deployment: "100", State: "running"},
	}
	got := SelectContainers(entries, 1)
	if len(got) != 0 {
		t.Errorf("running deployment container must be protected, got %v", got)
	}
}

func TestSelectImages(t *testing.T) {
	entries := []types.ImageEntry{
		{ID: "deadbeef", Repository: "<none>", Tag: "<none>", CreatedAt: "2026-07-01"},
		{ID: "aaaa", Repository: "tengiz-apps/myapp", Tag: "100", CreatedAt: "2026-07-01"},
		{ID: "bbbb", Repository: "tengiz-apps/myapp", Tag: "200", CreatedAt: "2026-07-02"},
		{ID: "cccc", Repository: "tengiz-apps/myapp", Tag: "300", CreatedAt: "2026-07-03"},
		{ID: "dddd", Repository: "tengiz-apps/myapp", Tag: "latest", CreatedAt: "2026-07-04"},
		{ID: "eeee", Repository: "node", Tag: "18-alpine", CreatedAt: "2026-06-01"},
	}
	got := SelectImages(entries, 1)
	want := []string{"deadbeef", "tengiz-apps/myapp:100", "tengiz-apps/myapp:200"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SelectImages() = %v, want %v", got, want)
	}
}

func TestSelectImagesKeepsLatestTagAlways(t *testing.T) {
	entries := []types.ImageEntry{
		{ID: "aaaa", Repository: "tengiz-apps/myapp", Tag: "latest", CreatedAt: "2026-07-01"},
		{ID: "bbbb", Repository: "tengiz-apps/myapp", Tag: "100", CreatedAt: "2026-07-01"},
	}
	got := SelectImages(entries, 1)
	want := []string{"tengiz-apps/myapp:100"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SelectImages(latest only, keep 1) = %v, want %v", got, want)
	}
}

func TestSelectNetworks(t *testing.T) {
	all := []string{"bridge", "host", "none", "custom-net", "tengiz-net"}
	got := SelectNetworks(all)
	want := []string{"custom-net", "tengiz-net"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SelectNetworks() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run the selection tests to verify they fail**

Run: `go test ./internal/cleanup/ -run TestSelectContainers -count=1`
Expected: FAIL with `undefined: SelectContainers`

- [ ] **Step 3: Implement the selection logic**

```go
// internal/cleanup/select.go
package cleanup

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

// SelectContainers returns the names of containers that should be removed.
// Protection rules:
//   - running containers are never selected
//   - the current (non-versioned) container of a Tengiz app is never selected,
//     even when stopped (scale-to-zero keeps it for on-demand restarts)
//   - versioned deployment containers (tengiz-deployment label) older than the
//     newest keepDeployments per app are selected
//   - stopped containers not managed by Tengiz (no tengiz-app label) are selected
func SelectContainers(entries []types.ContainerEntry, keepDeployments int) []string {
	if keepDeployments < 1 {
		keepDeployments = 1
	}
	byApp := make(map[string][]types.ContainerEntry)
	for _, e := range entries {
		if e.App != "" && e.Deployment != "" {
			byApp[e.App] = append(byApp[e.App], e)
		}
	}
	// Deployment IDs are Unix timestamps, so descending string order == newest first.
	keep := make(map[string]bool)
	for app, deployments := range byApp {
		sort.Slice(deployments, func(i, j int) bool {
			return deployments[i].Deployment > deployments[j].Deployment
		})
		for i := 0; i < len(deployments) && i < keepDeployments; i++ {
			keep[deployments[i].Name] = true
		}
	}
	var removed []string
	for _, e := range entries {
		if e.State == "running" {
			continue
		}
		if e.App == "" {
			removed = append(removed, e.Name)
			continue
		}
		if e.Deployment == "" {
			continue // current Tengiz container
		}
		if !keep[e.Name] {
			removed = append(removed, e.Name)
		}
	}
	sort.Strings(removed)
	return removed
}

// SelectImages returns references to remove: dangling images by image ID, and
// "tengiz-apps/*" images older than the newest keepPerApp per app. Non Tengiz
// repositories (e.g. base images) are never selected. The "latest" tag of each
// app is always kept.
func SelectImages(entries []types.ImageEntry, keepPerApp int) []string {
	if keepPerApp < 1 {
		keepPerApp = 1
	}
	byRepo := make(map[string][]types.ImageEntry)
	for _, e := range entries {
		if strings.HasPrefix(e.Repository, "tengiz-apps/") {
			byRepo[e.Repository] = append(byRepo[e.Repository], e)
		}
	}
	var removed []string
	for _, e := range entries {
		if e.Repository == "" || e.Repository == "<none>" {
			if e.ID != "" {
				removed = append(removed, e.ID)
			}
		}
	}
	for repo, images := range byRepo {
		// Newest first so we keep the newest keepPerApp.
		sort.Slice(images, func(i, j int) bool {
			return images[i].CreatedAt > images[j].CreatedAt
		})
		kept := 0
		for _, img := range images {
			if img.Tag == "latest" {
				continue
			}
			if kept < keepPerApp {
				kept++
				continue
			}
			removed = append(removed, fmt.Sprintf("%s:%s", repo, img.Tag))
		}
	}
	sort.Strings(removed)
	return removed
}

var builtinNetworks = map[string]bool{
	"bridge": true, "host": true, "none": true,
	"ingress": true, "docker_gwbridge": true,
}

// SelectNetworks returns the names of non-built-in networks. Docker refuses to
// remove networks still in use, so removal failures for in-use networks are
// expected and tolerated by the caller.
func SelectNetworks(all []string) []string {
	var selected []string
	for _, name := range all {
		if !builtinNetworks[name] {
			selected = append(selected, name)
		}
	}
	sort.Strings(selected)
	return selected
}
```

- [ ] **Step 4: Run the selection tests to verify they pass**

Run: `go test ./internal/cleanup/ -v -count=1`
Expected: PASS for all selection tests

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/select.go internal/cleanup/select_test.go
git commit -m "feat(cleanup): add label-based selection logic for containers, images, networks"
```

---

### Task 3: Cleanup runner

**Files:**
- Create: `internal/cleanup/runner.go`
- Create: `internal/cleanup/runner_test.go`
- Test: `go test ./internal/cleanup/... -v -count=1`

**Interfaces:**
- Consumes: `runtime.CleanupManager` (Task 1), selection functions (Task 2)
- Produces: `cleanup.NewRunner(rt runtime.CleanupManager) *Runner`, `(*Runner).Run(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error)` — consumed by the CLI in Task 4

- [ ] **Step 1: Write the failing runner tests**

```go
// internal/cleanup/runner_test.go
package cleanup

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type fakeManager struct {
	containers          []types.ContainerEntry
	images              []types.ImageEntry
	volumes             []string
	networks            []string
	removedContainers   []string
	removedImages       []string
	removedVolumes      []string
	removedNetworks     []string
	pruneCacheOutput    string
	systemDFOutput      string
	failNetworkRemoval  bool
}

func (f *fakeManager) ListContainers(ctx context.Context) ([]types.ContainerEntry, error) { return f.containers, nil }
func (f *fakeManager) ListImages(ctx context.Context) ([]types.ImageEntry, error)          { return f.images, nil }
func (f *fakeManager) ListDanglingVolumes(ctx context.Context) ([]string, error)           { return f.volumes, nil }
func (f *fakeManager) ListNetworks(ctx context.Context) ([]string, error)                  { return f.networks, nil }
func (f *fakeManager) RemoveContainer(ctx context.Context, name string) error              { f.removedContainers = append(f.removedContainers, name); return nil }
func (f *fakeManager) RemoveImage(ctx context.Context, imageTag string) error               { f.removedImages = append(f.removedImages, imageTag); return nil }
func (f *fakeManager) RemoveVolume(ctx context.Context, name string) error                 { f.removedVolumes = append(f.removedVolumes, name); return nil }
func (f *fakeManager) RemoveNetwork(ctx context.Context, name string) error {
	f.removedNetworks = append(f.removedNetworks, name)
	if f.failNetworkRemoval {
		return fmt.Errorf("network is in use")
	}
	return nil
}
func (f *fakeManager) PruneBuildCache(ctx context.Context) (string, error) { return f.pruneCacheOutput, nil }
func (f *fakeManager) SystemDF(ctx context.Context) (string, error)        { return f.systemDFOutput, nil }

func TestRunnerRunAllCategories(t *testing.T) {
	f := &fakeManager{
		containers: []types.ContainerEntry{
			{Name: "helper-stopped", State: "exited"},
			{Name: "tengiz-myapp", App: "myapp", State: "exited"},
		},
		images: []types.ImageEntry{
			{ID: "deadbeef", Repository: "<none>", Tag: "<none>"},
		},
		volumes:          []string{"vol-a"},
		networks:         []string{"custom-net"},
		pruneCacheOutput: "Total reclaimed space: 1.2MB",
		systemDFOutput:   "Images: ...",
	}
	r := NewRunner(f)
	report, err := r.Run(context.Background(), types.CleanupOptions{
		Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(report.ContainersRemoved, []string{"helper-stopped"}) {
		t.Errorf("containers removed = %v", report.ContainersRemoved)
	}
	if !reflect.DeepEqual(f.removedContainers, []string{"helper-stopped"}) {
		t.Errorf("fake removedContainers = %v", f.removedContainers)
	}
	if !reflect.DeepEqual(report.ImagesRemoved, []string{"deadbeef"}) {
		t.Errorf("images removed = %v", report.ImagesRemoved)
	}
	if !reflect.DeepEqual(report.VolumesRemoved, []string{"vol-a"}) {
		t.Errorf("volumes removed = %v", report.VolumesRemoved)
	}
	if !reflect.DeepEqual(report.NetworksRemoved, []string{"custom-net"}) {
		t.Errorf("networks removed = %v", report.NetworksRemoved)
	}
	if report.BuildCacheReclaimed != "Total reclaimed space: 1.2MB" {
		t.Errorf("build cache reclaimed = %q", report.BuildCacheReclaimed)
	}
	if report.SystemDF != "Images: ..." {
		t.Errorf("SystemDF output not captured: %q", report.SystemDF)
	}
}

func TestRunnerDryRunDoesNotRemove(t *testing.T) {
	f := &fakeManager{
		containers:      []types.ContainerEntry{{Name: "helper-stopped", State: "exited"}},
		volumes:         []string{"vol-a"},
		systemDFOutput:  "SIZE 100MB",
	}
	r := NewRunner(f)
	report, err := r.Run(context.Background(), types.CleanupOptions{
		Containers: true, Volumes: true, DryRun: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(report.ContainersRemoved, []string{"helper-stopped"}) {
		t.Errorf("dry-run containers = %v", report.ContainersRemoved)
	}
	if len(f.removedContainers) != 0 || len(f.removedVolumes) != 0 {
		t.Errorf("dry-run must not remove anything, got %v / %v", f.removedContainers, f.removedVolumes)
	}
	if report.SystemDF != "" {
		t.Errorf("dry-run must not query SystemDF, got %q", report.SystemDF)
	}
}

func TestRunnerImageSelectionUsesKeepImages(t *testing.T) {
	f := &fakeManager{
		images: []types.ImageEntry{
			{ID: "aaaa", Repository: "tengiz-apps/myapp", Tag: "100", CreatedAt: "2026-07-01"},
			{ID: "bbbb", Repository: "tengiz-apps/myapp", Tag: "200", CreatedAt: "2026-07-02"},
		},
	}
	r := NewRunner(f)
	report, err := r.Run(context.Background(), types.CleanupOptions{Images: true, KeepImages: 1})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(report.ImagesRemoved, []string{"tengiz-apps/myapp:100"}) {
		t.Errorf("images removed = %v", report.ImagesRemoved)
	}
}

func TestRunnerNetworkRemovalErrorTolerated(t *testing.T) {
	f := &fakeManager{networks: []string{"inuse-net"}, failNetworkRemoval: true}
	r := NewRunner(f)
	report, err := r.Run(context.Background(), types.CleanupOptions{Networks: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.NetworksRemoved) != 0 {
		t.Errorf("networks removed = %v, want none", report.NetworksRemoved)
	}
	if len(report.Errors) != 1 {
		t.Errorf("errors = %v, want 1", report.Errors)
	}
}

func TestRunnerSatisfyCleanupManagerGuard(t *testing.T) {
	var _ runtime.CleanupManager = (*fakeManager)(nil)
}
```

- [ ] **Step 2: Run the runner tests to verify they fail**

Run: `go test ./internal/cleanup/ -run TestRunnerRunAllCategories -count=1`
Expected: FAIL with `undefined: NewRunner` (or `undefined: Runner`)

- [ ] **Step 3: Implement the runner**

```go
// internal/cleanup/runner.go
package cleanup

import (
	"context"
	"fmt"

	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

// Runner executes cleanup across reported resource categories.
type Runner struct {
	rt runtime.CleanupManager
}

// NewRunner returns a cleanup Runner backed by the given docker manager.
func NewRunner(rt runtime.CleanupManager) *Runner {
	return &Runner{rt: rt}
}

// Run performs the requested cleanup. Per-item removal failures are collected
// into report.Errors and do not abort the remaining work. In dry-run mode, the
// report lists targets that would be removed but nothing is removed and
// docker system df is not queried.
func (r *Runner) Run(ctx context.Context, opts types.CleanupOptions) (types.CleanupReport, error) {
	report := types.CleanupReport{DryRun: opts.DryRun}

	if opts.Containers {
		entries, err := r.rt.ListContainers(ctx)
		if err != nil {
			return report, fmt.Errorf("list containers: %w", err)
		}
		for _, name := range SelectContainers(entries, opts.KeepDeployments) {
			if opts.DryRun {
				report.ContainersRemoved = append(report.ContainersRemoved, name)
				continue
			}
			if err := r.rt.RemoveContainer(ctx, name); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("container %s: %v", name, err))
				continue
			}
			report.ContainersRemoved = append(report.ContainersRemoved, name)
		}
	}

	if opts.Images {
		entries, err := r.rt.ListImages(ctx)
		if err != nil {
			return report, fmt.Errorf("list images: %w", err)
		}
		for _, ref := range SelectImages(entries, opts.KeepImages) {
			if opts.DryRun {
				report.ImagesRemoved = append(report.ImagesRemoved, ref)
				continue
			}
			if err := r.rt.RemoveImage(ctx, ref); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("image %s: %v", ref, err))
				continue
			}
			report.ImagesRemoved = append(report.ImagesRemoved, ref)
		}
	}

	if opts.Volumes {
		volumes, err := r.rt.ListDanglingVolumes(ctx)
		if err != nil {
			return report, fmt.Errorf("list volumes: %w", err)
		}
		for _, name := range volumes {
			if opts.DryRun {
				report.VolumesRemoved = append(report.VolumesRemoved, name)
				continue
			}
			if err := r.rt.RemoveVolume(ctx, name); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("volume %s: %v", name, err))
				continue
			}
			report.VolumesRemoved = append(report.VolumesRemoved, name)
		}
	}

	if opts.Networks {
		all, err := r.rt.ListNetworks(ctx)
		if err != nil {
			return report, fmt.Errorf("list networks: %w", err)
		}
		for _, name := range SelectNetworks(all) {
			if opts.DryRun {
				report.NetworksRemoved = append(report.NetworksRemoved, name)
				continue
			}
			if err := r.rt.RemoveNetwork(ctx, name); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("network %s: %v", name, err))
				continue
			}
			report.NetworksRemoved = append(report.NetworksRemoved, name)
		}
	}

	if opts.BuildCache {
		if opts.DryRun {
			report.BuildCacheReclaimed = "build cache prune skipped (dry-run)"
		} else {
			out, err := r.rt.PruneBuildCache(ctx)
			if err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("build cache: %v", err))
			} else {
				report.BuildCacheReclaimed = out
			}
		}
	}

	if !opts.DryRun && (opts.Images || opts.Containers || opts.Volumes || opts.Networks) {
		if df, err := r.rt.SystemDF(ctx); err == nil {
			report.SystemDF = df
		}
	}

	return report, nil
}
```

- [ ] **Step 4: Run the runner tests to verify they pass**

Run: `go test ./internal/cleanup/ -v -count=1`
Expected: PASS for all runner tests

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/runner.go internal/cleanup/runner_test.go
git commit -m "feat(cleanup): add cleanup runner with dry-run and error tolerance"
```

---

### Task 4: CLI command + documentation

**Files:**
- Modify: `internal/cli/root.go` — add import, `cleanupCmd`, `addCleanupFlags`, `cleanupOptionsFromFlags`, `printCleanupSection`, registration
- Modify: `internal/cli/root_test.go` — add cleanup tests
- Modify: `README.md` — Features bullet + CLI Reference section
- Test: `go test ./internal/cli/... -v -count=1`

**Interfaces:**
- Consumes: `runtime.NewCleanup() runtime.CleanupManager` (Task 1), `cleanup.NewRunner` + `(*Runner).Run` (Task 3), `types.CleanupOptions/CleanupReport` (Task 1)
- Produces: `tengiz cleanup` command; `cleanupOptionsFromFlags(cmd *cobra.Command) types.CleanupOptions` (testable helper)

- [ ] **Step 1: Write the failing CLI tests**

```go
// added to internal/cli/root_test.go

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsExist(t *testing.T) {
	for _, name := range []string{"all", "containers", "images", "volumes", "networks", "cache", "dry-run", "keep-images", "keep-deployments"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupOptionsFromFlagsDefault(t *testing.T) {
	cmd := &cobra.Command{}
	addCleanupFlags(cmd)
	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("default opts must clean all categories: %+v", opts)
	}
	if opts.DryRun {
		t.Error("dry-run must default to false")
	}
	if opts.KeepImages != 5 {
		t.Errorf("KeepImages default = %d, want 5", opts.KeepImages)
	}
	if opts.KeepDeployments != 1 {
		t.Errorf("KeepDeployments default = %d, want 1", opts.KeepDeployments)
	}
}

func TestCleanupOptionsFromFlagsExplicitCategoryDisablesAll(t *testing.T) {
	cmd := &cobra.Command{}
	addCleanupFlags(cmd)
	if err := cmd.Flags().Parse([]string{"--containers", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	opts := cleanupOptionsFromFlags(cmd)
	if !opts.Containers {
		t.Error("containers must be enabled")
	}
	if opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("explicit category must disable others: %+v", opts)
	}
	if !opts.DryRun {
		t.Error("dry-run must be true")
	}
}
```

The test file already imports `"github.com/spf13/cobra"` (line 13 of `root_test.go`), so no new imports are needed.

- [ ] **Step 2: Run the CLI tests to verify they fail**

Run: `go test ./internal/cli/ -run TestCleanupCommandRegistered -count=1`
Expected: FAIL (`cleanup command not registered`) or compile error for `cleanupCmd`/`addCleanupFlags` being undefined — that is the expected failure state for this TDD step.

- [ ] **Step 3: Add the cleanup command to `root.go`**

Add the import `"github.com/yaso09/tengiz/internal/cleanup"` to the import block of `internal/cli/root.go` (after `"github.com/yaso09/tengiz/internal/builder"`).

Add the following to `internal/cli/root.go` (place it just after the `rmCmd` definition and before `logsCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: "Clean up Docker resources to reclaim disk space. Tengiz-managed containers and images " +
		"are protected by labels: running containers, each app's current container (even when " +
		"stopped for scale-to-zero), and the newest images are always kept.",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewCleanup()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		runner := cleanup.NewRunner(rt)
		report, runErr := runner.Run(context.Background(), cleanupOptionsFromFlags(cmd))

		if report.DryRun {
			fmt.Println("[tengiz] dry-run: the following would be removed")
		}
		printCleanupSection(report.DryRun, "container(s)", report.ContainersRemoved)
		printCleanupSection(report.DryRun, "image(s)", report.ImagesRemoved)
		printCleanupSection(report.DryRun, "volume(s)", report.VolumesRemoved)
		printCleanupSection(report.DryRun, "network(s)", report.NetworksRemoved)
		if report.BuildCacheReclaimed != "" {
			fmt.Printf("Build cache: %s\n", report.BuildCacheReclaimed)
		}
		if report.SystemDF != "" {
			fmt.Printf("\nDocker disk usage:\n%s", report.SystemDF)
		}
		for _, e := range report.Errors {
			log.Printf("[tengiz] cleanup warning: %v", e)
		}
		return runErr
	},
}

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("all", true, "clean all categories (default)")
	cmd.Flags().Bool("containers", false, "remove stopped/orphaned containers (label-protected)")
	cmd.Flags().Bool("images", false, "remove dangling and old app images (keeps newest per app)")
	cmd.Flags().Bool("volumes", false, "remove unused volumes")
	cmd.Flags().Bool("networks", false, "remove unused networks")
	cmd.Flags().Bool("cache", false, "prune Docker build cache")
	cmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cmd.Flags().Int("keep-images", 5, "number of images to keep per app")
	cmd.Flags().Int("keep-deployments", 1, "number of versioned deployment containers to keep per app")
}

func cleanupOptionsFromFlags(cmd *cobra.Command) types.CleanupOptions {
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	cache, _ := cmd.Flags().GetBool("cache")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	keepImages, _ := cmd.Flags().GetInt("keep-images")
	keepDeployments, _ := cmd.Flags().GetInt("keep-deployments")

	if cmd.Flags().Changed("containers") || cmd.Flags().Changed("images") ||
		cmd.Flags().Changed("volumes") || cmd.Flags().Changed("networks") || cmd.Flags().Changed("cache") {
		all = false
	}

	return types.CleanupOptions{
		Containers:      all || containers,
		Images:          all || images,
		Volumes:         all || volumes,
		Networks:        all || networks,
		BuildCache:      all || cache,
		DryRun:          dryRun,
		KeepImages:      keepImages,
		KeepDeployments: keepDeployments,
	}
}

func printCleanupSection(dryRun bool, label string, items []string) {
	if len(items) == 0 {
		return
	}
	verb := "Removed"
	if dryRun {
		verb = "Would remove"
	}
	fmt.Printf("%s %d %s:\n", verb, len(items), label)
	for _, item := range items {
		fmt.Printf("  - %s\n", item)
	}
}
```

Note: `root.go` already imports `context`, `fmt`, `log`, `runtime`, and `types`; only the `cleanup` import is new.

- [ ] **Step 4: Register the command and flags in `init()`**

Add to `internal/cli/root.go`'s `init()` function, next to the other `rootCmd.AddCommand` calls:

```go
	rootCmd.AddCommand(cleanupCmd)
	addCleanupFlags(cleanupCmd)
```

- [ ] **Step 5: Run the CLI tests to verify they pass**

Run: `go test ./internal/cli/... -v -count=1`
Expected: PASS for the cleanup command/flag/options tests (plus all pre-existing CLI tests, including `TestMockRTForDeployImplementsManager` — the `runtime.Manager` interface was not changed, so no mock edits are needed).

- [ ] **Step 6: Update README.md — Features list**

Add this bullet to the Features list in `README.md` (after the "Health check configuration" bullet, line 21):

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, images, volumes, networks, and build cache with label-based protection (running containers, current app containers, and the newest images are never removed).
```

- [ ] **Step 7: Update README.md — CLI Reference section**

Insert this section after the `### tengiz rm <app>` section (after line 229) and before `### tengiz rollback <app>`:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space. Tengiz-managed containers and images are protected by labels: running containers, each app's current container (even when stopped for scale-to-zero), and the newest images are always kept.

```
tengiz cleanup                    # clean all categories
tengiz cleanup --dry-run          # show what would be removed without removing
tengiz cleanup --images --volumes # only images and volumes
tengiz cleanup --cache            # only build cache
```

| Flag | Description |
|------|-------------|
| `--all` | Clean all categories (default) |
| `--containers` | Remove stopped/orphaned containers (label-protected) |
| `--images` | Remove dangling and old app images (keeps newest 5 per app) |
| `--volumes` | Remove unused volumes |
| `--networks` | Remove unused networks |
| `--cache` | Prune Docker build cache |
| `--dry-run` | Show what would be removed without removing anything |
| `--keep-images N` | Number of images to keep per app (default 5) |
| `--keep-deployments N` | Number of versioned deployment containers to keep per app (default 1) |
```

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go README.md
git commit -m "feat(cleanup): add tengiz cleanup CLI command and documentation"
```

---

### Task 5: Documentation of implementation status + final verification

**Files:**
- Modify: `docs/FUTURES_FEATURES.md`
- Test: full suite + vet

**Interfaces:**
- Consumes: no new code; confirms the completed command

- [ ] **Step 1: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table row for **Docker Housekeeping** (line 19), change the `⬜` to `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 2: Add a Status line to the feature's detailed section**

In the `## Docker Housekeeping (Otomatik Temizlik)` section (after line 381), add:

```markdown
- **Status:** ✅ Implemented (2026-08-05)
```

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -v -count=1`
Expected: PASS for all packages (`builder`, `cli`, `config`, `encrypt`, `git`, `gitdeploy`, `health`, `idle`, `notify`, `preview`, `proxy`, `runtime`, `secrets`, `types`, `webhook`, `cleanup`)

- [ ] **Step 4: Run static analysis**

Run: `go vet ./...`
Expected: no warnings

- [ ] **Step 5: Commit**

```bash
git add docs/FUTURES_FEATURES.md
git commit -m "docs: mark docker housekeeping as implemented"
```

---

## Self-Review

**1. Spec coverage** (from `docs/FUTURES_FEATURES.md`, feature #6 "Docker Housekeeping"):
- "Disk space is the #1 production issue... Label-based docker system prune" — Task 2 `SelectContainers`/`SelectImages` implement label-based protection; Task 1 lists labeled containers/images; Task 3 removes them. ✓
- "`tengiz cleanup`" command — Task 4. ✓
- "kullanılmayan volume, network, container ve image'leri temizleme" — Task 1/3 cover volumes (`ListDanglingVolumes`/`RemoveVolume`), networks (`ListNetworks`/`SelectNetworks`/`RemoveNetwork`), containers, images. ✓
- "CleanupHelperContainersJob ile yardımcı container'ları temizler" — `SelectContainers` removes stopped containers with no `tengiz-app` label (helper/stale containers). ✓
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" — running + current containers protected. ✓

**2. Placeholder scan:** No TBD/TODO/"implement later" / vague "add validation" steps. Every code step contains complete compilable source. ✓

**3. Type consistency:**
- `SelectContainers([]types.ContainerEntry, int) []string` defined in Task 2, called with `(entries, opts.KeepDeployments)` in Task 3. ✓
- `SelectImages([]types.ImageEntry, int) []string` defined in Task 2, called with `(entries, opts.KeepImages)` in Task 3. ✓
- `SelectNetworks([]string) []string` defined in Task 2, called with `(all)` in Task 3. ✓
- `runtime.CleanupManager` methods match both the `*dockerRuntime` implementations (Task 1) and the `fakeManager` in `runner_test.go` (Task 3). ✓
- `NewCleanup() (CleanupManager, error)` (Task 1) → `cleanup.NewRunner(rt)` expecting `runtime.CleanupManager` (Task 3) → CLI passes `rt` from `runtime.NewCleanup()` (Task 4). ✓
- `types.CleanupOptions` and `types.CleanupReport` field names consistent between `types.go` (Task 1), `runner.go` (Task 3), and `root.go` (Task 4). ✓
- `addCleanupFlags` defined in `root.go` (Task 4), used both in `init()` and in CLI tests. ✓
- `deploymentLabelKey` constant defined in `docker_cleanup.go` (Task 1), used only there. ✓

**Manual CLI verification (requires a live Docker daemon):**

```bash
go build -o tengiz .
./tengiz cleanup --dry-run          # lists targets, removes nothing
./tengiz cleanup --images --cache   # only old/dangling images + build cache
./tengiz cleanup                    # all categories, prints docker system df
```

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-05-docker-housekeeping.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**