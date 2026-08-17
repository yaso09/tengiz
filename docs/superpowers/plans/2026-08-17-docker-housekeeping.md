# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, networks, volumes) via label-based filtering, protecting all Tengiz-managed containers (running AND stopped, since scale-to-zero relies on stopped containers for cold starts).

**Architecture:** The `runtime.Manager` interface gains a `Cleanup(ctx, opts CleanupOptions) (CleanupResult, error)` method. The `dockerRuntime` implementation shells out to per-category Docker prune commands (`docker container prune`, `docker image prune`, `docker network prune`, `docker volume prune`) through the existing `os/exec` pattern. Tengiz containers are protected with the label filter `label!=tengiz-app`. All command construction and output parsing live in small pure functions (`containerPruneArgs`, `imagePruneArgs`, `networkPruneArgs`, `volumePruneArgs`, `countDeletedLines`, `countPrunedItems`, `parseReclaimedBytes`) so they are unit-testable without Docker. The CLI wires flags → `runtime.CleanupOptions` and prints a human-readable report.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface, `os/exec` Docker CLI passthrough. No new external dependencies — parsing uses stdlib `regexp`/`strconv`.

## Global Constraints

- Command name must be `tengiz cleanup` (from spec: "`tengiz cleanup`")
- Label-based filtering must protect Tengiz-managed containers: use `--filter label!=tengiz-app` on the container prune; running AND stopped app containers (scale-to-zero cold-start state) must NEVER be removed
- Granular per-category cleaning required (containers, images, networks, volumes) — matches Coolify's `DockerCleanupJob` reference
- Volumes are never pruned by default (data loss risk) — only via explicit `--volumes` or `--all`
- No new external dependencies (stdlib `regexp`, `strconv` only)
- Follow existing repo patterns: `os/exec` docker passthrough, `[tengiz]` output prefix, cobra command registered in `init()`, flags testable
- `runtime.Manager` is implemented by exactly 4 types — adding a method requires updating all of them: `stubManager` (runtime.go), `mockRTForDeploy` (root_test.go), `mockRuntime` (proxy_test.go), `mockRuntime` (idle_test.go)
- Existing tests must continue to pass without modification (mocks only gain methods, nothing removed)
- Docker prune commands require Docker 17.06+ for `label!=` negative filters (safe assumption; Tengiz already requires Docker)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Add `CleanupOptions`, `CleanupResult`, pure arg-builder helpers, output parsers, `dockerRuntime.Cleanup()` |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager.Cleanup` |
| `internal/runtime/cleanup_test.go` | Tests: arg builders, output parsers, stub `Cleanup` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` method to existing `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` method to existing `mockRuntime` |
| `internal/cli/root.go` | Add `cleanupCmd` + `cleanupOptionsFromFlags` + `humanizeBytes` helpers |
| `internal/cli/cleanup_test.go` (new) | Tests: command registration, flag resolution, `humanizeBytes` |
| `internal/cli/root_test.go` | Add `Cleanup` method to existing `mockRTForDeploy` |
| `README.md` | Document `tengiz cleanup` command |
| `AGENTS.md` | Add command to Commands section |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 implemented |

---

### Task 1: Runtime prune helpers (pure functions)

**Files:**
- Modify: `internal/runtime/cleanup.go` (add imports + helpers)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `containerPruneArgs() []string` → `["container", "prune", "-f", "--filter", "label!=tengiz-app"]`
  - `imagePruneArgs(all bool) []string` → `["image", "prune", "-f"]` or `["image", "prune", "-f", "-a"]`
  - `networkPruneArgs() []string` → `["network", "prune", "-f"]`
  - `volumePruneArgs() []string` → `["volume", "prune", "-f"]`
  - `countDeletedLines(output string) int` — counts lines starting with `deleted:` (image prune output)
  - `countPrunedItems(output, header string) int` — counts non-empty lines between `header` and `Total reclaimed space:` (container/network/volume prune output)
  - `parseReclaimedBytes(output string) int64` — parses the `Total reclaimed space: 2.1MB` line into bytes

