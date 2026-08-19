# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning unused Docker resources (stopped containers, dangling images, unused networks, unused volumes) while protecting Tengiz-managed containers via labels and retaining the last N images per app for rollback.

**Architecture:** A new `Prune(ctx) (PruneReport, error)` method on `runtime.Manager` runs label-filtered docker prune commands (`docker container prune --filter label!=tengiz-app`, `docker image prune -f`, `docker network prune -f`, `docker volume prune -f`) and aggregates per-category counts plus total reclaimed space parsed from docker output. The `tengiz cleanup` CLI command (env-aware, like all other commands) calls per-app `KeepLastNImages` for retention, then `Prune`, and prints a report. Pure helpers (`parsePruneOutput`, `parseSize`, `formatSize`, `sumReclaimed`, `confirmCleanup`) are unit-tested; orchestration is tested with the existing mock runtime pattern.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface, `config.Store`, no new external dependencies.

## Global Constraints

- Docker CLI is invoked via `os/exec` (no Docker SDK) — same as all existing runtime methods
- All prune commands use `-f` (force) to avoid interactive prompts
- Tengiz-managed containers are protected with `--filter label!=tengiz-app` (the `tengiz-app` label constant is `labelKey` in `internal/runtime/docker.go:76`)
- No `docker image prune -a` aggressive flag: scale-to-zero means no container references most images, so `-a` would destroy rollback images. Image cleanup = per-app `KeepLastNImages` + dangling-image prune
- Default retention is 5 images per app (`--keep N` flag, matches existing `KeepLastNImages(ctx, app, 5)` calls in deploy)
- `tengiz cleanup` respects the global `--env` flag; store lookups use `config.NewStoreWithEnv(dataDir, env)`
- `Prune` itself is env-agnostic (docker prune is global); only per-app image retention is env-scoped
- No new external dependencies
- Existing tests must continue to pass without modification (except mock files gaining the new `Prune` method to satisfy the interface)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` | New: `PruneReport` type, `Prune()` on `dockerRuntime`, `runPrune()` docker exec helper, pure helpers `parsePruneOutput`, `parseSize`, `formatSize`, `sumReclaimed` |
| `internal/runtime/prune_test.go` | New: unit tests for all pure helpers |
| `internal/runtime/runtime.go` | Modify: add `Prune` to `Manager` interface (line 36) + stub impl (after line 119) |
| `internal/runtime/cleanup_test.go` | Modify: add stub `Prune` test |
| `internal/cli/root.go` | Modify: `cleanupCmd` + flags in `init()` + `runCleanup`/`confirmCleanup` helpers |
| `internal/cli/root_test.go` | Modify: add `Prune` to `mockRTForDeploy` (after line 99) + CLI tests |
| `internal/idle/idle_test.go` | Modify: add `Prune` to `mockRuntime` (after line 34) |
| `internal/proxy/proxy_test.go` | Modify: add `Prune` to `mockRuntime` (after line 35) |
| `README.md` | Modify: add `tengiz cleanup` command section |
| `docs/FUTURES_FEATURES.md` | Modify: mark feature #6 Docker Housekeeping as ✅ Implemented |

---

### Task 1: Add `Prune` to `runtime.Manager` interface + stub + mock implementations

**Files:**
- Modify: `internal/runtime/runtime.go:36` (interface), `internal/runtime/runtime.go:119` (stub)
- Modify: `internal/cli/root_test.go:99`
- Modify: `internal/idle/idle_test.go:34`
- Modify: `internal/proxy/proxy_test.go:35`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneReport{Containers, Images, Networks, Volumes int; Reclaimed string}`, `Manager.Prune(ctx context.Context) (PruneReport, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go — add
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background())
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.Containers != 0 || report.Images != 0 || report.Networks != 0 || report.Volumes != 0 {
		t.Errorf("expected empty prune report, got %+v", report)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubPrune -v -count=1`

Expected: FAIL with `m.Prune undefined (type Manager has no field or method Prune)`

- [ ] **Step 3: Add the `PruneReport` type and `Prune` method to the interface**

```go
// internal/runtime/runtime.go — add type + interface method
type PruneReport struct {
	Containers int
	Images     int
	Networks   int
	Volumes    int
	Reclaimed  string
}

// add to Manager interface after KeepLastNImages:
Prune(ctx context.Context) (PruneReport, error)
```

- [ ] **Step 4: Add stub implementation**

```go
// internal/runtime/runtime.go — after stub KeepLastNImages (line 119)
func (m *stubManager) Prune(ctx context.Context) (PruneReport, error) {
	return PruneReport{}, nil
}
```

- [ ] **Step 5: Update all mock runtime implementations to satisfy the interface**

