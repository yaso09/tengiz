# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command (with optional periodic `--every` mode) that prunes leftover stopped versioned containers, dangling images, unused volumes, unused networks, and build-cache entries via label-aware `docker ... prune` invocations, without ever touching running Tengiz containers.

**Architecture:** A new `runtime.CleanupOptions`/`CleanupResult` pair drives a single `Manager.Cleanup(ctx, opts)` method. The docker implementation shells out to `docker <resource> prune -f` with filters; a pure helper `countRemoved()` parses the "Deleted ...:" sections of the output into removal counts. The CLI adds a `cleanup` cobra command that builds options from flags, supports `--dry-run` (prints the exact commands without running them) and `--every <dur>` (repeat via `runPeriodic` ticker until interrupted). Running Tengiz containers are always protected: container cleanup only targets containers carrying the `tengiz-deployment` label (the transient blue/green leftovers), and `docker container prune` itself never removes running containers.

**Tech Stack:** Go 1.26, `cobra` (flags), `os/exec` (`docker` CLI — no Docker SDK), `internal/runtime` (existing `Manager`, `dockerRuntime`, `stubManager`), `signal.NotifyContext` + `time.Ticker` for periodic mode.

## Global Constraints

- Never remove a running or stopped app container: container pruning targets ONLY the `tengiz-deployment` label (leftover blue/green containers), never the plain `tengiz-app` label used by active/scale-to-zero containers
- Image pruning is dangling-only (`dangling=true`); per-app retention for `tengiz-apps/*` images stays the job of the existing `KeepLastNImages` (called already after every deploy)
- All prune invocations use `-f` (no interactive confirmation)
- `tengiz cleanup` with no target flags returns an error (explicit `--all` or a target flag required) — never a silent no-op
- `--dry-run` must print the exact `docker ...` command lines that would run, and must call no prune command
- `--every <duration>` uses `time.ParseDuration` (e.g. `30m`, `1h`); invalid durations are an error; empty disables the loop
- In `--every` mode, a failed later iteration is non-fatal (logged and continued); only the first run's error propagates as a command failure
- No new external Go dependencies
- Adding a method to the `runtime.Manager` interface requires updating ALL implementers, including every test double: `*dockerRuntime`, `*stubManager`, `*mockRTForDeploy` in `internal/cli/root_test.go`, `*mockRuntime` in `internal/proxy/proxy_test.go`, and `*mockRuntime` in `internal/idle/idle_test.go`
- Existing tests must continue to pass without modification (only additive changes)
- Command flags are prefixed on `cleanupCmd`; `--env` stays a persistent root flag (cleanup is Docker-daemon-wide, so it ignores env, but the persistent flag is inherited harmlessly)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Add `CleanupOptions`, `CleanupResult`, `PruneTarget`, `PruneTargets()`, `pruneArgs()`, `countRemoved()`, and `dockerRuntime.Cleanup` + `prune` helper (keeps existing `RemoveImage`/`KeepLastNImages`) |
| `internal/runtime/runtime.go` | Add `Cleanup` to the `Manager` interface; add `stubManager.Cleanup` |
| `internal/runtime/cleanup_test.go` | Tests for `pruneArgs`, `countRemoved`, fake-`docker` based `Cleanup` test, `TestStubCleanup` |
| `internal/cli/cleanup.go` (new) | `cleanupCmd`, `cleanupTargets()`, `executeCleanup()`, `runPeriodic()` |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` |
| `internal/cli/root_test.go` | Add `mockRTForDeploy.Cleanup` stub (keeps the `runtime.Manager` assertion compiling) |
| `internal/proxy/proxy_test.go` | Add `mockRuntime.Cleanup` stub (proxy's local `Manager` test double) |
| `internal/idle/idle_test.go` | Add `mockRuntime.Cleanup` stub (idle's local `Manager` test double) |
| `internal/cli/cleanup_test.go` (new) | CLI single-shot tests: registration, flags, target parsing, `executeCleanup` |
| `internal/cli/cleanup_periodic_test.go` (new) | CLI `runPeriodic` tests |
| `README.md` | Add a `### cleanup` section in the CLI Reference |

