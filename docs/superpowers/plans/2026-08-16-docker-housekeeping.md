# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused volumes/networks, build cache) via label-based `docker prune` calls, while protecting all Tengiz-managed containers through the existing `tengiz-app` label filter.

**Architecture:** The `runtime.Manager` interface gains a `Cleanup(ctx, opts CleanupOptions) (*CleanupReport, error)` method. The Docker implementation builds per-category `docker ... prune` commands from the options (each carrying `--filter label!=tengiz-app` where the prune target honors labels, so Tengiz containers survive), runs them in sequence, and parses `Total reclaimed space:` from each output into a `CleanupReport`. The stub returns an empty report. The new `tengiz cleanup` CLI command resolves flags (defaults to all categories *except* volumes; volumes require an explicit `--volumes`/`--all` plus `--force`) and prints the reclaimed-space summary. `--dry-run` lists planned categories without touching Docker.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` for Docker CLI (no Docker SDK), existing `runtime.Manager` interface + `internal/cli` command pattern, existing test helpers (`NewStub()`, `captureOutput()`, `findSubcommand()`).

## Global Constraints

- No new external dependencies (no Docker SDK; shell out to `docker` via `os/exec`, matching the rest of the codebase)
- Every prune command that can carry a label filter MUST include `--filter label!=tengiz-app` so Tengiz-managed containers are never removed
- Volumes are NEVER pruned by default — only when `--volumes` or `--all` is explicitly passed AND `--force` is passed
- `--dry-run` must not invoke Docker at all (safe to run anywhere)
- Env-awareness: the CLI reads `--env` via the existing `getEnv(cmd)` helper; container name/label conventions stay untouched
- `runtime.NewStub()` (and the `mockRTForDeploy` test double in `internal/cli/root_test.go`) must implement the new interface method so all existing tests compile
- Tests must pass with `go test ./... -v -count=1` and `go vet ./...`
- Follow existing commit style in this repo: short, imperative, `feat:`/`test:`/`docs:` prefixed single-line messages
- Per AGENTS.md, create a feature branch before implementing: `git checkout -b feat/docker-housekeeping`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupReport`, extend `Manager` interface with `Cleanup`, add stub method |
| `internal/runtime/cleanup.go` | Docker implementation of `Cleanup` + `cleanupCommands()` helper that maps options → prune command args |
| `internal/runtime/cleanup_test.go` | Unit tests: stub `Cleanup`, `cleanupCommands()` mapping, interface compliance |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy` so it keeps satisfying `runtime.Manager` |
| `internal/cli/cleanup.go` | New `tengiz cleanup` Cobra command (flags, dry-run, volume guard, report printing) |
| `internal/cli/cleanup_test.go` | CLI tests: registration, flag defaults, dry-run, volume guard, help output |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to the Commands section |

---

### Task 1: Add `Cleanup` to the runtime `Manager` interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface), `internal/runtime/runtime.go:51-122` (stub)
- Modify: `internal/runtime/cleanup_test.go`
- Modify: `internal/cli/root_test.go:98-100` (`mockRTForDeploy` needs the new method)

**Interfaces:**
- Consumes: nothing new (pure addition)
- Produces: `runtime.CleanupOptions{Containers, Images, Volumes, Networks, BuildCache bool}`, `runtime.CleanupReport{ContainersReclaimed, ImagesReclaimed, VolumesReclaimed, NetworksReclaimed, BuildCacheReclaimed string}`, `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
	}
}

