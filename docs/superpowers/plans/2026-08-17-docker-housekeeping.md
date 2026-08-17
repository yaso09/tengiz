# Docker Housekeeping (`tengiz cleanup`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tengiz cleanup` command that reclaims disk space by pruning unused Docker containers, images, volumes, networks, and build cache — while label-based filters guarantee Tengiz-managed containers and images are never removed.

**Architecture:** A new `Cleanup(ctx, opts CleanupOptions) (CleanupResult, error)` method on the `runtime.Manager` interface. The `dockerRuntime` implementation shells out to granular `docker container prune`, `docker image prune`, `docker volume prune`, `docker network prune`, and `docker builder prune` commands, each guarded by `--filter label!=tengiz-app` so Tengiz-managed resources are protected. Pure helper functions build the exact docker argv (testable without Docker, mirroring `buildLogArgs`/`buildRunArgs`). `--dry-run` runs `docker system df` to preview reclaimable space without removing anything. Built images gain `tengiz-app`/`tengiz-env` labels at build time so image pruning also protects rollback images.

**Tech Stack:** Go 1.26, `os/exec` (no Docker SDK), Cobra, existing `runtime.Manager` interface, `regexp`/`strings` for prune-output parsing.

## Global Constraints

- Every prune command that could remove Tengiz resources MUST include `--filter label!=tengiz-app` (containers, images, volumes, networks)
- Build cache uses `docker builder prune -af` (no label filter — build cache carries no labels and is always safe to remove)
- New interface method signature: `Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)`
- With no category flags selected, all five categories are enabled by default
- `--dry-run` MUST NOT execute any prune command — it runs `docker system df` only
- `--force` skips the confirmation prompt; without it, the command prompts `Continue? [y/N]` reading from `cmd.InOrStdin()`
- No new external dependencies
- All `runtime.Manager` implementations must gain `Cleanup`: `stubManager`, `mockRTForDeploy` (cli/root_test.go), `mockRuntime` (idle/idle_test.go), `mockRuntime` (proxy/proxy_test.go)
- Built images must carry labels `tengiz-app=<appName>` and `tengiz-env=<env>` (both docker and nixpacks build paths)
- Existing tests must continue to pass unchanged
- New feature work happens on branch `feat/cleanup` (AGENTS.md rule)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/runtime/cleanup.go` | `CleanupOptions`, `CleanupResult`, `resolveCleanupOptions`, `buildContainerPruneArgs`/`buildImagePruneArgs`/`buildVolumePruneArgs`/`buildNetworkPruneArgs`/`buildCachePruneArgs`, `parsePruneCount`, `dockerRuntime.Cleanup` |
| `internal/runtime/runtime.go` | Add `Cleanup` to `Manager` interface + `stubManager.Cleanup` |
| `internal/runtime/cleanup_test.go` | Tests for arg builders, `resolveCleanupOptions`, `parsePruneCount`, stub `Cleanup`, docker smoke test |
| `internal/builder/builder.go` | `buildImageLabels(appName, env)`, wire into docker + nixpacks build args |
| `internal/builder/builder_test.go` | Tests for `buildImageLabels` |
| `internal/cli/root.go` | `cleanupCmd`, `confirmCleanup`, flag registration in `init()` |
| `internal/cli/cleanup_test.go` | Tests for `cleanupCmd` registration, flags, `confirmCleanup` |
| `internal/cli/root_test.go` | Add `Cleanup` to `mockRTForDeploy` |
| `internal/idle/idle_test.go` | Add `Cleanup` to `mockRuntime` |
| `internal/proxy/proxy_test.go` | Add `Cleanup` to `mockRuntime` |
| `README.md` | Document `tengiz cleanup` |
| `docs/FUTURES_FEATURES.md` | Mark feature #6 Docker Housekeeping as implemented |

---

### Task 1: Runtime cleanup types, arg builders, options resolution, prune parser

**Files:**
- Modify: `internal/runtime/cleanup.go` (append — currently holds `RemoveImage` and `KeepLastNImages`)
- Test: `internal/runtime/cleanup_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces:
  - `type CleanupOptions struct { Containers, Images, Volumes, Networks, Cache, DryRun bool }`
  - `type CleanupResult struct { Containers, Images, Volumes, Networks, Cache int; Output []string; DryRun bool }`
  - `func resolveCleanupOptions(opts CleanupOptions) CleanupOptions`
  - `func buildContainerPruneArgs() []string`
  - `func buildImagePruneArgs() []string`
  - `func buildVolumePruneArgs() []string`
  - `func buildNetworkPruneArgs() []string`
  - `func buildCachePruneArgs() []string`
  - `func parsePruneCount(output string) int`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/cleanup
