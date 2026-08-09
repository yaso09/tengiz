# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes abandoned Docker resources (stopped non-Tengiz containers, dangling images, unused networks/volumes) while protecting every Tengiz-managed resource via the `tengiz-app` label, and retains a configurable number of old images per app.

**Architecture:** `runtime.Manager` gains a `Prune` method. `dockerRuntime.Prune` (1) discovers candidates with read-only, label-filtered `docker` list commands, (2) deletes the discovered containers and dangling images with `docker rm -f` / `docker rmi -f`, (3) runs label-protected `docker network prune` / `docker volume prune`, and (4) reuses the existing `KeepLastNImages` for per-app image retention. A new Cobra command in `internal/cli/cleanup.go` wires app names from `config.Store.ListApps()`, plus `--dry-run` and `--keep` flags, to `Prune` and prints a housekeeping report. Dry-run performs no mutating commands at all.

**Tech Stack:** Go 1.26, Cobra CLI, Docker CLI via `os/exec` (no Docker SDK), existing `config.Store`, existing `runtime.Manager` + `KeepLastNImages`.

## Global Constraints

- Single Go module `github.com/yaso09/tengiz`, Go 1.26
- No Docker SDK — every Docker call goes through `os/exec` → `docker` CLI
- **Label protection rule:** Tengiz-managed containers carry label `tengiz-app=<appname>` (see `docker.go` constants `labelKey = "tengiz-app"`). Cleanup must NEVER remove a container/network/volume with that label, and must never remove an image referenced by any container
- All destructive prune filters use the value `label!=tengiz-app`
- Image tags follow `tengiz-apps/<name>:<env>-<deploymentID>` and `tengiz-apps/<name>:<env>-latest` (see `builder.go:61,84`); `KeepLastNImages` already filters by `reference=tengiz-apps/<name>:*`
- `--env` is a persistent root flag; env-scoped store is created via `config.NewStoreWithEnv(dataDir, env)` and read via `getEnv(cmd)` (both already exist in `internal/cli/root.go`)
- Cleanup operates on the whole Docker daemon, not per-environment; the `--env` flag only selects which apps' old images get retained
- No new external dependencies
- Every task ends with a passing `go build ./...`, `go test ./... -v -count=1`, and `go vet ./...`
- Work on branch `feat/docker-housekeeping` (repo rule: create branch for new features)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | Add `PruneReport` struct |
| `internal/runtime/prune.go` | **New.** `PruneOptions`, pure output parsers, `dockerRuntime.Prune` implementation |
| `internal/runtime/runtime.go` | Add `Prune` to `Manager` interface + no-op on `stubManager` |
| `internal/runtime/prune_test.go` | **New.** Tests for stub `Prune`, parsers, and fake-docker integration |
| `internal/runtime/testdata/fake-docker` | **New.** Fake `docker` CLI script used by integration tests |
| `internal/cli/cleanup.go` | **New.** `tengiz cleanup` Cobra command + registration |
| `internal/cli/cleanup_test.go` | **New.** CLI command registration + flag tests |
| `internal/cli/root_test.go` | Add `Prune` no-op to `mockRTForDeploy` (keeps it a valid `Manager`) |
| `internal/proxy/proxy_test.go` | Add `Prune` no-op to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Prune` no-op to `mockRuntime` |
| `README.md` | Document the `cleanup` command + feature bullet |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping ✅ Implemented |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI reference block |

Adding `Prune` to the `Manager` interface breaks compilation of every existing mock, so Tasks 1–2 update all four mocks in lockstep with the interface change.

---

### Task 1: Create branch + add `Prune` to the `Manager` interface

**Files:**
- Modify: `internal/types/types.go` (append `PruneReport`)
- Modify: `internal/runtime/runtime.go:31-49` (interface), `:113-122` (stub)
- Modify: `internal/cli/root_test.go:98-100` (mockRTForDeploy)
- Modify: `internal/proxy/proxy_test.go:33-35` (mockRuntime)
- Modify: `internal/idle/idle_test.go:32-34` (mockRuntime)
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `types.PruneReport struct { Containers []string; DanglingImages []string }`
  - `runtime.PruneOptions struct { DryRun bool; AppNames []string; KeepImages int }`
  - `Manager.Prune(ctx context.Context, opts PruneOptions) (types.PruneReport, error)`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing test**

Create `internal/runtime/prune_test.go`:

```go
package runtime

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	rep, err := m.Prune(context.Background(), PruneOptions{
		DryRun:     true,
		AppNames:   []string{"testapp"},
		KeepImages: 5,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(rep.Containers) != 0 {
		t.Errorf("Containers = %v, want empty", rep.Containers)
	}
	if len(rep.DanglingImages) != 0 {
		t.Errorf("DanglingImages = %v, want empty", rep.DanglingImages)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`

Expected: FAIL with `undefined: PruneOptions` / `runtime.Manager does not implement ... Prune`.

- [ ] **Step 4: Add `PruneReport` to `internal/types/types.go`**

Append at the end of the file:

```go
type PruneReport struct {
	Containers     []string `json:"containers"`
	DanglingImages []string `json:"dangling_images"`
}
```

- [ ] **Step 5: Add `PruneOptions` + interface method + stub in `internal/runtime`**

Create `internal/runtime/prune.go`:

```go
package runtime

import (
	"context"

	"github.com/yaso09/tengiz/internal/types"
)

type PruneOptions struct {
	DryRun     bool
	AppNames   []string
	KeepImages int
}
```

In `internal/runtime/runtime.go`, add the method to the `Manager` interface (after `KeepLastNImages`):

```go
	Prune(ctx context.Context, opts PruneOptions) (types.PruneReport, error)
```

And add the stub implementation (after `KeepLastNImages`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (types.PruneReport, error) {
	return types.PruneReport{}, nil
}
```

- [ ] **Step 6: Update the three existing mocks so they still implement `Manager`**

In `internal/cli/root_test.go` (after line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (types.PruneReport, error) { return types.PruneReport{}, nil }
```

In `internal/proxy/proxy_test.go` (after line 34):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (types.PruneReport, error) { return types.PruneReport{}, nil }
```

In `internal/idle/idle_test.go` (after line 33):

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (types.PruneReport, error) { return types.PruneReport{}, nil }
```

The mock files already import `runtime` and `types` (verify: `internal/proxy/proxy_test.go` imports both; `internal/idle/idle_test.go` imports both; `internal/cli/root_test.go` imports both). If a file does not import `types`, add it.

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/proxy/... ./internal/idle/... -v -count=1`

Expected: PASS (TestStubPrune passes, all existing tests still compile and pass).

Run: `go vet ./...`
Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add internal/types/types.go internal/runtime/runtime.go internal/runtime/prune.go internal/runtime/prune_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): add Prune method to Manager interface"
```

---

### Task 2: Pure parsers for Docker list output

**Files:**
- Modify: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `parseListLines(out string) []string` — splits a command's output into non-empty, trimmed lines. Used by `dockerRuntime.Prune` in Task 3.

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/prune_test.go`:

```go
func TestParseListLines(t *testing.T) {
	out := "orphan-app\n\n  old-worker  \n"
	got := parseListLines(out)
	want := []string{"orphan-app", "old-worker"}
	if len(got) != len(want) {
		t.Fatalf("parseListLines() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseListLines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseListLinesEmpty(t *testing.T) {
	if got := parseListLines(""); len(got) != 0 {
		t.Errorf("parseListLines(\"\") = %v, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestParseListLines -v -count=1`

Expected: FAIL with `undefined: parseListLines`.

- [ ] **Step 3: Implement `parseListLines`**

Append to `internal/runtime/prune.go`:

```go
func parseListLines(out string) []string {
	var items []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			items = append(items, l)
		}
	}
	return items
}
```

Update the imports of `internal/runtime/prune.go` to add `"strings"`:

```go
import (
	"context"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add prune output line parser"
```

---

### Task 3: Implement `dockerRuntime.Prune` with fake-docker integration tests

**Files:**
- Modify: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`
- Create: `internal/runtime/testdata/fake-docker`

**Interfaces:**
- Consumes: `parseListLines`, `PruneOptions`, `KeepLastNImages`
- Produces: `dockerRuntime.Prune(ctx context.Context, opts PruneOptions) (types.PruneReport, error)` — the full housekeeping routine used by the CLI in Task 4.

- [ ] **Step 1: Write the failing tests**

Create the fake Docker CLI at `internal/runtime/testdata/fake-docker` (make sure the file has Unix line endings):

```sh
#!/bin/sh
# Fake docker CLI for prune integration tests.
# Logs every invocation's arguments to $FAKE_DOCKER_LOG and returns canned
# listings for the read-only discovery commands.
echo "$@" >> "$FAKE_DOCKER_LOG"
case "$1" in
  ps)
    echo "orphan-app"
    echo "old-worker"
    ;;
  images)
    echo "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    echo "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    ;;
  rm|rmi)
    :
    ;;
  *)
    :
    ;;
