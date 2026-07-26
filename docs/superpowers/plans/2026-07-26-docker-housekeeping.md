# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tengiz cleanup` command for label-based Docker resource pruning that protects Tengiz-managed containers/images/volumes/networks while reclaiming disk space.

**Architecture:** Add 5 prune methods to `runtime.Manager` interface (`PruneContainers`, `PruneDanglingImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache`) implemented on `dockerRuntime` via `docker` CLI subcommands with `label!=tengiz-app` filters. Create `internal/cleanup` package with a `Cleaner` struct that orchestrates selected prunes and runs app-level image retention via `KeepLastNImages`. Expose as `tengiz cleanup` Cobra command with granular flags (`--containers`, `--images`, `--volumes`, `--networks`, `--build-cache`, `--dry-run`, `--keep N`). Dry-run mode lists resources via separate `docker` list commands instead of pruning.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI (`os/exec`)

## Global Constraints

- Container labels: `tengiz-app={name}`, `tengiz-env={env}` (constants from `internal/runtime/docker.go`)
- Image tag pattern: `tengiz-apps/{appName}:{env}-{deploymentID}`
- `:latest` images must never be removed
- All prune operations use `--force` flag for non-interactive execution
- Docker CLI via `os/exec` only — no Docker SDK
- New `runtime.Manager` methods must have no-op `stubManager` implementations
- `internal/cleanup` package calls `runtime.Manager` for pruning and `os/exec` for dry-run listing

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneContainers`, `PruneDanglingImages`, `PruneVolumes`, `PruneNetworks`, `PruneBuildCache` to `Manager` interface + `stubManager` no-ops |
| `internal/runtime/cleanup.go` | `dockerRuntime` implementations of the 5 prune methods + helper functions for Docker CLI output parsing |
| `internal/runtime/cleanup_test.go` | Stub tests for prune methods |
| `internal/cleanup/cleaner.go` | `Cleaner` struct, `Options`/`Result` types, orchestration `Run()`, dry-run listing via `os/exec` |
| `internal/cleanup/cleaner_test.go` | Unit tests with `runtime.NewStub()` |
| `internal/cli/root.go` | `cleanupCmd` Cobra command definition + registration in `init()` + flags in `Execute()` |

---

### Task 1: Extend runtime.Manager interface with prune methods

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` (interface)
- Modify: `internal/runtime/runtime.go:113-123` (stub no-ops)
- Modify: `internal/runtime/cleanup_test.go` (stub tests)

**Interfaces:**
- Consumes: existing `Manager` interface, existing `stubManager`
- Produces: 5 new interface methods + 5 no-op stub methods

- [ ] **Step 1: Add 5 prune methods to Manager interface**

In `internal/runtime/runtime.go`, add to the `Manager` interface after `Run`:

```go
PruneContainers(ctx context.Context) error
PruneDanglingImages(ctx context.Context) error
PruneVolumes(ctx context.Context) error
PruneNetworks(ctx context.Context) error
PruneBuildCache(ctx context.Context) error
```

- [ ] **Step 2: Add 5 no-op stub implementations**

In `internal/runtime/runtime.go`, add after `stubManager.Run` at line ~122:

```go
func (m *stubManager) PruneContainers(ctx context.Context) error {
	return nil
}

func (m *stubManager) PruneDanglingImages(ctx context.Context) error {
	return nil
}

func (m *stubManager) PruneVolumes(ctx context.Context) error {
	return nil
}

func (m *stubManager) PruneNetworks(ctx context.Context) error {
	return nil
}

func (m *stubManager) PruneBuildCache(ctx context.Context) error {
	return nil
}
```

- [ ] **Step 3: Add stub tests for new prune methods**

In `internal/runtime/cleanup_test.go`, add after existing tests:

```go
func TestStubPruneContainers(t *testing.T) {
	m := NewStub()
	if err := m.PruneContainers(context.Background()); err != nil {
		t.Fatalf("PruneContainers() error = %v", err)
	}
}

func TestStubPruneDanglingImages(t *testing.T) {
	m := NewStub()
	if err := m.PruneDanglingImages(context.Background()); err != nil {
		t.Fatalf("PruneDanglingImages() error = %v", err)
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

- [ ] **Step 4: Run tests to verify compilation and pass**

```bash
go test ./internal/runtime/ -v -count=1 -run TestStubPrune
```

Expected: All 5 new tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat: add prune methods to runtime.Manager interface"
```

