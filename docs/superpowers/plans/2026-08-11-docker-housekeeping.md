# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, dangling images, unused volumes, and unused networks using label-based filtering so Tengiz-managed containers are never touched, plus a `--dry-run` report mode.

**Architecture:** The pruning logic lives on `runtime.Manager` as a new `Cleanup(ctx, opts)` method implemented by `dockerRuntime` in `internal/runtime/cleanup.go`, which shells out to `docker <object> prune -f`. Every prune (except images) carries `--filter label!=tengiz-app` so stopped Tengiz containers (scale-to-zero) and Tengiz volumes/networks are protected; images are pruned as *dangling only* (`docker image prune -f`), which is inherently safe because every Tengiz image is tagged (`tengiz-apps/<name>:<deploymentID>`). Dry-run lists the same objects via `docker ps -a --filter status=exited`, `docker images --filter dangling=true`, `docker volume ls --filter dangling=true`, and `docker network ls --filter dangling=true`. The Docker command construction is factored into pure functions (`buildPruneArgs`, `buildDryRunArgs`) so it is fully unit-testable without Docker. The CLI command lives in a new `internal/cli/cleanup.go` file, following the existing `internal/cli/preview.go` self-registration pattern. When no category flag is given, all four categories are enabled.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `os/exec` Docker CLI passthrough (no new dependencies).

## Global Constraints

- All `tengiz-*` containers (production and env-scoped) are protected via `--filter label!=tengiz-app` — never prune a container with the `tengiz-app` label
- Images are pruned dangling-only (`docker image prune -f`) — tagged `tengiz-apps/*` images are never removed here (deploy-time `KeepLastNImages` already handles versioned image retention)
- `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` is added to the `runtime.Manager` interface — every implementor (`stubManager`, `mockRTForDeploy`, both `mockRuntime` types) must implement it or the tree won't compile
- No new external Go dependencies
- Default `tengiz cleanup` (no category flags) enables all four categories
- `--dry-run` lists what would be removed but removes nothing
- Existing tests must continue to pass without modification (other than adding the new required `Cleanup` method to existing test mocks)
- Run full suite with `go test ./... -v -count=1`; static check with `go vet ./...`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupResult` types; add `Cleanup` to `Manager` interface; stub implementation |
| `internal/runtime/cleanup.go` | `buildPruneArgs`, `buildDryRunArgs` pure builders + `dockerRuntime.Cleanup` / `runPrune` / `listForCleanup` exec implementation |
| `internal/runtime/cleanup_test.go` | Tests for pure builders + stub `Cleanup` |
| `internal/cli/cleanup.go` | New `cleanupCmd` (self-registers via `init()`), flag parsing, `defaultCleanupOptions` helper, output |
| `internal/cli/cleanup_test.go` | Tests for registration, flags, and default-options resolution |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` method to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` method to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |

---

### Task 1: Add `Cleanup` to the `runtime.Manager` interface (types + stub + all mocks)

**Files:**
- Modify: `internal/runtime/runtime.go` — add `CleanupOptions`, `CleanupResult` types, interface method, stub method
- Modify: `internal/cli/root_test.go:69-100` — add `Cleanup` to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:15-35` — add `Cleanup` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:14-34` — add `Cleanup` to `mockRuntime`
- Test: `internal/runtime/cleanup_test.go` — stub `Cleanup` test

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{Containers, Images, Volumes, Networks, DryRun bool}`, `runtime.CleanupResult{Containers, Images, Volumes, Networks []string}`, `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	result, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(result.Containers) != 0 || len(result.Images) != 0 || len(result.Volumes) != 0 || len(result.Networks) != 0 {
		t.Errorf("stub Cleanup should return empty result, got %+v", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: FAIL with `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add types + interface method + stub implementation in `internal/runtime/runtime.go`**

Add these types above the `Manager` interface (after the existing `RunOptions` block):

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	DryRun     bool
}

type CleanupResult struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
}
```

Add to the `Manager` interface (after the `Run` method):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add to the `stubManager` (after its `Run` method):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 4: Add `Cleanup` to the three existing test mocks so the tree compiles**

In `internal/cli/root_test.go`, add to `mockRTForDeploy` (after its `Run` method):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

In `internal/proxy/proxy_test.go`, add to `mockRuntime` (after its `Run` method):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

In `internal/idle/idle_test.go`, add to `mockRuntime` (after its `Run` method):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/proxy/... ./internal/idle/... -count=1`

