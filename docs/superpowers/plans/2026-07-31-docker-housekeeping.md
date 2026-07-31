# Docker Housekeeping (tengiz cleanup) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, networks, and build cache with label-based protection of Tengiz-managed containers and a dry-run preview mode.

**Architecture:** A new `runtime.Manager.Cleanup(ctx, opts)` method wraps the `docker * prune` CLI family (`docker container prune`, `docker image prune -a`, `docker volume prune`, `docker network prune`, `docker builder prune`). Command construction and output parsing live in pure, unit-testable functions in `internal/runtime/cleanup.go` (following the existing `buildLogArgs`/`buildRunArgs` pattern). Tengiz-managed containers are protected by default via the `--filter label!=tengiz-app` prune filter, so scale-to-zero cold starts and rollbacks keep working. `--dry-run` lists what would be removed using non-destructive `docker ps`/`docker images`/`docker volume ls`/`docker network ls`/`docker builder du` queries. The CLI command in `internal/cli/root.go` maps flags to `CleanupOptions` and prints a summary.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (Docker CLI, no Docker SDK), existing `runtime.Manager` interface. No new external dependencies.

## Global Constraints

- Command: `tengiz cleanup` — a single command, flags only, `cobra.NoArgs`
- Safe defaults: only `--containers` and `--images` are enabled by default; `--volumes`, `--networks`, `--build-cache` require explicit opt-in or `--all`
- Tengiz-managed containers (label `tengiz-app`) are protected by default via `--filter label!=tengiz-app` on the container prune; opt out with `--prune-stopped-tengiz`
- `docker image prune -a -f` never removes images referenced by any container (running or stopped) — the current deployment's image and rollback image stay intact
- `--dry-run` performs no mutation: it reports candidate counts (containers/images/volumes/networks) and reclaimable build-cache bytes
- No new external Go dependencies; Docker CLI must be present (existing `NewDocker()` already errors clearly if not)
- Adding `Cleanup` to the `Manager` interface requires updating all 4 implementers in the same commit: `stubManager`, `mockRTForDeploy` (cli), `mockRuntime` (idle), `mockRuntime` (proxy) — otherwise the package won't compile
- The cleanup command is host-wide and ignores the global `--env` flag (it protects containers in every environment via the label filter)
- `go vet ./...`, `go test ./... -v -count=1`, and `go build -o tengiz .` must all pass at the end of every task

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupResult` types, `Cleanup` to `Manager` interface, no-op stub impl |
| `internal/runtime/cleanup.go` | Pure docker command builders + output parsers + `dockerRuntime.Cleanup` exec implementation |
| `internal/runtime/cleanup_test.go` | Unit tests for helpers/parsers, stub test, fake-docker integration tests for `Cleanup` |
| `internal/cli/root.go` | `cleanupCmd` Cobra command, flags, `cleanupOptionsFromFlags`, `printCleanupResult` |
| `internal/cli/root_test.go` | CLI registration/flag/options tests; add `Cleanup` to `mockRTForDeploy` |
| `internal/idle/idle_test.go` | Add `Cleanup` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` command |
| `docs/FUTURES_FEATURES.md` | Mark #6 and #56 as implemented |

Pre-flight (do once, before Task 1):

- [ ] Create the feature branch and confirm the baseline is green:
```bash
git checkout -b feat/docker-housekeeping
go build -o tengiz .
go test ./... -v -count=1
go vet ./...
```
Expected: build succeeds, all existing tests pass, `go vet` reports no issues.

---

### Task 1: Runtime cleanup types, interface method, stub, and mock updates

**Files:**
- Modify: `internal/runtime/runtime.go` (add types after `RunOptions`, interface method after `KeepLastNImages` at line 36, stub method after line 119)
- Modify: `internal/runtime/cleanup_test.go` (add stub test)
- Modify: `internal/cli/root_test.go:100` (add `Cleanup` to `mockRTForDeploy`)
- Modify: `internal/idle/idle_test.go:33` (add `Cleanup` to `mockRuntime`)
- Modify: `internal/proxy/proxy_test.go` (add `Cleanup` to `mockRuntime`)

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions` struct (fields `Containers`, `Images`, `Volumes`, `Networks`, `BuildCache`, `ProtectTengizContainers`, `DryRun` — all `bool`), `runtime.CleanupResult` struct, and `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` on the `Manager` interface

- [ ] **Step 1: Write the failing stub test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	result, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !result.DryRun {
		t.Errorf("CleanupResult.DryRun = false, want true")
	}
}
```

