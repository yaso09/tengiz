# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by removing unused Docker resources (stopped non-Tengiz containers, dangling images, unused volumes, unused networks) while always preserving Tengiz-managed containers via label-based filtering.

**Architecture:** A new `Cleanup(ctx, opts)` method on the existing `runtime.Manager` interface (implemented by `dockerRuntime`) follows the repo's existing exec-based pattern — candidates are listed with `docker ps -a`, `docker images`, `docker volume ls`, `docker network ls`, and the `tengiz-app` label filter is applied in Go so the decision logic is unit-testable without a Docker daemon (CI has none). A new `cleanupCmd` Cobra command wires category flags (`--containers`, `--images`, `--volumes`, `--networks`), `--all`, and `--dry-run` to it. The existing per-app versioned-image retention (`KeepLastNImages`) already runs at deploy time, so cleanup only handles dangling images.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager`/`dockerRuntime` exec pattern, `os/exec`. No new external dependencies.

## Global Constraints

- Tengiz-managed containers (Docker label `tengiz-app=<app>`) are NEVER removed by cleanup — scale-to-zero deliberately leaves them stopped
- Container cleanup removes only non-running containers that do NOT carry the `tengiz-app` label
- Image cleanup removes only dangling (untagged) images (`--filter dangling=true`); per-app versioned-image retention is already handled at deploy time by `KeepLastNImages`
- Volume and network cleanup remove only dangling/unused resources (not referenced by any container)
- Cleanup is env-agnostic: it protects every environment's containers via the shared `tengiz-app` label, so the command takes NO `--env` flag
- With no category selected, `tengiz cleanup` must fail with an explicit error (destructive-op safety); `--dry-run` lists candidates without removing anything
- No new external dependencies (go.mod unchanged)
- All tests must pass WITHOUT a Docker daemon (CI runs `go test ./... -v -count=1` on ubuntu-latest with no Docker) — use pure-function, arg-builder, and stub tests; any docker-exec test must skip when the daemon is unavailable
- `runtime.Manager` is implemented by `stubManager` plus three test mocks (`mockRuntime` in `internal/proxy/proxy_test.go` and `internal/idle/idle_test.go`, `mockRTForDeploy` in `internal/cli/root_test.go`) — every task must keep the module compiling, so all four gain the new `Cleanup` method
- Every task ends with a passing test run and a commit

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `CleanupOptions`, `CleanupSummary`, `(*dockerRuntime).Cleanup` orchestration, list/remove helpers, `hasLabel`, `selectCleanupContainers`, `splitLines` |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + stub method |
| `internal/runtime/cleanup_test.go` | Pure-function tests, stub test, skippable Docker smoke test |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` to `mockRuntime` |
| `internal/cli/cleanup.go` | New `cleanupCmd` Cobra command + `formatList` helper |
| `internal/cli/cleanup_test.go` | Command registration, flag parsing, no-category error, `formatList` tests |
| `internal/cli/root.go` | Register `cleanupCmd` + flags in `init()` |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy` |
| `README.md` | Features bullet + `tengiz cleanup` CLI reference section |
| `AGENTS.md` | Add `tengiz cleanup` to CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark Docker Housekeeping as implemented |

Reused existing symbols (do NOT redefine): `labelKey` and `dockerPS` from `internal/runtime/docker.go`; `(*dockerRuntime).Remove` and `(*dockerRuntime).RemoveImage` from `docker.go`/`cleanup.go`.

---

### Task 1: Cleanup types + label-based container selection helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` (add `encoding/json` to imports)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new (reuses `dockerPS`, `labelKey` from `internal/runtime/docker.go`)
- Produces: `CleanupOptions{ DryRun, Containers, Images, Volumes, Networks bool }`, `CleanupSummary{ ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved []string }`, `hasLabel(labels, key string) bool`, `selectCleanupContainers(psOutput string) []string`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestHasLabel(t *testing.T) {
	tests := []struct {
		labels string
		key    string
		want   bool
	}{
		{"tengiz-app=myapp,tengiz-env=production", "tengiz-app", true},
		{"tengiz-app=myapp", "tengiz-env", false},
		{"tengiz-env=production", "tengiz-app", false},
		{"", "tengiz-app", false},
		{"random=value,tengiz-app=other", "tengiz-app", true},
	}
	for _, tt := range tests {
		if got := hasLabel(tt.labels, tt.key); got != tt.want {
			t.Errorf("hasLabel(%q, %q) = %v, want %v", tt.labels, tt.key, got, tt.want)
		}
	}
}

func TestSelectCleanupContainers(t *testing.T) {
	output := `{"ID":"aaa111","Name":"/junk","State":"Exited (0) 2 hours ago","Ports":"","Labels":""}
{"ID":"bbb222","Name":"/tengiz-myapp","State":"Exited (0) 1 hour ago","Ports":"","Labels":"tengiz-app=myapp,tengiz-env=production"}
{"ID":"ccc333","Name":"/web","State":"running","Ports":"","Labels":""}
{"ID":"ddd444","Name":"/sidecar","State":"Dead","Ports":"","Labels":"com.example=x"}
{"ID":"eee555","Name":"/staging","State":"Exited (137) 3 days ago","Ports":"","Labels":"tengiz-app=staging,tengiz-env=staging"}
`
	got := selectCleanupContainers(output)
	want := []string{"aaa111", "ddd444"}
	if len(got) != len(want) {
		t.Fatalf("selectCleanupContainers() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("selectCleanupContainers()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestHasLabel|TestSelectCleanupContainers" -v -count=1`

Expected: FAIL with `undefined: hasLabel` / `undefined: selectCleanupContainers`

- [ ] **Step 3: Write minimal implementation**

In `internal/runtime/cleanup.go`, change the import block to:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)
```

Then append (keep the existing `RemoveImage`/`KeepLastNImages` functions):

```go
type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
}

type CleanupSummary struct {
	ContainersRemoved []string
	ImagesRemoved     []string
	VolumesRemoved    []string
	NetworksRemoved   []string
}

// hasLabel reports whether the comma-separated Docker label string contains key.
func hasLabel(labels, key string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 && kv[0] == key {
			return true
		}
	}
	return false
}

// selectCleanupContainers parses `docker ps -a --format '{{json .}}'` output and
// returns the IDs of stopped containers that are NOT managed by Tengiz.
// Tengiz containers (label tengiz-app) are preserved because scale-to-zero
// deliberately leaves them stopped.
func selectCleanupContainers(psOutput string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(psOutput), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry dockerPS
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.State == "running" {
			continue
		}
		if hasLabel(entry.Labels, labelKey) {
			continue
		}
		ids = append(ids, entry.ID)
	}
	return ids
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestHasLabel|TestSelectCleanupContainers" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS (these tests never need a Docker daemon)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add cleanup types and label-based container selection"
```

---

### Task 2: Implement `(*dockerRuntime).Cleanup` orchestration

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Test: `internal/runtime/cleanup_test.go` (add `os/exec` and `context` imports — `context` is already imported)

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupSummary`, `selectCleanupContainers` from Task 1; `(*dockerRuntime).Remove` and `(*dockerRuntime).RemoveImage` (existing)
- Produces: `(*dockerRuntime).Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error)`, `splitLines(s string) []string`, `listStoppedNonTengizContainers(ctx) ([]string, error)`, `listDanglingImages(ctx) ([]string, error)`, `listDanglingVolumes(ctx) ([]string, error)`, `listDanglingNetworks(ctx) ([]string, error)`, `removeVolume(ctx, name string) error`, `removeNetwork(ctx, id string) error`, `dockerAvailable() bool` (test-only)

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go` and add `"os/exec"` to its imports (it already imports `context` and `testing`):

```go
func TestSplitLines(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"abc\n", []string{"abc"}},
		{"abc\ndef\n", []string{"abc", "def"}},
		{"  abc  \n\n def \n", []string{"abc", "def"}},
	}
	for _, tt := range tests {
		got := splitLines(tt.input)
		if len(got) != len(tt.want) {
			t.Fatalf("splitLines(%q) = %v (len=%d), want %v (len=%d)", tt.input, got, len(got), tt.want, len(tt.want))
		}
		for i := range tt.want {
			if got[i] != tt.want[i] {
				t.Errorf("splitLines(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func dockerAvailable() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	return exec.Command("docker", "version", "--format", "{{.Server.Version}}").Run() == nil
}

func TestDockerCleanupSmoke(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker daemon not available")
	}
	r := &dockerRuntime{}
	summary, err := r.Cleanup(context.Background(), CleanupOptions{
		DryRun:     true,
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup(dry-run) error = %v", err)
	}
	_ = summary
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestSplitLines|TestDockerCleanupSmoke" -v -count=1`

Expected: FAIL with `undefined: splitLines` and `r.Cleanup undefined`

- [ ] **Step 3: Write minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error) {
	var summary CleanupSummary

	if opts.Containers {
		ids, err := listStoppedNonTengizContainers(ctx)
		if err != nil {
			return summary, err
		}
		if opts.DryRun {
			summary.ContainersRemoved = ids
		} else {
			for _, id := range ids {
				if err := r.Remove(ctx, id); err != nil {
					log.Printf("[runtime] cleanup: failed to remove container %s: %v", id, err)
					continue
				}
				summary.ContainersRemoved = append(summary.ContainersRemoved, id)
			}
		}
	}

	if opts.Images {
		ids, err := listDanglingImages(ctx)
		if err != nil {
			return summary, err
		}
		if opts.DryRun {
			summary.ImagesRemoved = ids
		} else {
			for _, id := range ids {
				if err := r.RemoveImage(ctx, id); err != nil {
					log.Printf("[runtime] cleanup: failed to remove image %s: %v", id, err)
					continue
				}
				summary.ImagesRemoved = append(summary.ImagesRemoved, id)
			}
		}
	}

	if opts.Volumes {
		names, err := listDanglingVolumes(ctx)
		if err != nil {
			return summary, err
		}
		if opts.DryRun {
			summary.VolumesRemoved = names
		} else {
			for _, name := range names {
				if err := removeVolume(ctx, name); err != nil {
					log.Printf("[runtime] cleanup: failed to remove volume %s: %v", name, err)
					continue
				}
				summary.VolumesRemoved = append(summary.VolumesRemoved, name)
			}
		}
	}

	if opts.Networks {
		ids, err := listDanglingNetworks(ctx)
		if err != nil {
			return summary, err
		}
		if opts.DryRun {
			summary.NetworksRemoved = ids
		} else {
			for _, id := range ids {
				if err := removeNetwork(ctx, id); err != nil {
					log.Printf("[runtime] cleanup: failed to remove network %s: %v", id, err)
					continue
				}
				summary.NetworksRemoved = append(summary.NetworksRemoved, id)
			}
		}
	}

	return summary, nil
}

func listStoppedNonTengizContainers(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", `{{json .}}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return selectCleanupContainers(string(out)), nil
}

func listDanglingImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return splitLines(string(out)), nil
}

func listDanglingVolumes(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	return splitLines(string(out)), nil
}

func listDanglingNetworks(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return splitLines(string(out)), nil
}

func removeVolume(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "volume", "rm", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker volume rm %s: %w\n%s", name, err, string(out))
	}
	return nil
}

func removeNetwork(ctx context.Context, id string) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "rm", id)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker network rm %s: %w\n%s", id, err, string(out))
	}
	return nil
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestSplitLines|TestDockerCleanupSmoke" -v -count=1`

