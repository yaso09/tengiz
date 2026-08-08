# Docker Housekeeping (tengiz cleanup) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command with label-based, category-scoped Docker prune calls plus per-app image retention so operators can reclaim disk space caused by continuous deploys and scale-to-zero churn without ever touching running or Tengiz-managed (stopped) containers.

**Architecture:** A new `runtime.PruneOptions` / `runtime.PruneResult` / `runtime.DFEntry` API is added to the `runtime.Manager` interface. Pure, Docker-free helpers (`parseSystemDFOutput`, `categoryEnabled`, `PrunePlan`, `findOrphanTengizImages`, `appSet`) live in `internal/runtime/prune.go` and are unit-tested without a daemon. The real `dockerRuntime` shells out to `docker container/network/volume/image/builder prune`, and a `docker container prune --filter label!=tengiz-app` guard keeps every managed container (including stopped scale-to-zero ones) safe. Image retention reuses the existing `KeepLastNImages`. A new `internal/cli/cleanup.go` Cobra command exposes `--dry-run`, per-category toggles, `--all`, and `--keep N`.

**Tech Stack:** Go 1.26, existing `os/exec`-based `runtime` package (no Docker SDK), existing `config.Store` (env-scoped), existing `dockerRuntime.KeepLastNImages`, `github.com/spf13/cobra`.

## Global Constraints

- Default environment is `"production"`; cleanup runs against the env selected by the existing global `--env` flag via `getEnv(cmd)`
- Prune **must never** remove a container carrying the `tengiz-app` label — including stopped scale-to-zero containers; enforced with `docker container prune -f --filter label!=tengiz-app` (running containers are never pruned by Docker anyway)
- Only images matching the `tengiz-apps/<app>:*` schema produced by `internal/builder` are considered for removal (tag format `tengiz-apps/<app>:<env>-<deploymentID>` and `<env>-latest`)
- Per-app image retention keeps the newest `--keep` images per app (default 5), consistent with existing `deploy`/`gitdeploy` call sites
- `--dry-run` only prints the plan (`runtime.PrunePlan`) and never invokes Docker
- `--all` enables every category; `build-cache` prune failures are logged as warnings, never fatal
- Every pure helper must be unit-testable without a Docker daemon (CI runs without Docker)
- Adding `Prune` to the `runtime.Manager` interface requires updating every implementer: `stubManager` (`internal/runtime/runtime.go`) and mocks `mockRTForDeploy` (`internal/cli/root_test.go:99`), `mockRuntime` (`internal/proxy/proxy_test.go`, `internal/idle/idle_test.go`)
- Follow existing CLI conventions: `fmt.Printf("[tengiz] ...")`, `getEnv(cmd)`, `config.NewStoreWithEnv(dataDir, env)`, per-file `init()` registration like `internal/cli/preview.go`
- No new external Go dependencies

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` (new) | `PruneOptions`, `DFEntry`, `PruneResult` types; pure helpers `parseSystemDFOutput`, `categoryEnabled`, `PrunePlan`, `findOrphanTengizImages`, `appSet`; dockerRuntime methods `systemDF`, `listAppImages`, `Prune` |
| `internal/runtime/runtime.go` | Add `Prune(ctx, opts, apps) (PruneResult, error)` to `Manager` + stub implementation |
| `internal/runtime/prune_test.go` (new) | Unit tests for pure helpers + stub `Prune` |
| `internal/cli/cleanup.go` (new) | `cleanupCmd`, `init()` registration + flags, DF table printer |
| `internal/cli/cleanup_test.go` (new) | Registration / flags / dry-run tests |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` in the CLI section |

---

