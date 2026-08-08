# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that removes unused Docker resources (stopped containers, unused images, unused volumes, unused networks) while protecting all Tengiz-managed containers and Tengiz-built images, so single-server disk usage stays under control.

**Architecture:** The `runtime.Manager` interface gains a `Cleanup(ctx, CleanupOptions) (*CleanupResult, error)` method. The `dockerRuntime` implementation uses the Docker CLI via `os/exec` (no SDK): it lists containers with `docker ps -a` (JSON), images with `docker images`, dangling volumes/networks with `docker volume ls -f dangling=true` / `docker network ls -f dangling=true`, and deletes with `docker rm` / `docker rmi` / `docker volume rm` / `docker network rm`. The safety rules (which containers/images may be removed) live in pure, unit-testable functions `selectContainersForRemoval` and `selectImagesForRemoval` — no real Docker required for the logic tests. The CLI exposes `tengiz cleanup` with per-category flags plus `--dry-run`; running it with no category flags cleans everything.

**Tech Stack:** Go 1.26, Cobra, `os/exec` Docker CLI (no Docker SDK). No new external dependencies.

## Global Constraints

- `tengiz cleanup` calls the `docker` CLI through `os/exec` — no Docker SDK
- Containers labeled `tengiz-app=<name>` are NEVER removed (label-based protection per spec)
- Images with repository prefix `tengiz-apps/` are NEVER removed by cleanup; per-app image retention is already bounded by `runtime.KeepLastNImages` at deploy time
- Default behavior (no category flags) cleans containers, images, volumes, and networks
- `--dry-run` lists what would be removed without deleting anything
- The command is environment-independent (operates on the whole Docker daemon); no `--env` flag
- No new external dependencies; `go.mod` is unchanged
- All existing tests must continue to pass without modification except the two `Manager` mock/stub files noted in the File Structure
- Follow AGENTS.md: work on branch `feat/cleanup`, add/update tests per change, tests must pass before each commit, update README for CLI changes

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `CleanupOptions`/`CleanupResult` types, `imageRef`/`containerInfo` helpers, pure selection functions, `dockerRuntime.Cleanup` + `prunable*` exec helpers. Extends existing cleanup file that already holds `RemoveImage`/`KeepLastNImages`. |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager.Cleanup` |
| `internal/runtime/cleanup_test.go` | Pure-function tests (labels, container selection, image selection, image ref) |
| `internal/runtime/runtime_test.go` | Stub `Cleanup` smoke test |
| `internal/cli/root.go` | `cleanupCmd` command, category + `--dry-run` flags, `cleanupScope()` helper, registration in `init()` |
| `internal/cli/cleanup_test.go` | Command registration, flag presence, scope/flag-default logic |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy` (required for compilation once the interface grows) |
| `README.md` | Document `tengiz cleanup` + add to the Commands table |

---

### Task 1: Cleanup types and pure selection logic

**Files:**
- Modify: `internal/runtime/cleanup.go` — add types and pure functions
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `type CleanupOptions struct { Containers, Images, Volumes, Networks, DryRun bool }`; `type CleanupResult struct { Containers, Images, Volumes, Networks []string }`; `const tengizAppPrefix = "tengiz-apps/"`; `type containerInfo struct { ID, Name, State string; Labels map[string]string }`; `parseLabels(s string) map[string]string`; `type imageRef struct { ID, Repository, Tag string }`; `imageRef.String() string`; `imageRef.Ref() string`; `selectContainersForRemoval([]containerInfo) []string`; `selectImagesForRemoval([]imageRef, map[string]bool) []imageRef`

- [ ] **Step 1: Create the feature branch**

Run: `git checkout -b feat/cleanup`

Expected: on branch `feat/cleanup`

- [ ] **Step 2: Write the failing tests**

Create `internal/runtime/cleanup_test.go`:

```go
package runtime

import "testing"

func TestParseLabels(t *testing.T) {
	labels := parseLabels("tengiz-app=myapp,tengiz-env=production,foo=bar")
	if labels["tengiz-app"] != "myapp" {
		t.Errorf("tengiz-app = %q, want myapp", labels["tengiz-app"])
	}
	if labels["tengiz-env"] != "production" {
		t.Errorf("tengiz-env = %q, want production", labels["tengiz-env"])
	}
	if labels["foo"] != "bar" {
		t.Errorf("foo = %q, want bar", labels["foo"])
	}
}

func TestParseLabelsEmpty(t *testing.T) {
	labels := parseLabels("")
	if len(labels) != 0 {
		t.Errorf("expected empty map, got %v", labels)
	}
}

func TestSelectContainersForRemoval(t *testing.T) {
	tengiz := containerInfo{ID: "c1", State: "exited", Labels: map[string]string{"tengiz-app": "myapp"}}
	exited := containerInfo{ID: "c2", State: "exited"}
	created := containerInfo{ID: "c3", State: "created"}
	running := containerInfo{ID: "c4", State: "running"}
	dead := containerInfo{ID: "c5", State: "dead"}

	got := selectContainersForRemoval([]containerInfo{tengiz, exited, created, running, dead})
	want := map[string]bool{"c2": true, "c3": true, "c5": true}
	if len(got) != 3 {
		t.Fatalf("got %d containers, want 3: %v", len(got), got)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected container %q selected for removal", id)
		}
	}
}

func TestSelectImagesForRemoval(t *testing.T) {
	images := []imageRef{
		{ID: "i1", Repository: "<none>", Tag: "<none>"},
		{ID: "i2", Repository: "nginx", Tag: "latest"},
		{ID: "i3", Repository: "nginx", Tag: "1.25"},
		{ID: "i4", Repository: "tengiz-apps/myapp", Tag: "latest"},
		{ID: "i5", Repository: "tengiz-apps/myapp", Tag: "1712345678"},
	}
	inUse := map[string]bool{"nginx:1.25": true}

	got := selectImagesForRemoval(images, inUse)
	want := map[string]string{"i1": "<none>:<none>", "i2": "nginx:latest"}
	if len(got) != 2 {
		t.Fatalf("got %d images, want 2: %v", len(got), got)
	}
	for _, img := range got {
		if want[img.ID] != img.String() {
			t.Errorf("image %s (%s) should not be removed", img.ID, img.String())
		}
	}
}

func TestImageRefRef(t *testing.T) {
	dangling := imageRef{ID: "abc123", Repository: "<none>", Tag: "<none>"}
	if got := dangling.Ref(); got != "abc123" {
		t.Errorf("dangling Ref() = %q, want abc123", got)
	}
	tagged := imageRef{ID: "def456", Repository: "nginx", Tag: "latest"}
	if got := tagged.Ref(); got != "nginx:latest" {
		t.Errorf("tagged Ref() = %q, want nginx:latest", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestParseLabels|TestSelectContainersForRemoval|TestSelectImagesForRemoval|TestImageRefRef" -v -count=1`

Expected: FAIL with `undefined: parseLabels`, `undefined: containerInfo`, `undefined: selectContainersForRemoval`, `undefined: imageRef`, `undefined: selectImagesForRemoval`

- [ ] **Step 4: Write the implementation**

Add to `internal/runtime/cleanup.go` (append after the existing `KeepLastNImages` function):

```go
const tengizAppPrefix = "tengiz-apps/"

type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	DryRun     bool
}

type CleanupResult struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
}

type containerInfo struct {
	ID     string
	Name   string
	State  string
	Labels map[string]string
}

func parseLabels(s string) map[string]string {
	labels := make(map[string]string)
	if s == "" {
		return labels
	}
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			labels[kv[0]] = kv[1]
		}
	}
	return labels
}

type imageRef struct {
	ID         string
	Repository string
	Tag        string
}

func (i imageRef) String() string {
	return i.Repository + ":" + i.Tag
}

func (i imageRef) Ref() string {
	if i.Repository == "<none>" || i.Repository == "" {
		return i.ID
	}
	return i.String()
}

func selectContainersForRemoval(containers []containerInfo) []string {
	var out []string
	for _, c := range containers {
		if c.Labels["tengiz-app"] != "" {
			continue
		}
		switch c.State {
		case "exited", "created", "dead":
			out = append(out, c.ID)
		}
	}
	return out
}

func selectImagesForRemoval(images []imageRef, inUse map[string]bool) []imageRef {
	var out []imageRef
	for _, img := range images {
		if img.Repository == "<none>" {
			out = append(out, img)
			continue
		}
		if strings.HasPrefix(img.Repository, tengizAppPrefix) {
			continue
		}
		if !inUse[img.String()] {
			out = append(out, img)
		}
	}
	return out
}
```

Note: `encoding/json` is not needed yet — it arrives in Task 2. The file already imports `sort` (used by `KeepLastNImages`) and `strings` (used above); no import changes in this task.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestParseLabels|TestSelectContainersForRemoval|TestSelectImagesForRemoval|TestImageRefRef" -v -count=1`