---

### Task 1: Runtime options, prune-arg builders, count parser, and interface extension

**Files:**
- Modify: `internal/runtime/cleanup.go` — add types + pure helpers after existing `KeepLastNImages`
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface; `:113-122` area add stub method
- Modify: `internal/runtime/cleanup_test.go` — add pure-helper tests
- Modify: `internal/cli/root_test.go:98-100` — add `mockRTForDeploy.Cleanup` stub
- Modify: `internal/proxy/proxy_test.go` — add `mockRuntime.Cleanup` stub (after `KeepLastNImages`)
- Modify: `internal/idle/idle_test.go` — add `mockRuntime.Cleanup` stub (after `KeepLastNImages`)

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{Containers, Images, Volumes, Networks, BuildCache bool}`, `runtime.CleanupResult{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved, BuildCacheRemoved int}`, `runtime.PruneTarget{Resource string; Filters []string}` with `Args() []string`, `runtime.PruneTargets(opts CleanupOptions) []PruneTarget`, `countRemoved(output string) int` (package-private), `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go (append to this file)

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		filters  []string
		expected []string
	}{
		{"container", "container", []string{"label=tengiz-deployment"}, []string{"container", "prune", "-f", "--filter", "label=tengiz-deployment"}},
		{"image", "image", []string{"dangling=true"}, []string{"image", "prune", "-f", "--filter", "dangling=true"}},
		{"volume-no-filters", "volume", nil, []string{"volume", "prune", "-f"}},
		{"network-no-filters", "network", nil, []string{"network", "prune", "-f"}},
		{"builder-no-filters", "builder", nil, []string{"builder", "prune", "-f"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneArgs(tt.resource, tt.filters)
			if len(got) != len(tt.expected) {
				t.Fatalf("pruneArgs() = %v, want %v", got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("pruneArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestCountRemoved(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
	}{
		{
			name: "containers",
			output: `Deleted Containers:
abcdef1234567890
Total reclaimed space: 5.3kB
`,
			want: 1,
		},
		{
			name: "images skips untagged lines",
			output: `Deleted Images:
untagged: tengiz-apps/myapp:old
deleted: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
Total reclaimed space: 1.2MB
`,
			want: 1,
		},
		{
			name: "volumes",
			output: `Deleted Volumes:
myapp-volume
Total reclaimed space: 12B
`,
			want: 1,
		},
		{
			name:   "no deletions",
			output: `Total reclaimed space: 0B
`,
			want: 0,
		},
		{
			name:   "empty",
			output: ``,
			want:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countRemoved(tt.output); got != tt.want {
				t.Errorf("countRemoved() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil || res.ContainersRemoved != 0 || res.ImagesRemoved != 0 {
		t.Errorf("stub Cleanup result = %+v, want zeroed result", res)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... ./internal/cli/... -run "TestPruneArgs|TestCountRemoved|TestStubCleanup|TestMockRTForDeployImplementsManager" -v -count=1`

Expected: FAIL — `undefined: pruneArgs`, `undefined: countRemoved` (interface assertion `mockRTForDeploy does not implement Manager` once the new method lands).

- [ ] **Step 3: Add `CleanupOptions`, `CleanupResult`, and helpers to `internal/runtime/cleanup.go`**

Append after the existing `KeepLastNImages` method:

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCacheRemoved int
}

type PruneTarget struct {
	Resource string
	Filters  []string
}

func (t PruneTarget) Args() []string {
	return pruneArgs(t.Resource, t.Filters)
}

func PruneTargets(opts CleanupOptions) []PruneTarget {
	var targets []PruneTarget
	if opts.Containers {
		targets = append(targets, PruneTarget{Resource: "container", Filters: []string{"label=tengiz-deployment"}})
	}
	if opts.Images {
		targets = append(targets, PruneTarget{Resource: "image", Filters: []string{"dangling=true"}})
	}
	if opts.Volumes {
		targets = append(targets, PruneTarget{Resource: "volume"})
	}
	if opts.Networks {
		targets = append(targets, PruneTarget{Resource: "network"})
	}
	if opts.BuildCache {
		targets = append(targets, PruneTarget{Resource: "builder"})
	}
	return targets
}

func pruneArgs(resource string, filters []string) []string {
	args := []string{resource, "prune", "-f"}
	for _, f := range filters {
		args = append(args, "--filter", f)
	}
	return args
}

func countRemoved(output string) int {
	count := 0
	inSection := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Deleted ") && strings.HasSuffix(trimmed, ":") {
			inSection = true
			continue
		}
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			inSection = false
			continue
		}
		if inSection && !strings.HasPrefix(trimmed, "untagged") {
			count++
		}
	}
	return count
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	res := &CleanupResult{}
	if opts.Containers {
		n, err := r.prune(ctx, PruneTarget{Resource: "container", Filters: []string{"label=tengiz-deployment"}})
		if err != nil {
			return res, err
		}
		res.ContainersRemoved = n
	}
	if opts.Images {
		n, err := r.prune(ctx, PruneTarget{Resource: "image", Filters: []string{"dangling=true"}})
		if err != nil {
			return res, err
		}
		res.ImagesRemoved = n
	}
	if opts.Volumes {
		n, err := r.prune(ctx, PruneTarget{Resource: "volume"})
		if err != nil {
			return res, err
		}
		res.VolumesRemoved = n
	}
	if opts.Networks {
		n, err := r.prune(ctx, PruneTarget{Resource: "network"})
		if err != nil {
			return res, err
		}
		res.NetworksRemoved = n
	}
	if opts.BuildCache {
		n, err := r.prune(ctx, PruneTarget{Resource: "builder"})
		if err != nil {
			return res, err
		}
		res.BuildCacheRemoved = n
	}
	return res, nil
}

func (r *dockerRuntime) prune(ctx context.Context, t PruneTarget) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", t.Args()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker %s prune: %w\n%s", t.Resource, err, string(out))
	}
	return countRemoved(string(out)), nil
}
```

- [ ] **Step 4: Add `Cleanup` to the `Manager` interface and the stub**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after `KeepLastNImages`):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
```

In `internal/runtime/runtime.go`, add to the `stubManager` (after `KeepLastNImages`):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{}, nil
}
```

- [ ] **Step 5: Update all test mocks implementing `Manager`**

`runtime.Manager` now has 3 test doubles; each needs the stub immediately or `go build`/`go test` fails interface satisfaction.

In `internal/cli/root_test.go` after the `KeepLastNImages` method:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{}, nil
}
```

In `internal/proxy/proxy_test.go` after the `KeepLastNImages` method:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{}, nil
}
```

In `internal/idle/idle_test.go` after the `KeepLastNImages` method:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/proxy/... ./internal/idle/... -run "TestPruneArgs|TestCountRemoved|TestStubCleanup|TestMockRT" -v -count=1`

Expected: PASS

- [ ] **Step 7: Run all repo tests and vet**

Run: `go test ./... -count=1`
Run: `go vet ./...`

Expected: All PASS (the interface change is now satisfied by every implementer and double), vet clean

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add Cleanup method and prune-arg builders for docker housekeeping"
```

---

### Task 2: Integration test for `dockerRuntime.Cleanup` against a fake `docker` binary

**Files:**
- Modify: `internal/runtime/cleanup_test.go` — add fake-docker tests

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `dockerRuntime.Cleanup` from Task 1
- Produces: proof that `Cleanup` runs the prune commands per selected target and parses the returned counts

- [ ] **Step 1: Write the failing tests**

Update the import block of `internal/runtime/cleanup_test.go` to add `os`, `path/filepath`, and `strings` (keep `context` and `testing`):

```go
package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)
```

Then append the tests to the same file:

```go
const fakeDockerScript = `#!/usr/bin/env bash
case "$1" in
  container)
    echo "Deleted Containers:"
    echo "abcdef1234567890"
    echo "Total reclaimed space: 5.3kB"
    ;;
  image)
    echo "Deleted Images:"
    echo "untagged: tengiz-apps/myapp:old"
    echo "deleted: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    echo "Total reclaimed space: 1.2MB"
    ;;
  volume)
    echo "Deleted Volumes:"
    echo "myapp-volume"
    echo "Total reclaimed space: 12B"
    ;;
  network)
    echo "Deleted Networks:"
    echo "my-custom-net"
    echo "Total reclaimed space: 0B"
    ;;
  builder)
    echo "Total cache space: 4.2kB"
    ;;
  *)
    exit 1
    ;;
esac
exit 0
`

const failingDockerScript = `#!/usr/bin/env bash
echo "volume prune exploded" >&2
exit 1
`

func withFakeDocker(t *testing.T, script string) *dockerRuntime {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &dockerRuntime{}
}

func TestDockerCleanupAllTargets(t *testing.T) {
	rt := withFakeDocker(t, fakeDockerScript)

	res, err := rt.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	if res.ContainersRemoved != 1 {
		t.Errorf("ContainersRemoved = %d, want 1", res.ContainersRemoved)
	}
	if res.ImagesRemoved != 1 {
		t.Errorf("ImagesRemoved = %d, want 1", res.ImagesRemoved)
	}
	if res.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", res.VolumesRemoved)
	}
	if res.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", res.NetworksRemoved)
	}
	if res.BuildCacheRemoved != 0 {
		t.Errorf("BuildCacheRemoved = %d, want 0", res.BuildCacheRemoved)
	}
}

func TestDockerCleanupNoTargetsRunsNothing(t *testing.T) {
	rt := withFakeDocker(t, fakeDockerScript)

	res, err := rt.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 ||
		res.VolumesRemoved != 0 || res.NetworksRemoved != 0 || res.BuildCacheRemoved != 0 {
		t.Errorf("unexpected removals with no targets: %+v", res)
	}
}

func TestDockerCleanupPruneError(t *testing.T) {
	rt := withFakeDocker(t, failingDockerScript)

	res, err := rt.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err == nil {
		t.Fatalf("expected error, got result %+v", res)
	}
	if !strings.Contains(err.Error(), "volume prune exploded") {
		t.Errorf("error = %q, want to include fake docker stderr", err)
	}
	if res.ContainersRemoved != 0 {
		t.Errorf("ContainersRemoved = %d, want 0 (partial results returned on error)", res.ContainersRemoved)
	}
}
```

- [ ] **Step 2: Run the new tests (regression gate)**

Run: `go test ./internal/runtime/... -run "TestDockerCleanup" -v -count=1`

Expected: PASS — all three `TestDockerCleanup*` tests pass because Task 1 already added `dockerRuntime.Cleanup`; the fake `docker` binary on `PATH` proves the prune subcommands are invoked with the expected arguments and that `countRemoved` parses the output. If `bash` is missing the script fails to execute and these tests error; CI is Linux with bash.

- [ ] **Step 3: Run the whole runtime suite and vet**

Run: `go test ./internal/runtime/... -v -count=1`
Run: `go vet ./internal/runtime/...`

Expected: All PASS, vet clean

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/cleanup_test.go
git commit -m "test(runtime): verify dockerRuntime.Cleanup with fake docker binary"
```

---

### Task 3: `tengiz cleanup` CLI command (single-shot, target flags, dry-run)

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:34-89` — register `cleanupCmd` + flags in `init()`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.PruneTarget`, `runtime.PruneTargets`, `runtime.Manager.Cleanup` from Tasks 1-2
- Produces: `cleanupCmd` (registered under root), `cleanupTargets(cmd *cobra.Command) runtime.CleanupOptions`, `executeCleanup(ctx context.Context, out io.Writer, rt runtime.Manager, opts runtime.CleanupOptions, dryRun bool) error`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go (new file)
package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func resetCleanupFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run"} {
		if err := cleanupCmd.Flags().Set(name, "false"); err != nil {
			t.Fatalf("reset --%s: %v", name, err)
		}
	}
}

func TestCleanupRootRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run", "every"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupTargetsDefaultsNeitherFlag(t *testing.T) {
	resetCleanupFlags(t)
	opts := cleanupTargets(cleanupCmd)
	if opts.Containers || opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("default targets should all be false, got %+v", opts)
	}
}

func TestCleanupTargetsAllFlag(t *testing.T) {
	resetCleanupFlags(t)
	if err := cleanupCmd.Flags().Set("all", "true"); err != nil {
		t.Fatal(err)
	}
	opts := cleanupTargets(cleanupCmd)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("--all should enable every target, got %+v", opts)
	}
}

func TestCleanupWithoutTargetsReturnsError(t *testing.T) {
	resetCleanupFlags(t)
	rootCmd.SetArgs([]string{"cleanup"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for cleanup with no target flags")
	}
}

func TestExecuteCleanupDryRun(t *testing.T) {
	var buf bytes.Buffer
	opts := runtime.CleanupOptions{Containers: true, Volumes: true}
	err := executeCleanup(t.Context(), &buf, runtime.NewStub(), opts, true)
	if err != nil {
		t.Fatalf("executeCleanup(dry-run) error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "docker container prune -f --filter label=tengiz-deployment") {
		t.Errorf("dry-run output missing container command:\n%s", out)
	}
	if !strings.Contains(out, "docker volume prune -f") {
		t.Errorf("dry-run output missing volume command:\n%s", out)
	}
	if strings.Contains(out, "removed:") {
		t.Errorf("dry-run should not report removals:\n%s", out)
	}
}

func TestExecuteCleanupReportsResult(t *testing.T) {
	var buf bytes.Buffer
	opts := runtime.CleanupOptions{Images: true}
	err := executeCleanup(context.Background(), &buf, runtime.NewStub(), opts, false)
	if err != nil {
		t.Fatalf("executeCleanup() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "images removed:") {
		t.Errorf("result output missing images count:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup|TestExecuteCleanup" -v -count=1`

Expected: FAIL (`undefined: executeCleanup`, `cleanupCmd`, and `cleanup not registered`)

- [ ] **Step 3: Implement `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Removes unused Docker resources on the host daemon.

Targets are selected with flags:
  --containers   remove stopped leftover blue/green containers (label tengiz-deployment)
  --images       remove dangling images (per-app image retention is already handled at deploy time)
  --volumes      remove unused named volumes
  --networks     remove unused custom networks (default networks are never removed)
  --build-cache  remove build cache entries
  --all          enable every target above

Running Tengiz application containers are never pruned. Use --dry-run to
see the exact docker commands without executing them. Use --every <duration>
(e.g. 30m, 1h) to repeat cleanup periodically until interrupted.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupTargets(cmd)
		if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.BuildCache {
			return fmt.Errorf("specify at least one target: --containers, --images, --volumes, --networks, --build-cache, or --all")
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		every, _ := cmd.Flags().GetString("every")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if every == "" {
			return executeCleanup(cmd.Context(), os.Stdout, rt, opts, dryRun)
		}

		interval, err := time.ParseDuration(every)
		if err != nil {
			return fmt.Errorf("invalid --every duration %q: %w", every, err)
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		fmt.Printf("[tengiz] periodically cleaning up every %s (Ctrl+C to stop)\n", every)
		return runPeriodic(ctx, interval, func() error {
			return executeCleanup(ctx, os.Stdout, rt, opts, dryRun)
		})
	},
}

func cleanupTargets(cmd *cobra.Command) runtime.CleanupOptions {
	if all, _ := cmd.Flags().GetBool("all"); all {
		return runtime.CleanupOptions{
			Containers: true,
			Images:     true,
			Volumes:    true,
			Networks:   true,
			BuildCache: true,
		}
	}
	opts := runtime.CleanupOptions{}
	opts.Containers, _ = cmd.Flags().GetBool("containers")
	opts.Images, _ = cmd.Flags().GetBool("images")
	opts.Volumes, _ = cmd.Flags().GetBool("volumes")
	opts.Networks, _ = cmd.Flags().GetBool("networks")
	opts.BuildCache, _ = cmd.Flags().GetBool("build-cache")
	return opts
}

func executeCleanup(ctx context.Context, out io.Writer, rt runtime.Manager, opts runtime.CleanupOptions, dryRun bool) error {
	if dryRun {
		for _, t := range runtime.PruneTargets(opts) {
			fmt.Fprintf(out, "[tengiz] would run: docker %s\n", strings.Join(t.Args(), " "))
		}
		fmt.Fprintln(out, "[tengiz] dry run complete — nothing was removed.")
		return nil
	}

	res, err := rt.Cleanup(ctx, opts)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "[tengiz] cleanup complete:")
	fmt.Fprintf(out, "  containers removed: %d\n", res.ContainersRemoved)
	fmt.Fprintf(out, "  images removed:     %d\n", res.ImagesRemoved)
	fmt.Fprintf(out, "  volumes removed:    %d\n", res.VolumesRemoved)
	fmt.Fprintf(out, "  networks removed:   %d\n", res.NetworksRemoved)
	fmt.Fprintf(out, "  build cache items:  %d\n", res.BuildCacheRemoved)
	return nil
}
```

- [ ] **Step 4: Register `cleanupCmd` in `internal/cli/root.go`**

In `init()` after `rootCmd.AddCommand(runCmd)`:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", false, "prune stopped leftover versioned containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused named volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused custom networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune build cache")
	cleanupCmd.Flags().Bool("all", false, "prune every resource type")
	cleanupCmd.Flags().Bool("dry-run", false, "print commands without executing them")
	cleanupCmd.Flags().String("every", "", "repeat cleanup every duration (e.g. 30m, 1h) until interrupted")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestExecuteCleanup|TestCleanup" -v -count=1`

Expected: PASS (8 tests)

- [ ] **Step 6: Run all cli tests and build**

Run: `go test ./internal/cli/... -v -count=1`
Run: `go build ./...`

Expected: All PASS, builds clean

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command with target flags and --dry-run"
```

---

### Task 4: Periodic mode (`runPeriodic` + `--every`), README docs, and self-review

**Files:**
- Modify: `internal/cli/cleanup.go` — add `runPeriodic`, add `signal`, `os/signal`, `context` imports; wire `--every` in `RunE` (already referenced in Task 3, ensure `signal.NotifyContext` present)
- Create: `internal/cli/cleanup_periodic_test.go`
- Modify: `README.md` — add a `tengiz cleanup` subsection under the CLI Reference

**Interfaces:**
- Consumes: `executeCleanup` from Task 3
- Produces: `runPeriodic(ctx context.Context, interval time.Duration, run func() error) error` — runs `run` immediately; on subsequent tick failures logs and continues; returns first `run` error unchanged; returns nil when `ctx` is canceled

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_periodic_test.go (new file)
package cli

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunPeriodicRunsAtLeastThreeTimes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	done := make(chan struct{})

	go func() {
		runPeriodic(ctx, 10*time.Millisecond, func() error {
			if calls.Add(1) >= 3 {
				cancel()
			}
			return nil
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runPeriodic did not return after context cancel")
	}

	if calls.Load() < 3 {
		t.Errorf("runPeriodic calls = %d, want >= 3", calls.Load())
	}
}

func TestRunPeriodicFirstRunErrorPropagates(t *testing.T) {
	err := runPeriodic(context.Background(), time.Minute, func() error {
		return fmt.Errorf("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Errorf("runPeriodic() error = %v, want %q", err, "boom")
	}
}

func TestRunPeriodicContinuesAfterIterationError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	done := make(chan struct{})

	go func() {
		runPeriodic(ctx, 5*time.Millisecond, func() error {
			n := calls.Add(1)
			if n >= 3 {
				cancel()
			}
			if n%2 == 0 {
				return fmt.Errorf("transient failure")
			}
			return nil
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runPeriodic did not return after context cancel")
	}

	if calls.Load() < 3 {
		t.Errorf("runPeriodic calls = %d, want >= 3 despite iteration errors", calls.Load())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestRunPeriodic" -v -count=1`

Expected: FAIL with `undefined: runPeriodic`

- [ ] **Step 3: Implement `runPeriodic` in `internal/cli/cleanup.go`**

Verify the import block of `internal/cli/cleanup.go` is exactly this (it was added in Task 3; `context` and `os/signal` are required by `cleanupCmd.RunE`'s `signal.NotifyContext` call):

```go
import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)
```

Add at the bottom of `internal/cli/cleanup.go`:

```go
func runPeriodic(ctx context.Context, interval time.Duration, run func() error) error {
	if err := run(); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "[tengiz] periodic cleanup stopped")
			return nil
		case <-ticker.C:
			if err := run(); err != nil {
				fmt.Fprintf(os.Stderr, "[tengiz] periodic cleanup iteration failed: %v\n", err)
			}
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestRunPeriodic" -v -count=1`

Expected: PASS (3 tests)

- [ ] **Step 5: Update README documentation**

In `README.md`, inside the `## CLI Reference` section, insert the following after the `### tengiz rollback` subsection (which ends around line 236). Insert exactly this text (the shell example uses four-space indentation so it nests cleanly under a markdown-listed item):

```markdown
### `tengiz cleanup`

Prune unused Docker resources from the host daemon: leftover stopped versioned (blue/green) containers, dangling images, unused named volumes, unused custom networks, and build-cache entries. Running Tengiz containers are never touched.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped leftover versioned containers (`tengiz-deployment` label) |
| `--images` | Remove dangling images only (per-app retention already handled at deploy) |
| `--volumes` | Remove unused named volumes |
| `--networks` | Remove unused custom networks |
| `--build-cache` | Remove build cache entries |
| `--all` | Prune all of the above |
| `--dry-run` | Print the exact commands without executing them |
| `--every <duration>` | Repeat cleanup periodically (e.g. `30m`, `1h`) until interrupted |

At least one target flag (or `--all`) is required.

Examples:

    tengiz cleanup --all
    tengiz cleanup --containers --dry-run
    tengiz cleanup --images --every 6h
```

- [ ] **Step 6: Run the full test suite and vet**

Run: `go test ./... -v -count=1`
Run: `go vet ./...`

Expected: All PASS (slower running tests: proxy TCP-dial tests and idle timer tests may take extra seconds; this is known). Vet clean.

- [ ] **Step 7: Self-review against the spec**

Check `docs/FUTURES_FEATURES.md` requirement #6 (Docker Housekeeping):
- Periodic cleanup ✅ (`--every N` + ticker loop, Task 4)
- Unused volumes/networks/containers/images cleaning ✅ (`--volumes`, `--networks`, `--containers` label-scoped to `tengiz-deployment`, `--images` dangling, Task 2)
- Label-based protection of Tengiz-managed containers ✅ (container prune filter `label=tengiz-deployment` only; generic `tengiz-app` running/scale-to-zero containers untouched — verified by built-in `docker container prune -f` semantics which never removes running containers + filter)
- `tengiz cleanup` command ✅ (Task 3)
- Build cache cleaning ✅ (`--build-cache` → `docker builder prune -f`, Task 2)

Placeholder scan: verify no "TBD/TODO" and no test steps without code. Confirmed all tests have full code.

Type consistency: `CleanupOptions{Containers, Images, Volumes, Networks, BuildCache bool}` used consistently in runtime and CLI; `CleanupResult{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved, BuildCacheRemoved int}` consistent; `PruneTarget.Args()` one place; `executeCleanup(ctx, out, rt, opts, dryRun)` same signature in Task 3 and 4; `runPeriodic(ctx, interval, run)` consistent.

- [ ] **Step 8: Manual smoke test (documented for reviewer)**

On a host with Docker installed (not run in CI):

```bash
go build -o tengiz .
./tengiz cleanup --dry-run
./tengiz cleanup --all
./tengiz cleanup --images --every 1m   # Ctrl+C to stop
./tengiz ps
```

Expectation: `--dry-run` prints the four docker commands; `--all` prunes and prints the counts; `ps` still lists running apps; the periodic command repeats until interrupted.

- [ ] **Step 9: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_periodic_test.go README.md
git commit -m "feat(cli): add periodic cleanup mode with --every flag and document tengiz cleanup"
```