### Task 1: Runtime prune types + system-df parser

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.PruneOptions{Env string; DryRun bool; Keep int; Containers, Images, Volumes, Networks, Helper, All bool}`, `runtime.DFEntry{Type string; Active int; Size, Reclaimable string}`, `runtime.PruneResult{DryRun bool; Plan []string; SystemBefore, SystemAfter []DFEntry; Orphans []string}`, `func parseSystemDFOutput(out string) []DFEntry`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/prune_test.go
package runtime

import (
	"context"
	"testing"
)

func testCtx() context.Context { return context.Background() }

func TestParseSystemDFOutput(t *testing.T) {
	out := "Images|5|2.3GB|1.8GB\nContainers|3|12B|12B\nLocal Volumes|2|4kB|4kB"
	entries := parseSystemDFOutput(out)
	if len(entries) != 3 {
		t.Fatalf("len = %d, want 3", len(entries))
	}
	if entries[0].Type != "Images" || entries[0].Active != 5 || entries[0].Size != "2.3GB" || entries[0].Reclaimable != "1.8GB" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
}

func TestParseSystemDFOutputTrailingNewlineAndEmpty(t *testing.T) {
	if got := parseSystemDFOutput(""); len(got) != 0 {
		t.Fatalf("empty input parsed into %d entries", len(got))
	}
	if got := parseSystemDFOutput("Images|5|2.3GB|1.8GB\n\n"); len(got) != 1 {
		t.Fatalf("trailing newline produced %d entries, want 1", len(got))
	}
}

func TestStubPruneReturnsEmptyResult(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(testCtx(), PruneOptions{All: true, Keep: 5}, []string{"testapp"})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.SystemBefore != nil || len(res.Orphans) != 0 {
		t.Fatalf("stub Prune() = %+v, want zero-valued result", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestParseSystemDFOutput|TestStubPruneReturnsEmptyResult' -v -count=1`
Expected: FAIL — compile error `undefined: parseSystemDFOutput`, `undefined: PruneOptions`, `m.Prune undefined (type runtime.Manager has no field or method Prune)`. (The stub `Prune` test is only fully green after Task 3 adds the interface method; until then it intentionally fails — see Task 3.)

- [ ] **Step 3: Write minimal implementation**

```go
// internal/runtime/prune.go
package runtime

import (
	"sort"
	"strconv"
	"strings"
)

type PruneOptions struct {
	Env        string
	DryRun     bool
	Keep       int
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	All        bool
}

type DFEntry struct {
	Type        string
	Active      int
	Size        string
	Reclaimable string
}

type PruneResult struct {
	DryRun        bool
	Plan          []string
	SystemBefore  []DFEntry
	SystemAfter   []DFEntry
	Orphans       []string
}

// parseSystemDFOutput parses the output of
// `docker system df --format '{{.Type}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}'`.
func parseSystemDFOutput(out string) []DFEntry {
	var entries []DFEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		active, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		entries = append(entries, DFEntry{
			Type:        parts[0],
			Active:      active,
			Size:        parts[2],
			Reclaimable: parts[3],
		})
	}
	return entries
}
```

- [ ] **Step 4: Run test to verify the parser passes**

Run: `go test ./internal/runtime/ -run TestParseSystemDFOutput -v -count=1`
Expected: PASS (2 tests). The stub test remains red until Task 4's interface method exists.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add prune types and system df parser"
```

---

### Task 2: Category gating + disk snapshot helper

**Files:**
- Modify (append to): `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `PruneOptions`, `DFEntry`, `parseSystemDFOutput` from Task 1
- Produces: `func categoryEnabled(opts PruneOptions, category string) bool`, `func PrunePlan(opts PruneOptions) []string`, `func (r *dockerRuntime) systemDF(ctx context.Context) []DFEntry`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/prune_test.go
package runtime

import (
	"reflect"
	"testing"
)

func TestCategoryEnabled(t *testing.T) {
	if !categoryEnabled(PruneOptions{Containers: true}, "Containers") {
		t.Error("explicit Containers flag should enable Containers")
	}
	if categoryEnabled(PruneOptions{Containers: true}, "Images") {
		t.Error("Images must stay disabled without its flag")
	}
	if !categoryEnabled(PruneOptions{All: true}, "Volumes") {
		t.Error("All should enable Volumes")
	}
	if !categoryEnabled(PruneOptions{All: true}, "BuildCache") {
		t.Error("All should enable BuildCache")
	}
}

