# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely prunes unused Docker resources (stopped containers, unused images, networks, build cache, and optionally volumes) using a `label!=tengiz-app` filter so Tengiz-managed containers and images are always preserved.

**Architecture:** A new `internal/cleanup` package wraps the per-category Docker prune commands (`docker container/image/network/builder/volume prune`) — together they are equivalent to a label-filtered `docker system prune`. Each command passes `--filter label!=tengiz-app`, which tells Docker to only prune resources *lacking* the `tengiz-app` label, so every Tengiz-managed container (created by `internal/runtime` and `internal/preview` with `--label tengiz-app=<name>`) survives. Generated Dockerfiles get a `LABEL tengiz-app=managed` instruction so Tengiz-built images also survive `--all` unused-image pruning. The CLI command confirms with the user (unless `--force`) and prints reclaimed space per category. The `Manager` uses an injectable command runner so all prune logic is unit-testable without a Docker daemon.

**Tech Stack:** Go 1.26 (stdlib only — `os/exec`, `context`, `fmt`, `strings`), Cobra (CLI), the existing `docker` CLI on the host. No new external dependencies.

## Global Constraints

- Default environment is `"production"`; `tengiz cleanup` takes **no** `--env` flag — pruning operates on the whole Docker daemon and nothing it removes is env-scoped
- The label filter is exactly `label!=tengiz-app` and must be applied to containers, images, and networks (volumes and build cache have no Tengiz-managed equivalents)
- Volumes are **never** pruned by default; `--volumes` is required (Tengiz mounts host paths, but user-created named volumes must not be deleted without explicit opt-in)
- Default categories (no category flags passed): `containers`, `images`, `networks`, `build-cache`
- `--all` / `-a` removes all unused images, not just dangling images; old tagged deployment images may be removed (only the image referenced by a running container is guaranteed to survive). Rollback safety remains provided by `runtime.KeepLastNImages` during deploy
- Confirmation prompt required unless `--force` / `-f`; empty input means No
- `tengiz cleanup` takes no positional arguments (`cobra.NoArgs`)
- All Docker invocations use `exec.CommandContext` (matches `internal/runtime/docker.go` style) and never the Docker SDK
- Image retention during deploy (`runtime.KeepLastNImages`) is unchanged — this feature is user-initiated housekeeping only
- No new external dependencies
- Docs must be updated: `README.md`, `AGENTS.md`, `docs/FUTURES_FEATURES.md` (repo rule)
- New feature → create branch `feat/docker-housekeeping` before starting (repo rule)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/cleanup/cleanup.go` (Create) | `Category` constants, `Options`, `Result`, `Manager` with injectable `runCmd`, `New()`, `DefaultCategories()`, `BuildCategoryArgs()`, `Clean()`, `parseReclaimedSpace()` |
| `internal/cleanup/cleanup_test.go` (Create) | Unit tests for arg building, default categories, reclaimed-space parsing, and `Clean` command dispatch using a fake runner |
| `internal/cli/cleanup.go` (Create) | `tengiz cleanup` Cobra command, `confirmCleanup()`, `selectedCategories()`, flag registration in `init()` |
| `internal/cli/cleanup_test.go` (Create) | Tests for command registration, flags, confirmation logic, and category selection |
| `internal/builder/builder.go` (Modify) | Append `LABEL tengiz-app=managed` to every generated Dockerfile in `generateDockerfile()` |
| `internal/builder/builder_test.go` (Modify) | Test that generated Dockerfiles carry the managed label |
| `README.md` (Modify) | Add cleanup to the Features list and a `### tengiz cleanup` CLI Reference section |
| `AGENTS.md` (Modify) | Add the `tengiz cleanup` line to the CLI section |
| `docs/FUTURES_FEATURES.md` (Modify) | Mark feature #6 (Docker Housekeeping) as ✅ Implemented |

No new packages depend on `internal/runtime`; the new `internal/cleanup` package is standalone.

---

