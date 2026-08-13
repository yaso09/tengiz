# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused networks, build cache, optionally volumes and unused images) while protecting all Tengiz-managed containers via label-based filtering.

**Architecture:** A new `Prune(ctx, opts)` method on the existing `runtime.Manager` interface shells out to the `docker <kind> prune` CLI (matching the existing exec-based pattern in `internal/runtime/docker.go`). Every prune runs with `--filter label!=tengiz-app` so stopped Tengiz containers (including scale-to-zero idle ones, which carry the `tengiz-app` label) are never removed. The CLI command `tengiz cleanup` translates flags into `PruneOptions`, prompts for confirmation unless `--force` is given, and prints a parsed summary report. Pure parsing helpers (`parsePruneOutput`, `parseSize`) are unit-tested; the full prune flow is verified with a fake `docker` binary placed on `PATH`.

**Tech Stack:** Go 1.26, stdlib only (`os/exec`, `regexp`, `strconv`, `strings`, `bufio`), existing Cobra CLI, existing `runtime.Manager` interface. No new external dependencies.

## Global Constraints

- No new external dependencies — stdlib + existing `cobra`/`viper` only
- Docker CLI must be installed at runtime (existing requirement); all pruning shells out to `docker` via `os/exec`
- Tengiz-managed containers (labeled `tengiz-app=<name>`) are **never** pruned — every container prune runs `--filter label!=tengiz-app`
- Default `tengiz cleanup` (no category flags) prunes: stopped non-Tengiz containers, dangling images, unused networks, builder cache — **never** volumes
- `--volumes` explicitly enables volume pruning; volumes are never touched by default
- `--all` additionally prunes unused (non-dangling) images older than `--until` (default `24h`); the current deployment image of every app is always protected because it is referenced by its container
- A failed category returns partial results plus an error; the CLI prints the report then the error
- Go module `github.com/yaso09/tengiz`, Go 1.26.0
- Verification commands: `go build ./...`, `go vet ./...`, `go test ./... -v -count=1`
- Periodic/scheduled cleanup is **out of scope** (tracked separately as feature #57 "Background Monitoring Scheduler"); this plan delivers the manual `tengiz cleanup` command only
- Feature #6 in `docs/FUTURES_FEATURES.md` must be marked `✅ Implemented` with the current date

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` (create) | `PruneOptions`, `PruneReport` types; `parsePruneOutput`, `parseSize` helpers; `dockerRuntime.Prune` + per-category prune methods |
| `internal/runtime/runtime.go` (modify) | Add `Prune` to `Manager` interface; add `stubManager.Prune` |
| `internal/runtime/prune_test.go` (create) | Unit tests for parsers; fake-docker integration test for `Prune` |
| `internal/proxy/proxy_test.go` (modify) | Add `Prune` to `mockRuntime` |
| `internal/idle/idle_test.go` (modify) | Add `Prune` to `mockRuntime` |
| `internal/cli/root_test.go` (modify) | Add `Prune` to `mockRTForDeploy` |
| `internal/cli/cmd_cleanup.go` (create) | `cleanupCmd` Cobra command, `cleanupFlags`, `buildPruneOptions`, `printPruneReport`, `formatBytes`, `isTTY` |
| `internal/cli/root.go` (modify) | Register `cleanupCmd` and its flags in `init()` |
| `internal/cli/cmd_cleanup_test.go` (create) | CLI command registration, flag, options, and report-output tests |
| `README.md` (modify) | Document `tengiz cleanup` in the CLI Reference |
| `docs/FUTURES_FEATURES.md` (modify) | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Prune types + output-parsing helpers

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing (pure package-internal helpers)
- Produces: `runtime.PruneOptions{Containers, Images, Volumes, Networks, BuildCache, All, Until bool|string}`, `runtime.PruneReport{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved, BuildCacheRemoved, SpaceReclaimedBytes uint64}`, `parsePruneOutput(output string) (count uint64, space uint64)`, `parseSize(value, unit string) uint64`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		value string
		unit  string
		want  uint64
	}{
		{"2", "GB", 2000000000},
		{"2.5", "GB", 2500000000},
		{"123", "B", 123},
		{"1", "GiB", 1073741824},
		{"512", "kB", 512000},
		{"1.5", "MiB", 1572864},
		{"abc", "GB", 0},
		{"4.5", "XX", 0},
	}
	for _, tt := range tests {
		if got := parseSize(tt.value, tt.unit); got != tt.want {
			t.Errorf("parseSize(%q, %q) = %d, want %d", tt.value, tt.unit, got, tt.want)
		}
	}
}

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantCount uint64
		wantSpace uint64
	}{
		{
			name: "container prune output",
			output: `Deleted Containers:
abc123def456

Total reclaimed space: 0 B`,
			wantCount: 1,
			wantSpace: 0,
		},
		{
			name: "image prune output",
			output: `Deleted Images:
untagged: tengiz-apps/foo:latest@sha256:aaa
deleted: sha256:bbb

Total reclaimed space: 2 GB`,
			wantCount: 2,
			wantSpace: 2000000000,
		},
		{
			name:      "empty output",
			output:    "",
			wantCount: 0,
			wantSpace: 0,
		},
		{
			name: "headers only",
			output: `Deleted Networks:

Total reclaimed space: 0 B`,
			wantCount: 0,
			wantSpace: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, space := parsePruneOutput(tt.output)
			if count != tt.wantCount {
				t.Errorf("parsePruneOutput() count = %d, want %d", count, tt.wantCount)
			}
			if space != tt.wantSpace {
				t.Errorf("parsePruneOutput() space = %d, want %d", space, tt.wantSpace)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParseSize|TestParsePruneOutput' -v`
Expected: FAIL — compile error `undefined: parseSize` and `undefined: parsePruneOutput`

- [ ] **Step 3: Write minimal implementation**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"regexp"
	"strconv"
	"strings"
)

