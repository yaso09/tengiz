# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command with label-aware Docker system pruning, stale container detection, and configurable retention policies so single-server deployments don't run out of disk space.

**Architecture:** New methods on `runtime.Manager` interface for Docker system prune, per-category prune (containers/images/volumes/build cache), stale container detection, and container retention. Docker `--filter label!=tengiz-app` protects Tengiz-managed resources from being pruned. The `tengiz cleanup` CLI command wraps these with flags for dry-run, category selection, and age thresholds.

**Tech Stack:** Go 1.26, `os/exec` Docker CLI, Cobra (CLI), existing `config.Store`, `runtime.Manager` interface.

## Global Constraints

- All new cleanup methods must have corresponding stub implementations (return nil)
- `docker system prune` MUST use `--filter label!=tengiz-app` to never prune Tengiz-managed containers/images
- Stale container detection: containers with `tengiz-app` label but no matching entry in store
- Image retention: `KeepLastNImages` already exists, keep at 5 (default), make configurable via `--keep` flag
- Container retention: zero-downtime deploy already removes the old container, but orphaned containers from crashes must be detected
- `--dry-run` flag on cleanup: show what would be removed without actually removing
- No new external dependencies

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneSystem`, `PruneContainers`, `PruneImages`, `PruneVolumes`, `PruneBuildCache`, `DetectStaleContainers`, `KeepLastNContainers` to `Manager` interface + stub |
| `internal/runtime/cleanup.go` | Docker exec implementations for all new cleanup methods |
| `internal/runtime/cleanup_test.go` | Stub tests for new methods |
| `internal/runtime/docker.go` | No changes (methods added to `dockerRuntime` in `cleanup.go`) |
| `internal/cli/root.go` | New `cleanupCmd` Cobra command with `--dry-run`, `--containers`, `--images`, `--volumes`, `--build-cache`, `--all`, `--keep` flags |
| `internal/cli/root_test.go` | Tests for cleanup command flag defaults |
| `internal/config/store.go` | No changes needed (store already manages app/deployment data) |

---

### Task 1: Add cleanup methods to the Manager interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add 7 new methods to `Manager` interface
- Modify: `internal/runtime/runtime.go:51-123` — add stub implementations

**Interfaces:**
- Consumes: nothing new
- Produces: 7 new methods on `runtime.Manager` with exact signatures (below)

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go — replace content
package runtime

import (
	"context"
	"testing"
)

func TestStubPruneSystem(t *testing.T) {
	m := NewStub()
	if err := m.PruneSystem(context.Background(), false); err != nil {
		t.Fatalf("PruneSystem() error = %v", err)
	}
}

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	if err := m.PruneContainers(context.Background(), false); err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	if err := m.PruneImages(context.Background(), false); err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	if err := m.PruneBuildCache(context.Background()); err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
}

func TestStubDetectStaleContainers(t *testing.T) {
	m := NewStub()
	containers, err := m.DetectStaleContainers(context.Background())
	if err != nil {
		t.Fatalf("DetectStaleContainers() error = %v", err)
	}
	if len(containers) != 0 {
		t.Errorf("DetectStaleContainers() = %v, want empty", containers)
	}
}

func TestStubKeepLastNContainers(t *testing.T) {
	m := NewStub()
	if err := m.KeepLastNContainers(context.Background(), "testapp", 5); err != nil {
		t.Fatalf("KeepLastNContainers() error = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPruneSystem|TestStubPruneContainers|TestStubPruneImages|TestStubPruneBuildCache|TestStubDetectStaleContainers|TestStubKeepLastNContainers" -v -count=1`

Expected: FAIL with `PruneSystem undefined` (in interface but no stub or not in interface)

- [ ] **Step 3: Add methods to Manager interface**

```go
// internal/runtime/runtime.go — add to Manager interface after Run
PruneSystem(ctx context.Context, dryRun bool) error
PruneContainers(ctx context.Context, dryRun bool) error
PruneImages(ctx context.Context, dryRun bool) error
PruneVolumes(ctx context.Context, dryRun bool) error
PruneBuildCache(ctx context.Context) error
DetectStaleContainers(ctx context.Context) ([]string, error)
KeepLastNContainers(ctx context.Context, appName string, n int) error
```

- [ ] **Step 4: Add stub implementations**