func TestStubSatisfiesCleanupInterface(t *testing.T) {
	var iface Manager = NewStub()
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`

Expected: FAIL with `undefined: CleanupOptions` (types don't exist yet).

- [ ] **Step 3: Write minimal implementation in `internal/runtime/runtime.go`**

Add the types above the `Manager` interface (after `RunOptions`, around line 29):

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type CleanupReport struct {
	ContainersReclaimed string
	ImagesReclaimed     string
	VolumesReclaimed    string
	NetworksReclaimed   string
	BuildCacheReclaimed string
}
```

Add the method to the `Manager` interface after the `KeepLastNImages` line (line 36):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
```

Add the stub method at the end of the stub (after the `Run` stub, line 122):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return &CleanupReport{}, nil
}
```

- [ ] **Step 4: Update `mockRTForDeploy` in `internal/cli/root_test.go`**

Add after the `KeepLastNImages` mock (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) { return &runtime.CleanupReport{}, nil }
```

This keeps the test double satisfying the `runtime.Manager` interface (`TestMockRTForDeployImplementsManager` at line 102).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run TestStub -v -count=1 && go test ./internal/cli/ -run TestMockRTForDeploy -v -count=1`

Expected: PASS for all `TestStub*` and `TestMockRTForDeployImplementsManager` tests.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup method to Manager interface"
```

---

### Task 2: Docker implementation of `Cleanup`

**Files:**
- Modify: `internal/runtime/cleanup.go`
- Modify: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.Manager.Cleanup` (from Task 1)
- Produces: `func cleanupCommands(opts CleanupOptions) [][]string` (lower-level docker `prune` arg slices), Docker impl of `Cleanup`; the CLI in Task 3 will rely on the `CleanupOptions` bools and `CleanupReport` fields exactly as defined in Task 1

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestCleanupCommandsEmpty(t *testing.T) {
	cmds := cleanupCommands(CleanupOptions{})
	if len(cmds) != 0 {
		t.Fatalf("expected 0 prune commands for empty opts, got %d: %v", len(cmds), cmds)
	}
}

func TestCleanupCommandsAllCategories(t *testing.T) {
	opts := CleanupOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}
	cmds := cleanupCommands(opts)
	if len(cmds) != 5 {
		t.Fatalf("expected 5 prune commands, got %d: %v", len(cmds), cmds)
	}
}

