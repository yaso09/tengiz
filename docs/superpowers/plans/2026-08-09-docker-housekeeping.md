# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning unused Docker containers, images, volumes, networks, and build cache — protecting Tengiz-managed containers and images via the `tengiz-app` label — plus an optional periodic scheduler (`--interval`).

**Architecture:** Five new prune primitives on the `runtime.Manager` interface (`PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache`) each wrap a single `docker ... prune` CLI invocation (consistent with the exec-based runtime). A new `internal/cleanup` package orchestrates them: `Cleaner.Clean(ctx, opts)` runs the enabled categories and returns a `Report`; `Scheduler` runs `Cleaner` on a ticker so `tengiz cleanup --interval 1h` runs periodically like `tengiz proxy`/`tengiz webhook`. Protection is label-based: containers already carry `tengiz-app=<app>` at creation, and the builder is extended to stamp the same label on built images, so both prune commands use `--filter "label!=tengiz-app"`.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` interface, Docker CLI via `os/exec`. No new external dependencies.

## Global Constraints

- Protect Tengiz-managed containers: container prune MUST use `docker container prune -f --filter "label!=tengiz-app"` — never removes containers Tengiz created
- Protect Tengiz-built images: builder stamps `--label tengiz-app=<app>` on docker builds; image prune MUST use `docker image prune -a -f --filter "label!=tengiz-app"`
- `tengiz cleanup` cleans all five categories by default: containers, images, volumes, networks, build cache
- `--dry-run` lists candidates without removing; counts for images/networks are estimates (negative label filters are unsupported by `docker image ls`/`docker network ls`)
- `--interval <duration>` runs cleanup periodically in the foreground; `Ctrl+C` stops it
- No new external Go dependencies; all Docker access via the `docker` CLI (`os/exec`), never the Docker SDK
- `runtime.Manager` grows by 5 methods → update `stubManager` (runtime.go) AND the mock implementations in `internal/cli/root_test.go`, `internal/proxy/proxy_test.go`, `internal/idle/idle_test.go` or the build breaks
- Nixpacks-built images cannot carry our label (nixpacks CLI has no label flag) — documented limitation; they may be pruned when unused. The currently-deployed image stays protected because its running container references it, and `KeepLastNImages` already manages per-app retention at deploy time
- Real prune commands must never prompt (always pass `-f`/`--force`)
- All commands must be safe to run on a host with zero containers/images (empty docker output → 0 removed, no error)
- Tests must pass with `go test ./... -v -count=1`; proxy/idle tests must continue to pass
- Work happens on a feature branch: `git checkout -b feat/docker-housekeeping` (per repo AGENTS.md rule)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` (new) | `PruneResult` type, per-category docker CLI arg builders, `countLines`, `parsePruneOutput`, the 5 `dockerRuntime` prune methods |
| `internal/runtime/prune_test.go` (new) | Unit tests for arg builders, `countLines`, `parsePruneOutput`, stub prune methods |
| `internal/runtime/runtime.go` (modify) | Add 5 methods to `Manager` interface + `stubManager` implementations |
| `internal/builder/builder.go` (modify) | Extract `dockerfileBuildArgs` and add `--label tengiz-app=<app>` to docker builds |
| `internal/builder/builder_test.go` (modify) | Test `dockerfileBuildArgs` includes the label |
| `internal/cleanup/cleanup.go` (new) | `Options`, `DefaultOptions`, `Report`, `Cleaner.Clean` orchestration |
| `internal/cleanup/cleanup_test.go` (new) | `fakeRT` test double + Cleaner orchestration tests |
| `internal/cleanup/scheduler.go` (new) | `Scheduler` periodic runner (`Start`/`Stop`/`run`) |
| `internal/cleanup/scheduler_test.go` (new) | Scheduler start/stop timing tests |
| `internal/cli/root.go` (modify) | `cleanupCmd` + `--dry-run`/`--interval` flags + `init()` registration + `printCleanupReport` |
| `internal/cli/root_test.go` (modify) | Add 5 prune methods to `mockRTForDeploy` + cleanup command registration/flag/report tests |
| `internal/proxy/proxy_test.go` (modify) | Add 5 prune methods to `mockRuntime` (compile fix) |
| `internal/idle/idle_test.go` (modify) | Add 5 prune methods to `mockRuntime` (compile fix) |
| `README.md` (modify) | Document `tengiz cleanup` + feature bullet |
| `docs/FUTURES_FEATURES.md` (modify) | Mark feature #6 implemented (auto-workflow may overwrite) |

---

### Task 1: Prune helpers in `internal/runtime`

