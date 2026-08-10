# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a safe, label-aware `tengiz cleanup` command that prunes unused Docker containers, dangling images, networks, and build cache (optionally volumes) without ever touching Tengiz-managed containers or rollback images, plus an `--interval` mode for periodic housekeeping.

**Architecture:** All operations shell out to the `docker` CLI via `os/exec` (the existing pattern — no SDK). Safety comes from Docker filters, not from guessing: container pruning only matches containers that **lack** the `tengiz-app` label (`docker container prune -f --filter label!=tengiz-app`), so idle scale-to-zero containers survive; image pruning never uses `-a`, so tagged `tengiz-apps/<app>:<id>` rollback images survive. A new `Cleanup(ctx, opts)` method is added to `runtime.Manager`, backed by pure, unit-testable argument-builder helpers (`pruneArgs`, `systemDfArgs`, `cleanupCategories`) following the existing `buildLogArgs`/`buildRunArgs` pattern. The CLI command lives in its own file `internal/cli/cleanup.go` and self-registers via `init()` like `internal/cli/preview.go:87`.

**Tech Stack:** Go 1.26, `docker` CLI (verified present), `os/exec`, `context`, Cobra (`github.com/spf13/cobra`). No new dependencies.

## Global Constraints

- Command name is `cleanup` (`Use: "cleanup"`), registered on `rootCmd`, `Args: cobra.NoArgs`
- Every Docker operation goes through `exec.CommandContext(ctx, "docker", args...)` — never a Docker SDK
- Container prune MUST include `--filter label!=tengiz-app` (docker-filter semantic `label!=key` = "does not have this label"; verified live on Docker 28.0.4 that tengiz-labeled stopped containers are preserved)
- Image prune MUST NOT use `-a`; only dangling images are removed so tagged `tengiz-apps/<app>:<id>` rollback images are preserved
- `--volumes` is opt-in only and never part of the default category set
- Default category set when no category flag is passed: containers, images, networks, build-cache (containers first)
- Category order is fixed: `containers, build-cache, images, networks, volumes`
- Adding `Cleanup` to the `runtime.Manager` interface requires updating all four existing implementations/mocks: `stubManager`, `mockRTForDeploy` (root_test.go), proxy `mockRuntime`, idle `mockRuntime` (verified via grep — missing them breaks the build)
- No changes to existing `RemoveImage`/`KeepLastNImages` behavior
- New files: `internal/cli/cleanup.go` and `internal/cli/cleanup_test.go`; all other edits are additive to existing files
- Existing tests must continue to pass unchanged (only additive edits to existing test mocks)
- The implement workflow commits directly to its checked-out ref — do not create a feature branch

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Modify: `CleanupOptions` type; pure helpers `pruneArgs`, `systemDfArgs`, `cleanupCategories`; `dockerRuntime.Cleanup` implementation |
| `internal/runtime/runtime.go` | Modify: add `Cleanup` to `Manager` interface; implement on `stubManager` |
| `internal/runtime/cleanup_test.go` | Modify: unit tests for helpers + stub `Cleanup` |
| `internal/proxy/proxy_test.go` | Modify: add `Cleanup` method to `mockRuntime` |
| `internal/idle/idle_test.go` | Modify: add `Cleanup` method to `mockRuntime` |
| `internal/cli/root_test.go` | Modify: add `Cleanup` method to `mockRTForDeploy` |
| `internal/cli/cleanup.go` | Create: `cleanupCmd` + `init()` command/flag registration + `cleanupOptionsFromFlags` |
| `internal/cli/cleanup_test.go` | Create: CLI registration, flag, and options-default tests |
| `README.md` | Modify: add `### tengiz cleanup` section to CLI Reference |
| `AGENTS.md` | Modify: add `tengiz cleanup` line to CLI list |

---

### Task 1: Runtime cleanup helpers (pure, unit-testable)

