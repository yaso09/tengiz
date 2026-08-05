# Docker Housekeeping (tengiz cleanup) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning unused Docker containers, images, volumes, networks, and build cache while always protecting Tengiz-managed resources (labeled `tengiz-app=...`).

**Architecture:** A new `internal/cleanup` package wraps the `docker` CLI (same `os/exec` pattern as `internal/runtime`). A `Housekeeper` interface with an exec-based `dockerHousekeeper` impl and an injectable command runner for tests drives five per-category prune commands. Each category uses Docker's `--filter` support with `label!=tengiz-app` so Tengiz resources survive. Tengiz-built images are labeled `tengiz-app=<app>`/`tengiz-env=<env>` at build time (both `docker build` and `nixpacks build`) so the aggressive `--unused` image prune also protects them. The CLI wires the package to a `tengiz cleanup` command with per-category flags, `--all`, `--unused`, and `--dry-run`.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` (Docker CLI), existing `internal/types`, `internal/runtime` conventions, no new external dependencies.

## Global Constraints

- Docker CLI is invoked via `os/exec` exactly like `internal/runtime`; unit tests never require a real Docker daemon (pure arg-builders + injected `runFunc`)
- Tengiz-managed resources are labeled `tengiz-app=<app>` (`types.LabelApp = "tengiz-app"`) and `tengiz-env=<env>` (`types.LabelEnv = "tengiz-env"`)
- Tengiz-built images are tagged `tengiz-apps/<app>:<env>-<deploymentID>` and must carry the `tengiz-app` label from build time
- `docker image prune` supports ONLY the `until` and `label` filters — image protection MUST use `--filter "label!=tengiz-app"`, never a `reference` filter
- Image pruning defaults to dangling-only (safe). `--unused` (`docker image prune -a`) is opt-in and still protects labeled Tengiz images
- When no category flag is given to `tengiz cleanup`, all categories run (dangling images only)
- All docker prune commands pass `-f` (no interactive prompts)
- No new external Go dependencies
- Existing tests must continue to pass without modification
- TDD per task: write failing test → run to confirm fail → minimal implementation → run to confirm pass → commit
- Commit messages use the repo style: `feat: ...` / `test: ...` / `docs: ...`
- Every task must pass `go build ./...`, `go test ./internal/<pkg>/... -v -count=1`, and `go vet ./internal/<pkg>/...`
- `--env` is a global persistent flag and is NOT needed by `tengiz cleanup` (it protects resources across all environments)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | Add `LabelApp` and `LabelEnv` constants (single source of truth for label names) |
| `internal/types/types_test.go` | Test the label constants |
| `internal/builder/builder.go` | Extract `dockerBuildArgs()` / `nixpacksBuildArgs()` and add `--label tengiz-app=<app>` / `--label tengiz-env=<env>` to both build paths |
| `internal/builder/builder_test.go` | Tests asserting build args include the labels |
| `internal/cleanup/cleanup.go` | NEW package: `Options`, `Summary`, `Housekeeper` interface, `Resolve`, per-category prune + dry-run arg builders, `parsePruneOutput`, exec-based `dockerHousekeeper`, `NewDocker`, `NewStub` |
| `internal/cleanup/cleanup_test.go` | NEW tests: arg builders, parser, `Resolve`, orchestration via injected runner, dry-run behavior |
| `internal/cli/root.go` | Register `tengiz cleanup` command + flags, `cleanupOptionsFromFlags`, `printCleanupSummary` |
| `internal/cli/root_test.go` | Tests: command registered, flags present, options parsing, no-flag→all resolution |
| `README.md` | Document `tengiz cleanup` under CLI Reference + Features bullet |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Label constants + build-time image labels

**Files:**
- Modify: `internal/types/types.go` — add `LabelApp`, `LabelEnv` constants
- Modify: `internal/builder/builder.go` — extract arg-builders, add `--label` flags to `buildWithDockerfile` (lines 57-91) and `buildWithNixpacks` (lines 129-170)
- Modify: `internal/types/types_test.go` — test constants
- Modify: `internal/builder/builder_test.go` — test arg-builders include labels

**Interfaces:**
- Consumes: nothing new
- Produces: `types.LabelApp string` (= `"tengiz-app"`), `types.LabelEnv string` (= `"tengiz-env"`); `(*Builder).dockerBuildArgs(appName, env, tag, dir string) []string`; `(*Builder).nixpacksBuildArgs(appName, env, tag, dir string) []string`

- [ ] **Step 1: Write the failing tests**

Add to `internal/types/types_test.go`:

```go
package types

import "testing"

func TestLabelConstants(t *testing.T) {
	if LabelApp != "tengiz-app" {
		t.Errorf("LabelApp = %q, want %q", LabelApp, "tengiz-app")
	}
	if LabelEnv != "tengiz-env" {
		t.Errorf("LabelEnv = %q, want %q", LabelEnv, "tengiz-env")
	}
}
```