(`context` is already imported in `cleanup_test.go`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`
Expected: FAIL — `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add the types to `internal/runtime/runtime.go`**

Insert after the `RunOptions` struct:

```go
type CleanupOptions struct {
	Containers              bool
	Images                  bool
	Volumes                 bool
	Networks                bool
	BuildCache              bool
	ProtectTengizContainers bool
	DryRun                  bool
}

type CleanupResult struct {
	ContainersRemoved int
	ContainersSpace   string
	ImagesRemoved     int
	ImagesSpace       string
	VolumesRemoved    int
	VolumesSpace      string
	NetworksRemoved   int
	BuildCacheSpace   string
	BuildCacheBytes   int64
	DryRun            bool
}
```

- [ ] **Step 4: Add the interface method to `Manager`**

Add this line to the `Manager` interface, right after the `KeepLastNImages` entry:

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

- [ ] **Step 5: Add the stub implementation**

Add to `internal/runtime/runtime.go`, after the stub `KeepLastNImages` method:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 6: Update the three test mocks so the package still compiles**

`internal/cli/root_test.go` — after the `KeepLastNImages` method of `mockRTForDeploy`:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{DryRun: opts.DryRun}, nil
}
```

`internal/idle/idle_test.go` — after the `KeepLastNImages` method of `mockRuntime`:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{DryRun: opts.DryRun}, nil
}
```

`internal/proxy/proxy_test.go` — after the `KeepLastNImages` method of `mockRuntime`:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 7: Run tests to verify everything passes**

Run: `go test ./... -v -count=1`
Expected: PASS — all tests, including the new `TestStubCleanup` and the interface-satisfaction tests (`TestStubSatisfiesInterface`, `TestMockRTForDeployImplementsManager`).

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Cleanup to runtime Manager interface"
```

---

### Task 2: Pure docker command builders and output parsers

**Files:**
- Modify: `internal/runtime/cleanup.go` (add helpers; add `"encoding/json"` to imports)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (these are standalone pure functions)
- Produces (all consumed by Task 3):
  - `containerPruneCmd(protectTengiz bool) []string`
  - `imagePruneCmd() []string`
  - `volumePruneCmd() []string`
  - `networkPruneCmd() []string`
  - `buildCachePruneCmd() []string`
  - `parsePruneOutput(out string) (int, string)`
  - `stoppedContainerNames(psOut string) []string`
  - `unusedImageRefs(imagesOut, containersOut string) []string`
  - `reclaimableBuildCacheSize(duOut string) int64`
  - `nonEmptyLines(out string) []string`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestContainerPruneCmd(t *testing.T) {
	if got, want := containerPruneCmd(true), []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}; !reflect.DeepEqual(got, want) {
		t.Errorf("containerPruneCmd(true) = %v, want %v", got, want)
	}
	if got, want := containerPruneCmd(false), []string{"container", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("containerPruneCmd(false) = %v, want %v", got, want)
	}
}

func TestImagePruneCmd(t *testing.T) {
	if got, want := imagePruneCmd(), []string{"image", "prune", "-a", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("imagePruneCmd() = %v, want %v", got, want)
	}
}

func TestVolumePruneCmd(t *testing.T) {
	if got, want := volumePruneCmd(), []string{"volume", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("volumePruneCmd() = %v, want %v", got, want)
	}
}

func TestNetworkPruneCmd(t *testing.T) {
	if got, want := networkPruneCmd(), []string{"network", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("networkPruneCmd() = %v, want %v", got, want)
	}
}

func TestBuildCachePruneCmd(t *testing.T) {
	if got, want := buildCachePruneCmd(), []string{"builder", "prune", "-f"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildCachePruneCmd() = %v, want %v", got, want)
	}
}

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name      string
		out       string
		wantCount int
		wantSpace string
	}{
		{
			name:      "containers",
			out:       "Deleted Containers:\nabc123\n\nTotal reclaimed space: 4.096kB\n",
			wantCount: 1,
			wantSpace: "4.096kB",
		},
		{
			name:      "images",
			out:       "Untagged: nginx:latest\nDeleted Images:\ndeleted: sha256:xyz\n\nTotal reclaimed space: 25.5MB\n",
			wantCount: 1,
			wantSpace: "25.5MB",
		},
		{
			name:      "networks has no reclaimed line",
			out:       "Deleted Networks:\nnet1\n",
			wantCount: 1,
			wantSpace: "",
		},
		{
			name:      "empty output",
			out:       "",
			wantCount: 0,
			wantSpace: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count, space := parsePruneOutput(tc.out)
			if count != tc.wantCount {
				t.Errorf("parsePruneOutput(%q) count = %d, want %d", tc.out, count, tc.wantCount)
			}
			if space != tc.wantSpace {
				t.Errorf("parsePruneOutput(%q) space = %q, want %q", tc.out, space, tc.wantSpace)
			}
		})
	}
}

func TestStoppedContainerNames(t *testing.T) {
	names := stoppedContainerNames("tengiz-myapp|Exited (0) 5 minutes ago\nfoo|Up 2 hours\nbar|Created\n")
	if !reflect.DeepEqual(names, []string{"tengiz-myapp", "bar"}) {
		t.Errorf("stoppedContainerNames() = %v, want [tengiz-myapp bar]", names)
	}
}

func TestUnusedImageRefs(t *testing.T) {
	images := "sha256:img1|nginx:latest\nsha256:img2|<none>\nsha256:img3|tengiz-apps/myapp:v1\nsha256:img4|alpine:3.19\n"
	containers := "nginx:latest\ntengiz-apps/myapp:v1\n"
	unused := unusedImageRefs(images, containers)
	if !reflect.DeepEqual(unused, []string{"sha256:img2", "alpine:3.19"}) {
		t.Errorf("unusedImageRefs() = %v, want [sha256:img2 alpine:3.19]", unused)
	}
}

func TestReclaimableBuildCacheSize(t *testing.T) {
	out := `{"ID":"a","Reclaimable":true,"Size":100}
{"ID":"b","Reclaimable":false,"Size":50}
{"ID":"c","Reclaimable":true,"Size":25}`
	if got := reclaimableBuildCacheSize(out); got != 125 {
		t.Errorf("reclaimableBuildCacheSize() = %d, want 125", got)
	}
}

func TestNonEmptyLines(t *testing.T) {
	if got, want := nonEmptyLines("  a \nb\n\n"), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("nonEmptyLines() = %v, want %v", got, want)
	}
}
```

