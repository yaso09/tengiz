# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes stopped/unused non-Tengiz containers, dangling images, unused anonymous volumes and unused networks through label-protected `docker` prune commands, and reports the reclaimed disk space.

**Architecture:** Native `docker` CLI prune primitives live in `internal/runtime` (a new `prune.go` file plus new methods on the `dockerRuntime` and `Manager` interface). A new `internal/cleanup` package orchestrates them: it applies per-app image retention via the existing `KeepLastNImages`, supports per-category selection and `--dry-run`, and estimates reclaimed space by comparing `docker system df` total-reclaim before/after. The CLI command `tengiz cleanup` in `internal/cli` wires it together with a testable `formatCleanupResult`/`humanBytes` output layer.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` interface, `config.Store`, Docker CLI (`docker container prune`, `docker image prune`, `docker volume prune`, `docker network prune`, `docker system df`). No new external dependencies.

## Global Constraints

- Tengiz-managed containers carry the label `tengiz-app=<name>` — they must NEVER be deleted by cleanup.
- `docker container prune` must use the negated label filter `--filter label!=tengiz-app` so stopped (idle) Tengiz containers are preserved.
- Image cleanup is conservative: only dangling images plus per-app retention via the existing `KeepLastNImages` (image rollback safety). Never run `docker image prune -a` or `docker system prune -a`.
- Tengiz volumes are host bind mounts (not named volumes), so `docker volume prune` cannot affect app data.
- `--dry-run` must never mutate Docker state (no prune, no `KeepLastNImages`).
- `tengiz cleanup` with no category flag runs all four categories.
- Default image retention is `5` (matches deploy-time `rt.KeepLastNImages(ctx, name, 5)`).
- Individual prune failures are non-fatal: log a `[cleanup] warning:` and continue with the remaining categories.
- `DockerDiskInfo` parsing must tolerate missing/absent fields (older Docker versions); missing `TotalReclaim` parses as `0`.
- Extending `runtime.Manager` requires updating every type that implements it (see Task 2 file list).
- Scheduled runs are intentionally out of scope: `tengiz cleanup` is designed to be invoked from the user's cron/systemd timer.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` (create) | Pure helpers (`parseHumanSize`, `parseSystemDF`, `parsePruneCount`, `pruneArgs`, `systemDFArgs`, `DockerDiskInfo` type) + dockerRuntime exec methods added in Task 2 |
| `internal/runtime/prune_test.go` (create) | Unit tests for the pure helpers and stub prune methods |
| `internal/runtime/runtime.go` (modify) | Extend `Manager` interface with 5 new methods + add stub implementations to `stubManager` |
| `internal/proxy/proxy_test.go` (modify) | Add 5 new stub methods to `mockRuntime` so it keeps satisfying `runtime.Manager` |
| `internal/idle/idle_test.go` (modify) | Add 5 new stub methods to `mockRuntime` so it keeps satisfying `runtime.Manager` |
| `internal/cli/root_test.go` (modify) | Add 5 new stub methods to `mockRTForDeploy` so it keeps satisfying `runtime.Manager` |
| `internal/cleanup/cleanup.go` (create) | `Cleaner` orchestrator: `Options`, `Result`, retention, category selection, dry-run, reclaim diff |
| `internal/cleanup/cleanup_test.go` (create) | Tests for `Cleaner.Run` using a fake `runtime.Manager` |
| `internal/cli/cleanup.go` (create) | `tengiz cleanup` Cobra command + `formatCleanupResult` + `humanBytes` |
| `internal/cli/cleanup_test.go` (create) | Command registration, flag parsing, formatter and `humanBytes` tests |
| `README.md` (modify) | New `## tengiz cleanup` CLI reference section + Features bullet |
| `AGENTS.md` (modify) | Add `tengiz cleanup` line to the Command list |
| `docs/FUTURES_FEATURES.md` (modify) | Mark priority #6 Docker Housekeeping as implemented (✅) |

---

### Task 1: Pure docker pruning helpers in `internal/runtime`

