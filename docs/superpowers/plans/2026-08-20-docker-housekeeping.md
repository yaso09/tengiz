# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, volumes, networks, and BuildKit cache — always protecting Tengiz-managed containers (including stopped scale-to-zero ones) via label-based filtering — and reports how much disk space was reclaimed.

**Architecture:** Extend the `runtime.Manager` interface with `Cleanup(ctx, CleanupOptions) (CleanupReport, error)`. The `dockerRuntime` implementation composes category-specific prune jobs (`docker container/image/volume/network/builder prune`) through pure, unit-testable helper functions, executes each via `os/exec` (no Docker SDK — matching the rest of the repo), and parses the "Total reclaimed space" line plus removed-item counts from stdout. The Cobra `cleanup` command resolves options from flags via a pure `cleanupOptionsFromFlags(cmd)` function, calls `rt.Cleanup`, and prints the report through a pure `printCleanupReport(report)` formatter. Container pruning uses `--filter label!=tengiz-app` so no Tengiz container is ever removed.

**Tech Stack:** Go 1.26.0, Cobra, `os/exec` + `docker` CLI (no SDK), stdlib `regexp`/`strconv`. No new external dependencies.

## Global Constraints

- All containers managed by Tengiz carry the `tengiz-app=<appname>` label (`labelKey = "tengiz-app"` in `internal/runtime/docker.go:76`). Cleanup must NEVER remove them — stopped containers are expected (scale-to-zero) and get cold-started by the proxy.
- Container names: `tengiz-<name>` (production) / `tengiz-<name>-<env>` (non-production); env label `tengiz-env=<env>`. Versioned deploy containers add `tengiz-deployment=<suffix>`.
- Image tags: `tengiz-apps/<appname>:<env>-<deploymentID>` and `tengiz-apps/<appname>:<env>-latest`.
- Docker is invoked via `exec.CommandContext(ctx, "docker", args...)`; docker must be installed separately. No Docker SDK.
- Adding a method to `runtime.Manager` breaks every hand-written mock — all of these MUST gain the new method in the same commit or the package won't compile:
  - `internal/cli/root_test.go` → `mockRTForDeploy`
  - `internal/proxy/proxy_test.go` → `mockRuntime`
  - `internal/idle/idle_test.go` → `mockRuntime`
  - (`internal/preview/manager_test.go` and `internal/gitdeploy/deployer_test.go` use `runtime.NewStub()`, so they are fine automatically.)
- Commands are registered in `init()` in `internal/cli/root.go`; flags use `cmd.Flags().GetBool(...)`.
- Test commands: `go test ./... -v -count=1`, `go vet ./...`, `go build -o tengiz .`.
- Commit per task, matching repo style `feat(scope): message`. Only commit the intended files.
- README must be updated when the CLI surface changes (repo rule).

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupReport` types; add `Cleanup` to `Manager` interface; stub impl on `stubManager` |
| `internal/runtime/cleanup.go` | `dockerRuntime.Cleanup` + `runPrune`; pure helpers: `containerPruneArgs`, `imagePruneArgs`, `volumePruneArgs`, `networkPruneArgs`, `builderPruneArgs`, `cleanupPruneJobs`, `parsePruneOutput`, `isShortID`, `isCacheRow`, `parseSize`, `formatBytes` |
| `internal/runtime/cleanup_test.go` | Stub test + unit tests for every pure helper |
| `internal/cli/root.go` | `cleanupCmd` (Cobra), flags, registration, `cleanupOptionsFromFlags`, `printCleanupReport` |
| `internal/cli/root_test.go` | `Cleanup` on `mockRTForDeploy`; registration/flag/options/report tests |
| `internal/proxy/proxy_test.go` | `Cleanup` on `mockRuntime` (compile fix) |
| `internal/idle/idle_test.go` | `Cleanup` on `mockRuntime` (compile fix) |
| `README.md` | Document `tengiz cleanup` in Quickstart + CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

No new files created. Changes touch 9 existing files.

---

### Task 1: Cleanup types, Manager interface method, stub, and mock updates

**Files:**
- Modify: `internal/runtime/runtime.go:18-49` — add types after `RunOptions`, add interface method, add stub method
- Modify: `internal/runtime/cleanup_test.go` — add stub test
- Modify: `internal/proxy/proxy_test.go:35` — add `Cleanup` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:34` — add `Cleanup` to `mockRuntime`
- Modify: `internal/cli/root_test.go:100` — add `Cleanup` to `mockRTForDeploy`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions` (struct with `All, Containers, Images, Volumes, Networks, BuildCache bool`), `runtime.CleanupReport` (struct with `ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved, BuildCacheRemoved int`, `TotalFreed string`), interface method `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)` on `runtime.Manager`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{All: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.TotalFreed != "" {
		t.Fatalf("expected empty TotalFreed from stub, got %q", report.TotalFreed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -count=1`