type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	All        bool
	Until      string
}

type PruneReport struct {
	ContainersRemoved   uint64
	ImagesRemoved       uint64
	VolumesRemoved      uint64
	NetworksRemoved     uint64
	BuildCacheRemoved   uint64
	SpaceReclaimedBytes uint64
}

var spaceReclaimedRe = regexp.MustCompile(`(?i)total reclaimed space:\s*([0-9.]+)\s*(TiB|TB|GiB|GB|MiB|MB|KiB|kB|KB|B)`)

var sizeMultipliers = map[string]uint64{
	"B":   1,
	"kB":  1000,
	"KB":  1000,
	"KiB": 1024,
	"MB":  1000000,
	"MiB": 1024 * 1024,
	"GB":  1000000000,
	"GiB": 1024 * 1024 * 1024,
	"TB":  1000000000000,
	"TiB": 1024 * 1024 * 1024 * 1024,
}

func parsePruneOutput(output string) (count uint64, space uint64) {
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := spaceReclaimedRe.FindStringSubmatch(line); m != nil {
			space = parseSize(m[1], m[2])
			continue
		}
		if strings.HasPrefix(line, "Deleted ") {
			continue
		}
		count++
	}
	return count, space
}

func parseSize(value, unit string) uint64 {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	mult, ok := sizeMultipliers[unit]
	if !ok {
		return 0
	}
	return uint64(f * float64(mult))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestParseSize|TestParsePruneOutput' -v`
Expected: PASS — both tests report `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: add docker prune output parsing helpers"
```

---

### Task 2: Manager interface + `dockerRuntime.Prune` implementation

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (add `Prune` to `Manager` interface)
- Modify: `internal/runtime/runtime.go:117-119` (add `stubManager.Prune` after `KeepLastNImages` stub)
- Modify: `internal/runtime/prune.go` (append `Prune` implementation)
- Modify: `internal/proxy/proxy_test.go:34` (add `Prune` to `mockRuntime`)
- Modify: `internal/idle/idle_test.go:33` (add `Prune` to `mockRuntime`)
- Modify: `internal/cli/root_test.go:99` (add `Prune` to `mockRTForDeploy`)
- Test: `internal/runtime/prune_test.go` (append fake-docker integration test)

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport`, `parsePruneOutput` (from Task 1)
- Produces: `Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)` — implemented on `*dockerRuntime`; all three existing mock runtimes (`proxy.mockRuntime`, `idle.mockRuntime`, `cli.mockRTForDeploy`) and `stubManager` gain a no-op implementation

- [ ] **Step 1: Add `Prune` to the `Manager` interface**

In `internal/runtime/runtime.go`, after line 36 (`KeepLastNImages(ctx context.Context, appName string, n int) error`):

```go
	Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)