esac
exit 0
```

Append to `internal/runtime/prune_test.go`:

```go
func setupFakeDocker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	if err := os.WriteFile(script, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	logFile := filepath.Join(dir, "log")
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("FAKE_DOCKER_LOG", logFile)
	return logFile
}

func TestDockerPruneDryRun(t *testing.T) {
	logFile := setupFakeDocker(t)
	rt := &dockerRuntime{}

	rep, err := rt.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune(dry run) error = %v", err)
	}
	if len(rep.Containers) != 2 {
		t.Errorf("Containers = %v, want 2", rep.Containers)
	}
	if len(rep.DanglingImages) != 2 {
		t.Errorf("DanglingImages = %v, want 2", rep.DanglingImages)
	}

	log, _ := os.ReadFile(logFile)
	if strings.Contains(string(log), "rm -f") {
		t.Errorf("dry run executed destructive docker rm: %s", log)
	}
	if strings.Contains(string(log), "prune") {
		t.Errorf("dry run executed docker prune: %s", log)
	}
}

func TestDockerPruneApplies(t *testing.T) {
	logFile := setupFakeDocker(t)
	rt := &dockerRuntime{}

	rep, err := rt.Prune(context.Background(), PruneOptions{
		AppNames:   []string{"myapp"},
		KeepImages: 3,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(rep.Containers) != 2 {
		t.Errorf("Containers = %v, want 2", rep.Containers)
	}

	log, _ := os.ReadFile(logFile)
	for _, want := range []string{"rm -f orphan-app", "rm -f old-worker", "network prune", "volume prune"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("log missing %q, got:\n%s", want, log)
		}
	}
}
```

Add the `fakeDockerScript` constant and the new imports (`os`, `path/filepath`) to `internal/runtime/prune_test.go`:

```go
const fakeDockerScript = `#!/bin/sh
echo "$@" >> "$FAKE_DOCKER_LOG"
case "$1" in
  ps)
    echo "orphan-app"
    echo "old-worker"
    ;;
  images)
    echo "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    echo "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    ;;
  rm|rmi)
    :
    ;;
  *)
    :
    ;;
esac
exit 0
`
```

The imports at the top of `prune_test.go` must become:

```go
import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run TestDockerPrune -v -count=1`

Expected: FAIL with `cannot use &dockerRuntime{} ... (missing method Prune)`.

- [ ] **Step 3: Implement `dockerRuntime.Prune`**

Replace the contents of `internal/runtime/prune.go` with:

```go
package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

const foreignLabel = "label!=tengiz-app"

type PruneOptions struct {
	DryRun     bool
	AppNames   []string
	KeepImages int
}

func parseListLines(out string) []string {
	var items []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			items = append(items, l)
		}
	}
	return items
}

