# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by removing stopped orphan containers, dangling images, unused volumes/networks, and the Docker build cache — while always protecting Tengiz-managed containers and app volumes.

**Architecture:** Extend the `runtime.Manager` interface with a `Clean(ctx, opts) (CleanResult, error)` method implemented on the existing exec-based `dockerRuntime`. `Clean` lists candidates with label-based Docker filters (`label!=tengiz-app` protects scale-to-zero stopped apps), removes them one at a time, and returns a `CleanResult` for reporting. A `--dry-run` flag lists candidates without removing. The CLI wraps this in a `tengiz cleanup` command that also applies the existing per-app image retention (`KeepLastNImages(name, 5)`) via the config store.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager`/`config.Store` packages, `os/exec` (no Docker SDK). Tests use a fake `docker` shell script injected into `PATH`.

## Global Constraints

- Never remove running containers — only `docker ps` states `exited` and `created` are targeted
- Never remove containers labeled `tengiz-app` (scale-to-zero stopped apps are intentional, must be protected)
- Image cleanup removes only *dangling* images (`dangling=true`); tagged `tengiz-apps/*` images are handled by `KeepLastNImages(name, 5)` only
- `--dry-run` must perform zero destructive operations
- `tengiz cleanup` with no category flag behaves as `--all` (the user asked for cleanup)
- Per-app image retention keeps the last 5 images (matches existing deploy-time policy)
- No new external Go dependencies; Docker CLI is invoked via `os/exec`
- `cleanup` respects the existing global `--env` flag
- Existing tests must continue to pass; all four `Manager` mocks (`stubManager`, `mockRTForDeploy`, proxy `mockRuntime`, idle `mockRuntime`) must be updated when the interface grows

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `Clean(ctx, opts) (CleanResult, error)` to `Manager` interface + `stubManager` no-op |
| `internal/runtime/prune.go` | New: `CleanOptions`, `CleanItem`, `CleanResult`, `dockerRuntime.Clean` + docker-command helpers, `parseIDList` |
| `internal/runtime/prune_test.go` | New: fake-docker tests for `Clean` (remove / dry-run / no-op) and `parseIDList` |
| `internal/cli/root_test.go` | Add `Clean` to `mockRTForDeploy` (keeps interface compile) |
| `internal/proxy/proxy_test.go` | Add `Clean` to `mockRuntime` (keeps interface compile) |
| `internal/idle/idle_test.go` | Add `Clean` to `mockRuntime` (keeps interface compile) |
| `internal/cli/root.go` | New `cleanupCmd` + registration in `init()` + flags |
| `internal/cli/cmd_cleanup_test.go` | New: CLI registration test + `--dry-run` output test (fake docker) |
| `README.md` | Add `tengiz cleanup` CLI reference section |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as implemented + move to Implemented table |

---

### Task 1: Runtime `Clean` capability

**Files:**
- Modify: `internal/runtime/runtime.go:36` (add `Clean` to interface) and `internal/runtime/runtime.go:117` (add stub method)
- Create: `internal/runtime/prune.go`
- Create: `internal/runtime/prune_test.go`
- Modify: `internal/cli/root_test.go:99` (add `Clean` to `mockRTForDeploy`)
- Modify: `internal/proxy/proxy_test.go:34` (add `Clean` to `mockRuntime`)
- Modify: `internal/idle/idle_test.go:33` (add `Clean` to `mockRuntime`)

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanOptions{Containers, Images, Volumes, Networks, Cache, DryRun bool}`, `runtime.CleanItem{Kind, ID string}`, `runtime.CleanResult{Items []CleanItem, DryRun bool}`, `Manager.Clean(ctx context.Context, opts CleanOptions) (CleanResult, error)`, `func parseIDList(out []byte) []string`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeDocker(t *testing.T, dir, logPath string) {
	t.Helper()
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$TENGIZ_TEST_DOCKER_LOG"
case "$*" in
  "ps -a -q --filter status=exited --filter status=created --filter label!=tengiz-app")
    printf 'abc123\nxyz789\n'
    ;;
  "images --filter dangling=true -q")
    printf 'img1\n'
    ;;
  "volume ls --filter dangling=true -q")
    printf 'vol1\n'
    ;;
  "network ls --filter dangling=true -q")
    printf 'net1\n'
    ;;
esac
exit 0
`
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("TENGIZ_TEST_DOCKER_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func readDockerCalls(t *testing.T, logPath string) string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	return string(data)
}

func TestParseIDList(t *testing.T) {
	got := parseIDList([]byte("abc123\nxyz789\n"))
	if len(got) != 2 || got[0] != "abc123" || got[1] != "xyz789" {
		t.Fatalf("parseIDList() = %v, want [abc123 xyz789]", got)
	}

	if empty := parseIDList([]byte("")); len(empty) != 0 {
		t.Fatalf("parseIDList(empty) = %v, want []", empty)
	}
}

func TestCleanRemovesResources(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	writeFakeDocker(t, dir, logPath)

	r := &dockerRuntime{}
	result, err := r.Clean(context.Background(), CleanOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		Cache:      true,
	})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if len(result.Items) != 6 {
		t.Fatalf("Clean() Items = %d, want 6: %+v", len(result.Items), result.Items)
	}
	if result.DryRun {
		t.Fatal("Clean() DryRun = true, want false")
	}

	calls := readDockerCalls(t, logPath)
	for _, want := range []string{
		"rm -f abc123",
		"rm -f xyz789",
		"rmi -f img1",
		"volume rm vol1",
		"network rm net1",
		"builder prune -f",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("docker log missing %q in:\n%s", want, calls)
		}
	}
}

func TestCleanDryRunDoesNotRemove(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	writeFakeDocker(t, dir, logPath)

	r := &dockerRuntime{}
	result, err := r.Clean(context.Background(), CleanOptions{
		Containers: true,
		Images:     true,
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if !result.DryRun {
		t.Fatal("Clean() DryRun = false, want true")
	}
	if len(result.Items) != 3 {
		t.Fatalf("Clean() Items = %d, want 3: %+v", len(result.Items), result.Items)
	}

	calls := readDockerCalls(t, logPath)
	for _, forbid := range []string{"rm -f abc123", "rmi -f img1"} {
		if strings.Contains(calls, forbid) {
			t.Errorf("dry-run must not call %q, log:\n%s", forbid, calls)
		}
	}
}

func TestCleanNoSelectionDoesNothing(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	writeFakeDocker(t, dir, logPath)

	r := &dockerRuntime{}
	result, err := r.Clean(context.Background(), CleanOptions{})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("Clean() Items = %d, want 0", len(result.Items))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestParseIDList|TestClean" -v -count=1`

Expected: FAIL — compile error `undefined: parseIDList`, `undefined: CleanOptions`, `undefined: Clean`, `undefined: CleanResult`.

- [ ] **Step 3: Add `Clean` to the interface + stub in `internal/runtime/runtime.go`**

In the `Manager` interface (after the `KeepLastNImages` line at `runtime.go:36`), add:

```go
	Clean(ctx context.Context, opts CleanOptions) (CleanResult, error)
```

In `stubManager` (after the `KeepLastNImages` method at `runtime.go:117`), add:

```go
func (m *stubManager) Clean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	return CleanResult{}, nil
}
```

- [ ] **Step 4: Create `internal/runtime/prune.go`**

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CleanOptions selects which categories of unused Docker resources to clean.
type CleanOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	Cache      bool
	DryRun     bool
}

// CleanItem is a single resource that was (or would be) removed.
type CleanItem struct {
	Kind string
	ID   string
}

// CleanResult summarizes a cleanup run.
type CleanResult struct {
	Items  []CleanItem
	DryRun bool
}

func (r *CleanResult) add(kind, id string) {
	r.Items = append(r.Items, CleanItem{Kind: kind, ID: id})
}

// Clean removes unused Docker resources. Tengiz-managed containers (labeled
// tengiz-app) and volumes mounted by apps are never touched. In dry-run mode
// it only reports what would be removed.
func (r *dockerRuntime) Clean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	result := CleanResult{DryRun: opts.DryRun}

	if opts.Containers {
		ids, err := r.orphanStoppedContainers(ctx)
		if err != nil {
			return result, err
		}
		for _, id := range ids {
			result.add("container", id)
			if opts.DryRun {
				continue
			}
			if err := r.removeResource(ctx, "rm", "-f", id); err != nil {
				return result, err
			}
		}
	}

	if opts.Images {
		ids, err := r.danglingImages(ctx)
		if err != nil {
			return result, err
		}
		for _, id := range ids {
			result.add("image", id)
			if opts.DryRun {
				continue
			}
			if err := r.removeResource(ctx, "rmi", "-f", id); err != nil {
				return result, err
			}
		}
	}

	if opts.Volumes {
		ids, err := r.danglingVolumes(ctx)
		if err != nil {
			return result, err
		}
		for _, id := range ids {
			result.add("volume", id)
			if opts.DryRun {
				continue
			}
			if err := r.removeResource(ctx, "volume", "rm", id); err != nil {
				return result, err
			}
		}
	}

	if opts.Networks {
		ids, err := r.danglingNetworks(ctx)
		if err != nil {
			return result, err
		}
		for _, id := range ids {
			result.add("network", id)
			if opts.DryRun {
				continue
			}
			if err := r.removeResource(ctx, "network", "rm", id); err != nil {
				return result, err
			}
		}
	}

	if opts.Cache {
		result.add("cache", "build-cache")
		if !opts.DryRun {
			if err := r.removeResource(ctx, "builder", "prune", "-f"); err != nil {
				return result, err
			}
		}
	}

	return result, nil
}

func (r *dockerRuntime) orphanStoppedContainers(ctx context.Context) ([]string, error) {
	out, err := r.dockerOutput(ctx, "ps", "-a", "-q",
		"--filter", "status=exited",
		"--filter", "status=created",
		"--filter", "label!=tengiz-app")
	if err != nil {
		return nil, err
	}
	return parseIDList(out), nil
}

func (r *dockerRuntime) danglingImages(ctx context.Context) ([]string, error) {
	out, err := r.dockerOutput(ctx, "images", "--filter", "dangling=true", "-q")
	if err != nil {
		return nil, err
	}
	return parseIDList(out), nil
}

func (r *dockerRuntime) danglingVolumes(ctx context.Context) ([]string, error) {
	out, err := r.dockerOutput(ctx, "volume", "ls", "--filter", "dangling=true", "-q")
	if err != nil {
		return nil, err
	}
	return parseIDList(out), nil
}

func (r *dockerRuntime) danglingNetworks(ctx context.Context) ([]string, error) {
	out, err := r.dockerOutput(ctx, "network", "ls", "--filter", "dangling=true", "-q")
	if err != nil {
		return nil, err
	}
	return parseIDList(out), nil
}

func (r *dockerRuntime) dockerOutput(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return out, nil
}

func (r *dockerRuntime) removeResource(ctx context.Context, args ...string) error {
	full := append([]string{"docker"}, args...)
	cmd := exec.CommandContext(ctx, full[0], full[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", strings.Join(full, " "), err, string(out))
	}
	return nil
}

func parseIDList(out []byte) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids
}
```

- [ ] **Step 5: Update the three remaining `Manager` mocks**

In `internal/cli/root_test.go` (after the `KeepLastNImages` method at `root_test.go:99`):

```go
func (m *mockRTForDeploy) Clean(ctx context.Context, opts runtime.CleanOptions) (runtime.CleanResult, error) {
	return runtime.CleanResult{}, nil
}
```

In `internal/proxy/proxy_test.go` (after the `KeepLastNImages` method at `proxy_test.go:34`):

```go
func (m *mockRuntime) Clean(ctx context.Context, opts runtime.CleanOptions) (runtime.CleanResult, error) {
	return runtime.CleanResult{}, nil
}
```

In `internal/idle/idle_test.go` (after the `KeepLastNImages` method at `idle_test.go:33`):

```go
func (m *mockRuntime) Clean(ctx context.Context, opts runtime.CleanOptions) (runtime.CleanResult, error) {
	return runtime.CleanResult{}, nil
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/proxy/... ./internal/idle/... -v -count=1`

Expected: PASS for all tests in these packages (including the four new `TestClean*`/`TestParseIDList` tests).

- [ ] **Step 7: Build + vet**

Run: `go build -o /tmp/tengiz . && go vet ./...`

Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune.go internal/runtime/prune_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add Clean method for Docker housekeeping"
```

---

### Task 2: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` (insert after the `rmCmd` block, before `var logsCmd`), register it in `init()` (after `rootCmd.AddCommand(runCmd)`), and add flags (at the end of `init()`, after the webhook flags)
- Create: `internal/cli/cmd_cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `Manager.Clean(ctx, CleanOptions) (CleanResult, error)`, `Manager.KeepLastNImages(ctx, appName, n)`, `config.NewStoreWithEnv(dataDir, env)`, `Store.ListApps() ([]types.AppEntry, error)`, `cleanupCmd` flags
- Produces: `tengiz cleanup [--containers|--images|--volumes|--networks|--cache|--all|--dry-run]` command

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cmd_cleanup_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeDockerForCLI(t *testing.T, dir, logPath string) {
	t.Helper()
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$TENGIZ_TEST_DOCKER_LOG"
case "$*" in
  "ps -a -q --filter status=exited --filter status=created --filter label!=tengiz-app")
    printf 'abc123\nxyz789\n'
    ;;
  "images --filter dangling=true -q")
    printf 'img1\n'
    ;;
  "volume ls --filter dangling=true -q")
    printf 'vol1\n'
    ;;
  "network ls --filter dangling=true -q")
    printf 'net1\n'
    ;;
esac
exit 0
`
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("TENGIZ_TEST_DOCKER_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"containers", "images", "volumes", "networks", "cache", "all", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupDryRunOutput(t *testing.T) {
	dir := t.TempDir()
	dataDir = dir
	writeFakeDockerForCLI(t, dir, filepath.Join(dir, "calls.log"))

	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})

	for _, want := range []string{
		"container (2): abc123, xyz789",
		"image (1): img1",
		"volume (1): vol1",
		"network (1): net1",
		"cache (1): build-cache",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("cleanup output missing %q, got:\n%s", want, output)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanupCmdRegistered|TestCleanupDryRunOutput" -v -count=1`

Expected: FAIL — `cleanup command not registered` (command does not exist yet).

- [ ] **Step 3: Add the `cleanupCmd` command to `internal/cli/root.go`**

Insert this block after the `rmCmd` definition (after the closing `}` of `rmCmd`, before `var logsCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long: `Remove unused Docker resources to reclaim disk space.

By default (or with --all) cleans: stopped containers not managed by Tengiz,
dangling images, unused volumes, unused networks, and the Docker build cache.
Tengiz-managed containers (labeled tengiz-app) and volumes mounted by apps are
always protected. Tagged app images are pruned to the latest 5 per app.

Use --dry-run to see what would be removed without removing anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")
		all, _ := cmd.Flags().GetBool("all")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if all || (!containers && !images && !volumes && !networks && !cache) {
			containers, images, volumes, networks, cache = true, true, true, true, true
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := runtime.CleanOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			Cache:      cache,
			DryRun:     dryRun,
		}

		result, err := rt.Clean(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		if images {
			store := config.NewStoreWithEnv(dataDir, env)
			apps, listErr := store.ListApps()
			if listErr == nil {
				for _, app := range apps {
					if err := rt.KeepLastNImages(cmd.Context(), app.Name, 5); err != nil {
						log.Printf("[tengiz] warning: image retention for %s: %v", app.Name, err)
					}
				}
			}
		}

		if len(result.Items) == 0 {
			fmt.Printf("[tengiz] nothing to clean\n")
			return nil
		}

		mode := "removed"
		if dryRun {
			mode = "would be removed"
		}
		fmt.Printf("[tengiz] %d item(s) %s:\n", len(result.Items), mode)
		byKind := make(map[string][]string)
		for _, item := range result.Items {
			byKind[item.Kind] = append(byKind[item.Kind], item.ID)
		}
		for _, kind := range []string{"container", "image", "volume", "network", "cache"} {
			ids := byKind[kind]
			if len(ids) == 0 {
				continue
			}
			fmt.Printf("  %s (%d): %s\n", kind, len(ids), strings.Join(ids, ", "))
		}
		return nil
	},
}
```

In `init()` (after `rootCmd.AddCommand(runCmd)`), register the command:

```go
	rootCmd.AddCommand(cleanupCmd)
```

At the end of `init()` (after the `webhookCmd` flags, before the closing brace), add the flags:

```go
	cleanupCmd.Flags().Bool("containers", false, "remove stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "remove dangling images and prune old app images")
	cleanupCmd.Flags().Bool("volumes", false, "remove unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "remove unused networks")
	cleanupCmd.Flags().Bool("cache", false, "prune the Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "clean all categories (default when none selected)")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanupCmdRegistered|TestCleanupDryRunOutput" -v -count=1`

Expected: PASS — command found, all flags present, and dry-run output contains all five category lines.

- [ ] **Step 5: Build + vet + full test**

Run: `go build -o /tmp/tengiz . && go vet ./... && go test ./... -count=1`

Expected: no errors; all packages pass.

- [ ] **Step 6: Manual smoke test**

Run:
```bash
/tmp/tengiz cleanup --dry-run
```

Expected: prints a categorized summary (`[tengiz] N item(s) would be removed:`) and removes nothing. On a machine with no stale resources it prints `[tengiz] nothing to clean`.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cmd_cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 3: Documentation

**Files:**
- Modify: `README.md` — add `tengiz cleanup` CLI reference (after the `### tengiz rm <app>` section at `README.md:228`, before `### tengiz rollback <app>`)
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented

**Interfaces:**
- Consumes: Task 2's completed `tengiz cleanup` command and its flags
- Produces: updated user-facing and feature-tracking documentation

- [ ] **Step 1: Add the CLI reference to `README.md`**

Insert after the `tengiz rm <app>` section (line 228) and before `### tengiz rollback <app>` (line 230):

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space. Tengiz-managed containers (labeled `tengiz-app`) and volumes mounted by apps are always protected. By default (or with `--all`) cleans all categories. Use `--dry-run` to preview what would be removed.

| Flag | Description |
|------|-------------|
| `--containers` | Remove stopped containers not managed by Tengiz |
| `--images` | Remove dangling build images and prune old app images (keep latest 5) |
| `--volumes` | Remove unused volumes |
| `--networks` | Remove unused networks |
| `--cache` | Prune the Docker build cache |
| `--all` | Clean all categories (default when no category flag is given) |
| `--dry-run` | Show what would be removed without removing anything |

#### `tengiz cleanup --dry-run`

Prints a categorized summary of candidates without deleting anything. Use it before running a destructive cleanup.
```

- [ ] **Step 2: Update `docs/FUTURES_FEATURES.md`**

Change the priority table row (line 19) from pending to implemented:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

In the `## Docker Housekeeping (Otomatik Temizlik)` feature section (after the `- **Description:**` line), add:

```markdown
- **Status:** ✅ Implemented (2026-08-15)
```

In the `### ✅ Implemented Features (Not Pending)` table (after the Webhook row at line 253), add:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-15) |
```

- [ ] **Step 3: Verify the full suite still passes**

Run: `go build -o /tmp/tengiz . && go vet ./... && go test ./... -count=1`

Expected: no errors; all tests pass.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker housekeeping implemented"
```