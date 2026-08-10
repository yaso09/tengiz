# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes stopped/unused Docker containers, images, networks, and build cache (optionally volumes) to reclaim disk space while never touching Tengiz-managed resources.

**Architecture:** All Docker exec lives in `runtime`. A pure, unit-testable arg-builder layer (`PruneActions`, `build*PruneArgs`) constructs `docker <subcommand> prune --force` invocations with a `label!=tengiz-app` filter so containers/images carrying the `tengiz-app` label (all Tengiz-managed apps) are excluded. `dockerRuntime.Prune`/`DiskUsage` and the `runtime.Manager` interface expose this to the CLI. The new `tengiz cleanup` command maps flags to `runtime.PruneOptions`, prints `docker system df` before/after, and supports `--dry-run` (prints commands without executing). Images built by Tengiz get `tengiz-app`/`tengiz-env` labels at build time so the label filter protects them too (they are additionally protected because Docker never prunes images referenced by a container).

**Tech Stack:** Go 1.26, Cobra (CLI), existing `os/exec`-based Docker client in `internal/runtime`, existing `runtime.Manager` interface + `stubManager` test mock. No new external dependencies.

## Global Constraints

- Every pruning invocation that could touch a Tengiz app uses the `--filter "label!=tengiz-app"` flag (containers and images only)
- Defaults: containers/images/networks/build-cache pruned; volumes NOT pruned; `--all` off (dangling images only)
- `--dry-run` prints the exact `docker` commands without executing them
- Docker never removes images still referenced by any container (running or stopped), so scale-to-zero stopped Tengiz containers protect their images even without labels
- Image labels added only to the `docker build` path (`buildWithDockerfile`); nixpacks-built images rely on the container-reference safety net (no code change to nixpacks path)
- Reuse existing constants: `labelKey = "tengiz-app"` (`internal/runtime/docker.go:76`); do not redefine
- All work on branch `feat/docker-housekeeping`
- Existing tests must continue to pass without modification
- No new external dependencies
- Run `go build ./...`, `go vet ./...`, `go test ./... -v -count=1` before the final commit of each task

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/housekeeping.go` | **Create** — `PruneOptions`, `PruneAction`, `PruneSummary` types; pure arg builders (`buildContainerPruneArgs`, `buildImagePruneArgs`, `buildVolumePruneArgs`, `buildNetworkPruneArgs`, `buildBuilderPruneArgs`, `buildDiskUsageArgs`); `PruneActions(opts)` selection; `(*dockerRuntime).Prune` and `(*dockerRuntime).DiskUsage` |
| `internal/runtime/runtime.go` | **Modify** — add `DiskUsage` and `Prune` to `Manager` interface; add stub implementations |
| `internal/runtime/housekeeping_test.go` | **Create** — arg-builder tests, `PruneActions` selection tests, stub tests |
| `internal/builder/builder.go` | **Modify** — `buildLabelArgs(appName, env)` helper + wire into `buildWithDockerfile` |
| `internal/builder/builder_test.go` | **Modify** — add `TestBuildLabelArgs` |
| `internal/cli/root.go` | **Modify** — add `cleanupCmd`, `addCleanupFlags`, `buildPruneOptions` helper; register in `init()` |
| `internal/cli/cmd_cleanup_test.go` | **Create** — command registration, flag defaults, options mapping tests |
| `README.md` | **Modify** — feature bullet + `tengiz cleanup` CLI reference section |
| `docs/FUTURES_FEATURES.md` | **Modify** — mark #6 Docker Housekeeping ✅ Implemented, add to implemented table |

---

### Task 1: Create runtime prune types + arg builders (pure, testable)

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `labelKey` constant (`internal/runtime/docker.go:76`)
- Produces: `type PruneOptions struct { All, Containers, Images, Volumes, Networks, BuildCache bool }`, `type PruneAction struct { Kind string; Args []string }`, `func buildContainerPruneArgs() []string`, `func buildImagePruneArgs(all bool) []string`, `func buildVolumePruneArgs() []string`, `func buildNetworkPruneArgs() []string`, `func buildBuilderPruneArgs() []string`, `func buildDiskUsageArgs() []string`, `func PruneActions(opts PruneOptions) []PruneAction`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

- [ ] **Step 2: Write the failing test**

Create `internal/runtime/housekeeping_test.go`:

```go
package runtime

import (
	"reflect"
	"testing"
)

