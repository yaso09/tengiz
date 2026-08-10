# Docker Housekeeping (Label-Protected `tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a label-protected `tengiz cleanup` command that removes unused Docker containers, images, volumes, networks, and build cache without ever touching scale-to-zero'd Tengiz apps, so disk space stays healthy on single-server deployments.

**Architecture:** Every Tengiz container already carries `label tengiz-app=<app>` (set in `Create`, `CreateFromImage`, `CreateVersioned`, and one-off `Run`). The runtime gets a single `Prune(ctx, opts PruneOptions) (*PruneResult, error)` method backed by per-category `docker <type> prune -f --filter label!=tengiz-app` commands, which delete only resources *without* the `tengiz-app` label. Image pruning intentionally omits `-a` (dangling-only) so tagged rollback images survive; per-app image retention stays owned by the existing `KeepLastNImages`. A `--dry-run` mode lists candidate counts via label-free `docker ps/image ls/volume ls/network ls` output parsed in Go, because Docker does not support negative label filters on `ls`/`ps` commands. The CLI validates categories via a pure `resolveCleanupOptions` helper and prints a summary via `formatPruneSummary`.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` interface, `docker` CLI via `os/exec` (no Docker SDK — matches the codebase). No new external dependencies.

## Global Constraints

- Use **exactly one** `label!=tengiz-app` filter per prune command. Docker OR-combines multiple negated label filters, which would prune nearly everything on the host — never add a second `label!=`.
- `label!=` is only valid on `prune` commands. It is **not** supported by `docker ps`, `docker image ls`, `docker volume ls`, or `docker network ls` (error: `Invalid filter`). Dry-run listings are label-free; excluded `tengiz-app` containers are filtered in Go.
- Image prune must **not** use `-a`. `docker image prune -f` removes only dangling (untagged) build intermediates; `-a` would delete old tagged rollback images that `KeepLastNImages` is meant to retain.
- Canonical category identifiers: `containers`, `images`, `volumes`, `networks`, `builder`.
- `docker builder prune -f` requires BuildKit/buildx. Its failure is **non-fatal**: log a warning and continue with the other categories.
- `tengiz cleanup` requires at least one category flag or `--all`; with none it returns an error before touching docker.
- Container names: `tengiz-<app>` (production) / `tengiz-<app>-<env>` (non-production). All carry `tengiz-app=<app>` label.
- `PruneOptions` and `PruneResult` are comparable structs (all bool/int fields) so tests can assert equality directly.
- Tests must never require a running Docker daemon. Test the pure arg builders, parsers, option resolution, summary formatter, and the stub manager.
- Adding `Prune` to the `Manager` interface requires updating both `stubManager` (in `internal/runtime/runtime.go`) and `mockRTForDeploy` (in `internal/cli/root_test.go`) in the same commit, or existing compile-time interface assertions fail.
- Summary output ends with a `\n`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | `PruneOptions`, `PruneResult` types; `Manager.Prune` interface method; `stubManager.Prune` |
| `internal/runtime/cleanup.go` | docker exec implementation: `buildPruneArgs`, `buildPruneListArgs`, `countPrunableContainers`, `countLines`, `(*dockerRuntime).Prune` |
| `internal/runtime/cleanup_test.go` | Tests for builders, parsers, `stubManager.Prune`, `dockerRuntime.Prune` (no-category) |
| `internal/cli/cleanup.go` | New `cleanupCmd` Cobra command, `resolveCleanupOptions`, `formatPruneSummary`, flag definitions |
| `internal/cli/cleanup_test.go` | Tests for option resolution, summary formatting, registration, flags, no-category error |
| `internal/cli/root.go` | Register `rootCmd.AddCommand(cleanupCmd)` in `init()` |
| `internal/cli/root_test.go` | Add `Prune` method to `mockRTForDeploy` |
| `README.md` | New `### tengiz cleanup` section in CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` line to CLI list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 ✅ Implemented |

No restructuring of existing large files needed; `cleanup.go` stays focused on housekeeping.

---

### Task 1: Prune types, Manager interface, stub + mock

