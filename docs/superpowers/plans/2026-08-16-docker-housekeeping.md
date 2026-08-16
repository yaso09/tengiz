# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command and a `runtime.Manager.Cleanup` method so users can reclaim disk space by pruning stopped Tengiz-managed containers, dangling images, build cache, and (opt-in) unused volumes and networks — without ever touching running containers or tagged images.

**Architecture:** A new `Cleanup(ctx, opts CleanupOptions) (CleanupResult, error)` method is added to the existing `runtime.Manager` interface. The Docker implementation shells out to granular `docker <category> prune` commands (`container prune` filtered by the `tengiz-app` label, `image prune` for dangling images only, `builder prune`, and opt-in `volume prune`/`network prune`), concatenates their output, and counts reported deletions with a pure `countPruneOutput` helper. The CLI exposes a `tengiz cleanup` cobra command whose flags map 1:1 onto `CleanupOptions`. Cleanup is intentionally env-agnostic — labels identify Tengiz containers across all environments.

**Tech Stack:** Cobra (CLI), `os/exec` Docker CLI calls (no Docker SDK), Go 1.26, existing `runtime.Manager` interface, existing `labelKey = "tengiz-app"` constant in `internal/runtime/docker.go`.

## Global Constraints

- All prune operations shell out to `docker <category> prune` via `os/exec` — no Docker SDK, consistent with the rest of `internal/runtime/docker.go`
- Running containers are NEVER removed: `docker container prune` only targets stopped containers; `--restart no` + label filters protect the rest
- Tagged images are NEVER removed: `docker image prune` without `-a` only targets dangling (`<none>:<none>`) images
- Tengiz-managed containers are identified solely by the existing `labelKey = "tengiz-app"` label (defined in `internal/runtime/docker.go:76`); preview containers already carry this label via `runtime.Create`
- `--volumes` and `--networks` default to `false` because they can affect Docker resources Tengiz does not manage; everything else defaults to `true`
- `Cleanup` is env-agnostic — no `--env` flag, it operates on the whole Docker daemon via labels
- Per-app versioned-image retention stays with the existing `KeepLastNImages` (keep 5) called during deploy; cleanup only removes dangling images
- No new external dependencies
- Every existing test must continue to pass without modification (only the CLI mock `mockRTForDeploy` and stub `stubManager` gain a new method)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions` and `CleanupResult` types; add `Cleanup` to the `Manager` interface; add no-op stub implementation |
| `internal/runtime/cleanup.go` | `dockerRuntime.Cleanup` implementation + pure helpers `pruneCategories`, `cleanupCommandArgs`, `countPruneOutput` |
| `internal/runtime/cleanup_test.go` | Unit tests for stub `Cleanup`, `pruneCategories`, `cleanupCommandArgs`, `countPruneOutput` |
| `internal/cli/root.go` | `cleanupCmd` cobra command, its 5 bool flags, and registration in `init()` |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy`; tests for cleanup command registration, flags, defaults, and option parsing |
| `README.md` | Document the `tengiz cleanup` command in the Commands section |
| `AGENTS.md` | Add the `tengiz cleanup` line to the CLI section |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) as implemented |

---