Add to `internal/builder/builder_test.go`:

```go
func TestDockerBuildArgsIncludeLabels(t *testing.T) {
	b := New(t.TempDir())
	b.SetBuildSecrets(map[string]string{"NPM_TOKEN": "secret-token"})
	args := b.dockerBuildArgs("myapp", "staging", "tengiz-apps/myapp:staging-123", "/tmp/app")
	got := strings.Join(args, " ")
	for _, want := range []string{
		"--label", "tengiz-app=myapp",
		"--label", "tengiz-env=staging",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("dockerBuildArgs() missing %q in %q", want, got)
		}
	}
	if !strings.Contains(got, "--secret") {
		t.Errorf("dockerBuildArgs() should keep build secrets: %q", got)
	}
}

func TestNixpacksBuildArgsIncludeLabels(t *testing.T) {
	b := New(t.TempDir())
	b.SetNixpacksConfig(&types.NixpacksConfig{Packages: []string{"curl"}})
	args := b.nixpacksBuildArgs("myapp", "production", "tengiz-apps/myapp:production-456", "/tmp/app")
	got := strings.Join(args, " ")
	for _, want := range []string{
		"--label", "tengiz-app=myapp",
		"--label", "tengiz-env=production",
		"--name", "tengiz-apps/myapp:production-456",
		"--pkgs", "curl",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("nixpacksBuildArgs() missing %q in %q", want, got)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/types/... ./internal/builder/... -run "TestLabelConstants|TestDockerBuildArgsIncludeLabels|TestNixpacksBuildArgsIncludeLabels" -v -count=1`

Expected: FAIL — `undefined: LabelApp`, `b.dockerBuildArgs undefined (type *Builder has no field or method dockerBuildArgs)`, `b.nixpacksBuildArgs undefined`

- [ ] **Step 3: Implement label constants**

Add to `internal/types/types.go`, right after the `import "time"` line:

```go
const (
	LabelApp = "tengiz-app"
	LabelEnv = "tengiz-env"
)
```

- [ ] **Step 4: Implement the builder changes**

In `internal/builder/builder.go`:

1. Add two new methods (place them between `SetBuildSecrets` and `Build`):

```go
func (b *Builder) dockerBuildArgs(appName, env, tag, dir string) []string {
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "--label", fmt.Sprintf("%s=%s", types.LabelApp, appName))
	args = append(args, "--label", fmt.Sprintf("%s=%s", types.LabelEnv, env))
	args = append(args, "-t", tag, dir)
	return args
}

func (b *Builder) nixpacksBuildArgs(appName, env, tag, dir string) []string {
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
	args = append(args, "--label", fmt.Sprintf("%s=%s", types.LabelApp, appName))
	args = append(args, "--label", fmt.Sprintf("%s=%s", types.LabelEnv, env))
	return args
}
```

2. Replace the arg construction in `buildWithDockerfile` (currently lines 69-71):

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

with:

```go
	args := b.dockerBuildArgs(appName, env, tag, dir)
```

3. Replace the arg construction in `buildWithNixpacks` (currently lines 139-150):

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
	args := b.nixpacksBuildArgs(appName, env, tag, dir)
```

No other lines in those two functions change.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/types/... ./internal/builder/... -v -count=1`

Expected: PASS (the two existing integration tests `TestBuildCapturesOutput` / `TestBuildWithDeploymentIDCompiles` may SKIP if Docker is unavailable — that is fine).

- [ ] **Step 6: Run all package tests + build + vet**

Run: `go build ./... && go vet ./internal/types/... ./internal/builder/... && go test ./internal/types/... ./internal/builder/... -v -count=1`

Expected: build OK, vet OK, all tests PASS/SKIP.

- [ ] **Step 7: Commit**

```bash
git add internal/types/types.go internal/types/types_test.go internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: label Tengiz-built images for safe cleanup protection"
```

---

### Task 2: Create the cleanup package (types, arg builders, parser, Resolve, Stub)

**Files:**
- Create: `internal/cleanup/cleanup.go`
- Create: `internal/cleanup/cleanup_test.go`

**Interfaces:**
- Consumes: `types.LabelApp` from Task 1
- Produces: `Options`, `Summary`, `Housekeeper` (interface with `Clean(ctx context.Context, opts Options) (*Summary, error)`), `Resolve(opts Options) Options`, `NewDocker() Housekeeper`, `NewStub() Housekeeper`, `containerPruneArgs()`, `imagePruneArgs(unused bool)`, `volumePruneArgs()`, `networkPruneArgs()`, `builderPruneArgs()`, `parsePruneOutput(out string) (items []string, reclaimed string)`, `joinReclaimed([]string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/cleanup/cleanup_test.go`:

```go
package cleanup

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestContainerPruneArgs(t *testing.T) {
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !equalStrings(containerPruneArgs(), want) {
		t.Errorf("containerPruneArgs() = %v, want %v", containerPruneArgs(), want)
	}
}

func TestImagePruneArgs(t *testing.T) {
	if !equalStrings(imagePruneArgs(false), []string{"image", "prune", "-f"}) {
		t.Errorf("imagePruneArgs(false) = %v", imagePruneArgs(false))
	}
	want := []string{"image", "prune", "-a", "-f", "--filter", "label!=tengiz-app"}
	if !equalStrings(imagePruneArgs(true), want) {
		t.Errorf("imagePruneArgs(true) = %v, want %v", imagePruneArgs(true), want)
	}
}

func TestVolumePruneArgs(t *testing.T) {
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !equalStrings(volumePruneArgs(), want) {
		t.Errorf("volumePruneArgs() = %v, want %v", volumePruneArgs(), want)
	}
}

func TestNetworkPruneArgs(t *testing.T) {
	want := []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	if !equalStrings(networkPruneArgs(), want) {
		t.Errorf("networkPruneArgs() = %v, want %v", networkPruneArgs(), want)
	}
}

func TestBuilderPruneArgs(t *testing.T) {
	want := []string{"builder", "prune", "-f"}
	if !equalStrings(builderPruneArgs(), want) {
		t.Errorf("builderPruneArgs() = %v, want %v", builderPruneArgs(), want)
	}
}

func TestParsePruneOutput(t *testing.T) {
	out := "Deleted Containers:\nabcd1234\nefgh5678\nTotal reclaimed space: 27 B\n"
	items, reclaimed := parsePruneOutput(out)
	if len(items) != 2 || items[0] != "abcd1234" || items[1] != "efgh5678" {
		t.Errorf("items = %v, want [abcd1234 efgh5678]", items)
	}
	if reclaimed != "27 B" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "27 B")
	}
}

func TestParsePruneOutputImagesSkipsMetadataLines(t *testing.T) {
	out := "Untagged: alpine:latest\nDeleted Images:\ndeleted: sha256:abc123\nsha256:0f3d9f2a\nTotal reclaimed space: 792.6 MB\n"
	items, reclaimed := parsePruneOutput(out)
	if len(items) != 1 || items[0] != "sha256:0f3d9f2a" {
		t.Errorf("items = %v, want [sha256:0f3d9f2a]", items)
	}
	if reclaimed != "792.6 MB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "792.6 MB")
	}
}

func TestParsePruneOutputBuilderTotal(t *testing.T) {
	items, reclaimed := parsePruneOutput("Total: 123.4MB\n")
	if len(items) != 0 {
		t.Errorf("items = %v, want empty", items)
	}
	if reclaimed != "123.4MB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "123.4MB")
	}
}

func TestJoinReclaimed(t *testing.T) {
	got := joinReclaimed([]string{"27 B", "", "0 B", "27 B", "792.6 MB"})
	if got != "27 B + 792.6 MB" {
		t.Errorf("joinReclaimed() = %q, want %q", got, "27 B + 792.6 MB")
	}
}

func TestResolveDefaultsToAll(t *testing.T) {
	opts := Resolve(Options{})
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Error("Resolve(empty Options{}) should enable all categories")
	}
}

func TestResolveAllFlagEnablesAll(t *testing.T) {
	opts := Resolve(Options{All: true})
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Error("Resolve(All:true) should enable all categories")
	}
}

func TestResolveRespectsExplicitCategory(t *testing.T) {
	opts := Resolve(Options{Containers: true})
	if !opts.Containers {
		t.Error("containers should stay enabled")
	}
	if opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Error("explicit category must not enable the others")
	}
}

func TestStubSatisfiesInterface(t *testing.T) {
	var hk Housekeeper = NewStub()
	if hk == nil {
		t.Fatal("NewStub() returned nil")
	}
}

func TestStubCleanReturnsEmptySummary(t *testing.T) {
	summary, err := NewStub().Clean(context.Background(), Options{All: true})
	if err != nil {
		t.Fatalf("Stub Clean() error = %v", err)
	}
	if summary == nil {
		t.Fatal("Stub Clean() returned nil summary")
	}
}

func TestDockerHousekeeperSatisfiesInterface(t *testing.T) {
	var hk Housekeeper = &dockerHousekeeper{
		run: func(ctx context.Context, name string, args ...string) (string, error) { return "", nil },
	}
	if hk == nil {
		t.Fatal("dockerHousekeeper does not satisfy Housekeeper")
	}
}

func TestCleanPropagatesError(t *testing.T) {
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		return "docker: error", fmt.Errorf("boom")
	}
	h := &dockerHousekeeper{run: run}
	if _, err := h.Clean(context.Background(), Options{Containers: true}); err == nil {
		t.Fatal("expected error from prune failure")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: FAIL — `package github.com/yaso09/tengiz/internal/cleanup is not in std` / `undefined: Options`, `undefined: containerPruneArgs`, etc. (package does not exist yet).

- [ ] **Step 3: Implement `internal/cleanup/cleanup.go`**

```go
package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

