# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that frees disk space by pruning unused Docker containers, images, volumes, networks, and build cache — while protecting Tengiz-managed containers (labeled `tengiz-app`) and images (`tengiz-apps/*`) from deletion.

**Architecture:** A new `Prune(ctx, opts)` method on the `runtime.Manager` interface (matching the existing `RemoveImage`/`KeepLastNImages` pattern in `internal/runtime/cleanup.go`) executes category-specific Docker prune subcommands built by a pure, unit-testable helper `buildPruneCommands`. Instead of a single `docker system prune` (which has no way to protect tagged rollback images), each category uses its own safe command: containers are filtered with `label!=tengiz-app`; aggressive image pruning excludes `tengiz-apps/*` via a negative `reference` filter; volumes/networks/build-cache use their standard prune commands. `--dry-run` runs `docker system df` and lists the commands that *would* run without deleting anything. A new CLI `cleanupCmd` parses category flags (default: all) and prints the report.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface, `os/exec` Docker CLI invocation (no Docker SDK). No new external dependencies.

## Global Constraints

- Tengiz-managed containers are identified by the `tengiz-app` label (const `labelKey` in `internal/runtime/docker.go`) and must never be pruned by cleanup
- Tengiz images live under the `tengiz-apps/` repository prefix (see `internal/builder/builder.go:61`) and must be excluded from aggressive image pruning
- Default behavior with no category flags cleans all five categories (containers, images, volumes, networks, build cache)
- `--all-images` without `--images` is a user error and must return an error
- `--dry-run` never executes any prune command; it only runs `docker system df` for a disk-usage snapshot
- Every commit must leave `go build ./...` and `go test ./...` green
- No new external dependencies; Docker must be installed separately (already a runtime requirement)
- README.md must be updated (AGENTS.md rule: UI/UX changes require documentation updates)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions` + `PruneReport` types; add `Prune` to `Manager` interface; stub impl |
| `internal/runtime/cleanup.go` | `dockerRuntime.Prune` implementation + pure helpers `buildPruneCommands`, `parseReclaimedSpace` |
| `internal/runtime/cleanup_test.go` | Tests: stub `Prune`, `buildPruneCommands`, `parseReclaimedSpace` |
| `internal/cli/root.go` | New `cleanupCmd` cobra command, `registerCleanupFlags`, `cleanupOptionsFromFlags`, registration in `init()` |
| `internal/cli/cleanup_test.go` | New file — CLI command/flag/options tests |
| `internal/cli/root_test.go` | Add `Prune` method to `mockRTForDeploy` (interface conformance) |
| `internal/idle/idle_test.go` | Add `Prune` method to `mockRuntime` (interface conformance) |
| `internal/proxy/proxy_test.go` | Add `Prune` method to `mockRuntime` (interface conformance) |
| `README.md` | Document `tengiz cleanup` in CLI Reference + add feature bullet |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Add `Prune` to the `runtime.Manager` interface

**Files:**
- Modify: `internal/runtime/runtime.go` — add `CleanupOptions`, `PruneReport` types, `Prune` to `Manager` interface, stub implementation
- Test: `internal/runtime/cleanup_test.go`
- Modify: `internal/cli/root_test.go:98-99` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:32-33` — add `Prune` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:33-34` — add `Prune` to `mockRuntime`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions`, `runtime.PruneReport`, and `Manager.Prune(ctx context.Context, opts CleanupOptions) (*PruneReport, error)` — used by Tasks 2 and 3

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report == nil {
		t.Fatal("Prune() returned nil report")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: FAIL — does not compile, `m.Prune undefined` / `CleanupOptions undefined`

- [ ] **Step 3: Add the types and interface method in `internal/runtime/runtime.go`**

After the `RunOptions` struct (around line 29), add:

```go
type CleanupOptions struct {
	DryRun     bool
	Containers bool
	Images     bool
	AllImages  bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type PruneReport struct {
	Commands       []string
	Details        []string
	ReclaimedSpace string
}
```

In the `Manager` interface (after the `KeepLastNImages` line, line 36), add:

```go
	Prune(ctx context.Context, opts CleanupOptions) (*PruneReport, error)
```

- [ ] **Step 4: Add the stub implementation in `internal/runtime/runtime.go`**

After the stub `KeepLastNImages` method (line 118), add:

```go
func (m *stubManager) Prune(ctx context.Context, opts CleanupOptions) (*PruneReport, error) {
	return &PruneReport{}, nil
}
```

- [ ] **Step 5: Update the three mock types that implement `Manager`**

In `internal/cli/root_test.go`, after the `KeepLastNImages` method (line 99), add:

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.CleanupOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{}, nil
}
```

In `internal/idle/idle_test.go`, after the `KeepLastNImages` method (line 33), add:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.CleanupOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{}, nil
}
```

In `internal/proxy/proxy_test.go`, after the `KeepLastNImages` method (line 34), add:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.CleanupOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`
Expected: PASS

Run: `go test ./internal/cli/... ./internal/idle/... ./internal/proxy/... ./internal/preview/... ./internal/health/... -count=1`
Expected: PASS (proxy package tests may take ~2s each due to TCP dial timeouts)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Prune method to runtime.Manager interface for docker housekeeping"
```

---

### Task 2: Implement `dockerRuntime.Prune` with pure arg builders

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `buildPruneCommands`, `parseReclaimedSpace`, `Prune` method
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.PruneReport`, `Manager.Prune` from Task 1
- Produces: `buildPruneCommands(opts CleanupOptions) []string`, `parseReclaimedSpace(output string) string`, and the real `dockerRuntime.Prune` implementation — consumed by Task 3's CLI handler

- [ ] **Step 1: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
func TestBuildPruneCommands(t *testing.T) {
	tests := []struct {
		name     string
		opts     CleanupOptions
		expected []string
	}{
		{
			name:     "no categories enabled",
			opts:     CleanupOptions{},
			expected: nil,
		},
		{
			name:     "containers only",
			opts:     CleanupOptions{Containers: true},
			expected: []string{"docker container prune -f --filter label!=tengiz-app"},
		},
		{
			name:     "images dangling only",
			opts:     CleanupOptions{Images: true},
			expected: []string{"docker image prune -f"},
		},
		{
			name:     "images all unused",
			opts:     CleanupOptions{Images: true, AllImages: true},
			expected: []string{"docker image prune -a -f --filter reference!=tengiz-apps/*"},
		},
		{
			name: "all categories",
			opts: CleanupOptions{
				Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true,
			},
			expected: []string{
				"docker container prune -f --filter label!=tengiz-app",
				"docker image prune -f",
				"docker volume prune -f",
				"docker network prune -f",
				"docker builder prune -f",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPruneCommands(tt.opts)
			if len(got) != len(tt.expected) {
				t.Fatalf("buildPruneCommands() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("buildPruneCommands()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	output := "Deleted Containers:\nfoo\n\nTotal reclaimed space: 1.234GB\n"
	if got := parseReclaimedSpace(output); got != "1.234GB" {
		t.Errorf("parseReclaimedSpace() = %q, want %q", got, "1.234GB")
	}
}

func TestParseReclaimedSpaceNone(t *testing.T) {
	if got := parseReclaimedSpace("nothing to do\n"); got != "" {
		t.Errorf("parseReclaimedSpace() = %q, want empty string", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestBuildPruneCommands|TestParseReclaimedSpace" -v -count=1`

Expected: FAIL — does not compile, `buildPruneCommands undefined`, `parseReclaimedSpace undefined`

- [ ] **Step 3: Implement the pure helpers and `Prune` in `internal/runtime/cleanup.go`**

Append to `internal/runtime/cleanup.go` (the existing imports `context`, `fmt`, `os/exec`, `strings` are already present):

