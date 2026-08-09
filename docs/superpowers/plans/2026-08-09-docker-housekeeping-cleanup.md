# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that runs label-aware Docker housekeeping (`docker system prune`) so operators can reclaim disk space on single-server deployments without ever touching Tengiz-managed containers or their images.

**Architecture:** A new `Cleanup(ctx, CleanupOptions)` method on the `runtime.Manager` interface builds the exact `docker system prune` command via a pure, unit-testable arg builder. The command always passes `--filter label!=tengiz-app`, so stopped containers/images/networks/volumes that carry the `tengiz-app` label (everything Tengiz creates) are excluded from pruning; Docker additionally never prunes images/volumes/networks still referenced by a protected container. `--all` additionally removes unused (non-dangling) images, `--volumes` additionally prunes anonymous volumes, and `--dry-run` lists what would be removed plus a `docker system df` report without changing anything. The CLI wires these flags to the runtime call and prints a human-readable summary.

**Tech Stack:** Go 1.26, `os/exec` (Docker CLI), Cobra (CLI), existing `runtime.Manager` interface and `dockerRuntime`/`stubManager` implementations.

## Global Constraints

- Every Docker command must keep the `--filter label!=tengiz-app` argument so Tengiz-managed resources are never pruned
- Tengiz containers are labeled `tengiz-app=<appname>` (set in `internal/runtime/docker.go`); preview containers get the label via the same `Create` path
- Default `tengiz cleanup` must NOT remove tagged images (dangling-only), so rollback images are preserved; `--all` is documented as aggressive
- `cleanup` is a host-level operation and is NOT env-scoped — it operates on the whole Docker daemon (the `--env` root persistent flag is ignored)
- No new external dependencies
- The `runtime.Manager` interface gains `Cleanup` and `DiskUsage`; every existing mock of the interface MUST be updated in the same task (compile guarantees this)
- Arg-builder and output-parser functions are pure and fully unit-tested; integration tests `t.Skip` when Docker is unavailable (existing repo pattern)
- Feature branch: create `feat/docker-housekeeping` before starting (`git checkout -b feat/docker-housekeeping`)
- Existing tests must continue to pass without modification (except the required mock updates)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `CleanupOptions`/`CleanupResult` types, pure docker-arg builders (`buildCleanupArgs`, `buildStoppedContainersArgs`, `buildDfArgs`), `parseCleanupOutput`, and the `dockerRuntime.Cleanup`/`DiskUsage`/`listStoppedNonTengizContainers` methods |
| `internal/runtime/runtime.go` | Add `Cleanup` + `DiskUsage` to `Manager` interface; implement both on `stubManager` |
| `internal/runtime/cleanup_test.go` | Unit tests for arg builders + `parseCleanupOutput`; stub/interface test; skip-if-no-docker integration tests |
| `internal/cli/root.go` | New `cleanupCmd` with `--all`, `--volumes`, `--dry-run` flags; registered in `init()` |
| `internal/cli/cleanup_test.go` | CLI registration, flag, and `--help` text tests |
| `internal/cli/root_test.go` | Add `Cleanup` + `DiskUsage` methods to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` + `DiskUsage` methods to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` + `DiskUsage` methods to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as implemented |

---

### Task 1: Cleanup types + pure docker-arg builders

