# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning stopped Tengiz containers, dangling images, Docker build cache, and unused networks — all label-scoped so non-Tengiz resources are never touched.

**Architecture:** A new `Cleanup` method is added to the `runtime.Manager` interface. The Docker implementation builds and executes per-category `docker <x> prune --force` commands (containers filtered by the `tengiz-app` label), parses each prune's output ("Deleted ..." / "Total reclaimed space") into counts and bytes, and returns a `CleanupResult`. The CLI wraps this in `tengiz cleanup` with `--dry-run`, per-category toggles, `--all`, and `--keep-images` (per-app image retention reuses the existing `KeepLastNImages` so rollback images survive). Per-category command construction, prune-output parsing, and byte-size parsing are pure functions that are unit-tested without Docker; the CLI is tested through a `newDockerRuntime` injection seam so tests pass in CI where no Docker daemon is installed.

**Tech Stack:** Go 1.26, `cobra`, Docker CLI via `os/exec` (no new dependencies).

## Global Constraints

- Single Go module `github.com/yaso09/tengiz`, Go 1.26. No new dependencies.
- Runtime calls the `docker` CLI via `os/exec`; never use a Docker SDK.
- Every destructive Docker operation MUST be scoped: stopped containers only and filtered by the `tengiz-app` label (`labelKey` const in `internal/runtime/docker.go`); images via `docker image prune --force` (dangling only — never `-a`, which would delete rollback images); never prune a running container.
- Image tags built by Tengiz are `tengiz-apps/<app>:<env>-<deploymentID>` and `tengiz-apps/<app>:<env>-latest`. The existing `KeepLastNImages(ctx, appName, n)` (in `internal/runtime/cleanup.go`) keeps the newest `n` per app and never removes `:latest` tags.
- `~/.tengiz/` state is env-scoped. The cleanup command's image-retention pass must read the app list with `config.NewStoreWithEnv(dataDir, env)` so `--env` scoping is respected.
- **Tests MUST pass without a Docker daemon** (CI `ci.yml` does not set up Docker). Never write a test that executes a real `docker prune`. The only executable glue test is `dockerRuntime.Cleanup` with `DryRun: true`, which short-circuits before any `exec` call and works on any machine.
- Do not modify files outside the list in each task. Follow existing code style (no new comments beyond what is shown).

---

### Task 1: Add `Cleanup` to the `runtime.Manager` interface (types, stub, and test-mocks)

**Files:**
- Modify: `internal/runtime/cleanup.go` (add `CleanupOptions`, `CleanupResult` type definitions)
- Modify: `internal/runtime/runtime.go` (interface method + `stubManager` method)
- Modify: `internal/runtime/cleanup_test.go` (add `TestStubCleanup`)
- Modify: `internal/cli/root_test.go` (mock `mockRTForDeploy`)
- Modify: `internal/proxy/proxy_test.go` (mock `mockRuntime`)
- Modify: `internal/idle/idle_test.go` (mock `mockRuntime`)

**Interfaces:**
- Produces (consumed by later tasks):
  - `type CleanupOptions struct { DryRun bool; Containers bool; Images bool; BuildCache bool; Volumes bool; Networks bool; KeepImages int }`
  - `type CleanupResult struct { ContainersPruned int64; ImagesPruned int64; BuildCacheFreed int64; VolumesPruned int64; NetworksPruned int64 }`
  - `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` on the `runtime.Manager` interface.

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go` (keep the two existing tests in the file):

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersPruned != 0 || res.ImagesPruned != 0 || res.BuildCacheFreed != 0 {
		t.Errorf("stub Cleanup() should return a zero result, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -count=1`
Expected: FAIL — compile error `undefined: CleanupOptions` (or `Cleanup undefined`).

- [ ] **Step 3: Implement the interface method, stub, and type definitions**

In `internal/runtime/cleanup.go`, add these type definitions at the top of the file (below the imports):

```go
type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	BuildCache bool
	Volumes    bool
	Networks   bool
	KeepImages int
}

type CleanupResult struct {
	ContainersPruned int64
	ImagesPruned     int64
	BuildCacheFreed  int64
	VolumesPruned    int64
	NetworksPruned   int64
}
```