```

Expected: now on `feat/cleanup`.

- [ ] **Step 2: Write the failing tests**

Append to `internal/runtime/cleanup_test.go`:

```go
package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestBuildContainerPruneArgs(t *testing.T) {
	got := buildContainerPruneArgs()
	want := []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildImagePruneArgs(t *testing.T) {
	got := buildImagePruneArgs()
	want := []string{"image", "prune", "-af", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildVolumePruneArgs(t *testing.T) {
	got := buildVolumePruneArgs()
	want := []string{"volume", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildNetworkPruneArgs(t *testing.T) {
	got := buildNetworkPruneArgs()
	want := []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildCachePruneArgs(t *testing.T) {
	got := buildCachePruneArgs()
	want := []string{"builder", "prune", "-af"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveCleanupOptionsDefaultAll(t *testing.T) {
	opts := resolveCleanupOptions(CleanupOptions{})
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.Cache {
		t.Errorf("expected all categories enabled by default, got %+v", opts)
	}
}

func TestResolveCleanupOptionsPreservesSelection(t *testing.T) {
	opts := resolveCleanupOptions(CleanupOptions{Images: true, DryRun: true})
	if !opts.Images || opts.Containers || opts.Volumes || opts.Networks || opts.Cache {
		t.Errorf("expected only Images enabled, got %+v", opts)
	}
	if !opts.DryRun {
		t.Error("DryRun should be preserved")
	}
}

func TestParsePruneCount(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
	}{
		{
			name:   "containers",
			output: "Deleted Containers:\nabc123def456\n7890abcdef12\n\nTotal reclaimed space: 12B\n",
			want:   2,
		},
		{
			name:   "images counts untagged only",
			output: "Deleted Images:\nuntagged: foo:latest\nuntagged: bar:latest\ndeleted: sha256:abc123\ndeleted: sha256:def456\nTotal reclaimed space: 50MB\n",
			want:   2,
		},
		{
			name:   "empty output",
			output: "",
			want:   0,
		},
		{
			name:   "no deletions",
			output: "Total reclaimed space: 0B\n",
			want:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePruneCount(tt.output); got != tt.want {
				t.Errorf("parsePruneCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !res.DryRun {
		t.Errorf("DryRun = %v, want true", res.DryRun)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/runtime/... -run "TestBuildContainerPruneArgs|TestBuildImagePruneArgs|TestBuildVolumePruneArgs|TestBuildNetworkPruneArgs|TestBuildCachePruneArgs|TestResolveCleanupOptions|TestParsePruneCount|TestStubCleanup" -v -count=1`

Expected: FAIL — `undefined: buildContainerPruneArgs`, `undefined: resolveCleanupOptions`, `undefined: parsePruneCount`, and `m.Cleanup undefined` for the stub test.

- [ ] **Step 4: Implement the runtime types and helpers in `internal/runtime/cleanup.go`**

Append to `internal/runtime/cleanup.go` (add `regexp` to its imports):

```go
const protectLabelFilter = "label!=tengiz-app"

type CleanupOptions struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	Cache      bool
	DryRun     bool
}

type CleanupResult struct {
	Containers int
	Images     int
	Volumes    int
	Networks   int
	Cache      int
	Output     []string
	DryRun     bool
}

func resolveCleanupOptions(opts CleanupOptions) CleanupOptions {
	if !opts.Containers && !opts.Images && !opts.Volumes && !opts.Networks && !opts.Cache {
		opts.Containers = true
		opts.Images = true
		opts.Volumes = true
		opts.Networks = true
		opts.Cache = true
	}
	return opts
}

func buildContainerPruneArgs() []string {
	return []string{"container", "prune", "-f", "--filter", protectLabelFilter}
}

func buildImagePruneArgs() []string {
	return []string{"image", "prune", "-af", "--filter", protectLabelFilter}
}

func buildVolumePruneArgs() []string {
	return []string{"volume", "prune", "-f", "--filter", protectLabelFilter}
}

func buildNetworkPruneArgs() []string {
	return []string{"network", "prune", "-f", "--filter", protectLabelFilter}
}

func buildCachePruneArgs() []string {
	return []string{"builder", "prune", "-af"}
}

var pruneLineRe = regexp.MustCompile(`^(untagged: |[0-9a-f]{12,64}$)`)

func parsePruneCount(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if pruneLineRe.MatchString(strings.TrimSpace(line)) {
			count++
		}
	}
	return count
}
```

The updated import block of `internal/runtime/cleanup.go` must become:

```go
import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run "TestBuildContainerPruneArgs|TestBuildImagePruneArgs|TestBuildVolumePruneArgs|TestBuildNetworkPruneArgs|TestBuildCachePruneArgs|TestResolveCleanupOptions|TestParsePruneCount" -v -count=1`

Expected: PASS for all arg-builder / resolver / parser tests. `TestStubCleanup` still fails (`m.Cleanup undefined`) — it is fixed in Task 2.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: add runtime cleanup types, arg builders, and prune parser"
```

---

### Task 2: Add `Cleanup` to the `Manager` interface and all implementations

**Files:**
- Modify: `internal/runtime/runtime.go:31-49` — add `Cleanup` to `Manager` interface
- Modify: `internal/runtime/runtime.go:113-122` — add `stubManager.Cleanup`
- Modify: `internal/cli/root_test.go:76-100` — add `Cleanup` to `mockRTForDeploy`
- Modify: `internal/idle/idle_test.go:14-34` — add `Cleanup` to `mockRuntime`
- Modify: `internal/proxy/proxy_test.go:15-35` — add `Cleanup` to `mockRuntime`
- Test: `internal/runtime/cleanup_test.go` (`TestStubCleanup` from Task 1)

**Interfaces:**
- Consumes: `CleanupOptions`, `CleanupResult` from Task 1
- Produces: `Manager.Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)` — all implementers now satisfy it

- [ ] **Step 1: Write the failing test (already written in Task 1)**

`TestStubCleanup` in `internal/runtime/cleanup_test.go` calls `NewStub().Cleanup(...)`.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/runtime/... -run TestStubCleanup -v -count=1`

Expected: FAIL — `m.Cleanup undefined (type Manager has no field or method Cleanup)`.

- [ ] **Step 3: Add `Cleanup` to the `Manager` interface in `internal/runtime/runtime.go`**

In the `Manager` interface, after `KeepLastNImages`:

```go
	RemoveImage(ctx context.Context, imageTag string) error
	KeepLastNImages(ctx context.Context, appName string, n int) error
	Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error)
```

- [ ] **Step 4: Add `stubManager.Cleanup` in `internal/runtime/runtime.go`**

After `stubManager.KeepLastNImages`:

```go
func (m *stubManager) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{DryRun: opts.DryRun}, nil
}
```

- [ ] **Step 5: Add `Cleanup` to `mockRTForDeploy` in `internal/cli/root_test.go`**

After the `KeepLastNImages` method of `mockRTForDeploy` (line 99):

```go
func (m *mockRTForDeploy) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 6: Add `Cleanup` to `mockRuntime` in `internal/idle/idle_test.go`**

After the `Run` method (line 34):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 7: Add `Cleanup` to `mockRuntime` in `internal/proxy/proxy_test.go`**

After the `Run` method (line 35):

```go
func (m *mockRuntime) Cleanup(ctx context.Context, opts runtime.CleanupOptions) (runtime.CleanupResult, error) { return runtime.CleanupResult{}, nil }
```

- [ ] **Step 8: Run all tests to verify they pass**

Run: `go test ./internal/runtime/... ./internal/cli/... ./internal/idle/... ./internal/proxy/... -v -count=1`

Expected: PASS (idle tests are time-sensitive and may need reruns; proxy tests are slow ~2s each).

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/runtime.go internal/cli/root_test.go internal/idle/idle_test.go internal/proxy/proxy_test.go
git commit -m "feat: add Cleanup to runtime.Manager interface and all mocks"
```

---

### Task 3: Implement `dockerRuntime.Cleanup`

**Files:**
- Modify: `internal/runtime/cleanup.go` — add `dockerRuntime.Cleanup`
- Test: `internal/runtime/cleanup_test.go` — add smoke test

**Interfaces:**
- Consumes: `resolveCleanupOptions`, `build*PruneArgs`, `parsePruneCount` from Task 1
- Produces: working `Cleanup` on `dockerRuntime` that prunes each enabled category and returns per-category counts plus raw docker output

- [ ] **Step 1: Write the failing smoke test**

Append to `internal/runtime/cleanup_test.go`:

```go
func TestDockerCleanupDryRunIntegration(t *testing.T) {
	r, err := NewDocker()
	if err != nil {
		t.Skipf("docker not available: %v", err)
	}
	res, err := r.Cleanup(context.Background(), CleanupOptions{DryRun: true})
	if err != nil {
		t.Skipf("docker system df failed (permissions): %v", err)
	}
	if !res.DryRun {
		t.Error("DryRun = false, want true")
	}
	if len(res.Output) != 1 {
		t.Errorf("expected 1 output block (system df), got %d", len(res.Output))
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/runtime/... -run TestDockerCleanupDryRunIntegration -v -count=1`

Expected: FAIL — `r.Cleanup undefined (type Manager has no field or method Cleanup)` (dockerRuntime has not implemented it yet), OR skip if the binary has no docker access. The compile error is the "fail".

- [ ] **Step 3: Implement `dockerRuntime.Cleanup` in `internal/runtime/cleanup.go`**

Append:

```go
func (r *dockerRuntime) Cleanup(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	opts = resolveCleanupOptions(opts)
	result := CleanupResult{DryRun: opts.DryRun}

	if opts.DryRun {
		out, err := exec.CommandContext(ctx, "docker", "system", "df").CombinedOutput()
		if err != nil {
			return result, fmt.Errorf("docker system df: %w\n%s", err, string(out))
		}
		result.Output = append(result.Output, string(out))
		return result, nil
	}

	prune := func(args []string) (string, error) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
		}
		return string(out), nil
	}

	if opts.Containers {
		out, err := prune(buildContainerPruneArgs())
		if err != nil {
			return result, err
		}
		result.Containers = parsePruneCount(out)
		result.Output = append(result.Output, out)
	}
	if opts.Images {
		out, err := prune(buildImagePruneArgs())
		if err != nil {
			return result, err
		}
		result.Images = parsePruneCount(out)
		result.Output = append(result.Output, out)
	}
	if opts.Volumes {
		out, err := prune(buildVolumePruneArgs())
		if err != nil {
			return result, err
		}
		result.Volumes = parsePruneCount(out)
		result.Output = append(result.Output, out)
	}
	if opts.Networks {
		out, err := prune(buildNetworkPruneArgs())
		if err != nil {
			return result, err
		}
		result.Networks = parsePruneCount(out)
		result.Output = append(result.Output, out)
	}
	if opts.Cache {
		out, err := prune(buildCachePruneArgs())
		if err != nil {
			return result, err
		}
		result.Cache = parsePruneCount(out)
		result.Output = append(result.Output, out)
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/... -run TestDockerCleanupDryRunIntegration -v -count=1`

Expected: PASS (if docker is available) or SKIP (with the skip reason printed).

- [ ] **Step 5: Run all runtime tests**

Run: `go test ./internal/runtime/... -v -count=1`

Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/cleanup.go internal/runtime/cleanup_test.go
git commit -m "feat: implement dockerRuntime.Cleanup with label-protected pruning"
```

---

### Task 4: Label built images so cleanup protects rollback images

**Files:**
- Modify: `internal/builder/builder.go` — add `buildImageLabels`, wire into `buildWithDockerfile` and `buildWithNixpacks`
- Test: `internal/builder/builder_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `func buildImageLabels(appName, env string) []string` returning `--label tengiz-app=<appName>` and `--label tengiz-env=<env>` docker argv entries; images built by Tengiz carry these labels so `docker image prune --filter label!=tengiz-app` preserves them for rollback

- [ ] **Step 1: Write the failing tests**

Append to `internal/builder/builder_test.go`:

```go
func TestBuildImageLabels(t *testing.T) {
	got := buildImageLabels("myapp", "staging")
	want := []string{"--label", "tengiz-app=myapp", "--label", "tengiz-env=staging"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildImageLabelsDefaultEnv(t *testing.T) {
	joined := strings.Join(buildImageLabels("myapp", ""), " ")
	if !strings.Contains(joined, "tengiz-app=myapp") {
		t.Errorf("missing tengiz-app label, got %q", joined)
	}
	if !strings.Contains(joined, "tengiz-env=production") {
		t.Errorf("expected tengiz-env=production for empty env, got %q", joined)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/builder/... -run "TestBuildImageLabels" -v -count=1`

Expected: FAIL — `undefined: buildImageLabels`.

- [ ] **Step 3: Implement `buildImageLabels` and wire it into the docker build path**

Add to `internal/builder/builder.go` (package level):

```go
func buildImageLabels(appName, env string) []string {
	if env == "" {
		env = "production"
	}
	return []string{
		"--label", fmt.Sprintf("tengiz-app=%s", appName),
		"--label", fmt.Sprintf("tengiz-env=%s", env),
	}
}
```

In `buildWithDockerfile`, replace:

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, "-t", tag, dir)
```

with:

```go
	args := []string{"build"}
	args = append(args, b.buildSecretArgs()...)
	args = append(args, buildImageLabels(appName, env)...)
	args = append(args, "-t", tag, dir)
```

- [ ] **Step 4: Wire labels into the nixpacks build path**

In `buildWithNixpacks`, replace:

```go
	args := []string{"build", dir, "--name", tag}
	if b.nixpacksCfg != nil {
```

with:

```go
	args := []string{"build", dir, "--name", tag}
	args = append(args, buildImageLabels(appName, env)...)
	if b.nixpacksCfg != nil {
```

(Confirmed: nixpacks CLI supports `--label <labels...>` / `-l`.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/builder/... -run "TestBuildImageLabels" -v -count=1`

Expected: PASS.

- [ ] **Step 6: Run all builder tests**

Run: `go test ./internal/builder/... -v -count=1`

Expected: All PASS (integration tests that need docker/nixpacks skip gracefully).

- [ ] **Step 7: Commit**

```bash
git add internal/builder/builder.go internal/builder/builder_test.go
git commit -m "feat: label built images with tengiz-app and tengiz-env"
```

---

### Task 5: Add the `tengiz cleanup` CLI command

**Files:**
- Modify: `internal/cli/root.go` — add `cleanupCmd`, `confirmCleanup`, register in `init()`
- Test: `internal/cli/cleanup_test.go` (new file)

**Interfaces:**
- Consumes: `runtime.CleanupOptions`, `runtime.CleanupResult`, `runtime.NewDocker()` from Tasks 1-3
- Produces: `tengiz cleanup [--dry-run] [--force] [--containers] [--images] [--volumes] [--networks] [--cache]` command; `func confirmCleanup(r io.Reader) bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/cleanup_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not registered: %v", err)
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCmdFlags(t *testing.T) {
	for _, flag := range []string{"dry-run", "force", "containers", "images", "volumes", "networks", "cache"} {
		if cleanupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestConfirmCleanupYes(t *testing.T) {
	if !confirmCleanup(strings.NewReader("y\n")) {
		t.Error("confirmCleanup('y') = false, want true")
	}
}

func TestConfirmCleanupNo(t *testing.T) {
	if confirmCleanup(strings.NewReader("n\n")) {
		t.Error("confirmCleanup('n') = true, want false")
	}
}

func TestConfirmCleanupDefaultNo(t *testing.T) {
	if confirmCleanup(strings.NewReader("\n")) {
		t.Error("confirmCleanup(empty) = true, want false")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/... -run "TestCleanupCmd|TestConfirmCleanup" -v -count=1`

Expected: FAIL — `undefined: cleanupCmd`, `undefined: confirmCleanup`.

- [ ] **Step 3: Implement `cleanupCmd` and `confirmCleanup` in `internal/cli/root.go`**

Add `"bufio"` to the imports of `internal/cli/root.go` (`io` is already imported).

Add the command (place after `runCmd`):

```go
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove unused Docker resources to reclaim disk space",
	Long: `Remove unused Docker resources (containers, images, volumes, networks, build cache)
to reclaim disk space.

Tengiz-managed containers and images (labeled tengiz-app=*) are always protected.

By default all categories are cleaned. Use category flags to limit.

Flags:
  --dry-run     show reclaimable space without removing anything
  --force       skip the confirmation prompt
  --containers  only prune stopped non-Tengiz containers
  --images      only prune unused non-Tengiz images
  --volumes     only prune unused volumes
  --networks    only prune unused networks
  --cache       only prune the Docker build cache`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")
		containers, _ := cmd.Flags().GetBool("containers")
		images, _ := cmd.Flags().GetBool("images")
		volumes, _ := cmd.Flags().GetBool("volumes")
		networks, _ := cmd.Flags().GetBool("networks")
		cache, _ := cmd.Flags().GetBool("cache")

		opts := runtime.CleanupOptions{
			Containers: containers,
			Images:     images,
			Volumes:    volumes,
			Networks:   networks,
			Cache:      cache,
			DryRun:     dryRun,
		}

		rt, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}

		if !dryRun && !force {
			if !confirmCleanup(cmd.InOrStdin()) {
				fmt.Println("[tengiz] cleanup cancelled")
				return nil
			}
		}

		result, err := rt.Cleanup(cmd.Context(), opts)
		if err != nil {
			return err
		}

		for _, out := range result.Output {
			fmt.Print(out)
		}
		if dryRun {
			fmt.Println("[tengiz] dry run — nothing removed")
		} else {
			fmt.Printf("[tengiz] cleanup complete: %d containers, %d images, %d volumes, %d networks, %d cache entries\n",
				result.Containers, result.Images, result.Volumes, result.Networks, result.Cache)
		}
		return nil
	},
}

func confirmCleanup(r io.Reader) bool {
	fmt.Print("This will remove unused Docker resources. Continue? [y/N]: ")
	reader := bufio.NewReader(r)
	answer, err := reader.ReadString('\n')
	if err != nil && answer == "" {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
```

- [ ] **Step 4: Register the command and flags in `init()`**

In `init()` of `internal/cli/root.go`, after the `webhookCmd` flag registration:

```go
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().Bool("dry-run", false, "show reclaimable space without removing anything")
	cleanupCmd.Flags().Bool("force", false, "skip confirmation prompt")
	cleanupCmd.Flags().Bool("containers", false, "only prune containers")
	cleanupCmd.Flags().Bool("images", false, "only prune images")
	cleanupCmd.Flags().Bool("volumes", false, "only prune volumes")
	cleanupCmd.Flags().Bool("networks", false, "only prune networks")
	cleanupCmd.Flags().Bool("cache", false, "only prune build cache")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/... -run "TestCleanupCmd|TestConfirmCleanup" -v -count=1`

Expected: PASS.

- [ ] **Step 6: Build and run the CLI suite**

Run: `go build ./... && go test ./internal/cli/... -v -count=1`

Expected: Build succeeds; all CLI tests PASS (proxy-TCP-dependent tests are in `internal/proxy`, not here).

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cleanup_test.go
git commit -m "feat: add tengiz cleanup command with label-protected pruning"
```

---

### Task 6: Update docs and final verification

**Files:**
- Modify: `README.md` — add `tengiz cleanup` to CLI reference
- Modify: `docs/FUTURES_FEATURES.md` — mark feature #6 implemented
- No code changes

**Interfaces:**
- Consumes: nothing new
- Produces: accurate user-facing documentation

- [ ] **Step 1: Document `tengiz cleanup` in `README.md`**

Insert a new section after `### tengiz rm <app>` (around line 228):

```markdown
### `tengiz cleanup`

Remove unused Docker resources (containers, images, volumes, networks, build cache) to reclaim disk space. Tengiz-managed containers and images (labeled `tengiz-app=*`) are always protected.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show reclaimable space without removing anything |
| `--force` | Skip the confirmation prompt |
| `--containers` | Only prune stopped non-Tengiz containers |
| `--images` | Only prune unused non-Tengiz images |
| `--volumes` | Only prune unused volumes |
| `--networks` | Only prune unused networks |
| `--cache` | Only prune the Docker build cache |

With no category flags, all categories are cleaned. Without `--force` (and not `--dry-run`), the command asks for confirmation.
```

- [ ] **Step 2: Mark feature #6 as implemented in `docs/FUTURES_FEATURES.md`**

In the P0 table, replace:

```markdown
| 6 | **Docker Housekeeping** ⬜ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

with:

```markdown
| 6 | **Docker Housekeeping** ✅ | Yüksek | Düşük | Mükemmel | Disk space is the #1 production issue on single-server deployments. Label-based `docker system prune`. `tengiz cleanup`. |
```

Add to the "Implemented Features (Not Pending)" table:

```markdown
| — | **Docker Housekeeping** | Yüksek | Düşük | Mükemmel | ✅ Implemented (2026-08-17) |
```

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -v -count=1`

Expected: All PASS. Known slow/sensitive packages: `internal/proxy` (~2s each due to TCP dial timeouts) and `internal/idle` (time-sensitive, 50ms granularity — rerun individually if flaky).

- [ ] **Step 4: Run vet and build**

Run: `go vet ./... && go build -o tengiz .`

Expected: No vet issues; binary builds.

- [ ] **Step 5: Manual smoke test (requires Docker)**

```bash
./tengiz cleanup --dry-run
```

Expected: prints `docker system df` output plus `[tengiz] dry run — nothing removed`.

```bash
./tengiz cleanup --force
```

Expected: runs label-protected prunes and prints `[tengiz] cleanup complete: N containers, M images, ...`. Verify `docker ps -a` still shows any running/stopped Tengiz containers (label `tengiz-app=*` preserved).

- [ ] **Step 6: Commit**

```bash
git add README.md docs/FUTURES_FEATURES.md
git commit -m "docs: document tengiz cleanup and mark Docker Housekeeping implemented"
```

---

## Self-Review

**1. Spec coverage** (from `docs/FUTURES_FEATURES.md` #6 "Docker Housekeeping"): label-based pruning → Tasks 1-5 (`--filter label!=tengiz-app` on every prune); `tengiz cleanup` command → Task 5; protects Tengiz-managed containers/images → Task 2/3 (container label filter) + Task 4 (image labels); cleans volumes/networks/containers/images → Tasks 1-3 (all five categories). No gaps.

**2. Placeholder scan:** No TBD/TODO/“implement later”/“similar to” patterns. Every code step contains complete code.

**3. Type consistency:** `CleanupOptions`/`CleanupResult` field names (`Containers`, `Images`, `Volumes`, `Networks`, `Cache`, `DryRun`, `Output`) are identical across Tasks 1-3 and 5. `Cleanup(ctx, opts) (CleanupResult, error)` signature is consistent across the interface (Task 2), `dockerRuntime` (Task 3), stub (Task 2), and all three test mocks (Task 2). `buildImageLabels(appName, env)` is the same name in Task 4's test and implementation. `confirmCleanup(r io.Reader) bool` matches its test and CLI usage.