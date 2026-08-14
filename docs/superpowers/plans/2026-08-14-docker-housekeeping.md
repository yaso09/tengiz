# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes stopped containers, dangling images, unused volumes/networks and build cache — always protecting Tengiz-managed containers via labels — plus a lightweight auto-cleanup after every successful deploy.

**Architecture:** A new `runtime.Manager.Cleanup(ctx, opts)` method runs `docker ... prune` per category. Category docker args are produced by a pure, unit-testable function `cleanupArgs(category)` so the pruning strategy (label filters, dangling-only images) is locked in one place. The CLI command `tengiz cleanup` maps flags to `CleanupOptions` with safe defaults (containers + images + build-cache; volumes/networks opt-in; `--dry-run` prints commands without executing). A `runtime.AutoCleanup` helper (images + build-cache only, always safe) is invoked after successful deploys in both the `deploy` command and the git webhook pipeline.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` interface, `docker` CLI via `os/exec` (BuildKit `docker builder prune` requires Docker 19.03+). No new external dependencies.

## Global Constraints

- Tengiz-managed containers are identified by label `tengiz-app` — `docker container prune` MUST use `--filter "label!=tengiz-app"` so scale-to-zero stopped apps are never deleted
- Containers with `tengiz-app` label are `--label tengiz-app=<name>` (see `internal/runtime/docker.go:76`, `:98`, `:125`, `:456`)
- Image pruning is DANGLING-ONLY (`docker image prune -f` without `-a`) — never prune all unused images, which would remove `tengiz-apps/<name>:*` rollback images
- Volumes and networks are OPT-IN flags (`--volumes`, `--networks`); they are never pruned by default
- `--env` flag is NOT used by `tengiz cleanup`: cleanup protects all Tengiz containers across every environment via the label
- No new external Go dependencies
- Existing tests must continue to pass without modification (except the mock in `internal/cli/root_test.go`, which must gain the new interface method)
- Every `docker ... prune` execution uses `exec.CommandContext` with a 5-minute timeout at the CLI level
- Feature branch required: `feat/docker-housekeeping`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupCategoryResult`, `CleanupResult` types; add `Cleanup` to `Manager` interface; stub implementation |
| `internal/runtime/cleanup.go` | Docker implementation: `cleanupArgs(category)`, `extractReclaimedSpace()`, `dockerRuntime.Cleanup`, `dockerRuntime.cleanupCategory`, `AutoCleanup(ctx, rt)` helper |
| `internal/runtime/cleanup_test.go` | Tests for stub `Cleanup`, `cleanupArgs`, `extractReclaimedSpace`, `AutoCleanup` |
| `internal/cli/cleanup.go` | **New file.** `cleanupCmd` command, `cleanupOptionsFromFlags(cmd)`, `printCleanupResult(result, dryRun)` |
| `internal/cli/root.go` | Register `cleanupCmd` + its flags in `init()`; call `runtime.AutoCleanup` after both deploy paths' `KeepLastNImages` |
| `internal/cli/root_test.go` | Add `Cleanup` method + recorder fields to `mockRTForDeploy`; CLI command/flag/default tests; `AutoCleanup` options test |
| `internal/gitdeploy/deployer.go` | Call `runtime.AutoCleanup` after both `KeepLastNImages` call sites |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

No changes to `internal/config`, `internal/builder`, `internal/proxy`, `internal/types`.

---

### Task 0: Create the feature branch

**Files:**
- (none — git only)

- [ ] **Step 1: Create branch**

```bash
git checkout -b feat/docker-housekeeping
```

Expected: branch created and checked out.

---

### Task 1: Runtime Cleanup API (`runtime.Manager.Cleanup`)

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface; add stub method after line 121
- Modify: `internal/runtime/cleanup.go` — add types, constants, `cleanupArgs`, `extractReclaimedSpace`, docker methods
- Modify: `internal/runtime/cleanup_test.go` — new tests
- Modify: `internal/cli/root_test.go:69-100` — add `Cleanup` method + recorder fields to `mockRTForDeploy`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `runtime.CleanupOptions{DryRun, Containers, Images, Volumes, Networks, BuildCache bool}`
  - `runtime.CleanupCategoryResult{Category, Command, Reclaimed, Err string}`
  - `runtime.CleanupResult{Categories []CleanupCategoryResult}`
  - constants `runtime.CleanupContainers`, `runtime.CleanupImages`, `runtime.CleanupVolumes`, `runtime.CleanupNetworks`, `runtime.CleanupBuildCache` (all type `string`)
  - `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`
  - `runtime.AutoCleanup(ctx context.Context, rt Manager)` (added in Task 3; Task 1 delivers the type surface)