func TestPrunePlan(t *testing.T) {
	opts := PruneOptions{Images: true, Volumes: true}
	want := []string{"stopped containers not managed by Tengiz (docker container prune --filter label!=tengiz-app)", "unused networks (docker network prune)", "dangling + old images (docker image prune + per-app retention)"}
	got := PrunePlan(PruneOptions{Containers: true, Networks: true, Images: true})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PrunePlan = %v, want %v", got, want)
	}
	if len(PrunePlan(PruneOptions{All: true})) != 5 {
		t.Fatalf("All plan should have 5 entries, got %d", len(PrunePlan(PruneOptions{All: true})))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestCategoryEnabled|TestPrunePlan' -v -count=1`
Expected: FAIL — `undefined: categoryEnabled`, `undefined: PrunePlan`

- [ ] **Step 3: Write minimal implementation**

Replace the import block of `internal/runtime/prune.go` with:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)
```

Then append:

```go
// categoryEnabled reports whether a category should be pruned under opts.
func categoryEnabled(opts PruneOptions, category string) bool {
	if opts.All {
		return true
	}
	switch category {
	case "Containers":
		return opts.Containers
	case "Images":
		return opts.Images
	case "Volumes":
		return opts.Volumes
	case "Networks":
		return opts.Networks
	case "BuildCache":
		return opts.BuildCache
	}
	return false
}

// PrunePlan returns the human-readable list of categories that would run under opts.
func PrunePlan(opts PruneOptions) []string {
	cats := []struct {
		key   string
		label string
	}{
		{"Containers", "stopped containers not managed by Tengiz (docker container prune --filter label!=tengiz-app)"},
		{"Networks", "unused networks (docker network prune)"},
		{"Volumes", "unused volumes (docker volume prune)"},
		{"Images", "dangling + old images (docker image prune + per-app retention)"},
		{"BuildCache", "Docker build cache (docker builder prune)"},
	}
	var plan []string
	for _, c := range cats {
		if categoryEnabled(opts, c.key) {
			plan = append(plan, c.label)
		}
	}
	return plan
}