Pure functions only (no Docker calls) so they are fully unit-testable: `PruneResult`, per-category docker CLI arg builders, `countLines`, `parsePruneOutput`.

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing (standalone helpers)
- Produces: `type PruneResult struct { Type string; Removed int; Space string }`, `pruneContainerArgs(dryRun bool) []string`, `pruneImageArgs(dryRun bool) []string`, `pruneVolumeArgs(dryRun bool) []string`, `pruneNetworkArgs(dryRun bool) []string`, `pruneBuildCacheArgs(dryRun bool) []string`, `countLines(output string) int`, `parsePruneOutput(resType, output string) PruneResult`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing tests**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"reflect"
	"testing"
)

func TestPruneContainerArgs(t *testing.T) {
	tests := []struct {
		name   string
		dryRun bool
		want   []string
	}{
		{
			name:   "real",
			dryRun: false,
			want:   []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:   "dry run",
			dryRun: true,
			want:   []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.ID}}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneContainerArgs(tt.dryRun)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pruneContainerArgs(%v) = %v, want %v", tt.dryRun, got, tt.want)
			}
		})
	}
}

func TestPruneImageArgs(t *testing.T) {
	tests := []struct {
		name   string
		dryRun bool
		want   []string
	}{
		{name: "real", dryRun: false, want: []string{"image", "prune", "-a", "-f", "--filter", "label!=tengiz-app"}},
		{name: "dry run", dryRun: true, want: []string{"images", "-q"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneImageArgs(tt.dryRun)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pruneImageArgs(%v) = %v, want %v", tt.dryRun, got, tt.want)
			}
		})
	}
}

func TestPruneVolumeArgs(t *testing.T) {
	tests := []struct {
		name   string
		dryRun bool
		want   []string
	}{
		{name: "real", dryRun: false, want: []string{"volume", "prune", "-f"}},
		{name: "dry run", dryRun: true, want: []string{"volume", "ls", "-q", "--filter", "dangling=true"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneVolumeArgs(tt.dryRun)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pruneVolumeArgs(%v) = %v, want %v", tt.dryRun, got, tt.want)
			}
		})
	}
}

func TestPruneNetworkArgs(t *testing.T) {
	tests := []struct {
		name   string
		dryRun bool
		want   []string
	}{
		{name: "real", dryRun: false, want: []string{"network", "prune", "-f"}},
		{name: "dry run", dryRun: true, want: []string{"network", "ls", "-q"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneNetworkArgs(tt.dryRun)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pruneNetworkArgs(%v) = %v, want %v", tt.dryRun, got, tt.want)
			}
		})
	}
}

func TestPruneBuildCacheArgs(t *testing.T) {
	tests := []struct {
		name   string
		dryRun bool
		want   []string
	}{
		{name: "real", dryRun: false, want: []string{"builder", "prune", "-f"}},
		{name: "dry run", dryRun: true, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneBuildCacheArgs(tt.dryRun)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pruneBuildCacheArgs(%v) = %v, want %v", tt.dryRun, got, tt.want)
			}
		})
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name, output string
		want         int
	}{
		{name: "empty", output: "", want: 0},
		{name: "single", output: "abc123\n", want: 1},
		{name: "multiple with trailing blank", output: "abc123\ndef456\n\n", want: 2},
		{name: "whitespace only", output: "  \n \n", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countLines(tt.output); got != tt.want {
				t.Errorf("countLines(%q) = %d, want %d", tt.output, got, tt.want)
			}
		})
	}
}

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name        string
		resType     string
		output      string
		wantRemoved int
		wantSpace   string
	}{
		{
			name:        "containers",
			resType:     "containers",
			output:      "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 12.5MB\n",
			wantRemoved: 2,
			wantSpace:   "12.5MB",
		},
		{
			name:        "nothing to remove",
			resType:     "containers",
			output:      "Total reclaimed space: 0B\n",
			wantRemoved: 0,
			wantSpace:   "0B",
		},
		{
			name:        "images",
			resType:     "images",
			output:      "Deleted Images:\nuntagged: alpine:latest\ndeleted: sha256:4e38e38c8ce0\n\nTotal reclaimed space: 16.43MB\n",
			wantRemoved: 2,
			wantSpace:   "16.43MB",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := parsePruneOutput(tt.resType, tt.output)
			if res.Type != tt.resType {
				t.Errorf("Type = %q, want %q", res.Type, tt.resType)
			}
			if res.Removed != tt.wantRemoved {
				t.Errorf("Removed = %d, want %d", res.Removed, tt.wantRemoved)
			}
			if res.Space != tt.wantSpace {
				t.Errorf("Space = %q, want %q", res.Space, tt.wantSpace)
			}
		})
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestPruneContainerArgs|TestPruneImageArgs|TestPruneVolumeArgs|TestPruneNetworkArgs|TestPruneBuildCacheArgs|TestCountLines|TestParsePruneOutput" -v -count=1`