**Files:**
- Modify: `internal/runtime/cleanup.go` (imports already include `context`, `fmt`, `os/exec`, `strings` — no import changes needed)
- Test: `internal/runtime/cleanup_test.go` (add `reflect` import)

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type CleanupOptions struct { Containers, Images, Networks, BuildCache, Volumes, DryRun bool }`
  - `func pruneArgs(category string) []string` — docker sub-args per category, always `-f`, containers always filtered by `label!=tengiz-app`
  - `func systemDfArgs() []string` — returns `[]string{"system", "df"}`
  - `func cleanupCategories(opts CleanupOptions) []string` — requested categories in fixed order (`containers, build-cache, images, networks, volumes`); empty when none requested

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestPruneArgsContainersProtectsTengizLabeledContainers(t *testing.T) {
	args := pruneArgs("containers")
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("pruneArgs(containers) = %v, want %v", args, want)
	}
}

func TestPruneArgsAllCategories(t *testing.T) {
	tests := []struct {
		category string
		want     []string
	}{
		{"build-cache", []string{"builder", "prune", "-f"}},
		{"images", []string{"image", "prune", "-f"}},
		{"networks", []string{"network", "prune", "-f"}},
		{"volumes", []string{"volume", "prune", "-f"}},
		{"unknown", nil},
	}
	for _, tc := range tests {
		if got := pruneArgs(tc.category); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("pruneArgs(%q) = %v, want %v", tc.category, got, tc.want)
		}
	}
}

func TestSystemDfArgs(t *testing.T) {
	want := []string{"system", "df"}
	if got := systemDfArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("systemDfArgs() = %v, want %v", got, want)
	}
}

func TestCleanupCategoriesOrder(t *testing.T) {
	opts := CleanupOptions{Containers: true, Images: true, Networks: true, BuildCache: true, Volumes: true}
	want := []string{"containers", "build-cache", "images", "networks", "volumes"}
	if got := cleanupCategories(opts); !reflect.DeepEqual(got, want) {
		t.Errorf("cleanupCategories() = %v, want %v", got, want)
	}
}

func TestCleanupCategoriesPartial(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want []string
	}{
		{"empty", CleanupOptions{}, nil},
		{"volumes only", CleanupOptions{Volumes: true}, []string{"volumes"}},
		{"containers only", CleanupOptions{Containers: true}, []string{"containers"}},
	}
	for _, tc := range tests {
		if got := cleanupCategories(tc.opts); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: cleanupCategories() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
```

Note: `cleanup_test.go` currently starts with `package runtime` then imports `context` and `testing` only — after this step it imports `context`, `reflect`, `testing`. The existing `TestStubRemoveImage` and `TestStubKeepLastNImages` tests remain untouched.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneArgs|TestSystemDfArgs|TestCleanupCategories" -v -count=1`

Expected: FAIL — `undefined: pruneArgs`, `undefined: systemDfArgs`, `undefined: cleanupCategories`, `undefined: CleanupOptions`

- [ ] **Step 3: Write the implementation**

Append to `internal/runtime/cleanup.go`:

```go
// CleanupOptions controls which Docker resources are pruned by Cleanup.
type CleanupOptions struct {
	Containers bool // prune stopped containers NOT labeled tengiz-app (idle scale-to-zero containers are kept)
	Images     bool // prune dangling images only — tagged rollback images are preserved
	Networks   bool // prune unused networks
	BuildCache bool // prune the Docker builder cache
	Volumes    bool // prune unused volumes (opt-in; never enabled by default)
	DryRun     bool // report disk usage only, delete nothing
}

// pruneArgs returns the docker CLI sub-arguments for a cleanup category.
// Every prune is non-interactive (-f). Container pruning excludes Tengiz-managed
// containers (label tengiz-app) so idle scale-to-zero containers survive cleanup.
func pruneArgs(category string) []string {
	switch category {
	case "containers":
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case "build-cache":
		return []string{"builder", "prune", "-f"}
	case "images":
		return []string{"image", "prune", "-f"}
	case "networks":
		return []string{"network", "prune", "-f"}
	case "volumes":
		return []string{"volume", "prune", "-f"}
	}
	return nil
}

// systemDfArgs returns docker sub-arguments for the disk-usage report used by dry runs.
func systemDfArgs() []string {
	return []string{"system", "df"}
}

// cleanupCategories returns the requested categories in a fixed order (containers first).
func cleanupCategories(opts CleanupOptions) []string {
	var cats []string
	if opts.Containers {
		cats = append(cats, "containers")
	}
	if opts.BuildCache {
		cats = append(cats, "build-cache")
	}
	if opts.Images {
		cats = append(cats, "images")
	}
	if opts.Networks {
		cats = append(cats, "networks")
	}
	if opts.Volumes {
		cats = append(cats, "volumes")
	}
	return cats
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneArgs|TestSystemDfArgs|TestCleanupCategories" -v -count=1`