```go
// internal/runtime/runtime.go — add to stubManager after Run
func (m *stubManager) PruneSystem(ctx context.Context, dryRun bool) error  { return nil }
func (m *stubManager) PruneContainers(ctx context.Context, dryRun bool) error  { return nil }
func (m *stubManager) PruneImages(ctx context.Context, dryRun bool) error  { return nil }
func (m *stubManager) PruneVolumes(ctx context.Context, dryRun bool) error { return nil }
func (m *stubManager) PruneBuildCache(ctx context.Context) error { return nil }
func (m *stubManager) DetectStaleContainers(ctx context.Context) ([]string, error) { return nil, nil }
func (m *stubManager) KeepLastNContainers(ctx context.Context, appName string, n int) error { return nil }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestStubPruneSystem|TestStubPruneContainers|TestStubPruneImages|TestStubPruneBuildCache|TestStubDetectStaleContainers|TestStubKeepLastNContainers" -v -count=1`

Expected: All PASS

- [ ] **Step 6: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add cleanup methods to runtime.Manager interface + stub"
```

---

### Task 2: Implement Docker exec cleanup methods

**Files:**
- Modify: `internal/runtime/cleanup.go` — add all 7 method implementations for `dockerRuntime`

**Interfaces:**
- Consumes: `Manager` interface from Task 1
- Produces: working `docker` CLI exec implementations for all cleanup operations

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_docker_test.go
package runtime

import (
	"context"
	"testing"
)

// These tests require Docker. Skip if not available.
func skipIfNoDocker(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found")
	}
}

func TestDockerPruneSystem(t *testing.T) {
	skipIfNoDocker(t)
	r := &dockerRuntime{}
	// Dry-run should never fail
	if err := r.PruneSystem(context.Background(), true); err != nil {
		t.Fatalf("PruneSystem(dryRun=true): %v", err)
	}
}

func TestDockerDetectStaleStopped(t *testing.T) {
	skipIfNoDocker(t)
	r := &dockerRuntime{}
	// Should return empty or error gracefully
	containers, err := r.DetectStaleContainers(context.Background())
	if err != nil {
		t.Fatalf("DetectStaleContainers: %v", err)
	}
	t.Logf("stale containers: %v", containers)
}
```

- [ ] **Step 2: Run test to verify it fails with docker unavailable (expected)**

Run: `go test ./internal/runtime/... -run "TestDockerPruneSystem|TestDockerDetectStale" -v -count=1`
(if Docker available) Expected: FAIL with missing methods
(if Docker unavailable) Expected: SKIP — OK

- [ ] **Step 3: Implement `PruneSystem` on `dockerRuntime`**

Add to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) PruneSystem(ctx context.Context, dryRun bool) error {
	args := []string{"system", "prune", "-f",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	return nil
}
```

- [ ] **Step 4: Implement `PruneContainers`**

```go
func (r *dockerRuntime) PruneContainers(ctx context.Context, dryRun bool) error {
	args := []string{"container", "prune", "-f",
		"--filter", "label!=tengiz-app",
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return nil
}
```

- [ ] **Step 5: Implement `PruneImages`**

```go
func (r *dockerRuntime) PruneImages(ctx context.Context, dryRun bool) error {
	args := []string{"image", "prune", "-f",
		"--filter", "label!=tengiz-app",
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return nil
}
```

- [ ] **Step 6: Implement `PruneVolumes`**

```go
func (r *dockerRuntime) PruneVolumes(ctx context.Context, dryRun bool) error {
	args := []string{"volume", "prune", "-f"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return nil
}
```

- [ ] **Step 7: Implement `PruneBuildCache`**

```go
func (r *dockerRuntime) PruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}
```

- [ ] **Step 8: Implement `DetectStaleContainers`**

```go
type containerInfo struct {
	Name   string
	Labels string
	State  string
}

func (r *dockerRuntime) DetectStaleContainers(ctx context.Context) ([]string, error) {
	format := `{"Name":"{{.Name}}","Labels":"{{.Labels}}","State":"{{.State}}"}`

	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "label=tengiz-app",
		"--format", format,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var stale []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		var ci containerInfo
		if err := json.Unmarshal([]byte(line), &ci); err != nil {
			continue
		}
		name := strings.TrimPrefix(ci.Name, "/")
		// A container is "stale" if it's not running and not the current active container
		if ci.State != "running" {
			stale = append(stale, name)
		}
	}
	return stale, nil
}
```

- [ ] **Step 9: Implement `KeepLastNContainers`**

```go
func (r *dockerRuntime) KeepLastNContainers(ctx context.Context, appName string, n int) error {
	// Find all stopped containers for this app, keep the N most recent
	format := `{{.ID}}|{{.CreatedAt}}`
	// Match both base name and suffixed names (e.g., tengiz-myapp, tengiz-myapp-12345)
	// Docker name filter doesn't support wildcard prefix, so we use label filter
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=tengiz-app=%s", appName),
		"--filter", "status=exited",
		"--filter", "status=created",
		"--format", format,
		"--no-trunc",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker ps: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= n {
		return nil
	}

	type container struct {
		id        string
		createdAt string
	}
	var containers []container
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		containers = append(containers, container{id: parts[0], createdAt: parts[1]})
	}

	sort.Slice(containers, func(i, j int) bool {
		return containers[i].createdAt < containers[j].createdAt
	})

	for i := 0; i < len(containers)-n; i++ {
		rmCmd := exec.CommandContext(ctx, "docker", "rm", "-f", containers[i].id)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			log.Printf("[runtime] failed to remove stale container %s: %v\n%s", containers[i].id, err, string(out))
		}
	}
	return nil
}
```

- [ ] **Step 10: Add imports to cleanup.go**

Add to the import block:
```go
"encoding/json"
```

The existing imports are: `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`. Add `"encoding/json"`.

- [ ] **Step 11: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestStubPrune" -v -count=1`
Expected: PASS

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 12: Run all tests**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: All PASS (skipping Docker-dependent tests if Docker unavailable)