```go
// internal/cli/root_test.go — after mock KeepLastNImages (line 99)
func (m *mockRTForDeploy) Prune(ctx context.Context) (runtime.PruneReport, error) { return runtime.PruneReport{}, nil }
```

```go
// internal/idle/idle_test.go — after mock KeepLastNImages (line 34)
func (m *mockRuntime) Prune(ctx context.Context) (runtime.PruneReport, error) { return runtime.PruneReport{}, nil }
```

```go
// internal/proxy/proxy_test.go — after mock KeepLastNImages (line 35)
func (m *mockRuntime) Prune(ctx context.Context) (runtime.PruneReport, error) { return runtime.PruneReport{}, nil }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/ ./internal/cli/ ./internal/idle/ ./internal/proxy/ -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Prune method to runtime.Manager interface"
```

---

### Task 2: Implement `dockerRuntime.Prune` + pure parsing helpers

**Files:**
- Create: `internal/runtime/prune.go`
- Create: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `PruneReport` type and `Manager.Prune` signature from Task 1; `labelKey` constant from `internal/runtime/docker.go:76`
- Produces: `(r *dockerRuntime) Prune(ctx context.Context) (PruneReport, error)`, pure helpers `parsePruneOutput(output string) (int, string)`, `parseSize(s string) float64`, `formatSize(b float64) string`, `sumReclaimed(sizes []string) string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/prune_test.go
package runtime

import "testing"

func TestParsePruneOutput(t *testing.T) {
	output := "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 2.345kB\n"
	count, reclaimed := parsePruneOutput(output)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if reclaimed != "2.345kB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "2.345kB")
	}
}

func TestParsePruneOutputNothingDeleted(t *testing.T) {
	count, reclaimed := parsePruneOutput("Total reclaimed space: 0B\n")
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if reclaimed != "0B" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "0B")
	}
}

func TestParsePruneOutputImagePrune(t *testing.T) {
	output := "Untagged: tengiz-apps/myapp:prod-123\nDeleted Images:\ndeleted: sha256:aaa\ndeleted: sha256:bbb\n\nTotal reclaimed space: 1.2MB\n"
	count, reclaimed := parsePruneOutput(output)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if reclaimed != "1.2MB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "1.2MB")
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{"0B", 0},
		{"512B", 512},
		{"2.5kB", 2500},
		{"1.5MB", 1500000},
		{"1GB", 1000000000},
		{"2TB", 2000000000000},
		{"garbage", 0},
	}
	for _, tt := range tests {
		got := parseSize(tt.in)
		if got != tt.want {
			t.Errorf("parseSize(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{2500, "2.5kB"},
		{1500000, "1.5MB"},
		{1000000000, "1GB"},
	}
	for _, tt := range tests {
		got := formatSize(tt.in)
		if got != tt.want {
			t.Errorf("formatSize(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSumReclaimed(t *testing.T) {
	got := sumReclaimed([]string{"1.5MB", "500kB"})
	if got != "2MB" {
		t.Errorf("sumReclaimed = %q, want %q", got, "2MB")
	}
}

func TestSumReclaimedEmpty(t *testing.T) {
	got := sumReclaimed(nil)
	if got != "0B" {
		t.Errorf("sumReclaimed(nil) = %q, want %q", got, "0B")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestParsePruneOutput|TestParseSize|TestFormatSize|TestSumReclaimed" -v -count=1`

Expected: FAIL with `undefined: parsePruneOutput` / `undefined: parseSize` / `undefined: formatSize` / `undefined: sumReclaimed`

- [ ] **Step 3: Write the implementation**

