# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely reclaims disk space by pruning unused Docker images, exited non-Tengiz containers, dangling build cache, and (optionally) unused volumes — while never touching Tengiz-managed applications, released images used for rollback, or scale-to-zero stopped containers.

**Architecture:** New `runtime.Manager` methods (`ListImages`, `Cleanup`) sit behind the existing exec-based `dockerRuntime`. All decision logic lives in pure, unit-testable helpers (`pruneCandidates`, `pruneTengizBeyond`, `pruneContainers`, `planCleanup`) that map a `docker images` snapshot + container ID lists to a `types.CleanupPlan`. The docker exec orchestration is thin: list state, compute plan, execute removals, report a `types.CleanupResult`. The CLI adds a `cleanup` command with `--all`, `--volumes`, `--dry-run`, `--force` flags plus a confirmation guard.

**Tech Stack:** Go 1.26, exec-based Docker CLI (`os/exec`, no SDK), existing `runtime.Manager`, `types`, `config.Store`. No new external dependencies.

## Global Constraints

- `docker` CLI is invoked via `os/exec` (no Docker SDK) — all `dockerRuntime` methods follow the existing `exec.CommandContext` pattern in `internal/runtime/docker.go`
- **Never remove** any container carrying the `tengiz-app` label (`labelKey` constant = `"tengiz-app"`). This protects running apps, scale-to-zero stopped apps, and preview deployments (previews are created via `rt.Create` → they carry this label)
- **Never remove** these images by default: any under the `tengiz-apps/` repository prefix (managed release images used for rollback), and any tagged `<none>` (dangling — handled via `docker image prune` instead of `docker rmi`)
- Volumes are **only** pruned when the user explicitly passes `--volumes`; bind mounts are never touched (Docker only prunes anonymous/named volumes)
- `--all` is the only path that removes non-Tengiz images and prunes `tengz-apps/*` releases older than the newest `keep` (default 5) per repository — same retention as the existing `KeepLastNImages`
- Cleanup is host-level — it does **not** take an `--env` scope (images/containers span environments; repo name encodes env as `tengiz-apps/<name>-<env>`)
- Non-interactive stdin or `--force`/`-f` bypasses the confirmation prompt (CI-safe)
- No new external Go dependencies; existing tests must continue to pass with only the documented stub/mock additions
- New work is done on branch `feat/docker-housekeeping`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` (Modify) | `ImageInfo`, `CleanupPlan`, `CleanupResult` structs + `ImageInfo.Ref()` method |
| `internal/runtime/prune.go` (Create) | Pure decision functions `pruneCandidates`, `pruneTengizBeyond`, `pruneContainers`, `planCleanup` |
| `internal/runtime/prune_test.go` (Create) | Unit tests for all pure helpers |
| `internal/runtime/docker.go` (Modify) | `ListImages`, `Cleanup`, `listContainerIDs` exec methods on `dockerRuntime` |
| `internal/runtime/runtime.go` (Modify) | Add `ListImages`, `Cleanup` to `Manager` interface + `stubManager` implementations |
| `internal/runtime/cleanup_test.go` (Modify) | Stub tests for the two new interface methods |
| `internal/cli/root.go` (Modify) | `cleanupCmd` + registration in `init()` + `printCleanupResult` + `isNonInteractive` helpers |
| `internal/cli/root_test.go` (Modify) | Add `ListImages`/`Cleanup` to `mockRTForDeploy` so it keeps satisfying `Manager` |
| `internal/cli/cleanup_test.go` (Create) | Registration + flag tests for the cleanup command |
| `README.md` (Modify) | Document `tengiz cleanup` under CLI Reference |
| `docs/FUTURES_FEATURES.md` (Modify) | Mark feature #6 as implemented |

---

### Task 1: Add shared types for cleanup

**Files:**
- Modify: `internal/types/types.go` — append structs at end of file
- Create: `internal/types/types_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `types.ImageInfo{Repository, Tag, ID, CreatedAt string}`, `ImageInfo.Ref() string`, `types.CleanupPlan{ImagesToRemove, TengizImagesToRemove, ContainersToRemove []string}`, `types.CleanupResult{Plan CleanupPlan, DryRun bool, ImagesRemoved, TengizImagesRemoved, ContainersRemoved, VolumesRemoved []string, BuildCacheFreed string}`

