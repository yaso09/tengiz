# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that prunes unused Docker resources (containers, images, networks, volumes, build cache) while always preserving Tengiz-managed resources via label-based filtering, plus optional periodic cleanup via `--every`.

**Architecture:** A new `internal/cleanup` package owns pruning. Pure arg-builder functions (`pruneContainersArgs`, `pruneImagesArgs`, etc.) construct `docker` CLI invocations and are unit-tested without a daemon; a thin `Manager` executes them via `os/exec`. All Tengiz-built images gain `tengiz-app`/`tengiz-env` labels at build time (Dockerfile and Nixpacks paths) so the `--filter label!=tengiz-app` guards protect both containers and images. A `tengiz cleanup` cobra command wires options, confirmation, `--dry-run`, and `--every <duration>` periodic mode.

**Tech Stack:** Go 1.26, Cobra (CLI), Docker CLI via `os/exec` (no Docker SDK), existing `internal/runtime` label constants.

## Global Constraints

- Use the `docker` CLI via `os/exec` — never the Docker SDK (repo rule)
- Protect every Tengiz-managed resource with `--filter label!=tengiz-app` (containers/networks) and `--filter label!=tengiz-app` + `--filter label!=tengiz-env` (images)
- Volumes and build cache are pruned ONLY when explicitly requested via `--volumes`/`--build-cache`/`--all` — never by default
- Default (no category flags) prunes exactly: containers, images, networks
- `--dry-run` prints the exact `docker` commands and executes nothing; must NOT require docker to be installed
- `--every <duration>` periodic mode runs non-interactively (no confirmation prompt, equivalent to `--force`)
- No new external Go dependencies (stdlib only: `context`, `os/exec`, `time`, `strings`, `fmt`, `bufio`, `os/signal`)
- Cleanup is host-wide, NOT env-scoped: labels protect resources across all environments
- Label keys are defined once in `internal/runtime` as exported constants `LabelAppKey`/`LabelEnvKey` and reused by `builder` and `cleanup`
- All new code must pass `go vet ./...` and `go test ./... -count=1`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/runtime.go` | Export `LabelAppKey`/`LabelEnvKey` constants (existing unexported `labelKey`/`envLabelKey` become aliases) |
| `internal/builder/builder.go` | Add `--label tengiz-app=<app>` / `--label tengiz-env=<env>` to docker build and nixpacks build via new `imageLabelArgs`/`buildDockerArgs`/`buildNixpacksArgs` helpers |
| `internal/builder/builder_test.go` | Tests for label args and build-arg construction |
| `internal/cleanup/cleanup.go` (new) | `Options`, `Category`, `Result`, `Cleaner` interface, `Manager`, pure `prune*Args`/`Commands`/`extractReclaimedSpace` functions |
| `internal/cleanup/cleanup_test.go` (new) | Unit tests for options resolution, arg builders, `Commands`, reclaimed-space parsing |
| `internal/cli/cleanup.go` (new) | `tengiz cleanup` cobra command, flags, confirmation, dry-run, periodic mode |
| `internal/cli/cleanup_test.go` (new) | Registration, flag parsing, category defaults, dry-run/force behavior with a fake `Cleaner` |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` |
| `README.md` | Document `tengiz cleanup` in CLI Reference |

---

### Task 1: Label Tengiz-built images