func TestBuildContainerPruneArgs(t *testing.T) {
	got := buildContainerPruneArgs()
	want := []string{"container", "prune", "--force", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildContainerPruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildImagePruneArgsDangling(t *testing.T) {
	got := buildImagePruneArgs(false)
	want := []string{"image", "prune", "--force", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildImagePruneArgs(false) = %v, want %v", got, want)
	}
}

func TestBuildImagePruneArgsAll(t *testing.T) {
	got := buildImagePruneArgs(true)
	want := []string{"image", "prune", "-a", "--force", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildImagePruneArgs(true) = %v, want %v", got, want)
	}
}

func TestBuildVolumePruneArgs(t *testing.T) {
	got := buildVolumePruneArgs()
	want := []string{"volume", "prune", "--force"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildVolumePruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildNetworkPruneArgs(t *testing.T) {
	got := buildNetworkPruneArgs()
	want := []string{"network", "prune", "--force"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildNetworkPruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildBuilderPruneArgs(t *testing.T) {
	got := buildBuilderPruneArgs()
	want := []string{"builder", "prune", "--force"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildBuilderPruneArgs() = %v, want %v", got, want)
	}
}

func TestBuildDiskUsageArgs(t *testing.T) {
	got := buildDiskUsageArgs()
	want := []string{"system", "df"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildDiskUsageArgs() = %v, want %v", got, want)
	}
}

func TestPruneActionsSelection(t *testing.T) {
	tests := []struct {
		name      string
		opts      PruneOptions
		wantKinds []string
	}{
		{"all disabled", PruneOptions{}, nil},
		{"containers only", PruneOptions{Containers: true}, []string{"containers"}},
		{"default full", PruneOptions{Containers: true, Images: true, Networks: true, BuildCache: true}, []string{"containers", "images", "networks", "build-cache"}},
		{"full with volumes and all-images", PruneOptions{All: true, Containers: true, Images: true, Volumes: true, Networks: true, BuildCache: true}, []string{"containers", "images", "volumes", "networks", "build-cache"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actions := PruneActions(tc.opts)
			if len(actions) != len(tc.wantKinds) {
				t.Fatalf("PruneActions() returned %d actions (%v), want %d (%v)", len(actions), actionKinds(actions), len(tc.wantKinds), tc.wantKinds)
			}
			for i, a := range actions {
				if a.Kind != tc.wantKinds[i] {
					t.Fatalf("action[%d].Kind = %q, want %q", i, a.Kind, tc.wantKinds[i])
				}
			}
		})
	}
}

func TestPruneActionsImageAllFlag(t *testing.T) {
	actual := PruneActions(PruneOptions{Containers: true, Images: true})
	images := actual[1]
	if !reflect.DeepEqual(images.Args, buildImagePruneArgs(false)) {
		t.Fatalf("image action args = %v, want %v (no -a)", images.Args, buildImagePruneArgs(false))
	}

	actualAll := PruneActions(PruneOptions{All: true, Containers: true, Images: true})
	imagesAll := actualAll[1]
	if !reflect.DeepEqual(imagesAll.Args, buildImagePruneArgs(true)) {
		t.Fatalf("image action args (all) = %v, want %v", imagesAll.Args, buildImagePruneArgs(true))
	}
}

func actionKinds(actions []PruneAction) []string {
	kinds := make([]string, 0, len(actions))
	for _, a := range actions {
		kinds = append(kinds, a.Kind)
	}
	return kinds
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestBuild|TestPruneActions" -v -count=1`

Expected: FAIL with `undefined: buildContainerPruneArgs`, `undefined: PruneActions`, `undefined: PruneOptions`

- [ ] **Step 4: Write minimal implementation**

Create `internal/runtime/housekeeping.go`:

```go
package runtime

type PruneOptions struct {
	All        bool
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
}

type PruneAction struct {
	Kind string
	Args []string
}

type PruneSummary struct {
	Kind   string
	Output string
}

func buildContainerPruneArgs() []string {
	return []string{"container", "prune", "--force", "--filter", "label!=" + labelKey}
}

func buildImagePruneArgs(all bool) []string {
	args := []string{"image", "prune"}
	if all {
		args = append(args, "-a")
	}
	args = append(args, "--force", "--filter", "label!="+labelKey)
	return args
}

func buildVolumePruneArgs() []string {
	return []string{"volume", "prune", "--force"}
}

func buildNetworkPruneArgs() []string {
	return []string{"network", "prune", "--force"}
}

func buildBuilderPruneArgs() []string {
	return []string{"builder", "prune", "--force"}
}

func buildDiskUsageArgs() []string {
	return []string{"system", "df"}
}

func PruneActions(opts PruneOptions) []PruneAction {
	var actions []PruneAction
	if opts.Containers {
		actions = append(actions, PruneAction{Kind: "containers", Args: buildContainerPruneArgs()})
	}
	if opts.Images {
		actions = append(actions, PruneAction{Kind: "images", Args: buildImagePruneArgs(opts.All)})
	}
	if opts.Volumes {
		actions = append(actions, PruneAction{Kind: "volumes", Args: buildVolumePruneArgs()})
	}
	if opts.Networks {
		actions = append(actions, PruneAction{Kind: "networks", Args: buildNetworkPruneArgs()})
	}
	if opts.BuildCache {
		actions = append(actions, PruneAction{Kind: "build-cache", Args: buildBuilderPruneArgs()})
	}
	return actions
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestBuild|TestPruneActions" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all runtime tests + vet + build**

Run: `go test ./internal/runtime/... -v -count=1 && go vet ./internal/runtime/... && go build ./...`

Expected: All PASS, no vet issues, build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add docker prune arg builders and action selection for housekeeping"
```

---

### Task 2: Implement `dockerRuntime.Prune`/`DiskUsage` + Manager interface + stub

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — `Manager` interface
- Modify: `internal/runtime/runtime.go:113-119` — stub implementation block
- Modify: `internal/runtime/housekeeping.go` — add exec methods
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `PruneActions(opts PruneOptions) []PruneAction` from Task 1
- Produces: `func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) ([]PruneSummary, error)`, `func (r *dockerRuntime) DiskUsage(ctx context.Context) (string, error)`, and Manager interface methods `DiskUsage(ctx context.Context) (string, error)` / `Prune(ctx context.Context, opts PruneOptions) ([]PruneSummary, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/housekeeping_test.go`:

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{Containers: true})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("stub Prune() returned %d summaries, want 0", len(res))
	}
}

func TestStubDiskUsage(t *testing.T) {
	m := NewStub()
	out, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("stub DiskUsage() error = %v", err)
	}
	if out != "" {
		t.Fatalf("stub DiskUsage() = %q, want empty", out)
	}
}
```

Add `"context"` to the imports of `housekeeping_test.go`:

```go
import (
	"context"
	"reflect"
	"testing"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestStubDiskUsage" -v -count=1`

Expected: FAIL — `stubManager does not implement Manager` (missing `Prune`/`DiskUsage` methods)

- [ ] **Step 3: Add methods to the Manager interface**

In `internal/runtime/runtime.go`, inside the `Manager` interface (after the `Run` line):

```go
	Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts RunOptions) error
	DiskUsage(ctx context.Context) (string, error)
	Prune(ctx context.Context, opts PruneOptions) ([]PruneSummary, error)
```

- [ ] **Step 4: Add stub implementations**

In `internal/runtime/runtime.go`, after the existing stub `Run` method:

```go
func (m *stubManager) DiskUsage(ctx context.Context) (string, error) {
	return "", nil
}

func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) ([]PruneSummary, error) {
	return nil, nil
}
```

- [ ] **Step 5: Add exec implementations**

Append to `internal/runtime/housekeeping.go` (add imports `"context"`, `"fmt"`, `"os/exec"`, `"strings"` at the top of the file):

```go
func (r *dockerRuntime) DiskUsage(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", buildDiskUsageArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return string(out), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) ([]PruneSummary, error) {
	var summaries []PruneSummary
	var errs []string
	for _, action := range PruneActions(opts) {
		cmd := exec.CommandContext(ctx, "docker", action.Args...)
		out, err := cmd.CombinedOutput()
		summaries = append(summaries, PruneSummary{Kind: action.Kind, Output: string(out)})
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v\n%s", action.Kind, err, string(out)))
		}
	}
	if len(errs) > 0 {
		return summaries, fmt.Errorf("prune errors: %s", strings.Join(errs, "; "))
	}
	return summaries, nil
}
```

The resulting import block of `internal/runtime/housekeeping.go` must be:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/runtime/... -run "TestStubPrune|TestStubDiskUsage" -v -count=1`

Expected: PASS

- [ ] **Step 7: Run all runtime tests + vet + build**

Run: `go test ./internal/runtime/... -v -count=1 && go vet ./internal/runtime/... && go build ./...`

Expected: All PASS, no vet issues, build succeeds

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add DiskUsage and Prune to runtime.Manager"
```

---

### Task 3: Label Tengiz-built images so label-based pruning protects them

**Files:**
- Modify: `internal/builder/builder.go:69-71` — `buildWithDockerfile` build args
- Modify: `internal/builder/builder.go` — add `buildLabelArgs` helper
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `appName` and `env` already passed to `buildWithDockerfile`
- Produces: `func buildLabelArgs(appName, env string) []string`, images tagged `tengiz-apps/<app>:<env>-<id>` also carry labels `tengiz-app=<app>` and `tengiz-env=<env>`

- [ ] **Step 1: Write the failing test**

Append to `internal/builder/builder_test.go`:

```go
func TestBuildLabelArgs(t *testing.T) {
	got := buildLabelArgs("myapp", "staging")
	want := []string{"--label", "tengiz-app=myapp", "--label", "tengiz-env=staging"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildLabelArgs(myapp, staging) = %v, want %v", got, want)
	}

	gotDefault := buildLabelArgs("myapp", "")
	wantDefault := []string{"--label", "tengiz-app=myapp", "--label", "tengiz-env=production"}
	if !reflect.DeepEqual(gotDefault, wantDefault) {
		t.Fatalf("buildLabelArgs(myapp, \"\") = %v, want %v", gotDefault, wantDefault)
	}
}
```

Update the imports of `internal/builder/builder_test.go` to include `"reflect"`:

```go
import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/... -run "TestBuildLabelArgs" -v -count=1`

Expected: FAIL with `undefined: buildLabelArgs`

- [ ] **Step 3: Add the `buildLabelArgs` helper**

In `internal/builder/builder.go`, add this function (place it above `buildWithDockerfile`):

```go
func buildLabelArgs(appName, env string) []string {
	if env == "" {
		env = "production"
	}
	return []string{
		"--label", fmt.Sprintf("tengiz-app=%s", appName),
		"--label", fmt.Sprintf("tengiz-env=%s", env),
	}
}
```

- [ ] **Step 4: Wire labels into the docker build command**

In `internal/builder/builder.go`, change `buildWithDockerfile` so the build args include the labels:

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, buildLabelArgs(appName, env)...)
	args = append(args, "-t", tag, dir)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/builder/... -run "TestBuildLabelArgs" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run all builder tests + vet + build**

Run: `go test ./internal/builder/... -v -count=1 && go vet ./internal/builder/... && go build ./...`

Expected: All PASS, no vet issues, build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: label tengiz-built images for label-safe housekeeping"
```

---

### Task 4: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — new `cleanupCmd`, `addCleanupFlags`, `buildPruneOptions`; register in `init()`
- Create: `internal/cli/cmd_cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneActions(opts)`, `(*dockerRuntime).Prune(ctx, opts) ([]PruneSummary, error)`, `(*dockerRuntime).DiskUsage(ctx) (string, error)` from Tasks 1-2
- Produces: `tengiz cleanup [--containers|--images|--networks|--build-cache|--volumes|--all|--dry-run]` command; `func buildPruneOptions(cmd *cobra.Command) runtime.PruneOptions`; `func addCleanupFlags(cmd *cobra.Command)`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cmd_cleanup_test.go`:

```go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCommandRegistered(t *testing.T) {
	if findSubcommand(rootCmd, "cleanup") == nil {
		t.Fatal("cleanup command not registered on rootCmd")
	}
}

func TestCleanupFlagDefaults(t *testing.T) {
	c := &cobra.Command{}
	addCleanupFlags(c)
	checks := map[string]bool{
		"containers":  true,
		"images":      true,
		"networks":    true,
		"build-cache": true,
		"volumes":     false,
		"all":         false,
		"dry-run":     false,
	}
	for name, want := range checks {
		f := c.Flags().Lookup(name)
		if f == nil {
			t.Fatalf("cleanup missing --%s flag", name)
		}
		got, _ := c.Flags().GetBool(name)
		if got != want {
			t.Errorf("--%s default = %v, want %v", name, got, want)
		}
	}
}

func TestBuildPruneOptionsFromFlags(t *testing.T) {
	c := &cobra.Command{}
	addCleanupFlags(c)
	must := func(name, val string) {
		t.Helper()
		if err := c.Flags().Set(name, val); err != nil {
			t.Fatalf("Set(%s=%s): %v", name, val, err)
		}
	}
	must("all", "true")
	must("volumes", "true")
	must("containers", "false")
	must("build-cache", "false")

	opts := buildPruneOptions(c)
	if !opts.All {
		t.Error("All = false, want true")
	}
	if !opts.Volumes {
		t.Error("Volumes = false, want true")
	}
	if !opts.Images {
		t.Error("Images = false, want true (default)")
	}
	if !opts.Networks {
		t.Error("Networks = false, want true (default)")
	}
	if opts.Containers {
		t.Error("Containers = true, want false")
	}
	if opts.BuildCache {
		t.Error("BuildCache = true, want false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -run "TestCleanup|TestBuildPruneOptions" -v -count=1`

Expected: FAIL — `undefined: addCleanupFlags`, `undefined: buildPruneOptions`, `findSubcommand(rootCmd, "cleanup")` returns nil

- [ ] **Step 3: Add the command, flags, and helper**

In `internal/cli/root.go`, register the command in `init()` after `rootCmd.AddCommand(notificationCmd)`:

```go
	rootCmd.AddCommand(cleanupCmd)
```

Add the `cleanupCmd` definition and helpers (place below `psCmd` in the file):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources to reclaim disk space",
	Long: `Prunes stopped, unused containers, unused images, networks, build cache, and (optionally) volumes.

Tengiz-managed resources (containers and images labeled tengiz-app) are never removed.

Examples:
  tengiz cleanup                       # containers + dangling images + networks + build cache
  tengiz cleanup --all                 # also remove all unused images, not just dangling
  tengiz cleanup --volumes             # also remove unused named volumes
  tengiz cleanup --dry-run             # print the docker commands without running them`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		opts := buildPruneOptions(cmd)
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		before, _ := rt.DiskUsage(cmd.Context())
		if before != "" {
			fmt.Println(before)
		}

		if dryRun {
			for _, action := range runtime.PruneActions(opts) {
				fmt.Printf("[tengiz] [dry-run] docker %s\n", strings.Join(action.Args, " "))
			}
			return nil
		}

		summaries, err := rt.Prune(cmd.Context(), opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[tengiz] %v\n", err)
		}
		for _, s := range summaries {
			fmt.Fprintf(os.Stdout, "[tengiz] %s:\n%s", s.Kind, s.Output)
		}

		after, _ := rt.DiskUsage(cmd.Context())
		if after != "" {
			fmt.Println(after)
		}
		return nil
	},
}

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("containers", true, "prune stopped, unused containers")
	cmd.Flags().Bool("images", true, "prune unused images (dangling by default; add --all)")
	cmd.Flags().Bool("networks", true, "prune unused networks")
	cmd.Flags().Bool("build-cache", true, "prune build cache")
	cmd.Flags().Bool("volumes", false, "prune unused named volumes")
	cmd.Flags().Bool("all", false, "include all unused images, not just dangling")
	cmd.Flags().Bool("dry-run", false, "print the docker commands without running them")
}