**Files:**
- Modify: `internal/runtime/runtime.go` — add types after `RunOptions` (line 30), add interface method (line 48), add stub at end (after line 122)
- Modify: `internal/cli/root_test.go:69-100` — add `Prune` to `mockRTForDeploy`
- Test: `internal/runtime/cleanup_test.go` — add `TestStubPrune`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `runtime.PruneOptions{ Containers, Images, Volumes, Networks, BuildCache, DryRun bool }` (all fields bool)
  - `runtime.PruneResult{ DryRun bool; Containers, Images, Volumes, Networks int; BuildCache bool }`
  - `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)`
  - `stubManager.Prune(ctx, opts) (*PruneResult, error)` returns `&PruneResult{DryRun: opts.DryRun}, nil`
  - `mockRTForDeploy.Prune(ctx, opts) (*PruneResult, error)` returns `&PruneResult{}, nil`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true, DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res == nil || !res.DryRun {
		t.Errorf("Prune() result = %+v, want DryRun=true result", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: FAIL — `PruneOptions undefined`, `m.Prune undefined`

- [ ] **Step 3: Add types and interface method to `internal/runtime/runtime.go`**

Add after the `RunOptions` struct (line 30):

```go
type PruneOptions struct {
	Containers   bool
	Images       bool
	Volumes      bool
	Networks     bool
	BuildCache   bool
	DryRun       bool
}

type PruneResult struct {
	DryRun     bool
	Containers int
	Images     int
	Volumes    int
	Networks   int
	BuildCache bool
}
```

Add to the `Manager` interface (after `KeepLastNImages`, line 36):

```go
	Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)
```

Add to `stubManager` (after `KeepLastNImages`, line 118):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	return &PruneResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 4: Add `Prune` to `mockRTForDeploy` in `internal/cli/root_test.go`**

Add after the `KeepLastNImages` method (line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneResult, error) {
	return &runtime.PruneResult{}, nil
}
```

(`context` and `runtime` are already imported in that file.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... -run "TestStubPrune|TestStubSatisfiesInterface|TestMockRTForDeployImplementsManager" -v -count=1`

Expected: PASS (interface compile checks confirm the method is wired)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Prune method and result types to Manager"
```

---

### Task 2: Pure prune arg builders and candidate parsers

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `pruneLabelFilter` const, `buildPruneArgs`, `buildPruneListArgs`, `countPrunableContainers`, `countLines`
- Test: `internal/runtime/cleanup_test.go` — add `TestBuildPruneArgs`, `TestBuildPruneListArgs`, `TestCountPrunableContainers`, `TestCountLines`

**Interfaces:**
- Consumes: nothing (pure functions, no types from Task 1 needed)
- Produces:
  - `pruneLabelFilter = "label!=tengiz-app"` (const)
  - `buildPruneArgs(category string) []string` — full `docker <cmd>...` args for pruning a category; `nil` for unknown categories
  - `buildPruneListArgs(category string) []string` — args to list prune candidates (label-free); `nil` for unknown/`builder`
  - `countPrunableContainers(output string) int` — counts stopped non-tengiz containers from `ID|State|Labels` lines
  - `countLines(output string) int` — counts non-empty lines (used for image/volume/network candidates)

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		category string
		expected []string
	}{
		{"containers", []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"images", []string{"image", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"volumes", []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"networks", []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"builder", []string{"builder", "prune", "-f"}},
		{"unknown", nil},
	}
	for _, tt := range tests {
		got := buildPruneArgs(tt.category)
		if len(got) != len(tt.expected) {
			t.Errorf("buildPruneArgs(%q) = %v (len=%d), want %v (len=%d)", tt.category, got, len(got), tt.expected, len(tt.expected))
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("buildPruneArgs(%q)[%d] = %q, want %q", tt.category, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestBuildPruneListArgs(t *testing.T) {
	tests := []struct {
		category string
		expected []string
	}{
		{"containers", []string{"ps", "-aq", "--format", "{{.ID}}|{{.State}}|{{.Labels}}"}},
		{"images", []string{"image", "ls", "-q", "--filter", "dangling=true"}},
		{"volumes", []string{"volume", "ls", "-q", "--filter", "dangling=true"}},
		{"networks", []string{"network", "ls", "-q"}},
		{"builder", nil},
		{"unknown", nil},
	}
	for _, tt := range tests {
		got := buildPruneListArgs(tt.category)
		if len(got) != len(tt.expected) {
			t.Errorf("buildPruneListArgs(%q) = %v (len=%d), want %v (len=%d)", tt.category, got, len(got), tt.expected, len(tt.expected))
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("buildPruneListArgs(%q)[%d] = %q, want %q", tt.category, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestCountPrunableContainers(t *testing.T) {
	out := strings.Join([]string{
		"abc123|exited|tengiz-app=myapp,tengiz-env=production", // tengiz, stopped → NOT pruned
		"def456|exited|foo=bar",                                // non-tengiz, stopped → pruned
		"ghi789|running|",                                      // running → NOT pruned
		"jkl012|dead|",                                         // non-tengiz, dead → pruned
		"mno345|created|com.docker.compose.project=blog",       // non-tengiz, created → pruned
	}, "\n") + "\n"
	if got := countPrunableContainers(out); got != 3 {
		t.Errorf("countPrunableContainers() = %d, want 3 (output: %q)", got, out)
	}
}

func TestCountLines(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"  \n\t\n", 0},
		{"a\nb\n", 2},
		{"a\n\nb\n", 2},
		{"x", 1},
	}
	for _, c := range cases {
		if got := countLines(c.input); got != c.want {
			t.Errorf("countLines(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestBuildPruneListArgs|TestCountPrunableContainers|TestCountLines" -v -count=1`

Expected: FAIL with `undefined: buildPruneArgs` (etc.)

- [ ] **Step 3: Write minimal implementation in `internal/runtime/cleanup.go`**

Add at the end of the file (package-level; `strings` is already imported in `cleanup.go`):

```go
const pruneLabelFilter = "label!=tengiz-app"

var pruneCategorySingular = map[string]string{
	"containers": "container",
	"images":     "image",
	"volumes":    "volume",
	"networks":   "network",
	"builder":    "builder",
}

func buildPruneArgs(category string) []string {
	if category == "builder" {
		return []string{"builder", "prune", "-f"}
	}
	singular, ok := pruneCategorySingular[category]
	if !ok {
		return nil
	}
	return []string{singular, "prune", "-f", "--filter", pruneLabelFilter}
}

func buildPruneListArgs(category string) []string {
	switch category {
	case "containers":
		return []string{"ps", "-aq", "--format", "{{.ID}}|{{.State}}|{{.Labels}}"}
	case "images":
		return []string{"image", "ls", "-q", "--filter", "dangling=true"}
	case "volumes":
		return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
	case "networks":
		return []string{"network", "ls", "-q"}
	default:
		return nil
	}
}

func countPrunableContainers(output string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		state, labels := parts[1], parts[2]
		if state == "running" {
			continue
		}
		if strings.Contains(labels, "tengiz-app=") {
			continue
		}
		count++
	}
	return count
}

func countLines(output string) int {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return 0
	}
	n := 0
	for _, l := range strings.Split(trimmed, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestBuildPruneListArgs|TestCountPrunableContainers|TestCountLines" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add label-protected prune arg builders and candidate parsers"
```

---

### Task 3: Implementation of `(*dockerRuntime).Prune`

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `Prune` plus private helpers `optsEnabled`, `setPruneCount`, `countCandidates`, `runPrune`
- Test: `internal/runtime/cleanup_test.go` — add `TestDockerPruneNoCategories`

**Interfaces:**
- Consumes: `runtime.PruneOptions`/`runtime.PruneResult` (Task 1), `buildPruneArgs`/`buildPruneListArgs`/`countPrunableContainers`/`countLines` (Task 2)
- Produces:
  - `(*dockerRuntime).Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)` — full exec implementation on `dockerRuntime` (the struct returned by `runtime.NewDocker()`)
  - `optsEnabled(opts PruneOptions, category string) bool`
  - `setPruneCount(result *PruneResult, category string, count int)`
  - `countCandidates(ctx context.Context, category string) int`
  - `runPrune(ctx context.Context, category string) error`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestDockerPruneNoCategories(t *testing.T) {
	r := &dockerRuntime{}
	res, err := r.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() with no categories error = %v", err)
	}
	if res.DryRun || res.Containers != 0 || res.Images != 0 || res.Volumes != 0 || res.Networks != 0 || res.BuildCache {
		t.Errorf("unexpected Prune() result: %+v", res)
	}
}
```

`dockerRuntime` is an empty struct, so this test runs zero docker commands when no category is enabled — it needs no Docker daemon.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestDockerPruneNoCategories -v -count=1`

Expected: FAIL with `r.Prune undefined`

- [ ] **Step 3: Write the implementation in `internal/runtime/cleanup.go`**

Add after `countLines` (Task 2 code):

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	result := &PruneResult{DryRun: opts.DryRun}
	for _, category := range []string{"containers", "images", "volumes", "networks", "builder"} {
		if !optsEnabled(opts, category) {
			continue
		}
		if category == "builder" {
			if opts.DryRun {
				result.BuildCache = true
				continue
			}
			if err := r.runPrune(ctx, "builder"); err != nil {
				log.Printf("[runtime] build cache prune failed (buildx not available?): %v", err)
				continue
			}
			result.BuildCache = true
			continue
		}
		count := r.countCandidates(ctx, category)
		setPruneCount(result, category, count)
		if opts.DryRun {
			continue
		}
		if err := r.runPrune(ctx, category); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func optsEnabled(opts PruneOptions, category string) bool {
	switch category {
	case "containers":
		return opts.Containers
	case "images":
		return opts.Images
	case "volumes":
		return opts.Volumes
	case "networks":
		return opts.Networks
	case "builder":
		return opts.BuildCache
	}
	return false
}

func setPruneCount(result *PruneResult, category string, count int) {
	switch category {
	case "containers":
		result.Containers = count
	case "images":
		result.Images = count
	case "volumes":
		result.Volumes = count
	case "networks":
		result.Networks = count
	}
}

func (r *dockerRuntime) countCandidates(ctx context.Context, category string) int {
	args := buildPruneListArgs(category)
	if args == nil {
		return 0
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[runtime] count %s candidates: %v", category, err)
		return 0
	}
	if category == "containers" {
		return countPrunableContainers(string(out))
	}
	return countLines(string(out))
}

func (r *dockerRuntime) runPrune(ctx context.Context, category string) error {
	args := buildPruneArgs(category)
	if args == nil {
		return fmt.Errorf("unknown prune category %q", category)
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s prune: %w\n%s", category, err, string(out))
	}
	return nil
}
```

(`context`, `fmt`, `log`, and `os/exec` are already imported in `cleanup.go`.)

- [ ] **Step 4: Run the full runtime test suite to verify nothing regressed**

Run: `go test ./internal/runtime/... -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement label-protected docker prune exec"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go` — `cleanupCmd`, `resolveCleanupOptions`, `formatPruneSummary`, `init()` flag registration
- Modify: `internal/cli/root.go:40-75` — add `rootCmd.AddCommand(cleanupCmd)` in `init()`
- Test: `internal/cli/cleanup_test.go` — `TestResolveCleanupOptions`, `TestFormatPruneSummary`, `TestCleanupCommandRegistered`, `TestCleanupNoCategoryErrors`, `TestCleanupFlagsParsed`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.Manager.Prune` (Task 1)
- Produces:
  - `cleanupCmd` (`*cobra.Command`) — registered subcommand `cleanup`, flags: `--all`, `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--dry-run`
  - `resolveCleanupOptions(containers, images, volumes, networks, buildCache, dryRun, all bool) (runtime.PruneOptions, error)`
  - `formatPruneSummary(r *runtime.PruneResult) string`
  - `plur(n int, singular, plural string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestResolveCleanupOptions(t *testing.T) {
	tests := []struct {
		name       string
		containers bool
		images     bool
		volumes    bool
		networks   bool
		buildCache bool
		dryRun     bool
		all        bool
		want       runtime.PruneOptions
		wantErr    bool
	}{
		{
			name:    "no category flag errors",
			wantErr: true,
		},
		{
			name: "all enables every category",
			all:  true,
			want: runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true},
		},
		{
			name:       "single category",
			containers: true,
			want:       runtime.PruneOptions{Containers: true},
		},
		{
			name:       "dry run passes through",
			images:     true,
			dryRun:     true,
			want:       runtime.PruneOptions{Images: true, DryRun: true},
		},
		{
			name:       "all with dry run",
			all:        true,
			dryRun:     true,
			want:       runtime.PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true, DryRun: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCleanupOptions(tt.containers, tt.images, tt.volumes, tt.networks, tt.buildCache, tt.dryRun, tt.all)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveCleanupOptions() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFormatPruneSummary(t *testing.T) {
	tests := []struct {
		name string
		res  *runtime.PruneResult
		want string
	}{
		{
			name: "nothing to clean",
			res:  &runtime.PruneResult{},
			want: "Nothing to clean up.\n",
		},
		{
			name: "dry run nothing to clean",
			res:  &runtime.PruneResult{DryRun: true},
			want: "Nothing to clean up.\n",
		},
		{
			name: "mixed categories",
			res:  &runtime.PruneResult{Containers: 3, Images: 2, Volumes: 1, Networks: 1},
			want: "Pruned: 3 containers, 2 dangling images, 1 volume, 1 network.\n",
		},
		{
			name: "dry run mixed with build cache",
			res:  &runtime.PruneResult{DryRun: true, Containers: 1, Networks: 2, BuildCache: true},
			want: "Dry run: nothing was deleted.\nWould prune: 1 container, 2 networks, build cache.\n",
		},
		{
			name: "build cache only",
			res:  &runtime.PruneResult{BuildCache: true},
			want: "Pruned: build cache.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatPruneSummary(tt.res); got != tt.want {
				t.Errorf("formatPruneSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"all", "containers", "images", "volumes", "networks", "build-cache", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupNoCategoryErrorsBeforeDocker(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no category flag provided")
	}
	if !strings.Contains(err.Error(), "specify at least one") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCleanupFlagsParsed(t *testing.T) {
	var got runtime.PruneOptions
	var errGot error

	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		got, errGot = resolveCleanupOptions(containers, images, volumes, networks, buildCache, dryRun, all)
		return errGot
	}

	rootCmd.SetArgs([]string{"cleanup", "--images", "--volumes", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := runtime.PruneOptions{Images: true, Volumes: true, DryRun: true}
	if got != want {
		t.Errorf("parsed options = %+v, want %+v", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestResolveCleanupOptions|TestFormatPruneSummary|TestCleanupCommandRegistered|TestCleanupNoCategoryErrorsBeforeDocker|TestCleanupFlagsParsed" -v -count=1`

Expected: FAIL with `resolveCleanupOptions undefined`, `cleanupCmd undefined`

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
	Short: "Remove unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Remove unused Docker resources while protecting Tengiz-managed containers.

Stopped Tengiz containers (scale-to-zero idle apps) carry the tengiz-app label and are
never removed. Deployed images referenced by containers are kept; only dangling build
intermediates are pruned, so rollback images are preserved.

Use category flags to target specific resources, or --all to clean everything. Use
--dry-run to preview what would be removed without deleting anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts, err := resolveCleanupOptions(containers, images, volumes, networks, buildCache, dryRun, all)
		if err != nil {
			return err
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Print(formatPruneSummary(result))
		return nil
	},
}

func resolveCleanupOptions(containers, images, volumes, networks, buildCache, dryRun, all bool) (runtime.PruneOptions, error) {
	if all {
		return runtime.PruneOptions{
			Containers: true,
			Images:     true,
			Volumes:    true,
			Networks:   true,
			BuildCache: true,
			DryRun:     dryRun,
		}, nil
	}
	if !containers && !images && !volumes && !networks && !buildCache {
		return runtime.PruneOptions{}, fmt.Errorf("specify at least one of --containers, --images, --volumes, --networks, --build-cache, or --all")
	}
	return runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		DryRun:     dryRun,
	}, nil
}

func formatPruneSummary(r *runtime.PruneResult) string {
	var parts []string
	add := func(n int, singular, plural string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, plur(n, singular, plural)))
		}
	}
	add(r.Containers, "container", "containers")
	add(r.Images, "dangling image", "dangling images")
	add(r.Volumes, "volume", "volumes")
	add(r.Networks, "network", "networks")
	if r.BuildCache {
		parts = append(parts, "build cache")
	}
	if len(parts) == 0 {
		return "Nothing to clean up.\n"
	}
	if r.DryRun {
		return "Dry run: nothing was deleted.\nWould prune: " + strings.Join(parts, ", ") + ".\n"
	}
	return "Pruned: " + strings.Join(parts, ", ") + ".\n"
}

func plur(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func init() {
	cleanupCmd.Flags().Bool("all", false, "prune all resource categories")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped, unused containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused anonymous volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without deleting anything")
}
```

- [ ] **Step 4: Register the command in `internal/cli/root.go`**

In `init()` inside `root.go`, after the line `rootCmd.AddCommand(rollbackCmd)` (line 65), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -count=1`

Expected: PASS (all new cleanup tests plus existing CLI tests; the `Prune` mock addition from Task 1 keeps `TestMockRTForDeployImplementsManager` green)

- [ ] **Step 6: Build the binary and smoke-check help output**

Run: `go build -o /tmp/tengiz .`

Then: `/tmp/tengiz cleanup --help`

Expected: usage text listing `--all`, `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--dry-run`. Then:

Run: `/tmp/tengiz cleanup`

Expected: exit code 1 with the error `specify at least one of --containers, --images, --volumes, --networks, --build-cache, or --all`

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command for label-protected housekeeping"
```

---

### Task 5: Documentation and feature-status update

**Files:**
- Modify: `README.md` — insert `### tengiz cleanup` section after the `tengiz rollback` section (after line 237, before `### tengiz domain` at line 238)
- Modify: `AGENTS.md` — add a `tengiz cleanup` line to the CLI block
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented in the P0 table (line 19)
- Test: none needed beyond the existing suites

**Interfaces:**
- Consumes: the command surface produced in Task 4 (`tengiz cleanup` and its flags)

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

Insert after the `tengiz rollback` block (after line 237):

```markdown
### `tengiz cleanup [--all|--containers|--images|--volumes|--networks|--build-cache] [--dry-run]`

Remove unused Docker resources to reclaim disk space on the host.

| Flag | Description |
|------|-------------|
| `--all` | Prune all resource categories |
| `--containers` | Prune stopped, unused containers |
| `--images` | Prune dangling (untagged) images |
| `--volumes` | Prune unused anonymous volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune the Docker build cache |
| `--dry-run` | Show what would be removed without deleting anything |

Cleaning is **label-protected**: everything is filtered with a single
`label!=tengiz-app` filter, so stopped Tengiz containers (scale-to-zero idle
apps), deployed images, and their data are never touched. Image pruning is
dangling-only — tagged images used for rollback are preserved. At least one
category flag or `--all` is required.

```bash
tengiz cleanup --dry-run --all     # preview before deleting
tengiz cleanup --containers --images
```

- [ ] **Step 2: Add the command to `AGENTS.md`**

In the CLI block, after the `tengiz rollback <app>` line, add:

```
tengiz cleanup [--all|--containers|--images|--volumes|--networks|--build-cache] [--dry-run] → remove unused Docker resources (label-protected, preserves scale-to-zero apps)
```

Note: preserve the existing indentation (4 spaces) used by the other lines in that block.

- [ ] **Step 3: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table, change the row (currently line 19):

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 4: Run the full test suite and vet**

Run: `go test ./... -count=1`

Expected: PASS

Run: `go vet ./...`

Expected: no output (clean)

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage.**

- "Label-based `docker system prune`" → Task 2/Task 3 use `label!=tengiz-app` on every prune command; containers carrying `tengiz-app` are never removed.
- "`tengiz cleanup`" → Task 4 adds the command with `--all` and per-category flags.
- "Tengiz yönetimindeki container'lar korunur" (protected containers) → the single filter protects stopped scale-to-zero containers; Task 1/2/3 implement and TestCountPrunableContainers asserts the tengiz-labeled stopped container is excluded.
- Coolify-style periodic `DockerCleanupJob` → periodic scheduling is explicitly OUT of scope for this deliverable; `tengiz cleanup` is the manual/CI-ready primitive (can be dropped into cron or the future background scheduler). The plan does not silently add a daemon — matches the unevaluated-effort P0 scope (Effort Düşük).
- "kullanılmayan volume, network, container ve image'leri temizleme" → per-category prunes cover containers, images (dangling), volumes (unused anonymous), networks (unused). Build cache added as a natural companion.
- Granular flags (#56) deliberately NOT extended with `until=`/`dangling=` toggles — YAGNI; the plan keeps one label filter per command per Global Constraints.

**2. Placeholder scan:** No "TBD"/"implement later" steps. Every code step contains full compilable code. No "Similar to Task N" references — each task repeats the exact code it needs. All helper names referenced in later tasks (`buildPruneArgs`, `countPrunableContainers`, `resolveCleanupOptions`, `formatPruneSummary`, `PruneOptions`, `PruneResult`, `Manager.Prune`) are defined in earlier tasks.

**3. Type consistency:** `Prune(ctx context.Context, opts PruneOptions) (*PruneResult, error)` is identical across the interface (Task 1), stub (Task 1), mock (Task 1), dockerRuntime (Task 3), and CLI call sites (Task 4). Category identifiers are consistently `containers/images/volumes/networks/builder` across `buildPruneArgs`, `buildPruneListArgs`, `optsEnabled`, and `setPruneCount`. Summary labels ("dangling images" pluralization via `plur`) match the tested output strings byte-for-byte.

**4. Documentation requirement:** AGENTS.md mandates updating README/docs for UI/UX changes, adding tests, and committing — covered by Task 4 (tests) and Task 5 (README.md, AGENTS.md, FUTURES_FEATURES.md status flip).