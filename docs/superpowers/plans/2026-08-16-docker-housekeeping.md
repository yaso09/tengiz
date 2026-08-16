# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused networks, build cache, opt-in volumes) while protecting Tengiz-managed containers via label-based filtering, so disk space stops being the #1 production failure mode on single-server deployments.

**Architecture:** Add `Cleanup(ctx, opts CleanupOptions) ([]CleanupReport, error)` to the existing `runtime.Manager` interface. The exec-based `dockerRuntime` impl loops over selected categories and shells out to `docker <resource> prune` with the standard `os/exec` pattern already used by `RemoveImage`/`KeepLastNImages` in `internal/runtime/cleanup.go`. A pure function `pruneCommand(category)` builds the docker args (unit-testable without Docker), and the dry-run path never invokes `exec` (so it is also unit-testable without Docker). The Cobra `cleanupCmd` in `internal/cli/root.go` wires flags → `runtime.CleanupOptions` → `rt.Cleanup()` → prints a per-category report.

**Tech Stack:** Go 1.26, Cobra, existing `runtime.Manager` / `config.Store` patterns, Docker CLI via `os/exec`. No new external dependencies.

## Global Constraints

- **Protection rule:** every container prune MUST include `--filter label!=tengiz-app` so stopped Tengiz containers (scale-to-zero cold-start state) and preview containers are never removed
- **Image pruning is dangling-only:** use `docker image prune` WITHOUT `-a` (and with `--filter dangling=true`); user-pulled images and Tengiz versioned images are preserved — per-app image retention is already handled by the existing `KeepLastNImages` (called on every deploy)
- **Volumes are opt-in, NOT default:** Tengiz uses bind mounts (`-v host_path:container_path`), never named volumes, so `docker volume prune` only runs when the user passes `--volumes` (mirrors Docker's own `docker system prune --volumes` safety semantics); never add volumes to `defaultCleanupCategories()`
- **Default category set (no flags):** containers, images, networks, build-cache — mirroring `docker system prune` (which the feature rationale cites) minus the dangerous volume step
- **Explicit category flags replace defaults:** if the user passes any of `--containers/--images/--volumes/--networks/--build-cache`, ONLY those categories run (surgical mode)
- **Dry-run never executes docker:** `--dry-run` returns the exact `docker ...` command strings that would run, with `CleanupReport.Command` populated, and skips `exec.CommandContext`
- **Per-category resilience:** a failing prune is recorded in `CleanupReport.Error` and the loop continues; `Cleanup` returns `(reports, nil)` unless a category cannot be dispatched at all
- `--env` global flag is ignored by `cleanup` (the command operates on the whole Docker host, not per-environment)
- **Interface propagation:** adding `Cleanup` to `runtime.Manager` requires updating all 4 existing implementers in the same task or the build breaks: `stubManager` (runtime.go), `mockRTForDeploy` (cli/root_test.go), `mockRuntime` (idle/idle_test.go), `mockRuntime` (proxy/proxy_test.go)
- No new external dependencies; existing tests must continue to pass (verified baseline: `go test ./internal/runtime/... ./internal/idle/... ./internal/proxy/... ./internal/cli/... -count=1` is green)
- CLI output uses the existing `[tengiz]` prefix convention; runtime warnings use `log.Printf("[runtime] ...")`
- README.md CLI Reference must document the new command (AGENTS.md rule: update docs on UI/UX changes)
- Commit style: `feat: ...` (matches repo convention)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupCategory` type + consts, `CleanupOptions`, `CleanupReport`; extend `Manager` interface; `stubManager.Cleanup` no-op |
| `internal/runtime/cleanup.go` | Pure `pruneCommand(cat)` arg builder, `defaultCleanupCategories()`, `dockerRuntime.Cleanup()` |
| `internal/runtime/cleanup_test.go` | Unit tests for `pruneCommand`, `defaultCleanupCategories`, dry-run `Cleanup` (no Docker needed), stub `Cleanup` |
| `internal/cli/root.go` | `cleanupCmd` + flag registration in `init()` |
| `internal/cli/root_test.go` | Registration test, flag-existence test, flag-parsing RunE test; add `Cleanup` to `mockRTForDeploy` |
| `internal/idle/idle_test.go` | Add `Cleanup` method to `mockRuntime` (interface compile) |
| `internal/proxy/proxy_test.go` | Add `Cleanup` method to `mockRuntime` (interface compile) |
| `README.md` | Document `tengiz cleanup` under CLI Reference (insert after `tengiz rollback` section) |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 implemented: table row ⬜→✅, add to "Implemented Features" table, add `Status` to feature detail section |

No new source files created beyond tests. 9 files touched total.

---

### Task 1: Runtime Cleanup API (types, command builder, `Manager.Cleanup`)

**Files:**
- Modify: `internal/runtime/runtime.go` — add types/consts, interface method, stub
- Modify: `internal/runtime/cleanup.go` — add `pruneCommand`, `defaultCleanupCategories`, `dockerRuntime.Cleanup`
- Modify: `internal/runtime/cleanup_test.go` — add tests
- Modify: `internal/cli/root_test.go:69-100` — add `Cleanup` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:14-34` — add `Cleanup` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:15-35` — add `Cleanup` to `mockRuntime`

**Interfaces:**
- Consumes: nothing new (uses existing `context`, `os/exec` imports already in package)
- Produces:
  - `type CleanupCategory string` with consts `CleanupContainers = "containers"`, `CleanupImages = "images"`, `CleanupVolumes = "volumes"`, `CleanupNetworks = "networks"`, `CleanupBuildCache = "build-cache"`
  - `type CleanupOptions struct { Categories []CleanupCategory; DryRun bool }`
  - `type CleanupReport struct { Category CleanupCategory; Command string; Output string; Error string }`
  - `Manager.Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupReport, error)`
  - `pruneCommand(cat CleanupCategory) []string` (package-private)
  - `defaultCleanupCategories() []CleanupCategory` (package-private)

- [ ] **Step 1: Write the failing tests** in `internal/runtime/cleanup_test.go`

Replace the entire contents of `internal/runtime/cleanup_test.go` with:

```go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestStubRemoveImage(t *testing.T) {
	m := NewStub()
	if err := m.RemoveImage(context.Background(), "tengiz-apps/testapp:v1"); err != nil {
		t.Fatalf("RemoveImage() error = %v", err)
	}
}