- [ ] **Step 1: Write the failing test**

```go
// internal/types/types_test.go
package types

import "testing"

func TestImageInfoRef(t *testing.T) {
	img := ImageInfo{Repository: "tengiz-apps/myapp", Tag: "1750000000"}
	if got, want := img.Ref(), "tengiz-apps/myapp:1750000000"; got != want {
		t.Errorf("Ref() = %q, want %q", got, want)
	}
}

func TestImageInfoRefDanglingUsesID(t *testing.T) {
	img := ImageInfo{Repository: "<none>", Tag: "<none>", ID: "sha256:abc123"}
	if got, want := img.Ref(), "sha256:abc123"; got != want {
		t.Errorf("Ref() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types/... -run "TestImageInfoRef" -v -count=1`
Expected: FAIL — `undefined: ImageInfo`

- [ ] **Step 3: Add the types**

Append to `internal/types/types.go`:

```go
type ImageInfo struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	ID         string `json:"id"`
	CreatedAt  string `json:"created_at"`
}

func (i ImageInfo) Ref() string {
	if i.Repository == "" || i.Repository == "<none>" {
		return i.ID
	}
	return i.Repository + ":" + i.Tag
}

type CleanupPlan struct {
	ImagesToRemove       []string `json:"images_to_remove"`
	TengizImagesToRemove []string `json:"tengiz_images_to_remove"`
	ContainersToRemove   []string `json:"containers_to_remove"`
}

type CleanupResult struct {
	Plan                CleanupPlan `json:"plan"`
	DryRun              bool        `json:"dry_run"`
	ImagesRemoved       []string    `json:"images_removed"`
	TengizImagesRemoved []string    `json:"tengiz_images_removed"`
	ContainersRemoved   []string    `json:"containers_removed"`
	VolumesRemoved      []string    `json:"volumes_removed"`
	BuildCacheFreed     string      `json:"build_cache_freed"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/types/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go