Expected: PASS — `TestStubCleanup` passes, and all mock types still satisfy `runtime.Manager`.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface"
```

---

### Task 2: Pure Docker command builders for prune and dry-run listing

**Files:**
- Modify: `internal/runtime/cleanup.go` — add category constants, `buildPruneArgs`, `buildDryRunArgs`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing (pure functions, no manager state)
- Produces: `buildPruneArgs(category string) []string`, `buildDryRunArgs(category string) []string` — full `docker` subcommand args, category constants `categoryContainers/categoryImages/categoryVolumes/categoryNetworks` (values `"containers"`, `"images"`, `"volumes"`, `"networks"`). Task 3 uses these.

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		category string
		expected []string
	}{
		{categoryContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{categoryImages, []string{"image", "prune", "-f"}},
		{categoryVolumes, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{categoryNetworks, []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}},
	}
	for _, tt := range tests {
		got := buildPruneArgs(tt.category)
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("buildPruneArgs(%q) = %v, want %v", tt.category, got, tt.expected)
		}
	}
}

func TestBuildDryRunArgs(t *testing.T) {
	tests := []struct {
		category string
		expected []string
	}{
		{categoryContainers, []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.ID}}"}},
		{categoryImages, []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}},
		{categoryVolumes, []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}},
		{categoryNetworks, []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}},
	}
	for _, tt := range tests {
		got := buildDryRunArgs(tt.category)
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("buildDryRunArgs(%q) = %v, want %v", tt.category, got, tt.expected)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestBuildDryRunArgs" -v -count=1`

Expected: FAIL with `undefined: buildPruneArgs`, `undefined: buildDryRunArgs`

- [ ] **Step 3: Add imports + constants + pure builders in `internal/runtime/cleanup.go`**

Replace the import block at the top of the file with:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)
```

(unchanged — `reflect` is only used in the test file). Add these functions to the end of `internal/runtime/cleanup.go`:

```go
const (
	categoryContainers = "containers"
	categoryImages     = "images"
	categoryVolumes    = "volumes"
	categoryNetworks   = "networks"
)

func buildPruneArgs(category string) []string {
	switch category {
	case categoryContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case categoryImages:
		return []string{"image", "prune", "-f"}
	case categoryVolumes:
		return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	case categoryNetworks:
		return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	}
	return nil
}

func buildDryRunArgs(category string) []string {
	switch category {
	case categoryContainers:
		return []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.ID}}"}
	case categoryImages:
		return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
	case categoryVolumes:
		return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
	case categoryNetworks:
		return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
	}
	return nil
}
```

- [ ] **Step 4: Add `reflect` import to `internal/runtime/cleanup_test.go`**

Replace the import block at the top of `internal/runtime/cleanup_test.go`:

```go
import (
	"context"
	"reflect"
	"testing"
)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestBuildDryRunArgs" -v -count=1`

Expected: PASS (all 8 subtests)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add prune and dry-run arg builders"
```

---

### Task 3: `dockerRuntime.Cleanup` exec implementation

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `Cleanup`, `runPrune`, `listForCleanup`, `cleanupEnabled`
- Test: `internal/runtime/cleanup_test.go` — stub already covers the interface; add a test for the pure `cleanupEnabled` helper

**Interfaces:**
- Consumes: `buildPruneArgs(category)`, `buildDryRunArgs(category)`, `CleanupOptions` from Tasks 1–2
- Produces: `dockerRuntime.Cleanup(ctx, opts) (CleanupResult, error)` — the concrete exec implementation consumed by the CLI in Task 4; dry-run returns the listed IDs/names, real mode runs prunes and returns an empty result

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestCleanupEnabled(t *testing.T) {
	tests := []struct {
		name     string
		opts     CleanupOptions
		category string
		want     bool
	}{
		{"containers on", CleanupOptions{Containers: true}, categoryContainers, true},
		{"containers off", CleanupOptions{}, categoryContainers, false},
		{"images on", CleanupOptions{Images: true}, categoryImages, true},
		{"volumes on", CleanupOptions{Volumes: true}, categoryVolumes, true},
		{"networks on", CleanupOptions{Networks: true}, categoryNetworks, true},
		{"unknown category", CleanupOptions{Containers: true}, "bogus", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanupEnabled(tt.opts, tt.category); got != tt.want {
				t.Errorf("cleanupEnabled(%+v, %q) = %v, want %v", tt.opts, tt.category, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestCleanupEnabled -v -count=1`

Expected: FAIL with `undefined: cleanupEnabled`

- [ ] **Step 3: Implement `Cleanup`, `runPrune`, `listForCleanup`, `cleanupEnabled`**