```go
func buildPruneCommands(opts CleanupOptions) []string {
	var cmds []string
	if opts.Containers {
		cmds = append(cmds, "docker container prune -f --filter label!=tengiz-app")
	}
	if opts.Images {
		if opts.AllImages {
			cmds = append(cmds, "docker image prune -a -f --filter reference!=tengiz-apps/*")
		} else {
			cmds = append(cmds, "docker image prune -f")
		}
	}
	if opts.Volumes {
		cmds = append(cmds, "docker volume prune -f")
	}
	if opts.Networks {
		cmds = append(cmds, "docker network prune -f")
	}
	if opts.BuildCache {
		cmds = append(cmds, "docker builder prune -f")
	}
	return cmds
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
	}
	return ""
}

func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (*PruneReport, error) {
	report := &PruneReport{Commands: buildPruneCommands(opts)}

	if opts.DryRun {
		dfCmd := exec.CommandContext(ctx, "docker", "system", "df")
		out, err := dfCmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		report.Details = append(report.Details, string(out))
		return report, nil
	}

	for _, cmdStr := range report.Commands {
		parts := strings.Split(cmdStr, " ")
		cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("%s: %w\n%s", cmdStr, err, string(out))
		}
		report.Details = append(report.Details, string(out))
		if reclaimed := parseReclaimedSpace(string(out)); reclaimed != "" {
			if report.ReclaimedSpace != "" {
				report.ReclaimedSpace += ", "
			}
			report.ReclaimedSpace += reclaimed
		}
	}
	return report, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestBuildPruneCommands|TestParseReclaimedSpace|TestStubPrune" -v -count=1`
Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement runtime Prune with label-based docker housekeeping commands"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` after `buildLogsCmd`, add `registerCleanupFlags` + `cleanupOptionsFromFlags` helpers, register in `init()`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.PruneReport`, `runtime.NewDocker()`, `Manager.Prune` from Tasks 1-2
- Produces: `registerCleanupFlags(cmd *cobra.Command)`, `cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error)`, and the working `tengiz cleanup` command

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func newTestCleanupCmd() *cobra.Command {
	c := &cobra.Command{Use: "cleanup"}
	registerCleanupFlags(c)
	return c
}

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	for _, flag := range []string{"dry-run", "containers", "images", "all-images", "volumes", "networks", "build-cache"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupOptionsFromFlagsDefaultAll(t *testing.T) {
	c := newTestCleanupCmd()
	c.ParseFlags([]string{})
	opts, err := cleanupOptionsFromFlags(c)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("expected all categories when no flags given, got %+v", opts)
	}
	if opts.AllImages {
		t.Error("AllImages should be false by default")
	}
	if opts.DryRun {
		t.Error("DryRun should be false by default")
	}
}

func TestCleanupOptionsFromFlagsSingleCategory(t *testing.T) {
	c := newTestCleanupCmd()
	c.ParseFlags([]string{"--volumes", "--dry-run"})
	opts, err := cleanupOptionsFromFlags(c)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.Volumes {
		t.Error("Volumes should be true")
	}
	if !opts.DryRun {
		t.Error("DryRun should be true")
	}
	if opts.Containers || opts.Images || opts.Networks || opts.BuildCache {
		t.Errorf("expected only Volumes enabled, got %+v", opts)
	}
}

func TestCleanupOptionsFromFlagsAllImagesRequiresImages(t *testing.T) {
	c := newTestCleanupCmd()
	c.ParseFlags([]string{"--all-images"})
	if _, err := cleanupOptionsFromFlags(c); err == nil {
		t.Error("expected error when --all-images used without --images")
	}
}

