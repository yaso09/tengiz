# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused volumes/networks, build cache) with label-based protection so Tengiz-managed containers are never removed, plus an optional periodic mode.

**Architecture:** A new `Cleanup(ctx, opts) (CleanupResult, error)` method on the existing `runtime.Manager` interface, implemented by the exec-based `dockerRuntime` using `os/exec` (no Docker SDK). All Docker commands are constructed by pure, testable helper functions that return `[]string` args; the destructive `container prune` always carries `--filter label!=tengiz-app` so every container labeled by Tengiz (apps, env-qualified, versioned, previews — all carry the `tengiz-app` label) is protected. `CleanupOptions` selects categories (`Containers/Images/Volumes/Networks/Cache`) plus a `DryRun` mode that runs only non-destructive listing commands. The CLI command `tengiz cleanup` defaults to all categories, supports `--dry-run`, per-category flags, and `--interval <duration>` for a foreground periodic loop (same signal-handling pattern as `proxy`/`webhook` — no daemon).

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` docker CLI (existing pattern), `regexp`/`strconv` (reclaimed-space parsing). No new external dependencies.

## Global Constraints

- New command name is exactly `tengiz cleanup` (spec: "`tengiz cleanup` komutu eklenebilir")
- Container pruning MUST always include `--filter label!=tengiz-app` so Tengiz-managed containers are protected (spec: "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur")
- Cleaned resource categories: unused volumes, networks, containers and images (spec: "kullanılmayan volume, network, container ve image'leri"), plus Docker build cache
- Periodic cleaning supported (spec: "periyodik temizleme"); implemented as a foreground `--interval` loop, NOT a background daemon (Tengiz is CLI-first, "No daemon required")
- `--dry-run` MUST only run non-destructive listing commands (`docker ps -a`, `docker images`, `docker volume ls`, `docker network ls`, `docker system df`) — never a prune
- Default `tengiz cleanup` (no category flags) cleans all five categories; any category flag set restricts to only those set
- All docker invocations go through `os/exec` `docker` CLI — no Docker SDK
- Docker command construction must live in pure functions returning `[]string` so unit tests run without a Docker daemon
- Adding `Cleanup` to the `runtime.Manager` interface requires updating ALL existing implementers: `stubManager` (`internal/runtime/runtime.go`), `mockRTForDeploy` (`internal/cli/root_test.go`), and the two `mockRuntime` types (`internal/proxy/proxy_test.go`, `internal/idle/idle_test.go`) — otherwise the module won't compile
- No new external dependencies
- Existing tests must continue to pass without modification (except the compile-required mock additions above)
- Image prune removes only dangling images (safe default; per-app retention is already handled by `KeepLastNImages` during deploy)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupResult` types, `Cleanup` method to `Manager` interface, stub implementation |
| `internal/runtime/housekeeping.go` | NEW — pure helpers: prune/list Docker arg builders + `parseReclaimedBytes` |
| `internal/runtime/housekeeping_test.go` | NEW — unit tests for arg builders + parser |
| `internal/runtime/cleanup.go` | Add `dockerRuntime.Cleanup` exec-based implementation |
| `internal/runtime/cleanup_test.go` | Add tests: stub `Cleanup`, no-category no-op |
| `internal/cli/cmd_cleanup.go` | NEW — `cleanupCmd` Cobra command, flag→options mapping, `humanBytes`, `getRuntime` injection var |
| `internal/cli/cmd_cleanup_test.go` | NEW — command registration, flag mapping, mock-manager invocation tests |
| `internal/cli/root.go` | Register `cleanupCmd` + its flags in `init()` |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy` (compile fix) |
| `internal/proxy/proxy_test.go` | Add `Cleanup` method to `mockRuntime` (compile fix) |
| `internal/idle/idle_test.go` | Add `Cleanup` method to `mockRuntime` (compile fix) |
| `README.md` | Feature bullet + `### tengiz cleanup` CLI Reference section |
| `AGENTS.md` | Add `tengiz cleanup` line to CLI block + note `Cleanup` on `runtime.Manager` |

---

### Task 1: Add `CleanupOptions`/`CleanupResult` types and `Cleanup` to the `runtime.Manager` interface