- [ ] **Step 1: Write the failing tests** (append to `internal/runtime/cleanup_test.go`)

```go
func TestContainerPruneArgs(t *testing.T) {
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	got := containerPruneArgs()
	if len(got) != len(want) {
		t.Fatalf("containerPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("containerPruneArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestImagePruneArgs(t *testing.T) {
	dangling := imagePruneArgs(false)
	wantDangling := []string{"image", "prune", "-f"}
	if len(dangling) != len(wantDangling) {
		t.Fatalf("imagePruneArgs(false) = %v, want %v", dangling, wantDangling)
	}
	for i := range wantDangling {
		if dangling[i] != wantDangling[i] {
			t.Fatalf("imagePruneArgs(false)[%d] = %q, want %q", i, dangling[i], wantDangling[i])
		}
	}

	all := imagePruneArgs(true)
	wantAll := []string{"image", "prune", "-f", "-a"}
	if len(all) != len(wantAll) {
		t.Fatalf("imagePruneArgs(true) = %v, want %v", all, wantAll)
	}
	for i := range wantAll {
		if all[i] != wantAll[i] {
			t.Fatalf("imagePruneArgs(true)[%d] = %q, want %q", i, all[i], wantAll[i])
		}
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	got := networkPruneArgs()
	want := []string{"network", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("networkPruneArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("networkPruneArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestVolumePruneArgs(t *testing.T) {
	got := volumePruneArgs()
	want := []string{"volume", "prune", "-f"}
	if len(got) != len(want) {
		t.Fatalf("volumePruneArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("volumePruneArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCountPrunedItems(t *testing.T) {
	out := `Deleted Containers:
3f2a1c9b0e1a4d2a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f
9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c

Total reclaimed space: 2.1MB`
	if got := countPrunedItems(out, "Deleted Containers:"); got != 2 {
		t.Errorf("countPrunedItems() = %d, want 2", got)
	}
}

func TestCountPrunedItemsNothingDeleted(t *testing.T) {
	out := `Total reclaimed space: 0B`
	if got := countPrunedItems(out, "Deleted Containers:"); got != 0 {
		t.Errorf("countPrunedItems() = %d, want 0", got)
	}
}

func TestCountDeletedLines(t *testing.T) {
	out := `Deleted Images:
untagged: tengiz-apps/myapp:old
untagged: tengiz-apps/other:latest
deleted: sha256:aaaabbbbccccdddd
deleted: sha256:1111222233334444

Total reclaimed space: 45.6MB`
	if got := countDeletedLines(out); got != 2 {
		t.Errorf("countDeletedLines() = %d, want 2", got)
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{`Total reclaimed space: 0B`, 0},
		{`Total reclaimed space: 512B`, 512},
		{`Total reclaimed space: 2.5MB`, int64(2.5 * 1024 * 1024)},
		{`Total reclaimed space: 1.2GB`, int64(1.2 * 1024 * 1024 * 1024)},
		{`Total reclaimed space: 36.38kB`, int64(36.38 * 1024)},
		{`no reclaimed info here`, 0},
	}
	for _, tt := range tests {
		if got := parseReclaimedBytes(tt.in); got != tt.want {
			t.Errorf("parseReclaimedBytes(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestContainerPruneArgs|TestImagePruneArgs|TestNetworkPruneArgs|TestVolumePruneArgs|TestCountPrunedItems|TestCountDeletedLines|TestParseReclaimedBytes" -v -count=1`

Expected: FAIL — compile error `undefined: containerPruneArgs` (and the other helpers)

- [ ] **Step 3: Write minimal implementation in `internal/runtime/cleanup.go`**

Add `regexp` and `strconv` to the imports (current imports: `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`):

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

Append the following helper functions at the end of the file:

