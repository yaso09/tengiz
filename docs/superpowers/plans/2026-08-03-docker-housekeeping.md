# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning stopped non-Tengiz containers, dangling images, unused volumes/networks, and build cache — with label-based protection so Tengiz-managed resources are never removed.

**Architecture:** A single `Prune` method on the existing `runtime.Manager` interface (exec-based `dockerRuntime` implementation + `stubManager` test mock) accepts a `PruneOptions` struct with one bool per category plus a `DryRun` flag. A pure, table-tested helper `buildPruneArgs(kind, dryRun)` produces the exact `docker` argument slices, mirroring the existing `buildLogArgs`/`buildRunArgs` pattern. Containers are filtered with `label!=tengiz-app` so Tengiz's stopped scale-to-zero containers are untouched; images are pruned dangling-only so versioned `tengiz-apps/*` images (needed for rollback) survive. The CLI command defaults to all categories when no category flag is given and prints a per-category report.

**Tech Stack:** Go 1.26, Cobra (existing), `os/exec` Docker CLI (existing), `runtime.Manager` interface (existing), no new dependencies.

## Global Constraints

- No new external dependencies; only the Go stdlib and existing Cobra
- All Docker interaction stays exec-based via `docker` CLI (`os/exec`), never the Docker SDK
- Tengiz-managed containers are ALWAYS protected via the `tengiz-app` label (`--filter label!=tengiz-app` on container prune/list)
- Image pruning is dangling-only (`docker image prune -f`, no `-a`) — `tengiz-apps/*` versioned images must survive for rollback
- The `runtime.Manager` interface gets exactly one new method: `Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`
- Every mock implementing `runtime.Manager` must gain the `Prune` method or the repo will not compile: `stubManager`, `mockRTForDeploy` (root_test.go), `mockRuntime` (proxy_test.go), `mockRuntime` (idle_test.go)
- `tengiz cleanup` takes no positional args; `--env` is intentionally NOT added (cleanup is daemon-wide, not per-environment)
- Existing tests must continue to pass without modification
- `go vet ./...` must be clean

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Add `PruneOptions`, `PruneReport`, `buildPruneArgs()` pure helper, `dockerRuntime.Prune()` implementation |
| `internal/runtime/runtime.go` | Add `Prune` to `Manager` interface (line 36 area) + `stubManager.Prune` (line 119 area) |
| `internal/runtime/cleanup_test.go` | Table tests for `buildPruneArgs`, stub `Prune` test |
| `internal/cli/cleanup.go` | NEW file: `cleanupCmd`, `addCleanupFlags`, `pruneOptionsFromFlags`, `printPruneReport` |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` (line 67 area) |
| `internal/cli/cleanup_test.go` | NEW file: registration, flag presence, options-defaulting tests |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` (after line 99) |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` (after line 34) |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` (after line 33) |
| `README.md` | Document `tengiz cleanup` under CLI Reference (after `tengiz rm`, line 228) |

---

### Task 1: Runtime prune capability (`PruneOptions`, `PruneReport`, `buildPruneArgs`, interface + impls)