**Files:**
- Create: `internal/runtime/prune.go` (helpers only — exec methods come in Task 2)
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.DockerDiskInfo` struct with fields `Images int`, `Containers int`, `Volumes int`, `BuildCache int`, `TotalReclaimBytes int64`; package functions `parseHumanSize(s string) int64`, `parseSystemDF(out []byte) (DockerDiskInfo, error)`, `systemDFArgs() []string`, `pruneArgs(resource string) []string`, `parsePruneCount(out []byte) int`. These are consumed by Task 2's exec methods and Task 3.

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import "testing"

func TestParseHumanSize(t *testing.T) {
	tests := []struct {
		in       string
		expected int64
	}{
		{"", 0},
		{"0B", 0},
		{"500B", 500},
		{"1kB", 1000},
		{"28.1kB", 28100},
		{"445.1MB", 445100000},
		{"3GB", 3000000000},
		{"2TB", 2000000000000},
	}
	for _, tc := range tests {
		if got := parseHumanSize(tc.in); got != tc.expected {
			t.Errorf("parseHumanSize(%q) = %d, want %d", tc.in, got, tc.expected)
		}
	}
}

func TestParseSystemDF(t *testing.T) {
	in := []byte(`{"Images":10,"Containers":20,"Volumes":3,"BuildCache":8,"ImagesSize":"4.2GB","TotalReclaim":"3.5GB"}`)
	info, err := parseSystemDF(in)
	if err != nil {
		t.Fatalf("parseSystemDF: %v", err)
	}
	if info.Images != 10 {
		t.Errorf("Images = %d, want 10", info.Images)
	}
	if info.Containers != 20 {
		t.Errorf("Containers = %d, want 20", info.Containers)
	}
	if info.Volumes != 3 {
		t.Errorf("Volumes = %d, want 3", info.Volumes)
	}
	if info.BuildCache != 8 {
		t.Errorf("BuildCache = %d, want 8", info.BuildCache)
	}
	if info.TotalReclaimBytes != 3500000000 {
		t.Errorf("TotalReclaimBytes = %d, want 3500000000", info.TotalReclaimBytes)
	}
}

func TestParseSystemDFMissingReclaim(t *testing.T) {
	in := []byte(`{"Images":1}`)
	info, err := parseSystemDF(in)
	if err != nil {
		t.Fatalf("parseSystemDF: %v", err)
	}
	if info.Images != 1 {
		t.Errorf("Images = %d, want 1", info.Images)
	}
	if info.TotalReclaimBytes != 0 {
		t.Errorf("TotalReclaimBytes = %d, want 0", info.TotalReclaimBytes)
	}
}

func TestParseSystemDFInvalid(t *testing.T) {
	if _, err := parseSystemDF([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestPruneArgs(t *testing.T) {
	got := pruneArgs("container")
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("pruneArgs(container) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pruneArgs(container) = %v, want %v", got, want)
		}
	}

	got = pruneArgs("image")
	want = []string{"image", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("pruneArgs(image) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pruneArgs(image) = %v, want %v", got, want)
		}
	}
}

func TestSystemDFArgs(t *testing.T) {
	args := systemDFArgs()
	want := []string{"system", "df", "--format", "{{json .}}"}
	if len(args) != len(want) {
		t.Fatalf("systemDFArgs() = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("systemDFArgs() = %v, want %v", args, want)
		}
	}
}

func TestParsePruneCountContainers(t *testing.T) {
	out := []byte("Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 0B\n")
	if got := parsePruneCount(out); got != 2 {
		t.Errorf("parsePruneCount(containers) = %d, want 2", got)
	}
}

func TestParsePruneCountImagesIgnoresUntagged(t *testing.T) {
	out := []byte("Deleted Images:\nuntagged: tengiz-apps/app:123\ndeleted: sha256:abc123\ndeleted: sha256:def456\n\nTotal reclaimed space: 12.3MB\n")
	if got := parsePruneCount(out); got != 2 {
		t.Errorf("parsePruneCount(images) = %d, want 2", got)
	}
}

func TestParsePruneCountNothing(t *testing.T) {
	out := []byte("Total reclaimed space: 0B\n")
	if got := parsePruneCount(out); got != 0 {
		t.Errorf("parsePruneCount(empty) = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestParse|TestPruneArgs|TestSystemDFArgs" -v -count=1`

Expected: FAIL to compile with `undefined: parseHumanSize`, `undefined: parseSystemDF`, `undefined: pruneArgs`, `undefined: systemDFArgs`, `undefined: parsePruneCount`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const keepLabel = "tengiz-app"

// DockerDiskInfo is a snapshot of Docker's reclaimable disk usage,
// parsed from `docker system df --format '{{json .}}'`.
type DockerDiskInfo struct {
	Images            int
	Containers        int
	Volumes           int
	BuildCache        int
	TotalReclaimBytes int64
}