**Files:**
- Modify: `internal/runtime/cleanup.go` — add types and pure helper functions at the bottom of the file
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{All, Volumes, DryRun bool}`, `runtime.CleanupResult{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved, BuildCacheRemoved int, Reclaimed string, DryRun bool}`, `buildCleanupArgs(opts CleanupOptions) []string`, `buildStoppedContainersArgs() []string`, `buildDfArgs() []string`, `parseCleanupOutput(out string) CleanupResult`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go
package runtime

import (
	"reflect"
	"testing"
)

func TestBuildCleanupArgs(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want []string
	}{
		{
			name: "default",
			opts: CleanupOptions{},
			want: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name: "all",
			opts: CleanupOptions{All: true},
			want: []string{"system", "prune", "-a", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name: "volumes",
			opts: CleanupOptions{Volumes: true},
			want: []string{"system", "prune", "-f", "--volumes", "--filter", "label!=tengiz-app"},
		},
		{
			name: "all and volumes",
			opts: CleanupOptions{All: true, Volumes: true},
			want: []string{"system", "prune", "-a", "-f", "--volumes", "--filter", "label!=tengiz-app"},
		},
		{
			name: "dry run only",
			opts: CleanupOptions{DryRun: true},
			want: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCleanupArgs(tt.opts)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildCleanupArgs(%+v) = %v, want %v", tt.opts, got, tt.want)
			}
		})
	}
}

func TestBuildStoppedContainersArgs(t *testing.T) {
	want := []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=dead",
		"--filter", "label!=tengiz-app",
		"--format", "{{.Names}}",
	}
	got := buildStoppedContainersArgs()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildStoppedContainersArgs() = %v, want %v", got, want)
	}
}

func TestBuildDfArgs(t *testing.T) {
	want := []string{"system", "df"}
	got := buildDfArgs()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildDfArgs() = %v, want %v", got, want)
	}
}

func TestParseCleanupOutput(t *testing.T) {
	sample := `Deleted Containers:
c1abc123
c2def456

Deleted Networks:
net1

Deleted Images:
img1
img2
img3

Deleted Build Cache Objects:
b1

Total reclaimed space: 1.234GB
`
	res := parseCleanupOutput(sample)
	if res.ContainersRemoved != 2 {
		t.Errorf("ContainersRemoved = %d, want 2", res.ContainersRemoved)
	}
	if res.NetworksRemoved != 1 {
		t.Errorf("NetworksRemoved = %d, want 1", res.NetworksRemoved)
	}
	if res.ImagesRemoved != 3 {
		t.Errorf("ImagesRemoved = %d, want 3", res.ImagesRemoved)
	}
	if res.BuildCacheRemoved != 1 {
		t.Errorf("BuildCacheRemoved = %d, want 1", res.BuildCacheRemoved)
	}
	if res.VolumesRemoved != 0 {
		t.Errorf("VolumesRemoved = %d, want 0", res.VolumesRemoved)
	}
	if res.Reclaimed != "Total reclaimed space: 1.234GB" {
		t.Errorf("Reclaimed = %q, want %q", res.Reclaimed, "Total reclaimed space: 1.234GB")
	}
	if res.DryRun {
		t.Error("DryRun should be false for parsed prune output")
	}
}

func TestParseCleanupOutputEmpty(t *testing.T) {
	res := parseCleanupOutput("Total reclaimed space: 0B\n")
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 ||
		res.NetworksRemoved != 0 || res.BuildCacheRemoved != 0 || res.VolumesRemoved != 0 {
		t.Errorf("expected all-zero counts, got %+v", res)
	}
	if res.Reclaimed != "Total reclaimed space: 0B" {
		t.Errorf("Reclaimed = %q, want %q", res.Reclaimed, "Total reclaimed space: 0B")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestBuildCleanupArgs|TestBuildStoppedContainersArgs|TestBuildDfArgs|TestParseCleanupOutput" -v -count=1`

Expected: FAIL with `undefined: buildCleanupArgs` (and the other helpers)

- [ ] **Step 3: Write the pure helper functions**

Append to the end of `internal/runtime/cleanup.go`:

```go
type CleanupOptions struct {
	All     bool // also remove unused (non-dangling) images
	Volumes bool // also remove unused volumes
	DryRun  bool // report what would be removed without removing anything
}

type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCacheRemoved int
	Reclaimed         string // "Total reclaimed space: X" line from docker output
	DryRun            bool
}

func buildCleanupArgs(opts CleanupOptions) []string {
	args := []string{"system", "prune"}
	if opts.All {
		args = append(args, "-a")
	}
	args = append(args, "-f")
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	args = append(args, "--filter", "label!=tengiz-app")
	return args
}

func buildStoppedContainersArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", "status=dead",
		"--filter", "label!=tengiz-app",
		"--format", "{{.Names}}",
	}
}

func buildDfArgs() []string {
	return []string{"system", "df"}
}

func parseCleanupOutput(out string) CleanupResult {
	var res CleanupResult
	section := ""
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.Contains(trimmed, "Total reclaimed space"):
			res.Reclaimed = trimmed
		case strings.Contains(trimmed, "Deleted Containers"):
			section = "containers"
		case strings.Contains(trimmed, "Deleted Images"):
			section = "images"
		case strings.Contains(trimmed, "Deleted Volumes"):
			section = "volumes"
		case strings.Contains(trimmed, "Deleted Networks"):
			section = "networks"
		case strings.Contains(trimmed, "Deleted Build Cache Objects"):
			section = "buildcache"
		case trimmed != "" && !strings.HasSuffix(trimmed, ":"):
			switch section {
			case "containers":
				res.ContainersRemoved++
			case "images":
				res.ImagesRemoved++
			case "volumes":
				res.VolumesRemoved++
			case "networks":
				res.NetworksRemoved++
			case "buildcache":
				res.BuildCacheRemoved++
			}
		}
	}
	return res
}
```

