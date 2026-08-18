# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, networks, and build cache, while always protecting Tengiz-managed containers via label-based filtering.

**Architecture:** A new `Prune(ctx, opts) (PruneReport, error)` method on the `runtime.Manager` interface (exec-based `dockerRuntime` impl + no-op `stubManager`). Each resource category maps to a granular `docker <category> prune` command carrying `--filter label!=tengiz-app` so resources labeled `tengiz-app` (every container Tengiz creates) are never touched. Per-category prune commands are used instead of a single `docker system prune` because the `label` filter on `docker system prune` only applies to containers, not volumes/networks/images. The CLI wires a `tengiz cleanup` command to the runtime method with per-category flags; `--all-images` implies `--images`. Pure helper functions (`buildPruneArgs`, `countDeleted`, `parseReclaimedBytes`, `formatBytes`) keep the Docker output parsing unit-testable without a daemon.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface, `os/exec` Docker CLI passthrough (no Docker SDK). No new external dependencies.

## Global Constraints

- No new external dependencies — stdlib + existing `cobra` only
- Docker is invoked via `os/exec` (`docker` CLI must be in PATH), never the Docker SDK
- Never prune resources carrying the `tengiz-app` label — implemented with `--filter label!=tengiz-app` on every category that supports label filters
- Command syntax verified against Docker 28.0.4 (see outputs captured below in Task 2)
- `tengiz cleanup` with no flags prunes **all** categories (matches Coolify's periodic `DockerCleanupJob` semantics); category flags restrict the operation
- `--all-images` implies `--images`; `--all` overrides all category flags
- Keep report printing simple and deterministic: counts per category + total reclaimed space string
- Existing tests must continue to pass; the 4 mock `runtime.Manager` implementations must each gain a `Prune` stub
- Update `README.md`, `AGENTS.md`, and mark feature #6 as ✅ Implemented in `docs/FUTURES_FEATURES.md` (AGENTS.md rule: UI/UX değişikliklerinde README.md ve dokümantasyonu güncelle)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` | **Create.** `PruneOptions`, `PruneReport`, `Any()`, `buildPruneArgs`, `dockerRuntime.Prune`, output parsers (`countDeleted`, `countBuildCache`, `parseReclaimedBytes`, `parseSize`, `formatBytes`) |
| `internal/runtime/prune_test.go` | **Create.** Unit tests for arg builder, parsers, formatter, stub |
| `internal/runtime/runtime.go` | **Modify.** Add `Prune` to `Manager` interface + `stubManager.Prune` |
| `internal/cli/cleanup.go` | **Create.** `tengiz cleanup` cobra command + `cleanupPruneOptions` helper |
| `internal/cli/cleanup_test.go` | **Create.** Tests for command registration, flags, option building |
| `internal/cli/root.go` | **Modify.** Register `cleanupCmd` |
| `internal/cli/root_test.go` | **Modify.** Add `Prune` stub to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | **Modify.** Add `Prune` stub to `mockRuntime` |
| `internal/idle/idle_test.go` | **Modify.** Add `Prune` stub to `mockRuntime` |
| `README.md` | **Modify.** Document `tengiz cleanup` in Features + CLI Reference |
| `AGENTS.md` | **Modify.** Document `tengiz cleanup` in CLI list |
| `docs/FUTURES_FEATURES.md` | **Modify.** Mark feature #6 ✅ Implemented |

---

### Task 1: Runtime prune primitives (types, arg builder, interface, stubs)

**Files:**
- Create: `internal/runtime/prune.go`
- Modify: `internal/runtime/runtime.go` — add `Prune` to `Manager` interface + stub method
- Modify: `internal/cli/root_test.go:69-100` — add `Prune` stub to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:15-35` — add `Prune` stub to `mockRuntime`
- Modify: `internal/idle/idle_test.go:14-34` — add `Prune` stub to `mockRuntime`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{Containers, Images, AllImages, Volumes, Networks, BuildCache bool}`, `runtime.PruneReport{Containers, Images, Volumes, Networks, BuildCache int; Space string}`, `runtime.PruneOptions.Any() bool`, `runtime.buildPruneArgs(category string, opts PruneOptions) []string`, `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildPruneArgsContainers(t *testing.T) {
	got := buildPruneArgs("containers", PruneOptions{})
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneArgs(containers) = %v, want %v", got, want)
	}
}

func TestBuildPruneArgsImages(t *testing.T) {
	got := buildPruneArgs("images", PruneOptions{})
	want := []string{"image", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneArgs(images dangling) = %v, want %v", got, want)
	}

	got = buildPruneArgs("images", PruneOptions{AllImages: true})
	want = []string{"image", "prune", "-f", "--filter", "label!=tengiz-app", "-a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneArgs(images all) = %v, want %v", got, want)
	}
}

func TestBuildPruneArgsVolumes(t *testing.T) {
	got := buildPruneArgs("volumes", PruneOptions{})
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneArgs(volumes) = %v, want %v", got, want)
	}
}

func TestBuildPruneArgsNetworks(t *testing.T) {
	got := buildPruneArgs("networks", PruneOptions{})
	want := []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneArgs(networks) = %v, want %v", got, want)
	}
}

func TestBuildPruneArgsBuildCache(t *testing.T) {
	got := buildPruneArgs("build-cache", PruneOptions{})
	want := []string{"builder", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneArgs(build-cache) = %v, want %v", got, want)
	}

	got = buildPruneArgs("build-cache", PruneOptions{AllImages: true})
	want = []string{"builder", "prune", "-f", "-a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildPruneArgs(build-cache all) = %v, want %v", got, want)
	}
}

func TestBuildPruneArgsUnknownCategory(t *testing.T) {
	if got := buildPruneArgs("bogus", PruneOptions{}); got != nil {
		t.Errorf("buildPruneArgs(bogus) = %v, want nil", got)
	}
}

func TestPruneOptionsAny(t *testing.T) {
	if PruneOptions{}.Any() {
		t.Error("empty PruneOptions should have Any() == false")
	}
	if !PruneOptions{Containers: true}.Any() {
		t.Error("PruneOptions{Containers:true} should have Any() == true")
	}
	if !PruneOptions{AllImages: true}.Any() {
		t.Error("PruneOptions{AllImages:true} should have Any() == true")
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.Containers != 0 || report.Images != 0 || report.Space != "" {
		t.Errorf("stub Prune() returned unexpected report: %+v", report)
	}
}

func TestStubPruneSatisfiesInterface(t *testing.T) {
	var iface Manager = NewStub()
	if _, err := iface.Prune(context.Background(), PruneOptions{Networks: true}); err != nil {
		t.Fatalf("Prune() via interface error = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestPruneOptionsAny|TestStubPrune" -v -count=1`

Expected: FAIL — compile error `undefined: PruneOptions`, `undefined: buildPruneArgs`. (The `Manager` interface will also fail to compile because `stubManager` won't have `Prune` yet.)

- [ ] **Step 3: Write minimal implementation in `internal/runtime/prune.go`**

```go
package runtime

// PruneOptions selects which Docker resource categories to clean up.
type PruneOptions struct {
	Containers bool // prune stopped containers
	Images     bool // prune unused images (dangling only unless AllImages)
	AllImages  bool // also remove all unused images, not just dangling
	Volumes    bool // prune unused volumes
	Networks   bool // prune unused networks
	BuildCache bool // prune build cache
}

// PruneReport summarizes a cleanup run.
type PruneReport struct {
	Containers int    // number of stopped containers pruned
	Images     int    // number of images pruned
	Volumes    int    // number of volumes pruned
	Networks   int    // number of networks pruned
	BuildCache int    // number of build cache entries pruned
	Space      string // total reclaimed space (human readable, e.g. "15.6MB")
}

// Any reports whether at least one category is selected.
func (o PruneOptions) Any() bool {
	return o.Containers || o.Images || o.Volumes || o.Networks || o.BuildCache
}

// tengizAppLabelFilter excludes resources carrying the tengiz-app label
// (i.e. every container Tengiz manages) from pruning.
const tengizAppLabelFilter = "label!=tengiz-app"

// buildPruneArgs returns the docker CLI arguments for pruning one category.
// Category names: "containers", "images", "volumes", "networks", "build-cache".
func buildPruneArgs(category string, opts PruneOptions) []string {
	switch category {
	case "containers":
		return []string{"container", "prune", "-f", "--filter", tengizAppLabelFilter}
	case "images":
		args := []string{"image", "prune", "-f", "--filter", tengizAppLabelFilter}
		if opts.AllImages {
			args = append(args, "-a")
		}
		return args
	case "volumes":
		return []string{"volume", "prune", "-f", "--filter", tengizAppLabelFilter}
	case "networks":
		return []string{"network", "prune", "-f", "--filter", tengizAppLabelFilter}
	case "build-cache":
		args := []string{"builder", "prune", "-f"}
		if opts.AllImages {
			args = append(args, "-a")
		}
		return args
	default:
		return nil
	}
}
```

- [ ] **Step 4: Add `Prune` to the `Manager` interface and stub in `internal/runtime/runtime.go`**

In the `Manager` interface, after the `Run` method (line 48), add:

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

In `stubManager`, after the `Run` stub (line 121), add:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}
```

- [ ] **Step 5: Update the three other `runtime.Manager` mocks so the packages compile**

In `internal/cli/root_test.go`, after the `Run` method on `mockRTForDeploy` (line 100), add:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

In `internal/proxy/proxy_test.go`, after the `Run` method on `mockRuntime` (line 35), add:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

In `internal/idle/idle_test.go`, after the `Run` method on `mockRuntime` (line 34), add:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestPruneOptionsAny|TestStubPrune" -v -count=1`

Expected: PASS (6 test functions)

- [ ] **Step 7: Verify whole repo still builds and all tests pass**

Run: `go build ./...`

Expected: Build succeeds (all mocks updated).

Run: `go test ./... -v -count=1`

Expected: All PASS (proxy tests are slow ~2s each, idle tests are time-sensitive — both expected).

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/runtime.go internal/runtime/prune_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add Prune method and prune primitives to runtime"
```

---

### Task 2: Docker prune implementation and output parsing

**Files:**
- Modify: `internal/runtime/prune.go` — add `dockerRuntime.Prune` + parser helpers
- Test: `internal/runtime/prune_test.go` — add parser/formatter tests

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport`, `buildPruneArgs` from Task 1
- Produces: `(r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`, `countDeleted(output, kind string) int`, `countBuildCache(output string) int`, `parseReclaimedBytes(output string) int64`, `parseSize(s string) int64`, `formatBytes(b int64) string`

**Verified Docker output formats (Docker 28.0.4):**
- Container/volume/network prune:
  ```
  Deleted Containers:
  <container-id>

  Total reclaimed space: 0B
  ```
- Image prune:
  ```
  Deleted Images:
  untagged: test-img:latest
  deleted: sha256:abc123

  Total reclaimed space: 1.2kB
  ```
- Builder prune (note tab-separated `Total:` line, not "Total reclaimed space:"):
  ```
  ID                               RECLAIMABLE SIZE      LAST ACCESSED
  mqcxr7u5yp5lfjhkyzc1k89yf              true   71B       Less than a second ago

  Total:	71B
  ```

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/prune_test.go`:

```go
func TestCountDeletedContainers(t *testing.T) {
	output := `Deleted Containers:
1e5343053854ed9f2661d9507e9ae5735a5a8fb22db3339f08d9428871fac089
0a3bde59ec54ebfa1fed69cb86608ba73d4b4560b19b2a81e67e2ba371e4c2f0

Total reclaimed space: 0B
`
	if got := countDeleted(output, "Containers"); got != 2 {
		t.Errorf("countDeleted(containers) = %d, want 2", got)
	}
}

func TestCountDeletedNoItems(t *testing.T) {
	if got := countDeleted("Total reclaimed space: 0B\n", "Containers"); got != 0 {
		t.Errorf("countDeleted(no items) = %d, want 0", got)
	}
}

func TestCountDeletedImages(t *testing.T) {
	output := `Deleted Images:
untagged: test-img:latest
deleted: sha256:abc123
deleted: sha256:def456

Total reclaimed space: 1.2kB
`
	if got := countDeleted(output, "Images"); got != 2 {
		t.Errorf("countDeleted(images) = %d, want 2", got)
	}
}

func TestCountDeletedVolumes(t *testing.T) {
	output := `Deleted Volumes:
3f2a9c1d

Total reclaimed space: 0B
`
	if got := countDeleted(output, "Volumes"); got != 1 {
		t.Errorf("countDeleted(volumes) = %d, want 1", got)
	}
}

func TestCountDeletedNetworks(t *testing.T) {
	output := `Deleted Networks:
my-network

Total reclaimed space: 0B
`
	if got := countDeleted(output, "Networks"); got != 1 {
		t.Errorf("countDeleted(networks) = %d, want 1", got)
	}
}

func TestCountBuildCache(t *testing.T) {
	output := `ID                               RECLAIMABLE SIZE      LAST ACCESSED
mqcxr7u5yp5lfjhkyzc1k89yf              true   71B       Less than a second ago
kjab7tugrez3175tdk33gjxqo              true   0B        Less than a second ago
Total:	71B
`
	if got := countBuildCache(output); got != 2 {
		t.Errorf("countBuildCache = %d, want 2", got)
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	if got := parseReclaimedBytes("Total reclaimed space: 12.34MB\n"); got != 12340000 {
		t.Errorf("parseReclaimedBytes(MB) = %d, want 12340000", got)
	}
	if got := parseReclaimedBytes("Total:	71B\n"); got != 71 {
		t.Errorf("parseReclaimedBytes(tab) = %d, want 71", got)
	}
	if got := parseReclaimedBytes("Total reclaimed space: 0B\n"); got != 0 {
		t.Errorf("parseReclaimedBytes(0B) = %d, want 0", got)
	}
	multi := "Total reclaimed space: 500B\nTotal reclaimed space: 1.5kB\n"
	if got := parseReclaimedBytes(multi); got != 2000 {
		t.Errorf("parseReclaimedBytes(multi) = %d, want 2000", got)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{1500, "1.50kB"},
		{2000000, "2.00MB"},
		{3000000000, "3.00GB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestCountDeleted|TestCountBuildCache|TestParseReclaimedBytes|TestFormatBytes" -v -count=1`

Expected: FAIL — compile error `undefined: countDeleted`, `undefined: countBuildCache`, `undefined: parseReclaimedBytes`, `undefined: formatBytes`

- [ ] **Step 3: Add the `dockerRuntime.Prune` implementation and parser helpers to `internal/runtime/prune.go`**

Append to `internal/runtime/prune.go` and add the imports `context`, `fmt`, `os/exec`, `regexp`, `strconv` (prune.go currently imports nothing):

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

Implementation:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	report := PruneReport{}

	var categories []string
	if opts.Containers {
		categories = append(categories, "containers")
	}
	if opts.Images {
		categories = append(categories, "images")
	}
	if opts.Volumes {
		categories = append(categories, "volumes")
	}
	if opts.Networks {
		categories = append(categories, "networks")
	}
	if opts.BuildCache {
		categories = append(categories, "build-cache")
	}

	var reclaimed int64
	for _, cat := range categories {
		args := buildPruneArgs(cat, opts)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out))
		}
		output := string(out)
		switch cat {
		case "containers":
			report.Containers = countDeleted(output, "Containers")
		case "images":
			report.Images = countDeleted(output, "Images")
		case "volumes":
			report.Volumes = countDeleted(output, "Volumes")
		case "networks":
			report.Networks = countDeleted(output, "Networks")
		case "build-cache":
			report.BuildCache = countBuildCache(output)
		}
		reclaimed += parseReclaimedBytes(output)
	}
	report.Space = formatBytes(reclaimed)
	return report, nil
}

// countDeleted counts the items a docker <kind> prune command removed. docker
// prints the header "Deleted <Kind>:" followed by one ID per line, then a
// summary line. For images the removable items are the "deleted:" lines (the
// "untagged:" lines are tag references, not image deletions).
func countDeleted(output, kind string) int {
	lines := strings.Split(output, "\n")
	in := false
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Deleted "+kind+":") {
			in = true
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			in = false
			continue
		}
		if in {
			if kind == "Images" {
				if strings.HasPrefix(line, "deleted:") {
					count++
				}
			} else if line != "" {
				count++
			}
		}
	}
	return count
}

// countBuildCache counts the entries a docker builder prune removed. The
// output is a table whose data rows start with the cache entry ID; the header
// row starts with "ID" and the footer starts with "Total".
func countBuildCache(output string) int {
	lines := strings.Split(output, "\n")
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "ID") || strings.HasPrefix(line, "Total") {
			continue
		}
		count++
	}
	return count
}

// reclaimedLine matches docker prune summary lines. Both formats occur in the
// wild: "Total reclaimed space: X" (container/image/volume/network prune) and
// "Total:\tX" (builder prune).
var reclaimedLine = regexp.MustCompile(`(?:Total reclaimed space|Total):\s*(\S+)`)

// parseReclaimedBytes sums the reclaimed sizes printed by one or more docker
// prune commands. Sizes use SI units (e.g. "1.2kB", "34MB", "2GB").
func parseReclaimedBytes(output string) int64 {
	var total int64
	for _, m := range reclaimedLine.FindAllStringSubmatch(output, -1) {
		total += parseSize(m[1])
	}
	return total
}

func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.') {
		i++
	}
	num := s[:i]
	if num == "" {
		return 0
	}
	val, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	switch unit := strings.ToUpper(strings.TrimSpace(s[i:])); unit {
	case "B":
		return int64(val)
	case "K", "KB":
		return int64(val * 1e3)
	case "M", "MB":
		return int64(val * 1e6)
	case "G", "GB":
		return int64(val * 1e9)
	case "T", "TB":
		return int64(val * 1e12)
	default:
		return int64(val)
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1e12:
		return fmt.Sprintf("%.2fTB", float64(b)/1e12)
	case b >= 1e9:
		return fmt.Sprintf("%.2fGB", float64(b)/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.2fMB", float64(b)/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.2fkB", float64(b)/1e3)
	default:
		return fmt.Sprintf("%dB", b)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestCountDeleted|TestCountBuildCache|TestParseReclaimedBytes|TestFormatBytes" -v -count=1`

Expected: PASS (6 new test functions)

- [ ] **Step 5: Run the full runtime test package**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Smoke-test against the real Docker daemon**

Run:

```bash
go build -o tengiz .
docker run -d --name cleanup-smoke-test alpine sleep 3600
docker stop cleanup-smoke-test
```

Then verify label protection manually (should NOT delete the tengiz-labeled container):

```bash
docker run -d --name tengiz-protected --label tengiz-app=smoke alpine sleep 3600
docker stop tengiz-protected
docker container prune -f --filter "label!=tengiz-app"
docker ps -a --filter name=tengiz-protected --format "{{.Names}}"   # must still print tengiz-protected
```

Expected: `tengiz-protected` still listed after prune (label filter works). Clean up:

```bash
docker rm -f cleanup-smoke-test tengiz-protected
rm -f tengiz
```

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: implement docker prune with label-based protection and output parsing"
```

---

### Task 3: CLI `tengiz cleanup` command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go` — register `cleanupCmd` in `init()`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `Manager.Prune(ctx, opts)`, `PruneOptions`, `PruneReport`, `PruneOptions.Any()` from Tasks 1-2
- Produces: `cleanupCmd *cobra.Command` (Use: `cleanup`), `cleanupPruneOptions(cmd *cobra.Command) runtime.PruneOptions`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
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

func TestCleanupFlagsRegistered(t *testing.T) {
	for _, name := range []string{"all", "containers", "images", "all-images", "volumes", "networks", "build-cache"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func resetCleanupFlags(cmd *cobra.Command) {
	for _, name := range []string{"all", "containers", "images", "all-images", "volumes", "networks", "build-cache"} {
		cmd.Flags().Set(name, "false")
	}
}

func TestCleanupPruneOptionsDefaultAll(t *testing.T) {
	resetCleanupFlags(cleanupCmd)
	opts := cleanupPruneOptions(cleanupCmd)
	want := runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	if opts != want {
		t.Errorf("default opts = %+v, want %+v", opts, want)
	}
}

func TestCleanupPruneOptionsContainersOnly(t *testing.T) {
	resetCleanupFlags(cleanupCmd)
	cleanupCmd.Flags().Set("containers", "true")
	opts := cleanupPruneOptions(cleanupCmd)
	if !opts.Containers || opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("--containers opts = %+v, want only Containers", opts)
	}
}

func TestCleanupPruneOptionsAllImagesImpliesImages(t *testing.T) {
	resetCleanupFlags(cleanupCmd)
	cleanupCmd.Flags().Set("all-images", "true")
	opts := cleanupPruneOptions(cleanupCmd)
	if !opts.Images || !opts.AllImages {
		t.Errorf("--all-images opts = %+v, want Images+AllImages", opts)
	}
	if opts.Containers || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("--all-images should not select other categories: %+v", opts)
	}
}

func TestCleanupPruneOptionsAllFlag(t *testing.T) {
	resetCleanupFlags(cleanupCmd)
	cleanupCmd.Flags().Set("all", "true")
	opts := cleanupPruneOptions(cleanupCmd)
	if !opts.Any() {
		t.Errorf("--all opts = %+v, want all categories", opts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — compile error `undefined: cleanupCmd`, `undefined: cleanupPruneOptions`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prune unused Docker resources (stopped containers, unused images,
volumes, networks, and build cache) to reclaim disk space.

With no flags, every category is pruned. Category flags restrict the operation
to specific resource types. Resources carrying the 'tengiz-app' label (every
container Tengiz manages) are always protected.

Examples:
  tengiz cleanup                       # prune every category
  tengiz cleanup --containers          # only stopped containers
  tengiz cleanup --images              # only dangling images
  tengiz cleanup --images --all-images # also remove all unused images
  tengiz cleanup --build-cache         # only build cache`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupPruneOptions(cmd)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Prune(context.Background(), opts)
		if err != nil {
			return err
		}

		if opts.Containers {
			fmt.Printf("[tengiz] containers pruned: %d\n", report.Containers)
		}
		if opts.Images {
			fmt.Printf("[tengiz] images pruned: %d\n", report.Images)
		}
		if opts.Volumes {
			fmt.Printf("[tengiz] volumes pruned: %d\n", report.Volumes)
		}
		if opts.Networks {
			fmt.Printf("[tengiz] networks pruned: %d\n", report.Networks)
		}
		if opts.BuildCache {
			fmt.Printf("[tengiz] build cache pruned: %d\n", report.BuildCache)
		}
		fmt.Printf("[tengiz] cleanup complete: %s reclaimed\n", report.Space)
		return nil
	},
}

// cleanupPruneOptions translates the cleanup command's flags into
// runtime.PruneOptions. With no category flag (or --all) every category is
// selected; --all-images also selects --images.
func cleanupPruneOptions(cmd *cobra.Command) runtime.PruneOptions {
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	allImages, _ := cmd.Flags().GetBool("all-images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")

	opts := runtime.PruneOptions{
		Containers: containers,
		Images:     images || allImages,
		AllImages:  allImages,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
	}
	if all || !opts.Any() {
		opts = runtime.PruneOptions{
			Containers: true,
			Images:     true,
			Volumes:    true,
			Networks:   true,
			BuildCache: true,
		}
	}
	return opts
}

func init() {
	cleanupCmd.Flags().Bool("all", false, "prune every category (default when no category flag is set)")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune unused images (dangling only)")
	cleanupCmd.Flags().Bool("all-images", false, "also remove all unused images, not just dangling (with --images)")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
}
```

- [ ] **Step 4: Register the command in `internal/cli/root.go`**

In `init()`, after `rootCmd.AddCommand(buildLogsCmd)` (line 66), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS (6 test functions)

- [ ] **Step 6: Verify the whole repo builds and all tests pass**

Run: `go build ./...`

Expected: Build succeeds

Run: `go test ./... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Documentation and feature tracking

**Files:**
- Modify: `README.md` — Features list + CLI Reference
- Modify: `AGENTS.md` — CLI list + `runtime.Manager` row
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 ✅ Implemented
- Test: none (docs only) — verification is `go build ./...` + `go test ./...`

**Interfaces:**
- Consumes: the `tengiz cleanup` command from Task 3
- Produces: updated user-facing docs

- [ ] **Step 1: Add `tengiz cleanup` to the README Features list**

In `README.md`, after the "Deployment history" bullet (line 20), add:

```markdown
- **Docker housekeeping** — Reclaim disk space with `tengiz cleanup` (containers, images, volumes, networks, build cache). Tengiz-managed containers are protected via the `tengiz-app` label filter.
```

- [ ] **Step 2: Add a `tengiz cleanup` section to the README CLI Reference**

In `README.md`, after the `### tengiz ps` section (line 150), add:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--all` | Prune every category (default when no category flag is set) |
| `--containers` | Prune stopped containers |
| `--images` | Prune unused images (dangling only) |
| `--all-images` | Also remove all unused images, not just dangling (with `--images`) |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune build cache |

With no flags, every category is pruned. Category flags restrict the operation. Containers, images, volumes, and networks carrying the `tengiz-app` label are always protected — Tengiz-managed containers are never pruned.

Run it manually or schedule it with cron (e.g. nightly at 3am) via a crontab line:

`0 3 * * * tengiz cleanup`
```

- [ ] **Step 3: Update `AGENTS.md`**

In the CLI list (after the `tengiz ps` line, line 43), add:

```
tengiz cleanup [--containers|--images|--volumes|--networks|--build-cache] → prune unused Docker resources (label-protected, all categories by default)
```

In the `runtime.Manager` row (line 15), change the sentence to include the new method:

```
| `runtime.Manager` | Interface for container lifecycle. `NewDocker()` = exec-based impl, `NewStub()` = test mock. Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup, `Prune` for `tengiz cleanup`. `ContainerName(name, env)` helper. |
```

- [ ] **Step 4: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 priority table (line 19), change:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the "✅ Implemented Features (Not Pending)" table, add a row after the Nixpacks row (line 388):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-18) |
```

- [ ] **Step 5: Final verification**

Run: `go build ./...`

Expected: Build succeeds

Run: `go vet ./...`

Expected: No issues

Run: `go test ./... -v -count=1`

Expected: All PASS (proxy tests ~2s each, idle tests time-sensitive — both expected)

- [ ] **Step 6: Manual smoke test of the full command**

Run:

```bash
go build -o tengiz .
docker run -d --name smoke-label-protect --label tengiz-app=smoke alpine sleep 3600
docker stop smoke-label-protect
./tengiz cleanup --containers
docker ps -a --filter name=smoke-label-protect --format "{{.Names}}"   # must still print smoke-label-protect
./tengiz cleanup                        # full cleanup, prints per-category counts + reclaimed space
docker rm -f smoke-label-protect
rm -f tengiz
```

Expected: `smoke-label-protect` survives `--containers` prune (label protection); the full `cleanup` prints lines like `[tengiz] cleanup complete: 0B reclaimed`.

- [ ] **Step 7: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark Docker Housekeeping implemented"
```

---

## Self-Review

**Spec coverage (from `docs/FUTURES_FEATURES.md` feature #6):**
- "Label-based `docker system prune`" → Task 2 uses `--filter label!=tengiz-app` on every category that supports it, protecting Tengiz-managed containers. Per-category prune commands are used instead of a single `docker system prune` because the `label` filter on `docker system prune` only applies to containers (verified against Docker 28.0.4); the per-category approach extends label protection to volumes/networks/images as well. ✅
- "`tengiz cleanup`" command → Task 3. ✅
- "kullanılmayan volume, network, container ve image'leri temizleme" (clean unused volumes/networks/containers/images) → Task 2 (`Prune` runs all categories). Build cache added too, matching Coolify's `DockerCleanupJob`. ✅
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" (label-based filtering protects Tengiz-managed containers) → `tengizAppLabelFilter = "label!=tengiz-app"`, smoke-tested in Task 2 Step 6 and Task 4 Step 6. ✅
- "periyodik temizleme" (periodic cleaning) → Tengiz is a stateless CLI with no daemon; the cron example in README (Task 4 Step 2) covers periodic scheduling. ✅

**Placeholder scan:** Every step contains complete code or exact commands. No "TBD"/"TODO"/"implement later"/"similar to Task N" patterns. The parser tests embed the exact Docker 28.0.4 output formats captured during planning. ✅

**Type consistency:**
- `PruneOptions` fields (`Containers`, `Images`, `AllImages`, `Volumes`, `Networks`, `BuildCache`) used identically in Tasks 1, 2, 3. ✅
- `PruneReport` fields (`Containers`, `Images`, `Volumes`, `Networks`, `BuildCache int`, `Space string`) consistent between `dockerRuntime.Prune` (Task 2) and CLI printing (Task 3). ✅
- `buildPruneArgs(category string, opts PruneOptions) []string` — same signature in Task 1 (defined) and Task 2 (consumed). ✅
- `Any()` used in Task 1 (defined) and Task 3 (default-all logic). ✅
- All four `Manager` mocks updated with the identical `Prune` stub in Task 1. ✅
- `cleanupPruneOptions(cmd *cobra.Command) runtime.PruneOptions` — defined and consumed in Task 3. ✅