func TestCleanupCommandsFiltered(t *testing.T) {
	opts := CleanupOptions{Containers: true, Volumes: true}
	cmds := cleanupCommands(opts)
	if len(cmds) != 2 {
		t.Fatalf("expected 2 prune commands, got %d: %v", len(cmds), cmds)
	}
	// Container prune must protect Tengiz containers via label filter
	if cmds[0][0] != "container" {
		t.Fatalf("first command should be container prune, got %v", cmds[0])
	}
	hasLabelFilter := false
	for _, arg := range cmds[0] {
		if arg == "label!=tengiz-app" {
			hasLabelFilter = true
		}
	}
	if !hasLabelFilter {
		t.Fatalf("container prune must include label!=tengiz-app filter, got %v", cmds[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestCleanupCommands -v -count=1`

Expected: FAIL with `undefined: cleanupCommands`.

- [ ] **Step 3: Write minimal implementation in `internal/runtime/cleanup.go`**

Add at the top of the file (after imports):

```go
func cleanupCommands(opts CleanupOptions) [][]string {
	var cmds [][]string
	if opts.Containers {
		cmds = append(cmds, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.Images {
		cmds = append(cmds, []string{"image", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.Volumes {
		cmds = append(cmds, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.Networks {
		cmds = append(cmds, []string{"network", "prune", "-f"})
	}
	if opts.BuildCache {
		cmds = append(cmds, []string{"builder", "prune", "-f"})
	}
	return cmds
}
```

Add the Docker implementation at the end of `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{}
	cmds := cleanupCommands(opts)
	for _, args := range cmds {
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
		reclaimed := parseReclaimedSpace(string(out))
		switch args[0] {
		case "container":
			report.ContainersReclaimed = reclaimed
		case "image":
			report.ImagesReclaimed = reclaimed
		case "volume":
			report.VolumesReclaimed = reclaimed
		case "network":
			report.NetworksReclaimed = reclaimed
		case "builder":
			report.BuildCacheReclaimed = reclaimed
		}
	}
	return report, nil
}

func parseReclaimedSpace(output string) string {
	const marker = "Total reclaimed space:"
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, marker) {
			return strings.TrimSpace(strings.TrimPrefix(line, marker))
		}
	}
	return ""
}
```

Note: `context`, `fmt`, `os/exec`, and `strings` are already imported in `cleanup.go`.

Note: image prune deliberately does NOT use `-a` (all unused images). Dangling-only pruning leaves tagged images (including `tengiz-apps/*` rollback images referenced by stopped containers) untouched, preserving cold-start and rollback capability.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestCleanupCommands -v -count=1`

Expected: PASS.

- [ ] **Step 5: Add parser unit test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestParseReclaimedSpace(t *testing.T) {
	out := "Deleted Containers:\n4f6a1c\n2b8d90\n\nTotal reclaimed space: 1.234MB\n"
	got := parseReclaimedSpace(out)
	if got != "1.234MB" {
		t.Fatalf("parseReclaimedSpace() = %q, want %q", got, "1.234MB")
	}
}

func TestParseReclaimedSpaceEmpty(t *testing.T) {
	got := parseReclaimedSpace("no reclaimed space line")
	if got != "" {
		t.Fatalf("parseReclaimedSpace() = %q, want empty string", got)
	}
}
```

Run: `go test ./internal/runtime/ -run TestParseReclaimedSpace -v -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement Docker housekeeping prune commands"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupReport`, `runtime.NewDocker()` (from Tasks 1-2), `getEnv(cmd)`, `findSubcommand(rootCmd, name)`, `captureOutput(fn)` (existing CLI helpers)
- Produces: `var cleanupCmd *cobra.Command` registered on `rootCmd` with flags `--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--all`, `--force`, `--dry-run`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd := findSubcommand(rootCmd, "cleanup")
	if cmd == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupDryRunOutput(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		rootCmd.Execute()
	})
	for _, want := range []string{"containers", "images", "networks", "build cache"} {
		if !strings.Contains(output, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "volumes") {
		t.Errorf("dry-run should not include volumes by default:\n%s", output)
	}
}

func TestCleanupVolumesRequireForce(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--volumes"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --volumes passed without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should mention --force, got: %v", err)
	}
}

func TestCleanupAllDryRunIncludesVolumes(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--all", "--force", "--dry-run"})
	output := captureOutput(func() {
		rootCmd.Execute()
	})
	if !strings.Contains(output, "volumes") {
		t.Errorf("dry-run with --all should include volumes:\n%s", output)
	}
}

func TestCleanupHelpFlag(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--help"})
	output := captureOutput(func() {
		rootCmd.Execute()
	})
	for _, flag := range []string{"--containers", "--images", "--volumes", "--networks", "--build-cache", "--all", "--force", "--dry-run"} {
		if !strings.Contains(output, flag) {
			t.Errorf("help output missing flag %q", flag)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`

Expected: FAIL with `cleanup command not registered on rootCmd` (and `unknown command "cleanup"` errors).

- [ ] **Step 3: Write minimal implementation in `internal/cli/cleanup.go`**

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
	Long: `Prunes unused Docker resources: stopped containers, dangling images,
unused volumes, unused networks, and the build cache.

Tengiz-managed containers are protected via the tengiz-app label and are
never removed.

Volumes are only pruned when explicitly requested with --volumes (or --all)
and --force, because volume removal is destructive.

Use --dry-run to list what would be cleaned without touching Docker.

Examples:
  tengiz cleanup                          # containers, images, networks, build cache
  tengiz cleanup --dry-run                # preview only
  tengiz cleanup --volumes --force        # also prune unused volumes
  tengiz cleanup --all --force --dry-run  # preview full cleanup`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		all, _ := cmd.Flags().GetBool("all")
		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if all {
			containers, images, volumes, networks, buildCache = true, true, true, true, true
		}
		if !containers && !images && !volumes && !networks && !buildCache {
			containers, images, networks, buildCache = true, true, true, true
		}

		if volumes && !force {
			return fmt.Errorf("volume pruning requires --force (volumes hold persistent data)")
		}

		if dryRun {
			fmt.Println("[tengiz] dry-run: would clean")
			if containers {
				fmt.Println("  - stopped containers")
			}
			if images {
				fmt.Println("  - dangling images")
			}
			if volumes {
				fmt.Println("  - unused volumes")
			}
			if networks {
				fmt.Println("  - unused networks")
			}
			if buildCache {
				fmt.Println("  - build cache")
			}
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
		})
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] cleanup complete")
		if containers {
			fmt.Printf("  containers: %s\n", reclaimedOrDash(report.ContainersReclaimed))
		}
		if images {
			fmt.Printf("  images: %s\n", reclaimedOrDash(report.ImagesReclaimed))
		}
		if volumes {
			fmt.Printf("  volumes: %s\n", reclaimedOrDash(report.VolumesReclaimed))
		}
		if networks {
			fmt.Printf("  networks: %s\n", reclaimedOrDash(report.NetworksReclaimed))
		}
		if buildCache {
			fmt.Printf("  build cache: %s\n", reclaimedOrDash(report.BuildCacheReclaimed))
		}
		return nil
	},
}

func reclaimedOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (requires --force)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "clean all categories including volumes")
	cleanupCmd.Flags().Bool("force", false, "confirm destructive volume pruning")
	cleanupCmd.Flags().Bool("dry-run", false, "list what would be cleaned without touching Docker")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: the `tengiz cleanup` command surface from Task 3 (exact flags and semantics)

- [ ] **Step 1: Update README.md CLI Reference**

After the `tengiz rollback` section (around line 236), insert a new section:

````markdown
### `tengiz cleanup`

Prune unused Docker resources: stopped containers, dangling images, unused networks, and the build cache. Tengiz-managed containers are protected via the `tengiz-app` label and are never removed.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers |
| `--images` | Prune dangling images |
| `--volumes` | Prune unused volumes (requires `--force`) |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune the Docker build cache |
| `--all` | Clean all categories including volumes |
| `--force` | Confirm destructive volume pruning |
| `--dry-run` | List what would be cleaned without touching Docker |

With no flags, defaults to containers + images + networks + build cache. Volumes are never pruned unless explicitly enabled.

Examples:
```
tengiz cleanup
tengiz cleanup --dry-run
tengiz cleanup --volumes --force
tengiz cleanup --all --force
```
````

- [ ] **Step 2: Update AGENTS.md Commands section**

In the `## Commands` block, after the `tengiz ps` line (line 43), add:

```
tengiz cleanup [--all] [--force] [--dry-run] → prune unused Docker resources (volumes require --force)
```

- [ ] **Step 3: Verify docs render (no tests needed)**

Run: `go test ./... -v -count=1` and `go vet ./...`

Expected: ALL PASS, no vet issues.

- [ ] **Step 4: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**Spec coverage (feature #6 — Docker Housekeeping, "Label-based `docker system prune`. `tengiz cleanup`."):**
- Label-based protection of Tengiz containers → Task 1-2 (`--filter label!=tengiz-app` on container/image/network prunes)
- `tengiz cleanup` command → Task 3
- Per-category pruning (containers, images, volumes, networks, build cache) → Task 2 (`cleanupCommands`)
- Reclaimed-space reporting → Task 2 (`parseReclaimedSpace`), Task 3 (report printing)
- Feature rationale mentions periodic cleanup via `DockerCleanupJob` in the source repo — out of scope for this task (no scheduler exists yet; `idle`/`health` provide background timers as precedent). The command is the CLI-first deliverable.

**Placeholder scan:** No TBD/TODO; every step contains complete code and expected output.

**Type consistency:** `CleanupOptions` and `CleanupReport` fields are identical across Tasks 1, 2, 3 (`Containers/Images/Volumes/Networks/BuildCache` and `ContainersReclaimed/ImagesReclaimed/VolumesReclaimed/NetworksReclaimed/BuildCacheReclaimed`). `cleanupCommands` uses the same option names. Flag names match help output and `--volumes`-requires-`--force` guard in Task 3.