Expected: PASS (`TestDockerCleanupSmoke` reports `SKIP` when no Docker daemon)

- [ ] **Step 5: Verify build and vet**

Run: `go build ./... && go vet ./internal/runtime/...`

Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement dockerRuntime.Cleanup for disk-space housekeeping"
```

---

### Task 3: Add `Cleanup` to the `Manager` interface, stub, and test mocks

**Files:**
- Modify: `internal/runtime/runtime.go` (interface + stub)
- Modify: `internal/proxy/proxy_test.go` (`mockRuntime`)
- Modify: `internal/idle/idle_test.go` (`mockRuntime`)
- Modify: `internal/cli/root_test.go` (`mockRTForDeploy`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `(*dockerRuntime).Cleanup` from Task 2
- Produces: `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error)` contract used by Task 4

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	summary, err := m.Cleanup(context.Background(), CleanupOptions{
		DryRun:     true,
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(summary.ContainersRemoved) != 0 || len(summary.ImagesRemoved) != 0 ||
		len(summary.VolumesRemoved) != 0 || len(summary.NetworksRemoved) != 0 {
		t.Errorf("stub Cleanup should remove nothing, got %+v", summary)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubCleanup" -v -count=1`

Expected: FAIL with `m.Cleanup undefined` (stub does not implement it yet)

- [ ] **Step 3: Add `Cleanup` to the `Manager` interface**

In `internal/runtime/runtime.go`, inside the `Manager` interface, change:

```go
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Start(ctx context.Context, name string) error
```

to:

```go
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error)
	Start(ctx context.Context, name string) error
```

- [ ] **Step 4: Add `Cleanup` to the stub**

In `internal/runtime/runtime.go`, after the `KeepLastNImages` stub method, add:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupSummary, error) {
	return CleanupSummary{}, nil
}
```

- [ ] **Step 5: Add `Cleanup` to the three test mocks**

In `internal/proxy/proxy_test.go`, after the `mockRuntime.KeepLastNImages` line (line 34), add:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupSummary, error) {
	return runtime.CleanupSummary{}, nil
}
```