func buildPruneOptions(cmd *cobra.Command) runtime.PruneOptions {
	return runtime.PruneOptions{
		All:        getBoolFlag(cmd, "all"),
		Containers: getBoolFlag(cmd, "containers"),
		Images:     getBoolFlag(cmd, "images"),
		Volumes:    getBoolFlag(cmd, "volumes"),
		Networks:   getBoolFlag(cmd, "networks"),
		BuildCache: getBoolFlag(cmd, "build-cache"),
	}
}

func getBoolFlag(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name)
	return v
}
```

In `init()`, register the flags (near the other `*.Flags()` calls at the bottom of `init()`):

```go
	addCleanupFlags(cleanupCmd)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/... -run "TestCleanup|TestBuildPruneOptions" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run all CLI tests + vet + build**

Run: `go test ./internal/cli/... -v -count=1 && go vet ./internal/cli/... && go build ./...`

Expected: All PASS, no vet issues, build succeeds

- [ ] **Step 6: Verify manually against a real Docker daemon (if available)**

```bash
go build -o tengiz .
./tengiz cleanup --dry-run
```

Expected: prints `[tengiz] [dry-run] docker container prune --force --filter label!=tengiz-app`, `docker image prune --force --filter label!=tengiz-app`, `docker network prune --force`, `docker builder prune --force`, preceded by a `docker system df` table. If Docker is not installed, the command returns `docker: docker not found in PATH` — acceptable.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cmd_cleanup_test.go
git commit -m "feat: add tengiz cleanup command for docker housekeeping"
```

---

### Task 5: Document the feature and mark it implemented

**Files:**
- Modify: `README.md` — Features bullet + CLI reference section
- Modify: `docs/FUTURES_FEATURES.md` — mark #6 Docker Housekeeping implemented

**Interfaces:**
- Consumes: final `tengiz cleanup` behavior from Task 4
- Produces: user-facing documentation and roadmap status update

- [ ] **Step 1: Add a Features bullet to the README**

In `README.md`, after the `- **Deployment history** — Track deploy versions with automatic rollback foundation (last 10 deployments preserved).` line (line 20), insert:

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes stopped/unused containers, dangling images, networks, and build cache. Label-safe: Tengiz-managed apps are never touched.
```