type dockerDFJSON struct {
	Images       int    `json:"Images"`
	Containers   int    `json:"Containers"`
	Volumes      int    `json:"Volumes"`
	BuildCache   int    `json:"BuildCache"`
	TotalReclaim string `json:"TotalReclaim"`
}

func parseHumanSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	i := 0
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	var mult float64
	switch strings.ToLower(s[i:]) {
	case "b":
		mult = 1
	case "kb":
		mult = 1e3
	case "mb":
		mult = 1e6
	case "gb":
		mult = 1e9
	case "tb":
		mult = 1e12
	default:
		mult = 1
	}
	return int64(num * mult)
}

func parseSystemDF(out []byte) (DockerDiskInfo, error) {
	var raw dockerDFJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return DockerDiskInfo{}, fmt.Errorf("parse docker system df: %w", err)
	}
	return DockerDiskInfo{
		Images:            raw.Images,
		Containers:        raw.Containers,
		Volumes:           raw.Volumes,
		BuildCache:        raw.BuildCache,
		TotalReclaimBytes: parseHumanSize(raw.TotalReclaim),
	}, nil
}

func systemDFArgs() []string {
	return []string{"system", "df", "--format", "{{json .}}"}
}

func pruneArgs(resource string) []string {
	args := []string{resource, "prune", "-f"}
	if resource == "container" {
		args = append(args, "--filter", "label!="+keepLabel)
	}
	return args
}

func parsePruneCount(out []byte) int {
	count := 0
	inside := false
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Deleted ") {
			inside = true
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed") {
			break
		}
		if inside && !strings.HasPrefix(line, "untagged:") {
			count++
		}
	}
	return count
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestParse|TestPruneArgs|TestSystemDFArgs" -v -count=1`

Expected: PASS (all 7 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: add docker prune helper functions for housekeeping"
```

---

### Task 2: Docker prune exec methods + extend `runtime.Manager` interface

**Files:**
- Modify: `internal/runtime/prune.go` — add exec methods + imports `context`, `os/exec`
- Modify: `internal/runtime/runtime.go:31-49` — extend `Manager` interface
- Modify: `internal/runtime/runtime.go` — add 5 stub methods to `stubManager`
- Modify: `internal/proxy/proxy_test.go` — add 5 stub methods to `mockRuntime`
- Modify: `internal/idle/idle_test.go` — add 5 stub methods to `mockRuntime`
- Modify: `internal/cli/root_test.go` — add 5 stub methods to `mockRTForDeploy`
- Test: `internal/runtime/prune_test.go` — add `TestStubPruneMethods`

**Interfaces:**
- Consumes: `parsePruneCount`, `pruneArgs`, `systemDFArgs`, `parseSystemDF`, `DockerDiskInfo` from Task 1
- Produces: `runtime.Manager.PruneContainers(ctx context.Context) (int, error)`, `runtime.Manager.PruneImages(ctx context.Context) (int, error)`, `runtime.Manager.PruneVolumes(ctx context.Context) (int, error)`, `runtime.Manager.PruneNetworks(ctx context.Context) (int, error)`, `runtime.Manager.DockerDiskInfo(ctx context.Context) (DockerDiskInfo, error)`. Task 3's `Cleaner` calls these.

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/prune_test.go`:

```go
func TestStubPruneMethods(t *testing.T) {
	m := NewStub()
	if n, err := m.PruneContainers(context.Background()); err != nil || n != 0 {
		t.Fatalf("PruneContainers() = %d, %v; want 0, nil", n, err)
	}
	if n, err := m.PruneImages(context.Background()); err != nil || n != 0 {
		t.Fatalf("PruneImages() = %d, %v; want 0, nil", n, err)
	}
	if n, err := m.PruneVolumes(context.Background()); err != nil || n != 0 {
		t.Fatalf("PruneVolumes() = %d, %v; want 0, nil", n, err)
	}
	if n, err := m.PruneNetworks(context.Background()); err != nil || n != 0 {
		t.Fatalf("PruneNetworks() = %d, %v; want 0, nil", n, err)
	}
	if info, err := m.DockerDiskInfo(context.Background()); err != nil || info.Images != 0 {
		t.Fatalf("DockerDiskInfo() = %+v, %v; want zero, nil", info, err)
	}
}
```

The test file needs a `context` import. Update the import block to:

```go
import (
	"context"
	"testing"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPruneMethods -v -count=1`

Expected: FAIL with `m.PruneContainers undefined (type runtime.Manager has no field or method PruneContainers)`.

- [ ] **Step 3: Write minimal implementation**

In `internal/runtime/prune.go`, replace the import block with:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)
```

Append the exec methods to `internal/runtime/prune.go`:

```go
func (r *dockerRuntime) PruneContainers(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", pruneArgs("container")...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return parsePruneCount(out), nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", pruneArgs("image")...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return parsePruneCount(out), nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", pruneArgs("volume")...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return parsePruneCount(out), nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", pruneArgs("network")...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return parsePruneCount(out), nil
}