In `internal/idle/idle_test.go`, after the `mockRuntime.KeepLastNImages` line (line 33), add:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupSummary, error) {
	return runtime.CleanupSummary{}, nil
}
```

In `internal/cli/root_test.go`, after the `mockRTForDeploy.KeepLastNImages` line (line 99), add:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupSummary, error) {
	return runtime.CleanupSummary{}, nil
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestStubCleanup" -v -count=1`

Expected: PASS

- [ ] **Step 7: Run all tests to confirm nothing is broken**

Run: `go test ./... -v -count=1`

Expected: All PASS (except the pre-existing time-sensitive `idle` and slow `proxy` tests may take a few seconds; `TestDockerCleanupSmoke` skips without a daemon)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat: expose Cleanup on runtime.Manager interface and test mocks"
```

---

### Task 4: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` (register command + flags in `init()`)
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupSummary` from Tasks 1-3
- Produces: `cleanupCmd *cobra.Command`, `formatList(items []string) string`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
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
	for _, flag := range []string{"dry-run", "all", "containers", "images", "volumes", "networks"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	var called bool
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		if !dryRun || !all || !containers {
			t.Errorf("flags not parsed: dry-run=%v all=%v containers=%v", dryRun, all, containers)
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all", "--containers"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}

func TestCleanupCmdNoCategoryError(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no cleanup category selected")
	}
	if !strings.Contains(err.Error(), "no cleanup category selected") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFormatList(t *testing.T) {
	if got := formatList(nil); got != "none" {
		t.Errorf("formatList(nil) = %q, want \"none\"", got)
	}
	if got := formatList([]string{}); got != "none" {
		t.Errorf("formatList([]) = %q, want \"none\"", got)
	}
	if got := formatList([]string{"a", "b"}); got != "a, b" {
		t.Errorf("formatList([a b]) = %q, want \"a, b\"", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` and `undefined: formatList`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Reclaim disk space by removing unused Docker resources",
	Long: `Removes Docker resources that are no longer in use, freeing disk space.

Tengiz-managed containers (those labeled tengiz-app) are always preserved,
including stopped ones, because scale-to-zero stops containers on purpose.

Select at least one category (or use --all):
  --containers  remove stopped containers not managed by Tengiz
  --images      remove dangling (untagged) images
  --volumes     remove unused volumes
  --networks    remove unused networks

Use --dry-run to preview what would be removed without changing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")

		if all {
			containers, images, volumes, networks = true, true, true, true
		}
		if !containers && !images && !volumes && !networks {
			return fmt.Errorf("no cleanup category selected: use --all or one of --containers/--images/--volumes/--networks")
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		summary, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			DryRun:     dryRun,
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "Removed"
		if dryRun {
			verb = "Would remove"
		}
		if containers {
			fmt.Printf("[tengiz] %s %d container(s): %s\n", verb, len(summary.ContainersRemoved), formatList(summary.ContainersRemoved))
		}
		if images {
			fmt.Printf("[tengiz] %s %d image(s): %s\n", verb, len(summary.ImagesRemoved), formatList(summary.ImagesRemoved))
		}
		if volumes {
			fmt.Printf("[tengiz] %s %d volume(s): %s\n", verb, len(summary.VolumesRemoved), formatList(summary.VolumesRemoved))
		}
		if networks {
			fmt.Printf("[tengiz] %s %d network(s): %s\n", verb, len(summary.NetworksRemoved), formatList(summary.NetworksRemoved))
		}
		if dryRun {
			fmt.Println("[tengiz] dry run — no changes made")
		}
		return nil
	},
}

func formatList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}
```

- [ ] **Step 4: Register the command and flags in `internal/cli/root.go`**

Inside `init()`, after the line `rootCmd.AddCommand(runCmd)`, add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

In the same `init()` block, after the `logsCmd.Flags()` lines, add:

```go
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without changing anything")
	cleanupCmd.Flags().Bool("all", false, "clean all categories: containers, images, volumes, networks")
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling (untagged) images")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: All PASS

- [ ] **Step 6: Run all CLI tests and build**

Run: `go test ./internal/cli/... -v -count=1 && go build ./...`

Expected: All PASS, build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 5: Update documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the `tengiz cleanup` command from Task 4
- Produces: accurate user-facing documentation and an updated feature-status file

- [ ] **Step 1: Add a Features bullet in `README.md`**

In the Features list, after the `- **Deployment history** —` line (line 20), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` reclaims disk space by removing unused containers/images/volumes/networks while preserving all Tengiz-managed containers via label-based filtering.
```

- [ ] **Step 2: Add the CLI reference section in `README.md`**

After the `### \`tengiz ps\`` section (which ends at the line `Output: \`NAME\`, \`STATE\` (running/stopped), \`PORT\`, \`ENVIRONMENT\`, \`HEALTH\`.`), insert:

```markdown
### `tengiz cleanup [--all | --containers --images --volumes --networks] [--dry-run]`

Reclaim disk space by removing unused Docker resources. Tengiz-managed containers (labeled `tengiz-app`) are always preserved — including stopped ones, since scale-to-zero stops containers on purpose.

| Flag | Description |
|------|-------------|
| `--all` | Clean all four categories |
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling (untagged) images |
| `--volumes` | Remove unused volumes |
| `--networks` | Remove unused networks |
| `--dry-run` | Preview what would be removed without changing anything |

At least one category flag (or `--all`) is required. Per-app versioned-image retention (last 5) already runs automatically at deploy time; cleanup targets the leftover junk.
```

- [ ] **Step 3: Add the command to `AGENTS.md`**

In the CLI section, after the `tengiz ps` line, add:

```markdown
tengiz cleanup [--all|--containers|--images|--volumes|--networks] [--dry-run] → reclaim disk space (preserves tengiz-app-labeled containers)
```

- [ ] **Step 4: Mark the feature implemented in `docs/FUTURES_FEATURES.md`**

In the Priority Ranking table (row #6), change:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the `## Docker Housekeeping (Otomatik Temizlik)` detailed section, right after the `- **Why add to Tengiz:** ...` line, add a status line using the current date:

```markdown
- **Status:** ✅ Implemented (2026-07-31)
```

- [ ] **Step 5: Verify the CLI help renders**

Run: `go build -o tengiz . && ./tengiz cleanup --help`

Expected: Help text shows the `--all`, `--containers`, `--images`, `--volumes`, `--networks`, and `--dry-run` flags

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

---

### Task 6: Full verification

**Files:**
- No new code — verification gate only

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS (the pre-existing slow `proxy` TCP-timeout tests and time-sensitive `idle` tests may take a few seconds; `TestDockerCleanupSmoke` skips without a Docker daemon)

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 3: Build the binary**

Run: `go build -o tengiz .`

Expected: Build succeeds

- [ ] **Step 4: Manual smoke test (only if a Docker daemon is available)**

Run:

```bash
./tengiz cleanup --dry-run --all
./tengiz cleanup --containers --images --volumes --networks
```

Expected: `--dry-run` prints only `Would remove N ...` lines plus `dry run — no changes made`; the real run prints `Removed N ...` lines and no error. If `docker ps -a` shows no non-Tengiz stopped containers, both invocations print `none` counts — that is correct behavior.

- [ ] **Step 5: No commit needed** (all changes were committed in Tasks 1-5; run `git status` to confirm a clean tree)

---

## Self-Review

**Spec coverage:**
- `tengiz cleanup` command → Task 4
- Label-based filtering protecting Tengiz containers → Task 1 (`selectCleanupContainers` skips `tengiz-app`-labeled containers) and enforced in the orchestration via `listStoppedNonTengizContainers`
- Periodic cleanup of unused volumes, networks, containers, images → Task 2 (all four categories)
- Disk-space reclaim (the stated rationale) → Task 2 + Task 4 `--dry-run`/reporting

**Placeholder scan:** No TBD/TODO/"similar to" content; every code step contains complete code. All referenced symbols are defined either in this plan (`CleanupOptions`, `CleanupSummary`, `hasLabel`, `selectCleanupContainers`, `splitLines`, list/remove helpers, `cleanupCmd`, `formatList`) or already exist in the codebase (`dockerPS`, `labelKey`, `(*dockerRuntime).Remove`, `(*dockerRuntime).RemoveImage`, `runtime.NewDocker`).

**Type consistency:** `runtime.Cleanup(ctx, opts CleanupOptions) (CleanupSummary, error)` is spelled identically in Tasks 2-4. The mock methods in Task 3 use `runtime.CleanupOptions`/`runtime.CleanupSummary` qualified with the `runtime` package alias exactly as the other mock methods do. `formatList(items []string) string` is used only in Task 4. `dockerAvailable() bool` is defined in the test file of Task 2 and used only there.
