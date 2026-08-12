# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, build cache, and optionally networks/volumes) using label-based filtering so Tengiz-managed containers are never touched, and reports reclaimed disk space.

**Architecture:** Extend the `runtime.Manager` interface with a `Cleanup(ctx, CleanupOptions)` method implemented by `dockerRuntime` (exec-based `docker` CLI calls, per AGENTS.md "no Docker SDK") and a no-op stub. Docker command construction is extracted into pure, unit-testable arg-builder functions (`buildContainerPruneArgs`, `buildImagePruneArgs`, etc.) following the existing `buildLogArgs`/`buildRunArgs` pattern in `internal/runtime/docker.go`, plus output parsers (`parsePruneOutput`, `parseReclaimedSpace`). A new `cleanupCmd` Cobra command in `internal/cli/root.go` with `--all`, `--volumes`, `--dry-run` flags wires it to the CLI. Safety: containers labeled `tengiz-app` (stopped scale-to-zero containers, versioned deploy containers, preview containers) are protected by `docker container prune --filter label!=tengiz-app`; volumes are only pruned with an explicit opt-in flag.

**Tech Stack:** Go 1.26, Cobra, `os/exec` docker CLI (no Docker SDK), existing `runtime.Manager` interface and `dockerRuntime` struct.

## Global Constraints

- All Docker interaction via `os/exec` (no Docker SDK); `runtime.NewDocker()` already fails fast if `docker` is not in PATH
- Containers with label `tengiz-app` must NEVER be pruned — use `docker container prune -f --filter label!=tengiz-app`
- `tengiz cleanup` (default) prunes: non-Tengiz stopped containers, dangling images, Docker build cache
- `--all` additionally prunes unused images (`docker image prune -f -a`) and unused networks
- `--volumes` additionally prunes dangling volumes (opt-in — volumes hold data)
- `--dry-run` shows reclaimable space via `docker system df` and deletes nothing
- `--all` may remove tagged images not attached to any container (e.g. older deploy images beyond `KeepLastNImages`); rollback to those deployments will fail — documented tradeoff, opt-in only
- `cleanup` is a global operation; the root `--env` flag is ignored because label-based protection covers all environments
- No new external dependencies
- Every changed file ends with a passing test run; commands: `go test ./... -v -count=1` and `go vet ./...`
- Output follows existing conventions: `[tengiz]` prefix, `RunE` returning errors, `log.Printf("[tengiz] warning: ...")` for non-fatal issues
- README.md must be updated (AGENTS.md rule: UI/UX changes update documentation)
- Feature branch: `git checkout -b feat/docker-cleanup` at the start of Task 1

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `CleanupOptions`, `CleanupReport` types; `Cleanup()` on `dockerRuntime`; arg-builder functions (`buildContainerPruneArgs`, `buildImagePruneArgs`, `buildBuilderPruneArgs`, `buildNetworkPruneArgs`, `buildVolumePruneArgs`, `buildSystemDFArgs`); output parsers (`parsePruneOutput`, `parseReclaimedSpace`); private `pruneOutput` exec helper |
| `internal/runtime/runtime.go` | Add `Cleanup(ctx, CleanupOptions) (*CleanupReport, error)` to `Manager` interface and `stubManager` |
| `internal/runtime/cleanup_test.go` | Unit tests for arg builders, output parsers, and stub `Cleanup` |
| `internal/cli/root.go` | `cleanupCmd` definition + flag registration in `init()` |
| `internal/cli/cmd_cleanup_test.go` | CLI registration + flags test |
| `README.md` | Document `tengiz cleanup` command |

---

### Task 1: Runtime cleanup core (`internal/runtime`)

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface
- Modify: `internal/runtime/runtime.go` — add stub `Cleanup` after the existing `Run` stub (~line 121)
- Modify: `internal/runtime/cleanup.go` — add all new types, functions, and the `Cleanup` method
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: existing `runtime.Manager` interface, existing `dockerRuntime` struct, `exec.CommandContext` pattern from `internal/runtime/docker.go`
- Produces:
  - `type CleanupOptions struct { All bool; Volumes bool; DryRun bool }`
  - `type CleanupReport struct { DryRun bool; SystemDF string; ContainersRemoved int; ContainersReclaimed string; ImagesRemoved int; ImagesReclaimed string; BuildCacheReclaimed string; NetworksRemoved int; NetworksReclaimed string; VolumesRemoved int; VolumesReclaimed string; Warnings []string }`
  - `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)`
  - `func buildContainerPruneArgs() []string` → `[]string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}`
  - `func buildImagePruneArgs(all bool) []string`
  - `func buildBuilderPruneArgs() []string`
  - `func buildNetworkPruneArgs() []string`
  - `func buildVolumePruneArgs() []string`
  - `func buildSystemDFArgs() []string`
  - `func parsePruneOutput(output string) (int, string)`
  - `func parseReclaimedSpace(output string) string`

