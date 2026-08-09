# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped non-Tengiz containers, dangling images, unused networks, build cache, volumes) using label-based filtering so Tengiz-managed apps are never removed.

**Architecture:** Extend the existing `dockerRuntime` in `internal/runtime` with a `Prune(ctx, opts)` method that mirrors the current `RemoveImage`/`KeepLastNImages` pattern (`internal/runtime/cleanup.go`). Pure, Docker-free helpers — `pruneArgs(category)`, `parseReclaimed(output)`, `activeCategories(opts)` — carry the command-building and output parsing so they are unit-testable without a Docker daemon. The new `cleanupCmd` in `internal/cli/root.go` translates Cobra flags into `runtime.PruneOptions`, calls `rt.Prune(...)`, and renders a per-category report via `printCleanupResults`. Everything is wired through the `runtime.Manager` interface (same seam as `RemoveImage`/`KeepLastNImages`), so CLI and all consumers talk to one abstraction.

**Tech Stack:** Go 1.26, Cobra, `os/exec` (Docker CLI, no Docker SDK), existing `runtime.Manager` interface, stdlib only (no new dependencies).

## Global Constraints

- Follow repo rules: single Go module `github.com/yaso09/tengiz`, Go 1.26 — no new external dependencies (stdlib + existing `cobra`)
- Runtime executes `docker` CLI via `os/exec` (in `internal/runtime/docker.go` + `cleanup.go`) — never use a Docker SDK
- **Label-based protection is the core requirement:** never remove any container that carries the `tengiz-app` label; `docker container prune` MUST be filtered with `--filter label!=tengiz-app`
- Image pruning removes **dangling images only** (`docker image prune -f`, no `-a`); tagged deploy images (`tengiz-apps/<app>:<deploymentID>`) remain governed by the existing `KeepLastNImages`/`RemoveImage` logic and must never be touched by cleanup
- `tengiz cleanup` is host-wide and env-agnostic — it takes **no** `--env` flag (Docker hosts are shared across environments)
- Default category set (no flags): containers, images, networks, build cache — equivalent to `docker system prune`. Volumes are pruned only with `--volumes` or `--all` because they may hold data
- **Scope:** this plan delivers the on-demand `tengiz cleanup` command (the prioritized spec: "`tengiz cleanup`" + "Label-based `docker system prune`"). *Periodic/background* execution is out of scope here — it is tracked separately as P2 #57 (Background Monitoring Scheduler) and P2 #56 (Granular Docker Prune Operations)
- Every code step lands with tests; `go build ./...`, `go vet ./...`, `go test ./... -count=1` must all pass before committing
- UI change → update `README.md`, `AGENTS.md`, and mark the feature implemented in `docs/FUTURES_FEATURES.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Modify: add `PruneCategory`, `PruneOptions`, `PruneResult` types; `activeCategories`, `pruneArgs`, `parseReclaimed` pure helpers; `(r *dockerRuntime) Prune` exec implementation |
| `internal/runtime/runtime.go` | Modify: add `Prune` to the `Manager` interface + `stubManager` implementation |
| `internal/runtime/cleanup_test.go` | Modify: tests for helpers, stub, and `Prune` dry-run path |
| `internal/cli/root.go` | Modify: add `cleanupCmd`, `cleanupPlan`, `printCleanupResults`; register command + flags in `init()` |
| `internal/cli/root_test.go` | Modify: tests for command registration, flags, `cleanupPlan`, `printCleanupResults`; add `Prune` to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Modify: add `Prune` to `mockRuntime` (interface conformance) |
| `internal/idle/idle_test.go` | Modify: add `Prune` to `mockRuntime` (interface conformance) |
| `README.md` | Modify: add `### tengiz cleanup` CLI reference section + feature bullet |
| `AGENTS.md` | Modify: add `tengiz cleanup` line to CLI section; mention `Prune` on `runtime.Manager` |
| `docs/FUTURES_FEATURES.md` | Modify: mark row #6 Docker Housekeeping as ✅ Implemented |

