# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that safely prunes stopped non-Tengiz containers, unused non-Tengiz images, dangling volumes, and unused custom networks — while always protecting Tengiz-managed resources — so a single-server Tengiz host never runs out of disk from stale Docker objects.

**Architecture:** A new `Prune(ctx, opts PruneOptions) (PruneReport, error)` method is added to the existing `runtime.Manager` interface and implemented on `dockerRuntime` by shelling out to the `docker` CLI (the established pattern — no SDK). Each resource category is listed with outcome-based filtering (label exclusion for containers, `tengiz-apps/*` repository prefix for images, `dangling=true` for volumes, `type=custom` + container-inspect for networks), then each candidate is removed individually; per-item "in use" errors from `docker rmi`/`docker network rm` are skipped (this is the safety net that protects images/networks still referenced by containers). Pure arg-builder/parsing helpers are isolated for unit testing. A new `internal/cli/cleanup.go` file wires the command, flags (`--dry-run` + per-category toggles), and a human-readable report.

**Tech Stack:** Go 1.26, Cobra, `os/exec` docker CLI (existing `runtime` pattern), existing `runtime.dockerRuntime` / `runtime.stubManager` / `cli.mockRTForDeploy`.

## Global Constraints

- Feature #6 (Docker Housekeeping): "Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`." — from `docs/FUTURES_FEATURES.md` P0 table.
- Feature rationale (from `docs/FUTURES_FEATURES.md`): "Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur."
- Container labels set by Tengiz: `tengiz-app=<appname>` and `tengiz-env=<env>` (`labelKey` / `envLabelKey` consts in `internal/runtime/docker.go`). Images built by Tengiz live under the `tengiz-apps/*` repository. **Never** prune anything carrying these labels or this repo prefix.
- Runtime always shells out to `docker` via `os/exec` — no Docker SDK import. Keep every command's args behind a pure helper so behavior is unit-testable.
- Add the new method to `runtime.Manager` **and** all three implementers in the same commit: `dockerRuntime`, `stubManager`, and the `mockRTForDeploy` test mock in `internal/cli/root_test.go` (otherwise `internal/cli` tests fail to compile).
- No new external dependencies.
- Go formatting via `gofmt`; verify with `go build ./...`, `go vet ./...`, `go test ./... -count=1`.
- New feature work happens on a branch: `git checkout -b feat/docker-housekeeping`.
- Every task ends with passed tests and its own commit.
- CLI surface changes require updating `README.md`; the CLI reference in `AGENTS.md` is updated too; `docs/FUTURES_FEATURES.md` feature #6 is marked ✅ when the full suite passes.
- Out of scope: scheduled/periodic cleanup (that is #57 Background Monitoring Scheduler), granular per-category prune (#56), build-cache/git GC (#103). This plan ships the manual `tengiz cleanup` command only.

---

## File Structure

| File | Responsibility |
|------|---------------|
| Create: `internal/runtime/housekeeping.go` | `PruneOptions`, `PruneReport`, kind constants, pure docker-arg builders, output parsers (`parseLines`, `prunableImageIDs`, `networkIsUnused`), `dockerOutput` exec wrapper, `dockerRuntime.Prune` + per-kind prune methods |
| Create: `internal/runtime/housekeeping_test.go` | Unit tests for all pure helpers + stub `Prune` behavior |
| Modify: `internal/runtime/runtime.go` | Add `Prune` to the `Manager` interface; add `stubManager.Prune` |
| Modify: `internal/cli/root_test.go` | Add `Prune` to `mockRTForDeploy` (required to keep compiling) |
| Create: `internal/cli/cleanup.go` | `cleanupCmd` + `init()` registering flags and `rootCmd.AddCommand` (mirrors `internal/cli/preview.go:83-88` pattern); `printPruneReport` |
| Create: `internal/cli/cleanup_test.go` | Registration + flag presence tests (no docker execution) |
| Modify: `README.md` | New `### tengiz cleanup` command section after the rollback section |
| Modify: `AGENTS.md` | Add `tengiz cleanup` line to the CLI reference block |
| Modify: `docs/FUTURES_FEATURES.md` | Mark P0 feature #6 as ✅ implemented |

---

### Task 1: Prune types + pure docker-arg builders + output parsers

**Files:**
- Create: `internal/runtime/housekeeping.go`
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `PruneKindContainers/PruneKindImages/PruneKindVolumes/PruneKindNetworks` (untyped string consts), `type PruneOptions struct { DryRun bool; Kinds []string }`, `type PruneReport struct { DryRun bool; Containers []string; Images []string; Volumes []string; Networks []string }`, and unexported helpers: `pruneExitedContainersArgs() []string`, `pruneRemoveContainerArgs(id string) []string`, `pruneImagesArgs() []string`, `pruneRemoveImageArgs(id string) []string`, `pruneVolumesArgs() []string`, `pruneRemoveVolumeArgs(name string) []string`, `pruneNetworksArgs() []string`, `pruneNetworkUsageArgs(id string) []string`, `pruneRemoveNetworkArgs(id string) []string`, `parseLines(out []byte) []string`, `prunableImageIDs(out []byte) []string`, `networkIsUnused(out []byte) bool`. Later tasks depend on these exact names.

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/docker-housekeeping
```

Expected: switched to `feat/docker-housekeeping`.

- [ ] **Step 2: Write the failing tests**

Create `internal/runtime/housekeeping_test.go`:

```go
package runtime

import (
	"reflect"
	"testing"
)

func TestParseLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty output", "", nil},
		{"single line", "abc123\n", []string{"abc123"}},
		{"no trailing newline", "a\nb", []string{"a", "b"}},
		{"blank line skipped", "a\n\nb\n", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLines([]byte(tt.in))
			if tt.want == nil && got != nil {
				t.Fatalf("parseLines(%q) = %v, want nil", tt.in, got)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseLines(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestPrunableImageIDs(t *testing.T) {
	in := "tengiz-apps/myapp:123|sha-tengiz-1\n" +
		"tengiz-apps/other:456|sha-tengiz-2\n" +
		"postgres:16|sha-postgres\n" +
		"redis:7|<sha-redis>\n" +
		"<none>|<sha-dangling>\n"
	got := prunableImageIDs([]byte(in))
	want := []string{"sha-postgres", "<sha-redis>", "<sha-dangling>"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("prunableImageIDs() = %v, want %v", got, want)
	}
}

func TestNetworkIsUnused(t *testing.T) {
	if !networkIsUnused([]byte("")) {
		t.Error("empty output should be unused")
	}
	if !networkIsUnused([]byte("null\n")) {
		t.Error("JSON null should be unused")
	}
	if !networkIsUnused([]byte("\n")) {
		t.Error("whitespace-only should be unused")
	}
	if networkIsUnused([]byte("{\"123abc\":{\"Name\":\"foo\"}}\n")) {
		t.Error("non-empty containers map should be in use")
	}
}

func TestPruneExitedContainersArgs(t *testing.T) {
	want := []string{
		"ps", "-a", "-q",
		"--filter", "status=exited",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
	if !reflect.DeepEqual(pruneExitedContainersArgs(), want) {
		t.Errorf("pruneExitedContainersArgs() = %v, want %v", pruneExitedContainersArgs(), want)
	}
}

func TestPruneRemoveContainerArgs(t *testing.T) {
	want := []string{"rm", "-f", "abc123"}
	if !reflect.DeepEqual(pruneRemoveContainerArgs("abc123"), want) {
		t.Errorf("pruneRemoveContainerArgs() = %v, want %v", pruneRemoveContainerArgs("abc123"), want)
	}
}

func TestPruneImagesArgs(t *testing.T) {
	want := []string{"images", "--format", "{{.Repository}}|{{.ID}}"}
	if !reflect.DeepEqual(pruneImagesArgs(), want) {
		t.Errorf("pruneImagesArgs() = %v, want %v", pruneImagesArgs(), want)
	}
}

func TestPruneRemoveImageArgs(t *testing.T) {
	want := []string{"rmi", "sha-postgres"}
	if !reflect.DeepEqual(pruneRemoveImageArgs("sha-postgres"), want) {
		t.Errorf("pruneRemoveImageArgs() = %v, want %v", pruneRemoveImageArgs("sha-postgres"), want)
	}
}

func TestPruneVolumesArgs(t *testing.T) {
	want := []string{"volume", "ls", "-q", "--filter", "dangling=true"}
	if !reflect.DeepEqual(pruneVolumesArgs(), want) {
		t.Errorf("pruneVolumesArgs() = %v, want %v", pruneVolumesArgs(), want)
	}
}

func TestPruneRemoveVolumeArgs(t *testing.T) {
	want := []string{"volume", "rm", "pgdata"}
	if !reflect.DeepEqual(pruneRemoveVolumeArgs("pgdata"), want) {
		t.Errorf("pruneRemoveVolumeArgs() = %v, want %v", pruneRemoveVolumeArgs("pgdata"), want)
	}
}

func TestPruneNetworksArgs(t *testing.T) {
	want := []string{"network", "ls", "-q", "--filter", "type=custom"}
	if !reflect.DeepEqual(pruneNetworksArgs(), want) {
		t.Errorf("pruneNetworksArgs() = %v, want %v", pruneNetworksArgs(), want)
	}
}

func TestPruneNetworkUsageArgs(t *testing.T) {
	want := []string{"network", "inspect", "--format", "{{json .Containers}}", "net123"}
	if !reflect.DeepEqual(pruneNetworkUsageArgs("net123"), want) {
		t.Errorf("pruneNetworkUsageArgs() = %v, want %v", pruneNetworkUsageArgs("net123"), want)
	}
}

func TestPruneRemoveNetworkArgs(t *testing.T) {
	want := []string{"network", "rm", "net123"}
	if !reflect.DeepEqual(pruneRemoveNetworkArgs("net123"), want) {
		t.Errorf("pruneRemoveNetworkArgs() = %v, want %v", pruneRemoveNetworkArgs("net123"), want)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/ -run "TestParseLines|TestPrunableImageIDs|TestNetworkIsUnused|TestPrune" -v -count=1`

Expected: FAIL — `undefined: parseLines`, `undefined: prunableImageIDs`, `undefined: networkIsUnused`, `undefined: pruneExitedContainersArgs`, etc.

- [ ] **Step 4: Write the minimal implementation**

Create `internal/runtime/housekeeping.go`:

```go
package runtime

import (
	"strings"
)

const (
	// PruneKindContainers prunes exited non-Tengiz containers.
	PruneKindContainers = "containers"
	// PruneKindImages prunes unused non-Tengiz images.
	PruneKindImages = "images"
	// PruneKindVolumes prunes volumes not referenced by any container.
	PruneKindVolumes = "volumes"
	// PruneKindNetworks prunes custom networks with no attached containers.
	PruneKindNetworks = "networks"
)

const tengizImageRepoPrefix = "tengiz-apps/"

// PruneOptions selects which resource categories to prune. An empty Kinds
// slice means "all categories".
type PruneOptions struct {
	DryRun bool
	Kinds  []string
}

// PruneReport lists the resources that were removed (or would be removed with
// DryRun=true).
type PruneReport struct {
	DryRun     bool
	Containers []string
	Images     []string
	Volumes    []string
	Networks   []string
}

// pruneExitedContainersArgs lists exited containers that are NOT managed by
// Tengiz (no tengiz-app / tengiz-env label). Stopped Tengiz containers must
// survive cleanup because scale-to-zero restarts them on demand.
func pruneExitedContainersArgs() []string {
	return []string{
		"ps", "-a", "-q",
		"--filter", "status=exited",
		"--filter", "label!=" + labelAppKey,
		"--filter", "label!=" + labelEnvKey,
	}
}

func pruneRemoveContainerArgs(id string) []string {
	return []string{"rm", "-f", id}
}

// pruneImagesArgs lists every local image as Repository|ID. Tengiz images are
// filtered out later in prunableImageIDs.
func pruneImagesArgs() []string {
	return []string{"images", "--format", "{{.Repository}}|{{.ID}}"}
}

// pruneRemoveImageArgs uses a non-forced rmi: docker refuses images still
// referenced by a container, which is our safety net for in-use images.
func pruneRemoveImageArgs(id string) []string {
	return []string{"rmi", id}
}

func pruneVolumesArgs() []string {
	return []string{"volume", "ls", "-q", "--filter", "dangling=true"}
}

func pruneRemoveVolumeArgs(name string) []string {
	return []string{"volume", "rm", name}
}

func pruneNetworksArgs() []string {
	return []string{"network", "ls", "-q", "--filter", "type=custom"}
}

func pruneNetworkUsageArgs(id string) []string {
	return []string{"network", "inspect", "--format", "{{json .Containers}}", id}
}

func pruneRemoveNetworkArgs(id string) []string {
	return []string{"network", "rm", id}
}

// parseLines splits docker -q style output into non-empty lines.
func parseLines(out []byte) []string {
	var items []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			items = append(items, line)
		}
	}
	return items
}

// prunableImageIDs filters `docker images --format "{{.Repository}}|{{.ID}}"` output,
// dropping every image stored under the Tengiz repository prefix. Images are
// identified by ID so a single rmi call removes all of an image's tags.
func prunableImageIDs(out []byte) []string {
	var ids []string
	for _, line := range parseLines(out) {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.HasPrefix(parts[0], tengizImageRepoPrefix) {
			continue
		}
		ids = append(ids, parts[1])
	}
	return ids
}

// networkIsUnused reports whether a network inspect of `.Containers` shows an
// empty or null map (no attached containers).
func networkIsUnused(out []byte) bool {
	s := strings.TrimSpace(string(out))
	return s == "" || s == "null"
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/ -run "TestParseLines|TestPrunableImageIDs|TestNetworkIsUnused|TestPrune" -v -count=1`

Expected: PASS (all 10 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go
git commit -m "feat: add docker prune arg builders and parsing helpers"
```

---

### Task 2: Wire `Prune` into `runtime.Manager` and implement `dockerRuntime.Prune`

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add `Prune` to the `Manager` interface; add `stubManager.Prune` near line 121
- Modify: `internal/runtime/housekeeping.go` — add `dockerOutput` + `dockerRuntime.Prune` + per-kind methods
- Modify: `internal/cli/root_test.go:69-100` — add `Prune` to `mockRTForDeploy` (required to keep `internal/cli` compiling)
- Test: `internal/runtime/housekeeping_test.go`

**Interfaces:**
- Consumes: `PruneOptions` / `PruneReport` / kind consts and all arg-builder helpers from Task 1
- Produces: `runtime.Manager.Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)`. All three implementers satisfy the extended interface.

- [ ] **Step 1: Write the failing test**

Append to `internal/runtime/housekeeping_test.go` (also add `"context"` to that file's import block — the Task 1 tests do not use it yet):

```go
func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !report.DryRun {
		t.Error("DryRun = false, want true")
	}
	total := len(report.Containers) + len(report.Images) + len(report.Volumes) + len(report.Networks)
	if total != 0 {
		t.Error("stub Prune should report no removed items")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run "TestStubPrune" -v -count=1`

Expected: FAIL — `undefined: (stubManager).Prune` (the `Manager` interface also has no `Prune` yet).

- [ ] **Step 3: Add `Prune` to the `Manager` interface**

In `internal/runtime/runtime.go`, add this method to the `Manager` interface (after `KeepLastNImages`):

```go
	Prune(ctx context.Context, opts PruneOptions) (PruneReport, error)
```

- [ ] **Step 4: Implement `stubManager.Prune`**

In `internal/runtime/runtime.go`, add after `KeepLastNImages`:

```go
func (m *stubManager) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	return PruneReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 5: Add `Prune` to `mockRTForDeploy` in `internal/cli/root_test.go`**

In `internal/cli/root_test.go`, replace the `KeepLastNImages` method (line 99) with the same method plus a new `Prune` method, so the mock continues to satisfy `runtime.Manager`:

```go
func (m *mockRTForDeploy) KeepLastNImages(ctx context.Context, appName string, n int) error { return nil }
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneReport, error) {
	return runtime.PruneReport{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 6: Implement `dockerRuntime.Prune`**

Append the following to `internal/runtime/housekeeping.go` (and add these imports to the file's import block: `context`, `fmt`, `log`, `os/exec`):

```go
// dockerOutput runs a docker subcommand and returns its combined output.
func dockerOutput(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
}

// Prune removes unused non-Tengiz Docker resources. Tengiz-managed containers
// (labels tengiz-app / tengiz-env) and images (tengiz-apps/* repository) are
// always protected. Per-item removal failures are logged and skipped, so one
// busy resource never aborts the whole cleanup.
func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneReport, error) {
	report := PruneReport{DryRun: opts.DryRun}
	kinds := opts.Kinds
	if len(kinds) == 0 {
		kinds = []string{PruneKindContainers, PruneKindImages, PruneKindVolumes, PruneKindNetworks}
	}
	for _, kind := range kinds {
		var err error
		switch kind {
		case PruneKindContainers:
			err = r.pruneContainers(ctx, opts.DryRun, &report)
		case PruneKindImages:
			err = r.pruneImages(ctx, opts.DryRun, &report)
		case PruneKindVolumes:
			err = r.pruneVolumes(ctx, opts.DryRun, &report)
		case PruneKindNetworks:
			err = r.pruneNetworks(ctx, opts.DryRun, &report)
		default:
			log.Printf("[runtime] cleanup: unknown kind %q, skipping", kind)
		}
		if err != nil {
			return report, err
		}
	}
	return report, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool, report *PruneReport) error {
	out, err := dockerOutput(ctx, pruneExitedContainersArgs()...)
	if err != nil {
		return fmt.Errorf("docker ps: %w", err)
	}
	for _, id := range parseLines(out) {
		if !dryRun {
			if _, err := dockerOutput(ctx, pruneRemoveContainerArgs(id)...); err != nil {
				log.Printf("[runtime] cleanup: skip container %s: %v", id, err)
				continue
			}
		}
		report.Containers = append(report.Containers, id)
	}
	return nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, dryRun bool, report *PruneReport) error {
	out, err := dockerOutput(ctx, pruneImagesArgs()...)
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}
	for _, id := range prunableImageIDs(out) {
		if !dryRun {
			// Non-forced rmi refuses in-use images; skip them for next time.
			if _, err := dockerOutput(ctx, pruneRemoveImageArgs(id)...); err != nil {
				log.Printf("[runtime] cleanup: skip image %s: %v", id, err)
				continue
			}
		}
		report.Images = append(report.Images, id)
	}
	return nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool, report *PruneReport) error {
	out, err := dockerOutput(ctx, pruneVolumesArgs()...)
	if err != nil {
		return fmt.Errorf("docker volume ls: %w", err)
	}
	for _, name := range parseLines(out) {
		if !dryRun {
			if _, err := dockerOutput(ctx, pruneRemoveVolumeArgs(name)...); err != nil {
				log.Printf("[runtime] cleanup: skip volume %s: %v", name, err)
				continue
			}
		}
		report.Volumes = append(report.Volumes, name)
	}
	return nil
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool, report *PruneReport) error {
	out, err := dockerOutput(ctx, pruneNetworksArgs()...)
	if err != nil {
		return fmt.Errorf("docker network ls: %w", err)
	}
	for _, id := range parseLines(out) {
		usage, err := dockerOutput(ctx, pruneNetworkUsageArgs(id)...)
		if err != nil || !networkIsUnused(usage) {
			continue
		}
		if !dryRun {
			if _, err := dockerOutput(ctx, pruneRemoveNetworkArgs(id)...); err != nil {
				log.Printf("[runtime] cleanup: skip network %s: %v", id, err)
				continue
			}
		}
		report.Networks = append(report.Networks, id)
	}
	return nil
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/runtime/ ./internal/cli/ -run "TestStubPrune|TestMockRTForDeployImplementsManager|TestStubSatisfiesInterface" -v -count=1`

Expected: PASS — stub satisfies the extended interface, and `mockRTForDeploy` continues to satisfy it too.

- [ ] **Step 8: Build the whole module**

Run: `go build ./...`

Expected: exits 0, no output.

- [ ] **Step 9: Manual smoke test (requires a real docker daemon — skip if unavailable)**

Run: `docker ps -a` to confirm docker works, then `go run . cleanup --dry-run`

Expected (no Tengiz apps running): prints `[tengiz] nothing to clean up` if the host is clean, or one `[tengiz] would be removed <kind> <id>` line per candidate, and never lists any `tengiz-app` / `tengiz-apps/` resource.

- [ ] **Step 10: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/housekeeping.go internal/runtime/housekeeping_test.go internal/cli/root_test.go
git commit -m "feat: add Prune to runtime.Manager with docker-based housekeeping"
```

---

### Task 3: `tengiz cleanup` CLI command + documentation

**Files:**
- Create: `internal/cli/cleanup.go` — command, flags, `init()` registration, report printer
- Test: `internal/cli/cleanup_test.go`
- Modify: `README.md` — add `### tengiz cleanup` section after the rollback section (after line ~236, before `### tengiz domain`)
- Modify: `AGENTS.md` — add the command to the CLI reference block (after the `tengiz rollback` line)

**Interfaces:**
- Consumes: `runtime.NewDocker()`, `runtime.PruneOptions`, `runtime.PruneReport`, `runtime.PruneKindContainers/PruneKindImages/PruneKindVolumes/PruneKindNetworks` from Tasks 1-2
- Produces: `tengiz cleanup [--dry-run] [--containers] [--images] [--volumes] [--networks]`; commands `cleanupCmd` is registered on `rootCmd` via this file's `init()` (mirrors `internal/cli/preview.go:83-88`)

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

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
	for _, flag := range []string{"dry-run", "containers", "images", "volumes", "networks"} {
		t.Run(flag, func(t *testing.T) {
			if f := cleanupCmd.Flags().Lookup(flag); f == nil {
				t.Errorf("cleanupCmd missing --%s flag", flag)
			}
		})
	}
}

func TestPruneReportPrintEmpty(t *testing.T) {
	out := captureOutput(func() {
		printPruneReport(runtime.PruneReport{})
	})
	if out != "[tengiz] nothing to clean up\n" {
		t.Errorf("empty report output = %q, want %q", out, "[tengiz] nothing to clean up\n")
	}
}

func TestPruneReportPrintDryRunItems(t *testing.T) {
	out := captureOutput(func() {
		printPruneReport(runtime.PruneReport{
			DryRun:     true,
			Containers: []string{"c1", "c2"},
			Images:     []string{"sha1"},
		})
	})
	if !strings.Contains(out, "[tengiz] would be removed container c1\n") {
		t.Errorf("missing c1 line, got: %q", out)
	}
	if !strings.Contains(out, "[tengiz] would be removed container c2\n") {
		t.Errorf("missing c2 line, got: %q", out)
	}
	if !strings.Contains(out, "[tengiz] would be removed image sha1\n") {
		t.Errorf("missing sha1 line, got: %q", out)
	}
	if !strings.Contains(out, "[tengiz] total: 3 ") {
		t.Errorf("missing total line, got: %q", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run "TestCleanupCmd|TestPruneReportPrint" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd` and `undefined: printPruneReport`.

- [ ] **Step 3: Write the command implementation**

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
	Short: "Prune unused Docker resources to free disk space",
	Long: `Removes Docker resources not managed by Tengiz: stopped non-Tengiz
containers, unused images (except tengiz-apps builds), unused volumes, and
unused custom networks.

Tengiz-managed containers and images (labelled tengiz-app / tengiz-env, or
stored under the tengiz-apps/ repository) are always protected.

Use --dry-run to preview what would be removed. Without any category flag,
prunes all categories.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")

		var kinds []string
		if containers || images || volumes || networks {
			if containers {
				kinds = append(kinds, runtime.PruneKindContainers)
			}
			if images {
				kinds = append(kinds, runtime.PruneKindImages)
			}
			if volumes {
				kinds = append(kinds, runtime.PruneKindVolumes)
			}
			if networks {
				kinds = append(kinds, runtime.PruneKindNetworks)
			}
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		report, err := rt.Prune(cmd.Context(), runtime.PruneOptions{DryRun: dryRun, Kinds: kinds})
		if err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}

		printPruneReport(report)
		return nil
	},
}

func init() {
	cleanupCmd.Flags().Bool("dry-run", false, "show what would be removed without removing anything")
	cleanupCmd.Flags().Bool("containers", false, "prune stopped non-Tengiz containers")
	cleanupCmd.Flags().Bool("images", false, "prune unused non-Tengiz images")
	cleanupCmd.Flags().Bool("volumes", false, "prune unused volumes")
	cleanupCmd.Flags().Bool("networks", false, "prune unused custom networks")
	rootCmd.AddCommand(cleanupCmd)
}

func printPruneReport(r runtime.PruneReport) {
	n := len(r.Containers) + len(r.Images) + len(r.Volumes) + len(r.Networks)
	if n == 0 {
		fmt.Println("[tengiz] nothing to clean up")
		return
	}
	verb := "removed"
	if r.DryRun {
		verb = "would be removed"
	}
	for _, id := range r.Containers {
		fmt.Printf("[tengiz] %s container %s\n", verb, id)
	}
	for _, id := range r.Images {
		fmt.Printf("[tengiz] %s image %s\n", verb, id)
	}
	for _, name := range r.Volumes {
		fmt.Printf("[tengiz] %s volume %s\n", verb, name)
	}
	for _, id := range r.Networks {
		fmt.Printf("[tengiz] %s network %s\n", verb, id)
	}
	fmt.Printf("[tengiz] total: %d (%d containers, %d images, %d volumes, %d networks)\n",
		n, len(r.Containers), len(r.Images), len(r.Volumes), len(r.Networks))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run "TestCleanupCmd|TestPruneReportPrint" -v -count=1`

Expected: PASS (4 tests).

- [ ] **Step 5: Verify help output**

Run: `go run . cleanup --help`

Expected: shows `Prune unused Docker resources to free disk space` and lists `--dry-run`, `--containers`, `--images`, `--volumes`, `--networks`.

- [ ] **Step 6: Add the command to `README.md`**

After the `### tengiz rollback <app>` section (ends at line ~236), insert:

```markdown
### `tengiz cleanup`

Prune unused Docker resources to free disk space. Tengiz-managed containers and images are always protected — stopped Tengiz containers (kept for scale-to-zero cold starts) and images under the `tengiz-apps/` repository are never removed.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without removing anything |
| `--containers` | Prune stopped non-Tengiz containers |
| `--images` | Prune unused non-Tengiz images |
| `--volumes` | Prune unused volumes |
| `--networks` | Prune unused custom networks |

Without a category flag, prunes all categories. Examples:
```
tengiz cleanup --dry-run
tengiz cleanup --containers --images
tengiz cleanup
```
```

- [ ] **Step 7: Add the command to `AGENTS.md`**

In the `## Commands` CLI reference block, add after the `tengiz rollback <app>` line:

```text
tengiz cleanup [--dry-run]  → prune unused non-Tengiz Docker resources (containers/images/volumes/networks), Tengiz-managed resources always protected
```

- [ ] **Step 8: Commit**

```bash
git add internal/cli/cleanup.go internal/cli/cleanup_test.go README.md AGENTS.md
git commit -m "feat: add tengiz cleanup command for Docker housekeeping"
```

---

### Task 4: Feature flag update + full verification

**Files:**
- Modify: `docs/FUTURES_FEATURES.md` — mark P0 feature #6 as ✅ implemented

**Interfaces:**
- Consumes: everything from Tasks 1-3
- Produces: a verified, documented, committed feature

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -count=1`

Expected: All PASS. Note: `internal/proxy` tests are slow (~2s each, TCP dial timeouts) and `internal/idle` tests are time-sensitive — wait for them; timeouts/flakiness here are pre-existing and unrelated to this feature.

- [ ] **Step 2: Run static analysis**

Run: `go vet ./...`

Expected: no issues reported.

- [ ] **Step 3: Verify gofmt cleanliness**

Run: `gofmt -l internal/runtime internal/cli`

Expected: empty output (no files listed).

- [ ] **Step 4: Mark feature #6 implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table, replace the #6 row with the same row marked ✅:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

- [ ] **Step 5: Self-review against the spec**

Check the feature requirements from `docs/FUTURES_FEATURES.md`:
- `tengiz cleanup` komutu ✅ (Task 3 — `cleanupCmd`)
- Label-based filtreleme ile Tengiz yönetimindeki container'lar korunur ✅ (Tasks 1-2 — `label!=tengiz-app`, `label!=tengiz-env`, `tengiz-apps/*` repo prefix)
- Kullanılmayan container/image/volume/network temizleme ✅ (Task 2 — all four `PruneKind*` categories)
- No new dependencies ✅, no breaking changes to stored state ✅ (cleanup touches Docker only; `apps.json`/`ports.json` untouched)

- [ ] **Step 6: Commit**

```bash
git add docs/FUTURES_FEATURES.md
git commit -m "docs: mark Docker Housekeeping as implemented"
```