func (r *dockerRuntime) listExitedForeignContainers(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "status=exited",
		"--filter", foreignLabel,
		"--format", "{{.Names}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return parseListLines(string(out)), nil
}

func (r *dockerRuntime) listDanglingImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "-a",
		"--filter", "dangling=true",
		"--format", "{{.ID}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return parseListLines(string(out)), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (types.PruneReport, error) {
	var rep types.PruneReport

	containers, err := r.listExitedForeignContainers(ctx)
	if err != nil {
		return rep, err
	}
	rep.Containers = containers

	images, err := r.listDanglingImages(ctx)
	if err != nil {
		return rep, err
	}
	rep.DanglingImages = images

	if opts.DryRun {
		return rep, nil
	}

	for _, c := range containers {
		cmd := exec.CommandContext(ctx, "docker", "rm", "-f", c)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[tengiz] failed to remove container %s: %v\n%s", c, err, string(out))
		}
	}

	for _, id := range images {
		cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", id)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[tengiz] failed to remove dangling image %s: %v\n%s", id, err, string(out))
		}
	}

	netCmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f", "--filter", foreignLabel)
	if out, err := netCmd.CombinedOutput(); err != nil {
		log.Printf("[tengiz] docker network prune: %v\n%s", err, string(out))
	}

	volCmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f", "--filter", foreignLabel)
	if out, err := volCmd.CombinedOutput(); err != nil {
		log.Printf("[tengiz] docker volume prune: %v\n%s", err, string(out))
	}

	if opts.KeepImages > 0 {
		for _, app := range opts.AppNames {
			if err := r.KeepLastNImages(ctx, app, opts.KeepImages); err != nil {
				log.Printf("[tengiz] failed to keep last %d images for app %s: %v", opts.KeepImages, app, err)
			}
		}
	}

	return rep, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: PASS (both fake-docker tests, parser tests, stub tests).