No new files created. Changes touch 10 existing files.

---

### Task 1: Runtime prune command builders (pure helpers)

Build the Docker-argument builders and output parser as pure functions with no Docker dependency. These are the unit-testable core; later tasks reuse their exact signatures.

**Files:**
- Modify: `internal/runtime/cleanup.go` (append after existing content, currently 59 lines)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new (package `runtime`, stdlib `context`/`strings` only)
- Produces:
  - `type PruneCategory string`
  - Constants `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache` of type `PruneCategory`, with values `"containers"`, `"images"`, `"volumes"`, `"networks"`, `"build-cache"`
  - `type PruneOptions struct { Containers, Images, Volumes, Networks, BuildCache, DryRun bool }`
  - `type PruneResult struct { Category PruneCategory; DryRun bool; Args []string; Reclaimed string; Err error }`
  - `func activeCategories(opts PruneOptions) []PruneCategory`
  - `func pruneArgs(category PruneCategory) []string`
  - `func parseReclaimed(output string) string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestPruneArgs(t *testing.T) {
	tests := []struct {
		category PruneCategory
		want     []string
	}{
		{PruneContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{PruneImages, []string{"image", "prune", "-f"}},
		{PruneVolumes, []string{"volume", "prune", "-f"}},
		{PruneNetworks, []string{"network", "prune", "-f"}},
		{PruneBuildCache, []string{"builder", "prune", "-f"}},
		{PruneCategory("bogus"), nil},
	}
	for _, tc := range tests {
		got := pruneArgs(tc.category)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("pruneArgs(%q) = %v, want %v", tc.category, got, tc.want)
		}
	}
}

func TestParseReclaimed(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"Deleted Containers:\n[...]\n\nTotal reclaimed space: 123.4MB\n", "123.4MB"},
		{"Some deleted\nTotal reclaimed space: 0B\n", "0B"},
		{"No containers to delete.\n", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := parseReclaimed(tc.output)
		if got != tc.want {
			t.Errorf("parseReclaimed(%q) = %q, want %q", tc.output, got, tc.want)
		}
	}
}

func TestActiveCategoriesOrderAndFilter(t *testing.T) {
	tests := []struct {
		opts PruneOptions
		want []PruneCategory
	}{
		{PruneOptions{}, []PruneCategory{}},
		{PruneOptions{Containers: true}, []PruneCategory{PruneContainers}},
		{PruneOptions{Containers: true, Images: true}, []PruneCategory{PruneContainers, PruneImages}},
		{PruneOptions{Volumes: true, Networks: true, BuildCache: true}, []PruneCategory{PruneVolumes, PruneNetworks, PruneBuildCache}},
		{PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}, []PruneCategory{
			PruneContainers, PruneImages, PruneVolumes, PruneNetworks, PruneBuildCache,
		}},
	}
	for _, tc := range tests {
		got := activeCategories(tc.opts)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("activeCategories(%+v) = %v, want %v", tc.opts, got, tc.want)
		}
	}
}
```

Note: this test needs a `reflect` import. The current `internal/runtime/cleanup_test.go` imports only `"context"` and `"testing"` — add `"reflect"` and `"github.com/yaso09/tengiz/internal/runtime"`? No — the test is in `package runtime`, so just add `"reflect"`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneArgs|TestParseReclaimed|TestActiveCategoriesOrderAndFilter" -v -count=1`

Expected: FAIL — compile errors `undefined: PruneCategory`, `undefined: pruneArgs`, `undefined: parseReclaimed`, `undefined: activeCategories`.

- [ ] **Step 3: Implement the types and helpers**

Append to `internal/runtime/cleanup.go` (after the existing `KeepLastNImages`):

