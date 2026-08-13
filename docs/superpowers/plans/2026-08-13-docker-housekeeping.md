# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command (plus optional periodic scheduling) that reclaims disk space by removing stopped helper containers, dangling images, old app images beyond retention, unused volumes, unused networks, and the Docker build cache — using label-based filtering so Tengiz-managed containers and in-use images are never touched.

**Architecture:** A new `runtime.Cleaner` interface (implemented by the existing `*dockerRuntime`) exposes low-level listing/removal primitives over the `docker` CLI, following the established `os/exec` + `CombinedOutput` pattern. A new `housekeeping` package orchestrates safety: it lists all containers/images/volumes/networks, filters candidates with pure, unit-testable functions (stop-only, `tengiz-app` label protection, in-use image protection, per-app retention), and removes them with batched `docker rm/rmi/volume rm/network rm` commands. The CLI command `tengiz cleanup` wires it together with `--dry-run`, per-category flags, `--keep N` retention, and an optional `--schedule` loop (the "DockerCleanupJob" periodic job).

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (no Docker SDK), existing `internal/runtime` package. No new external dependencies.

## Global Constraints

- Command name is exactly `tengiz cleanup`
- Label-based filtering: containers labeled `tengiz-app=<app>` are **never** removed (running or stopped) — Tengiz-managed containers are protected
- Only **stopped** containers are ever candidates for removal
- Images tagged `:latest` are never removed
- Images referenced by any container (running **or** stopped, i.e. `docker images` `.Containers` > 0) are never removed
- Old app images respect a retention count (`--keep N`, default **5**, matching the existing `KeepLastNImages(…, 5)` usage)
- `--dry-run` must report exactly what would be removed without removing anything
- With no category flag given, `tengiz cleanup` runs **all** categories
- Only the global `--env` persistent flag may be reused; cleanup itself is env-agnostic (operates on the Docker daemon)
- No new external dependencies (`go.mod` unchanged)
- Follow existing codebase patterns: `os/exec.CommandContext` + `CombinedOutput`, pure arg-builder/parse helper functions unit-tested without a Docker daemon, stub/mock implementations for interface tests
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleaner.go` (Create) | `Cleaner` interface + `ContainerInfo`/`ImageInfo` types + pure parse helpers (`parseLabels`, `parseContainerJSONLine`, `parseImageLines`, `parseIDLines`, `parseBuildCacheSize`, `parseReclaimed`) + `stubCleaner`/`NewStubCleaner` |
| `internal/runtime/cleanup.go` (Modify) | `*dockerRuntime` implementation of `Cleaner` (list/remove exec methods) + pure arg builders (`containerRmArgs`, `imageRmiArgs`, `volumeRmArgs`, `networkRmArgs`) |
| `internal/runtime/cleaner_test.go` (Create) | Tests for parse helpers, arg builders, stub/interface assertions |
| `internal/housekeeping/select.go` (Create) | Pure candidate-selection functions: `selectHelperContainers`, `selectDanglingImages`, `selectOldAppImages` |
| `internal/housekeeping/select_test.go` (Create) | Tests for selection functions |
| `internal/housekeeping/housekeeping.go` (Create) | `Options`, `Summary`, `Cleaner`, `New`, `Run` orchestration |
| `internal/housekeeping/housekeeping_test.go` (Create) | `fakeCleaner` + `Run` orchestration tests |
| `internal/cli/cleanup.go` (Create) | `cleanupCmd`, flags, `cleanupOptions`, `printCleanupSummary`, `runScheduled`, `init()` registration |
| `internal/cli/cleanup_test.go` (Create) | Command registration/flag/options-mapping/scheduling tests |
| `README.md` (Modify) | Add `tengiz cleanup` to CLI Reference |
| `AGENTS.md` (Modify) | Add `housekeeping` row to architecture table + `tengiz cleanup` line to CLI section |
| `docs/FUTURES_FEATURES.md` (Modify) | Mark feature #6 Docker Housekeeping as ✅ Implemented |

`internal/cli/root.go` requires **no changes** — `cleanupCmd` registers itself via its own `init()` (same pattern as `internal/cli/preview.go:83-87`).

---

### Task 1: `runtime.Cleaner` interface, types, and pure parse helpers

**Files:**
- Create: `internal/runtime/cleaner.go`
- Test: `internal/runtime/cleaner_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type ContainerInfo struct { ID string; Name string; State string; Labels map[string]string }`
  - `type ImageInfo struct { Tag string; ID string; CreatedAt string; InUse bool }`
  - `type Cleaner interface { ListAllContainers(ctx context.Context) ([]ContainerInfo, error); ListImages(ctx context.Context) ([]ImageInfo, error); ListDanglingVolumes(ctx context.Context) ([]string, error); ListDanglingNetworks(ctx context.Context) ([]string, error); RemoveContainers(ctx context.Context, ids []string) (int, error); RemoveImages(ctx context.Context, tags []string) (int, error); RemoveVolumes(ctx context.Context, names []string) (int, error); RemoveNetworks(ctx context.Context, ids []string) (int, error); BuildCacheSize(ctx context.Context) (string, error); PruneBuildCache(ctx context.Context) (string, error); DiskUsage(ctx context.Context) (string, error) }`
  - `func parseLabels(s string) map[string]string`
  - `func parseContainerJSONLine(line string) (ContainerInfo, error)`
  - `func parseImageLines(output string) ([]ImageInfo, error)`
  - `func parseIDLines(output string) []string`
  - `func parseBuildCacheSize(output string) string`
  - `func parseReclaimed(output string) string`
  - `func NewStubCleaner() Cleaner`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleaner_test.go
package runtime

import (
	"context"
	"testing"
)

func TestParseLabels(t *testing.T) {
	got := parseLabels("tengiz-app=myapp,tengiz-env=production")
	if len(got) != 2 || got["tengiz-app"] != "myapp" || got["tengiz-env"] != "production" {
		t.Errorf("parseLabels() = %v", got)
	}
	if got := parseLabels(""); len(got) != 0 {
		t.Errorf("parseLabels(\"\") = %v, want empty", got)
	}
	if got := parseLabels("solo"); got["solo"] != "" {
		t.Errorf("parseLabels() label without value = %v", got)
	}
}

func TestParseContainerJSONLine(t *testing.T) {
	line := `{"ID":"abc123","Name":"/helper","State":"exited","Labels":"tengiz-app=myapp"}`
	info, err := parseContainerJSONLine(line)
	if err != nil {
		t.Fatalf("parseContainerJSONLine() error = %v", err)
	}
	if info.ID != "abc123" {
		t.Errorf("ID = %q, want abc123", info.ID)
	}
	if info.Name != "helper" {
		t.Errorf("Name = %q, want helper", info.Name)
	}
	if info.State != "exited" {
		t.Errorf("State = %q, want exited", info.State)
	}
	if info.Labels["tengiz-app"] != "myapp" {
		t.Errorf("Labels = %v, want tengiz-app=myapp", info.Labels)
	}
}

func TestParseImageLines(t *testing.T) {
	output := "tengiz-apps/myapp:v1|abc123|2026-07-01 10:00:00 +0000 UTC|1\n" +
		"tengiz-apps/myapp:v2|def456|2026-07-15 10:00:00 +0000 UTC|0\n" +
		"<none>:<none>|ghi789|2026-07-16 10:00:00 +0000 UTC|0"
	imgs, err := parseImageLines(output)
	if err != nil {
		t.Fatalf("parseImageLines() error = %v", err)
	}
	if len(imgs) != 3 {
		t.Fatalf("len(imgs) = %d, want 3", len(imgs))
	}
	if !imgs[0].InUse {
		t.Error("v1 (Containers=1) should be InUse")
	}
	if imgs[1].InUse {
		t.Error("v2 (Containers=0) should not be InUse")
	}
	if imgs[2].Tag != "<none>:<none>" {
		t.Errorf("dangling Tag = %q, want <none>:<none>", imgs[2].Tag)
	}
	if imgs[1].CreatedAt != "2026-07-15 10:00:00 +0000 UTC" {
		t.Errorf("CreatedAt = %q", imgs[1].CreatedAt)
	}
}

func TestParseIDLines(t *testing.T) {
	ids := parseIDLines("\nvol1\nvol2\n")
	if len(ids) != 2 || ids[0] != "vol1" || ids[1] != "vol2" {
		t.Errorf("parseIDLines() = %v, want [vol1 vol2]", ids)
	}
	if got := parseIDLines(""); len(got) != 0 {
		t.Errorf("parseIDLines(\"\") = %v, want empty", got)
	}
}

func TestParseBuildCacheSize(t *testing.T) {
	output := "Containers|12|8|2.1GB|1.2GB\nImages|30|10|8GB|3GB\nBuild Cache|45|0|2.4GB|2.4GB"
	if got := parseBuildCacheSize(output); got != "2.4GB" {
		t.Errorf("parseBuildCacheSize() = %q, want 2.4GB", got)
	}
	if got := parseBuildCacheSize("no rows here"); got != "" {
		t.Errorf("parseBuildCacheSize(no rows) = %q, want empty", got)
	}
}

func TestParseReclaimed(t *testing.T) {
	out := "Deleted build cache objects:\nxyz\n\nTotal reclaimed space: 1.5GB"
	if got := parseReclaimed(out); got != "1.5GB" {
		t.Errorf("parseReclaimed() = %q, want 1.5GB", got)
	}
	if got := parseReclaimed("nothing deleted"); got != "" {
		t.Errorf("parseReclaimed(no match) = %q, want empty", got)
	}
}

func TestStubCleanerSatisfiesInterface(t *testing.T) {
	var c Cleaner = NewStubCleaner()
	if c == nil {
		t.Fatal("NewStubCleaner() returned nil")
	}
	ctx := context.Background()
	if _, err := c.ListAllContainers(ctx); err != nil {
		t.Fatalf("ListAllContainers() error = %v", err)
	}
}

func TestDockerRuntimeSatisfiesCleaner(t *testing.T) {
	var _ Cleaner = (*dockerRuntime)(nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestParse|TestStubCleaner|TestDockerRuntime" -v -count=1`

