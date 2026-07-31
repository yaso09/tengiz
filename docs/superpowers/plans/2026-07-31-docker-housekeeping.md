# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (stopped containers, dangling images, unused networks) using label-based safety filters so Tengiz-managed containers are never removed, freeing disk space on single-server deployments.

**Architecture:** Extend the existing `runtime.Manager` interface with a `Cleanup(ctx, opts)` method. The Docker implementation runs granular `docker <object> prune -f` commands (`container`, `image`, `network` by default; `volume` and `builder` opt-in) with `--filter label!=tengiz-app` to protect every resource labeled `tengiz-app=*`, and `--filter dangling=true` for images (tagged `tengiz-apps/*` rollback images are untouched — they are already trimmed to the last 5 by the existing `KeepLastNImages`). A pure `parsePruneOutput` helper converts Docker's stdout into deleted counts + reclaimed space for the CLI summary. The Cobra command lives in `internal/cli/root.go` alongside the other lifecycle commands.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` Docker CLI (no Docker SDK), existing `runtime.Manager` interface and `stubManager`/mock test pattern.

## Global Constraints

- No new external dependencies — only Go stdlib + existing cobra
- Docker is invoked via the `docker` CLI with `os/exec` (never the Docker SDK)
- Every Tengiz-managed container and network is labeled `tengiz-app=<appname>` (`labelKey = "tengiz-app"` in `internal/runtime/docker.go:76`) and MUST be protected: prune filters use `label!=tengiz-app`
- Image pruning targets ONLY dangling images (`--filter dangling=true`); versioned `tengiz-apps/<app>:<deploymentID>` images are managed exclusively by the existing `KeepLastNImages` (keep 5) and must never be pruned here
- Safe-by-default categories: `containers`, `images`, `networks` are pruned unless explicitly disabled; `volumes` and `build-cache` are OFF by default (potentially destructive) and require explicit flags
- `tengiz cleanup` does NOT need the global `--env` flag — it operates on the Docker daemon, not on env-scoped state files
- Adding a method to the `runtime.Manager` interface requires updating ALL mock implementations: `stubManager` (runtime.go), `mockRuntime` (proxy_test.go), `mockRuntime` (idle_test.go), `mockRTForDeploy` (root_test.go). `gitdeploy/deployer_test.go` passes `nil` for runtime and needs no change
- All new tests must pass WITHOUT Docker installed (pure helper + stub + CLI no-op path only)
- Tests run with `go test ./... -v -count=1`; vet with `go vet ./...`; build with `go build ./...`

## Scope