type Options struct {
	Containers bool
	Images     bool
	Unused     bool
	Volumes    bool
	Networks   bool
	BuildCache bool
	All        bool
	DryRun     bool
}

type Summary struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
	BuildCache string
	Reclaimed  string
}

type Housekeeper interface {
	Clean(ctx context.Context, opts Options) (*Summary, error)
}

func Resolve(opts Options) Options {
	if opts.All || (!opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.BuildCache) {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.BuildCache = true
	}
	return opts
}

func containerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", types.LabelApp)}
}

func imagePruneArgs(unused bool) []string {
	if unused {
		return []string{"image", "prune", "-a", "-f", "--filter", fmt.Sprintf("label!=%s", types.LabelApp)}
	}
	return []string{"image", "prune", "-f"}
}

func volumePruneArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", types.LabelApp)}
}

func networkPruneArgs() []string {
	return []string{"network", "prune", "-f", "--filter", fmt.Sprintf("label!=%s", types.LabelApp)}
}

func builderPruneArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func containerDryRunArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", fmt.Sprintf("label!=%s", types.LabelApp),
		"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}",
	}
}

func imageDryRunArgs(unused bool) []string {
	if unused {
		return []string{
			"images", "-a",
			"--filter", fmt.Sprintf("label!=%s", types.LabelApp),
			"--format", "{{.ID}}\t{{.Repository}}:{{.Tag}}\t{{.Size}}",
		}
	}
	return []string{
		"images",
		"--filter", "dangling=true",
		"--format", "{{.ID}}\t{{.Repository}}:{{.Tag}}\t{{.Size}}",
	}
}

func volumeDryRunArgs() []string {
	return []string{
		"volume", "ls",
		"--filter", "dangling=true",
		"--filter", fmt.Sprintf("label!=%s", types.LabelApp),
		"--format", "{{.Name}}",
	}
}

func networkDryRunArgs() []string {
	return []string{
		"network", "ls",
		"--format", "{{.ID}}\t{{.Name}}",
	}
}

func parsePruneOutput(out string) (items []string, reclaimed string) {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "total"):
			if idx := strings.Index(line, ":"); idx >= 0 {
				reclaimed = strings.TrimSpace(line[idx+1:])
			}
		case isPruneMetadataLine(line):
			// section headers ("Deleted Containers:"), untagged/deleted notes,
			// and prompt text — none are pruned items
		default:
			items = append(items, line)
		}
	}
	return items, reclaimed
}

func isPruneMetadataLine(line string) bool {
	lower := strings.ToLower(line)
	prefixes := []string{"deleted", "untagged", "warning", "are you sure", "removed", "total"}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return strings.HasSuffix(line, ":")
}

func joinReclaimed(values []string) string {
	seen := make(map[string]bool)
	var parts []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || v == "0 B" || seen[v] {
			continue
		}
		seen[v] = true
		parts = append(parts, v)
	}
	return strings.Join(parts, " + ")
}

type runFunc func(ctx context.Context, name string, args ...string) (string, error)

type dockerHousekeeper struct {
	run runFunc
}

func NewDocker() Housekeeper {
	return &dockerHousekeeper{
		run: func(ctx context.Context, name string, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return string(out), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, string(out))
			}
			return string(out), nil
		},
	}
}

type stubHousekeeper struct{}

func NewStub() Housekeeper {
	return &stubHousekeeper{}
}

func (s *stubHousekeeper) Clean(ctx context.Context, opts Options) (*Summary, error) {
	return &Summary{}, nil
}