- [ ] **Step 1: Write the failing tests**

Modify `internal/runtime/cleanup_test.go`: the file already declares `package runtime` and imports `context` and `testing` — add `"slices"` to that import block:

```go
import (
	"context"
	"slices"
	"testing"
)
```

Then append the test functions:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Images: true})
	if err != nil {
		t.Fatalf("stub Cleanup() error = %v", err)
	}
	if len(res.Categories) != 0 {
		t.Errorf("stub Cleanup() Categories = %v, want empty", res.Categories)
	}
}

func TestCleanupArgs(t *testing.T) {
	tests := []struct {
		category    string
		wantPresent []string
		wantAbsent  []string
	}{
		{
			category:    CleanupContainers,
			wantPresent: []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
			wantAbsent:  []string{"label=tengiz-app"},
		},
		{
			category:    CleanupImages,
			wantPresent: []string{"image", "prune", "-f"},
			wantAbsent:  []string{"-a"},
		},
		{
			category:    CleanupVolumes,
			wantPresent: []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			category:    CleanupNetworks,
			wantPresent: []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
		{
			category:    CleanupBuildCache,
			wantPresent: []string{"builder", "prune", "-f"},
		},
	}
	for _, tc := range tests {
		args := cleanupArgs(tc.category)
		for _, want := range tc.wantPresent {
			if !slices.Contains(args, want) {
				t.Errorf("cleanupArgs(%q) = %v, missing %q", tc.category, args, want)
			}
		}
		for _, absent := range tc.wantAbsent {
			if slices.Contains(args, absent) {
				t.Errorf("cleanupArgs(%q) = %v, must not contain %q", tc.category, args, absent)
			}
		}
	}
	if args := cleanupArgs("bogus"); args != nil {
		t.Errorf("cleanupArgs(\"bogus\") = %v, want nil", args)
	}
}

func TestExtractReclaimedSpace(t *testing.T) {
	output := "Deleted Images:\nuntagged: tengiz-apps/x@sha256:abc\n\nTotal reclaimed space: 5.123 MB\n"
	got := extractReclaimedSpace(output)
	if got != "Total reclaimed space: 5.123 MB" {
		t.Errorf("extractReclaimedSpace() = %q, want %q", got, "Total reclaimed space: 5.123 MB")
	}
	if got := extractReclaimedSpace("nothing to delete"); got != "" {
		t.Errorf("extractReclaimedSpace() = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestCleanupArgs|TestExtractReclaimedSpace" -v -count=1`

Expected: compile error — `undefined: CleanupOptions`, `undefined: Cleanup`, `undefined: cleanupArgs`, `undefined: CleanupContainers`, `undefined: extractReclaimedSpace`

- [ ] **Step 3: Add the types and interface method**

In `internal/runtime/runtime.go`, add to the `Manager` interface (after `Run(...)` on line 48):

```go
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add the stub method after `func (m *stubManager) Run(...)` (line 121):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 4: Add the types and constants to `internal/runtime/cleanup.go`**

Add at the top of `internal/runtime/cleanup.go` (after the imports):

```go
type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type CleanupCategoryResult struct {
	Category  string
	Command   string
	Reclaimed string
	Err       string
}

type CleanupResult struct {
	Categories []CleanupCategoryResult
}

const (
	CleanupContainers = "containers"
	CleanupImages     = "images"
	CleanupVolumes    = "volumes"
	CleanupNetworks   = "networks"
	CleanupBuildCache = "build-cache"
)

func cleanupArgs(category string) []string {
	switch category {
	case CleanupContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case CleanupImages:
		return []string{"image", "prune", "-f"}
	case CleanupVolumes:
		return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	case CleanupNetworks:
		return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	case CleanupBuildCache:
		return []string{"builder", "prune", "-f"}
	default:
		return nil
	}
}

func extractReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return line
		}
	}
	return ""
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var categories []string
	if opts.Containers {
		categories = append(categories, CleanupContainers)
	}
	if opts.Images {
		categories = append(categories, CleanupImages)
	}
	if opts.Volumes {
		categories = append(categories, CleanupVolumes)
	}
	if opts.Networks {
		categories = append(categories, CleanupNetworks)
	}
	if opts.BuildCache {
		categories = append(categories, CleanupBuildCache)
	}

	result := CleanupResult{Categories: make([]CleanupCategoryResult, 0, len(categories))}
	for _, cat := range categories {
		result.Categories = append(result.Categories, r.cleanupCategory(ctx, cat, opts.DryRun))
	}
	return result, nil
}

func (r *dockerRuntime) cleanupCategory(ctx context.Context, category string, dryRun bool) CleanupCategoryResult {
	args := cleanupArgs(category)
	res := CleanupCategoryResult{
		Category: category,
		Command:  "docker " + strings.Join(args, " "),
	}
	if dryRun {
		return res
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		res.Err = fmt.Sprintf("docker %s prune: %v\n%s", category, err, string(out))
		return res
	}
	res.Reclaimed = extractReclaimedSpace(string(out))
	return res
}
```

`internal/runtime/cleanup.go` already imports `context`, `fmt`, `os/exec`, and `strings` — no import changes needed.

- [ ] **Step 5: Update `mockRTForDeploy` so `internal/cli` still compiles**

Adding `Cleanup` to the interface breaks compilation of `internal/cli/root_test.go`, which asserts `mockRTForDeploy` implements `runtime.Manager`. Update the struct definition in `internal/cli/root_test.go:69-74`:

```go
type mockRTForDeploy struct {
	created         atomic.Int32
	removed         atomic.Int32
	started         atomic.Int32
	stopped         atomic.Int32
	cleanupCalls    atomic.Int32
	lastCleanupOpts runtime.CleanupOptions
}
```

Add the method after `func (m *mockRTForDeploy) Run(...)` (line 100):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	m.cleanupCalls.Add(1)
	m.lastCleanupOpts = opts
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 6: Run the new runtime tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestCleanupArgs|TestExtractReclaimedSpace" -v -count=1`

Expected: all PASS (3 tests)

- [ ] **Step 7: Run the full test suite**

Run: `go vet ./... && go test ./... -count=1`

Expected: PASS everywhere (confirms the interface change did not break `internal/cli` or `internal/gitdeploy`)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup API for Docker housekeeping"
```

---

### Task 2: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:57` — add `rootCmd.AddCommand(cleanupCmd)` in `init()`
- Modify: `internal/cli/root.go:88` — register cleanup flags after the webhook flags in `init()`
- Modify: `internal/cli/root_test.go` — new command/flag/default tests

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.Manager.Cleanup` from Task 1
- Produces: `cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error)`, `printCleanupResult(result runtime.CleanupResult, dryRun bool)`, command `cleanupCmd`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks", "build-cache"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func TestCleanupOptionsDefaults(t *testing.T) {
	cmd := cleanupCmd
	cmd.ParseFlags([]string{})
	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Containers || !opts.Images || !opts.BuildCache {
		t.Errorf("defaults: expected containers+images+build-cache enabled, got %+v", opts)
	}
	if opts.Volumes || opts.Networks || opts.DryRun {
		t.Errorf("defaults: expected volumes/networks/dry-run disabled, got %+v", opts)
	}
}

func TestCleanupOptionsExplicit(t *testing.T) {
	cmd := cleanupCmd
	cmd.ParseFlags([]string{"--volumes", "--dry-run"})
	opts, err := cleanupOptionsFromFlags(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Volumes || !opts.DryRun {
		t.Errorf("expected volumes+dry-run enabled, got %+v", opts)
	}
	if opts.Containers || opts.Images || opts.BuildCache || opts.Networks {
		t.Errorf("expected containers/images/build-cache/networks disabled, got %+v", opts)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd`, `undefined: cleanupOptionsFromFlags` (the file `internal/cli/cleanup.go` does not exist yet)

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Removes Docker resources that are no longer needed. All Tengiz-managed
containers are protected via the "tengiz-app" label and are never touched.

Default (no category flags): stopped non-Tengiz containers, dangling images,
and the Docker build cache.

Categories:
  --containers   remove stopped containers without the tengiz-app label
  --images       remove dangling (untagged) images only — rollback images are never touched
  --volumes      remove unused Docker volumes (opt-in)
  --networks     remove unused Docker networks (opt-in)
  --build-cache  remove the Docker build cache

Use --dry-run to print the docker commands without executing them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
		defer cancel()

		result, err := rt.Cleanup(ctx, opts)
		if err != nil {
			return err
		}
		printCleanupResult(result, opts.DryRun)
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")

	if !containers && !images && !buildCache && !volumes && !networks {
		containers, images, buildCache = true, true, true
	}

	return runtime.CleanupOptions{
		DryRun:     dryRun,
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
	}, nil
}

func printCleanupResult(result runtime.CleanupResult, dryRun bool) {
	if len(result.Categories) == 0 {
		fmt.Println("[tengiz] nothing to clean")
		return
	}
	for _, c := range result.Categories {
		switch {
		case c.Err != "":
			fmt.Printf("[tengiz] %s: error: %s\n", c.Category, c.Err)
		case dryRun:
			fmt.Printf("[tengiz] %s: would run: %s\n", c.Category, c.Command)
		case c.Reclaimed != "":
			fmt.Printf("[tengiz] %s: %s\n", c.Category, c.Reclaimed)
		default:
			fmt.Printf("[tengiz] %s: nothing to remove\n", c.Category)
		}
	}
}
```

- [ ] **Step 4: Register the command and its flags in `internal/cli/root.go`**

In `init()`, after `rootCmd.AddCommand(webhookCmd)` (line 57) add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

At the end of `init()`, after the webhook flag registration (line 88) add:

```go
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be cleaned without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling (untagged) images")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused Docker volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused Docker networks")
	cleanupCmd.Flags().Bool("build-cache", false, "remove Docker build cache")
```

- [ ] **Step 5: Run the CLI tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS (3 tests)

- [ ] **Step 6: Run the full test suite**

Run: `go vet ./... && go test ./... -count=1`

Expected: PASS everywhere

- [ ] **Step 7: Manual smoke test**

Note: `tengiz cleanup` requires the `docker` CLI in PATH (it calls `runtime.NewDocker()`, which fails with `docker not found in PATH` otherwise). On a machine with the docker CLI installed:

Run: `go build -o /tmp/tengiz . && /tmp/tengiz cleanup --dry-run`

Expected: prints (no Docker daemon needed for dry-run — commands are printed, not executed):
```
[tengiz] containers: would run: docker container prune -f --filter label!=tengiz-app
[tengiz] images: would run: docker image prune -f
[tengiz] build-cache: would run: docker builder prune -f
```
(If a real Docker daemon is available, `tengiz cleanup --containers --images` runs the prunes and prints `Total reclaimed space: ...` lines.)

- [ ] **Step 8: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 3: Auto-cleanup after successful deploys

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `AutoCleanup(ctx, rt)` helper
- Modify: `internal/runtime/cleanup_test.go` — stub-based `AutoCleanup` test
- Modify: `internal/cli/root.go:346` and `:466` — call `runtime.AutoCleanup` after each `rt.KeepLastNImages(...)` in the deploy handler
- Modify: `internal/gitdeploy/deployer.go:215` and `:315` — call `runtime.AutoCleanup` after each `p.rt.KeepLastNImages(...)`
- Modify: `internal/cli/root_test.go` — `AutoCleanup` options test using `mockRTForDeploy`

**Interfaces:**
- Consumes: `runtime.Cleanup`, `runtime.CleanupOptions`, `runtime.CleanupResult` from Task 1
- Produces: `runtime.AutoCleanup(ctx context.Context, rt Manager)` — invoked in Task 4 wiring; safe to call on every deploy

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestAutoCleanupWithStub(t *testing.T) {
	AutoCleanup(context.Background(), NewStub())
}
```

Add to `internal/cli/root_test.go`:

```go
func TestAutoCleanupCallsRuntime(t *testing.T) {
	m := &mockRTForDeploy{}
	runtime.AutoCleanup(context.Background(), m)
	if m.cleanupCalls.Load() != 1 {
		t.Fatalf("AutoCleanup: expected 1 Cleanup call, got %d", m.cleanupCalls.Load())
	}
	opts := m.lastCleanupOpts
	if !opts.Images || !opts.BuildCache {
		t.Errorf("AutoCleanup options = %+v, want Images+BuildCache", opts)
	}
	if opts.Containers || opts.Volumes || opts.Networks || opts.DryRun {
		t.Errorf("AutoCleanup options = %+v, want Containers/Volumes/Networks/DryRun unset", opts)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run TestAutoCleanupWithStub -v -count=1`

Expected: FAIL — `undefined: AutoCleanup`

- [ ] **Step 3: Add `AutoCleanup` to `internal/runtime/cleanup.go`**

Add at the end of `internal/runtime/cleanup.go`:

```go
func AutoCleanup(ctx context.Context, rt Manager) {
	res, err := rt.Cleanup(ctx, CleanupOptions{Images: true, BuildCache: true})
	if err != nil {
		log.Printf("[tengiz] auto cleanup: %v", err)
		return
	}
	for _, c := range res.Categories {
		if c.Reclaimed != "" {
			log.Printf("[tengiz] auto cleanup: %s", c.Reclaimed)
		}
	}
}
```

- [ ] **Step 4: Wire auto-cleanup into the `deploy` command**

In `internal/cli/root.go`, immediately after each of the two `rt.KeepLastNImages(context.Background(), cfg.Name, 5)` calls in the deploy handler (lines 346 and 466), add:

```go
		runtime.AutoCleanup(context.Background(), rt)
```

- [ ] **Step 5: Wire auto-cleanup into the git webhook pipeline**

In `internal/gitdeploy/deployer.go`, immediately after each of the two `p.rt.KeepLastNImages(ctx, appName, 5)` calls (lines 215 and 315), add:

```go
		runtime.AutoCleanup(ctx, p.rt)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestAutoCleanup" -v -count=1 && go test ./internal/cli/... -run "TestAutoCleanup" -v -count=1`

Expected: PASS (both tests)

- [ ] **Step 7: Run the full test suite**

Run: `go vet ./... && go test ./... -count=1`

Expected: PASS everywhere

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root.go internal/cli/root_test.go internal/gitdeploy/deployer.go
git commit -m "feat: auto-clean dangling images and build cache after deploy"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md` — add `tengiz cleanup` section to CLI Reference
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI command list
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented

- [ ] **Step 1: Add `tengiz cleanup` to `README.md`**

Insert a new section after the `### tengiz run <app> [--] <command> [args...]` section (which ends just before line 206, `### tengiz start <app>`):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space. All Tengiz-managed containers (including scale-to-zero stopped apps) are protected via the `tengiz-app` label and are never removed.

| Flag | Description |
|------|-------------|
| `--dry-run` | Print the docker commands that would run without executing them |
| `--containers` | Remove stopped containers not managed by tengiz |
| `--images` | Remove dangling (untagged) images |
| `--volumes` | Remove unused Docker volumes (opt-in) |
| `--networks` | Remove unused Docker networks (opt-in) |
| `--build-cache` | Remove the Docker build cache |

With no category flags, defaults to `--containers --images --build-cache`. Runs automatically (images + build-cache) after every successful deploy.

```bash
tengiz cleanup            # safe defaults
tengiz cleanup --dry-run  # preview only
tengiz cleanup --volumes --networks --build-cache  # aggressive
```
```

- [ ] **Step 2: Add `tengiz cleanup` to `AGENTS.md`**

In the CLI section of `AGENTS.md`, after the `tengiz build-logs <app> [deployment-id]` line, add:

```
tengiz cleanup [--dry-run] [--containers] [--images] [--volumes] [--networks] [--build-cache] → prune unused Docker resources (label-protected, safe defaults)
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table, change row 6's status:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "Implemented Features (Not Pending)" table:

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-14) |
```

- [ ] **Step 4: Verify docs render**

Run: `go build -o /tmp/tengiz . && /tmp/tengiz cleanup --help`

Expected: help text lists `--build-cache`, `--containers`, `--dry-run`, `--images`, `--networks`, `--volumes` and the description. No tests affected by docs.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```