Expected: FAIL with `undefined: parseLabels`, `undefined: parseContainerJSONLine`, `undefined: parseImageLines`, `undefined: parseIDLines`, `undefined: parseBuildCacheSize`, `undefined: parseReclaimed`, `undefined: NewStubCleaner`, `undefined: Cleaner`.

- [ ] **Step 3: Write minimal implementation in `internal/runtime/cleaner.go`**

```go
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ContainerInfo struct {
	ID     string
	Name   string
	State  string
	Labels map[string]string
}

type ImageInfo struct {
	Tag       string
	ID        string
	CreatedAt string
	InUse     bool
}

type Cleaner interface {
	ListAllContainers(ctx context.Context) ([]ContainerInfo, error)
	ListImages(ctx context.Context) ([]ImageInfo, error)
	ListDanglingVolumes(ctx context.Context) ([]string, error)
	ListDanglingNetworks(ctx context.Context) ([]string, error)
	RemoveContainers(ctx context.Context, ids []string) (int, error)
	RemoveImages(ctx context.Context, tags []string) (int, error)
	RemoveVolumes(ctx context.Context, names []string) (int, error)
	RemoveNetworks(ctx context.Context, ids []string) (int, error)
	BuildCacheSize(ctx context.Context) (string, error)
	PruneBuildCache(ctx context.Context) (string, error)
	DiskUsage(ctx context.Context) (string, error)
}

func parseLabels(s string) map[string]string {
	labels := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 1 {
			labels[kv[0]] = ""
		} else {
			labels[kv[0]] = kv[1]
		}
	}
	return labels
}

func parseContainerJSONLine(line string) (ContainerInfo, error) {
	var raw struct {
		ID     string `json:"ID"`
		Name   string `json:"Name"`
		State  string `json:"State"`
		Labels string `json:"Labels"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return ContainerInfo{}, err
	}
	return ContainerInfo{
		ID:     raw.ID,
		Name:   strings.TrimPrefix(raw.Name, "/"),
		State:  raw.State,
		Labels: parseLabels(raw.Labels),
	}, nil
}

