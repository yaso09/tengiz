# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that removes unused Docker resources (exited containers, dangling images, unused volumes/networks, build cache) while always protecting Tengiz-managed containers via the `tengiz-app` label, so admins can reclaim disk on single-server deployments.

**Architecture:** Add a `Cleanup(ctx, CleanupOptions) (CleanupResult, error)` method to the existing `runtime.Manager` interface. The docker runtime implementation lists candidate objects (`docker ps -a`, `docker images --filter dangling=true`, `docker volume ls`, `docker network ls`) and removes them individually, so that a report of *exactly which objects were removed* can be returned with no brittle output-parsing of `docker system prune`. Label-based protection is built in: all candidate-listing commands carry `--filter label!=tengiz-app`. The `--images` flow first runs the existing `KeepLastNImages(app, keep)` for every app in the env store so rollback images survive. A new `internal/cli/cleanup.go` wires flags → `CleanupOptions` and prints a human-readable report; the image-retention preamble and the report writer are pure functions, so they are testable with stubs.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface, existing `config.Store` for listing apps, docker CLI via `os/exec` (no SDK). No new external dependencies.

## Global Constraints

- All candidate-listing docker commands MUST include `--filter "label!=tengiz-app"` so Tengiz-managed containers/volumes/networks are never candidates (scale-to-zero stopped containers are protected — feature requirement)
- Default `--keep N` is 5 image versions per app (matches existing deploy-time `KeepLastNImages(..., 5)`)
- Image tags/references: `tengiz-apps/<appName>:<env>-<deploymentID>` for all envs; repository part is always `tengiz-apps/<appName>`
- No new external dependencies (only stdlib + existing cobra/viper)
- `Cleanup` must be added to the `Manager` interface AND to every existing mock/stub implementation or the package won't compile
- `tengiz cleanup` with no category flag runs all categories (`--containers --images --volumes --networks --cache`)
- `--dry-run` lists candidates but removes nothing; the printed report says "would remove"
- Category flags may be combined; `--build-cache` accepts `--cache` as an alias (long flag `--cache`)
- Existing tests must continue to pass unchanged
- Feature docs `docs/FUTURES_FEATURES.md` marks #6 as implemented

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupResult`, `Cleanup` to `Manager` interface; add stub method |
| `internal/runtime/housekeeping.go` (create) | Docker implementation of `Cleanup` + pure args builders + object list/remove helpers |
| `internal/runtime/cleanup_test.go` | Stub test for `Cleanup`; pure arg-builder tests |
| `internal/cli/cleanup.go` (CREATE) | `cleanupCmd` with flags; `cleanupFlags()` pure mapper; `enforceImageRetention()`; `printCleanupReport()` |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()`, `rootCmd.AddCommand(cleanupCmd)` |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy`; CLI registration/flag/report tests |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in CLI Reference |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

---

### Task 0: Create feature branch

**Files:** none (git only)

**Interfaces:**
- Produces: working branch `feat/docker-cleanup`

- [ ] **Step 1: Create feature branch**

```bash
git checkout -b feat/docker-cleanup
```

- [ ] **Step 2: Verify emptiness**

Run: `git status`
Expected: on branch `feat/docker-cleanup`, clean working tree.

- [ ] **Step 3: Commit nothing yet** (steps that modify code follow)

---

### Task 1: Add `Cleanup` to the `runtime.Manager` interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go` — add `CleanupOptions`, `CleanupResult` types; add `Cleanup` to `Manager` interface; add stub method
- Test: `internal/runtime/cleanup_test.go` — add `TestStubCleanup`

**Interfaces:**
- Consumes: nothing
- Produces: `runtime.CleanupOptions{Containers, Images, Volumes, Networks, BuildCache, DryRun bool}`, `runtime.CleanupResult{Containers, Images, Volumes, Networks []string; CacheBytes string}`, `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`

- [ ] **Step 1: Write failing test**

