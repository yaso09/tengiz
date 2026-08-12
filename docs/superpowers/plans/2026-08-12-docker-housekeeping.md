# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused networks, and opt-in volumes) while always protecting Tengiz-managed resources via label-based filtering.

**Architecture:** The `runtime.Manager` interface gains a single `Prune(ctx, opts)` method that returns a `PruneReport`. The Docker implementation shells out to per-category `docker <type> prune` commands with the `-f` flag and a `label!=tengiz-app` filter on containers and volumes so Tengiz-managed containers and volumes are never removed. Pure helper functions (`buildPruneArgs`, `parsePruneOutput`, `sumReclaimed`) keep argument construction and Docker output parsing unit-testable without a Docker daemon. The CLI wires them into a `tengiz cleanup` cobra command. The default run prunes containers, images, and networks but **not** volumes (mirroring `docker system prune`, which also requires explicit `--volumes`).

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` to the `docker` CLI (no Docker SDK), existing `runtime.Manager` interfaces and test stubs.

## Global Constraints

- Module: `github.com/yaso09/tengiz`, Go 1.26. No new external dependencies.
- Docker is invoked via the `docker` CLI with `os/exec` — never the Docker SDK.
- Tengiz-managed containers and images carry the label `tengiz-app=<appname>`; any resource carrying this label MUST be preserved by cleanup.
- Default `tengiz cleanup` prunes stopped containers, dangling images, and unused networks. Volumes are pruned only with `--volumes`.
- Adding a method to `runtime.Manager` requires updating all four implementations: `stubManager` (`internal/runtime/runtime.go`), `mockRTForDeploy` (`internal/cli/root_test.go`), and `mockRuntime` in both `internal/proxy/proxy_test.go` and `internal/idle/idle_test.go`. The build fails to compile until all four are updated.
- Image prune uses dangling-only mode (`docker image prune`, no `-a`). Tengiz images are always tagged (`tengiz-apps/<app>:<id>`) and are therefore never dangling — no extra filter needed.
- `docker` prune commands are run with `-f` (force) so they are non-interactive; the CLI does not prompt (consistent with `tengiz rm`).
- Test commands: `go test ./... -v -count=1`; static analysis: `go vet ./...`.
- Per AGENTS.md the README and `docs/FUTURES_FEATURES.md` must be updated when the CLI surface changes.
- Each task ends with an independently testable deliverable and a commit.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `PruneOptions`, `PruneReport`, `pruneType` constants, pure helpers `buildPruneArgs`/`parsePruneOutput`/`parseSize`/`sumReclaimed`, `dockerRuntime.Prune` + `runPrune` |
| `internal/runtime/runtime.go` | Add `Prune` to the `Manager` interface; stub implementation on `stubManager` |
| `internal/cli/cleanup.go` | New `cleanupCmd` cobra command |
| `internal/cli/root.go` | Register `cleanupCmd` and its `--volumes` flag in `init()` |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` (keeps interface compile check green) |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `internal/runtime/cleanup_test.go` | Tests for helpers, stub, and report shape |
| `internal/cli/cleanup_test.go` | Registration + flag tests for `cleanupCmd` |
| `README.md` | New `### tengiz cleanup [--volumes]` CLI reference section + git auto-deploy commands table row |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

The feature is deliberately split into four tasks: (1) pure helper functions that are fully unit-testable, (2) the `Manager` interface method that consumes them, (3) the CLI command, (4) documentation. Each task compiles and leaves tests green on its own.

---

### Task 1: Prune types + pure helper functions

**Files:**
- Modify: `internal/runtime/cleanup.go` — append types and helper functions
- Modify: `internal/runtime/cleanup_test.go` — append tests

**Interfaces:**
- Consumes: nothing (self-contained pure functions + types)
- Produces:
  - `type PruneOptions struct { Containers, Images, Networks, Volumes bool }`
  - `type PruneReport struct { Containers, Images, Networks, Volumes int; ReclaimedSpace string }` (json tags: `containers`, `images`, `networks`, `volumes`, `reclaimed_space`)
  - `buildPruneArgs(t pruneType) []string`
  - `parsePruneOutput(output string) (count int, reclaimed string)`
  - `sumReclaimed(values []string) string`

- [ ] **Step 1: Write the failing tests**

Append these test functions to `internal/runtime/cleanup_test.go`:

```go
func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		kind     pruneType
		expected []string
	}{
		{pruneContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{pruneImages, []string{"image", "prune", "-f"}},
		{pruneNetworks, []string{"network", "prune", "-f"}},
		{pruneVolumes, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}},
	}
	for _, tc := range tests {
		got := buildPruneArgs(tc.kind)
		if len(got) != len(tc.expected) {
			t.Errorf("buildPruneArgs(%q) = %v, want %v", tc.kind, got, tc.expected)
			continue
		}
		for i := range got {
			if got[i] != tc.expected[i] {
				t.Errorf("buildPruneArgs(%q) = %v, want %v", tc.kind, got, tc.expected)
				break
			}
		}
	}
}

func TestParsePruneOutput(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantCount   int
		wantReclaim string
	}{
		{
			name:        "nothing removed",
			output:      "Total reclaimed space: 0B\n",
			wantCount:   0,
			wantReclaim: "0B",
		},
		{
			name:        "containers removed",
			output:      "Deleted Containers:\n9b1a4f2c\nf2c9a1b3\n\nTotal reclaimed space: 12.45MB\n",
			wantCount:   2,
			wantReclaim: "12.45MB",
		},
		{
			name:        "image detail lines counted once each",
			output:      "Deleted Images:\nuntagged: tengiz-apps/demo:1700000000\ndeleted: sha256:abcd\n\nTotal reclaimed space: 3B\n",
			wantCount:   2,
			wantReclaim: "3B",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count, reclaimed := parsePruneOutput(tc.output)
			if count != tc.wantCount {
				t.Errorf("parsePruneOutput(%q) count = %d, want %d", tc.output, count, tc.wantCount)
			}
			if reclaimed != tc.wantReclaim {
				t.Errorf("parsePruneOutput(%q) reclaimed = %q, want %q", tc.output, reclaimed, tc.wantReclaim)
			}
		})
	}
}

func TestSumReclaimed(t *testing.T) {
	tests := []struct {
		name     string
		values   []string
		expected string
	}{
		{"empty", nil, "0B"},
		{"single zero", []string{"0B"}, "0B"},
		{"bytes round to kB", []string{"500B", "500B"}, "1kB"},
		{"kb sum", []string{"1kB", "1kB"}, "2kB"},
		{"mb sum", []string{"1.5MB", "0.5MB"}, "2MB"},
		{"gb plus mb", []string{"1GB", "512MB"}, "1.512GB"},
		{"unparseable ignored", []string{"12.45MB", "??"}, "12.45MB"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sumReclaimed(tc.values)
			if got != tc.expected {
				t.Errorf("sumReclaimed(%v) = %q, want %q", tc.values, got, tc.expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestBuildPruneArgs|TestParsePruneOutput|TestSumReclaimed" -v -count=1`

Expected: FAIL with `undefined: pruneContainers`, `undefined: buildPruneArgs`, `undefined: parsePruneOutput`, `undefined: sumReclaimed`.

- [ ] **Step 3: Write the implementation**

Append to `internal/runtime/cleanup.go`:

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
}

type PruneReport struct {
	Containers     int    `json:"containers"`
	Images         int    `json:"images"`
	Networks       int    `json:"networks"`
	Volumes        int    `json:"volumes"`
	ReclaimedSpace string `json:"reclaimed_space"`
}

type pruneType string

const (
	pruneContainers pruneType = "containers"
	pruneImages     pruneType = "images"
	pruneNetworks   pruneType = "networks"
	pruneVolumes    pruneType = "volumes"
)

func buildPruneArgs(t pruneType) []string {
	switch t {
	case pruneContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case pruneImages:
		return []string{"image", "prune", "-f"}
	case pruneNetworks:
		return []string{"network", "prune", "-f"}
	case pruneVolumes:
		return []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	default:
		return nil
	}
}

func parsePruneOutput(output string) (int, string) {
	count := 0
	reclaimed := ""
	seenDeletedHeading := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			reclaimed = strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
			continue
		}
		if strings.HasPrefix(trimmed, "Deleted ") {
			seenDeletedHeading = true
			continue
		}
		if seenDeletedHeading && trimmed != "" {
			count++
		}
	}
	return count, reclaimed
}

var sizeUnits = []struct {
	suffix  string
	divisor float64
}{
	{"TB", 1e12},
	{"GB", 1e9},
	{"MB", 1e6},
	{"kB", 1e3},
	{"B", 1},
}

func parseSize(s string) float64 {
	s = strings.TrimSpace(s)
	for _, u := range sizeUnits {
		if strings.HasSuffix(s, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0
			}
			return f * u.divisor
		}
	}
	return 0
}

