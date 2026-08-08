# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command backed by label-scoped Docker pruning so operators can reclaim disk space on single-server deployments without ever touching non-Tengiz Docker resources.

**Architecture:** Extend the existing `runtime.Manager` interface with a single `Cleanup(ctx, opts) (CleanupResult, error)` method. `dockerRuntime` implements it by shelling out to label-scoped docker commands: `docker container prune --filter label=tengiz-app`, `docker image prune --filter reference=tengiz-apps/*` (dangling-only by default, `-a` with `--all`), `docker volume prune --filter label=tengiz-app` (opt-in `--volumes`), and `docker builder prune` (opt-in `--cache`). Pure unexported helpers build the docker argv and parse prune output so the behavior is unit-testable without a Docker daemon (matching the repo's existing convention of testing arg-builders and the stub rather than exec-based docker calls). A new Cobra `cleanupCmd` wires flags to the runtime and prints a summary.

**Tech Stack:** Go 1.26, Cobra, `os/exec` docker CLI. No new external dependencies.

## Global Constraints

- Every prune must be scoped to Tengiz-owned resources: containers/volumes via `label=tengiz-app`, images via `reference=tengiz-apps/*`. Never remove non-Tengiz resources.
- Default image pruning removes **dangling** images only; `--all` enables full unused-image pruning (`docker image prune -a`).
- `--volumes` is destructive (removes named volumes and their data) — print a warning before running.
- Build-cache pruning (`docker builder prune`) is **not** label-scoped; it runs only when `--cache` is explicitly passed.
- `--dry-run` lists candidates without removing; build-cache dry-run is not supported by docker, so cache is reported but not listed.
- Cleanup is host-global (operates on all Tengiz resources regardless of `--env`); container names and image tags already carry the labels/filters we rely on.
- Must be testable without a Docker daemon: pure arg-builder + output-parser helpers get real unit tests; the exec-based `dockerRuntime.Cleanup` is exercised only via the stub (repo convention).
- Adding `Cleanup` to `runtime.Manager` requires updating the stub (`stubManager`) and 3 existing test mocks: `mockRTForDeploy` (internal/cli/root_test.go), `mockRuntime` (internal/proxy/proxy_test.go), `mockRuntime` (internal/idle/idle_test.go).
- No new external dependencies.
- `go build -o tengiz .`, `go test ./... -v -count=1`, and `go vet ./...` must pass.
- Every change ships with its tests; commit after each task (repo rule: feature branch `feat/docker-cleanup`).

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `CleanupOptions`, `CleanupResult`, pure arg-builder + parser helpers, `dockerRuntime.Cleanup`, `runDocker` helper |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager.Cleanup` |
| `internal/runtime/cleanup_test.go` | Unit tests for helpers + stub `Cleanup` |
| `internal/cli/root.go` | New `cleanupCmd` + registration + flags |
| `internal/cli/root_test.go` | CLI registration/flag/arg tests + `Cleanup` on `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` to `mockRuntime` |
| `README.md` | CLI Reference section for `tengiz cleanup` |
| `AGENTS.md` | Add command to CLI list + note `Cleanup` on `runtime.Manager` row |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 ✅ Implemented |

---

### Task 1: Runtime cleanup helpers (pure functions)

**Files:**
- Modify: `internal/runtime/cleanup.go` — add helpers
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `labelKey` and `envLabelKey` consts already defined in `internal/runtime/docker.go:76-77`
- Produces (all unexported, package `runtime`):
  - `containerCleanupListArgs() []string`
  - `containerCleanupPruneArgs() []string`
  - `imageCleanupListArgs(all bool) []string`
  - `imageCleanupPruneArgs(all bool) []string`
  - `volumeCleanupListArgs() []string`
  - `volumeCleanupPruneArgs() []string`
  - `cacheCleanupPruneArgs() []string`
  - `parseIDList(output string) []string`
  - `parsePruneOutput(output string) (ids []string, space string)`
  - `runDocker(ctx context.Context, args ...string) (string, error)`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-cleanup
```

- [ ] **Step 2: Write the failing tests**

Add to `internal/runtime/cleanup_test.go`:

```go
package runtime

import (
	"reflect"
	"testing"
)

func TestContainerCleanupListArgs(t *testing.T) {
	got := containerCleanupListArgs()
	want := []string{"ps", "-a", "--filter", "label=tengiz-app", "--filter", "status=exited", "--format", "{{.ID}}"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("containerCleanupListArgs() = %v, want %v", got, want)
	}
}

func TestContainerCleanupPruneArgs(t *testing.T) {
	got := containerCleanupPruneArgs()
	want := []string{"container", "prune", "-f", "--filter", "label=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("containerCleanupPruneArgs() = %v, want %v", got, want)
	}
}

func TestImageCleanupArgs(t *testing.T) {
	got := imageCleanupPruneArgs(false)
	want := []string{"image", "prune", "-f", "--filter", "reference=tengiz-apps/*", "--filter", "dangling=true"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imageCleanupPruneArgs(false) = %v, want %v", got, want)
	}

	got = imageCleanupPruneArgs(true)
	want = []string{"image", "prune", "-f", "-a", "--filter", "reference=tengiz-apps/*"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imageCleanupPruneArgs(true) = %v, want %v", got, want)
	}

	got = imageCleanupListArgs(false)
	want = []string{"images", "--format", "{{.ID}}", "--filter", "reference=tengiz-apps/*", "--filter", "dangling=true"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imageCleanupListArgs(false) = %v, want %v", got, want)
	}

	got = imageCleanupListArgs(true)
	want = []string{"images", "--format", "{{.ID}}", "--filter", "reference=tengiz-apps/*"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imageCleanupListArgs(true) = %v, want %v", got, want)
	}
}

func TestVolumeCleanupArgs(t *testing.T) {
	got := volumeCleanupListArgs()
	want := []string{"volume", "ls", "--format", "{{.Name}}", "--filter", "label=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("volumeCleanupListArgs() = %v, want %v", got, want)
	}

	got = volumeCleanupPruneArgs()
	want = []string{"volume", "prune", "-f", "--filter", "label=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("volumeCleanupPruneArgs() = %v, want %v", got, want)
	}
}

func TestCacheCleanupPruneArgs(t *testing.T) {
	got := cacheCleanupPruneArgs()
	want := []string{"builder", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cacheCleanupPruneArgs() = %v, want %v", got, want)
	}
}

func TestParseIDList(t *testing.T) {
	if got := parseIDList(""); got != nil {
		t.Fatalf("parseIDList(\"\") = %v, want nil", got)
	}

	got := parseIDList("abc123\ndef456\n\n")
	want := []string{"abc123", "def456"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseIDList() = %v, want %v", got, want)
	}
}

func TestParsePruneOutput(t *testing.T) {
	containerOut := `Deleted Containers:
abc123
def456

Total reclaimed space: 1.2GB
`
	ids, space := parsePruneOutput(containerOut)
	if !reflect.DeepEqual(ids, []string{"abc123", "def456"}) {
		t.Fatalf("container ids = %v", ids)
	}
	if space != "1.2GB" {
		t.Fatalf("container space = %q, want 1.2GB", space)
	}

	imageOut := `Deleted Images:
untagged: tengiz-apps/myapp:production-123
deleted: sha256:abc

Total reclaimed space: 350MB
`
	ids, space = parsePruneOutput(imageOut)
	if !reflect.DeepEqual(ids, []string{"sha256:abc"}) {
		t.Fatalf("image ids = %v", ids)
	}
	if space != "350MB" {
		t.Fatalf("image space = %q, want 350MB", space)
	}

	_, space = parsePruneOutput("Deleted Volumes:\n")
	if space != "" {
		t.Fatalf("space = %q, want empty", space)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestContainerCleanup|TestImageCleanup|TestVolumeCleanup|TestCacheCleanup|TestParseIDList|TestParsePruneOutput' -v`
Expected: compile error — `undefined: containerCleanupListArgs` (functions don't exist yet)

- [ ] **Step 4: Write the minimal implementation**

Append to `internal/runtime/cleanup.go`:

```go
const imageRepoFilter = "reference=tengiz-apps/*"

func containerCleanupListArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", fmt.Sprintf("label=%s", labelKey),
		"--filter", "status=exited",
		"--format", "{{.ID}}",
	}
}

func containerCleanupPruneArgs() []string {
	return []string{
		"container", "prune", "-f",
		"--filter", fmt.Sprintf("label=%s", labelKey),
	}
}

func imageCleanupListArgs(all bool) []string {
	args := []string{"images", "--format", "{{.ID}}", "--filter", imageRepoFilter}
	if !all {
		args = append(args, "--filter", "dangling=true")
	}
	return args
}

func imageCleanupPruneArgs(all bool) []string {
	args := []string{"image", "prune", "-f", "--filter", imageRepoFilter}
	if all {
		args = append(args, "-a")
	} else {
		args = append(args, "--filter", "dangling=true")
	}
	return args
}

func volumeCleanupListArgs() []string {
	return []string{
		"volume", "ls",
		"--format", "{{.Name}}",
		"--filter", fmt.Sprintf("label=%s", labelKey),
	}
}

func volumeCleanupPruneArgs() []string {
	return []string{
		"volume", "prune", "-f",
		"--filter", fmt.Sprintf("label=%s", labelKey),
	}
}

func cacheCleanupPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func parseIDList(output string) []string {
	var ids []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func parsePruneOutput(output string) ([]string, string) {
	var ids []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "Deleted "):
			continue
		case strings.HasPrefix(line, "Total"):
			continue
		case strings.HasPrefix(line, "untagged:"):
			continue
		case strings.HasPrefix(line, "deleted:"):
			ids = append(ids, strings.TrimSpace(strings.TrimPrefix(line, "deleted:")))
		default:
			ids = append(ids, line)
		}
	}

	space := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			space = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			break
		}
	}
	if space == "" {
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Total:") {
				space = strings.TrimSpace(strings.TrimPrefix(line, "Total:"))
				break
			}
		}
	}
	return ids, space
}

func runDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestContainerCleanup|TestImageCleanup|TestVolumeCleanup|TestCacheCleanup|TestParseIDList|TestParsePruneOutput' -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add label-scoped docker cleanup helpers"
```

---

### Task 2: `Cleanup` on the runtime Manager (interface + dockerRuntime + stub + mocks)

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `CleanupOptions`, `CleanupResult`, `dockerRuntime.Cleanup`
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface; add `stubManager.Cleanup`
- Modify: `internal/cli/root_test.go` — add `Cleanup` to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go` — add `Cleanup` to `mockRuntime`
- Modify: `internal/idle/idle_test.go` — add `Cleanup` to `mockRuntime`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: all helpers from Task 1 (`containerCleanupListArgs`, `imageCleanupPruneArgs`, etc., `parseIDList`, `parsePruneOutput`, `runDocker`)
- Produces:
  - `type CleanupOptions struct { All, Volumes, Cache, DryRun bool }`
  - `type CleanupResult struct { Containers []string; Images []string; Volumes []string; Cache bool; Space string }`
  - `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` (interface method)

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/cleanup_test.go` (add `"context"` to the existing imports):

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{All: true, Volumes: true, Cache: true})
	if err != nil {
		t.Fatalf("stub Cleanup() error = %v", err)
	}
	if len(res.Containers) != 0 || len(res.Images) != 0 || len(res.Volumes) != 0 {
		t.Fatalf("stub Cleanup() = %+v, want empty result", res)
	}
	if res.Cache || res.Space != "" {
		t.Fatalf("stub Cleanup() = %+v, want Cache=false and empty Space", res)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v`
Expected: FAIL — `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add the types and interface method**

In `internal/runtime/cleanup.go`, above `const imageRepoFilter`, add:

```go
type CleanupOptions struct {
	All     bool // also remove all unused Tengiz images, not only dangling
	Volumes bool // also remove unused named volumes
	Cache   bool // also prune the Docker build cache
	DryRun  bool // report candidates without removing anything
}

type CleanupResult struct {
	Containers []string
	Images     []string
	Volumes    []string
	Cache      bool
	Space      string // human-readable reclaimed space (comma-separated across steps)
}
```

In `internal/runtime/runtime.go`, add to the `Manager` interface (after the `KeepLastNImages` line):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add to `stubManager` (after `KeepLastNImages`):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 4: Implement `dockerRuntime.Cleanup`**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult
	var spaces []string
	addSpace := func(space string) {
		if space != "" {
			spaces = append(spaces, space)
		}
	}

	if opts.DryRun {
		out, err := runDocker(ctx, containerCleanupListArgs()...)
		if err != nil {
			return res, fmt.Errorf("list containers: %w", err)
		}
		res.Containers = parseIDList(out)
	} else {
		out, err := runDocker(ctx, containerCleanupPruneArgs()...)
		if err != nil {
			return res, fmt.Errorf("prune containers: %w", err)
		}
		ids, space := parsePruneOutput(out)
		res.Containers = ids
		addSpace(space)
	}

	if opts.DryRun {
		out, err := runDocker(ctx, imageCleanupListArgs(opts.All)...)
		if err != nil {
			return res, fmt.Errorf("list images: %w", err)
		}
		res.Images = parseIDList(out)
	} else {
		out, err := runDocker(ctx, imageCleanupPruneArgs(opts.All)...)
		if err != nil {
			return res, fmt.Errorf("prune images: %w", err)
		}
		ids, space := parsePruneOutput(out)
		res.Images = ids
		addSpace(space)
	}

	if opts.Volumes {
		if opts.DryRun {
			out, err := runDocker(ctx, volumeCleanupListArgs()...)
			if err != nil {
				return res, fmt.Errorf("list volumes: %w", err)
			}
			res.Volumes = parseIDList(out)
		} else {
			out, err := runDocker(ctx, volumeCleanupPruneArgs()...)
			if err != nil {
				return res, fmt.Errorf("prune volumes: %w", err)
			}
			ids, space := parsePruneOutput(out)
			res.Volumes = ids
			addSpace(space)
		}
	}

	if opts.Cache {
		res.Cache = true
		if !opts.DryRun {
			out, err := runDocker(ctx, cacheCleanupPruneArgs()...)
			if err != nil {
				return res, fmt.Errorf("prune build cache: %w", err)
			}
			_, space := parsePruneOutput(out)
			addSpace(space)
		}
	}

	res.Space = strings.Join(spaces, ", ")
	return res, nil
}
```

- [ ] **Step 5: Update the 3 test mocks**

In `internal/cli/root_test.go`, add to `mockRTForDeploy`:

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

In `internal/proxy/proxy_test.go`, add to `mockRuntime`:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

In `internal/idle/idle_test.go`, add to `mockRuntime`:

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./... -v -count=1`
Expected: all PASS (stub test passes; interface compile error in the 3 mock packages resolved)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add Cleanup to Manager with dockerRuntime + stub implementation"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, register it, add flags
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker() Manager`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `getEnv(cmd)` (root.go:97)
- Produces: `cleanupCmd *cobra.Command` (registered as `cleanup` on root), flags `--all`, `--volumes`, `--cache`, `--dry-run`

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/root_test.go`:

```go
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
	for _, flag := range []string{"all", "volumes", "cache", "dry-run"} {
		if f := cleanupCmd.Flags().Lookup(flag); f == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupCommandRejectsArgs(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "extra"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when passing arguments to cleanup")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v`
Expected: FAIL — `undefined: cleanupCmd`

- [ ] **Step 3: Write the minimal implementation**

In `internal/cli/root.go`, register the command in `init()` (after `rootCmd.AddCommand(runCmd)` at line 67):

```go
	rootCmd.AddCommand(cleanupCmd)
```

Add flags in `init()` (after the `initCmd.Flags().String("git-branch", ...)` block):

```go
	cleanupCmd.Flags().Bool("all", false, "also remove all unused Tengiz images (not only dangling)")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused named volumes (removes their data)")
	cleanupCmd.Flags().Bool("cache", false, "also prune the Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "list what would be removed without removing anything")
```

Add the command definition (place it after `psCmd`, before `stopCmd` at line 603):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Tengiz Docker resources to reclaim disk space",
	Long: `Prunes Docker resources owned by Tengiz to reclaim disk space on the host.

Safe by default: only removes exited Tengiz containers, dangling Tengiz images
(tengiz-apps/*), and — with --volumes — unused named volumes labeled with the
Tengiz app label. Non-Tengiz Docker resources are never touched.

Flags:
  --all        also remove all unused Tengiz images, not just dangling ones
  --volumes    also remove unused named volumes (removes their data!)
  --cache      also prune the Docker build cache
  --dry-run    only list what would be removed, without removing anything`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		cache, _ := cmd.Flags().GetBool("cache")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if volumes && !dryRun {
			fmt.Println("[tengiz] WARNING: --volumes removes unused named volumes and their data")
		}

		opts := runtime.CleanupOptions{All: all, Volumes: volumes, Cache: cache, DryRun: dryRun}
		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "removed"
		if dryRun {
			verb = "would remove"
		}
		fmt.Printf("[tengiz] containers %s: %d\n", verb, len(result.Containers))
		fmt.Printf("[tengiz] images %s: %d\n", verb, len(result.Images))
		if volumes {
			fmt.Printf("[tengiz] volumes %s: %d\n", verb, len(result.Volumes))
		}
		if cache {
			if dryRun {
				fmt.Println("[tengiz] build cache: dry-run (docker does not support cache dry-run)")
			} else {
				fmt.Println("[tengiz] build cache pruned")
			}
		}
		if result.Space != "" && !dryRun {
			fmt.Printf("[tengiz] space reclaimed: %s\n", result.Space)
		}
		if len(result.Containers)+len(result.Images)+len(result.Volumes) == 0 && !dryRun {
			fmt.Println("[tengiz] nothing to clean up")
		}
		return nil
	},
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v`
Expected: all PASS

- [ ] **Step 5: Run the full suite + vet**

```bash
go test ./... -v -count=1
go vet ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/FUTURES_FEATURES.md`

**Interfaces:**
- Consumes: the `tengiz cleanup` command surface from Task 3 (flags `--all`, `--volumes`, `--cache`, `--dry-run`)

- [ ] **Step 1: Add the CLI reference section to README.md**

Insert after the `### \`tengiz ps\`` section (README.md:146) — i.e., before `### \`tengiz rollback <app>\``:

```markdown
### `tengiz cleanup`

Remove unused Tengiz Docker resources to reclaim disk space on the host.

| Flag | Description |
|------|-------------|
| `--all` | Also remove all unused Tengiz images, not just dangling ones |
| `--volumes` | Also remove unused named volumes (removes their data) |
| `--cache` | Also prune the Docker build cache |
| `--dry-run` | List what would be removed without removing anything |

Safe by default: only removes exited Tengiz containers, dangling Tengiz images (`tengiz-apps/*`), and unused Tengiz-labeled volumes (with `--volumes`). Never touches non-Tengiz Docker resources. Operates host-wide, across all environments.
```

- [ ] **Step 2: Update AGENTS.md**

In the CLI block (after `tengiz ps             → list apps from Docker`, AGENTS.md:43), add:

```
tengiz cleanup [--all] [--volumes] [--cache] [--dry-run] → prune unused Tengiz Docker resources (label-scoped)
```

In the `runtime.Manager` row of the architecture table (AGENTS.md:15), extend the description:

```
| `runtime.Manager` | Interface for container lifecycle. `NewDocker()` = exec-based impl, `NewStub()` = test mock. Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup. `Cleanup(ctx, CleanupOptions) (CleanupResult, error)` for label-scoped Docker pruning (containers/images/volumes/build cache). `ContainerName(name, env)` helper. |
```

- [ ] **Step 3: Mark feature #6 implemented in FUTURES_FEATURES.md**

In the P0 table (docs/FUTURES_FEATURES.md:17), change the status marker from `⬜` to `✅`:

```
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the "Implemented Features (Not Pending)" table (after the Webhook row, line 295):

```
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-08) |
```

- [ ] **Step 4: Verify docs build / no code regressions**

```bash
go build -o tengiz .
go test ./... -v -count=1
```

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**Spec coverage** (feature #6 rationale: "Disk space is #1 production issue... label-based `docker system prune`. `tengiz cleanup`."):
- Reclaim disk space → Task 2 `Cleanup` runs prunes + reports reclaimed space. ✅
- Label-based scoping (never touch non-Tengiz resources) → `label=tengiz-app` (containers/volumes) + `reference=tengiz-apps/*` (images) filters; Global Constraints enforce it. ✅
- `tengiz cleanup` command → Task 3. ✅
- Build cache → `--cache` flag (Task 3) using `docker builder prune` (Task 1 helper). ✅
- Docs/UI rule (AGENTS.md) → Task 4 README + AGENTS.md + feature status. ✅
- Test-every-change + feature branch rule → each task ends with tests passing + commit; Task 1 creates `feat/docker-cleanup`. ✅

**Placeholder scan:** No TBD/TODO/"add error handling"/"similar to Task N" — all steps contain exact code and expected output.

**Type consistency:**
- `CleanupOptions{All, Volumes, Cache, DryRun bool}` and `CleanupResult{Containers, Images, Volumes []string; Cache bool; Space string}` are defined once (Task 2) and referenced identically in Task 3 (`runtime.CleanupOptions`, `runtime.CleanupResult`).
- Helper names/signatures in Task 1 (`containerCleanupListArgs()`, `imageCleanupPruneArgs(all bool)`, `parseIDList(string)`, `parsePruneOutput(string) ([]string, string)`, `runDocker(ctx, args...)`) are consumed verbatim in Task 2.
- Interface method `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` is the same across interface, stub, dockerRuntime, and all 3 mocks.
- `labelKey`/`envLabelKey` consts already exist (docker.go:76-77); `imageRepoFilter` defined in Task 1 and used only there.