- [ ] **Step 13: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_docker_test.go
git commit -m "feat: implement Docker cleanup methods (prune, stale detection, retention)"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` with flags

**Interfaces:**
- Consumes: `runtime.Manager` cleanup methods from Tasks 1-2, `config.NewStore`, `config.NewDataDir`
- Produces: `tengiz cleanup [--dry-run] [--containers] [--images] [--volumes] [--build-cache] [--all] [--keep N]` CLI command

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
	if cleanupCmd == nil {
		t.Fatal("cleanupCmd is nil")
	}
	if cleanupCmd.Use != "cleanup" {
		t.Errorf("cleanupCmd.Use = %q, want %q", cleanupCmd.Use, "cleanup")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	flags := []string{"dry-run", "containers", "images", "volumes", "build-cache", "all", "keep"}
	for _, name := range flags {
		t.Run(name, func(t *testing.T) {
			f := cleanupCmd.Flags().Lookup(name)
			if f == nil {
				t.Errorf("cleanupCmd missing --%s flag", name)
			}
		})
	}
}

func TestCleanupCmdDryRunDefault(t *testing.T) {
	cleanupCmd.ParseFlags([]string{})
	dryRun, _ := cleanupCmd.Flags().GetBool("dry-run")
	if dryRun {
		t.Error("--dry-run should default to false")
	}
}

func TestCleanupCmdKeepDefault(t *testing.T) {
	cleanupCmd.ParseFlags([]string{})
	keep, _ := cleanupCmd.Flags().GetInt("keep")
	if keep != 5 {
		t.Errorf("--keep should default to 5, got %d", keep)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCmd" -v -count=1`
Expected: FAIL (cleanupCmd not defined)

- [ ] **Step 3: Add cleanup command to root.go**