**Files:**
- Modify: `internal/runtime/runtime.go` — export label constants
- Modify: `internal/runtime/docker.go:76-77` — alias existing constants to exported ones
- Modify: `internal/builder/builder.go` — add `imageLabelArgs`, `buildDockerArgs`, `buildNixpacksArgs`; use them in `buildWithDockerfile`/`buildWithNixpacks`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `runtime.LabelAppKey string` (= `"tengiz-app"`), `runtime.LabelEnvKey string` (= `"tengiz-env"`), `imageLabelArgs(appName, env string) []string`, `buildDockerArgs(appName, env, tag, dir string, secretArgs []string) []string`, `buildNixpacksArgs(appName, env, tag, dir string, cfg *types.NixpacksConfig) []string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/builder/builder_test.go
func TestImageLabelArgs(t *testing.T) {
	got := imageLabelArgs("myapp", "production")
	want := []string{
		"--label", "tengiz-app=myapp",
		"--label", "tengiz-env=production",
	}
	if len(got) != len(want) {
		t.Fatalf("imageLabelArgs() = %v (len=%d), want %v (len=%d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("imageLabelArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDockerBuildArgsIncludesLabels(t *testing.T) {
	args := buildDockerArgs("myapp", "staging", "tengiz-apps/myapp:staging-123", ".", []string{"--secret", "id=NPM_TOKEN,src=/tmp/t"})
	got := strings.Join(args, " ")
	for _, want := range []string{
		"build",
		"--label", "tengiz-app=myapp",
		"--label", "tengiz-env=staging",
		"-t", "tengiz-apps/myapp:staging-123",
		".",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("buildDockerArgs() missing %q in %q", want, got)
		}
	}
}

func TestNixpacksBuildArgsIncludesLabels(t *testing.T) {
	args := buildNixpacksArgs("myapp", "prod", "tengiz-apps/myapp:prod-123", ".", nil)
	got := strings.Join(args, " ")
	for _, want := range []string{
		"build",
		"--name", "tengiz-apps/myapp:prod-123",
		"--label", "tengiz-app=myapp",
		"--label", "tengiz-env=prod",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("buildNixpacksArgs() missing %q in %q", want, got)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/builder/ -run "TestImageLabelArgs|TestDockerBuildArgsIncludesLabels|TestNixpacksBuildArgsIncludesLabels" -v -count=1`
Expected: FAIL with `undefined: imageLabelArgs`, `undefined: buildDockerArgs`, `undefined: buildNixpacksArgs`

- [ ] **Step 3: Implement**

Add exported constants in `internal/runtime/runtime.go` (after the `ContainerName` function):
```go
const (
	LabelAppKey = "tengiz-app"
	LabelEnvKey = "tengiz-env"
)
```

Alias the existing constants in `internal/runtime/docker.go:76-77`:
```go
const labelKey = LabelAppKey
const envLabelKey = LabelEnvKey
```

Add label/build helpers in `internal/builder/builder.go` (import `"github.com/yaso09/tengiz/internal/runtime"`):
```go
func imageLabelArgs(appName, env string) []string {
	return []string{
		"--label", fmt.Sprintf("%s=%s", runtime.LabelAppKey, appName),
		"--label", fmt.Sprintf("%s=%s", runtime.LabelEnvKey, env),
	}
}

func buildDockerArgs(appName, env, tag, dir string, secretArgs []string) []string {
	args := []string{"build"}
	args = append(args, secretArgs...)
	args = append(args, imageLabelArgs(appName, env)...)
	args = append(args, "-t", tag, dir)
	return args
}

func buildNixpacksArgs(appName, env, tag, dir string, cfg *types.NixpacksConfig) []string {
	args := []string{"build", dir, "--name", tag}
	args = append(args, imageLabelArgs(appName, env)...)
	if cfg != nil {
		if len(cfg.Packages) > 0 {
			args = append(args, "--pkgs", strings.Join(cfg.Packages, ","))
		}
		if len(cfg.AptPackages) > 0 {
			args = append(args, "--apt-pkgs", strings.Join(cfg.AptPackages, ","))
		}
		if cfg.Cmd != "" {
			args = append(args, "--cmd", cfg.Cmd)
		}
	}
	return args
}
```

Refactor `buildWithDockerfile` in `internal/builder/builder.go:69-72` — replace the inline args block:
```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```
with:
```go
	args := buildDockerArgs(appName, env, tag, dir, b.buildSecretArgs())
```

Refactor `buildWithNixpacks` in `internal/builder/builder.go:139-150` — replace the inline args block:
```go
	args := []string{"build", dir, "--name", tag}
	if b.nixpacksCfg != nil {
		if len(b.nixpacksCfg.Packages) > 0 {
			args = append(args, "--pkgs", strings.Join(b.nixpacksCfg.Packages, ","))
		}
		if len(b.nixpacksCfg.AptPackages) > 0 {
			args = append(args, "--apt-pkgs", strings.Join(b.nixpacksCfg.AptPackages, ","))
		}
		if b.nixpacksCfg.Cmd != "" {
			args = append(args, "--cmd", b.nixpacksCfg.Cmd)
		}
	}
```
with:
```go
	args := buildNixpacksArgs(appName, env, tag, dir, b.nixpacksCfg)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/builder/ -run "TestImageLabelArgs|TestDockerBuildArgsIncludesLabels|TestNixpacksBuildArgsIncludesLabels|TestBuildWithDeploymentIDCompiles" -v -count=1`
