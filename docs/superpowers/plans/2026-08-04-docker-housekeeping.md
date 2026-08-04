# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, volumes, networks) with label-based protection so Tengiz-managed containers are never removed, plus optional build-log pruning under `~/.tengiz/build-logs`.

**Architecture:** A new `runtime.Cleanup(ctx, opts)` method on the `Manager` interface runs `docker system prune -f` with the `--filter label!=tengiz-app` guard (Tengiz containers all carry the `tengiz-app` label), optional `--all`, `--volumes`, and `--until` flags, and a `--dry-run` mode that runs `docker system df` instead of pruning. Pure helper functions `buildPruneArgs()` and `parseReclaimedSpace()` keep the argument construction and output parsing unit-testable without Docker. The CLI `cleanup` command wires flags into `HousekeepingOptions` and optionally prunes stale build logs per app using the existing `config.Store.PruneBuildLogs(app, keep)`.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface, existing `config.Store`, `os/exec` (docker CLI). No new external dependencies.

## Global Constraints

- Tengiz-managed containers carry the `tengiz-app` label and must **never** be removed by `tengiz cleanup`
- Every prune invocation adds the filter `label!=tengiz-app` (via `runtime.HousekeepingProtectFilter()`)
- Default `tengiz cleanup` removes only dangling images and stopped containers (no `--all`, no `--volumes`)
- `--until` values use Docker duration syntax (`48h`, `1w`, `30d`)
- `--dry-run` must never remove anything — it runs `docker system df` only
- `--build-logs` prunes old build logs keeping the newest `--keep` per app (default `5`)
- `tengiz cleanup` must respect the global `--env` flag when pruning build logs
- Adding `Cleanup` to the `runtime.Manager` interface requires updating **all** implementations: `stubManager` (runtime.go) and the mocks in `cli/root_test.go`, `proxy/proxy_test.go`, `idle/idle_test.go`
- No new Go module dependencies
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/housekeeping.go` | **New.** `HousekeepingOptions`, `HousekeepingResult` types; `HousekeepingProtectFilter()`; `buildPruneArgs()` pure arg builder; `parseReclaimedSpace()` output parser; `(*dockerRuntime).Cleanup()` |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager` implementation |
| `internal/runtime/housekeeping_test.go` | **New.** Tests for `buildPruneArgs`, `parseReclaimedSpace`, stub `Cleanup`, and fake-docker integration tests for prune + dry-run |
| `internal/cli/root.go` | Add `cleanupCmd` + registration; add `pruneBuildLogsAllApps()` helper; flag definitions |
| `internal/cli/cleanup_test.go` | **New.** Command registration, flag presence, flag passthrough, build-log pruning test |
| `internal/cli/root_test.go` | Add `Cleanup` method to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` method to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Cleanup` method to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in the commands section |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |

---

### Task 1: Housekeeping types + pure helper functions

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: nothing new (uses existing `labelKey = "tengiz-app"` from `internal/runtime/docker.go:76`)
- Produces: `HousekeepingOptions{All bool; Volumes bool; Until string; Filters []string; DryRun bool}`, `HousekeepingResult{Output string; SpaceFreed string}`, `HousekeepingProtectFilter() string`, `buildPruneArgs(opts HousekeepingOptions) []string`, `parseReclaimedSpace(output string) string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/housekeeping_test.go
package runtime

import (
	"strings"
	"testing"
)

func TestBuildPruneArgsDefaults(t *testing.T) {
	args := buildPruneArgs(HousekeepingOptions{})
	want := "system prune -f"
	if got := strings.Join(args, " "); got != want {
		t.Errorf("buildPruneArgs() = %q, want %q", got, want)
	}
}

func TestBuildPruneArgsAllVolumes(t *testing.T) {
	args := buildPruneArgs(HousekeepingOptions{All: true, Volumes: true})
	want := "system prune -f --all --volumes"
	if got := strings.Join(args, " "); got != want {
		t.Errorf("buildPruneArgs() = %q, want %q", got, want)
	}
}

func TestBuildPruneArgsUntilAndFilters(t *testing.T) {
	args := buildPruneArgs(HousekeepingOptions{
		Until:   "48h",
		Filters: []string{HousekeepingProtectFilter()},
	})
	want := "system prune -f --filter until=48h --filter label!=tengiz-app"
	if got := strings.Join(args, " "); got != want {
		t.Errorf("buildPruneArgs() = %q, want %q", got, want)
	}
}

func TestHousekeepingProtectFilter(t *testing.T) {
	if got := HousekeepingProtectFilter(); got != "label!=tengiz-app" {
		t.Errorf("HousekeepingProtectFilter() = %q, want %q", got, "label!=tengiz-app")
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	output := "Deleted Containers: 2\nDeleted Images: 3\nTotal reclaimed space: 1.234GB\n"
	if got := parseReclaimedSpace(output); got != "1.234GB" {
		t.Errorf("parseReclaimedSpace() = %q, want %q", got, "1.234GB")
	}
}

func TestParseReclaimedSpaceEmpty(t *testing.T) {
	if got := parseReclaimedSpace("nothing to report"); got != "" {
		t.Errorf("parseReclaimedSpace() = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestHousekeepingProtectFilter|TestParseReclaimedSpace" -v -count=1`

Expected: FAIL with `undefined: buildPruneArgs`, `undefined: HousekeepingProtectFilter`, `undefined: parseReclaimedSpace`

- [ ] **Step 3: Write the implementation**

```go
// internal/runtime/housekeeping.go
package runtime

import (
	"strings"
)

// HousekeepingOptions controls what docker system prune removes.
type HousekeepingOptions struct {
	All     bool
	Volumes bool
	Until   string
	Filters []string
	DryRun  bool
}

// HousekeepingResult reports what cleanup found or removed.
type HousekeepingResult struct {
	Output     string
	SpaceFreed string
}

// HousekeepingProtectFilter returns a Docker prune filter that excludes
// resources managed by Tengiz (those carrying the tengiz-app label).
func HousekeepingProtectFilter() string {
	return "label!=" + labelKey
}

func buildPruneArgs(opts HousekeepingOptions) []string {
	args := []string{"system", "prune", "-f"}
	if opts.All {
		args = append(args, "--all")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}
	if opts.Until != "" {
		args = append(args, "--filter", "until="+opts.Until)
	}
	for _, f := range opts.Filters {
		args = append(args, "--filter", f)
	}
	return args
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestBuildPruneArgs|TestHousekeepingProtectFilter|TestParseReclaimedSpace" -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add housekeeping types and prune-arg helpers"
```

---

### Task 2: Add `Cleanup` to the Manager interface + docker runtime implementation

**Files:**
- Create: `internal/runtime/housekeeping.go` (append `Cleanup` method)
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface; `internal/runtime/runtime.go:121` — add `stubManager.Cleanup`
- Modify: `internal/cli/root_test.go:69-100` — add `Cleanup` to `mockRTForDeploy`
- Modify: `internal/proxy/proxy_test.go:15-35` — add `Cleanup` to `mockRuntime`
- Modify: `internal/idle/idle_test.go:14-34` — add `Cleanup` to `mockRuntime`
- Test: `internal/runtime/housekeeping_test.go` (append)

**Interfaces:**
- Consumes: `HousekeepingOptions`, `HousekeepingResult`, `HousekeepingProtectFilter()`, `buildPruneArgs()` from Task 1
- Produces: `Manager.Cleanup(ctx context.Context, opts HousekeepingOptions) (*HousekeepingResult, error)` on all implementations

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/housekeeping_test.go — append these test functions

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), HousekeepingOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil || res.Output != "" || res.SpaceFreed != "" {
		t.Errorf("Cleanup() result = %+v, want empty result", res)
	}
}

func TestDockerCleanupRunsSystemPrune(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	fakeDocker := filepath.Join(dir, "docker")
	script := "#!/bin/sh\n" +
		fmt.Sprintf("printf '%%s\\n' \"$*\" > %s\n", argsFile) +
		"echo 'Deleted Containers: 2'\n" +
		"echo 'Deleted Images: 3'\n" +
		"echo 'Total reclaimed space: 1.234GB'\n"
	if err := os.WriteFile(fakeDocker, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rt := &dockerRuntime{}
	res, err := rt.Cleanup(context.Background(), HousekeepingOptions{
		All:     true,
		Volumes: true,
		Until:   "48h",
		Filters: []string{HousekeepingProtectFilter()},
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.SpaceFreed != "1.234GB" {
		t.Errorf("SpaceFreed = %q, want %q", res.SpaceFreed, "1.234GB")
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "system prune -f --all --volumes --filter until=48h --filter label!=tengiz-app"
	if got := strings.TrimSpace(string(data)); got != want {
		t.Errorf("docker args = %q, want %q", got, want)
	}
}

func TestDockerCleanupDryRunRunsSystemDF(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	fakeDocker := filepath.Join(dir, "docker")
	script := "#!/bin/sh\n" +
		fmt.Sprintf("printf '%%s\\n' \"$*\" > %s\n", argsFile) +
		"echo 'TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE'\n" +
		"echo 'Images          5         2         1.2GB     800MB (66%)'\n"
	if err := os.WriteFile(fakeDocker, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rt := &dockerRuntime{}
	res, err := rt.Cleanup(context.Background(), HousekeepingOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !strings.Contains(res.Output, "RECLAIMABLE") {
		t.Errorf("Output missing df table: %q", res.Output)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "system df" {
		t.Errorf("docker args = %q, want %q", got, "system df")
	}
}
```

Add the imports `context`, `fmt`, `os`, `path/filepath` to the top of `internal/runtime/housekeeping_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestDockerCleanup" -v -count=1`

Expected: FAIL with `cannot use m (type Manager) as ... missing method Cleanup`

- [ ] **Step 3: Add `Cleanup` to the `Manager` interface in `internal/runtime/runtime.go`**

In the `Manager` interface (after the `Run` line):

```go
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	Cleanup(ctx context.Context, opts HousekeepingOptions) (*HousekeepingResult, error)
}
```

- [ ] **Step 4: Add `stubManager.Cleanup` in `internal/runtime/runtime.go`**

After the `stubManager.Run` method (end of file):

```go
func (m *stubManager) Cleanup(ctx context.Context, opts HousekeepingOptions) (*HousekeepingResult, error) {
	return &HousekeepingResult{}, nil
}
```

- [ ] **Step 5: Implement `(*dockerRuntime).Cleanup` in `internal/runtime/housekeeping.go`**

Add to the top of the file's imports `context`, `fmt`, `os/exec`, then append:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts HousekeepingOptions) (*HousekeepingResult, error) {
	if opts.DryRun {
		cmd := exec.CommandContext(ctx, "docker", "system", "df")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		return &HousekeepingResult{Output: string(out)}, nil
	}

	args := buildPruneArgs(opts)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}
	output := string(out)
	return &HousekeepingResult{
		Output:     output,
		SpaceFreed: parseReclaimedSpace(output),
	}, nil
}
```

- [ ] **Step 6: Add `Cleanup` to the three mock Manager implementations**

`internal/cli/root_test.go` (in `mockRTForDeploy`, after the `Run` method):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.HousekeepingOptions) (*runtime.HousekeepingResult, error) {
	return &runtime.HousekeepingResult{}, nil
}
```

`internal/proxy/proxy_test.go` (in `mockRuntime`, after the `Run` method):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.HousekeepingOptions) (*runtime.HousekeepingResult, error) {
	return &runtime.HousekeepingResult{}, nil
}
```

`internal/idle/idle_test.go` (in `mockRuntime`, after the `Run` method):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.HousekeepingOptions) (*runtime.HousekeepingResult, error) {
	return &runtime.HousekeepingResult{}, nil
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go build ./...`

Expected: Build succeeds

Run: `go test ./internal/runtime/... -run "TestStubCleanup|TestDockerCleanup" -v -count=1`

Expected: PASS

Run: `go test ./internal/cli/... ./internal/proxy/... ./internal/idle/... -v -count=1`

Expected: PASS (proxy tests may take ~2s each due to TCP dial timeouts)

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat: add Cleanup method to runtime.Manager and docker runtime"
```

---

### Task 3: `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go:44` — register command; after `internal/cli/root.go:662` — add `cleanupCmd` and `pruneBuildLogsAllApps`; in `init()` add flags
- Test: `internal/cli/cleanup_test.go` (new)

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.HousekeepingOptions`, `runtime.HousekeepingResult`, `runtime.HousekeepingProtectFilter()`, `config.NewStoreWithEnv(dataDir, env)`, `store.ListApps()`, `store.ListBuildLogs()`, `store.PruneBuildLogs()`, `getEnv(cmd)` from `internal/cli/root.go:97`
- Produces: `tengiz cleanup [--all] [--volumes] [--until D] [--dry-run] [--build-logs] [--keep N]` command; `pruneBuildLogsAllApps(store *config.Store, keep int) int` helper

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
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
	for _, flag := range []string{"all", "volumes", "until", "dry-run", "build-logs", "keep"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupCmdFlagsParsed(t *testing.T) {
	var captured struct {
		all       bool
		volumes   bool
		until     string
		dryRun    bool
		buildLogs bool
		keep      int
	}
	originalRunE := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = originalRunE }()
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		captured.all, _ = cmd.Flags().GetBool("all")
		captured.volumes, _ = cmd.Flags().GetBool("volumes")
		captured.until, _ = cmd.Flags().GetString("until")
		captured.dryRun, _ = cmd.Flags().GetBool("dry-run")
		captured.buildLogs, _ = cmd.Flags().GetBool("build-logs")
		captured.keep, _ = cmd.Flags().GetInt("keep")
		return nil
	}

	rootCmd.SetArgs([]string{"cleanup", "--all", "--volumes", "--until", "48h", "--dry-run", "--build-logs", "--keep", "3"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !captured.all || !captured.volumes || captured.until != "48h" || !captured.dryRun || !captured.buildLogs || captured.keep != 3 {
		t.Errorf("flags not parsed correctly: %+v", captured)
	}
}

func TestPruneBuildLogsAllApps(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)

	for _, app := range []string{"app1", "app2"} {
		store.SaveApp(types.AppEntry{Name: app, Config: types.AppConfig{Name: app}})
		for _, id := range []string{"v1", "v2", "v3"} {
			if err := store.SaveBuildLog(app, id, "log "+id); err != nil {
				t.Fatal(err)
			}
		}
	}

	removed := pruneBuildLogsAllApps(store, 2)
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	for _, app := range []string{"app1", "app2"} {
		ids, err := store.ListBuildLogs(app)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 2 {
			t.Errorf("app %s has %d logs, want 2", app, len(ids))
		}
	}
}
```

Add the import `"github.com/spf13/cobra"` to `internal/cli/cleanup_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup|TestPruneBuildLogsAllApps" -v -count=1`

Expected: FAIL with `undefined: cleanupCmd`, `undefined: pruneBuildLogsAllApps`

- [ ] **Step 3: Register the command in `internal/cli/root.go` `init()`**

After `rootCmd.AddCommand(rmCmd)`:

```go
	rootCmd.AddCommand(cleanupCmd)
```

And in `init()`, after the `logsCmd` flag definitions:

```go
	cleanupCmd.Flags().Bool("all", false, "also remove all unused images, not just dangling ones")
	cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
	cleanupCmd.Flags().String("until", "", "only remove resources created before this duration (e.g. 48h, 1w, 30d)")
	cleanupCmd.Flags().Bool("dry-run", false, "show current Docker disk usage without removing anything")
	cleanupCmd.Flags().Bool("build-logs", false, "also prune old build logs under ~/.tengiz/build-logs")
	cleanupCmd.Flags().Int("keep", 5, "number of build logs to keep per app when --build-logs is set")
```

- [ ] **Step 4: Add the `cleanupCmd` and helper in `internal/cli/root.go`**

Insert this block immediately after the `rmCmd` variable (after line 662):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources (containers, images, volumes, networks)",
	Long: "Prunes unused Docker resources while protecting Tengiz-managed containers " +
		"via the tengiz-app label. Use --all for unused images, --volumes for volumes, " +
		"and --until to limit by age. Pass --dry-run to inspect disk usage first.",
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")
		until, _ := cmd.Flags().GetString("until")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		buildLogs, _ := cmd.Flags().GetBool("build-logs")
		keep, _ := cmd.Flags().GetInt("keep")

		opts := runtime.HousekeepingOptions{
			All:     all,
			Volumes: volumes,
			Until:   until,
			DryRun:  dryRun,
			Filters: []string{runtime.HousekeepingProtectFilter()},
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		result, err := rt.Cleanup(context.Background(), opts)
		if err != nil {
			return err
		}
		fmt.Print(result.Output)
		if result.SpaceFreed != "" {
			fmt.Printf("[tengiz] reclaimed: %s\n", result.SpaceFreed)
		}

		if buildLogs && !dryRun {
			env := getEnv(cmd)
			store := config.NewStoreWithEnv(dataDir, env)
			removed := pruneBuildLogsAllApps(store, keep)
			fmt.Printf("[tengiz] pruned %d build log(s), keeping %d per app\n", removed, keep)
		}
		return nil
	},
}

func pruneBuildLogsAllApps(store *config.Store, keep int) int {
	apps, err := store.ListApps()
	if err != nil {
		return 0
	}
	removed := 0
	for _, app := range apps {
		before, err := store.ListBuildLogs(app.Name)
		if err != nil {
			continue
		}
		if err := store.PruneBuildLogs(app.Name, keep); err != nil {
			continue
		}
		after, err := store.ListBuildLogs(app.Name)
		if err != nil {
			continue
		}
		if len(before) > len(after) {
			removed += len(before) - len(after)
		}
	}
	return removed
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestPruneBuildLogsAllApps" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command with label-protected docker prune"
```

---

### Task 4: Documentation + full verification + self-review

**Files:**
- Modify: `README.md` (commands section)
- Modify: `AGENTS.md` (CLI command list)

**Interfaces:**
- Consumes: the `tengiz cleanup` command from Task 3
- Produces: user-facing documentation

- [ ] **Step 1: Document `tengiz cleanup` in `README.md`**

Insert this section after the `### tengiz rm <app>` section (before `### tengiz rollback <app>`):

```markdown
### `tengiz cleanup [--all] [--volumes] [--until DURATION] [--dry-run] [--build-logs] [--keep N]`

Remove unused Docker resources (containers, images, volumes, networks) to reclaim disk space. Tengiz-managed containers are **protected** via the `tengiz-app` label and are never removed by this command.

| Flag | Description |
|------|-------------|
| `--all` | Also remove all unused images, not just dangling ones |
| `--volumes` | Also prune unused volumes |
| `--until` | Only remove resources created before this duration (e.g. `48h`, `1w`, `30d`) |
| `--dry-run` | Show current Docker disk usage without removing anything |
| `--build-logs` | Also prune old build logs under `~/.tengiz/build-logs` |
| `--keep N` | Number of build logs to keep per app when `--build-logs` is set (default: 5) |

Examples:
```
tengiz cleanup
tengiz cleanup --all --volumes --until 48h
tengiz cleanup --dry-run
```
```

- [ ] **Step 2: Add `tengiz cleanup` to the `AGENTS.md` CLI list**

In the CLI commands block, after the `tengiz rollback <app>` line:

```
tengiz cleanup             → prune unused Docker resources (label-protected)
```

- [ ] **Step 3: Run the full test suite and static checks**

Run: `go build ./...`

Expected: Build succeeds

Run: `go vet ./...`

Expected: No issues

Run: `go test ./... -count=1`

Expected: All PASS (proxy tests are slow, ~2s each; idle tests are time-sensitive but deterministic)

- [ ] **Step 4: Self-review against the spec**

Check against `docs/FUTURES_FEATURES.md` #6 (Docker Housekeeping):
- `tengiz cleanup` command ✅ (Task 3)
- Label-based protection so Tengiz-managed containers are never removed ✅ (Task 2 — `label!=tengiz-app` filter)
- Cleanup of unused containers, images, volumes, networks ✅ (Task 2 — `docker system prune` with `--all`/`--volumes`)
- Per-app image retention already covered by existing `KeepLastNImages` ✅ (unchanged)
- Scheduled/periodic cleanup is a separate feature (#57 Background Monitoring Scheduler) — out of scope, noted in plan ✅

- [ ] **Step 5: Placeholder scan**

Search this plan for any "TBD", "TODO", "implement later", "fill in details", "Similar to Task" patterns. None found. Every code step contains complete code and every run step has an expected result.

- [ ] **Step 6: Type consistency check**

- `HousekeepingOptions{All bool; Volumes bool; Until string; Filters []string; DryRun bool}` — defined in Task 1, used identically in Tasks 2-3
- `HousekeepingResult{Output string; SpaceFreed string}` — defined in Task 1, returned by `Cleanup` in Task 2, read in Task 3
- `Cleanup(ctx context.Context, opts HousekeepingOptions) (*HousekeepingResult, error)` — same signature on `Manager`, `stubManager`, `dockerRuntime`, and all three test mocks
- `pruneBuildLogsAllApps(store *config.Store, keep int) int` — defined and used only in Task 3
- `getEnv(cmd *cobra.Command) string` — existing helper used in Task 3 matches its definition at `internal/cli/root.go:97`

- [ ] **Step 7: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```