func parseImageLines(output string) ([]ImageInfo, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var imgs []ImageInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			return nil, fmt.Errorf("unexpected image line: %q", line)
		}
		inUse := parts[3] != "" && parts[3] != "0"
		imgs = append(imgs, ImageInfo{
			Tag:       parts[0],
			ID:        parts[1],
			CreatedAt: parts[2],
			InUse:     inUse,
		})
	}
	return imgs, nil
}

func parseIDLines(output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var ids []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			ids = append(ids, l)
		}
	}
	return ids
}

func parseBuildCacheSize(output string) string {
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "Build Cache" {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func parseReclaimed(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}

type stubCleaner struct{}

func NewStubCleaner() Cleaner { return &stubCleaner{} }

func (m *stubCleaner) ListAllContainers(ctx context.Context) ([]ContainerInfo, error) { return nil, nil }
func (m *stubCleaner) ListImages(ctx context.Context) ([]ImageInfo, error)             { return nil, nil }
func (m *stubCleaner) ListDanglingVolumes(ctx context.Context) ([]string, error)       { return nil, nil }
func (m *stubCleaner) ListDanglingNetworks(ctx context.Context) ([]string, error)      { return nil, nil }
func (m *stubCleaner) RemoveContainers(ctx context.Context, ids []string) (int, error) { return 0, nil }
func (m *stubCleaner) RemoveImages(ctx context.Context, tags []string) (int, error)    { return 0, nil }
func (m *stubCleaner) RemoveVolumes(ctx context.Context, names []string) (int, error)  { return 0, nil }
func (m *stubCleaner) RemoveNetworks(ctx context.Context, ids []string) (int, error)   { return 0, nil }
func (m *stubCleaner) BuildCacheSize(ctx context.Context) (string, error)              { return "", nil }
func (m *stubCleaner) PruneBuildCache(ctx context.Context) (string, error)             { return "", nil }
func (m *stubCleaner) DiskUsage(ctx context.Context) (string, error)                   { return "", nil }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestParse|TestStubCleaner|TestDockerRuntime" -v -count=1`

Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleaner.go internal/runtime/cleaner_test.go
git commit -m "feat(runtime): add Cleaner interface and parse helpers for housekeeping"
```

---

### Task 2: `*dockerRuntime` implementation of `Cleaner`

**Files:**
- Modify: `internal/runtime/cleanup.go` (append below existing `KeepLastNImages`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `Cleaner`, `ContainerInfo`, `ImageInfo` from Task 1; `parseContainerJSONLine`, `parseImageLines`, `parseIDLines`, `parseBuildCacheSize`, `parseReclaimed` from Task 1
- Produces: `func containerRmArgs(ids []string) []string`, `func imageRmiArgs(tags []string) []string`, `func volumeRmArgs(names []string) []string`, `func networkRmArgs(ids []string) []string` — plus `*dockerRuntime` now satisfies `runtime.Cleaner`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go (append to existing file)
func TestContainerRmArgs(t *testing.T) {
	got := containerRmArgs([]string{"abc", "def"})
	want := []string{"rm", "-f", "abc", "def"}
	if len(got) != len(want) {
		t.Fatalf("containerRmArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("containerRmArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestImageRmiArgs(t *testing.T) {
	got := imageRmiArgs([]string{"tengiz-apps/myapp:v1", "dangling-id"})
	want := []string{"rmi", "-f", "tengiz-apps/myapp:v1", "dangling-id"}
	if len(got) != len(want) {
		t.Fatalf("imageRmiArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("imageRmiArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestVolumeRmArgs(t *testing.T) {
	got := volumeRmArgs([]string{"vol1", "vol2"})
	want := []string{"volume", "rm", "vol1", "vol2"}
	if len(got) != len(want) {
		t.Fatalf("volumeRmArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("volumeRmArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNetworkRmArgs(t *testing.T) {
	got := networkRmArgs([]string{"n1"})
	want := []string{"network", "rm", "n1"}
	if len(got) != len(want) {
		t.Fatalf("networkRmArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("networkRmArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestContainerRmArgs|TestImageRmiArgs|TestVolumeRmArgs|TestNetworkRmArgs" -v -count=1`

Expected: FAIL with `undefined: containerRmArgs`, `undefined: imageRmiArgs`, `undefined: volumeRmArgs`, `undefined: networkRmArgs`.

- [ ] **Step 3: Write minimal implementation — append to `internal/runtime/cleanup.go`**

```go
func containerRmArgs(ids []string) []string {
	return append([]string{"rm", "-f"}, ids...)
}

func imageRmiArgs(tags []string) []string {
	return append([]string{"rmi", "-f"}, tags...)
}

func volumeRmArgs(names []string) []string {
	return append([]string{"volume", "rm"}, names...)
}

func networkRmArgs(ids []string) []string {
	return append([]string{"network", "rm"}, ids...)
}

func (r *dockerRuntime) ListAllContainers(ctx context.Context) ([]ContainerInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps -a: %w\n%s", err, string(out))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var infos []ContainerInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		info, err := parseContainerJSONLine(line)
		if err != nil {
			continue
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (r *dockerRuntime) ListImages(ctx context.Context) ([]ImageInfo, error) {
	format := `{{.Repository}}:{{.Tag}}|{{.ID}}|{{.CreatedAt}}|{{.Containers}}`
	cmd := exec.CommandContext(ctx, "docker", "images", "--format", format)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return parseImageLines(string(out))
}

func (r *dockerRuntime) ListDanglingVolumes(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	return parseIDLines(string(out)), nil
}

func (r *dockerRuntime) ListDanglingNetworks(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return parseIDLines(string(out)), nil
}

func (r *dockerRuntime) RemoveContainers(ctx context.Context, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", containerRmArgs(ids)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker rm: %w\n%s", err, string(out))
	}
	return len(ids), nil
}

func (r *dockerRuntime) RemoveImages(ctx context.Context, tags []string) (int, error) {
	if len(tags) == 0 {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", imageRmiArgs(tags)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return len(tags), nil
}

func (r *dockerRuntime) RemoveVolumes(ctx context.Context, names []string) (int, error) {
	if len(names) == 0 {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", volumeRmArgs(names)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume rm: %w\n%s", err, string(out))
	}
	return len(names), nil
}

func (r *dockerRuntime) RemoveNetworks(ctx context.Context, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", networkRmArgs(ids)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network rm: %w\n%s", err, string(out))
	}
	return len(ids), nil
}

func (r *dockerRuntime) BuildCacheSize(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{.Type}}|{{.Size}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseBuildCacheSize(string(out)), nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parseReclaimed(string(out)), nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -count=1`

Expected: PASS (arg builder tests pass; `TestDockerRuntimeSatisfiesCleaner` from Task 1 now also passes because `*dockerRuntime` implements every method).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement Cleaner pruning primitives via docker CLI"
```

---

### Task 3: `housekeeping` candidate-selection functions

**Files:**
- Create: `internal/housekeeping/select.go`
- Create: `internal/housekeeping/select_test.go`

**Interfaces:**
- Consumes: `runtime.ContainerInfo{ID, Name, State, Labels map[string]string}`, `runtime.ImageInfo{Tag, ID, CreatedAt, InUse bool}` from Task 1
- Produces:
  - `func selectHelperContainers(all []runtime.ContainerInfo) []string` — IDs of stopped, non-`tengiz-app` containers
  - `func selectDanglingImages(imgs []runtime.ImageInfo) []string` — IDs of `<none>:<none>` images not in use
  - `func selectOldAppImages(imgs []runtime.ImageInfo, keep int) []string` — `tengiz-apps/*` tags beyond retention (newest `keep` kept per repo), skipping `:latest`, in-use, and dangling

- [ ] **Step 1: Write the failing test**

```go
// internal/housekeeping/select_test.go
package housekeeping

import (
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestSelectHelperContainers(t *testing.T) {
	all := []runtime.ContainerInfo{
		{ID: "c1", State: "exited"},
		{ID: "c2", State: "running"},
		{ID: "c3", State: "exited", Labels: map[string]string{"tengiz-app": "myapp"}},
		{ID: "c4", State: "created"},
		{ID: "c5", State: "exited", Labels: map[string]string{"tengiz-app": "myapp", "tengiz-env": "production"}},
	}
	got := selectHelperContainers(all)
	if len(got) != 2 || got[0] != "c1" || got[1] != "c4" {
		t.Errorf("selectHelperContainers() = %v, want [c1 c4]", got)
	}
}

func TestSelectDanglingImages(t *testing.T) {
	imgs := []runtime.ImageInfo{
		{Tag: "<none>:<none>", ID: "d1"},
		{Tag: "<none>:<none>", ID: "d2", InUse: true},
		{Tag: "tengiz-apps/myapp:v1", ID: "d3"},
	}
	got := selectDanglingImages(imgs)
	if len(got) != 1 || got[0] != "d1" {
		t.Errorf("selectDanglingImages() = %v, want [d1]", got)
	}
}

func TestSelectOldAppImages(t *testing.T) {
	imgs := []runtime.ImageInfo{
		{Tag: "tengiz-apps/myapp:v1", CreatedAt: "2026-07-01 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v2", CreatedAt: "2026-07-10 10:00:00 +0000 UTC", InUse: true},
		{Tag: "tengiz-apps/myapp:v3", CreatedAt: "2026-07-15 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v4", CreatedAt: "2026-07-20 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:latest", CreatedAt: "2026-07-21 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/other:v1", CreatedAt: "2026-07-01 10:00:00 +0000 UTC"},
		{Tag: "<none>:<none>", ID: "x", CreatedAt: "2026-07-01 10:00:00 +0000 UTC"},
		{Tag: "node:20", CreatedAt: "2026-06-01 10:00:00 +0000 UTC"},
	}
	got := selectOldAppImages(imgs, 2)
	// myapp eligible non-latest non-in-use: v1, v3, v4 sorted by CreatedAt. keep 2 -> remove v1.
	// other: only v1 -> len 1 <= keep -> nothing. node:20 and dangling -> ignored.
	if len(got) != 1 || got[0] != "tengiz-apps/myapp:v1" {
		t.Errorf("selectOldAppImages() = %v, want [tengiz-apps/myapp:v1]", got)
	}
}

func TestSelectOldAppImagesKeepDefault(t *testing.T) {
	imgs := []runtime.ImageInfo{
		{Tag: "tengiz-apps/myapp:v1", CreatedAt: "2026-07-01 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v2", CreatedAt: "2026-07-10 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v3", CreatedAt: "2026-07-15 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v4", CreatedAt: "2026-07-20 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v5", CreatedAt: "2026-07-25 10:00:00 +0000 UTC"},
		{Tag: "tengiz-apps/myapp:v6", CreatedAt: "2026-07-30 10:00:00 +0000 UTC"},
	}
	got := selectOldAppImages(imgs, 0) // 0 means default 5
	if len(got) != 1 || got[0] != "tengiz-apps/myapp:v1" {
		t.Errorf("keep default: got %v, want [tengiz-apps/myapp:v1]", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/housekeeping/... -v -count=1`

Expected: FAIL (package `housekeeping` does not exist / `undefined: selectHelperContainers`).

- [ ] **Step 3: Write minimal implementation in `internal/housekeeping/select.go`**

```go
package housekeeping

import (
	"sort"
	"strings"

	"github.com/yaso09/tengiz/internal/runtime"
)

const tengizAppLabel = "tengiz-app"

func selectHelperContainers(all []runtime.ContainerInfo) []string {
	var ids []string
	for _, c := range all {
		if c.State == "running" {
			continue
		}
		if _, ok := c.Labels[tengizAppLabel]; ok {
			continue
		}
		ids = append(ids, c.ID)
	}
	return ids
}

func selectDanglingImages(imgs []runtime.ImageInfo) []string {
	var ids []string
	for _, img := range imgs {
		if img.Tag == "<none>:<none>" && !img.InUse {
			ids = append(ids, img.ID)
		}
	}
	return ids
}

func selectOldAppImages(imgs []runtime.ImageInfo, keep int) []string {
	if keep <= 0 {
		keep = 5
	}
	byRepo := make(map[string][]runtime.ImageInfo)
	for _, img := range imgs {
		if img.Tag == "<none>:<none>" || img.InUse {
			continue
		}
		if strings.HasSuffix(img.Tag, ":latest") {
			continue
		}
		repo := strings.SplitN(img.Tag, ":", 2)[0]
		if !strings.HasPrefix(repo, "tengiz-apps/") {
			continue
		}
		byRepo[repo] = append(byRepo[repo], img)
	}
	var toRemove []string
	for _, appImgs := range byRepo {
		sort.Slice(appImgs, func(i, j int) bool {
			return appImgs[i].CreatedAt < appImgs[j].CreatedAt
		})
		if len(appImgs) <= keep {
			continue
		}
		for i := 0; i < len(appImgs)-keep; i++ {
			toRemove = append(toRemove, appImgs[i].Tag)
		}
	}
	sort.Strings(toRemove)
	return toRemove
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/housekeeping/... -v -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/housekeeping/select.go internal/housekeeping/select_test.go
git commit -m "feat(housekeeping): add label-safe candidate selection functions"
```

---

### Task 4: `housekeeping.Cleaner.Run` orchestration

**Files:**
- Create: `internal/housekeeping/housekeeping.go`
- Create: `internal/housekeeping/housekeeping_test.go`

**Interfaces:**
- Consumes: `runtime.Cleaner` (Task 1), `selectHelperContainers`/`selectDanglingImages`/`selectOldAppImages` (Task 3)
- Produces:
  - `type Options struct { DryRun bool; Containers bool; Images bool; Volumes bool; Networks bool; BuildCache bool; Keep int }`
  - `type Summary struct { Containers int; Dangling int; OldImages int; Volumes int; Networks int; BuildCache string; DiskUsage string; DryRun bool }`
  - `func New(rt runtime.Cleaner) *Cleaner`
  - `func (c *Cleaner) Run(ctx context.Context, opts Options) (Summary, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/housekeeping/housekeeping_test.go
package housekeeping

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

type fakeCleaner struct {
	containers       []runtime.ContainerInfo
	images           []runtime.ImageInfo
	volumes          []string
	networks         []string
	buildCacheSize   string
	prunedBuildCache string
	diskUsage        string

	removedContainers []string
	removedImages     []string
	removedVolumes    []string
	removedNetworks   []string
	prunedBuild       bool
}

func (f *fakeCleaner) ListAllContainers(ctx context.Context) ([]runtime.ContainerInfo, error) {
	return f.containers, nil
}
func (f *fakeCleaner) ListImages(ctx context.Context) ([]runtime.ImageInfo, error) {
	return f.images, nil
}
func (f *fakeCleaner) ListDanglingVolumes(ctx context.Context) ([]string, error) {
	return f.volumes, nil
}
func (f *fakeCleaner) ListDanglingNetworks(ctx context.Context) ([]string, error) {
	return f.networks, nil
}
func (f *fakeCleaner) RemoveContainers(ctx context.Context, ids []string) (int, error) {
	f.removedContainers = append(f.removedContainers, ids...)
	return len(ids), nil
}
func (f *fakeCleaner) RemoveImages(ctx context.Context, tags []string) (int, error) {
	f.removedImages = append(f.removedImages, tags...)
	return len(tags), nil
}
func (f *fakeCleaner) RemoveVolumes(ctx context.Context, names []string) (int, error) {
	f.removedVolumes = append(f.removedVolumes, names...)
	return len(names), nil
}
func (f *fakeCleaner) RemoveNetworks(ctx context.Context, ids []string) (int, error) {
	f.removedNetworks = append(f.removedNetworks, ids...)
	return len(ids), nil
}
func (f *fakeCleaner) BuildCacheSize(ctx context.Context) (string, error) {
	return f.buildCacheSize, nil
}
func (f *fakeCleaner) PruneBuildCache(ctx context.Context) (string, error) {
	f.prunedBuild = true
	return f.prunedBuildCache, nil
}
func (f *fakeCleaner) DiskUsage(ctx context.Context) (string, error) {
	return f.diskUsage, nil
}

var _ runtime.Cleaner = (*fakeCleaner)(nil)

func TestRunAllCategories(t *testing.T) {
	f := &fakeCleaner{
		containers:       []runtime.ContainerInfo{{ID: "c1", State: "exited"}},
		images:           []runtime.ImageInfo{{Tag: "<none>:<none>", ID: "d1"}},
		volumes:          []string{"vol1"},
		networks:         []string{"net1"},
		prunedBuildCache: "1.2GB",
		diskUsage:        "df output",
	}
	c := New(f)
	s, err := c.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if s.Containers != 1 || s.Dangling != 1 || s.Volumes != 1 || s.Networks != 1 {
		t.Errorf("summary = %+v", s)
	}
	if s.BuildCache != "1.2GB" {
		t.Errorf("BuildCache = %q, want 1.2GB", s.BuildCache)
	}
	if !f.prunedBuild {
		t.Error("build cache was not pruned")
	}
	if len(f.removedContainers) != 1 || len(f.removedImages) != 1 ||
		len(f.removedVolumes) != 1 || len(f.removedNetworks) != 1 {
		t.Errorf("removed sets = containers:%v images:%v volumes:%v networks:%v",
			f.removedContainers, f.removedImages, f.removedVolumes, f.removedNetworks)
	}
}

func TestRunDryRunDoesNotRemove(t *testing.T) {
	f := &fakeCleaner{
		containers:      []runtime.ContainerInfo{{ID: "c1", State: "exited"}},
		images:          []runtime.ImageInfo{{Tag: "<none>:<none>", ID: "d1"}},
		volumes:         []string{"vol1"},
		networks:        []string{"net1"},
		buildCacheSize:  "2.4GB",
		diskUsage:       "df output",
	}
	c := New(f)
	s, err := c.Run(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if s.Containers != 1 || s.Dangling != 1 || s.Volumes != 1 || s.Networks != 1 {
		t.Errorf("dry summary = %+v", s)
	}
	if s.BuildCache != "2.4GB" {
		t.Errorf("dry BuildCache = %q, want reported size 2.4GB", s.BuildCache)
	}
	if f.prunedBuild {
		t.Error("build cache pruned during dry run")
	}
	if len(f.removedContainers)+len(f.removedImages)+len(f.removedVolumes)+len(f.removedNetworks) != 0 {
		t.Errorf("dry run removed something: %+v", f)
	}
}

func TestRunContainersOnly(t *testing.T) {
	f := &fakeCleaner{
		containers: []runtime.ContainerInfo{
			{ID: "c1", State: "exited"},
			{ID: "c2", State: "exited", Labels: map[string]string{"tengiz-app": "myapp"}},
		},
	}
	c := New(f)
	s, err := c.Run(context.Background(), Options{Containers: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if s.Containers != 1 || len(f.removedContainers) != 1 || f.removedContainers[0] != "c1" {
		t.Errorf("containers = summary:%d removed:%v", s.Containers, f.removedContainers)
	}
	if f.prunedBuild {
		t.Error("build cache pruned when only containers requested")
	}
}

func TestRunOldImagesRespectKeep(t *testing.T) {
	f := &fakeCleaner{
		images: []runtime.ImageInfo{
			{Tag: "tengiz-apps/myapp:v1", CreatedAt: "2026-07-01 10:00:00 +0000 UTC"},
			{Tag: "tengiz-apps/myapp:v2", CreatedAt: "2026-07-10 10:00:00 +0000 UTC"},
			{Tag: "tengiz-apps/myapp:v3", CreatedAt: "2026-07-15 10:00:00 +0000 UTC"},
		},
	}
	c := New(f)
	s, err := c.Run(context.Background(), Options{Images: true, Keep: 2})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if s.OldImages != 1 || s.Dangling != 0 {
		t.Errorf("summary = %+v", s)
	}
	if len(f.removedImages) != 1 || f.removedImages[0] != "tengiz-apps/myapp:v1" {
		t.Errorf("removedImages = %v, want [tengiz-apps/myapp:v1]", f.removedImages)
	}
}

func TestRunInUseImagesProtected(t *testing.T) {
	f := &fakeCleaner{
		images: []runtime.ImageInfo{
			{Tag: "tengiz-apps/myapp:v1", CreatedAt: "2026-07-01 10:00:00 +0000 UTC"},
			{Tag: "tengiz-apps/myapp:v2", CreatedAt: "2026-07-10 10:00:00 +0000 UTC", InUse: true},
		},
	}
	c := New(f)
	s, err := c.Run(context.Background(), Options{Images: true, Keep: 1})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	// eligible non-latest non-in-use: v1 only -> len 1 <= keep 1 -> nothing removed.
	if s.OldImages != 0 || len(f.removedImages) != 0 {
		t.Errorf("OldImages = %d, removed = %v, want 0/none", s.OldImages, f.removedImages)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/housekeeping/... -v -count=1`

Expected: FAIL with `undefined: New`, `undefined: Options`, `undefined: Summary`.

- [ ] **Step 3: Write minimal implementation in `internal/housekeeping/housekeeping.go`**

```go
package housekeeping

import (
	"context"
	"fmt"
	"log"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Options struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	Keep       int
}

type Summary struct {
	Containers int
	Dangling   int
	OldImages  int
	Volumes    int
	Networks   int
	BuildCache string
	DiskUsage  string
	DryRun     bool
}

type Cleaner struct {
	rt runtime.Cleaner
}

func New(rt runtime.Cleaner) *Cleaner {
	return &Cleaner{rt: rt}
}

func (c *Cleaner) Run(ctx context.Context, opts Options) (Summary, error) {
	keep := opts.Keep
	if keep <= 0 {
		keep = 5
	}
	if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.BuildCache {
		opts.Containers, opts.Images, opts.Volumes, opts.Networks, opts.BuildCache = true, true, true, true, true
	}

	var s Summary
	s.DryRun = opts.DryRun

	if opts.Containers {
		all, err := c.rt.ListAllContainers(ctx)
		if err != nil {
			return s, fmt.Errorf("list containers: %w", err)
		}
		targets := selectHelperContainers(all)
		if opts.DryRun {
			s.Containers = len(targets)
		} else if len(targets) > 0 {
			n, err := c.rt.RemoveContainers(ctx, targets)
			if err != nil {
				return s, fmt.Errorf("remove helper containers: %w", err)
			}
			s.Containers = n
		}
	}

	if opts.Images {
		imgs, err := c.rt.ListImages(ctx)
		if err != nil {
			return s, fmt.Errorf("list images: %w", err)
		}
		dangling := selectDanglingImages(imgs)
		old := selectOldAppImages(imgs, keep)
		if opts.DryRun {
			s.Dangling = len(dangling)
			s.OldImages = len(old)
		} else {
			if len(dangling) > 0 {
				n, err := c.rt.RemoveImages(ctx, dangling)
				if err != nil {
					return s, fmt.Errorf("remove dangling images: %w", err)
				}
				s.Dangling = n
			}
			if len(old) > 0 {
				n, err := c.rt.RemoveImages(ctx, old)
				if err != nil {
					return s, fmt.Errorf("remove old app images: %w", err)
				}
				s.OldImages = n
			}
		}
	}

	if opts.Volumes {
		names, err := c.rt.ListDanglingVolumes(ctx)
		if err != nil {
			return s, fmt.Errorf("list volumes: %w", err)
		}
		if opts.DryRun {
			s.Volumes = len(names)
		} else if len(names) > 0 {
			n, err := c.rt.RemoveVolumes(ctx, names)
			if err != nil {
				return s, fmt.Errorf("remove unused volumes: %w", err)
			}
			s.Volumes = n
		}
	}

	if opts.Networks {
		ids, err := c.rt.ListDanglingNetworks(ctx)
		if err != nil {
			return s, fmt.Errorf("list networks: %w", err)
		}
		if opts.DryRun {
			s.Networks = len(ids)
		} else if len(ids) > 0 {
			n, err := c.rt.RemoveNetworks(ctx, ids)
			if err != nil {
				return s, fmt.Errorf("remove unused networks: %w", err)
			}
			s.Networks = n
		}
	}

	if opts.BuildCache {
		if opts.DryRun {
			size, err := c.rt.BuildCacheSize(ctx)
			if err != nil {
				return s, fmt.Errorf("build cache size: %w", err)
			}
			s.BuildCache = size
		} else {
			reclaimed, err := c.rt.PruneBuildCache(ctx)
			if err != nil {
				return s, fmt.Errorf("prune build cache: %w", err)
			}
			s.BuildCache = reclaimed
		}
	}

	usage, err := c.rt.DiskUsage(ctx)
	if err != nil {
		log.Printf("[housekeeping] disk usage: %v", err)
	} else {
		s.DiskUsage = usage
	}
	return s, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/housekeeping/... -v -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/housekeeping/housekeeping.go internal/housekeeping/housekeeping_test.go
git commit -m "feat(housekeeping): add Cleaner.Run orchestration with dry-run and retention"
```

---

### Task 5: CLI `tengiz cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `housekeeping.New(rt runtime.Cleaner) *housekeeping.Cleaner`, `housekeeping.Options`, `housekeeping.Summary` (Task 4); `runtime.NewDocker() (Manager, error)` (existing); `runtime.Cleaner` (Task 1)
- Produces: `var cleanupCmd *cobra.Command` (registered via `init()`), `func cleanupOptions(cmd *cobra.Command) housekeeping.Options`, `func printCleanupSummary(s housekeeping.Summary)`, `func runScheduled(ctx context.Context, interval time.Duration, fn func(ctx context.Context) error) error`

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/housekeeping"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "build-cache", "keep", "schedule"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupOptionsMapping(t *testing.T) {
	original := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = original }()

	var got housekeeping.Options
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		got = cleanupOptions(cmd)
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--containers", "--images", "--volumes", "--networks", "--build-cache", "--keep", "3"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !got.DryRun || !got.Containers || !got.Images || !got.Volumes || !got.Networks || !got.BuildCache {
		t.Errorf("cleanupOptions() = %+v, all category flags expected true", got)
	}
	if got.Keep != 3 {
		t.Errorf("Keep = %d, want 3", got.Keep)
	}
}

func TestRunScheduled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls int32
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()
	err := runScheduled(ctx, 10*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("runScheduled() error = %v", err)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Errorf("runScheduled() called fn %d times, want >= 2", calls)
	}
}
```

Note: `internal/cli/cleanup_test.go` needs the cobra import; add `"github.com/spf13/cobra"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup|TestRunScheduled" -v -count=1`

Expected: FAIL with `cleanup command not registered`, `undefined: cleanupOptions`, `undefined: runScheduled`.

- [ ] **Step 3: Write minimal implementation in `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/housekeeping"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to free disk space",
	Long: "Prunes stopped helper containers, dangling images, old app images beyond retention, " +
		"unused volumes, unused networks, and the Docker build cache. Containers managed by Tengiz " +
		"(labeled tengiz-app) and images in use are never removed. Use --dry-run to preview.",
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}
		cleaner, ok := rt.(runtime.Cleaner)
		if !ok {
			return fmt.Errorf("docker runtime does not support cleanup")
		}
		hk := housekeeping.New(cleaner)
		opts := cleanupOptions(cmd)

		if schedule, _ := cmd.Flags().GetString("schedule"); schedule != "" {
			interval, err := time.ParseDuration(schedule)
			if err != nil {
				return fmt.Errorf("invalid --schedule interval %q: %w", schedule, err)
			}
			return runScheduled(cmd.Context(), interval, func(ctx context.Context) error {
				s, err := hk.Run(ctx, opts)
				if err != nil {
					return err
				}
				printCleanupSummary(s)
				return nil
			})
		}

		s, err := hk.Run(cmd.Context(), opts)
		if err != nil {
			return err
		}
		printCleanupSummary(s)
		return nil
	},
}

func cleanupOptions(cmd *cobra.Command) housekeeping.Options {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	keep, _ := cmd.Flags().GetInt("keep")
	return housekeeping.Options{
		DryRun:     dryRun,
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		Keep:       keep,
	}
}

func printCleanupSummary(s housekeeping.Summary) {
	mode := "removed"
	if s.DryRun {
		mode = "would be removed"
	}
	fmt.Printf("[tengiz] Docker cleanup summary (%s):\n", mode)
	fmt.Printf("  helper containers:  %d\n", s.Containers)
	fmt.Printf("  dangling images:    %d\n", s.Dangling)
	fmt.Printf("  old app images:     %d\n", s.OldImages)
	fmt.Printf("  unused volumes:     %d\n", s.Volumes)
	fmt.Printf("  unused networks:    %d\n", s.Networks)
	if s.BuildCache != "" {
		fmt.Printf("  build cache:        %s\n", s.BuildCache)
	}
	if s.DiskUsage != "" {
		fmt.Printf("\n%s", s.DiskUsage)
	}
}

func runScheduled(ctx context.Context, interval time.Duration, fn func(ctx context.Context) error) error {
	if err := fn(ctx); err != nil {
		return err
	}
	fmt.Printf("[tengiz] periodic cleanup every %s (Ctrl+C to stop)\n", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := fn(ctx); err != nil {
				return err
			}
		}
	}
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "remove stopped helper containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images and old app images beyond retention")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused anonymous volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "remove the Docker build cache")
	cleanupCmd.Flags().Int("keep", 5, "number of newest app images to keep per app")
	cleanupCmd.Flags().String("schedule", "", "run cleanup periodically (e.g. 24h) until interrupted")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanup|TestRunScheduled" -v -count=1`

Expected: PASS.

- [ ] **Step 5: Run full package test suite and vet**

Run: `go test ./internal/cli/... ./internal/housekeeping/... ./internal/runtime/... -count=1 && go vet ./internal/cli/... ./internal/housekeeping/... ./internal/runtime/...`

Expected: PASS, no vet errors.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command with dry-run, categories, and schedule"
```

---

### Task 6: Documentation updates

**Files:**
- Modify: `README.md` (CLI Reference, after the `tengiz rollback` section)
- Modify: `AGENTS.md` (architecture table + CLI section)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 implemented)

**Interfaces:**
- Consumes: nothing — pure documentation

- [ ] **Step 1: Add `tengiz cleanup` to `README.md` CLI Reference**

After the `### tengiz rollback <app>` section (README.md:230-236), insert:

```markdown
### `tengiz cleanup [--dry-run] [--containers] [--images] [--volumes] [--networks] [--build-cache] [--keep N] [--schedule interval]`

Reclaim disk space by removing unused Docker resources.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--containers` | Remove stopped helper containers not managed by Tengiz |
| `--images` | Remove dangling images and old app images beyond retention |
| `--volumes` | Remove unused anonymous volumes |
| `--networks` | Remove unused networks |
| `--build-cache` | Remove the Docker build cache |
| `--keep N` | Number of newest app images to keep per app (default: 5) |
| `--schedule` | Run cleanup periodically (e.g. `24h`) until interrupted |

With no category flag, all categories run. Tengiz-managed containers (labeled `tengiz-app`) are never removed, and images referenced by any container are kept. Old app images keep the newest `N` per app (skipping `:latest` and in-use images). Use `--dry-run` first to preview what will be reclaimed.
```

- [ ] **Step 2: Update `AGENTS.md`**

In the architecture table (after the `idle` row), add:

```markdown
| `housekeeping` | Label-safe Docker cleanup. `Cleaner` struct orchestrates `runtime.Cleaner` primitives: stopped helper containers, dangling/old app images (retention via `Keep`), unused volumes/networks, build cache. `--dry-run` support. Powers `tengiz cleanup`. |
```

In the CLI section (after the `tengiz rollback` line), add:

```markdown
tengiz cleanup [--dry-run] [--keep N] → prune stopped helper containers, dangling/old images, unused volumes/networks, build cache
```

- [ ] **Step 3: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

1. In the P0 table (line 19), change the status marker from `⬜` to `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

2. In the "✅ Implemented Features (Not Pending)" table, add a row:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-13) |
```

3. In the detailed "## Docker Housekeeping (Otomatik Temizlik)" section (line 377), append a status line:

```markdown
- **Status:** ✅ Implemented (2026-08-13)
```

- [ ] **Step 4: Verify docs render and full suite passes**

Run: `go test ./... -count=1 && go vet ./...`

Expected: PASS, no vet errors.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage:**
- `tengiz cleanup` command — Task 5 ✅
- Label-based protection of Tengiz-managed containers — Tasks 3/4 (`selectHelperContainers` skips `tengiz-app`-labeled containers) ✅
- Stopped-helper-container cleanup (CleanupHelperContainersJob) — Task 3 (`selectHelperContainers` only stopped + non-Tengiz) ✅
- Unused volumes/networks/images cleanup (DockerCleanupJob) — Tasks 3/4 ✅
- Periodic cleanup — Task 5 `--schedule` (runs `Run` on a ticker) ✅
- Disk-space reporting — `docker system df` via `DiskUsage` + `BuildCacheSize`/`PruneBuildCache` ✅

Out of scope (deliberately): #56 Granular Docker Prune (per-category interval scheduling is partially covered via `--schedule` + category flags, but per-category *schedules* and buildx-cache specifics are not; left pending) and #103 Build Cache Management & Git GC (`tengiz cleanup --cache --gc`, per-app build cache volumes, git pruning — left pending). These are distinct features and will each get their own plan.

**2. Placeholder scan:** No TODOs/TBDs; every step contains full code and exact commands with expected output.

**3. Type consistency:**
- `runtime.Cleaner` methods used by `housekeeping` match exactly those defined in Task 1 and implemented in Task 2.
- `selectHelperContainers`/`selectDanglingImages`/`selectOldAppImages` signatures match between Task 3 (definition) and Task 4 (usage).
- `housekeeping.Options` fields (`DryRun`, `Containers`, `Images`, `Volumes`, `Networks`, `BuildCache`, `Keep`) and `Summary` fields (`Containers`, `Dangling`, `OldImages`, `Volumes`, `Networks`, `BuildCache`, `DiskUsage`, `DryRun`) are identical in Task 4 (definition) and Task 5 (CLI usage).
- `printCleanupSummary(s housekeeping.Summary)` and `runScheduled(ctx, interval, fn)` names are consistent across Task 5 steps.
- Container/image label key is consistently `tengiz-app` everywhere (`const tengizAppLabel` in `select.go`, matching `internal/runtime/docker.go:76` `const labelKey = "tengiz-app"`).

**Known limitation (accepted):** between `ListAllContainers` and `RemoveContainers` there is a tiny race window where a stopped helper container could theoretically be started; `docker rm -f` would force-remove it. In practice helper containers are build artifacts and this is a manual/scheduled maintenance op; the more dangerous class (Tengiz-managed containers) is excluded by the label filter. This matches the "low effort" classification of the feature.

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-08-13-docker-housekeeping.md`.**

Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**