# Docker Housekeeping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that label-scopes `docker system prune`-style pruning to Tengiz-managed resources only, so operators can reclaim disk space without ever touching non-Tengiz containers, images, volumes, or networks.

**Architecture:** All Tengiz images are labeled at build time with `tengiz-app=<app>` and `tengiz-env=<env>` (Dockerfile builds pass `--label` to `docker build`; Nixpacks images get the same labels via a metadata-only relabel build). A new `runtime.Manager.Prune` method runs per-resource `docker <resource> prune` commands filtered by `label=tengiz-app` (optionally `label=tengiz-app=<app>`), parses the reclaimed space, and returns a report. A thin `tengiz cleanup` CLI command (new `internal/cli/cleanup.go`) maps flags to `PruneOptions`, prompts for confirmation unless `--force`, and prints the report.

**Tech Stack:** Go 1.26, Cobra (CLI), `os/exec` Docker CLI (no Docker SDK), existing `runtime.Manager` interface + `builder.Builder` package.

## Global Constraints

- Label keys: `tengiz-app` and `tengiz-env` (already used on containers in `internal/runtime/docker.go`; must be reused, not duplicated)
- Image repo prefix: `tengiz-apps` (already used in `internal/builder/builder.go` and `internal/runtime/cleanup.go`)
- Pruning must ONLY ever touch resources carrying the `tengiz-app` label — non-Tengiz Docker resources are never affected
- Default environment is `"production"` — `getEnv(cmd)` must be reused, same as every other command
- No new external dependencies
- Docker CLI is invoked via `os/exec` exactly like the rest of the `runtime` package (no Docker SDK)
- Every docker command sequence must be built by a pure function (arg-building) that is unit-testable without Docker installed
- Existing tests must continue to pass; the `mockRTForDeploy` mock in `internal/cli/root_test.go` implements `runtime.Manager` and MUST be updated when the interface grows

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/types/types.go` | New `LabelAppKey` / `LabelEnvKey` constants (single source of truth) |
| `internal/builder/builder.go` | Label built images at build time (Dockerfile + Nixpacks paths) |
| `internal/builder/builder_test.go` | Unit tests for `buildImageArgs` / `relabelImageArgs` |
| `internal/runtime/runtime.go` | New `PruneOptions`, `PruneReport`, `Manager.Prune` + stub impl |
| `internal/runtime/prune.go` | **New file** — `buildPruneArgs`, `parsePruneOutput`, `parseSize`, `formatSize`, `dockerRuntime.Prune` |
| `internal/runtime/prune_test.go` | **New file** — unit tests for the prune arg-building/parsing helpers |
| `internal/runtime/cleanup_test.go` | Stub `Prune` test |
| `internal/cli/cleanup.go` | **New file** — `cleanupCmd`, `confirmCleanup`, `printPruneReport` |
| `internal/cli/root.go` | Register `cleanupCmd` |
| `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy`; command registration + report tests |
| `README.md` | `### tengiz cleanup` CLI reference section |
| `AGENTS.md` | `tengiz cleanup` in CLI list; `Prune` in `runtime.Manager` table |

No new external packages. Two new Go files, five modified Go files, two modified docs.

---

### Task 1: Label Dockerfile-built images at build time

**Files:**
- Modify: `internal/types/types.go` — add constants after the `NotificationEvent` block (line ~13)
- Modify: `internal/builder/builder.go:57-91` — replace inline arg building with `buildImageArgs`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `types.LabelAppKey string` (`"tengiz-app"`), `types.LabelEnvKey string` (`"tengiz-env"`), `builder.buildImageArgs(appName, env, tag, dir string, secretArgs []string) []string`

- [ ] **Step 1: Write the failing test**

```go
// internal/builder/builder_test.go (append to end of file)
func TestBuildImageArgsIncludesLabels(t *testing.T) {
	args := buildImageArgs("myapp", "production", "tengiz-apps/myapp:production-1", ".", []string{"--secret", "id=npm,src=/tmp/npm"})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"build",
		"--label tengiz-app=myapp",
		"--label tengiz-env=production",
		"-t tengiz-apps/myapp:production-1",
		".",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("buildImageArgs() missing %q in: %s", want, joined)
		}
	}
	if !strings.Contains(joined, "--secret id=npm,src=/tmp/npm") {
		t.Errorf("buildImageArgs() dropped secret args: %s", joined)
	}
}

func TestBuildImageArgsDefaultsEnv(t *testing.T) {
	args := buildImageArgs("myapp", "", "tengiz-apps/myapp:production-1", ".", nil)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--label tengiz-env=production") {
		t.Errorf("buildImageArgs() env should default to production, got: %s", joined)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run "TestBuildImageArgs" -v -count=1`