func (h *dockerHousekeeper) Clean(ctx context.Context, opts Options) (*Summary, error) {
	opts = Resolve(opts)
	summary := &Summary{}

	if opts.DryRun {
		return summary, h.dryRun(ctx, opts, summary)
	}

	prune := func(args []string) ([]string, string, error) {
		out, err := h.run(ctx, "docker", args...)
		if err != nil {
			return nil, "", err
		}
		items, rec := parsePruneOutput(out)
		return items, rec, nil
	}

	reclaimed := make([]string, 0, 5)
	if opts.Containers {
		items, rec, err := prune(containerPruneArgs())
		if err != nil {
			return summary, err
		}
		summary.Containers = items
		reclaimed = append(reclaimed, rec)
	}
	if opts.Images {
		items, rec, err := prune(imagePruneArgs(opts.Unused))
		if err != nil {
			return summary, err
		}
		summary.Images = items
		reclaimed = append(reclaimed, rec)
	}
	if opts.Volumes {
		items, rec, err := prune(volumePruneArgs())
		if err != nil {
			return summary, err
		}
		summary.Volumes = items
		reclaimed = append(reclaimed, rec)
	}
	if opts.Networks {
		items, rec, err := prune(networkPruneArgs())
		if err != nil {
			return summary, err
		}
		summary.Networks = items
		reclaimed = append(reclaimed, rec)
	}
	if opts.BuildCache {
		items, rec, err := prune(builderPruneArgs())
		if err != nil {
			return summary, err
		}
		summary.BuildCache = strings.Join(items, "\n")
		reclaimed = append(reclaimed, rec)
	}

	summary.Reclaimed = joinReclaimed(reclaimed)
	return summary, nil
}

func (h *dockerHousekeeper) dryRun(ctx context.Context, opts Options, summary *Summary) error {
	lines := func(out string) []string {
		var res []string
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				res = append(res, line)
			}
		}
		return res
	}

	if opts.Containers {
		out, err := h.run(ctx, "docker", containerDryRunArgs()...)
		if err != nil {
			return err
		}
		summary.Containers = lines(out)
	}
	if opts.Images {
		out, err := h.run(ctx, "docker", imageDryRunArgs(opts.Unused)...)
		if err != nil {
			return err
		}
		summary.Images = lines(out)
	}
	if opts.Volumes {
		out, err := h.run(ctx, "docker", volumeDryRunArgs()...)
		if err != nil {
			return err
		}
		summary.Volumes = lines(out)
	}
	if opts.Networks {
		out, err := h.run(ctx, "docker", networkDryRunArgs()...)
		if err != nil {
			return err
		}
		summary.Networks = lines(out)
	}
	if opts.BuildCache {
		out, err := h.run(ctx, "docker", "builder", "du")
		if err != nil {
			return err
		}
		summary.BuildCache = strings.TrimSpace(out)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS (all tests). Note: `TestParsePruneOutputImagesSkipsMetadataLines` expects the line `delel` (a made-up non-metadata token) to be the only item — verify `deleted:` and `Untagged:` prefixes are skipped.

- [ ] **Step 5: Run build + vet + all package tests**

Run: `go build ./... && go vet ./internal/cleanup/... && go test ./internal/cleanup/... -v -count=1`

Expected: build OK, vet OK, all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cleanup/cleanup.go internal/cleanup/cleanup_test.go
git commit -m "feat: add cleanup package with label-protected docker prune commands"
```

---

### Task 3: Orchestration (Clean sequence, dry-run, summary accumulation)

**Files:**
- Modify: `internal/cleanup/cleanup_test.go` — add orchestration tests
- (No production code changes — `Clean` and `dryRun` were written in Task 2; this task locks in their behavior with tests)

**Interfaces:**
- Consumes: `dockerHousekeeper` with injectable `run runFunc` from Task 2
- Produces: verified behavior contract — `Clean` runs one prune command per enabled category in order containers→images→volumes→networks→build-cache, uses `-a` + label filter when `Unused`, runs list commands (never `prune`) under `DryRun`, aggregates reclaimed space

- [ ] **Step 1: Write the failing orchestration tests**

Append to `internal/cleanup/cleanup_test.go`:

```go
func TestCleanRunsPruneCommandsInOrder(t *testing.T) {
	var calls [][]string
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		switch args[0] {
		case "container":
			return "Deleted Containers:\nabc123\nTotal reclaimed space: 27 B\n", nil
		case "image":
			return "Deleted Images:\nsha:1\nTotal reclaimed space: 100 MB\n", nil
		case "volume":
			return "Deleted Volumes:\nvol1\nTotal reclaimed space: 5 B\n", nil
		case "network":
			return "Deleted Networks:\nnet1\nTotal reclaimed space: 0 B\n", nil
		case "builder":
			return "Total: 200 MB\n", nil
		default:
			return "", nil
		}
	}
	h := &dockerHousekeeper{run: run}
	summary, err := h.Clean(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if len(calls) != 5 {
		t.Fatalf("expected 5 prune commands, got %d: %v", len(calls), calls)
	}
	wantFirst := "docker container prune -f --filter label!=tengiz-app"
	if got := strings.Join(calls[0], " "); got != wantFirst {
		t.Errorf("first command = %q, want %q", got, wantFirst)
	}
	wantImages := "docker image prune -f"
	if got := strings.Join(calls[1], " "); got != wantImages {
		t.Errorf("images command = %q, want %q", got, wantImages)
	}
	if len(summary.Containers) != 1 || summary.Containers[0] != "abc123" {
		t.Errorf("summary.Containers = %v", summary.Containers)
	}
	if len(summary.Images) != 1 || summary.Images[0] != "sha:1" {
		t.Errorf("summary.Images = %v", summary.Images)
	}
	if summary.BuildCache != "" {
		t.Errorf("summary.BuildCache = %q, want empty (builder Total line is captured as reclaimed space)", summary.BuildCache)
	}
	if !strings.Contains(summary.Reclaimed, "27 B") || !strings.Contains(summary.Reclaimed, "200 MB") {
		t.Errorf("summary.Reclaimed = %q, want it to include 27 B and 200 MB", summary.Reclaimed)
	}
	if strings.Contains(summary.Reclaimed, "0 B") {
		t.Errorf("summary.Reclaimed = %q, must not include 0 B", summary.Reclaimed)
	}
}

func TestCleanUnusedImagesUsesAggressiveFilter(t *testing.T) {
	var calls [][]string
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "", nil
	}
	h := &dockerHousekeeper{run: run}
	if _, err := h.Clean(context.Background(), Options{Images: true, Unused: true}); err != nil {
		t.Fatal(err)
	}
	want := "docker image prune -a -f --filter label!=tengiz-app"
	if got := strings.Join(calls[0], " "); got != want {
		t.Errorf("unused images command = %q, want %q", got, want)
	}
	if len(calls) != 1 {
		t.Errorf("expected only 1 command, got %d: %v", len(calls), calls)
	}
}