// systemDF snapshots current Docker disk usage.
func (r *dockerRuntime) systemDF(ctx context.Context) []DFEntry {
	out, err := exec.CommandContext(ctx, "docker", "system", "df",
		"--format", "{{.Type}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}",
	).CombinedOutput()
	if err != nil {
		return nil
	}
	return parseSystemDFOutput(string(out))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestCategoryEnabled|TestPrunePlan' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add prune category gating and disk snapshot helper"
```

---

### Task 3: Image listing + orphan detection helpers

**Files:**
- Modify (append to): `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `PruneResult`, `PruneOptions`, helpers from Tasks 1–2
- Produces: `func findOrphanTengizImages(images []string, known map[string]bool, env string) []string`, `func appSet(apps []string) map[string]bool`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/prune_test.go
package runtime

import (
	"reflect"
	"testing"
)

func TestFindOrphanTengizImages(t *testing.T) {
	images := []string{
		"tengiz-apps/webapp:production-1712",
		"tengiz-apps/webapp:development-1715",
		"tengiz-apps/removedapp:production-999",
		"tengiz-apps/old:staging-1",
	}
	known := map[string]bool{"webapp": true}
	orphans := findOrphanTengizImages(images, known, "production")
	want := []string{"tengiz-apps/removedapp:production-999"}
	if !reflect.DeepEqual(orphans, want) {
		t.Fatalf("orphans = %v, want %v", orphans, want)
	}
}

func TestFindOrphanTengizImagesKeepsOtherEnvs(t *testing.T) {
	orphans := findOrphanTengizImages(
		[]string{"tengiz-apps/webapp:development-latest"},
		map[string]bool{},
		"production",
	)
	if len(orphans) != 0 {
		t.Fatalf("production cleanup touched a development image: %v", orphans)
	}
}

func TestAppSet(t *testing.T) {
	got := appSet([]string{"a", "b", "a"})
	if len(got) != 2 || !got["a"] || !got["b"] {
		t.Fatalf("appSet = %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run 'TestFindOrphanTengizImages|TestAppSet' -v -count=1`
Expected: FAIL — `undefined: findOrphanTengizImages`, `undefined: appSet`

- [ ] **Step 3: Write minimal implementation**

```go
// append to internal/runtime/prune.go

// findOrphanTengizImages returns tengiz-apps images whose app no longer exists
// and whose tag prefixes the given env. Tags that do not belong to env are never
// touched, so one env's cleanup cannot delete another env's images. The literal
// "latest" tag (used only outside the builder naming) is also skipped.
func findOrphanTengizImages(images []string, known map[string]bool, env string) []string {
	if env == "" {
		env = "production"
	}
	envPrefix := env + "-"
	var orphans []string
	for _, img := range images {
		idx := strings.LastIndex(img, ":")
		if idx < 0 {
			continue
		}
		repo, tag := img[:idx], img[idx+1:]
		app := strings.TrimPrefix(repo, "tengiz-apps/")
		if app == "" || repo == app || tag == "latest" {
			continue
		}
		if !known[app] && strings.HasPrefix(tag, envPrefix) {
			orphans = append(orphans, img)
		}
	}
	sort.Strings(orphans)
	return orphans
}

func appSet(apps []string) map[string]bool {
	seen := make(map[string]bool, len(apps))
	for _, a := range apps {
		seen[a] = true
	}
	return seen
}
```

`sort` and `strings` are already imported (Task 1/2).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run 'TestFindOrphanTengizImages|TestAppSet' -v -count=1`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add orphan image detection helpers"
```

---

### Task 4: Runtime orchestration — `Prune` on dockerRuntime, interface + stubs

**Files:**
- Modify (append to): `internal/runtime/prune.go`
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/cli/root_test.go` (mock)
- Modify: `internal/proxy/proxy_test.go` (mock)
- Modify: `internal/idle/idle_test.go` (mock)
- Test: `internal/runtime/prune_test.go`

**Interfaces:**
- Consumes: `categoryEnabled`, `PrunePlan`, `systemDF`, `findOrphanTengizImages`, `appSet`, existing `dockerRuntime.KeepLastNImages`, `dockerRuntime.RemoveImage`
- Produces: `func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions, apps []string) (PruneResult, error)` — added to `Manager` as `Prune(ctx context.Context, opts PruneOptions, apps []string) (PruneResult, error)`; `func runPruneCommand(ctx context.Context, args ...string) error`; `func (r *dockerRuntime) listAppImages(ctx context.Context) []string`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/prune_test.go
package runtime

import "testing"

func TestStubPruneImplementsManager(t *testing.T) {
	var _ Manager = NewStub()
	m := NewStub()
	res, err := m.Prune(testCtx(), PruneOptions{Keep: 3}, []string{"app"})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.DryRun || len(res.Plan) != 0 || len(res.Orphans) != 0 {
		t.Fatalf("stub Prune() = %+v, want zero-valued", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/... -count=1 2>&1 | head -40`
Expected: Compile failure — `Manager` interface has no `Prune` method (and `*stubManager` does not implement it), so `internal/cli`, `internal/proxy`, and `internal/idle` packages also report "missing method Prune" for their mocks. That is expected and resolved in Step 3.

- [ ] **Step 3: Write the implementation**

Append to `internal/runtime/prune.go` — replace the import block once more with:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)
```

then append:

```go
// runPruneCommand executes `docker <args...>` and wraps any error with output.
func runPruneCommand(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}

// listAppImages returns all Tengiz-built image tags (so it is filterable).
func (r *dockerRuntime) listAppImages(ctx context.Context) []string {
	out, err := exec.CommandContext(ctx, "docker", "images",
		"--filter", "reference=tengiz-apps/*",
		"--format", "{{.Repository}}:{{.Tag}}",
	).CombinedOutput()
	if err != nil {
		return nil
	}
	var images []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			images = append(images, line)
		}
	}
	sort.Strings(images)
	return images
}