Expected: FAIL — `m.Cleanup undefined (type Manager has no field or method Cleanup)` (compile error), and `internal/proxy`, `internal/idle`, `internal/cli` also fail to compile because their mocks no longer satisfy `runtime.Manager`.

- [ ] **Step 3: Implement the interface + stub + mock updates**

In `internal/runtime/runtime.go`, after the `RunOptions` struct (line 29), add:

```go
type CleanupOptions struct {
	All        bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type CleanupReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCacheRemoved int
	TotalFreed        string
}
```

In the `Manager` interface (runtime.go), after `KeepLastNImages` (line 36), add:

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)
```

On `stubManager`, after the `KeepLastNImages` stub (runtime.go:117-119), add:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
}
```

In `internal/proxy/proxy_test.go`, after the `Run` method (line 35), add:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return runtime.CleanupReport{}, nil
}
```

In `internal/idle/idle_test.go`, after the `Run` method (line 34), add:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return runtime.CleanupReport{}, nil
}
```

In `internal/cli/root_test.go`, after the `Run` method (line 100), add:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return runtime.CleanupReport{}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -count=1`
Expected: PASS for all packages. `TestStubCleanup` passes and every package compiles.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface"
```

---

### Task 2: Prune arg builders and output-parsing helpers (pure functions)

**Files:**
- Modify: `internal/runtime/cleanup.go` — add imports `regexp`, `strconv`; add all pure helpers
- Modify: `internal/runtime/cleanup_test.go` — add unit tests for every helper

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupReport`, `labelKey` (const in `internal/runtime/docker.go:76`)
- Produces (all package-private, used by Task 3):
  - `containerPruneArgs() []string` → `["container", "prune", "-f", "--filter", "label!=tengiz-app"]`
  - `imagePruneArgs() []string` → `["image", "prune", "-a", "-f"]`
  - `volumePruneArgs() []string` → `["volume", "prune", "-f"]`
  - `networkPruneArgs() []string` → `["network", "prune", "-f"]`
  - `builderPruneArgs() []string` → `["builder", "prune", "-f"]`
  - `cleanupPruneJobs(opts CleanupOptions) [][]string` — ordered category job list honoring `All`/per-category flags
  - `pruneResult` struct `{ removed int; freed uint64 }`
  - `parsePruneOutput(out []byte) pruneResult`
  - `isShortID(s string) bool`, `isCacheRow(s string) bool`
  - `parseSize(s string) uint64`, `formatBytes(b uint64) string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestContainerPruneArgs(t *testing.T) {
	got := containerPruneArgs()
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("containerPruneArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("containerPruneArgs() = %v, want %v", got, want)
		}
	}
}

func TestCategoryPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		got  func() []string
		want []string
	}{
		{"image", imagePruneArgs, []string{"image", "prune", "-a", "-f"}},
		{"volume", volumePruneArgs, []string{"volume", "prune", "-f"}},
		{"network", networkPruneArgs, []string{"network", "prune", "-f"}},
		{"builder", builderPruneArgs, []string{"builder", "prune", "-f"}},
	}
	for _, tt := range tests {
		got := tt.got()
		if len(got) != len(tt.want) {
			t.Fatalf("%sPruneArgs() = %v, want %v", tt.name, got, tt.want)
		}
		for i := range tt.want {
			if got[i] != tt.want[i] {
				t.Fatalf("%sPruneArgs() = %v, want %v", tt.name, got, tt.want)
			}
		}
	}
}