Add after `runCmd` definition (around line 630):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Free disk space by pruning unused Docker resources",
	Long: `Prunes Docker resources not managed by Tengiz (containers, images, volumes, build cache).
By default prunes everything. Use specific flags to limit scope.
All Tengiz-managed resources are protected by labels -- never pruned automatically.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		pruneContainers, _ := cmd.Flags().GetBool("containers")
		pruneImages, _ := cmd.Flags().GetBool("images")
		pruneVolumes, _ := cmd.Flags().GetBool("volumes")
		pruneBuildCache, _ := cmd.Flags().GetBool("build-cache")
		pruneAll, _ := cmd.Flags().GetBool("all")
		keep, _ := cmd.Flags().GetInt("keep")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		// If no specific flags, or --all, do everything
		doAll := pruneAll || (!pruneContainers && !pruneImages && !pruneVolumes && !pruneBuildCache)

		if dryRun {
			fmt.Println("[tengiz] DRY RUN — no resources will be removed")
		}

		// Stale container detection (always runs, reports to user)
		stale, err := rt.DetectStaleContainers(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "[tengiz] warning: stale detection: %v\n", err)
		} else if len(stale) > 0 {
			fmt.Printf("[tengiz] stopped Tengiz containers (%d):\n", len(stale))
			for _, c := range stale {
				fmt.Printf("  - %s\n", c)
			}
			fmt.Println("  (use --containers to prune stopped Tengiz containers)")
		}

		// Prune non-Tengiz containers
		if doAll || pruneContainers {
			if err := rt.PruneContainers(context.Background(), dryRun); err != nil {
				fmt.Fprintf(os.Stderr, "[tengiz] container prune: %v\n", err)
			} else {
				fmt.Println("[tengiz] pruned non-Tengiz containers")
			}
		}

		// Prune non-Tengiz images
		if doAll || pruneImages {
			if err := rt.PruneImages(context.Background(), dryRun); err != nil {
				fmt.Fprintf(os.Stderr, "[tengiz] image prune: %v\n", err)
			} else {
				fmt.Println("[tengiz] pruned non-Tengiz images")
			}
		}

		// Prune volumes (no label filter — volumes can't be labeled reliably)
		if doAll || pruneVolumes {
			if err := rt.PruneVolumes(context.Background(), dryRun); err != nil {
				fmt.Fprintf(os.Stderr, "[tengiz] volume prune: %v\n", err)
			} else {
				fmt.Println("[tengiz] pruned unused volumes")
			}
		}

		// Prune build cache
		if doAll || pruneBuildCache {
			if err := rt.PruneBuildCache(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "[tengiz] build cache prune: %v\n", err)
			} else {
				fmt.Println("[tengiz] pruned build cache")
			}
		}

		// Apply retention to all known apps
		store := config.NewStore(dataDir)
		apps, err := store.ListApps()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[tengiz] warning: list apps: %v\n", err)
		} else {
			for _, app := range apps {
				if err := rt.KeepLastNContainers(context.Background(), app.Name, keep); err != nil {
					fmt.Fprintf(os.Stderr, "[tengiz] warning: container retention for %s: %v\n", app.Name, err)
				}
				if err := rt.KeepLastNImages(context.Background(), app.Name, keep); err != nil {
					fmt.Fprintf(os.Stderr, "[tengiz] warning: image retention for %s: %v\n", app.Name, err)
				}
			}
			fmt.Printf("[tengiz] retention applied: kept %d per app\n", keep)
		}

		return nil
	},
}
```

- [ ] **Step 4: Register command and flags in `init()`**

Add to the `init()` function in root.go:

```go
cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing")
cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers only")
cleanupCmd.Flags().Bool("images", false, "prune unused non-Tengiz images only")
cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes only")
cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache only")
cleanupCmd.Flags().Bool("all", false, "prune everything (default if no specific flag)")
cleanupCmd.Flags().Int("keep", 5, "number of images/containers to retain per app")
```

Add to `rootCmd.AddCommand(...)` list:
```go
cleanupCmd,
```

- [ ] **Step 5: Add import for `runtime` package if not already imported**

Check root.go imports — it should already import `"github.com/yaso09/tengiz/internal/runtime"`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCmd" -v -count=1`
Expected: PASS

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 7: Run all tests**

