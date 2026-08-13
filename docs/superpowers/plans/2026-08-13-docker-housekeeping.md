# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker containers, images, networks, and (opt-in) volumes — while protecting all Tengiz-managed containers via label-based filtering — to reclaim disk space on single-server deployments.

**Architecture:** `runtime.Manager` gains a `Cleanup(ctx, opts)` method implemented on `dockerRuntime` by shelling out to `docker system prune -f --filter label!=tengiz-app`. The `label!=tengiz-app` filter protects every container carrying the `tengiz-app` label (all deployed apps, including stopped scale-to-zero containers and versioned deploy containers), so pruning only touches non-Tengiz stopped containers, dangling images, and unused networks. The CLI command additionally calls the existing `KeepLastNImages(app, keep)` per app to cap old tagged Tengiz images (rollback-safe) and prints a summary parsed from Docker's output. A pure helper `buildCleanupArgs` and a pure parser `parsePruneSummary` keep the Docker-dependent surface minimal and unit-testable without a Docker daemon.

**Tech Stack:** Go 1.26, `os/exec` (Docker CLI, no SDK), Cobra (CLI), existing `config.Store` and `runtime.Manager` interfaces.

## Global Constraints

- Command name is exactly `tengiz cleanup`
- Protect all containers labeled `tengiz-app=<appname>` from pruning (label key const `labelKey = "tengiz-app"` already in `internal/runtime/docker.go`)
- Prune target: stopped non-Tengiz containers, dangling images, unused networks, and unused volumes **only when** `--volumes` is passed
- Volume pruning is opt-in (`--volumes`); default run must not delete volumes
- Use `docker system prune -f` (force flag, non-interactive) — Docker CLI via `os/exec`, never a Docker SDK
- Rollback must keep working: never run `docker system prune -a` (would delete tagged rollback images); Tengiz image retention stays with the existing `KeepLastNImages` (default keep 5)
- New `Cleanup` method on `Manager` requires updating every implementer: `dockerRuntime`, `stubManager`, and the test mock `mockRTForDeploy` in `internal/cli/root_test.go`
- `--env` flag comes from the root persistent flag (no new flag registration needed)
- No new external Go dependencies
- Existing tests must continue to pass without modification
- Documentation (README.md, AGENTS.md) and `docs/FUTURES_FEATURES.md` must be updated

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `CleanupOptions`, `CleanupReport` types; `buildCleanupArgs`; `parsePruneSummary`; `dockerRuntime.Cleanup` |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager` implementation |
| `internal/runtime/cleanup_test.go` | Tests: stub Cleanup, arg builder, prune summary parser |
| `internal/cli/root.go` | `cleanupCmd` command + registration + flags; `cleanupSummaryLines` helper |
| `internal/cli/root_test.go` | Add `Cleanup` stub to `mockRTForDeploy` (compile fix) |
| `internal/cli/cleanup_test.go` | Tests: command registration, flags, summary formatting, dry-run without Docker |
| `README.md` | Document `tengiz cleanup` under CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

Changes touch 3 new files and 6 existing files.

---

### Task 1: Add Cleanup types, Manager interface method, and stub

**Files:**
- Modify: `internal/runtime/cleanup.go:1-11` — add `CleanupOptions` and `CleanupReport` types above the existing `RemoveImage`
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface
- Modify: `internal/runtime/runtime.go:113-119` — add `Cleanup` to `stubManager`
- Modify: `internal/runtime/cleanup_test.go:1-20` — add `TestStubCleanup`
- Modify: `internal/cli/root_test.go:69-100` — add `Cleanup` method to `mockRTForDeploy` (compile fix)

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `runtime.CleanupOptions struct { Volumes bool }`
  - `runtime.CleanupReport struct { ContainersRemoved, ImagesRemoved, NetworksRemoved, VolumesRemoved int; ReclaimedSpace string }`
  - `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{Volumes: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 ||
		report.NetworksRemoved != 0 || report.VolumesRemoved != 0 || report.ReclaimedSpace != "" {
		t.Errorf("expected empty report, got %+v", report)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: FAIL — does not compile with `undefined: CleanupOptions` and `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add the types to `internal/runtime/cleanup.go`**

At the top of `internal/runtime/cleanup.go` (after the `import` block, before `RemoveImage`):

```go
type CleanupOptions struct {
	Volumes bool // also prune unused volumes (opt-in; removes data)
}

type CleanupReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	NetworksRemoved   int
	VolumesRemoved    int
	ReclaimedSpace    string
}
```

- [ ] **Step 4: Add `Cleanup` to the `Manager` interface in `internal/runtime/runtime.go`**

In the `Manager` interface (after the `KeepLastNImages` line, currently line 36):

```go
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)
```

- [ ] **Step 5: Add `Cleanup` to `stubManager` in `internal/runtime/runtime.go`**

After the `stubManager.KeepLastNImages` method (currently around line 119):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return &CleanupReport{}, nil
}
```