func TestCleanupPruneJobsAll(t *testing.T) {
	jobs := cleanupPruneJobs(CleanupOptions{All: true})
	if len(jobs) != 5 {
		t.Fatalf("expected 5 jobs, got %d", len(jobs))
	}
	wantOrder := []string{"container", "image", "volume", "network", "builder"}
	for i, job := range jobs {
		if job[0] != wantOrder[i] {
			t.Fatalf("job %d = %v, want first element %q", i, job, wantOrder[i])
		}
	}
}

func TestCleanupPruneJobsSelective(t *testing.T) {
	jobs := cleanupPruneJobs(CleanupOptions{Containers: true, Volumes: true})
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	if jobs[0][0] != "container" || jobs[1][0] != "volume" {
		t.Fatalf("unexpected jobs: %v", jobs)
	}
}

func TestCleanupPruneJobsNone(t *testing.T) {
	jobs := cleanupPruneJobs(CleanupOptions{})
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs, got %d", len(jobs))
	}
}

func TestIsShortID(t *testing.T) {
	valid := []string{"a1b2c3d4e5f6", "0123456789ab", "abcdefabcdefabcdefabcdef"}
	invalid := []string{"", "abc", "ABCDEF123456", "a1b2c3d4e5f6g7h8i9", "deleted: sha256:abc", "a1b2c3d4e5f6  true"}
	for _, s := range valid {
		if !isShortID(s) {
			t.Errorf("isShortID(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if isShortID(s) {
			t.Errorf("isShortID(%q) = true, want false", s)
		}
	}
}

func TestIsCacheRow(t *testing.T) {
	if !isCacheRow("ab12cd34ef56  true  1.5GB  5 minutes ago") {
		t.Error("expected cache row to match")
	}
	if isCacheRow("ID  RECLAIMABLE  SIZE  LAST ACCESSED") {
		t.Error("expected header row NOT to match")
	}
	if isCacheRow("ab12cd34ef56") {
		t.Error("expected short line NOT to match")
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want uint64
	}{
		{"", 0},
		{"0B", 0},
		{"1.234GB", 1234000000},
		{"500MB", 500000000},
		{"123.4kB", 123400},
		{"12.5MiB", 12.5 * 1024 * 1024},
		{"1.5GiB", uint64(1.5 * 1024 * 1024 * 1024)},
		{"garbage", 0},
	}
	for _, tt := range tests {
		if got := parseSize(tt.in); got != tt.want {
			t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0B"},
		{1024, "1.00kB"},
		{1048576, "1.00MB"},
		{1 << 30, "1.00GB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParsePruneOutputContainer(t *testing.T) {
	out := []byte("Deleted Containers:\na1b2c3d4e5f6\nab12cd34ef56\n\nTotal reclaimed space: 1.234GB\n")
	res := parsePruneOutput(out)
	if res.removed != 2 {
		t.Errorf("removed = %d, want 2", res.removed)
	}
	if res.freed != 1234000000 {
		t.Errorf("freed = %d, want 1234000000", res.freed)
	}
}

func TestParsePruneOutputImage(t *testing.T) {
	out := []byte("Untagged: tengiz-apps/myapp:production-123\nDeleted Images:\ndeleted: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\ndeleted: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n\nTotal reclaimed space: 500MB\n")
	res := parsePruneOutput(out)
	if res.removed != 2 {
		t.Errorf("removed = %d, want 2", res.removed)
	}
	if res.freed != 500000000 {
		t.Errorf("freed = %d, want 500000000", res.freed)
	}
}

func TestParsePruneOutputBuilder(t *testing.T) {
	out := []byte("ID            RECLAIMABLE  SIZE       LAST ACCESSED\nab12cd34ef56  true         1.5GB      5 minutes ago\ncd34ef56ab12  true         500MB      1 hour ago\n\nTotal:  2GB\n")
	res := parsePruneOutput(out)
	if res.removed != 2 {
		t.Errorf("removed = %d, want 2", res.removed)
	}
	if res.freed != 2000000000 {
		t.Errorf("freed = %d, want 2000000000", res.freed)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	res := parsePruneOutput([]byte(""))
	if res.removed != 0 || res.freed != 0 {
		t.Fatalf("expected zero result, got %+v", res)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestContainerPruneArgs|TestCategoryPruneArgs|TestCleanupPruneJobs|TestIsShortID|TestIsCacheRow|TestParseSize|TestFormatBytes|TestParsePruneOutput' -count=1`
Expected: FAIL — compile error: `undefined: containerPruneArgs` (and the other helpers).

- [ ] **Step 3: Implement the pure helpers**

In `internal/runtime/cleanup.go`, replace the import block (lines 3-10) with:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)
```

Append the following to `internal/runtime/cleanup.go`:

```go
func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=" + labelKey}
}

func imagePruneArgs() []string {
	return []string{"image", "prune", "-a", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func builderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func cleanupPruneJobs(opts CleanupOptions) [][]string {
	var jobs [][]string
	if opts.All || opts.Containers {
		jobs = append(jobs, containerPruneArgs())
	}
	if opts.All || opts.Images {
		jobs = append(jobs, imagePruneArgs())
	}
	if opts.All || opts.Volumes {
		jobs = append(jobs, volumePruneArgs())
	}
	if opts.All || opts.Networks {
		jobs = append(jobs, networkPruneArgs())
	}
	if opts.All || opts.BuildCache {
		jobs = append(jobs, builderPruneArgs())
	}
	return jobs
}

type pruneResult struct {
	removed int
	freed   uint64
}

var totalSpaceRe = regexp.MustCompile(`(?m)^(?:Total reclaimed space|Total):\s*(.+)$`)

func parsePruneOutput(out []byte) pruneResult {
	var res pruneResult
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if m := totalSpaceRe.FindStringSubmatch(line); m != nil {
			res.freed = parseSize(m[1])
			continue
		}
		if isShortID(line) || strings.HasPrefix(line, "deleted:") || isCacheRow(line) {
			res.removed++
		}
	}
	return res
}

func isShortID(s string) bool {
	if len(s) < 12 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func isCacheRow(s string) bool {
	fields := strings.Fields(s)
	if len(fields) < 4 {
		return false
	}
	return isShortID(fields[0])
}

var sizeRe = regexp.MustCompile(`^\s*([0-9.]+)\s*(b|kb|mb|gb|tb|kib|mib|gib|tib)?\s*$`)

func parseSize(s string) uint64 {
	m := sizeRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return 0
	}
	num, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	var mult float64
	switch m[2] {
	case "", "b":
		mult = 1
	case "kb":
		mult = 1000
	case "mb":
		mult = 1e6
	case "gb":
		mult = 1e9
	case "tb":
		mult = 1e12
	case "kib":
		mult = 1024
	case "mib":
		mult = 1024 * 1024
	case "gib":
		mult = 1024 * 1024 * 1024
	case "tib":
		mult = 1024 * 1024 * 1024 * 1024
	default:
		mult = 1
	}
	return uint64(num * mult)
}

func formatBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.2fkB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -count=1`
Expected: PASS for all tests, including the new helper tests.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add docker prune arg builders and output parsing"
```

---

### Task 3: dockerRuntime.Cleanup implementation

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `runPrune` and `Cleanup` on `dockerRuntime`
- Modify: `internal/runtime/cleanup_test.go` — add compile-time interface assertion

**Interfaces:**
- Consumes: `cleanupPruneJobs`, `parsePruneOutput`, `parseSize`, `formatBytes` (from Task 2), `CleanupOptions`/`CleanupReport` (from Task 1)
- Produces: `dockerRuntime` now satisfies `runtime.Manager`; later tasks call `rt.Cleanup(ctx, opts)` and read `CleanupReport`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestDockerRuntimeImplementsManager(t *testing.T) {
	var m Manager = &dockerRuntime{}
	if m == nil {
		t.Fatal("dockerRuntime does not implement Manager")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestDockerRuntimeImplementsManager -count=1`
Expected: FAIL — compile error: `*dockerRuntime does not implement Manager (missing method Cleanup)`.

- [ ] **Step 3: Implement Cleanup on dockerRuntime**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) runPrune(ctx context.Context, args []string) (pruneResult, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return pruneResult{}, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return parsePruneOutput(out), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	var report CleanupReport
	var freed uint64
	for _, args := range cleanupPruneJobs(opts) {
		res, err := r.runPrune(ctx, args)
		if err != nil {
			return report, err
		}
		freed += res.freed
		switch args[0] {
		case "container":
			report.ContainersRemoved = res.removed
		case "image":
			report.ImagesRemoved = res.removed
		case "volume":
			report.VolumesRemoved = res.removed
		case "network":
			report.NetworksRemoved = res.removed
		case "builder":
			report.BuildCacheRemoved = res.removed
		}
	}
	report.TotalFreed = formatBytes(freed)
	return report, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -count=1`
Expected: PASS for all packages.

- [ ] **Step 5: Manual verification against a real Docker daemon (optional but recommended)**

Run: `go build -o tengiz . && ./tengiz cleanup --images`
Expected: outputs `[tengiz] cleanup complete` with counts and `space reclaimed: ...`. Confirm no Tengiz containers/images are removed.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker cleanup"
```

---

### Task 4: CLI `cleanup` command + docs + feature doc update

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, flags, registration in `init()`, `cleanupOptionsFromFlags`, `printCleanupReport`
- Modify: `internal/cli/root_test.go` — registration/flag/options/report tests
- Modify: `README.md` — Quickstart list (line ~99) + new CLI Reference section
- Modify: `docs/FUTURES_FEATURES.md:19` — mark feature #6 as ✅ Implemented

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.NewDocker()`, `rt.Cleanup(ctx, opts)` (all from Tasks 1-3)
- Produces: `tengiz cleanup` command; pure `cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions`; pure `printCleanupReport(report runtime.CleanupReport) string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

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

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "build-cache"} {
		f := cleanupCmd.Flags().Lookup(flag)
		if f == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupOptionsFromFlagsDefaultAll(t *testing.T) {
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.All {
		t.Error("expected All=true when no category flags are set")
	}
}

func TestCleanupOptionsFromFlagsSelective(t *testing.T) {
	cleanupCmd.Flags().Set("images", "true")
	defer cleanupCmd.Flags().Set("images", "false")
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if opts.All {
		t.Error("expected All=false when a category flag is set")
	}
	if !opts.Images {
		t.Error("expected Images=true")
	}
	if opts.Containers {
		t.Error("expected Containers=false")
	}
}

func TestPrintCleanupReportEmpty(t *testing.T) {
	out := printCleanupReport(runtime.CleanupReport{})
	if !strings.Contains(out, "nothing to clean") {
		t.Errorf("expected 'nothing to clean', got: %q", out)
	}
}

func TestPrintCleanupReportPopulated(t *testing.T) {
	out := printCleanupReport(runtime.CleanupReport{
		ContainersRemoved: 2,
		ImagesRemoved:     5,
		VolumesRemoved:    1,
		TotalFreed:        "1.23GB",
	})
	if !strings.Contains(out, "containers removed: 2") {
		t.Errorf("missing container count: %q", out)
	}
	if !strings.Contains(out, "images removed: 5") {
		t.Errorf("missing image count: %q", out)
	}
	if !strings.Contains(out, "volumes removed: 1") {
		t.Errorf("missing volume count: %q", out)
	}
	if !strings.Contains(out, "space reclaimed: 1.23GB") {
		t.Errorf("missing reclaimed space: %q", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup' -count=1`
Expected: FAIL — compile error: `undefined: cleanupCmd` / `undefined: cleanupOptionsFromFlags` / `undefined: printCleanupReport`.

- [ ] **Step 3: Implement the CLI command**

In `internal/cli/root.go`, in `init()` (after the `secretCmd` registration, line 69), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

and after the existing flag registrations (line 88), add:

```go
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune unused and dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune BuildKit build cache")
```

Add the command and its helpers after `runCmd` (after line 1162):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to free disk space",
	Long: `Prunes unused Docker containers, images, volumes, networks, and build cache.

Containers managed by Tengiz (labeled tengiz-app=*) are always kept, including
stopped ones (scale-to-zero containers are cold-started on demand).

By default all categories are pruned. Use flags to select specific categories:
  tengiz cleanup
  tengiz cleanup --containers --images
  tengiz cleanup --build-cache`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupOptionsFromFlags(cmd)
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		report, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		fmt.Print(printCleanupReport(report))
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	opts := runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
	}
	if !containers && !images && !volumes && !networks && !buildCache {
		opts.All = true
	}
	return opts
}

func printCleanupReport(report runtime.CleanupReport) string {
	var b strings.Builder
	b.WriteString("[tengiz] cleanup complete\n")
	printed := false
	if report.ContainersRemoved > 0 {
		fmt.Fprintf(&b, "  containers removed: %d\n", report.ContainersRemoved)
		printed = true
	}
	if report.ImagesRemoved > 0 {
		fmt.Fprintf(&b, "  images removed: %d\n", report.ImagesRemoved)
		printed = true
	}
	if report.VolumesRemoved > 0 {
		fmt.Fprintf(&b, "  volumes removed: %d\n", report.VolumesRemoved)
		printed = true
	}
	if report.NetworksRemoved > 0 {
		fmt.Fprintf(&b, "  networks removed: %d\n", report.NetworksRemoved)
		printed = true
	}
	if report.BuildCacheRemoved > 0 {
		fmt.Fprintf(&b, "  build cache removed: %d\n", report.BuildCacheRemoved)
		printed = true
	}
	if !printed {
		b.WriteString("  nothing to clean\n")
	}
	if report.TotalFreed != "" && report.TotalFreed != "0B" {
		fmt.Fprintf(&b, "  space reclaimed: %s\n", report.TotalFreed)
	}
	return strings.TrimSuffix(b.String(), "\n")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -count=1`
Expected: PASS for all packages, including the new `TestCleanup*` tests.

- [ ] **Step 5: Run vet and build**

Run: `go vet ./...`
Expected: no output (clean).

Run: `go build -o tengiz .`
Expected: builds successfully.

- [ ] **Step 6: Update README.md**

In the Quickstart section (around line 99), after the `tengiz ps` line, add:

```markdown
tengiz cleanup       # prune unused Docker resources (containers, images, volumes, networks, build cache)
```

In the CLI Reference section, add a new `### tengiz cleanup` section after `### tengiz ps` (line ~151):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to free disk space. Tengiz-managed containers (labeled `tengiz-app=*`)
are always kept, including stopped ones (scale-to-zero containers are cold-started on demand).

By default all categories are pruned. Use flags to select specific categories:

| Flag | Category |
|------|----------|
| `--containers` | stopped containers not managed by Tengiz |
| `--images` | unused and dangling images |
| `--volumes` | unused volumes |
| `--networks` | unused networks |
| `--build-cache` | BuildKit build cache |

```bash
tengiz cleanup                # prune everything
tengiz cleanup --containers --images   # prune only containers and images
tengiz cleanup --build-cache  # prune only BuildKit cache
```
```

- [ ] **Step 7: Update docs/FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md` line 19, change:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go README.md docs/FUTURES_FEATURES.md
git commit -m "feat(cli): add tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage (feature #6 Docker Housekeeping):**
- "kullanılmayan volume, network, container ve image'leri temizleme" → `Cleanup` prunes containers, images, volumes, networks, and build cache (Task 3).
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → `containerPruneArgs` uses `--filter label!=tengiz-app` (Task 2).
- "`tengiz cleanup` komutu eklenebilir" → `cleanupCmd` (Task 4).
- "Disk alanını tüketir" (disk space motivation) → report prints `space reclaimed` (Tasks 3-4).
- AGENTS.md rule "Her değişiklikte test ekle/güncelle, testleri geçir, sonra commit et" → every task ends with tests passing + commit.
- AGENTS.md rule "UI/UX değişikliklerinde README.md ve dokümantasyonu güncelle" → Task 4 Step 6.

**2. Placeholder scan:** No TBD/TODO/placeholder. Every code step shows complete code. Test commands include exact expected output.

**3. Type consistency:** `CleanupOptions`/`CleanupReport` fields defined once (Task 1) and used identically in Tasks 2-4. Helper names (`containerPruneArgs`, `parsePruneOutput`, `cleanupPruneJobs`, `runPrune`) are consistent across Tasks 2-3. `printCleanupReport`/`cleanupOptionsFromFlags` defined in Task 4 and used only there. `labelKey` is reused from `docker.go:76` (already defined). `formatBytes`/`parseSize` are only used inside `runtime` — `TotalFreed` is passed to the CLI as a pre-formatted string, so no cross-package formatting leak.