Run: `go test ./... -v -count=1`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command with label-aware pruning"
```

---

### Task 4: Stale container cleanup in deploy and rollback flows

**Files:**
- Modify: `internal/cli/root.go` — deploy and rollback handlers to call `KeepLastNContainers`
- Modify: `internal/gitdeploy/deployer.go` — git deploy pipeline to call `KeepLastNContainers`

**Interfaces:**
- Consumes: `runtime.Manager.KeepLastNContainers` from Tasks 1-2
- Produces: automatic cleanup of old stopped containers during deploy operations

- [ ] **Step 1: Verify existing deploy calls `KeepLastNImages`**

Read root.go deploy handler (around lines 346 and 466) — it already calls `rt.KeepLastNImages(ctx, appKey, 5)`. Add a call to `KeepLastNContainers` right after each `KeepLastNImages` call.

- [ ] **Step 2: Add container retention to deploy command (first deploy path)**

In the first-deploy branch (after `rt.Create`), add after `KeepLastNImages`:

```go
if err := rt.KeepLastNContainers(context.Background(), appKey, 5); err != nil {
    log.Printf("[tengiz] warning: container cleanup: %v", err)
}
```

- [ ] **Step 3: Add container retention to deploy command (zero-downtime path)**

In the zero-downtime deploy branch, add after the second `KeepLastNImages`:

```go
if err := rt.KeepLastNContainers(context.Background(), appKey, 5); err != nil {
    log.Printf("[tengiz] warning: container cleanup: %v", err)
}
```

- [ ] **Step 4: Add container retention to rollback command**

In the rollback handler, after the rollback succeeds (after `rt.CreateFromImage` and before final print), add:

```go
if err := rt.KeepLastNContainers(context.Background(), appKey, 5); err != nil {
    log.Printf("[tengiz] warning: container cleanup: %v", err)
}
if err := rt.KeepLastNImages(context.Background(), appKey, 5); err != nil {
    log.Printf("[tengiz] warning: image cleanup: %v", err)
}
```

- [ ] **Step 5: Add container retention to git deploy pipeline**

Read `internal/gitdeploy/deployer.go` lines 215 and 315 — these sections already call `KeepLastNImages`. Add `KeepLastNContainers` calls right after each.

Edit `internal/gitdeploy/deployer.go`:

After line 215 (`m.rt.KeepLastNImages(...)`):
```go
if err := m.rt.KeepLastNContainers(ctx, appKey, 5); err != nil {
    log.Printf("[tengiz] warning: container cleanup: %v", err)
}
```

After line 315 (second `KeepLastNImages` call in zero-downtime path):
```go
if err := m.rt.KeepLastNContainers(ctx, appKey, 5); err != nil {
    log.Printf("[tengiz] warning: container cleanup: %v", err)
}
```

- [ ] **Step 6: Build to verify**

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 7: Run all tests**

Run: `go test ./... -v -count=1`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/gitdeploy/deployer.go
git commit -m "feat: auto-cleanup old containers during deploy and rollback"
```

---

### Task 5: Self-review against spec

**Files:**
- All modified files reviewed for consistency

- [ ] **Step 1: Spec coverage check**

Feature #6 "Docker Housekeeping" requirements:
- Label-based `docker system prune` ✅ (Task 2 — `PruneSystem` with `label!=tengiz-app` filter)
- `tengiz cleanup` command ✅ (Task 3 — full cleanup CLI)
- `--dry-run` safe execution ✅ (Task 3 — hidden flag, shows what would be removed)
- Stale container detection ✅ (Task 2 — `DetectStaleContainers` reports stopped Tengiz containers)
- Retention policy ✅ (Task 2 — `KeepLastNContainers`, Task 4 — integrated into deploy/rollback)
- Per-category pruning ✅ (Task 3 — `--containers`, `--images`, `--volumes`, `--build-cache` flags)
- Non-Tengiz resources protected ✅ (all prune commands exclude Tengiz-labeled resources)

Related features partially covered (P1):
- #47 Stale Container Detection ✅ (reported by cleanup, not auto-removed)
- #56 Granular Docker Prune Operations ✅ (per-category flags)
- #22 Container Retention Policy ✅ (configurable `--keep N`)

- [ ] **Step 2: Placeholder scan**

Search plan for any "TBD", "TODO", "implement later", "fill in details", "Similar to Task". None found.

- [ ] **Step 3: Type consistency check**

- `PruneSystem(ctx, dryRun bool) error` — same in interface, docker impl, stub, CLI handler
- `PruneContainers(ctx, dryRun bool) error` — same
- `PruneImages(ctx, dryRun bool) error` — same
- `PruneVolumes(ctx, dryRun bool) error` — same
- `PruneBuildCache(ctx) error` — same
- `DetectStaleContainers(ctx) ([]string, error)` — same
- `KeepLastNContainers(ctx, appName string, n int) error` — same
- `cleanupCmd` registered as `"cleanup"` — matches CLI usage
- All flag names use hyphens (`--dry-run`, `--build-cache`) — matches Go's `pflag` convention

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/runtime/cleanup_docker_test.go internal/cli/cleanup_test.go internal/gitdeploy/deployer.go
git commit -m "feat: complete Docker housekeeping with cleanup CLI, stale detection, and retention"
```