func TestCleanDryRunListsInsteadOfPruning(t *testing.T) {
	var calls [][]string
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		switch args[0] {
		case "ps":
			return "abc123\tmyapp\ttengiz-apps/myapp:prod-1\n", nil
		case "images":
			return "sha:1\t<none>:<none>\t5 MB\n", nil
		case "volume":
			return "vol1\n", nil
		case "network":
			return "net1\n", nil
		case "builder":
			return "ID\tRECLAIMABLE\nabc\t123 MB\n", nil
		default:
			return "", nil
		}
	}
	h := &dockerHousekeeper{run: run}
	summary, err := h.Clean(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if c[1] == "prune" {
			t.Errorf("dry-run must not call prune, got %v", c)
		}
	}
	if len(summary.Containers) != 1 || summary.Containers[0] != "abc123\tmyapp\ttengiz-apps/myapp:prod-1" {
		t.Errorf("dry-run containers = %v", summary.Containers)
	}
	if len(calls) != 5 {
		t.Errorf("expected 5 list commands, got %d: %v", len(calls), calls)
	}
	if summary.Reclaimed != "" {
		t.Errorf("dry-run should not report reclaimed space, got %q", summary.Reclaimed)
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/cleanup/... -v -count=1`

Expected: PASS. If a test fails, fix the production code in `internal/cleanup/cleanup.go` (do not weaken assertions).

- [ ] **Step 3: Run full package + build + vet**

Run: `go build ./... && go vet ./internal/cleanup/... && go test ./internal/cleanup/... -v -count=1`

Expected: build OK, vet OK, all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/cleanup/cleanup_test.go
git commit -m "test: verify cleanup orchestration order, dry-run, and unused filtering"
```

---

### Task 4: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add import, register command, define `cleanupCmd`, `cleanupOptionsFromFlags`, `printCleanupSummary`
- Modify: `internal/cli/root_test.go` — add CLI tests

**Interfaces:**
- Consumes: `cleanup.Options`, `cleanup.NewDocker()`, `cleanup.Resolve`, `cleanup.Summary` from Tasks 2-3
- Produces: `tengiz cleanup` cobra command with flags `--containers`, `--images`, `--unused`, `--volumes`, `--networks`, `--build-cache`, `--all`, `--dry-run`; helper `cleanupOptionsFromFlags(cmd *cobra.Command) cleanup.Options`

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/root_test.go`:

```go
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	expected := []string{"containers", "images", "unused", "volumes", "networks", "build-cache", "all", "dry-run"}
	for _, flag := range expected {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup command missing --%s flag", flag)
		}
	}
}

func resetCleanupFlags(t *testing.T) {
	t.Helper()
	for _, flag := range []string{"containers", "images", "unused", "volumes", "networks", "build-cache", "all", "dry-run"} {
		if err := cleanupCmd.Flags().Set(flag, "false"); err != nil {
			t.Fatalf("reset --%s: %v", flag, err)
		}
	}
}

func TestCleanupOptionsFromFlags(t *testing.T) {
	resetCleanupFlags(t)
	cleanupCmd.ParseFlags([]string{"--containers", "--dry-run"})
	opts := cleanupOptionsFromFlags(cleanupCmd)
	if !opts.Containers {
		t.Error("containers = false, want true")
	}
	if !opts.DryRun {
		t.Error("dry-run = false, want true")
	}
	if opts.Images || opts.Volumes || opts.Networks || opts.BuildCache {
		t.Error("unset categories should be false")
	}
}