- [ ] **Step 2: Add a CLI reference section to the README**

In `README.md`, after the `### tengiz ps` section block (the `Output: NAME, STATE (running/stopped), PORT, ENVIRONMENT, HEALTH.` paragraph on line 150) and before `### tengiz logs`, insert:

```markdown
### `tengiz cleanup [flags]`

Prune unused Docker resources to reclaim disk space. Tengiz-managed resources (containers and images labeled `tengiz-app`) are never removed.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped, unused containers (default: true) |
| `--images` | Prune unused images (default: true) |
| `--networks` | Prune unused networks (default: true) |
| `--build-cache` | Prune build cache (default: true) |
| `--volumes` | Prune unused named volumes (default: false) |
| `--all` | Include all unused images, not just dangling (default: false) |
| `--dry-run` | Print the docker commands that would run without executing them |

Prints `docker system df` before and after the run to show reclaimed space. Example:

```
tengiz cleanup --dry-run
tengiz cleanup --all --volumes
```
```

- [ ] **Step 3: Mark the feature as implemented in the roadmap**

In `docs/FUTURES_FEATURES.md`, change the P0 row #6 marker:

From: `| 6 | **Docker Housekeeping** ⬜ | High...`

To: `| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based docker system prune. tengiz cleanup. |`

Precisely, replace the string `**Docker Housekeeping** ⬜` with `**Docker Housekeeping** ✅`.