- [ ] **Step 1: Create the feature branch**

Run: `git checkout -b feat/docker-cleanup`

- [ ] **Step 2: Write the failing tests in `internal/runtime/cleanup_test.go`**

Replace the entire current file (it only contains stub smoke tests) with:

```go
package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
	}
	if report.DryRun {
		t.Fatal("Cleanup() should not be in dry-run mode")
	}
}

func TestBuildContainerPruneArgs(t *testing.T) {
	got := buildContainerPruneArgs()
	expected := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(expected) {
		t.Fatalf("buildContainerPruneArgs() = %v (len=%d), want %v (len=%d)", got, len(got), expected, len(expected))
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("buildContainerPruneArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestBuildImagePruneArgs(t *testing.T) {
	dangling := buildImagePruneArgs(false)
	expectedDangling := []string{"image", "prune", "-f"}
	if len(dangling) != len(expectedDangling) {
		t.Fatalf("buildImagePruneArgs(false) = %v, want %v", dangling, expectedDangling)
	}
	for i := range expectedDangling {
		if dangling[i] != expectedDangling[i] {
			t.Fatalf("buildImagePruneArgs(false)[%d] = %q, want %q", i, dangling[i], expectedDangling[i])
		}
	}

	all := buildImagePruneArgs(true)
	expectedAll := []string{"image", "prune", "-f", "-a"}
	if len(all) != len(expectedAll) {
		t.Fatalf("buildImagePruneArgs(true) = %v, want %v", all, expectedAll)
	}
	for i := range expectedAll {
		if all[i] != expectedAll[i] {
			t.Fatalf("buildImagePruneArgs(true)[%d] = %q, want %q", i, all[i], expectedAll[i])
		}
	}
}

func TestBuildBuilderPruneArgs(t *testing.T) {
	got := buildBuilderPruneArgs()
	expected := []string{"builder", "prune", "-f"}
	if len(got) != len(expected) {
		t.Fatalf("buildBuilderPruneArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("buildBuilderPruneArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestBuildNetworkPruneArgs(t *testing.T) {
	got := buildNetworkPruneArgs()
	expected := []string{"network", "prune", "-f"}
	if len(got) != len(expected) {
		t.Fatalf("buildNetworkPruneArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("buildNetworkPruneArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestBuildVolumePruneArgs(t *testing.T) {
	got := buildVolumePruneArgs()
	expected := []string{"volume", "prune", "-f"}
	if len(got) != len(expected) {
		t.Fatalf("buildVolumePruneArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("buildVolumePruneArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestBuildSystemDFArgs(t *testing.T) {
	got := buildSystemDFArgs()
	expected := []string{"system", "df", "--format", "{{.Type}}\t{{.Reclaimable}}"}
	if len(got) != len(expected) {
		t.Fatalf("buildSystemDFArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("buildSystemDFArgs()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestParsePruneOutput(t *testing.T) {
	const output = `Deleted Containers:
2e4d5f6a7b8c
9a8b7c6d5e4f

Total reclaimed space: 2.1MB
`
	removed, reclaimed := parsePruneOutput(output)
	if removed != 2 {
		t.Fatalf("parsePruneOutput() removed = %d, want 2", removed)
	}
	if reclaimed != "2.1MB" {
		t.Fatalf("parsePruneOutput() reclaimed = %q, want %q", reclaimed, "2.1MB")
	}
}

func TestParsePruneOutputNothingToRemove(t *testing.T) {
	removed, reclaimed := parsePruneOutput("Total reclaimed space: 0B\n")
	if removed != 0 {
		t.Fatalf("parsePruneOutput() removed = %d, want 0", removed)
	}
	if reclaimed != "0B" {
		t.Fatalf("parsePruneOutput() reclaimed = %q, want %q", reclaimed, "0B")
	}
}

func TestParseReclaimedSpaceBuilderOutput(t *testing.T) {
	const output = "Build cache entries to be pruned:\nxxxx\n\nTotal: 300MB\n"
	if got := parseReclaimedSpace(output); got != "300MB" {
		t.Fatalf("parseReclaimedSpace() = %q, want %q", got, "300MB")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestStubCleanup|TestBuildContainerPruneArgs|TestBuildImagePruneArgs|TestBuildBuilderPruneArgs|TestBuildNetworkPruneArgs|TestBuildVolumePruneArgs|TestBuildSystemDFArgs|TestParsePruneOutput|TestParseReclaimedSpaceBuilderOutput" -v -count=1`