Add `"reflect"` to the import block of `internal/runtime/cleanup_test.go`:

```go
import (
	"context"
	"reflect"
	"testing"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestContainerPruneCmd|TestImagePruneCmd|TestVolumePruneCmd|TestNetworkPruneCmd|TestBuildCachePruneCmd|TestParsePruneOutput|TestStoppedContainerNames|TestUnusedImageRefs|TestReclaimableBuildCacheSize|TestNonEmptyLines" -v -count=1`
Expected: FAIL — `undefined: containerPruneCmd` (and the other helpers).

- [ ] **Step 3: Write the helper implementations**

Add to `internal/runtime/cleanup.go`, after the `KeepLastNImages` method:

```go
func containerPruneCmd(protectTengiz bool) []string {
	args := []string{"container", "prune", "-f"}
	if protectTengiz {
		args = append(args, "--filter", "label!=tengiz-app")
	}
	return args
}

func imagePruneCmd() []string {
	return []string{"image", "prune", "-a", "-f"}
}

func volumePruneCmd() []string {
	return []string{"volume", "prune", "-f"}
}

func networkPruneCmd() []string {
	return []string{"network", "prune", "-f"}
}

func buildCachePruneCmd() []string {
	return []string{"builder", "prune", "-f"}
}

func nonEmptyLines(out string) []string {
	var lines []string
	for _, ln := range strings.Split(out, "\n") {
		if line := strings.TrimSpace(ln); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func parsePruneOutput(out string) (int, string) {
	count := 0
	space := ""
	for _, ln := range strings.Split(out, "\n") {
		line := strings.TrimSpace(ln)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			space = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			continue
		}
		// Skip section headers ("Deleted Containers:") and "Untagged: ..." lines.
		if strings.HasSuffix(line, ":") || strings.HasPrefix(line, "Untagged") {
			continue
		}
		count++
	}
	return count, space
}

func stoppedContainerNames(psOut string) []string {
	var names []string
	for _, ln := range strings.Split(psOut, "\n") {
		if ln == "" {
			continue
		}
		parts := strings.SplitN(ln, "|", 2)
		if len(parts) < 2 {
			continue
		}
		status := parts[1]
		if strings.HasPrefix(status, "Exited") || strings.HasPrefix(status, "Created") {
			names = append(names, parts[0])
		}
	}
	return names
}

func unusedImageRefs(imagesOut, containersOut string) []string {
	used := make(map[string]bool)
	for _, ln := range strings.Split(containersOut, "\n") {
		ref := strings.TrimSpace(ln)
		if ref == "" {
			continue
		}
		used[strings.TrimPrefix(ref, "sha256:")] = true
	}
	var unused []string
	for _, ln := range strings.Split(imagesOut, "\n") {
		line := strings.TrimSpace(ln)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		id := parts[0]
		tag := parts[1]
		if tag == "<none>" {
			unused = append(unused, id)
			continue
		}
		if used[id] || used[tag] {
			continue
		}
		unused = append(unused, tag)
	}
	return unused
}

func reclaimableBuildCacheSize(duOut string) int64 {
	var total int64
	for _, ln := range strings.Split(duOut, "\n") {
		line := strings.TrimSpace(ln)
		if line == "" {
			continue
		}
		var rec struct {
			Reclaimable bool  `json:"Reclaimable"`
			Size        int64 `json:"Size"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.Reclaimable {
			total += rec.Size
		}
	}
	return total
}
```

Add `"encoding/json"` to the import block of `internal/runtime/cleanup.go` (current imports: `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestContainerPruneCmd|TestImagePruneCmd|TestVolumePruneCmd|TestNetworkPruneCmd|TestBuildCachePruneCmd|TestParsePruneOutput|TestStoppedContainerNames|TestUnusedImageRefs|TestReclaimableBuildCacheSize|TestNonEmptyLines" -v -count=1`
Expected: PASS for all.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add docker prune command builders and output parsers"
```

---

### Task 3: `dockerRuntime.Cleanup` exec implementation

**Files:**
- Modify: `internal/runtime/cleanup.go` (add `execPrune` helper and `Cleanup` method)
- Test: `internal/runtime/cleanup_test.go` (fake-docker integration tests)

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult` (Task 1); `containerPruneCmd`, `imagePruneCmd`, `volumePruneCmd`, `networkPruneCmd`, `buildCachePruneCmd`, `parsePruneOutput`, `stoppedContainerNames`, `unusedImageRefs`, `reclaimableBuildCacheSize`, `nonEmptyLines` (Task 2)
- Produces: `(r *dockerRuntime).Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — the concrete implementation behind `Manager.Cleanup`

- [ ] **Step 1: Write the failing fake-docker tests**

Add to `internal/runtime/cleanup_test.go`:

```go
const fakeDockerScript = `#!/bin/sh
echo "$*" >> "$TENGIZ_FAKE_DOCKER_LOG"
case "$1 $2" in
"container prune")
	echo "Deleted Containers:"
	echo "abc123"
	echo ""
	echo "Total reclaimed space: 4.096kB"
	;;
"image prune")
	echo "Untagged: nginx:latest"
	echo "Deleted Images:"
	echo "deleted: sha256:xyz"
	echo ""
	echo "Total reclaimed space: 25.5MB"
	;;
"volume prune")
	echo "Deleted Volumes:"
	echo "vol1"
	echo ""
	echo "Total reclaimed space: 0B"
	;;
"network prune")
	echo "Deleted Networks:"
	echo "net1"
	;;
"builder prune")
	echo "Deleted build cache objects:"
	echo "obj1"
	echo ""
	echo "Total reclaimed space: 42.3MB"
	;;
"ps -a")
	case "$*" in
	*--no-trunc*)
		echo "nginx:latest"
		echo "tengiz-apps/myapp:v1"
		;;
	*"label!=tengiz-app"*)
		echo "worker123|Exited (1) 2 hours ago"
		;;
	*)
		echo "tengiz-myapp|Exited (0) 5 minutes ago"
		echo "worker123|Exited (1) 2 hours ago"
		;;
	esac
	;;
"images --no-trunc")
	echo "sha256:img1|nginx:latest"
	echo "sha256:img2|<none>"
	;;
"volume ls")
	echo "vol1"
	;;
"network ls")
	echo "net1"
	;;
"builder du")
	echo '{"ID":"obj1","Reclaimable":true,"Size":44323328}'
	;;
esac
`