```

- [ ] **Step 2: Write the failing integration test**

Append to `internal/runtime/prune_test.go`:

```go
func TestPruneWithFakeDocker(t *testing.T) {
	binDir := t.TempDir()
	logFile := binDir + "/calls.log"

	script := `#!/bin/sh
echo "$@" >> "` + logFile + `"
case "$1" in
  container)
    echo "Deleted Containers:"
    echo "abc123def456"
    echo ""
    echo "Total reclaimed space: 0 B"
    ;;
  image)
    echo "Deleted Images:"
    echo "untagged: tengiz-apps/foo:latest@sha256:aaa"
    echo "deleted: sha256:bbb"
    echo ""
    echo "Total reclaimed space: 2 GB"
    ;;
  volume)
    echo "Deleted Volumes:"
    echo "myvolume"
    echo ""
    echo "Total reclaimed space: 12 MB"
    ;;
  network)
    echo "Deleted Networks:"
    echo "mynetwork"
    ;;
  builder)
    echo "Deleted build cache objects:"
    echo "deleted: sha256:ccc"
    echo ""
    echo "Total reclaimed space: 100 MB"
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(binDir+"/docker", []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	rt, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	report, err := rt.Prune(context.Background(), PruneOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", report.ContainersRemoved)
	}
	if report.ImagesRemoved != 2 {
		t.Errorf("ImagesRemoved = %d, want 2", report.ImagesRemoved)
	}
	if report.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", report.VolumesRemoved)
	}
	if report.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", report.NetworksRemoved)
	}
	if report.BuildCacheRemoved != 1 {
		t.Errorf("BuildCacheRemoved = %d, want 1", report.BuildCacheRemoved)
	}
	wantSpace := uint64(2000000000) + 12000000 + 100000000
	if report.SpaceReclaimedBytes != wantSpace {
		t.Errorf("SpaceReclaimedBytes = %d, want %d", report.SpaceReclaimedBytes, wantSpace)
	}

	calls, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read calls.log: %v", err)
	}
	if !strings.Contains(string(calls), "label!=tengiz-app") {
		t.Errorf("prune calls missing label protection filter: %s", calls)
	}
}
```

Update the imports in `internal/runtime/prune_test.go` to:

```go
import (
	"context"
	"os"
	"strings"
	"testing"
)
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestPruneWithFakeDocker -v`
Expected: FAIL — compile error `cannot use &dockerRuntime{} (value of type *dockerRuntime) as Manager: missing method Prune` (the interface now requires `Prune`, which the dockerRuntime and mocks don't implement yet)

- [ ] **Step 4: Implement `Prune` on `dockerRuntime`**

Append to `internal/runtime/prune.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	report := &PruneReport{}
	var firstErr error

	if opts.Containers {
		count, space, err := r.pruneContainers(ctx)
		report.ContainersRemoved = count
		report.SpaceReclaimedBytes += space
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if opts.Images {
		count, space, err := r.pruneImages(ctx, opts)
		report.ImagesRemoved = count
		report.SpaceReclaimedBytes += space
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if opts.Volumes {
		count, space, err := r.pruneVolumes(ctx)
		report.VolumesRemoved = count
		report.SpaceReclaimedBytes += space
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if opts.Networks {
		count, space, err := r.pruneNetworks(ctx)
		report.NetworksRemoved = count
		report.SpaceReclaimedBytes += space
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if opts.BuildCache {
		count, space, err := r.pruneBuildCache(ctx)
		report.BuildCacheRemoved = count
		report.SpaceReclaimedBytes += space
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return report, firstErr
}

func (r *dockerRuntime) runPrune(ctx context.Context, kind string, args []string) (uint64, uint64, error) {
	full := append([]string{kind, "prune", "-f"}, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("docker %s prune: %w\n%s", kind, err, string(out))
	}
	count, space := parsePruneOutput(string(out))
	return count, space, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context) (uint64, uint64, error) {
	return r.runPrune(ctx, "container", []string{"--filter", "label!=" + labelKey})
}

func (r *dockerRuntime) pruneImages(ctx context.Context, opts PruneOptions) (uint64, uint64, error) {
	if opts.All {
		until := opts.Until
		if until == "" {
			until = "24h"
		}
		return r.runPrune(ctx, "image", []string{"-a", "--filter", "until=" + until, "--filter", "label!=" + labelKey})
	}
	return r.runPrune(ctx, "image", []string{"--filter", "dangling=true"})
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context) (uint64, uint64, error) {
	return r.runPrune(ctx, "volume", []string{"--filter", "label!=" + labelKey})
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context) (uint64, uint64, error) {
	return r.runPrune(ctx, "network", []string{"--filter", "label!=" + labelKey})
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context) (uint64, uint64, error) {
	return r.runPrune(ctx, "builder", nil)
}
```

Update the imports in `internal/runtime/prune.go` to:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)
```

- [ ] **Step 5: Add the stub implementation**

In `internal/runtime/runtime.go`, after the `KeepLastNImages` stub (line 119):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	return &PruneReport{}, nil
}
```

- [ ] **Step 6: Update the three mock runtimes so the test suite compiles**

In `internal/proxy/proxy_test.go`, after line 34 (`KeepLastNImages`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) { return &runtime.PruneReport{}, nil }
```

In `internal/idle/idle_test.go`, after line 33 (`KeepLastNImages`):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) { return &runtime.PruneReport{}, nil }
```

In `internal/cli/root_test.go`, after line 99 (`KeepLastNImages`):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) { return &runtime.PruneReport{}, nil }
```

- [ ] **Step 7: Run the integration test to verify it passes**

Run: `go test ./internal/runtime/ -run TestPruneWithFakeDocker -v`
Expected: PASS — `TestPruneWithFakeDocker` reports `ok`

- [ ] **Step 8: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... -v -count=1`
Expected: build succeeds, `go vet` reports no issues, all tests PASS (including the newly-updated mock compile in proxy/idle/cli packages)

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go internal/runtime/runtime.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: add docker prune support to runtime manager"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cmd_cleanup.go`
- Modify: `internal/cli/root.go:34-89` (register `cleanupCmd` and flags in `init()`)
- Test: `internal/cli/cmd_cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.Manager.Prune`, `runtime.PruneOptions`, `runtime.PruneReport` (from Task 2)
- Produces: `cleanupCmd *cobra.Command`, `cleanupFlags{containers, images, volumes, networks, buildCache, all bool}`, `buildPruneOptions(f cleanupFlags) runtime.PruneOptions`, `printPruneReport(report *runtime.PruneReport, w io.Writer)`, `formatBytes(b uint64) string`, `isTTY(f *os.File) bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cmd_cleanup_test.go`:

```go
package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
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
	for _, name := range []string{"all", "volumes", "containers", "images", "networks", "build-cache", "force"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestBuildPruneOptions(t *testing.T) {
	tests := []struct {
		name string
		f    cleanupFlags
		want runtime.PruneOptions
	}{
		{
			name: "no category flags uses safe defaults",
			f:    cleanupFlags{},
			want: runtime.PruneOptions{Containers: true, Images: true, Networks: true, BuildCache: true},
		},
		{
			name: "volumes only",
			f:    cleanupFlags{volumes: true},
			want: runtime.PruneOptions{Volumes: true},
		},
		{
			name: "build-cache only",
			f:    cleanupFlags{buildCache: true},
			want: runtime.PruneOptions{BuildCache: true},
		},
		{
			name: "all with default images",
			f:    cleanupFlags{all: true},
			want: runtime.PruneOptions{Containers: true, Images: true, Networks: true, BuildCache: true, All: true},
		},
		{
			name: "all requires images",
			f:    cleanupFlags{all: true, images: true},
			want: runtime.PruneOptions{Images: true, All: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneOptions(tt.f)
			if got != tt.want {
				t.Errorf("buildPruneOptions() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPrintPruneReport(t *testing.T) {
	report := &runtime.PruneReport{
		ContainersRemoved:   2,
		ImagesRemoved:       3,
		VolumesRemoved:      1,
		NetworksRemoved:     0,
		BuildCacheRemoved:   1,
		SpaceReclaimedBytes: 2147483648,
	}
	var buf bytes.Buffer
	printPruneReport(report, &buf)
	out := buf.String()
	for _, want := range []string{
		"[tengiz] cleanup complete:",
		"containers: 2",
		"images: 3",
		"volumes: 1",
		"networks: 0",
		"build cache: 1",
		"space reclaimed: 2.0 GB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		b    uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1610612736, "1.5 GB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.b); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.b, got, tt.want)
		}
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--help"})
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("cleanup --help failed: %v", err)
	}
	for _, flag := range []string{"--all", "--volumes", "--containers", "--images", "--networks", "--build-cache", "--force"} {
		if !strings.Contains(buf.String(), flag) {
			t.Errorf("help text missing flag %q", flag)
		}
	}
}

func TestCleanupCmdRuns(t *testing.T) {
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	var force, all bool
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		force, _ = cmd.Flags().GetBool("force")
		all, _ = cmd.Flags().GetBool("all")
		return nil
	}
	rootCmd.SetArgs([]string{"cleanup", "--force", "--all"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !force {
		t.Error("--force flag not parsed")
	}
	if !all {
		t.Error("--all flag not parsed")
	}
}

func TestIsTTYOnPipe(t *testing.T) {
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	if isTTY(r) {
		t.Error("pipe reported as a TTY")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestBuildPruneOptions|TestPrintPruneReport|TestFormatBytes|TestIsTTY' -v`