Expected: FAIL with `undefined: CleanupOptions`, `undefined: Cleanup`, `undefined: buildContainerPruneArgs`, etc.

- [ ] **Step 4: Add `Cleanup` to the `Manager` interface in `internal/runtime/runtime.go`**

In the `Manager` interface (after the `Run` line at `runtime.go:48`), add:

```go
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
```

- [ ] **Step 5: Add the stub `Cleanup` implementation in `internal/runtime/runtime.go`**

After the existing `stubManager.Run` method (at `runtime.go:121-123`), add:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return &CleanupReport{}, nil
}
```

- [ ] **Step 6: Implement the arg builders, parsers, and `Cleanup` in `internal/runtime/cleanup.go`**

Replace the current content of `internal/runtime/cleanup.go` with:

```go
package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

type CleanupOptions struct {
	All     bool // prune unused images (-a) and unused networks
	Volumes bool // prune dangling volumes (opt-in; volumes hold data)
	DryRun  bool // only report reclaimable space, delete nothing
}

type CleanupReport struct {
	DryRun              bool
	SystemDF            string   // set only when DryRun is true
	ContainersRemoved   int
	ContainersReclaimed string
	ImagesRemoved       int
	ImagesReclaimed     string
	BuildCacheReclaimed string
	NetworksRemoved     int
	NetworksReclaimed   string
	VolumesRemoved      int
	VolumesReclaimed    string
	Warnings            []string
}

func buildContainerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func buildImagePruneArgs(all bool) []string {
	if all {
		return []string{"image", "prune", "-f", "-a"}
	}
	return []string{"image", "prune", "-f"}
}

func buildBuilderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func buildNetworkPruneArgs() []string {
	return []string{"network", "prune", "-f"}
}

func buildVolumePruneArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func buildSystemDFArgs() []string {
	return []string{"system", "df", "--format", "{{.Type}}\t{{.Reclaimable}}"}
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Total") {
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 {
				return fields[len(fields)-1]
			}
		}
	}
	return "0B"
}

func parsePruneOutput(output string) (int, string) {
	removed := 0
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Total") || strings.HasSuffix(trimmed, ":") {
			continue
		}
		removed++
	}
	return removed, parseReclaimedSpace(output)
}

func (r *dockerRuntime) pruneOutput(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	report := &CleanupReport{DryRun: opts.DryRun}

	if opts.DryRun {
		out, err := r.pruneOutput(ctx, buildSystemDFArgs())
		if err != nil {
			return nil, err
		}
		report.SystemDF = out
		return report, nil
	}

	if out, err := r.pruneOutput(ctx, buildContainerPruneArgs()); err != nil {
		report.Warnings = append(report.Warnings, err.Error())
	} else {
		report.ContainersRemoved, report.ContainersReclaimed = parsePruneOutput(out)
	}

	if out, err := r.pruneOutput(ctx, buildImagePruneArgs(opts.All)); err != nil {
		report.Warnings = append(report.Warnings, err.Error())
	} else {
		report.ImagesRemoved, report.ImagesReclaimed = parsePruneOutput(out)
	}

	if out, err := r.pruneOutput(ctx, buildBuilderPruneArgs()); err != nil {
		report.Warnings = append(report.Warnings, err.Error())
	} else {
		report.BuildCacheReclaimed = parseReclaimedSpace(out)
	}

	if opts.All {
		if out, err := r.pruneOutput(ctx, buildNetworkPruneArgs()); err != nil {
			report.Warnings = append(report.Warnings, err.Error())
		} else {
			report.NetworksRemoved, report.NetworksReclaimed = parsePruneOutput(out)
		}
	}

	if opts.Volumes {
		if out, err := r.pruneOutput(ctx, buildVolumePruneArgs()); err != nil {
			report.Warnings = append(report.Warnings, err.Error())
		} else {
			report.VolumesRemoved, report.VolumesReclaimed = parsePruneOutput(out)
		}
	}

	return report, nil
}