func newFakeDocker(t *testing.T, script string) (Manager, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "docker")
	logPath := filepath.Join(dir, "docker.log")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("TENGIZ_FAKE_DOCKER_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	rt, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() with fake docker: %v", err)
	}
	return rt, logPath
}

func TestDockerRuntimeCleanupPrunesResources(t *testing.T) {
	rt, logPath := newFakeDocker(t, fakeDockerScript)
	result, err := rt.Cleanup(context.Background(), CleanupOptions{
		Containers:              true,
		Images:                  true,
		Volumes:                 true,
		Networks:                true,
		BuildCache:              true,
		ProtectTengizContainers: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.DryRun {
		t.Error("DryRun = true, want false")
	}
	if result.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", result.ContainersRemoved)
	}
	if result.ContainersSpace != "4.096kB" {
		t.Errorf("ContainersSpace = %q, want 4.096kB", result.ContainersSpace)
	}
	if result.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1", result.ImagesRemoved)
	}
	if result.ImagesSpace != "25.5MB" {
		t.Errorf("ImagesSpace = %q, want 25.5MB", result.ImagesSpace)
	}
	if result.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", result.VolumesRemoved)
	}
	if result.VolumesSpace != "0B" {
		t.Errorf("VolumesSpace = %q, want 0B", result.VolumesSpace)
	}
	if result.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", result.NetworksRemoved)
	}
	if result.BuildCacheSpace != "42.3MB" {
		t.Errorf("BuildCacheSpace = %q, want 42.3MB", result.BuildCacheSpace)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	logStr := string(logData)
	for _, want := range []string{
		"container prune -f --filter label!=tengiz-app",
		"image prune -a -f",
		"volume prune -f",
		"network prune -f",
		"builder prune -f",
	} {
		if !strings.Contains(logStr, want) {
			t.Errorf("docker log missing %q; got:\n%s", want, logStr)
		}
	}
}