```go
// internal/runtime/prune.go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func (r *dockerRuntime) Prune(ctx context.Context) (PruneReport, error) {
	var report PruneReport
	var sizes []string

	out, err := r.runPrune(ctx, "container", "--filter", fmt.Sprintf("label!=%s", labelKey))
	if err != nil {
		return report, err
	}
	report.Containers, sizes = parsePruneOutput(out, sizes)

	out, err = r.runPrune(ctx, "image")
	if err != nil {
		return report, err
	}
	report.Images, sizes = parsePruneOutput(out, sizes)

	out, err = r.runPrune(ctx, "network")
	if err != nil {
		return report, err
	}
	report.Networks, sizes = parsePruneOutput(out, sizes)

	out, err = r.runPrune(ctx, "volume")
	if err != nil {
		return report, err
	}
	report.Volumes, sizes = parsePruneOutput(out, sizes)

	report.Reclaimed = sumReclaimed(sizes)
	return report, nil
}

func (r *dockerRuntime) runPrune(ctx context.Context, resource string, extraArgs ...string) (string, error) {
	args := append([]string{resource, "prune", "-f"}, extraArgs...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s prune: %w\n%s", resource, err, string(out))
	}
	return string(out), nil
}

func parsePruneOutput(output string, sizes []string) (int, []string) {
	count := 0
	inDeleted := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Deleted ") {
			inDeleted = true
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			sizes = append(sizes, strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:")))
			break
		}
		if inDeleted && line != "" {
			count++
		}
	}
	return count, sizes
}

func parseSize(s string) float64 {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		mult   float64
	}{
		{"TB", 1e12},
		{"GB", 1e9},
		{"MB", 1e6},
		{"kB", 1e3},
		{"B", 1},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			v, err := strconv.ParseFloat(num, 64)
			if err == nil {
				return v * u.mult
			}
		}
	}
	return 0
}

func formatSize(b float64) string {
	units := []struct {
		mult float64
		suf  string
	}{
		{1e12, "TB"},
		{1e9, "GB"},
		{1e6, "MB"},
		{1e3, "kB"},
		{1, "B"},
	}
	for _, u := range units {
		if b >= u.mult {
			v := b / u.mult
			if u.suf == "B" {
				return fmt.Sprintf("%.0f%s", v, u.suf)
			}
			return fmt.Sprintf("%g%s", v, u.suf)
		}
	}
	return "0B"
}

func sumReclaimed(sizes []string) string {
	var total float64
	for _, s := range sizes {
		total += parseSize(s)
	}
	return formatSize(total)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestParsePruneOutput|TestParseSize|TestFormatSize|TestSumReclaimed" -v -count=1`

Expected: All PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/ -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: implement dockerRuntime.Prune with label-protected pruning"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — `init()` (add flags + registration), new `cleanupCmd`, helpers `runCleanup` + `confirmCleanup`
- Modify: `internal/cli/root_test.go` — CLI tests

**Interfaces:**
- Consumes: `runtime.Manager.Prune(ctx) (PruneReport, error)` from Task 1, `runtime.Manager.KeepLastNImages(ctx, app, n)` (existing), `config.NewStoreWithEnv(dataDir, env)`, `getEnv(cmd)` helper (existing in `internal/cli/root.go:97`)
- Produces: `tengiz cleanup [--force] [--keep N]` command, `runCleanup(ctx, rt, store, keep) (runtime.PruneReport, error)`, `confirmCleanup(force bool, input string) bool`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/root_test.go — add
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not registered")
	}
	if cmd.Flags().Lookup("force") == nil {
		t.Error("cleanup missing --force flag")
	}
	if cmd.Flags().Lookup("keep") == nil {
		t.Error("cleanup missing --keep flag")
	}
}

func TestConfirmCleanup(t *testing.T) {
	tests := []struct {
		force bool
		input string
		want  bool
	}{
		{true, "", true},
		{false, "y", true},
		{false, "yes", true},
		{false, "Y", true},
		{false, "n", false},
		{false, "", false},
		{false, "maybe", false},
	}
	for _, tt := range tests {
		got := confirmCleanup(tt.force, tt.input)
		if got != tt.want {
			t.Errorf("confirmCleanup(force=%v, input=%q) = %v, want %v", tt.force, tt.input, got, tt.want)
		}
	}
}