func sumReclaimed(values []string) string {
	var total float64
	for _, v := range values {
		total += parseSize(v)
	}
	for _, u := range sizeUnits {
		if u.divisor > 1 && total >= u.divisor {
			return fmt.Sprintf("%.4g%s", total/u.divisor, u.suffix)
		}
	}
	return fmt.Sprintf("%.0fB", total)
}
```

The existing imports in `internal/runtime/cleanup.go` are `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`. Add `"strconv"` to the import block.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestBuildPruneArgs|TestParsePruneOutput|TestSumReclaimed" -v -count=1`

Expected: PASS for all test cases (including `TestStubRemoveImage`/`TestStubKeepLastNImages` if run without the `-run` filter).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add prune types and pure cleanup helpers"
```

---

### Task 2: `Prune` method on the Manager interface

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.Prune` and `dockerRuntime.runPrune`
- Modify: `internal/runtime/runtime.go:31-49` — add `Prune` to the `Manager` interface; add stub method after `KeepLastNImages`
- Modify: `internal/cli/root_test.go:99` — add `Prune` to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go` — add `Prune` to `mockRuntime`
- Modify: `internal/idle/idle_test.go` — add `Prune` to `mockRuntime`
- Modify: `internal/runtime/cleanup_test.go` — append `TestStubPrune`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport`, `pruneType`, `buildPruneArgs`, `parsePruneOutput`, `sumReclaimed` from Task 1.
- Produces: `Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)` on the `Manager` interface, `stubManager`, `dockerRuntime`, `mockRTForDeploy`, and both `mockRuntime` types. The dockerRuntime version returns a report with per-category counts and a single summed `ReclaimedSpace`.

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.Containers != 0 || report.Images != 0 || report.Networks != 0 || report.Volumes != 0 {
		t.Errorf("Prune() report = %+v, want zero-value", report)
	}
	if report.ReclaimedSpace != "" {
		t.Errorf("Prune() ReclaimedSpace = %q, want empty", report.ReclaimedSpace)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`

Expected: FAIL with `m.Prune undefined (type Manager has no field or method Prune)` (compile error).

- [ ] **Step 3: Write minimal implementation**

Add the interface method in `internal/runtime/runtime.go`, inside the `Manager` interface after the `KeepLastNImages` line:

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

Add the stub method in `internal/runtime/runtime.go` after the existing `stubManager.KeepLastNImages`:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}
```

Add the real implementation to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport
	var reclaimed []string

	if opts.Containers {
		count, space, err := r.runPrune(ctx, pruneContainers)
		if err != nil {
			return report, err
		}
		report.Containers = count
		reclaimed = append(reclaimed, space)
	}
	if opts.Images {
		count, space, err := r.runPrune(ctx, pruneImages)
		if err != nil {
			return report, err
		}
		report.Images = count
		reclaimed = append(reclaimed, space)
	}
	if opts.Networks {
		count, space, err := r.runPrune(ctx, pruneNetworks)
		if err != nil {
			return report, err
		}
		report.Networks = count
		reclaimed = append(reclaimed, space)
	}
	if opts.Volumes {
		count, space, err := r.runPrune(ctx, pruneVolumes)
		if err != nil {
			return report, err
		}
		report.Volumes = count
		reclaimed = append(reclaimed, space)
	}

	report.ReclaimedSpace = sumReclaimed(reclaimed)
	return report, nil
}

func (r *dockerRuntime) runPrune(ctx context.Context, kind pruneType) (int, string, error) {
	args := buildPruneArgs(kind)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("docker %s prune: %w\n%s", string(kind), err, string(out))
	}
	count, reclaimed := parsePruneOutput(string(out))
	return count, reclaimed, nil
}
```

Add `Prune` to the three test mocks so the whole repo still compiles:

In `internal/cli/root_test.go`, after the existing `KeepLastNImages` mock method (line ~99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

In `internal/proxy/proxy_test.go`, inside the `mockRuntime` type, after the existing `KeepLastNImages` method (line ~34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) { return runtime.PruneReport{}, nil }
```