```go
func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func imagePruneArgs(all bool) []string {
	args := []string{"image", "prune", "-f"}
	if all {
		args = append(args, "-a")
	}
	return args
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func countPrunedItems(output, header string) int {
	count := 0
	in := false
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, header) {
			in = true
			continue
		}
		if in {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "Total reclaimed space:") {
				break
			}
			if trimmed != "" {
				count++
			}
		}
	}
	return count
}

func countDeletedLines(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "deleted:") {
			count++
		}
	}
	return count
}

var reclaimedSpaceRe = regexp.MustCompile(`(?i)total reclaimed space:\s*([0-9.]+)\s*([a-z]+)`)

func parseReclaimedBytes(output string) int64 {
	m := reclaimedSpaceRe.FindStringSubmatch(output)
	if m == nil {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(m[2]) {
	case "b":
		return int64(val)
	case "kb":
		return int64(val * 1024)
	case "mb":
		return int64(val * 1024 * 1024)
	case "gb":
		return int64(val * 1024 * 1024 * 1024)
	case "tb":
		return int64(val * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(val)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestContainerPruneArgs|TestImagePruneArgs|TestNetworkPruneArgs|TestVolumePruneArgs|TestCountPrunedItems|TestCountDeletedLines|TestParseReclaimedBytes" -v -count=1`

Expected: PASS for all 7 test functions

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add docker prune arg builders and output parsers"
```

---

### Task 2: Add `Cleanup` to Manager interface + implement `dockerRuntime.Cleanup()`

**Files:**
- Modify: `internal/runtime/runtime.go` (interface + stub)
- Modify: `internal/runtime/cleanup.go` (types + implementation)
- Test: `internal/runtime/cleanup_test.go`
- Modify: `internal/proxy/proxy_test.go` (mock)
- Modify: `internal/idle/idle_test.go` (mock)
- Modify: `internal/cli/root_test.go` (mock)

**Interfaces:**
- Consumes: Task 1 helpers (`containerPruneArgs`, `imagePruneArgs`, `networkPruneArgs`, `volumePruneArgs`, `countPrunedItems`, `countDeletedLines`, `parseReclaimedBytes`)
- Produces:
  - `type CleanupOptions struct { Containers, Images, AllImages, Networks, Volumes bool }`
  - `type CleanupResult struct { ContainersDeleted, ImagesDeleted, NetworksDeleted, VolumesDeleted int; TotalReclaimedBytes int64; RawOutput []string }`
  - `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` on the interface, `stubManager`, `dockerRuntime`, and the three test mocks

- [ ] **Step 1: Write the failing test** (append to `internal/runtime/cleanup_test.go`)

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersDeleted != 0 {
		t.Errorf("ContainersDeleted = %d, want 0", res.ContainersDeleted)
	}
	if res.ImagesDeleted != 0 {
		t.Errorf("ImagesDeleted = %d, want 0", res.ImagesDeleted)
	}
}
```

- [ ] **Step 2: Add `Cleanup` to the `Manager` interface in `internal/runtime/runtime.go`**

In the `Manager` interface, after the `KeepLastNImages` line (line 36), add:

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`

Expected: FAIL — compile error `stubManager does not implement Manager` (missing `Cleanup`)

- [ ] **Step 4: Add types + `stubManager.Cleanup` + `dockerRuntime.Cleanup`**

In `internal/runtime/cleanup.go`, add the types at the top of the file (after imports) and the implementation at the bottom:

```go
type CleanupOptions struct {
	Containers bool // remove stopped containers not managed by Tengiz
	Images     bool // remove dangling (unreferenced) images
	AllImages  bool // remove all unused images (implies Images)
	Networks   bool // remove unused networks
	Volumes    bool // remove unused volumes (data loss risk)
}