Expected: PASS (5 functions, all subtests pass)

- [ ] **Step 5: Run the full runtime test suite**

Run: `go test ./internal/runtime/... -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add Docker cleanup arg builders and CleanupOptions for housekeeping"
```

---

### Task 2: `Cleanup` on the Manager interface + dockerRuntime implementation

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.Cleanup`
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface; `internal/runtime/runtime.go:113-122` — add stub impl
- Modify (mock structs that implement `Manager` — required to keep the build green):
  - `internal/cli/root_test.go:76-100` — `mockRTForDeploy`
  - `internal/proxy/proxy_test.go:19-35` — `mockRuntime`
  - `internal/idle/idle_test.go:18-34` — `mockRuntime`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: Task 1 helpers `pruneArgs`, `systemDfArgs`, `cleanupCategories`, type `CleanupOptions`
- Produces: `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (string, error)` — returns a human-readable aggregate report (or `docker system df` output in dry-run mode)

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go` (imports after Task 1 are `context`, `reflect`, `testing` — enough):

```go
func TestStubCleanupReturnsEmptyReport(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report != "" {
		t.Errorf("Cleanup() report = %q, want empty", report)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubCleanupReturnsEmptyReport" -v -count=1`

Expected: FAIL — compile error `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add `Cleanup` to the `Manager` interface and stub**

In `internal/runtime/runtime.go`, inside the `Manager` interface, add after the `KeepLastNImages` line (line 36):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (string, error)
```

In the `stubManager` section, after `KeepLastNImages` (after line 119), add:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (string, error) {
	return "", nil
}
```

- [ ] **Step 4: Add `Cleanup` to the three mock structs**

In `internal/cli/root_test.go`, after the `KeepLastNImages` method of `mockRTForDeploy` (after line 99), add:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (string, error) { return "", nil }
```

In `internal/proxy/proxy_test.go`, after the `KeepLastNImages` method of `mockRuntime` (after line 34), add:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (string, error) { return "", nil }
```

In `internal/idle/idle_test.go`, after the `KeepLastNImages` method of `mockRuntime` (after line 33), add:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (string, error) { return "", nil }
```

- [ ] **Step 5: Implement `dockerRuntime.Cleanup`**

Append to `internal/runtime/cleanup.go`:

```go
// Cleanup prunes the requested Docker resource categories. Containers without
// the tengiz-app label, dangling images, unused networks, build cache, and
// (opt-in) unused volumes are removed. In dry-run mode nothing is deleted;
// instead docker system df output plus the planned categories is returned.
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (string, error) {
	cats := cleanupCategories(opts)
	if !opts.DryRun && len(cats) == 0 {
		return "", fmt.Errorf("nothing to clean: enable at least one category (--containers, --images, --networks, --build-cache, --volumes)")
	}

	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", systemDfArgs()...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		var b strings.Builder
		b.WriteString("Dry run — nothing will be deleted.\n")
		if len(cats) == 0 {
			b.WriteString("Planned categories: containers, build-cache, images, networks\n")
		} else {
			b.WriteString("Planned categories: " + strings.Join(cats, ", ") + "\n")
		}
		b.WriteString("\nCurrent disk usage:\n")
		b.Write(out)
		return b.String(), nil
	}

	var b strings.Builder
	for _, cat := range cats {
		args := pruneArgs(cat)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return strings.TrimSuffix(b.String(), "\n"), fmt.Errorf("docker %s prune: %w\n%s", cat, err, string(out))
		}
		fmt.Fprintf(&b, "[%s]\n%s\n", cat, strings.TrimSpace(string(out)))
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}
```