// Prune performs housekeeping. The `apps` slice names apps still managed in this
// env; any tengiz-apps image for a removed app whose tag belongs to opts.Env is
// removed. Containers labeled tengiz-app are never touched.
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions, apps []string) (PruneResult, error) {
	if opts.Keep <= 0 {
		opts.Keep = 5
	}
	res := PruneResult{DryRun: opts.DryRun, Plan: PrunePlan(opts)}
	res.SystemBefore = r.systemDF(ctx)

	if opts.DryRun {
		return res, nil
	}

	if categoryEnabled(opts, "Containers") {
		if err := runPruneCommand(ctx, "container", "prune", "-f", "--filter", "label!="+labelKey); err != nil {
			return res, fmt.Errorf("containers: %w", err)
		}
	}
	if categoryEnabled(opts, "Networks") {
		if err := runPruneCommand(ctx, "network", "prune", "-f"); err != nil {
			return res, fmt.Errorf("networks: %w", err)
		}
	}
	if categoryEnabled(opts, "Volumes") {
		if err := runPruneCommand(ctx, "volume", "prune", "-f"); err != nil {
			return res, fmt.Errorf("volumes: %w", err)
		}
	}
	if categoryEnabled(opts, "BuildCache") {
		if err := runPruneCommand(ctx, "builder", "prune", "-f"); err != nil {
			log.Printf("[runtime] build cache prune skipped: %v", err)
		}
	}
	if categoryEnabled(opts, "Images") {
		if err := runPruneCommand(ctx, "image", "prune", "-f"); err != nil {
			return res, fmt.Errorf("images: %w", err)
		}
		for _, img := range findOrphanTengizImages(r.listAppImages(ctx), appSet(apps), opts.Env) {
			if err := r.RemoveImage(ctx, img); err != nil {
				log.Printf("[runtime] failed to remove orphan image %s: %v", img, err)
				continue
			}
			res.Orphans = append(res.Orphans, img)
		}
		for _, app := range apps {
			if err := r.KeepLastNImages(ctx, app, opts.Keep); err != nil {
				log.Printf("[runtime] failed to retain images for %s: %v", app, err)
			}
		}
	}

	res.SystemAfter = r.systemDF(ctx)
	return res, nil
}
```

In `internal/runtime/runtime.go`, add `Prune` to the `Manager` interface after line 36 (`KeepLastNImages`):

```go
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Prune(ctx context.Context, opts PruneOptions, apps []string) (PruneResult, error)
	Start(ctx context.Context, name string) error
```

And add the stub implementation after the `KeepLastNImages` stub in `runtime.go`:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions, apps []string) (PruneResult, error) {
	return PruneResult{}, nil
}
```

Update the three test mocks. In `internal/cli/root_test.go`, after the `KeepLastNImages` stub (line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions, apps []string) (runtime.PruneResult, error) {
	return runtime.PruneResult{}, nil
}
```

In `internal/proxy/proxy_test.go` and `internal/idle/idle_test.go`, after each `KeepLastNImages` stub:

```go
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions, apps []string) (runtime.PruneResult, error) {
	return runtime.PruneResult{}, nil
}
```

- [ ] **Step 4: Run tests to verify all pass**

Run: `go build ./... && go test ./internal/... -count=1`
Expected: PASS — all packages build and every `Manager` mock implements the new method.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/runtime.go internal/runtime/prune_test.go internal/cli/root_test.go internal/proxy/proxy_test.go internal/idle/idle_test.go
git commit -m "feat(runtime): implement docker housekeeping Prune with label-based protection"
```

---

### Task 5: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Test: `internal/cli/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneResult`, `runtime.DFEntry`, `runtime.PrunePlan`, `getEnv(cmd)`, package var `dataDir`, `config.NewStoreWithEnv`
- Produces: `cleanupCmd *cobra.Command` (registered in `init()`)

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/cleanup_test.go
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

func TestCleanupCommandHasFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "containers", "images", "volumes", "networks", "build-cache", "all", "keep"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("--%s flag not found", name)
		}
	}
}

