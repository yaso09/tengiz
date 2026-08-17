# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning unused Docker resources (stopped containers, dangling images, unused volumes, unused networks, build cache) while protecting all Tengiz-managed containers via label-based filtering.

**Architecture:** A new `runtime.Manager.Cleanup(ctx, opts)` method runs label-protected Docker prune subcommands (`docker container prune --filter label!=tengiz-app`, `docker volume prune --filter label!=tengiz-app`, `docker network prune --filter label!=tengiz-app`, `docker image prune -f`, `docker builder prune -f`). The `tengiz cleanup` CLI command maps `--containers/--images/--volumes/--networks/--cache` flags to a `CleanupOptions` struct; with no flags it cleans every category. The pure command-builder function `cleanupCommands(opts)` is unit-tested without Docker; the exec-based runtime and stub both satisfy the new interface method.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (Docker CLI), existing `runtime.Manager` interface, existing label conventions (`tengiz-app=<name>`, `tengiz-env=<env>`).

## Global Constraints

- Tengiz-managed containers are labeled `tengiz-app=<appname>`; they MUST NEVER be pruned (scale-to-zero cold starts depend on stopped Tengiz containers surviving)
- Container/volume/network prune commands MUST include `--filter label!=tengiz-app`
- Image prune is `docker image prune -f` (dangling images ONLY — never `-a`, which would delete tagged `tengiz-apps/<app>:<env>-<deploymentID>` rollback images)
- No new external Go dependencies
- `tengiz cleanup` with NO category flags = clean all categories
- `tengiz cleanup --containers` (or any single flag) = clean ONLY the explicitly-flagged categories
- All category flag defaults are `false`; "clean all" is detected via `cmd.Flags().Changed()`
- The `Cleanup` interface method must be added to all mock implementations (`stubManager`, `mockRTForDeploy` in `internal/cli/root_test.go`, `mockRuntime` in `internal/proxy/proxy_test.go`) in the same commit as the interface change so the build stays green
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | Add `CleanupOptions`, `CleanupResult`, pure helper `cleanupCommands(opts)`, and `dockerRuntime.Cleanup` |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager.Cleanup` no-op |
| `internal/runtime/cleanup_test.go` | Tests for `cleanupCommands` label protection + stub `Cleanup` |
| `internal/cli/cleanup.go` | New file: `tengiz cleanup` Cobra command |
| `internal/cli/root.go` | Register `cleanupCmd` + its 5 boolean flags in `init()` |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy`; add registration test |
| `internal/proxy/proxy_test.go` | Add `Cleanup` method to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` command |
| `AGENTS.md` | Add `tengiz cleanup` to CLI list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 as ✅ Implemented |

---

### Task 1: Add `Cleanup` to the runtime Manager interface (types + stub + mocks)

**Files:**
- Modify: `internal/runtime/cleanup.go:1-10` — add `CleanupOptions`, `CleanupResult`
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface
- Modify: `internal/runtime/runtime.go:113-122` — add `stubManager.Cleanup`
- Modify: `internal/cli/root_test.go:69-100` — add `Cleanup` to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:15-35` — add `Cleanup` to `mockRuntime`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.CleanupOptions{Containers, Images, Volumes, Networks, BuildCache bool}`, `runtime.CleanupResult{Output string}`, `runtime.Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	result, err := m.Cleanup(context.Background(), CleanupOptions{Containers: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.Output != "" {
		t.Errorf("Cleanup() Output = %q, want empty", result.Output)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: FAIL with compile error `m.Cleanup undefined (type Manager has no field or method Cleanup)`

- [ ] **Step 3: Add the types + interface method + stub**

In `internal/runtime/cleanup.go`, add these two type declarations (before the existing `func (r *dockerRuntime) RemoveImage`):

```go
type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type CleanupResult struct {
	Output string
}
```

In `internal/runtime/runtime.go`, add this line to the `Manager` interface (after the `KeepLastNImages` line):

```go
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

In `internal/runtime/runtime.go`, add this stub method (after the existing `KeepLastNImages` stub):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
}
```

In `internal/cli/root_test.go`, add this method to `mockRTForDeploy` (after the existing `KeepLastNImages` mock):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

In `internal/proxy/proxy_test.go`, add this method to `mockRuntime` (after the existing `KeepLastNImages` mock):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 4: Run all tests to verify they pass**

Run: `go test ./... -count=1`

Expected: All PASS (the two proxy TCP-dial tests and idle time-sensitive tests may be flaky per AGENTS.md — not caused by this change)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Cleanup method to Manager interface"
```

---

### Task 2: Implement `cleanupCommands` + `dockerRuntime.Cleanup`

**Files:**
- Modify: `internal/runtime/cleanup.go:1-10` — add `cleanupCommands` + `dockerRuntime.Cleanup`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.CleanupOptions` (from Task 1)
- Produces: `func cleanupCommands(opts CleanupOptions) [][]string`, `func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestCleanupCommands(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want int
	}{
		{"all", CleanupOptions{Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}, 5},
		{"containers only", CleanupOptions{Containers: true}, 1},
		{"images only", CleanupOptions{Images: true}, 1},
		{"volumes only", CleanupOptions{Volumes: true}, 1},
		{"networks only", CleanupOptions{Networks: true}, 1},
		{"build cache only", CleanupOptions{BuildCache: true}, 1},
		{"none", CleanupOptions{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanupCommands(tt.opts)
			if len(got) != tt.want {
				t.Fatalf("cleanupCommands() = %d commands, want %d", len(got), tt.want)
			}
		})
	}
}

func TestCleanupCommandsProtectTengiz(t *testing.T) {
	cmds := cleanupCommands(CleanupOptions{Containers: true, Volumes: true, Networks: true})
	if len(cmds) != 3 {
		t.Fatalf("cleanupCommands() = %d commands, want 3", len(cmds))
	}
	for _, c := range cmds {
		found := false
		for _, arg := range c {
			if arg == "label!=tengiz-app" {
				found = true
			}
		}
		if !found {
			t.Errorf("command %v missing label!=tengiz-app protection", c)
		}
	}
}

func TestCleanupCommandsImagePruneDanglingOnly(t *testing.T) {
	cmds := cleanupCommands(CleanupOptions{Images: true})
	if len(cmds) != 1 {
		t.Fatalf("cleanupCommands() = %d commands, want 1", len(cmds))
	}
	cmd := cmds[0]
	if cmd[0] != "image" || cmd[1] != "prune" {
		t.Errorf("expected image prune command, got %v", cmd)
	}
	for _, arg := range cmd {
		if arg == "-a" || arg == "--all" {
			t.Errorf("image prune must NOT use -a (would delete tagged rollback images), got %v", cmd)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestCleanupCommands" -v -count=1`

Expected: FAIL with compile error `undefined: cleanupCommands`

- [ ] **Step 3: Write the minimal implementation**

Append to `internal/runtime/cleanup.go` (after the type declarations from Task 1):

```go
func cleanupCommands(opts CleanupOptions) [][]string {
	var cmds [][]string
	if opts.Containers {
		cmds = append(cmds, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.Images {
		cmds = append(cmds, []string{"image", "prune", "-f"})
	}
	if opts.Volumes {
		cmds = append(cmds, []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.Networks {
		cmds = append(cmds, []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"})
	}
	if opts.BuildCache {
		cmds = append(cmds, []string{"builder", "prune", "-f"})
	}
	return cmds
}

func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	var result CleanupResult
	for _, args := range cleanupCommands(opts) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return result, fmt.Errorf("docker %s: %w\n%s", strings.Join(args[:2], " "), err, string(out))
		}
		result.Output += string(out)
	}
	return result, nil
}
```

Note: `context`, `fmt`, `os/exec`, and `strings` are already imported in `internal/runtime/cleanup.go` — no import changes needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestCleanupCommands|TestStubCleanup" -v -count=1`

Expected: ALL PASS (dockerRuntime.Cleanup is not exec-tested here; the pure command-builder is)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat(runtime): implement label-protected docker cleanup"
```

---

### Task 3: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:65` — register `cleanupCmd` in `init()`
- Modify: `internal/cli/root.go:76-88` — add category flags in `init()`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.CleanupOptions`, `runtime.CleanupResult` (Tasks 1-2)
- Produces: `var cleanupCmd *cobra.Command` (registered on `rootCmd`)

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"containers", "images", "volumes", "networks", "cache"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run TestCleanupCommandRegistered -v -count=1`

Expected: FAIL with `cleanup command not registered`

- [ ] **Step 3: Write the minimal implementation**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources to reclaim disk space",
	Long: `Prune Docker resources not managed by Tengiz to reclaim disk space.

Tengiz-managed containers (labeled tengiz-app=*) are always protected.
Prunes stopped non-Tengiz containers, dangling images, unused volumes
and networks, and the build cache.

With no flags, every category is cleaned. Pass one or more of
--containers, --images, --volumes, --networks, --cache to clean only
those categories.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		all := !cmd.Flags().Changed("containers") &&
			!cmd.Flags().Changed("images") &&
			!cmd.Flags().Changed("volumes") &&
			!cmd.Flags().Changed("networks") &&
			!cmd.Flags().Changed("cache")

		opts := runtime.CleanupOptions{
			Containers: all,
			Images:     all,
			Volumes:    all,
			Networks:   all,
			BuildCache: all,
		}
		if cmd.Flags().Changed("containers") {
			opts.Containers, _ = cmd.Flags().GetBool("containers")
		}
		if cmd.Flags().Changed("images") {
			opts.Images, _ = cmd.Flags().GetBool("images")
		}
		if cmd.Flags().Changed("volumes") {
			opts.Volumes, _ = cmd.Flags().GetBool("volumes")
		}
		if cmd.Flags().Changed("networks") {
			opts.Networks, _ = cmd.Flags().GetBool("networks")
		}
		if cmd.Flags().Changed("cache") {
			opts.BuildCache, _ = cmd.Flags().GetBool("cache")
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}
		fmt.Print(result.Output)
		return nil
	},
}
```

In `internal/cli/root.go` `init()`, add the registration (after `rootCmd.AddCommand(rollbackCmd)`):

```go
	rootCmd.AddCommand(cleanupCmd)
```

In `internal/cli/root.go` `init()`, add the flags (after the `webhookCmd.Flags()` block):

```go
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("cache", false, "prune Docker build cache")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run TestCleanupCommandRegistered -v -count=1`

Expected: PASS

- [ ] **Step 5: Verify the CLI builds and check help output**

Run: `go build -o tengiz . && ./tengiz cleanup --help`

Expected: Build succeeds; help shows `cleanup` with the 5 boolean flags.

Run: `./tengiz cleanup` (if Docker is not installed)

Expected: `Error: docker: docker not found in PATH: ...` (acceptable — Docker must be installed separately per AGENTS.md)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 4: Documentation + final verification

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md:36-65` — add to CLI list
- Modify: `docs/FUTURES_FEATURES.md:19` — mark feature #6 as implemented

- [ ] **Step 1: Update README.md**

Add a `### tengiz cleanup` section after the `### tengiz ps` section:

```markdown
### `tengiz cleanup`

Reclaim disk space by pruning Docker resources not managed by Tengiz. Tengiz-managed containers (labeled `tengiz-app=*`) are always protected. With no flags, every category is cleaned.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--cache` | Prune Docker build cache |

Examples:
```
tengiz cleanup                  # clean everything
tengiz cleanup --containers     # clean only stopped non-Tengiz containers
tengiz cleanup --images --cache # clean only dangling images and build cache
```
```

- [ ] **Step 2: Update AGENTS.md CLI list**

Add after the `tengiz rollback <app>` line:

```
tengiz cleanup              → prune unused Docker resources (Tengiz containers protected via label filter)
```

- [ ] **Step 3: Mark feature #6 as implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md`, change the feature #6 row's status from ⬜ to ✅:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add a row to the `✅ Implemented Features (Not Pending)` table:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-17) |
```

- [ ] **Step 4: Run full verification**

Run: `go build -o tengiz .`

Expected: Build succeeds.

Run: `go vet ./...`

Expected: No issues.

Run: `go test ./... -v -count=1`

Expected: All PASS. (Proxy TCP-dial tests and idle time-sensitive tests may be flaky per AGENTS.md; re-run those packages if they fail without a code change.)

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark feature implemented"
```

---

## Self-Review

**1. Spec coverage** (from `docs/FUTURES_FEATURES.md` feature #6):
- `tengiz cleanup` command ✅ (Task 3)
- Label-based `docker system prune` equivalent with `--filter label!=tengiz-app` ✅ (Task 2 — `cleanupCommands`)
- Clean unused volumes, networks, containers, images ✅ (Tasks 2-3 — all 5 categories exposed as flags)
- Label-based filtering protects Tengiz-managed containers ✅ (Task 2 — `label!=tengiz-app` on container/volume/network prune)
- Rollback images protected (no `-a` on image prune) ✅ (Task 2 + dedicated test)

**2. Placeholder scan:** Every step has complete code. No "TBD"/"TODO"/"similar to Task N" patterns.

**3. Type consistency:** `CleanupOptions{Containers, Images, Volumes, Networks, BuildCache bool}` and `CleanupResult{Output string}` are defined once in Task 1 and referenced identically in Tasks 2-3. `cleanupCommands(opts CleanupOptions) [][]string` signature is identical between its test (Task 2 Step 1) and its implementation (Task 2 Step 3). CLI flag names (`containers`, `images`, `volumes`, `networks`, `cache`) match both the flags registered in Task 3 and the `CleanupOptions` fields.

**4. Mock coverage:** Every `runtime.Manager` implementation (`dockerRuntime`, `stubManager`, `mockRTForDeploy` in `internal/cli/root_test.go`, `mockRuntime` in `internal/proxy/proxy_test.go`) gets the `Cleanup` method in Task 1, keeping the whole repo compiling after the interface change.