- [ ] **Step 6: Verify build, vet, and run the affected test suites**

Run: `go build ./...`

Expected: compiles cleanly

Run: `go vet ./internal/runtime/ ./internal/cli/ ./internal/proxy/ ./internal/idle/`

Expected: no findings

Run: `go test ./internal/runtime/ ./internal/cli/ ./internal/proxy/ ./internal/idle/ -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add dockerRuntime.Cleanup and runtime.Manager interface method"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions` from Task 1, `runtime.Manager.Cleanup` from Task 2
- Produces:
  - `var cleanupCmd = &cobra.Command{...}` registered on `rootCmd` via an `init()` in the file (same pattern as `internal/cli/preview.go:87`)
  - `func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions` — CLI flag → options; when no category flag is set, defaults to containers+images+networks+build-cache
  - CLI flags: `--containers`, `--images`, `--networks`, `--build-cache`, `--volumes`, `--dry-run`, `--interval` (minutes, 0 = run once)

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "networks", "build-cache", "volumes", "dry-run", "interval"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupIntervalFlagValue(t *testing.T) {
	cleanupCmd.ParseFlags([]string{"--interval", "30"})
	v, _ := cleanupCmd.Flags().GetInt("interval")
	if v != 30 {
		t.Errorf("interval = %d, want 30", v)
	}
}

func TestCleanupOptionsDefaultSafeSet(t *testing.T) {
	// Explicitly false everywhere → no category selected → the safe default set applies.
	cleanupCmd.ParseFlags([]string{
		"--containers=false", "--images=false", "--networks=false",
		"--build-cache=false", "--volumes=false", "--dry-run=false",
	})
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.Containers || !opts.Images || !opts.Networks || !opts.BuildCache {
		t.Errorf("defaults must enable containers/images/networks/build-cache, got %+v", opts)
	}
	if opts.Volumes {
		t.Errorf("volumes must not be pruned by default, got %+v", opts)
	}
	if opts.DryRun {
		t.Errorf("dry-run must default to false, got %+v", opts)
	}
}

func TestCleanupOptionsVolumeIsExplicit(t *testing.T) {
	cleanupCmd.ParseFlags([]string{
		"--volumes", "--containers=false", "--images=false",
		"--networks=false", "--build-cache=false", "--dry-run=false",
	})
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.Volumes {
		t.Fatalf("expected volumes enabled, got %+v", opts)
	}
	if opts.Containers || opts.Images || opts.Networks || opts.BuildCache {
		t.Errorf("explicit flags must not enable the default set, got %+v", opts)
	}
}

func TestCleanupOptionsDryRunKeepsDefaults(t *testing.T) {
	cleanupCmd.ParseFlags([]string{
		"--dry-run", "--containers=false", "--images=false",
		"--networks=false", "--build-cache=false", "--volumes=false",
	})
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.DryRun {
		t.Fatalf("expected dry-run enabled, got %+v", opts)
	}
	if !opts.Containers {
		t.Errorf("dry-run must still carry the default categories, got %+v", opts)
	}
}
```

Note: the package — `cli` — already has `captureOutput`, `mockRTForDeploy`, etc. in `root_test.go`; no name collisions. Because `cleanupCmd` is a package-level singleton and Go tests run sequentially in one process, every test sets every flag explicitly so ordering never matters.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`

Expected: FAIL — compile error `undefined: cleanupCmd` in `cleanup_test.go`

