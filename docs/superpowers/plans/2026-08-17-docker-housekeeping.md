# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely prunes unused Docker resources (containers, images, volumes, networks, build cache) using label-based filtering that protects Tengiz-managed containers.

**Architecture:** Extends the existing `runtime.Manager` interface with a `Cleanup` method. Docker CLI prune commands are built by pure, testable helper functions following the existing `buildRunArgs`/`buildLogArgs` pattern in `internal/runtime/docker.go`. `docker container prune` uses `label!=tengiz-app` and `label!=tengiz-env` filters so Tengiz containers are never removed. `--all-images` removes unused non-Tengiz images one-by-one (reusing the existing `RemoveImage`), preserving `tengiz-apps/*` rollback images. The CLI wires a `tengiz cleanup` command with per-category flags, a `--dry-run` preview, and a `-y/--force`-gated confirmation prompt.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (no Docker SDK), `bufio` (std lib). No new external dependencies.

## Global Constraints

- Tengiz-managed containers (labels `tengiz-app` / `tengiz-env`) must NEVER be removed by cleanup
- `tengiz-apps/*` images (kept for rollback by `KeepLastNImages`) must be preserved by `--all-images`
- Volumes are opt-in only (`--volumes` or `--all`) because they may contain data
- Default cleanup (no category flags) = containers + dangling images + networks + build cache (safe set, excludes volumes)
- `docker image prune` does NOT support `reference!=` negation — verified: `Error response from daemon: invalid filter 'reference!'`. Do not use it; use the one-by-one `RemoveImage` approach for all-image cleanup
- Cleanup behavior is env-agnostic: the label filters protect Tengiz containers in ALL environments, so the `--env` global flag does not change cleanup behavior
- No new external dependencies
- All new runtime tests use the stub or pure helper functions — no live Docker daemon required (matches existing convention; existing runtime tests never call real docker)
- Existing tests must continue to pass without modification; the 3 mock types implementing `runtime.Manager` only gain the new `Cleanup` method
- Feature/UI-adjacent changes update `README.md`, `AGENTS.md`, and `docs/FUTURES_FEATURES.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Add `CleanupOptions`, `CleanupResult`, `CleanupCommand`, pure prune-arg builders, `reclaimedFromOutput`, `nonTengizImageRefs`, `CleanupCommands`, and the `dockerRuntime.Cleanup` implementation |
| `internal/runtime/runtime.go` | Add `Cleanup` to the `Manager` interface + `stubManager.Cleanup` |
| `internal/runtime/cleanup_test.go` | Tests for all pure helpers + `TestStubCleanup` |
| `internal/cli/root.go` | Add `cleanupCmd`, register it in `init()`, add flags, add `confirmCleanup` and `orDash` helpers (add `bufio` import) |
| `internal/cli/cleanup_test.go` | New file: registration/flags/dry-run/confirmation tests |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy` |
| `internal/idle/idle_test.go` | Add `Cleanup` method to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` method to `mockRuntime` |
| `README.md` | Add `tengiz cleanup` to Quick Start + CLI Reference; add feature bullet |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as ✅ Implemented |

---

### Task 1: Runtime cleanup types + pure prune helpers

**Files:**
- Modify: `internal/runtime/cleanup.go` — append types + pure helper functions
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new (pure functions, no Manager dependency)
- Produces: `runtime.CleanupOptions{Containers, Images, AllImages, Volumes, Networks, Cache bool}`, `runtime.CleanupResult{Containers, Images, Volumes, Networks, Cache string}`, `runtime.CleanupCommand{Args []string}`, `runtime.CleanupCommands(opts CleanupOptions) []CleanupCommand`, `pruneContainersArgs() []string`, `pruneImagesArgs() []string`, `pruneVolumesArgs() []string`, `pruneNetworksArgs() []string`, `pruneCacheArgs() []string`, `reclaimedFromOutput(out []byte) string`, `nonTengizImageRefs(out string) []string`, const `tengizImagePrefix = "tengiz-apps/"`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestPruneContainersArgs(t *testing.T) {
	expected := []string{
		"container", "prune", "-f",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
	got := pruneContainersArgs()
	if len(got) != len(expected) {
		t.Fatalf("pruneContainersArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestPruneImagesArgs(t *testing.T) {
	got := pruneImagesArgs()
	expected := []string{"image", "prune", "-f"}
	if len(got) != len(expected) {
		t.Fatalf("pruneImagesArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestPruneVolumesNetworksCacheArgs(t *testing.T) {
	if got := pruneVolumesArgs(); strings.Join(got, " ") != "volume prune -f" {
		t.Fatalf("pruneVolumesArgs() = %v", got)
	}
	if got := pruneNetworksArgs(); strings.Join(got, " ") != "network prune -f" {
		t.Fatalf("pruneNetworksArgs() = %v", got)
	}
	if got := pruneCacheArgs(); strings.Join(got, " ") != "builder prune -f" {
		t.Fatalf("pruneCacheArgs() = %v", got)
	}
}

func TestReclaimedFromOutput(t *testing.T) {
	tests := []struct {
		name string
		out  []byte
		want string
	}{
		{"empty output", []byte(""), ""},
		{"container prune", []byte("Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 1.234GB\n"), "1.234GB"},
		{"builder prune", []byte("Total:\t0B\n"), "0B"},
		{"no reclaim line", []byte("nothing happened here"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reclaimedFromOutput(tt.out); got != tt.want {
				t.Errorf("reclaimedFromOutput(%q) = %q, want %q", tt.out, got, tt.want)
			}
		})
	}
}

func TestNonTengizImageRefs(t *testing.T) {
	out := `tengiz-apps/myapp:latest
