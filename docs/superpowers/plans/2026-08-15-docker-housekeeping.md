# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that label-scoped Docker pruning reclaims disk space on single-server deployments while never touching Tengiz-managed containers, images, or volumes.

**Architecture:** A new `runtime.Manager.Cleanup(ctx, opts)` method shells out to per-category `docker ... prune` commands, each protected by a `label!=tengiz-app` filter (containers/networks) or a `reference!=tengiz-apps/*` filter (images). A pure `parseReclaimedSpace` helper extracts `Total reclaimed space:` bytes from each command's output; the CLI sums them for a human-readable report. `--dry-run` runs `docker system df` instead of pruning. The CLI command lives in a new `internal/cli/cleanup.go` file, matching the existing pattern of splitting command families out of `root.go` (like `preview.go`).

**Tech Stack:** Go 1.26 stdlib only (`os/exec`, `context`, `regexp`, `strconv`, `strings`, `fmt`), Cobra (CLI), existing `runtime.Manager` interface. No new external dependencies.

## Global Constraints

- Disk safety: never prune any resource with the `tengiz-app` label or the `tengiz-apps/` image repository prefix
- No new external dependencies — stdlib only
- `runtime.Manager` interface stays the single seam the CLI talks to; `NewDocker()` still returns `Manager`
- Default (safe) prune set is always: stopped containers `label!=tengiz-app`, dangling images, networks `label!=tengiz-app`, build cache
- Destructive categories are opt-in flags only: `--all-images` (adds `-a` to image prune with `reference!=tengiz-apps/*`), `--volumes`
- `--dry-run` never mutates Docker state — it only runs `docker system df`
- Container names, image tag formats unchanged: images are `tengiz-apps/<name>:<env>-<deploymentID>` and `tengiz-apps/<name>:<env>-latest` (from `internal/builder/builder.go:61`)
- Existing tests must continue to pass; `stubManager` and the CLI test mock `mockRTForDeploy` must gain the new interface method
- Commit messages use the repo's conventional style (`feat:`, `test:`)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` | **Create.** `CleanupTarget` enum, `CleanupOptions`, `CleanupStats`, `cleanupCommand()` (pure docker-args builder), `parseReclaimedSpace()` (pure output parser), `dockerRuntime.Cleanup()` |
| `internal/runtime/prune_test.go` | **Create.** Unit tests for `parseReclaimedSpace`, `cleanupCommand`, and the stub `Cleanup` |
| `internal/runtime/runtime.go` | **Modify.** Add `Cleanup` to `Manager` interface + stub implementation on `stubManager` |
| `internal/cli/cleanup.go` | **Create.** `cleanupCmd` Cobra command, flags, `formatBytes()` helper, registration helper |
| `internal/cli/cleanup_test.go` | **Create.** Command registration, flag parsing, `formatBytes` tests |
| `internal/cli/root_test.go` | **Modify.** Add `Cleanup` method to `mockRTForDeploy` so the package still compiles |
| `internal/cli/root.go` | **Modify.** Register `cleanupCmd` and its flags in `init()` |
| `README.md` | **Modify.** Add `### tengiz cleanup` section to CLI Reference |
| `AGENTS.md` | **Modify.** Add `tengiz cleanup` line to CLI list |
| `docs/FUTURES_FEATURES.md` | **Modify.** Move feature #1 to the Implemented table |

---

### Task 1: Runtime output parser — `parseReclaimedSpace`

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `func parseReclaimedSpace(output string) (uint64, bool)` — returns reclaimed bytes and `true` when the output contains a `Total reclaimed space: <N><unit>` line (last match wins; `false` if absent or unparsable). Units: `B`=1, `kB`/`KB`=1e3, `MB`=1e6, `GB`=1e9, `TB`=1e12, `KiB`=1<<10, `MiB`=1<<20, `GiB`=1<<30, `TiB`=1<<40.

- [ ] **Step 1: Write the failing test**