```go
// internal/runtime/cleanup_test.go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(res.Containers) != 0 || len(res.Images) != 0 || len(res.Volumes) != 0 || len(res.Networks) != 0 {
		t.Errorf("stub Cleanup() should report nothing removed, got %+v", res)
	}
	if res.CacheBytes != "" {
		t.Errorf("stub Cleanup() CacheBytes should be empty, got %q", res.CacheBytes)
	}
}
```

- [ ] **Step 2: Run test to verify it fails (compile error)**

Run: `go test ./internal/runtime/... -run TestStubCleanup -count=1`
Expected: FAIL — `stubManager does not implement Manager` / `undefined: CleanupOptions` / `undefined: CleanupResult`.

- [ ] **Step 3: Add types and interface method in `internal/runtime/runtime.go`**

Add after the existing `RunOptions` declaration (near line 29):

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	DryRun     bool
}

type CleanupResult struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
	CacheBytes string
}
```

Add to the `Manager` interface (after line 48, the `Run` method):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add the stub method after the existing stub `Run` (after line 121):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `internal/cli` no — run: `go test ./internal/runtime/... -run TestStubCleanup -count=1`
Expected: PASS (stub returns empty result).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface plus stub"
```

---

### Task 2: Docker implementation of `Cleanup`

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/cleanup_test.go` — add `TestHousekeepingArgBuilders`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult` from Task 1
- Produces: `dockerRuntime.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`; pure builders `exitedContainersArgs()`, `danglingImagesArgs()`, `unusedVolumesArgs()`, `unusedNetworksArgs()`, `buildCachePruneArgs()`; helpers `(r *dockerRuntime) dockerList(ctx, args) ([]string, error)` and `(r *dockerRuntime) pruneListed(ctx, listArgs []string, rmArgs []string, dryRun bool) ([]string, error)`

- [ ] **Step 1: Write failing arg-builder tests**

```go
// internal/runtime/cleanup_test.go
func TestExitedContainersArgs(t *testing.T) {
	args := exitedContainersArgs()
	want := []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", "label!=tengiz-app",
		"--format", "{{.ID}}",
	}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestDanglingImagesArgs(t *testing.T) {
	args := danglingImagesArgs()
	want := []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestUnusedVolumesArgs(t *testing.T) {
	args := unusedVolumesArgs()
	want := []string{"volume", "ls", "--filter", "label!=tengiz-app", "--format", "{{.Name}}"}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestUnusedNetworkArgs(t *testing.T) {
	args := unusedNetworksArgs()
	want := []string{"network", "ls", "--filter", "label!=tengiz-app", "--format", "{{.ID}}"}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestbuildCachePruneArgs(t *testing.T) {
	args := buildCachePruneArgs()
	want := []string{"builder", "prune", "-af"}
	if len(args) != len(want) {
		t.Fatalf("got %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestExitedContainersArgs|TestDanglingImagesArgs|TestUnusedVolumesArgs|TestUnusedNetworkArgs|TestbuildCachePruneArgs" -count=1`
Expected: FAIL — `undefined: exitedContainersArgs` (and the other four).

- [ ] **Step 3: Implement `internal/runtime/housekeeping.go`**

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const nonTengizLabelFilter = "label!=tengiz-app"

func exitedContainersArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", nonTengizLabelFilter,
		"--format", "{{.ID}}",
	}
}

func danglingImagesArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}"}
}

func unusedVolumesArgs() []string {
	return []string{"volume", "ls", "--filter", nonTengizLabelFilter, "--format", "{{.Name}}"}
}

func unusedNetworksArgs() []string {
	return []string{"network", "ls", "--filter", nonTengizLabelFilter, "--format", "{{.ID}}"}
}

func buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-af"}
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	res := CleanupResult{}
	var err error

	if opts.Containers {
		res.Containers, err = r.pruneListed(ctx, exitedContainersArgs(), "rm", opts.DryRun)
		if err != nil {
			return res, fmt.Errorf("containers cleanup: %w", err)
		}
	}
	if opts.Images {
		res.Images, err = r.pruneListed(ctx, danglingImagesArgs(), "rmi", opts.DryRun)
		if err != nil {
			return res, fmt.Errorf("images cleanup: %w", err)
		}
	}
	if opts.Volumes {
		res.Volumes, err = r.pruneListed(ctx, unusedVolumesArgs(), "volume rm", opts.DryRun)
		if err != nil {
			return res, fmt.Errorf("volumes cleanup: %w", err)
		}
	}
	if opts.Networks {
		res.Networks, err = r.pruneListed(ctx, unusedNetworksArgs(), "network rm", opts.DryRun)
		if err != nil {
			return res, fmt.Errorf("networks cleanup: %w", err)
		}
	}
	if opts.BuildCache && !opts.DryRun {
		out, cacheErr := exec.CommandContext(ctx, "docker", buildCachePruneArgs()...).CombinedOutput()
		if cacheErr != nil {
			return res, fmt.Errorf("build cache cleanup: %w\n%s", cacheErr, strings.TrimSpace(string(out)))
		}
		res.CacheBytes = strings.TrimSpace(string(out))
	}
	return res, nil
}