func TestDockerRuntimeCleanupDryRun(t *testing.T) {
	rt, _ := newFakeDocker(t, fakeDockerScript)
	result, err := rt.Cleanup(context.Background(), CleanupOptions{
		Containers:              true,
		Images:                  true,
		Volumes:                 true,
		Networks:                true,
		BuildCache:              true,
		ProtectTengizContainers: true,
		DryRun:                  true,
	})
	if err != nil {
		t.Fatalf("Cleanup() dry-run error = %v", err)
	}
	if !result.DryRun {
		t.Error("DryRun = false, want true")
	}
	if result.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1 (tengiz-managed containers excluded)", result.ContainersRemoved)
	}
	if result.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1 (nginx:latest used, <none> dangling)", result.ImagesRemoved)
	}
	if result.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", result.VolumesRemoved)
	}
	if result.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", result.NetworksRemoved)
	}
	if result.BuildCacheBytes != 44323328 {
		t.Errorf("BuildCacheBytes = %d, want 44323328", result.BuildCacheBytes)
	}
	if result.ContainersSpace != "" || result.ImagesSpace != "" {
		t.Error("dry-run must not report reclaimed space (nothing was removed)")
	}
}
```

Update the import block of `internal/runtime/cleanup_test.go` to:

```go
import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestDockerRuntimeCleanup" -v -count=1`
Expected: FAIL — `invalid memory address or nil pointer dereference` or `m.Cleanup undefined` style compile error because `dockerRuntime` has no `Cleanup` method yet (the stub satisfies the interface, so the compile may succeed and the test fails at runtime with a nil/invalid call — either way the test must fail before the implementation exists).

- [ ] **Step 3: Write the `execPrune` helper and `Cleanup` method**

Add to `internal/runtime/cleanup.go` (after the Task 2 helpers):

```go
func (r *dockerRuntime) execPrune(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	result := CleanupResult{DryRun: opts.DryRun}

	if opts.Containers {
		if opts.DryRun {
			args := []string{"ps", "-a", "--format", "{{.Names}}|{{.Status}}"}
			if opts.ProtectTengizContainers {
				args = append(args, "--filter", "label!=tengiz-app")
			}
			out, err := r.execPrune(ctx, args)
			if err != nil {
				return result, err
			}
			result.ContainersRemoved = len(stoppedContainerNames(out))
		} else {
			out, err := r.execPrune(ctx, containerPruneCmd(opts.ProtectTengizContainers))
			if err != nil {
				return result, err
			}
			result.ContainersRemoved, result.ContainersSpace = parsePruneOutput(out)
		}
	}

	if opts.Images {
		if opts.DryRun {
			imgs, err := r.execPrune(ctx, []string{"images", "--no-trunc", "--format", "{{.ID}}|{{.Repository}}:{{.Tag}}"})
			if err != nil {
				return result, err
			}
			ps, err := r.execPrune(ctx, []string{"ps", "-a", "--no-trunc", "--format", "{{.Image}}"})
			if err != nil {
				return result, err
			}
			result.ImagesRemoved = len(unusedImageRefs(imgs, ps))
		} else {
			out, err := r.execPrune(ctx, imagePruneCmd())
			if err != nil {
				return result, err
			}
			result.ImagesRemoved, result.ImagesSpace = parsePruneOutput(out)
		}
	}

	if opts.Volumes {
		if opts.DryRun {
			out, err := r.execPrune(ctx, []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"})
			if err != nil {
				return result, err
			}
			result.VolumesRemoved = len(nonEmptyLines(out))
		} else {
			out, err := r.execPrune(ctx, volumePruneCmd())
			if err != nil {
				return result, err
			}
			result.VolumesRemoved, result.VolumesSpace = parsePruneOutput(out)
		}
	}

	if opts.Networks {
		if opts.DryRun {
			out, err := r.execPrune(ctx, []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"})
			if err != nil {
				return result, err
			}
			result.NetworksRemoved = len(nonEmptyLines(out))
		} else {
			out, err := r.execPrune(ctx, networkPruneCmd())
			if err != nil {
				return result, err
			}
			result.NetworksRemoved, _ = parsePruneOutput(out)
		}
	}

	if opts.BuildCache {
		if opts.DryRun {
			out, err := r.execPrune(ctx, []string{"builder", "du", "--format", "{{json .}}"})
			if err != nil {
				return result, err
			}
			result.BuildCacheBytes = reclaimableBuildCacheSize(out)
		} else {
			out, err := r.execPrune(ctx, buildCachePruneCmd())
			if err != nil {
				return result, err
			}
			_, result.BuildCacheSpace = parsePruneOutput(out)
		}
	}

	return result, nil
}
```