tengiz-apps/myapp:1700000000
ghcr.io/foo/bar:latest
node:20
<none>:<none>
`
	got := nonTengizImageRefs(out)
	expected := []string{"ghcr.io/foo/bar:latest", "node:20"}
	if len(got) != len(expected) {
		t.Fatalf("nonTengizImageRefs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("ref[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestCleanupCommands(t *testing.T) {
	cmds := CleanupCommands(CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		Cache:      true,
	})
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d: %+v", len(cmds), cmds)
	}
	if strings.Join(cmds[0].Args, " ") != "container prune -f --filter label!=tengiz-app --filter label!=tengiz-env" {
		t.Fatalf("first command = %v", cmds[0].Args)
	}
	if strings.Join(cmds[4].Args, " ") != "builder prune -f" {
		t.Fatalf("last command = %v", cmds[4].Args)
	}
	if len(CleanupCommands(CleanupOptions{})) != 0 {
		t.Fatal("expected no commands for empty options")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestPruneContainersArgs|TestPruneImagesArgs|TestPruneVolumesNetworksCacheArgs|TestReclaimedFromOutput|TestNonTengizImageRefs|TestCleanupCommands" -v -count=1`

Expected: FAIL with `undefined: pruneContainersArgs`, `undefined: CleanupOptions`, etc.

- [ ] **Step 3: Add the types and helper functions to `internal/runtime/cleanup.go`**

Append to the bottom of `internal/runtime/cleanup.go` (existing imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings` already cover what's needed):

```go
const tengizImagePrefix = "tengiz-apps/"

// CleanupOptions selects which Docker resource categories to clean.
type CleanupOptions struct {
	Containers bool // prune stopped containers not managed by Tengiz
	Images     bool // prune dangling images
	AllImages  bool // also remove all unused non-Tengiz images (preserves tengiz-apps/* rollback images)
	Volumes    bool // prune unused anonymous volumes (may contain data)
	Networks   bool // prune unused networks
	Cache      bool // prune Docker build cache
}

// CleanupResult reports reclaimed space per category.
// An empty string means nothing was reclaimed (or the category was not run).
type CleanupResult struct {
	Containers string
	Images     string
	Volumes    string
	Networks   string
	Cache      string
}

// CleanupCommand describes a single docker command cleanup would execute.
type CleanupCommand struct {
	Args []string // docker CLI arguments, e.g. {"container", "prune", "-f", ...}
}

func pruneContainersArgs() []string {
	return []string{
		"container", "prune", "-f",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
}

func pruneImagesArgs() []string {
	return []string{"image", "prune", "-f"}
}

func pruneVolumesArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func pruneNetworksArgs() []string {
	return []string{"network", "prune", "-f"}
}

func pruneCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

// CleanupCommands returns the static docker prune commands that a cleanup with opts
// would execute, in order. AllImages is handled separately because it removes images
// one-by-one after enumerating them.
func CleanupCommands(opts CleanupOptions) []CleanupCommand {
	var cmds []CleanupCommand
	if opts.Containers {
		cmds = append(cmds, CleanupCommand{Args: pruneContainersArgs()})
	}
	if opts.Images {
		cmds = append(cmds, CleanupCommand{Args: pruneImagesArgs()})
	}
	if opts.Volumes {
		cmds = append(cmds, CleanupCommand{Args: pruneVolumesArgs()})
	}
	if opts.Networks {
		cmds = append(cmds, CleanupCommand{Args: pruneNetworksArgs()})
	}
	if opts.Cache {
		cmds = append(cmds, CleanupCommand{Args: pruneCacheArgs()})
	}
	return cmds
}

// reclaimedFromOutput extracts the reclaimed-space figure from a docker prune command's
// output. Container/image/volume/network prunes print "Total reclaimed space: X";
// builder prune prints "Total:\tX". Returns "" when nothing was reclaimed.
func reclaimedFromOutput(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
		if strings.HasPrefix(line, "Total:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total:"))
		}
	}
	return ""
}