In `internal/runtime/runtime.go`, add the method to the `Manager` interface (after the `KeepLastNImages` line):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

And add the stub method to `stubManager` in `internal/runtime/runtime.go` (after the `KeepLastNImages` stub method):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

Update the three test mocks so the package still compiles (the interface grew a method, so every implementation must add it):

`internal/cli/root_test.go` — add to `mockRTForDeploy` (after the `KeepLastNImages` method):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

`internal/proxy/proxy_test.go` — add to `mockRuntime` (after the `KeepLastNImages` method):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

`internal/idle/idle_test.go` — add to `mockRuntime` (after the `KeepLastNImages` method):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -count=1`
Expected: PASS (all packages). `go vet ./...` should also be clean.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface and stub"
```

---

### Task 2: `buildCleanupCommands` — pure command-list builder

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions` from Task 1.
- Produces: `func buildCleanupCommands(opts CleanupOptions) [][]string` — returns one docker-args slice per enabled category, in a fixed order: containers, images, build-cache, volumes, networks.

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestBuildCleanupCommands(t *testing.T) {
	tests := []struct {
		name     string
		opts     CleanupOptions
		expected [][]string
	}{
		{
			name:     "nothing enabled",
			opts:     CleanupOptions{},
			expected: nil,
		},
		{
			name: "containers only",
			opts: CleanupOptions{Containers: true},
			expected: [][]string{
				{"container", "prune", "--force", "--filter", "label=tengiz-app"},
			},
		},
		{
			name: "all categories",
			opts: CleanupOptions{Containers: true, Images: true, BuildCache: true, Volumes: true, Networks: true},
			expected: [][]string{
				{"container", "prune", "--force", "--filter", "label=tengiz-app"},
				{"image", "prune", "--force"},
				{"builder", "prune", "--force"},
				{"volume", "prune", "--force"},
				{"network", "prune", "--force"},
			},
		},
		{
			name: "images and networks",
			opts: CleanupOptions{Images: true, Networks: true},
			expected: [][]string{
				{"image", "prune", "--force"},
				{"network", "prune", "--force"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCleanupCommands(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("len = %d, want %d: %v", len(got), len(tt.expected), got)
			}
			for i := range got {
				if len(got[i]) != len(tt.expected[i]) {
					t.Fatalf("cmd %d len = %d, want %d: %v", i, len(got[i]), len(tt.expected[i]), got[i])
				}
				for j := range got[i] {
					if got[i][j] != tt.expected[i][j] {
						t.Errorf("cmd %d arg %d = %q, want %q", i, j, got[i][j], tt.expected[i][j])
					}
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestBuildCleanupCommands -count=1`
Expected: FAIL — compile error `undefined: buildCleanupCommands`.

- [ ] **Step 3: Implement `buildCleanupCommands`**

Add to `internal/runtime/cleanup.go`:

```go
const cleanupLabelFilter = "label=" + labelKey

func buildCleanupCommands(opts CleanupOptions) [][]string {
	var cmds [][]string
	if opts.Containers {
		cmds = append(cmds, []string{"container", "prune", "--force", "--filter", cleanupLabelFilter})
	}
	if opts.Images {
		cmds = append(cmds, []string{"image", "prune", "--force"})
	}
	if opts.BuildCache {
		cmds = append(cmds, []string{"builder", "prune", "--force"})
	}
	if opts.Volumes {
		cmds = append(cmds, []string{"volume", "prune", "--force"})
	}
	if opts.Networks {
		cmds = append(cmds, []string{"network", "prune", "--force"})
	}
	return cmds
}
```