Add to `internal/runtime/cleanup.go` (after the pure builders):

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var result CleanupResult
	for _, cat := range []string{categoryContainers, categoryImages, categoryVolumes, categoryNetworks} {
		if !cleanupEnabled(opts, cat) {
			continue
		}
		if opts.DryRun {
			items, err := r.listForCleanup(ctx, cat, opts)
			if err != nil {
				return result, err
			}
			switch cat {
			case categoryContainers:
				result.Containers = items
			case categoryImages:
				result.Images = items
			case categoryVolumes:
				result.Volumes = items
			case categoryNetworks:
				result.Networks = items
			}
			continue
		}
		if err := r.runPrune(ctx, cat, opts); err != nil {
			return result, err
		}
	}
	return result, nil
}

func cleanupEnabled(opts CleanupOptions, category string) bool {
	switch category {
	case categoryContainers:
		return opts.Containers
	case categoryImages:
		return opts.Images
	case categoryVolumes:
		return opts.Volumes
	case categoryNetworks:
		return opts.Networks
	}
	return false
}

func (r *dockerRuntime) runPrune(ctx context.Context, category string, opts CleanupOptions) error {
	args := buildPruneArgs(category)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s prune: %w\n%s", category, err, string(out))
	}
	log.Printf("[runtime] docker %s prune: %s", category, strings.TrimSpace(string(out)))
	return nil
}

func (r *dockerRuntime) listForCleanup(ctx context.Context, category string, opts CleanupOptions) ([]string, error) {
	args := buildDryRunArgs(category)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s list: %w\n%s", category, err, string(out))
	}
	var items []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			items = append(items, line)
		}
	}
	return items, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -count=1`

Expected: PASS — `TestCleanupEnabled`, `TestBuildPruneArgs`, `TestBuildDryRunArgs`, `TestStubCleanup`, and all pre-existing runtime tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement docker cleanup with dry-run"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult` from Tasks 1–3
- Produces: `cleanupCmd *cobra.Command` (self-registered via `init()`), `defaultCleanupOptions(opts runtime.CleanupOptions) runtime.CleanupOptions`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

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
		t.Fatalf("cleanup command not found, got %v", cmd)
	}
}

func TestCleanupFlags(t *testing.T) {
	for _, f := range []string{"containers", "images", "volumes", "networks", "dry-run"} {
		if cleanupCmd.Flags().Lookup(f) == nil {
			t.Errorf("cleanup command missing --%s flag", f)
		}
	}
}

func TestDefaultCleanupOptions(t *testing.T) {
	all := defaultCleanupOptions(runtime.CleanupOptions{})
	if !all.Containers || !all.Images || !all.Volumes || !all.Networks {
		t.Errorf("default cleanup should enable all categories, got %+v", all)
	}

	partial := defaultCleanupOptions(runtime.CleanupOptions{Images: true})
	if !partial.Images || partial.Containers || partial.Volumes || partial.Networks {
		t.Errorf("explicit category should not enable others, got %+v", partial)
	}

	dry := defaultCleanupOptions(runtime.CleanupOptions{DryRun: true})
	if !dry.DryRun || !dry.Containers || !dry.Images || !dry.Volumes || !dry.Networks {
		t.Errorf("dry-run with no categories should keep DryRun and enable all, got %+v", dry)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupFlags|TestDefaultCleanupOptions" -v -count=1`

Expected: FAIL with `cleanup command not registered`, and `undefined: cleanupCmd`, `undefined: defaultCleanupOptions`

- [ ] **Step 3: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources",
	Long: `Prune unused Docker resources to reclaim disk space.

By default prunes all categories: stopped non-Tengiz containers, dangling
images, unused volumes, and unused networks. Tengiz-managed containers
(labeled tengiz-app=...) are always protected and never removed.

Use category flags to prune only specific resources. Use --dry-run to list
what would be removed without removing anything.

Examples:
  tengiz cleanup                 # prune all categories
  tengiz cleanup --containers    # prune only stopped non-Tengiz containers
  tengiz cleanup --dry-run       # show what would be removed, remove nothing`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		opts := defaultCleanupOptions(runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			DryRun:     dryRun,
		})

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "pruned"
		if opts.DryRun {
			verb = "would remove"
		}

		if opts.Containers {
			printCleanupCategory("containers", result.Containers, verb, opts.DryRun)
		}
		if opts.Images {
			printCleanupCategory("images", result.Images, verb, opts.DryRun)
		}
		if opts.Volumes {
			printCleanupCategory("volumes", result.Volumes, verb, opts.DryRun)
		}
		if opts.Networks {
			printCleanupCategory("networks", result.Networks, verb, opts.DryRun)
		}
		return nil
	},
}