type CleanupResult struct {
	ContainersDeleted   int
	ImagesDeleted       int
	NetworksDeleted     int
	VolumesDeleted      int
	TotalReclaimedBytes int64
	RawOutput           []string
}
```

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	run := func(args []string) (string, error) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		output := string(out)
		if err != nil {
			return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, output)
		}
		return output, nil
	}

	if opts.Containers {
		out, err := run(containerPruneArgs())
		if err != nil {
			return res, err
		}
		res.RawOutput = append(res.RawOutput, out)
		res.ContainersDeleted = countPrunedItems(out, "Deleted Containers:")
		res.TotalReclaimedBytes += parseReclaimedBytes(out)
	}
	if opts.Images || opts.AllImages {
		out, err := run(imagePruneArgs(opts.AllImages))
		if err != nil {
			return res, err
		}
		res.RawOutput = append(res.RawOutput, out)
		res.ImagesDeleted = countDeletedLines(out)
		res.TotalReclaimedBytes += parseReclaimedBytes(out)
	}
	if opts.Networks {
		out, err := run(networkPruneArgs())
		if err != nil {
			return res, err
		}
		res.RawOutput = append(res.RawOutput, out)
		res.NetworksDeleted = countPrunedItems(out, "Deleted Networks:")
		res.TotalReclaimedBytes += parseReclaimedBytes(out)
	}
	if opts.Volumes {
		out, err := run(volumePruneArgs())
		if err != nil {
			return res, err
		}
		res.RawOutput = append(res.RawOutput, out)
		res.VolumesDeleted = countPrunedItems(out, "Deleted Volumes:")
		res.TotalReclaimedBytes += parseReclaimedBytes(out)
	}
	return res, nil
}
```

In `internal/runtime/runtime.go`, add the stub method after the `KeepLastNImages` stub method (after line 119):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 5: Update the three test mocks so the packages compile**

In `internal/proxy/proxy_test.go`, after the `KeepLastNImages` mock method (line 34), add:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

In `internal/idle/idle_test.go`, after the `KeepLastNImages` mock method (line 33), add:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

In `internal/cli/root_test.go`, after the `KeepLastNImages` mock method (line 99), add:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 6: Run build + tests to verify everything passes**

Run: `go build ./... && go test ./internal/runtime/ ./internal/proxy/ ./internal/idle/ ./internal/cli/ -count=1`

Expected: PASS — build succeeds, all four packages pass (proxy tests are slow, ~2s each)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface with docker prune implementation"
```

---

### Task 3: CLI `tengiz cleanup` command

**Files:**
- Modify: `internal/cli/root.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `Manager.Cleanup` (from Task 2)
- Produces:
  - `cleanupCmd *cobra.Command` (`Use: "cleanup"`)
  - `cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error)`
  - `humanizeBytes(b int64) string`
  - Flags registered on `cleanupCmd` in `init()`: `--containers`, `--images`, `--all-images`, `--networks`, `--volumes`, `--all`

- [ ] **Step 1: Write the failing tests** (new file `internal/cli/cleanup_test.go`)