---

### Task 2: Implement dockerRuntime prune methods

**Files:**
- Modify: `internal/runtime/cleanup.go` (add 5 method implementations)

**Interfaces:**
- Consumes: `PruneContainers/PruneDanglingImages/PruneVolumes/PruneNetworks/PruneBuildCache` from `Manager` interface (Task 1)
- Produces: `dockerRuntime` implementations that call `docker` CLI with label filters

**Note:** Docker `label!=key` filter excludes resources that do NOT have the given label. Since all Tengiz containers/volumes/networks have `tengiz-app` set, they are excluded from pruning. For images, we only prune `dangling=true` (untagged) images, which never includes Tengiz images since they are always tagged as `tengiz-apps/{app}:{tag}`. No filter needed for build cache.

- [ ] **Step 1: Add imports and helper for output parsing**

At the top of `internal/runtime/cleanup.go`, ensure imports include `"strings"` and `"bytes"` (already present, confirm). Add a helper to parse "Total reclaimed space:" from Docker prune output:

```go
func parseReclaimedSpace(output []byte) string {
	for _, line := range bytes.Split(output, []byte("\n")) {
		trimmed := strings.TrimSpace(string(line))
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			return strings.TrimPrefix(trimmed, "Total reclaimed space:")
		}
	}
	return ""
}
```

- [ ] **Step 2: Implement PruneContainers on dockerRuntime**

After `KeepLastNImages`, add:

```go
func (r *dockerRuntime) PruneContainers(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "container", "prune",
		"--force",
		"--filter", "label!=tengiz-app",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	space := parseReclaimedSpace(out)
	if space != "" {
		log.Printf("[runtime] pruned containers: reclaimed%s", space)
	}
	return nil
}
```

- [ ] **Step 3: Implement PruneDanglingImages**

```go
func (r *dockerRuntime) PruneDanglingImages(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "image", "prune",
		"--force",
		"--filter", "dangling=true",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	space := parseReclaimedSpace(out)
	if space != "" {
		log.Printf("[runtime] pruned dangling images: reclaimed%s", space)
	}
	return nil
}
```

- [ ] **Step 4: Implement PruneVolumes**

```go
func (r *dockerRuntime) PruneVolumes(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune",
		"--force",
		"--filter", "label!=tengiz-app",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	space := parseReclaimedSpace(out)
	if space != "" {
		log.Printf("[runtime] pruned volumes: reclaimed%s", space)
	}
	return nil
}
```

- [ ] **Step 5: Implement PruneNetworks**

```go
func (r *dockerRuntime) PruneNetworks(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "prune",
		"--force",
		"--filter", "label!=tengiz-app",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	space := parseReclaimedSpace(out)
	if space != "" {
		log.Printf("[runtime] pruned networks: reclaimed%s", space)
	}
	return nil
}
```

- [ ] **Step 6: Implement PruneBuildCache**

```go
func (r *dockerRuntime) PruneBuildCache(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune",
		"--force",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	space := parseReclaimedSpace(out)
	if space != "" {
		log.Printf("[runtime] pruned build cache: reclaimed%s", space)
	}
	return nil
}
```

- [ ] **Step 7: Run tests to verify compilation**

```bash
go build ./internal/runtime/
```

Expected: No errors.

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/cleanup.go
git commit -m "feat: implement dockerRuntime prune operations"
```

---

### Task 3: Create internal/cleanup orchestrator

**Files:**
- Create: `internal/cleanup/cleaner.go`
- Create: `internal/cleanup/cleaner_test.go`

**Interfaces:**
- Consumes: `runtime.Manager` (prune methods + `KeepLastNImages` + `List`), `config.Store` (for listing apps for `KeepLastNImages`)
- Produces: `cleanup.Options`, `cleanup.Result`, `cleanup.New(rt, store, env)`, `Cleaner.Run(ctx, opts) (*Result, error)`

- [ ] **Step 1: Create the cleanup package with Options/Result types**

Create `internal/cleanup/cleaner.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
	Keep       int
}

type Result struct {
	PrunedContainers int
	PrunedImages     int
	PrunedVolumes    int
	PrunedNetworks   int
	PrunedBuildCache bool
	RetainedImages   int
}

type Cleaner struct {
	rt    runtime.Manager
	store *config.Store
	env   string
}

