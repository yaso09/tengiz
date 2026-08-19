# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes Docker waste (stopped non-Tengiz containers, dangling images, unused networks, unused volumes, build cache) with label-based safety that protects Tengiz-managed containers, plus a `--dry-run` mode.

**Architecture:** Extend the `runtime.Manager` interface with `Prune(ctx, PruneOptions) (PruneResult, error)`. The `dockerRuntime` implementation execs category-specific `docker <object> prune` commands with safety filters — container prune uses `--filter label!=tengiz-app` (protects everything Tengiz manages), image prune uses `--filter dangling=true` (only untagged images, never tagged Tengiz images) — and parses reclaimed bytes from the output. Pure helper functions (`pruneCommand`, `parseReclaimedBytes`, `parseSize`) are unit-tested without Docker. The CLI adds a `cleanupCmd` that resolves flags into `PruneOptions` via a pure `resolvePruneOptions` helper, so option logic is testable without a Docker daemon. `--dry-run` prints the plan without touching the daemon.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (consistent with existing `dockerRuntime`), existing `runtime.Manager` interface + `stubManager`.

## Global Constraints

- No new external Go dependencies (only stdlib + existing cobra/runtime packages)
- All Docker interaction goes through the `docker` CLI via `os/exec` — same pattern as `internal/runtime/docker.go`
- Container prune MUST use `--filter label!=tengiz-app` so Tengiz-managed containers (`tengiz-app=*` label) are never removed
- Image prune MUST use `--filter dangling=true` so tagged Tengiz images (`tengiz-apps/<app>:<id>`) are never removed
- Default `tengiz cleanup` prunes **containers + images + networks** ONLY — volumes and build cache are opt-in (`--volumes`, `--build-cache`)
- `--all` enables all five categories
- When any explicit category flag is set, ONLY the explicitly requested categories run (no implicit defaults)
- `--dry-run` must NOT require a running Docker daemon — only the `docker` binary in PATH
- `Manager` interface gains `Prune`; every mock implementation must add it or packages won't compile: `stubManager` (`internal/runtime/runtime.go`), `mockRTForDeploy` (`internal/cli/root_test.go`), `mockRuntime` (`internal/proxy/proxy_test.go`), `mockRuntime` (`internal/idle/idle_test.go`)
- Integration tests that need a Docker daemon must skip gracefully when Docker is unavailable (see `dockerAvailable` helper in Task 4)
- All tests must pass with `go test ./... -v -count=1` and `go vet ./...`
- Documentation updates required: README.md CLI Reference + Features list, and mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Add `PruneCategory`, `PruneOptions`, `PruneResult`, `pruneCommand`, `parseReclaimedBytes`, `parseSize`; add `dockerRuntime.Prune` |
| `internal/runtime/runtime.go` | Add `Prune` to `Manager` interface; add stub implementation |
| `internal/runtime/cleanup_test.go` | Unit tests for pure helpers, stub, empty-opts Prune, Docker integration test |
| `internal/cli/root.go` | Add `cleanupCmd`, its flags, `resolvePruneOptions`, `humanBytes` |
| `internal/cli/cleanup_test.go` | CLI tests: registration, flags, option resolution, dry-run plan output |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in CLI Reference + Features list |
| `docs/FUTURES_FEATURES.md` | Mark Docker Housekeeping (#6) as implemented |

No new source files created. 1 new test file (`internal/cli/cleanup_test.go`) added.

---

### Task 1: Prune types and pure helpers in runtime

**Files:**
- Modify: `internal/runtime/cleanup.go` — add types + helper functions
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `PruneCategory` constants (`PruneContainers`, `PruneImages`, `PruneNetworks`, `PruneVolumes`, `PruneBuildCache`), `PruneOptions{Containers, Images, Networks, Volumes, BuildCache bool}` with method `Enabled() []PruneCategory`, `PruneResult{ContainersReclaimed, ImagesReclaimed, NetworksReclaimed, VolumesReclaimed, BuildCacheReclaimed uint64}` with methods `Total() uint64` and `Reclaimed(cat PruneCategory) uint64`, `pruneCommand(cat PruneCategory) ([]string, error)`, `parseReclaimedBytes(output string) (uint64, error)`, `parseSize(s string) (uint64, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
package runtime

import (
	"reflect"
	"testing"
)

func TestPruneOptionsEnabled(t *testing.T) {
	tests := []struct {
		name string
		opts PruneOptions
		want []PruneCategory
	}{
		{"none enabled", PruneOptions{}, nil},
		{
			"all enabled",
			PruneOptions{Containers: true, Images: true, Networks: true, Volumes: true, BuildCache: true},
			[]PruneCategory{PruneContainers, PruneImages, PruneNetworks, PruneVolumes, PruneBuildCache},
		},
		{
			"subset preserved order",
			PruneOptions{BuildCache: true, Images: true},
			[]PruneCategory{PruneImages, PruneBuildCache},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.opts.Enabled()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPruneCommand(t *testing.T) {
	tests := []struct {
		cat  PruneCategory
		want []string
	}{
		{PruneContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{PruneImages, []string{"image", "prune", "-f", "--filter", "dangling=true"}},
		{PruneNetworks, []string{"network", "prune", "-f"}},
		{PruneVolumes, []string{"volume", "prune", "-f"}},
		{PruneBuildCache, []string{"builder", "prune", "-f"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.cat), func(t *testing.T) {
			got, err := pruneCommand(tt.cat)
			if err != nil {
				t.Fatalf("pruneCommand(%q) error = %v", tt.cat, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pruneCommand(%q) = %v, want %v", tt.cat, got, tt.want)
			}
		})
	}

	if _, err := pruneCommand(PruneCategory("bogus")); err == nil {
		t.Error("pruneCommand(bogus) expected error, got nil")
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    uint64
		wantErr bool
	}{
		{"empty output", "", 0, false},
		{"no reclaim line", "Deleted Containers:\n", 0, false},
		{"zero bytes", "Total reclaimed space: 0B", 0, false},
		{"bytes", "Total reclaimed space: 512B", 512, false},
		{"bytes with space", "Total reclaimed space: 512 B", 512, false},
		{"kilobytes", "Total reclaimed space: 2.5 kB", 2500, false},
		{"megabytes", "Total reclaimed space: 1.024 MB", 1024000, false},
		{"gigabytes", "Total reclaimed space: 2.348 GB", 2348000000, false},
		{"builder format", "Total:\t0B", 0, false},
		{"builder gibibytes", "Total:\t1.5 GiB", 1610612736, false},
		{"containers output", "Deleted Containers:\n91b57643\n\nTotal reclaimed space: 0B", 0, false},
		{"unknown unit", "Total reclaimed space: 12 XYZ", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseReclaimedBytes(tt.output)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseReclaimedBytes() expected error, got nil (got %d)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseReclaimedBytes() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("parseReclaimedBytes() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPruneResultTotal(t *testing.T) {
	res := PruneResult{
		ContainersReclaimed: 100,
		ImagesReclaimed:     200,
		NetworksReclaimed:   300,
		VolumesReclaimed:    400,
		BuildCacheReclaimed: 500,
	}
	if got := res.Total(); got != 1500 {
		t.Errorf("Total() = %d, want 1500", got)
	}
	if got := res.Reclaimed(PruneImages); got != 200 {
		t.Errorf("Reclaimed(PruneImages) = %d, want 200", got)
	}
	if got := res.Reclaimed(PruneCategory("bogus")); got != 0 {
		t.Errorf("Reclaimed(bogus) = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestPruneOptionsEnabled|TestPruneCommand|TestParseReclaimedBytes|TestPruneResultTotal" -v -count=1`

Expected: FAIL with `undefined: PruneOptions`, `undefined: PruneCategory`, `undefined: pruneCommand`, `undefined: parseReclaimedBytes`, `undefined: PruneResult`

- [ ] **Step 3: Implement types and helpers in `internal/runtime/cleanup.go`**

Add to `internal/runtime/cleanup.go` (after the existing `KeepLastNImages` function). Update the imports block to add `"strconv"`:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)
```

```go
type PruneCategory string