Then add a row to the `✅ Implemented Features (Not Pending)` table, directly after the `|---|---------|---|---|---|--------|` row:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-10) |
```

- [ ] **Step 4: Run the full test suite + vet + build**

Run: `go test ./... -v -count=1 && go vet ./... && go build -o tengiz .`

Expected: All tests PASS (note: `proxy` tests take ~2s each due to TCP dial timeouts; `idle` tests are time-sensitive — both are pre-existing behavior). `vet` clean, build succeeds.

- [ ] **Step 5: Self-review against the spec**

Check the feature spec from `docs/FUTURES_FEATURES.md` #6:
- `tengiz cleanup` command ✅ (Task 4)
- Label-based `docker system prune` using `tengiz-app` label filter ✅ (Tasks 1-4 — containers + images filtered with `label!=tengiz-app`; images labeled at build time in Task 3)
- Disk space reclaiming, single-server safe ✅ (defaults conservative: dangling-only images, volumes off; `--all`/`--volumes` opt-in)

Placeholder scan: no `TBD`/`TODO`/`implement later` patterns — every step ships complete code.

Type consistency check:
- `PruneOptions{All, Containers, Images, Volumes, Networks, BuildCache bool}` — used identically in Task 1 (`PruneActions`), Task 2 (interface + stub), Task 4 (`buildPruneOptions`)
- `PruneAction{Kind, Args}` — Task 1 producer, consumed in Task 2 (`Prune`) and Task 4 (dry-run loop)
- `PruneSummary{Kind, Output}` — Task 2 producer, consumed in Task 4 (summary print loop)
- `func (r *dockerRuntime) DiskUsage(ctx context.Context) (string, error)` — Task 2 producer, consumed in Task 4
- `addCleanupFlags(cmd *cobra.Command)` and `buildPruneOptions(cmd *cobra.Command) runtime.PruneOptions` — Task 4 only, consistent
- `getBoolFlag(cmd *cobra.Command, name string) bool` helper — defined once, used internally only

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark docker housekeeping implemented"
```

---

## Notes for the Implementer

- `findSubcommand` used in `TestCleanupCommandRegistered` is already defined in `internal/cli/cmd_secret_test.go` (package `cli`) — do not redefine it.
- The `--env` global persistent flag is inherited by `cleanupCmd` automatically; the cleanup handler intentionally ignores it because housekeeping operates at the Docker daemon level.
- `docker image prune -a` never removes images referenced by a running or stopped container; this is the safety net that protects scale-to-zero Tengiz images even before the Task 3 labels are applied to newly built images.
- If any individual prune subcommand fails (e.g. Docker daemon is busy), `Prune` still runs the remaining actions and returns an aggregated error; the CLI prints the error to stderr and continues.