Expected: FAIL with `undefined: buildImageArgs`

- [ ] **Step 3: Add the shared label constants to `internal/types/types.go`**

```go
// internal/types/types.go — append after the NotificationEvent constants (after line 13)
const (
	LabelAppKey = "tengiz-app"
	LabelEnvKey = "tengiz-env"
)
```

- [ ] **Step 4: Add `buildImageArgs` and wire it into `buildWithDockerfile`**

In `internal/builder/builder.go`, add the helper (place it directly above `buildWithDockerfile`):

```go
func buildImageArgs(appName, env, tag, dir string, secretArgs []string) []string {
	if env == "" {
		env = "production"
	}
	args := []string{"build"}
	args = append(args, secretArgs...)
	args = append(args,
		"--label", fmt.Sprintf("%s=%s", types.LabelAppKey, appName),
		"--label", fmt.Sprintf("%s=%s", types.LabelEnvKey, env),
		"-t", tag,
		dir,
	)
	return args
}
```

Replace the body of `buildWithDockerfile` (lines 57-91) with:

```go
func (b *Builder) buildWithDockerfile(ctx context.Context, dir string, appName string, env string, deploymentID string) (string, string, error) {
	if env == "" {
		env = "production"
	}
	tag := fmt.Sprintf("tengiz-apps/%s:%s-%s", appName, env, deploymentID)

	cleanup, err := b.writeBuildSecrets()
	if err != nil {
		return "", "", err
	}
	defer cleanup()

	args := buildImageArgs(appName, env, tag, dir, b.buildSecretArgs())

	cmd := exec.CommandContext(ctx, "docker", args...)

	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(os.Stdout, &logBuf)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	if err := cmd.Run(); err != nil {
		return "", logBuf.String(), fmt.Errorf("docker build: %w", err)
	}

	latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", appName, env)
	tagCmd := exec.CommandContext(ctx, "docker", "tag", tag, latestTag)
	if out, err := tagCmd.CombinedOutput(); err != nil {
		return "", logBuf.String() + string(out), fmt.Errorf("docker tag latest: %w\n%s", err, string(out))
	}

	return tag, logBuf.String(), nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/builder/ -run "TestBuildImageArgs" -v -count=1`

Expected: PASS

- [ ] **Step 6: Run the full builder suite**

Run: `go test ./internal/builder/... -count=1`

Expected: PASS (integration tests that need Docker will `t.Skip` on machines without Docker — this is pre-existing behavior)

- [ ] **Step 7: Commit**

```bash
git add internal/types/types.go internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: label built images with tengiz-app/tengiz-env"
```

---

### Task 2: Label Nixpacks-built images

**Files:**
- Modify: `internal/builder/builder.go:129-170` — add relabel step in `buildWithNixpacks`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: `types.LabelAppKey`, `types.LabelEnvKey` (from Task 1)
- Produces: `builder.relabelImageArgs(appName, env, tag string) []string`

- [ ] **Step 1: Write the failing test**

```go
// internal/builder/builder_test.go (append)
func TestRelabelImageArgs(t *testing.T) {
	args := relabelImageArgs("myapp", "production", "tengiz-apps/myapp:production-1")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"build",
		"--label tengiz-app=myapp",
		"--label tengiz-env=production",
		"-t tengiz-apps/myapp:production-1",
		"-f -",
		".",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("relabelImageArgs() missing %q in: %s", want, joined)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/builder/ -run "TestRelabelImageArgs" -v -count=1`

Expected: FAIL with `undefined: relabelImageArgs`

- [ ] **Step 3: Add `relabelImageArgs` and wire it into `buildWithNixpacks`**

In `internal/builder/builder.go`, add the helper:

```go
func relabelImageArgs(appName, env, tag string) []string {
	if env == "" {
		env = "production"
	}
	return []string{"build",
		"--label", fmt.Sprintf("%s=%s", types.LabelAppKey, appName),
		"--label", fmt.Sprintf("%s=%s", types.LabelEnvKey, env),
		"-t", tag,
		"-f", "-",
		".",
	}
}
```

In `buildWithNixpacks`, insert this block immediately after the nixpacks `cmd.Run()` success check (`if err := cmd.Run(); err != nil { ... }`) and BEFORE the `latestTag` block (currently lines 163-167):

```go
	// Re-tag the nixpacks image with Tengiz labels so label-based pruning
	// (tengiz cleanup) works uniformly for every build backend. The FROM
	// image resolves from the local store; this is a metadata-only build.
	relabelCmd := exec.CommandContext(ctx, "docker", relabelImageArgs(appName, env, tag)...)
	relabelCmd.Stdin = strings.NewReader(fmt.Sprintf("FROM %s\n", tag))
	if out, err := relabelCmd.CombinedOutput(); err != nil {
		return "", logBuf.String() + string(out), fmt.Errorf("docker relabel: %w\n%s", err, string(out))
	}
```

The existing `latestTag := fmt.Sprintf("tengiz-apps/%s:%s-latest", ...)` block stays where it is (it must run AFTER the relabel so `-latest` also points at the labeled image).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/builder/ -run "TestRelabelImageArgs" -v -count=1`

Expected: PASS

- [ ] **Step 5: Run the full builder suite**

Run: `go test ./internal/builder/... -count=1`

Expected: PASS (`TestBuildWithNixpacksDispatches` skips when nixpacks CLI is absent — pre-existing behavior)

- [ ] **Step 6: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: apply tengiz labels to nixpacks-built images"
```

---

### Task 3: Add `Prune` to the runtime.Manager interface

**Files:**
- Modify: `internal/runtime/runtime.go` — types + interface method + stub impl
- Modify: `internal/cli/root_test.go:98` — add `Prune` to `mockRTForDeploy`
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks
- Produces: `runtime.PruneOptions{Containers, Images, Volumes, Networks, AllImages bool; App string}`, `runtime.PruneReport{Containers, Images, Volumes, Networks []string; Reclaimed string}`, `Manager.Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/cleanup_test.go (append)
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{Containers: true, Images: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(report.Containers) != 0 || len(report.Images) != 0 || report.Reclaimed != "" {
		t.Errorf("expected empty report, got %+v", report)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestStubPrune" -v -count=1`

Expected: FAIL with `m.Prune undefined (type Manager has no field or method Prune)`

- [ ] **Step 3: Add the types and interface method to `internal/runtime/runtime.go`**

Add after the `RunOptions` struct (line 29):

```go
type PruneOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	AllImages  bool // pass -a to docker image prune (remove all unused images, not just dangling)
	App        string
}

type PruneReport struct {
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
	Reclaimed  string // human-readable total reclaimed space, e.g. "1.5MiB"
}
```

Add to the `Manager` interface (after `KeepLastNImages`, line 36):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

Add the stub implementation (after `KeepLastNImages` stub, line 119):

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{}, nil
}
```

- [ ] **Step 4: Update `mockRTForDeploy` in `internal/cli/root_test.go`**

Add this method next to the other mock methods (after `KeepLastNImages`, line 99):

```go
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{}, nil
}
```

This is REQUIRED — `mockRTForDeploy` implements `runtime.Manager` and the build fails to compile without it.

- [ ] **Step 5: Run tests to verify they pass and the build compiles**

Run: `go build ./... && go test ./internal/runtime/... ./internal/cli/ -run "TestStubPrune" -count=1`

Expected: build succeeds, `TestStubPrune` PASSes

- [ ] **Step 6: Run all tests**

Run: `go test ./... -count=1`

Expected: PASS (any Docker-dependent integration tests skip on Docker-less hosts — pre-existing)

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/cleanup_test.go internal/cli/root_test.go
git commit -m "feat: add Prune to runtime.Manager interface and stub"
```

---

### Task 4: Implement `dockerRuntime.Prune` with label-scoped pruning