func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= n {
		return nil
	}

	sort.Slice(lines, func(i, j int) bool {
		partsI := strings.SplitN(lines[i], "|", 2)
		partsJ := strings.SplitN(lines[j], "|", 2)
		if len(partsI) < 2 || len(partsJ) < 2 {
			return false
		}
		return partsI[1] < partsJ[1]
	})

	for i := 0; i < len(lines)-n; i++ {
		parts := strings.SplitN(lines[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") {
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
		}
	}
	return nil
}
```

Note: `RemoveImage` and `KeepLastNImages` are preserved unchanged — they already exist and must not be deleted. The `log` import is required by `KeepLastNImages`.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestStubCleanup|TestBuildContainerPruneArgs|TestBuildImagePruneArgs|TestBuildBuilderPruneArgs|TestBuildNetworkPruneArgs|TestBuildVolumePruneArgs|TestBuildSystemDFArgs|TestParsePruneOutput|TestParseReclaimedSpaceBuilderOutput" -v -count=1`

Expected: all PASS.

- [ ] **Step 8: Run the full runtime package test suite + vet**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS.

Run: `go vet ./internal/runtime/...`
Expected: no output, exit 0.

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add Cleanup to prune unused Docker resources"
```

---

### Task 2: CLI command `tengiz cleanup`

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` (before the `gitCmd` var around line 1164) and register it + flags in `init()`
- Test: `internal/cli/cmd_cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()` (exists), `runtime.CleanupOptions{All, Volumes, DryRun bool}`, `runtime.Manager.Cleanup(ctx, CleanupOptions) (*runtime.CleanupReport, error)`, `runtime.CleanupReport` fields (`DryRun`, `SystemDF`, `ContainersRemoved`, `ContainersReclaimed`, `ImagesRemoved`, `ImagesReclaimed`, `BuildCacheReclaimed`, `NetworksRemoved`, `NetworksReclaimed`, `VolumesRemoved`, `VolumesReclaimed`, `Warnings`), and `findSubcommand(parent, name)` helper defined in `internal/cli/cmd_secret_test.go`
- Produces: registered `tengiz cleanup` command with `--all`, `--volumes`, `--dry-run` flags

- [ ] **Step 1: Write the failing CLI test in `internal/cli/cmd_cleanup_test.go`**

Create the file:

```go
package cli

import "testing"

func TestCleanupCommandRegistered(t *testing.T) {
	c := findSubcommand(rootCmd, "cleanup")
	if c == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}

	for _, flag := range []string{"all", "volumes", "dry-run"} {
		if c.Flags().Lookup(flag) == nil {
			t.Fatalf("cleanup missing --%s flag", flag)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCleanupCommandRegistered -v -count=1`

Expected: FAIL with `cleanup command not registered on rootCmd`.

- [ ] **Step 3: Register the `cleanupCmd` flags and command in `internal/cli/root.go` `init()`**

In `init()` (after `rootCmd.AddCommand(notificationCmd)` at `root.go:75`), add:

```go
	cleanupCmd.Flags().Bool("all", false, "also prune unused images and unused networks")
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused (dangling) volumes")
	cleanupCmd.Flags().Bool("dry-run", false, "show reclaimable space without deleting anything")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 4: Define `cleanupCmd` in `internal/cli/root.go`**

Immediately before the `var gitCmd` declaration (at `root.go:1164`), add:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (containers, images, build cache)",
	Long: `Prune unused Docker resources to reclaim disk space.

By default prunes: stopped containers not managed by Tengiz, dangling images,
and the Docker build cache. Containers managed by Tengiz (label tengiz-app) are
always protected — including stopped scale-to-zero containers and versioned
deploy containers.

Flags:
  --all       also prune unused images (docker image prune -a) and unused networks
  --volumes   also prune unused (dangling) volumes
  --dry-run   show reclaimable space per resource type without deleting anything`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			All:     all,
			Volumes: volumes,
			DryRun:  dryRun,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		for _, w := range report.Warnings {
			log.Printf("[tengiz] warning: %s", w)
		}

		if report.DryRun {
			fmt.Println("[tengiz] dry-run: reclaimable space by resource type")
			fmt.Print(report.SystemDF)
			return nil
		}

		fmt.Println("[tengiz] cleanup complete:")
		fmt.Printf("  containers:  %d removed (%s)\n", report.ContainersRemoved, report.ContainersReclaimed)
		fmt.Printf("  images:      %d removed (%s)\n", report.ImagesRemoved, report.ImagesReclaimed)
		fmt.Printf("  build cache: %s\n", report.BuildCacheReclaimed)
		if all {
			fmt.Printf("  networks:    %d removed (%s)\n", report.NetworksRemoved, report.NetworksReclaimed)
		}
		if volumes {
			fmt.Printf("  volumes:     %d removed (%s)\n", report.VolumesRemoved, report.VolumesReclaimed)
		}
		return nil
	},
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestCleanupCommandRegistered -v -count=1`

Expected: PASS.

- [ ] **Step 6: Run the full CLI test package**

Run: `go test ./internal/cli/... -v -count=1`
Expected: PASS.

Run: `go vet ./internal/cli/...`
Expected: no output, exit 0.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cmd_cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 3: Documentation + full verification

**Files:**
- Modify: `README.md` — add `tengiz cleanup` to the CLI commands list

**Interfaces:**
- Consumes: the `tengiz cleanup` command registered in Task 2
- Produces: updated README documenting `tengiz cleanup`

- [ ] **Step 1: Document `tengiz cleanup` in `README.md`**

Find the CLI commands section in `README.md` that lists commands like `tengiz ps`, `tengiz logs`, `tengiz rollback`. Add a line after `tengiz rollback <app>`:

```markdown
tengiz cleanup          → prune unused Docker resources (containers, images, build cache)
tengiz cleanup --all    → also prune unused images and unused networks
tengiz cleanup --volumes → also prune unused volumes
tengiz cleanup --dry-run → show reclaimable space without deleting anything
```

(Adjust the surrounding text to match the exact existing list format — keep the `→` style used by the rest of the README's CLI listing.)

- [ ] **Step 2: Run the complete test suite**

Run: `go test ./... -v -count=1`
Expected: all packages PASS (note: `internal/proxy` tests are slow, ~2s each — that is expected per AGENTS.md).

- [ ] **Step 3: Run static analysis**

Run: `go vet ./...`
Expected: no output, exit 0.

- [ ] **Step 4: Build the binary**

Run: `go build -o tengiz .`
Expected: succeeds, produces `tengiz` binary.

- [ ] **Step 5: Manual smoke test (if Docker is available)**

Run: `./tengiz cleanup --dry-run`
Expected: prints `[tengiz] dry-run: reclaimable space by resource type` followed by a `docker system df` table. If Docker is unavailable, this step is skipped — the command must have already returned a clean error path at `runtime.NewDocker()`.

Run: `./tengiz cleanup`
Expected: prints `[tengiz] cleanup complete:` with per-category reclaimed counts.

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage** — Feature #6 (Docker Housekeeping, P0) requirements from `docs/FUTURES_FEATURES.md`:
- "Label-based `docker system prune`" → Task 1 `buildContainerPruneArgs` uses `--filter label!=tengiz-app`; pruning is composed from granular `docker <category> prune` commands instead of unfiltered `docker system prune` so the label filter actually applies (the granular commands are the only way to apply the label filter). Covered.
- "`tengiz cleanup`" command → Task 2. Covered.
- "Disk space is the #1 production issue" → cleanup reports reclaimed space per category; `--dry-run` shows reclaimable space before deleting. Covered.
- Scale-to-zero stopped containers must survive cleanup → protected by the `tengiz-app` label filter (stopped Tengiz containers carry the label). Covered.

**2. Placeholder scan** — No TBD/TODO/placeholder patterns; every step contains full code or exact commands.

**3. Type consistency** — `CleanupOptions{All, Volumes, DryRun}`, `CleanupReport` fields, and `Manager.Cleanup(ctx, CleanupOptions) (*CleanupReport, error)` are identical across Task 1 (definition) and Task 2 (consumption). Flag names `all`/`volumes`/`dry-run` in Task 2 match `report.All`/`report.Volumes`/`report.DryRun` usage. Arg-builder names (`buildContainerPruneArgs`, `buildImagePruneArgs`, `buildBuilderPruneArgs`, `buildNetworkPruneArgs`, `buildVolumePruneArgs`, `buildSystemDFArgs`) and parser names (`parsePruneOutput`, `parseReclaimedSpace`) are used consistently in Task 1 tests and implementation.

**Known tradeoff (documented, opt-in):** `tengiz cleanup --all` runs `docker image prune -a`, which removes tagged images not referenced by any existing container. A rollback to a deployment whose image was pruned will fail. Default `tengiz cleanup` (dangling images only) does not have this risk, and `KeepLastNImages` (existing) already limits retained images per app. This matches the aggressive nature of the `--all` flag in the spec.
