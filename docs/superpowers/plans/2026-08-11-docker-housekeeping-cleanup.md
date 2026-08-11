# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources — stopped non-Tengiz containers, dangling images, unused volumes, and unused networks — using label-based filtering to protect Tengiz-managed containers, with an optional periodic (`--interval`) mode.

**Architecture:** A new `Prune(ctx, opts) (PruneReport, error)` method on the existing `runtime.Manager` interface, implemented on `dockerRuntime` via exec'd `docker` CLI calls (no Docker SDK — matches repo convention). Each resource category is first *collected* by querying Docker with label/dangling filters (`collectRefs`), then removed one-by-one through a shared, dry-run-aware `pruneByIDs` helper. The Cobra `cleanupCmd` resolves its flags into `runtime.PruneOptions`, runs once by default, or on a `time.Ticker` when `--interval` is set.

**Tech Stack:** Go 1.26, Cobra (CLI), existing `runtime.Manager` interface + `dockerRuntime` exec-based implementation, Docker CLI (no new Go dependencies).

## Global Constraints

- No new external Go dependencies — stdlib + existing `cobra` only
- Tengiz-managed containers (labeled `tengiz-app`, `tengiz-env`) are **never** pruned, including scale-to-zero stopped containers and preview deployments
- Only non-running (`status=exited`) containers without a `tengiz-app`/`tengiz-env` label are container-prune candidates
- `docker system prune` is **not** used — granular per-category pruning only (this also covers FUTURES_FEATURES #56 "Granular Docker Prune Operations")
- Build cache / git GC (`--cache --gc`, feature #103) is **out of scope** — separate feature
- Command registered as `tengiz cleanup`; zero category flags set ⇒ all four categories pruned
- `--interval 0` (default) = run once and exit; `--interval 1h` = run once, then every 1h until interrupted
- `--dry-run` counts what *would* be removed without removing anything
- Command must be documented in `README.md` (AGENTS.md rule: update docs on UI changes)
- Test commands: `go test ./... -v -count=1`, `go vet ./...`, `go build -o tengiz .`
- Existing tests must continue to pass; all new unit tests run without a Docker daemon (only the final integration smoke test needs Docker)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Add `PruneOptions`, `PruneReport` types; add `Prune` to `Manager` interface; stub implementation |
| `internal/runtime/prune.go` | **NEW** — dockerRuntime prune implementations: arg builders, `collectRefs`, `parseIDs`, `pruneByIDs`, per-category collect/remove functions, `dockerRuntime.Prune` |
| `internal/runtime/runtime_test.go` | Add `TestStubPrune` |
| `internal/runtime/prune_test.go` | **NEW** — tests for `parseIDs`, `pruneByIDs`, arg builders |
| `internal/cli/root.go` | Add `cleanupCmd`, its flags, and the `pruneOptionsFromFlags` helper |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` (interface fix); tests for command registration, flags, and `pruneOptionsFromFlags` |
| `README.md` | Document `tengiz cleanup` |
| `AGENTS.md` | Add `tengiz cleanup` to the CLI command list |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 (Docker Housekeeping) implemented + add row to implemented table |

---

### Task 1: Prune types + Manager interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/cli/root_test.go` — add `Prune` to `mockRTForDeploy`
- Test: `internal/runtime/runtime_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{Containers, Images, Volumes, Networks, DryRun bool}`, `runtime.PruneReport{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved int}`, `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`, `stubManager.Prune(...)` returning a zero report

- [ ] **Step 1: Write the failing test** — append to `internal/runtime/runtime_test.go`

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 ||
		report.VolumesRemoved != 0 || report.NetworksRemoved != 0 {
		t.Errorf("expected empty report, got %+v", report)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubPrune -v -count=1`
Expected: FAIL — `undefined: PruneOptions`

- [ ] **Step 3: Write minimal implementation** in `internal/runtime/runtime.go`

Add the two types just above the `Manager` interface:

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	DryRun     bool
}

type PruneReport struct {
	ContainersRemoved int
	ImagesRemoved     int
	VolumesRemoved    int
	NetworksRemoved   int
}
```

Add to the `Manager` interface (after `KeepLastNImages`):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

Add to `stubManager` (after `KeepLastNImages`):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}
```

- [ ] **Step 4: Fix `mockRTForDeploy`** in `internal/cli/root_test.go` (adding a method to `Manager` breaks compilation of this mock)

Add inside the `mockRTForDeploy` method set (it already imports `context` and `runtime`):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

- [ ] **Step 5: Run all tests to verify they pass**

Run: `go test ./... -count=1`
Expected: PASS (including `TestStubPrune`, and cli package still compiles with the updated mock)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go internal/cli/root_test.go
git commit -m "feat(runtime): add Prune to Manager interface and stub"
```

---

### Task 2: parseIDs + pruneByIDs helpers + container pruning

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport` from Task 1
- Produces: `parseIDs(output string) []string`, `pruneByIDs(ctx, ids []string, remove func(context.Context, string) error, dryRun bool) int`, `containerPruneFilters []string`, `collectContainersArgs() []string`, `removeContainerArgs(id string) []string`, `execDocker(ctx, args ...string) ([]byte, error)`, `collectRefs(ctx, args []string) ([]string, error)`, `(r *dockerRuntime) removeContainer(ctx, id) error`, `(r *dockerRuntime) pruneContainers(ctx, dryRun) (int, error)`

- [ ] **Step 1: Write the failing tests** — create `internal/runtime/prune_test.go`

```go
package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestParseIDs(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{"empty", "", []string{}},
		{"single", "abc123\n", []string{"abc123"}},
		{"multiple", "abc def\nghi\n", []string{"abc", "def", "ghi"}},
		{"extra whitespace", "  abc   def  \n", []string{"abc", "def"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIDs(tt.output)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseIDs(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestPruneByIDs(t *testing.T) {
	var removed []string
	remove := func(ctx context.Context, id string) error {
		removed = append(removed, id)
		return nil
	}
	n := pruneByIDs(context.Background(), []string{"a", "b", "c"}, remove, false)
	if n != 3 {
		t.Errorf("removed count = %d, want 3", n)
	}
	if len(removed) != 3 {
		t.Errorf("remove called %d times, want 3", len(removed))
	}
}

func TestPruneByIDsDryRun(t *testing.T) {
	var removed []string
	remove := func(ctx context.Context, id string) error {
		removed = append(removed, id)
		return nil
	}
	n := pruneByIDs(context.Background(), []string{"a", "b"}, remove, true)
	if n != 2 {
		t.Errorf("dry-run count = %d, want 2", n)
	}
	if len(removed) != 0 {
		t.Errorf("dry-run must not remove anything, removed %v", removed)
	}
}

func TestPruneByIDsIgnoresErrors(t *testing.T) {
	remove := func(ctx context.Context, id string) error {
		if id == "bad" {
			return errors.New("boom")
		}
		return nil
	}
	n := pruneByIDs(context.Background(), []string{"ok", "bad", "ok2"}, remove, false)
	if n != 2 {
		t.Errorf("removed count = %d, want 2 (failures skipped)", n)
	}
}

func TestPruneContainerArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			"containers",
			collectContainersArgs(),
			[]string{"ps", "-aq",
				"--filter", "status=exited",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env"},
		},
		{"remove container", removeContainerArgs("abc"), []string{"rm", "-f", "abc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestParseIDs|TestPruneByIDs|TestPruneContainerArgs" -v -count=1`
Expected: FAIL — `undefined: parseIDs`, `undefined: pruneByIDs`, `undefined: collectContainersArgs`, `undefined: removeContainerArgs`

- [ ] **Step 3: Write minimal implementation** — create `internal/runtime/prune.go`

```go
package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

var containerPruneFilters = []string{
	"--filter", "status=exited",
	"--filter", "label!=tengiz-app",
	"--filter", "label!=tengiz-env",
}

func parseIDs(output string) []string {
	fields := strings.Fields(output)
	ids := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			ids = append(ids, f)
		}
	}
	return ids
}

func pruneByIDs(ctx context.Context, ids []string, remove func(context.Context, string) error, dryRun bool) int {
	removed := 0
	for _, id := range ids {
		if dryRun {
			removed++
			continue
		}
		if err := remove(ctx, id); err != nil {
			log.Printf("[runtime] cleanup: failed to remove %s: %v", id, err)
			continue
		}
		removed++
	}
	return removed
}

func collectContainersArgs() []string {
	return append([]string{"ps", "-aq"}, containerPruneFilters...)
}

func removeContainerArgs(id string) []string {
	return []string{"rm", "-f", id}
}

func execDocker(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

func collectRefs(ctx context.Context, args []string) ([]string, error) {
	out, err := execDocker(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return parseIDs(string(out)), nil
}

func (r *dockerRuntime) removeContainer(ctx context.Context, id string) error {
	out, err := execDocker(ctx, removeContainerArgs(id)...)
	if err != nil {
		return fmt.Errorf("docker rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) (int, error) {
	ids, err := collectRefs(ctx, collectContainersArgs())
	if err != nil {
		return 0, err
	}
	return pruneByIDs(ctx, ids, r.removeContainer, dryRun), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestParseIDs|TestPruneByIDs|TestPruneContainerArgs" -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): prune stopped non-Tengiz containers"
```

---

### Task 3: image/volume/network pruning + `Prune` composition

**Files:**
- Modify: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `collectRefs`, `pruneByIDs`, `execDocker`, `PruneOptions`/`PruneReport` (Tasks 1-2)
- Produces: `collectImagesArgs() []string`, `collectVolumesArgs() []string`, `collectNetworksArgs() []string`, `removeImageArgs(id) []string`, `removeVolumeArgs(name) []string`, `removeNetworkArgs(id) []string`, `(r *dockerRuntime) removeImage/removeVolume/removeNetwork`, `(r *dockerRuntime) pruneImages/pruneVolumes/pruneNetworks`, and the full `(r *dockerRuntime) Prune(ctx, opts PruneOptions) (PruneReport, error)`

- [ ] **Step 1: Write the failing test** — append to `internal/runtime/prune_test.go`

```go
func TestPruneResourceArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{"images", collectImagesArgs(), []string{"images", "-q", "--filter", "dangling=true"}},
		{"volumes", collectVolumesArgs(), []string{"volume", "ls", "-q", "--filter", "dangling=true"}},
		{"networks", collectNetworksArgs(), []string{"network", "ls", "-q", "--filter", "dangling=true"}},
		{"remove image", removeImageArgs("abc"), []string{"rmi", "-f", "abc"}},
		{"remove volume", removeVolumeArgs("vol1"), []string{"volume", "rm", "vol1"}},
		{"remove network", removeNetworkArgs("abc"), []string{"network", "rm", "abc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run TestPruneResourceArgs -v -count=1`
Expected: FAIL — `undefined: collectImagesArgs`, `undefined: removeImageArgs`, etc.

- [ ] **Step 3: Write minimal implementation** — append to `internal/runtime/prune.go`

```go
func collectImagesArgs() []string {
	return []string{"images", "-q", "--filter", "dangling=true"}
}

func collectVolumesArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
}

func collectNetworksArgs() []string {
	return []string{"network", "ls", "-q", "--filter", "dangling=true"}
}

func removeImageArgs(id string) []string {
	return []string{"rmi", "-f", id}
}

func removeVolumeArgs(name string) []string {
	return []string{"volume", "rm", name}
}

func removeNetworkArgs(id string) []string {
	return []string{"network", "rm", id}
}

func (r *dockerRuntime) removeImage(ctx context.Context, id string) error {
	out, err := execDocker(ctx, removeImageArgs(id)...)
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) removeVolume(ctx context.Context, name string) error {
	out, err := execDocker(ctx, removeVolumeArgs(name)...)
	if err != nil {
		return fmt.Errorf("docker volume rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) removeNetwork(ctx context.Context, id string) error {
	out, err := execDocker(ctx, removeNetworkArgs(id)...)
	if err != nil {
		return fmt.Errorf("docker network rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, dryRun bool) (int, error) {
	ids, err := collectRefs(ctx, collectImagesArgs())
	if err != nil {
		return 0, err
	}
	return pruneByIDs(ctx, ids, r.removeImage, dryRun), nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) (int, error) {
	names, err := collectRefs(ctx, collectVolumesArgs())
	if err != nil {
		return 0, err
	}
	return pruneByIDs(ctx, names, r.removeVolume, dryRun), nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) (int, error) {
	ids, err := collectRefs(ctx, collectNetworksArgs())
	if err != nil {
		return 0, err
	}
	return pruneByIDs(ctx, ids, r.removeNetwork, dryRun), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport
	if opts.Containers {
		n, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.ContainersRemoved = n
	}
	if opts.Images {
		n, err := r.pruneImages(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.ImagesRemoved = n
	}
	if opts.Volumes {
		n, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.VolumesRemoved = n
	}
	if opts.Networks {
		n, err := r.pruneNetworks(ctx, opts.DryRun)
		if err != nil {
			return report, err
		}
		report.NetworksRemoved = n
	}
	return report, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestParseIDs|TestPruneByIDs|TestPruneContainerArgs|TestPruneResourceArgs|TestStubPrune" -v -count=1`
Expected: PASS

- [ ] **Step 5: Manual smoke check against real Docker (optional if daemon available)**

```bash
# Create one non-Tengiz stopped container (prune candidate) and one Tengiz-labeled stopped container (must be protected)
docker run -d --name cleanup-tmp --label purpose=cleanup-test busybox sleep 300
docker run -d --name tengiz-protected --label tengiz-app=myapp --label tengiz-env=production busybox sleep 300
docker stop cleanup-tmp tengiz-protected

go run . cleanup --dry-run --containers
# Expected output: "[tengiz] cleanup: N containers, 0 images, 0 volumes, 0 networks" where N >= 1

go run . cleanup --containers
# Expected: cleanup-tmp is removed; tengiz-protected still exists (exited)

docker ps -a --filter name=cleanup-tmp      # Expected: empty
docker ps -a --filter name=tengiz-protected # Expected: still listed
docker rm -f tengiz-protected
```

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): prune dangling images, unused volumes and networks"
```

---

### Task 4: CLI `tengiz cleanup` command + flag resolution

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.NewDocker()`, `Manager.Prune` (Tasks 1-3)
- Produces: `cleanupCmd *cobra.Command` (registered on root), flags `--containers/--images/--volumes/--networks/--dry-run/--interval`, and `pruneOptionsFromFlags(cmd *cobra.Command) (runtime.PruneOptions, error)`

- [ ] **Step 1: Write the failing tests** — append to `internal/cli/root_test.go` and add `"time"` to its imports (needed for the `Duration` flag)

```go
func newCleanupFlagsCmd() *cobra.Command {
	c := &cobra.Command{Use: "cleanup"}
	c.Flags().Bool("containers", false, "")
	c.Flags().Bool("images", false, "")
	c.Flags().Bool("volumes", false, "")
	c.Flags().Bool("networks", false, "")
	c.Flags().Bool("dry-run", false, "")
	c.Flags().Duration("interval", 0, "")
	return c
}

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"containers", "images", "volumes", "networks", "dry-run", "interval"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestPruneOptionsFromFlagsDefaultAll(t *testing.T) {
	c := newCleanupFlagsCmd()
	opts, err := pruneOptionsFromFlags(c)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks {
		t.Errorf("expected all categories by default, got %+v", opts)
	}
	if opts.DryRun {
		t.Error("dry-run should default to false")
	}
}

func TestPruneOptionsFromFlagsExplicit(t *testing.T) {
	c := newCleanupFlagsCmd()
	if err := c.Flags().Set("containers", "true"); err != nil {
		t.Fatal(err)
	}
	if err := c.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	opts, err := pruneOptionsFromFlags(c)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Containers {
		t.Error("containers should be true when explicitly set")
	}
	if opts.Images || opts.Volumes || opts.Networks {
		t.Errorf("only containers should be set, got %+v", opts)
	}
	if !opts.DryRun {
		t.Error("dry-run should be true")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup|TestPruneOptionsFromFlags" -v -count=1`
Expected: FAIL — `cleanup command not registered` (and/or `undefined: pruneOptionsFromFlags`)

- [ ] **Step 3: Register flags in `init()`** — inside `init()` in `internal/cli/root.go`, add after the webhook flag block:

```go
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Duration("interval", 0, "run cleanup periodically (e.g. 1h, 24h); 0 = run once and exit")
	rootCmd.AddCommand(cleanupCmd)
```

- [ ] **Step 4: Add the command and helper** — place after the `webhookCmd` definition in `internal/cli/root.go`:

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources",
	Long: `Prunes unused Docker resources to reclaim disk space: stopped containers
not managed by Tengiz, dangling images, unused volumes, and unused networks.

Tengiz-managed containers (labeled tengiz-app, including scale-to-zero
stopped containers and preview deployments) are always preserved.

By default all categories are pruned. Use --containers, --images,
--volumes, --networks to select specific categories. Use --dry-run to
show what would be removed without removing anything. Use --interval to
run cleanup periodically (e.g. --interval 24h).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := pruneOptionsFromFlags(cmd)
		if err != nil {
			return err
		}
		interval, _ := cmd.Flags().GetDuration("interval")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		runOnce := func() error {
			report, err := rt.Prune(cmd.Context(), opts)
			if err != nil {
				return err
			}
			fmt.Printf("[tengiz] cleanup: %d containers, %d images, %d volumes, %d networks\n",
				report.ContainersRemoved, report.ImagesRemoved, report.VolumesRemoved, report.NetworksRemoved)
			return nil
		}

		if interval <= 0 {
			return runOnce()
		}

		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer cancel()
		if err := runOnce(); err != nil {
			return err
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if err := runOnce(); err != nil {
					log.Printf("[tengiz] cleanup: %v", err)
				}
			}
		}
	},
}

func pruneOptionsFromFlags(cmd *cobra.Command) (runtime.PruneOptions, error) {
	containers, err := cmd.Flags().GetBool("containers")
	if err != nil {
		return runtime.PruneOptions{}, err
	}
	images, err := cmd.Flags().GetBool("images")
	if err != nil {
		return runtime.PruneOptions{}, err
	}
	volumes, err := cmd.Flags().GetBool("volumes")
	if err != nil {
		return runtime.PruneOptions{}, err
	}
	networks, err := cmd.Flags().GetBool("networks")
	if err != nil {
		return runtime.PruneOptions{}, err
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return runtime.PruneOptions{}, err
	}

	if !cmd.Flags().Changed("containers") && !cmd.Flags().Changed("images") &&
		!cmd.Flags().Changed("volumes") && !cmd.Flags().Changed("networks") {
		containers, images, volumes, networks = true, true, true, true
	}

	return runtime.PruneOptions{
		Containers: containers,
		Images:     images,
		Volumes:    volumes,
		Networks:   networks,
		DryRun:     dryRun,
	}, nil
}
```

(`root.go` already imports `context`, `fmt`, `log`, `os`, `os/signal`, `time`, `strings`, and `github.com/yaso09/tengiz/internal/runtime`.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup|TestPruneOptionsFromFlags" -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add tengiz cleanup command"
```

---

### Task 5: Documentation (README + AGENTS.md + FUTURES_FEATURES.md)

**Files:**
- Modify: `README.md` (add `tengiz cleanup` section after the `tengiz rollback` section)
- Modify: `AGENTS.md` (CLI list)
- Modify: `docs/FUTURES_FEATURES.md` (mark feature #6 implemented)

- [ ] **Step 1: Document `tengiz cleanup` in `README.md`** — insert after the `### \`tengiz rollback <app>\`` section

```markdown
### `tengiz cleanup`

Prune unused Docker resources to reclaim disk space.

Removes stopped containers **not** managed by Tengiz, dangling images, unused volumes, and unused networks. Containers managed by Tengiz (labeled `tengiz-app`, including scale-to-zero stopped containers and preview deployments) are always preserved.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--dry-run` | Show what would be removed without removing anything |
| `--interval` | Run cleanup periodically (e.g. `1h`, `24h`); `0` = run once |

By default all four categories are pruned. Set `--interval` to run cleanup on a schedule, e.g. `tengiz cleanup --interval 24h`.

Examples:

```bash
tengiz cleanup                 # prune everything (once)
tengiz cleanup --dry-run       # preview what would be removed
tengiz cleanup --containers    # only prune stopped non-Tengiz containers
tengiz cleanup --interval 1h   # run every hour
```
```

- [ ] **Step 2: Add to the CLI list in `AGENTS.md`** — after the `tengiz logs`/`build-logs` lines

```markdown
tengiz cleanup [--containers|--images|--volumes|--networks] [--dry-run] [--interval 1h] → prune unused Docker resources (Tengiz containers protected via labels)
```

- [ ] **Step 3: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table, change the row:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

And add a row to the `✅ Implemented Features (Not Pending)` table:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-11) |
```

- [ ] **Step 4: Verify the docs changes**

Run: `grep -rn "tengiz cleanup" README.md AGENTS.md` and `grep -n "Docker Housekeeping" docs/FUTURES_FEATURES.md`
Expected: README and AGENTS.md each show the `tengiz cleanup` line; FUTURES_FEATURES.md shows the row 6 marker changed to ✅ and the new implemented-table row

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup"
```

---

### Task 6: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -v -count=1`
Expected: ALL PASS

- [ ] **Step 2: Run static analysis and build**

Run: `go vet ./... && go build -o tengiz .`
Expected: no output from vet; binary `tengiz` produced

- [ ] **Step 3: End-to-end smoke test (requires Docker daemon)**

```bash
# Build a throwaway non-Tengiz stopped container and a Tengiz-labeled one
docker run -d --name smoke-tmp --label purpose=smoke busybox sleep 300
docker run -d --name smoke-tengiz --label tengiz-app=demoapp --label tengiz-env=production busybox sleep 300
docker stop smoke-tmp smoke-tengiz

./tengiz cleanup --dry-run
# Expected: "[tengiz] cleanup: >=1 containers, ..." and neither container removed
docker ps -a --filter name=smoke-tmp | grep smoke-tmp     # still present
docker ps -a --filter name=smoke-tengiz | grep smoke-tengiz # still present

./tengiz cleanup --containers
# Expected: smoke-tmp removed, smoke-tengiz preserved
docker ps -a --filter name=smoke-tmp | grep smoke-tmp      || echo "removed (expected)"
docker ps -a --filter name=smoke-tengiz | grep smoke-tengiz # still present

# Clean up the protected container
docker rm -f smoke-tengiz
```

- [ ] **Step 4: Commit any leftover changes (none expected)**

```bash
git status
```

---

## Self-Review

**1. Spec coverage (FUTURES_FEATURES #6):**
- "kullanılmayan volume, network, container ve image'leri temizleme" → Tasks 2-3 prune all four categories
- "periyodik temizleme" (periodic cleanup) → Task 4 `--interval` ticker mode
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → `label!=tengiz-app`/`label!=tengiz-env` filters (Task 2) protect all Tengiz containers including scale-to-zero stopped and preview deployments
- "`tengiz cleanup` komutu" → Task 4
- Coverage of related #56 "Granular Docker Prune Operations" via `--containers/--images/--volumes/--networks` flags
- AGENTS.md "update README/docs" rule → Task 5

**2. Placeholder scan:** All code steps contain complete, compilable code with exact file paths, commands, and expected outputs. No TBD/TODO/"add validation" placeholders. Out-of-scope items (#103 build cache/git GC, periodic auto-registration as a systemd job) are explicitly excluded rather than stubbed.

**3. Type consistency:** `PruneOptions{Containers, Images, Volumes, Networks, DryRun}` and `PruneReport{ContainersRemoved, ImagesRemoved, VolumesRemoved, NetworksRemoved}` are defined once (Task 1) and referenced identically everywhere. `pruneOptionsFromFlags` returns `runtime.PruneOptions` (Task 4) matching the `Manager.Prune` signature (Task 1). `mockRTForDeploy.Prune` returns `runtime.PruneReport{}` matching the stub. Helper names (`collectContainersArgs`, `collectImagesArgs`, `removeContainerArgs`, etc.) are consistent across the arg-builder tests and implementations. `collectRefs`/`parseIDs` treat Docker IDs and volume/network names uniformly as whitespace-delimited tokens.