**Files:**
- Modify: `internal/runtime/cleanup.go` (append new types + methods)
- Modify: `internal/runtime/runtime.go:36` (interface), `runtime.go:119` (stub)
- Modify: `internal/runtime/cleanup_test.go` (append tests)
- Modify: `internal/cli/root_test.go:99` (mock)
- Modify: `internal/proxy/proxy_test.go:34` (mock)
- Modify: `internal/idle/idle_test.go:33` (mock)

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.buildPruneArgs(kind string, dryRun bool) []string`, `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		dryRun   bool
		expected []string
	}{
		{
			name:     "containers prune",
			kind:     "containers",
			expected: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			name:     "containers dry-run",
			kind:     "containers",
			dryRun:   true,
			expected: []string{"container", "ls", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.ID}} {{.Names}} {{.Status}}"},
		},
		{
			name:     "images prune",
			kind:     "images",
			expected: []string{"image", "prune", "-f"},
		},
		{
			name:     "images dry-run",
			kind:     "images",
			dryRun:   true,
			expected: []string{"image", "ls", "--filter", "dangling=true", "--format", "{{.ID}} {{.Repository}}:{{.Tag}}"},
		},
		{
			name:     "volumes prune",
			kind:     "volumes",
			expected: []string{"volume", "prune", "-f"},
		},
		{
			name:     "volumes dry-run",
			kind:     "volumes",
			dryRun:   true,
			expected: []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"},
		},
		{
			name:     "networks prune",
			kind:     "networks",
			expected: []string{"network", "prune", "-f"},
		},
		{
			name:     "networks dry-run",
			kind:     "networks",
			dryRun:   true,
			expected: []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.ID}} {{.Name}}"},
		},
		{
			name:     "build-cache prune",
			kind:     "build-cache",
			expected: []string{"builder", "prune", "-af"},
		},
		{
			name:     "build-cache dry-run",
			kind:     "build-cache",
			dryRun:   true,
			expected: []string{"builder", "du"},
		},
		{
			name:     "unknown kind",
			kind:     "bogus",
			expected: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneArgs(tt.kind, tt.dryRun)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildPruneArgs(%q, %v) = %v (len %d), want %v (len %d)", tt.kind, tt.dryRun, got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("buildPruneArgs(%q, %v)[%d] = %q, want %q", tt.kind, tt.dryRun, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true, DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.Containers != "" || report.Images != "" || report.Volumes != "" {
		t.Errorf("stub Prune report should be empty, got %+v", report)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestStubPrune" -v -count=1`

Expected: FAIL with `undefined: buildPruneArgs` and `undefined: PruneOptions` (test file does not compile).

- [ ] **Step 3: Add prune types + `buildPruneArgs` to `internal/runtime/cleanup.go`**

Append to the end of the file:

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type PruneReport struct {
	Containers string
	Images     string
	Volumes    string
	Networks   string
	BuildCache string
}

func buildPruneArgs(kind string, dryRun bool) []string {
	switch kind {
	case "containers":
		if dryRun {
			return []string{"container", "ls", "-a",
				"--filter", "status=exited",
				"--filter", "label!=tengiz-app",
				"--format", "{{.ID}} {{.Names}} {{.Status}}"}
		}
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "images":
		if dryRun {
			return []string{"image", "ls", "--filter", "dangling=true", "--format", "{{.ID}} {{.Repository}}:{{.Tag}}"}
		}
		return []string{"image", "prune", "-f"}
	case "volumes":
		if dryRun {
			return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
		}
		return []string{"volume", "prune", "-f"}
	case "networks":
		if dryRun {
			return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.ID}} {{.Name}}"}
		}
		return []string{"network", "prune", "-f"}
	case "build-cache":
		if dryRun {
			return []string{"builder", "du"}
		}
		return []string{"builder", "prune", "-af"}
	}
	return nil
}
```

- [ ] **Step 4: Add `Prune` to `Manager` interface in `internal/runtime/runtime.go`**

In the `Manager` interface, directly after the `KeepLastNImages` line (currently line 36):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

- [ ] **Step 5: Implement `dockerRuntime.Prune` in `internal/runtime/cleanup.go`**

Append to the end of the file:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport
	var errs []error
	run := func(kind string, enabled bool, dst *string) {
		if !enabled {
			return
		}
		args := buildPruneArgs(kind, opts.DryRun)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			errs = append(errs, fmt.Errorf("docker %s prune: %w\n%s", kind, err, string(out)))
			return
		}
		*dst = string(out)
	}
	run("containers", opts.Containers, &report.Containers)
	run("images", opts.Images, &report.Images)
	run("volumes", opts.Volumes, &report.Volumes)
	run("networks", opts.Networks, &report.Networks)
	run("build-cache", opts.BuildCache, &report.BuildCache)
	if len(errs) > 0 {
		return report, fmt.Errorf("cleanup: %v", errs)
	}
	return report, nil
}
```

- [ ] **Step 6: Implement `stubManager.Prune` in `internal/runtime/runtime.go`**

Directly after the `stubManager.KeepLastNImages` method (currently line 117-119):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}
```

- [ ] **Step 7: Update the three mock implementations of `runtime.Manager`**

Add this exact method to each mock (they will not compile until all are updated):

`internal/cli/root_test.go` — inside `mockRTForDeploy` after the `KeepLastNImages` method (after line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

`internal/proxy/proxy_test.go` — inside `mockRuntime` after its `KeepLastNImages` method (after line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

`internal/idle/idle_test.go` — inside `mockRuntime` after its `KeepLastNImages` method (after line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

Check that each mock's `Prune` signature matches the interface exactly (`runtime.PruneOptions` / `runtime.PruneReport`). If a mock imports the runtime package as a blank-import alias, confirm the import path is `github.com/yaso09/tengiz/internal/runtime`.

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestStubPrune" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds (all mocks updated, interface satisfied).

- [ ] **Step 9: Run full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS. (proxy tests are slow, ~2s each; idle tests are time-sensitive — both pre-existing.)

- [ ] **Step 10: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add Prune method and prune types to runtime manager"
```