Expected: PASS

- [ ] **Step 5: Run full package tests**

Run: `go build ./... && go vet ./... && go test ./internal/runtime/... ./internal/builder/... -count=1`
Expected: no errors; all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/docker.go internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: label Tengiz-built images for cleanup protection"
```

---

### Task 2: Cleanup package — options and pure command builders

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Create: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `runtime.LabelAppKey`, `runtime.LabelEnvKey`
- Produces: `Options`, `Category`, `DefaultCategories()`, `Options.Categories()`, `Options.CategoryNames()`, `Options.Selected()`, `Commands(o Options) [][]string`, `pruneContainersArgs(until string) []string`, `pruneImagesArgs(until string) []string`, `pruneNetworksArgs(until string) []string`, `pruneVolumesArgs() []string`, `pruneBuildCacheArgs() []string`, `extractReclaimedSpace(outputs []string) string`, `Result`, `Cleaner` interface, `Manager`, `NewManager()`, `(*Manager).Clean`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cleanup/cleanup_test.go
package cleanup

import (
	"reflect"
	"testing"
)

func TestPruneContainersArgs(t *testing.T) {
	got := pruneContainersArgs("")
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneContainersArgs() = %v, want %v", got, want)
	}
}

func TestPruneContainersArgsWithUntil(t *testing.T) {
	got := pruneContainersArgs("24h")
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app", "--filter", "until=24h"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneContainersArgs(24h) = %v, want %v", got, want)
	}
}

func TestPruneImagesArgs(t *testing.T) {
	got := pruneImagesArgs("")
	want := []string{
		"image", "prune", "-a", "-f",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneImagesArgs() = %v, want %v", got, want)
	}
}

func TestPruneNetworksArgs(t *testing.T) {
	got := pruneNetworksArgs("")
	want := []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneNetworksArgs() = %v, want %v", got, want)
	}
}

func TestPruneVolumesArgs(t *testing.T) {
	got := pruneVolumesArgs()
	want := []string{"volume", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneVolumesArgs() = %v, want %v", got, want)
	}
}

func TestPruneBuildCacheArgs(t *testing.T) {
	got := pruneBuildCacheArgs()
	want := []string{"builder", "prune", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pruneBuildCacheArgs() = %v, want %v", got, want)
	}
}

func TestOptionsCategories(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want []Category
	}{
		{name: "empty defaults to none", opts: Options{}, want: nil},
		{name: "all", opts: Options{All: true}, want: []Category{CategoryContainers, CategoryImages, CategoryNetworks, CategoryVolumes, CategoryBuildCache}},
		{name: "containers only", opts: Options{Containers: true}, want: []Category{CategoryContainers}},
		{name: "images and networks", opts: Options{Images: true, Networks: true}, want: []Category{CategoryImages, CategoryNetworks}},
		{name: "volumes only", opts: Options{Volumes: true}, want: []Category{CategoryVolumes}},
		{name: "build cache only", opts: Options{BuildCache: true}, want: []Category{CategoryBuildCache}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.Categories(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Categories() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOptionsSelected(t *testing.T) {
	if Options{}.Selected() {
		t.Error("empty Options.Selected() = true, want false")
	}
	for _, o := range []Options{
		{All: true}, {Containers: true}, {Images: true}, {Networks: true}, {Volumes: true}, {BuildCache: true},
	} {
		if !o.Selected() {
			t.Errorf("Selected() = false, want true for %+v", o)
		}
	}
}

func TestDefaultCategories(t *testing.T) {
	want := []Category{CategoryContainers, CategoryImages, CategoryNetworks}
	if got := DefaultCategories(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultCategories() = %v, want %v", got, want)
	}
}

func TestCommandsResolvesDefaults(t *testing.T) {
	cmds := Commands(Options{})
	want := [][]string{
		{"container", "prune", "-f", "--filter", "label!=tengiz-app"},
		{"image", "prune", "-a", "-f", "--filter", "label!=tengiz-app", "--filter", "label!=tengiz-env"},
		{"network", "prune", "-f", "--filter", "label!=tengiz-app"},
	}
	if !reflect.DeepEqual(cmds, want) {
		t.Fatalf("Commands(empty) = %v, want %v", cmds, want)
	}
}

func TestCommandsForExplicitCategories(t *testing.T) {
	cmds := Commands(Options{Volumes: true, BuildCache: true, Until: "7d"})
	want := [][]string{
		{"volume", "prune", "-f"},
		{"builder", "prune", "-f"},
	}
	if !reflect.DeepEqual(cmds, want) {
		t.Fatalf("Commands() = %v, want %v", cmds, want)
	}
}

func TestCategoryNames(t *testing.T) {
	opts := Options{All: true}
	want := []string{"containers", "images", "networks", "volumes", "build-cache"}
	if got := opts.CategoryNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("CategoryNames() = %v, want %v", got, want)
	}
}

func TestExtractReclaimedSpace(t *testing.T) {
	outputs := []string{
		"Deleted Containers:\nabc123\n\nTotal reclaimed space: 2.716MB\n",
		"Deleted Images:\nxyz\n\nTotal reclaimed space: 0B\n",
	}
	if got := extractReclaimedSpace(outputs); got != "2.716MB" {
		t.Fatalf("extractReclaimedSpace() = %q, want %q", got, "2.716MB")
	}
}

func TestExtractReclaimedSpaceEmpty(t *testing.T) {
	if got := extractReclaimedSpace(nil); got != "0B" {
		t.Fatalf("extractReclaimedSpace(nil) = %q, want %q", got, "0B")
	}
}

func TestManagerSatisfiesCleaner(t *testing.T) {
	var _ Cleaner = &Manager{}
}
```