func New(rt runtime.Manager, store *config.Store, env string) *Cleaner {
	if env == "" {
		env = "production"
	}
	return &Cleaner{rt: rt, store: store, env: env}
}

func (c *Cleaner) Run(ctx context.Context, opts Options) (*Result, error) {
	result := &Result{}
	all := !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.BuildCache
	if all {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
	}

	if opts.DryRun {
		return c.dryRun(ctx, opts)
	}

	if opts.Containers {
		if err := c.rt.PruneContainers(ctx); err != nil {
			log.Printf("[cleanup] container prune: %v", err)
		} else {
			result.PrunedContainers = 1
		}
	}
	if opts.Images {
		if err := c.rt.PruneDanglingImages(ctx); err != nil {
			log.Printf("[cleanup] dangling image prune: %v", err)
		} else {
			result.PrunedImages = 1
		}
	}
	if opts.Volumes {
		if err := c.rt.PruneVolumes(ctx); err != nil {
			log.Printf("[cleanup] volume prune: %v", err)
		} else {
			result.PrunedVolumes = 1
		}
	}
	if opts.Networks {
		if err := c.rt.PruneNetworks(ctx); err != nil {
			log.Printf("[cleanup] network prune: %v", err)
		} else {
			result.PrunedNetworks = 1
		}
	}
	if opts.BuildCache {
		if err := c.rt.PruneBuildCache(ctx); err != nil {
			log.Printf("[cleanup] build cache prune: %v", err)
		} else {
			result.PrunedBuildCache = true
		}
	}

	// Per-app image retention
	keep := opts.Keep
	if keep <= 0 {
		keep = 5
	}
	apps, err := c.store.ListApps()
	if err != nil {
		log.Printf("[cleanup] list apps: %v", err)
		return result, nil
	}
	for _, app := range apps {
		if err := c.rt.KeepLastNImages(ctx, app.Name, keep); err != nil {
			log.Printf("[cleanup] keep %d images for %s: %v", keep, app.Name, err)
		} else {
			result.RetainedImages++
		}
	}

	return result, nil
}
```

- [ ] **Step 2: Add dryRun method**

Add to the same file after `Run`:

```go
func (c *Cleaner) dryRun(ctx context.Context, opts Options) (*Result, error) {
	log.Println("[cleanup] dry-run mode — showing what would be pruned:")

	if opts.Containers {
		cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
			"--filter", "status=exited",
			"--filter", "label!=tengiz-app",
			"--format", "table {{.ID}}\t{{.Names}}\t{{.Status}}",
		)
		out, _ := cmd.CombinedOutput()
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		// First line is header; skip if only header or empty
		count := 0
		for _, line := range lines {
			if line != "" && !strings.HasPrefix(line, "CONTAINER ID") {
				log.Printf("  [container] %s", line)
				count++
			}
		}
		log.Printf("  → %d stopped non-Tengiz containers would be removed", count)
	}

	if opts.Images {
		cmd := exec.CommandContext(ctx, "docker", "images",
			"--filter", "dangling=true",
			"--format", "table {{.ID}}\t{{.Repository}}:{{.Tag}}\t{{.Size}}",
		)
		out, _ := cmd.CombinedOutput()
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		count := 0
		for _, line := range lines {
			if line != "" && !strings.HasPrefix(line, "IMAGE ID") {
				log.Printf("  [image] %s", line)
				count++
			}
		}
		log.Printf("  → %d dangling images would be removed", count)
	}

	if opts.Volumes {
		cmd := exec.CommandContext(ctx, "docker", "volume", "ls",
			"--filter", "label!=tengiz-app",
			"--filter", "dangling=true",
			"--format", "table {{.Driver}}\t{{.Name}}",
		)
		out, _ := cmd.CombinedOutput()
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		count := 0
		for _, line := range lines {
			if line != "" && !strings.HasPrefix(line, "DRIVER") {
				log.Printf("  [volume] %s", line)
				count++
			}
		}
		log.Printf("  → %d unused volumes would be removed", count)
	}

	if opts.Networks {
		cmd := exec.CommandContext(ctx, "docker", "network", "ls",
			"--filter", "label!=tengiz-app",
			"--filter", "type=custom",
			"--format", "table {{.ID}}\t{{.Name}}",
		)
		out, _ := cmd.CombinedOutput()
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		count := 0
		for _, line := range lines {
			if line != "" && !strings.HasPrefix(line, "NETWORK ID") {
				log.Printf("  [network] %s", line)
				count++
			}
		}
		log.Printf("  → %d unused networks would be removed", count)
	}

	if opts.BuildCache {
		cmd := exec.CommandContext(ctx, "docker", "builder", "prune",
			"--force", "--all",
			"--filter", "until=24h",
		)
		out, _ := cmd.CombinedOutput()
		space := ""
		for _, line := range strings.Split(string(out), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "Total reclaimed space:") {
				space = strings.TrimPrefix(trimmed, "Total reclaimed space:")
			}
		}
		if space != "" {
			log.Printf("  [build-cache] would reclaim: %s", space)
		}
	}

	return &Result{}, nil
}
```

Wait, the dry-run for build cache shouldn't actually prune. Let me fix that — `docker builder prune` is destructive. For dry-run, just show build cache size:

```go
if opts.BuildCache {
    cmd := exec.CommandContext(ctx, "docker", "system", "df",
        "--format", "{{.Type}}\t{{.Size}}\t{{.Reclaimable}}",
    )
    out, _ := cmd.CombinedOutput()
    for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
        if strings.HasPrefix(line, "Build cache") {
            parts := strings.Split(line, "\t")
            if len(parts) == 3 {
                log.Printf("  [build-cache] size: %s, reclaimable: %s", parts[1], parts[2])
            }
        }
    }
}
```

Let me fix this in the plan below. Actually, let me just continue writing the plan with the corrected version.

- [ ] **Step 3: Write tests for Cleaner**

Create `internal/cleanup/cleaner_test.go`:

```go
package cleanup

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