**Files:**
- Create: `internal/runtime/prune.go`
- Test: `internal/runtime/prune_test.go` (new file)

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneReport` (Task 3), `types.LabelAppKey` (Task 1)
- Produces: `buildPruneArgs(opts PruneOptions) [][]string`, `parsePruneOutput(out string) ([]string, string)`, `parseSize(s string) float64`, `formatSize(bytes float64) string`

- [ ] **Step 1: Write the failing test**

```go
// internal/runtime/prune_test.go
package runtime

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildPruneArgsDefaultCategories(t *testing.T) {
	cmds := buildPruneArgs(PruneOptions{Containers: true, Images: true, Networks: true})
	if len(cmds) != 3 {
		t.Fatalf("expected 3 commands, got %d: %v", len(cmds), cmds)
	}
	if got := strings.Join(cmds[0], " "); !strings.Contains(got, "container prune -f --filter label=tengiz-app") {
		t.Errorf("container prune args wrong: %s", got)
	}
	if got := strings.Join(cmds[1], " "); !strings.Contains(got, "image prune -f --filter label=tengiz-app") {
		t.Errorf("image prune args wrong: %s", got)
	}
	if got := strings.Join(cmds[2], " "); !strings.Contains(got, "network prune -f --filter label=tengiz-app") {
		t.Errorf("network prune args wrong: %s", got)
	}
}

func TestBuildPruneArgsAppScoped(t *testing.T) {
	cmds := buildPruneArgs(PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, App: "myapp"})
	if len(cmds) != 4 {
		t.Fatalf("expected 4 commands, got %d: %v", len(cmds), cmds)
	}
	for _, c := range cmds {
		if got := strings.Join(c, " "); !strings.Contains(got, "label=tengiz-app=myapp") {
			t.Errorf("expected app-scoped filter, got: %s", got)
		}
	}
}

func TestBuildPruneArgsAllImagesFlag(t *testing.T) {
	cmds := buildPruneArgs(PruneOptions{Images: true, AllImages: true})
	if got := strings.Join(cmds[0], " "); !strings.Contains(got, "image prune -f -a") {
		t.Errorf("expected -a flag, got: %s", got)
	}
}

func TestBuildPruneArgsNoFlags(t *testing.T) {
	if cmds := buildPruneArgs(PruneOptions{}); len(cmds) != 0 {
		t.Errorf("expected no commands for empty options, got %v", cmds)
	}
}

func TestParsePruneOutput(t *testing.T) {
	out := "Deleted Containers:\nabc123def456\n\nTotal reclaimed space: 1.5KB\n"
	ids, reclaimed := parsePruneOutput(out)
	if len(ids) != 1 || ids[0] != "abc123def456" {
		t.Errorf("ids = %v, want [abc123def456]", ids)
	}
	if reclaimed != "1.5KB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "1.5KB")
	}
}

func TestParsePruneOutputSkipsHeaders(t *testing.T) {
	out := "Deleted Images:\nuntagged: tengiz-apps/myapp:production-1\ndeleted: sha256:abcdef\n\nTotal reclaimed space: 2MB\n"
	ids, reclaimed := parsePruneOutput(out)
	if len(ids) != 2 {
		t.Errorf("ids = %v, want 2 entries", ids)
	}
	if reclaimed != "2MB" {
		t.Errorf("reclaimed = %q, want %q", reclaimed, "2MB")
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]float64{
		"0B":    0,
		"1KB":   1e3,
		"1MB":   1e6,
		"1GB":   1e9,
		"1KiB":  1024,
		"1MiB":  1 << 20,
		"1.5GB": 1.5e9,
		"":      0,
	}
	for in, want := range cases {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	if got := formatSize(0); got != "0B" {
		t.Errorf("formatSize(0) = %q, want 0B", got)
	}
	if got := formatSize(1 << 20); got != "1.00MiB" {
		t.Errorf("formatSize(1MiB) = %q, want 1.00MiB", got)
	}
	if got := formatSize(1.5e9); got != "1.40GiB" {
		t.Errorf("formatSize(1.5GB) = %q, want 1.40GiB", got)
	}
}

func TestPruneCmdCount(t *testing.T) {
	// sanity: a full prune produces exactly one docker invocation per category
	cmds := buildPruneArgs(PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true})
	if len(cmds) != 4 {
		t.Fatalf("expected 4 commands, got %d", len(cmds))
	}
	_ = fmt.Sprint(cmds)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestBuildPruneArgs|TestParsePruneOutput|TestParseSize|TestFormatSize|TestPruneCmdCount" -v -count=1`

Expected: FAIL with `undefined: buildPruneArgs`, `undefined: parsePruneOutput`, `undefined: parseSize`, `undefined: formatSize`

- [ ] **Step 3: Create `internal/runtime/prune.go`**

```go
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

const reclaimedPrefix = "Total reclaimed space:"

func buildPruneArgs(opts PruneOptions) [][]string {
	filter := "label=" + types.LabelAppKey
	if opts.App != "" {
		filter = filter + "=" + opts.App
	}

	var cmds [][]string
	if opts.Containers {
		cmds = append(cmds, []string{"container", "prune", "-f", "--filter", filter})
	}
	if opts.Images {
		imageArgs := []string{"image", "prune", "-f"}
		if opts.AllImages {
			imageArgs = append(imageArgs, "-a")
		}
		imageArgs = append(imageArgs, "--filter", filter)
		cmds = append(cmds, imageArgs)
	}
	if opts.Volumes {
		cmds = append(cmds, []string{"volume", "prune", "-f", "--filter", filter})
	}
	if opts.Networks {
		cmds = append(cmds, []string{"network", "prune", "-f", "--filter", filter})
	}
	return cmds
}

func parsePruneOutput(out string) ([]string, string) {
	var ids []string
	reclaimed := ""
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		switch line {
		case "", "Deleted Containers:", "Deleted Images:", "Deleted Volumes:", "Deleted Networks:":
			continue
		}
		if strings.HasPrefix(line, reclaimedPrefix) {
			reclaimed = strings.TrimSpace(strings.TrimPrefix(line, reclaimedPrefix))
			continue
		}
		ids = append(ids, line)
	}
	return ids, reclaimed
}

func parseSize(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0B" {
		return 0
	}
	units := map[string]float64{
		"B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
		"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
	}
	for u, mult := range units {
		if strings.HasSuffix(s, u) {
			num := strings.TrimSpace(strings.TrimSuffix(s, u))
			v, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0
			}
			return v * mult
		}
	}
	return 0
}

func formatSize(bytes float64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.2fGiB", bytes/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.2fMiB", bytes/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.2fKiB", bytes/(1<<10))
	default:
		return fmt.Sprintf("%.0fB", bytes)
	}
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	var report PruneReport
	total := 0.0
	for _, args := range buildPruneArgs(opts) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return report, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
		ids, reclaimed := parsePruneOutput(string(out))
		switch args[0] {
		case "container":
			report.Containers = ids
		case "image":
			report.Images = ids
		case "volume":
			report.Volumes = ids
		case "network":
			report.Networks = ids
		}
		total += parseSize(reclaimed)
	}
	report.Reclaimed = formatSize(total)
	return report, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestBuildPruneArgs|TestParsePruneOutput|TestParseSize|TestFormatSize|TestPruneCmdCount" -v -count=1`

Expected: PASS (all 10 new tests)

- [ ] **Step 5: Run all runtime + builder tests**

Run: `go test ./internal/runtime/... ./internal/builder/... -count=1`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/prune.go internal/runtime/prune_test.go
git commit -m "feat: label-scoped docker prune with reclaimed-space reporting"
```

---

### Task 5: Add the `tengiz cleanup` CLI command

**Files:**
- Create: `internal/cli/cleanup.go`
- Modify: `internal/cli/root.go:38-77` — register `cleanupCmd` in `init()`
- Modify: `internal/cli/root_test.go` — registration + report tests

**Interfaces:**
- Consumes: `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.NewDocker()` (Task 4), `getEnv(cmd)` (root.go)
- Produces: `cleanupCmd` (Cobra command), `confirmCleanup() bool`, `printPruneReport(report runtime.PruneReport, env string)`

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/root_test.go (append)
func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, name := range []string{"containers", "images", "volumes", "networks", "all", "all-images", "app", "force"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanup missing --%s flag", name)
		}
	}
}

func TestPrintPruneReportEmpty(t *testing.T) {
	output := captureOutput(func() {
		printPruneReport(runtime.PruneReport{}, "production")
	})
	if !strings.Contains(output, "nothing to clean") {
		t.Errorf("expected 'nothing to clean', got %q", output)
	}
}

func TestPrintPruneReportNonEmpty(t *testing.T) {
	report := runtime.PruneReport{
		Containers: []string{"abc123"},
		Images:     []string{"sha256:aaa"},
		Networks:   []string{"n1"},
		Reclaimed:  "1.5MiB",
	}
	output := captureOutput(func() {
		printPruneReport(report, "production")
	})
	for _, want := range []string{
		"containers removed: 1",
		"images removed: 1",
		"networks removed: 1",
		"total reclaimed space: 1.5MiB",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("report missing %q, got %q", want, output)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestPrintPruneReport" -v -count=1`

Expected: FAIL — `cleanup command not registered` (root_test.go compile note: `printPruneReport` is also undefined until Step 4)

- [ ] **Step 3: Register `cleanupCmd` in `internal/cli/root.go`**

In `init()` (after `rootCmd.AddCommand(psCmd)`, line 41):

```go
	rootCmd.AddCommand(cleanupCmd)
```

No other changes to `root.go`.

- [ ] **Step 4: Create `internal/cli/cleanup.go`**

```go
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/runtime"
)

func init() {
	cleanupCmd.Flags().Bool("containers", false, "prune stopped containers")
	cleanupCmd.Flags().Bool("images", false, "prune images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused networks")
	cleanupCmd.Flags().Bool("all", false, "prune containers, images, volumes, and networks")
	cleanupCmd.Flags().Bool("all-images", false, "also remove all unused Tengiz images (docker image prune -a)")
	cleanupCmd.Flags().String("app", "", "scope pruning to a single app (label tengiz-app=<app>)")
	cleanupCmd.Flags().BoolP("force", "f", false, "skip the confirmation prompt")
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Prune unused Docker resources managed by Tengiz",
	Long: `Removes stopped containers, dangling images, unused volumes and unused
networks that belong to Tengiz (identified by the "tengiz-app" label).
Resources not managed by Tengiz are never touched.

By default only stopped containers, dangling images and unused networks
are pruned. Use --volumes to also prune unused volumes, or --all to prune
everything. Use --app to scope pruning to a single application.

Pass --force to skip the confirmation prompt (required in scripts/CI).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env := getEnv(cmd)
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		all, _ := cmd.Flags().GetBool("all")
		allImages, _ := cmd.Flags().GetBool("all-images")
		app, _ := cmd.Flags().GetString("app")
		force, _ := cmd.Flags().GetBool("force")

		if all {
			containers, images, volumes, networks = true, true, true, true
		}
		if !containers && !images && !volumes && !networks {
			containers, images, networks = true, true, true
		}

		if !force {
			if !confirmCleanup() {
				fmt.Println("[tengiz] cleanup aborted")
				return nil
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Prune(cmd.Context(), runtime.PruneOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			AllImages:  allImages,
			App:        app,
		})
		if err != nil {
			return err
		}

		printPruneReport(report, env)
		return nil
	},
}