- [ ] **Step 3: Write the CLI command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images (rollback images are kept)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune the Docker builder cache")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (opt-in — never enabled by default)")
	cleanupCmd.Flags().Bool("dry-run", false, "show current disk usage and what would be cleaned, without deleting")
	cleanupCmd.Flags().Int("interval", 0, "repeat cleanup every N minutes until interrupted (0 = run once)")
	rootCmd.AddCommand(cleanupCmd)
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (safe label-based housekeeping)",
	Long: `Remove Docker resources that are no longer used while keeping Tengiz-managed
containers and rollback images intact.

Default behavior (when no category flag is passed):
  containers  - stops only containers WITHOUT the tengiz-app label, so idle
                scale-to-zero containers are preserved
  images      - dangling images only; tagged rollback images are kept
  networks    - unused networks
  build-cache - Docker builder cache

Volumes are NEVER pruned unless --volumes is passed explicitly.
Use --dry-run to inspect disk usage first. Use --interval N (minutes) to keep
running periodically, e.g. via cron or a systemd timer.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		opts := cleanupOptionsFromFlags(cmd)
		interval, _ := cmd.Flags().GetInt("interval")

		if interval > 0 {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			for {
				report, cleanErr := rt.Cleanup(ctx, opts)
				if report != "" {
					fmt.Println(report)
				}
				if cleanErr != nil {
					return cleanErr
				}
				fmt.Printf("[tengiz] next cleanup in %d minute(s)\n", interval)
				select {
				case <-ctx.Done():
					fmt.Println("[tengiz] cleanup stopped")
					return nil
				case <-time.After(time.Duration(interval) * time.Minute):
				}
			}
		}

		report, err := rt.Cleanup(cmd.Context(), opts)
		if report != "" {
			fmt.Println(report)
		}
		if err != nil {
			return err
		}
		if opts.DryRun {
			fmt.Println("[tengiz] dry run complete — nothing was deleted")
		} else {
			fmt.Println("[tengiz] cleanup complete")
		}
		return nil
	},
}

// cleanupOptionsFromFlags builds CleanupOptions from CLI flags. When no category
// flag is set, the safe defaults (containers, images, networks, build-cache) are used.
func cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	volumes, _ := cmd.Flags().GetBool("volumes")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if !containers && !images && !networks && !buildCache && !volumes {
		containers, images, networks, buildCache = true, true, true, true
	}

	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Networks:   networks,
		BuildCache: buildCache,
		Volumes:    volumes,
		DryRun:     dryRun,
	}
}
```

Note: `os/signal` and `time` are only used by the `--interval` path, which is why the file imports them from the start (the same `init()` self-registration pattern as `internal/cli/preview.go:87`). `cmd.Context()` is already used elsewhere in this package (e.g. `rollbackCmd`, `runCmd`), so it is available. Do NOT import `context` — it is unused: `signal.NotifyContext(cmd.Context(), ...)` requires no explicit `context` package reference.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`

Expected: All 6 `TestCleanup*` tests PASS

- [ ] **Step 5: Build and vet**

Run: `go build ./...`

Expected: compiles cleanly

Run: `go vet ./internal/cli/`

Expected: no findings

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command with label-safe Docker housekeeping"
```

---

### Task 4: Documentation + full verification + self-review

**Files:**
- Modify: `README.md` — add a `### tengiz cleanup` section to the CLI Reference (insert after the `tengiz rollback <app>` section, i.e. after README line 236, before `### tengiz domain` at line 238)
- Modify: `AGENTS.md` — add a `tengiz cleanup` line to the `## CLI` code block

**Interfaces:** consumes nothing new; this task only documents the `cleanup` command produced in Task 3.

- [ ] **Step 1: Add the CLI reference to README.md**

First insert this markdown section after the `tengiz rollback <app>` section (after README line 236, before the `### tengiz domain` header):