Out of scope for this plan (separate subsystem, tracked as FUTURES_FEATURES.md #57 "Background Monitoring Scheduler"): running `cleanup` periodically via a background `DockerCleanupJob`. This plan delivers the reusable `runtime.Manager.Cleanup` primitive + the `tengiz cleanup` CLI command; a scheduler can call the same method later. Helper-container cleanup (`CleanupHelperContainersJob`) is also out of scope — Tengiz builds via BuildKit and creates no helper containers.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `CleanupOptions`, `CleanupResult` types; add `Cleanup` to `Manager` interface; implement `stubManager.Cleanup` |
| `internal/runtime/cleanup.go` | Docker impl: `buildPruneArgs(category)`, `parsePruneOutput(category, output)`, `runPrune`, `(dockerRuntime).Cleanup` |
| `internal/runtime/cleanup_test.go` | Tests: `TestStubCleanup`, `TestBuildPruneArgs`, `TestParsePruneOutput` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to `mockRuntime` (interface compliance) |
| `internal/idle/idle_test.go` | Add `Cleanup` to `mockRuntime` (interface compliance) |
| `internal/cli/root.go` | Add `cleanupCmd` + flag registration in `init()` |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy`; CLI tests: registration, flags, no-op path |
| `README.md` | Add `### tengiz cleanup` to CLI Reference |
| `AGENTS.md` | Add `tengiz cleanup` to CLI list + update `runtime.Manager` row |

No new files created. Changes touch 7 existing files + 2 docs.

---

### Task 1: Extend `runtime.Manager` with `Cleanup` (types, interface, stub, mocks)

**Files:**
- Modify: `internal/runtime/runtime.go:18-29` (add types after `RunOptions`) and `:31-49` (interface) and `:113-119` (stub)
- Modify: `internal/proxy/proxy_test.go:34` (after `KeepLastNImages`)
- Modify: `internal/idle/idle_test.go:33` (after `KeepLastNImages`)
- Modify: `internal/cli/root_test.go:99` (after `KeepLastNImages`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{Containers, Images, Networks, Volumes, BuildCache bool}`, `runtime.CleanupResult{ContainersDeleted, ImagesDeleted, NetworksDeleted, VolumesDeleted, BuildCacheDeleted int, ReclaimedSpace string}`, `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go` (the file already imports `context` and `testing`):

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true,
		Images:     true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ContainersDeleted != 0 || res.ImagesDeleted != 0 || res.NetworksDeleted != 0 {
		t.Errorf("expected zero counts, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`

Expected: FAIL — compile error: `undefined: CleanupOptions` (the type and method do not exist yet).

- [ ] **Step 3: Implement the interface addition**

In `internal/runtime/runtime.go`, after the `RunOptions` struct (line 29), add:

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
}

type CleanupResult struct {
	ContainersDeleted int
	ImagesDeleted     int
	NetworksDeleted   int
	VolumesDeleted    int
	BuildCacheDeleted int
	ReclaimedSpace    string
}
```

In the `Manager` interface (line 36), directly after the `KeepLastNImages` line, add:

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

In `internal/runtime/runtime.go`, after the `stubManager.KeepLastNImages` method (line 119), add:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

- [ ] **Step 4: Update the three mock implementations (required for compilation)**

In `internal/proxy/proxy_test.go`, after line 34 (`KeepLastNImages`):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

In `internal/idle/idle_test.go`, after line 33 (`KeepLastNImages`):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

In `internal/cli/root_test.go`, after line 99 (`KeepLastNImages`):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestStubCleanup -v -count=1`

Expected: PASS. Then run the packages containing the updated mocks to confirm interface compliance:
`go test ./internal/runtime/ ./internal/proxy/ ./internal/idle/ ./internal/cli/ -count=1`

Expected: all PASS. (Note: proxy tests are slow, ~2s each per AGENTS.md.)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Cleanup to Manager interface"
```

---

### Task 2: Docker prune helpers + `Cleanup` implementation

**Files:**
- Modify: `internal/runtime/cleanup.go` (append — file already imports `context`, `fmt`, `log`, `os/exec`, `sort`, `strings`; no import changes needed)
- Test: `internal/runtime/cleanup_test.go` (append)

**Interfaces:**
- Consumes: `CleanupOptions`/`CleanupResult` from Task 1; `labelKey` constant (`internal/runtime/docker.go:76`)
- Produces: `buildPruneArgs(category string) []string`, `parsePruneOutput(category, output string) (deleted int, reclaimed string)`, `(r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`, `(r *dockerRuntime) runPrune(ctx context.Context, category string) (string, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go` (add `reflect` to the existing imports):

```go
import (
	"context"
	"reflect"
	"testing"
)

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		category string
		want     []string
	}{
		{"container", []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"image", []string{"image", "prune", "-f", "--filter", "dangling=true"}},
		{"network", []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{"volume", []string{"volume", "prune", "-f"}},
		{"builder", []string{"builder", "prune", "-f"}},
	}
	for _, tc := range tests {
		got := buildPruneArgs(tc.category)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("buildPruneArgs(%q) = %v, want %v", tc.category, got, tc.want)
		}
	}
}

func TestParsePruneOutput(t *testing.T) {
	t.Run("containers count deleted IDs", func(t *testing.T) {
		out := "Deleted Containers:\n" +
			"abc123\n" +
			"def456\n" +
			"\n" +
			"Total reclaimed space: 12.3kB\n"
		n, reclaimed := parsePruneOutput("container", out)
		if n != 2 {
			t.Errorf("deleted = %d, want 2", n)
		}
		if reclaimed != "12.3kB" {
			t.Errorf("reclaimed = %q, want %q", reclaimed, "12.3kB")
		}
	})

	t.Run("images count untagged lines only", func(t *testing.T) {
		out := "Deleted Images:\n" +
			"untagged: sha256:111\n" +
			"untagged: sha256:222\n" +
			"deleted: sha256:aaa\n" +
			"deleted: sha256:bbb\n" +
			"\n" +
			"Total reclaimed space: 4.096kB\n"
		n, reclaimed := parsePruneOutput("image", out)
		if n != 2 {
			t.Errorf("deleted = %d, want 2 (untagged only)", n)
		}
		if reclaimed != "4.096kB" {
			t.Errorf("reclaimed = %q, want %q", reclaimed, "4.096kB")
		}
	})

	t.Run("empty output", func(t *testing.T) {
		n, reclaimed := parsePruneOutput("volume", "Total reclaimed space: 0B\n")
		if n != 0 || reclaimed != "0B" {
			t.Errorf("got (%d, %q), want (0, 0B)", n, reclaimed)
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run 'TestBuildPruneArgs|TestParsePruneOutput' -v -count=1`

Expected: FAIL — compile error: `undefined: buildPruneArgs` / `undefined: parsePruneOutput`.

- [ ] **Step 3: Write the Docker implementation**

Append to `internal/runtime/cleanup.go` (after the existing `KeepLastNImages` method):

```go
func buildPruneArgs(category string) []string {
	args := []string{category, "prune", "-f"}
	switch category {
	case "container", "network":
		return append(args, "--filter", "label!="+labelKey)
	case "image":
		return append(args, "--filter", "dangling=true")
	}
	return args
}

func parsePruneOutput(category, output string) (int, string) {
	deleted := 0
	reclaimed := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Deleted ") {
			continue
		}
		if strings.HasPrefix(line, "Total reclaimed space:") {
			reclaimed = strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			continue
		}
		if category == "image" {
			if strings.HasPrefix(line, "untagged:") {
				deleted++
			}
			continue
		}
		deleted++
	}
	return deleted, reclaimed
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	result := CleanupResult{}
	spaces := make([]string, 0, 5)

	if opts.Containers {
		out, err := r.runPrune(ctx, "container")
		if err != nil {
			return result, err
		}
		n, space := parsePruneOutput("container", out)
		result.ContainersDeleted = n
		if space != "" {
			spaces = append(spaces, "containers: "+space)
		}
	}
	if opts.Images {
		out, err := r.runPrune(ctx, "image")
		if err != nil {
			return result, err
		}
		n, space := parsePruneOutput("image", out)
		result.ImagesDeleted = n
		if space != "" {
			spaces = append(spaces, "images: "+space)
		}
	}
	if opts.Networks {
		out, err := r.runPrune(ctx, "network")
		if err != nil {
			return result, err
		}
		n, space := parsePruneOutput("network", out)
		result.NetworksDeleted = n
		if space != "" {
			spaces = append(spaces, "networks: "+space)
		}
	}
	if opts.Volumes {
		out, err := r.runPrune(ctx, "volume")
		if err != nil {
			return result, err
		}
		n, space := parsePruneOutput("volume", out)
		result.VolumesDeleted = n
		if space != "" {
			spaces = append(spaces, "volumes: "+space)
		}
	}
	if opts.BuildCache {
		out, err := r.runPrune(ctx, "builder")
		if err != nil {
			return result, err
		}
		n, space := parsePruneOutput("builder", out)
		result.BuildCacheDeleted = n
		if space != "" {
			spaces = append(spaces, "build cache: "+space)
		}
	}

	result.ReclaimedSpace = strings.Join(spaces, ", ")
	return result, nil
}

func (r *dockerRuntime) runPrune(ctx context.Context, category string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildPruneArgs(category)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s prune: %w\n%s", category, err, string(out))
	}
	return string(out), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run 'TestBuildPruneArgs|TestParsePruneOutput|TestStubCleanup' -v -count=1`

Expected: all PASS.

- [ ] **Step 5: Run full package tests + vet**

Run: `go test ./internal/runtime/ -count=1 && go vet ./internal/runtime/`

Expected: all PASS, no vet warnings.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement label-safe docker cleanup"
```

---

### Task 3: CLI `tengiz cleanup` command + documentation

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd` var after `rmCmd` (line 662); register command + flags in `init()`
- Test: `internal/cli/root_test.go` (append)
- Modify: `README.md` — add `### tengiz cleanup` after the rollback section (line 237)
- Modify: `AGENTS.md` — add `tengiz cleanup` to CLI list; mention `Cleanup` in the runtime architecture row

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult` from Tasks 1-2
- Produces: `cleanupCmd *cobra.Command` (`Use: "cleanup"`), flags `--containers`/`--images`/`--networks` (default true), `--volumes`/`--build-cache` (default false)

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	flags := cleanupCmd.Flags()
	for _, name := range []string{"containers", "images", "networks", "volumes", "build-cache"} {
		if flags.Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
	if v, _ := flags.GetBool("containers"); !v {
		t.Error("containers should default to true")
	}
	if v, _ := flags.GetBool("volumes"); v {
		t.Error("volumes should default to false")
	}
}

func TestCleanupNothingToDo(t *testing.T) {
	rootCmd.SetArgs([]string{
		"cleanup",
		"--containers=false", "--images=false", "--networks=false",
		"--volumes=false", "--build-cache=false",
	})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})
	if !strings.Contains(output, "nothing to clean") {
		t.Errorf("expected 'nothing to clean' in output, got: %s", output)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`

Expected: FAIL — compile error: `undefined: cleanupCmd`.

- [ ] **Step 3: Implement the command**

In `internal/cli/root.go`, after the `rmCmd` block (ends line 662), add:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to free disk space",
	Long: `Prunes stopped containers, dangling images, and unused networks that are
NOT managed by Tengiz. Resources labeled tengiz-app=* are always protected.

Use --volumes and --build-cache to enable those (potentially destructive)
categories.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		networks, _ := cmd.Flags().GetBool("networks")
		volumes, _ := cmd.Flags().GetBool("volumes")
		buildCache, _ := cmd.Flags().GetBool("build-cache")

		if !containers && !images && !networks && !volumes && !buildCache {
			fmt.Println("[tengiz] nothing to clean (no categories enabled)")
			return nil
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return err
		}

		res, err := rt.Cleanup(context.Background(), runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Networks:   networks,
			Volumes:    volumes,
			BuildCache: buildCache,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		fmt.Println("[tengiz] cleanup complete")
		fmt.Printf("  containers:  %d deleted\n", res.ContainersDeleted)
		fmt.Printf("  images:      %d deleted\n", res.ImagesDeleted)
		fmt.Printf("  networks:    %d deleted\n", res.NetworksDeleted)
		fmt.Printf("  volumes:     %d deleted\n", res.VolumesDeleted)
		fmt.Printf("  build cache: %d deleted\n", res.BuildCacheDeleted)
		if res.ReclaimedSpace != "" {
			fmt.Printf("  reclaimed:   %s\n", res.ReclaimedSpace)
		}
		return nil
	},
}
```

In `internal/cli/root.go` `init()`, after `rootCmd.AddCommand(rollbackCmd)` (line 65), add:

```go
	rootCmd.AddCommand(cleanupCmd)
```

In `internal/cli/root.go` `init()`, after the `runCmd.Flags().StringArrayP(...)` line (line 78), add:

```go
	cleanupCmd.Flags().Bool("containers", true, "prune stopped containers not managed by tengiz")
	cleanupCmd.Flags().Bool("images", true, "prune dangling images")
	cleanupCmd.Flags().Bool("networks", true, "prune unused networks not managed by tengiz")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes (CAUTION: may remove app data)")
	cleanupCmd.Flags().Bool("build-cache", false, "prune BuildKit build cache")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`

Expected: all PASS. Also run `go build ./...` to confirm the binary compiles.

- [ ] **Step 5: Update documentation**

In `README.md`, after the `tengiz rollback` section (line 237) and before `### tengiz domain`, insert:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to free disk space on single-server deployments.

| Flag | Description | Default |
|------|-------------|---------|
| `--containers` | Prune stopped containers not managed by Tengiz | `true` |
| `--images` | Prune dangling images | `true` |
| `--networks` | Prune unused networks not managed by Tengiz | `true` |
| `--volumes` | Prune unused volumes (CAUTION: may remove app data) | `false` |
| `--build-cache` | Prune BuildKit build cache | `false` |

Resources labeled `tengiz-app=*` are always protected and never pruned. Tagged
rollback images (`tengiz-apps/*`) are already trimmed to the last 5 on every
deploy. Run `tengiz cleanup` periodically (e.g. via cron) to prevent disk
exhaustion.
```

In `AGENTS.md`, in the CLI section, after the `tengiz stop/start/rm  → lifecycle` line, add:

```
tengiz cleanup        → prune unused Docker resources (label-safe; keeps tengiz-app containers)
```

In `AGENTS.md`, in the Key architecture table `runtime.Manager` row, change:

```
| `runtime.Manager` | Interface for container lifecycle. `NewDocker()` = exec-based impl, `NewStub()` = test mock. Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup. `ContainerName(name, env)` helper. |
```

to:

```
| `runtime.Manager` | Interface for container lifecycle. `NewDocker()` = exec-based impl, `NewStub()` = test mock. Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages`, `Cleanup` for rollback + image cleanup + housekeeping. `ContainerName(name, env)` helper. |
```

- [ ] **Step 6: Run full build, vet, and test suite**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: all pass. (Proxy tests are slow — allow time. All new tests run without Docker installed.)

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go README.md AGENTS.md
git commit -m "feat(cli): add tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage (FUTURES_FEATURES.md #6):**
- "Label-based docker system prune" → Task 2 (`--filter label!=tengiz-app` on container/network prunes; `dangling=true` on images).
- "`tengiz cleanup`" → Task 3 CLI command.
- "kullanılmayan volume, network, container ve image'leri temizleme" → Task 2 categories: containers, images, networks (default), volumes + build cache (opt-in).
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → Global Constraints + Task 2 filters.
- Periodic `DockerCleanupJob` → explicitly out of scope (deferred to #57 Background Monitoring Scheduler); noted in Scope section.

**2. Placeholder scan:** No TBD/TODO/implement-later placeholders; every code step contains complete, compilable code with exact file/line anchors; all test code is fully written.

**3. Type consistency:** `CleanupOptions`/`CleanupResult` defined once in Task 1 and used identically in Tasks 2 and 3. `buildPruneArgs(category string) []string`, `parsePruneOutput(category, output string) (int, string)`, `runPrune(ctx context.Context, category string) (string, error)` all match between Task 2's implementation and Task 2's tests. `cleanupCmd` name consistent across Task 3 test and implementation. Flag names (`containers`, `images`, `networks`, `volumes`, `build-cache`) consistent between registration and RunE reads and the tests.