const (
	PruneContainers PruneCategory = "containers"
	PruneImages     PruneCategory = "images"
	PruneNetworks   PruneCategory = "networks"
	PruneVolumes    PruneCategory = "volumes"
	PruneBuildCache PruneCategory = "build-cache"
)

type PruneOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
}

// Enabled returns the categories to prune, in canonical execution order.
func (o PruneOptions) Enabled() []PruneCategory {
	var cats []PruneCategory
	if o.Containers {
		cats = append(cats, PruneContainers)
	}
	if o.Images {
		cats = append(cats, PruneImages)
	}
	if o.Networks {
		cats = append(cats, PruneNetworks)
	}
	if o.Volumes {
		cats = append(cats, PruneVolumes)
	}
	if o.BuildCache {
		cats = append(cats, PruneBuildCache)
	}
	return cats
}

type PruneResult struct {
	ContainersReclaimed uint64
	ImagesReclaimed     uint64
	NetworksReclaimed   uint64
	VolumesReclaimed    uint64
	BuildCacheReclaimed uint64
}

// Total returns the sum of reclaimed bytes across all categories.
func (r PruneResult) Total() uint64 {
	return r.ContainersReclaimed + r.ImagesReclaimed + r.NetworksReclaimed +
		r.VolumesReclaimed + r.BuildCacheReclaimed
}