```markdown
### `tengiz cleanup`

Prune unused Docker resources while keeping Tengiz-managed containers and rollback images intact.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers that do not have the `tengiz-app` label (idle scale-to-zero containers are preserved) |
| `--images` | Prune dangling images only — tagged `tengiz-apps/<app>:<id>` rollback images are kept |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune the Docker builder cache |
| `--volumes` | Prune unused volumes (opt-in — never enabled by default) |
| `--dry-run` | Show current disk usage (`docker system df`) and what would be cleaned, without deleting anything |
| `--interval <minutes>` | Repeat cleanup every N minutes until interrupted (default `0` = run once) |

When no category flag is passed, the safe defaults apply: containers, images, networks, and build cache. Volumes are only ever pruned with an explicit `--volumes`. Use `--interval` with cron or a systemd timer for periodic housekeeping. Examples:

- `tengiz cleanup` — safe defaults (weekly cron recommended)
- `tengiz cleanup --dry-run` — inspect first, delete nothing
- `tengiz cleanup --volumes` — include unused volumes
- `tengiz cleanup --interval 60` — keep running every hour until Ctrl+C
```

Then add the inline command to the README Quick Start bash block (the code fence near README line 99 that lists `tengiz proxy`):

```bash
tengiz cleanup       # safe label-based Docker housekeeping
```

- [ ] **Step 2: Add `tengiz cleanup` to AGENTS.md CLI list**

In `AGENTS.md`, inside the `## CLI` code block (after the `tengiz rollback <app>` line), add:

```bash
tengiz cleanup              → prune unused Docker resources (label-safe, rolls back images kept)
```

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All tests PASS (the `proxy` package tests are slow — several seconds each — and the `idle` tests are time-sensitive; both suites already pass before this change and remain unchanged)

- [ ] **Step 4: Static analysis and build**

Run: `go vet ./...`

Expected: no findings

Run: `go build -o tengiz .`

Expected: binary builds successfully

- [ ] **Step 5: Manual smoke test (Docker available)**

Verify the real dockerRuntime path works:

```bash
./tengiz cleanup --dry-run
```

Expected: prints `Dry run — nothing will be deleted.` plus a `docker system df` table, and exits 0.

```bash
./tengiz cleanup --volumes --dry-run
```

Expected: dry run includes `volumes` in `Planned categories`.

- [ ] **Step 6: Self-review against spec**

Check against `docs/FUTURES_FEATURES.md` "#6 Docker Housekeeping" and its full write-up:
- `tengiz cleanup` komutu ✅ (Task 3 command)
- Label-based filtreleme ile tengiz container korunur ✅ (Task 1/2 `--filter label!=tengiz-app`, containers missing this label are the only ones pruned — verified live)
- Kullanılmayan volume, network, container ve image temizleme ✅ (volumes opted-in via `--volumes`; networks/images/containers in default set)
- Periyodik temizleme (periodic) ✅ (`--interval N` for cron/systemd)
- Rollback images (`KeepLastNImages`-tagged) preserved ✅ (image prune is dangling-only, no `-a`)
- No breaking changes ✅ (existing tests untouched; `Manager` mocks updated additively)

- [ ] **Step 7: Placeholder scan**

Search the plan for any `TBD`, `TODO`, `implement later`, `fill in details`, `Similar to Task` strings. None present. Every code step contains complete, copy-pasteable code.

- [ ] **Step 8: Type consistency check**

- `runtime.CleanupOptions{Containers, Images, Networks, BuildCache, Volumes, DryRun bool}` — identical fields in Task 1 (definition), Task 2 (method signature) and Task 3 (CLI builder)
- `runtime.Manager.Cleanup(ctx, opts) (string, error)` — Task 2 interface, stub, mocks, and Task 3 handler all use the same signature
- `pruneArgs(category string) []string`, `systemDfArgs() []string`, `cleanupCategories(opts) []string` — names/signatures identical across Task 1 tests, Task 2 implementation, and Task 3
- `cleanupOptionsFromFlags(cmd *cobra.Command) runtime.CleanupOptions` — defined and used only in Task 3
- Flag names (`containers`, `images`, `networks`, `build-cache`, `volumes`, `dry-run`, `interval`) — identical between the command definition (Task 3 Step 3), its tests (Task 3 Step 1), and the README table (Task 4)

- [ ] **Step 9: Commit docs**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command for Docker housekeeping"
```