Note: `Manager.Clean` executes real `docker` prune commands against the daemon, so it is deliberately NOT unit-tested (no destructive commands in tests). Its behavior is fully determined by `Commands`/`argsForCategory`, which are covered above, and by `extractReclaimedSpace`.

- [ ] **Step 2: Create the package skeleton and run tests to verify they fail**

Create `internal/cleanup/cleanup.go` with only the package declaration (so the package compiles as an empty unit and the test file surfaces the undefined symbols):

```go
package cleanup
```

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: FAIL with `undefined: pruneContainersArgs`, `undefined: pruneImagesArgs`, `undefined: pruneNetworksArgs`, `undefined: pruneVolumesArgs`, `undefined: pruneBuildCacheArgs`, `undefined: Commands`, `undefined: DefaultCategories`, `undefined: Options`, `undefined: Category`, `undefined: CategoryNames`, `undefined: extractReclaimedSpace`, `undefined: Manager`, `undefined: Cleaner`, `undefined: Result`

- [ ] **Step 3: Implement `internal/cleanup/cleanup.go`**

```go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/yaso09/tengiz/internal/runtime"
)

type Category string

const (
	CategoryContainers Category = "containers"
	CategoryImages     Category = "images"
	CategoryNetworks   Category = "networks"
	CategoryVolumes    Category = "volumes"
	CategoryBuildCache Category = "build-cache"
)

type Options struct {
	All        bool
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	DryRun     bool
	Until      string
}

func (o Options) Categories() []Category {
	var cats []Category
	if o.All || o.Containers {
		cats = append(cats, CategoryContainers)
	}
	if o.All || o.Images {
		cats = append(cats, CategoryImages)
	}
	if o.All || o.Networks {
		cats = append(cats, CategoryNetworks)
	}
	if o.All || o.Volumes {
		cats = append(cats, CategoryVolumes)
	}
	if o.All || o.BuildCache {
		cats = append(cats, CategoryBuildCache)
	}
	return cats
}

func (o Options) Selected() bool {
	return o.All || o.Containers || o.Images || o.Networks || o.Volumes || o.BuildCache
}

func (o Options) CategoryNames() []string {
	cats := o.Categories()
	names := make([]string, len(cats))
	for i, c := range cats {
		names[i] = string(c)
	}
	return names
}

func DefaultCategories() []Category {
	return []Category{CategoryContainers, CategoryImages, CategoryNetworks}
}

func resolveCategories(o Options) []Category {
	if cats := o.Categories(); len(cats) > 0 {
		return cats
	}
	return DefaultCategories()
}

func pruneContainersArgs(until string) []string {
	args := []string{"container", "prune", "-f", "--filter", "label!=" + runtime.LabelAppKey}
	if until != "" {
		args = append(args, "--filter", "until="+until)
	}
	return args
}

func pruneImagesArgs(until string) []string {
	args := []string{"image", "prune", "-a", "-f",
		"--filter", "label!=" + runtime.LabelAppKey,
		"--filter", "label!=" + runtime.LabelEnvKey,
	}
	if until != "" {
		args = append(args, "--filter", "until="+until)
	}
	return args
}

func pruneNetworksArgs(until string) []string {
	args := []string{"network", "prune", "-f", "--filter", "label!=" + runtime.LabelAppKey}
	if until != "" {
		args = append(args, "--filter", "until="+until)
	}
	return args
}

func pruneVolumesArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func pruneBuildCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func argsForCategory(cat Category, until string) []string {
	switch cat {
	case CategoryContainers:
		return pruneContainersArgs(until)
	case CategoryImages:
		return pruneImagesArgs(until)
	case CategoryNetworks:
		return pruneNetworksArgs(until)
	case CategoryVolumes:
		return pruneVolumesArgs()
	case CategoryBuildCache:
		return pruneBuildCacheArgs()
	}
	return nil
}

func Commands(o Options) [][]string {
	cats := resolveCategories(o)
	cmds := make([][]string, 0, len(cats))
	for _, c := range cats {
		if args := argsForCategory(c, o.Until); len(args) > 0 {
			cmds = append(cmds, args)
		}
	}
	return cmds
}

type Result struct {
	ReclaimedSpace string
	Categories     []Category
}

func (r *Result) CategoryNames() []string {
	names := make([]string, len(r.Categories))
	for i, c := range r.Categories {
		names[i] = string(c)
	}
	return names
}

func extractReclaimedSpace(outputs []string) string {
	for _, out := range outputs {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Total reclaimed space:") {
				return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
			}
		}
	}
	return "0B"
}

type Cleaner interface {
	Clean(ctx context.Context, opts Options) (*Result, error)
}

type Manager struct{}

func NewManager() (Cleaner, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &Manager{}, nil
}

func (m *Manager) Clean(ctx context.Context, opts Options) (*Result, error) {
	res := &Result{Categories: resolveCategories(opts)}
	var outputs []string
	for _, args := range Commands(opts) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return res, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
		if s := strings.TrimSpace(string(out)); s != "" {
			outputs = append(outputs, s)
		}
	}
	res.ReclaimedSpace = extractReclaimedSpace(outputs)
	return res, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/... -v -count=1`