```go
package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupOptionsFromFlagsDefaults(t *testing.T) {
	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	want := runtime.CleanupOptions{Containers: true, Images: true, Networks: true}
	if opts != want {
		t.Errorf("defaults = %+v, want %+v", opts, want)
	}
}

func TestCleanupOptionsFromFlagsAll(t *testing.T) {
	cleanupCmd.Flags().Set("all", "true")
	t.Cleanup(func() { cleanupCmd.Flags().Set("all", "false") })

	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	want := runtime.CleanupOptions{Containers: true, Images: true, AllImages: true, Networks: true, Volumes: true}
	if opts != want {
		t.Errorf("--all = %+v, want %+v", opts, want)
	}
}

func TestCleanupOptionsFromFlagsVolumesOnly(t *testing.T) {
	cleanupCmd.Flags().Set("volumes", "true")
	t.Cleanup(func() { cleanupCmd.Flags().Set("volumes", "false") })

	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	want := runtime.CleanupOptions{Volumes: true}
	if opts != want {
		t.Errorf("--volumes = %+v, want %+v", opts, want)
	}
}

func TestCleanupOptionsFromFlagsAllImages(t *testing.T) {
	cleanupCmd.Flags().Set("all-images", "true")
	t.Cleanup(func() { cleanupCmd.Flags().Set("all-images", "false") })

	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	want := runtime.CleanupOptions{Images: true, AllImages: true}
	if opts != want {
		t.Errorf("--all-images = %+v, want %+v", opts, want)
	}
}

func TestHumanizeBytes(t *testing.T) {
	tests := []struct {
		b    int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{5 * 1024 * 1024, "5.0MB"},
		{2 * 1024 * 1024 * 1024, "2.0GB"},
	}
	for _, tt := range tests {
		if got := humanizeBytes(tt.b); got != tt.want {
			t.Errorf("humanizeBytes(%d) = %q, want %q", tt.b, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanup|TestHumanizeBytes" -v -count=1`

Expected: FAIL — compile error `undefined: cleanupCmd`, `undefined: cleanupOptionsFromFlags`, `undefined: humanizeBytes`

- [ ] **Step 3: Register the command + flags in `internal/cli/root.go`**

In `init()` (inside the `rootCmd.AddCommand(...)` block), after the `runCmd` registration (line 67), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling (unreferenced) images")
	cleanupCmd.Flags().Bool("all-images", false, "prune all unused images (not just dangling)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (data loss risk)")
	cleanupCmd.Flags().Bool("all", false, "clean everything, including unused volumes")
```

- [ ] **Step 4: Implement `cleanupCmd` + helpers**

Add the command and helper functions after the `runCmd` definition (before `var gitCmd`, ~line 1164):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to free disk space",
	Long: `Remove unused Docker resources (containers, images, networks, volumes).

Tengiz-managed containers are always protected: pruning uses label-based
filtering (label!=tengiz-app) so running and stopped app containers — including
scale-to-zero cold-start state — are never removed.

By default (no flags) removes:
  - stopped containers not managed by Tengiz
  - dangling (unreferenced) images
  - unused networks

If any category flag is set, only those categories are cleaned.
Use --all to clean everything, including unused volumes (data loss risk).

Examples:
  tengiz cleanup
  tengiz cleanup --all-images
  tengiz cleanup --volumes
  tengiz cleanup --all`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		fmt.Println("[tengiz] cleaning up unused Docker resources...")
		res, err := rt.Cleanup(context.Background(), opts)
		if err != nil {
			return err
		}

		fmt.Printf("[tengiz] cleanup complete:\n")
		fmt.Printf("  containers removed: %d\n", res.ContainersDeleted)
		fmt.Printf("  images removed: %d\n", res.ImagesDeleted)
		fmt.Printf("  networks removed: %d\n", res.NetworksDeleted)
		fmt.Printf("  volumes removed: %d\n", res.VolumesDeleted)
		fmt.Printf("  space reclaimed: %s\n", humanizeBytes(res.TotalReclaimedBytes))
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	allImages, _ := cmd.Flags().GetBool("all-images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	all, _ := cmd.Flags().GetBool("all")

	if all {
		return runtime.CleanupOptions{
			Containers: true,
			Images:     true,
			AllImages:  true,
			Networks:   true,
			Volumes:    true,
		}, nil
	}
	if !containers && !images && !allImages && !networks && !volumes {
		return runtime.CleanupOptions{
			Containers: true,
			Images:     true,
			Networks:   true,
		}, nil
	}
	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images || allImages,
		AllImages:  allImages,
		Networks:   networks,
		Volumes:    volumes,
	}, nil
}

func humanizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanup|TestHumanizeBytes" -v -count=1`

Expected: PASS for all 6 test functions

- [ ] **Step 6: Build + vet + full CLI tests**

Run: `go build ./... && go vet ./... && go test ./internal/cli/ -count=1`

