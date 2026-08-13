# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped non-Tengiz containers, dangling images, old deployment images, unused volumes/networks, build cache) using label-based filters so Tengiz-managed containers are never removed.

**Architecture:** The `runtime` package (exec-based `dockerRuntime` already owns all `os/exec` docker calls — see `internal/runtime/cleanup.go`) gains one new `Manager` method: `Cleanup(ctx, opts) (CleanupReport, error)`. It runs the safe subset of `docker <object> prune` commands with `--filter label!=tengiz-app` so containers/networks labeled `tengiz-app=<name>` (running, stopped scale-to-zero cold-start candidates, and blue/green versioned containers) are preserved. Parsers for docker's prune output (`Deleted ...:` sections and `Total reclaimed space`) live in the same file as pure, unit-testable functions. The CLI adds a `cleanup` cobra command that builds `CleanupOptions` from flags, supports `--dry-run` and a confirmation prompt, and additionally calls the existing `KeepLastNImages` per app to retire old deployment images.

**Tech Stack:** Go 1.26, `os/exec`, Cobra (existing), existing `runtime.Manager`, `config.Store`. No new external dependencies.

## Global Constraints

- Container label key is `tengiz-app`, env label key is `tengiz-env` (both defined in `internal/runtime/docker.go:76-77`)
- Tengiz-managed containers must never be removed: every prune that touches containers/networks uses `--filter label!=tengiz-app`
- Image pruning uses `--filter dangling=true` only (untagged build leftovers); tagged `tengiz-apps/<app>:*` images are handled by the existing `KeepLastNImages` (default keep 5, matching `internal/cli/root.go:346`)
- `tengiz cleanup` with no category flags runs all categories (same as `--all`)
- `--dry-run` and `--force` must not require a working Docker daemon (pure CLI paths, testable in CI)
- Stale versioned containers (`tengiz-deployment` label) and scale-to-zero stopped containers are intentionally **kept** — removing them is the separate "Stale Container Detection" feature (#47), out of scope here
- `--env` scoping: image retention runs against the env-scoped store via `config.NewStoreWithEnv(dataDir, env)`
- No new external Go dependencies
- All existing tests must continue to pass unmodified

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Prune arg builders, output parsers, `CleanupOptions`/`CleanupReport`, `Cleanup()` on `dockerRuntime` |
| `internal/runtime/runtime.go` | Add `Cleanup` to the `Manager` interface + stub no-op |
| `internal/runtime/cleanup_test.go` | Unit tests for arg builders, parsers, count/reclaim helpers, stub `Cleanup` |
| `internal/cli/root.go` | Register `cleanupCmd` + flags; `runCleanup`, `printCleanupCategories`, `humanBytes` helpers; import `bufio` |
| `internal/cli/root_test.go` | Tests: command registered, flags exist, dry-run output, `runCleanup` image-retention loop |
| `README.md` | New `### tengiz cleanup` section in CLI Reference (after `tengiz ps`, ~line 150) |
| `AGENTS.md` | Add `tengiz cleanup` line to the CLI command list |

---

### Task 1: Runtime prune arg builders + output parsers

**Files:**
- Modify: `internal/runtime/cleanup.go` (append pure helper functions; keep existing `RemoveImage`/`KeepLastNImages`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces (pure functions used by Task 2):
  - `containerPruneArgs() []string` → `["container", "prune", "-f", "--filter", "label!=tengiz-app"]`
  - `imagePruneArgs() []string` → `["image", "prune", "-f", "--filter", "dangling=true"]`
  - `volumePruneArgs() []string` → `["volume", "prune", "-f"]`
  - `networkPruneArgs() []string` → `["network", "prune", "-f", "--filter", "label!=tengiz-app"]`
  - `builderPruneArgs() []string` → `["builder", "prune", "-f"]`
  - `parsePruneOutput(output string) []string` → section lines between `Deleted <X>:` and the `Total reclaimed space`/blank line
  - `countPruned(lines []string, skipPrefix string) int` → count of lines, skipping any with the given prefix (used to ignore `untagged:` lines in image output)
  - `parseReclaimedBytes(output string) int64` → bytes from `Total reclaimed space: <N><unit>` line

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go (append to existing file)
package runtime

import (
	"reflect"
	"testing"
)

func TestContainerPruneArgs(t *testing.T) {
	args := containerPruneArgs()
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("containerPruneArgs() = %v, want %v", args, want)
	}
}

func TestImagePruneArgs(t *testing.T) {
	args := imagePruneArgs()
	want := []string{"image", "prune", "-f", "--filter", "dangling=true"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("imagePruneArgs() = %v, want %v", args, want)
	}
}

func TestVolumePruneArgs(t *testing.T) {
	args := volumePruneArgs()
	want := []string{"volume", "prune", "-f"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("volumePruneArgs() = %v, want %v", args, want)
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	args := networkPruneArgs()
	want := []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("networkPruneArgs() = %v, want %v", args, want)
	}
}

func TestBuilderPruneArgs(t *testing.T) {
	args := builderPruneArgs()
	want := []string{"builder", "prune", "-f"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("builderPruneArgs() = %v, want %v", args, want)
	}
}

func TestParsePruneOutputContainers(t *testing.T) {
	out := "Deleted Containers:\n6f4a9c2b7e31\n8b1d0f3a9c2e\n\nTotal reclaimed space: 1.2kB\n"
	got := parsePruneOutput(out)
	want := []string{"6f4a9c2b7e31", "8b1d0f3a9c2e"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePruneOutput() = %v, want %v", got, want)
	}
}

func TestParsePruneOutputImages(t *testing.T) {
	out := "Deleted Images:\nuntagged: myapp:latest\ndeleted: sha256:abc\ndeleted: sha256:def\n\nTotal reclaimed space: 50.2MB\n"
	got := parsePruneOutput(out)
	want := []string{"untagged: myapp:latest", "deleted: sha256:abc", "deleted: sha256:def"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePruneOutput() = %v, want %v", got, want)
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	if got := parsePruneOutput(""); len(got) != 0 {
		t.Errorf("parsePruneOutput(\"\") = %v, want empty", got)
	}
	if got := parsePruneOutput("WARNING! No containers were found.\n"); len(got) != 0 {
		t.Errorf("parsePruneOutput(no-match) = %v, want empty", got)
	}
}

func TestCountPruned(t *testing.T) {
	lines := []string{"untagged: myapp:latest", "deleted: sha256:abc", "deleted: sha256:def"}
	if got := countPruned(lines, "untagged"); got != 2 {
		t.Errorf("countPruned(lines, \"untagged\") = %d, want 2", got)
	}
	if got := countPruned(lines, ""); got != 3 {
		t.Errorf("countPruned(lines, \"\") = %d, want 3", got)
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	tests := []struct {
		out  string
		want int64
	}{
		{"Total reclaimed space: 512B", 512},
		{"Total reclaimed space: 1.5kB", 1500},
		{"Total reclaimed space: 2MB", 2000000},
		{"Total reclaimed space: 3GiB", 3 * 1024 * 1024 * 1024},
		{"no reclaimed line here", 0},
	}
	for _, tc := range tests {
		if got := parseReclaimedBytes(tc.out); got != tc.want {
			t.Errorf("parseReclaimedBytes(%q) = %d, want %d", tc.out, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestContainerPruneArgs|TestImagePruneArgs|TestVolumePruneArgs|TestNetworkPruneArgs|TestBuilderPruneArgs|TestParsePruneOutput|TestCountPruned|TestParseReclaimedBytes" -v -count=1`

Expected: FAIL with `undefined: containerPruneArgs`, `undefined: imagePruneArgs`, etc.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func imagePruneArgs() []string {
	return []string{"image", "prune", "-f", "--filter", "dangling=true"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func builderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

// parsePruneOutput extracts the item lines between a docker prune's
// "Deleted <X>:" header and the trailing "Total reclaimed space" line.
func parsePruneOutput(output string) []string {
	var ids []string
	started := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			if started {
				return ids
			}
		case strings.HasPrefix(line, "Deleted "):
			started = true
		case strings.HasPrefix(line, "Total reclaimed"):
			return ids
		case started:
			ids = append(ids, line)
		}
	}
	return ids
}

// countPruned counts item lines, skipping any with the given prefix
// (e.g. "untagged" so image output counts only actual deletions).
func countPruned(lines []string, skipPrefix string) int {
	n := 0
	for _, l := range lines {
		if skipPrefix != "" && strings.HasPrefix(l, skipPrefix) {
			continue
		}
		n++
	}
	return n
}

// parseReclaimedBytes parses "Total reclaimed space: <N><unit>" into bytes.
func parseReclaimedBytes(output string) int64 {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Total reclaimed space:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		return parseSize(rest)
	}
	return 0
}

func parseSize(s string) int64 {
	i := 0
	for i < len(s) && (s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	if i == 0 {
		return 0
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	unit := strings.TrimSpace(s[i:])
	switch unit {
	case "", "B", "b":
		return int64(num)
	case "kB", "KB", "K", "k":
		return int64(num * 1000)
	case "KiB", "KIB":
		return int64(num * 1024)
	case "MB", "mB", "M", "m":
		return int64(num * 1000 * 1000)
	case "MiB", "MIB":
		return int64(num * 1024 * 1024)
	case "GB", "G", "g":
		return int64(num * 1000 * 1000 * 1000)
	case "GiB", "GIB":
		return int64(num * 1024 * 1024 * 1024)
	case "TB", "T":
		return int64(num * 1000 * 1000 * 1000 * 1000)
	case "TiB", "TIB":
		return int64(num * 1024 * 1024 * 1024 * 1024)
	default:
		return 0
	}
}
```

Note: keep the file's existing imports (`context`, `fmt`, `log`, `os/exec`, `sort`, `strings`) — they are already present from `RemoveImage`/`KeepLastNImages`; only `strconv` is new.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestContainerPruneArgs|TestImagePruneArgs|TestVolumePruneArgs|TestNetworkPruneArgs|TestBuilderPruneArgs|TestParsePruneOutput|TestCountPruned|TestParseReclaimedBytes" -v -count=1`

Expected: PASS (all new tests)

- [ ] **Step 5: Run full package tests + vet**

Run: `go test ./internal/runtime/... -v -count=1 && go vet ./internal/runtime/...`

Expected: all PASS, vet clean

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add docker prune arg builders and output parsers"
```

---

### Task 2: Runtime `Cleanup` interface method + implementation

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to the `Manager` interface
- Modify: `internal/runtime/runtime.go:113-119` — add stub no-op next to the other stub methods
- Modify: `internal/runtime/cleanup.go` — add `CleanupOptions`, `CleanupReport`, `runDockerPrune`, `pruneAndCount`, `pruneAndReclaimed`, and `Cleanup` on `dockerRuntime`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `containerPruneArgs`, `imagePruneArgs`, `volumePruneArgs`, `networkPruneArgs`, `builderPruneArgs`, `parsePruneOutput`, `countPruned`, `parseReclaimedBytes` (from Task 1)
- Produces (used by Task 3):
  - `type CleanupOptions struct { Containers, Images, Volumes, Networks, BuildCache bool }`
  - `type CleanupReport struct { Containers, Images, Volumes, Networks int; BuildCache int64 }`
  - `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)` on `Manager` and `dockerRuntime`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go (append)
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report != (CleanupReport{}) {
		t.Errorf("Cleanup() report = %+v, want empty CleanupReport", report)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: FAIL with `m.Cleanup undefined (type Manager has no field or method Cleanup)` / `undefined: CleanupOptions`

- [ ] **Step 3: Add interface method + stub**

In `internal/runtime/runtime.go`, inside the `Manager` interface (after `KeepLastNImages`), add:

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error)
```

In `internal/runtime/runtime.go`, add to the stub manager (after the existing `KeepLastNImages` stub):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	return CleanupReport{}, nil
}
```

- [ ] **Step 4: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type CleanupReport struct {
	Containers int
	Images     int
	Volumes    int
	Networks   int
	BuildCache int64
}

func runDockerPrune(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return string(out), nil
}

func pruneAndCount(ctx context.Context, args []string, skipPrefix string, firstErr *error) int {
	out, err := runDockerPrune(ctx, args)
	if err != nil {
		if *firstErr == nil {
			*firstErr = err
		}
		return 0
	}
	return countPruned(parsePruneOutput(out), skipPrefix)
}

func pruneAndReclaimed(ctx context.Context, args []string, firstErr *error) int64 {
	out, err := runDockerPrune(ctx, args)
	if err != nil {
		if *firstErr == nil {
			*firstErr = err
		}
		return 0
	}
	return parseReclaimedBytes(out)
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupReport, error) {
	var report CleanupReport
	var firstErr error

	if opts.Containers {
		report.Containers = pruneAndCount(ctx, containerPruneArgs(), "", &firstErr)
	}
	if opts.Images {
		report.Images = pruneAndCount(ctx, imagePruneArgs(), "untagged", &firstErr)
	}
	if opts.Volumes {
		report.Volumes = pruneAndCount(ctx, volumePruneArgs(), "", &firstErr)
	}
	if opts.Networks {
		report.Networks = pruneAndCount(ctx, networkPruneArgs(), "", &firstErr)
	}
	if opts.BuildCache {
		report.BuildCache = pruneAndReclaimed(ctx, builderPruneArgs(), &firstErr)
	}
	return report, firstErr
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: PASS

- [ ] **Step 6: Run full package tests + vet + build**

Run: `go test ./internal/runtime/... -v -count=1 && go vet ./internal/runtime/... && go build -o /tmp/tengiz .`

Expected: all PASS, vet clean, build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface with docker prune implementation"
```

---

### Task 3: CLI `tengiz cleanup` command

**Files:**
- Modify: `internal/cli/root.go:32-89` — add `bufio` import, register `cleanupCmd`, define flags
- Modify: `internal/cli/root.go` — add `cleanupCmd`, `runCleanup`, `printCleanupCategories`, `humanBytes` (place near `rollbackCmd`, before `buildLogsCmd`)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.Manager.Cleanup`, `runtime.Manager.KeepLastNImages` (Tasks 1-2), `config.NewStoreWithEnv(dataDir, env)`, `config.Store.ListApps`
- Produces (tested in this task):
  - `runCleanup(ctx context.Context, rt runtime.Manager, store *config.Store, opts runtime.CleanupOptions, keep int) (runtime.CleanupReport, error)`
  - `printCleanupCategories(opts runtime.CleanupOptions)`
  - `humanBytes(n int64) string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/root_test.go (append)
import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

type recordingCleanupRT struct {
	runtime.Manager
	keepCalls []string
	report    runtime.CleanupReport
}

func (m *recordingCleanupRT) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupReport, error) {
	return m.report, nil
}

func (m *recordingCleanupRT) KeepLastNImages(ctx context.Context, appName string, n int) error {
	m.keepCalls = append(m.keepCalls, appName)
	return nil
}

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatalf("cleanup command not registered")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run", "force", "keep"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdDryRun(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--force"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Errorf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(output, "would remove") {
		t.Errorf("dry-run output missing 'would remove', got: %s", output)
	}
	for _, cat := range []string{"containers", "images", "volumes", "networks", "build cache"} {
		if !strings.Contains(output, cat) {
			t.Errorf("dry-run output missing category %q, got: %s", cat, output)
		}
	}
	if strings.Contains(output, "containers removed:") {
		t.Errorf("dry-run must not report actual removals, got: %s", output)
	}
}

func TestRunCleanupPrunesOldImagesPerApp(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)
	if err := store.SaveApp(types.AppEntry{Name: "alpha", Config: types.AppConfig{Name: "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveApp(types.AppEntry{Name: "beta", Config: types.AppConfig{Name: "beta"}}); err != nil {
		t.Fatal(err)
	}

	mock := &recordingCleanupRT{Manager: runtime.NewStub()}
	opts := runtime.CleanupOptions{Images: true}
	_, err := runCleanup(context.Background(), mock, store, opts, 5)
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if len(mock.keepCalls) != 2 {
		t.Fatalf("expected 2 KeepLastNImages calls, got %v", mock.keepCalls)
	}
	if mock.keepCalls[0] != "alpha" || mock.keepCalls[1] != "beta" {
		t.Errorf("keepCalls = %v, want [alpha beta]", mock.keepCalls)
	}
}

func TestRunCleanupSkipsImageRetentionWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)
	if err := store.SaveApp(types.AppEntry{Name: "alpha", Config: types.AppConfig{Name: "alpha"}}); err != nil {
		t.Fatal(err)
	}

	mock := &recordingCleanupRT{Manager: runtime.NewStub()}
	opts := runtime.CleanupOptions{Containers: true}
	if _, err := runCleanup(context.Background(), mock, store, opts, 5); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if len(mock.keepCalls) != 0 {
		t.Errorf("expected no KeepLastNImages calls, got %v", mock.keepCalls)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0.00B"},
		{512, "512.00B"},
		{1500, "1.50kB"},
		{2000000, "2.00MB"},
	}
	for _, tc := range tests {
		if got := humanBytes(tc.in); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
```

Note: `captureOutput`, `rootCmd`, and `cleanupCmd` must exist. `captureOutput` already exists in `internal/cli/root_test.go:57`. Add the new imports to the existing import block (the file already imports `context`, `strings`, `testing`, `config`, `runtime`, `types`; add nothing else).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCmd|TestRunCleanup|TestHumanBytes" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: runCleanup`, `undefined: humanBytes`, and compile error `imported and not used: bufio` until Step 3 adds the usage.

- [ ] **Step 3: Add the `bufio` import + register the command + flags**

In `internal/cli/root.go`, add `"bufio"` to the import block (alphabetical, before `"context"`).

In `init()` (after `rootCmd.AddCommand(rollbackCmd)` around line 65), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling build images and old deployment images")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "remove the Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "run all cleanup categories (default)")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("force", false, "skip the confirmation prompt")
	cleanupCmd.Flags().Int("keep", 5, "number of deployment images to keep per app")
```

- [ ] **Step 4: Write minimal implementation**

Add after the `rollbackCmd` declaration in `internal/cli/root.go`:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (housekeeping)",
	Long: `Remove unused Docker resources to reclaim disk space.

Safe by default: containers labeled tengiz-app=<name> (running, stopped
scale-to-zero cold-start candidates, and blue/green versioned containers) are
never removed. Only stopped containers without the tengiz-app label, dangling
build images, unused volumes and networks, and the Docker build cache are
pruned. Old deployment images are retained per app (--keep, default 5).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		keep, _ := cmd.Flags().GetInt("keep")

		if all || !(containers || images || volumes || networks || buildCache) {
			containers, images, volumes, networks, buildCache = true, true, true, true, true
		}

		opts := runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
		}

		if dryRun {
			fmt.Println("[tengiz] dry-run: no changes made")
			printCleanupCategories(opts)
			return nil
		}

		if !force {
			fmt.Print("[tengiz] this will remove unused Docker resources. Continue? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			resp, _ := reader.ReadString('\n')
			resp = strings.TrimSpace(strings.ToLower(resp))
			if resp != "y" && resp != "yes" {
				fmt.Println("[tengiz] aborted")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := runCleanup(cmd.Context(), rt, config.NewStoreWithEnv(dataDir, env), opts, keep)
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] cleanup complete")
		fmt.Printf("  containers removed: %d\n", report.Containers)
		fmt.Printf("  images removed:     %d\n", report.Images)
		fmt.Printf("  volumes removed:    %d\n", report.Volumes)
		fmt.Printf("  networks removed:   %d\n", report.Networks)
		fmt.Printf("  build cache freed:  %s\n", humanBytes(report.BuildCache))
		return nil
	},
}

func runCleanup(ctx context.Context, rt runtime.Manager, store *config.Store, opts runtime.CleanupOptions, keep int) (runtime.CleanupReport, error) {
	report, err := rt.Cleanup(ctx, opts)
	if err != nil {
		return report, err
	}
	if opts.Images {
		apps, err := store.ListApps()
		if err != nil {
			return report, err
		}
		for _, app := range apps {
			if err := rt.KeepLastNImages(ctx, app.Name, keep); err != nil {
				log.Printf("[tengiz] warning: image retention for %s: %v", app.Name, err)
			}
		}
	}
	return report, nil
}

func printCleanupCategories(opts runtime.CleanupOptions) {
	fmt.Println("[tengiz] would remove:")
	if opts.Containers {
		fmt.Println("  - stopped containers not managed by Tengiz")
	}
	if opts.Images {
		fmt.Println("  - dangling build images and old deployment images")
	}
	if opts.Volumes {
		fmt.Println("  - unused volumes")
	}
	if opts.Networks {
		fmt.Println("  - unused networks")
	}
	if opts.BuildCache {
		fmt.Println("  - Docker build cache")
	}
}

func humanBytes(n int64) string {
	if n < 0 {
		return "0.00B"
	}
	units := []string{"B", "kB", "MB", "GB", "TB"}
	val := float64(n)
	i := 0
	for val >= 1000 && i < len(units)-1 {
		val /= 1000
		i++
	}
	return fmt.Sprintf("%.2f%s", val, units[i])
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanupCmd|TestRunCleanup|TestHumanBytes" -v -count=1`

Expected: PASS (all new tests)

- [ ] **Step 6: Run full test suite + vet + build**

Run: `go test ./... -v -count=1 && go vet ./... && go build -o /tmp/tengiz .`

Expected: all PASS, vet clean, build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Document `tengiz cleanup`

**Files:**
- Modify: `README.md` — new `### tengiz cleanup` section after the `tengiz ps` section (~line 150)
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI command list

**Interfaces:**
- Consumes: the final command surface from Task 3 (flags: `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--all`, `--dry-run`, `--force`, `--keep N`)

- [ ] **Step 1: Write the documentation**

In `README.md`, immediately after the `### tengiz ps` section (which ends with `Output: NAME, STATE (running/stopped), PORT, ENVIRONMENT, HEALTH.`), add:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling build images and old deployment images |
| `--volumes` | Remove unused volumes |
| `--networks` | Remove unused networks |
| `--build-cache` | Remove the Docker build cache |
| `--all` | Run all cleanup categories (default when no category flag is given) |
| `--dry-run` | Show what would be removed without removing anything |
| `--force` | Skip the confirmation prompt |
| `--keep N` | Number of deployment images to keep per app (default: 5) |

Safe by default: containers labeled `tengiz-app=<name>` (running, stopped scale-to-zero cold-start candidates, and blue/green versioned containers) are never removed. Only stopped containers without the `tengiz-app` label, dangling images, unused volumes/networks, and the build cache are pruned. Old deployment images are retained per app via `--keep`. Run `tengiz cleanup --dry-run` first to preview, then `tengiz cleanup --force` to apply without prompting.
```

In `AGENTS.md`, add this line after the `tengiz rollback <app>` line (in the CLI section):

```markdown
tengiz cleanup [--all] [--containers] [--images] [--volumes] [--networks] [--build-cache] [--dry-run] [--force] [--keep N] → docker housekeeping (label-safe prune)
```

- [ ] **Step 2: Verify rendering and full suite**

Run: `go build -o /tmp/tengiz . && go test ./... -v -count=1`

Expected: build succeeds, all tests PASS

- [ ] **Step 3: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage.** The FUTURES_FEATURES.md #6 spec ("Label-based `docker system prune`. `tengiz cleanup`", protecting Tengiz-managed containers from continuous-deploy waste) is covered by:
- Label-safe prune: `--filter label!=tengiz-app` on containers and networks (Task 1, used in Task 2)
- `tengiz cleanup` command with dry-run/force/keep (Task 3)
- Old deployment image retention reuses the existing `KeepLastNImages` so `tengiz-apps/*` images don't grow unbounded (Task 3 `runCleanup`)
- Documentation (Task 4)

**2. Placeholder scan.** No TBDs/TODOs; every step contains concrete code, exact paths, and expected test output.

**3. Type consistency.**
- `CleanupOptions{Containers, Images, Volumes, Networks, BuildCache bool}` — identical in runtime (Task 2) and CLI usage (Task 3).
- `CleanupReport{Containers, Images, Volumes, Networks int; BuildCache int64}` — same in both tasks.
- `runCleanup(ctx, rt runtime.Manager, store *config.Store, opts runtime.CleanupOptions, keep int) (runtime.CleanupReport, error)` — the definition in Task 3 matches the test signature in Task 3 Step 1.
- `KeepLastNImages(ctx, appName string, n int) error` — already exists on `Manager` (`internal/runtime/runtime.go:36`) and is only invoked, not redefined.
- `parseSize` handles docker's SI output (`kB`/`MB`) which `parseReclaimedBytes`/`humanBytes` both assume; `humanBytes` outputs SI units consistent with `parseSize`.
- `countPruned` with `"untagged"` skip matches the image prune output format asserted in `TestParsePruneOutputImages`.

**Out of scope (future plans):** stale versioned-container detection (#47), granular per-category scheduling (#56), automated background cleanup.