(`exec` and `fmt` are already imported in `internal/runtime/cleanup.go`; `strings` is already imported too.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS — all runtime tests including `TestDockerRuntimeCleanupPrunesResources` and `TestDockerRuntimeCleanupDryRun`.

Run: `go test ./... -v -count=1`
Expected: PASS — full suite (the `Cleanup` interface method is already stubbed everywhere from Task 1).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement dockerRuntime.Cleanup via docker prune"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` (command definition, registration in `init()`, flag registration, `cleanupOptionsFromFlags`, `printCleanupResult`, `spaceSuffix`)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.Manager.Cleanup` (Tasks 1-3)
- Produces: `cleanupCmd` (`*cobra.Command`), `addCleanupFlags(cmd *cobra.Command)`, `cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error)`, `printCleanupResult(result runtime.CleanupResult)`, `spaceSuffix(space string) string`

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

func TestCleanupCmdFlagsAndDefaults(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"dry-run", "prune-stopped-tengiz", "all", "containers", "images", "volumes", "networks", "build-cache"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanupCmd missing flag --%s", name)
		}
	}
	if c, _ := flags.GetBool("containers"); !c {
		t.Error("--containers should default to true")
	}
	if i, _ := flags.GetBool("images"); !i {
		t.Error("--images should default to true")
	}
	if v, _ := flags.GetBool("volumes"); v {
		t.Error("--volumes should default to false")
	}
	if n, _ := flags.GetBool("networks"); n {
		t.Error("--networks should default to false")
	}
	if b, _ := flags.GetBool("build-cache"); b {
		t.Error("--build-cache should default to false")
	}
}

func TestCleanupOptionsFromFlags(t *testing.T) {
	cmd := &cobra.Command{}
	addCleanupFlags(cmd)
	if err := cmd.ParseFlags([]string{"--all", "--dry-run", "--prune-stopped-tengiz"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags: %v", err)
	}
	if !opts.DryRun {
		t.Error("DryRun = false, want true")
	}
	if !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Error("--all should enable volumes, networks, and build cache")
	}
	if opts.ProtectTengizContainers {
		t.Error("ProtectTengizContainers = true, want false with --prune-stopped-tengiz")
	}
}

func TestCleanupOptionsFromFlagsDefaults(t *testing.T) {
	cmd := &cobra.Command{}
	addCleanupFlags(cmd)
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags: %v", err)
	}
	if !opts.Containers || !opts.Images {
		t.Error("containers/images should default to true")
	}
	if opts.Volumes || opts.Networks || opts.BuildCache {
		t.Error("volumes/networks/build-cache should default to false")
	}
	if !opts.ProtectTengizContainers {
		t.Error("ProtectTengizContainers should default to true")
	}
	if opts.DryRun {
		t.Error("DryRun should default to false")
	}
}

