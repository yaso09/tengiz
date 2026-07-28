# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command for label-based Docker system prune operations to reclaim disk space on single-server deployments.

**Architecture:** Three-layer design: (1) Manager interface gets 5 new prune methods, (2) dockerRuntime implements them via `docker <resource> prune` with `--filter label!=tengiz-app` protection, (3) CLI command `tengiz cleanup` with per-category flags and `--dry-run` support.

**Tech Stack:** Go 1.26, Cobra CLI, `os/exec` for Docker CLI calls, existing label conventions (`tengiz-app`, `tengiz-env`).

## Global Constraints

- All new Docker commands must use `exec.CommandContext` with `CombinedOutput()` and wrap errors with `fmt.Errorf("docker <cmd>: %w\n%s", err, out)`
- Container label keys: `tengiz-app` (value = app name) and `tengiz-env` (value = environment) — never prune containers with `tengiz-app` label
- Image naming: `tengiz-apps/{app}:{tag}` — protect tengiz images from pruning
- New files go in `internal/runtime/` or `internal/cli/` following existing package patterns
- All stub methods on `stubManager` must return nil
- All new Manager methods must be tested via stub in `runtime_test.go`
- Port range: 9000-9999 (unused for this feature but documented for context)
- Container naming: `tengiz-{name}` or `tengiz-{name}-{env}` for non-production

---

### Task 1: Add Prune Methods to Manager Interface + Stub

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (Manager interface), `internal/runtime/runtime.go:51-123` (stubManager)
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: Existing `Manager` interface, `stubManager` struct
- Produces: 5 new methods on `Manager` interface, stub implementations

- [ ] **Step 1: Add 5 prune methods to Manager interface**

Add after line 48 (after `Run` method):

```go
PruneContainers(ctx context.Context) error
PruneImages(ctx context.Context, all bool) error
PruneVolumes(ctx context.Context) error
PruneNetworks(ctx context.Context) error
PruneBuildCache(ctx context.Context) error
```

The final interface block becomes (lines 31-54):

```go
type Manager interface {
	Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error
	CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	RemoveBySuffix(ctx context.Context, name string, suffix string) error
	IsActive(ctx context.Context, name string) (bool, error)
	GetContainerPort(ctx context.Context, name string, suffix string) (int, error)
	List(ctx context.Context) ([]types.AppStatus, error)
	Logs(ctx context.Context, name string, opts LogOptions) (io.ReadCloser, error)
	WaitForReady(ctx context.Context, name string, internalPort int) error
	WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	PruneContainers(ctx context.Context) error
	PruneImages(ctx context.Context, all bool) error
	PruneVolumes(ctx context.Context) error
	PruneNetworks(ctx context.Context) error
	PruneBuildCache(ctx context.Context) error
}
```

- [ ] **Step 2: Run tests to verify interface compile check passes**

Run: `go vet ./internal/runtime/`
Expected: PASS (compilation error because stubManager doesn't implement the new methods)

- [ ] **Step 3: Add stub implementations to stubManager**

Add after line 122 (after `Run` stub):

```go
func (m *stubManager) PruneContainers(_ context.Context) error { return nil }
func (m *stubManager) PruneImages(_ context.Context, _ bool) error { return nil }
func (m *stubManager) PruneVolumes(_ context.Context) error { return nil }
func (m *stubManager) PruneNetworks(_ context.Context) error { return nil }
func (m *stubManager) PruneBuildCache(_ context.Context) error { return nil }
```

- [ ] **Step 4: Add interface satisfaction test**

Add to `runtime_test.go`:

```go
func TestStubPruneMethods(t *testing.T) {
	m := NewStub()
	if err := m.PruneContainers(context.Background()); err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
	if err := m.PruneImages(context.Background(), false); err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if err := m.PruneImages(context.Background(), true); err != nil {
		t.Fatalf("PruneImages(all) error = %v", err)
	}
	if err := m.PruneVolumes(context.Background()); err != nil {
		t.Fatalf("PruneVolumes() error = %v", err)
	}
	if err := m.PruneNetworks(context.Background()); err != nil {
		t.Fatalf("PruneNetworks() error = %v", err)
	}
	if err := m.PruneBuildCache(context.Background()); err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
}
```

- [ ] **Step 5: Run tests to verify all pass**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go
git commit -m "feat: add prune methods to runtime Manager interface + stub"
```

---

### Task 2: Implement Docker Prune Methods

**Files:**
- Create: `internal/runtime/docker_prune.go`
- Modify: `internal/runtime/cleanup_test.go` (add test stubs)
- Referenced: `internal/runtime/docker.go` (label constants at lines 76-77)

**Interfaces:**
- Consumes: `Manager` interface from Task 1, `dockerRuntime` struct from `docker.go`, label constants `labelKey`, `envLabelKey`
- Produces: Full docker exec implementations of all 5 prune methods on `*dockerRuntime`

- [ ] **Step 1: Write the failing test**

Modify `internal/runtime/cleanup_test.go` — append these test functions after the existing ones:

```go
func TestDockerRuntimePruneMethodsCompile(t *testing.T) {
	var _ Manager = &dockerRuntime{}
}

func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	if err := m.PruneContainers(context.Background()); err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
}