Expected: all tests PASS

- [ ] **Step 5: Run vet and commit**

Run: `go vet ./internal/cleanup/...`
Expected: no output (clean)

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup package with docker prune command builders"
```

---

### Task 3: CLI command `tengiz cleanup`

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go:38` — register `cleanupCmd`

**Interfaces:**
- Consumes: `cleanup.Options`, `cleanup.Cleaner`, `cleanup.NewManager`, `cleanup.Commands`, `cleanup.Result`
- Produces: `cleanupCmd *cobra.Command`, `addCleanupFlags(cmd *cobra.Command)`, `cleanupFlags(cmd *cobra.Command) (cleanup.Options, error)`, `defaultedCleanupOptions(opts cleanup.Options) cleanup.Options`, `confirmCleanup(opts cleanup.Options) (bool, error)`, `printCleanupResult(res *cleanup.Result)`, package var `newCleaner func() (cleanup.Cleaner, error)`

- [ ] **Step 1: Register the command and write the failing tests**

Add to `internal/cli/root.go` `init()` (after `rootCmd.AddCommand(notificationCmd)` on line 75):
```go
	rootCmd.AddCommand(cleanupCmd)
```

```go
// internal/cli/cleanup_test.go
package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

func newTestCleanupCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	addCleanupFlags(cmd)
	return cmd
}

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"all", "containers", "images", "networks", "volumes", "build-cache", "dry-run", "force", "until", "every"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanupCmd missing --%s flag", flag)
		}
	}
}

func TestCleanupFlagsParsing(t *testing.T) {
	cmd := newTestCleanupCmd()
	if err := cmd.ParseFlags([]string{"--all", "--dry-run", "--until", "24h"}); err != nil {
		t.Fatal(err)
	}
	opts, err := cleanupFlags(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.All || !opts.DryRun || opts.Until != "24h" {
		t.Fatalf("cleanupFlags() = %+v, want All+DryRun+Until=24h", opts)
	}
	if got := opts.Categories(); len(got) != 5 {
		t.Fatalf("expected 5 categories for --all, got %v", got)
	}
}

func TestCleanupFlagsInvalidUntil(t *testing.T) {
	cmd := newTestCleanupCmd()
	if err := cmd.ParseFlags([]string{"--until", "not-a-duration"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanupFlags(cmd); err == nil {
		t.Fatal("expected error for invalid --until duration")
	}
}

func TestDefaultedCleanupOptions(t *testing.T) {
	opts := defaultedCleanupOptions(cleanup.Options{})
	if !opts.Containers || !opts.Images || !opts.Networks {
		t.Fatalf("defaultedCleanupOptions() = %+v, want containers+images+networks", opts)
	}
	if opts.Volumes || opts.BuildCache {
		t.Fatal("defaults must NOT include volumes or build-cache")
	}

	explicit := cleanup.Options{Volumes: true}
	got := defaultedCleanupOptions(explicit)
	if !got.Volumes || got.Containers || got.Images {
		t.Fatalf("explicit options must be preserved, got %+v", got)
	}
}

func TestCleanupDryRunPrintsCommandsWithoutDocker(t *testing.T) {
	called := false
	old := newCleaner
	newCleaner = func() (cleanup.Cleaner, error) {
		called = true
		return nil, nil
	}
	defer func() { newCleaner = old }()

	cleanupCmd.SetArgs([]string{"--dry-run"})
	out := captureOutput(func() { cleanupCmd.Execute() })

	if called {
		t.Fatal("dry run must not construct a Cleaner (must not require docker)")
	}
	for _, want := range []string{"docker container prune", "docker image prune", "docker network prune", "tengiz-app"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q: %s", want, out)
		}
	}
}

type fakeCleaner struct {
	calls int
	res   *cleanup.Result
	err   error
}

func (f *fakeCleaner) Clean(ctx context.Context, opts cleanup.Options) (*cleanup.Result, error) {
	f.calls++
	return f.res, f.err
}

func TestCleanupForceRunsCleaner(t *testing.T) {
	fake := &fakeCleaner{res: &cleanup.Result{
		ReclaimedSpace: "1.5GB",
		Categories:     []cleanup.Category{cleanup.CategoryContainers, cleanup.CategoryImages, cleanup.CategoryNetworks},
	}}
	old := newCleaner
	newCleaner = func() (cleanup.Cleaner, error) { return fake, nil }
	defer func() { newCleaner = old }()

	cleanupCmd.SetArgs([]string{"--force"})
	out := captureOutput(func() { cleanupCmd.Execute() })

	if fake.calls != 1 {
		t.Fatalf("Cleaner.Clean calls = %d, want 1", fake.calls)
	}
	if !strings.Contains(out, "1.5GB") {
		t.Errorf("output missing reclaimed space %q: %s", "1.5GB", out)
	}
}

func TestCleanupExplicitCategoriesForce(t *testing.T) {
	fake := &fakeCleaner{res: &cleanup.Result{ReclaimedSpace: "0B", Categories: []cleanup.Category{cleanup.CategoryVolumes}}}
	old := newCleaner
	newCleaner = func() (cleanup.Cleaner, error) { return fake, nil }
	defer func() { newCleaner = old }()

	cleanupCmd.SetArgs([]string{"--force", "--volumes"})
	captureOutput(func() { cleanupCmd.Execute() })

	if fake.calls != 1 {
		t.Fatalf("Cleaner.Clean calls = %d, want 1", fake.calls)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`