func TestCleanupNoFlagsResolvesToAll(t *testing.T) {
	resetCleanupFlags(t)
	cleanupCmd.ParseFlags([]string{})
	opts := cleanupOptionsFromFlags(cleanupCmd)
	resolved := cleanup.Resolve(opts)
	if !resolved.Containers || !resolved.Images || !resolved.Volumes || !resolved.Networks || !resolved.BuildCache {
		t.Error("no flags should resolve to cleaning all categories")
	}
}

func TestCleanupUnusedFlagRequiresImagesResolution(t *testing.T) {
	resetCleanupFlags(t)
	cleanupCmd.ParseFlags([]string{"--unused"})
	opts := cleanupOptionsFromFlags(cleanupCmd)
	resolved := cleanup.Resolve(opts)
	if !resolved.Images {
		t.Error("--unused alone should still enable image pruning")
	}
	if !resolved.Unused {
		t.Error("Unused flag was dropped")
	}
}
```

Note: `root_test.go` needs the import `"github.com/yaso09/tengiz/internal/cleanup"` added.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: FAIL — `cleanup command not found`, `undefined: cleanupCmd`, `undefined: cleanup`.

- [ ] **Step 3: Implement the CLI command**

1. Add the import to `internal/cli/root.go`:

```go
	"github.com/yaso09/tengiz/internal/builder"
	"github.com/yaso09/tengiz/internal/cleanup"
	"github.com/yaso09/tengiz/internal/config"
```

2. Register the command in `init()` (add after `rootCmd.AddCommand(rollbackCmd)`):

```go
	rootCmd.AddCommand(cleanupCmd)
```

3. Register the flags in `init()` (add after `buildLogsCmd` flag registration or anywhere inside `init()`):

```go
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers not managed by Tengiz")
	cleanupCmd.Flags().Bool("images", false, "prune dangling images")
	cleanupCmd.Flags().Bool("unused", false, "also remove unused tagged images (with --images or --all)")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("build-cache", false, "prune Docker build cache")
	cleanupCmd.Flags().Bool("all", false, "prune all resource types (default when no category flag is given)")
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing")
```

4. Add the command definition + helpers after the `healthCmd` block (which ends at line 783):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up unused Docker resources",
	Long: `Removes unused Docker containers, images, volumes, networks, and build
cache to reclaim disk space.

Tengiz-managed resources are always protected:
  - containers, volumes, and networks labeled "tengiz-app=..."
  - images carrying the "tengiz-app" label (all Tengiz-built images)

When no category flag is given, all categories are cleaned (dangling images
only). Use --unused to also remove unused tagged images.

Examples:
  tengiz cleanup                # prune all safe categories
  tengiz cleanup --containers   # stopped containers only
  tengiz cleanup --all --unused # aggressive: also unused tagged images
  tengiz cleanup --dry-run      # show what would be removed`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		hk := cleanup.NewDocker()
		summary, err := hk.Clean(cmd.Context(), cleanupOptionsFromFlags(cmd))
		if err != nil {
			return err
		}
		printCleanupSummary(summary)
		return nil
	},
}

func cleanupOptionsFromFlags(cmd *cobra.Command) cleanup.Options {
	containers, _ := cmd.Flags().GetBool("containers")
	images, _ := cmd.Flags().GetBool("images")
	unused, _ := cmd.Flags().GetBool("unused")
	volumes, _ := cmd.Flags().GetBool("volumes")
	networks, _ := cmd.Flags().GetBool("networks")
	buildCache, _ := cmd.Flags().GetBool("build-cache")
	all, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	return cleanup.Options{
		Containers: containers,
		Images:     images,
		Unused:     unused,
		Volumes:    volumes,
		Networks:   networks,
		BuildCache: buildCache,
		All:        all,
		DryRun:     dryRun,
	}
}