func TestStubPruneImages(t *testing.T) {
	m := NewStub()
	if err := m.PruneImages(context.Background(), false); err != nil {
		t.Fatalf("PruneImages() error = %v", err)
	}
	if err := m.PruneImages(context.Background(), true); err != nil {
		t.Fatalf("PruneImages(all) error = %v", err)
	}
}

func TestStubPruneVolumes(t *testing.T) {
	m := NewStub()
	if err := m.PruneVolumes(context.Background()); err != nil {
		t.Fatalf("PruneVolumes() error = %v", err)
	}
}

func TestStubPruneNetworks(t *testing.T) {
	m := NewStub()
	if err := m.PruneNetworks(context.Background()); err != nil {
		t.Fatalf("PruneNetworks() error = %v", err)
	}
}

func TestStubPruneBuildCache(t *testing.T) {
	m := NewStub()
	if err := m.PruneBuildCache(context.Background()); err != nil {
		t.Fatalf("PruneBuildCache() error = %v", err)
	}
}
```

- [ ] **Step 2: Run to verify the compile-time check fails (dockerRuntime doesn't implement Prune methods yet)**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: COMPILE ERROR — `*dockerRuntime does not implement Manager` (missing PruneContainers, PruneImages, etc.)

- [ ] **Step 3: Create `internal/runtime/docker_prune.go` with Docker implementations**

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
)

func (r *dockerRuntime) PruneContainers(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "container", "prune",
		"--filter", fmt.Sprintf("label!=%s", labelKey),
		"-f",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context, all bool) error {
	args := []string{"image", "prune", "-f"}
	if all {
		args = append(args, "-a")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-a", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify**

Run: `go test ./internal/runtime/ -v -count=1`
Expected: ALL PASS (stub tests pass, compile-time check passes now that dockerRuntime implements the interface)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/docker_prune.go internal/runtime/cleanup_test.go
git commit -m "feat: implement Docker prune methods in dockerRuntime"
```

---

### Task 3: Create `tengiz cleanup` CLI Command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:37-89` (init function — register cleanup command)

**Interfaces:**
- Consumes: `runtime.Manager` (all 5 prune methods), `runtime.NewDocker()`, `getEnv(cmd)`, `dataDir`
- Produces: Cobra command `cleanupCmd` registered at `rootCmd`

- [ ] **Step 1: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prune unused Docker resources (containers, images, volumes, networks, build cache).
Uses label-based filtering to protect Tengiz-managed containers from deletion.
Use flags to select which resource types to clean.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		allImages, _ := cmd.Flags().GetBool("all-images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if !all && !containers && !images && !volumes && !networks && !buildCache {
			return fmt.Errorf("specify at least one resource type to clean (use --all or a specific flag)")
		}

		if all && dryRun {
			fmt.Println("[tengiz] Dry-run mode: showing what would be pruned")
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("runtime: %w", err)
		}

		if dryRun {
			return dryRunCleanup(rt, all, containers, images, allImages, volumes, networks, buildCache)
		}
		return runCleanup(rt, all, containers, images, allImages, volumes, networks, buildCache)
	},
}

func runCleanup(rt runtime.Manager, all, containers, images, allImages, volumes, networks, buildCache bool) error {
	ctx := context.Background()

	if all || containers {
		fmt.Println("[tengiz] Pruning stopped containers...")
		if err := rt.PruneContainers(ctx); err != nil {
			fmt.Printf("[tengiz] container prune error: %v\n", err)
		}
	}
	if all || images {
		fmt.Println("[tengiz] Pruning dangling images...")
		if err := rt.PruneImages(ctx, allImages || all); err != nil {
			fmt.Printf("[tengiz] image prune error: %v\n", err)
		}
	}
	if all || volumes {
		fmt.Println("[tengiz] Pruning unused volumes...")
		if err := rt.PruneVolumes(ctx); err != nil {
			fmt.Printf("[tengiz] volume prune error: %v\n", err)
		}
	}
	if all || networks {
		fmt.Println("[tengiz] Pruning unused networks...")
		if err := rt.PruneNetworks(ctx); err != nil {
			fmt.Printf("[tengiz] network prune error: %v\n", err)
		}
	}
	if all || buildCache {
		fmt.Println("[tengiz] Pruning build cache...")
		if err := rt.PruneBuildCache(ctx); err != nil {
			fmt.Printf("[tengiz] build cache prune error: %v\n", err)
		}
	}

	fmt.Println("[tengiz] Cleanup complete")
	return nil
}