func TestStubKeepLastNImages(t *testing.T) {
	m := NewStub()
	if err := m.KeepLastNImages(context.Background(), "testapp", 5); err != nil {
		t.Fatalf("KeepLastNImages() error = %v", err)
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	reports, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if reports != nil {
		t.Errorf("Cleanup() reports = %v, want nil", reports)
	}
}

func TestPruneCommandContainers(t *testing.T) {
	got := pruneCommand(CleanupContainers)
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneCommand(containers) = %v, want %v", got, want)
	}
}

func TestPruneCommandImages(t *testing.T) {
	got := pruneCommand(CleanupImages)
	want := []string{"image", "prune", "-f", "--filter", "dangling=true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneCommand(images) = %v, want %v", got, want)
	}
}

func TestPruneCommandVolumes(t *testing.T) {
	got := pruneCommand(CleanupVolumes)
	want := []string{"volume", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneCommand(volumes) = %v, want %v", got, want)
	}
}

func TestPruneCommandNetworks(t *testing.T) {
	got := pruneCommand(CleanupNetworks)
	want := []string{"network", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneCommand(networks) = %v, want %v", got, want)
	}
}

func TestPruneCommandBuildCache(t *testing.T) {
	got := pruneCommand(CleanupBuildCache)
	want := []string{"builder", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pruneCommand(build-cache) = %v, want %v", got, want)
	}
}

func TestDefaultCleanupCategories(t *testing.T) {
	want := []CleanupCategory{
		CleanupContainers,
		CleanupImages,
		CleanupNetworks,
		CleanupBuildCache,
	}
	got := defaultCleanupCategories()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("defaultCleanupCategories() = %v, want %v", got, want)
	}
}