```go
type PruneCategory string

const (
	PruneContainers PruneCategory = "containers"
	PruneImages     PruneCategory = "images"
	PruneVolumes    PruneCategory = "volumes"
	PruneNetworks   PruneCategory = "networks"
	PruneBuildCache PruneCategory = "build-cache"
)

type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type PruneResult struct {
	Category  PruneCategory
	DryRun    bool
	Args      []string
	Reclaimed string
	Err       error
}

func activeCategories(opts PruneOptions) []PruneCategory {
	var categories []PruneCategory
	if opts.Containers {
		categories = append(categories, PruneContainers)
	}
	if opts.Images {
		categories = append(categories, PruneImages)
	}
	if opts.Volumes {
		categories = append(categories, PruneVolumes)
	}
	if opts.Networks {
		categories = append(categories, PruneNetworks)
	}
	if opts.BuildCache {
		categories = append(categories, PruneBuildCache)
	}
	return categories
}

func pruneArgs(category PruneCategory) []string {
	switch category {
	case PruneContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case PruneImages:
		return []string{"image", "prune", "-f"}
	case PruneVolumes:
		return []string{"volume", "prune", "-f"}
	case PruneNetworks:
		return []string{"network", "prune", "-f"}
	case PruneBuildCache:
		return []string{"builder", "prune", "-f"}
	default:
		return nil
	}
}

func parseReclaimed(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneArgs|TestParseReclaimed|TestActiveCategoriesOrderAndFilter" -v -count=1`

Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add prune command builders for cleanup"
```

---

### Task 2: Wire `Prune` into the `Manager` interface

Expose pruning through the `runtime.Manager` interface with a real `dockerRuntime` implementation, a no-op `stubManager` implementation, and updated mock implementations in all three test packages that already implement `Manager`. The interface change and all implementer updates must land in the same task or the module won't compile.

**Files:**
- Modify: `internal/runtime/cleanup.go` — add the `(r *dockerRuntime) Prune` method (end of file)
- Modify: `internal/runtime/runtime.go:31-49` — add `Prune` to the `Manager` interface
- Modify: `internal/runtime/runtime.go:113-122` — add `Prune` to `stubManager`
- Modify: `internal/proxy/proxy_test.go:35` — add `Prune` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:34` — add `Prune` to `mockRuntime`
- Modify: `internal/cli/root_test.go:100` — add `Prune` to `mockRTForDeploy`
- Test: `internal/runtime/cleanup_test.go` — add `TestStubPrune` and `TestPruneDryRun`

**Interfaces:**
- Consumes: Task 1 types (`PruneOptions`, `PruneResult`, `activeCategories`, `pruneArgs`, `parseReclaimed`)
- Produces: `func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) ([]PruneResult, error)`; `Prune` on the `Manager` interface with the same signature. Later tasks consume `rt.Prune(ctx, opts)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	results, err := m.Prune(context.Background(), PruneOptions{Containers: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected no results from stub, got %v", results)
	}
}

func TestPruneDryRun(t *testing.T) {
	r := &dockerRuntime{}
	results, err := r.Prune(context.Background(), PruneOptions{Containers: true, Images: true, DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Category != PruneContainers || results[1].Category != PruneImages {
		t.Errorf("categories out of order: %v, %v", results[0].Category, results[1].Category)
	}
	if !results[0].DryRun {
		t.Error("result[0] missing DryRun flag")
	}
	if len(results[0].Args) != 5 || results[0].Args[4] != "label!=tengiz-app" {
		t.Errorf("result[0].Args = %v, want label-filtered container prune args", results[0].Args)
	}
	if results[0].Reclaimed != "(dry run)" {
		t.Errorf("result[0].Reclaimed = %q, want %q", results[0].Reclaimed, "(dry run)")
	}
}

func TestPruneNoCategories(t *testing.T) {
	r := &dockerRuntime{}
	results, err := r.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty opts, got %d", len(results))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestPruneDryRun|TestPruneNoCategories" -v -count=1`