func TestCleanupOptionsFromFlagsImagesAndAllImages(t *testing.T) {
	c := newTestCleanupCmd()
	c.ParseFlags([]string{"--images", "--all-images"})
	opts, err := cleanupOptionsFromFlags(c)
	if err != nil {
		t.Fatalf("cleanupOptionsFromFlags() error = %v", err)
	}
	if !opts.Images || !opts.AllImages {
		t.Errorf("expected Images and AllImages true, got %+v", opts)
	}
	if opts.Containers {
		t.Error("Containers should stay false")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — does not compile, `cleanupCmd undefined`, `registerCleanupFlags undefined`, `cleanupOptionsFromFlags undefined`

- [ ] **Step 3: Add the `cleanupCmd` cobra command**

In `internal/cli/root.go`, after the `buildLogsCmd` definition (ends at line 1090), add:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Remove unused Docker containers, images, volumes, networks, and build cache.
Tengiz-managed containers (labeled tengiz-app) and images (tengiz-apps/*) are protected.

With no category flags, all categories are cleaned. Use flags to select specific categories.

  --containers   remove stopped containers not managed by Tengiz
  --images       remove dangling (untagged) images
  --all-images   with --images, also remove all unused images (tengiz-apps/* are kept)
  --volumes      remove unused anonymous volumes
  --networks     remove unused networks
  --build-cache  remove Docker build cache
  --dry-run      show what would be cleaned and current disk usage without deleting anything`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := cleanupOptionsFromFlags(cmd)
		if err != nil {
			return err
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return err
		}

		if opts.DryRun {
			fmt.Println("[tengiz] dry-run: no resources were deleted")
			for _, c := range report.Commands {
				fmt.Printf("[tengiz] would run: %s\n", c)
			}
			for _, d := range report.Details {
				fmt.Print(d)
			}
			return nil
		}

		for _, c := range report.Commands {
			fmt.Printf("[tengiz] ran: %s\n", c)
		}
		for _, d := range report.Details {
			fmt.Print(d)
		}
		fmt.Printf("[tengiz] cleanup complete (reclaimed: %s)\n", report.ReclaimedSpace)
		return nil
	},
}
```

- [ ] **Step 4: Add the flag registration and options helpers**

In `internal/cli/root.go`, add these functions near `cleanupCmd` (package level):

```go
func registerCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("dry-run", false, "show what would be cleaned without deleting anything")
	cmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cmd.Flags().Bool("images", false, "remove dangling (untagged) images")
	cmd.Flags().Bool("all-images", false, "with --images, also remove all unused images (tengiz-apps/* are kept)")
	cmd.Flags().Bool("volumes", false, "remove unused anonymous volumes")
	cmd.Flags().Bool("networks", false, "remove unused networks")
	cmd.Flags().Bool("build-cache", false, "remove Docker build cache")
}

func cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error) {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	allImages, _ := cmd.Flags().GetBool("all-images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")

	if allImages && !images {
		return runtime.CleanupOptions{}, fmt.Errorf("--all-images requires --images")
	}

	opts := runtime.CleanupOptions{
		DryRun:     dryRun,
		Containers: containers,
		Images:     images,
		AllImages:  allImages,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
	}

	if !containers && !images && !volumes && !networks && !buildCache {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts, nil
}
```

- [ ] **Step 5: Register the command in `init()`**

In `internal/cli/root.go`, inside `init()` (after `rootCmd.AddCommand(runCmd)` at line 67), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	registerCleanupFlags(cleanupCmd)
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`
Expected: PASS

- [ ] **Step 7: Build and run the full test suite**

Run: `go build ./...`
Expected: Build succeeds

Run: `go test ./internal/cli/... -v -count=1`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Documentation, feature tracking, and final verification

**Files:**
- Modify: `README.md` — add feature bullet (line ~23, after the health check bullet) and a `### tengiz cleanup` section after the `### tengiz rm <app>` section (line 228)
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented (P0 table row 6 + Implemented Features table)

- [ ] **Step 1: Add the feature bullet to `README.md`**

In the Features list (after line 21, the health check bullet), add:

```markdown
- **Docker housekeeping** — One-command `tengiz cleanup` prunes unused containers, images, volumes, networks, and build cache while always protecting Tengiz-managed resources.
```

- [ ] **Step 2: Add the `tengiz cleanup` section to `README.md`**

After the `### tengiz rm <app>` section (ends line 228, before `### tengiz rollback <app>`), insert:

```markdown
### `tengiz cleanup`

Clean up unused Docker resources to free disk space. Stopped containers managed by Tengiz (labeled `tengiz-app`) and Tengiz images (`tengiz-apps/*`) are always protected.

With no category flags, all categories are cleaned. Use flags to select specific categories.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling (untagged) images |
| `--all-images` | With `--images`, also remove all unused images (`tengiz-apps/*` are kept) |
| `--volumes` | Remove unused anonymous volumes |
| `--networks` | Remove unused networks |
| `--build-cache` | Remove Docker build cache |
| `--dry-run` | Show what would be cleaned and current disk usage without deleting anything |

Examples:
```
tengiz cleanup                    # prune everything safely
tengiz cleanup --dry-run          # preview what would be removed
tengiz cleanup --containers --volumes
tengiz cleanup --images --all-images
```
```

- [ ] **Step 3: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table (row 6, line 19), change:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the Implemented Features table (after the last row, line 253), add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-18) |
```

- [ ] **Step 4: Run the full test suite and static analysis**

Run: `go build -o tengiz .`
Expected: Build succeeds

Run: `go test ./... -v -count=1`
Expected: All PASS (proxy package tests are slow ~2s each; idle tests are time-sensitive)

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 5: Self-review against the spec**

Check the plan against the feature requirements from `docs/FUTURES_FEATURES.md`:
- `tengiz cleanup` command ✅ (Task 3 — `cleanupCmd`)
- Label-based protection of Tengiz containers ✅ (Task 2 — `--filter label!=tengiz-app`)
- Protect Tengiz rollback images ✅ (Task 2 — `--filter reference!=tengiz-apps/*` on aggressive image prune)
- Clean unused volumes/networks/build cache ✅ (Task 2 — per-category prune commands)
- Dry-run preview ✅ (Task 2/3 — `docker system df` + command listing, nothing deleted)
- README + feature tracking updated ✅ (Task 4)

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark docker housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage**
- Feature #6 (Docker Housekeeping): `tengiz cleanup` command (Task 3), label-based protection of Tengiz containers via `label!=tengiz-app` (Task 2), protection of rollback images via `reference!=tengiz-apps/*` (Task 2), volume/network/build-cache pruning (Task 2), README docs + FUTURES tracking (Task 4). Fully covered.
- The feature's "label-based `docker system prune`" is implemented as per-category prune commands instead of a single `docker system prune` — this is intentional and safer (see Architecture) and also satisfies the granular-prune needs of feature #56 without adding scope.

**2. Placeholder scan**
- Every step contains complete code or exact commands. No "TBD", "TODO", "add error handling", "similar to Task N", or undefined references. Tests include full expected values.

**3. Type consistency**
- `runtime.CleanupOptions` fields (`DryRun`, `Containers`, `Images`, `AllImages`, `Volumes`, `Networks`, `BuildCache`) are defined in Task 1 and used identically in Tasks 2 and 3.
- `Manager.Prune(ctx, opts) (*PruneReport, error)` signature is consistent across the interface (Task 1), dockerRuntime impl (Task 2), stub, and all three mocks.
- `buildPruneCommands(opts CleanupOptions) []string` and `parseReclaimedSpace(output string) string` are defined and used only in Task 2.
- `registerCleanupFlags(cmd *cobra.Command)` and `cleanupOptionsFromFlags(cmd *cobra.Command) (runtime.CleanupOptions, error)` are defined in Task 3 and used by the handler and tests.
- The image filter string `reference!=tengiz-apps/*` matches the actual image tag format `tengiz-apps/<name>:<env>-<deploymentID>` produced by `internal/builder/builder.go`.