```go
package runtime

import "testing"

func TestParseReclaimedSpace(t *testing.T) {
	tests := []struct {
		output   string
		want     uint64
		wantOK   bool
	}{
		{"Total reclaimed space: 0B\n", 0, true},
		{"Total reclaimed space: 512B\n", 512, true},
		{"Total reclaimed space: 1.2kB\n", 1200, true},
		{"Total reclaimed space: 12.31MB\n", 12310000, true},
		{"Total reclaimed space: 1.2GB\n", 1200000000, true},
		{"Total reclaimed space: 3MiB\n", 3 << 20, true},
		{"no matches here\n", 0, false},
		{"", 0, false},
	}
	for _, tc := range tests {
		got, ok := parseReclaimedSpace(tc.output)
		if ok != tc.wantOK || got != tc.want {
			t.Errorf("parseReclaimedSpace(%q) = (%d, %v), want (%d, %v)", tc.output, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestParseReclaimedSpaceLastMatchWins(t *testing.T) {
	out := "Deleted Images:\nuntagged: foo\n\nTotal reclaimed space: 1.2kB\n"
	got, ok := parseReclaimedSpace(out)
	if !ok || got != 1200 {
		t.Fatalf("got (%d, %v), want (1200, true)", got, ok)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestParseReclaimedSpace -v -count=1`
Expected: FAIL — `undefined: parseReclaimedSpace`

- [ ] **Step 3: Write minimal implementation**

```go
package runtime

import (
	"regexp"
	"strconv"
)

var reclaimedSpaceRe = regexp.MustCompile(`Total reclaimed space:\s*([0-9]+(?:\.[0-9]+)?)\s*([A-Za-z]+)`)

var sizeFactor = map[string]float64{
	"B":   1,
	"kB":  1e3,
	"KB":  1e3,
	"MB":  1e6,
	"GB":  1e9,
	"TB":  1e12,
	"KiB": 1 << 10,
	"MiB": 1 << 20,
	"GiB": 1 << 30,
	"TiB": 1 << 40,
}

func parseReclaimedSpace(output string) (uint64, bool) {
	matches := reclaimedSpaceRe.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return 0, false
	}
	m := matches[len(matches)-1]
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	factor, ok := sizeFactor[m[2]]
	if !ok {
		return 0, false
	}
	return uint64(val * factor), true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestParseReclaimedSpace -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: add parseReclaimedSpace helper for docker prune output"
```

---

### Task 2: Runtime cleanup types + docker-args builder

**Files:**
- Modify: `internal/runtime/prune.go` (append)
- Test: `internal/runtime/prune_test.go` (append)

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type CleanupTarget string` with constants `TargetContainers = "containers"`, `TargetImages = "images"`, `TargetVolumes = "volumes"`, `TargetNetworks = "networks"`, `TargetBuilder = "builder"`
  - `type CleanupOptions struct { DryRun bool; PruneAllImages bool; PruneVolumes bool }`
  - `type CleanupStats struct { SpaceReclaimed uint64; Detail string }`
  - `func cleanupCommand(t CleanupTarget, pruneAllImages bool) []string` — exact docker args for a category

- [ ] **Step 1: Write the failing test**

```go
package runtime

import (
	"reflect"
	"testing"
)