Note: `labelKey` is already defined as `"tengiz-app"` in `internal/runtime/docker.go`. The containers prune is intentionally filtered by that label so only stopped Tengiz-managed containers are candidates.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestBuildCleanupCommands -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): build label-scoped docker prune command list"
```

---

### Task 3: `parsePruneOutput`, `parseBytes`, and `FormatBytes` helpers

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing beyond the standard library.
- Produces:
  - `func parsePruneOutput(output string) (count int64, freed int64)` — counts lines in the `Deleted ...:` section and parses the trailing `Total reclaimed space:` value.
  - `func parseBytes(s string) int64` — parses Docker human sizes (`0B`, `100B`, `1.5MB`, `2GB`, `1GiB`).
  - `func FormatBytes(n int64) string` — formats a byte count as `0 B`, `1.0 KiB`, `1.5 MiB`, etc. (exported; used by the CLI).

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantCount int64
		wantFreed int64
	}{
		{
			name:      "nothing to delete",
			output:    "Total reclaimed space: 0B\n",
			wantCount: 0,
			wantFreed: 0,
		},
		{
			name: "containers",
			output: "Deleted Containers:\n" +
				"abc123\n" +
				"def456\n" +
				"\n" +
				"Total reclaimed space: 12.5MB\n",
			wantCount: 2,
			wantFreed: 12500000,
		},
		{
			name: "images",
			output: "Deleted Images:\n" +
				"deleted: sha256:abc\n" +
				"untagged: tengiz-apps/foo:production-123\n" +
				"deleted: sha256:def\n" +
				"Total reclaimed space: 523MB\n",
			wantCount: 3,
			wantFreed: 523000000,
		},
		{
			name: "empty deleted section",
			output: "Deleted Networks:\n" +
				"Total reclaimed space: 0B\n",
			wantCount: 0,
			wantFreed: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, freed := parsePruneOutput(tt.output)
			if count != tt.wantCount {
				t.Errorf("count = %d, want %d", count, tt.wantCount)
			}
			if freed != tt.wantFreed {
				t.Errorf("freed = %d, want %d", freed, tt.wantFreed)
			}
		})
	}
}

func TestParseBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"0B", 0},
		{"100B", 100},
		{"1.5MB", 1500000},
		{"2GB", 2000000000},
		{"1GiB", 1073741824},
		{"512KiB", 524288},
		{"garbage", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := parseBytes(tt.in); got != tt.want {
				t.Errorf("parseBytes(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1048576, "1.0 MiB"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.in), func(t *testing.T) {
			if got := FormatBytes(tt.in); got != tt.want {
				t.Errorf("FormatBytes(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
```