Expected: FAIL — compile errors: `undefined: cleanupCmd`, `undefined: cleanupFlags`, `undefined: buildPruneOptions`, `undefined: printPruneReport`, `undefined: formatBytes`, `undefined: isTTY`

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cmd_cleanup.go`:

```go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

type cleanupFlags struct {
	containers bool
	images     bool
	volumes    bool
	networks   bool
	buildCache bool
	all        bool
}

func buildPruneOptions(f cleanupFlags) runtime.PruneOptions {
	specific := f.containers || f.images || f.volumes || f.networks || f.buildCache
	opts := runtime.PruneOptions{All: f.all}
	if specific {
		opts.Containers = f.containers
		opts.Images = f.images
		opts.Volumes = f.volumes
		opts.Networks = f.networks
		opts.BuildCache = f.buildCache
	} else {
		opts.Containers = true
		opts.Images = true
		opts.Networks = true
		opts.BuildCache = true
	}
	if !opts.Images {
		opts.All = false
	}
	return opts
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, networks, volumes, build cache)",
	Long: "Removes stopped containers, dangling images, unused networks and build cache while keeping all " +
		"Tengiz-managed containers (labeled tengiz-app). Use --volumes to also prune unused volumes and " +
		"--all to also remove unused images. Pass --force to skip the confirmation prompt.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		f := cleanupFlags{
			containers: mustBool(cmd, "containers"),
			images:     mustBool(cmd, "images"),
			volumes:    mustBool(cmd, "volumes"),
			networks:   mustBool(cmd, "networks"),
			buildCache: mustBool(cmd, "build-cache"),
			all:        mustBool(cmd, "all"),
		}
		opts := buildPruneOptions(f)

		if !force && isTTY(os.Stdin) {
			fmt.Print("This will remove unused Docker resources (stopped non-Tengiz containers, dangling images, unused networks). Continue? [y/N] ")
			answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		report, err := rt.Prune(context.Background(), opts)
		if report != nil {
			printPruneReport(report, os.Stdout)
		}
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		return nil
	},
}