Expected: FAIL — compile errors `stubManager.Prune undefined`, `dockerRuntime.Prune undefined`. (At this point `pruneArgs`, `parseReclaimed`, `activeCategories` already exist from Task 1.)

- [ ] **Step 3: Add `Prune` to the `Manager` interface**

In `internal/runtime/runtime.go`, add this line to the `Manager` interface block (after the `KeepLastNImages` line, current line 36):

```go
	Prune(ctx context.Context, opts PruneOptions) ([]PruneResult, error)
```

- [ ] **Step 4: Add `Prune` to `stubManager`**

In `internal/runtime/runtime.go`, after the `KeepLastNImages` stub implementation (current line 119):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) ([]PruneResult, error) {
	return nil, nil
}
```

- [ ] **Step 5: Implement `Prune` on `dockerRuntime`**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) ([]PruneResult, error) {
	var results []PruneResult
	for _, category := range activeCategories(opts) {
		args := pruneArgs(category)
		result := PruneResult{Category: category, DryRun: opts.DryRun, Args: args}
		if opts.DryRun {
			result.Reclaimed = "(dry run)"
			results = append(results, result)
			continue
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			result.Err = fmt.Errorf("docker %s: %w\n%s", category, err, string(out))
			results = append(results, result)
			continue
		}
		result.Reclaimed = parseReclaimed(string(out))
		results = append(results, result)
	}
	return results, nil
}
```

(`exec` and `fmt` are already imported in `internal/runtime/cleanup.go`.)

- [ ] **Step 6: Update the remaining `Manager` implementers**

Add this method to each of the three mock structs (they must satisfy `Manager` after the interface change):

In `internal/proxy/proxy_test.go`, after the `Run` line (current line 35):
```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) ([]runtime.PruneResult, error) { return nil, nil }
```

In `internal/idle/idle_test.go`, after the `Run` line (current line 34):
```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) ([]runtime.PruneResult, error) { return nil, nil }
```

In `internal/cli/root_test.go`, after the `Run` line (current line 100):
```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) ([]runtime.PruneResult, error) { return nil, nil }
```

- [ ] **Step 7: Run tests and verify compilation**

Run: `go test ./internal/runtime/... ./internal/proxy/... ./internal/idle/... ./internal/cli/... -count=1`

Run: `go build ./... && go vet ./...`

Expected: all packages PASS, build and vet clean.

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Prune to Manager interface and docker implementation"
```

---

### Task 3: `tengiz cleanup` CLI command + documentation

Add the user-facing command, its flag-to-options planner, and report printer; then document the feature in `README.md`, `AGENTS.md`, and `docs/FUTURES_FEATURES.md`.

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupPlan`, `printCleanupResults`, `cleanupCmd`; register command + flags in `init()`
- Modify: `internal/cli/root_test.go` — add `TestCleanupCommandRegistered`, `TestCleanupHelpListsFlags`, `TestCleanupPlan`, `TestPrintCleanupResults`
- Modify: `README.md` — add `### tengiz cleanup` section + feature bullet
- Modify: `AGENTS.md` — add CLI line + update `runtime.Manager` row
- Modify: `docs/FUTURES_FEATURES.md` — mark row #6 implemented

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult`, `rt.Prune(ctx, opts)` from Task 2
- Produces:
  - `func cleanupPlan(dryRun, containers, images, volumes, networks, buildCache, all bool) runtime.PruneOptions`
  - `func printCleanupResults(w io.Writer, results []runtime.PruneResult, dryRun bool)`
  - `cleanupCmd *cobra.Command` (registered on `rootCmd`)

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`. This requires adding `"errors"` and `"reflect"` to the existing imports (current imports: `bytes`, `context`, `io`, `os`, `path/filepath`, `strings`, `sync/atomic`, `testing`, `github.com/spf13/cobra`, `github.com/yaso09/tengiz/internal/config`, `github.com/yaso09/tengiz/internal/runtime`, `github.com/yaso09/tengiz/internal/types`):

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupHelpListsFlags(t *testing.T) {
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
	for _, flag := range []string{"--dry-run", "--containers", "--images", "--volumes", "--networks", "--build-cache", "--all"} {
		if !strings.Contains(helpText, flag) {
			t.Errorf("help text missing flag %q", flag)
		}
	}
}