func printCleanupSummary(s *cleanup.Summary) {
	if s == nil {
		fmt.Println("[tengiz] cleanup complete")
		return
	}
	printGroup := func(label string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Printf("[tengiz] %s (%d):\n", label, len(items))
		for _, it := range items {
			fmt.Printf("  %s\n", it)
		}
	}
	printGroup("removed containers", s.Containers)
	printGroup("removed images", s.Images)
	printGroup("removed volumes", s.Volumes)
	printGroup("removed networks", s.Networks)
	if s.BuildCache != "" {
		fmt.Printf("[tengiz] build cache: %s\n", s.BuildCache)
	}
	if s.Reclaimed != "" {
		fmt.Printf("[tengiz] total reclaimed space: %s\n", s.Reclaimed)
	} else {
		fmt.Println("[tengiz] cleanup complete")
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanup" -v -count=1`

Expected: PASS.

- [ ] **Step 5: Run full cli package + build + vet**

Run: `go build ./... && go vet ./internal/cli/... && go test ./internal/cli/... -v -count=1`

Expected: build OK, vet OK, all existing cli tests still PASS (shared `rootCmd` state is reset by each `Execute()` call; `dataDir` is overridden per-test where needed).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command for label-protected docker housekeeping"
```

---

### Task 5: Documentation + feature tracking + full verification

**Files:**
- Modify: `README.md` — Features bullet + `tengiz cleanup` CLI Reference section
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 as implemented
- No production code changes

**Interfaces:**
- Consumes: everything from Tasks 1-4

- [ ] **Step 1: Add a Features bullet to `README.md`**

In `README.md`, in the `## Features` list, add after the "**Health check configuration**" bullet (line 21):

```markdown
- **Docker housekeeping** — `tengiz cleanup` prunes unused containers, images, volumes, networks, and build cache while always protecting Tengiz-managed resources.
```

- [ ] **Step 2: Add the CLI Reference section to `README.md`**

In `README.md`, insert after the `### tengiz rollback <app>` section (which ends at line 236) and before `### tengiz domain`:

```markdown
### `tengiz cleanup`

Remove unused Docker resources to reclaim disk space.

| Flag | Description |
|------|-------------|
| `--containers` | Prune stopped containers not managed by Tengiz |
| `--images` | Prune dangling images |
| `--unused` | Also remove unused tagged images (with `--images` or `--all`) |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused networks |
| `--build-cache` | Prune Docker build cache |
| `--all` | Prune all resource types (default when no category flag is given) |
| `--dry-run` | Show what would be removed without removing |

Tengiz-managed resources are always protected: containers, volumes, and networks labeled `tengiz-app=...` are never pruned, and all Tengiz-built images carry the `tengiz-app` label and are preserved even when `--unused` is used.

Examples:
```
tengiz cleanup                # prune all safe categories
tengiz cleanup --containers   # stopped containers only
tengiz cleanup --all --unused # aggressive: also unused tagged images
tengiz cleanup --dry-run      # show what would be removed
```
```

- [ ] **Step 3: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

1. In the P0 table (line 19), change:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

2. In the `### ✅ Implemented Features (Not Pending)` table, add a row (after the rollback row):

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-05) |
```

- [ ] **Step 4: Full verification**

Run: `go build ./... && go vet ./... && go test ./... -v -count=1`

Expected: build OK, vet OK, all tests PASS. Note: `internal/proxy` tests are slow (~2s each) and `internal/idle` tests are time-sensitive — this is expected and documented in `AGENTS.md`.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Manual Verification (post-implementation, requires Docker)

Run these against a real Docker daemon to confirm end-to-end behavior:

```bash
# All safe categories, dry-run first
tengiz cleanup --dry-run

# Containers only
tengiz cleanup --containers

# Everything safe (dangling images)
tengiz cleanup

# Aggressive with protection of Tengiz images
tengiz cleanup --all --unused

# Verify a deployed app still works after cleanup
tengiz ps
curl -s http://myapp.tengiz.local:8080 | head
```

---

## Self-Review

**1. Spec coverage (feature #6 Docker Housekeeping from `docs/FUTURES_FEATURES.md`):**
- "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur" → `label!=tengiz-app` filters on containers/volumes/networks in Tasks 2-4
- "`tengiz cleanup` komutu eklenebilir" → Task 4 adds the command
- "kullanılmayan volume, network, container ve image'leri periyodik temizleme" → per-category prune flags + `--all` cover all four plus build cache (granular prune #56 overlap)
- Image protection (rollback safety) → Task 1 adds build-time labels; `--unused` still protects labeled images

**2. Placeholder scan:** No TBD/TODO, no "add validation"-style vague steps, every code step contains complete code, every test step contains complete test code. All referenced functions are defined in an earlier task.

**3. Type consistency:**
- `cleanup.Options` fields (`Containers/Images/Unused/Volumes/Networks/BuildCache/All/DryRun`) used identically in Tasks 2-4
- `Summary` fields (`Containers/Images/Volumes/Networks/BuildCache/Reclaimed`) consistent between `Clean`, `dryRun`, and `printCleanupSummary`
- `Resolve(opts Options) Options` signature consistent between Task 2 definition, Task 2 tests, and Task 4 tests
- `types.LabelApp`/`types.LabelEnv` defined in Task 1, consumed in Task 1 (builder) and Task 2 (cleanup)
- `containerPruneArgs()`/`imagePruneArgs(bool)`/`volumePruneArgs()`/`networkPruneArgs()`/`builderPruneArgs()` defined in Task 2, used by `Clean` in the same task and asserted in Task 2/3 tests