Expected: FAIL — `undefined: cleanupCmd` and package `cleanup` not yet imported/used

- [ ] **Step 3: Implement `internal/cli/cleanup.go`**

```go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/cleanup"
)

var newCleaner = cleanup.NewManager

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources, protecting Tengiz-managed ones",
	Long: `Prunes unused Docker containers, images, networks, and (optionally) volumes
and build cache. Every resource Tengiz manages (labeled tengiz-app=*) is always
preserved.

By default only containers, images, and networks are pruned. Add --volumes and
--build-cache explicitly, or use --all for everything. Use --dry-run to preview
without deleting anything. Use --every <duration> to run cleanup on an interval
(e.g. --every 24h) until interrupted.`,
	RunE: cleanupRun,
}

func init() {
	addCleanupFlags(cleanupCmd)
}

func addCleanupFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("all", false, "prune all categories including volumes and build cache")
	cmd.Flags().Bool("containers", false, "prune stopped containers")
	cmd.Flags().Bool("images", false, "prune unused images")
	cmd.Flags().Bool("networks", false, "prune unused networks")
	cmd.Flags().Bool("volumes", false, "prune unused volumes (opt-in)")
	cmd.Flags().Bool("build-cache", false, "prune Docker build cache (opt-in)")
	cmd.Flags().Bool("dry-run", false, "show what would be pruned without deleting anything")
	cmd.Flags().BoolP("force", "f", false, "skip the confirmation prompt")
	cmd.Flags().String("until", "", "only prune resources older than this duration (e.g. 24h, 7d)")
	cmd.Flags().String("every", "", "run cleanup periodically (e.g. 24h) until interrupted")
}

func cleanupFlags(cmd *cobra.Command) (cleanup.Options, error) {
	all, _ := cmd.Flags().GetBool("all")
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	networks, _ := cmd.Flags().GetBool("networks")
	volumes, _ := cmd.Flags().GetBool("volumes")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	until, _ := cmd.Flags().GetString("until")

	if until != "" {
		if _, err := time.ParseDuration(until); err != nil {
			return cleanup.Options{}, fmt.Errorf("invalid --until duration %q: %w", until, err)
		}
	}

	return cleanup.Options{
		All:        all,
		Containers: containers,
		Images:     images,
		Networks:   networks,
		Volumes:    volumes,
		BuildCache: buildCache,
		DryRun:     dryRun,
		Until:      until,
	}, nil
}

func defaultedCleanupOptions(opts cleanup.Options) cleanup.Options {
	if opts.Selected() {
		return opts
	}
	opts.Containers = true
	opts.Images = true
	opts.Networks = true
	return opts
}

func cleanupRun(cmd *cobra.Command, args []string) error {
	opts, err := cleanupFlags(cmd)
	if err != nil {
		return err
	}
	opts = defaultedCleanupOptions(opts)

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")
	every, _ := cmd.Flags().GetString("every")

	if dryRun {
		fmt.Printf("[tengiz] dry run — would prune: %s\n", strings.Join(opts.CategoryNames(), ", "))
		for _, args := range cleanup.Commands(opts) {
			fmt.Printf("  docker %s\n", strings.Join(args, " "))
		}
		return nil
	}

	if !force && every == "" {
		ok, err := confirmCleanup(opts)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("[tengiz] cleanup aborted.")
			return nil
		}
	}

	c, err := newCleaner()
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}

	if every == "" {
		res, err := c.Clean(cmd.Context(), opts)
		if err != nil {
			return err
		}
		printCleanupResult(res)
		return nil
	}

	dur, err := time.ParseDuration(every)
	if err != nil {
		return fmt.Errorf("invalid --every duration %q: %w", every, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Printf("[tengiz] running cleanup every %s (Ctrl-C to stop)\n", dur)
	ticker := time.NewTicker(dur)
	defer ticker.Stop()
	for {
		res, err := c.Clean(ctx, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[tengiz] cleanup error: %v\n", err)
		} else {
			printCleanupResult(res)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func confirmCleanup(opts cleanup.Options) (bool, error) {
	fmt.Printf("[tengiz] will prune: %s\n", strings.Join(opts.CategoryNames(), ", "))
	fmt.Println("[tengiz] Tengiz-managed resources (labeled tengiz-app=*) will be preserved.")
	fmt.Print("Continue? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func printCleanupResult(res *cleanup.Result) {
	fmt.Printf("[tengiz] pruned: %s\n", strings.Join(res.CategoryNames(), ", "))
	fmt.Printf("[tengiz] total reclaimed space: %s\n", res.ReclaimedSpace)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanup" -v -count=1`