func (r *dockerRuntime) dockerList(ctx context.Context, args []string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

// pruneListed lists candidates with listArgs and removes them one at a time with rm (split on spaces) when not running dry.
func (r *dockerRuntime) pruneListed(ctx context.Context, listArgs, rm string, dryRun bool) ([]string, error) {
	ids, err := r.dockerList(ctx, listArgs)
	if err != nil {
		return nil, err
	}
	if dryRun {
		return ids, nil
	}
	rmParts := strings.Fields(rm)
	var removed []string
	for _, id := range ids {
		args := append([]string{}, rmParts...)
		args = append(args, id)
		cmd := exec.CommandContext(ctx, "docker", args...)
		if out, rmErr := cmd.CombinedOutput(); rmErr != nil {
			_ = out // individual failures are non-fatal for cleanup
			continue
		}
		removed = append(removed, id)
	}
	return removed, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestExitedContainersArgs|TestDanglingImagesArgs|TestUnusedVolumesArgs|TestUnusedNetworkArgs|TestbuildCachePruneArgs" -count=1`
Expected: PASS.

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -count=1`
Expected: All PASS (including the earlier `TestStubCleanup`).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement Docker Housekeeping Cleanup via dockerd exec"
```

---

### Task 3: Update existing mock runtime implementations

**Files:**
- Modify: `internal/proxy/proxy_test.go` — add `Cleanup` to `mockRuntime`
- Modify: `internal/idle/idle_test.go` — add `Cleanup` to `mockRuntime`
- Modify: `internal/cli/root_test.go` — add `Cleanup` to `mockRTForDeploy`

**Interfaces:**
- Consumes: Task 1 types + interface method
- Produces: All packages compile again after the interface grew

- [ ] **Step 1: Add `Cleanup` to proxy mock**

In `internal/proxy/proxy_test.go`, after the existing `Run` method (line ~35):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 2: Add `Cleanup` to idle mock**

In `internal/idle/idle_test.go`, after the existing `Run` method (line ~34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 3: Add `Cleanup` to cli mock**

In `internal/cli/root_test.go`, after the existing `Run` method (line ~100):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 4: Verify everything compiles**

Run: `go build ./...`
Expected: builds without error.

- [ ] **Step 5: Run affected package tests**

Run: `go test ./internal/proxy/... ./internal/idle/... ./internal/cli/... -count=1`
Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "test: implement Cleanup on existing cli/proxy/idle runtime mocks"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go` — register `cleanupCmd`
- Test: `internal/cli/root_test.go` — command/flag registration tests, `cleanupFlags` and `printCleanupReport` unit tests

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.Manager.Cleanup`, `config.NewStoreWithEnv`
- Produces: `cleanupCmd` (cobra.Command), `func cleanupFlags(cmd *cobra.Command) runtime.CleanupOptions`, `func enforceImageRetention(cmd *cobra.Command, rt runtime.Manager, env string) error`, `func printCleanupReport(w io.Writer, opts runtime.CleanupOptions, res runtime.CleanupResult)`

- [ ] **Step 1: Write failing tests**

```go
// internal/cli/root_test.go
func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupFlagsRegistered(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "cache", "dry-run", "keep"} {
		fl := cleanupCmd.Flags().Lookup(flag)
		if fl == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
	if cleanupCmd.Flags().ShorthandLookup("c") != nil {
		t.Error("--cache should not have shorthand -c")
	}
}

func TestCleanupFlagsDefaultsToAll(t *testing.T) {
	cleanupCmd.ParseFlags(nil)
	opts := cleanupFlags(cleanupCmd)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("no flag given should mean all categories, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("DryRun should default false")
	}
}

func TestCleanupFlagsCategoryOnly(t *testing.T) {
	cleanupCmd.SetArgs([]string{"--networks"})
	cleanupCmd.ParseFlags([]string{"--networks"})
	opts := cleanupFlags(cleanupCmd)
	if opts.Networks != true {
		t.Error("--networks flag not picked up")
	}
	if opts.Containers || opts.Images || opts.Volumes || opts.BuildCache {
		t.Error("only --networks requested, other categories should be off")
	}
}

func TestCleanupFlagsDryRun(t *testing.T) {
	cleanupCmd.ParseFlags([]string{"--dry-run"})
	opts := cleanupFlags(cleanupCmd)
	if !opts.DryRun {
		t.Error("--dry-run should be true")
	}
}

func TestPrintCleanupReport(t *testing.T) {
	var buf bytes.Buffer
	opts := runtime.CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		BuildCache: true,
		DryRun:     true,
	}
	res := runtime.CleanupResult{
		Containers: []string{"c1"},
		Images:     []string{"i1", "i2"},
		Networks:   []string{},
		CacheBytes: "1.2 GB",
	}
	printCleanupReport(&buf, opts, res)
	out := buf.String()
	for _, want := range []string{
		"would remove",
		"containers: 1",
		"images: 2",
		"volume: 1",
		"networks: 0",
		"build cache: 1.2 GB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q in:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -count=1`
Expected: FAIL — `undefined: cleanupCmd`, `undefined: cleanupFlags`, `undefined: printCleanupReport`.

- [ ] **Step 3: Implement `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, volumes, networks, build cache)",
	Long: `Removes unused Docker resources on this host to reclaim disk space.

With no category flags, every category runs:
  --containers   exited containers not managed by Tengiz
  --images       dangling/unused images (keeps the last app image for rollback)
  --volumes      unused volumes
  --networks     unused networks
  --cache        Docker build cache

Tengiz-managed containers are always protected via the 'tengiz-app' label,
so stopped apps scheduled for scale-to-zero cold-start are never removed.

Use --dry-run to see exactly what would be removed without removing it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		opts := cleanupFlags(cmd)

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if opts.Images {
			if err := enforceImageRetention(cmd, rt, env); err != nil {
				return err
			}
		}

		res, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		printCleanupReport(os.Stdout, opts, res)
		return nil
	},
}

func cleanupFlags(cmd *cobra.Command) runtime.CleanupOptions {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	cache, _ := cmd.Flags().GetBool("cache")
	dry, _ := cmd.Flags().GetBool("dry-run")

	if !containers && !images && !volumes && !networks && !cache {
		containers, images, volumes, networks, cache = true, true, true, true, true
	}
	return runtime.CleanupOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: cache,
		DryRun:     dry,
	}
}

func enforceImageRetention(cmd *cobra.Command, rt runtime.Manager, env string) error {
	keep, _ := cmd.Flags().GetInt("keep")
	if keep <= 0 {
		keep = 5
	}
	store := config.NewStoreWithEnv(dataDir, env)
	apps, err := store.ListApps()
	if err != nil {
		return fmt.Errorf("list apps: %w", err)
	}
	for _, app := range apps {
		if err := rt.KeepLastNImages(context.Background(), app.Name, keep); err != nil {
			log.Printf("[cleanup] keeping last %d images for %s: %v", keep, app.Name, err)
		}
	}
	return nil
}

