# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes stale stopped containers and dangling images (and, with `--all`, unused volumes/networks/all unused images) while always protecting registered Tengiz apps and previews, freeing disk space on single-server deployments.

**Architecture:** A label-aware, store-aware cleanup. The runtime package (`internal/runtime`) gains exec-based housekeeping methods backed by small pure arg-builder functions (the codebase's existing test pattern — see `buildLogArgs`/`buildRunArgs`). The CLI (`internal/cli/cleanup.go`) computes the set of protected container names from the env-scoped `config.Store` (registered apps + their zero-downtime deployment-suffix containers + preview containers), lists stopped Tengiz-labeled containers via the runtime, removes only the unprotected ones, then prunes images (volumes/networks only with `--all`, behind a confirmation prompt unless `--force`). `--dry-run` lists what would be removed without deleting anything.

**Tech Stack:** Go 1.26, `os/exec` (docker CLI), Cobra (CLI), existing `config.Store`, `runtime.Manager` interface, `types.AppEntry`/`types.PreviewEntry`. No new external dependencies.

## Global Constraints

- No new external dependencies — docker is invoked via `os/exec`, matching every existing runtime method
- New `runtime.Manager` methods MUST be added to `dockerRuntime` AND the `stubManager` AND the `mockRTForDeploy` in `internal/cli/root_test.go` or the packages won't compile
- Container name prefixes: `tengiz-<app>` (production), `tengiz-<app>-<env>` (non-production), `tengiz-<app>-<env>-<deploymentID>` (zero-downtime versioned), `tengiz-<app>-pr-<n>` (previews) — see `runtime.ContainerName` and `internal/preview/manager.go:40-42`
- Tengiz containers carry label `tengiz-app=<appname>`; envs add `tengiz-env=<env>`; versioned adds `tengiz-deployment=<suffix>`
- Image tags: `tengiz-apps/<app>:<env>-<deploymentID>` and `tengiz-apps/<app>:pr-<n>-<id>` (see `internal/builder/builder.go:61,84`)
- Store is env-scoped via `config.NewStoreWithEnv(dataDir, env)`; env defaults to `"production"` (files: `apps-production.json`, `previews-production.json`)
- Default `tengiz cleanup` (no flags) NEVER removes volumes, networks, or running containers; it removes only stopped/unused Tengiz-labeled containers that are NOT registered, plus dangling images
- Registered app containers (base name + deployment-suffix variant) and registered preview containers are ALWAYS protected
- `--all` requires interactive confirmation unless `--force`/`-y` is passed; `--dry-run` never prompts and never deletes
- Per-app image retention (`KeepLastNImages`, keep 5) already runs automatically on every deploy — cleanup does not re-implement it
- Test commands: `go test ./... -v -count=1`, `go vet ./...`, `go build -o tengiz .`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/housekeeping.go` | NEW — pure docker-arg builders + `dockerRuntime` exec methods for listing stopped containers and pruning images/volumes/networks |
| `internal/runtime/housekeeping_test.go` | NEW — table-driven tests for arg builders, `nonEmptyLines`, `pruneOutputLines`, stub methods |
| `internal/runtime/runtime.go` | MODIFY — add 5 new methods to `Manager` interface + `stubManager` |
| `internal/cli/cleanup.go` | NEW — `cleanupCmd`, `newDocker` injectable factory, `runCleanup`, `protectedContainerNames`, `cleanupReport`, `printCleanupReport`, `confirm` |
| `internal/cli/cleanup_test.go` | NEW — tests for protected-name computation, `runCleanup`, `confirm`, report printing, command registration/flags, full command runs with injected mock runtime |
| `internal/cli/root.go` | MODIFY — register `cleanupCmd` in `init()` + define its flags |
| `internal/cli/root_test.go` | MODIFY — add the 5 new `Manager` methods to `mockRTForDeploy` (compilation requirement) |
| `README.md` | MODIFY — add `tengiz cleanup` to Features + CLI Reference |
| `docs/FUTURES_FEATURES.md` | MODIFY — mark #6 Docker Housekeeping implemented |
| `AGENTS.md` | MODIFY — add `tengiz cleanup` to the CLI command list + runtime row |

---

### Task 1: Runtime housekeeping arg builders (pure functions)

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `labelKey` const (already defined in `internal/runtime/docker.go:76`)
- Produces: pure functions consumed by Task 2's exec methods:
  - `buildListStoppedContainersArgs() []string`
  - `buildListDanglingImagesArgs(all bool) []string`
  - `buildListDanglingVolumesArgs() []string`
  - `buildListDanglingNetworksArgs() []string`
  - `buildPruneImagesArgs(all bool) []string`
  - `buildPruneVolumesArgs() []string`
  - `buildPruneNetworksArgs() []string`
  - `nonEmptyLines(s string) []string`
  - `pruneOutputLines(out string) []string`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/housekeeping_test.go`:

```go
package runtime

import (
	"reflect"
	"testing"
)

func TestBuildListStoppedContainersArgs(t *testing.T) {
	got := buildListStoppedContainersArgs()
	want := []string{
		"ps", "-a",
		"--filter", "label=tengiz-app",
		"--filter", "status=created",
		"--filter", "status=exited",
		"--filter", "status=dead",
		"--format", "{{.Names}}",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildListStoppedContainersArgs() = %v, want %v", got, want)
	}
}

func TestBuildListDanglingImagesArgs(t *testing.T) {
	tests := []struct {
		all  bool
		want []string
	}{
		{false, []string{"image", "ls", "-q", "--filter", "dangling=true"}},
		{true, []string{"image", "ls", "-q"}},
	}
	for _, tt := range tests {
		if got := buildListDanglingImagesArgs(tt.all); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("buildListDanglingImagesArgs(%v) = %v, want %v", tt.all, got, tt.want)
		}
	}
}

func TestBuildListDanglingVolumesArgs(t *testing.T) {
	got := buildListDanglingVolumesArgs()
	want := []string{"volume", "ls", "-q", "--filter", "dangling=true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildListDanglingVolumesArgs() = %v, want %v", got, want)
	}
}

func TestBuildListDanglingNetworksArgs(t *testing.T) {
	got := buildListDanglingNetworksArgs()
	want := []string{"network", "ls", "-q", "--filter", "dangling=true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildListDanglingNetworksArgs() = %v, want %v", got, want)
	}
}

func TestBuildPruneImagesArgs(t *testing.T) {
	tests := []struct {
		all  bool
		want []string
	}{
		{false, []string{"image", "prune", "-f"}},
		{true, []string{"image", "prune", "-f", "-a"}},
	}
	for _, tt := range tests {
		if got := buildPruneImagesArgs(tt.all); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("buildPruneImagesArgs(%v) = %v, want %v", tt.all, got, tt.want)
		}
	}
}

func TestBuildPruneVolumesArgs(t *testing.T) {
	got := buildPruneVolumesArgs()
	want := []string{"volume", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneVolumesArgs() = %v, want %v", got, want)
	}
}

func TestBuildPruneNetworksArgs(t *testing.T) {
	got := buildPruneNetworksArgs()
	want := []string{"network", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneNetworksArgs() = %v, want %v", got, want)
	}
}

func TestNonEmptyLines(t *testing.T) {
	got := nonEmptyLines("  a  \nb\n\n  c\n")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nonEmptyLines() = %v, want %v", got, want)
	}
}

func TestPruneOutputLines(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want []string
	}{
		{
			name: "images",
			out:  "Deleted Images:\ndeleted: sha256:abc123\ndeleted: sha256:def456\n\nTotal reclaimed space: 12.3MB\n",
			want: []string{"sha256:abc123", "sha256:def456"},
		},
		{
			name: "volumes",
			out:  "Deleted Volumes:\nlocal    myvolume\n\nTotal reclaimed space: 0B\n",
			want: []string{"myvolume"},
		},
		{
			name: "networks",
			out:  "Deleted Networks:\nfoo\nbar\n\nTotal reclaimed space: 0B\n",
			want: []string{"foo", "bar"},
		},
		{
			name: "empty",
			out:  "",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pruneOutputLines(tt.out); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pruneOutputLines() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestBuild|TestNonEmptyLines|TestPruneOutputLines' -v -count=1`
Expected: FAIL with `undefined: buildListStoppedContainersArgs` (and the other build functions)

- [ ] **Step 3: Write minimal implementation**

Create `internal/runtime/housekeeping.go`:

```go
package runtime

import "strings"

// buildListStoppedContainersArgs returns the docker args that list the names of
// stopped (created/exited/dead) Tengiz-managed containers.
func buildListStoppedContainersArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "label=" + labelKey,
		"--filter", "status=created",
		"--filter", "status=exited",
		"--filter", "status=dead",
		"--format", "{{.Names}}",
	}
}

// buildListDanglingImagesArgs returns the docker args that list image IDs that
// cleanup would remove in dry-run mode (dangling by default, all unused with all).
func buildListDanglingImagesArgs(all bool) []string {
	args := []string{"image", "ls", "-q"}
	if !all {
		args = append(args, "--filter", "dangling=true")
	}
	return args
}

// buildListDanglingVolumesArgs returns the docker args that list unused volumes.
func buildListDanglingVolumesArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
}

// buildListDanglingNetworksArgs returns the docker args that list unused networks.
func buildListDanglingNetworksArgs() []string {
	return []string{"network", "ls", "-q", "--filter", "dangling=true"}
}

// buildPruneImagesArgs returns the docker args that remove dangling (or all unused) images.
func buildPruneImagesArgs(all bool) []string {
	args := []string{"image", "prune", "-f"}
	if all {
		args = append(args, "-a")
	}
	return args
}

// buildPruneVolumesArgs returns the docker args that remove unused volumes.
func buildPruneVolumesArgs() []string {
	return []string{"volume", "prune", "-f"}
}

// buildPruneNetworksArgs returns the docker args that remove unused networks.
func buildPruneNetworksArgs() []string {
	return []string{"network", "prune", "-f"}
}

// nonEmptyLines splits s into trimmed, non-empty lines.
func nonEmptyLines(s string) []string {
	raw := strings.Split(strings.TrimSpace(s), "\n")
	lines := make([]string, 0, len(raw))
	for _, l := range raw {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// pruneOutputLines extracts deleted items from a `docker <resource> prune` output.
// Handles the headers/footers docker emits ("Deleted Images:", "Total reclaimed space: ...").
func pruneOutputLines(out string) []string {
	lines := nonEmptyLines(out)
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "Total ") ||
			strings.HasPrefix(trimmed, "Deleted ") ||
			strings.HasPrefix(trimmed, "WARNING") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "deleted:" || fields[0] == "Deleted:" {
			if len(fields) > 1 {
				result = append(result, fields[1])
			}
			continue
		}
		result = append(result, fields[len(fields)-1])
	}
	return result
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestBuild|TestNonEmptyLines|TestPruneOutputLines' -v -count=1`
Expected: PASS (9 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat(runtime): add docker housekeeping command builders"
```

---

### Task 2: Runtime housekeeping exec methods + Manager interface + stubs

**Files:**
- Modify: `internal/runtime/housekeeping.go` (append exec methods, expand imports)
- Modify: `internal/runtime/runtime.go` (add 5 methods to `Manager` interface + `stubManager`)
- Modify: `internal/cli/root_test.go` (add 5 methods to `mockRTForDeploy`)
- Test: `internal/runtime/housekeeping_test.go` (append stub tests)

**Interfaces:**
- Consumes: Task 1's builders + existing `dockerRuntime.Remove` (`internal/runtime/docker.go:364`)
- Produces: new `runtime.Manager` methods used by Task 3:
  - `ListStoppedContainers(ctx context.Context) ([]string, error)`
  - `PruneImages(ctx context.Context, all, dryRun bool) ([]string, error)`
  - `PruneContainers(ctx context.Context, names []string, dryRun bool) ([]string, error)`
  - `PruneVolumes(ctx context.Context, dryRun bool) ([]string, error)`
  - `PruneNetworks(ctx context.Context, dryRun bool) ([]string, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/housekeeping_test.go`:

```go
func TestStubListStoppedContainers(t *testing.T) {
	m := NewStub()
	got, err := m.ListStoppedContainers(context.Background())
	if err != nil {
		t.Fatalf("ListStoppedContainers() error = %v", err)
	}
	if got != nil {
		t.Errorf("ListStoppedContainers() = %v, want nil", got)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	if _, err := m.PruneImages(context.Background(), true, false); err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
}

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	if _, err := m.PruneContainers(context.Background(), []string{"a"}, false); err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
}

func TestStubPruneVolumes(t *testing.T) {
	m := NewStub()
	if _, err := m.PruneVolumes(context.Background(), false); err != nil {
		t.Fatalf("PruneVolumes() error = %v", err)
	}
}

func TestStubPruneNetworks(t *testing.T) {
	m := NewStub()
	if _, err := m.PruneNetworks(context.Background(), false); err != nil {
		t.Fatalf("PruneNetworks() error = %v", err)
	}
}
```

Add `"context"` to the existing imports in `internal/runtime/housekeeping_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`
Expected: FAIL with `m.ListStoppedContainers undefined (type Manager has no field or method ListStoppedContainers)` (interface has no such method yet)

- [ ] **Step 3: Implement the interface + stub + exec methods + mock update**

3a. Append these methods to `internal/runtime/housekeeping.go`:

```go
func (r *dockerRuntime) ListStoppedContainers(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildListStoppedContainersArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	return nonEmptyLines(string(out)), nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, all, dryRun bool) ([]string, error) {
	var args []string
	if dryRun {
		args = buildListDanglingImagesArgs(all)
	} else {
		args = buildPruneImagesArgs(all)
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker image prune: %w", err)
	}
	if dryRun {
		return nonEmptyLines(string(out)), nil
	}
	return pruneOutputLines(string(out)), nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context, dryRun bool) ([]string, error) {
	var args []string
	if dryRun {
		args = buildListDanglingVolumesArgs()
	} else {
		args = buildPruneVolumesArgs()
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume prune: %w", err)
	}
	if dryRun {
		return nonEmptyLines(string(out)), nil
	}
	return pruneOutputLines(string(out)), nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context, dryRun bool) ([]string, error) {
	var args []string
	if dryRun {
		args = buildListDanglingNetworksArgs()
	} else {
		args = buildPruneNetworksArgs()
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network prune: %w", err)
	}
	if dryRun {
		return nonEmptyLines(string(out)), nil
	}
	return pruneOutputLines(string(out)), nil
}

func (r *dockerRuntime) PruneContainers(ctx context.Context, names []string, dryRun bool) ([]string, error) {
	removed := make([]string, 0, len(names))
	for _, name := range names {
		if dryRun {
			removed = append(removed, name)
			continue
		}
		if err := r.Remove(ctx, name); err != nil {
			return removed, err
		}
		removed = append(removed, name)
	}
	return removed, nil
}
```

Update the imports at the top of `internal/runtime/housekeeping.go` to:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)
```

3b. In `internal/runtime/runtime.go`, add these 5 lines to the `Manager` interface (after `KeepLastNImages`, around line 36):

```go
	ListStoppedContainers(ctx context.Context) ([]string, error)
	PruneImages(ctx context.Context, all, dryRun bool) ([]string, error)
	PruneContainers(ctx context.Context, names []string, dryRun bool) ([]string, error)
	PruneVolumes(ctx context.Context, dryRun bool) ([]string, error)
	PruneNetworks(ctx context.Context, dryRun bool) ([]string, error)
```

3c. In `internal/runtime/runtime.go`, add these methods to `stubManager` (after `KeepLastNImages`, around line 118):

```go
func (m *stubManager) ListStoppedContainers(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *stubManager) PruneImages(ctx context.Context, all, dryRun bool) ([]string, error) {
	return nil, nil
}

func (m *stubManager) PruneContainers(ctx context.Context, names []string, dryRun bool) ([]string, error) {
	return nil, nil
}

func (m *stubManager) PruneVolumes(ctx context.Context, dryRun bool) ([]string, error) {
	return nil, nil
}

func (m *stubManager) PruneNetworks(ctx context.Context, dryRun bool) ([]string, error) {
	return nil, nil
}
```

3d. In `internal/cli/root_test.go`, add these methods to `mockRTForDeploy` (after its `KeepLastNImages` method, around line 99):

```go
func (m *mockRTForDeploy) ListStoppedContainers(ctx context.Context) ([]string, error) { return nil, nil }
func (m *mockRTForDeploy) PruneImages(ctx context.Context, all, dryRun bool) ([]string, error) { return nil, nil }
func (m *mockRTForDeploy) PruneContainers(ctx context.Context, names []string, dryRun bool) ([]string, error) { return nil, nil }
func (m *mockRTForDeploy) PruneVolumes(ctx context.Context, dryRun bool) ([]string, error) { return nil, nil }
func (m *mockRTForDeploy) PruneNetworks(ctx context.Context, dryRun bool) ([]string, error) { return nil, nil }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1 && go test ./internal/cli/ -count=1`
Expected: PASS for the stub prune tests; PASS for the existing cli tests (mock now satisfies the extended interface)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/runtime.go internal/runtime/housekeeping_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add housekeeping prune methods to runtime manager"
```

---

### Task 3: CLI cleanup logic (`runCleanup`, `protectedContainerNames`, report, confirm)

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: Task 2's `runtime.Manager` methods; `config.NewStoreWithEnv`; `runtime.ContainerName`; existing `mockRTForDeploy` (embed); existing `captureOutput` helper in `internal/cli/root_test.go:57`
- Produces: consumed by Task 4:
  - `type cleanupOptions struct { DryRun bool; All bool }`
  - `type cleanupReport struct { Containers, Images, Volumes, Networks []string; DryRun bool }`
  - `protectedContainerNames(store *config.Store) (map[string]bool, error)`
  - `runCleanup(ctx context.Context, rt runtime.Manager, store *config.Store, opts cleanupOptions) (*cleanupReport, error)`
  - `printCleanupReport(r *cleanupReport)`
  - `joinOrNone(items []string) string`
  - `confirm(r io.Reader, prompt string) (bool, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type cleanupMockRT struct {
	mockRTForDeploy
	stopped []string
	removed []string
	all     bool
}

func (m *cleanupMockRT) ListStoppedContainers(ctx context.Context) ([]string, error) {
	return m.stopped, nil
}

func (m *cleanupMockRT) PruneContainers(ctx context.Context, names []string, dryRun bool) ([]string, error) {
	if !dryRun {
		m.removed = append(m.removed, names...)
	}
	return names, nil
}

func (m *cleanupMockRT) PruneImages(ctx context.Context, all, dryRun bool) ([]string, error) {
	m.all = all
	return []string{"sha256:abc"}, nil
}

func (m *cleanupMockRT) PruneVolumes(ctx context.Context, dryRun bool) ([]string, error) {
	return []string{"myvolume"}, nil
}

func (m *cleanupMockRT) PruneNetworks(ctx context.Context, dryRun bool) ([]string, error) {
	return []string{"mynetwork"}, nil
}

func newCleanupTestStore(t *testing.T) *config.Store {
	t.Helper()
	store := config.NewStoreWithEnv(t.TempDir(), "production")
	if err := store.SaveApp(types.AppEntry{
		Name:             "myapp",
		DeploymentSuffix: "555",
		Config:           types.AppConfig{Name: "myapp", Environment: "production"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddPreview(types.PreviewEntry{
		AppName:       "other",
		PRNumber:      5,
		ContainerName: "tengiz-other-pr-5",
	}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestProtectedContainerNames(t *testing.T) {
	store := newCleanupTestStore(t)
	protected, err := protectedContainerNames(store)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"tengiz-myapp":      true,
		"tengiz-myapp-555":  true,
		"tengiz-other-pr-5": true,
	}
	if !reflect.DeepEqual(protected, want) {
		t.Errorf("protectedContainerNames() = %v, want %v", protected, want)
	}
}

func TestRunCleanupRemovesOnlyUnprotected(t *testing.T) {
	mock := &cleanupMockRT{stopped: []string{
		"tengiz-myapp", "tengiz-myapp-555", "tengiz-other-pr-5", "tengiz-orphan-123",
	}}
	report, err := runCleanup(context.Background(), mock, newCleanupTestStore(t), cleanupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Containers, []string{"tengiz-orphan-123"}) {
		t.Errorf("Containers = %v, want [tengiz-orphan-123]", report.Containers)
	}
	if !reflect.DeepEqual(mock.removed, []string{"tengiz-orphan-123"}) {
		t.Errorf("removed = %v, want [tengiz-orphan-123]", mock.removed)
	}
	if mock.all {
		t.Error("PruneImages should be called with all=false by default")
	}
	if report.DryRun {
		t.Error("report.DryRun = true, want false")
	}
}

func TestRunCleanupDryRunDoesNotRemove(t *testing.T) {
	mock := &cleanupMockRT{stopped: []string{"tengiz-orphan-123"}}
	report, err := runCleanup(context.Background(), mock, newCleanupTestStore(t), cleanupOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(mock.removed) != 0 {
		t.Errorf("removed = %v, want none in dry-run", mock.removed)
	}
	if !reflect.DeepEqual(report.Containers, []string{"tengiz-orphan-123"}) {
		t.Errorf("Containers = %v, want [tengiz-orphan-123]", report.Containers)
	}
	if !report.DryRun {
		t.Error("report.DryRun = false, want true")
	}
}

func TestRunCleanupAllPrunesVolumesAndNetworks(t *testing.T) {
	mock := &cleanupMockRT{}
	report, err := runCleanup(context.Background(), mock, newCleanupTestStore(t), cleanupOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if !mock.all {
		t.Error("PruneImages should be called with all=true")
	}
	if !reflect.DeepEqual(report.Volumes, []string{"myvolume"}) {
		t.Errorf("Volumes = %v, want [myvolume]", report.Volumes)
	}
	if !reflect.DeepEqual(report.Networks, []string{"mynetwork"}) {
		t.Errorf("Networks = %v, want [mynetwork]", report.Networks)
	}
}

func TestJoinOrNone(t *testing.T) {
	if got := joinOrNone(nil); got != "none" {
		t.Errorf("joinOrNone(nil) = %q, want none", got)
	}
	if got := joinOrNone([]string{"a", "b"}); got != "a, b" {
		t.Errorf("joinOrNone([a b]) = %q, want 'a, b'", got)
	}
}

func TestConfirm(t *testing.T) {
	yes, err := confirm(strings.NewReader("y\n"), "continue? ")
	if err != nil {
		t.Fatal(err)
	}
	if !yes {
		t.Error("confirm(y) = false, want true")
	}
	no, err := confirm(strings.NewReader("n\n"), "continue? ")
	if err != nil {
		t.Fatal(err)
	}
	if no {
		t.Error("confirm(n) = true, want false")
	}
	eof, err := confirm(strings.NewReader(""), "continue? ")
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Error("confirm(<eof>) = true, want false")
	}
}

func TestPrintCleanupReport(t *testing.T) {
	out := captureOutput(func() {
		printCleanupReport(&cleanupReport{Containers: []string{"tengiz-orphan"}, DryRun: true})
	})
	for _, want := range []string{"would remove", "tengiz-orphan", "volumes: none", "networks: none"} {
		if !strings.Contains(out, want) {
			t.Errorf("printCleanupReport() output missing %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestProtected|TestRunCleanup|TestJoinOrNone|TestConfirm|TestPrintCleanupReport' -v -count=1`
Expected: FAIL with `undefined: protectedContainerNames` / `undefined: runCleanup` etc.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

type cleanupOptions struct {
	DryRun bool
	All    bool
}

type cleanupReport struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
	DryRun     bool
}

// protectedContainerNames returns the names of containers that cleanup must
// never remove: every registered app's active container (plus its zero-downtime
// deployment-suffix variant) and every registered preview container.
func protectedContainerNames(store *config.Store) (map[string]bool, error) {
	protected := make(map[string]bool)
	apps, err := store.ListApps()
	if err != nil {
		return nil, err
	}
	for _, app := range apps {
		base := runtime.ContainerName(app.Name, app.Config.Environment)
		protected[base] = true
		if app.DeploymentSuffix != "" {
			protected[base+"-"+app.DeploymentSuffix] = true
		}
	}
	previews, err := store.ListAllPreviews()
	if err != nil {
		return nil, err
	}
	for _, pv := range previews {
		if pv.ContainerName != "" {
			protected[pv.ContainerName] = true
		}
	}
	return protected, nil
}

// runCleanup performs the housekeeping using the given runtime, protecting all
// registered app and preview containers.
func runCleanup(ctx context.Context, rt runtime.Manager, store *config.Store, opts cleanupOptions) (*cleanupReport, error) {
	protected, err := protectedContainerNames(store)
	if err != nil {
		return nil, fmt.Errorf("resolve protected containers: %w", err)
	}

	stopped, err := rt.ListStoppedContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list stopped containers: %w", err)
	}

	var toRemove []string
	for _, name := range stopped {
		if !protected[name] {
			toRemove = append(toRemove, name)
		}
	}

	removed, err := rt.PruneContainers(ctx, toRemove, opts.DryRun)
	if err != nil {
		return nil, fmt.Errorf("remove containers: %w", err)
	}

	images, err := rt.PruneImages(ctx, opts.All, opts.DryRun)
	if err != nil {
		return nil, fmt.Errorf("prune images: %w", err)
	}

	report := &cleanupReport{
		Containers: removed,
		Images:     images,
		DryRun:     opts.DryRun,
	}

	if opts.All {
		volumes, err := rt.PruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return nil, fmt.Errorf("prune volumes: %w", err)
		}
		report.Volumes = volumes

		networks, err := rt.PruneNetworks(ctx, opts.DryRun)
		if err != nil {
			return nil, fmt.Errorf("prune networks: %w", err)
		}
		report.Networks = networks
	}

	return report, nil
}

// printCleanupReport writes a human-readable summary of a cleanup run.
func printCleanupReport(r *cleanupReport) {
	verb := "removed"
	if r.DryRun {
		verb = "would remove"
	}
	fmt.Printf("[tengiz] cleanup (%s):\n", verb)
	fmt.Printf("  containers: %s\n", joinOrNone(r.Containers))
	fmt.Printf("  images: %s\n", joinOrNone(r.Images))
	fmt.Printf("  volumes: %s\n", joinOrNone(r.Volumes))
	fmt.Printf("  networks: %s\n", joinOrNone(r.Networks))
}

func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

// confirm reads a single yes/no answer from r and returns whether it was yes.
func confirm(r io.Reader, prompt string) (bool, error) {
	fmt.Fprint(os.Stdout, prompt)
	reader := bufio.NewReader(r)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestProtected|TestRunCleanup|TestJoinOrNone|TestConfirm|TestPrintCleanupReport' -v -count=1`
Expected: PASS (8 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add cleanup orchestration logic with protected container handling"
```

---

### Task 4: CLI command wiring (`cleanupCmd` + registration + flags)

**Files:**
- Modify: `internal/cli/cleanup.go` (append `newDocker` var + `cleanupCmd`, add `cobra` import)
- Modify: `internal/cli/root.go` (`init()` — register command + flags)
- Test: `internal/cli/cleanup_test.go` (append command-level tests)

**Interfaces:**
- Consumes: Task 3's `runCleanup`/`confirm`/`printCleanupReport`; `getEnv(cmd)` and `dataDir` from `internal/cli/root.go`
- Produces: `tengiz cleanup` command with `--dry-run`, `--all`, `--force`/`-y` flags; injectable `var newDocker = runtime.NewDocker` for tests

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/cleanup_test.go`:

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

func TestCleanupFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "all", "force"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupCmdForceAll(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir
	store := config.NewStoreWithEnv(tmpDir, "production")
	if err := store.SaveApp(types.AppEntry{
		Name:   "myapp",
		Config: types.AppConfig{Name: "myapp", Environment: "production"},
	}); err != nil {
		t.Fatal(err)
	}

	mock := &cleanupMockRT{stopped: []string{"tengiz-orphan-123"}}
	old := newDocker
	newDocker = func() (runtime.Manager, error) { return mock, nil }
	defer func() { newDocker = old }()

	rootCmd.SetArgs([]string{"cleanup", "--all", "--force"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mock.removed, []string{"tengiz-orphan-123"}) {
		t.Errorf("removed = %v, want [tengiz-orphan-123]", mock.removed)
	}
	if !mock.all {
		t.Error("expected PruneImages(all=true)")
	}
}

func TestCleanupCmdCancelledWithoutForce(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir
	mock := &cleanupMockRT{}
	old := newDocker
	newDocker = func() (runtime.Manager, error) { return mock, nil }
	defer func() { newDocker = old }()

	rootCmd.SetIn(strings.NewReader("n\n"))
	rootCmd.SetArgs([]string{"cleanup", "--all"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(mock.removed) != 0 {
		t.Errorf("removed = %v, want none (cancelled)", mock.removed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`
Expected: FAIL with `undefined: cleanupCmd` (command not defined/registered)

- [ ] **Step 3: Write minimal implementation**

3a. Append to `internal/cli/cleanup.go`:

```go
// newDocker is a package-level factory so tests can inject a fake runtime.
var newDocker = runtime.NewDocker

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Removes stale stopped containers and dangling images, freeing disk space.

Tengiz-managed containers that are still registered (active apps, previews) are
always protected and never removed. By default only containers that are no
longer tracked, plus dangling images, are cleaned.

Use --dry-run to preview what would be removed without deleting anything.
Use --all to also prune unused volumes, networks, and all unused images.
Use --force to skip the confirmation prompt shown with --all.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		force, _ := cmd.Flags().GetBool("force")

		rt, err := newDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)

		if all && !dryRun && !force {
			ok, err := confirm(cmd.InOrStdin(), "This will remove unused volumes and networks. Continue? [y/N] ")
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		report, err := runCleanup(cmd.Context(), rt, store, cleanupOptions{DryRun: dryRun, All: all})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		printCleanupReport(report)
		return nil
	},
}
```

Update the imports at the top of `internal/cli/cleanup.go` to add:

```go
	"github.com/spf13/cobra"
```

3b. In `internal/cli/root.go`, inside `init()` (after `rootCmd.AddCommand(runCmd)` at line 67), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without deleting anything")
	cleanupCmd.Flags().Bool("all", false, "also prune unused volumes, networks, and all unused images")
	cleanupCmd.Flags().BoolP("force", "y", false, "skip the confirmation prompt")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`
Expected: PASS (4 tests)

- [ ] **Step 5: Verify full suite and build**

Run: `go test ./... -count=1 && go vet ./... && go build -o /tmp/tengiz-check .`
Expected: PASS for all packages; vet clean; build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Documentation (README, FUTURES_FEATURES, AGENTS.md)

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: the implemented `tengiz cleanup` command behavior

- [ ] **Step 1: Update `README.md` Features list**

Insert this bullet into the Features list (after the `- **Deployment history**` bullet, around line 20):

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes stale stopped containers and dangling images (and optionally unused volumes/networks) while protecting registered apps.
```

- [ ] **Step 2: Add the `tengiz cleanup` section to the CLI Reference**

Insert this section into `README.md` between `### tengiz rm <app>` (ends line 228) and `### tengiz rollback <app>` (starts line 230):

````markdown
### `tengiz cleanup`

Remove stale stopped containers and dangling images to free disk space. Tengiz-managed containers that are still registered (active apps, previews) are always protected and never removed.

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview what would be removed without deleting anything |
| `--all` | Also prune unused volumes, networks, and all unused images (requires confirmation) |
| `--force`, `-y` | Skip the confirmation prompt shown with `--all` |
| `--env <env>` | Environment whose app state should be protected (default `production`) |

Default behavior removes only containers that are no longer registered (e.g. orphaned zero-downtime deployment containers) and dangling images. `--all` additionally prunes unused volumes, networks, and all unused images — you are prompted for confirmation unless `--force` is given.

```bash
tengiz cleanup --dry-run    # preview first
tengiz cleanup              # safe default: containers + dangling images
tengiz cleanup --all --force
```
````

- [ ] **Step 3: Update `docs/FUTURES_FEATURES.md`**

3a. In the P0 table, change row 6 to mark it implemented:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

3b. In the "✅ Implemented Features (Not Pending)" table (near line 253), add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-13) |
```

- [ ] **Step 4: Update `AGENTS.md`**

4a. In the CLI command list, after the `tengiz stop/start/rm  → lifecycle` line, add:

```markdown
tengiz cleanup [--dry-run] [--all] [--force] → prune stale containers, dangling images, and (with --all) unused volumes/networks
```

4b. In the `runtime.Manager` row of the architecture table, extend the description:

```markdown
| `runtime.Manager` | Interface for container lifecycle. `NewDocker()` = exec-based impl, `NewStub()` = test mock. Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup, `ListStoppedContainers`/`PruneImages`/`PruneContainers`/`PruneVolumes`/`PruneNetworks` for `tengiz cleanup`. `ContainerName(name, env)` helper. |
```

- [ ] **Step 5: Verify docs are consistent**

Run: `go test ./... -count=1 && go vet ./...`
Expected: PASS (no code changed; docs only)

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup docker housekeeping"
```

---

## Self-Review

**1. Spec coverage** (feature #6 "Docker Housekeeping" from `docs/FUTURES_FEATURES.md`):
- "kullanılmayan volume, network, container ve image'leri temizleme" → Task 2 `PruneVolumes`/`PruneNetworks`/`PruneContainers`/`PruneImages`; Task 3/4 `--all` flag
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → Task 3 `protectedContainerNames` + `ListStoppedContainers` label filter; registered apps/previews always protected
- "`tengiz cleanup` komutu" → Task 4 `cleanupCmd`
- "atık container/image'ler disk alanını tüketir" → default mode removes orphaned stopped containers + dangling images; per-app image retention already automatic at deploy time (documented in Global Constraints)
- Env-awareness (AGENTS.md) → `--env` passthrough via `getEnv`, env-scoped store
- Docs update rule (AGENTS.md) → Task 5
- No gaps. A raw `docker system prune` escape hatch and a `--keep-images` flag were deliberately excluded (YAGNI): precise label-aware pruning + the existing deploy-time `KeepLastNImages` cover the stated problem.

**2. Placeholder scan:** No TBD/TODO/placeholder steps; every step contains complete code, exact file paths, exact commands, and expected output. Reused code is repeated inline (e.g. the mock additions in Task 2) rather than "similar to Task N".

**3. Type consistency:**
- `cleanupOptions{ DryRun, All bool }`, `cleanupReport{ Containers, Images, Volumes, Networks []string; DryRun bool }` defined once in Task 3 and used identically in Task 4
- `runCleanup(ctx, rt runtime.Manager, store *config.Store, opts cleanupOptions) (*cleanupReport, error)` matches between Tasks 3 and 4
- `runtime.Manager` methods (`ListStoppedContainers`, `PruneImages(ctx, all, dryRun)`, `PruneContainers(ctx, names, dryRun)`, `PruneVolumes(ctx, dryRun)`, `PruneNetworks(ctx, dryRun)`) defined in Task 2 and consumed with identical signatures in Task 3/4
- `PruneImages(ctx, all, dryRun)` — `all` and `dryRun` bool order consistent everywhere (Task 2 interface/stub/mock, Task 3/4 calls)
- Field names match `types.PreviewEntry.ContainerName`, `types.AppEntry.DeploymentSuffix`, `types.AppConfig.Environment`