**Files:**
- Modify: `internal/runtime/runtime.go` — types + interface method + stub
- Modify: `internal/cli/root_test.go:69-100` — add `Cleanup` to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:15-35` — add `Cleanup` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:14-34` — add `Cleanup` to `mockRuntime`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `runtime.CleanupOptions{ Containers, Images, Volumes, Networks, Cache, DryRun bool }`
  - `runtime.CleanupResult{ ReclaimedBytes uint64; Details string }`
  - `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ReclaimedBytes != 0 || res.Details != "" {
		t.Fatalf("expected empty result, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`

Expected: FAIL — `m.Cleanup undefined (type Manager has no field or method Cleanup)`; `undefined: CleanupOptions`.

- [ ] **Step 3: Add types and interface method in `internal/runtime/runtime.go`**

After the `RunOptions` struct (around line 29), add:

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	Cache      bool
	DryRun     bool
}

type CleanupResult struct {
	ReclaimedBytes uint64
	Details        string
}
```

Add to the `Manager` interface (after the `Run` method, line 48):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add stub implementation at the end of `runtime.go` (after the `Run` stub):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 4: Update the three test mocks so the module compiles**

In `internal/cli/root_test.go`, add this method to `mockRTForDeploy` (after the `Run` method at line 100):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

In `internal/proxy/proxy_test.go`, add this method to `mockRuntime` (after line 35):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

In `internal/idle/idle_test.go`, add this method to `mockRuntime` (after line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all tests to confirm nothing broke**

Run: `go test ./... -count=1`

Expected: All PASS (proxy tests remain slow ~2s each, idle tests time-sensitive — all within existing baselines)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add Cleanup method to runtime.Manager interface"
```

---