func (r *dockerRuntime) DockerDiskInfo(ctx context.Context) (DockerDiskInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", systemDFArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return DockerDiskInfo{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseSystemDF(out)
}
```

In `internal/runtime/runtime.go`, extend the `Manager` interface (append inside the interface block after the existing `Run` method):

```go
	PruneContainers(ctx context.Context) (int, error)
	PruneImages(ctx context.Context) (int, error)
	PruneVolumes(ctx context.Context) (int, error)
	PruneNetworks(ctx context.Context) (int, error)
	DockerDiskInfo(ctx context.Context) (DockerDiskInfo, error)
```

Add stub methods to `stubManager` in `internal/runtime/runtime.go` (after the existing `Run` stub):

```go
func (m *stubManager) PruneContainers(ctx context.Context) (int, error) { return 0, nil }
func (m *stubManager) PruneImages(ctx context.Context) (int, error)     { return 0, nil }
func (m *stubManager) PruneVolumes(ctx context.Context) (int, error)    { return 0, nil }
func (m *stubManager) PruneNetworks(ctx context.Context) (int, error)   { return 0, nil }
func (m *stubManager) DockerDiskInfo(ctx context.Context) (DockerDiskInfo, error) {
	return DockerDiskInfo{}, nil
}
```

**Update the three package mocks** so they keep satisfying `runtime.Manager`. In `internal/proxy/proxy_test.go`, `internal/idle/idle_test.go`, and `internal/cli/root_test.go`, append these 5 methods after each mock's existing `Run` method (receiver is `*mockRuntime` in the proxy/idle files, `*mockRTForDeploy` in the cli file):

```go
func (m *mockRuntime) PruneContainers(ctx context.Context) (int, error) { return 0, nil }
func (m *mockRuntime) PruneImages(ctx context.Context) (int, error)     { return 0, nil }
func (m *mockRuntime) PruneVolumes(ctx context.Context) (int, error)    { return 0, nil }
func (m *mockRuntime) PruneNetworks(ctx context.Context) (int, error)   { return 0, nil }
func (m *mockRuntime) DockerDiskInfo(ctx context.Context) (runtime.DockerDiskInfo, error) {
	return runtime.DockerDiskInfo{}, nil
}
```

In `internal/cli/root_test.go`, the receiver is `mockRTForDeploy`, so the last method becomes:

```go
func (m *mockRTForDeploy) DockerDiskInfo(ctx context.Context) (runtime.DockerDiskInfo, error) {
	return runtime.DockerDiskInfo{}, nil
}
```

- [ ] **Step 4: Run full build + test to verify it passes**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: PASS — the `TestStubPruneMethods` test passes and all packages (including proxy/idle/cli which use the updated mocks) compile.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/runtime.go internal/runtime/prune_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: add docker prune methods to runtime manager"
```

---