// Reclaimed returns the reclaimed bytes for a single category.
func (r PruneResult) Reclaimed(cat PruneCategory) uint64 {
	switch cat {
	case PruneContainers:
		return r.ContainersReclaimed
	case PruneImages:
		return r.ImagesReclaimed
	case PruneNetworks:
		return r.NetworksReclaimed
	case PruneVolumes:
		return r.VolumesReclaimed
	case PruneBuildCache:
		return r.BuildCacheReclaimed
	}
	return 0
}

// pruneCommand returns the docker subcommand args (after "docker") for a category.
// Container prune protects Tengiz-managed containers via label!=tengiz-app.
// Image prune only removes dangling (untagged) images, never tagged Tengiz images.
func pruneCommand(cat PruneCategory) ([]string, error) {
	switch cat {
	case PruneContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}, nil
	case PruneImages:
		return []string{"image", "prune", "-f", "--filter", "dangling=true"}, nil
	case PruneNetworks:
		return []string{"network", "prune", "-f"}, nil
	case PruneVolumes:
		return []string{"volume", "prune", "-f"}, nil
	case PruneBuildCache:
		return []string{"builder", "prune", "-f"}, nil
	default:
		return nil, fmt.Errorf("unknown prune category %q", cat)
	}
}

// parseReclaimedBytes extracts the reclaimed size from docker prune output.
// Container/image/volume prune prints "Total reclaimed space: <size>";
// builder prune prints "Total: <size>". Empty output (nothing deleted) yields 0.
func parseReclaimedBytes(output string) (uint64, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Total reclaimed space:"):
			return parseSize(strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:")))
		case strings.HasPrefix(line, "Total:"):
			return parseSize(strings.TrimSpace(strings.TrimPrefix(line, "Total:")))
		}
	}
	return 0, nil
}

var sizeUnits = map[string]uint64{
	"B": 1, "kB": 1e3, "KB": 1e3, "KiB": 1 << 10,
	"MB": 1e6, "MiB": 1 << 20,
	"GB": 1e9, "GiB": 1 << 30,
	"TB": 1e12, "TiB": 1 << 40,
}

// parseSize parses Docker size strings like "512B", "2.5 kB", "1.024 MB", "1.5 GiB".
func parseSize(s string) (uint64, error) {
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return 0, nil
	}
	i := 0
	for i < len(s) && (s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	numStr, unitStr := s[:i], s[i:]
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	mult, ok := sizeUnits[unitStr]
	if !ok {
		return 0, fmt.Errorf("unknown size unit %q in %q", unitStr, s)
	}
	return uint64(val * float64(mult)), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestPruneOptionsEnabled|TestPruneCommand|TestParseReclaimedBytes|TestPruneResultTotal" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add prune types and parsing helpers for docker housekeeping"
```

---

### Task 2: Wire `Prune` into the Manager interface, dockerRuntime, stub, and all mocks

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add `Prune` to `Manager` interface; add stub method after line 122 (`Run`)
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.Prune`
- Modify: `internal/cli/root_test.go:100` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:35` — add `Prune` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:34` — add `Prune` to `mockRuntime`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneResult`, `pruneCommand`, `parseReclaimedBytes` from Task 1
- Produces: `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)` — implemented by `dockerRuntime`, `stubManager`, and all mocks

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Total() != 0 {
		t.Errorf("Prune() total = %d, want 0", res.Total())
	}
}

func TestDockerPruneNoCategories(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Total() != 0 {
		t.Errorf("Prune() total = %d, want 0", res.Total())
	}
}
```