Expected: FAIL with `undefined: pruneContainerArgs` (and the other helpers)

- [ ] **Step 4: Implement the helpers**

Create `internal/runtime/prune.go`:

```go
package runtime

import "strings"

type PruneResult struct {
	Type    string
	Removed int
	Space   string
}

// protectLabel marks Tengiz-managed containers and images. Both prune commands
// filter with label!=tengiz-app so Tengiz resources are never removed.
const protectLabel = "tengiz-app"

func pruneContainerArgs(dryRun bool) []string {
	if dryRun {
		return []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=" + protectLabel, "--format", "{{.ID}}"}
	}
	return []string{"container", "prune", "-f", "--filter", "label!=" + protectLabel}
}

func pruneImageArgs(dryRun bool) []string {
	if dryRun {
		return []string{"images", "-q"}
	}
	return []string{"image", "prune", "-a", "-f", "--filter", "label!=" + protectLabel}
}

func pruneVolumeArgs(dryRun bool) []string {
	if dryRun {
		return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
	}
	return []string{"volume", "prune", "-f"}
}

func pruneNetworkArgs(dryRun bool) []string {
	if dryRun {
		return []string{"network", "ls", "-q"}
	}
	return []string{"network", "prune", "-f"}
}

func pruneBuildCacheArgs(dryRun bool) []string {
	if dryRun {
		return nil
	}
	return []string{"builder", "prune", "-f"}
}

func countLines(output string) int {
	n := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func parsePruneOutput(resType, output string) PruneResult {
	res := PruneResult{Type: resType}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space: ") {
			res.Space = strings.TrimPrefix(line, "Total reclaimed space: ")
			continue
		}
		if strings.HasPrefix(line, "Deleted ") {
			continue
		}
		res.Removed++
	}
	return res
}
```

Note: the docker CLI emits `Deleted Containers:` / `Deleted Images:` headers (skipped via the `"Deleted "` prefix) followed by one line per removed item (e.g. container IDs, or `untagged: ...` / `deleted: sha256:...` lines for images, both counted), then `Total reclaimed space: <size>`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestPruneContainerArgs|TestPruneImageArgs|TestPruneVolumeArgs|TestPruneNetworkArgs|TestPruneBuildCacheArgs|TestCountLines|TestParsePruneOutput" -v -count=1`

Expected: PASS (all tests)

- [ ] **Step 6: Run the full runtime test suite**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: PASS (existing tests unaffected)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: add prune helper args and output parser for docker housekeeping"
```

---

### Task 2: Prune methods on `runtime.Manager`

Wire the helpers into the `dockerRuntime`, grow the `Manager` interface by 5 methods, implement them on `stubManager`, and update the 3 mock implementations in test files so the package tree still compiles.

**Files:**
- Modify: `internal/runtime/prune.go` — add `dockerRuntime` prune methods + two shared exec helpers
- Modify: `internal/runtime/runtime.go:31-49` (Manager interface) and `:51-123` (stubManager)
- Modify: `internal/cli/root_test.go:69-100` (`mockRTForDeploy`)
- Modify: `internal/proxy/proxy_test.go:15-35` (`mockRuntime`)
- Modify: `internal/idle/idle_test.go:14-34` (`mockRuntime`)
- Test: `internal/runtime/prune_test.go` — append stub-method tests

**Interfaces:**
- Consumes: `PruneResult`, arg builders, `countLines`, `parsePruneOutput` from Task 1
- Produces: `PruneContainers(ctx context.Context, dryRun bool) (PruneResult, error)`, `PruneImages(...)`, `PruneVolumes(...)`, `PruneNetworks(...)`, `PruneBuildCache(...)` on the `Manager` interface — used by the cleanup package in Task 4

- [ ] **Step 1: Write the failing tests (stub methods)**

Append to `internal/runtime/prune_test.go`:

```go
func TestStubPruneMethods(t *testing.T) {
	ctx := context.Background()
	m := NewStub()
	cases := []struct {
		name string
		run  func() (PruneResult, error)
	}{
		{"PruneContainers", func() (PruneResult, error) { return m.PruneContainers(ctx, false) }},
		{"PruneImages", func() (PruneResult, error) { return m.PruneImages(ctx, false) }},
		{"PruneVolumes", func() (PruneResult, error) { return m.PruneVolumes(ctx, false) }},
		{"PruneNetworks", func() (PruneResult, error) { return m.PruneNetworks(ctx, false) }},
		{"PruneBuildCache", func() (PruneResult, error) { return m.PruneBuildCache(ctx, false) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := tc.run()
			if err != nil {
				t.Fatalf("%s() error = %v", tc.name, err)
			}
			if res.Type == "" {
				t.Errorf("%s() returned empty Type", tc.name)
			}
		})
	}
}
```

Add `"context"` to the imports block of `internal/runtime/prune_test.go` (it already imports `reflect`, `testing`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestStubPruneMethods" -v -count=1`