### Task 1: Add `Cleanup` to `runtime.Manager` interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — `Manager` interface; `internal/runtime/runtime.go:113-122` — stub
- Modify: `internal/cli/root_test.go:69-100` — `mockRTForDeploy` (adds the new method so the package still compiles)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type CleanupOptions struct { Containers, Images, BuildCache, Volumes, Networks bool }`
  - `type CleanupResult struct { ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved int; BuildCacheCleared bool; Output string }`
  - `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`
  - `stubManager.Cleanup` returns `(CleanupResult{}, nil)`
  - `mockRTForDeploy.Cleanup` returns `(runtime.CleanupResult{}, nil)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go (append to the existing file)
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		BuildCache: true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersRemoved != 0 || res.ImagesRemoved != 0 || res.VolumesRemoved != 0 || res.NetworksRemoved != 0 {
		t.Errorf("Cleanup() result = %+v, want all-zero counts", res)
	}
	if res.BuildCacheCleared {
		t.Error("BuildCacheCleared = true, want false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: compile error `m.Cleanup undefined (type Manager has no field or method Cleanup)` (the `Manager` interface has no `Cleanup` yet).

- [ ] **Step 3: Add `CleanupOptions` and `CleanupResult` types and the interface method**

In `internal/runtime/runtime.go`, directly above the `Manager` interface (after the `RunOptions` struct at line 29):

```go
// CleanupOptions controls which Docker resource categories are pruned.
//
// Containers, Images, and BuildCache are safe-by-default (running
// containers and tagged images are never touched). Volumes and Networks
// are opt-in because they can affect resources not managed by Tengiz.
type CleanupOptions struct {
	Containers bool // prune stopped Tengiz-managed containers (label tengiz-app)
	Images     bool // prune dangling images (untagged only)
	BuildCache bool // prune unused build cache
	Volumes    bool // prune unused volumes (opt-in)
	Networks   bool // prune unused networks (opt-in)
}

// CleanupResult reports what a Cleanup call removed. Counts are
// best-effort and derived from the docker prune output; the full docker
// output is available in Output.
type CleanupResult struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
	BuildCacheCleared bool
	Output            string
}
```

Add the method to the `Manager` interface (line 32 area, after `Run`):

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
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
}
```

- [ ] **Step 4: Add the stub implementation**

In `internal/runtime/runtime.go`, after the `stubManager`'s `Run` method (line 122):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 5: Add the method to the CLI test mock**

In `internal/cli/root_test.go`, after `mockRTForDeploy.Run` (line 100):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`
Run: `go build ./...`

Expected: all runtime tests PASS (including `TestStubSatisfiesInterface`), and the whole repo builds (the mock now satisfies `Manager`).

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup method to Manager interface and stub"
```

---

### Task 2: Implement `dockerRuntime.Cleanup` + pure helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` (add to the existing file — it already has `RemoveImage` and `KeepLastNImages`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, `Manager.Cleanup` from Task 1; existing `labelKey` const and the `fmt`, `os/exec`, `sort`, `strings` imports already present in `internal/runtime/cleanup.go`
- Produces:
  - `func pruneCategories(opts CleanupOptions) []string` — ordered list of enabled category names (`"containers"`, `"images"`, `"build-cache"`, `"volumes"`, `"networks"`)
  - `func cleanupCommandArgs(category string) []string` — the docker argv for a category's prune command
  - `func countPruneOutput(out string) int` — number of reported deleted items in docker prune output
  - `dockerRuntime.Cleanup(ctx, opts)` behavior: runs each enabled category's prune, collects output, fills `CleanupResult`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go (append to the existing file)
func TestPruneCategories(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want []string
	}{
		{"nothing enabled", CleanupOptions{}, nil},
		{"containers only", CleanupOptions{Containers: true}, []string{"containers"}},
		{"safe defaults", CleanupOptions{Containers: true, Images: true, BuildCache: true},
			[]string{"containers", "images", "build-cache"}},
		{"all enabled", CleanupOptions{Containers: true, Images: true, BuildCache: true, Volumes: true, Networks: true},
			[]string{"containers", "images", "build-cache", "volumes", "networks"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneCategories(tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("pruneCategories(%+v) = %v (len %d), want %v (len %d)", tt.opts, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("pruneCategories(%+v)[%d] = %q, want %q", tt.opts, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCleanupCommandArgs(t *testing.T) {
	tests := []struct {
		category string
		want     []string
	}{
		{"containers", []string{"container", "prune", "-f", "--filter", "label=tengiz-app"}},
		{"images", []string{"image", "prune", "-f"}},
		{"build-cache", []string{"builder", "prune", "-f"}},
		{"volumes", []string{"volume", "prune", "-f"}},
		{"networks", []string{"network", "prune", "-f"}},
	}
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			got := cleanupCommandArgs(tt.category)
			if len(got) != len(tt.want) {
				t.Fatalf("cleanupCommandArgs(%q) = %v, want %v", tt.category, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("cleanupCommandArgs(%q)[%d] = %q, want %q", tt.category, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCountPruneOutput(t *testing.T) {
	const containerOutput = `Deleted Containers:
f3b9c2e1a4d5
a1b2c3d4e5f6

Total reclaimed space: 1.234MB`
	if got := countPruneOutput(containerOutput); got != 2 {
		t.Errorf("countPruneOutput() = %d, want 2", got)
	}

	const emptyOutput = `Total reclaimed space: 0B`
	if got := countPruneOutput(emptyOutput); got != 0 {
		t.Errorf("countPruneOutput() = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestPruneCategories|TestCleanupCommandArgs|TestCountPruneOutput" -v -count=1`

Expected: FAIL with `undefined: pruneCategories`, `undefined: cleanupCommandArgs`, `undefined: countPruneOutput`.

- [ ] **Step 3: Write the minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult
	for _, category := range pruneCategories(opts) {
		args := cleanupCommandArgs(category)
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return res, fmt.Errorf("docker %s prune: %w\n%s", category, err, string(out))
		}
		res.Output += string(out)
		removed := countPruneOutput(string(out))
		switch category {
		case "containers":
			res.ContainersRemoved = removed
		case "images":
			res.ImagesRemoved = removed
		case "build-cache":
			res.BuildCacheCleared = removed > 0
		case "volumes":
			res.VolumesRemoved = removed
		case "networks":
			res.NetworksRemoved = removed
		}
	}
	return res, nil
}

// pruneCategories returns the ordered list of enabled cleanup categories.
func pruneCategories(opts CleanupOptions) []string {
	var cats []string
	if opts.Containers {
		cats = append(cats, "containers")
	}
	if opts.Images {
		cats = append(cats, "images")
	}
	if opts.BuildCache {
		cats = append(cats, "build-cache")
	}
	if opts.Volumes {
		cats = append(cats, "volumes")
	}
	if opts.Networks {
		cats = append(cats, "networks")
	}
	return cats
}

// cleanupCommandArgs returns the docker argv for pruning a category.
//
// Containers are filtered by the tengiz-app label so only stopped
// Tengiz-managed containers are candidates. Images are pruned without
// -a, so only dangling (untagged) images are removed. Volumes and
// networks are removed only when unused by any container.
func cleanupCommandArgs(category string) []string {
	switch category {
	case "containers":
		return []string{"container", "prune", "-f", "--filter", fmt.Sprintf("label=%s", labelKey)}
	case "images":
		return []string{"image", "prune", "-f"}
	case "build-cache":
		return []string{"builder", "prune", "-f"}
	case "volumes":
		return []string{"volume", "prune", "-f"}
	case "networks":
		return []string{"network", "prune", "-f"}
	default:
		return nil
	}
}

// countPruneOutput counts the deleted items reported by a
// `docker <category> prune` invocation. The output looks like:
//
//	Deleted Containers:
//	f3b9c2e1a4d5
//	a1b2c3d4e5f6
//
//	Total reclaimed space: 1.234MB
//
// It skips empty lines, the section header (a line ending with ':'),
// and the "Total reclaimed space: ..." footer, then counts the rest.
func countPruneOutput(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "Total reclaimed space") {
			continue
		}
		if strings.HasSuffix(line, ":") {
			continue
		}
		count++
	}
	return count
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneCategories|TestCleanupCommandArgs|TestCountPruneOutput|TestStubCleanup" -v -count=1`

Expected: PASS.

- [ ] **Step 5: Run all runtime tests and vet**

Run: `go test ./internal/runtime/... -v -count=1`
Run: `go vet ./internal/runtime/...`

Expected: All PASS, vet clean.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement dockerRuntime.Cleanup with label-safe pruning"
```

---

### Task 3: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` after the `runCmd` block (ends right before `var gitCmd = &cobra.Command{` around line 1163) and register it in `init()` (after `rootCmd.AddCommand(runCmd)` around line 67)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()` → `runtime.Manager`, `runtime.CleanupOptions`, `runtime.CleanupResult` from Tasks 1-2
- Produces: `tengiz cleanup` cobra command registered on `rootCmd`, with bool flags `--containers` (default true), `--images` (default true), `--build-cache` (default true), `--volumes` (default false), `--networks` (default false)

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/root_test.go (append)
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "build-cache", "volumes", "networks"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdFlagDefaults(t *testing.T) {
	containers, _ := cleanupCmd.Flags().GetBool("containers")
	images, _ := cleanupCmd.Flags().GetBool("images")
	buildCache, _ := cleanupCmd.Flags().GetBool("build-cache")
	volumes, _ := cleanupCmd.Flags().GetBool("volumes")
	networks, _ := cleanupCmd.Flags().GetBool("networks")

	if !containers || !images || !buildCache {
		t.Error("--containers/--images/--build-cache should default to true (safe categories)")
	}
	if volumes || networks {
		t.Error("--volumes/--networks should default to false (opt-in categories)")
	}
}

func TestCleanupCmdParsesOptions(t *testing.T) {
	var got runtime.CleanupOptions
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		got = runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			BuildCache: buildCache,
			Volumes:    volumes,
			Networks:   networks,
		}
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--containers=false", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got.Containers {
		t.Error("Containers = true, want false (--containers=false)")
	}
	if !got.Images {
		t.Error("Images = false, want true (default)")
	}
	if !got.BuildCache {
		t.Error("BuildCache = false, want true (default)")
	}
	if !got.Volumes {
		t.Error("Volumes = false, want true (--volumes)")
	}
	if got.Networks {
		t.Error("Networks = true, want false (default)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCommand|TestCleanupCmd" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`.

- [ ] **Step 3: Define the command**

In `internal/cli/root.go`, directly after the `runCmd` block (which ends with `},` right before `var gitCmd = &cobra.Command{` around line 1163), add:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Removes unused Docker resources to reclaim disk space.

Stopped Tengiz-managed containers (labeled tengiz-app), dangling build
images, and unused build cache are removed by default. Running containers
and tagged images are never touched. Use --volumes and --networks to also
prune unused volumes and networks (they may affect resources not managed
by Tengiz).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			BuildCache: buildCache,
			Volumes:    volumes,
			Networks:   networks,
		}

		fmt.Println("[tengiz] cleaning up Docker resources...")
		res, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] cleanup complete:")
		fmt.Printf("  containers removed: %d\n", res.ContainersRemoved)
		fmt.Printf("  images removed:     %d\n", res.ImagesRemoved)
		fmt.Printf("  volumes removed:    %d\n", res.VolumesRemoved)
		fmt.Printf("  networks removed:   %d\n", res.NetworksRemoved)
		if buildCache {
			if res.BuildCacheCleared {
				fmt.Println("  build cache:        cleared")
			} else {
				fmt.Println("  build cache:        nothing to clear")
			}
		}
		return nil
	},
}
```

- [ ] **Step 4: Register the command and its flags**

In `init()` in `internal/cli/root.go`, after `rootCmd.AddCommand(runCmd)` (around line 67), add:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("containers", true, "remove stopped Tengiz-managed containers")
	cleanupCmd.Flags().Bool("images", true, "remove dangling build images")
	cleanupCmd.Flags().Bool("build-cache", true, "remove unused build cache")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "also remove unused networks")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCommand|TestCleanupCmd" -v -count=1`
Run: `go build ./...`

Expected: PASS, build succeeds.

- [ ] **Step 6: Run all cli tests and vet**

Run: `go test ./internal/cli/... -v -count=1`
Run: `go vet ./internal/cli/...`

Expected: All PASS, vet clean.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 4: Update documentation

**Files:**
- Modify: `README.md` — add a `### tengiz cleanup` section after the `### tengiz rollback <app>` section (around line 236) and a row in the Commands table (around line 570)
- Modify: `AGENTS.md` — add the `tengiz cleanup` line to the CLI section (after the `tengiz proxy` line around line 42)
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented

**Interfaces:**
- Consumes: the `tengiz cleanup` command and flags from Task 3
- Produces: user-facing documentation matching the actual CLI help text

- [ ] **Step 1: Add the command section to `README.md`**

After the `### tengiz rollback <app>` section (which ends before `### tengiz domain` around line 238), insert:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space.

| Flag | Description | Default |
|------|-------------|---------|
| `--containers` | Remove stopped Tengiz-managed containers (labeled `tengiz-app`) | `true` |
| `--images` | Remove dangling (untagged) build images | `true` |
| `--build-cache` | Remove unused build cache | `true` |
| `--volumes` | Also remove unused volumes (may affect resources not managed by Tengiz) | `false` |
| `--networks` | Also remove unused networks (may affect resources not managed by Tengiz) | `false` |

Running containers and tagged images are never removed. Versioned images for deployed apps are retained by the existing per-app policy (last 5 kept) during deploy. Useful as a periodic maintenance step on long-running single-server deployments.

Example:
```
tengiz cleanup
tengiz cleanup --volumes --networks
```
```

- [ ] **Step 2: Add a row to the README Commands table**

In the `### Commands` table in `README.md` (around line 570), after the `tengiz webhook` row, add:

```markdown
| `tengiz cleanup` | Prune unused Docker resources (containers, images, build cache) |
```

- [ ] **Step 3: Add the command to `AGENTS.md`**

In `AGENTS.md` CLI section, after the `tengiz proxy` line (around line 42), add:

```markdown
tengiz cleanup [--containers --images --build-cache] [--volumes --networks] → prune unused Docker resources
```

- [ ] **Step 4: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In `docs/FUTURES_FEATURES.md`, change the P0 table row for feature #6:

```diff
- | 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
+ | 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Also add a row to the `### ✅ Implemented Features (Not Pending)` table:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-16) |
```

- [ ] **Step 5: Run the full test suite, vet, and build**

Run: `go test ./... -v -count=1`
Run: `go vet ./...`
Run: `go build -o tengiz .`

Expected: All PASS (except known time-sensitive proxy/idle tests, which must pass or be confirmed flaky-only), vet clean, build succeeds.

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup command and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage.** The feature spec (docs/FUTURES_FEATURES.md #6) asks for: label-based `docker system prune` behavior and a `tengiz cleanup` command. Coverage: Task 1 defines the `runtime.Manager.Cleanup` contract; Task 2 implements the label-filtered `docker container prune` (containers labeled `tengiz-app` are the only containers touched, and only when stopped) plus `docker image prune`/`builder prune`/opt-in volume+network prune — this is the "label-based docker system prune" from the spec; Task 3 exposes the `tengiz cleanup` CLI command; Task 4 updates README/AGENTS and marks the feature implemented. The Coolify-source detail ("kullanılmayan volume, network, container ve image'leri temizleme, label-based filtreleme ile Tengiz yönetimindeki container'lar korunur") is fully covered: volumes/networks are supported (opt-in), and label filtering protects Tengiz-managed containers while pruning only stopped ones.

**2. Placeholder scan.** No "TBD", "TODO", "implement later", "similar to Task", or "add error handling" placeholders. Every code step contains complete, copy-pasteable code. The only text outside code blocks is prose describing where to insert content.

**3. Type consistency.**
- `CleanupOptions` fields (`Containers`, `Images`, `BuildCache`, `Volumes`, `Networks`) are defined once in Task 1 and referenced identically in Tasks 2 and 3.
- `CleanupResult` fields (`ContainersRemoved`, `ImagesRemoved`, `VolumesRemoved`, `NetworksRemoved`, `BuildCacheCleared`, `Output`) are defined in Task 1 and filled in Task 2's `dockerRuntime.Cleanup`, consumed in Task 3's `cleanupCmd`.
- `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — same signature in the interface (Task 1), stub (Task 1), mock (Task 1), and docker implementation (Task 2).
- Helper names `pruneCategories`, `cleanupCommandArgs`, `countPruneOutput` are identical in Task 2's tests and implementation.
- `labelKey` (the existing `"tengiz-app"` constant) is used directly in `cleanupCommandArgs` — no duplicated string literal in the plan's docker implementation.
- CLI flags map 1:1 to `CleanupOptions` field names (`--containers` ↔ `Containers`, `--build-cache` ↔ `BuildCache`), keeping Task 3's parsing code aligned with Task 1's type.
- Docker argv orders (`container`, `image`, `builder`, `volume`, `network`) are identical between the Task 2 tests and implementation.

No issues found; no fixes required.

---

## Execution Handoff

Plan complete. Two execution options:

**1. Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints.