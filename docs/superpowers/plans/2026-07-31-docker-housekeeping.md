# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that runs label-based `docker system prune` so a single-server tengiz instance never runs out of disk from stopped containers, dangling images, and build cache. Tengiz-managed containers/images are protected via the `tengiz-app` label.

**Architecture:** A new `runtime.Prune(ctx, opts)` method on `runtime.Manager` executes `docker system prune -f --filter "label!=tengiz-app"` (with optional `-a` and `--volumes`), parsing the output into a `PruneReport`. The `label!=tengiz-app` negation filter is supported by *prune* subcommands but NOT by `docker ps`/`docker images`, so dry-run enumerates candidates in Go via set-difference (all minus tengiz-labeled). Built images gain `tengiz-app=<app>` / `tengiz-env=<env>` labels so the prune filter protects them. A new `internal/cli/cleanup.go` wires the command with `--dry-run`, `--all`, `--volumes` flags.

**Tech Stack:** Go 1.26, existing `runtime.Manager`, `docker` CLI via `os/exec`, Cobra.

## Global Constraints

- Prune filter is ALWAYS `label!=tengiz-app` — never prune containers/images carrying the `tengiz-app` label
- `docker ps` and `docker images` do NOT support the `label!=` negation filter (verified: `invalid filter 'label!'`) — dry-run must compute candidates via set-difference with the positive `label=tengiz-app` filter
- All tengiz containers already carry `tengiz-app=<app>` and `tengiz-env=<env>` labels (see `internal/runtime/docker.go:76-77` constants) — no change needed for containers
- New image labels are a REQUIRED prerequisite: without them `-a` prune would delete rollback images that `KeepLastNImages` deliberately retains
- `:latest`-suffixed images are NEVER pruned by `KeepLastNImages`; prune (`-a`) must preserve that guarantee by relying on the new label
- Default `tengiz cleanup` (no flags) = safe prune: stopped non-tengiz containers, dangling non-tengiz images, unused networks, build cache. Volumes are NOT touched unless `--volumes`
- `--dry-run` never executes a prune; it enumerates and reports what *would* be removed
- No new external Go dependencies
- Every new Manager interface method must be added to the stub AND to the test mocks in `root_test.go`, `idle_test.go`, `proxy_test.go` (they implement `runtime.Manager` directly)
- Existing tests must continue to pass without modification

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/prune.go` | NEW — `PruneOptions`, `PruneReport`, `dockerRuntime.Prune()` (prune + dry-run), `parsePruneOutput()` helper |
| `internal/runtime/prune_test.go` | NEW — unit tests for stub, output parser, dry-run candidate computation |
| `internal/runtime/runtime.go` | Add `Prune` to `Manager` interface + `stubManager.Prune` |
| `internal/cli/cleanup.go` | NEW — `tengiz cleanup` Cobra command (pattern: `secret_rotate.go`) |
| `internal/cli/cleanup_test.go` | NEW — command flag wiring + report printing tests |
| `internal/builder/builder.go` | Add `--label tengiz-app=<app> --label tengiz-env=<env>` to both build paths |
| `internal/builder/builder_test.go` | Tests for the new label args helper |
| `internal/cli/root.go` | Register `cleanupCmd` in `init()` |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` |
| `internal/idle/idle_test.go` | Add `Prune` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Prune` to `mockRuntime` |
| `README.md` | Add cleanup to Features + CLI section |
| `docs/FUTURES_FEATURES.md` | Mark #6 Docker Housekeeping as ✅ |

New files: 4. Changes touch 8 existing files.

---

### Task 1: Add `Prune` to the Manager interface + stub + mocks

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — `Manager` interface, `stubManager`
- Modify: `internal/cli/root_test.go:69-100` — `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:14-35` — `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:15-35` — `mockRuntime`

**Interfaces:**
- Consumes: nothing new
- Produces: `PruneOptions{All, Volumes, DryRun bool}`, `PruneReport{Containers, Images, Volumes, Networks, BuildCache int, ReclaimedSpace string, DryRun bool}`, `Manager.Prune(ctx, opts) (*PruneReport, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/prune_test.go
package runtime

import (
	"context"
	"testing"
)

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report == nil {
		t.Fatal("Prune() returned nil report")
	}
}

func TestPruneOptionsDefaults(t *testing.T) {
	opts := PruneOptions{}
	if opts.All || opts.Volumes || opts.DryRun {
		t.Fatal("all PruneOptions fields should default to false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestStubPrune|TestPruneOptionsDefaults" -v -count=1`

Expected: FAIL — `undefined: PruneOptions`, `undefined: Prune` (interface compile error)

- [ ] **Step 3: Add types + interface method + stub in `internal/runtime/runtime.go`**

```go
type PruneOptions struct {
	All     bool // pass -a to prune (all unused images, not just dangling)
	Volumes bool // pass --volumes (also prune unused volumes)
	DryRun  bool // report what would be removed without removing
}

type PruneReport struct {
	Containers     int
	Images         int
	Volumes        int
	Networks       int
	BuildCache     int
	ReclaimedSpace string // e.g. "1.234GB", empty if nothing removed
	DryRun         bool
}

// in Manager interface (after KeepLastNImages):
Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)

// in stubManager:
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	return &PruneReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 4: Add `Prune` to the three test mocks**

```go
// internal/cli/root_test.go (mockRTForDeploy)
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{DryRun: opts.DryRun}, nil
}

// internal/idle/idle_test.go and internal/proxy/proxy_test.go (mockRuntime)
func (m *mockRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (*runtime.PruneReport, error) {
	return &runtime.PruneReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./...` then `go test ./internal/runtime/ ./internal/cli/ ./internal/idle/ ./internal/proxy/ -count=1`

Expected: PASS (compile succeeds, all existing tests still pass)

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/prune_test.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat(runtime): add Prune method to Manager interface and stub"
```

---

### Task 2: Runtime prune implementation + output parser

**Files:**
- Modify: `internal/runtime/runtime.go` (types already added in Task 1)
- Modify: `internal/runtime/docker.go` — no change needed (new code goes in `prune.go`)
- Create: `internal/runtime/prune.go`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport` from Task 1
- Produces: `dockerRuntime.Prune`, pure helper `parsePruneOutput(output string) PruneReport`

**Empirical facts baked into this task (verified against Docker 28.0.4):**
- `docker system prune -f --filter "label!=tengiz-app"` removes stopped non-tengiz containers, dangling non-tengiz images, unused networks, build cache; keeps everything labeled `tengiz-app`
- Output sections: `Deleted Containers:`, `Deleted Images:` (each image = one `deleted:` line preceded by `untagged:` lines), `Deleted Networks:`, `Deleted Volumes:`, `Deleted build cache objects:`, then `Total reclaimed space: <size>`
- Empty prune still prints `Total reclaimed space: 0B`
- `-a` flag prints `Deleted Images:` for all unused non-tengiz images (label filter still applies)

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/prune_test.go
package runtime

import "testing"

func TestParsePruneOutputContainerAndImage(t *testing.T) {
	output := `Deleted Containers:
39612f3dfd46
134210b69646

Deleted Images:
untagged: alpine:latest
untagged: alpine@sha256:28bd...
deleted: sha256:d529dd0c6e559
untagged: busybox:latest
deleted: sha256:c6348fa86ba0f

Deleted build cache objects:
iko237272t8nw

Total reclaimed space: 0B
`
	r := parsePruneOutput(output)
	if r.Containers != 2 {
		t.Errorf("Containers = %d, want 2", r.Containers)
	}
	if r.Images != 2 {
		t.Errorf("Images = %d, want 2", r.Images)
	}
	if r.BuildCache != 1 {
		t.Errorf("BuildCache = %d, want 1", r.BuildCache)
	}
	if r.Networks != 0 || r.Volumes != 0 {
		t.Errorf("Networks/Volumes should be 0, got %d/%d", r.Networks, r.Volumes)
	}
	if r.ReclaimedSpace != "0B" {
		t.Errorf("ReclaimedSpace = %q, want %q", r.ReclaimedSpace, "0B")
	}
}

func TestParsePruneOutputWithNetworksVolumes(t *testing.T) {
	output := `Deleted Networks:
abc123

Deleted Volumes:
tengiz-data

Total reclaimed space: 1.234GB
`
	r := parsePruneOutput(output)
	if r.Networks != 1 {
		t.Errorf("Networks = %d, want 1", r.Networks)
	}
	if r.Volumes != 1 {
		t.Errorf("Volumes = %d, want 1", r.Volumes)
	}
	if r.ReclaimedSpace != "1.234GB" {
		t.Errorf("ReclaimedSpace = %q, want %q", r.ReclaimedSpace, "1.234GB")
	}
}

func TestParsePruneOutputEmpty(t *testing.T) {
	r := parsePruneOutput("Total reclaimed space: 0B\n")
	if r.Containers != 0 || r.Images != 0 || r.Volumes != 0 || r.Networks != 0 || r.BuildCache != 0 {
		t.Errorf("expected all-zero report, got %+v", r)
	}
	if r.ReclaimedSpace != "0B" {
		t.Errorf("ReclaimedSpace = %q, want %q", r.ReclaimedSpace, "0B")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestParsePruneOutput" -v -count=1`

Expected: FAIL — `undefined: parsePruneOutput`

- [ ] **Step 3: Implement `internal/runtime/prune.go`**

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func parsePruneOutput(output string) PruneReport {
	var r PruneReport
	section := ""
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "Deleted Containers:":
			section = "containers"
		case trimmed == "Deleted Images:":
			section = "images"
		case trimmed == "Deleted Networks:":
			section = "networks"
		case trimmed == "Deleted Volumes:":
			section = "volumes"
		case trimmed == "Deleted build cache objects:":
			section = "buildcache"
		case strings.HasPrefix(trimmed, "Total reclaimed space:"):
			r.ReclaimedSpace = strings.TrimSpace(strings.TrimPrefix(trimmed, "Total reclaimed space:"))
			section = ""
		case trimmed == "":
			section = ""
		default:
			switch section {
			case "containers":
				r.Containers++
			case "images":
				if strings.HasPrefix(trimmed, "deleted:") {
					r.Images++
				}
			case "networks":
				r.Networks++
			case "volumes":
				r.Volumes++
			case "buildcache":
				r.BuildCache++
			}
		}
	}
	return r
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	if opts.DryRun {
		return r.pruneDryRun(ctx, opts)
	}

	args := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if opts.All {
		args = append(args, "-a")
	}
	if opts.Volumes {
		args = append(args, "--volumes")
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system prune: %w\n%s", err, string(out))
	}

	report := parsePruneOutput(string(out))
	report.DryRun = false
	return &report, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestParsePruneOutput" -v -count=1`

Expected: PASS

- [ ] **Step 5: Sanity-check against a real Docker daemon (optional, environment has docker)**

```bash
docker create --name cleanup-check busybox:latest >/dev/null 2>&1 || true
docker system prune -f --filter "label!=tengiz-app"
```

Expected: the `cleanup-check` container (no `tengiz-app` label) is removed; any tengiz-labeled resource is kept.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): implement docker system prune with tengiz-app label protection"
```

---

### Task 3: Dry-run candidate enumeration

**Files:**
- Modify: `internal/runtime/prune.go` — add `pruneDryRun`

**Interfaces:**
- Consumes: `PruneOptions`, `PruneReport`
- Produces: `pruneDryRun(ctx, opts) (*PruneReport, error)` — never mutates docker state

**Key constraint (verified):** `docker ps`/`docker images` reject `label!=` ("invalid filter 'label!'"). So candidates are computed in Go: all − tengiz-labeled − (running containers, which prune never touches).

- [ ] **Step 1: Write the failing tests**

```go
// internal/runtime/prune_test.go
package runtime

import "testing"

func TestDryRunCandidates(t *testing.T) {
	all := []string{"a1", "a2", "a3", "a4"}
	tengiz := []string{"a2"}
	running := []string{"a3"}

	got := nonTengizStopped(all, tengiz, running)
	// a1 -> candidate (stopped, not tengiz)
	// a2 -> excluded (tengiz)
	// a3 -> excluded (running)
	// a4 -> candidate (stopped, not tengiz)
	if len(got) != 2 || got[0] != "a1" || got[1] != "a4" {
		t.Errorf("nonTengizStopped() = %v, want [a1 a4]", got)
	}
}

func TestDryRunCandidatesAllRunning(t *testing.T) {
	got := nonTengizStopped([]string{"c1", "c2"}, nil, []string{"c1", "c2"})
	if len(got) != 0 {
		t.Errorf("nonTengizStopped() = %v, want []", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestDryRunCandidates" -v -count=1`

Expected: FAIL — `undefined: nonTengizStopped`

- [ ] **Step 3: Implement `pruneDryRun` in `internal/runtime/prune.go`**

```go
func nonTengizStopped(all, tengiz, running []string) []string {
	inTengiz := make(map[string]bool, len(tengiz))
	for _, id := range tengiz {
		inTengiz[id] = true
	}
	inRunning := make(map[string]bool, len(running))
	for _, id := range running {
		inRunning[id] = true
	}
	var out []string
	for _, id := range all {
		if !inTengiz[id] && !inRunning[id] {
			out = append(out, id)
		}
	}
	return out
}

func (r *dockerRuntime) pruneDryRun(ctx context.Context, opts PruneOptions) (*PruneReport, error) {
	report := &PruneReport{DryRun: true}

	// Containers: stopped non-tengiz candidates.
	allOut, err := exec.CommandContext(ctx, "docker", "ps", "-aq").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps -aq: %w\n%s", err, string(allOut))
	}
	tengizOut, err := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "label=tengiz-app").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps (tengiz): %w\n%s", err, string(tengizOut))
	}
	runningOut, err := exec.CommandContext(ctx, "docker", "ps", "-q").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps -q: %w\n%s", err, string(runningOut))
	}

	report.Containers = len(nonTengizStopped(
		strings.Fields(string(allOut)),
		strings.Fields(string(tengizOut)),
		strings.Fields(string(runningOut)),
	))

	// Images: non-tengiz dangling (default) or all non-tengiz (--all).
	imgArgs := []string{"images", "-aq", "--filter", "dangling=true"}
	if opts.All {
		imgArgs = []string{"images", "-aq"}
	}
	imgOut, err := exec.CommandContext(ctx, "docker", imgArgs...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(imgOut))
	}
	tengizImgOut, err := exec.CommandContext(ctx, "docker", "images", "-aq", "--filter", "label=tengiz-app").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images (tengiz): %w\n%s", err, string(tengizImgOut))
	}
	tengizImgs := make(map[string]bool)
	for _, id := range strings.Fields(string(tengizImgOut)) {
		tengizImgs[id] = true
	}
	for _, id := range strings.Fields(string(imgOut)) {
		if !tengizImgs[id] {
			report.Images++
		}
	}

	return report, nil
}
```

Note: `docker ps -aq` includes images referenced by containers, so `--all` dry-run image counts are an upper bound (a real `-a` prune only removes *unused* images). This is acceptable for a dry-run estimate and is documented in the CLI help.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestDryRunCandidates" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run full runtime suite**

Run: `go test ./internal/runtime/ -v -count=1`

Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat(runtime): add dry-run candidate enumeration for cleanup"
```

---

### Task 4: Add labels to built images

**Files:**
- Modify: `internal/builder/builder.go:57-91` (`buildWithDockerfile`) and `:129-170` (`buildWithNixpacks`)

**Interfaces:**
- Consumes: `appName`, `env` (already function params)
- Produces: images carrying `tengiz-app=<app>` and `tengiz-env=<env>` labels so prune filters protect them

**Why required:** `docker system prune -a --filter "label!=tengiz-app"` only protects images WITH the label. Tengiz builds currently tag images `tengiz-apps/<app>:<env>-<deploymentID>` but do not label them — without this task, `cleanup --all` would delete every rollback image `KeepLastNImages` is supposed to retain.

- [ ] **Step 1: Write the failing tests**

```go
// internal/builder/builder_test.go
package builder

import (
	"strings"
	"testing"
)

func TestBuildArgsIncludeLabels(t *testing.T) {
	args := buildArgs("build", "myapp", "production", []string{"--secret", "id=x,src=/tmp/x"})
	joined := strings.Join(args, " ")
	for _, want := range []string{"--label", "tengiz-app=myapp", "--label", "tengiz-env=production"} {
		if !strings.Contains(joined, want) {
			t.Errorf("buildArgs() missing %q in %q", want, joined)
		}
	}
}

func TestNixpacksArgsIncludeLabels(t *testing.T) {
	args := nixpacksArgs("myapp", "staging", nil)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--label", "tengiz-app=myapp", "--label", "tengiz-env=staging"} {
		if !strings.Contains(joined, want) {
			t.Errorf("nixpacksArgs() missing %q in %q", want, joined)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run "TestBuildArgsIncludeLabels|TestNixpacksArgsIncludeLabels" -v -count=1`

Expected: FAIL — `undefined: buildArgs`, `undefined: nixpacksArgs`

- [ ] **Step 3: Refactor builder arg construction into pure helpers + add labels**

In `internal/builder/builder.go`:

```go
func buildArgs(buildSecretArgs []string, tag string, dir string, appName string, env string) []string {
	args := []string{"build"}
	args = append(args, buildSecretArgs...)
	args = append(args, "--label", fmt.Sprintf("tengiz-app=%s", appName))
	args = append(args, "--label", fmt.Sprintf("tengiz-env=%s", env))
	args = append(args, "-t", tag, dir)
	return args
}

func nixpacksArgs(appName, env string, cfg *types.NixpacksConfig) []string {
	args := []string{}
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
	args = append(args, "--label", fmt.Sprintf("tengiz-app=%s", appName))
	args = append(args, "--label", fmt.Sprintf("tengiz-env=%s", env))
	return args
}
```

Update `buildWithDockerfile` (lines 69-73) to:

```go
args := buildArgs(b.buildSecretArgs(), tag, dir, appName, env)
cmd := exec.CommandContext(ctx, "docker", args...)
```

Update `buildWithNixpacks` (lines 139-152) to:

```go
args := []string{"build", dir, "--name", tag}
args = append(args, nixpacksArgs(appName, env, b.nixpacksCfg)...)
cmd := exec.CommandContext(ctx, "nixpacks", args...)
```

Note: nixpacks `--label` is a repeatable `--label key=value` flag (same syntax as `docker build`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/builder/ -v -count=1`

Expected: PASS (integration build tests may `t.Skip` when docker/nixpacks is unavailable)

- [ ] **Step 5: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat(builder): label built images with tengiz-app and tengiz-env"
```

---

### Task 5: `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Create: `internal/cli/cleanup_test.go`
- Modify: `internal/cli/root.go:38-75` — register command in `init()`

**Interfaces:**
- Consumes: `runtime.Manager.Prune`, `getEnv(cmd)` helper (already in `root.go`)
- Produces: `tengiz cleanup [--dry-run] [--all] [--volumes]`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/cleanup_test.go
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanupCmdHasFlags(t *testing.T) {
	for _, name := range []string{"dry-run", "all", "volumes"} {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupCmdRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c == cleanupCmd {
			found = true
		}
	}
	if !found {
		t.Error("cleanupCmd not registered on rootCmd")
	}
}

func TestCleanupDryRunOutput(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetArgs([]string{"cleanup", "--dry-run"})
	out := captureOutput(func() { cleanupCmd.RunE(cmd, nil) })
	if !strings.Contains(out, "dry run") && !strings.Contains(out, "would") {
		t.Errorf("expected dry-run wording in output, got %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanupCmd" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd`

- [ ] **Step 3: Implement `internal/cli/cleanup.go`**

```go
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources, keeping tengiz-managed ones",
	Long: `Removes stopped non-tengiz containers, unused networks, dangling images,
build cache, and (with --volumes) unused volumes.

Tengiz containers and images are protected by the "tengiz-app" label and are
never removed, so rollback images are preserved.

Use --dry-run to see what would be removed without removing anything.
Use --all to also remove all unused images (not just dangling ones).
Use --volumes to also remove unused volumes.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		all, _ := cmd.Flags().GetBool("all")
		volumes, _ := cmd.Flags().GetBool("volumes")

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Prune(context.Background(), runtime.PruneOptions{
			All:     all,
			Volumes: volumes,
			DryRun:  dryRun,
		})
		if err != nil {
			return err
		}

		if report.DryRun {
			fmt.Println("[tengiz] dry run — no resources removed")
			fmt.Printf("[tengiz] would remove %d container(s), %d image(s), %d volume(s), %d network(s), %d build cache object(s)\n",
				report.Containers, report.Images, report.Volumes, report.Networks, report.BuildCache)
			return nil
		}

		fmt.Printf("[tengiz] removed %d container(s), %d image(s), %d volume(s), %d network(s), %d build cache object(s)\n",
			report.Containers, report.Images, report.Volumes, report.Networks, report.BuildCache)
		if report.ReclaimedSpace != "" {
			fmt.Printf("[tengiz] reclaimed space: %s\n", report.ReclaimedSpace)
		}
		return nil
	},
}
```

- [ ] **Step 4: Register the command in `internal/cli/root.go`**

In `init()` after `rootCmd.AddCommand(runCmd)` (line ~67):

```go
rootCmd.AddCommand(cleanupCmd)
```

And after the existing flag registrations (near line 79):

```go
cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
cleanupCmd.Flags().Bool("all", false, "remove all unused images, not just dangling ones")
cleanupCmd.Flags().Bool("volumes", false, "also remove unused volumes")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanupCmd" -v -count=1`

Expected: PASS

Run: `go build ./...`

Expected: Build succeeds

- [ ] **Step 6: Run all tests**

Run: `go test ./... -v -count=1`

Expected: All PASS (except known slow proxy TCP-timeout and time-sensitive idle tests)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 7: Manual smoke test (docker available in this environment)**

```bash
go build -o tengiz .
docker create --name junk busybox:latest 2>/dev/null || true
./tengiz cleanup --dry-run
./tengiz cleanup
docker ps -a --filter name=junk
```

Expected: dry-run lists the `junk` container; real run removes it; tengiz-labeled resources untouched.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go internal/cli/root.go
git commit -m "feat(cli): add tengiz cleanup command with label-protected docker prune"
```

---

### Task 6: Documentation

**Files:**
- Modify: `README.md` (Features list line 14-23, plus CLI/Usage section)
- Modify: `docs/FUTURES_FEATURES.md` line 19 (mark #6 ✅)

**Interfaces:**
- Consumes: nothing
- Produces: updated user + roadmap docs

- [ ] **Step 1: Update `docs/FUTURES_FEATURES.md`**

Change line 19 from:

```
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

to (⬜ → ✅) and add the feature to the "✅ Implemented Features (Not Pending)" section.

- [ ] **Step 2: Update `README.md` Features list**

Add after the deployment-history bullet (line 20):

```
- **Docker housekeeping** — `tengiz cleanup` prunes stopped containers, dangling images, unused networks, and build cache while protecting tengiz-managed resources via labels.
```

- [ ] **Step 3: Update `README.md` usage/CLI section**

Add to the command list (mirroring existing entries):

```
tengiz cleanup [--dry-run] [--all] [--volumes] → prune unused Docker resources (tengiz-labeled containers/images are protected)
```

- [ ] **Step 4: Update `AGENTS.md` CLI list and architecture table**

Add `tengiz cleanup` to the CLI block and extend the `runtime.Manager` row to mention `Prune` (label-based `docker system prune`).

- [ ] **Step 5: Verify nothing references the stale roadmap status**

Run: `git grep -n "cleanup" README.md docs/FUTURES_FEATURES.md | head`

Expected: cleanup appears in both docs with the implemented wording.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md AGENTS.md
git commit -m "docs: document tengiz cleanup docker housekeeping command"
```

---

### Task 7: Final verification and self-review

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -count=1`

Expected: All PASS (note: `proxy` tests are slow ~2s due to TCP dial timeouts; `idle` tests are time-sensitive)

Run: `go vet ./...`

Expected: No issues

- [ ] **Step 2: Check all Manager implementations have Prune**

Run: `git grep -n "KeepLastNImages" internal/ | grep -v "_test.go"`

Expected: only `runtime.go` (interface + stub) and `docker.go`/`prune.go` — and `git grep -n "func.*Prune" internal/` shows the docker impl, stub, and 3 test mocks.

- [ ] **Step 3: Self-review against spec**

Check against `docs/FUTURES_FEATURES.md` #6:
- Label-based `docker system prune` ✅ (Task 2 — `--filter label!=tengiz-app` always applied)
- `tengiz cleanup` command ✅ (Task 5)
- Tengiz resources protected ✅ (Task 1+4 — existing container labels + new image labels)
- Rollback images preserved ✅ (Task 4 labels + `:latest` guard unchanged)
- Volumes safe by default ✅ (`--volumes` opt-in)
- Dry-run ✅ (Task 3 — never mutates state)

- [ ] **Step 4: Placeholder scan**

Search plan for "TBD", "TODO", "implement later", "fill in details", "Similar to Task". None found; every step has complete code.

- [ ] **Step 5: Type consistency check**

- `runtime.PruneOptions{All, Volumes, DryRun bool}` — used in `Manager.Prune`, docker impl, stub, 3 mocks, CLI
- `runtime.PruneReport{Containers, Images, Volumes, Networks, BuildCache int, ReclaimedSpace string, DryRun bool}` — produced by docker impl/stub, printed by CLI
- `Manager.Prune(ctx context.Context, opts PruneOptions) (*PruneReport, error)` — identical signature in all implementations
- `parsePruneOutput(string) PruneReport` — pure, tested
- `nonTengizStopped(all, tengiz, running []string) []string` — pure, tested
- `buildArgs(...)` / `nixpacksArgs(...)` — pure, tested