Note: `TestFormatBytes` uses `fmt.Sprintf` — add `"fmt"` to the import block of `internal/runtime/cleanup_test.go` (the file currently imports only `context` and `testing`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestParsePruneOutput|TestParseBytes|TestFormatBytes' -count=1`
Expected: FAIL — compile error `undefined: parsePruneOutput`.

- [ ] **Step 3: Implement the helpers**

Add to `internal/runtime/cleanup.go` and add `"strconv"` to its imports (current imports: `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`):

```go
const reclaimedPrefix = "Total reclaimed space:"

func parsePruneOutput(output string) (int64, int64) {
	var count, freed int64
	inDeleted := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "total reclaimed space:") {
			freed = parseBytes(strings.TrimSpace(line[len(reclaimedPrefix):]))
			inDeleted = false
			continue
		}
		if strings.HasPrefix(lower, "deleted ") && strings.HasSuffix(line, ":") {
			inDeleted = true
			continue
		}
		if inDeleted {
			count++
		}
	}
	return count, freed
}

func parseBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	type unit struct {
		suffix string
		mult   float64
	}
	units := []unit{
		{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
		{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3},
		{"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0
			}
			return int64(f * u.mult)
		}
	}
	return 0
}

func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
```

Note: longer suffixes must be checked before shorter ones (`GiB` before `GB`, `MB` before `B`) so `1.5MB` is not truncated to `M` and then misparsed — the ordered `units` list handles this. The `deleted ` header detection intentionally requires a space (not a colon) after `deleted`, so image lines like `deleted: sha256:...` are counted as pruned items, not treated as headers.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestParsePruneOutput|TestParseBytes|TestFormatBytes' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): parse docker prune output and human byte sizes"
```

---

### Task 4: Implement `dockerRuntime.Cleanup` (the exec glue)

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `buildCleanupCommands` (Task 2), `parsePruneOutput` (Task 3), `CleanupOptions`/`CleanupResult` (Task 1).
- Produces: `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestDockerCleanupDryRun(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Cleanup(context.Background(), CleanupOptions{
		DryRun:     true,
		Containers: true,
		Images:     true,
		BuildCache: true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup(dry-run) error = %v", err)
	}
	if res.ContainersPruned != 0 || res.ImagesPruned != 0 || res.BuildCacheFreed != 0 {
		t.Errorf("dry-run should not prune anything, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestDockerCleanupDryRun -count=1`
Expected: FAIL — compile error `(*dockerRuntime).Cleanup undefined`.

- [ ] **Step 3: Implement `dockerRuntime.Cleanup`**

Add to `internal/runtime/cleanup.go` (this file already imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`):

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var result CleanupResult
	for _, args := range buildCleanupCommands(opts) {
		desc := strings.Join(args, " ")
		if opts.DryRun {
			log.Printf("[tengiz] dry-run: docker %s", desc)
			continue
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return result, fmt.Errorf("docker %s: %w\n%s", desc, err, string(out))
		}
		count, freed := parsePruneOutput(string(out))
		switch args[0] {
		case "container":
			result.ContainersPruned += count
		case "image":
			result.ImagesPruned += count
		case "builder":
			result.BuildCacheFreed += freed
		case "volume":
			result.VolumesPruned += count
		case "network":
			result.NetworksPruned += count
		}
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -count=1`
Expected: PASS (all packages — `TestDockerCleanupDryRun` proves the glue works and never execs Docker because `DryRun` short-circuits before `exec.CommandContext`).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker Cleanup with dry-run support"
```

---

### Task 5: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root_test.go` — already updated in Task 1 (no further change here)

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.FormatBytes` from earlier tasks; `config.NewStoreWithEnv(dataDir, env)`; package vars `dataDir` and `getEnv(cmd)` from `internal/cli/root.go`; `captureOutput` helper from `internal/cli/root_test.go`.
- Produces:
  - `var newDockerRuntime = runtime.NewDocker` — package-level seam that tests override.
  - `cleanupCmd` (a `*cobra.Command`) registered on `rootCmd` from its own `init()` (matching the `preview.go` pattern — do NOT edit `root.go`).
  - Flags: `--dry-run`, `--all`, `--containers`, `--images`, `--build-cache`, `--networks`, `--volumes`, `--keep-images` (default 5).
  - Behavior: when no category flag is explicitly set (and `--all` is not set), defaults are `--containers --images --build-cache --networks` (volumes stay opt-in). `--all` enables every category including `--volumes`. `--keep-images` retains the last N images per app via `KeepLastNImages` — only when `--images` is enabled and NOT in `--dry-run`. Prints a result summary.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"dry-run", "all", "containers", "images", "build-cache", "networks", "volumes", "keep-images"}
	for _, name := range expected {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup command missing flag --%s", name)
		}
	}
}

type recordingRuntime struct {
	runtime.Manager
	cleanupOpts []runtime.CleanupOptions
	keepImages  []string
}

func (m *recordingRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	m.cleanupOpts = append(m.cleanupOpts, opts)
	return runtime.CleanupResult{
		ContainersPruned: 3,
		ImagesPruned:     2,
		BuildCacheFreed:  1048576,
		VolumesPruned:    0,
		NetworksPruned:   1,
	}, nil
}

func (m *recordingRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	m.keepImages = append(m.keepImages, appName)
	return nil
}

func withCleanupRuntime(t *testing.T, rt runtime.Manager) func() {
	t.Helper()
	old := newDockerRuntime
	newDockerRuntime = func() (runtime.Manager, error) { return rt, nil }
	return func() { newDockerRuntime = old }
}

func TestCleanupDefaultsToSafeCategories(t *testing.T) {
	rt := &recordingRuntime{}
	defer withCleanupRuntime(t, rt)()
	dataDir = t.TempDir()

	rootCmd.SetArgs([]string{"cleanup"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(rt.cleanupOpts) != 1 {
		t.Fatalf("Cleanup called %d times, want 1", len(rt.cleanupOpts))
	}
	opts := rt.cleanupOpts[0]
	if !opts.Containers || !opts.Images || !opts.BuildCache || !opts.Networks {
		t.Errorf("default categories wrong: %+v", opts)
	}
	if opts.Volumes {
		t.Error("--volumes should default to false")
	}
	if opts.KeepImages != 5 {
		t.Errorf("KeepImages = %d, want 5", opts.KeepImages)
	}
}

func TestCleanupAllEnablesVolumes(t *testing.T) {
	rt := &recordingRuntime{}
	defer withCleanupRuntime(t, rt)()
	dataDir = t.TempDir()

	rootCmd.SetArgs([]string{"cleanup", "--all"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !rt.cleanupOpts[0].Volumes {
		t.Error("--all should enable --volumes")
	}
	if !rt.cleanupOpts[0].Containers || !rt.cleanupOpts[0].Images || !rt.cleanupOpts[0].BuildCache || !rt.cleanupOpts[0].Networks {
		t.Errorf("--all should enable all categories: %+v", rt.cleanupOpts[0])
	}
}

func TestCleanupFlagOverrideDisablesDefaults(t *testing.T) {
	rt := &recordingRuntime{}
	defer withCleanupRuntime(t, rt)()
	dataDir = t.TempDir()

	rootCmd.SetArgs([]string{"cleanup", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	opts := rt.cleanupOpts[0]
	if !opts.Volumes {
		t.Error("--volumes flag not honored")
	}
	if opts.Containers || opts.Images || opts.BuildCache || opts.Networks {
		t.Errorf("explicit --volumes should disable the default categories: %+v", opts)
	}
}

func TestCleanupDryRunSkipsImageRetention(t *testing.T) {
	rt := &recordingRuntime{}
	defer withCleanupRuntime(t, rt)()
	dataDir = t.TempDir()
	store := config.NewStore(dataDir)
	if err := store.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp"}}); err != nil {
		t.Fatal(err)
	}

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(output, "Dry run") {
		t.Errorf("output missing 'Dry run' marker, got: %s", output)
	}
	if !strings.Contains(output, "Containers pruned: 3") {
		t.Errorf("output missing container count, got: %s", output)
	}
	if len(rt.keepImages) != 0 {
		t.Errorf("KeepLastNImages should be skipped in dry-run, got %v", rt.keepImages)
	}
	if !rt.cleanupOpts[0].DryRun {
		t.Error("DryRun not passed through to Cleanup")
	}
}

func TestCleanupImageRetentionCallsKeepLastNImages(t *testing.T) {
	rt := &recordingRuntime{}
	defer withCleanupRuntime(t, rt)()
	dataDir = t.TempDir()
	store := config.NewStore(dataDir)
	if err := store.SaveApp(types.AppEntry{Name: "alpha", Config: types.AppConfig{Name: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveApp(types.AppEntry{Name: "beta", Config: types.AppConfig{Name: "beta"}}); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"cleanup", "--images"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(rt.keepImages) != 2 {
		t.Fatalf("KeepLastNImages called %d times, want 2 (got %v)", len(rt.keepImages), rt.keepImages)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup' -count=1`
Expected: FAIL — `cleanup command not found` / `undefined: newDockerRuntime`.

- [ ] **Step 3: Implement the command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var newDockerRuntime = runtime.NewDocker

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Removes Docker resources created by Tengiz deployments to reclaim disk space.

Stopped containers are filtered by the tengiz-app label, so non-Tengiz containers are never touched.
Unused (dangling) images and the Docker build cache are pruned. With --images, the last
--keep-images images per app are retained so rollback continues to work.

Categories (default when no category flag is given: --containers --images --build-cache --networks):
  --containers   remove stopped Tengiz containers (label-filtered)
  --images       remove dangling images and old per-app images beyond --keep-images
  --build-cache  remove Docker BuildKit build cache
  --networks     remove unused Docker networks
  --volumes      remove unused Docker volumes (opt-in: may affect non-Tengiz named volumes)
  --all          enable all categories including --volumes

Use --dry-run to print the docker commands that would run without removing anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		networks, _ := cmd.Flags().GetBool("networks")
		volumes, _ := cmd.Flags().GetBool("volumes")
		keepImages, _ := cmd.Flags().GetInt("keep-images")
		if keepImages <= 0 {
			keepImages = 5
		}

		if all {
			containers, images, buildCache, networks, volumes = true, true, true, true, true
		} else if !cmd.Flags().Changed("containers") && !cmd.Flags().Changed("images") &&
			!cmd.Flags().Changed("build-cache") && !cmd.Flags().Changed("networks") &&
			!cmd.Flags().Changed("volumes") {
			containers, images, buildCache, networks = true, true, true, true
		}

		rt, err := newDockerRuntime()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			DryRun:     dryRun,
			Containers: containers,
			Images:     images,
			BuildCache: buildCache,
			Networks:   networks,
			Volumes:    volumes,
			KeepImages: keepImages,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if images && !dryRun {
			store := config.NewStoreWithEnv(dataDir, env)
			apps, _ := store.ListApps()
			for _, app := range apps {
				if err := rt.KeepLastNImages(cmd.Context(), app.Name, keepImages); err != nil {
					fmt.Printf("[tengiz] warning: keep images for %s: %v\n", app.Name, err)
				}
			}
		}

		fmt.Printf("Containers pruned: %d\n", result.ContainersPruned)
		fmt.Printf("Images pruned: %d\n", result.ImagesPruned)
		fmt.Printf("Build cache freed: %s\n", runtime.FormatBytes(result.BuildCacheFreed))
		fmt.Printf("Volumes pruned: %d\n", result.VolumesPruned)
		fmt.Printf("Networks pruned: %d\n", result.NetworksPruned)
		if dryRun {
			fmt.Println("Dry run -- no resources were removed.")
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "print docker commands without running them")
	cleanupCmd.Flags().Bool("all", false, "enable all categories including --volumes")
	cleanupCmd.Flags().Bool("containers", false, "remove stopped Tengiz containers (label-filtered)")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images and old per-app images")
	cleanupCmd.Flags().Bool("build-cache", false, "remove Docker BuildKit build cache")
	cleanupCmd.Flags().Bool("networks", false, "remove unused Docker networks")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused Docker volumes (opt-in)")
	cleanupCmd.Flags().Int("keep-images", 5, "keep last N images per app (used with --images)")
	rootCmd.AddCommand(cleanupCmd)
}
```

Note: `cleanupCmd` registers itself in its own `init()` — do NOT edit `internal/cli/root.go`. `dataDir` and `getEnv` come from `root.go` in the same package. The `--env` persistent flag inherited from `rootCmd` is read by `getEnv(cmd)`, so `--env staging` scopes the image-retention app list.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup' -count=1`
Expected: PASS. Then run the whole suite:

Run: `go test ./... -count=1`
Expected: PASS. Also run: `go vet ./...` — expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 6: Documentation (README.md and AGENTS.md)

**Files:**
- Modify: `README.md` (add a `### \`tengiz cleanup\`` section to the CLI Reference)
- Modify: `AGENTS.md` (add the command to the CLI list)

**Interfaces:** none (documentation only).

- [ ] **Step 1: Update README.md**

In `README.md`, inside the `## CLI Reference` section, insert a new subsection immediately after the `### \`tengiz build-logs <app> [deployment-id]\`` block (which ends with the sentence "Use `--tail N` to show only the last N lines."):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space on the host.

| Flag | Description |
|------|-------------|
| `--dry-run` | Print the docker commands that would run without removing anything |
| `--all` | Enable all categories, including `--volumes` |
| `--containers` | Remove stopped Tengiz containers (filtered by the `tengiz-app` label) |
| `--images` | Remove dangling images and old per-app images beyond `--keep-images` |
| `--build-cache` | Remove Docker BuildKit build cache |
| `--networks` | Remove unused Docker networks |
| `--volumes` | Remove unused Docker volumes (opt-in: may affect non-Tengiz named volumes) |
| `--keep-images N` | Keep the last N images per app so rollback still works (default: 5, used with `--images`) |

When no category flag is given, `--containers --images --build-cache --networks` are used. Stopped containers are filtered by the `tengiz-app` label, so containers not managed by Tengiz are never removed. `--all` enables every category including `--volumes`. Run `tengiz cleanup --dry-run` first to see exactly which docker commands would execute.
```

- [ ] **Step 2: Update AGENTS.md**

In `AGENTS.md`, in the `## CLI` code block, add a line after the `tengiz ps` line:

```markdown
tengiz cleanup          → prune stopped containers, dangling images, build cache, unused networks (label-scoped; --dry-run, --all, --volumes, --keep-images)
```

- [ ] **Step 3: Verify nothing is broken**

Run: `go build -o /tmp/tengiz . && go vet ./... && go test ./... -count=1`
Expected: build succeeds, vet clean, all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

### Task 7: Mark the feature implemented and final verification

**Files:**
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:** none (docs + verification only).

- [ ] **Step 1: Mark feature #6 as implemented in the Priority Ranking**

In `docs/FUTURES_FEATURES.md`, change the P0 table row for **Docker Housekeeping** (row #6, currently):

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 2: Add the Status line to the feature's detail section**

In the `## Özellikler` section, the feature block `## Docker Housekeeping (Otomatik Temizlik)` currently ends its "Why add to Tengiz" line at "…`tengiz cleanup` komutu eklenebilir.". Add a status line immediately after that "Why add to Tengiz" line:

```markdown
- **Status:** ✅ Implemented (2026-08-14)
```

- [ ] **Step 3: Add a row to the Implemented Features table**

In `docs/FUTURES_FEATURES.md`, in the `### ✅ Implemented Features (Not Pending)` table, add the following row (anywhere in the table):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-14) |
```

- [ ] **Step 4: Final verification**

Run: `go build -o tengiz . && go vet ./... && go test ./... -v -count=1`
Expected: build succeeds, vet clean, ALL tests PASS.

Also manually confirm the new command appears in help:

Run: `./tengiz cleanup --help`
Expected: help text lists `--dry-run`, `--all`, `--containers`, `--images`, `--build-cache`, `--networks`, `--volumes`, `--keep-images`.

- [ ] **Step 5: Commit**

```bash
git add docs/FUTURES_FEATURES.md
git commit -m "docs: mark Docker Housekeeping as implemented"
```

---

## Self-Review

**1. Spec coverage.** The feature (#6 Docker Housekeeping) requires: label-based pruning, a `tengiz cleanup` command, and protection of Tengiz-managed containers. Covered: label-filtered container prune (Task 2/4), dangling-image + build-cache + network/volume pruning with `--keep-images` rollback safety (Tasks 2-5), and the `tengiz cleanup` CLI (Task 5). The Coolify rationale also calls for per-category granularity (stopped containers, images, volumes, networks, build cache) — provided via the per-category flags and `--all`. `--dry-run` is added as a safety-first default so operators can preview before deleting. No gaps.

**2. Placeholder scan.** All code is written inline; no TBD/TODO/“handle edge cases”/“similar to Task N” remains. Every code step shows the exact final content.

**3. Type consistency.** `CleanupOptions` fields (`DryRun`, `Containers`, `Images`, `BuildCache`, `Volumes`, `Networks`, `KeepImages`) and `CleanupResult` fields (`ContainersPruned`, `ImagesPruned`, `BuildCacheFreed`, `VolumesPruned`, `NetworksPruned`) are defined in Task 1 and used identically in Tasks 4 and 5. `buildCleanupCommands`, `parsePruneOutput`, `parseBytes`, `FormatBytes` signatures match their call sites. The CLI uses `newDockerRuntime` (defined in Task 5) and the tests swap it via `withCleanupRuntime`. Mocks updated in Task 1 keep `runtime.Manager` implemented across `root_test.go`, `proxy_test.go`, `idle_test.go`.

**Execution note:** the `implement-top-feature` workflow deletes this plan file after all tasks pass; do not delete it yourself.