### Task 1: Create the `internal/cleanup` package

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Test: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only)
- Produces:
  - `type Category string` with constants `CategoryContainers`, `CategoryImages`, `CategoryVolumes`, `CategoryNetworks`, `CategoryBuildCache`
  - `type Options struct { Categories []Category; AllImages bool }`
  - `type Result struct { Reclaimed map[Category]string; Raw map[Category]string; Errors []error }`
  - `type Manager struct { dockerBin string; runCmd func(ctx context.Context, args ...string) (string, error) }`
  - `func New() (*Manager, error)`
  - `func DefaultCategories() []Category`
  - `func BuildCategoryArgs(cat Category, allImages bool) []string`
  - `func (m *Manager) Clean(ctx context.Context, opts Options) (*Result, error)`
  - `func parseReclaimedSpace(output string) string`

- [ ] **Step 1: Create the feature branch**

Run:
```bash
git checkout -b feat/docker-housekeeping
```
Expected: branch created, `git branch --show-current` prints `feat/docker-housekeeping`.

- [ ] **Step 2: Write the failing tests**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestDefaultCategories(t *testing.T) {
	got := DefaultCategories()
	want := []Category{CategoryContainers, CategoryImages, CategoryNetworks, CategoryBuildCache}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DefaultCategories() = %v, want %v", got, want)
	}
}

func TestDefaultCategoriesExcludesVolumes(t *testing.T) {
	for _, c := range DefaultCategories() {
		if c == CategoryVolumes {
			t.Error("volumes must not be pruned by default")
		}
	}
}

