# Docker Housekeeping (tengiz cleanup) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes stopped/unmanaged containers, dangling images, unused volumes and networks, and the Docker build cache — always protecting Tengiz-managed containers via label-based filtering — with a `--dry-run` preview.

**Architecture:** The `runtime.Manager` interface grows a single `Prune(ctx, CleanupOptions) (CleanupSummary, error)` method. The Docker implementation follows the codebase's existing "list candidates then remove each" pattern (no Docker SDK): it reads candidate names/IDs via `docker ps -a --format {{json .}}` / `docker images --filter dangling=true` / `docker volume ls -f dangling=true` / `docker network ls -f dangling=true`, filters them through small pure helper functions, and removes each with `docker rm` / `docker rmi` / `docker volume rm` / `docker network rm`. `--dry-run` only lists candidates and never executes a destructive command. The CLI wires flags into `CleanupOptions` and prints the summary; a `runCleanup(ctx, rt, opts, w)` seam keeps the command testable without Docker. Per-app old-image retention stays with the existing `KeepLastNImages` (already invoked at deploy time).

**Tech Stack:** Go 1.26 stdlib, Cobra CLI, existing exec-based `runtime.dockerRuntime` (no new dependencies). Tests use `go test ./... -v -count=1` and `go vet ./...`.

## Global Constraints

- No new external dependencies (stdlib + `os/exec` only, matching every other runtime operation)
- Managed protection: any container whose label set contains a `tengiz-app=` label is **never** removed by cleanup, no matter its state (`hasManagedLabel`)
- Default Docker state for containers: only `running` containers are skipped; `exited`/`created`/`dead`/`paused`/`restarting` are candidates
- Resource categories and scope: containers = stopped/unmanaged only; images = dangling only; volumes = unused; networks = unused; build cache = `docker builder prune -f`
- `--dry-run` must return candidates in the summary without running any destructive command (`docker rm`, `docker rmi`, `docker volume rm`, `docker network rm`, `docker builder prune`)
- Naming contract used identically everywhere (interface, CLI, tests): `CleanupOptions{Containers, Images, Volumes, Networks, BuildCache, DryRun bool}` and `CleanupSummary{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved []string, BuildCacheOutput string}`, method `Prune(ctx context.Context, opts CleanupOptions) (CleanupSummary, error)`
- `tengiz cleanup` with no category flag (or with `--all`) cleans every category; giving at least one category flag limits cleanup to those categories
- All existing tests must pass unmodified **except** the 3 mock runtime files that gain the new `Prune` method (listed in Task 1)
- Out of scope (future plans): stale Tengiz versioned containers from crashed zero-downtime deploys (Container Retention Policy), periodic/scheduled cleanup (Background Monitoring Scheduler), per-app old-image pruning (already handled by `KeepLastNImages` at deploy time), `docker system prune`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`/`CleanupSummary` types, `Prune` to `Manager` interface, stub impl |
| `internal/runtime/prune.go` | **Create.** Pure helpers (`hasManagedLabel`, `selectUnmanagedStopped`, `parseNameLines`, `build*Args`) + candidate listers + `Prune` on `dockerRuntime` |
| `internal/runtime/cleanup_test.go` | Stub `Prune` test (Task 1) |
| `internal/runtime/prune_test.go` | **Create.** Unit tests for all pure helpers (Task 2) |
| `internal/idle/idle_test.go` | Add `Prune` method to its `mockRuntime` (Task 1) |
| `internal/proxy/proxy_test.go` | Add `Prune` method to its `mockRuntime` (Task 1) |
| `internal/cli/root_test.go` | Add `Prune` method to `mockRTForDeploy` (Task 1) |
| `internal/cli/cleanup.go` | **Create.** `cleanupCmd`, `cleanupOptionsFromFlags`, `runCleanup`, `writeCleanupSummary` |
| `internal/cli/root.go` | Register `cleanupCmd` + its flags in `init()` |
| `internal/cli/cleanup_test.go` | **Create.** Registration, flag, options-resolution, and summary/output tests (Task 3) |
| `README.md` | Document `tengiz cleanup` in CLI Reference (Task 4) |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented (Task 4) |

---

### Task 1: Cleanup types + `Prune` on the Manager interface

**Files:**
- Modify: `internal/runtime/runtime.go:18-31` — add types; modify `Manager` interface (line 31-49); add stub method near `KeepLastNImages` (line 117)
- Test: `internal/runtime/cleanup_test.go:8-19`
- Modify (mock implementations — each gains one `Prune` method): `internal/idle/idle_test.go:34`, `internal/proxy/proxy_test.go:34`, `internal/cli/root_test.go:100`

**Interfaces:**
- Consumes: nothing new
- Produces: `type CleanupOptions struct { Containers, Images, Volumes, Networks, BuildCache, DryRun bool }`; `type CleanupSummary struct { ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved []string; BuildCacheOutput string }`; `Manager.Prune(ctx context.Context, opts CleanupOptions) (CleanupSummary, error)`; `stubManager.Prune(...) (CleanupSummary, error)` returning an empty summary

- [ ] **Step 1: Write the failing stub test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	summary, err := m.Prune(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(summary.ContainersRemoved) != 0 {
		t.Fatalf("expected 0 containers removed, got %d", len(summary.ContainersRemoved))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: FAIL — compile error `undefined: CleanupOptions` and `m.Prune undefined (type runtime.Manager has no field or method Prune)`

- [ ] **Step 3: Add types + interface method + stub implementation in `internal/runtime/runtime.go`**

Insert the two types directly after the `RunOptions` struct (currently at lines 26-29):

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type CleanupSummary struct {
	ContainersRemoved []string `json:"containers_removed"`
	ImagesRemoved     []string `json:"images_removed"`
	VolumesRemoved    []string `json:"volumes_removed"`
	NetworksRemoved   []string `json:"networks_removed"`
	BuildCacheOutput  string   `json:"build_cache_output,omitempty"`
}
```