func TestCleanupCommand(t *testing.T) {
	tests := []struct {
		target         CleanupTarget
		pruneAllImages bool
		want           []string
	}{
		{TargetContainers, false, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{TargetImages, false, []string{"image", "prune", "-f"}},
		{TargetImages, true, []string{"image", "prune", "-f", "-a", "--filter", "reference!=tengiz-apps/*"}},
		{TargetVolumes, false, []string{"volume", "prune", "-f"}},
		{TargetNetworks, false, []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{TargetBuilder, false, []string{"builder", "prune", "-f"}},
		{CleanupTarget("unknown"), false, nil},
	}
	for _, tc := range tests {
		got := cleanupCommand(tc.target, tc.pruneAllImages)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("cleanupCommand(%q, %v) = %v, want %v", tc.target, tc.pruneAllImages, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestCleanupCommand -v -count=1`
Expected: FAIL — `undefined: CleanupTarget` / `undefined: cleanupCommand`

- [ ] **Step 3: Write minimal implementation**

```go
package runtime

import (
	"regexp"
	"strconv"
)

type CleanupTarget string

const (
	TargetContainers CleanupTarget = "containers"
	TargetImages     CleanupTarget = "images"
	TargetVolumes    CleanupTarget = "volumes"
	TargetNetworks   CleanupTarget = "networks"
	TargetBuilder    CleanupTarget = "builder"
)

type CleanupOptions struct {
	DryRun         bool
	PruneAllImages bool
	PruneVolumes   bool
}

type CleanupStats struct {
	SpaceReclaimed uint64
	Detail         string
}

func cleanupCommand(t CleanupTarget, pruneAllImages bool) []string {
	switch t {
	case TargetContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case TargetImages:
		args := []string{"image", "prune", "-f"}
		if pruneAllImages {
			args = append(args, "-a", "--filter", "reference!=tengiz-apps/*")
		}
		return args
	case TargetVolumes:
		return []string{"volume", "prune", "-f"}
	case TargetNetworks:
		return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	case TargetBuilder:
		return []string{"builder", "prune", "-f"}
	default:
		return nil
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestCleanupCommand -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: add cleanup target types and docker args builder"
```

---

### Task 3: Runtime `Manager.Cleanup` — interface, stub, docker implementation

**Files:**
- Modify: `internal/runtime/runtime.go` — add `Cleanup` to `Manager` interface + `stubManager.Cleanup`
- Modify: `internal/runtime/prune.go` — add `dockerRuntime.Cleanup`
- Modify: `internal/cli/root_test.go:69-100` — add `Cleanup` to `mockRTForDeploy`
- Test: `internal/runtime/prune_test.go` (append stub test)

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupStats`, `cleanupCommand`, `parseReclaimedSpace` from Tasks 1–2
- Produces: `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupStats, error)` on the `runtime.Manager` interface (so the CLI can call it on a `runtime.NewDocker()` value). Later tasks depend on this exact signature.

- [ ] **Step 1: Write the failing test**

```go
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	stats, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if stats.SpaceReclaimed != 0 || stats.Detail != "" {
		t.Fatalf("Cleanup() = %+v, want zero-value stats", stats)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: FAIL — `stubManager does not implement Manager (missing method Cleanup)` (the package will not compile)

- [ ] **Step 3: Write minimal implementation**

Add to `internal/runtime/runtime.go` — interface declaration (insert after the `Run` line, inside `Manager`):

```go
type Manager interface {
	// ...existing methods unchanged...
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupStats, error)
}
```

Add to `internal/runtime/runtime.go` — stub implementation (after `stubManager.Run`):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupStats, error) {
	return CleanupStats{}, nil
}
```

Add to `internal/runtime/prune.go` — the docker implementation (append at end of file):

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupStats, error) {
	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "system", "df")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return CleanupStats{}, fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		return CleanupStats{Detail: string(out)}, nil
	}

	targets := []CleanupTarget{TargetContainers, TargetImages, TargetNetworks, TargetBuilder}
	if opts.PruneVolumes {
		targets = append(targets, TargetVolumes)
	}

	var stats CleanupStats
	var detail strings.Builder
	for _, t := range targets {
		args := cleanupCommand(t, opts.PruneAllImages)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return CleanupStats{}, fmt.Errorf("docker %s prune: %w\n%s", t, err, string(out))
		}
		if n, ok := parseReclaimedSpace(string(out)); ok {
			stats.SpaceReclaimed += n
		}
		detail.Write(out)
	}
	stats.Detail = detail.String()
	return stats, nil
}
```

Update `internal/runtime/prune.go` imports to include `"context"`, `"fmt"`, `"os/exec"`, `"strings"`:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)
```

Add to `internal/cli/root_test.go` — inside `mockRTForDeploy` (after its `Run` method, line 100):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupStats, error) {
	return runtime.CleanupStats{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`
Expected: PASS

Then run the full suite to confirm the interface change compiles everywhere:
Run: `go build ./... && go vet ./...`
Expected: no output (build + vet succeed)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune.go internal/runtime/prune_test.go internal/cli/root_test.go
git commit -m "feat: add Cleanup to runtime.Manager with label-scoped docker prune"
```

---

### Task 4: CLI `tengiz cleanup` command + flags + report formatting

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:38-45` — register `cleanupCmd` and flags in `init()`
- Test: `internal/cli/cleanup_test.go` (create)

**Interfaces:**
- Consumes: `runtime.NewDocker() Manager`, `Manager.Cleanup(ctx, CleanupOptions) (CleanupStats, error)` from Task 3
- Produces: `cleanupCmd *cobra.Command` (registered on `rootCmd`), `func formatBytes(n uint64) string` (decimal units matching docker output: `B`, `kB`, `MB`, `GB`...)

- [ ] **Step 1: Write the failing test**

```go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "all-images", "volumes"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupCmdForwardsOptions(t *testing.T) {
	var gotOpts runtime.CleanupOptions
	var gotDryRun bool
	original := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = original }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		gotDryRun, _ = cmd.Flags().GetBool("dry-run")
		allImages, _ := cmd.Flags().GetBool("all-images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		gotOpts = runtime.CleanupOptions{
			DryRun:         gotDryRun,
			PruneAllImages: allImages,
			PruneVolumes:   volumes,
		}
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all-images", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !gotDryRun || !gotOpts.PruneAllImages || !gotOpts.PruneVolumes {
		t.Fatalf("options = %+v, want DryRun=true PruneAllImages=true PruneVolumes=true", gotOpts)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		n    uint64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1200, "1.2kB"},
		{12310000, "12.3MB"},
		{1200000000, "1.2GB"},
	}
	for _, tc := range tests {
		if got := formatBytes(tc.n); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
```

Note: `TestCleanupCmdForwardsOptions` needs the `"github.com/spf13/cobra"` import — it is already imported in `root_test.go`, but to keep `cleanup_test.go` self-contained add it there. For the same reason the check `if got := formatBytes(tc.n); got != tc.want` uses `strings` import only if needed (it does not in this test — omit `strings` from the test file's imports).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestFormatBytes' -v -count=1`
Expected: FAIL — `cleanupCmd` undefined, `formatBytes` undefined

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

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
	Short: "Prune Docker resources (containers, images, networks, build cache)",
	Long: `Prunes Docker resources that Tengiz no longer needs while protecting all
Tengiz-managed containers and images via labels.

Always prunes (safe defaults):
  - stopped containers NOT labeled tengiz-app
  - dangling build images
  - unused networks NOT labeled tengiz-app
  - build cache

Opt-in (destructive, use with care):
  --all-images  also prune all unused images outside tengiz-apps/*
  --volumes     also prune unused Docker volumes

Use --dry-run to show current disk usage without pruning anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		allImages, _ := cmd.Flags().GetBool("all-images")
		volumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		stats, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			DryRun:         dryRun,
			PruneAllImages: allImages,
			PruneVolumes:   volumes,
		})
		if err != nil {
			return err
		}

		if out := strings.TrimSpace(stats.Detail); out != "" {
			fmt.Println(out)
		}
		if dryRun {
			fmt.Println("[tengiz] run 'tengiz cleanup' to prune the safe categories (add --all-images/--volumes to extend)")
			return nil
		}
		fmt.Printf("[tengiz] cleanup complete: reclaimed %s\n", formatBytes(stats.SpaceReclaimed))
		return nil
	},
}

var sizeUnits = []string{"B", "kB", "MB", "GB", "TB", "PB", "EB"}

func formatBytes(n uint64) string {
	if n < 1000 {
		return fmt.Sprintf("%dB", n)
	}
	val := float64(n)
	idx := 0
	for val >= 1000 && idx < len(sizeUnits)-1 {
		val /= 1000
		idx++
	}
	return fmt.Sprintf("%.1f%s", val, sizeUnits[idx])
}
```

Register in `internal/cli/root.go` `init()` (after `rootCmd.AddCommand(volumeCmd)`, i.e. after line 64):

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "show current disk usage without pruning anything")
	cleanupCmd.Flags().Bool("all-images", false, "also prune all unused images outside tengiz-apps/* (destructive)")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused Docker volumes (destructive, opt-in)")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestCleanup|TestFormatBytes' -v -count=1`
Expected: PASS

Then run the full suite:
Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command with label-scoped docker prune"
```

---

### Task 5: Documentation — README, AGENTS.md, FUTURES_FEATURES.md

**Files:**
- Modify: `README.md` — add cleanup section to CLI Reference
- Modify: `AGENTS.md` — add command to CLI list
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #1 implemented

**Interfaces:**
- Consumes: nothing (docs reflect the Task 4 command surface)
- Produces: nothing code-wise; keeps repo docs in sync per AGENTS.md rules

- [ ] **Step 1: Write the failing test (docs are not tested — skip to implementation)**

No automated test applies to documentation. Proceed directly.

- [ ] **Step 2: (skipped)**

- [ ] **Step 3: Update the docs**

Insert the following section into `README.md` between the `### tengiz rm <app>` section (ends line 229) and the `### tengiz rollback <app>` section (starts line 230):

```markdown
### `tengiz cleanup`

Prune Docker resources that Tengiz no longer needs. Protects all Tengiz-managed containers and images via labels, so running apps, cold-start candidates, and the last N deployment images are never touched.

Always prunes:

- Stopped containers **not** labeled `tengiz-app`
- Dangling build images
- Unused networks **not** labeled `tengiz-app`
- Build cache

| Flag | Description |
|------|-------------|
| `--dry-run` | Show current disk usage (`docker system df`) without pruning anything |
| `--all-images` | Also prune all unused images outside `tengiz-apps/*` (destructive) |
| `--volumes` | Also prune unused Docker volumes (destructive, opt-in) |

Example:

```
tengiz cleanup            # prune safe categories, prints reclaimed space
tengiz cleanup --dry-run  # preview disk usage first
tengiz cleanup --volumes  # also prune unused volumes
```
```

Add one line to `AGENTS.md` in the CLI list, directly after the `tengiz rollback <app> → rollback to previous deployment` line:

```markdown
tengiz cleanup           → prune Docker resources (label-scoped; --dry-run/--all-images/--volumes)
```

Update `docs/FUTURES_FEATURES.md`:
1. Delete the row `| 1 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk alanı single-server deploy'ların #1 üretim sorunu. Label-based \`docker system prune\`. \`tengiz cleanup\`. |` from the P0 table (line 16).
2. Renumber the remaining P0 rows from 2–16 to 1–15 (change only the first cell of each row).
3. Add a row to the "✅ Implemented Features (Not Pending)" table (after the first `| — | **Rollback Sistemi** ...` row, line 283):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-15) |
```

- [ ] **Step 4: Verify the docs render correctly**

Run: `grep -n "tengiz cleanup" README.md AGENTS.md`
Expected: matches in both files
Run: `grep -n "Docker Housekeeping" docs/FUTURES_FEATURES.md`
Expected: exactly one match (in the Implemented table)

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage (from FUTURES_FEATURES.md #1):**
- "Label-based `docker system prune`" → Task 3 `cleanupCommand()` uses `label!=tengiz-app` filters on `docker container prune` / `docker network prune` and `reference!=tengiz-apps/*` on image prune. ✅
- "`tengiz cleanup`" → Task 4 `cleanupCmd`. ✅
- "Disk alanı single-server deploy'ların #1 üretim sorunu" → `--dry-run` (`docker system df`) surfaces disk usage; reclaimed-space reporting surfaces the effect. ✅
- Related-but-separate features (#66 granular per-category prune, #79 build-cache/git-gc) are **out of scope** by design — this plan covers only feature #1.

**2. Placeholder scan:** No TBD/TODO/`add appropriate error handling`/`similar to Task N` steps; every code step contains full code and exact commands. ✅

**3. Type consistency:**
- `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupStats, error)` — same signature on interface (Task 3), stub (Task 3), docker impl (Task 3), mock (Task 3), and CLI call site (Task 4). ✅
- `CleanupOptions{DryRun, PruneAllImages, PruneVolumes bool}` and `CleanupStats{SpaceReclaimed uint64, Detail string}` defined once (Task 2), used identically in Tasks 3–4. ✅
- `cleanupCommand(t CleanupTarget, pruneAllImages bool)` (Task 2) matches its call in `dockerRuntime.Cleanup` (Task 3). ✅
- `formatBytes(uint64) string` (Task 4) and its tests agree; `parseReclaimedSpace` unit map keys (`kB` vs `KiB`) do not collide in the Go map. ✅
