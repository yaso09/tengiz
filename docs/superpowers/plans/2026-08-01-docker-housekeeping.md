# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes stopped non-Tengiz containers, dangling/unused images, unused volumes, unused networks, and the Docker build cache — safely protected by label-based filtering so Tengiz-managed containers are never removed — with dry-run preview, per-category selection, confirmation, and disk-usage reporting.

**Architecture:** A new `Housekeeper` interface in `internal/runtime` exposes `Prune`, `DryRun`, and `DiskUsage`. The production implementation `dockerHousekeeper` shells out to the `docker` CLI (matching the existing `dockerRuntime` exec pattern); a `stubHousekeeper` backs tests. Docker commands are built by pure, individually-tested arg-builder functions that encode the label-based safety filter `label!=tengiz-app`. The CLI registers a `tengiz cleanup` command that maps flags to `PruneOptions`, prompts for confirmation (skippable with `--force`), and prints a `docker system df` report with `--stats`.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `internal/runtime` exec-based Docker pattern, no new external dependencies.

## Global Constraints

- Docker CLI required at runtime (exec-based, no Docker SDK) — consistent with `internal/runtime/docker.go`
- **Safety rule:** Tengiz-managed containers are always protected via the `label!=tengiz-app` filter on container/volume/network prune and list commands. Stopped Tengiz containers (idle scale-to-zero) are never pruned.
- Default behavior: when no category flag is given, all five categories are pruned (same as passing `--all`)
- Category flags narrow the default scope; `--all-images` is a modifier on the `images` category only
- `--dry-run` never deletes anything; `--force` skips the interactive confirmation prompt
- Confirmation prompt requires an exact `y` (case-insensitive, trimmed) on stdin; anything else cancels
- Cleanup is global (not `--env`-scoped): it operates at the Docker daemon level and protects containers from all environments via the shared label
- Supported Docker CLI features: `container prune`, `image prune`, `volume prune`, `network prune` with `--filter label!=` (Docker 17.06+) and `builder prune` / `builder du` (BuildKit, Docker 19.03+)
- No new Go module dependencies; Go 1.26 module `github.com/yaso09/tengiz`
- Tests follow existing repo conventions: pure arg-builder helpers + stub implementations (see `internal/runtime/runtime_test.go`, `internal/cli/root_test.go`). Docker CLI integration is exercised manually, not in unit tests.
- Verification commands: `go build -o tengiz .`, `go vet ./...`, `go test ./... -v -count=1` must all pass
- Commit style: conventional commits (`feat: ...`, `test: ...`, `docs: ...`) — one commit per task

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/housekeeping.go` | **Create.** `PruneOptions`, `PruneResult`, `DryRunResult`, `Housekeeper` interface, `dockerHousekeeper` + `stubHousekeeper` implementations, pure Docker arg-builder helpers, `runDockerCommand`, `countLines` |
| `internal/runtime/housekeeping_test.go` | **Create.** Tests for arg builders, `countLines`, `pruneCommands`/`dryRunCommands` composition, stub interface satisfaction |
| `internal/cli/cleanup.go` | **Create.** `cleanupCmd` Cobra command with all flags, confirmation prompt, dry-run/force/stats handling, output printers, `newHousekeeper` injectable factory var, flag registration `init()` |
| `internal/cli/cleanup_test.go` | **Create.** CLI registration, flag presence, `describePruneTargets`, dry-run output, `--force` skip-prompt, selective-category execution tests |
| `internal/cli/root.go` | **Modify.** Register `cleanupCmd` in `init()` (one line, next to `rollbackCmd`) |
| `README.md` | **Modify.** Add `tengiz cleanup` to Features list + CLI Reference section |
| `docs/FUTURES_FEATURES.md` | **Modify.** Mark feature #6 Docker Housekeeping as ✅ Implemented |

Task dependencies: Task 1 → 2 → 3 → 4 → 5. Each task is independently testable.

---

### Task 1: Housekeeper interface, types, and stub

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: nothing new (uses only Go stdlib)
- Produces: `runtime.PruneOptions` struct (fields `Containers, Images, AllImages, Volumes, Networks, Cache bool`), `PruneOptions.Any() bool`, `runtime.PruneResult` struct (fields `ContainerOutput, ImageOutput, VolumeOutput, NetworkOutput, CacheOutput string`), `runtime.DryRunResult` struct (fields `Containers, Images, Volumes, Networks, Cache int`), `runtime.Housekeeper` interface with `Prune(ctx, opts) (PruneResult, error)`, `DryRun(ctx, opts) (DryRunResult, error)`, `DiskUsage(ctx) (string, error)`, `runtime.NewDockerHousekeeper() (Housekeeper, error)`, `runtime.NewStubHousekeeper() Housekeeper`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/housekeeping_test.go
package runtime

import (
	"context"
	"testing"
)

func TestStubHousekeeperSatisfiesInterface(t *testing.T) {
	var iface Housekeeper = NewStubHousekeeper()
	if iface == nil {
		t.Fatal("stubHousekeeper does not implement Housekeeper")
	}
}

func TestNewStubHousekeeper(t *testing.T) {
	h := NewStubHousekeeper()
	if h == nil {
		t.Fatal("NewStubHousekeeper() returned nil")
	}
}

func TestPruneOptionsAny(t *testing.T) {
	if (PruneOptions{}).Any() {
		t.Error("empty PruneOptions should return false for Any()")
	}
	for name, opts := range map[string]PruneOptions{
		"containers": {Containers: true},
		"images":     {Images: true},
		"all-images": {AllImages: true},
		"volumes":    {Volumes: true},
		"networks":   {Networks: true},
		"cache":      {Cache: true},
	} {
		if !opts.Any() {
			t.Errorf("PruneOptions{%s}.Any() = false, want true", name)
		}
	}
}

func TestStubHousekeeperMethods(t *testing.T) {
	h := NewStubHousekeeper()
	ctx := context.Background()
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, Cache: true}

	res, err := h.Prune(ctx, opts)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.ContainerOutput != "" || res.ImageOutput != "" || res.VolumeOutput != "" ||
		res.NetworkOutput != "" || res.CacheOutput != "" {
		t.Errorf("Prune() = %+v, want empty PruneResult", res)
	}

	dr, err := h.DryRun(ctx, opts)
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if dr.Containers != 0 || dr.Images != 0 || dr.Volumes != 0 || dr.Networks != 0 || dr.Cache != 0 {
		t.Errorf("DryRun() = %+v, want zero DryRunResult", dr)
	}

	du, err := h.DiskUsage(ctx)
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if du != "" {
		t.Errorf("DiskUsage() = %q, want empty string", du)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestStubHousekeeper|TestNewStubHousekeeper|TestPruneOptionsAny" -v -count=1`

