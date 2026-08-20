# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, images, networks, volumes, build cache) to reclaim disk space while always protecting Tengiz-managed containers via the `tengiz-app` label filter.

**Architecture:** A `Cleanup(ctx, opts)` method is added to the existing `runtime.Manager` interface (following the established pattern of `RemoveImage`/`KeepLastNImages`). The `dockerRuntime` implementation shells out to `docker system prune -f` with a `--filter "label!=tengiz-app"` guard, plus optional `-a`/`--volumes` flags. Prune output is parsed by a pure `parsePruneOutput` function so it is unit-testable without Docker. A companion `tengiz cleanup --df` flag shows `docker system df` reclaimable-space summary. A new `internal/cli/cleanup.go` file wires the Cobra command.

**Tech Stack:** Go 1.26, Cobra, `os/exec` (Docker CLI — no Docker SDK), existing `runtime.Manager` interface. No new external dependencies.

## Global Constraints

- Tengiz-managed containers are **always** protected: every prune run appends `--filter "label!=tengiz-app"` — there is no flag to disable this
- `Cleanup` is added to the `runtime.Manager` interface → the `stubManager` and the three test mocks (`internal/cli/root_test.go`, `internal/idle/idle_test.go`, `internal/proxy/proxy_test.go`) MUST be updated in the same task as the interface change or the package won't compile
- No new external dependencies (`go.mod` must not change)
- Default behavior prunes only dangling images; `-a/--all` prunes all unused images
- Volume pruning happens **only** when `-v/--volumes` is passed (Docker's own safety default)
- `-d/--df` performs a read-only `docker system df` and never prunes
- CLI flag names: `-a/--all`, `-v/--volumes`, `-d/--df`
- The command is registered on `rootCmd` so the global `--env` persistent flag applies automatically (cleanup is host-wide; env is accepted but ignored)
- Repo rules: README.md and `docs/FUTURES_FEATURES.md` must be updated in this plan (Task 5)
- Existing tests must continue to pass; only additions allowed are the new mock methods required by the interface change

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Add `CleanupOptions`, `CleanupResult` types, `cleanupArgs()` arg builder, `parsePruneOutput()` parser, `pruneSection()` classifier, `dockerRuntime.Cleanup`, package func `DiskUsage` |
| `internal/runtime/runtime.go` | Add `Cleanup(ctx, opts) (CleanupResult, error)` to `Manager` interface + `stubManager.Cleanup` |
| `internal/runtime/cleanup_test.go` | Tests for `parsePruneOutput`, `cleanupArgs`, `TestStubCleanup` |
| `internal/cli/cleanup.go` | **New** `cleanupCmd` Cobra command (flags `all`, `volumes`, `df`) |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()`; register flags in `Execute()` |
| `internal/cli/cleanup_test.go` | **New** CLI tests: registration, flags, flag parsing |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy` (interface compliance) |
| `internal/idle/idle_test.go` | Add `Cleanup` method to `mockRuntime` (interface compliance) |
| `internal/proxy/proxy_test.go` | Add `Cleanup` method to `mockRuntime` (interface compliance) |
| `README.md` | Document `tengiz cleanup` + add feature bullet |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as ✅ Implemented |

---

### Task 1: `CleanupOptions`/`CleanupResult` types + `Manager.Cleanup` interface + stub + mock updates

**Files:**
- Modify: `internal/runtime/cleanup.go` — add types (top of file, after imports)
- Modify: `internal/runtime/runtime.go:31-49` — add method to `Manager` interface
- Modify: `internal/runtime/runtime.go:113-119` — add `stubManager.Cleanup`
- Modify: `internal/runtime/cleanup_test.go` — add `TestStubCleanup`
- Modify: `internal/cli/root_test.go:98-100` — add method to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:32-33` — add method to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:33-34` — add method to `mockRuntime`

**Interfaces:**
- Consumes: nothing new
- Produces: `type CleanupOptions struct { All, Volumes bool }`, `type CleanupResult struct { ContainersDeleted, ImagesDeleted, NetworksDeleted, VolumesDeleted, BuildCacheDeleted int; ReclaimedSpace string }`, and `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — consumed by Tasks 3 and 4.

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	result, err := m.Cleanup(context.Background(), CleanupOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result != (CleanupResult{}) {
		t.Errorf("expected zero CleanupResult, got %+v", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: FAIL with `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add types to `internal/runtime/cleanup.go`**

Add above the existing `RemoveImage` method (after the imports):

```go
type CleanupOptions struct {
	All     bool
	Volumes bool
}

type CleanupResult struct {
	ContainersDeleted int
	ImagesDeleted     int
	NetworksDeleted   int
	VolumesDeleted    int
	BuildCacheDeleted int
	ReclaimedSpace    string
}
```

- [ ] **Step 4: Add `Cleanup` to the `Manager` interface in `internal/runtime/runtime.go`**

Insert into the interface (after the `KeepLastNImages` line):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

- [ ] **Step 5: Add `stubManager.Cleanup` in `internal/runtime/runtime.go`**

Insert after the `KeepLastNImages` stub:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 6: Add `Cleanup` to the three test mocks (compile fix)**

In `internal/cli/root_test.go`, after the `KeepLastNImages` mock method:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

In `internal/idle/idle_test.go`, after the `KeepLastNImages` mock method:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

In `internal/proxy/proxy_test.go`, after the `KeepLastNImages` mock method:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: PASS

- [ ] **Step 8: Verify all packages still compile**

Run: `go build ./...`

Expected: builds without error

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface with CleanupOptions/CleanupResult types"
```

---

### Task 2: `parsePruneOutput` parser + `pruneSection` classifier

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `parsePruneOutput`, `pruneSection`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupResult` from Task 1
- Produces: `parsePruneOutput(output string) CleanupResult` and `pruneSection(line string) string` — consumed by Task 3's `dockerRuntime.Cleanup`.

The parser must handle real `docker system prune -f` output. With `-f` there is no prompt, but the parser tolerates the `WARNING!` block and any empty sections. Section content lines (container IDs, network names, `untagged:`/`deleted:` image lines, volume names, build-cache IDs) are counted per section; the `Total reclaimed space: X` line sets `ReclaimedSpace`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestParsePruneOutput(t *testing.T) {
	output := `Deleted Containers:
abc123
def456

Deleted Networks:
tengiz-custom-net

Deleted Images:
untagged: tengiz-apps/myapp:production-123
deleted: sha256:abc123def456

Total reclaimed space: 1.234GB`

	got := parsePruneOutput(output)
	if got.ContainersDeleted != 2 {
		t.Errorf("ContainersDeleted = %d, want 2", got.ContainersDeleted)
	}
	if got.NetworksDeleted != 1 {
		t.Errorf("NetworksDeleted = %d, want 1", got.NetworksDeleted)
	}
	if got.ImagesDeleted != 2 {
		t.Errorf("ImagesDeleted = %d, want 2", got.ImagesDeleted)
	}
	if got.VolumesDeleted != 0 {
		t.Errorf("VolumesDeleted = %d, want 0", got.VolumesDeleted)
	}
	if got.BuildCacheDeleted != 0 {
		t.Errorf("BuildCacheDeleted = %d, want 0", got.BuildCacheDeleted)
	}
	if got.ReclaimedSpace != "1.234GB" {
		t.Errorf("ReclaimedSpace = %q, want %q", got.ReclaimedSpace, "1.234GB")
	}
}

func TestParsePruneOutputVolumesAndCache(t *testing.T) {
	output := `Deleted Volumes:
vol1

Deleted Build Cache Objects:
abc123`

	got := parsePruneOutput(output)
	if got.VolumesDeleted != 1 {
		t.Errorf("VolumesDeleted = %d, want 1", got.VolumesDeleted)
	}
	if got.BuildCacheDeleted != 1 {
		t.Errorf("BuildCacheDeleted = %d, want 1", got.BuildCacheDeleted)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	got := parsePruneOutput("")
	if got != (CleanupResult{}) {
		t.Errorf("expected zero CleanupResult, got %+v", got)
	}
}

func TestParsePruneOutputIgnoresWarningBlock(t *testing.T) {
	output := `WARNING! This will remove:
  - all stopped containers
  - all networks not used by at least one container
  - all dangling images
  - all dangling build cache

Deleted Containers:
abc123

Total reclaimed space: 5MB`

	got := parsePruneOutput(output)
	if got.ContainersDeleted != 1 {
		t.Errorf("ContainersDeleted = %d, want 1", got.ContainersDeleted)
	}
	if got.ReclaimedSpace != "5MB" {
		t.Errorf("ReclaimedSpace = %q, want %q", got.ReclaimedSpace, "5MB")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestParsePruneOutput" -v -count=1`

Expected: FAIL with `undefined: parsePruneOutput`

- [ ] **Step 3: Write the minimal implementation**

Add to `internal/runtime/cleanup.go`:

```go
func parsePruneOutput(output string) CleanupResult {
	var result CleanupResult
	section := ""
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			result.ReclaimedSpace = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			section = ""
			continue
		}
		if strings.HasSuffix(line, ":") {
			section = pruneSection(line)
			continue
		}
		switch section {
		case "containers":
			result.ContainersDeleted++
		case "images":
			result.ImagesDeleted++
		case "networks":
			result.NetworksDeleted++
		case "volumes":
			result.VolumesDeleted++
		case "buildcache":
			result.BuildCacheDeleted++
		}
	}
	return result
}

func pruneSection(line string) string {
	switch {
	case strings.HasPrefix(line, "Deleted Containers"):
		return "containers"
	case strings.HasPrefix(line, "Deleted Images"):
		return "images"
	case strings.HasPrefix(line, "Deleted Networks"):
		return "networks"
	case strings.HasPrefix(line, "Deleted Volumes"):
		return "volumes"
	case strings.HasPrefix(line, "Deleted Build Cache"):
		return "buildcache"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestParsePruneOutput" -v -count=1`

Expected: PASS (all four tests)

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add parsePruneOutput parser for docker system prune output"
```

---

### Task 3: `dockerRuntime.Cleanup` + `cleanupArgs` builder + `DiskUsage`

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `cleanupArgs`, `dockerRuntime.Cleanup`, `DiskUsage`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, `parsePruneOutput` from Tasks 1-2
- Produces: `cleanupArgs(opts CleanupOptions) []string`, `dockerRuntime.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` (implements `Manager.Cleanup`), and package-level `DiskUsage(ctx context.Context) (string, error)` — `DiskUsage` is consumed by Task 4's `--df` branch.

The `cleanupArgs` builder is extracted as a pure function (same pattern as `buildLogArgs`, `buildRunArgs`, `resourceArgs`) so it is testable without Docker. The `label!=tengiz-app` filter is **always** appended — there is no way to disable it.

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestCleanupArgs(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want []string
	}{
		{"default protects tengiz", CleanupOptions{},
			[]string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"all images", CleanupOptions{All: true},
			[]string{"system", "prune", "-f", "-a", "--filter", "label!=tengiz-app"}},
		{"volumes", CleanupOptions{Volumes: true},
			[]string{"system", "prune", "-f", "--volumes", "--filter", "label!=tengiz-app"}},
		{"all and volumes", CleanupOptions{All: true, Volumes: true},
			[]string{"system", "prune", "-f", "-a", "--volumes", "--filter", "label!=tengiz-app"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanupArgs(tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("cleanupArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("cleanupArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDockerRuntimeImplementsCleanup(t *testing.T) {
	var iface Manager = &dockerRuntime{}
	if iface == nil {
		t.Fatal("dockerRuntime does not implement Manager")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestCleanupArgs|TestDockerRuntimeImplementsCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupArgs` (the interface method already exists from Task 1, so `TestDockerRuntimeImplementsCleanup` may already pass — that is fine)

- [ ] **Step 3: Write the minimal implementation**

Add to `internal/runtime/cleanup.go`:

```go
func cleanupArgs(opts CleanupOptions) []string {
	args := []string{"system", "prune", "-f"}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	args = append(args, "--filter", "label!=tengiz-app")
	return args
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	cmd := exec.CommandContext(ctx, "docker", cleanupArgs(opts)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return CleanupResult{}, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return parsePruneOutput(string(out)), nil
}

func DiskUsage(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestCleanupArgs|TestDockerRuntimeImplementsCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement dockerRuntime.Cleanup and DiskUsage via docker system prune/df"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:38` — register `cleanupCmd` in `init()`
- Modify: `internal/cli/root.go:1785-1809` — register flags in `Execute()`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.DiskUsage` from Tasks 1-3
- Produces: `cleanupCmd` registered on `rootCmd` with `-a/--all`, `-v/--volumes`, `-d/--df` flags. `tengiz cleanup` prints the summary; `tengiz cleanup --df` prints raw `docker system df` output.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
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

func TestCleanupFlags(t *testing.T) {
	for _, flag := range []string{"all", "volumes", "df"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupFlagParsing(t *testing.T) {
	called := false
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		df, _ := cmd.Flags().GetBool("df")
		if !all {
			t.Error("all = false, want true")
		}
		if !volumes {
			t.Error("volumes = false, want true")
		}
		if df {
			t.Error("df = true, want false")
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--all", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}

func TestCleanupDfFlagParsing(t *testing.T) {
	called := false
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		df, _ := cmd.Flags().GetBool("df")
		if !df {
			t.Error("df = false, want true")
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--df"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Prunes unused Docker resources (stopped containers, unused images, networks,
volumes, and build cache) to reclaim disk space.

Tengiz-managed containers are always protected via the "tengiz-app" label filter,
so scale-to-zero stopped containers and their images are never removed.

Flags:
  -a, --all       Remove all unused images, not just dangling ones
  -v, --volumes   Also remove unused volumes
  -d, --df        Show Docker disk usage summary without pruning anything`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		df, _ := cmd.Flags().GetBool("df")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if df {
			out, err := runtime.DiskUsage(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		}

		result, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{All: all, Volumes: volumes})
		if err != nil {
			return err
		}

		fmt.Printf("[tengiz] cleanup complete\n")
		fmt.Printf("  containers removed: %d\n", result.ContainersDeleted)
		fmt.Printf("  images removed:     %d\n", result.ImagesDeleted)
		fmt.Printf("  networks removed:   %d\n", result.NetworksDeleted)
		fmt.Printf("  volumes removed:    %d\n", result.VolumesDeleted)
		fmt.Printf("  build cache removed: %d\n", result.BuildCacheDeleted)
		if result.ReclaimedSpace != "" {
			fmt.Printf("  space reclaimed:    %s\n", result.ReclaimedSpace)
		}
		return nil
	},
}
```

- [ ] **Step 4: Register the command in `internal/cli/root.go`**

In `init()` (after `rootCmd.AddCommand(notificationCmd)` on line 75), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

In `Execute()` (next to the `buildLogsCmd.Flags()` block on line 1789), add:

```go
	cleanupCmd.Flags().BoolP("all", "a", false, "remove all unused images, not just dangling ones")
	cleanupCmd.Flags().BoolP("volumes", "v", false, "also remove unused volumes")
	cleanupCmd.Flags().BoolP("df", "d", false, "show Docker disk usage summary without pruning")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS (all four tests)

Run: `go build ./...`

Expected: builds without error

- [ ] **Step 6: Run all CLI tests**

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command with --all/--volumes/--df flags"
```

---

### Task 5: Documentation + self-review + full test pass

**Files:**
- Modify: `README.md` — feature bullet + CLI reference section
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as ✅ Implemented

- [ ] **Step 1: Add a feature bullet to `README.md`**

In the Features list (after the `- **Deployment history**` bullet, line 20), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, images, networks, volumes, and build cache; Tengiz-managed containers are always protected.
```

- [ ] **Step 2: Add a CLI reference section to `README.md`**

After the `### \`tengiz ps\`` section (line 150), insert:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space. Prunes stopped containers, unused images, networks, volumes, and build cache.

| Flag | Description |
|------|-------------|
| `-a`, `--all` | Remove all unused images, not just dangling ones |
| `-v`, `--volumes` | Also remove unused volumes |
| `-d`, `--df` | Show Docker disk usage summary (`docker system df`) without pruning anything |

Tengiz-managed containers are always protected via the `tengiz-app` label filter — scale-to-zero stopped containers and their images are never removed. Run `tengiz cleanup --df` first to preview reclaimable space.
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

Update the P0 table row for feature #6 (line 19) from `⬜` to `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "✅ Implemented Features (Not Pending)" table (after line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-20) |
```

Update the "## Docker Housekeeping (Otomatik Temizlik)" entry (after line 380) to add:

```markdown
- **Status:** ✅ Implemented (2026-08-20)
```

- [ ] **Step 4: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS (proxy tests may take ~2s each due to TCP dial timeouts; idle tests are time-sensitive — these are known and acceptable)

- [ ] **Step 5: Run static analysis**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 6: Self-review against spec**

Verify each requirement from `docs/FUTURES_FEATURES.md` #6:
- `tengiz cleanup` command exists ✅ (Task 4)
- Label-based filtering protects Tengiz-managed containers ✅ (`--filter "label!=tengiz-app"` always appended — Tasks 1-3)
- Cleans unused volumes, networks, containers, images ✅ (`docker system prune` + `--volumes` flag — Tasks 3-4)
- Build cache cleaned ✅ (`docker system prune` includes build cache — Task 3)
- No new dependencies ✅ (`go.mod` unchanged)

Placeholder scan: no "TBD"/"TODO"/"implement later" patterns. Every step has complete code.

Type consistency check:
- `CleanupOptions{All, Volumes bool}` — identical across Tasks 1, 3, 4
- `CleanupResult` field names (`ContainersDeleted`, `ImagesDeleted`, `NetworksDeleted`, `VolumesDeleted`, `BuildCacheDeleted`, `ReclaimedSpace`) — identical across Tasks 1, 2, 3, 4
- `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — identical in interface (Task 1), stub (Task 1), mocks (Task 1), dockerRuntime (Task 3), CLI call (Task 4)
- `DiskUsage(ctx context.Context) (string, error)` — defined Task 3, called Task 4

- [ ] **Step 7: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark Docker Housekeeping as implemented"
```