### Task 2: Pure Docker arg-builder helpers and reclaimed-space parser

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: nothing from Task 1
- Produces (used by Task 3's `dockerRuntime.Cleanup`):
  - `cleanupContainerArgs() []string` → `{"container", "prune", "-f", "--filter", "label!=tengiz-app"}`
  - `cleanupImageArgs() []string`, `cleanupVolumeArgs() []string`, `cleanupNetworkArgs() []string`, `cleanupCacheArgs() []string`
  - `listContainerArgs() []string`, `listDanglingImageArgs() []string`, `listDanglingVolumeArgs() []string`, `listDanglingNetworkArgs() []string`, `cacheUsageArgs() []string`
  - `parseReclaimedBytes(output string) uint64`

- [ ] **Step 1: Write the failing tests**

Create `internal/runtime/housekeeping_test.go`:

```go
package runtime

import "testing"

func TestCleanupContainerArgsProtectsTengiz(t *testing.T) {
	args := cleanupContainerArgs()
	found := false
	for _, a := range args {
		if a == "label!=tengiz-app" {
			found = true
		}
	}
	if !found {
		t.Fatalf("container prune args must exclude tengiz-app label, got %v", args)
	}
}

func TestCleanupContainerArgsPrefix(t *testing.T) {
	args := cleanupContainerArgs()
	want := []string{"container", "prune", "-f"}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("cleanupContainerArgs()[%d] = %q, want %q (full: %v)", i, args[i], want[i], args)
		}
	}
}

func TestCleanupImageArgs(t *testing.T) {
	args := cleanupImageArgs()
	want := []string{"image", "prune", "-f"}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("cleanupImageArgs()[%d] = %q, want %q (full: %v)", i, args[i], want[i], args)
		}
	}
}

func TestCleanupVolumeArgs(t *testing.T) {
	args := cleanupVolumeArgs()
	if len(args) != 3 || args[0] != "volume" || args[1] != "prune" || args[2] != "-f" {
		t.Fatalf("cleanupVolumeArgs() = %v", args)
	}
}

func TestCleanupNetworkArgs(t *testing.T) {
	args := cleanupNetworkArgs()
	if len(args) != 3 || args[0] != "network" || args[1] != "prune" || args[2] != "-f" {
		t.Fatalf("cleanupNetworkArgs() = %v", args)
	}
}

func TestCleanupCacheArgs(t *testing.T) {
	args := cleanupCacheArgs()
	if len(args) != 3 || args[0] != "builder" || args[1] != "prune" || args[2] != "-f" {
		t.Fatalf("cleanupCacheArgs() = %v", args)
	}
}

func TestListContainerArgsProtectsTengiz(t *testing.T) {
	args := listContainerArgs()
	if args[0] != "ps" || args[1] != "-a" {
		t.Fatalf("listContainerArgs() prefix = %v", args)
	}
	for _, a := range args {
		if a == "label!=tengiz-app" {
			return
		}
	}
	t.Fatalf("listContainerArgs() must exclude tengiz-app label, got %v", args)
}

func TestListDanglingImageArgs(t *testing.T) {
	args := listDanglingImageArgs()
	if args[0] != "images" {
		t.Fatalf("listDanglingImageArgs()[0] = %q, want %q", args[0], "images")
	}
}

func TestListDanglingVolumeArgs(t *testing.T) {
	args := listDanglingVolumeArgs()
	if args[0] != "volume" || args[1] != "ls" {
		t.Fatalf("listDanglingVolumeArgs() prefix = %v", args)
	}
}

func TestListDanglingNetworkArgs(t *testing.T) {
	args := listDanglingNetworkArgs()
	if args[0] != "network" || args[1] != "ls" {
		t.Fatalf("listDanglingNetworkArgs() prefix = %v", args)
	}
}

func TestCacheUsageArgs(t *testing.T) {
	args := cacheUsageArgs()
	if len(args) != 2 || args[0] != "system" || args[1] != "df" {
		t.Fatalf("cacheUsageArgs() = %v", args)
	}
}

func TestParseReclaimedBytes(t *testing.T) {
	tests := []struct {
		output string
		want   uint64
	}{
		{"Total reclaimed space: 0B", 0},
		{"Total reclaimed space: 1.5KB", 1500},
		{"Total reclaimed space: 12.34MB", 12340000},
		{"Total reclaimed space: 2GB", 2000000000},
		{"Deleted Containers:\nabc\n\nTotal reclaimed space: 5.321MB\n", 5321000},
		{"no match here", 0},
		{"Total reclaimed space: ", 0},
	}
	for _, tt := range tests {
		if got := parseReclaimedBytes(tt.output); got != tt.want {
			t.Errorf("parseReclaimedBytes(%q) = %d, want %d", tt.output, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestCleanup|TestList|TestCacheUsage|TestParseReclaimedBytes" -v -count=1`

Expected: FAIL — `undefined: cleanupContainerArgs`, etc.

- [ ] **Step 3: Implement the helpers**

Create `internal/runtime/housekeeping.go`:

```go
package runtime

import (
	"regexp"
	"strconv"
	"strings"
)

func cleanupContainerArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func cleanupImageArgs() []string {
	return []string{"image", "prune", "-f"}
}

func cleanupVolumeArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func cleanupNetworkArgs() []string {
	return []string{"network", "prune", "-f"}
}

func cleanupCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func listContainerArgs() []string {
	return []string{"ps", "-a", "--filter", "label!=tengiz-app", "--filter", "status=exited", "--format", "{{.ID}}\t{{.Names}}\t{{.Status}}"}
}

func listDanglingImageArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}\t{{.Repository}}:{{.Tag}}\t{{.Size}}"}
}

func listDanglingVolumeArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func listDanglingNetworkArgs() []string {
	return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.ID}}\t{{.Name}}"}
}

func cacheUsageArgs() []string {
	return []string{"system", "df"}
}

var reclaimedSpaceRe = regexp.MustCompile(`(?i)Total reclaimed space:\s*([0-9.]+)\s*([a-z]+)`)

func parseReclaimedBytes(output string) uint64 {
	m := reclaimedSpaceRe.FindStringSubmatch(output)
	if m == nil {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	var mult uint64 = 1
	switch strings.ToLower(m[2]) {
	case "kb":
		mult = 1000
	case "mb":
		mult = 1000 * 1000
	case "gb":
		mult = 1000 * 1000 * 1000
	case "tb":
		mult = 1000 * 1000 * 1000 * 1000
	case "pb":
		mult = 1000 * 1000 * 1000 * 1000 * 1000
	}
	return uint64(val * float64(mult))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestCleanup|TestList|TestCacheUsage|TestParseReclaimedBytes" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add docker cleanup arg builders and reclaimed-space parser"
```

---

### Task 3: Implement `dockerRuntime.Cleanup`

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `Cleanup` method
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: all arg builders + `parseReclaimedBytes` from Task 2; `CleanupOptions`/`CleanupResult` from Task 1
- Produces: `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — used by Task 4's CLI

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestDockerCleanupNoCategoriesDoesNothing(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ReclaimedBytes != 0 {
		t.Errorf("ReclaimedBytes = %d, want 0", res.ReclaimedBytes)
	}
	if res.Details != "" {
		t.Errorf("Details = %q, want empty", res.Details)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestDockerCleanupNoCategoriesDoesNothing -v -count=1`

Expected: FAIL — `r.Cleanup undefined (type *dockerRuntime has no field or method Cleanup)`

- [ ] **Step 3: Implement `dockerRuntime.Cleanup`**

Append to `internal/runtime/cleanup.go` (file already imports `context`, `fmt`, `log`, `os/exec`, `strings`):

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var details strings.Builder
	var total uint64

	execCmd := func(category string, args []string, parseReclaim bool) error {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[runtime] cleanup %s failed: %v\n%s", category, err, string(out))
			return fmt.Errorf("docker %s cleanup: %w", category, err)
		}
		details.WriteString(string(out))
		if parseReclaim {
			total += parseReclaimedBytes(string(out))
		}
		return nil
	}

	if opts.DryRun {
		if opts.Containers {
			if err := execCmd("containers", listContainerArgs(), false); err != nil {
				return CleanupResult{}, err
			}
		}
		if opts.Images {
			if err := execCmd("images", listDanglingImageArgs(), false); err != nil {
				return CleanupResult{}, err
			}
		}
		if opts.Volumes {
			if err := execCmd("volumes", listDanglingVolumeArgs(), false); err != nil {
				return CleanupResult{}, err
			}
		}
		if opts.Networks {
			if err := execCmd("networks", listDanglingNetworkArgs(), false); err != nil {
				return CleanupResult{}, err
			}
		}
		if opts.Cache {
			if err := execCmd("cache", cacheUsageArgs(), false); err != nil {
				return CleanupResult{}, err
			}
		}
		return CleanupResult{Details: details.String()}, nil
	}

	if opts.Containers {
		if err := execCmd("containers", cleanupContainerArgs(), true); err != nil {
			return CleanupResult{}, err
		}
	}
	if opts.Images {
		if err := execCmd("images", cleanupImageArgs(), true); err != nil {
			return CleanupResult{}, err
		}
	}
	if opts.Volumes {
		if err := execCmd("volumes", cleanupVolumeArgs(), true); err != nil {
			return CleanupResult{}, err
		}
	}
	if opts.Networks {
		if err := execCmd("networks", cleanupNetworkArgs(), true); err != nil {
			return CleanupResult{}, err
		}
	}
	if opts.Cache {
		if err := execCmd("cache", cleanupCacheArgs(), true); err != nil {
			return CleanupResult{}, err
		}
	}

	return CleanupResult{ReclaimedBytes: total, Details: details.String()}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestDockerCleanupNoCategoriesDoesNothing -v -count=1`

Expected: PASS (no categories selected → no docker command executes, empty result)

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/ -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement dockerRuntime.Cleanup for housekeeping"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cmd_cleanup.go`
- Create: `internal/cli/cmd_cleanup_test.go`
- Modify: `internal/cli/root.go:38-45` — register command; `root.go:76-88` — add flags in `init()`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.Manager.Cleanup` from Tasks 1-3
- Produces:
  - `cleanupCmd *cobra.Command`
  - `cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error)`
  - `runCleanup(ctx context.Context, rt runtime.Manager, opts runtime.CleanupOptions) error`
  - `humanBytes(b uint64) string`
  - `var getRuntime = runtime.NewDocker` (package-level injection point for tests)

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cmd_cleanup_test.go`:

```go
package cli

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func resetCleanupFlags() {
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache"} {
		f := cleanupCmd.Flags().Lookup(name)
		if f != nil {
			f.Value.Set(f.DefValue)
			f.Changed = false
		}
	}
}

type cleanupRecorder struct {
	runtime.Manager
	opts []runtime.CleanupOptions
	res  runtime.CleanupResult
	err  error
}

func (m *cleanupRecorder) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	m.opts = append(m.opts, opts)
	return m.res, m.err
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil || cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupCommandHasFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "cache", "interval"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupOptionsDefaultAll(t *testing.T) {
	resetCleanupFlags()
	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags: %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.Cache {
		t.Fatalf("expected all categories enabled by default, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("DryRun should be false by default")
	}
}

func TestCleanupOptionsSelective(t *testing.T) {
	resetCleanupFlags()
	cleanupCmd.Flags().Set("containers", "true")
	cleanupCmd.Flags().Set("cache", "true")
	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags: %v", err)
	}
	if !opts.Containers || !opts.Cache {
		t.Fatalf("expected containers+cache enabled, got %+v", opts)
	}
	if opts.Images || opts.Volumes || opts.Networks {
		t.Fatalf("expected images/volumes/networks disabled, got %+v", opts)
	}
	resetCleanupFlags()
}

func TestCleanupOptionsDryRunFlag(t *testing.T) {
	resetCleanupFlags()
	cleanupCmd.Flags().Set("dry-run", "true")
	opts, err := cleanupOptionsFromFlags(cleanupCmd)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags: %v", err)
	}
	if !opts.DryRun {
		t.Error("DryRun should be true when --dry-run set")
	}
	resetCleanupFlags()
}

func TestCleanupRunsOnceThroughManager(t *testing.T) {
	resetCleanupFlags()
	old := getRuntime
	defer func() { getRuntime = old }()

	rec := &cleanupRecorder{Manager: runtime.NewStub(), res: runtime.CleanupResult{ReclaimedBytes: 42}}
	getRuntime = func() (runtime.Manager, error) { return rec, nil }

	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("cleanup execute: %v", err)
	}
	if len(rec.opts) != 1 {
		t.Fatalf("expected exactly 1 Cleanup call, got %d", len(rec.opts))
	}
	if !rec.opts[0].DryRun {
		t.Error("expected DryRun=true (--dry-run passed)")
	}
	if !rec.opts[0].Containers {
		t.Error("expected Containers=true (default all categories)")
	}
	resetCleanupFlags()
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{1500, "1.50KB"},
		{1234567, "1.23MB"},
		{2000000000, "2.00GB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.in); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanup|TestHumanBytes" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd`, `undefined: cleanupOptionsFromFlags`, `undefined: humanBytes`

- [ ] **Step 3: Create `internal/cli/cmd_cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var getRuntime = runtime.NewDocker

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Remove unused Docker resources to reclaim disk space: stopped non-Tengiz containers,
dangling images, unused volumes and networks, and the Docker build cache.

Tengiz-managed containers are always protected via the tengiz-app label filter.

Use --dry-run to preview what would be removed. Use --interval to run cleanup
periodically until interrupted. By default all categories are cleaned; pass any
of --containers/--images/--volumes/--networks/--cache to clean only those.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		interval, _ := cmd.Flags().GetDuration("interval")

		rt, err := getRuntime()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if interval > 0 {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			for {
				if err := runCleanup(ctx, rt, opts); err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					fmt.Println("[tengiz] cleanup stopped")
					return nil
				case <-time.After(interval):
				}
			}
		}
		return runCleanup(cmd.Context(), rt, opts)
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	cache, _ := cmd.Flags().GetBool("cache")

	anySet := cmd.Flags().Changed("containers") || cmd.Flags().Changed("images") ||
		cmd.Flags().Changed("volumes") || cmd.Flags().Changed("networks") || cmd.Flags().Changed("cache")
	if !anySet {
		containers, images, volumes, networks, cache = true, true, true, true, true
	}

	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		Cache:      cache,
		DryRun:     dryRun,
	}, nil
}

func runCleanup(ctx context.Context, rt runtime.Manager, opts runtime.CleanupOptions) error {
	res, err := rt.Cleanup(ctx, opts)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	if res.Details != "" {
		fmt.Print(res.Details)
	}
	if opts.DryRun {
		fmt.Println("[tengiz] cleanup dry-run complete (candidates listed above)")
		return nil
	}
	fmt.Printf("[tengiz] cleanup complete — reclaimed %s\n", humanBytes(res.ReclaimedBytes))
	return nil
}

func humanBytes(b uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	val := float64(b)
	i := 0
	for val >= 1000 && i < len(units)-1 {
		val /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%dB", b)
	}
	return fmt.Sprintf("%.2f%s", val, units[i])
}
```

- [ ] **Step 4: Register the command and flags in `internal/cli/root.go`**

In `init()`, after the `rootCmd.AddCommand(secretCmd)` line (line 69), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

In `init()`, after the webhook flags (line 88), add:

```go
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without deleting anything")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Duration("interval", 0, "run cleanup periodically (e.g. 6h) until interrupted")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanup|TestHumanBytes" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all CLI tests**

Run: `go test ./internal/cli/ -v -count=1`

Expected: All PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cmd_cleanup.go internal/cli/cmd_cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 5: Documentation, full verification, and self-review

**Files:**
- Modify: `README.md` — add a feature bullet and `### tengiz cleanup` CLI Reference section
- Modify: `AGENTS.md` — add `tengiz cleanup` to CLI block and note `Cleanup` on `runtime.Manager`

**Interfaces:**
- Consumes: final `tengiz cleanup` command from Task 4
- Produces: updated user documentation

- [ ] **Step 1: Update `README.md`**

Add a bullet to the **Features** list (after the "Deployment history" bullet, ~line 20):

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes stopped non-Tengiz containers, dangling images, unused volumes/networks, and the build cache. Label-based filtering protects Tengiz apps; `--dry-run` previews, `--interval` schedules.
```

Add a new section in the **CLI Reference** after `### tengiz rm <app>` (line 229):

```markdown
### `tengiz cleanup [--dry-run] [--containers|--images|--volumes|--networks|--cache] [--interval <duration>]`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview what would be removed (lists candidates, deletes nothing) |
| `--containers` | Prune stopped containers NOT managed by Tengiz |
| `--images` | Prune dangling images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--cache` | Prune the Docker build cache |
| `--interval` | Run cleanup periodically (e.g. `6h`) until interrupted |

With no category flags, all categories are cleaned. Tengiz-managed containers are always protected via the `tengiz-app` label filter. Use `--interval 6h` (or with `--dry-run`) for scheduled housekeeping.
```

- [ ] **Step 2: Update `AGENTS.md`**

In the CLI block, after the `tengiz rollback <app>` line (line 60), add:

```markdown
tengiz cleanup [--dry-run] [--containers|--images|--volumes|--networks|--cache] [--interval DURATION] → prune unused Docker resources (Tengiz containers protected by label)
```

In the `runtime.Manager` row of the architecture table (line 15), append `Cleanup` to the method list so it reads:

```markdown
| `runtime.Manager` | Interface for container lifecycle. `NewDocker()` = exec-based impl, `NewStub()` = test mock. Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages`, `Cleanup` for rollback + image/housekeeping cleanup. `ContainerName(name, env)` helper. |
```

- [ ] **Step 3: Full verification**

Run: `go build -o tengiz .`

Expected: Build succeeds

Run: `go vet ./...`

Expected: No issues

Run: `go test ./... -v -count=1`

Expected: All PASS (proxy tests remain slow ~2s each; idle tests time-sensitive — within existing baselines)

- [ ] **Step 4: Self-review against the spec**

Check requirements from `docs/FUTURES_FEATURES.md` #6 Docker Housekeeping:
- `tengiz cleanup` command ✅ (Task 4)
- Label-based filtering protects Tengiz containers ✅ (`label!=tengiz-app` in `cleanupContainerArgs`/`listContainerArgs`, Tasks 2-3)
- Cleans unused volumes, networks, containers, images ✅ (default all categories, Task 4)
- Periodic cleaning ✅ (`--interval` foreground loop, Task 4)
- CLI-first, no daemon ✅ (periodic mode is a signal-handled foreground loop)
- `docker system prune`-style disk reclamation ✅ (per-category prunes + build cache)

- [ ] **Step 5: Placeholder scan**

Search this plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task". None present — every code step contains complete, compilable code.

- [ ] **Step 6: Type consistency check**

- `runtime.CleanupOptions{Containers, Images, Volumes, Networks, Cache, DryRun bool}` — identical struct across Tasks 1, 3, 4
- `runtime.CleanupResult{ReclaimedBytes uint64, Details string}` — identical across Tasks 1, 3, 4
- `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — defined in Task 1, implemented in Tasks 3-4
- `cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error)` — same signature in Task 4 tests and implementation
- `getRuntime` var is `func() (runtime.Manager, error)` — matches `runtime.NewDocker` and the test override
- Arg-builder names (`cleanupContainerArgs`, `listDanglingImageArgs`, etc.) consistent between Task 2 (defined) and Task 3 (consumed)

- [ ] **Step 7: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Notes for the implementer

- **Do not modify** `docs/FUTURES_FEATURES.md` in this plan. Marking #6 Docker Housekeeping as ✅ Implemented is done by the implement-top-feature workflow after all tasks pass.
- After all tasks, delete this plan file per the implement workflow instructions.
- If a task's changes already exist in the codebase (check `git log`), skip to the next task.