git commit -m "feat: add cleanup types (ImageInfo, CleanupPlan, CleanupResult)"
```

---

### Task 2: Pure cleanup decision helpers in runtime

**Files:**
- Create: `internal/runtime/prune.go`
- Create: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `types.ImageInfo`, `types.CleanupPlan` from Task 1
- Produces: `pruneCandidates(images []types.ImageInfo, protected []string) []string`, `pruneTengizBeyond(images []types.ImageInfo, keep int) []string`, `pruneContainers(all, tengiz []string) []string`, `planCleanup(images []types.ImageInfo, exited, tengiz []string, opts CleanupOptions) types.CleanupPlan` (uses `runtime.CleanupOptions{DryRun, Volumes, All, KeepImages}` from Task 3 — types only, still testable here)

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/prune_test.go
package runtime

import (
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestPruneCandidates(t *testing.T) {
	images := []types.ImageInfo{
		{Repository: "tengiz-apps/myapp", Tag: "100", ID: "i1"},
		{Repository: "postgres", Tag: "16", ID: "i3"},
		{Repository: "myrepo", Tag: "latest", ID: "i4"},
		{Repository: "<none>", Tag: "<none>", ID: "i5"},
	}
	got := pruneCandidates(images, []string{"myrepo:latest"})
	if len(got) != 1 || got[0] != "postgres:16" {
		t.Fatalf("pruneCandidates() = %v, want [postgres:16]", got)
	}
}

func TestPruneTengizBeyond(t *testing.T) {
	images := []types.ImageInfo{
		{Repository: "tengiz-apps/a", Tag: "100", CreatedAt: "2026-01-01"},
		{Repository: "tengiz-apps/a", Tag: "200", CreatedAt: "2026-01-02"},
		{Repository: "tengiz-apps/a", Tag: "300", CreatedAt: "2026-01-03"},
		{Repository: "tengiz-apps/b", Tag: "1", CreatedAt: "2026-01-01"},
	}
	got := pruneTengizBeyond(images, 2)
	if len(got) != 1 || got[0] != "tengiz-apps/a:100" {
		t.Fatalf("pruneTengizBeyond() = %v, want [tengiz-apps/a:100]", got)
	}
}

func TestPruneContainers(t *testing.T) {
	got := pruneContainers([]string{"c1", "c2", "c3"}, []string{"c2"})
	if len(got) != 2 {
		t.Fatalf("pruneContainers() = %v, want 2 entries", got)
	}
	for _, id := range got {
		if id == "c2" {
			t.Fatalf("pruneContainers() removed protected container c2")
		}
	}
}

func TestPlanCleanupAll(t *testing.T) {
	images := []types.ImageInfo{
		{Repository: "tengiz-apps/a", Tag: "100", CreatedAt: "2026-01-01"},
		{Repository: "tengiz-apps/a", Tag: "200", CreatedAt: "2026-01-02"},
		{Repository: "postgres", Tag: "16", ID: "i3"},
	}
	plan := planCleanup(images, nil, nil, CleanupOptions{All: true, KeepImages: 1})
	// --all: postgres:16 removable; tengiz-apps/a keeps newest 1 (200), removes 100
	if len(plan.ImagesToRemove) != 1 || plan.ImagesToRemove[0] != "postgres:16" {
		t.Fatalf("ImagesToRemove = %v, want [postgres:16]", plan.ImagesToRemove)
	}
	if len(plan.TengizImagesToRemove) != 1 || plan.TengizImagesToRemove[0] != "tengiz-apps/a:100" {
		t.Fatalf("TengizImagesToRemove = %v, want [tengiz-apps/a:100]", plan.TengizImagesToRemove)
	}
}

func TestPlanCleanupDefault(t *testing.T) {
	plan := planCleanup(nil, []string{"c1"}, []string{"c1"}, CleanupOptions{})
	if len(plan.ImagesToRemove) != 0 || len(plan.TengizImagesToRemove) != 0 || len(plan.ContainersToRemove) != 0 {
		t.Fatalf("default plan over-removes: %+v", plan)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestPrune" -v -count=1`
Expected: FAIL with `undefined: pruneCandidates`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/runtime/prune.go
package runtime