func TestBuildCategoryArgs(t *testing.T) {
	tests := []struct {
		cat       Category
		allImages bool
		want      []string
	}{
		{CategoryContainers, false, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{CategoryImages, false, []string{"image", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{CategoryImages, true, []string{"image", "prune", "-f", "--filter", "label!=tengiz-app", "-a"}},
		{CategoryVolumes, false, []string{"volume", "prune", "-f"}},
		{CategoryNetworks, false, []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{CategoryBuildCache, false, []string{"builder", "prune", "-f"}},
		{Category("bogus"), false, nil},
	}
	for _, tt := range tests {
		got := BuildCategoryArgs(tt.cat, tt.allImages)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("BuildCategoryArgs(%q, %v) = %v, want %v", tt.cat, tt.allImages, got, tt.want)
		}
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	output := "Deleted Containers:\nabc123\n\nTotal reclaimed space: 12.34MB\n"
	if got := parseReclaimedSpace(output); got != "12.34MB" {
		t.Errorf("parseReclaimedSpace() = %q, want %q", got, "12.34MB")
	}
}

func TestParseReclaimedSpaceEmpty(t *testing.T) {
	if got := parseReclaimedSpace("nothing here"); got != "" {
		t.Errorf("parseReclaimedSpace() = %q, want empty", got)
	}
}

func TestCleanRunsCommandsForAllDefaultCategories(t *testing.T) {
	var got [][]string
	m := &Manager{runCmd: func(ctx context.Context, args ...string) (string, error) {
		got = append(got, args)
		return "Total reclaimed space: 5MB\n", nil
	}}
	_, err := m.Clean(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"builder", "prune", "-f"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

func TestCleanAggregatesReclaimedSpace(t *testing.T) {
	m := &Manager{runCmd: func(ctx context.Context, args ...string) (string, error) {
		return "Total reclaimed space: 2MB\n", nil
	}}
	res, err := m.Clean(context.Background(), Options{
		Categories: []Category{CategoryContainers, CategoryImages},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reclaimed[CategoryContainers] != "2MB" || res.Reclaimed[CategoryImages] != "2MB" {
		t.Errorf("Reclaimed = %v", res.Reclaimed)
	}
}

func TestCleanAllImagesFlag(t *testing.T) {
	var got []string
	m := &Manager{runCmd: func(ctx context.Context, args ...string) (string, error) {
		got = args
		return "", nil
	}}
	_, err := m.Clean(context.Background(), Options{
		Categories: []Category{CategoryImages},
		AllImages:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"image", "prune", "-f", "--filter", "label!=tengiz-app", "-a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestCleanContinuesAfterCategoryError(t *testing.T) {
	calls := 0
	m := &Manager{runCmd: func(ctx context.Context, args ...string) (string, error) {
		calls++
		if calls == 1 {
			return "boom\n", errors.New("boom")
		}
		return "Total reclaimed space: 1MB\n", nil
	}}
	res, err := m.Clean(context.Background(), Options{})
	if err == nil {
		t.Fatal("expected error when a category prune fails")
	}
	if calls != 4 {
		t.Errorf("expected all categories attempted, calls = %d", calls)
	}
	if len(res.Errors) != 1 {
		t.Errorf("expected 1 recorded error, got %d", len(res.Errors))
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL — compile error `no required module provides package github.com/yaso09/tengiz/internal/cleanup` (package does not exist yet).

- [ ] **Step 4: Write the minimal implementation**

Create `internal/cleanup/cleanup.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Category string

const (
	CategoryContainers Category = "containers"
	CategoryImages     Category = "images"
	CategoryVolumes    Category = "volumes"
	CategoryNetworks   Category = "networks"
	CategoryBuildCache Category = "build-cache"
)

// labelFilter tells Docker to prune only resources that do NOT carry the
// tengiz-app label. Every Tengiz-managed container and image carries it,
// so they are always preserved.
const labelFilter = "label!=tengiz-app"

type Options struct {
	Categories []Category
	AllImages  bool
}

type Result struct {
	Reclaimed map[Category]string
	Raw       map[Category]string
	Errors    []error
}

type Manager struct {
	dockerBin string
	runCmd    func(ctx context.Context, args ...string) (string, error)
}

func New() (*Manager, error) {
	bin, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	m := &Manager{dockerBin: bin}
	m.runCmd = m.execDocker
	return m, nil
}

func (m *Manager) execDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, m.dockerBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return string(out), nil
}

func DefaultCategories() []Category {
	return []Category{CategoryContainers, CategoryImages, CategoryNetworks, CategoryBuildCache}
}

func BuildCategoryArgs(cat Category, allImages bool) []string {
	switch cat {
	case CategoryContainers:
		return []string{"container", "prune", "-f", "--filter", labelFilter}
	case CategoryImages:
		args := []string{"image", "prune", "-f", "--filter", labelFilter}
		if allImages {
			args = append(args, "-a")
		}
		return args
	case CategoryVolumes:
		return []string{"volume", "prune", "-f"}
	case CategoryNetworks:
		return []string{"network", "prune", "-f", "--filter", labelFilter}
	case CategoryBuildCache:
		return []string{"builder", "prune", "-f"}
	default:
		return nil
	}
}

func (m *Manager) Clean(ctx context.Context, opts Options) (*Result, error) {
	cats := opts.Categories
	if len(cats) == 0 {
		cats = DefaultCategories()
	}
	res := &Result{
		Reclaimed: make(map[Category]string, len(cats)),
		Raw:       make(map[Category]string, len(cats)),
	}
	for _, cat := range cats {
		out, err := m.runCmd(ctx, BuildCategoryArgs(cat, opts.AllImages)...)
		res.Raw[cat] = out
		res.Reclaimed[cat] = parseReclaimedSpace(out)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("%s: %w", cat, err))
		}
	}
	if len(res.Errors) > 0 {
		return res, fmt.Errorf("cleanup completed with %d error(s)", len(res.Errors))
	}
	return res, nil
}

func parseReclaimedSpace(output string) string {
	const prefix = "Total reclaimed space:"
	for _, line := range strings.Split(output, "\n") {
		if i := strings.Index(line, prefix); i >= 0 {
			return strings.TrimSpace(line[i+len(prefix):])
		}
	}
	return ""
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: All PASS (8 test functions).

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/
git commit -m "feat: add internal/cleanup package with label-filtered docker prune"
```

---

### Task 2: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: from Task 1 — `cleanup.New()`, `cleanup.DefaultCategories()`, `cleanup.Options{Categories, AllImages}`, `cleanup.Category` constants, `cleanup.Clean(ctx, opts) (*cleanup.Result, error)`
- Produces: `tengiz cleanup` registered on `rootCmd`; package funcs `confirmCleanup(r io.Reader, force bool) (bool, error)` and `selectedCategories(containers, images, volumes, networks, buildCache bool) []cleanup.Category`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/cleanup"
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

func TestCleanupCommandFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, flag := range []string{"force", "all", "containers", "images", "volumes", "networks", "build-cache"} {
		if flags.Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestConfirmCleanupForce(t *testing.T) {
	ok, err := confirmCleanup(strings.NewReader("n\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("force=true should skip the prompt and return true")
	}
}

func TestConfirmCleanupYes(t *testing.T) {
	ok, err := confirmCleanup(strings.NewReader("y\n"), false)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("'y' answer should return true")
	}
}

func TestConfirmCleanupNo(t *testing.T) {
	ok, err := confirmCleanup(strings.NewReader("n\n"), false)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("'n' answer should return false")
	}
}

func TestConfirmCleanupEmptyIsNo(t *testing.T) {
	ok, err := confirmCleanup(strings.NewReader("\n"), false)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("empty answer should default to No")
	}
}

func TestSelectedCategoriesDefault(t *testing.T) {
	got := selectedCategories(false, false, false, false, false)
	want := cleanup.DefaultCategories()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectedCategories() = %v, want %v", got, want)
	}
}

func TestSelectedCategoriesExplicit(t *testing.T) {
	got := selectedCategories(true, false, true, false, true)
	want := []cleanup.Category{cleanup.CategoryContainers, cleanup.CategoryVolumes, cleanup.CategoryBuildCache}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectedCategories() = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup|TestConfirmCleanup|TestSelectedCategories" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd`, `undefined: confirmCleanup`, `undefined: selectedCategories`.

- [ ] **Step 3: Write the minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to free disk space",
	Long: `Remove unused Docker resources (stopped containers, unused images,
networks, build cache, and optionally volumes) to free disk space.

By default every category except volumes is pruned. Tengiz-managed
containers and images are always protected via the tengiz-app label.

Examples:
  tengiz cleanup                 # prune containers, images, networks, build cache
  tengiz cleanup --force         # skip the confirmation prompt
  tengiz cleanup --volumes       # also prune unused volumes
  tengiz cleanup --images --all  # remove ALL unused images, not just dangling
  tengiz cleanup --build-cache   # prune only the build cache`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		allImages, _ := cmd.Flags().GetBool("all")

		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")

		ok, err := confirmCleanup(cmd.InOrStdin(), force)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("[tengiz] cleanup cancelled")
			return nil
		}

		cats := selectedCategories(containers, images, volumes, networks, buildCache)

		m, err := cleanup.New()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		res, err := m.Clean(cmd.Context(), cleanup.Options{Categories: cats, AllImages: allImages})
		if err != nil {
			fmt.Printf("[tengiz] warning: %v\n", err)
		}

		fmt.Println("[tengiz] cleanup complete")
		for _, cat := range cats {
			reclaimed := res.Reclaimed[cat]
			if reclaimed == "" {
				reclaimed = "unknown"
			}
			fmt.Printf("  %-14s reclaimed %s\n", string(cat), reclaimed)
		}
		return nil
	},
}

func confirmCleanup(r io.Reader, force bool) (bool, error) {
	if force {
		return true, nil
	}
	fmt.Print("This will remove unused Docker resources. Continue? [y/N] ")
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.TrimSpace(line)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

func selectedCategories(containers, images, volumes, networks, buildCache bool) []cleanup.Category {
	if !containers && !images && !volumes && !networks && !buildCache {
		return cleanup.DefaultCategories()
	}
	var cats []cleanup.Category
	if containers {
		cats = append(cats, cleanup.CategoryContainers)
	}
	if images {
		cats = append(cats, cleanup.CategoryImages)
	}
	if volumes {
		cats = append(cats, cleanup.CategoryVolumes)
	}
	if networks {
		cats = append(cats, cleanup.CategoryNetworks)
	}
	if buildCache {
		cats = append(cats, cleanup.CategoryBuildCache)
	}
	return cats
}

func init() {
	cleanupCmd.Flags().BoolP("force", "f", false, "skip the confirmation prompt")
	cleanupCmd.Flags().BoolP("all", "a", false, "remove all unused images, not just dangling images")
	cleanupCmd.Flags().Bool("containers", false, "prune only stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune only unused images")
	cleanupCmd.Flags().Bool("volumes", false, "prune only unused volumes (not included by default)")
	cleanupCmd.Flags().Bool("networks", false, "prune only unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune only the Docker build cache")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestConfirmCleanup|TestSelectedCategories" -v -count=1`

Expected: All PASS (9 test functions).

- [ ] **Step 5: Build the binary**

Run: `go build -o tengiz .`

Expected: build succeeds with no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 3: Label generated Docker images so `--all` pruning preserves them

**Files:**
- Modify: `internal/builder/builder.go:172-269` — `generateDockerfile()`
- Modify: `internal/builder/builder_test.go` — add label test

**Interfaces:**
- Consumes: nothing new
- Produces: every generated Dockerfile ends with `LABEL tengiz-app=managed` so `docker image prune --filter label!=tengiz-app --all` keeps Tengiz-built images (rollback safety under `--all`)

- [ ] **Step 1: Write the failing test**

Add to `internal/builder/builder_test.go`:

```go
func TestGenerateDockerfileHasManagedLabel(t *testing.T) {
	d := &Detection{Framework: FrameworkNode, InternalPort: 3000}
	df := generateDockerfile(d)
	if !strings.Contains(df, "LABEL tengiz-app=managed") {
		t.Error("generated Dockerfile missing LABEL tengiz-app=managed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -run "TestGenerateDockerfileHasManagedLabel" -v -count=1`

Expected: FAIL — `generated Dockerfile missing LABEL tengiz-app=managed`.

- [ ] **Step 3: Write the minimal implementation**

In `internal/builder/builder.go`, inside `generateDockerfile`, immediately after the `switch` block ends (the closing `}` of the `default:` case, line 246) and before the `if d.HealthCheck != nil ...` block, insert:

```go
	df += "\nLABEL tengiz-app=managed\n"
```

The surrounding code must now read:

```go
	default:
		df = fmt.Sprintf(`FROM alpine
EXPOSE %d
CMD ["echo", "no dockerfile generated for this framework"]`, port)
	}

	df += "\nLABEL tengiz-app=managed\n"

	if d.HealthCheck != nil && d.HealthCheck.Enabled {
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/... -run "TestGenerateDockerfileHasManagedLabel" -v -count=1`

Expected: PASS.

- [ ] **Step 5: Run all builder tests**

Run: `go test ./internal/builder/... -v -count=1`

Expected: All PASS (existing tests unchanged; the health-check tests still pass because the LABEL line is appended before the HEALTHCHECK block).

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: label generated Docker images with tengiz-app for prune protection"
```

---

### Task 4: Update documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the finished `tengiz cleanup` command from Task 2
- Produces: documented feature and command surface consistent with the repo rule that docs are updated with UI/UX changes

- [ ] **Step 1: Add the feature bullet to `README.md`**

In `README.md` under `## Features`, append after the "Self-contained" bullet (line 23):

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, images, networks, and build cache with label-based protection so Tengiz-managed apps are never removed.
```

- [ ] **Step 2: Add the CLI reference section to `README.md`**

In `README.md` under `## CLI Reference`, after the `### tengiz run ...` section (after line 204) and before `### tengiz start <app>`, insert:

```markdown
### `tengiz cleanup [--force] [--all] [--containers|--images|--volumes|--networks|--build-cache]`

Remove unused Docker resources to free disk space.

| Flag | Description |
|------|-------------|
| `-f`, `--force` | Skip the confirmation prompt |
| `-a`, `--all` | Remove all unused images, not just dangling images |
| `--containers` | Prune only stopped containers |
| `--images` | Prune only unused images |
| `--volumes` | Prune only unused volumes (not included by default) |
| `--networks` | Prune only unused networks |
| `--build-cache` | Prune only the Docker build cache |

With no category flags, prunes containers, images, networks, and build cache (volumes excluded by default). Tengiz-managed containers and images are always protected via the `tengiz-app` label. Examples:

```
tengiz cleanup                # prune all default categories (asks to confirm)
tengiz cleanup --force        # prune without prompting
tengiz cleanup --volumes      # also prune unused volumes
tengiz cleanup --images --all # remove ALL unused images
```
```

- [ ] **Step 3: Add the command to `AGENTS.md`**

In `AGENTS.md` under the `## CLI` block, after the `tengiz rollback <app>` line (line 60), add:

```
tengiz cleanup [--force] [--all] [--containers|--images|--volumes|--networks|--build-cache] → prune unused Docker resources (Tengiz-managed apps protected)
```

- [ ] **Step 4: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In `docs/FUTURES_FEATURES.md`:

1. In the `### P0 — Critical` table, change line 19 (`| 6 | **Docker Housekeeping** ⬜ |`) to use `✅`:
   `| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | ...` (keep the rest of the row unchanged).
2. In the `### ✅ Implemented Features (Not Pending)` table, add a row:
   `| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-14) |`
3. In the `## Docker Housekeeping (Otomatik Temizlik)` feature entry (line 377), add a status line after the `**Detected:** 2026-07-14` line:
   `- **Status:** ✅ Implemented (2026-08-14)`

- [ ] **Step 5: Run the full test suite and vet**

Run: `go test ./... -v -count=1`

Expected: All PASS. (Note: `internal/proxy` tests may take ~2s each and `internal/idle` tests are time-sensitive — pre-existing behavior, not caused by this change.)

Run: `go vet ./...`

Expected: No issues.

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker housekeeping as implemented"
```

---

## Self-Review

- [ ] **Step 1: Spec coverage**

Feature #6 from `docs/FUTURES_FEATURES.md`: "Label-based `docker system prune`. `tengiz cleanup`."
- `tengiz cleanup` command → Task 2
- Label-based pruning (`label!=tengiz-app`) → Task 1 (`BuildCategoryArgs`) + Task 3 (image labels)
- Disk-space protection for Tengiz-managed resources → Task 1 label filter; runtime/preview containers already carry `tengiz-app` label
- `docker system prune` behavior → achieved via per-category prunes in Task 1 (containers/images/networks/build-cache), which is the same operation with finer control
- Docs update requirement (repo rule) → Task 4

- [ ] **Step 2: Placeholder scan**

Search the plan for `TBD`, `TODO`, `implement later`, `fill in details`, `add appropriate error handling`, `similar to Task`. None present — every code step includes complete code and every run step includes the expected output.

- [ ] **Step 3: Type consistency**

- `cleanup.Category` constants used in Task 1, Task 2, Task 3 tests, and Task 4 — identical names everywhere (`CategoryContainers`, `CategoryImages`, `CategoryVolumes`, `CategoryNetworks`, `CategoryBuildCache`)
- `cleanup.Options{Categories, AllImages}` — same field names in Task 1 (`Clean`) and Task 2 (`m.Clean`)
- `cleanup.DefaultCategories() []Category` — defined Task 1, used Task 2 (`selectedCategories`) and Task 1 test
- `confirmCleanup(r io.Reader, force bool) (bool, error)` — same signature in Task 2 implementation and tests
- `selectedCategories(...bool) []cleanup.Category` — same signature in Task 2 implementation and tests
- Label string `tengiz-app` matches the existing runtime label key `tengiz-app` (runtime.go labelKey) and the new builder `LABEL tengiz-app=managed` — no naming drift