### Task 3: `internal/cleanup` orchestrator package

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager` (from Task 2), `config.Store` (`NewStoreWithEnv` persists apps in `~/.tengiz/apps-{env}.json`)
- Produces: `cleanup.Options` struct (`Containers, Images, Volumes, Networks, DryRun bool; Retention int`), `cleanup.Result` struct (`DryRun bool; ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved int; RetentionApps []string; ReclaimedBytes int64`), `cleanup.New(rt runtime.Manager, store *config.Store) *Cleaner`, `(*Cleaner).Run(ctx context.Context, opts Options) *Result`. Task 4's CLI uses these.

- [ ] **Step 1: Write the failing test**

Create `internal/cleanup/cleanup_test.go`:

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

type fakeManager struct {
	containersCalls int
	imagesCalls     int
	volumesCalls    int
	networksCalls   int
	keepCalls       int
	keepApps        []string
	keepN           []int
	containersRet   int
	imagesRet       int
	volumesRet      int
	networksRet     int
	before          runtime.DockerDiskInfo
	after           runtime.DockerDiskInfo
	diskCalls       int
}

func (m *fakeManager) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *fakeManager) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *fakeManager) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error { return nil }
func (m *fakeManager) Start(ctx context.Context, name string) error { return nil }
func (m *fakeManager) Stop(ctx context.Context, name string) error { return nil }
func (m *fakeManager) Restart(ctx context.Context, name string) error { return nil }
func (m *fakeManager) Remove(ctx context.Context, name string) error { return nil }
func (m *fakeManager) RemoveBySuffix(ctx context.Context, name string, suffix string) error { return nil }
func (m *fakeManager) IsActive(ctx context.Context, name string) (bool, error) { return true, nil }
func (m *fakeManager) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) { return 0, nil }
func (m *fakeManager) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *fakeManager) Logs(ctx context.Context, name string, opts runtime.LogOptions) (io.ReadCloser, error) { return nil, nil }
func (m *fakeManager) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }
func (m *fakeManager) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error { return nil }
func (m *fakeManager) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts runtime.RunOptions) error { return nil }
func (m *fakeManager) RemoveImage(ctx context.Context, imageTag string) error { return nil }
func (m *fakeManager) KeepLastNImages(ctx context.Context, appName string, n int) error {
	m.keepCalls++
	m.keepApps = append(m.keepApps, appName)
	m.keepN = append(m.keepN, n)
	return nil
}
func (m *fakeManager) PruneContainers(ctx context.Context) (int, error) { m.containersCalls++; return m.containersRet, nil }
func (m *fakeManager) PruneImages(ctx context.Context) (int, error)     { m.imagesCalls++; return m.imagesRet, nil }
func (m *fakeManager) PruneVolumes(ctx context.Context) (int, error)    { m.volumesCalls++; return m.volumesRet, nil }
func (m *fakeManager) PruneNetworks(ctx context.Context) (int, error)   { m.networksCalls++; return m.networksRet, nil }
func (m *fakeManager) DockerDiskInfo(ctx context.Context) (runtime.DockerDiskInfo, error) {
	m.diskCalls++
	if m.diskCalls > 1 {
		return m.after, nil
	}
	return m.before, nil
}

func TestRunDefaultsToAllCategories(t *testing.T) {
	fake := &fakeManager{}
	c := New(fake, config.NewStore(t.TempDir()))
	c.Run(context.Background(), Options{})
	if fake.containersCalls != 1 || fake.imagesCalls != 1 || fake.volumesCalls != 1 || fake.networksCalls != 1 {
		t.Errorf("expected all prune categories called once, got containers=%d images=%d volumes=%d networks=%d",
			fake.containersCalls, fake.imagesCalls, fake.volumesCalls, fake.networksCalls)
	}
}

func TestRunSelectiveCategories(t *testing.T) {
	fake := &fakeManager{}
	c := New(fake, config.NewStore(t.TempDir()))
	c.Run(context.Background(), Options{Containers: true})
	if fake.containersCalls != 1 {
		t.Errorf("containersCalls = %d, want 1", fake.containersCalls)
	}
	if fake.imagesCalls != 0 || fake.volumesCalls != 0 || fake.networksCalls != 0 {
		t.Errorf("unexpected prune calls for unselected categories: images=%d volumes=%d networks=%d",
			fake.imagesCalls, fake.volumesCalls, fake.networksCalls)
	}
}

func TestRunAggregatesCounts(t *testing.T) {
	fake := &fakeManager{
		containersRet: 3,
		imagesRet:     4,
		volumesRet:    5,
		networksRet:   6,
	}
	c := New(fake, config.NewStore(t.TempDir()))
	res := c.Run(context.Background(), Options{})
	if res.ContainersRemoved != 3 || res.ImagesRemoved != 4 || res.VolumesRemoved != 5 || res.NetworksRemoved != 6 {
		t.Errorf("aggregated counts wrong: %+v", res)
	}
}

func TestRunAppliesRetentionAcrossApps(t *testing.T) {
	fake := &fakeManager{}
	store := config.NewStore(t.TempDir())
	if err := store.SaveApp(types.AppEntry{Name: "beta"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveApp(types.AppEntry{Name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	c := New(fake, store)
	res := c.Run(context.Background(), Options{Images: true, Retention: 3})
	if fake.keepCalls != 2 {
		t.Fatalf("keepCalls = %d, want 2", fake.keepCalls)
	}
	if len(res.RetentionApps) != 2 || res.RetentionApps[0] != "alpha" || res.RetentionApps[1] != "beta" {
		t.Errorf("RetentionApps = %v, want [alpha beta]", res.RetentionApps)
	}
	for _, n := range fake.keepN {
		if n != 3 {
			t.Errorf("KeepLastNImages retention = %d, want 3", n)
		}
	}
}

func TestRunRetentionDefaultsToFive(t *testing.T) {
	fake := &fakeManager{}
	store := config.NewStore(t.TempDir())
	if err := store.SaveApp(types.AppEntry{Name: "myapp"}); err != nil {
		t.Fatal(err)
	}
	c := New(fake, store)
	c.Run(context.Background(), Options{Containers: true})
	if fake.keepCalls != 1 {
		t.Fatalf("keepCalls = %d, want 1", fake.keepCalls)
	}
	if fake.keepN[0] != 5 {
		t.Errorf("KeepLastNImages n = %d, want default 5", fake.keepN[0])
	}
}

func TestRunDryRunPerformsNoMutations(t *testing.T) {
	fake := &fakeManager{before: runtime.DockerDiskInfo{TotalReclaimBytes: 100}}
	store := config.NewStore(t.TempDir())
	if err := store.SaveApp(types.AppEntry{Name: "myapp"}); err != nil {
		t.Fatal(err)
	}
	c := New(fake, store)
	res := c.Run(context.Background(), Options{DryRun: true})
	if fake.containersCalls != 0 || fake.imagesCalls != 0 || fake.volumesCalls != 0 || fake.networksCalls != 0 {
		t.Errorf("dry run performed prune mutations: containers=%d images=%d volumes=%d networks=%d",
			fake.containersCalls, fake.imagesCalls, fake.volumesCalls, fake.networksCalls)
	}
	if fake.keepCalls != 0 {
		t.Errorf("dry run performed retention mutation, keepCalls = %d", fake.keepCalls)
	}
	if res.DryRun != true {
		t.Error("res.DryRun = false, want true")
	}
	if res.ReclaimedBytes != 100 {
		t.Errorf("ReclaimedBytes = %d, want 100 (reclaimable total)", res.ReclaimedBytes)
	}
}

func TestRunReportsReclaimedDiff(t *testing.T) {
	fake := &fakeManager{
		before: runtime.DockerDiskInfo{TotalReclaimBytes: 1000},
		after:  runtime.DockerDiskInfo{TotalReclaimBytes: 400},
	}
	c := New(fake, config.NewStore(t.TempDir()))
	res := c.Run(context.Background(), Options{})
	if res.ReclaimedBytes != 600 {
		t.Errorf("ReclaimedBytes = %d, want 600", res.ReclaimedBytes)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleanup/ -v -count=1`

Expected: FAIL to compile with `undefined: New`, `undefined: Options`, `undefined: cleanup.Cleaner` or `no required module provides package github.com/yaso09/tengiz/internal/cleanup`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"context"
	"log"
	"sort"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

const defaultRetention = 5

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	DryRun     bool
	Retention  int
}

type Result struct {
	DryRun            bool
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	RetentionApps     []string
	ReclaimedBytes    int64
}

type Cleaner struct {
	rt    runtime.Manager
	store *config.Store
}

func New(rt runtime.Manager, store *config.Store) *Cleaner {
	return &Cleaner{rt: rt, store: store}
}

func (c *Cleaner) Run(ctx context.Context, opts Options) *Result {
	if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
	}
	if opts.Retention <= 0 {
		opts.Retention = defaultRetention
	}

	res := &Result{DryRun: opts.DryRun}

	before, err := c.rt.DockerDiskInfo(ctx)
	if err != nil {
		log.Printf("[cleanup] warning: docker system df: %v", err)
	}

	if opts.DryRun {
		res.ReclaimedBytes = before.TotalReclaimBytes
		return res
	}

	if opts.Retention > 0 {
		apps, listErr := c.store.ListApps()
		if listErr == nil && len(apps) > 0 {
			sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
			for _, app := range apps {
				if keepErr := c.rt.KeepLastNImages(ctx, app.Name, opts.Retention); keepErr != nil {
					log.Printf("[cleanup] warning: retain images for %s: %v", app.Name, keepErr)
					continue
				}
				res.RetentionApps = append(res.RetentionApps, app.Name)
			}
		}
	}

	if opts.Containers {
		if n, opErr := c.rt.PruneContainers(ctx); opErr != nil {
			log.Printf("[cleanup] warning: container prune: %v", opErr)
		} else {
			res.ContainersRemoved = n
		}
	}
	if opts.Images {
		if n, opErr := c.rt.PruneImages(ctx); opErr != nil {
			log.Printf("[cleanup] warning: image prune: %v", opErr)
		} else {
			res.ImagesRemoved = n
		}
	}
	if opts.Volumes {
		if n, opErr := c.rt.PruneVolumes(ctx); opErr != nil {
			log.Printf("[cleanup] warning: volume prune: %v", opErr)
		} else {
			res.VolumesRemoved = n
		}
	}
	if opts.Networks {
		if n, opErr := c.rt.PruneNetworks(ctx); opErr != nil {
			log.Printf("[cleanup] warning: network prune: %v", opErr)
		} else {
			res.NetworksRemoved = n
		}
	}

	after, dfErr := c.rt.DockerDiskInfo(ctx)
	if dfErr == nil && after.TotalReclaimBytes < before.TotalReclaimBytes {
		res.ReclaimedBytes = before.TotalReclaimBytes - after.TotalReclaimBytes
	}
	return res
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleanup/ -v -count=1`

Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup orchestrator with label-protected pruning"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanup.Cleaner`/`cleanup.Options`/`cleanup.Result` (Task 3), `runtime.NewDocker()` (Task 2), `config.NewStoreWithEnv(dataDir, env)`, package var `dataDir` from `internal/cli/root.go`
- Produces: `cleanupCmd *cobra.Command` (registered on `rootCmd`), `formatCleanupResult(r *cleanup.Result) string`, `humanBytes(n int64) string`. The tests in this task cover these.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/cleanup"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlagsRegistered(t *testing.T) {
	for _, name := range []string{"containers", "images", "volumes", "networks", "dry-run", "retain-images"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	called := false
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		called = true
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		retain, _ := cmd.Flags().GetInt("retain-images")
		if containers != true {
			t.Errorf("containers = %v, want true", containers)
		}
		if images != false {
			t.Errorf("images = %v, want false", images)
		}
		if volumes != true {
			t.Errorf("volumes = %v, want true", volumes)
		}
		if networks != false {
			t.Errorf("networks = %v, want false", networks)
		}
		if dryRun != true {
			t.Errorf("dry-run = %v, want true", dryRun)
		}
		if retain != 2 {
			t.Errorf("retain-images = %d, want 2", retain)
		}
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--containers", "--volumes", "--dry-run", "--retain-images", "2"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}

func TestFormatCleanupResult(t *testing.T) {
	r := &cleanup.Result{
		ContainersRemoved: 2,
		ImagesRemoved:     3,
		VolumesRemoved:    1,
		NetworksRemoved:   0,
		RetentionApps:     []string{"alpha", "beta"},
		ReclaimedBytes:    1500,
	}
	out := formatCleanupResult(r)
	for _, want := range []string{
		"cleanup complete",
		"containers removed: 2",
		"images removed: 3",
		"volumes removed: 1",
		"networks removed: 0",
		"image retention applied to: alpha, beta",
		"1.5 KB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestFormatCleanupResultDryRun(t *testing.T) {
	r := &cleanup.Result{DryRun: true, ReclaimedBytes: 5000}
	out := formatCleanupResult(r)
	for _, want := range []string{"dry run", "would reclaim approximately 4.9 KB"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in       int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1500, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tc := range tests {
		if got := humanBytes(tc.in); got != tc.expected {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.expected)
		}
	}
}
```

The test references `*cobra.Command`, so the imports must be:

```go
import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanup|TestFormatCleanupResult|TestHumanBytes" -v -count=1`

Expected: FAIL to compile with `undefined: cleanupCmd`, `undefined: formatCleanupResult`, `undefined: humanBytes`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Reclaim disk space by pruning unused Docker resources",
	Long: `Prunes unused Docker resources to free disk space.

Categories:
  --containers  remove stopped/unused containers that are NOT managed by Tengiz
  --images      remove dangling images; old images beyond --retain-images are removed per app
  --volumes     remove anonymous volumes no longer referenced by any container
  --networks    remove networks no longer referenced by any container

With no category flag, all categories run. Resources managed by Tengiz (containers
labeled tengiz-app=*, images tagged tengiz-apps/*) are always preserved.

Use --dry-run to preview the reclaim without deleting anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		retain, _ := cmd.Flags().GetInt("retain-images")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)
		cl := cleanup.New(rt, store)
		res := cl.Run(cmd.Context(), cleanup.Options{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			DryRun:     dryRun,
			Retention:  retain,
		})
		fmt.Print(formatCleanupResult(res))
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped/unused containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images and images beyond retention")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused anonymous volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("dry-run", false, "preview reclaim without deleting anything")
	cleanupCmd.Flags().Int("retain-images", 5, "keep the last N images per app for rollback")
	rootCmd.AddCommand(cleanupCmd)
}

func formatCleanupResult(r *cleanup.Result) string {
	var b strings.Builder
	if r.DryRun {
		b.WriteString("[tengiz] dry run - no resources deleted\n")
		b.WriteString(fmt.Sprintf("[tengiz] would reclaim approximately %s\n", humanBytes(r.ReclaimedBytes)))
		return b.String()
	}
	b.WriteString("[tengiz] cleanup complete\n")
	b.WriteString(fmt.Sprintf("  containers removed: %d\n", r.ContainersRemoved))
	b.WriteString(fmt.Sprintf("  images removed:     %d\n", r.ImagesRemoved))
	b.WriteString(fmt.Sprintf("  volumes removed:    %d\n", r.VolumesRemoved))
	b.WriteString(fmt.Sprintf("  networks removed:   %d\n", r.NetworksRemoved))
	if len(r.RetentionApps) > 0 {
		b.WriteString(fmt.Sprintf("  image retention applied to: %s\n", strings.Join(r.RetentionApps, ", ")))
	}
	b.WriteString(fmt.Sprintf("  space reclaimed:     %s\n", humanBytes(r.ReclaimedBytes)))
	return b.String()
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run "TestCleanup|TestFormatCleanupResult|TestHumanBytes" -v -count=1`

Expected: PASS.

- [ ] **Step 5: Run the full suite and commit**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: PASS.

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 5: Documentation and roadmap status

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the `tengiz cleanup` command from Task 4
- Produces: nothing code-related

- [ ] **Step 1: Add README CLI reference section**

In `README.md`, insert a new section between `### tengiz rollback <app>` (line ~237) and `### tengiz domain` (line ~238), after the rollback section block:

```markdown
### `tengiz cleanup`

Reclaim disk space by pruning unused Docker resources. Tengiz-managed resources (containers labeled `tengiz-app=*`, images tagged `tengiz-apps/*`) are always preserved.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped/unused containers that are not managed by Tengiz |
| `--images` | Prune dangling images; old images beyond `--retain-images` are removed per app |
| `--volumes` | Prune anonymous volumes no longer referenced by any container |
| `--networks` | Prune networks no longer referenced by any container |
| `--dry-run` | Preview the reclaim without deleting anything |
| `--retain-images N` | Keep the last N images per app for rollback (default 5) |

With no category flag, all categories run:

```
tengiz cleanup
tengiz cleanup --dry-run
tengiz cleanup --containers --volumes
```

Schedulable via cron/systemd for automatic housekeeping.
```

In the `## Features` list (around line 20, after the "Deployment history" bullet), add:

```markdown
- **Docker housekeeping** — Reclaim disk space with label-safe `tengiz cleanup` (containers, images, volumes, networks).
```

- [ ] **Step 2: Update AGENTS.md command list**

In `AGENTS.md`, in the CLI listing, add a line after the `tengiz rollback <app>` entry:

```
tengiz cleanup [--containers] [--images] [--volumes] [--networks] [--dry-run] [--retain-images N] → prune unused Docker resources (label-protected, preserves Tengiz-managed containers/images)
```

- [ ] **Step 3: Update FUTURES_FEATURES.md status**

In `docs/FUTURES_FEATURES.md`, in the P0 table, change the Docker Housekeeping row (#6) marker from ⬜ to ✅:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a matching entry to the "✅ Implemented Features" table at the bottom of the file:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-11) |
```

- [ ] **Step 4: Verify build and tests still pass**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Manual Verification (after all tasks)

With Docker installed and a deployed app:

```bash
tengiz cleanup --dry-run         # preview only, nothing deleted
tengiz cleanup                   # run all categories
tengiz ps                        # confirm app containers still listed (not pruned)
```

Expected behavior: stopped Tengiz containers (labeled `tengiz-app=*`) survive cleanup; dangling images, unused networks, anonymous volumes, and non-Tengiz stopped containers are removed; the summary reports per-category counts and reclaimed space.