func TestCleanupCmdInvokesOptions(t *testing.T) {
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	var captured runtime.CleanupOptions
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		captured = opts
		return nil
	}
	rootCmd.SetArgs([]string{"cleanup", "--all", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !captured.DryRun || !captured.Volumes || !captured.Networks || !captured.BuildCache {
		t.Errorf("captured options = %+v, want dry-run + all categories", captured)
	}
}

func TestCleanupPrintDryRun(t *testing.T) {
	out := captureOutput(func() {
		printCleanupResult(runtime.CleanupResult{DryRun: true, ContainersRemoved: 3, ImagesRemoved: 2})
	})
	if !strings.Contains(out, "Dry run") {
		t.Errorf("dry-run output missing 'Dry run', got: %s", out)
	}
	if !strings.Contains(out, "containers removed: 3") {
		t.Errorf("output missing containers count, got: %s", out)
	}
}

func TestCleanupPrintResult(t *testing.T) {
	out := captureOutput(func() {
		printCleanupResult(runtime.CleanupResult{
			ContainersRemoved: 1, ContainersSpace: "4.096kB",
			ImagesRemoved: 2, ImagesSpace: "25.5MB",
			VolumesRemoved: 1, VolumesSpace: "0B",
			NetworksRemoved: 1,
			BuildCacheSpace: "42.3MB",
		})
	})
	for _, want := range []string{"Cleanup complete", "containers removed: 1 (4.096kB reclaimed)", "images removed: 2 (25.5MB reclaimed)", "volumes removed: 1 (0B reclaimed)", "networks removed: 1", "build cache: 42.3MB reclaimed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`
Expected: FAIL — `undefined: cleanupCmd`, `undefined: addCleanupFlags`, `undefined: cleanupOptionsFromFlags`, `undefined: printCleanupResult`.

- [ ] **Step 3: Add the command definition**

In `internal/cli/root.go`, insert `cleanupCmd` right before `var buildLogsCmd` (after the `rollbackCmd` block ends at line ~1017):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long:  "Prunes stopped containers, unused images, volumes, networks, and build cache. Tengiz-managed containers (label tengiz-app) are protected by default so cold starts and rollbacks keep working.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}
		printCleanupResult(result)
		return nil
	},
}
```

- [ ] **Step 4: Register the command and flags**

In `internal/cli/root.go` `init()`, after `rootCmd.AddCommand(rollbackCmd)`:

```go
	rootCmd.AddCommand(cleanupCmd)
	addCleanupFlags(cleanupCmd)
```

Add the flag helper and option-mapping/printing helpers at the bottom of `internal/cli/root.go`, near the other helpers (e.g. after `addSecretProviderFlags`):

```go
func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cmd.Flags().Bool("prune-stopped-tengiz", false, "also remove stopped Tengiz-managed containers")
	cmd.Flags().Bool("all", false, "enable all cleanup categories (volumes, networks, build cache)")
	cmd.Flags().Bool("containers", true, "prune stopped containers")
	cmd.Flags().Bool("images", true, "prune unused images")
	cmd.Flags().Bool("volumes", false, "prune unused volumes")
	cmd.Flags().Bool("networks", false, "prune unused networks")
	cmd.Flags().Bool("build-cache", false, "prune BuildKit build cache")
}

func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	var opts runtime.CleanupOptions
	var err error
	if opts.DryRun, err = cmd.Flags().GetBool("dry-run"); err != nil {
		return opts, err
	}
	if opts.Containers, err = cmd.Flags().GetBool("containers"); err != nil {
		return opts, err
	}
	if opts.Images, err = cmd.Flags().GetBool("images"); err != nil {
		return opts, err
	}
	if opts.Volumes, err = cmd.Flags().GetBool("volumes"); err != nil {
		return opts, err
	}
	if opts.Networks, err = cmd.Flags().GetBool("networks"); err != nil {
		return opts, err
	}
	if opts.BuildCache, err = cmd.Flags().GetBool("build-cache"); err != nil {
		return opts, err
	}
	pruneStopped, err := cmd.Flags().GetBool("prune-stopped-tengiz")
	if err != nil {
		return opts, err
	}
	opts.ProtectTengizContainers = !pruneStopped
	all, err := cmd.Flags().GetBool("all")
	if err != nil {
		return opts, err
	}
	if all {
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts, nil
}

func printCleanupResult(result runtime.CleanupResult) {
	if result.DryRun {
		fmt.Println("[tengiz] Dry run — nothing was removed. Run without --dry-run to prune.")
	} else {
		fmt.Println("[tengiz] Cleanup complete.")
	}
	fmt.Printf("  containers removed: %d%s\n", result.ContainersRemoved, spaceSuffix(result.ContainersSpace))
	fmt.Printf("  images removed:     %d%s\n", result.ImagesRemoved, spaceSuffix(result.ImagesSpace))
	fmt.Printf("  volumes removed:    %d%s\n", result.VolumesRemoved, spaceSuffix(result.VolumesSpace))
	fmt.Printf("  networks removed:   %d\n", result.NetworksRemoved)
	if result.BuildCacheSpace != "" {
		fmt.Printf("  build cache:        %s reclaimed\n", result.BuildCacheSpace)
	} else if result.BuildCacheBytes > 0 {
		fmt.Printf("  build cache:        %d bytes reclaimable\n", result.BuildCacheBytes)
	}
}