func TestDockerCleanupDryRun(t *testing.T) {
	r := &dockerRuntime{}
	reports, err := r.Cleanup(context.Background(), CleanupOptions{
		Categories: []CleanupCategory{CleanupContainers, CleanupImages},
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("len(reports) = %d, want 2", len(reports))
	}
	if reports[0].Category != CleanupContainers ||
		reports[0].Command != "docker container prune -f --filter label!=tengiz-app" {
		t.Errorf("reports[0] = %+v", reports[0])
	}
	if reports[1].Category != CleanupImages ||
		reports[1].Command != "docker image prune -f --filter dangling=true" {
		t.Errorf("reports[1] = %+v", reports[1])
	}
}

func TestDockerCleanupDryRunDefaults(t *testing.T) {
	r := &dockerRuntime{}
	reports, err := r.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	want := []CleanupCategory{CleanupContainers, CleanupImages, CleanupNetworks, CleanupBuildCache}
	if len(reports) != len(want) {
		t.Fatalf("len(reports) = %d, want %d", len(reports), len(want))
	}
	for i, w := range want {
		if reports[i].Category != w {
			t.Errorf("reports[%d].Category = %s, want %s", i, reports[i].Category, w)
		}
		if reports[i].Command == "" {
			t.Errorf("reports[%d].Command is empty, want a docker command", i)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneCommand|TestDefaultCleanupCategories|TestDockerCleanup|TestStubCleanup" -v -count=1`

Expected: FAIL with `undefined: CleanupOptions`, `undefined: CleanupCategory`, `undefined: pruneCommand`, `undefined: defaultCleanupCategories`, and `Cleanup` not having a field/method for `m.Cleanup`.

- [ ] **Step 3: Add types and consts to `internal/runtime/runtime.go`**

Add this block right after the `type RunOptions struct { ... }` definition (currently lines 18-29, ending at `runtime.go:29`):

```go
type CleanupCategory string

const (
	CleanupContainers CleanupCategory = "containers"
	CleanupImages     CleanupCategory = "images"
	CleanupVolumes    CleanupCategory = "volumes"
	CleanupNetworks   CleanupCategory = "networks"
	CleanupBuildCache CleanupCategory = "build-cache"
)

type CleanupOptions struct {
	Categories []CleanupCategory
	DryRun     bool
}

type CleanupReport struct {
	Category CleanupCategory `json:"category"`
	Command  string          `json:"command,omitempty"`
	Output   string          `json:"output,omitempty"`
	Error    string          `json:"error,omitempty"`
}
```

- [ ] **Step 4: Add `Cleanup` to the `Manager` interface and the stub**

In `runtime.go`, add this line to the `Manager` interface (after the `KeepLastNImages` line at `runtime.go:36`):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupReport, error)
```

Add this method to `stubManager` (after the `KeepLastNImages` stub method at `runtime.go:117-119`):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupReport, error) {
	return nil, nil
}
```

- [ ] **Step 5: Implement `pruneCommand`, `defaultCleanupCategories`, and `dockerRuntime.Cleanup` in `internal/runtime/cleanup.go`**

Add to the imports block of `cleanup.go` (currently imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings` — all already present, no new imports needed):

Append these functions at the end of `internal/runtime/cleanup.go`:

```go
var pruneResource = map[CleanupCategory]string{
	CleanupContainers: "container",
	CleanupImages:     "image",
	CleanupVolumes:    "volume",
	CleanupNetworks:   "network",
	CleanupBuildCache: "builder",
}

func pruneCommand(cat CleanupCategory) []string {
	resource, ok := pruneResource[cat]
	if !ok {
		return []string{"help"}
	}
	args := []string{resource, "prune", "-f"}
	switch cat {
	case CleanupContainers:
		args = append(args, "--filter", "label!=tengiz-app")
	case CleanupImages:
		args = append(args, "--filter", "dangling=true")
	}
	return args
}

func defaultCleanupCategories() []CleanupCategory {
	return []CleanupCategory{
		CleanupContainers,
		CleanupImages,
		CleanupNetworks,
		CleanupBuildCache,
	}
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) ([]CleanupReport, error) {
	cats := opts.Categories
	if len(cats) == 0 {
		cats = defaultCleanupCategories()
	}
	reports := make([]CleanupReport, 0, len(cats))
	for _, cat := range cats {
		cmdArgs := pruneCommand(cat)
		report := CleanupReport{
			Category: cat,
			Command:  strings.Join(append([]string{"docker"}, cmdArgs...), " "),
		}
		if opts.DryRun {
			reports = append(reports, report)
			continue
		}
		cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			report.Error = fmt.Sprintf("%v\n%s", err, strings.TrimSpace(string(out)))
		} else {
			report.Output = strings.TrimSpace(string(out))
		}
		reports = append(reports, report)
	}
	return reports, nil
}
```

- [ ] **Step 6: Update the remaining `Manager` implementers (required for compilation)**

In `internal/cli/root_test.go`, add to `mockRTForDeploy` (after its `KeepLastNImages` method at `root_test.go:99`):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) ([]runtime.CleanupReport, error) {
	return nil, nil
}
```

In `internal/idle/idle_test.go`, add to `mockRuntime` (after the `KeepLastNImages` method at `idle_test.go:33`):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) ([]runtime.CleanupReport, error) { return nil, nil }
```

In `internal/proxy/proxy_test.go`, add to `mockRuntime` (after the `KeepLastNImages` method at `proxy_test.go:34`):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) ([]runtime.CleanupReport, error) { return nil, nil }
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go build ./... && go vet ./internal/runtime/... && go test ./internal/runtime/... ./internal/idle/... ./internal/proxy/... ./internal/cli/... -count=1`

Expected: `ok` for all four packages; the new tests PASS (TestPruneCommand*, TestDefaultCleanupCategories, TestDockerCleanupDryRun*, TestStubCleanup).

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Cleanup API to runtime manager for docker housekeeping"
```

---

### Task 2: CLI `tengiz cleanup` command + README docs

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` and register in `init()`
- Modify: `internal/cli/root_test.go` — registration + flag-parsing tests
- Modify: `README.md` — document the command (insert after the `tengiz rollback` section, currently ending at `README.md:237`)

**Interfaces:**
- Consumes: `runtime.NewDocker() error`, `runtime.CleanupOptions{Categories []runtime.CleanupCategory, DryRun bool}`, `runtime.CleanupReport{Category, Command, Output, Error}`, category consts `CleanupContainers/Images/Volumes/Networks/BuildCache`, and `rt.Cleanup(ctx, opts)` — all from Task 1
- Produces: `cleanupCmd` (Cobra command registered on `rootCmd`), flags `--containers/--images/--volumes/--networks/--build-cache/--dry-run`; README section `### \`tengiz cleanup\``

- [ ] **Step 1: Write the failing tests** in `internal/cli/root_test.go`

Append these tests:

```go
func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "build-cache", "dry-run"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdFlagParsing(t *testing.T) {
	var called bool
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		for _, flag := range []string{"containers", "images", "volumes", "networks", "build-cache", "dry-run"} {
			v, _ := cmd.Flags().GetBool(flag)
			if !v {
				t.Errorf("flag --%s = false, want true", flag)
			}
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--containers", "--images", "--volumes", "--networks", "--build-cache", "--dry-run"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}
```

(`cobra` is already imported in `root_test.go:13`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCmd" -v -count=1`

Expected: FAIL with `cleanup command not found` and `cleanupCmd` undefined compile error.

- [ ] **Step 3: Implement `cleanupCmd` and register it**

In `internal/cli/root.go`, add this command definition right before `var configCmd = &cobra.Command{` (currently at `root.go:1483`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, networks, build cache, volumes)",
	Long: `Removes unused Docker resources while protecting Tengiz-managed containers.

By default runs: stopped non-Tengiz containers, dangling images, unused networks, and the Docker build cache.
Use --volumes to also prune unused volumes. Passing any category flag runs ONLY those categories.

Use --dry-run to print the exact docker commands that would run without executing them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")

		cats := []runtime.CleanupCategory{}
		for _, f := range []struct {
			flag string
			cat  runtime.CleanupCategory
		}{
			{"containers", runtime.CleanupContainers},
			{"images", runtime.CleanupImages},
			{"volumes", runtime.CleanupVolumes},
			{"networks", runtime.CleanupNetworks},
			{"build-cache", runtime.CleanupBuildCache},
		} {
			set, _ := cmd.Flags().GetBool(f.flag)
			if set {
				cats = append(cats, f.cat)
			}
		}

		reports, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			Categories: cats,
			DryRun:     dryRun,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if len(reports) == 0 {
			fmt.Println("[tengiz] nothing to clean")
			return nil
		}

		for _, r := range reports {
			if dryRun {
				fmt.Printf("[tengiz] dry-run: %s\n", r.Command)
				continue
			}
			fmt.Printf("[tengiz] cleaned %s\n", r.Category)
			if r.Output != "" {
				for _, line := range strings.Split(r.Output, "\n") {
					if line != "" {
						fmt.Printf("  %s\n", line)
					}
				}
			}
			if r.Error != "" {
				fmt.Printf("  warning: %s\n", r.Error)
			}
		}
		return nil
	},
}
```

In `internal/cli/root.go`, inside `init()` (after `rootCmd.AddCommand(rollbackCmd)` at `root.go:65`), add:

```go
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "print commands without executing")
	rootCmd.AddCommand(cleanupCmd)
```

(`strings` and `fmt` are already imported in `root.go:9,11`; `runtime` at `root.go:26`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCmd" -v -count=1`

Expected: all three tests PASS. Then run the full affected suite:

Run: `go build ./... && go test ./internal/runtime/... ./internal/cli/... -count=1`

Expected: `ok` for both packages.

- [ ] **Step 5: Manual smoke test (optional, requires Docker)**

```bash
go build -o tengiz . && ./tengiz cleanup --dry-run
```

Expected output (four dry-run lines, no docker commands executed):
```
[tengiz] dry-run: docker container prune -f --filter label!=tengiz-app
[tengiz] dry-run: docker image prune -f --filter dangling=true
[tengiz] dry-run: docker network prune -f
[tengiz] dry-run: docker builder prune -f
```

- [ ] **Step 6: Document `tengiz cleanup` in `README.md`**

Insert this section after the `tengiz rollback <app>` section (after `README.md:237`, before `### \`tengiz domain\``):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space, while protecting Tengiz-managed containers.

By default prunes stopped non-Tengiz containers, dangling images, unused networks, and the Docker build cache. Passing any category flag runs ONLY those categories. Volumes are opt-in because they may contain unmounted data.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers that are NOT managed by Tengiz (`label!=tengiz-app`) |
| `--images` | Prune dangling (untagged) images |
| `--volumes` | Prune unused named volumes (opt-in) |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune the Docker BuildKit build cache |
| `--dry-run` | Print the exact commands that would run without executing them |

Examples:

```
tengiz cleanup              # default safe cleanup
tengiz cleanup --volumes    # also prune unused volumes
tengiz cleanup --dry-run    # preview commands first
tengiz cleanup --build-cache
```
```

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go README.md
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 3: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`

**Files:**
- Modify: `docs/FUTURES_FEATURES.md:19` — Priority Ranking row #6: ⬜ → ✅
- Modify: `docs/FUTURES_FEATURES.md:253` — add row to "Implemented Features (Not Pending)" table
- Modify: `docs/FUTURES_FEATURES.md:381` — add Status line to the feature detail section

**Interfaces:**
- Consumes: nothing (docs only)
- Produces: updated status bookkeeping

- [ ] **Step 1: Update the Priority Ranking table row**

In `docs/FUTURES_FEATURES.md`, change line 19 from:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 2: Add a row to the "Implemented Features" table**

After the row at line 253 (`| — | **Webhook ile Otomatik Deploy** | ... | ✅ Implemented (2026-07-17) |`), add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-16) |
```

- [ ] **Step 3: Add Status to the feature detail section**

After the `- **Detected:** 2026-07-14` line at `docs/FUTURES_FEATURES.md:381` (in the `## Docker Housekeeping (Otomatik Temizlik)` section), add:

```markdown
- **Status:** ✅ Implemented (2026-08-16)
```

- [ ] **Step 4: Verify and commit**

Run: `git diff --stat` to confirm only the three doc edits, then:

```bash
git add docs/FUTURES_FEATURES.md
git commit -m "docs: mark docker housekeeping as implemented"
```

---

## Self-Review

**1. Spec coverage** — checked against `docs/FUTURES_FEATURES.md` feature #6:
- "unused volume, network, container ve image'leri periyodik temizleme" → all four categories implemented as prune targets (volumes opt-in via `--volumes`, documented tradeoff in Global Constraints) ✔
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → `--filter label!=tengiz-app` on container prune (Task 1 `pruneCommand`) ✔
- "`tengiz cleanup` komutu" → Task 2 CLI command ✔
- Feature rationale "Label-based `docker system prune`" → default set mirrors `docker system prune` (containers + dangling images + networks + build cache), volumes mirroring the opt-in `--volumes` flag ✔
- No scope creep: build-cache/network/image retention (`KeepLastNImages`) pre-existing behavior left intact; granular-category flags (`#56`), scheduled cleanup (Coolify's `DockerCleanupJob`), and cache/git GC (`#103`) are NOT implemented here ✔

**2. Placeholder scan** — no "TBD"/"TODO"/"add error handling" placeholders; every code step has complete, copy-pasteable code with exact file paths and expected test output. ✔

**3. Type consistency** — `CleanupCategory` consts (`containers/images/volumes/networks/build-cache`), `CleanupOptions{Categories, DryRun}`, and `CleanupReport{Category, Command, Output, Error}` are defined once in Task 1 and referenced identically in Task 2 (CLI flag names match consts: `--containers` → `CleanupContainers`, etc.). `pruneCommand` maps category → docker resource singular (`container/image/volume/network/builder`) in the `pruneResource` map, matching real docker CLI commands. Method name is consistently `Cleanup(ctx, opts)` across interface, stub, dockerRuntime, and all three mocks. ✔