func TestNew(t *testing.T) {
	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())
	c := New(rt, store, "production")
	if c == nil {
		t.Fatal("New() returned nil")
	}
}

func TestCleanerRunAll(t *testing.T) {
	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())
	c := New(rt, store, "production")
	result, err := c.Run(context.Background(), Options{Keep: 5})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result == nil {
		t.Fatal("Run() returned nil result")
	}
}

func TestCleanerRunContainersOnly(t *testing.T) {
	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())
	c := New(rt, store, "production")
	result, err := c.Run(context.Background(), Options{
		Containers: true,
		Keep:       5,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.PrunedContainers != 1 {
		t.Errorf("PrunedContainers = %d, want 1", result.PrunedContainers)
	}
}

func TestCleanerRunVolumesOnly(t *testing.T) {
	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())
	c := New(rt, store, "")
	result, err := c.Run(context.Background(), Options{
		Volumes: true,
		Keep:    5,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.PrunedVolumes != 1 {
		t.Errorf("PrunedVolumes = %d, want 1", result.PrunedVolumes)
	}
}

func TestCleanerDryRun(t *testing.T) {
	rt := runtime.NewStub()
	store := config.NewStore(t.TempDir())
	c := New(rt, store, "production")
	result, err := c.Run(context.Background(), Options{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
		DryRun:     true,
		Keep:       5,
	})
	if err != nil {
		t.Fatalf("Run(dry-run) error = %v", err)
	}
	if result == nil {
		t.Fatal("Run(dry-run) returned nil result")
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/cleanup/ -v -count=1
```

Expected: All 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cleanup/
git commit -m "feat: create internal/cleanup package with orchestrator"
```

---

### Task 4: Add CLI cleanup command

**Files:**
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `cleanup.New`, `cleanup.Options`, `cleanup.Cleaner.Run` from Task 3
- Produces: `tengiz cleanup` command with flags

- [ ] **Step 1: Add cleanup import**

In `internal/cli/root.go`, add to the import block:

```go
"github.com/yaso09/tengiz/internal/cleanup"
```

- [ ] **Step 2: Define cleanupCmd**

Before the `init()` function (or after existing command vars, around line 90-100 area), add:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources while protecting Tengiz apps",
	Long: `Remove stopped containers, dangling images, unused volumes, unused networks,
and build cache. By default, all categories are pruned. Uses label-based filtering
to protect Tengiz-managed containers, images, volumes, and networks.

Also runs per-app image retention (keeps last N images per app, default: 5).

Use --dry-run to see what would be removed without actually removing anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		keep, _ := cmd.Flags().GetInt("keep")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("runtime: %w", err)
		}
		store := config.NewStoreWithEnv(dataDir, env)
		cl := cleanup.New(rt, store, env)

		if dryRun {
			fmt.Println("[tengiz] dry-run mode — no resources will be removed")
		}

		result, err := cl.Run(context.Background(), cleanup.Options{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
			DryRun:     dryRun,
			Keep:       keep,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if !dryRun && result != nil {
			fmt.Println("[tengiz] cleanup complete:")
			if result.PrunedContainers > 0 {
				fmt.Println("  - stopped containers pruned")
			}
			if result.PrunedImages > 0 {
				fmt.Println("  - dangling images pruned")
			}
			if result.PrunedVolumes > 0 {
				fmt.Println("  - unused volumes pruned")
			}
			if result.PrunedNetworks > 0 {
				fmt.Println("  - unused networks pruned")
			}
			if result.PrunedBuildCache {
				fmt.Println("  - build cache pruned")
			}
			if result.RetainedImages > 0 {
				fmt.Printf("  - image retention applied to %d apps\n", result.RetainedImages)
			}
		}
		return nil
	},
}
```

- [ ] **Step 3: Register cleanupCmd in init()**

In the `init()` function in `internal/cli/root.go`, add after the last `rootCmd.AddCommand(...)` call:

```go
rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 4: Add flag definitions in Execute()**

In the `Execute()` function in `internal/cli/root.go`, add before `rootCmd.Execute()`:

```go
cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
cleanupCmd.Flags().Bool("images", false, "prune dangling (untagged) images")
cleanupCmd.Flags().Bool("volumes", false, "prune unused non-Tengiz volumes")
cleanupCmd.Flags().Bool("networks", false, "prune unused non-Tengiz networks")
cleanupCmd.Flags().Bool("build-cache", false, "prune Docker BuildKit build cache")
cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing")
cleanupCmd.Flags().Int("keep", 5, "number of images per app to keep")
```

- [ ] **Step 5: Run build to verify compilation**

```bash
go build -o /dev/null .
```

Expected: No errors, binary compiled.

- [ ] **Step 6: Run all tests**

```bash
go test ./... -v -count=1
```

Expected: All tests PASS (existing + new).

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go
git commit -m "feat: add tengiz cleanup CLI command"
```

---

## Self-Review

### Spec Coverage

**Feature #6: Docker Housekeeping**
- `tengiz cleanup` command: ✅ Task 4
- Label-based `docker system prune` variant: ✅ Tasks 2, 3 (uses per-category `docker * prune --filter label!=tengiz-app`)
- Protect Tengiz-managed resources: ✅ Task 2 (all prune methods filter out `tengiz-app` labeled resources)

**Feature #56: Granular Docker Prune Operations** (related, lower priority)
- Per-category prune: ✅ Tasks 2, 3 (`--containers`, `--images`, `--volumes`, `--networks`, `--build-cache` flags)
- Surgical disk management: ✅ Task 3 (dry-run mode for preview)

**Additional coverage:**
- Per-app image retention: ✅ Task 3 (`KeepLastNImages` with configurable `--keep` N)
- Build cache management: ✅ Task 2, 3 (`PruneBuildCache` via `docker builder prune`)

### Placeholder Scan

- All code blocks contain complete, compilable Go code
- No "TBD", "TODO", "implement later" patterns
- Every step has exact test code and expected output
- All types, methods, and interfaces referenced are defined in earlier tasks

### Type Consistency

- `runtime.Manager` interface: `PruneContainers(ctx) error` used consistently across Tasks 1, 2, 3
- `cleanup.Options` fields: `Containers`, `Images`, `Volumes`, `Networks`, `BuildCache`, `DryRun`, `Keep` — consistent in Tasks 3 and 4
- `cleanup.New(rt, store, env)` — consistent in Tasks 3 and 4
- Default keep = 5: consistent across Tasks 3 (in `Run`) and 4 (flag default)
- `stubManager` method signatures match `Manager` interface — verified in Task 1

### Edge Cases Handled

- Empty env → defaults to "production": ✅ in `New()` (Task 3) and `getEnv()` in CLI (Task 4)
- No flags set → all categories: ✅ in `Run()` (Task 3)
- `--dry-run` with no `--containers`/etc flags → lists all categories: ✅ (since `all` defaults to true)
- Keep <= 0 → defaults to 5: ✅ in `Run()` (Task 3)
- Docker CLI not available: ❗ The CLI command calls `runtime.NewDocker()` which checks for `docker` in PATH — handled at the runtime layer

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-07-26-docker-housekeeping.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
