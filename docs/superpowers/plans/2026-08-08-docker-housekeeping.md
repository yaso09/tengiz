# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely prunes unused Docker containers, images, volumes, networks, and build cache while keeping Tengiz-managed containers and rollback-eligible images intact.

**Architecture:** The `runtime` package already owns all Docker CLI access and the label constants (`tengiz-app`, `tengiz-env`). A new `Cleanup(ctx, opts)` method on the `runtime.Manager` interface runs `docker <object> prune -f` per category. Stopped containers created by other tools are pruned with `--filter label!=tengiz-app` so Tengiz containers are never touched. Images are only pruned as *dangling* images (`docker image prune -f`); after that, per-app retention via the existing `KeepLastNImages(ctx, app, n)` keeps the last N versioned images so rollback always has its image. A pure `executeCleanup(ctx, run, opts)` function takes an injectable command runner so every prune is unit-testable without a Docker daemon. The CLI maps flags → `CleanupOptions`, loads registered app names from the store, prints a dry-run plan unless `--yes` is given, and prints a summary (counts + reclaimed bytes) after pruning.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (Docker CLI passthrough — no Docker SDK), `regexp` + `strconv` (prune-output parsing). No new external dependencies.

## Global Constraints

- Existing Docker labels are sacred: containers labeled `tengiz-app=*` must **never** be removed by cleanup
- Rollback safety: the last `--keep-images N` (default `5`) images per app must always be retained
- All volume/network/build-cache operations are default-preserving: only unreferenced/dangling resources are removed by `docker prune`
- No new external dependencies required
- Every task compiles: `go build ./...`, `go vet ./...`, and `go test ./... -v -count=1` must stay green
- Command name is `tengiz cleanup`; flags: `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--dry-run`, `--yes`, `--keep-images N` (default 5)
- Without `--yes` (and not `--dry-run`), the command prints a plan and exits 0 without touching Docker
- Follow repo rules: run tests, commit per task
- This repo's package test convention: table tests, exact paths below; run entire `go test ./...` before each commit

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupSummary`, `DefaultKeepImages`, and the `Cleanup` method on the `Manager` interface + stub implementation |
| `internal/runtime/clean.go` (new) | `pruneArgs`, `countRemovedItems`, `parseReclaimedBytes`, `HumanBytes`, `executeCleanup`, `cleanupRunner`, `(dockerRuntime).Cleanup` |
| `internal/runtime/clean_test.go` (new) | Unit tests for the pure helpers and `executeCleanup` with an injected fake runner |
| `internal/cli/root.go` | New `cleanupCmd` command registered as `tengiz cleanup` + `cleanupOptionsFromCmd` helper |
| `internal/cli/root_test.go` | Implement `Cleanup` on `mockRTForDeploy`; new CLI tests |
| `README.md` | `tengiz cleanup` section in CLI Reference + Commands table row |
| `docs/FUTURES_FEATURES.md` | Mark P0 #6 as ✅ Implemented, add to Implemented Features table |

---

### Task 1: `Cleanup` on the Manager interface + types

**Files:**
- Modify: `internal/runtime/runtime.go` — types + interface method + stub
- Modify: `internal/cli/root_test.go:69-100` — satisfy the new interface method on `mockRTForDeploy`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{Containers, Images, Volumes, Networks, BuildCache bool; AppNames []string; KeepImages int}`, `runtime.CleanupSummary{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved int; BuildCacheCleared bool; ReclaimedSpace int64}`, `runtime.DefaultKeepImages = 5`, and `Manager.Cleanup(ctx, opts) (CleanupSummary, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanupReturnsZeroSummary(t *testing.T) {
	m := NewStub()
	summary, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if summary.ContainersRemoved != 0 || summary.ImagesRemoved != 0 ||
		summary.VolumesRemoved != 0 || summary.NetworksRemoved != 0 {
		t.Fatalf("expected zero counters, got %+v", summary)
	}
	if summary.BuildCacheCleared || summary.ReclaimedSpace != 0 {
		t.Fatalf("expected zero build-cache flag and reclaimed space, got %+v", summary)
	}
}
```

Also add to `internal/cli/root_test.go`:

```go
func TestMockRTForDeployHasCleanup(t *testing.T) {
	// compile-time guard: cleanup must be part of the Manager contract
	m := runtime.NewStub()
	if _, err := m.Cleanup(context.Background(), runtime.CleanupOptions{}); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanupReturnsZeroSummary -v -count=1`

Expected: FAIL — `undefined: mirror.Cleanup` (no `Cleanup` method on the `Manager` interface yet).

- [ ] **Step 3: Add types and interface method to `internal/runtime/runtime.go`**

Add after the `LogOptions`/`RunOptions` declarations:

```go
const DefaultKeepImages = 5

type CleanupOptions struct {
	// Containers prunes stopped containers that are NOT labeled tengiz-app.
	Containers bool
	// Images prunes dangling images, then retains the newest KeepImages per app.
	Images bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	AppNames   []string // registered apps whose newest images must be kept
	KeepImages int      // per-app retention; 0 defaults to DefaultKeepImages
}

type CleanupSummary struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCacheCleared bool
	ReclaimedSpace    int64
}
```

Add the method to the `Manager` interface (after `KeepLastNImages`):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error)
```

Add the stub implementation on `stubManager`:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error) {
	return CleanupSummary{}, nil
}
```

Now `internal/cli/root_test.go` must implement the new method on `mockRTForDeploy` (otherwise the build fails). Add next to the existing `KeepLastNImages` mock:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupSummary, error) {
	return runtime.CleanupSummary{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubCleanupReturnsZeroSummary -v -count=1`

Expected: PASS.

- [ ] **Step 5: Run the full suite + vet**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: Build, vet, tests all pass (mock now satisfies `Manager`).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat: add Cleanup method and types to runtime.Manager interface"
```

---

### Task 2: Pure docker prune helpers (`internal/runtime/clean.go`)

**Files:**
- Create: `internal/runtime/clean.go` — `pruneArgs`, `isIDLine`, `countRemovedItems`, `parseReclaimedBytes`, `HumanBytes`
- Test: `internal/runtime/clean_test.go`

**Interfaces:**
- Consumes: no producer contract
- Produces: `pruneArgs(category string, labelProtected bool) []string`, `countRemovedItems(out string) int`, `parseReclaimedBytes(out string) int64`, `HumanBytes(n int64) string`

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/clean_test.go`:

```go
func TestPruneArgs(t *testing.T) {
	tests := []struct {
		category string
		protect  []bool
		want     []string
	}{
		{"containers", []bool{true}, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"containers", []bool{false}, []string{"container", "prune", "-f"}},
		{"images", []bool{false}, []string{"image", "prune", "-f"}},
		{"volumes", []bool{false}, []string{"volume", "prune", "-f"}},
		{"networks", []bool{false}, []string{"network", "prune", "-f"}},
		{"build-cache", []bool{false}, []string{"builder", "prune", "-f"}},
	}
	for _, tc := range tests {
		got := pruneArgs(tc.category, tc.protect[0])
		if len(got) != len(tc.want) {
			t.Fatalf("pruneArgs(%q) = %v, want %v", tc.category, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("pruneArgs(%q)[%d] = %q, want %q", tc.category, i, got[i], tc.want[i])
			}
		}
	}
}

func TestCountRemovedItems(t *testing.T) {
	out := "Deleted containers:\n02b14df6268e\n89f42a5f1a96\n\nTotal EST space: 0B\n"
	if got := countRemovedItems(out); got != 2 {
		t.Errorf("countRemovedItems(containers) = %d, want 2", got)
	}

	img := "Untagged: alpine:latest\nDeleted: sha256:abc123abc123\n\nTotal reclaimed space: 1.5MB\n"
	if got := countRemovedItems(img); got != 2 {
		t.Errorf("countRemovedItems(images) = %d, want 2", got)
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	// NOTE: compute multipliers at runtime (float64 vars) — Go rejects
	// constant float→int conversions like int64(8.4 * 1024 * 1024).
	mb := float64(1 << 20)
	gb := float64(1 << 30)

	tests := []struct {
		out  string
		want int64
	}{
		{"Total reclaimed space: 0B\n", 0},
		{"Total reclaimed space: 8.4MB\n", int64(8.4 * mb)},
		{"Total reclaimed space: 5.2GB\n", int64(5.2 * gb)},
		{"no summary here\n", 0},
	}
	for _, tc := range tests {
		if got := parseReclaimedBytes(tc.out); got != tc.want {
			t.Errorf("parseReclaimedBytes(%q) = %d, want %d", tc.out, got, tc.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	if got := HumanBytes(0); got != "0.00B" {
		t.Errorf("HumanBytes(0) = %q, want 0.00B", got)
	}
	if got := HumanBytes(5 * 1024 * 1024); got != "5.00MB" {
		t.Errorf("HumanBytes(5MB) = %q, want 5.00MB", got)
	}
}
```

`int64(8.4 * mb)` uses a runtime `float64` variable so it compiles and truncates toward zero (`8.4*2^20 = 8808038.4` → `8808038`). `parseReclaimedBytes` mirrors this: `val` comes from `strconv.ParseFloat` (runtime) and truncates identically via `int64(val * float64(multiplier))`. Keep these two computations consistent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestPruneArgs|TestCountRemovedItems|TestParseReclaimedBytes|TestHumanBytes" -v -count=1`

Expected: FAIL — `undefined: pruneArgs`, `undefined: countRemovedItems`, `undefined: parseReclaimedBytes`, `undefined: HumanBytes`.

- [ ] **Step 3: Write the minimal implementation in `internal/runtime/clean.go`**

```go
package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// pruneArgs returns the docker CLI arguments for pruning one resource category.
// When labelProtected is true, stopped containers without the tengiz-app label
// are removed, and every Tengiz-managed container is kept.
func pruneArgs(category string, labelProtected bool) []string {
	switch category {
	case "containers":
		args := []string{"container", "prune", "-f"}
		if labelProtected {
			args = append(args, "--filter", "label!=tengiz-app")
		}
		return args
	case "images":
		return []string{"image", "prune", "-f"}
	case "volumes":
		return []string{"volume", "prune", "-f"}
	case "networks":
		return []string{"network", "prune", "-f"}
	case "build-cache":
		return []string{"builder", "prune", "-f"}
	default:
		return nil
	}
}

func isHexID(line string) bool {
	if len(line) < 8 {
		return false
	}
	for _, r := range line {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// countRemovedItems counts ID lines plus Untagged:/Deleted: lines in docker
// prune output. It is intentionally lenient across docker output versions.
func countRemovedItems(out string) int {
	count := 0
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "Total reclaimed") {
			continue
		}
		if strings.HasPrefix(line, "Untagged:") || strings.HasPrefix(line, "Deleted:") || isHexID(line) {
			count++
		}
	}
	return count
}

var reclaimedRE = regexp.MustCompile(`(?i)total reclaimed space:\s*([0-9.]+)\s*(b|kb|mb|gb|tb)`)

// parseReclaimedBytes extracts the "Total reclaimed space" value from a docker
// prune run and returns it as a byte count.
func parseReclaimedBytes(out string) int64 {
	m := reclaimedRE.FindStringSubmatch(out)
	if m == nil {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	var multiplier int64 = 1
	switch strings.ToLower(m[2]) {
	case "kb":
		multiplier = 1 << 10
	case "mb":
		multiplier = 1 << 20
	case "gb":
		multiplier = 1 << 30
	case "tb":
		multiplier = 1 << 40
	}
	return int64(val * float64(multiplier))
}

// HumanBytes renders a byte count as a compact string (1.50MB).
func HumanBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	u := 0
	for f >= 1024 && u < len(units)-1 {
		f /= 1024
		u++
	}
	return fmt.Sprintf("%.2f%s", f, units[u])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run "TestPruneArgs|TestCountRemovedItems|TestParseReclaimedBytes|TestHumanBytes" -v -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/clean.go internal/runtime/clean_test.go
git commit -m "feat: add pure helpers for docker prune argument building and output parsing"
```

---

### Task 3: `executeCleanup` + `dockerRuntime.Cleanup`

**Files:**
- Modify: `internal/runtime/clean.go` — add `cleanupRunner`, `runDocker`, `executeCleanup`, `(dockerRuntime).Cleanup`
- Test: `internal/runtime/clean_test.go`

**Interfaces:**
- Consumes: `pruneArgs`, `countRemovedItems`, `parseReclaimedBytes` (Task 2); `KeepLastNImages` (existing), `DefaultKeepImages` (Task 1)
- Produces: `executeCleanup(ctx context.Context, run cleanupRunner, opts CleanupOptions) (CleanupSummary, error)`, `dockerRuntime.Cleanup(ctx, opts) (CleanupSummary, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/clean_test.go`:

```go
func TestExecuteCleanup(t *testing.T) {
	var calls []string
	run := func(ctx context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "container":
			return "Deleted containers:\n02b14df6268e\n89f42a5f1a96\n\nTotal reclaimed space: 0MB\n", nil
		case "image":
			return "Deleted: sha256:aaaaaaaa\nUntagged: alpine:latest\n\nTotal reclaimed space: 148.5MB\n", nil
		case "volume":
			return "Deleted volumes:\n75af63345a42\n\nTotal reclaimed space: 8.4MB\n", nil
		case "network":
			return "Deleted networks:\ne10b410bd84b\n\nTotal reclaimed space: 0B\n", nil
		case "builder":
			return "", nil
		}
		return "", nil
	}

	opts := CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
	}
	s, err := executeCleanup(context.Background(), run, opts)
	if err != nil {
		t.Fatalf("executeCleanup() error = %v", err)
	}
	if s.ContainersRemoved != 2 || s.ImagesRemoved != 2 || s.VolumesRemoved != 1 || s.NetworksRemoved != 1 {
		t.Fatalf("unexpected summary: %+v", s)
	}
	if !s.BuildCacheCleared {
		t.Fatal("BuildCacheCleared = false, want true")
	}
	// NOTE: compute at runtime (float64 vars) — Go rejects constant
	// float→int conversions like int64(148.5 * 1024 * 1024).
	mb := float64(1 << 20)
	wantSpace := int64(148.5*mb) + int64(8.4*mb)
	if s.ReclaimedSpace != wantSpace {
		t.Errorf("ReclaimedSpace = %d, want %d", s.ReclaimedSpace, wantSpace)
	}

	// A call with label protection must include the label filter.
	found := false
	for _, c := range calls {
		if strings.HasPrefix(c, "container prune -f") && strings.Contains(c, "label!=tengiz-app") {
			found = true
		}
	}
	if !found {
		t.Errorf("container prune did not include the label filter; calls = %v", calls)
	}

	// Subset runs only enabled categories.
	calls = nil
	_, _ = executeCleanup(context.Background(), run, CleanupOptions{Volumes: true})
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "volume ") {
		t.Errorf("expected only volume prune, got %v", calls)
	}
}
```

The expected reclaim `wantSpace` uses the same runtime `float64` multiplier (`1 << 20`) as `parseReclaimedBytes`, so both computations truncate identically. `148.5` (network) and `8.4` (volume) are summed; the container and network outputs return `0`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestExecuteCleanup -v -count=1`

Expected: FAIL — `undefined: executeCleanup`.

- [ ] **Step 3: Implement in `internal/runtime/clean.go`**

Append to the file (keeping `os/exec` import):

```go
type cleanupRunner func(ctx context.Context, args ...string) (string, error)

func runDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

// executeCleanup runs one docker prune per enabled category through `run` and
// accumulates the resulting summary. It is kept pure so tests can pass a fake
// runner instead of a Docker daemon.
func executeCleanup(ctx context.Context, run cleanupRunner, opts CleanupOptions) (CleanupSummary, error) {
	var s CleanupSummary
	type step struct {
		category string
		enabled  bool
		count    *int
	}
	pipe := []step{
		{"containers", opts.Containers, &s.ContainersRemoved},
		{"images", opts.Images, &s.ImagesRemoved},
		{"volumes", opts.Volumes, &s.VolumesRemoved},
		{"networks", opts.Networks, &s.NetworksRemoved},
	}
	for _, st := range pipe {
		if !st.enabled {
			continue
		}
		out, err := run(ctx, pruneArgs(st.category, true)...)
		if err != nil {
			return s, err
		}
		*st.count = countRemovedItems(out)
		s.ReclaimedSpace += parseReclaimedBytes(out)
	}
	if opts.BuildCache {
		if _, err := run(ctx, pruneArgs("build-cache", false)...); err != nil {
			return s, err
		}
		s.BuildCacheCleared = true
	}
	return s, nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error) {
	summary, err := executeCleanup(ctx, runDocker, opts)
	if err != nil {
		return summary, err
	}
	if opts.Images {
		keep := opts.KeepImages
		if keep <= 0 {
			keep = DefaultKeepImages
		}
		for _, app := range opts.AppNames {
			if err := r.KeepLastNImages(ctx, app, keep); err != nil {
				log.Printf("[runtime] keep %d images for %s: %v", keep, app, err)
			}
		}
	}
	return summary, nil
}
```

Update the import block in `clean.go` to include `os/exec` and adjust `parseReclaimedBytes` — it already uses the `int64(val * float64(multiplier))` math; keep it as the single source of truth. If the test's expected value differs from the parser's, update the expected in Task 3 Step 1's test to match the parser (a dry-run arithmetic error, not a code error).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestExecuteCleanup -v -count=1`

Expected: PASS.

- [ ] **Step 5: Run the full suite + vet**

Run: `go build ./... && go vet ./... && go test ./internal/runtime/ ./internal/cli/ -count=1`

Expected: All PASS, vet clean.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/clean.go internal/runtime/clean_test.go
git commit -m "feat: implement runtime.Cleanup docker prune runner"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — `cleanupCmd` + `cleanupOptionsFromCmd` + `register` in `init()`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupSummary`, `runtime.HumanBytes`, `config.NewStoreWithEnv` (all existing)
- Produces: `tengiz cleanup [--containers] [--images] [--volumes] [--networks] [--build-cache] [--dry-run] [--yes] [--keep-images N]`

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil || cmd == nil {
		t.Fatal("cleanup command not registered")
	}
	for _, flag := range []string{"containers", "images", "volumes", "networks", "build-cache", "dry-run", "yes", "keep-images"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupOptionsFromCmdDefaultAll(t *testing.T) {
	c := cleanupCmd
	for _, f := range []string{"containers", "images", "volumes", "networks", "build-cache"} {
		if err := c.Flags().Set(f, "false"); err != nil {
			t.Fatalf("set %s: %v", f, err)
		}
	}
	opts := cleanupOptionsFromCmd(c)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("default cleanup (no category flag) should enable all, got %+v", opts)
	}
}

func TestCleanupOptionsFromCmdSubset(t *testing.T) {
	c := cleanupCmd
	for _, f := range []string{"containers", "images", "volumes", "networks", "build-cache"} {
		c.Flags().Set(f, "false")
	}
	c.Flags().Set("containers", "true")
	c.Flags().Set("keep-images", "3")
	opts := cleanupOptionsFromCmd(c)
	if !opts.Containers {
		t.Error("containers not enabled")
	}
	if opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("only containers should be enabled, got %+v", opts)
	}
	if opts.KeepImages != 3 {
		t.Errorf("KeepImages = %d, want 3", opts.KeepImages)
	}
}

func TestCleanupDryRunDoesNotNeedDocker(t *testing.T) {
	old := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = old }()

	// Reset shared cleanupCmd flags (mutated by the two option tests above).
	for _, f := range []string{"containers", "images", "volumes", "networks", "build-cache"} {
		cleanupCmd.Flags().Set(f, "false")
	}
	cleanupCmd.Flags().Set("keep-images", "5")

	rootCmd.SetArgs([]string{"cleanup"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}
```

Test isolation: `cleanupCmd.Flags().Set(...)` mutates the shared package-level command, so the dry-run test resets all category flags to `false` (the no-flags default then enables everything) and resets `keep-images`. The dry-run path never calls `runtime.NewDocker()`, so it runs in environments without Docker.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`

Expected: FAIL — `cleanup command not registered`, `undefined: cleanupCmd`.

- [ ] **Step 3: Implement `cleanupCmd` + helper in `internal/cli/root.go`**

Add the command definition (near the other app commands, e.g. after `psCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (Tengiz containers are always kept)",
	Long: `Prune stopped containers, dangling images, unused volumes, unused networks, and
Docker build cache. Containers carrying the tengiz-app label are never pruned.
Runs as a dry-run unless --yes is provided.

Examples:
  tengiz cleanup                  # print what would be cleaned (dry-run)
  tengiz cleanup --yes            # apply all categories
  tengiz cleanup --images --yes
`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		opts := cleanupOptionsFromCmd(cmd)

		store := config.NewStoreWithEnv(dataDir, env)
		if apps, err := store.ListApps(); err == nil {
			for _, app := range apps {
				opts.AppNames = append(opts.AppNames, app.Name)
			}
		}

		apply, _ := cmd.Flags().GetBool("yes")

		fmt.Println("[tengiz] cleanup plan:")
		fmt.Printf("  containers: %v\n", opts.Containers)
		fmt.Printf("  images:     %v (keep %d newest per app)\n", opts.Images, keepFor(opts.KeepImages))
		fmt.Printf("  volumes:    %v\n", opts.Volumes)
		fmt.Printf("  networks:   %v\n", opts.Networks)
		fmt.Printf("  build-cache:%v\n", opts.BuildCache)

		if !apply {
			fmt.Println("[tengiz] dry-run — rerun with --yes to apply.")
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		summary, err := rt.Cleanup(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		fmt.Printf("[tengiz] cleanup complete:\n")
		fmt.Printf("  containers removed: %d\n", summary.ContainersRemoved)
		fmt.Printf("  images removed:     %d\n", summary.ImagesRemoved)
		fmt.Printf("  volumes removed:    %d\n", summary.VolumesRemoved)
		fmt.Printf("  networks removed:   %d\n", summary.NetworksRemoved)
		fmt.Printf("  build cache cleared: %v\n", summary.BuildCacheCleared)
		fmt.Printf("  reclaimed:          %s\n", runtime.HumanBytes(summary.ReclaimedSpace))
		return nil
	},
}

func keepFor(n int) int {
	if n <= 0 {
		return runtime.DefaultKeepImages
	}
	return n
}

// cleanupOptionsFromCmd converts the cleanup CLI flags into a CleanupOptions.
// When no category flag is set, every category is enabled.
func cleanupOptionsFromCmd(cmd *cobra.Command) runtime.CleanupOptions {
	cats := []string{"containers", "images", "volumes", "networks", "build-cache"}
	any := false
	for _, c := range cats {
		if b, _ := cmd.Flags().GetBool(c); b {
			any = true
		}
	}
	var opts runtime.CleanupOptions
	for _, c := range cats {
		b, _ := cmd.Flags().GetBool(c)
		if !any {
			b = true
		}
		switch c {
		case "containers":
			opts.Containers = b
		case "images":
			opts.Images = b
		case "volumes":
			opts.Volumes = b
		case "networks":
			opts.Networks = b
		case "build-cache":
			opts.BuildCache = b
		}
	}
	keep, _ := cmd.Flags().GetInt("keep-images")
	opts.KeepImages = keep
	return opts
}
```

Register in `init()` (next to the other `rootCmd.AddCommand` calls):

```go
	rootCmd.AddCommand(cleanupCmd)
```

And add the flags (near the end of `init()`):

```go
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images and old per-app images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "print the plan without removing anything")
	cleanupCmd.Flags().BoolP("yes", "y", false, "apply the prune (default is dry-run)")
	cleanupCmd.Flags().Int("keep-images", 5, "newest images to keep per app for rollback")
```

The `--dry-run` flag is accepted for explicitness; behavior matches the no-`--yes` path (plan printed, nothing removed).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`

Expected: PASS.

- [ ] **Step 5: Full suite + vet + build**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: All PASS, vet clean.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 5: Documentation

**Files:**
- Modify: `README.md` (CLI Reference + Commands table)
- Modify: `docs/FUTURES_FEATURES.md` (mark P0 #6 implemented)

**Interfaces:** none

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md` after the `tengiz ps` section (after line ~150)**

```markdown
### `tengiz cleanup`

Prune unused Docker resources. Tengiz-managed containers (labeled `tengiz-app`) are always kept, and the newest `N` versioned images per app are retained so `tengiz rollback` keeps working.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers **not** managed by Tengiz |
| `--images` | Prune dangling images + old per-app images (keep `--keep-images`) |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune Docker build cache |
| `--keep-images N` | Newest images kept per app for rollback (default: 5) |
| `-y`, `--yes` | Apply the plan (default: print a dry-run) |
| `--dry-run` | Print the plan without removing anything |

With no category flags, all categories are cleaned. Without `--yes`, the command only prints a plan.

Example:
```
tengiz cleanup               # dry-run
tengiz cleanup --yes         # prune all categories
tengiz cleanup --images --keep-images 3 --yes
```
```

- [ ] **Step 2: Add a row to the Commands table in `README.md` (near line 575)**

```markdown
| `tengiz cleanup [--yes]` | Prune unused Docker resources (containers, images, volumes, networks, build cache) |
```

- [ ] **Step 3: Update `docs/FUTURES_FEATURES.md`**

In the P0 table, change row 6 from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the `✅ Implemented Features (Not Pending)` table (after the Webhook row):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-08) |
```

- [ ] **Step 4: Verify + commit**

Run: `go build ./...` (no code change; sanity check)

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

### Task 6: Integration verification + self-review

**Files:**
- Test: `internal/cli/root_test.go` (end-to-end flag → options → dry-run)

**Interfaces:** none new

- [ ] **Step 1: Add one end-to-end flag→options→dry-run test**

```go
func TestCleanupFullFlow(t *testing.T) {
	old := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = old }()

	for _, f := range []string{"containers", "images", "volumes", "networks", "build-cache"} {
		cleanupCmd.Flags().Set(f, "false")
	}
	cleanupCmd.Flags().Set("images", "true")
	cleanupCmd.Flags().Set("yes", "false")

	// dry-run path must not require Docker
	out := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--images", "--dry-run"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(out, "plan") && !strings.Contains(out, "dry-run") {
		t.Errorf("expected dry-run output, got %s", out)
	}
}
```

- [ ] **Step 2: Run the full suite + vet + build**

Run: `go build ./... && go vet ./... && go test ./... -v -count=1`

Expected: All packages PASS, `go vet` clean. (Proxy tests are ~2s each and idle tests are time-sensitive — if they flake, rerun with `-count=1`.)

- [ ] **Step 3: Self-review against spec**

Check against the `Docker Housekeeping` entry (P0 #6) and its long-form detail in `docs/FUTURES_FEATURES.md`:
- Label-based `docker system prune` semantics → covered: `docker container prune --filter label!=tengiz-app` (Tasks 2-3), plus volumes/networks/build-cache.
- `tengiz cleanup` command → Task 4.
- Viewing resource usage (no manual change, and Tengiz containers protected) → containers labeled `tengiz-app` retained in Task 2's prune args + tests.
- Keep N images for rollback → `KeepLastNImages` wired in `(dockerRuntime).Cleanup` (Task 3) honoring `KeepImages`.
- No config-file/state format change → no migration steps required.

- [ ] **Step 4: Placeholder scan**

Search the plan: no "TBD", "TODO", "implement later", "fill in details", "similar to Task N". All steps carry concrete code and exact commands.

- [ ] **Step 5: Type-consistency check**

- `pruneArgs(category string, labelProtected bool) []string` — Task 2 defines, Task 3 calls with `st.category`; matches.
- `countRemovedItems(out string) int`, `parseReclaimedBytes(out string) int64`, `HumanBytes(int64) string` — defined Task 2, used Task 3 + Task 4 (`runtime.HumanBytes`).
- `cleanupRunner` = `type cleanupRunner func(ctx context.Context, args ...string) (string, error)` — Task 3; `runDocker` satisfies it.
- `executeCleanup(ctx, run cleanupRunner, opts CleanupOptions) (CleanupSummary, error)` — consistent between Task 3 tests and implementation.
- `runtime.CleanupOptions` field names (`Containers`, `Images`, `Volumes`, `Networks`, `BuildCache`, `AppNames`, `KeepImages`) and `runtime.CleanupSummary` (`ContainersRemoved`, `ImagesRemoved`, `VolumesRemoved`, `NetworksRemoved`, `BuildCacheCleared`, `ReclaimedSpace`) match across Tasks 1, 3, 4.
- `cleanupOptionsFromCmd(cmd *cobra.Command) runtime.CleanupOptions` — Task 4 tests and implementation agree.
- `DefaultKeepImages`/`keepFor` keep the default `5` in both runtime and CLI.

If any mismatch is found above, fix it inline before proceeding.

- [ ] **Step 6: Final commit if any fixes were made**

```bash
git add .
git commit -m "test: verify tengiz cleanup dry-run flow"
```