Add `Prune` to the `Manager` interface right after `KeepLastNImages` (line 36):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts CleanupOptions) (CleanupSummary, error)
```

Add the stub implementation right after the `KeepLastNImages` stub (after line 119):

```go
func (m *stubManager) Prune(ctx context.Context, opts CleanupOptions) (CleanupSummary, error) {
	return CleanupSummary{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: PASS

- [ ] **Step 5: Add `Prune` to the three mock implementations**

This is required for the other packages to keep compiling (each mock implements `runtime.Manager`). Add the exact same method to each mock:

`internal/idle/idle_test.go` (after line 33, the `KeepLastNImages` mock):
```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupSummary, error) { return runtime.CleanupSummary{}, nil }
```

`internal/proxy/proxy_test.go` (after line 34, the `KeepLastNImages` mock):
```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupSummary, error) { return runtime.CleanupSummary{}, nil }
```

`internal/cli/root_test.go` (after line 99, the `KeepLastNImages` mock):
```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupSummary, error) { return runtime.CleanupSummary{}, nil }
```

Note: these are intentionally one-liners to keep them identical to the existing mock style.

- [ ] **Step 6: Verify the whole module compiles and tests pass**

Run: `go test ./... -count=1`

Expected: PASS across all packages (all mocks now satisfy `runtime.Manager`)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Prune to Manager interface for docker housekeeping"
```

---

### Task 2: Docker prune implementation + pure helper tests

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`/`CleanupSummary` from Task 1, existing `dockerPS` struct (`internal/runtime/docker.go:382-388`), existing `r.Remove(ctx, name)` and `r.RemoveImage(ctx, imageTag)` methods
- Produces: `dockerRuntime.Prune(ctx, opts CleanupOptions) (CleanupSummary, error)`; package functions `hasManagedLabel(labels string) bool`, `selectUnmanagedStopped(entries []dockerPS) []string`, `parseNameLines(out string) []string`, `buildPSAllArgs() []string`, `buildDanglingImagesArgs() []string`, `buildDanglingVolumesArgs() []string`, `buildDanglingNetworksArgs() []string`, `buildBuilderPruneArgs() []string`; unexported dockerRuntime methods `listStoppedUnmanagedContainers`, `listDanglingImages`, `listDanglingVolumes`, `listDanglingNetworks`

- [ ] **Step 1: Write the failing unit tests**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"strings"
	"testing"
)

func TestHasManagedLabel(t *testing.T) {
	tests := []struct {
		labels string
		want   bool
	}{
		{"tengiz-app=myapp,tengiz-env=production", true},
		{"maintainer=me,tengiz-app=myapp", true},
		{"com.docker.compose.project=wordpress", false},
		{"tengiz-env=production", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := hasManagedLabel(tc.labels); got != tc.want {
			t.Errorf("hasManagedLabel(%q) = %v, want %v", tc.labels, got, tc.want)
		}
	}
}

func TestSelectUnmanagedStopped(t *testing.T) {
	entries := []dockerPS{
		{Name: "/myapp", State: "running", Labels: "tengiz-app=myapp,tengiz-env=production"},
		{Name: "/myapp-1735000000", State: "exited", Labels: "tengiz-app=myapp"},
		{Name: "/old-helper", State: "exited", Labels: "com.docker.compose.project=foo"},
		{Name: "/build-cache-abc", State: "created", Labels: ""},
		{Name: "/dead-box", State: "dead", Labels: "maintainer=nobody"},
	}
	got := selectUnmanagedStopped(entries)
	if len(got) != 3 {
		t.Fatalf("expected 3 candidates, got %v", got)
	}
	if got[0] != "old-helper" || got[1] != "build-cache-abc" || got[2] != "dead-box" {
		t.Fatalf("unexpected selection order: %v", got)
	}
}

func TestParseNameLines(t *testing.T) {
	out := "  sha256:abc \n\nsha256:def\n"
	got := parseNameLines(out)
	if len(got) != 2 || got[0] != "sha256:abc" || got[1] != "sha256:def" {
		t.Fatalf("parseNameLines(%q) = %v", out, got)
	}
}

func TestBuildCleanupArgs(t *testing.T) {
	cases := []struct {
		got      []string
		expected string
	}{
		{buildPSAllArgs(), "ps -a --format {{json .}}"},
		{buildDanglingImagesArgs(), "images --filter dangling=true --format {{.ID}}"},
		{buildDanglingVolumesArgs(), "volume ls -f dangling=true --format {{.Name}}"},
		{buildDanglingNetworksArgs(), "network ls -f dangling=true --format {{.Name}}"},
		{buildBuilderPruneArgs(), "builder prune -f"},
	}
	for _, c := range cases {
		if got := strings.Join(c.got, " "); got != c.expected {
			t.Errorf("args = %q, want %q", got, c.expected)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestHasManagedLabel|TestSelectUnmanagedStopped|TestParseNameLines|TestBuildCleanupArgs" -v -count=1`

Expected: FAIL — compile error `undefined: hasManagedLabel`, `undefined: selectUnmanagedStopped`, `undefined: parseNameLines`, `undefined: buildPSAllArgs`

- [ ] **Step 3: Write the implementation in `internal/runtime/prune.go`**

```go
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

const managedLabelPrefix = "tengiz-app="

func hasManagedLabel(labels string) bool {
	for _, part := range strings.Split(labels, ",") {
		if strings.HasPrefix(strings.TrimSpace(part), managedLabelPrefix) {
			return true
		}
	}
	return false
}

func selectUnmanagedStopped(entries []dockerPS) []string {
	var names []string
	for _, e := range entries {
		if e.State == "running" {
			continue
		}
		if hasManagedLabel(e.Labels) {
			continue
		}
		name := strings.TrimPrefix(e.Name, "/")
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func parseNameLines(out string) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names
}

func buildPSAllArgs() []string {
	return []string{"ps", "-a", "--format", "{{json .}}"}
}

func buildDanglingImagesArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func buildDanglingVolumesArgs() []string {
	return []string{"volume", "ls", "-f", "dangling=true", "--format", "{{.Name}}"}
}

func buildDanglingNetworksArgs() []string {
	return []string{"network", "ls", "-f", "dangling=true", "--format", "{{.Name}}"}
}

func buildBuilderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func (r *dockerRuntime) listStoppedUnmanagedContainers(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildPSAllArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps -a: %w\n%s", err, string(out))
	}
	var entries []dockerPS
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var entry dockerPS
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return selectUnmanagedStopped(entries), nil
}

func (r *dockerRuntime) listDanglingImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildDanglingImagesArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images dangling: %w\n%s", err, string(out))
	}
	return parseNameLines(string(out)), nil
}

func (r *dockerRuntime) listDanglingVolumes(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildDanglingVolumesArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	return parseNameLines(string(out)), nil
}

func (r *dockerRuntime) listDanglingNetworks(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildDanglingNetworksArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return parseNameLines(string(out)), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupSummary, error) {
	var summary CleanupSummary

	if opts.Containers {
		names, err := r.listStoppedUnmanagedContainers(ctx)
		if err != nil {
			return summary, err
		}
		for _, name := range names {
			if !opts.DryRun {
				if err := r.Remove(ctx, name); err != nil {
					return summary, err
				}
			}
			summary.ContainersRemoved = append(summary.ContainersRemoved, name)
		}
	}

	if opts.Images {
		ids, err := r.listDanglingImages(ctx)
		if err != nil {
			return summary, err
		}
		for _, id := range ids {
			if !opts.DryRun {
				if err := r.RemoveImage(ctx, id); err != nil {
					return summary, err
				}
			}
			summary.ImagesRemoved = append(summary.ImagesRemoved, id)
		}
	}

	if opts.Volumes {
		names, err := r.listDanglingVolumes(ctx)
		if err != nil {
			return summary, err
		}
		for _, name := range names {
			if !opts.DryRun {
				dcmd := exec.CommandContext(ctx, "docker", "volume", "rm", name)
				if out, err := dcmd.CombinedOutput(); err != nil {
					return summary, fmt.Errorf("docker volume rm %s: %w\n%s", name, err, string(out))
				}
			}
			summary.VolumesRemoved = append(summary.VolumesRemoved, name)
		}
	}

	if opts.Networks {
		names, err := r.listDanglingNetworks(ctx)
		if err != nil {
			return summary, err
		}
		for _, name := range names {
			if !opts.DryRun {
				dcmd := exec.CommandContext(ctx, "docker", "network", "rm", name)
				if out, err := dcmd.CombinedOutput(); err != nil {
					return summary, fmt.Errorf("docker network rm %s: %w\n%s", name, err, string(out))
				}
			}
			summary.NetworksRemoved = append(summary.NetworksRemoved, name)
		}
	}

	if opts.BuildCache && !opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", buildBuilderPruneArgs()...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return summary, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
		}
		summary.BuildCacheOutput = strings.TrimSpace(string(out))
	}

	return summary, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestHasManagedLabel|TestSelectUnmanagedStopped|TestParseNameLines|TestBuildCleanupArgs|TestStubPrune" -v -count=1`

Expected: PASS for all four new tests plus the Task 1 stub test

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): implement Prune for containers, images, volumes, networks, build cache"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:65-75` — register `cleanupCmd` and its flags in `init()`
- Test: `internal/cli/cleanup_test.go` (also uses `mockRTForDeploy` and the `captureOutput`-style seam from this task)

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupSummary`, `runtime.Manager` (from Task 1); `runtime.NewDocker()`; `mockRTForDeploy` (Task 1 adds its `Prune` method)
- Produces: package-level `cleanupCmd *cobra.Command`; `cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions`; `runCleanup(ctx context.Context, rt runtime.Manager, opts runtime.CleanupOptions, w io.Writer) error`; `writeCleanupSummary(w io.Writer, s runtime.CleanupSummary, dryRun bool)`

- [ ] **Step 1: Write the failing CLI tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func newCleanupTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "cleanup"}
	c.Flags().Bool("containers", false, "")
	c.Flags().Bool("images", false, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("build-cache", false, "")
	c.Flags().Bool("all", false, "")
	c.Flags().Bool("dry-run", false, "")
	return c
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatalf("cleanup command not found: %v", cmd)
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "build-cache", "all", "dry-run"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupDefaultsToAll(t *testing.T) {
	opts := cleanupOptionsFromFlags(newCleanupTestCmd())
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories enabled by default, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("DryRun should default to false")
	}
}

func TestCleanupCategoryFlagsScope(t *testing.T) {
	c := newCleanupTestCmd()
	c.Flags().Set("containers", "true")
	c.Flags().Set("dry-run", "true")
	opts := cleanupOptionsFromFlags(c)
	if !opts.Containers || opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("expected containers-only, got %+v", opts)
	}
	if !opts.DryRun {
		t.Error("expected DryRun true")
	}
}

func TestCleanupAllFlag(t *testing.T) {
	c := newCleanupTestCmd()
	c.Flags().Set("images", "true")
	c.Flags().Set("all", "true")
	opts := cleanupOptionsFromFlags(c)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories with --all, got %+v", opts)
	}
}

func TestRunCleanupWithMock(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	opts := runtime.CleanupOptions{Containers: true, DryRun: true}
	err := runCleanup(ctx, &mockRTForDeploy{}, opts, &buf)
	if err != nil {
		t.Fatalf("runCleanup: %v", err)
	}
	if !strings.Contains(buf.String(), "prune candidates (dry-run) containers: 0") {
		t.Errorf("unexpected output: %q", buf.String())
	}
}

func TestWriteCleanupSummary(t *testing.T) {
	s := runtime.CleanupSummary{
		ContainersRemoved: []string{"old-helper"},
		ImagesRemoved:     []string{"sha256:abc"},
		VolumesRemoved:    []string{"junk-vol"},
		NetworksRemoved:   []string{"junk-net"},
		BuildCacheOutput:  "Total reclaimed space: 12MB",
	}
	var buf bytes.Buffer
	writeCleanupSummary(&buf, s, false)
	out := buf.String()
	for _, want := range []string{
		"[tengiz] removed containers: 1",
		"  - old-helper",
		"[tengiz] removed images: 1",
		"  - sha256:abc",
		"[tengiz] removed volumes: 1",
		"  - junk-vol",
		"[tengiz] removed networks: 1",
		"  - junk-net",
		"[tengiz] build cache: Total reclaimed space: 12MB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — compile error `undefined: cleanupCmd`, `undefined: cleanupOptionsFromFlags`, `undefined: runCleanup`, `undefined: writeCleanupSummary`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Prunes unused Docker resources to reclaim disk space.

Containers managed by Tengiz (labeled tengiz-app=*) are always protected and
never removed, even when stopped. With no category flags, every resource type
is cleaned. Use --dry-run to preview what would be removed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		opts := cleanupOptionsFromFlags(cmd)
		return runCleanup(cmd.Context(), rt, opts, cmd.OutOrStdout())
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	opts := runtime.CleanupOptions{DryRun: dryRun}
	anyCategory := containers || images || volumes || networks || buildCache
	if all || !anyCategory {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
		return opts
	}
	opts.Containers = containers
	opts.Images = images
	opts.Volumes = volumes
	opts.Networks = networks
	opts.BuildCache = buildCache
	return opts
}

func runCleanup(ctx context.Context, rt runtime.Manager, opts runtime.CleanupOptions, w io.Writer) error {
	summary, err := rt.Prune(ctx, opts)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	writeCleanupSummary(w, summary, opts.DryRun)
	return nil
}

func writeCleanupSummary(w io.Writer, s runtime.CleanupSummary, dryRun bool) {
	mode := "removed"
	if dryRun {
		mode = "prune candidates (dry-run)"
	}
	fmt.Fprintf(w, "[tengiz] %s containers: %d\n", mode, len(s.ContainersRemoved))
	for _, c := range s.ContainersRemoved {
		fmt.Fprintf(w, "  - %s\n", c)
	}
	fmt.Fprintf(w, "[tengiz] %s images: %d\n", mode, len(s.ImagesRemoved))
	for _, id := range s.ImagesRemoved {
		fmt.Fprintf(w, "  - %s\n", id)
	}
	fmt.Fprintf(w, "[tengiz] %s volumes: %d\n", mode, len(s.VolumesRemoved))
	for _, v := range s.VolumesRemoved {
		fmt.Fprintf(w, "  - %s\n", v)
	}
	fmt.Fprintf(w, "[tengiz] %s networks: %d\n", mode, len(s.NetworksRemoved))
	for _, n := range s.NetworksRemoved {
		fmt.Fprintf(w, "  - %s\n", n)
	}
	if s.BuildCacheOutput != "" {
		fmt.Fprintf(w, "[tengiz] build cache: %s\n", s.BuildCacheOutput)
	}
}
```

- [ ] **Step 4: Register the command and its flags in `internal/cli/root.go` `init()`**

Add these lines right after `rootCmd.AddCommand(rollbackCmd)` (line 65), keeping the existing style:

```go
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling (untagged) images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune docker build cache")
	cleanupCmd.Flags().Bool("all", false, "prune all resource types (default when no category flag is given)")
	cleanupCmd.Flags().Bool("dry-run", false, "show candidates without removing anything")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS for all six `TestCleanup*` tests

- [ ] **Step 6: Run the full suite + vet**

Run: `go test ./... -count=1 && go vet ./...`

Expected: PASS (no vet findings)

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Documentation updates

**Files:**
- Modify: `README.md:378-418` — add `tengiz cleanup` section after the `secret` section, before `## Configuration`
- Modify: `docs/FUTURES_FEATURES.md:19` (P0 row 6) and the implemented-features table around lines 237-253
- Modify: `AGENTS.md` CLI command list (add `tengiz cleanup` line)

No code changed — run the full suite once at the end as a safety check.

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

Insert between the `secret list` section (ends line 416) and `## Configuration` (line 418):

````markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space. Containers managed by Tengiz (labeled `tengiz-app=*`) are always protected and never removed, even when stopped. With no category flags all resource types are cleaned; use `--dry-run` to preview first.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling (untagged) images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune the Docker build cache |
| `--all` | Prune all resource types (default) |
| `--dry-run` | Show what would be removed without removing anything |

```bash
tengiz cleanup                 # prune everything
tengiz cleanup --dry-run       # preview first
tengiz cleanup --images --volumes
```

````

- [ ] **Step 2: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table, change row 6 (line 19) status marker from `⬜` to `✅` and append the date to the rationale, so it reads:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. ✅ Implemented (2026-08-10). |
```

In the "✅ Implemented Features (Not Pending)" table (after line 253), add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-10) |
```

- [ ] **Step 3: Add the command to `AGENTS.md`**

In the CLI list (after the `tengiz logs` line, matching alphabetical/section flow used there), add:

```markdown
tengiz cleanup [--all|--containers|--images|--volumes|--networks|--build-cache] [--dry-run] → prune unused Docker resources; Tengiz-labeled containers are always protected
```

- [ ] **Step 4: Verify the full suite still passes**

Run: `go test ./... -count=1 && go vet ./...`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping as implemented"
```

---

## Self-Review

**1. Spec coverage** (from `docs/FUTURES_FEATURES.md`, feature #6 "Docker Housekeeping"):
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → Task 2 prunes all four resource categories; periodic scheduling intentionally deferred (Background Monitoring Scheduler #57)
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → `hasManagedLabel` + `selectUnmanagedStopped` in Task 2, covered by `TestHasManagedLabel`/`TestSelectUnmanagedStopped`
- "`tengiz cleanup` komutu eklenebilir" → Task 3
- "`DockerCleanupJob`" / helper-container cleanup → stopped non-managed containers include orphaned helper containers; a daemon job is explicitly out of scope
- Related #22 (Container Retention) and #57 (Background Monitoring) are referenced in Global Constraints as future work, not silently dropped

**2. Placeholder scan:** No "TBD"/"TODO"/"handle edge cases"/"similar to Task N" — every step contains exact file paths, full runnable code, exact commands, and expected output.

**3. Type consistency:** `CleanupOptions`/`CleanupSummary` field names are identical in runtime.go, prune.go, cleanup.go, and all tests. `Prune(ctx, opts) (CleanupSummary, error)` is the single signature on `Manager`, `stubManager`, dockerRuntime, and all three mocks. Helper names (`hasManagedLabel`, `selectUnmanagedStopped`, `parseNameLines`, `buildPSAllArgs`, `buildDangling*Args`, `buildBuilderPruneArgs`) match Task 2's "Produces" block exactly. CLI names (`cleanupCmd`, `cleanupOptionsFromFlags`, `runCleanup`, `writeCleanupSummary`) match Task 3's "Produces" block exactly. `dockerPS` is reused as declared in `internal/runtime/docker.go`.