func TestCleanupPlan(t *testing.T) {
	tests := []struct {
		name                                            string
		dryRun, containers, images, volumes, networks, buildCache, all bool
		want                                            runtime.PruneOptions
	}{
		{
			name: "no flags defaults to safe set",
			want: runtime.PruneOptions{Containers: true, Images: true, Networks: true, BuildCache: true},
		},
		{
			name:    "explicit single category",
			volumes: true,
			want:    runtime.PruneOptions{Volumes: true},
		},
		{
			name: "all enables everything",
			all:  true,
			want: runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true},
		},
		{
			name:    "dry run preserved and defaults applied",
			dryRun:  true,
			want:    runtime.PruneOptions{Containers: true, Images: true, Networks: true, BuildCache: true, DryRun: true},
		},
		{
			name:       "all overrides dry run flag",
			dryRun:     true,
			all:        true,
			want:       runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true, DryRun: true},
		},
	}
	for _, tc := range tests {
		got := cleanupPlan(tc.dryRun, tc.containers, tc.images, tc.volumes, tc.networks, tc.buildCache, tc.all)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: cleanupPlan() = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestPrintCleanupResults(t *testing.T) {
	var buf bytes.Buffer
	printCleanupResults(&buf, []runtime.PruneResult{
		{Category: runtime.PruneContainers, DryRun: true, Args: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{Category: runtime.PruneImages, DryRun: true, Args: []string{"image", "prune", "-f"}},
	}, true)
	out := buf.String()
	if !strings.Contains(out, "would run: docker container prune -f --filter label!=tengiz-app") {
		t.Errorf("dry-run output missing container command, got: %s", out)
	}
	if !strings.Contains(out, "nothing was removed (dry run)") {
		t.Errorf("dry-run output missing summary, got: %s", out)
	}

	buf.Reset()
	printCleanupResults(&buf, []runtime.PruneResult{
		{Category: runtime.PruneVolumes, Reclaimed: "1.2GB"},
		{Category: runtime.PruneNetworks, Err: errors.New("docker network prune: exit status 1")},
	}, false)
	out = buf.String()
	if !strings.Contains(out, "reclaimed 1.2GB") {
		t.Errorf("output missing reclaimed size, got: %s", out)
	}
	if !strings.Contains(out, "docker network prune: exit status 1") {
		t.Errorf("output missing per-category error, got: %s", out)
	}
	if !strings.Contains(out, "cleanup complete") {
		t.Errorf("output missing summary, got: %s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — compile errors `undefined: cleanupPlan`, `undefined: printCleanupResults`; `TestCleanupCommandRegistered` fails with "command not found"; `TestCleanupHelpListsFlags` fails because cobra returns "unknown command \"cleanup\"".

- [ ] **Step 3: Implement the command planner and printer**

In `internal/cli/root.go`, add these two helpers near `getEnv` (after the `rootCmd` definition block, current line ~95):

```go
func cleanupPlan(dryRun, containers, images, volumes, networks, buildCache, all bool) runtime.PruneOptions {
	if all {
		containers, images, volumes, networks, buildCache = true, true, true, true, true
	}
	if !containers && !images && !volumes && !networks && !buildCache {
		containers, images, networks, buildCache = true, true, true, true
	}
	return runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		DryRun:     dryRun,
	}
}

func printCleanupResults(w io.Writer, results []runtime.PruneResult, dryRun bool) {
	for _, res := range results {
		if dryRun {
			fmt.Fprintf(w, "[tengiz] %-12s would run: docker %s\n", res.Category, strings.Join(res.Args, " "))
			continue
		}
		if res.Err != nil {
			fmt.Fprintf(w, "[tengiz] %-12s error: %v\n", res.Category, res.Err)
			continue
		}
		fmt.Fprintf(w, "[tengiz] %-12s reclaimed %s\n", res.Category, res.Reclaimed)
	}
	if dryRun {
		fmt.Fprintln(w, "[tengiz] nothing was removed (dry run)")
		return
	}
	fmt.Fprintln(w, "[tengiz] cleanup complete")
}
```

- [ ] **Step 4: Implement `cleanupCmd`**

In `internal/cli/root.go`, add the command (e.g. right after `runCmd`, current line ~1162):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long: `Remove unused Docker resources (stopped non-Tengiz containers, dangling
images, unused networks and build cache) to free disk space on the host.

Containers managed by Tengiz (labeled tengiz-app) are always protected.
Tagged deploy images are never touched - only dangling images are removed.

Examples:
  tengiz cleanup            # prune containers, images, networks, build cache
  tengiz cleanup --dry-run  # show what would be removed without removing
  tengiz cleanup --all      # also prune unused Docker volumes`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		all, _ := cmd.Flags().GetBool("all")

		opts := cleanupPlan(dryRun, containers, images, volumes, networks, buildCache, all)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		results, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		printCleanupResults(os.Stdout, results, opts.DryRun)
		return nil
	},
}
```

- [ ] **Step 5: Register the command and its flags**

In `internal/cli/root.go` inside `init()` (current `rootCmd.AddCommand(rollbackCmd)` is at line 65 — add after the `buildLogsCmd` registration block near current line 66):

```go
	rootCmd.AddCommand(cleanupCmd)
```

And add the flags in `init()` after the existing flag registrations (after current line 88 `webhookCmd.Flags().String("config", ...)`, still inside `init()`):

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused Docker volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused Docker networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "prune all categories, including volumes")
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS (all four tests).

Run full suite + gate: `go test ./... -count=1 && go build ./... && go vet ./...`

Expected: all packages PASS, build and vet clean.

- [ ] **Step 7: Update `README.md`**

Add a feature bullet to the Features list (after the "**Deployment history**" bullet, current line 20):

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, images, networks, and build cache (with optional volumes) in one command while protecting Tengiz-managed apps via labels.
```

Add this section to the CLI Reference, after the `### tengiz rollback <app>` section and before `### tengiz domain` (current line ~237):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to free disk space on the host. Tengiz-managed containers (labeled `tengiz-app`) are always protected, and tagged deploy images are never touched.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling images |
| `--networks` | Prune unused Docker networks |
| `--build-cache` | Prune Docker build cache |
| `--volumes` | Prune unused Docker volumes |
| `--all` | Prune all categories, including volumes |

With no category flags, defaults to containers, images, networks, and build cache — equivalent to `docker system prune`. Volumes are only pruned with `--volumes` or `--all` because they may hold data.

Examples:
```
tengiz cleanup            # prune containers, images, networks, build cache
tengiz cleanup --dry-run  # preview what would be removed
tengiz cleanup --all      # also prune unused Docker volumes
```
```

- [ ] **Step 8: Update `AGENTS.md`**

Add a CLI line after the `tengiz rollback <app>` line (current line 60):

```
tengiz cleanup            → prune unused Docker resources (--dry-run, --all, category flags)
```

Update the `runtime.Manager` row (current line 15) to name the new method: change `Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup.` to `Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup; `Prune(ctx, opts)` for label-based `tengiz cleanup`.`

- [ ] **Step 9: Mark the feature implemented in `docs/FUTURES_FEATURES.md`**

Edit row #6 in the P0 priority table (current line 19): change the second column from `⬜` to `✅` so it reads:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 10: Final verification**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: build clean, vet clean, all test packages PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "feat(cli): add tengiz cleanup command and documentation"
```