Run: `go vet ./...`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go internal/runtime/testdata/fake-docker
git commit -m "feat(runtime): implement label-protected docker housekeeping Prune"
```

---

### Task 4: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go` (nothing needed — registration happens in `cleanup.go`'s own `init()`, mirroring `preview.go`)

**Interfaces:**
- Consumes: `runtime.PruneOptions{DryRun, AppNames, KeepImages}`, `types.PruneReport`, `config.NewStoreWithEnv(dataDir, env).ListApps()`, `getEnv(cmd)`
- Produces: `cleanupCmd *cobra.Command` registered as `tengiz cleanup` with `--dry-run` and `--keep` flags

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	if cleanupCmd.Flags().Lookup("dry-run") == nil {
		t.Error("cleanup missing --dry-run flag")
	}
	if cleanupCmd.Flags().Lookup("keep") == nil {
		t.Error("cleanup missing --keep flag")
	}
}

func TestCleanupFlagsParsed(t *testing.T) {
	var called bool
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()

	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		dry, _ := cmd.Flags().GetBool("dry-run")
		keep, _ := cmd.Flags().GetInt("keep")
		if !dry {
			t.Errorf("dry-run = false, want true")
		}
		if keep != 3 {
			t.Errorf("keep = %d, want 3", keep)
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--keep", "3"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("cleanupCmd.RunE was not called")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL with `cleanup command not registered` (command does not exist yet).

- [ ] **Step 3: Implement the command**

Create `internal/cli/cleanup.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources (housekeeping)",
	Long: `Removes stopped containers and dangling images that are NOT managed by Tengiz
(containers labeled tengiz-app are always protected), prunes unused networks and
volumes that are not labeled tengiz-app, and keeps the last N Docker images per
deployed app.

Use --dry-run to preview what would be removed without deleting anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		keep, _ := cmd.Flags().GetInt("keep")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		var appNames []string
		store := config.NewStoreWithEnv(dataDir, env)
		if apps, listErr := store.ListApps(); listErr == nil {
			for _, a := range apps {
				appNames = append(appNames, a.Config.Name)
			}
		}

		rep, err := rt.Prune(cmd.Context(), runtime.PruneOptions{
			DryRun:     dryRun,
			AppNames:   appNames,
			KeepImages: keep,
		})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		mode := "removed"
		if dryRun {
			mode = "would remove"
		}

		fmt.Printf("[tengiz] stopped non-managed containers %s: %d\n", mode, len(rep.Containers))
		for _, c := range rep.Containers {
			fmt.Printf("  - %s\n", c)
		}
		fmt.Printf("[tengiz] dangling images %s: %d\n", mode, len(rep.DanglingImages))
		for _, id := range rep.DanglingImages {
			fmt.Printf("  - %s\n", id)
		}
		if dryRun {
			fmt.Println("[tengiz] skipped: network/volume prune and per-app image retention (dry run)")
		} else {
			fmt.Println("[tengiz] pruned unused networks and volumes (tengiz-app labels protected)")
			fmt.Printf("[tengiz] kept last %d images per app\n", keep)
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "preview what would be removed without deleting anything")
	cleanupCmd.Flags().Int("keep", 5, "number of old Docker images to keep per app")
	rootCmd.AddCommand(cleanupCmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS.

Run: `go build ./...`
Expected: no errors.

Run: `go vet ./...`
Expected: no output.

- [ ] **Step 5: Verify end-to-end with the real CLI (manual smoke test)**

With Docker available, run the dry-run then the real cleanup against an environment with at least one deployed app:

```bash
go build -o tengiz .
./tengiz cleanup --dry-run
./tengiz cleanup --keep 3
```

Expected: a report listing stopped non-managed containers and dangling images, then a real cleanup summary that never mentions a container whose name starts with `tengiz-`.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command for docker housekeeping"
```

---

### Task 5: Documentation and feature tracking

**Files:**
- Modify: `README.md`
- Modify: `docs/FUTURES_FEATURES.md`
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: `tengiz cleanup` command from Task 4
- Produces: no code — user-facing docs and feature status update

- [ ] **Step 1: Document `tengiz cleanup` in README.md**

Add a feature bullet under the `## Features` list (after the deployment-history bullet near line 20):

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes stopped non-managed containers, dangling images, and unused networks/volumes while protecting all Tengiz-managed resources and keeping the last N images per app.
```

Add a new command section in the CLI Reference after `### \`tengiz rm <app>\`` (line ~222):

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space. Stopped containers and dangling images that are NOT managed by Tengiz (no `tengiz-app` label) are removed, unused networks and volumes are pruned, and the last N Docker images per deployed app are retained for rollback.

| Flag | Default | Description |
|------|---------|-------------|
| `--dry-run` | `false` | Preview what would be removed without deleting anything |
| `--keep N` | `5` | Number of old Docker images to keep per app |

Tengiz-managed containers (labeled `tengiz-app`) are always protected — including stopped containers needed for scale-to-zero cold starts.
```

- [ ] **Step 2: Mark feature #6 implemented in FUTURES_FEATURES.md**

In `docs/FUTURES_FEATURES.md` line 19, change the status of feature #6:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 3: Update AGENTS.md CLI reference**

In `AGENTS.md`, add to the CLI block (after `tengiz rollback <app>`):

```
tengiz cleanup [-d] [--keep N] → prune unused docker resources (housekeeping)
```

- [ ] **Step 4: Verify nothing broke and commit**

Run: `go build ./... && go test ./... -v -count=1 && go vet ./...`

Expected: all PASS.

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage.** Feature #6 in `docs/FUTURES_FEATURES.md` (P0, "Docker Housekeeping"): "Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`." Task 3 implements the label-protected prune (`label!=tengiz-app` filters on containers, networks, volumes; dangling-only image pruning; per-app retention via existing `KeepLastNImages`). Task 4 adds the `tengiz cleanup` CLI command with `--dry-run` and `--keep`. Task 5 documents it. The detailed description's requirement — "kullanılmayan volume, network, container ve image'leri periyodik temizleme" (periodic cleanup of unused volumes, networks, containers, images) — is satisfied by the prune commands. Periodic scheduling is deliberately out of scope (not part of #6's rationale; the table lists only the command), and build-cache/`--gc` handling is a separate feature (#103).

**2. Placeholder scan.** Every code step contains full, compilable code. No "TBD", no "handle edge cases" prose without code, no "similar to Task N" references. The only prose-only step is the Task 4 smoke test (an interactive manual verification), which shows the exact commands to run.

**3. Type consistency.**
- `runtime.PruneOptions` fields: `DryRun bool`, `AppNames []string`, `KeepImages int` — identical in Task 1 (test + stub), Task 3 (docker impl), Task 4 (CLI construction). ✓
- `types.PruneReport` fields: `Containers []string`, `DanglingImages []string` — identical in Task 1, Task 3, Task 4. ✓
- `Manager.Prune(ctx context.Context, opts PruneOptions) (types.PruneReport, error)` — same signature in interface, stub, three mocks, and docker impl. ✓
- Mock method names all `Prune`, none `Cleanup`/`Housekeeping`. ✓
- `parseListLines(out string) []string` — same in Task 2 and Task 3. ✓
- Image tag reference `tengiz-apps/<name>:*` reused verbatim from the existing `KeepLastNImages`; no new naming invented. ✓