Expected: all `TestCleanup*` tests PASS

- [ ] **Step 5: Run full verification**

Run: `go build ./... && go vet ./... && go test ./internal/cli/... ./internal/cleanup/... -count=1`
Expected: no errors; all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat: add tengiz cleanup command"
```

---

### Task 4: Documentation and final verification

**Files:**
- Modify: `README.md` — add `tengiz cleanup` to CLI Reference

- [ ] **Step 1: Add README documentation**

Insert a new section in `README.md` under the CLI Reference (after the `### tengiz volume` block ends at line 302, before `### tengiz preview`):

```markdown
### `tengiz cleanup`

Prune unused Docker resources (containers, images, networks, and optionally
volumes and build cache). Resources managed by Tengiz (labeled `tengiz-app=*`)
are always preserved. Use `--dry-run` to preview without deleting anything.

```bash
tengiz cleanup                  # prune containers + images + networks (with confirmation)
tengiz cleanup --force          # skip the confirmation prompt
tengiz cleanup --dry-run        # show the docker commands without running them
tengiz cleanup --all            # also prune volumes and build cache
tengiz cleanup --volumes        # prune only unused volumes
tengiz cleanup --build-cache    # prune only the Docker build cache
tengiz cleanup --until 7d       # only prune resources older than 7 days
tengiz cleanup --every 24h      # run cleanup every 24h until interrupted
```
```