func TestCleanupDryRunDoesNotTouchDocker(t *testing.T) {
	// The real RunE short-circuits before NewDocker() when --dry-run is set.
	// Override RunE to confirm flag flow, since real docker is absent in CI.
	original := cleanupCmd.RunE
	defer func() { cleanupCmd.RunE = original }()
	var dryRun bool
	cleanupCmd.RunE = func(cmd *cobra.Command, args []string) error {
		var err error
		dryRun, err = cmd.Flags().GetBool("dry-run")
		return err
	}
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("cleanup --dry-run error = %v", err)
	}
	if !dryRun {
		t.Fatal("dry-run flag was not read")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`
Expected: FAIL — `undefined: cleanupCmd`

- [ ] **Step 3: Write the CLI command**

```go
// internal/cli/cleanup.go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker images, containers, volumes, networks, and build cache",
	Long: `Reclaim disk space from continuous deploys and scale-to-zero churn.
Never touches running or stopped containers labeled 'tengiz-app' (managed apps).
Use --dry-run to preview what would be cleaned.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		buildCache, _ := cmd.Flags().GetBool("build-cache")
		all, _ := cmd.Flags().GetBool("all")
		keep, _ := cmd.Flags().GetInt("keep")

		opts := runtime.PruneOptions{
			Env:        env,
			DryRun:     dryRun,
			Keep:       keep,
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			BuildCache: buildCache,
			All:        all,
		}

		if dryRun {
			fmt.Println("[tengiz] cleanup preview (no changes made):")
			for _, p := range runtime.PrunePlan(opts) {
				fmt.Printf("  - %s\n", p)
			}
			return nil
		}

		store := config.NewStoreWithEnv(dataDir, env)
		var appNames []string
		if entries, err := store.ListApps(); err == nil {
			for _, e := range entries {
				appNames = append(appNames, e.Name)
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		res, err := rt.Prune(cmd.Context(), opts, appNames)
		if err != nil {
			return err
		}

		fmt.Println("[tengiz] cleanup complete:")
		fmt.Println("  Before:")
		printDF(res.SystemBefore)
		fmt.Println("  After:")
		printDF(res.SystemAfter)
		for _, img := range res.Orphans {
			fmt.Printf("  removed orphan image: %s\n", img)
		}
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "print what would be cleaned without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images and apply per-app retention")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused Docker volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused Docker networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune the Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "enable all cleanup categories")
	cleanupCmd.Flags().Int("keep", 5, "number of images to keep per app")
	rootCmd.AddCommand(cleanupCmd)
}

func printDF(entries []runtime.DFEntry) {
	if len(entries) == 0 {
		fmt.Println("    (no docker system df data)")
		return
	}
	fmt.Printf("    %-14s %-8s %-12s %s\n", "TYPE", "ACTIVE", "SIZE", "RECLAIMABLE")
	for _, e := range entries {
		fmt.Printf("    %-14s %-8d %-12s %s\n", e.Type, e.Active, e.Size, e.Reclaimable)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestCleanup' -v -count=1`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go
git commit -m "feat(cli): add tengiz cleanup command for Docker housekeeping"
```

---

### Task 6: Documentation update

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add the CLI entry**

In the CLI command list of `README.md`, add a line (next to the other maintenance commands):

```markdown
tengiz cleanup [--dry-run] [--containers] [--images] [--volumes] [--networks] [--build-cache] [--all] [--keep N] → reclaim disk space by pruning unused Docker containers/images/volumes/networks (never touches managed containers)
```

- [ ] **Step 2: Verify build, vet, and full suite**

Run: `go build -o /tmp/tengiz-check . && go vet ./... && go test ./... -count=1`
Expected: build succeeds, `go vet` clean, all tests PASS (runtime + cli + proxy + idle packages compile with the new `Manager` method)

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage.** The feature spec (`docs/FUTURES_FEATURES.md` #6 + its detail section) asks for label-based pruning and a `tengiz cleanup` command that preserves Tengiz-managed containers while reclaiming disk from build artifacts. Coverage: label-filtered `docker container prune --filter label!=tengiz-app` (Task 4 Step 3), per-app image retention `--keep N` reusing `KeepLastNImages` (Task 4), orphan-image removal for deleted apps (Task 3 + Task 4 via `findOrphanTengizImages`), granular category flags + `--all` (Tasks 2, 4, 5), `--dry-run` preview (Tasks 2 & 5), README docs (Task 6, fulfills the repo rule to update README on UX changes). We chose a granular category-wise prune series rather than a single `docker system prune`, matching the spirit of companion feature #56 (Granular Docker Prune) without introducing a maintenance daemon.

**2. Placeholder scan.** Every code step contains full code; no TBD/TODO. All types referenced in later tasks are defined in earlier tasks: `PruneOptions`, `DFEntry`, `PruneResult` (Task 1); `categoryEnabled`, `PrunePlan`, `systemDF` (Task 2); `findOrphanTengizImages`, `appSet` (Task 3); `Prune`, `runPruneCommand`, `listAppImages` (Task 4). The `Prune` interface method is added in Task 4 in the same step that updates the stub and all three test mocks.

**3. Type consistency.** `Manager.Prune(ctx context.Context, opts PruneOptions, apps []string) (PruneResult, error)` is identical on the interface, stub, and all three mocks. `PruneResult` fields used by the CLI are `Plan []string`, `SystemBefore/SystemAfter []DFEntry`, `Orphans []string`, declared identically in Task 1 and produced in Task 4. `runtime.PrunePlan` is exported and consumed by both `Prune` (via `res.Plan`) and the CLI dry-run path.