- [ ] **Step 6: Final verification**

Run: `go vet ./... && go test ./... -count=1`

Expected: PASS everywhere.

---

## Self-Review

**1. Spec coverage:** Feature #6 (Docker Housekeeping) requires label-based pruning + `tengiz cleanup`. Covered: label protection in `cleanupArgs` (Task 1), the `tengiz cleanup` command (Task 2), and the rationale that "continuous deploy and scale-to-zero waste disk" is addressed by post-deploy auto-cleanup (Task 3). Docs requirement from AGENTS.md handled in Task 4. The Coolify "periodic DockerCleanupJob" aspect is intentionally out of scope (belongs to feature #57 Background Monitoring Scheduler) — YAGNI; the CLI + deploy-hook deliver the value now.

**2. Placeholder scan:** No TBD/TODO; every code step contains complete code; every test step contains full test code; commands have expected output.

**3. Type consistency:** `CleanupOptions{DryRun Containers Images Volumes Networks BuildCache}` is defined once in Task 1 and used identically in Tasks 2-3. `CleanupResult{Categories []CleanupCategoryResult}` and `CleanupCategoryResult{Category Command Reclaimed Err}` match across `cleanupCategory` (Task 1), `printCleanupResult` (Task 2), and `AutoCleanup` (Task 3). `runtime.AutoCleanup(ctx, rt Manager)` signature is used identically in both wiring sites. `mockRTForDeploy.cleanupCalls`/`lastCleanupOpts` fields (Task 1) are consumed by the Task 3 test. Flag names match: `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-14-docker-housekeeping.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