// nonTengizImageRefs parses `docker images --format '{{.Repository}}:{{.Tag}}'` output
// and returns the references that are not managed by Tengiz and not dangling.
func nonTengizImageRefs(out string) []string {
	var refs []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, tengizImagePrefix) || strings.HasPrefix(line, "<none>:") {
			continue
		}
		refs = append(refs, line)
	}
	return refs
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestPruneContainersArgs|TestPruneImagesArgs|TestPruneVolumesNetworksCacheArgs|TestReclaimedFromOutput|TestNonTengizImageRefs|TestCleanupCommands" -v -count=1`

Expected: PASS for all 6 tests.

- [ ] **Step 5: Run the full runtime package test suite**

Run: `go test ./internal/runtime/... -count=1`

Expected: `ok` (existing tests unaffected).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): add cleanup types and docker prune helpers"
```

---

### Task 2: Wire Cleanup into the Manager interface + implementation

**Files:**
- Modify: `internal/runtime/runtime.go` — add `Cleanup` to `Manager` interface + `stubManager.Cleanup`
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.Cleanup`
- Modify: `internal/cli/root_test.go:98-100` — add `Cleanup` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:32-34` — add `Cleanup` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:33-35` — add `Cleanup` to `mockRuntime`
- Test: `internal/runtime/cleanup_test.go` — add `TestStubCleanup`

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult`, `CleanupCommands`, `nonTengizImageRefs`, `reclaimedFromOutput`, `RemoveImage` (all from Task 1)
- Produces: `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — later tasks call `rt.Cleanup(ctx, opts)` and read each `CleanupResult` field (empty string = nothing reclaimed)

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.Containers != "" || res.Images != "" {
		t.Errorf("expected empty CleanupResult, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubCleanup" -v -count=1`

Expected: FAIL with `stubManager.Cleanup undefined` / `Manager does not implement Cleanup`.

- [ ] **Step 3: Add `Cleanup` to the `Manager` interface and stub in `internal/runtime/runtime.go`**

In the `Manager` interface (`internal/runtime/runtime.go:31-49`), add after the `KeepLastNImages` line:

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

Add the stub method after `func (m *stubManager) KeepLastNImages(...)` (`internal/runtime/runtime.go:117-119`):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 4: Implement `dockerRuntime.Cleanup` in `internal/runtime/cleanup.go`**

Append to the bottom of `internal/runtime/cleanup.go`:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var res CleanupResult

	runPrune := func(args []string) (string, error) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
		return reclaimedFromOutput(out), nil
	}

	for _, c := range CleanupCommands(opts) {
		reclaimed, err := runPrune(c.Args)
		if err != nil {
			return res, err
		}
		switch c.Args[0] {
		case "container":
			res.Containers = reclaimed
		case "image":
			res.Images = reclaimed
		case "volume":
			res.Volumes = reclaimed
		case "network":
			res.Networks = reclaimed
		case "builder":
			res.Cache = reclaimed
		}
	}

	if opts.AllImages {
		imgCmd := exec.CommandContext(ctx, "docker", "images", "--format", "{{.Repository}}:{{.Tag}}")
		out, err := imgCmd.CombinedOutput()
		if err != nil {
			return res, fmt.Errorf("docker images: %w", err)
		}
		for _, ref := range nonTengizImageRefs(string(out)) {
			if err := r.RemoveImage(ctx, ref); err != nil {
				log.Printf("[runtime] cleanup: failed to remove image %s: %v", ref, err)
			}
		}
	}

	return res, nil
}
```

Note: `cleanup.go` already imports `context`, `fmt`, `log`, `os/exec`, `strings` — no import changes needed.

- [ ] **Step 5: Add `Cleanup` to the three mock types so all packages still compile**

In `internal/cli/root_test.go`, add after `func (m *mockRTForDeploy) KeepLastNImages(...)` (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

In `internal/idle/idle_test.go`, add after `func (m *mockRuntime) KeepLastNImages(...)` (line 33):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

In `internal/proxy/proxy_test.go`, add after `func (m *mockRuntime) KeepLastNImages(...)` (line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) {
	return runtime.CleanupResult{}, nil
}
```

- [ ] **Step 6: Run tests to verify everything compiles and passes**

Run: `go build ./... && go test ./... -count=1`

Expected: build succeeds and `ok` for all packages.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface with docker implementation"
```

---

### Task 3: CLI `tengiz cleanup` command

**Files:**
- Modify: `internal/cli/root.go:4-14` — add `bufio` import; `:34-89` — register command + flags; add `cleanupCmd` + `confirmCleanup` + `orDash` after `psCmd`
- Test: Create `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.CleanupCommands`, `runtime.NewDocker()`, `runtime.Manager.Cleanup`
- Produces: `tengiz cleanup` command with flags `--containers`, `--images`, `--all-images`, `--volumes`, `--networks`, `--cache`, `--all`, `--dry-run`, `-y/--force`; helpers `confirmCleanup(r io.Reader) bool`, `orDash(s string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	expected := []string{
		"containers", "images", "all-images", "volumes", "networks", "cache",
		"all", "dry-run", "force",
	}
	for _, name := range expected {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupDryRun(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		rootCmd.Execute()
	})
	for _, want := range []string{
		"docker container prune -f --filter label!=tengiz-app --filter label!=tengiz-env",
		"docker image prune -f",
		"docker network prune -f",
		"docker builder prune -f",
		"nothing was removed",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("dry-run output missing %q; got:\n%s", want, output)
		}
	}
}

func TestCleanupDryRunAll(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all"})
	output := captureOutput(func() {
		rootCmd.Execute()
	})
	for _, want := range []string{
		"docker volume prune -f",
		"docker rmi -f",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("dry-run --all output missing %q; got:\n%s", want, output)
		}
	}
}

func confirmResult(input string) bool {
	var accepted bool
	captureOutput(func() {
		accepted = confirmCleanup(strings.NewReader(input))
	})
	return accepted
}

func TestConfirmCleanup(t *testing.T) {
	if !confirmResult("y\n") {
		t.Error("confirmCleanup should accept 'y'")
	}
	if !confirmResult("YES\n") {
		t.Error("confirmCleanup should accept 'YES'")
	}
	if confirmResult("n\n") {
		t.Error("confirmCleanup should reject 'n'")
	}
	if confirmResult("\n") {
		t.Error("confirmCleanup should reject empty input")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup|TestConfirmCleanup" -v -count=1`

Expected: FAIL with `cleanup command not found`, `undefined: cleanupCmd`, `undefined: confirmCleanup`.

- [ ] **Step 3: Add the `bufio` import to `internal/cli/root.go`**

In the import block (`internal/cli/root.go:4-14`), add `"bufio"` as the first import:

```go
import (
	"bufio"
	"context"
	"fmt"
	...
```

- [ ] **Step 4: Register the command and its flags in `init()`**

In `init()` (`internal/cli/root.go:34-89`), add after `rootCmd.AddCommand(psCmd)`:

```go
	rootCmd.AddCommand(cleanupCmd)
```

And at the end of `init()` (after the existing `webhookCmd.Flags()` lines):

```go
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("all-images", false, "also remove all unused non-Tengiz images (preserves tengiz-apps/* rollback images)")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused anonymous volumes (may contain data)")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "enable all cleanup categories, including volumes")
	cleanupCmd.Flags().Bool("dry-run", false, "print the commands that would run without executing them")
	cleanupCmd.Flags().BoolP("force", "y", false, "skip the confirmation prompt")
```

- [ ] **Step 5: Add the `cleanupCmd` command and helpers**

Add immediately after the `psCmd` block (after `internal/cli/root.go:601`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources",
	Long: `Remove unused Docker resources to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app=*) are always protected and are never removed.

By default cleanup runs the safe categories: stopped non-Tengiz containers, dangling
images, unused networks, and the Docker build cache. Volumes are excluded because they
may contain data; include them with --volumes or --all.

Use --dry-run to preview the exact docker commands that would run, and -y/--force to
skip the confirmation prompt (for scripts and CI).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		allImages, _ := cmd.Flags().GetBool("all-images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")
		all, _ := cmd.Flags().GetBool("all")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")

		if all {
			containers, images, allImages, volumes, networks, cache = true, true, true, true, true, true
		}
		if allImages {
			images = true // --all-images also prunes dangling images
		}
		if !containers && !images && !allImages && !volumes && !networks && !cache {
			// Safe defaults: protect data and preserve rollback images.
			containers, images, networks, cache = true, true, true, true
		}

		opts := runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			AllImages:  allImages,
			Volumes:    volumes,
			Networks:   networks,
			Cache:      cache,
		}

		if dryRun {
			fmt.Println("[tengiz] cleanup dry-run — commands that would run:")
			for _, c := range runtime.CleanupCommands(opts) {
				fmt.Printf("  docker %s\n", strings.Join(c.Args, " "))
			}
			if opts.AllImages {
				fmt.Println("  docker images --format '{{.Repository}}:{{.Tag}}'")
				fmt.Println("  docker rmi -f <each unused non-tengiz-apps image>")
			}
			fmt.Println("[tengiz] dry-run complete — nothing was removed.")
			return nil
		}

		if !force && !confirmCleanup(os.Stdin) {
			fmt.Println("[tengiz] cleanup aborted.")
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		res, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Println("[tengiz] cleanup complete:")
		if opts.Containers {
			fmt.Printf("  containers: %s reclaimed\n", orDash(res.Containers))
		}
		if opts.Images {
			fmt.Printf("  images: %s reclaimed\n", orDash(res.Images))
		}
		if opts.AllImages {
			fmt.Println("  images: unused non-Tengiz images removed")
		}
		if opts.Volumes {
			fmt.Printf("  volumes: %s reclaimed\n", orDash(res.Volumes))
		}
		if opts.Networks {
			fmt.Printf("  networks: %s reclaimed\n", orDash(res.Networks))
		}
		if opts.Cache {
			fmt.Printf("  build cache: %s reclaimed\n", orDash(res.Cache))
		}
		return nil
	},
}

// confirmCleanup prompts the user on r (typically os.Stdin) and returns true when they
// confirm with y/yes.
func confirmCleanup(r io.Reader) bool {
	fmt.Print("This will remove unused Docker resources. Continue? [y/N] ")
	reader := bufio.NewReader(r)
	input, _ := reader.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "y", "yes":
		return true
	}
	return false
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestConfirmCleanup" -v -count=1`

Expected: PASS for all 5 tests.

- [ ] **Step 7: Run the full test suite**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: build succeeds, `go vet` reports nothing, all packages `ok`.

- [ ] **Step 8: Manually verify the dry-run and real cleanup against the Docker daemon**

Run: `go run . cleanup --dry-run`

Expected: prints the default 4 commands (`container prune`, `image prune`, `network prune`, `builder prune`) with the `label!=tengiz-app` filter on the container prune line, followed by `dry-run complete — nothing was removed`.

Run: `go run . cleanup -y`

Expected: prints `cleanup complete:` with reclaimed figures (`0B` or larger) for containers/images/networks/build cache, and does NOT print a volumes line.

Run: `go run . cleanup --dry-run --all`

Expected: also prints `docker volume prune -f` and the `docker rmi -f` note lines.

- [ ] **Step 9: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command for docker housekeeping"
```

---

### Task 4: Documentation updates

**Files:**
- Modify: `README.md:12-24` — add feature bullet; `:98-99` — add to Quick Start; after `:150` — add `tengiz cleanup` CLI Reference section
- Modify: `AGENTS.md` — add `tengiz cleanup` to the CLI command list
- Modify: `docs/FUTURES_FEATURES.md:19` — mark feature #6 as ✅ Implemented; add row to the Implemented Features table; add Status line to the detailed section

**Interfaces:**
- Consumes: final command behavior from Task 3
- Produces: updated docs only

- [ ] **Step 1: Update `README.md` Features list**

After the `- **Deployment history** ...` bullet (`README.md:20`), add:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, images, volumes, networks, and build cache with label-aware protection for Tengiz-managed resources.
```

- [ ] **Step 2: Update `README.md` Quick Start**

After `tengiz proxy` line (`README.md:99`), add:

```markdown
tengiz cleanup        # prune unused Docker resources (label-aware, dry-run with --dry-run)
```

- [ ] **Step 3: Add the `tengiz cleanup` section to `README.md` CLI Reference**

After the `### \`tengiz ps\`` section (ends `README.md:150`), add:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling images |
| `--all-images` | Also remove all unused non-Tengiz images (preserves `tengiz-apps/*` rollback images) |
| `--volumes` | Prune unused anonymous volumes (may contain data) |
| `--networks` | Prune unused networks |
| `--cache` | Prune the Docker build cache |
| `--all` | Enable all categories, including volumes |
| `--dry-run` | Print the commands that would run without executing them |
| `-y`, `--force` | Skip the confirmation prompt |

Tengiz-managed containers (labeled `tengiz-app=*`) are **never** removed. By default only
the safe categories run (containers, dangling images, networks, build cache); volumes are
excluded because they may contain data. Runs a confirmation prompt unless `-y/--force` is
passed.

```bash
tengiz cleanup                # safe default cleanup with confirmation
tengiz cleanup --dry-run      # preview the exact docker commands
tengiz cleanup --all -y       # full cleanup incl. volumes, no prompt
```
```

- [ ] **Step 4: Update `AGENTS.md` CLI command list**

In the `## CLI` section, add after the `tengiz ps` line:

```
tengiz cleanup        → prune unused Docker resources (label-aware; --dry-run, --all, -y)
```

- [ ] **Step 5: Update `docs/FUTURES_FEATURES.md`**

1. In the P0 table (`docs/FUTURES_FEATURES.md:19`), change row #6 to mark it implemented:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

2. In the "✅ Implemented Features" table, add after the Nixpacks row (`:387`):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-17) |
```

3. In the detailed `## Docker Housekeeping (Otomatik Temizlik)` section (`:377-381`), add:

```markdown
- **Status:** ✅ Implemented (2026-08-17)
```

- [ ] **Step 6: Verify docs reference real behavior**

Run: `go run . cleanup --help`

Expected: help text shows all flags (`--containers`, `--images`, `--all-images`, `--volumes`, `--networks`, `--cache`, `--all`, `--dry-run`, `-y, --force`), and the Short text `Remove unused Docker resources`.

- [ ] **Step 7: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup docker housekeeping"
```

---

## Self-Review

**1. Spec coverage:**

| Feature spec requirement | Task |
|---|---|
| `tengiz cleanup` command | Task 3 |
| Label-based filtering protects Tengiz containers | Task 1 (`pruneContainersArgs` filters), Task 2 (implementation) |
| Clean unused containers | Task 1/Task 2 (`docker container prune`) |
| Clean unused images | Task 1/Task 2 (`--images` dangling; `--all-images` via `RemoveImage`) |
| Clean unused volumes | Task 1/Task 2 (`--volumes`, opt-in) |
| Clean unused networks | Task 1/Task 2 (`--networks`) |
| Periodic/automatic cleanup (Coolify `DockerCleanupJob`) | Out of scope — the spec's concrete deliverable is the `tengiz cleanup` command ("`tengiz cleanup` komutu eklenebilir"). Periodic scheduling is implicitly covered by `--force` + `--dry-run` for cron use. |
| Per-category granular prune (#56, P1) | Deliberately NOT included — separate pending feature; `--all` here is an aggregate switch, not surgical category control |
| Env-awareness | Satisfied: label filters protect Tengiz containers in all envs; documented in Global Constraints |

**2. Placeholder scan:** No TBD/TODO/"add error handling"/"write tests" placeholders. Every code step contains complete, runnable code. Verified the exact docker command output formats (`Total reclaimed space:` vs builder's `Total:` and the `reference!` filter failure) against the live Docker daemon before writing the plan.

**3. Type consistency:** `CleanupOptions` and `CleanupResult` field names (`Containers`, `Images`, `AllImages`, `Volumes`, `Networks`, `Cache`) are used identically in Tasks 1-3. `CleanupCommands(opts CleanupOptions) []CleanupCommand` signature matches across Task 1 (definition), Task 2 (used by `dockerRuntime.Cleanup`), and Task 3 (used by dry-run). `ConfirmCleanup(r io.Reader) bool` in Task 3 matches its test. Mock methods added in Task 2 use the same `Cleanup(ctx, opts runtime.CleanupOptions) (runtime.CleanupResult, error)` signature in all three files.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-17-docker-housekeeping.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?