In `internal/idle/idle_test.go`, inside the `mockRuntime` type, after the existing `KeepLastNImages` method (line ~33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) { return runtime.PruneReport{}, nil }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -v -count=1`

Expected: all packages compile and pass, including `TestStubPrune`, `TestMockRTForDeployImplementsManager`, `TestStubRemoveImage`, and the proxy/idle/runtime suites.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add prune method to manager interface"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:44` — register `cleanupCmd` after `rootCmd.AddCommand(rmCmd)`
- Modify: `internal/cli/root.go:88` — register the `--volumes` flag after `webhookCmd.Flags()` definitions
- Test: `internal/cli/cleanup_test.go` (new file)

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneReport` from Tasks 1-2.
- Produces: `cleanupCmd` (package-level `*cobra.Command`), registered on the root command with `Use: "cleanup"` and a `--volumes` bool flag.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "cleanup" {
			found = true
			break
		}
	}
	if !found {
		t.Error("cleanup command not registered on root")
	}
}

func TestCleanupCmdHasVolumesFlag(t *testing.T) {
	if cleanupCmd == nil {
		t.Skip("cleanupCmd not defined")
	}
	if cleanupCmd.Flags().Lookup("volumes") == nil {
		t.Error("cleanup command missing --volumes flag")
	}
}

func TestCleanupCmdRejectsArgs(t *testing.T) {
	if cleanupCmd == nil {
		t.Skip("cleanupCmd not defined")
	}
	if err := cleanupCmd.Args(cleanupCmd, []string{"myapp"}); err == nil {
		t.Error("expected error when cleanup is called with arguments")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`

Expected: FAIL — `TestCleanupCmdRegistered` reports "cleanup command not registered on root"; `TestCleanupCmdRejectsArgs` may also fail on `cleanupCmd.Args` with `cleanupCmd` nil or `Args` nil causing a panic — if `cleanupCmd` is undefined the package fails to compile, which is also an acceptable FAIL at this stage.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cleanup.go`:

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

Removes stopped containers (without a tengiz-app label), dangling images, and
unused networks. Tengiz-managed containers and volumes are always preserved.

Pass --volumes to also prune unused Docker volumes.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		withVolumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.PruneOptions{
			Containers: true,
			Images:     true,
			Networks:   true,
			Volumes:    withVolumes,
		}

		report, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Printf("Containers removed: %d\n", report.Containers)
		fmt.Printf("Images removed: %d\n", report.Images)
		fmt.Printf("Networks removed: %d\n", report.Networks)
		fmt.Printf("Volumes removed: %d\n", report.Volumes)
		fmt.Printf("Total reclaimed space: %s\n", report.ReclaimedSpace)
		return nil
	},
}
```

Register it in `internal/cli/root.go` `init()`. After the line `rootCmd.AddCommand(rmCmd)` (line 44) add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

After the `webhookCmd.Flags()` block (line 88) add:

```go
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused Docker volumes")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestCleanup -v -count=1`

Expected: PASS for `TestCleanupCmdRegistered`, `TestCleanupCmdHasVolumesFlag`, `TestCleanupCmdRejectsArgs`.

Also run the full suite to confirm nothing else regressed:
Run: `go test ./... -v -count=1`
Expected: all PASS.

Optional manual smoke test (requires Docker installed):
Run: `go run . cleanup`
Expected: prints `Containers removed: N`, `Images removed: N`, `Networks removed: N`, `Volumes removed: 0`, `Total reclaimed space: <size>`.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md:230-237` — add `### tengiz cleanup [--volumes]` section after the `tengiz rollback` section
- Modify: `README.md:568-575` — add a row to the Commands table
- Modify: `docs/FUTURES_FEATURES.md:19` — mark feature #6 implemented
- Modify: `docs/FUTURES_FEATURES.md:377-381` — add Status line to the feature detail section

**Interfaces:**
- Consumes: nothing.
- Produces: documentation entries only.

- [ ] **Step 1: Document `tengiz cleanup` in README.md**

Insert after the `tengiz rollback` section (after line 236, before `### tengiz domain`):

```markdown
### `tengiz cleanup [--volumes]`

Prune unused Docker resources to reclaim disk space. Removes stopped containers, dangling images, and unused networks. Tengiz-managed containers and volumes (labeled `tengiz-app=*`) are always preserved.

By default, volumes are **not** pruned; pass `--volumes` to also remove unused Docker volumes.

| Flag | Description |
|------|-------------|
| `--volumes` | Also prune unused Docker volumes |
```

Add a row to the Commands table in the Git Auto-Deploy section (line 570):

```markdown
| `tengiz cleanup [--volumes]` | Prune unused Docker resources and reclaim disk space |
```

- [ ] **Step 2: Mark the feature implemented in FUTURES_FEATURES.md**

In the P0 table row `#6` (line 19), change the status cell from `⬜` to `✅`:

Old:
```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```
New:
```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the `## Docker Housekeeping (Otomatik Temizlik)` detail section, add a Status line after the `- **Why add to Tengiz:**` line:

```markdown
- **Status:** ✅ Implemented (2026-08-12)
```

- [ ] **Step 3: Verify nothing is broken**

Run: `go build -o /dev/null . && go vet ./... && go test ./... -v -count=1`

Expected: build succeeds, vet clean, all tests pass.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command"
```