func mustBool(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name)
	return v
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func printPruneReport(report *runtime.PruneReport, w io.Writer) {
	fmt.Fprintln(w, "[tengiz] cleanup complete:")
	fmt.Fprintf(w, "  containers: %d\n", report.ContainersRemoved)
	fmt.Fprintf(w, "  images: %d\n", report.ImagesRemoved)
	fmt.Fprintf(w, "  volumes: %d\n", report.VolumesRemoved)
	fmt.Fprintf(w, "  networks: %d\n", report.NetworksRemoved)
	fmt.Fprintf(w, "  build cache: %d\n", report.BuildCacheRemoved)
	fmt.Fprintf(w, "  space reclaimed: %s\n", formatBytes(report.SpaceReclaimedBytes))
}

func formatBytes(b uint64) string {
	switch {
	case b >= 1<<40:
		return fmt.Sprintf("%.1f TB", float64(b)/float64(1<<40))
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
```

- [ ] **Step 4: Register the command and flags**

In `internal/cli/root.go`, inside `init()`, after line 65 (`rootCmd.AddCommand(volumeCmd)`), add the registration:

```go
	rootCmd.AddCommand(cleanupCmd)
```

And after line 88 (`webhookCmd.Flags().String("config", ...)`), add the flags:

```go
	cleanupCmd.Flags().BoolP("all", "a", false, "also remove all unused images (not just dangling)")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers (default when no category flag given)")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images (default when no category flag given)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks (default when no category flag given)")
	cleanupCmd.Flags().Bool("build-cache", false, "prune builder cache (default when no category flag given)")
	cleanupCmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestBuildPruneOptions|TestPrintPruneReport|TestFormatBytes|TestIsTTY' -v`
Expected: PASS — all cleanup tests report `ok`

- [ ] **Step 6: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... -v -count=1`
Expected: build succeeds, `go vet` reports no issues, all tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cmd_cleanup.go internal/cli/cmd_cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Documentation updates

**Files:**
- Modify: `README.md:103-131` (CLI Reference — add a `### \`tengiz cleanup\`` section after the `deploy` section)
- Modify: `docs/FUTURES_FEATURES.md:19` (mark feature #6 implemented) and `docs/FUTURES_FEATURES.md:377-381` (add a Status line to the detailed feature entry)

**Interfaces:**
- Consumes: `cleanupCmd` behavior from Task 3
- Produces: nothing

- [ ] **Step 1: Document the command in README.md**

In `README.md`, after the `### \`tengiz deploy [directory]\`` section (ends at line 131), insert:

```markdown
### `tengiz cleanup [--all] [--volumes] [--containers] [--images] [--networks] [--build-cache] [--force]`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz (default) |
| `--images` | Prune dangling images (default) |
| `--networks` | Prune unused networks (default) |
| `--build-cache` | Prune builder cache (default) |
| `--volumes` | Also prune unused volumes |
| `--all`, `-a` | Also remove unused images (older than `--until`, default 24h) |
| `--force`, `-f` | Skip the confirmation prompt |

Default behavior removes stopped non-Tengiz containers, dangling images, unused networks, and build cache. Containers labeled `tengiz-app` are always preserved. Pass `--force` (or run non-interactively) to skip the confirmation.
```

- [ ] **Step 2: Mark feature #6 as implemented in the priority ranking**

In `docs/FUTURES_FEATURES.md` line 19, change:

```
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 3: Add a Status line to the detailed feature entry**

In `docs/FUTURES_FEATURES.md`, in the `## Docker Housekeeping (Otomatik Temizlik)` section (around line 377-381), after the `- **Detected:** 2026-07-14` line, insert:

```markdown
- **Status:** ✅ Implemented (2026-08-13)
```

- [ ] **Step 4: Verify no formatting breaks**

Run: `git diff --stat && git diff docs/FUTURES_FEATURES.md | head -40`
Expected: only the two edited lines in `FUTURES_FEATURES.md` plus the new README section; no table rows mangled

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Final Verification

After all tasks are committed, run:

```bash
go build ./... && go vet ./... && go test ./... -v -count=1
```

Expected: build succeeds, `go vet` clean, all tests PASS. Then verify the binary exposes the command:

```bash
go build -o tengiz .
./tengiz cleanup --help
```

Expected: help text lists `--all`, `--volumes`, `--containers`, `--images`, `--networks`, `--build-cache`, `--force`.