func confirmCleanup() bool {
	fmt.Print("[tengiz] Remove stopped containers, dangling images, unused volumes and networks managed by Tengiz? [y/N] ")
	var resp string
	if _, err := fmt.Fscanln(os.Stdin, &resp); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(resp), "y")
}

func printPruneReport(report runtime.PruneReport, env string) {
	total := len(report.Containers) + len(report.Images) + len(report.Volumes) + len(report.Networks)
	if total == 0 {
		fmt.Printf("[tengiz] nothing to clean (%s)\n", env)
		return
	}
	fmt.Printf("[tengiz] cleanup complete (%s)\n", env)
	if n := len(report.Containers); n > 0 {
		fmt.Printf("  containers removed: %d\n", n)
	}
	if n := len(report.Images); n > 0 {
		fmt.Printf("  images removed: %d\n", n)
	}
	if n := len(report.Volumes); n > 0 {
		fmt.Printf("  volumes removed: %d\n", n)
	}
	if n := len(report.Networks); n > 0 {
		fmt.Printf("  networks removed: %d\n", n)
	}
	if report.Reclaimed != "" {
		fmt.Printf("  total reclaimed space: %s\n", report.Reclaimed)
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanupCommandRegistered|TestPrintPruneReport" -v -count=1`

Expected: PASS

- [ ] **Step 6: Build the binary and smoke-test help output**

Run: `go build -o /tmp/tengiz . && /tmp/tengiz cleanup --help`

Expected: prints usage with all 8 flags listed

- [ ] **Step 7: Run all tests**

Run: `go test ./... -count=1`

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add tengiz cleanup command for label-scoped pruning"
```

---

### Task 6: Document the feature

**Files:**
- Modify: `README.md` — add `### tengiz cleanup` under the CLI Reference
- Modify: `AGENTS.md` — CLI list + `runtime.Manager` table row

**Interfaces:**
- Consumes: nothing code-related
- Produces: documentation only (no code)

- [ ] **Step 1: Add the `tengiz cleanup` section to `README.md`**

Insert after the `### tengiz rm <app>` section (after line 228) and before `### tengiz rollback <app>` (line 230):

```markdown
### `tengiz cleanup`

Prune unused Docker resources that belong to Tengiz, identified by the `tengiz-app` label. Resources not managed by Tengiz are never touched.

```bash
tengiz cleanup                # prune stopped containers, dangling images, unused networks
tengiz cleanup --volumes      # also prune unused volumes
tengiz cleanup --all          # prune containers, images, volumes, and networks
tengiz cleanup --app myapp    # scope pruning to a single app
tengiz cleanup --all-images   # also remove all unused Tengiz images (docker image prune -a)
tengiz cleanup -f             # skip the confirmation prompt (required for scripts/CI)
```
```

- [ ] **Step 2: Add the command to the `AGENTS.md` CLI list**

In `AGENTS.md`, add a line in the CLI code block after the `rollback` line:

```
tengiz cleanup             → label-scoped prune of stopped containers, dangling images, unused volumes/networks (--app, --all, --all-images, -f)
```

- [ ] **Step 3: Update the `runtime.Manager` row in `AGENTS.md`**

Change the existing `runtime.Manager` table row so it ends with:

```
Also: `CreateFromImage`, `RemoveImage`, `KeepLastNImages` for rollback + image cleanup, and `Prune` for label-scoped housekeeping. `ContainerName(name, env)` helper.
```

- [ ] **Step 4: Verify nothing is broken**

Run: `go build ./... && go vet ./... && go test ./... -count=1`

Expected: build + vet clean, all tests PASS

- [ ] **Step 5: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: document tengiz cleanup command"
```

---

## Self-Review

**1. Spec coverage:**
- "Disk space is the #1 production issue" → `tengiz cleanup` reclaims disk via label-scoped prunes (Tasks 4-5).
- "Label-based `docker system prune`" → images are labeled at build (Tasks 1-2), pruning filters on `label=tengiz-app` (Task 4), and mirrors `docker system prune` defaults: stopped containers + dangling images + unused networks, with `--volumes`/`--all` opt-ins (Task 5).
- "`tengiz cleanup`" → CLI command with confirmation, `--force`, `--app` scoping, and a report (Task 5).
- AGENTS.md rule "UI/UX değişikliklerinde README.md ve dokümantasyonu güncelle" → README + AGENTS.md updated (Task 6).
- AGENTS.md rule "Her değişiklikte test ekle/güncelle, testleri geçir, sonra commit et" → every task is TDD with a commit gate.

**2. Placeholder scan:** No TBD/TODO, no vague "handle edge cases" steps; every code step contains complete code; test commands include expected output.

**3. Type consistency:**
- `PruneOptions` fields (`Containers`, `Images`, `Volumes`, `Networks`, `AllImages`, `App`) are identical in Task 3 (definition), Task 4 (`buildPruneArgs` reads them), and Task 5 (CLI constructs it).
- `PruneReport` fields (`Containers`, `Images`, `Volumes`, `Networks`, `Reclaimed`) consistent across Tasks 3-5.
- `types.LabelAppKey`/`types.LabelEnvKey` defined in Task 1, consumed in Tasks 2 and 4.
- `runtime.Manager.Prune` signature matches stub (Task 3), docker impl (Task 4), mock (Task 3), and CLI call (Task 5).
- `getEnv(cmd)` reused from root.go in Task 5 — no new env logic.