---

### Task 2: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go` init() (add registration after line 67 `rootCmd.AddCommand(runCmd)`)

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.Manager.Prune(ctx, opts) (PruneReport, error)` from Task 1
- Produces: `tengiz cleanup` command with `--containers/--images/--volumes/--networks/--build-cache/--dry-run` flags; `cli.pruneOptionsFromFlags(cmd *cobra.Command) runtime.PruneOptions`; `cli.printPruneReport(report runtime.PruneReport, dryRun bool)`

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

func TestCleanupCommandHasFlags(t *testing.T) {
	expected := []string{"containers", "images", "volumes", "networks", "build-cache", "dry-run"}
	for _, name := range expected {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup command missing --%s flag", name)
		}
	}
}

func TestPruneOptionsDefaultsToAll(t *testing.T) {
	cmd := &cobra.Command{}
	addCleanupFlags(cmd)
	opts := pruneOptionsFromFlags(cmd)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories default true, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("dry-run should default false")
	}
}

func TestPruneOptionsSelectedCategories(t *testing.T) {
	cmd := &cobra.Command{}
	addCleanupFlags(cmd)
	if err := cmd.Flags().Set("images", "true"); err != nil {
		t.Fatal(err)
	}
	opts := pruneOptionsFromFlags(cmd)
	if !opts.Images {
		t.Error("images should be enabled")
	}
	if opts.Containers || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Errorf("expected only images enabled, got %+v", opts)
	}
}

func TestPruneOptionsDryRun(t *testing.T) {
	cmd := &cobra.Command{}
	addCleanupFlags(cmd)
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	opts := pruneOptionsFromFlags(cmd)
	if !opts.DryRun {
		t.Error("dry-run should be true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup|TestPruneOptions" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: addCleanupFlags`, `undefined: pruneOptionsFromFlags`.

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
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Remove unused Docker resources (stopped non-Tengiz containers, dangling
images, unused volumes/networks, and build cache) to reclaim disk space.