Expected: PASS (4 tests)

- [ ] **Step 6: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add cleanup selection logic for unused docker resources"
```

---

### Task 2: `dockerRuntime.Cleanup`, Manager interface, stub and mock

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `prunableContainers`, `prunableImages`, `danglingVolumes`, `danglingNetworks`, `Cleanup`
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to the `Manager` interface; add `stubManager.Cleanup`
- Modify: `internal/cli/root_test.go:76-100` — add `Cleanup` to `mockRTForDeploy`
- Test: `internal/runtime/runtime_test.go` — add `TestStubCleanup`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, `imageRef`, `parseLabels`, `selectContainersForRemoval`, `selectImagesForRemoval` from Task 1; existing `RemoveImage` method
- Produces: `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)` on the `dockerRuntime` and `stubManager`; unexported `(r *dockerRuntime) prunableContainers(ctx context.Context) ([]string, error)`, `prunableImages(ctx context.Context) ([]imageRef, error)`, `danglingVolumes(ctx context.Context) ([]string, error)`, `danglingNetworks(ctx context.Context) ([]string, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/runtime_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil {
		t.Fatal("Cleanup() returned nil result")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: FAIL with `stubManager.Cleanup undefined`

- [ ] **Step 3: Add `Cleanup` to the `Manager` interface**

In `internal/runtime/runtime.go`, inside the `Manager` interface (after the `Run` line at the end of the interface):

```go
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
```

- [ ] **Step 4: Add the stub implementation**

In `internal/runtime/runtime.go`, after the `stubManager.Run` method:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	return &CleanupResult{}, nil
}
```

- [ ] **Step 5: Update the test mock in `internal/cli/root_test.go`**

Add to `mockRTForDeploy` (after its `Run` method, line ~100):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (*runtime.CleanupResult, error) {
	return &runtime.CleanupResult{}, nil
}
```

This is required because `TestMockRTForDeployImplementsManager` asserts `var m runtime.Manager = &mockRTForDeploy{}`; without the method the `internal/cli` package would not compile.

- [ ] **Step 6: Write the `dockerRuntime` implementation**

Append to `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) prunableContainers(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{json .}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	var containers []containerInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var e dockerPS
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		containers = append(containers, containerInfo{
			ID:     e.ID,
			Name:   e.Name,
			State:  e.State,
			Labels: parseLabels(e.Labels),
		})
	}
	return selectContainersForRemoval(containers), nil
}

func (r *dockerRuntime) prunableImages(ctx context.Context) ([]imageRef, error) {
	imgCmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{.ID}}\t{{.Repository}}\t{{.Tag}}")
	out, err := imgCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	var images []imageRef
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		images = append(images, imageRef{ID: parts[0], Repository: parts[1], Tag: parts[2]})
	}

	useCmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{.Image}}")
	useOut, err := useCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps (usage): %w\n%s", err, string(useOut))
	}
	inUse := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(useOut)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			inUse[line] = true
		}
	}

	return selectImagesForRemoval(images, inUse), nil
}

func (r *dockerRuntime) danglingVolumes(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-f", "dangling=true", "--format", "{{.Name}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

func (r *dockerRuntime) danglingNetworks(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "ls", "-f", "dangling=true", "--format", "{{.ID}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error) {
	result := &CleanupResult{}

	if opts.Containers {
		removable, err := r.prunableContainers(ctx)
		if err != nil {
			return nil, err
		}
		result.Containers = removable
		if !opts.DryRun && len(removable) > 0 {
			args := append([]string{"rm", "-f"}, removable...)
			cmd := exec.CommandContext(ctx, "docker", args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("docker rm: %w\n%s", err, string(out))
			}
		}
	}

	if opts.Images {
		removable, err := r.prunableImages(ctx)
		if err != nil {
			return nil, err
		}
		for _, img := range removable {
			result.Images = append(result.Images, img.Ref())
		}
		if !opts.DryRun {
			for _, img := range removable {
				if err := r.RemoveImage(ctx, img.Ref()); err != nil {
					return nil, err
				}
			}
		}
	}

	if opts.Volumes {
		removable, err := r.danglingVolumes(ctx)
		if err != nil {
			return nil, err
		}
		result.Volumes = removable
		if !opts.DryRun && len(removable) > 0 {
			args := append([]string{"volume", "rm"}, removable...)
			cmd := exec.CommandContext(ctx, "docker", args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("docker volume rm: %w\n%s", err, string(out))
			}
		}
	}

	if opts.Networks {
		removable, err := r.danglingNetworks(ctx)
		if err != nil {
			return nil, err
		}
		result.Networks = removable
		if !opts.DryRun && len(removable) > 0 {
			args := append([]string{"network", "rm"}, removable...)
			cmd := exec.CommandContext(ctx, "docker", args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("docker network rm: %w\n%s", err, string(out))
			}
		}
	}

	return result, nil
}
```

Add `"encoding/json"` to the import block of `internal/runtime/cleanup.go` so it reads:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS including `TestStubCleanup`

- [ ] **Step 8: Verify the `internal/cli` package compiles with the new interface method**

Run: `go build ./internal/cli/`

Expected: Build succeeds (proves `mockRTForDeploy` now satisfies `Manager`)

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/runtime_test.go internal/cli/root_test.go
git commit -m "feat: add Cleanup to runtime.Manager and docker exec implementation"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupScope()` helper (near `getEnv`, line ~97), add `cleanupCmd` (after `rollbackCmd`, line ~1017), register command + flags in `init()`
- Create: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult`, `Manager.Cleanup` from Tasks 1-2
- Produces: `cleanupScope(cmd *cobra.Command) runtime.CleanupOptions` helper; `tengiz cleanup` command with `--containers`, `--images`, `--volumes`, `--networks`, `--dry-run` flags; no category flags = all categories

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import "testing"

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
	for _, flag := range []string{"containers", "images", "volumes", "networks", "dry-run"} {
		if f := cleanupCmd.Flags().Lookup(flag); f == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupScopeAllByDefault(t *testing.T) {
	cleanupCmd.ParseFlags([]string{})
	opts := cleanupScope(cleanupCmd)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks {
		t.Errorf("expected all categories by default, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("expected DryRun=false by default")
	}
}

func TestCleanupScopeImagesOnly(t *testing.T) {
	cleanupCmd.ParseFlags([]string{"--images"})
	opts := cleanupScope(cleanupCmd)
	if !opts.Images || opts.Containers || opts.Volumes || opts.Networks {
		t.Errorf("expected only Images, got %+v", opts)
	}
}

func TestCleanupScopeDryRun(t *testing.T) {
	cleanupCmd.ParseFlags([]string{"--dry-run"})
	opts := cleanupScope(cleanupCmd)
	if !opts.DryRun {
		t.Error("expected DryRun=true")
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks {
		t.Errorf("expected all categories with --dry-run, got %+v", opts)
	}
}

func TestCleanupScopeVolumeNetwork(t *testing.T) {
	cleanupCmd.ParseFlags([]string{"--volumes", "--networks"})
	opts := cleanupScope(cleanupCmd)
	if !opts.Volumes || !opts.Networks || opts.Containers || opts.Images {
		t.Errorf("expected only Volumes+Networks, got %+v", opts)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: cleanupScope`

- [ ] **Step 3: Add the `cleanupScope` helper and `cleanupCmd` command**

Add the helper to `internal/cli/root.go` right after the `getEnv` function (line ~103):

```go
func cleanupScope(cmd *cobra.Command) runtime.CleanupOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if !containers && !images && !volumes && !networks {
		containers, images, volumes, networks = true, true, true, true
	}
	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		DryRun:     dryRun,
	}
}
```

Add the command to `internal/cli/root.go` right after the `rollbackCmd` block (line ~1017):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, volumes, networks)",
	Long: `Remove unused Docker resources to reclaim disk space.

By default all resource categories are cleaned. Use the category flags to
limit the operation. Tengiz-managed containers (labeled tengiz-app) and
Tengiz-built images (tengiz-apps/*) are always protected. Use --dry-run to
preview what would be removed without removing anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cleanupScope(cmd)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		verb := "removed"
		if opts.DryRun {
			verb = "would remove"
		}

		fmt.Printf("[tengiz] containers %s: %d\n", verb, len(result.Containers))
		for _, id := range result.Containers {
			fmt.Printf("  %s\n", id)
		}
		fmt.Printf("[tengiz] images %s: %d\n", verb, len(result.Images))
		for _, tag := range result.Images {
			fmt.Printf("  %s\n", tag)
		}
		fmt.Printf("[tengiz] volumes %s: %d\n", verb, len(result.Volumes))
		for _, name := range result.Volumes {
			fmt.Printf("  %s\n", name)
		}
		fmt.Printf("[tengiz] networks %s: %d\n", verb, len(result.Networks))
		for _, id := range result.Networks {
			fmt.Printf("  %s\n", id)
		}
		return nil
	},
}
```

- [ ] **Step 4: Register the command and flags in `init()`**

Add inside `internal/cli/root.go` `init()` (after the `webhookCmd.Flags().String("config", ...)` line, ~88):

```go
	cleanupCmd.Flags().Bool("containers", false, "remove stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "remove unused non-Tengiz images")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without removing anything")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS (5 tests)

- [ ] **Step 6: Build and run all tests**

Run: `go build -o tengiz . && go test ./... -v -count=1`

Expected: Build succeeds, all tests pass (proxy TCP-timeout tests and `idle` time-sensitive tests may be slow but must pass)

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: README documentation and final verification

**Files:**
- Modify: `README.md` — add a `### tengiz cleanup` section after `### tengiz rollback` (line ~238), and add a row to the Commands table (line ~570)

- [ ] **Step 1: Document the command in README**

Insert after the `### tengiz rollback <app>` section (after its argument table, before `### tengiz domain` at line ~238):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space.

By default all resource categories are cleaned. Use the category flags to limit the operation. Tengiz-managed containers (labeled `tengiz-app=<name>`) and Tengiz-built images (`tengiz-apps/*`) are always protected — cleanup never removes them. Per-app image retention is handled automatically by the deploy pipeline (`KeepLastNImages`, 5 per app). Use `--dry-run` to preview what would be removed without removing anything.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped non-Tengiz containers |
| `--images` | Remove dangling images and unused non-Tengiz images |
| `--volumes` | Remove volumes not referenced by any container |
| `--networks` | Remove networks not referenced by any container |
| `--dry-run` | List what would be removed without deleting anything |

Example:

```
tengiz cleanup --dry-run
tengiz cleanup --images --volumes
tengiz cleanup
```
```

- [ ] **Step 2: Add the command to the Commands table**

In the "### Commands" table (line ~570), add after the `tengiz init --git-repo URL` row:

```markdown
| `tengiz cleanup [--containers] [--images] [--volumes] [--networks] [--dry-run]` | Remove unused Docker resources to reclaim disk space |
```

- [ ] **Step 3: Run the full verification suite**

Run: `go build -o tengiz .`

Expected: Build succeeds

Run: `go vet ./...`

Expected: No issues

Run: `go test ./... -v -count=1`

Expected: All tests pass

- [ ] **Step 4: Self-review against spec**

Check against the spec (`docs/FUTURES_FEATURES.md`, "Docker Housekeeping"):
- `tengiz cleanup` command ✅ (Task 3)
- Label-based protection of Tengiz-managed containers ✅ (Task 1 — `selectContainersForRemoval` skips `tengiz-app` labeled containers; Task 2 — `prunableContainers` reads labels via JSON)
- Cleanup of unused volumes, networks, containers, images ✅ (Task 2 — `prunable*`/`dangling*` helpers)
- Dry-run for safe preview ✅ (Task 2 — `DryRun` skips delete calls; Task 3 — `--dry-run` flag + "would remove" output)
- No new external dependencies ✅ (`os/exec` only)
- AGENTS.md README rule ✅ (Tasks 4 Steps 1-2)

- [ ] **Step 5: Placeholder scan**

Search the plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task". All steps contain complete code; none of the forbidden patterns appear.

- [ ] **Step 6: Type consistency check**

- `CleanupOptions{Containers, Images, Volumes, Networks, DryRun bool}` — identical in Task 1 (definition), Task 2 (stub + impl signature), Task 3 (`cleanupScope` construction)
- `CleanupResult{Containers, Images, Volumes, Networks []string}` — identical in Task 1 (definition), Task 2 (return type), Task 3 (field access `result.Containers`, `result.Images`, `result.Volumes`, `result.Networks`)
- `imageRef.Ref()` returns `ID` for dangling and `Repository:Tag` otherwise — consistent between Task 1 test `TestImageRefRef` and Task 2 `Cleanup` delete calls
- `dockerPS` struct (existing in `docker.go`) fields `ID`, `Name`, `State`, `Labels` match the JSON keys produced by `docker ps -a --format "{{json .}}"` — same struct the existing `List()` method already parses
- `tengizAppPrefix = "tengiz-apps/"` used in Task 1 `selectImagesForRemoval` matches the reference filter `tengiz-apps/%s:*` already used by `KeepLastNImages`

- [ ] **Step 7: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```