- [ ] **Step 2: Run the full test suite and build**

Run: `go build -o tengiz . && go vet ./... && go test ./... -count=1`
Expected: build succeeds, `go vet` clean, all tests PASS

- [ ] **Step 3: Manual smoke test (requires a Docker daemon)**

Run:
```bash
./tengiz cleanup --dry-run
./tengiz cleanup --force
```
Expected: `--dry-run` prints exactly three commands (`docker container prune ...`, `docker image prune ...`, `docker network prune ...`); `--force` executes them and prints `[tengiz] total reclaimed space: ...`. If no non-Tengiz junk exists, docker prints nothing and reclaimed space is `0B`.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage:** The FUTURES_FEATURES.md feature #6 (Docker Housekeeping) requires label-based `docker system prune`-style cleanup and a `tengiz cleanup` command. Tasks 2–3 deliver the `tengiz cleanup` command with per-category pruning (containers/images/networks/volumes/build-cache) and label protection. Task 1 makes label protection actually work for images by labeling them at build time. Periodic cleanup (the "DockerCleanupJob" analog) is delivered via `--every` in Task 3. The opt-in policy for the dangerous categories (volumes, build cache) matches the spec's caution. Documentation is covered in Task 4.

**2. Placeholder scan:** No TBD/TODO/placeholder content. Every step contains complete code and exact commands. The `newTestCleanupCmd` helper is fully specified in Task 3 Step 1.

**3. Type consistency:** `cleanup.Options`/`Options.Categories()`/`CategoryNames()`/`Selected()`/`Commands()` are defined in Task 2 and consumed identically in Task 3. `runtime.LabelAppKey`/`LabelEnvKey` are defined in Task 1 and used in Task 2. `newCleaner func() (cleanup.Cleaner, error)` is defined in Task 3 and used by the Task 3 tests. `Result.ReclaimedSpace`/`Result.Categories`/`Result.CategoryNames()` are defined in Task 2 and printed in Task 3. No naming drift.

**Known behavior (documented, not a defect):** Images built before Task 1 shipped do not carry the labels; an unused old rollback image could be pruned by `docker image prune -a`. Mitigation: re-deploy after upgrade re-labels the current image, and `KeepLastNImages` (5) already limits how many old images exist. Old Nixpacks CLIs without `--label` support will fail the build with docker's error output — the plan targets current Nixpacks.