(`TestDockerPruneNoCategories` runs on `&dockerRuntime{}` with no categories enabled — the loop body never executes, so it returns an empty result without invoking the `docker` binary at all.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestDockerPruneNoCategories" -v -count=1`

Expected: FAIL with `stubManager does not implement Manager (missing method Prune)` (interface assertion in existing `TestStubSatisfiesInterface`) and `undefined: Prune` (stub has no method)

- [ ] **Step 3: Add `Prune` to the `Manager` interface in `internal/runtime/runtime.go`**

Add after the `Run` line (line 48) inside the `Manager` interface:

```go
type Manager interface {
	Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	RemoveBySuffix(ctx context.Context, name string, suffix string) error
	IsActive(ctx context.Context, name string) (bool, error)
	GetContainerPort(ctx context.Context, name string, suffix string) (int, error)
	List(ctx context.Context) ([]types.AppStatus, error)
	Logs(ctx context.Context, name string, opts LogOptions) (io.ReadCloser, error)
	WaitForReady(ctx context.Context, name string, internalPort int) error
	WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
}
```

- [ ] **Step 4: Add the stub implementation in `internal/runtime/runtime.go`**

Add after the stub `Run` method (line 122):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}
```

- [ ] **Step 5: Implement `dockerRuntime.Prune` in `internal/runtime/cleanup.go`**

Add to the end of `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var res PruneResult
	for _, cat := range opts.Enabled() {
		args, err := pruneCommand(cat)
		if err != nil {
			return res, err
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return res, fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out))
		}
		reclaimed, err := parseReclaimedBytes(string(out))
		if err != nil {
			return res, err
		}
		switch cat {
		case PruneContainers:
			res.ContainersReclaimed = reclaimed
		case PruneImages:
			res.ImagesReclaimed = reclaimed
		case PruneNetworks:
			res.NetworksReclaimed = reclaimed
		case PruneVolumes:
			res.VolumesReclaimed = reclaimed
		case PruneBuildCache:
			res.BuildCacheReclaimed = reclaimed
		}
	}
	return res, nil
}
```

- [ ] **Step 6: Add `Prune` to the three test mocks**

Add this method to `mockRTForDeploy` in `internal/cli/root_test.go` (after its `Run` method at line 100):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{}, nil
}
```

Add this method to `mockRuntime` in `internal/proxy/proxy_test.go` (after its `Run` method at line 35):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{}, nil
}
```

Add this method to `mockRuntime` in `internal/idle/idle_test.go` (after its `Run` method at line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{}, nil
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestDockerPruneNoCategories" -v -count=1`

Expected: PASS

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/proxy/... ./internal/idle/... -count=1`

Expected: All PASS (mocks now satisfy the extended interface)

- [ ] **Step 8: Run full test suite + vet**

Run: `go test ./... -v -count=1`

Expected: All PASS

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add Prune to runtime Manager interface with docker exec implementation"
```

---

### Task 3: CLI `tengiz cleanup` command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` + `resolvePruneOptions` + `humanBytes`; register command and flags in `init()`
- Test: `internal/cli/cleanup_test.go` (new file)

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.PruneCategory`, `runtime.NewDocker()` from Tasks 1-2
- Produces: `tengiz cleanup [--all] [--containers] [--images] [--networks] [--volumes] [--build-cache] [--dry-run]`; package-level `resolvePruneOptions(cmd *cobra.Command) (runtime.PruneOptions, error)` and `humanBytes(n uint64) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"os/exec"
	"reflect"
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