Expected: FAIL — `m.PruneContainers undefined (type Manager has no field or method PruneContainers)`

- [ ] **Step 3: Add the 5 methods to the `Manager` interface**

In `internal/runtime/runtime.go`, add these lines to the `Manager` interface (after the `KeepLastNImages` line, line 36):

```go
	PruneContainers(ctx context.Context, dryRun bool) (PruneResult, error)
	PruneImages(ctx context.Context, dryRun bool) (PruneResult, error)
	PruneVolumes(ctx context.Context, dryRun bool) (PruneResult, error)
	PruneNetworks(ctx context.Context, dryRun bool) (PruneResult, error)
	PruneBuildCache(ctx context.Context, dryRun bool) (PruneResult, error)
```

- [ ] **Step 4: Implement the `dockerRuntime` prune methods**

Replace the entire import block of `internal/runtime/prune.go` with:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)
```

Append to `internal/runtime/prune.go`:

```go
func (r *dockerRuntime) pruneCount(ctx context.Context, resType string, args []string) (PruneResult, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneResult{Type: resType}, fmt.Errorf("docker %s list: %w\n%s", resType, err, string(out))
	}
	return PruneResult{Type: resType, Removed: countLines(string(out))}, nil
}

func (r *dockerRuntime) pruneRun(ctx context.Context, resType string, args []string) (PruneResult, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneResult{Type: resType}, fmt.Errorf("docker %s prune: %w\n%s", resType, err, string(out))
	}
	return parsePruneOutput(resType, string(out)), nil
}

func (r *dockerRuntime) PruneContainers(ctx context.Context, dryRun bool) (PruneResult, error) {
	if dryRun {
		return r.pruneCount(ctx, "containers", pruneContainerArgs(true))
	}
	return r.pruneRun(ctx, "containers", pruneContainerArgs(false))
}

func (r *dockerRuntime) PruneImages(ctx context.Context, dryRun bool) (PruneResult, error) {
	if dryRun {
		return r.pruneCount(ctx, "images", pruneImageArgs(true))
	}
	return r.pruneRun(ctx, "images", pruneImageArgs(false))
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context, dryRun bool) (PruneResult, error) {
	if dryRun {
		return r.pruneCount(ctx, "volumes", pruneVolumeArgs(true))
	}
	return r.pruneRun(ctx, "volumes", pruneVolumeArgs(false))
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context, dryRun bool) (PruneResult, error) {
	if dryRun {
		return r.pruneCount(ctx, "networks", pruneNetworkArgs(true))
	}
	return r.pruneRun(ctx, "networks", pruneNetworkArgs(false))
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context, dryRun bool) (PruneResult, error) {
	if dryRun {
		return PruneResult{Type: "build cache"}, nil
	}
	return r.pruneRun(ctx, "build cache", pruneBuildCacheArgs(false))
}
```

- [ ] **Step 5: Implement the 5 methods on `stubManager`**

In `internal/runtime/runtime.go`, append after the `stubManager.KeepLastNImages` method (lines 117-119):

```go
func (m *stubManager) PruneContainers(ctx context.Context, dryRun bool) (PruneResult, error) {
	return PruneResult{Type: "containers"}, nil
}

func (m *stubManager) PruneImages(ctx context.Context, dryRun bool) (PruneResult, error) {
	return PruneResult{Type: "images"}, nil
}

func (m *stubManager) PruneVolumes(ctx context.Context, dryRun bool) (PruneResult, error) {
	return PruneResult{Type: "volumes"}, nil
}

func (m *stubManager) PruneNetworks(ctx context.Context, dryRun bool) (PruneResult, error) {
	return PruneResult{Type: "networks"}, nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context, dryRun bool) (PruneResult, error) {
	return PruneResult{Type: "build cache"}, nil
}
```

- [ ] **Step 6: Update the 3 mock implementations in test files (compile fix)**

`internal/cli/root_test.go` — append after the `mockRTForDeploy.KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) PruneContainers(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "containers"}, nil }
func (m *mockRTForDeploy) PruneImages(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "images"}, nil }
func (m *mockRTForDeploy) PruneVolumes(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "volumes"}, nil }
func (m *mockRTForDeploy) PruneNetworks(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "networks"}, nil }
func (m *mockRTForDeploy) PruneBuildCache(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "build cache"}, nil }
```

`internal/proxy/proxy_test.go` — append after the `mockRuntime.KeepLastNImages` method (line 34):

```go
func (m *mockRuntime) PruneContainers(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "containers"}, nil }
func (m *mockRuntime) PruneImages(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "images"}, nil }
func (m *mockRuntime) PruneVolumes(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "volumes"}, nil }
func (m *mockRuntime) PruneNetworks(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "networks"}, nil }
func (m *mockRuntime) PruneBuildCache(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "build cache"}, nil }
```

`internal/idle/idle_test.go` — append after the `mockRuntime.KeepLastNImages` method (line 33):

```go
func (m *mockRuntime) PruneContainers(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "containers"}, nil }
func (m *mockRuntime) PruneImages(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "images"}, nil }
func (m *mockRuntime) PruneVolumes(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "volumes"}, nil }
func (m *mockRuntime) PruneNetworks(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "networks"}, nil }
func (m *mockRuntime) PruneBuildCache(ctx context.Context, dryRun bool) (runtime.PruneResult, error) { return runtime.PruneResult{Type: "build cache"}, nil }
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestStubPruneMethods" -v -count=1`