Tengiz-managed containers are protected via the tengiz-app label and are
never pruned. Versioned tengiz-apps/* images are kept for rollback.

With no category flags, all categories are cleaned. Use --dry-run to
preview what would be removed without removing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := pruneOptionsFromFlags(cmd)

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}

		report, err := rt.Prune(cmd.Context(), opts)
		printPruneReport(report, opts.DryRun)
		return err
	},
}

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cmd.Flags().Bool("images", false, "remove dangling images")
	cmd.Flags().Bool("volumes", false, "remove unused volumes")
	cmd.Flags().Bool("networks", false, "remove unused networks")
	cmd.Flags().Bool("build-cache", false, "remove Docker build cache")
	cmd.Flags().Bool("dry-run", false, "preview what would be removed without removing anything")
}

func pruneOptionsFromFlags(cmd *cobra.Command) runtime.PruneOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	any := containers || images || volumes || networks || buildCache
	if !any {
		containers, images, volumes, networks, buildCache = true, true, true, true, true
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

func printPruneReport(report runtime.PruneReport, dryRun bool) {
	verb := "removed"
	if dryRun {
		verb = "would be removed"
	}
	sections := []struct{ name, out string }{
		{"containers", report.Containers},
		{"images", report.Images},
		{"volumes", report.Volumes},
		{"networks", report.Networks},
		{"build-cache", report.BuildCache},
	}
	printed := false
	for _, s := range sections {
		if strings.TrimSpace(s.out) == "" {
			continue
		}
		if !printed {
			fmt.Printf("[tengiz] resources that %s:\n", verb)
			printed = true
		}
		fmt.Printf("  [%s] %s\n", s.name, strings.TrimSuffix(s.out, "\n"))
	}
	if !printed {
		fmt.Println("[tengiz] nothing to clean")
	}
}
```

- [ ] **Step 4: Register `cleanupCmd` in `internal/cli/root.go`**

In `init()`, directly after `rootCmd.AddCommand(runCmd)` (currently line 67), add exactly these two lines:

```go
	addCleanupFlags(cleanupCmd)
	rootCmd.AddCommand(cleanupCmd)
```

`addCleanupFlags` defines all six flags (`--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--dry-run`), so no flag is registered here directly. Do not add a separate `--dry-run` line.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestPruneOptions" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds.

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 3: README documentation + final verification

**Files:**
- Modify: `README.md` (insert `tengiz cleanup` section after the `tengiz rm <app>` section, i.e. after line 228)

**Interfaces:**
- Consumes: the final `tengiz cleanup` CLI surface from Task 2
- Produces: user-facing documentation matching the implemented flags

- [ ] **Step 1: Add the `tengiz cleanup` section to README.md**

Insert the following block directly after the `tengiz rm <app>` section (after line 228, before `### tengiz rollback <app>`):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling images |
| `--volumes` | Remove unused volumes |
| `--networks` | Remove unused networks |
| `--build-cache` | Remove Docker build cache |
| `--dry-run` | Preview what would be removed without removing anything |

With no category flags, all categories are cleaned. Tengiz-managed containers (labeled `tengiz-app`) and versioned `tengiz-apps/*` images are always protected.
```

- [ ] **Step 2: Run full verification**

Run: `go test ./... -v -count=1`

Expected: All PASS.

Run: `go vet ./...`

Expected: No issues.

Run: `go build -o tengiz .`

Expected: Binary builds successfully.

- [ ] **Step 3: Manual smoke test (requires Docker daemon)**

Run: `./tengiz cleanup --dry-run`

Expected: Prints `[tengiz] resources that would be removed:` (or `[tengiz] nothing to clean` if the host is already clean). Nothing is actually deleted.

Run: `./tengiz cleanup`

Expected: Runs all five prune commands, prints per-category `[tengiz] resources that removed:` output.

Run: `./tengiz cleanup --images --dry-run`

Expected: Only the `[images]` section appears; `--dry-run` prevents deletion.

- [ ] **Step 4: Self-review against spec**

Check against `docs/FUTURES_FEATURES.md` #6:
- `tengiz cleanup` command ✅ (Task 2)
- Label-based filtering protects Tengiz containers ✅ (`--filter label!=tengiz-app` in `buildPruneArgs`)
- Prunes containers, images, volumes, networks ✅ (four of five categories)
- Build cache pruning ✅ (fifth category, from Coolify's `CleanupHelperContainersJob` counterpart)
- Default "prune everything" UX ✅ (no category flags = all categories)
- Rollback safety preserved ✅ (dangling-only image prune, versioned images untouched)

- [ ] **Step 5: Placeholder scan**

Search the changed files for `TBD`, `TODO`, `implement later`, `fill in details`, `Similar to Task`. Run:

```bash
grep -rn "TBD\|TODO\|implement later\|fill in details\|Similar to Task" internal/runtime/cleanup.go internal/runtime/runtime.go internal/cli/cleanup.go internal/cli/cleanup_test.go || echo "clean"
```

Expected: Outputs `clean` (no matches).

- [ ] **Step 6: Type consistency check**

Verify these names match exactly across all files:
- `PruneOptions` (struct with `Containers, Images, Volumes, Networks, BuildCache, DryRun bool`) — defined in `internal/runtime/cleanup.go`, used in `runtime.go` interface/stub, `cli/cleanup.go`, `cli/cleanup_test.go`
- `PruneReport` (struct with `Containers, Images, Volumes, Networks, BuildCache string`) — same usage sites
- `buildPruneArgs(kind string, dryRun bool) []string` — lowercase, package-private; used only in `cleanup.go`, tested in `cleanup_test.go`
- `Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)` — identical signature on `dockerRuntime`, `stubManager`, and all three mocks
- `pruneOptionsFromFlags(cmd *cobra.Command) runtime.PruneOptions` and `printPruneReport(report runtime.PruneReport, dryRun bool)` — package-private in `cli`, used only in `cleanup.go` + `cleanup_test.go`

- [ ] **Step 7: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage:** FUTURES_FEATURES.md #6 Docker Housekeeping is fully covered: `tengiz cleanup` command (Task 2), label-based container protection + dangling image/volume/network prune (Task 1), build cache prune (Task 1), README (Task 3). No spec gaps.

**2. Placeholder scan:** Every code step contains complete, compilable code. No `TBD`/`TODO`/"add error handling"/"write tests" placeholders.

**3. Type consistency:** `PruneOptions`, `PruneReport`, `buildPruneArgs`, `Prune` interface method, `pruneOptionsFromFlags`, and `printPruneReport` are named identically in every task. The `Prune` signature is identical on the interface, both implementations, and all three test mocks.

**Potential pitfalls called out inline:** (a) all four `runtime.Manager` implementations must gain `Prune` in the same commit or `go build ./...` breaks; (b) do not duplicate the `--dry-run` flag registration in Task 2 Step 4 — rely solely on `addCleanupFlags`; (c) `tengiz cleanup` deliberately has no `--env` flag — cleanup is daemon-wide.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-03-docker-housekeeping.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
