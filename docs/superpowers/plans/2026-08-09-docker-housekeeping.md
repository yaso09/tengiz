# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes stopped non-Tengiz containers, dangling/unused images, unused volumes, and unused networks while always protecting Tengiz-managed containers via the `tengiz-app` label.

**Architecture:** A new `Cleanup(ctx, opts)` method on the `runtime.Manager` interface. The `dockerRuntime` implementation discovers candidates with read-only `docker` listing commands, filters out anything carrying the `tengiz-app` label (or the `tengiz-apps/*` image repository), and removes candidates one-by-one so `--dry-run` is a pure "list, don't remove" pass. The CLI adds a `cleanup` subcommand with `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks` flags that default to "all categories". Pure parsing/filter helpers are unit-tested with sample Docker output; the stub manager satisfies the interface.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` interface (`os/exec`-based `docker` calls), existing `internal/runtime/cleanup.go` file where `RemoveImage`/`KeepLastNImages` already live.

## Global Constraints

- Container protection: never remove any container whose labels contain `tengiz-app=` (running OR stopped — stopped Tengiz containers are cold-start candidates and must survive)
- Image protection: never remove images whose repository starts with `tengiz-apps/` (rollback images); never remove images referenced by any container
- Container candidate rule: status starts with `Exited`, equals `Created`, or equals `Dead` (skip `Up`/`Restarting`/`Paused`)
- Network protection: never touch `bridge`, `host`, or `none` networks; only remove networks with zero attached containers
- Volumes: only remove volumes reported as dangling (`docker volume ls -f dangling=true`)
- `--dry-run` must make zero mutation calls; it only lists candidates and prints them
- `runtime.NewDocker()` already guards on `docker` binary presence — CLI must propagate that error, not panic
- No new external dependencies (no Docker SDK; keep `os/exec`)
- All four existing `Manager` implementations must gain the new method in one commit: `stubManager` (runtime.go), `mockRTForDeploy` (root_test.go), `mockRuntime` (idle_test.go), `mockRuntime` (proxy_test.go) — otherwise the build breaks
- All tests must pass: `go test ./... -v -count=1`; static analysis: `go vet ./...`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `CleanupOptions`, `CleanupResult`, docker `Cleanup` orchestration + per-category exec methods, pure parse/filter helpers |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + stub method |
| `internal/runtime/cleanup_test.go` | Tests for pure helpers + stub `Cleanup` |
| `internal/cli/root.go` | New `cleanupCmd` cobra command + flag registration + `init()` wiring |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy`; command registration test |
| `internal/idle/idle_test.go` | Add `Cleanup` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` line to the CLI command list |

---

### Task 1: Add Cleanup types, Manager interface method, stub, and update all mocks

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface), `internal/runtime/runtime.go:113-122` (stub)
- Modify: `internal/cli/root_test.go:69-100` (mockRTForDeploy)
- Modify: `internal/idle/idle_test.go:14-34` (mockRuntime)
- Modify: `internal/proxy/proxy_test.go:15-35` (mockRuntime)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `runtime.CleanupOptions{DryRun, Containers, Images, Volumes, Networks bool}`, `runtime.CleanupResult{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved []string}`, interface method `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true, Images: true, Volumes: true, Networks: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	empty := CleanupResult{}
	if !reflect.DeepEqual(res, empty) {
		t.Fatalf("stub Cleanup should return empty result, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -count=1`
Expected: compile error — `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add types + interface method + stub + mock methods**

Add to `internal/runtime/cleanup.go` (top of file, after imports):

```go
type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

type CleanupResult struct {
	ContainersRemoved []string
	ImagesRemoved     []string
	VolumesRemoved    []string
	NetworksRemoved   []string
}
```

Add to `internal/runtime/runtime.go` interface (after the `KeepLastNImages` line):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add to `internal/runtime/runtime.go` stub (after `KeepLastNImages` stub):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

Add to `internal/cli/root_test.go` mock (after the `KeepLastNImages` line, before `Run`):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

Add to `internal/idle/idle_test.go` mock (after the `KeepLastNImages` line):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

Add to `internal/proxy/proxy_test.go` mock (after the `KeepLastNImages` line):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubCleanup -count=1`
Expected: PASS. Then run `go build ./...` to confirm all mocks compile.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add Cleanup method to Manager interface"
```

---

### Task 2: Container cleanup — pure helpers + exec

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, existing `labelKey` const (`"tengiz-app"`) from `internal/runtime/docker.go`
- Produces: `parseContainerList(output string) []containerInfo`, `stoppedForeignContainers(list []containerInfo) []containerInfo`, method `cleanupContainers(ctx context.Context, opts CleanupOptions) ([]string, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
func TestParseContainerList(t *testing.T) {
	output := "abc123|web-app|Exited (0) 2 days ago|tengiz-app=myapp,tengiz-env=production\n" +
		"def456|helper|Created|\n" +
		"ghi789|runner|Up 10 seconds|tengiz-app=other\n" +
		"jkl012|stale|Exited (137) Less than a second ago|"
	list := parseContainerList(output)
	if len(list) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(list), list)
	}
	if list[0].Name != "web-app" {
		t.Errorf("entry[0].Name = %q, want %q", list[0].Name, "web-app")
	}
	if list[0].Labels != "tengiz-app=myapp,tengiz-env=production" {
		t.Errorf("entry[0].Labels = %q", list[0].Labels)
	}
}

func TestStoppedForeignContainers(t *testing.T) {
	list := []containerInfo{
		{ID: "a", Name: "web", Status: "Exited (0) 1 hour ago", Labels: "tengiz-app=myapp"},
		{ID: "b", Name: "stale", Status: "Exited (137) 2 hours ago", Labels: ""},
		{ID: "c", Name: "created", Status: "Created", Labels: ""},
		{ID: "d", Name: "running", Status: "Up 1 hour", Labels: ""},
		{ID: "e", Name: "dead", Status: "Dead", Labels: ""},
		{ID: "f", Name: "restarting", Status: "Restarting (1) 5 seconds ago", Labels: ""},
	}
	got := stoppedForeignContainers(list)
	want := []string{"stale", "created", "dead"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i, c := range got {
		if c.Name != want[i] {
			t.Errorf("got[%d].Name = %q, want %q", i, c.Name, want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParseContainerList|TestStoppedForeignContainers' -count=1`
Expected: FAIL — `undefined: parseContainerList` / `undefined: containerInfo`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/runtime/cleanup.go`:

```go
type containerInfo struct {
	ID     string
	Name   string
	Status string
	Labels string
}

func parseContainerList(output string) []containerInfo {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var list []containerInfo
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		list = append(list, containerInfo{
			ID:     parts[0],
			Name:   parts[1],
			Status: parts[2],
			Labels: parts[3],
		})
	}
	return list
}

func stoppedForeignContainers(list []containerInfo) []containerInfo {
	var out []containerInfo
	for _, c := range list {
		if strings.Contains(c.Labels, labelKey+"=") {
			continue
		}
		if !isStoppedStatus(c.Status) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func isStoppedStatus(status string) bool {
	return strings.HasPrefix(status, "Exited") ||
		status == "Created" ||
		status == "Dead"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestParseContainerList|TestStoppedForeignContainers' -count=1`
Expected: PASS

- [ ] **Step 5: Implement the exec method**

Add to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) cleanupContainers(ctx context.Context, opts CleanupOptions) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--format", "{{.ID}}|{{.Names}}|{{.Status}}|{{.Labels}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	var removed []string
	for _, c := range stoppedForeignContainers(parseContainerList(string(out))) {
		removed = append(removed, c.Name)
		if opts.DryRun {
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "rm", "-f", c.ID)
		if rerr, rerrOut := rm.CombinedOutput(); rerr != nil {
			log.Printf("[runtime] cleanup: remove container %s: %v\n%s", c.Name, rerr, string(rerrOut))
		}
	}
	return removed, nil
}
```

- [ ] **Step 6: Run all runtime tests**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add label-protected stopped container cleanup"
```

---

### Task 3: Image cleanup — pure helpers + exec

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, existing `RemoveImage` method
- Produces: `parseImageList(output string) []imageInfo`, `unusedForeignImages(all []imageInfo, inUse []string) []imageInfo`, method `cleanupImages(ctx context.Context, opts CleanupOptions) ([]string, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
func TestParseImageList(t *testing.T) {
	output := "abc123|tengiz-apps/myapp:production-latest\n" +
		"def456|nginx:latest\n" +
		"ghi789|<none>:<none>\n" +
		"jkl012|redis:7"
	list := parseImageList(output)
	if len(list) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(list), list)
	}
	if list[0].ID != "abc123" || list[0].Ref != "tengiz-apps/myapp:production-latest" {
		t.Errorf("entry[0] = %+v", list[0])
	}
}

func TestUnusedForeignImages(t *testing.T) {
	all := []imageInfo{
		{ID: "a", Ref: "tengiz-apps/myapp:production-latest"}, // protected repo
		{ID: "b", Ref: "nginx:latest"},                        // used
		{ID: "c", Ref: "redis:7"},                             // unused foreign
		{ID: "d", Ref: "<none>:<none>"},                       // dangling, unused
	}
	got := unusedForeignImages(all, []string{"nginx:latest", "someid"})
	want := []string{"c", "d"}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range got {
		if id != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, id, want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParseImageList|TestUnusedForeignImages' -count=1`
Expected: FAIL — `undefined: parseImageList` / `undefined: imageInfo`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/runtime/cleanup.go`:

```go
type imageInfo struct {
	ID  string
	Ref string
}

func parseImageList(output string) []imageInfo {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var list []imageInfo
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		list = append(list, imageInfo{ID: parts[0], Ref: parts[1]})
	}
	return list
}

func unusedForeignImages(all []imageInfo, inUse []string) []imageInfo {
	used := make(map[string]bool, len(inUse))
	for _, ref := range inUse {
		used[ref] = true
	}
	var out []imageInfo
	for _, img := range all {
		if strings.HasPrefix(img.Ref, "tengiz-apps/") {
			continue
		}
		if used[img.Ref] || used[img.ID] {
			continue
		}
		out = append(out, img)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestParseImageList|TestUnusedForeignImages' -count=1`
Expected: PASS

- [ ] **Step 5: Implement the exec method**

Add to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) cleanupImages(ctx context.Context, opts CleanupOptions) ([]string, error) {
	allCmd := exec.CommandContext(ctx, "docker", "images",
		"--format", "{{.ID}}|{{.Repository}}:{{.Tag}}")
	allOut, err := allCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(allOut))
	}

	psCmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--format", "{{.Image}}")
	psOut, err := psCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps (images): %w\n%s", err, string(psOut))
	}
	var inUse []string
	for _, line := range strings.Split(strings.TrimSpace(string(psOut)), "\n") {
		if line != "" {
			inUse = append(inUse, line)
		}
	}

	var removed []string
	for _, img := range unusedForeignImages(parseImageList(string(allOut)), inUse) {
		removed = append(removed, img.Ref)
		if opts.DryRun {
			continue
		}
		if err := r.RemoveImage(ctx, img.ID); err != nil {
			log.Printf("[runtime] cleanup: remove image %s: %v", img.ID, err)
		}
	}
	return removed, nil
}
```

- [ ] **Step 6: Run all runtime tests**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add unused image cleanup protecting tengiz-apps images"
```

---

### Task 4: Volume + Network cleanup — pure helpers + exec

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`
- Produces: `parseNameList(output string) []string`, `parseNetworkList(output string) []networkInfo`, `foreignUnusedNetworks(all []networkInfo, inUse []string) []networkInfo`, methods `cleanupVolumes(ctx, opts) ([]string, error)` and `cleanupNetworks(ctx, opts) ([]string, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
func TestParseNameList(t *testing.T) {
	got := parseNameList("vol-a\nvol-b\n\nvol-c")
	want := []string{"vol-a", "vol-b", "vol-c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseNetworkList(t *testing.T) {
	output := "1|bridge|bridge\n2|ffnet|bridge\n3|host|host\n4|none|null"
	got := parseNetworkList(output)
	if len(got) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(got), got)
	}
	if got[1].Name != "ffnet" || got[1].Driver != "bridge" {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestForeignUnusedNetworks(t *testing.T) {
	all := []networkInfo{
		{ID: "1", Name: "bridge"},   // protected default
		{ID: "2", Name: "host"},     // protected default
		{ID: "3", Name: "none"},     // protected default
		{ID: "4", Name: "ffnet"},    // unused
		{ID: "5", Name: "inuse-net"}, // in use
	}
	got := foreignUnusedNetworks(all, []string{"inuse-net"})
	want := []string{"ffnet"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, name := range got {
		if name != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, name, want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParseNameList|TestParseNetworkList|TestForeignUnusedNetworks' -count=1`
Expected: FAIL — undefined functions/types

- [ ] **Step 3: Write minimal implementation**

Add to `internal/runtime/cleanup.go`:

```go
func parseNameList(output string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

type networkInfo struct {
	ID     string
	Name   string
	Driver string
}

func parseNetworkList(output string) []networkInfo {
	var out []networkInfo
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		out = append(out, networkInfo{ID: parts[0], Name: parts[1], Driver: parts[2]})
	}
	return out
}

var protectedNetworks = map[string]bool{"bridge": true, "host": true, "none": true}

func foreignUnusedNetworks(all []networkInfo, inUse []string) []networkInfo {
	used := make(map[string]bool, len(inUse))
	for _, n := range inUse {
		used[n] = true
	}
	var out []networkInfo
	for _, n := range all {
		if protectedNetworks[n.Name] || used[n.Name] {
			continue
		}
		out = append(out, n)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestParseNameList|TestParseNetworkList|TestForeignUnusedNetworks' -count=1`
Expected: PASS

- [ ] **Step 5: Implement the exec methods**

Add to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) cleanupVolumes(ctx context.Context, opts CleanupOptions) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls",
		"-f", "dangling=true", "--format", "{{.Name}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	var removed []string
	for _, name := range parseNameList(string(out)) {
		removed = append(removed, name)
		if opts.DryRun {
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "volume", "rm", "-f", name)
		if rerr, rerrOut := rm.CombinedOutput(); rerr != nil {
			log.Printf("[runtime] cleanup: remove volume %s: %v\n%s", name, rerr, string(rerrOut))
		}
	}
	return removed, nil
}

func (r *dockerRuntime) cleanupNetworks(ctx context.Context, opts CleanupOptions) ([]string, error) {
	ls := exec.CommandContext(ctx, "docker", "network", "ls",
		"--format", "{{.ID}}|{{.Name}}|{{.Driver}}")
	out, err := ls.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	all := parseNetworkList(string(out))

	var inUse []string
	for _, n := range all {
		insp := exec.CommandContext(ctx, "docker", "network", "inspect",
			"--format", "{{.Name}}", n.ID)
		_ = insp
		cnt := exec.CommandContext(ctx, "docker", "network", "inspect",
			"--format", "{{len .Containers}}", n.ID)
		cntOut, cntErr := cnt.CombinedOutput()
		if cntErr == nil && strings.TrimSpace(string(cntOut)) != "0" {
			inUse = append(inUse, n.Name)
		}
	}

	var removed []string
	for _, n := range foreignUnusedNetworks(all, inUse) {
		removed = append(removed, n.Name)
		if opts.DryRun {
			continue
		}
		rm := exec.CommandContext(ctx, "docker", "network", "rm", n.ID)
		if rerr, rerrOut := rm.CombinedOutput(); rerr != nil {
			log.Printf("[runtime] cleanup: remove network %s: %v\n%s", n.Name, rerr, string(rerrOut))
		}
	}
	return removed, nil
}
```

- [ ] **Step 6: Run all runtime tests**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add volume and network cleanup"
```

---

### Task 5: Orchestrate Cleanup on dockerRuntime

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `cleanupContainers`, `cleanupImages`, `cleanupVolumes`, `cleanupNetworks`, `CleanupOptions`, `CleanupResult`
- Produces: `(r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — the concrete implementation of the interface method

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
func TestCleanupOptionsDefaults(t *testing.T) {
	opts := CleanupOptions{}
	if opts.DryRun || opts.Containers || opts.Images || opts.Volumes || opts.Networks {
		t.Fatalf("zero-value CleanupOptions must be all-false, got %+v", opts)
	}
}
```

- [ ] **Step 2: Run test to verify it passes trivially**

Run: `go test ./internal/runtime/ -run TestCleanupOptionsDefaults -count=1`
Expected: PASS (this pins the zero-value contract; the CLI layer decides defaults)

- [ ] **Step 3: Implement Cleanup orchestration**

Add to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var result CleanupResult
	var err error

	if opts.Containers {
		result.ContainersRemoved, err = r.cleanupContainers(ctx, opts)
		if err != nil {
			return result, err
		}
	}
	if opts.Images {
		result.ImagesRemoved, err = r.cleanupImages(ctx, opts)
		if err != nil {
			return result, err
		}
	}
	if opts.Volumes {
		result.VolumesRemoved, err = r.cleanupVolumes(ctx, opts)
		if err != nil {
			return result, err
		}
	}
	if opts.Networks {
		result.NetworksRemoved, err = r.cleanupNetworks(ctx, opts)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
```

- [ ] **Step 4: Run all runtime tests + vet**

Run: `go test ./internal/runtime/ -count=1` then `go vet ./internal/runtime/`
Expected: PASS both

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): orchestrate category cleanup in Cleanup"
```

---

### Task 6: CLI `tengiz cleanup` command

**Files:**
- Modify: `internal/cli/root.go` (add `cleanupCmd`, wire in `init()`)
- Modify: `internal/cli/root_test.go` (registration test)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `getEnv(cmd)` unused (cleanup is daemon-wide, not env-scoped)
- Produces: cobra command `cleanup` with flags `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`; defaults to all categories when no category flag is set

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/root_test.go
func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not found")
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not registered")
	}
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup flag %q not found", flag)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanupCmdRegistered -count=1`
Expected: FAIL — `cleanup command not found`

- [ ] **Step 3: Implement the command**

Add to `internal/cli/root.go` (place after `rollbackCmd`/`buildLogsCmd` block):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Clean up unused Docker resources.

Prunes stopped containers, unused images, unused volumes, and unused networks.
Containers managed by Tengiz (labeled tengiz-app) and images built by Tengiz
(tengiz-apps/*) are always protected.

By default all categories are cleaned. Use --containers, --images, --volumes,
or --networks to limit cleanup to specific categories. Use --dry-run to preview
what would be removed without removing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")

		if !containers && !images && !volumes && !networks {
			containers, images, volumes, networks = true, true, true, true
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.CleanupOptions{
			DryRun:     dryRun,
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
		}

		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "removed"
		if dryRun {
			verb = "would remove"
		}
		if len(result.ContainersRemoved) > 0 {
			fmt.Printf("[tengiz] %s containers: %s\n", verb, strings.Join(result.ContainersRemoved, ", "))
		}
		if len(result.ImagesRemoved) > 0 {
			fmt.Printf("[tengiz] %s images: %s\n", verb, strings.Join(result.ImagesRemoved, ", "))
		}
		if len(result.VolumesRemoved) > 0 {
			fmt.Printf("[tengiz] %s volumes: %s\n", verb, strings.Join(result.VolumesRemoved, ", "))
		}
		if len(result.NetworksRemoved) > 0 {
			fmt.Printf("[tengiz] %s networks: %s\n", verb, strings.Join(result.NetworksRemoved, ", "))
		}
		total := len(result.ContainersRemoved) + len(result.ImagesRemoved) +
			len(result.VolumesRemoved) + len(result.NetworksRemoved)
		if total == 0 {
			fmt.Println("[tengiz] nothing to clean")
		}
		return nil
	},
}
```

Register in `init()` (after `rootCmd.AddCommand(buildLogsCmd)`), and register flags:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without removing")
	cleanupCmd.Flags().Bool("containers", false, "clean stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "clean unused non-Tengiz images")
	cleanupCmd.Flags().Bool("volumes", false, "clean unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "clean unused networks")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestCleanupCmdRegistered -count=1`
Expected: PASS

- [ ] **Step 5: Manual smoke test (dry-run against local docker)**

Run:
```bash
go build -o /tmp/tengiz .
/tmp/tengiz cleanup --dry-run
```
Expected: prints `[tengiz] nothing to clean` (or lists candidates) and exits 0. Verify it never removes anything in dry-run.

- [ ] **Step 6: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS (proxy tests may take ~2s each — expected per AGENTS.md)

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 7: Documentation (README + AGENTS.md)

**Files:**
- Modify: `README.md` (CLI Reference section, after `### tengiz rollback <app>` block)
- Modify: `AGENTS.md` (CLI command list, after the `tengiz rollback` line)

- [ ] **Step 1: Add README documentation**

Insert after the `### tengiz rollback <app>` section (after line ~236):

```markdown
### `tengiz cleanup`

Clean up unused Docker resources to reclaim disk space. Prunes stopped containers, unused images, unused volumes, and unused networks.

Containers managed by Tengiz (labeled `tengiz-app`) and images built by Tengiz (`tengiz-apps/*`) are always protected and never removed.

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview what would be removed without removing anything |
| `--containers` | Clean stopped containers not managed by Tengiz |
| `--images` | Clean unused images not built by Tengiz |
| `--volumes` | Clean unused volumes |
| `--networks` | Clean unused networks |

With no category flag, all four categories are cleaned. Example:

```bash
tengiz cleanup              # clean all categories
tengiz cleanup --dry-run    # preview without removing
tengiz cleanup --containers # only stopped non-Tengiz containers
```
```

- [ ] **Step 2: Add AGENTS.md CLI line**

Insert after the `tengiz rollback <app>` line in AGENTS.md:

```
tengiz cleanup [--dry-run] → prune unused Docker resources (protects tengiz-app labeled containers)
```

- [ ] **Step 3: Verify build + vet + tests**

Run: `go build -o /tmp/tengiz . && go vet ./... && go test ./... -count=1`
Expected: all pass

- [ ] **Step 4: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage** — Feature #6 "Docker Housekeeping" from the Priority Ranking: label-based `docker system prune` + `tengiz cleanup`. The plan delivers the `tengiz cleanup` CLI command (Task 6) backed by a `runtime.Cleanup` method (Task 5) that prunes stopped containers, unused images, unused volumes, and unused networks (Tasks 2-4) while protecting `tengiz-app`-labeled containers and `tengiz-apps/*` images — matching the "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" requirement. Docs updated in Task 7.

**2. Placeholder scan** — Every code step contains complete, copy-pasteable Go. No "TBD", no "add validation" without code, no "similar to Task N" references. All exec methods (cleanupContainers/Images/Volumes/Networks) and pure helpers are fully written.

**3. Type consistency** — `CleanupOptions`/`CleanupResult` fields match across Tasks 1, 5, and 6. The container filter uses the existing `labelKey` const (`"tengiz-app"`) from docker.go — consistent with `Create`/`CreateVersioned`/`buildRunArgs` labeling. `stoppedForeignContainers`, `unusedForeignImages`, `foreignUnusedNetworks` signatures are consistent between their defining tasks and their tests. The interface method name `Cleanup` matches in the stub (Task 1), the concrete impl (Task 5), and the CLI call site (Task 6). Note: the mocks in idle/proxy/root_test are updated in Task 1 so the build never breaks mid-plan.

**4. YAGNI check** — No scheduling/periodic job, no `docker system df` space reporting, no prune-on-deploy integration: the spec asks for a manual `tengiz cleanup` command, which is exactly what's built. Periodic scheduling is listed as a separate future feature (#57) and out of scope here.