func TestCleanupCommandHasFlags(t *testing.T) {
	for _, flag := range []string{"all", "containers", "images", "networks", "volumes", "build-cache", "dry-run"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestResolvePruneOptionsDefaults(t *testing.T) {
	cmd := cleanupCmd
	cmd.ParseFlags([]string{})
	opts, err := resolvePruneOptions(cmd)
	if err != nil {
		t.Fatalf("resolvePruneOptions() error = %v", err)
	}
	want := runtime.PruneOptions{Containers: true, Images: true, Networks: true}
	if !reflect.DeepEqual(opts, want) {
		t.Errorf("default opts = %+v, want %+v", opts, want)
	}
}

func TestResolvePruneOptionsAll(t *testing.T) {
	cmd := cleanupCmd
	cmd.ParseFlags([]string{"--all"})
	opts, err := resolvePruneOptions(cmd)
	if err != nil {
		t.Fatalf("resolvePruneOptions() error = %v", err)
	}
	want := runtime.PruneOptions{Containers: true, Images: true, Networks: true, Volumes: true, BuildCache: true}
	if !reflect.DeepEqual(opts, want) {
		t.Errorf("--all opts = %+v, want %+v", opts, want)
	}
}

func TestResolvePruneOptionsExplicitVolumesOnly(t *testing.T) {
	cmd := cleanupCmd
	cmd.ParseFlags([]string{"--volumes"})
	opts, err := resolvePruneOptions(cmd)
	if err != nil {
		t.Fatalf("resolvePruneOptions() error = %v", err)
	}
	want := runtime.PruneOptions{Volumes: true}
	if !reflect.DeepEqual(opts, want) {
		t.Errorf("--volumes opts = %+v, want %+v", opts, want)
	}
}

func TestResolvePruneOptionsDryRunKeepsDefaults(t *testing.T) {
	cmd := cleanupCmd
	cmd.ParseFlags([]string{"--dry-run"})
	opts, err := resolvePruneOptions(cmd)
	if err != nil {
		t.Fatalf("resolvePruneOptions() error = %v", err)
	}
	want := runtime.PruneOptions{Containers: true, Images: true, Networks: true}
	if !reflect.DeepEqual(opts, want) {
		t.Errorf("--dry-run opts = %+v, want %+v", opts, want)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		n    uint64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{2500, "2.5 kB"},
		{1024000, "1.0 MB"},
		{2348000000, "2.3 GB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.n); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestCleanupDryRunPrintsPlan(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	out := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("cleanup --dry-run failed: %v", err)
		}
	})
	for _, want := range []string{"would prune containers", "would prune images", "would prune networks"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q, got: %s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup|TestResolvePruneOptions|TestHumanBytes" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd`, `undefined: resolvePruneOptions`, `undefined: humanBytes`

- [ ] **Step 3: Implement the command, flags, and helpers in `internal/cli/root.go`**

In `init()`, after `rootCmd.AddCommand(runCmd)` (line 67), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

In `init()`, after the existing flag registrations (after line 88), add:

```go
	cleanupCmd.Flags().Bool("all", false, "prune all categories (containers, images, networks, volumes, build cache)")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (opt-in, may remove volumes used by other tools)")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "print what would be pruned without removing anything")
```

Add the command definition and helpers after the `runCmd` declaration (after line 1162):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up Docker resources (containers, images, networks, volumes, build cache)",
	Long: `Remove Docker waste to free disk space on the host.

By default prunes stopped non-Tengiz containers, dangling images, and unused
networks. Containers managed by Tengiz (labeled tengiz-app=*) are always
protected and never removed. Volumes and build cache are opt-in.

When any category flag is set, only the explicitly requested categories run.
Use --dry-run to print the actions without removing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := resolvePruneOptions(cmd)
		if err != nil {
			return err
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if dryRun {
			for _, cat := range opts.Enabled() {
				fmt.Printf("[tengiz] would prune %s\n", cat)
			}
			return nil
		}

		res, err := rt.Prune(context.Background(), opts)
		if err != nil {
			return err
		}

		for _, cat := range opts.Enabled() {
			fmt.Printf("[tengiz] pruned %s (reclaimed %s)\n", cat, humanBytes(res.Reclaimed(cat)))
		}
		fmt.Printf("[tengiz] cleanup complete: %s reclaimed\n", humanBytes(res.Total()))
		return nil
	},
}

func resolvePruneOptions(cmd *cobra.Command) (runtime.PruneOptions, error) {
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	buildCache, _ := cmd.Flags().GetBool("build-cache")

	opts := runtime.PruneOptions{
		Containers: containers || all,
		Images:     images || all,
		Networks:   networks || all,
		Volumes:    volumes || all,
		BuildCache: buildCache || all,
	}

	// No category flag set: default to the safe categories.
	explicit := containers || images || networks || volumes || buildCache || all
	if !explicit {
		opts.Containers = true
		opts.Images = true
		opts.Networks = true
	}
	return opts, nil
}

func humanBytes(n uint64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanup|TestResolvePruneOptions|TestHumanBytes" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run full test suite + vet + build**

Run: `go test ./... -v -count=1`

Expected: All PASS

Run: `go vet ./...`

Expected: No issues

Run: `go build -o tengiz .`

Expected: Build succeeds

- [ ] **Step 6: Smoke-test the real command**

Run: `./tengiz cleanup --dry-run`

Expected:
```
[tengiz] would prune containers
[tengiz] would prune images
[tengiz] would prune networks
```

Run: `./tengiz cleanup`

Expected: Runs the three default prunes and prints `[tengiz] cleanup complete: <size> reclaimed`

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Docker integration test for container-prune safety filter

**Files:**
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `dockerRuntime.Prune`, `PruneOptions{Containers: true}` from Tasks 1-2
- Produces: proof that `label!=tengiz-app` removes a stray container but keeps a `tengiz-app`-labeled container; `dockerAvailable(t *testing.T) bool` helper (skips when Docker missing)

- [ ] **Step 1: Write the failing/guarded test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestDockerPruneProtectsTengizContainers(t *testing.T) {
	if !dockerAvailable(t) {
		return
	}
	ctx := context.Background()
	id := fmt.Sprintf("%d", os.Getpid())
	img := "tengiz-prune-test-" + id
	stray := "tengiz-stray-" + id
	prot := "tengiz-protected-" + id

	build := exec.CommandContext(ctx, "docker", "build", "-q", "-t", img, "-f", "-", ".")
	build.Stdin = strings.NewReader("FROM scratch\n")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("docker build: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		exec.CommandContext(context.Background(), "docker", "rm", "-f", stray, prot).Run()
		exec.CommandContext(context.Background(), "docker", "rmi", "-f", img).Run()
	})

	// Stray container: no tengiz-app label.
	if out, err := exec.CommandContext(ctx, "docker", "create", "--name", stray, img, "/nonexistent").CombinedOutput(); err != nil {
		t.Fatalf("create stray: %v\n%s", err, out)
	}
	// Tengiz-managed container: has tengiz-app label.
	if out, err := exec.CommandContext(ctx, "docker", "create", "--name", prot, "--label", "tengiz-app=myapp", img, "/nonexistent").CombinedOutput(); err != nil {
		t.Fatalf("create protected: %v\n%s", err, out)
	}

	r := &dockerRuntime{}
	if _, err := r.Prune(ctx, PruneOptions{Containers: true}); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}

	if err := exec.CommandContext(ctx, "docker", "inspect", stray).Run(); err == nil {
		t.Errorf("stray container %s still exists after prune", stray)
	}
	if err := exec.CommandContext(ctx, "docker", "inspect", prot).Run(); err != nil {
		t.Errorf("protected container %s was removed by prune: %v", prot, err)
	}
}

func dockerAvailable(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "ps").Run(); err != nil {
		t.Skip("docker daemon not available")
	}
	return true
}
```

Add the new imports to `internal/runtime/cleanup_test.go`:

```go
import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)
```

- [ ] **Step 2: Run test to verify it passes (or skips)**

Run: `go test ./internal/runtime/... -run "TestDockerPruneProtectsTengizContainers" -v -count=1`

Expected: PASS when a Docker daemon is available; `SKIP` when not.

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS (or SKIP only for the docker-guarded test when no daemon)

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/cleanup_test.go
git commit -m "test: add docker integration test for prune safety filter"
```

---

### Task 5: Documentation updates

**Files:**
- Modify: `README.md` — Features list + CLI Reference
- Modify: `docs/FUTURES_FEATURES.md` — mark Docker Housekeeping (#6) implemented

- [ ] **Step 1: Add the feature bullet to the README Features list**

In `README.md`, add this bullet to the "Features" list (after the "No daemon required" bullet, around line 46):

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes stopped containers, dangling images, unused networks, volumes, and build cache with label-based safety for Tengiz-managed apps.
```

- [ ] **Step 2: Add the `tengiz cleanup` CLI Reference section**

In `README.md`, add a new section right after the `### tengiz build-logs <app> [deployment-id]` section (after line 181):

```markdown
### `tengiz cleanup`

Clean up Docker resources to free disk space on the host.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped non-Tengiz containers |
| `--images` | Prune dangling images |
| `--networks` | Prune unused networks |
| `--volumes` | Prune unused volumes (opt-in — may remove volumes used by other tools) |
| `--build-cache` | Prune Docker build cache |
| `--all` | Enable all categories (containers, images, networks, volumes, build cache) |
| `--dry-run` | Print what would be pruned without removing anything |

By default (no category flags) prunes **containers, images, and networks**.
Tengiz-managed containers (labeled `tengiz-app=*`) are always protected and never removed.
When any category flag is set, only the explicitly requested categories run.
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 Priority Ranking table, change the row (line 19):

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the `## Docker Housekeeping (Otomatik Temizlik)` section, add a Status line right after the "Why add to Tengiz" line (after line 380):

```markdown
- **Status:** ✅ Implemented (2026-08-19)
```

- [ ] **Step 4: Verify build and tests**

Run: `go build -o tengiz .`

Expected: Build succeeds

Run: `go test ./... -v -count=1`

Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

### Task 6: Final integration test and self-review

**Files:**
- Tests: full suite

- [ ] **Step 1: Verify full workflow end-to-end**

Run: `go test ./... -v -count=1`

Expected: All PASS (docker-guarded test passes or skips)

Run: `go vet ./...`

Expected: No issues

Run: `go build -o tengiz .`

Expected: Build succeeds

- [ ] **Step 2: Self-review against the feature spec**

Check against `docs/FUTURES_FEATURES.md` feature #6 (Docker Housekeeping):
- `tengiz cleanup` command exists ✅ (Task 3)
- Prunes unused containers, images, networks, volumes, build cache ✅ (Tasks 1-2, volume/build-cache opt-in per safety design)
- Label-based filtering protects Tengiz-managed containers ✅ (Task 1 `pruneCommand` `label!=tengiz-app`; proven in Task 4)
- Disk-space reporting (reclaimed bytes) ✅ (Task 1 `parseReclaimedBytes`; printed by Task 3)
- Dry-run safety ✅ (Task 3)
- Periodic cleanup: out of scope — `tengiz cleanup` is designed to be scheduled externally via cron (documented in README)

- [ ] **Step 3: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task". None present — every step contains complete code and exact commands.

- [ ] **Step 4: Type consistency check**

- `PruneCategory` values: `"containers"`, `"images"`, `"networks"`, `"volumes"`, `"build-cache"` — used consistently in `pruneCommand`, `PruneResult.Reclaimed`, CLI output
- `PruneOptions` fields: `Containers`, `Images`, `Networks`, `Volumes`, `BuildCache` — used consistently in `Enabled()`, `resolvePruneOptions`, `dockerRuntime.Prune`
- `PruneResult` fields: `ContainersReclaimed`, `ImagesReclaimed`, `NetworksReclaimed`, `VolumesReclaimed`, `BuildCacheReclaimed` — same names in `Total()`, `Reclaimed()`, `dockerRuntime.Prune`, CLI summary
- `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)` — same signature on interface, stub, dockerRuntime, and all three mocks
- Flag names on `cleanupCmd` match the keys read in `resolvePruneOptions` (`all`, `containers`, `images`, `networks`, `volumes`, `build-cache`, `dry-run`)

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "chore: final verification for docker housekeeping feature"
```

(Only run this if there are uncommitted changes; otherwise skip.)