func TestRunCleanupRetainsAndPrunes(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir
	store := config.NewStore(tmpDir)
	store.SaveApp(types.AppEntry{Name: "myapp", Config: types.AppConfig{Name: "myapp"}})
	store.SaveApp(types.AppEntry{Name: "other", Config: types.AppConfig{Name: "other"}})

	m := &mockRTForDeploy{}
	report, err := runCleanup(context.Background(), m, store, 5)
	if err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if report.Containers != 0 || report.Images != 0 {
		t.Errorf("expected empty prune report from mock, got %+v", report)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestConfirmCleanup|TestRunCleanupRetainsAndPrunes" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd` / `undefined: confirmCleanup` / `undefined: runCleanup`

- [ ] **Step 3: Add the cleanup command definition and helpers**

Add to `internal/cli/root.go` (place near `buildLogsCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Prunes stopped containers, dangling images, unused networks, and unused volumes.
Tengiz-managed containers are protected via labels. Per-app image retention
keeps the last N images for rollback (--keep, default 5).

Use --force to skip the confirmation prompt.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		force, _ := cmd.Flags().GetBool("force")
		keep, _ := cmd.Flags().GetInt("keep")

		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		if !confirmCleanup(force, input) {
			fmt.Println("[tengiz] cleanup aborted")
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		store := config.NewStoreWithEnv(dataDir, env)
		report, err := runCleanup(cmd.Context(), rt, store, keep)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Printf("[tengiz] removed: %d containers, %d images, %d networks, %d volumes\n",
			report.Containers, report.Images, report.Networks, report.Volumes)
		fmt.Printf("[tengiz] reclaimed: %s\n", report.Reclaimed)
		return nil
	},
}

func runCleanup(ctx context.Context, rt runtime.Manager, store *config.Store, keep int) (runtime.PruneReport, error) {
	apps, err := store.ListApps()
	if err != nil {
		return runtime.PruneReport{}, fmt.Errorf("list apps: %w", err)
	}
	for _, app := range apps {
		if err := rt.KeepLastNImages(ctx, app.Name, keep); err != nil {
			log.Printf("[tengiz] warning: image retention for %s: %v", app.Name, err)
		}
	}
	return rt.Prune(ctx)
}

func confirmCleanup(force bool, input string) bool {
	if force {
		return true
	}
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}
```

- [ ] **Step 4: Add flags and register the command in `init()`**

```go
// internal/cli/root.go — in init(), after existing flag registrations (around line 88)
cleanupCmd.Flags().Bool("force", false, "skip confirmation prompt")
cleanupCmd.Flags().Int("keep", 5, "number of images to retain per app")
// register with rootCmd (after rootCmd.AddCommand(notificationCmd))
rootCmd.AddCommand(cleanupCmd)
```

Add `"bufio"` to the imports of `internal/cli/root.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestConfirmCleanup|TestRunCleanupRetainsAndPrunes" -v -count=1`

Expected: All PASS

- [ ] **Step 6: Run all CLI tests**

Run: `go test ./internal/cli/ -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Update README and FUTURES_FEATURES docs

**Files:**
- Modify: `README.md` — add `tengiz cleanup` section after `### tengiz rollback` (line 236)
- Modify: `docs/FUTURES_FEATURES.md:19` — mark feature #6 as implemented

- [ ] **Step 1: Add the README section**

Add after the `### tengiz rollback` section (line 236):

```markdown
### `tengiz cleanup`

Reclaim disk space by pruning unused Docker resources. Removes stopped containers (excluding Tengiz-managed ones, which are protected via the `tengiz-app` label), dangling images, unused networks, and unused volumes. Retains the last N images per app for rollback.

| Flag | Description |
|------|-------------|
| `--force` | Skip the confirmation prompt |
| `--keep N` | Number of images to retain per app (default: 5) |

Example:
```
tengiz cleanup
tengiz cleanup --force --keep 3
```

Use the global `--env` flag to scope image retention to a specific environment: `tengiz cleanup --env staging`.
```

- [ ] **Step 2: Mark the feature as implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md:19`, change the status marker for feature #6:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add an entry to the "✅ Implemented Features (Not Pending)" table:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-19) |
```

Also add a status line to the "## Docker Housekeeping (Otomatik Temizlik)" feature section (line ~377):

```markdown
- **Status:** ✅ Implemented (2026-08-19)
```

- [ ] **Step 3: Run the full test suite**

Run: `go build ./... && go vet ./... && go test ./... -v -count=1`

Expected: Build succeeds, vet clean, all tests pass

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage:** Feature #6 Docker Housekeeping (`docs/FUTURES_FEATURES.md:19`) requires label-based docker pruning that protects Tengiz-managed containers + `tengiz cleanup` command. Task 1 adds the `Prune` interface method, Task 2 implements label-filtered `docker container prune` (`label!=tengiz-app`) + dangling-image/network/volume prune, Task 3 adds the `tengiz cleanup` CLI command, Task 4 documents it and marks it implemented. The "kullanılmayan volume, network, container ve image'leri temizleme" (cleanup of unused volumes/networks/containers/images) requirement is fully covered. Per-app image retention for rollback safety is covered via `KeepLastNImages`. No gaps.

**2. Placeholder scan:** No "TBD"/"TODO"/"implement later" patterns. Every step contains complete code, exact file paths, exact commands, and expected output.

**3. Type consistency:** `PruneReport` fields (`Containers/Images/Networks/Volumes int`, `Reclaimed string`) are identical across Task 1 (interface/stub/mocks), Task 2 (implementation), and Task 3 (CLI report printing). `Prune(ctx context.Context) (PruneReport, error)` is used consistently. Pure helper signatures `parsePruneOutput(output string, sizes []string) (int, []string)`, `parseSize(string) float64`, `formatSize(float64) string`, `sumReclaimed([]string) string`, `confirmCleanup(bool, string) bool`, `runCleanup(ctx, rt, store, keep) (runtime.PruneReport, error)` are consistent across tests and implementations. `labelKey` (`tengiz-app`) matches the constant in `docker.go:76`.