Expected: PASS

- [ ] **Step 8: Build the whole module and run affected packages**

Run: `go build ./...`

Expected: no errors (all mock/stub implementations satisfy `Manager`)

Run: `go test ./internal/proxy/... ./internal/idle/... ./internal/cli/... -count=1`

Expected: PASS (proxy tests are slow, ~2s each — be patient)

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/runtime.go internal/runtime/prune_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add prune primitives to runtime.Manager for docker housekeeping"
```

---

### Task 3: Stamp `tengiz-app` label on built images

Make docker-built images carry the `tengiz-app=<app>` label so `PruneImages` never removes Tengiz-built images. Container protection already exists at run time.

**Files:**
- Modify: `internal/builder/builder.go`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `dockerfileBuildArgs` (new private helper returning the shared docker build flags) and `--label tengiz-app=<app>` on every dockerfile-based build — complements the `label!=tengiz-app` prune filter from Task 2

- [ ] **Step 1: Explore current builder code**

Run: `grep -n "docker build" internal/builder/builder.go`

Read `internal/builder/builder.go` around the docker build invocations to see how flags are assembled and where `appName` is available.

- [ ] **Step 2: Refactor build flags into a shared helper**

Add a private helper to `internal/builder/builder.go` (next to the docker build logic):

```go
func dockerfileBuildArgs(appName string, baseArgs ...string) []string {
	args := []string{"build", "--label", "tengiz-app=" + appName}
	return append(args, baseArgs...)
}
```

- [ ] **Step 3: Use the helper in the docker build**

In `internal/builder/builder.go`, `buildWithDockerfile` (lines 57-91) is the ONLY docker build path — nixpacks uses the `nixpacks` CLI instead. Replace the args assembly (lines 69-71):

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

with:

```go
	args := dockerfileBuildArgs(appName, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

`appName` is already a parameter of `buildWithDockerfile`, so no threading needed. The `--label tengiz-app=<app>` is prepended before the build secrets and `-t`/dir args.

- [ ] **Step 4: Write the failing test**

Append to `internal/builder/builder_test.go` and add `"reflect"` to its import block:

```go
func TestDockerfileBuildArgs(t *testing.T) {
	got := dockerfileBuildArgs("myapp", "-t", "dev-myapp-abc123", ".")
	want := []string{"build", "--label", "tengiz-app=myapp", "-t", "dev-myapp-abc123", "."}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dockerfileBuildArgs() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/builder/... -v -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: stamp tengiz-app label on docker-built images"
```

---

### Task 4: `internal/cleanup` — Cleaner orchestration

Create the `cleanup` package. `Cleaner` iterates enabled categories and calls the corresponding `runtime.Manager` prune method, returning a `Report`.

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.Manager` (interface), `runtime.PruneResult`
- Produces: `Options`, `DefaultOptions`, `Report`, `Cleaner`, `Cleaner.Clean(ctx, opts) (Report, error)` — used by the CLI command (Task 6) and the `Scheduler` (Task 5)

- [ ] **Step 1: Write the failing tests**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

type fakeRT struct {
	runtime.Manager
	containerCalls atomic.Int32
	imageCalls     atomic.Int32
	volumeCalls    atomic.Int32
	networkCalls   atomic.Int32
	cacheCalls     atomic.Int32
	lastDryRun     bool
	containerResult runtime.PruneResult
	imageResult     runtime.PruneResult
	volumeResult    runtime.PruneResult
	networkResult   runtime.PruneResult
	cacheResult     runtime.PruneResult
	containerErr    error
}

func (f *fakeRT) PruneContainers(ctx context.Context, dryRun bool) (runtime.PruneResult, error) {
	f.containerCalls.Add(1)
	f.lastDryRun = dryRun
	return f.containerResult, f.containerErr
}
func (f *fakeRT) PruneImages(ctx context.Context, dryRun bool) (runtime.PruneResult, error) {
	f.imageCalls.Add(1)
	f.lastDryRun = dryRun
	return f.imageResult, nil
}
func (f *fakeRT) PruneVolumes(ctx context.Context, dryRun bool) (runtime.PruneResult, error) {
	f.volumeCalls.Add(1)
	f.lastDryRun = dryRun
	return f.volumeResult, nil
}
func (f *fakeRT) PruneNetworks(ctx context.Context, dryRun bool) (runtime.PruneResult, error) {
	f.networkCalls.Add(1)
	f.lastDryRun = dryRun
	return f.networkResult, nil
}
func (f *fakeRT) PruneBuildCache(ctx context.Context, dryRun bool) (runtime.PruneResult, error) {
	f.cacheCalls.Add(1)
	f.lastDryRun = dryRun
	return f.cacheResult, nil
}

func TestCleanRunsAllCategories(t *testing.T) {
	f := &fakeRT{Manager: runtime.NewStub(),
		containerResult: runtime.PruneResult{Type: "containers", Removed: 3, Space: "12.5MB"},
		imageResult:     runtime.PruneResult{Type: "images", Removed: 2, Space: "16.43MB"},
		volumeResult:    runtime.PruneResult{Type: "volumes", Removed: 1, Space: "36B"},
		networkResult:   runtime.PruneResult{Type: "networks", Removed: 0, Space: "0B"},
		cacheResult:     runtime.PruneResult{Type: "build cache", Removed: 0, Space: "0B"},
	}
	c := New(f)
	report, err := c.Clean(context.Background(), DefaultOptions())
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if len(report.Results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(report.Results))
	}
	wantOrder := []string{"containers", "images", "volumes", "networks", "build cache"}
	for i, want := range wantOrder {
		if report.Results[i].Type != want {
			t.Errorf("result[%d] = %q, want %q", i, report.Results[i].Type, want)
		}
	}
	if report.DryRun {
		t.Error("expected DryRun=false in report")
	}
}

func TestCleanRespectsDisabledCategories(t *testing.T) {
	f := &fakeRT{Manager: runtime.NewStub(), imageResult: runtime.PruneResult{Type: "images"}}
	c := New(f)
	opts := DefaultOptions()
	opts.Images = false
	report, err := c.Clean(context.Background(), opts)
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if f.imageCalls.Load() != 0 {
		t.Error("expected PruneImages not to be called when Images disabled")
	}
	if len(report.Results) != 4 {
		t.Errorf("expected 4 results, got %d", len(report.Results))
	}
	for _, r := range report.Results {
		if r.Type == "images" {
			t.Error("images should not be in results when disabled")
		}
	}
}

func TestCleanPassesDryRun(t *testing.T) {
	f := &fakeRT{Manager: runtime.NewStub()}
	c := New(f)
	opts := DefaultOptions()
	opts.DryRun = true
	report, err := c.Clean(context.Background(), opts)
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if !report.DryRun {
		t.Error("expected DryRun=true in report")
	}
	if !f.lastDryRun {
		t.Error("expected dryRun=true propagated to runtime calls")
	}
}

func TestCleanPropagatesError(t *testing.T) {
	f := &fakeRT{Manager: runtime.NewStub(), containerErr: errors.New("docker exploded")}
	c := New(f)
	_, err := c.Clean(context.Background(), DefaultOptions())
	if err == nil {
		t.Fatal("expected error from Clean(), got nil")
	}
	if !strings.Contains(err.Error(), "containers") {
		t.Errorf("expected error to mention category, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cleanup/ -v -count=1`

Expected: FAIL with `undefined: New` / `undefined: DefaultOptions` / `undefined: Report`

- [ ] **Step 3: Implement the Cleaner**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"context"
	"fmt"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

func DefaultOptions() Options {
	return Options{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
}

type Report struct {
	DryRun  bool
	Results []runtime.PruneResult
}

type Cleaner struct {
	rt runtime.Manager
}

func New(rt runtime.Manager) *Cleaner {
	return &Cleaner{rt: rt}
}

func (c *Cleaner) Clean(ctx context.Context, opts Options) (Report, error) {
	report := Report{DryRun: opts.DryRun}
	steps := []struct {
		enabled bool
		label   string
		run     func(context.Context, bool) (runtime.PruneResult, error)
	}{
		{opts.Containers, "containers", c.rt.PruneContainers},
		{opts.Images, "images", c.rt.PruneImages},
		{opts.Volumes, "volumes", c.rt.PruneVolumes},
		{opts.Networks, "networks", c.rt.PruneNetworks},
		{opts.BuildCache, "build cache", c.rt.PruneBuildCache},
	}
	for _, s := range steps {
		if !s.enabled {
			continue
		}
		res, err := s.run(ctx, opts.DryRun)
		if err != nil {
			return report, fmt.Errorf("%s prune: %w", s.label, err)
		}
		report.Results = append(report.Results, res)
	}
	return report, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/ -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup orchestrator for docker housekeeping"
```

---

### Task 5: `internal/cleanup` — Scheduler

Add `Scheduler` so `tengiz cleanup --interval 1h` runs periodically until interrupted. Uses the `Cleaner` from Task 4. Patterned after the existing `idle` package's timer loop but driven by a fixed ticker.

**Files:**
- Create: `internal/cleanup/scheduler.go`
- Test: `internal/cleanup/scheduler_test.go`

**Interfaces:**
- Consumes: `runtime.Manager`, `Cleaner`, `Options`
- Produces: `Scheduler`, `NewScheduler(rt, interval, opts)`, `Start()`, `Stop()` — used by the CLI command (Task 6)

- [ ] **Step 1: Write the failing tests**

Create `internal/cleanup/scheduler_test.go`:

```go
package cleanup

import (
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestSchedulerRunsCleanPeriodically(t *testing.T) {
	f := &fakeRT{Manager: runtime.NewStub(), containerResult: runtime.PruneResult{Type: "containers", Removed: 1}}
	s := NewScheduler(f, 30*time.Millisecond, DefaultOptions())
	s.Start()
	defer s.Stop()
	time.Sleep(120 * time.Millisecond)
	if f.containerCalls.Load() == 0 {
		t.Error("expected PruneContainers to be called by the scheduler")
	}
}

func TestSchedulerStopStopsCleaner(t *testing.T) {
	f := &fakeRT{Manager: runtime.NewStub()}
	s := NewScheduler(f, 10*time.Millisecond, DefaultOptions())
	s.Start()
	time.Sleep(50 * time.Millisecond)
	s.Stop()
	before := f.containerCalls.Load()
	time.Sleep(50 * time.Millisecond)
	if got := f.containerCalls.Load(); got != before {
		t.Errorf("calls increased after Stop: before=%d after=%d", before, got)
	}
}
```

The first test uses `time.Sleep` with ~30ms granularity (consistent with the `idle` package's time-sensitive tests).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cleanup/ -run "TestScheduler" -v -count=1`

Expected: FAIL with `undefined: NewScheduler`

- [ ] **Step 3: Implement the Scheduler**

Create `internal/cleanup/scheduler.go`:

```go
package cleanup

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Scheduler struct {
	rt       runtime.Manager
	opts     Options
	interval time.Duration
	mu       sync.Mutex
	cancel   context.CancelFunc
}

func NewScheduler(rt runtime.Manager, interval time.Duration, opts Options) *Scheduler {
	return &Scheduler{rt: rt, opts: opts, interval: interval}
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.run(ctx)
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *Scheduler) run(ctx context.Context) {
	c := New(s.rt)
	for {
		report, err := c.Clean(ctx, s.opts)
		if err != nil {
			log.Printf("[cleanup] scheduled run failed: %v", err)
		} else {
			log.Printf("[cleanup] run complete: %d categories processed (dry run: %v)", len(report.Results), report.DryRun)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.interval):
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/ -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/scheduler.go internal/cleanup/scheduler_test.go
git commit -m "feat: add scheduled cleanup runner"
```

---

### Task 6: `tengiz cleanup` CLI command

Add the Cobra command wiring the Cleaner and Scheduler, plus the report printer.

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `cleanup.Cleaner`, `cleanup.Scheduler`, `cleanup.Report`
- Produces: `cleanupCmd` (`tengiz cleanup --dry-run --interval 1h`), `printCleanupReport(report)` — wired into the CLI via `init()`

- [ ] **Step 1: Write the failing tests**

Add `"github.com/yaso09/tengiz/internal/cleanup"` to the import block of `internal/cli/root_test.go` (after the `config` import), then append:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd.Name() != "cleanup" {
		t.Fatalf("expected cleanup command, got %q", cmd.Name())
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	if cleanupCmd.Flags().Lookup("dry-run") == nil {
		t.Error("expected --dry-run flag")
	}
	if cleanupCmd.Flags().Lookup("interval") == nil {
		t.Error("expected --interval flag")
	}
}

func TestPrintCleanupReport(t *testing.T) {
	report := cleanup.Report{
		DryRun: true,
		Results: []runtime.PruneResult{
			{Type: "containers", Removed: 2, Space: "12.5MB"},
			{Type: "images", Removed: 0, Space: "0B"},
		},
	}
	out := captureOutput(func() { printCleanupReport(report) })
	for _, want := range []string{"dry run", "containers", "2", "12.5MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestCleanupCommandFlags|TestPrintCleanupReport" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` and `undefined: cleanup`

- [ ] **Step 3: Implement the command**

Add the `cleanup` import to `internal/cli/root.go` (in the import block):

```go
"github.com/yaso09/tengiz/internal/cleanup"
```

Add `cleanupCmd` and `printCleanupReport` to `internal/cli/root.go` (top-level, alongside the other commands):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Remove unused Docker resources to reclaim disk space.

Tengiz-managed containers and images are protected via the "tengiz-app" label
and are never removed by this command.

Use --dry-run to preview what would be removed without removing anything.
Use --interval 1h to run cleanup periodically (foreground; Ctrl+C to stop).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		intervalStr, _ := cmd.Flags().GetString("interval")

		opts := cleanup.DefaultOptions()
		opts.DryRun = dryRun

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if intervalStr != "" {
			interval, err := time.ParseDuration(intervalStr)
			if err != nil {
				return fmt.Errorf("invalid --interval %q: %w", intervalStr, err)
			}
			if interval <= 0 {
				return fmt.Errorf("--interval must be a positive duration")
			}
			sched := cleanup.NewScheduler(rt, interval, opts)
			sched.Start()
			defer sched.Stop()

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()
			fmt.Printf("[tengiz] running cleanup every %s (dry run: %v). Press Ctrl+C to stop.\n", interval, dryRun)
			<-ctx.Done()
			return nil
		}

		report, err := cleanup.New(rt).Clean(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupReport(report)
		return nil
	},
}

func printCleanupReport(report cleanup.Report) {
	if report.DryRun {
		fmt.Println("[tengiz] cleanup dry run — nothing was removed:")
	} else {
		fmt.Println("[tengiz] cleanup complete:")
	}
	if len(report.Results) == 0 {
		fmt.Println("  nothing to do")
		return
	}
	fmt.Printf("  %-13s %9s %12s\n", "CATEGORY", "COUNT", "SPACE")
	for _, res := range report.Results {
		space := res.Space
		if space == "" {
			space = "-"
		}
		fmt.Printf("  %-13s %9d %12s\n", res.Type, res.Removed, space)
	}
}
```

- [ ] **Step 4: Register the command and flags in `init()`**

In `internal/cli/root.go`, inside the existing `init()` function (near the other `rootCmd.AddCommand` / flag registrations):

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().String("interval", "", "run cleanup periodically every duration (e.g. 1h, 30m); empty runs once and exits")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestCleanupCommandFlags|TestPrintCleanupReport" -v -count=1`

Expected: PASS

- [ ] **Step 6: Build and run all tests**

Run: `go build ./...`

Expected: no errors

Run: `go test ./... -count=1`

Expected: PASS (all packages, including proxy — be patient)

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command with dry-run and interval"
```

---

### Task 7: Documentation

Update `README.md` with the new command and mark the feature in `docs/FUTURES_FEATURES.md` (auto-workflow may overwrite).

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: nothing
- Produces: user-facing docs

- [ ] **Step 1: Add a feature bullet to `README.md`**

Add to the feature list:

```
- **Docker housekeeping** — `tengiz cleanup` reclaims disk space by pruning unused containers, images, volumes, networks, and build cache; Tengiz-managed resources are protected via the `tengiz-app` label
```

- [ ] **Step 2: Add a CLI section to `README.md`**

Add to the CLI reference section (following the existing `### \`tengiz <cmd>\`` convention):

```
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space on single-server deployments:

```bash
tengiz cleanup              # prune stopped containers, unused images, volumes, networks, and build cache
tengiz cleanup --dry-run    # show what would be removed without removing anything
tengiz cleanup --interval 1h  # run cleanup every hour (foreground; Ctrl+C to stop)
```

Tengiz-managed containers and images carry the `tengiz-app` label and are protected — they are never removed.
```

- [ ] **Step 3: Mark the feature in `docs/FUTURES_FEATURES.md`**

Flip the entry for Docker housekeeping (auto-workflow may overwrite; skip if the file is auto-generated).

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

Before declaring completion, verify each of these (referencing the corresponding task):

- [ ] `tengiz cleanup` appears in `tengiz --help`
- [ ] `tengiz cleanup --dry-run` shows what would be removed and removes nothing (real Docker host smoke test)
- [ ] Tengiz containers/images survive `tengiz cleanup` because of the `label!=tengiz-app` filter (real Docker host smoke test)
- [ ] All mock/stub implementations of `runtime.Manager` in the repo compile (grep for `KeepLastNImages` in `_test.go` files to find any mock we missed)
- [ ] No new external dependencies in `go.mod` / `go.sum`
- [ ] `gofmt -l .` is empty
- [ ] `go vet ./...` passes
- [ ] `go test ./... -v -count=1` passes

## Final Verification

Run the full suite on the finished branch:

```bash
go build ./...
gofmt -l .
go vet ./...
go test ./... -v -count=1
```

All commands must pass with zero failures. Then:

```bash
git status
git log --oneline -10
```