Note: `internal/runtime/cleanup.go` already imports `"strings"` (used by `KeepLastNImages`), so no import changes are needed for the pure helpers.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestBuildCleanupArgs|TestBuildStoppedContainersArgs|TestBuildDfArgs|TestParseCleanupOutput" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add cleanup types and docker prune arg builders"
```

---

### Task 2: Add `Cleanup` and `DiskUsage` to the Manager interface + dockerRuntime + all mocks

**Files:**
- Modify: `internal/runtime/runtime.go` — add `Cleanup` + `DiskUsage` to `Manager` interface and to `stubManager`
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.Cleanup`, `listStoppedNonTengizContainers`, `DiskUsage`
- Modify: `internal/cli/root_test.go:98-99` — add the two methods to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:34` — add the two methods to `mockRuntime`
- Modify: `internal/idle/idle_test.go:33` — add the two methods to `mockRuntime`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, `buildCleanupArgs`, `buildStoppedContainersArgs`, `buildDfArgs`, `parseCleanupOutput` from Task 1
- Produces: `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`, `runtime.Manager.DiskUsage(ctx context.Context) (string, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go
func TestStubImplementsCleanup(t *testing.T) {
	var m Manager = NewStub()
	if _, err := m.Cleanup(context.Background(), CleanupOptions{}); err != nil {
		t.Fatalf("stub Cleanup() error = %v", err)
	}
	if _, err := m.DiskUsage(context.Background()); err != nil {
		t.Fatalf("stub DiskUsage() error = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubImplementsCleanup" -v -count=1`

Expected: FAIL with `stubManager does not implement Manager (missing method Cleanup)` at compile time — this proves the interface change is required and forces the mock updates everywhere.

- [ ] **Step 3: Add methods to the `Manager` interface**

In `internal/runtime/runtime.go`, inside the `Manager` interface (after the `KeepLastNImages` line):

```go
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
	DiskUsage(ctx context.Context) (string, error)
```

- [ ] **Step 4: Implement `stubManager` methods**

In `internal/runtime/runtime.go`, after `func (m *stubManager) KeepLastNImages(...)`:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}

func (m *stubManager) DiskUsage(ctx context.Context) (string, error) {
	return "", nil
}
```

- [ ] **Step 5: Implement `dockerRuntime` methods**

Append to `internal/runtime/cleanup.go` (after `KeepLastNImages`):

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	if opts.DryRun {
		containers, err := r.listStoppedNonTengizContainers(ctx)
		if err != nil {
			return CleanupResult{}, err
		}
		return CleanupResult{
			ContainersRemoved: len(containers),
			DryRun:            true,
		}, nil
	}

	cmd := exec.CommandContext(ctx, "docker", buildCleanupArgs(opts)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return CleanupResult{}, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	res := parseCleanupOutput(string(out))
	res.DryRun = false
	return res, nil
}

func (r *dockerRuntime) listStoppedNonTengizContainers(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildStoppedContainersArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func (r *dockerRuntime) DiskUsage(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildDfArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}
```

- [ ] **Step 6: Add `Cleanup` + `DiskUsage` to the three mock implementations**

`internal/cli/root_test.go` (add after the `KeepLastNImages` line):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
func (m *mockRTForDeploy) DiskUsage(ctx context.Context) (string, error) { return "", nil }
```

`internal/proxy/proxy_test.go` (add after the `KeepLastNImages` line):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
func (m *mockRuntime) DiskUsage(ctx context.Context) (string, error) { return "", nil }
```

`internal/idle/idle_test.go` (add after the `KeepLastNImages` line):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
func (m *mockRuntime) DiskUsage(ctx context.Context) (string, error) { return "", nil }
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestStubImplementsCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 8: Add skip-if-no-docker integration tests**

```go
// internal/runtime/cleanup_test.go
func TestDockerCleanupIntegration(t *testing.T) {
	rt, err := NewDocker()
	if err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	res, err := rt.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.DryRun {
		t.Error("expected DryRun=false for real cleanup")
	}
}

func TestDockerCleanupDryRunIntegration(t *testing.T) {
	rt, err := NewDocker()
	if err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	res, err := rt.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup(dry-run) error = %v", err)
	}
	if !res.DryRun {
		t.Error("expected DryRun=true for dry-run cleanup")
	}
}

func TestDockerDiskUsageIntegration(t *testing.T) {
	rt, err := NewDocker()
	if err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	out, err := rt.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if !strings.Contains(out, "TYPE") || !strings.Contains(out, "RECLAIMABLE") {
		t.Errorf("DiskUsage() output missing docker system df headers, got: %q", out)
	}
}
```

The integration tests above need `"context"` and `"strings"` imports in `internal/runtime/cleanup_test.go` — the file currently imports only `"context"` and `"testing"`.

- [ ] **Step 9: Run all runtime + dependent package tests**

Run: `go build ./...`

Expected: builds cleanly

Run: `go test ./internal/runtime/... ./internal/proxy/... ./internal/idle/... ./internal/cli/... -v -count=1`

Expected: All PASS (proxy tests are slow ~2s each; idle tests are time-sensitive)

- [ ] **Step 10: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add Cleanup and DiskUsage to runtime.Manager interface"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` and register it in `init()`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.Manager.Cleanup`, `runtime.Manager.DiskUsage` from Tasks 1-2
- Produces: `tengiz cleanup [--all] [--volumes] [--dry-run]` command

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil || cmd == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"all", "volumes", "dry-run"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupHelpText(t *testing.T) {
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

	helpText := buf.String()
	for _, s := range []string{"--all", "--volumes", "--dry-run", "tengiz-app"} {
		if !strings.Contains(helpText, s) {
			t.Errorf("help text missing %q", s)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`

- [ ] **Step 3: Add the command and register it**

In `internal/cli/root.go` `init()`, add the registration (near the other `rootCmd.AddCommand(...)` calls):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("all", false, "also remove unused (non-dangling) images")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without removing anything")
```

Add the command definition after `runCmd` (anywhere after the imports and before the helper functions):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (label-aware housekeeping)",
	Long: `Remove unused Docker resources not managed by Tengiz to reclaim disk space.

By default removes:
  - stopped containers NOT labeled tengiz-app
  - dangling (untagged) images
  - unused networks
  - unused build cache

Use --all to also remove unused tagged images (this may remove rollback images).
Use --volumes to also remove unused anonymous volumes.
Use --dry-run to preview what would be removed without changing anything.

Containers labeled tengiz-app (and the images, volumes, and networks they reference)
are always protected.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.CleanupOptions{All: all, Volumes: volumes, DryRun: dryRun}

		res, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}

		if dryRun {
			fmt.Println("[tengiz] dry-run: no resources removed")
			fmt.Printf("[tengiz] stopped non-Tengiz containers that would be removed: %d\n", res.ContainersRemoved)
		} else {
			fmt.Printf("[tengiz] cleanup complete: %d containers, %d images, %d volumes, %d networks, %d build cache objects removed\n",
				res.ContainersRemoved, res.ImagesRemoved, res.VolumesRemoved, res.NetworksRemoved, res.BuildCacheRemoved)
			if res.Reclaimed != "" {
				fmt.Printf("[tengiz] %s\n", res.Reclaimed)
			}
		}

		df, dfErr := rt.DiskUsage(cmd.Context())
		if dfErr != nil {
			log.Printf("[tengiz] warning: docker system df: %v", dfErr)
			return nil
		}
		fmt.Print(df)
		return nil
	},
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: builds cleanly

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for label-aware docker housekeeping"
```

---

### Task 4: Documentation + feature status + full verification

**Files:**
- Modify: `README.md` — add `tengiz cleanup` section to CLI Reference
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented
- Test: full suite + vet

- [ ] **Step 1: Add README documentation**

Insert a new section after the `tengiz rm <app>` section (after line ~228, before `tengiz rollback`):

```markdown
### `tengiz cleanup`

Prune unused Docker resources that are NOT managed by Tengiz, reclaiming disk space on
single-server deployments.

By default removes:
- stopped containers **not** labeled `tengiz-app`
- dangling (untagged) images
- unused networks
- unused build cache

Flags:

| Flag | Description |
|------|-------------|
| `--all` | Also remove unused (non-dangling) tagged images. This may remove rollback images. |
| `--volumes` | Also remove unused anonymous volumes. |
| `--dry-run` | Preview what would be removed and show `docker system df` without removing anything. |

Containers labeled `tengiz-app` (everything Tengiz deploys, including previews) and the images,
volumes, and networks they reference are always protected. This command is host-wide and ignores `--env`.
```

- [ ] **Step 2: Update FUTURES_FEATURES.md**

In the P0 table, change row #6 status:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | ... |
```

And add a row to the "Implemented Features (Not Pending)" table:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-09) |
```

Also update the detailed `## Docker Housekeeping (Otomatik Temizlik)` section — add `- **Status:** ✅ Implemented (2026-08-09)` after the `- **Detected:** 2026-07-14` line.

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS (proxy tests are slow; idle tests are time-sensitive)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 4: Verify the binary help output manually**

Run: `go build -o tengiz . && ./tengiz cleanup --help`

Expected: help text shows `cleanup` with `--all`, `--volumes`, `--dry-run` flags

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** (`docs/FUTURES_FEATURES.md` #6 "Docker Housekeeping"):
- Label-based filtering protecting Tengiz containers → Task 1 (`label!=tengiz-app` arg) + Task 2 (`dockerRuntime.Cleanup`)
- `tengiz cleanup` command → Task 3
- Prunes unused containers/images/networks/build cache (disk reclamation) → Tasks 1-3
- Rollback images preserved by default → Global Constraint + default dangling-only prune
- No gaps; periodic/daemon mode intentionally excluded (YAGNI — cron can schedule `tengiz cleanup`).

**2. Placeholder scan:** No "TBD"/"TODO"/"implement later" text; every code step contains complete, compilable code. Integration tests follow the existing `t.Skip` pattern.

**3. Type consistency:**
- `CleanupOptions{All, Volumes, DryRun bool}` — identical in Task 1 and all usages (Tasks 2-3)
- `CleanupResult{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved, BuildCacheRemoved int, Reclaimed string, DryRun bool}` — defined once in Task 1, used in Tasks 2-3
- `Cleanup(ctx, opts CleanupOptions) (CleanupResult, error)` and `DiskUsage(ctx) (string, error)` — same signatures on the interface, `dockerRuntime`, `stubManager`, and all three mocks
- All four `Manager` mock implementations updated in Task 2 Step 6 — the interface compile check enforces this
- `buildCleanupArgs`, `buildStoppedContainersArgs`, `buildDfArgs`, `parseCleanupOutput` names consistent across Tasks 1-2