func printCleanupCategory(label string, items []string, verb string, dryRun bool) {
	if dryRun {
		if len(items) == 0 {
			fmt.Printf("[tengiz] no %s to %s\n", label, verb)
			return
		}
		fmt.Printf("[tengiz] %s %s:\n", label, verb)
		for _, item := range items {
			fmt.Printf("  %s\n", item)
		}
		return
	}
	fmt.Printf("[tengiz] %s %s\n", label, verb)
}

func defaultCleanupOptions(opts runtime.CleanupOptions) runtime.CleanupOptions {
	if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
	}
	return opts
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("dry-run", false, "report what would be removed without removing anything")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanupCommandRegistered|TestCleanupFlags|TestDefaultCleanupOptions" -v -count=1`

Expected: PASS (all tests green)

- [ ] **Step 5: Run the full suite to confirm no regressions**

Run: `go vet ./... && go test ./... -v -count=1`

Expected: PASS — everything compiles, all existing tests pass, vet is clean. (`TestCleanupCommandRegistered` exercises the new command through the shared `rootCmd`; note `--env` is inherited from the root persistent flag.)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Documentation update

**Files:**
- Modify: `README.md` — add `tengiz cleanup` to CLI Reference + Quick Start
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI command list

**Interfaces:**
- Consumes: the command surface produced by Task 4
- Produces: documentation (no code)

- [ ] **Step 1: Add `tengiz cleanup` to the README CLI Reference**

In `README.md`, after the `### tengiz ps` section (which ends before the `### tengiz logs` heading), insert:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped non-Tengiz containers |
| `--images` | Prune dangling images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--dry-run` | Report what would be removed without removing anything |

With no category flags, all four categories are pruned. Tengiz-managed containers (labeled `tengiz-app=...`) are always protected and never removed. Images are pruned dangling-only, so tagged `tengiz-apps/*` images are never removed here. Examples:

```
tengiz cleanup               # prune all categories
tengiz cleanup --containers  # prune only stopped non-Tengiz containers
tengiz cleanup --dry-run     # show what would be removed, remove nothing
```
```

- [ ] **Step 2: Add `tengiz cleanup` to the README Quick Start**

In `README.md`, in the Quick Start code block (lines ~96-101), add a line:

```bash
tengiz cleanup        # reclaim disk space (prune unused Docker resources)
```

- [ ] **Step 3: Add `tengiz cleanup` to AGENTS.md CLI list**

In `AGENTS.md`, in the `## Commands` code block, after the `tengiz build-logs <app> [deployment-id]` line, add:

```
tengiz cleanup [--containers] [--images] [--volumes] [--networks] [--dry-run] → prune unused Docker resources (Tengiz containers always protected)
```

- [ ] **Step 4: Verify no Markdown/code issues**

Run: `git diff --stat` and confirm only `README.md` and `AGENTS.md` changed. No test run needed for a docs-only change.

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage.** FUTURES_FEATURES.md #6 (Docker Housekeeping) requires: label-based `docker system prune` and a `tengiz cleanup` command. Task 4 adds `tengiz cleanup`; Tasks 2–3 implement label-based pruning (`label!=tengiz-app`) per category (containers, images, volumes, networks). The Coolify rationale ("kullanılmayan volume, network, container ve image'leri periyodik temizleme" + "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur") is fully covered: all four categories plus the label protection. Dry-run and granular category flags are deliberate v1 additions beyond the spec (matching the future Granular Docker Prune Operations #56 direction) and do not remove any required behavior.

**2. Placeholder scan.** Every step has concrete code, exact file paths, exact commands with expected output. No TBD/TODO, no "add error handling" without code, no "similar to Task N", no undefined references.

**3. Type consistency.** `CleanupOptions`/`CleanupResult` field names are identical across Tasks 1, 3, and 4 (`Containers`, `Images`, `Volumes`, `Networks`, `DryRun`). `cleanupEnabled(opts CleanupOptions, category string)` (Task 3) matches the call site. `buildPruneArgs(category string)`/`buildDryRunArgs(category string)` signatures match Task 3's calls. Category constants (`categoryContainers` etc.) are defined once in Task 2 and reused in Task 3 tests. `defaultCleanupOptions(opts runtime.CleanupOptions) runtime.CleanupOptions` is defined and tested in Task 4. `printCleanupCategory(label string, items []string, verb string, dryRun bool)` matches its call sites. All four `runtime.Manager` implementors get `Cleanup` in Task 1, so the tree compiles at every task boundary.

---

## Execution Handoff

Plan complete. Two execution options:

1. **Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** — execute tasks in this session with checkpoints

Which approach?