func printCleanupReport(w io.Writer, opts runtime.CleanupOptions, res runtime.CleanupResult) {
	mode := "removed"
	if opts.DryRun {
		mode = "would remove"
	}
	fmt.Fprintf(w, "[tengiz] cleanup Report:\n")
	fmt.Fprintf(w, "  mode: %s\n", mode)
	if opts.Containers {
		fmt.Fprintf(w, "  containers: %d\n", len(res.Containers))
	}
	if opts.Images {
		fmt.Fprintf(w, "  images: %d\n", len(res.Images))
	}
	if opts.Volumes {
		fmt.Fprintf(w, "  volumes: %d\n", len(res.Volumes))
	}
	if opts.Networks {
		fmt.Fprintf(w, "  networks: %d\n", len(res.Networks))
	}
	if opts.BuildCache {
		fmt.Fprintf(w, "  build cache: %s\n", orDisplay(res.CacheBytes))
	}
}

func orDisplay(s string) string {
	if s == "" {
		return "n/a"
	}
	return s
}
```

- [ ] **Step 4: Register the command and its flags in `internal/cli/root.go`**

Add to `init()` (near the other `rootCmd.AddCommand(...)` calls):

```go
	cleanupCmd.Flags().Bool("containers", false, "clean exited containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "clean dangling/unused images (keeps rollback images)")
	cleanupCmd.Flags().Bool("volumes", false, "clean unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "clean unused networks")
	cleanupCmd.Flags().Bool("cache", false, "clean Docker build cache")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be cleaned without removing anything")
	cleanupCmd.Flags().Int("keep", 5, "keep last N images per app when cleaning images")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -count=1`
Expected: PASS.

- [ ] **Step 6: Run full build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 5: Docs + self-review

**Files:**
- Modify: `README.md` — CLI Reference: add `tengiz cleanup`
- Modify: `docs/FUTURES_FEATURES.md` — mark #6 implemented
- Test: none new

**Interfaces:**
- Consumes: everything from Tasks 1-4
- Produces: documented, ready-to-merge feature

- [ ] **Step 1: Document in README CLI Reference**

In `README.md` CLI Reference section, in the same code fence listing commands that currently ends with `tengiz rollback <app>`, add:

```bash
tengiz cleanup          # remove unused Docker resources (containers, images, volumes, networks, build cache)
```

- [ ] **Step 2: Mark feature in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`, priority table row #6 change status:

From:
`| 6 | **Docker Housekeeping** ⬜ | ...`
To:
`| 6 | **Docker Housekeeping** ✅ | ...`

Also update the status of the closing "✅ Implemented (2026-08-09)" note in the "Implemented Features" section below the P0 table: add a row

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-09) |
```

- [ ] **Step 3: Full test suite**

Run: `go test ./... -count=1`
Expected: ALL PASS.

Run: `go vet ./...`
Expected: no issues.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document teng cleanup and mark Docker Housekeeping implemented"
```

- [ ] **Step 5: Self-review against spec**

Check the FUTURES_FEATURES #6 requirement items:
- `tengiz cleanup` command ✅ (Task 4)
- Label-based protection of Tengiz-managed containers (`label!=tengiz-app`) ✅ (Task 2, all candidate builders)
- Removes unused containers/images/volumes/networks + build cache ✅ (Task 2)
- Scale-down stopped containers preserved ✅ (`status=exited` + label filter)
- Env-aware already supported via persistent `--env` flag ✅

- [ ] **Step 6: Placeholder scan**

Search the plan for `TBD|TODO|implement later|fill in details|appropriate|similar to`. Expected: none.

- [ ] **Step 7: Type consistency check**

- `runtime.CleanupOptions{Containers,Images,Volumes,Networks,BuildCache,DryRun}` — same in Task 1, 2, 4
- `runtime.CleanupResult{Containers,Images,Volumes,Networks []string; CacheBytes string}` — same everywhere
- `Manager.Cleanup(ctx, opts) (CleanupResult, error)` — implemented identically in Task 1 stub and Task 2 docker
- `cleanupFlags(cmd) runtime.CleanupOptions`; `enforceImageRetention(cmd, rt, env) error`; `printCleanupReport(w, opts, res)` — used consistently in Task 4