Expected: PASS — build succeeds, vet is clean, all CLI tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 4: Documentation + full verification

**Files:**
- Modify: `README.md` (add `tengiz cleanup` section after `tengiz rm`, before `tengiz rollback`)
- Modify: `AGENTS.md` (add command to Commands section after the `tengiz rollback <app>` line)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 implemented)

**Interfaces:**
- Consumes: nothing new — documents the CLI surface from Task 3

- [ ] **Step 1: Add `tengiz cleanup` section to `README.md`**

Insert after the `### tengiz rm <app>` section (after line 228) and before `### tengiz rollback <app>`:

```markdown
### `tengiz cleanup`

Remove unused Docker resources (containers, images, networks, volumes) to free disk space on the host.

Tengiz-managed containers are always protected — pruning uses label-based filtering (`label!=tengiz-app`), so running and stopped app containers (including scale-to-zero cold-start state) are never removed.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling (unreferenced) images |
| `--all-images` | Prune all unused images (not just dangling) |
| `--networks` | Prune unused networks |
| `--volumes` | Prune unused volumes (data loss risk — opt-in) |
| `--all` | Clean everything, including unused volumes |

With no flags, the safe default set runs: containers, dangling images, and networks. If any category flag is provided, only those categories are cleaned.

Examples:
```
tengiz cleanup
tengiz cleanup --all-images
tengiz cleanup --volumes
tengiz cleanup --all
```
```

- [ ] **Step 2: Add the command to `AGENTS.md`**

After the `tengiz rollback <app>` line in the Commands section, add:

```markdown
tengiz cleanup [--all-images] [--volumes] [--all] → prune unused Docker resources (label-protected)
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

Change the P0 priority ranking row for feature #6 (line 19) from `⬜` to `✅`:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "✅ Implemented Features (Not Pending)" table, after the `Webhook ile Otomatik Deploy` row (line 253):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-17) |
```

In the detailed "## Docker Housekeeping (Otomatik Temizlik)" section (lines 377-381), add a Status line after the "Why add to Tengiz" line:

```markdown
- **Status:** ✅ Implemented (2026-08-17)
```

- [ ] **Step 4: Full verification**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: PASS — all packages build, vet is clean, all tests pass (note: proxy tests take ~2s each)

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage:**
- `tengiz cleanup` command → Task 3
- Label-based protection of Tengiz containers (`label!=tengiz-app`) → Task 1 (`containerPruneArgs`), Task 2 (`Cleanup`)
- Granular per-category cleaning (containers/images/networks/volumes) → Task 2 flags, Task 3 CLI flags
- Volumes opt-in (data loss risk) → Task 3 defaults logic (`--volumes`/`--all` only)
- Result reporting (counts + reclaimed space) → Task 2 `CleanupResult`, Task 3 output printing
- Periodic scheduling (Coolify `DockerCleanupJob`) intentionally NOT included — that is a separate future feature (#57 Background Monitoring Scheduler / #103 Build Cache Management). YAGNI.

**2. Placeholder scan:** No TBD/TODO, no "add error handling", every code step contains complete code, exact file paths and commands throughout.

**3. Type consistency:**
- `CleanupOptions{Containers, Images, AllImages, Networks, Volumes}` — defined in Task 2, consumed identically in Task 3.
- `CleanupResult{ContainersDeleted, ImagesDeleted, NetworksDeleted, VolumesDeleted, TotalReclaimedBytes, RawOutput}` — defined in Task 2, used in Task 3 output.
- Helper names are consistent: `countPrunedItems` for container/network/volume sections, `countDeletedLines` for image `deleted:` lines, `parseReclaimedBytes` for the total line.
- All four `Manager` implementers updated in Task 2 (verified via `go build` + full test run).
- `humanizeBytes` produces `0B/512B/1.0KB/1.5KB/5.0MB/2.0GB` matching the test expectations.