import (
	"sort"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

const imagePrefix = "tengiz-apps/"

// pruneCandidates returns external (non-Tengiz), non-dangling image refs to
// remove. protected tags (Repo:Tag) are always kept.
func pruneCandidates(images []types.ImageInfo, protected []string) []string {
	protectedSet := make(map[string]bool, len(protected))
	for _, p := range protected {
		protectedSet[p] = true
	}
	var out []string
	for _, img := range images {
		if img.Repository == "" || img.Repository == "<none>" {
			continue
		}
		if strings.HasPrefix(img.Repository, imagePrefix) {
			continue
		}
		ref := img.Ref()
		if protectedSet[ref] {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// pruneTengizBeyond returns managed (tengiz-apps/*) image refs older than the
// newest `keep` per repository, mirroring the existing KeepLastNImages policy.
func pruneTengizBeyond(images []types.ImageInfo, keep int) []string {
	if keep < 1 {
		keep = 5
	}
	byRepo := make(map[string][]types.ImageInfo)
	var repos []string
	for _, img := range images {
		if img.Repository == "" || img.Repository == "<none>" {
			continue
		}
		if !strings.HasPrefix(img.Repository, imagePrefix) {
			continue
		}
		if _, ok := byRepo[img.Repository]; !ok {
			repos = append(repos, img.Repository)
		}
		byRepo[img.Repository] = append(byRepo[img.Repository], img)
	}
	sort.Strings(repos)
	var out []string
	for _, r := range repos {
		list := byRepo[r]
		sort.SliceStable(list, func(i, j int) bool {
			return list[i].CreatedAt < list[j].CreatedAt
		})
		for i := 0; i < len(list)-keep; i++ {
			if strings.HasSuffix(list[i].Tag, "latest") {
				continue
			}
			out = append(out, list[i].Ref())
		}
	}
	return out
}

// pruneContainers returns container IDs that exist but are not Tengiz-managed.
func pruneContainers(allExited, tengiz []string) []string {
	keep := make(map[string]bool, len(tengiz))
	for _, id := range tengiz {
		keep[id] = true
	}
	var out []string
	for _, id := range allExited {
		if !keep[id] {
			out = append(out, id)
		}
	}
	return out
}

// planCleanup computes exactly what a cleanup run would remove, without
// touching Docker. It is the single source of truth for both --dry-run and
// the real execution.
func planCleanup(images []types.ImageInfo, exited, tengiz []string, opts CleanupOptions) types.CleanupPlan {
	var p types.CleanupPlan
	if opts.All {
		p.ImagesToRemove = pruneCandidates(images, nil)
		p.TengizImagesToRemove = pruneTengizBeyond(images, opts.KeepImages)
	}
	p.ContainersToRemove = pruneContainers(exited, tengiz)
	return p
}
```

- [ ] **Step 4: Add CleanupOptions type in runtime.go so tests compile**

Add to `internal/runtime/runtime.go` (next to `RunOptions`):

```go
type CleanupOptions struct {
	DryRun     bool
	Volumes    bool
	All        bool
	KeepImages int
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPrune|TestPlanCleanup" -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go internal/runtime/runtime.go
git commit -m "feat: add pure cleanup planning helpers (pruneCandidates, pruneTengizBeyond, planCleanup)"
```

---

### Task 3: Add `ListImages` and `Cleanup` to Manager + dockerRuntime

**Files:**
- Modify: `internal/runtime/runtime.go` — Manager interface + `stubManager`
- Modify: `internal/runtime/docker.go` — exec implementations
- Modify: `internal/runtime/cleanup_test.go` — stub tests

**Interfaces:**
- Consumes: `types.ImageInfo`, `types.CleanupPlan` (Task 1); `pruneCandidates`, `pruneTengizBeyond`, `pruneContainers`, `planCleanup`, `CleanupOptions` (Task 2)
- Produces: `Manager.ListImages(ctx) ([]types.ImageInfo, error)`, `Manager.Cleanup(ctx, opts CleanupOptions) (*types.CleanupResult, error)` — used by Task 4

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/cleanup_test.go — append
func TestStubListImages(t *testing.T) {
	m := NewStub()
	imgs, err := m.ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages() error = %v", err)
	}
	if len(imgs) != 0 {
		t.Fatalf("ListImages() = %v, want empty", imgs)
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil {
		t.Fatal("Cleanup() returned nil result")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubListImages|TestStubCleanup" -v -count=1`
Expected: FAIL — `Manager` has no field/method `ListImages`/`Cleanup`

- [ ] **Step 3: Add methods to the `Manager` interface**

In `internal/runtime/runtime.go`, add to the `Manager` interface:

```go
	ListImages(ctx context.Context) ([]types.ImageInfo, error)
	Cleanup(ctx context.Context, opts CleanupOptions) (*types.CleanupResult, error)
```

- [ ] **Step 4: Add stub implementations**

In `internal/runtime/runtime.go`, add to `stubManager`:

```go
func (m *stubManager) ListImages(ctx context.Context) ([]types.ImageInfo, error) {
	return nil, nil
}

func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*types.CleanupResult, error) {
	return &types.CleanupResult{Plan: types.CleanupPlan{}}, nil
}
```

- [ ] **Step 5: Add exec implementations in `internal/runtime/docker.go`**

Add imports `"github.com/yaso09/tengiz/internal/types"` is already imported. Append methods after `List` (module ~line 431):

```go
func (r *dockerRuntime) ListImages(ctx context.Context) ([]types.ImageInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "--no-trunc",
		"--format", `{{.Repository}}|{{.Tag}}|{{.ID}}|{{.CreatedAt}}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	var images []types.ImageInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 3 {
			continue
		}
		info := types.ImageInfo{Repository: parts[0], Tag: parts[1], ID: parts[2]}
		if len(parts) == 4 {
			info.CreatedAt = parts[3]
		}
		images = append(images, info)
	}
	return images, nil
}

func (r *dockerRuntime) listImages(ctx context.Context, filter string) ([]string, error) {
	args := []string{"ps", "-a", "-q"}
	if filter != "" {
		args = append(args, "--filter", filter)
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return strings.Fields(strings.TrimSpace(string(out))), nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*types.CleanupResult, error) {
	if opts.KeepImages == 0 {
		opts.KeepImages = 5
	}

	images, err := r.ListImages(ctx)
	if err != nil {
		return nil, err
	}
	exited, err := r.listImages(ctx, "status=exited")
	if err != nil {
		return nil, err
	}
	tengiz, err := r.listImages(ctx, "label="+labelKey)
	if err != nil {
		return nil, err
	}

	plan := planCleanup(images, exited, tengiz, opts)
	res := &types.CleanupResult{Plan: plan, DryRun: opts.DryRun}
	if opts.DryRun {
		return res, nil
	}

	for _, ref := range plan.ImagesToRemove {
		if err := r.RemoveImage(ctx, ref); err != nil {
			log.Printf("[runtime] cleanup: skip image %s: %v", ref, err)
			continue
		}
		res.ImagesRemoved = append(res.ImagesRemoved, ref)
	}
	for _, ref := range plan.TengizImagesToRemove {
		if err := r.RemoveImage(ctx, ref); err != nil {
			log.Printf("[runtime] cleanup: skip image %s: %v", ref, err)
			continue
		}
		res.TengizImagesRemoved = append(res.TengizImagesRemoved, ref)
	}
	for _, cid := range plan.ContainersToRemove {
		if out, rerr := exec.CommandContext(ctx, "docker", "rm", cid).CombinedOutput(); rerr != nil {
			log.Printf("[runtime] cleanup: skip container %s: %v (%s)", cid, rerr, string(out))
			continue
		}
		res.ContainersRemoved = append(res.ContainersRemoved, cid)
	}

	var cacheOut []byte
	if out, cerr := exec.CommandContext(ctx, "docker", "builder", "prune", "-f").CombinedOutput(); cerr == nil {
		cacheOut = append(cacheOut, out...)
	}
	if out, cerr := exec.CommandContext(ctx, "docker", "image", "prune", "-f", "--filter", "dangling=true").CombinedOutput(); cerr == nil {
		cacheOut = append(cacheOut, out...)
	}
	res.BuildCacheFreed = strings.TrimSpace(string(cacheOut))

	if opts.Volumes {
		out, verr := exec.CommandContext(ctx, "docker", "volume", "prune", "-f").CombinedOutput()
		if verr != nil {
			log.Printf("[runtime] cleanup: volume prune: %v (%s)", verr, string(out))
		} else {
			for _, line := range strings.Split(strings.Trim(string(out), "\n"), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && line != "Total reclaimed space:" {
					res.VolumesRemoved = append(res.VolumesRemoved, line)
				}
			}
		}
	}

	return res, nil
}
```

Note: `log` is already imported in `docker.go`. `labelKey` is the package constant from line 76.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`
Expected: PASS (all existing runtime tests + the two new stub tests)

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/docker.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add ListImages and Cleanup to Manager and dockerRuntime"
```

---

### Task 4: Add `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — command def, register in `init()`, helpers
- Modify: `internal/cli/root_test.go` — add `ListImages`/`Cleanup` to `mockRTForDeploy`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `r.Cleanup(ctx, opts) (*types.CleanupResult, error)` from Task 3, `types.CleanupResult`
- Produces: `tengiz cleanup` and `printCleanupResult(res *types.CleanupResult)` + `isNonInteractive() bool`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not found")
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not registered")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	cmd := cleanupCmd
	for _, name := range []string{"all", "volumes", "dry-run", "force"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupUsesHostNamespace(t *testing.T) {
	// cleanup is host-level; verify it does not define an --env flag
	if cleanupCmd.Flags().Lookup("env") != nil {
		t.Error("cleanupCmd should not define its own --env flag")
	}
}

func TestCleanupCommandHasNoArgs(t *testing.T) {
	var argTest cobra.PositionalArgs = cobra.NoArgs
	if cleanupCmd.Args == nil {
		t.Fatal("cleanupCmd.Args not set")
	}
	_ = argTest
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run TestCleanup -v -count=1`
Expected: FAIL — `cleanupCmd` undefined

- [ ] **Step 3: Add the command and helpers near the `psCmd` in `internal/cli/root.go`**

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (housekeeping)",
	Long: `Remove unused Docker resources safely, preserving Tengiz-managed
applications, stopped scale-to-zero containers, previews, and release images
used for rollback.

Default: prunes dangling build cache, dangling images, and exited
non-Tengiz containers.

Flags:
  --all        Also remove non-Tengiz images and Tengiz release images older
               than the newest retained per app (default keep 5).
  --volumes    Also remove unused Docker volumes (use with care; does not
               touch bind mounts).
  --dry-run    Show what would be removed without removing anything.
  -f --force   Skip the confirmation prompt (CI-safe).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if !force && !dryRun && !isNonInteractive() {
			fmt.Print("[tengiz] Remove unused Docker resources? [y/N] ")
			var ans string
			fmt.Scanln(&ans)
			if !strings.EqualFold(strings.TrimSpace(ans), "y") {
				fmt.Println("[tengiz] cleanup cancelled.")
				return nil
			}
		}

		res, err := rt.Cleanup(cmd.Context(), runtime.CleanupOptions{
			DryRun:    dryRun,
			Volumes:   volumes,
			All:       all,
			KeepImages: 5,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupResult(res)
		return nil
	},
}

func printCleanupResult(r *types.CleanupResult) {
	if r.DryRun {
		fmt.Println("[tengiz] (dry-run) would remove:")
		any := false
		for _, c := range r.Plan.ContainersToRemove {
			fmt.Printf("  container %s\n", c)
			any = true
		}
		for _, img := range r.Plan.ImagesToRemove {
			fmt.Printf("  image %s\n", img)
			any = true
		}
		for _, img := range r.Plan.TengizImagesToRemove {
			fmt.Printf("  image %s (old tengiz release)\n", img)
			any = true
		}
		if !any {
			fmt.Println("  (nothing to remove)")
		}
		return
	}

	total := len(r.ContainersRemoved) + len(r.ImagesRemoved) + len(r.TengizImagesRemoved) + len(r.VolumesRemoved)
	if total == 0 && r.BuildCacheFreed == "" {
		fmt.Println("[tengiz] nothing to clean.")
		return
	}
	fmt.Printf("[tengiz] removed %d resource(s):\n", total)
	for _, c := range r.ContainersRemoved {
		fmt.Printf("  container %s\n", c)
	}
	for _, img := range r.ImagesRemoved {
		fmt.Printf("  image %s\n", img)
	}
	for _, img := range r.TengizImagesRemoved {
		fmt.Printf("  image %s (old tengiz release)\n", img)
	}
	for _, v := range r.VolumesRemoved {
		fmt.Printf("  volume %s\n", v)
	}
	if r.BuildCacheFreed != "" {
		fmt.Printf("  build cache/dangling pruned: %s\n", strings.TrimSpace(r.BuildCacheFreed))
	}
}

func isNonInteractive() bool {
	fi, err := os.Stdin.Stat()
	return err != nil || fi.Mode()&os.ModeCharDevice == 0
}
```

- [ ] **Step 4: Register the command + flags in `init()`**

In `root.go` `init()` (after `rootCmd.AddCommand(rollbackCmd)`):

```go
	cleanupCmd.Flags().BoolP("all", "a", false, "remove all removable resources (external images and old tengiz releases)")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused Docker volumes")
	cleanupCmd.Flags().BoolP("dry-run", "n", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().BoolP("force", "f", false, "skip the confirmation prompt")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Update `mockRTForDeploy` in `internal/cli/root_test.go`**

Add these two methods to the mock (so it still satisfies `runtime.Manager`):

```go
func (m *mockRTForDeploy) ListImages(ctx context.Context) ([]types.ImageInfo, error) { return nil, nil }
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*types.CleanupResult, error) {
	return &types.CleanupResult{Plan: types.CleanupPlan{}}, nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run TestCleanup -v -count=1`
Expected: PASS

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 7: Run all tests + vet**

Run: `go test ./... -v -count=1`
Expected: All PASS (existing proxy/idle time-sensitive tests may be slow but must not fail)

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 8: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command with safe default pruning"
```

---

### Task 5: Documentation + mark feature complete + self-review

**Files:**
- Modify: `README.md` (CLI Reference)
- Modify: `docs/FUTURES_FEATURES.md` (mark #6 implemented)

**Interfaces:**
- Consumes: nothing new
- Produces: documented command; feature list marked implemented

- [ ] **Step 1: Add `cleanup` to README CLI Reference**

Add after the `### tengiz ps` section (~line 150):

```markdown
### `tengiz cleanup [flags]`

Reclaim disk space by removing unused Docker resources, strictly preserving
Tengiz-managed apps, scale-to-zero stopped containers, previews, and the release
images used for rollback. Runs on the host (all environments).

| Flag | Description |
|------|-------------|
| `-a`, `--all` | Also remove non-Tengiz images and Tengiz release images older than the newest 5 per repository |
| `--volumes` | Also remove unused Docker volumes (does not touch bind mounts) |
| `-n`, `--dry-run` | Show what would be removed without removing anything |
| `-f`, `--force` | Skip the confirmation prompt (CI-safe) |

Uses label-based protection (`tengiz-app` label) and the `tengiz-apps/*`
image prefix so running apps, scale-to-zero stopped containers, previews, and
rollback images are never removed.
```

- [ ] **Step 2: Update `docs/FUTURES_FEATURES.md`**

Change row #6 in the P0 table:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Label-based `docker system prune`. `teng cleanup`. Implemented (2026-08-07). |
```

Also add `— | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-07)` under the "✅ Implemented Features" section.

- [ ] **Step 3: Run full suite + vet**

Run: `go test ./... -v -count=1`
Run: `go vet ./...`
Expected: All PASS / no issues

- [ ] **Step 4: Self-review against spec**

Check requirements from `docs/FUTURES_FEATURES.md` feature #6:
- Label-based protection of Tengiz-managed resources ✅ (Task 2 `pruneContainers`, Task 3 label filter; `teng-apps/*` prefix protection)
- `tengiz cleanup` command ✅ (Task 4)
- Safe default (never touches scale-to-zero/apps/previews/volumes) ✅ (`planCleanup` default path)

Placeholder scan: no "TBD/TODO/implement later/fill in details" remain; every code step has complete code.
Type consistency check:
- `types.ImageInfo{Repository, Tag, ID, CreatedAt string}` + `Ref()` — consistent across Task 1, 2, 3 (`ListImages` populates; helpers read)
- `runtime.CleanupOptions{DryRun, Volumes, All, KeepImages}` — same fields in Task 2 `planCleanup` and Task 3/4 usage
- `types.CleanupPlan{ImagesToRemove, TengizImagesToRemove, ContainersToRemove}` — produced by `planCleanup`, returned by `Cleanup`, printed by `printCleanupResult`
- `runtime.Manager.ListImages/Cleanup` — interface (Task 3) + stub + `mockRTForDeploy` + `dockerRuntime` all implement it

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker housekeeping implemented"
```