Expected: FAIL with `undefined: Housekeeper`, `undefined: NewStubHousekeeper`, `undefined: PruneOptions`

- [ ] **Step 3: Write the implementation**

```go
// internal/runtime/housekeeping.go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
)

// PruneOptions selects which Docker resource categories are cleaned.
type PruneOptions struct {
	Containers bool
	Images     bool
	AllImages  bool
	Volumes    bool
	Networks   bool
	Cache      bool
}

// Any reports whether at least one category (or the AllImages modifier) is selected.
func (o PruneOptions) Any() bool {
	return o.Containers || o.Images || o.AllImages || o.Volumes || o.Networks || o.Cache
}

// PruneResult holds the raw `docker <object> prune` output per category.
type PruneResult struct {
	ContainerOutput string
	ImageOutput     string
	VolumeOutput    string
	NetworkOutput   string
	CacheOutput     string
}

// DryRunResult holds the count of items that would be removed per category.
type DryRunResult struct {
	Containers int
	Images     int
	Volumes    int
	Networks   int
	Cache      int
}

// Housekeeper manages Docker resource cleanup.
type Housekeeper interface {
	Prune(ctx context.Context, opts PruneOptions) (PruneResult, error)
	DryRun(ctx context.Context, opts PruneOptions) (DryRunResult, error)
	DiskUsage(ctx context.Context) (string, error)
}

// NewDockerHousekeeper returns a Housekeeper backed by the docker CLI.
func NewDockerHousekeeper() (Housekeeper, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerHousekeeper{}, nil
}

type dockerHousekeeper struct{}

// NewStubHousekeeper returns a no-op Housekeeper for tests.
func NewStubHousekeeper() Housekeeper {
	return &stubHousekeeper{}
}

type stubHousekeeper struct{}

func (h *stubHousekeeper) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	return PruneResult{}, nil
}

func (h *stubHousekeeper) DryRun(ctx context.Context, opts PruneOptions) (DryRunResult, error) {
	return DryRunResult{}, nil
}

func (h *stubHousekeeper) DiskUsage(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestStubHousekeeper|TestNewStubHousekeeper|TestPruneOptionsAny" -v -count=1`

Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add Housekeeper interface, types, and stub for Docker cleanup"
```

---

### Task 2: Docker prune/list arg builders (label-based safety logic)

**Files:**
- Modify: `internal/runtime/housekeeping.go` (append after Task 1 code)
- Test: `internal/runtime/housekeeping_test.go` (append)

**Interfaces:**
- Consumes: `PruneOptions` from Task 1; `labelKey` const (`"tengiz-app"`) already defined in `internal/runtime/docker.go:76`
- Produces: `tengizLabelFilter string` (value `"label!=tengiz-app"`), pure functions `buildContainerPruneArgs() []string`, `buildImagePruneArgs(opts PruneOptions) []string`, `buildVolumePruneArgs() []string`, `buildNetworkPruneArgs() []string`, `buildCachePruneArgs() []string`, `buildContainerListArgs() []string`, `buildImageListArgs(opts PruneOptions) []string`, `buildVolumeListArgs() []string`, `buildNetworkListArgs() []string`, `buildCacheUsageArgs() []string`, `buildSystemDFArgs() []string`, `countLines(s string) int`, `pruneCommand` struct (fields `kind string`, `args []string`), `pruneCommands(opts PruneOptions) []pruneCommand`, `dryRunCommands(opts PruneOptions) []pruneCommand`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/housekeeping_test.go (append)
package runtime

import (
	"reflect"
	"testing"
)

// Note: merge `reflect` into the existing import block of housekeeping_test.go
// (which already imports `context` and `testing` from Task 1).

func TestBuildContainerPruneArgs(t *testing.T) {
	got := buildContainerPruneArgs()
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildContainerPruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildImagePruneArgs(t *testing.T) {
	if got := buildImagePruneArgs(PruneOptions{}); !reflect.DeepEqual(got, []string{"image", "prune", "-f"}) {
		t.Errorf("dangling: buildImagePruneArgs() = %v", got)
	}
	if got := buildImagePruneArgs(PruneOptions{AllImages: true}); !reflect.DeepEqual(got, []string{"image", "prune", "-f", "-a"}) {
		t.Errorf("all: buildImagePruneArgs() = %v", got)
	}
}

func TestBuildVolumePruneArgs(t *testing.T) {
	got := buildVolumePruneArgs()
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildVolumePruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildNetworkPruneArgs(t *testing.T) {
	got := buildNetworkPruneArgs()
	want := []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildNetworkPruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildCachePruneArgs(t *testing.T) {
	got := buildCachePruneArgs()
	want := []string{"builder", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildCachePruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildContainerListArgs(t *testing.T) {
	got := buildContainerListArgs()
	want := []string{"ps", "-aq", "--filter", "status=exited", "--filter", "status=created", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildContainerListArgs() = %v, want %v", got, want)
	}
}

func TestBuildImageListArgs(t *testing.T) {
	want := []string{"images", "-q", "--filter", "dangling=true"}
	if got := buildImageListArgs(PruneOptions{}); !reflect.DeepEqual(got, want) {
		t.Errorf("dangling: buildImageListArgs() = %v, want %v", got, want)
	}
	wantAll := []string{"images", "-q"}
	if got := buildImageListArgs(PruneOptions{AllImages: true}); !reflect.DeepEqual(got, wantAll) {
		t.Errorf("all: buildImageListArgs() = %v, want %v", got, wantAll)
	}
}

func TestBuildVolumeListArgs(t *testing.T) {
	got := buildVolumeListArgs()
	want := []string{"volume", "ls", "-q", "--filter", "dangling=true", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildVolumeListArgs() = %v, want %v", got, want)
	}
}

func TestBuildNetworkListArgs(t *testing.T) {
	got := buildNetworkListArgs()
	want := []string{"network", "ls", "-q", "--filter", "dangling=true", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildNetworkListArgs() = %v, want %v", got, want)
	}
}

func TestBuildCacheUsageArgs(t *testing.T) {
	got := buildCacheUsageArgs()
	want := []string{"builder", "du"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildCacheUsageArgs() = %v, want %v", got, want)
	}
}

func TestBuildSystemDFArgs(t *testing.T) {
	got := buildSystemDFArgs()
	want := []string{"system", "df"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildSystemDFArgs() = %v, want %v", got, want)
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"whitespace", "   \n\n ", 0},
		{"one", "abc123", 1},
		{"multi", "a\nb\nc\n", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countLines(tt.in); got != tt.want {
				t.Errorf("countLines(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestPruneCommandsAll(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, Cache: true}
	got := pruneCommands(opts)
	if len(got) != 5 {
		t.Fatalf("pruneCommands() len = %d, want 5: %+v", len(got), got)
	}
	wantKinds := []string{"containers", "images", "volumes", "networks", "cache"}
	for i, kind := range wantKinds {
		if got[i].kind != kind {
			t.Errorf("pruneCommands()[%d].kind = %q, want %q", i, got[i].kind, kind)
		}
	}
	if !reflect.DeepEqual(got[0].args, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}) {
		t.Errorf("pruneCommands()[0].args = %v", got[0].args)
	}
}

func TestPruneCommandsSelective(t *testing.T) {
	got := pruneCommands(PruneOptions{Images: true, AllImages: true})
	if len(got) != 1 || got[0].kind != "images" {
		t.Fatalf("pruneCommands() = %+v, want only images", got)
	}
	want := []string{"image", "prune", "-f", "-a"}
	if !reflect.DeepEqual(got[0].args, want) {
		t.Errorf("images args = %v, want %v", got[0].args, want)
	}
}

func TestPruneCommandsEmpty(t *testing.T) {
	if got := pruneCommands(PruneOptions{}); len(got) != 0 {
		t.Errorf("pruneCommands() = %+v, want empty", got)
	}
}

func TestDryRunCommandsAll(t *testing.T) {
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, Cache: true}
	got := dryRunCommands(opts)
	if len(got) != 5 {
		t.Fatalf("dryRunCommands() len = %d, want 5: %+v", len(got), got)
	}
	wantKinds := []string{"containers", "images", "volumes", "networks", "cache"}
	for i, kind := range wantKinds {
		if got[i].kind != kind {
			t.Errorf("dryRunCommands()[%d].kind = %q, want %q", i, got[i].kind, kind)
		}
	}
	if !reflect.DeepEqual(got[0].args, []string{"ps", "-aq", "--filter", "status=exited", "--filter", "status=created", "--filter", "label!=tengiz-app"}) {
		t.Errorf("dryRunCommands()[0].args = %v", got[0].args)
	}
}

func TestDryRunCommandsSelective(t *testing.T) {
	got := dryRunCommands(PruneOptions{Images: true, AllImages: true})
	if len(got) != 1 || got[0].kind != "images" {
		t.Fatalf("dryRunCommands() = %+v, want only images", got)
	}
	if !reflect.DeepEqual(got[0].args, []string{"images", "-q"}) {
		t.Errorf("images args = %v", got[0].args)
	}
}

func TestDryRunCommandsEmpty(t *testing.T) {
	if got := dryRunCommands(PruneOptions{}); len(got) != 0 {
		t.Errorf("dryRunCommands() = %+v, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestBuild|TestCountLines|TestPruneCommands|TestDryRunCommands" -v -count=1`

Expected: FAIL with `undefined: buildContainerPruneArgs` (first undefined symbol)

- [ ] **Step 3: Write the implementation**

Append to `internal/runtime/housekeeping.go`:

```go
// tengizLabelFilter protects Tengiz-managed containers (labeled "tengiz-app")
// from being pruned. Docker's label!= filter matches resources WITHOUT the label.
const tengizLabelFilter = "label!=" + labelKey

func buildContainerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", tengizLabelFilter}
}

func buildImagePruneArgs(opts PruneOptions) []string {
	args := []string{"image", "prune", "-f"}
	if opts.AllImages {
		args = append(args, "-a")
	}
	return args
}

func buildVolumePruneArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", tengizLabelFilter}
}

func buildNetworkPruneArgs() []string {
	return []string{"network", "prune", "-f", "--filter", tengizLabelFilter}
}

func buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func buildContainerListArgs() []string {
	return []string{"ps", "-aq", "--filter", "status=exited", "--filter", "status=created", "--filter", tengizLabelFilter}
}

func buildImageListArgs(opts PruneOptions) []string {
	if opts.AllImages {
		return []string{"images", "-q"}
	}
	return []string{"images", "-q", "--filter", "dangling=true"}
}

func buildVolumeListArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "dangling=true", "--filter", tengizLabelFilter}
}

func buildNetworkListArgs() []string {
	return []string{"network", "ls", "-q", "--filter", "dangling=true", "--filter", tengizLabelFilter}
}

func buildCacheUsageArgs() []string {
	return []string{"builder", "du"}
}

func buildSystemDFArgs() []string {
	return []string{"system", "df"}
}

// countLines counts non-empty lines in command output.
func countLines(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

// pruneCommand pairs a resource category with the docker args that prune it.
type pruneCommand struct {
	kind string
	args []string
}

func pruneCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand
	if opts.Containers {
		cmds = append(cmds, pruneCommand{"containers", buildContainerPruneArgs()})
	}
	if opts.Images {
		cmds = append(cmds, pruneCommand{"images", buildImagePruneArgs(opts)})
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{"volumes", buildVolumePruneArgs()})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{"networks", buildNetworkPruneArgs()})
	}
	if opts.Cache {
		cmds = append(cmds, pruneCommand{"cache", buildCachePruneArgs()})
	}
	return cmds
}

func dryRunCommands(opts PruneOptions) []pruneCommand {
	var cmds []pruneCommand
	if opts.Containers {
		cmds = append(cmds, pruneCommand{"containers", buildContainerListArgs()})
	}
	if opts.Images {
		cmds = append(cmds, pruneCommand{"images", buildImageListArgs(opts)})
	}
	if opts.Volumes {
		cmds = append(cmds, pruneCommand{"volumes", buildVolumeListArgs()})
	}
	if opts.Networks {
		cmds = append(cmds, pruneCommand{"networks", buildNetworkListArgs()})
	}
	if opts.Cache {
		cmds = append(cmds, pruneCommand{"cache", buildCacheUsageArgs()})
	}
	return cmds
}
```

Note: `trimSpace` and `strings.Split` require updating the imports block at the top of `housekeeping.go`:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestBuild|TestCountLines|TestPruneCommands|TestDryRunCommands" -v -count=1`

Expected: PASS (all builder and composition tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add label-safe docker prune/list arg builders"
```

---

### Task 3: dockerHousekeeper Prune/DryRun/DiskUsage implementation

**Files:**
- Modify: `internal/runtime/housekeeping.go` (append after Task 2 code)
- Test: `internal/runtime/housekeeping_test.go` (append)

**Interfaces:**
- Consumes: `PruneOptions`, `PruneResult`, `DryRunResult` from Task 1; `pruneCommands`, `dryRunCommands`, `buildSystemDFArgs`, `countLines` from Task 2
- Produces: `runDockerCommand(ctx context.Context, args ...string) (string, error)`, concrete `dockerHousekeeper` methods `Prune`, `DryRun`, `DiskUsage` satisfying the `Housekeeper` interface from Task 1

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/housekeeping_test.go (append)
package runtime

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestRunDockerCommandWhenDockerMissing(t *testing.T) {
	if _, err := exec.LookPath("docker"); err == nil {
		t.Skip("docker binary present; skipping missing-binary test")
	}
	_, err := runDockerCommand(context.Background(), "version")
	if err == nil {
		t.Fatal("runDockerCommand() with no docker binary returned nil error")
	}
}

func TestRunDockerCommandWhenDockerPresent(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker binary not present; skipping integration smoke test")
	}
	out, err := runDockerCommand(context.Background(), "version", "--format", "{{.Client.Version}}")
	if err != nil {
		t.Fatalf("runDockerCommand() error = %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Error("runDockerCommand() returned empty docker client version")
	}
}
```

Note: merge the new imports (`os/exec`, `strings`) into the existing import block of `housekeeping_test.go`; `context` and `testing` are already imported from earlier tasks.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestRunDockerCommand" -v -count=1`

Expected: FAIL with `undefined: runDockerCommand`

- [ ] **Step 3: Write the implementation**

Append to `internal/runtime/housekeeping.go`:

```go
// runDockerCommand executes a docker CLI command and returns its combined output.
func runDockerCommand(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func (h *dockerHousekeeper) Prune(ctx context.Context, opts PruneOptions) (PruneResult, error) {
	var res PruneResult
	for _, pc := range pruneCommands(opts) {
		out, err := runDockerCommand(ctx, pc.args...)
		if err != nil {
			return res, err
		}
		switch pc.kind {
		case "containers":
			res.ContainerOutput = out
		case "images":
			res.ImageOutput = out
		case "volumes":
			res.VolumeOutput = out
		case "networks":
			res.NetworkOutput = out
		case "cache":
			res.CacheOutput = out
		}
	}
	return res, nil
}

func (h *dockerHousekeeper) DryRun(ctx context.Context, opts PruneOptions) (DryRunResult, error) {
	var res DryRunResult
	for _, pc := range dryRunCommands(opts) {
		out, err := runDockerCommand(ctx, pc.args...)
		if err != nil {
			return res, err
		}
		n := countLines(out)
		switch pc.kind {
		case "containers":
			res.Containers = n
		case "images":
			res.Images = n
		case "volumes":
			res.Volumes = n
		case "networks":
			res.Networks = n
		case "cache":
			res.Cache = n
		}
	}
	return res, nil
}

func (h *dockerHousekeeper) DiskUsage(ctx context.Context) (string, error) {
	out, err := runDockerCommand(ctx, buildSystemDFArgs()...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestRunDockerCommand" -v -count=1`

Expected: PASS (both tests; the docker-present variant skips gracefully on machines without docker, runs and passes where docker exists)

- [ ] **Step 5: Run the full runtime package suite**

Run: `go test ./internal/runtime/ -count=1`

Expected: PASS (existing tests + all new tests)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: implement dockerHousekeeper Prune, DryRun, and DiskUsage"
```

---

### Task 4: CLI `tengiz cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go:38-75` — add `rootCmd.AddCommand(cleanupCmd)` in `init()`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.DryRunResult`, `runtime.Housekeeper`, `runtime.NewDockerHousekeeper()`, `runtime.NewStubHousekeeper()` from Tasks 1-3
- Produces: `cleanupCmd *cobra.Command` (registered under `rootCmd`), package-level `var newHousekeeper = func() (runtime.Housekeeper, error)` (test seam), helpers `describePruneTargets(opts runtime.PruneOptions) string`, `printDryRunResult(res runtime.DryRunResult)`, `printPruneResult(res runtime.PruneResult)`

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

// resetCleanupFlags restores all cleanup flag values to their defaults.
// Cobra/pflag persist flag values across Execute() calls on the global
// rootCmd, so tests that set different flags must reset state to stay
// order-independent.
func resetCleanupFlags() {
	for _, name := range []string{"all", "containers", "images", "all-images", "volumes", "networks", "cache", "dry-run", "force", "stats"} {
		cleanupCmd.Flags().Set(name, "false")
	}
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"all", "containers", "images", "all-images", "volumes", "networks", "cache", "dry-run", "force", "stats"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestDescribePruneTargets(t *testing.T) {
	tests := []struct {
		name string
		opts runtime.PruneOptions
		want string
	}{
		{
			name: "all categories",
			opts: runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, Cache: true},
			want: "stopped non-Tengiz containers, dangling images, unused volumes, unused networks, build cache",
		},
		{
			name: "all images",
			opts: runtime.PruneOptions{Images: true, AllImages: true},
			want: "unused images",
		},
		{
			name: "empty",
			opts: runtime.PruneOptions{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describePruneTargets(tt.opts); got != tt.want {
				t.Errorf("describePruneTargets() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanupForceSkipsPrompt(t *testing.T) {
	old := newHousekeeper
	defer func() { newHousekeeper = old }()
	newHousekeeper = func() (runtime.Housekeeper, error) {
		return runtime.NewStubHousekeeper(), nil
	}
	resetCleanupFlags()

	rootCmd.SetArgs([]string{"cleanup", "--force"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if strings.Contains(output, "Continue?") {
		t.Error("expected --force to skip the confirmation prompt")
	}
}

func TestCleanupDryRunOutput(t *testing.T) {
	old := newHousekeeper
	defer func() { newHousekeeper = old }()
	newHousekeeper = func() (runtime.Housekeeper, error) {
		return runtime.NewStubHousekeeper(), nil
	}
	resetCleanupFlags()

	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	for _, want := range []string{"containers:", "images:", "volumes:", "networks:", "cache:"} {
		if !strings.Contains(output, want) {
			t.Errorf("dry-run output missing %q, got: %s", want, output)
		}
	}
}

func TestCleanupSelectiveContainersOnly(t *testing.T) {
	old := newHousekeeper
	defer func() { newHousekeeper = old }()
	newHousekeeper = func() (runtime.Housekeeper, error) {
		return runtime.NewStubHousekeeper(), nil
	}
	resetCleanupFlags()

	rootCmd.SetArgs([]string{"cleanup", "--force", "--containers"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if strings.Contains(output, "dry run") {
		t.Error("--containers must not trigger the dry-run path")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` / `undefined: newHousekeeper`

- [ ] **Step 3: Register the command in `internal/cli/root.go`**

In `init()`, after `rootCmd.AddCommand(rollbackCmd)` (currently line 65), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 4: Write the implementation**

```go
// internal/cli/cleanup.go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

// newHousekeeper is a test seam; production uses the docker-backed housekeeper.
var newHousekeeper = func() (runtime.Housekeeper, error) {
	return runtime.NewDockerHousekeeper()
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prunes stopped non-Tengiz containers, dangling images, unused volumes and
networks, and the Docker build cache. Tengiz-managed containers are always
protected by their "tengiz-app" label. Use --dry-run to preview what would be
removed without deleting anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		allImages, _ := cmd.Flags().GetBool("all-images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")
		showStats, _ := cmd.Flags().GetBool("stats")

		anySet := containers || images || volumes || networks || cache
		if all || !anySet {
			containers, images, volumes, networks, cache = true, true, true, true, true
		}

		opts := runtime.PruneOptions{
			Containers: containers,
			Images:     images,
			AllImages:  allImages,
			Volumes:    volumes,
			Networks:   networks,
			Cache:      cache,
		}

		if !force && !dryRun {
			fmt.Printf("[tengiz] This will prune %s. Continue? [y/N]: ", describePruneTargets(opts))
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(input)) != "y" {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		hk, err := newHousekeeper()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if dryRun {
			res, err := hk.DryRun(context.Background(), opts)
			if err != nil {
				return err
			}
			printDryRunResult(res)
			return nil
		}

		if showStats {
			if before, err := hk.DiskUsage(context.Background()); err == nil && before != "" {
				fmt.Printf("[tengiz] Docker disk usage before:\n%s\n", before)
			}
		}

		res, err := hk.Prune(context.Background(), opts)
		if err != nil {
			return err
		}
		printPruneResult(res)

		if showStats {
			if after, err := hk.DiskUsage(context.Background()); err == nil && after != "" {
				fmt.Printf("[tengiz] Docker disk usage after:\n%s\n", after)
			}
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("all", false, "prune all resource categories (default when no category flag is set)")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling Docker images")
	cleanupCmd.Flags().Bool("all-images", false, "also remove all unused (not just dangling) images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without deleting anything")
	cleanupCmd.Flags().Bool("force", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Bool("stats", false, "show docker system df before and after cleanup")
}

func describePruneTargets(opts runtime.PruneOptions) string {
	var parts []string
	if opts.Containers {
		parts = append(parts, "stopped non-Tengiz containers")
	}
	if opts.Images {
		if opts.AllImages {
			parts = append(parts, "unused images")
		} else {
			parts = append(parts, "dangling images")
		}
	}
	if opts.Volumes {
		parts = append(parts, "unused volumes")
	}
	if opts.Networks {
		parts = append(parts, "unused networks")
	}
	if opts.Cache {
		parts = append(parts, "build cache")
	}
	return strings.Join(parts, ", ")
}

func printDryRunResult(res runtime.DryRunResult) {
	fmt.Println("[tengiz] dry run - nothing was removed:")
	fmt.Printf("  containers: %d\n", res.Containers)
	fmt.Printf("  images:     %d\n", res.Images)
	fmt.Printf("  volumes:    %d\n", res.Volumes)
	fmt.Printf("  networks:   %d\n", res.Networks)
	fmt.Printf("  cache:      %d\n", res.Cache)
}

func printPruneResult(res runtime.PruneResult) {
	if out := strings.TrimSpace(res.ContainerOutput); out != "" {
		fmt.Printf("[tengiz] containers pruned:\n%s\n", out)
	}
	if out := strings.TrimSpace(res.ImageOutput); out != "" {
		fmt.Printf("[tengiz] images pruned:\n%s\n", out)
	}
	if out := strings.TrimSpace(res.VolumeOutput); out != "" {
		fmt.Printf("[tengiz] volumes pruned:\n%s\n", out)
	}
	if out := strings.TrimSpace(res.NetworkOutput); out != "" {
		fmt.Printf("[tengiz] networks pruned:\n%s\n", out)
	}
	if out := strings.TrimSpace(res.CacheOutput); out != "" {
		fmt.Printf("[tengiz] build cache pruned:\n%s\n", out)
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`

Expected: PASS (6 tests)

- [ ] **Step 6: Build and vet**

Run: `go build -o tengiz . && go vet ./internal/runtime/ ./internal/cli/`

Expected: build succeeds, vet reports no issues

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 5: Documentation (README + feature tracking)

**Files:**
- Modify: `README.md` — Features list (line ~23) and CLI Reference (after the `tengiz rollback` section, ~line 232)
- Modify: `docs/FUTURES_FEATURES.md` — Priority Ranking table row for #6, and the Implemented Features table

**Interfaces:**
- Consumes: the `tengiz cleanup` command + flags from Task 4 (command name, flag names, default behavior)
- Produces: nothing for later tasks (final task)

- [ ] **Step 1: Add the feature bullet to README Features list**

In `README.md`, after the `- **Self-contained**` line (line 23), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes stopped non-Tengiz containers, dangling images, unused volumes/networks, and the build cache. Tengiz-managed containers are always protected via labels.
```

- [ ] **Step 2: Add the CLI Reference section**

In `README.md`, after the `### tengiz rollback <app>` section (ends ~line 236), add:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space. Tengiz-managed containers (labeled `tengiz-app`) are **always protected** — only stopped non-Tengiz containers, dangling images, unused volumes/networks, and the build cache are removed.

| Flag | Description |
|------|-------------|
| `--all` | Prune all categories (default when no category flag is set) |
| `--containers` | Prune stopped non-Tengiz containers |
| `--images` | Prune dangling Docker images |
| `--all-images` | Also remove all unused (not just dangling) images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--cache` | Prune the Docker build cache |
| `--dry-run` | Show what would be removed without deleting anything |
| `--force` | Skip the confirmation prompt (for CI/scripts) |
| `--stats` | Show `docker system df` before and after cleanup |

Examples:
```
tengiz cleanup                  # prune all categories (prompts for confirmation)
tengiz cleanup --dry-run        # preview without deleting
tengiz cleanup --force          # non-interactive, prune everything
tengiz cleanup --images --cache --stats   # prune images + build cache, show disk usage
```

- [ ] **Step 3: Update the Priority Ranking table**

In `docs/FUTURES_FEATURES.md`, change line 19:

From: `| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based \`docker system prune\`. \`tengiz cleanup\`. |`

To: `| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based \`docker system prune\`. \`tengiz cleanup\`. |`

- [ ] **Step 4: Add to the Implemented Features table**

In `docs/FUTURES_FEATURES.md`, in the `### ✅ Implemented Features (Not Pending)` table, add a row:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-01) |
```

- [ ] **Step 5: Run the full test suite**

Run: `go test ./... -count=1`

Expected: PASS (all packages, including the new runtime and cli tests)

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review Checklist

- [ ] **Spec coverage:** The #6 priority entry requires label-based `docker system prune` + a `tengiz cleanup` command. Covered: `tengiz cleanup` (Task 4), label-based protection via `label!=tengiz-app` on container/volume/network prune + list builders (Task 2), and all five resource categories from the spec description (containers, images, volumes, networks + build cache) (Tasks 2-3). `--stats` provides the `docker system df` visibility the spec implies for operators. Periodic scheduling is intentionally out of scope (separate #57 Background Monitoring Scheduler) and noted in the execution handoff below.
- [ ] **Placeholder scan:** Every task contains complete test code and implementation code. No TBD/TODO/"similar to" phrasing. All commands include expected output.
- [ ] **Type consistency:** `PruneOptions{Containers, Images, AllImages, Volumes, Networks, Cache}` is identical across Tasks 1-4. `PruneResult`, `DryRunResult`, `Housekeeper`, `NewDockerHousekeeper`, `NewStubHousekeeper`, `describePruneTargets`, `printDryRunResult`, `printPruneResult` use the same signatures everywhere. `pruneCommand.kind` values (`containers`, `images`, `volumes`, `networks`, `cache`) are matched consistently in `pruneCommands`, `dryRunCommands`, and the `switch` statements in `Prune`/`DryRun`.
- [ ] **Verification note (test isolation):** Cobra/pflag persist flag values across `Execute()` calls on the global `rootCmd`. The Task 4 CLI tests therefore call `resetCleanupFlags()` before each `Execute` to stay order-independent (verified with `go test -shuffle=on`). All implementation and test code in Tasks 1-5 was compiled and executed against a copy of the repo: `go build ./...`, `go vet ./internal/runtime/ ./internal/cli/`, and `go test ./... -count=1` all pass.