- [ ] **Step 6: Fix the test mock in `internal/cli/root_test.go`**

Add this method to `mockRTForDeploy` (after its `KeepLastNImages` method, currently line 99). Without it the CLI package test files fail to compile:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupReport, error) {
	return &runtime.CleanupReport{}, nil
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 8: Verify CLI package still compiles and tests pass**

Run: `go test ./internal/cli/... -run "TestMockRTForDeployImplementsManager" -v -count=1`

Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup method to Manager interface and stub"
```

---

### Task 2: Implement `dockerRuntime.Cleanup` with pure helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `buildCleanupArgs`, `parsePruneSummary`, `dockerRuntime.Cleanup`
- Modify: `internal/runtime/cleanup_test.go` — add `TestBuildCleanupArgs`, `TestBuildCleanupArgsVolumes`, `TestParsePruneSummary`, `TestParsePruneSummaryVolumes`, `TestParsePruneSummaryEmpty`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupReport` (Task 1), `labelKey` const (`"tengiz-app"`, from `internal/runtime/docker.go`)
- Produces: `dockerRuntime.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)`, `buildCleanupArgs(opts CleanupOptions) []string`, `parsePruneSummary(output string) *CleanupReport`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestBuildCleanupArgs(t *testing.T) {
	args := buildCleanupArgs(CleanupOptions{})
	expected := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(args) != len(expected) {
		t.Fatalf("buildCleanupArgs() = %v, want %v", args, expected)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestBuildCleanupArgsVolumes(t *testing.T) {
	args := buildCleanupArgs(CleanupOptions{Volumes: true})
	expected := []string{"system", "prune", "-f", "--volumes", "--filter", "label!=tengiz-app"}
	if len(args) != len(expected) {
		t.Fatalf("buildCleanupArgs() = %v, want %v", args, expected)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestParsePruneSummary(t *testing.T) {
	output := `WARNING! This will remove:
  - all stopped containers
  - all networks not used by at least one container
  - all dangling images
  - all dangling build cache

Deleted Containers:
f44f9b81948b3919590d5f79a680d8378f1139b41952e219830a33027c80c867
792776e68ac9d75bce4092bc1b5cc17b779bc926ab04f4185aec9bf1c0d4641f

Deleted Networks:
network1
network2

Deleted Images:
untagged: hello-world@sha256:f3b3b28a45160805bb16542c9531888519430e9e6d6ffc09d72261b0d26ff74f
deleted: sha256:1815c82652c03bfd8644afda26fb184f2ed891d921b20a0703b46768f9755c57
deleted: sha256:45761469c965421a92a69cc50e92c01e0cfa94fe026cdd1233445ea00e96289a

Deleted build cache objects:
zkvg3lzxi1fxy4r0e1hwaop6o

Total reclaimed space: 1.84kB
`
	report := parsePruneSummary(output)
	if report.ContainersRemoved != 2 {
		t.Errorf("ContainersRemoved = %d, want 2", report.ContainersRemoved)
	}
	if report.NetworksRemoved != 2 {
		t.Errorf("NetworksRemoved = %d, want 2", report.NetworksRemoved)
	}
	if report.ImagesRemoved != 3 {
		t.Errorf("ImagesRemoved = %d, want 3", report.ImagesRemoved)
	}
	if report.VolumesRemoved != 0 {
		t.Errorf("VolumesRemoved = %d, want 0", report.VolumesRemoved)
	}
	if report.ReclaimedSpace != "1.84kB" {
		t.Errorf("ReclaimedSpace = %q, want %q", report.ReclaimedSpace, "1.84kB")
	}
}

func TestParsePruneSummaryVolumes(t *testing.T) {
	output := `Deleted Volumes:
demo-app_postgres_data

Total reclaimed space: 1.24GB
`
	report := parsePruneSummary(output)
	if report.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", report.VolumesRemoved)
	}
	if report.ReclaimedSpace != "1.24GB" {
		t.Errorf("ReclaimedSpace = %q, want %q", report.ReclaimedSpace, "1.24GB")
	}
}

func TestParsePruneSummaryEmpty(t *testing.T) {
	report := parsePruneSummary("")
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 ||
		report.NetworksRemoved != 0 || report.VolumesRemoved != 0 || report.ReclaimedSpace != "" {
		t.Errorf("expected empty report, got %+v", report)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestBuildCleanupArgs|TestParsePruneSummary" -v -count=1`

Expected: FAIL — does not compile with `undefined: buildCleanupArgs` and `undefined: parsePruneSummary`

- [ ] **Step 3: Write minimal implementation in `internal/runtime/cleanup.go`**

Append to `internal/runtime/cleanup.go`:

```go
func buildCleanupArgs(opts CleanupOptions) []string {
	args := []string{"system", "prune", "-f"}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	// Protect every container carrying the tengiz-app label (deployed apps,
	// stopped scale-to-zero containers, versioned deploy containers).
	args = append(args, "--filter", fmt.Sprintf("label!=%s", labelKey))
	return args
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	cmd := exec.CommandContext(ctx, "docker", buildCleanupArgs(opts)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return parsePruneSummary(string(out)), nil
}

func parsePruneSummary(output string) *CleanupReport {
	report := &CleanupReport{}
	section := ""
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Deleted Containers:"):
			section = "containers"
		case strings.HasPrefix(line, "Deleted Images:"):
			section = "images"
		case strings.HasPrefix(line, "Deleted Networks:"):
			section = "networks"
		case strings.HasPrefix(line, "Deleted Volumes:"):
			section = "volumes"
		case strings.HasPrefix(line, "Deleted build cache"):
			section = "buildcache"
		case strings.HasPrefix(line, "Total reclaimed space:"):
			report.ReclaimedSpace = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			section = ""
		default:
			switch section {
			case "containers":
				report.ContainersRemoved++
			case "images":
				report.ImagesRemoved++
			case "networks":
				report.NetworksRemoved++
			case "volumes":
				report.VolumesRemoved++
			}
		}
	}
	return report
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestBuildCleanupArgs|TestParsePruneSummary" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement dockerRuntime.Cleanup via docker system prune with label protection"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go:34-89` — register `cleanupCmd` and its flags in `init()`
- Modify: `internal/cli/root.go` — add `cleanupCmd` variable and `cleanupSummaryLines` helper (place near `buildLogsCmd`, after `runCmd`)
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupReport` (Tasks 1-2), `config.NewStoreWithEnv(dataDir, env)`, `store.ListApps()`, `rt.KeepLastNImages(ctx, app, keep)`, `getEnv(cmd)`
- Produces: `cleanupCmd *cobra.Command`, `cleanupSummaryLines(report *runtime.CleanupReport) []string`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlags(t *testing.T) {
	for _, name := range []string{"volumes", "keep", "dry-run"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup missing --%s flag", name)
		}
	}
}

func TestCleanupSummaryLines(t *testing.T) {
	report := &runtime.CleanupReport{
		ContainersRemoved: 2,
		ImagesRemoved:     5,
		NetworksRemoved:   1,
		VolumesRemoved:    0,
		ReclaimedSpace:    "1.55GB",
	}
	lines := cleanupSummaryLines(report)
	expected := []string{
		"[tengiz] containers removed: 2",
		"[tengiz] images removed: 5",
		"[tengiz] networks removed: 1",
		"[tengiz] volumes removed: 0",
		"[tengiz] reclaimed space: 1.55GB",
	}
	if len(lines) != len(expected) {
		t.Fatalf("cleanupSummaryLines() returned %d lines, want %d", len(lines), len(expected))
	}
	for i := range expected {
		if lines[i] != expected[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], expected[i])
		}
	}
}

func TestCleanupDryRun(t *testing.T) {
	tmpDir := t.TempDir()
	oldDataDir := dataDir
	dataDir = tmpDir
	defer func() { dataDir = oldDataDir }()

	store := config.NewStore(dataDir)
	if err := store.SaveApp(types.AppEntry{
		Name:   "testapp",
		Config: types.AppConfig{Name: "testapp"},
	}); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "testapp") {
		t.Errorf("dry-run output missing app name, got: %s", output)
	}
	if !strings.Contains(output, "dry-run") {
		t.Errorf("dry-run output missing marker, got: %s", output)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — does not compile with `undefined: cleanupCmd` and `undefined: cleanupSummaryLines`

- [ ] **Step 3: Add the command and helper to `internal/cli/root.go`**

Add the `cleanupCmd` variable and helper after the `runCmd` variable (near the end of the command definitions, before `gitCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long: `Removes stopped containers, dangling images, and unused networks to reclaim disk space.
Use --volumes to also prune unused volumes (removes data).

Containers labeled tengiz-app (all deployed apps, including stopped scale-to-zero
containers) are protected from pruning. Per-app image retention caps each app's
image count at --keep (default 5) so rollback keeps working.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		volumes, _ := cmd.Flags().GetBool("volumes")
		keep, _ := cmd.Flags().GetInt("keep")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		store := config.NewStoreWithEnv(dataDir, env)
		apps, err := store.ListApps()
		if err != nil {
			return fmt.Errorf("list apps: %w", err)
		}

		if dryRun {
			for _, app := range apps {
				fmt.Printf("[tengiz] (dry-run) would retain %d images for %s\n", keep, app.Name)
			}
			msg := "[tengiz] (dry-run) would prune stopped non-tengiz containers, dangling images, unused networks"
			if volumes {
				msg += " and unused volumes"
			}
			fmt.Println(msg)
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		for _, app := range apps {
			if err := rt.KeepLastNImages(cmd.Context(), app.Name, keep); err != nil {
				log.Printf("[tengiz] warning: image retention for %s: %v", app.Name, err)
			}
		}

		report, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{Volumes: volumes})
		if err != nil {
			return err
		}
		for _, line := range cleanupSummaryLines(report) {
			fmt.Println(line)
		}
		return nil
	},
}

func cleanupSummaryLines(report *runtime.CleanupReport) []string {
	return []string{
		fmt.Sprintf("[tengiz] containers removed: %d", report.ContainersRemoved),
		fmt.Sprintf("[tengiz] images removed: %d", report.ImagesRemoved),
		fmt.Sprintf("[tengiz] networks removed: %d", report.NetworksRemoved),
		fmt.Sprintf("[tengiz] volumes removed: %d", report.VolumesRemoved),
		fmt.Sprintf("[tengiz] reclaimed space: %s", report.ReclaimedSpace),
	}
}
```

- [ ] **Step 4: Register the command and flags in `init()`**

In `init()` in `internal/cli/root.go`, after `rootCmd.AddCommand(runCmd)` (currently line 67), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("volumes", false, "also prune unused volumes (removes data)")
	cleanupCmd.Flags().Int("keep", 5, "number of images to retain per app")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be done without changing anything")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Run all CLI tests**

Run: `go test ./internal/cli/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 4: Documentation and final verification

**Files:**
- Modify: `README.md` — add `### tengiz cleanup` section to CLI Reference
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI command list
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented

- [ ] **Step 1: Document `tengiz cleanup` in `README.md`**

Insert this section immediately after the `### tengiz build-logs <app> [deployment-id]` section (before `### tengiz run`):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--volumes` | Also prune unused volumes (removes data — use with care) |
| `--keep <n>` | Number of images to retain per app (default: 5) |
| `--dry-run` | Show what would be removed without changing anything |

Prunes stopped non-Tengiz containers, dangling images, unused networks, and (with `--volumes`) unused volumes. Containers labeled `tengiz-app` (all deployed apps, including stopped scale-to-zero containers) are protected by label-based filtering. Per-app image retention (`--keep`, default 5) reclaims space from old builds while keeping rollback working.
```

- [ ] **Step 2: Update `AGENTS.md`**

In the CLI command list (after the `tengiz preview deploy <app> <pr> → create/update preview deployment (webhook preferred)` line), add:

```markdown
tengiz cleanup [--volumes] [--keep N] [--dry-run] → prune unused Docker resources (Tengiz containers protected via label filter)
```

- [ ] **Step 3: Update `docs/FUTURES_FEATURES.md`**

1. In the P0 table row #6, change `**Docker Housekeeping** ⬜` to `**Docker Housekeeping** ✅`.
2. In the "✅ Implemented Features (Not Pending)" table, add a row:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-13) |
```

- [ ] **Step 4: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS. Note: the proxy package tests are slow (~2s each) due to TCP dial timeouts and the idle tests are time-sensitive — failures in those two packages unrelated to this change should be re-run individually before concluding a regression.

- [ ] **Step 5: Run static analysis**

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 6: Manual smoke test (requires Docker)**

Run:
```bash
go build -o tengiz .
./tengiz cleanup --dry-run
./tengiz cleanup
./tengiz cleanup --volumes
```

Expected: dry-run prints app names and the "would prune" message without invoking Docker; the real runs print `[tengiz] containers removed: N` … `[tengiz] reclaimed space: X`, and running apps remain reachable afterward.

- [ ] **Step 7: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** — `docs/FUTURES_FEATURES.md` feature #6 (Docker Housekeeping):
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → Task 2 `docker system prune` prunes stopped containers, unused networks, dangling images; `--volumes` adds unused volumes ✅
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → Task 2 `--filter label!=tengiz-app` ✅
- "`tengiz cleanup` komutu eklenebilir" → Task 3 `cleanupCmd` ✅
- Per-app image retention keeps rollback intact (existing `KeepLastNImages`) ✅
- Out of scope (noted, separate features): #56 Granular Docker Prune Operations, #103 Build Cache Management & Git GC, #47 Stale Container Detection.

**2. Placeholder scan** — No "TBD", "TODO", "implement later", "add error handling", or "similar to Task N" patterns. Every step contains complete, runnable code.

**3. Type consistency** —
- `CleanupOptions{Volumes bool}` defined in Task 1, used identically in Tasks 2 (`buildCleanupArgs`, `dockerRuntime.Cleanup`) and 3 (`runtime.CleanupOptions{Volumes: volumes}`) ✅
- `CleanupReport` fields: `ContainersRemoved`, `ImagesRemoved`, `NetworksRemoved`, `VolumesRemoved`, `ReclaimedSpace` — used consistently across the parser (Task 2), the stub (Task 1), and `cleanupSummaryLines` (Task 3) ✅
- `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error)` — implemented by `dockerRuntime`, `stubManager`, and `mockRTForDeploy` with matching signatures ✅
- `cleanupSummaryLines(report *runtime.CleanupReport) []string` — single definition, one caller ✅
- `KeepLastNImages(ctx, app.Name, keep)` — matches existing interface signature `KeepLastNImages(ctx context.Context, appName string, n int) error` ✅

**4. Interface implementers updated** — adding `Cleanup` to `Manager` breaks compilation of `stubManager` and `mockRTForDeploy` unless updated; Task 1 Step 5 and Step 6 cover both, and Task 1 Step 8 verifies the CLI test package compiles ✅

**5. Rollback safety** — no `-a`/`--all` flag anywhere; Tengiz tagged images (`tengiz-apps/<app>:<env>-<deploymentID>`) are never dangling so `system prune` never touches them; only `KeepLastNImages` removes old Tengiz images ✅