func dryRunCleanup(rt runtime.Manager, all, containers, images, allImages, volumes, networks, buildCache bool) error {
	if all || containers {
		fmt.Println("[tengiz] Would prune: stopped containers (excluding tengiz-* labeled)")
	}
	if all || images {
		mode := "dangling"
		if allImages || all {
			mode = "all unused"
		}
		fmt.Printf("[tengiz] Would prune: %s images\n", mode)
	}
	if all || volumes {
		fmt.Println("[tengiz] Would prune: unused volumes")
	}
	if all || networks {
		fmt.Println("[tengiz] Would prune: unused networks")
	}
	if all || buildCache {
		fmt.Println("[tengiz] Would prune: build cache")
	}

	fmt.Println("[tengiz] Dry-run complete — no resources were deleted (use without --dry-run to execute)")
	return nil
}

func init() {
	cleanupCmd.Flags().BoolP("all", "A", false, "Prune all resource types (containers, images, volumes, networks, build cache)")
	cleanupCmd.Flags().BoolP("containers", "c", false, "Prune stopped containers")
	cleanupCmd.Flags().BoolP("images", "i", false, "Prune dangling images")
	cleanupCmd.Flags().Bool("all-images", false, "Prune all unused images (requires --images or --all)")
	cleanupCmd.Flags().BoolP("volumes", "v", false, "Prune unused volumes")
	cleanupCmd.Flags().BoolP("networks", "n", false, "Prune unused networks")
	cleanupCmd.Flags().BoolP("build-cache", "b", false, "Prune build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "Show what would be pruned without deleting")
}
```

- [ ] **Step 2: Register cleanup command in root.go**

Add to `init()` in `internal/cli/root.go` — insert after `rootCmd.AddCommand(runCmd)` (around line 67):

```go
rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 3: Verify compilation**

Run: `go build -o tengiz .`
Expected: Binary compiles successfully with `tengiz cleanup` command available

Run: `go vet ./...`
Expected: PASS

- [ ] **Step 4: Quick smoke test (if Docker available)**

Run: `./tengiz cleanup --help`
Expected: Shows cleanup command help with all flags

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup CLI command for Docker housekeeping"
```

---

### Task 4: Enable Auto-Cleanup After Deploy

**Files:**
- Modify: `internal/gitdeploy/deployer.go` (after successful deploy)
- Modify: `internal/cli/root.go` deploy command (after successful deploy)

**Interfaces:**
- Consumes: `runtime.Manager.PruneImages(ctx, false)`, `runtime.Manager.PruneContainers(ctx)`
- Produces: Automatic dangling image + stopped container cleanup after each deploy

- [ ] **Step 1: Add auto-cleanup to gitdeploy Pipeline.Deploy**

Find `internal/gitdeploy/deployer.go` and locate the end of `Deploy()` method (after `p.rt.KeepLastNImages(...)` call). Add:

```go
// Prune old resources after deploy
if err := p.rt.PruneContainers(ctx); err != nil {
	log.Printf("[gitdeploy] container prune error: %v", err)
}
if err := p.rt.PruneImages(ctx, false); err != nil {
	log.Printf("[gitdeploy] image prune error: %v", err)
}
```

- [ ] **Step 2: Add auto-cleanup to CLI deploy command**

Find the `deployCmd` RunE in `internal/cli/root.go`. After successful deploy (after `store.SaveApp(...)` and any post-deploy steps), add:

```go
fmt.Println("[tengiz] Running post-deploy cleanup...")
if err := rt.PruneContainers(context.Background()); err != nil {
	log.Printf("[tengiz] container prune error: %v", err)
}
if err := rt.PruneImages(context.Background(), false); err != nil {
	log.Printf("[tengiz] image prune error: %v", err)
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build -o tengiz .`
Expected: Binary compiles

Run: `go vet ./...`
Expected: PASS

- [ ] **Step 4: Run all tests**

Run: `go test ./... -v -count=1`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gitdeploy/deployer.go internal/cli/root.go
git commit -m "feat: auto-cleanup dangling images and stopped containers after deploy"
```