func spaceSuffix(space string) string {
	if space == "" {
		return ""
	}
	return fmt.Sprintf(" (%s reclaimed)", space)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`
Expected: PASS for all cleanup tests.

Run: `go test ./... -v -count=1`
Expected: PASS — full suite.

Run: `go vet ./...` and `go build -o tengiz .`
Expected: `go vet` clean; build succeeds. Smoke-check the help output:

Run: `./tengiz cleanup --help`
Expected: shows the command summary and all 8 flags with the documented defaults.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 5: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the final CLI surface from Task 4 (`tengiz cleanup [flags]`)
- Produces: user-facing documentation

- [ ] **Step 1: Add the `tengiz cleanup` section to README.md**

Insert after the `### tengiz rollback <app>` section (ends at line 236, before `### tengiz domain`):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space. By default it prunes stopped containers (excluding Tengiz-managed ones) and unused images. Volumes, networks, and build cache are opt-in because deleting them can remove data.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--containers` | Prune stopped containers (default: `true`) |
| `--images` | Prune unused images (default: `true`) |
| `--volumes` | Prune unused volumes (default: `false`) |
| `--networks` | Prune unused networks (default: `false`) |
| `--build-cache` | Prune BuildKit build cache (default: `false`) |
| `--all` | Enable all categories (volumes, networks, build cache) |
| `--prune-stopped-tengiz` | Also remove stopped Tengiz-managed containers (default: `false`) |

Examples:
```
tengiz cleanup                    # prune stopped containers + unused images
tengiz cleanup --dry-run          # preview what would be removed
tengiz cleanup --all              # full housekeeping (volumes, networks, build cache too)
```

Stopped containers labeled `tengiz-app` are protected by default so scale-to-zero cold starts and rollbacks keep working. Images referenced by any container (running or stopped) are never removed.
```

- [ ] **Step 2: Add `tengiz cleanup` to the README Commands quick-reference table**

In the `### Commands` table (around line 575), add:

```markdown
| `tengiz cleanup [flags]` | Prune unused Docker resources (containers, images, volumes, networks, build cache) |
```

- [ ] **Step 3: Mark the features implemented in `docs/FUTURES_FEATURES.md`**

Flip the status markers in the Priority Ranking P0 and P1 tables:

- Line 19: `| 6 | **Docker Housekeeping** ⬜ |` → `| 6 | **Docker Housekeeping** ✅ |`
- Line 74: `| 56 | **Granular Docker Prune Operations** ⬜ |` → `| 56 | **Granular Docker Prune Operations** ✅ |`

Add two rows to the `### ✅ Implemented Features (Not Pending)` table (after the existing rows):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-07-31) |
| — | **Granular Docker Prune Operations** | Orta | Düşük | Mükemmel | ✅ Implemented (2026-07-31) |
```

- [ ] **Step 4: Verify the build and full test suite**

Run: `go build -o tengiz . && go test ./... -v -count=1 && go vet ./...`
Expected: build succeeds, all tests pass, `go vet` clean.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage** (from `docs/FUTURES_FEATURES.md` #6 "Docker Housekeeping"):
- `tengiz cleanup` command → Task 4
- Label-based filtering protecting Tengiz-managed containers → Task 3 (`--filter label!=tengiz-app`), default-on via `ProtectTengizContainers` → Task 4
- Prune unused volumes, networks, containers, images → Tasks 3 (containers/images default, volumes/networks opt-in)
- Build cache pruning (`CleanupHelperContainersJob` analog is out of scope) → `--build-cache` covers BuildKit cache; helper-container cleanup is a separate feature (#57 background scheduler), deliberately not included
- Disk space as the motivating problem → dry-run reports reclaimed space and reclaimable bytes

**2. Placeholder scan:** every step contains complete code, exact commands, and expected output. No "TBD"/"add error handling"/"similar to Task N" patterns.

**3. Type consistency:**
- `CleanupOptions` field names used in Task 4 (`Containers`, `Images`, `Volumes`, `Networks`, `BuildCache`, `ProtectTengizContainers`, `DryRun`) match the definition in Task 1
- `CleanupResult` fields used in `printCleanupResult` (Task 4) and asserted in tests (Task 3) match the Task 1 definition (`ContainersRemoved`, `ContainersSpace`, `ImagesRemoved`, `ImagesSpace`, `VolumesRemoved`, `VolumesSpace`, `NetworksRemoved`, `BuildCacheSpace`, `BuildCacheBytes`, `DryRun`)
- Helper names/signatures used in Task 3 (`containerPruneCmd(bool)`, `parsePruneOutput(string) (int, string)`, `stoppedContainerNames(string) []string`, `unusedImageRefs(string, string) []string`, `reclaimableBuildCacheSize(string) int64`, `nonEmptyLines(string) []string`, `imagePruneCmd()`, `volumePruneCmd()`, `networkPruneCmd()`, `buildCachePruneCmd()`) match the Task 2 definitions
- The `Manager` interface method added in Task 1 is implemented by `stubManager` (Task 1) and `dockerRuntime` (Task 3); all test mocks updated in Task 1

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-31-docker-housekeeping.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
